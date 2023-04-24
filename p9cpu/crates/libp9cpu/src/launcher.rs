use justerror::Error;
use log::warn;
use nix::mount::{mount, MsFlags};
use nix::sched::{unshare, CloneFlags};
use nix::unistd::setsid;
use std::ffi::{CString, OsStr};
use std::net::TcpStream;
use std::os::unix::prelude::OsStrExt;
use std::{io::Read, process::Command};

use crate::cmd;

pub trait Launch {
    fn launch(&self, port: u16) -> Command;
}

fn parse_fstab_opt(opt: &str) -> (MsFlags, String) {
    let mut opts = vec![];
    let mut flag = MsFlags::empty();
    for f in opt.split(',') {
        if f == "defaults" {
            continue;
        }
        if f == "bind" {
            flag |= MsFlags::MS_BIND;
        } else {
            opts.push(f);
        }
    }
    (flag, opts.join(","))
}

fn get_ptr(s: &str) -> Option<&str> {
    if s.is_empty() {
        None
    } else {
        Some(s)
    }
}

#[Error]
pub enum Error {
    Unshare {
        flag: &'static str,
        #[source]
        err: nix::Error,
    },
    MountRoot(#[source] nix::Error),
    UnexpectedBytes,
    ShutdownSocket(#[source] std::io::Error),
    ConvertCString(#[source] std::ffi::NulError),
    Exec(#[source] nix::Error),
    ConnectToPort(#[source] std::io::Error),
    ReadUds(#[source] std::io::Error),
    ProtoDecode(#[source] prost::DecodeError),
    SetSid(#[source] nix::Error),
    SetControllingTerminal(#[source] nix::Error),
    SetUid(#[source] nix::Error),
    SetGid(#[source] nix::Error),
}

pub fn connect(port: u16) -> Result<(), Error> {
    unshare(CloneFlags::CLONE_NEWNS).map_err(|err| Error::Unshare {
        flag: "CLONE_NEWNS",
        err,
    })?;
    let mut stream = TcpStream::connect(std::net::SocketAddrV4::new(
        std::net::Ipv4Addr::LOCALHOST,
        port,
    ))
    .map_err(Error::ConnectToPort)?;
    let mut buf = [0; std::mem::size_of::<usize>()];
    stream.read_exact(&mut buf).map_err(Error::ReadUds)?;
    let length = usize::from_le_bytes(buf);
    let mut buf = vec![0; length];
    stream
        .read_exact(&mut buf[0..length])
        .map_err(Error::ReadUds)?;
    let cmd: cmd::Cmd = prost::Message::decode(buf.as_slice()).map_err(Error::ProtoDecode)?;
    mount::<str, str, str, str>(None, "/", None, MsFlags::MS_REC | MsFlags::MS_PRIVATE, None)
        .map_err(Error::MountRoot)?;

    for tab in &cmd.fstab {
        let (flags, data) = parse_fstab_opt(&tab.mntops);
        if let Err(err) = nix::mount::mount(
            get_ptr(&tab.spec),
            tab.file.as_str(),
            get_ptr(&tab.vfstype),
            flags,
            get_ptr(&data),
        ) {
            warn!("Mounting {:?}: {:?}", tab, err)
        }
    }
    let mut buf = [0];
    match stream.read(&mut buf) {
        Ok(0) => {}
        _ => return Err(Error::UnexpectedBytes),
    }
    stream
        .shutdown(std::net::Shutdown::Both)
        .map_err(Error::ShutdownSocket)?;

    for env in &cmd.envs {
        std::env::set_var(OsStr::from_bytes(&env.key), OsStr::from_bytes(&env.val));
    }
    let arg0_c = CString::new(cmd.program).map_err(Error::ConvertCString)?;
    let mut args_c = vec![];
    for arg in cmd.args {
        args_c.push(CString::new(arg).map_err(Error::ConvertCString)?);
    }
    let mut full_args = vec![arg0_c.as_c_str()];
    for arg_c in &args_c {
        full_args.push(&arg_c);
    }

    if cmd.tty {
        setsid().map_err(Error::SetSid)?;
        nix::ioctl_none_bad!(tiocsctty, libc::TIOCSCTTY);
        unsafe { tiocsctty(libc::STDIN_FILENO) }.map_err(Error::SetControllingTerminal)?;
    }

    nix::unistd::setgid(cmd.gid.into()).map_err(Error::SetGid)?;
    nix::unistd::setuid(cmd.uid.into()).map_err(Error::SetUid)?;

    nix::unistd::execvp(&arg0_c, &full_args).map_err(Error::Exec)?;
    Ok(())
}
