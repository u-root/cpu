// Copyright 2024 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/moby/sys/mountinfo"
	"github.com/u-root/cpu/session"
)

var (
	// v allows debug printing.
	// Do not call it directly, call verbose instead.
	v = func(string, ...interface{}) {}
)

func verbose(f string, a ...interface{}) {
	v("CPUNS:"+f, a...)
}

func checkprivate() error {
	mnts, err := mountinfo.GetMounts(mountinfo.SingleEntryFilter("/"))
	if err != nil {
		return err
	}
	if len(mnts) != 1 {
		return fmt.Errorf("got more than 1 mount for /(%v):%v", mnts, os.ErrInvalid)
	}
	entry := mnts[0]
	fmt.Printf("Optional is %q\n", entry.Optional)
	isShared := strings.Contains(entry.Optional, "shared:1")
	if isShared {
		return fmt.Errorf("/ is not private")
	}
	return nil
}

func main() {
	fmt.Printf("uid %v git %v", os.Getuid(), os.Getgid())
	if err := checkprivate(); err != nil {
		log.Fatal(err)
	}
	flag.CommandLine = flag.NewFlagSet("cpuns", flag.ExitOnError)
	debug := flag.Bool("d", false, "enable debug prints")
	flag.Parse()
	if *debug {
		v = log.Printf
		session.SetVerbose(v)
	}
	args := flag.Args()
	shell := "/bin/sh"
	if len(args) == 0 {
		sh, ok := os.LookupEnv("SHELL")
		if ok {
			shell = sh
		}
		args = []string{shell}
	}

	// Can not set the first arg (port9p) since there is no
	// good way to pass it (it is passed as as switch in cpud).
	// That is ok, 9p has never been that good on Linux.
	s := session.New("", args[0], args[1:]...)
	if err := s.NameSpace(); err != nil {
		log.Fatalf("CPUD(remote): %v", err)
	}

	if err := s.Terminal(); err != nil {
		log.Printf("s.Terminal failed(%v); continuing anyway", err)
	}

	u, ok := os.LookupEnv("SUDO_UID")
	if !ok {
		log.Printf("no SUDO_UID; continuing anyway")
	}
	g, ok := os.LookupEnv("SUDO_GID")
	if !ok {
		log.Printf("no SUDO_GID; continuing anyway")
	}

	uid, err := strconv.Atoi(u)
	if err != nil {
		log.Fatal(err)
	}

	gid, err := strconv.Atoi(g)
	if err != nil {
		log.Fatal(err)
	}
	c := s.Command()
	c.Stdin, c.Stdout, c.Stderr = s.Stdin, s.Stdout, s.Stderr
	c.SysProcAttr = &syscall.SysProcAttr{}
	c.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	if err := c.Run(); err != nil {
		log.Fatalf("Run %v returns %v", c, err)
	}

}
