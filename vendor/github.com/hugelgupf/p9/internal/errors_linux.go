// +build linux

package internal

import (
	"syscall"

	"github.com/hugelgupf/p9/internal/linux"
)

func sysErrno(err error) linux.Errno {
	se, ok := err.(syscall.Errno)
	if ok {
		return linux.Errno(se)
	}
	return 0
}
