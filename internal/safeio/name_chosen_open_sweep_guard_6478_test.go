package safeio_test

// name_chosen_open_sweep_guard_6478_test.go — the repo-wide, BINDING guard on
// the #6416/#6478 "a filename an attacker can name is read with os.ReadFile"
// class.
//
// # Why this file exists, and why prose was not enough
//
// #6468 claimed the blocking-open class was closed FOUR TIMES and was wrong
// each time, because each round fixed exactly the sites the previous review
// named and swept no further. #6478 records the diagnosis: the enumeration in
// docs/blocking-open-audit.md is a snapshot, and "without a checker it goes
// stale the first time someone adds a new read, which is precisely how this
// happened four times." A hand-maintained list of file:line rows is the same
// failure mode as a hand-maintained integer (#6521), and this repository has
// re-filed that defect enough times to stop arguing about it.
//
// So the durable half of #6478 is this: an AST sweep that OBSERVES the
// boundary, plus a ledger that can only shrink.
//
// # What it observes
//
// testsupport.FindNameChosenOpens reports every os.ReadFile / os.Open /
// os.OpenFile in a non-test file under internal/ or cmd/ whose path
// expression, resolved up to two assignments back, contains a filename-shaped
// string literal. Routing a site through safeio removes it from the scan,
// which is the entire feedback loop: the fix silences the guard, and nothing
// else does — except a ledger entry, which cannot be added.
//
// # The division of labour, stated because #6478 predicted the failure mode
//
// The issue says up front that an AST pass "cannot decide provenance — 'is
// this directory inside a repo an attacker can write to, or inside ~/.grafel?'
// is a judgement no AST pass makes", and warns that a badly-sized allowlist
// makes "the lint a rubber stamp: a gate that cannot fail, which is a defect
// class this repo has shipped more than once already."
//
// Two things keep that from happening here:
//
//  1. The ledger is SMALL and it is MEASURED. The issue projected 243
//     not-applicable rows needing day-one entries. The scan finds 34, because
//     the audit's 324 raw call sites include writes, procfs, walker-produced
//     paths and paths with no literal in them at all — none of which have this
//     shape. Every one of the 34 was read, and every one resolves under
//     ~/.grafel, the daemon root, grafel's own generated output, or a path the
//     user typed. That divergence is reported on the issue rather than
//     silently absorbed.
//  2. The ledger is RATCHETED, exactly as
//     internal/registry/home_isolation_sweep_guard_6735_test.go is. An author
//     who trips the sweep cannot silence it with a one-line append. That hole
//     was real in the reference guard's first version and was demonstrated,
//     not argued; there is no reason to re-learn it here.
//
// # This guard's own guard
//
// TestNameChosenOpenSweepCanFail plants a deliberately unguarded read in a
// synthetic tree and requires the scan to name it, so "the lint can go red" is
// an assertion rather than a hope. TestNameChosenOpenSweepIsNotVacuous pins
// that the repo walk actually reaches source, because a walk that reads
// nothing reports nothing and looks green.
//
// # Inherited blind spots, restated rather than buried
//
// The detector resolves identifiers, not calls. A path that arrives as a bare
// function parameter is invisible — internal/agents.upsertFile(path, block) is
// the live example, and it is a #6478 site that this scan does NOT see even
// though the change routes it. The same goes for a filename with no extension
// and no leading dot: internal/install/hooks reads `pre-push` out of the
// HookNames slice, and "pre-push" is not filename-shaped by any rule that does
// not also match half the identifiers in the tree. Both are pinned as MISS
// rows in internal/testsupport/blockingopenscan_test.go. The guard pins the
// BOUNDARY; it does not pin the existing sites, and saying otherwise would be
// the fifth premature closure.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// nameChosenOpenAllowed is the ledger of call sites the sweep reports and that
// are NOT name-chosen in the threat-model sense: the path resolves somewhere an
// attacker who could plant a FIFO there already owns the account, or somewhere
// the user typed themselves.
//
// The value is a family tag; see nameChosenFamilyReason. Keyed by file and
// enclosing function, NOT by line: a line number churns on every edit above it,
// and a ledger that churns is a ledger nobody re-reads.
//
// This list must only ever SHRINK, and that is ENFORCED — see
// nameChosenOpenAllowedMax.
var nameChosenOpenAllowed = map[string]string{
	"cmd/grafel/selftest.go:selftestIncremental":                             "selftest-fixture",
	"internal/cli/doctor_summary.go:computeRepoHealth":                       "own-state",
	"internal/cli/rebuild_summary.go:loadCandidateCounts":                    "own-state",
	"internal/cli/rebuild_summary.go:loadGraphStats":                         "own-state",
	"internal/cli/repair.go:loadWebhookSettings":                             "own-state",
	"internal/cli/status_stats.go:ComputeStatusSummaryForRef":                "own-state",
	"internal/daemon/algo/cache.go:readFromDisk":                             "own-state",
	"internal/daemon/root_manifest.go:ReadRootManifest":                      "own-state",
	"internal/daemon/service.go:readGraphStatsSidecar":                       "own-state",
	"internal/daemon/worktree_seed.go:ReadSeedStamp":                         "own-state",
	"internal/dashboard/graphstate.go:loadPersistedAlgoResults":              "own-state",
	"internal/dashboard/handlers_patterns.go:loadPatterns":                   "own-state",
	"internal/dashboard/handlers_ref_endpoints.go:Server.handleGroupRefs":    "own-state",
	"internal/dashboard/handlers_repairs.go:readAllCandidates":               "own-state",
	"internal/dashboard/handlers_skills.go:parseSkillMeta":                   "skills-cache",
	"internal/dashboard/handlers_snapshots.go:Server.handleSnapshotDiff":     "own-state",
	"internal/dashboard/handlers_snapshots.go:loadSnapshotList":              "own-state",
	"internal/dashboard/handlers_v2_refs.go:Server.handleV2GroupRefs":        "own-state",
	"internal/dashboard/store.go:aggregateGroupStats":                        "own-state",
	"internal/dashboard/v2_group_settings.go:repoStats":                      "own-state",
	"internal/docgen/cleanup.go:readRunGroupMarker":                          "own-state",
	"internal/docgen/tier3.go:RunTier3":                                      "docgen-output",
	"internal/docgen/tier4_contracts.go:checkGroupIndex":                     "docgen-output",
	"internal/enrichment/docgen_repair.go:loadExistingResolutionIDs":         "own-state",
	"internal/enrichment/docgen_repair.go:mergeDocgenRepairsIntoResolutions": "own-state",
	"internal/graph/manifest.go:ReadManifest":                                "own-state",
	"internal/indexer/diff/diff.go:LoadManifest":                             "own-state",
	"internal/links/string_pass.go:scanFile":                                 "own-state",
	"internal/mcp/candidates.go:readEnrichmentCandidates":                    "own-state",
	"internal/mcp/repair.go:readRepairEdgeCandidates":                        "own-state",
	"internal/mcp/repair.go:readRepairFile":                                  "own-state",
	"internal/mcp/security_findings_tool.go:Server.handleSecurityFindings":   "own-state",
	"internal/mcp/tools.go:loadPatternsQuiet":                                "own-state",
	"internal/quality/expected.go:LoadFixture":                               "user-typed-path",
}

// nameChosenFamilyReason explains each family tag. A ledger entry without a
// stated reason is not a decision, it is a silence.
var nameChosenFamilyReason = map[string]string{
	"own-state": "The path resolves under ~/.grafel, the daemon root, or a sidecar grafel itself wrote " +
		"(graph-stats.json, enrichment-candidates.json, manifest.json, the links scan cache, …). " +
		"docs/blocking-open-audit.md's own bucket definition: \"reads of grafel's own state … " +
		"A hostile FIFO in any of those means the attacker already owns the account.\"",
	"docgen-output": "The path is inside the docgen output directory this same process just generated. " +
		"Same reasoning as own-state; called out separately because the directory lives in the " +
		"USER's repo rather than under ~/.grafel, so the judgement rests on grafel having written " +
		"the tree moments earlier rather than on the location.",
	"skills-cache": "SKILL.md inside grafel's own skills cache. Listed as a stated-threat-model " +
		"boundary case in docs/blocking-open-audit.md rather than as an obvious not-applicable.",
	"user-typed-path": "The directory came from a CLI flag the user typed (--fixture-dir). The audit " +
		"records this as not-applicable on a STATED threat model, and records that it moves if a " +
		"hostile --fixture-dir ever enters that model.",
	"selftest-fixture": "cmd/grafel selftest reads main.go out of the throwaway repo it created itself " +
		"seconds earlier in a temp dir. There is no attacker-supplied name in the path.",
}

// nameChosenOpenAllowedMax is the RATCHET on nameChosenOpenAllowed: the exact
// number of entries the ledger is allowed to hold.
//
// Without it, "this ledger only shrinks" is a sentence in a comment and nothing
// more — an author who trips the sweep silences it by appending one
// correctly-spelled line, go vet clean, suite green. #6478's own acceptance
// criterion is that the lint must be able to FAIL on a new unguarded read; an
// append-able ledger is exactly the "gate that cannot fail" the issue names as
// the thing to avoid. The same hole was demonstrated (not theorised) against
// internal/registry/home_isolation_sweep_guard_6735_test.go's first version.
//
// The assertion is EXACT, not an upper bound, so it ratchets both ways:
//
//   - GROWS → the sweep already named the site; this fires second and says the
//     fix is safeio, not a bigger number.
//   - SHRINKS (a site was routed, which is the point) → this fires and requires
//     the constant to come down with it, so the bar is never left slack for a
//     future append to slip under.
const nameChosenOpenAllowedMax = 34

// TestNameChosenOpenLedgerOnlyShrinks is the ratchet itself.
func TestNameChosenOpenLedgerOnlyShrinks(t *testing.T) {
	if len(nameChosenOpenAllowed) > nameChosenOpenAllowedMax {
		t.Fatalf("nameChosenOpenAllowed has GROWN to %d entries (ratchet: %d).\n\n"+
			"This ledger records reads that were READ and judged not-attacker-named; it is not a "+
			"suppression list for new work. If the sweep just named your call site, the fix is "+
			"safeio.ReadFileReported(path, safeio.FollowSymlinks, safeio.MaxConfigFileBytes) or "+
			"safeio.OpenReported(path, safeio.FollowSymlinks) — a one-line substitution that also "+
			"reports the skip instead of swallowing it. Raising this constant is not that.",
			len(nameChosenOpenAllowed), nameChosenOpenAllowedMax)
	}
	if len(nameChosenOpenAllowed) < nameChosenOpenAllowedMax {
		t.Fatalf("nameChosenOpenAllowed has shrunk to %d entries but the ratchet still reads %d — "+
			"lower nameChosenOpenAllowedMax to %d in the same change.\n\n"+
			"Thank you for routing a site. The constant has to follow the ledger down, or the slack "+
			"it leaves behind is exactly the room a future append needs to pass unnoticed.",
			len(nameChosenOpenAllowed), nameChosenOpenAllowedMax, len(nameChosenOpenAllowed))
	}
}

// TestNameChosenOpenRatchetIsWired pins the ratchet's EXISTENCE, not just its
// current verdict. Deleting TestNameChosenOpenLedgerOnlyShrinks, or relaxing it
// to a one-sided bound, leaves every other test in this file green and returns
// the ledger to being append-able in one line — with the "only shrinks" prose
// above it still sitting there, now lying.
//
// It asserts the two comparisons BY OPERATOR AND BY OPERAND. An identifier walk
// would survive `_ = nameChosenOpenAllowedMax`, which is the shape #6290 found
// dead in the equivalent guard one milestone earlier.
func TestNameChosenOpenRatchetIsWired(t *testing.T) {
	dir, err := testsupport.PackageDirOfCaller(0)
	if err != nil {
		t.Fatalf("locate package dir: %v", err)
	}
	const self = "name_chosen_open_sweep_guard_6478_test.go"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(dir, self), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", self, err)
	}

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
				if n.Name == "nameChosenOpenAllowedMax" {
					constFound = true
				}
			}
		}
	}
	if !constFound {
		t.Fatal("nameChosenOpenAllowedMax is no longer declared as a package-level const. " +
			"Without it nothing stops nameChosenOpenAllowed from growing, and the \"this ledger " +
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
			if isLenOfAllowedLedger(be.X) && isNCIdent(be.Y, "nameChosenOpenAllowedMax") {
				ops[be.Op] = true
			}
			if isLenOfAllowedLedger(be.Y) && isNCIdent(be.X, "nameChosenOpenAllowedMax") {
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
		t.Fatal("no Test in this file compares len(nameChosenOpenAllowed) > nameChosenOpenAllowedMax. " +
			"That is the growth half of the ratchet — the one that stops a tripped sweep being " +
			"silenced with a one-line append.")
	}
	if !ops[token.LSS] {
		t.Fatal("no Test in this file compares len(nameChosenOpenAllowed) < nameChosenOpenAllowedMax. " +
			"That is the shrink half — without it the constant is an upper bound that stays slack " +
			"after a site is routed, leaving room for a future append to pass unnoticed.")
	}
}

func isLenOfAllowedLedger(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !isNCIdent(call.Fun, "len") {
		return false
	}
	return isNCIdent(call.Args[0], "nameChosenOpenAllowed")
}

func isNCIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// TestNoUnguardedNameChosenOpens is the binding sweep.
func TestNoUnguardedNameChosenOpens(t *testing.T) {
	for key, fam := range nameChosenOpenAllowed {
		if _, ok := nameChosenFamilyReason[fam]; !ok {
			t.Fatalf("nameChosenOpenAllowed[%q] uses family %q, which has no entry in "+
				"nameChosenFamilyReason — a ledger entry without a stated reason is not a "+
				"decision, it is a silence", key, fam)
		}
	}

	found := scanNameChosenOpens(t, repoRootFor6478(t))

	seen := map[string]bool{}
	var unexpected []string
	for _, o := range found {
		seen[o.Key()] = true
		if _, allowed := nameChosenOpenAllowed[o.Key()]; !allowed {
			unexpected = append(unexpected, o.String())
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("name-chosen blocking open(s) not routed through internal/safeio and not on the ledger (#6478):\n  %s\n\n"+
			"os.ReadFile / os.Open / os.OpenFile park in open(2) until a writer appears when the path "+
			"is a FIFO, and never reach EOF when it is a character device. A filename an attacker can "+
			"choose — .gitignore, CLAUDE.md, .grafel/group.json, a git hook — therefore wedges the "+
			"reading goroutine forever, and `mkfifo` is all it takes.\n\n"+
			"Fix: safeio.ReadFileReported(path, safeio.FollowSymlinks, safeio.MaxConfigFileBytes), or "+
			"safeio.OpenReported(path, safeio.FollowSymlinks) for the *os.File form. Both return the "+
			"error UNCHANGED, so an existing os.IsNotExist branch keeps working, and both report the "+
			"skip so a refused file stops being invisible (#6338).\n\n"+
			"If the path genuinely resolves under ~/.grafel, the daemon root or a directory the user "+
			"typed, it belongs in nameChosenOpenAllowed with a family tag — but that ledger is "+
			"ratcheted by nameChosenOpenAllowedMax and will fail the moment you append to it. Say so "+
			"in review and move the constant deliberately, or route the read.",
			strings.Join(unexpected, "\n  "))
	}

	// The ledger is a declaration about the live tree, not a wishlist. An entry
	// the scan no longer produces means the site was routed, renamed or deleted;
	// leaving it behind hides that and lets the ledger stop shrinking without
	// anyone noticing.
	var stale []string
	for key := range nameChosenOpenAllowed {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("nameChosenOpenAllowed entries the live scan no longer produces (routed, renamed or "+
			"removed — delete the entry and lower nameChosenOpenAllowedMax):\n  %s",
			strings.Join(stale, "\n  "))
	}
}

// TestNameChosenOpenSweepCanFail is the guard's own guard: a gate that cannot go
// red is the failure mode #6478 names explicitly. The scan is pointed at a
// synthetic tree holding one offender, three compliant files and one exclusion,
// and required to report exactly the offender.
func TestNameChosenOpenSweepCanFail(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "internal", "planted")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(pkg, name), []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// Offender: a config file named by a literal, read raw.
	write("offender.go", `package planted
import ("os"; "path/filepath")
func ReadRules(repo string) []byte {
	p := filepath.Join(repo, "CLAUDE.md")
	b, _ := os.ReadFile(p)
	return b
}`)
	// Compliant: routed through safeio.
	write("routed.go", `package planted
import ("path/filepath"; "github.com/cajasmota/grafel/internal/safeio")
func ReadRouted(repo string) []byte {
	p := filepath.Join(repo, "CLAUDE.md")
	b, _ := safeio.ReadFileReported(p, safeio.FollowSymlinks, safeio.MaxConfigFileBytes)
	return b
}`)
	// Compliant: a write, not a read.
	write("write.go", `package planted
import ("os"; "path/filepath")
func WriteRules(repo string) {
	f, _ := os.OpenFile(filepath.Join(repo, "CLAUDE.md"), os.O_CREATE|os.O_WRONLY, 0o644)
	_ = f
}`)
	// Compliant: no filename-shaped literal anywhere in the path expression.
	write("nolit.go", `package planted
import "os"
func ReadArg(p string) []byte { b, _ := os.ReadFile(p); return b }`)
	// Out of the scan's reach: a _test.go file.
	write("offender_test.go", `package planted
import "os"
func helper() { os.ReadFile("CLAUDE.md") }`)
	// Offender: the literal lives in a package-level const in ANOTHER FILE.
	// This is the shape that evaded the sweep on f5cc2410c, reproduced here end
	// to end rather than only at the detector's unit level — the evasion was in
	// the SWEEP's file-at-a-time walk as much as in the detector's scope, and a
	// unit test of the detector alone would not have caught it.
	write("consts.go", `package planted
const rulesFile = ".grafel"`)
	write("crossfile.go", `package planted
import "os"
func ReadCrossFile() []byte { b, _ := os.ReadFile(rulesFile); return b }`)

	got := scanNameChosenOpens(t, root)
	want := map[string]string{
		"internal/planted/offender.go:ReadRules":      "CLAUDE.md",
		"internal/planted/crossfile.go:ReadCrossFile": ".grafel",
	}
	if len(got) != len(want) {
		t.Fatalf("sweep reported %d sites over the synthetic tree, want exactly %d: %v", len(got), len(want), got)
	}
	for _, o := range got {
		lit, ok := want[o.Key()]
		if !ok {
			t.Fatalf("sweep named %s, which is not one of the two planted offenders", o.Key())
		}
		if o.Literal != lit {
			t.Fatalf("%s reported literal %q, want %q — the report must name the literal that "+
				"triggered it, or the author cannot tell which argument the guard objected to",
				o.Key(), o.Literal, lit)
		}
		delete(want, o.Key())
	}
	for k := range want {
		t.Fatalf("sweep did not report planted offender %s", k)
	}
}

// TestNameChosenOpenSweepIsNotVacuous pins that the repo walk actually reaches
// source. A walk that parses nothing reports nothing and looks green.
func TestNameChosenOpenSweepIsNotVacuous(t *testing.T) {
	if n := countNonTestGoFiles(t, repoRootFor6478(t)); n < 500 {
		t.Fatalf("repo walk parsed %d non-test .go files under internal/ and cmd/; the sweep is not "+
			"binding the repository", n)
	}
}

// scanNameChosenOpens scans root one PACKAGE at a time.
//
// Per-package, not per-file, and that distinction was a real hole rather than
// tidiness: with a per-file scope, a `const rulesFile = ".grafel"` in consts.go
// and an `os.ReadFile(rulesFile)` in reader.go evaded the sweep, while the
// byte-identical read with the const in the same file was caught. A separate
// consts.go or paths.go holding path literals is ordinary Go, so that gap was
// likelier to be hit than either limit the detector documents.
func scanNameChosenOpens(t *testing.T, root string) []testsupport.NameChosenOpen {
	t.Helper()
	var out []testsupport.NameChosenOpen
	// A directory is a package for this purpose. Files carrying build tags for
	// another GOOS are parsed and merged in too, which can only ever WIDEN the
	// scope — the failure direction of a guard is the one to prefer.
	type pkgFile struct {
		rel  string
		fset *token.FileSet
		f    *ast.File
	}
	byDir := map[string][]pkgFile{}
	var dirs []string
	walkNonTestGoFiles(t, root, func(rel string, fset *token.FileSet, f *ast.File) {
		d := filepath.Dir(rel)
		if _, ok := byDir[d]; !ok {
			dirs = append(dirs, d)
		}
		byDir[d] = append(byDir[d], pkgFile{rel, fset, f})
	})
	sort.Strings(dirs)
	for _, d := range dirs {
		files := make([]*ast.File, 0, len(byDir[d]))
		for _, pf := range byDir[d] {
			files = append(files, pf.f)
		}
		scope := testsupport.CollectPackageValues(files)
		for _, pf := range byDir[d] {
			out = append(out, testsupport.FindNameChosenOpensInPackage(pf.fset, pf.f, pf.rel, scope)...)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func countNonTestGoFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	walkNonTestGoFiles(t, root, func(string, *token.FileSet, *ast.File) { n++ })
	return n
}

// walkNonTestGoFiles visits every non-test .go file under root/internal and
// root/cmd. Those two trees are the scope docs/blocking-open-audit.md
// enumerated; a directory that does not exist under root is skipped, so the
// same walker serves the repo and the synthetic tree.
func walkNonTestGoFiles(t *testing.T, root string, visit func(rel string, fset *token.FileSet, f *ast.File)) {
	t.Helper()
	for _, top := range []string{"internal", "cmd"} {
		dir := filepath.Join(root, top)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				// .claude holds full worktree checkouts of this same repo;
				// walking it would scan (and re-report) other branches' source.
				case ".git", ".claude", "node_modules", "vendor", "testdata", "dist", "build":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if perr != nil {
				t.Fatalf("parse %s: %v", rel, perr)
			}
			visit(filepath.ToSlash(rel), fset, f)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// repoRootFor6478 locates the repository root from this package's own directory
// rather than from the working directory, which `go test` sets per-package.
func repoRootFor6478(t *testing.T) string {
	t.Helper()
	dir, err := testsupport.PackageDirOfCaller(0)
	if err != nil {
		t.Fatalf("locate package dir: %v", err)
	}
	root := filepath.Dir(filepath.Dir(dir)) // internal/safeio -> internal -> repo
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolved repo root %q has no go.mod: %v", root, err)
	}
	return root
}
