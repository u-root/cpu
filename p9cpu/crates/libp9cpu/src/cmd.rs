use thiserror::Error;

tonic::include_proto!("cmd");

#[derive(Error, Debug)]
pub enum FsTabError {
    #[error("line does not contain 6 fields.")]
    NotSixFields,
    #[error("Invalid Number for {0}: {1}")]
    InvalidNumber(&'static str, #[source] std::num::ParseIntError),
}

impl TryFrom<&str> for FsTab {
    type Error = FsTabError;
    fn try_from(line: &str) -> Result<Self, Self::Error> {
        let mut it = line.split_whitespace();
        let spec = it.next().ok_or(FsTabError::NotSixFields)?.to_string();
        let file = it.next().ok_or(FsTabError::NotSixFields)?.to_string();
        let vfstype = it.next().ok_or(FsTabError::NotSixFields)?.to_string();
        let mntops = it.next().ok_or(FsTabError::NotSixFields)?.to_string();
        let freq = it
            .next()
            .ok_or(FsTabError::NotSixFields)?
            .parse()
            .map_err(|e| FsTabError::InvalidNumber("freq", e))?;
        let passno = it
            .next()
            .ok_or(FsTabError::NotSixFields)?
            .parse()
            .map_err(|e| FsTabError::InvalidNumber("passno", e))?;
        if it.next().is_some() {
            return Err(FsTabError::NotSixFields);
        }
        Ok(FsTab {
            spec,
            file,
            vfstype,
            mntops,
            freq,
            passno,
        })
    }
}

pub struct Command {
    pub(crate) cmd: Cmd,
}

impl Command {
    pub fn new(program: String) -> Self {
        Self {
            cmd: Cmd {
                program,
                ..Default::default()
            },
        }
    }

    pub fn args(&mut self, args: impl IntoIterator<Item = String>) -> &mut Self {
        self.cmd.args.extend(args);
        self
    }

    pub fn envs<I, K, V>(&mut self, vars: I) -> &mut Self
    where
        I: IntoIterator<Item = (K, V)>,
        K: Into<Vec<u8>>,
        V: Into<Vec<u8>>,
    {
        self.cmd.envs.extend(vars.into_iter().map(|(k, v)| EnvVar {
            key: k.into(),
            val: v.into(),
        }));
        self
    }

    pub fn fstab(&mut self, tab: impl IntoIterator<Item = FsTab>) -> &mut Self {
        self.cmd.fstab.extend(tab);
        self
    }

    pub fn ninep(&mut self, enable: bool) -> &mut Self {
        self.cmd.ninep = enable;
        self
    }

    pub fn tty(&mut self, enable: bool) -> &mut Self {
        self.cmd.tty = enable;
        self
    }

    pub fn tmp_mnt(&mut self, tmp_mnt: String) -> &mut Self {
        self.cmd.tmp_mnt = tmp_mnt;
        self
    }

    pub fn ugid(&mut self, uid: u32, gid: u32) -> &mut Self {
        self.cmd.uid = uid;
        self.cmd.gid = gid;
        self
    }
}
