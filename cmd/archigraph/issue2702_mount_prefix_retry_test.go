package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/links"
)

// TestIssue2702_MountPrefixRetry asserts that the consumer-side mount-prefix
// retry (#2702) resolves a frontend call whose path omits the mount prefix
// declared via `path("internal/v3/", include(...))` on the producer side.
//
// The issue's worked examples use `/api/v1/`, but that exact prefix is
// already absorbed by the byPath generic-strip alias (#1409) — the producer
// is registered under both /api/v1/things AND /things, so the consumer's
// raw /things would resolve via the standard byPath probe labelled "exact"
// before the new retry ever fires. To prove the new code path actually
// runs, the fixture uses a non-canonical mount prefix (/internal/v3/) that
// is invisible to both the generic-strip pass and the hardcoded
// prefix-injection retry (#2569).
//
// The fixture under testdata/audit_mount_prefix_retry/ ships hand-crafted
// per-repo graph.json files (see the README in that directory for why we
// do not re-extract from `*.py` / `*.js` here). The producer graph contains
// a `url_mount_point` synthetic carrying url_prefix="/api/v1" plus a
// regular http_endpoint for GET /internal/v3/things — but the regular endpoint
// has NO url_prefix property, so the byPath generic-strip alias (#1409) and
// url_prefix-keyed strip (#819) both fail to register a /things alias for
// it. The only resolution path left for the consumer's /things call is the
// new mount_prefix_added strategy.
//
// Expectations:
//  1. A cross-repo HTTP link exists from consumer → producer.
//  2. The link's Properties carry resolve_strategy="mount_prefix_added"
//     and applied_mount_prefix="/internal/v3/".
//  3. The PassResult slice records hits_by_strategy["mount_prefix_added"]≥1.
func TestIssue2702_MountPrefixRetry(t *testing.T) {
	tmpGraphs := t.TempDir()
	tmpHome := t.TempDir()

	// Stage the hand-crafted per-repo graph.json files under tmpGraphs in
	// the layout loadAllGraphs expects: one <slug>/graph.json per repo.
	stageGraph := func(slug, src string) {
		t.Helper()
		dstDir := filepath.Join(tmpGraphs, slug)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dstDir, err)
		}
		copyFile(t, src, filepath.Join(dstDir, "graph.json"))
	}
	stageGraph("producer", "testdata/audit_mount_prefix_retry/producer/graph.json")
	stageGraph("consumer", "testdata/audit_mount_prefix_retry/consumer/graph.json")

	res, err := links.RunAllPasses("audit-mount-prefix", tmpGraphs, tmpHome)
	if err != nil {
		t.Fatalf("RunAllPasses: %v", err)
	}

	doc, err := readLinksDoc(res.OutLinks)
	if err != nil {
		t.Fatalf("read links: %v", err)
	}
	var hit *links.Link
	for i := range doc.Links {
		l := &doc.Links[i]
		if l.Method != links.MethodHTTP {
			continue
		}
		if l.Properties["resolve_strategy"] == "mount_prefix_added" {
			hit = l
			break
		}
	}
	if hit == nil {
		t.Fatalf("no mount_prefix_added link emitted; links=%+v", doc.Links)
	}
	if got := hit.Properties["applied_mount_prefix"]; got != "/internal/v3/" {
		t.Errorf("applied_mount_prefix=%q want /internal/v3/", got)
	}

	var mountHits int
	for _, r := range res.Results {
		mountHits += r.CrossRepoResolveHitsByStrategy["mount_prefix_added"]
	}
	if mountHits < 1 {
		t.Errorf("hits_by_strategy.mount_prefix_added=%d want >=1", mountHits)
	}
}

// readLinksDoc unmarshals a links/candidates/rejections document from disk.
// Kept local so the test does not depend on internal helpers in the links
// package.
func readLinksDoc(path string) (*links.Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc links.Document
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// copyFile copies src to dst, fatal-erroring through t. The contents are
// streamed so the helper works on graph.json files of any size.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy %s -> %s: %v", src, dst, err)
	}
}
