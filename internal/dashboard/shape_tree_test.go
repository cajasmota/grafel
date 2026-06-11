package dashboard

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
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
		// A response DTO indexed as a Schema model (the upvate-v3 shape).
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
		sig       string
		fieldName string
		wantType  string
		wantAnnos []string
	}{
		{"@NotBlank String email", "email", "String", []string{"@NotBlank"}},
		{"@Min(0) @NotNull BigDecimal qty", "qty", "BigDecimal", []string{"@Min(0)", "@NotNull"}},
		{"int count", "count", "int", nil},
		{"@Email @NotNull String addr", "addr", "String", []string{"@Email", "@NotNull"}},
		{"List<String> roles", "roles", "List<String>", nil},
	}
	for _, c := range cases {
		annos, typ := parseFieldSignature(c.sig, c.fieldName)
		if typ != c.wantType {
			t.Errorf("sig=%q: type want %q got %q", c.sig, c.wantType, typ)
		}
		if !reflect.DeepEqual(annos, c.wantAnnos) {
			t.Errorf("sig=%q: annos want %v got %v", c.sig, c.wantAnnos, annos)
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
	}
	for _, c := range cases {
		if got := inferNullable(c.annos, c.typ); got != c.want {
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
		field := &graph.Entity{
			Name:       "CreateUserDto." + c.name,
			Kind:       "SCOPE.Schema",
			Subtype:    "field",
			Signature:  c.name + ": string",
			Properties: c.props,
		}
		row := buildShapeRow(grp, field)
		if !reflect.DeepEqual(row.Validations, c.want) {
			t.Errorf("field %q: Validations=%v want %v", c.name, row.Validations, c.want)
		}
	}
}

// nestMappedTypeFixture models the live upvate-v3 NestJS mapped-type DTO shape:
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
		{FromID: "cls_update", ToID: "cls_create", Kind: "EXTENDS",
			Properties: map[string]string{"to": "CreateThingBody"}},
		{FromID: "cls_admin", ToID: "cls_create", Kind: "EXTENDS",
			Properties: map[string]string{"to": "CreateThingBody"}},
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
		{FromID: "iface_resp", ToID: "iface_base", Kind: "EXTENDS",
			Properties: map[string]string{"to": "BaseResponse"}},
		{FromID: "alias_pet", ToID: "fld_pet_name", Kind: "CONTAINS"},
	}
	return makePathsTestGroup(entities, rels)
}

// TestShape_InterfaceResponseExpandsWithInheritedFields proves #4856's dashboard
// side: a NestJS response DTO declared as an `interface` returns non-empty shape
// rows for both its own field and the field inherited via EXTENDS, and reports
// has_children=true. This is the upvate-v3 AlternateAddressResponse case that
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
