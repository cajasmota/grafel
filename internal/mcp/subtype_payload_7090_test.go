package mcp

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/golang"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
	_ "github.com/cajasmota/grafel/internal/extractors/javascript"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tsgo "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/golang"
	tsjava "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/java"
	tstypescript "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/typescript"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
	"github.com/cajasmota/grafel/internal/types"
)

// #7090 — `Entity.Subtype` decides graph identity (it is hashed into the digest)
// and engine passes key on it, but `serializeEntity` never emitted it, so what a
// consumer could learn about an entity's subtype depended on whether its
// extractor happened to ALSO dual-stamp Properties["subtype"] — and even then
// only under verbose, since `properties` is verbose-only.
//
// These tests grade the EMITTED PAYLOAD, never the struct field: the defect was
// precisely that a populated field never reached the output, so an assertion
// reading e.Subtype would grade nothing.
//
// The entities come from the REAL extractors, not hand-built records, because
// the claim under test is per-producer. The record→entity mapping below copies
// Kind/Subtype/Properties verbatim, exactly as production's
// entityRecordToGraphEntity (internal/extractors/incremental.go) does.

func parseFor7090(t *testing.T, lang ts.Language, src string) ts.Tree {
	t.Helper()
	parser, err := tsofficial.New().NewParser(lang)
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

// extract7090 runs the registered extractor for language and returns the
// records as graph entities, mapped the way production maps them.
func extract7090(t *testing.T, language, path, src string, lang ts.Language) []graph.Entity {
	t.Helper()
	ext, ok := extractor.Get(language)
	if !ok {
		t.Fatalf("%s extractor not registered", language)
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: language,
		TSTree: parseFor7090(t, lang, src),
	})
	if err != nil {
		t.Fatalf("%s extract: %v", language, err)
	}
	out := make([]graph.Entity, 0, len(recs))
	for _, r := range recs {
		out = append(out, entity7090(r))
	}
	return out
}

func entity7090(r types.EntityRecord) graph.Entity {
	return graph.Entity{
		ID: r.Name + "|" + r.Kind + "|" + r.Subtype, Name: r.Name,
		QualifiedName: r.QualifiedName, Kind: r.Kind, Subtype: r.Subtype,
		SourceFile: r.SourceFile, StartLine: r.StartLine, EndLine: r.EndLine,
		Language: r.Language,
	}.WithProperties(r.Properties)
}

func findEntity7090(t *testing.T, ents []graph.Entity, name string) *graph.Entity {
	t.Helper()
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	t.Fatalf("no entity named %q among %d extracted", name, len(ents))
	return nil
}

const javaSrc7090 = `package com.example;

public class OrderService {
    public void place() {}
}

public interface OrderRepository {
    void save();
}
`

const goSrc7090 = `package shop

import "fmt"

type OrderService struct {
	name string
}

type OrderRepository interface {
	Save() error
}

var _ = fmt.Sprint
`

const tsSrc7090 = `export enum Status { Open = "open" }

export interface Order {
  id: string;
}
`

// Java is the producer that encodes the class/interface distinction ONLY in
// Subtype: both land on Kind "SCOPE.Component" and Java does NOT dual-stamp
// Properties["subtype"]. So before #7090 the payload could not tell a Java class
// from a Java interface at all — the "list this project's annotations /
// interfaces" query the field exists to answer was unanswerable over MCP.
func TestSubtypeReachesPayload_Java_7090(t *testing.T) {
	ents := extract7090(t, "java", "OrderService.java", javaSrc7090, tsjava.Language())
	class := findEntity7090(t, ents, "OrderService")
	iface := findEntity7090(t, ents, "OrderRepository")

	// Premise 1 — the distinction really is Subtype-only for this producer.
	if class.Kind != iface.Kind {
		t.Fatalf("premise broken: java class Kind=%q interface Kind=%q — this test "+
			"grades the case where Kind CANNOT distinguish them", class.Kind, iface.Kind)
	}
	// Premise 2 — no dual-stamp, so the payload key is the only possible route.
	if p := class.PropGet("subtype"); p != "" {
		t.Fatalf("premise broken: java now dual-stamps Properties[subtype]=%q; the "+
			"'only route' claim in serializeEntity needs re-grounding", p)
	}

	for _, tc := range []struct {
		ent  *graph.Entity
		want string
	}{
		{class, "class"}, {iface, "interface"},
	} {
		got := serializeEntity("r", tc.ent, true)
		if got["subtype"] != tc.want {
			t.Errorf("default payload for %s: subtype=%v want %q (keys: %v)",
				tc.ent.Name, got["subtype"], tc.want, keysOf7090(got))
		}
		// Verbose must keep it too — verbose is documented as a superset.
		if v := serializeEntity("r", tc.ent, true, true); v["subtype"] != tc.want {
			t.Errorf("verbose payload for %s: subtype=%v want %q", tc.ent.Name, v["subtype"], tc.want)
		}
	}
	// The consequence, stated as the consumer sees it: two rows that are
	// otherwise identical in `kind` are now distinguishable.
	if serializeEntity("r", class, true)["subtype"] == serializeEntity("r", iface, true)["subtype"] {
		t.Error("java class and interface still emit the same subtype — #7090 unfixed")
	}
}

// Go, measured rather than assumed: its class-likes are ALSO Subtype-only
// (struct and interface both land on Kind "SCOPE.Component" with no
// Properties["subtype"]), while its FILE entity is one of the rows that DOES
// dual-stamp. Both shapes are graded here: the dual-stamped row's payload key
// must agree with the property, and the Subtype-only rows must still be
// distinguishable — before #7090 neither was visible at all without verbose.
func TestSubtypeReachesPayload_Go_7090(t *testing.T) {
	ents := extract7090(t, "go", "shop.go", goSrc7090, tsgo.Language())
	strct := findEntity7090(t, ents, "OrderService")
	iface := findEntity7090(t, ents, "OrderRepository")
	file := findEntity7090(t, ents, "shop.go")

	// The dual-stamped row: payload key and Properties["subtype"] must agree, so
	// a consumer never has to choose between two representations.
	if file.PropGet("subtype") == "" {
		t.Fatalf("premise broken: the go file entity no longer dual-stamps " +
			"Properties[subtype]; this leg grades the dual-stamped shape")
	}
	if got := serializeEntity("r", file, true); got["subtype"] != file.PropGet("subtype") {
		t.Errorf("file entity: payload subtype=%v disagrees with Properties[subtype]=%q",
			got["subtype"], file.PropGet("subtype"))
	}

	// The Subtype-only rows.
	for _, tc := range []struct {
		ent  *graph.Entity
		want string
	}{{strct, "struct"}, {iface, "interface"}} {
		if p := tc.ent.PropGet("subtype"); p != "" {
			t.Fatalf("premise broken: go %s now dual-stamps Properties[subtype]=%q",
				tc.ent.Name, p)
		}
		if tc.ent.Kind != strct.Kind {
			t.Fatalf("premise broken: go %s Kind=%q differs from the struct's %q — "+
				"this leg grades the case where Kind cannot distinguish them",
				tc.ent.Name, tc.ent.Kind, strct.Kind)
		}
		if got := serializeEntity("r", tc.ent, true); got["subtype"] != tc.want {
			t.Errorf("default payload for %s: subtype=%v want %q (keys: %v)",
				tc.ent.Name, got["subtype"], tc.want, keysOf7090(got))
		}
	}
}

// TypeScript is the producer that also encodes distinctions in Kind
// (SCOPE.Enum / SCOPE.Schema), so the subtype key is additive rather than the
// only route here — it must still be emitted, and must still agree.
func TestSubtypeReachesPayload_TypeScript_7090(t *testing.T) {
	ents := extract7090(t, "typescript", "order.ts", tsSrc7090, tstypescript.Language())
	var withSubtype int
	for i := range ents {
		e := &ents[i]
		got := serializeEntity("r", e, true)
		if e.Subtype == "" {
			if _, present := got["subtype"]; present {
				t.Errorf("%s (%s): no Subtype but payload carries subtype=%v",
					e.Name, e.Kind, got["subtype"])
			}
			continue
		}
		withSubtype++
		if got["subtype"] != e.Subtype {
			t.Errorf("%s (%s): payload subtype=%v want %q", e.Name, e.Kind, got["subtype"], e.Subtype)
		}
	}
	if withSubtype == 0 {
		t.Fatal("vacuous: no typescript entity carried a subtype")
	}
}

// The direction that gets skipped: an entity with NO subtype must not acquire an
// empty-string `subtype` key that consumers then have to filter out. Absence of
// the KEY is asserted, not an empty value — `got["subtype"] == ""` is true for a
// missing key too and would pass even if the guard were deleted.
func TestNoSubtypeMeansNoKey_7090(t *testing.T) {
	bare := graph.Entity{ID: "e1", Name: "Bare", Kind: "SCOPE.Component", SourceFile: "a.go", StartLine: 1}
	for _, verbose := range []bool{false, true} {
		got := serializeEntity("r", &bare, true, verbose)
		if v, present := got["subtype"]; present {
			t.Errorf("verbose=%v: subtype key present (=%#v) on an entity with no Subtype", verbose, v)
		}
	}

	// Same claim on rows a REAL producer emitted with an empty Subtype — so the
	// guard is graded against real data, not only a hand-built row. The go
	// import entity ("fmt") is such a row; every other entity the go extractor
	// emits for this source carries a subtype.
	ents := extract7090(t, "go", "shop.go", goSrc7090, tsgo.Language())
	var empties int
	for i := range ents {
		e := &ents[i]
		if e.Subtype != "" {
			continue
		}
		empties++
		for _, verbose := range []bool{false, true} {
			if v, present := serializeEntity("r", e, true, verbose)["subtype"]; present {
				t.Errorf("%s (%s) verbose=%v: empty Subtype but payload carries subtype=%#v",
					e.Name, e.Kind, verbose, v)
			}
		}
	}
	if empties == 0 {
		t.Fatal("vacuous: no producer-emitted subtype-less entity in this go source — " +
			"the real-data half of the negative direction graded nothing")
	}
}

func keysOf7090(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
