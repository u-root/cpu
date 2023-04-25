//! 9P error representations.
//!
//! In 9P2000 errors are represented as strings.
//! All the error strings in this module are imported from include/net/9p/error.c of Linux kernel.
//!
//! By contrast, in 9P2000.L, errors are represented as numbers (errno).
//! Using the Linux system errno numbers is the expected behaviour.

use crate::error::errno::*;
use std::io::ErrorKind::*;
use std::{fmt, io};

fn errno_from_io_error(e: &io::Error) -> nix::errno::Errno {
    e.raw_os_error()
        .map(nix::errno::from_i32)
        .unwrap_or_else(|| match e.kind() {
            NotFound => ENOENT,
            PermissionDenied => EPERM,
            ConnectionRefused => ECONNREFUSED,
            ConnectionReset => ECONNRESET,
            ConnectionAborted => ECONNABORTED,
            NotConnected => ENOTCONN,
            AddrInUse => EADDRINUSE,
            AddrNotAvailable => EADDRNOTAVAIL,
            BrokenPipe => EPIPE,
            AlreadyExists => EALREADY,
            WouldBlock => EAGAIN,
            InvalidInput => EINVAL,
            InvalidData => EINVAL,
            TimedOut => ETIMEDOUT,
            WriteZero => EAGAIN,
            Interrupted => EINTR,
            _ => EIO,
        })
}

/// 9P error type which is convertible to an errno.
///
/// The value of `Error::errno()` will be used for Rlerror.
///
/// # Protocol
/// 9P2000.L
#[derive(Debug)]
pub enum Error {
    /// System error containing an errno.
    No(nix::errno::Errno),
    /// I/O error.
    Io(io::Error),
}

impl Error {
    /// Get an errno representations.
    pub fn errno(&self) -> nix::errno::Errno {
        match *self {
            Error::No(ref e) => *e,
            Error::Io(ref e) => errno_from_io_error(e),
        }
    }
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match *self {
            Error::No(ref e) => write!(f, "System error: {}", e.desc()),
            Error::Io(ref e) => write!(f, "I/O error: {}", e),
        }
    }
}

impl std::error::Error for Error {
    fn cause(&self) -> Option<&dyn std::error::Error> {
        match *self {
            Error::No(_) => None,
            Error::Io(ref e) => Some(e),
        }
    }
}

impl From<io::Error> for Error {
    fn from(e: io::Error) -> Self {
        Error::Io(e)
    }
}

impl<'a> From<&'a io::Error> for Error {
    fn from(e: &'a io::Error) -> Self {
        Error::No(errno_from_io_error(e))
    }
}

impl From<nix::Error> for Error {
    fn from(e: nix::Error) -> Self {
        Error::No(e)
    }
}

/// The system errno definitions.
///
/// # Protocol
/// 9P2000.L
pub mod errno {
    pub use nix::errno::Errno::*;
}

/// 9P error strings imported from Linux.
///
/// # Protocol
/// 9P2000
pub mod string {
    pub const EPERM: &str = "Operation not permitted";
    pub const EPERM_WSTAT: &str = "wstat prohibited";
    pub const ENOENT: &str = "No such file or directory";
    pub const ENOENT_DIR: &str = "directory entry not found";
    pub const ENOENT_FILE: &str = "file not found";
    pub const EINTR: &str = "Interrupted system call";
    pub const EIO: &str = "Input/output error";
    pub const ENXIO: &str = "No such device or address";
    pub const E2BIG: &str = "Argument list too long";
    pub const EBADF: &str = "Bad file descriptor";
    pub const EAGAIN: &str = "Resource temporarily unavailable";
    pub const ENOMEM: &str = "Cannot allocate memory";
    pub const EACCES: &str = "Permission denied";
    pub const EFAULT: &str = "Bad address";
    pub const ENOTBLK: &str = "Block device required";
    pub const EBUSY: &str = "Device or resource busy";
    pub const EEXIST: &str = "File exists";
    pub const EXDEV: &str = "Invalid cross-device link";
    pub const ENODEV: &str = "No such device";
    pub const ENOTDIR: &str = "Not a directory";
    pub const EISDIR: &str = "Is a directory";
    pub const EINVAL: &str = "Invalid argument";
    pub const ENFILE: &str = "Too many open files in system";
    pub const EMFILE: &str = "Too many open files";
    pub const ETXTBSY: &str = "Text file busy";
    pub const EFBIG: &str = "File too large";
    pub const ENOSPC: &str = "No space left on device";
    pub const ESPIPE: &str = "Illegal seek";
    pub const EROFS: &str = "Read-only file system";
    pub const EMLINK: &str = "Too many links";
    pub const EPIPE: &str = "Broken pipe";
    pub const EDOM: &str = "Numerical argument out of domain";
    pub const ERANGE: &str = "Numerical result out of range";
    pub const EDEADLK: &str = "Resource deadlock avoided";
    pub const ENAMETOOLONG: &str = "File name too long";
    pub const ENOLCK: &str = "No locks available";
    pub const ENOSYS: &str = "Function not implemented";
    pub const ENOTEMPTY: &str = "Directory not empty";
    pub const ELOOP: &str = "Too many levels of symbolic links";
    pub const ENOMSG: &str = "No message of desired type";
    pub const EIDRM: &str = "Identifier removed";
    pub const ENODATA: &str = "No data available";
    pub const ENONET: &str = "Machine is not on the network";
    pub const ENOPKG: &str = "Package not installed";
    pub const EREMOTE: &str = "Object is remote";
    pub const ENOLINK: &str = "Link has been severed";
    pub const ECOMM: &str = "Communication error on send";
    pub const EPROTO: &str = "Protocol error";
    pub const EBADMSG: &str = "Bad message";
    pub const EBADFD: &str = "File descriptor in bad state";
    pub const ESTRPIPE: &str = "Streams pipe error";
    pub const EUSERS: &str = "Too many users";
    pub const ENOTSOCK: &str = "Socket operation on non-socket";
    pub const EMSGSIZE: &str = "Message too long";
    pub const ENOPROTOOPT: &str = "Protocol not available";
    pub const EPROTONOSUPPORT: &str = "Protocol not supported";
    pub const ESOCKTNOSUPPORT: &str = "Socket type not supported";
    pub const EOPNOTSUPP: &str = "Operation not supported";
    pub const EPFNOSUPPORT: &str = "Protocol family not supported";
    pub const ENETDOWN: &str = "Network is down";
    pub const ENETUNREACH: &str = "Network is unreachable";
    pub const ENETRESET: &str = "Network dropped connection on reset";
    pub const ECONNABORTED: &str = "Software caused connection abort";
    pub const ECONNRESET: &str = "Connection reset by peer";
    pub const ENOBUFS: &str = "No buffer space available";
    pub const EISCONN: &str = "Transport endpoint is already connected";
    pub const ENOTCONN: &str = "Transport endpoint is not connected";
    pub const ESHUTDOWN: &str = "Cannot send after transport endpoint shutdown";
    pub const ETIMEDOUT: &str = "Connection timed out";
    pub const ECONNREFUSED: &str = "Connection refused";
    pub const EHOSTDOWN: &str = "Host is down";
    pub const EHOSTUNREACH: &str = "No route to host";
    pub const EALREADY: &str = "Operation already in progress";
    pub const EINPROGRESS: &str = "Operation now in progress";
    pub const EISNAM: &str = "Is a named type file";
    pub const EREMOTEIO: &str = "Remote I/O error";
    pub const EDQUOT: &str = "Disk quota exceeded";
    pub const EBADF2: &str = "fid unknown or out of range";
    pub const EACCES2: &str = "permission denied";
    pub const ENOENT_FILE2: &str = "file does not exist";
    pub const ECONNREFUSED2: &str = "authentication failed";
    pub const ESPIPE2: &str = "bad offset in directory read";
    pub const EBADF3: &str = "bad use of fid";
    pub const EPERM_CONV: &str = "wstat can't convert between files and directories";
    pub const ENOTEMPTY2: &str = "directory is not empty";
    pub const EEXIST2: &str = "file exists";
    pub const EEXIST3: &str = "file already exists";
    pub const EEXIST4: &str = "file or directory already exists";
    pub const EBADF4: &str = "fid already in use";
    pub const ETXTBSY2: &str = "file in use";
    pub const EIO2: &str = "i/o error";
    pub const ETXTBSY3: &str = "file already open for I/O";
    pub const EINVAL2: &str = "illegal mode";
    pub const ENAMETOOLONG2: &str = "illegal name";
    pub const ENOTDIR2: &str = "not a directory";
    pub const EPERM_GRP: &str = "not a member of proposed group";
    pub const EACCES3: &str = "not owner";
    pub const EACCES4: &str = "only owner can change group in wstat";
    pub const EROFS2: &str = "read only file system";
    pub const EPERM_SPFILE: &str = "no access to special file";
    pub const EIO3: &str = "i/o count too large";
    pub const EINVAL3: &str = "unknown group";
    pub const EINVAL4: &str = "unknown user";
    pub const EPROTO2: &str = "bogus wstat buffer";
    pub const EAGAIN2: &str = "exclusive use file already open";
    pub const EIO4: &str = "corrupted directory entry";
    pub const EIO5: &str = "corrupted file entry";
    pub const EIO6: &str = "corrupted block label";
    pub const EIO7: &str = "corrupted meta data";
    pub const EINVAL5: &str = "illegal offset";
    pub const ENOENT_PATH: &str = "illegal path element";
    pub const EIO8: &str = "root of file system is corrupted";
    pub const EIO9: &str = "corrupted super block";
    pub const EPROTO3: &str = "protocol botch";
    pub const ENOSPC2: &str = "file system is full";
    pub const EAGAIN3: &str = "file is in use";
    pub const ENOENT_ALLOC: &str = "directory entry is not allocated";
    pub const EROFS3: &str = "file is read only";
    pub const EIDRM2: &str = "file has been removed";
    pub const EPERM_TRUNCATE: &str = "only support truncation to zero length";
    pub const EPERM_RMROOT: &str = "cannot remove root";
    pub const EFBIG2: &str = "file too big";
    pub const EIO10: &str = "venti i/o error";
}
