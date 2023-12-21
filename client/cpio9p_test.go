// Copyright 2023 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hugelgupf/p9/p9"
)

func TestCPIO9PBadFile(t *testing.T) {
	d := t.TempDir()
	bogus := filepath.Join(d, "bogus")
	if _, err := NewCPIO9P(bogus); err == nil {
		t.Fatalf("Opening non-existent file: got nil, want err")
	}
	if err := os.WriteFile(bogus, []byte("bogus"), 0666); err != nil {
		t.Fatal(err)
	}

	v = t.Logf
	if _, err := NewCPIO9P(bogus); err == nil {
		t.Fatalf("Opening bad file: got nil, want err")
	}
}

func TestCPIOBadOperators(t *testing.T) {
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}
	// See if anything is there.
	attach, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", attach)

	_, root, err := attach.Walk([]string{})
	if err != nil {
		t.Fatalf("walking '': want nil, got %v", err)
	}

	walkto := []string{"b", "c", "hi"}
	_, hi, err := root.Walk(walkto)
	if err != nil {
		t.Fatalf("walking %v from /: want nil, got %v", walkto, err)
	}
	var data [2]byte
	off := int64(1)
	if n, err := hi.WriteAt(data[:], off); err == nil || n != -1 {
		t.Fatalf("WriteAt: got (%d, nil), want (-1, err)", n)
	}

	if _, err := hi.Symlink("", "", p9.UID(0), p9.GID(0)); err == nil {
		t.Fatalf("symlink: got nil, want err")
	}

	if err := hi.Link(root, ""); err == nil {
		t.Fatalf("link: got nil, want err")
	}

	var m p9.FileMode
	if _, err := hi.Mknod("", m, 0, 0, p9.UID(0), p9.GID(0)); err == nil {
		t.Fatalf("Mknod: got nil, want err")
	}

	if err := hi.Rename(root, "k"); err == nil {
		t.Fatalf("rename: got nil, want err")
	}

	if err := hi.RenameAt("", root, ""); err == nil {
		t.Fatalf("renameat: got nil, want err")
	}

	if err := hi.UnlinkAt("hi", 0); err == nil {
		t.Fatalf("unlinkat: got nil, want err")
	}

	if _, err := root.StatFS(); err == nil {
		t.Fatalf("statfs: got nil, want err")
	}

}

func TestCPIO9PAttach(t *testing.T) {
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}
	// See if anything is there.
	attach, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", attach)
}

func TestCPIO9PAttachWalk(t *testing.T) {
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}
	// See if anything is there.
	attach, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", attach)

	_, root, err := attach.Walk([]string{})
	if err != nil {
		t.Fatalf("walking '': want nil, got %v", err)
	}

	if q, f, err := root.Walk([]string{"barf"}); err == nil {
		t.Fatalf("walking 'barf': want err, got (%v,%v,%v)", q, f, err)
	}

	_, b, err := root.Walk([]string{"b"})
	if err != nil {
		t.Fatalf("walking 'b': want nil, got %v", err)
	}
	t.Logf("b %v", b)

	walkto := []string{"b", "c"}
	q, c, err := root.Walk(walkto)
	if err != nil {
		t.Fatalf("walking %v: want nil, got %v", walkto, err)
	}
	if len(q) != 2 {
		t.Fatalf("walking %v: want 2 qids, got (%v,%v)", walkto, q, err)
	}
	if c == nil {
		t.Fatalf("walking %v: want non-nil file, got nil", walkto)
	}
}

func TestCPIO9PAttachWalkCreate(t *testing.T) {
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}
	// See if anything is there.
	attach, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", attach)
	_, root, err := attach.Walk([]string{})
	if err != nil {
		t.Fatalf("walking '': want nil, got %v", err)
	}

	var (
		of p9.OpenFlags
		m  p9.FileMode
	)
	if _, _, _, err := root.Create("", of, m, p9.UID(0), p9.GID(0)); err == nil {
		t.Fatalf("create in root: got nil, want err")
	}

	if _, err := root.Mkdir("", m, p9.UID(0), p9.GID(0)); err == nil {
		t.Fatalf("mkdir in root: got hil, want err")
	}

	walkto := []string{"b", "c"}
	_, c, err := root.Walk(walkto)
	if err != nil {
		t.Fatalf("walking %v: want nil, got %v", walkto, err)
	}

	if _, _, err := c.Walk([]string{"d"}); err != nil {
		t.Fatalf("walking d from %v: want nil, got %v", walkto, err)
	}

	_, hi, err := c.Walk([]string{"hi"})
	if err != nil {
		t.Fatalf("walking hi from %v: want nil, got %v", walkto, err)
	}
	var data [2]byte
	off := int64(1)
	if _, err := hi.ReadAt(data[:], off); err != nil {
		t.Fatalf("Reading hi: want nil, got %v", err)
	}
	if n, _ := hi.ReadAt(data[:], off); n != 2 {
		t.Fatalf("Reading hi: want 2 bytes, got %v", n)
	}
	if string(data[:]) != "i\n" {
		t.Fatalf("Reading hi: want %q, got %q", "i\n", string(data[:]))
	}

	var (
		mask p9.SetAttrMask
		attr p9.SetAttr
	)

	if err := hi.SetAttr(mask, attr); err == nil {
		t.Fatalf("setattr: got nil, want err")
	}

	var am p9.AttrMask
	if _, _, _, err = hi.GetAttr(am); err != nil {
		t.Fatalf("getattr: want nil, got %v", err)
	}

}

func TestCPIORootReaddir(t *testing.T) {
	v = t.Logf
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}
	// See if anything is there.
	attach, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", attach)
	_, root, err := attach.Walk([]string{})
	if err != nil {
		t.Fatalf("walking '': want nil, got %v", err)
	}

	walkto := []string{"b", "c"}
	_, c, err := root.Walk(walkto)
	if err != nil {
		t.Fatalf("walking %v: want nil, got %v", walkto, err)
	}

	dirs, err := c.Readdir(0, 64*1024)
	if err != nil {
		t.Fatalf("readdir on root: want nil, got %v", err)
	}
	if len(dirs) != 4 {
		t.Fatalf("readdir on root: want %d entries, got %d", 4, len(dirs))
	}
	t.Logf("readdir c/ %v", dirs)

	// Make sure the names are simple
	for _, dir := range dirs {
		d, b := filepath.Split(dir.Name)
		if len(d) != 0 {
			t.Fatalf("readdir on %q: want %q, got %q", "b/c", b, dir.Name)
		}
	}

	dirs, err = root.Readdir(0, 64*1024)
	if err != nil {
		t.Fatalf("readdir on root: want nil, got %v", err)
	}
	if len(dirs) != 3 {
		t.Fatalf("readdir on root: want %d entries, got %d", 3, len(dirs))
	}
	t.Logf("readdir / %v", dirs)
}

func TestCPIOReadLink(t *testing.T) {
	v = t.Logf
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	t.Logf("[2] is %v #%x", fs.recs[2], fs.recs[2])
	t.Logf("[3] is %v #%x", fs.recs[3], fs.recs[3])
	t.Logf("mode %#x", fs.recs[3].Mode)
	// See if anything is there.
	root, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", root)

	v = t.Logf
	q, hosts, err := root.Walk([]string{"b", "e", "hosts"})
	if err != nil {
		t.Fatalf("walking 'b/e/hosts': want nil, got %v", err)
	}
	if len(q) != 3 {
		t.Fatalf("walking 'b/e/hosts': want 3 QIDs, got %d", len(q))
	}
	// The last QID type MUST be TypeSymlink
	if q[2].Type != p9.TypeSymlink {
		t.Fatalf("walking 'b/e/hosts': want QIDS[2].Type to be %#x, got %#x", p9.TypeSymlink, q[2].Type)
	}

	h, err := hosts.Readlink()
	if err != nil {
		t.Fatalf("Readlink 'b/e/hosts': want nil, got %v", err)
	}
	if h != "/etc/hosts" {
		t.Fatalf("Readlink 'b/e/hosts': want '/etc/hosts', got %v", h)
	}

}
