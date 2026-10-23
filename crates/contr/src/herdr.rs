//! The herdr connection as the UI holds it. The reader thread in `contr-herdr` sends its updates
//! over a channel; a task here applies them on the UI thread. Nothing here does I/O.

use crate::visibility::{GlobalVisibility, Visibility};
use async_channel::Receiver;
use contr_herdr::{ConnState, HerdrReader, HerdrUpdate, LiveHerdr, Target, spawn_reader};
use gpui::{App, AppContext, Context, Entity, Global, Subscription, Task};
use std::io;
use tracing::warn;

/// Updates the reader may queue ahead of the UI. When the UI falls this far behind, the reader
/// thread waits for it.
const QUEUE: usize = 64;

/// The herdr connection. `live` is the reader's latest state and tree.
pub struct Herdr {
    pub live: LiveHerdr,
    /// The window is shown. While it is hidden, updates are kept but observers are not notified.
    visible: bool,
    /// `live` changed while the window was hidden, and observers have not been told yet.
    stale: bool,
    _reader: Option<HerdrReader>,
    _drain: Task<()>,
    _visibility: Subscription,
}

/// The app-wide handle to the herdr connection, created by `Herdr::init`.
pub struct GlobalHerdr(pub Entity<Herdr>);

impl Global for GlobalHerdr {}

impl Herdr {
    /// Follows the local herdr and registers the connection as a global. Panics without
    /// `Visibility::init` first.
    pub fn init(cx: &mut App) {
        let visibility = cx.global::<GlobalVisibility>().0.clone();
        let (tx, rx) = async_channel::bounded(QUEUE);
        let (reader, live) = reader_or_not_running(spawn_reader(Target::Local, tx));
        let herdr = cx.new(|cx| Self::new(live, reader, rx, &visibility, cx));
        cx.set_global(GlobalHerdr(herdr));
    }

    /// Keeps the reader's handle and starts the task that applies its updates in order.
    fn new(
        live: LiveHerdr,
        reader: Option<HerdrReader>,
        rx: Receiver<HerdrUpdate>,
        visibility: &Entity<Visibility>,
        cx: &mut Context<Self>,
    ) -> Self {
        let drain = cx.spawn(async move |this, cx| {
            while let Ok(update) = rx.recv().await {
                if this
                    .update(cx, |herdr, cx| herdr.apply(update, cx))
                    .is_err()
                {
                    break; // the connection was dropped
                }
            }
        });
        let visible = visibility.read(cx).visible;
        let visibility_sub = cx.observe(visibility, |herdr, visibility, cx| {
            herdr.visible = visibility.read(cx).visible;
            if herdr.visible && herdr.stale {
                herdr.stale = false;
                cx.notify();
            }
        });
        Self {
            live,
            visible,
            stale: false,
            _reader: reader,
            _drain: drain,
            _visibility: visibility_sub,
        }
    }

    /// Applies one update from the reader. Observers hear about it only when `live` changed, and
    /// only while the window is shown; a hidden change is announced when the window shows again.
    pub(crate) fn apply(&mut self, update: HerdrUpdate, cx: &mut Context<Self>) {
        let before = self.live.clone();
        self.live.apply(update);
        if self.live == before {
            return;
        }
        if self.visible {
            cx.notify();
        } else {
            self.stale = true;
        }
    }

    /// A connection with no reader and no queue, for the views' tests. Its window is shown.
    #[cfg(test)]
    pub fn detached(live: LiveHerdr, cx: &mut Context<Self>) -> Self {
        let visibility = cx.new(|_| Visibility { visible: true });
        let (_tx, rx) = async_channel::bounded(1);
        Self::new(live, None, rx, &visibility, cx)
    }
}

/// The reader handle and the state to start from. When the reader thread could not start, the
/// state is `NotRunning`, so the UI shows the same line as for a herdr that is not running.
fn reader_or_not_running(spawned: io::Result<HerdrReader>) -> (Option<HerdrReader>, LiveHerdr) {
    match spawned {
        Ok(reader) => (Some(reader), LiveHerdr::default()),
        Err(error) => {
            warn!(target: "contr_herdr", "reader did not start: {error}");
            let live = LiveHerdr {
                state: ConnState::NotRunning {
                    reason: error.to_string(),
                },
                tree: None,
            };
            (None, live)
        }
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use contr_herdr::HerdrTree;
    use gpui::TestAppContext;
    use std::cell::Cell;
    use std::rc::Rc;
    use std::sync::Arc;

    fn connected() -> ConnState {
        ConnState::Connected {
            server_version: "0.9.3".into(),
        }
    }

    fn empty_tree() -> HerdrTree {
        HerdrTree {
            version: "0.9.3".into(),
            focused_pane_id: None,
            workspaces: Vec::new(),
        }
    }

    fn visibility(cx: &mut TestAppContext) -> Entity<Visibility> {
        cx.update(Visibility::init);
        cx.read(|app| app.global::<GlobalVisibility>().0.clone())
    }

    fn set_visible(cx: &mut TestAppContext, visibility: &Entity<Visibility>, visible: bool) {
        cx.update(|app| visibility.update(app, |v, cx| v.set(visible, cx)));
    }

    /// A connection with no reader, fed only by the test, on `visibility`.
    fn herdr_on(cx: &mut TestAppContext, visibility: &Entity<Visibility>) -> Entity<Herdr> {
        cx.update(|app| {
            let (_tx, rx) = async_channel::bounded(1);
            app.new(|cx| Herdr::new(LiveHerdr::default(), None, rx, visibility, cx))
        })
    }

    fn apply(cx: &mut TestAppContext, herdr: &Entity<Herdr>, update: HerdrUpdate) {
        cx.update(|app| herdr.update(app, |herdr, cx| herdr.apply(update, cx)));
    }

    /// Counts the notifications `herdr` sends. Keep the returned subscription alive.
    fn count_notifications(
        cx: &mut TestAppContext,
        herdr: &Entity<Herdr>,
    ) -> (Rc<Cell<u32>>, Subscription) {
        let count = Rc::new(Cell::new(0));
        let seen = count.clone();
        let sub = cx.update(|app| app.observe(herdr, move |_, _| seen.set(seen.get() + 1)));
        (count, sub)
    }

    #[test]
    fn a_reader_that_did_not_start_means_not_running() {
        let (reader, live) = reader_or_not_running(Err(io::Error::other("no thread")));
        assert!(reader.is_none());
        assert_eq!(
            live,
            LiveHerdr {
                state: ConnState::NotRunning {
                    reason: "no thread".into()
                },
                tree: None,
            }
        );
    }

    #[test]
    fn a_started_reader_is_kept_and_starts_connecting() {
        // A socket path that does not exist: the reader retries in the background until dropped.
        let dir = tempfile::tempdir().unwrap();
        let (tx, _rx) = async_channel::bounded(1);
        let spawned = spawn_reader(Target::Socket(dir.path().join("missing.sock")), tx);
        let (reader, live) = reader_or_not_running(spawned);
        assert!(reader.is_some());
        assert_eq!(live, LiveHerdr::default());
    }

    #[gpui::test]
    fn apply_notifies_only_when_something_changed(cx: &mut TestAppContext) {
        let visibility = visibility(cx);
        let herdr = herdr_on(cx, &visibility);
        let (count, _sub) = count_notifications(cx, &herdr);

        apply(cx, &herdr, HerdrUpdate::State(connected()));
        assert_eq!(count.get(), 1);
        apply(cx, &herdr, HerdrUpdate::State(connected())); // same state again
        assert_eq!(count.get(), 1);
        apply(cx, &herdr, HerdrUpdate::Tree(Arc::new(empty_tree())));
        assert_eq!(count.get(), 2);
        apply(cx, &herdr, HerdrUpdate::Tree(Arc::new(empty_tree()))); // same tree, new allocation
        assert_eq!(count.get(), 2);
        apply(
            cx,
            &herdr,
            HerdrUpdate::State(ConnState::NotRunning {
                reason: "gone".into(),
            }),
        );
        assert_eq!(count.get(), 3);
        assert!(cx.read(|app| herdr.read(app).live.tree.is_none()));
    }

    #[gpui::test]
    fn changes_while_hidden_are_kept_and_announced_once_shown(cx: &mut TestAppContext) {
        let visibility = visibility(cx);
        let herdr = herdr_on(cx, &visibility);
        let (count, _sub) = count_notifications(cx, &herdr);
        set_visible(cx, &visibility, false);

        apply(cx, &herdr, HerdrUpdate::State(connected()));
        apply(cx, &herdr, HerdrUpdate::Tree(Arc::new(empty_tree())));
        assert_eq!(count.get(), 0, "hidden changes must not notify");
        assert!(cx.read(|app| herdr.read(app).live.tree.is_some()));

        set_visible(cx, &visibility, true);
        assert_eq!(count.get(), 1, "showing announces the hidden changes once");

        apply(cx, &herdr, HerdrUpdate::State(ConnState::Connecting));
        assert_eq!(count.get(), 2, "shown changes notify at once");
    }

    #[gpui::test]
    fn showing_without_a_hidden_change_does_not_notify(cx: &mut TestAppContext) {
        let visibility = visibility(cx);
        let herdr = herdr_on(cx, &visibility);
        apply(cx, &herdr, HerdrUpdate::State(connected()));
        let (count, _sub) = count_notifications(cx, &herdr);

        set_visible(cx, &visibility, false);
        set_visible(cx, &visibility, true);
        assert_eq!(count.get(), 0);
    }

    #[gpui::test]
    fn updates_from_the_reader_reach_the_entity_in_order(cx: &mut TestAppContext) {
        let visibility = visibility(cx);
        let (tx, rx) = async_channel::bounded(8);
        let herdr = cx.update(|app| {
            app.new(|cx| Herdr::new(LiveHerdr::default(), None, rx, &visibility, cx))
        });
        tx.try_send(HerdrUpdate::State(connected())).unwrap();
        tx.try_send(HerdrUpdate::Tree(Arc::new(empty_tree())))
            .unwrap();
        cx.run_until_parked();

        let live = cx.read(|app| herdr.read(app).live.clone());
        assert_eq!(live.state, connected());
        assert_eq!(live.tree.as_deref(), Some(&empty_tree()));
    }
}
