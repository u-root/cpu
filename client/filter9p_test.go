// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/hugelgupf/p9/p9"
)

func TestParseBindRemotes(t *testing.T) {
	for _, tt := range []struct {
		namespace string
		want      []string
		err       error
	}{
		{namespace: "", want: nil, err: nil},
		{namespace: "/lib", want: []string{"/lib"}, err: nil},
		{namespace: "/lib=/arm/lib", want: []string{"/arm/lib"}, err: nil},
		{namespace: "/lib=/arm/lib:/bin", want: []string{"/arm/lib", "/bin"}, err: nil},
		{namespace: "/lib=/arm/lib:/bin=/b/bin", want: []string{"/arm/lib", "/b/bin"}, err: nil},
		{namespace: "/a:/bin==/b/bin", want: []string{"/a", "=/b/bin"}, err: nil},
		{namespace: "/lib=:/bin=/b/bin", want: nil, err: strconv.ErrSyntax},
		{namespace: "=/lib:/bin=/b/bin", want: nil, err: strconv.ErrSyntax},
		{namespace: "/a::/bin=/b/bin", want: nil, err: strconv.ErrSyntax},
	} {
		got, err := ParseBindRemotes(tt.namespace)
		if !errors.Is(err, tt.err) {
			t.Errorf("ParseBindRemotes(%q) error = %v, want %v", tt.namespace, err, tt.err)
			continue
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("ParseBindRemotes(%q) = %v, want %v", tt.namespace, got, tt.want)
		}
	}
}

func setupFilterTestTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dirs := []string{
		"home/user",
		"b/bin",
		"b/other",
		"etc",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", d, err)
		}
	}
	files := map[string]string{
		"home/user/hello.txt": "hello world",
		"b/bin/ls":            "binary-ls",
		"b/other/secret":      "other-secret",
		"etc/shadow":          "root-secret",
		"topsecret":           "top-secret",
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", rel, err)
		}
	}
	return root
}

func TestFilter9PEmptyAllow(t *testing.T) {
	rootDir := setupFilterTestTree(t)
	attacher := NewFilter9P(NewCPU9P(rootDir), nil)
	root, err := attacher.Attach()
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer root.Close()

	// Empty Walk clones the root ancestor fid.
	_, rootCopy, err := root.Walk(nil)
	if err != nil {
		t.Fatalf("Walk(nil): %v", err)
	}
	defer rootCopy.Close()

	// Walking to any child fails when nothing is exported.
	for _, name := range []string{"home", "etc", "topsecret", ".."} {
		if _, _, err := root.Walk([]string{name}); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Walk([%q]) = %v, want os.ErrNotExist", name, err)
		}
	}

	// Root can be opened read-only and Readdir returns no entries.
	if _, _, err := root.Open(p9.ReadOnly); err != nil {
		t.Fatalf("root.Open(ReadOnly): %v", err)
	}
	dirents, err := root.Readdir(0, 64)
	if err != nil {
		t.Fatalf("root.Readdir: %v", err)
	}
	if len(dirents) != 0 {
		t.Errorf("root.Readdir with empty allow list returned %v, want empty", dirents)
	}
}

func TestFilter9P(t *testing.T) {
	rootDir := setupFilterTestTree(t)
	attacher := NewFilter9P(NewCPU9P(rootDir), []string{"/home", "/b/bin"})
	root, err := attacher.Attach()
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer root.Close()

	// Disallowed paths return os.ErrNotExist on Walk.
	for _, names := range [][]string{
		{"etc"},
		{"etc", "shadow"},
		{"topsecret"},
		{"b", "other"},
		{"b", "other", "secret"},
		{"home", "..", "etc"},
		{".."},
	} {
		if _, _, err := root.Walk(names); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Walk(%v) = %v, want os.ErrNotExist", names, err)
		}
	}

	// Readdir on "/" only exposes "b" and "home" (even with pagination).
	_, rootReader, err := root.Walk(nil)
	if err != nil {
		t.Fatalf("Walk(nil): %v", err)
	}
	defer rootReader.Close()
	if _, _, err := rootReader.Open(p9.ReadOnly); err != nil {
		t.Fatalf("rootReader.Open(ReadOnly): %v", err)
	}
	var rootNames []string
	var offset uint64
	for {
		ents, err := rootReader.Readdir(offset, 1)
		if err != nil {
			t.Fatalf("rootReader.Readdir(%d, 1): %v", offset, err)
		}
		if len(ents) == 0 {
			break
		}
		for _, e := range ents {
			rootNames = append(rootNames, e.Name)
			offset = e.Offset
		}
	}
	slices.Sort(rootNames)
	if want := []string{"b", "home"}; !slices.Equal(rootNames, want) {
		t.Errorf("paginated Readdir(/) = %v, want %v", rootNames, want)
	}

	// Readdir on ancestor "/b" only exposes "bin", hiding "other".
	_, bDir, err := root.Walk([]string{"b"})
	if err != nil {
		t.Fatalf("Walk([\"b\"]): %v", err)
	}
	defer bDir.Close()

	// Opening an ancestor for writing must fail.
	if _, _, err := bDir.Open(p9.ReadWrite); !errors.Is(err, os.ErrPermission) {
		t.Errorf("bDir.Open(ReadWrite) = %v, want os.ErrPermission", err)
	}
	if _, _, err := bDir.Open(p9.ReadOnly); err != nil {
		t.Fatalf("bDir.Open(ReadOnly): %v", err)
	}
	bEnts, err := bDir.Readdir(0, 64)
	if err != nil {
		t.Fatalf("bDir.Readdir: %v", err)
	}
	if len(bEnts) != 1 || bEnts[0].Name != "bin" {
		t.Errorf("bDir.Readdir = %v, want [bin]", bEnts)
	}

	// Mutations on ancestor directories are rejected.
	if _, _, _, err := bDir.Create("evil", p9.ReadWrite, 0o644, p9.NoUID, p9.NoGID); !errors.Is(err, os.ErrPermission) {
		t.Errorf("bDir.Create = %v, want os.ErrPermission", err)
	}
	if _, err := bDir.Mkdir("evil", 0o755, p9.NoUID, p9.NoGID); !errors.Is(err, os.ErrPermission) {
		t.Errorf("bDir.Mkdir = %v, want os.ErrPermission", err)
	}
	if _, err := bDir.Symlink("target", "link", p9.NoUID, p9.NoGID); !errors.Is(err, os.ErrPermission) {
		t.Errorf("bDir.Symlink = %v, want os.ErrPermission", err)
	}
	if err := bDir.SetAttr(p9.SetAttrMask{Permissions: true}, p9.SetAttr{Permissions: 0o777}); !errors.Is(err, os.ErrPermission) {
		t.Errorf("bDir.SetAttr = %v, want os.ErrPermission", err)
	}
	if _, err := bDir.Lock(1, p9.WriteLock, 0, 0, 0, "test"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("bDir.Lock = %v, want os.ErrPermission", err)
	}
	// Unlinking or renaming an exported mount root from its non-exported
	// parent must fail.
	if err := bDir.UnlinkAt("bin", 0); !errors.Is(err, os.ErrPermission) {
		t.Errorf("bDir.UnlinkAt(\"bin\") = %v, want os.ErrPermission", err)
	}
	if err := root.UnlinkAt("home", 0); !errors.Is(err, os.ErrPermission) {
		t.Errorf("root.UnlinkAt(\"home\") = %v, want os.ErrPermission", err)
	}

	// Walking to "/home", then ".." lands back on "/" as an ancestor
	// (not exported).
	_, backToRoot, err := root.Walk([]string{"home", ".."})
	if err != nil {
		t.Fatalf("Walk([\"home\", \"..\"]): %v", err)
	}
	defer backToRoot.Close()
	if _, err := backToRoot.Mkdir("escaped", 0o755, p9.NoUID, p9.NoGID); !errors.Is(err, os.ErrPermission) {
		t.Errorf("backToRoot.Mkdir = %v, want os.ErrPermission", err)
	}

	// Exported subtree "/home" supports full read/write operations.
	_, homeDir, err := root.Walk([]string{"home"})
	if err != nil {
		t.Fatalf("Walk([\"home\"]): %v", err)
	}
	defer homeDir.Close()

	// Removing the mount point root itself via Remove() is blocked.
	if r, ok := homeDir.(interface{ Remove() error }); ok {
		if err := r.Remove(); !errors.Is(err, os.ErrPermission) {
			t.Errorf("homeDir.Remove() = %v, want os.ErrPermission", err)
		}
	}

	// Symlink inside "/home" pointing outside the export cannot be used
	// to mutate the target via SetAttr or walked through as a directory.
	if _, err := homeDir.Symlink(filepath.Join(rootDir, "etc/shadow"), "sym_shadow", p9.NoUID, p9.NoGID); err != nil {
		t.Fatalf("homeDir.Symlink: %v", err)
	}
	_, symFile, err := homeDir.Walk([]string{"sym_shadow"})
	if err != nil {
		t.Fatalf("Walk([\"sym_shadow\"]): %v", err)
	}
	defer symFile.Close()
	if err := symFile.SetAttr(p9.SetAttrMask{Size: true}, p9.SetAttr{Size: 0}); err == nil {
		t.Errorf("SetAttr(Size=0) on symlink succeeded, want error")
	}
	if _, err := homeDir.Symlink(filepath.Join(rootDir, "etc"), "sym_etc", p9.NoUID, p9.NoGID); err != nil {
		t.Fatalf("homeDir.Symlink(etc): %v", err)
	}
	if _, _, err := homeDir.Walk([]string{"sym_etc", "shadow"}); err == nil {
		t.Errorf("Walk through symlink succeeded, want error")
	}
	_ = homeDir.UnlinkAt("sym_shadow", 0)
	_ = homeDir.UnlinkAt("sym_etc", 0)

	// Create, write, read, rename, and unlink inside "/home".
	nf, _, _, err := homeDir.Create("created.txt", p9.ReadWrite, 0o644, p9.NoUID, p9.NoGID)
	if err != nil {
		t.Fatalf("homeDir.Create: %v", err)
	}
	defer nf.Close()
	if _, err := nf.WriteAt([]byte("payload"), 0); err != nil {
		t.Fatalf("nf.WriteAt: %v", err)
	}
	buf := make([]byte, 7)
	if n, err := nf.ReadAt(buf, 0); err != nil || n != 7 || string(buf) != "payload" {
		t.Fatalf("nf.ReadAt = (%d, %q, %v), want (7, \"payload\", nil)", n, buf, err)
	}
	if err := homeDir.RenameAt("created.txt", homeDir, "renamed.txt"); err != nil {
		t.Fatalf("homeDir.RenameAt: %v", err)
	}
	if err := homeDir.RenameAt("renamed.txt", root, "escaped.txt"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("RenameAt to ancestor root = %v, want os.ErrPermission", err)
	}
	if err := homeDir.UnlinkAt("renamed.txt", 0); err != nil {
		t.Fatalf("homeDir.UnlinkAt: %v", err)
	}
}

func TestFilterFS(t *testing.T) {
	rootDir := setupFilterTestTree(t)
	baseFS := COS{Filesystem: NewOSFS(rootDir)}
	fs := NewFilterFS(baseFS, []string{"/home", "/b/bin"})

	// Stat and Lstat succeed on ancestors and exported paths, and fail
	// on others.
	for _, okPath := range []string{"/", "/b", "/b/bin", "/b/bin/ls", "/home", "/home/user/hello.txt"} {
		if _, err := fs.Stat(okPath); err != nil {
			t.Errorf("Stat(%q): %v", okPath, err)
		}
		if _, err := fs.Lstat(okPath); err != nil {
			t.Errorf("Lstat(%q): %v", okPath, err)
		}
	}
	for _, badPath := range []string{"/etc", "/etc/shadow", "/topsecret", "/b/other", "/b/other/secret"} {
		if _, err := fs.Stat(badPath); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Stat(%q) = %v, want os.ErrNotExist", badPath, err)
		}
		if _, err := fs.Lstat(badPath); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Lstat(%q) = %v, want os.ErrNotExist", badPath, err)
		}
	}

	// ReadDir on "/" only returns "b" and "home".
	rootEnts, err := fs.ReadDir("/")
	if err != nil {
		t.Fatalf("ReadDir(/): %v", err)
	}
	var names []string
	for _, e := range rootEnts {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	if want := []string{"b", "home"}; !slices.Equal(names, want) {
		t.Errorf("ReadDir(/) = %v, want %v", names, want)
	}

	// ReadDir on "/b" only returns "bin".
	bEnts, err := fs.ReadDir("/b")
	if err != nil {
		t.Fatalf("ReadDir(/b): %v", err)
	}
	if len(bEnts) != 1 || bEnts[0].Name() != "bin" {
		t.Errorf("ReadDir(/b) = %v, want [bin]", bEnts)
	}

	// ReadDir on disallowed path returns os.ErrNotExist.
	if _, err := fs.ReadDir("/etc"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadDir(/etc) = %v, want os.ErrNotExist", err)
	}

	// Open on disallowed path returns os.ErrNotExist; on ancestor returns
	// os.ErrPermission.
	if _, err := fs.Open("/etc/shadow"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Open(/etc/shadow) = %v, want os.ErrNotExist", err)
	}
	if _, err := fs.Open("/b"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Open(/b) = %v, want os.ErrPermission", err)
	}

	// Mutations on ancestors, disallowed paths, or mount roots fail.
	if _, err := fs.Create("/evil"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Create(/evil) = %v, want os.ErrPermission", err)
	}
	if _, err := fs.Create("/home"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Create(/home) = %v, want os.ErrPermission", err)
	}
	if _, err := fs.OpenFile("/home", os.O_CREATE|os.O_RDWR, 0o644); !errors.Is(err, os.ErrPermission) {
		t.Errorf("OpenFile(/home, O_CREATE) = %v, want os.ErrPermission", err)
	}
	if err := fs.MkdirAll("/b/newdir", 0o755); !errors.Is(err, os.ErrPermission) {
		t.Errorf("MkdirAll(/b/newdir) = %v, want os.ErrPermission", err)
	}
	if err := fs.Remove("/home"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Remove(/home) = %v, want os.ErrPermission", err)
	}
	if err := fs.Rename("/home", "/home/moved"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Rename(/home, /home/moved) = %v, want os.ErrPermission", err)
	}
	if err := fs.Symlink("/home/user/hello.txt", "/b/link"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Symlink(/b/link) = %v, want os.ErrPermission", err)
	}
	chg := fs.(billy.Change)
	if err := chg.Chmod("/b", 0o777); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Chmod(/b) = %v, want os.ErrPermission", err)
	}
	if err := chg.Chtimes("/b", time.Now(), time.Now()); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Chtimes(/b) = %v, want os.ErrPermission", err)
	}

	// Operations inside exported "/home" succeed.
	f, err := fs.Open("/home/user/hello.txt")
	if err != nil {
		t.Fatalf("Open(/home/user/hello.txt): %v", err)
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(data) != "hello world" {
		t.Fatalf("ReadAll = (%q, %v), want (\"hello world\", nil)", data, err)
	}

	nf, err := fs.Create("/home/user/new.txt")
	if err != nil {
		t.Fatalf("Create(/home/user/new.txt): %v", err)
	}
	if _, err := nf.Write([]byte("nfs-test")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	nf.Close()

	if err := chg.Chmod("/home/user/new.txt", 0o600); err != nil {
		t.Fatalf("Chmod(/home/user/new.txt): %v", err)
	}
	if err := fs.Rename("/home/user/new.txt", "/home/user/renamed.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := fs.Remove("/home/user/renamed.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Symlinks inside "/home" pointing to disallowed paths must not allow
	// reading, writing, creating, or chmodding the target via FilterFS.
	if err := fs.Symlink("../../etc/shadow", "/home/user/escape"); err != nil {
		t.Fatalf("Symlink(escape): %v", err)
	}
	if _, err := fs.Lstat("/home/user/escape"); err != nil {
		t.Errorf("Lstat(/home/user/escape): %v", err)
	}
	if _, err := fs.Open("/home/user/escape"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Open(/home/user/escape) = %v, want os.ErrPermission", err)
	}
	if err := chg.Chmod("/home/user/escape", 0o777); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Chmod(/home/user/escape) = %v, want os.ErrPermission", err)
	}
	if err := fs.Symlink("../../etc/newfile", "/home/user/dangling"); err != nil {
		t.Fatalf("Symlink(dangling): %v", err)
	}
	if _, err := fs.Create("/home/user/dangling"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("Create(/home/user/dangling) = %v, want os.ErrPermission", err)
	}
}

func TestWithUnsafeExportOption(t *testing.T) {
	c := Command("host", "ls")
	if c.UnsafeExport {
		t.Fatalf("default UnsafeExport = true, want false")
	}
	if err := WithUnsafeExport(true)(c); err != nil {
		t.Fatalf("WithUnsafeExport(true): %v", err)
	}
	if !c.UnsafeExport {
		t.Fatalf("UnsafeExport after WithUnsafeExport(true) = false, want true")
	}
}
