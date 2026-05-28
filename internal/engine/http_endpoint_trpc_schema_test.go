package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// trpcDefByPath returns the tRPC http_endpoint_definition whose `path`
// property equals the dotted procedure path, or nil.
func trpcDefByPath(ents []types.EntityRecord, path string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == httpEndpointDefinitionKind &&
			e.Properties["framework"] == "trpc" &&
			e.Properties["path"] == path {
			return e
		}
	}
	return nil
}

// TestTRPCSchema_InputExtraction proves schema_extraction (#2865): each tRPC
// procedure with an `.input(z.object({…}))` validator has its input schema
// recovered and stamped on the emitted endpoint, while a procedure with no
// input is left unstamped.
func TestTRPCSchema_InputExtraction(t *testing.T) {
	src := readBackendFixture(t, "trpc_input_schema.ts")
	res := runDetectWS(t, "typescript", "server/router.ts", src)

	getUser := trpcDefByPath(res.Entities, "getUser")
	if getUser == nil {
		t.Fatalf("expected tRPC endpoint for getUser; entities=%d", len(res.Entities))
	}
	if getUser.Properties["has_input_schema"] != "true" {
		t.Errorf("getUser has_input_schema=%q want true", getUser.Properties["has_input_schema"])
	}
	if getUser.Properties["input_schema_lib"] != "zod" {
		t.Errorf("getUser input_schema_lib=%q want zod", getUser.Properties["input_schema_lib"])
	}
	if s := getUser.Properties["input_schema"]; s == "" ||
		!containsAll(s, "z.object", "id", "z.string") {
		t.Errorf("getUser input_schema=%q missing expected shape", s)
	}

	createUser := trpcDefByPath(res.Entities, "createUser")
	if createUser == nil {
		t.Fatal("expected tRPC endpoint for createUser")
	}
	if createUser.Properties["has_input_schema"] != "true" {
		t.Errorf("createUser should carry input schema, got %+v", createUser.Properties)
	}
	if s := createUser.Properties["input_schema"]; !containsAll(s, "name", "email", "z.string") {
		t.Errorf("createUser input_schema=%q missing fields", s)
	}

	// listUsers has no .input(...) — must be left unstamped (honest).
	listUsers := trpcDefByPath(res.Entities, "listUsers")
	if listUsers == nil {
		t.Fatal("expected tRPC endpoint for listUsers")
	}
	if listUsers.Properties["has_input_schema"] != "" {
		t.Errorf("listUsers should have NO input schema, got %q", listUsers.Properties["has_input_schema"])
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
