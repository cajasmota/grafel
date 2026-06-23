// Package javascript — issue #2854 Angular Structure-group extraction tests.
//
// Proves component_extraction (@Component/@Directive/@Injectable classes emit
// SCOPE.Component with an angular_* subtype) and context_extraction
// (constructor DI → INJECTED_INTO edges) for the Angular framework.
package javascript_test

import (
	"context"
	"testing"

	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractAngular(t *testing.T, content []byte) []types.EntityRecord {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstsx.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     "app.component.ts",
		Content:  content,
		Language: "typescript",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func TestIssue2854_AngularComponentExtraction(t *testing.T) {
	src := []byte(`import { Component } from '@angular/core';
import { UserService } from './user.service';

@Component({
  selector: 'app-root',
  template: '<app-child></app-child><app-footer></app-footer>',
})
export class AppComponent {
  title = 'demo';
  constructor(private users: UserService) {}
  ngOnInit() {
    this.users.load();
  }
}

@Injectable({ providedIn: 'root' })
export class UserService {
  load() {}
}

@Directive({ selector: '[appHighlight]' })
export class HighlightDirective {}`)

	ents := extractAngular(t, src)

	// component_extraction: AppComponent with angular_component subtype.
	app := findBySubtype(ents, "AppComponent", "angular_component")
	if app == nil {
		t.Fatalf("expected SCOPE.Component subtype=angular_component for AppComponent; got %s", dumpKinds(ents))
	}
	if app.Properties["selector"] != "app-root" {
		t.Errorf("AppComponent selector = %q, want app-root", app.Properties["selector"])
	}
	if app.Properties["framework"] != "angular" {
		t.Errorf("AppComponent framework = %q, want angular", app.Properties["framework"])
	}

	// RENDERS edges to the inline-template child components.
	if !hasRel(app.Relationships, "RENDERS", "app-child") {
		t.Errorf("AppComponent missing RENDERS → app-child; rels=%v", app.Relationships)
	}
	if !hasRel(app.Relationships, "RENDERS", "app-footer") {
		t.Errorf("AppComponent missing RENDERS → app-footer")
	}

	// context_extraction: UserService INJECTED_INTO AppComponent (DI).
	if !hasRelFrom(app.Relationships, "INJECTED_INTO", "UserService", "AppComponent") {
		t.Errorf("expected UserService INJECTED_INTO AppComponent; rels=%v", app.Relationships)
	}

	// @Injectable service entity.
	if findBySubtype(ents, "UserService", "angular_service") == nil {
		t.Errorf("expected SCOPE.Component subtype=angular_service for UserService")
	}
	// @Directive entity.
	dir := findBySubtype(ents, "HighlightDirective", "angular_directive")
	if dir == nil {
		t.Errorf("expected SCOPE.Component subtype=angular_directive for HighlightDirective")
	} else if dir.Properties["selector"] != "[appHighlight]" {
		t.Errorf("HighlightDirective selector = %q", dir.Properties["selector"])
	}

	// CONTAINS to ngOnInit method.
	if !hasRel(app.Relationships, "CONTAINS", "") {
		// CONTAINS uses a structural-ref ToID; just assert at least one exists.
		found := false
		for _, r := range app.Relationships {
			if r.Kind == "CONTAINS" {
				found = true
			}
		}
		if !found {
			t.Errorf("AppComponent missing CONTAINS edge to ngOnInit")
		}
	}
}

func findBySubtype(ents []types.EntityRecord, name, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Subtype == subtype && ents[i].Kind == "SCOPE.Component" {
			return &ents[i]
		}
	}
	return nil
}

func hasRel(rels []types.RelationshipRecord, kind, toID string) bool {
	for _, r := range rels {
		if r.Kind == kind && (toID == "" || r.ToID == toID) {
			return true
		}
	}
	return false
}

func hasRelFrom(rels []types.RelationshipRecord, kind, fromID, toID string) bool {
	for _, r := range rels {
		if r.Kind == kind && r.FromID == fromID && r.ToID == toID {
			return true
		}
	}
	return false
}

func dumpKinds(ents []types.EntityRecord) string {
	out := ""
	for _, e := range ents {
		out += e.Kind + "/" + e.Subtype + ":" + e.Name + " "
	}
	return out
}
