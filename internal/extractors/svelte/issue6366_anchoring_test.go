package svelte_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// issue6366_anchoring_test.go — RENDERS, USES, NAVIGATES_TO and CONTAINS must be
// anchored on the .svelte component that owns them, not on the file path.
//
// WHAT WAS MEASURED BEFORE THE FIX. The two .svelte files below, extracted,
// id-stamped and pushed through the production resolver pipeline
// (ResolveImports → ReferencesEmbedded), produced — all fifteen template and
// script edges, every one of the four kinds:
//
//	owner="Card" kind=RENDERS      FROM=<UNRESOLVED:src/lib/Card.svelte> TO=Button@src/lib/Button.svelte
//	owner="Card" kind=USES         FROM=<UNRESOLVED:src/lib/Card.svelte> TO=count@src/lib/Card.svelte
//	owner="Card" kind=CONTAINS     FROM=<UNRESOLVED:src/lib/Card.svelte> TO=count.set@src/lib/Card.svelte
//	owner="Card" kind=NAVIGATES_TO FROM=<UNRESOLVED:src/lib/Card.svelte> TO=<UNRESOLVED:route:/home>
//
// "UNRESOLVED" is literal: no entity in the set carries that ID. This package
// emits no extractor.FileEntity, so — as with astro (#6298) and unlike Solidity
// (#6295)/verilog, where the rewrite at least landed on a real file component —
// the path survived rewriting verbatim and reached graph assembly as a DANGLING
// FromID. Both assembly paths (cmd/grafel/index.go and relRecordToGraphRel in
// internal/extractors/incremental.go) substitute the owning record's own entity
// id only when FromID is EMPTY, so a non-empty path is passed straight through.
//
// All four kinds behaved identically on the FROM side: 15 of 15 dangling before,
// 0 of 15 after. NAVIGATES_TO differs only on the TO side — its target is the
// symbolic "route:<path>" id shared with vue and javascript (see
// internal/extractors/javascript/navigation.go), which is resolved downstream
// and is deliberately not an entity in this package's own output. That is not
// what this test is about, and it is unchanged by the fix.
//
// After the fix every one of the four kinds carries FromID == "", which assembly
// stamps with Card's id — asserted below by replaying that substitution.
func TestSvelte_ComponentRelsAnchoredOnComponent(t *testing.T) {
	const cardPath = "src/lib/Card.svelte"
	files := map[string]string{
		cardPath: `<script>
  import Button from './Button.svelte';
  import { writable } from 'svelte/store';
  import { setContext } from 'svelte';
  import { navigate } from 'svelte-routing';
  const count = writable(0);
  setContext('theme', 'dark');
  $: doubled = $count * 2;
  function go() { navigate('/home'); }
  function bump() { count.set(1); }
</script>

<div use:tooltip>
  {#if $count > 0}
    <Button />
  {/if}
  <Link to="/about">About</Link>
</div>
`,
		"src/lib/Button.svelte": "<button>hi</button>\n",
	}

	var recs []types.EntityRecord
	for _, p := range slices.Sorted(maps.Keys(files)) {
		recs = append(recs, mustExtract(t, p, files[p])...)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6366", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}

	// The premise of the dangle: nothing in this package's output is named for
	// the file, so the file path can never resolve to an entity here. If svelte
	// ever starts emitting a FileEntity this assertion is the thing that should
	// be revisited — but the fix below is correct either way.
	for i := range recs {
		if recs[i].Name == cardPath {
			t.Errorf("svelte now emits a file-named entity (%s/%s); "+
				"re-read the dangle rationale in this file's doc comment",
				recs[i].Kind, recs[i].Subtype)
		}
	}

	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))

	var card *types.EntityRecord
	for i := range recs {
		if recs[i].Name == "Card" && recs[i].Kind == "SCOPE.Component" {
			card = &recs[i]
		}
	}
	if card == nil {
		t.Fatal("no Card component entity")
	}

	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	// resolveEndpoint renders an id the way the graph would see it.
	resolveEndpoint := func(id string) string {
		if e := byID[id]; e != nil {
			return e.Name + "@" + e.SourceFile
		}
		return "<UNRESOLVED:" + id + ">"
	}

	// Endpoints, not counts: a count can stay identical while an edge lands on
	// the wrong node.
	got := map[string][]string{}
	for _, r := range card.Relationships {
		switch r.Kind {
		case "RENDERS", "USES", "NAVIGATES_TO", "CONTAINS":
		default:
			continue // IMPORTS keeps the file path on purpose (#120).
		}
		// Replay graph assembly: the owning record's id is substituted only when
		// FromID is empty (cmd/grafel/index.go, internal/extractors/incremental.go).
		from := r.FromID
		if from == "" {
			from = card.ID
		}
		if fromName := resolveEndpoint(from); fromName != "Card@"+cardPath {
			t.Errorf("%s → %s: FROM = %s, want Card@%s (FromID=%q must be empty so "+
				"assembly stamps the component)", r.Kind, resolveEndpoint(r.ToID), fromName, cardPath, r.FromID)
		}
		got[r.Kind] = append(got[r.Kind], resolveEndpoint(r.ToID))
	}

	want := map[string][]string{
		"RENDERS":      {"<UNRESOLVED:Link>", "Button@src/lib/Button.svelte"},
		"USES":         {"<UNRESOLVED:provider:theme>", "<UNRESOLVED:use:tooltip>", "count@" + cardPath, "doubled@" + cardPath, "if@" + cardPath},
		"NAVIGATES_TO": {"<UNRESOLVED:route:/about>", "<UNRESOLVED:route:/home>"},
		"CONTAINS":     {"count.set@" + cardPath},
	}
	for kind, wantTargets := range want {
		gotTargets := slices.Clone(got[kind])
		slices.Sort(gotTargets)
		slices.Sort(wantTargets)
		if !slices.Equal(gotTargets, wantTargets) {
			t.Errorf("%s targets = %v, want %v", kind, gotTargets, wantTargets)
		}
	}
	for kind := range got {
		if _, ok := want[kind]; !ok {
			t.Errorf("unexpected relationship kind %q on Card: %v", kind, got[kind])
		}
	}
}
