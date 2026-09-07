package proto

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	tsproto "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/proto"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// #6912 arm B, internal half. Everything else about this arm is graded from
// package proto_test through the real Extract; the two tests here need to see
// unexported state and cannot move out.

// protoScalarLiteral is an INDEPENDENT LITERAL of protobuf's scalar set,
// transcribed from the protobuf language spec's scalar-value-types table rather
// than derived from protoScalars (#6975: a table test that reads the production
// collection it is grading can only ever agree with it).
//
// It is used twice: here, to pin the production set namedTypeRefs consults, and
// in TestProtoFieldTypeRefs_EveryScalarProducesNoEdge below, to GENERATE one
// field per scalar so the claim "scalars produce no edge" is asserted per
// member instead of on a hand-picked representative. The #6912 arm B path has
// no scalar blocklist of its own — see field_type_refs.go — so the generated
// test grades the OBSERVABLE claim through Extract, which is the only place the
// claim is true.
var protoScalarLiteral = []string{
	"bool",
	"bytes",
	"double",
	"fixed32",
	"fixed64",
	"float",
	"int32",
	"int64",
	"sfixed32",
	"sfixed64",
	"sint32",
	"sint64",
	"string",
	"uint32",
	"uint64",
}

func TestProtoScalars_MatchesTheSpecList(t *testing.T) {
	got := make([]string, 0, len(protoScalars))
	for k, v := range protoScalars {
		if !v {
			t.Errorf("protoScalars[%q] is false; a false entry is not a scalar and the map is used as a set", k)
		}
		got = append(got, k)
	}
	sort.Strings(got)
	want := append([]string(nil), protoScalarLiteral...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("protoScalars drifted from the spec list\n got: %v\nwant: %v", got, want)
	}
}

// TestProtoFieldTypeCandidates_Rules grades the candidate rule branch by
// branch, at the unit that owns it, so a change of behaviour is attributable
// here as well as visible end-to-end.
//
// The map<> row set is the explicit answer to "what happens to map<K, V>":
// the wrapper is NEVER a candidate and each of K and V is one, subject to the
// same qualified-name rule. Note these are CANDIDATES, not edges — the in-file
// gate runs afterwards, which is why a scalar component appears here and no
// scalar field ever carries an edge (TestProtoFieldTypeRefs_EveryScalarProducesNoEdge).
func TestProtoFieldTypeCandidates_Rules(t *testing.T) {
	cases := []struct {
		ftype string
		want  []string
		why   string
	}{
		{"Order", []string{"Order"}, "bare named type"},
		{"", nil, "empty type string"},
		{"  Order  ", []string{"Order"}, "surrounding whitespace"},
		{"map<string, Order>", []string{"string", "Order"}, "the map WRAPPER is never a candidate; each component is one"},
		{"map<int32, string>", []string{"int32", "string"}, "a map of scalars still yields its components HERE — the in-file gate is what drops them, not this function"},
		{"map<, >", nil, "degenerate map text yields nothing, not an empty-named target"},
		{"demo.Profile", nil, "qualified name is NOT stripped to its trailing segment"},
		{".demo.Profile", nil, "fully-qualified leading-dot form"},
		{"Outer.Inner", nil, "nested message reference is a qualified name"},
		{"map<string, demo.Profile>", []string{"string"}, "a qualified map value is rejected, the scalar key still surfaces"},
		{"string", []string{"string"}, "NO scalar blocklist lives here: `string` is a candidate that the in-file gate then drops, because no .proto declares `message string`. See the header block."},
		{"repeated", []string{"repeated"}, "the label is not filtered HERE — it never reaches here; see TestProtoFieldTypeRefs_LabelIsNeverTheTarget"},
	}
	for _, c := range cases {
		got := protoFieldTypeCandidates(c.ftype)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("protoFieldTypeCandidates(%q) = %v, want %v (%s)", c.ftype, got, c.want, c.why)
		}
	}
}

// extractInternal runs the registered protobuf extractor over one source. It
// duplicates proto_test.go's helper because this file must live in package
// proto to see protoScalars, and the external helper is not visible here.
func extractInternal(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	parser, err := tsofficial.New().NewParser(tsproto.Language())
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	ext, ok := extractor.Get("protobuf")
	if !ok {
		t.Fatal("protobuf extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "protobuf", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// TestProtoFieldTypeRefs_EveryScalarProducesNoEdge is the biggest negative
// direction on this arm, and it is ENUMERATED rather than sampled: the fixture
// is GENERATED from protoScalarLiteral, one field per scalar, so the set cannot
// grow a member that goes ungraded. Scalars are the dominant field population in
// any real .proto file, and an edge for one of them would be a dangling
// `bug-extractor` endpoint per occurrence (#6906).
//
// The message deliberately ALSO declares a same-file message and a field using
// it, as a positive control: without it, "no field-type edges here" would pass
// just as well for a pass that emits nothing at all.
func TestProtoFieldTypeRefs_EveryScalarProducesNoEdge(t *testing.T) {
	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\n\nmessage Control { string x = 1; }\n\nmessage S {\n")
	for i, s := range protoScalarLiteral {
		fmt.Fprintf(&b, "  %s f_%s = %d;\n", s, s, i+1)
	}
	fmt.Fprintf(&b, "  Control control = %d;\n", len(protoScalarLiteral)+1)
	b.WriteString("}\n")

	ents := extractInternal(t, "s.proto", b.String())

	// Positive control on the fixture: every scalar field entity must exist and
	// carry the scalar as its declared type, or the assertion below is vacuous
	// for a reason that has nothing to do with the edge rule.
	byName := map[string]*types.EntityRecord{}
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "field" {
			byName[ents[i].Name] = &ents[i]
		}
	}
	for _, s := range protoScalarLiteral {
		f, ok := byName["S.f_"+s]
		if !ok {
			t.Fatalf("fixture produced no field entity S.f_%s; the %q case is not exercised", s, s)
		}
		if f.Properties["type"] != s {
			t.Fatalf("S.f_%s declared type = %q, want %q", s, f.Properties["type"], s)
		}
		for _, r := range f.Relationships {
			if r.Kind == "REFERENCES" {
				t.Errorf("scalar field S.f_%s must carry no field-type edge, got -> %s", s, r.ToID)
			}
		}
	}

	// Positive control on the PASS: the non-scalar field in the same message
	// must have got its edge, so "no edges for scalars" is not trivially true.
	ctrl, ok := byName["S.control"]
	if !ok {
		t.Fatal("fixture produced no S.control field")
	}
	var ctrlTargets []string
	for _, r := range ctrl.Relationships {
		if r.Kind == "REFERENCES" {
			ctrlTargets = append(ctrlTargets, r.ToID)
		}
	}
	want := []string{"scope:schema:message:proto:s.proto:Control"}
	if strings.Join(ctrlTargets, ",") != strings.Join(want, ",") {
		t.Fatalf("positive control S.control edges = %v, want %v", ctrlTargets, want)
	}
}

// TestProtoFieldTypeRefs_DedupesPerTarget grades the per-field dedupe. It is
// tested at the unit and not end to end because the only type expression that
// names one target twice — `map<Order, Order>` — is INVALID proto3 (a map key
// must be an integral or string scalar), so no fixture can reach it through
// Extract. Mutation scoring found the dedupe alive without this test; grading
// it cost twelve lines, which is cheaper than arguing it cannot happen.
//
// Two edges to one target would double-count the field in every "which fields
// point at Order?" answer and add a second endpoint to the disposition
// denominator for one declaration.
func TestProtoFieldTypeRefs_DedupesPerTarget(t *testing.T) {
	got := protoFieldTypeRefs("m.proto", "both", "map<Order, Order>")
	if len(got) != 1 {
		t.Fatalf("map<Order, Order> produced %d edges, want 1 (deduped): %+v", len(got), got)
	}
	if got[0].ToID != "scope:schema:message:proto:m.proto:Order" {
		t.Fatalf("ToID = %q", got[0].ToID)
	}
	// Positive control: two DIFFERENT targets must still produce two edges, so
	// the assertion above is about dedupe and not about a pass that emits at
	// most one edge per field.
	if two := protoFieldTypeRefs("m.proto", "pair", "map<Key, Order>"); len(two) != 2 {
		t.Fatalf("map<Key, Order> produced %d edges, want 2: %+v", len(two), two)
	}
}
