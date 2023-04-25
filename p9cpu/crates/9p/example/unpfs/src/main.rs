use {
    async_trait::async_trait,
    filetime::FileTime,
    nix::libc::{O_CREAT, O_RDONLY, O_RDWR, O_TRUNC, O_WRONLY},
    rs9p::{
        srv::{srv_async, Fid, Filesystem},
        *,
    },
    std::{
        io::SeekFrom,
        os::unix::{fs::PermissionsExt, io::FromRawFd},
        path::PathBuf,
    },
    tokio::{
        fs,
        io::{AsyncReadExt, AsyncSeekExt, AsyncWriteExt},
        sync::{Mutex, RwLock},
    },
    tokio_stream::{wrappers::ReadDirStream, StreamExt},
};

mod utils;
use crate::utils::*;

// Some clients will incorrectly set bits in 9p flags that don't make sense.
// For exmaple, the linux 9p kernel client propagates O_DIRECT to TCREATE and TOPEN
// and from there to the server.
// Processes on client machines set O_DIRECT to bypass the cache, but if
// the server uses O_DIRECT in the open or create, then subsequent server
// write and read system calls will fail, as O_DIRECT requires at minimum 512
// byte aligned data, and the data is almost always not aligned.
// While the linux kernel client is arguably broken, we won't be able
// to fix every kernel out there, and this is surely not the only buggy client
// we will see.
// The fix is to enumerate the set of flags we support and then and that with
// the flags received in a TCREATE or TOPEN. This nicely fixes a real problem
// we are seeing with a file system benchmark.
const UNIX_FLAGS: u32 = (O_WRONLY | O_RDONLY | O_RDWR | O_CREAT | O_TRUNC) as u32;

#[derive(Default)]
struct UnpfsFid {
    realpath: RwLock<PathBuf>,
    file: Mutex<Option<fs::File>>,
}

#[derive(Clone)]
struct Unpfs {
    realroot: PathBuf,
}

#[async_trait]
impl Filesystem for Unpfs {
    type Fid = UnpfsFid;

    async fn rattach(
        &self,
        fid: &Fid<Self::Fid>,
        _afid: Option<&Fid<Self::Fid>>,
        _uname: &str,
        _aname: &str,
        _n_uname: u32,
    ) -> Result<Fcall> {
        {
            let mut realpath = fid.aux.realpath.write().await;
            *realpath = PathBuf::from(&self.realroot);
        }

        Ok(Fcall::Rattach {
            qid: get_qid(&self.realroot).await?,
        })
    }

    async fn rwalk(
        &self,
        fid: &Fid<Self::Fid>,
        newfid: &Fid<Self::Fid>,
        wnames: &[String],
    ) -> Result<Fcall> {
        let mut wqids = Vec::new();
        let mut path = {
            let realpath = fid.aux.realpath.read().await;
            realpath.clone()
        };

        for (i, name) in wnames.iter().enumerate() {
            path.push(name);

            let qid = match get_qid(&path).await {
                Ok(qid) => qid,
                Err(e) => {
                    if i == 0 {
                        return Err(e);
                    } else {
                        break;
                    }
                }
            };

            wqids.push(qid);
        }

        {
            let mut new_realpath = newfid.aux.realpath.write().await;
            *new_realpath = path;
        }

        Ok(Fcall::Rwalk { wqids })
    }

    async fn rgetattr(&self, fid: &Fid<Self::Fid>, req_mask: GetattrMask) -> Result<Fcall> {
        let attr = {
            let realpath = fid.aux.realpath.read().await;
            fs::symlink_metadata(&*realpath).await?
        };

        Ok(Fcall::Rgetattr {
            valid: req_mask,
            qid: qid_from_attr(&attr),
            stat: From::from(attr),
        })
    }

    async fn rsetattr(
        &self,
        fid: &Fid<Self::Fid>,
        valid: SetattrMask,
        stat: &SetAttr,
    ) -> Result<Fcall> {
        let filepath = {
            let realpath = fid.aux.realpath.read().await;
            realpath.clone()
        };

        if valid.contains(SetattrMask::MODE) {
            fs::set_permissions(&filepath, PermissionsExt::from_mode(stat.mode)).await?;
        }

        if valid.intersects(SetattrMask::UID | SetattrMask::GID) {
            let uid = if valid.contains(SetattrMask::UID) {
                Some(nix::unistd::Uid::from_raw(stat.uid))
            } else {
                None
            };
            let gid = if valid.contains(SetattrMask::GID) {
                Some(nix::unistd::Gid::from_raw(stat.gid))
            } else {
                None
            };
            nix::unistd::chown(&filepath, uid, gid)?;
        }

        if valid.contains(SetattrMask::SIZE) {
            let _ = fs::OpenOptions::new()
                .write(true)
                .create(false)
                .open(&filepath)
                .await?
                .set_len(stat.size)
                .await?;
        }

        if valid.intersects(SetattrMask::ATIME_SET | SetattrMask::MTIME_SET) {
            let attr = fs::metadata(&filepath).await?;
            let atime = if valid.contains(SetattrMask::ATIME_SET) {
                FileTime::from_unix_time(stat.atime.sec as i64, stat.atime.nsec as u32)
            } else {
                FileTime::from_last_access_time(&attr)
            };

            let mtime = if valid.contains(SetattrMask::MTIME_SET) {
                FileTime::from_unix_time(stat.mtime.sec as i64, stat.mtime.nsec as u32)
            } else {
                FileTime::from_last_modification_time(&attr)
            };

            let _ = tokio::task::spawn_blocking(move || {
                filetime::set_file_times(filepath, atime, mtime)
            })
            .await;
        }

        Ok(Fcall::Rsetattr)
    }

    async fn rreadlink(&self, fid: &Fid<Self::Fid>) -> Result<Fcall> {
        let link = {
            let realpath = fid.aux.realpath.read().await;
            fs::read_link(&*realpath).await?
        };

        Ok(Fcall::Rreadlink {
            target: link.to_string_lossy().into_owned(),
        })
    }

    async fn rreaddir(&self, fid: &Fid<Self::Fid>, off: u64, count: u32) -> Result<Fcall> {
        let mut dirents = DirEntryData::new();

        let offset = if off == 0 {
            dirents.push(get_dirent_from(".", 0).await?);
            dirents.push(get_dirent_from("..", 1).await?);
            off
        } else {
            off - 1
        } as usize;

        let mut entries = {
            let realpath = fid.aux.realpath.read().await;
            ReadDirStream::new(fs::read_dir(&*realpath).await?).skip(offset)
        };

        let mut i = offset;
        while let Some(entry) = entries.next().await {
            let dirent = get_dirent(&entry?, 2 + i as u64).await?;
            if dirents.size() + dirent.size() > count {
                break;
            }
            dirents.push(dirent);
            i += 1;
        }

        Ok(Fcall::Rreaddir { data: dirents })
    }

    async fn rlopen(&self, fid: &Fid<Self::Fid>, flags: u32) -> Result<Fcall> {
        let realpath = {
            let realpath = fid.aux.realpath.read().await;
            realpath.clone()
        };

        let qid = get_qid(&realpath).await?;
        if !qid.typ.contains(QidType::DIR) {
            let oflags = nix::fcntl::OFlag::from_bits_truncate((flags & UNIX_FLAGS) as i32);
            let omode = nix::sys::stat::Mode::from_bits_truncate(0);
            let fd = nix::fcntl::open(&realpath, oflags, omode)?;

            {
                let mut file = fid.aux.file.lock().await;
                *file = Some(fs::File::from_std(unsafe {
                    std::fs::File::from_raw_fd(fd)
                }));
            }
        }

        Ok(Fcall::Rlopen { qid, iounit: 0 })
    }

    async fn rlcreate(
        &self,
        fid: &Fid<Self::Fid>,
        name: &str,
        flags: u32,
        mode: u32,
        _gid: u32,
    ) -> Result<Fcall> {
        let path = {
            let realpath = fid.aux.realpath.read().await;
            realpath.join(name)
        };
        let oflags = nix::fcntl::OFlag::from_bits_truncate((flags & UNIX_FLAGS) as i32);
        let omode = nix::sys::stat::Mode::from_bits_truncate(mode);
        let fd = nix::fcntl::open(&path, oflags, omode)?;

        let qid = get_qid(&path).await?;
        {
            let mut realpath = fid.aux.realpath.write().await;
            *realpath = path;
        }
        {
            let mut file = fid.aux.file.lock().await;
            *file = Some(fs::File::from_std(unsafe {
                std::fs::File::from_raw_fd(fd)
            }));
        }

        Ok(Fcall::Rlcreate { qid, iounit: 0 })
    }

    async fn rread(&self, fid: &Fid<Self::Fid>, offset: u64, count: u32) -> Result<Fcall> {
        let buf = {
            let mut file = fid.aux.file.lock().await;
            let file = file.as_mut().ok_or_else(|| INVALID_FID!())?;
            file.seek(SeekFrom::Start(offset)).await?;

            let mut buf = create_buffer(count as usize);
            let bytes = file.read(&mut buf[..]).await?;
            buf.truncate(bytes);
            buf
        };

        Ok(Fcall::Rread { data: Data(buf) })
    }

    async fn rwrite(&self, fid: &Fid<Self::Fid>, offset: u64, data: &Data) -> Result<Fcall> {
        let count = {
            let mut file = fid.aux.file.lock().await;
            let file = file.as_mut().ok_or_else(|| INVALID_FID!())?;
            file.seek(SeekFrom::Start(offset)).await?;
            file.write(&data.0).await? as u32
        };

        Ok(Fcall::Rwrite { count })
    }

    async fn rmkdir(
        &self,
        dfid: &Fid<Self::Fid>,
        name: &str,
        _mode: u32,
        _gid: u32,
    ) -> Result<Fcall> {
        let path = {
            let realpath = dfid.aux.realpath.read().await;
            realpath.join(name)
        };

        fs::create_dir(&path).await?;

        Ok(Fcall::Rmkdir {
            qid: get_qid(&path).await?,
        })
    }

    async fn rrenameat(
        &self,
        olddir: &Fid<Self::Fid>,
        oldname: &str,
        newdir: &Fid<Self::Fid>,
        newname: &str,
    ) -> Result<Fcall> {
        let oldpath = {
            let realpath = olddir.aux.realpath.read().await;
            realpath.join(oldname)
        };

        let newpath = {
            let realpath = newdir.aux.realpath.read().await;
            realpath.join(newname)
        };

        fs::rename(&oldpath, &newpath).await?;

        Ok(Fcall::Rrenameat)
    }

    async fn runlinkat(&self, dirfid: &Fid<Self::Fid>, name: &str, _flags: u32) -> Result<Fcall> {
        let path = {
            let realpath = dirfid.aux.realpath.read().await;
            realpath.join(name)
        };

        match fs::symlink_metadata(&path).await? {
            ref attr if attr.is_dir() => fs::remove_dir(&path).await?,
            _ => fs::remove_file(&path).await?,
        };

        Ok(Fcall::Runlinkat)
    }

    async fn rfsync(&self, fid: &Fid<Self::Fid>) -> Result<Fcall> {
        {
            let mut file = fid.aux.file.lock().await;
            file.as_mut()
                .ok_or_else(|| INVALID_FID!())?
                .sync_all()
                .await?;
        }

        Ok(Fcall::Rfsync)
    }

    async fn rclunk(&self, _: &Fid<Self::Fid>) -> Result<Fcall> {
        Ok(Fcall::Rclunk)
    }

    async fn rstatfs(&self, fid: &Fid<Self::Fid>) -> Result<Fcall> {
        let path = {
            let realpath = fid.aux.realpath.read().await;
            realpath.clone()
        };

        //let fs = nix::sys::statvfs::statvfs(&path)?;
        let fs = tokio::task::spawn_blocking(move || nix::sys::statvfs::statvfs(&path))
            .await
            .unwrap()?;

        Ok(Fcall::Rstatfs {
            statfs: From::from(fs),
        })
    }
}

async fn unpfs_main(args: Vec<String>) -> rs9p::Result<i32> {
    if args.len() < 3 {
        eprintln!("Usage: {} proto!address!port mountpoint", args[0]);
        eprintln!("  where: proto = tcp | unix");
        return Ok(-1);
    }

    let (addr, mountpoint) = (&args[1], PathBuf::from(&args[2]));
    if !fs::metadata(&mountpoint).await?.is_dir() {
        return res!(io_err!(Other, "mount point must be a directory"));
    }

    println!("[*] Ready to accept clients: {}", addr);
    srv_async(
        Unpfs {
            realroot: mountpoint,
        },
        addr,
    )
    .await
    .and(Ok(0))
}

#[tokio::main]
async fn main() {
    env_logger::init();

    let args = std::env::args().collect();
    let exit_code = unpfs_main(args).await.unwrap_or_else(|e| {
        eprintln!("Error: {:?}", e);
        -1
    });

    std::process::exit(exit_code);
}
