// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package session is for managing cpu sessions, i.e. the process
// started by cpud.
//
// New(port9p, cmd string, args ...string) creates a new Session.  The
// port9p argument, if is not empty, specifies the 9p port to use. The
// cmd parameter specifies a command and arguments, a la exec.Command.
// Sessions are very similar to exec.Command, providing access to
// Stdin, Stdout, Stderr.  For Stdin, Run determines if a pty is
// needed and will set one up.
//
// Run will also set up a process namespace via 9p and other mounts,
// if needed.  If CPU_NAMESPACE is non-empty, it defines the bind
// mounts from /tmp/cpu.  See the cpu command documentation for more
// information on how CPU_NAMESPACE is set.  If CPU_FSTAB is set, it
// is assumed to be a string in fstab(5) format and Run will mount the
// specified file systems. CPU_FSTAB is most often used for virtiofs
// mounts from virtual machines.
//
// For the moment, servers only call Run(), which
// does all namespace, tty, and process startup. Run returns when the
// process it directly started returns. It does not wait for children.
package session
