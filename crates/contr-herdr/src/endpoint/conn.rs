//! Connection to herdr's endpoint socket: connect, hello, the welcome check,
//! then framed reads and writes. Written from herdr's wire layout; see NOTICE.

use std::error::Error;
use std::fmt;
use std::io::{self, Read};
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use tracing::{info, warn};

use super::codec::{CodecError, write_frame};
use super::messages::{
    CellPx, ClientMsg, ENDPOINT_GENERATION, GridSize, MAX_FRAME, ServerMsg, VersionCheck,
    WELCOME_KIND, Welcome, check_welcome, decode_server,
};

/// herdr's endpoint socket for this user, found the way herdr finds it.
pub fn endpoint_socket_path() -> Option<PathBuf> {
    endpoint_path_from(|key| std::env::var(key).ok())
}

/// Testable core of [`endpoint_socket_path`]. Empty values count as unset.
/// `HERDR_SOCKET_PATH` gives `<stem>-client.sock` in its directory. Otherwise
/// `HERDR_CLIENT_SOCKET_PATH`. Otherwise the default API socket's directory
/// with the file name `herdr-client.sock`.
pub(crate) fn endpoint_path_from(env: impl Fn(&str) -> Option<String>) -> Option<PathBuf> {
    let value = |key: &str| env(key).filter(|v| !v.is_empty());
    if let Some(api) = value("HERDR_SOCKET_PATH") {
        let api = PathBuf::from(api);
        let stem = api.file_stem().and_then(|s| s.to_str()).unwrap_or("herdr");
        let dir = api.parent().unwrap_or_else(|| Path::new(""));
        return Some(dir.join(format!("{stem}-client.sock")));
    }
    if let Some(path) = value("HERDR_CLIENT_SOCKET_PATH") {
        return Some(PathBuf::from(path));
    }
    crate::wire::socket_path_from(env).map(|api| api.with_file_name("herdr-client.sock"))
}

/// Why a connection to herdr's endpoint socket failed.
#[derive(Debug)]
pub enum ConnError {
    /// Nothing listens there, or herdr closed or stayed silent before its welcome.
    NotRunning(String),
    /// herdr refused contr, or sent something contr cannot read. Retrying the same way will fail the same way.
    Incompatible(String),
    /// A read or write failed. A read that ends with `UnexpectedEof` means herdr closed the socket.
    Io(io::Error),
    /// A frame did not decode after the handshake. The connection must be dropped.
    Codec(CodecError),
}

impl fmt::Display for ConnError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NotRunning(why) | Self::Incompatible(why) => f.write_str(why),
            Self::Io(e) => write!(f, "endpoint socket: {e}"),
            Self::Codec(e) => write!(f, "{e}"),
        }
    }
}

impl Error for ConnError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        match self {
            Self::Io(e) => Some(e),
            Self::Codec(e) => Some(e),
            Self::NotRunning(_) | Self::Incompatible(_) => None,
        }
    }
}

impl From<CodecError> for ConnError {
    fn from(e: CodecError) -> Self {
        Self::Codec(e)
    }
}

/// An endpoint connection that passed the welcome check.
#[derive(Debug)]
pub struct Conn {
    stream: UnixStream,
    inbox: Inbox,
    pub welcome: Welcome,
    pub version: VersionCheck,
}

impl Conn {
    /// Connects, sends the hello and reads the welcome. `timeout` bounds the
    /// whole handshake. No surface is requested yet. The read timeout is left
    /// at the last value the handshake set (at most `timeout`), so call
    /// [`Conn::set_read_timeout`] before `recv`. The handshake makes no socket
    /// call after the welcome: on macOS, setting a timeout on a socket whose
    /// peer already closed fails with `EINVAL`.
    pub fn open(
        path: &Path,
        size: GridSize,
        cell: CellPx,
        timeout: Duration,
    ) -> Result<Conn, ConnError> {
        let mut stream = UnixStream::connect(path).map_err(|e| match e.kind() {
            io::ErrorKind::NotFound | io::ErrorKind::ConnectionRefused => {
                ConnError::NotRunning(format!("herdr is not running: {e}"))
            }
            _ => ConnError::Io(e),
        })?;
        stream
            .set_write_timeout(Some(timeout))
            .map_err(ConnError::Io)?;
        write_frame(&mut stream, &ClientMsg::Hello { size, cell }.encode())
            .map_err(ConnError::Io)?;

        let mut inbox = Inbox::default();
        let deadline = Instant::now() + timeout;
        let first = loop {
            let left = deadline.saturating_duration_since(Instant::now());
            if left.is_zero() {
                return Err(ConnError::NotRunning(format!(
                    "herdr sent no welcome within {timeout:?}"
                )));
            }
            stream.set_read_timeout(Some(left)).map_err(ConnError::Io)?;
            match read_payload(&mut stream, &mut inbox) {
                Ok(Some(payload)) => break payload,
                // A read timeout: the deadline check at the top ends the wait.
                Ok(None) => {}
                Err(ConnError::Io(e)) if e.kind() == io::ErrorKind::UnexpectedEof => {
                    return Err(ConnError::NotRunning(
                        "herdr closed the endpoint before its welcome".to_owned(),
                    ));
                }
                Err(ConnError::Io(e)) if e.kind() == io::ErrorKind::InvalidData => {
                    return Err(ConnError::Incompatible(format!(
                        "herdr sent a frame contr cannot read: {e}"
                    )));
                }
                Err(e) => return Err(e),
            }
        };

        let welcome: Welcome = match decode_server(&first).map_err(|e| {
            ConnError::Incompatible(format!("herdr's first message does not decode: {e}"))
        })? {
            ServerMsg::Control { kind, data } if kind == WELCOME_KIND => {
                serde_json::from_str(&data).map_err(|e| {
                    ConnError::Incompatible(format!("herdr's welcome is not readable: {e}"))
                })?
            }
            ServerMsg::LegacyWelcome { error } => {
                return Err(ConnError::Incompatible(
                    error.unwrap_or_else(|| "herdr refused the endpoint hello".to_owned()),
                ));
            }
            _ => {
                return Err(ConnError::Incompatible(
                    "herdr's first message is not a welcome".to_owned(),
                ));
            }
        };
        let version = check_welcome(&welcome).map_err(ConnError::Incompatible)?;

        info!(
            "herdr endpoint connected server_version={} generation={}",
            welcome.server_version, ENDPOINT_GENERATION
        );
        if let VersionCheck::Untested(v) = &version {
            warn!("herdr endpoint {v} not tested with contr; wire layout may differ");
        }
        Ok(Conn {
            stream,
            inbox,
            welcome,
            version,
        })
    }

    /// Writes one client message as one frame.
    pub fn send(&mut self, msg: &ClientMsg<'_>) -> Result<(), ConnError> {
        write_frame(&mut self.stream, &msg.encode()).map_err(ConnError::Io)
    }

    /// The next server message. `Ok(None)` when the read timeout passed before a
    /// whole frame arrived. The bytes read so far are kept, and the next call
    /// finishes that frame. A frame that does not decode is `Err(Codec)`.
    pub fn recv(&mut self) -> Result<Option<ServerMsg>, ConnError> {
        match read_payload(&mut self.stream, &mut self.inbox)? {
            Some(payload) => Ok(Some(decode_server(&payload)?)),
            None => Ok(None),
        }
    }

    pub fn set_read_timeout(&self, t: Option<Duration>) -> io::Result<()> {
        self.stream.set_read_timeout(t)
    }

    /// A second handle to the socket, for writes from another thread.
    pub fn try_clone_writer(&self) -> io::Result<UnixStream> {
        self.stream.try_clone()
    }
}

/// The next whole frame's payload. `Ok(None)` on a read timeout; the bytes read
/// so far stay in `inbox`. EOF is `UnexpectedEof`.
fn read_payload(r: &mut impl Read, inbox: &mut Inbox) -> Result<Option<Vec<u8>>, ConnError> {
    loop {
        if let Some(payload) = inbox.take_frame().map_err(ConnError::Io)? {
            return Ok(Some(payload));
        }
        match inbox.fill(r).map_err(ConnError::Io)? {
            Fill::Read => {}
            Fill::Timeout => return Ok(None),
            Fill::Eof => {
                return Err(ConnError::Io(io::Error::new(
                    io::ErrorKind::UnexpectedEof,
                    "herdr closed the endpoint",
                )));
            }
        }
    }
}

/// Bytes read from the socket that do not make a whole frame yet. `read_frame`
/// cannot resume after a timeout, so a timeout leaves the partial frame here.
#[derive(Debug, Default)]
struct Inbox {
    buf: Vec<u8>,
}

/// What one read from the socket gave.
enum Fill {
    Read,
    Timeout,
    Eof,
}

impl Inbox {
    /// Cuts the first whole frame off the buffer. `Ok(None)` while it is incomplete.
    fn take_frame(&mut self) -> io::Result<Option<Vec<u8>>> {
        let Some(header) = self.buf.first_chunk::<4>() else {
            return Ok(None);
        };
        let len = u32::from_le_bytes(*header) as usize;
        if len > MAX_FRAME {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "endpoint frame larger than MAX_FRAME",
            ));
        }
        let end = 4 + len;
        if self.buf.len() < end {
            return Ok(None);
        }
        let rest = self.buf.split_off(end);
        let mut frame = std::mem::replace(&mut self.buf, rest);
        frame.drain(..4);
        Ok(Some(frame))
    }

    /// One read from `r`. Interrupted reads are retried. A timeout and EOF are
    /// reported as values, not errors.
    fn fill(&mut self, r: &mut impl Read) -> io::Result<Fill> {
        let mut chunk = [0_u8; 8192];
        loop {
            match r.read(&mut chunk) {
                Ok(0) => return Ok(Fill::Eof),
                Ok(n) => {
                    self.buf.extend_from_slice(&chunk[..n]);
                    return Ok(Fill::Read);
                }
                Err(e) if e.kind() == io::ErrorKind::Interrupted => {}
                Err(e)
                    if matches!(
                        e.kind(),
                        io::ErrorKind::WouldBlock | io::ErrorKind::TimedOut
                    ) =>
                {
                    return Ok(Fill::Timeout);
                }
                Err(e) => return Err(e),
            }
        }
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use std::collections::VecDeque;
    use std::io::Write;
    use std::os::unix::net::UnixListener;
    use std::sync::{Arc, Mutex};
    use std::thread::{self, JoinHandle};

    use tracing_subscriber::fmt::MakeWriter;

    use super::super::codec::{Dec, Enc, read_frame};
    use super::super::messages::{
        BLOB_CODEC, C_CONTROL, HELLO_KIND, INPUT_CODEC, S_CONTROL, S_WELCOME_LEGACY,
        SNAPSHOT_CODEC, SNAPSHOT_KIND, SURFACE_CODEC,
    };
    use super::*;

    const SIZE: GridSize = GridSize { cols: 60, rows: 12 };
    const CELL: CellPx = CellPx {
        width: 8,
        height: 16,
    };
    const WAIT: Duration = Duration::from_secs(2);

    fn env_of(pairs: &[(&str, &str)]) -> impl Fn(&str) -> Option<String> {
        let owned: Vec<(String, String)> = pairs
            .iter()
            .map(|(k, v)| ((*k).to_owned(), (*v).to_owned()))
            .collect();
        move |key| owned.iter().find(|(k, _)| k == key).map(|(_, v)| v.clone())
    }

    #[test]
    fn endpoint_path_follows_the_api_socket_name() {
        assert_eq!(
            endpoint_path_from(env_of(&[("HERDR_SOCKET_PATH", "/s/herdr.sock")])),
            Some(PathBuf::from("/s/herdr-client.sock"))
        );
        assert_eq!(
            endpoint_path_from(env_of(&[("HERDR_SOCKET_PATH", "/s/a.sock")])),
            Some(PathBuf::from("/s/a-client.sock"))
        );
    }

    #[test]
    fn endpoint_path_stem_defaults_to_herdr_without_a_file_name() {
        assert_eq!(
            endpoint_path_from(env_of(&[("HERDR_SOCKET_PATH", "/s/..")])),
            Some(PathBuf::from("/s/herdr-client.sock"))
        );
    }

    #[test]
    fn endpoint_path_uses_the_client_variable_when_no_api_socket_is_set() {
        assert_eq!(
            endpoint_path_from(env_of(&[("HERDR_CLIENT_SOCKET_PATH", "/c/x.sock")])),
            Some(PathBuf::from("/c/x.sock"))
        );
        // An empty API socket counts as unset.
        assert_eq!(
            endpoint_path_from(env_of(&[
                ("HERDR_SOCKET_PATH", ""),
                ("HERDR_CLIENT_SOCKET_PATH", "/c/x.sock"),
            ])),
            Some(PathBuf::from("/c/x.sock"))
        );
        // The API socket wins over the client variable.
        assert_eq!(
            endpoint_path_from(env_of(&[
                ("HERDR_SOCKET_PATH", "/s/a.sock"),
                ("HERDR_CLIENT_SOCKET_PATH", "/c/x.sock"),
            ])),
            Some(PathBuf::from("/s/a-client.sock"))
        );
    }

    #[test]
    fn endpoint_path_falls_back_to_the_default_api_directory() {
        assert_eq!(
            endpoint_path_from(env_of(&[("HOME", "/h")])),
            Some(PathBuf::from("/h/.config/herdr/herdr-client.sock"))
        );
        assert_eq!(
            endpoint_path_from(env_of(&[("XDG_CONFIG_HOME", "/x"), ("HOME", "/h")])),
            Some(PathBuf::from("/x/herdr/herdr-client.sock"))
        );
    }

    #[test]
    fn endpoint_path_is_none_without_any_setting() {
        assert_eq!(endpoint_path_from(env_of(&[])), None);
    }

    /// A fake endpoint in a short temp dir (the socket path must stay under
    /// 104 bytes). The thread runs `script` on the first client that connects.
    fn fake_herdr<T: Send + 'static>(
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
    fn read_hello(stream: &mut UnixStream) {
        let payload = read_frame(stream).unwrap().unwrap();
        let mut d = Dec::new(&payload);
        assert_eq!(d.vu().unwrap(), u64::from(C_CONTROL));
        assert_eq!(d.str().unwrap(), HELLO_KIND);
    }

    fn put(stream: &mut UnixStream, payload: &[u8]) {
        write_frame(stream, payload).unwrap();
    }

    /// A server `EndpointControl` payload.
    fn control(kind: &str, data: &str) -> Vec<u8> {
        let mut e = Enc::default();
        e.vu(u64::from(S_CONTROL));
        e.str(kind);
        e.str(data);
        e.buf
    }

    fn welcome_json(generation: u32, version: &str, extra: &str) -> String {
        format!(
            r#"{{"generation":{generation},"server_version":"{version}","snapshot_codec":"{SNAPSHOT_CODEC}","surface_codec":"{SURFACE_CODEC}","input_codec":"{INPUT_CODEC}","blob_codec":"{BLOB_CODEC}"{extra}}}"#
        )
    }

    /// The v1 welcome as herdr sends it after a good hello.
    fn welcome(version: &str) -> Vec<u8> {
        control(WELCOME_KIND, &welcome_json(1, version, ""))
    }

    /// herdr's pre-generation-1 answer: a legacy `Welcome` (tag 0).
    fn legacy_welcome(error: Option<&str>) -> Vec<u8> {
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

    fn expect_err(opened: Result<Conn, ConnError>) -> ConnError {
        match opened {
            Ok(conn) => panic!("open succeeded: {conn:?}"),
            Err(e) => e,
        }
    }

    /// Log lines written while `f` runs, from the thread that runs `f`.
    #[derive(Clone, Default)]
    struct Logs(Arc<Mutex<Vec<u8>>>);

    impl Write for Logs {
        fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
            self.0.lock().unwrap().extend_from_slice(buf);
            Ok(buf.len())
        }

        fn flush(&mut self) -> io::Result<()> {
            Ok(())
        }
    }

    impl<'a> MakeWriter<'a> for Logs {
        type Writer = Logs;

        fn make_writer(&'a self) -> Logs {
            self.clone()
        }
    }

    fn capture<T>(f: impl FnOnce() -> T) -> (T, String) {
        let logs = Logs::default();
        let subscriber = tracing_subscriber::fmt()
            .with_ansi(false)
            .without_time()
            .with_writer(logs.clone())
            .finish();
        let out = tracing::subscriber::with_default(subscriber, f);
        let text = String::from_utf8(logs.0.lock().unwrap().clone()).unwrap();
        (out, text)
    }

    #[test]
    fn open_accepts_a_v1_welcome_after_the_hello() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
        });
        let (opened, logs) = capture(|| Conn::open(&path, SIZE, CELL, WAIT));
        let conn = opened.unwrap();
        assert_eq!(conn.version, VersionCheck::Ok);
        assert_eq!(conn.welcome.generation, 1);
        assert_eq!(conn.welcome.server_version, "0.9.3");
        assert!(
            logs.contains("herdr endpoint connected server_version=0.9.3 generation=1"),
            "{logs}"
        );
        assert!(!logs.contains("not tested"), "{logs}");
        server.join().unwrap();
    }

    #[test]
    fn open_connects_and_warns_on_an_untested_version() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.9"));
        });
        let (opened, logs) = capture(|| Conn::open(&path, SIZE, CELL, WAIT));
        let conn = opened.unwrap();
        assert_eq!(conn.version, VersionCheck::Untested("0.9.9".to_owned()));
        assert!(
            logs.contains("herdr endpoint 0.9.9 not tested with contr; wire layout may differ"),
            "{logs}"
        );
        server.join().unwrap();
    }

    #[test]
    fn open_refuses_a_welcome_with_an_error() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            let json = welcome_json(
                1,
                "0.9.3",
                r#","error":{"code":"invalid_hello","message":"bad hello"}"#,
            );
            put(&mut stream, &control(WELCOME_KIND, &json));
        });
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::Incompatible(why) => {
                assert!(why.contains("invalid_hello"), "{why}");
                assert!(why.contains("bad hello"), "{why}");
            }
            other => panic!("{other:?}"),
        }
        server.join().unwrap();
    }

    #[test]
    fn open_refuses_another_generation() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(
                &mut stream,
                &control(WELCOME_KIND, &welcome_json(2, "0.9.3", "")),
            );
        });
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::Incompatible(why) => assert!(why.contains("generation 2"), "{why}"),
            other => panic!("{other:?}"),
        }
        server.join().unwrap();
    }

    #[test]
    fn open_refuses_a_changed_codec_name() {
        let json = welcome_json(1, "0.9.3", "").replace(SURFACE_CODEC, "shell.surface.v2");
        let (_dir, path, server) = fake_herdr(move |mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &control(WELCOME_KIND, &json));
        });
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::Incompatible(why) => assert!(why.contains("shell.surface.v2"), "{why}"),
            other => panic!("{other:?}"),
        }
        server.join().unwrap();
    }

    #[test]
    fn open_refuses_a_legacy_welcome_with_its_error() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &legacy_welcome(Some("client too old")));
        });
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::Incompatible(why) => assert_eq!(why, "client too old"),
            other => panic!("{other:?}"),
        }
        server.join().unwrap();
    }

    #[test]
    fn open_refuses_a_legacy_welcome_without_an_error() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &legacy_welcome(None));
        });
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::Incompatible(why) => assert_eq!(why, "herdr refused the endpoint hello"),
            other => panic!("{other:?}"),
        }
        server.join().unwrap();
    }

    #[test]
    fn open_refuses_a_first_message_that_is_not_a_welcome() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &control(SNAPSHOT_KIND, "{}"));
        });
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::Incompatible(why) => assert!(why.contains("not a welcome"), "{why}"),
            other => panic!("{other:?}"),
        }
        server.join().unwrap();
    }

    #[test]
    fn open_refuses_an_undecodable_first_frame() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &[0xFF]); // a varint marker that does not exist
        });
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::Incompatible(why) => assert!(why.contains("does not decode"), "{why}"),
            other => panic!("{other:?}"),
        }
        server.join().unwrap();
    }

    #[test]
    fn open_refuses_a_welcome_whose_json_is_unreadable() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &control(WELCOME_KIND, "{"));
        });
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::Incompatible(why) => assert!(why.contains("not readable"), "{why}"),
            other => panic!("{other:?}"),
        }
        server.join().unwrap();
    }

    #[test]
    fn open_refuses_a_first_frame_over_the_frame_limit() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            let too_long = u32::try_from(MAX_FRAME + 1).unwrap();
            stream.write_all(&too_long.to_le_bytes()).unwrap();
        });
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::Incompatible(why) => assert!(why.contains("cannot read"), "{why}"),
            other => panic!("{other:?}"),
        }
        server.join().unwrap();
    }

    #[test]
    fn open_without_a_reply_is_not_running_after_the_timeout() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            thread::sleep(Duration::from_millis(600));
        });
        let started = Instant::now();
        let err = expect_err(Conn::open(&path, SIZE, CELL, Duration::from_millis(200)));
        assert!(matches!(err, ConnError::NotRunning(_)), "{err:?}");
        assert!(started.elapsed() < Duration::from_secs(5));
        server.join().unwrap();
    }

    #[test]
    fn open_when_herdr_closes_before_the_welcome_is_not_running() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
        });
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::NotRunning(why) => assert!(why.contains("closed"), "{why}"),
            other => panic!("{other:?}"),
        }
        server.join().unwrap();
    }

    #[test]
    fn open_with_no_socket_is_not_running() {
        let dir = tempfile::Builder::new()
            .prefix("ce")
            .tempdir_in("/tmp")
            .unwrap();
        let err = expect_err(Conn::open(
            &dir.path().join("herdr-client.sock"),
            SIZE,
            CELL,
            WAIT,
        ));
        assert!(matches!(err, ConnError::NotRunning(_)), "{err:?}");
    }

    #[test]
    fn open_with_a_socket_nobody_listens_on_is_not_running() {
        let dir = tempfile::Builder::new()
            .prefix("ce")
            .tempdir_in("/tmp")
            .unwrap();
        let path = dir.path().join("herdr-client.sock");
        // Dropping the listener leaves the socket file behind: connect is refused.
        drop(UnixListener::bind(&path).unwrap());
        match expect_err(Conn::open(&path, SIZE, CELL, WAIT)) {
            ConnError::NotRunning(why) => assert!(why.contains("not running"), "{why}"),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn open_with_a_path_too_long_for_a_socket_is_an_io_error() {
        let long = PathBuf::from(format!("/tmp/{}", "a".repeat(120)));
        assert!(matches!(
            expect_err(Conn::open(&long, SIZE, CELL, WAIT)),
            ConnError::Io(_)
        ));
    }

    #[test]
    fn send_writes_one_frame_the_server_reads() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            read_frame(&mut stream).unwrap().unwrap()
        });
        let mut conn = Conn::open(&path, SIZE, CELL, WAIT).unwrap();
        conn.send(&ClientMsg::Focus(true)).unwrap();
        drop(conn);
        assert_eq!(server.join().unwrap(), ClientMsg::Focus(true).encode());
    }

    #[test]
    fn a_cloned_writer_writes_to_the_same_connection() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            read_frame(&mut stream).unwrap().unwrap()
        });
        let conn = Conn::open(&path, SIZE, CELL, WAIT).unwrap();
        let mut writer = conn.try_clone_writer().unwrap();
        write_frame(&mut writer, &ClientMsg::Focus(false).encode()).unwrap();
        assert_eq!(server.join().unwrap(), ClientMsg::Focus(false).encode());
        drop(conn);
    }

    #[test]
    fn errors_name_their_cause() {
        assert_eq!(
            ConnError::NotRunning("herdr is not running".to_owned()).to_string(),
            "herdr is not running"
        );
        assert_eq!(
            ConnError::Incompatible("generation 2".to_owned()).to_string(),
            "generation 2"
        );
        assert_eq!(
            ConnError::Io(io::Error::other("boom")).to_string(),
            "endpoint socket: boom"
        );
        assert_eq!(
            ConnError::Codec(CodecError::Truncated).to_string(),
            "endpoint payload ended early"
        );
    }

    #[test]
    fn recv_keeps_a_partial_frame_across_read_timeouts() {
        let mut frame = Vec::new();
        let payload = control(SNAPSHOT_KIND, r#"{"boot_id":"b1","revision":3}"#);
        write_frame(&mut frame, &payload).unwrap();
        let (first, rest) = frame.split_at(6);
        let (first, rest) = (first.to_vec(), rest.to_vec());
        let (_dir, path, server) = fake_herdr(move |mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            stream.write_all(&first).unwrap();
            thread::sleep(Duration::from_millis(300));
            stream.write_all(&rest).unwrap();
            thread::sleep(Duration::from_millis(100));
        });
        let mut conn = Conn::open(&path, SIZE, CELL, WAIT).unwrap();
        conn.set_read_timeout(Some(Duration::from_millis(30)))
            .unwrap();
        let started = Instant::now();
        let mut timeouts = 0;
        let msg = loop {
            assert!(
                started.elapsed() < Duration::from_secs(5),
                "frame never completed"
            );
            match conn.recv().unwrap() {
                Some(msg) => break msg,
                None => timeouts += 1,
            }
        };
        assert!(
            timeouts > 0,
            "no read timeout hit while the frame was split"
        );
        assert_eq!(
            msg,
            ServerMsg::Control {
                kind: SNAPSHOT_KIND.to_owned(),
                data: r#"{"boot_id":"b1","revision":3}"#.to_owned(),
            }
        );
        server.join().unwrap();
    }

    #[test]
    fn recv_reports_an_undecodable_frame_as_a_codec_error() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
            put(&mut stream, &[0xFF]);
            thread::sleep(Duration::from_millis(100));
        });
        let mut conn = Conn::open(&path, SIZE, CELL, WAIT).unwrap();
        assert!(matches!(
            conn.recv(),
            Err(ConnError::Codec(CodecError::BadVarint(0xFF)))
        ));
        server.join().unwrap();
    }

    #[test]
    fn recv_after_herdr_closes_is_an_unexpected_eof() {
        let (_dir, path, server) = fake_herdr(|mut stream| {
            read_hello(&mut stream);
            put(&mut stream, &welcome("0.9.3"));
        });
        let mut conn = Conn::open(&path, SIZE, CELL, WAIT).unwrap();
        server.join().unwrap();
        match conn.recv() {
            Err(ConnError::Io(e)) => assert_eq!(e.kind(), io::ErrorKind::UnexpectedEof),
            other => panic!("{other:?}"),
        }
    }

    /// A scripted reader: each step is one read's outcome.
    enum Step {
        Bytes(Vec<u8>),
        Timeout(io::ErrorKind),
        Interrupted,
        Eof,
    }

    struct Script(VecDeque<Step>);

    impl Script {
        fn new(steps: Vec<Step>) -> Self {
            Self(steps.into())
        }
    }

    impl Read for Script {
        fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
            match self.0.pop_front() {
                None | Some(Step::Eof) => Ok(0),
                Some(Step::Timeout(kind)) => Err(kind.into()),
                Some(Step::Interrupted) => Err(io::ErrorKind::Interrupted.into()),
                Some(Step::Bytes(bytes)) => {
                    let n = bytes.len().min(buf.len());
                    buf[..n].copy_from_slice(&bytes[..n]);
                    if n < bytes.len() {
                        self.0.push_front(Step::Bytes(bytes[n..].to_vec()));
                    }
                    Ok(n)
                }
            }
        }
    }

    /// `[u32 LE len][payload]` as bytes.
    fn framed(payload: &[u8]) -> Vec<u8> {
        let mut wire = Vec::new();
        write_frame(&mut wire, payload).unwrap();
        wire
    }

    #[test]
    fn a_frame_split_across_timeouts_is_assembled() {
        let wire = framed(b"hello");
        let mut r = Script::new(vec![
            Step::Bytes(wire[..2].to_vec()),
            Step::Timeout(io::ErrorKind::WouldBlock),
            Step::Bytes(wire[2..6].to_vec()),
            Step::Timeout(io::ErrorKind::TimedOut),
            Step::Bytes(wire[6..].to_vec()),
        ]);
        let mut inbox = Inbox::default();
        assert_eq!(read_payload(&mut r, &mut inbox).unwrap(), None);
        assert_eq!(read_payload(&mut r, &mut inbox).unwrap(), None);
        assert_eq!(
            read_payload(&mut r, &mut inbox).unwrap(),
            Some(b"hello".to_vec())
        );
    }

    #[test]
    fn two_frames_in_one_read_come_out_in_order() {
        let mut both = framed(b"one");
        both.extend(framed(b"two"));
        let mut r = Script::new(vec![Step::Bytes(both)]);
        let mut inbox = Inbox::default();
        assert_eq!(
            read_payload(&mut r, &mut inbox).unwrap(),
            Some(b"one".to_vec())
        );
        assert_eq!(
            read_payload(&mut r, &mut inbox).unwrap(),
            Some(b"two".to_vec())
        );
    }

    #[test]
    fn interrupted_reads_are_retried() {
        let mut r = Script::new(vec![Step::Interrupted, Step::Bytes(framed(b"ok"))]);
        assert_eq!(
            read_payload(&mut r, &mut Inbox::default()).unwrap(),
            Some(b"ok".to_vec())
        );
    }

    #[test]
    fn eof_between_frames_is_an_unexpected_eof() {
        let mut r = Script::new(vec![Step::Eof]);
        match read_payload(&mut r, &mut Inbox::default()) {
            Err(ConnError::Io(e)) => assert_eq!(e.kind(), io::ErrorKind::UnexpectedEof),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn eof_inside_a_frame_is_an_unexpected_eof() {
        let wire = framed(b"hello");
        let mut r = Script::new(vec![Step::Bytes(wire[..3].to_vec()), Step::Eof]);
        match read_payload(&mut r, &mut Inbox::default()) {
            Err(ConnError::Io(e)) => assert_eq!(e.kind(), io::ErrorKind::UnexpectedEof),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn oversized_header_is_refused_before_the_body() {
        let too_long = u32::try_from(MAX_FRAME + 1).unwrap();
        let mut r = Script::new(vec![Step::Bytes(too_long.to_le_bytes().to_vec())]);
        match read_payload(&mut r, &mut Inbox::default()) {
            Err(ConnError::Io(e)) => assert_eq!(e.kind(), io::ErrorKind::InvalidData),
            other => panic!("{other:?}"),
        }
    }
}
