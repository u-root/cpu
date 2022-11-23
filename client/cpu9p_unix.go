// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows && !plan9
// +build !windows,!plan9

package client

import (
	"os"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/hugelgupf/p9/p9"
	"golang.org/x/sys/unix"
)

// SetAttr implements p9.File.SetAttr.
func (l *cpu9p) SetAttr(mask p9.SetAttrMask, attr p9.SetAttr) error {
	var err error
	// Any or ALL can be set.
	// We started out with this:
	// err = multierror.Append
	// for each case. It seemed clean. But it returns a non-nil
	// error, even if all arguments are nil. Interesting.
	// Don't believe HashiCorp's docs about result.ErrorOrNil():
	// they're just flat-out wrong; even their example doesn't work.
	// A setattr could include things to set,
	// and a permission value that makes setting those
	// things impossible. Therefore, do these
	// permission-y things last:
	// Permissions
	// GID
	// UID
	// Since changing, e.g., UID or GID might make
	// changing permissions impossible.
	//
	// The test actually caught this ...

	if mask.Size {
		if e := unix.Truncate(l.path, int64(attr.Size)); e != nil {
			err = multierror.Append(e)
		}
	}
	if mask.ATime || mask.MTime {
		atime, mtime := time.Now(), time.Now()
		if mask.ATimeNotSystemTime {
			atime = time.Unix(int64(attr.ATimeSeconds), int64(attr.ATimeNanoSeconds))
		}
		if mask.MTimeNotSystemTime {
			mtime = time.Unix(int64(attr.MTimeSeconds), int64(attr.MTimeNanoSeconds))
		}
		if e := os.Chtimes(l.path, atime, mtime); e != nil {
			err = multierror.Append(e)
		}
	}

	if mask.CTime {
		// The Linux client sets CTime. I did not even know that was allowed.
		// if e := errors.New("Can not set CTime on Unix"); e != nil { err = multierror.Append(e)}
		verbose("mask.CTime is set by client; ignoring")
	}
	if mask.Permissions {
		if e := unix.Chmod(l.path, uint32(attr.Permissions)); e != nil {
			err = multierror.Append(e)
		}
	}

	if mask.GID {
		if e := unix.Chown(l.path, -1, int(attr.GID)); e != nil {
			err = multierror.Append(e)
		}
	}
	if mask.UID {
		if e := unix.Chown(l.path, int(attr.UID), -1); e != nil {
			err = multierror.Append(e)
		}
	}
	return err
}
