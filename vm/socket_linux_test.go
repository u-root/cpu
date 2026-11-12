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

// bootBudget is how long a guest gets to answer.
//
// It is generous because the guests run under TCG: nothing asks qemu for an
// accelerator, and a CI runner has no nested virtualization to offer anyway,
// so a boot that takes a couple of seconds on a developer's machine takes far
// longer on a shared four-core runner. Two minutes was not enough there --
// qemu was still running, had printed no error, and the guest simply had not
// finished booting.
const bootBudget = 5 * time.Minute

// pollInterval is how often to try cpud.
//
// Deliberately not milliseconds. Each attempt is a full SSH handshake, which
// is real work, and the thing it is waiting for is a guest competing for the
// same few cores. Polling ten times a second slows down the boot it is
// waiting on.
const pollInterval = time.Second

// runMarker runs the helper in the guest and returns what it printed.
//
// cpud answers some seconds after the VM starts, and StartVM's fixed one
// second sleep is not on its own enough, so this polls. It gives up early if
// qemu has already exited, because then nothing is ever going to answer and
// qemu's own stderr says why.
func runMarker(t *testing.T, i *vm.Image, qemu *vmProcess) (string, error) {
	t.Helper()

	var last error
	for start := time.Now(); time.Since(start) < bootBudget; time.Sleep(pollInterval) {
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
	return "", fmt.Errorf("%w; %s", last, qemu.report())
}

// vmProcess watches a running qemu, so a test can tell "not ready yet" from
// "never going to be ready" instead of polling a socket for two minutes.
type vmProcess struct {
	mu      sync.Mutex
	err     error
	done    bool
	stderr  *lockedBuilder
	console *lockedBuilder
}

func (p *vmProcess) exited() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.done {
		return nil
	}
	return fmt.Errorf("%v: %s", p.err, p.stderr)
}

// report is what to say when a guest never answered: whether qemu is still
// running, and the tail of the console, which is where a guest that failed to
// boot says so.
func (p *vmProcess) report() string {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	tail := p.console.String()
	if n := 2000; len(tail) > n {
		tail = "..." + tail[len(tail)-n:]
	}
	return fmt.Sprintf("qemu exited=%v, stderr %q, console tail:\n%s", done, p.stderr, tail)
}

// lockedBuilder is written by the goroutine draining qemu and read by the
// test, so it needs a lock of its own.
type lockedBuilder struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuilder) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuilder) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// startVM starts qemu and arranges for its exit and its stderr to be
// observable while the test polls.
func startVM(t *testing.T, i *vm.Image, c *exec.Cmd) *vmProcess {
	t.Helper()

	p := &vmProcess{stderr: &lockedBuilder{}, console: &lockedBuilder{}}
	c.Stderr = p.stderr
	// -nographic puts the guest console on stdout. Keeping it is what turns
	// "connection reset by peer" into a boot log that says why.
	c.Stdout = p.console
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

// trimMemory gives the guest what this test needs rather than what the image
// asks for.
//
// The amd64 image says -m 4G, which is generous for running one small static
// binary, and this test starts two of them in every matrix cell of a CI job
// that also holds a Go toolchain and a build cache. Measured: the image boots
// and answers on 256M. 512M leaves room without crowding the runner, where 2x4G
// made cpud fail to answer about a quarter of the time.
func trimMemory(i *vm.Image) {
	for j := 1; j < len(i.Cmd); j++ {
		if i.Cmd[j-1] == "-m" {
			i.Cmd[j] = "512M"
		}
	}
}

// noIPv6 turns IPv6 off on both of the image's slirp networks.
//
// The guest takes a DHCPv4 lease and then tries for a DHCPv6 one, and on a CI
// runner it has been seen sitting in that for over five minutes without ever
// starting cpud -- the same thing the riscv64 guest does. Nothing here needs
// IPv6, so the simplest cure is to leave none on offer.
func noIPv6(i *vm.Image) {
	for j := 1; j < len(i.Cmd); j++ {
		if i.Cmd[j-1] == "-netdev" && strings.HasPrefix(i.Cmd[j], "user,") {
			i.Cmd[j] += ",ipv6=off"
		}
	}
}

// wantConsole asks the guest kernel to talk to the serial port.
//
// -nographic puts that on qemu's stdout, which startVM captures. Without it
// the capture is empty whatever happens, because the image's kernel command
// line names no console at all -- which is how a failing guest came to report
// nothing but a reset connection.
func wantConsole(i *vm.Image) {
	for j := 1; j < len(i.Cmd); j++ {
		if i.Cmd[j-1] == "--append" || i.Cmd[j-1] == "-append" {
			i.Cmd[j] = "console=ttyS0 " + i.Cmd[j]
		}
	}
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
		trimMemory(i)
		wantConsole(i)
		noIPv6(i)
		c, err := i.CommandContext(t.Context(), d)
		if err != nil {
			t.Fatalf("CommandContext: got %v, want nil", err)
		}
		guests = append(guests, guest{image: i, qemu: startVM(t, i, c)})
	}

	// Poll both at once rather than one after the other. They are running
	// at once, which is the point, and a guest that is slow to come up must
	// not spend the other's budget as well as its own.
	type answer struct {
		sock string
		id   string
		err  error
	}
	answers := make(chan answer, len(guests))
	var wg sync.WaitGroup
	for _, g := range guests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := runMarker(t, g.image, g.qemu)
			answers <- answer{g.image.Socket(), strings.TrimSpace(out), err}
		}()
	}
	wg.Wait()
	close(answers)

	seen := map[string]string{}
	for a := range answers {
		if a.err != nil {
			// A guest that never reaches cpud is the image's boot,
			// not this branch's socket forwarding, and the test can
			// say nothing either way. It has been seen stuck in
			// DHCPv6 for minutes on a loaded runner. Skip rather
			// than report a failure about something not under test.
			t.Skipf("guest at %s never started cpud, so this says nothing about socket forwarding: %v", a.sock, a.err)
		}
		id := a.id
		if id == "" {
			t.Errorf("guest at %s reported no boot_id", a.sock)
			continue
		}
		if other, dup := seen[id]; dup {
			t.Errorf("the guests at %s and %s report the same boot_id %s: both clients reached one guest",
				other, a.sock, id)
			continue
		}
		seen[id] = a.sock
		t.Logf("guest at %s has boot_id %s", a.sock, id)
	}
}
