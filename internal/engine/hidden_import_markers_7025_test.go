package engine

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

// #7025: the ten rule files that still declare import_markers the engine cannot
// see — and the measurement that says renaming their container is NOT the fix.
//
// # What #7024 fixed, and why the same move does not apply here
//
// #7024 found 247 loader-scoped rule files spelling the `frameworks:` container
// 14 other ways. Those files WERE opened by the loader, WERE unmarshalled into
// FrameworkRule, and their content was dropped only because yaml.Unmarshal
// discards unknown top-level keys. Renaming the key to `frameworks:` was
// therefore sufficient: 52 files' markers became readable.
//
// The remaining ten files fail for two different reasons, and neither is the
// container spelling:
//
//  1. THE PATH. LoadAllRulesFromFS accepts exactly <lang>/<subdir>/<file>.yaml
//     for subdir in ruleSubdirs (frameworks, orms, queues). All ten sit at
//     <lang>/<file>.yaml — two path segments — so the loader never opens them,
//     never adds them to RuleLoadReport.Candidates, and never decodes them at
//     all. Spelling the container `frameworks:` changes nothing, because
//     nothing reads the file.
//
//  2. THE SHAPE. FrameworkRule.Frameworks is a single FrameworkMeta MAPPING.
//     Every one of the ten containers is a SEQUENCE of framework entries
//     (`- name: clojure.test` …). Renaming such a container to `frameworks:` in
//     a file the loader does open makes the file stop parsing: the decode
//     errors, the loader records a parse failure, and the file's rules —
//     including any it currently contributes — are dropped. The rename is not a
//     no-op there, it is a regression.
//
// Both mechanisms are pinned below on a synthetic FS, so each is exercised on
// its own rather than only through the real tree (a guard that fires only when
// the tree happens to feed it grades nothing).
//
// # Who reads these containers today: nobody (measured, #7025 step 1)
//
// Established by rename-to-break, not by grep. All 16 top-level keys on the ten
// files — testing_frameworks, testing, test_conventions, complexity_signals,
// domain_roots, orms_db, detection, extraction_targets, package_patterns,
// toolchains — were renamed to `zzprobe_<key>:` and every disk-walking consumer
// of the rule tree was re-run:
//
//   - internal/engine (the embedded loader): compiled rule set byte-identical.
//   - internal/entkinds (walks the YAML at ANY depth): site list identical.
//   - internal/relkinds (top-level relationship_rules only): identical.
//   - tools/coverage (Discover over the repo root): identical once its own
//     map-order nondeterminism is normalised away.
//
// Positive control on that measurement: appending `probe_block: {entity_type:
// ProbeKind7025}` to go/test_patterns.yaml DID appear in the entkinds site
// list, so the scanners do read these files — they simply do not decode the
// container key. tools/coverage's only use of test_patterns.yaml is a
// whole-file `strings.Contains` needle for a framework NAME
// (discover.go testFrameworkWalker), which is blind to key names by
// construction.
//
// So the containers are inert, exactly as #7024's 247 were. What is NOT
// available here is #7024's remedy.
//
// # What this guard therefore asserts
//
// The inventory is a closed set: these ten files, these container keys, these
// block counts, and every one of them outside loader scope. An eleventh such
// file fails here. One of the ten moving INTO loader scope while still keyed on
// an unmodelled container fails here (and in #7024's guard). Rescuing one — by
// relocating its entries under <lang>/frameworks/ in the mapping shape the
// schema models — fails here too, which is the point: the count moves only when
// something real happens, and the test names what changed.
//
// Deliberately NOT done: the rescue. Relocating these catalogues into
// loader-scoped rule files turns each entry into a rule set the engine
// compiles, one per framework, which is a schema and behaviour decision, not a
// key rename. Left to the #7025 follow-up.

// hiddenMarkerInventoryEntry is one (file, top-level container key) pair that
// declares import_markers the engine cannot reach.
type hiddenMarkerInventoryEntry struct {
	// Path is relative to the rules root, slash-separated.
	Path string
	// Key is the top-level container key the markers sit under.
	Key string
	// Blocks is the number of non-empty import_markers lists under Key, at any
	// depth.
	Blocks int
	// LoaderScoped reports whether the loader opens this file at all. It is
	// taken from LoadAllRulesFromFSReport's own Candidates list rather than
	// re-derived from the path here: a local copy of the path rule would agree
	// with a loader that had changed, and the whole point of this field is to
	// track what the loader actually does.
	LoaderScoped bool
	// SequenceShaped reports whether the container is a YAML sequence.
	// FrameworkRule.Frameworks is a mapping, so a sequence cannot be rescued by
	// renaming the key — the decode would fail.
	SequenceShaped bool
}

// hiddenImportMarkerInventory walks EVERY yaml file under rootDir — not only
// the loader-scoped ones — and reports each top-level container other than
// `frameworks:` that carries non-empty import_markers anywhere beneath it.
//
// The whole-tree walk is the difference from #7024's importMarkerVisibility,
// which is scoped to <lang>/<subdir>/<file>.yaml and is structurally unable to
// see any of these ten files. Enumerating one set thoroughly is what made its
// sibling look covered; this function exists to look at the sibling.
//
// examined counts files parsed, so "nothing hidden" is distinguishable from
// "nothing scanned". skipped lists every file the walk reached but could not
// read as a mapping, so "nothing hidden" is also distinguishable from "the
// file was never decoded" — see the block comment at the skip sites.
func hiddenImportMarkerInventory(fsys fs.FS, rootDir string) (entries []hiddenMarkerInventoryEntry, examined int, skipped []string, err error) {
	// Ask the loader which files it opens, rather than re-deriving its path
	// rule. See hiddenMarkerInventoryEntry.LoaderScoped.
	_, report, loadErr := LoadAllRulesFromFSReport(fsys, rootDir)
	if loadErr != nil {
		return nil, 0, nil, loadErr
	}
	candidates := make(map[string]bool, len(report.Candidates))
	for _, c := range report.Candidates {
		candidates[filepath.ToSlash(c)] = true
	}

	walkErr := fs.WalkDir(fsys, rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
			return nil
		}
		// Every early return from here on is recorded. A scan-and-assert-absence
		// guard has more ways to be a no-op than to work, and "the decoder could
		// not read this file" is the one that looks healthiest: examined stays
		// plausible, the entry set stays exactly right, and a file carrying
		// hidden markers is simply never looked at. Two real shapes do it — a
		// top-level SEQUENCE and a MULTI-DOCUMENT file — and neither is
		// hypothetical: the loader and internal/entkinds have the same
		// first-document-only limit. So a skip must be loud, not impossible.
		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			skipped = append(skipped, filepath.ToSlash(path)+": read: "+readErr.Error())
			return nil
		}
		//
		// The two shapes fail differently and both have to be caught. A
		// top-level sequence fails the decode outright. A multi-document file
		// does NOT: yaml.Unmarshal reads document 1 and returns nil, so a second
		// document carrying markers is dropped with no error at all — the
		// quieter of the two. A decoder loop is what tells them apart.
		dec := yaml.NewDecoder(bytes.NewReader(data))
		var raw map[string]any
		if uerr := dec.Decode(&raw); uerr != nil && !errors.Is(uerr, io.EOF) {
			skipped = append(skipped, filepath.ToSlash(path)+": decode into map[string]any: "+uerr.Error())
			return nil
		}
		var extra any
		if derr := dec.Decode(&extra); derr == nil {
			skipped = append(skipped, filepath.ToSlash(path)+
				": multi-document: only document 1 is decoded, later documents are not scanned")
			return nil
		}
		examined++
		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			skipped = append(skipped, filepath.ToSlash(path)+": relative path: "+relErr.Error())
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		scoped := candidates[filepath.ToSlash(path)]

		for key, val := range raw {
			if key == "frameworks" {
				continue
			}
			n := countImportMarkerBlocks(val)
			if n == 0 {
				continue
			}
			_, isSeq := val.([]any)
			entries = append(entries, hiddenMarkerInventoryEntry{
				Path:           relSlash,
				Key:            key,
				Blocks:         n,
				LoaderScoped:   scoped,
				SequenceShaped: isSeq,
			})
		}
		return nil
	})
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Key < entries[j].Key
	})
	sort.Strings(skipped)
	return entries, examined, skipped, walkErr
}

// countImportMarkerBlocks counts non-empty `import_markers:` lists anywhere
// beneath node. An empty list carries nothing for the engine to lose, so it is
// not counted — the same convention #7024's declaresImportMarkers uses.
func countImportMarkerBlocks(node any) int {
	switch v := node.(type) {
	case map[string]any:
		n := 0
		for k, child := range v {
			if k == "import_markers" {
				if list, ok := child.([]any); ok && len(list) > 0 {
					n++
					continue
				}
			}
			n += countImportMarkerBlocks(child)
		}
		return n
	case []any:
		n := 0
		for _, child := range v {
			n += countImportMarkerBlocks(child)
		}
		return n
	default:
		return 0
	}
}

// knownHiddenMarkerInventory is the measured state of the tree at the commit
// that added this test: ten files, 49 non-empty import_markers blocks, every
// one outside loader scope and every container sequence-shaped.
//
// It is written out per file rather than as a total, because a total is blind
// to substitution: markers deleted from one file and added to another leave the
// count where it was.
//
// Note where this disagrees with the #7025 issue body, which listed each file's
// FULL top-level key set rather than the marker-bearing one. `domain_roots`,
// `test_conventions`, `detection`, `extraction_targets` and `package_patterns`
// carry no import_markers at all; rust/test_patterns.yaml's 8 are all under
// `testing`, protobuf/extras.yaml's 3 are all under `toolchains`, and
// python/test_patterns.yaml has 6 non-empty blocks, not 7 (one further
// `import_markers:` key there is an empty list).
var knownHiddenMarkerInventory = []hiddenMarkerInventoryEntry{
	{Path: "clojure/test_patterns.yaml", Key: "testing_frameworks", Blocks: 4},
	{Path: "elixir/extras.yaml", Key: "orms_db", Blocks: 5},
	{Path: "elixir/test_patterns.yaml", Key: "testing", Blocks: 6},
	{Path: "go/test_patterns.yaml", Key: "testing_frameworks", Blocks: 7},
	{Path: "lua/test_patterns.yaml", Key: "testing", Blocks: 3},
	{Path: "protobuf/extras.yaml", Key: "toolchains", Blocks: 3},
	{Path: "python/complexity.yaml", Key: "complexity_signals", Blocks: 6},
	{Path: "python/test_patterns.yaml", Key: "testing_frameworks", Blocks: 6},
	{Path: "rust/test_patterns.yaml", Key: "testing", Blocks: 8},
	{Path: "zig/test_patterns.yaml", Key: "testing", Blocks: 1},
}

// TestRuleTree_HiddenImportMarkersAreOutsideLoaderScope is the whole-tree
// assertion #7024's guard cannot make, because its walk is loader-scoped and
// these ten files are not.
func TestRuleTree_HiddenImportMarkersAreOutsideLoaderScope(t *testing.T) {
	got, examined, skipped, err := hiddenImportMarkerInventory(rulesFS, "rules")
	if err != nil {
		t.Fatalf("walking the embedded rules tree: %v", err)
	}
	// A file the walk could not decode is a file this guard did not judge. All
	// 714 rule files parse as mappings today, so this costs nothing and closes
	// the hole permanently: a rule file added as a top-level sequence, or as a
	// multi-document file, must fail here rather than pass silently.
	for _, sk := range skipped {
		t.Errorf("the hidden-marker walk skipped %s. This guard asserts an ABSENCE, so a "+
			"file it cannot decode is a file it did not check — markers hidden there would "+
			"read as a clean tree (#7025). Fix the file, or teach the walk its shape.", sk)
	}
	// Non-vacuity: the whole tree, not the loader-scoped fifth of it.
	if examined < 700 {
		t.Fatalf("examined %d yaml files, want >= 700: this guard is about EVERY rule "+
			"file, not the loader-scoped subset #7024 walks; a smaller number means the "+
			"walk, not the tree, is what came back clean", examined)
	}

	render := func(e hiddenMarkerInventoryEntry) string {
		return fmt.Sprintf("%s:%s=%d", e.Path, e.Key, e.Blocks)
	}
	wantSet := map[string]bool{}
	for _, e := range knownHiddenMarkerInventory {
		wantSet[render(e)] = true
	}
	gotSet := map[string]bool{}
	totalBlocks := 0
	for _, e := range got {
		gotSet[render(e)] = true
		totalBlocks += e.Blocks

		// The load-bearing property. Either of these being false would mean the
		// #7024 remedy applies and this file should have been rescued, not
		// inventoried.
		if e.LoaderScoped {
			t.Errorf("%s declares import_markers under unmodelled container %q AND is "+
				"loader-scoped: the loader opens it, so this is the #7024 defect and the "+
				"file must be reconciled onto `frameworks:`, not listed here.", e.Path, e.Key)
		}
		if !e.SequenceShaped {
			t.Errorf("%s's %q container is a MAPPING, not a sequence: FrameworkRule."+
				"Frameworks is a FrameworkMeta mapping, so a mapping-shaped container can "+
				"be rescued by a key rename once the file is in loader scope. Re-check "+
				"whether this file belongs in the inventory.", e.Path, e.Key)
		}
	}

	for k := range wantSet {
		if !gotSet[k] {
			t.Errorf("expected hidden-marker entry %q is gone. If it was rescued — the "+
				"entries relocated under <lang>/frameworks/ in the mapping shape the "+
				"schema models — update knownHiddenMarkerInventory and say so. If the "+
				"markers were merely deleted, that is not a fix (#7025).", k)
		}
	}
	for k := range gotSet {
		if !wantSet[k] {
			t.Errorf("new hidden-marker entry %q: a rule file declares import_markers "+
				"under a top-level container the schema does not model, in a file the "+
				"loader does not open. Written markers that reach nothing (#7025). Put "+
				"them under `frameworks: {detection: {import_markers: [...]}}` in a "+
				"<lang>/frameworks/<file>.yaml.", k)
		}
	}
	if totalBlocks != 49 {
		t.Errorf("total unreachable import_markers blocks = %d, want 49", totalBlocks)
	}
}

// TestLoader_IgnoresRuleFilesOutsideARuleSubdir is reason 1, exercised alone.
//
// The file below is spelled perfectly: top-level `frameworks:`, a mapping, a
// `detection.import_markers` list. It is what a naive #7025 fix would produce
// by renaming clojure/test_patterns.yaml's container. The loader still loads
// nothing, because <lang>/<file>.yaml is not a rule path.
//
// Without this test, the claim "the rename is a no-op" rests on the real tree
// happening not to contain such a file — which is the mutually-masking pair
// #7024 hit. Here the input is constructed, so the guard is graded on its own.
func TestLoader_IgnoresRuleFilesOutsideARuleSubdir(t *testing.T) {
	const perfect = "frameworks:\n  name: clojure.test\n  detection:\n    import_markers:\n" +
		"    - clojure.test\n"

	fsys := fstest.MapFS{
		// Two path segments: the shape all ten #7025 files have.
		"rules/clojure/test_patterns.yaml": {Data: []byte(perfect)},
		// The identical bytes at a rule path, as the positive control. Without
		// it, a loader that returned nothing at all would pass.
		"rules/clojure/frameworks/clojure_test.yaml": {Data: []byte(perfect)},
	}

	rules, report, err := LoadAllRulesFromFSReport(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFSReport: %v", err)
	}

	for _, c := range report.Candidates {
		if c == "rules/clojure/test_patterns.yaml" {
			t.Fatalf("the loader now opens <lang>/<file>.yaml. If that is deliberate, the "+
				"ten #7025 files became loader-scoped and their containers must be "+
				"reconciled onto `frameworks:`; candidates = %v", report.Candidates)
		}
	}
	if len(report.Candidates) != 1 {
		t.Fatalf("candidates = %v, want exactly the one file at a rule path", report.Candidates)
	}
	if n := len(rules["clojure"]); n != 1 {
		t.Fatalf("loaded %d clojure rule sets, want 1 (the control file at a rule path)", n)
	}
	// The control's markers loaded, so "no markers" below is about the path,
	// not about a loader that reads nothing.
	if got := rules["clojure"][0].Frameworks.Detection.ImportMarkers; len(got) != 1 || got[0] != "clojure.test" {
		t.Fatalf("control file's import_markers = %q, want [clojure.test]: the positive "+
			"control did not load, so this test cannot say anything about the other file", got)
	}
}

// TestLoader_SequenceShapedFrameworksContainerFailsToParse is reason 2,
// exercised alone: even inside loader scope, renaming one of these containers
// to `frameworks:` does not rescue the markers — it breaks the file.
//
// FrameworkRule.Frameworks is a FrameworkMeta mapping. The ten #7025 containers
// are sequences. So the "fix" turns a file whose metadata is silently dropped
// into a file the loader refuses outright, taking any rules it does carry with
// it (loader.go records a parse failure and skips).
func TestLoader_SequenceShapedFrameworksContainerFailsToParse(t *testing.T) {
	// clojure/test_patterns.yaml's real shape, reduced: a sequence of entries.
	const renamed = "frameworks:\n- name: clojure.test\n  detection:\n    import_markers:\n" +
		"    - clojure.test\n" +
		"source_patterns:\n- pattern: deftest\n  entity_type: Function\n"

	fsys := fstest.MapFS{
		"rules/clojure/frameworks/test_patterns.yaml": {Data: []byte(renamed)},
	}

	rules, report, err := LoadAllRulesFromFSReport(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFSReport: %v", err)
	}
	if len(report.Candidates) != 1 {
		t.Fatalf("candidates = %v, want 1", report.Candidates)
	}
	if len(report.Failures) != 1 || report.Failures[0].Stage != "parse" {
		t.Fatalf("failures = %v, want exactly one parse failure: a sequence-shaped "+
			"`frameworks:` must not decode into FrameworkMeta. If it now does, the schema "+
			"learned the sequence shape and the ten #7025 files ARE rescuable by rename — "+
			"say so on #7025.", report.Failures)
	}
	if n := len(rules["clojure"]); n != 0 {
		t.Fatalf("loaded %d clojure rule sets from a file that failed to parse, want 0", n)
	}
	// And the cost is not confined to the markers: the file's source_patterns
	// go with it. This is why the #7024 rename is a regression here, not a
	// no-op.
	if len(report.Loaded) != 0 {
		t.Errorf("loaded = %v, want none", report.Loaded)
	}
}

// TestHiddenImportMarkerInventory_Controls grades the checker itself. An
// inventory function that returned nil, or that counted every import_markers
// key including the empty ones, or that got LoaderScoped backwards, would make
// the tree assertion above meaningless.
func TestHiddenImportMarkerInventory_Controls(t *testing.T) {
	fsys := fstest.MapFS{
		// The #7025 shape: outside loader scope, sequence container, 2 blocks.
		"rules/go/test_patterns.yaml": {Data: []byte(
			"testing_frameworks:\n- name: testing\n  detection:\n    import_markers:\n    - '\"testing\"'\n" +
				"- name: testify\n  detection:\n    import_markers:\n    - '\"github.com/stretchr/testify\"'\n")},
		// The #7024 shape: INSIDE loader scope under an unmodelled key. Must be
		// reported, and must be reported as loader-scoped — that is what tells
		// the two defects apart.
		"rules/csharp/orms/mongodb.yaml": {Data: []byte(
			"orms_database:\n  name: MongoDB\n  detection:\n    import_markers:\n    - using MongoDB.Driver\n")},
		// Correctly keyed: never in the inventory.
		"rules/go/orms/bun.yaml": {Data: []byte(
			"frameworks:\n  name: bun\n  detection:\n    import_markers:\n    - '\"github.com/uptrace/bun\"'\n")},
		// Empty list: nothing to lose, not an entry.
		"rules/lua/test_patterns.yaml": {Data: []byte(
			"testing:\n- name: busted\n  detection:\n    import_markers: []\n")},
		// No markers anywhere: not an entry, whatever the key is called.
		"rules/rust/build_tools.yaml": {Data: []byte(
			"toolchains:\n- name: cargo\n  detection:\n    files:\n    - Cargo.toml\n")},
	}

	got, examined, skipped, err := hiddenImportMarkerInventory(fsys, "rules")
	if err != nil {
		t.Fatalf("hiddenImportMarkerInventory: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none: every fixture here is a mapping", skipped)
	}
	if examined != 5 {
		t.Errorf("examined = %d, want 5 (every yaml file, not only loader-scoped ones)", examined)
	}

	want := []hiddenMarkerInventoryEntry{
		{Path: "csharp/orms/mongodb.yaml", Key: "orms_database", Blocks: 1, LoaderScoped: true, SequenceShaped: false},
		{Path: "go/test_patterns.yaml", Key: "testing_frameworks", Blocks: 2, LoaderScoped: false, SequenceShaped: true},
	}
	if len(got) != len(want) {
		t.Fatalf("inventory = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("inventory[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestHiddenImportMarkerInventory_UndecodableFilesAreReported is the positive
// control on the skip counter. Without it, `skipped == 0` on the real tree is
// satisfied just as well by a walk that never appends to skipped at all — the
// same vacuity the counter exists to remove, moved one level up.
//
// Both fixtures are real shapes, not invented ones, and both carry hidden
// markers so that a silent skip would be a MISSED FINDING rather than a
// harmless omission:
//
//   - a top-level SEQUENCE, which does not decode into map[string]any; and
//   - a MULTI-DOCUMENT file, whose second document is where the markers are.
//
// The third fixture is an ordinary mapping, so "everything was skipped" cannot
// pass either.
func TestHiddenImportMarkerInventory_UndecodableFilesAreReported(t *testing.T) {
	fsys := fstest.MapFS{
		// Top-level sequence: yaml.Unmarshal cannot fit it into a map.
		"rules/go/seq_patterns.yaml": {Data: []byte(
			"- name: testing\n  detection:\n    import_markers:\n    - '\"testing\"'\n")},
		// Multi-document: the markers live after the separator.
		"rules/go/multidoc_patterns.yaml": {Data: []byte(
			"testing_frameworks: []\n---\ntesting:\n- name: testify\n  detection:\n" +
				"    import_markers:\n    - '\"github.com/stretchr/testify\"'\n")},
		// Decodable, and genuinely hidden: the walk must still do its job.
		"rules/lua/test_patterns.yaml": {Data: []byte(
			"testing:\n- name: busted\n  detection:\n    import_markers:\n    - \"require('busted')\"\n")},
	}

	got, examined, skipped, err := hiddenImportMarkerInventory(fsys, "rules")
	if err != nil {
		t.Fatalf("hiddenImportMarkerInventory: %v", err)
	}

	if len(skipped) != 2 {
		t.Fatalf("skipped = %v, want 2 entries (the sequence file and the multi-document "+
			"file): if either now decodes, the walk grew a shape it did not have and the "+
			"tree assertion's skipped==0 is checking something different", skipped)
	}
	for _, want := range []string{"rules/go/multidoc_patterns.yaml", "rules/go/seq_patterns.yaml"} {
		found := false
		for _, sk := range skipped {
			if strings.HasPrefix(sk, want+":") {
				found = true
			}
		}
		if !found {
			t.Errorf("skipped = %v, want an entry naming %s", skipped, want)
		}
	}
	// The skip is loud, not fatal: the rest of the walk still runs and still
	// reports. A counter that fired by aborting the walk would make the tree
	// assertion pass for the wrong reason.
	if examined != 1 {
		t.Errorf("examined = %d, want 1 (only the decodable fixture)", examined)
	}
	if len(got) != 1 || got[0].Path != "lua/test_patterns.yaml" || got[0].Key != "testing" || got[0].Blocks != 1 {
		t.Errorf("inventory = %+v, want the one decodable hidden entry", got)
	}
}

// TestCountImportMarkerBlocks_Depth pins the one thing the counter has to get
// right that the fixtures above do not vary: markers nested at more than one
// level, and markers under a key that merely CONTAINS "import_markers".
func TestCountImportMarkerBlocks_Depth(t *testing.T) {
	var doc any
	src := "" +
		"a:\n" +
		"- b:\n" +
		"    detection:\n" +
		"      import_markers: [x, y]\n" +
		"- c:\n" +
		"    nested:\n" +
		"      deeper:\n" +
		"        import_markers: [z]\n" +
		"- d:\n" +
		"    import_markers: []\n" +
		"- e:\n" +
		"    import_markers_legacy: [q]\n"
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if n := countImportMarkerBlocks(doc); n != 2 {
		t.Errorf("countImportMarkerBlocks = %d, want 2: two non-empty lists at different "+
			"depths; the empty list and the `import_markers_legacy` key must not count", n)
	}
}
