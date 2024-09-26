// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mount

import (
	"bufio"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func init() {
	for k, v := range map[string]uintptr{
		"active":       unix.MS_ACTIVE,
		"bind":         unix.MS_BIND,
		"born":         unix.MS_BORN,
		"dirsync":      unix.MS_DIRSYNC,
		"i_version":    unix.MS_I_VERSION,
		"kernmount":    unix.MS_KERNMOUNT,
		"lazytime":     unix.MS_LAZYTIME,
		"mandlock":     unix.MS_MANDLOCK,
		"mgc_MSK":      unix.MS_MGC_MSK,
		"mgc_val":      unix.MS_MGC_VAL,
		"move":         unix.MS_MOVE,
		"noatime":      unix.MS_NOATIME,
		"nodev":        unix.MS_NODEV,
		"nodiratime":   unix.MS_NODIRATIME,
		"noexec":       unix.MS_NOEXEC,
		"noremotelock": unix.MS_NOREMOTELOCK,
		"nosec":        unix.MS_NOSEC,
		"nosuid":       unix.MS_NOSUID,
		"nosymfollow":  unix.MS_NOSYMFOLLOW,
		"posixacl":     unix.MS_POSIXACL,
		"private":      unix.MS_PRIVATE,
		"rdonly":       unix.MS_RDONLY,
		"rec":          unix.MS_REC,
		"relatime":     unix.MS_RELATIME,
		"remount":      unix.MS_REMOUNT,
		"rmt_mask":     unix.MS_RMT_MASK,
		"ro":           unix.MS_RDONLY,
		"shared":       unix.MS_SHARED,
		"silent":       unix.MS_SILENT,
		"slave":        unix.MS_SLAVE,
		"strictatime":  unix.MS_STRICTATIME,
		"submount":     unix.MS_SUBMOUNT,
		"synchronous":  unix.MS_SYNCHRONOUS,
		"unbindable":   unix.MS_UNBINDABLE,
		"verbose":      unix.MS_VERBOSE,
	} {
		convert[k] = v
	}
}

// Mount takes a full fstab as a string and does whatever mounts are needed.
// It ignores comment lines, and lines with less than 6 fields. In principal,
// Mount should be able to do a full remount with the contents of /proc/mounts.
// Mount makes a best-case effort to mount the mounts passed in a
// string formatted to the fstab standard.  Callers should not die on
// a returned error, but be left in a situation in which further
// diagnostics are possible.  i.e., follow the "Boots not Bricks"
// principle.
func Mount(fstab string) error {
	return mount(unix.Mount, fstab)
}

func mount(m mounter, fstab string) error {
	var lineno int
	s := bufio.NewScanner(strings.NewReader(fstab))
	var err error
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
		if e := m(dev, where, fstype, flags, data); e != nil {
			err = errors.Join(err, fmt.Errorf("Mount(%q, %q, %q, %q=>(%#x, %q)): %w", dev, where, fstype, opts, flags, data, e))
		}
	}
	return err
}
