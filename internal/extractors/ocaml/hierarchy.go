// hierarchy.go — OCaml inheritance topology: `inherit` → EXTENDS, a class's
// `: class-type` annotation → IMPLEMENTS (#6370).
//
// Before this, OCaml emitted NO hierarchy edge by either of the two paths a
// language can get one: it is absent from `supportedLanguages` in
// `internal/extractors/cross/hierarchy/extractor.go`, no YAML rule pack under
// `internal/engine/rules/` mentions it (checked, per the graphql arm's finding
// that a scoped Go grep answers a narrower question than the one intended),
// and this extractor suppressed `inherit` as a keyword. "What extends this
// class" returned empty, which is indistinguishable from "nothing does".
//
// # Why the edges are emitted HERE and not by registering ocaml in cross/hierarchy
//
// That pass invents its own graph nodes: for every class it addEntity's a
// SCOPE.Component for the class AND another for each parent. This file emits
// the class entities itself (see below), so registering ocaml there would mint
// a duplicate component per class with the edges anchored on the pass's own
// node rather than on the one the rest of the OCaml graph uses. That is why
// #6335 emitted F#'s edges from the F# extractor and why groovy's hierarchy.go
// — the template this file follows — does the same.
//
// # This arm has to introduce the class vocabulary, because nothing else has
//
// #6939 (2) records that no `class` construct produced any entity at all: the
// extractor's vocabulary was module / type / let. A hierarchy edge needs a
// FROM node, and #6370's bar is that the edge anchors on the TYPE and not on
// the file, so the class entity is a precondition of the edge rather than a
// separate feature. The other three gaps in #6939 (functors invisible, dead
// module CONTAINS, wrong module spans) are untouched here.
//
// Naming: a `class` declaration becomes SCOPE.Component subtype "class"; a
// `class type` declaration becomes SCOPE.Component subtype "class_type".
// "class_type" and not "interface": an OCaml class type is the language's
// interface-shaped construct and is what an IMPLEMENTS edge points at, but it
// is also legal as a plain type abbreviation, and calling it an interface
// would assert a role the declaration does not carry. The relation, not the
// subtype, is what makes it findable.
//
// # Why the CST and not a regex
//
// #6812's owner decision forbids adding regex to this package, and the reason
// is measured rather than stylistic: the regex depth walker got 230 of 408
// `inherit` attributions wrong. The vendored grammar makes every distinction
// this file needs a node type rather than a judgement —
//
//	module Extended = struct include Base end
//	  → module_definition > module_binding > structure > include_module
//	open List
//	  → open_module
//	module Make (C : Comparable) = struct … end
//	  → module_definition > module_binding > … (a functor application)
//	class circle radius = object inherit shape "circle" … end
//	  → class_definition > class_binding > object_expression
//	                     > inheritance_definition > class_application > class_path
//	class type verbose_printer = object inherit printer … end
//	  → class_type_definition > class_type_binding > class_body_type
//	                          > inheritance_specification > class_type_path
//
// — so `include`, `open` and functor application cannot mint a hierarchy edge
// by construction, not by a blocklist that has to be kept in step with the
// language. The fixture forbids all three anyway, because "cannot happen" is
// an argument and a forbidden row is a measurement.
//
// # Emit only when the target resolves (the #6370 constraint)
//
// `internal/quality/analytics/scan.go` (accumulateRel) keys touched[from]
// UNCONDITIONALLY, so a DANGLING edge de-orphans its source just as well as a
// real one. The orphan rate therefore cannot tell a working edge from a
// fabricated one, and "emit only when resolvable" has to be enforced by the
// producer. Two rules do it, and both are producer-side facts rather than
// hopes about a later pass:
//
//  1. the target must be UNQUALIFIED. `inherit Foo.Bar.base` names a class
//     this file cannot see; taking its last segment would bind `base` to a
//     same-named local class and point the edge at the wrong node, which is
//     #6369's failure mode, so a qualified path yields no edge at all.
//  2. the target must be the name of a class or class type DECLARED IN THIS
//     FILE. The producer holds the declaration in hand, so "does this bind"
//     is answered before the edge exists rather than hoped for afterwards.
//
// ToID is the entity's Format A structural ref, NOT the bare written name that
// Solidity / Crystal / F# / Groovy use. Those languages emit a bare name
// because their parent is usually declared in another file, so a file-pinned
// ref would never bind; rule (2) inverts that premise here — this producer
// only ever names a class it has just declared in the SAME file, so the ref
// always binds and is strictly more specific than the name.
//
// THIS COMMENT HAS BEEN WRONG TWICE ABOUT THIS FIELD, AND BOTH CORRECTIONS ARE
// KEPT BECAUSE EACH MISTAKE IS EASY TO REPEAT.
//
// First it argued the ref was UNGRADEABLE, because the fixture fence
// `remote_relay --[EXTENDS]--> external_base` cannot match an edge whose ToID
// is `scope:component:class:ocaml:<file>:<name>`. The premise was true; the
// conclusion did not follow. A fixture row is not the only instrument —
// TestOCamlHierarchy_UndeclaredParentYieldsNoEdge_6370 kills the
// gate-deletion mutant under EITHER spelling, because hier() reduces a target
// past the last ':' and so grades the guard rather than the dialect. "The
// fixture cannot express it" is a fact about the fixture.
//
// Then, having switched to the bare name on that argument, it claimed the
// bare name was sufficient — that two files each declaring a class of one
// name bind to their own file's node. MEASURED, AND FALSE. Two `.ml` files
// each declaring `class shape` with a local subclass, graded in both file
// orders:
//
//	bare name        0/2 bound, 0 cross-file hits
//	structural ref   2/2 bound, 0 cross-file hits
//
// The bare name is SAFE — ambiguity leaves the edge unresolved rather than
// pointing it at the other file's node, so #6369's wrong-node failure does not
// occur either way — but it silently LOSES the edge, which is a recall defect
// the ref does not have. Same-named classes across files are ordinary in
// OCaml, where a module's classes are named for their role (`t`, `printer`,
// `writer`) rather than uniquely.
//
// The fixture fence is spelled with the ref and fires; the unit test above
// grades the same guard without naming a spelling at all, so the two are
// independent instruments rather than one restated.
//
// The cost is stated rather than hidden: cross-file and cross-module
// inheritance produces no edge today. That is deliberate under (1) and (2) and
// is filed rather than fudged — an unresolvable bare name would improve the
// orphan metric and the edge count while pointing at nothing.
package ocaml

import (
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

// hierarchyEdge is one declared parent, with the line of the clause that
// declared it.
type hierarchyEdge struct {
	kind   string // "EXTENDS" | "IMPLEMENTS"
	target string
	line   int
}

// classDecl is one `class` or `class type` binding, recorded from the CST and
// independent of the file path so it can live on the src-keyed block memo.
type classDecl struct {
	name      string
	subtype   string // "class" | "class_type"
	startLine int
	endLine   int
	parents   []hierarchyEdge
}

// collectClasses records every class and class-type binding in the tree.
//
// It walks the WHOLE tree rather than the top level, so a class declared
// inside `module M = struct … end` is found. Anonymous `object … end` — the
// `let make_logger prefix = object inherit stream_writer prefix … end` shape —
// has no class_binding and therefore yields neither an entity nor an edge:
// there is no class to anchor an edge on, and inventing one named after the
// enclosing `let` would be a fabricated node.
func collectClasses(n ts.Node, src string) []classDecl {
	var out []classDecl
	var walk func(ts.Node)
	walk = func(n ts.Node) {
		switch n.Type() {
		case "class_definition":
			for _, b := range childrenOfType(n, "class_binding") {
				if d, ok := classBindingDecl(b, src); ok {
					out = append(out, d)
				}
			}
		case "class_type_definition":
			for _, b := range childrenOfType(n, "class_type_binding") {
				if d, ok := classTypeBindingDecl(b, src); ok {
					out = append(out, d)
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			if c := n.Child(i); c != nil {
				walk(c)
			}
		}
	}
	walk(n)
	return out
}

// classBindingDecl reads one `class [virtual] ['a] name params [: ct] = object
// … end` binding.
func classBindingDecl(b ts.Node, src string) (classDecl, bool) {
	name := firstChildText(b, "class_name", src)
	if name == "" {
		return classDecl{}, false
	}
	d := classDecl{
		name:      name,
		subtype:   "class",
		startLine: int(b.StartPoint().Row) + 1,
		endLine:   int(b.EndPoint().Row) + 1,
	}
	// The `: class-type` annotation, when present, is a DIRECT child of the
	// binding and sits before the `=`. It is the only IMPLEMENTS source.
	//
	// The direct-child read is LOAD-BEARING, and this comment has been wrong
	// about that twice. It first claimed anything deeper "is a method's type
	// annotation"; five annotation sites were then probed — a self-type
	// `object (self : printer)`, a `val` annotation, a method return
	// annotation, an ascription `(p : printer)` and a coercion
	// `(p :> printer)` — all five produced `type_constructor_path`, and the
	// comment was rewritten to claim the search depth was therefore
	// equivalent. A reviewer found the sixth site: `#printer`, the hash-type
	// form OCaml uses for "any object matching this class type", parses as
	// `hash_type > class_type_path`, so
	//
	//	class g = object method m (x : #printer) = x end
	//
	// makes a recursive search emit `g IMPLEMENTS printer` for a conformance
	// `g` never declared. FIVE SITES CHOSEN BY ONE PERSON IS ONE
	// CONFIRMATION, NOT FIVE — the second claim rested on the very
	// enumeration that had just been shown incomplete. The depth is now
	// graded by
	// TestOCamlHierarchy_AnnotationIsTheBindingsOwnDirectChild_6370.
	for _, p := range childrenOfType(b, "class_type_path") {
		if t := pathLeafName(p, "class_type_name", src); t != "" {
			d.parents = append(d.parents, hierarchyEdge{
				kind: "IMPLEMENTS", target: t,
				line: int(p.StartPoint().Row) + 1,
			})
		}
	}
	// `inherit` clauses are direct children of the binding's OWN object body.
	// Requiring a direct child is what keeps an `object … inherit … end`
	// nested inside a method from being attributed to the enclosing class.
	for _, body := range childrenOfType(b, "object_expression") {
		d.parents = append(d.parents, inheritEdges(body, "inheritance_definition", "class_name", src)...)
	}
	return d, true
}

// classTypeBindingDecl reads one `class type name = object … end` binding.
func classTypeBindingDecl(b ts.Node, src string) (classDecl, bool) {
	name := firstChildText(b, "class_type_name", src)
	if name == "" {
		return classDecl{}, false
	}
	d := classDecl{
		name:      name,
		subtype:   "class_type",
		startLine: int(b.StartPoint().Row) + 1,
		endLine:   int(b.EndPoint().Row) + 1,
	}
	for _, body := range childrenOfType(b, "class_body_type") {
		d.parents = append(d.parents, inheritEdges(body, "inheritance_specification", "class_type_name", src)...)
	}
	return d, true
}

// inheritEdges reads the `inherit` clauses that are direct children of one
// class body.
//
// Three carriers exist for the parent, and they are matched by node type
// rather than by unwrapping whatever is there:
//
//	inherit shape             → class_path
//	inherit shape "circle"    → class_application  > class_path
//	inherit ['a] boxed        → instantiated_class > class_path
//
// `inherit!` differs only by an anonymous `!` token and needs no arm. Anything
// else — an inherited object literal, say — yields no edge rather than a
// guess.
func inheritEdges(body ts.Node, clauseType, leafType string, src string) []hierarchyEdge {
	pathType := "class_path"
	if leafType == "class_type_name" {
		pathType = "class_type_path"
	}
	var out []hierarchyEdge
	for _, cl := range childrenOfType(body, clauseType) {
		line := int(cl.StartPoint().Row) + 1
		var path ts.Node
		for i := 0; i < int(cl.ChildCount()); i++ {
			c := cl.Child(i)
			if c == nil || !c.IsNamed() {
				continue
			}
			switch c.Type() {
			case pathType:
				path = c
			case "class_application", "instantiated_class":
				if p := firstChildOfType(c, pathType); p != nil {
					path = p
				}
			}
			if path != nil {
				break
			}
		}
		if path == nil {
			continue
		}
		if t := pathLeafName(path, leafType, src); t != "" {
			out = append(out, hierarchyEdge{kind: "EXTENDS", target: t, line: line})
		}
	}
	return out
}

// pathLeafName returns the bare name of an UNQUALIFIED class/class-type path.
//
// A module-qualified path — `Foo.Bar.base`, carrying a module_path or
// extended_module_path child — returns "". Its last segment names a class in
// another module, which this file cannot resolve, and binding it to a
// same-named local class is exactly the wrong-node defect #6369 records.
func pathLeafName(path ts.Node, leafType string, src string) string {
	for i := 0; i < int(path.ChildCount()); i++ {
		c := path.Child(i)
		if c == nil || !c.IsNamed() {
			continue
		}
		if c.Type() == "module_path" || c.Type() == "extended_module_path" {
			return ""
		}
	}
	return firstChildText(path, leafType, src)
}

func childrenOfType(n ts.Node, want string) []ts.Node {
	var out []ts.Node
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c != nil && c.IsNamed() && c.Type() == want {
			out = append(out, c)
		}
	}
	return out
}

func firstChildOfType(n ts.Node, want string) ts.Node {
	for _, c := range childrenOfType(n, want) {
		return c
	}
	return nil
}

func firstChildText(n ts.Node, want string, src string) string {
	c := firstChildOfType(n, want)
	if c == nil {
		return ""
	}
	s, e := int(c.StartByte()), int(c.EndByte())
	// A bound no input is known to reach: src is the exact string the tree
	// was parsed from (blockIndexFor keys its memo on string EQUALITY, so a
	// hash collision cannot hand one file's tree to another's text), and a
	// named node's span is non-empty. Kept as a slice bound rather than
	// deleted, and labelled as unexercised rather than described as if it
	// filtered something.
	if e > len(src) || s >= e {
		return ""
	}
	return src[s:e]
}

// classEntities turns the CST's class bindings into entity records carrying
// their own hierarchy edges.
//
// FromID is deliberately left EMPTY on every edge. The assembly loop stamps
// the owning record's own entity id, the only value that anchors the edge on
// the CLASS. A non-empty non-hex FromID (e.g. the file path) would be
// rewritten by ReferencesEmbedded onto the FILE entity, merging every class in
// a multi-class file onto one node — the defect fixed in #6295 (Solidity) and
// #6298 (Verilog, Astro) and guarded by
// internal/extractors/file_anchored_rels_guard_test.go (#6367).
func classEntities(src, filePath string, imports []string) []types.EntityRecord {
	decls := blockIndexFor(src).classes
	if len(decls) == 0 {
		return nil
	}

	// Declared names, first declaration wins. OCaml puts classes and class
	// types in the same namespace as types, so two bindings with one name in
	// one file is not legal source; the dedup is a defence against malformed
	// input producing two entities with one id, not a language feature.
	declared := make(map[string]bool, len(decls))
	out := make([]types.EntityRecord, 0, len(decls))
	for _, d := range decls {
		if declared[d.name] {
			continue
		}
		declared[d.name] = true
		kw := "class "
		if d.subtype == "class_type" {
			kw = "class type "
		}
		out = append(out, types.EntityRecord{
			Name:       d.name,
			Kind:       "SCOPE.Component",
			Subtype:    d.subtype,
			SourceFile: filePath,
			Language:   "ocaml",
			StartLine:  d.startLine,
			EndLine:    d.endLine,
			Signature:  kw + d.name,
			Properties: map[string]string{"imports": strings.Join(imports, ",")},
		})
	}

	byName := make(map[string]int, len(out))
	for i := range out {
		byName[out[i].Name] = i
	}
	for _, d := range decls {
		i, ok := byName[d.name]
		if !ok {
			continue
		}
		seen := make(map[string]bool, len(d.parents))
		for _, p := range d.parents {
			// A self-edge is never information; it is the signature of a
			// mis-attributed owner (#6369).
			if p.target == d.name {
				continue
			}
			// The resolution gate: no entity for the target, no edge.
			if !declared[p.target] {
				continue
			}
			key := p.kind + ":" + p.target
			if seen[key] {
				continue
			}
			seen[key] = true
			out[i].Relationships = append(out[i].Relationships, types.RelationshipRecord{
				// FromID intentionally empty — see the doc comment above.
				ToID: extractor.BuildComponentStructuralRef("ocaml", filePath, p.target),
				Kind: p.kind,
				Properties: types.Props{
					{K: "line", V: strconv.Itoa(p.line)},
				},
			})
		}
	}
	return out
}
