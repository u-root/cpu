// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// These tests are about how an Image is configured, not about running one, so
// they belong in a file with no OS suffix: vm builds on darwin and freebsd as
// well as linux, and a _linux_test.go file would silently not run there --
// which is exactly where maxSockPath differs.

package vm_test

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/u-root/cpu/client"
	"github.com/u-root/cpu/vm"
)

// maxSockPath mirrors the unexported constant in vm, derived the same way so
// the fixtures below are right on every platform rather than on Linux alone.
const maxSockPath = len(syscall.RawSockaddrUnix{}.Path) - 1

// forward returns the hostfwd argument an Image passes to qemu.
func forward(t *testing.T, i *vm.Image) string {
	t.Helper()

	for _, a := range i.Cmd {
		if strings.Contains(a, "hostfwd=") {
			return a
		}
	}
	t.Fatalf("no hostfwd in %v", i.Cmd)
	return ""
}

// An Image with no options forwards the default port, as it always has.
func TestTheDefaultIsUnchanged(t *testing.T) {
	i, err := vm.New("linux", "amd64")
	if err != nil {
		t.Fatalf("New: got %v, want nil", err)
	}
	if got := i.Socket(); got != "" {
		t.Errorf("Socket: got %q, want empty when the default port is forwarded", got)
	}
	want := fmt.Sprintf("hostfwd=tcp::%s-:%s", client.DefaultPort, client.DefaultPort)
	if got := forward(t, i); !strings.Contains(got, want) {
		t.Errorf("forward: got %q, want it to contain %q", got, want)
	}
}

// A socket path is a name the caller owns, so two VMs cannot collide over it
// and nothing has to be reserved against a race.
func TestWithSocketForwardsAUnixSocket(t *testing.T) {
	i, err := vm.New("linux", "amd64", vm.WithSocket("/tmp/s/c"))
	if err != nil {
		t.Fatalf("New: got %v, want nil", err)
	}
	if got, want := i.Socket(), "/tmp/s/c"; got != want {
		t.Errorf("Socket: got %q, want %q", got, want)
	}
	want := fmt.Sprintf("hostfwd=unix:/tmp/s/c-:%s", client.DefaultPort)
	if got := forward(t, i); !strings.Contains(got, want) {
		t.Errorf("forward: got %q, want it to contain %q", got, want)
	}
}

// -netdev is a keyval list, so qemu splits it on "," and reads ",," as one
// literal comma. An unescaped comma in the path makes qemu refuse the whole
// option -- "Invalid parameter 'mma/c.sock-:17010'" -- so only the qemu
// argument is escaped, and Socket keeps reporting the path the caller gave.
func TestWithSocketEscapesCommasForQemuOnly(t *testing.T) {
	const path = "/tmp/co,mma/c"
	i, err := vm.New("linux", "amd64", vm.WithSocket(path))
	if err != nil {
		t.Fatalf("New: got %v, want nil", err)
	}
	if got := i.Socket(); got != path {
		t.Errorf("Socket: got %q, want the path unescaped, %q", got, path)
	}
	want := fmt.Sprintf("hostfwd=unix:/tmp/co,,mma/c-:%s", client.DefaultPort)
	if got := forward(t, i); !strings.Contains(got, want) {
		t.Errorf("forward: got %q, want it to contain %q", got, want)
	}
}

func TestWithSocketRefusesAnEmptyPath(t *testing.T) {
	if _, err := vm.New("linux", "amd64", vm.WithSocket("")); !errors.Is(err, os.ErrInvalid) {
		t.Errorf("New with an empty socket: got %v, want %v", err, os.ErrInvalid)
	}
}

// qemu refuses a relative path -- "unix socket path must be absolute" -- and
// CommandContext runs qemu with Dir set to the VM's directory, so a relative
// path would not name the same file to qemu as to the dial here. Refusing it
// here names the rule rather than leaving qemu to fail later.
func TestWithSocketRefusesARelativePath(t *testing.T) {
	for _, path := range []string{"c.sock", "./c.sock", "sub/c.sock"} {
		if _, err := vm.New("linux", "amd64", vm.WithSocket(path)); !errors.Is(err, os.ErrInvalid) {
			t.Errorf("New with the relative socket %q: got %v, want %v", path, err, os.ErrInvalid)
		}
	}
}

// A NUL cannot reach a path: exec would refuse the argument and the kernel
// would truncate sun_path there.
func TestWithSocketRefusesANUL(t *testing.T) {
	if _, err := vm.New("linux", "amd64", vm.WithSocket("/tmp/s/c\x00extra")); !errors.Is(err, os.ErrInvalid) {
		t.Errorf("New with a NUL in the socket path: got %v, want %v", err, os.ErrInvalid)
	}
}

// unix(7) caps sun_path, and the cap is not the same everywhere: 107 usable
// bytes on Linux, 103 on Darwin and FreeBSD. qemu would fail to bind, late
// and without mentioning the length, so the path is checked here -- against
// the number this platform actually uses, which is why the fixture is built
// from maxSockPath rather than written down.
func TestWithSocketRefusesAPathUnixCannotHold(t *testing.T) {
	long := "/tmp/" + strings.Repeat("a", maxSockPath+1-len("/tmp/"))
	if len(long) != maxSockPath+1 {
		t.Fatalf("the fixture is %d bytes, want %d", len(long), maxSockPath+1)
	}
	if _, err := vm.New("linux", "amd64", vm.WithSocket(long)); !errors.Is(err, os.ErrInvalid) {
		t.Errorf("New with a %d byte socket path: got %v, want %v", len(long), err, os.ErrInvalid)
	}
	if _, err := vm.New("linux", "amd64", vm.WithSocket(long[:maxSockPath])); err != nil {
		t.Errorf("New with a %d byte socket path: got %v, want nil", maxSockPath, err)
	}
}

// The limit WithSocket enforces has to be the limit the kernel enforces.
//
// This drives WithSocket, which uses its own constant, and then asks the
// kernel to bind the same paths, so it fails if the two ever disagree. That
// is the whole point of taking the number from sun_path rather than writing
// 107 down: sun_path is four bytes shorter on Darwin and FreeBSD, which this
// package also builds for, and macOS $TMPDIR is long enough to land in the
// gap on its own.
func TestTheAcceptedLengthIsTheOneTheKernelBinds(t *testing.T) {
	dir, err := os.MkdirTemp("", "s")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	longest := dir + "/" + strings.Repeat("a", maxSockPath-len(dir)-1)
	if len(longest) != maxSockPath {
		t.Fatalf("fixture is %d bytes, want %d", len(longest), maxSockPath)
	}

	// The longest path WithSocket accepts must be one the kernel binds.
	if _, err := vm.New("linux", "amd64", vm.WithSocket(longest)); err != nil {
		t.Errorf("WithSocket(%d bytes): got %v, want nil", len(longest), err)
	}
	l, err := net.Listen("unix", longest)
	if err != nil {
		t.Errorf("binding %d bytes: got %v, want nil -- WithSocket accepts a path the kernel will not bind", len(longest), err)
	} else {
		l.Close()
	}

	// One byte more must be refused by both.
	if _, err := vm.New("linux", "amd64", vm.WithSocket(longest+"b")); !errors.Is(err, os.ErrInvalid) {
		t.Errorf("WithSocket(%d bytes): got %v, want %v", len(longest)+1, err, os.ErrInvalid)
	}
	if l, err := net.Listen("unix", longest+"b"); err == nil {
		l.Close()
		t.Errorf("binding %d bytes succeeded, so maxSockPath is one too small", len(longest)+1)
	}
}
