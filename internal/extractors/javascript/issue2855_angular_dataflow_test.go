// Package javascript — issue #2855 Angular Data-Flow proving tests.
//
// Flips the four Angular Data-Flow cells (missing→full):
//   - prop_extraction    : @Input()/@Output() fields → component_prop
//   - state_management   : ngrx Store select/dispatch → CALLS Store.*
//   - data_fetching      : HttpClient.get/post/… → data_fetch + CALLS
//   - branch_conditions  : *ngIf / @if / [ngSwitch] template → branch_condition
package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestIssue2855_AngularDataFlow(t *testing.T) {
	src := []byte("import { Component, Input, Output, EventEmitter } from '@angular/core';\n" +
		"import { Store } from '@ngrx/store';\n" +
		"import { HttpClient } from '@angular/common/http';\n" +
		"\n" +
		"@Component({\n" +
		"  selector: 'app-user-card',\n" +
		"  template: '<div *ngIf=\"loaded\"><app-avatar></app-avatar></div>@if (admin) { <span>x</span> }'\n" +
		"})\n" +
		"export class UserCardComponent {\n" +
		"  @Input() userId: string;\n" +
		"  @Input() title = 'User';\n" +
		"  @Output() saved = new EventEmitter<void>();\n" +
		"\n" +
		"  constructor(private store: Store, private http: HttpClient) {}\n" +
		"\n" +
		"  load() {\n" +
		"    this.http.get('/api/users');\n" +
		"    this.http.post('/api/users', {});\n" +
		"    this.store.select(s => s.user);\n" +
		"    this.store.dispatch(loadUser());\n" +
		"  }\n" +
		"}\n")

	ents := extractReact(t, "user-card.component.ts", src)

	comp := findByName(ents, "UserCardComponent")
	if comp == nil {
		t.Fatalf("UserCardComponent not extracted; %s", dumpKinds(ents))
	}

	// prop_extraction: @Input/@Output fields.
	hasProp := func(name, dir string) bool {
		for i := range ents {
			e := &ents[i]
			if e.Subtype == "component_prop" && e.Name == name && e.Properties["prop_direction"] == dir {
				return true
			}
		}
		return false
	}
	if !hasProp("userId", "input") {
		t.Errorf("missing @Input userId; %s", dumpKinds(ents))
	}
	if !hasProp("title", "input") {
		t.Errorf("missing @Input title")
	}
	if !hasProp("saved", "output") {
		t.Errorf("missing @Output saved")
	}

	// data_fetching: HttpClient verbs.
	hasFetch := func(verb string) bool {
		for i := range ents {
			e := &ents[i]
			if e.Subtype == "data_fetch" && e.Properties["http_method"] == verb {
				return true
			}
		}
		return false
	}
	if !hasFetch("get") || !hasFetch("post") {
		t.Errorf("missing HttpClient data_fetch (get/post); %s", dumpKinds(ents))
	}

	// state_management: ngrx Store select + dispatch as CALLS edges.
	if !hasRel(comp.Relationships, "CALLS", "Store.select") {
		t.Errorf("missing CALLS Store.select; rels=%v", comp.Relationships)
	}
	if !hasRel(comp.Relationships, "CALLS", "Store.dispatch") {
		t.Errorf("missing CALLS Store.dispatch")
	}

	// branch_conditions: template *ngIf + @if.
	hasBranch := func(kind string) bool {
		for i := range ents {
			e := &ents[i]
			if e.Subtype == "branch_condition" && e.Properties["branch_kind"] == kind {
				return true
			}
		}
		return false
	}
	if !hasBranch("*ngIf") {
		t.Errorf("missing branch_condition *ngIf; %s", dumpKinds(ents))
	}
	if !hasBranch("@if") {
		t.Errorf("missing branch_condition @if")
	}

	// CONTAINS wiring: component contains its props/fetches/branches.
	var anyProp *types.EntityRecord
	for i := range ents {
		if ents[i].Subtype == "component_prop" && ents[i].Name == "userId" {
			anyProp = &ents[i]
		}
	}
	if anyProp == nil || !hasRel(comp.Relationships, "CONTAINS", anyProp.ID) {
		t.Errorf("component missing CONTAINS → userId prop")
	}
}
