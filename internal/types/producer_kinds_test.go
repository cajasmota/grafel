package types_test

// This test scans the grafel producer codebase for hard-coded `Kind: "..."`
// string literals on Relationship / RelationshipRecord composite literals and
// asserts that every distinct literal value is covered by
// IsValidRelationshipKind.
//
// It is the runtime-free guardrail for Issue #86: it catches typos and stale
// kind strings the moment a producer is compiled into the test binary, with
// zero overhead in the production hot path.
//
// Qualified types (e.g. graph.RelationshipRecord) are accepted.
//
// # The ENTITY half moved (#6776 arm B3)
//
// It used to live here and read only *ast.BasicLit values, so a producer
// writing `Kind: workflowKind` — an identifier naming a package-level string
// constant — was invisible to it. Six SCOPE.*-prefixed kinds drifted out of
// types.AllEntityKinds() behind that blindness. The entity guard now lives in
// producer_entity_kinds_6776_test.go and delegates resolution to
// internal/entkinds.ScanGo, which resolves source constants as well as
// literals. That guard is a strict superset of what this file used to do for
// entities, which is why the entity branch is gone rather than duplicated.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// looksLikeRelationshipKind reports whether s matches the UPPER_SNAKE shape
// used by typed RelationshipKind values (e.g. "CALLS", "PUBLISHES_TO"). The
// scan uses this to decide whether a given relationship `Kind: "..."` literal
// is meant to live in the validator namespace.
func looksLikeRelationshipKind(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r == '_':
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// walkGoFiles collects all .go files under root, skipping testdata/ and
// vendor/ subtrees.
func walkGoFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "testdata" || name == "vendor" || name == "fixtures" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// repoRoot returns the absolute path to the repository root, derived from this
// file's location at <root>/internal/types/producer_kinds_test.go.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// here = <root>/internal/types/producer_kinds_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
}

var relationshipTypeNames = map[string]struct{}{
	"Relationship":       {},
	"RelationshipRecord": {},
}

// kindBucket categorises a composite-literal type expression as
// "relationship" or "" (ignore). Entity literals are the business of
// producer_entity_kinds_6776_test.go.
func kindBucket(typeExpr ast.Expr) string {
	name := compositeTypeName(typeExpr)
	if name == "" {
		return ""
	}
	if _, ok := relationshipTypeNames[name]; ok {
		return "relationship"
	}
	return ""
}

// compositeTypeName returns the trailing identifier of a composite literal's
// type expression: `Entity`, `graph.Entity`, `*Entity`, `[]Entity` all return
// "Entity". Pointer / slice wrappers are unusual on composite literals but we
// handle them defensively.
func compositeTypeName(e ast.Expr) string {
	for {
		switch v := e.(type) {
		case *ast.Ident:
			return v.Name
		case *ast.SelectorExpr:
			return v.Sel.Name
		case *ast.StarExpr:
			e = v.X
		case *ast.ArrayType:
			e = v.Elt
		case *ast.ParenExpr:
			e = v.X
		default:
			return ""
		}
	}
}

// findKindLiterals walks node and collects (bucket, literalValue, position)
// triples for every `Kind: "..."` field appearing inside an Entity-ish or
// Relationship-ish composite literal.
type kindHit struct {
	bucket  string // "relationship"
	value   string
	pos     token.Position
	typeTag string
}

func collectKindHits(fset *token.FileSet, file *ast.File) []kindHit {
	var hits []kindHit
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		bucket := kindBucket(cl.Type)
		if bucket == "" {
			return true
		}
		typeName := compositeTypeName(cl.Type)
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			keyIdent, ok := kv.Key.(*ast.Ident)
			if !ok || keyIdent.Name != "Kind" {
				continue
			}
			lit, ok := kv.Value.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				// Non-literal (constant ref, variable, etc.) — already type-safe
				// or out of scope for this static check.
				continue
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				continue
			}
			hits = append(hits, kindHit{
				bucket:  bucket,
				value:   val,
				pos:     fset.Position(lit.Pos()),
				typeTag: typeName,
			})
		}
		return true
	})
	return hits
}

func TestProducerRelationshipKindLiterals_AreValid(t *testing.T) {
	root := repoRoot(t)
	scanDir := filepath.Join(root, "internal")

	goFiles, err := walkGoFiles(scanDir)
	if err != nil {
		t.Fatalf("walking %s: %v", scanDir, err)
	}
	if len(goFiles) == 0 {
		t.Fatalf("no .go files found under %s", scanDir)
	}

	fset := token.NewFileSet()
	var allHits []kindHit
	for _, path := range goFiles {
		// Skip test files: tests deliberately construct invalid kinds for
		// negative-path coverage, and they are not "producers".
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		allHits = append(allHits, collectKindHits(fset, f)...)
	}

	if len(allHits) == 0 {
		t.Fatal("scanner found zero Kind: \"...\" literals; producer set may have moved")
	}

	// Aggregate bad literals so the failure message lists every offender.
	//
	// The scan enforces the validator only on literals matching the UPPER_SNAKE
	// shape a typed RelationshipKind takes; anything else is a string that
	// happens to sit in a `Kind` field, not a claim about the vocabulary.
	type badKey struct {
		bucket string
		value  string
	}
	bad := map[badKey][]token.Position{}
	for _, h := range allHits {
		var ok bool
		switch h.bucket {
		case "relationship":
			if !looksLikeRelationshipKind(h.value) {
				continue
			}
			ok = types.IsValidRelationshipKind(h.value)
		}
		if !ok {
			k := badKey{bucket: h.bucket, value: h.value}
			bad[k] = append(bad[k], h.pos)
		}
	}

	if len(bad) == 0 {
		t.Logf("scanned %d producer Kind literals, all valid", len(allHits))
		return
	}

	keys := make([]badKey, 0, len(bad))
	for k := range bad {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].bucket != keys[j].bucket {
			return keys[i].bucket < keys[j].bucket
		}
		return keys[i].value < keys[j].value
	})

	var b strings.Builder
	b.WriteString("producer-emitted Kind literals not present in validator set:\n")
	for _, k := range keys {
		positions := bad[k]
		b.WriteString("  ")
		b.WriteString(k.bucket)
		b.WriteString(" Kind: ")
		b.WriteString(strconv.Quote(k.value))
		b.WriteString("\n")
		// Show up to 3 sample call sites so failures stay actionable.
		maxSamples := len(positions)
		if maxSamples > 3 {
			maxSamples = 3
		}
		for i := 0; i < maxSamples; i++ {
			b.WriteString("    at ")
			b.WriteString(positions[i].String())
			b.WriteString("\n")
		}
		if len(positions) > maxSamples {
			b.WriteString("    ... and ")
			b.WriteString(strconv.Itoa(len(positions) - maxSamples))
			b.WriteString(" more\n")
		}
	}
	b.WriteString("Either add the kind to internal/types/kinds.go (and ")
	b.WriteString("AllRelationshipKinds) or fix the producer to use the typed constant.")
	t.Fatal(b.String())
}

// TestProducerKindLiterals_CoverEveryDeclaredKind is the inverse smoke check:
// every declared kind constant should be referenced (as a literal or via the
// typed constant — we only check literals here) by at least one producer or
// be explicitly allow-listed as not-yet-emitted. Today we only assert the
// literal scan is non-empty and that the validator sets themselves are
// non-trivial; the stronger coverage check is deferred to avoid churn from
// kinds added pre-emptively for upcoming extractors.
func TestProducerKindLiterals_ValidatorSetsNonEmpty(t *testing.T) {
	if len(types.AllEntityKinds()) == 0 {
		t.Fatal("AllEntityKinds is empty")
	}
	if len(types.AllRelationshipKinds()) == 0 {
		t.Fatal("AllRelationshipKinds is empty")
	}
}
