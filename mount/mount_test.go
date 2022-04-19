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

	"golang.org/x/sys/unix"
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
	for i, tt := range []struct {
		in   string
		flag uintptr
		opt  string
	}{
		{in: "defaults", flag: 0, opt: ""},
		{in: "ro,defaults", flag: unix.MS_RDONLY, opt: ""},
		{in: "ro,nodev,relatime", flag: unix.MS_RELATIME | unix.MS_RDONLY | unix.MS_NODEV, opt: ""},
		{in: "ro,nosuid,nodev,noexec,size=4096k,nr_inodes=1024,mode=755", flag: unix.MS_RDONLY | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "size=4096k,nr_inodes=1024,mode=755"},
		{in: "rw", flag: 0, opt: ""},
		{in: "rw,nosuid,nodev", flag: unix.MS_NOSUID | unix.MS_NODEV, opt: ""},
		{in: "rw,nosuid,nodev,noexec,relatime", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: ""},
		{in: "rw,nosuid,nodev,noexec,relatime", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: ""},

		{in: "rw,nosuid,nodev,noexec,relatime,cpu,cpuacct", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "cpu,cpuacct"},
		{in: "rw,nosuid,nodev,noexec,relatime,cpuset", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "cpuset"},
		{in: "rw,nosuid,nodev,noexec,relatime", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: ""},
		{in: "rw,nosuid,nodev,noexec,relatime", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: ""},
		{in: "rw,nosuid,nodev,noexec,relatime,cpu,cpuacct", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "cpu,cpuacct"},
		{in: "rw,nosuid,nodev,noexec,relatime,cpuset", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "cpuset"},
		{in: "rw,nosuid,nodev,noexec,relatime,devices", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "devices"},
		{in: "rw,nosuid,nodev,noexec,relatime,freezer", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "freezer"},
		{in: "rw,nosuid,nodev,noexec,relatime,hugetlb", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "hugetlb"},
		{in: "rw,nosuid,nodev,noexec,relatime,memory", flag: unix.MS_RELATIME | unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "memory"},
		{in: "rw,nosuid,nodev,noexec,relatime,mode=700", flag: unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "mode=700"},
		{in: "rw,nosuid,nodev,noexec,relatime,net_cls,net_prio", flag: unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "net_cls,net_prio"},
		{in: "rw,nosuid,nodev,noexec,relatime,nsdelegate", flag: unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "nsdelegate"},
		{in: "rw,nosuid,nodev,noexec,relatime,perf_event", flag: unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "perf_event"},
		{in: "rw,nosuid,nodev,noexec,relatime,pids", flag: unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "pids"},
		{in: "rw,nosuid,nodev,noexec,relatime,rdma", flag: unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "rdma"},
		{in: "rw,nosuid,nodev,noexec,relatime,size=3902136k,mode=755", flag: unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "size=3902136k,mode=755"},
		{in: "rw,nosuid,nodev,noexec,relatime,size=5120k", flag: unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "size=5120k"},
		{in: "rw,nosuid,nodev,noexec,relatime,xattr,name=systemd", flag: unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, opt: "xattr,name=systemd"},
		{in: "rw,nosuid,nodev,relatime,size=3902132k,nr_inodes=975533,mode=700,uid=1000,gid=1000", flag: 0x200006, opt: "size=3902132k,nr_inodes=975533,mode=700,uid=1000,gid=1000"},
		{in: "rw,nosuid,noexec,relatime,gid=5,mode=620,ptmxmode=000", flag: unix.MS_NOSUID | unix.MS_NOEXEC, opt: "gid=5,mode=620,ptmxmode=000"},
		{in: "rw,nosuid,noexec,relatime,size=19429784k,nr_inodes=4857446,mode=755", flag: 0x20000a, opt: "size=19429784k,nr_inodes=4857446,mode=755"},
		{in: "rw,relatime", flag: 0x200000, opt: ""},
		{in: "rw,relatime,fd=28,pgrp=1,timeout=0,minproto=5,maxproto=5,direct,pipe_ino=24647", flag: 0x200000, opt: "fd=28,pgrp=1,timeout=0,minproto=5,maxproto=5,direct,pipe_ino=24647"},
		{in: "rw,relatime,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro", flag: 0x200000, opt: "fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro"},
		{in: "rw,relatime,pagesize=2M", flag: 0x200000, opt: "pagesize=2M"},
	} {
		opt, flag := parse(tt.in)
		if opt != tt.opt {
			t.Errorf("Parsing %s(%d): got (%#x, %s), want (%#x, %s)", tt.in, i, flag, opt, tt.flag, tt.opt)
			t.Errorf("{in: %q, flag: %#x, opt: %q,},", tt.in, flag, opt)
		}

	}

}
