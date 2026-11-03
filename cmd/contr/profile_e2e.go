//go:build e2e

package main

import (
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"syscall"
)

// With CONTR_PROFILE set to a folder, SIGUSR1 starts a CPU profile and
// SIGUSR2 stops it and writes a heap profile: cpu.pprof and heap.pprof in
// that folder. It is in e2e builds only, to profile one bench phase
// (docs/BENCHMARKS.md).
func init() {
	dir := os.Getenv("CONTR_PROFILE")
	if dir == "" {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		var cpu *os.File
		for s := range ch {
			switch {
			case s == syscall.SIGUSR1 && cpu == nil:
				f, err := os.Create(filepath.Join(dir, "cpu.pprof"))
				if err != nil {
					slog.Error("cpu profile", "err", err)
					continue
				}
				if err := pprof.StartCPUProfile(f); err != nil {
					slog.Error("cpu profile", "err", err)
					_ = f.Close()
					continue
				}
				cpu = f
			case s == syscall.SIGUSR2 && cpu != nil:
				pprof.StopCPUProfile()
				if err := cpu.Close(); err != nil {
					slog.Error("cpu profile", "err", err)
				}
				cpu = nil
				writeHeap(filepath.Join(dir, "heap.pprof"))
			}
		}
	}()
}

func writeHeap(path string) {
	f, err := os.Create(path)
	if err != nil {
		slog.Error("heap profile", "err", err)
		return
	}
	defer f.Close()
	runtime.GC() // the profile shows the live heap
	if err := pprof.WriteHeapProfile(f); err != nil {
		slog.Error("heap profile", "err", err)
	}
}
