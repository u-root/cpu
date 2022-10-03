// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This is init code for the case that cpu finds itself as pid 1.
// This is duplicative of the real init, but we're implementing it
// as a duplicate so we can get some idea of:
// what an init package should have
// what an init interface should have
// So we take a bit of duplication now to better understand these
// things. We also assume for now this is a busybox environment.
// It is unusual (I guess?) for cpu to be an init in anything else.
// So far, the case for an init pkg is not as strong as I thought
// it might be.

//go:build linux
// +build linux

package main

import (
	"log"
	"runtime"
	"syscall"
	"time"

	"github.com/u-root/u-root/pkg/libinit"
)

func cpuSetup() error {
	// The process reaper runs from here, and needs to run
	// as PID 1.
	runtime.LockOSThread()
	log.Printf(`

  ####   #####   #    #   ##
 #    #  #    #  #    #   ##
 #       #    #  #    #   ##
 #       #####   #    #   ##
 #    #  #       #    #
  ####   #        ####    ##
`)
	libinit.SetEnv()
	libinit.CreateRootfs()
	libinit.NetInit()
	// Wait for orphans, forever.
	// Since there is no way of knowning when we are
	// done for good, our work here is never done.
	// A complication is that for long periods of time, there
	// may be no orphans.In that case, sleep for one second,
	// and try again. This background load is hardly enough
	// to matter. And, in general, it will happen by definition
	// when there is nothing to wait for, i.e. there is nothing
	// on the node to be upset about.
	// Were this ever to be a concern, an option is to kick off
	// a process that will never exit, such that wait4 will always
	// block and always return when any child process exits.
	go func() {
		var numReaped int
		for {
			var (
				s syscall.WaitStatus
				r syscall.Rusage
			)
			p, err := syscall.Wait4(-1, &s, 0, &r)
			// Once per second, Wait 4 returns if there's nothing
			// else to do.
			if err != nil && err.Error() == "no child processes" {
				continue
			}
			verbose("orphan reaper: returns with %v", p)
			if p == -1 {
				verbose("Nothing to wait for, %d wait for so far", numReaped)
				time.Sleep(time.Second)
			}
			if err != nil {
				log.Printf("CPUD: a process exited with %v, status %v, rusage %v, err %v", p, s, r, err)
			}
			numReaped++
		}
	}()

	runtime.UnlockOSThread()
	return nil
}
