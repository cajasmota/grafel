package vbnet

// references.go — S7b of #6327: `Handles` clauses and `AddressOf` operands.
//
// # Why REFERENCES, and not a new kind
//
// Both constructs name a member as a VALUE rather than invoking it, which is
// what REFERENCES already means everywhere else in grafel:
// golang/references.go, java/references.go and python/references.go all define
// it as "an identifier use that is NOT the callee of a call". `AddressOf
// Worker` is precisely that — a method converted to a delegate — and `Handles
// Button1.Click` names a control and an event without touching either.
//
// CALLS was rejected on evidence rather than taste. The whole point of #6327's
// disambiguator is that a call-SHAPED site need not be a call, and neither of
// these sites is even call-shaped: no control transfer happens at an
// AddressOf, and a Handles clause transfers control at some later, unmodelled
// moment chosen by the framework. Emitting CALLS would inflate the corpus's
// measured 8,702 IsCall sites with edges no execution path takes, which is the
// confidently-wrong edge this epic exists to prevent.
//
// A NEW kind (HANDLES / SUBSCRIBES_TO) was considered and rejected. No other
// language in the graph models event wiring, so a VB-only kind would be
// invisible to every existing traversal, query and quality gate, and would
// have to be threaded through types.AllRelationshipKinds, the producer-
// boundary scan and the dashboard for one language's benefit. REFERENCES fits,
// so ADR-0018's append-only rule is not invoked. The construct that produced
// each edge is not lost: it is stamped as `via`.
//
// # Direction
//
// handler -> handled, always. That is forced as well as correct: FromID must
// stay EMPTY so graph assembly stamps the OWNING record (a path-valued FromID
// is the #6295/#6298/#6365/#6367 defect class, guarded by
// internal/extractors/file_anchored_rels_guard_test.go), and the owning record
// is the method carrying the clause or the member containing the operand. The
// opposite direction — event -> handler — would need the FROM endpoint to be
// the event, which this anchor cannot express.
//
// # What a target is NAMED, and why it is not what the source says
//
// MEASURED on the 302-file corpus, all three variants built and resolved
// through the full Path-B chain, counting hex ToIDs after ReferencesEmbedded:
//
//	target rendering                                   edges  resolved
//	as written, Me/MyBase folded (`Worker`, `b.Click`)  1,070        17
//	+ bare names qualified by the enclosing type        1,213       434
//	+ Handles targeting the RECEIVER, event as a prop   1,212       952
//
// The first two lines are the interesting ones. A bare `Worker` resolves for
// CALLS and not for REFERENCES, and that asymmetry is real, not a bug here:
// internal/resolve/refs.go:3273 extends the leaf-name fallback to bare stubs
// only when `relKind == "CALLS"` (#778, #6177 — a CALLS destination must be
// callable). Members are EMITTED as `<Type>.<member>` by declName, so a bare
// REFERENCES stub names nothing in the graph. Rendering the target under the
// extractor's own naming convention is what closes the gap; it is not a
// heuristic but the same rule declName already applies to the declaration end.
//
// The third line is why a `Handles` target is the RECEIVER and not the event.
// `Handles Button1.Click` names a WithEvents field of the enclosing type and
// an event of THAT FIELD'S TYPE. The field is in-tree — S7a had to land first
// so a designer-declared control and its handler share one Component — while
// the event belongs to System.Windows.Forms.Button and can never resolve in
// any per-file pass. Targeting the event costs 518 resolved edges and buys a
// dangling stub. The event name is kept as the `event` property, so nothing
// the source said is discarded, and `Handles Me.Shown` / `Handles MyBase.Load`
// still target the EVENT, because there the receiver is the current instance
// and names no entity of its own.

import (
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
	"github.com/cajasmota/grafel/internal/vbnet"
)

// appendReferences adds one REFERENCES edge per distinct target among this
// node's `Handles` targets and `AddressOf` operands.
//
// As with appendCalls, descendants are NOT walked: emit recurses, and a
// declaration with its own record owns its own wiring.
func appendReferences(rec *types.EntityRecord, n *vbnet.Node) {
	seen := map[string]bool{}
	for _, r := range rec.Relationships {
		if r.Kind == "REFERENCES" {
			seen[r.ToID] = true
		}
	}
	// types.Props is a SORTED slice binary-searched by Props.Get
	// (types/props.go:67); keys must ascend or they read back absent.
	add := func(target, event, line, via string) {
		if target == "" || seen[target] {
			return
		}
		seen[target] = true
		props := types.Props{}
		if event != "" {
			props = append(props, types.PropKV{K: "event", V: event})
		}
		props = append(props,
			types.PropKV{K: "line", V: line},
			types.PropKV{K: "via", V: via})
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			// FromID intentionally empty: assembly stamps the owning member.
			ToID:       target,
			Kind:       "REFERENCES",
			Properties: props,
		})
	}

	owner := enclosingTypeName(n)

	// A `Handles` clause is stamped with the METHOD's declaration line.
	// vbnet.Node records the clause targets as a []string with no positions —
	// the same limit hierarchyEdges already documents for Inherits/Implements
	// — so the clause's own line is not available. It is on or just after the
	// declaration line in every case, since the clause closes the signature.
	handlesLine := strconv.Itoa(n.Span.StartLine)
	for _, raw := range n.Handles {
		target, event := handlesTarget(raw, owner)
		add(target, event, handlesLine, "handles")
	}
	for _, a := range n.AddressOfs {
		add(qualifyBare(owner, memberTarget(a.Qualifier, a.Name)), "",
			strconv.Itoa(a.Line), "addressof")
	}
}

// handlesTarget renders the REFERENCES ToID for one `Handles` operand, plus
// the event name to stamp beside it.
//
// The grammar admits exactly three receivers, and each gets the answer that
// names a real node:
//
//	Handles Button1.Click  -> ("Form1.Button1", "Click")  the WithEvents FIELD
//	Handles MyBase.Load    -> ("Form1.Load",    "")       the event itself
//	Handles Me.Shown       -> ("Form1.Shown",   "")       the event itself
//
// Me / MyClass / MyBase denote the current instance and name no entity, so
// there is no receiver to point at and the event is the only target left —
// which is also the only case where the event can be in-tree, since a form
// declaring `Public Event Shown` is emitted as `Form1.Shown`.
func handlesTarget(raw, owner string) (target, event string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	dot := strings.LastIndexByte(raw, '.')
	if dot < 0 {
		// No receiver at all. Not legal VB — a Handles operand is a member
		// access by grammar — but a malformed signature must not silently
		// produce a differently-shaped edge.
		return qualifyBare(owner, raw), ""
	}
	recv := strings.TrimSpace(raw[:dot])
	ev := strings.TrimSpace(raw[dot+1:])
	switch vbnet.FoldName(recv) {
	case "", "me", "myclass", "mybase":
		return qualifyBare(owner, ev), ""
	}
	return qualifyBare(owner, recv), ev
}

// memberTarget renders the ToID for a member named through an optional
// qualifier, folding away the three pseudo-receivers exactly as callTarget
// does.
//
// Me / MyClass / MyBase denote the current instance and name no entity, so
// keeping them would guarantee a dangling edge; the member resolves as a bare
// name against the enclosing type, which is the answer vbnet's own classifier
// already reaches for `Me.Foo(`.
func memberTarget(qualifier, name string) string {
	if name == "" {
		return ""
	}
	switch vbnet.FoldName(qualifier) {
	case "", "me", "myclass", "mybase":
		return name
	}
	return qualifier + "." + name
}

// enclosingTypeName returns the BARE name of the nearest enclosing type,
// which is the prefix declName gives every member of that type.
func enclosingTypeName(n *vbnet.Node) string {
	for x := n; x != nil; x = x.Parent() {
		if x.Kind.IsType() && x.Name != "" {
			return x.Name
		}
	}
	return ""
}

// qualifyBare renders an unqualified member reference under the extractor's
// own member-naming convention, `<enclosing type>.<member>`.
//
// An ALREADY-dotted target is left alone: its qualifier names something
// outside this type — a module, a local, `My.Forms.Explorer` — and prefixing
// it would invent a nesting that does not exist. That is the same refusal
// callTarget makes for a qualified call site, and it is the bulk of the
// residual: those references are real, and naming their receiver needs
// cross-file type information no per-file pass has.
func qualifyBare(owner, target string) string {
	if owner == "" || target == "" || strings.Contains(target, ".") {
		return target
	}
	return owner + "." + target
}
