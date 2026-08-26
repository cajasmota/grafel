package engine

import (
	"context"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// routesToEdges returns every ROUTES_TO edge in the result as "from -> to",
// sorted. Asserting on the emitted artefact (the edge set itself) rather than
// on a count means a wrong-but-equal number of edges cannot pass.
func routesToEdges(t *testing.T, rels []relLike) []string {
	t.Helper()
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, r.From+" -> "+r.To)
	}
	sort.Strings(out)
	return out
}

type relLike struct{ From, To string }

func collectRoutesTo(t *testing.T, src, path string) []relLike {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "java",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	var got []relLike
	for _, r := range result.Relationships {
		if r.Kind == "ROUTES_TO" {
			got = append(got, relLike{From: r.FromID, To: r.ToID})
		}
	}
	return got
}

func assertEdgeSet(t *testing.T, got []relLike, want []string) {
	t.Helper()
	gotS := routesToEdges(t, got)
	sort.Strings(want)
	seen := map[string]bool{}
	for _, g := range gotS {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("MISSING ROUTES_TO edge: %s\n  got edges: %v", w, gotS)
		}
	}
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	for _, g := range gotS {
		if !wantSet[g] {
			t.Errorf("UNEXPECTED ROUTES_TO edge: %s\n  got edges: %v", g, gotS)
		}
	}
}

// TestDetect_SpringRoute_SameMethodNameInTwoControllers is the regression pin
// for issue #6498.
//
// Axis VARIED: the controller class (OrdersController vs ReportsController) —
// one carries a class-level @RequestMapping so the AST pass composes it, the
// other does not so the YAML edge is its ONLY edge.
// Axis HELD CONSTANT: the handler method name (`list` in both classes).
//
// Before the fix, `claimedHandlerMethods` was keyed on the bare method name
// and scoped to the file, so OrdersController.list claimed the name `list`
// and ReportsController's legitimate YAML `Route:/reports -> Controller:list`
// edge was dropped — a real endpoint with no route edge, silently.
func TestDetect_SpringRoute_SameMethodNameInTwoControllers(t *testing.T) {
	src := `package com.example.api;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/orders")
public class OrdersController {

    @GetMapping("/list")
    public String list() {
        return "orders";
    }

    @GetMapping("/detail")
    public String detail() {
        return "order";
    }
}

@RestController
public class ReportsController {

    @GetMapping("/reports")
    public String list() {
        return "reports";
    }
}
`
	got := collectRoutesTo(t, src, "src/main/java/com/example/api/Controllers.java")
	assertEdgeSet(t, got, []string{
		// AST-composed edges for the mapped class.
		"Route:/orders/list -> SCOPE.Operation:OrdersController.list",
		"Route:/orders/detail -> SCOPE.Operation:OrdersController.detail",
		// The unmapped sibling class keeps its sole (YAML) edge even though
		// its handler shares the method name `list` with the mapped class.
		"Route:/reports -> Controller:list",
	})
}

// TestDetect_SpringRoute_SamePathTwoControllers is the neighbouring-axis
// control for issue #6498.
//
// Axis VARIED: the handler method name (`ping` vs `legacyPing`).
// Axis HELD CONSTANT: the annotation path (`/ping` in both classes).
//
// This pins the OTHER half of the claim key. A mutant that keys the claim on
// the route path alone (dropping the method half) would swallow
// LegacyPingController's only edge here, reproducing the same silent recall
// loss on the opposite axis.
func TestDetect_SpringRoute_SamePathTwoControllers(t *testing.T) {
	src := `package com.example.api;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/v1")
public class MappedPingController {

    @GetMapping("/ping")
    public String ping() {
        return "v1";
    }
}

@RestController
public class LegacyPingController {

    @GetMapping("/ping")
    public String legacyPing() {
        return "legacy";
    }
}
`
	got := collectRoutesTo(t, src, "src/main/java/com/example/api/Pings.java")
	assertEdgeSet(t, got, []string{
		// The mapped class's YAML twin (Route:/ping -> Controller:ping) is
		// correctly replaced by the composed edge.
		"Route:/v1/ping -> SCOPE.Operation:MappedPingController.ping",
		// The unmapped sibling's distinct handler keeps its sole edge.
		"Route:/ping -> Controller:legacyPing",
	})
}

// TestDetect_SpringRoute_ConcatAmbiguousClaimKey pins the SEPARATOR in the
// claim key (issue #6498).
//
// The two halves of the key are a route path and a method name; a key built by
// bare concatenation is ambiguous. Here `/ab` + `c` and `/a` + `bc` both
// concatenate to `/abc`, so a separator-less key lets the mapped class's claim
// swallow the unmapped sibling's only edge — the same silent recall loss the
// issue is about, arriving through a different door.
//
// Axis VARIED: both halves at once, held at a constant concatenation.
func TestDetect_SpringRoute_ConcatAmbiguousClaimKey(t *testing.T) {
	src := `package com.example.api;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/root")
public class MappedAbController {

    @GetMapping("/ab")
    public String c() {
        return "mapped";
    }
}

@RestController
public class UnmappedAController {

    @GetMapping("/a")
    public String bc() {
        return "unmapped";
    }
}
`
	got := collectRoutesTo(t, src, "src/main/java/com/example/api/Concat.java")
	assertEdgeSet(t, got, []string{
		"Route:/root/ab -> SCOPE.Operation:MappedAbController.c",
		"Route:/a -> Controller:bc",
	})
}
