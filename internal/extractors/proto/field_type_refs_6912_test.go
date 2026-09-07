package proto_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6912 arm B — the field→declared-type edge for protobuf. See
// field_type_refs.go for the kind / address / same-file decisions; this file
// grades them.
//
// Every assertion below names FIELDS AND TARGETS. A count cannot see a
// substitution, and recall cannot see over-firing at all — and over-firing is
// the failure mode that hurts here, because a dangling stub is kept verbatim
// and counted as one more `bug-extractor` endpoint (#6906). So the forbidden
// set — every scalar, an imported type, a qualified type, a nested-qualified
// type, an undeclared type, the `map<>` wrapper and the proto labels — is
// asserted BY NAME beside the expected set.
//
// The per-scalar enumeration lives in field_type_refs_6912_internal_test.go,
// which generates one field per member of the production scalar set.

const ftrOwnerPath = "svc/user.proto"

// ftrOwnerSrc packs one case per branch of the rule. Read beside
// TestProtoFieldTypeRefs_EmittedEdges and
// TestProtoFieldTypeRefs_ForbiddenTargets, which grade the two directions of
// the SAME fixture.
const ftrOwnerSrc = `syntax = "proto3";

package demo;

import "common.proto";

enum Role {
  ROLE_UNKNOWN = 0;
  ROLE_ADMIN = 1;
}

message Profile { string display_name = 1; }

message Order { string id = 1; }

message User {
  string email = 1;
  int32 age = 2;
  Profile profile = 3;
  Role role = 4;
  repeated Order orders = 5;
  optional Profile backup = 6;
  map<string, Order> by_id = 7;
  map<int32, string> tags = 8;
  Address address = 9;
  demo.Profile qualified = 10;
  .demo.Profile fq = 11;
  Missing gone = 12;
  User parent = 13;
  oneof choice {
    Profile alt = 14;
    string note = 15;
  }
}

message Outer {
  message Inner { string v = 1; }
  Inner direct = 1;
  Outer.Inner deep = 2;
}
`

// ftrRivalPath / ftrRivalSrc declare messages under the SAME bare names in a
// different file. Their only job is to prove the address is file-scoped: the
// edges emitted for svc/user.proto must not change when they are present.
const ftrRivalPath = "other/rival.proto"

const ftrRivalSrc = `syntax = "proto3";

package rival;

message Profile { string other = 1; }

message Order { string id = 1; }

message User { Profile profile = 1; }
`

// ftrExtract runs the extractor over each file independently, exactly as pass 1
// does, and concatenates the records.
func ftrExtract(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []types.EntityRecord
	for _, p := range paths {
		out = append(out, extract(t, p, files[p])...)
	}
	return out
}

// ftrEdges returns "<record name> -> <ToID>" for every REFERENCES edge carrying
// ref_kind=field_target_type, across ALL records — so an edge that escaped onto
// the message, the file entity or an enum is still seen rather than filtered
// out of the picture. It fails on a non-empty FromID: the Django precedent
// leaves it empty so assembly anchors the edge on the record that carries it.
func ftrEdges(t *testing.T, recs []types.EntityRecord) []string {
	t.Helper()
	var out []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || ftrProp(r.Properties, "ref_kind") != "field_target_type" {
				continue
			}
			if r.FromID != "" {
				t.Errorf("%s -[REFERENCES]-> %s has FromID=%q, want empty",
					recs[i].Name, r.ToID, r.FromID)
			}
			if recs[i].Kind != "SCOPE.Schema" || recs[i].Subtype != "field" {
				t.Errorf("field-type edge escaped onto %s/%s %q; it must be carried by the FIELD",
					recs[i].Kind, recs[i].Subtype, recs[i].Name)
			}
			out = append(out, recs[i].Name+" -> "+r.ToID)
		}
	}
	sort.Strings(out)
	return out
}

func ftrProp(p types.Props, key string) string {
	for _, kv := range p {
		if kv.K == key {
			return kv.V
		}
	}
	return ""
}

// TestProtoFieldTypeRefs_EmittedEdges is the positive direction, written as an
// INDEPENDENT LITERAL set (#6975) rather than derived from the extractor's own
// target index. Each ToID is spelled out, so a change of address dialect, of
// target node, or of owning field fails here instead of passing silently.
func TestProtoFieldTypeRefs_EmittedEdges(t *testing.T) {
	recs := ftrExtract(t, map[string]string{ftrOwnerPath: ftrOwnerSrc})
	want := []string{
		// A field whose type is a nested message, written BARE, binds — the
		// nested message is its own top-level-addressed entity.
		"Outer.direct -> scope:schema:message:proto:svc/user.proto:Inner",
		// oneof member: recovered by #6358 and edged like any other field.
		"User.alt -> scope:schema:message:proto:svc/user.proto:Profile",
		// `optional` label: the target is the TYPE, not the label.
		"User.backup -> scope:schema:message:proto:svc/user.proto:Profile",
		// map<string, Order>: ONE row. Not two, and never a `map` target.
		"User.by_id -> scope:schema:message:proto:svc/user.proto:Order",
		// `repeated` label: same as `optional` above.
		"User.orders -> scope:schema:message:proto:svc/user.proto:Order",
		// Self-reference.
		"User.parent -> scope:schema:message:proto:svc/user.proto:User",
		"User.profile -> scope:schema:message:proto:svc/user.proto:Profile",
		// An in-file ENUM is a target, and takes the same address dialect as a
		// message — protobuf enums are SCOPE.Schema/enum, so there is no second
		// dialect here the way arm A needed one for SCOPE.Enum.
		"User.role -> scope:schema:message:proto:svc/user.proto:Role",
	}
	sort.Strings(want)
	got := ftrEdges(t, recs)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("field-type edges mismatch\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestProtoFieldTypeRefs_ForbiddenTargets is the negative direction, by name.
func TestProtoFieldTypeRefs_ForbiddenTargets(t *testing.T) {
	recs := ftrExtract(t, map[string]string{ftrOwnerPath: ftrOwnerSrc})
	edges := ftrEdges(t, recs)

	// FIELDS that must carry no field-type edge at all, each for a stated
	// reason. Scalars are enumerated over the whole production set in
	// field_type_refs_6912_internal_test.go; the two here keep the reason
	// visible in the fixture that also holds the positive rows.
	for _, c := range []struct{ field, why string }{
		{"User.email", "scalar (string)"},
		{"User.age", "scalar (int32)"},
		{"User.note", "scalar inside a oneof"},
		{"User.tags", "map<int32, string> — both components are scalars"},
		{"User.address", "IMPORTED type: Address is declared in common.proto, not here"},
		{"User.qualified", "QUALIFIED name demo.Profile — not stripped to the same-file Profile"},
		{"User.fq", "fully-qualified .demo.Profile"},
		{"User.gone", "Missing is declared nowhere at all"},
		{"Outer.deep", "Outer.Inner — a nested reference is a qualified name"},
		{"Profile.display_name", "scalar"},
		{"Order.id", "scalar"},
		{"Inner.v", "scalar"},
	} {
		for _, e := range edges {
			if strings.HasPrefix(e, c.field+" -> ") {
				t.Errorf("%s must have no field-type edge (%s), got %q", c.field, c.why, e)
			}
		}
	}

	// TARGET NAMES that must never appear, whichever field reached them. The
	// three proto labels are here because they are prepended to the SIGNATURE
	// and a pass that read the signature instead of the type would target them.
	for _, banned := range []string{
		"map", "Address", "Missing", "demo", "string", "int32", "bytes",
		"repeated", "optional", "required",
	} {
		for _, e := range edges {
			if strings.HasSuffix(e, ":"+banned) {
				t.Errorf("no field-type edge may target %q, got %q", banned, e)
			}
		}
	}
	// The map<> wrapper must not survive into a ToID in any spelling.
	for _, e := range edges {
		if strings.Contains(e, "map<") {
			t.Errorf("the map<> wrapper reached a ToID: %q", e)
		}
	}
}

// TestProtoFieldTypeRefs_LabelIsNeverTheTarget isolates the label branch with a
// positive control, so "no edge targets `repeated`" is not passing merely
// because the fixture happens not to produce one.
func TestProtoFieldTypeRefs_LabelIsNeverTheTarget(t *testing.T) {
	const src = `syntax = "proto3";

message Order { string id = 1; }

message Bag {
  repeated Order a = 1;
  optional Order b = 2;
  Order c = 3;
}
`
	recs := ftrExtract(t, map[string]string{"b.proto": src})
	// All three fields must reach the SAME target: the label changes nothing.
	want := []string{
		"Bag.a -> scope:schema:message:proto:b.proto:Order",
		"Bag.b -> scope:schema:message:proto:b.proto:Order",
		"Bag.c -> scope:schema:message:proto:b.proto:Order",
	}
	if got := ftrEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("label handling changed the target\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Positive control on the fixture: the labels really are on these fields,
	// so the assertion above is about label HANDLING and not about a fixture
	// that silently lost its labels.
	labels := map[string]string{}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" {
			labels[recs[i].Name] = recs[i].Properties["label"]
		}
	}
	if labels["Bag.a"] != "repeated" || labels["Bag.b"] != "optional" || labels["Bag.c"] != "" {
		t.Fatalf("fixture labels = %v; the labelled cases are not exercised", labels)
	}
}

// TestProtoFieldTypeRefs_MapValueIsTheTarget grades every branch of the map<>
// rule stated in protoFieldTypeCandidates' doc, end to end.
func TestProtoFieldTypeRefs_MapValueIsTheTarget(t *testing.T) {
	const src = `syntax = "proto3";

message Order { string id = 1; }

message Bag {
  map<string, Order> by_name = 1;
  map<int32, string> scalars = 2;
  map<string, Missing> unknown = 3;
}
`
	recs := ftrExtract(t, map[string]string{"m.proto": src})
	want := []string{"Bag.by_name -> scope:schema:message:proto:m.proto:Order"}
	if got := ftrEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("map handling\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Positive control: all three map fields exist and carry a map<> type, so
	// the two silent rows are silent for the right reason.
	types_ := map[string]string{}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" {
			types_[recs[i].Name] = recs[i].Properties["type"]
		}
	}
	for _, f := range []string{"Bag.by_name", "Bag.scalars", "Bag.unknown"} {
		if !strings.HasPrefix(types_[f], "map<") {
			t.Fatalf("%s declared type = %q, want a map<...>; the map case is not exercised",
				f, types_[f])
		}
	}
}

// TestProtoFieldTypeRefs_CrossFileTypeGetsNoEdge states the deliberate cost of
// the same-file rule as an assertion rather than as prose, and in doing so
// grades the gate this arm REUSES (dropUnresolvableTypeRefs, #6357) for the new
// anchor: an imported type is the majority case in real protobuf, and an edge
// pass 1 cannot verify would dangle.
func TestProtoFieldTypeRefs_CrossFileTypeGetsNoEdge(t *testing.T) {
	recs := ftrExtract(t, map[string]string{
		"svc/user.proto":   "syntax = \"proto3\";\n\nimport \"svc/common.proto\";\n\nmessage User { Address address = 1; }\n",
		"svc/common.proto": "syntax = \"proto3\";\n\nmessage Address { string city = 1; }\n",
	})
	// Positive control: the field and the target message both exist, so "no
	// edge" is not trivially true for the wrong reason.
	var haveField, haveTarget bool
	for i := range recs {
		if recs[i].Name == "User.address" && recs[i].Subtype == "field" {
			haveField = true
		}
		if recs[i].Name == "Address" && recs[i].Subtype == "message" {
			haveTarget = true
		}
	}
	if !haveField || !haveTarget {
		t.Fatalf("fixture incomplete (field=%v target=%v); the cross-file case is not exercised",
			haveField, haveTarget)
	}
	if got := ftrEdges(t, recs); len(got) != 0 {
		t.Fatalf("a type declared in another file must produce no edge, got %v", got)
	}
}

// TestProtoFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName pins the ONE
// place the field edge is deliberately NARROWER than the message→type edge that
// already existed, and pins that this arm did not change the older edge.
//
// namedTypeRefs strips `demo.Profile` to `Profile`, so the MESSAGE-level edge
// binds to this file's own Profile even though the field names a type in
// another package. protoFieldTypeCandidates does not strip, so the FIELD-level
// edge is absent. The message row here is EXISTING behaviour recorded, not
// endorsed: it is a silent mis-bind, and narrowing it belongs with the
// cross-file package index #6357 defers.
func TestProtoFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName(t *testing.T) {
	const src = `syntax = "proto3";

package demo;

message Profile { string display_name = 1; }

message Holder {
  demo.Profile qualified = 1;
  Profile local = 2;
}
`
	recs := ftrExtract(t, map[string]string{"h.proto": src})

	// The FIELD edge exists only for the bare-name field.
	want := []string{"Holder.local -> scope:schema:message:proto:h.proto:Profile"}
	if got := ftrEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("qualified-name field edges\n got: %v\nwant: %v", got, want)
	}

	// The pre-existing MESSAGE edge is unchanged: it still reaches Profile via
	// the qualified field. If a later pass narrows namedTypeRefs, this row goes
	// away and this test is where that shows up.
	var viaFields []string
	for i := range recs {
		if recs[i].Name != "Holder" {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && ftrProp(r.Properties, "ref_kind") == "" {
				viaFields = append(viaFields, ftrProp(r.Properties, "via_field")+" -> "+r.ToID)
			}
		}
	}
	sort.Strings(viaFields)
	// One row: the message-level dedupe is per TARGET, so `qualified` (first in
	// source order) mints it and `local` collapses into it.
	wantMsg := []string{"qualified -> scope:schema:message:proto:h.proto:Profile"}
	if strings.Join(viaFields, "\n") != strings.Join(wantMsg, "\n") {
		t.Fatalf("this arm changed the pre-existing message→type edge\n got: %v\nwant: %v",
			viaFields, wantMsg)
	}
}

// TestProtoFieldTypeRefs_SameNameInAnotherFileChangesNothing is the reason the
// address is structural and file-scoped rather than a bare name: a rival .proto
// declaring Profile / Order / User must not perturb this file's edges.
func TestProtoFieldTypeRefs_SameNameInAnotherFileChangesNothing(t *testing.T) {
	alone := ftrEdges(t, ftrExtract(t, map[string]string{ftrOwnerPath: ftrOwnerSrc}))
	withRival := ftrEdges(t, ftrExtract(t, map[string]string{
		ftrOwnerPath: ftrOwnerSrc,
		ftrRivalPath: ftrRivalSrc,
	}))
	const rivalOwn = "User.profile -> scope:schema:message:proto:other/rival.proto:Profile"
	var got []string
	seenRival := false
	for _, e := range withRival {
		if e == rivalOwn && !seenRival {
			seenRival = true
			continue
		}
		got = append(got, e)
	}
	if !seenRival {
		t.Fatalf("the rival file's own edge is missing; the fixture is not exercising a rival: %v", withRival)
	}
	if strings.Join(got, "\n") != strings.Join(alone, "\n") {
		t.Fatalf("a same-named message in another file changed this file's edges\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(alone, "\n"))
	}
}

// TestProtoFieldTypeRefs_ResolverBindsEveryEdge is the gate this whole issue
// class turns on: #6906 exists because a comment described a binding nobody
// built. Arm A established that the resolver binds a field→type REFERENCES edge
// in general; this drives PROTOBUF entity kinds (SCOPE.Schema/message and
// SCOPE.Schema/enum) and the PROTOBUF address dialect
// (scope:schema:message:proto:...) through the production resolver
// (BuildIndex → ReferencesEmbedded, exactly as graph assembly does) and asserts
// every ToID comes back rewritten to a real entity ID.
//
// Zero dangling is the assertion, not a ratio: a dangling stub is kept verbatim
// and is one more `bug-extractor` endpoint.
func TestProtoFieldTypeRefs_ResolverBindsEveryEdge(t *testing.T) {
	recs := ftrExtract(t, map[string]string{
		ftrOwnerPath: ftrOwnerSrc,
		ftrRivalPath: ftrRivalSrc,
	})
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	idx := resolve.BuildIndex(recs)

	// Positive control on the address dialect: the BARE name of a target is
	// AMBIGUOUS across these two files, which is exactly what the structural
	// form exists to avoid. If it ever binds, the ambiguity this test controls
	// for is absent and the address choice is ungraded.
	if _, st := idx.LookupStatusHint("Profile", "REFERENCES"); st == 1 {
		t.Fatal("the bare name \"Profile\" binds in this fixture, so the file-scoped " +
			"address is ungraded here")
	}

	idToName := map[string]string{}
	for i := range recs {
		idToName[recs[i].ID] = recs[i].SourceFile + ":" + recs[i].Kind + "/" + recs[i].Subtype + ":" + recs[i].Name
	}

	before := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && ftrProp(r.Properties, "ref_kind") == "field_target_type" {
				before++
			}
		}
	}
	if before == 0 {
		t.Fatal("no field-type edges to drive through the resolver")
	}

	resolve.ReferencesEmbedded(recs, idx)

	var bound []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || ftrProp(r.Properties, "ref_kind") != "field_target_type" {
				continue
			}
			target, ok := idToName[r.ToID]
			if !ok {
				t.Errorf("%s -[REFERENCES]-> %q did NOT bind to an entity ID (dangling stub)",
					recs[i].Name, r.ToID)
				continue
			}
			bound = append(bound, recs[i].SourceFile+":"+recs[i].Name+" => "+target)
		}
	}
	sort.Strings(bound)
	want := []string{
		"other/rival.proto:User.profile => other/rival.proto:SCOPE.Schema/message:Profile",
		"svc/user.proto:Outer.direct => svc/user.proto:SCOPE.Schema/message:Inner",
		"svc/user.proto:User.alt => svc/user.proto:SCOPE.Schema/message:Profile",
		"svc/user.proto:User.backup => svc/user.proto:SCOPE.Schema/message:Profile",
		"svc/user.proto:User.by_id => svc/user.proto:SCOPE.Schema/message:Order",
		"svc/user.proto:User.orders => svc/user.proto:SCOPE.Schema/message:Order",
		"svc/user.proto:User.parent => svc/user.proto:SCOPE.Schema/message:User",
		"svc/user.proto:User.profile => svc/user.proto:SCOPE.Schema/message:Profile",
		// The enum target, resolved to the SCOPE.Schema/enum record.
		"svc/user.proto:User.role => svc/user.proto:SCOPE.Schema/enum:Role",
	}
	sort.Strings(want)
	if strings.Join(bound, "\n") != strings.Join(want, "\n") {
		t.Fatalf("resolved endpoints mismatch\n got:\n%s\nwant:\n%s",
			strings.Join(bound, "\n"), strings.Join(want, "\n"))
	}
}
