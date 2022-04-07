// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package server is for building cpu servers, a.k.a. cpud.
//
// A cpud is an ssh server with a special handler.  On a normal ssh
// session, the main task is to set up to run a command attached to a
// stdin, stdout, and stderr.  This special handler allows port
// forwarding for 9p mounts, and, on Linux, sets up a bind mount for
// /tmp/cpu/local to /.
//
// cpu was original developed on Plan 9 systems. The assumption was
// that cpu is used in a single administrative domain. It is very
// important to ensure that the system you connect to is trusted,
// since when you connect to it, you are serving files to it, from
// your system, via 9p. Note that you can serve the remote system from
// a chroot, docker container, or other restricted environment -- even
// a virtual machine!  But the two use cases we consider safe are a
// remote system which is an IoT device which you control; or a remote
// VM or cloud node which you, similarly, control, i.e. is considered
// to be part of your own administrative domain.
//
// Hence, this implementation of cpu assumes a remote system is
// in our administrative domain, because it is an IoT, cloud, VM,
// or similar system. We do not recommend using CPU on systems
// which you do not completely trust. Use ssh and scp/rsync instead.
// Making cpu usable in untrusted environments is an unsolved problem.
// Note that this problem applies, to a lesser extent, to ssh; ssh port
// forwards are also a point of attack from a remote system.
//
// The basic flow of setting up a server is similar to most such servers:
// a call to a New(), preceded or followed by a call to net.Listen to get
// a socket, and a call to Serve with the listener. For a usage example,
// see TestDaemonConnect. The handler code is made a bit messy by the
// need to support PTYs.
//
// Each connection to the server results in the invocation of the
// commands send from the client. The most common command is something
// like: cpud -remote -bin cpud -port9p <9pportnumber> [command
// [arguments]].  If there is no command, servers typically run
// $SHELL; that is up to whatever binary cpud is asked to run for each
// session. The -bin cpud part of the args is no longer strictly
// necessary but is left in for older cpud binaries.xs
//
// This package also provides a Session type, created by a call to
// NewSession.  Sessions are very similar to exec.Command, providing
// access to Stdin, Stdout, Stderr and a Wait function, for example,
// although the only Session function that servers usually call is
// Run.
package server
