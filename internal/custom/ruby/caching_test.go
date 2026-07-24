package ruby_test

// caching_test.go — value-asserting tests for the custom_ruby_caching extractor
// (#3692, epic #3628, area #18).

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func runRubyCaching(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_ruby_caching")
	if !ok {
		t.Fatal("custom_ruby_caching not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "app.rb", Language: "ruby", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func findRubyCacheRel(ents []types.EntityRecord, kind, targetRef string) *types.RelationshipRecord {
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == kind && r.ToID == targetRef {
				return r
			}
		}
	}
	return nil
}

func TestRubyCaching_Fetch_ReadThrough(t *testing.T) {
	src := `
class HomeController
  def index
    Rails.cache.fetch("home") { expensive_render }
  end
end
`
	ents := runRubyCaching(t, src)
	r := findRubyCacheRel(ents, "CACHES", "cache:rails:home")
	if r == nil {
		t.Fatalf("expected Rails.cache.fetch(\"home\") to CACHES key home (read-through)")
	}
	if r.Properties.Get("mode") != "read_through" {
		t.Errorf("mode = %q, want read_through", r.Properties.Get("mode"))
	}
}

func TestRubyCaching_Delete_Invalidates(t *testing.T) {
	src := `
class HomeController
  def update
    Rails.cache.delete("home")
  end
end
`
	ents := runRubyCaching(t, src)
	r := findRubyCacheRel(ents, "INVALIDATES", "cache:rails:home")
	if r == nil {
		t.Fatalf("expected Rails.cache.delete(\"home\") to INVALIDATE key home")
	}
	if r.Properties.Get("mode") != "evict" {
		t.Errorf("mode = %q, want evict", r.Properties.Get("mode"))
	}
}

func TestRubyCaching_DeleteMatched_Prefix(t *testing.T) {
	src := `Rails.cache.delete_matched("home/*")`
	ents := runRubyCaching(t, src)
	r := findRubyCacheRel(ents, "INVALIDATES", "cache:rails:home/*")
	if r == nil || r.Properties.Get("mode") != "evict_matched" {
		t.Fatalf("expected evict_matched on home/*, got %+v", r)
	}
}

func TestRubyCaching_Interpolated_Dynamic(t *testing.T) {
	src := `Rails.cache.fetch("user/#{id}") { load }`
	ents := runRubyCaching(t, src)
	r := findRubyCacheRel(ents, "CACHES", "cache:rails:user/*")
	if r == nil {
		t.Fatalf("expected interpolated key to CACHES prefix user/*")
	}
	if r.Properties.Get("dynamic") != "true" {
		t.Errorf("interpolated key should be dynamic")
	}
}

func TestRubyCaching_Write(t *testing.T) {
	src := `Rails.cache.write("config", v)`
	ents := runRubyCaching(t, src)
	r := findRubyCacheRel(ents, "CACHES", "cache:rails:config")
	if r == nil || r.Properties.Get("mode") != "write" {
		t.Fatalf("expected write mode on config, got %+v", r)
	}
}

// Negative: a non-cache method call emits no cache edge.
func TestRubyCaching_PlainCall_NoEdge(t *testing.T) {
	src := `
class HomeController
  def index
    render :index
  end
end
`
	ents := runRubyCaching(t, src)
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "CACHES" || r.Kind == "INVALIDATES" {
				t.Fatalf("plain call should emit no cache edge, got %+v", r)
			}
		}
	}
}
