// graphql_ruby_probe_test.go — VERIFY-FIRST probes for ticket #3948.
//
// These probes prove that the per-LANGUAGE Ruby substrate sniffers fire
// on a graphql-ruby resolver `.rb` file exactly as they do on Sinatra.
// Substrate dispatch in internal/links/def_use_pass.go keys on
// substrate.LanguageForPath(file) → *SnifferFor("ruby"); it is entirely
// framework-agnostic, so a graphql-ruby type/resolver `.rb` file gets the
// identical sniffer set. The probes below are VALUE-ASSERTING: they check
// the concrete db_read/db_write effects, def-use chain, taint sink, and
// payload shape actually produced for a representative graphql-ruby
// QueryType / MutationType resolver body.
//
// They also document the honest NEGATIVE: graphql-ruby resolvers read
// `args` (the GraphQL field arguments), never `request.body` — so the
// request_sink_dataflow capability does NOT fire (and Ruby has no
// dataflow sniffer at all — #3947). That cell stays missing.
//
// This file is a probe harness for the crediting work; it is kept in the
// tree as a regression guard (mirrors how the sibling GraphQL credits —
// jsts Pothos #3903 / python graphene #3911 / go gqlgen #3918 — are
// pinned by language-level substrate tests).
package substrate

import "testing"

// graphqlRubyResolverSrc is a representative graphql-ruby resolver file:
// a QueryType with a `field :user` + `def user` resolver that reads
// `args` and performs an ActiveRecord read, and a MutationType field
// that writes via ActiveRecord. One resolver also builds a raw SQL
// string by interpolating a GraphQL arg (a genuine taint sink), and a
// sibling resolver uses the safe placeholder form (a sanitizer).
const graphqlRubyResolverSrc = `
module Types
  class QueryType < Types::BaseObject
    field :user, UserType, null: true do
      argument :id, ID, required: true
    end

    def user(id:)
      record = User.find_by(id: id)
      @last_user = record
      record
    end

    field :search_users, [UserType], null: false do
      argument :term, String, required: true
    end

    def search_users(term:)
      User.where("name LIKE '%#{term}%'")
    end

    field :safe_users, [UserType], null: false do
      argument :name, String, required: true
    end

    def safe_users(name:)
      User.where("name = ?", name)
    end
  end

  class MutationType < Types::BaseObject
    field :create_user, UserType, null: false do
      argument :name, String, required: true
      argument :email, String, required: true
    end

    def create_user(name:, email:)
      user = User.create!(name: name, email: email)
      user
    end
  end
end
`

// TestProbe_GraphQLRuby_LanguageDispatch confirms a graphql-ruby `.rb`
// file resolves to the "ruby" language and that every substrate sniffer
// family is registered for it — i.e. the dispatch is by file language,
// framework-agnostic. (Sinatra parity proof.)
func TestProbe_GraphQLRuby_LanguageDispatch(t *testing.T) {
	const path = "app/graphql/types/query_type.rb"
	if got := LanguageForPath(path); got != "ruby" {
		t.Fatalf("LanguageForPath(%q) = %q, want \"ruby\"", path, got)
	}
	if DefUseSnifferFor("ruby") == nil {
		t.Error("no def-use sniffer registered for ruby")
	}
	if EffectSnifferFor("ruby") == nil {
		t.Error("no effect sniffer registered for ruby")
	}
	if TaintSnifferFor("ruby") == nil {
		t.Error("no taint sniffer registered for ruby")
	}
	if PayloadShapeSnifferFor("ruby") == nil {
		t.Error("no payload-shape sniffer registered for ruby")
	}
	if TemplatePatternSnifferFor("ruby") == nil {
		t.Error("no template-pattern sniffer registered for ruby")
	}
	if EntryPointSnifferFor("ruby") == nil {
		t.Error("no entry-points sniffer registered for ruby")
	}
}

// TestProbe_GraphQLRuby_Effects asserts the effect sniffer produces both
// db_read (find_by / where) and db_write (create!) on the resolver
// bodies, attributed to the resolver method names. This backs the
// db_effect Substrate cell.
func TestProbe_GraphQLRuby_Effects(t *testing.T) {
	matches := EffectSnifferFor("ruby")(graphqlRubyResolverSrc)
	if len(matches) == 0 {
		t.Fatal("expected effect matches on graphql-ruby resolvers, got none")
	}
	want := map[Effect]bool{EffectDBRead: false, EffectDBWrite: false}
	sawMutation := false
	for _, m := range matches {
		if _, ok := want[m.Effect]; ok {
			want[m.Effect] = true
		}
		if m.Effect == EffectMutation {
			sawMutation = true
		}
	}
	if !want[EffectDBRead] {
		t.Error("expected db_read effect (User.find_by / User.where) on a graphql-ruby resolver")
	}
	if !want[EffectDBWrite] {
		t.Error("expected db_write effect (User.create!) on a graphql-ruby mutation resolver")
	}
	// @last_user = record is an instance-variable mutation inside def user.
	if !sawMutation {
		t.Error("expected mutation effect (@last_user assignment) on a graphql-ruby resolver")
	}
	// Function attribution must bind db_write to the create_user resolver.
	boundWrite := false
	for _, m := range matches {
		if m.Effect == EffectDBWrite && m.Function == "create_user" {
			boundWrite = true
		}
	}
	if !boundWrite {
		t.Error("db_write not attributed to the create_user resolver method")
	}
}

// TestProbe_GraphQLRuby_DefUse asserts a def-use chain is produced for a
// local inside a resolver body (record def → record use). Backs the
// def_use_chain_extraction Substrate cell.
func TestProbe_GraphQLRuby_DefUse(t *testing.T) {
	defs, uses := DefUseSnifferFor("ruby")(graphqlRubyResolverSrc)
	if len(defs) == 0 || len(uses) == 0 {
		t.Fatalf("expected defs and uses on graphql-ruby resolvers, got defs=%d uses=%d", len(defs), len(uses))
	}
	// `record = User.find_by(...)` defines `record` in def user; the next
	// line returns `record` (a use) in the same function.
	defOK := false
	for _, d := range defs {
		if d.Var == "record" && d.Function == "user" {
			defOK = true
		}
	}
	useOK := false
	for _, u := range uses {
		if u.Var == "record" && u.Function == "user" {
			useOK = true
		}
	}
	if !defOK {
		t.Error("expected a def of `record` inside the `user` resolver")
	}
	if !useOK {
		t.Error("expected a use of `record` inside the `user` resolver")
	}
}

// TestProbe_GraphQLRuby_Taint asserts the taint sniffer flags the SQL
// string-interpolation sink (where("... #{term} ...")) and recognises the
// placeholder form as a sanitizer. Backs taint_sink_detection /
// sanitizer_recognition / vulnerability_finding Substrate cells.
func TestProbe_GraphQLRuby_Taint(t *testing.T) {
	matches := TaintSnifferFor("ruby")(graphqlRubyResolverSrc)
	if len(matches) == 0 {
		t.Fatal("expected taint matches on graphql-ruby resolvers, got none")
	}
	sawSink := false
	sawSanitizer := false
	for _, m := range matches {
		if m.Kind == TaintKindSink {
			sawSink = true
		}
		if m.Kind == TaintKindSanitizer {
			sawSanitizer = true
		}
	}
	if !sawSink {
		t.Error("expected a taint SINK (SQL interpolation where(\"...#{term}...\")) in a graphql-ruby resolver")
	}
	if !sawSanitizer {
		t.Error("expected a taint SANITIZER (placeholder where(\"name = ?\", name)) in a graphql-ruby resolver")
	}
}

// gqlRubyIOResolverSrc is a graphql-ruby resolver that performs outbound
// HTTP, a filesystem read, and a JSON.parse of a non-literal — proving
// http_effect / fs_effect / taint_source_detection fire when a resolver
// body genuinely does that I/O (the Ruby sniffers are framework-agnostic).
const gqlRubyIOResolverSrc = `
module Types
  class QueryType < Types::BaseObject
    field :weather, String, null: true do
      argument :city, String, required: true
    end

    def weather(city:)
      resp = Net::HTTP.get(URI("https://api.example.com/w?c=#{city}"))
      cached = File.read("/var/cache/weather.json")
      JSON.parse(resp)
    end
  end
end
`

// TestProbe_GraphQLRuby_IOEffects asserts http_effect and fs_effect fire
// when a graphql-ruby resolver calls an HTTP client / reads a file, and
// that JSON.parse of a non-literal registers a taint SOURCE. Backs the
// http_effect, fs_effect, and taint_source_detection Substrate cells.
func TestProbe_GraphQLRuby_IOEffects(t *testing.T) {
	effs := EffectSnifferFor("ruby")(gqlRubyIOResolverSrc)
	sawHTTP, sawFS := false, false
	for _, m := range effs {
		switch m.Effect {
		case EffectHTTPOut:
			sawHTTP = true
		case EffectFSRead, EffectFSWrite:
			sawFS = true
		}
	}
	if !sawHTTP {
		t.Error("expected http_out effect (Net::HTTP.get) in a graphql-ruby resolver")
	}
	if !sawFS {
		t.Error("expected fs_read effect (File.read) in a graphql-ruby resolver")
	}
	srcCount := 0
	for _, m := range TaintSnifferFor("ruby")(gqlRubyIOResolverSrc) {
		if m.Kind == TaintKindSource {
			srcCount++
		}
	}
	if srcCount == 0 {
		t.Error("expected a taint SOURCE (JSON.parse of non-literal) in a graphql-ruby resolver")
	}
}

// TestProbe_GraphQLRuby_NonFiringStaySilent documents the cells that
// honestly stay missing for graphql-ruby's schema-first convention. The
// Ruby payload-shape / template-pattern / entry-point sniffers key on
// `params[:x]` request access, route-handler entry points, and render /
// log template literals — none of which a convention-driven graphql-ruby
// type/resolver `.rb` file emits (it returns ActiveRecord objects and
// takes keyword args). So request/response shape, schema-drift,
// reachability/dead-code, and template_pattern_catalog stay missing,
// exactly like the sibling gqlgen (#3918) / Pothos (#3903) credits.
func TestProbe_GraphQLRuby_NonFiringStaySilent(t *testing.T) {
	if got := len(PayloadShapeSnifferFor("ruby")(graphqlRubyResolverSrc)); got != 0 {
		t.Errorf("payload shapes on a graphql-ruby resolver = %d, want 0 (re-evaluate request/response_shape cells)", got)
	}
	if got := len(TemplatePatternSnifferFor("ruby")(graphqlRubyResolverSrc)); got != 0 {
		t.Errorf("template patterns on a graphql-ruby resolver = %d, want 0 (re-evaluate template_pattern_catalog)", got)
	}
	if got := len(EntryPointSnifferFor("ruby")(graphqlRubyResolverSrc)); got != 0 {
		t.Errorf("entry points on a graphql-ruby resolver = %d, want 0 (re-evaluate reachability/dead_code cells)", got)
	}
}

// TestProbe_GraphQLRuby_RequestSinkDataflowDoesNotFire is the documented
// HONEST NEGATIVE. graphql-ruby resolvers receive validated GraphQL field
// arguments (`args` / keyword args), never request.body — so there is no
// request→sink dataflow signal here, and Ruby ships no dataflow sniffer
// at all (#3947). The request_sink_dataflow Substrate cell stays missing.
func TestProbe_GraphQLRuby_RequestSinkDataflowDoesNotFire(t *testing.T) {
	if DataFlowSnifferFor("ruby") != nil {
		t.Fatal("unexpected: a Ruby dataflow sniffer now exists — re-evaluate the request_sink_dataflow cell (#3947)")
	}
}
