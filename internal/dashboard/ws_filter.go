package dashboard

// ws_filter.go — per-connection subscription filter for the WebSocket push endpoint.
//
// # Subscription semantics
//
// A wsFilter is installed on a wsClient when the browser sends a "subscribe"
// message. The filter holds two optional allow-lists:
//
//   - groups: a set of group names. When non-empty, only events whose
//     WSEvent.Group is in the set pass the filter. When empty, all groups pass.
//   - refs: a set of ref names (branch / tag). When non-empty, only events
//     whose WSEvent.Ref is in the set pass the filter — with one exception:
//     if WSEvent.Ref is "" (the event carries no ref context, e.g. a legacy
//     daemon-log event), it passes unconditionally because legacy publishers
//     pre-date the ref field and dropping them would be surprising.
//     When refs is empty, all refs pass.
//
// The two conditions are combined with AND:
//
//	pass = groupPass(evt) AND refPass(evt)
//
// Within each condition the test is OR-across-members (any matching entry is
// sufficient). This is consistent with how ?ref= multi-value queries work
// elsewhere in the API (#2220).
//
// # Backward compatibility
//
// Clients that never send a "subscribe" message have sub==nil and receive every
// event (firehose mode). This is the pre-#2221 behaviour.

// wsFilter holds the compiled allow-lists for one connection.
type wsFilter struct {
	// groups is the set of allowed group names. nil/empty means all groups.
	groups map[string]struct{}
	// refs is the set of allowed ref names. nil/empty means all refs.
	refs map[string]struct{}
}

// newWSFilter builds a wsFilter from the raw slices in a "subscribe" message.
// Duplicate entries are deduplicated. nil slices and empty slices are treated
// identically (no constraint on that dimension).
func newWSFilter(groups, refs []string) *wsFilter {
	f := &wsFilter{}
	if len(groups) > 0 {
		f.groups = make(map[string]struct{}, len(groups))
		for _, g := range groups {
			if g != "" {
				f.groups[g] = struct{}{}
			}
		}
		if len(f.groups) == 0 {
			f.groups = nil // all entries were empty strings
		}
	}
	if len(refs) > 0 {
		f.refs = make(map[string]struct{}, len(refs))
		for _, r := range refs {
			if r != "" {
				f.refs[r] = struct{}{}
			}
		}
		if len(f.refs) == 0 {
			f.refs = nil // all entries were empty strings
		}
	}
	return f
}

// Matches reports whether evt passes this filter.
//
// Group check (when groups allow-list is non-empty):
//
//	evt.Group must be in the allow-list.
//
// Ref check (when refs allow-list is non-empty):
//
//	evt.Ref must be in the allow-list OR evt.Ref == "" (legacy event with no
//	ref context always passes, preserving backward compatibility).
//
// Both checks must pass for the event to be delivered.
func (f *wsFilter) Matches(evt WSEvent) bool {
	// Group check.
	if len(f.groups) > 0 {
		if _, ok := f.groups[evt.Group]; !ok {
			return false
		}
	}

	// Ref check.
	if len(f.refs) > 0 {
		// Legacy events (Ref=="") always pass so pre-#2221 publishers stay visible.
		if evt.Ref != "" {
			if _, ok := f.refs[evt.Ref]; !ok {
				return false
			}
		}
	}

	return true
}
