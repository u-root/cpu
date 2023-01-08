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

    /// The ID for one session, which corresponds to one command running on a
    /// remote host.
    type SessionId: Clone + Debug + Sync + Send + 'static;

    /// Talks to the remote machine and prepares for running a command.
    async fn dial(&self) -> Result<Self::SessionId, Self::Error>;

    /// Starts the command on the remote machine. This method should return as
    /// long as the command is spawned successfully. To obtained the command
    /// return code, call [wait()](Self::wait).
    async fn start(&self, sid: Self::SessionId, command: CommandReq) -> Result<(), Self::Error>;

    type EmptyFuture: Future<Output = Result<(), Self::Error>> + Send + 'static;

    type ByteVecStream: Stream<Item = Vec<u8>> + Unpin + Send + 'static;

    /// Returns the stdout byte stream of a remote command. Callers `.await` the
    /// stream to get the actual stdout bytes.
    async fn stdout(&self, sid: Self::SessionId) -> Result<Self::ByteVecStream, Self::Error>;

    /// Returns the stderr byte stream of a remote command. Callers `.await` the
    /// stream to get the actual stderr bytes.
    async fn stderr(&self, sid: Self::SessionId) -> Result<Self::ByteVecStream, Self::Error>;

    /// Accepts a stream and writes the stream contents to the remote command's
    /// stdin. Callers need to `.await` the returned [Future] to check if any
    /// error happens.
    async fn stdin(
        &self,
        sid: Self::SessionId,
        stream: impl Stream<Item = Vec<u8>> + Send + Sync + 'static + Unpin,
    ) -> Self::EmptyFuture;

    /// Forwards 9p requests from the remote machine to the local machine.
    /// The returned [ByteVecStream](Self::ByteVecStream) contains 9p requests
    /// from the remote machine and the input `stream` should contain 9p
    /// responses from a local 9p server.
    async fn ninep_forward(
        &self,
        sid: Self::SessionId,
        stream: impl Stream<Item = Vec<u8>> + Send + Sync + 'static + Unpin,
    ) -> Result<Self::ByteVecStream, Self::Error>;

    async fn wait(&self, sid: Self::SessionId) -> Result<i32, Self::Error>;
}
