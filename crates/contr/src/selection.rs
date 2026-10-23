//! The sidebar's selection, shared by the spaces column (worktree cards) and the agents list
//! (agent rows). One entity behind one global, so a click in either list moves the card.

use gpui::{App, AppContext, Context, Entity, Global};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Selected {
    /// A worktree row, by its id: the canonical root path of the checkout.
    Worktree(String),
    /// An agent row, by herdr pane id.
    Agent(String),
}

#[derive(Default)]
pub struct Selection {
    pub current: Option<Selected>,
}

pub struct GlobalSelection(pub Entity<Selection>);

impl Global for GlobalSelection {}

impl Selection {
    /// Creates the selection and makes it global. Call once at startup, before a window opens.
    pub fn init(cx: &mut App) {
        let selection = cx.new(|_| Selection::default());
        cx.set_global(GlobalSelection(selection));
    }

    /// Sets the selection. Observers are notified only when the value changes.
    pub fn select(&mut self, selected: Selected, cx: &mut Context<Self>) {
        if self.current.as_ref() == Some(&selected) {
            return;
        }
        self.current = Some(selected);
        cx.notify();
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use gpui::TestAppContext;
    use std::cell::Cell;
    use std::rc::Rc;

    /// Counts how often `selection` notifies its observers.
    fn count_notifications(
        cx: &mut TestAppContext,
        selection: &Entity<Selection>,
    ) -> Rc<Cell<usize>> {
        let count = Rc::new(Cell::new(0));
        let seen = count.clone();
        cx.update(|cx| {
            cx.observe(selection, move |_, _| seen.set(seen.get() + 1))
                .detach()
        });
        count
    }

    #[gpui::test]
    fn selecting_the_same_value_notifies_once(cx: &mut TestAppContext) {
        let selection = cx.new(|_| Selection::default());
        let notified = count_notifications(cx, &selection);
        selection.update(cx, |s, cx| s.select(Selected::Worktree("a".into()), cx));
        selection.update(cx, |s, cx| s.select(Selected::Worktree("a".into()), cx));
        cx.run_until_parked();
        assert_eq!(notified.get(), 1);
        let current = selection.read_with(cx, |s, _| s.current.clone());
        assert_eq!(current, Some(Selected::Worktree("a".into())));
    }

    #[gpui::test]
    fn agent_replaces_worktree(cx: &mut TestAppContext) {
        let selection = cx.new(|_| Selection::default());
        let notified = count_notifications(cx, &selection);
        selection.update(cx, |s, cx| s.select(Selected::Worktree("a".into()), cx));
        selection.update(cx, |s, cx| s.select(Selected::Agent("w1:p1".into()), cx));
        cx.run_until_parked();
        assert_eq!(notified.get(), 2);
        let current = selection.read_with(cx, |s, _| s.current.clone());
        assert_eq!(current, Some(Selected::Agent("w1:p1".into())));
    }
}
