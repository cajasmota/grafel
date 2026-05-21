package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// buildV2DocsTestServer creates a minimal Server with one group "testgrp"
// containing two entities: a function with a CALLS relationship and a method.
func buildV2DocsTestServer(t *testing.T) *Server {
	t.Helper()

	fn := graph.Entity{
		ID:         "fn1",
		Name:       "doWork",
		Kind:       "Function",
		SourceFile: "src/work.ts",
		StartLine:  10,
		Signature:  "function doWork(x: number): void",
	}
	mth := graph.Entity{
		ID:         "mth1",
		Name:       "MyClass.run",
		Kind:       "Method",
		SourceFile: "src/my-class.ts",
		StartLine:  22,
	}
	rel := graph.Relationship{
		ID:     "r1",
		FromID: "fn1",
		ToID:   "mth1",
		Kind:   "CALLS",
	}

	doc := &graph.Document{
		Version:       graph.SchemaVersion,
		Entities:      []graph.Entity{fn, mth},
		Relationships: []graph.Relationship{rel},
	}

	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	srv.graphs.mu.Lock()
	srv.graphs.entries["testgrp"] = &cacheEntry{
		group: &DashGroup{
			Name: "testgrp",
			Repos: map[string]*DashRepo{
				"repo1": {
					Slug: "repo1",
					Path: "",
					Doc:  doc,
				},
			},
		},
		loadedAt: time.Now().Add(60 * time.Second),
	}
	srv.graphs.mu.Unlock()

	return srv
}

func TestHandleV2DocsTree(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/testgrp/docs/tree", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var env v2Envelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatal("envelope.ok is false")
	}
	// data must be a non-empty slice
	data, ok := env.Data.([]interface{})
	if !ok || len(data) == 0 {
		t.Fatalf("expected non-empty tree, got %T %v", env.Data, env.Data)
	}
}

func TestHandleV2DocsEntityDetail(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/testgrp/docs/entities/fn1", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var env v2Envelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatal("envelope.ok is false")
	}
	obj, ok := env.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %T", env.Data)
	}
	if obj["name"] != "doWork" {
		t.Errorf("expected name=doWork, got %v", obj["name"])
	}
	// callees must contain mth1 label
	callees, _ := obj["callees"].([]interface{})
	if len(callees) != 1 {
		t.Errorf("expected 1 callee, got %v", callees)
	}
}

func TestHandleV2DocsEntityNotFound(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/testgrp/docs/entities/no-such-entity", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleV2DocsTreeGroupNotFound(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/ghost/docs/tree", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
