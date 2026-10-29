// Written from herdr's wire layout; see NOTICE.

package endpoint

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var (
	testSize = GridSize{Cols: 80, Rows: 24}
	testCell = CellPx{Width: 8, Height: 16}
)

func welcomeJSON(extra string) string {
	return `{"generation":1,"server_version":"0.9.3","snapshot_codec":"shell.snapshot.v1","surface_codec":"shell.surface.v1","input_codec":"shell.input.semantic.v1","blob_codec":"shell.blob.v1"` + extra + `}`
}

func parseWelcome(t *testing.T, s string) *Welcome {
	t.Helper()
	var w Welcome
	if err := json.Unmarshal([]byte(s), &w); err != nil {
		t.Fatal(err)
	}
	return &w
}

func wantBytes(t *testing.T, got []byte, want ...byte) {
	t.Helper()
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func str(s string) *string { return &s }

// serverPayload builds a server payload with the encoder, as herdr would send it.
func serverPayload(build func(e *Enc)) []byte {
	var e Enc
	build(&e)
	return e.Buf
}

func wantServer(t *testing.T, payload []byte, want ServerMsg) {
	t.Helper()
	got, err := DecodeServer(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestResizeGoldenBytes(t *testing.T) {
	wantBytes(t, ResizeMsg{Size: testSize, Cell: testCell}.Encode(), 12, 8, 16, 80, 24, 0)
}

func TestInputGoldenBytes(t *testing.T) {
	msg := InputMsg{PaneID: "w1:p1", Events: []InputEvent{
		TextEvent{"ls"},
		KeyEvent{Code: KeyCode{Kind: KeyEnter}},
		KeyEvent{Code: CharKey('c'), Mods: ModControl},
	}}
	wantBytes(t, msg.Encode(),
		13, 5, 'w', '1', ':', 'p', '1',
		3,
		1, 2, 'l', 's',
		0, 1, 0, 0, 1, 0, 0, 0, 0, 0,
		0, 15, 'c', 2, 0, 1, 0, 0, 0, 0, 0,
	)
}

func TestEveryKeyCodeHasItsTag(t *testing.T) {
	cases := []struct {
		code KeyCode
		want []byte
	}{
		{KeyCode{Kind: KeyBackspace}, []byte{0}},
		{KeyCode{Kind: KeyEnter}, []byte{1}},
		{KeyCode{Kind: KeyLeft}, []byte{2}},
		{KeyCode{Kind: KeyRight}, []byte{3}},
		{KeyCode{Kind: KeyUp}, []byte{4}},
		{KeyCode{Kind: KeyDown}, []byte{5}},
		{KeyCode{Kind: KeyHome}, []byte{6}},
		{KeyCode{Kind: KeyEnd}, []byte{7}},
		{KeyCode{Kind: KeyPageUp}, []byte{8}},
		{KeyCode{Kind: KeyPageDown}, []byte{9}},
		{KeyCode{Kind: KeyTab}, []byte{10}},
		{KeyCode{Kind: KeyBackTab}, []byte{11}},
		{KeyCode{Kind: KeyDelete}, []byte{12}},
		{KeyCode{Kind: KeyInsert}, []byte{13}},
		{KeyCode{Kind: KeyEsc}, []byte{14}},
		{CharKey('é'), []byte{15, 0xC3, 0xA9}},
		{FKey(5), []byte{16, 5}},
	}
	for _, c := range cases {
		var e Enc
		putCode(&e, c.code)
		if !bytes.Equal(e.Buf, c.want) {
			t.Errorf("%+v: got %v, want %v", c.code, e.Buf, c.want)
		}
	}
}

func TestKeyCodesAndEventsEncodeAsSpecified(t *testing.T) {
	var e Enc
	putCode(&e, FKey(5))
	wantBytes(t, e.Buf, 16, 5)

	e = Enc{}
	PasteEvent{"x"}.put(&e)
	wantBytes(t, e.Buf, 3, 1, 'x')

	e = Enc{}
	KeyEvent{Code: KeyCode{Kind: KeyEnter}, Repeat: true}.put(&e)
	wantBytes(t, e.Buf, 0, 1, 0, 1, 1, 0, 0, 0, 0, 0) // repeat is kind 1
}

func TestWheelGoldenBytes(t *testing.T) {
	msg := InputMsg{PaneID: "w1:p1", Events: []InputEvent{
		WheelEvent{Dir: WheelUp, Column: 3, Row: 200, Mods: ModShift, Lines: 3},
		WheelEvent{Dir: WheelDown, Lines: 300},
	}}
	wantBytes(t, msg.Encode(),
		13, 5, 'w', '1', ':', 'p', '1',
		2,
		2, 4, 0, 3, 200, 0, 1, 3,
		2, 5, 0, 0, 0, 0, 0, 251, 44, 1,
	)
}

func TestEveryWheelDirectionHasItsKind(t *testing.T) {
	dirs := map[WheelDir]byte{WheelUp: 4, WheelDown: 5, WheelLeft: 6, WheelRight: 7}
	for dir, kind := range dirs {
		var e Enc
		WheelEvent{Dir: dir, Lines: 1}.put(&e)
		wantBytes(t, e.Buf, 2, kind, 0, 0, 0, 0, 0, 1)
	}
}

func TestFocusAndDetachGoldenBytes(t *testing.T) {
	wantBytes(t, FocusMsg{true}.Encode(), 18, 1)
	wantBytes(t, FocusMsg{false}.Encode(), 18, 0)
	wantBytes(t, DetachMsg{}.Encode(), 4)
}

func TestHelloCarriesEveryFieldOfTheContract(t *testing.T) {
	d := NewDec(HelloMsg{Size: testSize, Cell: testCell}.Encode())
	if v, err := d.Vu(); err != nil || v != CControl {
		t.Fatalf("tag %d, %v", v, err)
	}
	if s, err := d.Str(); err != nil || s != HelloKind {
		t.Fatalf("kind %q, %v", s, err)
	}
	data, err := d.Str()
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{
		"generation": 1, "cell_width_px": 8, "cell_height_px": 16,
		"surface_size": {"cols": 80, "rows": 24},
		"pixel_mouse": false, "direct_graphics": false, "endpoint_keybindings": false,
		"mouse_capture": false, "surface_active": true, "surface_reuse": false,
		"surface_delta": false, "surface_scroll": false,
		"snapshot_codecs": ["shell.snapshot.v1"], "surface_codecs": ["shell.surface.v1"],
		"input_codecs": ["shell.input.semantic.v1"], "blob_codecs": ["shell.blob.v1"]
	}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWelcomeCheckAcceptsATestedVersion(t *testing.T) {
	v, err := CheckWelcome(parseWelcome(t, welcomeJSON("")))
	if err != nil || !v.Tested() {
		t.Fatalf("got %+v, %v", v, err)
	}
}

func TestWelcomeCheckWarnsOnAnUntestedVersion(t *testing.T) {
	w := parseWelcome(t, strings.Replace(welcomeJSON(""), "0.9.3", "0.10.0", 1))
	v, err := CheckWelcome(w)
	if err != nil || v.Untested != "0.10.0" || v.Tested() {
		t.Fatalf("got %+v, %v", v, err)
	}
}

func TestWelcomeCheckRefusesAnotherGeneration(t *testing.T) {
	w := parseWelcome(t, strings.Replace(welcomeJSON(""), `"generation":1`, `"generation":2`, 1))
	_, err := CheckWelcome(w)
	if err == nil || !strings.Contains(err.Error(), "generation 2") {
		t.Fatalf("got %v", err)
	}
}

func TestWelcomeCheckRefusesAServerErrorWithItsMessage(t *testing.T) {
	w := parseWelcome(t, welcomeJSON(`,"error":{"code":"no_common_core","message":"no shared core"}`))
	_, err := CheckWelcome(w)
	if err == nil || !strings.Contains(err.Error(), "no_common_core") || !strings.Contains(err.Error(), "no shared core") {
		t.Fatalf("got %v", err)
	}
}

func TestWelcomeCheckRefusesAnotherSurfaceCodec(t *testing.T) {
	w := parseWelcome(t, strings.Replace(welcomeJSON(""), "shell.surface.v1", "shell.surface.v2", 1))
	_, err := CheckWelcome(w)
	if err == nil || !strings.Contains(err.Error(), "surface") || !strings.Contains(err.Error(), "shell.surface.v2") {
		t.Fatalf("got %v", err)
	}
}

func TestWelcomeParsesWithUnknownFieldsAndWithoutError(t *testing.T) {
	w := parseWelcome(t, welcomeJSON(`,"future_field":[1,2],"another":{"x":true}`))
	if w.Error != nil {
		t.Fatal("error set")
	}
	if v, err := CheckWelcome(w); err != nil || !v.Tested() {
		t.Fatalf("got %+v, %v", v, err)
	}
}

func TestSnapshotReadsTheFocusedPaneWhenPresent(t *testing.T) {
	var snap ShellSnapshot
	if err := json.Unmarshal([]byte(`{"boot_id":"b1","revision":7,"focused_pane_id":"w1:p1","extra":1}`), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.BootID != "b1" || snap.Revision != 7 || snap.FocusedPaneID == nil || *snap.FocusedPaneID != "w1:p1" {
		t.Fatalf("got %+v", snap)
	}
	var bare ShellSnapshot
	if err := json.Unmarshal([]byte(`{"boot_id":"b2","revision":1}`), &bare); err != nil {
		t.Fatal(err)
	}
	if bare.FocusedPaneID != nil {
		t.Fatalf("got %v", *bare.FocusedPaneID)
	}
}

func TestDecodesControlFrames(t *testing.T) {
	p := serverPayload(func(e *Enc) {
		e.Vu(SControl)
		e.Str("shell.snapshot.v1")
		e.Str("{}")
	})
	wantServer(t, p, ControlMsg{Kind: "shell.snapshot.v1", Data: "{}"})
}

func TestDecodesShutdownWithAndWithoutReason(t *testing.T) {
	wantServer(t, serverPayload(func(e *Enc) { e.Vu(SShutdown); e.None() }), ShutdownMsg{})
	wantServer(t, serverPayload(func(e *Enc) { e.Vu(SShutdown); e.Some(); e.Str("bye") }), ShutdownMsg{Reason: str("bye")})
}

func TestDecodesShellError(t *testing.T) {
	wantServer(t, serverPayload(func(e *Enc) { e.Vu(SShellError); e.Str("hi") }), ShellErrorMsg{Text: "hi"})
}

// clipboardPayload holds data as raw bytes, so any byte can be sent.
func clipboardPayload(data []byte) []byte {
	return serverPayload(func(e *Enc) {
		e.Vu(SClipboard)
		e.Vu(uint64(len(data)))
		e.Buf = append(e.Buf, data...)
	})
}

func TestDecodesAClipboardWriteToItsText(t *testing.T) {
	wantServer(t, serverPayload(func(e *Enc) { e.Vu(SClipboard); e.Str("aGk=") }), ClipboardMsg{Text: "hi"})
	wantServer(t, clipboardPayload([]byte("aGk")), ClipboardMsg{Text: "hi"}) // unpadded
}

func TestAClipboardPayloadThatIsNotUsableTextIsOther(t *testing.T) {
	cases := []struct {
		what string
		data []byte
	}{
		{"not base64", []byte("!!")},
		// An OSC 52 query asks for the clipboard; contr never answers it.
		{"a query", []byte("?")},
		{"not UTF-8 once decoded", []byte("/w==")},
		{"not UTF-8 on the wire", []byte{0xFF}},
		{"a lone symbol", []byte("A")},
		{"too large", bytes.Repeat([]byte("A"), MaxTextBytes*2)},
	}
	for _, c := range cases {
		got, err := DecodeServer(clipboardPayload(c.data))
		if err != nil || got != (OtherMsg{Tag: SClipboard}) {
			t.Errorf("%s: got %#v, %v", c.what, got, err)
		}
	}
}

func TestAClipboardFrameWhoseLengthIsPastTheEndIsAnError(t *testing.T) {
	_, err := DecodeServer([]byte{5, 9, 'a'})
	wantKind(t, err, TooLong, 9)
}

func TestDecodesLegacyWelcomeWithAndWithoutError(t *testing.T) {
	refused := serverPayload(func(e *Enc) {
		e.Vu(SWelcomeLegacy)
		e.Vu(1) // version
		e.Vu(0) // encoding
		e.Some()
		e.Str("nope")
	})
	wantServer(t, refused, LegacyWelcomeMsg{Error: str("nope")})

	ok := serverPayload(func(e *Enc) {
		e.Vu(SWelcomeLegacy)
		e.Vu(1)
		e.Vu(0)
		e.None()
	})
	wantServer(t, ok, LegacyWelcomeMsg{})
}

func TestLegacyWelcomeChecksTheRenderEncodingIndex(t *testing.T) {
	for _, enc := range []uint64{0, 1} {
		wantServer(t, serverPayload(func(e *Enc) {
			e.Vu(SWelcomeLegacy)
			e.Vu(1)
			e.Vu(enc)
			e.None()
		}), LegacyWelcomeMsg{})
	}
	_, err := DecodeServer(serverPayload(func(e *Enc) {
		e.Vu(SWelcomeLegacy)
		e.Vu(1)
		e.Vu(2)
		e.None()
	}))
	wantKind(t, err, BadTag, 2)
	if ce := err.(*CodecError); ce.What != "render encoding" {
		t.Fatalf("what %q", ce.What)
	}
}

func TestUnknownTagsAreOtherAndTheirBodyIsNotRead(t *testing.T) {
	wantServer(t, []byte{7}, OtherMsg{Tag: 7})
	wantServer(t, []byte{7, 1, 2, 3}, OtherMsg{Tag: 7})
}

func TestDecodeErrorsAreReported(t *testing.T) {
	_, err := DecodeServer(nil)
	wantKind(t, err, Truncated, 0)
	// Shutdown with an option tag that is neither 0 nor 1.
	_, err = DecodeServer([]byte{3, 9})
	wantKind(t, err, BadTag, 9)
	if ce := err.(*CodecError); ce.What != "option" {
		t.Fatalf("what %q", ce.What)
	}
	// Control whose kind string claims 5 bytes but has 1.
	_, err = DecodeServer([]byte{20, 5, 'a'})
	wantKind(t, err, TooLong, 5)
}
