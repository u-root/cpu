//! Asynchronous server side 9P library.
//!
//! # Protocol
//! 9P2000.L

use {
    crate::{
        error,
        error::errno::*,
        fcall::*,
        serialize,
        utils::{self, Result},
    },
    async_trait::async_trait,
    bytes::buf::{Buf, BufMut},
    futures::sink::SinkExt,
    std::{collections::HashMap, sync::Arc},
    tokio::{
        io::{AsyncRead, AsyncWrite},
        net::{TcpListener, UnixListener},
        sync::{Mutex, RwLock},
    },
    tokio_stream::StreamExt,
    tokio_util::codec::length_delimited::LengthDelimitedCodec,
};

/// Represents a fid of clients holding associated `Filesystem::Fid`.
#[derive(Clone, Debug, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Fid<T> {
    /// Raw client side fid.
    fid: u32,

    /// `Filesystem::Fid` associated with this fid.
    /// Changing this value affects the continuous callbacks.
    pub aux: T,
}

impl<T> Fid<T> {
    /// Get the raw fid.
    pub fn fid(&self) -> u32 {
        self.fid
    }
}

#[async_trait]
/// Filesystem server trait.
///
/// Implementors can represent an error condition by returning an `Err`.
/// Otherwise, they must return `Fcall` with the required fields filled.
///
/// The default implementation, returning EOPNOTSUPP error, is provided to the all methods
/// except Rversion.
/// The default implementation of Rversion returns a message accepting 9P2000.L.
///
/// # NOTE
/// Defined as `Srv` in 9p.h of Plan 9.
///
/// # Protocol
/// 9P2000.L
pub trait Filesystem: Send {
    /// User defined fid type to be associated with a client's fid.
    type Fid: Send + Sync + Default;

    // 9P2000.L
    async fn rstatfs(&self, _: &Fid<Self::Fid>) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rlopen(&self, _: &Fid<Self::Fid>, _flags: u32) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rlcreate(
        &self,
        _: &Fid<Self::Fid>,
        _name: &str,
        _flags: u32,
        _mode: u32,
        _gid: u32,
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rsymlink(
        &self,
        _: &Fid<Self::Fid>,
        _name: &str,
        _sym: &str,
        _gid: u32,
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rmknod(
        &self,
        _: &Fid<Self::Fid>,
        _name: &str,
        _mode: u32,
        _major: u32,
        _minor: u32,
        _gid: u32,
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rrename(&self, _: &Fid<Self::Fid>, _: &Fid<Self::Fid>, _name: &str) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rreadlink(&self, _: &Fid<Self::Fid>) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rgetattr(&self, _: &Fid<Self::Fid>, _req_mask: GetattrMask) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rsetattr(
        &self,
        _: &Fid<Self::Fid>,
        _valid: SetattrMask,
        _stat: &SetAttr,
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rxattrwalk(
        &self,
        _: &Fid<Self::Fid>,
        _: &Fid<Self::Fid>,
        _name: &str,
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rxattrcreate(
        &self,
        _: &Fid<Self::Fid>,
        _name: &str,
        _attr_size: u64,
        _flags: u32,
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rreaddir(&self, _: &Fid<Self::Fid>, _offset: u64, _count: u32) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rfsync(&self, _: &Fid<Self::Fid>) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rlock(&self, _: &Fid<Self::Fid>, _lock: &Flock) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rgetlock(&self, _: &Fid<Self::Fid>, _lock: &Getlock) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rlink(&self, _: &Fid<Self::Fid>, _: &Fid<Self::Fid>, _name: &str) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rmkdir(
        &self,
        _: &Fid<Self::Fid>,
        _name: &str,
        _mode: u32,
        _gid: u32,
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rrenameat(
        &self,
        _: &Fid<Self::Fid>,
        _oldname: &str,
        _: &Fid<Self::Fid>,
        _newname: &str,
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn runlinkat(&self, _: &Fid<Self::Fid>, _name: &str, _flags: u32) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    /*
     * 9P2000.u subset
     */
    async fn rauth(
        &self,
        _: &Fid<Self::Fid>,
        _uname: &str,
        _aname: &str,
        _n_uname: u32,
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rattach(
        &self,
        _: &Fid<Self::Fid>,
        _afid: Option<&Fid<Self::Fid>>,
        _uname: &str,
        _aname: &str,
        _n_uname: u32,
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    /*
     * 9P2000 subset
     */
    async fn rflush(&self, _old: Option<&Fcall>) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rwalk(
        &self,
        _: &Fid<Self::Fid>,
        _new: &Fid<Self::Fid>,
        _wnames: &[String],
    ) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rread(&self, _: &Fid<Self::Fid>, _offset: u64, _count: u32) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rwrite(&self, _: &Fid<Self::Fid>, _offset: u64, _data: &Data) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rclunk(&self, _: &Fid<Self::Fid>) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rremove(&self, _: &Fid<Self::Fid>) -> Result<Fcall> {
        Err(error::Error::No(EOPNOTSUPP))
    }

    async fn rversion(&self, msize: u32, ver: &str) -> Result<Fcall> {
        Ok(Fcall::Rversion {
            msize,
            version: match ver {
                P92000L => ver.to_owned(),
                _ => VERSION_UNKNOWN.to_owned(),
            },
        })
    }
}

#[rustfmt::skip]
async fn dispatch_once<Fs, FsFid>(
    msg: &Msg,
    fs: Arc<Fs>,
    fsfids: Arc<RwLock<HashMap<u32, Fid<FsFid>>>>,
) -> Result<Fcall>
where
    Fs: Filesystem<Fid = FsFid> + Send + Sync,
    FsFid: Send + Sync + Default,
{
    let newfid = msg.body.newfid().map(|f| Fid {
        fid: f,
        aux: Default::default(),
    });

    use crate::Fcall::*;
    let response = {
        let fids = fsfids.read().await;
        let get_fid = |fid: &u32| fids.get(fid).ok_or(error::Error::No(EBADF));

        let fut = match msg.body {
            Tstatfs { fid }                                                     => fs.rstatfs(get_fid(&fid)?),
            Tlopen { fid, ref flags }                                           => fs.rlopen(get_fid(&fid)?, *flags),
            Tlcreate { fid, ref name, ref flags, ref mode, ref gid }            => fs.rlcreate(get_fid(&fid)?, name, *flags, *mode, *gid),
            Tsymlink { fid, ref name, ref symtgt, ref gid }                     => fs.rsymlink(get_fid(&fid)?, name, symtgt, *gid),
            Tmknod { dfid, ref name, ref mode, ref major, ref minor, ref gid }  => fs.rmknod(get_fid(&dfid)?, name, *mode, *major, *minor, *gid),
            Trename { fid, dfid, ref name }                                     => fs.rrename(get_fid(&fid)?, get_fid(&dfid)?, name),
            Treadlink { fid }                                                   => fs.rreadlink(get_fid(&fid)?),
            Tgetattr { fid, ref req_mask }                                      => fs.rgetattr(get_fid(&fid)?, *req_mask),
            Tsetattr { fid, ref valid, ref stat }                               => fs.rsetattr(get_fid(&fid)?, *valid, stat),
            Txattrwalk { fid, newfid: _, ref name }                             => fs.rxattrwalk(get_fid(&fid)?, newfid.as_ref().unwrap(), name),
            Txattrcreate { fid, ref name, ref attr_size, ref flags }            => fs.rxattrcreate(get_fid(&fid)?, name, *attr_size, *flags),
            Treaddir { fid, ref offset, ref count }                             => fs.rreaddir(get_fid(&fid)?, *offset, *count),
            Tfsync { fid }                                                      => fs.rfsync(get_fid(&fid)?),
            Tlock { fid, ref flock }                                            => fs.rlock(get_fid(&fid)?, flock),
            Tgetlock { fid, ref flock }                                         => fs.rgetlock(get_fid(&fid)?, flock),
            Tlink { dfid, fid, ref name }                                       => fs.rlink(get_fid(&dfid)?, get_fid(&fid)?, name),
            Tmkdir { dfid, ref name, ref mode, ref gid }                        => fs.rmkdir(get_fid(&dfid)?, name, *mode, *gid),
            Trenameat { olddirfid, ref oldname, newdirfid, ref newname }        => fs.rrenameat(get_fid(&olddirfid)?, oldname, get_fid(&newdirfid)?, newname),
            Tunlinkat { dirfd, ref name, ref flags }                            => fs.runlinkat(get_fid(&dirfd)?, name, *flags) ,
            Tauth { afid: _, ref uname, ref aname, ref n_uname }                => fs.rauth(newfid.as_ref().unwrap(), uname, aname, *n_uname),
            Tattach { fid: _, afid: _, ref uname, ref aname, ref n_uname }      => fs.rattach(newfid.as_ref().unwrap(), None, uname, aname, *n_uname),
            Tversion { ref msize, ref version }                                 => fs.rversion(*msize, version),
            Tflush { oldtag: _ }                                                => fs.rflush(None),
            Twalk { fid, newfid: _, ref wnames }                                => fs.rwalk(get_fid(&fid)?, newfid.as_ref().unwrap(), wnames),
            Tread { fid, ref offset, ref count }                                => fs.rread(get_fid(&fid)?, *offset, *count),
            Twrite { fid, ref offset, ref data }                                => fs.rwrite(get_fid(&fid)?, *offset, data),
            Tclunk { fid }                                                      => fs.rclunk(get_fid(&fid)?),
            Tremove { fid }                                                     => fs.rremove(get_fid(&fid)?),
            _                                                                   => return Err(error::Error::No(EOPNOTSUPP)),
        };

        fut.await?
    };

    /* Drop the fid which the Tclunk contains */
    if let Tclunk { fid } = msg.body {
        let mut fids = fsfids.write().await;
        fids.remove(&fid);
    }

    if let Some(newfid) = newfid {
        let mut fids = fsfids.write().await;
        fids.insert(newfid.fid, newfid);
    }

    Ok(response)
}

async fn dispatch<Fs, Reader, Writer>(filesystem: Fs, reader: Reader, writer: Writer) -> Result<()>
where
    Fs: 'static + Filesystem + Send + Sync,
    Reader: 'static + AsyncRead + Send + std::marker::Unpin,
    Writer: 'static + AsyncWrite + Send + std::marker::Unpin,
{
    let fsfids = Arc::new(RwLock::new(HashMap::new()));
    let filesystem = Arc::new(filesystem);

    let mut framedread = LengthDelimitedCodec::builder()
        .length_field_offset(0)
        .length_field_length(4)
        .length_adjustment(-4)
        .little_endian()
        .new_read(reader);
    let framedwrite = LengthDelimitedCodec::builder()
        .length_field_offset(0)
        .length_field_length(4)
        .length_adjustment(-4)
        .little_endian()
        .new_write(writer);
    let framedwrite = Arc::new(Mutex::new(framedwrite));

    while let Some(bytes) = framedread.next().await {
        let bytes = bytes?;

        let msg = serialize::read_msg(&mut bytes.reader())?;
        info!("\t← {:?}", msg);

        let fids = fsfids.clone();
        let fs = filesystem.clone();
        let framedwrite = framedwrite.clone();

        tokio::spawn(async move {
            let response_fcall = dispatch_once(&msg, fs, fids).await.unwrap_or_else(|e| {
                error!("{:?}: Error: \"{}\": {:?}", MsgType::from(&msg.body), e, e);
                Fcall::Rlerror {
                    ecode: e.errno() as u32,
                }
            });

            if MsgType::from(&response_fcall).is_r() {
                let response = Msg {
                    tag: msg.tag,
                    body: response_fcall,
                };

                let mut writer = bytes::BytesMut::with_capacity(65535).writer();
                serialize::write_msg(&mut writer, &response).unwrap();

                {
                    let mut framedwrite_locked = framedwrite.lock().await;
                    framedwrite_locked
                        .send(writer.into_inner().freeze())
                        .await
                        .unwrap();
                }
                info!("\t→ {:?}", response);
            }
        });
    }

    Ok(())
}

async fn srv_async_tcp<Fs>(filesystem: Fs, addr: &str) -> Result<()>
where
    Fs: 'static + Filesystem + Send + Sync + Clone,
{
    let listener = TcpListener::bind(addr).await?;

    loop {
        let (stream, peer) = listener.accept().await?;
        info!("accepted: {:?}", peer);

        let fs = filesystem.clone();
        tokio::spawn(async move {
            let (readhalf, writehalf) = stream.into_split();
            let res = dispatch(fs, readhalf, writehalf).await;
            if let Err(e) = res {
                error!("Error: {}: {:?}", e, e);
            }
        });
    }
}

pub async fn srv_async_unix<Fs>(filesystem: Fs, addr: &str) -> Result<()>
where
    Fs: 'static + Filesystem + Send + Sync + Clone,
{
    let listener = UnixListener::bind(addr)?;

    loop {
        let (stream, peer) = listener.accept().await?;
        info!("accepted: {:?}", peer);

        let fs = filesystem.clone();
        tokio::spawn(async move {
            let (readhalf, writehalf) = tokio::io::split(stream);
            let res = dispatch(fs, readhalf, writehalf).await;
            if let Err(e) = res {
                error!("Error: {:?}", e);
            }
        });
    }
}

pub async fn srv_async<Fs>(filesystem: Fs, addr: &str) -> Result<()>
where
    Fs: 'static + Filesystem + Send + Sync + Clone,
{
    let (proto, listen_addr) = utils::parse_proto(addr)
        .ok_or_else(|| io_err!(InvalidInput, "Invalid protocol or address"))?;

    match proto {
        "tcp" => srv_async_tcp(filesystem, &listen_addr).await,
        "unix" => srv_async_unix(filesystem, &listen_addr).await,
        _ => Err(From::from(io_err!(InvalidInput, "Protocol not supported"))),
    }
}
