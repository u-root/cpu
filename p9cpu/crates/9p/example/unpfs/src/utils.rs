use {
    rs9p::fcall::*,
    std::{fs::Metadata, os::unix::prelude::*, path::Path},
    tokio::fs,
};

#[macro_export]
macro_rules! res {
    ($err:expr) => {
        Err(From::from($err))
    };
}

#[macro_export]
macro_rules! io_err {
    ($kind:ident, $msg:expr) => {
        ::std::io::Error::new(::std::io::ErrorKind::$kind, $msg)
    };
}

#[macro_export]
macro_rules! INVALID_FID {
    () => {
        io_err!(InvalidInput, "Invalid fid")
    };
}

pub fn create_buffer(size: usize) -> Vec<u8> {
    let mut buffer = Vec::with_capacity(size);
    unsafe {
        buffer.set_len(size);
    }
    buffer
}

pub async fn get_qid<T: AsRef<Path> + ?Sized>(path: &T) -> rs9p::Result<Qid> {
    Ok(qid_from_attr(&fs::symlink_metadata(path.as_ref()).await?))
}

pub fn qid_from_attr(attr: &Metadata) -> Qid {
    Qid {
        typ: From::from(attr.file_type()),
        version: 0,
        path: attr.ino(),
    }
}

pub async fn get_dirent_from<P: AsRef<Path> + ?Sized>(
    p: &P,
    offset: u64,
) -> rs9p::Result<DirEntry> {
    Ok(DirEntry {
        qid: get_qid(p).await?,
        offset,
        typ: 0,
        name: p.as_ref().to_string_lossy().into_owned(),
    })
}

pub async fn get_dirent(entry: &fs::DirEntry, offset: u64) -> rs9p::Result<DirEntry> {
    Ok(DirEntry {
        qid: qid_from_attr(&entry.metadata().await?),
        offset,
        typ: 0,
        name: entry.file_name().to_string_lossy().into_owned(),
    })
}
