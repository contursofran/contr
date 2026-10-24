//! The sidebar's part of the dev state, and its clicks. Until it is built, it adds no
//! section and no control.

use crate::dev_protocol::DevError;
use crate::frame::FrameParts;
use gpui::{App, Window};
use serde_json::Value;

/// The `sidebar` section of the state. `null` until the sidebar is built.
pub(crate) fn state(_parts: &FrameParts, _window: &Window, _cx: &App) -> Value {
    Value::Null
}

/// The controls of the sidebar, with whether each is live. None until the sidebar is built.
pub(crate) fn controls(_parts: &FrameParts, _cx: &App) -> Vec<(&'static str, bool)> {
    Vec::new()
}

/// A click on a sidebar control. `None` when `id` is not one of its controls.
pub(crate) fn click(
    _id: &str,
    _parts: &FrameParts,
    _window: &mut Window,
    _cx: &mut App,
) -> Option<Result<(), DevError>> {
    None
}
