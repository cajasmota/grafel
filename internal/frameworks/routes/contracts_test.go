package routes

import "testing"

// TestStamp_RailsResources asserts the SPECIFIC per-verb provenance + status a
// Rails `resources :widgets` route carries after T10 synthesis. VALUE-ASSERTING:
// the exact verb/action/provenance/status, not len>0.
func TestStamp_RailsResources(t *testing.T) {
	cases := []struct {
		action   string
		wantVerb string
		wantStat string
		wantErrs string
	}{
		{"index", "GET", "200", ""},
		{"create", "POST", "201", "422"},
		{"show", "GET", "200", "404"},
		{"update", "PATCH", "200", "404,422"},
		{"destroy", "DELETE", "204", "404"},
	}
	for _, tc := range cases {
		v, ok := Lookup(FrameworkRailsResources, tc.action)
		if !ok {
			t.Fatalf("rails action %q: no contract", tc.action)
		}
		if v.HTTPVerb != tc.wantVerb {
			t.Errorf("rails %s: verb=%q want %q", tc.action, v.HTTPVerb, tc.wantVerb)
		}
		props := map[string]string{}
		Stamp(props, FrameworkRailsResources, tc.action)
		if props[PropProvenance] != "framework_synthesized" {
			t.Errorf("rails %s: provenance=%q want framework_synthesized", tc.action, props[PropProvenance])
		}
		if props[PropEffectiveKind] != "synthesized" {
			t.Errorf("rails %s: effective_kind=%q want synthesized", tc.action, props[PropEffectiveKind])
		}
		if props[PropEffectiveAction] != tc.action {
			t.Errorf("rails %s: effective_action=%q", tc.action, props[PropEffectiveAction])
		}
		if props[PropEffectiveStatus] != tc.wantStat {
			t.Errorf("rails %s: effective_status=%q want %q", tc.action, props[PropEffectiveStatus], tc.wantStat)
		}
		if props[PropEffectiveErrors] != tc.wantErrs {
			t.Errorf("rails %s: error_statuses=%q want %q", tc.action, props[PropEffectiveErrors], tc.wantErrs)
		}
		if props[PropDefiningClass] == "" {
			t.Errorf("rails %s: defining_class empty", tc.action)
		}
	}
}

// TestStamp_LaravelResource asserts the Laravel resource controller contract:
// store→201, destroy→204, update→PUT 200, show/index→200.
func TestStamp_LaravelResource(t *testing.T) {
	cases := []struct {
		action   string
		wantVerb string
		wantStat string
	}{
		{"index", "GET", "200"},
		{"store", "POST", "201"},
		{"show", "GET", "200"},
		{"update", "PUT", "200"},
		{"destroy", "DELETE", "204"},
	}
	for _, tc := range cases {
		props := map[string]string{}
		Stamp(props, FrameworkLaravelResource, tc.action)
		if props[PropProvenance] != "framework_synthesized" {
			t.Errorf("laravel %s: provenance=%q", tc.action, props[PropProvenance])
		}
		if props[PropEffectiveStatus] != tc.wantStat {
			t.Errorf("laravel %s: status=%q want %q", tc.action, props[PropEffectiveStatus], tc.wantStat)
		}
		v, _ := Lookup(FrameworkLaravelResource, tc.action)
		if v.HTTPVerb != tc.wantVerb {
			t.Errorf("laravel %s: verb=%q want %q", tc.action, v.HTTPVerb, tc.wantVerb)
		}
	}
	// laravel_api_resource shares the table and resolves the same store→201.
	props := map[string]string{}
	Stamp(props, FrameworkLaravelAPIResrc, "store")
	if props[PropEffectiveStatus] != "201" {
		t.Errorf("laravel_api_resource store: status=%q want 201", props[PropEffectiveStatus])
	}
}

// TestStamp_SpringDataREST asserts the Spring Data REST verb contract:
// create→201, delete→204, get/list/update→200.
func TestStamp_SpringDataREST(t *testing.T) {
	cases := map[string]struct {
		verb, status string
	}{
		"list":   {"GET", "200"},
		"get":    {"GET", "200"},
		"create": {"POST", "201"},
		"update": {"PUT", "200"},
		"delete": {"DELETE", "204"},
	}
	for action, want := range cases {
		v, ok := Lookup(FrameworkSpringDataREST, action)
		if !ok {
			t.Fatalf("spring action %q missing", action)
		}
		if v.HTTPVerb != want.verb {
			t.Errorf("spring %s: verb=%q want %q", action, v.HTTPVerb, want.verb)
		}
		props := map[string]string{}
		Stamp(props, FrameworkSpringDataREST, action)
		if props[PropEffectiveStatus] != want.status {
			t.Errorf("spring %s: status=%q want %q", action, props[PropEffectiveStatus], want.status)
		}
		if props[PropProvenance] != "framework_synthesized" {
			t.Errorf("spring %s: provenance=%q", action, props[PropProvenance])
		}
	}
}

// TestStamp_NestJSCrud asserts the @Crud() 5-route contract:
// createOne→201, the rest→200.
func TestStamp_NestJSCrud(t *testing.T) {
	cases := map[string]struct{ verb, status string }{
		"getMany":   {"GET", "200"},
		"getOne":    {"GET", "200"},
		"createOne": {"POST", "201"},
		"updateOne": {"PATCH", "200"},
		"deleteOne": {"DELETE", "200"},
	}
	for action, want := range cases {
		v, ok := Lookup(FrameworkNestJSCrud, action)
		if !ok {
			t.Fatalf("nestjs action %q missing", action)
		}
		if v.HTTPVerb != want.verb {
			t.Errorf("nestjs %s: verb=%q want %q", action, v.HTTPVerb, want.verb)
		}
		props := map[string]string{}
		Stamp(props, FrameworkNestJSCrud, action)
		if props[PropEffectiveStatus] != want.status {
			t.Errorf("nestjs %s: status=%q want %q", action, props[PropEffectiveStatus], want.status)
		}
	}
}

// TestStamp_HonestPartial_UnknownAction asserts the negative: an action with no
// curated contract still tags provenance (the route IS synthesized) but NEVER
// fabricates a status.
func TestStamp_HonestPartial_UnknownAction(t *testing.T) {
	props := map[string]string{}
	Stamp(props, FrameworkRailsResources, "totally_custom_action")
	if props[PropProvenance] != "framework_synthesized" {
		t.Errorf("provenance=%q want framework_synthesized", props[PropProvenance])
	}
	if _, has := props[PropEffectiveStatus]; has {
		t.Errorf("unknown action must not fabricate a status, got %q", props[PropEffectiveStatus])
	}
}

// TestStamp_NonSynthesizedFramework asserts a non-resource framework key is not
// contracted (no synthesis), so a plain Rails `get 'x', to:'c#a'` route or a
// plain controller is never given a synthesized provenance.
func TestStamp_NonSynthesizedFramework(t *testing.T) {
	if IsSynthesizedFramework("rails") {
		t.Error("plain 'rails' framework must not be a synthesized framework")
	}
	if IsSynthesizedFramework("laravel") {
		t.Error("plain 'laravel' framework must not be a synthesized framework")
	}
	if _, ok := Lookup("rails", "index"); ok {
		t.Error("plain rails framework must have no contract")
	}
}

// TestStamp_BodyOverrideWins asserts a status already resolved from a handler
// body is NOT clobbered by the framework default.
func TestStamp_BodyOverrideWins(t *testing.T) {
	props := map[string]string{PropEffectiveStatus: "418"}
	Stamp(props, FrameworkRailsResources, "create")
	if props[PropEffectiveStatus] != "418" {
		t.Errorf("body override clobbered: status=%q want 418", props[PropEffectiveStatus])
	}
}
