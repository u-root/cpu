// Copyright 2024 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
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
	isShared := strings.Contains(entry.Optional, "shared:1")
	if isShared {
		return fmt.Errorf("/ is not private")
	}
	return nil
}

func sudoUnshareCpunfs(env string, args ...string) error {
	n, err := os.Executable()
	if err != nil {
		return err
	}

	v("Executable: %q", n)
	// sshd filters most environment variables save LC_*.
	// sudo strips most LC_* variables.
	// the cpu command sets LC_GLENDA_CPU_FSTAB to the fstab;
	// we need to transform it here.

	c := exec.Command("sudo", append([]string{"-E", "unshare", "-m", n, "-env=" + env}, args...)...)
	v("exec.Cmd args %q", c.Args)

	// Find the environment variable, and transform it.
	// sudo or unshare seem to strip many LC_* variables.
	fstab, ok := os.LookupEnv("LC_GLENDA_CPU_FSTAB")
	v("fstab set? %v value %q", ok, fstab)
	if ok {
		c.Env = append(c.Env, "CPU_FSTAB="+fstab)
		v("extended c.Env: %v", c.Env)
	}
	c.Stdin, c.Stdout, c.Stderr, c.Dir = os.Stdin, os.Stdout, os.Stderr, os.Getenv("PWD")
	v("Run %q", c)
	return c.Run()
}

// We make an effort here to make this convenient, accepting the risk
// of a fork bomb. Such bombs rarely if ever take systems down any
// more anyway ...
func main() {
	flag.CommandLine = flag.NewFlagSet("cpuns", flag.ExitOnError)
	debug := flag.Bool("d", false, "enable debug prints")
	env := flag.String("env", "", "newline-separated array of environment variables")
	flag.Parse()
	if *debug {
		v = log.Printf
		verbose("cpuns: os.Args %q", os.Args)
		session.SetVerbose(v)
	}
	args := flag.Args()
	v("LC_GLENDA_CPU_FSTAB %s", os.Getenv("LC_GLENDA_CPU_FSTAB"))
	v("CPU_FSTAB %s", os.Getenv("CPU_FSTAB"))
	if os.Getuid() != 0 {
		if err := sudoUnshareCpunfs(*env, args...); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}
	if err := checkprivate(); err != nil {
		log.Fatal(err)
	}
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

	uid, err := strconv.ParseUint(u, 0, 32)
	if err != nil {
		log.Fatal(err)
	}

	gid, err := strconv.ParseUint(g, 0, 32)
	if err != nil {
		log.Fatal(err)
	}
	c := s.Command()
	if s := strings.Split(*env, "\n"); len(s) > 0 {
		c.Env = append(c.Env, s...)
	}
	verbose("cpuns: Command is %q, with args %q", c, args)
	c.Stdin, c.Stdout, c.Stderr = s.Stdin, s.Stdout, s.Stderr
	c.SysProcAttr = &syscall.SysProcAttr{}
	c.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	if err := c.Run(); err != nil {
		log.Fatalf("Run %v returns %v", c, err)
	}

}
