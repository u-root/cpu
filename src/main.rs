use clap::Parser;
use env_logger::Env;

pub mod client;

#[derive(Debug, Parser)]
#[clap(
    name = "cpu",
    about = "like Plan 9 cpu but for Linux",
    long_about = None
)]
struct Args {
    #[clap(value_parser, index = 1)]
    key_file: String,
}

fn main() {
    env_logger::init_from_env(Env::default().filter_or("CPU_LOG", "info"));
    log::debug!("DEBUG");

    let args = Args::parse();

    // TODO: make those CLI args/switches
    let hostname = "localhost";
    let port = 17010;
    let host = format!("{hostname}:{port}");
    let command = "/bbin/ls";

    client::cpu(&args.key_file, &host, command).unwrap()
}
