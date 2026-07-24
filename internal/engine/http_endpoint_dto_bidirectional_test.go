// Tests for the DTO ↔ Handler bidirectional REFERENCES edge pass (#1999).
//
// http_endpoint_definition entities encode the handler → DTO direction via
// the request_body_type / response_body_type properties. The
// ResolveHTTPEndpointHandlers post-pass must additionally emit a REFERENCES
// edge in the INVERSE direction (DTO → handler) so the DTO entity is
// self-documenting in its 1-hop neighbourhood.

package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

func TestResolveHTTPEndpointHandlers_DTOHandlerBidirectional_RequestBody(t *testing.T) {
	merged := []types.EntityRecord{
		// DTO target — typical Spring @RequestBody parameter type.
		{
			ID:         "dto-create-order",
			Name:       "CreateOrderRequest",
			Kind:       "SCOPE.Component",
			SourceFile: "src/main/java/com/example/dto/CreateOrderRequest.java",
		},
		// http_endpoint_definition consumer.
		{
			ID:         "ep-post-orders",
			Name:       "http:POST:/orders",
			Kind:       httpEndpointDefinitionKind,
			SourceFile: "src/main/java/com/example/api/OrderController.java",
			Properties: map[string]string{
				"request_body_type": "CreateOrderRequest",
			},
		},
	}

	out, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.DTOHandlerEdgesEmitted != 1 {
		t.Fatalf("want DTOHandlerEdgesEmitted=1, got %d", stats.DTOHandlerEdgesEmitted)
	}
	if stats.DTOHandlerEdgesUnresolved != 0 {
		t.Errorf("want DTOHandlerEdgesUnresolved=0, got %d", stats.DTOHandlerEdgesUnresolved)
	}

	// Locate the DTO and verify the new edge.
	var dto *types.EntityRecord
	for i := range out {
		if out[i].ID == "dto-create-order" {
			dto = &out[i]
			break
		}
	}
	if dto == nil {
		t.Fatal("DTO entity dropped from merged set")
	}
	if len(dto.Relationships) != 1 {
		t.Fatalf("want 1 relationship on DTO, got %d: %+v", len(dto.Relationships), dto.Relationships)
	}
	rel := dto.Relationships[0]
	if rel.Kind != "REFERENCES" {
		t.Errorf("want REFERENCES, got %q", rel.Kind)
	}
	if rel.FromID != "dto-create-order" {
		t.Errorf("want FromID=dto-create-order, got %q", rel.FromID)
	}
	if rel.ToID != "ep-post-orders" {
		t.Errorf("want ToID=ep-post-orders, got %q", rel.ToID)
	}
	if rel.Properties.Get("reference_kind") != "request_body" {
		t.Errorf("want reference_kind=request_body, got %q", rel.Properties.Get("reference_kind"))
	}
}

func TestResolveHTTPEndpointHandlers_DTOHandlerBidirectional_ResponseBody(t *testing.T) {
	merged := []types.EntityRecord{
		{
			ID:         "dto-order-response",
			Name:       "OrderResponse",
			Kind:       "SCOPE.Component",
			SourceFile: "src/main/java/com/example/dto/OrderResponse.java",
		},
		{
			ID:         "ep-get-orders",
			Name:       "http:GET:/orders",
			Kind:       httpEndpointDefinitionKind,
			SourceFile: "src/main/java/com/example/api/OrderController.java",
			Properties: map[string]string{
				"response_body_type": "OrderResponse",
			},
		},
	}

	out, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.DTOHandlerEdgesEmitted != 1 {
		t.Fatalf("want DTOHandlerEdgesEmitted=1, got %d", stats.DTOHandlerEdgesEmitted)
	}
	var dto *types.EntityRecord
	for i := range out {
		if out[i].ID == "dto-order-response" {
			dto = &out[i]
		}
	}
	if dto == nil || len(dto.Relationships) != 1 {
		t.Fatalf("DTO missing REFERENCES edge")
	}
	if dto.Relationships[0].Properties.Get("reference_kind") != "response_body" {
		t.Errorf("want reference_kind=response_body, got %q",
			dto.Relationships[0].Properties.Get("reference_kind"))
	}
}

func TestResolveHTTPEndpointHandlers_DTOHandlerBidirectional_UnresolvedExternalDTO(t *testing.T) {
	// External / third-party DTO type — no matching entity in merged.
	merged := []types.EntityRecord{
		{
			ID:         "ep-post-ext",
			Name:       "http:POST:/webhook",
			Kind:       httpEndpointDefinitionKind,
			SourceFile: "src/main/java/com/example/api/Webhook.java",
			Properties: map[string]string{
				"request_body_type": "ExternalSdkPayload",
			},
		},
	}
	_, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.DTOHandlerEdgesEmitted != 0 {
		t.Errorf("want emitted=0 for unknown DTO, got %d", stats.DTOHandlerEdgesEmitted)
	}
	if stats.DTOHandlerEdgesUnresolved != 1 {
		t.Errorf("want unresolved=1 for unknown DTO, got %d", stats.DTOHandlerEdgesUnresolved)
	}
}

func TestResolveHTTPEndpointHandlers_DTOHandlerBidirectional_BothBodies(t *testing.T) {
	// Single endpoint with both request and response DTO types — must emit
	// two edges from the respective DTOs.
	merged := []types.EntityRecord{
		{
			ID:   "dto-in",
			Name: "TransferRequest",
			Kind: "SCOPE.Component",
		},
		{
			ID:   "dto-out",
			Name: "TransferResponse",
			Kind: "SCOPE.Component",
		},
		{
			ID:   "ep-put-transfer",
			Name: "http:PUT:/transfers",
			Kind: httpEndpointDefinitionKind,
			Properties: map[string]string{
				"request_body_type":  "TransferRequest",
				"response_body_type": "TransferResponse",
			},
		},
	}
	out, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.DTOHandlerEdgesEmitted != 2 {
		t.Fatalf("want emitted=2, got %d", stats.DTOHandlerEdgesEmitted)
	}
	// Each DTO should now have exactly one REFERENCES edge.
	for _, want := range []string{"dto-in", "dto-out"} {
		var found bool
		for i := range out {
			if out[i].ID == want {
				if len(out[i].Relationships) != 1 {
					t.Errorf("DTO %s: want 1 edge, got %d", want, len(out[i].Relationships))
				}
				found = true
			}
		}
		if !found {
			t.Errorf("DTO %s missing from merged", want)
		}
	}
}
