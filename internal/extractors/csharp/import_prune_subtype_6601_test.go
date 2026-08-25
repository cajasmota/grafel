package csharp_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6601 — the C# `using` carrier and its prune predicate disagreed on a
// string literal.
//
// `buildImport` emitted `Kind="SCOPE.Component"` with an EMPTY `Subtype`, while
// every prune predicate in internal/resolve/imports.go selects on
//
//	r.Kind == "SCOPE.Component" && r.Subtype == "import"
//
// The kind matched and the subtype never did, so the prune pass did not merely
// fail to remove the carriers — it never looked at them. `considered=0`.
//
// These tests assert the producer/consumer agreement from BOTH ends, because
// either end alone is satisfiable without the graph being fixed:
//
//   - the producer test pins the literal at the emission site;
//   - the consumer test runs the real prune over the real extractor output and
//     asserts the pass CONSIDERED a nonzero number of carriers.
//
// `Considered` is the load-bearing counter, not `Pruned`: a `Pruned` of zero is
// ambiguous (nothing to prune, or nothing looked at), whereas `Considered == 0`
// on a file that demonstrably contains `using` directives is unambiguous, and
// that ambiguity is precisely why the defect survived.
//
// NOTE: the C# extractor returns `(nil, nil)` when `file.TSTree == nil`
// (csharp.go:62), so this fixture MUST be parsed into a real tree-sitter tree —
// a FileInput without one silently measures nothing.
const csharpImportPruneSrc6601 = `
using System.Collections.Generic;
using Microsoft.Extensions.Logging;
using Alias = System.Console;

namespace Billing
{
    public class InvoiceService
    {
        private readonly ILogger _log;

        public List<string> Names()
        {
            Alias.WriteLine("x");
            return new List<string>();
        }
    }
}
`

// extractCSharp6601 parses and extracts the fixture above, failing the test if
// the extractor produced nothing (the nil-tree trap).
func extractCSharp6601(t *testing.T) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, csharpImportPruneSrc6601)
	defer tree.Close()

	ex, ok := extractor.Get("csharp")
	if !ok {
		t.Fatal("csharp extractor not registered")
	}
	recs, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:    "src/Billing/InvoiceService.cs",
		Content: []byte(csharpImportPruneSrc6601),
		TSTree:  tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("extractor returned no records — fixture did not parse (nil TSTree trap)")
	}
	return recs
}

// importCarriers6601 returns every record that carries an IMPORTS edge, which is
// the shape `buildImport` emits regardless of what Subtype it stamps. Selecting
// on the edge rather than on the Subtype is deliberate: selecting on the Subtype
// would make the test agree with whatever the producer happens to say.
func importCarriers6601(recs []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPORTS" {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// TestCSharpImportCarrier_StampsPruneSubtype pins the producer end: every
// import carrier the C# extractor emits must stamp the exact literal the prune
// predicates select on.
func TestCSharpImportCarrier_StampsPruneSubtype(t *testing.T) {
	recs := extractCSharp6601(t)
	carriers := importCarriers6601(recs)
	if len(carriers) == 0 {
		t.Fatal("no IMPORTS-carrying records — fixture has three `using` directives")
	}
	for _, c := range carriers {
		if c.Kind != "SCOPE.Component" {
			t.Errorf("carrier %q: Kind = %q, want SCOPE.Component", c.Name, c.Kind)
		}
		if c.Subtype != "import" {
			t.Errorf("carrier %q: Subtype = %q, want %q — the prune predicates in "+
				"internal/resolve/imports.go select on this literal (#6601)",
				c.Name, c.Subtype, "import")
		}
	}
}

// TestCSharpImportCarrier_PruneConsidersThem pins the consumer end: the real
// prune pass, fed the real extractor output, must LOOK AT the carriers.
func TestCSharpImportCarrier_PruneConsidersThem(t *testing.T) {
	recs := extractCSharp6601(t)
	carriers := importCarriers6601(recs)
	if len(carriers) == 0 {
		t.Fatal("no IMPORTS-carrying records — fixture has three `using` directives")
	}

	_, _, stats := resolve.PruneImportPlaceholders(recs)
	if stats.Considered == 0 {
		t.Fatalf("prune considered=0 over %d C# import carriers — the prune pass "+
			"never looked at them (#6601); pruned=%d", len(carriers), stats.Pruned)
	}
}

// TestCSharpImportCarrier_PruneRemovesThemAll pins the BEHAVIOURAL gain, which
// `Considered` alone does not.
//
// `Considered != 0` proves the prune LOOKS at the carriers. It does not prove it
// ACTS on them: a mutant that increments Considered and then `continue`s on
// `r.Language == "csharp"` keeps every junk carrier in the graph while leaving
// the Considered assertion, the edge-preservation assertion and both package
// suites green (scored while landing #6601 — it survived everything). The whole
// point of the issue is that these entities stop shipping, so that has to be the
// assertion.
//
// The invariant is stated on the OUTPUT rather than on a counter: after the
// prune, the ONLY surviving SCOPE.Component allowed to carry an IMPORTS edge is
// the file-level carrier (Subtype=="file"), which is the hoist DESTINATION and is
// a durable entity in its own right. Every per-import carrier must be gone. That
// is a producer-side property of the C# extractor's own output — it does not
// borrow an internal invariant from internal/resolve — and it is what "the
// carriers no longer ship" actually means.
func TestCSharpImportCarrier_PruneRemovesThemAll(t *testing.T) {
	recs := extractCSharp6601(t)
	carriers := importCarriers6601(recs)
	if len(carriers) == 0 {
		t.Fatal("no IMPORTS-carrying records — fixture has three `using` directives")
	}

	after, _, stats := resolve.PruneImportPlaceholders(recs)

	var survived []string
	for _, r := range after {
		if r.Kind != "SCOPE.Component" || r.Subtype == "file" {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPORTS" {
				survived = append(survived, r.Name+" (subtype="+r.Subtype+")")
				break
			}
		}
	}
	if len(survived) != 0 {
		t.Errorf("%d import carriers survived the prune and still ship in the graph: %v "+
			"(considered=%d pruned=%d) — the prune looked at them and did not act (#6601)",
			len(survived), survived, stats.Considered, stats.Pruned)
	}
	if stats.Pruned < len(carriers) {
		t.Errorf("prune pruned=%d, want >= %d (one per import carrier); considered=%d",
			stats.Pruned, len(carriers), stats.Considered)
	}
}

// TestCSharpImportCarrier_PruneKeepsImportEdges guards the other direction: a
// carrier that is genuinely referenced must not lose its IMPORTS edge to the
// prune. Every edge the extractor emitted has to still be reachable afterwards,
// either hoisted onto the file-level carrier or returned as a standalone rel.
func TestCSharpImportCarrier_PruneKeepsImportEdges(t *testing.T) {
	recs := extractCSharp6601(t)

	countImports := func(rs []types.EntityRecord, standalone []types.RelationshipRecord) int {
		n := 0
		for _, r := range rs {
			for _, rel := range r.Relationships {
				if rel.Kind == "IMPORTS" {
					n++
				}
			}
		}
		for _, rel := range standalone {
			if rel.Kind == "IMPORTS" {
				n++
			}
		}
		return n
	}

	before := countImports(recs, nil)
	if before == 0 {
		t.Fatal("no IMPORTS edges before prune — fixture did not extract")
	}

	after, orphanRels, _ := resolve.PruneImportPlaceholders(recs)
	if got := countImports(after, orphanRels); got != before {
		t.Errorf("IMPORTS edges: before=%d after=%d — the prune dropped edges it "+
			"should have hoisted (#6601)", before, got)
	}
}
