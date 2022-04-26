// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"os"
	"os/exec"
	"syscall"
)

// cpud can run in one of three modes
// o init
// o daemon started by init
// o manager of one cpu session.
// It is *critical* that the session manager have a private
// name space, else every cpu session will interfere with every
// other session's mounts. What's the best way to ensure the manager
// gets a private name space, and ensure that no improper use
// of this package will result in NOT having a private name space?
// How do we make the logic failsafe?
//
// It turns out there is no harm in always privatizing the name space,
// no matter the mode.
// So in this init function, we do not parse flags (that breaks tests;
// flag.Parse() in init is a no-no), and then, no
// matter what, privatize the namespace, and mount a private /tmp/cpu if we
// are not pid1. As for pid1 tasks, they should be specified by the cpud
// itself, not this package. This code merely ensures correction operation
// of cpud no matter what mode it is invoked in.
func init() {
	// placeholder. It's not clear we ever want to do this. We used to create
	// a root file system here, but that should be up to the server. The files
	// might magically exist, b/c of initrd; or be automagically mounted via
	// some other mechanism.
	if os.Getpid() == 1 {
		v("PID 1")
	}
}

func command(n string, args ...string) *exec.Cmd {
	cmd := exec.Command(n, args...)
	// N.B.: in the go runtime, after not long ago, CLONE_NEWNS in the Unshareflags
	// also does two things: an unshare, and a remount of / to unshare mounts.
	// see d8ed449d8eae5b39ffe227ef7f56785e978dd5e2 in the go tree for a discussion.
	// This meant we could remove ALL calls of unshare and mount from cpud.
	// Fun fact: I wrote that fix years ago, and then forgot to remove
	// the support code from cpu. Oops.
	cmd.SysProcAttr = &syscall.SysProcAttr{Unshareflags: syscall.CLONE_NEWNS}
	return cmd
}
