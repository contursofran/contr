//go:build !darwin

package host

import "errors"

// SystemReader has no source on this platform; contr runs on macOS.
func SystemReader() Reader {
	return func() (Stats, error) { return Stats{}, errors.New("host stats: unsupported platform") }
}
