// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.

	config "github.com/kevinburke/ssh_config"
	"github.com/u-root/cpu/client"
	"github.com/u-root/cpu/ds"
	"github.com/u-root/u-root/pkg/ulog"

	// We use this ssh because it can unpack password-protected private keys.
	ossh "golang.org/x/crypto/ssh"
)

const defaultPort = "17010"

var (
	defaultKeyFile = filepath.Join(os.Getenv("HOME"), ".ssh/cpu_rsa")
	// For the ssh server part
	cpudCmd     = flag.String("cpudcmd", "decpud -remote", "cpud invocation to run at remote, e.g. decpud -d")
	debug       = flag.Bool("d", false, "enable debug prints")
	dbg9p       = flag.Bool("dbg9p", false, "show 9p io")
	dump        = flag.Bool("dump", false, "Dump copious output, including a 9p trace, to a temp file at exit")
	fstab       = flag.String("fstab", "", "pass an fstab to the cpud")
	hostKeyFile = flag.String("hk", "" /*"/etc/ssh/ssh_host_rsa_key"*/, "file for host key")
	keyFile     = flag.String("key", "", "key file")
	namespace   = flag.String("namespace", "/lib:/lib64:/usr:/bin:/etc:/home", "Default namespace for the remote process -- set to none for none")
	network     = flag.String("net", "", "network type to use. Defaults to whatever the cpu client defaults to")
	numCPUs     = flag.Int("n", 1, "number CPUs to run on")
	sp          = flag.String("sp", "", "cpu default port")
	root        = flag.String("root", "/", "9p root")
	timeout9P   = flag.String("timeout9p", "100ms", "time to wait for the 9p mount to happen.")
	ninep       = flag.Bool("9p", true, "Enable the 9p mount in the client")
	tmpMnt      = flag.String("tmpMnt", "/tmp", "Mount point of the private namespace.")
	// v allows debug printing.
	// Do not call it directly, call verbose instead.
	v          = func(string, ...interface{}) {}
	dumpWriter *os.File
)

func verbose(f string, a ...interface{}) {
	v("DECPU:"+f+"\r\n", a...)
}

func flags() {
	flag.Parse()
	if *dump && *debug {
		log.Fatalf("You can only set either dump OR debug")
	}
	if *debug {
		v = log.Printf
		client.SetVerbose(verbose)
		ds.Verbose(verbose)
	}
	if *dump {
		var err error
		dumpWriter, err = ioutil.TempFile("", "cpu")
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Logging to %s", dumpWriter.Name())
		*dbg9p = true
		ulog.Log = log.New(dumpWriter, "", log.Ltime|log.Lmicroseconds)
		v = ulog.Log.Printf
	}
}

// getKeyFile picks a keyfile if none has been set.
// It will use sshconfig, else use a default.
func getKeyFile(host, kf string) string {
	verbose("getKeyFile for %q", kf)
	if len(kf) == 0 {
		kf = config.Get(host, "IdentityFile")
		verbose("key file from config is %q", kf)
		if len(kf) == 0 {
			kf = defaultKeyFile
		}
	}
	// The kf will always be non-zero at this point.
	if strings.HasPrefix(kf, "~") {
		kf = filepath.Join(os.Getenv("HOME"), kf[1:])
	}
	verbose("getKeyFile returns %q", kf)
	// this is a tad annoying, but the config package doesn't handle ~.
	return kf
}

// getHostName reads the host name from the config file,
// if needed. If it is not found, the host name is returned.
func getHostName(host string) string {
	h := config.Get(host, "HostName")
	if len(h) != 0 {
		host = h
	}
	return host
}

// getPort gets a port.
// The rules here are messy, since config.Get will return "22" if
// there is no entry in .ssh/config. 22 is not allowed. So in the case
// of "22", convert to defaultPort
func getPort(host, port string) string {
	p := port
	verbose("getPort(%q, %q)", host, port)
	if len(port) == 0 {
		if cp := config.Get(host, "Port"); len(cp) != 0 {
			verbose("config.Get(%q,%q): %q", host, port, cp)
			p = cp
		}
	}
	if len(p) == 0 || p == "22" {
		p = defaultPort
		verbose("getPort: return default %q", p)
	}
	verbose("returns %q", p)
	return p
}

// TODO: we've been tryinmg to figure out the right way to do usage for years.
// If this is a good way, it belongs in the uroot package.
func usage() {
	var b bytes.Buffer
	flag.CommandLine.SetOutput(&b)
	flag.PrintDefaults()
	log.Fatalf("Usage: cpu [options] host [shell command]:\n%v", b.String())
}

func newCPU(host, port string, args ...string) error {
	// note that 9P is enabled if namespace is not empty OR if ninep is true
	c := client.Command(host, args...)
	if err := c.SetOptions(
		client.WithPrivateKeyFile(*keyFile),
		client.WithHostKeyFile(*hostKeyFile),
		client.WithPort(port),
		client.WithRoot(*root),
		client.WithNameSpace(*namespace),
		client.With9P(*ninep),
		client.WithFSTab(*fstab),
		client.WithCpudCommand(*cpudCmd),
		client.WithNetwork(*network),
		client.WithTempMount(*tmpMnt),
		client.WithTimeout(*timeout9P)); err != nil {
		log.Fatal(err)
	}

	c.Env = os.Environ()

	if err := c.Dial(); err != nil {
		return fmt.Errorf("Dial: %v", err)
	}
	verbose("CPU:start")
	if err := c.Start(); err != nil {
		return fmt.Errorf("Start: %v", err)
	}
	verbose("CPU:wait")
	if err := c.Wait(); err != nil {
		log.Printf("Wait: %v", err)
	}
	verbose("CPU:close")
	err := c.Close()
	verbose("CPU:close done")
	return err
}

func main() {
	flags()
	args := flag.Args()
	host := ds.DsDefault
	port := *sp
	a := []string{}
	if len(args) > 0 {
		host = args[0]
		a = args[1:]
	}
	if host == "." {
		host = ds.DsDefault
	}
	if len(a) == 0 {
		if *numCPUs > 1 {
			log.Fatal("Interactive access with more than one CPU is not supported (yet)")
		}
		shellEnv := os.Getenv("SHELL")
		if len(shellEnv) > 0 {
			a = []string{shellEnv}
		} else {
			a = []string{"/bin/sh"}
		}
	}

	// Try to parse it as a dnssd: path.
	// If that fails, we will run as though
	// it were just a host name.
	dq, err := ds.Parse(host)

	type cpu struct {
		host, port string
	}
	var cpus []cpu

	if err == nil {
		c, err := ds.Lookup(dq, *numCPUs)
		if err != nil {
			log.Printf("%v", err)
		}
		for _, e := range c {
			cpus = append(cpus, cpu{host: e.Entry.IPs[0].String(), port: strconv.Itoa(e.Entry.Port)})
		}
	} else {
		cpus = append(cpus, cpu{host: host, port: port})
	}

	verbose("Running as client, to host %q, args %q", host, a)

	var wg sync.WaitGroup
	for _, cpu := range cpus {
		wg.Add(1)
		*keyFile = getKeyFile(cpu.host, *keyFile)
		port = getPort(cpu.host, cpu.port)
		hn := getHostName(cpu.host)

		verbose("cpu to %v:%v", hn, port)
		if err := newCPU(hn, port, a...); err != nil {
			e := 1
			log.Printf("SSH error %s", err)
			sshErr := &ossh.ExitError{}
			if errors.As(err, &sshErr) {
				e = sshErr.ExitStatus()
			}
			log.Printf("%v", e)
		}
		wg.Done()
	}
	wg.Wait()
}
