package links

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #6902 — frameworkEntryKinds drops bare "Route" (types.EntityKindRouteBare).
//
// frameworkEntryKinds is the link pass's SEED set: kinds whose mere presence
// makes an entity a framework-managed entry-point, no inbound edge required.
// It holds "SCOPE.Route" and not "Route", so every bare-"Route" entity — the
// spelling internal/custom/java/{play,spring_webflux,akka_http,javalin,vertx,
// struts}_routes.go and internal/engine/{spring,django}_routes.go emit, and
// the spelling four golden fixtures assert with must_exist:true — is left
// unseeded and written to the reachability sidecar as UNREACHABLE, together
// with everything only it reaches.
//
// Both Route spellings name the same concept (#6894 §1b; #6776 arm B7 added
// EntityKindRoute and EntityKindRouteBare together, noting both are live), so
// both belong in the seed set. internal/mcp/dead_code.go's
// frameworkEntryKindsMCP is a documented mirror of this map and carries the
// same gap; it is fixed and graded in that package.
//
// THE POSITIVE CONTROL, against the #6893 trap. This fixture has NO EDGES at
// all and an EMPTY FileRoot with an unrecognised source extension, so neither
// the framework-invocation edge kinds (HANDLES / ROUTES_TO / REGISTERS / …)
// nor the source sniffer can seed anything. The ONLY thing that can make an
// entity reachable here is frameworkEntryKinds[e.Kind], and "Route" is a key
// no other entry in that map equals. The http_endpoint_definition entity is
// the vacuity guard only.
//
// The assertion reads the EMITTED ARTEFACT — the persisted
// <group>-reachability.json sidecar — not an in-memory counter.
// ---------------------------------------------------------------------------

func TestReachability_BareRouteIsAFrameworkEntrySeed_6902(t *testing.T) {
	graphs := []repoGraph{{
		Repo:     "repo-6902",
		FileRoot: t.TempDir(), // empty: the source sniffer can seed nothing
		Entities: []entityNode{
			{ID: "bare-route-6902", Name: "/orders-6902",
				Kind: string(types.EntityKindRouteBare), SourceFile: "src/x.unknown"},
			{ID: "scope-route-6902", Name: "/invoices-6902",
				Kind: string(types.EntityKindRoute), SourceFile: "src/x.unknown"},
			{ID: "decoy-6902", Name: "LedgerReconciler6902",
				Kind: string(types.EntityKindClass), SourceFile: "src/x.unknown"},
			{ID: "real-http-6902", Name: "/health-6902",
				Kind: "http_endpoint_definition", SourceFile: "src/x.unknown"},
		},
		// deliberately no edges
	}}

	paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
	if _, err := runReachabilityPass("g", graphs, paths); err != nil {
		t.Fatalf("runReachabilityPass: %v", err)
	}

	sidecar := strings.TrimSuffix(paths.Links, ".json") + "-reachability.json"
	buf, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	var doc reachabilityDocument
	if err := json.Unmarshal(buf, &doc); err != nil {
		t.Fatalf("unmarshal sidecar: %v", err)
	}
	got := map[string]bool{}
	seen := map[string]bool{}
	for _, e := range doc.Entries {
		got[e.EntityID] = e.Reachable
		seen[e.EntityID] = true
	}

	// 1. vacuity guard — if the sidecar lists nothing, or lists nothing as
	//    reachable, the assertions below grade nothing.
	if !seen["real-http-6902"] || !got["real-http-6902"] {
		t.Fatalf("the http_endpoint_definition seed is not in the sidecar as "+
			"reachable (seen=%v reachable=%v); every assertion below would be "+
			"vacuous. entries=%d", seen["real-http-6902"], got["real-http-6902"], len(doc.Entries))
	}
	// 2. the #6902 recall direction.
	if !got["bare-route-6902"] {
		t.Errorf("the bare-%q entity is reachable=%v in the sidecar. "+
			"frameworkEntryKinds holds \"SCOPE.Route\" but not \"Route\", so every "+
			"Java/Spring/Django route is unseeded and persisted as unreachable.",
			string(types.EntityKindRouteBare), got["bare-route-6902"])
	}
	// 3. SCOPE.Route must stay seeded.
	if !got["scope-route-6902"] {
		t.Errorf("the %q entity is reachable=%v in the sidecar; adding the bare "+
			"spelling must not cost the prefixed one.",
			string(types.EntityKindRoute), got["scope-route-6902"])
	}
	// 4. the FORBIDDEN direction — a SCOPE.Class with no edges is NOT a
	//    framework entry-point and must stay unreachable.
	if got["decoy-6902"] {
		t.Errorf("the %q decoy is reachable=true with no inbound edge and no "+
			"sniffable source. Adding bare \"Route\" to frameworkEntryKinds must "+
			"not seed anything else.", string(types.EntityKindClass))
	}
}
