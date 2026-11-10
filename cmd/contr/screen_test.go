package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// screenLocked reads the output of `ioreg -n Root -d1 -a` and tells whether
// the screen is locked: IOConsoleLocked is true, or the user on the console
// has CGSSessionScreenIsLocked (#608, D230). While the screen is locked,
// macOS shows no window, and MyGo holds the frames of a window that does not
// show. The automation tree then keeps the first frame of the app.
func screenLocked(plist []byte) (bool, error) {
	v, err := decodePlist(plist)
	if err != nil {
		return false, err
	}
	root, ok := v.(map[string]any)
	if !ok {
		return false, errors.New("ioreg: the plist is not a dict")
	}
	if root["IOConsoleLocked"] == true {
		return true, nil
	}
	users, _ := root["IOConsoleUsers"].([]any)
	for _, u := range users {
		if m, ok := u.(map[string]any); ok && m["kCGSSessionOnConsoleKey"] == true && m["CGSSessionScreenIsLocked"] == true {
			return true, nil
		}
	}
	return false, nil
}

// decodePlist decodes an XML property list: a dict is a map[string]any, an
// array a []any, true and false a bool, and each other value its text.
func decodePlist(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil, errors.New("plist: no value")
		}
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local != "plist" {
			return plistValue(d, se)
		}
	}
}

// plistValue decodes the value that starts with se.
func plistValue(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "true", "false":
		if err := d.Skip(); err != nil {
			return nil, err
		}
		return se.Name.Local == "true", nil
	case "dict":
		m := map[string]any{}
		key := ""
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.EndElement:
				return m, nil
			case xml.StartElement:
				if t.Name.Local == "key" {
					var k string
					if err := d.DecodeElement(&k, &t); err != nil {
						return nil, err
					}
					key = k
					continue
				}
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				m[key] = v
			}
		}
	case "array":
		var a []any
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.EndElement:
				return a, nil
			case xml.StartElement:
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
		}
	default:
		var s string
		if err := d.DecodeElement(&s, &se); err != nil {
			return nil, fmt.Errorf("plist: %s: %w", se.Name.Local, err)
		}
		return strings.TrimSpace(s), nil
	}
}

const plistHead = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
`

// ioregRoot is the start of `ioreg -n Root -d1 -a` with these values.
func ioregRoot(consoleLocked string, users ...string) []byte {
	return []byte(plistHead + `<dict>
	<key>IOConsoleLocked</key>
	<` + consoleLocked + `/>
	<key>IOConsoleUsers</key>
	<array>` + strings.Join(users, "") + `</array>
	<key>IOKitBuildVersion</key>
	<string>Darwin Kernel Version 25.6.0</string>
	<key>IOKitDiagnostics</key>
	<dict><key>Classes</key><dict><key>A</key><integer>1</integer></dict></dict>
</dict>
</plist>
`)
}

// consoleUser is one dict of IOConsoleUsers.
func consoleUser(onConsole, locked string) string {
	return `<dict>
		<key>CGSSessionScreenIsLocked</key>
		<` + locked + `/>
		<key>CGSSessionScreenLockedTime</key>
		<integer>1794267726</integer>
		<key>kCGSSessionOnConsoleKey</key>
		<` + onConsole + `/>
		<key>kCGSSessionUserNameKey</key>
		<string>scratch</string>
	</dict>`
}

func TestScreenLocked(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want bool
	}{
		{"locked console user", ioregRoot("false", consoleUser("true", "true")), true},
		{"console locked only", ioregRoot("true", consoleUser("true", "false")), true},
		{"unlocked", ioregRoot("false", consoleUser("true", "false")), false},
		{"locked user off the console", ioregRoot("false", consoleUser("false", "true"), consoleUser("true", "false")), false},
		{"no users", ioregRoot("false"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := screenLocked(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("screenLocked = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScreenLockedBadPlist(t *testing.T) {
	for _, in := range []string{"", "not xml <", plistHead + "<array></array></plist>", plistHead + "<dict><key>A</key>"} {
		if _, err := screenLocked([]byte(in)); err == nil {
			t.Errorf("screenLocked(%q) has no error", in)
		}
	}
}
