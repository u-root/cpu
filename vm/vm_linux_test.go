// Copyright 2023 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// runvmtest sets VMTEST_QEMU and VMTEST_KERNEL (if not already set) with
// binaries downloaded from Docker images, then executes a command.
package vm_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u-root/cpu/client"
	"github.com/u-root/cpu/vm"
)

// TestCPUAMD64 tests both general and specific things. The specific parts are the io and cmos commands.
// It being cheaper to use a single generated initramfs, we use the full u-root for several tests.
func TestCPUAMD64(t *testing.T) {
	d := t.TempDir()
	i, err := vm.New("linux", "amd64")
	if !errors.Is(err, nil) {
		t.Fatalf("Testing kernel=linux arch=amd64: got %v, want nil", err)
	}

	if err := os.WriteFile(filepath.Join(d, "a"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	// Cancel before wg.Wait(), so goroutine can exit.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n, err := vm.Uroot(d, "linux", "amd64")
	if err != nil {
		t.Skipf("skipping this test as we have no uroot command")
	}

	c, err := i.CommandContext(ctx, d, n)
	if err != nil {
		t.Fatalf("starting VM: got %v, want nil", err)
	}
	if err := i.StartVM(c); err != nil {
		t.Fatalf("starting VM: got %v, want nil", err)
	}

	for _, tt := range []struct {
		cmd  string
		args []string
		ok   bool
	}{
		{cmd: "/bbin/dd", args: []string{"if=/dev/x"}, ok: false},
		{cmd: "/bbin/dd", args: []string{"if=/dev/null"}, ok: true},
		{cmd: "/bbin/dd", args: []string{"if=/tmp/cpu/a", "of=/tmp/cpu/b"}, ok: true},
	} {
		cpu, err := i.CPUCommand(tt.cmd, tt.args...)
		if !errors.Is(err, nil) {
			t.Errorf("CPUCommand: got %v, want nil", err)
			continue
		}
		client.SetVerbose(t.Logf)

		b, err := cpu.CombinedOutput()
		if err == nil != tt.ok {
			t.Errorf("%s %s: got %v, want %v", tt.cmd, tt.args, err == nil != tt.ok, err == nil == tt.ok)
		}
		t.Logf("%q", string(b))
	}

	b, err := os.ReadFile(filepath.Join(d, "b"))
	if err != nil {
		t.Fatalf("reading b: got %v, want nil", err)
	}
	if string(b) != "hi" {
		t.Fatalf("file b: got %q, want %q", b, "hi")
	}

	for _, tt := range []struct {
		args string
		out  string
	}{
		{args: "cw 14 1", out: ""},
		{args: "cr 14", out: "0x01\n"},
		{args: "cw 14 0", out: ""},
		{args: "cr 14", out: "0x00\n"},
	} {
		cpu, err := i.CPUCommand("/bbin/io", strings.Split(tt.args, " ")...)
		if err != nil {
			t.Fatalf("CPUCommand: got %v, want nil", err)
		}
		client.SetVerbose(t.Logf)

		b, err := cpu.CombinedOutput()
		if err != nil {
			t.Errorf("io %s: got %v, want nil", tt.args, err)
		}
		if string(b) != tt.out {
			t.Errorf("io %s: got %v, want %v", tt.args, string(b), tt.out)
		}
		t.Logf("io %s = %q", tt.args, string(b))
	}
}

// TestCPUARM tests both general and specific things. The specific parts are the io and cmos commands.
// It being cheaper to use a single generated initramfs, we use the full u-root for several tests.
func TestCPUARM(t *testing.T) {
	d := t.TempDir()
	i, err := vm.New("linux", "arm")
	if !errors.Is(err, nil) {
		t.Fatalf("Testing kernel=linux arch=arm: got %v, want nil", err)
	}

	if err := os.WriteFile(filepath.Join(d, "a"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	// Cancel before wg.Wait(), so goroutine can exit.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n, err := i.Uroot(d)
	if err != nil {
		t.Skipf("skipping this test as we have no uroot command")
	}

	c, err := i.CommandContext(ctx, d, n)
	if err != nil {
		t.Fatalf("starting VM: got %v, want nil", err)
	}
	t.Logf("Start VM: %v", c)
	if err := i.StartVM(c); err != nil {
		t.Fatalf("starting VM: got %v, want nil", err)
	}

	for _, tt := range []struct {
		cmd  string
		args []string
		ok   bool
	}{
		{cmd: "/bbin/dd", args: []string{"if=/dev/x"}, ok: false},
		{cmd: "/bbin/dd", args: []string{"if=/dev/null"}, ok: true},
		{cmd: "/bbin/dd", args: []string{"if=/tmp/cpu/a", "of=/tmp/cpu/b"}, ok: true},
	} {
		cpu, err := i.CPUCommand(tt.cmd, tt.args...)
		if !errors.Is(err, nil) {
			t.Errorf("CPUCommand: got %v, want nil", err)
			continue
		}
		client.SetVerbose(t.Logf)

		b, err := cpu.CombinedOutput()
		if err == nil != tt.ok {
			t.Errorf("%s %s: got %v, want %v", tt.cmd, tt.args, err == nil != tt.ok, err == nil == tt.ok)
		}
		t.Logf("%q", string(b))
	}

	b, err := os.ReadFile(filepath.Join(d, "b"))
	if err != nil {
		t.Fatalf("reading b: got %v, want nil", err)
	}
	if string(b) != "hi" {
		t.Fatalf("file b: got %q, want %q", b, "hi")
	}

}
