// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"github.com/hashicorp/go-multierror"
	"golang.org/x/sys/unix"
)

// Namespace assembles a NameSpace for this cpud, iff CPU_NONCE
// is set and len(s.binds) > 0.
//
// This code assumes you have a non-shared namesapce. This is
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
func (s *Session) Namespace() (error, error) {
	if len(s.binds) == 0 {
		return nil, nil
	}
	// Get the nonce and remove it from the environment.
	// N.B. We do not save the nonce in the cpu struct.
	nonce, ok := os.LookupEnv("CPUNONCE")
	if !ok {
		return nil, nil
	}
	os.Unsetenv("CPUNONCE")
	v("CPUD:namespace is %q", s.binds)

	// Connect to the socket, return the nonce.
	a := net.JoinHostPort("127.0.0.1", s.port9p)
	v("CPUD:Dial %v", a)
	so, err := net.Dial("tcp4", a)
	if err != nil {
		return nil, fmt.Errorf("CPUD:Dial 9p port: %v", err)
	}
	v("CPUD:Connected: write nonce %s\n", nonce)
	if _, err := fmt.Fprintf(so, "%s", nonce); err != nil {
		return nil, fmt.Errorf("CPUD:Write nonce: %v", err)
	}
	v("CPUD:Wrote the nonce")
	// Zero it. I realize I am not a crypto person.
	// improvements welcome.
	copy([]byte(nonce), make([]byte, len(nonce)))

	// the kernel takes over the socket after the Mount.
	defer so.Close()
	flags := uintptr(unix.MS_NODEV | unix.MS_NOSUID)
	cf, err := so.(*net.TCPConn).File()
	if err != nil {
		return nil, fmt.Errorf("CPUD:Cannot get fd for %v: %v", so, err)
	}

	fd := cf.Fd()
	v("CPUD:fd is %v", fd)

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
	v("CPUD: mount 127.0.0.1 on /tmp/cpu 9p %#x %s", flags, opts)
	if err := unix.Mount("127.0.0.1", "/tmp/cpu", "9p", flags, opts); err != nil {
		return nil, fmt.Errorf("9p mount %v", err)
	}
	v("CPUD: mount done")

	// In some cases if you set LD_LIBRARY_PATH it is ignored.
	// This is disappointing to say the least. We just bind a few things into /
	// bind *may* hide local resources but for now it's the least worst option.
	var warning error
	for _, n := range s.binds {
		t := filepath.Join("/tmp/cpu", n.Remote)
		v("CPUD: mount %v over %v", t, n.Local)
		if err := unix.Mount(t, n.Local, "", syscall.MS_BIND, ""); err != nil {
			s.fail = true
			warning = multierror.Append(fmt.Errorf("CPUD:Warning: mounting %v on %v failed: %v", t, n, err))
		} else {
			v("CPUD:Mounted %v on %v", t, n)
		}

	}
	return warning, nil
}

func osMounts() error {
	var errors error
	// Further, bind / onto /tmp/local so a non-hacked-on version may be visible.
	if err := unix.Mount("/", "/tmp/local", "", syscall.MS_BIND, ""); err != nil {
		errors = multierror.Append(fmt.Errorf("CPUD:Warning: binding / over /tmp/local did not work: %v, continuing anyway", err))
	}
	return errors
}

// runSetup performs kernel-specific operations for starting a Session.
func runSetup() error {
	if err := unix.Mount("cpu", "/tmp", "tmpfs", 0, ""); err != nil {
		return fmt.Errorf(`unix.Mount("cpu", "/tmp", "tmpfs", 0, ""); %v != nil`, err)
	}
	return nil
}
