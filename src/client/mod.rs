use anyhow::Result;
use async_trait::async_trait;
use log::{info, trace, warn};
// use futures::Future;
use russh::*;
// use russh::server::{Auth, Session};
use russh_keys::*;
use std::io::{Read, Write};
use std::sync::Arc;

struct Client {}

#[async_trait]
impl russh::client::Handler for Client {
    type Error = russh::Error;

    /*
    fn finished(self, session: client::Session) -> Self::FutureUnit {
        println!("FINISHED");
        futures::future::ready(Ok((self, session)))
    }
    */

    async fn check_server_key(
        &mut self,
        server_public_key: &key::PublicKey,
    ) -> Result<bool, Self::Error> {
        let key = server_public_key.public_key_base64();
        println!("check_server_key: {key}");
        // TODO: compare against preshared key?
        Ok(true)
    }

    /*
    // FIXME: this here makes the session hang
    fn channel_open_confirmation(
        self,
        channel: ChannelId,
        max_packet_size: u32,
        window_size: u32,
        session: client::Session,
    ) -> Self::FutureUnit {
        println!("channel_open_confirmation: {:?}", channel);
        self.finished(session)
    }
    fn data(self, channel: ChannelId, data: &[u8], session: client::Session) -> Self::FutureUnit {
        println!(
            "data on channel {:?}: {:?}",
            channel,
            std::str::from_utf8(data)
        );
        self.finished(session)
    }
    */
}

// from https://nest.pijul.com/pijul/russh/discussions/20
pub struct Session {
    session: client::Handle<Client>,
}

impl Session {
    async fn connect(
        key_file: &str,
        user: impl Into<String>,
        addrs: impl tokio::net::ToSocketAddrs,
    ) -> Result<Self> {
        let key_pair = russh_keys::load_secret_key(key_file, None)?;
        let config = russh::client::Config {
            inactivity_timeout: Some(std::time::Duration::from_secs(3)),
            ..<_>::default()
        };
        let config = Arc::new(config);
        let sh = Client {};

        let mut session = client::connect(config, addrs, sh).await?;
        let auth_res = session
            .authenticate_publickey(user, Arc::new(key_pair))
            .await?;

        if !auth_res {
            anyhow::bail!("Authentication failed");
        }

        /*
        let mut agent = agent::client::AgentClient::connect_env().await?;
        trace!("add key to agent");
        agent.add_identity(&key_pair, &[]).await?;
        trace!("request identities");
        let mut identities = agent.request_identities().await?;
        trace!("start session");
        let mut session = client::connect(config, addr, sh).await?;
        let pubkey = identities.pop().unwrap();
        trace!("start authentication");
        let (_, auth_res) = session.authenticate_future(user, pubkey, agent).await;
        let _auth_res = auth_res?;
        */

        Ok(Self { session })
    }

    async fn call(&mut self, command: &str) -> Result<u32> {
        let mut channel = self.session.channel_open_session().await?;
        channel.exec(true, command).await?;

        let mut code = None;
        use tokio::io::AsyncWriteExt;
        let mut stdout = tokio::io::stdout();

        loop {
            // There's an event available on the session channel
            let Some(msg) = channel.wait().await else {
                break;
            };
            match msg {
                // Write data to the terminal
                ChannelMsg::Data { ref data } => {
                    // info!("DATA {}", data);
                    stdout.write_all(data).await?;
                    stdout.flush().await?;
                }
                // The command has returned an exit code
                ChannelMsg::ExitStatus { exit_status } => {
                    code = Some(exit_status);
                    // cannot leave the loop immediately, there might still be more data to receive
                }
                _ => {}
            }
        }

        /*
        let mut output = Vec::new();
        let mut code = None;
        while let Some(msg) = channel.wait().await {
            match msg {
                russh::ChannelMsg::Data { ref data } => {
                    output.write_all(&data).unwrap();
                }
                russh::ChannelMsg::ExitStatus { exit_status } => {
                    code = Some(exit_status);
                }
                _ => {}
            }
        }
        */
        // Ok(CommandResult { output, code })

        Ok(code.expect("program did not exit cleanly"))
    }

    async fn close(&mut self) -> Result<()> {
        self.session
            .disconnect(Disconnect::ByApplication, "", "English")
            .await?;
        Ok(())
    }
}

struct CommandResult {
    output: Vec<u8>,
    code: Option<u32>,
}

impl CommandResult {
    fn output(&self) -> String {
        String::from_utf8_lossy(&self.output).into()
    }

    fn success(&self) -> bool {
        self.code == Some(0)
    }
}

#[tokio::main]
pub async fn cpu(key_file: &str, host: &str, command: &str) -> Result<()> {
    let user = match std::env::var("USER") {
        Ok(val) => val,
        Err(e) => {
            let user = "root".to_string();
            warn!("No USER set({e}); going with {user}");
            user
        }
    };

    info!("Let's connect {host}");
    let mut ssh = Session::connect(key_file, user, host).await?;
    info!("Let's run a command {:?}", command);
    let r = ssh.call(command).await?;
    // info!("{:?} {:?}", r.output(), r.success());
    // assert!(r.success());
    // info!("Who am I, anyway? {:?}", r.output());
    ssh.close().await?;
    Ok(())
}

/*
#[derive(Clone)]
struct Server {
    client_pubkey: Arc<russh_keys::key::PublicKey>,
    clients: Arc<Mutex<HashMap<(usize, ChannelId), russh::server::Handle>>>,
    id: usize,
}

impl server::Server for Server {
    type Handler = Self;
    fn new(&mut self, _: Option<std::net::SocketAddr>) -> Self {
        let s = self.clone();
        self.id += 1;
        s
    }
}

impl server::Handler for Server {
    type Error = anyhow::Error;
    type FutureAuth = futures::future::Ready<Result<(Self, server::Auth), anyhow::Error>>;
    type FutureUnit = futures::future::Ready<Result<(Self, Session), anyhow::Error>>;
    type FutureBool = futures::future::Ready<Result<(Self, Session, bool), anyhow::Error>>;

    fn finished_auth(mut self, auth: Auth) -> Self::FutureAuth {
        futures::future::ready(Ok((self, auth)))
    }
    fn finished_bool(self, b: bool, s: Session) -> Self::FutureBool {
        futures::future::ready(Ok((self, s, b)))
    }
    fn finished(self, s: Session) -> Self::FutureUnit {
        futures::future::ready(Ok((self, s)))
    }
    fn channel_open_session(self, channel: ChannelId, session: Session) -> Self::FutureUnit {
        {
            let mut clients = self.clients.lock().unwrap();
            clients.insert((self.id, channel), session.handle());
        }
        self.finished(session)
    }
    fn auth_publickey(self, _: &str, _: &key::PublicKey) -> Self::FutureAuth {
        self.finished_auth(server::Auth::Accept)
    }
    fn data(self, channel: ChannelId, data: &[u8], mut session: Session) -> Self::FutureUnit {
        {
            let mut clients = self.clients.lock().unwrap();
            for ((id, channel), ref mut s) in clients.iter_mut() {
                if *id != self.id {
                    s.data(*channel, CryptoVec::from_slice(data));
                }
            }
        }
        session.data(channel, CryptoVec::from_slice(data));
        self.finished(session)
    }
}

#[tokio::main]
async fn main() {
    let client_key = russh_keys::key::KeyPair::generate_ed25519().unwrap();
    let client_pubkey = Arc::new(client_key.clone_public_key());
    let mut config = russh::server::Config::default();
    config.connection_timeout = Some(std::time::Duration::from_secs(3));
    config.auth_rejection_time = std::time::Duration::from_secs(3);
    config.keys.push(russh_keys::key::KeyPair::generate_ed25519().unwrap());
    let config = Arc::new(config);
    let sh = Server{
        client_pubkey,
        clients: Arc::new(Mutex::new(HashMap::new())),
        id: 0
    };
    tokio::time::timeout(
       std::time::Duration::from_secs(1),
       russh::server::run(config, "0.0.0.0:2222", sh)
    ).await.unwrap_or(Ok(()));
}
*/
