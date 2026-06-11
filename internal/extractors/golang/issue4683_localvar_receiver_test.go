// issue4683_localvar_receiver_test.go — Go local-variable receiver typing for
// test→CALLS coverage crediting (issue #4683, part of epic #4615/#4672).
//
// Generalises the TS/JS (#4680), Python (#4716) and Java (#4717) local-receiver
// wins to Go. A Go unit test binds a local from a constructor and then calls a
// method on it; the call must resolve to the type's method so ComputeCoverage
// credits the endpoint through test→CALLS→handler.
//
// Go already typed several receiver-local shapes (collectBodyVarTypes /
// collectParamTypes, #364/#1840): composite literals (`x := &Foo{}`), declared
// `var x T`, method receivers/params, and a hardcoded constructor allowlist
// (`r := chi.NewRouter()`). The genuine RED this child closes is the
// USER-DEFINED same-package constructor: `svc := NewProposalService();
// svc.GetCounts()`. `NewProposalService` is not in the allowlist, so the local
// was left untyped and the method call carried no receiver_type stamp.
//
// collectFileConstructorReturns now scans the file's `func NewX(...) *X` /
// `func NewX(...) X` declarations and types the local from the constructor's
// same-file STRUCT return type. Conservatism mirrors the prior slices:
//   - single, unnamed result only (`(T, error)` / multi-return → skipped);
//   - the return type must be a same-file struct (interface-returning factories
//     stay bare — the runtime concrete type is ambiguous);
//   - qualified/slice/map/generic/func return types → skipped.
//
// Route-hit test-client linkage (httptest NewRequest/ServeHTTP/NewServer,
// fixture B in the epic) is already covered by the e2e_route_calls path — see
// internal/custom/golang/httptest_e2e.go and
// internal/engine/http_endpoint_e2e_testmap*. Direct `handler.ServeHTTP(...)`
// calls credit via the test→handler CALLS edge in coverage.go.

package golang_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// callsEdge returns the CALLS edge to method `to` on entity `from` in records,
// or nil.
func callsEdge4683(records []types.EntityRecord, from, to string) *types.RelationshipRecord {
	e := findEntity(records, from)
	if e == nil {
		return nil
	}
	for i := range e.Relationships {
		r := &e.Relationships[i]
		if r.Kind == "CALLS" && r.ToID == to {
			return r
		}
	}
	return nil
}

// Fixture A — user-defined constructor local resolves the method call.
// `svc := NewProposalService(); svc.GetCounts()` inside a Test* func must stamp
// receiver_type=ProposalService on the CALLS→GetCounts edge so coverage credits
// the handler/endpoint through test→CALLS→handler.
func TestIssue4683_ConstructorLocalPointerReturn(t *testing.T) {
	src := `package svc

type ProposalService struct{}

func NewProposalService() *ProposalService { return &ProposalService{} }

func (s *ProposalService) GetCounts() int { return 0 }

func TestGetCounts(t *T) {
	svc := NewProposalService()
	svc.GetCounts()
}
`
	records := extractRecords(t, src, "svc_test.go")
	hit := callsEdge4683(records, "TestGetCounts", "GetCounts")
	if hit == nil {
		t.Fatalf("expected CALLS edge to GetCounts on TestGetCounts")
	}
	if hit.Properties == nil || hit.Properties["receiver_type"] != "ProposalService" {
		t.Errorf("expected receiver_type=ProposalService, got %+v", hit.Properties)
	}
}

// Value-return constructor (`func NewX() X`) is also typed.
func TestIssue4683_ConstructorLocalValueReturn(t *testing.T) {
	src := `package svc

type Handler struct{}

func NewHandler() Handler { return Handler{} }

func (h *Handler) Serve() {}

func TestServe(t *T) {
	h := NewHandler()
	h.Serve()
}
`
	records := extractRecords(t, src, "h_test.go")
	hit := callsEdge4683(records, "TestServe", "Serve")
	if hit == nil {
		t.Fatalf("expected CALLS edge to Serve on TestServe")
	}
	if hit.Properties == nil || hit.Properties["receiver_type"] != "Handler" {
		t.Errorf("expected receiver_type=Handler, got %+v", hit.Properties)
	}
}

// Negative — a factory declared to return an INTERFACE must leave the local
// bare. The concrete type behind `Counter` is a runtime choice, so
// `c := NewCounter(); c.GetCounts()` must NOT carry a receiver_type stamp.
func TestIssue4683_InterfaceFactoryStaysBare(t *testing.T) {
	src := `package svc

type Counter interface { GetCounts() int }

type impl struct{}

func (i *impl) GetCounts() int { return 0 }

func NewCounter() Counter { return &impl{} }

func TestCounter(t *T) {
	c := NewCounter()
	c.GetCounts()
}
`
	records := extractRecords(t, src, "c_test.go")
	hit := callsEdge4683(records, "TestCounter", "GetCounts")
	if hit == nil {
		t.Fatalf("expected CALLS edge to GetCounts on TestCounter")
	}
	if hit.Properties != nil && hit.Properties["receiver_type"] != "" {
		t.Errorf("interface-factory local must stay bare; got receiver_type=%q",
			hit.Properties["receiver_type"])
	}
}

// Negative — a multi-return constructor (`func NewX() (*X, error)`) is NOT a
// single unnamed result, so the local stays bare (tuple-slot pairing is out of
// scope, matching the multi-LHS short-var-decl skip).
func TestIssue4683_MultiReturnConstructorStaysBare(t *testing.T) {
	src := `package svc

type Repo struct{}

func NewRepo() (*Repo, error) { return &Repo{}, nil }

func (r *Repo) Find() {}

func TestFind(t *T) {
	r, _ := NewRepo()
	r.Find()
}
`
	records := extractRecords(t, src, "r_test.go")
	hit := callsEdge4683(records, "TestFind", "Find")
	if hit == nil {
		// multi-LHS short-var-decl may not produce a resolvable CALLS at all;
		// that's an acceptable honest exclusion.
		return
	}
	if hit.Properties != nil && hit.Properties["receiver_type"] != "" {
		t.Errorf("multi-return constructor local must stay bare; got receiver_type=%q",
			hit.Properties["receiver_type"])
	}
}
