//! Codex's session logs as a usage source. Codex writes the plan's limit
//! windows into its rollout files under `sessions/`, on each `token_count`
//! event. `read_codex` reads the tail of the newest rollout file and nothing
//! else under `CODEX_HOME`: no credentials, config or transcript text is read.

use crate::model::{PlanId, PlanUsage, UsageWindow};
use serde::Deserialize;
use std::ffi::OsString;
use std::fs::{self, DirEntry, File};
use std::io::{self, Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

/// How many of the newest day folders are searched for rollout files.
const DAY_FOLDERS: usize = 2;
/// Largest tail read from a rollout file. The newest `token_count` line is
/// searched for in it and nowhere earlier.
const TAIL_BYTES: u64 = 256 * 1024;

/// `$CODEX_HOME`, else `~/.codex`. See [`codex_home_from`].
pub fn codex_home() -> Option<PathBuf> {
    codex_home_from(|key| std::env::var_os(key), std::env::home_dir())
}

/// `$CODEX_HOME` when set and not empty, else `~/.codex`. The environment and
/// home are passed in so tests need no real variables.
fn codex_home_from(
    env: impl Fn(&str) -> Option<OsString>,
    home: Option<PathBuf>,
) -> Option<PathBuf> {
    match env("CODEX_HOME").filter(|value| !value.is_empty()) {
        Some(dir) => Some(PathBuf::from(dir)),
        None => home.map(|home| home.join(".codex")),
    }
}

/// Codex's usage from the newest rollout file under `home/sessions`, or `None`
/// when there is no rollout file, its tail holds no usable `token_count` line,
/// or the folder cannot be read. Blocking: run it on a background task.
pub fn read_codex(home: &Path) -> Option<PlanUsage> {
    let rollout = newest_rollout(&home.join("sessions"))?;
    let tail = match read_tail(&rollout.path) {
        Ok(tail) => tail,
        Err(error) => {
            tracing::warn!(%error, "unable to read the Codex rollout file");
            return None;
        }
    };
    let windows = parse_rollout_tail(&tail, rollout.modified)?;
    Some(PlanUsage {
        plan: PlanId::Codex,
        windows,
        read_at: rollout.modified,
    })
}

/// The limit windows of the last usable `token_count` line in a rollout tail.
/// A line is usable when `rate_limits` is present, its `limit_id` is absent or
/// `"codex"`, and its `primary` window has a percent. `primary` becomes kind
/// `"primary"`; `secondary` becomes `"secondary"` when it has a percent too.
/// A window resets at `resets_at`, or at `file_mtime + resets_in_seconds` when
/// the line gives only that. Lines that do not parse are skipped.
pub fn parse_rollout_tail(tail: &str, file_mtime: u64) -> Option<Vec<UsageWindow>> {
    tail.lines()
        .rev()
        .filter(|line| line.contains("token_count"))
        .find_map(|line| windows_of_line(line, file_mtime))
}

/// The parts of a rollout line that contr reads.
#[derive(Deserialize)]
struct Entry {
    payload: Option<Payload>,
}

#[derive(Deserialize)]
struct Payload {
    #[serde(rename = "type")]
    kind: Option<String>,
    rate_limits: Option<RateLimits>,
}

#[derive(Deserialize)]
struct RateLimits {
    limit_id: Option<String>,
    primary: Option<RawWindow>,
    secondary: Option<RawWindow>,
}

/// The reset fields are read as numbers, so a float such as `1777927196.0` parses. See
/// [`whole_seconds`].
#[derive(Deserialize)]
struct RawWindow {
    used_percent: Option<f64>,
    resets_at: Option<f64>,
    resets_in_seconds: Option<f64>,
}

/// The windows of one rollout line, or `None` when the line is not usable.
fn windows_of_line(line: &str, file_mtime: u64) -> Option<Vec<UsageWindow>> {
    let entry: Entry = serde_json::from_str(line).ok()?;
    let payload = entry.payload?;
    if payload.kind.as_deref() != Some("token_count") {
        return None;
    }
    let limits = payload.rate_limits?;
    if limits.limit_id.as_deref().is_some_and(|id| id != "codex") {
        return None;
    }
    let mut windows = vec![usage_window("primary", limits.primary?, file_mtime)?];
    if let Some(secondary) = limits
        .secondary
        .and_then(|raw| usage_window("secondary", raw, file_mtime))
    {
        windows.push(secondary);
    }
    Some(windows)
}

/// One window, or `None` when it has no percent.
fn usage_window(kind: &str, raw: RawWindow, file_mtime: u64) -> Option<UsageWindow> {
    let resets_at = whole_seconds(raw.resets_at).or_else(|| {
        whole_seconds(raw.resets_in_seconds).map(|seconds| file_mtime.saturating_add(seconds))
    });
    Some(UsageWindow {
        kind: kind.to_string(),
        used_percent: raw.used_percent?,
        resets_at,
    })
}

/// Whole seconds from a JSON number: the fraction is dropped. A negative or
/// non-finite number is no time, so the window keeps no reset time.
fn whole_seconds(value: Option<f64>) -> Option<u64> {
    value
        .filter(|seconds| seconds.is_finite() && *seconds >= 0.0)
        .map(|seconds| seconds as u64)
}

/// A rollout file and the Unix seconds it was last written.
struct Rollout {
    path: PathBuf,
    modified: u64,
}

/// The rollout file written last among the rollout files of the newest
/// `DAY_FOLDERS` day folders. Ties go to the name that sorts last.
fn newest_rollout(sessions: &Path) -> Option<Rollout> {
    newest_day_folders(sessions, DAY_FOLDERS)
        .iter()
        .flat_map(|day| rollouts_in(day))
        .max_by(|a, b| {
            a.modified
                .cmp(&b.modified)
                .then_with(|| a.path.cmp(&b.path))
        })
}

/// The newest `count` day folders, as `sessions/YEAR/MONTH/DAY`. Folder names
/// are numbers and are compared as numbers.
fn newest_day_folders(sessions: &Path, count: usize) -> Vec<PathBuf> {
    let mut days = Vec::with_capacity(count);
    for year in numbered_subfolders(sessions) {
        for month in numbered_subfolders(&year) {
            for day in numbered_subfolders(&month) {
                days.push(day);
                if days.len() == count {
                    return days;
                }
            }
        }
    }
    days
}

/// The subfolders of `dir` whose names are numbers, largest first. Files and
/// other names are ignored.
fn numbered_subfolders(dir: &Path) -> Vec<PathBuf> {
    let mut folders: Vec<(u32, PathBuf)> = entries_of(dir)
        .into_iter()
        .filter(|entry| entry.file_type().is_ok_and(|kind| kind.is_dir()))
        .filter_map(|entry| {
            let number = entry.file_name().to_str()?.parse::<u32>().ok()?;
            Some((number, entry.path()))
        })
        .collect();
    folders.sort_by_key(|(number, _)| std::cmp::Reverse(*number));
    folders.into_iter().map(|(_, path)| path).collect()
}

/// The `rollout-*.jsonl` files directly in `dir`, with their modified times.
/// A file whose time cannot be read is skipped.
fn rollouts_in(dir: &Path) -> Vec<Rollout> {
    entries_of(dir)
        .into_iter()
        .filter_map(|entry| {
            let name = entry.file_name();
            let name = name.to_str()?;
            if !(name.starts_with("rollout-") && name.ends_with(".jsonl")) {
                return None;
            }
            if !entry.file_type().ok()?.is_file() {
                return None;
            }
            let modified = unix_seconds(entry.metadata().ok()?.modified().ok()?)?;
            Some(Rollout {
                path: entry.path(),
                modified,
            })
        })
        .collect()
}

/// The entries of `dir`. A missing folder is quiet; any other error is logged
/// and reads as an empty folder.
fn entries_of(dir: &Path) -> Vec<DirEntry> {
    match fs::read_dir(dir) {
        Ok(entries) => entries.filter_map(Result::ok).collect(),
        Err(error) => {
            if error.kind() != io::ErrorKind::NotFound {
                tracing::warn!(%error, "unable to list a Codex sessions folder");
            }
            Vec::new()
        }
    }
}

fn unix_seconds(time: SystemTime) -> Option<u64> {
    time.duration_since(UNIX_EPOCH)
        .ok()
        .map(|since| since.as_secs())
}

/// The last `TAIL_BYTES` of the file as text. When the tail starts inside a
/// line, that partial first line is dropped. When it starts at a line start,
/// the first line is kept. The read begins one byte early to tell the two apart.
fn read_tail(path: &Path) -> io::Result<String> {
    let mut file = File::open(path)?;
    let len = file.metadata()?.len();
    let start = len.saturating_sub(TAIL_BYTES);
    if start == 0 {
        let mut bytes = Vec::new();
        file.take(TAIL_BYTES).read_to_end(&mut bytes)?;
        return Ok(String::from_utf8_lossy(&bytes).into_owned());
    }
    file.seek(SeekFrom::Start(start - 1))?;
    let mut bytes = Vec::new();
    file.take(TAIL_BYTES + 1).read_to_end(&mut bytes)?;
    let Some((&before, rest)) = bytes.split_first() else {
        return Ok(String::new());
    };
    let whole: &[u8] = if before == b'\n' {
        rest
    } else {
        match rest.iter().position(|&byte| byte == b'\n') {
            Some(at) => rest.get(at + 1..).unwrap_or_default(),
            None => &[],
        }
    };
    Ok(String::from_utf8_lossy(whole).into_owned())
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use std::time::Duration;

    const PRIMARY: &str = r#"{"used_percent":4.0,"window_minutes":300,"resets_at":1777927196}"#;
    const SECONDARY: &str =
        r#"{"used_percent":25.0,"window_minutes":10080,"resets_at":1778500000}"#;
    const MTIME: u64 = 1_777_900_000;

    /// A `token_count` line whose `rate_limits` is the given JSON.
    fn token_line(rate_limits: &str) -> String {
        format!(
            r#"{{"type":"event_msg","payload":{{"type":"token_count","rate_limits":{rate_limits}}}}}"#
        )
    }

    /// The usual `rate_limits` with both windows.
    fn limits(primary: &str, secondary: &str) -> String {
        format!(
            r#"{{"limit_id":"codex","primary":{primary},"secondary":{secondary},"credits":null}}"#
        )
    }

    fn env_of(pairs: &'static [(&'static str, &'static str)]) -> impl Fn(&str) -> Option<OsString> {
        move |key| {
            pairs
                .iter()
                .find(|(name, _)| *name == key)
                .map(|(_, value)| OsString::from(value))
        }
    }

    /// Writes a rollout file at `home/sessions/<day>/<name>` and sets its mtime.
    fn write_rollout(home: &Path, day: &str, name: &str, lines: &[String], mtime: u64) -> PathBuf {
        let folder = home.join("sessions").join(day);
        fs::create_dir_all(&folder).expect("folder");
        let path = folder.join(name);
        let mut body = lines.join("\n");
        body.push('\n');
        fs::write(&path, body).expect("rollout");
        File::options()
            .write(true)
            .open(&path)
            .expect("open")
            .set_modified(UNIX_EPOCH + Duration::from_secs(mtime))
            .expect("mtime");
        path
    }

    fn primary_only(used: f64) -> String {
        token_line(&format!(
            r#"{{"limit_id":"codex","primary":{{"used_percent":{used},"resets_at":1777927196}}}}"#
        ))
    }

    #[test]
    fn the_documented_shape_gives_two_windows() {
        let tail = token_line(&limits(PRIMARY, SECONDARY));
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(
            windows,
            vec![
                UsageWindow {
                    kind: "primary".into(),
                    used_percent: 4.0,
                    resets_at: Some(1_777_927_196),
                },
                UsageWindow {
                    kind: "secondary".into(),
                    used_percent: 25.0,
                    resets_at: Some(1_778_500_000),
                },
            ]
        );
    }

    #[test]
    fn primary_alone_gives_one_window() {
        let tail = token_line(&limits(PRIMARY, "null"));
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(windows.len(), 1);
        assert_eq!(windows[0].kind, "primary");
    }

    #[test]
    fn a_secondary_without_a_percent_is_dropped_and_primary_kept() {
        let tail = token_line(&limits(PRIMARY, r#"{"resets_at":1778500000}"#));
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(windows.len(), 1);
        assert_eq!(windows[0].kind, "primary");
    }

    #[test]
    fn a_missing_limit_id_is_accepted() {
        let tail = token_line(&format!(r#"{{"primary":{PRIMARY}}}"#));
        assert!(parse_rollout_tail(&tail, MTIME).is_some());
    }

    #[test]
    fn the_last_usable_line_wins() {
        let tail = [primary_only(10.0), primary_only(30.0)].join("\n");
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(windows[0].used_percent, 30.0);
    }

    #[test]
    fn a_line_with_null_rate_limits_is_skipped() {
        let tail = [primary_only(10.0), token_line("null")].join("\n");
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(windows[0].used_percent, 10.0);
    }

    #[test]
    fn a_line_with_null_primary_is_skipped() {
        let tail = [primary_only(10.0), token_line(&limits("null", SECONDARY))].join("\n");
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(windows[0].used_percent, 10.0);
    }

    #[test]
    fn a_primary_without_a_percent_is_skipped() {
        let no_percent = token_line(r#"{"primary":{"resets_at":1777927196}}"#);
        let tail = [primary_only(10.0), no_percent].join("\n");
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(windows[0].used_percent, 10.0);
    }

    #[test]
    fn a_limit_id_other_than_codex_is_skipped() {
        let other = token_line(&format!(r#"{{"limit_id":"other","primary":{PRIMARY}}}"#));
        let tail = [primary_only(10.0), other].join("\n");
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(windows[0].used_percent, 10.0);
    }

    #[test]
    fn rate_limits_on_another_event_are_ignored() {
        let other_event = format!(
            r#"{{"type":"event_msg","payload":{{"type":"agent_message","rate_limits":{}}}}}"#,
            limits(PRIMARY, SECONDARY)
        );
        assert_eq!(parse_rollout_tail(&other_event, MTIME), None);
    }

    #[test]
    fn a_truncated_first_line_is_ignored() {
        let tail = [
            r#"ed_percent":4.0,"resets_at":1777927196}}"#.to_string(),
            primary_only(12.0),
        ]
        .join("\n");
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(windows[0].used_percent, 12.0);
    }

    #[test]
    fn resets_in_seconds_counts_from_the_file_time() {
        let relative = token_line(
            r#"{"limit_id":"codex","primary":{"used_percent":4.0,"resets_in_seconds":3600}}"#,
        );
        let windows = parse_rollout_tail(&relative, MTIME).expect("windows");
        assert_eq!(windows[0].resets_at, Some(MTIME + 3600));
    }

    #[test]
    fn resets_at_wins_over_resets_in_seconds() {
        let both = token_line(
            r#"{"primary":{"used_percent":4.0,"resets_at":5,"resets_in_seconds":3600}}"#,
        );
        let windows = parse_rollout_tail(&both, MTIME).expect("windows");
        assert_eq!(windows[0].resets_at, Some(5));
    }

    #[test]
    fn a_float_resets_at_is_read_as_whole_seconds() {
        let float = token_line(
            r#"{"primary":{"used_percent":4.0,"resets_at":1777927196.0,"resets_in_seconds":60}}"#,
        );
        let windows = parse_rollout_tail(&float, MTIME).expect("windows");
        assert_eq!(windows[0].resets_at, Some(1_777_927_196));
    }

    #[test]
    fn a_float_resets_in_seconds_is_read_and_its_fraction_dropped() {
        let float = token_line(r#"{"primary":{"used_percent":4.0,"resets_in_seconds":3600.9}}"#);
        let windows = parse_rollout_tail(&float, MTIME).expect("windows");
        assert_eq!(windows[0].resets_at, Some(MTIME + 3600));
    }

    #[test]
    fn a_negative_resets_at_keeps_the_window_without_a_reset_time() {
        let negative = token_line(r#"{"primary":{"used_percent":4.0,"resets_at":-5}}"#);
        let windows = parse_rollout_tail(&negative, MTIME).expect("windows");
        assert_eq!(windows[0].used_percent, 4.0);
        assert_eq!(windows[0].resets_at, None);
    }

    #[test]
    fn a_tail_without_a_usable_line_is_none() {
        let tail = [
            r#"{"type":"response_item","payload":{"type":"message"}}"#.to_string(),
            token_line("null"),
            "not json".to_string(),
        ]
        .join("\n");
        assert_eq!(parse_rollout_tail(&tail, MTIME), None);
        assert_eq!(parse_rollout_tail("", MTIME), None);
    }

    #[test]
    fn codex_home_prefers_the_override() {
        let home = Some(PathBuf::from("/h"));
        let env = env_of(&[("CODEX_HOME", "/x/codex")]);
        assert_eq!(codex_home_from(env, home), Some(PathBuf::from("/x/codex")));
    }

    #[test]
    fn codex_home_defaults_to_dot_codex_and_empty_counts_as_unset() {
        let home = Some(PathBuf::from("/h"));
        assert_eq!(
            codex_home_from(env_of(&[]), home.clone()),
            Some(PathBuf::from("/h/.codex"))
        );
        assert_eq!(
            codex_home_from(env_of(&[("CODEX_HOME", "")]), home),
            Some(PathBuf::from("/h/.codex"))
        );
        assert_eq!(codex_home_from(env_of(&[]), None), None);
    }

    #[test]
    fn reads_the_shape_with_the_file_time_as_read_at() {
        let dir = tempfile::tempdir().expect("tempdir");
        write_rollout(
            dir.path(),
            "2026/10/23",
            "rollout-a.jsonl",
            &[token_line(&limits(PRIMARY, SECONDARY))],
            MTIME,
        );
        let usage = read_codex(dir.path()).expect("usage");
        assert_eq!(usage.plan, PlanId::Codex);
        assert_eq!(usage.read_at, MTIME);
        assert_eq!(usage.windows.len(), 2);
        assert_eq!(usage.left_percent(MTIME), Some(75.0));
    }

    #[test]
    fn the_newest_file_by_mtime_across_two_day_folders_wins() {
        let dir = tempfile::tempdir().expect("tempdir");
        write_rollout(
            dir.path(),
            "2026/10/22",
            "rollout-old.jsonl",
            &[primary_only(90.0)],
            MTIME + 50,
        );
        write_rollout(
            dir.path(),
            "2026/10/23",
            "rollout-new.jsonl",
            &[primary_only(10.0)],
            MTIME,
        );
        let usage = read_codex(dir.path()).expect("usage");
        assert_eq!(usage.windows[0].used_percent, 90.0);
        assert_eq!(usage.read_at, MTIME + 50);
    }

    #[test]
    fn a_third_day_folder_is_not_searched() {
        let dir = tempfile::tempdir().expect("tempdir");
        write_rollout(
            dir.path(),
            "2026/10/21",
            "rollout-older.jsonl",
            &[primary_only(90.0)],
            MTIME + 99,
        );
        write_rollout(
            dir.path(),
            "2026/10/22",
            "rollout-a.jsonl",
            &[primary_only(20.0)],
            MTIME + 5,
        );
        write_rollout(
            dir.path(),
            "2026/10/23",
            "rollout-b.jsonl",
            &[primary_only(10.0)],
            MTIME,
        );
        let usage = read_codex(dir.path()).expect("usage");
        assert_eq!(usage.windows[0].used_percent, 20.0);
    }

    #[test]
    fn folder_names_compare_as_numbers() {
        let dir = tempfile::tempdir().expect("tempdir");
        write_rollout(
            dir.path(),
            "2026/10/1",
            "rollout-a.jsonl",
            &[primary_only(10.0)],
            MTIME,
        );
        write_rollout(
            dir.path(),
            "2026/9/30",
            "rollout-b.jsonl",
            &[primary_only(20.0)],
            MTIME - 10,
        );
        write_rollout(
            dir.path(),
            "2026/2/28",
            "rollout-c.jsonl",
            &[primary_only(30.0)],
            MTIME + 10,
        );
        let usage = read_codex(dir.path()).expect("usage");
        assert_eq!(usage.windows[0].used_percent, 10.0);
    }

    #[test]
    fn a_folder_that_is_not_a_number_is_ignored() {
        let dir = tempfile::tempdir().expect("tempdir");
        write_rollout(
            dir.path(),
            "2026/10/23",
            "rollout-a.jsonl",
            &[primary_only(10.0)],
            MTIME,
        );
        write_rollout(
            dir.path(),
            "archive/10/23",
            "rollout-b.jsonl",
            &[primary_only(90.0)],
            MTIME + 10,
        );
        let usage = read_codex(dir.path()).expect("usage");
        assert_eq!(usage.windows[0].used_percent, 10.0);
    }

    #[test]
    fn files_that_are_not_rollouts_are_not_read() {
        let dir = tempfile::tempdir().expect("tempdir");
        write_rollout(
            dir.path(),
            "2026/10/23",
            "rollout-a.jsonl",
            &[primary_only(10.0)],
            MTIME,
        );
        write_rollout(
            dir.path(),
            "2026/10/23",
            "notes.jsonl",
            &[primary_only(90.0)],
            MTIME + 10,
        );
        write_rollout(
            dir.path(),
            "2026/10/23",
            "rollout-b.txt",
            &[primary_only(80.0)],
            MTIME + 20,
        );
        let usage = read_codex(dir.path()).expect("usage");
        assert_eq!(usage.windows[0].used_percent, 10.0);
    }

    #[test]
    fn a_newest_file_without_a_usable_line_is_none() {
        let dir = tempfile::tempdir().expect("tempdir");
        write_rollout(
            dir.path(),
            "2026/10/23",
            "rollout-a.jsonl",
            &[primary_only(10.0)],
            MTIME,
        );
        write_rollout(
            dir.path(),
            "2026/10/23",
            "rollout-b.jsonl",
            &["{}".to_string()],
            MTIME + 10,
        );
        assert!(read_codex(dir.path()).is_none());
    }

    #[test]
    fn only_the_tail_of_a_large_file_is_searched() {
        let dir = tempfile::tempdir().expect("tempdir");
        let filler = "x".repeat(1024);
        let early = [primary_only(10.0), filler.repeat(300)].join("\n");
        let late = [filler.repeat(300), primary_only(20.0)].join("\n");
        write_rollout(
            dir.path(),
            "2026/10/22",
            "rollout-early.jsonl",
            &[early],
            MTIME,
        );
        assert!(read_codex(dir.path()).is_none());
        write_rollout(
            dir.path(),
            "2026/10/23",
            "rollout-late.jsonl",
            &[late],
            MTIME + 10,
        );
        let usage = read_codex(dir.path()).expect("usage");
        assert_eq!(usage.windows[0].used_percent, 20.0);
    }

    #[test]
    fn missing_or_empty_sessions_is_none() {
        let dir = tempfile::tempdir().expect("tempdir");
        assert!(read_codex(dir.path()).is_none());
        fs::create_dir_all(dir.path().join("sessions/2026/10/23")).expect("folders");
        assert!(read_codex(dir.path()).is_none());
    }

    #[test]
    fn a_sessions_path_that_is_a_file_is_none() {
        let dir = tempfile::tempdir().expect("tempdir");
        fs::write(dir.path().join("sessions"), "not a folder").expect("file");
        assert!(read_codex(dir.path()).is_none());
    }

    #[test]
    fn a_missing_rollout_is_an_error_for_the_tail_reader() {
        let dir = tempfile::tempdir().expect("tempdir");
        assert!(read_tail(&dir.path().join("rollout-gone.jsonl")).is_err());
    }

    #[test]
    fn a_tail_that_starts_on_a_line_start_keeps_its_first_line() {
        // A junk line, then one `token_count` line padded to exactly TAIL_BYTES
        // with its newline. The tail starts on that line, so the line is whole.
        let dir = tempfile::tempdir().expect("tempdir");
        let line = primary_only(33.0);
        let padded = format!("{line}{}", " ".repeat(TAIL_BYTES as usize - 1 - line.len()));
        let path = dir.path().join("rollout-aligned.jsonl");
        fs::write(&path, format!("junk\n{padded}\n")).expect("rollout");
        let tail = read_tail(&path).expect("tail");
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(windows[0].used_percent, 33.0);
    }

    #[test]
    fn a_tail_that_starts_inside_a_line_drops_that_partial_line() {
        // A long run of `p` fills the file's first TAIL_BYTES, so the tail starts
        // inside it. Only the run's partial line goes; the complete line after it stays.
        let dir = tempfile::tempdir().expect("tempdir");
        let path = dir.path().join("rollout-inside.jsonl");
        let body = format!(
            "{}\n{}\n",
            "p".repeat(TAIL_BYTES as usize),
            primary_only(44.0)
        );
        fs::write(&path, body).expect("rollout");
        let tail = read_tail(&path).expect("tail");
        let windows = parse_rollout_tail(&tail, MTIME).expect("windows");
        assert_eq!(windows[0].used_percent, 44.0);
        assert!(!tail.starts_with('p'));
    }
}
