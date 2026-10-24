//! The title bar's and the status bar's part of the dev state, and their clicks. Until it is built,
//! both sections are `null` and add no control.

use crate::dev_protocol::DevError;
use crate::frame::FrameParts;
use gpui::{App, Window};
use serde_json::Value;

/// The `title` section of the state. `null` until the title bar is built.
pub(crate) fn state(_parts: &FrameParts, _window: &Window, _cx: &App) -> Value {
    Value::Null
}

/// The `status_bar` section of the state. `null` until the title bar is built.
pub(crate) fn status_bar_state(_parts: &FrameParts, _window: &Window, _cx: &App) -> Value {
    Value::Null
}

/// The controls of the title bar, with whether each is live. None until the title bar is built.
pub(crate) fn controls(_parts: &FrameParts, _cx: &App) -> Vec<(&'static str, bool)> {
    Vec::new()
}

/// A click on a title bar control. `None` when `id` is not one of its controls.
pub(crate) fn click(
    _id: &str,
    _parts: &FrameParts,
    _window: &mut Window,
    _cx: &mut App,
) -> Option<Result<(), DevError>> {
    None
}
