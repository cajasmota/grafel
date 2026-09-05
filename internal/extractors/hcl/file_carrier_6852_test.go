package hcl_test

// file_carrier_6852_test.go — #6852, terraform arm. emitFileLevelRelationships
// (relationships.go) anchors IMPORTS on the .tf file's own path at TWO sites —
// the `module` branch (`source = "..."`) and the `provider` branch — while the
// file-level SCOPE.Component it returns is named the BASENAME, not the path.
// internal/resolve/refs.go has no path→entity index, so a path-valued FromID
// resolves if and only if some emitted record carries that exact string as its
// Name. For any nested .tf nothing does, and the raw path reached the graph as
// the edge's FROM end. Same defect #6815 fixed in erlang, nim and groovy and
// #6852/#6864 fixed in bicep; same fix, the CONDITIONAL carrier in
// internal/extractor/file_carrier.go.
//
// ONE CARRIER SERVES BOTH SITES, AND THAT IS A MEASURED CLAIM, NOT A GUESS.
// Both sites live in the same function, both anchor on the same `path` value,
// and both hang off the same file-level record — so the resolution requirement
// is one string, not two. TestTerraform_ImportsFromEndResolves_6852 drives the
// two sites SEPARATELY (module-only and provider-only fixtures) precisely so a
// carrier wired to one branch could not pass by riding on the other, and
// TestTerraform_OneCarrierPerFileNotPerImport_6852 pins that four anchored
// edges across BOTH sites still yield exactly one carrier.
//
// PATH DEPTH IS THE LOAD-BEARING AXIS HERE, unlike bicep. The pre-existing file
// component's Name is basename(path), so at a ROOT path ("main.tf") it already
// equals the path and the FROM end already resolved — by accident, the same
// accident #6367 documents for CONTAINS. The two depths therefore pin different
// halves:
//
//   - NESTED ("infra/envs/prod/main.tf") is the only depth that can fail before
//     the fix; it is the pin on the fix itself.
//   - ROOT ("main.tf") is the only depth that exercises FileCarrierFor clause 3
//     ("no record is ALREADY named path"). Without that clause a second
//     SCOPE.Component/file would be minted under the same name and the rewrite
//     target would be ambiguous. A nested-only test cannot see that.
//
// Neither depth may be dropped "to simplify".
//
// BOTH LANGUAGE TOKENS. internal/extractors/hcl registers "hcl" AND
// "terraform" against ONE HCLExtractor with one Extract method, so the carrier
// is not token-conditional and must not be made so: a token-scoped branch would
// leave every .hcl file's IMPORTS dangling for no stated reason. The ledger
// (#6847) names only terraform because the corpus never drives an .hcl file —
// "hcl" is one of the 24 unexercised base languages listed in
// internal/extractors/file_anchored_carrier_runtime_6847_test.go. Being broad
// is safe BECAUSE the carrier is conditional: an .hcl file with no
// module/provider block anchors nothing and gains nothing.
// TestHCLToken_ImportsFromEndResolves_6852 and
// TestHCLToken_NoCarrierWithoutAPathAnchoredImport_6852 grade both directions
// under the other token.
//
// GRADED IN BOTH DIRECTIONS. A recall-shaped assertion ("the carrier exists")
// licenses an UNCONDITIONAL carrier, which would mint one bare orphan node per
// .tf file across a whole repo — invisible to every such assertion. The
// forbidden-row controls below are what forbid it.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// tfNamedExactly6852 returns every record whose Name or QualifiedName is path.
// This is the resolution question refs.go actually asks — it has no
// path→entity index — so it is the forbidden-row form: a carrier smuggled in
// under a different Kind or Subtype is caught too.
func tfNamedExactly6852(recs []types.EntityRecord, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Name == path || r.QualifiedName == path {
			out = append(out, r)
		}
	}
	return out
}

// tfPathAnchored6852 returns every relationship in recs whose FromID is exactly
// path — the shape whose FROM end has nothing to resolve onto.
func tfPathAnchored6852(recs []types.EntityRecord, path string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.FromID == path {
				out = append(out, r)
			}
		}
	}
	return out
}

// moduleSrc6852 exercises SITE ONE ONLY: relationships.go's `module` branch,
// `FromID: path` on the module `source` IMPORTS. No provider block, so a fix
// wired to the provider branch alone cannot pass this.
const moduleSrc6852 = `
module "vpc" {
  source = "terraform-aws-modules/vpc/aws"
}
`

// providerSrc6852 exercises SITE TWO ONLY: relationships.go's `provider`
// branch, `FromID: path` on the provider-name IMPORTS. No module block, so a
// fix wired to the module branch alone cannot pass this.
const providerSrc6852 = `
provider "aws" {
  region = "us-east-1"
}
`

// resolveTF6852 extracts src at path under a language token, stamps ids the way
// graph assembly does, runs the production resolver pipeline, and returns the
// records plus the id→record index. The assertion is on the EMITTED ARTEFACT
// after resolution — the edge's FROM end — not on a helper's return value or a
// counter the code keeps about itself.
func resolveTF6852(t *testing.T, extract func(string, string) ([]types.EntityRecord, error), src, path string) ([]types.EntityRecord, map[string]*types.EntityRecord) {
	t.Helper()
	recs, err := extract(src, path)
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	if len(recs) == 0 {
		t.Fatalf("extract %s: no records", path)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6852", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))
	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	return recs, byID
}

// assertImportsResolve6852 fails for every IMPORTS edge whose FROM end names no
// record, and fails vacuously-empty fixtures.
func assertImportsResolve6852(t *testing.T, recs []types.EntityRecord, byID map[string]*types.EntityRecord) {
	t.Helper()
	seen := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			seen++
			if _, ok := byID[r.FromID]; !ok {
				t.Errorf("IMPORTS owned by %q: FROM end %q resolves to no record "+
					"(refs.go has no path→entity index; a path-valued FromID resolves "+
					"iff some record carries that exact string as its Name — emit a file "+
					"carrier, internal/extractor/file_carrier.go)", recs[i].Name, r.FromID)
			}
		}
	}
	if seen == 0 {
		t.Fatal("fixture produced no IMPORTS edges — this measurement is vacuous")
	}
}

// TestTerraform_ImportsFromEndResolves_6852 is the fix's behavioural test,
// driven over the CROSS PRODUCT of the two anchoring sites and the two path
// depths. Axes VARIED: anchoring site (module source / provider name) and path
// depth (nested / root). HELD CONSTANT: one block per fixture, the terraform
// token, the same resolver pipeline.
func TestTerraform_ImportsFromEndResolves_6852(t *testing.T) {
	sites := map[string]string{
		"module_source": moduleSrc6852,
		"provider_name": providerSrc6852,
	}
	for site, src := range sites {
		for _, path := range []string{"infra/envs/prod/main.tf", "main.tf"} {
			t.Run(site+"/"+path, func(t *testing.T) {
				recs, byID := resolveTF6852(t, extractTerraform, src, path)
				assertImportsResolve6852(t, recs, byID)
			})
		}
	}
}

// TestTerraform_NoCarrierWithoutAPathAnchoredImport_6852 is the OVER-FIRING
// control, and it is the half of the grade a "the edge now resolves" test
// cannot supply. Axis VARIED: the presence of any module/provider block
// (absent). HELD CONSTANT: a full set of other declarations — locals, two
// resources with a depends_on, a variable and an output — so the file still
// extracts a full record set AND still emits the file-level component with its
// CONTAINS edges. Only the path-anchored edge is gone.
func TestTerraform_NoCarrierWithoutAPathAnchoredImport_6852(t *testing.T) {
	const src = `
locals {
  prefix = "x"
}

variable "region" {
  type = string
}

resource "aws_iam_role" "r" {}

resource "aws_s3_bucket" "b" {
  depends_on = [aws_iam_role.r]
}

output "bucket" {
  value = aws_s3_bucket.b.id
}
`
	// At a NESTED path no record may be named after the file at all. At a ROOT
	// path the pre-existing file-level component is ALREADY named "main.tf"
	// (its Name is the basename), so exactly one record answers to the path and
	// a carrier would make two. Both are forbidden-row assertions; the counts
	// differ because the baseline does.
	cases := []struct {
		path      string
		wantNamed int
	}{
		{"infra/envs/prod/main.tf", 0},
		{"main.tf", 1},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			recs, err := extractTerraform(src, tc.path)
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			if len(recs) == 0 {
				t.Fatal("premise: fixture produced no records at all")
			}
			if n := len(tfPathAnchored6852(recs, tc.path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
			}
			got := tfNamedExactly6852(recs, tc.path)
			if len(got) != tc.wantNamed {
				t.Errorf("a .tf file with nothing to carry must not gain a file carrier: "+
					"%d records answer to %q, want %d — an unconditional carrier mints one "+
					"bare orphan node per .tf file across a whole repo, which no "+
					"recall-shaped assertion can see", len(got), tc.path, tc.wantNamed)
				for _, r := range got {
					t.Errorf("  named %q: kind=%q subtype=%q name=%q qname=%q",
						tc.path, r.Kind, r.Subtype, r.Name, r.QualifiedName)
				}
			}
		})
	}
}

// TestTerraform_EmptyFileGetsNoCarrier_6852 is the second over-firing control,
// on the OTHER shape Extract can return: a .tf file with no top-level blocks at
// all makes emitFileLevelRelationships return nil, so there is no file-level
// component and no relationship of any kind. Nothing to anchor, nothing to
// carry — and an unconditional carrier would still mint a node here.
func TestTerraform_EmptyFileGetsNoCarrier_6852(t *testing.T) {
	const src = "# just a comment\n"
	for _, path := range []string{"infra/envs/prod/main.tf", "main.tf"} {
		t.Run(path, func(t *testing.T) {
			recs, err := extractTerraform(src, path)
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			if n := len(tfPathAnchored6852(recs, path)); n != 0 {
				t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
			}
			if n := len(tfNamedExactly6852(recs, path)); n != 0 {
				t.Errorf("a block-less .tf file must gain no record named %q, got %d", path, n)
			}
		})
	}
}

// TestTerraform_OneCarrierPerFileNotPerImport_6852 is the third over-firing
// control and the direct answer to "one carrier or two?". Axis VARIED: the
// NUMBER of path-anchored edges (four) AND the fact that they come from BOTH
// sites (two module sources, two provider names). HELD CONSTANT: one file, one
// nested path. The carrier is per-FILE, not per-EDGE and not per-SITE; a
// per-edge or per-site carrier would put several nodes under one id.
func TestTerraform_OneCarrierPerFileNotPerImport_6852(t *testing.T) {
	const src = `
provider "aws" {
  region = "us-east-1"
}

provider "google" {
  project = "p"
}

module "vpc" {
  source = "terraform-aws-modules/vpc/aws"
}

module "db" {
  source = "../../modules/db"
}
`
	const path = "infra/envs/prod/main.tf"
	recs, err := extractTerraform(src, path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if n := len(tfPathAnchored6852(recs, path)); n != 4 {
		t.Fatalf("premise: want 4 path-anchored IMPORTS edges (2 module + 2 provider), got %d", n)
	}
	if n := len(tfNamedExactly6852(recs, path)); n != 1 {
		t.Errorf("4 path-anchored imports across BOTH sites must still yield exactly 1 "+
			"record named %q, got %d", path, n)
	}
}

// TestTerraform_RootPathGetsNoSecondCarrier_6852 is the fourth over-firing
// control and the one that exercises FileCarrierFor clause 3. At a root path
// the pre-existing file-level component (relationships.go, Name = basename) is
// ALREADY named "main.tf", so the anchored IMPORTS already resolved and no
// carrier is due. Emitting one anyway would put two SCOPE.Component/file nodes
// under one id and make the rewrite target ambiguous.
func TestTerraform_RootPathGetsNoSecondCarrier_6852(t *testing.T) {
	const path = "main.tf"
	for site, src := range map[string]string{
		"module_source": moduleSrc6852,
		"provider_name": providerSrc6852,
	} {
		t.Run(site, func(t *testing.T) {
			recs, err := extractTerraform(src, path)
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			if n := len(tfPathAnchored6852(recs, path)); n != 1 {
				t.Fatalf("premise: want 1 path-anchored IMPORTS edge, got %d", n)
			}
			got := tfNamedExactly6852(recs, path)
			if len(got) != 1 {
				t.Errorf("exactly 1 record may answer to %q at a root path (the file-level "+
					"component relationships.go already emits), got %d", path, len(got))
				for _, r := range got {
					t.Errorf("  kind=%q subtype=%q name=%q qname=%q quality=%v",
						r.Kind, r.Subtype, r.Name, r.QualifiedName, r.QualityScore)
				}
			}
		})
	}
}

// TestTerraform_CarrierShape_6852 pins what the carrier IS: stamped with the
// extractor's language token, anchored on the file it names, first in the
// record slice (#577), and owning NO relationships of its own — the file-level
// component still carries the IMPORTS edges, so re-homing them onto the carrier
// would double every edge.
//
// The Language assertion is load-bearing and not decorative: hcl runs no
// extractor.TagEntitiesLanguage, so the carrier keeps whatever token
// PrependFileCarrier is handed. A wrong or empty token would survive every
// other test in this file.
func TestTerraform_CarrierShape_6852(t *testing.T) {
	const path = "infra/envs/prod/main.tf"
	recs, err := extractTerraform(moduleSrc6852, path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	cs := tfNamedExactly6852(recs, path)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	c := cs[0]
	if c.Kind != "SCOPE.Component" || c.Subtype != "file" {
		t.Errorf("carrier kind/subtype = %q/%q, want %q/%q", c.Kind, c.Subtype, "SCOPE.Component", "file")
	}
	if c.Language != "terraform" {
		t.Errorf("carrier Language = %q, want %q", c.Language, "terraform")
	}
	if c.SourceFile != path {
		t.Errorf("carrier SourceFile = %q, want %q", c.SourceFile, path)
	}
	if n := len(c.Relationships); n != 0 {
		t.Errorf("the terraform file carrier must own no relationships, got %d", n)
	}
	if n := len(tfPathAnchored6852(recs, path)); n != 1 {
		t.Errorf("the module IMPORTS edge must still be emitted exactly once, got %d", n)
	}
	// #577 convention: the file entity is the FIRST record. python's
	// re_exports.go and prune_import_placeholders.go both rely on index 0.
	if recs[0].Name != path {
		t.Errorf("carrier must be record 0 (#577 convention), got %q at index 0", recs[0].Name)
	}
}

// TestHCLToken_ImportsFromEndResolves_6852 grades the OTHER registered token.
// internal/extractors/hcl registers "hcl" and "terraform" against one Extract
// method; the ledger names only terraform because the corpus drives no .hcl
// file. Axis VARIED: the language token. HELD CONSTANT: the fixture, the path,
// the pipeline. If someone ever makes the carrier token-conditional, this is
// what reports it.
func TestHCLToken_ImportsFromEndResolves_6852(t *testing.T) {
	const path = "infra/envs/prod/main.hcl"
	recs, byID := resolveTF6852(t, extractHCL, moduleSrc6852, path)
	assertImportsResolve6852(t, recs, byID)
	cs := tfNamedExactly6852(recs, path)
	if len(cs) != 1 {
		t.Fatalf("want exactly 1 record named %q, got %d", path, len(cs))
	}
	if cs[0].Language != "hcl" {
		t.Errorf("carrier Language = %q, want %q — the token comes from the extractor "+
			"instance, and hcl runs no TagEntitiesLanguage to repair it", cs[0].Language, "hcl")
	}
}

// TestHCLToken_NoCarrierWithoutAPathAnchoredImport_6852 is the over-firing
// direction under the "hcl" token. Widening a fix into a language nobody
// measured is only safe because the carrier is CONDITIONAL; this is the row
// that says so.
func TestHCLToken_NoCarrierWithoutAPathAnchoredImport_6852(t *testing.T) {
	const src = `
resource "aws_iam_role" "r" {}
`
	const path = "infra/envs/prod/main.hcl"
	recs, err := extractHCL(src, path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("premise: fixture produced no records at all")
	}
	if n := len(tfPathAnchored6852(recs, path)); n != 0 {
		t.Fatalf("premise: want 0 path-anchored relationships, got %d", n)
	}
	if n := len(tfNamedExactly6852(recs, path)); n != 0 {
		t.Errorf("an .hcl file with nothing to carry must gain no record named %q, got %d", path, n)
	}
}
