mod async_fd;
pub mod client;
pub mod cmd;
mod rpc;
pub mod server;

use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};

#[derive(Debug)]
pub enum Addr {
    Tcp(std::net::SocketAddr),
    Vsock(tokio_vsock::VsockAddr),
    Uds(String),
}

pub(crate) struct TryOrErrInto<F> {
    future: F,
}

impl<F, R, E1, E2> Future for TryOrErrInto<F>
where
    E1: From<E2>,
    F: Future<Output = Result<Result<R, E1>, E2>> + Unpin,
{
    type Output = Result<R, E1>;
    fn poll(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Self::Output> {
        match Pin::new(&mut self.future).poll(cx) {
            Poll::Pending => Poll::Pending,
            Poll::Ready(Ok(r)) => Poll::Ready(r),
            Poll::Ready(Err(e)) => Poll::Ready(Err(E1::from(e))),
        }
    }
}
