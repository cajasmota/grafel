package php_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/php"
	"github.com/cajasmota/grafel/internal/types"
)

// extractPHPForExc parses + extracts PHP source via the registered extractor.
func extractPHPForExc(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("php")
	if !ok {
		t.Fatal("php extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "svc.php",
		Content:  []byte(src),
		Language: "php",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return got
}

// exceptionNode returns the SCOPE.ExceptionType entity for bareType, or fails.
func exceptionNode(t *testing.T, ents []types.EntityRecord, bareType string) types.EntityRecord {
	t.Helper()
	want := extractor.ExceptionTypeName(bareType)
	for _, e := range ents {
		if e.Kind == string(types.EntityKindExceptionType) && e.Name == want {
			if e.SourceFile != extractor.ExceptionTypeSourceFile {
				t.Fatalf("exception node %q SourceFile=%q want synthetic %q",
					bareType, e.SourceFile, extractor.ExceptionTypeSourceFile)
			}
			if e.QualifiedName != extractor.ExceptionTypeTargetID(bareType) {
				t.Fatalf("exception node %q QualifiedName=%q want %q",
					bareType, e.QualifiedName, extractor.ExceptionTypeTargetID(bareType))
			}
			return e
		}
	}
	t.Fatalf("no SCOPE.ExceptionType node for %q", bareType)
	return types.EntityRecord{}
}

// relCount counts edges of kind from the named host entity to bareType's node.
func relCount(ents []types.EntityRecord, hostName, kind, bareType string) int {
	target := extractor.ExceptionTypeTargetID(bareType)
	n := 0
	for _, e := range ents {
		if e.Name != hostName {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == kind && r.ToID == target {
				n++
			}
		}
	}
	return n
}

// TestPHP_ErrorFlow_ThrowAndCatchConverge asserts a type thrown in one method
// and caught in another converge on ONE shared exception node (flagship shape).
func TestPHP_ErrorFlow_ThrowAndCatchConverge(t *testing.T) {
	src := `<?php
namespace App;
class UserService {
  public function find(int $id) {
    throw new NotFoundException("missing");
  }
  public function handle() {
    try { $this->find(1); }
    catch (NotFoundException $e) { }
  }
}
`
	ents := extractPHPForExc(t, src)

	// Exactly ONE convergence node for NotFoundException.
	var nodes int
	for _, e := range ents {
		if e.Kind == string(types.EntityKindExceptionType) &&
			e.Name == extractor.ExceptionTypeName("NotFoundException") {
			nodes++
		}
	}
	if nodes != 1 {
		t.Fatalf("want 1 NotFoundException node, got %d", nodes)
	}
	exceptionNode(t, ents, "NotFoundException")

	// THROWS from UserService.find, CATCHES from UserService.handle — both to
	// the SAME node id.
	if got := relCount(ents, "UserService.find", "THROWS", "NotFoundException"); got != 1 {
		t.Errorf("UserService.find THROWS NotFoundException = %d, want 1", got)
	}
	if got := relCount(ents, "UserService.handle", "CATCHES", "NotFoundException"); got != 1 {
		t.Errorf("UserService.handle CATCHES NotFoundException = %d, want 1", got)
	}
}

// TestPHP_ErrorFlow_UnionMultiCatch asserts PHP 8 union catch yields ONE
// CATCHES edge per type, each to its own node.
func TestPHP_ErrorFlow_UnionMultiCatch(t *testing.T) {
	src := `<?php
class Svc {
  public function run() {
    try { go(); }
    catch (IOException | TimeoutException $e) { }
  }
}
`
	ents := extractPHPForExc(t, src)

	exceptionNode(t, ents, "IOException")
	exceptionNode(t, ents, "TimeoutException")

	if got := relCount(ents, "Svc.run", "CATCHES", "IOException"); got != 1 {
		t.Errorf("Svc.run CATCHES IOException = %d, want 1", got)
	}
	if got := relCount(ents, "Svc.run", "CATCHES", "TimeoutException"); got != 1 {
		t.Errorf("Svc.run CATCHES TimeoutException = %d, want 1", got)
	}
}

// TestPHP_ErrorFlow_QualifiedNormalized asserts a fully-qualified thrown type
// is normalized to its bare leaf.
func TestPHP_ErrorFlow_QualifiedNormalized(t *testing.T) {
	src := `<?php
class Svc {
  public function boom() {
    throw new \App\Errors\Boom();
  }
}
`
	ents := extractPHPForExc(t, src)
	exceptionNode(t, ents, "Boom")
	if got := relCount(ents, "Svc.boom", "THROWS", "Boom"); got != 1 {
		t.Errorf("Svc.boom THROWS Boom = %d, want 1", got)
	}
}

// TestPHP_ErrorFlow_CatchAllBroadGuard asserts broad guards \Throwable /
// \Exception ARE recorded (typed, statically-recoverable) — documented
// convention.
func TestPHP_ErrorFlow_CatchAllBroadGuard(t *testing.T) {
	src := `<?php
class Svc {
  public function swallow() {
    try { go(); }
    catch (\Throwable $e) { }
  }
  public function swallow2() {
    try { go(); }
    catch (\Exception $e) { }
  }
}
`
	ents := extractPHPForExc(t, src)
	exceptionNode(t, ents, "Throwable")
	exceptionNode(t, ents, "Exception")
	if got := relCount(ents, "Svc.swallow", "CATCHES", "Throwable"); got != 1 {
		t.Errorf("Svc.swallow CATCHES Throwable = %d, want 1", got)
	}
	if got := relCount(ents, "Svc.swallow2", "CATCHES", "Exception"); got != 1 {
		t.Errorf("Svc.swallow2 CATCHES Exception = %d, want 1", got)
	}
}

// TestPHP_ErrorFlow_Negatives asserts re-throw, computed throw, and a plain
// method emit NO exception edges / nodes.
func TestPHP_ErrorFlow_Negatives(t *testing.T) {
	src := `<?php
class Svc {
  public function rethrow(\Exception $e) {
    throw $e;
  }
  public function computed() {
    throw $this->makeError();
  }
  public function plain(int $id) {
    return $id + 1;
  }
}
`
	ents := extractPHPForExc(t, src)

	// No THROWS edges at all (re-throw + computed carry no NEW static type).
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "THROWS" {
				t.Errorf("unexpected THROWS edge from %q to %q", e.Name, r.ToID)
			}
		}
	}
	// No exception nodes were fabricated.
	for _, e := range ents {
		if e.Kind == string(types.EntityKindExceptionType) {
			t.Errorf("unexpected exception node %q for negatives-only source", e.Name)
		}
	}
}

// TestPHP_ErrorFlow_FreeFunction asserts edges attach to free-function hosts.
func TestPHP_ErrorFlow_FreeFunction(t *testing.T) {
	src := `<?php
function loadConfig() {
  throw new ConfigError("bad");
}
`
	ents := extractPHPForExc(t, src)
	exceptionNode(t, ents, "ConfigError")
	if got := relCount(ents, "loadConfig", "THROWS", "ConfigError"); got != 1 {
		t.Errorf("loadConfig THROWS ConfigError = %d, want 1", got)
	}
}
