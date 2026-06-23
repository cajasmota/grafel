// qualified_name_field_1725_test.go — verifies #1725 fix for the Python
// SCOPE.Schema/field emission path. The parent class already has a populated
// qualified_name; the field must follow the same form: "<mod>.<class>.<field>".
package python_test

import (
	"context"
	"testing"

	tspython "github.com/smacker/go-tree-sitter/python"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/python"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
)

func TestExtract_SchemaFieldQualifiedName_1725(t *testing.T) {
	src := []byte(`
class DeficiencyCreateSerializer:
    model = Deficiency
    fields = ['title', 'body']
    permission_classes = [IsAuthenticated]
`)
	p, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tspython.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer p.Close()
	tree, err := p.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	fi := extractor.FileInput{
		Path:     "core/serializers/deficiency_serializer.py",
		Content:  src,
		Language: "python",
		TSTree:   tree,
	}
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var fields []types.EntityRecord
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			fields = append(fields, e)
		}
	}
	if len(fields) == 0 {
		t.Fatal("expected SCOPE.Schema/field entities, got none")
	}

	wantPrefix := "core.serializers.deficiency_serializer.DeficiencyCreateSerializer."
	for _, f := range fields {
		if f.QualifiedName == "" {
			t.Errorf("field %q has empty QualifiedName (#1725 regression)", f.Name)
			continue
		}
		if f.QualifiedName[:len(wantPrefix)] != wantPrefix {
			t.Errorf("field %q QN=%q, want prefix %q",
				f.Name, f.QualifiedName, wantPrefix)
		}
	}
}
