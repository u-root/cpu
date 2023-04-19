// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !plan9
// +build !plan9

package mount

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

// This mounter type may be useful should we need more tests: we can call mount with a mock
// mounter.
type mounter func(source string, target string, fstype string, flags uintptr, data string) error

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
		// The man page implies that the Linux kernel handles flags of "defaults"
		// we do no further manipulation of opts.
		flags, data := parse(opts)
		if src, e := os.Stat(dev); e == nil && !src.IsDir() && flags&unix.MS_BIND != 0 {
			// Source dev is a file and we are going to do a bind mount.
			if target, e := os.Stat(where); e != nil {
				// Destination does not exist, so we are going to create an empty file.
				if e := os.MkdirAll(path.Dir(where), 0666); e != nil {
					// Creation failed.
					err = errors.Join(err, fmt.Errorf("cannot create dir %s: %s", path.Dir(where), e))
					continue
				}
				if err := os.WriteFile(where, []byte{}, 0666); err != nil {
					// Creation failed.
					err = errors.Join(err, fmt.Errorf("cannot create target file %s: %s", where, e))
					continue
				}
			} else if target.IsDir() {
				// Destination exists, but it is a directory.
				err = errors.Join(err, fmt.Errorf("cannot bind file %s to a dir %s", dev, where))
				continue
			}
		} else {
			if e := os.MkdirAll(where, 0666); e != nil && !os.IsExist(e) {
				err = errors.Join(err, e)
				continue
			}
		}
		if e := m(dev, where, fstype, flags, data); e != nil {
			err = errors.Join(err, fmt.Errorf("Mount(%q, %q, %q, %q=>(%#x, %q)): %v", dev, where, fstype, opts, flags, data, e))
		}
	}
	return err
}

// There are string args that must be converted to uintptr

var convert = map[string]uintptr{
	"active":       unix.MS_ACTIVE,
	"async":        unix.MS_ASYNC,
	"bind":         unix.MS_BIND,
	"born":         unix.MS_BORN,
	"dirsync":      unix.MS_DIRSYNC,
	"invalidate":   unix.MS_INVALIDATE,
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
	// internal use only according to mount(2) "nouser":       unix.MS_NOUSER,
	"posixacl":    unix.MS_POSIXACL,
	"private":     unix.MS_PRIVATE,
	"rdonly":      unix.MS_RDONLY,
	"rec":         unix.MS_REC,
	"relatime":    unix.MS_RELATIME,
	"remount":     unix.MS_REMOUNT,
	"rmt_mask":    unix.MS_RMT_MASK,
	"ro":          unix.MS_RDONLY,
	"rw":          0,
	"shared":      unix.MS_SHARED,
	"silent":      unix.MS_SILENT,
	"slave":       unix.MS_SLAVE,
	"strictatime": unix.MS_STRICTATIME,
	"submount":    unix.MS_SUBMOUNT,
	"sync":        unix.MS_SYNC,
	"synchronous": unix.MS_SYNCHRONOUS,
	"unbindable":  unix.MS_UNBINDABLE,
	"verbose":     unix.MS_VERBOSE,
}

var ignore = map[string]interface{}{
	"blkio":  nil,
	"nouser": nil,
}

func parse(m string) (uintptr, string) {
	var opts []string
	var flags uintptr
	for _, f := range strings.Split(strings.TrimSpace(m), ",") {
		if f == "defaults" {
			// "rw", "suid", "dev", "exec", "auto", "nouser", "async"
			// rw is 0
			// suid is 0
			// exec is 0
			// auto is 0
			// nouser is internal to the kernel -- why does mount(1) document it as a default then?
			// async is documented as default on mount(1) but does not show up in /proc/mounts
			// So: defaults is just consumed ... opt remains unchanged, ret remains unchanged.
			// weird. It's almost a noise word now.
			continue
		}
		if v, ok := convert[f]; ok {
			flags |= v
		} else if _, ok := ignore[f]; !ok {
			opts = append(opts, f)
		}
	}
	return flags, strings.Join(opts, ",")

}
