//! Plan usage for the status bar's left side. Each plan is read every `USAGE_READ_EVERY` while the
//! frame window is visible, and once on show when the last read is older than that. Reads run on
//! the background executor. While the window is hidden there are no reads.

use crate::visibility::{GlobalVisibility, Visibility};
use async_channel::Receiver;
use contr_usage::claude::read_claude;
use contr_usage::codex::{codex_home, read_codex};
use contr_usage::model::{PLANS, PlanId, PlanUsage, state_dir};
use gpui::{App, AsyncApp, Context, Entity, Global, Subscription, Task, WeakEntity, prelude::*};
use std::path::Path;
use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

/// How often each plan is read while the window is visible. This is the issue's ceiling.
pub const USAGE_READ_EVERY: Duration = Duration::from_secs(60);

/// Every plan in `PLANS` order, with its usage when a read found any.
pub type Plans = Vec<(PlanId, Option<PlanUsage>)>;

/// The plan usage the status bar shows. `plans` holds the last read.
pub struct Usage {
    pub plans: Plans,
    /// The whole percent each plan showed at the last notify, in `PLANS` order. The low state
    /// follows from the percent, so comparing the percent also compares the low state.
    shown: Vec<Option<f64>>,
    /// When the last read finished, on the executor's clock.
    last_read: Option<Instant>,
    _task: Task<()>,
    _sub: Option<Subscription>,
}

/// The app-wide handle to the plan usage, created by `Usage::init`.
pub struct GlobalUsage(pub Entity<Usage>);

impl Global for GlobalUsage {}

/// Reads every plan. Called on the background executor only.
type Reader = Arc<dyn Fn() -> Plans + Send + Sync>;

/// The real reader: Claude's status line file and Codex's newest rollout file.
fn file_reader() -> Reader {
    Arc::new(|| read_plans(state_dir().as_deref(), codex_home().as_deref()))
}

/// Each plan's usage from its source, in `PLANS` order. A source that is absent, or has no usable
/// data, gives `None` for its plan. Blocking: run it on a background task.
pub fn read_plans(state: Option<&Path>, codex: Option<&Path>) -> Plans {
    PLANS
        .into_iter()
        .map(|plan| {
            let usage = match plan {
                PlanId::Claude => state.and_then(read_claude),
                PlanId::Codex => codex.and_then(read_codex),
            };
            (plan, usage)
        })
        .collect()
}

/// Every plan with no read yet.
pub fn unread() -> Plans {
    PLANS.into_iter().map(|plan| (plan, None)).collect()
}

/// Unix seconds now. The windows' reset times are Unix seconds, so `Usage::left` takes this value.
pub fn unix_now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |since| since.as_secs())
}

/// The whole percent left for `plan`, as the bar shows it. `None` when the plan has no data.
fn left_in(plans: &Plans, plan: PlanId, now: u64) -> Option<f64> {
    plans
        .iter()
        .find(|(id, _)| *id == plan)
        .and_then(|(_, usage)| usage.as_ref())
        .and_then(|usage| usage.left_percent(now))
        .map(f64::round)
}

/// The shown percent of every plan, in `PLANS` order.
fn shown(plans: &Plans, now: u64) -> Vec<Option<f64>> {
    PLANS
        .into_iter()
        .map(|plan| left_in(plans, plan, now))
        .collect()
}

impl Usage {
    /// Registers the plan usage as a global. It reads only while the window is visible. Panics
    /// without `Visibility::init` first.
    pub fn init(cx: &mut App) {
        Self::init_with(cx, file_reader());
    }

    fn init_with(cx: &mut App, reader: Reader) {
        let visibility = cx.global::<GlobalVisibility>().0.clone();
        let usage = cx.new(|cx| Self::start(&visibility, reader, cx));
        cx.set_global(GlobalUsage(usage));
    }

    fn start(visibility: &Entity<Visibility>, reader: Reader, cx: &mut Context<Self>) -> Self {
        // Every change is sent; the loop keeps the last one.
        let (changes_tx, changes_rx) = async_channel::unbounded::<bool>();
        let sub = cx.observe(visibility, move |_, visibility, cx| {
            let _ = changes_tx.try_send(visibility.read(cx).visible);
        });
        let visible = visibility.read(cx).visible;
        let task = cx.spawn(async move |this, cx| {
            run(this, cx, changes_rx, visible, reader).await;
        });
        Self {
            plans: unread(),
            // Nothing is shown before the first read.
            shown: vec![None; PLANS.len()],
            last_read: None,
            _task: task,
            _sub: Some(sub),
        }
    }

    /// The whole percent left for `plan` at Unix time `now`, or `None` when the plan has no data.
    pub fn left(&self, plan: PlanId, now: u64) -> Option<f64> {
        left_in(&self.plans, plan, now)
    }

    /// Stores the plans. Notifies only when a percent differs from the one the bar last showed.
    /// A window can reset between two reads while the source file stays the same, so the new
    /// percents are compared with the stored ones at `now`, not with the previous plans.
    fn record(&mut self, plans: Plans, read_at: Instant, now: u64, cx: &mut Context<Self>) {
        self.plans = plans;
        self.last_read = Some(read_at);
        let now_shown = shown(&self.plans, now);
        if now_shown != self.shown {
            self.shown = now_shown;
            cx.notify();
        }
    }

    /// Usage with fixed plans and no reading loop, for the views' tests.
    #[cfg(test)]
    pub fn detached(plans: Plans, _cx: &mut Context<Self>) -> Self {
        Self {
            shown: shown(&plans, unix_now()),
            plans,
            last_read: None,
            _task: Task::ready(()),
            _sub: None,
        }
    }
}

/// Plans with Claude and Codex at the given used percents. `None` leaves a plan without data.
#[cfg(test)]
pub(crate) fn fixture(claude_used: Option<f64>, codex_used: Option<f64>) -> Plans {
    let window = |plan, used: Option<f64>| {
        used.map(|used| PlanUsage {
            plan,
            windows: vec![contr_usage::model::UsageWindow {
                kind: "five_hour".into(),
                used_percent: used,
                resets_at: Some(4_102_444_800),
            }],
            read_at: 0,
        })
    };
    vec![
        (PlanId::Claude, window(PlanId::Claude, claude_used)),
        (PlanId::Codex, window(PlanId::Codex, codex_used)),
    ]
}

/// The reading loop. Hidden: waits for the next change and starts no timer. A timer already pending
/// when the window hides fires once more and does nothing. Shown: reads at once when the last read
/// is at least `USAGE_READ_EVERY` old, then one read every `USAGE_READ_EVERY`.
async fn run(
    this: WeakEntity<Usage>,
    cx: &mut AsyncApp,
    changes: Receiver<bool>,
    mut visible: bool,
    reader: Reader,
) {
    loop {
        while !visible {
            match changes.recv().await {
                Ok(now_visible) => visible = now_visible,
                Err(_) => return, // the usage was dropped
            }
        }
        if is_stale(&this, cx) && !read_into(&this, cx, &reader).await {
            return;
        }
        loop {
            cx.background_executor().timer(USAGE_READ_EVERY).await;
            apply_changes(&changes, &mut visible);
            if !visible {
                break;
            }
            if !read_into(&this, cx, &reader).await {
                return;
            }
        }
    }
}

/// True when no read has finished yet, or the last one is `USAGE_READ_EVERY` old or older.
fn is_stale(this: &WeakEntity<Usage>, cx: &mut AsyncApp) -> bool {
    match this.update(cx, |usage, _| usage.last_read).ok().flatten() {
        Some(read_at) => {
            cx.background_executor()
                .now()
                .saturating_duration_since(read_at)
                >= USAGE_READ_EVERY
        }
        None => true,
    }
}

/// Reads every plan on the background executor, then records the result. False when the usage is
/// gone, so the loop should stop.
async fn read_into(this: &WeakEntity<Usage>, cx: &mut AsyncApp, reader: &Reader) -> bool {
    let reader = reader.clone();
    let plans = cx
        .background_executor()
        .spawn(async move { reader() })
        .await;
    let read_at = cx.background_executor().now();
    let now = unix_now();
    this.update(cx, |usage, cx| usage.record(plans, read_at, now, cx))
        .is_ok()
}

/// Applies every change that arrived while the loop was waiting. The last one wins.
fn apply_changes(changes: &Receiver<bool>, visible: &mut bool) {
    while let Ok(now_visible) = changes.try_recv() {
        *visible = now_visible;
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use contr_usage::claude::{CLAUDE_FILE, ClaudeSnapshot, write_snapshot};
    use contr_usage::model::UsageWindow;
    use gpui::TestAppContext;
    use std::cell::Cell;
    use std::fs;
    use std::rc::Rc;
    use std::sync::Mutex;
    use std::sync::atomic::{AtomicUsize, Ordering::SeqCst};

    /// One Codex `token_count` line: 4 % used of a window that resets far in the future.
    const CODEX_LINE: &str = r#"{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":4.0,"window_minutes":300,"resets_at":4102444800}}}}"#;

    /// Counts the reader's calls and returns the plans the test sets.
    #[derive(Clone)]
    struct Fake {
        calls: Arc<AtomicUsize>,
        plans: Arc<Mutex<Plans>>,
    }

    impl Fake {
        fn new(plans: Plans) -> Self {
            Self {
                calls: Arc::new(AtomicUsize::new(0)),
                plans: Arc::new(Mutex::new(plans)),
            }
        }

        fn set(&self, plans: Plans) {
            *self.plans.lock().unwrap() = plans;
        }

        fn calls(&self) -> usize {
            self.calls.load(SeqCst)
        }

        /// Registers `Visibility` (visible) and a `Usage` that reads through this fake.
        fn init(&self, cx: &mut TestAppContext) {
            let fake = self.clone();
            cx.update(Visibility::init);
            cx.update(|app| {
                Usage::init_with(
                    app,
                    Arc::new(move || {
                        fake.calls.fetch_add(1, SeqCst);
                        fake.plans.lock().unwrap().clone()
                    }),
                )
            });
        }
    }

    fn usage(cx: &mut TestAppContext) -> Entity<Usage> {
        cx.read(|app| app.global::<GlobalUsage>().0.clone())
    }

    fn left(cx: &mut TestAppContext, plan: PlanId) -> Option<f64> {
        let usage = usage(cx);
        cx.read(|app| usage.read(app).left(plan, unix_now()))
    }

    fn set_visible(cx: &mut TestAppContext, visible: bool) {
        cx.update(|app| {
            let visibility = app.global::<GlobalVisibility>().0.clone();
            visibility.update(app, |v, cx| v.set(visible, cx));
        });
    }

    /// Runs the loop up to its next timer, moves the test clock by `by`, and runs what is due.
    fn advance(cx: &mut TestAppContext, by: Duration) {
        cx.run_until_parked();
        cx.executor().advance_clock(by);
        cx.run_until_parked();
    }

    #[test]
    fn left_is_the_whole_percent_left_and_none_without_data() {
        let now = unix_now();
        assert_eq!(
            left_in(&fixture(Some(38.4), None), PlanId::Claude, now),
            Some(62.)
        );
        assert_eq!(
            left_in(&fixture(Some(38.4), None), PlanId::Codex, now),
            None
        );
        let no_windows = vec![(
            PlanId::Claude,
            Some(PlanUsage {
                plan: PlanId::Claude,
                windows: Vec::new(),
                read_at: 0,
            }),
        )];
        assert_eq!(left_in(&no_windows, PlanId::Claude, now), None);
    }

    #[test]
    fn read_plans_reads_each_source_in_plans_order() {
        let state = tempfile::tempdir().expect("tempdir");
        let snapshot = ClaudeSnapshot {
            schema: 1,
            updated_at: 1_700_000_000,
            claude_version: None,
            windows: vec![UsageWindow {
                kind: "five_hour".into(),
                used_percent: 23.5,
                resets_at: Some(4_102_444_800),
            }],
        };
        write_snapshot(state.path(), &snapshot).expect("snapshot");
        let home = tempfile::tempdir().expect("tempdir");
        let folder = home.path().join("sessions/2026/10/23");
        fs::create_dir_all(&folder).expect("folders");
        fs::write(folder.join("rollout-a.jsonl"), format!("{CODEX_LINE}\n")).expect("rollout");

        let read = read_plans(Some(state.path()), Some(home.path()));
        let order: Vec<PlanId> = read.iter().map(|(plan, _)| *plan).collect();
        assert_eq!(order, PLANS.to_vec());
        assert_eq!(left_in(&read, PlanId::Claude, unix_now()), Some(77.));
        assert_eq!(left_in(&read, PlanId::Codex, unix_now()), Some(96.));
    }

    #[test]
    fn read_plans_gives_none_for_a_missing_or_unusable_source() {
        let empty = tempfile::tempdir().expect("tempdir");
        let corrupt = tempfile::tempdir().expect("tempdir");
        fs::create_dir_all(corrupt.path().join("usage")).expect("folder");
        fs::write(corrupt.path().join(CLAUDE_FILE), "{ not json").expect("file");

        let read = read_plans(Some(corrupt.path()), Some(empty.path()));
        assert!(read.iter().all(|(_, usage)| usage.is_none()));
        let unset = read_plans(None, None);
        assert!(unset.iter().all(|(_, usage)| usage.is_none()));
    }

    #[gpui::test]
    fn reads_at_once_then_once_a_minute(cx: &mut TestAppContext) {
        let fake = Fake::new(fixture(Some(38.4), None));
        fake.init(cx);
        cx.run_until_parked();
        assert_eq!(fake.calls(), 1, "the first read is at once");
        advance(cx, USAGE_READ_EVERY);
        assert_eq!(fake.calls(), 2);
        advance(cx, USAGE_READ_EVERY);
        assert_eq!(fake.calls(), 3);
    }

    #[gpui::test]
    fn a_hidden_window_takes_no_reads(cx: &mut TestAppContext) {
        let fake = Fake::new(fixture(Some(38.4), None));
        fake.init(cx);
        cx.run_until_parked();
        set_visible(cx, false);
        // The timer pending at the hide fires once and finds the window hidden.
        advance(cx, Duration::from_secs(600));
        assert_eq!(fake.calls(), 1);
    }

    #[gpui::test]
    fn showing_after_a_minute_reads_at_once(cx: &mut TestAppContext) {
        let fake = Fake::new(fixture(Some(38.4), None));
        fake.init(cx);
        cx.run_until_parked();
        set_visible(cx, false);
        advance(cx, Duration::from_secs(600));
        set_visible(cx, true);
        cx.run_until_parked();
        assert_eq!(fake.calls(), 2, "the last read is ten minutes old");
        advance(cx, USAGE_READ_EVERY);
        assert_eq!(fake.calls(), 3);
    }

    #[gpui::test]
    fn showing_within_a_minute_waits_for_the_timer(cx: &mut TestAppContext) {
        let fake = Fake::new(fixture(Some(38.4), None));
        fake.init(cx);
        cx.run_until_parked();
        set_visible(cx, false);
        advance(cx, Duration::from_secs(10));
        set_visible(cx, true);
        cx.run_until_parked();
        assert_eq!(fake.calls(), 1, "the last read is fresh");
        advance(cx, Duration::from_secs(50));
        assert_eq!(fake.calls(), 2, "the timer reads at 60 s");
    }

    #[gpui::test]
    fn notifies_only_when_a_shown_percent_changes(cx: &mut TestAppContext) {
        let fake = Fake::new(fixture(Some(38.4), Some(88.)));
        fake.init(cx);
        let usage = usage(cx);
        let notified = Rc::new(Cell::new(0));
        let counter = notified.clone();
        let _subscription =
            cx.update(|app| app.observe(&usage, move |_, _| counter.set(counter.get() + 1)));

        cx.run_until_parked();
        assert_eq!(notified.get(), 1, "the first read shows values");
        assert_eq!(left(cx, PlanId::Claude), Some(62.));

        // 38.3 used is 61.7 left, which shows as 62%: nothing to repaint.
        fake.set(fixture(Some(38.3), Some(88.)));
        advance(cx, USAGE_READ_EVERY);
        assert_eq!(fake.calls(), 2);
        assert_eq!(notified.get(), 1);

        fake.set(fixture(Some(45.), Some(88.)));
        advance(cx, USAGE_READ_EVERY);
        assert_eq!(notified.get(), 2);
        assert_eq!(left(cx, PlanId::Claude), Some(55.));

        // The Claude source has no data any more: the plan shows no value.
        fake.set(fixture(None, Some(88.)));
        advance(cx, USAGE_READ_EVERY);
        assert_eq!(notified.get(), 3);
        assert_eq!(left(cx, PlanId::Claude), None);
    }

    /// Unix seconds when the Codex window of the reset test resets. It is far after the wall clock,
    /// so the detached start counts the window as not yet reset.
    const RESET: u64 = 4_102_444_800;

    /// Plans with one Codex window at `used` percent that resets at `resets_at`. No Claude data.
    fn codex_window(used: f64, resets_at: u64) -> Plans {
        vec![
            (PlanId::Claude, None),
            (
                PlanId::Codex,
                Some(PlanUsage {
                    plan: PlanId::Codex,
                    windows: vec![UsageWindow {
                        kind: "five_hour".into(),
                        used_percent: used,
                        resets_at: Some(resets_at),
                    }],
                    read_at: 0,
                }),
            ),
        ]
    }

    #[gpui::test]
    fn a_reset_with_an_unchanged_file_notifies_once(cx: &mut TestAppContext) {
        let plans = codex_window(88., RESET);
        let usage = cx.new(|cx| Usage::detached(plans.clone(), cx));
        let notified = Rc::new(Cell::new(0));
        let counter = notified.clone();
        let _subscription =
            cx.update(|app| app.observe(&usage, move |_, _| counter.set(counter.get() + 1)));
        let read_at = cx.executor().now();

        // Before the reset the bar shows 12 %, as the detached start does: nothing to repaint.
        usage.update(cx, |usage, cx| {
            usage.record(plans.clone(), read_at, RESET - 1_000, cx)
        });
        assert_eq!(notified.get(), 0);

        // The window resets after the last read, and the source file does not change.
        usage.update(cx, |usage, cx| {
            usage.record(plans.clone(), read_at, RESET + 1, cx)
        });
        assert_eq!(notified.get(), 1, "the reset changes the shown percent");
        assert_eq!(
            cx.read(|app| usage.read(app).left(PlanId::Codex, RESET + 1)),
            Some(100.)
        );

        // The next read sees the same reset, so there is nothing new to show.
        usage.update(cx, |usage, cx| usage.record(plans, read_at, RESET + 60, cx));
        assert_eq!(notified.get(), 1);
    }
}
