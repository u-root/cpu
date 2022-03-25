extern crate env_logger;
extern crate futures;
extern crate thrussh;
extern crate thrussh_keys;
extern crate tokio;
use anyhow::Result;
use futures::Future;
use std::io::{Read, Write};
use std::sync::Arc;
use thrussh::*;
// use thrussh::server::{Auth, Session};
use thrussh_keys::*;

struct Client {}

impl client::Handler for Client {
    type Error = thrussh::Error;
    type FutureUnit = futures::future::Ready<Result<(Self, client::Session), Self::Error>>;
    type FutureBool = futures::future::Ready<Result<(Self, bool), Self::Error>>;

    fn finished_bool(self, b: bool) -> Self::FutureBool {
        futures::future::ready(Ok((self, b)))
    }
    fn finished(self, session: client::Session) -> Self::FutureUnit {
        println!("FINISHED");
        futures::future::ready(Ok((self, session)))
    }
    fn check_server_key(self, server_public_key: &key::PublicKey) -> Self::FutureBool {
        println!(
            "check_server_key: {:?}",
            server_public_key.public_key_base64()
        );
        // TODO: compare against preshared key?
        self.finished_bool(true)
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

// from https://nest.pijul.com/pijul/thrussh/discussions/20
pub struct Session {
    session: client::Handle<Client>,
}

impl Session {
    async fn connect(
        key_file: &str,
        user: impl Into<String>,
        addr: impl std::net::ToSocketAddrs,
    ) -> Result<Self> {
        let key_pair = thrussh_keys::load_secret_key(key_file, None)?;
        // TODO: import openssl for RSA key support
        /*
        let key_pair = key::KeyPair::RSA {
            key: openssl::rsa::Rsa::private_key_from_pem(pem)?,
            hash: key::SignatureHash::SHA2_512,
        };
        */
        let mut config = client::Config::default();
        config.connection_timeout = Some(std::time::Duration::from_secs(3));
        let config = Arc::new(config);
        let sh = Client {};
        let mut agent = agent::client::AgentClient::connect_env().await?;
        agent.add_identity(&key_pair, &[]).await?;
        let mut identities = agent.request_identities().await?;
        let mut session = client::connect(config, addr, sh).await?;
        let pubkey = identities.pop().unwrap();
        let (_, auth_res) = session.authenticate_future(user, pubkey, agent).await;
        let _auth_res = auth_res?;
        Ok(Self { session })
    }

    async fn call(&mut self, command: &str) -> Result<CommandResult> {
        let mut channel = self.session.channel_open_session().await?;
        channel.exec(true, command).await?;
        let mut output = Vec::new();
        let mut code = None;
        while let Some(msg) = channel.wait().await {
            match msg {
                thrussh::ChannelMsg::Data { ref data } => {
                    output.write_all(&data).unwrap();
                }
                thrussh::ChannelMsg::ExitStatus { exit_status } => {
                    code = Some(exit_status);
                }
                _ => {}
            }
        }
        Ok(CommandResult { output, code })
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
pub async fn ssh() -> Result<()> {
    let host = "localhost:2342";
    let key_file = "/home/dama/.ssh/id_ed25519";
    let command = "/bbin/ls";

    let user: String;
    match std::env::var("USER") {
        Ok(val) => user = val,
        Err(e) => {
            user = "root".to_string();
            println!("No USER set({}); going with {}", e, user);
        }
    }

    println!("Let's connect {:?}", host);
    let mut ssh = Session::connect(key_file, user, host).await?;
    println!("Let's run a command {:?}", command);
    let r = ssh.call(command).await?;
    assert!(r.success());
    println!("Who am I, anyway? {:?}", r.output());
    ssh.close().await?;
    Ok(())
}

/*
#[derive(Clone)]
struct Server {
    client_pubkey: Arc<thrussh_keys::key::PublicKey>,
    clients: Arc<Mutex<HashMap<(usize, ChannelId), thrussh::server::Handle>>>,
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
    let client_key = thrussh_keys::key::KeyPair::generate_ed25519().unwrap();
    let client_pubkey = Arc::new(client_key.clone_public_key());
    let mut config = thrussh::server::Config::default();
    config.connection_timeout = Some(std::time::Duration::from_secs(3));
    config.auth_rejection_time = std::time::Duration::from_secs(3);
    config.keys.push(thrussh_keys::key::KeyPair::generate_ed25519().unwrap());
    let config = Arc::new(config);
    let sh = Server{
        client_pubkey,
        clients: Arc::new(Mutex::new(HashMap::new())),
        id: 0
    };
    tokio::time::timeout(
       std::time::Duration::from_secs(1),
       thrussh::server::run(config, "0.0.0.0:2222", sh)
    ).await.unwrap_or(Ok(()));
}
*/
