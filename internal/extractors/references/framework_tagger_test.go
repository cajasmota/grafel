package references_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsjava "github.com/smacker/go-tree-sitter/java"
	tsjs "github.com/smacker/go-tree-sitter/javascript"
	tspython "github.com/smacker/go-tree-sitter/python"
	tsruby "github.com/smacker/go-tree-sitter/ruby"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors/references"
	"github.com/cajasmota/archigraph/internal/types"
)

func extractWithTagger(t *testing.T, lang string, grammar *sitter.Language, src, path string, tagger references.FrameworkTagger) []types.EntityRecord {
	t.Helper()
	content := []byte(src)
	p := sitter.NewParser()
	p.SetLanguage(grammar)
	tree, err := p.ParseCtx(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	ext := references.NewReferenceExtractor()
	ext.FrameworkTagger = tagger
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  content,
		Language: lang,
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract err: %v", err)
	}
	return recs
}

func findTagged(recs []types.EntityRecord, framework string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Properties["framework"] == framework {
			out = append(out, r)
		}
	}
	return out
}

func TestDefaultTaggerNonFrameworkFilesAreUntouched(t *testing.T) {
	tagger := references.DefaultTagger()
	src := `def plain(x):
    return x + 1

plain(7)
`
	recs := extractWithTagger(t, "python", tspython.GetLanguage(), src, "plain.py", tagger)
	for _, r := range recs {
		if r.Properties["framework"] != "" {
			t.Errorf("expected no framework tag on plain file, got %q", r.Properties["framework"])
		}
	}
}

func TestDjangoORMTagger_TagsWriteAndRead(t *testing.T) {
	tagger := references.DefaultTagger()
	src := `from django.db import models

class User(models.Model):
    pass

def run(u):
    u.save()
    User.objects.filter(id=1)
`
	recs := extractWithTagger(t, "python", tspython.GetLanguage(), src, "views.py", tagger)
	tagged := findTagged(recs, "django_orm")
	if len(tagged) == 0 {
		t.Fatalf("expected at least one django_orm tagged reference")
	}

	var sawWrite, sawRead bool
	for _, r := range tagged {
		switch r.Properties["semantic_tag"] {
		case "database_write":
			sawWrite = true
		case "database_read":
			sawRead = true
		}
	}
	if !sawWrite {
		t.Errorf("expected a database_write tag for .save()")
	}
	if !sawRead {
		t.Errorf("expected a database_read tag for .filter()")
	}
}

func TestReactHookTagger_TagsUseState(t *testing.T) {
	tagger := references.DefaultTagger()
	src := `import React, { useState } from 'react'

function App() {
    const [count, setCount] = useState(0);
    return count;
}
`
	recs := extractWithTagger(t, "javascript", tsjs.GetLanguage(), src, "App.js", tagger)
	tagged := findTagged(recs, "react")
	if len(tagged) == 0 {
		t.Fatalf("expected a react tagged reference for useState")
	}
	foundHook := false
	for _, r := range tagged {
		if r.Properties["semantic_tag"] == "react_hook" {
			foundHook = true
		}
	}
	if !foundHook {
		t.Errorf("expected semantic_tag=react_hook")
	}
}

func TestSpringDataTagger_TagsFindBy(t *testing.T) {
	tagger := references.DefaultTagger()
	src := `import org.springframework.data.repository.CrudRepository;

class Service {
    void go(UserRepo repo) {
        repo.findByName("alice");
        repo.save(null);
    }
}
`
	recs := extractWithTagger(t, "java", tsjava.GetLanguage(), src, "Service.java", tagger)
	tagged := findTagged(recs, "spring_data")
	if len(tagged) == 0 {
		t.Fatalf("expected spring_data tagged references")
	}
	var sawRead, sawWrite bool
	for _, r := range tagged {
		switch r.Properties["semantic_tag"] {
		case "database_read":
			sawRead = true
		case "database_write":
			sawWrite = true
		}
	}
	if !sawRead {
		t.Errorf("expected a database_read tag for findByName")
	}
	if !sawWrite {
		t.Errorf("expected a database_write tag for save")
	}
}

func TestActiveRecordTagger_TagsSave(t *testing.T) {
	tagger := references.DefaultTagger()
	src := `require 'active_record'

class User < ActiveRecord::Base
end

def run(u)
  u.save
  User.where(id: 1)
end
`
	recs := extractWithTagger(t, "ruby", tsruby.GetLanguage(), src, "user.rb", tagger)
	tagged := findTagged(recs, "active_record")
	if len(tagged) == 0 {
		t.Fatalf("expected active_record tagged references")
	}
}

func TestCompositeTagger_NilEntryIsSkipped(t *testing.T) {
	c := &references.CompositeTagger{Taggers: []references.FrameworkTagger{nil}}
	// Just call Tag directly on a dummy record — no panic expected.
	rec := &types.EntityRecord{Properties: map[string]string{}}
	c.Tag(rec, references.FrameworkContext{})
}

func TestCompositeTagger_NilReceiverSafe(t *testing.T) {
	var c *references.CompositeTagger
	rec := &types.EntityRecord{}
	c.Tag(rec, references.FrameworkContext{})
}

func TestFrameworkContextHasFramework(t *testing.T) {
	ctx := references.FrameworkContext{
		Frameworks: map[string]struct{}{"django": {}},
	}
	if !ctx.HasFramework("django") {
		t.Fatalf("expected HasFramework(django) = true")
	}
	if ctx.HasFramework("rails") {
		t.Fatalf("expected HasFramework(rails) = false")
	}
	// Nil-map safety.
	empty := references.FrameworkContext{}
	if empty.HasFramework("django") {
		t.Fatalf("expected empty context to return false")
	}
}
