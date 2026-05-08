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
