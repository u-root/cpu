use crate::{cmd, rpc, TryOrErrInto};
use async_trait::async_trait;
use futures::{Stream, StreamExt};
use std::pin::Pin;
use std::task::{Context, Poll};
use thiserror::Error;
use tokio::task::JoinHandle;
use tonic::{transport::Channel, Status, Streaming};

pub(crate) struct ByteVecStream<I> {
    inner: I,
    session: uuid::Uuid,
    name: &'static str,
}

impl<Inner, B> Stream for ByteVecStream<Inner>
where
    Inner: Stream<Item = Result<B, Status>> + Unpin,
    Vec<u8>: From<B>,
{
    type Item = Vec<u8>;
    fn poll_next(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Option<Self::Item>> {
        match self.inner.poll_next_unpin(cx) {
            Poll::Pending => Poll::Pending,
            Poll::Ready(None) => Poll::Ready(None),
            Poll::Ready(Some(Ok(b))) => Poll::Ready(Some(b.into())),
            Poll::Ready(Some(Err(e))) => {
                log::error!("Session {}: {}: {}", self.session, self.name, e);
                Poll::Ready(None)
            }
        }
    }
}

#[derive(Error, Debug)]
pub(crate) enum RpcError {
    #[error("RPC error: {0}")]
    Rpc(#[from] Status),
    #[error("Invalid UUID: {0}")]
    InvalidUuid(#[from] uuid::Error),
    #[error("Task join error: {0}")]
    JoinError(#[from] tokio::task::JoinError),
}

pub(crate) struct RpcClient {
    channel: Channel,
}

#[async_trait]
impl crate::client::ClientInnerT for RpcClient {
    type Error = RpcError;
    type SessionId = uuid::Uuid;

    async fn dial(&self) -> Result<Self::SessionId, Self::Error> {
        let mut client = rpc::p9cpu_client::P9cpuClient::new(self.channel.clone());
        let id_vec = client.dial(rpc::Empty {}).await?.into_inner().id;
        let sid = uuid::Uuid::from_slice(&id_vec)?;
        Ok(sid)
    }

    async fn start(
        &self,
        sid: Self::SessionId,
        command: cmd::CommandReq,
    ) -> Result<(), Self::Error> {
        let req = rpc::StartRequest {
            id: sid.into_bytes().into(),
            cmd: Some(command),
        };
        let mut client = rpc::p9cpu_client::P9cpuClient::new(self.channel.clone());
        client.start(req).await?.into_inner();
        Ok(())
    }

    type EmptyFuture = TryOrErrInto<JoinHandle<Result<(), Self::Error>>>;

    type ByteVecStream = ByteVecStream<Streaming<rpc::Bytes>>;
    async fn stdout(&self, sid: Self::SessionId) -> Result<Self::ByteVecStream, Self::Error> {
        let request = rpc::SessionId {
            id: sid.into_bytes().into(),
        };
        let mut client = rpc::p9cpu_client::P9cpuClient::new(self.channel.clone());
        let out_stream = client.stdout(request).await?.into_inner();
        Ok(ByteVecStream {
            inner: out_stream,
            name: "stdout",
            session: sid,
        })
    }

    async fn stderr(&self, sid: Self::SessionId) -> Result<Self::ByteVecStream, Self::Error> {
        let request = rpc::SessionId {
            id: sid.into_bytes().into(),
        };
        let mut client = rpc::p9cpu_client::P9cpuClient::new(self.channel.clone());
        let err_stream = client.stderr(request).await?.into_inner();
        Ok(ByteVecStream {
            inner: err_stream,
            name: "stderr",
            session: sid,
        })
    }

    #[allow(clippy::async_yields_async)]
    async fn stdin(
        &self,
        sid: Self::SessionId,
        mut stream: impl Stream<Item = Vec<u8>> + Send + Sync + 'static + Unpin,
    ) -> Self::EmptyFuture {
        let channel = self.channel.clone();
        let handle = tokio::spawn(async move {
            let Some(first_vec) = stream.next().await else {
                return Ok(());
            };
            let first_req = rpc::StdinRequest {
                id: Some(sid.into_bytes().into()),
                data: first_vec,
            };
            let req_stream = stream.map(|data| rpc::StdinRequest { id: None, data });
            let stream = rpc::PrependedStream::new(req_stream, first_req);
            let mut stdin_client = rpc::p9cpu_client::P9cpuClient::new(channel);
            stdin_client.stdin(stream).await?;
            Ok(())
        });
        TryOrErrInto { future: handle }
    }

    async fn ninep_forward(
        &self,
        sid: Self::SessionId,
        in_stream: impl Stream<Item = Vec<u8>> + Send + Sync + 'static + Unpin,
    ) -> Result<Self::ByteVecStream, Self::Error> {
        let first_req = crate::rpc::NinepForwardRequest {
            id: Some(sid.into_bytes().into()),
            data: vec![],
        };
        let req_stream = in_stream.map(|data| rpc::NinepForwardRequest { data, id: None });
        let stream = rpc::PrependedStream::new(req_stream, first_req);
        let mut client = rpc::p9cpu_client::P9cpuClient::new(self.channel.clone());
        let out_stream = client.ninep_forward(stream).await.unwrap().into_inner();
        Ok(ByteVecStream {
            inner: out_stream,
            name: "9p out",
            session: sid,
        })
    }

    async fn wait(&self, sid: Self::SessionId) -> Result<i32, Self::Error> {
        let req = rpc::SessionId {
            id: sid.into_bytes().to_vec(),
        };
        let mut client = rpc::p9cpu_client::P9cpuClient::new(self.channel.clone());
        let resp = client.wait(req).await?;

        Ok(resp.into_inner().code)
    }
}
