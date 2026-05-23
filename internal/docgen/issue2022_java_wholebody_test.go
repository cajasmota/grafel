package docgen_test

// Issue #2022 — Wave-10 retest of #1995 surfaced that the
// SourceWindowStrategyWholeBody override for Java Class entities does not
// reliably fire. The default ±20-line window covered small classes by
// accident (AuthController, 52 lines) but clipped a 102-line controller
// (TransfersController) so 7 of 10 methods were invisible to the LLM.
//
// Two regression checks cover the contract:
//
//  1. Resolver level — ResolveSectionProfile("SCOPE.Component", "java")
//     MUST return SourceWindowStrategyWholeBody. Locks in the kind/language
//     match the resolver uses; protects against accidentally renaming the
//     profile key or changing the Component substring match.
//
//  2. End-to-end BuildBundle — given a fixture Java Class spanning 80+
//     lines, the emitted graph_context.source_window MUST cover the full
//     class body (start_line to end_line) and NOT the ±20-line default.
//     Includes the legacy-graph case (entity.Language=""): #2022 root
//     cause was older graph.json snapshots carrying empty Language; the
//     fix infers the language from the source file extension so the
//     WholeBody override still fires.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/docgen"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// TestIssue2022_ResolveSectionProfile_JavaComponent_WholeBody locks the
// resolver invariant: ("SCOPE.Component", "java") -> WholeBody. Without
// this the BuildBundle source_window silently falls back to ±20 lines
// for every Java class and most controller methods disappear from the
// LLM prompt.
func TestIssue2022_ResolveSectionProfile_JavaComponent_WholeBody(t *testing.T) {
	cases := []struct {
		name string
		kind string
		lang string
		want string // expected SourceWindowStrategy
	}{
		// The canonical kind the Java extractor emits.
		{"java/SCOPE.Component", "SCOPE.Component", "java", docgen.SourceWindowStrategyWholeBody},
		// Substring + lowercase variants — the resolver lowercases kind
		// and uses Contains(k, "component"); these MUST still resolve.
		{"java/Component", "Component", "java", docgen.SourceWindowStrategyWholeBody},
		{"java/scope.component", "scope.component", "java", docgen.SourceWindowStrategyWholeBody},
		// Negative control: other languages must NOT pick up WholeBody
		// — only Java overrides Component today.
		{"go/SCOPE.Component", "SCOPE.Component", "go", docgen.SourceWindowStrategyDefault},
		{"python/SCOPE.Component", "SCOPE.Component", "python", docgen.SourceWindowStrategyDefault},
		// Negative control: empty language goes to the default branch
		// (the legacy-graph fallback applies in BuildBundle, not here).
		{"blank-lang/SCOPE.Component", "SCOPE.Component", "", docgen.SourceWindowStrategyDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := docgen.ResolveSectionProfile(tc.kind, tc.lang)
			if got.SourceWindowStrategy != tc.want {
				t.Fatalf("ResolveSectionProfile(%q, %q).SourceWindowStrategy = %q; want %q",
					tc.kind, tc.lang, got.SourceWindowStrategy, tc.want)
			}
		})
	}
}

// TestIssue2022_BuildBundle_JavaClass_WholeBodyEmitted is the end-to-end
// regression check. It writes a fake group with one Java file containing
// a controller class spanning 80+ lines, runs BuildBundle, and asserts
// that the source_window:
//   - starts at the class start_line (not start-20)
//   - extends to the class end_line (not start+20)
//   - is long enough to be the whole class, not a ±20-line slice
//
// Two sub-cases:
//   - With entity.Language = "java": the explicit-language path.
//   - With entity.Language = "":     the legacy-graph fallback path
//     (inferLanguageFromSourceFile picks .java from SourceFile).
func TestIssue2022_BuildBundle_JavaClass_WholeBodyEmitted(t *testing.T) {
	for _, lang := range []string{"java", ""} {
		name := "language=" + lang
		if lang == "" {
			name = "language=blank-legacy-graph"
		}
		t.Run(name, func(t *testing.T) {
			groupName, entityID, startLine, endLine := java2022Harness(t, lang)

			bundle, err := docgen.BuildBundle(context.Background(), docgen.BuildBundleOpts{
				RunOpts: docgen.RunOpts{
					Group:        groupName,
					SeedEntityID: entityID,
					Section:      "overview",
					NoCache:      true,
				},
				Tier:    0,
				NoCache: true,
			})
			if err != nil {
				t.Fatalf("BuildBundle: %v", err)
			}

			sw := bundle.GraphContext.SourceWindow
			if sw == "" {
				t.Fatalf("source_window is empty (lang=%q)", lang)
			}

			// The whole-body strategy emits start_line..end_line inclusive.
			// Count newlines to estimate window line span — must be >=
			// (endLine-startLine) - 1, i.e. close to the full class.
			gotLines := strings.Count(sw, "\n")
			fullClassLines := endLine - startLine + 1
			defaultWindowLines := 2*20 + 1 // ±20 around start_line

			// Must cover the whole class body, not the default ±20 window.
			if gotLines < fullClassLines-2 {
				t.Fatalf("source_window only %d lines (lang=%q) — expected ≥ %d (whole class body, start=%d end=%d). Got window:\n%s",
					gotLines, lang, fullClassLines-2, startLine, endLine, sw)
			}

			// Defensive: the window MUST be larger than the default ±20
			// window for a class this size — otherwise we silently fell
			// back to the default branch.
			if gotLines <= defaultWindowLines {
				t.Fatalf("source_window only %d lines (lang=%q) — equal to or smaller than the ±20 default (%d), meaning WholeBody did NOT fire",
					gotLines, lang, defaultWindowLines)
			}

			// Body markers from across the whole class must all appear in
			// the window — proves no clipping at the tail.
			for _, marker := range []string{"FIRST_METHOD_MARKER", "MIDDLE_METHOD_MARKER", "LAST_METHOD_MARKER"} {
				if !strings.Contains(sw, marker) {
					t.Errorf("source_window missing %q (lang=%q) — window appears clipped:\n%s",
						marker, lang, sw)
				}
			}

			// The discriminator for "WholeBody actually fired vs. default
			// ±20-line window" is the head of the window: WholeBody starts
			// at startLine (the `public class` declaration), the default
			// starts at startLine-20 (which on this fixture lands inside
			// the import block above the class). If the source_window
			// contains the "import jakarta.inject.Inject" line, the
			// default branch was taken and #1995 silently did not fire —
			// the symptom #2022 reports.
			if strings.Contains(sw, "import jakarta.inject.Inject") {
				t.Errorf("source_window contains the pre-class import line (lang=%q) — WholeBody did NOT fire; default ±20 window was used instead. Window:\n%s",
					lang, sw)
			}
		})
	}
}

// java2022Harness writes an isolated group with one Java file containing
// a long controller class (>50 lines, with method markers spread
// throughout), and registers a Java SCOPE.Component entity in graph.json.
// Returns groupName, entityID, startLine, endLine for the seed class.
func java2022Harness(t *testing.T, language string) (groupName, entityID string, startLine, endLine int) {
	t.Helper()

	tmp := t.TempDir()
	homeDir := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	daemonRoot := filepath.Join(tmp, "daemon")
	repoPath := filepath.Join(tmp, "repo")
	for _, d := range []string{homeDir, xdgDir, daemonRoot, repoPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	t.Setenv("ARCHIGRAPH_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName = "issue2022-java-wholebody-" + language
	if language == "" {
		groupName = "issue2022-java-wholebody-blank"
	}

	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir fleet config dir: %v", err)
	}
	fleetJSON, _ := json.Marshal(map[string]interface{}{
		"name": groupName,
		"repos": []map[string]interface{}{
			{"path": repoPath, "slug": "client-fixture-x"},
		},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	// Build a Java controller class that mimics the W10R2 fixture shape:
	// class-level annotations, two @Inject fields, ten short methods. The
	// fixture spans well over the ±20-line default so a clipped window
	// would be visibly shorter than the whole-class window.
	src := buildLongJavaController()
	srcRelPath := "src/main/java/client_fixture_x/api/TransfersController.java"
	srcAbsPath := filepath.Join(repoPath, srcRelPath)
	if err := os.MkdirAll(filepath.Dir(srcAbsPath), 0o755); err != nil {
		t.Fatalf("mkdir src dir: %v", err)
	}
	if err := os.WriteFile(srcAbsPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	// Find class start/end lines by scanning markers in the file.
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if strings.Contains(line, "CLASS_START_MARKER") {
			startLine = i + 1
		}
		if strings.Contains(line, "CLASS_END_MARKER") {
			endLine = i + 1
		}
	}
	if startLine == 0 || endLine == 0 || endLine <= startLine+30 {
		t.Fatalf("fixture self-check: start=%d end=%d (need end > start+30)", startLine, endLine)
	}

	// Write the graph.json registering this class as a SCOPE.Component.
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	entityID = "fffffffffff22222"
	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Stats:          graph.Stats{Files: 1, Entities: 1, Relationships: 0},
		Entities: []graph.Entity{
			{
				ID:            entityID,
				Name:          "TransfersController",
				QualifiedName: "client_fixture_x.api.TransfersController",
				Kind:          "SCOPE.Component",
				Subtype:       "class",
				SourceFile:    srcRelPath,
				StartLine:     startLine,
				EndLine:       endLine,
				Language:      language,
			},
		},
	}
	docJSON, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	return
}

// buildLongJavaController returns a Java source file whose
// TransfersController class spans 80+ lines. Methods carry distinct
// FIRST/MIDDLE/LAST markers so the test can assert the source_window
// covers the entire body and not just the first ±20 lines.
//
// The class shape mirrors the W10R2 fixture used in the original Wave-10
// retest report: stacked class-level annotations, two @Inject fields,
// ten short HTTP handler methods. All identifiers use the
// client_fixture_x neutral package name (project rule: never leak real
// client names in fixtures).
func buildLongJavaController() string {
	var b strings.Builder
	b.WriteString("package client_fixture_x.api;\n\n")
	b.WriteString("import jakarta.inject.Inject;\n\n")
	b.WriteString("@Secured\n")
	b.WriteString("@RequestScoped\n")
	b.WriteString("@Path(\"/api/transfers\")\n")
	b.WriteString("@Tag(name = \"transfers\")\n")
	b.WriteString("public class TransfersController { // CLASS_START_MARKER\n")
	b.WriteString("\n")
	b.WriteString("    @Inject UsersService usersService;\n")
	b.WriteString("    @Inject AuditLog auditLog;\n")
	b.WriteString("\n")
	// 10 methods, with markers at first/middle/last.
	for i := 1; i <= 10; i++ {
		var marker string
		switch i {
		case 1:
			marker = " // FIRST_METHOD_MARKER"
		case 5:
			marker = " // MIDDLE_METHOD_MARKER"
		case 10:
			marker = " // LAST_METHOD_MARKER"
		}
		fmt.Fprintf(&b, "    public Response method%d() {%s\n", i, marker)
		b.WriteString("        return Response.ok().build();\n")
		b.WriteString("    }\n")
		b.WriteString("\n")
	}
	b.WriteString("} // CLASS_END_MARKER\n")
	return b.String()
}
