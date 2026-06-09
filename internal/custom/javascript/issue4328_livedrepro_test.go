package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

// Issue #4328 LIVE-REPRO.
//
// Byte-copies of REAL core-backend-v3 files are committed under
// testdata/issue4328. We run the ACTUAL typeorm / validation-schema /
// nestjs-mongoose extractors AND the ACTUAL resolve.BuildIndex symbol table
// over them and assert that the DTO properties and entity fields they decorate
// are NOT orphans:
//
//	(1) field membership — every emitted @Column / relation / @Prop field entity
//	    carries a CONTAINS edge FROM its owning @Entity / @Schema class (so it is
//	    a member, not a standalone node); and
//	(2) thunk-target-type resolution — a @ManyToOne(() => Role) relation field and
//	    a @ValidateNested() @Type(() => XDto) nested-DTO field emit a REFERENCES
//	    edge to the target class stub, and that stub RESOLVES against a real
//	    same-file (or symbol-table) class entity.
//
// Pre-fix: typeorm.go emitted each @Column / relation as a standalone entity
// with ZERO inbound/outbound edges (pure orphan), and the class-validator
// extractor folded nested @Type(() => XDto) targets into props with no edge —
// leaving the nested DTO with no inbound link. ~619 such fields ringed on v3.

func loadRepro4328(t *testing.T, base, repoPath string) extreg.FileInput {
	t.Helper()
	p := filepath.Join("testdata", "issue4328", base)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read repro %s: %v", p, err)
	}
	return extreg.FileInput{Path: repoPath, Language: "typescript", Content: b}
}

func extract4328(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract %s: %v", name, err)
	}
	return ents
}

// hasContainsFrom reports whether some entity in ents declares a CONTAINS edge
// whose ToID names the given member entity (by ID or by Class:/field name stub),
// i.e. the member is owned by a class rather than floating free.
func hasContainsTo(ents []types.EntityRecord, memberID, memberName string) bool {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindContains) {
				continue
			}
			if r.ToID == memberID || r.ToID == "Field:"+memberName || r.ToID == memberName {
				return true
			}
		}
	}
	return false
}

// edgesOfKind collects all relationships of a kind across all entities.
func edgesOfKind(ents []types.EntityRecord, kind types.RelationshipKind) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(kind) {
				out = append(out, r)
			}
		}
	}
	return out
}

// TestIssue4328_TypeORMEntity_FieldsAreMembers_NotOrphans runs the real TypeORM
// entity through the pipeline and asserts every @Column and the @ManyToOne
// relation field is a CONTAINS member of UserGroupRole, and the relation's
// thunk-target (Role) is captured (REFERENCES) and resolves.
func TestIssue4328_TypeORMEntity_FieldsAreMembers_NotOrphans(t *testing.T) {
	file := loadRepro4328(t,
		"user-group-role.entity.ts.txt",
		"src/common/auth/persistence/user-group-role.entity.ts")

	ents := extract4328(t, "custom_js_typeorm", file)
	for _, e := range ents {
		t.Logf("ENTITY kind=%s subtype=%s name=%q rels=%d", e.Kind, e.Subtype, e.Name, len(e.Relationships))
		for _, r := range e.Relationships {
			t.Logf("   EDGE %s from=%s to=%s", r.Kind, r.FromID, r.ToID)
		}
	}

	// (1) Every @Column / relation field must be a CONTAINS member of its owner.
	fieldSubtypes := map[string]bool{"column": true, "relation": true}
	var fieldEnts []types.EntityRecord
	for _, e := range ents {
		if fieldSubtypes[e.Subtype] {
			fieldEnts = append(fieldEnts, e)
		}
	}
	if len(fieldEnts) == 0 {
		t.Fatal("no column/relation field entities extracted")
	}
	orphans := 0
	for _, fe := range fieldEnts {
		fieldName := fe.Properties["field_name"]
		if fieldName == "" {
			fieldName = fe.Name
		}
		if !hasContainsTo(ents, fe.ID, fieldName) {
			orphans++
			t.Errorf("ORPHAN field (no CONTAINS from owner): subtype=%s name=%q", fe.Subtype, fe.Name)
		}
	}
	t.Logf("orphaned field entities (TypeORM): %d / %d", orphans, len(fieldEnts))

	// (2) The @ManyToOne(() => Role) relation's target type must be REFERENCES'd.
	refs := edgesOfKind(ents, types.RelationshipKindReferences)
	foundRoleRef := false
	for _, r := range refs {
		if r.ToID == "Class:Role" {
			foundRoleRef = true
		}
	}
	if !foundRoleRef {
		t.Errorf("expected REFERENCES edge to Class:Role for @ManyToOne thunk target; refs=%v", refs)
	}

	// (2 continued) Role resolves in the symbol table (alongside a real Role entity).
	roleEnt := types.EntityRecord{
		Name: "Role", Kind: "SCOPE.Schema", Subtype: "entity",
		SourceFile: "src/common/auth/persistence/role.entity.ts", Language: "typescript",
		Properties: map[string]string{"kind": "SCOPE.Schema", "subtype": "entity"},
	}
	roleEnt.ID = roleEnt.ComputeID()
	idx := resolve.BuildIndex(append(ents, roleEnt))
	if id, ok := idx.Lookup("Class:Role"); !ok || id != roleEnt.ID {
		t.Errorf("Class:Role stub did not resolve to real Role entity (ok=%v id=%s)", ok, id)
	}
}

// TestIssue4328_ClassValidatorDTO_NestedTypeTargetEdge runs the real
// class-validator DTO with @ValidateNested() @Type(() => XDto) through the
// pipeline and asserts the nested-DTO target gets a REFERENCES edge that
// resolves against the same-file nested DTO class entity.
func TestIssue4328_ClassValidatorDTO_NestedTypeTargetEdge(t *testing.T) {
	file := loadRepro4328(t,
		"create-checklist.body.dto.ts.txt",
		"src/modules/checklists/dto/request/create-checklist.body.dto.ts")

	ents := extract4328(t, "custom_js_validation_schema", file)
	for _, e := range ents {
		t.Logf("ENTITY kind=%s subtype=%s name=%q rels=%d", e.Kind, e.Subtype, e.Name, len(e.Relationships))
		for _, r := range e.Relationships {
			t.Logf("   EDGE %s from=%s to=%s props=%v", r.Kind, r.FromID, r.ToID, r.Properties)
		}
	}

	// CreateChecklistBody.categories -> CreateChecklistCategoryBody (nested DTO).
	// CreateChecklistCategoryBody.datasource -> CreateChecklistItemBody.
	refs := edgesOfKind(ents, types.RelationshipKindReferences)
	wantTargets := map[string]bool{
		"Class:CreateChecklistCategoryBody": false,
		"Class:CreateChecklistItemBody":     false,
	}
	for _, r := range refs {
		if _, ok := wantTargets[r.ToID]; ok {
			wantTargets[r.ToID] = true
		}
	}
	for stub, got := range wantTargets {
		if !got {
			t.Errorf("expected REFERENCES edge to nested DTO %q; refs=%v", stub, refs)
		}
	}

	// Targets resolve to the same-file nested DTO class entities in the symbol table.
	idx := resolve.BuildIndex(ents)
	for stub := range wantTargets {
		if _, ok := idx.Lookup(stub); !ok {
			t.Errorf("nested DTO stub %q failed to resolve in symbol table", stub)
		}
	}
}

// TestIssue4328_MongooseSchema_PropFieldsAreMembers runs the real NestJS
// @Schema()/@Prop() mongoose schema through the pipeline and asserts each @Prop
// field is a CONTAINS member of the Violation schema class (not an orphan).
func TestIssue4328_MongooseSchema_PropFieldsAreMembers(t *testing.T) {
	file := loadRepro4328(t,
		"violation.schema.ts.txt",
		"src/modules/buildings/models/violation.schema.ts")

	ents := extract4328(t, "custom_js_mongoose", file)
	for _, e := range ents {
		t.Logf("ENTITY kind=%s subtype=%s name=%q rels=%d", e.Kind, e.Subtype, e.Name, len(e.Relationships))
		for _, r := range e.Relationships {
			t.Logf("   EDGE %s from=%s to=%s", r.Kind, r.FromID, r.ToID)
		}
	}

	// Expect a Violation @Schema class and its @Prop fields as members.
	var propEnts []types.EntityRecord
	for _, e := range ents {
		if e.Subtype == "prop" {
			propEnts = append(propEnts, e)
		}
	}
	if len(propEnts) == 0 {
		t.Fatal("no @Prop field entities extracted from NestJS mongoose schema (#4328)")
	}
	for _, pe := range propEnts {
		fieldName := pe.Properties["field_name"]
		if fieldName == "" {
			fieldName = pe.Name
		}
		if !hasContainsTo(ents, pe.ID, fieldName) {
			t.Errorf("ORPHAN @Prop field (no CONTAINS from Violation schema): name=%q", pe.Name)
		}
	}
}
