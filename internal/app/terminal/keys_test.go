package terminal

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/term"
)

// press runs KeyEvent and returns the event, or nil when there is none.
func press(key ui.Key, m ui.Modifiers) *term.KeyEvent {
	ev, ok := KeyEvent(key, m)
	if !ok {
		return nil
	}
	return &ev
}

func want(t *testing.T, got *term.KeyEvent, code term.KeyCode, mods uint8) {
	t.Helper()
	if got == nil {
		t.Fatalf("got no event, want %+v mods %d", code, mods)
	}
	if got.Code != code || got.Mods != mods || got.Repeat {
		t.Fatalf("got %+v, want code %+v mods %d", *got, code, mods)
	}
}

func none(t *testing.T, got *term.KeyEvent) {
	t.Helper()
	if got != nil {
		t.Fatalf("got %+v, want no event", *got)
	}
}

func kind(k term.KeyKind) term.KeyCode { return term.KeyCode{Kind: k} }

func TestCtrlLettersAreControlCodes(t *testing.T) {
	want(t, press(ui.KeyC, ui.Ctrl), term.CharKey('c'), term.ModControl)
	want(t, press(ui.KeyC, ui.Ctrl|ui.Shift), term.CharKey('c'), term.ModControl|term.ModShift)
}

func TestCtrlSpaceAndTheCtrlPunctuationAreControlCodes(t *testing.T) {
	want(t, press(ui.KeySpace, ui.Ctrl), term.CharKey(' '), term.ModControl)
	want(t, press(ui.KeyBracketLeft, ui.Ctrl), term.CharKey('['), term.ModControl)
	want(t, press(ui.KeyBackslash, ui.Ctrl), term.CharKey('\\'), term.ModControl)
	want(t, press(ui.KeyBracketRight, ui.Ctrl), term.CharKey(']'), term.ModControl)
	// Ctrl-@ is Ctrl-Shift-2 on a US layout.
	want(t, press(ui.Key2, ui.Ctrl|ui.Shift), term.CharKey('@'), term.ModControl)
	none(t, press(ui.Key1, ui.Ctrl))
}

func TestCtrlShiftSixAndMinusSendCtrlCaretAndUnderscore(t *testing.T) {
	want(t, press(ui.Key6, ui.Ctrl|ui.Shift), term.CharKey('^'), term.ModControl)
	want(t, press(ui.KeyMinus, ui.Ctrl|ui.Shift), term.CharKey('_'), term.ModControl)
}

func TestCtrlSixAndMinusWithoutShiftAreNotKeys(t *testing.T) {
	none(t, press(ui.Key6, ui.Ctrl))
	none(t, press(ui.KeyMinus, ui.Ctrl))
}

func TestNamedKeysKeepTheirModifiers(t *testing.T) {
	want(t, press(ui.KeyEnter, 0), kind(term.KeyEnter), 0)
	want(t, press(ui.KeyEnter, ui.Shift), kind(term.KeyEnter), term.ModShift)
	want(t, press(ui.KeyEscape, 0), kind(term.KeyEsc), 0)
	want(t, press(ui.KeyUp, 0), kind(term.KeyUp), 0)
	want(t, press(ui.KeyTab, 0), kind(term.KeyTab), 0)
	want(t, press(ui.KeyTab, ui.Shift), kind(term.KeyBackTab), term.ModShift)
	want(t, press(ui.KeyBackspace, ui.Alt), kind(term.KeyBackspace), term.ModAlt)
	want(t, press(ui.KeyLeft, ui.Ctrl), kind(term.KeyLeft), term.ModControl)
	want(t, press(ui.KeyPageDown, 0), kind(term.KeyPageDown), 0)
}

// MyGo has F1 to F12 only, so F13 to F20 and the out-of-range cases of the
// Rust test cannot be written. Here F1, F12 and a non-function key are checked.
func TestFunctionKeysRunFromOneToTwelve(t *testing.T) {
	want(t, press(ui.KeyF1, 0), term.FKey(1), 0)
	want(t, press(ui.KeyF12, 0), term.FKey(12), 0)
	none(t, press(ui.KeyUnknown, 0))
}

func TestPlainTextIsNotAKey(t *testing.T) {
	none(t, press(ui.KeyA, 0))
	none(t, press(ui.KeyA, ui.Shift))
	none(t, press(ui.Key1, 0))
	none(t, press(ui.KeySpace, 0))
	none(t, press(ui.KeyUnknown, 0))
}

func TestOptionTypesCharactersSoAltWithACharacterIsText(t *testing.T) {
	none(t, press(ui.KeyE, ui.Alt))
	none(t, press(ui.KeySpace, ui.Alt))
	none(t, press(ui.KeyC, ui.Ctrl|ui.Alt))
}

func TestCmdChordsAreLeftToTheApp(t *testing.T) {
	none(t, press(ui.KeyC, ui.Super))
	none(t, press(ui.KeyV, ui.Super))
	none(t, press(ui.KeyEnter, ui.Super))
}
