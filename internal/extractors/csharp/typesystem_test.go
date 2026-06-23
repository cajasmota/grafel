package csharp_test

// ---------------------------------------------------------------------------
// Type-system: enum_declaration + record_declaration
// ---------------------------------------------------------------------------

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

func TestCSharpExtractor_EnumDeclaration(t *testing.T) {
	src := `
public enum OrderStatus
{
    Pending,
    Processing,
    Shipped,
    Delivered,
    Cancelled
}
`
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("csharp")
	if !ok {
		t.Fatal("csharp extractor not registered")
	}

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "OrderStatus.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "enum" && e.Name == "OrderStatus" {
			found = true
			// Verify members appear in signature.
			if e.Signature == "" {
				t.Error("expected non-empty Signature for enum entity")
			}
		}
	}
	if !found {
		t.Error("expected SCOPE.Schema/enum entity for OrderStatus")
	}
}

func TestCSharpExtractor_EnumSimpleOneLine(t *testing.T) {
	src := `public enum Color { Red, Green, Blue }`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Color.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, e := range got {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "enum" && e.Name == "Color" {
			found = true
		}
	}
	if !found {
		t.Error("expected SCOPE.Schema/enum for one-line Color enum")
	}
}

func TestCSharpExtractor_RecordDeclarationPositional(t *testing.T) {
	src := `public record UserDto(string FirstName, string LastName, int Age);`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "UserDto.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "type" && e.Name == "UserDto" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Component/type for record UserDto; got %d entities", len(got))
	}
}

func TestCSharpExtractor_RecordDeclarationBlock(t *testing.T) {
	src := `
public record OrderRecord
{
    public string Id { get; init; }
    public decimal Total { get; init; }
}
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "OrderRecord.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, e := range got {
		if e.Kind == "SCOPE.Component" && e.Subtype == "type" && e.Name == "OrderRecord" {
			found = true
		}
	}
	if !found {
		t.Error("expected SCOPE.Component/type for block record OrderRecord")
	}
}

func TestCSharpExtractor_MultipleTypeSystemEntities(t *testing.T) {
	src := `
public enum PaymentMethod { CreditCard, Debit, PayPal }

public interface IPayable { void Pay(decimal amount); }

public class Invoice { }

public record InvoiceDto(string Number, decimal Amount);
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("csharp")

	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "types.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	counts := map[string]int{}
	for _, e := range got {
		key := e.Kind + "/" + e.Subtype
		counts[key]++
	}

	if counts["SCOPE.Schema/enum"] == 0 {
		t.Error("expected at least one SCOPE.Schema/enum entity")
	}
	if counts["SCOPE.Component/interface"] == 0 {
		t.Error("expected at least one SCOPE.Component/interface entity")
	}
	if counts["SCOPE.Component/class"] == 0 {
		t.Error("expected at least one SCOPE.Component/class entity")
	}
	if counts["SCOPE.Component/type"] == 0 {
		t.Error("expected at least one SCOPE.Component/type (record) entity")
	}
}
