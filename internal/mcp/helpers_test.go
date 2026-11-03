package mcp

import (
	"bytes"
	"io"
	"sync"
)

func ioPipe() (*io.PipeReader, *io.PipeWriter) { return io.Pipe() }

// syncBuffer is a bytes.Buffer that is safe for one writer and one reader.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
