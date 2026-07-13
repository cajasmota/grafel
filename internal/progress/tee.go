package progress

// TeePublisher fans one Publish out to N wrapped publishers so a caller can, in
// a later PR, tee the engine's in-memory Broker AND a SidecarWriter from a
// single Publisher handed to the indexer. Non-blocking semantics are preserved:
// each child is itself a non-blocking Publisher (Broker drops-on-full,
// SidecarWriter drops-oldest), so a straight sequential fan-out cannot stall
// one child behind another — the simplest correct approach.
type TeePublisher struct {
	children []Publisher
}

// NewTeePublisher returns a Publisher that forwards every event to each of pubs
// in order. A nil child is tolerated and skipped.
func NewTeePublisher(pubs ...Publisher) *TeePublisher {
	return &TeePublisher{children: pubs}
}

// Publish forwards e to every wrapped publisher. Safe for concurrent use so
// long as the children are (Broker and SidecarWriter both are).
func (t *TeePublisher) Publish(e Event) {
	if t == nil {
		return
	}
	for _, p := range t.children {
		if p == nil {
			continue
		}
		p.Publish(e)
	}
}
