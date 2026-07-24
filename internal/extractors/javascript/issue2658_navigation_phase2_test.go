// Package javascript_test — issue #2658 Phase 2: template route normalization
// and hook-rename binding detection for NAVIGATES_TO edges.
//
// Tests cover:
//   - router.push(`/users/${id}`)               → captured as /users/{*}
//   - const nav = useNavigation(); nav.navigate('X') → NAVIGATES_TO edge emitted
package javascript_test

import (
	"strings"
	"testing"
)

// TestNavigation_TemplateRouteNormalized verifies that a template-literal
// route like `/users/${id}/profile` is normalized to `/users/{*}/profile`
// (replacing ${…} interpolations with the {*} sentinel). Phase 2 of #2658.
func TestNavigation_TemplateRouteNormalized(t *testing.T) {
	src := `
import { useRouter } from "expo-router";

const UserProfile = () => {
  const router = useRouter();
  const goToUser = (id) => {
    router.push(` + "`" + `/users/${id}/profile` + "`" + `);
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// The route should be normalized: /users/${id}/profile → /users/{*}/profile
	wantToID := "route:/users/{*}/profile"
	if !hasRelEdge(ents, "goToUser", "NAVIGATES_TO", wantToID) {
		e := findByNameRel(ents, "goToUser")
		if e != nil {
			t.Logf("goToUser relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
		} else {
			t.Log("entity 'goToUser' not found")
		}
		t.Errorf("expected NAVIGATES_TO goToUser→%s (template normalized), not found", wantToID)
	}

	// Verify the route property itself is also normalized.
	e := findByNameRel(ents, "goToUser")
	if e == nil {
		t.Fatal("entity 'goToUser' not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "NAVIGATES_TO" && r.ToID == wantToID {
			if r.Properties == nil {
				t.Fatal("NAVIGATES_TO edge has no Properties")
			}
			gotRoute := r.Properties.Get("route")
			if !strings.Contains(gotRoute, "{*}") {
				t.Errorf("expected route to contain '{*}' placeholder, got %q", gotRoute)
			}
			// Must NOT contain raw ${...} interpolation syntax.
			if strings.Contains(gotRoute, "${") {
				t.Errorf("route must not contain raw ${...} syntax after normalization, got %q", gotRoute)
			}
			return
		}
	}
	t.Error("NAVIGATES_TO edge found by hasRelEdge but not iterable — should not happen")
}

// TestNavigation_HookRenameBinding verifies that when useNavigation() is called
// and its result is bound to a local variable (e.g. `const nav = useNavigation()`),
// a subsequent call like `nav.navigate('Home')` is recognized as a navigation
// call and emits a NAVIGATES_TO edge. Phase 2 of #2658.
func TestNavigation_HookRenameBinding(t *testing.T) {
	src := `
import { useNavigation } from "@react-navigation/native";

const MyScreen = () => {
  const nav = useNavigation();
  const goHome = () => {
    nav.navigate('Home');
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// nav.navigate('Home') should produce a NAVIGATES_TO edge because 'nav'
	// is bound to useNavigation() which is in navigationHookNames.
	if !hasRelEdge(ents, "goHome", "NAVIGATES_TO", "route:Home") {
		e := findByNameRel(ents, "goHome")
		if e != nil {
			t.Logf("goHome relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
		} else {
			t.Log("entity 'goHome' not found")
		}
		t.Errorf("expected NAVIGATES_TO goHome→route:Home via hook-rename binding (const nav = useNavigation()), not found")
	}

	// Verify the route property.
	e := findByNameRel(ents, "goHome")
	if e == nil {
		t.Fatal("entity 'goHome' not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "NAVIGATES_TO" && r.ToID == "route:Home" {
			if r.Properties.Get("route") != "Home" {
				t.Errorf("expected Properties[route]='Home', got %v", r.Properties.Get("route"))
			}
			if r.Properties.Get("via") != "navigation_call" {
				t.Errorf("expected Properties[via]='navigation_call', got %v", r.Properties.Get("via"))
			}
			return
		}
	}
	t.Error("NAVIGATES_TO edge not found on goHome after hasRelEdge passed — unexpected")
}
