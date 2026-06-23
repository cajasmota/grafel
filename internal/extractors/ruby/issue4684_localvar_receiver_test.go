package ruby

import (
	"context"
	"testing"

	tsruby "github.com/smacker/go-tree-sitter/ruby"

	"github.com/cajasmota/grafel/internal/extractor"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #4684 (Ruby slice of epic #4615 / #4672): local-variable receiver typing
// + an RSpec test-scope owner so test→CALLS→handler coverage credits the
// endpoint. Mirrors the TS/JS (#4680/#4671), Python (#4681) and Go (#4683)
// slices.
//
// RSpec example logic lives in anonymous `it ... do … end` blocks (not named
// methods), so before this slice the base extractor emitted ZERO CALLS edges for
// a spec file — the handler the spec exercises stayed uncovered. The new
// emitRubyTestScopeOwner mines the example/hook blocks, types local-variable
// receivers from their constructor bindings (`c = ProposalsController.new`,
// `instance = described_class.new`), and emits one SCOPE.Operation owner per
// spec file carrying the resolved `Class.method` CALLS edges.

func parseRubyFixture(t *testing.T, path, src string) extractor.FileInput {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsruby.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return extractor.FileInput{Path: path, Language: "ruby", Content: []byte(src), TSTree: tree}
}

func extractRuby(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ents, err := (&Extractor{}).Extract(context.Background(), parseRubyFixture(t, path, src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

// testScopeRels returns the CALLS targets owned by the per-file RSpec test-scope
// owner entity (Subtype="test_scope"), or nil when none was emitted.
func testScopeCalls(ents []types.EntityRecord) map[string]bool {
	for _, e := range ents {
		if e.Subtype == "test_scope" {
			out := map[string]bool{}
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" {
					out[r.ToID] = true
				}
			}
			return out
		}
	}
	return nil
}

// A — explicit constructor local + described_class.new: `c.get_counts` /
// `instance.get_counts` resolve to ProposalsController.get_counts, owned by the
// it-block test scope.
func TestRubyTestScope_LocalReceiverTyping(t *testing.T) {
	const src = `require 'rails_helper'

RSpec.describe ProposalsController, type: :controller do
  it 'returns counts via explicit construction' do
    c = ProposalsController.new
    c.get_counts('2025')
    expect(response).to have_http_status(:ok)
  end

  it 'returns counts via described_class' do
    instance = described_class.new
    instance.get_counts('2025')
  end
end
`
	calls := testScopeCalls(extractRuby(t, "spec/controllers/proposals_controller_spec.rb", src))
	if calls == nil {
		t.Fatal("no RSpec test-scope owner emitted (expected CALLS-bearing scope)")
	}
	if !calls["ProposalsController.get_counts"] {
		t.Errorf("missing typed CALLS to ProposalsController.get_counts; got %v", calls)
	}
	// The DSL matchers must NOT leak as CALLS edges.
	for k := range calls {
		if k == "expect" || k == "have_http_status" || k == "get_counts" {
			t.Errorf("unexpected bare/matcher CALLS edge leaked: %q", k)
		}
	}
}

// Direct PascalCase-constant call (`ProposalsController.create`) types without a
// local binding.
func TestRubyTestScope_ConstantReceiver(t *testing.T) {
	const src = `RSpec.describe ProposalsController do
  it 'creates' do
    ProposalsController.create(params)
  end
end
`
	calls := testScopeCalls(extractRuby(t, "spec/controllers/proposals_controller_spec.rb", src))
	if !calls["ProposalsController.create"] {
		t.Errorf("missing typed CALLS to ProposalsController.create; got %v", calls)
	}
}

// Negative — a factory-helper local (`x = make_thing()`) stays untyped, so its
// method call yields no `Class.method` edge; a shape-only assertion spec emits
// no test-scope owner at all.
func TestRubyTestScope_HonestExclusion(t *testing.T) {
	const factorySrc = `RSpec.describe Thing do
  it 'uses a factory helper' do
    x = make_thing
    x.do_work
  end
end
`
	calls := testScopeCalls(extractRuby(t, "spec/models/thing_spec.rb", factorySrc))
	for k := range calls {
		if k == "Thing.do_work" || k == "make_thing.do_work" {
			t.Errorf("factory-helper receiver must stay untyped, got edge %q", k)
		}
	}

	const shapeOnlySrc = `RSpec.describe 'JSON shape' do
  it 'has the right keys' do
    expect(payload).to include(:id, :name)
    expect(payload[:id]).to be_a(Integer)
  end
end
`
	ents := extractRuby(t, "spec/requests/shape_spec.rb", shapeOnlySrc)
	if scope := testScopeCalls(ents); scope != nil {
		t.Errorf("shape-only spec must emit no test-scope owner, got CALLS %v", scope)
	}
}

// Non-spec Ruby files must never get a test-scope owner.
func TestRubyTestScope_NonSpecFileSkipped(t *testing.T) {
	const src = `class ProposalsController
  def get_counts(year)
    Proposal.where(year: year).count
  end
end
`
	ents := extractRuby(t, "app/controllers/proposals_controller.rb", src)
	for _, e := range ents {
		if e.Subtype == "test_scope" {
			t.Errorf("non-spec file emitted a test-scope owner: %q", e.Name)
		}
	}
}
