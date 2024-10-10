// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows && !plan9
// +build !windows,!plan9

package client

import (
	"os"
	"reflect"
	"runtime"

	"path/filepath"
	"testing"

	"github.com/hugelgupf/p9/p9"
	"golang.org/x/sys/unix"
)

func Test9pUnix(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "a")
	if err := os.WriteFile(f, []byte("hi"), 0666); err != nil {
		t.Fatalf(`os.WriteFile(%q, "hi", 0666): %v != nil`, f, err)
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

	c := &CPU9P{
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
	_, _, ga2, err := c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	if !reflect.DeepEqual(ga, ga2) {
		t.Fatalf("Second getattr after empty setattr differs: %v != %v", ga2, ga)
	}

	sam.Permissions = true
	if err := c.SetAttr(sam, sa); err != nil {
		t.Fatalf("Setattr(%v, %v): %v != nil", sam, sa, err)
	}
	_, _, ga2, err = c.GetAttr(m)
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
	_, _, ga2, err = c.GetAttr(m)
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
	_, _, ga2, err = c.GetAttr(m)
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
	_, _, ga2, err = c.GetAttr(m)
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

	_, _, ga2, err = c.GetAttr(m)
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
	_, _, ga2, err = c.GetAttr(m)
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
		t.Fatalf("Setattr(%v,%v): %v != nil", sam, sa, err)
	}

	// The second time, since we blew permissions to
	// zero, we ought to get an error.
	// It seems, on freebsd, there is no error.
	if err := c.SetAttr(sam, sa); runtime.GOOS != "freebsd" && err == nil {
		t.Fatalf("Setattr(%v,%v): nil != %v", sam, sa, unix.EPERM)
	}

	// But getattr should work.
	_, _, ga2, err = c.GetAttr(m)
	if err != nil {
		t.Fatalf("Getattr: %v != nil", err)
	}
	if ga2.Mode&p9.AllPermissions != sa.Permissions {
		t.Errorf("Permissions: %o != %o", ga2.Mode&p9.AllPermissions, sa.Permissions)
	}

}

func Test9pRemove(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "a")
	if err := os.WriteFile(f, []byte("hi"), 0666); err != nil {
		t.Fatalf(`os.WriteFile(%q, "hi", 0666): %v != nil`, f, err)
	}
	c := &CPU9P{
		path: f,
	}

	err := c.Remove()
	if err != nil {
		t.Errorf("Remove: %v != nil", err)
	}
	if _, err := os.Stat(f); err == nil {
		t.Errorf("After removing file: os.Stat(%q): got nil, want err", f)
	}
}

func Test9pUnlinkat(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "a")
	if err := os.WriteFile(f, []byte("hi"), 0666); err != nil {
		t.Fatalf(`os.WriteFile(%q, "hi", 0666): %v != nil`, f, err)
	}
	c := &CPU9P{
		path: d,
	}

	err := c.UnlinkAt("a", 0)
	if err != nil {
		t.Errorf("UnlinkAt: %v != nil", err)
	}
	if _, err := os.Stat(f); err == nil {
		t.Errorf("After UnlinkAt file: os.Stat(%q): got nil, want err", f)
	}
}

func Test9pRenameAt(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "a")
	if err := os.WriteFile(f, []byte("hi"), 0666); err != nil {
		t.Fatalf(`os.WriteFile(%q, "hi", 0666): %v != nil`, f, err)
	}
	c := &CPU9P{
		path: filepath.Join(d, "nd"),
	}
	if err := os.Mkdir(c.path, 0777); err != nil {
		t.Fatalf("Mkdir(%q, 0777): %v != nil", c.path, err)
	}
	oldPath := &CPU9P{
		path: d,
	}
	if err := oldPath.RenameAt("a", c, "z"); err != nil {
		t.Errorf("RenameAt(%q, %q, \"z\"): %v != nil", f, c.path, err)
	}
	newFile := filepath.Join(c.path, "z")
	if _, err := os.Stat(newFile); err != nil {
		t.Errorf("os.Stat(%q): %v != nil", newFile, err)
	}
}
