package links

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaintFlow_DirectSourceSinkPython exercises the canonical Phase
// 2B chain: a Flask-style handler reads request.args then passes the
// value into cursor.execute with string formatting. The pass should
// emit one SecurityFinding categorised as sql_injection.
func TestTaintFlow_DirectSourceSinkPython(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "view.py", `
from flask import request

def search(cursor):
    q = request.args.get("q")
    cursor.execute("SELECT * FROM users WHERE name = '%s'" % q)
    return cursor.fetchall()
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "s1", Name: "search", Kind: "SCOPE.Function", SourceFile: "view.py"},
		},
	}}
	paths := Paths{Links: filepath.Join(root, "out", "links.json")}
	res, err := runTaintFlowPass(graphs, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.LinksAdded == 0 {
		t.Fatal("expected at least one entity stamped with taint_role")
	}
	role := graphs[0].Entities[0].Properties.Get(TaintRolePropertyKey)
	if !strings.Contains(role, "source") || !strings.Contains(role, "sink") {
		t.Errorf("entity taint_role=%q; want source+sink", role)
	}
	// Sidecar written and contains the finding.
	raw, err := os.ReadFile(filepath.Join(root, "out", "links-taint.json"))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	var doc taintDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse sidecar: %v", err)
	}
	if len(doc.Findings) == 0 {
		t.Fatal("expected at least one SecurityFinding")
	}
	var sqlFound bool
	for _, f := range doc.Findings {
		if f.Category == "sql_injection" && f.Confidence >= 0.7 {
			sqlFound = true
		}
	}
	if !sqlFound {
		t.Errorf("expected a sql_injection finding ≥0.7; got %+v", doc.Findings)
	}
}

// TestTaintFlow_SanitizerBreaksPropagation verifies that a function
// containing a parameterised-query sanitizer on the SQL path blocks
// the finding even though a source and a sink-categoryitied call site
// both exist in the chain. Conservative-> aggressive: false positives
// erode trust.
func TestTaintFlow_SanitizerBreaksPropagation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "safe.py", `
from flask import request

def safe(cursor):
    q = request.args.get("q")
    cursor.execute("SELECT * FROM users WHERE name = %s", (q,))
    return cursor.fetchall()
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "s1", Name: "safe", Kind: "SCOPE.Function", SourceFile: "safe.py"},
		},
	}}
	paths := Paths{Links: filepath.Join(root, "out", "links.json")}
	if _, err := runTaintFlowPass(graphs, paths, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "out", "links-taint.json"))
	var doc taintDocument
	_ = json.Unmarshal(raw, &doc)
	for _, f := range doc.Findings {
		if f.Category == "sql_injection" {
			t.Errorf("sanitizer present but sql_injection still flagged: %+v", f)
		}
	}
}

// TestTaintFlow_TransitiveCommandInjectionGo confirms the BFS reaches
// a sink across one CALLS hop and decays confidence per hop.
func TestTaintFlow_TransitiveCommandInjectionGo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "h.go", `
package h

import "os/exec"

func handler(r *Req) {
	name := r.URL.Query().Get("name")
	runCmd(name)
}

func runCmd(name string) {
	exec.Command(name).Run()
}
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "h", Name: "handler", Kind: "SCOPE.Function", SourceFile: "h.go"},
			{ID: "r", Name: "runCmd", Kind: "SCOPE.Function", SourceFile: "h.go"},
		},
		Edges: []edgeRef{
			{FromID: "h", ToID: "r", Kind: "CALLS"},
		},
	}}
	paths := Paths{Links: filepath.Join(root, "out", "links.json")}
	if _, err := runTaintFlowPass(graphs, paths, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "out", "links-taint.json"))
	var doc taintDocument
	_ = json.Unmarshal(raw, &doc)
	var got *SecurityFinding
	for i := range doc.Findings {
		if doc.Findings[i].Category == "command_injection" {
			got = &doc.Findings[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("expected a command_injection finding; got %+v", doc.Findings)
	}
	if len(got.Path) < 2 {
		t.Errorf("expected transitive finding (path ≥2 entities); got %v", got.Path)
	}
	if got.Confidence >= 1.0 {
		t.Errorf("expected hop-decayed confidence < 1.0; got %v", got.Confidence)
	}
}

// TestTaintFlow_NoSourceNoFinding documents the baseline: a function
// with sinks but no taint source upstream is not flagged.
func TestTaintFlow_NoSourceNoFinding(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "static.py", `
def static_sql(cursor):
    cursor.execute("SELECT 1")
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "s", Name: "static_sql", Kind: "SCOPE.Function", SourceFile: "static.py"},
		},
	}}
	paths := Paths{Links: filepath.Join(root, "out", "links.json")}
	if _, err := runTaintFlowPass(graphs, paths, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "out", "links-taint.json"))
	var doc taintDocument
	_ = json.Unmarshal(raw, &doc)
	if len(doc.Findings) != 0 {
		t.Errorf("expected zero findings without a source; got %d", len(doc.Findings))
	}
}
