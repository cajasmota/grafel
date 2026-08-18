// no_glob_skip_6330_test.go — #6330: the classifier must have no file-glob skip
// mechanism and no YAML configuration surface.
//
// History. The classifier used to carry a `globSkips []globSkip` list loaded
// from 31 `internal/engine/rules/*/skip_patterns.yaml` files by a
// `yamlDataDir` constructor parameter. All four production call sites passed
// "", so the loader never ran once and the config was never validated: 6 of the
// 31 files did not unmarshal at all, 63% of the parsed patterns could never
// match (filepath.Match has no `**` and does not cross separators), and of the
// patterns that WOULD have matched, enabling them would have silently dropped
// every `*_test.go` file, `install.sh`, `AssemblyInfo.cs` and `sqlite3.c` from
// the graph. It was a loaded gun in a directory that read like settled config.
//
// The whole path was deleted. Generated-source handling is being rebuilt in
// #6329 with a real consumer and real tests; see
// docs/generated-source-patterns.md for the salvaged, explicitly unvalidated
// input.
//
// This file exists to stop the dead path coming back. Three assertions, each
// binding a different half of the property:
//
//  1. TestNew_HasNoConfigurationParameter — compile-time: the production
//     constructor takes a tracer and nothing else. Re-adding a directory
//     parameter breaks the build here.
//  2. TestClassifierSourceHasNoGlobSkipMachinery — source-level: no
//     glob-matching or YAML-decoding machinery exists in the package at all,
//     reachable or not. This is the assertion that catches a REINTRODUCED BUT
//     UNWIRED loader, which no behavioural test can see.
//  3. TestGeneratedAndTestFileNames_AreNotSkipped — behavioural: the exact
//     filenames the old config would have eaten still classify normally. This
//     catches a loader that is reintroduced AND wired up.
package classifier_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/cajasmota/grafel/internal/classifier"
)

// TestNew_HasNoConfigurationParameter pins the production constructor's shape.
// The assignment below does not compile if New grows a config parameter (e.g.
// a yamlDataDir) or an error return, which is precisely the change that would
// reopen the door.
func TestNew_HasNoConfigurationParameter(t *testing.T) {
	var newFn func(trace.Tracer) *classifier.Classifier = classifier.New
	if c := newFn(noop.NewTracerProvider().Tracer("test")); c == nil {
		t.Fatal("New returned nil")
	}
	// nil tracer is the production call shape at all four call sites.
	if c := classifier.New(nil); c == nil {
		t.Fatal("New(nil) returned nil")
	}
}

// forbiddenInClassifierSource are tokens whose presence in non-test package
// source means a file-glob skip mechanism or a YAML config loader has come
// back. Each maps to the reason it is banned.
var forbiddenInClassifierSource = map[string]string{
	"filepath.Match": "glob matching against file paths — the deleted skip mechanism's matcher",
	"path.Match":     "glob matching against file paths — the deleted skip mechanism's matcher",
	"yaml.Unmarshal": "YAML decoding — the classifier has no configuration file",
	"skip_patterns":  "the deleted skip_patterns.yaml config",
	"globSkip":       "the deleted glob-skip list",
	"yamlDataDir":    "the deleted config-directory constructor parameter",
}

// TestClassifierSourceHasNoGlobSkipMachinery asserts the package contains no
// glob-skip or YAML-config machinery in its production source.
//
// A behavioural test cannot express this: a reintroduced loader that nothing
// calls changes no observable behaviour, yet is exactly the hazard #6330
// removed — unvalidated config sitting one line away from being switched on.
// So this test reads the source.
func TestClassifierSourceHasNoGlobSkipMachinery(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		src := string(data)
		for tok, why := range forbiddenInClassifierSource {
			if strings.Contains(src, tok) {
				t.Errorf("#6330: %s contains %q (%s).\n"+
					"The classifier must not regain a file-glob skip mechanism or a YAML config surface.\n"+
					"If you are implementing generated-source handling, that is #6329: it needs a real\n"+
					"consumer and a test per pattern class. See docs/generated-source-patterns.md.",
					name, tok, why)
			}
		}
	}
	// Guard against the scan silently covering nothing.
	if scanned == 0 {
		t.Fatal("scanned 0 production .go files — the source scan is vacuous")
	}
}

// TestNoSkipPatternsYAMLInTree asserts the 31 deleted config files have not
// been restored anywhere under the engine rules tree or this package's
// testdata. Re-adding the data is the first half of re-adding the trap.
func TestNoSkipPatternsYAMLInTree(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "internal", "engine", "rules"),
		"testdata",
	}
	checkedAtLeastOne := false
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue // testdata may legitimately not exist
		}
		checkedAtLeastOne = true
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && d.Name() == "skip_patterns.yaml" {
				t.Errorf("#6330: %s was restored. This config never executed and was never "+
					"validated; see docs/generated-source-patterns.md and #6329.", p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if !checkedAtLeastOne {
		t.Fatal("no root was walked — the tree scan is vacuous")
	}
}

// TestGeneratedAndTestFileNames_AreNotSkipped is the behavioural half: every
// filename below was matchable by the deleted config, and several would have
// been dropped from the graph outright. They must all classify normally.
//
// `*_test.go` is the load-bearing case: the old go/skip_patterns.yaml listed it
// with action "scope=test", but the loader collapsed every entry to an
// unconditional skip, so enabling the config would have erased every Go test
// file. Any future generated-source work (#6329) must keep this passing.
func TestGeneratedAndTestFileNames_AreNotSkipped(t *testing.T) {
	c := classifier.New(noop.NewTracerProvider().Tracer("test"))
	cases := []struct {
		path string
		lang string
	}{
		{"internal/classifier/classifier_test.go", "go"}, // would have been dropped
		{"internal/proto/service.pb.go", "go"},
		{"internal/di/wire_gen.go", "go"},
		{"api/zz_generated.deepcopy.go", "go"},
		{"internal/foo/mock_store.go", "go"},
		{"scripts/install.sh", "shell"},
		{"scripts/bootstrap.sh", "shell"},
		{"Properties/AssemblyInfo.cs", "csharp"},
		{"src/GlobalUsings.cs", "csharp"},
		{"lib/model.g.dart", "dart"},
		{"app/BuildConfig.kt", "kotlin"},
		{"src/api.generated.ts", "typescript"},
	}
	for _, tc := range cases {
		r := c.Classify(context.Background(), tc.path)
		if r.Skip {
			t.Errorf("#6330: %q was skipped (reason=%q); the classifier must have no "+
				"file-glob skip mechanism", tc.path, r.SkipReason)
			continue
		}
		if r.Language != tc.lang {
			t.Errorf("%q: Language = %q, want %q", tc.path, r.Language, tc.lang)
		}
	}
}
