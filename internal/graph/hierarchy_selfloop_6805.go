package graph

import "strings"

// hierarchySelfLoopKinds is the set of relationship kinds for which a
// self-loop is not merely uninteresting but semantically impossible: no type
// is its own supertype.
//
// CALLS is deliberately absent — a self-loop CALLS edge is direct recursion,
// a real fact about the code. That exclusion is right in general and it
// knowingly leaves two rules in #6809 uncovered: celery's
// `CALLS Task -> Task` and github_actions' `CALLS Operation -> Operation`
// produce self-loops from a capture-group collision, not from recursion
// (measured: a plain function dispatching two DIFFERENT celery tasks emits
// one self-loop per task). Do NOT widen this set to catch them — that would
// delete real recursion edges to work around a rule-authoring defect. The
// right fix for those is a LOAD-TIME guard rejecting
// `source_group == target_group && source_type == target_type` when rules are
// compiled (detector.go:172-187, the way #6666 rejects `terminator` with
// `source_group: 0`). That is #6809's business, not this file's.
//
// DEPENDS_ON and ROUTES_TO are absent for the same reason — three more #6809
// rules (sqlalchemy x2, django) emit literal self-loops under those kinds,
// and 13 such edges are emitted across the golden corpus today. This filter
// deliberately does not touch them: they are a DIFFERENT mechanism, built as
// `FromID == ToID` at emission (detector.go:522) with no resolver
// involvement, and hiding them here would remove the evidence #6809 needs.
//
// Keys are the upper-case wire spellings (types.RelationshipKind* values);
// DropHierarchySelfLoops upper-cases before probing so a producer that emits
// a lower-case kind is covered too.
var hierarchySelfLoopKinds = map[string]bool{
	"IMPLEMENTS": true,
	"EXTENDS":    true,
	"INHERITS":   true,
}

// DropHierarchySelfLoops removes every relationship whose two endpoints
// resolved to the SAME entity id and whose kind asserts a type-hierarchy
// relation. It returns the filtered slice (filtered in place; the input must
// not be reused) and the number of rows dropped.
//
// Issue #6805. The self-loop this closes is NOT produced by a single bad
// rule; it is produced by the RESOLVER collapsing two differently-typed
// stubs onto one entity. The Kotlin Multiplatform rule pack
// (internal/engine/rules/kotlin/frameworks/kmp.yaml) emits
// `Implementation:<name> IMPLEMENTS Interface:<name>` for every `actual fun`
// / `actual class`. When the paired `expect` declaration IS in the indexed
// set, those are two distinct entities and the edge is correct — that is the
// expect/actual pairing, and it is the reason those rules are kept rather
// than deleted. When it is NOT — the commonest case being a member function
// of an `expect class`, which carries no `expect` keyword of its own, so
// `actual fun nowMillis` inside `actual class Clock` has no `Interface`
// counterpart to bind to — the unmatched `Interface:<name>` stub falls
// through to the resolver's bare-name lookup and binds back to the
// `Implementation:<name>` entity. Both endpoints then carry one id.
//
// A self-loop is worse than a missing edge for a graph consumer: "what
// implements Clock.nowMillis?" answers "Clock.nowMillis", a confident wrong
// answer rather than an absence. Downstream ANALYSIS already discards
// self-loops (orientation.go, module_gds.go, algorithms.go, pr_impact.go),
// but the persisted document is what the MCP expand/traces surface reads, so
// the drop has to happen before the Document is built, not inside each
// consumer.
//
// This is a graph-level invariant, not a Kotlin fix: any producer, in any
// language, that lands a hierarchy edge on one entity is expressing nothing.
//
// HONEST LIMIT — "no type is its own supertype" is true of the SOURCE
// LANGUAGE and not always true of what this function removes. The predicate
// is equality of grafel entity IDs, and the resolver can collapse two
// genuinely distinct types onto one id. Measured: `base.py: class Handler` +
// `impl.py: from base import Handler; class Handler(Handler)` — legal,
// idiomatic Python — resolves parent and child to the same id, and this
// filter deletes the edge, leaving the subclass with no recorded superclass
// at all. (F-bounded polymorphism and CRTP were also probed and are NOT
// affected: `class C : Base<C>` resolves to two distinct ids and is kept.)
// Trading a confidently-wrong self-loop for a silent absence is judged an
// improvement, because a wrong answer to "what implements X" is worse than
// no answer — but it IS a trade, not a pure win.
//
// The dropped-count printed to stderr therefore conflates two causes:
// edges that never meant anything (the KMP stub collapse above) and edges
// that meant something the resolver could not keep apart. The counter cannot
// separate them; do not read it as "N pieces of noise removed".
func DropHierarchySelfLoops(rels []Relationship) ([]Relationship, int) {
	dropped := 0
	out := rels[:0]
	for _, r := range rels {
		if r.FromID != "" && r.FromID == r.ToID && hierarchySelfLoopKinds[strings.ToUpper(r.Kind)] {
			dropped++
			continue
		}
		out = append(out, r)
	}
	return out, dropped
}
