package view

import (
	"context"
	"time"
)

// Latest makes the latest read win. A view keeps one Latest for its reads.
// Reset starts a new generation: the reads of the old one get a context that
// ends, and their results are dropped. Reads of the current generation apply
// their results on the UI thread. The zero value works. Use it on the UI
// thread only.
type Latest struct {
	gen uint64
	// ctx ends when the generation ends; nil until a read needs it. cancel
	// ends it. inflight counts the reads of this generation not yet applied.
	ctx      context.Context
	cancel   context.CancelFunc
	inflight int
}

// Reset ends the reads in flight: their context ends and their results are
// dropped. Call it when a newer read replaces them or the view closes.
func (l *Latest) Reset() {
	l.gen++
	l.inflight = 0
	if l.cancel != nil {
		l.cancel()
	}
	l.ctx, l.cancel = nil, nil
}

// Busy tells whether a read of the current generation is in flight.
func (l *Latest) Busy() bool { return l.inflight > 0 }

// Bind returns fn guarded by the current generation: it runs fn only while
// no Reset came after Bind.
func (l *Latest) Bind(fn func()) func() {
	gen := l.gen
	return func() {
		if gen == l.gen {
			fn()
		}
	}
}

// Do runs work off the UI thread through env, in the current generation. The
// function that work returns runs on the UI thread, unless a Reset came
// first.
func (l *Latest) Do(env Env, work func() func()) {
	l.Read(env, 0, func(context.Context) func() { return work() })
}

// Read is Do with a context: it ends at a Reset, or after limit when limit is
// more than 0. When the last read of the generation is applied, the context
// ends too.
func (l *Latest) Read(env Env, limit time.Duration, read func(ctx context.Context) func()) {
	if l.ctx == nil {
		l.ctx, l.cancel = context.WithCancel(context.Background())
	}
	ctx, gen := l.ctx, l.gen
	l.inflight++
	env.Do(func() func() {
		rctx, stop := ctx, context.CancelFunc(func() {})
		if limit > 0 {
			rctx, stop = context.WithTimeout(ctx, limit)
		}
		apply := read(rctx)
		stop()
		return func() {
			if gen != l.gen {
				return
			}
			l.inflight--
			if l.inflight == 0 {
				l.cancel()
				l.ctx, l.cancel = nil, nil
			}
			if apply != nil {
				apply()
			}
		}
	})
}
