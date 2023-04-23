mod async_fd;
pub mod client;
pub mod cmd;
mod rpc;
pub mod server;

use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};
use std::collections::HashSet;
use log::warn;

#[derive(Debug)]
pub enum Addr {
    Tcp(std::net::SocketAddr),
    Vsock(tokio_vsock::VsockAddr),
    Uds(String),
}

pub struct TryOrErrInto<F> {
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

pub const NINEP_MOUNT: &str = "mnt9p";
pub const LOCAL_ROOT_MOUNT: &str = "local";

pub fn parse_namespace(namespace: &str, tmp_mnt: &str) -> Vec<cmd::FsTab> {
    let mut result = vec![];
    if namespace.is_empty() {
        return result;
    }
    let mut targets = HashSet::new();
    for part in namespace.split(':') {
        let mut iter = part.split('=');
        let Some(target) = iter.next() else {
                    warn!("invalid namespace: {}", part);
                    continue;
                };
        let source = iter.next().unwrap_or(target);
        if iter.next().is_some() {
            warn!("invalid namespace: {}", part);
            continue;
        }
        if targets.contains(target) {
            warn!("duplicate target: {}", target);
            continue;
        }
        targets.insert(target);
        result.push(cmd::FsTab {
            spec: format!("{}/{}{}", tmp_mnt, NINEP_MOUNT, source),
            file: target.to_owned(),
            vfstype: "none".to_owned(),
            mntops: "defaults,bind".to_owned(),
            freq: 0,
            passno: 0,
        });
    }
    result
}