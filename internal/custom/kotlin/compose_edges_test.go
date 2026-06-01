package kotlin_test

// ---------------------------------------------------------------------------
// Compose edge tests (issue #3576): NAVIGATES_TO (screen -> route) and
// USES (composable -> ViewModel). These assert the SPECIFIC edge endpoints,
// not len>0.
// ---------------------------------------------------------------------------

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractRels runs the named extractor and returns the raw entity records so
// embedded Relationships are inspectable (the shared entitySummary helper
// drops them).
func extractRels(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

// hasEdge reports whether some entity named fromName carries a relationship of
// the given kind whose ToID equals wantToID.
func hasEdge(ents []types.EntityRecord, fromName, kind, wantToID string) bool {
	for _, e := range ents {
		if e.Name != fromName {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == kind && r.ToID == wantToID {
				return true
			}
		}
	}
	return false
}

// TestComposeNavigatesToEdge asserts the screen->route NAVIGATES_TO edge with a
// normalized {id} param: composable "home"'s screen navigates to detail/{id}.
func TestComposeNavigatesToEdge(t *testing.T) {
	src := `
@Composable
fun HomeScreen(navController: NavController) {
    Button(onClick = { navController.navigate("detail/42") }) {
        Text("Open")
    }
}

@Composable
fun DetailScreen(navController: NavController) {
    Button(onClick = { navController.navigate("home") }) {
        Text("Back")
    }
}
`
	ents := extractRels(t, "custom_kotlin_compose", fi("Nav.kt", "kotlin", src))

	// HomeScreen -NAVIGATES_TO-> route:detail/{id} (42 normalized to {id})
	if !hasEdge(ents, "HomeScreen", "NAVIGATES_TO", "route:detail/{id}") {
		t.Errorf("expected HomeScreen NAVIGATES_TO route:detail/{id}; edges=%v", dumpEdges(ents))
	}
	// DetailScreen -NAVIGATES_TO-> route:home (constant route, unchanged)
	if !hasEdge(ents, "DetailScreen", "NAVIGATES_TO", "route:home") {
		t.Errorf("expected DetailScreen NAVIGATES_TO route:home; edges=%v", dumpEdges(ents))
	}
	// Negative: HomeScreen must NOT own DetailScreen's edge.
	if hasEdge(ents, "HomeScreen", "NAVIGATES_TO", "route:home") {
		t.Error("HomeScreen should not own DetailScreen's NAVIGATES_TO route:home edge")
	}
}

// TestComposeNavigatesToRouteConstPartial asserts that sealed-class route
// indirection (Screen.Detail.route) still emits a NAVIGATES_TO edge, marked
// unresolved (honest-partial for cross-file constant resolution).
func TestComposeNavigatesToRouteConstPartial(t *testing.T) {
	src := `
@Composable
fun HomeScreen(navController: NavController) {
    Button(onClick = { navController.navigate(Screen.Detail.route) }) {
        Text("Open")
    }
}
`
	ents := extractRels(t, "custom_kotlin_compose", fi("NavConst.kt", "kotlin", src))
	if !hasEdge(ents, "HomeScreen", "NAVIGATES_TO", "route:Screen.Detail.route") {
		t.Fatalf("expected HomeScreen NAVIGATES_TO route:Screen.Detail.route; edges=%v", dumpEdges(ents))
	}
	// Assert it carries the unresolved marker.
	for _, e := range ents {
		if e.Name != "HomeScreen" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "NAVIGATES_TO" && r.ToID == "route:Screen.Detail.route" {
				if r.Properties["unresolved"] != "true" {
					t.Errorf("expected unresolved=true on route-const edge, got %q", r.Properties["unresolved"])
				}
			}
		}
	}
}

// TestComposeUsesViewModelEdge asserts the composable->ViewModel USES edge for
// each injection form (viewModel(), hiltViewModel(), koinViewModel()).
func TestComposeUsesViewModelEdge(t *testing.T) {
	src := `
@Composable
fun HomeScreen() {
    val vm: HomeViewModel = viewModel()
    Text("Home")
}

@Composable
fun ProfileScreen() {
    val pvm: ProfileViewModel = hiltViewModel()
    Text("Profile")
}

@Composable
fun SettingsScreen() {
    val svm = koinViewModel<SettingsViewModel>()
    Text("Settings")
}
`
	ents := extractRels(t, "custom_kotlin_compose", fi("Screens.kt", "kotlin", src))

	if !hasEdge(ents, "HomeScreen", "USES", "HomeViewModel") {
		t.Errorf("expected HomeScreen USES HomeViewModel; edges=%v", dumpEdges(ents))
	}
	if !hasEdge(ents, "ProfileScreen", "USES", "ProfileViewModel") {
		t.Errorf("expected ProfileScreen USES ProfileViewModel; edges=%v", dumpEdges(ents))
	}
	if !hasEdge(ents, "SettingsScreen", "USES", "SettingsViewModel") {
		t.Errorf("expected SettingsScreen USES SettingsViewModel; edges=%v", dumpEdges(ents))
	}
	// Negative: HomeScreen must not USE ProfileViewModel.
	if hasEdge(ents, "HomeScreen", "USES", "ProfileViewModel") {
		t.Error("HomeScreen should not USE ProfileViewModel (cross-screen leak)")
	}
}

// TestComposeEdgesFromIDEmpty confirms emitted edges leave FromID empty so the
// resolver substitutes the host (enclosing composable) entity ID at assembly.
func TestComposeEdgesFromIDEmpty(t *testing.T) {
	src := `
@Composable
fun HomeScreen(navController: NavController) {
    val vm: HomeViewModel = viewModel()
    navController.navigate("detail/1")
}
`
	ents := extractRels(t, "custom_kotlin_compose", fi("Home.kt", "kotlin", src))
	count := 0
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "NAVIGATES_TO" || r.Kind == "USES" {
				count++
				if r.FromID != "" {
					t.Errorf("expected empty FromID on %s edge, got %q", r.Kind, r.FromID)
				}
			}
		}
	}
	if count == 0 {
		t.Fatal("expected at least one NAVIGATES_TO/USES edge")
	}
}

func dumpEdges(ents []types.EntityRecord) string {
	out := ""
	for _, e := range ents {
		for _, r := range e.Relationships {
			out += "\n  " + e.Name + " -" + r.Kind + "-> " + r.ToID
		}
	}
	if out == "" {
		return "(none)"
	}
	return out
}
