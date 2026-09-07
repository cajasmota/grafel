package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6975 / #6973 — the EDGE side of the un-gated Blazor extractor.
//
// WHY A COUNT ASSERTION CANNOT SEE THIS DEFECT. On the `aspnetcore-mvc` corpus
// repo, turning the custom-extractor gate ON (GRAFEL_INPROC_CUSTOM_EXTRACTORS=1,
// in-process Index(), macOS/APFS) moved entities 39,765 -> 75,174 (+89.0%)
// while TESTS edges moved 43,621 -> 36,982 (-6,639). #6973 measured explicit
// TESTS at +8 net while 7,221 edges were substituted underneath. Any assertion
// of the form "count went up" or "count did not change" is satisfied by that
// by construction. This test therefore names ONE SPECIFIC EDGE and requires it
// to be present with the gate ON.
//
// THE MECHANISM. `internal/custom/csharp/blazor.go` had no file gate, so
// `var repo = new OrderRepository(_db);` in ordinary C# minted a
// SCOPE.Operation/function literally named `OrderRepository`, in a DIFFERENT
// file from the real class of that name. The two collide in the repo-wide
// byName index (internal/resolve/refs.go indexByName), whose collision arm
// deletes the entry and sets ambigName — so the surviving name resolves to
// nothing and the testmap stub `scope:operation:<file>#<member>` loses its
// byName global fallback. The seed TESTS edge disappears and DeriveTestsWalkUp
// loses its starting point.
//
// SCOPE NOTE. The general declaration-vs-reference eviction in indexByName is
// #6976 and is deliberately NOT addressed here; it accounts for the residual
// edges the file gate does not recover. This test pins only what the gate is
// responsible for.
func TestBlazorGateKeepsTestsEdges6975(t *testing.T) {
	repo := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The production type. Its NAME is the one the false positive duplicated.
	write("src/OrderRepository.cs", `
namespace Shop.Data;

public class OrderRepository
{
    public int Count()
    {
        return 0;
    }
}
`)
	// An ordinary consumer that constructs it. `new OrderRepository(` here is
	// the exact site that used to mint the colliding SCOPE.Operation.
	write("src/OrderService.cs", `
namespace Shop.Services;

public class OrderService
{
    public int Total()
    {
        var repo = new OrderRepository();
        var mapper = new OrderMapper();
        return repo.Count();
    }
}
`)
	// The test that must keep its TESTS edge.
	write("tests/OrderRepositoryTests.cs", `
namespace Shop.Tests;

public class OrderRepositoryTests
{
    [Fact]
    public void CountReturnsZero()
    {
        var repo = new OrderRepository();
        Assert.Equal(0, repo.Count());
    }
}
`)

	testsEdgesFor := func(arm string) map[string]bool {
		t.Helper()
		state := t.TempDir()
		t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
		switch arm {
		case "off":
			t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
		case "on":
			t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
		}
		if err := Index(repo, filepath.Join(state, "graph.json"), "blazor_gate_6975", nil, false, false); err != nil {
			t.Fatalf("Index(arm=%s): %v", arm, err)
		}
		doc, err := graph.LoadGraphFromDir(state)
		if err != nil {
			t.Fatalf("LoadGraphFromDir(arm=%s): %v", arm, err)
		}
		byID := map[string]*graph.Entity{}
		for i := range doc.Entities {
			byID[doc.Entities[i].ID] = &doc.Entities[i]
		}
		out := map[string]bool{}
		for i := range doc.Relationships {
			r := &doc.Relationships[i]
			if r.Kind != "TESTS" {
				continue
			}
			from, ok1 := byID[r.FromID]
			to, ok2 := byID[r.ToID]
			if !ok1 || !ok2 {
				continue
			}
			out[from.Name+" -> "+to.Name] = true
		}
		return out
	}

	// The gate-OFF arm establishes that the edge exists at all, so a
	// gate-ON failure below is attributable to the custom extractors and not
	// to the fixture never having produced the edge. Without this control the
	// assertion could pass vacuously in the wrong direction.
	off := testsEdgesFor("off")
	if len(off) == 0 {
		t.Fatal("control arm (gate OFF) produced no TESTS edges at all; the fixture cannot grade anything")
	}
	on := testsEdgesFor("on")

	// Every TESTS edge present with the gate OFF must survive the gate being
	// turned ON. Named individually — the failure message lists the missing
	// edges, not a delta.
	var lost []string
	for e := range off {
		if !on[e] {
			lost = append(lost, e)
		}
	}
	if len(lost) > 0 {
		t.Errorf("custom extractors destroyed %d TESTS edge(s) that exist without them:\n  %s",
			len(lost), strings.Join(lost, "\n  "))
	}

	// And the specific false positive must be gone: no SCOPE.Operation named
	// `OrderRepository` may be minted from OrderService.cs. This is what makes
	// the edge assertion above non-accidental.
	state := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
	if err := Index(repo, filepath.Join(state, "graph.json"), "blazor_gate_6975", nil, false, false); err != nil {
		t.Fatal(err)
	}
	doc, err := graph.LoadGraphFromDir(state)
	if err != nil {
		t.Fatal(err)
	}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if p, _ := e.PropLookup("provenance"); strings.HasPrefix(p, "INFERRED_FROM_BLAZOR_") &&
			!strings.HasSuffix(e.SourceFile, ".razor") && !strings.HasSuffix(e.SourceFile, ".razor.cs") {
			t.Errorf("blazor provenance %q on non-razor file %s (entity %s %q)",
				p, e.SourceFile, e.Kind, e.Name)
		}
	}
}
