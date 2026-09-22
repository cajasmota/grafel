// #7328 arm (c) — THE ROSTER'S REFERENT.
//
// internal/types/produced_entity_kind.go records fifteen entity kinds the tree
// produces today that the EntityKind enum does not carry, so that the runtime
// guard can fail on a NEW off-vocabulary kind without turning the existing
// ninety-three sites red before #7310 decides the taxonomy.
//
// (An earlier revision of this header said eleven kinds at sixty-one sites.
// Those were #7328's scoping figures and they were low: that sweep matched
// `"SCOPE.*"` literals, and four of the fifteen kinds are not spelled that
// way. The roster's own comment narrates the correction.)
//
// A hand-maintained roster with no relation to the population it covers is the
// #6887 defect, and an allowlist that nothing re-derives goes stale silently in
// the direction that matters least visibly: an entry whose sites are gone keeps
// waving through a kind the tree no longer has any reason to accept.
//
// So this test is the roster's referent. It re-derives the whole population
// from the source tree and asserts the derived result equals the roster
// EXACTLY. It fails when a kind is added, removed, renamed, or when any kind's
// site count moves in either direction.
//
// THE RECOGNISER IS THE RISK, NOT THE VERDICT. A derivation that finds zero
// constructors, or reads zero files, reports a perfectly clean tree and a
// perfectly matching roster if the roster is also empty. So the three stages
// are pinned separately and none of them is a bare count floor:
//
//  1. the CONSTRUCTOR set is pinned by identity — the exact nineteen
//     `<import path>.<func>` names — not by a count. A derivation that finds
//     eighteen, or nineteen different ones, fails. This stage is not
//     decorative: it is what caught the two method-shaped forwarders an
//     `fd.Recv != nil` skip had been dropping;
//  2. every derived constructor must actually contain the guard call, so a
//     constructor that exists but was never wired fails here;
//  3. the derived call-site population is pinned per kind WITH its count.
//
// WHAT THIS IS NOT. It is not #7328 arm (a). It does not touch
// internal/entkinds.ScanGo and does not feed the entkinds ledger. It exists
// only to keep one roster honest, and it deliberately UNDER-approximates the
// runtime population. Its restrictions, stated rather than implied, because a
// detector that is quietly narrower than its prose is how the method-shaped
// forwarders below went unnoticed:
//
//   - NON-LITERAL kind arguments are not resolved — a kind computed at
//     runtime, read from config, or spelled as a conversion;
//   - CLOSURES are not entered. A func literal that builds an EntityRecord
//     from its own parameter is the same defect shape and this sweep does not
//     see it;
//   - CARRIER structs are swept only in a package that demonstrably forwards
//     one into a constructor, and only when the composite literal names its
//     type. An ELIDED literal (`[]SecondaryEntity{{Kind: "X"}}`) is dropped.
//     No such form exists in internal/custom/java today, so the count is
//     right; the shape would be invisible here AND — per the carrier path's
//     runtime behaviour — invisible to the owning package's own suite, which
//     stops before the conversion;
//   - MULTI-HOP forwarding is not followed.
//
// METHODS ARE derived, as of the revision that found
// internal/extractors/javascript's emitWithRels/emitWithProps. Both the
// declaration side (`fd.Recv`) and the call side (`x.emitKind(...)` is a
// SelectorExpr, not an Ident) had to change; either one alone leaves the
// blindness in place.
//
// Everything on that list is the RUNTIME guard's job, and that division is why
// the guard is the enforcement and this file is only the roster's referent.

package types_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// pinnedConstructors is the exact set of production functions AND METHODS that
// forward one of their own parameters into types.EntityRecord.Kind. It is
// pinned BY NAME rather than by count because a count floor is satisfied by
// nineteen wrong answers.
//
// The value is the name of the parameter each one forwards; the derivation
// checks that too, because the argument index the call-site stage reads is the
// position of THAT parameter, and a derivation that picked the wrong parameter
// would read the wrong argument at every call site while still reporting a
// plausible-looking population.
var pinnedConstructors = map[string]string{
	"internal/custom/cpp.makeEntity":        "kind",
	"internal/custom/csharp.makeEntity":     "kind",
	"internal/custom/dart.makeEntity":       "kind",
	"internal/custom/elixir.makeEntity":     "kind",
	"internal/custom/golang.makeEntity":     "kind",
	"internal/custom/java.makeEntity":       "kind",
	"internal/custom/javascript.makeEntity": "kind",
	"internal/custom/kotlin.makeEntity":     "kind",
	"internal/custom/lua.makeEntity":        "kind",
	"internal/custom/php.makeEntity":        "kind",
	"internal/custom/python.entity":         "kind",
	"internal/custom/ruby.makeEntity":       "kind",
	"internal/custom/rust.makeEntity":       "kind",
	"internal/custom/scala.makeEntity":      "kind",
	"internal/custom/swift.makeEntity":      "kind",
	"internal/extractors/yaml.entity":       "kind",
	"internal/patterns.makeEntity":          "kind",
	// Methods. Identical in shape to the makeEntity family but for the
	// receiver, which is exactly why an `fd.Recv != nil` skip hid them, and
	// why they sat unwired in a DEFAULT-ON producer while the population count
	// stayed correct.
	"internal/extractors/javascript.(*extractor).emitWithRels":  "kind",
	"internal/extractors/javascript.(*extractor).emitWithProps": "kind",
}

type ctor struct {
	pkgDir   string // import-path-relative directory, e.g. "internal/custom/scala"
	recv     string // "" for a plain function, "(*extractor)" for a method
	funcName string
	param    string
	argIndex int
	wired    bool   // body calls types.ValidateProducedEntityKind
	site     string // the literal first argument that call passes, "" if absent
	file     string
}

func (c ctor) id() string {
	if c.recv != "" {
		return c.pkgDir + "." + c.recv + "." + c.funcName
	}
	return c.pkgDir + "." + c.funcName
}

// recvString renders a method receiver the way the guard's site labels spell
// it: "(*extractor)" or "(extractor)".
func recvString(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	switch t := fl.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return "(*" + id.Name + ")"
		}
	case *ast.Ident:
		return "(" + t.Name + ")"
	}
	return "(?)"
}

// TestProducedKindRoster7328_DerivedPopulationMatchesRoster is the whole point
// of the file; the helpers below it are shared with the negative controls.
func TestProducedKindRoster7328_DerivedPopulationMatchesRoster(t *testing.T) {
	root := internalRoot(t)
	fset := token.NewFileSet()
	files := parseTree(t, fset, root)
	if len(files) < 500 {
		t.Fatalf("derivation read only %d production .go files under %s; it is not "+
			"looking at the tree it claims to cover", len(files), root)
	}

	ctors := deriveConstructors(fset, files)

	// Stage 1 — the constructor set, pinned by identity.
	gotIDs := make([]string, 0, len(ctors))
	for _, c := range ctors {
		gotIDs = append(gotIDs, c.id())
	}
	sort.Strings(gotIDs)
	wantIDs := make([]string, 0, len(pinnedConstructors))
	for id := range pinnedConstructors {
		wantIDs = append(wantIDs, id)
	}
	sort.Strings(wantIDs)
	if strings.Join(gotIDs, "\n") != strings.Join(wantIDs, "\n") {
		t.Fatalf("derived entity-kind constructors differ from the pinned set.\n"+
			"A constructor that appears here is a new argument-passed kind producer and "+
			"needs the guard call; one that disappears was renamed or removed.\n"+
			"derived:\n  %s\npinned:\n  %s",
			strings.Join(gotIDs, "\n  "), strings.Join(wantIDs, "\n  "))
	}

	// Stage 2 — every derived constructor forwards the pinned parameter, is
	// actually wired to the guard, and passes its OWN identity as the site
	// label.
	for _, c := range ctors {
		if want := pinnedConstructors[c.id()]; c.param != want {
			t.Errorf("%s forwards parameter %q into EntityRecord.Kind, pinned as %q (%s)",
				c.id(), c.param, want, c.file)
		}
		if !c.wired {
			t.Errorf("%s does not call types.ValidateProducedEntityKind (%s): every kind "+
				"this constructor is handed reaches the graph unchecked", c.id(), c.file)
		}
		if c.site != c.id() {
			t.Errorf("%s passes site label %q to types.ValidateProducedEntityKind, want %q "+
				"(%s). The label is the only thing in the panic that says WHICH constructor "+
				"let the kind through, so a wrong or swapped one sends the reader to the "+
				"wrong file", c.id(), c.site, c.id(), c.file)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// Stage 3 — the off-vocabulary population.
	derived, sites, resolved := derivePopulation(t, fset, files, ctors)

	// Stage 3 — the CALL-SIDE resolver reads BOTH call shapes.
	//
	// Stages 1 and 2 are about declarations: they find a constructor and check
	// it is wired. Neither says the call-site stage can READ that
	// constructor's call sites, and the two are independently breakable. A
	// plain function is called as `makeEntity(...)` — an *ast.Ident — and a
	// method as `x.emitWithRels(...)` — an *ast.SelectorExpr. An earlier
	// revision handled only the first, so even once methods were derived their
	// call sites resolved to nothing.
	//
	// That blindness changed the derived POPULATION not at all, because the
	// two method forwarders pass only enum kinds. So the population diff
	// cannot grade it and this assertion must, which is the whole reason the
	// stage exists.
	//
	// It is pinned BY SHAPE rather than per constructor. A per-constructor
	// floor looks stricter and is wrong: internal/custom/lua.makeEntity has
	// ZERO literal call sites — every one of them passes
	// `string(types.EntityKindPattern)`, a conversion — so a blanket floor
	// would fail on a package that is behaving correctly, and the exception
	// list needed to keep it green would be another hand-maintained roster
	// with no referent. Shape is the axis the defect actually lives on.
	for _, shape := range []string{"ident", "selector"} {
		if resolved["shape:"+shape] == 0 {
			t.Errorf("the call-site stage resolved ZERO literal kind arguments through any "+
				"%s call. That branch of the resolver is dead, so every constructor called "+
				"that way contributes nothing to the derived population while stages 1 and "+
				"2 still report it as found and wired", shape)
		}
	}
	// The selector branch has exactly two constructors behind it, so name them
	// rather than resting on the aggregate: an aggregate stays green while one
	// of the two goes dark.
	for _, c := range ctors {
		if c.recv == "" {
			continue
		}
		if resolved[c.id()] == 0 {
			t.Errorf("the call-site stage resolved ZERO literal kind arguments through the "+
				"method %s (%s), though it has literal call sites in its own package",
				c.id(), c.file)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	roster := types.KnownUndeclaredProducedKinds()
	if diff := diffCounts(derived, roster); diff != "" {
		t.Fatalf("the known-undeclared roster in internal/types/produced_entity_kind.go no "+
			"longer describes the tree.\n%s\n\nsites:\n%s\n\n"+
			"A kind only in the DERIVED column is a new vocabulary defect: declare it in the "+
			"EntityKind enum or fix the producer. A kind only in the ROSTER column, or one "+
			"whose count moved, means the roster went stale — re-derive it, do not edit the "+
			"number to make this pass.", diff, formatSites(sites))
	}
}

// TestProducedKindRoster7328_DerivationIsNotVacuous is the positive control for
// the derivation itself. The test above compares two things this file computes;
// if the recogniser silently found nothing AND the roster were empty it would
// pass. These floors are deliberately far below the real values so that they
// report a broken recogniser, not ordinary tree drift.
func TestProducedKindRoster7328_DerivationIsNotVacuous(t *testing.T) {
	root := internalRoot(t)
	fset := token.NewFileSet()
	files := parseTree(t, fset, root)
	ctors := deriveConstructors(fset, files)
	if len(ctors) == 0 {
		t.Fatal("derived zero entity-kind constructors: the recogniser is inert and every " +
			"verdict this file produces is meaningless")
	}
	_, sites, resolved := derivePopulation(t, fset, files, ctors)
	if resolved["shape:ident"] == 0 || resolved["shape:selector"] == 0 {
		t.Fatalf("the call-site stage is inert for at least one call shape "+
			"(ident=%d selector=%d): the population it reports is vacuous for every "+
			"constructor called that way",
			resolved["shape:ident"], resolved["shape:selector"])
	}
	if len(sites) == 0 {
		t.Fatal("derived zero off-vocabulary sites: either the call-site stage is inert, or " +
			"the whole tree is clean — in which case the roster must be emptied and this " +
			"control retired deliberately")
	}
	if len(types.KnownUndeclaredProducedKinds()) == 0 {
		t.Fatal("the roster is empty while the derivation found off-vocabulary sites")
	}
}

// TestProducedKindRoster7328_StaleRosterEntryIsDetected proves the comparison
// fails in the direction that has no runtime symptom. The runtime guard cannot
// see a stale entry at all — an allowlisted kind nobody emits simply never
// arrives — so if diffCounts tolerated one, the roster could keep a dead row
// forever and nothing in the tree would notice.
//
// Both stale directions are exercised: an entry whose sites are gone, and an
// entry whose count drifted.
func TestProducedKindRoster7328_StaleRosterEntryIsDetected(t *testing.T) {
	derived := map[string]int{"SCOPE.Alpha": 3, "SCOPE.Beta": 1}

	t.Run("identical rosters agree", func(t *testing.T) {
		if d := diffCounts(derived, map[string]int{"SCOPE.Alpha": 3, "SCOPE.Beta": 1}); d != "" {
			t.Fatalf("a roster that matches the tree was reported as stale: %s", d)
		}
	})
	t.Run("roster entry with no sites left", func(t *testing.T) {
		d := diffCounts(derived, map[string]int{"SCOPE.Alpha": 3, "SCOPE.Beta": 1, "SCOPE.Gone": 2})
		if !strings.Contains(d, "SCOPE.Gone") {
			t.Fatalf("a roster entry the tree no longer produces was not reported: %q", d)
		}
	})
	t.Run("roster count drifted", func(t *testing.T) {
		d := diffCounts(derived, map[string]int{"SCOPE.Alpha": 99, "SCOPE.Beta": 1})
		if !strings.Contains(d, "SCOPE.Alpha") {
			t.Fatalf("a roster count that no longer matches the tree was not reported: %q", d)
		}
	})
	t.Run("new kind absent from the roster", func(t *testing.T) {
		d := diffCounts(derived, map[string]int{"SCOPE.Alpha": 3})
		if !strings.Contains(d, "SCOPE.Beta") {
			t.Fatalf("a kind the tree produces but the roster omits was not reported: %q", d)
		}
	})
}

// ---------------------------------------------------------------- derivation

func internalRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve internal/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "custom", "scala")); err != nil {
		t.Fatalf("derivation is not rooted at internal/ (%s): %v", root, err)
	}
	return root
}

type parsedFile struct {
	rel    string // "internal/..."
	pkgDir string
	file   *ast.File
}

func parseTree(t *testing.T, fset *token.FileSet, root string) []parsedFile {
	t.Helper()
	var out []parsedFile
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(filepath.Dir(root), p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		out = append(out, parsedFile{rel: rel, pkgDir: filepath.ToSlash(filepath.Dir(rel)), file: f})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
	return out
}

// deriveConstructors finds every top-level production function whose body
// builds a types.EntityRecord composite literal with `Kind:` set to one of the
// function's own parameters. That is the shape internal/entkinds.ScanGo cannot
// resolve, and it is derived rather than listed so a new one cannot be added
// without this test noticing.
func deriveConstructors(fset *token.FileSet, files []parsedFile) []ctor {
	var out []ctor
	for _, pf := range files {
		for _, d := range pf.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			// METHODS ARE INCLUDED. An earlier revision skipped them
			// (`fd.Recv != nil`, uncommented) and thereby missed
			// internal/extractors/javascript's emitWithRels/emitWithProps —
			// two forwarders identical in shape to makeEntity but for the
			// receiver, unwired, with 27 literal call sites in a DEFAULT-ON
			// producer. They emitted only enum kinds, so the population was
			// right and the detector was not.
			idx := map[string]int{}
			i := 0
			for _, fl := range fd.Type.Params.List {
				for _, n := range fl.Names {
					idx[n.Name] = i
					i++
				}
			}
			var found string
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if _, isLit := n.(*ast.FuncLit); isLit {
					// CLOSURES ARE NOT DERIVED. A func literal that forwards
					// its own parameter into an EntityRecord is the same
					// defect shape, and this stage does not look inside one.
					// The runtime guard still covers any closure that calls a
					// derived constructor; one that builds the EntityRecord
					// itself is outside this sweep, and the file header names
					// that as a restriction.
					return false
				}
				cl, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				sel, ok := cl.Type.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "EntityRecord" {
					return true
				}
				for _, e := range cl.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "Kind" {
						continue
					}
					if v, ok := kv.Value.(*ast.Ident); ok {
						if _, isParam := idx[v.Name]; isParam {
							found = v.Name
						}
					}
				}
				return true
			})
			if found == "" {
				continue
			}
			wired := false
			site := ""
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := ce.Fun.(*ast.SelectorExpr); ok &&
					sel.Sel.Name == "ValidateProducedEntityKind" {
					wired = true
					if len(ce.Args) > 0 {
						if v, ok := stringLit(ce.Args[0]); ok {
							site = v
						}
					}
				}
				return true
			})
			out = append(out, ctor{
				pkgDir: pf.pkgDir, recv: recvString(fd.Recv), funcName: fd.Name.Name,
				param: found, argIndex: idx[found], wired: wired, site: site, file: pf.rel,
			})
		}
	}
	return out
}

type siteRec struct {
	kind string
	loc  string
	how  string
}

// derivePopulation collects every off-vocabulary entity kind the tree produces
// through a derived constructor, in two stages:
//
//   - a STRING-LITERAL argument at the constructor's own kind-argument index,
//     resolved within the constructor's package (these constructors are all
//     unexported, so an out-of-package caller cannot exist);
//   - a `Kind: "<literal>"` field on a carrier struct that a constructor later
//     forwards. internal/custom/java's SecondaryEntity is the live instance.
//
// It deliberately UNDER-approximates: a kind computed at runtime, read from a
// config, or spelled as a non-literal expression is not derivable here at all.
// That is the runtime guard's job, and the division is the reason the guard is
// the enforcement and this file is only the roster's referent.
func derivePopulation(t *testing.T, fset *token.FileSet, files []parsedFile, ctors []ctor) (map[string]int, []siteRec, map[string]int) {
	t.Helper()
	byPkg := map[string]map[string]ctor{}
	for _, c := range ctors {
		if byPkg[c.pkgDir] == nil {
			byPkg[c.pkgDir] = map[string]ctor{}
		}
		if prev, dup := byPkg[c.pkgDir][c.funcName]; dup {
			t.Fatalf("two derived constructors in %s share the bare name %q (%s and %s). "+
				"The call-site stage keys on the bare name so that a method's SelectorExpr "+
				"call resolves, which means one of these would silently shadow the other "+
				"and every call site would be read at the survivor's argument index",
				c.pkgDir, c.funcName, prev.id(), c.id())
		}
		byPkg[c.pkgDir][c.funcName] = c
	}

	// calledCtor resolves a call expression to the derived constructor it
	// invokes, if any. Both call shapes must be handled: a plain function is
	// `makeEntity(...)` — an *ast.Ident — while a method is
	// `x.emitWithRels(...)` — an *ast.SelectorExpr. Handling only the first
	// was the second half of the method blindness: even once methods were
	// derived, their call sites would have resolved to nothing.
	//
	// Matching a SelectorExpr on the method NAME alone cannot collide across
	// packages, because every derived constructor is unexported and an
	// unexported name cannot be selected from another package. It could in
	// principle collide with a same-named field or method on another local
	// type; deriveConstructors' collision check below is what would surface
	// that as a failure rather than a silent miscount.
	calledCtor := func(local map[string]ctor, ce *ast.CallExpr) (ctor, string, bool) {
		if local == nil {
			return ctor{}, "", false
		}
		var name, shape string
		switch fn := ce.Fun.(type) {
		case *ast.Ident:
			name, shape = fn.Name, "ident"
		case *ast.SelectorExpr:
			name, shape = fn.Sel.Name, "selector"
		default:
			return ctor{}, "", false
		}
		c, ok := local[name]
		if !ok || c.argIndex >= len(ce.Args) {
			return ctor{}, "", false
		}
		return c, shape, ok
	}

	// A package only gets its carrier structs swept if it actually FORWARDS a
	// carrier's Kind into one of its constructors — i.e. some call passes
	// `<something>.Kind` at the kind-argument index. internal/custom/java does
	// (patterns_dispatch.go hands SecondaryEntity.Kind to makeEntity); nothing
	// else in the tree does. Deriving the condition is what stops this stage
	// from being a blanket `Kind:`-literal grep.
	forwards := map[string]bool{}
	for _, pf := range files {
		local := byPkg[pf.pkgDir]
		if local == nil {
			continue
		}
		ast.Inspect(pf.file, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			c, _, ok := calledCtor(local, ce)
			if !ok {
				return true
			}
			if sel, ok := ce.Args[c.argIndex].(*ast.SelectorExpr); ok && sel.Sel.Name == "Kind" {
				forwards[pf.pkgDir] = true
			}
			return true
		})
	}

	counts := map[string]int{}
	resolved := map[string]int{}
	var sites []siteRec
	record := func(kind, loc, how string) {
		if kind == "" || types.IsValidEntityKind(kind) {
			return
		}
		counts[kind]++
		sites = append(sites, siteRec{kind: kind, loc: loc, how: how})
	}

	for _, pf := range files {
		local := byPkg[pf.pkgDir]
		ast.Inspect(pf.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				c, shape, ok := calledCtor(local, x)
				if !ok {
					return true
				}
				if v, ok := stringLit(x.Args[c.argIndex]); ok {
					resolved[c.id()]++
					resolved["shape:"+shape]++
					record(v, pos(fset, pf.rel, x.Args[c.argIndex].Pos()), "arg to "+c.id())
				}
			case *ast.CompositeLit:
				// Carrier structs — a `Kind:` literal on some struct that a
				// constructor in the same package later forwards.
				// internal/custom/java's SecondaryEntity is the live instance.
				//
				// Scoped to packages that OWN a derived constructor, which is
				// what keeps the unrelated `Kind:` fields elsewhere in
				// internal/ (job kinds, edge kinds, CFG kinds) out. It is
				// deliberately NOT narrowed to `SCOPE.`-prefixed spellings: the
				// first run of this derivation did narrow that way and missed
				// `"Handler"`, seven un-prefixed literals in
				// internal/custom/java that the runtime guard then caught in
				// internal/extractors. An off-vocabulary kind is precisely the
				// thing that need not be spelled like the vocabulary.
				//
				// Two conditions keep it narrow, and both are derived:
				// the package must forward a carrier Kind into a constructor
				// at all, and the literal's type must be a package-LOCAL
				// identifier. The second is what excludes types.RelationshipRecord
				// and types.EntityRecord — qualified types, whose `Kind:`
				// fields are edge kinds and already-scannable entity kinds
				// respectively, neither of which belongs in this population.
				if !forwards[pf.pkgDir] {
					return true
				}
				if _, localType := x.Type.(*ast.Ident); !localType {
					return true
				}
				for _, e := range x.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "Kind" {
						continue
					}
					v, ok := stringLit(kv.Value)
					if !ok {
						continue
					}
					carrier := ""
					switch ty := x.Type.(type) {
					case *ast.Ident:
						carrier = ty.Name
					case *ast.SelectorExpr:
						carrier = ty.Sel.Name
					}
					if carrier == "EntityRecord" || carrier == "Entity" {
						continue // ScanGo already resolves this shape; #7328 is the rest
					}
					record(v, pos(fset, pf.rel, kv.Value.Pos()), "carrier "+carrier)
				}
			}
			return true
		})
	}
	return counts, sites, resolved
}

func stringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

func pos(fset *token.FileSet, rel string, p token.Pos) string {
	return fmt.Sprintf("%s:%d", rel, fset.Position(p).Line)
}

// diffCounts returns "" when derived and roster agree exactly, else a
// human-readable description naming every kind that differs in either
// direction.
func diffCounts(derived, roster map[string]int) string {
	keys := map[string]bool{}
	for k := range derived {
		keys[k] = true
	}
	for k := range roster {
		keys[k] = true
	}
	var all []string
	for k := range keys {
		all = append(all, k)
	}
	sort.Strings(all)
	var lines []string
	for _, k := range all {
		d, inD := derived[k]
		r, inR := roster[k]
		switch {
		case inD && !inR:
			lines = append(lines, fmt.Sprintf("  %-22s DERIVED=%d  ROSTER=absent", k, d))
		case !inD && inR:
			lines = append(lines, fmt.Sprintf("  %-22s DERIVED=absent  ROSTER=%d (stale)", k, r))
		case d != r:
			lines = append(lines, fmt.Sprintf("  %-22s DERIVED=%d  ROSTER=%d", k, d, r))
		}
	}
	return strings.Join(lines, "\n")
}

func formatSites(sites []siteRec) string {
	out := make([]string, 0, len(sites))
	for _, s := range sites {
		out = append(out, fmt.Sprintf("  %-22s %-46s %s", s.kind, s.loc, s.how))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}
