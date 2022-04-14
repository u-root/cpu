// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !plan9
// +build !plan9

package mount

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestMkdir(t *testing.T) {
	// Call Mount with a one-line fstab with a bogus mount point. It should do nothing but return
	// the mkdir error
	var fstab = "a /dev/zero/x none defaults 0 0"
	if err := Mount(fstab); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("mount(%v): %v != %v", fstab, err, syscall.ENOTDIR)
	}
}

func TestBadFSType(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skipf("Skipping test: not uid 0")
	}
	var fstab = "/dev/zero /tmp thisfilesystemdoesnotexist defaults 0 0"
	if err := Mount(fstab); err == nil {
		t.Fatalf("mount(%v): %v != an error, e.g. %v", fstab, err, syscall.ENXIO)
	}

}

func TestBadDev(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skipf("Skipping test: not uid 0")
	}
	var fstab = "/dev/thisdevicedoesnotexist /tmp ext4 defaults 0 0"
	if err := Mount(fstab); err == nil {
		t.Fatalf("mount(%v): %v != an error, e.g. %v", fstab, err, syscall.ENODEV)
	}

}

func TestParse(t *testing.T) {
	for _, tt := range []struct {
		in  string
		out string
	}{
		{in: "defaults", out: "rw,suid,dev,exec,auto,nouser,async"},
		{in: "ro,defaults", out: "ro,rw,suid,dev,exec,auto,nouser,async"},
		{in: "ro,nodev,relatime", out: "ro,nodev,relatime"},
		{in: "ro,nosuid,nodev,noexec,size=4096k,nr_inodes=1024,mode=755", out: "ro,nosuid,nodev,noexec,size=4096k,nr_inodes=1024,mode=755"},
		{in: "rw", out: "rw"},
		{in: "rw,nosuid,nodev", out: "rw,nosuid,nodev"},
		{in: "rw,nosuid,nodev,noexec,relatime", out: "rw,nosuid,nodev,noexec,relatime"},
		{in: "rw,nosuid,nodev,noexec,relatime,blkio", out: "rw,nosuid,nodev,noexec,relatime,blkio"},
		{in: "rw,nosuid,nodev,noexec,relatime,cpu,cpuacct", out: "rw,nosuid,nodev,noexec,relatime,cpu,cpuacct"},
		{in: "rw,nosuid,nodev,noexec,relatime,cpuset", out: "rw,nosuid,nodev,noexec,relatime,cpuset"},
		{in: "rw,nosuid,nodev,noexec,relatime,devices", out: "rw,nosuid,nodev,noexec,relatime,devices"},
		{in: "rw,nosuid,nodev,noexec,relatime,freezer", out: "rw,nosuid,nodev,noexec,relatime,freezer"},
		{in: "rw,nosuid,nodev,noexec,relatime,hugetlb", out: "rw,nosuid,nodev,noexec,relatime,hugetlb"},
		{in: "rw,nosuid,nodev,noexec,relatime,memory", out: "rw,nosuid,nodev,noexec,relatime,memory"},
		{in: "rw,nosuid,nodev,noexec,relatime,mode=700", out: "rw,nosuid,nodev,noexec,relatime,mode=700"},
		{in: "rw,nosuid,nodev,noexec,relatime,net_cls,net_prio", out: "rw,nosuid,nodev,noexec,relatime,net_cls,net_prio"},
		{in: "rw,nosuid,nodev,noexec,relatime,nsdelegate", out: "rw,nosuid,nodev,noexec,relatime,nsdelegate"},
		{in: "rw,nosuid,nodev,noexec,relatime,perf_event", out: "rw,nosuid,nodev,noexec,relatime,perf_event"},
		{in: "rw,nosuid,nodev,noexec,relatime,pids", out: "rw,nosuid,nodev,noexec,relatime,pids"},
		{in: "rw,nosuid,nodev,noexec,relatime,rdma", out: "rw,nosuid,nodev,noexec,relatime,rdma"},
		{in: "rw,nosuid,nodev,noexec,relatime,size=3902136k,mode=755", out: "rw,nosuid,nodev,noexec,relatime,size=3902136k,mode=755"},
		{in: "rw,nosuid,nodev,noexec,relatime,size=5120k", out: "rw,nosuid,nodev,noexec,relatime,size=5120k"},
		{in: "rw,nosuid,nodev,noexec,relatime,xattr,name=systemd", out: "rw,nosuid,nodev,noexec,relatime,xattr,name=systemd"},
		{in: "rw,nosuid,nodev,relatime,size=3902132k,nr_inodes=975533,mode=700,uid=1000,gid=1000", out: "rw,nosuid,nodev,relatime,size=3902132k,nr_inodes=975533,mode=700,uid=1000,gid=1000"},
		{in: "rw,nosuid,noexec,relatime,gid=5,mode=620,ptmxmode=000", out: "rw,nosuid,noexec,relatime,gid=5,mode=620,ptmxmode=000"},
		{in: "rw,nosuid,noexec,relatime,size=19429784k,nr_inodes=4857446,mode=755", out: "rw,nosuid,noexec,relatime,size=19429784k,nr_inodes=4857446,mode=755"},
		{in: "rw,relatime", out: "rw,relatime"},
		{in: "rw,relatime,fd=28,pgrp=1,timeout=0,minproto=5,maxproto=5,direct,pipe_ino=24647", out: "rw,relatime,fd=28,pgrp=1,timeout=0,minproto=5,maxproto=5,direct,pipe_ino=24647"},
		{in: "rw,relatime,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro", out: "rw,relatime,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro"},
		{in: "rw,relatime,pagesize=2M", out: "rw,relatime,pagesize=2M"},
		{in: "sw", out: "sw"},
	} {
		out := parse(tt.in)
		if out != tt.out {
			t.Errorf("Parsing %s: got %s, want %s", tt.in, out, tt.out)
		}

	}

}
