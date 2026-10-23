//! Fake endpoint servers for the unit tests of `conn` and `link`. They run in
//! this process on a socket in a temp dir. They never touch herdr.

#![allow(clippy::unwrap_used, clippy::expect_used)]

use std::os::unix::net::{UnixListener, UnixStream};
use std::path::PathBuf;
use std::thread::{self, JoinHandle};

use super::codec::{Dec, Enc, read_frame, write_frame};
use super::messages::{
    BLOB_CODEC, C_CONTROL, HELLO_KIND, INPUT_CODEC, S_CONTROL, S_WELCOME_LEGACY, SNAPSHOT_CODEC,
    SURFACE_CODEC, WELCOME_KIND,
};

/// A fake endpoint in a short temp dir (the socket path must stay under
/// 104 bytes). The thread runs `script` on the first client that connects.
pub(crate) fn fake_herdr<T: Send + 'static>(
    script: impl FnOnce(UnixStream) -> T + Send + 'static,
) -> (tempfile::TempDir, PathBuf, JoinHandle<T>) {
    let dir = tempfile::Builder::new()
        .prefix("ce")
        .tempdir_in("/tmp")
        .unwrap();
    let path = dir.path().join("herdr-client.sock");
    let listener = UnixListener::bind(&path).unwrap();
    let handle = thread::spawn(move || {
        let (stream, _) = listener.accept().unwrap();
        script(stream)
    });
    (dir, path, handle)
}

/// Reads the client's hello and checks its kind.
pub(crate) fn read_hello(stream: &mut UnixStream) {
    let payload = read_frame(stream).unwrap().unwrap();
    let mut d = Dec::new(&payload);
    assert_eq!(d.vu().unwrap(), u64::from(C_CONTROL));
    assert_eq!(d.str().unwrap(), HELLO_KIND);
}

/// Writes one frame.
pub(crate) fn put(stream: &mut UnixStream, payload: &[u8]) {
    write_frame(stream, payload).unwrap();
}

/// A server `EndpointControl` payload.
pub(crate) fn control(kind: &str, data: &str) -> Vec<u8> {
    let mut e = Enc::default();
    e.vu(u64::from(S_CONTROL));
    e.str(kind);
    e.str(data);
    e.buf
}

pub(crate) fn welcome_json(generation: u32, version: &str, extra: &str) -> String {
    format!(
        r#"{{"generation":{generation},"server_version":"{version}","snapshot_codec":"{SNAPSHOT_CODEC}","surface_codec":"{SURFACE_CODEC}","input_codec":"{INPUT_CODEC}","blob_codec":"{BLOB_CODEC}"{extra}}}"#
    )
}

/// The v1 welcome as herdr sends it after a good hello.
pub(crate) fn welcome(version: &str) -> Vec<u8> {
    control(WELCOME_KIND, &welcome_json(1, version, ""))
}

/// herdr's pre-generation-1 answer: a legacy `Welcome` (tag 0).
pub(crate) fn legacy_welcome(error: Option<&str>) -> Vec<u8> {
    let mut e = Enc::default();
    e.vu(u64::from(S_WELCOME_LEGACY));
    e.vu(1); // version
    e.vu(0); // render encoding
    match error {
        Some(text) => {
            e.some();
            e.str(text);
        }
        None => e.none(),
    }
    e.buf
}
