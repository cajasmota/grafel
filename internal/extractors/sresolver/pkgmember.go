package sresolver

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// receiverTypeProp is the extractor-supplied property that names the receiver
// a method-dispatch CALLS edge was invoked on. Stamped by six extractors at the
// nine sites enumerated in buildMemberIndexes below, and dropped when the
// extractor is unsure, as the Go one does at golang/extractor.go:509 — so an
// absent stamp means "no receiver information", never "no receiver".
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
//     Same, widened to the caller's package directory.
//
// The scoped ladder previously had none of them. Its nameToID tier is keyed on
// the WHOLE entity name (`T11.Do11`, never `Do11`) and its byLocation tier on
// the EXACT source file, so a same-package/different-file member was
// unreachable — which is #6098's root cause and #6090's residual.
//
// # Tier 1 and tiers 2-3 are REDUNDANT on the integration fixture
//
// Stated because an earlier version of this comment claimed the opposite.
// Ablated tier-by-tier against
// cmd/grafel:TestIncremental_NewlyIntroducedCrossFileCall_6098:
//
//	leaf tiers removed, receiver tier only  → PASSES
//	receiver tier removed, leaf tiers only  → PASSES
//	both removed                            → FAILS 3/3 (0 resolved, want 2)
//
// The Go extractor DOES stamp receiver_type for `a := &T11{...}; a.Do11(x)`,
// so tier 1 alone binds that fixture — #6098's stated root cause was
// sufficient for it. Tiers 2-3 are carried for LADDER PARITY with refs.go, not
// because this fixture needs them; they are exercised by the unit tests in
// scoped_member_6098_test.go only.
//
// A shape that separates them was looked for and not found within the time
// box. The nearest candidate — a chained call `New23().Do23(x)`, which the
// extractor emits as an unstamped Format A stub — is not one: the corpus-wide
// resolver leaves it unresolved too, so there is nothing to reach parity with.
//
// The general lesson, recorded because it cost a wrong published claim:
// "measured end-to-end" must mean tier-by-tier ablation, not "the whole change
// makes the test pass".
//
// # Memory — why this is not the corpus-sized index the scoped path avoids
//
// The corpus-wide resolver indexes every dotted-name entity in the corpus.
// Doing that here would reintroduce exactly the peak the scoped path exists to
// avoid (epic #5954). Instead the index is built ONLY over the package
// directories of the freshly extracted files — the changed-file delta. Every
// other entity is scanned (one pointer-walk, no allocation) and discarded.
//
// So the retained size is O(entities in the PACKAGES THE EDIT TOUCHES) — not
// of the repository, but not of the edit alone either: probeDirs is keyed on
// directories, so a delta spanning K packages costs K packages' worth of
// entities regardless of how few files changed in each. On a one-file edit to
// a 20-file package that is a few hundred map entries; the pre-existing
// nameToID and byLocation maps in ResolveScoped, which ARE built over every
// surviving entity, dominate it by orders of magnitude. Measured in
// pkgmember_memory_test.go.
//
// Precision note for future work here: what ResolveScoped already holds is ONE
// REPOSITORY's entity set. Epic #5954's peak concerns the multi-repo GROUP
// corpus, which this path genuinely never materialises. "The scoped path
// already holds the corpus" is true only at repo granularity.
//
// A caller outside those directories simply does not get the tiers, which is
// the status quo for it — a surviving edge left unresolved by the previous
// build was already unresolvable by the corpus-wide resolver that produced it.
type memberIndexes struct {
	byPackageMember map[string]map[string]map[string]string // [dir][scope][member]
	leafByFile      map[string]map[string]string            // [file][member]
	leafByPkg       map[string]map[string]string            // [dir][member]

	// #6141 — operation-only twins of the two leaf indexes, probed FIRST.
	// The leaf tiers do not know the scope they are binding into, so an
	// unrelated same-leaf-named FIELD used to enter the candidate set and
	// trip the ambiguity guard, destroying a correct binding to a real
	// operation. Preferring operations removes the field from that
	// contest. They are a PREFERENCE, not a filter: the kind-blind twins
	// above are still consulted when no operation matches, because Ruby
	// (attr_accessor) and JS/TS (`handler = function(){}`) model genuinely
	// invocable members as SCOPE.Schema fields and the leaf tier is their
	// only binding route. See scanLeafMembersPreferring in
	// internal/resolve/refs.go for the measurement that established this.
	leafByFileOp map[string]map[string]string // [file][member], operations only
	leafByPkgOp  map[string]map[string]string // [dir][member],  operations only

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
//
// Divergence, verified inert today: refs.go:2875 gates the equivalent bare
// lookup on !strings.ContainsAny(ref, ":.#"), i.e. it also rejects a DOT and
// the Format B member delimiter. We reject only ':'. That is currently
// harmless because every index key here is built from the segment AFTER the
// last dot, so a dotted stub cannot spuriously match a member key. It goes
// live the moment key construction changes — e.g. if a scope were ever
// indexed under its dotted form. Left divergent rather than aligned so the
// gate keeps stating the condition this code actually depends on, but the
// coupling is written down here.
// isOperationKind reports whether an entity Kind is call-target shaped —
// the scoped port of internal/resolve/refs.go's operationKindFamily
// (issue #6141). Both the raw kind and its SCOPE.-trimmed form are
// consulted, mirroring the dual-indexing the corpus-wide resolver applies,
// so a hypothetical "SCOPE.Method" still classifies as an operation.
//
// Kept as a literal list rather than importing internal/resolve because
// that package depends on the extractor types and importing it here would
// close a cycle. The list is pinned against drift by
// TestResolveScoped_LeafTier_OperationKindsStillBind_6141, which walks the
// same four kinds the resolve-side test walks.
//
// #6459/#6492 — this port deliberately does NOT admit "Service". The
// resolve side admits SCOPE.Service only through lookupProtoServiceTier, an
// ordered fallback for Format A structural refs carrying the proto language
// segment; relWantsMemberTier above EXCLUDES Format A stubs from every tier
// this predicate gates, so no ref that could carry the proto admission ever
// reaches here. The bare-name package tiers this does
// gate are Kotlin- and Go-shaped, where a "Service"-kinded entity is a
// framework marker sharing a name with a real declaration, not a call
// target.
func isOperationKind(kind string) bool {
	switch strings.TrimPrefix(kind, "SCOPE.") {
	case "Operation", "Function", "Method":
		return true
	}
	return false
}

// Whole word, so `publicKey` or a type named `PublicFoo` is not the keyword.
var publicKeywordRE = regexp.MustCompile(`\bpublic\b`)

// uncallableSolidityField is the scoped port of
// internal/resolve/refs.go:uncallableSolidityField (issue #6177): Solidity
// synthesises a getter for a state variable IF AND ONLY IF it is declared
// `public`, so a CALLS edge to a non-public one describes no call in the
// source. See that function for why the verdict is read out of Signature text,
// why ("solidity", "SCOPE.Schema") names the state variable exactly, and why an
// empty Signature must fail OPEN.
//
// Duplicated rather than imported for the same reason pkgDirOf is: internal/
// resolve's own in-package tests import internal/extractors, so an import here
// closes a cycle in that package's test binary (`go build` alone stays happy —
// `go vet ./internal/resolve/...` is what reports it). Pinned against drift by
// the cross-resolver agreement gate in resolver_agreement_6141_test.go, which
// drives BOTH resolvers over one entity set and fails when they bind the same
// stub differently.
func uncallableSolidityField(e *graph.Entity) bool {
	if e.Language != "solidity" || e.Kind != "SCOPE.Schema" || e.Signature == "" {
		return false
	}
	return !publicKeywordRE.MatchString(e.Signature)
}

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
		leafByFileOp:    map[string]map[string]string{},
		leafByPkgOp:     map[string]map[string]string{},
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

			// #6177 — a member no call can reach is withheld from BOTH leaf
			// indexes rather than merely losing the operation preference,
			// because a preference falls through (see leafByFileOp) and
			// eligibility must not. Withholding is the STRONGER form of the
			// per-candidate skip in internal/resolve's scanLeafMembers: the
			// member cannot bind, and it cannot make an eligible sibling
			// ambiguous via put's sentinel either. internal/resolve pays for the
			// sentinel it does build with Index.eligibleMember, which is what
			// keeps the two paths' answers identical (#6177's own divergence:
			// two sibling files declaring the same contract name).
			//
			// Scoping to CALLS comes for free: leafByFile and leafByPkg are
			// read ONLY by lookupLeaf, which is CALLS-gated. The receiver
			// tier's byPackageMember above keeps the entity, and that is safe
			// because NO Solidity extractor stamps receiver_type: all 9 sites
			// that stamp it on a relationship are golang/extractor.go:1437,
			// :1440, :1469, :1472, crystal/extractor.go:498, php/php.go:827,
			// scala/scala.go:532, swift/swift.go:423 and groovy/groovy.go:801,
			// so a Solidity CALLS edge never reaches that tier and its
			// precision comes from the receiver anyway.
			if uncallableSolidityField(e) {
				continue
			}

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

			// #6141 — operation-only twins, probed ahead of the two above.
			// A field never enters these, so it can no longer trip the
			// ambiguity guard against a real operation.
			if !isOperationKind(e.Kind) {
				continue
			}
			fileOp := idx.leafByFileOp[file]
			if fileOp == nil {
				fileOp = map[string]string{}
				idx.leafByFileOp[file] = fileOp
			}
			put(fileOp, member, e.ID)

			pkgOp := idx.leafByPkgOp[dir]
			if pkgOp == nil {
				pkgOp = map[string]string{}
				idx.leafByPkgOp[dir] = pkgOp
			}
			put(pkgOp, member, e.ID)
		}
	}
	scan(existingEntities)
	scan(newEntities)
	return idx
}

// lookup probes the ported tiers for one edge. callerEndpoint is the edge's
// RAW (pre-resolution) FromID.
//
// The return contract has THREE outcomes. The SIGNATURE mirrors
// internal/resolve/refs.go:lookupPackageMember; the LADDER BEHAVIOUR
// deliberately does not — see the asymmetry note below.
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
//
// # Deliberate asymmetry with refs.go on ambiguity — NOT parity
//
// The corpus-wide resolver does the OPPOSITE. At refs.go:5681-5697 an
// ambiguous (pkg, recv, member) `break`s with resolved=false, so control
// falls into the Go component tier and then rewriteOneWithCaller → the global
// name index, WHICH CAN AND DOES BIND. The inline comment there ("fall through
// to record as unmatched (preserve the stub)") is wrong about its own code —
// the same misconception this port had to fix on the scoped side.
//
// Concretely: for an ambiguous (svc, T11, Do11) plus a single global entity
// named `Do11` elsewhere, a FULL REBUILD binds the edge and this resolver
// leaves a stub. That is a divergence in the #6090 loss direction, and
// TestResolveScoped_ReceiverTypeTier_AmbiguityDoesNotFallThrough asserts our
// behaviour, not refs.go's.
//
// We keep our behaviour because refs.go's is unsound HERE: its global tier
// carries an ambiguity sentinel, ours does not, so falling through would bind
// arbitrarily rather than bind-or-refuse. Closing the divergence properly
// means fixing the refs.go side, which is filed separately. Until then this is
// a known, deliberate asymmetry — do not "restore parity" by deleting the
// guard.
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
//
// # What was NOT ported, and the gap it leaves
//
// These two are the tail of refs.go:lookupBareWithLocality (refs.go:2909),
// which is a four-step ladder: byLocationKind[callerFile] →
// byPackageOperation[callerPkgDir] → the two leaf lookups. Only the leaf
// lookups are ported here, and only on an outright MISS — refs.go enters
// lookupBareWithLocality on statusAmbiguous as well as statusUnmatched.
//
// The live gap: when a bare name is GLOBALLY AMBIGUOUS, refs.go rescues it
// locally via byPackageOperation, whereas the scoped ladder's nameToID is
// last-writer-wins and binds arbitrarily — possibly cross-package — so
// control never reaches here at all. That is pre-existing scoped-path
// unsoundness, not something this change introduced, but it now sits directly
// beneath tiers that claim refs.go parity, so it is named rather than left
// implicit. Closing it requires giving nameToID an ambiguity sentinel first;
// doing that here would change the binding of every existing bare stub and is
// out of scope for #6098.
//
// # The ("", true) refusals below are INERT TODAY
//
// Reverting either leaf tier's ambiguity to ("", false) does not change any
// observable behaviour: lookupLeaf is the LAST rung of resolveToID, so a
// refusal and a miss are indistinguishable to the caller, and tier-2
// ambiguity implies tier-3 ambiguity. No guard is missing and no test is
// absent — the mutants are genuine no-ops.
//
// They become load-bearing AND unpinned the instant a tier is appended after
// lookupLeaf in resolveToID. If you add one, add the counter-test too; the
// receiver tier's TestResolveScoped_ReceiverTypeTier_AmbiguityDoesNotFallThrough
// is the shape to copy.
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
	// #6141 — the operation preference is applied WITHIN each tier, never
	// ACROSS them: (fileOp, fileAny) and only then (pkgOp, pkgAny).
	//
	// The distinction is load-bearing and was got wrong first time round.
	// Probing both operation indexes up front — fileOp, pkgOp, fileAny,
	// pkgAny — reaches into a SIBLING FILE for an operation instead of
	// binding a same-leaf-named member in the caller's own file, which is
	// the opposite of what internal/resolve does and of what every
	// surrounding tier does (locality first). Measured on a randomized
	// differential: the across-tiers order changed 6,616 of ~200,000
	// bindings; the within-tier order changes 0 and keeps all the gains.
	//
	// This resolver exists to agree with internal/resolve on the same
	// source — divergence here means a full rebuild and an incremental
	// build produce different graphs, reachable in ordinary code (a Ruby
	// `attr_accessor :owner` beside a sibling class's `def owner`).
	// Pinned by TestResolveScoped_LeafTier_PreferenceIsWithinTierNotAcrossTiers_6141
	// and its twin in internal/resolve.
	//
	// An ambiguous operation hit still refuses (returns handled) rather
	// than falling through to the kind-blind twin: two real operations
	// contesting the name is not made resolvable by adding fields to the
	// contest.

	// Tier 2 — same file: operations first, then any scope.
	if id, ok := idx.leafByFileOp[loc.file][member]; ok {
		if id == ambiguous {
			return "", true
		}
		return id, true
	}
	if id, ok := idx.leafByFile[loc.file][member]; ok {
		if id == ambiguous {
			return "", true
		}
		return id, true
	}
	// Tier 3 — same package directory. The #6090-residual tier.
	if id, ok := idx.leafByPkgOp[loc.dir][member]; ok {
		if id == ambiguous {
			return "", true
		}
		return id, true
	}
	if id, ok := idx.leafByPkg[loc.dir][member]; ok {
		if id == ambiguous {
			return "", true
		}
		return id, true
	}
	return "", false
}
