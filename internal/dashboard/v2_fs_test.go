package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
)

// fsListEnvelope is the decoded GET /api/v2/fs/list response.
type fsListEnvelope struct {
	OK   bool          `json:"ok"`
	Data v2FsListReply `json:"data"`
}

func getFsList(t *testing.T, baseURL, path string) fsListEnvelope {
	t.Helper()
	u := baseURL + "/api/v2/fs/list"
	if path != "" {
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		q := req.URL.Query()
		q.Set("path", path)
		req.URL.RawQuery = q.Encode()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET fs/list: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; want 200", resp.StatusCode)
		}
		var env fsListEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return env
	}
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET fs/list: %v", err)
	}
	defer resp.Body.Close()
	var env fsListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env
}

// TestV2FsList_ListsSubdirsWithAbsPaths verifies the folder browser returns
// subdirectories (not files) with their ABSOLUTE on-disk paths — the key
// requirement that lets the wizard proceed without a manual paste.
func TestV2FsList_ListsSubdirsWithAbsPaths(t *testing.T) {
	ts, _ := newWizardTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	root := t.TempDir()
	for _, d := range []string{"alpha", "beta", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "afile.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := getFsList(t, ts.URL, root)
	if !env.OK {
		t.Fatalf("ok = false; reply = %+v", env.Data)
	}
	if env.Data.Path != root {
		t.Fatalf("Path = %q; want %q", env.Data.Path, root)
	}
	if env.Data.Parent != filepath.Dir(root) {
		t.Fatalf("Parent = %q; want %q", env.Data.Parent, filepath.Dir(root))
	}

	byName := map[string]v2FsEntry{}
	for _, e := range env.Data.Entries {
		byName[e.Name] = e
	}
	// Directories present; file absent.
	if _, ok := byName["afile.txt"]; ok {
		t.Fatal("file afile.txt should not be listed")
	}
	for _, want := range []string{"alpha", "beta", ".hidden"} {
		e, ok := byName[want]
		if !ok {
			t.Fatalf("missing dir %q", want)
		}
		if e.Path != filepath.Join(root, want) {
			t.Fatalf("%q Path = %q; want absolute %q", want, e.Path, filepath.Join(root, want))
		}
		if !e.IsDir {
			t.Fatalf("%q IsDir = false", want)
		}
		if !filepath.IsAbs(e.Path) {
			t.Fatalf("%q Path %q is not absolute", want, e.Path)
		}
	}
	if !byName[".hidden"].Hidden {
		t.Fatal(".hidden should be flagged Hidden")
	}
	// Sorted ascending case-insensitively: .hidden, alpha, beta.
	if env.Data.Entries[0].Name != ".hidden" || env.Data.Entries[1].Name != "alpha" {
		t.Fatalf("entries not sorted: %+v", env.Data.Entries)
	}
}

// TestV2FsList_DefaultsToHome verifies that an empty path defaults to the
// daemon's home directory.
func TestV2FsList_DefaultsToHome(t *testing.T) {
	ts, _ := newWizardTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	env := getFsList(t, ts.URL, "")
	if !env.OK {
		t.Fatalf("ok = false; reply = %+v", env.Data)
	}
	if env.Data.Path != home {
		t.Fatalf("Path = %q; want home %q", env.Data.Path, home)
	}
}

// TestV2FsList_NonexistentPathGraceful verifies a bad path returns a graceful
// error in the envelope (HTTP 200), not a hard failure.
func TestV2FsList_NonexistentPathGraceful(t *testing.T) {
	ts, _ := newWizardTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	missing := filepath.Join(t.TempDir(), "does-not-exist-xyz")
	env := getFsList(t, ts.URL, missing)
	if !env.OK {
		t.Fatalf("ok should stay true for graceful error; reply = %+v", env.Data)
	}
	if env.Data.Error == "" {
		t.Fatal("expected Error for nonexistent path")
	}
	if len(env.Data.Entries) != 0 {
		t.Fatalf("expected no entries; got %d", len(env.Data.Entries))
	}
}
