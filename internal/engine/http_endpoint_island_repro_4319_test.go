package engine

import (
	"context"
	tstypescript "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/typescript"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
	"os"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #4319 LIVE REPRO + LONG-TERM-FIX gate.
//
// The endpoint `POST /v1/buildings/inspections/me-manage/merge` (acme-v3 BuildingController)
// was a graph ISLAND on the live graph after a full reindex, despite TWO prior
// detect-and-repair fixes (#4326 bare↔qualified name match, #4330 file:line
// co-location). Both reproduced in HAND-BUILT fixtures (handler StartLine set
// equal to the synthetic's) but failed on the live graph because the bridge was
// rebuilt POST-merge from a string/line that the merge/dedup step destabilises.
//
// This file reproduces the LIVE island mechanism faithfully and proves the
// long-term synthesis-time structural bridge survives it.

const reproControllerFile = "src/modules/buildings/api/building.controller.ts"

// realControllerSrc loads the exact acme-v3 controller copied into the repo for
// the repro; falls back to a faithful reconstruction of the merge route shape.
func realControllerSrc(t *testing.T) []byte {
	t.Helper()
	if b, err := os.ReadFile("_repro_building.controller.txt"); err == nil && len(b) > 0 {
		return b
	}
	return []byte(`import { Body, Controller, HttpCode, HttpStatus, Post } from '@nestjs/common';
import { BuildingService } from '../services/building.service';
import { BuildingMeMergeBody } from '../dto/request/building-me-merge.body.dto';
import { BuildingMeMergeResponse } from '../dto/response/building-me.response.dto';

@Controller({ path: 'buildings', version: '1' })
export class BuildingController {
  constructor(private readonly service: BuildingService) {}

  @Post('inspections/me-manage/merge')
  @HttpCode(HttpStatus.OK)
  mergeMaintenanceEvaluations(@Body() body: BuildingMeMergeBody): Promise<BuildingMeMergeResponse> {
    return this.service.mergeMaintenanceEvaluations(body);
  }
}
`)
}

// liveProducers runs the two real producers the indexer runs on a NestJS
// controller and returns (handler+endpoint entities, synthesis relationships).
func liveProducers(t *testing.T, src []byte) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()

	// Producer 1 — tree-sitter JS/TS extractor → handler Operation(s).
	parser, err := tsofficial.New().NewParser(tstypescript.Language())
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parseTS: %v", err)
	}
	jsEnts, err := javascript.New().Extract(context.Background(), extreg.FileInput{
		Path: reproControllerFile, Content: src, Language: "typescript", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("js extract: %v", err)
	}

	// Producer 2 — engine route synthesizer → http_endpoint_definition (+ #4319
	// synthesis-time bridge relationship).
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	res, err := New(rules).Detect(context.Background(), extreg.FileInput{
		Path: reproControllerFile, Content: src, Language: "typescript",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	ents := append([]types.EntityRecord{}, jsEnts...)
	ents = append(ents, res.Entities...)
	return ents, res.Relationships
}

func findMergeHandler(ents []types.EntityRecord) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Name == "mergeMaintenanceEvaluations" {
			return &ents[i]
		}
	}
	return nil
}

func findMergeEndpoint(ents []types.EntityRecord) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == httpEndpointDefinitionKind && ents[i].Properties != nil &&
			ents[i].Properties["verb"] == "POST" &&
			ents[i].Properties["path"] == "/v1/buildings/inspections/me-manage/merge" {
			return &ents[i]
		}
	}
	return nil
}

// runFullBridge mirrors buildDocument: ResolveHTTPEndpointHandlers (the
// http-endpoint resolve pass) over the merged entities, then ComputeID + the
// CENTRAL resolver (resolve.References) over the synthesis relationships. It
// returns true iff, after both passes, the merge endpoint has a resolved inbound
// IMPLEMENTS edge from its handler entity (i.e. it is NOT an island).
func runFullBridge(t *testing.T, ents []types.EntityRecord, rels []types.RelationshipRecord) (bool, ResolveHTTPEndpointStats) {
	t.Helper()
	merged, stats := ResolveHTTPEndpointHandlers(ents)

	// Stamp deterministic IDs (no repo tag, like ComputeID in stampEntityIDs path).
	for i := range merged {
		merged[i].ID = merged[i].ComputeID()
	}
	endpoint := findMergeEndpoint(merged)
	handler := findMergeHandler(merged)
	if endpoint == nil {
		t.Fatal("no http_endpoint_definition for the merge route")
	}

	// Central resolver over the standalone synthesis relationships (the #4319
	// synthesis-time bridge lives here). This is exactly what buildDocument does
	// via resolve.References(pass2Rels, idx).
	idx := resolve.BuildIndex(merged)
	resolve.References(rels, idx)

	// Is the endpoint reachable from a handler via a RESOLVED IMPLEMENTS edge?
	endpointID := endpoint.ID
	bridged := false

	// (a) standalone synthesis-time bridge resolved to hex IDs.
	for _, r := range rels {
		if r.Kind == implementsEdgeKind && r.ToID == endpointID {
			if handler != nil && r.FromID == handler.ID {
				bridged = true
			}
		}
	}
	// (b) embedded IMPLEMENTS from the http-endpoint resolve pass (stub form —
	//     resolved later in the real pipeline; here we accept the stub that
	//     targets the endpoint, which is the legacy co-location/name path).
	if handler != nil {
		for _, r := range handler.Relationships {
			if r.Kind == implementsEdgeKind &&
				(r.ToID == httpEndpointDefinitionKind+":"+endpoint.Name) {
				bridged = true
			}
		}
	}
	return bridged, stats
}

// TestIslandRepro4319_SynthesisTimeBridgeEmitted proves the bridge is born AT
// SYNTHESIS time as a merge-stable, file-scoped structural edge — independent of
// any later name/line resolution. This is the core of the long-term fix.
func TestIslandRepro4319_SynthesisTimeBridgeEmitted(t *testing.T) {
	_, rels := liveProducers(t, realControllerSrc(t))
	want := "scope:operation:method:typescript:" + reproControllerFile + ":mergeMaintenanceEvaluations"
	var found *types.RelationshipRecord
	for i := range rels {
		if rels[i].Kind == implementsEdgeKind &&
			rels[i].Properties.Get("pattern_type") == "http_endpoint_synthesis_time_bridge" &&
			rels[i].FromID == want {
			found = &rels[i]
		}
	}
	if found == nil {
		t.Fatal("no synthesis-time IMPLEMENTS bridge emitted for the NestJS merge route")
	}
	if found.ToID != httpEndpointDefinitionKind+":http:POST:/v1/buildings/inspections/me-manage/merge" {
		t.Errorf("bridge ToID = %q, want endpoint stub", found.ToID)
	}
}

// TestIslandRepro4319_BridgeSurvivesLegacyFailure is the architectural guarantee:
// the synthesis-time structural bridge resolves the endpoint→handler edge through
// the CENTRAL resolver EVEN WHEN every legacy resolve-pass path (name resolution
// AND file:line co-location) is dead. This is the exact failure mode that left the
// endpoint an island on the live graph after #4326/#4330: a surviving merged
// synthetic with NO source_handler (so name resolution misses) whose StartLine no
// longer agrees with the handler's after merge (so co-location misses).
//
// We model that worst case by ZEROING the synthetic's StartLine (killing the
// co-location signal) and confirming source_handler is absent — then assert the
// http-endpoint resolve pass produces NO bridge, while the synthesis-time
// structural edge (line-independent, file+name scoped) STILL resolves end-to-end.
func TestIslandRepro4319_BridgeSurvivesLegacyFailure(t *testing.T) {
	ents, rels := liveProducers(t, realControllerSrc(t))

	// Kill the co-location signal on the merge endpoint (StartLine→0) and confirm
	// it carries no source_handler — the live post-merge island shape.
	ep := findMergeEndpoint(ents)
	if ep == nil {
		t.Fatal("merge endpoint not synthesized")
	}
	// Model the live post-merge island shape: the merge/dedup step kept a
	// same-(verb,path) synthetic that carries NO bindable source_handler (a
	// same-path definition from another pass — e.g. the OpenAPI-spec synthetic
	// observed on the live acme graph — won attribution), and the surviving
	// synthetic's StartLine no longer agrees with the handler's. Strip both.
	ep.StartLine = 0
	delete(ep.Properties, "source_handler")

	// The http-endpoint resolve pass alone CANNOT bridge this shape (no name, no
	// line) — negative control proving the legacy paths are dead here.
	mergedNoBridge, stats := ResolveHTTPEndpointHandlers(cloneEnts(ents))
	if legacyBridged(mergedNoBridge, "http:POST:/v1/buildings/inspections/me-manage/merge") {
		t.Fatalf("expected legacy resolve pass to FAIL on the no-handler/no-line shape (the live island), but it bridged (resolved=%d)", stats.HandlerResolved)
	}

	// The synthesis-time structural bridge resolves it end-to-end via the central
	// resolver — the endpoint is NOT an island.
	bridged, _ := runFullBridge(t, ents, rels)
	if !bridged {
		t.Fatal("ISLAND: synthesis-time bridge failed to resolve the endpoint→handler edge under the live island shape")
	}
}

// cloneEnts deep-enough copies the entity slice (and each record's Relationships
// header) so a resolve pass that appends embedded edges doesn't perturb the
// caller's slice.
func cloneEnts(in []types.EntityRecord) []types.EntityRecord {
	out := make([]types.EntityRecord, len(in))
	copy(out, in)
	for i := range out {
		out[i].Relationships = append([]types.RelationshipRecord(nil), in[i].Relationships...)
	}
	return out
}

// legacyBridged reports whether any handler entity carries an embedded IMPLEMENTS
// stub targeting the named endpoint — i.e. the http-endpoint resolve pass bridged
// it (as opposed to the synthesis-time structural edge, which is a standalone rel).
func legacyBridged(ents []types.EntityRecord, endpointName string) bool {
	target := httpEndpointDefinitionKind + ":" + endpointName
	for i := range ents {
		if ents[i].Kind == httpEndpointDefinitionKind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == implementsEdgeKind && r.ToID == target {
				return true
			}
		}
	}
	return false
}

// TestIslandRepro4319_HappyPath_NotIsland is the straightforward gate: on the
// clean live pipeline the merge endpoint is not an island.
func TestIslandRepro4319_HappyPath_NotIsland(t *testing.T) {
	ents, rels := liveProducers(t, realControllerSrc(t))
	bridged, stats := runFullBridge(t, ents, rels)
	if !bridged {
		t.Fatalf("endpoint is an island on the clean pipeline (handler_resolved=%d)", stats.HandlerResolved)
	}
}
