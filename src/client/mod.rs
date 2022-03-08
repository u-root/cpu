extern crate thrussh;
extern crate thrussh_keys;
extern crate futures;
extern crate tokio;
extern crate env_logger;
use std::sync::Arc;
use thrussh::*;
use thrussh::server::{Auth, Session};
use thrussh_keys::*;
use futures::Future;
use std::io::Read;
use std::process::exit;

struct Client {
}

impl client::Handler for Client {
   type Error = anyhow::Error;
   type FutureUnit = futures::future::Ready<Result<(Self, client::Session), anyhow::Error>>;
   type FutureBool = futures::future::Ready<Result<(Self, bool), anyhow::Error>>;

   fn finished_bool(self, b: bool) -> Self::FutureBool {
       futures::future::ready(Ok((self, b)))
   }
   fn finished(self, session: client::Session) -> Self::FutureUnit {
       futures::future::ready(Ok((self, session)))
   }
   fn check_server_key(self, server_public_key: &key::PublicKey) -> Self::FutureBool {
       println!("check_server_key: {:?}", server_public_key);
       self.finished_bool(true)
   }
   fn channel_open_confirmation(self, channel: ChannelId, max_packet_size: u32, window_size: u32, session: client::Session) -> Self::FutureUnit {
       println!("channel_open_confirmation: {:?}", channel);
       self.finished(session)
   }
   fn data(self, channel: ChannelId, data: &[u8], session: client::Session) -> Self::FutureUnit {
       println!("data on channel {:?}: {:?}", channel, std::str::from_utf8(data));
       self.finished(session)
   }
}

#[tokio::main]
pub async fn ssh() {
  let config = thrussh::client::Config::default();
  let config = Arc::new(config);
  let sh = Client{};

    let key = thrussh_keys::key::KeyPair::generate_ed25519().unwrap();
    println!("key is {:?}", key);
  let mut agent = thrussh_keys::agent::client::AgentClient::connect_env().await.unwrap();
      agent.add_identity(&key, &[]).await.unwrap();
    let ids = agent.request_identities();
    let keys;
    match ids.await {
        Err(e) => {
            println!("Failed to get ids: {}", e);
            exit(3);
        },
        Ok(v) => {
            println!("ids are {:?}", v);
            keys = v;
        },
    }
            
  let mut session = thrussh::client::connect(config, "192.168.86.165:22", sh).await.unwrap();
  let user:String;
  match std::env::var("USER") {
    Ok(val) => user = val,
    Err(e) => {
        user = "root".to_string();
        println!("No USER set({}); going with {}", e, user);
    },
  }
//    for key in keys {
        let r = session.authenticate_future(user, key.clone_public_key(), agent).await;
        match r.1 {
            Err(e) => {
                println!("auth awa error {}", e)
            },
            _ => {
                
                let r=  session.channel_open_session().await;
                match r {
                    Err(e) => {
                        println!("open session fail {}", e);
                        std::process::exit(2);
                    },
                    Ok(mut channel) => {
                        let r = channel.data(&b"Hello, world!"[..]).await;
                        match r {
                            Err(e) => {
                                println!("channel data fail {}", e);
                                std::process::exit(3);
                            },
                            _ => {
                                if let Some(msg) = channel.wait().await {
                                    println!("{:?}", msg)
                                }
                            },
                        }
                    },
                }
            },
        }
//    }
}
