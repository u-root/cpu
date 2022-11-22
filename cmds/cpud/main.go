// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"log"
	"os"

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

	pid1 bool
)

func verbose(f string, a ...interface{}) {
	if *remote {
		v("CPUD(remote):"+f+"\r\n", a...)
	} else {
		v("CPUD:"+f, a...)
	}
}

func main() {
	flag.Parse()
	if *remote {
		if *debug {
			v = log.Printf
			session.SetVerbose(log.Printf)
		}
	} else {
		if err := commonsetup(); err != nil {
			log.Fatal(err)
		}
	}
	pid1 = os.Getpid() == 1
	*runAsInit = *runAsInit || pid1
	verbose("Args %v pid %d *runasinit %v *remote %v env %v", os.Args, os.Getpid(), *runAsInit, *remote, os.Environ())
	args := flag.Args()
	if *remote {
		verbose("server package: Running as remote: args %q, port9p %v", args, *port9p)
		tmpMnt, ok := os.LookupEnv("CPU_TMPMNT")
		if !ok || len(tmpMnt) == 0 {
			tmpMnt = "/tmp"
		}
		s := session.New(*port9p, tmpMnt, args[0], args[1:]...)
		if err := s.Run(); err != nil {
			log.Fatalf("CPUD(as remote):%v", err)
		}
	} else {
		log.Printf("CPUD:running as a server (a.k.a. starter of cpud's for sessions)")
		if *runAsInit {
			if err := initsetup(); err != nil {
				log.Fatal(err)
			}
		}
		if err := serve(); err != nil {
			log.Fatal(err)
		}
	}
}
