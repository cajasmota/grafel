package svelte_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #2855 — Svelte Data-Flow group: prop_extraction (export let / $props),
// state_management (writable/derived stores), data_fetching (fetch/axios),
// branch_conditions ({#if}/{#each}/{:else if}).
func TestIssue2855_SvelteDataFlow(t *testing.T) {
	src := `<script lang="ts">
  import { writable, derived } from 'svelte/store'
  export let title: string
  let { count = 0 } = $props()

  const items = writable([])
  const total = derived(items, ($i) => $i.length)

  async function load() {
    const res = await fetch('/api/items')
    return res.json()
  }
</script>

{#if title}
  <h1>{title}</h1>
{:else if count}
  <p>{count}</p>
{/if}
{#each $items as item}
  <Row {item} />
{/each}`

	recs := mustExtract(t, "src/lib/List.svelte", src)

	comp := findByName(recs, "List")
	if comp == nil {
		t.Fatalf("List component not extracted; %s", dump(recs))
	}

	has := func(subtype, name string) bool {
		for i := range recs {
			if recs[i].Subtype == subtype && recs[i].Name == name {
				return true
			}
		}
		return false
	}

	// prop_extraction: export let + $props destructure (existing #2854 props).
	if !has("prop", "title") {
		t.Errorf("missing prop title; %s", dump(recs))
	}
	if !has("prop", "count") {
		t.Errorf("missing prop count")
	}

	// state_management: writable + derived stores.
	if !has("state_store", "items") {
		t.Errorf("missing state_store items; %s", dump(recs))
	}
	if !has("state_store", "total") {
		t.Errorf("missing state_store total")
	}

	// data_fetching: fetch call.
	hasFetch := false
	for i := range recs {
		if recs[i].Subtype == "data_fetch" {
			hasFetch = true
		}
	}
	if !hasFetch {
		t.Errorf("missing data_fetch; %s", dump(recs))
	}

	// branch_conditions: {#if}, {:else if}, {#each}.
	branchKinds := map[string]bool{}
	for i := range recs {
		if recs[i].Subtype == "branch_condition" {
			branchKinds[recs[i].Properties["branch_kind"]] = true
		}
	}
	for _, want := range []string{"if", "else_if", "each"} {
		if !branchKinds[want] {
			t.Errorf("missing branch_condition %q; %s", want, dump(recs))
		}
	}

	// USES wiring from component file to a store.
	var itemsStore *types.EntityRecord
	for i := range recs {
		if recs[i].Subtype == "state_store" && recs[i].Name == "items" {
			itemsStore = &recs[i]
		}
	}
	if itemsStore == nil || !relTo(comp.Relationships, "USES", "items") {
		t.Errorf("component missing USES → items store; rels=%v", comp.Relationships)
	}
}

// TestIssue2855_SvelteRealData runs the registered Svelte extractor over the
// in-repo real-world fixture and asserts Data-Flow signals on real source.
func TestIssue2855_SvelteRealData(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "real-world", "svelte", "UserList.svelte")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real-world svelte fixture not present: %v", err)
	}
	recs := mustExtract(t, path, string(content))

	count := func(subtype string) int {
		n := 0
		for i := range recs {
			if recs[i].Subtype == subtype {
				n++
			}
		}
		return n
	}
	if count("prop") < 3 {
		t.Errorf("svelte prop = %d, want >= 3 (title/pageSize/selectedId); %s", count("prop"), dump(recs))
	}
	if count("state_store") < 2 {
		t.Errorf("svelte state_store = %d, want >= 2 (users/count/loading)", count("state_store"))
	}
	if count("data_fetch") < 1 {
		t.Errorf("svelte data_fetch = %d, want >= 1 (fetch)", count("data_fetch"))
	}
	if count("branch_condition") < 2 {
		t.Errorf("svelte branch_condition = %d, want >= 2 (if/else_if/each); %s", count("branch_condition"), dump(recs))
	}
	t.Logf("real-data svelte: prop=%d store=%d fetch=%d branch=%d",
		count("prop"), count("state_store"), count("data_fetch"), count("branch_condition"))
}
