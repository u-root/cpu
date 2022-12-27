use crate::cmd::CommandReq;
use async_trait::async_trait;
use futures::{Future, Stream};
use std::fmt::Debug;

/// A transport-layer client.
///
/// This trait defines a client that handles the data transfer between the local
/// and the remote machine. [RpcInner](crate::rpc::rpc_client::RpcInner) is an
/// implementation based on gRPC.
#[async_trait]
pub trait ClientInnerT {
    type Error: std::error::Error + Sync + Send + 'static;
    type SessionId: Clone + Debug + Sync + Send + 'static;

    async fn dial(&mut self) -> Result<Self::SessionId, Self::Error>;

    async fn start(&mut self, sid: Self::SessionId, command: CommandReq)
        -> Result<(), Self::Error>;

    type EmptyFuture: Future<Output = Result<(), Self::Error>> + Send + 'static;

    type ByteVecStream: Stream<Item = Vec<u8>> + Unpin + Send + 'static;
    async fn stdout(&mut self, sid: Self::SessionId) -> Result<Self::ByteVecStream, Self::Error>;
    async fn stderr(&mut self, sid: Self::SessionId) -> Result<Self::ByteVecStream, Self::Error>;

    async fn stdin(
        &mut self,
        sid: Self::SessionId,
        stream: impl Stream<Item = Vec<u8>> + Send + Sync + 'static + Unpin,
    ) -> Self::EmptyFuture;

    async fn ninep_forward(
        &mut self,
        sid: Self::SessionId,
        stream: impl Stream<Item = Vec<u8>> + Send + Sync + 'static + Unpin,
    ) -> Result<Self::ByteVecStream, Self::Error>;

    async fn wait(&mut self, sid: Self::SessionId) -> Result<i32, Self::Error>;
}
