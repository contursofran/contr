//! The socket side of the dev control. It binds the socket, reads one request per connection
//! and passes each request to the UI thread through a channel. `start` also runs the UI pump,
//! which takes the requests in turn inside the window and sends the replies back.

use super::Handler;
use crate::dev_protocol::{self, DevError, MAX_LINE, Reply, Request, code};
use gpui::{AnyWindowHandle, App};
use std::io::{self, BufRead, BufReader, Read, Write};
use std::net::Shutdown;
use std::os::unix::ffi::OsStrExt;
use std::os::unix::fs::{FileTypeExt, PermissionsExt};
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::{Path, PathBuf};
use std::sync::mpsc::{self, RecvTimeoutError};
use std::thread::JoinHandle;
use std::time::{Duration, Instant};

/// A socket path must be shorter than this many bytes. macOS keeps 104 bytes for the path,
/// with the terminating NUL.
const PATH_LIMIT: usize = 104;

/// How long a connection may wait for the client's line, and for the reply to be written.
const IO_TIMEOUT: Duration = Duration::from_secs(5);

/// How long a connection waits for the UI thread to answer. It is shorter than the client's 15 s
/// budget, so the client gets a reply rather than a timeout of its own.
const REPLY_WAIT: Duration = Duration::from_secs(10);

/// How many requests may wait for the UI thread.
const QUEUE: usize = 16;

/// How long the accept loop waits after a failed accept, so that a fault does not spin.
const ACCEPT_RETRY: Duration = Duration::from_millis(100);

/// How many bytes of a too-long line are read and dropped before the connection closes.
const DRAIN_LIMIT: u64 = 1024 * 1024;

/// A request that reached the UI thread. `reply` takes its answer. The connection stops waiting
/// at `until`, so a job that is still queued after that is skipped: nobody reads its answer.
pub struct Job {
    pub request: Request,
    pub until: Instant,
    pub reply: mpsc::SyncSender<Reply>,
}

/// Binds the socket at `path`. A live socket at the path is an error, because another window
/// owns it. A dead socket is removed first. Any other file at the path is an error too. The
/// socket file gets mode 0600.
pub fn bind(path: &Path) -> io::Result<UnixListener> {
    let length = path.as_os_str().as_bytes().len();
    if length >= PATH_LIMIT {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            format!("the socket path is {length} bytes, but it must be shorter than {PATH_LIMIT}"),
        ));
    }
    match std::fs::symlink_metadata(path) {
        Ok(metadata) if metadata.file_type().is_socket() => {
            if UnixStream::connect(path).is_ok() {
                return Err(io::Error::new(
                    io::ErrorKind::AddrInUse,
                    "another dev window listens at this path",
                ));
            }
            // Nothing accepted the connection, so the socket is left from a window that quit.
            std::fs::remove_file(path)?;
        }
        Ok(_) => {
            return Err(io::Error::new(
                io::ErrorKind::AlreadyExists,
                "the path exists and is not a socket",
            ));
        }
        Err(error) if error.kind() == io::ErrorKind::NotFound => {}
        Err(error) => return Err(error),
    }
    let listener = UnixListener::bind(path)?;
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600))?;
    Ok(listener)
}

/// Starts the listener thread. It takes one connection at a time, and it sends each request to
/// `jobs`. The thread ends when the process ends.
pub fn spawn_listener(
    listener: UnixListener,
    jobs: async_channel::Sender<Job>,
) -> io::Result<JoinHandle<()>> {
    std::thread::Builder::new()
        .name("contr-dev-control".to_owned())
        .spawn(move || {
            for connection in listener.incoming() {
                match connection {
                    Ok(stream) => serve(stream, &jobs, IO_TIMEOUT, REPLY_WAIT),
                    Err(error) => {
                        tracing::warn!(target: "contr", "dev control: accept failed: {error}");
                        std::thread::sleep(ACCEPT_RETRY);
                    }
                }
            }
        })
}

/// Serves one connection: reads one request line, runs it and writes the reply. A line that is
/// too long gets its reply, and then the rest of the line is read and dropped. The client then
/// sees the reply, not a reset.
fn serve(
    mut stream: UnixStream,
    jobs: &async_channel::Sender<Job>,
    io_timeout: Duration,
    reply_wait: Duration,
) {
    // An accepted socket can inherit the listener's non-blocking mode, so the mode is set here.
    let setup = stream
        .set_nonblocking(false)
        .and_then(|()| stream.set_read_timeout(Some(io_timeout)))
        .and_then(|()| stream.set_write_timeout(Some(io_timeout)));
    if let Err(error) = setup {
        tracing::warn!(target: "contr", "dev control: cannot set up the connection: {error}");
        return;
    }
    let (reply, too_long) = match read_line(&stream) {
        Ok(line) => {
            let too_long = line.len() > MAX_LINE && !line.ends_with(b"\n");
            (answer(&line, jobs, reply_wait), too_long)
        }
        Err(error) => (
            Reply::Err(DevError::new(
                code::BAD_REQUEST,
                format!("cannot read the request: {error}"),
            )),
            false,
        ),
    };
    // A client that has gone away cannot read the reply, and nothing else needs it.
    let _ = stream.write_all(dev_protocol::reply_line(&reply).as_bytes());
    if too_long {
        let _ = stream.shutdown(Shutdown::Write);
        let _ = io::copy(&mut (&stream).take(DRAIN_LIMIT), &mut io::sink());
    }
}

/// Reads one line of at most `MAX_LINE + 1` bytes. That holds the longest request and its newline.
fn read_line(stream: &UnixStream) -> io::Result<Vec<u8>> {
    let mut line = Vec::new();
    BufReader::new(stream)
        .take((MAX_LINE + 1) as u64)
        .read_until(b'\n', &mut line)?;
    Ok(line)
}

/// The reply to one line. A line that is not valid is answered here. A valid request goes to the
/// UI thread, and its reply comes back on a channel of its own. The wait has a limit, so a UI
/// thread that does not answer cannot hold up the accept loop.
fn answer(line: &[u8], jobs: &async_channel::Sender<Job>, reply_wait: Duration) -> Reply {
    let Ok(text) = std::str::from_utf8(line) else {
        return Reply::Err(DevError::new(code::BAD_REQUEST, "the request is not UTF-8"));
    };
    let request = match dev_protocol::parse_request(text) {
        Ok(request) => request,
        Err(error) => return Reply::Err(error),
    };
    let (reply, waiting) = mpsc::sync_channel(1);
    let job = Job {
        request,
        until: Instant::now() + reply_wait,
        reply,
    };
    match jobs.try_send(job) {
        Ok(()) => {}
        Err(async_channel::TrySendError::Full(_)) => return busy(),
        Err(async_channel::TrySendError::Closed(_)) => return no_window(),
    }
    match waiting.recv_timeout(reply_wait) {
        Ok(reply) => reply,
        Err(RecvTimeoutError::Timeout) => Reply::Err(DevError::new(
            code::NO_WINDOW,
            "the dev window did not answer in time",
        )),
        Err(RecvTimeoutError::Disconnected) => no_window(),
    }
}

/// The reply when the window that should answer is gone.
fn no_window() -> Reply {
    Reply::Err(DevError::new(code::NO_WINDOW, "the dev window is closed"))
}

/// The reply when the requests waiting for the window are full. The window is not answering.
fn busy() -> Reply {
    Reply::Err(DevError::new(
        code::NO_WINDOW,
        "the dev window has too many requests waiting",
    ))
}

/// Binds the socket at `path`, starts the listener thread and the UI pump for `window`. A
/// failure is logged as a warning, and the app runs on without the server.
pub fn start(path: PathBuf, window: AnyWindowHandle, handler: Handler, cx: &mut App) {
    let listener = match bind(&path) {
        Ok(listener) => listener,
        Err(error) => {
            tracing::warn!(
                target: "contr",
                "dev control: cannot listen at {}: {error}",
                path.display()
            );
            return;
        }
    };
    let jobs = pump(window, handler, cx);
    if let Err(error) = spawn_listener(listener, jobs) {
        tracing::warn!(target: "contr", "dev control: cannot start the listener: {error}");
    }
}

/// The UI side. One task on the foreground executor takes each job in turn, runs the handler in
/// the window and sends the reply back. A job whose connection has stopped waiting is skipped.
/// The task lives as long as the app. Once the window is gone, every job gets `no_window`.
fn pump(window: AnyWindowHandle, handler: Handler, cx: &mut App) -> async_channel::Sender<Job> {
    let (jobs, queue) = async_channel::bounded::<Job>(QUEUE);
    cx.spawn(async move |cx| {
        while let Ok(job) = queue.recv().await {
            if Instant::now() >= job.until {
                continue;
            }
            let answer = cx.update(|cx| {
                window
                    .update(cx, |_, window, cx| handler(job.request, window, cx))
                    .unwrap_or_else(|_| no_window())
            });
            // The waiting connection may have gone. Then nothing reads the reply.
            let _ = job.reply.try_send(answer);
        }
    })
    .detach();
    jobs
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::dev_control::handler;
    use gpui::{Context, IntoElement, Render, TestAppContext, Window};
    use serde_json::json;
    use std::cell::RefCell;
    use std::rc::Rc;

    /// A folder under /tmp. Its path is short, so that a socket in it stays under the limit.
    fn short_dir() -> tempfile::TempDir {
        tempfile::Builder::new()
            .prefix("contr-dev")
            .tempdir_in("/tmp")
            .unwrap()
    }

    fn socket_in(dir: &tempfile::TempDir) -> PathBuf {
        dir.path().join("dev.sock")
    }

    /// Connects to `path`, writes `bytes` and returns the reply line. A failed write is ignored,
    /// because a line that is too long can meet a reset.
    fn send(path: &Path, bytes: &[u8]) -> String {
        let mut stream = UnixStream::connect(path).unwrap();
        let _ = stream.write_all(bytes);
        let mut reply = String::new();
        BufReader::new(stream).read_line(&mut reply).unwrap();
        reply
    }

    /// The error in a reply line. A reply that is not an error fails the test.
    fn error_of(line: &str) -> DevError {
        match dev_protocol::parse_reply(line).unwrap() {
            Reply::Err(error) => error,
            Reply::Ok(result) => panic!("expected an error, got {result}"),
        }
    }

    /// A listener at a path in `dir`, with its job receiver.
    fn listener_at(dir: &tempfile::TempDir) -> (PathBuf, async_channel::Receiver<Job>) {
        let path = socket_in(dir);
        let listener = bind(&path).unwrap();
        let (jobs, queue) = async_channel::bounded(QUEUE);
        spawn_listener(listener, jobs).unwrap();
        (path, queue)
    }

    #[test]
    fn a_dead_socket_file_is_replaced() {
        let dir = short_dir();
        let path = socket_in(&dir);
        drop(UnixListener::bind(&path).unwrap());
        assert!(bind(&path).is_ok());
    }

    #[test]
    fn a_live_listener_at_the_path_is_refused() {
        let dir = short_dir();
        let path = socket_in(&dir);
        let _live = UnixListener::bind(&path).unwrap();
        let error = bind(&path).unwrap_err();
        assert_eq!(error.kind(), io::ErrorKind::AddrInUse);
    }

    #[test]
    fn a_plain_file_at_the_path_is_refused_and_kept() {
        let dir = short_dir();
        let path = socket_in(&dir);
        std::fs::write(&path, "keep").unwrap();
        let error = bind(&path).unwrap_err();
        assert_eq!(error.kind(), io::ErrorKind::AlreadyExists);
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "keep");
    }

    #[test]
    fn the_socket_file_is_only_for_its_owner() {
        let dir = short_dir();
        let path = socket_in(&dir);
        let _listener = bind(&path).unwrap();
        let mode = std::fs::metadata(&path).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o600);
    }

    /// A path of exactly `length` bytes, in `dir`.
    fn path_of_length(dir: &tempfile::TempDir, length: usize) -> PathBuf {
        let folder = dir.path().as_os_str().len() + 1;
        dir.path().join("a".repeat(length - folder))
    }

    #[test]
    fn a_path_one_byte_under_the_limit_is_accepted() {
        let dir = short_dir();
        let path = path_of_length(&dir, PATH_LIMIT - 1);
        assert_eq!(path.as_os_str().as_bytes().len(), PATH_LIMIT - 1);
        assert!(bind(&path).is_ok());
    }

    #[test]
    fn a_path_at_the_limit_is_refused_before_anything_is_touched() {
        let dir = short_dir();
        let path = path_of_length(&dir, PATH_LIMIT);
        let error = bind(&path).unwrap_err();
        assert_eq!(error.kind(), io::ErrorKind::InvalidInput);
        assert!(error.to_string().contains("shorter than 104"), "{error}");
        assert!(!path.exists());
    }

    /// A job for `request` whose connection waits until `until`, and the receiver of its reply.
    fn job_until(request: Request, until: Instant) -> (Job, mpsc::Receiver<Reply>) {
        let (reply, waiting) = mpsc::sync_channel(1);
        (
            Job {
                request,
                until,
                reply,
            },
            waiting,
        )
    }

    #[test]
    fn a_state_request_reaches_the_jobs_and_its_reply_goes_back() {
        let dir = short_dir();
        let (path, jobs) = listener_at(&dir);
        let client = std::thread::spawn(move || send(&path, b"{\"v\":1,\"cmd\":\"state\"}\n"));
        let job = jobs.recv_blocking().unwrap();
        assert_eq!(job.request, Request::State);
        job.reply.send(Reply::Ok(json!({"v": 1}))).unwrap();
        assert_eq!(
            client.join().unwrap(),
            "{\"ok\":true,\"result\":{\"v\":1}}\n"
        );
    }

    #[test]
    fn a_bad_version_is_answered_without_a_job() {
        let dir = short_dir();
        let (path, jobs) = listener_at(&dir);
        let error = error_of(&send(&path, b"{\"v\":2,\"cmd\":\"state\"}\n"));
        assert_eq!(error.code, code::UNSUPPORTED_VERSION);
        assert!(jobs.try_recv().is_err());
    }

    #[test]
    fn a_line_that_is_not_json_is_answered_without_a_job() {
        let dir = short_dir();
        let (path, jobs) = listener_at(&dir);
        let error = error_of(&send(&path, b"nope\n"));
        assert_eq!(error.code, code::BAD_REQUEST);
        assert!(jobs.try_recv().is_err());
    }

    #[test]
    fn a_line_that_is_not_utf8_is_a_bad_request() {
        let dir = short_dir();
        let (path, jobs) = listener_at(&dir);
        let error = error_of(&send(&path, b"\xff\xfe\n"));
        assert_eq!(error.code, code::BAD_REQUEST);
        assert_eq!(error.message, "the request is not UTF-8");
        assert!(jobs.try_recv().is_err());
    }

    #[test]
    fn a_line_longer_than_the_limit_gets_its_reply_and_no_job() {
        let dir = short_dir();
        let (path, jobs) = listener_at(&dir);
        let padding = "a".repeat(MAX_LINE);
        let line = format!("{{\"v\":1,\"cmd\":\"click\",\"id\":\"{padding}\"}}\n");
        let error = error_of(&send(&path, line.as_bytes()));
        assert_eq!(error.code, code::BAD_REQUEST);
        assert_eq!(
            error.message,
            format!("the request is longer than {MAX_LINE} bytes")
        );
        assert!(jobs.try_recv().is_err());
    }

    #[test]
    fn a_client_that_sends_nothing_gets_a_read_error_and_no_job() {
        let (server_end, mut client_end) = UnixStream::pair().unwrap();
        let (jobs, queue) = async_channel::bounded(QUEUE);
        // The client keeps its end open and sends nothing, so the read times out.
        serve(server_end, &jobs, Duration::from_millis(50), REPLY_WAIT);
        let mut reply = String::new();
        client_end.read_to_string(&mut reply).unwrap();
        let error = error_of(&reply);
        assert_eq!(error.code, code::BAD_REQUEST);
        assert!(
            error.message.starts_with("cannot read the request:"),
            "{}",
            error.message
        );
        assert!(queue.try_recv().is_err());
    }

    #[test]
    fn the_listener_keeps_going_after_an_accept_error() {
        let dir = short_dir();
        let path = socket_in(&dir);
        let listener = bind(&path).unwrap();
        // A non-blocking listener fails each accept with WouldBlock until a client arrives.
        listener.set_nonblocking(true).unwrap();
        let (jobs, queue) = async_channel::bounded(QUEUE);
        spawn_listener(listener, jobs).unwrap();
        let client = std::thread::spawn(move || send(&path, b"{\"v\":1,\"cmd\":\"state\"}\n"));
        let job = queue.recv_blocking().unwrap();
        assert_eq!(job.request, Request::State);
        job.reply.send(Reply::Ok(json!({"v": 1}))).unwrap();
        assert_eq!(
            client.join().unwrap(),
            "{\"ok\":true,\"result\":{\"v\":1}}\n"
        );
    }

    #[test]
    fn two_connections_in_a_row_both_work() {
        let dir = short_dir();
        let (path, jobs) = listener_at(&dir);
        for turn in ["first", "second"] {
            let path = path.clone();
            let client = std::thread::spawn(move || send(&path, b"{\"v\":1,\"cmd\":\"state\"}\n"));
            let job = jobs.recv_blocking().unwrap();
            let sent = Reply::Ok(json!({"turn": turn}));
            job.reply.send(sent.clone()).unwrap();
            assert_eq!(client.join().unwrap(), dev_protocol::reply_line(&sent));
        }
    }

    /// Runs one connection over a socket pair, with `request` written first, and returns the
    /// reply line. The connection waits for the UI at most `reply_wait`.
    fn serve_pair(
        jobs: &async_channel::Sender<Job>,
        request: &[u8],
        reply_wait: Duration,
    ) -> String {
        let (server_end, mut client_end) = UnixStream::pair().unwrap();
        client_end.write_all(request).unwrap();
        serve(server_end, jobs, IO_TIMEOUT, reply_wait);
        let mut reply = String::new();
        client_end.read_to_string(&mut reply).unwrap();
        reply
    }

    #[test]
    fn a_job_the_ui_does_not_answer_in_time_gets_a_timeout_reply() {
        let (jobs, queue) = async_channel::bounded(QUEUE);
        let reply = serve_pair(
            &jobs,
            b"{\"v\":1,\"cmd\":\"state\"}\n",
            Duration::from_millis(50),
        );
        let error = error_of(&reply);
        assert_eq!(error.code, code::NO_WINDOW);
        assert_eq!(error.message, "the dev window did not answer in time");
        // The job stays queued, and its wait has ended, so the UI skips it when it comes round.
        let job = queue.try_recv().unwrap();
        assert_eq!(job.request, Request::State);
        assert!(job.until <= Instant::now());
    }

    #[test]
    fn a_full_queue_gets_a_busy_reply_without_a_wait() {
        let (jobs, queue) = async_channel::bounded(QUEUE);
        for _ in 0..QUEUE {
            let (job, _waiting) = job_until(Request::State, Instant::now() + REPLY_WAIT);
            assert!(jobs.try_send(job).is_ok());
        }
        let reply = serve_pair(&jobs, b"{\"v\":1,\"cmd\":\"state\"}\n", REPLY_WAIT);
        let error = error_of(&reply);
        assert_eq!(error.code, code::NO_WINDOW);
        assert_eq!(
            error.message,
            "the dev window has too many requests waiting"
        );
        assert_eq!(queue.len(), QUEUE, "the refused request was not queued");
    }

    #[test]
    fn a_closed_queue_gets_no_window() {
        let (jobs, queue) = async_channel::bounded::<Job>(QUEUE);
        drop(queue);
        let reply = serve_pair(&jobs, b"{\"v\":1,\"cmd\":\"state\"}\n", REPLY_WAIT);
        let error = error_of(&reply);
        assert_eq!(error.code, code::NO_WINDOW);
        assert_eq!(error.message, "the dev window is closed");
    }

    /// The root view of a test window. It draws nothing.
    struct Blank;

    impl Render for Blank {
        fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
            gpui::div()
        }
    }

    fn test_window(cx: &mut TestAppContext) -> AnyWindowHandle {
        cx.add_window(|_, _| Blank).into()
    }

    /// Sends one request to the pump and returns its reply. The test runs the foreground tasks
    /// first, so the pump has taken the job.
    fn ask(cx: &mut TestAppContext, jobs: &async_channel::Sender<Job>, request: Request) -> Reply {
        let (job, reply) = job_until(request, Instant::now() + Duration::from_secs(60));
        assert!(jobs.try_send(job).is_ok());
        cx.run_until_parked();
        reply.try_recv().unwrap()
    }

    #[gpui::test]
    fn a_job_whose_connection_stopped_waiting_is_not_run(cx: &mut TestAppContext) {
        let window = test_window(cx);
        let seen = Rc::new(RefCell::new(Vec::new()));
        let recorder = seen.clone();
        let recording: Handler = Rc::new(
            move |request: Request, _window: &mut Window, _cx: &mut App| {
                recorder.borrow_mut().push(request);
                Reply::Ok(json!({"seen": true}))
            },
        );
        let jobs = cx.update(|cx| pump(window, recording, cx));
        // Its wait ended before the UI took it: the handler does not run, and the reply is dropped.
        let (stale, stale_reply) = job_until(Request::State, Instant::now());
        assert!(jobs.try_send(stale).is_ok());
        cx.run_until_parked();
        assert!(seen.borrow().is_empty());
        assert_eq!(
            stale_reply.try_recv(),
            Err(mpsc::TryRecvError::Disconnected)
        );
        // A job that is still waited for runs.
        assert_eq!(
            ask(cx, &jobs, Request::WindowId),
            Reply::Ok(json!({"seen": true}))
        );
        assert_eq!(*seen.borrow(), vec![Request::WindowId]);
    }

    #[gpui::test]
    fn a_job_runs_in_the_window_and_its_reply_comes_back(cx: &mut TestAppContext) {
        let window = test_window(cx);
        let seen = Rc::new(RefCell::new(Vec::new()));
        let recorder = seen.clone();
        let recording: Handler = Rc::new(
            move |request: Request, _window: &mut Window, _cx: &mut App| {
                recorder.borrow_mut().push(request);
                Reply::Ok(json!({"seen": true}))
            },
        );
        let jobs = cx.update(|cx| pump(window, recording, cx));
        assert_eq!(
            ask(cx, &jobs, Request::State),
            Reply::Ok(json!({"seen": true}))
        );
        assert_eq!(*seen.borrow(), vec![Request::State]);
    }

    #[gpui::test]
    fn the_first_handler_answers_with_the_version(cx: &mut TestAppContext) {
        let window = test_window(cx);
        let jobs = cx.update(|cx| pump(window, handler(), cx));
        assert_eq!(
            ask(cx, &jobs, Request::WindowId),
            Reply::Ok(json!({"v": 1}))
        );
    }

    #[gpui::test]
    fn a_job_for_a_closed_window_gets_no_window(cx: &mut TestAppContext) {
        let window = test_window(cx);
        let removed = cx.update(|cx| window.update(cx, |_, window, _| window.remove_window()));
        assert!(removed.is_ok());
        cx.run_until_parked();
        let jobs = cx.update(|cx| pump(window, handler(), cx));
        let error = match ask(cx, &jobs, Request::State) {
            Reply::Err(error) => error,
            Reply::Ok(result) => panic!("expected no_window, got {result}"),
        };
        assert_eq!(error.code, code::NO_WINDOW);
        // The task keeps going, so a later job still gets an answer.
        let again = ask(cx, &jobs, Request::State);
        assert!(matches!(again, Reply::Err(ref error) if error.code == code::NO_WINDOW));
    }

    /// The round trip through the socket runs on a real app and is checked by hand. The test
    /// scheduler refuses a wake of a foreground task from another thread, which the listener needs.
    #[gpui::test]
    fn start_binds_a_private_socket_for_the_window(cx: &mut TestAppContext) {
        let dir = short_dir();
        let path = socket_in(&dir);
        let window = test_window(cx);
        cx.update(|cx| start(path.clone(), window, handler(), cx));
        let metadata = std::fs::symlink_metadata(&path).unwrap();
        assert!(metadata.file_type().is_socket());
        assert_eq!(metadata.permissions().mode() & 0o777, 0o600);
    }

    #[gpui::test]
    fn a_bind_failure_leaves_the_app_running(cx: &mut TestAppContext) {
        let dir = short_dir();
        let path = socket_in(&dir);
        std::fs::write(&path, "keep").unwrap();
        let window = test_window(cx);
        cx.update(|cx| start(path.clone(), window, handler(), cx));
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "keep");
    }
}
