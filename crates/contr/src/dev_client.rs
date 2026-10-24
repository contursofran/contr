//! `contr dev`: sends one request to a dev window and prints its reply. The client runs in
//! every build and does not need GPUI. It connects to the socket that `CONTR_DEV_CONTROL`
//! names, writes one request line, reads one reply line and maps the reply to an exit status.

use crate::dev_protocol::{self, Reply, Request};
use serde_json::Value;
use std::ffi::OsString;
use std::io::{self, BufRead, BufReader, Write};
use std::os::unix::net::UnixStream;
use std::path::Path;
use std::process::ExitCode;
use std::time::Duration;

/// How long the client waits for the connection and for the reply.
const TIMEOUT: Duration = Duration::from_secs(15);

/// The forms that `contr dev` accepts. A usage error prints them.
const USAGE: &str = "Usage:
  contr dev state
  contr dev click <id>
  contr dev key <key> [<key>…]
  contr dev select <row-id>
  contr dev resize <width> <height>
  contr dev window-id
";

/// `contr dev <words…>`. `env` is the value of `CONTR_DEV_CONTROL`, and `None` when it is
/// not set. Exit status: 0 for a result, 1 for an error reply or a failed exchange, 2 for a
/// usage error or an unset variable, 3 when no dev window listens at the path.
pub fn run(
    words: &[String],
    env: Option<OsString>,
    out: &mut impl Write,
    err: &mut impl Write,
) -> ExitCode {
    run_with_timeout(words, env, TIMEOUT, out, err)
}

/// `run` with the timeout as an argument, so that a test can wait less than 15 seconds.
fn run_with_timeout(
    words: &[String],
    env: Option<OsString>,
    timeout: Duration,
    out: &mut impl Write,
    err: &mut impl Write,
) -> ExitCode {
    let request = match parse_words(words) {
        Ok(request) => request,
        Err(message) => return usage_error(&message, err),
    };
    // An empty value counts as not set, as the window does.
    let Some(socket) = env.filter(|value| !value.is_empty()) else {
        return usage_error(&format!("{} is not set", dev_protocol::ENV), err);
    };
    let path = Path::new(&socket);
    let line = match dev_protocol::request_line(&request) {
        Ok(line) => line,
        Err(error) => return failure(&format!("cannot encode the request: {error}"), err),
    };
    let reply = match exchange(path, &line, timeout) {
        Ok(reply) => reply,
        Err(Exchange::NoServer) => {
            // With stderr closed there is nowhere else to report. The status is 3 either way.
            let _ = writeln!(err, "contr dev: no dev window at {}", path.display());
            return ExitCode::from(3);
        }
        Err(Exchange::Failed(message)) => return failure(&message, err),
    };
    match dev_protocol::parse_reply(&reply) {
        Ok(Reply::Ok(result)) => print_result(&request, &result, out, err),
        Ok(Reply::Err(error)) => {
            // With stderr closed there is nowhere else to report. The status is 1 either way.
            let _ = writeln!(err, "contr dev: {}: {}", error.code, error.message);
            ExitCode::from(1)
        }
        Err(message) => failure(&format!("bad reply: {message}"), err),
    }
}

/// Reads the words after `dev`. The error is the message for the error line.
fn parse_words(words: &[String]) -> Result<Request, String> {
    let Some((command, rest)) = words.split_first() else {
        return Err("missing command".to_owned());
    };
    match (command.as_str(), rest) {
        ("state", []) => Ok(Request::State),
        ("window-id", []) => Ok(Request::WindowId),
        ("click", [id]) => Ok(Request::Click { id: id.clone() }),
        ("select", [row]) => Ok(Request::Select { row: row.clone() }),
        ("key", [_, ..]) => Ok(Request::Key {
            keys: rest.to_vec(),
        }),
        ("resize", [width, height]) => Ok(Request::Resize {
            width: parse_size(width)?,
            height: parse_size(height)?,
        }),
        ("state" | "window-id", _) => Err(format!("{command} takes no arguments")),
        ("click", _) => Err("click needs one id".to_owned()),
        ("select", _) => Err("select needs one row id".to_owned()),
        ("key", _) => Err("key needs at least one key".to_owned()),
        ("resize", _) => Err("resize needs a width and a height".to_owned()),
        _ => Err(format!("unknown command {command}")),
    }
}

/// A width or height. JSON cannot carry NaN or infinity, so only finite numbers pass.
fn parse_size(text: &str) -> Result<f32, String> {
    text.parse::<f32>()
        .ok()
        .filter(|size| size.is_finite())
        .ok_or_else(|| format!("{text} is not a finite number"))
}

/// Why no reply came back. `NoServer` means that nothing listens at the path.
enum Exchange {
    NoServer,
    Failed(String),
}

/// Connects to `path`, sends `line` and returns the reply line. The timeout covers the
/// reading and the writing. The standard library cannot time out a connect on a Unix socket.
fn exchange(path: &Path, line: &str, timeout: Duration) -> Result<String, Exchange> {
    let mut stream = UnixStream::connect(path).map_err(|error| match error.kind() {
        io::ErrorKind::NotFound | io::ErrorKind::ConnectionRefused => Exchange::NoServer,
        _ => io_failure(&error, timeout),
    })?;
    stream
        .set_read_timeout(Some(timeout))
        .and_then(|()| stream.set_write_timeout(Some(timeout)))
        .map_err(|error| io_failure(&error, timeout))?;
    stream
        .write_all(line.as_bytes())
        .map_err(|error| io_failure(&error, timeout))?;
    let mut reply = String::new();
    BufReader::new(&stream)
        .read_line(&mut reply)
        .map_err(|error| io_failure(&error, timeout))?;
    if reply.is_empty() {
        return Err(Exchange::Failed(
            "the window closed the connection without a reply".to_owned(),
        ));
    }
    Ok(reply)
}

fn io_failure(error: &io::Error, timeout: Duration) -> Exchange {
    match error.kind() {
        io::ErrorKind::WouldBlock | io::ErrorKind::TimedOut => {
            Exchange::Failed(format!("no reply within {timeout:?}"))
        }
        _ => Exchange::Failed(error.to_string()),
    }
}

/// Prints the result of a request that succeeded. A window id prints as a number, and the
/// other results print as pretty JSON. A closed stdout exits 1.
fn print_result(
    request: &Request,
    result: &Value,
    out: &mut impl Write,
    err: &mut impl Write,
) -> ExitCode {
    let text = match request {
        Request::WindowId => match result.get("window_id").and_then(Value::as_i64) {
            Some(id) => id.to_string(),
            None => return failure("bad reply: the result has no window_id", err),
        },
        _ => format!("{result:#}"),
    };
    match writeln!(out, "{text}") {
        Ok(()) => ExitCode::SUCCESS,
        Err(_) => ExitCode::FAILURE,
    }
}

/// A usage error: the error line and the dev usage on stderr. Exits 2.
fn usage_error(message: &str, err: &mut impl Write) -> ExitCode {
    // With stderr closed there is nowhere else to report. The status is 2 either way.
    let _ = writeln!(err, "contr dev: {message}");
    let _ = err.write_all(USAGE.as_bytes());
    ExitCode::from(2)
}

/// A failure after the command was read: the error line on stderr. Exits 1.
fn failure(message: &str, err: &mut impl Write) -> ExitCode {
    // With stderr closed there is nowhere else to report. The status is 1 either way.
    let _ = writeln!(err, "contr dev: {message}");
    ExitCode::from(1)
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use std::os::unix::net::UnixListener;
    use std::path::PathBuf;
    use std::thread::JoinHandle;

    /// A writer that fails on every write, as a closed pipe does.
    struct Closed;

    impl Write for Closed {
        fn write(&mut self, _: &[u8]) -> io::Result<usize> {
            Err(io::Error::other("closed"))
        }

        fn flush(&mut self) -> io::Result<()> {
            Ok(())
        }
    }

    /// A fake dev window: it takes one request line, waits `hold`, writes `reply` and
    /// closes. The thread returns the request line.
    struct Fake {
        _dir: tempfile::TempDir,
        path: PathBuf,
        request: JoinHandle<String>,
    }

    fn fake_server(reply: &'static str, hold: Duration) -> Fake {
        // A short base folder keeps the socket path under the 104-byte limit.
        let dir = tempfile::Builder::new()
            .prefix("contr-dev")
            .tempdir_in("/tmp")
            .unwrap();
        let path = dir.path().join("dev.sock");
        let listener = UnixListener::bind(&path).unwrap();
        let request = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut line = String::new();
            BufReader::new(&stream).read_line(&mut line).unwrap();
            std::thread::sleep(hold);
            let _ = stream.write_all(reply.as_bytes());
            line
        });
        Fake {
            _dir: dir,
            path,
            request,
        }
    }

    fn words(args: &[&str]) -> Vec<String> {
        args.iter().map(|arg| arg.to_string()).collect()
    }

    /// Runs the client with `args` and `env` as the socket path. Returns the status and both
    /// outputs.
    fn run_case<P: AsRef<Path> + ?Sized>(
        args: &[&str],
        env: Option<&P>,
        timeout: Duration,
    ) -> (ExitCode, String, String) {
        let mut out = Vec::new();
        let mut err = Vec::new();
        let env = env.map(|path| {
            let path: &Path = path.as_ref();
            path.as_os_str().to_owned()
        });
        let status = run_with_timeout(&words(args), env, timeout, &mut out, &mut err);
        (
            status,
            String::from_utf8(out).unwrap(),
            String::from_utf8(err).unwrap(),
        )
    }

    fn run_against(reply: &'static str, args: &[&str]) -> (ExitCode, String, String, String) {
        let fake = fake_server(reply, Duration::ZERO);
        let (status, out, err) = run_case(args, Some(&fake.path), Duration::from_secs(5));
        let request = fake.request.join().unwrap();
        (status, out, err, request)
    }

    #[test]
    fn state_prints_the_pretty_result_and_exits_zero() {
        let (status, out, err, request) = run_against(
            "{\"ok\":true,\"result\":{\"v\":1,\"focus\":\"panel\"}}\n",
            &["state"],
        );
        assert_eq!(status, ExitCode::SUCCESS);
        assert_eq!(err, "");
        assert_eq!(request, "{\"v\":1,\"cmd\":\"state\"}\n");
        assert_eq!(
            serde_json::from_str::<Value>(&out).unwrap(),
            serde_json::json!({"v": 1, "focus": "panel"})
        );
        assert!(
            out.contains("\n  \"focus\": \"panel\""),
            "not pretty: {out}"
        );
    }

    #[test]
    fn window_id_prints_the_number_and_a_newline() {
        let (status, out, err, request) = run_against(
            "{\"ok\":true,\"result\":{\"window_id\":12345}}\n",
            &["window-id"],
        );
        assert_eq!(status, ExitCode::SUCCESS);
        assert_eq!(out, "12345\n");
        assert_eq!(err, "");
        assert_eq!(request, "{\"v\":1,\"cmd\":\"window-id\"}\n");
    }

    #[test]
    fn window_id_without_a_window_id_is_a_bad_reply() {
        let (status, out, err, _) = run_against("{\"ok\":true,\"result\":{}}\n", &["window-id"]);
        assert_eq!(status, ExitCode::from(1));
        assert_eq!(out, "");
        assert_eq!(err, "contr dev: bad reply: the result has no window_id\n");
    }

    #[test]
    fn each_form_sends_its_fields() {
        let ok = "{\"ok\":true,\"result\":{}}\n";
        let cases = [
            (
                vec!["click", "wt:/a#b"],
                "{\"v\":1,\"cmd\":\"click\",\"id\":\"wt:/a#b\"}\n",
            ),
            (
                vec!["key", "down", "enter"],
                "{\"v\":1,\"cmd\":\"key\",\"keys\":[\"down\",\"enter\"]}\n",
            ),
            (
                vec!["select", "agent:w1:p1"],
                "{\"v\":1,\"cmd\":\"select\",\"row\":\"agent:w1:p1\"}\n",
            ),
            (
                vec!["resize", "1200", "800"],
                "{\"v\":1,\"cmd\":\"resize\",\"width\":1200.0,\"height\":800.0}\n",
            ),
        ];
        for (args, expected) in cases {
            let (status, _, err, request) = run_against(ok, &args);
            assert_eq!(status, ExitCode::SUCCESS, "args {args:?}: {err}");
            assert_eq!(request, expected, "args {args:?}");
        }
    }

    #[test]
    fn an_error_reply_prints_the_code_and_exits_one() {
        let (status, out, err, _) = run_against(
            "{\"ok\":false,\"error\":{\"code\":\"unknown_id\",\"message\":\"no control with id nope\"}}\n",
            &["click", "nope"],
        );
        assert_eq!(status, ExitCode::from(1));
        assert_eq!(out, "");
        assert_eq!(err, "contr dev: unknown_id: no control with id nope\n");
    }

    #[test]
    fn a_reply_that_is_not_json_is_a_bad_reply() {
        let (status, _, err, _) = run_against("nope\n", &["state"]);
        assert_eq!(status, ExitCode::from(1));
        assert!(
            err.starts_with("contr dev: bad reply: the reply is not JSON:"),
            "{err}"
        );
    }

    #[test]
    fn an_ok_reply_without_a_result_is_a_bad_reply() {
        let (status, _, err, _) = run_against("{\"ok\":true}\n", &["state"]);
        assert_eq!(status, ExitCode::from(1));
        assert_eq!(err, "contr dev: bad reply: the reply has no result\n");
    }

    #[test]
    fn a_window_that_closes_without_a_reply_exits_one() {
        let (status, out, err, _) = run_against("", &["state"]);
        assert_eq!(status, ExitCode::from(1));
        assert_eq!(out, "");
        assert_eq!(
            err,
            "contr dev: the window closed the connection without a reply\n"
        );
    }

    #[test]
    fn a_window_that_stays_silent_times_out() {
        let fake = fake_server("", Duration::from_millis(600));
        let (status, _, err) = run_case(&["state"], Some(&fake.path), Duration::from_millis(100));
        assert_eq!(status, ExitCode::from(1));
        assert_eq!(err, "contr dev: no reply within 100ms\n");
        fake.request.join().unwrap();
    }

    #[test]
    fn a_closed_stdout_exits_one() {
        let fake = fake_server("{\"ok\":true,\"result\":{}}\n", Duration::ZERO);
        let mut err = Vec::new();
        let env = Some(fake.path.as_os_str().to_owned());
        let status = run_with_timeout(
            &words(&["state"]),
            env,
            Duration::from_secs(5),
            &mut Closed,
            &mut err,
        );
        assert_eq!(status, ExitCode::FAILURE);
        fake.request.join().unwrap();
    }

    #[test]
    fn an_error_reply_with_a_closed_stderr_still_exits_one() {
        let fake = fake_server(
            "{\"ok\":false,\"error\":{\"code\":\"bad_key\",\"message\":\"unknown key\"}}\n",
            Duration::ZERO,
        );
        let mut out = Vec::new();
        let env = Some(fake.path.as_os_str().to_owned());
        let status = run_with_timeout(
            &words(&["key", "x"]),
            env,
            Duration::from_secs(5),
            &mut out,
            &mut Closed,
        );
        assert_eq!(status, ExitCode::from(1));
        fake.request.join().unwrap();
    }

    #[test]
    fn no_socket_exits_three() {
        let dir = tempfile::Builder::new()
            .prefix("contr-dev")
            .tempdir_in("/tmp")
            .unwrap();
        let path = dir.path().join("missing.sock");
        let (status, out, err) = run_case(&["state"], Some(&path), TIMEOUT);
        assert_eq!(status, ExitCode::from(3));
        assert_eq!(out, "");
        assert_eq!(
            err,
            format!("contr dev: no dev window at {}\n", path.display())
        );
    }

    #[test]
    fn a_socket_file_without_a_listener_exits_three() {
        // A window that quit leaves its socket file behind. Nothing accepts on it.
        let dir = tempfile::Builder::new()
            .prefix("contr-dev")
            .tempdir_in("/tmp")
            .unwrap();
        let path = dir.path().join("stale.sock");
        drop(UnixListener::bind(&path).unwrap());
        let (status, _, err) = run_case(&["state"], Some(&path), TIMEOUT);
        assert_eq!(status, ExitCode::from(3));
        assert!(err.starts_with("contr dev: no dev window at "), "{err}");
    }

    #[test]
    fn an_unset_variable_exits_two_with_the_usage() {
        let mut out = Vec::new();
        let mut err = Vec::new();
        let status = run_with_timeout(&words(&["state"]), None, TIMEOUT, &mut out, &mut err);
        assert_eq!(status, ExitCode::from(2));
        assert_eq!(out, Vec::<u8>::new());
        assert_eq!(
            String::from_utf8(err).unwrap(),
            format!("contr dev: CONTR_DEV_CONTROL is not set\n{USAGE}")
        );
    }

    #[test]
    fn an_empty_variable_counts_as_unset() {
        let (status, _, err) = run_case(&["state"], Some(Path::new("")), TIMEOUT);
        assert_eq!(status, ExitCode::from(2));
        assert_eq!(
            err,
            format!("contr dev: CONTR_DEV_CONTROL is not set\n{USAGE}")
        );
    }

    #[test]
    fn bad_forms_exit_two_without_connecting() {
        // The socket does not exist. A connect would exit 3, so exit 2 shows that no connect
        // was tried.
        let dir = tempfile::Builder::new()
            .prefix("contr-dev")
            .tempdir_in("/tmp")
            .unwrap();
        let path = dir.path().join("never.sock");
        let bad: [&[&str]; 9] = [
            &[],
            &["bogus"],
            &["click"],
            &["click", "a", "b"],
            &["select"],
            &["key"],
            &["resize", "10"],
            &["resize", "a", "b"],
            &["resize", "NaN", "800"],
        ];
        for args in bad {
            let (status, _, err) = run_case(args, Some(&path), TIMEOUT);
            assert_eq!(status, ExitCode::from(2), "args {args:?}");
            assert!(err.starts_with("contr dev: "), "args {args:?}: {err}");
            assert!(err.ends_with(USAGE), "args {args:?}: {err}");
        }
        let (status, _, _) = run_case(&["state", "extra"], Some(&path), TIMEOUT);
        assert_eq!(status, ExitCode::from(2));
        let (status, _, _) = run_case(&["window-id", "extra"], Some(&path), TIMEOUT);
        assert_eq!(status, ExitCode::from(2));
    }

    #[test]
    fn the_form_errors_name_the_problem() {
        let messages = [
            (vec![], "missing command"),
            (vec!["bogus"], "unknown command bogus"),
            (vec!["state", "x"], "state takes no arguments"),
            (vec!["window-id", "x"], "window-id takes no arguments"),
            (vec!["click"], "click needs one id"),
            (vec!["select"], "select needs one row id"),
            (vec!["key"], "key needs at least one key"),
            (vec!["resize", "10"], "resize needs a width and a height"),
            (vec!["resize", "a", "2"], "a is not a finite number"),
            (vec!["resize", "inf", "2"], "inf is not a finite number"),
        ];
        for (args, expected) in messages {
            assert_eq!(
                parse_words(&words(&args)).unwrap_err(),
                expected,
                "args {args:?}"
            );
        }
    }

    #[test]
    fn the_forms_read_their_arguments() {
        assert_eq!(parse_words(&words(&["state"])), Ok(Request::State));
        assert_eq!(parse_words(&words(&["window-id"])), Ok(Request::WindowId));
        assert_eq!(
            parse_words(&words(&["click", "terminal"])),
            Ok(Request::Click {
                id: "terminal".into()
            })
        );
        assert_eq!(
            parse_words(&words(&["select", "wt:/a"])),
            Ok(Request::Select {
                row: "wt:/a".into()
            })
        );
        assert_eq!(
            parse_words(&words(&["key", "down", "enter"])),
            Ok(Request::Key {
                keys: vec!["down".into(), "enter".into()]
            })
        );
        assert_eq!(
            parse_words(&words(&["resize", "1200", "800.5"])),
            Ok(Request::Resize {
                width: 1200.0,
                height: 800.5
            })
        );
    }
}
