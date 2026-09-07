// Package extractors_test — incremental_custom_extractors_6960_test.go
//
// Issue #6960: does the daemon's incremental re-extraction reach
// RunCustomExtractors? It did not. The hop chain is
//
//	watcher → sched worker → cmd/grafel/daemon.go:daemonSchedulerIncremental
//	        → extractors.TryIncremental → tryIncremental Step 6 re-extract loop
//	        → Get(cr.Language) → safeExtract
//
// and Get is an EXACT-KEY registry lookup. Every custom/framework extractor
// registers under a PREFIXED key ("custom_scala_di", "python_django"), so that
// lookup can never select one; RunCustomExtractors — which does the prefix fan-
// out — had two non-test call sites and neither is on this path.
//
// WHAT THE CONSEQUENCE ACTUALLY IS, which is narrower than the issue assumed.
// Both full-index paths gate custom dispatch OFF by default
// (cmd/grafel/index.go:4138 behind GRAFEL_INPROC_CUSTOM_EXTRACTORS,
// internal/daemon/extract/subproc.go:364 behind the opt-in GRAFEL_SUBPROC_EXTRACT
// branch), so in the DEFAULT configuration the initial index emits no custom
// entities and there is nothing for an incremental pass to lose. The divergence
// is real only with the gate ON — and there it is the silent-degradation shape:
// the full index emits them, Step 5 evicts them on the next edit, and the loop
// re-adds nothing, so a CHANGED file loses what every UNCHANGED file keeps.
//
// Both tests below drive the real TryIncremental and both require res.Done —
// that is the proof the pass was INCREMENTAL and not a fallback to a full
// reindex, which would compute the right answer for the wrong reason and read
// as a false all-clear.
package extractors_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/treesitter"
)

// A Play controller: the base scala extractor emits SCOPE.Component/Operation
// for it, and internal/custom/scala's DI extractor emits SCOPE.DI records that
// ONLY RunCustomExtractors can select. The two are the base/custom halves of
// the same file, which is what makes the assertions below separable.
const playControllerV1_6960 = `package controllers

import javax.inject._
import play.api.mvc._

@Singleton
class HomeController @Inject()(cc: ControllerComponents) extends AbstractController(cc) {
  def index() = Action { implicit request: Request[AnyContent] =>
    Ok("hi")
  }
}
`

const playControllerV2_6960 = `package controllers

import javax.inject._
import play.api.mvc._

@Singleton
class HomeController @Inject()(cc: ControllerComponents) extends AbstractController(cc) {
  def index() = Action { implicit request: Request[AnyContent] =>
    Ok("hello")
  }
  def about() = Action { implicit request: Request[AnyContent] =>
    Ok("about")
  }
}
`

const playControllerRel6960 = "app/controllers/HomeController.scala"

// The custom-only entity the assertions turn on. `singleton:HomeController` is
// chosen over the sibling `inject:HomeController.scala:7` deliberately: the
// latter embeds a LINE NUMBER, so an edit above it renames it and the test
// would be asserting the edit rather than the dispatch.
const customOnlyEntity6960 = "singleton:HomeController"

// seedAndEdit6960 stands up a repo whose graph already holds the file's
// entities, then edits the file, then runs one incremental pass. It returns the
// resulting graph document. The seeded graph deliberately CONTAINS the custom
// entity: that is the "initial index ran with the gate on" state, and it is what
// makes the gate-on case a LOSS rather than merely an absence.
func seedAndEdit6960(t *testing.T) *graph.Document {
	t.Helper()
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, playControllerRel6960, playControllerV1_6960)
	seed := []graph.Entity{
		{
			ID:   graph.EntityID("test-repo", "SCOPE.DI", customOnlyEntity6960, playControllerRel6960),
			Name: customOnlyEntity6960, Kind: "SCOPE.DI", SourceFile: playControllerRel6960, Language: "scala",
		},
		{
			ID:   graph.EntityID("test-repo", "SCOPE.Component", "HomeController", playControllerRel6960),
			Name: "HomeController", Kind: "SCOPE.Component", SourceFile: playControllerRel6960, Language: "scala",
		},
	}
	buildMinimalGraph(t, stateDir, seed, nil)
	seedManifest(t, repo, stateDir)

	writeFile(t, repo, playControllerRel6960, playControllerV2_6960)

	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	// THE INCREMENTALITY PROOF. Done=false means TryIncremental declined and the
	// caller runs a FULL index instead — on which custom dispatch is a different
	// question entirely. Every assertion below is about the incremental path, so
	// none of them means anything without this.
	if !res.Done {
		t.Fatalf("pass was NOT incremental — TryIncremental fell back (%s); every assertion "+
			"in this test is about the incremental path and none survives a full reindex",
			res.FallbackReason)
	}
	if res.ChangedFiles != 1 {
		t.Fatalf("expected exactly the edited file to be re-extracted, got ChangedFiles=%d", res.ChangedFiles)
	}

	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	return doc
}

func hasEntity6960(doc *graph.Document, kind, name string) bool {
	for i := range doc.Entities {
		if doc.Entities[i].Kind == kind && doc.Entities[i].Name == name {
			return true
		}
	}
	return false
}

func anyEntityOfKind6960(doc *graph.Document, kind string) []string {
	var out []string
	for i := range doc.Entities {
		if doc.Entities[i].Kind == kind {
			out = append(out, doc.Entities[i].Name)
		}
	}
	return out
}

// TestIncrementalDispatchesCustomExtractorsWhenTheGateIsOn6960 is the #6960
// regression pin. With GRAFEL_INPROC_CUSTOM_EXTRACTORS=1 the full index emits
// the SCOPE.DI records; before the fix, editing the file evicted them and the
// re-extract loop re-added nothing.
func TestIncrementalDispatchesCustomExtractorsWhenTheGateIsOn6960(t *testing.T) {
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")

	doc := seedAndEdit6960(t)

	// POSITIVE CONTROL, and not decoration: it proves the loop re-extracted the
	// file at all. Without it, a pass that silently produced NOTHING for this
	// file would fail the custom assertion below with the same message as a pass
	// that ran only the base extractor, and the two are different bugs.
	if !hasEntity6960(doc, "SCOPE.Operation", "about") {
		t.Fatalf("premise: the edit added `def about()` and the base scala extractor did not "+
			"emit it — the re-extraction did not happen, so this test is measuring nothing. "+
			"entities: %v", entityNames6960(doc))
	}

	if !hasEntity6960(doc, "SCOPE.DI", customOnlyEntity6960) {
		t.Errorf("#6960: SCOPE.DI|%s is absent after an INCREMENTAL pass with "+
			"GRAFEL_INPROC_CUSTOM_EXTRACTORS=1. Only RunCustomExtractors selects the "+
			"custom_scala_di extractor that produces it, so the re-extract loop never "+
			"dispatched the custom pass: the file's custom entities were evicted in Step 5 "+
			"and nothing re-added them. SCOPE.DI entities present: %v",
			customOnlyEntity6960, anyEntityOfKind6960(doc, "SCOPE.DI"))
	}
}

// TestIncrementalSkipsCustomExtractorsWhenTheGateIsOff6960 grades the direction
// an "entities survive" assertion structurally cannot see: entities WRONGLY
// ADDED. The correctness property this path owes is "same graph as a full
// reindex", and with the gate off a full reindex emits no custom entities — so
// an incremental pass that dispatched them unconditionally would be just as
// divergent as one that never dispatched them, only in the opposite direction
// (and would silently pay the +17.5% wall cost the gate exists to withhold).
func TestIncrementalSkipsCustomExtractorsWhenTheGateIsOff6960(t *testing.T) {
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")

	doc := seedAndEdit6960(t)

	if !hasEntity6960(doc, "SCOPE.Operation", "about") {
		t.Fatalf("premise: base re-extraction did not happen; entities: %v", entityNames6960(doc))
	}
	if got := anyEntityOfKind6960(doc, "SCOPE.DI"); len(got) != 0 {
		t.Errorf("#6960: the incremental pass emitted SCOPE.DI entities %v with the gate OFF — "+
			"a default full reindex emits none, so this is a divergence, not a bonus", got)
	}
}

// TestCustomScalaDIIsSelectableAtAllForThisFile6960 is the premise the two tests
// above rest on. Without it, "SCOPE.DI is absent" is satisfied by an extractor
// that emits nothing for this content, and the gate-off test would pass no
// matter what the incremental path did.
func TestCustomScalaDIIsSelectableAtAllForThisFile6960(t *testing.T) {
	ents, errs := extractors.RunCustomExtractors(context.Background(), extractor.FileInput{
		Path:     playControllerRel6960,
		Content:  []byte(playControllerV2_6960),
		Language: "scala",
	})
	for _, e := range errs {
		t.Logf("custom extractor error (non-fatal): %v", e)
	}
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.DI" && e.Name == customOnlyEntity6960 {
			found = true
		}
	}
	if !found {
		var got []string
		for _, e := range ents {
			got = append(got, e.Kind+"|"+e.Name)
		}
		t.Fatalf("premise: RunCustomExtractors on %s emits no SCOPE.DI|%s — the two incremental "+
			"tests above are then vacuous in the gate-off direction. Emitted: %v",
			playControllerRel6960, customOnlyEntity6960, got)
	}

	// And the other half of the premise: the EXACT-KEY dispatch cannot produce
	// it. This is what makes the entity custom-only, i.e. what makes the
	// gate-on assertion a test of RunCustomExtractors rather than of anything
	// the base scala extractor might also happen to emit.
	base, _ := extractors.Extract(context.Background(), extractor.FileInput{
		Path:     playControllerRel6960,
		Content:  []byte(playControllerV2_6960),
		Language: "scala",
	})
	for _, e := range base {
		if e.Kind == "SCOPE.DI" && e.Name == customOnlyEntity6960 {
			t.Fatalf("premise: the BASE scala extractor also emits SCOPE.DI|%s, so the gate-on "+
				"assertion does not distinguish custom dispatch from base extraction",
				customOnlyEntity6960)
		}
	}
}

func entityNames6960(doc *graph.Document) []string {
	out := make([]string, 0, len(doc.Entities))
	for i := range doc.Entities {
		out = append(out, doc.Entities[i].Kind+"|"+doc.Entities[i].Name)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// The nil-tree guard
// ─────────────────────────────────────────────────────────────────────────────

// dart has NO tree-sitter grammar, so the re-extract loop's parse returns
// ErrUnsupportedLanguage and input.TSTree stays nil — while the base dart
// extractor is a source scanner that emits records anyway, so the pass does NOT
// fall back and really does re-extract. That combination is what makes this file
// able to observe the `input.TSTree != nil` guard: on a scala file the tree is
// always there and the guard is unobservable.
const dartClientV1_6960 = `import 'package:flutter/material.dart';

class HomePage extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Text('hi');
  }
}
`

const dartClientV2_6960 = `import 'package:flutter/material.dart';

class HomePage extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Text('hello');
  }
}
`

const dartClientRel6960 = "lib/home_page.dart"

// TestIncrementalHonoursTheNilTreeGuard6960 grades the guard the fix carries
// over from cmd/grafel/index.go:4138, in the permissive direction: WITHOUT it,
// an incremental pass over a file that produced no parse tree emits custom
// entities the full in-process path — which holds the identical
// `file.TSTree != nil` condition — would never produce. That is the same
// divergence as #6960 itself with the sign flipped, and no assertion about
// entities SURVIVING can see it.
func TestIncrementalHonoursTheNilTreeGuard6960(t *testing.T) {
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")

	repo := t.TempDir()
	stateDir := t.TempDir()
	writeFile(t, repo, dartClientRel6960, dartClientV1_6960)
	buildMinimalGraph(t, stateDir, []graph.Entity{{
		ID:   graph.EntityID("test-repo", "SCOPE.Component", "HomePage", dartClientRel6960),
		Name: "HomePage", Kind: "SCOPE.Component", SourceFile: dartClientRel6960, Language: "dart",
	}}, nil)
	seedManifest(t, repo, stateDir)

	writeFile(t, repo, dartClientRel6960, dartClientV2_6960)
	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("pass was NOT incremental — fell back (%s)", res.FallbackReason)
	}
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}

	// PREMISE 1 — the re-extraction happened. Without it the absence asserted
	// below is the absence of everything.
	if !hasEntity6960(doc, "SCOPE.Component", "HomePage") {
		t.Fatalf("premise: the base dart extractor emitted nothing for the re-extracted file; "+
			"entities: %v", entityNames6960(doc))
	}
	// PREMISE 2 — the custom dart extractor WOULD emit for this content, with no
	// tree. Without this the guard assertion is satisfied by an extractor that
	// has nothing to say.
	cus, _ := extractors.RunCustomExtractors(context.Background(), extractor.FileInput{
		Path: dartClientRel6960, Content: []byte(dartClientV2_6960), Language: "dart",
	})
	wouldEmit := false
	for _, e := range cus {
		if e.Kind == "SCOPE.UIComponent" && e.Name == "HomePage" {
			wouldEmit = true
		}
	}
	if !wouldEmit {
		t.Fatalf("premise: RunCustomExtractors(dart) emits no SCOPE.UIComponent|HomePage with a "+
			"nil tree, so this test cannot observe the guard at all; emitted %d records", len(cus))
	}

	if got := anyEntityOfKind6960(doc, "SCOPE.UIComponent"); len(got) != 0 {
		t.Errorf("#6960: the incremental pass dispatched custom extractors on a file with NO parse "+
			"tree and emitted %v — cmd/grafel/index.go:4138 holds the same `file.TSTree != nil` "+
			"guard, so the full path emits none of these and the two paths now disagree", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The merge, not an append
// ─────────────────────────────────────────────────────────────────────────────

// A Spring controller. The base java extractor and the custom java routing
// extractor BOTH emit SCOPE.Operation|UserController.listUsers for it — the same
// (SourceFile, Kind, Name) identity — which is what MergeWithCustom's #6104
// contract exists for and what a plain append would double. Measured on this
// content: MergeWithCustom yields 3 records where append yields 4.
const springControllerV1_6960 = `package com.example;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api")
public class UserController {
    @GetMapping("/users")
    public String listUsers() {
        return "[]";
    }
}
`

const springControllerV2_6960 = `package com.example;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api")
public class UserController {
    @GetMapping("/users")
    public String listUsers() {
        return "[{}]";
    }
}
`

const springControllerRel6960 = "src/main/java/com/example/UserController.java"

// TestIncrementalMergesCustomRecordsRatherThanAppending6960 grades the merge
// call itself. `records = append(records, customEnts...)` compiles, satisfies
// every assertion above (the scala custom records collide with nothing) and
// still ships the #4405/#6104 hazard: two records with one identity, whose
// entity ids are DERIVED from that identity downstream.
func TestIncrementalMergesCustomRecordsRatherThanAppending6960(t *testing.T) {
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")

	repo := t.TempDir()
	stateDir := t.TempDir()
	writeFile(t, repo, springControllerRel6960, springControllerV1_6960)
	buildMinimalGraph(t, stateDir, []graph.Entity{{
		ID:   graph.EntityID("test-repo", "SCOPE.Operation", "UserController.listUsers", springControllerRel6960),
		Name: "UserController.listUsers", Kind: "SCOPE.Operation", SourceFile: springControllerRel6960, Language: "java",
	}}, nil)
	seedManifest(t, repo, stateDir)

	writeFile(t, repo, springControllerRel6960, springControllerV2_6960)
	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("pass was NOT incremental — fell back (%s)", res.FallbackReason)
	}
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}

	// PREMISE — the base and custom passes really do collide on this content.
	// Without it the cardinality assertion below is satisfied by a file where
	// only one producer ever spoke, and append and merge are indistinguishable.
	//
	// The premise is computed WITH a parse tree, because that is the state the
	// re-extract loop is in when it reaches the dispatch: java has a grammar, so
	// input.TSTree is non-nil there, and the base java extractor emits nothing
	// at all without one.
	pf := treesitter.NewParserFactory(nil)
	pr, perr := pf.Parse(context.Background(), []byte(springControllerV2_6960), "java")
	if perr != nil || pr == nil || pr.TSTree == nil {
		t.Fatalf("premise: the java fixture did not parse (%v) — the loop would have taken the "+
			"nil-tree path instead of the dispatch this test grades", perr)
	}
	defer pr.TSTree.Close()
	fi := extractor.FileInput{Path: springControllerRel6960, Content: []byte(springControllerV2_6960), Language: "java", TSTree: pr.TSTree}
	base, _ := extractors.Extract(context.Background(), fi)
	cus, _ := extractors.RunCustomExtractors(context.Background(), fi)
	collides := false
	for _, b := range base {
		for _, c := range cus {
			if b.SourceFile == c.SourceFile && b.Kind == c.Kind && b.Name == c.Name {
				collides = true
			}
		}
	}
	if !collides {
		t.Fatalf("premise: no (SourceFile, Kind, Name) collision between the base and custom java "+
			"records for this file, so append and MergeWithCustom produce the same set and this "+
			"test grades nothing (base=%d custom=%d)", len(base), len(cus))
	}

	n := 0
	var survivor graph.Entity
	for i := range doc.Entities {
		if doc.Entities[i].Kind == "SCOPE.Operation" && doc.Entities[i].Name == "UserController.listUsers" {
			n++
			survivor = doc.Entities[i]
		}
	}
	if n != 1 {
		t.Fatalf("#6960: %d entities named SCOPE.Operation|UserController.listUsers after one "+
			"incremental pass, want exactly 1 — the base and custom producers must not each land "+
			"a record under one derived id", n)
	}

	// CARDINALITY ALONE DOES NOT GRADE THE MERGE, and asserting only n==1 was
	// measured to be exactly that mistake: with `append` in place of
	// MergeWithCustom the downstream entity dedupe still collapses the pair, so
	// n stays 1 and the whole test passes on the mutant. What append LOSES is
	// the #6104 invariant the merge exists for — "a merge never narrows a span".
	// Measured on this fixture: MergeWithCustom keeps the method's real span
	// (lines 8-11, the `@GetMapping` through the closing brace) while append
	// leaves the custom record's single annotation line (8-8), and the flow
	// entity built from the endpoint disappears with it.
	if survivor.EndLine <= survivor.StartLine {
		t.Errorf("#6960: SCOPE.Operation|UserController.listUsers survived one incremental pass "+
			"with a collapsed span (start=%d end=%d). The custom records must go through "+
			"MergeWithCustom, which unions the two producers' spans; a plain append leaves "+
			"whichever record the dedupe happened to keep, narrowing the method to its annotation "+
			"line", survivor.StartLine, survivor.EndLine)
	}
	if !hasEntity6960(doc, "SCOPE.Process", "http:GET:/api/users \u2192 UserController.listUsers") {
		t.Errorf("#6960: the endpoint→handler flow entity is missing after the incremental pass — "+
			"it is derived from the merged operation's span, so it is the downstream casualty of "+
			"an un-merged append. entities: %v", entityNames6960(doc))
	}
}
