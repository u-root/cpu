use async_trait::async_trait;

#[async_trait]
pub trait P9cpuServerT {
    type Error: std::error::Error;
    async fn serve(&self, addr: crate::Addr) -> Result<(), Self::Error>;
}
