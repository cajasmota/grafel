package docgen_test

// llm_bundle_python_enrichment_test.go — regression tests for the Python
// enrichment cluster (#1862, #1867, #1877, #2012).
//
// Each test builds an isolated fleet group with a single repo whose graph
// is hand-written to mimic the post-extractor shape the Python extractor
// would produce. The bundle assertions exercise the wiring on the docgen
// side without depending on the full extractor pipeline (covered by the
// extractor-side tests in internal/extractors/python).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/docgen"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// writeGroupGraph is a small harness that builds an isolated grafel
// group whose only repo contains the supplied graph document. Returns the
// group name and the chosen seed entity ID.
func writeGroupGraph(t *testing.T, groupName, seedID string, doc graph.Document) string {
	t.Helper()
	tmp := t.TempDir()
	homeDir := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	daemonRoot := filepath.Join(tmp, "daemon")
	repoPath := filepath.Join(tmp, "myrepo")
	for _, d := range []string{homeDir, xdgDir, daemonRoot, repoPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	t.Setenv("GRAFEL_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

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
			{"path": repoPath, "slug": "myrepo"},
		},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	doc.Repo = repoPath
	doc.Version = 1
	if doc.GeneratedAt.IsZero() {
		doc.GeneratedAt = time.Now().UTC()
	}
	if doc.IndexerVersion == "" {
		doc.IndexerVersion = "test"
	}
	doc.Stats = graph.Stats{
		Files:         1,
		Entities:      len(doc.Entities),
		Relationships: len(doc.Relationships),
	}

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal graph doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	_ = seedID
	return groupName
}

// TestNeighbourBrief_1862_ActionDecoratorMetadata asserts that HTTP metadata
// stamped on a method entity by the DRF @action pass (#2004) surfaces on
// the corresponding NeighbourBrief for a class-seed bundle (#1862).
//
// Fixture: a ContractViewSet with one CONTAINS-child method `.approve`
// carrying http_method=post, http_methods="post,put", url_path="approve",
// is_detail="true" on the method entity Properties.
func TestNeighbourBrief_1862_ActionDecoratorMetadata(t *testing.T) {
	seedID := "1111111111111111"
	methodID := "2222222222222222"

	doc := graph.Document{
		Entities: []graph.Entity{
			{
				ID: seedID, Name: "ContractViewSet",
				Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "core/views/contract_viewset.py",
				Language:   "python",
				StartLine:  10, EndLine: 80,
			},
			graph.Entity{
				ID: methodID, Name: "ContractViewSet.approve",
				Kind: "SCOPE.Operation", Subtype: "method",
				SourceFile: "core/views/contract_viewset.py",
				Language:   "python",
				StartLine:  40, EndLine: 55,
			}.WithProperties(map[string]string{
				"drf_action":   "true",
				"http_method":  "post",
				"http_methods": "post,put",
				"url_path":     "approve",
				"is_detail":    "true",
			},
			),
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: seedID, ToID: methodID, Kind: "CONTAINS"},
		},
	}

	group := writeGroupGraph(t, "issue-1862-group", seedID, doc)

	bundle, err := docgen.BuildBundle(context.Background(), docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        group,
			SeedEntityID: seedID,
			Section:      "api",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	var brief *docgen.NeighbourBrief
	for i, b := range bundle.GraphContext.NeighbourBriefs {
		if b.Name == "ContractViewSet.approve" {
			brief = &bundle.GraphContext.NeighbourBriefs[i]
			break
		}
	}
	if brief == nil {
		t.Fatalf("approve neighbour brief missing — got %d briefs", len(bundle.GraphContext.NeighbourBriefs))
	}
	if brief.HTTPMethod != "post" {
		t.Errorf("HTTPMethod = %q, want %q", brief.HTTPMethod, "post")
	}
	if brief.HTTPMethods != "post,put" {
		t.Errorf("HTTPMethods = %q, want %q", brief.HTTPMethods, "post,put")
	}
	if brief.URLPath != "approve" {
		t.Errorf("URLPath = %q, want %q", brief.URLPath, "approve")
	}
	if !brief.IsDetail {
		t.Errorf("IsDetail = false, want true")
	}
}

// TestNeighbourBrief_1877_SchemaTypeHint asserts that SCOPE.Schema/field
// neighbours surface a structured TypeHint built from their entity
// Properties (field_type + kwarg.*), composing the Django Model field
// summary documented on composeDjangoFieldTypeHint (#1877).
func TestNeighbourBrief_1877_SchemaTypeHint(t *testing.T) {
	classID := "1111111111111111"
	fkFieldID := "2222222222222222"
	charFieldID := "3333333333333333"
	boolFieldID := "4444444444444444"

	doc := graph.Document{
		Entities: []graph.Entity{
			{
				ID: classID, Name: "Contract",
				Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "core/models/contract.py", Language: "python",
				StartLine: 10, EndLine: 90,
			},
			graph.Entity{
				ID: fkFieldID, Name: "Contract.client",
				Kind: "SCOPE.Schema", Subtype: "field",
				SourceFile: "core/models/contract.py", Language: "python",
				StartLine: 12, EndLine: 12,
			}.WithProperties(map[string]string{
				"field_type":      "ForeignKey",
				"kwarg.to":        "Client",
				"kwarg.on_delete": "CASCADE",
			},
			),
			graph.Entity{
				ID: charFieldID, Name: "Contract.status",
				Kind: "SCOPE.Schema", Subtype: "field",
				SourceFile: "core/models/contract.py", Language: "python",
				StartLine: 13, EndLine: 13,
			}.WithProperties(map[string]string{
				"field_type":       "CharField",
				"kwarg.max_length": "10",
				"kwarg.choices":    "STATUS_CHOICES",
			},
			),
			graph.Entity{
				ID: boolFieldID, Name: "Contract.is_active",
				Kind: "SCOPE.Schema", Subtype: "field",
				SourceFile: "core/models/contract.py", Language: "python",
				StartLine: 14, EndLine: 14,
			}.WithProperties(map[string]string{
				"field_type":    "BooleanField",
				"kwarg.default": "False",
			},
			),
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: classID, ToID: fkFieldID, Kind: "CONTAINS"},
			{ID: "r2", FromID: classID, ToID: charFieldID, Kind: "CONTAINS"},
			{ID: "r3", FromID: classID, ToID: boolFieldID, Kind: "CONTAINS"},
		},
	}

	group := writeGroupGraph(t, "issue-1877-group", classID, doc)

	bundle, err := docgen.BuildBundle(context.Background(), docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        group,
			SeedEntityID: classID,
			Section:      "reference-config",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	byName := map[string]docgen.NeighbourBrief{}
	for _, b := range bundle.GraphContext.NeighbourBriefs {
		byName[b.Name] = b
	}
	wants := map[string]string{
		"Contract.client":    "ForeignKey(Client) on_delete=CASCADE",
		"Contract.status":    "CharField(max_length=10, choices=STATUS_CHOICES)",
		"Contract.is_active": "BooleanField default=False",
	}
	for name, want := range wants {
		b, ok := byName[name]
		if !ok {
			t.Errorf("neighbour %q missing", name)
			continue
		}
		if b.TypeHint != want {
			t.Errorf("neighbour %q TypeHint = %q, want %q", name, b.TypeHint, want)
		}
	}
}

// TestNeighbourBrief_1867_ClassMethodHopExpansion asserts that for a Class
// seed, the bundle surfaces depth-2 typed dependencies reached through the
// class's contained methods. Specifically: a foreign Model that one of the
// class methods REFERENCES must appear as a NeighbourBrief on the class.
func TestNeighbourBrief_1867_ClassMethodHopExpansion(t *testing.T) {
	classID := "1111111111111111"
	methodAID := "2222222222222222"
	methodBID := "3333333333333333"
	foreignModelID := "4444444444444444"
	foreignServiceID := "5555555555555555"

	doc := graph.Document{
		Entities: []graph.Entity{
			{
				ID: classID, Name: "ContractViewSet",
				Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "core/views/contract_viewset.py",
				Language:   "python",
				StartLine:  10, EndLine: 100,
			},
			{
				ID: methodAID, Name: "ContractViewSet.list",
				Kind: "SCOPE.Operation", Subtype: "method",
				SourceFile: "core/views/contract_viewset.py", Language: "python",
				StartLine: 20, EndLine: 30,
			},
			{
				ID: methodBID, Name: "ContractViewSet.create",
				Kind: "SCOPE.Operation", Subtype: "method",
				SourceFile: "core/views/contract_viewset.py", Language: "python",
				StartLine: 32, EndLine: 50,
			},
			// Foreign Model referenced by a method body.
			{
				ID: foreignModelID, Name: "Building",
				Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "core/models/building.py", Language: "python",
				StartLine: 1, EndLine: 40,
			},
			// External service called by a method body.
			{
				ID: foreignServiceID, Name: "send_notification",
				Kind: "SCOPE.Operation", Subtype: "function",
				SourceFile: "core/services/notify.py", Language: "python",
				StartLine: 5, EndLine: 12,
			},
		},
		Relationships: []graph.Relationship{
			// Direct CONTAINS children (methods).
			{ID: "r1", FromID: classID, ToID: methodAID, Kind: "CONTAINS"},
			{ID: "r2", FromID: classID, ToID: methodBID, Kind: "CONTAINS"},
			// Method bodies touch foreign entities — these are the
			// depth-2 typed deps that should bubble up to the class.
			{ID: "r3", FromID: methodAID, ToID: foreignModelID, Kind: "REFERENCES"},
			{ID: "r4", FromID: methodBID, ToID: foreignServiceID, Kind: "CALLS"},
		},
	}

	group := writeGroupGraph(t, "issue-1867-group", classID, doc)

	bundle, err := docgen.BuildBundle(context.Background(), docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        group,
			SeedEntityID: classID,
			Section:      "flows",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	byName := map[string]docgen.NeighbourBrief{}
	for _, b := range bundle.GraphContext.NeighbourBriefs {
		byName[b.Name] = b
	}

	// Foreign Model and foreign Service must appear via the method hop.
	building, ok := byName["Building"]
	if !ok {
		t.Fatalf("Building (depth-2 typed dep) missing from neighbour_briefs; "+
			"got %d briefs", len(bundle.GraphContext.NeighbourBriefs))
	}
	if building.Relationship != "REFERENCES" {
		t.Errorf("Building Relationship = %q, want REFERENCES", building.Relationship)
	}
	if v := building.Properties["via_method_hop"]; v != "true" {
		t.Errorf("Building Properties[via_method_hop] = %q, want \"true\"", v)
	}
	send, ok := byName["send_notification"]
	if !ok {
		t.Fatalf("send_notification (depth-2 CALL) missing from neighbour_briefs")
	}
	if send.Relationship != "CALLS" {
		t.Errorf("send_notification Relationship = %q, want CALLS", send.Relationship)
	}
}

// TestNeighbourBrief_2012_ExtendsVisible asserts that an EXTENDS edge from
// the class seed to a base class surfaces in NeighbourBriefs with
// Relationship="EXTENDS" (verifying that the edge kind is preserved
// post-#1879 / #2025 — see #2012).
func TestNeighbourBrief_2012_ExtendsVisible(t *testing.T) {
	seedID := "1111111111111111"
	baseID := "2222222222222222"

	doc := graph.Document{
		Entities: []graph.Entity{
			{
				ID: seedID, Name: "ReportViewSet",
				Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "core/views/report.py", Language: "python",
				StartLine: 10, EndLine: 50,
			},
			{
				ID: baseID, Name: "BaseAuditedViewSet",
				Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "core/views/base.py", Language: "python",
				StartLine: 1, EndLine: 30,
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: seedID, ToID: baseID, Kind: "EXTENDS"},
		},
	}
	group := writeGroupGraph(t, "issue-2012-group", seedID, doc)
	bundle, err := docgen.BuildBundle(context.Background(), docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        group,
			SeedEntityID: seedID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	var rel string
	for _, b := range bundle.GraphContext.NeighbourBriefs {
		if b.Name == "BaseAuditedViewSet" {
			rel = b.Relationship
			break
		}
	}
	if rel == "" {
		t.Fatalf("BaseAuditedViewSet missing from neighbour_briefs — EXTENDS edge dropped (issue #2012)")
	}
	if !strings.EqualFold(rel, "EXTENDS") {
		t.Errorf("base-class neighbour Relationship = %q, want EXTENDS — issue #2012 still broken", rel)
	}
}
