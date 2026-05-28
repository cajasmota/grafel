// Package javascript — issue #2856 real-data verification (Navigation +
// Lifecycle). Runs the registered TypeScript extractor over the in-repo
// real-world Angular fixture and asserts NAVIGATES_TO edges + state_setter
// operations fire on real-shaped source.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestIssue2856_RealData_AngularNavLifecycle(t *testing.T) {
	ents := extractRealWorld(t, "angular_nav_lifecycle_component.ts")

	routes := map[string]bool{}
	vias := map[string]bool{}
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindNavigatesTo) {
				routes[r.Properties["route"]] = true
				vias[r.Properties["via"]] = true
			}
		}
	}
	// Imperative + routerLink navigation.
	for _, want := range []string{"/settings", "/profile", "/dashboard", "/users/{*}"} {
		if !routes[want] {
			t.Errorf("missing real-data NAVIGATES_TO %q; routes=%v vias=%v", want, routes, vias)
		}
	}
	if !vias["angular_router"] || !vias["router_link"] {
		t.Errorf("expected angular_router + router_link vias; got %v", vias)
	}

	// state_setter_emission: signals + ngrx dispatch.
	setters := map[string]bool{}
	writes := map[string]bool{}
	for i := range ents {
		if ents[i].Subtype != "state_setter" {
			continue
		}
		setters[ents[i].Name] = true
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindWritesTo) {
				writes[r.ToID] = true
			}
		}
	}
	for _, want := range []string{"count.set", "count.update", "userId.set", "dispatch:loadUser", "dispatch:clearUser"} {
		if !setters[want] {
			t.Errorf("missing real-data state_setter %q; setters=%v; %s", want, setters, dumpKinds(ents))
		}
	}
	for _, want := range []string{"state:count", "state:userId", "state:loadUser"} {
		if !writes[want] {
			t.Errorf("missing real-data WRITES_TO %q; writes=%v", want, writes)
		}
	}
	t.Logf("real-data angular #2856: routes=%d setters=%d", len(routes), len(setters))
}
