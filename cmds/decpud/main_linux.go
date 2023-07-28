// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux
// +build linux

package main

import (
	"flag"
	"log"
	"os"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.

	"github.com/u-root/cpu/ds"
	"github.com/u-root/cpu/session"
)

var (
	// For the ssh server part
	hostKeyFile = flag.String("hk", "" /*"/etc/ssh/ssh_host_rsa_key"*/, "file for host key")
	pubKeyFile  = flag.String("pk", "key.pub", "file for public key")
	port        = flag.String("sp", "17010", "cpu default port")

	debug     = flag.Bool("d", true, "enable debug prints")
	runAsInit = flag.Bool("init", false, "run as init (Debug only; normal test is if we are pid 1")
	v         = func(string, ...interface{}) {}
	remote    = flag.Bool("remote", false, "indicates we are the remote side of the cpu session")
	network   = flag.String("net", "tcp", "network to use")
	port9p    = flag.String("port9p", "", "port9p # on remote machine for 9p mount")
	klog      = flag.Bool("klog", false, "Log cpud messages in kernel log, not stdout")

	dsEnabled   = flag.Bool("dnssd", true, "advertise service using DNSSD")
	dsInstance  = flag.String("dsInstance", "", "DNSSD instance name")
	dsDomain    = flag.String("dsDomain", "local", "DNSSD domain")
	dsService   = flag.String("dsService", "_ncpu._tcp", "DNSSD Service Type")
	dsInterface = flag.String("dsInterface", "", "DNSSD Interface")
	dsTxtStr    = flag.String("dsTxt", "", "DNSSD key-value pair string parameterizing advertisement")
	dsTxt       map[string]string
	pid1        bool
)

func verbose(f string, a ...interface{}) {
	v("\r\nCPUD:"+f+"\r\n", a...)
}

func main() {
	flag.Parse()
	dsTxt = ds.ParseKv(*dsTxtStr)
	pid1 = os.Getpid() == 1
	*runAsInit = *runAsInit || pid1
	args := flag.Args()
	switch {
	case *runAsInit:
		if err := commonsetup(); err != nil {
			log.Fatal(err)
		}
		if err := initsetup(); err != nil {
			log.Fatal(err)
		}
		if err := serve(os.Args[0]); err != nil {
			log.Fatal(err)
		}
	case *remote:
		verbose("server package: Running as remote: args %q, port9p %v", args, *port9p)
		s := session.New(*port9p, args[0], args[1:]...)
		if err := s.Run(); err != nil {
			log.Fatalf("CPUD(as remote):%v", err)
		}
	default:
		log.Printf("cpud: running as a server (a.k.a. starter of cpud's for sessions")
		if err := commonsetup(); err != nil {
			log.Fatal(err)
		}
		if err := serve(os.Args[0]); err != nil {
			log.Fatal(err)
		}
	}
}
