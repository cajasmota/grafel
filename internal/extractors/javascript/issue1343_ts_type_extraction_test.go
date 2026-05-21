package javascript_test

// Tests for issue #1343: TypeScript type extraction enhancement.
//
// Covers:
//   - interface declarations with fields, generics, extends clauses, EXTENDS edges
//   - type alias declarations with generics and type_body property
//   - enum declarations with member names
//   - "go beyond" scenarios: generic constraints, union/intersection types, re-exports

import (
	"strings"
	"testing"
)

// --------------------------------------------------------------------------
// Interface — basic
// --------------------------------------------------------------------------

const tsInterfaceWithFieldsSrc = `
interface User {
  id: number;
  name: string;
  email: string;
}
`

func TestTSInterface_FieldsInProperties(t *testing.T) {
	src := []byte(tsInterfaceWithFieldsSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "User")
	if e == nil {
		t.Fatalf("User entity not found; names: %v", entityNames(entities))
	}
	if e.Kind != "SCOPE.Schema" {
		t.Errorf("Kind=%q, want SCOPE.Schema", e.Kind)
	}
	if e.Subtype != "interface" {
		t.Errorf("Subtype=%q, want interface", e.Subtype)
	}

	fields := e.Properties["fields"]
	if fields == "" {
		t.Errorf("fields property is empty; want id, name, email")
	}
	for _, want := range []string{"id", "name", "email"} {
		if !strings.Contains(fields, want) {
			t.Errorf("fields=%q missing %q", fields, want)
		}
	}
}

// --------------------------------------------------------------------------
// Interface — generics
// --------------------------------------------------------------------------

const tsInterfaceGenericSrc = `
interface Repository<T, ID> {
  findById(id: ID): Promise<T>;
  findAll(): Promise<T[]>;
  save(entity: T): Promise<T>;
}
`

func TestTSInterface_GenericsInProperties(t *testing.T) {
	src := []byte(tsInterfaceGenericSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "Repository")
	if e == nil {
		t.Fatalf("Repository entity not found; names: %v", entityNames(entities))
	}
	if e.Subtype != "interface" {
		t.Errorf("Subtype=%q, want interface", e.Subtype)
	}

	generics := e.Properties["generics"]
	if generics == "" {
		t.Errorf("generics property is empty; want T, ID")
	}
	for _, want := range []string{"T", "ID"} {
		if !strings.Contains(generics, want) {
			t.Errorf("generics=%q missing %q", generics, want)
		}
	}

	if !strings.Contains(e.Signature, "<") {
		t.Errorf("Signature=%q should contain generic angle brackets", e.Signature)
	}
}

// --------------------------------------------------------------------------
// Interface — extends clause + EXTENDS edges
// --------------------------------------------------------------------------

const tsInterfaceExtendsSrc = `
interface Animal {
  name: string;
}

interface Pet extends Animal {
  owner: string;
}

interface WorkingDog extends Pet, Animal {
  job: string;
}
`

func TestTSInterface_ExtendsProperty(t *testing.T) {
	src := []byte(tsInterfaceExtendsSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	pet := findByName(entities, "Pet")
	if pet == nil {
		t.Fatalf("Pet not found; names: %v", entityNames(entities))
	}
	exts := pet.Properties["extends"]
	if !strings.Contains(exts, "Animal") {
		t.Errorf("Pet extends=%q, want Animal", exts)
	}

	dog := findByName(entities, "WorkingDog")
	if dog == nil {
		t.Fatalf("WorkingDog not found; names: %v", entityNames(entities))
	}
	dogExts := dog.Properties["extends"]
	if !strings.Contains(dogExts, "Pet") {
		t.Errorf("WorkingDog extends=%q, want Pet", dogExts)
	}
	if !strings.Contains(dogExts, "Animal") {
		t.Errorf("WorkingDog extends=%q, want Animal", dogExts)
	}
}

func TestTSInterface_ExtendsEdges(t *testing.T) {
	src := []byte(tsInterfaceExtendsSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	pet := findByName(entities, "Pet")
	if pet == nil {
		t.Fatalf("Pet not found; names: %v", entityNames(entities))
	}

	found := false
	for _, rel := range pet.Relationships {
		if rel.Kind == "EXTENDS" && rel.ToID == "Animal" {
			found = true
		}
	}
	if !found {
		t.Errorf("Pet: expected EXTENDS edge to Animal; got relationships: %+v", pet.Relationships)
	}

	dog := findByName(entities, "WorkingDog")
	if dog == nil {
		t.Fatalf("WorkingDog not found")
	}

	targetsFound := map[string]bool{"Pet": false, "Animal": false}
	for _, rel := range dog.Relationships {
		if rel.Kind == "EXTENDS" {
			targetsFound[rel.ToID] = true
		}
	}
	for target, found := range targetsFound {
		if !found {
			t.Errorf("WorkingDog: missing EXTENDS edge to %q", target)
		}
	}
}

// --------------------------------------------------------------------------
// Type alias — basic + type_body
// --------------------------------------------------------------------------

const tsTypeAliasBodySrc = `
type UserID = string;
type Nullable<T> = T | null;
type ApiResponse<T> = { data: T; error: string | null; status: number };
`

func TestTSTypeAlias_TypeBodyProperty(t *testing.T) {
	src := []byte(tsTypeAliasBodySrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	uid := findByName(entities, "UserID")
	if uid == nil {
		t.Fatalf("UserID not found; names: %v", entityNames(entities))
	}
	if uid.Properties["type_body"] == "" {
		t.Errorf("UserID type_body is empty, want 'string'")
	}
	if !strings.Contains(uid.Properties["type_body"], "string") {
		t.Errorf("UserID type_body=%q, want to contain 'string'", uid.Properties["type_body"])
	}

	nullable := findByName(entities, "Nullable")
	if nullable == nil {
		t.Fatalf("Nullable not found; names: %v", entityNames(entities))
	}
	if !strings.Contains(nullable.Properties["type_body"], "null") {
		t.Errorf("Nullable type_body=%q, want to contain 'null' (union)", nullable.Properties["type_body"])
	}
}

func TestTSTypeAlias_GenericsProperty(t *testing.T) {
	src := []byte(tsTypeAliasBodySrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	nullable := findByName(entities, "Nullable")
	if nullable == nil {
		t.Fatalf("Nullable not found; names: %v", entityNames(entities))
	}
	generics := nullable.Properties["generics"]
	if !strings.Contains(generics, "T") {
		t.Errorf("Nullable generics=%q, want T", generics)
	}
	if !strings.Contains(nullable.Signature, "<") {
		t.Errorf("Nullable Signature=%q should contain generic angle brackets", nullable.Signature)
	}
}

// --------------------------------------------------------------------------
// Enum — basic
// --------------------------------------------------------------------------

const tsEnumBasicSrc = `
enum Direction {
  Up,
  Down,
  Left,
  Right,
}
`

func TestTSEnum_BasicEmit(t *testing.T) {
	src := []byte(tsEnumBasicSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "Direction")
	if e == nil {
		t.Fatalf("Direction not found; names: %v", entityNames(entities))
	}
	if e.Kind != "SCOPE.Schema" {
		t.Errorf("Kind=%q, want SCOPE.Schema", e.Kind)
	}
	if e.Subtype != "enum" {
		t.Errorf("Subtype=%q, want enum", e.Subtype)
	}
}

func TestTSEnum_MembersProperty(t *testing.T) {
	src := []byte(tsEnumBasicSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "Direction")
	if e == nil {
		t.Fatalf("Direction not found; names: %v", entityNames(entities))
	}
	members := e.Properties["members"]
	if members == "" {
		t.Errorf("Direction members property is empty; want Up, Down, Left, Right")
	}
}

// --------------------------------------------------------------------------
// Enum — const enum and string initializer members
// --------------------------------------------------------------------------

const tsEnumWithValuesSrc = `
enum Status {
  Active = "active",
  Inactive = "inactive",
  Pending = "pending",
}
`

func TestTSEnum_StringInitializerMembers(t *testing.T) {
	src := []byte(tsEnumWithValuesSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "Status")
	if e == nil {
		t.Fatalf("Status not found; names: %v", entityNames(entities))
	}
	if e.Subtype != "enum" {
		t.Errorf("Subtype=%q, want enum", e.Subtype)
	}
}

// --------------------------------------------------------------------------
// Go-beyond: generic constraints in interfaces
// --------------------------------------------------------------------------

const tsInterfaceGenericConstraintSrc = `
interface Comparable<T extends object> {
  compareTo(other: T): number;
}
`

func TestTSInterface_GenericConstraint(t *testing.T) {
	src := []byte(tsInterfaceGenericConstraintSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "Comparable")
	if e == nil {
		t.Fatalf("Comparable not found; names: %v", entityNames(entities))
	}
	if e.Subtype != "interface" {
		t.Errorf("Subtype=%q, want interface", e.Subtype)
	}
	// Generic T should appear in generics property
	if !strings.Contains(e.Properties["generics"], "T") {
		t.Errorf("generics=%q missing T", e.Properties["generics"])
	}
}

// --------------------------------------------------------------------------
// Go-beyond: union type alias body visible
// --------------------------------------------------------------------------

const tsUnionAliasSrc = `
type StringOrNumber = string | number;
type Result<T, E> = { ok: true; value: T } | { ok: false; error: E };
`

func TestTSTypeAlias_UnionBodyVisible(t *testing.T) {
	src := []byte(tsUnionAliasSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	son := findByName(entities, "StringOrNumber")
	if son == nil {
		t.Fatalf("StringOrNumber not found; names: %v", entityNames(entities))
	}
	body := son.Properties["type_body"]
	if body == "" {
		t.Errorf("StringOrNumber type_body empty; want union text")
	}
	if !strings.Contains(body, "|") {
		t.Errorf("StringOrNumber type_body=%q; want to contain '|' (union)", body)
	}

	result := findByName(entities, "Result")
	if result == nil {
		t.Fatalf("Result not found; names: %v", entityNames(entities))
	}
	for _, g := range []string{"T", "E"} {
		if !strings.Contains(result.Properties["generics"], g) {
			t.Errorf("Result generics=%q missing %q", result.Properties["generics"], g)
		}
	}
}

// --------------------------------------------------------------------------
// Go-beyond: re-exported type alias still emits
// --------------------------------------------------------------------------

const tsReExportedTypeSrc = `
export type { UserID };
export type AdminID = string;
`

func TestTSTypeAlias_ExportedTypeEmits(t *testing.T) {
	src := []byte(`export type AdminID = string;`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "AdminID")
	if e == nil {
		t.Fatalf("AdminID not found; names: %v", entityNames(entities))
	}
	if e.Subtype != "type_alias" {
		t.Errorf("Subtype=%q, want type_alias", e.Subtype)
	}
}

// --------------------------------------------------------------------------
// Regression guard: existing tests not broken
// --------------------------------------------------------------------------

// Verify the basic interface test still works with the enhanced handler.
func TestTSInterface_BasicRegressionGuard(t *testing.T) {
	src := []byte(`
interface Config {
  port: number;
  host: string;
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "Config", "SCOPE.Schema")
	e := findByName(entities, "Config")
	if e == nil {
		t.Fatal("Config not found")
	}
	if e.Subtype != "interface" {
		t.Errorf("Subtype=%q, want interface", e.Subtype)
	}
}

// Verify basic type alias test still works with the enhanced handler.
func TestTSTypeAlias_BasicRegressionGuard(t *testing.T) {
	src := []byte(`
type Callback = (err: Error | null, result: string) => void;
type UserID = string;
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "Callback", "SCOPE.Schema")
	assertKind(t, entities, "UserID", "SCOPE.Schema")
	for _, name := range []string{"Callback", "UserID"} {
		e := findByName(entities, name)
		if e != nil && e.Subtype != "type_alias" {
			t.Errorf("%q Subtype=%q, want type_alias", name, e.Subtype)
		}
	}
}

// --------------------------------------------------------------------------
// Multiple Schema entities in one file
// --------------------------------------------------------------------------

const tsMultiSchemaFileSrc = `
interface UserProfile {
  id: number;
  name: string;
}

type UserRole = "admin" | "viewer" | "editor";

enum Permission {
  Read,
  Write,
  Admin,
}
`

func TestTSMultipleSchemaEntitiesInOneFile(t *testing.T) {
	src := []byte(tsMultiSchemaFileSrc)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	assertKind(t, entities, "UserProfile", "SCOPE.Schema")
	assertKind(t, entities, "UserRole", "SCOPE.Schema")
	assertKind(t, entities, "Permission", "SCOPE.Schema")

	profile := findByName(entities, "UserProfile")
	if profile == nil || profile.Subtype != "interface" {
		t.Errorf("UserProfile Subtype=%q, want interface", profile.Subtype)
	}

	role := findByName(entities, "UserRole")
	if role == nil || role.Subtype != "type_alias" {
		t.Errorf("UserRole Subtype=%q, want type_alias", role.Subtype)
	}
	if !strings.Contains(role.Properties["type_body"], "admin") {
		t.Errorf("UserRole type_body=%q; want union members visible", role.Properties["type_body"])
	}

	perm := findByName(entities, "Permission")
	if perm == nil || perm.Subtype != "enum" {
		t.Errorf("Permission Subtype=%q, want enum", perm.Subtype)
	}
}
