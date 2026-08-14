package analysis

import (
	"context"
	"io"
)

// cancelableReads wraps r so that a Read blocked inside it — a network or
// upload stream waiting for bytes that never come — cannot pin a cancelled
// parse forever. The parser's own Cancel only takes effect between demo
// frames; this wrapper covers the other half of the cancellation contract,
// the time spent inside r.Read itself.
//
// Reads are served by a single goroutine that owns its own buffer. When ctx
// is cancelled while a read is in flight, Read abandons it and returns the
// context's error immediately; the abandoned r.Read keeps running in that
// goroutine until r itself returns, and its result is then discarded.
//
// A context that can never be cancelled costs nothing: r is returned as-is,
// with no goroutine and no copying. The returned stop function lets the
// serving goroutine exit once it is idle; call it exactly once, after the
// last Read.
func cancelableReads(ctx context.Context, r io.Reader) (io.Reader, func()) {
	if ctx.Done() == nil {
		return r, func() {}
	}
	c := &cancelableReader{
		ctx: ctx,
		req: make(chan int),
		// Room for one undelivered result, so a read abandoned mid-flight
		// never leaves the serving goroutine blocked on the send.
		resp: make(chan readOutcome, 1),
	}
	go func() {
		var buf []byte
		for size := range c.req {
			if cap(buf) < size {
				buf = make([]byte, size)
			}
			n, err := r.Read(buf[:size])
			c.resp <- readOutcome{data: buf[:n], err: err}
		}
	}()
	return c, func() { close(c.req) }
}

type readOutcome struct {
	data []byte
	err  error
}

type cancelableReader struct {
	ctx  context.Context
	req  chan int         // next read's requested size
	resp chan readOutcome // that read's result
	err  error            // sticky after an abandoned read, keeping req and resp in lockstep
}

// Read hands the request to the serving goroutine and waits for whichever
// comes first: the result or the context's cancellation. The goroutine only
// reuses its buffer after accepting the next request, so the copy below is
// always taken before the buffer can change.
func (c *cancelableReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	select {
	case c.req <- len(p):
	case <-c.ctx.Done():
		c.err = c.ctx.Err()
		return 0, c.err
	}
	select {
	case out := <-c.resp:
		return copy(p, out.data), out.err
	case <-c.ctx.Done():
		c.err = c.ctx.Err()
		return 0, c.err
	}
}
