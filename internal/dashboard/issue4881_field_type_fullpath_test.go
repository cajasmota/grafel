package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors/javascript"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #4881 — FULL-PATH regression: extractor → entity → shape.
//
// The #4868 fixture (TestShape_TSFieldTypesAndNullable) hand-built the field
// SIGNATURE, so it could not catch the real bug: the JS/TS extractor's class
// field path produced a malformed double-colon signature ("building: : number")
// — the type_annotation node text already carries the leading ": ", but the
// emitter prepended another. parseFieldSignature then read back a garbage type,
// so live DTO fields rendered with type="" on the dashboard. (Interface /
// type-alias members went through emitSchemaMemberFields, which already stripped
// the colon, so only class fields regressed.)
//
// This test indexes a REAL TS class DTO and a REAL TS interface DTO through the
// actual javascript extractor, converts the emitted EntityRecords to graph
// entities (the same fields the indexer's entityRecordToGraphEntity copies),
// rewires the extractor's CONTAINS structural-ref edges to the resolved field
// entity IDs (what the resolver does at index time), and asserts the dashboard
// shape endpoint returns rows with a NON-EMPTY type and correct nullability.

const issue4881ClassSrc = `
import { IsInt, IsString } from 'class-validator';

class CreateAlternateAddressBody {
  @IsInt()
  building: number;

  group: number | null;

  @IsString()
  name?: string;
}

interface AlternateAddressResponse {
  id: string;
  archivedAt: Date | null;
  label?: string;
}
`

// extractTSEntities runs the real javascript extractor over TS source.
func extractTSEntities(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tstypescript.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse TS: %v", err)
	}
	ents, err := javascript.New().Extract(context.Background(), extreg.FileInput{
		Path:     issue4881File,
		Content:  []byte(src),
		Language: "typescript",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

const issue4881File = "src/address/dto/alternate-address.ts"

// dashGroupFromRecords converts extractor EntityRecords into a DashGroup,
// resolving the extractor's CONTAINS structural-ref ToIDs to the hex IDs of the
// matching SCOPE.Schema/field entities (mirroring the resolver's structural-ref
// binding so collectShapeRows can walk class → field edges).
func dashGroupFromRecords(recs []types.EntityRecord) *DashGroup {
	// Map field structural-ref → field entity ID.
	refToID := map[string]string{}
	ents := make([]graph.Entity, 0, len(recs))
	for _, r := range recs {
		ents = append(ents, graph.Entity{
			ID:            r.ID,
			Name:          r.Name,
			QualifiedName: r.QualifiedName,
			Kind:          r.Kind,
			Subtype:       r.Subtype,
			SourceFile:    r.SourceFile,
			StartLine:     r.StartLine,
			EndLine:       r.EndLine,
			Language:      r.Language,
			Signature:     r.Signature,
			Properties:    r.Properties,
		})
		if r.Kind == "SCOPE.Schema" && r.Subtype == "field" {
			ref := extreg.BuildSchemaFieldStructuralRef(r.Language, r.SourceFile, r.Name)
			refToID[ref] = r.ID
		}
	}

	var rels []graph.Relationship
	for _, r := range recs {
		for _, rel := range r.Relationships {
			to := rel.ToID
			if id, ok := refToID[to]; ok {
				to = id
			}
			rels = append(rels, graph.Relationship{FromID: r.ID, ToID: to, Kind: rel.Kind})
		}
	}
	return makePathsTestGroup(ents, rels)
}

func TestShape_Issue4881_FieldTypeFromExtractor(t *testing.T) {
	recs := extractTSEntities(t, issue4881ClassSrc)

	// Sanity: the FIELD ENTITY itself must carry a non-empty type signature.
	// This is the extractor → entity half of the assertion.
	wantSig := map[string]string{
		"CreateAlternateAddressBody.building": "building: number",
		"CreateAlternateAddressBody.group":    "group: number | null",
		"CreateAlternateAddressBody.name":     "name?: string",
		"AlternateAddressResponse.id":         "id: string",
		"AlternateAddressResponse.archivedAt": "archivedAt: Date | null",
		"AlternateAddressResponse.label":      "label?: string",
	}
	gotField := map[string]bool{}
	for _, r := range recs {
		if r.Kind != "SCOPE.Schema" || r.Subtype != "field" {
			continue
		}
		gotField[r.Name] = true
		if want, ok := wantSig[r.Name]; ok {
			if r.Signature != want {
				t.Errorf("field %s: entity signature = %q, want %q", r.Name, r.Signature, want)
			}
			if r.Signature == r.Name || r.Signature == "" {
				t.Errorf("field %s: entity signature carries no type (%q)", r.Name, r.Signature)
			}
		}
	}
	for name := range wantSig {
		if !gotField[name] {
			t.Errorf("field entity %q was not emitted by the extractor", name)
		}
	}

	// entity → shape half: drive the real dashboard shape endpoint.
	grp := dashGroupFromRecords(recs)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	get := func(typeName string) map[string]v2ShapeRow {
		t.Helper()
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

	// Class DTO: type must be non-empty; `| null` → nullable, `?` → nullable.
	cls := get("CreateAlternateAddressBody")
	if len(cls) == 0 {
		t.Fatalf("class DTO produced zero shape rows (CONTAINS edges missing?)")
	}
	if r := cls["building"]; r.Type != "number" || r.Nullable {
		t.Errorf("class building: want type=number nullable=false, got %+v", r)
	}
	if r := cls["group"]; r.Type != "number | null" || !r.Nullable {
		t.Errorf("class group: want type='number | null' nullable=true, got %+v", r)
	}
	if r := cls["name"]; r.Type != "string" || !r.Nullable {
		t.Errorf("class name (TS-optional): want type=string nullable=true, got %+v", r)
	}
	// Guard the exact #4881 regression: no field type may be empty.
	for n, r := range cls {
		if r.Type == "" {
			t.Errorf("class field %q rendered with empty type (the #4881 bug)", n)
		}
	}

	// Interface DTO.
	iface := get("AlternateAddressResponse")
	if r := iface["id"]; r.Type != "string" || r.Nullable {
		t.Errorf("iface id: want type=string nullable=false, got %+v", r)
	}
	if r := iface["archivedAt"]; r.Type != "Date | null" || !r.Nullable {
		t.Errorf("iface archivedAt: want type='Date | null' nullable=true, got %+v", r)
	}
	if r := iface["label"]; r.Type != "string" || !r.Nullable {
		t.Errorf("iface label (TS-optional): want type=string nullable=true, got %+v", r)
	}
}
