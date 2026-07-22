// flagon_deadlock_5870_pr7a_test.go — deretain-flip PR7a (#5870), review round 2.
//
// The flag-ON default read path (serveFromMMap()==true with a LIVE Reader) holds
// the repo's readerMu across the ENTIRE forEach* scan (Option-B, ADR-0027). Any
// rmu-locking accessor called from INSIDE a forEach callback — relationshipAt
// (PR7a), getByIDOne/LabelIndex.at (pre-existing), or a NESTED forEach — re-locks
// that non-reentrant mutex and self-deadlocks. The suite previously only ran
// these tools flag-OFF (Doc-only repos take the unlocked fallback), so the
// deadlock was invisible.
//
// These tests run the affected tools flag-ON with a LIVE Reader and an EMPTIED
// Doc (simulating the future slice-drop), each under a hard timeout: without the
// collect-then-process fixes they HANG (fail); with them they complete AND match
// the flag-OFF full-Doc result.
package mcp

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// noHang runs fn in a goroutine and fails if it does not return within d — the
// deadlock guard. Returns only after fn completes (or the test has failed).
func noHang(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("DEADLOCK: %s hung >%s on the flag-ON live-Reader path", what, d)
	}
}

// contractMroTopoDoc builds one fixture exercising the flag-ON deadlock sites:
// a NestJS endpoint+handler+VALIDATES (composeNestContract→relPropsFor), an
// Express endpoint+handler+VALIDATES, a DRF ViewSet class + members + EXTENDS
// (findViewSetClassEntity / buildMROInbound), and Topic entities with
// PUBLISHES_TO / SUBSCRIBES_TO edges (topology orphan pub/sub).
func contractMroTopoDoc() *graph.Document {
	mkEnt := func(id, name, kind string) graph.Entity {
		return graph.Entity{ID: id, Name: name, QualifiedName: name, Kind: kind, SourceFile: "src/" + id + ".ts", Language: "typescript", StartLine: 1, EndLine: 5}
	}
	nestEp := graph.Entity{ID: "nestep", Name: "POST /users", Kind: "http_endpoint_definition", SourceFile: "src/users.controller.ts", Language: "typescript", StartLine: 10, EndLine: 12}
	nestEp.PropSet("framework", "nestjs")
	nestEp.PropSet("verb", "POST")
	nestEp.PropSet("path", "/users")
	nestHandler := mkEnt("nesthandler", "UsersController.create", "SCOPE.Operation")
	nestDto := mkEnt("nestdto", "CreateUserDto", "SCOPE.Component")

	expEp := graph.Entity{ID: "expep", Name: "GET /items", Kind: "http_endpoint_definition", SourceFile: "src/items.ts", Language: "javascript", StartLine: 20, EndLine: 22}
	expEp.PropSet("framework", "express")
	expEp.PropSet("verb", "GET")
	expEp.PropSet("path", "/items")
	expHandler := mkEnt("exphandler", "getItems", "SCOPE.Operation")
	expHandler.SourceFile = "src/items.js"
	expDto := mkEnt("expdto", "itemSchema", "SCOPE.Component")

	faEp := graph.Entity{ID: "faep", Name: "GET /things", Kind: "http_endpoint_definition", SourceFile: "app/api.py", Language: "python", StartLine: 30, EndLine: 32}
	faEp.PropSet("framework", "fastapi")
	faEp.PropSet("verb", "GET")
	faEp.PropSet("path", "/things")
	faHandler := mkEnt("fahandler", "get_thing", "SCOPE.Operation")
	faHandler.SourceFile = "app/api.py"

	spEp := graph.Entity{ID: "spep", Name: "GET /accounts", Kind: "http_endpoint_definition", SourceFile: "src/AccountController.java", Language: "java", StartLine: 40, EndLine: 42}
	spEp.PropSet("framework", "spring")
	spEp.PropSet("verb", "GET")
	spEp.PropSet("path", "/accounts")
	spHandler := mkEnt("sphandler", "AccountController.list", "SCOPE.Operation")
	spHandler.SourceFile = "src/AccountController.java"

	// DRF ViewSet class + member (MRO).
	vs := graph.Entity{ID: "vs", Name: "UserViewSet", QualifiedName: "UserViewSet", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "api/views.py", Language: "python", StartLine: 1, EndLine: 30}
	member := graph.Entity{ID: "vsmember", Name: "UserViewSet.list", QualifiedName: "UserViewSet.list", Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "api/views.py", Language: "python", StartLine: 5, EndLine: 9}
	member.PropSet("has_real_body", "true")

	// Topics + pub/sub edges.
	topicPub := graph.Entity{ID: "t_orders", Name: "orders.created", Kind: "Topic", SourceFile: "events.py", Language: "python"}
	topicSub := graph.Entity{ID: "t_ships", Name: "ships.done", Kind: "Topic", SourceFile: "events.py", Language: "python"}
	pubSvc := mkEnt("pubsvc", "OrderService", "SCOPE.Operation")
	subSvc := mkEnt("subsvc", "ShipService", "SCOPE.Operation")

	ents := []graph.Entity{nestEp, nestHandler, nestDto, expEp, expHandler, expDto, faEp, faHandler, spEp, spHandler, vs, member, topicPub, topicSub, pubSvc, subSvc}

	mkRel := func(from, to, kind string, props map[string]string) graph.Relationship {
		r := graph.Relationship{FromID: from, ToID: to, Kind: kind}
		if props != nil {
			r.PropsReplace(props)
		}
		return r
	}
	rels := []graph.Relationship{
		mkRel("nesthandler", "nestep", "IMPLEMENTS", nil),
		mkRel("nesthandler", "nestdto", "VALIDATES", map[string]string{"dto": "CreateUserDto", "method": "body"}),
		mkRel("exphandler", "expep", "IMPLEMENTS", nil),
		mkRel("exphandler", "expdto", "VALIDATES", map[string]string{"dto": "itemSchema", "method": "query"}),
		mkRel("fahandler", "faep", "IMPLEMENTS", nil),
		mkRel("sphandler", "spep", "IMPLEMENTS", nil),
		mkRel("vs", "ModelViewSet", "EXTENDS", map[string]string{"base_name": "rest_framework.viewsets.ModelViewSet"}),
		mkRel("vs", "vsmember", "CONTAINS", nil),
		mkRel("pubsvc", "t_orders", "PUBLISHES_TO", nil),
		mkRel("subsvc", "t_ships", "SUBSCRIBES_TO", nil),
	}
	return &graph.Document{Repo: "corpus", Entities: ents, Relationships: rels}
}

func loadContractMroTopoFixture(t *testing.T) (*graph.Document, *fbreader.Reader) {
	t.Helper()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, contractMroTopoDoc()); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return doc, r
}

// lgWith builds a single-repo LoadedGroup around lr.
func lgWith(lr *LoadedRepo) *LoadedGroup {
	return &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"corpus": lr}}
}

func TestEffectiveContractNestJS_flagON_noDeadlock_PR7a(t *testing.T) {
	doc, r := loadContractMroTopoFixture(t)

	withServeFromMMap(t, false)
	wantGroups, wantOK := nestJSContractResolver{}.Resolve(lgWith(docFullRepo(doc)), "UsersController", "userscontroller")
	if !wantOK || len(wantGroups) == 0 {
		t.Fatalf("fixture must yield a NestJS contract (ok=%v groups=%d)", wantOK, len(wantGroups))
	}

	withServeFromMMap(t, true)
	var gotGroups []effectiveContractGroup
	var gotOK bool
	noHang(t, 5*time.Second, "nestJSContractResolver.Resolve", func() {
		gotGroups, gotOK = nestJSContractResolver{}.Resolve(lgWith(readerEmptiedRepo(t, doc, r)), "UsersController", "userscontroller")
	})
	if gotOK != wantOK || !reflect.DeepEqual(gotGroups, wantGroups) {
		t.Fatalf("NestJS contract flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", gotGroups, wantGroups)
	}
}

func TestEffectiveContractExpress_flagON_noDeadlock_PR7a(t *testing.T) {
	doc, r := loadContractMroTopoFixture(t)

	withServeFromMMap(t, false)
	wantGroups, wantOK := expressContractResolver{}.Resolve(lgWith(docFullRepo(doc)), "getItems", "getitems")

	withServeFromMMap(t, true)
	var gotGroups []effectiveContractGroup
	var gotOK bool
	noHang(t, 5*time.Second, "expressContractResolver.Resolve", func() {
		gotGroups, gotOK = expressContractResolver{}.Resolve(lgWith(readerEmptiedRepo(t, doc, r)), "getItems", "getitems")
	})
	if gotOK != wantOK || !reflect.DeepEqual(gotGroups, wantGroups) {
		t.Fatalf("Express contract flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", gotGroups, wantGroups)
	}
}

// TestEffectiveContractFastAPISpring_flagON_noDeadlock_PR7a covers the sibling
// frameworks (fastapi + spring), whose forEach scans have the SAME pre-existing
// frameworkHandlerEntity→getByIDOne in-scan hazard, fixed for family consistency.
func TestEffectiveContractFastAPISpring_flagON_noDeadlock_PR7a(t *testing.T) {
	doc, r := loadContractMroTopoFixture(t)

	cases := []struct {
		name    string
		resolve func(lg *LoadedGroup) ([]effectiveContractGroup, bool)
	}{
		{"fastapi", func(lg *LoadedGroup) ([]effectiveContractGroup, bool) {
			return fastAPIContractResolver{}.Resolve(lg, "get_thing", "get_thing")
		}},
		{"spring", func(lg *LoadedGroup) ([]effectiveContractGroup, bool) {
			return springContractResolver{}.Resolve(lg, "AccountController", "accountcontroller")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withServeFromMMap(t, false)
			wantG, wantOK := tc.resolve(lgWith(docFullRepo(doc)))

			withServeFromMMap(t, true)
			var gotG []effectiveContractGroup
			var gotOK bool
			noHang(t, 5*time.Second, tc.name+"ContractResolver.Resolve", func() {
				gotG, gotOK = tc.resolve(lgWith(readerEmptiedRepo(t, doc, r)))
			})
			if gotOK != wantOK || !reflect.DeepEqual(gotG, wantG) {
				t.Fatalf("%s contract flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", tc.name, gotG, wantG)
			}
		})
	}
}

func TestFindViewSetClassEntity_flagON_noDeadlock_PR7a(t *testing.T) {
	doc, r := loadContractMroTopoFixture(t)

	withServeFromMMap(t, false)
	want := findViewSetClassEntity(docFullRepo(doc), "userviewset")
	if want == nil {
		t.Fatal("fixture must contain a UserViewSet class entity with an EXTENDS edge")
	}

	withServeFromMMap(t, true)
	var got *graph.Entity
	noHang(t, 5*time.Second, "findViewSetClassEntity", func() {
		got = findViewSetClassEntity(readerEmptiedRepo(t, doc, r), "userviewset")
	})
	if got == nil || got.ID != want.ID {
		t.Fatalf("findViewSetClassEntity flag-ON(emptied Doc) got=%v want id=%s", got, want.ID)
	}
}

func TestBuildMROInbound_flagON_noDeadlock_PR7a(t *testing.T) {
	doc, r := loadContractMroTopoFixture(t)

	withServeFromMMap(t, false)
	want := buildMROInbound(docFullRepo(doc))

	withServeFromMMap(t, true)
	var got map[string][]string
	noHang(t, 5*time.Second, "buildMROInbound", func() {
		got = buildMROInbound(readerEmptiedRepo(t, doc, r))
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildMROInbound flag-ON(emptied Doc) != flag-OFF\n got=%#v\nwant=%#v", got, want)
	}
}

func topologyServer(t *testing.T, lr *LoadedRepo) *Server {
	t.Helper()
	reg := &Registry{Groups: map[string]RegistryGroup{"g": {Repos: map[string]RegistryRepo{"corpus": {Path: t.TempDir()}}}}}
	st := NewState(reg)
	st.mu.Lock()
	st.groups["g"] = lgWith(lr)
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

func callTopology(t *testing.T, s *Server, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error)) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(mcpapi.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestTopologyOrphans_flagON_noDeadlock_PR7a(t *testing.T) {
	doc, r := loadContractMroTopoFixture(t)

	withServeFromMMap(t, false)
	sOff := topologyServer(t, docFullRepo(doc))
	wantPub := callTopology(t, sOff, sOff.handleTopologyOrphanPublishers)
	wantSub := callTopology(t, sOff, sOff.handleTopologyOrphanSubscribers)

	withServeFromMMap(t, true)
	sOn := topologyServer(t, readerEmptiedRepo(t, doc, r))
	var gotPub, gotSub string
	noHang(t, 5*time.Second, "handleTopologyOrphanPublishers", func() {
		gotPub = callTopology(t, sOn, sOn.handleTopologyOrphanPublishers)
	})
	noHang(t, 5*time.Second, "handleTopologyOrphanSubscribers", func() {
		gotSub = callTopology(t, sOn, sOn.handleTopologyOrphanSubscribers)
	})
	if gotPub != wantPub {
		t.Fatalf("orphan publishers flag-ON != flag-OFF\n got=%s\nwant=%s", gotPub, wantPub)
	}
	if gotSub != wantSub {
		t.Fatalf("orphan subscribers flag-ON != flag-OFF\n got=%s\nwant=%s", gotSub, wantSub)
	}
	// Teeth: the fixture has exactly one orphan publisher (orders.created) and one
	// orphan subscriber (ships.done).
	if wantPub == "" || wantSub == "" {
		t.Fatal("fixture must produce a non-empty topology result")
	}
}
