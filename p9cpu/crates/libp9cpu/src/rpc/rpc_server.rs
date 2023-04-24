use super::{
    Bytes, Code, Empty, NinepForwardRequest, PrependedStream, SessionId, StartRequest, StdinRequest,
};
use crate::rpc;
use crate::server;
use crate::Addr;
use async_trait::async_trait;
use futures::stream::{Stream, StreamExt};
use std::pin::Pin;
use thiserror::Error;
use tonic::{Request, Response, Status, Streaming};
use tokio::net::UnixListener;
use tokio_stream::wrappers::UnixListenerStream;
use tokio_vsock::{VsockListener, VsockStream};
use std::task::Poll;

struct VsockListenerStream {
    listener: VsockListener,
}

impl Stream for VsockListenerStream {
    type Item = std::io::Result<VsockStream>;

    fn poll_next(
        mut self: Pin<&mut Self>,
        cx: &mut std::task::Context<'_>,
    ) -> std::task::Poll<Option<Self::Item>> {
        match self.listener.poll_accept(cx) {
            Poll::Ready(Ok((stream, _))) => Poll::Ready(Some(Ok(stream))),
            Poll::Ready(Err(err)) => Poll::Ready(Some(Err(err))),
            Poll::Pending => Poll::Pending,
        }
    }
}


#[derive(Error, Debug)]
pub enum Error {
    #[error("RPC error: {0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}

pub struct RpcServer {}

#[async_trait]
impl crate::server::P9cpuServerT for RpcServer {
    type Error = Error;
    async fn serve(&self, addr: Addr) -> Result<(), Error> {
        let p9cpu_service = rpc::p9cpu_server::P9cpuServer::new(P9cpuService::default());
        let router = tonic::transport::Server::builder().add_service(p9cpu_service);
        match addr {
            Addr::Tcp(addr) => router.serve(addr).await?,
            Addr::Uds(addr) => {
                let uds = UnixListener::bind(addr)?;
                let stream = UnixListenerStream::new(uds);
                router.serve_with_incoming(stream).await?
            }
            Addr::Vsock(addr) => {
                let listener = VsockListener::bind(addr.cid(), addr.port())?;
                let stream = VsockListenerStream { listener };
                router.serve_with_incoming(stream).await?
            }
        }
        Ok(())
    }
}

type RpcResult<T> = Result<Response<T>, Status>;

impl From<server::Error> for Status {
    fn from(e: server::Error) -> Self {
        tonic::Status::internal(e.to_string())
    }
}

fn vec_to_uuid(v: &Vec<u8>) -> Result<uuid::Uuid, Status> {
    uuid::Uuid::from_slice(v).map_err(|e| Status::invalid_argument(e.to_string()))
}

#[derive(Debug, Default)]
pub struct P9cpuService {
    server: server::Server<uuid::Uuid>,
}

#[async_trait]
impl rpc::p9cpu_server::P9cpu for P9cpuService {
    type StdoutStream = Pin<Box<dyn Stream<Item = Result<rpc::Bytes, Status>> + Send>>;
    type StderrStream = Pin<Box<dyn Stream<Item = Result<rpc::Bytes, Status>> + Send>>;
    type NinepForwardStream = Pin<Box<dyn Stream<Item = Result<rpc::Bytes, Status>> + Send>>;

    async fn start(&self, request: Request<rpc::StartRequest>) -> RpcResult<rpc::Empty> {
        let StartRequest { id, cmd: Some(cmd) } = request.into_inner() else {
            return Err(Status::invalid_argument("No cmd provided."));
        };
        let sid = vec_to_uuid(&id)?;
        // let Some(cmd) = request.
        self.server.start(cmd, sid).await?;
        Ok(Response::new(Empty {}))
    }

    async fn stdin(&self, request: Request<Streaming<rpc::StdinRequest>>) -> RpcResult<rpc::Empty> {
        let mut in_stream = request.into_inner();
        let Some(Ok(StdinRequest { id: Some(id), data })) = in_stream.next().await else {
            return Err(Status::invalid_argument("no session id."));
        };
        let sid = vec_to_uuid(&id)?;
        let byte_stream = in_stream.scan((), |_s, req| match req {
            Ok(r) => futures::future::ready(Some(r.data)),
            Err(e) => {
                log::error!("Session {} stdin stream error: {:?}", sid, e);
                futures::future::ready(None)
            }
        });
        let stream = PrependedStream {
            stream: byte_stream,
            item: Some(data),
        };
        self.server.stdin(&sid, stream).await?;
        Ok(Response::new(Empty {}))
    }

    async fn stdout(&self, request: Request<rpc::SessionId>) -> RpcResult<Self::StdoutStream> {
        let sid = vec_to_uuid(&request.into_inner().id)?;
        let stream = self.server.stdout(&sid).await?;
        let out_stream = stream.map(|data| Ok(Bytes { data }));
        Ok(Response::new(Box::pin(out_stream) as Self::StdoutStream))
    }

    async fn stderr(&self, request: Request<rpc::SessionId>) -> RpcResult<Self::StderrStream> {
        let sid = vec_to_uuid(&request.into_inner().id)?;
        let stream = self.server.stderr(&sid).await?;
        let err_stream = stream.map(|data| Ok(Bytes { data }));
        Ok(Response::new(Box::pin(err_stream) as Self::StderrStream))
    }

    async fn dial(&self, _: Request<rpc::Empty>) -> RpcResult<rpc::SessionId> {
        let sid = uuid::Uuid::new_v4();
        self.server.dial(sid).await?;
        let r = SessionId {
            id: sid.into_bytes().into(),
        };
        Ok(Response::new(r))
    }

    async fn ninep_forward(
        &self,
        request: Request<Streaming<rpc::NinepForwardRequest>>,
    ) -> RpcResult<Self::NinepForwardStream> {
        let mut in_stream = request.into_inner();
        let Some(Ok(NinepForwardRequest { id: Some(id), data: _ })) = in_stream.next().await else {
            return Err(Status::invalid_argument("no session id."));
        };
        let sid = vec_to_uuid(&id)?;
        let byte_stream = in_stream.scan((), move |_s, req| match req {
            Ok(r) => futures::future::ready(Some(r.data)),
            Err(e) => {
                log::error!("Session {} stdin stream error: {:?}", sid, e);
                futures::future::ready(None)
            }
        });
        let out_stream = self.server.ninep_forward(&sid, byte_stream).await?;
        let result_stream = out_stream.map(|data| Ok(Bytes { data }));
        Ok(Response::new(
            Box::pin(result_stream) as Self::NinepForwardStream
        ))
    }

    async fn wait(&self, request: Request<rpc::SessionId>) -> RpcResult<rpc::Code> {
        let sid = vec_to_uuid(&request.into_inner().id)?;
        let code = self.server.wait(&sid).await?;
        Ok(Response::new(Code { code }))
    }
}
