package graphql_test

// #6370 D1 — graphql must have exactly ONE producer of IMPLEMENTS.
//
// THE DEFECT THIS PINS. #6370 added IMPLEMENTS emission to
// internal/extractors/graphql while internal/engine/rules/graphql/frameworks/
// graphql_schema.yaml was already emitting the kind from a
// `type\s+(\w+)\s+implements\s+(\w+)` relationship rule. Nothing connected the
// two: the extractor's own tests see only the extractor's output, and the
// golden fixture grades bare-name and resolved rows independently, so a
// duplicated fact scores twice instead of failing. Measured on
// internal/quality/golden/graphql-schema-mini with --keep-graph, a schema
// declaring SIX `implements` facts produced EIGHT IMPLEMENTS edges: `User ->
// Node` and `Post -> Node` each appeared once from each producer.
//
// THE RULE. Over the golden fixture's own source, the two producers together
// emit one edge per declared fact and no (owner, target) pair twice. That is
// asserted from BOTH sides — the pack contributes zero, and the union has no
// duplicates — because a test that only counted the union would go on passing
// if the pack came back and the extractor regressed by the same amount.
//
// This test is the only thing in the tree that observes both producers at
// once, which is precisely why the duplication survived review of the
// extractor alone.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/extractor"
)

// goldenFixtureSchema is the graded input, read from the fixture rather than
// inlined: an inline copy would drift from the file the gate actually scores,
// and this test's whole subject is that two independent readers agree about
// THAT file.
const goldenFixtureSchema = "../../quality/golden/graphql-schema-mini/src/schema.graphql"

func TestGraphQLImplementsHasExactlyOneProducer(t *testing.T) {
	src, err := os.ReadFile(goldenFixtureSchema)
	if err != nil {
		t.Fatalf("read the golden fixture source: %v\n"+
			"this test grades the file the extraction-quality gate scores; if the fixture "+
			"moved, point this path at it rather than inlining a copy", err)
	}
	path := filepath.Base(goldenFixtureSchema)

	// Producer 1 — the YAML rule pack.
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	res, err := engine.New(rules).Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  src,
		Language: "graphql",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	var packPairs []string
	for _, r := range res.Relationships {
		if r.Kind == "IMPLEMENTS" {
			packPairs = append(packPairs, r.FromID+" -> "+r.ToID)
		}
	}
	if len(packPairs) != 0 {
		sort.Strings(packPairs)
		t.Errorf("the YAML rule pack emitted %d IMPLEMENTS edge(s) for graphql: %s\n"+
			"internal/extractors/graphql is the producer since #6370; a second one makes "+
			"the graph report the same `implements` twice. See the comment where the rule "+
			"was removed in internal/engine/rules/graphql/frameworks/graphql_schema.yaml.",
			len(packPairs), strings.Join(packPairs, ", "))
	}

	// Producer 2 — this extractor.
	ents := extractGQL(t, path, string(src))
	var extractorPairs []string
	for owner, rels := range implementsByOwner(ents) {
		for _, r := range rels {
			extractorPairs = append(extractorPairs, owner+" -> "+r.ToID)
		}
	}
	sort.Strings(extractorPairs)
	want := []string{
		"Named -> Node",
		"Post -> Node",
		"Post -> Timestamped",
		"User -> Named",
		"User -> Node",
		"User -> Timestamped",
	}
	if strings.Join(extractorPairs, "|") != strings.Join(want, "|") {
		t.Errorf("extractor IMPLEMENTS pairs = %v, want %v — the fixture declares exactly "+
			"these six facts", extractorPairs, want)
	}

	// And the union: one edge per declared fact, nothing twice.
	count := map[string]int{}
	for _, p := range append(append([]string{}, packPairs...), extractorPairs...) {
		count[p]++
	}
	var dupes []string
	for p, n := range count {
		if n > 1 {
			dupes = append(dupes, p)
		}
	}
	sort.Strings(dupes)
	if len(dupes) != 0 {
		t.Errorf("the same IMPLEMENTS fact is emitted by more than one producer: %v\n"+
			"a consumer asking what a type implements gets the answer twice", dupes)
	}
	if total := len(packPairs) + len(extractorPairs); total != 6 {
		t.Errorf("total IMPLEMENTS edges over the fixture = %d, want 6 — one per declared "+
			"`implements` fact", total)
	}
}
