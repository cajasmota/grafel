// hierarchy.go — Erlang inheritance topology: `-behaviour(gen_server).` →
// IMPLEMENTS (#6370).
//
// Before this, erlang emitted NO hierarchy edge by either of the two paths a
// language can get one: it is absent from `supportedLanguages` in
// `internal/extractors/cross/hierarchy/extractor.go`, and this extractor
// collected the behaviour attribute into `Properties["otp_behaviour"]` and
// stopped there. "What implements gen_server" returned empty, which is
// indistinguishable from "nothing does".
//
// # Why the edge is IMPLEMENTS and why there is no kind ladder
//
// An OTP behaviour is a callback contract, not a superclass: the module
// supplies the callbacks a behaviour module declares and inherits no state or
// code from it. That is the IMPLEMENTS relation, and Erlang has no second
// construct that could be EXTENDS — so, unlike `internal/extractors/csharp/
// hierarchy.go`, there is nothing here to select between and no ladder to get
// wrong in either direction.
//
// # Why the edge is emitted HERE and not by registering erlang in cross/hierarchy
//
// That pass invents its own graph nodes: for every type it addEntity's a
// SCOPE.Component for the type AND another for each parent. `extractErlang`
// already emits one SCOPE.Component per module, so registering erlang there
// would mint a duplicate component per module — plus a synthesised component
// for `gen_server`, which lives in OTP outside the tree and would be a
// permanent orphan. That is why #6335 emitted F#'s edges from the F#
// extractor and #6437 groovy's from the groovy one.
// TestErlangHierarchyNoDuplicateComponents guards both halves.
package erlang

import (
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// behaviourDecl is one `-behaviour(X).` / `-behavior(X).` attribute: the
// declared atom and the 1-based line the attribute sits on.
type behaviourDecl struct {
	name string
	line int
}

// collectBehaviourDecls returns the module's behaviour declarations in source
// order, deduplicated by atom (the first declaration wins, so the reported
// line is the first one). It is the single reader of behaviourRE: the OTP
// property/tag/subtype stamping and the IMPLEMENTS edges are both derived from
// this one list, so the edge can never disagree with the property about which
// behaviours a module declares.
func collectBehaviourDecls(src string) []behaviourDecl {
	var out []behaviourDecl
	seen := make(map[string]bool)
	for _, m := range behaviourRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, behaviourDecl{
			name: name,
			line: strings.Count(src[:m[0]], "\n") + 1,
		})
	}
	return out
}

// behaviourImplementsEdges returns one IMPLEMENTS relationship per declared
// behaviour, meant to be EMBEDDED on the owning module's EntityRecord.
//
// FromID is deliberately left EMPTY. The assembly loop stamps the owning
// record's own entity id, the only value that anchors the edge on the MODULE.
// A non-empty non-hex FromID (e.g. the file path) would be rewritten by
// ReferencesEmbedded onto the FILE entity — the defect fixed in #6295
// (Solidity) and #6298 (Verilog, Astro) and now guarded by
// internal/extractors/file_anchored_rels_guard_test.go (#6367).
//
// ToID is the bare behaviour atom: gen_server and friends live in OTP, outside
// any indexed tree, so a file-pinned structural ref would never bind. This
// matches the SUPERVISES edges emitted a few sections down, which name their
// child module the same way.
//
// `owner` is the module's own name; a module naming itself as its own
// behaviour yields no edge, because a self-edge is never information and is
// the signature of a mis-attributed owner (#6369).
//
// There is deliberately no `owner == ""` early return: the sole call site sits
// inside extractErlang's `-module` branch, so a file with no module attribute
// never reaches here at all (TestErlangBehaviourWithoutModuleAttributeEmitsNoEdge
// pins that from the call site). A guard for a case the call site cannot
// produce is a branch no test can observe.
func behaviourImplementsEdges(decls []behaviourDecl, owner string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, d := range decls {
		if d.name == "" || d.name == owner {
			continue
		}
		out = append(out, types.RelationshipRecord{
			// FromID intentionally empty — see the doc comment above.
			ToID: d.name,
			Kind: "IMPLEMENTS",
			Properties: types.Props{
				{K: "line", V: strconv.Itoa(d.line)},
				{K: "provenance", V: "otp_behaviour"},
			},
		})
	}
	return out
}
