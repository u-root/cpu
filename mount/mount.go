// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !plan9
// +build !plan9

package mount

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/go-multierror"
	"golang.org/x/sys/unix"
)

// This mounter type may be useful should we need more tests: we can call mount with a mock
// mounter.
type mounter func(source string, target string, fstype string, flags uintptr, data string) error

// Mount takes a full fstab as a string and does whatever mounts are needed.
// It ignores comment lines, and lines with less than 6 fields. In principal,
// Mount should be able to do a full remount with the contents of /proc/mounts.
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
		// Note that flags are old school, binary numbers, and we're hoping to avoid them.
		// We do need NOT to set MS_PRIVATE, since we've done a successful unshare.
		// This note is here in case someone gets confused in the future.
		// If you set this flag to MS_PRIVATE, on some file systems, you'll get an EINVAL.
		var flags uintptr
		dev, where, fstype, opts := f[0], f[1], f[2], f[3]
		// The man page implies that the Linux kernel handles flags of "defaults"
		// we do no further manipulation of opts.
		if e := os.MkdirAll(where, 0666); e != nil && !os.IsExist(e) {
			err = multierror.Append(err, e)
			continue
		}
		if e := m(dev, where, fstype, flags, opts); e != nil {
			err = multierror.Append(err, fmt.Errorf("Mount(%q, %q, %q, %#x, %q): %v", dev, where, fstype, flags, opts, e))
		}
	}
	return err
}

// parse takes a mount options string and transforms
// elements as needed (e.g. 'defaults') so the kernel
// will accept it.
func parse(m string) string {
	var ret []string
	for _, f := range strings.Split(strings.TrimSpace(m), ",") {
		switch f {
		case "defaults":
			ret = append(ret, "rw", "suid", "dev", "exec", "auto", "nouser", "async")
		default:
			ret = append(ret, f)
		}
	}
	return strings.Join(ret, ",")

}
