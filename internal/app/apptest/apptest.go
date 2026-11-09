// Package apptest holds test helpers that the tests of more than one package
// under internal/app use. Only _test.go files import it.
package apptest

import "github.com/contursofran/contr/internal/app/view"

// HeldEnv holds the work and the results until the test runs them.
type HeldEnv struct {
	Work, Done []func()
}

// Env is a view.Env that holds the work in Work and the results in Done.
func (h *HeldEnv) Env() view.Env {
	return view.Env{Run: view.Runner{
		Go:   func(f func()) { h.Work = append(h.Work, f) },
		Post: func(f func()) { h.Done = append(h.Done, f) },
	}}
}

// RunAll runs the held work, then the held results, in order.
func (h *HeldEnv) RunAll() {
	for _, f := range h.Work {
		f()
	}
	h.Work = nil
	for _, f := range h.Done {
		f()
	}
	h.Done = nil
}
