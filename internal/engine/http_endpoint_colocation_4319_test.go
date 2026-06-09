package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// #4319 re-fix — file:line co-location bridge.
//
// #4326 added a same-file bare↔qualified NAME reconciliation, but it only fires
// when the surviving merged synthetic carries a `source_handler` whose bare name
// matches a same-file handler entity. The LIVE NestJS island is different: for
// `POST /inspections/me-manage/merge` the surviving merged http_endpoint_definition
// carried NO bindable source_handler, so every name-based path missed and the
// endpoint stayed an island even though its handler Operation sits at the EXACT
// same file:line (the decorated controller method).
//
// The re-fix is two-part:
//   1. synthesizeNestJS now anchors the synthetic at the handler METHOD line
//      (TestColocation4319_NestJSSynthCarriesMethodLine).
//   2. ResolveHTTPEndpointHandlers binds the endpoint to the lone handler-kind
//      Operation at its own file:line when name resolution fails — guarded to an
//      exactly-one match so it never guesses.

// TestColocation4319_NestJSSynthCarriesMethodLine proves part 1: the synthetic
// is anchored at the handler method's line, not 0, so co-location has a signal.
func TestColocation4319_NestJSSynthCarriesMethodLine(t *testing.T) {
	src := "import { Controller, Post, Body } from '@nestjs/common';\n" +
		"\n" +
		"@Controller('inspections/me-manage')\n" + // line 3
		"export class BuildingController {\n" + // line 4
		"  constructor(private readonly svc: BuildingService) {}\n" + // line 5
		"\n" + // line 6
		"  @Post('merge')\n" + // line 7
		"  mergeMaintenanceEvaluations(@Body() body: BuildingMeMergeBody): Promise<void> {\n" + // line 8
		"    return this.svc.mergeMaintenanceEvaluations(body);\n" +
		"  }\n" +
		"}\n"
	file := "src/modules/buildings/api/building.controller.ts"
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path: file, Content: []byte(src), Language: "typescript",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	var got *types.EntityRecord
	for i := range res.Entities {
		if res.Entities[i].Kind == httpEndpointDefinitionKind {
			got = &res.Entities[i]
		}
	}
	if got == nil {
		t.Fatal("no http_endpoint_definition synthesized for the NestJS @Post route")
	}
	// The handler method `mergeMaintenanceEvaluations(...)` is on line 8.
	if got.StartLine != 8 {
		t.Errorf("synthetic must anchor at the handler method line 8 for #4319 co-location, got line %d", got.StartLine)
	}
}

// TestColocation4319_LiveShape_NoSourceHandler_Bridges is the LIVE-shape repro:
// an http_endpoint_definition with NO resolvable source_handler and a single
// handler Operation at the SAME file:line. The bridge must now form and the
// handler's VALIDATES/CALLS must be reachable from the endpoint.
func TestColocation4319_LiveShape_NoSourceHandler_Bridges(t *testing.T) {
	file := "src/modules/buildings/api/building.controller.ts"
	const line = 108
	handler := types.EntityRecord{
		Kind: "SCOPE.Operation", Name: "mergeMaintenanceEvaluations",
		SourceFile: file, StartLine: line, Language: "typescript", Subtype: "method",
		Relationships: []types.RelationshipRecord{
			{Kind: string(types.RelationshipKindValidates), ToID: "Class:BuildingMeMergeBody"},
			{Kind: string(types.RelationshipKindCalls), ToID: "SCOPE.Operation:BuildingService.merge"},
		},
	}
	// The surviving merged synthetic carries NO source_handler (the live island).
	synth := types.EntityRecord{
		Kind: httpEndpointDefinitionKind, Name: "http:POST:/inspections/me-manage/merge",
		SourceFile: file, StartLine: line, Language: "typescript",
		Properties: map[string]string{
			"verb": "POST", "path": "/inspections/me-manage/merge",
			"framework": "nestjs", "pattern_type": "http_endpoint_synthesis",
		},
	}
	out, stats := ResolveHTTPEndpointHandlers([]types.EntityRecord{handler, synth})
	if stats.HandlerResolved != 1 {
		t.Fatalf("ISLAND: co-location did not bridge the live NestJS shape, resolved=%d dropped=%d", stats.HandlerResolved, stats.HandlerDropped)
	}
	edge, ok := implementsEdgeFor(out[0])
	if !ok {
		t.Fatal("no IMPLEMENTS edge from co-located handler to endpoint (#4319 re-fix)")
	}
	if edge.ToID != "http_endpoint_definition:http:POST:/inspections/me-manage/merge" {
		t.Errorf("IMPLEMENTS targets wrong endpoint: %s", edge.ToID)
	}
	// Traversing endpoint→handler reaches the handler's VALIDATES + CALLS.
	var sawValidates, sawCalls bool
	for _, r := range out[0].Relationships {
		switch r.Kind {
		case string(types.RelationshipKindValidates):
			sawValidates = true
		case string(types.RelationshipKindCalls):
			sawCalls = true
		}
	}
	if !sawValidates || !sawCalls {
		t.Errorf("co-located handler's downstream edges unreachable: validates=%v calls=%v", sawValidates, sawCalls)
	}
}

// TestColocation4319_TwoOperationsSameLine_NoGuess is the no-guess guard: when
// two handler-kind Operations share the endpoint's file:line, co-location must
// NOT bind either (it would be a guess), leaving the endpoint to existing
// fallbacks.
func TestColocation4319_TwoOperationsSameLine_NoGuess(t *testing.T) {
	file := "src/svc.ts"
	const line = 42
	h1 := types.EntityRecord{Kind: "SCOPE.Operation", Name: "alpha", SourceFile: file, StartLine: line, Language: "typescript"}
	h2 := types.EntityRecord{Kind: "SCOPE.Operation", Name: "beta", SourceFile: file, StartLine: line, Language: "typescript"}
	synth := types.EntityRecord{
		Kind: httpEndpointDefinitionKind, Name: "http:GET:/x",
		SourceFile: file, StartLine: line, Language: "typescript",
		Properties: map[string]string{"framework": "nestjs", "pattern_type": "http_endpoint_synthesis"},
	}
	out, stats := ResolveHTTPEndpointHandlers([]types.EntityRecord{h1, h2, synth})
	if stats.HandlerResolved != 0 {
		t.Fatalf("ambiguous co-location must NOT bridge, resolved=%d", stats.HandlerResolved)
	}
	for _, e := range out {
		if e.Kind != "SCOPE.Operation" {
			continue
		}
		if _, ok := implementsEdgeFor(e); ok {
			t.Errorf("ambiguous file:line must not be guess-bridged, but %s was", e.Name)
		}
	}
}

// TestColocation4319_DTOAtLineNotBridged guards that a non-handler entity (a DTO
// Schema/Component/Class) co-located with the endpoint line is NEVER chosen by
// co-location — only handler-kind Operations are candidates.
func TestColocation4319_DTOAtLineNotBridged(t *testing.T) {
	file := "src/orders.controller.ts"
	const line = 20
	dto := types.EntityRecord{Kind: "SCOPE.Class", Name: "CreateOrderDto", SourceFile: file, StartLine: line, Language: "typescript"}
	synth := types.EntityRecord{
		Kind: httpEndpointDefinitionKind, Name: "http:POST:/orders",
		SourceFile: file, StartLine: line, Language: "typescript",
		Properties: map[string]string{"framework": "nestjs", "pattern_type": "http_endpoint_synthesis"},
	}
	out, stats := ResolveHTTPEndpointHandlers([]types.EntityRecord{dto, synth})
	if stats.HandlerResolved != 0 {
		t.Fatalf("co-location must not bind a DTO at the endpoint line, resolved=%d", stats.HandlerResolved)
	}
	for _, e := range out {
		if _, ok := implementsEdgeFor(e); ok {
			t.Errorf("DTO %s must not receive an IMPLEMENTS bridge via co-location", e.Name)
		}
	}
}

// TestColocation4319_NoLine_NoColocation guards that an endpoint with no positive
// line (synthetic anchored at 0) does NOT co-locate to a line-0 handler — line 0
// is the unset sentinel and carries no co-location signal.
func TestColocation4319_NoLine_NoColocation(t *testing.T) {
	file := "src/x.controller.ts"
	handler := types.EntityRecord{Kind: "SCOPE.Operation", Name: "handle", SourceFile: file, StartLine: 0, Language: "typescript"}
	synth := types.EntityRecord{
		Kind: httpEndpointDefinitionKind, Name: "http:GET:/y",
		SourceFile: file, StartLine: 0, Language: "typescript",
		Properties: map[string]string{"framework": "nestjs", "pattern_type": "http_endpoint_synthesis"},
	}
	_, stats := ResolveHTTPEndpointHandlers([]types.EntityRecord{handler, synth})
	if stats.HandlerResolved != 0 {
		t.Fatalf("line-0 endpoint must not co-locate (no signal), resolved=%d", stats.HandlerResolved)
	}
}

// TestColocation4319_NameResolutionStillWinsFirst guards #4326's name-based path
// is preserved: when a bindable source_handler IS present, the bridge forms via
// name resolution (HandlerResolved), and co-location never needs to fire.
func TestColocation4319_NameResolutionStillWinsFirst(t *testing.T) {
	file := "src/orders.controller.ts"
	// Handler at a DIFFERENT line than the synthetic, so ONLY name resolution can
	// bind it — proving the name path runs and co-location is not required.
	handler := types.EntityRecord{
		Kind: "SCOPE.Operation", Name: "createOrder", SourceFile: file, StartLine: 55, Language: "typescript",
	}
	synth := types.EntityRecord{
		Kind: httpEndpointDefinitionKind, Name: "http:POST:/orders",
		SourceFile: file, StartLine: 50, Language: "typescript",
		Properties: map[string]string{
			"framework": "nestjs", "pattern_type": "http_endpoint_synthesis",
			"source_handler": "Controller:createOrder",
		},
	}
	out, stats := ResolveHTTPEndpointHandlers([]types.EntityRecord{handler, synth})
	if stats.HandlerResolved != 1 {
		t.Fatalf("name-based resolution regressed: resolved=%d", stats.HandlerResolved)
	}
	if _, ok := implementsEdgeFor(out[0]); !ok {
		t.Fatal("name-based handler should still bridge")
	}
}
