package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/registry"
)

// fakeStore is an in-memory RegistryStore. It removes the dependency on
// ~/.archigraph for tests so they stay hermetic and parallel-safe.
type fakeStore struct {
	mu        sync.Mutex
	groups    map[string]GroupSummary
	groupG    map[string][]byte
	repoG     map[string]map[string][]byte
	addRepoEr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		groups: map[string]GroupSummary{},
		groupG: map[string][]byte{},
		repoG:  map[string]map[string][]byte{},
	}
}

func (f *fakeStore) ListGroups() ([]GroupSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]GroupSummary, 0, len(f.groups))
	for _, v := range f.groups {
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeStore) GroupGraph(group string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.groupG[group]
	if !ok {
		return nil, fmt.Errorf("group %q not found", group)
	}
	return b, nil
}

func (f *fakeStore) RepoGraph(group, repo string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.repoG[group]
	if !ok {
		return nil, fmt.Errorf("group %q not found", group)
	}
	b, ok := g[repo]
	if !ok {
		return nil, fmt.Errorf("repo %q not found", repo)
	}
	return b, nil
}

func (f *fakeStore) CreateGroup(name string) (GroupSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.groups[name]; ok {
		return GroupSummary{}, fmt.Errorf("exists")
	}
	gs := GroupSummary{Name: name, ConfigPath: "/tmp/" + name + ".json"}
	f.groups[name] = gs
	return gs, nil
}

func (f *fakeStore) AddRepo(group string, repo registry.Repo) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addRepoEr != nil {
		return f.addRepoEr
	}
	gs, ok := f.groups[group]
	if !ok {
		return fmt.Errorf("group %q not found", group)
	}
	gs.Repos = append(gs.Repos, repo.Slug)
	f.groups[group] = gs
	return nil
}

// newTestServer wires a Server against a httptest harness. Returns the
// HTTP base URL and a cleanup func.
func newTestServer(t *testing.T, store RegistryStore, cfg Config) (string, func()) {
	t.Helper()
	s, err := NewServer(cfg, store)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s.routes())
	return ts.URL, ts.Close
}

func TestPortDiscovery_PicksFreePort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Bind = "127.0.0.1"
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	port, err := srv.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.listener.Close()
	if port < cfg.PortRange.Min || port > cfg.PortRange.Max {
		t.Errorf("port %d outside configured range %d-%d", port, cfg.PortRange.Min, cfg.PortRange.Max)
	}
	if got := srv.Addr(); !strings.HasSuffix(got, fmt.Sprintf(":%d", port)) {
		t.Errorf("Addr()=%q; want suffix :%d", got, port)
	}
}

func TestPortDiscovery_SkipsOccupied(t *testing.T) {
	// Bind a port in a tiny range so the server has to skip it.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("seed listener: %v", err)
	}
	defer occupied.Close()
	occPort := occupied.Addr().(*net.TCPAddr).Port

	// Range covering only [occPort, occPort+1] so the server must pick the
	// non-conflicting neighbour. If that neighbour is also taken on the
	// host we widen by one and try again — flaky-host hedge.
	for span := 1; span < 16; span++ {
		cfg := Config{
			PortRange: PortRange{Min: occPort, Max: occPort + span},
			Bind:      "127.0.0.1",
		}
		s, err := NewServer(cfg, newFakeStore())
		if err != nil {
			t.Fatalf("NewServer: %v", err)
		}
		port, err := s.Listen()
		if err == nil {
			defer s.listener.Close()
			if port == occPort {
				t.Fatalf("server picked occupied port %d", port)
			}
			return
		}
	}
	t.Fatalf("could not find free port near %d", occPort)
}

func TestRegistryEndpoint(t *testing.T) {
	st := newFakeStore()
	st.groups["alpha"] = GroupSummary{Name: "alpha", ConfigPath: "/x/alpha.json", Repos: []string{"r1"}}

	base, cleanup := newTestServer(t, st, DefaultConfig())
	defer cleanup()

	resp, err := http.Get(base + "/api/registry")
	if err != nil {
		t.Fatalf("GET /api/registry: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var body struct {
		Groups []GroupSummary `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Groups) != 1 || body.Groups[0].Name != "alpha" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestRepoGraphEndpoint(t *testing.T) {
	st := newFakeStore()
	st.repoG["alpha"] = map[string][]byte{
		"svc": []byte(`{"entities":[{"id":"e1"}]}`),
	}
	base, cleanup := newTestServer(t, st, DefaultConfig())
	defer cleanup()

	resp, err := http.Get(base + "/api/groups/alpha/repos/svc/graph")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"e1"`) {
		t.Fatalf("body missing entity id: %s", b)
	}

	// Unknown repo -> 404.
	resp2, err := http.Get(base + "/api/groups/alpha/repos/missing/graph")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 404 {
		t.Fatalf("status=%d, want 404", resp2.StatusCode)
	}
}

func TestGroupGraphEndpoint(t *testing.T) {
	st := newFakeStore()
	st.groupG["alpha"] = []byte(`{"group":"alpha","repos":[]}`)
	base, cleanup := newTestServer(t, st, DefaultConfig())
	defer cleanup()

	resp, err := http.Get(base + "/api/groups/alpha/graph")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestCreateGroupEndpoint(t *testing.T) {
	st := newFakeStore()
	base, cleanup := newTestServer(t, st, DefaultConfig())
	defer cleanup()

	resp, err := http.Post(base+"/api/admin/groups", "application/json",
		bytes.NewBufferString(`{"name":"beta"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	if _, ok := st.groups["beta"]; !ok {
		t.Fatalf("group not created in store")
	}
}

func TestAddRepoEndpoint(t *testing.T) {
	st := newFakeStore()
	st.groups["alpha"] = GroupSummary{Name: "alpha"}
	base, cleanup := newTestServer(t, st, DefaultConfig())
	defer cleanup()

	body := `{"slug":"svc","path":"/repo/svc","stack":"go"}`
	resp, err := http.Post(base+"/api/admin/groups/alpha/repos",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	if got := st.groups["alpha"].Repos; len(got) != 1 || got[0] != "svc" {
		t.Fatalf("repos=%v", got)
	}
}

func TestAuthGate(t *testing.T) {
	st := newFakeStore()
	cfg := DefaultConfig()
	cfg.Auth = AuthConfig{Enabled: true, Token: "secret"}
	base, cleanup := newTestServer(t, st, cfg)
	defer cleanup()

	// No auth header -> 401.
	resp, err := http.Get(base + "/api/registry")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}

	// Correct token -> 200.
	req, _ := http.NewRequest("GET", base+"/api/registry", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp2.StatusCode)
	}

	// Static asset is open even with auth on (so SPA shell can load).
	resp3, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != 200 {
		t.Fatalf("static status=%d, want 200", resp3.StatusCode)
	}
}

func TestServeShutsDownOnContextCancel(t *testing.T) {
	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if _, err := srv.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Serve did not return after cancel")
	}
}
