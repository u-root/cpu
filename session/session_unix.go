// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !plan9,!windows

package session

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/go-multierror"
	"golang.org/x/sys/unix"
)

// fstab makes a best-case effort to mount the mounts passed in a
// string formatted to the fstab standard.  Callers should not die on
// a returned error, but be left in a situation in which further
// diagnostics are possible.  i.e, follow the "Boots not Bricks"
// principle.
func fstab(t string) error {
	var result error

	var lineno int
	s := bufio.NewScanner(strings.NewReader(t))
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
		v("Line %d: Try to mount %q(%q)", lineno, l, f)
		if err := os.MkdirAll(where, 0666); err != nil && !os.IsExist(err) {
			result = multierror.Append(result, err)
			continue
		}
		if err := unix.Mount(dev, where, fstype, flags, opts); err != nil {
			result = multierror.Append(result, fmt.Errorf("Mount(%q, %q, %q, %#x, %q): %v", dev, where, fstype, flags, opts, err))
		}
	}
	return result
}
