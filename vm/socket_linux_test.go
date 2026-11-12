// Copyright 2026 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Running a guest needs a Linux kernel and qemu, so this file keeps the OS
// suffix. The tests that only configure an Image are in socket_test.go, so
// they run on darwin and freebsd too.

package vm_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u-root/cpu/vm"
)

// helperSource is built for the guest and run there. CPUCommand serves the
// VM's directory over 9P at /tmp/cpu, so the host puts the binary there and
// no u-root image is needed.
//
// It reports the guest kernel's boot_id, which is generated per boot. A
// marker file would not do: the 9P tree comes from the client's own
// WithRoot, so each session sees its own files even when two sessions have
// reached the same guest -- which is the very thing this has to detect.
const helperSource = `package main

import (
	"fmt"
	"os"
)

func main() {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(string(b))
}
`

// buildHelper compiles the guest helper, statically and for the guest's
// architecture rather than the host's.
func buildHelper(t *testing.T, goarch string) string {
	t.Helper()

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(helperSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module helper\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(src, "helper")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+goarch, "GOFLAGS=")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the guest helper: %v: %s", err, b)
	}
	return out
}

// runMarker runs the helper in the guest and returns what it printed.
//
// cpud answers a few seconds after the VM starts, and StartVM's fixed one
// second sleep is not on its own enough, so this polls. It gives up early if
// qemu has already exited, because then nothing is ever going to answer and
// qemu's own stderr says why.
func runMarker(t *testing.T, i *vm.Image, qemu *vmProcess) (string, error) {
	t.Helper()

	var last error
	for start := time.Now(); time.Since(start) < 2*time.Minute; time.Sleep(100 * time.Millisecond) {
		if err := qemu.exited(); err != nil {
			return "", fmt.Errorf("qemu exited before cpud answered: %w", err)
		}
		cpu, err := i.CPUCommand("/tmp/cpu/helper")
		if err != nil {
			last = err
			continue
		}
		b, err := cpu.CombinedOutput()
		cpu.Close()
		if err == nil {
			return string(b), nil
		}
		last = err
	}
	return "", last
}

// vmProcess watches a running qemu, so a test can tell "not ready yet" from
// "never going to be ready" instead of polling a socket for two minutes.
type vmProcess struct {
	mu     sync.Mutex
	err    error
	done   bool
	stderr *strings.Builder
}

func (p *vmProcess) exited() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.done {
		return nil
	}
	return fmt.Errorf("%v: %s", p.err, p.stderr)
}

// startVM starts qemu and arranges for its exit and its stderr to be
// observable while the test polls.
func startVM(t *testing.T, i *vm.Image, c *exec.Cmd) *vmProcess {
	t.Helper()

	p := &vmProcess{stderr: &strings.Builder{}}
	c.Stderr = p.stderr
	if err := i.StartVM(c); err != nil {
		t.Fatalf("StartVM: got %v, want nil", err)
	}
	go func() {
		err := c.Wait()
		p.mu.Lock()
		p.err, p.done = err, true
		p.mu.Unlock()
	}()
	t.Cleanup(func() {
		if c.Process != nil {
			c.Process.Kill()
		}
	})
	return p
}

// Two guests run at once and each answers on its own socket, which is the
// thing a single shared port makes impossible.
//
// Each guest reads a marker file from its own directory, so this also shows
// the two clients reached two different guests rather than the same one
// twice. That matters because qemu's slirp_hostfwd unlinks any socket already
// at the path it is given, so a command whose output is the same everywhere
// -- a date, say -- would not notice cross-talk.
func TestTwoGuestsRunAtOnce(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skipf("the embedded image is linux/amd64, and this host is %s", runtime.GOARCH)
	}
	if _, err := exec.LookPath("qemu-system-x86_64"); err != nil {
		t.Skipf("skipping: %v", err)
	}
	helper := buildHelper(t, "amd64")

	// Keep the directory short: see maxSockPath.
	sockDir, err := os.MkdirTemp("", "s")
	if err != nil {
		t.Fatalf("socket directory: got %v, want nil", err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })

	type guest struct {
		image *vm.Image
		qemu  *vmProcess
	}
	var guests []guest
	for id := range 2 {
		d := t.TempDir()
		b, err := os.ReadFile(helper)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "helper"), b, 0o755); err != nil {
			t.Fatal(err)
		}

		i, err := vm.New("linux", "amd64", vm.WithSocket(filepath.Join(sockDir, fmt.Sprintf("c%d", id))))
		if err != nil {
			t.Fatalf("New: got %v, want nil", err)
		}
		c, err := i.CommandContext(t.Context(), d)
		if err != nil {
			t.Fatalf("CommandContext: got %v, want nil", err)
		}
		guests = append(guests, guest{image: i, qemu: startVM(t, i, c)})
	}

	seen := map[string]string{}
	for _, g := range guests {
		out, err := runMarker(t, g.image, g.qemu)
		if err != nil {
			t.Errorf("guest at %s: got %v, want nil", g.image.Socket(), err)
			continue
		}
		id := strings.TrimSpace(out)
		if id == "" {
			t.Errorf("guest at %s reported no boot_id", g.image.Socket())
			continue
		}
		if other, dup := seen[id]; dup {
			t.Errorf("the guests at %s and %s report the same boot_id %s: both clients reached one guest",
				other, g.image.Socket(), id)
			continue
		}
		seen[id] = g.image.Socket()
		t.Logf("guest at %s has boot_id %s", g.image.Socket(), id)
	}
}
