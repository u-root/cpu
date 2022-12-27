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

    #[arg(long, default_value = "")]
    namespace: String,

    #[arg(long, default_value_t = false)]
    tty: bool,

    #[arg(long)]
    fs_tab: Option<String>,

    #[arg(long, default_value = "/tmp")]
    tmp_mnt: String,

    #[arg()]
    host: String,

    #[arg()]
    program: Option<String>,

    #[arg(last = true)]
    args: Vec<String>,
}

async fn app(args: Args) -> Result<()> {
    unimplemented!("not implemented: {:?}", args);
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
