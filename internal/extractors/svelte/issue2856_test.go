package svelte_test

import (
	"os"
	"path/filepath"
	"testing"
)

// Issue #2856 — Svelte Navigation (router_pattern) + Lifecycle
// (state_setter_emission).
//
// Svelte has no built-in router; routing in a plain Svelte SPA is provided by
// svelte-routing (<Route>, <Link>, navigate()) / svelte-spa-router (push()).
// Those genuine client-side routing idioms back the router_pattern cell. The
// state_setter cell is backed by store .set/.update and $store assignment.
func TestIssue2856_SvelteNavigation(t *testing.T) {
	src := `<script lang="ts">
  import { navigate } from 'svelte-routing'
  import { push } from 'svelte-spa-router'

  function go() {
    navigate('/dashboard')
    push('/settings')
  }
</script>

<Route path="/users" component={Users} />
<Link to="/about">About</Link>`

	recs := mustExtract(t, "src/App.svelte", src)

	routes := map[string]bool{}
	vias := map[string]bool{}
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "NAVIGATES_TO" {
				routes[r.Properties["route"]] = true
				vias[r.Properties["via"]] = true
			}
		}
	}
	for _, want := range []string{"/dashboard", "/settings", "/users", "/about"} {
		if !routes[want] {
			t.Errorf("missing NAVIGATES_TO %q; routes=%v vias=%v", want, routes, vias)
		}
	}
	for _, v := range []string{"navigate_call", "spa_router_call", "route_table", "link"} {
		if !vias[v] {
			t.Errorf("missing navigation via %q; vias=%v", v, vias)
		}
	}
}

func TestIssue2856_SvelteStateSetter(t *testing.T) {
	src := `<script lang="ts">
  import { writable } from 'svelte/store'

  const count = writable(0)
  const name = writable('')

  function update() {
    count.set(1)
    count.update(n => n + 1)
    name.set('x')
    $count = 5
  }
</script>`

	recs := mustExtract(t, "src/Counter.svelte", src)

	setters := map[string]string{}
	writes := map[string]bool{}
	for i := range recs {
		e := &recs[i]
		if e.Subtype != "state_setter" {
			continue
		}
		setters[e.Name] = e.Properties["state"]
		for _, r := range e.Relationships {
			if r.Kind == "WRITES_TO" {
				writes[r.ToID] = true
			}
		}
	}

	for name, wantState := range map[string]string{
		"count.set":    "count",
		"count.update": "count",
		"name.set":     "name",
		"$count=":      "count",
	} {
		if setters[name] != wantState {
			t.Errorf("missing/wrong state_setter %q (got state=%q want %q); setters=%v; %s",
				name, setters[name], wantState, setters, dump(recs))
		}
	}
	for _, target := range []string{"state:count", "state:name"} {
		if !writes[target] {
			t.Errorf("missing WRITES_TO %s; writes=%v", target, writes)
		}
	}
}

// TestIssue2856_SvelteNoFalseSetters guards that a .set call on a non-store
// receiver (e.g. a Set/Map) does not produce a state_setter.
func TestIssue2856_SvelteNoFalseSetters(t *testing.T) {
	src := `<script lang="ts">
  const seen = new Set()
  function go() { seen.add(1) }
  const m = new Map()
  function go2() { m.set('k', 1) }
</script>`
	recs := mustExtract(t, "src/X.svelte", src)
	for i := range recs {
		if recs[i].Subtype == "state_setter" {
			t.Errorf("unexpected state_setter for non-store receiver: %q", recs[i].Name)
		}
	}
}

// TestIssue2856_SvelteRealData runs the registered Svelte extractor over the
// extended real-world fixture and asserts navigation + setter signals fire.
func TestIssue2856_SvelteRealData(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "real-world", "svelte", "UserList.svelte")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real-world svelte fixture not present: %v", err)
	}
	recs := mustExtract(t, path, string(content))
	nav, setter := 0, 0
	for i := range recs {
		if recs[i].Subtype == "state_setter" {
			setter++
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "NAVIGATES_TO" {
				nav++
			}
		}
	}
	if nav < 1 {
		t.Errorf("svelte real-data NAVIGATES_TO = %d, want >= 1; %s", nav, dump(recs))
	}
	if setter < 1 {
		t.Errorf("svelte real-data state_setter = %d, want >= 1", setter)
	}
	t.Logf("real-data svelte #2856: nav=%d setters=%d", nav, setter)
}
