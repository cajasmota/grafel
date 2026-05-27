package main

import (
	"reflect"
	"testing"
)

func TestBucketOf(t *testing.T) {
	cases := []struct {
		category string
		want     string
	}{
		{"http_framework", BucketFrameworks},
		{"web_framework", BucketFrameworks},
		{"graphql", BucketFrameworks},
		{"build_system", BucketTools},
		{"package_manager", BucketTools},
		{"orm", BucketORMs},
		{"migration_tool", BucketORMs},
		{"observability", BucketOther},
		{"message_broker", BucketOther},
		{"language", BucketOther},
		{"unknown_new_category", BucketOther},
		{"", BucketOther},
	}
	for _, c := range cases {
		got := bucketOf(c.category)
		if got != c.want {
			t.Errorf("bucketOf(%q) = %q, want %q", c.category, got, c.want)
		}
	}
}

func TestBucketCapabilityKeys(t *testing.T) {
	// Frameworks: union of http_framework capability keys (only framework
	// category present in the registry today).
	got := bucketCapabilityKeys(BucketFrameworks)
	want := []string{"auth_coverage", "endpoint_synthesis", "handler_attribution", "middleware_coverage"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Frameworks keys = %v, want %v", got, want)
	}

	// ORMs: only `orm` category populated today.
	got = bucketCapabilityKeys(BucketORMs)
	want = []string{"migration_parsing", "model_extraction", "query_attribution"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ORMs keys = %v, want %v", got, want)
	}

	// Tools: union of build_system + package_manager.
	got = bucketCapabilityKeys(BucketTools)
	want = []string{"dependency_graph", "lockfile_parsing", "manifest_parsing", "target_extraction"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tools keys = %v, want %v", got, want)
	}

	// Other: no per-capability columns — digest only.
	if keys := bucketCapabilityKeys(BucketOther); keys != nil {
		t.Errorf("Other keys = %v, want nil", keys)
	}
}

func TestStatusGlyph(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{StatusFull, "✅"},
		{StatusPartial, "⚠️"},
		{StatusMissing, "❌"},
		{StatusNotApplicable, "—"},
		{"", "—"},
		{"bogus", "—"},
	}
	for _, c := range cases {
		if got := statusGlyph(c.in); got != c.want {
			t.Errorf("statusGlyph(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDigestStatus(t *testing.T) {
	// missing beats partial beats full beats n/a.
	caps := map[string]Capability{
		"a": {Status: StatusFull},
		"b": {Status: StatusPartial},
		"c": {Status: StatusMissing},
		"d": {Status: StatusNotApplicable},
	}
	if got := digestStatus(caps); got != StatusMissing {
		t.Errorf("digest = %q, want %q", got, StatusMissing)
	}

	// All full → full.
	caps = map[string]Capability{"a": {Status: StatusFull}, "b": {Status: StatusFull}}
	if got := digestStatus(caps); got != StatusFull {
		t.Errorf("digest = %q, want %q", got, StatusFull)
	}

	// Empty map → "".
	if got := digestStatus(map[string]Capability{}); got != "" {
		t.Errorf("digest empty = %q, want \"\"", got)
	}
}
