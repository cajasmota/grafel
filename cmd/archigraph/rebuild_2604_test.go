package main

// TestRebuild_AllPassesProduceExpectedEdges is the integration test mandated
// by #2604 (META-BUG: multiple fixes pass unit tests but don't appear in real
// graph data after archigraph rebuild).
//
// Root cause identified: the testmap extractor's pytest frameworkEntry only
// matched files with test_ prefix or _test suffix in their BASENAME.  Django
// projects (including upvate) place test files under a tests/ directory without
// that prefix (e.g. core/tests/schedule.py, api/tests/views.py).  selectFramework
// therefore returned nil for those files, the extractor emitted 0 entities, and
// test-coverage queries were blind to most of the codebase.
//
// The fix adds a pathHints field to frameworkEntry that matches the full
// repo-relative path (not just the basename) and registers
// `/tests?/.*\.py$` for pytest.  This test asserts the end-to-end effect
// on a mini Django fixture that mirrors the upvate_core tests/ directory layout.
//
// Companion unit tests live in:
//
//	internal/extractors/cross/testmap/extractor_test.go
//	  TestPytest_TestsDirWithoutPrefix_IsIndexed
//	  TestPytest_TestsDirMatchesAnyPath

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestRebuild_AllPassesProduceExpectedEdges runs the full indexer (no passes
// skipped) against a mini Django fixture and asserts the specific edges from
// the four affected fix areas:
//
//  1. Test entities from tests/ directories (fix #2600 / #2604 root cause):
//     core/tests/schedule.py and api/tests/views.py must produce test entities.
//
//  2. HANDLES_SIGNAL edge (fix #2598): @receiver(post_save, sender=Schedule)
//     in core/signals/handlers.py must produce a HANDLES_SIGNAL edge.
//
//  3. Serializer.Meta.model REFERENCES edge (fix #2584/#2578):
//     ScheduleSerializer.Meta.model = Schedule must produce a REFERENCES edge.
func TestRebuild_AllPassesProduceExpectedEdges(t *testing.T) {
	// Use the django_tests_fixture which mirrors the upvate tests/ layout.
	doc := runIndexerOn(t, "testdata/django_tests_fixture", "django_tests_fixture", nil)

	// Build an ID→Entity index for resolving hex IDs in edge assertions.
	entityByID := make(map[string]graph.Entity, len(doc.Entities))
	for _, e := range doc.Entities {
		entityByID[e.ID] = e
	}

	// -----------------------------------------------------------------------
	// 1. Test entities from tests/ directories (root cause of #2604).
	//    Files like core/tests/schedule.py (no test_ prefix) must produce
	//    SCOPE.Pattern entities (the test-coverage entities).  Before the fix,
	//    0 entities were emitted for files in tests/ dirs with no test_ prefix
	//    because selectFramework returned nil — filenameHints only matched
	//    test_*.py / *_test.py basenames.
	// -----------------------------------------------------------------------
	patternEntities := collectPatternEntities(doc)
	if len(patternEntities) == 0 {
		t.Error("fix #2604: expected ≥1 SCOPE.Pattern (test-coverage) entity from tests/ directories " +
			"(core/tests/schedule.py, api/tests/views.py), got 0 — " +
			"pytest pathHints for tests/ directories is not firing")
	}

	// Confirm the specific fixture files produced any entities (any kind).
	// The testmap extractor produces SCOPE.Pattern entities; the Python extractor
	// produces SCOPE.Operation (methods) and SCOPE.Component (classes).
	wantTestFiles := []string{
		"core/tests/schedule.py",
		"api/tests/views.py",
	}
	for _, wantFile := range wantTestFiles {
		if !hasEntityFromFile(doc, wantFile) {
			t.Errorf("fix #2604: expected entities from %q (tests/ dir, no test_ prefix) — "+
				"before fix, only files with test_ prefix were indexed", wantFile)
		}
	}

	// Confirm SCOPE.Pattern entities from tests/ dirs (the actual testmap output).
	wantPatternFiles := []string{"core/tests/schedule.py", "api/tests/views.py"}
	for _, wantFile := range wantPatternFiles {
		found := false
		for _, e := range patternEntities {
			if e.SourceFile == wantFile {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("fix #2604: expected SCOPE.Pattern entity from %q, got none — "+
				"testmap selectFramework not matching tests/ dir paths", wantFile)
		}
	}

	// -----------------------------------------------------------------------
	// 2. HANDLES_SIGNAL edge (fix #2598).
	//    @receiver(post_save, sender=Schedule) in handlers.py must emit a
	//    HANDLES_SIGNAL relationship from replicate_schedule → Schedule.
	//    Before #2598, FromID used "Function:" which the resolver treated as
	//    DispositionDynamic and never resolved. After #2598 it uses
	//    "SCOPE.Operation:" which resolves correctly.
	// -----------------------------------------------------------------------
	handlesSignalEdges := collectEdgesByKind(doc, "HANDLES_SIGNAL")
	if len(handlesSignalEdges) == 0 {
		t.Error("fix #2598: expected ≥1 HANDLES_SIGNAL edge from @receiver(post_save, sender=Schedule), got 0")
	} else {
		// The ToID may be "Class:Schedule" (pre-resolution stub) or a hex ID
		// pointing to the Schedule entity.  Check both.
		found := false
		for _, r := range handlesSignalEdges {
			toName := r.ToID
			if e, ok := entityByID[r.ToID]; ok {
				toName = e.Name
			}
			if strings.Contains(toName, "Schedule") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("fix #2598: HANDLES_SIGNAL edges present (%d) but none target Schedule", len(handlesSignalEdges))
		}
	}

	// -----------------------------------------------------------------------
	// 3. Serializer.Meta.model REFERENCES edge (fix #2584/#2578).
	//    ScheduleSerializer.Meta.model = Schedule must emit a REFERENCES edge
	//    so that graph queries for Schedule also surface its serializers.
	//    The FromID is a hex entity ID; we resolve it via entityByID to get the name.
	// -----------------------------------------------------------------------
	referencesEdges := collectEdgesByKind(doc, "REFERENCES")
	serializerModelRef := false
	for _, r := range referencesEdges {
		// Resolve the source entity name from the hex FromID.
		fromName := r.FromID
		if e, ok := entityByID[r.FromID]; ok {
			fromName = e.Name
		}
		// ToID may be "Class:Schedule" (unresolved stub) or a hex entity ID.
		toName := r.ToID
		if e, ok := entityByID[r.ToID]; ok {
			toName = e.Name
		}
		isFromSerializer := strings.Contains(fromName, "Serializer") || strings.Contains(fromName, "serializer")
		isToModel := strings.Contains(toName, "Schedule") || strings.Contains(toName, "schedule")
		if isFromSerializer && isToModel {
			serializerModelRef = true
			break
		}
	}
	if !serializerModelRef {
		t.Errorf("fix #2584: expected REFERENCES edge from ScheduleSerializer → Schedule (via Meta.model), got none — "+
			"REFERENCES edges present: %d", len(referencesEdges))
	}
}

// collectPatternEntities returns all SCOPE.Pattern entities (testmap output).
func collectPatternEntities(doc *graph.Document) []graph.Entity {
	var out []graph.Entity
	for _, e := range doc.Entities {
		if e.Kind == "SCOPE.Pattern" {
			out = append(out, e)
		}
	}
	return out
}

// hasEntityFromFile returns true when any entity in doc has SourceFile == path.
func hasEntityFromFile(doc *graph.Document, path string) bool {
	for _, e := range doc.Entities {
		if e.SourceFile == path {
			return true
		}
	}
	return false
}

// collectEdgesByKind returns all relationships of the given kind.
func collectEdgesByKind(doc *graph.Document, kind string) []graph.Relationship {
	var out []graph.Relationship
	for _, r := range doc.Relationships {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}
