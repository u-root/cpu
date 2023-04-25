use crate::error;

pub type Result<T> = ::std::result::Result<T, error::Error>;

macro_rules! io_err {
    ($kind:ident, $msg:expr) => {
        ::std::io::Error::new(::std::io::ErrorKind::$kind, $msg)
    };
}

macro_rules! res {
    ($err:expr) => {
        Err(From::from($err))
    };
}

pub fn parse_proto(arg: &str) -> Option<(&str, String)> {
    let mut split = arg.split('!');
    let (proto, addr, port) = (split.next()?, split.next()?, split.next()?);

    Some((proto, addr.to_owned() + ":" + port))
}
