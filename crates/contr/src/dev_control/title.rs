//! The title bar's and the status bar's part of the dev state, and their clicks. A click calls the
//! same named method that the control's own click handler calls.

use crate::dev_protocol::{DevError, code};
use crate::frame::FrameParts;
use crate::status_bar::StatusBar;
use crate::status_icon::Status;
use gpui::{App, Window};
use serde_json::{Value, json};

/// The `title` section of the state: the workspace the strip works in, and its tabs. Both are
/// empty while herdr is not connected.
pub(crate) fn state(parts: &FrameParts, _window: &Window, cx: &App) -> Value {
    let strip = parts.title_bar.read(cx).strip();
    let tabs = strip
        .tabs
        .iter()
        .map(|tab| {
            json!({
                "id": format!("tab:{}", tab.id),
                "label": tab.label,
                "status": Status::from(tab.status).slug(),
                "active": tab.active,
            })
        })
        .collect::<Vec<_>>();
    json!({"workspace_id": &strip.workspace_id, "tabs": tabs})
}

/// The `status_bar` section of the state: the texts of the two sides, in display order, without
/// the separators.
pub(crate) fn status_bar_state(_parts: &FrameParts, _window: &Window, cx: &App) -> Value {
    json!({"left": StatusBar::left_texts(cx), "right": StatusBar::right_texts(cx)})
}

/// The controls of the title bar, with whether each is live. The strip draws a control only when
/// it has the target it needs.
pub(crate) fn controls(parts: &FrameParts, cx: &App) -> Vec<(&'static str, bool)> {
    let strip = parts.title_bar.read(cx).strip();
    vec![
        ("tab-new", strip.workspace_id.is_some()),
        ("split", strip.split_pane_id.is_some()),
        ("panel-toggle", true),
    ]
}

/// A click on a title bar control. `None` when `id` is not one of its controls.
pub(crate) fn click(
    id: &str,
    parts: &FrameParts,
    _window: &mut Window,
    cx: &mut App,
) -> Option<Result<(), DevError>> {
    let result = match id {
        "tab-new" => {
            if parts.title_bar.read(cx).strip().workspace_id.is_none() {
                Err(DevError::new(code::DISABLED, "the strip has no workspace"))
            } else {
                parts.title_bar.update(cx, |bar, cx| bar.click_new(cx));
                Ok(())
            }
        }
        "split" => {
            if parts.title_bar.read(cx).strip().split_pane_id.is_none() {
                Err(DevError::new(
                    code::DISABLED,
                    "the strip has no pane to split",
                ))
            } else {
                parts.title_bar.update(cx, |bar, cx| bar.click_split(cx));
                Ok(())
            }
        }
        "panel-toggle" => {
            // The title bar emits `TogglePanel` and the frame's listener calls this method; the
            // dev click calls the method itself, because the event reaches the frame only after
            // the reply's state has been read.
            parts.frame.update(cx, |frame, cx| frame.toggle_panel(cx));
            Ok(())
        }
        _ => return tab_click(id, parts, cx),
    };
    Some(result)
}

/// A click on `tab:<tab id>`. The id must be in the strip now. `None` for an id that is not a tab.
fn tab_click(id: &str, parts: &FrameParts, cx: &mut App) -> Option<Result<(), DevError>> {
    let tab_id = id.strip_prefix("tab:")?;
    let index = parts
        .title_bar
        .read(cx)
        .strip()
        .tabs
        .iter()
        .position(|tab| tab.id == tab_id);
    let Some(index) = index else {
        return Some(Err(DevError::new(
            code::UNKNOWN_ID,
            format!("the title bar shows no tab {tab_id}"),
        )));
    };
    parts
        .title_bar
        .update(cx, |bar, cx| bar.click_tab(index, cx));
    Some(Ok(()))
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::dev_control::dispatch::handle;
    use crate::dev_protocol::{Reply, Request};
    use crate::frame::Frame;
    use crate::host::HostStats;
    use crate::selection::Selection;
    use crate::status_bar;
    use crate::title_bar::TabCommand;
    use crate::title_bar::tests::{connected_with, herdr_in, sample_tree};
    use crate::visibility::Visibility;
    use contr_herdr::{ConnState, LiveHerdr};
    use gpui::{Entity, TestAppContext, VisualTestContext, px, size};
    use serde_json::Value;

    /// A frame in a window of the reference size, with the globals the frame's own tests use. The
    /// host has a fixed reading, so the status bar's texts are the same in every run.
    fn frame_with(
        cx: &mut TestAppContext,
        live: LiveHerdr,
    ) -> (Entity<Frame>, &mut VisualTestContext) {
        cx.update(Visibility::init);
        cx.update(Selection::init);
        herdr_in(cx, live);
        cx.update(crate::board::init_detached);
        let stats = HostStats {
            cpu_percent: 40.,
            mem_used: 50,
            mem_total: 100,
        };
        cx.update(|app| status_bar::init_detached(app, Some(stats)));
        let (frame, cx) = cx.add_window_view(Frame::new);
        cx.simulate_resize(size(px(1440.), px(900.)));
        cx.run_until_parked();
        (frame, cx)
    }

    /// A frame on a herdr that is not running: no tree, so the title bar has an empty strip.
    fn frame_without_herdr(cx: &mut TestAppContext) -> (Entity<Frame>, &mut VisualTestContext) {
        let not_running = LiveHerdr {
            state: ConnState::NotRunning {
                reason: "no socket".into(),
            },
            tree: None,
        };
        frame_with(cx, not_running)
    }

    /// Sends one request to the window, as the dev control does on the UI thread.
    fn send(cx: &mut VisualTestContext, request: Request) -> Reply {
        cx.update(|window, app| handle(request, window, app))
    }

    /// The state after the request, or a panic with the error.
    fn ok(reply: Reply) -> Value {
        match reply {
            Reply::Ok(result) => result,
            Reply::Err(error) => panic!("expected a result, got {}: {}", error.code, error.message),
        }
    }

    fn state(cx: &mut VisualTestContext) -> Value {
        ok(send(cx, Request::State))
    }

    fn click(cx: &mut VisualTestContext, id: &str) -> Reply {
        send(cx, Request::Click { id: id.into() })
    }

    fn error(reply: Reply) -> DevError {
        match reply {
            Reply::Err(error) => error,
            Reply::Ok(result) => panic!("expected an error, got {result}"),
        }
    }

    /// The herdr commands the frame sent, in order.
    fn sent(cx: &mut VisualTestContext, frame: &Entity<Frame>) -> Vec<TabCommand> {
        frame.read_with(cx, |frame, _| frame.sent.clone())
    }

    /// The `live` flag of one control in `state`, or `None` when it is not listed.
    fn live(state: &Value, id: &str) -> Option<Value> {
        state["controls"]
            .as_array()
            .unwrap()
            .iter()
            .find(|control| control["id"] == id)
            .map(|control| control["live"].clone())
    }

    #[gpui::test]
    fn the_state_lists_the_tabs_of_the_workspace(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected_with(sample_tree()));
        let title = &state(cx)["title"];
        assert_eq!(title["workspace_id"], "w1");
        assert_eq!(
            title["tabs"],
            json!([
                {"id": "tab:w1:t1", "label": "api", "status": "working", "active": true},
                {"id": "tab:w1:t2", "label": "docs", "status": "blocked", "active": false},
            ])
        );
    }

    #[gpui::test]
    fn the_title_controls_are_live_with_a_strip(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected_with(sample_tree()));
        let state = state(cx);
        assert_eq!(live(&state, "tab-new"), Some(json!(true)));
        assert_eq!(live(&state, "split"), Some(json!(true)));
        assert_eq!(live(&state, "panel-toggle"), Some(json!(true)));
    }

    #[gpui::test]
    fn the_state_without_a_strip_has_no_workspace_and_no_tabs(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_without_herdr(cx);
        let state = state(cx);
        let title = &state["title"];
        assert_eq!(title["workspace_id"], Value::Null);
        assert_eq!(title["tabs"], json!([]));
        assert_eq!(live(&state, "tab-new"), Some(json!(false)));
        assert_eq!(live(&state, "split"), Some(json!(false)));
        assert_eq!(live(&state, "panel-toggle"), Some(json!(true)));
    }

    #[gpui::test]
    fn a_click_on_an_inactive_tab_asks_herdr_to_focus_it(cx: &mut TestAppContext) {
        let (frame, cx) = frame_with(cx, connected_with(sample_tree()));
        ok(click(cx, "tab:w1:t2"));
        assert_eq!(sent(cx, &frame), vec![TabCommand::Focus("w1:t2".into())]);
    }

    #[gpui::test]
    fn a_click_on_the_active_tab_sends_nothing(cx: &mut TestAppContext) {
        let (frame, cx) = frame_with(cx, connected_with(sample_tree()));
        ok(click(cx, "tab:w1:t1"));
        assert!(sent(cx, &frame).is_empty());
    }

    #[gpui::test]
    fn a_click_on_a_tab_that_is_not_shown_is_unknown(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected_with(sample_tree()));
        // w2 is not the current workspace, so its tab is not in the strip.
        let error = error(click(cx, "tab:w2:t3"));
        assert_eq!(error.code, code::UNKNOWN_ID);
        assert!(error.message.contains("w2:t3"), "{}", error.message);
    }

    #[gpui::test]
    fn a_click_on_the_plus_asks_for_a_new_tab_in_the_workspace(cx: &mut TestAppContext) {
        let (frame, cx) = frame_with(cx, connected_with(sample_tree()));
        ok(click(cx, "tab-new"));
        assert_eq!(sent(cx, &frame), vec![TabCommand::Create("w1".into())]);
    }

    #[gpui::test]
    fn a_click_on_the_split_asks_to_split_the_pane_right(cx: &mut TestAppContext) {
        let (frame, cx) = frame_with(cx, connected_with(sample_tree()));
        ok(click(cx, "split"));
        assert_eq!(
            sent(cx, &frame),
            vec![TabCommand::SplitRight("w1:p1".into())]
        );
    }

    #[gpui::test]
    fn the_plus_and_the_split_are_disabled_without_a_strip(cx: &mut TestAppContext) {
        let (frame, cx) = frame_without_herdr(cx);
        assert_eq!(error(click(cx, "tab-new")).code, code::DISABLED);
        assert_eq!(error(click(cx, "split")).code, code::DISABLED);
        assert!(sent(cx, &frame).is_empty());
        // A tab of a workspace that herdr no longer reports is not on screen either.
        assert_eq!(error(click(cx, "tab:w1:t1")).code, code::UNKNOWN_ID);
    }

    #[gpui::test]
    fn the_panel_toggle_closes_and_opens_the_panel(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected_with(sample_tree()));
        let state_after = ok(click(cx, "panel-toggle"));
        assert_eq!(state_after["layout"]["panel_open"], false);
        let state_after = ok(click(cx, "panel-toggle"));
        assert_eq!(state_after["layout"]["panel_open"], true);
    }

    #[gpui::test]
    fn the_status_bar_section_shows_the_bar_texts(cx: &mut TestAppContext) {
        let (_frame, cx) = frame_with(cx, connected_with(sample_tree()));
        let status_bar = &state(cx)["status_bar"];
        assert_eq!(status_bar["right"], json!(cx.read(StatusBar::right_texts)));
        assert_eq!(status_bar["left"], json!(cx.read(StatusBar::left_texts)));
        assert_eq!(status_bar["right"][0], "CPU 40%");
        assert_eq!(status_bar["left"][0], "claude –");
    }
}
