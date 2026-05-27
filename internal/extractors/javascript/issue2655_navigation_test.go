// Package javascript_test — issue #2655: NAVIGATES_TO edge extraction for
// Expo Router / React Navigation / Next.js navigation call sites.
//
// Tests cover:
//   - router.push('/path')                          → NAVIGATES_TO route:'/path'
//   - router.push({pathname: '/x', params: {a, b}}) → route='/x', params='a, b'
//   - navigation.navigate('Screen')                 → NAVIGATES_TO route:'Screen'
//   - Linking.openURL('https://...')                → NAVIGATES_TO (external URL)
//   - foo.push(item)                                → no NAVIGATES_TO edge
package javascript_test

import (
	"strings"
	"testing"
)

// TestNavigationExtractor_RouterPush_EmitsEdge verifies that router.push('/foo')
// emits a NAVIGATES_TO edge with ToID="route:/foo". Issue #2655.
func TestNavigationExtractor_RouterPush_EmitsEdge(t *testing.T) {
	src := `
import { useRouter } from "expo-router";

const MyComponent = () => {
  const router = useRouter();
  const goFoo = () => {
    router.push('/foo');
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasRelEdge(ents, "goFoo", "NAVIGATES_TO", "route:/foo") {
		e := findByNameRel(ents, "goFoo")
		if e != nil {
			t.Logf("goFoo relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
		} else {
			t.Log("entity 'goFoo' not found")
		}
		t.Errorf("expected NAVIGATES_TO goFoo→route:/foo, not found")
	}

	// Verify the route property is set correctly.
	e := findByNameRel(ents, "goFoo")
	if e == nil {
		t.Fatal("entity 'goFoo' not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "NAVIGATES_TO" && r.ToID == "route:/foo" {
			if r.Properties == nil || r.Properties["route"] != "/foo" {
				t.Errorf("expected Properties[route]='/foo', got %v", r.Properties)
			}
			if r.Properties["via"] != "navigation_call" {
				t.Errorf("expected Properties[via]='navigation_call', got %v", r.Properties["via"])
			}
			return
		}
	}
	t.Error("NAVIGATES_TO edge found but missing expected properties")
}

// TestNavigationExtractor_RouterPushObjectForm verifies that the object-form
// navigation call router.push({pathname: '/users/[id]', params: {id, type}})
// emits a NAVIGATES_TO edge with route='/users/[id]' and params='id, type'.
// Issue #2655.
func TestNavigationExtractor_RouterPushObjectForm(t *testing.T) {
	src := `
import { useRouter } from "expo-router";

const UserNavigator = () => {
  const router = useRouter();
  const goToUser = (id, type) => {
    router.push({
      pathname: '/users/[id]',
      params: { id, type },
    });
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasRelEdge(ents, "goToUser", "NAVIGATES_TO", "route:/users/[id]") {
		e := findByNameRel(ents, "goToUser")
		if e != nil {
			t.Logf("goToUser relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
		}
		t.Fatalf("expected NAVIGATES_TO goToUser→route:/users/[id], not found")
	}

	// Verify params list is present.
	e := findByNameRel(ents, "goToUser")
	if e == nil {
		t.Fatal("entity 'goToUser' not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "NAVIGATES_TO" && r.ToID == "route:/users/[id]" {
			if r.Properties == nil {
				t.Fatal("NAVIGATES_TO edge has no Properties")
			}
			gotParams := r.Properties["params"]
			if gotParams == "" {
				t.Errorf("expected params to be set, got empty")
			}
			// Both 'id' and 'type' must appear in the params list.
			for _, want := range []string{"id", "type"} {
				found := false
				for _, p := range splitParams(gotParams) {
					if p == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected param %q in params=%q", want, gotParams)
				}
			}
			return
		}
	}
	t.Error("NAVIGATES_TO edge not found on goToUser (after hasRelEdge passed — should not happen)")
}

// TestNavigationExtractor_NavigationNavigate verifies that
// navigation.navigate('Home') emits a NAVIGATES_TO edge with route='Home'.
// Issue #2655.
func TestNavigationExtractor_NavigationNavigate(t *testing.T) {
	src := `
const HomeButton = ({ navigation }) => {
  const goHome = () => {
    navigation.navigate('Home');
  };
  return null;
};
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	if !hasRelEdge(ents, "goHome", "NAVIGATES_TO", "route:Home") {
		e := findByNameRel(ents, "goHome")
		if e != nil {
			t.Logf("goHome relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
		}
		t.Errorf("expected NAVIGATES_TO goHome→route:Home, not found")
	}
}

// TestNavigationExtractor_LinkingOpenURL verifies that
// Linking.openURL('https://example.com') emits a NAVIGATES_TO edge.
// Issue #2655.
func TestNavigationExtractor_LinkingOpenURL(t *testing.T) {
	src := `
import { Linking } from "react-native";

const openExternal = () => {
  Linking.openURL('https://example.com/terms');
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasRelEdge(ents, "openExternal", "NAVIGATES_TO", "route:https://example.com/terms") {
		e := findByNameRel(ents, "openExternal")
		if e != nil {
			t.Logf("openExternal relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
		}
		t.Errorf("expected NAVIGATES_TO openExternal→route:https://example.com/terms, not found")
	}
}

// TestNavigationExtractor_NonNavCall_NoEdge verifies that a plain array .push()
// call does NOT emit a NAVIGATES_TO edge. Issue #2655.
func TestNavigationExtractor_NonNavCall_NoEdge(t *testing.T) {
	src := `
const addItem = (items, item) => {
  items.push(item);
  return items;
};
`
	tree := parseJSRel(t, []byte(src))
	ents := runJS(t, src, "javascript", tree)

	e := findByNameRel(ents, "addItem")
	if e == nil {
		t.Fatal("entity 'addItem' not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "NAVIGATES_TO" {
			t.Errorf("unexpected NAVIGATES_TO edge on addItem: %v", r)
		}
	}
}

// TestNavigationExtractor_RouterReplace_EmitsEdge verifies that router.replace
// also emits a NAVIGATES_TO edge. Issue #2655.
func TestNavigationExtractor_RouterReplace_EmitsEdge(t *testing.T) {
	src := `
import { useRouter } from "next/navigation";

const LoginPage = () => {
  const router = useRouter();
  const redirectHome = () => {
    router.replace('/home');
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasRelEdge(ents, "redirectHome", "NAVIGATES_TO", "route:/home") {
		e := findByNameRel(ents, "redirectHome")
		if e != nil {
			t.Logf("redirectHome relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
		}
		t.Errorf("expected NAVIGATES_TO redirectHome→route:/home, not found")
	}
}

// TestNavigationExtractor_RouterBack_EmitsEdge verifies that router.back()
// emits a NAVIGATES_TO edge with route='<back>'. Issue #2655.
func TestNavigationExtractor_RouterBack_EmitsEdge(t *testing.T) {
	src := `
import { useRouter } from "expo-router";

const BackButton = () => {
  const router = useRouter();
  const goBack = () => {
    router.back();
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if !hasRelEdge(ents, "goBack", "NAVIGATES_TO", "route:<back>") {
		e := findByNameRel(ents, "goBack")
		if e != nil {
			t.Logf("goBack relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
		}
		t.Errorf("expected NAVIGATES_TO goBack→route:<back>, not found")
	}
}

// splitParams is a helper that splits a comma-separated params string into
// individual param key names (trimming spaces).
func splitParams(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
