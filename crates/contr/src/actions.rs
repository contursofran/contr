// Adapted from Herdr UI (Apache-2.0); changes: only the app menu's actions and keys (Quit, Hide,
// Hide Others, Minimize, About, Copy, Paste, Select All); fixed macOS keystrokes instead of a
// keymap catalog; no config, palette, daemon or update actions.

//! The app menu's actions and the keystrokes bound to them. The menus read their shortcut labels
//! from these bindings, so a menu and its key cannot drift apart.

use gpui::{App, KeyBinding, actions};

actions!(app, [About, Quit, Hide, HideOthers, Minimize]);

// Edit menu items. The terminal view handles them while it has focus, so GPUI enables an item only
// where a handler exists.
actions!(edit, [Copy, Paste, SelectAll]);

/// No element sets this context, so these bindings never match a keystroke. They exist only so the
/// Edit menu shows its shortcuts. The focused element's key handler keeps the keystrokes.
const EDIT_MENU_LABELS: &str = "EditMenuLabels";

/// Binds Cmd-Q, Cmd-H, Cmd-Alt-H and Cmd-M to their actions, and gives the Edit menu its labels.
pub fn bind_keys(cx: &mut App) {
    cx.bind_keys([
        KeyBinding::new("cmd-q", Quit, None),
        KeyBinding::new("cmd-h", Hide, None),
        KeyBinding::new("cmd-alt-h", HideOthers, None),
        KeyBinding::new("cmd-m", Minimize, None),
        KeyBinding::new("cmd-c", Copy, Some(EDIT_MENU_LABELS)),
        KeyBinding::new("cmd-v", Paste, Some(EDIT_MENU_LABELS)),
        KeyBinding::new("cmd-a", SelectAll, Some(EDIT_MENU_LABELS)),
    ]);
}

/// Registers the handlers of the app menu's actions. The Edit actions have no handler here: the
/// terminal view handles them.
pub fn register(cx: &mut App) {
    cx.on_action(|_: &Quit, cx| cx.quit());
    cx.on_action(|_: &Hide, cx| cx.hide());
    cx.on_action(|_: &HideOthers, cx| cx.hide_other_apps());
    cx.on_action(|_: &Minimize, cx| minimize_active_window(cx));
    cx.on_action(|_: &About, _| show_about_panel());
}

/// Binds the keys, registers the handlers and installs the menu bar. Called once, before the
/// first window opens.
pub fn init(cx: &mut App) {
    bind_keys(cx);
    register(cx);
    crate::menus::install(cx);
}

/// Minimizes the active window. With no active window there is nothing to minimize.
fn minimize_active_window(cx: &mut App) {
    if let Some(window) = cx.active_window() {
        // A window that closed meanwhile fails the update, and then there is nothing to minimize.
        let _ = window.update(cx, |_, window, _| window.minimize_window());
    }
}

/// Opens the standard About panel, which shows the app name and version from the bundle. Off the
/// main thread there is no AppKit application, so nothing opens.
#[cfg(target_os = "macos")]
fn show_about_panel() {
    use objc2::MainThreadMarker;
    use objc2_app_kit::NSApplication;

    if let Some(mtm) = MainThreadMarker::new() {
        NSApplication::sharedApplication(mtm).orderFrontStandardAboutPanel(None);
    }
}

/// The About panel is macOS only. Elsewhere the item does nothing.
#[cfg(not(target_os = "macos"))]
fn show_about_panel() {}

#[cfg(test)]
mod tests {
    use std::cell::RefCell;
    use std::rc::Rc;

    use gpui::{Action, AsKeystroke, Keystroke, TestAppContext};

    use super::*;

    type Log = Rc<RefCell<Vec<&'static str>>>;

    /// Records the name of each app action that runs, in order. The real handlers are not
    /// registered here: the test platform cannot hide the app or minimize a window.
    fn record_app_actions(cx: &mut App) -> Log {
        let log: Log = Rc::new(RefCell::new(Vec::new()));
        let (quit, hide, hide_others, minimize) =
            (log.clone(), log.clone(), log.clone(), log.clone());
        cx.on_action(move |_: &Quit, _| quit.borrow_mut().push("quit"));
        cx.on_action(move |_: &Hide, _| hide.borrow_mut().push("hide"));
        cx.on_action(move |_: &HideOthers, _| hide_others.borrow_mut().push("hide others"));
        cx.on_action(move |_: &Minimize, _| minimize.borrow_mut().push("minimize"));
        log
    }

    /// Records the name of each Edit action that the keymap runs, in order.
    fn record_edit_actions(cx: &mut App) -> Log {
        let log: Log = Rc::new(RefCell::new(Vec::new()));
        let (copy, paste, select_all) = (log.clone(), log.clone(), log.clone());
        cx.on_action(move |_: &Copy, _| copy.borrow_mut().push("copy"));
        cx.on_action(move |_: &Paste, _| paste.borrow_mut().push("paste"));
        cx.on_action(move |_: &SelectAll, _| select_all.borrow_mut().push("select all"));
        log
    }

    #[gpui::test]
    fn cmd_q_h_alt_h_and_m_run_their_app_actions(cx: &mut TestAppContext) {
        let log = cx.update(|app| {
            bind_keys(app);
            record_app_actions(app)
        });
        let window = cx.add_empty_window();

        window.simulate_keystrokes("cmd-q cmd-h cmd-alt-h cmd-m");

        assert_eq!(
            *log.borrow(),
            vec!["quit", "hide", "hide others", "minimize"]
        );
    }

    #[gpui::test]
    fn the_edit_labels_do_not_run_their_actions_from_the_keymap(cx: &mut TestAppContext) {
        // The terminal's key handler owns Cmd-C, Cmd-V and Cmd-A. A keymap match would run the
        // action on top of it, so Cmd-V would paste twice.
        let log = cx.update(|app| {
            bind_keys(app);
            record_edit_actions(app)
        });
        let window = cx.add_empty_window();

        window.simulate_keystrokes("cmd-c cmd-v cmd-a");

        assert!(log.borrow().is_empty(), "{:?}", log.borrow());
    }

    #[gpui::test]
    fn each_shortcut_of_the_menus_is_bound(cx: &mut TestAppContext) {
        cx.update(bind_keys);
        cx.update(|app| {
            let cases: [(&dyn Action, &str); 7] = [
                (&Quit, "cmd-q"),
                (&Hide, "cmd-h"),
                (&HideOthers, "cmd-alt-h"),
                (&Minimize, "cmd-m"),
                (&Copy, "cmd-c"),
                (&Paste, "cmd-v"),
                (&SelectAll, "cmd-a"),
            ];
            for (action, keys) in cases {
                let want = Keystroke::parse(keys).ok();
                assert_eq!(first_keystroke(app, action), want, "{}", action.name());
            }
        });
    }

    /// The first keystroke bound to `action`, or `None` when nothing is bound.
    fn first_keystroke(cx: &App, action: &dyn Action) -> Option<Keystroke> {
        let keymap = cx.key_bindings();
        let keymap = keymap.borrow();
        keymap
            .bindings_for_action(action)
            .next()
            .map(|binding| binding.keystrokes()[0].as_keystroke().clone())
    }

    #[gpui::test]
    fn minimize_with_no_window_does_nothing(cx: &mut TestAppContext) {
        assert!(cx.update(|app| app.active_window().is_none()));

        cx.update(minimize_active_window);
    }
}
