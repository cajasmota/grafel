package transport

import (
	"net"
	"sync"
	"time"
)

// Pool is a fixed-size pool of idle connections to the daemon. It provides
// simple borrow/return semantics. Connections are closed (not recycled)
// when the pool is already full or when a returned connection has been
// closed by the remote end.
//
// Pool is safe for concurrent use.
type Pool struct {
	addr    string
	timeout time.Duration
	policy  RetryPolicy

	mu   sync.Mutex
	idle []net.Conn
	cap  int
}

// NewPool creates a Pool that maintains at most cap idle connections to addr.
// cap must be >= 1; values < 1 are treated as 1.
func NewPool(addr string, timeout time.Duration, policy RetryPolicy, cap int) *Pool {
	if cap < 1 {
		cap = 1
	}
	return &Pool{
		addr:    addr,
		timeout: timeout,
		policy:  policy,
		idle:    make([]net.Conn, 0, cap),
		cap:     cap,
	}
}

// Get returns an idle connection from the pool, or opens a new one if the
// pool is empty. Returns an error only when opening a new connection fails.
func (p *Pool) Get() (net.Conn, error) {
	p.mu.Lock()
	if len(p.idle) > 0 {
		c := p.idle[len(p.idle)-1]
		p.idle = p.idle[:len(p.idle)-1]
		p.mu.Unlock()
		return c, nil
	}
	p.mu.Unlock()
	return DialWithRetry(p.addr, p.timeout, p.policy)
}

// Put returns c to the pool. If the pool is already at capacity, or c has
// already been closed, c is closed instead. Callers should not use c after
// calling Put.
func (p *Pool) Put(c net.Conn) {
	if c == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.idle) >= p.cap {
		// Pool full — discard.
		go c.Close() //nolint:errcheck
		return
	}
	// Probe liveness: attempt a zero-byte read with a past deadline.
	// If the connection has been reset by the peer, this surfaces the error.
	if err := c.SetReadDeadline(time.Now()); err != nil {
		go c.Close() //nolint:errcheck
		return
	}
	var probe [1]byte
	if _, err := c.Read(probe[:]); err != nil {
		// timeout error == still alive; any other error means the conn is dead.
		if !isTimeout(err) {
			go c.Close() //nolint:errcheck
			return
		}
	}
	// Clear deadline before returning to pool.
	_ = c.SetReadDeadline(time.Time{})
	p.idle = append(p.idle, c)
}

// Close closes all idle connections in the pool.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.idle {
		_ = c.Close()
	}
	p.idle = p.idle[:0]
}

// Len returns the number of currently idle connections in the pool.
func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.idle)
}

// isTimeout reports whether err is a net.Error with Timeout() == true.
func isTimeout(err error) bool {
	if ne, ok := err.(net.Error); ok {
		return ne.Timeout()
	}
	return false
}
