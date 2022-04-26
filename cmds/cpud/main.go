// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"flag"
	"log"
	"os"
	"strings"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.
	// TODO: get rid of krpty

	"github.com/u-root/cpu/session"
)

var (
	// For the ssh server part
	hostKeyFile = flag.String("hk", "" /*"/etc/ssh/ssh_host_rsa_key"*/, "file for host key")
	pubKeyFile  = flag.String("pk", "key.pub", "file for public key")
	port        = flag.String("sp", "23", "cpu default port")

	debug     = flag.Bool("d", true, "enable debug prints")
	runAsInit = flag.Bool("init", false, "run as init (Debug only; normal test is if we are pid 1")
	v         = func(string, ...interface{}) {}
	remote    = flag.Bool("remote", false, "indicates we are the remote side of the cpu session")
	network   = flag.String("network", "tcp", "network to use")
	bin       = flag.String("bin", "cpu", "path of cpu binary")
	port9p    = flag.String("port9p", "", "port9p # on remote machine for 9p mount")
	dbg9p     = flag.String("dbg9p", "0", "show 9p io")
	root      = flag.String("root", "/", "9p root")
	klog      = flag.Bool("klog", false, "Log cpud messages in kernel log, not stdout")

	mountopts = flag.String("mountopts", "", "Extra options to add to the 9p mount")
	msize     = flag.Int("msize", 1048576, "msize to use")
	// To get debugging when Things Go Wrong, you can run as, e.g., -wtf /bbin/elvish
	// or change the value here to /bbin/elvish.
	// This way, when Things Go Wrong, you'll be dropped into a shell and look around.
	// This is sometimes your only way to debug if there is (e.g.) a Go runtime
	// bug around unsharing. Which has happened.
	wtf  = flag.String("wtf", "", "Command to run if setup (e.g. private name space mounts) fail")
	pid1 bool
	// This flag indicates we are running the package version of the server.
	packageServer = flag.Bool("new", true, "use Package version of cpud")
)

func verbose(f string, a ...interface{}) {
	v("\r\nCPUD:"+f+"\r\n", a...)
}

// errval can be used to examine errors that we don't consider errors
func errval(err error) error {
	if err == nil {
		return err
	}
	// Our zombie reaper is occasionally sneaking in and grabbing the
	// child's exit state. Looks like our process code still sux.
	if strings.Contains(err.Error(), "no child process") {
		return nil
	}
	return err
}

// TODO: we've been trying to figure out the right way to do usage for years.
// If this is a good way, it belongs in the uroot package.
func usage() {
	var b bytes.Buffer
	flag.CommandLine.SetOutput(&b)
	flag.PrintDefaults()
	log.Fatalf("Usage: cpu [options] host [shell command]:\n%v", b.String())
}

func main() {
	flag.Parse()
	pid1 = os.Getpid() == 1
	*runAsInit = *runAsInit || pid1
	verbose("Args %v pid %d *runasinit %v *remote %v env %v", os.Args, os.Getpid(), *runAsInit, *remote, os.Environ())
	args := flag.Args()
	switch {
	case *runAsInit:
		if err := serve(); err != nil {
			log.Fatal(err)
		}
	case *remote:
		verbose("server package: Running as remote: args %q, port9p %v", args, *port9p)
		s := session.New(*port9p, args[0], args[1:]...)
		if err := s.Run(); err != nil {
			log.Fatalf("CPUD(as remote):%v", err)
		}
		break
	default:
		log.Fatal("CPUD:can only run as remote or pid 1")
	}
}
