package svelte_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #2854 — Svelte Structure/context_extraction: setContext/getContext.
func TestIssue2854_SvelteContext(t *testing.T) {
	src := `<script lang="ts">
  import { setContext, getContext } from 'svelte';
  setContext('theme', { dark: true });
  const auth = getContext('auth');
</script>

<Panel />`

	recs := mustExtract(t, "src/lib/App.svelte", src)

	comp := findByName(recs, "App")
	if comp == nil {
		t.Fatalf("App component not extracted")
	}

	if !hasCtxEntity(recs, "provide_context", "theme") {
		t.Errorf("missing provide_context for key 'theme'; recs=%s", dump(recs))
	}
	if !hasCtxEntity(recs, "inject_context", "auth") {
		t.Errorf("missing inject_context for key 'auth'")
	}
	if !relTo(comp.Relationships, "USES", "provider:theme") {
		t.Errorf("App missing USES → provider:theme; rels=%v", comp.Relationships)
	}
	if !relTo(comp.Relationships, "USES", "consumer:auth") {
		t.Errorf("App missing USES → consumer:auth")
	}
}

func hasCtxEntity(recs []types.EntityRecord, subtype, key string) bool {
	for _, e := range recs {
		if e.Subtype == subtype && e.Properties["context_key"] == key {
			return true
		}
	}
	return false
}

func relTo(rels []types.RelationshipRecord, kind, toID string) bool {
	for _, r := range rels {
		if r.Kind == kind && r.ToID == toID {
			return true
		}
	}
	return false
}

func dump(recs []types.EntityRecord) string {
	out := ""
	for _, e := range recs {
		out += e.Subtype + ":" + e.Name + " "
	}
	return out
}
