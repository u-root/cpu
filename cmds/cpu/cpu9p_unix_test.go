// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows && !plan9
// +build !windows,!plan9

package main

import (
	"io/ioutil"
	"reflect"

	"path/filepath"
	"testing"

	"github.com/hugelgupf/p9/p9"
	"golang.org/x/sys/unix"
)

func Test9pUnix(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "a")
	if err := ioutil.WriteFile(f, []byte("hi"), 0666); err != nil {
		t.Fatalf(`ioutil.WriteFile(%q, "hi", 0666): %v != nil`, f, err)
	}
	// First test: do nothing
	m := p9.AttrMask{
		Mode:        true,
		NLink:       true,
		UID:         true,
		GID:         true,
		RDev:        true,
		ATime:       true,
		MTime:       true,
		CTime:       true,
		INo:         true,
		Size:        true,
		Blocks:      true,
		BTime:       true,
		Gen:         true,
		DataVersion: true,
	}

	c := &cpu9p{
		path: f,
	}

	q, gm, ga, err := c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	t.Logf("First getattr returns (%v, %v, %v)", q, gm, ga)

	sam := p9.SetAttrMask{
		Permissions:        false,
		UID:                false,
		GID:                false,
		Size:               false,
		ATime:              false,
		MTime:              false,
		CTime:              false,
		ATimeNotSystemTime: false,
		MTimeNotSystemTime: false,
	}
	sa := p9.SetAttr{
		Permissions:      0777,
		UID:              ga.UID,
		GID:              ga.GID,
		Size:             ga.Size + 4,
		ATimeSeconds:     ga.ATimeSeconds + 22,
		ATimeNanoSeconds: ga.ATimeNanoSeconds + 33,
		MTimeSeconds:     ga.MTimeSeconds + 55,
		MTimeNanoSeconds: ga.MTimeNanoSeconds + 66,
	}

	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr with no change: %v != nil", err)
	}
	q, gm, ga2, err := c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	if !reflect.DeepEqual(ga, ga2) {
		t.Fatalf("Second getattr after empty setattr differs: %v != %v", ga2, ga)
	}

	sam.Permissions = true
	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr with mode: %v != nil", err)
	}
	q, gm, ga2, err = c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	if ga2.Mode&p9.AllPermissions != sa.Permissions {
		t.Errorf("Permissions: %o != %o", ga2.Mode&p9.AllPermissions, sa.Permissions)
	}

	sam.Size = true
	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr with size: %v != nil", err)
	}
	q, gm, ga2, err = c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	if ga2.Size != sa.Size {
		t.Errorf("Size: %o != %o", ga2.Size, sa.Size)
	}

	sam.UID = true
	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr with UID: %v != nil", err)
	}
	q, gm, ga2, err = c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	if ga2.UID != sa.UID {
		t.Errorf("UID: %o != %o", ga2.UID, sa.UID)
	}

	sam.GID = true
	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr with GID: %v != nil", err)
	}
	q, gm, ga2, err = c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	if ga2.GID != sa.GID {
		t.Errorf("GID: %o != %o", ga2.GID, sa.GID)
	}

	// Setting either of these should be ok,
	// and will set both to current.
	// Checking is hazardous, as file systems
	// don't always do what you might expect
	// around time. We settle for no error.
	sam.ATime = true
	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr with ATime: %v != nil", err)
	}
	sam.MTime = true
	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr with MTime: %v != nil", err)
	}
	sam.ATimeNotSystemTime, sam.MTime = true, false
	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr with ATime, sam.ATimeNotSystemTime: %v != nil", err)
	}

	q, gm, ga2, err = c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	// Check for seconds; nanoseconds is usually
	// meaningless.
	if ga2.ATimeSeconds != sa.ATimeSeconds {
		t.Errorf("Setting ATimeSeconds %d != %d", ga2.ATimeSeconds, sa.ATimeSeconds)
	}

	sam.MTimeNotSystemTime, sam.ATime, sam.MTime = true, false, true
	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr with MTime, sam.MTimeNotSystemTime: %v != nil", err)
	}
	q, gm, ga2, err = c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	// Check for seconds; nanoseconds is usually
	// meaningless.
	if ga2.MTimeSeconds != sa.MTimeSeconds {
		t.Errorf("Setting MTimeSeconds %d != %d", ga2.MTimeSeconds, sa.MTimeSeconds)
	}

	sam.Permissions = true
	sa.Permissions = 0
	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr with mode: %v != nil", err)
	}

	// The second time, since we blew permissions to
	// zero, we ought to get an error.
	if err := c.SetAttr(sam, sa); err == nil {
		t.Fatalf("Setattr with mode: nil != %v", unix.EPERM)
	}

	// But getattr should work.
	q, gm, ga2, err = c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	if ga2.Mode&p9.AllPermissions != sa.Permissions {
		t.Errorf("Permissions: %o != %o", ga2.Mode&p9.AllPermissions, sa.Permissions)
	}

}

func Test9pUnixTable(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "a")
	if err := ioutil.WriteFile(f, []byte("hi"), 0666); err != nil {
		t.Fatalf(`ioutil.WriteFile(%q, "hi", 0666): %v != nil`, f, err)
	}
	// First test: do nothing
	m := p9.AttrMask{
		Mode:        true,
		NLink:       true,
		UID:         true,
		GID:         true,
		RDev:        true,
		ATime:       true,
		MTime:       true,
		CTime:       true,
		INo:         true,
		Size:        true,
		Blocks:      true,
		BTime:       true,
		Gen:         true,
		DataVersion: true,
	}

	c := &cpu9p{
		path: f,
	}

	_, _, ga, err := c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}

	sam := p9.SetAttrMask{
		Permissions:        false,
		UID:                false,
		GID:                false,
		Size:               false,
		ATime:              false,
		MTime:              false,
		CTime:              false,
		ATimeNotSystemTime: false,
		MTimeNotSystemTime: false,
	}
	sa := p9.SetAttr{
		Permissions:      0777,
		UID:              ga.UID,
		GID:              ga.GID,
		Size:             ga.Size + 4,
		ATimeSeconds:     ga.ATimeSeconds + 22,
		ATimeNanoSeconds: ga.ATimeNanoSeconds + 33,
		MTimeSeconds:     ga.MTimeSeconds + 55,
		MTimeNanoSeconds: ga.MTimeNanoSeconds + 66,
	}

	var tests = []string{
		"Change nothing",
		"Setattr with Permissions",
		"Setattr with size",
		"Setattr with UID",
		"Setattr with GID",
		"Setattr with ATime to local system time",
		"Setattr with MTime to local system time",
		"Settattr with ATime to time in TSetattr",
		"Settattr with MTime to time in TSetattr",
	}
	for i, msg := range tests {
		switch i {
		case 0:
		case 1:
			sam.Permissions = true
		case 2:
			sam.Size = true
		case 3:
			sam.UID = true
		case 4:
			sam.GID = true
		case 5:
			sam.ATime = true
		case 6:
			sam.MTime = true
		case 7:
			sam.ATimeNotSystemTime, sam.MTime = true, false
		case 8:
			sam.MTimeNotSystemTime, sam.ATime, sam.MTime = true, false, true
		}

		t.Logf("Test %v", msg)
		if err := c.SetAttr(sam, sa); err != nil {
			t.Fatalf(msg+":%v != nil", err)
		}
		_, _, gagot, err := c.GetAttr(m)
		if err != nil {
			t.Fatalf("Getattr: %v != nil", err)
		}

		switch i {
		case 0:
			if !reflect.DeepEqual(gagot, ga) {
				t.Fatalf(msg+":%v != %v", gagot, ga)
			}

		case 1:
			if gagot.Mode&p9.AllPermissions != sa.Permissions {
				t.Fatalf("Permissions: %o != %o", gagot.Mode&p9.AllPermissions, sa.Permissions)
			}
		case 2:
			if gagot.Size != sa.Size {
				t.Fatalf("Size: %o != %o", gagot.Size, sa.Size)
			}

		case 3:
			if gagot.UID != sa.UID {
				t.Errorf("UID: %o != %o", gagot.UID, sa.UID)
			}

		case 4:
			if gagot.GID != sa.GID {
				t.Errorf("GID: %o != %o", gagot.GID, sa.GID)
			}

		case 5, 6:
			// for setting ATime to "whatever the kernel says it is",
			// there's no great test to see if it worked. Even testing
			// to see it's not what it was can have weird errors if the
			// file system you are on is NFS, or if while you are running
			// the test NTP or sysadmins move time on you. We have to
			// settle for "SetAttr got no error"
		case 7:
			if gagot.ATimeSeconds != sa.ATimeSeconds {
				t.Errorf("Setting ATimeSeconds %d != %d", gagot.ATimeSeconds, sa.ATimeSeconds)
			}
		case 8:
			if gagot.MTimeSeconds != sa.MTimeSeconds {
				t.Errorf("Setting MTimeSeconds %d != %d", gagot.MTimeSeconds, sa.MTimeSeconds)
			}

		}

	}

}
