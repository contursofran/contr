//! contr: a native herdr client. Opens the empty v0.1 frame, or the status icon legend with `--icons`.

use contr::{
    board::Board, board_outbox::BoardOutbox, frame, herdr::Herdr, host::Host, legend,
    selection::Selection, status_message::StatusMessage, theme, usage::Usage,
    visibility::Visibility,
};
use gpui::{
    App, Bounds, TitlebarOptions, WindowBounds, WindowOptions, point, prelude::*, px, size,
};
use std::borrow::Cow;
use std::io::{self, Write};
use tracing_subscriber::EnvFilter;

const LEGEND_WIDTH: f32 = 480.;
const LEGEND_HEIGHT: f32 = 420.;

/// The usage text. It lists every form that the command line accepts.
const USAGE: &str = "Usage:
  contr                                     open the window
  contr --icons                             open the window with the status icon legend
  contr statusline [-- <command> [args…]]   print the Claude Code status line
  contr --help, -h                          print this text
";

static REGULAR: &[u8] = include_bytes!("../assets/fonts/JetBrainsMono-Regular.ttf");
static BOLD: &[u8] = include_bytes!("../assets/fonts/JetBrainsMono-Bold.ttf");

/// What the command line asks for. Only the forms in `USAGE` are accepted.
#[derive(Debug, PartialEq, Eq)]
enum Command {
    /// Open the window. `icons` selects the status icon legend.
    Window { icons: bool },
    /// Print the usage. `--help` or `-h`.
    Help,
    /// `statusline`. `then` is the command after `--`, if any.
    Statusline { then: Vec<String> },
    /// An argument that is not part of the command line.
    Unknown(String),
}

fn main() -> std::process::ExitCode {
    match parse_args(std::env::args()) {
        Command::Window { icons } => open_window(icons),
        Command::Help => print_usage(&mut io::stdout().lock()),
        Command::Unknown(arg) => reject_argument(&arg, &mut io::stderr().lock()),
        Command::Statusline { then } => statusline(&then),
    }
}

/// Reads the arguments after the program name. `statusline` must be the first
/// argument. Otherwise the arguments are read in order: the first `--help` or
/// `-h` asks for the usage, and the first argument other than `--icons` is unknown.
fn parse_args(args: impl IntoIterator<Item = String>) -> Command {
    let mut args = args.into_iter().skip(1).peekable();
    if args.peek().is_some_and(|arg| arg == "statusline") {
        args.next();
        return Command::Statusline {
            then: statusline_command(args),
        };
    }
    let mut icons = false;
    for arg in args {
        match arg.as_str() {
            "--icons" => icons = true,
            "--help" | "-h" => return Command::Help,
            // macOS adds a process serial number when an app starts from Finder or the Dock.
            serial if serial.starts_with("-psn_") => {}
            _ => return Command::Unknown(arg),
        }
    }
    Command::Window { icons }
}

/// The command to chain: the words after `--`, which must come right after
/// `statusline`. Without the separator, nothing is chained. The other words are
/// not checked, so the status line never fails on them.
fn statusline_command(mut args: impl Iterator<Item = String>) -> Vec<String> {
    match args.next().as_deref() {
        Some("--") => args.collect(),
        _ => Vec::new(),
    }
}

/// `contr statusline [-- <command> [args…]]`, the Claude Code status line. It
/// runs without GPUI and always exits 0, so the status line never breaks.
fn statusline(then: &[String]) -> std::process::ExitCode {
    if let Err(error) =
        contr_usage::claude::run_statusline(io::stdin().lock(), io::stdout().lock(), then)
    {
        eprintln!("contr statusline: {error}");
    }
    std::process::ExitCode::SUCCESS
}

/// Writes the usage text. After an unknown argument, the error line comes first.
fn write_usage(out: &mut impl Write, unknown: Option<&str>) -> io::Result<()> {
    if let Some(arg) = unknown {
        writeln!(out, "contr: unknown argument {arg}")?;
    }
    out.write_all(USAGE.as_bytes())
}

/// `--help`: the usage on stdout. Exits 1 when stdout cannot be written.
fn print_usage(out: &mut impl Write) -> std::process::ExitCode {
    match write_usage(out, None) {
        Ok(()) => std::process::ExitCode::SUCCESS,
        Err(_) => std::process::ExitCode::FAILURE,
    }
}

/// An unknown argument: the error line and the usage on stderr. Exits 2.
fn reject_argument(arg: &str, err: &mut impl Write) -> std::process::ExitCode {
    // With stderr closed there is nowhere else to report. The status is 2 either way.
    let _ = write_usage(err, Some(arg));
    std::process::ExitCode::from(2)
}

/// Opens the frame, or the legend with `--icons`, and runs GPUI until the last
/// window closes.
fn open_window(icons: bool) -> std::process::ExitCode {
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
        let (width, height) = if icons {
            (LEGEND_WIDTH, LEGEND_HEIGHT)
        } else {
            (theme::WINDOW_WIDTH, theme::WINDOW_HEIGHT)
        };
        let bounds = Bounds::centered(None, size(px(width), px(height)), cx);
        let options = WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            window_min_size: (!icons).then(|| size(px(960.), px(600.))),
            titlebar: Some(titlebar_options()),
            app_id: Some("dev.contr".into()),
            ..Default::default()
        };
        let result = if icons {
            cx.open_window(options, |_, cx| cx.new(legend::Legend::new))
                .map(|_| ())
        } else {
            // The legend shows no host stats and no herdr, so only the frame samples the host and
            // follows herdr.
            Visibility::init(cx);
            Host::init(cx);
            Herdr::init(cx);
            Board::init(cx);
            BoardOutbox::init(cx);
            StatusMessage::init(cx);
            Usage::init(cx);
            cx.open_window(options, |window, cx| {
                Visibility::watch(window, cx);
                cx.new(|cx| frame::Frame::new(window, cx))
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

    /// A writer that fails on every write, as a closed pipe does.
    struct Closed;

    impl Write for Closed {
        fn write(&mut self, _: &[u8]) -> io::Result<usize> {
            Err(io::Error::other("closed"))
        }

        fn flush(&mut self) -> io::Result<()> {
            Ok(())
        }
    }

    fn parse(args: &[&str]) -> Command {
        parse_args(args.iter().map(|arg| arg.to_string()))
    }

    #[test]
    fn no_argument_opens_the_window() {
        assert_eq!(parse(&["contr"]), Command::Window { icons: false });
        // The program name is never an argument, even if it looks like one.
        assert_eq!(parse(&["--icons"]), Command::Window { icons: false });
        assert_eq!(parse(&["statusline"]), Command::Window { icons: false });
    }

    #[test]
    fn icons_flag_opens_the_legend() {
        assert_eq!(
            parse(&["contr", "--icons"]),
            Command::Window { icons: true }
        );
    }

    #[test]
    fn help_flags_ask_for_the_usage() {
        assert_eq!(parse(&["contr", "--help"]), Command::Help);
        assert_eq!(parse(&["contr", "-h"]), Command::Help);
        assert_eq!(parse(&["contr", "--icons", "--help"]), Command::Help);
    }

    #[test]
    fn the_first_problem_decides_between_help_and_an_error() {
        assert_eq!(parse(&["contr", "--help", "--bogus"]), Command::Help);
        assert_eq!(
            parse(&["contr", "--bogus", "--help"]),
            Command::Unknown("--bogus".into())
        );
    }

    #[test]
    fn other_arguments_are_unknown() {
        assert_eq!(
            parse(&["contr", "--icon"]),
            Command::Unknown("--icon".into())
        );
        assert_eq!(
            parse(&["contr", "--icons", "--x"]),
            Command::Unknown("--x".into())
        );
        assert_eq!(parse(&["contr", "extra"]), Command::Unknown("extra".into()));
        // `statusline` is a subcommand only as the first argument.
        assert_eq!(
            parse(&["contr", "--icons", "statusline"]),
            Command::Unknown("statusline".into())
        );
    }

    #[test]
    fn finder_process_serial_number_is_ignored() {
        // macOS passes `-psn_<n>_<n>` when an app starts from Finder or the Dock.
        assert_eq!(
            parse(&["contr", "-psn_0_12345"]),
            Command::Window { icons: false }
        );
        assert_eq!(
            parse(&["contr", "-psn_0_12345", "--icons"]),
            Command::Window { icons: true }
        );
        assert_eq!(parse(&["contr", "-psn_0_12345", "--help"]), Command::Help);
        // Only the `-psn_` prefix is ignored. Other dash arguments stay unknown.
        assert_eq!(parse(&["contr", "-psn"]), Command::Unknown("-psn".into()));
    }

    #[test]
    fn statusline_argument_selects_the_status_line() {
        assert_eq!(
            parse(&["contr", "statusline"]),
            Command::Statusline { then: Vec::new() }
        );
        assert_eq!(
            parse(&["contr", "statusline", "--", "cat", "-n"]),
            Command::Statusline {
                then: vec!["cat".into(), "-n".into()]
            }
        );
    }

    #[test]
    fn statusline_without_a_separator_chains_nothing() {
        assert_eq!(
            parse(&["contr", "statusline", "cat"]),
            Command::Statusline { then: Vec::new() }
        );
        // A separator that is not right after `statusline` does not start a chain.
        assert_eq!(
            parse(&["contr", "statusline", "cat", "--", "x"]),
            Command::Statusline { then: Vec::new() }
        );
    }

    #[test]
    fn statusline_never_turns_its_words_into_help_or_an_error() {
        // The status line must not fail on its input, so these words are not checked.
        assert_eq!(
            parse(&["contr", "statusline", "--help"]),
            Command::Statusline { then: Vec::new() }
        );
        assert_eq!(
            parse(&["contr", "statusline", "--bogus"]),
            Command::Statusline { then: Vec::new() }
        );
    }

    #[test]
    fn usage_lists_every_form() {
        let mut text = Vec::new();
        write_usage(&mut text, None).unwrap();
        let text = String::from_utf8(text).unwrap();
        assert!(text.starts_with("Usage:\n"));
        for form in [
            "contr --icons",
            "contr statusline [-- <command> [args…]]",
            "contr --help, -h",
        ] {
            assert!(text.contains(form), "usage lacks {form}");
        }
    }

    #[test]
    fn help_prints_the_usage_and_exits_zero() {
        let mut out = Vec::new();
        assert_eq!(print_usage(&mut out), std::process::ExitCode::SUCCESS);
        assert_eq!(out, USAGE.as_bytes());
    }

    #[test]
    fn help_exits_one_when_stdout_is_closed() {
        assert_eq!(print_usage(&mut Closed), std::process::ExitCode::FAILURE);
    }

    #[test]
    fn unknown_argument_prints_the_error_and_the_usage_and_exits_two() {
        let mut err = Vec::new();
        assert_eq!(
            reject_argument("--bogus", &mut err),
            std::process::ExitCode::from(2)
        );
        assert_eq!(
            String::from_utf8(err).unwrap(),
            format!("contr: unknown argument --bogus\n{USAGE}")
        );
    }

    #[test]
    fn unknown_argument_still_exits_two_when_stderr_is_closed() {
        assert_eq!(
            reject_argument("--bogus", &mut Closed),
            std::process::ExitCode::from(2)
        );
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
