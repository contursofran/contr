//! Host CPU and memory for the status bar. One reading every `HOST_SAMPLE_EVERY` while the frame
//! window is visible; no timers and no sampling while it is hidden.

use crate::visibility::{GlobalVisibility, Visibility};
use async_channel::Receiver;
use gpui::{App, AsyncApp, Context, Entity, Global, Subscription, Task, WeakEntity, prelude::*};
use std::sync::{Arc, Mutex, PoisonError};
use std::time::Duration;
use sysinfo::System;

/// Steady interval between readings while the window is visible (Activity Monitor's "often").
pub const HOST_SAMPLE_EVERY: Duration = Duration::from_secs(2);
/// Gap between the two readings taken on show. CPU % is the change since the previous reading, so
/// the second reading covers only the shown time. Must exceed sysinfo's 200 ms minimum.
pub const HOST_RESUME_GAP: Duration = Duration::from_millis(250);

/// One reading of the whole host.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct HostStats {
    pub cpu_percent: f32,
    pub mem_used: u64,
    pub mem_total: u64,
}

impl HostStats {
    /// Used share of total memory, 0 to 1. 0 when the total is unknown.
    pub fn mem_fraction(&self) -> f32 {
        if self.mem_total == 0 {
            return 0.;
        }
        (self.mem_used as f64 / self.mem_total as f64).clamp(0., 1.) as f32
    }

    /// CPU rounded to a whole percent and clamped to 0 to 100, e.g. "38%".
    pub fn cpu_label(&self) -> String {
        format!("{}%", self.cpu_percent.round().clamp(0., 100.) as u32)
    }

    /// Memory share rounded to a whole percent, e.g. "62%".
    pub fn mem_label(&self) -> String {
        format!("{}%", (self.mem_fraction() * 100.).round() as u32)
    }
}

/// The latest published reading. `None` until the first one is published.
pub struct Host {
    pub stats: Option<HostStats>,
    _task: Task<()>,
    _sub: Option<Subscription>,
}

/// The app-wide handle to the host sampler, created by `Host::init`.
pub struct GlobalHost(pub Entity<Host>);

impl Global for GlobalHost {}

/// Takes one reading. Called on the background executor only.
type Sampler = Arc<dyn Fn() -> HostStats + Send + Sync>;

/// The real sampler: one `System` for the app's lifetime, refreshed in place.
fn system_sampler() -> Sampler {
    let system = Arc::new(Mutex::new(System::new()));
    Arc::new(move || {
        let mut system = system.lock().unwrap_or_else(PoisonError::into_inner);
        system.refresh_cpu_usage();
        system.refresh_memory();
        HostStats {
            cpu_percent: system.global_cpu_usage(),
            mem_used: system.used_memory(),
            mem_total: system.total_memory(),
        }
    })
}

impl Host {
    /// Registers the host as a global. It samples only while the window is visible.
    /// Panics without `Visibility::init` first.
    pub fn init(cx: &mut App) {
        Self::init_with(cx, system_sampler());
    }

    fn init_with(cx: &mut App, sampler: Sampler) {
        let visibility = cx.global::<GlobalVisibility>().0.clone();
        let host = cx.new(|cx| Self::start(&visibility, sampler, cx));
        cx.set_global(GlobalHost(host));
    }

    fn start(visibility: &Entity<Visibility>, sampler: Sampler, cx: &mut Context<Self>) -> Self {
        // Every change is sent; the loop keeps the last one.
        let (changes_tx, changes_rx) = async_channel::unbounded::<bool>();
        let sub = cx.observe(visibility, move |_, visibility, cx| {
            let _ = changes_tx.try_send(visibility.read(cx).visible);
        });
        let visible = visibility.read(cx).visible;
        let task = cx.spawn(async move |this, cx| {
            run(this, cx, changes_rx, visible, sampler).await;
        });
        Self {
            stats: None,
            _task: task,
            _sub: Some(sub),
        }
    }

    /// Stores the reading. Notifies only when a shown label changes.
    fn record(&mut self, stats: HostStats, cx: &mut Context<Self>) {
        let shown_before = self.stats.map(|s| (s.cpu_label(), s.mem_label()));
        let changed = shown_before != Some((stats.cpu_label(), stats.mem_label()));
        self.stats = Some(stats);
        if changed {
            cx.notify();
        }
    }

    /// A host with fixed stats and no sampling, for the views' tests.
    #[cfg(test)]
    pub fn detached(stats: Option<HostStats>, _cx: &mut Context<Self>) -> Self {
        Self {
            stats,
            _task: Task::ready(()),
            _sub: None,
        }
    }
}

/// The sampling loop. Hidden: waits for the next change and starts no timer. A timer already pending
/// when the window hides fires once more and does nothing. Shown: one reading whose result is dropped
/// (it covers the hidden time), then one after `HOST_RESUME_GAP`, then one every `HOST_SAMPLE_EVERY`.
/// Each reading runs on the background executor.
async fn run(
    this: WeakEntity<Host>,
    cx: &mut AsyncApp,
    changes: Receiver<bool>,
    mut visible: bool,
    sampler: Sampler,
) {
    loop {
        while !visible {
            match changes.recv().await {
                Ok(now_visible) => visible = now_visible,
                Err(_) => return, // the host was dropped
            }
        }
        let _baseline = sample(&sampler, cx).await;
        cx.background_executor().timer(HOST_RESUME_GAP).await;
        apply_changes(&changes, &mut visible);
        if !visible {
            continue;
        }
        if !push(&this, cx, sample(&sampler, cx).await) {
            return;
        }
        loop {
            cx.background_executor().timer(HOST_SAMPLE_EVERY).await;
            apply_changes(&changes, &mut visible);
            if !visible {
                break;
            }
            if !push(&this, cx, sample(&sampler, cx).await) {
                return;
            }
        }
    }
}

/// Takes one reading on the background executor, so the UI thread never waits for it.
async fn sample(sampler: &Sampler, cx: &AsyncApp) -> HostStats {
    let sampler = sampler.clone();
    cx.background_executor()
        .spawn(async move { sampler() })
        .await
}

/// Applies every change that arrived while the loop was waiting. The last one wins.
fn apply_changes(changes: &Receiver<bool>, visible: &mut bool) {
    while let Ok(now_visible) = changes.try_recv() {
        *visible = now_visible;
    }
}

/// Hands a reading to the host. False when the host is gone, so the loop should stop.
fn push(this: &WeakEntity<Host>, cx: &mut AsyncApp, stats: HostStats) -> bool {
    this.update(cx, |host, cx| host.record(stats, cx)).is_ok()
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use async_channel::Receiver;
    use gpui::{AsyncApp, TestAppContext};
    use std::cell::Cell;
    use std::rc::Rc;
    use std::sync::atomic::{AtomicUsize, Ordering::SeqCst};

    fn stats(cpu_percent: f32, mem_used: u64, mem_total: u64) -> HostStats {
        HostStats {
            cpu_percent,
            mem_used,
            mem_total,
        }
    }

    #[test]
    fn labels_round_and_clamp() {
        assert_eq!(stats(37.5, 0, 1).cpu_label(), "38%");
        assert_eq!(stats(38.4, 0, 1).cpu_label(), "38%");
        assert_eq!(stats(120., 0, 1).cpu_label(), "100%");
        assert_eq!(stats(-3., 0, 1).cpu_label(), "0%");
        assert_eq!(stats(0., 62, 100).mem_label(), "62%");
        assert_eq!(stats(0., 1, 3).mem_label(), "33%");
        assert_eq!(stats(0., 12, 10).mem_label(), "100%");
    }

    #[test]
    fn memory_is_zero_without_a_total() {
        assert_eq!(stats(0., 5, 0).mem_fraction(), 0.);
        assert_eq!(stats(0., 5, 0).mem_label(), "0%");
    }

    /// Counts sampler calls and reports the CPU value the test sets.
    #[derive(Clone)]
    struct Fake {
        calls: Arc<AtomicUsize>,
        cpu: Arc<Mutex<f32>>,
    }

    impl Fake {
        fn new(cpu: f32) -> Self {
            Self {
                calls: Arc::new(AtomicUsize::new(0)),
                cpu: Arc::new(Mutex::new(cpu)),
            }
        }

        /// Registers `Visibility` (visible) and a `Host` that samples through this fake.
        fn init(&self, cx: &mut TestAppContext) {
            let fake = self.clone();
            cx.update(Visibility::init);
            cx.update(|app| {
                Host::init_with(
                    app,
                    Arc::new(move || {
                        fake.calls.fetch_add(1, SeqCst);
                        stats(*fake.cpu.lock().unwrap(), 50, 100)
                    }),
                )
            });
        }

        fn calls(&self) -> usize {
            self.calls.load(SeqCst)
        }
    }

    fn host(cx: &mut TestAppContext) -> Entity<Host> {
        cx.read(|app| app.global::<GlobalHost>().0.clone())
    }

    fn shown(cx: &mut TestAppContext, host: &Entity<Host>) -> Option<HostStats> {
        cx.read(|app| host.read(app).stats)
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
    fn system_sampler_reads_this_host() {
        let stats = system_sampler()();
        assert!(stats.mem_total > 0);
        assert!((0. ..=100.).contains(&stats.cpu_percent));
    }

    /// Runs the loop on the test executor and reports when it returns.
    fn spawn_loop(
        cx: &mut TestAppContext,
        host: &Entity<Host>,
        changes: Receiver<bool>,
        visible: bool,
    ) -> Rc<Cell<bool>> {
        let (weak, done) = (host.downgrade(), Rc::new(Cell::new(false)));
        let flag = done.clone();
        let sampler: Sampler = Arc::new(|| stats(0., 0, 0));
        cx.spawn(move |mut cx: AsyncApp| async move {
            run(weak, &mut cx, changes, visible, sampler).await;
            flag.set(true);
        })
        .detach();
        done
    }

    #[gpui::test]
    fn loop_ends_when_the_change_channel_closes(cx: &mut TestAppContext) {
        let (changes_tx, changes_rx) = async_channel::unbounded::<bool>();
        drop(changes_tx);
        let host = cx.new(|cx| Host::detached(None, cx));
        let done = spawn_loop(cx, &host, changes_rx, false);
        cx.run_until_parked();
        assert!(done.get());
    }

    #[gpui::test]
    fn loop_ends_when_the_host_is_gone(cx: &mut TestAppContext) {
        let (_changes_tx, changes_rx) = async_channel::unbounded::<bool>();
        let host = cx.new(|cx| Host::detached(None, cx));
        let done = spawn_loop(cx, &host, changes_rx, true);
        drop(host);
        advance(cx, HOST_RESUME_GAP);
        assert!(done.get());
    }

    #[gpui::test]
    fn detached_host_keeps_its_stats(cx: &mut TestAppContext) {
        let host = cx.new(|cx| Host::detached(Some(stats(40., 5, 10)), cx));
        assert_eq!(cx.read(|app| host.read(app).stats), Some(stats(40., 5, 10)));
    }

    #[gpui::test]
    fn first_reading_is_dropped_then_one_per_gap_and_interval(cx: &mut TestAppContext) {
        let fake = Fake::new(40.);
        fake.init(cx);
        let host = host(cx);

        cx.run_until_parked();
        assert_eq!(fake.calls(), 1, "the baseline is taken");
        assert_eq!(shown(cx, &host), None, "and not shown");

        advance(cx, HOST_RESUME_GAP);
        assert_eq!(fake.calls(), 2);
        assert_eq!(shown(cx, &host).unwrap().cpu_label(), "40%");

        advance(cx, HOST_SAMPLE_EVERY);
        assert_eq!(fake.calls(), 3);
    }

    #[gpui::test]
    fn hidden_window_takes_no_readings(cx: &mut TestAppContext) {
        let fake = Fake::new(40.);
        fake.init(cx);
        advance(cx, HOST_RESUME_GAP);
        let before = fake.calls();

        set_visible(cx, false);
        // At most the timer already pending fires; it sees the hidden flag and stops.
        advance(cx, Duration::from_secs(60));
        assert_eq!(fake.calls(), before);
    }

    #[gpui::test]
    fn showing_again_takes_a_baseline_before_publishing(cx: &mut TestAppContext) {
        let fake = Fake::new(40.);
        fake.init(cx);
        advance(cx, HOST_RESUME_GAP);
        set_visible(cx, false);
        advance(cx, Duration::from_secs(60));
        let hidden = fake.calls();

        set_visible(cx, true);
        cx.run_until_parked();
        assert_eq!(fake.calls(), hidden + 1, "baseline on show");
        advance(cx, HOST_RESUME_GAP);
        assert_eq!(fake.calls(), hidden + 2);
        advance(cx, HOST_SAMPLE_EVERY);
        assert_eq!(fake.calls(), hidden + 3);
    }

    #[gpui::test]
    fn notifies_only_when_a_shown_label_changes(cx: &mut TestAppContext) {
        let fake = Fake::new(40.);
        fake.init(cx);
        let host = host(cx);
        let notified = Rc::new(Cell::new(0));
        let counter = notified.clone();
        let _subscription =
            cx.update(|app| app.observe(&host, move |_, _| counter.set(counter.get() + 1)));

        advance(cx, HOST_RESUME_GAP);
        assert_eq!(notified.get(), 1, "the first reading shows a value");

        // 40.2 rounds to the same "40%"; the memory label is unchanged too.
        *fake.cpu.lock().unwrap() = 40.2;
        advance(cx, HOST_SAMPLE_EVERY);
        assert_eq!(fake.calls(), 3);
        assert_eq!(notified.get(), 1);

        *fake.cpu.lock().unwrap() = 55.;
        advance(cx, HOST_SAMPLE_EVERY);
        assert_eq!(notified.get(), 2);
        assert_eq!(shown(cx, &host).unwrap().cpu_label(), "55%");
    }
}
