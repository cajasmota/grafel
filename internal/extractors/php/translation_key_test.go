package php_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func phpTransEdge(recs []types.EntityRecord, fromName, key string) bool {
	want := extractor.TranslationKeyTargetID(key)
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindUsesTranslation) && r.ToID == want {
				return true
			}
		}
	}
	return false
}

func phpTransNode(recs []types.EntityRecord, key string) (string, int) {
	want := extractor.TranslationKeyName(key)
	id, count := "", 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTranslationKey) && recs[i].Name == want {
			id = recs[i].ID
			count++
		}
	}
	return id, count
}

func phpAnyTransNode(recs []types.EntityRecord) bool {
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTranslationKey) {
			return true
		}
	}
	return false
}

// Laravel: `__('messages.welcome')` in greet →
// USES_TRANSLATION(Ctrl.greet -> i18n:messages.welcome). Asserts node id + edge.
func TestPHPTrans_LaravelDoubleUnderscore(t *testing.T) {
	src := `<?php
class Ctrl {
    public function greet() {
        return __('messages.welcome');
    }
}
`
	recs := extractPHPRecords(t, src)
	if !phpTransEdge(recs, "Ctrl.greet", "messages.welcome") {
		t.Fatalf("missing USES_TRANSLATION(Ctrl.greet -> messages.welcome)")
	}
	if id, n := phpTransNode(recs, "messages.welcome"); id == "" || n != 1 {
		t.Errorf("expected exactly 1 messages.welcome node, got id=%q n=%d", id, n)
	}
}

// trans('users.title') helper.
func TestPHPTrans_TransHelper(t *testing.T) {
	src := `<?php
function show() {
    return trans('users.title');
}
`
	recs := extractPHPRecords(t, src)
	if !phpTransEdge(recs, "show", "users.title") {
		t.Fatalf("missing USES_TRANSLATION(show -> users.title)")
	}
}

// Convergence: same key in two methods → one node, two edges.
func TestPHPTrans_Convergence(t *testing.T) {
	src := `<?php
class C {
    public function a() { return __('shared.k'); }
    public function b() { return __('shared.k'); }
}
`
	recs := extractPHPRecords(t, src)
	if id, n := phpTransNode(recs, "shared.k"); id == "" || n != 1 {
		t.Fatalf("expected exactly 1 shared.k node, got id=%q n=%d", id, n)
	}
	if !phpTransEdge(recs, "C.a", "shared.k") || !phpTransEdge(recs, "C.b", "shared.k") {
		t.Errorf("expected USES_TRANSLATION from both C.a and C.b")
	}
}

// Negative: dynamic key `__($key)` → no node/edge.
func TestPHPTrans_DynamicKey_Skipped(t *testing.T) {
	src := `<?php
function dyn($key) {
    return __($key);
}
`
	recs := extractPHPRecords(t, src)
	if phpAnyTransNode(recs) {
		t.Fatalf("dynamic key __($key) should NOT create a translation node")
	}
}
