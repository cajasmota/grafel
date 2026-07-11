package engine

// precision_dedup.go — post-assembly precision pass (issue #3729, epic #3628
// area #24). Reduces two classes of over-extraction surfaced by the acme
// graph-quality bench:
//
//  1. Multi-kind double-emit — one source symbol emitted as several kind-tagged
//     nodes for the same (Name, SourceFile, StartLine). Because every framework's
//     YAML rules share a language key (every Python rule-set fires on every
//     Python file), catch-all source_patterns can re-emit a class/function that a
//     more specific rule already emitted under a richer kind. The phantom node
//     inflates counts and (usually) carries no edges. We collapse the group to
//     the single most-specific canonical kind via a deterministic priority,
//     merging properties and REWRITING any edge endpoints that referenced the
//     dropped kind so no relationship is lost.
//
//  2. Statement-level noise — `Operation` entities whose Name is not a valid
//     identifier (e.g. the langchain `@tool` source_pattern emits an Operation
//     literally named "@tool" via name_group: 0). These are statement /
//     decorator fragments, not operations, and are dropped.
//
// CONSERVATIVE BY DESIGN:
//   - Multi-kind collapse only fires when a group genuinely contains >1 distinct
//     kind for the exact same (Name, SourceFile, StartLine); single-kind groups
//     and any group with no priority-ranked kind are left untouched.
//   - The statement-noise filter only ever drops `Operation`-kind entities, and
//     only when the Name is unambiguously non-identifier-shaped. A real
//     identifier-named operation is always kept, even if it carries no edges.
//
// The pass is opt-out-able via PrecisionDedupEnabled (default true).

import (
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// PrecisionDedupEnabled is the master switch for the precision pass. Default
// true. Set to false (e.g. in a test or via a future config flag) to skip both
// the multi-kind collapse and the statement-noise filter and emit the raw
// rule-level entity set.
var PrecisionDedupEnabled = true

// genericKinds is the closed set of low-specificity STRUCTURAL kinds that a
// catch-all source_pattern emits for any class/function it sees. These are the
// ONLY kinds the multi-kind collapse is ever allowed to drop, and only when a
// framework-specific kind covers the exact same symbol (the #3172 phantom
// shape: Falcon's catch-all re-emits a Django View as a bare Controller/
// Component). Anything NOT in this set is treated as framework-specific and is
// never dropped — so legitimate dual-emits of two specific kinds for the same
// captured text (e.g. Ansible play-name as both Service and Task, or a producer
// + consumer HTTP endpoint on the same line) are left fully intact.
//
// CRITICAL: keep this list tight. Adding a kind here authorises the pass to
// delete it whenever a more-specific node shadows it; over-broadening would
// silently drop real nodes.
var genericKinds = map[string]bool{
	"Component": true,
	"Class":     true,
	"Function":  true,
}

// kindPriority ranks the framework-specific (non-generic) kinds so that, when a
// group of generic duplicates is shadowed by MORE THAN ONE specific kind, we
// pick a single deterministic survivor. Specific kinds never collapse into each
// other (see collapseMultiKindEntities) — this table only chooses which already
// surviving specific node absorbs the dropped generics' edges/properties.
// Higher = preferred. Unlisted specific kinds get 0 and tie-break on kind name.
var kindPriority = map[string]int{
	"Model":      20,
	"Service":    21,
	"Controller": 22,
	"View":       23,
	"Route":      24,
	"Endpoint":   25,
	"Task":       26,
	"Schema":     27,
	"Repository": 28,
}

// applyPrecisionDedup runs the precision pass over a detector result. It is a
// pure function of (entities, relationships) → (entities, relationships) so it
// composes with the existing applyPass plumbing, but it is invoked directly in
// Detect because it must run before edge ID hashing downstream.
//
// Order matters: statement-noise filtering runs first (drops obvious junk and
// any edges anchored to it), then multi-kind collapse runs on the survivors.
func applyPrecisionDedup(entities []types.EntityRecord, rels []types.RelationshipRecord) ([]types.EntityRecord, []types.RelationshipRecord) {
	if !PrecisionDedupEnabled || len(entities) == 0 {
		return entities, rels
	}
	entities, rels = dropStatementNoiseOperations(entities, rels)
	entities, rels = collapseMultiKindEntities(entities, rels)
	return entities, rels
}

// dropStatementNoiseOperations removes `Operation` entities whose Name is not a
// valid identifier. Edges anchored to a dropped Operation (symbolic ID
// "Operation:<name>") are dropped with it — they were references to a node that
// never should have existed. Only the `Operation` kind is examined; all other
// kinds pass through untouched.
func dropStatementNoiseOperations(entities []types.EntityRecord, rels []types.RelationshipRecord) ([]types.EntityRecord, []types.RelationshipRecord) {
	dropped := make(map[string]bool) // symbolic ID of each dropped Operation
	out := make([]types.EntityRecord, 0, len(entities))
	for _, e := range entities {
		if isOperationKind(e.Kind) && !isRouteShapedOperation(e) && isStatementNoiseOperationName(e.Name) {
			dropped[symbolicID(e.Kind, e.Name)] = true
			continue
		}
		out = append(out, e)
	}
	if len(dropped) == 0 {
		return entities, rels // fast path: nothing dropped, original slices intact
	}
	keptRels := make([]types.RelationshipRecord, 0, len(rels))
	for _, r := range rels {
		if dropped[r.FromID] || dropped[r.ToID] {
			continue
		}
		keptRels = append(keptRels, r)
	}
	return out, keptRels
}

// collapseMultiKindEntities collapses GENERIC structural duplicates (Component
// / Class / Function) into a co-located framework-SPECIFIC entity for the same
// (Name, SourceFile, StartLine). The generic node is the #3172 phantom emitted
// by a catch-all source_pattern; the specific node is the real one. Properties
// from each dropped generic are merged into the surviving specific node
// (survivor wins on key conflict), and any edge that referenced a dropped
// generic's symbolic ID ("<genericKind>:<name>") is rewritten to the survivor's
// symbolic ID so no relationship is lost.
//
// Collapse fires ONLY when a group contains BOTH at least one generic kind AND
// at least one specific kind. Groups that are all-generic (no specific anchor to
// collapse into) or all-specific (legitimate dual-emit of two real roles, e.g.
// Ansible Service+Task or a producer+consumer HTTP endpoint) are left untouched.
func collapseMultiKindEntities(entities []types.EntityRecord, rels []types.RelationshipRecord) ([]types.EntityRecord, []types.RelationshipRecord) {
	type symKey struct {
		name  string
		file  string
		start int
	}

	// Group indices by symbol. Preserve first-seen order for determinism.
	groups := make(map[symKey][]int)
	order := make([]symKey, 0, len(entities))
	for i := range entities {
		e := &entities[i]
		k := symKey{e.Name, e.SourceFile, e.StartLine}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}

	// remap: dropped symbolic ID → canonical symbolic ID (for edge rewrite).
	remap := make(map[string]string)
	keep := make([]bool, len(entities)) // index → survives

	for _, k := range order {
		idxs := groups[k]
		if len(idxs) == 1 {
			keep[idxs[0]] = true
			continue
		}

		// Partition the group into generic vs specific members.
		var specificIdxs, genericIdxs []int
		for _, i := range idxs {
			if genericKinds[entities[i].Kind] {
				genericIdxs = append(genericIdxs, i)
			} else {
				specificIdxs = append(specificIdxs, i)
			}
		}

		// No specific anchor, or no generic phantom ⇒ nothing to collapse.
		// Keep every member untouched (all-generic and all-specific groups are
		// both legitimate as-is).
		if len(specificIdxs) == 0 || len(genericIdxs) == 0 {
			for _, i := range idxs {
				keep[i] = true
			}
			continue
		}

		// Choose the single specific survivor deterministically; keep ALL other
		// specific members too (they are real, distinct roles) — we only drop
		// the generics and fold their edges into the chosen survivor.
		winner := specificIdxs[0]
		for _, i := range specificIdxs[1:] {
			if betterCanonical(entities[i], entities[winner]) {
				winner = i
			}
		}
		for _, i := range specificIdxs {
			keep[i] = true
		}
		winSym := symbolicID(entities[winner].Kind, entities[winner].Name)

		// Drop each generic: merge its properties into the survivor and remap
		// its symbolic ID to the survivor's so edges are preserved.
		for _, i := range genericIdxs {
			mergeProperties(&entities[winner], entities[i])
			oldSym := symbolicID(entities[i].Kind, entities[i].Name)
			if oldSym != winSym {
				remap[oldSym] = winSym
			}
		}
	}

	// Rebuild entity slice in original order, survivors only.
	out := make([]types.EntityRecord, 0, len(entities))
	for i := range entities {
		if keep[i] {
			out = append(out, entities[i])
		}
	}

	if len(remap) == 0 {
		return out, rels
	}

	// Rewrite edge endpoints that pointed at a collapsed kind.
	rewritten := make([]types.RelationshipRecord, len(rels))
	for i, r := range rels {
		if to, ok := remap[r.FromID]; ok {
			r.FromID = to
		}
		if to, ok := remap[r.ToID]; ok {
			r.ToID = to
		}
		rewritten[i] = r
	}
	return out, rewritten
}

// betterCanonical reports whether candidate should be preferred over current as
// the canonical node for a multi-kind group. Higher kindPriority wins; ties
// break on the lexicographically smaller kind string for full determinism.
func betterCanonical(candidate, current types.EntityRecord) bool {
	pc := kindPriority[candidate.Kind]
	pp := kindPriority[current.Kind]
	if pc != pp {
		return pc > pp
	}
	return candidate.Kind < current.Kind
}

// mergeProperties copies properties from src into dst without overwriting keys
// dst already defines (the canonical entity's own properties are authoritative).
func mergeProperties(dst *types.EntityRecord, src types.EntityRecord) {
	if len(src.Properties) == 0 {
		return
	}
	if dst.Properties == nil {
		dst.Properties = make(map[string]string, len(src.Properties))
	}
	for kk, vv := range src.Properties {
		if _, exists := dst.Properties[kk]; !exists {
			dst.Properties[kk] = vv
		}
	}
}

// symbolicID reproduces the detector-level edge ID form ("<Kind>:<Name>") used
// when relationship rules emit FromID/ToID. Edge endpoints reference entities
// by this symbolic form (not the downstream SHA hash), so collapsing a kind
// requires rewriting these strings.
func symbolicID(kind, name string) string {
	return kind + ":" + name
}

// isStatementNoiseOperationName reports whether an Operation entity's Name is an
// UNAMBIGUOUS statement/decorator fragment that should never have been emitted
// as a standalone node (the langchain `@tool` / assignment-statement noise the
// acme bench flagged).
//
// This classifier is deliberately NARROW. The codebase legitimately uses
// non-identifier Operation names for call-idiom detection — e.g.
// "RunnableSequence.from(", ".bindTools(", "tool(async (", "createTRPCClient<".
// Those must be KEPT. So we drop ONLY three concrete statement shapes that carry
// no architectural signal:
//
//  1. Bare decorator text — name starts with `@` and the remainder is a plain
//     identifier with no call/args (e.g. "@tool", "@property"). A decorator
//     application WITH args ("@tool(" → contains `(`) is NOT matched here.
//  2. An assignment statement — contains a real `=` that is not part of a
//     comparison operator (==, !=, <=, >=). e.g. `msg = f"hi"`, `x = 1`.
//  3. A leading statement keyword followed by whitespace — `return x`,
//     `yield y`, `raise E`, etc.
//
// Everything else (including call idioms ending in `(` or `<`, dotted names,
// and ordinary identifiers) is treated as a real operation and kept.
// isOperationKind reports whether kind is an Operation-shaped kind that
// dropStatementNoiseOperations is allowed to examine. Handles both the bare
// "Operation" literal (some rule-engine/legacy paths) and the namespaced
// "SCOPE.Operation" form (internal/types.EntityKindOperation, the on-disk
// convention) so the noise filter behaves the same regardless of which
// producer stamped the entity.
func isOperationKind(kind string) bool {
	return kind == "Operation" || kind == "SCOPE.Operation"
}

// isRouteShapedOperation reports whether an Operation-kind entity is a
// recognized HTTP ROUTE captured by a framework's route-registration idiom
// (e.g. the PHP custom extractors in internal/custom/php/frameworks.go for
// Slim/CakePHP/CodeIgniter/Yii/WordPress/…, subtype "endpoint", stamped with
// a `route_path` property) rather than genuine statement-level noise (a bare
// decorator, an assignment fragment, a dangling keyword). These entities are
// EXEMPT from dropStatementNoiseOperations even when their captured Name
// looks noise-shaped (a route path such as "/users/:id" trips the "leading
// keyword" / non-identifier heuristics that were tuned for source
// statements, not URL paths) — deleting them silently zeroes out a
// framework's endpoint count (#5733, refs #5688) before any real
// http_endpoint_definition synthesis exists for it. Narrowly scoped: only
// fires for Subtype "endpoint" carrying a route_path/route property, never
// for the broader Operation population.
func isRouteShapedOperation(e types.EntityRecord) bool {
	if e.Subtype != "endpoint" {
		return false
	}
	if e.Properties == nil {
		return false
	}
	return e.Properties["route_path"] != "" || e.Properties["route"] != ""
}

func isStatementNoiseOperationName(name string) bool {
	if name == "" {
		return true // an unnamed Operation is noise
	}

	// (1) Bare decorator: "@" + identifier chars only.
	if name[0] == '@' {
		rest := name[1:]
		if rest == "" {
			return true
		}
		for i := 0; i < len(rest); i++ {
			c := rest[i]
			if !(c == '_' || c == '.' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				return false // has args/punctuation ⇒ a real call idiom, keep
			}
		}
		return true
	}

	// (2) Assignment statement: a bare `=` not part of ==, !=, <=, >=.
	for i := 0; i < len(name); i++ {
		if name[i] != '=' {
			continue
		}
		prev := byte(0)
		if i > 0 {
			prev = name[i-1]
		}
		next := byte(0)
		if i+1 < len(name) {
			next = name[i+1]
		}
		if prev == '=' || prev == '!' || prev == '<' || prev == '>' || next == '=' {
			continue // comparison operator, not assignment
		}
		return true
	}

	// (3) Leading statement keyword followed by whitespace.
	for _, kw := range []string{"return ", "yield ", "raise ", "assert ", "del ",
		"import ", "from ", "with ", "while ", "for ", "if ", "elif "} {
		if strings.HasPrefix(name, kw) {
			return true
		}
	}

	return false
}
