// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mount

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func init() {
	for k, v := range map[string]uintptr{
		"mnt_async":       unix.MNT_ASYNC,
		"mnt_automounted": unix.MNT_AUTOMOUNTED,
		"mnt_cmdflags":    unix.MNT_CMDFLAGS,
		"mnt_exported":    unix.MNT_EXPORTED,
		"mnt_force":       unix.MNT_FORCE,
		"mnt_local":       unix.MNT_LOCAL,
		"mnt_multilabel":  unix.MNT_MULTILABEL,
		"mnt_noatime":     unix.MNT_NOATIME,
		"mnt_noexec":      unix.MNT_NOEXEC,
		"mnt_nosuid":      unix.MNT_NOSUID,
		"mnt_nowait":      unix.MNT_NOWAIT,
		"mnt_quota":       unix.MNT_QUOTA,
		"mnt_rdonly":      unix.MNT_RDONLY,
		"mnt_reload":      unix.MNT_RELOAD,
		"mnt_rootfs":      unix.MNT_ROOTFS,
		"mnt_snapshot":    unix.MNT_SNAPSHOT,
		"mnt_synchronous": unix.MNT_SYNCHRONOUS,
		"mnt_union":       unix.MNT_UNION,
		"mnt_update":      unix.MNT_UPDATE,
		"mnt_visflagmask": unix.MNT_VISFLAGMASK,
		"mnt_wait":        unix.MNT_WAIT,
	} {
		convert[k] = v
	}
}

// iov returns an iovec for a string.
// there is no official package, and it is simple
// enough, that we just create it here.
func iovstring(val string) syscall.Iovec {
	s := val + "\x00"
	vec := syscall.Iovec{Base: (*byte)(unsafe.Pointer(&[]byte(s)[0]))}
	vec.SetLen(len(s))
	return vec
}

// Mount takes a full fstab as a string and does whatever mounts are needed.
// It ignores comment lines, and lines with less than 6 fields. In principal,
// Mount should be able to do a full remount with the contents of /proc/mounts.
// Mount makes a best-case effort to mount the mounts passed in a
// string formatted to the fstab standard.  Callers should not die on
// a returned error, but be left in a situation in which further
// diagnostics are possible.  i.e., follow the "Boots not Bricks"
// principle.
// Freebsd has very different ways of working than linux, so
// we shell out to mount for now.
func Mount(fstab string) error {
	f, err := ioutil.TempFile("", "cpu")
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.WriteString(f, fstab); err != nil {
		return err
	}

	//if o, err := exec.Command("mount", "-a", "-F", f.Name()).CombinedOutput(); err != nil {
	//	return fmt.Errorf("mount -F %q:%s:%w", f.Name(), string(o), err)
	//}

	return nil
	var lineno int
	s := bufio.NewScanner(strings.NewReader(fstab))
	for s.Scan() {
		lineno++
		l := s.Text()
		if strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Fields(l)
		// fstab is historical, pretty free format.
		// Users may have dropped a random fstab in and we need
		// to be forgiving.
		// The last two fields no longer have any meaning or use.
		if len(f) < 6 {
			continue
		}

		// fstab fields:
		// /dev/disk/by-uuid/c0d2b09d-5330-4d08-a787-6e0e95592bf3 /boot ext4 defaults 0 0
		// what to mount, where to mount, fstype, options
		// We do need NOT to set MS_PRIVATE, since we've done a successful unshare.
		// This note is here in case someone gets confused in the future.
		// Setting MS_PRIVATE will get an EINVAL.
		dev, where, fstype, opts := f[0], f[1], f[2], f[3]

		// surprise! It turns out that correct behavior from mount is to follow symlinks
		// on where and device and use that. That's why /bin -> /usr/bin gets mounted
		// correctly.
		if w, err := filepath.EvalSymlinks(where); err == nil {
			where = w
		}
		if w, err := filepath.EvalSymlinks(dev); err == nil {
			dev = w
		}

		// The man page implies that the Linux kernel handles flags of "defaults"
		// we do no further manipulation of opts.
		flags, data := parse(opts)

		fmt.Println("WARNING: DARWIN can't mount", dev, where, fstype, flags, data)
		err = nil
		//if _, e := mount.Mount(dev, where, fstype, data, flags); e != nil {
		//	err = errors.Join(err, fmt.Errorf("Mount(%q, %q, %q, %q=>(%#x, %q)): %w", dev, where, fstype, opts, flags, data, e))
		//}
	}
	return err
}
