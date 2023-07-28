// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// Namespace assembles a NameSpace for this cpud, iff CPU_NONCE
// is set and len(s.binds) > 0.
//
// This code assumes you have a non-shared namespace. This is
// archieved in go by setting exec.Cmd.SysprocAttr.Unshareflags to
// CLONE_NEWNS; the go runtime will then do what is needed to
// privatize a namespace. I can say this because I wrote that code 5
// years ago, and go tests for it are run as part of the go
// release process.
//
// To reiterate, this package requires, for proper operation, that the
// process using it be in a private name space, and, further, that the
// namespace can't magically be reshared.
//
// It's very hard and probably impossible to test for a namespace
// being set up properly on Linux. On plan 9 it's easy: read the
// process namespace file and see if it's empty. But no such operation
// is possible on Linux and, worse, since sometime in the 3.x kernel
// series, even once a namespace is unshared, another process can
// start using it via nsenter(2).
//
// Hence this note: it is a warning to our future selves or users of
// this package.
//
// Note, however, that cpud does the right thing, by setting
// Unshareflags to CLONE_NEWNS. Tests in the cpu server code ensure
// that continues to be the case.
//
// tl;dr: Linux namespaces are a pretty terrible mess. They may have
// been inspired by Plan 9, but an understanding of some critical core
// ideas has been lost. As a result, they do not remotely represent
// any kind of security boundary.
func (s *Session) Namespace() error {
	// Get the nonce and remove it from the environment.
	// N.B. We do not save the nonce in the cpu struct.
	nonce, ok := os.LookupEnv("CPUNONCE")
	if !ok {
		return nil
	}
	os.Unsetenv("CPUNONCE")

	// Connect to the socket, return the nonce.
	var (
		so net.Conn
		// using this array avoids using the name
		// "localhost", which makes the code
		// insenstive to missing/nmisconfigured
		// /etc/hosts files.
		// ip6 is first, because that's where
		// the world is going.
		localhosts = []string{"::1", "127.0.0.1"}
		errs       [2]error
	)

	for i, h := range localhosts {
		a := net.JoinHostPort(h, s.port9p)
		verbose("Dial %v", a)
		so, errs[i] = net.Dial("tcp", a)
		// Error trees are not quite ready yet.
		if errs[i] == nil {
			break
		}
		verbose("Dial %v:%v", a, errs[i])
	}
	// TODO: once everyone is up to go 1.20, use the error appending
	if so == nil {
		return fmt.Errorf("CPUD:Dial 9p port to %q: %v", localhosts, errs)
	}
	verbose("Connected: write nonce %s\n", nonce)
	if _, err := fmt.Fprintf(so, "%s", nonce); err != nil {
		return fmt.Errorf("CPUD:Write nonce: %v", err)
	}
	verbose("Wrote the nonce")
	// Zero it. I realize I am not a crypto person.
	// improvements welcome.
	copy([]byte(nonce), make([]byte, len(nonce)))

	// the kernel takes over the socket after the Mount.
	defer so.Close()
	flags := uintptr(unix.MS_NODEV | unix.MS_NOSUID)
	cf, err := so.(*net.TCPConn).File()
	if err != nil {
		return fmt.Errorf("CPUD:Cannot get fd for %v: %v", so, err)
	}

	fd := cf.Fd()
	verbose("fd is %v", fd)

	user := os.Getenv("USER")
	if user == "" {
		user = "nouser"
	}

	// The debug= option is here so you can see how to temporarily set it if needed.
	// It generates copious output so use it sparingly.
	// A useful compromise value is 5.
	opts := fmt.Sprintf("version=9p2000.L,trans=fd,rfdno=%d,wfdno=%d,uname=%v,debug=0,msize=%d", fd, fd, user, s.msize)
	if len(s.mopts) > 0 {
		opts += "," + s.mopts
	}
	mountTarget := filepath.Join(os.TempDir(), "cpu")
	verbose("mount 127.0.0.1 on %s 9p %#x %s", mountTarget, flags, opts)
	if err := unix.Mount("localhost", mountTarget, "9p", flags, opts); err != nil {
		return fmt.Errorf("9p mount %v", err)
	}
	verbose("mount done")

	return nil
}

func osMounts() error {
	var errs error
	tmpMnt := os.TempDir()
	// Further, bind / onto /tmp/local so a non-hacked-on version may be visible.
	if err := unix.Mount("/", filepath.Join(tmpMnt, "local"), "", syscall.MS_BIND, ""); err != nil {
		errs = errors.Join(errs, fmt.Errorf("CPUD:Warning: binding / over %s did not work: %v, continuing anyway", filepath.Join(tmpMnt, "local"), err))
	}
	return errs
}

// runSetup performs kernel-specific operations for starting a Session.
func runSetup() error {
	tmpMnt := os.TempDir()
	if err := unix.Mount("cpu", tmpMnt, "tmpfs", 0, ""); err != nil {
		return fmt.Errorf(`unix.Mount("cpu", %s, "tmpfs", 0, ""); %v != nil`, tmpMnt, err)
	}
	return nil
}
