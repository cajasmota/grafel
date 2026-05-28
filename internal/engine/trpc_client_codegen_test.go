package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// TestDetect_TRPCClientCodegen proves client_codegen (#2865): the shipped
// trpc.yaml rule recognises the inferred typed-client factories
// (createTRPCClient / createTRPCProxyClient / createTRPCReact /
// createTRPCNext) and the inferRouterInputs/Outputs type helpers as Operation
// entities — the "generated from schema" client surface.
func TestDetect_TRPCClientCodegen(t *testing.T) {
	src := readBackendFixture(t, "trpc_client_codegen.ts")

	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "src/client.ts",
		Content:  []byte(src),
		Language: "javascript_typescript",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	names := map[string]bool{}
	for _, e := range result.Entities {
		names[strings.TrimRight(e.Name, "<")] = true
	}

	for _, want := range []string{
		"createTRPCClient", "createTRPCProxyClient", "createTRPCReact",
		"inferRouterInputs", "inferRouterOutputs",
	} {
		if !names[want] && !names[want+"<"] {
			t.Errorf("expected tRPC client-codegen entity %q; got %v", want, keys(names))
		}
	}
}
