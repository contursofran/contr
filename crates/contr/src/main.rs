//! contr: a native herdr client. Opens the empty v0.1 frame, or the status icon legend with `--icons`.

use contr::{
    board::Board, frame, herdr::Herdr, host::Host, legend, selection::Selection,
    status_message::StatusMessage, theme, visibility::Visibility,
};
use gpui::{
    App, Bounds, TitlebarOptions, WindowBounds, WindowOptions, point, prelude::*, px, size,
};
use std::borrow::Cow;
use tracing_subscriber::EnvFilter;

const LEGEND_WIDTH: f32 = 480.;
const LEGEND_HEIGHT: f32 = 420.;

static REGULAR: &[u8] = include_bytes!("../assets/fonts/JetBrainsMono-Regular.ttf");
static BOLD: &[u8] = include_bytes!("../assets/fonts/JetBrainsMono-Bold.ttf");

fn main() -> std::process::ExitCode {
    if is_statusline(std::env::args()) {
        return statusline(std::env::args());
    }
    // herdr connection logs (`contr_herdr`); RUST_LOG overrides the default.
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| "contr_herdr=info".into()),
        )
        .init();
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
        Selection::init(cx);
        let show_icons = wants_icons(std::env::args());
        let (width, height) = if show_icons {
            (LEGEND_WIDTH, LEGEND_HEIGHT)
        } else {
            (theme::WINDOW_WIDTH, theme::WINDOW_HEIGHT)
        };
        let bounds = Bounds::centered(None, size(px(width), px(height)), cx);
        let options = WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            window_min_size: (!show_icons).then(|| size(px(960.), px(600.))),
            titlebar: Some(titlebar_options()),
            app_id: Some("dev.contr".into()),
            ..Default::default()
        };
        let result = if show_icons {
            cx.open_window(options, |_, cx| cx.new(legend::Legend::new))
                .map(|_| ())
        } else {
            // The legend shows no host stats and no herdr, so only the frame samples the host and
            // follows herdr.
            Visibility::init(cx);
            Host::init(cx);
            Herdr::init(cx);
            Board::init(cx);
            StatusMessage::init(cx);
            cx.open_window(options, |window, cx| {
                Visibility::watch(window, cx);
                cx.new(frame::Frame::new)
            })
            .map(|_| ())
        };
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

/// True when `--icons` is among the arguments (the program name, first, is skipped).
fn wants_icons(args: impl IntoIterator<Item = String>) -> bool {
    args.into_iter().skip(1).any(|arg| arg == "--icons")
}

/// True when the first argument after the program name is `statusline`.
fn is_statusline(args: impl IntoIterator<Item = String>) -> bool {
    args.into_iter().nth(1).as_deref() == Some("statusline")
}

/// `contr statusline [-- <command> [args…]]`, the Claude Code status line. It
/// runs without GPUI and always exits 0, so the status line never breaks.
fn statusline(args: impl IntoIterator<Item = String>) -> std::process::ExitCode {
    let then = statusline_command(args);
    if let Err(error) = contr_usage::claude::run_statusline(
        std::io::stdin().lock(),
        std::io::stdout().lock(),
        &then,
    ) {
        eprintln!("contr statusline: {error}");
    }
    std::process::ExitCode::SUCCESS
}

/// The command to chain: the words after `--`. Without the separator, nothing.
fn statusline_command(args: impl IntoIterator<Item = String>) -> Vec<String> {
    let mut args = args.into_iter().skip(2);
    match args.next().as_deref() {
        Some("--") => args.collect(),
        _ => Vec::new(),
    }
}

/// On macOS the title bar is transparent and the traffic lights are moved
/// inside the 40px title bar. Other platforms keep their system title bar.
fn titlebar_options() -> TitlebarOptions {
    let macos = cfg!(target_os = "macos");
    TitlebarOptions {
        title: Some("contr".into()),
        appears_transparent: macos,
        traffic_light_position: macos.then(|| point(px(14.), px(13.))),
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;

    #[test]
    fn icons_flag_opens_the_legend() {
        assert!(wants_icons(["contr", "--icons"].map(String::from)));
        assert!(wants_icons(["contr", "--x", "--icons"].map(String::from)));
    }

    #[test]
    fn without_the_flag_the_frame_opens() {
        assert!(!wants_icons(["contr"].map(String::from)));
        assert!(!wants_icons(["contr", "--icon"].map(String::from)));
        // The program name is never a flag, even if it looks like one.
        assert!(!wants_icons(["--icons"].map(String::from)));
    }

    #[test]
    fn statusline_argument_selects_the_status_line() {
        assert!(is_statusline(["contr", "statusline"].map(String::from)));
        assert!(is_statusline(
            ["contr", "statusline", "--", "cat"].map(String::from)
        ));
        assert!(!is_statusline(["contr", "--icons"].map(String::from)));
        // The program name is never the subcommand.
        assert!(!is_statusline(["statusline"].map(String::from)));
    }

    #[test]
    fn statusline_runs_the_words_after_the_separator() {
        assert_eq!(
            statusline_command(["contr", "statusline", "--", "cat", "-n"].map(String::from)),
            ["cat", "-n"].map(String::from)
        );
    }

    #[test]
    fn statusline_without_a_separator_chains_nothing() {
        assert!(statusline_command(["contr", "statusline"].map(String::from)).is_empty());
        assert!(statusline_command(["contr", "statusline", "cat"].map(String::from)).is_empty());
    }

    #[test]
    fn fonts_are_truetype() {
        assert_eq!(&REGULAR[..4], &[0, 1, 0, 0]);
        assert_eq!(&BOLD[..4], &[0, 1, 0, 0]);
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn titlebar_uses_inset_traffic_lights_on_macos() {
        let options = titlebar_options();
        assert!(options.appears_transparent);
        assert_eq!(
            options.traffic_light_position,
            Some(point(px(14.), px(13.)))
        );
    }
}
