// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/hugelgupf/p9/p9"
)

// SetAttr does not implement p9.File.SetAttr.
func (l *CPU9P) SetAttr(mask p9.SetAttrMask, attr p9.SetAttr) error {
	var err error
	// Any or ALL can be set.
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
		panic("truncate")
		//		if e := unix.Truncate(l.path, int64(attr.Size)); e != nil {
		//			err = errors.Join(err, e)
		//		}
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
			err = errors.Join(err, e)
		}
	}

	if mask.CTime {
		// The Linux client sets CTime. I did not even know that was allowed.
		// if e := errors.New("Can not set CTime on Unix"); e != nil { err = errors.Join(e)}
		verbose("mask.CTime is set by client; ignoring")
	}
	if mask.Permissions {
		err = errors.Join(err, fmt.Errorf("chmod: %w", os.ErrInvalid))
	}

	if mask.GID {
		err = errors.Join(err, fmt.Errorf("chgrp: %w", os.ErrInvalid))

	}
	if mask.UID {
		err = errors.Join(err, fmt.Errorf("chown: %w", os.ErrInvalid))
	}
	return err
}

// Lock does not implement p9.File.Lock.
func (l *CPU9P) Lock(pid int, locktype p9.LockType, flags p9.LockFlags, start, length uint64, client string) (p9.LockStatus, error) {
	return p9.LockStatusError, os.ErrInvalid
}

// SetXattr implements p9.File.SetXattr
func (l *CPU9P) SetXattr(attr string, data []byte, flags p9.XattrFlags) error {
	return os.ErrInvalid
}

// ListXattrs implements p9.File.ListXattrs
func (l *CPU9P) ListXattrs() ([]string, error) {
	return nil, os.ErrInvalid
}

// GetXattr implements p9.File.GetXattr
func (l *CPU9P) GetXattr(attr string) ([]byte, error) {
	return nil, os.ErrInvalid
}

// RemoveXattr implements p9.File.RemoveXattr
func (l *CPU9P) RemoveXattr(attr string) error {
	return os.ErrInvalid
}

func inode(_ os.FileInfo) uint64 {
	return 1
}
