package java_test

import (
	"testing"
)

// Refs #1935 Phase 1 — Java records emit a SCOPE.Component with
// subtype="record" plus one SCOPE.Schema field per record component
// (header parameter). This unlocks ShapeTree expansion of DTOs that
// modern Java codebases write as records.
func TestJava_RecordEmitsClassAndComponentFields(t *testing.T) {
	src := `
package com.example;

public record TransferRequest(String transferId, java.math.BigDecimal qty) {}
`
	ents := runJava(t, src)

	// Class entity present with subtype "record".
	rec := javaFind(ents, "TransferRequest", "SCOPE.Component")
	if rec == nil {
		t.Fatal("expected SCOPE.Component TransferRequest")
	}
	if rec.Subtype != "record" {
		t.Errorf("subtype: want record, got %q", rec.Subtype)
	}

	// Each record component must emit a SCOPE.Schema field whose name
	// is "<Record>.<component>" and signature carries the type token.
	idField := javaFind(ents, "TransferRequest.transferId", "SCOPE.Schema")
	if idField == nil {
		t.Fatal("expected SCOPE.Schema TransferRequest.transferId")
	}
	if idField.Subtype != "field" {
		t.Errorf("transferId.subtype: want field, got %q", idField.Subtype)
	}
	qtyField := javaFind(ents, "TransferRequest.qty", "SCOPE.Schema")
	if qtyField == nil {
		t.Fatal("expected SCOPE.Schema TransferRequest.qty")
	}

	// The class entity must own a CONTAINS edge to each component.
	contains := 0
	for _, r := range rec.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains < 2 {
		t.Errorf("expected >=2 CONTAINS edges from record, got %d", contains)
	}
}
