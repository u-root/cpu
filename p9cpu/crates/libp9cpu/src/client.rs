use crate::cmd;
use crate::cmd::Cmd;
use crate::rpc;
use async_trait::async_trait;
use futures::{Future, Stream, StreamExt};
use nix::sys::termios;
use std::fmt::Debug;
use thiserror::Error;
use tokio::io::{AsyncReadExt, AsyncWrite, AsyncWriteExt};
use tokio::{
    sync::{broadcast, mpsc},
    task::JoinHandle,
};
use tokio_stream::wrappers::ReceiverStream;

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
    async fn start(&self, sid: Self::SessionId, command: Cmd) -> Result<(), Self::Error>;

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

struct SessionInfo<S> {
    sid: S,
    handles: Vec<JoinHandle<Result<(), ClientError>>>,
    stop_tx: broadcast::Sender<()>,
    tty: bool,
}

#[derive(Error, Debug)]
pub enum ClientError {
    #[error("Command not started")]
    NotStarted,
    #[error("Command exits with {0}")]
    NonZeroExitCode(i32),
    #[error("Command already started")]
    AlreadyStarted,
    #[error("IO error: {0}")]
    IoErr(#[from] std::io::Error),
    #[error("System error: {0}")]
    Sys(#[from] nix::errno::Errno),
    #[error("Channel closed")]
    ChannelClosed,
    #[error("Inner: {0}")]
    Inner(#[source] Box<dyn std::error::Error + Send + Sync + 'static>),
}

impl<T> From<mpsc::error::SendError<T>> for ClientError {
    fn from(_: mpsc::error::SendError<T>) -> Self {
        ClientError::ChannelClosed
    }
}

pub struct P9cpuClient<Inner: ClientInnerT> {
    inner: Inner,
    session_info: Option<SessionInfo<Inner::SessionId>>,
}

impl<'a, Inner> P9cpuClient<Inner>
where
    Inner: ClientInnerT,
    ClientError: From<Inner::Error>,
{
    pub async fn new(inner: Inner) -> P9cpuClient<Inner> {
        Self {
            inner,
            session_info: None,
        }
    }

    const STDIN_BUF_SIZE: usize = 128;

    async fn setup_stdio(
        &mut self,
        sid: Inner::SessionId,
        tty: bool,
        mut stop_rx: broadcast::Receiver<()>,
    ) -> Result<Vec<JoinHandle<Result<(), ClientError>>>, Inner::Error> {
        let mut handles = vec![];

        let out_stream = self.inner.stdout(sid.clone()).await?;
        let stdout = tokio::io::stdout();
        let out_handle = Self::copy_stream(out_stream, stdout);
        handles.push(out_handle);

        if !tty {
            let err_stream = self.inner.stderr(sid.clone()).await?;
            let stderr = tokio::io::stderr();
            let err_handle = Self::copy_stream(err_stream, stderr);
            handles.push(err_handle);
        }

        let (tx, rx) = mpsc::channel(1);

        let in_stream = ReceiverStream::new(rx);
        let stdin_future = self.inner.stdin(sid.clone(), in_stream).await;
        let in_handle = tokio::spawn(async move {
            let mut stdin = tokio::io::stdin();
            loop {
                let mut buf = vec![0; Self::STDIN_BUF_SIZE];
                let len = tokio::select! {
                    len = stdin.read(&mut buf) => len,
                    _ = stop_rx.recv() => break,
                }?;
                if len == 0 {
                    break;
                }
                buf.truncate(len);
                tx.send(buf).await?;
            }
            drop(tx);
            stdin_future.await?;
            Ok(())
        });
        handles.push(in_handle);
        Ok(handles)
    }

    fn copy_stream<D>(
        mut src: Inner::ByteVecStream,
        mut dst: D,
    ) -> JoinHandle<Result<(), ClientError>>
    where
        D: AsyncWrite + Unpin + Send + 'static,
    {
        tokio::spawn(async move {
            while let Some(bytes) = src.next().await {
                dst.write_all(&bytes).await?;
                dst.flush().await?;
            }
            Ok(())
        })
    }

    pub async fn start(&mut self, command: cmd::Command) -> Result<(), ClientError> {
        if self.session_info.is_some() {
            return Err(ClientError::AlreadyStarted)?;
        }
        let tty = command.cmd.tty;
        let sid = self.inner.dial().await?;
        if command.cmd.ninep {
            unimplemented!("local 9p server is not implemented yet.")
        }
        self.inner.start(sid.clone(), command.cmd).await?;

        let (stop_tx, stop_rx) = broadcast::channel(1);

        let handles = self.setup_stdio(sid.clone(), tty, stop_rx).await?;

        self.session_info = Some(SessionInfo {
            tty,
            sid,
            handles,
            stop_tx,
        });
        Ok(())
    }

    pub async fn wait_inner(&mut self, sid: Inner::SessionId) -> Result<(), ClientError> {
        let code = self.inner.wait(sid).await?;
        if code == 0 {
            Ok(())
        } else {
            Err(ClientError::NonZeroExitCode(code))
        }
    }

    pub async fn wait(&mut self) -> Result<(), ClientError> {
        let SessionInfo {
            sid,
            handles,
            stop_tx,
            tty,
        } = self.session_info.take().ok_or(ClientError::NotStarted)?;
        let termios_attr = if tty {
            let current = termios::tcgetattr(libc::STDIN_FILENO)?;
            let mut raw = current.clone();
            termios::cfmakeraw(&mut raw);
            termios::tcsetattr(libc::STDIN_FILENO, termios::SetArg::TCSANOW, &raw)?;
            Some(current)
        } else {
            None
        };
        let ret = self.wait_inner(sid).await;
        if stop_tx.send(()).is_err() {
            log::error!("stdin thread is not working");
        }
        for handle in handles {
            match handle.await {
                Err(e) => log::error!("thread join error: {:?}", e),
                Ok(Err(e)) => log::error!("thread error {:?}", e),
                Ok(Ok(())) => {}
            }
        }
        if let Some(current) = termios_attr {
            if let Err(e) =
                termios::tcsetattr(libc::STDIN_FILENO, termios::SetArg::TCSANOW, &current)
            {
                log::error!("restore termios error: {:?}", e);
            }
        }
        ret
    }
}

pub async fn rpc_based(
    addr: crate::Addr,
) -> Result<P9cpuClient<rpc::rpc_client::RpcClient>, ClientError> {
    let inner = rpc::rpc_client::RpcClient::new(addr).await.unwrap();
    let client = P9cpuClient::new(inner).await;
    Ok(client)
}
