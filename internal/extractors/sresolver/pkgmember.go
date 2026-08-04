package sresolver

import (
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// receiverTypeProp is the extractor-supplied property that names the receiver
// a method-dispatch CALLS edge was invoked on. Stamped by the Go extractor
// only (internal/resolve/refs.go:1206), and dropped by
// golang/extractor.go:509 when the extractor is unsure — so an absent stamp
// means "no receiver information", never "no receiver".
const receiverTypeProp = "receiver_type"

// pkgDirOf returns the directory portion of a slash-normalised source file
// path, used as the package key for the member tiers. Mirrors
// internal/resolve/refs.go:126 exactly, including its edge cases: a path with
// no separator (file in repo root) returns "." so a caller in the root
// package still hits a non-empty bucket, a leading-slash path returns "/",
// and an empty input returns "".
//
// It is duplicated rather than imported because internal/resolve is a heavy
// package whose test suite imports internal/extractors; this package is
// deliberately kept light (see the package doc).
func pkgDirOf(p string) string {
	if p == "" {
		return ""
	}
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		if i == 0 {
			return "/"
		}
		return p[:i]
	}
	return "."
}

// normalizeSlashes puts a source path into the slash form pkgDirOf expects.
func normalizeSlashes(p string) string {
	if strings.IndexByte(p, '\\') >= 0 {
		p = strings.ReplaceAll(p, "\\", "/")
	}
	return strings.TrimPrefix(p, "./")
}

// ambiguous is the blank-string sentinel stored in every index below when two
// DISTINCT entities claim one key. The corpus-wide resolver leaves such a stub
// verbatim rather than picking a candidate, and so do we — see
// memberIndexes.lookupReceiver for why collapsing that into a plain miss would be
// actively harmful here.
const ambiguous = ""

// memberIndexes is the scoped path's port of the member tiers the corpus-wide
// resolver reaches for a bare-name CALLS/dispatch stub that its global
// name index cannot bind.
//
// # The three ported tiers, with their upstream sites
//
//  1. byPackageMember[pkgDir][receiverType][member] — issue #148 / #364,
//     internal/resolve/refs.go:5684. Fires when the edge carries
//     Properties["receiver_type"]. The most PRECISE tier: the receiver stamp
//     names the scope, so no cross-scope guessing is involved.
//
//  2. leafByFile[file][member] — issue #778,
//     internal/resolve/refs.go:lookupMemberByLeafName (probed from
//     rewriteOneWithCaller at refs.go:2963). A bare CALLS stub binds to a
//     member of ANY scope declared in the caller's own file, provided exactly
//     one scope declares it.
//
//  3. leafByPkg[pkgDir][member] — issue #778,
//     internal/resolve/refs.go:lookupPackageMemberByLeafName (refs.go:2969).
//     Same, widened to the caller's package directory. This is the tier that
//     actually binds the #6090-residual shape: `a.Do11(x)` inside a free
//     function, where the callee entity is `T11.Do11` in a SIBLING file.
//
// The scoped ladder previously had none of them. Its nameToID tier is keyed on
// the WHOLE entity name (`T11.Do11`, never `Do11`) and its byLocation tier on
// the EXACT source file, so a same-package/different-file member was
// unreachable — which is #6098's root cause and #6090's residual.
//
// # Memory — why this is not the corpus-sized index the scoped path avoids
//
// The corpus-wide resolver indexes every dotted-name entity in the corpus.
// Doing that here would reintroduce exactly the peak the scoped path exists to
// avoid (epic #5954). Instead the index is built ONLY over the package
// directories of the freshly extracted files — the changed-file delta. Every
// other entity is scanned (one pointer-walk, no allocation) and discarded.
//
// So the retained size is O(entities in the changed files' packages), which is
// a property of the edit, not of the repository. On a one-file edit to a
// 20-file package that is a few hundred map entries; the pre-existing nameToID
// and byLocation maps in ResolveScoped, which ARE built over every surviving
// entity, dominate it by orders of magnitude. Measured in
// pkgmember_memory_test.go.
//
// A caller outside those directories simply does not get the tiers, which is
// the status quo for it — a surviving edge left unresolved by the previous
// build was already unresolvable by the corpus-wide resolver that produced it.
type memberIndexes struct {
	byPackageMember map[string]map[string]map[string]string // [dir][scope][member]
	leafByFile      map[string]map[string]string            // [file][member]
	leafByPkg       map[string]map[string]string            // [dir][member]

	// callerLoc maps an entity ID to its location. It is probed with the
	// edge's RESOLVED FromID, which is why ResolveScoped must rewrite the
	// from-side of an edge before the to-side: the Go extractor emits the
	// from-side as a Format A structural stub
	// (`scope:operation:method:go:r07.go:Local07`), not as an entity ID, so
	// keying this on the raw FromID finds nothing and every tier silently
	// misses. That was a real defect during development — the tiers were
	// correct and never fired.
	callerLoc map[string]callerLocation
}

type callerLocation struct {
	file string
	dir  string
}

// relWantsMemberTier reports whether r is a candidate for any ported tier: an
// unresolved (non-hex) ToID that is either receiver-stamped or CALLS-shaped.
// Format A structural stubs are excluded — they carry colons and are the
// existing ladder's business.
func relWantsMemberTier(r *graph.Relationship) bool {
	if r.ToID == "" || isHexID(r.ToID) || strings.IndexByte(r.ToID, ':') >= 0 {
		return false
	}
	return strings.EqualFold(r.Kind, "CALLS") || r.PropGet(receiverTypeProp) != ""
}

// buildMemberIndexes constructs the tiers over newEntities ∪ existingEntities,
// restricted to the package directories of newEntities (the changed files).
// Returns nil — retaining nothing — when the delta has no package directory.
func buildMemberIndexes(newEntities, existingEntities []graph.Entity) *memberIndexes {
	// The probe set: the packages the changed files live in. This is the
	// memory bound, and it is deliberately derived from the DELTA.
	probeDirs := map[string]bool{}
	for i := range newEntities {
		if dir := pkgDirOf(normalizeSlashes(newEntities[i].SourceFile)); dir != "" {
			probeDirs[dir] = true
		}
	}
	if len(probeDirs) == 0 {
		return nil
	}

	idx := &memberIndexes{
		byPackageMember: map[string]map[string]map[string]string{},
		leafByFile:      map[string]map[string]string{},
		leafByPkg:       map[string]map[string]string{},
		callerLoc:       map[string]callerLocation{},
	}

	put := func(bucket map[string]string, key, id string) {
		if existing, ok := bucket[key]; ok && existing != id {
			bucket[key] = ambiguous
			return
		}
		bucket[key] = id
	}

	scan := func(ents []graph.Entity) {
		for i := range ents {
			e := &ents[i]
			file := normalizeSlashes(e.SourceFile)
			dir := pkgDirOf(file)
			// Everything retained is gated on the probe set, which is what
			// keeps this index delta-sized rather than corpus-sized.
			if dir == "" || !probeDirs[dir] {
				continue
			}
			idx.callerLoc[e.ID] = callerLocation{file: file, dir: dir}

			dot := strings.LastIndexByte(e.Name, '.')
			if dot <= 0 || dot >= len(e.Name)-1 {
				continue
			}
			scope, member := e.Name[:dot], e.Name[dot+1:]

			pkgBucket := idx.byPackageMember[dir]
			if pkgBucket == nil {
				pkgBucket = map[string]map[string]string{}
				idx.byPackageMember[dir] = pkgBucket
			}
			scopeBucket := pkgBucket[scope]
			if scopeBucket == nil {
				scopeBucket = map[string]string{}
				pkgBucket[scope] = scopeBucket
			}
			put(scopeBucket, member, e.ID)

			fileBucket := idx.leafByFile[file]
			if fileBucket == nil {
				fileBucket = map[string]string{}
				idx.leafByFile[file] = fileBucket
			}
			put(fileBucket, member, e.ID)

			pkgLeaf := idx.leafByPkg[dir]
			if pkgLeaf == nil {
				pkgLeaf = map[string]string{}
				idx.leafByPkg[dir] = pkgLeaf
			}
			put(pkgLeaf, member, e.ID)
		}
	}
	scan(existingEntities)
	scan(newEntities)
	return idx
}

// lookup probes the ported tiers for one edge. callerEndpoint is the edge's
// RAW (pre-resolution) FromID.
//
// The return contract has THREE outcomes, mirroring
// internal/resolve/refs.go:lookupPackageMember:
//
//	(id, true)  — unambiguous hit; bind.
//	("", true)  — AMBIGUOUS. Handled, but not bound. The caller must leave the
//	              stub verbatim and must NOT continue down the ladder.
//	("", false) — no entry; the caller falls through to the next tier.
//
// The ("", true) case is the soundness guard and it is not cosmetic. The
// scoped ladder's whole-string tier is LAST-WRITER-WINS with no ambiguity
// sentinel of its own, so collapsing ambiguity into ("", false) would turn a
// correctly-unresolved stub into a CONFIDENT WRONG binding the moment some
// unrelated entity happens to be named after the member. A wrong edge is worse
// than the stub it replaces — that is the whole reason #6098 was left open
// rather than half-fixed.
// It is split into two entry points because the two halves sit on OPPOSITE
// sides of the scoped ladder's whole-string tier, mirroring where the
// corpus-wide resolver probes each:
//
//	lookupReceiver — BEFORE (refs.go:5684 runs ahead of rewriteOneWithCaller)
//	lookupLeaf     — AFTER  (refs.go:2963/2969 are fallbacks inside it)
//
// Getting that order wrong in either direction changes which binding wins for
// a name that several tiers can see, so it is pinned by tests rather than
// left to the reader.
func (idx *memberIndexes) lookupReceiver(r *graph.Relationship, callerEndpoint string) (string, bool) {
	if idx == nil || callerEndpoint == "" || !relWantsMemberTier(r) {
		return "", false
	}
	loc := idx.callerLoc[callerEndpoint]
	if loc.dir == "" {
		return "", false
	}
	member := r.ToID

	// Tier 1 — receiver-stamped dispatch (#148 / #364). Most precise, so
	// first, exactly as internal/resolve/refs.go orders it.
	if recv := r.PropGet(receiverTypeProp); recv != "" {
		if pkgBucket := idx.byPackageMember[loc.dir]; pkgBucket != nil {
			// #148 baseline: the stamped type as-is. #364 follow-up: when the
			// stamp is package-qualified (`chi.Mux`), strip the package
			// segment and retry, because entities are emitted under their
			// bare receiver name. As-is FIRST so a type whose name genuinely
			// contains a dot still wins.
			tryTypes := [2]string{recv, ""}
			n := 1
			if dot := strings.LastIndexByte(recv, '.'); dot >= 0 && dot < len(recv)-1 {
				tryTypes[1] = recv[dot+1:]
				n = 2
			}
			for i := 0; i < n; i++ {
				scopeBucket := pkgBucket[tryTypes[i]]
				if scopeBucket == nil {
					continue
				}
				id, ok := scopeBucket[member]
				if !ok {
					continue
				}
				if id == ambiguous {
					// Do NOT retry the stripped form, do NOT fall through.
					return "", true
				}
				return id, true
			}
		}
	}

	return "", false
}

// lookupLeaf probes tiers 2 and 3. Same three-outcome contract as
// lookupReceiver.
func (idx *memberIndexes) lookupLeaf(r *graph.Relationship, callerEndpoint string) (string, bool) {
	if idx == nil || callerEndpoint == "" || !relWantsMemberTier(r) {
		return "", false
	}
	loc := idx.callerLoc[callerEndpoint]
	if loc.dir == "" {
		return "", false
	}
	member := r.ToID

	// Tiers 2 and 3 are CALLS-only (#778): a bare call name binding to a
	// member of some scope is only the natural shape for a call edge. The
	// same gate is what stops a DEPENDS_ON from catching a same-named method.
	if !strings.EqualFold(r.Kind, "CALLS") {
		return "", false
	}
	// Tier 2 — same file, any scope.
	if id, ok := idx.leafByFile[loc.file][member]; ok {
		if id == ambiguous {
			return "", true
		}
		return id, true
	}
	// Tier 3 — same package directory, any scope. The #6090-residual tier.
	if id, ok := idx.leafByPkg[loc.dir][member]; ok {
		if id == ambiguous {
			return "", true
		}
		return id, true
	}
	return "", false
}
