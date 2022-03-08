//! # client
//!
//! Ssh2-config implementation with a ssh2 client

/**
 * MIT License
 *
 * ssh2-config - Copyright (c) 2021 Christian Visintin
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */
use dirs::home_dir;
use ssh2::{MethodType, Session};
use ssh2_config::{HostParams, SshConfig};
use std::env::args;
use std::fs::File;
use std::io::BufReader;
use std::net::{SocketAddr, TcpStream, ToSocketAddrs};
use std::path::{Path, PathBuf};
use std::process::exit;
use std::time::Duration;

pub fn client() {
    // get args
    let args: Vec<String> = args().collect();
    let address = match args.get(1) {
        Some(addr) => addr.to_string(),
        None => {
            usage();
            exit(255)
        }
    };
    // check path
    let config_path = match args.get(2) {
        Some(p) => PathBuf::from(p),
        None => {
            let mut p = home_dir().expect("Failed to get home_dir for guest OS");
            p.extend(Path::new(".ssh/config"));
            p
        }
    };
    // Open config file
    let config = read_config(config_path.as_path());
    let params = config.query(address.as_str());
    connect(address.as_str(), &params);
}

fn usage() {
    eprintln!("Usage: cargo run --example client -- <address:port> [config-path]");
}

fn read_config(p: &Path) -> SshConfig {
    let mut reader = match File::open(p) {
        Ok(f) => BufReader::new(f),
        Err(err) => panic!("Could not open file '{}': {}", p.display(), err),
    };
    match SshConfig::default().parse(&mut reader) {
        Ok(config) => config,
        Err(err) => panic!("Failed to parse configuration: {}", err),
    }
}

fn connect(host: &str, params: &HostParams) {
    // Resolve host
    let host = match params.host_name.as_deref() {
        Some(h) => h,
        None => host,
    };
    let port = match params.port {
        None => 22,
        Some(p) => p,
    };
    let host = match host.contains(':') {
        true => host.to_string(),
        false => format!("{}:{}", host, port),
    };
    println!("Connecting to host {}...", host);
    let socket_addresses: Vec<SocketAddr> = match host.to_socket_addrs() {
        Ok(s) => s.collect(),
        Err(err) => {
            panic!("Could not parse host: {}", err);
        }
    };
    let mut tcp: Option<TcpStream> = None;
    // Try addresses
    for socket_addr in socket_addresses.iter() {
        match TcpStream::connect_timeout(
            socket_addr,
            params.connect_timeout.unwrap_or(Duration::from_secs(30)),
        ) {
            Ok(stream) => {
                println!("Established connection with {}", socket_addr);
                tcp = Some(stream);
                break;
            }
            Err(_) => continue,
        }
    }
    // If stream is None, return connection timeout
    let stream: TcpStream = match tcp {
        Some(t) => t,
        None => {
            panic!("No suitable socket address found; connection timeout");
        }
    };
    let mut session: Session = match Session::new() {
        Ok(s) => s,
        Err(err) => {
            panic!("Could not create session: {}", err);
        }
    };
    // Configure session
    configure_session(&mut session, params);
    // Connect
    session.set_tcp_stream(stream);
    if let Err(err) = session.handshake() {
        panic!("Handshake failed: {}", err);
    }
    // Get username
    let username = match params.user.as_ref() {
        Some(u) => {
            println!("Using username '{}'", u);
            u.clone()
        },
        None =>  {
        // reads until a \n is encountered
        println!("User:");
        let line: String = read!("{}\n");
        line.clone()
     }
    };
    let password = read_secret("Password: ");
    if let Err(err) = session.userauth_password(username.as_str(), password.as_str()) {
        panic!("Authentication failed: {}", err);
    }
    if let Some(banner) = session.banner() {
        println!("{}", banner);
    }
    println!("Connection OK!");
    if let Err(err) = session.disconnect(None, "mandi mandi!", None) {
        panic!("Disconnection failed: {}", err);
    }
}

fn configure_session(session: &mut Session, params: &HostParams) {
    println!("Configuring session...");
    if let Some(compress) = params.compression {
        println!("compression: {}", compress);
        session.set_compress(compress);
    }
    if params.tcp_keep_alive.unwrap_or(false) && params.server_alive_interval.is_some() {
        let interval = params.server_alive_interval.unwrap().as_secs() as u32;
        println!("keepalive interval: {} seconds", interval);
        session.set_keepalive(true, interval);
    }
    // algos
    if let Some(algos) = params.kex_algorithms.as_deref() {
        if let Err(err) = session.method_pref(MethodType::Kex, algos.join(",").as_str()) {
            panic!("Could not set KEX algorithms: {}", err);
        }
    }
    if let Some(algos) = params.host_key_algorithms.as_deref() {
        if let Err(err) = session.method_pref(MethodType::HostKey, algos.join(",").as_str()) {
            panic!("Could not set host key algorithms: {}", err);
        }
    }
    if let Some(algos) = params.ciphers.as_deref() {
        if let Err(err) = session.method_pref(MethodType::CryptCs, algos.join(",").as_str()) {
            panic!("Could not set crypt algorithms (client-server): {}", err);
        }
        if let Err(err) = session.method_pref(MethodType::CryptSc, algos.join(",").as_str()) {
            panic!("Could not set crypt algorithms (server-client): {}", err);
        }
    }
    if let Some(algos) = params.mac.as_deref() {
        if let Err(err) = session.method_pref(MethodType::MacCs, algos.join(",").as_str()) {
            panic!("Could not set MAC algorithms (client-server): {}", err);
        }
        if let Err(err) = session.method_pref(MethodType::MacSc, algos.join(",").as_str()) {
            panic!("Could not set MAC algorithms (server-client): {}", err);
        }
    }
}

fn read_secret(prompt: &str) -> String {
    rpassword::read_password_from_tty(Some(prompt)).expect("Failed to read from stdin")
}
