tonic::include_proto!("cmd");

pub struct Command {
    pub(crate) req: CommandReq,
}
