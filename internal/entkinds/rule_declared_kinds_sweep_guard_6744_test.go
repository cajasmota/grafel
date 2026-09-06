package entkinds_test

// rule_declared_kinds_sweep_guard_6744_test.go — the repo-wide, BINDING ledger
// of ENTITY kinds that internal/engine/rules/**/*.yaml declares while
// types.AllEntityKinds() does not (#6744).
//
// # What #6744 measured
//
// internal/types/producer_kinds_test.go enforces the entity vocabulary by
// parsing Go source. The rule YAML is a second producer family, and it is
// structurally invisible to that scan: internal/engine/detector.go:411 copies
// SourcePattern.EntityType straight into types.EntityRecord.Kind with no
// validation, so a rule file can mint any string it likes.
//
// The live scan of the rule tree finds 532 declaration sites and 27 distinct
// values. Exactly TWO of them were valid entity kinds — `Module` (valid only
// because types.EntityKindModule is itself un-prefixed) and `SCOPE.IngressHost`
// (added by this change). The other 25 were ledgered below; #6776 arms B5-B9
// declared ALL of them in types.AllEntityKinds() with their un-prefixed
// spellings unchanged (thirteen at B5, four at B6, four at B7, three at B8, one
// at B9).
//
// # THE LEDGER IS RETIRED, and this is the one number safe to state here
//
// Not "ratcheted to zero": the POPULATION is empty, and that is measured rather
// than inferred from an empty map. rule_ledger_retired_6776_test.go scans the
// rule tree and asserts, over a floor that catches a collapsed scan, that not
// one of the 532 sites names a kind AllEntityKinds() rejects. Every guard in
// this file that treated an empty ledger as evidence of a broken scan was
// correct to, while the ledger was the only evidence; it is not any more.
//
// DO NOT RESTATE A REMAINDER AS A NUMBER HERE — it went stale three arms
// running before this sentence replaced it. ruleDeclaredKindsDeferred below is
// the population, and it is pinned by exact set equality in both directions
// plus ruleDeclaredKindsDeferredMax, so reading it is always current and
// reading a comment is not.
//
// # Bound and unbound, and why the two halves of #6744 are not equal
//
// The issue's collision had one live side and one inert one, and the fix reads
// as more than it is unless that is said out loud:
//
//   - electron.yaml's three sites are under source_patterns[].entity_type,
//     which schema.go binds and detector.go copies into EntityRecord.Kind.
//     This is the ONLY site in the change whose graph output moves, and it is
//     therefore the one place a newly-invented kind would have been worst. It
//     uses `Module`, already the rule layer's spelling for this shape
//     (trpc.yaml:42, ktor.yaml:74) and already in the enum.
//   - kubernetes/extras.yaml's three sites are under entity_extraction and
//     k8s_resource_types, keys schema.go binds nowhere. They reach no code.
//     Renaming them to SCOPE.IngressHost aligns the documentation with #6451
//     and changes no graph.
//
// TestExternalAPICollisionIsSeparated asserts that asymmetry from the scan, so
// this paragraph cannot quietly go stale.
//
// # The namespace question, answered
//
// The issue asked whether the rule layer's un-prefixed names are a deliberate
// separate namespace or an accident. The scan settles it: it is an ACCIDENT,
// and a systemic one. 25 of 27 declared values were outside the enum when this
// guard landed — see the ledger below for what survives arms B5-B8 — and there
// is no code anywhere that treats `Route` differently from `SCOPE.Route` — they
// are simply written into EntityRecord.Kind and land in the graph as-is, which
// is why `Module` validates by coincidence rather than by design. Nothing
// documents an un-prefixed namespace, and nothing implements one.
//
// This verdict stands and is now the repository's only one. The competing note
// in internal/types/producer_kinds_test.go, which called the un-prefixed names
// "intentionally outside the validator set", was retracted by #6776 arm B3; the
// Go half of the same accident is ledgered alongside it in
// internal/types/producer_entity_kinds_6776_test.go.
//
// Fixing it is NOT this issue. Two costs, and #6776 separated them:
// making these kinds VALID is enum membership at the current spellings — no
// KindVocabularyVersion bump, no reindex, no stored-graph migration, no golden
// re-baseline. RENAMING them to SCOPE.* is the expensive half (~530 sites, the
// Kind of every rule-produced entity, a golden re-baseline and a migration
// story for stored graphs) and is a separate, separately-costed decision.
// Reported for separate filing; this guard freezes the population meanwhile, so
// the accident cannot grow.
//
// # Why a ledger, and not a fix
//
// Same stance as internal/relkinds' #6757 guard: declaring these kinds to empty
// the ledger would hide the population, which is the one thing the issue exists
// to prevent. What this file does is stop the population GROWING while the
// migration is triaged — anything new that is not on the ledger fails,
// immediately, naming the file and line.
//
// That stance was about the ORDER of the work, not about never finishing it,
// and #6776 kept it: the population was published, ranked and migrated arm by
// arm over B5-B9, each arm naming its sites, and only then did the ledger go
// empty. With the ratchet at 0 the sweep is now strictly stricter than it was
// — any rule-declared kind outside the enum fails, with no ledger row
// available to absorb it.
//
// # The ratchet, and why it is spelled this way
//
// ruleDeclaredKindsDeferredMax pins the ledger's EXACT size, so an author who
// trips the sweep cannot silence it by appending one correctly-spelled line.
// TestRuleDeclaredKindsRatchetIsWired asserts the two comparisons by OPERATOR
// and OPERAND, because an identifier-presence walk survives
// `_ = ruleDeclaredKindsDeferredMax`.
//
// # Scope of the ledger
//
// The ledger covers OriginRuleYAML sites only — that is #6744's population. The
// Go half is scanned too (and is required by the floors below to be carrying
// traffic), but the Go-declared entity vocabulary is internal/types'
// producer_kinds_test.go's business, and folding its residue in here would make
// this file the owner of a migration it did not measure.

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

	"github.com/cajasmota/grafel/internal/entkinds"
	"github.com/cajasmota/grafel/internal/types"
)

// ruleDeclaredKindsDeferred is the ledger: every entity kind a rule YAML file
// declares while AllEntityKinds() omits it. The value is a family tag; see
// ruleDeclaredFamily.
//
// Every entry was produced by the live scan, not transcribed from the issue —
// the issue named three sites and the scan found 532, of which 25 distinct
// values are invalid. This list must only ever SHRINK, and that is ENFORCED.
// RETIRED, and EMPTY is the finished state rather than a hole. #6776 arm B9
// declared the last entry (`Endpoint`, the Electron IPC kind) in
// internal/types/kinds.go, and the population this ledger stood in for is now
// measured directly by rule_ledger_retired_6776_test.go: 532 rule-YAML sites,
// zero naming a kind AllEntityKinds() rejects. That measurement is what makes
// the emptiness readable as "nothing left to defer" rather than "the scan
// stopped looking", which is the only reading an empty map could otherwise
// carry.
//
// The map is kept, not deleted, so the next drift lands on a mechanism instead
// of inventing one — and so the ratchet below can hold it at zero.
var ruleDeclaredKindsDeferred = map[string]string{}

// ruleDeclaredKindsDeferredMax is the RATCHET on ruleDeclaredKindsDeferred: the
// EXACT number of entries the ledger is allowed to hold.
//
// Without it, "this ledger only shrinks" is a sentence in a comment and nothing
// more — an author who trips the sweep silences it with one appended line, vet
// clean, suite green. The assertion is exact, so it ratchets in both
// directions:
//
//   - GROWS → the sweep already named the site; this fires second and says the
//     fix is to declare the kind in internal/types/kinds.go, not to raise a
//     number.
//   - SHRINKS (a kind was declared or removed, which is the point) → this fires
//     and requires the constant to come down with it, so the bar is never left
//     slack for a later append to slip under.
const ruleDeclaredKindsDeferredMax = 0

// ruleDeclaredFamily explains each family tag. A ledger entry without a stated
// reason is not a decision, it is a silence.
// EMPTY because the ledger is. The one family it held, `rule_namespace`,
// explained the un-prefixed rule-YAML spelling as an accident rather than a
// second namespace; #6776 acted on that reading across arms B4-B9 and there is
// now no deferred kind for it to explain. It is deliberately not left standing
// with no rows: TestYAMLHalfObservesDeclaredKinds requires live YAML traffic
// for every family here, so an unused family is either a red suite or, once
// that check goes unreachable, a documented slot inviting a row with no
// decision behind it. TestRuleDeclaredLedger6776_EveryFamilyIsUsedByARow is the
// standing check on that direction.
var ruleDeclaredFamily = map[string]struct {
	Origin string
	Why    string
}{}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// here = <root>/internal/entkinds/rule_declared_kinds_sweep_guard_6744_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
}

func scanRepo(t *testing.T) entkinds.Result {
	t.Helper()
	res, err := entkinds.Scan(repoRoot(t))
	if err != nil {
		t.Fatalf("scan repo: %v", err)
	}
	return res
}

// yamlSites returns the OriginRuleYAML half of a scan.
func yamlSites(res entkinds.Result) []entkinds.Site {
	var out []entkinds.Site
	for _, s := range res.Sites {
		if s.Origin == entkinds.OriginRuleYAML {
			out = append(out, s)
		}
	}
	return out
}

// TestNoUndeclaredRuleEntityKinds is the binding sweep. The comparison is EXACT
// SET EQUALITY between the invalid kinds the YAML half finds and the ledger: an
// unledgered kind fails, and a ledger entry the scan no longer produces fails
// too.
//
// The stale half is not bookkeeping. It is what makes a NARROWED scan fail: a
// scan that stops reading one language directory, or one YAML key, or the rule
// tree entirely, loses the entries that half produced, and this test names
// every one of them.
func TestNoUndeclaredRuleEntityKinds(t *testing.T) {
	for kind, fam := range ruleDeclaredKindsDeferred {
		if _, ok := ruleDeclaredFamily[fam]; !ok {
			t.Fatalf("ruleDeclaredKindsDeferred[%q] uses family %q, which has no entry in "+
				"ruleDeclaredFamily — a ledger entry without a stated reason is not a decision, "+
				"it is a silence", kind, fam)
		}
	}

	res := scanRepo(t)
	sites := yamlSites(res)
	if len(sites) == 0 {
		t.Fatalf("the rule-YAML half of the scan produced NO sites at all "+
			"(parsed %d go files / %d yaml files, found %d sites total). A silently-empty scan "+
			"reports every rule file clean; that is the exact failure #6744 exists to prevent.",
			res.GoFilesParsed, res.YAMLFilesParsed, len(res.Sites))
	}

	seen := map[string]bool{}
	var unexpected []string
	for _, s := range sites {
		if types.IsValidEntityKind(s.Kind) {
			continue
		}
		seen[s.Kind] = true
		if _, ledgered := ruleDeclaredKindsDeferred[s.Kind]; !ledgered {
			unexpected = append(unexpected, s.String())
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("rule YAML declares entity kind(s) that types.AllEntityKinds() does not, and that "+
			"are not on the ledger (#6744):\n  %s\n\n"+
			"IsValidEntityKind returns false for these. internal/engine/detector.go writes the "+
			"value straight into EntityRecord.Kind, so it reaches the graph unexamined — this is "+
			"how three rule sites kept emitting the kind #6451 had retired. Fix: use a kind "+
			"declared in internal/types/kinds.go. Do NOT add it to ruleDeclaredKindsDeferred — "+
			"that ledger only shrinks, and ruleDeclaredKindsDeferredMax will fail the moment you "+
			"try.",
			strings.Join(unexpected, "\n  "))
	}

	var stale []string
	for kind := range ruleDeclaredKindsDeferred {
		if !seen[kind] {
			stale = append(stale, kind)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("ledger entries the live scan no longer produces: %v\n\n"+
			"Either the kind was retired from the rule files (thank you — delete the entry and "+
			"lower ruleDeclaredKindsDeferredMax in the same change), or the SCAN stopped seeing a "+
			"mechanism it used to see, which is a hole in the detector and not a reason to delete "+
			"the entry. Scan summary: %d go files, %d yaml files, %d sites, %d unresolved.",
			stale, res.GoFilesParsed, res.YAMLFilesParsed, len(res.Sites), res.Unresolved())
	}

	// Each ledgered kind must arrive through the origin its family claims.
	// Scoped to the YAML half deliberately: the ledger is #6744's population,
	// and `Route` / `Migration` are ALSO emitted by Go producers
	// (internal/engine/django_routes.go:553 and friends) whose vocabulary is
	// internal/types/producer_kinds_test.go's business, not this file's.
	for _, s := range sites {
		fam, ok := ruleDeclaredKindsDeferred[s.Kind]
		if !ok {
			continue
		}
		if want := ruleDeclaredFamily[fam].Origin; s.Origin != want {
			t.Errorf("%s arrives via origin %q but its ledger family %q claims %q",
				s, s.Origin, fam, want)
		}
	}
}

// TestRuleDeclaredKindsRatchetOnlyShrinks is the ratchet itself.
func TestRuleDeclaredKindsRatchetOnlyShrinks(t *testing.T) {
	if len(ruleDeclaredKindsDeferred) > ruleDeclaredKindsDeferredMax {
		t.Fatalf("ruleDeclaredKindsDeferred has GROWN to %d entries (ratchet: %d).\n\n"+
			"This ledger freezes a measured population; it is not a suppression list for new work. "+
			"If the sweep just named your kind, use one declared in internal/types/kinds.go. "+
			"Raising this constant is not that.",
			len(ruleDeclaredKindsDeferred), ruleDeclaredKindsDeferredMax)
	}
	if len(ruleDeclaredKindsDeferred) < ruleDeclaredKindsDeferredMax {
		t.Fatalf("ruleDeclaredKindsDeferred has shrunk to %d entries but the ratchet still reads "+
			"%d — lower ruleDeclaredKindsDeferredMax to %d in the same change.\n\n"+
			"Thank you for removing a kind. The constant has to follow the ledger down, or the "+
			"slack it leaves behind is exactly the room a future append needs to pass unnoticed.",
			len(ruleDeclaredKindsDeferred), ruleDeclaredKindsDeferredMax, len(ruleDeclaredKindsDeferred))
	}
}

// TestRuleDeclaredKindsRatchetIsWired pins the ratchet's EXISTENCE, not just its
// current verdict. Deleting TestRuleDeclaredKindsRatchetOnlyShrinks, or relaxing
// it to a one-sided bound, leaves every other test in this file green and
// returns the ledger to being append-able in one line.
//
// It asserts the two comparisons by OPERATOR and OPERAND: an identifier walk
// would be satisfied by `_ = ruleDeclaredKindsDeferredMax`.
//
// KNOWN RESIDUAL WEAKNESS: this checks that the comparison EXISTS, not that it
// does anything. An empty-bodied `if len(ruleDeclaredKindsDeferred) > ruleDeclaredKindsDeferredMax {}`
// placed in any Test in this file satisfies both arms while the real ratchet is
// gutted. Closing it means asserting the comparison guards a t.Fatal/t.Error in
// the same branch. Left open deliberately and recorded rather than discovered.
func TestRuleDeclaredKindsRatchetIsWired(t *testing.T) {
	const self = "rule_declared_kinds_sweep_guard_6744_test.go"
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
				if n.Name == "ruleDeclaredKindsDeferredMax" {
					constFound = true
				}
			}
		}
	}
	if !constFound {
		t.Fatal("ruleDeclaredKindsDeferredMax is no longer declared as a package-level const. " +
			"Without it nothing stops ruleDeclaredKindsDeferred from growing, and the \"this " +
			"ledger only shrinks\" comment above it becomes prose asserting a property no code " +
			"implements.")
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
			if isLenOfLedger(be.X) && isIdent(be.Y, "ruleDeclaredKindsDeferredMax") {
				ops[be.Op] = true
			}
			// The mirrored spelling counts as the mirrored operator.
			if isLenOfLedger(be.Y) && isIdent(be.X, "ruleDeclaredKindsDeferredMax") {
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
		t.Fatal("no Test in this file compares len(ruleDeclaredKindsDeferred) > " +
			"ruleDeclaredKindsDeferredMax. That is the growth half of the ratchet — the one that " +
			"stops a tripped sweep being silenced with a one-line append.")
	}
	if !ops[token.LSS] {
		t.Fatal("no Test in this file compares len(ruleDeclaredKindsDeferred) < " +
			"ruleDeclaredKindsDeferredMax. That is the shrink half — without it the constant is " +
			"an upper bound that stays slack after a kind is removed, leaving room for a future " +
			"append to pass unnoticed.")
	}
}

func isLenOfLedger(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !isIdent(call.Fun, "len") {
		return false
	}
	return isIdent(call.Args[0], "ruleDeclaredKindsDeferred")
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// TestRepoSweepIsNotVacuous is the non-vacuity floor, and it is DERIVED rather
// than a magic number: this test walks the repository itself and requires the
// scan to have parsed exactly the files that walk finds. A scan that reads
// nothing fails, and — the case a `> 0` check would wave through — so does a
// scan narrowed to a SUBSET of the tree, e.g. one that skips a language
// directory.
func TestRepoSweepIsNotVacuous(t *testing.T) {
	root := repoRoot(t)
	wantGo, wantYAML := countSourceFiles(t, root)

	res := scanRepo(t)
	if res.GoFilesParsed != wantGo {
		t.Errorf("scan parsed %d non-test .go files; an independent walk of %s finds %d. "+
			"The Go half of the sweep is not reading the repository.", res.GoFilesParsed, root, wantGo)
	}
	if res.YAMLFilesParsed != wantYAML {
		t.Errorf("scan parsed %d YAML files; an independent walk of %s finds %d. "+
			"The YAML half is the half a Go-literal scan is blind to (#6744); if it reads a subset "+
			"of the tree, every rule file in the difference is unexamined and a kind declared "+
			"there reaches the graph with nothing to catch it.", res.YAMLFilesParsed, root, wantYAML)
	}
	if wantGo < 1000 || wantYAML < 500 {
		t.Fatalf("the independent walk itself found only %d .go and %d .yaml files under %s; "+
			"both walks are broken, not just the scan", wantGo, wantYAML, root)
	}
}

// countSourceFiles is this test's OWN walk of the tree, deliberately written
// out rather than delegating to the package under test: a floor that asks the
// scanner how many files the scanner read is not a floor.
//
// For the same reason this list is NOT internal/repowalk.SkippedDir (#6846),
// even though it is character-for-character the same set and entkinds.Scan now
// uses that shared copy. Sharing it here would put one predicate on both sides
// of the equality above. Measured on the twin of this test in internal/relkinds,
// widening the shared list with "security","atomicfile","registry":
//
//	independent countSourceFiles (as shipped)  FAIL — "scan parsed 2079 non-test
//	                                           .go files; an independent walk
//	                                           finds 2086"
//	countSourceFiles using SkippedDir          PASS — while 7 real production Go
//	                                           files went unread by BOTH sides
//
// The absolute wantGo/wantYAML floors below are a backstop, but a coarse one:
// they catch LARGE widenings only. The independence is what makes a small one
// visible. Keep the two copies in step by hand, and let them disagree loudly.
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

// TestYAMLHalfObservesDeclaredKinds is the NON-VACUITY proof #6744 asks for
// directly: the guard must be seen resolving a kind that is declared in YAML
// and is a VALID entity kind. Without it, every positive assertion in this file
// could be satisfied by a scan that only ever reads Go — the exact shape of the
// gap the issue was filed about, reproduced inside its own guard.
//
// The sentinels are pinned by kind AND origin AND file, so a Go site cannot
// stand in for a YAML one.
func TestYAMLHalfObservesDeclaredKinds(t *testing.T) {
	res := scanRepo(t)

	type key struct{ origin, kind string }
	byKey := map[key][]entkinds.Site{}
	for _, s := range res.Sites {
		byKey[key{s.Origin, s.Kind}] = append(byKey[key{s.Origin, s.Kind}], s)
	}

	sentinels := []struct {
		Origin, Kind, FileHint, Where string
	}{
		{entkinds.OriginRuleYAML, "Module", "internal/engine/rules/javascript_typescript/frameworks/electron.yaml",
			"the BOUND half of the #6744 separation — the only site whose graph output changed"},
		{entkinds.OriginRuleYAML, "SCOPE.IngressHost", "internal/engine/rules/kubernetes/extras.yaml",
			"the unbound half of the #6744 separation, in k8s_resource_types / entity_extraction"},
		{entkinds.OriginGo, "SCOPE.Function", "",
			"the most-emitted Go entity kind in the tree"},
		{entkinds.OriginGo, "SCOPE.ExternalEndpoint", "",
			"the #6451 split's HTTP-client half"},
	}
	for _, s := range sentinels {
		got := byKey[key{s.Origin, s.Kind}]
		if len(got) == 0 {
			t.Errorf("the %s half of the scan reports no %s site at all (%s). The mechanism is not "+
				"being read, so every kind declared through it is unenforced.", s.Origin, s.Kind, s.Where)
			continue
		}
		if !types.IsValidEntityKind(s.Kind) {
			t.Errorf("sentinel %q is no longer a declared entity kind; pick a sentinel that is, or "+
				"this floor measures the ledger instead of the vocabulary", s.Kind)
		}
		if s.FileHint == "" {
			continue
		}
		found := false
		for _, site := range got {
			if site.File == s.FileHint {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is declared, but not in %s — the sentinel no longer pins the site it names",
				s.Kind, s.FileHint)
		}
	}

	// Positive, per-mechanism traffic for the ledger itself: the YAML half must
	// carry ledgered sites, or the exact-set comparison above is measuring an
	// empty set against an empty set.
	//
	// UNREACHABLE TODAY, and deliberately kept: ruleDeclaredFamily is empty
	// since #6776 arm B9 retired the ledger, so this loop drives nothing. It is
	// the check that fires again the moment a family is reintroduced with no
	// live YAML site behind it. What it can no longer do — object to a family
	// with no ledger ROW — is carried by
	// TestRuleDeclaredLedger6776_EveryFamilyIsUsedByARow, and the sentinels
	// above still hold the YAML half of the scan to real, located traffic
	// without depending on the ledger at all.
	perFamily := map[string]int{}
	for _, s := range yamlSites(res) {
		if fam, ok := ruleDeclaredKindsDeferred[s.Kind]; ok {
			perFamily[fam]++
		}
	}
	for fam := range ruleDeclaredFamily {
		if perFamily[fam] == 0 {
			t.Errorf("no rule-YAML site at all for ledger family %q; the scan lost a mechanism", fam)
		}
	}
}

// TestExternalAPICollisionIsSeparated pins the #6744 fix itself: the two
// producer families that shared the name `ExternalAPI` no longer share a name,
// and the retired kind is gone from the rule layer entirely.
//
// Reverting either rename fails here — the k8s and electron kind sets overlap
// again, and the retired name reappears.
func TestExternalAPICollisionIsSeparated(t *testing.T) {
	res := scanRepo(t)

	const (
		k8sFile      = "internal/engine/rules/kubernetes/extras.yaml"
		electronFile = "internal/engine/rules/javascript_typescript/frameworks/electron.yaml"
	)

	// The retired kind, in either spelling, must not be declared anywhere.
	for _, retired := range []string{"ExternalAPI", "SCOPE.ExternalAPI"} {
		if sites := res.SitesFor(retired); len(sites) > 0 {
			var msgs []string
			for _, s := range sites {
				msgs = append(msgs, s.String())
			}
			t.Errorf("%q is still declared:\n  %s\n\n#6451 retired it because one name carried two "+
				"unrelated meanings under two address dialects. Use SCOPE.ExternalEndpoint (an "+
				"outbound HTTP call target, URL-path dialect) or SCOPE.IngressHost (a Kubernetes "+
				"ingress hostname).", retired, strings.Join(msgs, "\n  "))
		}
	}

	kindsIn := func(file string) map[string]bool {
		out := map[string]bool{}
		for _, s := range res.Sites {
			if s.File == file {
				out[s.Kind] = true
			}
		}
		return out
	}
	k8s, electron := kindsIn(k8sFile), kindsIn(electronFile)
	if len(k8s) == 0 || len(electron) == 0 {
		t.Fatalf("scan found no declarations in %s (%d) or %s (%d) — the files moved or the scan "+
			"is not reading them, and this test is measuring nothing",
			k8sFile, len(k8s), electronFile, len(electron))
	}

	// The two specific external-surface kinds must be the separated ones.
	if !k8s["SCOPE.IngressHost"] {
		t.Errorf("%s no longer declares SCOPE.IngressHost. Its LoadBalancer / ExternalName / "+
			"Ingress rows are the hostname dialect #6451 minted that kind for.", k8sFile)
	}
	if !electron["Module"] {
		t.Errorf("%s no longer declares Module for its native C++ addon loaders "+
			"(bindings, node-gyp-build, *.node). This is the BOUND half of the separation and the "+
			"only site in #6744 whose graph output changed, so it is also the one place the "+
			"replacement had to be a kind types.AllEntityKinds() declares rather than a new mint.",
			electronFile)
	}

	// The separation itself: neither file may declare the OTHER's half. A
	// revert of either rename collapses both families back onto one name and
	// trips this — which is what a blanket "these two files share no kind"
	// check could not do honestly, since they legitimately share `Service`
	// (a k8s Service resource and an Electron main-process marker are both
	// services, and that overlap predates and outlives #6744).
	if electron["SCOPE.IngressHost"] {
		t.Errorf("%s declares SCOPE.IngressHost. That kind is #6451's Kubernetes ingress HOSTNAME "+
			"dialect; an Electron surface on it re-creates the collision #6744 removed.", electronFile)
	}
	for _, shared := range []string{"ExternalAPI", "SCOPE.ExternalAPI", "SCOPE.ExternalEndpoint"} {
		if electron[shared] && k8s[shared] {
			t.Errorf("%s and %s both declare %q — the one-kind-two-meanings shape #6451 split and "+
				"#6744 removed from the rule layer.", electronFile, k8sFile, shared)
		}
	}

	// The ASYMMETRY, asserted rather than described. "We separated the
	// collision" overstates the change if it is not stated that only one side
	// was ever live: the electron sites are bound (detector.go copies
	// source_patterns[].entity_type into EntityRecord.Kind), the kubernetes
	// ones are not (nothing in Go reads entity_extraction or
	// k8s_resource_types). Pinning it here means the claim in the commit
	// message, the two rule files' comments and this package's doc are all
	// checked against the scan instead of being repeated prose.
	boundBy := func(file, kind string) (bound, unbound int) {
		for _, s := range res.Sites {
			if s.File != file || s.Kind != kind {
				continue
			}
			if s.Bound {
				bound++
			} else {
				unbound++
			}
		}
		return bound, unbound
	}
	if b, u := boundBy(electronFile, "Module"); b != 3 || u != 0 {
		t.Errorf("electron's Module sites are %d bound / %d unbound, want 3/0. This is the half "+
			"that reaches EntityRecord.Kind; if it stopped being bound, #6744's only live effect "+
			"is gone and every description of the change is now wrong.", b, u)
	}
	if b, u := boundBy(k8sFile, "SCOPE.IngressHost"); b != 0 || u != 3 {
		t.Errorf("kubernetes' SCOPE.IngressHost sites are %d bound / %d unbound, want 0/3. If "+
			"internal/engine/schema.go has learned to bind entity_extraction or "+
			"k8s_resource_types, these rows became live producers and the \"documentation only\" "+
			"notes in extras.yaml are now false.", b, u)
	}
}

// TestBoundPathsMirrorEngineSchema keeps entkinds' Bound flag honest. The
// package decides "does this declaration reach EntityRecord.Kind" from a
// hard-coded path list rather than a schema decode, on purpose — an unknown key
// must be reported, not dropped. The cost is a mirror that can drift, so the
// mirror is derived here from internal/engine/schema.go's actual yaml tags and
// compared.
func TestBoundPathsMirrorEngineSchema(t *testing.T) {
	schema := filepath.Join(repoRoot(t), "internal", "engine", "schema.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, schema, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", schema, err)
	}

	// typeTag[T][field-type-name] = yaml key, and entityKeyIn[T] = the yaml key
	// on T whose name is entity_type.
	structs := map[string]*ast.StructType{}
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		if st, ok := ts.Type.(*ast.StructType); ok {
			structs[ts.Name.Name] = st
		}
		return true
	})
	if len(structs) == 0 {
		t.Fatalf("no struct types found in %s; this test is measuring nothing", schema)
	}

	yamlTag := func(fld *ast.Field) string {
		if fld.Tag == nil {
			return ""
		}
		raw := strings.Trim(fld.Tag.Value, "`")
		i := strings.Index(raw, `yaml:"`)
		if i < 0 {
			return ""
		}
		rest := raw[i+len(`yaml:"`):]
		j := strings.Index(rest, `"`)
		if j < 0 {
			return ""
		}
		return strings.Split(rest[:j], ",")[0]
	}

	// Walk FrameworkRule's fields and derive, for each one that can carry an
	// entity kind, the YAML path that kind would sit at.
	//
	// The derivation is an ALLOW-LIST of field shapes, and an unrecognised
	// shape is a FATAL, not a skip. That is the whole point: an earlier
	// version understood only `[]Ident`, so binding a new rule block as
	// `[]*Probe`, `map[string]Probe` or a plain struct derived nothing, left
	// `want` equal to the unchanged mirror, and kept this test green while a
	// live producer was described as inert — the exact failure the message
	// below names.
	root, ok := structs["FrameworkRule"]
	if !ok {
		t.Fatalf("internal/engine/schema.go no longer declares FrameworkRule; entkinds' boundPaths " +
			"mirror is anchored to it")
	}

	// shapeOf reduces a field type to (element type name, path suffix).
	// Suffix "[]" marks a collection step, matching entkinds' path spelling;
	// "" marks a direct struct field. ok=false means the shape is one this
	// deriver does not understand, and the caller fatals rather than skipping.
	var shapeOf func(ast.Expr) (name, suffix string, ok bool)
	shapeOf = func(e ast.Expr) (string, string, bool) {
		switch t := e.(type) {
		case *ast.Ident:
			return t.Name, "", true
		case *ast.StarExpr:
			return shapeOf(t.X)
		case *ast.ArrayType:
			n, _, ok := shapeOf(t.Elt)
			return n, "[]", ok
		case *ast.MapType:
			n, _, ok := shapeOf(t.Value)
			return n, "[]", ok
		case *ast.SelectorExpr:
			// A type from another package. schema.go declares its rule structs
			// locally today; a cross-package one would not be in `structs`, so
			// it lands in the not-a-local-struct branch below with its real
			// name rather than being silently reduced to nothing.
			return t.Sel.Name, "", true
		}
		return "", "", false
	}

	var want []string
	for _, fld := range root.Fields.List {
		outer := yamlTag(fld)
		if outer == "" {
			continue
		}
		elem, suffix, ok := shapeOf(fld.Type)
		if !ok {
			t.Fatalf("FrameworkRule field with yaml tag %q has type shape %T, which this deriver "+
				"does not understand. Teach shapeOf about it — do NOT let it fall through. A "+
				"skipped shape derives no path, leaves `want` matching the unchanged mirror, and "+
				"passes this test while the producer it binds is reported as inert.",
				outer, fld.Type)
		}
		inner, isStruct := structs[elem]
		if !isStruct {
			// A named non-struct: a string, a bool, a defined scalar. It has
			// no fields, so it declares no entity_type. This is the only
			// legitimate skip, and it is keyed on schema.go's own type
			// declarations rather than on the syntax of the field.
			continue
		}
		for _, ifld := range inner.Fields.List {
			if tag := yamlTag(ifld); tag == "entity_type" || tag == "entity_mapping" {
				want = append(want, outer+suffix+"."+tag)
			}
		}
	}
	sort.Strings(want)

	got := entkinds.BoundPaths()
	if len(want) == 0 {
		t.Fatal("derived zero bound paths from internal/engine/schema.go; the derivation broke, " +
			"and an empty expectation would pass against any mirror")
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("entkinds.BoundPaths() = %v, but internal/engine/schema.go binds %v.\n\n"+
			"The mirror has drifted. A path the schema binds but entkinds does not is reported as "+
			"Bound=false — a live producer described as inert.", got, want)
	}
}

// TestLedgerIsDescribedByItsOwnScan keeps this file honest about how it was
// built: every ledger entry names a real, currently-scanned site with a
// location, so the failure text a future author sees can point at one.
func TestLedgerIsDescribedByItsOwnScan(t *testing.T) {
	res := scanRepo(t)
	for kind := range ruleDeclaredKindsDeferred {
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
