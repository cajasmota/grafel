package ruby

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #4398 — RSpec TESTS→subject affinity edge + Minitest test-suite collapse
// with a TESTS→subject edge. Honest: no spurious edge when no subject resolves.

// findTestsEdge returns the first TESTS edge with the given match_source on any
// entity of the given subtype, plus its ToID.
func findTestsEdgeTarget(ents []types.EntityRecord, subtype string) (string, bool) {
	for _, e := range ents {
		if subtype != "" && e.Subtype != subtype {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindTests) &&
				r.Properties["match_source"] == "spec_subject_affinity" {
				return r.ToID, true
			}
		}
	}
	return "", false
}

// RSpec `describe User` in user_spec.rb → TESTS→User (via described_class).
func TestRSpecSubjectAffinity_DescribedClass4398(t *testing.T) {
	src := `require 'rails_helper'

RSpec.describe User do
  it 'is valid' do
    u = User.new
    u.activate
  end
end
`
	ents := extractRuby(t, "spec/models/user_spec.rb", src)
	got, ok := findTestsEdgeTarget(ents, "test_scope")
	if !ok {
		t.Fatal("no RSpec TESTS→subject edge emitted for describe User")
	}
	if got != "Class:User" {
		t.Fatalf("RSpec subject edge ToID=%q, want Class:User", got)
	}
}

// RSpec with a string-label describe but a `_spec` filename → stem→class
// fallback (`order_service_spec.rb` → OrderService).
func TestRSpecSubjectAffinity_StemFallback4398(t *testing.T) {
	src := `RSpec.describe 'OrderService behaviour' do
  it 'places an order' do
    svc = OrderService.new
    svc.place_order
  end
end
`
	ents := extractRuby(t, "spec/services/order_service_spec.rb", src)
	got, ok := findTestsEdgeTarget(ents, "test_scope")
	if !ok {
		t.Fatal("no RSpec TESTS→subject edge via stem fallback")
	}
	if got != "Class:OrderService" {
		t.Fatalf("RSpec stem subject edge ToID=%q, want Class:OrderService", got)
	}
}

// Minitest `class UserTest < Minitest::Test` → a test_suite + TESTS→User.
func TestMinitestSuiteCollapse_Subject4398(t *testing.T) {
	src := `require 'test_helper'

class UserTest < Minitest::Test
  def setup
    @user = User.new
  end

  def test_activation
    @user.activate
    assert @user.active?
  end

  def test_name
    assert_equal 'a', User.new('a').name
  end
end
`
	ents := extractRuby(t, "test/models/user_test.rb", src)

	var suite *types.EntityRecord
	for i := range ents {
		if ents[i].Subtype == "test_suite" && ents[i].Properties["framework"] == "minitest" {
			suite = &ents[i]
			break
		}
	}
	if suite == nil {
		t.Fatal("no Minitest test_suite emitted")
	}
	if suite.Properties["test_count"] != "2" {
		t.Fatalf("Minitest test_count=%q, want 2", suite.Properties["test_count"])
	}
	got, ok := findTestsEdgeTarget(ents, "test_suite")
	if !ok {
		t.Fatal("no Minitest TESTS→subject edge")
	}
	if got != "Class:User" {
		t.Fatalf("Minitest subject edge ToID=%q, want Class:User", got)
	}
}

// A Minitest case under ActiveSupport::TestCase also collapses + links.
func TestMinitestSuiteCollapse_ActiveSupport4398(t *testing.T) {
	src := `class OrderTest < ActiveSupport::TestCase
  def test_total
    assert_equal 0, Order.new.total
  end
end
`
	ents := extractRuby(t, "test/models/order_test.rb", src)
	got, ok := findTestsEdgeTarget(ents, "test_suite")
	if !ok {
		t.Fatal("no Minitest (ActiveSupport) TESTS→subject edge")
	}
	if got != "Class:Order" {
		t.Fatalf("subject edge ToID=%q, want Class:Order", got)
	}
}

// Honest exclusion: an RSpec spec with NO resolvable subject (string-label
// describe AND a non-`_spec` path) emits no subject edge.
func TestRSpecSubjectAffinity_NoSubjectNoEdge4398(t *testing.T) {
	src := `RSpec.describe 'some behaviour' do
  it 'works' do
    Helper.do_thing
  end
end
`
	// Path under /spec/ (so it IS a spec file) but with no `_spec` stem token to
	// camelize and no described_class → no subject edge.
	ents := extractRuby(t, "spec/integration/behaviour.rb", src)
	if got, ok := findTestsEdgeTarget(ents, "test_scope"); ok {
		t.Fatalf("expected no subject edge for subject-less spec, got %q", got)
	}
}

// A non-Minitest plain class in a test file does NOT become a test_suite.
func TestMinitest_NonTestCaseClassIgnored4398(t *testing.T) {
	src := `class UserFactory
  def build
    User.new
  end
end
`
	ents := extractRuby(t, "test/support/user_factory.rb", src)
	for _, e := range ents {
		if e.Subtype == "test_suite" {
			t.Fatalf("plain class wrongly collapsed to a test_suite: %+v", e.Name)
		}
	}
}
