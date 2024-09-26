// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !plan9
// +build !plan9

package mount

import (
	"strings"

	"golang.org/x/sys/unix"
)

// This mounter type may be useful should we need more tests: we can call mount with a mock
// mounter.
type mounter func(source string, target string, fstype string, flags uintptr, data string) error

// There are string args that must be converted to uintptr

var convert = map[string]uintptr{

	"async":      unix.MS_ASYNC,
	"invalidate": unix.MS_INVALIDATE,
	// internal use only according to mount(2) "nouser":       unix.MS_NOUSER,
	"rw":   0,
	"sync": unix.MS_SYNC,
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
