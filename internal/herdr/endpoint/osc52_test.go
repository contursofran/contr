// Adapted from Herdr UI (Apache-2.0); changes: ported to Go, takes the raw wire bytes, drops the
// pending-write bound (the link queues updates), adds tests for a query and an empty clear.

package endpoint

import (
	"strings"
	"testing"
)

func decodeStr(data string) (string, bool) { return DecodeOSC52([]byte(data)) }

func TestDecodesPaddedUnpaddedAndMultibytePayloads(t *testing.T) {
	for data, want := range map[string]string{
		"dGVzdA==":         "test",
		"dGVzdA":           "test",
		"aGVsbG8gd29ybGQ=": "hello world",
		"5Lmf5LiN":         "也不",
	} {
		if got, ok := decodeStr(data); !ok || got != want {
			t.Errorf("%q = %q, %v; want %q", data, got, ok, want)
		}
	}
}

func TestAnEmptyPayloadIsAClearNotAnError(t *testing.T) {
	if got, ok := decodeStr(""); !ok || got != "" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestRejectsMalformedAndNoncanonicalPayloads(t *testing.T) {
	for _, data := range []string{
		"dGVzdA=",   // wrong padding for the symbol count
		"dGVzdA===", // more padding than a quantum allows
		"dGVzd!",    // outside the alphabet
		"dGVzdA==x", // data after padding
		"=",         // no symbols at all
		"A",         // a lone symbol carries no byte
		"AB",        // a two-symbol quantum with non-zero trailing bits
	} {
		if got, ok := decodeStr(data); ok {
			t.Errorf("%q accepted as %q", data, got)
		}
	}
}

func TestAQueryIsNotAWrite(t *testing.T) {
	// An OSC 52 query sends "?" in place of the data. It is not base64.
	if _, ok := decodeStr("?"); ok {
		t.Fatal("query accepted")
	}
}

func TestRejectsNonUTF8Payloads(t *testing.T) {
	// 0xff is a byte, not valid UTF-8 text.
	if _, ok := decodeStr("/w=="); ok {
		t.Fatal("non-UTF-8 accepted")
	}
}

func TestRejectsOversizedPayloads(t *testing.T) {
	// Four symbols carry three bytes, so this is the largest payload that
	// still fits the limit; one more quantum spills past it.
	allowed := strings.Repeat("AAAA", MaxTextBytes/3)
	if _, ok := decodeStr(allowed); !ok {
		t.Fatal("largest allowed payload rejected")
	}
	if _, ok := decodeStr(allowed + "AAAA"); ok {
		t.Fatal("payload past the limit accepted")
	}
	// Far past the limit: refused before any decoding.
	if _, ok := decodeStr(strings.Repeat("A", MaxTextBytes*2)); ok {
		t.Fatal("huge payload accepted")
	}
}
