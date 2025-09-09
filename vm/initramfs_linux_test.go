package vm_test

import (
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/u-root/cpu/vm"
)

func TestInitRamfs(t *testing.T) {
	if _, err := vm.New("no", "amd64"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Testing kernel=no: got %v, want %v", err, os.ErrNotExist)
	}

	if _, err := vm.New("linux", "no"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Testing arch=no: got %v, want %v", err, os.ErrNotExist)
	}

	b, err := vm.New("linux", "amd64")
	if !errors.Is(err, nil) {
		t.Fatalf("Testing kernel=linux arch=amd64: got %v, want nil", err)
	}

	n := "initramfs_linux_amd64.cpio"
	f, err := os.ReadFile(n)
	if err != nil {
		t.Fatalf("Reading cpio: got %v, want nil", err)
	}

	if !slices.Equal(b.InitRAMFS, f) {
		t.Fatalf("initramfs: Uncompress %q is not the same as compiled-in initramfs", n)
	}

	n = "kernel_linux_amd64"
	if f, err = os.ReadFile(n); err != nil {
		t.Fatalf("Reading kernel: got %v, want nil", err)
	}

	if !slices.Equal(b.Kernel, f) {
		t.Fatalf("kernel:uncompress %q is not the same as compiled-in kernel", n)
	}

}
