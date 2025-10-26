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
	"syscall"

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

	s, ns := fi.ModTime().Second(), fi.ModTime().Nanosecond()

	attr := p9.Attr{
		Mode:             p9.FileMode(fi.Mode()),
		Size:             uint64(fi.Size()),
		BlockSize:        8192,
		Blocks:           uint64(fi.Size()) / 8192,
		MTimeSeconds:     uint64(s),
		MTimeNanoSeconds: uint64(ns),
	}

	valid := p9.AttrMask{
		Mode:   true,
		UID:    false,
		GID:    false,
		NLink:  false,
		RDev:   false,
		Size:   true,
		Blocks: true,
		ATime:  false,
		MTime:  true,
		CTime:  false,
	}

	if d, ok := fi.Sys().(*syscall.Dir); ok {
		valid.ATime, attr.ATimeSeconds, attr.ATimeNanoSeconds = true, uint64(d.Atime), 0
		valid.RDev, attr.RDev = true, p9.Dev(d.Dev)
	}

	return qid, valid, attr, nil
}
