package kit

import (
	"encoding/xml"
	"testing"

	"github.com/contursofran/contr/assets"
)

// mascotPath is the path of the "Claude Code" icon, as NOTICE credits it.
const mascotPath = "M20.998 10.949H24v3.102h-3v3.028h-1.487V20H18v-2.921h-1.487V20H15v-2.921H9V20H7.488v-2.921H6V20H4.487v-2.921H3V14.05H0V10.95h3V5h17.998v5.949zM6 10.949h1.488V8.102H6v2.847zm10.51 0H18V8.102h-1.49v2.847z"

// The SVG is the credited path, unchanged, in a viewBox cropped to its
// shape, with one fill that the view replaces.
func TestMascotSVGIsTheCreditedIcon(t *testing.T) {
	data, err := assets.Icons.ReadFile("icons/claude-mascot.svg")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ViewBox string `xml:"viewBox,attr"`
		Fill    string `xml:"fill,attr"`
		Paths   []struct {
			D        string `xml:"d,attr"`
			FillRule string `xml:"fill-rule,attr"`
			Fill     string `xml:"fill,attr"`
		} `xml:"path"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.ViewBox != "0 5 24 15" || doc.Fill == "" {
		t.Errorf("svg has viewBox %q and fill %q, want 0 5 24 15 and one fill", doc.ViewBox, doc.Fill)
	}
	if len(doc.Paths) != 1 {
		t.Fatalf("svg has %d paths, want 1", len(doc.Paths))
	}
	p := doc.Paths[0]
	if p.D != mascotPath || p.FillRule != "evenodd" || p.Fill != "" {
		t.Errorf("path is %+v, want the credited path, evenodd, in the one fill", p)
	}
}
