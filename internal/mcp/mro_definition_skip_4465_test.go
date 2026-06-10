package mcp

// mro_definition_skip_4465_test.go — #4465. The inheritance resolver must NOT
// fire on TYPE DEFINITIONS. Live on upvate-v3, inspecting the TS class
// `PermitListQueryDto` (SCOPE.Component, dotted qualified name matching its
// file-module path, no EXTENDS edges) returned
//   inheritance:{inherited:true, resolved:false, member:'PermitListQueryDto',
//     owning_class:'src.modules.permits.dto.request.permit-list.query.dto',
//     note:'no defining class found via EXTENDS or knowledge pack'}
// — treating the class's own dotted qname as member=<TypeName>/owning=<module>
// and failing the EXTENDS walk. A definition is explicit by construction and
// must carry NO inheritance section.
//
// Regression guard alongside a genuinely-inherited member that MUST still
// resolve, so the fix skips definitions without disabling real resolution.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// permitListQueryDtoDoc mirrors the real entity shape extracted for
// src/modules/permits/dto/request/permit-list.query.dto.ts: a TS class
// definition emitted as SCOPE.Component with a dotted qualified name equal to
// its file-module path + class leaf, and a standalone Schema field member. No
// EXTENDS edges (the DTO extends nothing).
func permitListQueryDtoDoc() *graph.Document {
	const modulePath = "src.modules.permits.dto.request.permit-list.query.dto"
	return &graph.Document{
		Entities: []graph.Entity{
			{
				ID:            "dto",
				Name:          "PermitListQueryDto",
				QualifiedName: modulePath + ".PermitListQueryDto",
				Kind:          "SCOPE.Component",
				SourceFile:    "src/modules/permits/dto/request/permit-list.query.dto.ts",
				StartLine:     4, EndLine: 40, Language: "typescript",
			},
			// A standalone Schema field member (the engine currently emits DTO
			// fields this way). It must not be mis-tagged as an inherited method.
			{
				ID:            "dto_field",
				Name:          "PermitListQueryDto.generate_permit",
				QualifiedName: modulePath + ".PermitListQueryDto.generate_permit",
				Kind:          "SCOPE.Schema", Subtype: "field",
				SourceFile: "src/modules/permits/dto/request/permit-list.query.dto.ts",
				StartLine:  14, EndLine: 14, Language: "typescript",
			},
		},
	}
}

// TestInspect_TSClassDefinition_NoInheritanceSection — the class definition must
// NOT grow an inheritance section. Before #4465 this returned
// inherited:true/resolved:false.
func TestInspect_TSClassDefinition_NoInheritanceSection(t *testing.T) {
	srv := newTestServer(t, permitListQueryDtoDoc())
	out := callInspect(t, srv, "dto")
	if inh, present := out["inheritance"]; present {
		t.Fatalf("class DEFINITION must carry NO inheritance section, got %v", inh)
	}
}

// TestResolveMember_TSClassDefinition_Explicit — unit-level guard on the
// resolver: a definition resolves as provExplicit, never provUnresolved with a
// fabricated member/owning_class.
func TestResolveMember_TSClassDefinition_Explicit(t *testing.T) {
	srv := newTestServer(t, permitListQueryDtoDoc())
	lr := srv.State.groups["test"].Repos["repo1"]
	var dto *graph.Entity
	for i := range lr.Doc.Entities {
		if lr.Doc.Entities[i].ID == "dto" {
			dto = &lr.Doc.Entities[i]
		}
	}
	if dto == nil {
		t.Fatalf("dto entity not found in loaded repo")
	}
	res := resolveMember(lr, dto)
	if res.Provenance != provExplicit {
		t.Errorf("class definition must resolve as provExplicit, got %q (member=%q owning=%q note=%q)",
			res.Provenance, res.Member, res.OwningClass, res.Note)
	}
}

// TestInspect_GenuineInheritedMember_StillResolves — regression guard: skipping
// definitions must NOT break real inherited-member resolution. ChildService
// extends BaseService (both indexed), and ChildService.handle is a bodyless
// inherited member that must still resolve to BaseService's body.
func TestInspect_GenuineInheritedMember_StillResolves(t *testing.T) {
	srv := newTestServer(t, inRepoBaseDoc())
	out := callInspect(t, srv, "child_handle")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("genuine inherited member must keep its inheritance section, got keys: %v", mapKeys(out))
	}
	if inh["inherited"] != true || inh["resolved"] != true {
		t.Errorf("expected inherited=true/resolved=true, got %v", inh)
	}
	if dc, _ := inh["defining_class"].(string); dc != "BaseService" {
		t.Errorf("expected defining_class BaseService, got %q", dc)
	}
}
