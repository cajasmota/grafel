package resolve

// toid_shape.go — the single source of truth for classifying a relationship
// ToID by its *shape* into "resolved" vs "unresolved stub" (bug edge).
//
// This is the structural heuristic that grafel_stats / `orient view=stats`
// uses to derive import fidelity, and that the docgen-repair fidelity accounting
// (internal/mcp) and the `grafel feedback` collector (internal/feedback) both
// rely on. Keeping it here — beside the DispositionBugExtractor taxonomy —
// prevents the three call sites from drifting apart.
//
// A ToID is considered RESOLVED when it is either:
//   - a 16-char lowercase-hex entity ID (the shape of graph.EntityID()), or
//   - an ext:-qualified external reference (a known, deliberately-external target).
//
// Everything else is a raw stub the resolver never bound → unresolved → bug edge.

// IsHexString reports whether every byte in s is a lowercase hex digit.
func IsHexString(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// IsResolvedToID reports whether toID has been bound to a concrete target:
// a 16-char lowercase-hex entity ID or an ext:-qualified external reference.
// An empty ToID is not considered resolved.
func IsResolvedToID(toID string) bool {
	if toID == "" {
		return false
	}
	// Resolved: hex entity ID (16 lowercase hex chars).
	if len(toID) == 16 && IsHexString(toID) {
		return true
	}
	// Resolved: ext:-qualified external.
	if len(toID) > 4 && toID[:4] == "ext:" {
		return true
	}
	return false
}

// IsBugEdgeToID reports whether toID represents an unresolved stub — a target
// the resolver failed to bind to a hex entity ID or an ext:-qualified external.
// An empty ToID is NOT a bug edge (there is nothing to resolve).
func IsBugEdgeToID(toID string) bool {
	if toID == "" {
		return false
	}
	return !IsResolvedToID(toID)
}
