package bicep_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/bicep" // trigger init()
	"github.com/cajasmota/archigraph/internal/types"
)

func extractBicep(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("bicep")
	if !ok {
		t.Fatal("bicep extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "bicep",
		// Tree intentionally nil — no bicep grammar is vendored; the extractor
		// is regex/line-based.
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	return recs
}

func findByName(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

func hasEdgeTo(rec *types.EntityRecord, kind, toSuffix string) bool {
	for _, r := range rec.Relationships {
		if r.Kind == kind && len(r.ToID) >= len(toSuffix) && r.ToID[len(r.ToID)-len(toSuffix):] == toSuffix {
			return true
		}
	}
	return false
}

// TestBicep_TwoResources_DependsOn_AndModuleImport is the primary
// value-asserting test required by the ticket: two resources where one
// references the other's symbolic name plus a module. It asserts both resource
// entities (by symbolic name + AzureRP type + Kind), the DEPENDS_ON edge
// between them, and the module IMPORTS edge — NOT len>0.
func TestBicep_TwoResources_DependsOn_AndModuleImport(t *testing.T) {
	const src = `
resource storageAccount 'Microsoft.Storage/storageAccounts@2022-09-01' = {
  name: 'mystg'
  location: 'eastus'
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2022-09-01' = {
  name: '${storageAccount.name}/default'
  properties: {
    foo: storageAccount.id
  }
}

module network './modules/network.bicep' = {
  name: 'net'
  params: {
    sid: storageAccount.id
  }
}
`
	recs := extractBicep(t, src, "infra/main.bicep")

	// --- resource entity 1: storageAccount ---
	sa := findByName(recs, "storageAccount")
	if sa == nil {
		t.Fatal("storageAccount resource entity not found")
	}
	if sa.Kind != "SCOPE.InfraResource" {
		t.Errorf("storageAccount Kind = %q, want SCOPE.InfraResource", sa.Kind)
	}
	if got := sa.Metadata["azure_rp_type"]; got != "Microsoft.Storage/storageAccounts" {
		t.Errorf("storageAccount azure_rp_type = %v, want Microsoft.Storage/storageAccounts", got)
	}
	if got := sa.Metadata["api_version"]; got != "2022-09-01" {
		t.Errorf("storageAccount api_version = %v, want 2022-09-01", got)
	}
	if got := sa.Metadata["deployed_name"]; got != "mystg" {
		t.Errorf("storageAccount deployed_name = %v, want mystg", got)
	}
	// Storage accounts now classify as the more precise "storage" category via
	// the shared cross-tool classifier (#3549); resource_scope aliases it.
	if got := sa.Metadata["resource_category"]; got != "storage" {
		t.Errorf("storageAccount resource_category = %v, want storage", got)
	}
	if got := sa.Metadata["resource_scope"]; got != "storage" {
		t.Errorf("storageAccount resource_scope = %v, want storage", got)
	}

	// --- resource entity 2: blobService, references storageAccount ---
	blob := findByName(recs, "blobService")
	if blob == nil {
		t.Fatal("blobService resource entity not found")
	}
	if blob.Kind != "SCOPE.InfraResource" {
		t.Errorf("blobService Kind = %q, want SCOPE.InfraResource", blob.Kind)
	}
	if got := blob.Metadata["azure_rp_type"]; got != "Microsoft.Storage/storageAccounts/blobServices" {
		t.Errorf("blobService azure_rp_type = %v", got)
	}

	// --- DEPENDS_ON edge: blobService → storageAccount ---
	wantRef := extractor.BuildOperationStructuralRef("bicep", "infra/main.bicep", "storageAccount")
	var found bool
	for _, r := range blob.Relationships {
		if r.Kind == "DEPENDS_ON" && r.ToID == wantRef {
			found = true
		}
	}
	if !found {
		t.Errorf("blobService missing DEPENDS_ON → storageAccount; rels=%+v", blob.Relationships)
	}

	// --- module entity + IMPORTS edge to the .bicep path ---
	mod := findByName(recs, "network")
	if mod == nil {
		t.Fatal("network module entity not found")
	}
	if mod.Kind != "SCOPE.Component" || mod.Subtype != "module" {
		t.Errorf("network module Kind/Subtype = %q/%q, want SCOPE.Component/module", mod.Kind, mod.Subtype)
	}
	if got := mod.QualifiedName; got != "./modules/network.bicep" {
		t.Errorf("network module QualifiedName = %q, want ./modules/network.bicep", got)
	}
	if !hasEdgeTo(mod, "IMPORTS", "./modules/network.bicep") {
		t.Errorf("network module missing IMPORTS edge to ./modules/network.bicep; rels=%+v", mod.Relationships)
	}
	// module also DEPENDS_ON storageAccount (referenced storageAccount.id in params).
	if !hasEdgeTo(mod, "DEPENDS_ON", "storageAccount") {
		t.Errorf("network module missing DEPENDS_ON → storageAccount; rels=%+v", mod.Relationships)
	}
}

// TestBicep_ModuleStackAppTopology is the value-asserting test backing the
// iac_stack_app_topology (#4200) capability credit for Bicep. It drives the
// REAL bicep extractor over a `module` declaration and asserts BOTH halves of
// the module-composition topology: the composition ENTITY (a SCOPE.Component /
// subtype=module node carrying the child module path) and the CONTAINMENT
// relationship (an IMPORTS edge from the file to the referenced child .bicep
// module), distinct from any resource DEPENDS_ON edge.
func TestBicep_ModuleStackAppTopology(t *testing.T) {
	const src = `
module appModule './modules/app.bicep' = {
  name: 'app'
}
`
	recs := extractBicep(t, src, "infra/main.bicep")

	// (1) Composition entity: SCOPE.Component / subtype=module named appModule.
	mod := findByName(recs, "appModule")
	if mod == nil {
		t.Fatalf("expected module composition entity 'appModule', got %+v", recs)
	}
	if mod.Kind != "SCOPE.Component" || mod.Subtype != "module" {
		t.Errorf("expected Kind=SCOPE.Component / Subtype=module, got %q/%q", mod.Kind, mod.Subtype)
	}
	if mod.QualifiedName != "./modules/app.bicep" {
		t.Errorf("expected QualifiedName=./modules/app.bicep, got %q", mod.QualifiedName)
	}

	// (2) Containment relationship: an IMPORTS edge to the child .bicep module.
	if !hasEdgeTo(mod, "IMPORTS", "./modules/app.bicep") {
		t.Errorf("expected IMPORTS containment edge to child module ./modules/app.bicep, got %+v", mod.Relationships)
	}
}

// TestBicep_ExplicitDependsOn covers the dependsOn: [x] array form.
func TestBicep_ExplicitDependsOn(t *testing.T) {
	const src = `
resource vnet 'Microsoft.Network/virtualNetworks@2022-01-01' = {
  name: 'vnet1'
}

resource subnet 'Microsoft.Network/virtualNetworks/subnets@2022-01-01' = {
  name: 'subnet1'
  dependsOn: [
    vnet
  ]
}
`
	recs := extractBicep(t, src, "net.bicep")
	subnet := findByName(recs, "subnet")
	if subnet == nil {
		t.Fatal("subnet not found")
	}
	if !hasEdgeTo(subnet, "DEPENDS_ON", "vnet") {
		t.Errorf("subnet missing explicit DEPENDS_ON → vnet; rels=%+v", subnet.Relationships)
	}
	vnet := findByName(recs, "vnet")
	if got := vnet.Metadata["resource_category"]; got != "network" {
		t.Errorf("vnet resource_category = %v, want network", got)
	}
	if got := vnet.Metadata["resource_scope"]; got != "network" {
		t.Errorf("vnet resource_scope = %v, want network", got)
	}
}

// TestBicep_ParamVarOutput asserts param / var / output entities.
func TestBicep_ParamVarOutput(t *testing.T) {
	const src = `
param storageName string
param location string = 'eastus'
var tags = { env: 'prod' }
output storageId string = sa.id
`
	recs := extractBicep(t, src, "p.bicep")

	if p := findByName(recs, "param.storageName"); p == nil {
		t.Error("param.storageName not found")
	} else if p.Kind != "SCOPE.Schema" || p.Subtype != "param" {
		t.Errorf("param Kind/Subtype = %q/%q", p.Kind, p.Subtype)
	} else if p.Metadata["param_type"] != "string" {
		t.Errorf("param_type = %v, want string", p.Metadata["param_type"])
	}
	if v := findByName(recs, "var.tags"); v == nil || v.Subtype != "var" {
		t.Errorf("var.tags missing/wrong: %+v", v)
	}
	if o := findByName(recs, "output.storageId"); o == nil || o.Subtype != "output" {
		t.Errorf("output.storageId missing/wrong: %+v", o)
	} else if o.Metadata["output_type"] != "string" {
		t.Errorf("output_type = %v, want string", o.Metadata["output_type"])
	}
}

// TestBicep_ExistingAndLoop covers `existing` resources and [for …] loops.
func TestBicep_ExistingAndLoop(t *testing.T) {
	const src = `
resource kv 'Microsoft.KeyVault/vaults@2022-07-01' existing = {
  name: 'shared-kv'
}

resource sas 'Microsoft.Storage/storageAccounts@2022-09-01' = [for name in names: {
  name: name
}]
`
	recs := extractBicep(t, src, "x.bicep")
	kv := findByName(recs, "kv")
	if kv == nil {
		t.Fatal("kv not found")
	}
	if kv.Metadata["existing"] != "true" {
		t.Errorf("kv existing flag = %v, want true", kv.Metadata["existing"])
	}
	sas := findByName(recs, "sas")
	if sas == nil {
		t.Fatal("sas (loop) not found")
	}
	if sas.Metadata["loop"] != "true" {
		t.Errorf("sas loop flag = %v, want true", sas.Metadata["loop"])
	}
}

// TestBicep_Fixture exercises the on-disk testdata fixture end-to-end.
func TestBicep_Fixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "main.bicep"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	recs := extractBicep(t, string(data), "testdata/main.bicep")

	// Resources: storageAccount, blobService, existingVault.
	for _, n := range []string{"storageAccount", "blobService", "existingVault"} {
		if findByName(recs, n) == nil {
			t.Errorf("fixture missing resource %q", n)
		}
	}
	// Module + IMPORTS.
	mod := findByName(recs, "network")
	if mod == nil {
		t.Fatal("fixture missing module network")
	}
	if !hasEdgeTo(mod, "IMPORTS", "./modules/network.bicep") {
		t.Error("fixture module missing IMPORTS edge")
	}
	// blobService DEPENDS_ON storageAccount (via ${storageAccount.name} + dependsOn).
	blob := findByName(recs, "blobService")
	if !hasEdgeTo(blob, "DEPENDS_ON", "storageAccount") {
		t.Error("fixture blobService missing DEPENDS_ON → storageAccount")
	}
	// params/outputs present.
	if findByName(recs, "param.storageName") == nil {
		t.Error("fixture missing param.storageName")
	}
	if findByName(recs, "output.storageId") == nil {
		t.Error("fixture missing output.storageId")
	}
}

// TestBicep_Empty asserts graceful handling of empty input.
func TestBicep_Empty(t *testing.T) {
	recs := extractBicep(t, "", "empty.bicep")
	if len(recs) != 0 {
		t.Errorf("empty input produced %d records, want 0", len(recs))
	}
}
