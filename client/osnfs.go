package client

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-git/go-billy/v5"
	osfs "github.com/go-git/go-billy/v5/osfs"
)

// NewOSFS returns a billy.FileSystem for a path.
func NewOSFS(r string) billy.Filesystem {
	bfs := osfs.New(r, osfs.WithBoundOS())
	return bfs
}

// COS or OSFS + Change wraps a billy.FS to not fail the `Change` interface.
type COS struct {
	billy.Filesystem
}

// Chmod changes mode
func (fs COS) Chmod(name string, mode os.FileMode) error {
	return os.Chmod(fs.Join(fs.Root(), name), mode)
}

// Lchown changes ownership. As in lchown(2), it does not
// follow a symlink.
func (fs COS) Lchown(name string, uid, gid int) error {
	return os.Lchown(fs.Join(fs.Root(), name), uid, gid)
}

// Chown changes ownership
func (fs COS) Chown(name string, uid, gid int) error {
	return os.Chown(fs.Join(fs.Root(), name), uid, gid)
}

// Chtimes changes access time
func (fs COS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return os.Chtimes(fs.Join(fs.Root(), name), atime, mtime)
}

// FilterFS wraps a billy.Filesystem (and optional billy.Change) so that only
// paths in allow (and read-only traversal through their ancestor directories)
// are exposed.
type FilterFS struct {
	billy.Filesystem
	allow *allowList
}

var (
	_ billy.Filesystem = &FilterFS{}
	_ billy.Change     = &FilterFS{}
)

// NewFilterFS returns a billy.Filesystem restricted to paths in allow.
func NewFilterFS(fs billy.Filesystem, allow []string) billy.Filesystem {
	return &FilterFS{
		Filesystem: fs,
		allow:      newAllowList(allow),
	}
}

func relFSPath(p string) string {
	c := strings.TrimPrefix(cleanRelPath(p), "/")
	if c == "" {
		return "."
	}
	return c
}

func evalExistingPrefix(full string, depth int) (string, error) {
	if depth > 40 {
		return "", syscall.ELOOP
	}
	cur := full
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if fi, lerr := os.Lstat(cur); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			target, rerr := os.Readlink(cur)
			if rerr != nil {
				return "", rerr
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(cur), target)
			}
			for i := len(tail) - 1; i >= 0; i-- {
				target = filepath.Join(target, tail[i])
			}
			return evalExistingPrefix(target, depth+1)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func (fs *FilterFS) resolvePath(filename string, followFinal bool) (string, error) {
	clean := cleanRelPath(filename)
	if clean == "/" {
		return "/", nil
	}
	root := fs.Filesystem.Root()
	if root == "" {
		return clean, nil
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(realRoot, filepath.FromSlash(clean))
	var resolved string
	if followFinal {
		resolved, err = evalExistingPrefix(full, 0)
		if err != nil {
			return "", err
		}
	} else {
		parent, err := evalExistingPrefix(filepath.Dir(full), 0)
		if err != nil {
			return "", err
		}
		resolved = filepath.Join(parent, filepath.Base(full))
	}
	rel, err := filepath.Rel(realRoot, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", os.ErrPermission
	}
	return cleanRelPath(rel), nil
}

// Stat implements billy.Filesystem.Stat.
func (fs *FilterFS) Stat(filename string) (os.FileInfo, error) {
	if !fs.allow.isAllowed(filename) {
		return nil, os.ErrNotExist
	}
	resolved, err := fs.resolvePath(filename, true)
	if err != nil || !fs.allow.isAllowed(resolved) {
		return nil, os.ErrNotExist
	}
	return fs.Filesystem.Stat(relFSPath(resolved))
}

// Lstat implements billy.Filesystem.Lstat.
func (fs *FilterFS) Lstat(filename string) (os.FileInfo, error) {
	if !fs.allow.isAllowed(filename) {
		return nil, os.ErrNotExist
	}
	resolved, err := fs.resolvePath(filename, false)
	if err != nil || !fs.allow.isAllowed(resolved) {
		return nil, os.ErrNotExist
	}
	return fs.Filesystem.Lstat(relFSPath(resolved))
}

// ReadDir implements billy.Filesystem.ReadDir.
func (fs *FilterFS) ReadDir(filename string) ([]os.FileInfo, error) {
	if !fs.allow.isAllowed(filename) {
		return nil, os.ErrNotExist
	}
	resolved, err := fs.resolvePath(filename, true)
	if err != nil || !fs.allow.isAllowed(resolved) {
		return nil, os.ErrNotExist
	}
	if fs.allow.isExported(filename) && fs.allow.isExported(resolved) {
		return fs.Filesystem.ReadDir(relFSPath(resolved))
	}
	if !fs.allow.isAncestor(resolved) {
		return nil, os.ErrNotExist
	}
	entries, err := fs.Filesystem.ReadDir(relFSPath(resolved))
	if err != nil {
		return nil, err
	}
	var filtered []os.FileInfo
	for _, fi := range entries {
		if fs.allow.isAllowed(path.Join(resolved, fi.Name())) {
			filtered = append(filtered, fi)
		}
	}
	return filtered, nil
}

// Readlink implements billy.Filesystem.Readlink.
func (fs *FilterFS) Readlink(link string) (string, error) {
	if !fs.allow.isAllowed(link) {
		return "", os.ErrNotExist
	}
	resolved, err := fs.resolvePath(link, false)
	if err != nil || !fs.allow.isAllowed(resolved) {
		return "", os.ErrNotExist
	}
	return fs.Filesystem.Readlink(relFSPath(resolved))
}

// Open implements billy.Filesystem.Open.
func (fs *FilterFS) Open(filename string) (billy.File, error) {
	if !fs.allow.isExported(filename) {
		if fs.allow.isAncestor(filename) {
			return nil, os.ErrPermission
		}
		return nil, os.ErrNotExist
	}
	resolved, err := fs.resolvePath(filename, true)
	if err != nil || !fs.allow.isExported(resolved) {
		return nil, os.ErrPermission
	}
	return fs.Filesystem.Open(relFSPath(resolved))
}

// OpenFile implements billy.Filesystem.OpenFile.
func (fs *FilterFS) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error) {
	if !fs.allow.isExported(filename) {
		if fs.allow.isAncestor(filename) {
			return nil, os.ErrPermission
		}
		return nil, os.ErrNotExist
	}
	if flag&os.O_CREATE != 0 && !fs.allow.canRemovePath(filename) {
		return nil, os.ErrPermission
	}
	resolved, err := fs.resolvePath(filename, true)
	if err != nil || !fs.allow.isExported(resolved) {
		return nil, os.ErrPermission
	}
	if flag&os.O_CREATE != 0 && !fs.allow.canRemovePath(resolved) {
		return nil, os.ErrPermission
	}
	return fs.Filesystem.OpenFile(relFSPath(resolved), flag, perm)
}

// Create implements billy.Filesystem.Create.
func (fs *FilterFS) Create(filename string) (billy.File, error) {
	if !fs.allow.canRemovePath(filename) {
		return nil, os.ErrPermission
	}
	resolved, err := fs.resolvePath(filename, true)
	if err != nil || !fs.allow.canRemovePath(resolved) {
		return nil, os.ErrPermission
	}
	return fs.Filesystem.Create(relFSPath(resolved))
}

// MkdirAll implements billy.Filesystem.MkdirAll.
func (fs *FilterFS) MkdirAll(filename string, perm os.FileMode) error {
	if !fs.allow.canRemovePath(filename) {
		return os.ErrPermission
	}
	resolved, err := fs.resolvePath(filename, true)
	if err != nil || !fs.allow.canRemovePath(resolved) {
		return os.ErrPermission
	}
	return fs.Filesystem.MkdirAll(relFSPath(resolved), perm)
}

// Remove implements billy.Filesystem.Remove.
func (fs *FilterFS) Remove(filename string) error {
	if !fs.allow.canRemovePath(filename) {
		return os.ErrPermission
	}
	resolved, err := fs.resolvePath(filename, false)
	if err != nil || !fs.allow.canRemovePath(resolved) {
		return os.ErrPermission
	}
	return fs.Filesystem.Remove(relFSPath(resolved))
}

// Rename implements billy.Filesystem.Rename.
func (fs *FilterFS) Rename(oldpath, newpath string) error {
	if !fs.allow.canRemovePath(oldpath) || !fs.allow.canRemovePath(newpath) {
		return os.ErrPermission
	}
	oldResolved, err := fs.resolvePath(oldpath, false)
	if err != nil || !fs.allow.canRemovePath(oldResolved) {
		return os.ErrPermission
	}
	newResolved, err := fs.resolvePath(newpath, false)
	if err != nil || !fs.allow.canRemovePath(newResolved) {
		return os.ErrPermission
	}
	return fs.Filesystem.Rename(relFSPath(oldResolved), relFSPath(newResolved))
}

// Symlink implements billy.Filesystem.Symlink.
func (fs *FilterFS) Symlink(target, link string) error {
	if !fs.allow.canRemovePath(link) {
		return os.ErrPermission
	}
	resolved, err := fs.resolvePath(link, false)
	if err != nil || !fs.allow.canRemovePath(resolved) {
		return os.ErrPermission
	}
	return fs.Filesystem.Symlink(target, relFSPath(resolved))
}

// TempFile implements billy.Filesystem.TempFile.
func (fs *FilterFS) TempFile(dir, prefix string) (billy.File, error) {
	if !fs.allow.isExported(dir) {
		return nil, os.ErrPermission
	}
	resolved, err := fs.resolvePath(dir, true)
	if err != nil || !fs.allow.isExported(resolved) {
		return nil, os.ErrPermission
	}
	return fs.Filesystem.TempFile(relFSPath(resolved), prefix)
}

// Chroot implements billy.Filesystem.Chroot.
func (fs *FilterFS) Chroot(_ string) (billy.Filesystem, error) {
	return nil, os.ErrInvalid
}

// Chmod implements billy.Change.Chmod.
func (fs *FilterFS) Chmod(name string, mode os.FileMode) error {
	if !fs.allow.isExported(name) {
		return os.ErrPermission
	}
	resolved, err := fs.resolvePath(name, true)
	if err != nil || !fs.allow.isExported(resolved) {
		return os.ErrPermission
	}
	if c, ok := fs.Filesystem.(billy.Change); ok {
		return c.Chmod(relFSPath(resolved), mode)
	}
	return os.ErrPermission
}

// Lchown implements billy.Change.Lchown.
func (fs *FilterFS) Lchown(name string, uid, gid int) error {
	if !fs.allow.isExported(name) {
		return os.ErrPermission
	}
	resolved, err := fs.resolvePath(name, false)
	if err != nil || !fs.allow.isExported(resolved) {
		return os.ErrPermission
	}
	if c, ok := fs.Filesystem.(billy.Change); ok {
		return c.Lchown(relFSPath(resolved), uid, gid)
	}
	return os.ErrPermission
}

// Chown implements billy.Change.Chown.
func (fs *FilterFS) Chown(name string, uid, gid int) error {
	if !fs.allow.isExported(name) {
		return os.ErrPermission
	}
	resolved, err := fs.resolvePath(name, true)
	if err != nil || !fs.allow.isExported(resolved) {
		return os.ErrPermission
	}
	if c, ok := fs.Filesystem.(billy.Change); ok {
		return c.Chown(relFSPath(resolved), uid, gid)
	}
	return os.ErrPermission
}

// Chtimes implements billy.Change.Chtimes.
func (fs *FilterFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	if !fs.allow.isExported(name) {
		return os.ErrPermission
	}
	resolved, err := fs.resolvePath(name, true)
	if err != nil || !fs.allow.isExported(resolved) {
		return os.ErrPermission
	}
	if c, ok := fs.Filesystem.(billy.Change); ok {
		return c.Chtimes(relFSPath(resolved), atime, mtime)
	}
	return os.ErrPermission
}
