package lisp_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/lisp"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runLisp(t *testing.T, lang, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get(lang)
	if !ok {
		t.Fatalf("%s extractor not registered", lang)
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func lispFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func lispFindSubtype(ents []types.EntityRecord, name, kind, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind && ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

func lispHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == edgeKind && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Registration tests
// ---------------------------------------------------------------------------

func TestLisp_Registered(t *testing.T) {
	for _, lang := range []string{"commonlisp", "scheme", "racket"} {
		_, ok := extractor.Get(lang)
		if !ok {
			t.Errorf("%s extractor not registered", lang)
		}
	}
}

func TestLisp_EmptyInput(t *testing.T) {
	for _, lang := range []string{"commonlisp", "scheme", "racket"} {
		ext, _ := extractor.Get(lang)
		ents, err := ext.Extract(context.Background(), extractor.FileInput{
			Path:     "empty.lisp",
			Content:  []byte{},
			Language: lang,
		})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", lang, err)
		}
		if len(ents) != 0 {
			t.Errorf("%s: expected 0 entities, got %d", lang, len(ents))
		}
	}
}

// ---------------------------------------------------------------------------
// Common Lisp fixture
// ---------------------------------------------------------------------------

const clSrc = `
(in-package :myapp)

(defstruct point
  x y)

(defclass animal ()
  ((name :initarg :name :accessor animal-name)
   (sound :initarg :sound :accessor animal-sound)))

(defmacro with-logging (&body body)
  ` + "`" + `(progn
     (format t "start~%")
     ,@body
     (format t "end~%")))

(defun greet (name)
  (format t "Hello, ~a!~%" name))

(defun factorial (n)
  (if (<= n 1)
      1
      (* n (factorial (- n 1)))))

(defmethod animal-speak ((a animal))
  (format t "~a says ~a~%"
          (animal-name a)
          (animal-sound a)))

(defun make-animal (name sound)
  (make-instance 'animal :name name :sound sound))
`

func TestCommonLisp_Functions(t *testing.T) {
	ents := runLisp(t, "commonlisp", clSrc, "myapp.lisp")

	wantOps := []string{"greet", "factorial", "make-animal"}
	for _, name := range wantOps {
		e := lispFind(ents, name, "SCOPE.Operation")
		if e == nil {
			t.Errorf("expected SCOPE.Operation %q", name)
		} else if e.Language != "commonlisp" {
			t.Errorf("%q: expected Language=commonlisp, got %q", name, e.Language)
		}
	}
}

func TestCommonLisp_Macro(t *testing.T) {
	ents := runLisp(t, "commonlisp", clSrc, "myapp.lisp")
	e := lispFindSubtype(ents, "with-logging", "SCOPE.Operation", "macro")
	if e == nil {
		t.Error("expected macro with-logging as SCOPE.Operation(macro)")
	}
}

func TestCommonLisp_Struct(t *testing.T) {
	ents := runLisp(t, "commonlisp", clSrc, "myapp.lisp")
	e := lispFindSubtype(ents, "point", "SCOPE.Component", "struct")
	if e == nil {
		t.Error("expected SCOPE.Component(struct) for point")
	}
}

func TestCommonLisp_Class(t *testing.T) {
	ents := runLisp(t, "commonlisp", clSrc, "myapp.lisp")
	e := lispFindSubtype(ents, "animal", "SCOPE.Component", "class")
	if e == nil {
		t.Error("expected SCOPE.Component(class) for animal")
	}
}

func TestCommonLisp_Namespace(t *testing.T) {
	ents := runLisp(t, "commonlisp", clSrc, "myapp.lisp")
	e := lispFindSubtype(ents, "myapp", "SCOPE.Component", "namespace")
	if e == nil {
		t.Error("expected SCOPE.Component(namespace) for myapp")
	}
}

func TestCommonLisp_CallsEdge(t *testing.T) {
	ents := runLisp(t, "commonlisp", clSrc, "myapp.lisp")
	// factorial should call factorial (recursion) — not emitted (self-ref filtered) —
	// but make-animal should call make-instance
	if !lispHasRel(ents, "make-animal", "SCOPE.Operation", "CALLS", "make-instance") {
		t.Error("expected CALLS make-animal→make-instance")
	}
}

func TestCommonLisp_DefclassNotMatchScheme(t *testing.T) {
	// Ensure defclass is NOT matched in a Scheme file
	schemeSrc := `
(define (foo x) (+ x 1))
`
	ents := runLisp(t, "scheme", schemeSrc, "foo.scm")
	for _, e := range ents {
		if e.Subtype == "class" {
			t.Errorf("scheme should not produce class entities, got: %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Common Lisp recall fixture
// ---------------------------------------------------------------------------

const clRecallSrc = `
(in-package :cl-web)

(defstruct request
  method path headers body)

(defstruct response
  status headers body)

(defclass server ()
  ((host :initarg :host :accessor server-host :initform "localhost")
   (port :initarg :port :accessor server-port :initform 8080)
   (routes :initarg :routes :accessor server-routes :initform nil)))

(defmacro defroute (path method handler)
  ` + "`" + `(list ,path ,method ,handler))

(defun make-server (&key (host "localhost") (port 8080))
  (make-instance 'server :host host :port port))

(defun add-route (srv path method handler)
  (push (defroute path method handler)
        (server-routes srv)))

(defun handle-request (srv req)
  (let ((route (find-route srv (request-method req) (request-path req))))
    (if route
        (funcall (third route) req)
        (make-response 404 "Not Found"))))

(defun find-route (srv method path)
  (find-if (lambda (r)
              (and (string= (first r) path)
                   (string= (second r) method)))
            (server-routes srv)))

(defun make-response (status body)
  (make-instance 'response :status status :body body))

(defun start-server (srv)
  (format t "Starting server on ~a:~a~%"
          (server-host srv)
          (server-port srv)))
`

func TestCommonLisp_RecallFixture(t *testing.T) {
	ents := runLisp(t, "commonlisp", clRecallSrc, "cl-web.lisp")

	wantOps := []string{
		"make-server", "add-route", "handle-request",
		"find-route", "make-response", "start-server",
	}
	wantComps := []string{"request", "response", "server"}
	wantMacros := []string{"defroute"}

	foundOps := make(map[string]bool)
	foundComps := make(map[string]bool)
	foundMacros := make(map[string]bool)

	for _, e := range ents {
		switch {
		case e.Kind == "SCOPE.Operation" && e.Subtype == "function":
			foundOps[e.Name] = true
		case e.Kind == "SCOPE.Component" && (e.Subtype == "class" || e.Subtype == "struct"):
			foundComps[e.Name] = true
		case e.Kind == "SCOPE.Operation" && e.Subtype == "macro":
			foundMacros[e.Name] = true
		}
	}

	opHits, compHits, macroHits := 0, 0, 0
	for _, n := range wantOps {
		if foundOps[n] {
			opHits++
		} else {
			t.Logf("CL: missing op: %s", n)
		}
	}
	for _, n := range wantComps {
		if foundComps[n] {
			compHits++
		} else {
			t.Logf("CL: missing comp: %s", n)
		}
	}
	for _, n := range wantMacros {
		if foundMacros[n] {
			macroHits++
		} else {
			t.Logf("CL: missing macro: %s", n)
		}
	}

	total := len(wantOps) + len(wantComps) + len(wantMacros)
	found := opHits + compHits + macroHits
	recall := float64(found) / float64(total) * 100
	t.Logf("CL recall: %d/%d (%.0f%%)", found, total, recall)
	if recall < 80 {
		t.Errorf("CL recall %.0f%% below 80%% (%d/%d)", recall, found, total)
	}
}

// ---------------------------------------------------------------------------
// Scheme fixture
// ---------------------------------------------------------------------------

const schemeSrc = `
(require "srfi-1")
(require "srfi-13")

(define-struct point (x y))

(define (make-point x y)
  (point x y))

(define (point-distance p1 p2)
  (let ((dx (- (point-x p2) (point-x p1)))
        (dy (- (point-y p2) (point-y p1))))
    (sqrt (+ (* dx dx) (* dy dy)))))

(define (map-points f pts)
  (map f pts))

(define (fold-sum lst)
  (fold + 0 lst))

(define-syntax while
  (syntax-rules ()
    ((_ test body ...)
     (let loop ()
       (when test
         body ...
         (loop))))))
`

func TestScheme_Functions(t *testing.T) {
	ents := runLisp(t, "scheme", schemeSrc, "points.scm")

	wantOps := []string{"make-point", "point-distance", "map-points", "fold-sum"}
	for _, name := range wantOps {
		e := lispFind(ents, name, "SCOPE.Operation")
		if e == nil {
			t.Errorf("scheme: expected SCOPE.Operation %q", name)
		} else if e.Language != "scheme" {
			t.Errorf("%q: expected Language=scheme, got %q", name, e.Language)
		}
	}
}

func TestScheme_Struct(t *testing.T) {
	ents := runLisp(t, "scheme", schemeSrc, "points.scm")
	e := lispFindSubtype(ents, "point", "SCOPE.Component", "struct")
	if e == nil {
		t.Error("scheme: expected SCOPE.Component(struct) for point")
	}
}

func TestScheme_Macro(t *testing.T) {
	ents := runLisp(t, "scheme", schemeSrc, "points.scm")
	e := lispFindSubtype(ents, "while", "SCOPE.Operation", "macro")
	if e == nil {
		t.Error("scheme: expected SCOPE.Operation(macro) for while")
	}
}

func TestScheme_ImportEdge(t *testing.T) {
	ents := runLisp(t, "scheme", schemeSrc, "points.scm")
	if !lispHasRel(ents, "srfi-1", "SCOPE.Component", "IMPORTS", "srfi-1") {
		t.Error("scheme: expected IMPORTS edge for srfi-1")
	}
}

func TestScheme_CallsEdge(t *testing.T) {
	ents := runLisp(t, "scheme", schemeSrc, "points.scm")
	// map-points body calls map
	if !lispHasRel(ents, "map-points", "SCOPE.Operation", "CALLS", "map") {
		t.Error("scheme: expected CALLS map-points→map")
	}
}

// ---------------------------------------------------------------------------
// Scheme recall fixture
// ---------------------------------------------------------------------------

const schemeRecallSrc = `
(require "srfi-1")
(require "srfi-9")
(require "srfi-13")

(define-struct person (name age email))

(define (make-person name age email)
  (person name age email))

(define (greet-person p)
  (string-append "Hello, " (person-name p) "!"))

(define (adults lst)
  (filter (lambda (p) (>= (person-age p) 18)) lst))

(define (names lst)
  (map person-name lst))

(define (email-list lst)
  (map person-email lst))

(define (find-by-name lst name)
  (find (lambda (p) (string=? (person-name p) name)) lst))

(define (sort-by-age lst)
  (sort lst (lambda (a b) (< (person-age a) (person-age b)))))

(define-syntax when-defined
  (syntax-rules ()
    ((_ val body ...)
     (if val (begin body ...) #f))))
`

func TestScheme_RecallFixture(t *testing.T) {
	ents := runLisp(t, "scheme", schemeRecallSrc, "people.scm")

	wantOps := []string{
		"make-person", "greet-person", "adults",
		"names", "email-list", "find-by-name", "sort-by-age",
	}
	wantComps := []string{"person"}
	wantMacros := []string{"when-defined"}

	foundOps := make(map[string]bool)
	foundComps := make(map[string]bool)
	foundMacros := make(map[string]bool)

	for _, e := range ents {
		switch {
		case e.Kind == "SCOPE.Operation" && e.Subtype == "function":
			foundOps[e.Name] = true
		case e.Kind == "SCOPE.Component" && e.Subtype == "struct":
			foundComps[e.Name] = true
		case e.Kind == "SCOPE.Operation" && e.Subtype == "macro":
			foundMacros[e.Name] = true
		}
	}

	opHits, compHits, macroHits := 0, 0, 0
	for _, n := range wantOps {
		if foundOps[n] {
			opHits++
		} else {
			t.Logf("Scheme: missing op: %s", n)
		}
	}
	for _, n := range wantComps {
		if foundComps[n] {
			compHits++
		} else {
			t.Logf("Scheme: missing comp: %s", n)
		}
	}
	for _, n := range wantMacros {
		if foundMacros[n] {
			macroHits++
		} else {
			t.Logf("Scheme: missing macro: %s", n)
		}
	}

	total := len(wantOps) + len(wantComps) + len(wantMacros)
	found := opHits + compHits + macroHits
	recall := float64(found) / float64(total) * 100
	t.Logf("Scheme recall: %d/%d (%.0f%%)", found, total, recall)
	if recall < 80 {
		t.Errorf("Scheme recall %.0f%% below 80%% (%d/%d)", recall, found, total)
	}
}

// ---------------------------------------------------------------------------
// Racket fixture
// ---------------------------------------------------------------------------

const racketSrc = `
#lang racket/base

(require racket/list)
(require racket/string)
(require/typed racket/match [match-define (-> Any Void)])

(struct point (x y) #:transparent)
(struct circle (center radius) #:transparent)

(define (make-point x y)
  (point x y))

(define (circle-area c)
  (let ([r (circle-radius c)])
    (* 3.14159 r r)))

(define/contract (safe-divide a b)
  (-> number? (and/c number? (not/c zero?)) number?)
  (/ a b))

(define/contract (greet name)
  (-> string? string?)
  (string-append "Hello, " name "!"))

(define-syntax swap!
  (syntax-rules ()
    ((_ a b)
     (let ([tmp a])
       (set! a b)
       (set! b tmp)))))

(define (filter-positives lst)
  (filter positive? lst))
`

func TestRacket_Functions(t *testing.T) {
	ents := runLisp(t, "racket", racketSrc, "geometry.rkt")

	wantOps := []string{"make-point", "circle-area", "safe-divide", "greet", "filter-positives"}
	for _, name := range wantOps {
		e := lispFind(ents, name, "SCOPE.Operation")
		if e == nil {
			t.Errorf("racket: expected SCOPE.Operation %q", name)
		} else if e.Language != "racket" {
			t.Errorf("%q: expected Language=racket, got %q", name, e.Language)
		}
	}
}

func TestRacket_Structs(t *testing.T) {
	ents := runLisp(t, "racket", racketSrc, "geometry.rkt")
	for _, name := range []string{"point", "circle"} {
		e := lispFindSubtype(ents, name, "SCOPE.Component", "struct")
		if e == nil {
			t.Errorf("racket: expected SCOPE.Component(struct) for %q", name)
		}
	}
}

func TestRacket_DefineContract(t *testing.T) {
	ents := runLisp(t, "racket", racketSrc, "geometry.rkt")
	e := lispFind(ents, "safe-divide", "SCOPE.Operation")
	if e == nil {
		t.Error("racket: expected SCOPE.Operation for safe-divide (define/contract)")
	}
}

func TestRacket_Macro(t *testing.T) {
	ents := runLisp(t, "racket", racketSrc, "geometry.rkt")
	e := lispFindSubtype(ents, "swap!", "SCOPE.Operation", "macro")
	if e == nil {
		t.Error("racket: expected SCOPE.Operation(macro) for swap!")
	}
}

func TestRacket_ImportEdge(t *testing.T) {
	ents := runLisp(t, "racket", racketSrc, "geometry.rkt")
	if !lispHasRel(ents, "list", "SCOPE.Component", "IMPORTS", "racket/list") {
		t.Error("racket: expected IMPORTS edge for racket/list")
	}
}

func TestRacket_LanguageTagged(t *testing.T) {
	ents := runLisp(t, "racket", racketSrc, "geometry.rkt")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties["language"] != "racket" {
				t.Errorf("rel %s→%s missing language=racket (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Racket recall fixture
// ---------------------------------------------------------------------------

const racketRecallSrc = `
#lang racket

(require racket/list)
(require racket/string)
(require racket/match)
(require racket/contract)

(struct user (id name email role) #:transparent)
(struct session (token user-id expires) #:transparent)

(define/contract (make-user id name email)
  (-> exact-integer? string? string? user?)
  (user id name email 'member))

(define/contract (make-admin id name email)
  (-> exact-integer? string? string? user?)
  (user id name email 'admin))

(define (user-admin? u)
  (eq? (user-role u) 'admin))

(define/contract (create-session token user-id ttl)
  (-> string? exact-integer? exact-integer? session?)
  (session token user-id (+ (current-seconds) ttl)))

(define (session-expired? s)
  (> (current-seconds) (session-expires s)))

(define (find-user users id)
  (findf (lambda (u) (= (user-id u) id)) users))

(define-syntax with-auth
  (syntax-rules ()
    ((_ sess body ...)
     (if (not (session-expired? sess))
         (begin body ...)
         (error "Session expired")))))
`

func TestRacket_RecallFixture(t *testing.T) {
	ents := runLisp(t, "racket", racketRecallSrc, "auth.rkt")

	wantOps := []string{
		"make-user", "make-admin", "user-admin?",
		"create-session", "session-expired?", "find-user",
	}
	wantComps := []string{"user", "session"}
	wantMacros := []string{"with-auth"}

	foundOps := make(map[string]bool)
	foundComps := make(map[string]bool)
	foundMacros := make(map[string]bool)

	for _, e := range ents {
		switch {
		case e.Kind == "SCOPE.Operation" && e.Subtype == "function":
			foundOps[e.Name] = true
		case e.Kind == "SCOPE.Component" && e.Subtype == "struct":
			foundComps[e.Name] = true
		case e.Kind == "SCOPE.Operation" && e.Subtype == "macro":
			foundMacros[e.Name] = true
		}
	}

	opHits, compHits, macroHits := 0, 0, 0
	for _, n := range wantOps {
		if foundOps[n] {
			opHits++
		} else {
			t.Logf("Racket: missing op: %s", n)
		}
	}
	for _, n := range wantComps {
		if foundComps[n] {
			compHits++
		} else {
			t.Logf("Racket: missing comp: %s", n)
		}
	}
	for _, n := range wantMacros {
		if foundMacros[n] {
			macroHits++
		} else {
			t.Logf("Racket: missing macro: %s", n)
		}
	}

	total := len(wantOps) + len(wantComps) + len(wantMacros)
	found := opHits + compHits + macroHits
	recall := float64(found) / float64(total) * 100
	t.Logf("Racket recall: %d/%d (%.0f%%)", found, total, recall)
	if recall < 80 {
		t.Errorf("Racket recall %.0f%% below 80%% (%d/%d)", recall, found, total)
	}
}

// ---------------------------------------------------------------------------
// Cross-dialect isolation tests
// ---------------------------------------------------------------------------

func TestCrossDialect_CLDefclassNotInRacket(t *testing.T) {
	src := "(defclass animal () ())"
	ents := runLisp(t, "racket", src, "test.rkt")
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			t.Errorf("racket: defclass should not match, got entity: %+v", e)
		}
	}
}

func TestCrossDialect_RacketStructNotInCL(t *testing.T) {
	src := "(struct point (x y))"
	ents := runLisp(t, "commonlisp", src, "test.lisp")
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "struct" && e.Name == "point" {
			t.Errorf("commonlisp: (struct ...) should not match defstruct, got entity: %+v", e)
		}
	}
}

func TestCrossDialect_DefunNotInScheme(t *testing.T) {
	src := "(defun foo (x) (+ x 1))"
	ents := runLisp(t, "scheme", src, "test.scm")
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Name == "foo" {
			t.Errorf("scheme: defun should not match, got entity: %+v", e)
		}
	}
}
