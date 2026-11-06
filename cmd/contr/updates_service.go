//go:build !e2e

package main

import "github.com/contursofran/contr/internal/app"

// updateService is MyGo's updater (D115).
func updateService() app.Updates { return &mygoUpdates{} }
