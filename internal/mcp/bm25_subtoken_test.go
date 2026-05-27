// bm25_subtoken_test.go — tests for identifier subtokenization in BM25 (#2624).
// Covers tokenizeIdentifier unit cases and integration ranking scenarios.
package mcp

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// containsAll checks that every want element appears in got.
func containsAll(got []string, want []string) bool {
	set := make(map[string]bool, len(got))
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

// TestTokenizeIdentifier_CamelCase verifies that a camelCase identifier is
// split into sub-tokens and also returned as a full lowercased token.
func TestTokenizeIdentifier_CamelCase(t *testing.T) {
	t.Parallel()
	got := tokenizeIdentifier("unlockWithBiometrics")
	want := []string{"unlockwithbiometrics", "unlock", "with", "biometrics"}
	if !containsAll(got, want) {
		t.Errorf("tokenizeIdentifier(%q) = %v; want all of %v", "unlockWithBiometrics", got, want)
	}
	// First element must be the full lowercased identifier.
	if len(got) == 0 || got[0] != "unlockwithbiometrics" {
		t.Errorf("first element must be full token; got %v", got)
	}
}

// TestTokenizeIdentifier_SnakeCase verifies snake_case splitting.
func TestTokenizeIdentifier_SnakeCase(t *testing.T) {
	t.Parallel()
	got := tokenizeIdentifier("soft_logout")
	want := []string{"soft_logout", "soft", "logout"}
	if !containsAll(got, want) {
		t.Errorf("tokenizeIdentifier(%q) = %v; want all of %v", "soft_logout", got, want)
	}
	if len(got) == 0 || got[0] != "soft_logout" {
		t.Errorf("first element must be full lowercased token; got %v", got)
	}
}

// TestTokenizeIdentifier_Mixed verifies mixed case + underscore + digit boundary.
// oauth2_Client → full: "oauth2_client", subs: "oauth2" (letter-digit cluster),
// "client" (from PascalCase split at underscore boundary). The digit "2" is
// bundled with "oauth" because letter→digit isn't a split boundary in tokenize;
// the camelCase split fires at the digit→uppercase transition (2→C) giving
// the sub-tokens ["oauth2", "client"].
func TestTokenizeIdentifier_Mixed(t *testing.T) {
	t.Parallel()
	got := tokenizeIdentifier("oauth2_Client")
	// Must contain the full identifier and the camelCase+snake sub-tokens.
	want := []string{"oauth2_client", "oauth2", "client"}
	if !containsAll(got, want) {
		t.Errorf("tokenizeIdentifier(%q) = %v; want all of %v", "oauth2_Client", got, want)
	}
	if len(got) == 0 || got[0] != "oauth2_client" {
		t.Errorf("first element must be full lowercased token; got %v", got)
	}
}

// buildSubtokenDoc creates a small document with camelCase/snake_case entity
// names for integration testing BM25 subtokenization.
func buildSubtokenDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "e_fooBarBaz", Name: "fooBarBaz", Kind: "SCOPE.Function", SourceFile: "pkg/a.go", StartLine: 1},
			{ID: "e_fooQux", Name: "fooQux", Kind: "SCOPE.Function", SourceFile: "pkg/b.go", StartLine: 1},
			{ID: "e_unrelated", Name: "unrelated", Kind: "SCOPE.Function", SourceFile: "pkg/c.go", StartLine: 1},
		},
	}
}

// TestBM25_QueryBiometric_FindsCamelCaseEntities verifies the core bug fix:
// a sub-token query ("bar") must match a camelCase entity ("fooBarBaz"), and
// more specific queries must return the right subset.
func TestBM25_QueryBiometric_FindsCamelCaseEntities(t *testing.T) {
	t.Parallel()
	doc := buildSubtokenDoc()
	idx := BuildBM25(doc)

	// "bar" should match fooBarBaz (sub-token), not fooQux or unrelated.
	hits := idx.Search("bar", 10)
	if len(hits) == 0 {
		t.Fatal("query 'bar' should match 'fooBarBaz' via sub-token; got no hits")
	}
	if hits[0].Entity.ID != "e_fooBarBaz" {
		t.Errorf("query 'bar': top hit should be fooBarBaz, got %s", hits[0].Entity.ID)
	}
	// Ensure fooQux and unrelated are not returned for "bar".
	for _, h := range hits {
		if h.Entity.ID == "e_fooQux" || h.Entity.ID == "e_unrelated" {
			t.Errorf("query 'bar': should not match %s", h.Entity.ID)
		}
	}

	// "foo" should match both fooBarBaz and fooQux (sub-token "foo" in both).
	hits = idx.Search("foo", 10)
	ids := map[string]bool{}
	for _, h := range hits {
		ids[h.Entity.ID] = true
	}
	for _, want := range []string{"e_fooBarBaz", "e_fooQux"} {
		if !ids[want] {
			t.Errorf("query 'foo': expected %s in results; got ids=%v", want, ids)
		}
	}
	if ids["e_unrelated"] {
		t.Errorf("query 'foo': should not match 'unrelated'")
	}

	// "unrelated" should match only e_unrelated.
	hits = idx.Search("unrelated", 10)
	if len(hits) == 0 {
		t.Fatal("query 'unrelated' should have a hit")
	}
	if hits[0].Entity.ID != "e_unrelated" {
		t.Errorf("query 'unrelated': top hit should be e_unrelated, got %s", hits[0].Entity.ID)
	}
}

// TestBM25_FullTokenMatch_OutranksSubToken verifies that an entity whose full
// lowercased name matches the query ranks above entities that only match via
// a sub-token.
//
// Setup:
//   - "unlockWithBiometrics" — full name matches query "unlockWithBiometrics"
//   - "biometricsHelper"     — sub-token "biometrics" matches, but no full match
//   - "unlockDoor"           — sub-token "unlock" matches, but no full match
func TestBM25_FullTokenMatch_OutranksSubToken(t *testing.T) {
	t.Parallel()
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "full", Name: "unlockWithBiometrics", Kind: "SCOPE.Function", SourceFile: "auth/biometrics.go", StartLine: 1},
			{ID: "partial_bio", Name: "biometricsHelper", Kind: "SCOPE.Function", SourceFile: "auth/helper.go", StartLine: 5},
			{ID: "partial_unlock", Name: "unlockDoor", Kind: "SCOPE.Function", SourceFile: "auth/door.go", StartLine: 10},
		},
	}
	idx := BuildBM25(doc)

	// Querying the exact full name should rank the full-match entity first.
	hits := idx.Search("unlockWithBiometrics", 10)
	if len(hits) == 0 {
		t.Fatal("query 'unlockWithBiometrics' should return hits")
	}
	if hits[0].Entity.ID != "full" {
		t.Errorf("full-token match should rank first; top hit = %s (score=%.4f)",
			hits[0].Entity.ID, hits[0].Score)
	}
	// The full match must score strictly higher than any partial match.
	fullScore := hits[0].Score
	for _, h := range hits[1:] {
		if h.Score >= fullScore {
			t.Errorf("entity %s (score=%.4f) should score below full-match (%.4f)",
				h.Entity.ID, h.Score, fullScore)
		}
	}
}
