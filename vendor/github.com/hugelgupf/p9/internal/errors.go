package internal

import (
	"log"
	"os"

	"github.com/hugelgupf/p9/internal/linux"
)

// ExtractErrno extracts a linux.Errno from a error, best effort.
//
// If the system-specific or Go-specific error cannot be mapped to anything, it
// will be logged an EIO will be returned.
func ExtractErrno(err error) linux.Errno {
	switch err {
	case os.ErrNotExist:
		return linux.ENOENT
	case os.ErrExist:
		return linux.EEXIST
	case os.ErrPermission:
		return linux.EACCES
	case os.ErrInvalid:
		return linux.EINVAL
	}

	// Attempt to unwrap.
	switch e := err.(type) {
	case linux.Errno:
		return e
	case *os.PathError:
		return ExtractErrno(e.Err)
	case *os.SyscallError:
		return ExtractErrno(e.Err)
	case *os.LinkError:
		return ExtractErrno(e.Err)
	}

	if e := sysErrno(err); e != 0 {
		return e
	}

	// Default case.
	//
	// TODO: give the ability to turn this off.
	log.Printf("unknown error: %v", err)
	return linux.EIO
}
