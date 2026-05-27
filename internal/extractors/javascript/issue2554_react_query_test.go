// Package javascript_test — issue #2554: React Query hook bodies must emit
// CALLS edges through inline arrow-function config values (queryFn, mutationFn,
// onSuccess, onError, etc.).
//
// The outer hook call (e.g. useQuery / useMutation) is already recognized.
// What was missing is following into the arrow-function values of the config
// object to emit CALLS from the outer hook entity to inner service/API calls.
package javascript_test

import (
	"testing"
)

// TestTSExtractor_ReactQueryHook_FollowsArrowBody verifies that a useQuery
// call whose queryFn is an inline arrow function emits a CALLS edge from the
// outer hook entity to the inner service call. Edge must carry
// Properties["via"]="react_query_hook". Issue #2554.
func TestTSExtractor_ReactQueryHook_FollowsArrowBody(t *testing.T) {
	src := `
import { useQuery } from "@tanstack/react-query";
import { inspectionsService } from "./inspectionsService";

const useInspections = () => useQuery({
  queryKey: ['inspections'],
  queryFn: () => inspectionsService.getAll(),
});
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// Outer hook entity must exist.
	if findByNameRel(ents, "useInspections") == nil {
		t.Fatal("expected entity 'useInspections' to be emitted")
	}

	// Must emit CALLS from useInspections to getAll (the inner service call).
	if !hasRelEdge(ents, "useInspections", "CALLS", "getAll") {
		// Dump edges for debugging.
		e := findByNameRel(ents, "useInspections")
		if e != nil {
			t.Logf("useInspections relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
		}
		t.Errorf("expected CALLS useInspections→getAll (via react_query_hook), not found")
	}

	// Verify via property is set.
	e := findByNameRel(ents, "useInspections")
	if e != nil {
		found := false
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && r.ToID == "getAll" {
				if r.Properties != nil && r.Properties["via"] == "react_query_hook" {
					found = true
				} else {
					t.Errorf("CALLS useInspections→getAll missing Properties[via]=react_query_hook; got %v", r.Properties)
				}
			}
		}
		if !found {
			// edge not found — already reported above
		}
	}
}

// TestTSExtractor_ReactMutation_FollowsArrowBody verifies that a useMutation
// call whose mutationFn is an inline arrow function emits a CALLS edge from
// the outer hook entity to the inner mutation call. Issue #2554.
func TestTSExtractor_ReactMutation_FollowsArrowBody(t *testing.T) {
	src := `
import { useMutation } from "@tanstack/react-query";
import { inspectionsService } from "./inspectionsService";

const useCreateInspection = () => useMutation({
  mutationFn: (data) => inspectionsService.create(data),
  onSuccess: () => queryClient.invalidateQueries(['inspections']),
  onError: (err) => console.error(err),
});
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if findByNameRel(ents, "useCreateInspection") == nil {
		t.Fatal("expected entity 'useCreateInspection' to be emitted")
	}

	// Must emit CALLS from useCreateInspection to create (mutationFn body).
	if !hasRelEdge(ents, "useCreateInspection", "CALLS", "create") {
		e := findByNameRel(ents, "useCreateInspection")
		if e != nil {
			t.Logf("useCreateInspection relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
		}
		t.Errorf("expected CALLS useCreateInspection→create (via mutationFn), not found")
	}

	// Must also emit CALLS for onSuccess body (invalidateQueries).
	if !hasRelEdge(ents, "useCreateInspection", "CALLS", "invalidateQueries") {
		t.Errorf("expected CALLS useCreateInspection→invalidateQueries (via onSuccess), not found")
	}
}

// TestTSExtractor_NonReactHookNotTraversed verifies that a plain non-hook
// call expression (useState) with a non-React-Query argument does NOT trigger
// the config-object traversal. Issue #2554 — the fix must not over-emit.
func TestTSExtractor_NonReactHookNotTraversed(t *testing.T) {
	src := `
import { useState } from "react";

const useCounter = () => {
  const [count, setCount] = useState(0);
  return { count, setCount };
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if findByNameRel(ents, "useCounter") == nil {
		t.Fatal("expected entity 'useCounter' to be emitted")
	}

	// useState(0) has no object-literal config with React Query keys.
	// No CALLS with via=react_query_hook should appear.
	e := findByNameRel(ents, "useCounter")
	if e != nil {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && r.Properties != nil && r.Properties["via"] == "react_query_hook" {
				t.Errorf("unexpected CALLS with via=react_query_hook on plain useState; got %s→%s", "useCounter", r.ToID)
			}
		}
	}
}
