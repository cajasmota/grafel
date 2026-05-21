package audit

import "sync"

const brokerBuffer = 64

type sub struct {
	ch chan Entry
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
		select {
		case s.ch <- e:
		default:
			// subscriber buffer full — drop
		}
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
			close(s.ch)
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
