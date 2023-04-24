use libp9cpu::server::P9cpuServerT;

use anyhow::Result;

use clap::Parser;

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
    #[arg(long)]
    uds: Option<String>,
    #[arg(long, hide = true)]
    launch: Option<u16>,
}

struct Launcher {}
impl libp9cpu::launcher::Launch for Launcher {
    fn launch(&self, port: u16) -> std::process::Command {
        let arg0 = std::env::args().next().unwrap();
        let mut sub_cmd = std::process::Command::new(arg0);
        sub_cmd.arg("--launch").arg(port.to_string());
        sub_cmd
    }
}

async fn app(args: Args) -> Result<()> {
    let addr = match args.net {
        Net::Vsock => libp9cpu::Addr::Vsock(tokio_vsock::VsockAddr::new(
            vsock::VMADDR_CID_ANY,
            args.port,
        )),
        Net::Tcp => libp9cpu::Addr::Tcp(format!("[::]:{}", args.port).parse().unwrap()),
        Net::Unix => libp9cpu::Addr::Uds(args.uds.unwrap()),
    };
    let launcher = Launcher {};
    let server = libp9cpu::server::rpc_based();
    server.serve(addr, launcher).await?;
    Ok(())
}

fn main() -> Result<()> {
    let args = Args::parse();
    if let Some(port) = args.launch {
        libp9cpu::launcher::connect(port)?;
        return Ok(());
    }

    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap();
    let ret = runtime.block_on(app(args));
    ret
}
