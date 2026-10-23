//! Keystrokes to herdr's input events. Printable text is not a key here: the IME delivers it as
//! text (`EntityInputHandler`), so Option types characters (D11) and dead keys work. Named keys,
//! function keys and Ctrl chords become `InputEvent::Key`.

use contr_herdr::endpoint::messages::{InputEvent, KeyCode, mods};
use gpui::Keystroke;

/// The herdr event for `k`, or `None` when the keystroke is text or belongs to the app. Cmd chords
/// are the app's (Cmd-V is handled by the view). `repeat` is always false: the view does not
/// send repeat events yet.
pub fn key_event(k: &Keystroke) -> Option<InputEvent> {
    let m = &k.modifiers;
    if m.platform {
        return None;
    }
    let mut flags = 0;
    if m.shift {
        flags |= mods::SHIFT;
    }
    if m.control {
        flags |= mods::CONTROL;
    }
    if m.alt {
        flags |= mods::ALT;
    }
    let key = k.key.as_str();
    if let Some(code) = named(key, m.shift) {
        return Some(InputEvent::Key {
            code,
            mods: flags,
            repeat: false,
        });
    }
    let c = match key {
        "space" => ' ',
        _ => single_char(key)?,
    };
    // Option is not Alt for characters (D11), so `alt` with a character is text. Only Ctrl makes
    // a key out of a character: the control codes of the letters and of `[ \ ] ^ _ @ space`.
    if m.alt || !m.control {
        return None;
    }
    // On a US layout Ctrl-^ and Ctrl-_ are typed as Ctrl-Shift-6 and Ctrl-Shift-minus. The key is
    // then the unshifted `6` or `-`. The shift is in the character, so the key carries Ctrl only.
    if m.shift
        && let Some(punct) = shifted_control_char(c)
    {
        return Some(InputEvent::Key {
            code: KeyCode::Char(punct),
            mods: mods::CONTROL,
            repeat: false,
        });
    }
    let control_code =
        c.is_ascii_alphabetic() || matches!(c, '[' | '\\' | ']' | '^' | '_' | '@' | ' ');
    control_code.then(|| InputEvent::Key {
        code: KeyCode::Char(c.to_ascii_lowercase()),
        mods: flags,
        repeat: false,
    })
}

/// The key for a named key. `shift` makes Tab a Back-Tab.
fn named(key: &str, shift: bool) -> Option<KeyCode> {
    let code = match key {
        "enter" => KeyCode::Enter,
        "backspace" => KeyCode::Backspace,
        "delete" => KeyCode::Delete,
        "escape" => KeyCode::Esc,
        "tab" if shift => KeyCode::BackTab,
        "tab" => KeyCode::Tab,
        "left" => KeyCode::Left,
        "right" => KeyCode::Right,
        "up" => KeyCode::Up,
        "down" => KeyCode::Down,
        "home" => KeyCode::Home,
        "end" => KeyCode::End,
        "pageup" => KeyCode::PageUp,
        "pagedown" => KeyCode::PageDown,
        "insert" => KeyCode::Insert,
        _ => return function_key(key),
    };
    Some(code)
}

/// `f1` to `f20`. Other names are not keys.
fn function_key(key: &str) -> Option<KeyCode> {
    let n: u8 = key.strip_prefix('f')?.parse().ok()?;
    (1..=20).contains(&n).then_some(KeyCode::F(n))
}

/// The character that Ctrl and Shift type with the unshifted key `c`: `6` gives `^` (Ctrl-^) and `-`
/// gives `_` (Ctrl-_). Other keys give `None`.
fn shifted_control_char(c: char) -> Option<char> {
    match c {
        '6' => Some('^'),
        '-' => Some('_'),
        _ => None,
    }
}

/// The only character of `key`, or `None` when the name has more than one.
fn single_char(key: &str) -> Option<char> {
    let mut chars = key.chars();
    match (chars.next(), chars.next()) {
        (Some(c), None) => Some(c),
        _ => None,
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;

    fn key(text: &str) -> Option<InputEvent> {
        key_event(&Keystroke::parse(text).unwrap())
    }

    fn press(code: KeyCode, flags: u8) -> Option<InputEvent> {
        Some(InputEvent::Key {
            code,
            mods: flags,
            repeat: false,
        })
    }

    #[test]
    fn ctrl_letters_are_control_codes() {
        assert_eq!(key("ctrl-c"), press(KeyCode::Char('c'), mods::CONTROL));
        assert_eq!(
            key("ctrl-shift-c"),
            press(KeyCode::Char('c'), mods::CONTROL | mods::SHIFT)
        );
    }

    #[test]
    fn ctrl_space_and_the_ctrl_punctuation_are_control_codes() {
        assert_eq!(key("ctrl-space"), press(KeyCode::Char(' '), mods::CONTROL));
        assert_eq!(key("ctrl-["), press(KeyCode::Char('['), mods::CONTROL));
        assert_eq!(key("ctrl-@"), press(KeyCode::Char('@'), mods::CONTROL));
        assert_eq!(key("ctrl-1"), None);
    }

    #[test]
    fn ctrl_shift_six_and_minus_send_ctrl_caret_and_underscore() {
        // On a US layout the key is the unshifted `6` or `-`, with Shift held.
        assert_eq!(
            key("ctrl-shift-6"),
            press(KeyCode::Char('^'), mods::CONTROL)
        );
        assert_eq!(
            key("ctrl-shift--"),
            press(KeyCode::Char('_'), mods::CONTROL)
        );
        // They send what Ctrl-^ and Ctrl-_ send, named by the character.
        assert_eq!(key("ctrl-^"), press(KeyCode::Char('^'), mods::CONTROL));
        assert_eq!(key("ctrl-_"), press(KeyCode::Char('_'), mods::CONTROL));
        assert_eq!(key("ctrl-shift-6"), key("ctrl-^"));
        assert_eq!(key("ctrl-shift--"), key("ctrl-_"));
    }

    #[test]
    fn ctrl_six_and_minus_without_shift_are_not_keys() {
        assert_eq!(key("ctrl-6"), None);
        assert_eq!(key("ctrl--"), None);
    }

    #[test]
    fn named_keys_keep_their_modifiers() {
        assert_eq!(key("enter"), press(KeyCode::Enter, 0));
        assert_eq!(key("shift-enter"), press(KeyCode::Enter, mods::SHIFT));
        assert_eq!(key("escape"), press(KeyCode::Esc, 0));
        assert_eq!(key("up"), press(KeyCode::Up, 0));
        assert_eq!(key("tab"), press(KeyCode::Tab, 0));
        assert_eq!(key("shift-tab"), press(KeyCode::BackTab, mods::SHIFT));
        assert_eq!(key("alt-backspace"), press(KeyCode::Backspace, mods::ALT));
        assert_eq!(key("ctrl-left"), press(KeyCode::Left, mods::CONTROL));
        assert_eq!(key("pagedown"), press(KeyCode::PageDown, 0));
    }

    #[test]
    fn function_keys_run_from_one_to_twenty() {
        assert_eq!(key("f1"), press(KeyCode::F(1), 0));
        assert_eq!(key("f20"), press(KeyCode::F(20), 0));
        assert_eq!(key("f21"), None);
        assert_eq!(key("f0"), None);
    }

    #[test]
    fn plain_text_is_not_a_key() {
        assert_eq!(key("a"), None);
        assert_eq!(key("shift-a"), None);
        assert_eq!(key("1"), None);
        assert_eq!(key("space"), None);
        assert_eq!(
            key_event(&Keystroke {
                modifiers: Default::default(),
                key: "é".into(),
                key_char: Some("é".into())
            }),
            None
        );
    }

    #[test]
    fn option_types_characters_so_alt_with_a_character_is_text() {
        assert_eq!(key("alt-e"), None);
        assert_eq!(key("alt-space"), None);
        assert_eq!(key("ctrl-alt-c"), None);
    }

    #[test]
    fn cmd_chords_are_left_to_the_app() {
        assert_eq!(key("cmd-c"), None);
        assert_eq!(key("cmd-v"), None);
        assert_eq!(key("cmd-enter"), None);
    }
}
