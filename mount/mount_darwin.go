// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin
// +build darwin

package mount

import (
	"fmt"
	"golang.org/x/sys/unix"
)

// Mount takes a full fstab as a string and does whatever mounts are needed.
// It ignores comment lines, and lines with less than 6 fields. In principal,
// Mount should be able to do a full remount with the contents of /proc/mounts.
// Mount makes a best-case effort to mount the mounts passed in a
// string formatted to the fstab standard.  Callers should not die on
// a returned error, but be left in a situation in which further
// diagnostics are possible.  i.e., follow the "Boots not Bricks"
// principle.
func Mount(fstab string) error {
	return mount(nil, fstab)
}

var convert = map[string]uintptr{
	"async":      unix.MS_ASYNC,
	"invalidate": unix.MS_INVALIDATE,
	// internal use only according to mount(2) "nouser":       unix.MS_NOUSER,
	"rw":   0,
	"sync": unix.MS_SYNC,
}

func mount(m mounter, fstab string) error {
	return fmt.Errorf("No fstab mount on darwin")
}
