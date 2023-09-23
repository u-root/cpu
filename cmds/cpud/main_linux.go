// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"log"
	"os"
	"time"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.

	"github.com/u-root/cpu/session"
)

var (
	// For the ssh server part
	hostKeyFile = flag.String("hk", "" /*"/etc/ssh/ssh_host_rsa_key"*/, "file for host key")
	pubKeyFile  = flag.String("pk", "key.pub", "file for public key")
	port        = flag.String("sp", "17010", "cpu default port")

	debug     = flag.Bool("d", false, "enable debug prints")
	runAsInit = flag.Bool("init", false, "run as init (Debug only; normal test is if we are pid 1")
	// v allows debug printing.
	// Do not call it directly, call verbose instead.
	v       = func(string, ...interface{}) {}
	remote  = flag.Bool("remote", false, "indicates we are the remote side of the cpu session")
	network = flag.String("net", "tcp", "network to use")
	port9p  = flag.String("port9p", "", "port9p # on remote machine for 9p mount")
	klog    = flag.Bool("klog", false, "Log cpud messages in kernel log, not stdout")

	// Some networks are not well behaved, and for them we implement registration.
	registerAddr = flag.String("register", "", "address and port to register with after listen on cpu server port")
	registerTO   = flag.Duration("registerTO", time.Duration(5*time.Second), "time.Duration for Dial address for registering")

	// if we start up too quickly, mDNS won't work correctly.
	// This sleep may be useful for other cases, so it is here,
	// not specifically for mDNS uses.
	sleepBeforeServing = flag.Duration("sleepBeforeServing", 0, "add a sleep before serving -- usually only needed if cpud runs as init with mDNS")

	pid1 bool
)

func verbose(f string, a ...interface{}) {
	if *remote {
		v("CPUD(remote):"+f+"\r\n", a...)
	} else {
		v("CPUD:"+f, a...)
	}
}

// There are three distinct cases to cover.
//  1. running as init (indicated by pid == 1 OR -init=true switch
//  2. running as server. pid != 1 AND -remote=true AND -init=false
//  3. running as 'remote', i.e. the thing that starts a command for
//     a client. Indicated by remote=true.
//
// case (3) overrides case 2 and 1.
// This has evolved over the years, and, likely, the init and remote
// switches ought to be renamed to 'role'. But so it goes.
// The rules on arguments are very strict now. In the remote case,
// os.Args[1] MUST be remote; no other invocation is accepted, because
// the args to remote and the args to server are different.
// This invocation requirement is known to the server package.
func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-remote" || os.Args[1] == "-remote=true") {
		*remote = true
	}

	if *remote {
		// remote has far fewer args. Since they are specified by the client,
		// we want to limit the set of args it can set.
		flag.CommandLine = flag.NewFlagSet("cpud-remote", flag.ExitOnError)
		debug = flag.Bool("d", false, "enable debug prints")
		remote = flag.Bool("remote", false, "indicates we are the remote side of the cpu session")
		port9p = flag.String("port9p", "", "port9p # on remote machine for 9p mount")

		flag.Parse()
		if *debug {
			v = log.Printf
			session.SetVerbose(verbose)
		}
		// If we are here, no matter what they may set, *remote must be true.
		// sadly, cpud -d -remote=true -remote=false ... works.
		*remote = true
	} else {
		flag.Parse()
		// If we are here, no matter what they may set, *remote must be false.
		*remote = false
		if err := commonsetup(); err != nil {
			log.Fatal(err)
		}
	}
	pid := os.Getpid()
	pid1 = pid == 1
	*runAsInit = *runAsInit || pid1
	verbose("Args %v pid %d *runasinit %v *remote %v env %v", os.Args, pid, *runAsInit, *remote, os.Environ())
	args := flag.Args()
	if *remote {
		verbose("args %q, port9p %v", args, *port9p)

		// This can happen if the user gets clever and
		// invokes cpu with, e.g., nothing but switches.
		if len(args) == 0 {
			shell, ok := os.LookupEnv("SHELL")
			if !ok {
				log.Fatal("No arguments and $SHELL is not set")
			}
			args = []string{shell}
		}
		s := session.New(*port9p, args[0], args[1:]...)
		if err := s.Run(); err != nil {
			log.Fatalf("CPUD(remote): %v", err)
		}
	} else {
		log.Printf("CPUD:PID(%d):running as a server (a.k.a. starter of cpud's for sessions)", pid)
		if *runAsInit {
			log.Printf("CPUD:also running as init")
			if err := initsetup(); err != nil {
				log.Fatal(err)
			}
		}
		time.Sleep(*sleepBeforeServing)
		if err := serve(os.Args[0]); err != nil {
			log.Fatal(err)
		}
	}
}
