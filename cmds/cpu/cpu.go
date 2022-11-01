// Copyright 2018-2019 the u-root Authors. All rights reserved
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
	"strings"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.

	config "github.com/kevinburke/ssh_config"
	"github.com/u-root/cpu/client"
	"github.com/u-root/u-root/pkg/ulog"

	// We use this ssh because it can unpack password-protected private keys.
	ossh "golang.org/x/crypto/ssh"
)

const defaultPort = "17010"

var (
	defaultKeyFile = filepath.Join(os.Getenv("HOME"), ".ssh/cpu_rsa")
	// For the ssh server part
	cpudCmd     = flag.String("cpudcmd", "", "cpud invocation to run at remote, e.g. cpud -d")
	debug       = flag.Bool("d", false, "enable debug prints")
	dbg9p       = flag.Bool("dbg9p", false, "show 9p io")
	dump        = flag.Bool("dump", false, "Dump copious output, including a 9p trace, to a temp file at exit")
	fstab       = flag.String("fstab", "", "pass an fstab to the cpud")
	hostKeyFile = flag.String("hk", "" /*"/etc/ssh/ssh_host_rsa_key"*/, "file for host key")
	keyFile     = flag.String("key", "", "key file")
	namespace   = flag.String("namespace", "/lib:/lib64:/usr:/bin:/etc:/home", "Default namespace for the remote process -- set to none for none")
	network     = flag.String("net", "", "network type to use. Defaults to whatever the cpu client defaults to")
	port        = flag.String("sp", "", "cpu default port")
	root        = flag.String("root", "/", "9p root")
	timeout9P   = flag.String("timeout9p", "100ms", "time to wait for the 9p mount to happen.")
	ninep       = flag.Bool("9p", true, "Enable the 9p mount in the client")
	tmpMnt      = flag.String("tmpMnt", "/tmp", "Mount point of the private namespace.")

	v          = func(string, ...interface{}) {}
	dumpWriter *os.File
)

func verbose(f string, a ...interface{}) {
	v("\r\n"+f+"\r\n", a...)
}

func flags() {
	flag.Parse()
	if *dump && *debug {
		log.Fatalf("You can only set either dump OR debug")
	}
	if *debug {
		v = log.Printf
		client.SetVerbose(log.Printf)
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
	v("getKeyFile for %q", kf)
	if len(kf) == 0 {
		kf = config.Get(host, "IdentityFile")
		v("key file from config is %q", kf)
		if len(kf) == 0 {
			kf = defaultKeyFile
		}
	}
	// The kf will always be non-zero at this point.
	if strings.HasPrefix(kf, "~") {
		kf = filepath.Join(os.Getenv("HOME"), kf[1:])
	}
	v("getKeyFile returns %q", kf)
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
	v("getPort(%q, %q)", host, port)
	if len(port) == 0 {
		if cp := config.Get(host, "Port"); len(cp) != 0 {
			v("config.Get(%q,%q): %q", host, port, cp)
			p = cp
		}
	}
	if len(p) == 0 || p == "22" {
		p = defaultPort
		v("getPort: return default %q", p)
	}
	v("returns %q", p)
	return p
}

func newCPU(host string, args ...string) error {
	client.V = v
	// note that 9P is enabled if namespace is not empty OR if ninep is true
	c := client.Command(host, args...)
	defer c.Close()
	if err := c.SetOptions(
		client.WithPrivateKeyFile(*keyFile),
		client.WithHostKeyFile(*hostKeyFile),
		client.WithPort(*port),
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
	if err := c.Dial(); err != nil {
		return fmt.Errorf("Dial: %v", err)
	}
	v("CPU:start")
	if err := c.Start(); err != nil {
		return fmt.Errorf("Start: %v", err)
	}
	v("CPU:wait")
	if err := c.Wait(); err != nil {
		return fmt.Errorf("Wait: %v", err)
	}
	v("CPU:close")
	if err := c.Close(); err != nil {
		return fmt.Errorf("Close: %v", err)
	}
	v("CPU:close done")
	return nil
}

func usage() {
	var b bytes.Buffer
	flag.CommandLine.SetOutput(&b)
	flag.PrintDefaults()
	log.Fatalf("Usage: cpu [options] host [shell command]:\n%v", b.String())
}

func main() {
	flags()
	args := flag.Args()
	if len(args) == 0 {
		usage()
	}
	host := args[0]
	a := strings.Join(args[1:], " ")
	verbose("Running as client, to host %q, args %q", host, a)
	if len(a) == 0 {
		a = os.Getenv("SHELL")
		if len(a) == 0 {
			a = "/bin/sh"
		}
	}

	*keyFile = getKeyFile(host, *keyFile)
	*port = getPort(host, *port)
	hn := getHostName(host)

	v("Running package-based cpu command")
	if err := newCPU(hn, a); err != nil {
		e := 1
		log.Printf("SSH error %s", err)
		sshErr := &ossh.ExitError{}
		if errors.As(err, &sshErr) {
			e = sshErr.ExitStatus()
		}
		defer os.Exit(e)
	}
}
