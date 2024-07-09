// Copyright 2024 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"log"
	"os"
	"runtime"
	"syscall"

	"github.com/u-root/cpu/session"
	"golang.org/x/sys/unix"
)

var (
	// v allows debug printing.
	// Do not call it directly, call verbose instead.
	v = func(string, ...interface{}) {}
)

// os.Unshare is tricky to call. Go runtime does not really handle it
// correctly; if you have muiltiple procs, it only runs for one of them.
// init functions run in single-process context, or so we understand ...
func init() {
	m := runtime.GOMAXPROCS(1)
	if err := syscall.Unshare(syscall.CLONE_NEWNS); err != nil {
		log.Fatalf("can not run if unshare fails: %v", err)
	}
	if err := unix.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		log.Fatalf(`unix.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); %v`, err)
	}
	runtime.GOMAXPROCS(m)
}

func verbose(f string, a ...interface{}) {
	v("CPUNS:"+f, a...)
}

func main() {
	flag.CommandLine = flag.NewFlagSet("cpuns", flag.ExitOnError)
	debug := flag.Bool("d", false, "enable debug prints")
	flag.Parse()
	if *debug {
		v = log.Printf
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
	if err := s.Run(); err != nil {
		log.Fatalf("CPUD(remote): %v", err)
	}
}
