use crate::cmd;
use async_trait::async_trait;
use futures::Stream;
use thiserror::Error;
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;

#[async_trait]
pub trait P9cpuServerT {
    type Error: std::error::Error;
    async fn serve(&self, addr: crate::Addr) -> Result<(), Self::Error>;
}

#[derive(Error, Debug)]
pub enum Error {}

#[derive(Debug)]
pub struct Server<I> {
    _phantom: Option<I>,
}

impl<I> Default for Server<I> {
    fn default() -> Self {
        Self { _phantom: None }
    }
}

impl<I> Server<I> {
    pub async fn start(&self, _command: cmd::CommandReq, _sid: I) -> Result<(), Error> {
        unimplemented!()
    }

    pub async fn stdin(
        &self,
        _sid: &I,
        mut _in_stream: impl Stream<Item = Vec<u8>> + Unpin,
    ) -> Result<(), Error> {
        unimplemented!()
    }

    pub async fn stdout(&self, _sid: &I) -> Result<impl Stream<Item = Vec<u8>>, Error> {
        // Not implemented yet
        let (_tx, rx) = mpsc::channel(10);
        let stream = ReceiverStream::new(rx);
        Ok(stream)
    }

    pub async fn stderr(&self, _sid: &I) -> Result<impl Stream<Item = Vec<u8>>, Error> {
        // Not implemented yet
        let (_tx, rx) = mpsc::channel(10);
        let stream = ReceiverStream::new(rx);
        Ok(stream)
    }

    pub async fn dial(&self, _sid: I) -> Result<(), Error> {
        unimplemented!()
    }

    pub async fn wait(&self, _sid: &I) -> Result<i32, Error> {
        unimplemented!()
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
