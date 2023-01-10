pub mod rpc_client;

use futures::Stream;

tonic::include_proto!("p9cpu");

impl From<Bytes> for Vec<u8> {
    fn from(b: Bytes) -> Self {
        b.data
    }
}

/// A [Stream] with an extra item prepended to its head.
struct PrependedStream<I, S> {
    stream: S,
    item: Option<I>,
}

impl<I, S> Stream for PrependedStream<I, S>
where
    S: Stream<Item = I> + Unpin,
    I: Unpin,
{
    type Item = I;

    fn poll_next(
        mut self: std::pin::Pin<&mut Self>,
        cx: &mut std::task::Context<'_>,
    ) -> std::task::Poll<Option<Self::Item>> {
        if let Some(item) = self.as_mut().item.take() {
            std::task::Poll::Ready(Some(item))
        } else {
            S::poll_next(std::pin::Pin::new(&mut self.get_mut().stream), cx)
        }
    }
}

impl<I, S> PrependedStream<I, S> {
    /// Generates a new stream by prepending an extra item to an existing [Stream].
    pub fn new(stream: S, item: I) -> Self {
        PrependedStream {
            stream,
            item: Some(item),
        }
    }
}
