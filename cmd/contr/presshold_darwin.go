package main

import (
	"fmt"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// pressAndHoldKey is the defaults key that AppKit reads to decide if a held
// letter opens the accent menu.
const pressAndHoldKey = "ApplePressAndHoldEnabled"

// disablePressAndHold registers ApplePressAndHoldEnabled = NO in the app's own
// NSUserDefaults, so a held key repeats as it does in a terminal (D20).
// registerDefaults: lives in memory for this process. It writes nothing to
// disk and never changes the global defaults of the user. A value that the
// user set in the global domain still wins.
func disablePressAndHold() error {
	if _, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return fmt.Errorf("load Foundation: %w", err)
	}
	defaults := objc.ID(objc.GetClass("NSUserDefaults")).Send(objc.RegisterName("standardUserDefaults"))
	if defaults == 0 {
		return fmt.Errorf("NSUserDefaults is not available")
	}
	key := nsString(pressAndHoldKey)
	value := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithBool:"), false)
	dict := objc.ID(objc.GetClass("NSDictionary")).Send(objc.RegisterName("dictionaryWithObject:forKey:"), value, key)
	if key == 0 || value == 0 || dict == 0 {
		return fmt.Errorf("build the defaults dictionary")
	}
	defaults.Send(objc.RegisterName("registerDefaults:"), dict)
	return nil
}

// pressAndHoldEnabled reads the key back through NSUserDefaults.
func pressAndHoldEnabled() bool {
	defaults := objc.ID(objc.GetClass("NSUserDefaults")).Send(objc.RegisterName("standardUserDefaults"))
	return objc.Send[bool](defaults, objc.RegisterName("boolForKey:"), nsString(pressAndHoldKey))
}

// nsString makes an autoreleased NSString from s.
func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), s)
}
