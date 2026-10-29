//go:build !darwin

package main

// disablePressAndHold does nothing off macOS.
func disablePressAndHold() error { return nil }
