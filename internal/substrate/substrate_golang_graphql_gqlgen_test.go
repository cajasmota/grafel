package substrate

import "testing"

// substrate_golang_graphql_gqlgen_test.go — issue #3918: prove the
// framework-blind, per-LANGUAGE Go substrate sniffers fire on gqlgen
// (Go GraphQL) generated-resolver source. This is the Go analog of the
// jsts-Pothos (#3903) and python-graphene (#3911) verify-first credits.
//
// Every Go substrate sniffer is registered on the "go" language slug and
// dispatched solely by file extension via LanguageForPath (see
// internal/substrate/substrate.go LanguageForPath: ".go" -> "go"). None of
// def_use_golang.go / effect_sinks_golang.go / taint_sites_golang.go / golang.go
// contains a single framework reference; they Register("go", …) /
// RegisterDefUseSniffer("go", …) / RegisterEffectSniffer("go", …) /
// RegisterTaintSniffer("go", …) unconditionally. gqlgen resolvers live in
// ordinary Go (.go) files and therefore already receive these passes.
//
// VERIFY-FIRST findings encoded as assertions below:
//
//  1. The substrate primitive DETECTORS fire on a gqlgen generated-resolver
//     body and ATTRIBUTE to the enclosing resolver method:
//       - db_read  (gorm .First / .Find)            — proven
//       - db_write (gorm .Create / .Save)           — proven
//       - http_out (http.Get / client.Do)           — proven
//       - mutation (recv.field = …)                 — proven
//       - def/use chains over locals                — proven
//       - SQL-injection taint SINK (db.Query(fmt.Sprintf …)) — proven
//     Attribution succeeds for the gqlgen generated receiver form
//     `func (r *mutationResolver) CreateTodo(ctx …) (…)` because the Go
//     func-header scanner (goFuncHeaderRe in effect_sinks_golang.go) strips the
//     `(r *mutationResolver)` receiver and binds to the bare method name.
//
//     VERIFY-FIRST CAVEAT (why `partial`, not `full`): the SQL-sink regex
//     goSinkSQLRe anchors on a bare receiver token `db|tx|stmt|conn`, so it
//     fires on the common `db := r.DB; db.Query(fmt.Sprintf(…))` handle form
//     (and a package-level `db`) but NOT on the field-receiver `r.DB.Query(…)`
//     form, nor on the `"literal" + var` concat shape (only `ident + …` or
//     fmt.Sprintf). The effect detectors (.First/.Create/.Find/.Save) match on
//     any receiver and so fire on both `r.DB.X` and `db.X`. Hence partial.
//
//  2. Honest NEGATIVES (left `missing`/`n-a` with documented reason):
//       - taint SOURCE: the Go source regexes key on net/http request
//         accessors (r.URL.Query/Form/Body) and gin/chi/echo/fiber context
//         getters. A gqlgen resolver receives untrusted input via typed
//         resolver `args` (function parameters) + `ctx`, NOT those request
//         accessors, so no taint source fires. (#3918 negative probe.)
//       - request_sink_dataflow: there is NO Go dataflow sniffer registered at
//         all (only "jsts" and "python" call RegisterDataFlowSnifferEx in
//         dataflow_jsts.go / dataflow_python.go). The request_sink flow cannot
//         fire for ANY Go framework, and gqlgen reads args not req.body anyway.
//         Doubly N/A. (#3918 negative probe.)
//       - Type System / DTO: gqlgen is SDL-driven (schema-first); the object
//         type graph is parsed from *.graphql by the shared graphql extractor
//         (already credited under Schema -> Type graph extraction `full`).
//         Generated Go resolvers carry operation glue only, no code-first Go
//         type declarations, so no Go type/DTO extractor applies.
//
// These probes justify flipping the per-language Substrate effect / mutation /
// def_use / taint-sink + Data DB-effect cells on the gqlgen record to `partial`
// (honest: detectors fire + attribute on the generated resolver form), while
// leaving taint-source, request_sink_dataflow, and the code-first Type System /
// DTO cells `missing`.

// gqlgenResolverSrc is a representative gqlgen generated-resolver module: the
// canonical `func (r *mutationResolver) <Field>(ctx, args…)` receiver form,
// performing a gorm DB read + write, an outbound HTTP call, a struct-field
// mutation, and a raw-SQL concat sink.
const gqlgenResolverSrc = `
package graph

import (
	"context"
	"fmt"
	"net/http"
)

// CreateTodo is the resolver for the createTodo field.
func (r *mutationResolver) CreateTodo(ctx context.Context, input NewTodo) (*Todo, error) {
	text := input.Text
	db := r.DB
	existing := &Todo{}
	db.First(existing, "text = ?", text)
	created := &Todo{Text: text, UserID: input.UserID}
	db.Create(created)
	created.Done = true
	http.Get("https://audit.example.com/log")
	rows, _ := db.Query(fmt.Sprintf("SELECT * FROM todos WHERE text = '%s'", text))
	_ = rows
	return created, nil
}

// Todos is the resolver for the todos query field.
func (r *queryResolver) Todos(ctx context.Context) ([]*Todo, error) {
	out := []*Todo{}
	r.DB.Find(&out)
	return out, nil
}
`

// --- gqlgen positive probes -------------------------------------------------

func TestSubstrate_Go_Gqlgen_DefUseAttributes(t *testing.T) {
	defs, uses := sniffDefUseGo(gqlgenResolverSrc)
	if !hasDefIn(defs, "text", "CreateTodo") {
		t.Errorf("def_use: expected def of `text` in CreateTodo, defs=%+v", defs)
	}
	if !hasUseIn(uses, "text", "CreateTodo") {
		t.Errorf("def_use: expected use of `text` in CreateTodo, uses=%+v", uses)
	}
}

func TestSubstrate_Go_Gqlgen_EffectsAttribute(t *testing.T) {
	ms := sniffEffectsGo(gqlgenResolverSrc)
	for _, want := range []Effect{EffectDBRead, EffectDBWrite, EffectHTTPOut, EffectMutation} {
		if !hasEffectIn(ms, want, "CreateTodo") {
			t.Errorf("effects: expected %s attributed to CreateTodo, got %+v", want, ms)
		}
	}
	// db_read also fires + attributes on the second (query) resolver.
	if !hasEffectIn(ms, EffectDBRead, "Todos") {
		t.Errorf("effects: expected db_read attributed to Todos, got %+v", ms)
	}
}

func TestSubstrate_Go_Gqlgen_TaintSinkFires(t *testing.T) {
	ms := sniffTaintGo(gqlgenResolverSrc)
	if countTaint(ms, TaintKindSink, TaintCategorySQL) == 0 {
		t.Errorf("taint: expected a SQL-injection sink (raw db.Query concat), got %+v", ms)
	}
}

// --- gqlgen negative probes (honest non-credit) -----------------------------

// TestSubstrate_Go_Gqlgen_TaintSourceDoesNotFire documents WHY
// taint_source_detection is left `missing`: the Go taint SOURCE regexes key on
// net/http request accessors (r.URL.Query/Form/Body) and gin/chi/echo/fiber
// context getters. A gqlgen resolver receives untrusted input via typed
// resolver args (function parameters), so no taint source is produced.
func TestSubstrate_Go_Gqlgen_TaintSourceDoesNotFire(t *testing.T) {
	ms := sniffTaintGo(gqlgenResolverSrc)
	if n := countTaint(ms, TaintKindSource, TaintCategoryGeneric); n != 0 {
		t.Errorf("expected NO taint source (resolver reads typed args, not r.URL/ctx getters); got %d: %+v", n, ms)
	}
}

// TestSubstrate_Go_Gqlgen_NoGoDataFlowSniffer documents WHY
// request_sink_dataflow is left `missing`: no Go dataflow sniffer is registered
// at all — only "jsts" and "python" appear in the dataflow registry. The flow
// therefore cannot fire for any Go framework, and gqlgen reads args not
// req.body regardless.
func TestSubstrate_Go_Gqlgen_NoGoDataFlowSniffer(t *testing.T) {
	if DataFlowSnifferFor("go") != nil || DataFlowSnifferExFor("go") != nil {
		t.Errorf("expected NO registered Go dataflow sniffer (request_sink is jsts/python only); one was found")
	}
}
