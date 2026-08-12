package audit

import "sync"

const brokerBuffer = 64

// sub holds one SSE subscriber's channel plus the guard that orders a fan-out
// send against the close performed by that subscriber's cancel func.
//
// #6299 — Publish snapshots the subscriber slice under the read lock and sends
// OUTSIDE it, so one stalled SSE client cannot stall the audit writer for
// everyone else. That left the send unordered with respect to cancel's close: a
// data race, and a live `panic: send on closed channel` inside the daemon's HTTP
// server whenever a browser tab closed mid-publish. mu restores the ordering per
// subscriber rather than per broker; the guarded region is a bool check plus one
// non-blocking send, so a slow client still cannot hold up a publisher.
type sub struct {
	mu     sync.RWMutex
	closed bool
	ch     chan Entry
}

// send delivers e unless the subscriber has been cancelled or its buffer is
// full. It never blocks.
func (s *sub) send(e Entry) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		// Cancelled — the consumer has gone away. Dropping is correct: audit SSE
		// delivery is best-effort (Publish already drops on a full buffer), and
		// where an audit log is configured it keeps the durable record. Writer's
		// log may be nil, so that is a mitigation, not a guarantee — the entry is
		// only ever dropped for a subscriber whose channel is already closed and
		// whose consumer is gone.
		return
	}
	select {
	case s.ch <- e:
	default:
		// subscriber buffer full — drop
	}
}

// close closes the subscriber's channel so a ranging consumer terminates. It is
// idempotent and excludes any concurrent send.
func (s *sub) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

// Broker fans out audit entries to SSE subscribers in real time.
// It is wired into the dashboard server and called by the audit.Log
// wrapper whenever an entry is appended.
//
// Publish is non-blocking: if a subscriber's buffer is full the event
// is dropped rather than blocking the caller.
type Broker struct {
	mu   sync.RWMutex
	subs []*sub
}

// NewBroker constructs an empty Broker.
func NewBroker() *Broker {
	return &Broker{}
}

// Publish fans e out to every current subscriber.
func (b *Broker) Publish(e Entry) {
	b.mu.RLock()
	targets := make([]*sub, len(b.subs))
	copy(targets, b.subs)
	b.mu.RUnlock()

	for _, s := range targets {
		s.send(e)
	}
}

// Subscribe returns a receive-only channel that receives every entry
// published after the call. The caller must invoke the returned cancel
// function (e.g. on HTTP disconnect) to clean up.
func (b *Broker) Subscribe() (<-chan Entry, func()) {
	s := &sub{ch: make(chan Entry, brokerBuffer)}

	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			for i, cur := range b.subs {
				if cur == s {
					b.subs[i] = b.subs[len(b.subs)-1]
					b.subs[len(b.subs)-1] = nil
					b.subs = b.subs[:len(b.subs)-1]
					break
				}
			}
			// Delegated so the close is ordered against any in-flight fan-out
			// send (#6299). Lock order is b.mu -> sub.mu, never the reverse.
			s.close()
		})
	}
	return s.ch, cancel
}

// SubscriberCount returns the number of live SSE subscribers.
func (b *Broker) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
