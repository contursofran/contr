//! contr: a native herdr client. Opens the empty v0.1 frame, or the status icon legend with `--icons`.

#[cfg(debug_assertions)]
use contr::dev_control;
use contr::{
    board::Board, board_outbox::BoardOutbox, board_panel::BoardPanel, dev_client, dev_protocol,
    frame, herdr::Herdr, host::Host, legend, selection::Selection, status_message::StatusMessage,
    theme, usage::Usage, visibility::Visibility,
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
  contr                                                   open the window
  contr --icons                                           open the window with the status icon legend
  contr statusline [-- <command> [args…]]                 print the Claude Code status line
  contr dev <state|click|key|select|resize|window-id> …   drive a dev window (debug builds)
  contr --help, -h                                        print this text
";

static REGULAR: &[u8] = include_bytes!("../assets/fonts/JetBrainsMono-Regular.ttf");
static BOLD: &[u8] = include_bytes!("../assets/fonts/JetBrainsMono-Bold.ttf");
static ITALIC: &[u8] = include_bytes!("../assets/fonts/JetBrainsMono-Italic.ttf");
static BOLD_ITALIC: &[u8] = include_bytes!("../assets/fonts/JetBrainsMono-BoldItalic.ttf");

/// The bundled faces of JetBrains Mono v2.304. They register with the family `theme::FONT_FAMILY`,
/// so `FontStyle::Italic` picks the italic faces and bold takes the bold ones.
fn bundled_fonts() -> Vec<Cow<'static, [u8]>> {
    vec![
        Cow::Borrowed(REGULAR),
        Cow::Borrowed(BOLD),
        Cow::Borrowed(ITALIC),
        Cow::Borrowed(BOLD_ITALIC),
    ]
}

/// What the command line asks for. Only the forms in `USAGE` are accepted.
#[derive(Debug, PartialEq, Eq)]
enum Command {
    /// Open the window. `icons` selects the status icon legend.
    Window { icons: bool },
    /// Print the usage. `--help` or `-h`.
    Help,
    /// `statusline`. `then` is the command after `--`, if any.
    Statusline { then: Vec<String> },
    /// `dev`. The words after `dev` are read by `dev_client`.
    Dev(Vec<String>),
    /// An argument that is not part of the command line.
    Unknown(String),
}

fn main() -> std::process::ExitCode {
    match parse_args(std::env::args()) {
        Command::Window { icons } => open_window(icons),
        Command::Help => print_usage(&mut io::stdout().lock()),
        Command::Unknown(arg) => reject_argument(&arg, &mut io::stderr().lock()),
        Command::Statusline { then } => statusline(&then),
        Command::Dev(words) => dev_client::run(
            &words,
            std::env::var_os(dev_protocol::ENV),
            &mut io::stdout().lock(),
            &mut io::stderr().lock(),
        ),
    }
}

/// Reads the arguments after the program name. `statusline` or `dev` must be the first
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
    if args.peek().is_some_and(|arg| arg == "dev") {
        args.next();
        return Command::Dev(args.collect());
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
        if let Err(error) = cx.text_system().add_fonts(bundled_fonts()) {
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
            BoardPanel::bind_keys(cx);
            StatusMessage::init(cx);
            Usage::init(cx);
            let frame = cx.open_window(options, |window, cx| {
                Visibility::watch(window, cx);
                cx.new(|cx| frame::Frame::new(window, cx))
            });
            #[cfg(debug_assertions)]
            if let Ok(handle) = &frame {
                dev_control::start_from_env(*handle, cx);
            }
            frame.map(|_| ())
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
    fn dev_argument_passes_the_words_on() {
        assert_eq!(
            parse(&["contr", "dev", "state"]),
            Command::Dev(vec!["state".into()])
        );
        assert_eq!(parse(&["contr", "dev"]), Command::Dev(Vec::new()));
        // The words after `dev` are not read as flags, so `--help` reaches `dev_client`.
        assert_eq!(
            parse(&["contr", "dev", "click", "a b", "--help"]),
            Command::Dev(vec!["click".into(), "a b".into(), "--help".into()])
        );
    }

    #[test]
    fn dev_after_another_argument_is_unknown() {
        assert_eq!(
            parse(&["contr", "--icons", "dev"]),
            Command::Unknown("dev".into())
        );
        assert_eq!(
            parse(&["contr", "extra", "dev", "state"]),
            Command::Unknown("extra".into())
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
            "contr dev <state|click|key|select|resize|window-id> …",
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

    /// The table of `tag` in a TrueType face, or None when the directory or the table is cut short.
    fn table<'a>(face: &'a [u8], tag: &[u8]) -> Option<&'a [u8]> {
        let count = usize::from(u16::from_be_bytes(face.get(4..6)?.try_into().ok()?));
        (0..count).find_map(|i| {
            let record = face.get(12 + 16 * i..28 + 16 * i)?;
            if &record[..4] != tag {
                return None;
            }
            let offset = u32::from_be_bytes(record[8..12].try_into().ok()?) as usize;
            let length = u32::from_be_bytes(record[12..16].try_into().ok()?) as usize;
            face.get(offset..offset + length)
        })
    }

    /// The family name (name ID 1, Windows) and the italic bit (OS/2 `fsSelection`) of a face. None
    /// when a table or the name is missing.
    fn family_and_italic(face: &[u8]) -> Option<(String, bool)> {
        let os2 = table(face, b"OS/2")?;
        let italic = u16::from_be_bytes(os2.get(62..64)?.try_into().ok()?) & 1 != 0;
        let name = table(face, b"name")?;
        let count = usize::from(u16::from_be_bytes(name.get(2..4)?.try_into().ok()?));
        let storage = usize::from(u16::from_be_bytes(name.get(4..6)?.try_into().ok()?));
        let family = (0..count).find_map(|i| {
            let record = name.get(6 + 12 * i..18 + 12 * i)?;
            let field = |at: usize| u16::from_be_bytes([record[at], record[at + 1]]);
            let (platform, id) = (field(0), field(6));
            if platform != 3 || id != 1 {
                return None;
            }
            let start = storage + usize::from(field(10));
            let bytes = name.get(start..start + usize::from(field(8)))?;
            let units: Vec<u16> = bytes
                .chunks_exact(2)
                .map(|pair| u16::from_be_bytes([pair[0], pair[1]]))
                .collect();
            String::from_utf16(&units).ok()
        })?;
        Some((family, italic))
    }

    #[test]
    fn fonts_are_truetype() {
        for face in bundled_fonts() {
            assert_eq!(&face[..4], &[0, 1, 0, 0]);
        }
        assert_eq!(
            bundled_fonts().len(),
            4,
            "regular, bold, italic, bold italic"
        );
    }

    #[test]
    fn the_italic_faces_are_the_terminal_family_and_italic() {
        // GPUI matches the faces it loaded by family name and style. These are the same fields.
        let faces = [
            (REGULAR, false),
            (BOLD, false),
            (ITALIC, true),
            (BOLD_ITALIC, true),
        ];
        for (face, italic) in faces {
            assert_eq!(
                family_and_italic(face),
                Some((theme::FONT_FAMILY.to_owned(), italic))
            );
        }
    }

    #[test]
    fn a_face_cut_short_has_no_family() {
        assert_eq!(family_and_italic(&ITALIC[..12]), None);
        assert_eq!(family_and_italic(&[]), None);
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
