// field_type_4868_test.go — #4868: PEP-526 annotated class fields must carry a
// `name: type` Signature so the dashboard shape resolver shows the real type
// (and nullability for `| None` / `Optional[...]`) instead of "unknown".
package python_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tspython "github.com/smacker/go-tree-sitter/python"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/python"
)

func TestExtract_AnnotatedFieldSignature_4868(t *testing.T) {
	src := []byte(`
class CreateAddressBody:
    line1: str = ""
    effective_at: datetime | None = None
    count = 0
    building: int
    group: int | None
`)
	p := sitter.NewParser()
	p.SetLanguage(tspython.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fi := extractor.FileInput{
		Path:     "core/dto/address.py",
		Content:  src,
		Language: "python",
		Tree:     tree,
	}
	ext, _ := extractor.Get("python")
	ents, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	sig := map[string]string{}
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			name := e.Name
			if i := lastDot(name); i >= 0 {
				name = name[i+1:]
			}
			sig[name] = e.Signature
		}
	}
	if got := sig["line1"]; got != "line1: str" {
		t.Errorf("line1 signature: want %q, got %q", "line1: str", got)
	}
	if got := sig["effective_at"]; got != "effective_at: datetime | None" {
		t.Errorf("effective_at signature: want %q, got %q", "effective_at: datetime | None", got)
	}
	// Unannotated field keeps the bare-name signature (no fabricated type).
	if got := sig["count"]; got != "count" {
		t.Errorf("count signature: want %q, got %q", "count", got)
	}
	// #4881 generalization — annotation-ONLY fields (PEP-526 declaration with no
	// default value, the common live DTO shape) must also carry the type so the
	// dashboard shape row is non-empty, matching the JS/TS extractor fix.
	if got := sig["building"]; got != "building: int" {
		t.Errorf("building signature: want %q, got %q", "building: int", got)
	}
	if got := sig["group"]; got != "group: int | None" {
		t.Errorf("group signature: want %q, got %q", "group: int | None", got)
	}
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}
