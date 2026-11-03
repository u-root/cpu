use russh::keys::PrivateKey;
// Note: Depending on your exact russh version, the Algorithm enum
// might come from `ssh_key::Algorithm` or be directly re-exported.
use russh::keys::ssh_key::Algorithm;

use std::os::fd::AsFd;

use nix::pty::{Winsize, openpty};
use nix::unistd::setsid;
use russh::ChannelId;
use russh::keys::PublicKey;
use russh::server::{Config, Handler, Server, Session, VerifiedPublicKey};
use std::collections::HashMap;
use std::env;
use std::os::fd::{AsRawFd, OwnedFd};
use std::os::unix::prelude::CommandExt;
use std::path::Path;
use std::process::Command;
use std::process::Stdio;
use std::sync::Arc;
use tokio::fs::File;
use tokio::io::AsyncReadExt;
use tokio::io::AsyncWriteExt;
use tokio::net::TcpListener;
use tokio::sync::Mutex;

/* -------------------------------------------------------------------------- */
/* 1. Server Factory: Creates a new Handler per incoming TCP client         */
/* -------------------------------------------------------------------------- */

#[derive(Clone)]
pub struct AppServer {
    pub authorized_keys: Arc<Vec<PublicKey>>,
}

impl Server for AppServer {
    type Handler = PtyShellHandler;

    fn new_client(&mut self, _peer_addr: Option<std::net::SocketAddr>) -> Self::Handler {
        PtyShellHandler::new(self.authorized_keys.clone())
    }
}

/* -------------------------------------------------------------------------- */
/* 2. Connection Handler: Manages State & Process I/O for single SSH Session  */
/* -------------------------------------------------------------------------- */

pub struct PtyShellHandler {
    pub authorized_keys: Arc<Vec<PublicKey>>,
    pub client_env: HashMap<String, String>,
    pub pty_size: (u16, u16),
    pub pty_writer: Option<Arc<Mutex<tokio::io::WriteHalf<File>>>>,
    pub master_fd: Option<OwnedFd>,
}

impl PtyShellHandler {
    pub fn new(authorized_keys: Arc<Vec<PublicKey>>) -> Self {
        Self {
            authorized_keys,
            client_env: HashMap::new(),
            pty_size: (80, 24),
            pty_writer: None,
            master_fd: None,
        }
    }

    fn update_winsize(fd: &OwnedFd, cols: u16, rows: u16) {
        let ws = Winsize {
            ws_row: rows,
            ws_col: cols,
            ws_xpixel: 0,
            ws_ypixel: 0,
        };
        unsafe {
            #[cfg(target_os = "linux")]
            nix::libc::ioctl(fd.as_raw_fd(), nix::libc::TIOCSWINSZ, &ws);
        }
    }
}

impl Handler for PtyShellHandler {
    type Error = russh::Error;

    // Public Key Authentication (Matching key_data to ignore comment strings)
    async fn auth_publickey(
        &mut self,
        _user: &str,
        public_key: VerifiedPublicKey<'_>,
    ) -> Result<russh::server::Auth, Self::Error> {
        let authenticated = self
            .authorized_keys
            .iter()
            .any(|k| k.key_data() == public_key.public_key.key_data());

        if authenticated {
            Ok(russh::server::Auth::Accept)
        } else {
            Ok(russh::server::Auth::Reject {
                proceed_with_methods: None,
                partial_success: false,
            })
        }
    }

    // Capture environment variables (e.g. SHELL, PWD)
    async fn env_request(
        &mut self,
        _channel: ChannelId,
        name: &str,
        value: &str,
        _session: &mut Session,
    ) -> Result<(), Self::Error> {
        self.client_env.insert(name.to_string(), value.to_string());
        Ok(())
    }

    // Handle PTY creation request
    async fn pty_request(
        &mut self,
        _channel: ChannelId,
        _term: &str,
        col_width: u32,
        row_height: u32,
        _pix_width: u32,
        _pix_height: u32,
        _modes: &[(russh::Pty, u32)],
        _session: &mut Session,
    ) -> Result<(), Self::Error> {
        self.pty_size = (col_width as u16, row_height as u16);
        if let Some(ref fd) = self.master_fd {
            Self::update_winsize(fd, self.pty_size.0, self.pty_size.1);
        }
        Ok(())
    }

    // Handle Window Resize events
    async fn window_change_request(
        &mut self,
        _channel: ChannelId,
        col_width: u32,
        row_height: u32,
        _pix_width: u32,
        _pix_height: u32,
        _session: &mut Session,
    ) -> Result<(), Self::Error> {
        self.pty_size = (col_width as u16, row_height as u16);
        if let Some(ref fd) = self.master_fd {
            Self::update_winsize(fd, self.pty_size.0, self.pty_size.1);
        }
        Ok(())
    }

    // Pass SSH Client stdin -> Master PTY input
    async fn data(
        &mut self,
        _channel: ChannelId,
        data: &[u8],
        _session: &mut Session,
    ) -> Result<(), Self::Error> {
        if let Some(ref writer) = self.pty_writer {
            let mut w = writer.lock().await;
            let _ = w.write_all(data).await;
            let _ = w.flush().await;
        }
        Ok(())
    }

    // Allocate PTY, spawn child shell, and route Master PTY output -> SSH client
    async fn shell_request(
        &mut self,
        channel: ChannelId,
        session: &mut Session,
    ) -> Result<(), Self::Error> {
        let shell_bin = self
            .client_env
            .get("SHELL")
            .cloned()
            .or_else(|| env::var("SHELL").ok())
            .unwrap_or_else(|| "/bin/sh".to_string());

        let pwd_val = self
            .client_env
            .get("PWD")
            .cloned()
            .or_else(|| env::var("PWD").ok())
            .unwrap_or_else(|| "/".to_string());

        let work_dir = if Path::new(&pwd_val).is_dir() {
            pwd_val
        } else {
            "/".to_string()
        };

        let winsize = Winsize {
            ws_row: self.pty_size.1,
            ws_col: self.pty_size.0,
            ws_xpixel: 0,
            ws_ypixel: 0,
        };

        let pty_ends = match openpty(Some(&winsize), None) {
            Ok(ends) => ends,
            Err(e) => {
                eprintln!("Failed to allocate PTY: {e}");
                return Ok(());
            }
        };

        let master_fd = pty_ends.master;
        let slave_fd = pty_ends.slave;

        // Duplicate slave PTY endpoints safely
        let slave_stdin = Stdio::from(slave_fd.try_clone().expect("failed to dup slave PTY"));
        let slave_stdout = Stdio::from(slave_fd.try_clone().expect("failed to dup slave PTY"));
        let slave_stderr = Stdio::from(slave_fd);

        let cloned_fd = master_fd.as_fd().try_clone_to_owned()?;
        let master_file = File::from_std(std::fs::File::from(cloned_fd));

        let (master_reader, master_writer) = tokio::io::split(master_file);

        self.pty_writer = Some(Arc::new(Mutex::new(master_writer)));
        self.master_fd = Some(master_fd);

        let mut cmd = Command::new(&shell_bin);
        cmd.current_dir(work_dir)
            .envs(&self.client_env)
            .stdin(slave_stdin)
            .stdout(slave_stdout)
            .stderr(slave_stderr);

        unsafe {
            cmd.pre_exec(move || {
                setsid().map_err(|e| std::io::Error::new(std::io::ErrorKind::Other, e))?;
                Ok(())
            });
        }

        let mut child = match cmd.spawn() {
            Ok(c) => c,
            Err(e) => {
                eprintln!("Failed to spawn shell '{shell_bin}': {e}");
                return Ok(());
            }
        };

        // Output loop: Master PTY -> bytes::Bytes -> Channel handle
        let handle_out = session.handle();
        tokio::spawn(async move {
            let mut reader = master_reader;
            let mut buf = [0u8; 8192];
            while let Ok(n) = reader.read(&mut buf).await {
                if n == 0 {
                    break;
                }
                let _ = handle_out
                    .data(channel, bytes::Bytes::copy_from_slice(&buf[..n]))
                    .await;
            }
        });

        // Exit status loop
        let handle_exit = session.handle();
        tokio::spawn(async move {
            let status = child.wait();
            let code = status
                .map(|s| s.code().unwrap_or(255) as u32)
                .unwrap_or(255);
            let _ = handle_exit.exit_status_request(channel, code).await;
            let _ = handle_exit.close(channel).await;
        });

        Ok(())
    }
}

/* -------------------------------------------------------------------------- */
/* 3. Main Listener Entry Point                                               */
/* -------------------------------------------------------------------------- */

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Generate or load server host key
    let mut rng = rand::rng();

    let host_key = PrivateKey::random(&mut rng, Algorithm::Ed25519)?;

    let mut config = Config::default();
    config.keys.push(host_key);

    let config = Arc::new(config);

    let auth_file = format!("{}/.ssh/authorized_keys", env::var("HOME")?);
    let auth_keys = if let Ok(content) = std::fs::read_to_string(&auth_file) {
        content
            .lines()
            .filter_map(|l| PublicKey::from_openssh(l).ok())
            .collect()
    } else {
        Vec::new()
    };

    let mut server = AppServer {
        authorized_keys: Arc::new(auth_keys),
    };

    let socket_addr = "0.0.0.0:2222";
    let listener = TcpListener::bind(socket_addr).await?;
    println!("Starting p9cpud server on {socket_addr}...");

    while let Ok((stream, peer_addr)) = listener.accept().await {
        let config = config.clone();
        let handler = server.new_client(Some(peer_addr));

        tokio::spawn(async move {
            if let Err(e) = russh::server::run_stream(config, stream, handler).await {
                eprintln!("SSH session error from {peer_addr}: {e}");
            }
        });
    }

    Ok(())
}
