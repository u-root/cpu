// Copyright 2023 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"testing"

	"github.com/hugelgupf/p9/p9"
)

func TestUnion9PWalkReadlink(t *testing.T) {
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	// See if anything is there.
	c, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", c)

	u, err := NewUnion9P([]UnionMount{{walk: []string{}, mount: c}})

	if err != nil {
		t.Fatalf("NewUnion9P: got %v, want nil", err)
	}

	attach, err := u.Attach()
	if err != nil {
		t.Fatalf("union attach: want nil, got %v", err)
	}

	_, root, err := attach.Walk([]string{"b", "e", "hosts"})
	if err != nil {
		t.Fatalf("walking '': want nil, got %v", err)
	}
	t.Logf("root %v", root)
	r, err := root.Readlink()
	if err != nil {
		t.Fatalf("readlink: want nil, got %v", err)
	}
	t.Logf("readlink: %v, %v", r, err)
	if r != "/etc/hosts" {
		t.Fatalf("readlink: want %q, got %q", "/etc/hosts", r)
	}
}

// Test with just one Mount
// don't bother with the zero case, NewUnion9P does not allow it.
func TestUnion9POne(t *testing.T) {
	v = t.Logf
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	// See if anything is there.
	c, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", c)

	u, err := NewUnion9P([]UnionMount{{walk: []string{}, mount: c}})

	if err != nil {
		t.Fatalf("NewUnion9P: got %v, want nil", err)
	}

	attach, err := u.Attach()
	if err != nil {
		t.Fatalf("union attach: want nil, got %v", err)
	}

	_, root, err := attach.Walk([]string{})
	if err != nil {
		t.Fatalf("walking '': want nil, got %v", err)
	}

	d, err := attach.Readdir(0, 1024)
	if err != nil {
		t.Fatalf("union readdir: want nil, got %v", err)
	}
	t.Logf("dirents %v", d)

	if q, f, err := root.Walk([]string{"barf"}); err == nil {
		t.Fatalf("walking 'barf': want err, got (%v,%v,%v)", q, f, err)
	}

	if _, _, _, _, err := root.WalkGetAttr([]string{"barf"}); err == nil {
		t.Fatalf("walkgetattr to 'barf': want err, got nil")
	}

	_, b, err := root.Walk([]string{"b"})
	if err != nil {
		t.Fatalf("walking 'b': want nil, got %v", err)
	}
	t.Logf("b %v", b)

	if _, _, _, _, err := root.WalkGetAttr([]string{"b"}); err != nil {
		t.Fatalf("walkgetattr to 'b': want nil, got %v", err)
	}

	q, c, err := root.Walk([]string{"b", "c"})
	if err != nil {
		t.Fatalf("walking a/b: want nil, got %v", err)
	}
	if len(q) != 2 {
		t.Fatalf("walking a/b: want 2 qids, got (%v,%v)", q, err)
	}
	if c == nil {
		t.Fatalf("walking a/b: want non-nil file, got nil")
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

	if _, _, err := c.Walk([]string{"d"}); err != nil {
		t.Fatalf("walking d from b/c: want nil, got %v", err)
	}

	_, hi, err := c.Walk([]string{"hi"})
	if err != nil {
		t.Fatalf("walking hi from b/c: want nil, got %v", err)
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

	dirs, err := c.Readdir(0, 64*1024)
	if err != nil {
		t.Fatalf("readdir on root: want nil, got %v", err)
	}
	if len(dirs) != 4 {
		t.Fatalf("readdir on root: want %d entries, got %d", 4, len(dirs))
	}
	t.Logf("readdir c/ %v", dirs)

	dirs, err = root.Readdir(0, 64*1024)
	if err != nil {
		t.Fatalf("readdir on root: want nil, got %v", err)
	}
	if len(dirs) != 3 {
		t.Fatalf("readdir on root: want %d entries, got %d", 3, len(dirs))
	}
	t.Logf("readdir / %v", dirs)

}

func TestBadOperators(t *testing.T) {
	u, err := NewUnion9P([]UnionMount{})

	if err != nil {
		t.Fatalf("NewUnion9P: got %v, want nil", err)
	}

	attach, err := u.Attach()
	if err != nil {
		t.Fatalf("union attach: want nil, got %v", err)
	}

	data := make([]byte, 30)
	// test bad operators
	if n, err := attach.WriteAt(data[:], 0); err == nil || n != -1 {
		t.Fatalf("WriteAt: got (%d, nil), want (-1, err)", n)
	}

	if _, err := attach.Symlink("", "", p9.UID(0), p9.GID(0)); err == nil {
		t.Fatalf("symlink: got nil, want err")
	}

	if err := attach.Link(attach, ""); err == nil {
		t.Fatalf("link: got nil, want err")
	}

	if _, err := attach.Readlink(); err == nil {
		t.Fatalf("remove: got nil, want err")
	}

	if _, err := attach.Mknod("", p9.FileMode(0), 0, 0, p9.UID(0), p9.GID(0)); err == nil {
		t.Fatalf("Mknod: got nil, want err")
	}

	if err := attach.Rename(attach, "k"); err == nil {
		t.Fatalf("rename: got nil, want err")
	}

	if err := attach.RenameAt("", attach, ""); err == nil {
		t.Fatalf("renameat: got nil, want err")
	}

	if err := attach.UnlinkAt("hi", 0); err == nil {
		t.Fatalf("unlinkat: got nil, want err")
	}

	if _, err := attach.StatFS(); err == nil {
		t.Fatalf("statfs: got nil, want err")
	}
}

func TestUnion9PTwoCPIO(t *testing.T) {
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	// See if anything is there.
	a, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", a)

	fs, err = NewCPIO9P("data/b.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	// See if anything is there.
	b, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", b)

	u, err := NewUnion9P([]UnionMount{
		{walk: []string{"home"}, mount: b},
		{walk: []string{}, mount: a},
	})

	if err != nil {
		t.Fatalf("NewUnion9P: got %v, want nil", err)
	}

	attach, err := u.Attach()
	if err != nil {
		t.Fatalf("union attach: want nil, got %v", err)
	}
	d, err := attach.Readdir(0, 1024)
	if err != nil {
		t.Fatalf("union readdir: want nil, got %v", err)
	}
	t.Logf("dirents %v", d)
	if len(d) != 5 {
		t.Fatalf("union readdir: got %d entries, want 5", len(d))
	}
	for i, n := range []string{".", "home", "root", "hosts", "b"} {
		if d[i].Name != n {
			t.Fatalf("union readdir: d[%d].Name want %q, got %q", i, d[i].Name, n)
		}
	}

}

func TestUnion9PTwoCPIOSymlinkAtRoot(t *testing.T) {
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	// See if anything is there.
	a, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", a)

	fs, err = NewCPIO9P("data/b.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	// See if anything is there.
	b, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", b)

	u, err := NewUnion9P([]UnionMount{
		{walk: []string{"home"}, mount: b},
		{walk: []string{}, mount: a},
	})

	if err != nil {
		t.Fatalf("NewUnion9P: got %v, want nil", err)
	}

	attach, err := u.Attach()
	if err != nil {
		t.Fatalf("union attach: want nil, got %v", err)
	}
	_, root, err := attach.Walk([]string{"hosts"})
	if err != nil {
		t.Fatalf("walking '': want nil, got %v", err)
	}
	t.Logf("root %v", root)
	r, err := root.Readlink()
	if err != nil {
		t.Fatalf("readlink: want nil, got %v", err)
	}
	t.Logf("readlink: %v, %v", r, err)
	if r != "etc/hosts" {
		t.Fatalf("readlink: want %q, got %q", "/etc/hosts", r)
	}
}

func TestUnion9PTwoCPIOEmptyHome(t *testing.T) {
	v = t.Logf
	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	// See if anything is there.
	a, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", a)

	fs, err = NewCPIO9P("data/b.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	// See if anything is there.
	b, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", b)

	u, err := NewUnion9P([]UnionMount{
		{walk: []string{"home"}, mount: b},
		{walk: []string{"root"}, mount: b},
		{walk: []string{}, mount: a},
	})

	if err != nil {
		t.Fatalf("NewUnion9P: got %v, want nil", err)
	}

	attach, err := u.Attach()
	if err != nil {
		t.Fatalf("union attach: want nil, got %v", err)
	}
	_, root, err := attach.Walk([]string{"root"})
	if err != nil {
		t.Fatalf("walking '': want nil, got %v", err)
	}
	q, f, err := root.Open(0)
	if err != nil {
		t.Fatalf("opening root: want nil, got %v", err)
	}
	t.Logf("open root: q %v, f %v", q, f)
	d, err := root.Readdir(0, 1024)
	if err != nil {
		t.Fatalf("root readdir: want nil, got %v", err)
	}
	t.Logf("root readdir: %v", d)
}
