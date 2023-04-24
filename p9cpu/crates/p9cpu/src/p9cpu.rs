use anyhow::{bail, Result};
use clap::Parser;
use libp9cpu::cmd::{Command, FsTab};
use libp9cpu::parse_namespace;
use std::os::unix::prelude::OsStringExt;
use tokio::io::AsyncBufReadExt;

#[derive(clap::ValueEnum, Clone, Debug)]
enum Net {
    Tcp,
    Vsock,
    Unix,
}

#[derive(Parser, Debug)]
#[command(author, version, about, long_about = None)]
struct Args {
    #[arg(long, value_enum, default_value_t = Net::Tcp)]
    net: Net,

    #[arg(long, default_value_t = 17010)]
    port: u32,

    #[arg(long, default_value = "")]
    namespace: String,

    #[arg(long, default_value_t = false)]
    tty: bool,

    #[arg(long)]
    fs_tab: Option<String>,

    #[arg(long, default_value = "/tmp")]
    tmp_mnt: String,

    #[arg(long, default_value_t = nix::unistd::geteuid().as_raw())]
    uid: u32,

    #[arg(long, default_value_t = nix::unistd::getegid().as_raw())]
    gid: u32,

    #[arg()]
    host: String,

    #[arg()]
    program: Option<String>,

    #[arg(last = true)]
    args: Vec<String>,
}

async fn app(args: Args) -> Result<()> {
    let addr = match args.net {
        Net::Vsock => libp9cpu::Addr::Vsock(tokio_vsock::VsockAddr::new(
            args.host.parse().unwrap(),
            args.port,
        )),
        Net::Unix => libp9cpu::Addr::Uds(args.host),
        Net::Tcp => libp9cpu::Addr::Tcp(format!("{}:{}", args.host, args.port).parse()?),
    };

    if args.tmp_mnt.is_empty() {
        bail!("`tmp_mnt` cannot be empty");
    }
    let mut fs_tab_lines = parse_namespace(&args.namespace, &args.tmp_mnt);
    let ninep = !fs_tab_lines.is_empty();
    if let Some(ref fs_tab) = args.fs_tab {
        let fs_tab_file = tokio::fs::File::open(fs_tab).await?;
        let mut lines = tokio::io::BufReader::new(fs_tab_file).lines();
        while let Some(line) = lines.next_line().await? {
            if line.starts_with('#') {
                continue;
            }
            fs_tab_lines.push(FsTab::try_from(line.as_str())?);
        }
    }
    let program = args.args[0].clone();
    let mut cmd = Command::new(program);
    cmd.args(Vec::from(&args.args[1..]));
    cmd.envs(std::env::vars_os().map(|(k, v)| (k.into_vec(), v.into_vec())));
    cmd.ninep(ninep);
    cmd.fstab(fs_tab_lines);
    cmd.tty(args.tty);
    cmd.tmp_mnt(args.tmp_mnt);
    cmd.ugid(args.uid, args.gid);

    let mut client = libp9cpu::client::rpc_based(addr).await?;
    client.start(cmd).await?;
    client.wait().await?;
    Ok(())
}

fn main() -> Result<()> {
    let args = Args::parse();
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap();

    let ret = runtime.block_on(app(args));

    runtime.shutdown_timeout(std::time::Duration::from_secs(0));

    ret
}
