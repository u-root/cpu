// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/hugelgupf/p9/p9"
)

// ErrNoDirInfo means Sys() was empty after a Stat()
var ErrNoDirInfo = errors.New("no Sys() after a Stat()")

// SetAttr implements p9.File.SetAttr.
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
		if e := os.Truncate(l.path, int64(attr.Size)); e != nil {
			err = errors.Join(err, fmt.Errorf("truncate:%w", err))
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
			err = errors.Join(err, e)
		}
	}

	if mask.CTime {
		// The Linux client sets CTime. I did not even know that was allowed.
		// if e := errors.New("Can not set CTime on Unix"); e != nil { err = errors.Join(e)}
		verbose("mask.CTime is set by client; ignoring")
	}
	if mask.Permissions {
		perm := uint32(attr.Permissions)
		if e := os.Chmod(l.path, os.FileMode(perm)); e != nil {
			err = errors.Join(err, fmt.Errorf("%q:%o:%w", l.path, perm, err))
		}
	}

	if mask.GID {
		err = errors.Join(err, os.ErrPermission)
	}
	if mask.UID {
		err = errors.Join(err, os.ErrPermission)
	}
	return err
}

// Lock implements p9.File.Lock.
// No such thing on Plan 9. Just say ok.
func (l *CPU9P) Lock(pid int, locktype p9.LockType, flags p9.LockFlags, start, length uint64, client string) (p9.LockStatus, error) {
	return p9.LockStatusOK, nil
}

// info constructs a QID for this file.
func (l *CPU9P) info() (p9.QID, os.FileInfo, error) {
	var (
		qid p9.QID
		fi  os.FileInfo
		err error
	)

	// Stat the file.
	if l.file != nil {
		fi, err = l.file.Stat()
	}

	if err != nil {
		return qid, nil, err
	}

	d, ok := fi.Sys().(*syscall.Dir)
	if !ok {
		return qid, fi, fmt.Errorf("%q:%v", l.file.Name(), ErrNoDirInfo)

	}

	qid.Path, qid.Version, qid.Type = d.Qid.Path, d.Qid.Vers, p9.QIDType(d.Qid.Type)

	return qid, fi, nil
}

// SetXattr implements p9.File.SetXattr
func (l *CPU9P) SetXattr(attr string, data []byte, flags p9.XattrFlags) error {
	return ErrNosys
}

// ListXattrs implements p9.File.ListXattrs
// Since there technically are none, return an empty []string and
// no error.
func (l *CPU9P) ListXattrs() ([]string, error) {
	return []string{}, nil
}

// GetXattr implements p9.File.GetXattr
func (l *CPU9P) GetXattr(attr string) ([]byte, error) {
	return nil, nil
}

// RemoveXattr implements p9.File.RemoveXattr
func (l *CPU9P) RemoveXattr(attr string) error {
	return ErrNosys
}
