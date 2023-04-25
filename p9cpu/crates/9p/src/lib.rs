//! Filesystems library using 9P2000.L protocol, an extended variant of 9P from Plan 9.
//!
//! 9P protocol is originally developed for Plan 9 distributed OS.
//! As it's extendable and suitable for filesystems 9P is ported to Linux.
//! However, 9P protocol lacks Linux or Unix specific features,
//! which is the problem for developing serious filesystems.
//!
//! 9P2000.L is an extended variant protocol of 9P for Linux.
//! It has Linux specific features and is supported by Linux kernel 9P module.
//!
//! rs9p is a library to develop 9P2000.L virtual filesystems in Rust.
//! All you have to do is to implement `Filesystem` trait.

#[macro_use]
extern crate log;

#[macro_use]
mod utils;
pub mod error;
pub mod fcall;
pub mod serialize;
pub mod srv;

pub use crate::error::errno;
pub use crate::error::string as errstr;
pub use crate::error::Error;
pub use crate::fcall::*;
pub use crate::utils::Result;
