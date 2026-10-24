//! The dev control's request dispatcher. It runs on the UI thread, inside the window's update, and
//! answers one request. The state reads the same models that the views draw. A click calls the
//! same path that the control's own handler calls.

use super::{board, sidebar, title, window_id};
use crate::dev_protocol::{DevError, Reply, Request, VERSION, code};
use crate::frame::{Frame, FrameParts, parts};
use crate::selection::{GlobalSelection, Selected};
use crate::visibility::GlobalVisibility;
use gpui::{App, Keystroke, Window, px, size};
use serde_json::{Value, json};
use std::ops::RangeInclusive;

/// The sides a `resize` may ask for, in pixels.
const SIZE_RANGE: RangeInclusive<f32> = 1.0..=10000.0;

/// Answers one request. A window whose root view is not the frame answers `no_window`.
pub fn handle(request: Request, window: &mut Window, cx: &mut App) -> Reply {
    let Some(Some(root)) = window.root::<Frame>() else {
        return Reply::Err(DevError::new(
            code::NO_WINDOW,
            "the window does not show the frame",
        ));
    };
    let parts = parts(&root, cx);
    match request {
        Request::State => Reply::Ok(state(&parts, window, cx)),
        Request::Click { id } => {
            let result = click(&id, &parts, window, cx);
            finish(result, &parts, window, cx)
        }
        Request::Select { row } => {
            let result = select(&row, &parts, window, cx);
            finish(result, &parts, window, cx)
        }
        Request::Key { keys } => {
            let result = key(&keys, window, cx);
            finish(result, &parts, window, cx)
        }
        Request::Resize { width, height } => {
            let result = resize(width, height, window);
            finish(result, &parts, window, cx)
        }
        Request::WindowId => window_id::current(),
    }
}

/// The reply to a drive command: the state after the command ran, or its error.
fn finish(
    result: Result<(), DevError>,
    parts: &FrameParts,
    window: &Window,
    cx: &mut App,
) -> Reply {
    match result {
        Ok(()) => Reply::Ok(state(parts, window, cx)),
        Err(error) => Reply::Err(error),
    }
}

/// A click. The first section that owns `id` decides it. An id that no section owns is
/// `unknown_id`.
fn click(id: &str, parts: &FrameParts, window: &mut Window, cx: &mut App) -> Result<(), DevError> {
    if let Some(result) = core_click(id, parts, window, cx) {
        return result;
    }
    if let Some(result) = sidebar::click(id, parts, window, cx) {
        return result;
    }
    if let Some(result) = title::click(id, parts, window, cx) {
        return result;
    }
    if let Some(result) = board::click(id, parts, window, cx) {
        return result;
    }
    Err(DevError::new(
        code::UNKNOWN_ID,
        format!("no control has the id {id}"),
    ))
}

/// The clicks of the frame itself: the terminal, the board panel and the sidebar's reopen button.
/// `None` when `id` is not one of them.
fn core_click(
    id: &str,
    parts: &FrameParts,
    window: &mut Window,
    cx: &mut App,
) -> Option<Result<(), DevError>> {
    let result = match id {
        "terminal" => {
            let focus = parts.terminal.read(cx).focus_handle().clone();
            window.focus(&focus, cx);
            Ok(())
        }
        "board-panel" if !parts.frame.read(cx).panel_open() => {
            Err(DevError::new(code::DISABLED, "the board panel is closed"))
        }
        "board-panel" => {
            // A real mouse down on the panel takes its focus the same way.
            let focus = parts.board_panel.read(cx).focus_handle().clone();
            window.focus(&focus, cx);
            Ok(())
        }
        "sidebar-reopen" if parts.frame.read(cx).sidebar_open() => {
            Err(DevError::new(code::DISABLED, "the sidebar is open"))
        }
        "sidebar-reopen" => {
            parts.frame.update(cx, |frame, cx| frame.reopen_sidebar(cx));
            Ok(())
        }
        _ => return None,
    };
    Some(result)
}

/// `select`: a click on a worktree or an agent row. Any other id is not a row.
fn select(
    row: &str,
    parts: &FrameParts,
    window: &mut Window,
    cx: &mut App,
) -> Result<(), DevError> {
    if !(row.starts_with("wt:") || row.starts_with("agent:")) {
        return Err(DevError::new(
            code::NOT_A_ROW,
            format!("{row} is not a worktree or agent row"),
        ));
    }
    click(row, parts, window, cx)
}

/// `key`: every key is read first, so a bad one sends none. Then each goes to the focused element,
/// in order, as a typed key does.
fn key(keys: &[String], window: &mut Window, cx: &mut App) -> Result<(), DevError> {
    let strokes = keys
        .iter()
        .map(|key| {
            Keystroke::parse(key).map_err(|error| DevError::new(code::BAD_KEY, error.to_string()))
        })
        .collect::<Result<Vec<_>, _>>()?;
    for stroke in strokes {
        window.dispatch_keystroke(stroke, cx);
    }
    Ok(())
}

/// `resize`: the window asks the platform for the size. The platform applies its minimum and
/// reports the size later, so the state shows the new size only after that.
fn resize(width: f32, height: f32, window: &mut Window) -> Result<(), DevError> {
    if !SIZE_RANGE.contains(&width) || !SIZE_RANGE.contains(&height) {
        return Err(DevError::new(
            code::BAD_SIZE,
            format!("the width and the height must be 1 to 10000, got {width} x {height}"),
        ));
    }
    window.resize(size(px(width), px(height)));
    Ok(())
}

/// The state object of the Contract, version 1. The sections that a later slice adds are `null`
/// until it lands.
fn state(parts: &FrameParts, window: &Window, cx: &mut App) -> Value {
    let viewport = window.viewport_size();
    let visible = cx.global::<GlobalVisibility>().0.read(cx).visible;
    let frame = parts.frame.read(cx);
    json!({
        "v": VERSION,
        "window": {
            "width": f32::from(viewport.width),
            "height": f32::from(viewport.height),
            "visible": visible,
            "active": window.is_window_active(),
        },
        "focus": focus(parts, window, cx),
        "layout": {"sidebar_open": frame.sidebar_open(), "panel_open": frame.panel_open()},
        "herdr": {"line": parts.herdr.read(cx).live.state.line()},
        "selection": selection(cx),
        "controls": controls(parts, cx),
        "sidebar": sidebar::state(parts, window, cx),
        "title": title::state(parts, window, cx),
        "status_bar": title::status_bar_state(parts, window, cx),
        "board": board::state(parts, window, cx),
    })
}

/// Which element holds the keyboard focus: the board panel, the terminal, another element or none.
fn focus(parts: &FrameParts, window: &Window, cx: &App) -> &'static str {
    let Some(focused) = window.focused(cx) else {
        return "none";
    };
    if focused == *parts.board_panel.read(cx).focus_handle() {
        "panel"
    } else if focused == *parts.terminal.read(cx).focus_handle() {
        "terminal"
    } else {
        "other"
    }
}

/// The selected row, or `null`.
fn selection(cx: &App) -> Value {
    match cx.global::<GlobalSelection>().0.read(cx).current.as_ref() {
        Some(Selected::Worktree(id)) => json!({"kind": "worktree", "id": id}),
        Some(Selected::Agent(id)) => json!({"kind": "agent", "id": id}),
        None => Value::Null,
    }
}

/// The `controls` list: the fixed ids of the frame, each with whether it is live now, then the
/// ids of the sections.
fn controls(parts: &FrameParts, cx: &App) -> Value {
    let frame = parts.frame.read(cx);
    let mut list = vec![
        ("terminal", true),
        ("board-panel", frame.panel_open()),
        ("sidebar-reopen", !frame.sidebar_open()),
    ];
    list.extend(sidebar::controls(parts, cx));
    list.extend(title::controls(parts, cx));
    list.extend(board::controls(parts, cx));
    Value::Array(
        list.into_iter()
            .map(|(id, live)| json!({"id": id, "live": live}))
            .collect(),
    )
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::board::GlobalBoard;
    use crate::board_card::BoardPick;
    use crate::board_panel::BoardPanel;
    use crate::board_panel::test_support::{bridge, fixture};
    use crate::selection::Selection;
    use crate::spaces::SpacesEvent;
    use crate::status_bar;
    use crate::title_bar::TitleBarEvent;
    use crate::title_bar::tests::herdr_in;
    use crate::visibility::Visibility;
    use contr_herdr::{ConnState, LiveHerdr};
    use gpui::{
        AppContext, Context, Entity, IntoElement, Render, TestAppContext, VisualTestContext,
    };

    fn connected() -> LiveHerdr {
        LiveHerdr {
            state: ConnState::Connected {
                server_version: "0.9.3".into(),
            },
            tree: None,
        }
    }

    /// A frame in a window of the reference size, with the globals the frame's own tests use.
    /// The board panel's keys are bound, so the key tests can press them.
    fn frame_with(
        cx: &mut TestAppContext,
        live: LiveHerdr,
    ) -> (Entity<Frame>, &mut VisualTestContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        cx.update(BoardPanel::bind_keys);
        herdr_in(cx, live);
        cx.update(crate::board::init_detached);
        cx.update(|app| status_bar::init_detached(app, None));
        let (frame, cx) = cx.add_window_view(Frame::new);
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        (frame, cx)
    }

    /// Sends one request to the window, as the dev control does on the UI thread.
    fn send(cx: &mut VisualTestContext, request: Request) -> Reply {
        cx.update(|window, app| handle(request, window, app))
    }

    fn click_id(cx: &mut VisualTestContext, id: &str) -> Reply {
        send(cx, Request::Click { id: id.into() })
    }

    fn ok(reply: Reply) -> Value {
        match reply {
            Reply::Ok(result) => result,
            Reply::Err(error) => panic!("expected a result, got {}: {}", error.code, error.message),
        }
    }

    fn err(reply: Reply) -> DevError {
        match reply {
            Reply::Err(error) => error,
            Reply::Ok(result) => panic!("expected an error, got {result}"),
        }
    }

    fn focus_of(cx: &mut VisualTestContext) -> Value {
        ok(send(cx, Request::State))["focus"].clone()
    }

    /// Puts the frame's board in a state with items, and picks nothing.
    fn board_with_fixture(cx: &mut VisualTestContext) {
        cx.update(|_, app| {
            let board = app.global::<GlobalBoard>().0.clone();
            board.update(app, |board, cx| {
                board.set(Some(fixture()), None, Some(bridge()), cx)
            });
        });
    }

    /// Closes the sidebar, as the sidebar's toggle does.
    fn close_sidebar(cx: &mut VisualTestContext, frame: &Entity<Frame>) {
        let spaces = cx.update(|_, app| parts(frame, app).spaces);
        spaces.update(cx, |_, cx| cx.emit(SpacesEvent::ToggleSidebar));
        cx.run_until_parked();
    }

    /// Closes the board panel, as the title bar's panel toggle does.
    fn close_panel(cx: &mut VisualTestContext, frame: &Entity<Frame>) {
        let title_bar = cx.update(|_, app| parts(frame, app).title_bar);
        title_bar.update(cx, |_, cx| cx.emit(TitleBarEvent::TogglePanel));
        cx.run_until_parked();
    }

    #[gpui::test]
    fn the_state_shows_the_frame_at_the_start(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected());
        let state = ok(send(cx, Request::State));
        assert_eq!(state["v"], 1);
        assert_eq!(state["window"]["width"], 1440.0);
        assert_eq!(state["window"]["height"], 900.0);
        assert_eq!(state["window"]["visible"], true);
        assert_eq!(
            state["layout"],
            json!({"sidebar_open": true, "panel_open": true})
        );
        assert_eq!(state["herdr"]["line"], Value::Null);
        assert_eq!(state["selection"], Value::Null);
        // The terminal takes the focus when it is built.
        assert_eq!(state["focus"], "terminal");
        // The sidebar is open, so its section is there. It has no repos without a herdr tree.
        assert_eq!(state["sidebar"]["repos"], json!([]));
        // The title bar and the status bar follow the same herdr tree.
        assert_eq!(state["title"]["tabs"], json!([]));
        assert!(state["status_bar"]["right"].is_array());
        // The board panel is shown from the start, on its Board tab, with no board to read.
        assert_eq!(state["board"]["tab"], "board");
        assert_eq!(state["board"]["line"], "no coordinator board");
    }

    #[gpui::test]
    fn the_state_shows_the_herdr_line_while_herdr_is_not_connected(cx: &mut TestAppContext) {
        let not_running = LiveHerdr {
            state: ConnState::NotRunning {
                reason: "no socket".into(),
            },
            tree: None,
        };
        let line = not_running.state.line();
        let (_frame, cx) = frame_with(cx, not_running);
        assert_eq!(ok(send(cx, Request::State))["herdr"]["line"], json!(line));
    }

    #[gpui::test]
    fn the_controls_are_listed_with_whether_they_are_live(cx: &mut TestAppContext) {
        let (frame, cx) = frame_with(cx, connected());
        let live = |state: &Value, id: &str| {
            state["controls"]
                .as_array()
                .unwrap()
                .iter()
                .find(|control| control["id"] == id)
                .map(|control| control["live"].clone())
        };
        let state = ok(send(cx, Request::State));
        assert_eq!(live(&state, "terminal"), Some(json!(true)));
        assert_eq!(live(&state, "board-panel"), Some(json!(true)));
        assert_eq!(live(&state, "sidebar-reopen"), Some(json!(false)));

        close_sidebar(cx, &frame);
        let state = ok(send(cx, Request::State));
        assert_eq!(live(&state, "sidebar-reopen"), Some(json!(true)));
    }

    #[gpui::test]
    fn the_selection_is_read_from_the_selection_global(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected());
        let selection = cx.update(|_, app| app.global::<GlobalSelection>().0.clone());
        selection.update(cx, |selection, cx| {
            selection.select(Selected::Worktree("/work/a".into()), cx)
        });
        assert_eq!(
            ok(send(cx, Request::State))["selection"],
            json!({"kind": "worktree", "id": "/work/a"})
        );
        selection.update(cx, |selection, cx| {
            selection.select(Selected::Agent("w1:p1".into()), cx)
        });
        assert_eq!(
            ok(send(cx, Request::State))["selection"],
            json!({"kind": "agent", "id": "w1:p1"})
        );
    }

    #[gpui::test]
    fn clicking_the_board_panel_focuses_it_and_the_terminal_takes_it_back(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected());
        ok(click_id(cx, "board-panel"));
        assert_eq!(focus_of(cx), "panel");
        ok(click_id(cx, "terminal"));
        assert_eq!(focus_of(cx), "terminal");
    }

    #[gpui::test]
    fn a_focus_held_elsewhere_is_other_and_none_when_nothing_has_it(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected());
        // The handle must live on: a focus whose handle is dropped is no focus.
        let elsewhere = cx.update(|_, app| app.focus_handle());
        cx.update(|window, app| window.focus(&elsewhere, app));
        assert_eq!(focus_of(cx), "other");
        cx.update(|window, app| window.blur(app));
        assert_eq!(focus_of(cx), "none");
    }

    #[gpui::test]
    fn the_board_panel_click_is_disabled_while_the_panel_is_closed(cx: &mut TestAppContext) {
        let (frame, cx) = frame_with(cx, connected());
        close_panel(cx, &frame);
        let error = err(click_id(cx, "board-panel"));
        assert_eq!(error.code, code::DISABLED);
        assert_eq!(ok(send(cx, Request::State))["layout"]["panel_open"], false);
    }

    #[gpui::test]
    fn the_sidebar_reopen_is_disabled_while_the_sidebar_is_open(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected());
        let error = err(click_id(cx, "sidebar-reopen"));
        assert_eq!(error.code, code::DISABLED);
    }

    #[gpui::test]
    fn the_sidebar_reopen_opens_a_closed_sidebar(cx: &mut TestAppContext) {
        let (frame, cx) = frame_with(cx, connected());
        close_sidebar(cx, &frame);
        let state = ok(click_id(cx, "sidebar-reopen"));
        assert_eq!(state["layout"]["sidebar_open"], true);
    }

    #[gpui::test]
    fn an_id_that_no_control_owns_is_unknown(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected());
        for id in ["nope", "tab:"] {
            assert_eq!(err(click_id(cx, id)).code, code::UNKNOWN_ID, "{id}");
        }
    }

    #[gpui::test]
    fn a_key_reaches_the_focused_board_panel(cx: &mut TestAppContext) {
        let (frame, cx) = frame_with(cx, connected());
        board_with_fixture(cx);
        let panel = cx.update(|_, app| parts(&frame, app).board_panel);
        let picked =
            |cx: &mut VisualTestContext| panel.read_with(cx, |panel, _| panel.picked().cloned());

        // With the terminal focused, the key goes there and the pick does not move.
        ok(send(
            cx,
            Request::Key {
                keys: vec!["down".into()],
            },
        ));
        assert_eq!(picked(cx), None);

        ok(click_id(cx, "board-panel"));
        ok(send(
            cx,
            Request::Key {
                keys: vec!["down".into()],
            },
        ));
        assert_eq!(picked(cx), Some(BoardPick::Item("D7".into())));
    }

    #[gpui::test]
    fn a_bad_key_is_refused_and_no_key_is_sent(cx: &mut TestAppContext) {
        let (frame, cx) = frame_with(cx, connected());
        board_with_fixture(cx);
        let panel = cx.update(|_, app| parts(&frame, app).board_panel);
        ok(click_id(cx, "board-panel"));

        let error = err(send(
            cx,
            Request::Key {
                keys: vec!["down".into(), "not-a-key!!".into()],
            },
        ));
        assert_eq!(error.code, code::BAD_KEY);
        assert!(error.message.contains("not-a-key!!"), "{}", error.message);
        // The good key before the bad one was not sent either.
        assert_eq!(
            panel.read_with(cx, |panel, _| panel.picked().cloned()),
            None
        );
    }

    #[gpui::test]
    fn a_resize_asks_the_platform_for_the_size(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected());
        ok(send(
            cx,
            Request::Resize {
                width: 1200.0,
                height: 800.0,
            },
        ));
        let bounds = cx.update(|window, _| window.bounds());
        assert_eq!(bounds.size, size(px(1200.), px(800.)));
        // The platform reports the new size later. The test reports it now, as the platform does.
        cx.simulate_resize(size(px(1200.), px(800.)));
        cx.run_until_parked();
        let state = ok(send(cx, Request::State));
        assert_eq!(state["window"]["width"], 1200.0);
        assert_eq!(state["window"]["height"], 800.0);
    }

    #[gpui::test]
    fn a_size_outside_the_range_is_refused(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected());
        for (width, height) in [(0.0, 10.0), (10.0, 10001.0), (f32::NAN, 10.0)] {
            let error = err(send(cx, Request::Resize { width, height }));
            assert_eq!(error.code, code::BAD_SIZE, "{width} x {height}");
        }
        let bounds = cx.update(|window, _| window.bounds());
        assert_eq!(bounds.size, size(px(1440.), px(900.)));
    }

    #[gpui::test]
    fn select_takes_only_row_ids(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected());
        assert_eq!(
            err(send(
                cx,
                Request::Select {
                    row: "tab:w1:t1".into()
                }
            ))
            .code,
            code::NOT_A_ROW
        );
        assert_eq!(
            err(send(
                cx,
                Request::Select {
                    row: "terminal".into()
                }
            ))
            .code,
            code::NOT_A_ROW
        );
        // A row id passes the row check. No row has this id, so it is an unknown id.
        let reply = send(
            cx,
            Request::Select {
                row: "wt:/work/a".into(),
            },
        );
        assert_eq!(err(reply).code, code::UNKNOWN_ID);
    }

    #[gpui::test]
    fn the_window_id_request_goes_to_the_window_id_reader(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected());
        // Test threads are not the main thread, so no window number is found on macOS. Elsewhere
        // the number is unsupported. Either way the request is not answered with the state.
        let error = err(send(cx, Request::WindowId));
        assert!(
            error.code == code::NO_WINDOW || error.code == code::UNSUPPORTED,
            "{}",
            error.code
        );
    }

    #[gpui::test]
    fn a_window_that_is_not_the_frame_answers_no_window(cx: &mut TestAppContext) {
        /// A root view that draws nothing, and is not the frame.
        struct Blank;

        impl Render for Blank {
            fn render(
                &mut self,
                _window: &mut Window,
                _cx: &mut Context<Self>,
            ) -> impl IntoElement {
                gpui::div()
            }
        }

        let blank: gpui::AnyWindowHandle = cx.add_window(|_, _| Blank).into();
        let reply = cx
            .update_window(blank, |_, window, app| handle(Request::State, window, app))
            .unwrap();
        assert_eq!(err(reply).code, code::NO_WINDOW);
    }
}
