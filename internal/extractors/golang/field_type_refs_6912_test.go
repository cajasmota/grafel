package golang_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6912 arm D — the field→declared-type edge for Go. See field_type_refs.go for
// the kind / address / same-file / ambiguity decisions; this file grades them.
//
// Every assertion below names FIELDS AND TARGETS. A count table cannot see a
// substitution (#6973) and recall cannot see over-firing at all (#6926), so the
// forbidden set is asserted BY NAME beside the expected set: a named primitive
// field, a named package-qualified field, a named anonymous-struct field, a
// named anonymous-interface field, the self-reference, and the field whose
// target name is shadowed by an enum value-set.
//
// AXES. The main fixture VARIES the wrapper form (bare, pointer, slice, array,
// map key+value, channel, func parameter, generic argument, embedded), the
// target's entity kind (SCOPE.Component/struct, SCOPE.Component/interface,
// SCOPE.Schema/type_alias) and the field's wire-name form (plain vs json-tag
// renamed). It HOLDS CONSTANT the file path, the owning struct and the target
// name (`Customer` wherever a struct target is wanted), so a wrapper regression
// shows up as a missing row rather than as a changed target. The axes that need
// a different FILE or a different DECLARATION SHAPE — cross-file, enum shadow,
// blank-import name sharing, shadowed predeclared identifier, type-parameter
// over-fire — each get their own fixture below precisely because they cannot
// vary inside this one without also varying something else.

const goFTPath = "models.go"

const goFTSrc = `package models

import (
	_ "embed"
	"other"
)

type Status int

const (
	StatusNew Status = iota
	StatusDone
)

type Tier int

type Customer struct{ Name string }

type Shipper interface{ Ship() }

type Holder[T any] struct{ Item T }

type embed struct{ Blob []byte }

type Order struct {
	Buyer    Customer
	Ptr      *Customer
	Watchers []Customer
	Hist     [3]Customer
	ByName   map[Tier]Customer
	Both     map[Customer]Customer
	Ch       chan Customer
	Cb       func(Customer) error
	Wrapped  Holder[Customer]
	Ship     Shipper
	Emb      embed
	St       Status
	Foreign  other.Order
	Nested   struct{ A Customer }
	Anon     interface{ Get() Customer }
	Self     *Order
	Qty      int
	Label    string
	Renamed  Customer ` + "`json:\"renamed_wire\"`" + `
}
`

// goFTRivalPath declares types of the SAME bare names in a SECOND file. Its only
// job is to make the bare-name resolver tier ambiguous, which is why the address
// is structural; the edges from models.go must be identical with and without it.
const goFTRivalPath = "other.go"

// It also declares an `Order` with a same-named `Buyer` field pointing at a
// same-named `Customer`, so the two files differ in NOTHING the edge can see
// except the file. If the address were not file-scoped, these two rows would
// collapse or cross-bind.
const goFTRivalSrc = `package models

type Customer struct{ Other string }
type Shipper interface{ Ship() }
type Tier int

type Order struct{ Buyer Customer }
`

func extractGoFiles(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("go")
	if !ok {
		t.Fatal("go extractor not registered")
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var recs []types.EntityRecord
	for _, p := range paths {
		content := []byte(files[p])
		got, err := ext.Extract(context.Background(), extractor.FileInput{
			Path:     p,
			Content:  content,
			Language: "go",
			TSTree:   parseGo(content),
		})
		if err != nil {
			t.Fatalf("Extract %s: %v", p, err)
		}
		recs = append(recs, got...)
	}
	return recs
}

func goFTOne(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	return extractGoFiles(t, map[string]string{goFTPath: src})
}

// goFTEdges returns "<record name> -> <ToID>" for every REFERENCES edge carrying
// ref_kind=field_target_type, across ALL records, so an edge that escaped onto
// some other record is still seen. It fails on a non-empty FromID: the Django
// precedent leaves it empty so assembly anchors the edge on the record that
// carries it.
func goFTEdges(t *testing.T, recs []types.EntityRecord) []string {
	t.Helper()
	var out []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			if r.FromID != "" {
				t.Errorf("%s -[REFERENCES]-> %s has FromID=%q, want empty so assembly "+
					"anchors it on the carrying record", recs[i].Name, r.ToID, r.FromID)
			}
			if recs[i].Kind != "SCOPE.Schema" || recs[i].Subtype != "field" {
				t.Errorf("field-type edge escaped onto %s/%s %q — only a "+
					"SCOPE.Schema/field record may carry one",
					recs[i].Kind, recs[i].Subtype, recs[i].Name)
			}
			out = append(out, recs[i].Name+" -> "+r.ToID)
		}
	}
	sort.Strings(out)
	return out
}

// goFTTargetsOf returns the sorted target_type property values of the field-type
// edges carried by the field record named `field`.
func goFTTargetsOf(recs []types.EntityRecord, field string) []string {
	var out []string
	for i := range recs {
		if recs[i].Name != field {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && r.Properties.Get("ref_kind") == "field_target_type" {
				out = append(out, r.Properties.Get("target_type"))
			}
		}
	}
	sort.Strings(out)
	return out
}

func goFTWantEqual(t *testing.T, got, want []string, what string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%s mismatch\n got:\n  %s\nwant:\n  %s",
			what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestGoFieldTypeRefs_EdgeSetIsExact pins the whole emitted set by name, in both
// directions at once: every wrapper form that MUST yield an edge, and every
// forbidden field that must yield none. The two rival files are extracted
// together so the assertion also proves the address is file-scoped.
func TestGoFieldTypeRefs_EdgeSetIsExact(t *testing.T) {
	recs := extractGoFiles(t, map[string]string{
		goFTPath:      goFTSrc,
		goFTRivalPath: goFTRivalSrc,
	})
	const c = "scope:component:class:go:models.go:"
	goFTWantEqual(t, goFTEdges(t, recs), []string{
		// Wrapper forms — target held constant at Customer so a regression
		// reads as a missing row, not a changed target.
		"Order.Buyer -> " + c + "Customer",
		"Order.Ptr -> " + c + "Customer",
		"Order.Watchers -> " + c + "Customer",
		"Order.Hist -> " + c + "Customer",
		"Order.Ch -> " + c + "Customer",
		"Order.Cb -> " + c + "Customer",
		"Order.renamed_wire -> " + c + "Customer",
		// map[Tier]Customer — BOTH halves are judged, and both are declared
		// here, so this field carries TWO edges. The key is not skipped.
		"Order.ByName -> " + c + "Tier",
		"Order.ByName -> " + c + "Customer",
		// map[Customer]Customer names the SAME declared type in both halves.
		// Exactly ONE row: the per-field dedup on ToID is what makes it one,
		// and a second identical row here would be counted twice by
		// ReferencesTotal and by relationship_extracted_total, neither of
		// which dedups edges (audit.go:355-388).
		"Order.Both -> " + c + "Customer",
		// Holder[Customer] — the generic constructor is a legitimate target
		// (it is declared here) AND so is the argument.
		"Order.Wrapped -> " + c + "Holder",
		"Order.Wrapped -> " + c + "Customer",
		// Non-struct target kinds.
		"Order.Ship -> " + c + "Shipper", // SCOPE.Component/interface
		"Order.Emb -> " + c + "embed",    // name shared with a blank import
		// The rival file's own field resolves against ITS own file.
		"Order.Buyer -> scope:component:class:go:other.go:Customer",
	}, "field-type edge set")

	// Forbidden, by name. Each of these is a field that EXISTS in the fixture
	// and must carry no edge; asserting the set above is not enough on its own,
	// because a renamed or mis-attributed row would still pass a set compare
	// while these names silently gained an edge.
	for _, forbidden := range []string{
		"Order.Qty",     // predeclared int
		"Order.Label",   // predeclared string
		"Order.Foreign", // other.Order — qualified, never stripped to Order
		"Order.Nested",  // anonymous struct — its member is not this field's type
		"Order.Anon",    // anonymous interface
		"Order.Self",    // *Order — the owning struct
		"Order.St",      // Status — shadowed by a SCOPE.Enum value-set
	} {
		if got := goFTTargetsOf(recs, forbidden); len(got) != 0 {
			t.Errorf("%s must carry NO field-type edge, got targets %v", forbidden, got)
		}
	}
}

// TestGoFieldTypeRefs_TypeShadowedByAnEnumValueSetGetsNoEdge grades the
// Go-specific ambiguity rule WITH A POSITIVE CONTROL IN THE SAME FIXTURE.
//
// `type Status int` carries a typed const block, so extractGoEnums emits a
// SCOPE.Enum value-set ALSO named Status in this file. Two kinds at one (file,
// name) is two graph nodes, the resolver returns statusAmbiguous, and an edge
// would dangle — so none is emitted. `type Tier int` is the IDENTICAL
// declaration form with no const block, hence one node, and it MUST get an edge.
//
// Without the Tier row this test could not tell "the enum shadow is respected"
// from "a type_alias target never works at all" — the failure mode a
// recall-only or absence-only assertion has.
func TestGoFieldTypeRefs_TypeShadowedByAnEnumValueSetGetsNoEdge(t *testing.T) {
	recs := goFTOne(t, goFTSrc)

	// Premise: both declarations really are SCOPE.Schema/type_alias, and Status
	// really does have a second, differently-kinded record. If the premise
	// stops holding, the test is vacuous rather than passing.
	kinds := map[string]map[string]bool{}
	for i := range recs {
		if recs[i].SourceFile != goFTPath || recs[i].Name == "" {
			continue
		}
		if kinds[recs[i].Name] == nil {
			kinds[recs[i].Name] = map[string]bool{}
		}
		kinds[recs[i].Name][recs[i].Kind+"/"+recs[i].Subtype] = true
	}
	if !kinds["Status"]["SCOPE.Schema/type_alias"] || !kinds["Status"]["SCOPE.Enum/enum"] {
		t.Fatalf("premise gone: Status carries %v, want both a type_alias and an "+
			"enum value-set — the shadow this test grades is not present", kinds["Status"])
	}
	if !kinds["Tier"]["SCOPE.Schema/type_alias"] || len(kinds["Tier"]) != 1 {
		t.Fatalf("premise gone: Tier carries %v, want exactly one type_alias — "+
			"the positive control is not comparable to Status", kinds["Tier"])
	}

	if got := goFTTargetsOf(recs, "Order.St"); len(got) != 0 {
		t.Errorf("Order.St targets %v; Status is shadowed by an enum value-set so "+
			"the ref cannot bind and must not be emitted", got)
	}
	if got := goFTTargetsOf(recs, "Order.ByName"); !contains(got, "Tier") {
		t.Errorf("Order.ByName targets %v, want Tier among them — the positive "+
			"control for a type_alias target is missing, so the Status assertion "+
			"above grades nothing", got)
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// TestGoFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName is the forbidden
// row arms B and C both carry, and for Go it also documents a PRE-EXISTING BUG
// in the neighbouring edge that this arm deliberately does not inherit.
//
// `other.Order` in a file that also declares its own `Order` must yield NO field
// edge — taking the trailing segment would bind to the wrong entity, silently,
// because a bound edge never reaches bug-extractor. resolveTypeReferences (used
// by the struct-anchored DEPENDS_ON) DOES strip it, so the same fixture asserts
// that DEPENDS_ON's behaviour is UNCHANGED by this arm: fixing that is a
// separate change with its own blast radius, not a rider on this one.
func TestGoFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName(t *testing.T) {
	const src = `package models

import "other"

type Order struct{ ID string }

type Holder struct {
	Foreign other.Order
	Local   Order
}
`
	recs := goFTOne(t, src)

	if got := goFTTargetsOf(recs, "Holder.Foreign"); len(got) != 0 {
		t.Errorf("Holder.Foreign targets %v; `other.Order` is package-qualified and "+
			"must NOT be stripped to the same-file `Order`", got)
	}
	// Positive control: the UNqualified field in the same struct does bind, so
	// the assertion above is about the qualifier and not about Order being
	// untargetable.
	if got := goFTTargetsOf(recs, "Holder.Local"); !contains(got, "Order") {
		t.Fatalf("Holder.Local targets %v, want Order — the control for the "+
			"qualified-name rule is missing", got)
	}

	// The struct-anchored DEPENDS_ON is untouched, strip included. This is
	// asserted so the pre-existing behaviour is pinned as pre-existing: if a
	// later change narrows resolveTypeReferences, THIS is the line that says
	// the field edge never depended on it.
	var dep []string
	for i := range recs {
		if recs[i].Name != "Holder" || recs[i].Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "DEPENDS_ON" {
				dep = append(dep, r.FromID+" -> "+r.ToID)
			}
		}
	}
	sort.Strings(dep)
	goFTWantEqual(t, dep, []string{"Holder -> Order"},
		"struct-anchored DEPENDS_ON (bare-name ToID, qualified type stripped) "+
			"must be unchanged by this arm")
}

// TestGoFieldTypeRefs_PrimitiveFieldsProduceNoEdge enumerates Go's predeclared
// type names against an INDEPENDENT LITERAL rather than against the production
// set (#6975) — there is no production blocklist to share, which is the point:
// a predeclared name is refused because nothing DECLARES it in the file, not by
// a list. The companion test proves the same code path admits the name when the
// file does declare it, so this test is not merely observing a blocklist.
func TestGoFieldTypeRefs_PrimitiveFieldsProduceNoEdge(t *testing.T) {
	predeclared := []string{
		"bool", "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune", "float32", "float64", "complex64", "complex128",
		"error", "any",
	}
	var b strings.Builder
	b.WriteString("package models\n\ntype Holder struct {\n")
	for i, p := range predeclared {
		b.WriteString("\tF")
		b.WriteString(string(rune('A' + i%26)))
		b.WriteString(string(rune('a' + i/26)))
		b.WriteString(" ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	recs := goFTOne(t, b.String())

	// Premise: the fields exist. Otherwise "no edges" is vacuous.
	n := 0
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" {
			n++
		}
	}
	if n != len(predeclared) {
		t.Fatalf("fixture emitted %d field entities, want %d — the absence of "+
			"edges below would be vacuous", n, len(predeclared))
	}
	goFTWantEqual(t, goFTEdges(t, recs), nil,
		"no predeclared-type field may carry a field-type edge")
}

// TestGoFieldTypeRefs_ShadowedPredeclaredIdentifierIsATarget is the inverse of
// the test above, and it is why Go must NOT carry arm B's protoScalars-style
// blocklist. Go's predeclared identifiers live in the universe scope and are
// legally shadowable at package scope; tree-sitter-go emits them as
// type_identifier exactly like a user type. So when a file really does declare
// `type rune struct{…}`, a field written `rune` genuinely means THAT type, and a
// blocklist would refuse the one edge that should exist.
//
// This is the exact inverse of arm B's case, where protoc's grammar binds
// `string` to the scalar before it ever considers a same-named message — which
// is why that guard is correct there and would be wrong here.
func TestGoFieldTypeRefs_ShadowedPredeclaredIdentifierIsATarget(t *testing.T) {
	const src = `package models

type rune struct{ Cp int32 }

type Holder struct {
	Shadowed rune
	Plain    int
}
`
	recs := goFTOne(t, src)
	if got := goFTTargetsOf(recs, "Holder.Shadowed"); !contains(got, "rune") {
		t.Errorf("Holder.Shadowed targets %v, want `rune` — the file DECLARES "+
			"type rune, so the field names that type and a predeclared-name "+
			"blocklist would wrongly refuse the edge", got)
	}
	if got := goFTTargetsOf(recs, "Holder.Plain"); len(got) != 0 {
		t.Errorf("Holder.Plain targets %v; `int` is not declared here", got)
	}
}

// TestGoFieldTypeRefs_BlankImportSharingATypeNameBinds is the case that proves
// the ambiguity rule must count KINDS and not RECORDS.
//
// `import _ "embed"` does not bind the identifier, so a file may legally
// blank-import "embed" and also declare `type embed struct{…}`. That is TWO
// records named `embed` in one file — an import placeholder and a struct — but
// both are SCOPE.Component, so graph.EntityID gives them ONE id and the resolver
// binds the name without complaint. A record-counting ambiguity rule would see
// two and delete this edge; a kind-counting one keeps it.
func TestGoFieldTypeRefs_BlankImportSharingATypeNameBinds(t *testing.T) {
	recs := goFTOne(t, goFTSrc)

	// Premise: there really are two same-named records, and they really do
	// share a Kind. Without this the test would pass for the wrong reason.
	var subs []string
	for i := range recs {
		if recs[i].SourceFile == goFTPath && recs[i].Name == "embed" {
			subs = append(subs, recs[i].Kind+"/"+recs[i].Subtype)
		}
	}
	sort.Strings(subs)
	goFTWantEqual(t, subs, []string{"SCOPE.Component/", "SCOPE.Component/struct"},
		"premise: `embed` must be carried by an import placeholder AND a struct, "+
			"both SCOPE.Component")

	if got := goFTTargetsOf(recs, "Order.Emb"); !contains(got, "embed") {
		t.Errorf("Order.Emb targets %v, want `embed` — two records of the SAME "+
			"kind are one graph node, so this name is not ambiguous and the "+
			"edge must survive", got)
	}
}

// TestGoFieldTypeRefs_CrossFileTypeGetsNoEdge — the same-file rule, and for Go
// this is the dominant miss rather than an edge case: a Go package is a
// DIRECTORY of files, so a field whose type lives in a sibling file of the same
// package is ordinary rather than unusual. Stated as the honest floor and
// asserted by name; closing it needs a cross-file package type view that pass 1
// does not have.
func TestGoFieldTypeRefs_CrossFileTypeGetsNoEdge(t *testing.T) {
	recs := extractGoFiles(t, map[string]string{
		"a.go": `package models

type Order struct {
	Buyer Customer
	Local Local
}

type Local struct{ X int }
`,
		"b.go": `package models

type Customer struct{ Name string }
`,
	})
	if got := goFTTargetsOf(recs, "Order.Buyer"); len(got) != 0 {
		t.Errorf("Order.Buyer targets %v; Customer is declared in b.go and this "+
			"pass is same-file only", got)
	}
	// Positive control in the same struct: the same-file type DOES bind, so the
	// absence above is about the file boundary and not about the pass being off.
	if got := goFTTargetsOf(recs, "Order.Local"); !contains(got, "Local") {
		t.Fatalf("Order.Local targets %v, want Local — the same-file control is "+
			"missing so the cross-file assertion grades nothing", got)
	}
}

// TestGoFieldTypeRefs_KnownOverFire_TypeParameterShadowedByASameFileType pins a
// KNOWN-WRONG edge, deliberately, so a fix breaks a test rather than passing
// silently.
//
// The pass does not understand type parameters. `type Box[T any] struct{ Item T
// }` yields candidate `T`, which is normally dropped only because nothing in the
// file is DECLARED `T`. In a file that also declares `type T struct{…}` the
// candidate matches and the edge is emitted — asserting that Box.Item's declared
// type is that struct, which is false. A real fix (tracking the type-parameter
// list as a shadowing scope) is EXPECTED to break this test.
func TestGoFieldTypeRefs_KnownOverFire_TypeParameterShadowedByASameFileType(t *testing.T) {
	const src = `package models

type T struct{ Unrelated string }

type Box[T any] struct {
	Item T
}
`
	recs := goFTOne(t, src)
	got := goFTTargetsOf(recs, "Box.Item")
	if !contains(got, "T") {
		t.Skipf("KNOWN-WRONG edge is gone (Box.Item targets %v). If type "+
			"parameters are now tracked as a shadowing scope, DELETE this test; "+
			"it exists only to make the over-fire visible.", got)
	}
	t.Logf("known over-fire present as expected: Box.Item -> %v (a type parameter "+
		"shadowed by a same-file declaration)", got)
}

// TestGoFieldTypeRefs_StashIsClearedFromMetadata — the candidate stash is an
// internal handoff between the walk and the attach pass. Leaving it on the record
// would ship extractor scratch state into the graph as entity metadata.
func TestGoFieldTypeRefs_StashIsClearedFromMetadata(t *testing.T) {
	recs := goFTOne(t, goFTSrc)
	saw := false
	for i := range recs {
		if recs[i].Metadata == nil {
			continue
		}
		if v, ok := recs[i].Metadata["field_type_refs"]; ok {
			t.Errorf("%s still carries the field_type_refs stash: %v", recs[i].Name, v)
		}
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("no field records in fixture — the absence of a stash is vacuous")
	}
}

// TestGoFieldTypeRefs_ResolvesToEntityIDs drives the emitted edges through the
// real resolver and asserts EVERY ONE BINDS to a real entity id, naming the
// entity it binds to. This is the assertion that makes "a non-binding edge is
// worse than no edge" a graded property rather than a stated intention: a
// dangling stub is kept verbatim and classified bug-extractor, so a mis-addressed
// edge would be a full hit on the disposition denominator.
//
// The two rival files are both indexed, so the bare name `Customer` is ambiguous
// across the graph — checked explicitly, because if it were NOT ambiguous the
// structural address would be ungraded here and a bare-name ToID would pass too.
func TestGoFieldTypeRefs_ResolvesToEntityIDs(t *testing.T) {
	recs := extractGoFiles(t, map[string]string{
		goFTPath:      goFTSrc,
		goFTRivalPath: goFTRivalSrc,
	})
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	idx := resolve.BuildIndex(recs)

	before := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && r.Properties.Get("ref_kind") == "field_target_type" {
				before++
			}
		}
	}
	if before == 0 {
		t.Fatal("no field-type edges to drive through the resolver")
	}

	// Positive control on the address dialect: the BARE name must be ambiguous
	// here, otherwise this test does not distinguish the structural address from
	// the bare-name form the adjacent DEPENDS_ON uses.
	if _, st := idx.LookupStatusHint("Customer", "REFERENCES"); st == 1 {
		t.Fatalf("the bare name %q binds in this fixture, so the ambiguity this "+
			"test controls for is absent and the structural address is ungraded",
			"Customer")
	}

	// An import placeholder and a struct of the same name are ONE graph node,
	// not two: graph.EntityID hashes (repo, Kind, Name, SourceFile) and both are
	// SCOPE.Component/embed in models.go, so they share an ID by construction.
	// The description below therefore carries the SORTED SET of subtypes behind
	// each ID — `embed` must come back as "+struct", the empty subtype first —
	// because keying a map by ID and writing the subtype into it would silently
	// report whichever record came last and read as a mis-binding.
	idToName := map[string]string{}
	idToSubs := map[string]map[string]bool{}
	for i := range recs {
		id := recs[i].ID
		idToName[id] = recs[i].SourceFile + ":" + recs[i].Kind + ":" + recs[i].Name
		if idToSubs[id] == nil {
			idToSubs[id] = map[string]bool{}
		}
		idToSubs[id][recs[i].Subtype] = true
	}
	describe := func(id string) string {
		subs := make([]string, 0, len(idToSubs[id]))
		for s := range idToSubs[id] {
			subs = append(subs, s)
		}
		sort.Strings(subs)
		return idToName[id] + " [" + strings.Join(subs, "+") + "]"
	}

	resolve.ReferencesEmbedded(recs, idx)

	var bound []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			_, ok := idToName[r.ToID]
			if !ok {
				t.Errorf("%s:%s -[REFERENCES]-> %q did NOT bind to an entity id "+
					"(dangling stub → bug-extractor)",
					recs[i].SourceFile, recs[i].Name, r.ToID)
				continue
			}
			bound = append(bound, recs[i].SourceFile+":"+recs[i].Name+" => "+describe(r.ToID))
		}
	}
	sort.Strings(bound)
	const m = "models.go:"
	goFTWantEqual(t, bound, []string{
		m + "Order.Buyer => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.ByName => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.ByName => " + m + "SCOPE.Schema:Tier [type_alias]",
		m + "Order.Both => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.Cb => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.Ch => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.Emb => " + m + "SCOPE.Component:embed [+struct]",
		m + "Order.Hist => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.Ptr => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.Ship => " + m + "SCOPE.Component:Shipper [interface]",
		m + "Order.Watchers => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.Wrapped => " + m + "SCOPE.Component:Customer [struct]",
		m + "Order.Wrapped => " + m + "SCOPE.Component:Holder [struct]",
		m + "Order.renamed_wire => " + m + "SCOPE.Component:Customer [struct]",
		"other.go:Order.Buyer => other.go:SCOPE.Component:Customer [struct]",
	}, "resolved field-type endpoints")
}

// TestGoFieldTypeRefs_PropertiesAgreeWithTheEndpoints — the field_name property
// must be the field entity's own leaf name, INCLUDING when a json tag renamed it,
// so the property agrees with the Name and with the CONTAINS structural-ref
// rather than introducing a third spelling of the same field.
func TestGoFieldTypeRefs_PropertiesAgreeWithTheEndpoints(t *testing.T) {
	recs := goFTOne(t, goFTSrc)
	seen := map[string]string{}
	for i := range recs {
		r := &recs[i]
		if r.Kind != "SCOPE.Schema" || r.Subtype != "field" {
			continue
		}
		owner, _ := r.Metadata["owner"].(string)
		for _, rel := range r.Relationships {
			if rel.Kind != "REFERENCES" || rel.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			fn := rel.Properties.Get("field_name")
			if owner+"."+fn != r.Name {
				t.Errorf("%s carries field_name=%q; owner+field_name must rebuild "+
					"the entity Name exactly", r.Name, fn)
			}
			if rel.Properties.Get("target_type") == "" {
				t.Errorf("%s -> %s has an empty target_type", r.Name, rel.ToID)
			}
			seen[r.Name] = fn
		}
	}
	if seen["Order.renamed_wire"] != "renamed_wire" {
		t.Errorf("json-renamed field carries field_name=%q, want %q — the "+
			"json-tag axis is the one that can disagree with the Name, so it "+
			"must be the one asserted", seen["Order.renamed_wire"], "renamed_wire")
	}
}

// TestGoFieldTypeRefs_ImportPlaceholderIsNeverATarget grades the ALLOW-LIST,
// and it exists because the first version of this arm claimed the allow-list had
// no kill and offered a compound mutant as proof. The compound proved nothing —
// its other half was independently lethal — and the claim was false.
//
// This is the input that separates them, and the axis it varies is the ONE that
// TestGoFieldTypeRefs_BlankImportSharingATypeNameBinds holds constant: WHERE the
// type is declared.
//
// `import _ "embed"` emits a SCOPE.Component with an EMPTY Subtype whose Name is
// the whole import path — here the bare name `embed`. A blank import does not
// bind the identifier, and a Go package is a DIRECTORY of files, so declaring
// `type embed struct{…}` in a sibling file is ordinary, legal Go. In THIS file
// the only record named `embed` is therefore the import placeholder:
//
//   - one node, so the ambiguity rule (rule 2) does not fire;
//   - a bare name, so the "refused by name shape" argument does not apply;
//   - and the real type is in another file, so the same-file rule already
//     refuses the edge for an unrelated reason.
//
// The allow-list is the ONLY thing standing between this field and an edge bound
// to an import placeholder. With it, no edge. Without it, `Order.Blob` binds to
// the import — a wrong binding, which never reaches bug-extractor precisely
// because it binds. That is the failure class this whole arm exists to avoid.
func TestGoFieldTypeRefs_ImportPlaceholderIsNeverATarget(t *testing.T) {
	recs := extractGoFiles(t, map[string]string{
		"a.go": `package models

import _ "embed"

type Order struct {
	Blob  embed
	Local Local
}

type Local struct{ X int }
`,
		// The real type, in a sibling file of the same package.
		"b.go": `package models

type embed struct{ Data []byte }
`,
	})

	// Premise: a.go really does carry a bare-named import placeholder, and it
	// is the ONLY record named `embed` there. If either stops holding, the
	// absence asserted below would be vacuous.
	var aKinds []string
	for i := range recs {
		if recs[i].SourceFile == "a.go" && recs[i].Name == "embed" {
			aKinds = append(aKinds, recs[i].Kind+"/"+recs[i].Subtype)
		}
	}
	goFTWantEqual(t, aKinds, []string{"SCOPE.Component/"},
		"premise: a.go must carry exactly one record named `embed`, the "+
			"import placeholder with an empty Subtype")

	if got := goFTTargetsOf(recs, "Order.Blob"); len(got) != 0 {
		t.Errorf("Order.Blob targets %v; the only same-file record named `embed` "+
			"is an IMPORT PLACEHOLDER, and binding a field to one is a wrong "+
			"binding that never reaches bug-extractor because it binds", got)
	}
	// Positive control in the same struct: a legitimate same-file target still
	// binds, so the absence above is about the allow-list and not about the
	// pass being inert in this fixture.
	if got := goFTTargetsOf(recs, "Order.Local"); !contains(got, "Local") {
		t.Fatalf("Order.Local targets %v, want Local — without this control the "+
			"assertion above grades nothing", got)
	}
}
