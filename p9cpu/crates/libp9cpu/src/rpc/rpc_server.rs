use crate::rpc;
use crate::Addr;
use async_trait::async_trait;
use futures::stream::Stream;
use std::pin::Pin;
use thiserror::Error;
use tonic::transport::Server;
use tonic::{Request, Response, Status, Streaming};

#[derive(Error, Debug)]
pub enum RpcServerError {
    #[error("RPC error: {0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}

pub struct RpcServer {}

#[async_trait]
impl crate::server::P9cpuServerT for RpcServer {
    type Error = RpcServerError;
    async fn serve(&self, addr: Addr) -> Result<(), RpcServerError> {
        
        let p9cpu_service = rpc::p9cpu_server::P9cpuServer::new(P9cpuService::default());
        let router = Server::builder().add_service(p9cpu_service);
        match addr {
            Addr::Tcp(addr) => router.serve(addr).await?,
            Addr::Uds(_addr) => {
                unimplemented!()
            }
            Addr::Vsock(_addr) => {
                unimplemented!()
            }
        }
        Ok(())
    }
}

type RpcResult<T> = Result<Response<T>, Status>;

#[derive(Debug, Default)]
pub struct P9cpuService {}

#[async_trait]
impl rpc::p9cpu_server::P9cpu for P9cpuService {
    type StdoutStream = Pin<Box<dyn Stream<Item = Result<rpc::Bytes, Status>> + Send>>;
    type StderrStream = Pin<Box<dyn Stream<Item = Result<rpc::Bytes, Status>> + Send>>;
    type NinepForwardStream = Pin<Box<dyn Stream<Item = Result<rpc::Bytes, Status>> + Send>>;

    async fn start(&self, _request: Request<rpc::StartRequest>) -> RpcResult<rpc::Empty> {
        unimplemented!()
    }

    async fn stdin(
        &self,
        _request: Request<Streaming<rpc::StdinRequest>>,
    ) -> RpcResult<rpc::Empty> {
        unimplemented!()
    }

    async fn stdout(&self, _request: Request<rpc::SessionId>) -> RpcResult<Self::StdoutStream> {
        unimplemented!()
    }

    async fn stderr(&self, _request: Request<rpc::SessionId>) -> RpcResult<Self::StderrStream> {
        unimplemented!()
    }

    async fn dial(&self, _: Request<rpc::Empty>) -> RpcResult<rpc::SessionId> {
        unimplemented!()
    }

    async fn ninep_forward(
        &self,
        _request: Request<Streaming<rpc::NinepForwardRequest>>,
    ) -> RpcResult<Self::NinepForwardStream> {
        unimplemented!()
    }

    async fn wait(&self, _request: Request<rpc::SessionId>) -> RpcResult<rpc::Code> {
        unimplemented!()
    }
}
