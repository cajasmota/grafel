package clojure_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/clojure"
)

func TestClojureExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("clojure")
	if !ok {
		t.Fatal("clojure extractor not registered")
	}
}

func TestClojureExtractor_Functions(t *testing.T) {
	src := `(ns myapp.core
  (:require [clojure.string :as str]))

(defn greet [name]
  (str "Hello, " name))

(defn- private-helper [x]
  (* x 2))

(defn add [a b]
  (+ a b))
`
	ext, _ := extractor.Get("clojure")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "core.clj",
		Content:  []byte(src),
		Language: "clojure",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, e := range entities {
		if e.Kind == "SCOPE.Operation" {
			names[e.Name] = true
			if e.Subtype != "function" {
				t.Errorf("entity %q: expected Subtype=function, got %q", e.Name, e.Subtype)
			}
			if e.Language != "clojure" {
				t.Errorf("entity %q: expected Language=clojure, got %q", e.Name, e.Language)
			}
		}
	}
	for _, want := range []string{"greet", "private-helper", "add"} {
		if !names[want] {
			t.Errorf("expected function %q to be extracted", want)
		}
	}
}

func TestClojureExtractor_TypeDefinitions(t *testing.T) {
	src := `(defrecord User [id name email])

(defprotocol UserStore
  (get-user [this id])
  (create-user [this name email]))
`
	ext, _ := extractor.Get("clojure")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "types.clj",
		Content:  []byte(src),
		Language: "clojure",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := make(map[string]bool)
	for _, e := range entities {
		if e.Kind == "SCOPE.Component" {
			names[e.Name] = true
			if e.Subtype != "class" {
				t.Errorf("entity %q: expected Subtype=class, got %q", e.Name, e.Subtype)
			}
		}
	}
	for _, want := range []string{"User", "UserStore"} {
		if !names[want] {
			t.Errorf("expected type %q to be extracted", want)
		}
	}
}

func TestClojureExtractor_Macros(t *testing.T) {
	src := `(ns myapp.macros)

(defmacro unless [test & body]
  ` + "`" + `(if (not ~test) (do ~@body)))

(defmacro with-timing [& body]
  ` + "`" + `(let [start (now)]
     (log "start")
     ~@body))
`
	ext, _ := extractor.Get("clojure")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "macros.clj",
		Content:  []byte(src),
		Language: "clojure",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	macros := make(map[string]bool)
	var contains int
	for _, e := range entities {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "macro" {
			macros[e.Name] = true
			if e.Language != "clojure" {
				t.Errorf("macro %q: expected Language=clojure, got %q", e.Name, e.Language)
			}
		}
		if e.Kind == "SCOPE.Component" && e.Subtype == "namespace" {
			for _, r := range e.Relationships {
				if r.Kind == "CONTAINS" {
					contains++
				}
			}
		}
	}
	for _, want := range []string{"unless", "with-timing"} {
		if !macros[want] {
			t.Errorf("expected macro %q to be extracted as Subtype=macro", want)
		}
	}
	// Macros must be members of the namespace via CONTAINS, like defn.
	if contains < 2 {
		t.Errorf("expected namespace to CONTAIN both macros, got %d CONTAINS edges", contains)
	}
	// with-timing calls (now)/(log ...) — verify CALLS edges are mined from
	// a macro body just like a defn body.
	var withTimingCalls int
	for _, e := range entities {
		if e.Name == "with-timing" {
			withTimingCalls = len(e.Relationships)
		}
	}
	if withTimingCalls == 0 {
		t.Errorf("expected CALLS edges mined from macro body, got 0")
	}
}

func TestClojureExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("clojure")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.clj",
		Content:  []byte{},
		Language: "clojure",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestClojureExtractor_LineNumbers(t *testing.T) {
	src := `; comment
(defn hello [name]
  (str "Hello " name))
`
	ext, _ := extractor.Get("clojure")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "hello.clj",
		Content:  []byte(src),
		Language: "clojure",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Name == "hello" {
			if e.StartLine < 1 {
				t.Errorf("expected StartLine >= 1, got %d", e.StartLine)
			}
			return
		}
	}
	t.Error("entity 'hello' not found")
}
