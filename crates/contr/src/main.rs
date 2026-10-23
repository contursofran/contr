//! contr: a native herdr client. Opens the empty v0.1 frame.

mod frame;
mod theme;

use gpui::{
    App, Bounds, TitlebarOptions, WindowBounds, WindowOptions, point, prelude::*, px, size,
};
use std::borrow::Cow;

static REGULAR: &[u8] = include_bytes!("../assets/fonts/JetBrainsMono-Regular.ttf");
static BOLD: &[u8] = include_bytes!("../assets/fonts/JetBrainsMono-Bold.ttf");

fn main() -> std::process::ExitCode {
    let failed = std::rc::Rc::new(std::cell::Cell::new(false));
    let open_failed = failed.clone();
    gpui_platform::application().run(move |cx: &mut App| {
        if let Err(error) = cx
            .text_system()
            .add_fonts(vec![Cow::Borrowed(REGULAR), Cow::Borrowed(BOLD)])
        {
            eprintln!("contr: unable to load JetBrains Mono: {error}"); // falls back to system font
        }
        cx.on_window_closed(|cx, _| {
            if cx.windows().is_empty() {
                cx.quit()
            }
        })
        .detach();
        let bounds = Bounds::centered(
            None,
            size(px(theme::WINDOW_WIDTH), px(theme::WINDOW_HEIGHT)),
            cx,
        );
        let result = cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                window_min_size: Some(size(px(960.), px(600.))),
                titlebar: Some(titlebar_options()),
                app_id: Some("dev.contr".into()),
                ..Default::default()
            },
            |_, cx| cx.new(|_| frame::Frame::new()),
        );
        if let Err(error) = result {
            eprintln!("contr: unable to open the window: {error}");
            open_failed.set(true);
            cx.quit();
        }
        cx.activate(true);
    });
    if failed.get() {
        std::process::ExitCode::FAILURE
    } else {
        std::process::ExitCode::SUCCESS
    }
}

// Adapted from herdr-gpui (https://github.com/penso/herdr-gpui),
// crates/herdr-gpui/src/titlebar.rs at 1ad3319e12c202e52dc3341e3dc1c9f49a064eb8.
// Copyright 2026 Fabien Penso. Licensed under the Apache License, Version 2.0.
/// Transparent title bar; the traffic lights sit centred in the 40px bar.
fn titlebar_options() -> TitlebarOptions {
    TitlebarOptions {
        title: Some("contr".into()),
        appears_transparent: cfg!(target_os = "macos"),
        traffic_light_position: cfg!(target_os = "macos").then(|| point(px(14.), px(13.))),
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;

    #[test]
    fn fonts_are_truetype() {
        assert_eq!(&REGULAR[..4], &[0, 1, 0, 0]);
        assert_eq!(&BOLD[..4], &[0, 1, 0, 0]);
    }

    #[test]
    fn titlebar_uses_inset_traffic_lights_on_macos() {
        let options = titlebar_options();
        if cfg!(target_os = "macos") {
            assert!(options.appears_transparent);
            assert_eq!(
                options.traffic_light_position,
                Some(point(px(14.), px(13.)))
            );
        } else {
            assert!(!options.appears_transparent);
            assert_eq!(options.traffic_light_position, None);
        }
    }
}
