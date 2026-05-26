package extractor

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// TestTagEntitiesLanguage verifies the parallel-to-TagRelationshipsLanguage
// helper introduced in issue #2371.
func TestTagEntitiesLanguage(t *testing.T) {
	t.Run("sets Language and Properties language on each entity", func(t *testing.T) {
		records := []types.EntityRecord{
			{Name: "A", Kind: "SCOPE.Operation", SourceFile: "a.py"},
			{Name: "B", Kind: "SCOPE.Operation", SourceFile: "b.py"},
		}
		TagEntitiesLanguage(records, "python")
		for _, r := range records {
			if r.Language != "python" {
				t.Errorf("entity %q: Language = %q, want %q", r.Name, r.Language, "python")
			}
			if got := r.Properties["language"]; got != "python" {
				t.Errorf("entity %q: Properties[language] = %q, want %q", r.Name, got, "python")
			}
		}
	})

	t.Run("no-op when entity already has non-empty Language", func(t *testing.T) {
		records := []types.EntityRecord{
			{Name: "A", Kind: "SCOPE.Operation", SourceFile: "a.py", Language: "go"},
		}
		TagEntitiesLanguage(records, "python")
		if records[0].Language != "go" {
			t.Errorf("Language should be preserved: got %q, want %q", records[0].Language, "go")
		}
		// When Language is already set, Properties must remain nil (entire entity is skipped).
		if records[0].Properties != nil {
			t.Errorf("Properties should remain nil when Language is already set, got %v", records[0].Properties)
		}
	})

	t.Run("no-op when lang is empty string", func(t *testing.T) {
		records := []types.EntityRecord{
			{Name: "A", Kind: "SCOPE.Operation", SourceFile: "a.py"},
		}
		TagEntitiesLanguage(records, "")
		if records[0].Language != "" {
			t.Errorf("Language should remain empty, got %q", records[0].Language)
		}
		if records[0].Properties != nil {
			t.Errorf("Properties should remain nil, got %v", records[0].Properties)
		}
	})

	t.Run("empty input slice is a no-op", func(t *testing.T) {
		// Should not panic.
		TagEntitiesLanguage(nil, "python")
		TagEntitiesLanguage([]types.EntityRecord{}, "python")
	})

	t.Run("allocates Properties map lazily", func(t *testing.T) {
		records := []types.EntityRecord{
			{Name: "A", Kind: "SCOPE.Operation", SourceFile: "a.py"},
		}
		if records[0].Properties != nil {
			t.Fatal("precondition: Properties must be nil before tagging")
		}
		TagEntitiesLanguage(records, "rust")
		if records[0].Properties == nil {
			t.Fatal("Properties should be allocated after tagging")
		}
		if got := records[0].Properties["language"]; got != "rust" {
			t.Errorf("Properties[language] = %q, want %q", got, "rust")
		}
	})

	t.Run("preserves existing Properties language override", func(t *testing.T) {
		records := []types.EntityRecord{
			{
				Name:       "A",
				Kind:       "SCOPE.Operation",
				SourceFile: "a.py",
				Properties: map[string]string{"language": "dialect-python"},
			},
		}
		TagEntitiesLanguage(records, "python")
		// Language field was empty, so it gets set.
		if records[0].Language != "python" {
			t.Errorf("Language = %q, want python", records[0].Language)
		}
		// Existing Properties["language"] must be preserved.
		if got := records[0].Properties["language"]; got != "dialect-python" {
			t.Errorf("Properties[language] = %q, want dialect-python", got)
		}
	})
}
