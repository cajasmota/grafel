package feedback

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/vbnet"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// #6536. The field-extraction metric had never sampled a real class in any
// C#-family codebase: classLikeKindTails admitted class/struct/model/schema,
// while every VB.NET and C# class/structure/module/interface is emitted as
// SCOPE.Component. The 253-entity "100% zero fields" figure in #6535 was the
// SCOPE.Schema residue (enums and consts) after field leaves were filtered —
// a population that structurally cannot own fields, so 100% was guaranteed
// rather than measured.
//
// These tests deliberately take their kind strings FROM THE EXTRACTOR rather
// than from a hand-written literal. The entire defect was that the tail list
// and the extractor disagreed and nothing ever compared them; a fixture that
// hard-codes "SCOPE.Component" would agree with whatever the list says and
// reproduce the same blind spot.

const vbSource6536 = `
Namespace Demo
    Public Class Widget
        Public Name As String
        Private count As Integer

        Public Const Limit As Integer = 10

        Public Sub Run()
        End Sub
    End Class

    Public Structure Box
        Public Width As Integer
    End Structure

    Public Enum Colour
        Red
        Green
    End Enum

End Namespace
`

// extract6536 runs the real VB.NET extractor over src and returns the records.
func extract6536(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ex := &vbnet.Extractor{}
	recs, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.vb",
		Content:  []byte(src),
		Language: "vbnet",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(recs) == 0 {
		t.Fatalf("Extract returned no records")
	}
	return recs
}

// recordsToDoc6536 projects extractor records onto a graph.Document, binding
// the structural-ref ToIDs the way the resolver's byLocation fallback does:
// the trailing ":"-separated segment of a Format A ref is exactly the target
// entity's Name (internal/extractor/structural_ref.go).
func recordsToDoc6536(t *testing.T, recs []types.EntityRecord) *graph.Document {
	t.Helper()
	ents := make([]graph.Entity, 0, len(recs))
	byName := make(map[string]string, len(recs))
	for i := range recs {
		id := "e" + string(rune('A'+i))
		r := &recs[i]
		byName[r.Name] = id
		ents = append(ents, graph.Entity{
			ID:         id,
			Name:       r.Name,
			Kind:       r.Kind,
			Subtype:    r.Subtype,
			Language:   r.Language,
			SourceFile: r.SourceFile,
			StartLine:  r.StartLine,
		}.WithProperties(map[string]string{}))
	}

	var rels []graph.Relationship
	for i := range recs {
		from := ents[i].ID
		for j, rr := range recs[i].Relationships {
			name := rr.ToID
			if k := strings.LastIndex(name, ":"); k >= 0 {
				name = name[k+1:]
			}
			to, ok := byName[name]
			if !ok {
				continue // unresolved external — irrelevant to this metric
			}
			rels = append(rels, graph.Relationship{
				ID:     from + "-" + to + "-" + string(rune('0'+j)),
				FromID: from,
				ToID:   to,
				Kind:   rr.Kind,
			})
		}
	}
	return makeDoc(ents, rels)
}

// kindOf6536 returns the kind/subtype the extractor actually emitted for the
// named declaration.
func kindOf6536(t *testing.T, recs []types.EntityRecord, name string) (kind, subtype string) {
	t.Helper()
	for i := range recs {
		if recs[i].Name == name {
			return recs[i].Kind, recs[i].Subtype
		}
	}
	t.Fatalf("no entity named %q in extractor output", name)
	return "", ""
}

// TestClassLikeKindTailsAdmitsExtractorTypeKinds_6536 compares the tail list
// against the kinds the VB.NET extractor really emits for type declarations.
// This is the assertion whose absence let #6536 ship.
func TestClassLikeKindTailsAdmitsExtractorTypeKinds_6536(t *testing.T) {
	recs := extract6536(t, vbSource6536)
	for _, name := range []string{"Widget", "Box"} {
		kind, _ := kindOf6536(t, recs, name)
		if !isClassLikeKind(kind) {
			t.Errorf("type %s is emitted as kind %q, which isClassLikeKind rejects: "+
				"the field-extraction metric cannot sample it", name, kind)
		}
	}
}

// TestFieldExtractionSamplesRealClasses_6536 is the killing test for the
// missing "component" tail: a class with field children must report a
// NON-100% zero-field rate.
func TestFieldExtractionSamplesRealClasses_6536(t *testing.T) {
	recs := extract6536(t, vbSource6536)
	doc := recordsToDoc6536(t, recs)

	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.FieldExtractionRate.ClassTotal == 0 {
		t.Fatalf("no class-like entities sampled; the metric never saw the real classes")
	}
	if r.FieldExtractionRate.ZeroFieldsPct == 100.0 {
		t.Fatalf("zero-field rate is 100%% on a graph whose Widget owns 2 fields "+
			"and Box owns 1: ClassTotal=%d", r.FieldExtractionRate.ClassTotal)
	}
	// Widget (2 fields) and Box (1 field) are the only candidates once enums
	// and consts are exempt, so nothing is zero-field.
	if r.FieldExtractionRate.ZeroFieldsPct != 0.0 {
		t.Errorf("ZeroFieldsPct = %v, want 0; ClassTotal=%d",
			r.FieldExtractionRate.ZeroFieldsPct, r.FieldExtractionRate.ClassTotal)
	}
	if r.FieldExtractionRate.ClassTotal != 2 {
		t.Errorf("ClassTotal = %d, want 2 (Widget, Box)", r.FieldExtractionRate.ClassTotal)
	}
}

// TestEnumAndConstExemptFromFieldDenominator_6536 pins the denominator
// decision: enum and const declarations cannot own field children, so leaving
// them in the denominator is a permanent floor of false failures.
func TestEnumAndConstExemptFromFieldDenominator_6536(t *testing.T) {
	recs := extract6536(t, vbSource6536)

	// Guard the premise against the extractor: Colour/Limit must really be
	// class-like-kinded (SCOPE.Schema) and subtyped enum/const, otherwise this
	// test is exempting something that was never in the denominator.
	for _, name := range []string{"Colour", "Widget.Limit"} {
		kind, subtype := kindOf6536(t, recs, name)
		if !isClassLikeKind(kind) {
			t.Fatalf("premise gone: %s has kind %q which is not class-like", name, kind)
		}
		if subtype != "enum" && subtype != "const" {
			t.Fatalf("premise gone: %s has subtype %q", name, subtype)
		}
	}

	// A graph of ONLY enums and consts must sample nothing at all, rather than
	// reporting a guaranteed 100% failure.
	var only []types.EntityRecord
	for i := range recs {
		if recs[i].Name == "Colour" || recs[i].Name == "Widget.Limit" {
			only = append(only, recs[i])
		}
	}
	doc := recordsToDoc6536(t, only)
	r, err := Generate(context.Background(), []*graph.Document{doc}, Opts{GroupName: "g", Version: "t"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.FieldExtractionRate.ClassTotal != 0 {
		t.Errorf("ClassTotal = %d on an enum/const-only graph, want 0 "+
			"(zero-field rate %v)", r.FieldExtractionRate.ClassTotal,
			r.FieldExtractionRate.ZeroFieldsPct)
	}
}
