// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hugelgupf/p9/fsimpl/localfs"
	"github.com/hugelgupf/p9/p9"
)

// NewCPU9P returns a p9.Attacher for root using localfs.
func NewCPU9P(root string) p9.Attacher {
	return localfs.Attacher(root)
}

func cleanRelPath(p string) string {
	return path.Clean("/" + filepath.ToSlash(p))
}

// allowList classifies paths relative to the exported root into three states:
//   - exported: the path equals or is beneath an explicitly allowed entry.
//   - ancestor: the path is a strict parent directory of at least one allowed
//     entry (so the client kernel can walk from "/" down to the mount point,
//     stat it, and list only allowed children, without modifying anything).
//   - disallowed: all other paths.
type allowList struct {
	paths []string
}

func newAllowList(paths []string) *allowList {
	seen := make(map[string]struct{}, len(paths))
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		if len(p) == 0 {
			continue
		}
		c := cleanRelPath(p)
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		cleaned = append(cleaned, c)
	}
	return &allowList{paths: cleaned}
}

func (a *allowList) isExported(p string) bool {
	p = cleanRelPath(p)
	for _, allowed := range a.paths {
		if allowed == "/" || p == allowed || strings.HasPrefix(p, allowed+"/") {
			return true
		}
	}
	return false
}

func (a *allowList) isAncestor(p string) bool {
	p = cleanRelPath(p)
	if a.isExported(p) {
		return false
	}
	if p == "/" {
		return true
	}
	prefix := p + "/"
	for _, allowed := range a.paths {
		if strings.HasPrefix(allowed, prefix) {
			return true
		}
	}
	return false
}

func (a *allowList) isAllowed(p string) bool {
	return a.isExported(p) || a.isAncestor(p)
}

func validComponent(name string) bool {
	return len(name) > 0 && name != "." && name != ".." && !strings.Contains(name, "/")
}

func (a *allowList) canMutateEntry(parentDir, name string) bool {
	if !validComponent(name) {
		return false
	}
	parentDir = cleanRelPath(parentDir)
	return a.isExported(parentDir) && a.isExported(path.Join(parentDir, name))
}

func (a *allowList) canRemovePath(p string) bool {
	p = cleanRelPath(p)
	if p == "/" {
		return false
	}
	return a.isExported(p) && a.isExported(path.Dir(p))
}

type filter9PAttacher struct {
	p9.Attacher
	allow *allowList
}

// NewFilter9P wraps a p9.Attacher so that only paths in allow (and read-only
// traversal through their ancestor directories) are exposed to the client.
func NewFilter9P(a p9.Attacher, allow []string) p9.Attacher {
	return &filter9PAttacher{
		Attacher: a,
		allow:    newAllowList(allow),
	}
}

// Attach implements p9.Attacher.Attach.
func (a *filter9PAttacher) Attach() (p9.File, error) {
	f, err := a.Attacher.Attach()
	if err != nil {
		return nil, err
	}
	return &filter9PFile{
		File:    f,
		allow:   a.allow,
		relPath: "/",
		qidType: p9.TypeDir,
	}, nil
}

type filter9PFile struct {
	p9.File
	defaultWalker p9.DefaultWalkGetAttr
	allow         *allowList
	relPath       string
	qidType       p9.QIDType
}

var (
	_ p9.Attacher = &filter9PAttacher{}
	_ p9.File     = &filter9PFile{}
)

// WalkGetAttr implements p9.File.WalkGetAttr.
func (f *filter9PFile) WalkGetAttr(names []string) ([]p9.QID, p9.File, p9.AttrMask, p9.Attr, error) {
	return f.defaultWalker.WalkGetAttr(names)
}

// Walk implements p9.File.Walk.
func (f *filter9PFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		qids, nf, err := f.File.Walk(nil)
		if err != nil {
			return nil, nil, err
		}
		return qids, &filter9PFile{
			File:    nf,
			allow:   f.allow,
			relPath: f.relPath,
			qidType: f.qidType,
		}, nil
	}

	if f.qidType != p9.TypeDir {
		return nil, nil, syscall.ENOTDIR
	}

	cur := f.relPath
	for _, name := range names {
		if name != ".." && !validComponent(name) {
			return nil, nil, os.ErrInvalid
		}
		if cur == "/" && name == ".." {
			return nil, nil, os.ErrNotExist
		}
		next := cleanRelPath(path.Join(cur, name))
		if !f.allow.isAllowed(next) {
			return nil, nil, os.ErrNotExist
		}
		cur = next
	}

	qids, nf, err := f.File.Walk(names)
	if err != nil {
		return nil, nil, err
	}
	for i := 0; i < len(qids)-1; i++ {
		if qids[i].Type != p9.TypeDir {
			_ = nf.Close()
			return nil, nil, syscall.ENOTDIR
		}
	}
	lastType := f.qidType
	if len(qids) > 0 {
		lastType = qids[len(qids)-1].Type
	}
	return qids, &filter9PFile{
		File:    nf,
		allow:   f.allow,
		relPath: cur,
		qidType: lastType,
	}, nil
}

// Open implements p9.File.Open.
func (f *filter9PFile) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	if !f.allow.isExported(f.relPath) {
		if !f.allow.isAncestor(f.relPath) {
			return p9.QID{}, 0, os.ErrPermission
		}
		const writeFlags = p9.OpenFlags(os.O_WRONLY | os.O_RDWR | os.O_APPEND | os.O_CREATE | os.O_TRUNC)
		if f.qidType != p9.TypeDir || mode.Mode() != p9.ReadOnly || mode&writeFlags != 0 {
			return p9.QID{}, 0, os.ErrPermission
		}
	}
	return f.File.Open(mode)
}

// ReadAt implements p9.File.ReadAt.
func (f *filter9PFile) ReadAt(p []byte, offset int64) (int, error) {
	if !f.allow.isExported(f.relPath) {
		return 0, os.ErrPermission
	}
	return f.File.ReadAt(p, offset)
}

// WriteAt implements p9.File.WriteAt.
func (f *filter9PFile) WriteAt(p []byte, offset int64) (int, error) {
	if !f.allow.isExported(f.relPath) {
		return 0, os.ErrPermission
	}
	return f.File.WriteAt(p, offset)
}

// FSync implements p9.File.FSync.
func (f *filter9PFile) FSync() error {
	if !f.allow.isExported(f.relPath) {
		return os.ErrPermission
	}
	return f.File.FSync()
}

// Lock implements p9.File.Lock.
func (f *filter9PFile) Lock(pid int, locktype p9.LockType, flags p9.LockFlags, start, length uint64, client string) (p9.LockStatus, error) {
	if !f.allow.isExported(f.relPath) {
		return p9.LockStatusError, os.ErrPermission
	}
	return f.File.Lock(pid, locktype, flags, start, length, client)
}

// Readdir implements p9.File.Readdir.
func (f *filter9PFile) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	if f.allow.isExported(f.relPath) {
		return f.File.Readdir(offset, count)
	}
	if !f.allow.isAncestor(f.relPath) {
		return nil, os.ErrPermission
	}
	ents, err := f.File.Readdir(offset, math.MaxInt32)
	if err != nil {
		return nil, err
	}
	var filtered p9.Dirents
	for _, de := range ents {
		if uint64(len(filtered)) >= uint64(count) {
			break
		}
		if f.allow.isAllowed(path.Join(f.relPath, de.Name)) {
			filtered = append(filtered, de)
		}
	}
	return filtered, nil
}

// Readlink implements p9.File.Readlink.
func (f *filter9PFile) Readlink() (string, error) {
	if !f.allow.isAllowed(f.relPath) {
		return "", os.ErrPermission
	}
	return f.File.Readlink()
}

// Create implements p9.File.Create.
func (f *filter9PFile) Create(name string, mode p9.OpenFlags, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.File, p9.QID, uint32, error) {
	if !f.allow.canMutateEntry(f.relPath, name) {
		return nil, p9.QID{}, 0, os.ErrPermission
	}
	nf, qid, iounit, err := f.File.Create(name, mode, permissions, uid, gid)
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	return &filter9PFile{
		File:    nf,
		allow:   f.allow,
		relPath: cleanRelPath(path.Join(f.relPath, name)),
		qidType: qid.Type,
	}, qid, iounit, nil
}

// Mkdir implements p9.File.Mkdir.
func (f *filter9PFile) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	if !f.allow.canMutateEntry(f.relPath, name) {
		return p9.QID{}, os.ErrPermission
	}
	return f.File.Mkdir(name, permissions, uid, gid)
}

// Symlink implements p9.File.Symlink.
func (f *filter9PFile) Symlink(oldname string, newname string, uid p9.UID, gid p9.GID) (p9.QID, error) {
	if !f.allow.canMutateEntry(f.relPath, newname) {
		return p9.QID{}, os.ErrPermission
	}
	return f.File.Symlink(oldname, newname, uid, gid)
}

// Link implements p9.File.Link.
func (f *filter9PFile) Link(target p9.File, newname string) error {
	if !f.allow.canMutateEntry(f.relPath, newname) {
		return os.ErrPermission
	}
	tf, ok := target.(*filter9PFile)
	if !ok {
		return os.ErrInvalid
	}
	if !f.allow.isExported(tf.relPath) {
		return os.ErrPermission
	}
	return f.File.Link(tf.File, newname)
}

// Mknod implements p9.File.Mknod.
func (f *filter9PFile) Mknod(name string, mode p9.FileMode, major uint32, minor uint32, uid p9.UID, gid p9.GID) (p9.QID, error) {
	if !f.allow.canMutateEntry(f.relPath, name) {
		return p9.QID{}, os.ErrPermission
	}
	return f.File.Mknod(name, mode, major, minor, uid, gid)
}

// Rename implements p9.File.Rename.
func (f *filter9PFile) Rename(directory p9.File, name string) error {
	return syscall.ENOSYS
}

// RenameAt implements p9.File.RenameAt.
func (f *filter9PFile) RenameAt(oldName string, newDir p9.File, newName string) error {
	if !f.allow.canMutateEntry(f.relPath, oldName) {
		return os.ErrPermission
	}
	nd, ok := newDir.(*filter9PFile)
	if !ok {
		return os.ErrInvalid
	}
	if !f.allow.canMutateEntry(nd.relPath, newName) {
		return os.ErrPermission
	}
	return f.File.RenameAt(oldName, nd.File, newName)
}

// UnlinkAt implements p9.File.UnlinkAt.
func (f *filter9PFile) UnlinkAt(name string, flags uint32) error {
	if !f.allow.canMutateEntry(f.relPath, name) {
		return os.ErrPermission
	}
	return f.File.UnlinkAt(name, flags)
}

// Remove removes the file if it is inside an exported subtree and not the
// export root itself (unless its parent is also exported).
func (f *filter9PFile) Remove() error {
	if !f.allow.canRemovePath(f.relPath) {
		return os.ErrPermission
	}
	if r, ok := f.File.(interface{ Remove() error }); ok {
		return r.Remove()
	}
	return os.ErrPermission
}

// Renamed implements p9.File.Renamed.
func (f *filter9PFile) Renamed(parent p9.File, newName string) {
	if pf, ok := parent.(*filter9PFile); ok {
		f.relPath = cleanRelPath(path.Join(pf.relPath, newName))
		f.File.Renamed(pf.File, newName)
		return
	}
	f.File.Renamed(parent, newName)
}

// SetAttr implements p9.File.SetAttr.
func (f *filter9PFile) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	if !f.allow.isExported(f.relPath) {
		return os.ErrPermission
	}
	return f.File.SetAttr(valid, attr)
}

// SetXattr implements p9.File.SetXattr.
func (f *filter9PFile) SetXattr(attr string, data []byte, flags p9.XattrFlags) error {
	if !f.allow.isExported(f.relPath) || f.qidType == p9.TypeSymlink {
		return os.ErrPermission
	}
	return f.File.SetXattr(attr, data, flags)
}

// GetXattr implements p9.File.GetXattr.
func (f *filter9PFile) GetXattr(attr string) ([]byte, error) {
	if !f.allow.isExported(f.relPath) || f.qidType == p9.TypeSymlink {
		return nil, os.ErrPermission
	}
	return f.File.GetXattr(attr)
}

// ListXattrs implements p9.File.ListXattrs.
func (f *filter9PFile) ListXattrs() ([]string, error) {
	if !f.allow.isExported(f.relPath) || f.qidType == p9.TypeSymlink {
		return nil, os.ErrPermission
	}
	return f.File.ListXattrs()
}

// RemoveXattr implements p9.File.RemoveXattr.
func (f *filter9PFile) RemoveXattr(attr string) error {
	if !f.allow.isExported(f.relPath) || f.qidType == p9.TypeSymlink {
		return os.ErrPermission
	}
	return f.File.RemoveXattr(attr)
}
