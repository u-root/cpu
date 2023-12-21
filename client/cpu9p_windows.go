// Copyright 2018 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"errors"
	"os"

	"github.com/hugelgupf/p9/p9"
)

// GetAttr implements p9.File.GetAttr.
//
// Not fully implemented.
func (l *CPU9P) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	qid, fi, err := l.info()
	if err != nil {
		return qid, p9.AttrMask{}, p9.Attr{}, err
	}

	t := uint64(fi.ModTime().Unix())
	// In the unix-style code, we derive the Mode directly
	// from the syscall.Stat_t. That is not available on
	// Windows, so we have to reconstruct some mode bits
	// in other ways.
	mode := p9.FileMode(fi.Mode())
	if fi.IsDir() {
		mode |= p9.ModeDirectory
	}
	attr := p9.Attr{
		Mode:             mode,
		UID:              0,
		GID:              0,
		NLink:            3,
		Size:             uint64(fi.Size()),
		BlockSize:        uint64(512),
		Blocks:           uint64(fi.Size() / 512),
		ATimeSeconds:     t,
		ATimeNanoSeconds: 0,
		MTimeSeconds:     t,
		MTimeNanoSeconds: 0,
		CTimeSeconds:     t,
		CTimeNanoSeconds: 0,
	}
	valid := p9.AttrMask{
		Mode:   true,
		UID:    true,
		GID:    true,
		NLink:  true,
		RDev:   true,
		Size:   true,
		Blocks: true,
		ATime:  true,
		MTime:  true,
		CTime:  true,
	}

	return qid, valid, attr, nil
}

// Lock implements p9.File.Lock.
// Well, not really.
func (l *CPU9P) Lock(pid int, locktype p9.LockType, flags p9.LockFlags, start, length uint64, client string) (p9.LockStatus, error) {
	return p9.LockStatusOK, nil
}

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
		err = errors.Join(err, os.ErrInvalid)
	}
	if mask.ATime || mask.MTime {
		err = errors.Join(err, os.ErrInvalid)
	}

	if mask.CTime {
		err = errors.Join(err, os.ErrInvalid)
		verbose("mask.CTime is set by client; ignoring")
	}
	if mask.Permissions {
		err = errors.Join(err, os.ErrInvalid)
	}

	if mask.GID {
		err = errors.Join(err, os.ErrInvalid)
	}
	if mask.UID {
		err = errors.Join(err, os.ErrInvalid)
	}
	return err
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
	return 0xd00dfeed
}
