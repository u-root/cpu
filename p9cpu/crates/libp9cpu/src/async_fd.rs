use futures::ready;
use std::{
    fmt::Debug,
    io::ErrorKind,
    os::unix::prelude::{AsRawFd, OwnedFd},
    pin::Pin,
    task::{Context, Poll},
};
use tokio::io::{AsyncRead, AsyncWrite};

#[derive(Debug)]
pub(crate) struct AsyncFd {
    fd: tokio::io::unix::AsyncFd<OwnedFd>,
}

impl TryFrom<OwnedFd> for AsyncFd {
    type Error = std::io::Error;

    fn try_from(value: OwnedFd) -> Result<Self, Self::Error> {
        let flags = unsafe { libc::fcntl(value.as_raw_fd(), libc::F_GETFL) };
        if flags < 0 {
            return Err(std::io::Error::last_os_error());
        }

        let ret =
            unsafe { libc::fcntl(value.as_raw_fd(), libc::F_SETFL, flags | libc::O_NONBLOCK) };
        if ret < 0 {
            return Err(std::io::Error::last_os_error());
        }
        let fd = tokio::io::unix::AsyncFd::new(value)?;
        Ok(Self { fd })
    }
}

impl AsyncRead for AsyncFd {
    fn poll_read(
        self: Pin<&mut Self>,
        cx: &mut Context<'_>,
        buf: &mut tokio::io::ReadBuf<'_>,
    ) -> Poll<std::io::Result<()>> {
        loop {
            let mut guard = ready!(self.fd.poll_read_ready(cx))?;
            let ret = unsafe {
                libc::read(
                    self.fd.as_raw_fd(),
                    buf.unfilled_mut() as *mut _ as _,
                    buf.remaining(),
                )
            };
            if ret < 0 {
                let e = std::io::Error::last_os_error();
                if e.kind() == ErrorKind::WouldBlock {
                    guard.clear_ready();
                    continue;
                } else {
                    return Poll::Ready(Err(e));
                }
            } else {
                let n = ret as usize;
                unsafe { buf.assume_init(n) };
                buf.advance(n);
                return Poll::Ready(Ok(()));
            }
        }
    }
}

impl AsyncWrite for AsyncFd {
    fn poll_write(
        self: Pin<&mut Self>,
        cx: &mut Context<'_>,
        buf: &[u8],
    ) -> Poll<Result<usize, std::io::Error>> {
        loop {
            let mut guard = ready!(self.fd.poll_write_ready(cx))?;
            let ret = unsafe { libc::write(self.fd.as_raw_fd(), buf as *const _ as _, buf.len()) };
            if ret < 0 {
                let e = std::io::Error::last_os_error();
                if e.kind() == ErrorKind::WouldBlock {
                    guard.clear_ready();
                    continue;
                } else {
                    return Poll::Ready(Err(e));
                }
            } else {
                return Poll::Ready(Ok(ret as usize));
            }
        }
    }

    fn poll_flush(self: Pin<&mut Self>, _cx: &mut Context<'_>) -> Poll<Result<(), std::io::Error>> {
        Poll::Ready(Ok(()))
    }

    fn poll_shutdown(
        self: Pin<&mut Self>,
        _cx: &mut Context<'_>,
    ) -> Poll<Result<(), std::io::Error>> {
        Poll::Ready(Ok(()))
    }
}
