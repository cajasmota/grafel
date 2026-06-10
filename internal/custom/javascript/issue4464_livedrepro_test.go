package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
	_ "github.com/cajasmota/archigraph/internal/extractors/javascript"
)

// Issue #4464 LIVE-REPRO — handler→request-DTO ACCEPTS_INPUT edge.
//
// Byte-copies of REAL core-backend-v3 files are committed under
// testdata/dto_4464:
//
//   - permit.controller.ts        — `@Get() list(@Query() query: PermitListQueryDto)
//     : Promise<PermitListResponse>` plus several @Body handlers.
//   - permit-list.query.dto.ts    — the `@Query` request DTO class + its fields.
//
// PRE-FIX: the NestJS extractor emitted an ACCEPTS_INPUT edge ONLY for @Body()
// DTOs (reNestBodyParam). The whole-object @Query()/@Param()/@Headers() request
// DTOs got a `parameters` PROPERTY but NO graph EDGE, so PermitListQueryDto (and
// its CONTAINS field subtree, #4328) floated as orphan degree-1 nodes — the
// upvate-v3 'orphan ring' root cause.
//
// POST-FIX: each whole-object non-@Body request DTO yields an ACCEPTS_INPUT edge
// (match_source=param_decorator) to `Class:<dto>`, which the central resolver
// (resolve.BuildIndex / resolve.References) rewrites to the real DTO class
// entity post-merge — so the edge is merge-stable. This test runs the REAL
// extract → merge → resolve pipeline and asserts the handler becomes an inbound
// neighbor of the DTO (was 0 inbound, now 1).

func nestExtract4464(t *testing.T, base, repoPath string) []types.EntityRecord {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "dto_4464", base))
	if err != nil {
		t.Fatalf("read %s: %v", base, err)
	}
	e, ok := extreg.Get("custom_js_nestjs")
	if !ok {
		t.Fatal("custom_js_nestjs not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: repoPath, Language: "typescript", Content: b})
	if err != nil {
		t.Fatalf("nest extract %s: %v", base, err)
	}
	return ents
}

func tsExtract4464(t *testing.T, base, repoPath string) []types.EntityRecord {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "dto_4464", base))
	if err != nil {
		t.Fatalf("read %s: %v", base, err)
	}
	p := sitter.NewParser()
	p.SetLanguage(tstypescript.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, b)
	if err != nil {
		t.Fatalf("parse %s: %v", base, err)
	}
	e, ok := extreg.Get("typescript")
	if !ok {
		t.Fatal("typescript extractor not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: repoPath, Language: "typescript", Content: b, Tree: tree})
	if err != nil {
		t.Fatalf("ts extract %s: %v", base, err)
	}
	return ents
}

// flattenRels lifts every entity's embedded relationships into a flat slice
// with FromID anchored to the owning entity — the shape resolve.References
// rewrites in place.
func flattenRels(ents []types.EntityRecord) []types.RelationshipRecord {
	var rels []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			r.FromID = e.ID
			rels = append(rels, r)
		}
	}
	return rels
}

// TestIssue4464_QueryDTO_HandlerAcceptsInputEdge is the end-to-end gate: the
// @Query whole-object request DTO must end up with the handler as an inbound
// ACCEPTS_INPUT neighbor after the real merge+resolve pipeline.
func TestIssue4464_QueryDTO_HandlerAcceptsInputEdge(t *testing.T) {
	ctrl := nestExtract4464(t, "permit.controller.ts", "src/modules/permits/api/permit.controller.ts")
	dto := tsExtract4464(t, "permit-list.query.dto.ts", "src/modules/permits/dto/request/permit-list.query.dto.ts")

	// Extractor-side: the @Query() DTO now produces an ACCEPTS_INPUT edge (it
	// did NOT before — only @Body did).
	if !hasDTOEdge(ctrl, "ACCEPTS_INPUT", "Class:PermitListQueryDto") {
		t.Fatal("expected ACCEPTS_INPUT -> Class:PermitListQueryDto from @Query() param (#4464 root fix)")
	}
	if owner := dtoEdgeOwner(ctrl, "ACCEPTS_INPUT", "Class:PermitListQueryDto"); owner != "GET list" {
		t.Errorf("expected edge owner 'GET list', got %q", owner)
	}

	// The edge must be tagged so it is auditable / not confused with @Body.
	var paramEdge *types.RelationshipRecord
	for i := range ctrl {
		for j := range ctrl[i].Relationships {
			r := &ctrl[i].Relationships[j]
			if r.Kind == "ACCEPTS_INPUT" && r.ToID == "Class:PermitListQueryDto" {
				paramEdge = r
			}
		}
	}
	if paramEdge == nil {
		t.Fatal("param ACCEPTS_INPUT edge not found")
	}
	if paramEdge.Properties["match_source"] != "param_decorator" {
		t.Errorf("expected match_source=param_decorator, got %q", paramEdge.Properties["match_source"])
	}
	if paramEdge.Properties["param_in"] != "query" {
		t.Errorf("expected param_in=query, got %q", paramEdge.Properties["param_in"])
	}

	// In-pipeline merge + resolve: the Class:<dto> stub must rewrite to the real
	// DTO class entity (merge-stable resolution by qualified name).
	all := append(append([]types.EntityRecord{}, ctrl...), dto...)
	idx := resolve.BuildIndex(all)
	dtoID, ok := idx.Lookup("Class:PermitListQueryDto")
	if !ok || dtoID == "" {
		t.Fatal("PermitListQueryDto stub failed to resolve through resolve.BuildIndex — would stay orphan")
	}

	// BEFORE the fix this count is 0 (the orphan-ring symptom); AFTER it is >=1.
	rels := flattenRels(all)
	resolve.References(rels, idx)
	inbound := 0
	for _, r := range rels {
		if r.ToID == dtoID {
			inbound++
		}
	}
	if inbound < 1 {
		t.Fatalf("expected >=1 resolved inbound edge to PermitListQueryDto after fix, got %d (still orphan)", inbound)
	}
	t.Logf("resolved inbound edges to PermitListQueryDto = %d (was 0 pre-fix)", inbound)
}

// TestIssue4464_BodyDTOStillEdged guards the pre-existing @Body() ACCEPTS_INPUT
// path: the consolidation must not drop @Body DTO edges nor double-emit them.
func TestIssue4464_BodyDTOStillEdged(t *testing.T) {
	ctrl := nestExtract4464(t, "permit.controller.ts", "src/modules/permits/api/permit.controller.ts")

	// @Body() PermitCreateBodyDto must still carry exactly one ACCEPTS_INPUT edge
	// (emitted by reNestBodyParam, NOT duplicated by the #4464 param pass which
	// skips In=="body").
	count := 0
	for _, e := range ctrl {
		for _, r := range e.Relationships {
			if r.Kind == "ACCEPTS_INPUT" && r.ToID == "Class:PermitCreateBodyDto" {
				count++
				if r.Properties["match_source"] != "body_param_annotation" {
					t.Errorf("@Body edge should keep match_source=body_param_annotation, got %q",
						r.Properties["match_source"])
				}
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 ACCEPTS_INPUT to Class:PermitCreateBodyDto, got %d (double-emit?)", count)
	}
}

// TestIssue4464_KeyedQueryParamNoDTOEdge is the false-positive gate: a
// single-field selector like `@Query('group_id') gid: number` must NOT emit a
// handler→DTO edge (it binds a primitive field, not a DTO type).
func TestIssue4464_KeyedQueryParamNoDTOEdge(t *testing.T) {
	src := `@Controller('devices')
export class DeviceController {
  @Get('filters')
  filters(@Query('group_id', ParseIntPipe) groupId: number, @Query('building_id') buildingId: number) {
    return this.svc.filters(groupId, buildingId);
  }
}`
	ents := extractFull(t, "custom_js_nestjs", fi("device.controller.ts", "typescript", src))
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "ACCEPTS_INPUT" {
				t.Errorf("keyed primitive @Query param must not emit ACCEPTS_INPUT, got -> %s", r.ToID)
			}
		}
	}
}

// TestIssue4464_WholeQueryDTOEdge is the minimal positive unit: `@Query() q: Dto`
// (no quoted key, DTO type) → ACCEPTS_INPUT edge.
func TestIssue4464_WholeQueryDTOEdge(t *testing.T) {
	src := `@Controller('items')
export class ItemController {
  @Get()
  list(@Query() filter: ItemFilterDto) {
    return this.svc.list(filter);
  }
}`
	ents := extractFull(t, "custom_js_nestjs", fi("item.controller.ts", "typescript", src))
	if !hasDTOEdge(ents, "ACCEPTS_INPUT", "Class:ItemFilterDto") {
		t.Fatal("expected ACCEPTS_INPUT -> Class:ItemFilterDto from @Query() whole DTO")
	}
}
