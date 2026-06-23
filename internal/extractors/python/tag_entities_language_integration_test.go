package python_test

// Integration test for issue #2371: verify that TagEntitiesLanguage is called
// by the python extractor so all returned entities have Language="python".
// Before the fix, Language="" was emitted on entities (PR #2365 / issue #2341
// root cause).

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/python"
)

func TestIssue2371_AllEntitiesHaveLanguage_Python(t *testing.T) {
	src := `
class MyView:
    def get(self, request):
        return None

def helper():
    pass
`
	recs := extractPy(t, src, "myapp/views.py")
	if len(recs) == 0 {
		t.Fatal("expected at least one entity from extraction")
	}
	for _, r := range recs {
		if r.Language != "python" {
			t.Errorf("entity %q (kind=%s, src=%s): Language=%q, want %q",
				r.Name, r.Kind, r.SourceFile, r.Language, "python")
		}
		if r.Properties != nil {
			if got := r.Properties["language"]; got != "" && got != "python" {
				t.Errorf("entity %q: Properties[language]=%q, want python or empty", r.Name, got)
			}
		}
	}
}

// TestIssue2371_AllEntitiesHaveLanguage_Django verifies the Django-specific
// path through the python extractor also stamps Language on entities.
func TestIssue2371_AllEntitiesHaveLanguage_DjangoModel(t *testing.T) {
	src := `
from django.db import models

class Article(models.Model):
    title = models.CharField(max_length=200)
    body = models.TextField()
`
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "blog/models.py",
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, r := range recs {
		if r.Language != "python" {
			t.Errorf("entity %q (kind=%s): Language=%q, want python",
				r.Name, r.Kind, r.Language)
		}
	}
}
