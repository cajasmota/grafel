package engine

// Class-representation folding vocabulary, shared by the two paths that have to
// agree on what KIND a class-like entity ends up carrying (#6148).
//
// The full rebuild runs Pass 2.5 (Detector.Detect over the YAML rule sets),
// which emits a framework-typed record — Model / View / Controller / Service /
// … — for every class symbol a rule matches, and then folds the per-language
// AST's generic SCOPE.Component class node into that record (the #1613
// class-shadow fold in cmd/grafel/index.go).
//
// The incremental path re-extracts a changed file with the per-language
// extractor ONLY. Before #6148 it therefore never saw Pass 2.5's answer: a
// class arriving in a changed file kept the generic SCOPE.Component kind, while
// a full rebuild of the same tree typed it. Classes in UNCHANGED files hid the
// defect, because those entities are carried forward from the previous full
// rebuild's graph with the typed kind already on them.
//
// The maps below were the fold's private vocabulary in package main. They live
// here so the incremental path resolves the same kind from the same table
// rather than from a second copy that can drift.

import "github.com/cajasmota/grafel/internal/types"

// FrameworkClassKindPriority ranks framework-typed node kinds by how strongly
// they represent a class/type *declaration* (vs a nested artifact that merely
// shares the class name, e.g. a Django Meta `Constraint`). These are the only
// kinds eligible to be a fold *survivor*. When several share a fold source's
// source_file+name, the highest-priority kind wins. Higher value = stronger
// class signal. SCOPE.Component is intentionally absent: it is the generic AST
// node we fold AWAY into a framework-typed survivor (so a class with a View
// resolves to ONE node), and it is itself the survivor only when no
// framework-typed node exists (handled separately, never as a candidate here).
//
// Both bare kind names (emitted by Java/Django custom extractors) AND their
// "SCOPE."-prefixed forms (emitted by Kotlin, TypeScript, proto, and pattern
// extractors) must appear so that FrameworkClassKindPriority[r.Kind] matches
// regardless of which extractor emitted the survivor. Issue #1700.
var FrameworkClassKindPriority = map[string]int{
	// Bare names (Java/Django/Spring-boot custom extractors)
	"Model":          100,
	"View":           100,
	"Controller":     100,
	"Service":        100,
	"Middleware":     100,
	"Repository":     100,
	"Worker":         100,
	"Job":            100,
	"Topic":          100,
	"TestClass":      90,
	"Schema":         80,
	"Plugin":         80,
	"Implementation": 80,
	"Interface":      80,
	"Task":           70,

	// SCOPE.-prefixed equivalents (Kotlin extractor, proto extractor, NestJS
	// service_detector, and any other extractor that emits the canonical
	// "SCOPE.<Kind>" form for a class-like entity). Priorities mirror the bare
	// form so that the highest-fidelity named node always beats a generic shadow.
	// Issue #1700.
	"SCOPE.Service":     100,
	"SCOPE.View":        100, // #1727: View+Component fold (SCOPE-prefixed form)
	"SCOPE.Model":       100,
	"SCOPE.UIComponent": 100,
	"SCOPE.GrpcService": 90,
	"SCOPE.Schema":      80,
}

// FrameworkClassKindCanonRank is the deterministic tiebreaker used when two or
// more framework-typed nodes share the SAME (source_file, name) AND the SAME
// FrameworkClassKindPriority — i.e. one class symbol was double-emitted under
// two equally-strong kinds (the #3172/#3195 DRF/Django family: a Django
// `models.Model` class surfacing as BOTH "Model" AND "Controller"). Both are
// survivor candidates of priority 100, so without a tiebreaker the fold leaves
// two nodes for one class, violating the #1613 "every class → ONE node"
// invariant (TestClassShadowFold_NoLine0Shadows).
//
// Higher value = stronger canonical signal. The ranking prefers a kind that
// names what the class structurally *is* (its declaration role: a data Model, a
// rendered View, a Service, a Repository, a Schema) over a kind that names how
// the class is *dispatched/routed* ("Controller"). A Controller is the role
// most prone to spurious co-emission from route/endpoint synthesis on a class
// that is really a Model/View, so it is deliberately ranked below the structural
// declaration kinds. Kinds absent from this map rank as 0 (only consulted to
// break an exact-priority tie; the priority map remains the primary order).
var FrameworkClassKindCanonRank = map[string]int{
	"Model": 5, "SCOPE.Model": 5,
	"View": 5, "SCOPE.View": 5,
	"Repository": 4,
	"Service":    4, "SCOPE.Service": 4,
	"Schema": 3, "SCOPE.Schema": 3,
	"Worker": 2, "Job": 2, "Topic": 2,
	"Middleware": 1,
	"Controller": 0,
}

// ClassLikeComponentSubtypes are the SCOPE.Component subtypes that denote a
// class/type declaration (as opposed to subtype="file"/"import"/"module").
// Language AST subtypes ("class", "struct", …) are the primary set. Framework-
// injected subtypes from NestJS, Angular, Spring-boot, Quarkus, and similar
// extractors are included so that an inferential SCOPE.Component(subtype="service")
// node emitted alongside a real SCOPE.Service node for the same class symbol is
// recognised as a fold source and collapsed into the typed survivor. Issue #1700.
//
// "view" is included so that SCOPE.Component(subtype="view") nodes emitted
// alongside a real SCOPE.View (or bare "View") node for the same class symbol
// are recognised as fold sources. Issue #1727.
var ClassLikeComponentSubtypes = map[string]bool{
	// Language AST subtypes
	"class": true, "struct": true, "interface": true,
	"protocol": true, "trait": true, "behaviour": true,
	// Framework-injected subtypes (NestJS, Angular, Spring, Quarkus, …)
	"service": true, "controller": true, "repository": true,
	"guard": true, "interceptor": true, "pipe": true,
	"middleware": true, "resolver": true, "gateway": true,
	"worker": true, "job": true, "task": true,
	// View-type subtypes (Django CBV, MVC view layers, …) — #1727
	"view": true,
}

// IsClassHierarchyShadow reports whether r is the line-less duplicate the
// class-hierarchy pass emits for every class it sees.
func IsClassHierarchyShadow(r *types.EntityRecord) bool {
	return r.Properties["provenance"] == "INFERRED_FROM_CLASS_HIERARCHY"
}

// IsClassFoldSource reports whether r is a class-representation node that should
// be folded into a framework-typed node when one exists for the same symbol:
//   - the INFERRED_FROM_CLASS_HIERARCHY shadow emitted by the hierarchy pass, OR
//   - the generic SCOPE.Component class node emitted by the per-language AST
//     extractor (these two share an EntityID and pre-merge at assembly), OR
//   - a SCOPE.Component carrying Properties["role"]="class" (hierarchy pass
//     annotations on nodes where the language AST subtype is not yet in
//     ClassLikeComponentSubtypes — e.g. TypeScript/React class components
//     with a framework-injected role tag). Issue #1727.
//
// When NO framework-typed node exists, a fold source is kept as the single
// node for that class (it already carries a real line from its extractor).
func IsClassFoldSource(r *types.EntityRecord) bool {
	if r.Name == "" {
		return false
	}
	if IsClassHierarchyShadow(r) {
		return true
	}
	if r.Kind != "SCOPE.Component" {
		return false
	}
	// Subtype-based recognition: language AST and framework-injected subtypes.
	if ClassLikeComponentSubtypes[r.Subtype] {
		return true
	}
	// Role-property recognition: hierarchy pass sets role="class" on SCOPE.Component
	// nodes whose subtype is not yet in the allowlist (e.g. component, vue_component,
	// empty subtype from certain extractors). Issue #1727.
	return r.Properties["role"] == "class"
}

// betterFrameworkClassCandidate reports whether c should displace best as the
// canonical framework-typed survivor for one (source_file, name). It is the
// precedence the #1613 fold's bestCand applies, minus the final id tiebreak
// (ids are not stamped yet at the point the incremental path folds):
//
//  1. highest FrameworkClassKindPriority (class-declaration strength),
//  2. highest FrameworkClassKindCanonRank (structural-role tiebreaker),
//  3. smallest real (>0) start_line — the declaration; 0 == "no line" loses.
//
// A full tie keeps the incumbent, so the answer is a function of emission order
// only when the two candidates are indistinguishable on all three axes, and
// Detect's emission order is itself deterministic (rule-set order × regex match
// order over the file).
func betterFrameworkClassCandidate(c, best *types.EntityRecord) bool {
	cp, bp := FrameworkClassKindPriority[c.Kind], FrameworkClassKindPriority[best.Kind]
	if cp != bp {
		return cp > bp
	}
	cr, br := FrameworkClassKindCanonRank[c.Kind], FrameworkClassKindCanonRank[best.Kind]
	if cr != br {
		return cr > br
	}
	if c.StartLine != best.StartLine {
		return (best.StartLine == 0 && c.StartLine > 0) ||
			(c.StartLine > 0 && c.StartLine < best.StartLine)
	}
	return false
}

// FoldFrameworkClassKinds is the incremental path's port of the full rebuild's
// Pass 2.5 + #1613 fold, for ONE file. recs is the per-language extractor's
// output for that file; fw is engine.Detector's. It returns the record set to
// emit and the number of class-representation records folded.
//
// What "one file" costs, precisely. The full path's fold indexes survivor
// candidates from the WHOLE corpus but keys them by (SourceFile, Name), so no
// pair of records from different files can ever fold together — restricting the
// candidate set to one file's records is therefore lossless ON THE FILE AXIS.
// It is not lossless in general, and an earlier version of this comment claimed
// otherwise: the full path also indexes candidates emitted by the LANGUAGE
// EXTRACTOR itself, not just by Detect. Several extractors emit a
// framework-typed kind directly for a class they also emit a generic node for —
// kotlin (SCOPE.Service for a Spring stereotype, the #1700 case), proto, razor,
// cobol, fsharp, elm. Indexing only Detect's output left those classes with TWO
// nodes on this path, breaking the #1613 "one class → one node" invariant the
// fold exists to enforce. Both sets are candidates here.
//
// Detect's records that fold with nothing are EMITTED, not discarded (#6150):
// a Route from a responder method, a Service from an app object and a Config
// from its constructor have no extractor counterpart at all, and a full rebuild
// carries every one of them. Running the classifier and keeping only the class
// half would pay its whole cost for a seventh of its output.
//
// The two loops mirror the full path's, in its order:
//
//  1. SIBLING-SURVIVOR FOLD. All eligible framework-typed candidates for one
//     (file, name) collapse into the best one; the losers are dropped after
//     donating their properties and owned edges. Without it, a symbol emitted
//     under two equally-strong kinds resolves to >1 node (#3172/#3195).
//  2. FOLD-SOURCE LOOP. Each class-representation record (see IsClassFoldSource)
//     folds into the survivor for its symbol, if one exists.
//
// The framework-typed record is the SURVIVOR, exactly as in the full path: it
// carries the kind, subtype, qualified name, language and line span the fold
// keeps, and the folded-away record contributes only the properties the survivor
// lacks (minus "provenance"/"ref") plus the edges it owns. Doing it the other
// way round — keeping the source record and merely re-kinding it — reproduces
// the KIND but neither the subtype nor the property set, and the content-keyed
// parity gate compares both (measured: that mutant is killed by the gate AND by
// this package's tests).
//
// A fold source with no framework-typed candidate is returned unchanged: a class
// no rule matches must stay the generic node, which is what the full path does
// with it. Records that are neither candidates nor fold sources pass through
// untouched and in order.
func FoldFrameworkClassKinds(recs []types.EntityRecord, fw []types.EntityRecord) ([]types.EntityRecord, int) {
	if len(recs) == 0 && len(fw) == 0 {
		return recs, 0
	}

	// all is the candidate/emission universe in the full path's order: the
	// extractor's records first, then Detect's. Order is load-bearing only as the
	// last tiebreak (see betterFrameworkClassCandidate), where the full path uses
	// the smallest stamped id — not available here, ids are stamped downstream.
	all := make([]*types.EntityRecord, 0, len(recs)+len(fw))
	for i := range recs {
		all = append(all, &recs[i])
	}
	for i := range fw {
		all = append(all, &fw[i])
	}

	type key struct{ file, name string }
	// eligible reports whether r can be a fold SURVIVOR. The blank checks are
	// enforcement, not decoration: without them every file-less or nameless
	// record would key onto {"",""} and fold with the others.
	eligible := func(r *types.EntityRecord) bool {
		if r.Name == "" || r.SourceFile == "" {
			return false
		}
		_, ok := FrameworkClassKindPriority[r.Kind]
		return ok
	}

	// ── Loop 1: sibling-survivor fold ──
	best := make(map[key]*types.EntityRecord)
	for _, r := range all {
		if !eligible(r) {
			continue
		}
		k := key{r.SourceFile, r.Name}
		if cur, seen := best[k]; !seen || betterFrameworkClassCandidate(r, cur) {
			best[k] = r
		}
	}
	dropped := make(map[*types.EntityRecord]bool)
	for _, r := range all {
		if !eligible(r) {
			continue
		}
		if sv := best[key{r.SourceFile, r.Name}]; sv != r {
			donate(sv, r)
			dropped[r] = true
		}
	}

	// ── Loop 2: fold-source loop ──
	folded := 0
	for _, r := range all {
		if dropped[r] || !IsClassFoldSource(r) {
			continue
		}
		// sv == r is reachable: a class-hierarchy SHADOW carrying an eligible
		// framework kind is BOTH a survivor candidate and a fold source, and
		// without this it would donate its own edges to itself.
		sv, ok := best[key{r.SourceFile, r.Name}]
		if !ok || sv == r {
			continue
		}
		donate(sv, r)
		dropped[r] = true
		folded++
	}

	out := make([]types.EntityRecord, 0, len(all))
	for _, r := range all {
		if !dropped[r] {
			out = append(out, *r)
		}
	}
	return out, folded
}

// donate folds src away into sv: sv keeps every field of its own and gains the
// properties src carried that it lacks, plus the edges src owns.
//
// "provenance" and "ref" describe the record being folded AWAY — provenance
// records that a node was INFERRED_FROM_CLASS_HIERARCHY, which is false of the
// survivor — so they are the two the full path refuses to donate.
//
// An owned edge carries either an empty FromID (meaning "my owner") or its
// owner's id, and the record→graph seam substitutes the OWNING record's id for
// the empty form. Appending them to the survivor is therefore what re-homes
// them; rewriting FromID here would bind them to an id that no longer exists.
func donate(sv, src *types.EntityRecord) {
	if len(src.Properties) > 0 {
		if sv.Properties == nil {
			sv.Properties = make(map[string]string, len(src.Properties))
		}
		for pk, pv := range src.Properties {
			if pk == "provenance" || pk == "ref" {
				continue
			}
			if _, exists := sv.Properties[pk]; !exists {
				sv.Properties[pk] = pv
			}
		}
	}
	sv.Relationships = append(sv.Relationships, src.Relationships...)
	src.Relationships = nil
}
