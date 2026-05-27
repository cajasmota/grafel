// Package javascript_test — issue #2665: params_keys property on NAVIGATES_TO
// edges. Builds on #2655/#2658 by capturing the static keys of the `params:`
// object literal as a sorted, deduped JSON array string. This is what unlocks
// "which callers pass param X / which are missing it?" diff queries.
package javascript_test

import (
	"encoding/json"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// findNavParamsKeys returns the params_keys property of the first NAVIGATES_TO
// edge on entity `name`, or "" if no such edge / property exists.
func findNavParamsKeys(ents []types.EntityRecord, name string) string {
	e := findByNameRel(ents, name)
	if e == nil {
		return ""
	}
	for _, r := range e.Relationships {
		if r.Kind == "NAVIGATES_TO" && r.Properties != nil {
			return r.Properties["params_keys"]
		}
	}
	return ""
}

// decodeParamsKeys parses a params_keys JSON array string into a Go slice for
// assertion convenience.
func decodeParamsKeys(t *testing.T, raw string) []string {
	t.Helper()
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("params_keys is not a JSON array: %q (%v)", raw, err)
	}
	return out
}

// TestNavParamsKeys_Shorthand verifies that shorthand `{a, b}` extracts both
// keys, sorted and JSON-encoded.
func TestNavParamsKeys_Shorthand(t *testing.T) {
	src := `
import { useRouter } from "expo-router";

const Comp = () => {
  const router = useRouter();
  const go = (b, a) => {
    router.push({ pathname: '/x', params: { b, a } });
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	got := decodeParamsKeys(t, findNavParamsKeys(ents, "go"))
	want := []string{"a", "b"} // sorted
	if len(got) != len(want) {
		t.Fatalf("params_keys: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("params_keys[%d]: got %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

// TestNavParamsKeys_KeyValue verifies that explicit `{key: value}` form also
// captures keys (not values).
func TestNavParamsKeys_KeyValue(t *testing.T) {
	src := `
const Comp = ({ navigation }) => {
  const go = () => {
    navigation.navigate('Detail', { id: 7, mode: 'edit' });
  };
  return null;
};
`
	// navigation.navigate(name, params) — second-arg style is NOT yet in the
	// extractor's contract (object-form lives on router.push); but the same
	// shape via router.push must work. Use router.push here to keep the test
	// targeted at the params_keys extraction.
	src = `
import { useRouter } from "expo-router";
const Comp = () => {
  const router = useRouter();
  const go = () => {
    router.push({ pathname: '/detail', params: { id: 7, mode: 'edit' } });
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	got := decodeParamsKeys(t, findNavParamsKeys(ents, "go"))
	want := []string{"id", "mode"}
	if len(got) != len(want) {
		t.Fatalf("params_keys: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("params_keys[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestNavParamsKeys_Spread verifies that spread elements are recorded as the
// sentinel "...spread" so callers know dynamic keys may exist.
func TestNavParamsKeys_Spread(t *testing.T) {
	src := `
import { useRouter } from "expo-router";
const Comp = () => {
  const router = useRouter();
  const go = (rest) => {
    router.push({ pathname: '/x', params: { id: 1, ...rest } });
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	got := decodeParamsKeys(t, findNavParamsKeys(ents, "go"))
	foundSpread := false
	foundID := false
	for _, k := range got {
		if k == "...spread" {
			foundSpread = true
		}
		if k == "id" {
			foundID = true
		}
	}
	if !foundSpread {
		t.Errorf("expected ...spread sentinel in params_keys; got %v", got)
	}
	if !foundID {
		t.Errorf("expected 'id' key in params_keys; got %v", got)
	}
}

// TestNavParamsKeys_VariableRef verifies that `params: opts` (variable
// reference, not an object literal) yields an empty JSON array — we
// deliberately do NOT perform data-flow analysis, but we still emit the
// property so callers can distinguish "no params arg" from "dynamic params".
func TestNavParamsKeys_VariableRef(t *testing.T) {
	src := `
import { useRouter } from "expo-router";
const Comp = (opts) => {
  const router = useRouter();
  const go = () => {
    router.push({ pathname: '/x', params: opts });
  };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	raw := findNavParamsKeys(ents, "go")
	if raw != "[]" {
		t.Fatalf("expected params_keys=[] for variable-ref params, got %q", raw)
	}
}

// TestNavParamsKeys_OmittedWhenNoParamsArg verifies that calls without a
// params object (e.g. router.push('/foo') string-only) do NOT carry a
// params_keys property, so the property's presence is a meaningful signal.
func TestNavParamsKeys_OmittedWhenNoParamsArg(t *testing.T) {
	src := `
import { useRouter } from "expo-router";
const Comp = () => {
  const router = useRouter();
  const go = () => { router.push('/foo'); };
  return null;
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	raw := findNavParamsKeys(ents, "go")
	if raw != "" {
		t.Errorf("expected NO params_keys property for string-only push, got %q", raw)
	}
}
