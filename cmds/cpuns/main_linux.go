// Copyright 2024 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This program is used when cpud is not available.
// It takes a mount point environment variable, LC_CPU_FSTAB,
// and environment specified by -env, and invokes itself
// several times, as needed, to create a private name space
// for a user command.
// This all gets a bit tricky as we must do a few things as
// root, and setuid back in the end. In an earlier version, we
// used unshare, but that introduces a dependency we would prefer
// not to have.
// The sequence:
// 1st pass: check if we are uid 0, if not, sudo os.Executable
// 2nd pass: we are uid 0, check if namespace is private, if not,
//
//	execute ourselves with cmd.SysProcattr.Unshareflags set to NS_PRIVATE
//	This will only succeed or fail; it won't start the process
//	with a non-private namespace.
//
// 3rd pass: create the proper mounts, and execute the user command.
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
		return fmt.Errorf("got more than 1 mount for /(%v):%w", mnts, os.ErrInvalid)
	}
	entry := mnts[0]
	isShared := strings.Contains(entry.Optional, "shared:1")
	if isShared {
		return fmt.Errorf("/ is not private")
	}
	return nil
}

// sudo will get us into a root process, with correct environment
// set up.
func sudo(env string, args ...string) {
	n, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}

	v("Executable: %q", n)
	// sshd filters most environment variables save LC_*.
	// sudo strips most LC_* variables.
	// the cpu command sets LC_GLENDA_CPU_FSTAB to the fstab;
	// we need to transform it here.

	c := exec.Command("sudo", append([]string{n, "-env=" + env}, args...)...)
	v("exec.Cmd args %q", c.Args)

	// Find the environment variable, and transform it.
	// sudo seems to strip many LC_* variables.
	fstab, ok := os.LookupEnv("LC_GLENDA_CPU_FSTAB")
	v("fstab set? %v value %q", ok, fstab)
	if ok {
		c.Env = append(c.Env, "CPU_FSTAB="+fstab)
		v("extended c.Env: %v", c.Env)
	}
	if s := strings.Split(env, "\n"); len(s) > 0 {
		c.Env = append(c.Env, s...)
	}
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	v("Run %q", c)

	// The return is carefully done here to avoid the caller
	// making a mistake and fork-bomb.
	if err := c.Run(); err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}

// unshare execs os.Executable with SysProcAttr.Unshareflags set to
// CLONE_NEWNS. It avoids a need to use the unshare command.
// Be very careful in modifying this; it is designed to be
// simple and avoid fork bombs.
func unshare(env string, args ...string) {
	n, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}

	// sshd filters most environment variables save LC_*.
	// sudo strips most LC_* variables.
	// the cpu command sets LC_GLENDA_CPU_FSTAB to the fstab;
	// we need to transform it here.
	// Since we can get here direct from sshd, not sudo,
	// we have to this twice.
	v("Executable: %q", n)
	c := exec.Command(n, args...)
	v("exec.Cmd args %q", c.Args)

	c.Env = os.Environ()
	if s := strings.Split(env, "\n"); len(s) > 0 {
		c.Env = append(c.Env, s...)
	}

	fstab, ok := os.LookupEnv("LC_GLENDA_CPU_FSTAB")
	v("fstab set? %v value %q", ok, fstab)
	if ok {
		c.Env = append(c.Env, "CPU_FSTAB="+fstab)
		v("extended c.Env: %v", c.Env)
	}

	c.Stdin, c.Stdout, c.Stderr, c.Dir = os.Stdin, os.Stdout, os.Stderr, os.Getenv("PWD")
	v("Run %q", c)

	c.SysProcAttr = &syscall.SysProcAttr{Unshareflags: syscall.CLONE_NEWNS}
	// The return is carefully done here to avoid the caller
	// making a mistake and fork-bomb.
	if err := c.Run(); err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
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
	v("env\n\n%q\n\n", *env)
	if os.Getuid() != 0 {
		sudo(*env, args...)
	}

	if err := checkprivate(); err != nil {
		unshare(*env, args...)
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

	// the default value of uid, gid is 0
	uid := uint32(syscall.Getuid())
	gid := uint32(syscall.Getgid())
	if u, ok := os.LookupEnv("SUDO_UID"); ok {
		i, err := strconv.ParseUint(u, 0, 32)
		if err != nil {
			log.Fatal(err)
		}
		uid = uint32(i)
	}
	if g, ok := os.LookupEnv("SUDO_GID"); ok {
		i, err := strconv.ParseUint(g, 0, 32)
		if err != nil {
			log.Fatal(err)
		}
		gid = uint32(i)
	}

	c := s.Command()
	c.Env = os.Environ()
	if s := strings.Split(*env, "\n"); len(s) > 0 {
		c.Env = append(c.Env, s...)
	}
	pwd := os.Getenv("CPU_PWD")
	if _, err := os.Stat(pwd); err != nil {
		log.Printf("%v:setting pwd to /", err)
		pwd = "/"
	}
	c.Stdin, c.Stdout, c.Stderr, c.Dir = s.Stdin, s.Stdout, s.Stderr, pwd
	c.SysProcAttr = &syscall.SysProcAttr{}
	c.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	if err := c.Run(); err != nil {
		log.Fatalf("Run %v returns %v", c, err)
	}

}
