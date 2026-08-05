package dockerfile

// nil_tree_6154_internal_test.go — the self-parse fallback's nil-tree guard.
//
// #6154 made this fallback reachable (before, a nil TSTree returned at the top
// guard, so the block could never run). That newly-live path ends in
// tree.RootNode(), and ts.Parser.Parse is documented to return "nil if the
// binding produced no tree" (ts/ts.go:136) — official.Parse returns (nil, nil)
// whenever the parse watchdog is disabled (ts/official/official.go:155; it only
// converts a nil tree into ErrParseDeadlineExceeded while the watchdog is
// armed). python (extractor.go:142) and golang (:108) both guard exactly here.
//
// So this is not a hypothetical: making the fallback live is precisely what
// makes a nil dereference reachable, and the guard has to be pinned
// behaviourally rather than by inspection. Removing it turns this test into a
// panic rather than a failed assertion — which is the point.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// nilTreeAdapter produces parsers that return (nil, nil) — the documented
// "binding produced no tree" case, with no error to distinguish it.
type nilTreeAdapter struct{}

func (nilTreeAdapter) Name() string { return "official" }

func (nilTreeAdapter) NewParser(ts.Language) (ts.Parser, error) { return nilTreeParser{}, nil }

type nilTreeParser struct{}

func (nilTreeParser) Parse([]byte) (ts.Tree, error) { return nil, nil }
func (nilTreeParser) Close()                        {}

// TestDockerfileSelfParseGuardsNilTree6154 pins that a nil tree from the
// fallback parse is reported as an error rather than dereferenced.
func TestDockerfileSelfParseGuardsNilTree6154(t *testing.T) {
	prev := dockerfileAdapter
	dockerfileAdapter = nilTreeAdapter{}
	t.Cleanup(func() { dockerfileAdapter = prev })

	e := &Extractor{}
	entities, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "Dockerfile",
		Content:  []byte("FROM ubuntu:22.04\n"),
		Language: "dockerfile",
		TSTree:   nil, // forces the self-parse fallback
	})

	if err == nil {
		t.Fatal("a nil tree from the self-parse fallback returned no error — the guard is " +
			"missing and tree.RootNode() would be a nil dereference (#6154)")
	}
	if entities != nil {
		t.Errorf("expected no entities alongside the error, got %d", len(entities))
	}
}

// TestDockerfileSelfParseClosesItsOwnTree6154 pins the other half of the
// fallback's resource contract: a tree the extractor parsed ITSELF must be
// closed, and a tree handed in by the caller must NOT be — the indexer owns
// that one and closing it would be a double free.
//
// Without the Close the CST leaks for the daemon's lifetime: #5963
// (treesitter/parser.go) measures a tree-sitter CST at ~19.7 bytes of C heap per
// source byte, and the v0.24 binding attaches no finalizer, so nothing reclaims
// it. One leak per Dockerfile, on the watcher-driven reindex path.
func TestDockerfileSelfParseClosesItsOwnTree6154(t *testing.T) {
	const src = "FROM ubuntu:22.04\nEXPOSE 8080\n"

	t.Run("self-parsed tree is closed", func(t *testing.T) {
		real := parseWithRealAdapter(t, src)
		tracked := &closeTrackingTree{Tree: real}

		prev := dockerfileAdapter
		dockerfileAdapter = fixedTreeAdapter{tree: tracked}
		t.Cleanup(func() { dockerfileAdapter = prev })

		e := &Extractor{}
		if _, err := e.Extract(context.Background(), extractor.FileInput{
			Path: "Dockerfile", Content: []byte(src), Language: "dockerfile", TSTree: nil,
		}); err != nil {
			t.Fatalf("extract: %v", err)
		}
		if tracked.closes != 1 {
			t.Errorf("self-parsed tree closed %d times, want exactly 1 — the CST leaks for the "+
				"daemon's lifetime otherwise (#5963, ~19.7 B/source byte, no finalizer)", tracked.closes)
		}
	})

	t.Run("caller-supplied tree is not closed", func(t *testing.T) {
		tracked := &closeTrackingTree{Tree: parseWithRealAdapter(t, src)}
		defer tracked.Tree.Close()

		e := &Extractor{}
		if _, err := e.Extract(context.Background(), extractor.FileInput{
			Path: "Dockerfile", Content: []byte(src), Language: "dockerfile", TSTree: tracked,
		}); err != nil {
			t.Fatalf("extract: %v", err)
		}
		if tracked.closes != 0 {
			t.Errorf("caller-supplied tree closed %d times, want 0 — the indexer owns it and "+
				"closing it here is a double free", tracked.closes)
		}
	})
}

func parseWithRealAdapter(t *testing.T, src string) ts.Tree {
	t.Helper()
	p, err := tsofficial.New().NewParser(dockerfileGrammar())
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer p.Close()
	tree, err := p.Parse([]byte(src))
	if err != nil || tree == nil {
		t.Fatalf("parse: tree=%v err=%v", tree, err)
	}
	return tree
}

type closeTrackingTree struct {
	ts.Tree
	closes int
}

func (c *closeTrackingTree) Close() { c.closes++ }

type fixedTreeAdapter struct{ tree ts.Tree }

func (fixedTreeAdapter) Name() string                               { return "official" }
func (f fixedTreeAdapter) NewParser(ts.Language) (ts.Parser, error) { return fixedTreeParser(f), nil }

type fixedTreeParser struct{ tree ts.Tree }

func (f fixedTreeParser) Parse([]byte) (ts.Tree, error) { return f.tree, nil }
func (fixedTreeParser) Close()                          {}
