package ruby_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/ruby"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/types"
)

// extractRuby parses src with the real grammar and runs the registered ruby
// extractor, returning the entity records.
func extractRuby(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("ruby")
	if !ok {
		t.Fatal("ruby extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "ruby", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return recs
}

// edgesByKind returns the set of exception-type names on edges of the given
// kind whose ToID is the canonical convergence target for that type.
func edgesByKind(recs []types.EntityRecord, kind string) map[string]bool {
	out := map[string]bool{}
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == kind {
				if tn := rel.Properties["exception_type"]; tn != "" &&
					rel.ToID == extractor.ExceptionTypeTargetID(tn) {
					out[tn] = true
				}
			}
		}
	}
	return out
}

// convergeNode reports whether a single SCOPE.ExceptionType node exists for
// typeName with the expected synthetic-file convergence identity.
func convergeNode(recs []types.EntityRecord, typeName string) bool {
	want := extractor.ExceptionTypeName(typeName)
	for _, r := range recs {
		if r.Kind == string(types.EntityKindExceptionType) &&
			r.Name == want &&
			r.QualifiedName == extractor.ExceptionTypeTargetID(typeName) &&
			r.SourceFile == extractor.ExceptionTypeSourceFile {
			return true
		}
	}
	return false
}

// hostHasEdge asserts that the operation entity named host carries a THROWS or
// CATCHES edge (kind) targeting typeName's convergence node.
func hostHasEdge(recs []types.EntityRecord, host, kind, typeName string) bool {
	for _, r := range recs {
		if r.Name != host {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == kind && rel.ToID == extractor.ExceptionTypeTargetID(typeName) {
				return true
			}
		}
	}
	return false
}

func TestErrorFlow_RaiseAndRescueConverge(t *testing.T) {
	src := `class Accounts
  def find(id)
    raise NotFoundError, "missing"
  end
  def get(id)
    begin
      find(id)
    rescue NotFoundError => e
      handle(e)
    end
  end
end
`
	recs := extractRuby(t, "app/models/accounts.rb", src)

	if !convergeNode(recs, "NotFoundError") {
		t.Fatal("expected ONE exception:NotFoundError convergence node")
	}
	if !hostHasEdge(recs, "find", "THROWS", "NotFoundError") {
		t.Error("find should THROW NotFoundError")
	}
	if !hostHasEdge(recs, "get", "CATCHES", "NotFoundError") {
		t.Error("get should CATCH NotFoundError")
	}
	// Convergence invariant: THROWS and CATCHES share the SAME ToID.
	throwsTo := ""
	catchesTo := ""
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "THROWS" {
				throwsTo = rel.ToID
			}
			if rel.Kind == "CATCHES" {
				catchesTo = rel.ToID
			}
		}
	}
	if throwsTo == "" || throwsTo != catchesTo ||
		throwsTo != extractor.ExceptionTypeTargetID("NotFoundError") {
		t.Fatalf("THROWS (%q) and CATCHES (%q) must converge on ExceptionTypeTargetID(NotFoundError)", throwsTo, catchesTo)
	}
}

func TestErrorFlow_MultiClassRescue(t *testing.T) {
	src := `class Svc
  def run
    begin
      work
    rescue ArgumentError, TypeError => e
      log(e)
    end
  end
end
`
	recs := extractRuby(t, "svc.rb", src)
	catches := edgesByKind(recs, "CATCHES")
	if !catches["ArgumentError"] || !catches["TypeError"] {
		t.Fatalf("multi-class rescue should CATCH both ArgumentError and TypeError; got %v", catches)
	}
	if !hostHasEdge(recs, "run", "CATCHES", "ArgumentError") ||
		!hostHasEdge(recs, "run", "CATCHES", "TypeError") {
		t.Error("both catches should attach to run")
	}
}

func TestErrorFlow_MethodLevelRescue(t *testing.T) {
	src := `class Svc
  def take
    do_work
  rescue RuntimeError => e
    log(e)
  end
end
`
	recs := extractRuby(t, "svc.rb", src)
	if !hostHasEdge(recs, "take", "CATCHES", "RuntimeError") {
		t.Fatal("method-level (implicit begin) rescue should CATCH RuntimeError on take")
	}
}

func TestErrorFlow_RescueFrom(t *testing.T) {
	src := `class FooController < ApplicationController
  rescue_from RecordNotFound, with: :not_found
  rescue_from ArgumentError, RangeError, with: :bad
end
`
	recs := extractRuby(t, "app/controllers/foo_controller.rb", src)
	catches := edgesByKind(recs, "CATCHES")
	for _, want := range []string{"RecordNotFound", "ArgumentError", "RangeError"} {
		if !catches[want] {
			t.Errorf("rescue_from should CATCH %s; got %v", want, catches)
		}
	}
	// The `with:` symbol handler must NOT be treated as a type.
	if catches["not_found"] || catches["bad"] {
		t.Error("with: handler symbol must not become an exception type")
	}
}

func TestErrorFlow_ScopedConstantNormalizes(t *testing.T) {
	src := `class Svc
  def boom
    raise MyApp::NotFoundError, "x"
  end
end
`
	recs := extractRuby(t, "svc.rb", src)
	if !hostHasEdge(recs, "boom", "THROWS", "NotFoundError") {
		t.Fatal("raise MyApp::NotFoundError should THROW NotFoundError (last segment)")
	}
	if !convergeNode(recs, "NotFoundError") {
		t.Fatal("scoped raise should converge on exception:NotFoundError node")
	}
}

// --- Negatives: precision-first, honest-partial ---

func TestErrorFlow_Negatives(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"string_raise", `class S
  def m
    raise "plain message"
  end
end
`},
		{"bare_rescue_catchall", `class S
  def m
    begin
      work
    rescue => e
      log(e)
    end
  end
end
`},
		{"untyped_bare_rescue", `class S
  def m
    work
  rescue
    log
  end
end
`},
		{"plain_method", `class S
  def m
    do_work
  end
end
`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recs := extractRuby(t, "s.rb", c.src)
			for _, r := range recs {
				if r.Kind == string(types.EntityKindExceptionType) {
					t.Errorf("%s: unexpected exception-type node %q", c.name, r.Name)
				}
				for _, rel := range r.Relationships {
					if rel.Kind == "THROWS" {
						t.Errorf("%s: unexpected THROWS edge %s", c.name, rel.ToID)
					}
					if rel.Kind == "CATCHES" {
						t.Errorf("%s: unexpected CATCHES edge %s", c.name, rel.ToID)
					}
				}
			}
		})
	}
}

// TestErrorFlow_BareReraiseAddsNoType: a typed rescue followed by bare `raise`
// keeps ONLY the rescued type — the re-raise carries no NEW exception type.
func TestErrorFlow_BareReraiseAddsNoType(t *testing.T) {
	src := `class S
  def m
    begin
      work
    rescue NotFoundError => e
      raise
    end
  end
end
`
	recs := extractRuby(t, "s.rb", src)
	if !hostHasEdge(recs, "m", "CATCHES", "NotFoundError") {
		t.Fatal("typed rescue should still CATCH NotFoundError")
	}
	if throws := edgesByKind(recs, "THROWS"); len(throws) != 0 {
		t.Fatalf("bare re-raise must add no THROWS type; got %v", throws)
	}
}

// TestLiveFireRb drives the FULL production path: ParserFactory.Parse for a real
// app/*.rb file, then the registry-resolved ruby extractor's Extract, confirming
// error_flow fires live (THROWS + CATCHES converge on one node).
func TestLiveFireRb(t *testing.T) {
	src := []byte("class Accounts\n  def find(id)\n    raise NotFoundError, \"x\"\n  end\n  def get(id)\n    begin\n      find(id)\n    rescue NotFoundError => e\n      e\n    end\n  end\nend\n")
	pf := treesitter.NewParserFactory(noop.NewTracerProvider().Tracer("t"))
	res, err := pf.Parse(context.Background(), src, "ruby")
	if err != nil {
		t.Fatal(err)
	}
	ext, ok := extractor.Get("ruby")
	if !ok {
		t.Fatal("ruby extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "app/models/accounts.rb", Content: src, Language: "ruby", TSTree: res.TSTree,
	})
	if err != nil {
		t.Fatal(err)
	}
	throws := hostHasEdge(recs, "find", "THROWS", "NotFoundError")
	catches := hostHasEdge(recs, "get", "CATCHES", "NotFoundError")
	node := convergeNode(recs, "NotFoundError")
	t.Logf("LIVE .rb fire: throws=%v catches=%v convergeNode=%v", throws, catches, node)
	if !(throws && catches && node) {
		t.Fatal("error_flow did NOT fire live on .rb through ParserFactory + registered extractor")
	}
}
