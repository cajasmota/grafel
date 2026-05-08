// Package resolve rewrites stub-form RelationshipRecord endpoint references
// (e.g. "View:User", "Model:Article", or a bare "Hello") into deterministic
// 16-char graph entity IDs by looking them up in the merged entity set.
//
// This is the substance of PORT-2-FIX (issue #24). PORT-2 produced thousands
// of relationships but every cross-file ToID was left as a stub string, so
// graph traversal dead-ended at the first cross-file reference. The resolver
// closes that gap.
//
// PORT-2-FIX-3 (issue #31) extends the resolver to handle two additional
// reference shapes emitted by Pass 3 cross-language extractors:
//
//   - Format A: scope:<kind>:<subtype>:<lang>:<file_path>:<name>
//   - Format B: scope:<kind>:<subtype>:<lang>:<file_path>:<scope_name>#<member_name>
//
// and adds a kind-hint code path (driven by the relationship's Kind field)
// that biases ambiguous bare-name lookups toward the kind families typically
// referenced by EXTENDS / IMPLEMENTS / CALLS edges.
package resolve

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// Index is a kind-aware (kind, name) -> entity_id lookup. The inner map only
// retains a name when the (kind, name) tuple resolves to exactly one entity;
// ambiguous tuples are tracked separately in the embedded ambig set so the
// resolver can leave them as stubs rather than silently picking a wrong match.
type Index struct {
	// byKind[kind][name] = entity_id (only when unique within that kind).
	byKind map[string]map[string]string
	// ambigKind[kind][name] = true when a (kind, name) tuple is ambiguous.
	ambigKind map[string]map[string]bool

	// byName[name] = entity_id (only when unique across ALL kinds). Used
	// for the kind-agnostic fallback when a stub has no "Kind:" prefix or
	// when the kind-specific lookup misses.
	byName map[string]string
	// ambigName[name] = true when a name appears in two or more entities.
	ambigName map[string]bool

	// nameKinds[name][kind] = entity_id for every entity sharing this
	// name. A blank string sentinel means two entities share that
	// (name, kind) tuple — i.e. the kind itself is ambiguous for this
	// name and the kind hint cannot disambiguate via this family.
	nameKinds map[string]map[string]string

	// byLocation[file_path][name] = entity_id, retained only when unique
	// within the file. Used by structural-ref Format A resolution.
	byLocation LocationIndex
	// ambigLocation[file_path][name] = true when (file, name) collides.
	ambigLocation map[string]map[string]bool

	// byLocationKind[file_path][name][kind] = entity_id. Kind-aware
	// (file, name) lookup. PORT-2-FIX-2 emissions can produce two entities
	// at the same (file, name) with different kinds (e.g. SCOPE.Component
	// class + SCOPE.Operation method); kind-aware lookup picks the correct
	// one when the relationship's kind hint maps to a single family.
	// A blank string sentinel marks (file, name, kind) collisions.
	byLocationKind LocationKindIndex

	// byMember[file_path][scope_name][member_name] = entity_id. Used by
	// structural-ref Format B resolution. A blank string sentinel marks
	// (scope, member) collisions inside the same file. Entities are
	// indexed by splitting their dotted Name on the first '.'.
	byMember map[string]map[string]map[string]string
}

// LocationIndex maps file_path -> name -> entity_id, retaining only entries
// that are unique within their file. Returned by BuildLocationIndex.
type LocationIndex map[string]map[string]string

// LocationKindIndex maps file_path -> name -> kind -> entity_id. Used by the
// kind-aware structural-ref / location resolver path to disambiguate
// same-file (file, name) collisions when the relationship supplies a kind
// hint. A blank string value is the ambiguous-within-kind sentinel.
type LocationKindIndex map[string]map[string]map[string]string

// Stats reports how many relationship endpoints the resolver rewrote and how
// many it left as stubs because of ambiguity / missing matches. Surfaced via
// the log line in cmd/archigraph/index.go for instrumentation.
//
// Rewritten/Ambiguous/Unmatched are aggregate counters covering every endpoint
// the resolver inspected (FromID + ToID combined). PORT-2-FIX-4 added the
// per-endpoint counters so callers can tell which side of an edge is failing
// to resolve.
type Stats struct {
	Rewritten int
	Ambiguous int
	Unmatched int

	FromRewritten int
	FromAmbiguous int
	FromUnmatched int
	ToRewritten   int
	ToAmbiguous   int
	ToUnmatched   int
}

// BuildIndex constructs a (kind, name) -> entity_id lookup from a slice of
// EntityRecords. Records whose ID field is empty are skipped — the caller is
// expected to populate ID with graph.EntityID(...) before calling BuildIndex.
//
// The returned Index handles two kind forms emitted by upstream extractors:
//
//   - Plain kind, e.g. "Function", "Class", "Model".
//   - SCOPE-prefixed kind, e.g. "SCOPE.View", "SCOPE.Service" — emitted by
//     Pass 3 cross-language extractors. The lookup strips the "SCOPE." prefix
//     so a stub like "View:User" matches an entity of kind "SCOPE.View".
func BuildIndex(entities []types.EntityRecord) Index {
	idx := Index{
		byKind:         make(map[string]map[string]string),
		ambigKind:      make(map[string]map[string]bool),
		byName:         make(map[string]string),
		ambigName:      make(map[string]bool),
		nameKinds:      make(map[string]map[string]string),
		byLocation:     make(LocationIndex),
		ambigLocation:  make(map[string]map[string]bool),
		byLocationKind: make(LocationKindIndex),
		byMember:       make(map[string]map[string]map[string]string),
	}
	for k := range entities {
		e := &entities[k]
		if e.ID == "" || e.Name == "" {
			continue
		}
		// Index under both the plain kind and the trimmed kind ("SCOPE.View"
		// → "View"), so stubs can match either form.
		kinds := []string{e.Kind}
		if trimmed := strings.TrimPrefix(e.Kind, "SCOPE."); trimmed != e.Kind && trimmed != "" {
			kinds = append(kinds, trimmed)
		}
		for _, kind := range kinds {
			if kind == "" {
				continue
			}
			if idx.ambigKind[kind] != nil && idx.ambigKind[kind][e.Name] {
				continue
			}
			bucket := idx.byKind[kind]
			if bucket == nil {
				bucket = make(map[string]string)
				idx.byKind[kind] = bucket
			}
			if existing, ok := bucket[e.Name]; ok && existing != e.ID {
				delete(bucket, e.Name)
				if idx.ambigKind[kind] == nil {
					idx.ambigKind[kind] = make(map[string]bool)
				}
				idx.ambigKind[kind][e.Name] = true
				continue
			}
			bucket[e.Name] = e.ID
		}

		// Track every (name, kind) -> id so the kind-hint fallback can
		// disambiguate when byName flips to ambiguous. The plain entity
		// kind is enough here; SCOPE.* kinds are tracked under both forms
		// to mirror the byKind dual-indexing above.
		nameKindBucket := idx.nameKinds[e.Name]
		if nameKindBucket == nil {
			nameKindBucket = make(map[string]string)
			idx.nameKinds[e.Name] = nameKindBucket
		}
		for _, kind := range kinds {
			if kind == "" {
				continue
			}
			// First writer wins per kind; if a second entity shares the
			// (name, kind) we mark the kind ambiguous for that name by
			// blanking the entry so the hint falls through.
			if existing, ok := nameKindBucket[kind]; ok && existing != e.ID {
				nameKindBucket[kind] = ""
			} else {
				nameKindBucket[kind] = e.ID
			}
		}

		// Location index — (file_path, name) -> entity_id. Same logic as
		// byKind: ambiguous (file, name) tuples are tracked separately so
		// the structural-ref resolver leaves the stub alone.
		if e.SourceFile != "" {
			// Kind-aware (file, name, kind) bucket — collision-safe under
			// PORT-2-FIX-2 emissions. Indexed under both raw and SCOPE-
			// trimmed kinds to mirror byKind.
			fileKindBucket := idx.byLocationKind[e.SourceFile]
			if fileKindBucket == nil {
				fileKindBucket = make(map[string]map[string]string)
				idx.byLocationKind[e.SourceFile] = fileKindBucket
			}
			nameKindBucketLoc := fileKindBucket[e.Name]
			if nameKindBucketLoc == nil {
				nameKindBucketLoc = make(map[string]string)
				fileKindBucket[e.Name] = nameKindBucketLoc
			}
			for _, kind := range kinds {
				if kind == "" {
					continue
				}
				if existing, ok := nameKindBucketLoc[kind]; ok && existing != e.ID {
					nameKindBucketLoc[kind] = "" // ambiguous within (file, name, kind)
				} else if !ok || existing == e.ID {
					nameKindBucketLoc[kind] = e.ID
				}
			}

			if idx.ambigLocation[e.SourceFile] == nil || !idx.ambigLocation[e.SourceFile][e.Name] {
				bucket := idx.byLocation[e.SourceFile]
				if bucket == nil {
					bucket = make(map[string]string)
					idx.byLocation[e.SourceFile] = bucket
				}
				if existing, ok := bucket[e.Name]; ok && existing != e.ID {
					delete(bucket, e.Name)
					if idx.ambigLocation[e.SourceFile] == nil {
						idx.ambigLocation[e.SourceFile] = make(map[string]bool)
					}
					idx.ambigLocation[e.SourceFile][e.Name] = true
				} else {
					bucket[e.Name] = e.ID
				}
			}

			// Member index — Format B references address a member of an
			// enclosing scope (class/module/etc.) by qualified name. Pass 3
			// records typically encode this as "<scope>.<member>" in the
			// Name field, so we split on the first '.'.
			if dot := strings.IndexByte(e.Name, '.'); dot > 0 {
				scope, member := e.Name[:dot], e.Name[dot+1:]
				fileBucket := idx.byMember[e.SourceFile]
				if fileBucket == nil {
					fileBucket = make(map[string]map[string]string)
					idx.byMember[e.SourceFile] = fileBucket
				}
				scopeBucket := fileBucket[scope]
				if scopeBucket == nil {
					scopeBucket = make(map[string]string)
					fileBucket[scope] = scopeBucket
				}
				if existing, ok := scopeBucket[member]; ok && existing != e.ID {
					scopeBucket[member] = "" // blank sentinel → ambiguous
				} else {
					scopeBucket[member] = e.ID
				}
			}
		}

		// Kind-agnostic name index. Two different entities sharing a name
		// (even across kinds) flips the name to ambiguous.
		if idx.ambigName[e.Name] {
			continue
		}
		if existing, ok := idx.byName[e.Name]; ok && existing != e.ID {
			delete(idx.byName, e.Name)
			idx.ambigName[e.Name] = true
			continue
		}
		idx.byName[e.Name] = e.ID
	}
	return idx
}

// BuildLocationIndex returns a (file_path, name) -> entity_id map built from
// the supplied entity slice. Entries that are not unique within their file
// are dropped. Exposed for callers that only need the location lookup.
func BuildLocationIndex(entities []types.EntityRecord) LocationIndex {
	loc := make(LocationIndex)
	ambig := make(map[string]map[string]bool)
	for k := range entities {
		e := &entities[k]
		if e.ID == "" || e.Name == "" || e.SourceFile == "" {
			continue
		}
		if ambig[e.SourceFile] != nil && ambig[e.SourceFile][e.Name] {
			continue
		}
		bucket := loc[e.SourceFile]
		if bucket == nil {
			bucket = make(map[string]string)
			loc[e.SourceFile] = bucket
		}
		if existing, ok := bucket[e.Name]; ok && existing != e.ID {
			delete(bucket, e.Name)
			if ambig[e.SourceFile] == nil {
				ambig[e.SourceFile] = make(map[string]bool)
			}
			ambig[e.SourceFile][e.Name] = true
			continue
		}
		bucket[e.Name] = e.ID
	}
	return loc
}

// Lookup resolves a stub string to an entity ID. The stub is split on the
// first ':' into (kind, name). If only the right-hand side is supplied (no
// ':' present) we fall back to the kind-agnostic name index.
//
// Returns (id, true) only when the lookup is unambiguous. Returns
// ("", false) when the stub has zero matches OR multiple matches — the
// caller leaves the original string in place in either case but tracks the
// outcome in Stats.
func (idx Index) Lookup(stub string) (string, bool) {
	if stub == "" {
		return "", false
	}
	kind, name := splitStub(stub)
	if kind != "" {
		if bucket, ok := idx.byKind[kind]; ok {
			if id, ok := bucket[name]; ok {
				return id, true
			}
		}
		if idx.ambigKind[kind] != nil && idx.ambigKind[kind][name] {
			// Ambiguous within this kind; fall through to kind-agnostic
			// only if the kind-agnostic name is itself unique.
		}
	}
	// Kind-agnostic fallback: bare name (no prefix) OR missed kind lookup.
	lookupName := name
	if kind == "" {
		lookupName = stub
	}
	if id, ok := idx.byName[lookupName]; ok {
		return id, true
	}
	return "", false
}

// LookupStatus reports whether a stub is unambiguous, ambiguous, or unmatched.
// Used by References to populate Stats counters without doing two passes.
func (idx Index) LookupStatus(stub string) (id string, status int) {
	return idx.LookupStatusHint(stub, "")
}

// LookupStatusHint is LookupStatus with an optional relationship-kind hint.
// The hint is the RelationshipRecord.Kind value (e.g. "EXTENDS", "CALLS"),
// not the entity kind. When the bare-name path would otherwise be ambiguous
// the hint biases the lookup toward the entity-kind family typically
// targeted by that relationship. The hint is ignored when the structural-ref
// path or an explicit Kind: prefix already resolves.
//
// When passed "" the function behaves exactly like LookupStatus.
func (idx Index) LookupStatusHint(stub, relKind string) (id string, status int) {
	const (
		statusRewritten = 1
		statusAmbiguous = 2
		statusUnmatched = 3
	)
	if stub == "" {
		return "", statusUnmatched
	}

	// Structural-ref forms (Format A / B). Recognised by the "scope:"
	// prefix and resolved through the location/member indexes — bypasses
	// the kind / name path entirely.
	if id, st, handled := idx.lookupStructural(stub); handled {
		return id, st
	}

	kind, name := splitStub(stub)
	if kind != "" {
		if bucket, ok := idx.byKind[kind]; ok {
			if id, ok := bucket[name]; ok {
				return id, statusRewritten
			}
		}
		if idx.ambigKind[kind] != nil && idx.ambigKind[kind][name] {
			return "", statusAmbiguous
		}
	}
	lookupName := name
	if kind == "" {
		lookupName = stub
	}
	if id, ok := idx.byName[lookupName]; ok {
		return id, statusRewritten
	}
	if idx.ambigName[lookupName] {
		// Ambiguous bare-name. Try the kind hint: pick a family that
		// the relKind biases toward, and if exactly one entity with this
		// name lives in that family, resolve to it.
		if id, ok := idx.lookupByKindHint(lookupName, relKind); ok {
			return id, statusRewritten
		}
		return "", statusAmbiguous
	}
	return "", statusUnmatched
}

// hintKinds returns the entity-kind families preferred for a given
// relationship kind. EXTENDS / IMPLEMENTS prefer Component-shaped kinds;
// CALLS prefers Operation-shaped kinds. Everything else returns nil.
func hintKinds(relKind string) []string {
	switch strings.ToUpper(relKind) {
	case "EXTENDS", "IMPLEMENTS":
		return []string{"Component", "Class", "View", "Model", "SCOPE.Component", "SCOPE.View", "SCOPE.Model"}
	case "CALLS":
		return []string{"Operation", "Function", "Method", "SCOPE.Operation"}
	}
	return nil
}

// lookupByKindHint disambiguates a name using the relKind hint. Returns
// (id, true) only when exactly one entity in the hinted family has this
// name; otherwise ("", false).
func (idx Index) lookupByKindHint(name, relKind string) (string, bool) {
	families := hintKinds(relKind)
	if len(families) == 0 {
		return "", false
	}
	bucket := idx.nameKinds[name]
	if len(bucket) == 0 {
		return "", false
	}
	var match string
	for _, k := range families {
		id := bucket[k]
		if id == "" {
			continue
		}
		if match != "" && match != id {
			// Two distinct entities in the hinted family share this
			// name — hint cannot disambiguate.
			return "", false
		}
		match = id
	}
	if match == "" {
		return "", false
	}
	return match, true
}

// lookupStructural resolves Format A / B references. Returns handled=false
// when the stub doesn't start with "scope:" so the caller falls through to
// the normal Kind:Name / bare-name path.
//
// Format A: scope:<kind>:<subtype>:<lang>:<file_path>:<name>
// Format B: scope:<kind>:<subtype>:<lang>:<file_path>:<scope_name>#<member_name>
func (idx Index) lookupStructural(stub string) (id string, status int, handled bool) {
	const (
		statusRewritten = 1
		statusAmbiguous = 2
		statusUnmatched = 3
	)
	if !strings.HasPrefix(stub, "scope:") {
		return "", 0, false
	}
	parts := strings.SplitN(stub, ":", 6)
	if len(parts) != 6 {
		return "", statusUnmatched, true
	}
	scopeKind := parts[1] // e.g. "component", "operation"
	filePath := parts[4]
	tail := parts[5]
	if filePath == "" || tail == "" {
		return "", statusUnmatched, true
	}

	// Format B: tail contains "#" → (scope_name, member_name).
	if hash := strings.IndexByte(tail, '#'); hash >= 0 {
		scopeName, memberName := tail[:hash], tail[hash+1:]
		if scopeName == "" || memberName == "" {
			return "", statusUnmatched, true
		}
		fileBucket := idx.byMember[filePath]
		if fileBucket == nil {
			return "", statusUnmatched, true
		}
		scopeBucket := fileBucket[scopeName]
		if scopeBucket == nil {
			return "", statusUnmatched, true
		}
		if id, ok := scopeBucket[memberName]; ok {
			if id == "" {
				return "", statusAmbiguous, true
			}
			return id, statusRewritten, true
		}
		return "", statusUnmatched, true
	}

	// Format A: tail is the entity name. Try the kind-aware location
	// index first using the structural-ref's scope-kind segment; this
	// resolves PORT-2-FIX-2 same-file collisions.
	if id, ok := idx.lookupLocationKind(filePath, tail, structuralKindFamilies(scopeKind)); ok {
		return id, statusRewritten, true
	}
	if idx.ambigLocation[filePath] != nil && idx.ambigLocation[filePath][tail] {
		return "", statusAmbiguous, true
	}
	if bucket, ok := idx.byLocation[filePath]; ok {
		if id, ok := bucket[tail]; ok {
			return id, statusRewritten, true
		}
	}
	return "", statusUnmatched, true
}

// structuralKindFamilies maps a scope-kind segment from a structural ref
// (e.g. "component", "operation") to the entity-kind families it might be
// indexed under. Returns nil for unknown segments.
func structuralKindFamilies(scopeKind string) []string {
	switch strings.ToLower(scopeKind) {
	case "component":
		return []string{"Component", "Class", "View", "Model", "SCOPE.Component", "SCOPE.View", "SCOPE.Model"}
	case "operation":
		return []string{"Operation", "Function", "Method", "SCOPE.Operation"}
	}
	return nil
}

// lookupLocationKind picks an entity by (file, name) constrained to the
// supplied kind families. Returns (id, true) only when exactly one family
// resolves to a non-blank entity ID for this (file, name).
func (idx Index) lookupLocationKind(filePath, name string, families []string) (string, bool) {
	if len(families) == 0 {
		return "", false
	}
	fileBucket := idx.byLocationKind[filePath]
	if fileBucket == nil {
		return "", false
	}
	nameBucket := fileBucket[name]
	if len(nameBucket) == 0 {
		return "", false
	}
	var match string
	for _, k := range families {
		id := nameBucket[k]
		if id == "" {
			continue
		}
		if match != "" && match != id {
			return "", false
		}
		match = id
	}
	if match == "" {
		return "", false
	}
	return match, true
}

// splitStub splits a stub string on the first ':' into (kind, name). If no
// ':' is present the full string is returned as the name and kind is empty.
func splitStub(s string) (kind, name string) {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// rewriteOne resolves a single endpoint reference. It returns the (possibly
// rewritten) ID string and the status code from LookupStatusHint. Hex IDs
// and empty strings short-circuit with a zero status, signalling "skip".
func (idx Index) rewriteOne(ref, relKind string) (string, int) {
	if ref == "" || isHexID(ref) {
		return ref, 0
	}
	id, st := idx.LookupStatusHint(ref, relKind)
	if st == 1 { // statusRewritten
		return id, st
	}
	return ref, st
}

// applyEndpointStats records a single endpoint's outcome into the Stats
// counters, updating both the per-endpoint totals and the aggregate ones.
func applyEndpointStats(stats *Stats, status int, isFrom bool) {
	const (
		statusRewritten = 1
		statusAmbiguous = 2
		statusUnmatched = 3
	)
	switch status {
	case statusRewritten:
		stats.Rewritten++
		if isFrom {
			stats.FromRewritten++
		} else {
			stats.ToRewritten++
		}
	case statusAmbiguous:
		stats.Ambiguous++
		if isFrom {
			stats.FromAmbiguous++
		} else {
			stats.ToAmbiguous++
		}
	case statusUnmatched:
		stats.Unmatched++
		if isFrom {
			stats.FromUnmatched++
		} else {
			stats.ToUnmatched++
		}
	}
}

// References rewrites ToID and FromID values in rels in place. It returns
// per-endpoint stats — one rel with both endpoints rewritten counts twice in
// Stats.Rewritten (once per endpoint). The 16-char hex IDs already present
// (matching the shape of graph.EntityID output) are left untouched.
func References(rels []types.RelationshipRecord, idx Index) Stats {
	var stats Stats
	for k := range rels {
		r := &rels[k]
		if newID, st := idx.rewriteOne(r.FromID, r.Kind); st != 0 {
			r.FromID = newID
			applyEndpointStats(&stats, st, true)
		}
		if newID, st := idx.rewriteOne(r.ToID, r.Kind); st != 0 {
			r.ToID = newID
			applyEndpointStats(&stats, st, false)
		}
	}
	return stats
}

// ReferencesEmbedded walks every EntityRecord's embedded Relationships slice
// and applies the same resolver. Pass 1 extractors emit cross-file CALLS
// edges as embedded relationships, so this is where most of the rewriting
// happens on real codebases.
//
// PORT-2-FIX-4 extends this function to rewrite FromID in addition to ToID.
// Pass 3 cross-language extractors increasingly emit edges where the source
// endpoint is itself a stub (e.g. structural-ref Format A targeting an
// entity in another file). When FromID is empty the caller is still
// expected to substitute the parent entity ID at edge-emission time.
func ReferencesEmbedded(records []types.EntityRecord, idx Index) Stats {
	var stats Stats
	for k := range records {
		rels := records[k].Relationships
		for j := range rels {
			r := &rels[j]
			if newID, st := idx.rewriteOne(r.FromID, r.Kind); st != 0 {
				r.FromID = newID
				applyEndpointStats(&stats, st, true)
			}
			if newID, st := idx.rewriteOne(r.ToID, r.Kind); st != 0 {
				r.ToID = newID
				applyEndpointStats(&stats, st, false)
			}
		}
	}
	return stats
}

// isHexID reports whether s is a 16-char lower-hex string — the shape of
// graph.EntityID() output. Anything matching this shape is assumed to be an
// already-resolved entity ID and is left untouched.
func isHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
