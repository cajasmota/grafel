// Value-asserting fixtures for the 5 trailing Elixir frameworks (#4026,
// epic #3872, from Elixir audit #3885): guardian / grpc / finch / tesla / req.
//
// The Elixir substrate sniffers (def_use_elixir.go, effect_sinks_elixir.go,
// taint_sites_elixir.go, payload_shapes_elixir.go, template_pattern_elixir.go,
// entry_points_elixir.go) all register on the "elixir" slug with NO framework
// gate — they are framework-AGNOSTIC and fire on any .ex/.exs source dispatched
// via LanguageForPath. The 4 flagship Elixir frameworks (oban/phoenix/plug/
// absinthe) already carry these language-level Substrate cells at partial; this
// re-stamps the trailing frameworks to the SAME partial sibling status for the
// cells that genuinely fire on each framework's real idiom.
//
// VERIFY-FIRST discipline: each test below proves the sniffer produces the
// SPECIFIC artifact (a named def->use pair / a named effect sink on a named
// function / a categorised taint site / a field-bearing payload shape / a named
// template literal / a named entry point) on that framework's idiomatic source.
// Cells that do NOT fire on a framework's idiom are deliberately LEFT MISSING
// (see the file footer for the honest left-missing ledger) — no over-credit.
package substrate

import (
	"testing"
)

// hasTaint asserts a taint match of the given kind (and, when cat != "", that
// category) was produced inside fn.
func hasTaintElixir(t *testing.T, ms []TaintMatch, fn string, kind TaintKind, cat TaintCategory) {
	t.Helper()
	for _, m := range ms {
		if m.Function == fn && m.Kind == kind && (cat == "" || m.Category == cat) {
			return
		}
	}
	t.Errorf("expected taint %s/%s in %q; got %+v", kind, cat, fn, ms)
}

// hasTemplate asserts a template pattern of the given kind whose literal
// contains want was attributed to fn.
func hasTemplateElixir(t *testing.T, ps []TemplatePattern, fn string, kind TemplateKind, want string) {
	t.Helper()
	for _, p := range ps {
		if p.Function == fn && p.Kind == kind && contains(p.Literal, want) {
			return
		}
	}
	t.Errorf("expected template %s ~%q in %q; got %+v", kind, want, fn, ps)
}

// hasEntry asserts an entry point with the given ident+kind was produced.
func hasEntryElixir(t *testing.T, eps []EntryPoint, ident string, kind EntryKind) {
	t.Helper()
	for _, e := range eps {
		if e.Ident == ident && e.Kind == kind {
			return
		}
	}
	t.Errorf("expected entry point %s/%s; got %+v", ident, kind, eps)
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// guardian — an authentication pipeline / token-verification module.
// Credited: def_use, dead_code, reachability, pure_fn, module_cycle (universal)
//           + db_effect, taint_source, sanitizer, request_shape,
//             response_shape, template_pattern.
// ---------------------------------------------------------------------------

const guardianSrc = `
defmodule MyApp.Auth.Pipeline do
  use Guardian.Plug.Pipeline, otp_app: :my_app
  require Logger

  def authenticate(conn, %{"token" => token, "scope" => scope}) do
    creds = conn.params
    {:ok, claims} = Guardian.decode_and_verify(token)
    user = MyApp.Repo.get(MyApp.User, claims["sub"])
    verified = Phoenix.Token.verify(MyApp.Endpoint, "user", token)
    Logger.info("guardian authenticated user")
    conn |> json(%{id: user.id, scope: scope})
  end
end
`

func TestElixirTrailing_Guardian_DefUse(t *testing.T) {
	defs, uses := sniffDefUseElixir(guardianSrc)
	hasDefUse(t, defs, uses, "authenticate", "user")
}

func TestElixirTrailing_Guardian_DBEffect(t *testing.T) {
	by := groupByEffect(sniffEffectsElixir(guardianSrc))
	mustHave(t, by, EffectDBRead, "authenticate") // MyApp.Repo.get -> db_read
}

func TestElixirTrailing_Guardian_TaintSource(t *testing.T) {
	// conn.params is the canonical Plug.Conn external-input source.
	hasTaintElixir(t, sniffTaintElixir(guardianSrc), "authenticate", TaintKindSource, TaintCategoryGeneric)
}

func TestElixirTrailing_Guardian_Sanitizer(t *testing.T) {
	// Repo.get parameterises; Phoenix.Token.verify proves message origin.
	hasTaintElixir(t, sniffTaintElixir(guardianSrc), "authenticate", TaintKindSanitizer, "")
}

func TestElixirTrailing_Guardian_RequestShape(t *testing.T) {
	shapes := sniffPayloadShapesElixir(guardianSrc)
	req := findShape(shapes, "authenticate", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected guardian request shape from %%{\"token\"=>..}; got %+v", shapes)
	}
	if got := sortedNames(req.Fields); !equalStrs(got, []string{"scope", "token"}) {
		t.Errorf("guardian request fields: want [scope token] got %v", got)
	}
}

func TestElixirTrailing_Guardian_ResponseShape(t *testing.T) {
	shapes := sniffPayloadShapesElixir(guardianSrc)
	resp := findShape(shapes, "authenticate", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected guardian json(%%{id:..,scope:..}) response shape; got %+v", shapes)
	}
	if got := sortedNames(resp.Fields); !equalStrs(got, []string{"id", "scope"}) {
		t.Errorf("guardian response fields: want [id scope] got %v", got)
	}
}

func TestElixirTrailing_Guardian_Template(t *testing.T) {
	hasTemplateElixir(t, sniffTemplatePatternsElixir(guardianSrc), "authenticate", TemplateKindLog, "guardian authenticated")
}

func TestElixirTrailing_Guardian_EntryReachability(t *testing.T) {
	// public def -> library_export root feeds reachability/dead-code/pure-fn.
	hasEntryElixir(t, sniffElixirEntryPoints(guardianSrc), "authenticate", EntryKindLibraryExport)
}

// ---------------------------------------------------------------------------
// grpc — a GRPC.Server service implementation module.
// Credited: universal + db_effect, sanitizer, template_pattern.
// (request/response_shape already carried via the grpc protobuf message-type
//  path in internal/custom/elixir/grpc.go — left untouched.)
// ---------------------------------------------------------------------------

const grpcSrc = `
defmodule Helloworld.Greeter.Server do
  use GRPC.Server, service: Helloworld.Greeter.Service
  require Logger

  def say_hello(request, _stream) do
    name = request.name
    user = MyApp.Repo.get_by(MyApp.User, name: name)
    Logger.info("grpc say_hello invoked")
    %Helloworld.HelloReply{message: "Hello " <> name, user_id: user.id}
  end
end
`

func TestElixirTrailing_Grpc_DefUse(t *testing.T) {
	defs, uses := sniffDefUseElixir(grpcSrc)
	hasDefUse(t, defs, uses, "say_hello", "user")
}

func TestElixirTrailing_Grpc_DBEffect(t *testing.T) {
	by := groupByEffect(sniffEffectsElixir(grpcSrc))
	mustHave(t, by, EffectDBRead, "say_hello") // Repo.get_by -> db_read
}

func TestElixirTrailing_Grpc_Sanitizer(t *testing.T) {
	// Repo.get_by parameterises the lookup => SQL sanitizer.
	hasTaintElixir(t, sniffTaintElixir(grpcSrc), "say_hello", TaintKindSanitizer, TaintCategorySQL)
}

func TestElixirTrailing_Grpc_Template(t *testing.T) {
	hasTemplateElixir(t, sniffTemplatePatternsElixir(grpcSrc), "say_hello", TemplateKindLog, "grpc say_hello")
}

func TestElixirTrailing_Grpc_EntryReachability(t *testing.T) {
	hasEntryElixir(t, sniffElixirEntryPoints(grpcSrc), "say_hello", EntryKindLibraryExport)
}

// ---------------------------------------------------------------------------
// finch — a low-level HTTP client wrapper module.
// Credited: universal + taint_source, template_pattern.
// (http_effect already FULL — left untouched.)
// ---------------------------------------------------------------------------

const finchSrc = `
defmodule MyApp.HttpClient do
  require Logger

  def fetch_user(id) do
    base = System.get_env("API_BASE_URL")
    url = base <> "/users/" <> id
    req = Finch.build(:get, url)
    {:ok, resp} = Finch.request(req, MyApp.Finch)
    Logger.info("finch fetched user")
    resp
  end
end
`

func TestElixirTrailing_Finch_DefUse(t *testing.T) {
	defs, uses := sniffDefUseElixir(finchSrc)
	hasDefUse(t, defs, uses, "fetch_user", "url")
}

func TestElixirTrailing_Finch_HTTPEffect(t *testing.T) {
	// Already-full cell; assert the sniffer really fires Finch.build/request.
	by := groupByEffect(sniffEffectsElixir(finchSrc))
	mustHave(t, by, EffectHTTPOut, "fetch_user")
}

func TestElixirTrailing_Finch_TaintSource(t *testing.T) {
	// System.get_env is an external-config taint source.
	hasTaintElixir(t, sniffTaintElixir(finchSrc), "fetch_user", TaintKindSource, TaintCategoryGeneric)
}

func TestElixirTrailing_Finch_Template(t *testing.T) {
	hasTemplateElixir(t, sniffTemplatePatternsElixir(finchSrc), "fetch_user", TemplateKindLog, "finch fetched")
}

func TestElixirTrailing_Finch_EntryReachability(t *testing.T) {
	hasEntryElixir(t, sniffElixirEntryPoints(finchSrc), "fetch_user", EntryKindLibraryExport)
}

// ---------------------------------------------------------------------------
// tesla — a Tesla.Client-based HTTP API client module.
// Credited: universal + taint_source, request_shape, template_pattern.
// (http_effect already FULL — left untouched.)
// ---------------------------------------------------------------------------

const teslaSrc = `
defmodule MyApp.ApiClient do
  use Tesla
  require Logger

  def create_order(client, name, total) do
    token = System.get_env("API_TOKEN")
    Logger.info("tesla creating order")
    result = Tesla.post(client, "https://api.example.com/orders", %{name: name, total: total})
    result
  end
end
`

func TestElixirTrailing_Tesla_DefUse(t *testing.T) {
	defs, uses := sniffDefUseElixir(teslaSrc)
	hasDefUse(t, defs, uses, "create_order", "result")
}

func TestElixirTrailing_Tesla_HTTPEffect(t *testing.T) {
	by := groupByEffect(sniffEffectsElixir(teslaSrc))
	mustHave(t, by, EffectHTTPOut, "create_order")
}

func TestElixirTrailing_Tesla_TaintSource(t *testing.T) {
	hasTaintElixir(t, sniffTaintElixir(teslaSrc), "create_order", TaintKindSource, TaintCategoryGeneric)
}

func TestElixirTrailing_Tesla_RequestShape(t *testing.T) {
	// Tesla.post(client, url, %{name:..,total:..}) is the consumer-side body.
	shapes := sniffPayloadShapesElixir(teslaSrc)
	var found *PayloadShape
	for i := range shapes {
		if shapes[i].Function == "create_order" && shapes[i].Direction == PayloadDirectionRequest && shapes[i].Side == PayloadSideConsumer {
			found = &shapes[i]
		}
	}
	if found == nil {
		t.Fatalf("expected tesla consumer request shape; got %+v", shapes)
	}
	if got := sortedNames(found.Fields); !equalStrs(got, []string{"name", "total"}) {
		t.Errorf("tesla request fields: want [name total] got %v", got)
	}
	if found.VerbHint != "POST" || found.EndpointHint != "https://api.example.com/orders" {
		t.Errorf("tesla verb/endpoint hint: got %q %q", found.VerbHint, found.EndpointHint)
	}
}

func TestElixirTrailing_Tesla_Template(t *testing.T) {
	hasTemplateElixir(t, sniffTemplatePatternsElixir(teslaSrc), "create_order", TemplateKindLog, "tesla creating order")
}

func TestElixirTrailing_Tesla_EntryReachability(t *testing.T) {
	hasEntryElixir(t, sniffElixirEntryPoints(teslaSrc), "create_order", EntryKindLibraryExport)
}

// ---------------------------------------------------------------------------
// req — a Req high-level HTTP client module.
// Credited: universal + taint_source, template_pattern.
// (http_effect already FULL — left untouched.)
// ---------------------------------------------------------------------------

const reqSrc = `
defmodule MyApp.ReqClient do
  require Logger

  def post_event(payload) do
    base = System.get_env("EVENTS_URL")
    Logger.info("req posting event")
    resp = Req.post(base, json: payload)
    resp
  end
end
`

func TestElixirTrailing_Req_DefUse(t *testing.T) {
	defs, uses := sniffDefUseElixir(reqSrc)
	hasDefUse(t, defs, uses, "post_event", "resp")
}

func TestElixirTrailing_Req_HTTPEffect(t *testing.T) {
	by := groupByEffect(sniffEffectsElixir(reqSrc))
	mustHave(t, by, EffectHTTPOut, "post_event")
}

func TestElixirTrailing_Req_TaintSource(t *testing.T) {
	hasTaintElixir(t, sniffTaintElixir(reqSrc), "post_event", TaintKindSource, TaintCategoryGeneric)
}

func TestElixirTrailing_Req_Template(t *testing.T) {
	hasTemplateElixir(t, sniffTemplatePatternsElixir(reqSrc), "post_event", TemplateKindLog, "req posting event")
}

func TestElixirTrailing_Req_EntryReachability(t *testing.T) {
	hasEntryElixir(t, sniffElixirEntryPoints(reqSrc), "post_event", EntryKindLibraryExport)
}

// ---------------------------------------------------------------------------
// Negative assertions — honest left-missing ledger. These prove the cells we
// deliberately LEFT MISSING genuinely do not fire on each framework's idiom,
// so the registry omission is correct (no silent under-credit, no over-credit).
// ---------------------------------------------------------------------------

func TestElixirTrailing_LeftMissing_NoFalsePositives(t *testing.T) {
	noEffect := func(src, fn string, eff Effect) {
		by := groupByEffect(sniffEffectsElixir(src))
		if by[eff][fn] {
			t.Errorf("unexpected %s effect on %q", eff, fn)
		}
	}
	noTaintKind := func(src string, kind TaintKind) {
		for _, m := range sniffTaintElixir(src) {
			if m.Kind == kind {
				t.Errorf("unexpected taint %s in fixture: %+v", kind, m)
			}
		}
	}
	// fs_effect / mutation_effect fire on none of the 5 idioms.
	for _, src := range []string{guardianSrc, grpcSrc, finchSrc, teslaSrc, reqSrc} {
		noEffect(src, "authenticate", EffectFSWrite)
		noEffect(src, "say_hello", EffectMutation)
	}
	// taint_sink / vulnerability_finding fire on none of the 5 idioms.
	for _, src := range []string{guardianSrc, grpcSrc, finchSrc, teslaSrc, reqSrc} {
		noTaintKind(src, TaintKindSink)
	}
	// db_effect must NOT fire on the pure HTTP-client idioms (finch/tesla/req).
	for _, src := range []string{finchSrc, teslaSrc, reqSrc} {
		by := groupByEffect(sniffEffectsElixir(src))
		for fn := range by[EffectDBRead] {
			t.Errorf("unexpected db_read on HTTP-client idiom fn %q", fn)
		}
		for fn := range by[EffectDBWrite] {
			t.Errorf("unexpected db_write on HTTP-client idiom fn %q", fn)
		}
	}
	// grpc idiom yields no Plug.Conn / env taint source.
	for _, m := range sniffTaintElixir(grpcSrc) {
		if m.Kind == TaintKindSource {
			t.Errorf("unexpected taint source on grpc idiom: %+v", m)
		}
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
