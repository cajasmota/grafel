// imports_test.go — coverage for the IMPORTS ToID resolveImportToIDs
// pass (analog of #642/#650/#670 for Go).

package golang

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// findGoImportEdge returns the IMPORTS edge whose owning entity Name
// matches the supplied module path, or nil when no such edge exists.
func findGoImportEdge(ents []types.EntityRecord, modulePath string) *types.RelationshipRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind != "SCOPE.Component" || e.Name != modulePath {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind == "IMPORTS" {
				return r
			}
		}
	}
	return nil
}

// Known external stdlib root: `import "fmt"` → ToID="ext:fmt"; the
// `net/http` shape collapses onto the `net` allowlist entry.
func TestGoImportsRewriteStdlib(t *testing.T) {
	src := `package demo

import (
	"fmt"
	"net/http"
	"encoding/json"
)

func _refs() {
	_ = fmt.Sprint
	_ = http.StatusOK
	_ = json.Marshal
}
`
	ex := &GoExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.go",
		Language: "go",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	r := findGoImportEdge(ents, "fmt")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for fmt")
	}
	if r.ToID != "ext:fmt" {
		t.Fatalf("fmt import ToID = %q, want ext:fmt", r.ToID)
	}
	r2 := findGoImportEdge(ents, "net/http")
	if r2 == nil {
		t.Fatalf("missing IMPORTS edge for net/http")
	}
	if r2.ToID != "ext:net" {
		t.Fatalf("net/http import ToID = %q, want ext:net", r2.ToID)
	}
	r3 := findGoImportEdge(ents, "encoding/json")
	if r3 == nil {
		t.Fatalf("missing IMPORTS edge for encoding/json")
	}
	if r3.ToID != "ext:encoding" {
		t.Fatalf("encoding/json import ToID = %q, want ext:encoding", r3.ToID)
	}
}

// github.com 3-segment matching: `github.com/go-chi/chi/v5/middleware`
// must rewrite to `ext:github.com/go-chi/chi` (the 3-segment allowlist
// entry), NOT to a shorter prefix.
func TestGoImportsRewriteGithubThreeSegment(t *testing.T) {
	src := `package demo

import (
	chi "github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sirupsen/logrus"
)

var _ = chi.NewRouter
var _ = middleware.Logger
var _ = logrus.Info
`
	ex := &GoExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.go",
		Language: "go",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	r := findGoImportEdge(ents, "github.com/go-chi/chi/v5")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for github.com/go-chi/chi/v5")
	}
	if r.ToID != "ext:github.com/go-chi/chi" {
		t.Fatalf("chi import ToID = %q, want ext:github.com/go-chi/chi", r.ToID)
	}
	r2 := findGoImportEdge(ents, "github.com/go-chi/chi/v5/middleware")
	if r2 == nil {
		t.Fatalf("missing IMPORTS edge for chi middleware")
	}
	if r2.ToID != "ext:github.com/go-chi/chi" {
		t.Fatalf("chi/middleware import ToID = %q, want ext:github.com/go-chi/chi", r2.ToID)
	}
	r3 := findGoImportEdge(ents, "github.com/sirupsen/logrus")
	if r3 == nil {
		t.Fatalf("missing IMPORTS edge for logrus")
	}
	if r3.ToID != "ext:github.com/sirupsen/logrus" {
		t.Fatalf("logrus import ToID = %q, want ext:github.com/sirupsen/logrus", r3.ToID)
	}
}

// Unknown / in-tree imports are left untouched: the resolver's
// downstream cross-file path needs the original full module path to
// bind in-tree modules.
func TestGoImportsLeavesUnknownAlone(t *testing.T) {
	src := `package demo

import (
	"github.com/myorg/myrepo/internal/util"
	"example.com/some-org/some-pkg"
)

var _ = util.Foo
var _ = somepkg.Bar
`
	ex := &GoExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.go",
		Language: "go",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	r := findGoImportEdge(ents, "github.com/myorg/myrepo/internal/util")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for in-tree util")
	}
	if strings.HasPrefix(r.ToID, "ext:") {
		t.Fatalf("in-tree util import ToID = %q, must not be ext: form", r.ToID)
	}
	r2 := findGoImportEdge(ents, "example.com/some-org/some-pkg")
	if r2 == nil {
		t.Fatalf("missing IMPORTS edge for example.com pkg")
	}
	if strings.HasPrefix(r2.ToID, "ext:") {
		t.Fatalf("example.com import ToID = %q, must not be ext: form", r2.ToID)
	}
}
