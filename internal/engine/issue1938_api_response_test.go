// Tests for #1938 Phase 1 — @APIResponse per-status-code extraction.
//
// Verifies that the java_annotation_routes pass correctly extracts
// @APIResponse / @ApiResponse annotations and emits the api_responses
// property on the http_endpoint_definition entities.
package engine

import (
	"encoding/json"
	"testing"
)

func TestExtractAPIResponseAnnotations_MicroProfile(t *testing.T) {
	// MicroProfile OpenAPI @APIResponse with responseCode + implementation
	joined := `
@GET
@Path("/items/{sku}")
@APIResponse(responseCode = "200", description = "Item found", content = @Content(schema = @Schema(implementation = CatalogItem.class)))
@APIResponse(responseCode = "404", description = "Not found", content = @Content(schema = @Schema(implementation = ErrorResponse.class)))
@APIResponse(responseCode = "500", description = "Server error")
`
	entries := extractAPIResponseAnnotations(joined)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
	wantCodes := []int{200, 404, 500}
	for i, want := range wantCodes {
		if entries[i].StatusCode != want {
			t.Errorf("[%d] status_code: want %d, got %d", i, want, entries[i].StatusCode)
		}
	}
	if entries[0].TypeName != "CatalogItem" {
		t.Errorf("[0] type_name: want CatalogItem, got %q", entries[0].TypeName)
	}
	if entries[1].TypeName != "ErrorResponse" {
		t.Errorf("[1] type_name: want ErrorResponse, got %q", entries[1].TypeName)
	}
	if entries[2].TypeName != "" {
		t.Errorf("[2] type_name: want empty (no implementation), got %q", entries[2].TypeName)
	}
}

func TestExtractAPIResponseAnnotations_JAXRSLegacy(t *testing.T) {
	// JAX-RS 2.x / Swagger @ApiResponse with code (int) + response class
	joined := `
@GET
@ApiResponse(code = 200, message = "OK", response = LoginResponse.class)
@ApiResponse(code = 401, message = "Unauthorized", response = ErrorDTO.class)
`
	entries := extractAPIResponseAnnotations(joined)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].StatusCode != 200 {
		t.Errorf("status 0: want 200, got %d", entries[0].StatusCode)
	}
	if entries[0].TypeName != "LoginResponse" {
		t.Errorf("type 0: want LoginResponse, got %q", entries[0].TypeName)
	}
	if entries[1].StatusCode != 401 {
		t.Errorf("status 1: want 401, got %d", entries[1].StatusCode)
	}
}

func TestExtractAPIResponseAnnotations_Deduplication(t *testing.T) {
	// Duplicate status codes should be collapsed (first-wins).
	joined := `
@APIResponse(responseCode = "200", description = "first")
@APIResponse(responseCode = "200", description = "second")
@APIResponse(responseCode = "404", description = "not found")
`
	entries := extractAPIResponseAnnotations(joined)
	if len(entries) != 2 {
		t.Fatalf("dedup: want 2 entries, got %d", len(entries))
	}
	if entries[0].StatusCode != 200 || entries[1].StatusCode != 404 {
		t.Errorf("dedup: want [200 404], got [%d %d]", entries[0].StatusCode, entries[1].StatusCode)
	}
}

func TestEncodeDecodeAPIResponses_RoundTrip(t *testing.T) {
	entries := []APIResponseEntry{
		{StatusCode: 200, TypeName: "LoginResponse"},
		{StatusCode: 404, TypeName: "ErrorDTO", TypeEntityID: "myrepo:abc123", HasChildren: true},
		{StatusCode: 500},
	}
	encoded := EncodeAPIResponses(entries)
	if encoded == "" {
		t.Fatal("EncodeAPIResponses returned empty string")
	}
	decoded := DecodeAPIResponses(encoded)
	if len(decoded) != 3 {
		t.Fatalf("DecodeAPIResponses: want 3 entries, got %d", len(decoded))
	}
	if decoded[1].TypeEntityID != "myrepo:abc123" {
		t.Errorf("type_entity_id round-trip failed: %q", decoded[1].TypeEntityID)
	}
	if !decoded[1].HasChildren {
		t.Error("has_children round-trip failed")
	}
}

func TestAPIResponseAnnotations_EmittedOnEndpointEntity(t *testing.T) {
	// End-to-end: the api_responses property is set on the emitted entity.
	src := `package com.example;
import jakarta.ws.rs.*;
import org.eclipse.microprofile.openapi.annotations.responses.APIResponse;
import org.eclipse.microprofile.openapi.annotations.media.Content;
import org.eclipse.microprofile.openapi.annotations.media.Schema;

@Path("/transfers")
public class TransfersController {
    @PUT
    @Path("/confirm")
    @APIResponse(responseCode = "200", content = @Content(schema = @Schema(implementation = TransferResult.class)))
    @APIResponse(responseCode = "404", content = @Content(schema = @Schema(implementation = ErrorResponse.class)))
    public TransferResult confirmTransfer() { return null; }
}
`
	recs := collect(t, map[string]string{"TransfersController.java": src})
	var ep *recordLike
	for i := range recs {
		if recs[i].Props["path"] == "/transfers/confirm" {
			ep = &recs[i]
			break
		}
	}
	if ep == nil {
		t.Fatal("no endpoint entity found for /transfers/confirm")
	}
	raw := ep.Props["api_responses"]
	if raw == "" {
		t.Fatal("api_responses property is empty — annotation extraction did not fire")
	}
	var entries []APIResponseEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("failed to decode api_responses: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].StatusCode != 200 {
		t.Errorf("entry[0].StatusCode: want 200, got %d", entries[0].StatusCode)
	}
	if entries[0].TypeName != "TransferResult" {
		t.Errorf("entry[0].TypeName: want TransferResult, got %q", entries[0].TypeName)
	}
	if entries[1].StatusCode != 404 {
		t.Errorf("entry[1].StatusCode: want 404, got %d", entries[1].StatusCode)
	}
	if entries[1].TypeName != "ErrorResponse" {
		t.Errorf("entry[1].TypeName: want ErrorResponse, got %q", entries[1].TypeName)
	}
}
