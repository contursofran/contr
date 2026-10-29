package terminal

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// press runs KeyEvent and returns the event, or nil when there is none.
func press(key ui.Key, m ui.Modifiers) *endpoint.KeyEvent {
	ev, ok := KeyEvent(key, m)
	if !ok {
		return nil
	}
	return &ev
}

func want(t *testing.T, got *endpoint.KeyEvent, code endpoint.KeyCode, mods uint8) {
	t.Helper()
	if got == nil {
		t.Fatalf("got no event, want %+v mods %d", code, mods)
	}
	if got.Code != code || got.Mods != mods || got.Repeat {
		t.Fatalf("got %+v, want code %+v mods %d", *got, code, mods)
	}
}

func none(t *testing.T, got *endpoint.KeyEvent) {
	t.Helper()
	if got != nil {
		t.Fatalf("got %+v, want no event", *got)
	}
}

func kind(k endpoint.KeyKind) endpoint.KeyCode { return endpoint.KeyCode{Kind: k} }

func TestCtrlLettersAreControlCodes(t *testing.T) {
	want(t, press(ui.KeyC, ui.Ctrl), endpoint.CharKey('c'), endpoint.ModControl)
	want(t, press(ui.KeyC, ui.Ctrl|ui.Shift), endpoint.CharKey('c'), endpoint.ModControl|endpoint.ModShift)
}

func TestCtrlSpaceAndTheCtrlPunctuationAreControlCodes(t *testing.T) {
	want(t, press(ui.KeySpace, ui.Ctrl), endpoint.CharKey(' '), endpoint.ModControl)
	want(t, press(ui.KeyBracketLeft, ui.Ctrl), endpoint.CharKey('['), endpoint.ModControl)
	want(t, press(ui.KeyBackslash, ui.Ctrl), endpoint.CharKey('\\'), endpoint.ModControl)
	want(t, press(ui.KeyBracketRight, ui.Ctrl), endpoint.CharKey(']'), endpoint.ModControl)
	// Ctrl-@ is Ctrl-Shift-2 on a US layout.
	want(t, press(ui.Key2, ui.Ctrl|ui.Shift), endpoint.CharKey('@'), endpoint.ModControl)
	none(t, press(ui.Key1, ui.Ctrl))
}

func TestCtrlShiftSixAndMinusSendCtrlCaretAndUnderscore(t *testing.T) {
	want(t, press(ui.Key6, ui.Ctrl|ui.Shift), endpoint.CharKey('^'), endpoint.ModControl)
	want(t, press(ui.KeyMinus, ui.Ctrl|ui.Shift), endpoint.CharKey('_'), endpoint.ModControl)
}

func TestCtrlSixAndMinusWithoutShiftAreNotKeys(t *testing.T) {
	none(t, press(ui.Key6, ui.Ctrl))
	none(t, press(ui.KeyMinus, ui.Ctrl))
}

func TestNamedKeysKeepTheirModifiers(t *testing.T) {
	want(t, press(ui.KeyEnter, 0), kind(endpoint.KeyEnter), 0)
	want(t, press(ui.KeyEnter, ui.Shift), kind(endpoint.KeyEnter), endpoint.ModShift)
	want(t, press(ui.KeyEscape, 0), kind(endpoint.KeyEsc), 0)
	want(t, press(ui.KeyUp, 0), kind(endpoint.KeyUp), 0)
	want(t, press(ui.KeyTab, 0), kind(endpoint.KeyTab), 0)
	want(t, press(ui.KeyTab, ui.Shift), kind(endpoint.KeyBackTab), endpoint.ModShift)
	want(t, press(ui.KeyBackspace, ui.Alt), kind(endpoint.KeyBackspace), endpoint.ModAlt)
	want(t, press(ui.KeyLeft, ui.Ctrl), kind(endpoint.KeyLeft), endpoint.ModControl)
	want(t, press(ui.KeyPageDown, 0), kind(endpoint.KeyPageDown), 0)
}

// MyGo has F1 to F12 only, so F13 to F20 and the out-of-range cases of the
// Rust test cannot be written. Here F1, F12 and a non-function key are checked.
func TestFunctionKeysRunFromOneToTwelve(t *testing.T) {
	want(t, press(ui.KeyF1, 0), endpoint.FKey(1), 0)
	want(t, press(ui.KeyF12, 0), endpoint.FKey(12), 0)
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
