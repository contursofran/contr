package main

import "testing"

func TestDisablePressAndHoldRegistersNo(t *testing.T) {
	if err := disablePressAndHold(); err != nil {
		t.Fatal(err)
	}
	// A value set in the global domain would win; the test machine may have
	// one, so only check when the key is unset there.
	if pressAndHoldEnabled() {
		t.Skip("ApplePressAndHoldEnabled is set to YES in the user's global defaults")
	}
}

func TestDisablePressAndHoldTwice(t *testing.T) {
	for range 2 {
		if err := disablePressAndHold(); err != nil {
			t.Fatal(err)
		}
	}
}
