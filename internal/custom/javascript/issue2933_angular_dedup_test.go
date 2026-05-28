// Package javascript — issue #2933 Angular dedup regression test.
//
// Phase 0b of the JS/TS audit (#2847) removed the redundant regex-based
// custom_js_angular extractor because it duplicated the richer core
// javascript AST Angular path (internal/extractors/javascript/angular.go).
//
// Before removal, running BOTH paths over the same .ts file produced literal
// entity-ID collisions for @Injectable/@Pipe/guard classes (entity ID =
// sha256(OrgID+ProjectID+SourceFile+Kind+Name); both paths emitted the same
// SCOPE.Component/<ClassName>), plus disjoint-but-redundant representations of
// @Component/@Directive/@NgModule/@Input/@Output/routes.
//
// This test runs the full per-file extraction pipeline the daemon uses (base
// language extractor in Pass 1 + every CustomExtractorsFor("typescript")
// extractor in Pass 2 — see internal/daemon/extract/subproc.go) over an
// Angular fixture and asserts that the merged entity set contains NO duplicate
// IDs. It is a guard against re-introducing a second Angular extractor.
package javascript_test

import (
	"context"
	"sort"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors"
	"github.com/cajasmota/archigraph/internal/types"

	// Blank imports trigger init() registration of both the base javascript
	// AST extractor and every custom_js_* framework extractor.
	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
	_ "github.com/cajasmota/archigraph/internal/extractors/javascript"
)

const angularDedupFixture = `import { Component, Injectable, NgModule, Directive, Pipe, Input, Output, EventEmitter } from '@angular/core';
import { CanActivate, Routes } from '@angular/router';

@Component({ selector: 'app-root', template: '<app-child></app-child>' })
export class AppComponent {
  @Input() title: string;
  @Output() changed = new EventEmitter<string>();
}

@Injectable({ providedIn: 'root' })
export class UserService {}

@Directive({ selector: '[appHighlight]' })
export class HighlightDirective {}

@Pipe({ name: 'cap' })
export class CapPipe {}

@NgModule({ declarations: [AppComponent] })
export class AppModule {}

export class AuthGuard implements CanActivate {
  canActivate() { return true; }
}

const routes: Routes = [{ path: 'home', component: AppComponent }];
`

// extractAngularPipeline mirrors the daemon's per-file passes: base language
// extraction (Pass 1) followed by every matching custom extractor (Pass 2).
// Both contribute independent entities to the graph (subproc.go does NOT call
// MergeWithCustom), so this is the set the downstream ID-keyed store dedups.
func extractAngularPipeline(t *testing.T, path string, src []byte) []types.EntityRecord {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tstsx.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	file := extreg.FileInput{Path: path, Content: src, Language: "typescript", Tree: tree}

	base, ok := extreg.Get("typescript")
	if !ok {
		t.Fatal("base typescript extractor not registered")
	}
	baseEnts, err := base.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("base extract: %v", err)
	}

	customEnts, errs := extractors.RunCustomExtractors(context.Background(), file)
	for _, e := range errs {
		t.Fatalf("custom extract: %v", e)
	}

	all := append([]types.EntityRecord{}, baseEnts...)
	all = append(all, customEnts...)
	// The store keys entities by ComputeID with the entity's SourceFile;
	// make sure SourceFile is populated so IDs are realistic.
	for i := range all {
		if all[i].SourceFile == "" {
			all[i].SourceFile = path
		}
		all[i].ID = all[i].ComputeID()
	}
	return all
}

func TestIssue2933_NoDuplicateAngularEntities(t *testing.T) {
	ents := extractAngularPipeline(t, "app.ts", []byte(angularDedupFixture))

	byID := map[string][]types.EntityRecord{}
	for _, e := range ents {
		byID[e.ID] = append(byID[e.ID], e)
	}

	var dupIDs []string
	for id, group := range byID {
		if len(group) > 1 {
			dupIDs = append(dupIDs, id)
		}
	}
	sort.Strings(dupIDs)

	if len(dupIDs) > 0 {
		for _, id := range dupIDs {
			for _, e := range byID[id] {
				t.Logf("duplicate id=%s kind=%s subtype=%s name=%s", id, e.Kind, e.Subtype, e.Name)
			}
		}
		t.Fatalf("expected zero duplicate Angular entity IDs in the merged graph, got %d colliding id(s): %v", len(dupIDs), dupIDs)
	}

	// Sanity: the core AST path must still actually extract Angular entities,
	// otherwise the "no duplicates" assertion would be vacuously true.
	var sawComponent, sawService bool
	for _, e := range ents {
		if e.Subtype == "angular_component" && e.Name == "AppComponent" {
			sawComponent = true
		}
		if e.Subtype == "angular_service" && e.Name == "UserService" {
			sawService = true
		}
	}
	if !sawComponent || !sawService {
		t.Fatalf("core Angular AST path regressed: sawComponent=%v sawService=%v", sawComponent, sawService)
	}
}

// TestIssue2933_NoCustomAngularExtractorRegistered locks in the removal: no
// extractor keyed custom_js_angular should exist, and the typescript/javascript
// custom set must not contain a separate Angular path.
func TestIssue2933_NoCustomAngularExtractorRegistered(t *testing.T) {
	if _, ok := extreg.Get("custom_js_angular"); ok {
		t.Fatal("custom_js_angular extractor is registered again; the core javascript AST path is the sole Angular extractor (#2933)")
	}
}
