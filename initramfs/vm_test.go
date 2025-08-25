// Copyright 2023 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// runvmtest sets VMTEST_QEMU and VMTEST_KERNEL (if not already set) with
// binaries downloaded from Docker images, then executes a command.
package initramfs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/u-root/cpu/client"
	"github.com/u-root/cpu/initramfs"
)

func TestCPU(t *testing.T) {
	d := t.TempDir()
	i, err := initramfs.New("linux", "amd64")
	if !errors.Is(err, nil) {
		t.Fatalf("Testing kernel=linux arch=amd64: got %v, want nil", err)
	}

	if err := os.WriteFile(filepath.Join(d, "a"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	// Cancel before wg.Wait(), so goroutine can exit.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n, err := initramfs.Uroot(d)
	if err != nil {
		t.Fatal(err)
	}
	c, err := i.CommandContext(ctx, d, n)
	if err != nil {
		t.Fatalf("starting VM: got %v, want nil", err)
	}
	if err := i.StartVM(c); err != nil {
		t.Fatalf("starting VM: got %v, want nil", err)
	}

	// TODO: make stuff not appear on stderr/out.
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
		if err != nil {
			t.Fatalf("CPUCommand: got %v, want nil", err)
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
