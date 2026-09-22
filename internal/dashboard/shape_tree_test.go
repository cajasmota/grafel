package dashboard

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// Refs #1935 Phase 1 — backend tests for the ShapeTree subtree resolver.
//
// These exercise the GET /api/v2/groups/{id}/shape endpoint plus the
// helpers that populate type_entity_id / has_children on the path
// detail's parameter and response shape rows.

// shapeTreeFixture returns a DashGroup containing a TransferRequest
// POJO (3 fields) and a LoginResponse class whose `user` field
// references a nested UserDTO class — used to validate nested-type
// expansion via the has_children flag.
func shapeTreeFixture() *DashGroup {
	entities := []graph.Entity{
		// TransferRequest class with 3 fields.
		{
			ID: "cls_transfer", Name: "TransferRequest",
			Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: "src/TransferRequest.java", Language: "java",
		},
		{
			ID: "fld_transfer_id", Name: "TransferRequest.transferId",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/TransferRequest.java", Language: "java",
			Signature: "@NotBlank String transferId",
		},
		{
			ID: "fld_confirmed_qty", Name: "TransferRequest.confirmedQty",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/TransferRequest.java", Language: "java",
			Signature: "@Min(0) BigDecimal confirmedQty",
		},
		{
			ID: "fld_items", Name: "TransferRequest.items",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/TransferRequest.java", Language: "java",
			Signature: "List<ItemDTO> items",
		},
		// ItemDTO so the items field has_children=true.
		{
			ID: "cls_item_dto", Name: "ItemDTO",
			Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: "src/ItemDTO.java", Language: "java",
		},
		{
			ID: "fld_item_sku", Name: "ItemDTO.sku",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/ItemDTO.java", Language: "java",
			Signature: "String sku",
		},
		// LoginResponse with a nested UserDTO reference.
		{
			ID: "cls_login_response", Name: "LoginResponse",
			Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: "src/LoginResponse.java", Language: "java",
		},
		{
			ID: "fld_token", Name: "LoginResponse.token",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/LoginResponse.java", Language: "java",
			Signature: "String token",
		},
		{
			ID: "fld_user", Name: "LoginResponse.user",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/LoginResponse.java", Language: "java",
			Signature: "UserDTO user",
		},
		{
			ID: "fld_roles", Name: "LoginResponse.roles",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/LoginResponse.java", Language: "java",
			Signature: "List<String> roles",
		},
		// UserDTO with one field.
		{
			ID: "cls_user_dto", Name: "UserDTO",
			Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: "src/UserDTO.java", Language: "java",
		},
		{
			ID: "fld_user_id", Name: "UserDTO.id",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/UserDTO.java", Language: "java",
			Signature: "Long id",
		},
	}
	rels := []graph.Relationship{
		{FromID: "cls_transfer", ToID: "fld_transfer_id", Kind: "CONTAINS"},
		{FromID: "cls_transfer", ToID: "fld_confirmed_qty", Kind: "CONTAINS"},
		{FromID: "cls_transfer", ToID: "fld_items", Kind: "CONTAINS"},
		{FromID: "cls_item_dto", ToID: "fld_item_sku", Kind: "CONTAINS"},
		{FromID: "cls_login_response", ToID: "fld_token", Kind: "CONTAINS"},
		{FromID: "cls_login_response", ToID: "fld_user", Kind: "CONTAINS"},
		{FromID: "cls_login_response", ToID: "fld_roles", Kind: "CONTAINS"},
		{FromID: "cls_user_dto", ToID: "fld_user_id", Kind: "CONTAINS"},
	}
	return makePathsTestGroup(entities, rels)
}

// TestShape_TransferRequestResolvesFields verifies the canonical
// happy path: TransferRequest expands to 3 field rows with annotations
// and the `items` row has has_children=true because List<ItemDTO>
// unwraps to a known class.
func TestShape_TransferRequestResolvesFields(t *testing.T) {
	grp := shapeTreeFixture()
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/shape?type=TransferRequest")
	if err != nil {
		t.Fatalf("GET shape: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool            `json:"ok"`
		Data v2ShapeResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Fatal("ok=false")
	}
	if body.Data.TypeName != "TransferRequest" {
		t.Errorf("type_name: want TransferRequest, got %q", body.Data.TypeName)
	}
	if len(body.Data.Rows) != 3 {
		t.Fatalf("rows: want 3, got %d (rows=%+v)", len(body.Data.Rows), body.Data.Rows)
	}
	got := map[string]v2ShapeRow{}
	for _, r := range body.Data.Rows {
		got[r.Name] = r
	}
	// transferId — @NotBlank annotation surfaces, non-nullable.
	if r := got["transferId"]; r.Type != "String" || len(r.Annotations) != 1 ||
		r.Annotations[0] != "@NotBlank" {
		t.Errorf("transferId: want type=String anno=[@NotBlank], got %+v", r)
	}
	// confirmedQty — @Min(0) annotation, BigDecimal type, not expandable.
	if r := got["confirmedQty"]; r.Type != "BigDecimal" ||
		!reflect.DeepEqual(r.Annotations, []string{"@Min(0)"}) {
		t.Errorf("confirmedQty: want @Min(0) BigDecimal, got %+v", r)
	}
	if got["confirmedQty"].HasChildren {
		t.Error("confirmedQty.has_children: want false (primitive-like)")
	}
	// items — List<ItemDTO> unwraps to ItemDTO (known class) → has_children=true.
	if r := got["items"]; !r.HasChildren || r.Type != "List<ItemDTO>" {
		t.Errorf("items: want has_children=true type=List<ItemDTO>, got %+v", r)
	}
	if !strings.Contains(got["items"].TypeEntityID, "cls_item_dto") {
		t.Errorf("items.type_entity_id: want suffix cls_item_dto, got %q", got["items"].TypeEntityID)
	}
	// #4869 — the resolved type's source location flows to the row so the
	// frontend can open the type's source on a type-name click.
	if got["items"].TypeSourceFile != "src/ItemDTO.java" {
		t.Errorf("items.type_source_file: want src/ItemDTO.java, got %q", got["items"].TypeSourceFile)
	}
}

// TestShape_NestedExpansion verifies that requesting LoginResponse and
// then the resolved UserDTO type_entity_id walks one level deeper.
func TestShape_NestedExpansion(t *testing.T) {
	grp := shapeTreeFixture()
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	// Depth-1 over LoginResponse → 3 rows; `user` row expandable.
	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/shape?type=LoginResponse")
	if err != nil {
		t.Fatalf("GET LoginResponse: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		OK   bool            `json:"ok"`
		Data v2ShapeResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data.Rows) != 3 {
		t.Fatalf("LoginResponse rows: want 3, got %d", len(body.Data.Rows))
	}
	var userRow v2ShapeRow
	for _, r := range body.Data.Rows {
		if r.Name == "user" {
			userRow = r
		}
	}
	if !userRow.HasChildren {
		t.Fatal("LoginResponse.user.has_children=false; nested UserDTO expansion broken")
	}
	if userRow.TypeEntityID == "" {
		t.Fatal("LoginResponse.user.type_entity_id is empty")
	}
	// Follow the user.type_entity_id to UserDTO and expect 1 row.
	resp2, err := http.Get(ts.URL + "/api/v2/groups/testgrp/shape?type_entity_id=" + userRow.TypeEntityID)
	if err != nil {
		t.Fatalf("GET UserDTO: %v", err)
	}
	defer resp2.Body.Close()
	var body2 struct {
		OK   bool            `json:"ok"`
		Data v2ShapeResponse `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&body2); err != nil {
		t.Fatalf("decode UserDTO: %v", err)
	}
	if body2.Data.TypeName != "UserDTO" {
		t.Errorf("nested type_name: want UserDTO, got %q", body2.Data.TypeName)
	}
	if len(body2.Data.Rows) != 1 || body2.Data.Rows[0].Name != "id" {
		t.Errorf("UserDTO rows: want [id], got %+v", body2.Data.Rows)
	}
}

// tsFieldShapeFixture models TS DTOs whose field entities carry the JS/TS
// `name[?]: type` signature convention. CreateAlternateAddressBody is a class
// DTO (handlePublicFieldDefinition path); AlternateAddressResponse is an
// interface DTO (emitSchemaMemberFields path). It is the acme-v3 shape that
// surfaced #4868 — the field TYPE came out ” (→ "unknown") and everything was
// marked required because the Java parser misread `name: type`.
func tsFieldShapeFixture() *DashGroup {
	const cfile = "src/address/dto/create-alternate-address.body.ts"
	const ifile = "src/address/dto/alternate-address.response.ts"
	entities := []graph.Entity{
		// Class DTO with three fields: a plain string, a TS-optional string,
		// and a nullable Date union.
		{
			ID: "cls_body", Name: "CreateAlternateAddressBody",
			Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: cfile, Language: "typescript",
		},
		{
			ID: "fld_body_line1", Name: "CreateAlternateAddressBody.line1",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: cfile, Language: "typescript", Signature: "line1: string",
		},
		{
			ID: "fld_body_line2", Name: "CreateAlternateAddressBody.line2",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: cfile, Language: "typescript", Signature: "line2?: string",
		},
		{
			ID: "fld_body_effective", Name: "CreateAlternateAddressBody.effectiveAt",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: cfile, Language: "typescript", Signature: "effectiveAt: Date | null",
		},
		// Interface DTO with one required + one nullable-union field.
		{
			ID: "iface_resp", Name: "AlternateAddressResponse",
			Kind: "SCOPE.Schema", Subtype: "interface",
			SourceFile: ifile, Language: "typescript",
		},
		{
			ID: "fld_resp_id", Name: "AlternateAddressResponse.id",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: ifile, Language: "typescript", Signature: "id: string",
		},
		{
			ID: "fld_resp_archived", Name: "AlternateAddressResponse.archivedAt",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: ifile, Language: "typescript", Signature: "archivedAt: Date | null",
		},
	}
	rels := []graph.Relationship{
		{FromID: "cls_body", ToID: "fld_body_line1", Kind: "CONTAINS"},
		{FromID: "cls_body", ToID: "fld_body_line2", Kind: "CONTAINS"},
		{FromID: "cls_body", ToID: "fld_body_effective", Kind: "CONTAINS"},
		{FromID: "iface_resp", ToID: "fld_resp_id", Kind: "CONTAINS"},
		{FromID: "iface_resp", ToID: "fld_resp_archived", Kind: "CONTAINS"},
	}
	return makePathsTestGroup(entities, rels)
}

// TestShape_TSFieldTypesAndNullable is the #4868 regression: a TS class DTO and
// a TS interface DTO must surface real field types (not ”) and correct
// nullability — TS-optional `?` and `Date | null` unions both → nullable=true,
// while required fields stay nullable=false.
func TestShape_TSFieldTypesAndNullable(t *testing.T) {
	grp := tsFieldShapeFixture()
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	get := func(typeName string) map[string]v2ShapeRow {
		resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/shape?type=" + typeName)
		if err != nil {
			t.Fatalf("GET %s: %v", typeName, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: want 200, got %d", typeName, resp.StatusCode)
		}
		var body struct {
			OK   bool            `json:"ok"`
			Data v2ShapeResponse `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode %s: %v", typeName, err)
		}
		out := map[string]v2ShapeRow{}
		for _, r := range body.Data.Rows {
			out[r.Name] = r
		}
		return out
	}

	// Class DTO.
	cls := get("CreateAlternateAddressBody")
	if r := cls["line1"]; r.Type != "string" || r.Nullable {
		t.Errorf("class line1: want type=string nullable=false, got %+v", r)
	}
	if r := cls["line2"]; r.Type != "string" || !r.Nullable {
		t.Errorf("class line2 (TS-optional): want type=string nullable=true, got %+v", r)
	}
	if r := cls["effectiveAt"]; r.Type != "Date | null" || !r.Nullable {
		t.Errorf("class effectiveAt: want type=Date | null nullable=true, got %+v", r)
	}

	// Interface DTO.
	iface := get("AlternateAddressResponse")
	if r := iface["id"]; r.Type != "string" || r.Nullable {
		t.Errorf("iface id: want type=string nullable=false, got %+v", r)
	}
	if r := iface["archivedAt"]; r.Type != "Date | null" || !r.Nullable {
		t.Errorf("iface archivedAt: want type=Date | null nullable=true, got %+v", r)
	}
}

// TestShape_UnknownType returns 404 when the type token cannot be
// resolved to any class entity in the group.
func TestShape_UnknownType(t *testing.T) {
	grp := shapeTreeFixture()
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/shape?type=NoSuchType")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// TestFindClassEntityByName_ResolvesSchemaKindDTO covers #4569: a NestJS
// response DTO (e.g. ProposalCountsResponse under dto/response/) is indexed as
// kind SCOPE.Schema, NOT SCOPE.Component. The resolver must find it so the
// endpoint's Response row can expand its field-set instead of rendering
// "(none)". Before the fix findClassEntityByName only matched SCOPE.Component.
func TestFindClassEntityByName_ResolvesSchemaKindDTO(t *testing.T) {
	entities := []graph.Entity{
		// A response DTO indexed as a Schema model (the acme-v3 shape).
		{
			ID: "schema_counts", Name: "ProposalCountsResponse",
			Kind: "SCOPE.Schema", Subtype: "model",
			SourceFile: "src/modules/proposals/dto/response/proposal-counts.response.dto.ts",
			Language:   "typescript",
		},
		{
			ID: "fld_total", Name: "ProposalCountsResponse.total",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/modules/proposals/dto/response/proposal-counts.response.dto.ts",
			Language:   "typescript", Signature: "total: number",
		},
		// A Schema FIELD sub-node with a matching simple name must NOT be
		// mistaken for the object shape.
		{
			ID: "fld_decoy", Name: "DecoyShape",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/x.ts", Language: "typescript",
		},
	}
	rels := []graph.Relationship{
		{FromID: "schema_counts", ToID: "fld_total", Kind: "CONTAINS"},
	}
	grp := makePathsTestGroup(entities, rels)

	got := findClassEntityByName(grp, "ProposalCountsResponse")
	if got == nil {
		t.Fatal("#4569: SCOPE.Schema response DTO not resolved by findClassEntityByName")
	}
	if got.ID != "schema_counts" {
		t.Errorf("#4569: resolved %q, want schema_counts", got.ID)
	}
	if !classHasFieldChildren(grp, got) {
		t.Error("#4569: resolved Schema DTO must report field children (CONTAINS field)")
	}
	// A field sub-node must not resolve as an object shape.
	if d := findClassEntityByName(grp, "DecoyShape"); d != nil {
		t.Errorf("#4569: Schema/field sub-node must not resolve as a shape, got %q", d.ID)
	}
}

// TestShape_MissingTypeParam returns 400 when no type/type_entity_id
// query parameter is supplied.
func TestShape_MissingTypeParam(t *testing.T) {
	grp := shapeTreeFixture()
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/shape")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// TestShape_ParseFieldSignature_Annotations isolates the signature
// parser used by buildShapeRow. Verifies multi-annotation handling
// (including args), nullability inference, and primitive detection.
func TestShape_ParseFieldSignature_Annotations(t *testing.T) {
	cases := []struct {
		sig          string
		fieldName    string
		wantType     string
		wantAnnos    []string
		wantOptional bool
	}{
		{"@NotBlank String email", "email", "String", []string{"@NotBlank"}, false},
		{"@Min(0) @NotNull BigDecimal qty", "qty", "BigDecimal", []string{"@Min(0)", "@NotNull"}, false},
		{"int count", "count", "int", nil, false},
		{"@Email @NotNull String addr", "addr", "String", []string{"@Email", "@NotNull"}, false},
		{"List<String> roles", "roles", "List<String>", nil, false},
		// #4868 — JS/TS `name[?]: type` convention. Previously parsed as
		// Java `Type name`, yielding a garbage type ("email?:") → "unknown".
		{"email?: string", "email", "string", nil, true},
		{"count: number", "count", "number", nil, false},
		{"createdAt: Date | null", "createdAt", "Date | null", nil, false},
		{"id: string", "id", "string", nil, false},
		{"meta: Record<string, number>", "meta", "Record<string, number>", nil, false},
		{"readonly name?: string", "name", "string", nil, true},
	}
	for _, c := range cases {
		annos, typ, optional := parseFieldSignature(c.sig, c.fieldName)
		if typ != c.wantType {
			t.Errorf("sig=%q: type want %q got %q", c.sig, c.wantType, typ)
		}
		if !reflect.DeepEqual(annos, c.wantAnnos) {
			t.Errorf("sig=%q: annos want %v got %v", c.sig, c.wantAnnos, annos)
		}
		if optional != c.wantOptional {
			t.Errorf("sig=%q: optional want %v got %v", c.sig, c.wantOptional, optional)
		}
	}
}

// TestShape_UnwrapElementType covers the container-element extraction
// used to resolve `List<Foo>` → `Foo`, `Map<K,V>` → `V`.
func TestShape_UnwrapElementType(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"List<Foo>", "Foo"},
		{"Set<Bar>", "Bar"},
		{"Optional<Baz>", "Baz"},
		{"Map<String, UserDTO>", "UserDTO"},
		{"BigDecimal", "BigDecimal"},
		{"String", "String"},
	}
	for _, c := range cases {
		if got := unwrapElementType(c.in); got != c.out {
			t.Errorf("unwrapElementType(%q): want %q got %q", c.in, c.out, got)
		}
	}
}

// TestShape_InferNullable covers nullable inference precedence.
func TestShape_InferNullable(t *testing.T) {
	cases := []struct {
		annos []string
		typ   string
		want  bool
	}{
		{[]string{"@NotNull"}, "String", false},
		{[]string{"@NotBlank"}, "String", false},
		{[]string{"@Nullable"}, "String", true},
		{nil, "Optional<Foo>", true},
		{nil, "int", false},
		{nil, "String", false},
		// #4868 — TS union nullability.
		{nil, "Date | null", true},
		{nil, "string | undefined", true},
		{nil, "string | number", false},
		{nil, "Array<Foo> | null", true},
	}
	for _, c := range cases {
		if got := inferNullable(c.annos, c.typ, nil); got != c.want {
			t.Errorf("inferNullable(%v,%q)=%v want %v", c.annos, c.typ, got, c.want)
		}
	}
}

// TestShape_FieldValidationsChips covers #4858 — buildShapeRow surfaces the
// class-validator constraints stamped on a field entity (Properties["validations"],
// comma-joined) as the row's Validations slice for the dashboard chips. A field
// without the property yields an empty (nil) slice.
func TestShape_FieldValidationsChips(t *testing.T) {
	grp := &DashGroup{}
	cases := []struct {
		name  string
		props map[string]string
		want  []string
	}{
		{"name", map[string]string{"validations": "IsString,MaxLength:120,IsOptional"},
			[]string{"IsString", "MaxLength:120", "IsOptional"}},
		{"email", map[string]string{"validations": "IsEmail"}, []string{"IsEmail"}},
		{"plain", map[string]string{}, nil},
		{"sloppy", map[string]string{"validations": " IsInt , Min:0 ,"},
			[]string{"IsInt", "Min:0"}},
	}
	for _, c := range cases {
		field :=

			graph.EntityPtr(graph.Entity{
				Name:      "CreateUserDto." + c.name,
				Kind:      "SCOPE.Schema",
				Subtype:   "field",
				Signature: c.name + ": string",
			}.WithProperties(c.props))
		row := buildShapeRow(grp, field)
		if !reflect.DeepEqual(row.Validations, c.want) {
			t.Errorf("field %q: Validations=%v want %v", c.name, row.Validations, c.want)
		}
	}
}

// nestMappedTypeFixture models the live acme-v3 NestJS mapped-type DTO shape:
// CreateThingBody owns its fields, UpdateThingBody (extends PartialType(...))
// owns NONE — its field-set is inherited via the EXTENDS edge that #4845's
// extractor change emits to the base DTO. AdminThingBody adds its own field on
// top of a plain `extends CreateThingBody`.
func nestMappedTypeFixture() *DashGroup {
	const file = "src/thing/dto/thing.dto.ts"
	entities := []graph.Entity{
		{
			ID: "cls_create", Name: "CreateThingBody",
			Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: file, Language: "typescript",
		},
		{
			ID: "fld_create_name", Name: "CreateThingBody.name",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: file, Language: "typescript", Signature: "name: string",
		},
		{
			ID: "fld_create_size", Name: "CreateThingBody.size",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: file, Language: "typescript", Signature: "size: number",
		},
		// Mapped-type DTO — owns NO field entities of its own.
		{
			ID: "cls_update", Name: "UpdateThingBody",
			Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: file, Language: "typescript",
		},
		// Plain-extends DTO that adds one own field.
		{
			ID: "cls_admin", Name: "AdminThingBody",
			Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: file, Language: "typescript",
		},
		{
			ID: "fld_admin_role", Name: "AdminThingBody.role",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: file, Language: "typescript", Signature: "role: string",
		},
	}
	rels := []graph.Relationship{
		{FromID: "cls_create", ToID: "fld_create_name", Kind: "CONTAINS"},
		{FromID: "cls_create", ToID: "fld_create_size", Kind: "CONTAINS"},
		// EXTENDS edges resolved to the base entity ID (the extractor emits a
		// bare-name ToID that the resolver binds; here we model the resolved
		// shape and also exercise the Properties["to"] name fallback).
		graph.Relationship{FromID: "cls_update", ToID: "cls_create", Kind: "EXTENDS"}.WithProperties(map[string]string{"to": "CreateThingBody"}),
		graph.Relationship{FromID: "cls_admin", ToID: "cls_create", Kind: "EXTENDS"}.WithProperties(map[string]string{"to": "CreateThingBody"}),
		{FromID: "cls_admin", ToID: "fld_admin_role", Kind: "CONTAINS"},
	}
	return makePathsTestGroup(entities, rels)
}

// TestShape_MappedTypeInheritsBaseFields proves #4845's dashboard side: a NestJS
// mapped-type DTO that owns no fields of its own still returns its base DTO's
// field rows (and reports has_children=true) by recursing the EXTENDS edge.
func TestShape_MappedTypeInheritsBaseFields(t *testing.T) {
	grp := nestMappedTypeFixture()
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/shape?type=UpdateThingBody")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body struct {
		OK   bool            `json:"ok"`
		Data v2ShapeResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	names := map[string]bool{}
	for _, r := range body.Data.Rows {
		names[r.Name] = true
	}
	if !names["name"] || !names["size"] {
		t.Errorf("mapped-type DTO should inherit base fields name+size, got rows=%+v", body.Data.Rows)
	}

	// classHasFieldChildren must also see through EXTENDS so the path-detail
	// handler renders the expand glyph.
	upd := findClassEntityByName(grp, "UpdateThingBody")
	if upd == nil {
		t.Fatal("UpdateThingBody not resolved")
	}
	if !classHasFieldChildren(grp, upd) {
		t.Error("mapped-type DTO must report has_children=true via inherited fields")
	}
}

// TestShape_PlainExtendsMergesOwnAndInheritedFields proves the additive case: a
// DTO that extends a base AND declares its own field renders both.
func TestShape_PlainExtendsMergesOwnAndInheritedFields(t *testing.T) {
	grp := nestMappedTypeFixture()
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/shape?type=AdminThingBody")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		OK   bool            `json:"ok"`
		Data v2ShapeResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	names := map[string]bool{}
	for _, r := range body.Data.Rows {
		names[r.Name] = true
	}
	for _, want := range []string{"role", "name", "size"} {
		if !names[want] {
			t.Errorf("AdminThingBody should expose field %q, got rows=%+v", want, body.Data.Rows)
		}
	}
}

// nestInterfaceResponseFixture models a NestJS response DTO declared as a
// TypeScript `interface` (SCOPE.Schema/interface owner) that extends a base
// interface — the #4856 shape. AlternateAddressResponse owns one field and
// inherits one from BaseResponse via EXTENDS.
func nestInterfaceResponseFixture() *DashGroup {
	const file = "src/address/dto/alternate-address-response.dto.ts"
	entities := []graph.Entity{
		{
			ID: "iface_base", Name: "BaseResponse",
			Kind: "SCOPE.Schema", Subtype: "interface",
			SourceFile: file, Language: "typescript",
		},
		{
			ID: "fld_base_id", Name: "BaseResponse.id",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: file, Language: "typescript", Signature: "id: string",
		},
		{
			ID: "iface_resp", Name: "AlternateAddressResponse",
			Kind: "SCOPE.Schema", Subtype: "interface",
			SourceFile: file, Language: "typescript",
		},
		{
			ID: "fld_resp_line1", Name: "AlternateAddressResponse.line1",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: file, Language: "typescript", Signature: "line1: string",
		},
		// An object-shaped type alias owner, to prove it expands too.
		{
			ID: "alias_pet", Name: "Pet",
			Kind: "SCOPE.Schema", Subtype: "type_alias",
			SourceFile: file, Language: "typescript",
		},
		{
			ID: "fld_pet_name", Name: "Pet.name",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: file, Language: "typescript", Signature: "name: string",
		},
	}
	rels := []graph.Relationship{
		{FromID: "iface_base", ToID: "fld_base_id", Kind: "CONTAINS"},
		{FromID: "iface_resp", ToID: "fld_resp_line1", Kind: "CONTAINS"},
		graph.Relationship{FromID: "iface_resp", ToID: "iface_base", Kind: "EXTENDS"}.WithProperties(map[string]string{"to": "BaseResponse"}),
		{FromID: "alias_pet", ToID: "fld_pet_name", Kind: "CONTAINS"},
	}
	return makePathsTestGroup(entities, rels)
}

// TestShape_InterfaceResponseExpandsWithInheritedFields proves #4856's dashboard
// side: a NestJS response DTO declared as an `interface` returns non-empty shape
// rows for both its own field and the field inherited via EXTENDS, and reports
// has_children=true. This is the acme-v3 AlternateAddressResponse case that
// previously returned rows:[] / has_children=None.
func TestShape_InterfaceResponseExpandsWithInheritedFields(t *testing.T) {
	grp := nestInterfaceResponseFixture()
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/shape?type=AlternateAddressResponse")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body struct {
		OK   bool            `json:"ok"`
		Data v2ShapeResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data.Rows) == 0 {
		t.Fatal("interface response DTO returned empty shape rows (the #4856 bug)")
	}
	names := map[string]bool{}
	for _, r := range body.Data.Rows {
		names[r.Name] = true
	}
	if !names["line1"] {
		t.Errorf("interface DTO should expose its own field line1, got rows=%+v", body.Data.Rows)
	}
	if !names["id"] {
		t.Errorf("interface DTO should inherit base field id via EXTENDS, got rows=%+v", body.Data.Rows)
	}

	resolved := findClassEntityByName(grp, "AlternateAddressResponse")
	if resolved == nil {
		t.Fatal("AlternateAddressResponse interface not resolved")
	}
	if !classHasFieldChildren(grp, resolved) {
		t.Error("interface response DTO must report has_children=true")
	}
}

// TestShape_TypeAliasObjectExpands proves an object-shaped type alias renders
// its field rows in the dashboard shape endpoint.
func TestShape_TypeAliasObjectExpands(t *testing.T) {
	grp := nestInterfaceResponseFixture()
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/shape?type=Pet")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		OK   bool            `json:"ok"`
		Data v2ShapeResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	names := map[string]bool{}
	for _, r := range body.Data.Rows {
		names[r.Name] = true
	}
	if !names["name"] {
		t.Errorf("object type alias Pet should expose field name, got rows=%+v", body.Data.Rows)
	}
}

// TestFindClassEntityByName_ResolvesComponentTypeAlias covers #7296. A
// language-level type alias is emitted under SCOPE.Schema by Go/Kotlin/Swift/
// TS/Python/Dart, but under SCOPE.Component by four producers spelling the
// subtype three different ways:
//
//	internal/extractors/rust/rust.go buildTypeAlias   "type_alias"
//	internal/patterns/type_alias_extractor.go         "type_alias"
//	internal/extractors/crystal/depth.go              "alias"
//	internal/extractors/elm/extractor.go              "typealias"
//	internal/custom/swift/vapor_extended.go           "typealias"
//
// findClassEntityByName admitted only the Schema spelling, so an alias from
// any of those producers resolved nothing while the Go or TS spelling of the
// same construct resolved. Each of the three Component spellings gets its own
// positive row here, so a mutant dropping one spelling fails a row the other
// two do not cover.
//
// This also grades both ends of the admit-list boundary: the widening must not
// admit the field/column/property sub-node subtypes under Component, and must
// not change what SCOPE.Schema admits — each of Schema's three excluded
// subtypes has its own forbidden row.
func TestFindClassEntityByName_ResolvesComponentTypeAlias(t *testing.T) {
	entities := []graph.Entity{
		// Ordered FIRST on purpose: its name has the rust alias's name as a
		// strict prefix. A match relaxed from equality to HasPrefix returns
		// this instead of the alias, failing the rs_alias assertion below.
		{
			ID: "prefix_decoy", Name: "TransferIdListDecoy",
			Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: "src/x.rs", Language: "rust",
		},
		// rust: `type TransferId = u64;` — no CONTAINS field children.
		{
			ID: "rs_alias", Name: "TransferId",
			Kind: "SCOPE.Component", Subtype: "type_alias",
			SourceFile: "src/domain/ids.rs", Language: "rust",
			Signature: "type TransferId = u64",
		},
		// crystal: `alias UserId = Int64` — SCOPE.Component/"alias".
		{
			ID: "cr_alias", Name: "UserId",
			Kind: "SCOPE.Component", Subtype: "alias",
			SourceFile: "src/domain/ids.cr", Language: "crystal",
			Signature: "alias UserId = Int64",
		},
		// elm / vapor-swift: `type alias Money = Float` —
		// SCOPE.Component/"typealias".
		{
			ID: "elm_alias", Name: "Money",
			Kind: "SCOPE.Component", Subtype: "typealias",
			SourceFile: "src/Domain/Money.elm", Language: "elm",
			Signature: "type alias Money",
		},
		// go/TS spelling of the same construct — the already-accepted end.
		{
			ID: "go_alias", Name: "AccountId",
			Kind: "SCOPE.Schema", Subtype: "type_alias",
			SourceFile: "internal/domain/ids.go", Language: "go",
		},
		// Forbidden rows: sub-field nodes under SCOPE.Component must stay
		// rejected — the widening adds `type_alias` only.
		{
			ID: "cmp_field", Name: "ComponentFieldDecoy",
			Kind: "SCOPE.Component", Subtype: "field",
			SourceFile: "src/x.rs", Language: "rust",
		},
		{
			ID: "cmp_prop", Name: "ComponentPropertyDecoy",
			Kind: "SCOPE.Component", Subtype: "property",
			SourceFile: "src/x.rs", Language: "rust",
		},
		{
			ID: "cmp_col", Name: "ComponentColumnDecoy",
			Kind: "SCOPE.Component", Subtype: "column",
			SourceFile: "src/x.rs", Language: "rust",
		},
		// Forbidden rows on the Schema end: each of the three excluded
		// sub-node subtypes gets its own row, so a mutant that stops
		// excluding one of them fails a row the other two do not cover.
		{
			ID: "sch_field", Name: "SchemaFieldDecoy",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/x.ts", Language: "typescript",
		},
		{
			ID: "sch_col", Name: "SchemaColumnDecoy",
			Kind: "SCOPE.Schema", Subtype: "column",
			SourceFile: "src/x.ts", Language: "typescript",
		},
		{
			ID: "sch_prop", Name: "SchemaPropertyDecoy",
			Kind: "SCOPE.Schema", Subtype: "property",
			SourceFile: "src/x.ts", Language: "typescript",
		},
		// The docstring states the name match is case-sensitive. This decoy
		// differs from the looked-up token only in case, so a match relaxed
		// to EqualFold resolves it and fails the forbidden row below.
		{
			ID: "case_decoy", Name: "casesensitivedecoy",
			Kind: "SCOPE.Component", Subtype: "type_alias",
			SourceFile: "src/x.rs", Language: "rust",
		},
		// The empty-subtype member of the Component admit-list. Every
		// producer that reaches it emits an IMPORT STUB, not a type: 20
		// sites, all inside build*Import* / makeImport* / buildInclude*
		// helpers (go, rust, dart, scala, php, groovy, erlang, elixir,
		// clojure, lisp, pony, lua, shell, assembly, vhdl, verilog), each
		// naming the entity after a module/crate/package top-segment and
		// hanging an IMPORTS edge off it. Modelled faithfully here so the
		// case is pinned by the shape that actually reaches it.
		{
			ID: "import_stub", Name: "serde_json",
			Kind: "SCOPE.Component", Subtype: "",
			SourceFile: "src/main.rs", Language: "rust",
		},
		// The patterns pass (internal/patterns/type_alias_extractor.go) emits
		// SCOPE.Component/type_alias for typescript/javascript/kotlin/scala/
		// rust/go, but names the record `type_alias_<Alias>`. A type
		// reference carries the bare alias name, so this record is not what
		// a lookup for `PatternAlias` finds — pinned below rather than
		// assumed, since the widening makes its Kind/Subtype admissible.
		{
			ID: "pat_alias", Name: "type_alias_PatternAlias",
			Kind: "SCOPE.Component", Subtype: "type_alias",
			SourceFile: "src/x.ts", Language: "typescript",
		},
	}
	grp := makePathsTestGroup(entities, nil)

	got := findClassEntityByName(grp, "TransferId")
	if got == nil {
		t.Fatal("#7296: SCOPE.Component/type_alias (rust) not resolved by findClassEntityByName")
	}
	if got.ID != "rs_alias" {
		t.Errorf("#7296: resolved %q, want rs_alias", got.ID)
	}
	// The alias owns no CONTAINS field child, so the path-detail callers,
	// which gate HasChildren on classHasFieldChildren, report no expandable
	// children for it. Resolution is still what carries TypeEntityID and the
	// type-source location.
	if classHasFieldChildren(grp, got) {
		t.Error("#7296: childless alias must not report field children")
	}

	// Each Component alias spelling resolves on its own row.
	if c := findClassEntityByName(grp, "UserId"); c == nil || c.ID != "cr_alias" {
		t.Errorf(`#7296: SCOPE.Component/"alias" (crystal) must resolve, got %v`, c)
	}
	if e := findClassEntityByName(grp, "Money"); e == nil || e.ID != "elm_alias" {
		t.Errorf(`#7296: SCOPE.Component/"typealias" (elm, vapor-swift) must resolve, got %v`, e)
	}

	// The empty-subtype member resolves. This pins the member, not an
	// endorsement of it: the shape that reaches it is an import stub, and
	// the scan returns the first entity-slice hit, so a stub sharing a name
	// with a real DTO resolves ahead of it whenever extraction happens to
	// emit the stub first. That ordering hazard is tracked separately.
	if st := findClassEntityByName(grp, "serde_json"); st == nil || st.ID != "import_stub" {
		t.Errorf(`#7296: SCOPE.Component with empty Subtype must resolve, got %v`, st)
	}

	// The SCOPE.Schema spelling is unchanged by the widening.
	if s := findClassEntityByName(grp, "AccountId"); s == nil || s.ID != "go_alias" {
		t.Errorf("#7296: SCOPE.Schema/type_alias must still resolve, got %v", s)
	}

	// Forbidden rows — every one of these was rejected before the widening
	// and must remain rejected after it.
	for _, name := range []string{
		"ComponentFieldDecoy",
		"ComponentPropertyDecoy",
		"ComponentColumnDecoy",
		"SchemaFieldDecoy",
		"SchemaColumnDecoy",
		"SchemaPropertyDecoy",
		// Differs from the "casesensitivedecoy" entity only in case.
		"CaseSensitiveDecoy",
		// The patterns pass's record for this alias is named
		// `type_alias_PatternAlias`, so the bare reference name a handler
		// signature carries resolves nothing.
		"PatternAlias",
	} {
		if d := findClassEntityByName(grp, name); d != nil {
			t.Errorf("#7296: %s must not resolve as a shape, got %q (%s/%s)",
				name, d.ID, d.Kind, d.Subtype)
		}
	}
}

// TestFindClassEntityByName_ResolvesScalaComponentSubtypes7314 covers #7314.
// The Scala AST extractor (internal/extractors/scala — default-on, ungated)
// emits SCOPE.Component with subtypes "class", "case_class", "trait" and
// "object" (scala.go:141-161 → buildComponent :775). Before this change only
// "class" was in the Component admit-list, so a handler returning a `case
// class` — the ordinary way to spell a DTO in Scala — resolved no shape
// entity while the same handler returning a plain `class` did.
//
// "case_class" and "trait" each get their own positive row, built from their
// own entity set so a mutant dropping one spelling fails a row the other does
// not cover, and so neither row can take over a kill that belongs to the
// #7296 set (no name here collides with, prefixes or case-folds onto a name
// in that set).
//
// "object" gets a forbidden row: an object name is a term, not a type, in all
// three type-name positions this lookup serves, and a companion object shares
// its case class's Name while this scan returns the first entity-slice hit.
// The row is not vacuous — a mutant adding "object" to the admit-list fails
// it.
func TestFindClassEntityByName_ResolvesScalaComponentSubtypes7314(t *testing.T) {
	entities := []graph.Entity{
		// scala: `case class OrderSummary(id: Int, total: BigDecimal)` —
		// emitScalaCaseClassFields hangs one SCOPE.Schema/field child off the
		// class per class parameter, so this one has field children.
		{
			ID: "sc_case_class", Name: "OrderSummary",
			Kind: "SCOPE.Component", Subtype: "case_class",
			SourceFile: "src/main/scala/orders/OrderSummary.scala", Language: "scala",
			Signature: "case class OrderSummary(id: Int, total: BigDecimal)",
		},
		{
			ID: "sc_case_class_field", Name: "OrderSummary.id",
			Kind: "SCOPE.Schema", Subtype: "field",
			SourceFile: "src/main/scala/orders/OrderSummary.scala", Language: "scala",
		},
		// scala: `trait Shape { … }` — the interface-shaped declaration.
		// Declares no val/var member here, so it owns no field child.
		{
			ID: "sc_trait", Name: "Shape",
			Kind: "SCOPE.Component", Subtype: "trait",
			SourceFile: "src/main/scala/geo/Shape.scala", Language: "scala",
			Signature: "trait Shape",
		},
		// scala: `object Registry { … }` — deliberately NOT admitted.
		{
			ID: "sc_object", Name: "Registry",
			Kind: "SCOPE.Component", Subtype: "object",
			SourceFile: "src/main/scala/reg/Registry.scala", Language: "scala",
			Signature: "object Registry",
		},
	}
	rels := []graph.Relationship{
		{FromID: "sc_case_class", ToID: "sc_case_class_field", Kind: "CONTAINS"},
	}
	grp := makePathsTestGroup(entities, rels)

	cc := findClassEntityByName(grp, "OrderSummary")
	if cc == nil || cc.ID != "sc_case_class" {
		t.Fatalf(`#7314: SCOPE.Component/"case_class" (scala) must resolve, got %v`, cc)
	}
	// The case class's class parameters are CONTAINS field children, so the
	// resolved entity has rows for an expander to open.
	if !classHasFieldChildren(grp, cc) {
		t.Error("#7314: a case class with a class-parameter field child must report field children")
	}

	tr := findClassEntityByName(grp, "Shape")
	if tr == nil || tr.ID != "sc_trait" {
		t.Fatalf(`#7314: SCOPE.Component/"trait" (scala) must resolve, got %v`, tr)
	}
	// A trait declaring no val/var member owns no field child: resolution
	// carries TypeEntityID and the type-source location, and the path-detail
	// callers gate has_children on classHasFieldChildren.
	if classHasFieldChildren(grp, tr) {
		t.Error("#7314: a trait with no val/var member must not report field children")
	}

	// Forbidden row: an `object` is a term, not a type.
	if o := findClassEntityByName(grp, "Registry"); o != nil {
		t.Errorf(`#7314: SCOPE.Component/"object" must not resolve as a shape, got %q (%s/%s)`,
			o.ID, o.Kind, o.Subtype)
	}
}
