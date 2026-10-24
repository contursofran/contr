//! The window number of the frame window, which `screencapture -l` takes. GPUI does not expose
//! it, so it is read from AppKit. The calls used are safe ones, so no `unsafe` is needed.

use crate::dev_protocol::{DevError, Reply, code};

/// The title of the frame window. `main.rs` sets it.
#[cfg(target_os = "macos")]
const TITLE: &str = "contr";

/// The window number of the visible frame window. Off the main thread there is no AppKit
/// application to ask, so the answer is `no_window`.
#[cfg(target_os = "macos")]
pub fn current() -> Reply {
    use objc2::MainThreadMarker;
    use objc2_app_kit::NSApplication;
    use serde_json::json;

    let Some(mtm) = MainThreadMarker::new() else {
        return Reply::Err(DevError::new(code::NO_WINDOW, "not on the main thread"));
    };
    let windows = NSApplication::sharedApplication(mtm).windows();
    for window in windows.iter() {
        if window.isVisible() && window.title().to_string() == TITLE {
            return Reply::Ok(json!({"window_id": window.windowNumber()}));
        }
    }
    Reply::Err(DevError::new(code::NO_WINDOW, "no visible contr window"))
}

/// The window number is read on macOS only. Elsewhere the dev control cannot take a screenshot.
#[cfg(not(target_os = "macos"))]
pub fn current() -> Reply {
    Reply::Err(DevError::new(
        code::UNSUPPORTED,
        "the window number is read on macOS only",
    ))
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;

    #[cfg(target_os = "macos")]
    #[test]
    fn off_the_main_thread_there_is_no_window() {
        let reply = std::thread::spawn(current).join().unwrap();
        assert!(
            matches!(reply, Reply::Err(ref error) if error.code == code::NO_WINDOW),
            "{reply:?}"
        );
    }

    #[cfg(not(target_os = "macos"))]
    #[test]
    fn elsewhere_the_window_number_is_unsupported() {
        assert!(matches!(current(), Reply::Err(ref error) if error.code == code::UNSUPPORTED),);
    }
}
