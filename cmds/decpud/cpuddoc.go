// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// decpud -- decentralized cpu daemon
//
// Synopsis:
//
//	decpud [OPTIONS]
//
// Advisory:
//
// Options:
//
//		-d    enable debug prints
//		-dbg9p
//		      show 9p io
//		-hostkey string
//		      host key file
//		-key string
//		      key file (default "$HOME/.ssh/cpu_rsa")
//		-network string
//		      network to use (default "tcp")
//		-p string
//		      port to use (default "17010")
//		-port9p string
//		      port9p # on remote machine for 9p mount
//		-remote
//		      Indicates we are the remote side of the cpu session
//		-srv string
//		      what server to run (default none; use internal)
//		- dsEnabled (default true)
//			  advertise service using DNSSD
//		- dsInstance
//			  DNSSD instance name (default $HOSTNAME-cpud)
//		- dsDomain
//			  DNSSD domain (default local)
//		- dsService
//			  DNSSD Service Type (default "_ncpu._tcp")
//	 - dsInterface
//			  DNSSD Interface
//		- dsTxt
//			  Additional string key-value pair meta-data for host
//
// decpud is the daemon side of a decpu session.
// In the original Plan 9 implementation, cpu was a command that contained
// both server and client sides of a cpu session. The server side was started
// by their equivalent of inetd, with a -r switch.
//
// Our original implementation followed this model, but for several reasons,
// has diverged. cpu and cpud are separate programs.
//
// This cpud can run in one of 3 modes:
//
// as a single process for one cpu session, i.e. the old plan 9 cpu -r model
//
// as a deamon, listening on port 17010 or other well known port,
// forking single cpu sessions, as does sshd.
//
// as an init process, i.e. PID 1, which sets up local file systems,
// starts a process reaper, and starts the cpud in daemon mode, more a less
// a single-purpose initd.
//
// There are now several years of usage of cpud in the field, and with the
// package rewrite of the code, there is an opportunity to make running
// cpud interactively convenient, while making its invocation as init
// similarly convenient.
//
// Because we do not have the option of forking and handling the
// session in the child, the setup is necessarily a bit more exposed: code must
// indicate the role of the spawned process, via environment variable or flag.
// This unfortunately also makes mistaken invocations possible, and it's not
// clear how we might prevent them. We can make mistaken invocations a bit
// harder, however, but that's about the limit.
//
// The discussions on how to indicate the role an individual instance
// of this program is taking on have been extensive, to say the least.
// An important consideration is making it convenient for a user to start
// a cpud outside the context of an init and daemon.
//
// The question we must answer: what is the expected behavior of cpud
// when it starts? What switches should we require for maximal
// user convenience? What are the common use models?
//
// There is an interesting contradiction here.
// The single most used command-line *invocation* of cpud is as a daemon;
// the second most common is as PID1 (i.e. init); and the least common,
// and at this point never used, is as a single command started by a user
// to run a session (i.e. single command such as bash or ls).
// We may want to allow single command line usage in the future, but such
// use is complicated by the fact that mount on Linux is a privileged
// operation (not on Plan9 however), so cpud has to run as root for at
// least long enough to build the namespace. For now, we're going to ignore that
// case.
//
// Hence: there is one init process, or one daemon, but there are many sessions.
// But we know from usage that in almost all cases, people invoke cpud
// as a daemon almost exclusively, or run it as init. So we must make the
// usage as a daemon or init very easy, and we will even ignore the manual
// session invocation for now, as it turns out to be very hard on Unix.
//
// Ideally, we could make these separate binaries, but space considerations
// in flash preclude that.
//
// One last wrinkle: some setup needs to occur while cpud is just one process,
// in particular the unshare system call. flag.Parse() can not be called in init.
// This one fact makes use of an environment variable the most sensible thing
// to do.
// So:
// If cpud starts, and it is pid1, it will run as init and daemon.
// If cpud starts, and it is not pid1, but CPUD_SESSION is *not* set, then
// it will run as a daemon.
// If cpud starts, and CPUD_SESSION is set in the environment, it will
// run as it would for one session.
//
// These rules make cpud easy to run as init, and as a daemon from the command
// line, while also simplifying the init code.
package main
