package ruby

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// Issue #4371 (Ruby extractor side): the RSpec extractor must capture every
// Rails request/integration-spec route-by-string call and stamp the
// `VERB route` pairs onto a one-per-file test_suite's e2e_route_calls property.
// Named-route helpers and controller-spec symbol actions are skipped.

const railsRequestSpecSrc4371 = `require 'rails_helper'

RSpec.describe 'Inspections API', type: :request do
  let(:inspection) { create(:inspection) }

  it 'lists inspections' do
    get '/api/v1/inspections'
    expect(response).to have_http_status(:ok)
  end

  it 'shows one' do
    get "/api/v1/inspections/#{inspection.id}"
    expect(response).to have_http_status(:ok)
  end

  it 'creates an item' do
    post '/api/v1/inspections/123/items', params: { name: 'x' }
    expect(response).to have_http_status(:created)
  end

  it 'updates' do
    patch "/api/v1/inspections/1", params: { name: 'y' }
    expect(response).to have_http_status(:ok)
  end

  it 'deletes' do
    delete '/api/v1/inspections/9'
    expect(response).to have_http_status(:no_content)
  end

  it 'uses a named route helper (skipped)' do
    get inspections_path
    expect(response).to have_http_status(:ok)
  end
end
`

const railsControllerSpecSrc4371 = `require 'rails_helper'

RSpec.describe InspectionsController, type: :controller do
  it 'shows' do
    get :show, params: { id: 1 }
    expect(response).to have_http_status(:ok)
  end
end
`

func TestRSpecRequestSpecE2E_CapturesRoutes(t *testing.T) {
	ex := &rspecExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "spec/requests/inspections_spec.rb",
		Language: "ruby",
		Content:  []byte(railsRequestSpecSrc4371),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	var suite *struct {
		calls string
	}
	for _, e := range ents {
		if e.Subtype == "test_suite" {
			suite = &struct{ calls string }{calls: e.Properties["e2e_route_calls"]}
		}
	}
	if suite == nil {
		t.Fatal("RSpec extractor emitted no request-spec test_suite")
	}
	got := map[string]bool{}
	for _, l := range strings.Split(suite.calls, "\n") {
		got[l] = true
	}
	want := []string{
		"GET /api/v1/inspections",
		"GET /api/v1/inspections/#{inspection.id}",
		"POST /api/v1/inspections/123/items",
		"PATCH /api/v1/inspections/1",
		"DELETE /api/v1/inspections/9",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing route call %q (got: %v)", w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("captured %d routes, want %d: %v", len(got), len(want), got)
	}
	// Named-route helper (get inspections_path) must NOT be captured.
	for l := range got {
		if strings.Contains(l, "inspections_path") {
			t.Errorf("named-route helper leaked into capture: %q", l)
		}
	}
}

// Controller specs use a symbol action (get :show) — no leading-slash path, so
// no suite / route is emitted.
func TestRSpecControllerSpecE2E_NoRoutes(t *testing.T) {
	ex := &rspecExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "spec/controllers/inspections_controller_spec.rb",
		Language: "ruby",
		Content:  []byte(railsControllerSpecSrc4371),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, e := range ents {
		if e.Subtype == "test_suite" {
			t.Errorf("controller spec must not emit a route test_suite, got %q with calls=%q",
				e.Name, e.Properties["e2e_route_calls"])
		}
	}
}
