// Package main — #6472: a graph written before the local_scope stamp must not
// re-open the #6467 slot-capture regression when it is carried forward.
//
// #6472 moved the "a React props parameter is a callable-local" fact out of a
// hardcoded `subtype == "component_prop" && framework == "react"` clause in
// internal/resolve/refs.go and into a Properties["local_scope"] stamp written
// by internal/extractors/javascript/dataflow_react.go.
//
// Path B's incremental reindex carries the previous graph's UNCHANGED-file
// entities into the resolver index verbatim (index.go, the `cf = append(cf, …)`
// loop). Immediately after a user upgrades, those carried records were written
// by the OLD binary and therefore carry no stamp — while the NEW predicate
// reads nothing else. The props parameter would take the repository-wide byName
// slot again and an unrelated file's import of the same name would bind to it
// instead of reaching the external library, until the next FULL reindex.
//
// This test reproduces that upgrade by DOCTORING the persisted baseline: it
// runs a full index with this binary, strips local_scope from the persisted
// component_props (which is exactly what a pre-#6472 graph looks like on disk),
// writes it back, and then runs the incremental pass against it. The stamp the
// assertions depend on is therefore the one applied by the carry-forward seam
// at run time, not one that survived from extraction.
//
// Refs #6472, #6467, #6119.
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

const (
	lsUnchangedFile6472 = "widgetu.tsx"
	lsChangedFile6472   = "consumerc.tsx"
)

// lsWriteUnchanged6472 writes the file that is NOT touched between the baseline
// and the incremental run, so its entities are the ones carried forward.
//
// `WidgetU(toolkit)` is the whole-bag React props form: a PascalCase component
// whose single identifier parameter becomes one component_prop named for the
// bag — here `toolkit`, colliding with the package imported by the changed
// file below. That collision is the #6467 shape.
//
// `datakit` is the OVER-STAMP control, and it is load-bearing. The
// compatibility shim's job is to stamp carried React props and NOTHING ELSE; a
// shim that stamped every unstamped carried record would strip real
// declarations of their repo-wide byName slot and hide them from grafel_find —
// the #6481 trap, which has no loud symptom. With only the prop in this file,
// `needsLegacyLocalScopeStamp` mutated to `return true` is killed only by the
// helper's own unit test and this integration test SURVIVES.
//
// It is the exact MIRROR of the prop case: a declaration whose name collides
// with a package the changed file imports. The prop must LOSE that collision
// (it is callable-local); `datakit` must WIN it (it is a real, addressable
// module-scope declaration). One fixture, both directions, and the shim has to
// get both right at once. Deliberately camelCase so the React prop emitter,
// which only fires on PascalCase names, does not treat it as a component.
//
// Basenames across the corpus are unique: diff.Filter cross-invalidates
// same-basename files, which would drag the "unchanged" file into the changed
// set and make the case vacuous.
func lsWriteUnchanged6472(t *testing.T, repo string) {
	t.Helper()
	dvWriteFile(t, repo, lsUnchangedFile6472, `export function WidgetU(toolkit) {
  return toolkit.render();
}

export function datakit(v) {
  return v + 1;
}
`)
}

// lsWriteChanged6472 writes the file edited between passes. It imports two
// packages whose names collide with entities in the unchanged file: `toolkit`,
// which collides with the React props parameter and must NOT bind to it, and
// `datakit`, which collides with a real module-scope declaration and MUST bind
// to it. Stamp the prop, leave the declaration alone.
func lsWriteChanged6472(t *testing.T, repo string, pass int) {
	t.Helper()
	dvWriteFile(t, repo, lsChangedFile6472, `import toolkit from 'toolkit';
import datakit from 'datakit';

export function renderC(x) {
  return toolkit.run(x) + datakit(x) + `+strconv.Itoa(pass)+`;
}
`)
}

// lsStripLocalScope6472 rewrites the persisted baseline so it looks like a
// graph written by a pre-#6472 binary: every component_prop loses its
// local_scope property. It returns the number of props it stripped.
//
// WriteGraphGen allocates a fresh generation and flips the `current` pointer
// only after the new gen is durably in place, so the incremental run's
// OpenGraphStream reads the doctored graph.
func lsStripLocalScope6472(t *testing.T, stateDir string) int {
	t.Helper()
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load baseline for doctoring: %v", err)
	}
	stripped := 0
	for i := range doc.Entities {
		e := &doc.Entities[i]
		subtype := e.Subtype
		if subtype == "" {
			subtype = e.PropGet("subtype")
		}
		if subtype != "component_prop" || e.PropGet("local_scope") != "true" {
			continue
		}
		props := e.PropsSnapshot()
		delete(props, "local_scope")
		*e = e.WithProperties(props)
		stripped++
	}
	if stripped > 0 {
		if _, err := fbwriter.WriteGraphGen(stateDir, doc); err != nil {
			t.Fatalf("write doctored baseline: %v", err)
		}
	}
	return stripped
}

// lsPropEntityIDs6472 returns the IDs of every component_prop in doc that was
// extracted from the unchanged file.
func lsPropEntityIDs6472(doc *graph.Document) map[string]string {
	out := map[string]string{}
	for _, e := range doc.Entities {
		subtype := e.Subtype
		if subtype == "" {
			subtype = e.PropGet("subtype")
		}
		if subtype == "component_prop" && filepath.ToSlash(e.SourceFile) == lsUnchangedFile6472 {
			out[e.ID] = e.Name
		}
	}
	return out
}

// lsImportsBoundToProps6472 returns the component_prop entities that an IMPORTS
// edge out of the changed file actually resolved to, as "name(id)" strings.
//
// This is the identity check the load-bearing assertion needs: a resolved edge
// has had its ToID rewritten to the target's ID, so membership in the prop-ID
// set is exactly "this import bound to a props parameter" — not a proxy such as
// "this import bound to something in that file", which a correct binding also
// satisfies.
func lsImportsBoundToProps6472(doc *graph.Document) []string {
	props := lsPropEntityIDs6472(doc)
	if len(props) == 0 {
		return nil
	}
	fromIDs := make(map[string]bool)
	for _, e := range doc.Entities {
		if filepath.ToSlash(e.SourceFile) == lsChangedFile6472 && e.Kind == "SCOPE.Component" {
			fromIDs[e.ID] = true
		}
	}
	var hits []string
	for _, r := range doc.Relationships {
		if r.Kind != "IMPORTS" || !fromIDs[r.FromID] {
			continue
		}
		if name, ok := props[r.ToID]; ok {
			hits = append(hits, name+"("+r.ToID+")")
		}
	}
	return hits
}

// TestPathBIncremental_LegacyComponentPropKeepsLocality_6472 is the regression
// pin for the upgrade window.
//
// NON-VACUITY, in the order the checks run:
//
//  1. the baseline must actually contain a stamped component_prop named
//     "toolkit" — otherwise the extractor never produced the shape and every
//     later assertion is about an empty set;
//  2. the doctoring must actually strip at least one stamp — otherwise the
//     "pre-#6472 graph" premise is false and the carry-forward seam is never
//     asked to do anything;
//  3. the incremental run must really have taken the incremental branch and
//     carried entities forward (asserted inside cfPathBIncremental);
//  4. the full-rebuild reference must bind the import to something — otherwise
//     "did not bind to the prop" is satisfied by a graph with no edges at all.
//
// Only then is the load-bearing assertion made: the IMPORTS edge out of the
// changed file must not point at the carried prop, and must land on the same
// target set a clean full rebuild produces.
func TestPathBIncremental_LegacyComponentPropKeepsLocality_6472(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	lsWriteUnchanged6472(t, repo)
	lsWriteChanged6472(t, repo, 0)
	base := dvFullRebuild(t, repo, stateDir)

	// (1) The fixture really produced the shape under test, WITH the stamp.
	baseProps := lsPropEntityIDs6472(base)
	if len(baseProps) == 0 {
		t.Fatalf("fixture is inert: the baseline holds no component_prop from %s — "+
			"the React prop emitter never fired, so nothing below is under test",
			lsUnchangedFile6472)
	}
	foundToolkit := false
	for _, name := range baseProps {
		if name == "toolkit" {
			foundToolkit = true
		}
	}
	if !foundToolkit {
		t.Fatalf("fixture is inert: no component_prop named \"toolkit\" in %s (got %v); the "+
			"name collision with the imported package is what creates the #6467 shape",
			lsUnchangedFile6472, baseProps)
	}

	// End-state reference: a clean full rebuild of exactly what the incremental
	// run will land on. A full rebuild re-extracts everything, so its props
	// carry the stamp from the emitter and it is the correct-by-construction
	// answer.
	fullRepo := t.TempDir()
	lsWriteUnchanged6472(t, fullRepo)
	lsWriteChanged6472(t, fullRepo, 1)
	full := dvFullRebuild(t, fullRepo, t.TempDir())

	// (2) Doctor the baseline into a pre-#6472 graph.
	stripped := lsStripLocalScope6472(t, stateDir)
	if stripped == 0 {
		t.Fatalf("premise broken: nothing to strip — the baseline's component_props carried no " +
			"local_scope, so this run does not reproduce a pre-#6472 graph and the " +
			"carry-forward compatibility rule is never exercised")
	}

	// (3) Incremental pass over the doctored baseline.
	dvSeedManifest(t, repo, stateDir)
	lsWriteChanged6472(t, repo, 1)
	inc := cfPathBIncremental(t, repo, stateDir)

	// (4) The reference actually binds the import somewhere.
	wantT := cfOutboundTargets(full, lsChangedFile6472, "IMPORTS")
	want := cfResolvedTargetSet(wantT)
	gotT := cfOutboundTargets(inc, lsChangedFile6472, "IMPORTS")
	got := cfResolvedTargetSet(gotT)
	if len(wantT) == 0 {
		t.Fatalf("fixture is inert: the full rebuild emitted no IMPORTS edge out of %s at all",
			lsChangedFile6472)
	}

	// (5) Non-vacuity for the OVER-STAMP direction: the full rebuild must
	// actually bind an import INTO the unchanged file. That binding is what a
	// too-wide shim would destroy, so if it never existed the parity oracle
	// below could not observe over-stamping at all.
	boundIntoUnchanged := false
	for _, tgt := range wantT {
		if tgt.Resolved && filepath.ToSlash(tgt.SourceFile) == lsUnchangedFile6472 {
			boundIntoUnchanged = true
		}
	}
	if !boundIntoUnchanged {
		t.Fatalf("fixture is inert for the over-stamp direction: the full rebuild binds no "+
			"IMPORTS edge into %s, so a shim that stamped every carried record would not be "+
			"visible here. targets=%v", lsUnchangedFile6472, wantT)
	}

	// The load-bearing assertion, keyed on the PROP'S IDENTITY rather than on
	// its source file.
	//
	// An earlier version flagged any resolved target whose SourceFile was the
	// unchanged file while its message diagnosed "bound to the React props
	// parameter". With one bindable entity in that file those coincided; with
	// sharedHelperU present a CORRECT binding trips the file-level check and is
	// reported with a false diagnosis. The assertion must observe the artefact
	// it names, so it now matches IMPORTS edges whose ToID is a component_prop
	// entity ID.
	if hits := lsImportsBoundToProps6472(inc); len(hits) > 0 {
		t.Errorf("%s -IMPORTS-> %v: the import bound to a React props parameter carried "+
			"forward from the unchanged file. A binding that exists only inside the "+
			"component callable must never take the repository-wide byName slot — a graph "+
			"written before the #6472 stamp has to keep resolving the way it did on the "+
			"old binary (#6467). full-rebuild targets=%v",
			lsChangedFile6472, hits, wantT)
	}

	// And the incremental answer matches the full rebuild's, which is the
	// definition of correct for an incremental pass.
	if !sameTargetSet6472(got, want) {
		t.Errorf("IMPORTS target set out of %s diverged from a clean full rebuild.\n got=%v\nwant=%v",
			lsChangedFile6472, gotT, wantT)
	}
}

func sameTargetSet6472(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// TestLegacyLocalScopeStampRule_6472 pins the compatibility rule itself: it is
// byte-for-byte the predicate that was deleted from isLocalBindingKind, so a
// carried record resolves exactly as it did on the old binary — no more, no
// less.
//
// These cases are what the deleted clause's mutants used to kill. With the
// clause gone from the resolver they would have become decorative; the rule
// they describe now lives here, so they are pinned here (#6472 F5).
func TestLegacyLocalScopeStampRule_6472(t *testing.T) {
	cases := []struct {
		name    string
		subtype string
		props   map[string]string
		want    bool
	}{
		{
			name: "react component_prop with no stamp — the upgrade shape",
			// This is what a pre-#6472 graph holds for a React props parameter.
			subtype: "component_prop",
			props:   map[string]string{"subtype": "component_prop", "framework": "react"},
			want:    true,
		},
		{
			name:    "already stamped — written by a post-#6472 binary",
			subtype: "component_prop",
			props: map[string]string{
				"subtype": "component_prop", "framework": "react", "local_scope": "true",
			},
			want: false,
		},
		{
			// angular's @Input() IS a component's public surface, addressable
			// from a parent template as <chart [Data]="…">. It must keep the
			// repository-wide slot, so it must NOT be stamped.
			name:    "angular @Input() component_prop",
			subtype: "component_prop",
			props:   map[string]string{"subtype": "component_prop", "framework": "angular"},
			want:    false,
		},
		{
			// vue's defineProps, likewise: <Chart :Data="…">.
			name:    "vue defineProps component_prop",
			subtype: "component_prop",
			props:   map[string]string{"subtype": "component_prop", "framework": "vue"},
			want:    false,
		},
		{
			// The SUBTYPE half. Without this, `subtype != "" && framework ==
			// "react"` passes — the framework name alone carrying the rule,
			// which is the mutant that survived review on the original clause.
			name:    "react-stamped entity that is not a component_prop",
			subtype: "react_hook",
			props:   map[string]string{"subtype": "react_hook", "framework": "react"},
			want:    false,
		},
		{
			name:    "component_prop with no framework at all",
			subtype: "component_prop",
			props:   map[string]string{"subtype": "component_prop"},
			want:    false,
		},
		{
			// #2015 two-carrier fallback: a carried record has been through a
			// persist/load round-trip, so the struct field may be empty while
			// the subtype rides in Properties.
			name:    "subtype in Properties only",
			subtype: "",
			props:   map[string]string{"subtype": "component_prop", "framework": "react"},
			want:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsLegacyLocalScopeStamp(tc.subtype, tc.props); got != tc.want {
				t.Errorf("needsLegacyLocalScopeStamp(%q, %v) = %v, want %v",
					tc.subtype, tc.props, got, tc.want)
			}
			// applyLegacyLocalScopeStamp must agree, and must leave every other
			// property untouched.
			out := applyLegacyLocalScopeStamp(tc.subtype, copyProps6472(tc.props))
			if (out["local_scope"] == "true") != (tc.want || tc.props["local_scope"] == "true") {
				t.Errorf("applyLegacyLocalScopeStamp: local_scope=%q, want stamped=%v",
					out["local_scope"], tc.want || tc.props["local_scope"] == "true")
			}
			for k, v := range tc.props {
				if k == "local_scope" {
					continue
				}
				if out[k] != v {
					t.Errorf("applyLegacyLocalScopeStamp clobbered %q: %q → %q", k, v, out[k])
				}
			}
		})
	}
}

func copyProps6472(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// TestLegacyLocalScopeStampIsAppliedAtTheCarrySeam_6472 guards against the
// helper above being correct while the call site ignores it — a defect class
// this repo has hit before. It reads the production source and asserts the
// carry-forward record construction routes Properties through the shim.
//
// This is a source-level assertion, which is weak on its own; it exists only as
// a cheap tripwire beside
// TestPathBIncremental_LegacyComponentPropKeepsLocality_6472, which observes
// the behaviour end-to-end through the real indexer.
func TestLegacyLocalScopeStampIsAppliedAtTheCarrySeam_6472(t *testing.T) {
	b, err := os.ReadFile("index.go")
	if err != nil {
		t.Fatalf("read index.go: %v", err)
	}
	src := string(b)
	const want = "Properties:    applyLegacyLocalScopeStamp(pe.Subtype, pe.PropsSnapshot()),"
	if !strings.Contains(src, want) {
		t.Errorf("cmd/grafel/index.go's carry-forward record no longer routes Properties through "+
			"applyLegacyLocalScopeStamp. The compatibility rule is only reachable from that one "+
			"seam — bypassing it re-opens the #6467 regression for every graph written before "+
			"#6472. Wanted to find: %q", want)
	}
}

// TestResolverIndexHasExactlyOneProductionSeam_6472 guards the PREMISE the
// compatibility shim's completeness rests on.
//
// The argument for putting the #6472 backward-compatibility rule at the
// carry-forward seam, rather than as a clause inside isLocalBindingKind, is
// that the seam is the ONLY route by which a pre-#6472 record can reach the
// resolver's predicate:
//
//   - there is exactly one production construction of the resolver index, the
//     GRAFEL_RESOLVE_MODULE_INDEX two-branch switch below;
//   - both branches consume the same `indexEntities` slice;
//   - `indexEntities` is this run's freshly extracted `merged` (already stamped
//     by the emitter) plus `incrementalCarryForwardEntities` (stamped by the
//     shim on the way in).
//
// That premise is true today and was verified in review. It is also exactly the
// kind of premise that decays silently: a second index construction added
// anywhere in production would bypass the shim and re-open #6467 for upgraded
// users, with no test going red — the seam tripwire above cannot notice a seam
// it does not know about.
//
// So this counts them. If the count changes, the new site must either consume
// `indexEntities` or apply applyLegacyLocalScopeStamp itself, and this test's
// expectation must be updated deliberately rather than by accident.
func TestResolverIndexHasExactlyOneProductionSeam_6472(t *testing.T) {
	b, err := os.ReadFile("index.go")
	if err != nil {
		t.Fatalf("read index.go: %v", err)
	}
	// Comments mention these names freely; only real call expressions count.
	constructions := map[string]int{
		"resolve.BuildIndex(":                   0,
		"resolve.BuildIndexFromModulesOrdered(": 0,
		"resolve.BuildIndexFromModules(":        0,
	}
	for _, line := range strings.Split(string(b), "\n") {
		code := line
		if i := strings.Index(code, "//"); i >= 0 {
			code = code[:i]
		}
		for probe := range constructions {
			// BuildIndex( is a prefix of BuildIndexFromModules*( — count the
			// most specific match only, so the totals do not double-count.
			if probe == "resolve.BuildIndex(" &&
				(strings.Contains(code, "resolve.BuildIndexFromModules")) {
				continue
			}
			if strings.Contains(code, probe) {
				constructions[probe]++
			}
		}
	}

	total := 0
	for _, n := range constructions {
		total += n
	}
	// Non-vacuity: if the probes matched nothing at all the test would "pass"
	// any expectation, so require the two known sites to be found.
	if total == 0 {
		t.Fatalf("probe is broken: found no resolver-index construction in index.go at all; "+
			"counts=%v", constructions)
	}

	const wantTotal = 2 // the two branches of the GRAFEL_RESOLVE_MODULE_INDEX switch
	if total != wantTotal {
		t.Errorf("index.go constructs the resolver index at %d site(s), want %d (counts=%v).\n\n"+
			"The #6472 backward-compatibility shim is applied where previous-graph entities "+
			"enter the index (the incrementalCarryForwardEntities slice). That placement is "+
			"only complete while every construction consumes `indexEntities`. A new site that "+
			"builds an index from previous-graph records some other way bypasses the shim and "+
			"silently re-opens the #6467 slot-capture regression for every user who has "+
			"upgraded but not yet done a full reindex.\n\n"+
			"If the new site is legitimate: make it consume `indexEntities`, or apply "+
			"applyLegacyLocalScopeStamp to its inputs, then update wantTotal here.",
			total, wantTotal, constructions)
	}

	// Both known branches must feed from the stamped slice.
	for _, probe := range []string{
		"resolve.BuildIndexFromModulesOrdered(indexEntities,",
		"resolve.BuildIndex(indexEntities)",
	} {
		if !strings.Contains(string(b), probe) {
			t.Errorf("expected a resolver-index construction fed by the stamped `indexEntities` "+
				"slice: %q not found. If the argument was renamed, confirm it still carries the "+
				"carry-forward records that applyLegacyLocalScopeStamp wrote.", probe)
		}
	}
}
