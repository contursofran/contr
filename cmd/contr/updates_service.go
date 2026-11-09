//go:build !e2e

package main

import "github.com/contursofran/contr/internal/app/settings"

// updateService is MyGo's updater (D115).
func updateService() settings.Updates { return &mygoUpdates{} }
