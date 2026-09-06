package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// #6917: sinatra's `^\s*`-anchored rule patterns were compiled with plain
// regexp.Compile. Without `(?m)`, Go's `^` means START OF TEXT, not start of
// line, so every one of them could only fire when its construct was the FIRST
// thing in the file. On the idiomatic modular-style app — routes indented
// inside `class MyApp < Sinatra::Base` — that yielded ZERO Route entities.
//
// These tests drive the REAL embedded sinatra.yaml through LoadAllRules() and
// Detector.Detect(), not a hand-written in-memory rule, because the defect IS
// the shipped pattern text. A hermetic fixture rule would grade a copy and
// leave the shipped one unobserved.
//
// Both directions are asserted, because only one of them is a consequence of
// the fix and the other is a consequence of the fix being TOO BROAD (#6902):
//
//   - Recall  (TestIssue6917_ModularSinatraAppYieldsRoutes): the constructs an
//     indented, non-first-line sinatra app declares must now be found.
//   - Forbidden (TestIssue6917_MultilinePatternsDoNotOverFire): `(?m)` turns
//     `^` into a LINE anchor, which is exactly what makes over-firing possible
//     for the first time. A `get` inside a comment, a `get` inside a heredoc
//     string, and a `configure`/`before`/`after`/`error` invoked as a method on
//     another receiver must still not be mistaken for sinatra constructs.
//
// modularSinatraApp is deliberately NOT a toy whose first line is a route: the
// file opens with requires and a class declaration, which is precisely the
// shape the old start-of-text semantics could not see.
const modularSinatraApp = `require 'sinatra/base'
require 'json'

class MyApp < Sinatra::Base
  configure :production do
    set :show_exceptions, false
  end

  helpers do
    def current_user
      env['user']
    end
  end

  before '/admin/*' do
    halt 401 unless current_user
  end

  get '/invoices' do
    json_helper
  end

  post '/invoices' do
    status 201
  end

  delete '/invoices/:id' do
    status 204
  end

  after do
    response.headers['X-App'] = 'MyApp'
  end

  error 404 do
    'not found'
  end
end
`

// sinatraConfigRu is the second half of the defect's driven evidence: `use
// Rack::Session::Cookie` at byte 0 fired, the same line after a require did
// not.
const sinatraConfigRu = `require './app'

use Rack::Session::Cookie, secret: ENV['SECRET']
run MyApp
`

func detect6917Ruby(t *testing.T, path, src string) *DetectResult {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "ruby",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return res
}

func has6917Entity(res *DetectResult, kind, name string) bool {
	for _, e := range res.Entities {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

func entity6917Names(res *DetectResult, kind string) []string {
	var out []string
	for _, e := range res.Entities {
		if e.Kind == kind {
			out = append(out, e.Name)
		}
	}
	return out
}

// TestIssue6917_ModularSinatraAppYieldsRoutes is the recall arm. Before the
// fix every assertion below failed with ZERO Route entities.
func TestIssue6917_ModularSinatraAppYieldsRoutes(t *testing.T) {
	res := detect6917Ruby(t, "app/my_app.rb", modularSinatraApp)

	for _, want := range []string{"/invoices", "/invoices/:id"} {
		if !has6917Entity(res, "Route", want) {
			t.Errorf("Route %q not extracted; Routes found: %v", want, entity6917Names(res, "Route"))
		}
	}
	// FINDING, pinned rather than papered over: the app registers THREE routes
	// (get/post on /invoices, delete on /invoices/:id) and the graph carries
	// TWO entities. sinatra.yaml names a Route by `name_group: 2` — the PATH
	// alone — and detector.go:432 dedupes on `entityType:name:file`, so
	// `get '/invoices'` and `post '/invoices'` collapse into one entity and the
	// verb is lost. That is a modelling limitation of the rule, independent of
	// #6917's anchor defect, and this exact count is what tells a later reader
	// the collapse is still happening.
	if got := len(entity6917Names(res, "Route")); got != 2 {
		t.Errorf("expected 2 Route entities (3 registrations, path-only names collapse get+post on /invoices), got %d: %v",
			got, entity6917Names(res, "Route"))
	}

	// The class declaration was never `^`-anchored, so it was the ONE construct
	// that already worked. Asserting it holds the fix to changing only what the
	// anchors gated.
	if !has6917Entity(res, "Controller", "MyApp") {
		t.Errorf("Controller MyApp missing; Controllers: %v", entity6917Names(res, "Controller"))
	}

	if len(entity6917Names(res, "Middleware")) == 0 {
		t.Errorf("expected before/after/error filters as Middleware, got none")
	}
	if !has6917Entity(res, "Config", "production") {
		t.Errorf("configure :production not extracted as Config; Configs: %v",
			entity6917Names(res, "Config"))
	}
	if len(entity6917Names(res, "Service")) == 0 {
		t.Errorf("expected the `helpers do` block as a Service, got none")
	}
}

// TestIssue6917_UseDirectiveAfterRequire pins the exact driven evidence from
// the issue: the `use` relationship rule fired at byte 0 and nowhere else.
func TestIssue6917_UseDirectiveAfterRequire(t *testing.T) {
	res := detect6917Ruby(t, "config.ru", sinatraConfigRu)

	var found bool
	for _, r := range res.Relationships {
		if r.Kind == "REGISTERED_ON" && strings.Contains(r.FromID+r.ToID, "Rack::Session::Cookie") {
			found = true
		}
	}
	if !found {
		t.Errorf("`use Rack::Session::Cookie` after a require produced no REGISTERED_ON edge; got %d relationships: %v",
			len(res.Relationships), rel6917Kinds(res.Relationships))
	}
}

func rel6917Kinds(rs []types.RelationshipRecord) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Kind+":"+r.FromID+"->"+r.ToID)
	}
	return out
}

// sinatraDecoys contains ONLY things that must not be extracted. Every line
// that could be mistaken for a sinatra construct is in a position `(?m)^\s*`
// newly reaches — an indented line — so a too-broad pattern is visible here and
// nowhere else. There is deliberately no real route in this file: a decoy file
// that also contains a genuine construct cannot distinguish "the decoy was
// rejected" from "the real one was accepted".
const sinatraDecoys = `require 'sinatra/base'

class Decoys
  # get '/commented-out' do
  # before '/commented' do
  # error 500 do

  def docs
    <<~USAGE
      get '/from-a-heredoc' do
      configure :from_a_heredoc do
      helpers do
    USAGE
  end

  def run(server)
    server.helpers Whatever
    server.helpers do
    server.configure :production do
      server.before do
        server.after do
      end
    end
  end
end
`

// TestIssue6917_MultilinePatternsDoNotOverFire is the forbidden arm required by
// #6902: a recall assertion alone cannot see a pattern that started matching
// things it should not, and `(?m)` is precisely the change that lets `^\s*`
// reach indented text for the first time.
func TestIssue6917_MultilinePatternsDoNotOverFire(t *testing.T) {
	res := detect6917Ruby(t, "app/decoys.rb", sinatraDecoys)

	forbidden := []struct{ kind, name string }{
		{"Route", "/commented-out"},
		// The `helpers <Module>` include rule used to capture `(\w+)`, so it
		// matched the `do` of `helpers do` and minted a Service named "do".
		// Unreachable while the anchor defect kept both patterns dead; the
		// capture is now `([A-Z]\w*(?:::\w+)*)`, a Ruby constant.
		{"Service", "do"},
		// `server.helpers Whatever` is a call on another receiver. The capture
		// would accept `Whatever`, so ONLY the line anchor keeps it out — which
		// is what makes this row grade the anchor rather than the capture.
		{"Service", "Whatever"},
	}
	for _, f := range forbidden {
		if has6917Entity(res, f.kind, f.name) {
			t.Errorf("over-fired: %s %q extracted from a comment decoy", f.kind, f.name)
		}
	}

	// KNOWN AND NEWLY REACHABLE, asserted as current behaviour rather than
	// hidden: `(?m)^\s*` reaches every line, and a regex has no way to know a
	// line lives inside a `<<~HEREDOC` body. Route-shaped text in a usage
	// string IS extracted. Before this change the same text could only be
	// matched when it opened the file, so the fix does widen this case.
	//
	// It is pinned in the POSITIVE direction on purpose: writing it as a
	// forbidden row would make the test fail today and say nothing; written
	// this way, whoever teaches the detector about heredocs is told by a red
	// test to come here and flip these two assertions into the forbidden list
	// above. Comment decoys, by contrast, are genuinely excluded — `#` opens
	// the line, so `^\s*get` cannot reach past it.
	for _, known := range []struct{ kind, name string }{
		{"Route", "/from-a-heredoc"},
		{"Config", "from_a_heredoc"},
	} {
		if !has6917Entity(res, known.kind, known.name) {
			t.Errorf("heredoc over-firing is no longer happening for %s %q — good news; "+
				"move this case into the forbidden list above", known.kind, known.name)
		}
	}

	// `server.configure :production do` / `server.before do` / `server.after do`
	// are method calls on another receiver, not sinatra DSL invocations. `^\s*`
	// requires the keyword to open the line, so the receiver prefix must keep
	// them out.
	if has6917Entity(res, "Config", "production") {
		t.Errorf("over-fired: `server.configure :production do` extracted as Config")
	}
	for _, name := range entity6917Names(res, "Middleware") {
		t.Errorf("over-fired: Middleware %q extracted from a decoy file whose only "+
			"before/after text is `server.before do` on another receiver", name)
	}
	for _, name := range entity6917Names(res, "Route") {
		if name == "/from-a-heredoc" {
			continue // the pinned heredoc limitation above
		}
		t.Errorf("over-fired: Route %q extracted from a decoy file with no routes", name)
	}
}
