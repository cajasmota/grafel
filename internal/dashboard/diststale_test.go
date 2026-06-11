package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDist materializes a fake SPA bundle (index.html + a hashed asset) under
// dir so the staleness guard has real files to fingerprint.
func writeDist(t *testing.T, dir string, indexBody, assetName, assetBody string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(indexBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", assetName), []byte(assetBody), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDistDirsMatch_DetectsStaleEmbed is the core #4468 regression: an embedded
// bundle that lags the freshly built one (the `cp` step was skipped) must be
// reported as a NON-match.
func TestDistDirsMatch_DetectsStaleEmbed(t *testing.T) {
	built := t.TempDir()
	embedded := t.TempDir()

	// Fresh build references a new hashed asset (index-BWxccaeO.js).
	writeDist(t, built,
		`<script src="/assets/index-BWxccaeO.js"></script>`,
		"index-BWxccaeO.js", "console.log('new ui');")
	// Embedded bundle still references the OLD asset (index-BDdvveMN.js) — the
	// exact staleness observed in the live deploy.
	writeDist(t, embedded,
		`<script src="/assets/index-BDdvveMN.js"></script>`,
		"index-BDdvveMN.js", "console.log('old ui');")

	res, err := DistDirsMatch(built, embedded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Match {
		t.Fatalf("expected stale embed to be flagged as NON-match, got Match=true (reason=%q)", res.Reason)
	}
	if res.EmbeddedEmpty || res.BuiltEmpty {
		t.Fatalf("both dirs are populated; EmbeddedEmpty=%v BuiltEmpty=%v", res.EmbeddedEmpty, res.BuiltEmpty)
	}
	if !strings.Contains(res.Reason, "STALE") {
		t.Errorf("reason should explain staleness, got %q", res.Reason)
	}
}

// TestDistDirsMatch_FreshEmbedMatches verifies the happy path: an embedded
// bundle that is byte-identical to the built one passes.
func TestDistDirsMatch_FreshEmbedMatches(t *testing.T) {
	built := t.TempDir()
	embedded := t.TempDir()

	writeDist(t, built, `<script src="/assets/index-ABC.js"></script>`, "index-ABC.js", "same();")
	writeDist(t, embedded, `<script src="/assets/index-ABC.js"></script>`, "index-ABC.js", "same();")

	res, err := DistDirsMatch(built, embedded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Match {
		t.Fatalf("identical bundles should match, got NON-match (reason=%q)", res.Reason)
	}
	if res.BuiltFingerprint != res.EmbeddedFingerprint {
		t.Errorf("fingerprints should be equal: built=%s embedded=%s", res.BuiltFingerprint, res.EmbeddedFingerprint)
	}
}

// TestDistDirsMatch_PlaceholderOnlyEmbedIsStale verifies that an embedded dir
// holding ONLY the checked-in PLACEHOLDER.md (never built / never copied) is
// reported stale when a fresh build exists — the placeholder is not counted as
// real content.
func TestDistDirsMatch_PlaceholderOnlyEmbedIsStale(t *testing.T) {
	built := t.TempDir()
	embedded := t.TempDir()

	writeDist(t, built, `<script src="/assets/index-XYZ.js"></script>`, "index-XYZ.js", "real();")
	if err := os.WriteFile(filepath.Join(embedded, "PLACEHOLDER.md"), []byte("# placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := DistDirsMatch(built, embedded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Match {
		t.Fatalf("placeholder-only embed with a fresh build present should be NON-match (reason=%q)", res.Reason)
	}
	if !res.EmbeddedEmpty {
		t.Errorf("placeholder-only embed should report EmbeddedEmpty=true")
	}
}

// TestDistDirsMatch_NoBuiltDirSkips verifies that without a fresh build
// (webui-v2/dist absent) the guard does not false-fail — a Go-only or
// pre-built-CI flow legitimately has nothing to compare.
func TestDistDirsMatch_NoBuiltDirSkips(t *testing.T) {
	embedded := t.TempDir()
	writeDist(t, embedded, `<script src="/assets/index-ABC.js"></script>`, "index-ABC.js", "x();")

	res, err := DistDirsMatch(filepath.Join(t.TempDir(), "does-not-exist"), embedded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Match {
		t.Fatalf("missing built dir should skip (Match=true), got NON-match (reason=%q)", res.Reason)
	}
	if !res.BuiltEmpty {
		t.Errorf("expected BuiltEmpty=true when built dir is absent")
	}
}

// TestVerifyDashboardEmbed_StaleReturnsError exercises the make-target entry
// point against a stale repo layout and asserts it errors.
func TestVerifyDashboardEmbed_StaleReturnsError(t *testing.T) {
	root := t.TempDir()
	built := filepath.Join(root, "webui-v2", "dist")
	embedded := filepath.Join(root, "internal", "dashboard", "dist")

	writeDist(t, built, `<script src="/assets/new.js"></script>`, "new.js", "new();")
	writeDist(t, embedded, `<script src="/assets/old.js"></script>`, "old.js", "old();")

	if err := VerifyDashboardEmbed(root); err == nil {
		t.Fatal("expected VerifyDashboardEmbed to return an error for a stale embed")
	}
}

// TestVerifyDashboardEmbed_FreshPasses asserts the entry point passes when the
// embed is current.
func TestVerifyDashboardEmbed_FreshPasses(t *testing.T) {
	root := t.TempDir()
	built := filepath.Join(root, "webui-v2", "dist")
	embedded := filepath.Join(root, "internal", "dashboard", "dist")

	writeDist(t, built, `<script src="/assets/same.js"></script>`, "same.js", "s();")
	writeDist(t, embedded, `<script src="/assets/same.js"></script>`, "same.js", "s();")

	if err := VerifyDashboardEmbed(root); err != nil {
		t.Fatalf("expected pass for a current embed, got %v", err)
	}
}
