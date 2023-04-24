use crate::async_fd::AsyncFd;
use crate::cmd;
use async_trait::async_trait;
use futures::Stream;
use prost::Message;
use std::fmt::{Debug, Display};
use std::hash::Hash;
use std::os::unix::prelude::{FromRawFd, OwnedFd};
use std::{collections::HashMap, sync::Arc};
use thiserror::Error;
use tokio::net::TcpListener;

use tokio::{
    io::{AsyncRead, AsyncReadExt, AsyncWriteExt},
    process::{Child, Command},
    sync::{mpsc, RwLock},
    task::JoinHandle,
};
use tokio_stream::{wrappers::ReceiverStream, StreamExt};

#[async_trait]
pub trait P9cpuServerT {
    type Error: std::error::Error;
    async fn serve<L>(&self, addr: crate::Addr, launcher: L) -> Result<(), Self::Error>
    where
        L: crate::launcher::Launch + Send + Sync + 'static;
}

// enum ChildStdio {
//     Piped(OwnedFd, OwnedFd, OwnedFd),
//     Pty(OwnedFd),
// }

#[derive(Debug)]
pub struct Session {
    stdin: Arc<RwLock<AsyncFd>>,
    stdout: Arc<RwLock<AsyncFd>>,
    stderr: Arc<RwLock<Option<AsyncFd>>>,
    child: Arc<RwLock<Child>>,
    handles: Arc<RwLock<Vec<JoinHandle<Result<(), Error>>>>>,
}

#[derive(Debug)]
pub struct PendingSession {
    ninep: Arc<RwLock<Option<(u16, JoinHandle<Result<(), Error>>)>>>,
}

#[derive(Error, Debug)]
pub enum Error {
    #[error("Failed to spawn: {0}")]
    SpawnFail(#[source] std::io::Error),
    #[error("Session does not exist")]
    SessionNotExist,
    #[error("IO Error: {0}")]
    IoErr(#[source] std::io::Error),
    #[error("Duplicate session id")]
    DuplicateId,
    #[error("Command exited without return code.")]
    NoReturnCode,
    #[error("Command error: {0}")]
    CommandError(#[source] std::io::Error),
    #[error("Cannot open pty device")]
    OpenPtyFail(#[from] nix::Error),
    #[error("Cannot clone file descriptor: {0}")]
    FdCloneFail(#[source] std::io::Error),
    #[error("Cannot create directory: {0}")]
    MkDir(#[source] std::io::Error),
    #[error("Invalid FsTab: {0}")]
    InvalidFsTab(String),
    #[error("Cannot bind listener: {0}")]
    BindFail(#[source] std::io::Error),
    #[error("9p forward not setup")]
    No9pPort,
    #[error("String contains null: {0:?}")]
    StringContainsNull(#[from] std::ffi::NulError),
    #[error("Channel closed")]
    ChannelClosed,
}

impl<T> From<mpsc::error::SendError<T>> for Error {
    fn from(_: mpsc::error::SendError<T>) -> Self {
        Self::ChannelClosed
    }
}

#[derive(Debug)]
pub struct Server<I, L> {
    launcher: L,
    sessions: Arc<RwLock<HashMap<I, Session>>>,
    pending: Arc<RwLock<HashMap<I, PendingSession>>>,
}

impl<I, L> Server<I, L> {
    pub fn new(launcher: L) -> Self {
        Self {
            sessions: Arc::new(RwLock::new(HashMap::new())),
            pending: Arc::new(RwLock::new(HashMap::new())),
            launcher,
        }
    }
}

impl<I, L> Server<I, L>
where
    L: crate::launcher::Launch,
    I: Eq + Hash + Debug + Display + Sync + Send + Clone + 'static,
{
    async fn get_session<O, R>(&self, sid: &I, op: O) -> Result<R, Error>
    where
        O: Fn(&Session) -> R,
    {
        let sessions = self.sessions.read().await;
        let info = sessions.get(sid).ok_or(Error::SessionNotExist)?;
        Ok(op(info))
    }

    async fn copy_to(
        src: &mut (impl AsyncRead + Unpin),
        tx: mpsc::Sender<Vec<u8>>,
    ) -> Result<(), Error> {
        loop {
            let mut buf = vec![0; 128];
            let len = src.read(&mut buf).await.map_err(Error::IoErr)?;
            if len == 0 {
                break;
            }
            buf.truncate(len);
            tx.send(buf).await?;
        }
        Ok(())
    }

    pub async fn start(&self, cmd: cmd::Cmd, sid: I) -> Result<(), Error> {
        let Some(PendingSession { ninep }) = self.pending.write().await.remove(&sid) else {
            return Err(Error::SessionNotExist);
        };
        let mut sessions = self.sessions.write().await;
        if sessions.contains_key(&sid) {
            return Err(Error::DuplicateId);
        }
        let mut handles = vec![];
        let mut ninep_port = None;
        if let Some((port, handle)) = ninep.write().await.take() {
            ninep_port = Some(port);
            handles.push(handle);
        }

        let listener = TcpListener::bind(std::net::SocketAddrV4::new(
            std::net::Ipv4Addr::LOCALHOST,
            0,
        ))
        .await
        .map_err(Error::BindFail)?;
        let port = listener.local_addr().map_err(Error::BindFail)?.port();

        let command = self.launcher.launch(port);
        let mut command = Command::from(command);
        if cmd.ninep {
            let Some(_ninep_port) = ninep_port else {
                    return Err(Error::No9pPort);
            };
        }

        let (stdin, stdout, stderr) = if cmd.tty {
            let result = nix::pty::openpty(None, None).map_err(Error::OpenPtyFail)?;
            let stdin = unsafe { OwnedFd::from_raw_fd(result.slave) };
            let stdout = stdin.try_clone().map_err(Error::FdCloneFail)?;
            let stderr = stdin.try_clone().map_err(Error::FdCloneFail)?;
            command.stdin(stdin).stdout(stdout).stderr(stderr);
            let master = unsafe { OwnedFd::from_raw_fd(result.master) };
            let master_copy = master.try_clone().map_err(Error::FdCloneFail)?;
            (
                AsyncFd::try_from(master).map_err(Error::IoErr)?,
                AsyncFd::try_from(master_copy).map_err(Error::IoErr)?,
                None,
            )
        } else {
            let (stdin_rd, stdin_wr) = nix::unistd::pipe2(nix::fcntl::OFlag::O_CLOEXEC)?;
            let (stdout_rd, stdout_wr) = nix::unistd::pipe2(nix::fcntl::OFlag::O_CLOEXEC)?;
            let (stderr_rd, stderr_wr) = nix::unistd::pipe2(nix::fcntl::OFlag::O_CLOEXEC)?;
            let stdin = unsafe { OwnedFd::from_raw_fd(stdin_rd) };
            let stdout = unsafe { OwnedFd::from_raw_fd(stdout_wr) };
            let stderr = unsafe { OwnedFd::from_raw_fd(stderr_wr) };
            command.stdin(stdin).stdout(stdout).stderr(stderr);
            (
                AsyncFd::try_from(unsafe { OwnedFd::from_raw_fd(stdin_wr) })
                    .map_err(Error::IoErr)?,
                AsyncFd::try_from(unsafe { OwnedFd::from_raw_fd(stdout_rd) })
                    .map_err(Error::IoErr)?,
                Some(
                    AsyncFd::try_from(unsafe { OwnedFd::from_raw_fd(stderr_rd) })
                        .map_err(Error::IoErr)?,
                ),
            )
        };

        let child = command.spawn().map_err(Error::SpawnFail)?;

        let (mut stream, _) = listener.accept().await.map_err(Error::IoErr)?;
        let buf = cmd.encode_to_vec();
        let size_buf = buf.len().to_le_bytes();
        stream.write_all(&size_buf).await.map_err(Error::IoErr)?;
        stream.write_all(&buf).await.map_err(Error::IoErr)?;
        stream.shutdown().await.map_err(Error::IoErr)?;
        drop(stream);
        drop(listener);

        let info = Session {
            stdin: Arc::new(RwLock::new(stdin)),
            stdout: Arc::new(RwLock::new(stdout)),
            stderr: Arc::new(RwLock::new(stderr)),
            child: Arc::new(RwLock::new(child)),
            handles: Arc::new(RwLock::new(handles)),
        };
        log::info!("Session {} started", &sid);
        sessions.insert(sid, info);
        Ok(())
    }

    pub async fn stdin(
        &self,
        sid: &I,
        mut in_stream: impl Stream<Item = Vec<u8>> + Unpin,
    ) -> Result<(), Error> {
        let cmd_stdin = self.get_session(sid, |s| s.stdin.clone()).await?;
        let mut cmd_stdin = cmd_stdin.write().await;
        log::debug!("Session {} stdin stream started", sid);
        while let Some(item) = in_stream.next().await {
            cmd_stdin.write_all(&item).await.map_err(Error::IoErr)?;
        }
        Ok(())
    }

    pub async fn stdout(&self, sid: &I) -> Result<impl Stream<Item = Vec<u8>>, Error> {
        let cmd_stdout = self.get_session(sid, |s| s.stdout.clone()).await?;
        let (tx, rx) = mpsc::channel(10);
        let sid_copy = sid.clone();
        let out_handle = tokio::spawn(async move {
            let mut out = cmd_stdout.write().await;
            log::debug!("Session {} stdout stream started", &sid_copy);
            Self::copy_to(&mut *out, tx).await
        });
        let handles = self.get_session(sid, |s| s.handles.clone()).await?;
        handles.write().await.push(out_handle);
        let stream = ReceiverStream::new(rx);
        Ok(stream)
    }

    pub async fn stderr(&self, sid: &I) -> Result<impl Stream<Item = Vec<u8>>, Error> {
        let cmd_stderr = self.get_session(sid, |s| s.stderr.clone()).await?;
        let (tx, rx) = mpsc::channel(10);
        let sid_copy = sid.clone();
        let err_handle = tokio::spawn(async move {
            let mut err = cmd_stderr.write().await;
            let Some(ref mut err) = &mut *err else {
                log::info!("Session {} has no stderr", &sid_copy);
                return Ok(());
            };
            log::debug!("Session {} stderr stream started", &sid_copy);
            Self::copy_to(&mut *err, tx).await
        });
        let handles = self.get_session(sid, |s| s.handles.clone()).await?;
        handles.write().await.push(err_handle);
        let stream = ReceiverStream::new(rx);
        Ok(stream)
    }

    pub async fn dial(&self, sid: I) -> Result<(), Error> {
        let mut pending = self.pending.write().await;
        if pending.contains_key(&sid) {
            return Err(Error::DuplicateId);
        }
        let session = PendingSession {
            ninep: Arc::new(RwLock::new(None)),
        };
        log::info!("Session {} created", &sid);
        pending.insert(sid, session);
        Ok(())
    }

    pub async fn wait(&self, sid: &I) -> Result<i32, Error> {
        let child = self.get_session(sid, |s| s.child.clone()).await?;
        let ret = match child.write().await.wait().await {
            Ok(status) => status.code().ok_or(Error::NoReturnCode),
            Err(e) => Err(Error::CommandError(e)),
        };
        println!("child is done");
        let handles = self.get_session(sid, |s| s.handles.clone()).await?;
        for handle in handles.write().await.iter_mut() {
            if let Err(e) = handle.await {
                eprintln!("handle join error {:?}", e);
            }
        }
        self.sessions.write().await.remove(sid);
        log::info!("Session {} is done", &sid);
        ret
    }

    pub async fn ninep_forward(
        &self,
        _sid: &I,
        mut _in_stream: impl Stream<Item = Vec<u8>> + Unpin + Send + 'static,
    ) -> Result<impl Stream<Item = Vec<u8>>, Error> {
        // Not implemented yet
        let (_tx, rx) = mpsc::channel(10);
        let stream = ReceiverStream::new(rx);
        Ok(stream)
    }
}

pub fn rpc_based() -> crate::rpc::rpc_server::RpcServer {
    crate::rpc::rpc_server::RpcServer {}
}
