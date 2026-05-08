package links

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestLiveFixture is a smoke test against a hand-built 2-repo fixture.
// Useful as the "live" sign-off check called for in the task brief.
// It runs only when ARCHIGRAPH_LIVE_FIXTURE=1.
func TestLiveFixture(t *testing.T) {
	if os.Getenv("ARCHIGRAPH_LIVE_FIXTURE") != "1" {
		t.Skip("set ARCHIGRAPH_LIVE_FIXTURE=1 to run")
	}
	root := os.Getenv("ARCHIGRAPH_LIVE_ROOT")
	home := os.Getenv("ARCHIGRAPH_LIVE_HOME")
	if root == "" || home == "" {
		t.Skip("requires ARCHIGRAPH_LIVE_ROOT + ARCHIGRAPH_LIVE_HOME")
	}
	tmp, err := os.MkdirTemp("", "ag-stage-")
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"alpha", "beta"} {
		src := filepath.Join(root, repo, ".archigraph", "graph.json")
		dst := filepath.Join(tmp, repo, "graph.json")
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.Symlink(src, dst); err != nil {
			t.Fatal(err)
		}
	}
	res, err := RunAllPasses("livegroup", tmp, home)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(b))
	doc, err := readDoc(res.OutLinks)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.MarshalIndent(doc, "", "  ")
	fmt.Println("--- links ---")
	fmt.Println(string(out))
}
