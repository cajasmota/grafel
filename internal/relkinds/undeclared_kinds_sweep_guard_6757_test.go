package relkinds_test

// undeclared_kinds_sweep_guard_6757_test.go — the repo-wide, BINDING ledger of
// relationship kinds that reach the graph without being in the vocabulary
// types.AllRelationshipKinds() declares (#6757, Arm B).
//
// # What #6757 measured
//
// types.IsValidRelationshipKind has zero non-test callers, so the relationship
// vocabulary is enforced by nothing: any string can be written to the graph as
// a kind. Arm A closed the half that is visible from inside internal/types —
// every RelationshipKind CONSTANT must appear in AllRelationshipKinds()
// (internal/types/kinds_enum_completeness_6757_test.go). That guard reads
// kinds.go, so it cannot see a kind that was never declared as a constant at
// all, which is how every kind in the ledger below reaches the graph.
//
// # Why the ledger, and not a fix
//
// This is a migration, not a one-liner. Some of these kinds probably deserve
// declaring, some are probably wrong, and that triage is separate work. What
// this file does is stop the population growing while the triage happens:
// anything NEW that is not on the ledger fails, immediately, at the site.
// Declaring the kinds here to empty the ledger would hide the population, which
// is the one thing #6757 exists to prevent.
//
// This guard deliberately does NOT wire IsValidRelationshipKind into the write
// path. That is Arm C, and it is constrained: graph/fbwriter's
// buildRelationship is the sole serialization leaf, but it is per-edge, hot and
// returns no error — it can log or drop, not reject. An erroring gate sits one
// level up.
//
// # The ratchet, and why it is spelled this way
//
// undeclaredKindsDeferredMax pins the ledger's EXACT size, so an author who
// trips the sweep cannot silence it by appending one correctly-spelled line.
// That hole was real, found and closed in #6739; the reference implementation
// is internal/registry/home_isolation_sweep_guard_6735_test.go, and
// TestUndeclaredKindsRatchetIsWired below asserts the two comparisons by
// OPERATOR and OPERAND, because an identifier walk survives
// `_ = undeclaredKindsDeferredMax`.
//
// # Inherited blind spots
//
// The sweep is only as sharp as relkinds.Scan, whose limits are recorded in
// scan.go: a kind computed at runtime rather than written as a source constant
// is reported as unresolved, not resolved — 87 such sites exist today, e.g.
// internal/custom/java/caching.go:243's `RelationshipType: relType`. Those are
// invisible to any static ledger and are the reason Arm C's runtime gate is
// still needed.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/relkinds"
	"github.com/cajasmota/grafel/internal/types"
)

// undeclaredKindsDeferred is the ledger: every relationship kind this
// repository writes to the graph while AllRelationshipKinds() omits it. The
// value is a family tag; see undeclaredFamily for what each family is and where
// it is declared.
//
// This list must only ever SHRINK, and that is ENFORCED, not requested — see
// undeclaredKindsDeferredMax and TestUndeclaredKindsDeferredOnlyShrinks.
//
// Every entry was produced by the live scan at 278301296; none was copied from
// the issue. The issue's own count was 19 and the scan finds 22 — see
// undeclaredFamily's "engine_workflow" and "engine_coupling" notes for the
// four the issue's manual enumeration missed.
var undeclaredKindsDeferred = map[string]string{
	// internal/custom/java — patterns_dispatch.go copies Relationship.
	// RelationshipType verbatim into graph.Relationship.Kind.
	"OWNS":         "custom_java", // 15 sites, e.g. internal/custom/java/android.go:277
	"HANDLED_BY":   "custom_java", // 6 sites, e.g. internal/custom/java/akka_http_routes.go:329
	"FETCHES_FROM": "custom_java", // internal/custom/java/gwt_rpc.go:135
	"BINDS_INPUT":  "custom_java", // internal/custom/java/struts_routes.go:732
	"BINDS_MODEL":  "custom_java", // internal/custom/java/struts_routes.go:793

	// internal/extractors — bare string literals in RelationshipRecord.Kind.
	"PORT_OF":    "extractor_literal", // internal/extractors/vhdl/extractor.go:528, :705
	"INDEXES":    "extractor_literal", // internal/extractors/sql/sql.go:456
	"FIRES":      "extractor_literal", // internal/extractors/sql/sql.go:621
	"DEFINED_ON": "extractor_literal", // internal/extractors/sql/sql.go:627

	// internal/engine flow subsystems — package-local consts, never registered.
	"ENTRY_POINT_OF":     "engine_flow", // internal/engine/process_flow.go:513
	"STEP_IN_PROCESS":    "engine_flow", // internal/engine/process_flow.go:523
	"SEED_OF_EVENT_FLOW": "engine_flow", // internal/engine/event_flow.go:343
	"STEP_IN_EVENT_FLOW": "engine_flow", // internal/engine/event_flow.go:353

	// internal/engine workflow edges — missed by the issue's manual scan.
	"STARTS_WORKFLOW":           "engine_workflow", // internal/engine/workflow_edges.go:248 (2 sites)
	"STEPFUNCTION_STEP_INVOKES": "engine_workflow", // internal/engine/workflow_edges.go:969 (4 sites)
	"EXECUTES_ACTIVITY":         "engine_workflow", // internal/engine/workflow_dag_edges.go:196 (2 sites)

	// internal/engine commit coupling — also missed by the issue's manual scan.
	"COMMIT_COUPLED": "engine_coupling", // internal/engine/commit_coupling_edges.go:313

	// Rule YAML — engine/detector.go compiles rr.Relationship unvalidated and
	// writes it straight into RelationshipRecord.Kind.
	"REGISTERED_ON": "rule_yaml", // flask, falcon, quart, sinatra
	"MOUNTS":        "rule_yaml", // cherrypy
	"DECORATES":     "rule_yaml", // fastapi
	"INSTALLED_IN":  "rule_yaml", // ktor
	"DEFINED_BY":    "rule_yaml", // symfony
}

// undeclaredKindsDeferredMax is the RATCHET on undeclaredKindsDeferred: the
// exact number of entries the ledger is allowed to hold.
//
// Without it, "this ledger only shrinks" is a sentence in a comment and nothing
// more — an author who trips the sweep silences it with one appended line, vet
// clean, suite green. The assertion is EXACT, not an upper bound, so it
// ratchets in both directions:
//
//   - The ledger GROWS → the sweep already named the site; this fires second
//     and says the fix is to declare the kind in internal/types/kinds.go, not
//     to raise a number.
//   - The ledger SHRINKS (a kind was declared, which is the point) → this fires
//     and requires the constant to come down with it, so the bar is never left
//     slack for a later append to slip under.
const undeclaredKindsDeferredMax = 22

// undeclaredFamily explains each family tag. A ledger entry without a stated
// reason is not a decision, it is a silence.
var undeclaredFamily = map[string]struct {
	Origin string
	Why    string
}{
	"custom_java": {
		Origin: relkinds.OriginGo,
		Why: "custom-Java extractors set the package-local Relationship.RelationshipType to a bare " +
			"string; internal/custom/java/patterns_dispatch.go:378 copies it verbatim into " +
			"types.RelationshipRecord.Kind, so it reaches the graph unexamined.",
	},
	"extractor_literal": {
		Origin: relkinds.OriginGo,
		Why: "extractors write a bare string literal into RelationshipRecord.Kind. Nothing in the " +
			"extractor pipeline consults IsValidRelationshipKind.",
	},
	"engine_flow": {
		Origin: relkinds.OriginGo,
		Why: "the process-flow and event-flow subsystems declare their kinds as package-local " +
			"constants in internal/engine (process_flow_kinds.go, event_flow.go) that were never " +
			"registered in internal/types, so Arm A's constant-completeness guard cannot see them.",
	},
	"engine_workflow": {
		Origin: relkinds.OriginGo,
		Why: "workflow / Step Functions edges, same package-local-const shape as engine_flow. NOT " +
			"in the issue's enumeration — its scan was manual and these three were missed, which " +
			"is precisely why the ledger is derived from a scan rather than transcribed.",
	},
	"engine_coupling": {
		Origin: relkinds.OriginGo,
		Why: "commit-coupling edges (internal/engine/commit_coupling_edges.go). Also missed by the " +
			"issue's manual enumeration.",
	},
	"rule_yaml": {
		Origin: relkinds.OriginRuleYAML,
		Why: "declared in a `relationship_rules:` entry under internal/engine/rules. " +
			"internal/engine/detector.go:172-186 compiles rr.Relationship without validating it and " +
			":509 writes it into RelationshipRecord.Kind. A Go-literal scan is structurally blind " +
			"to this half of the population.",
	},
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// here = <root>/internal/relkinds/undeclared_kinds_sweep_guard_6757_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
}

func scanRepo(t *testing.T) relkinds.Result {
	t.Helper()
	res, err := relkinds.Scan(repoRoot(t))
	if err != nil {
		t.Fatalf("scan repo: %v", err)
	}
	return res
}

// TestNoUndeclaredRelationshipKinds is the binding sweep. The comparison is
// EXACT SET EQUALITY between the kinds the scan finds outside
// AllRelationshipKinds() and the ledger: an unledgered kind fails, and a ledger
// entry the scan no longer produces fails too.
//
// The stale half is not bookkeeping. It is what makes a narrowed detector fail:
// a scan that stops reading rule YAML, or one file, or one mechanism, loses the
// entries that mechanism produced and this test names every one of them.
func TestNoUndeclaredRelationshipKinds(t *testing.T) {
	for kind, fam := range undeclaredKindsDeferred {
		if _, ok := undeclaredFamily[fam]; !ok {
			t.Fatalf("undeclaredKindsDeferred[%q] uses family %q, which has no entry in "+
				"undeclaredFamily — a ledger entry without a stated reason is not a decision, "+
				"it is a silence", kind, fam)
		}
	}

	declared := map[string]bool{}
	for _, k := range types.AllRelationshipKinds() {
		declared[string(k)] = true
	}

	res := scanRepo(t)
	seen := map[string]bool{}
	var unexpected []string
	for _, s := range res.Sites {
		if declared[s.Kind] {
			// Cross-check: the accessor and the predicate must agree, or the
			// ledger is measured against a different vocabulary than the one
			// Arm C would enforce with.
			if !types.IsValidRelationshipKind(s.Kind) {
				t.Errorf("%s: AllRelationshipKinds() contains %q but IsValidRelationshipKind rejects it",
					s, s.Kind)
			}
			continue
		}
		seen[s.Kind] = true
		if _, ledgered := undeclaredKindsDeferred[s.Kind]; !ledgered {
			unexpected = append(unexpected, s.String())
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("relationship kind(s) written to the graph that AllRelationshipKinds() does not "+
			"declare, and that are not on the ledger (#6757):\n  %s\n\n"+
			"IsValidRelationshipKind returns false for these, so the moment #6757 Arm C wires the "+
			"validator into the write path they are dropped or spammed on. Fix: declare the kind as "+
			"a RelationshipKind constant in internal/types/kinds.go AND add it to "+
			"AllRelationshipKinds() (Arm A's guard enforces the second half). Do NOT add it to "+
			"undeclaredKindsDeferred — that ledger only shrinks, and undeclaredKindsDeferredMax "+
			"will fail the moment you try.",
			strings.Join(unexpected, "\n  "))
	}

	var stale []string
	for kind := range undeclaredKindsDeferred {
		if !seen[kind] {
			stale = append(stale, kind)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("ledger entries the live scan no longer produces: %v\n\n"+
			"Either the kind was declared (thank you — delete the entry and lower "+
			"undeclaredKindsDeferredMax in the same change), or the SCAN stopped seeing a mechanism "+
			"it used to see, which is a hole in the detector and not a reason to delete the entry.",
			stale)
	}

	// Each ledgered kind must arrive through the mechanism its family claims.
	// A family tag that no longer matches the site means the ledger's own
	// account of the population has drifted from the population.
	for _, s := range res.Sites {
		fam, ok := undeclaredKindsDeferred[s.Kind]
		if !ok {
			continue
		}
		if want := undeclaredFamily[fam].Origin; s.Origin != want {
			t.Errorf("%s arrives via origin %q but its ledger family %q claims %q",
				s, s.Origin, fam, want)
		}
	}
}

// TestUndeclaredKindsDeferredOnlyShrinks is the ratchet itself.
func TestUndeclaredKindsDeferredOnlyShrinks(t *testing.T) {
	if len(undeclaredKindsDeferred) > undeclaredKindsDeferredMax {
		t.Fatalf("undeclaredKindsDeferred has GROWN to %d entries (ratchet: %d).\n\n"+
			"This ledger freezes a measured population; it is not a suppression list for new work. "+
			"If the sweep just named your kind, declare it in internal/types/kinds.go and register "+
			"it in AllRelationshipKinds(). Raising this constant is not that.",
			len(undeclaredKindsDeferred), undeclaredKindsDeferredMax)
	}
	if len(undeclaredKindsDeferred) < undeclaredKindsDeferredMax {
		t.Fatalf("undeclaredKindsDeferred has shrunk to %d entries but the ratchet still reads %d — "+
			"lower undeclaredKindsDeferredMax to %d in the same change.\n\n"+
			"Thank you for declaring a kind. The constant has to follow the ledger down, or the "+
			"slack it leaves behind is exactly the room a future append needs to pass unnoticed.",
			len(undeclaredKindsDeferred), undeclaredKindsDeferredMax, len(undeclaredKindsDeferred))
	}
}

// TestUndeclaredKindsRatchetIsWired pins the ratchet's EXISTENCE, not just its
// current verdict. Deleting TestUndeclaredKindsDeferredOnlyShrinks, or relaxing
// it to a one-sided bound, leaves every other test in this file green and
// returns the ledger to being append-able in one line.
//
// It asserts the two comparisons by OPERATOR and OPERAND: an identifier walk
// would be satisfied by `_ = undeclaredKindsDeferredMax`.
func TestUndeclaredKindsRatchetIsWired(t *testing.T) {
	const self = "undeclared_kinds_sweep_guard_6757_test.go"
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(filepath.Dir(here), self), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", self, err)
	}

	// The bar must be a const — a var could be reassigned at run time.
	constFound := false
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				if n.Name == "undeclaredKindsDeferredMax" {
					constFound = true
				}
			}
		}
	}
	if !constFound {
		t.Fatal("undeclaredKindsDeferredMax is no longer declared as a package-level const. " +
			"Without it nothing stops undeclaredKindsDeferred from growing, and the \"this ledger " +
			"only shrinks\" comment above it becomes prose asserting a property no code implements.")
	}

	ops := map[token.Token]bool{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil || !strings.HasPrefix(fd.Name.Name, "Test") {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			be, ok := n.(*ast.BinaryExpr)
			if !ok {
				return true
			}
			if isLenOfLedger(be.X) && isIdent(be.Y, "undeclaredKindsDeferredMax") {
				ops[be.Op] = true
			}
			// The mirrored spelling counts as the mirrored operator.
			if isLenOfLedger(be.Y) && isIdent(be.X, "undeclaredKindsDeferredMax") {
				switch be.Op {
				case token.LSS:
					ops[token.GTR] = true
				case token.GTR:
					ops[token.LSS] = true
				}
			}
			return true
		})
	}
	if !ops[token.GTR] {
		t.Fatal("no Test in this file compares len(undeclaredKindsDeferred) > undeclaredKindsDeferredMax. " +
			"That is the growth half of the ratchet — the one that stops a tripped sweep being " +
			"silenced with a one-line append.")
	}
	if !ops[token.LSS] {
		t.Fatal("no Test in this file compares len(undeclaredKindsDeferred) < undeclaredKindsDeferredMax. " +
			"That is the shrink half — without it the constant is an upper bound that stays slack " +
			"after a kind is declared, leaving room for a future append to pass unnoticed.")
	}
}

func isLenOfLedger(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !isIdent(call.Fun, "len") {
		return false
	}
	return isIdent(call.Args[0], "undeclaredKindsDeferred")
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// TestRepoSweepIsNotVacuous is the non-vacuity floor, and it is DERIVED rather
// than a magic number: this test walks the repository itself and requires the
// scan to have parsed exactly the files that walk finds. A scan that reads
// nothing fails, and — the case a `> 0` check would wave through — so does a
// scan narrowed to a SUBSET of the tree.
//
// The absolute floors underneath are a second, independent failure mode: if
// BOTH walks were broken the same way the equality would still hold, so the
// counts are also required to be of the right order of magnitude for this
// repository.
func TestRepoSweepIsNotVacuous(t *testing.T) {
	root := repoRoot(t)
	wantGo, wantYAML := countSourceFiles(t, root)

	res := scanRepo(t)
	if res.GoFilesParsed != wantGo {
		t.Errorf("scan parsed %d non-test .go files; an independent walk of %s finds %d. "+
			"The Go half of the sweep is not reading the repository, so every Go-declared kind "+
			"in the difference is unexamined.", res.GoFilesParsed, root, wantGo)
	}
	if res.YAMLFilesParsed != wantYAML {
		t.Errorf("scan parsed %d YAML files; an independent walk of %s finds %d. "+
			"The YAML half of the sweep is the half a Go-literal scan is blind to (#6757); "+
			"if it reads nothing, five ledgered kinds are enforced by nothing.",
			res.YAMLFilesParsed, root, wantYAML)
	}
	if wantGo < 1000 || wantYAML < 500 {
		t.Fatalf("the independent walk itself found only %d .go and %d .yaml files under %s; "+
			"both walks are broken, not just the scan", wantGo, wantYAML, root)
	}
}

// TestBothMechanismsObserveDeclaredKinds is the other half of the floor. The
// exact-equality ledger constrains the UNDECLARED population only, so a
// detector that happened to find those 22 strings and nothing else would pass
// it. This requires each mechanism to be seen carrying kinds that ARE declared
// — sentinels pinned by kind AND origin, so the YAML half cannot be satisfied
// by a Go site and vice versa.
func TestBothMechanismsObserveDeclaredKinds(t *testing.T) {
	res := scanRepo(t)

	byOrigin := map[string]map[string]int{}
	for _, s := range res.Sites {
		if byOrigin[s.Origin] == nil {
			byOrigin[s.Origin] = map[string]int{}
		}
		byOrigin[s.Origin][s.Kind]++
	}

	sentinels := []struct {
		Origin string
		Kind   string
		Where  string
	}{
		{relkinds.OriginGo, "CONTAINS", "the most-emitted Go kind in the tree"},
		{relkinds.OriginGo, "CALLS", "internal/engine call edges"},
		{relkinds.OriginGo, "IMPORTS", "import edges"},
		{relkinds.OriginRuleYAML, "ROUTES_TO", "internal/engine/rules/**/frameworks/*.yaml route rules"},
		{relkinds.OriginRuleYAML, "INJECTED_INTO", "internal/engine/rules/python/frameworks/fastapi.yaml"},
	}
	for _, s := range sentinels {
		if byOrigin[s.Origin][s.Kind] == 0 {
			t.Errorf("the %s half of the scan reports no %s site at all (%s). The mechanism is not "+
				"being read, so every kind declared through it is unenforced.", s.Origin, s.Kind, s.Where)
		}
		if !types.IsValidRelationshipKind(s.Kind) {
			t.Errorf("sentinel %s is no longer a declared kind; pick a sentinel that is, or this "+
				"floor is measuring the ledger instead of the vocabulary", s.Kind)
		}
	}

	// Every ledgered kind must still be produced by the mechanism it claims,
	// stated positively: the stale check above is a set difference, this is a
	// direct assertion that each half carries ledgered traffic.
	perFamily := map[string]int{}
	for _, s := range res.Sites {
		if fam, ok := undeclaredKindsDeferred[s.Kind]; ok {
			perFamily[fam]++
		}
	}
	for fam := range undeclaredFamily {
		if perFamily[fam] == 0 {
			t.Errorf("no site at all for ledger family %q; the scan lost a whole mechanism", fam)
		}
	}
}

// countSourceFiles is this test's OWN walk of the tree, deliberately written
// out rather than delegating to the package under test: a floor that asks the
// scanner how many files the scanner read is not a floor.
func countSourceFiles(t *testing.T, root string) (goFiles, yamlFiles int) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			// .claude holds full worktree checkouts of this same repository.
			case ".git", ".claude", "node_modules", "vendor", "testdata", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		switch {
		case strings.HasSuffix(name, "_test.go"):
			// out of scope: fixtures emit invented kinds
		case strings.HasSuffix(name, ".go"):
			goFiles++
		case strings.HasSuffix(name, ".yaml"), strings.HasSuffix(name, ".yml"):
			yamlFiles++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("independent walk of %s: %v", root, err)
	}
	return goFiles, yamlFiles
}

// TestLedgerIsDescribedByItsOwnScan keeps this file honest about how it was
// built: every ledger entry names a real, currently-scanned site, and the
// failure text a future author sees can point at one.
func TestLedgerIsDescribedByItsOwnScan(t *testing.T) {
	res := scanRepo(t)
	for kind := range undeclaredKindsDeferred {
		sites := res.SitesFor(kind)
		if len(sites) == 0 {
			t.Errorf("ledger entry %q has no site in the live scan", kind)
			continue
		}
		if sites[0].File == "" || sites[0].Line == 0 {
			t.Errorf("ledger entry %q resolved to a site with no location: %s", kind, sites[0])
		}
	}
	if t.Failed() {
		t.Log(fmt.Sprintf("scan summary: %d go files, %d yaml files, %d sites, %d unresolved",
			res.GoFilesParsed, res.YAMLFilesParsed, len(res.Sites), res.Unresolved()))
	}
}
