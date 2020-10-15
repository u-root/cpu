// +build windows

package internal

import (
	"syscall"

	"github.com/hugelgupf/p9/internal/linux"
)

func sysErrno(err error) linux.Errno {
	switch err {
	case syscall.ERROR_FILE_NOT_FOUND:
		return linux.ENOENT
	case syscall.ERROR_PATH_NOT_FOUND:
		return linux.ENOENT
	case syscall.ERROR_ACCESS_DENIED:
		return linux.EACCES
	case syscall.ERROR_FILE_EXISTS:
		return linux.EEXIST
	case syscall.ERROR_INSUFFICIENT_BUFFER:
		return linux.ENOMEM
	default:
		// No clue what to do with others.
		return 0
	}
}
