package classifier_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/treesitter"
)

// protoSource is a self-contained proto3 file covering every construct the
// proto extractor claims: service, rpc, message, field, enum, enum value and
// import. It is deliberately not a fixture read from disk — the point of this
// test is the dispatch path, not the corpus.
const protoSource = `syntax = "proto3";

package acme.orders.v1;

import "google/protobuf/timestamp.proto";

enum Status {
  STATUS_UNSPECIFIED = 0;
  STATUS_PAID = 1;
}

message Order {
  string id = 1;
  Status status = 2;
}

message GetOrderRequest {
  string id = 1;
}

service OrderService {
  rpc GetOrder(GetOrderRequest) returns (Order);
}
`

// TestProtoRoutesEndToEndThroughDispatch reproduces #6356.
//
// The proto extractor has been complete and unit-tested since it was written,
// but every one of its own tests called the package directly. Production does
// not: it classifies a file, parses it with the classifier's language token,
// and dispatches through extractors.Extract keyed on that same token. The
// classifier said "protobuf"; the extractor registered as "proto"; the
// tree-sitter factory only knew "proto". So .proto files were counted at
// internal/daemon/extract/subproc.go as stats.Skipped++ and produced nothing,
// with a green suite over the whole component.
//
// This test walks the four production steps in order, so it fails on any one
// of the three name mismatches rather than only on the registry lookup.
//
// The GENERAL form of the registry half of this assertion is not repeated
// here: #6327 S2 (c096a4069) landed routedLanguagesWithoutExtractor() in
// classifier_test.go, derived from RoutedLanguagesForTest() x extractors.Get,
// and #6356 adds "protobuf" to classifierRepresentativeInputs so
// TestClassifier_EveryRoutedExtensionHasRegisteredExtractor now FAILS — not
// logs — if the registration key drifts from the classifier token again.
//
// What no other test covers is step 3. S2's machinery never parses, so a
// registry-only fix would leave FileInput.TSTree nil and this extractor
// silently returning nothing. That leg is why this test exists as well as
// the derived one.
func TestProtoRoutesEndToEndThroughDispatch(t *testing.T) {
	ctx := context.Background()
	c := newTestClassifier(t)

	// 1. Classification — the token everything downstream is keyed on.
	res := c.Classify(ctx, "api/orders/v1/orders.proto")
	if res.Skip {
		t.Fatalf(".proto was skipped by the classifier: reason=%q", res.SkipReason)
	}
	if res.Language != "protobuf" {
		t.Fatalf("classifier Language=%q, want %q", res.Language, "protobuf")
	}

	// 2. Registry lookup — the assertion whose absence hid #6356. The proto
	// package's own TestProtoExtractor_Registered asked for "proto", a token
	// no production caller ever passes.
	if _, ok := extractors.Get(res.Language); !ok {
		t.Fatalf("no extractor registered for classifier token %q "+
			"— .proto files will be counted as skipped in subproc.go", res.Language)
	}

	// 3. Parse — subproc.go and incremental.go both call Parse with the
	// classifier token verbatim. A registry fix alone leaves TSTree nil here
	// and the extractor returns no entities, turning one silent failure into
	// another.
	pr, err := treesitter.NewParserFactory(nil).Parse(ctx, []byte(protoSource), res.Language)
	if err != nil {
		t.Fatalf("parse with classifier token %q failed: %v", res.Language, err)
	}
	if pr == nil || pr.TSTree == nil {
		t.Fatalf("parse with classifier token %q returned a nil tree", res.Language)
	}

	// 4. Dispatch — the real thing, with the real FileInput shape.
	ents, err := extractors.Extract(ctx, extractor.FileInput{
		Path:     "api/orders/v1/orders.proto",
		Content:  []byte(protoSource),
		Language: res.Language,
		TSTree:   pr.TSTree,
	})
	if err != nil {
		t.Fatalf("extractors.Extract: %v", err)
	}
	if len(ents) == 0 {
		t.Fatal("dispatch produced 0 entities: lookup succeeded but the extractor emitted nothing")
	}

	rels := 0
	subtypes := map[string]int{}
	for i := range ents {
		rels += len(ents[i].Relationships)
		subtypes[ents[i].Subtype]++
		if ents[i].Language != res.Language {
			t.Errorf("entity %q: Language=%q, want %q (classifier token)",
				ents[i].Name, ents[i].Language, res.Language)
		}
	}
	if rels == 0 {
		t.Error("dispatch produced 0 relationships")
	}

	// Construct coverage: the extractor's package doc promises all four.
	for _, want := range []string{"service", "endpoint", "message", "enum"} {
		if subtypes[want] == 0 {
			t.Errorf("no entity with Subtype=%q emitted; got %v", want, subtypes)
		}
	}
	t.Logf("entities=%d relationships=%d subtypes=%v", len(ents), rels, subtypes)
}
