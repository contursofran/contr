//! Claude Code's status line as a usage source. Claude Code pipes a JSON
//! document to the `statusLine` command after each update. `run_statusline`
//! keeps the plan's limit windows from it in contr's own file, and
//! `read_claude` reads that file back. contr never reads Claude Code's files.

use crate::model::{PlanId, PlanUsage, UsageWindow, state_dir};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::fs;
use std::io::{self, Read, Write};
use std::path::Path;
use std::process::{Child, Command, Stdio};
use std::sync::mpsc::{self, Receiver, RecvTimeoutError};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

/// The usage file, under the state dir.
pub const CLAUDE_FILE: &str = "usage/claude.json";
/// Schema of `CLAUDE_FILE`. Other values are ignored.
const SCHEMA: u32 = 1;
/// Largest status line input used. The real input is a few KB. A longer input
/// is skipped whole: nothing is saved and the chained command does not run.
const MAX_INPUT: u64 = 1024 * 1024;
/// Time the chained command gets before it is killed.
const CHAIN_TIMEOUT: Duration = Duration::from_secs(2);
/// How often the chained command is checked while it runs.
const POLL: Duration = Duration::from_millis(10);
/// How long the output of a killed command is still collected. Output the
/// command wrote before the kill is already in the pipe.
const DRAIN: Duration = Duration::from_millis(100);

/// What `CLAUDE_FILE` holds.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ClaudeSnapshot {
    pub schema: u32,
    /// Unix seconds when the windows last changed on disk.
    pub updated_at: u64,
    pub claude_version: Option<String>,
    pub windows: Vec<UsageWindow>,
}

/// The parts of Claude Code's status line input that contr reads.
#[derive(Deserialize)]
struct StatusLine {
    version: Option<Value>,
    rate_limits: Option<RateLimits>,
}

#[derive(Deserialize)]
struct RateLimits {
    five_hour: Option<RawWindow>,
    seven_day: Option<RawWindow>,
}

#[derive(Deserialize)]
struct RawWindow {
    used_percentage: Option<f64>,
    resets_at: Option<u64>,
}

/// Reads the limit windows from Claude Code's status line JSON. `None` when
/// `rate_limits` is absent or no window has a usage percent.
pub fn parse_statusline(stdin_json: &str, now: u64) -> Option<ClaudeSnapshot> {
    let input: StatusLine = serde_json::from_str(stdin_json).ok()?;
    let limits = input.rate_limits?;
    let windows: Vec<UsageWindow> = [
        ("five_hour", limits.five_hour),
        ("seven_day", limits.seven_day),
    ]
    .into_iter()
    .filter_map(|(kind, window)| {
        let window = window?;
        Some(UsageWindow {
            kind: kind.to_string(),
            used_percent: window.used_percentage?,
            resets_at: window.resets_at,
        })
    })
    .collect();
    if windows.is_empty() {
        return None;
    }
    Some(ClaudeSnapshot {
        schema: SCHEMA,
        updated_at: now,
        claude_version: input
            .version
            .as_ref()
            .and_then(Value::as_str)
            .map(str::to_owned),
        windows,
    })
}

/// Writes `snap` to `dir`/`CLAUDE_FILE`, unless the file already holds the same
/// windows. Returns `Ok(true)` when it wrote. The write goes through a temp
/// file and a rename, so a reader never sees a half-written file.
pub fn write_snapshot(dir: &Path, snap: &ClaudeSnapshot) -> io::Result<bool> {
    let path = dir.join(CLAUDE_FILE);
    if read_snapshot(&path).is_some_and(|old| old.windows == snap.windows) {
        return Ok(false);
    }
    let folder = path
        .parent()
        .ok_or_else(|| io::Error::other("usage path has no folder"))?;
    fs::create_dir_all(folder)?;
    let bytes = serde_json::to_vec_pretty(snap).map_err(io::Error::other)?;
    let tmp = path.with_extension(format!("json.{}.tmp", std::process::id()));
    fs::write(&tmp, bytes)?;
    if let Err(error) = fs::rename(&tmp, &path) {
        let _ = fs::remove_file(&tmp);
        return Err(error);
    }
    Ok(true)
}

/// Claude's usage as last written, or `None` when the file is missing, corrupt
/// or of another schema. Blocking: run it on a background task.
pub fn read_claude(dir: &Path) -> Option<PlanUsage> {
    let snap = read_snapshot(&dir.join(CLAUDE_FILE))?;
    Some(PlanUsage {
        plan: PlanId::Claude,
        windows: snap.windows,
        read_at: snap.updated_at,
    })
}

/// The snapshot at `path`, or `None`. A missing file is quiet; a bad one is logged.
fn read_snapshot(path: &Path) -> Option<ClaudeSnapshot> {
    let text = match fs::read_to_string(path) {
        Ok(text) => text,
        Err(error) => {
            if error.kind() != io::ErrorKind::NotFound {
                tracing::warn!(%error, "unable to read the Claude usage file");
            }
            return None;
        }
    };
    match serde_json::from_str::<ClaudeSnapshot>(&text) {
        Ok(snap) if snap.schema == SCHEMA => Some(snap),
        Ok(snap) => {
            tracing::warn!(schema = snap.schema, "Claude usage file has another schema");
            None
        }
        Err(error) => {
            tracing::warn!(%error, "Claude usage file is corrupt");
            None
        }
    }
}

/// `contr statusline`. Reads Claude Code's status line JSON from `stdin`, keeps
/// the windows in the state dir, then copies to `stdout` what `then` prints for
/// the same input (nothing when `then` is empty). Input over 1 MiB is skipped:
/// nothing is saved and `then` does not run, so no part of it is used. Bad
/// input and failed saves are logged, not returned, so the status line keeps
/// working. Returns an error only when `stdin` or `stdout` fails.
pub fn run_statusline(stdin: impl Read, stdout: impl Write, then: &[String]) -> io::Result<()> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |elapsed| elapsed.as_secs());
    let dir = state_dir();
    run_with(stdin, stdout, then, dir.as_deref(), now, CHAIN_TIMEOUT)
}

fn run_with(
    stdin: impl Read,
    mut stdout: impl Write,
    then: &[String],
    dir: Option<&Path>,
    now: u64,
    timeout: Duration,
) -> io::Result<()> {
    let mut input = Vec::new();
    // One byte past the cap tells an over-long input from one of exactly the cap.
    stdin.take(MAX_INPUT + 1).read_to_end(&mut input)?;
    if input.len() as u64 > MAX_INPUT {
        tracing::warn!("status line input is over 1 MiB; skipped");
        return Ok(());
    }
    if let Some(snap) = std::str::from_utf8(&input)
        .ok()
        .and_then(|text| parse_statusline(text, now))
    {
        match dir {
            Some(dir) => {
                if let Err(error) = write_snapshot(dir, &snap) {
                    tracing::warn!(%error, "unable to write the Claude usage file");
                }
            }
            None => tracing::warn!("no state dir; Claude usage not saved"),
        }
    }
    if let Some((program, args)) = then.split_first() {
        stdout.write_all(&run_capped(program, args, &input, timeout))?;
    }
    stdout.flush()
}

/// Runs `program args…` with `input` on its stdin and returns its stdout,
/// whatever its exit status. A command that runs past `timeout` is killed with
/// the processes it forked, and returns what it wrote so far. A command that
/// cannot start, or whose pipes cannot be served, returns nothing.
fn run_capped(program: &str, args: &[String], input: &[u8], timeout: Duration) -> Vec<u8> {
    let mut command = Command::new(program);
    command
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    isolate(&mut command);
    let mut child = match command.spawn() {
        Ok(child) => child,
        Err(error) => {
            tracing::warn!(%error, program, "status line command did not start");
            return Vec::new();
        }
    };
    let Some(output) = serve_pipes(&mut child, input) else {
        tracing::warn!(
            program,
            "unable to serve the status line command's pipes; killed"
        );
        stop(&mut child);
        return Vec::new();
    };
    let deadline = Instant::now() + timeout;
    let exited = loop {
        match child.try_wait() {
            Ok(None) if Instant::now() < deadline => thread::sleep(POLL),
            Ok(None) => {
                tracing::warn!(program, "status line command ran past its limit; killed");
                break false;
            }
            Ok(Some(_)) => break true,
            Err(error) => {
                tracing::warn!(%error, program, "unable to wait for the status line command");
                break false;
            }
        }
    };
    if !exited {
        stop(&mut child);
    }
    collect(&output)
}

/// Feeds `input` to the child's stdin, and sends what the child writes to its
/// stdout over the returned channel. Each pipe has its own thread, so neither
/// can block the caller. `None` when a thread cannot start.
fn serve_pipes(child: &mut Child, input: &[u8]) -> Option<Receiver<Vec<u8>>> {
    if let Some(mut pipe) = child.stdin.take() {
        let input = input.to_vec();
        thread::Builder::new()
            .spawn(move || {
                let _ = pipe.write_all(&input);
            })
            .ok()?;
    }
    let mut pipe = child.stdout.take()?;
    let (sender, receiver) = mpsc::channel();
    thread::Builder::new()
        .spawn(move || {
            let mut chunk = [0; 8192];
            loop {
                match pipe.read(&mut chunk) {
                    Ok(0) => break,
                    Ok(read) => {
                        // The receiver is gone once the caller stopped waiting.
                        if sender.send(chunk[..read].to_vec()).is_err() {
                            break;
                        }
                    }
                    Err(error) if error.kind() == io::ErrorKind::Interrupted => {}
                    Err(_) => break,
                }
            }
        })
        .ok()?;
    Some(receiver)
}

/// The output sent so far. Waits for the pipe to close, but no longer than
/// `DRAIN`: a process that escaped the kill may hold the pipe open.
fn collect(output: &Receiver<Vec<u8>>) -> Vec<u8> {
    let deadline = Instant::now() + DRAIN;
    let mut bytes = Vec::new();
    loop {
        match output.recv_timeout(deadline.saturating_duration_since(Instant::now())) {
            Ok(chunk) => bytes.extend_from_slice(&chunk),
            Err(RecvTimeoutError::Timeout | RecvTimeoutError::Disconnected) => return bytes,
        }
    }
}

/// Kills the command and the processes it forked, then reaps it, so no zombie
/// is left.
fn stop(child: &mut Child) {
    kill_group(child.id());
    let _ = child.kill();
    let _ = child.wait();
}

/// Makes the command the leader of its own process group, so `kill_group`
/// reaches the processes it forks.
#[cfg(unix)]
fn isolate(command: &mut Command) {
    use std::os::unix::process::CommandExt;
    command.process_group(0);
}

#[cfg(not(unix))]
fn isolate(_: &mut Command) {}

/// Sends SIGKILL to every process in group `id`. A failure is ignored: `stop`
/// still kills the command itself.
#[cfg(unix)]
fn kill_group(id: u32) {
    let _ = Command::new("kill")
        .args(["-KILL", "--"])
        .arg(format!("-{id}"))
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
}

#[cfg(not(unix))]
fn kill_group(_: u32) {}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;

    const NOW: u64 = 1_761_200_000;
    const DOC: &str = r#"{"version":"2.1.309","rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":1761210000},"seven_day":{"used_percentage":41.2,"resets_at":1761700000}}}"#;

    fn window(kind: &str, used_percent: f64, resets_at: Option<u64>) -> UsageWindow {
        UsageWindow {
            kind: kind.to_string(),
            used_percent,
            resets_at,
        }
    }

    fn snapshot(windows: Vec<UsageWindow>) -> ClaudeSnapshot {
        ClaudeSnapshot {
            schema: SCHEMA,
            updated_at: NOW,
            claude_version: Some("2.1.309".to_string()),
            windows,
        }
    }

    fn doc_snapshot() -> ClaudeSnapshot {
        snapshot(vec![
            window("five_hour", 23.5, Some(1_761_210_000)),
            window("seven_day", 41.2, Some(1_761_700_000)),
        ])
    }

    /// A stdin that always fails, to test the read error path.
    struct Broken;

    impl Read for Broken {
        fn read(&mut self, _: &mut [u8]) -> io::Result<usize> {
            Err(io::Error::other("stdin broke"))
        }
    }

    /// A reader that counts the bytes it gives out.
    struct Counting<R> {
        inner: R,
        read: u64,
    }

    impl<R: Read> Read for Counting<R> {
        fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
            let n = self.inner.read(buf)?;
            self.read += n as u64;
            Ok(n)
        }
    }

    fn strings(words: &[&str]) -> Vec<String> {
        words.iter().map(|word| word.to_string()).collect()
    }

    #[test]
    fn parses_both_windows_and_the_version() {
        let snap = parse_statusline(DOC, NOW).expect("both windows");
        assert_eq!(snap, doc_snapshot());
    }

    #[test]
    fn parses_only_five_hour() {
        let json = r#"{"rate_limits":{"five_hour":{"used_percentage":10,"resets_at":5}}}"#;
        let snap = parse_statusline(json, NOW).expect("five_hour");
        assert_eq!(snap.windows, vec![window("five_hour", 10.0, Some(5))]);
        assert_eq!(snap.claude_version, None);
    }

    #[test]
    fn a_window_without_a_percent_is_skipped() {
        let json =
            r#"{"rate_limits":{"five_hour":{"resets_at":5},"seven_day":{"used_percentage":7}}}"#;
        let snap = parse_statusline(json, NOW).expect("seven_day");
        assert_eq!(snap.windows, vec![window("seven_day", 7.0, None)]);
    }

    #[test]
    fn input_without_rate_limits_is_none() {
        assert_eq!(parse_statusline(r#"{"model":{}}"#, NOW), None);
    }

    #[test]
    fn rate_limits_without_a_usable_window_is_none() {
        let json = r#"{"rate_limits":{"seven_day":{"resets_at":5}}}"#;
        assert_eq!(parse_statusline(json, NOW), None);
    }

    #[test]
    fn invalid_json_is_none() {
        assert_eq!(parse_statusline("not json", NOW), None);
    }

    #[test]
    fn a_non_text_version_is_ignored() {
        let json = r#"{"version":2,"rate_limits":{"five_hour":{"used_percentage":1}}}"#;
        let snap = parse_statusline(json, NOW).expect("five_hour");
        assert_eq!(snap.claude_version, None);
    }

    #[test]
    fn write_then_read_round_trips() {
        let dir = tempfile::tempdir().expect("tempdir");
        assert!(write_snapshot(dir.path(), &doc_snapshot()).expect("write"));
        let usage = read_claude(dir.path()).expect("usage");
        assert_eq!(usage.plan, PlanId::Claude);
        assert_eq!(usage.windows, doc_snapshot().windows);
        assert_eq!(usage.read_at, NOW);
    }

    #[test]
    fn the_file_has_the_documented_shape() {
        let dir = tempfile::tempdir().expect("tempdir");
        write_snapshot(dir.path(), &doc_snapshot()).expect("write");
        let text = fs::read_to_string(dir.path().join(CLAUDE_FILE)).expect("file");
        let value: Value = serde_json::from_str(&text).expect("json");
        assert_eq!(value["schema"], 1);
        assert_eq!(value["updated_at"], NOW);
        assert_eq!(value["claude_version"], "2.1.309");
        assert_eq!(value["windows"][0]["kind"], "five_hour");
        assert_eq!(value["windows"][1]["resets_at"], 1_761_700_000);
    }

    #[test]
    fn identical_windows_are_not_rewritten() {
        let dir = tempfile::tempdir().expect("tempdir");
        let path = dir.path().join(CLAUDE_FILE);
        assert!(write_snapshot(dir.path(), &doc_snapshot()).expect("first"));
        let modified = fs::metadata(&path)
            .expect("meta")
            .modified()
            .expect("mtime");
        let later = ClaudeSnapshot {
            updated_at: NOW + 60,
            ..doc_snapshot()
        };
        assert!(!write_snapshot(dir.path(), &later).expect("second"));
        assert_eq!(
            fs::metadata(&path)
                .expect("meta")
                .modified()
                .expect("mtime"),
            modified
        );
        assert_eq!(read_claude(dir.path()).expect("usage").read_at, NOW);
    }

    #[test]
    fn changed_windows_are_written() {
        let dir = tempfile::tempdir().expect("tempdir");
        write_snapshot(dir.path(), &doc_snapshot()).expect("first");
        let changed = snapshot(vec![window("five_hour", 50.0, Some(1_761_210_000))]);
        assert!(write_snapshot(dir.path(), &changed).expect("second"));
        assert_eq!(
            read_claude(dir.path()).expect("usage").windows,
            changed.windows
        );
    }

    #[test]
    fn a_write_into_a_blocked_folder_is_an_error() {
        let dir = tempfile::tempdir().expect("tempdir");
        fs::write(dir.path().join("usage"), "a file, not a folder").expect("blocker");
        assert!(write_snapshot(dir.path(), &doc_snapshot()).is_err());
    }

    #[test]
    fn a_rename_onto_a_folder_is_an_error_and_leaves_no_temp_file() {
        let dir = tempfile::tempdir().expect("tempdir");
        fs::create_dir_all(dir.path().join(CLAUDE_FILE)).expect("folder in the file's place");
        assert!(write_snapshot(dir.path(), &doc_snapshot()).is_err());
        // Only the folder itself is left in `usage/`; the temp file was removed.
        let entries = fs::read_dir(dir.path().join("usage"))
            .expect("usage")
            .count();
        assert_eq!(entries, 1);
    }

    #[test]
    fn missing_file_is_none() {
        let dir = tempfile::tempdir().expect("tempdir");
        assert_eq!(read_claude(dir.path()), None);
    }

    #[test]
    fn corrupt_file_is_none_and_is_replaced_on_write() {
        let dir = tempfile::tempdir().expect("tempdir");
        fs::create_dir_all(dir.path().join("usage")).expect("folder");
        fs::write(dir.path().join(CLAUDE_FILE), "{not json").expect("corrupt");
        assert_eq!(read_claude(dir.path()), None);
        assert!(write_snapshot(dir.path(), &doc_snapshot()).expect("write"));
        assert!(read_claude(dir.path()).is_some());
    }

    #[test]
    fn another_schema_is_none() {
        let dir = tempfile::tempdir().expect("tempdir");
        fs::create_dir_all(dir.path().join("usage")).expect("folder");
        let newer = ClaudeSnapshot {
            schema: 2,
            ..doc_snapshot()
        };
        fs::write(
            dir.path().join(CLAUDE_FILE),
            serde_json::to_string(&newer).expect("json"),
        )
        .expect("file");
        assert_eq!(read_claude(dir.path()), None);
    }

    #[test]
    fn statusline_saves_and_prints_nothing_without_a_command() {
        let dir = tempfile::tempdir().expect("tempdir");
        let mut out = Vec::new();
        run_with(
            DOC.as_bytes(),
            &mut out,
            &[],
            Some(dir.path()),
            NOW,
            CHAIN_TIMEOUT,
        )
        .expect("run");
        assert!(out.is_empty());
        assert!(read_claude(dir.path()).is_some());
    }

    #[test]
    fn input_without_limits_saves_nothing() {
        let dir = tempfile::tempdir().expect("tempdir");
        let mut out = Vec::new();
        run_with(
            r#"{"model":{}}"#.as_bytes(),
            &mut out,
            &[],
            Some(dir.path()),
            NOW,
            CHAIN_TIMEOUT,
        )
        .expect("run");
        assert!(!dir.path().join(CLAUDE_FILE).exists());
    }

    #[test]
    fn chained_command_gets_the_same_input_and_its_output_is_copied() {
        let dir = tempfile::tempdir().expect("tempdir");
        let mut out = Vec::new();
        run_with(
            DOC.as_bytes(),
            &mut out,
            &strings(&["cat"]),
            Some(dir.path()),
            NOW,
            CHAIN_TIMEOUT,
        )
        .expect("run");
        assert_eq!(out, DOC.as_bytes());
        assert!(read_claude(dir.path()).is_some());
    }

    #[test]
    fn a_missing_command_prints_nothing_and_still_saves() {
        let dir = tempfile::tempdir().expect("tempdir");
        let mut out = Vec::new();
        run_with(
            DOC.as_bytes(),
            &mut out,
            &strings(&["contr-no-such-command"]),
            Some(dir.path()),
            NOW,
            CHAIN_TIMEOUT,
        )
        .expect("run");
        assert!(out.is_empty());
        assert!(read_claude(dir.path()).is_some());
    }

    #[test]
    fn a_command_past_its_limit_is_killed() {
        let started = Instant::now();
        let mut out = Vec::new();
        run_with(
            DOC.as_bytes(),
            &mut out,
            &strings(&["sleep", "5"]),
            None,
            NOW,
            Duration::from_millis(200),
        )
        .expect("run");
        assert!(out.is_empty());
        assert!(started.elapsed() < Duration::from_secs(4));
    }

    #[test]
    fn a_forking_command_returns_within_the_cap() {
        let started = Instant::now();
        let mut out = Vec::new();
        run_with(
            DOC.as_bytes(),
            &mut out,
            &strings(&["sh", "-c", "echo early; sleep 5; echo late"]),
            None,
            NOW,
            CHAIN_TIMEOUT,
        )
        .expect("run");
        assert_eq!(out, b"early\n");
        assert!(started.elapsed() < Duration::from_secs(3));
    }

    #[test]
    fn a_forking_command_is_killed_at_a_short_limit() {
        let started = Instant::now();
        let mut out = Vec::new();
        run_with(
            DOC.as_bytes(),
            &mut out,
            &strings(&["sh", "-c", "sleep 5; echo x"]),
            None,
            NOW,
            Duration::from_millis(200),
        )
        .expect("run");
        assert!(out.is_empty());
        assert!(started.elapsed() < Duration::from_secs(2));
    }

    #[test]
    fn an_input_over_the_cap_is_skipped_whole() {
        let dir = tempfile::tempdir().expect("tempdir");
        // A valid document followed by spaces, past the cap. Without the cap
        // it would be saved and passed on.
        let mut input = Counting {
            inner: DOC.as_bytes().chain(io::repeat(b' ').take(MAX_INPUT)),
            read: 0,
        };
        let mut out = Vec::new();
        run_with(
            &mut input,
            &mut out,
            &strings(&["cat"]),
            Some(dir.path()),
            NOW,
            CHAIN_TIMEOUT,
        )
        .expect("run");
        assert_eq!(input.read, MAX_INPUT + 1);
        assert!(out.is_empty());
        assert!(!dir.path().join(CLAUDE_FILE).exists());
    }

    #[test]
    fn an_input_of_exactly_the_cap_is_used() {
        let dir = tempfile::tempdir().expect("tempdir");
        let mut input = DOC.as_bytes().to_vec();
        input.resize(MAX_INPUT as usize, b' ');
        let mut out = Vec::new();
        run_with(
            input.as_slice(),
            &mut out,
            &strings(&["cat"]),
            Some(dir.path()),
            NOW,
            CHAIN_TIMEOUT,
        )
        .expect("run");
        assert_eq!(out, input);
        assert!(read_claude(dir.path()).is_some());
    }

    #[test]
    fn a_blocked_folder_does_not_stop_the_chained_command() {
        let dir = tempfile::tempdir().expect("tempdir");
        fs::write(dir.path().join("usage"), "a file, not a folder").expect("blocker");
        let mut out = Vec::new();
        run_with(
            DOC.as_bytes(),
            &mut out,
            &strings(&["cat"]),
            Some(dir.path()),
            NOW,
            CHAIN_TIMEOUT,
        )
        .expect("run");
        assert_eq!(out, DOC.as_bytes());
    }

    #[test]
    fn no_state_dir_still_runs_the_chained_command() {
        let mut out = Vec::new();
        run_with(
            DOC.as_bytes(),
            &mut out,
            &strings(&["cat"]),
            None,
            NOW,
            CHAIN_TIMEOUT,
        )
        .expect("run");
        assert_eq!(out, DOC.as_bytes());
    }

    #[test]
    fn a_failing_stdin_is_an_error() {
        let mut out = Vec::new();
        assert!(run_with(Broken, &mut out, &[], None, NOW, CHAIN_TIMEOUT).is_err());
    }
}
