// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vm_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u-root/cpu/client"
	"github.com/u-root/cpu/vm"
)

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

func TestWithSocketRefusesAnEmptyPath(t *testing.T) {
	if _, err := vm.New("linux", "amd64", vm.WithSocket("")); !errors.Is(err, os.ErrInvalid) {
		t.Errorf("New with an empty socket: got %v, want %v", err, os.ErrInvalid)
	}
}

// unix(7) caps sun_path at 108 bytes including the terminator. qemu would
// fail to bind, late and obscurely, so the path is checked here.
func TestWithSocketRefusesAPathUnixCannotHold(t *testing.T) {
	long := "/tmp/" + strings.Repeat("a", 103)
	if len(long) != 108 {
		t.Fatalf("the fixture is %d bytes, want 108", len(long))
	}
	if _, err := vm.New("linux", "amd64", vm.WithSocket(long)); !errors.Is(err, os.ErrInvalid) {
		t.Errorf("New with a %d byte socket path: got %v, want %v", len(long), err, os.ErrInvalid)
	}
	if _, err := vm.New("linux", "amd64", vm.WithSocket(long[:107])); err != nil {
		t.Errorf("New with a 107 byte socket path: got %v, want nil", err)
	}
}

// waitForDate polls until cpud answers, which takes a few seconds after the
// VM starts. StartVM's fixed one second sleep is not on its own enough.
func waitForDate(t *testing.T, i *vm.Image) (string, error) {
	t.Helper()

	var last error
	for start := time.Now(); time.Since(start) < 2*time.Minute; time.Sleep(100 * time.Millisecond) {
		cpu, err := i.CPUCommand("/bbin/date")
		if err != nil {
			last = err
			continue
		}
		b, err := cpu.CombinedOutput()
		if err == nil {
			return string(b), nil
		}
		last = err
	}
	return "", last
}

// Two guests run at once and each answers on its own socket, which is the
// thing a single shared port makes impossible.
func TestTwoGuestsRunAtOnce(t *testing.T) {
	// Uroot runs the u-root command in the working directory, which go test
	// sets to this package. Building an image therefore needs a u-root
	// checkout, which this repository is not, so this skips unless one is
	// reachable.
	d := t.TempDir()
	n, err := vm.Uroot(d, "linux", "amd64")
	if err != nil {
		t.Skipf("skipping: cannot build a u-root image here: %v", err)
	}

	// Keep the directory short: see maxSockPath.
	sockDir, err := os.MkdirTemp("", "s")
	if err != nil {
		t.Fatalf("socket directory: got %v, want nil", err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })

	var images []*vm.Image
	for id := range 2 {
		i, err := vm.New("linux", "amd64", vm.WithSocket(filepath.Join(sockDir, fmt.Sprintf("c%d", id))))
		if err != nil {
			t.Fatalf("New: got %v, want nil", err)
		}
		c, err := i.CommandContext(t.Context(), t.TempDir(), n)
		if err != nil {
			t.Fatalf("CommandContext: got %v, want nil", err)
		}
		if err := i.StartVM(c); err != nil {
			t.Fatalf("StartVM: got %v, want nil", err)
		}
		t.Cleanup(func() {
			if c.Process != nil {
				c.Process.Kill()
				c.Wait()
			}
		})
		images = append(images, i)
	}

	for _, i := range images {
		out, err := waitForDate(t, i)
		if err != nil {
			t.Errorf("guest at %s: got %v, want nil", i.Socket(), err)
			continue
		}
		t.Logf("guest at %s said %q", i.Socket(), strings.TrimSpace(out))
	}
}
