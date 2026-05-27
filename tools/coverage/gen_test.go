package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// walkGenerated returns a sorted map of repo-relative-path → sha256 hex
// for every file under <root>/docs/coverage. Used by the determinism test
// to compare two independent gen runs.
func walkGenerated(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	base := filepath.Join(root, docsDir)
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", base, err)
	}
	return out
}

func TestGenDeterministic(t *testing.T) {
	reg, err := loadRegistry(fixturePath(t))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	a, b := t.TempDir(), t.TempDir()
	if err := generate(reg, a); err != nil {
		t.Fatalf("generate a: %v", err)
	}
	if err := generate(reg, b); err != nil {
		t.Fatalf("generate b: %v", err)
	}
	ha := walkGenerated(t, a)
	hb := walkGenerated(t, b)
	if len(ha) != len(hb) {
		t.Fatalf("different file counts: a=%d b=%d", len(ha), len(hb))
	}
	keysA := make([]string, 0, len(ha))
	for k := range ha {
		keysA = append(keysA, k)
	}
	sort.Strings(keysA)
	for _, k := range keysA {
		if ha[k] != hb[k] {
			t.Errorf("non-deterministic content for %s: a=%s b=%s", k, ha[k], hb[k])
		}
	}
}

func TestGenSmokeFixturePaths(t *testing.T) {
	reg, err := loadRegistry(fixturePath(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	root := t.TempDir()
	if err := generate(reg, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	want := []string{
		"docs/coverage/summary.md",
		"docs/coverage/by-language/python.md",
		"docs/coverage/by-language/jsts.md",
		"docs/coverage/by-category/http_framework.md",
		"docs/coverage/detail/lang.python.framework.flask.md",
		"docs/coverage/detail/lang.jsts.framework.express.md",
	}
	for _, rel := range want {
		full := filepath.Join(root, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("expected file %s: %v", rel, err)
			continue
		}
		if !bytes.HasPrefix(data, []byte("<!-- DO NOT EDIT")) {
			t.Errorf("%s missing DO-NOT-EDIT marker", rel)
		}
	}
}

func TestGenMarkerOnEveryFile(t *testing.T) {
	reg, err := loadRegistry(fixturePath(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	root := t.TempDir()
	if err := generate(reg, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	err = filepath.Walk(filepath.Join(root, docsDir), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(data, []byte("<!-- DO NOT EDIT")) {
			t.Errorf("%s missing DO-NOT-EDIT marker", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestGenGoldenFixture renders the fixture and compares the produced
// summary.md byte-for-byte against testdata/fixture-out/summary.md.
// This locks template rendering against regression.
func TestGenGoldenFixture(t *testing.T) {
	reg, err := loadRegistry(fixturePath(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	root := t.TempDir()
	if err := generate(reg, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	goldenRoot := filepath.Join(wd, "testdata", "fixture-out")
	// Walk the golden tree; every file there must match its counterpart
	// under the generated tree. Extra generated files are tolerated only
	// if explicitly missing from golden (forces an opt-in to expand).
	err = filepath.Walk(goldenRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(goldenRoot, path)
		if err != nil {
			return err
		}
		genPath := filepath.Join(root, "docs", "coverage", rel)
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(genPath)
		if err != nil {
			t.Errorf("missing generated file for golden %s: %v", rel, err)
			return nil
		}
		if !bytes.Equal(want, got) {
			t.Errorf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s", rel, want, got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk golden: %v", err)
	}
}

// TestGenSummaryIncludesExtractorSupportedLanguages confirms that when a
// synthetic internal/extractors/ tree contains languages with no records
// in the loaded registry, those languages still appear in the summary
// pivot (zero counts) and a footnote enumerates them. Also asserts that
// placeholder by-language pages are emitted for each untracked language.
func TestGenSummaryIncludesExtractorSupportedLanguages(t *testing.T) {
	reg, err := loadRegistry(fixturePath(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	root := t.TempDir()
	// Synthetic extractor tree: zig (untracked) + python (tracked by fixture).
	for _, d := range []string{"zig", "python", "complexity", "yaml"} {
		if err := os.MkdirAll(filepath.Join(root, "internal", "extractors", d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := generate(reg, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	summary, err := os.ReadFile(filepath.Join(root, "docs", "coverage", "summary.md"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !bytes.Contains(summary, []byte("by-language/zig.md")) {
		t.Errorf("expected summary to link zig.md, got:\n%s", summary)
	}
	if !bytes.Contains(summary, []byte("## Languages with extractor support, no records yet")) {
		t.Errorf("expected placeholder section, got:\n%s", summary)
	}
	if !bytes.Contains(summary, []byte("| [Zig](by-language/zig.md) |")) {
		t.Errorf("expected Zig row in placeholder table, got:\n%s", summary)
	}
	// Placeholder page must exist and cite the extractor dir.
	pl, err := os.ReadFile(filepath.Join(root, "docs", "coverage", "by-language", "zig.md"))
	if err != nil {
		t.Fatalf("read placeholder: %v", err)
	}
	if !bytes.Contains(pl, []byte("No ecosystem records tracked yet")) {
		t.Errorf("placeholder missing notice")
	}
	if !bytes.Contains(pl, []byte("internal/extractors/zig/")) {
		t.Errorf("placeholder missing extractor dir cite")
	}
	// Utility + non-language dirs must NOT produce placeholder pages.
	if _, err := os.Stat(filepath.Join(root, "docs", "coverage", "by-language", "complexity.md")); !os.IsNotExist(err) {
		t.Errorf("complexity.md should not exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "coverage", "by-language", "yaml.md")); !os.IsNotExist(err) {
		t.Errorf("yaml.md should not exist: %v", err)
	}
}

func TestGenSubcommandWiring(t *testing.T) {
	reg, err := loadRegistry(fixturePath(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	root := t.TempDir()
	// Copy the fixture so we can point --file at a writable path that lives
	// next to the --out target. (gen does not write the registry; the copy
	// is just to test the CLI dispatcher with real flag plumbing.)
	regPath := filepath.Join(root, "coverage.json")
	if err := saveRegistry(regPath, reg); err != nil {
		t.Fatalf("save copy: %v", err)
	}
	out, _, err := runCmd(t, "gen", "--file", regPath, "--out", root)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	if !strings.Contains(out, "generated") {
		t.Errorf("expected confirmation message, got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "coverage", "summary.md")); err != nil {
		t.Errorf("summary.md not created: %v", err)
	}
}
