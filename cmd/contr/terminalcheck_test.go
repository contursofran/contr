package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestTerminalCheckPrintsThePath(t *testing.T) {
	var out, errb bytes.Buffer
	code := terminalCheck(func() error { return nil }, func() (string, error) { return "/lib/x.dylib", nil }, &out, &errb)
	if code != 0 || out.String() != "/lib/x.dylib\n" || errb.Len() != 0 {
		t.Errorf("code %d out %q err %q", code, out.String(), errb.String())
	}
}

func TestTerminalCheckFailsWhenTheLibraryDoesNotLoad(t *testing.T) {
	var out, errb bytes.Buffer
	code := terminalCheck(func() error { return errors.New("missing") }, func() (string, error) { return "", nil }, &out, &errb)
	if code != 1 || out.Len() != 0 || errb.String() != "contr: terminal unavailable: missing\n" {
		t.Errorf("code %d out %q err %q", code, out.String(), errb.String())
	}
}

func TestTerminalCheckFailsWithoutAPath(t *testing.T) {
	var out, errb bytes.Buffer
	code := terminalCheck(func() error { return nil }, func() (string, error) { return "", errors.New("none") }, &out, &errb)
	if code != 1 || out.Len() != 0 {
		t.Errorf("code %d out %q", code, out.String())
	}
}

func TestTerminalCheckArgument(t *testing.T) {
	same(t, parse("contr", "terminal-check"), Command{Kind: kindTerminalCheck})
	same(t, parse("contr", "terminal-check", "x"), Command{Kind: kindUnknown, Unknown: "terminal-check"})
}
