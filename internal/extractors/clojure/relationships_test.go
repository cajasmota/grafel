package clojure_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/clojure"
	"github.com/cajasmota/archigraph/internal/types"
)

// extract is a small helper to invoke the registered clojure extractor.
func extract(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("clojure")
	if !ok {
		t.Fatal("clojure extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "clojure",
	})
	if err != nil {
		t.Fatalf("Extract(%q) error: %v", path, err)
	}
	return got
}

// importTargets returns the set of IMPORTS edge ToIDs across all entities.
func importTargets(records []types.EntityRecord) map[string]types.RelationshipRecord {
	out := make(map[string]types.RelationshipRecord)
	for _, e := range records {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				out[r.ToID] = r
			}
		}
	}
	return out
}

// callTargets returns the set of CALLS ToIDs emitted from the operation
// entity with the given Name.
func callTargets(records []types.EntityRecord, op string) map[string]bool {
	out := map[string]bool{}
	for _, e := range records {
		if e.Kind != "SCOPE.Operation" || e.Name != op {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" {
				out[r.ToID] = true
			}
		}
	}
	return out
}

// containsTargets returns the set of CONTAINS ToIDs emitted from the
// component entity with the given Name.
func containsTargets(records []types.EntityRecord, comp string) map[string]bool {
	out := map[string]bool{}
	for _, e := range records {
		if e.Kind != "SCOPE.Component" || e.Name != comp {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "CONTAINS" {
				out[r.ToID] = true
			}
		}
	}
	return out
}

func TestClojureExtractor_RequireImports(t *testing.T) {
	src := `(ns myapp.core
  (:require [clojure.string :as str]
            [clojure.set :refer [union intersection]]
            [clojure.walk :refer :all]
            clojure.pprint))

(defn foo [] nil)
`
	got := extract(t, "core.clj", src)
	imps := importTargets(got)

	for _, want := range []string{
		"clojure.string",
		"clojure.set",
		"clojure.walk",
		"clojure.pprint",
	} {
		if _, ok := imps[want]; !ok {
			t.Errorf("expected IMPORTS edge for %q, got %v", want, keys(imps))
		}
	}

	// :refer :all → wildcard property.
	if rel, ok := imps["clojure.walk"]; ok {
		if rel.Properties["wildcard"] != "1" {
			t.Errorf("expected wildcard=1 on clojure.walk, got %q", rel.Properties["wildcard"])
		}
	}
	// :as alias becomes local_name.
	if rel, ok := imps["clojure.string"]; ok {
		if rel.Properties["local_name"] != "str" {
			t.Errorf("expected local_name=str on clojure.string, got %q", rel.Properties["local_name"])
		}
		if rel.Properties["language"] != "clojure" {
			t.Errorf("expected language=clojure tag, got %q", rel.Properties["language"])
		}
	}
}

func TestClojureExtractor_UseImports(t *testing.T) {
	src := `(ns myapp.core
  (:use clojure.test
        [clojure.repl :only [doc]]))

(defn x [] nil)
`
	got := extract(t, "core.clj", src)
	imps := importTargets(got)

	if _, ok := imps["clojure.test"]; !ok {
		t.Errorf("expected IMPORTS edge for clojure.test, got %v", keys(imps))
	}
	if rel, ok := imps["clojure.test"]; ok {
		if rel.Properties["wildcard"] != "1" {
			t.Errorf("expected :use to mark wildcard=1 on clojure.test")
		}
	}
}

func TestClojureExtractor_JavaImports(t *testing.T) {
	src := `(ns myapp.core
  (:import java.util.Date
           [java.util Calendar TimeZone]))

(defn now [] nil)
`
	got := extract(t, "core.clj", src)
	imps := importTargets(got)

	for _, want := range []string{
		"java.util.Date",
		"java.util.Calendar",
		"java.util.TimeZone",
	} {
		if _, ok := imps[want]; !ok {
			t.Errorf("expected IMPORTS edge for %q, got %v", want, keys(imps))
		}
	}
}

func TestClojureExtractor_Calls(t *testing.T) {
	src := `(ns myapp.core
  (:require [clojure.string :as str]))

(defn greet [name]
  (let [upper (str/upper-case name)
        msg (format "Hello, %s" upper)]
    (println msg)))
`
	got := extract(t, "core.clj", src)
	calls := callTargets(got, "greet")

	for _, want := range []string{"str/upper-case", "format", "println"} {
		if !calls[want] {
			t.Errorf("expected CALLS edge to %q from greet, got %v", want, keys(calls))
		}
	}
	if calls["let"] {
		t.Error("special form 'let' should not be emitted as a CALLS target")
	}
	if calls["greet"] {
		t.Error("self-recursion should be filtered from CALLS")
	}
}

func TestClojureExtractor_CallsDedup(t *testing.T) {
	src := `(defn f [xs]
  (println xs)
  (println (count xs))
  (println (count xs)))
`
	got := extract(t, "f.clj", src)
	calls := callTargets(got, "f")

	// println should appear exactly once even though it's called 3x.
	if !calls["println"] {
		t.Fatal("expected CALLS edge to println")
	}
	count := 0
	for _, e := range got {
		if e.Name == "f" {
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" && r.ToID == "println" {
					count++
				}
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 CALLS edge to println, got %d", count)
	}
}

func TestClojureExtractor_NamespaceContains(t *testing.T) {
	src := `(ns myapp.core)

(defn alpha [] nil)
(defn beta [] (alpha))
(defrecord User [id name])
`
	got := extract(t, "core.clj", src)
	contains := containsTargets(got, "myapp.core")

	wantOpRefAlpha := extractor.BuildOperationStructuralRef("clojure", "core.clj", "alpha")
	wantOpRefBeta := extractor.BuildOperationStructuralRef("clojure", "core.clj", "beta")

	if !contains[wantOpRefAlpha] {
		t.Errorf("expected CONTAINS edge to %q, got %v", wantOpRefAlpha, keys(contains))
	}
	if !contains[wantOpRefBeta] {
		t.Errorf("expected CONTAINS edge to %q, got %v", wantOpRefBeta, keys(contains))
	}
	if !contains["User"] {
		t.Errorf("expected CONTAINS edge to component User, got %v", keys(contains))
	}
}

func TestClojureExtractor_NoNamespaceNoContains(t *testing.T) {
	src := `(defn standalone [] nil)`
	got := extract(t, "standalone.clj", src)

	for _, e := range got {
		for _, r := range e.Relationships {
			if r.Kind == "CONTAINS" {
				t.Errorf("expected no CONTAINS edges without (ns ...), got %+v", r)
			}
		}
	}
}

func TestClojureExtractor_LanguageTag(t *testing.T) {
	src := `(ns myapp.core (:require [clojure.string :as str]))
(defn greet [n] (println n))
`
	got := extract(t, "core.clj", src)

	for _, e := range got {
		for _, r := range e.Relationships {
			if r.Properties["language"] != "clojure" {
				t.Errorf("expected language=clojure on every relationship, got %q on %+v", r.Properties["language"], r)
			}
		}
	}
}

func TestClojureExtractor_RealWorldFixture(t *testing.T) {
	// Smoke test on the compojure-style fixture: ensure we emit at
	// least one of each new edge kind without panicking.
	src := `(ns myapp.routes
  (:require
   [compojure.core :refer [defroutes GET POST]]
   [myapp.db :as db]))

(defn list-posts [request]
  (let [user (:user request)
        posts (db/find-posts {:user-id (:id user)})]
    (response posts)))
`
	got := extract(t, "routes.clj", src)

	var sawImport, sawCall, sawContain bool
	for _, e := range got {
		for _, r := range e.Relationships {
			switch r.Kind {
			case "IMPORTS":
				sawImport = true
			case "CALLS":
				sawCall = true
			case "CONTAINS":
				sawContain = true
			}
		}
	}
	if !sawImport {
		t.Error("expected at least one IMPORTS edge")
	}
	if !sawCall {
		t.Error("expected at least one CALLS edge")
	}
	if !sawContain {
		t.Error("expected at least one CONTAINS edge")
	}
}

func keys(m interface{}) []string {
	switch x := m.(type) {
	case map[string]bool:
		out := make([]string, 0, len(x))
		for k := range x {
			out = append(out, k)
		}
		return out
	case map[string]types.RelationshipRecord:
		out := make([]string, 0, len(x))
		for k := range x {
			out = append(out, k)
		}
		return out
	}
	return nil
}
