package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/extractors/cross/deplinker"
)

// ---------------------------------------------------------------------------
// filterPackages unit tests
// ---------------------------------------------------------------------------

func TestFilterPackages_NoFilter(t *testing.T) {
	pkgs := []deplinker.PackageEntry{
		{Name: "express", Status: deplinker.StatusUsed, PackageManager: "npm", DependencyKind: "runtime"},
		{Name: "lodash", Status: deplinker.StatusUnused, PackageManager: "npm", DependencyKind: "runtime"},
	}
	got := filterPackages(pkgs, "", "", "")
	if len(got) != 2 {
		t.Errorf("got %d want 2", len(got))
	}
}

func TestFilterPackages_StatusFilter(t *testing.T) {
	pkgs := []deplinker.PackageEntry{
		{Name: "express", Status: deplinker.StatusUsed},
		{Name: "lodash", Status: deplinker.StatusUnused},
		{Name: "axios", Status: deplinker.StatusPhantom},
	}
	got := filterPackages(pkgs, "unused", "", "")
	if len(got) != 1 {
		t.Fatalf("got %d want 1", len(got))
	}
	if got[0].Name != "lodash" {
		t.Errorf("name=%q want lodash", got[0].Name)
	}
}

func TestFilterPackages_PMFilter(t *testing.T) {
	pkgs := []deplinker.PackageEntry{
		{Name: "gin", PackageManager: "go_modules", Status: deplinker.StatusUsed},
		{Name: "express", PackageManager: "npm", Status: deplinker.StatusUsed},
	}
	got := filterPackages(pkgs, "", "npm", "")
	if len(got) != 1 || got[0].Name != "express" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestFilterPackages_KindFilter(t *testing.T) {
	pkgs := []deplinker.PackageEntry{
		{Name: "jest", DependencyKind: "dev", Status: deplinker.StatusUnused},
		{Name: "express", DependencyKind: "runtime", Status: deplinker.StatusUsed},
	}
	got := filterPackages(pkgs, "", "", "dev")
	if len(got) != 1 || got[0].Name != "jest" {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// statusOrder
// ---------------------------------------------------------------------------

func TestStatusOrder(t *testing.T) {
	if statusOrder(deplinker.StatusPhantom) >= statusOrder(deplinker.StatusUnused) {
		t.Error("phantom should sort before unused")
	}
	if statusOrder(deplinker.StatusUnused) >= statusOrder(deplinker.StatusUsed) {
		t.Error("unused should sort before used")
	}
}

// ---------------------------------------------------------------------------
// HTTP handler integration (empty group → 404)
// ---------------------------------------------------------------------------

func TestHandleDependencies_MissingGroup(t *testing.T) {
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs = NewGraphCache(60 * time.Second)

	mux := srv.routes()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dependencies/no-such-group", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// HTTP handler integration (group with no repos → empty summary)
// ---------------------------------------------------------------------------

func TestHandleDependencies_EmptyGroup(t *testing.T) {
	cfg := DefaultConfig()
	store := newFakeStore()
	store.groups["mygroup"] = GroupSummary{Name: "mygroup"}
	srv, err := NewServer(cfg, store)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Pre-populate the cache with an empty group so we don't hit the filesystem.
	srv.graphs = &GraphCache{
		entries: map[string]*cacheEntry{
			"mygroup": {
				group:    &DashGroup{Name: "mygroup", Repos: map[string]*DashRepo{}},
				loadedAt: time.Now(),
			},
		},
		ttl: 60 * time.Second,
	}

	mux := srv.routes()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dependencies/mygroup", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200, body=%s", rec.Code, rec.Body.String())
	}

	var reply DependenciesReply
	if err := json.NewDecoder(rec.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Group != "mygroup" {
		t.Errorf("group=%q want mygroup", reply.Group)
	}
	if reply.Summary.Declared != 0 {
		t.Errorf("summary.declared=%d want 0", reply.Summary.Declared)
	}
}
