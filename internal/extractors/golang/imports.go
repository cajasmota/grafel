// imports.go — IMPORTS to_id resolution for the Go extractor.
//
// Analog of #642 (JS/TS), #650 (Python), and #670 (Java) for Go. The
// Go extractor emits IMPORTS edges whose ToID is the full module path
// exactly as it appears in the import statement (`"fmt"`,
// `"github.com/go-chi/chi/v5"`, `"github.com/cajasmota/archigraph/internal/types"`).
// None of those carry the `ext:<package>` prefix the resolver's
// external-disposition gate (refs.go: stubPrefixExternal) keys on, so
// every imported leaf from a known external Go package — stdlib
// (`fmt`, `context`, `time`, ...) and well-known third-party
// (`github.com/go-chi/chi`, `github.com/sirupsen/logrus`,
// `go.uber.org/zap`, ...) — had to round-trip through the bare-name
// resolver, miss, and fall back to ExternalUnknown / bug-extractor.
//
// The fix mirrors #642/#650/#670: AFTER extractImportEntities has
// emitted the IMPORTS edges, walk every edge and rewrite the ToID for
// edges whose import path's longest matching prefix points at a known
// external Go package:
//
//	import "fmt"
//	    → ToID = "ext:fmt"
//	import "github.com/go-chi/chi/v5"
//	    → ToID = "ext:github.com/go-chi/chi"
//	import "github.com/go-chi/chi/v5/middleware"
//	    → ToID = "ext:github.com/go-chi/chi"
//	import "github.com/cajasmota/archigraph/internal/types"
//	    → untouched (not on the external allowlist; in-tree path)
//
// Go's module-path convention requires special handling versus Python
// (single-segment package roots) and Java (dotted package roots):
//
//   * Stdlib packages are single- or multi-segment paths with NO
//     domain prefix: `fmt`, `net/http`, `encoding/json`. We allowlist
//     the FIRST segment (`fmt`, `net`, `encoding`).
//   * Third-party packages use the canonical Go-module form
//     `<domain>/<owner>/<repo>[/...]`. We allowlist the FIRST 2-3
//     segments so `github.com/go-chi/chi/v5/middleware` matches the
//     `github.com/go-chi/chi` entry.
//
// In-tree imports (any module path not on the allowlist) are NOT
// touched here — the resolver's downstream cross-file path binds them
// via the import's source_module / imported_name properties when those
// are populated.
//
// Keep in sync with internal/external/synth.go knownExternalPackages —
// this list need not be exhaustive (any miss stays as-is, which is the
// pre-fix shape), but every entry SHOULD also be present in the
// authoritative allowlist or the resolver may misclassify the edge as
// ExternalUnknown.

package golang

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// buildGoInTreeQualifiers returns a map from the package qualifier used in
// selector calls (the identifier before the dot in `resolve.BuildIndex()`)
// to the in-tree package directory (`internal/resolve`) for every in-tree
// import in the file. moduleRoot is the go.mod module path; when empty (no
// go.mod) the map is empty — external imports are never in-tree.
//
// The qualifier is the import alias when present (`r "github.com/o/p/internal/resolve"`
// → qualifier "r"), else the last slash-segment of the import path
// (`github.com/o/p/internal/resolve` → "resolve"). Only imports whose path
// starts with moduleRoot+"/" are recorded — external packages resolve via the
// ext: pathway and must not shadow an in-tree qualifier of the same leaf name.
//
// This map is consumed by extractCallRelationships to stamp a CALLS edge's
// package qualifier so the resolver can bind a cross-package call back to the
// imported package's directory (issue #4332) instead of dropping the
// package qualifier and emitting an ambiguity-prone bare name.
func buildGoInTreeQualifiers(root *sitter.Node, src []byte, moduleRoot string, replaces []goReplace) map[string]string {
	if root == nil || (moduleRoot == "" && len(replaces) == 0) {
		return nil
	}
	out := make(map[string]string)
	for _, spec := range findAll(root, "import_spec") {
		pathNode := spec.ChildByFieldName("path")
		if pathNode == nil {
			count := int(spec.ChildCount())
			for i := 0; i < count; i++ {
				ch := spec.Child(i)
				if ch.Type() == "interpreted_string_literal" || ch.Type() == "raw_string_literal" {
					pathNode = ch
					break
				}
			}
		}
		if pathNode == nil {
			continue
		}
		importPath := strings.Trim(nodeText(pathNode, src), "\"`")
		if importPath == "" {
			continue
		}
		var pkgDir string
		if moduleRoot != "" && strings.HasPrefix(importPath, moduleRoot+"/") {
			pkgDir = importPath[len(moduleRoot)+1:]
		} else if dir, ok := goReplacePkgDir(importPath, replaces); ok {
			// #4705c: local-path `replace` redirect to an in-repo package.
			pkgDir = dir
		} else {
			continue // external or the module root itself — not an in-tree subpackage
		}
		if pkgDir == "" {
			continue
		}
		// Qualifier: alias if present, else last path segment. Dot and blank
		// imports do not introduce a usable selector qualifier, so skip them.
		qualifier := ""
		if nameNode := spec.ChildByFieldName("name"); nameNode != nil {
			switch nameNode.Type() {
			case "package_identifier":
				qualifier = nodeText(nameNode, src)
			case "dot", "blank_identifier":
				continue
			}
		}
		if qualifier == "" {
			if slash := strings.LastIndexByte(pkgDir, '/'); slash >= 0 {
				qualifier = pkgDir[slash+1:]
			} else {
				qualifier = pkgDir
			}
		}
		if qualifier == "" {
			continue
		}
		// A package directory whose last segment carries a major-version
		// suffix (`.../redis/v8` → import name is still `redis`) is an
		// external concern handled elsewhere; in-tree dirs don't use the
		// `vN` convention, so the last-segment default is correct here.
		if _, dup := out[qualifier]; dup {
			// Two in-tree imports share a leaf name in this file (rare; both
			// would need an alias to compile). Without a reliable per-call
			// qualifier we cannot disambiguate — drop the mapping so the
			// resolver leaves the bare-name fallback in place rather than
			// guessing wrong.
			out[qualifier] = ""
			continue
		}
		out[qualifier] = pkgDir
	}
	// Strip the conflict sentinels.
	for q, dir := range out {
		if dir == "" {
			delete(out, q)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// goKnownExternalRoots is the set of import-path prefixes that the
// resolver's external-disposition gate classifies as ExternalKnown via
// the `ext:<prefix>` prefix. When the Go extractor sees an IMPORTS
// edge whose import path's LONGEST matching prefix is on this list, it
// rewrites the ToID to `ext:<prefix>` so the edge bypasses the bare-
// name resolver and lands on ExternalKnown directly.
//
// Entries are split into:
//   - Single-segment stdlib roots (`fmt`, `context`, `net`, ...).
//   - Multi-segment third-party roots
//     (`github.com/go-chi/chi`, `go.uber.org/zap`, ...).
//
// Longest-prefix matching is applied at lookup time so
// `github.com/go-chi/chi/v5/middleware` matches `github.com/go-chi/chi`
// without falling through to a hypothetical single-segment match.
var goKnownExternalRoots = map[string]struct{}{
	// Stdlib (single-segment first-level package names; net/http etc.
	// match via the `net` root, encoding/json via `encoding`, ...).
	"archive":   {},
	"bufio":     {},
	"bytes":     {},
	"compress":  {},
	"container": {},
	"context":   {},
	"crypto":    {},
	"database":  {},
	"debug":     {},
	"embed":     {},
	"encoding":  {},
	"errors":    {},
	"expvar":    {},
	"flag":      {},
	"fmt":       {},
	"go":        {}, // go/ast, go/types — `go` as a leading segment is reserved for stdlib only
	"hash":      {},
	"html":      {},
	"image":     {},
	"index":     {},
	"io":        {},
	"log":       {},
	"math":      {},
	"mime":      {},
	"net":       {},
	"os":        {},
	"path":      {},
	"plugin":    {},
	"reflect":   {},
	"regexp":    {},
	"runtime":   {},
	"sort":      {},
	"strconv":   {},
	"strings":   {},
	"sync":      {},
	"syscall":   {},
	"testing":   {},
	"text":      {},
	"time":      {},
	"unicode":   {},
	"unsafe":    {},
	"slices":    {},
	"maps":      {},
	"cmp":       {},
	"iter":      {},

	// HTTP / web frameworks
	"github.com/go-chi/chi":               {},
	"github.com/gin-gonic/gin":            {},
	"github.com/gorilla/mux":              {},
	"github.com/gorilla/websocket":        {},
	"github.com/labstack/echo":            {},
	"github.com/gofiber/fiber":            {},
	"github.com/julienschmidt/httprouter": {},
	"github.com/valyala/fasthttp":         {},

	// Logging
	"github.com/sirupsen/logrus":    {},
	"github.com/rs/zerolog":         {},
	"go.uber.org/zap":               {},
	"github.com/golang/glog":        {},
	"github.com/hashicorp/go-hclog": {},

	// CLI / config
	"github.com/spf13/cobra":        {},
	"github.com/spf13/viper":        {},
	"github.com/spf13/pflag":        {},
	"github.com/urfave/cli":         {},
	"github.com/alecthomas/kingpin": {},
	"github.com/joho/godotenv":      {},

	// Errors / utilities
	"github.com/pkg/errors":              {},
	"github.com/hashicorp/go-multierror": {},
	"github.com/cockroachdb/errors":      {},

	// Testing / assertions / mocking
	"github.com/stretchr/testify": {},
	"github.com/onsi/ginkgo":      {},
	"github.com/onsi/gomega":      {},
	"github.com/golang/mock":      {},
	"github.com/google/go-cmp":    {},
	"go.uber.org/mock":            {},

	// gRPC / proto / RPC
	"github.com/golang/protobuf": {},
	"google.golang.org/grpc":     {},
	"google.golang.org/protobuf": {},
	"google.golang.org/genproto": {},
	"github.com/grpc-ecosystem":  {},

	// golang.org/x umbrella
	"golang.org/x": {},

	// Databases / ORMs
	"github.com/jackc/pgx":               {},
	"github.com/lib/pq":                  {},
	"github.com/go-sql-driver/mysql":     {},
	"github.com/mattn/go-sqlite3":        {},
	"github.com/jmoiron/sqlx":            {},
	"gorm.io/gorm":                       {},
	"github.com/jinzhu/gorm":             {},
	"github.com/uptrace/bun":             {},
	"entgo.io/ent":                       {},
	"github.com/golang-migrate/migrate":  {},
	"github.com/mongodb/mongo-go-driver": {},
	"go.mongodb.org/mongo-driver":        {},

	// Redis / kafka / streaming
	"github.com/redis/go-redis":      {},
	"github.com/go-redis/redis":      {},
	"github.com/gomodule/redigo":     {},
	"github.com/segmentio/kafka-go":  {},
	"github.com/IBM/sarama":          {},
	"github.com/Shopify/sarama":      {},
	"github.com/nats-io/nats.go":     {},
	"github.com/streadway/amqp":      {},
	"github.com/rabbitmq/amqp091-go": {},

	// Cloud SDKs
	"github.com/aws/aws-sdk-go":         {},
	"github.com/aws/aws-sdk-go-v2":      {},
	"github.com/aws/aws-lambda-go":      {},
	"cloud.google.com/go":               {},
	"github.com/Azure/azure-sdk-for-go": {},

	// Observability
	"github.com/prometheus/client_golang":   {},
	"go.opentelemetry.io/otel":              {},
	"github.com/opentracing/opentracing-go": {},

	// Crypto / JWT / auth
	"github.com/golang-jwt/jwt":   {},
	"github.com/dgrijalva/jwt-go": {},
	"golang.org/x/crypto":         {},
	"github.com/google/uuid":      {},
	"github.com/satori/go.uuid":   {},
	"github.com/gofrs/uuid":       {},

	// Serialization / YAML / TOML
	"gopkg.in/yaml.v2":             {},
	"gopkg.in/yaml.v3":             {},
	"github.com/BurntSushi/toml":   {},
	"github.com/pelletier/go-toml": {},
	"github.com/json-iterator/go":  {},
	"github.com/mailru/easyjson":   {},

	// Misc utility
	"github.com/google/wire":          {},
	"github.com/google/go-github":     {},
	"github.com/pquerna/cachecontrol": {},
	"github.com/patrickmn/go-cache":   {},
	"github.com/dgraph-io/ristretto":  {},
	"github.com/hashicorp/golang-lru": {},
	"github.com/robfig/cron":          {},
	"github.com/cenkalti/backoff":     {},
	"github.com/fsnotify/fsnotify":    {},
	"github.com/davecgh/go-spew":      {},
	"github.com/stretchr/objx":        {},
	"github.com/uber-go/zap":          {},

	// TUI / UI
	"github.com/charmbracelet/huh":       {},
	"github.com/charmbracelet/bubbletea": {},
	"github.com/charmbracelet/lipgloss":  {},

	// Tree-sitter Go bindings (widely used in polyglot extractors)
	"github.com/smacker/go-tree-sitter": {},

	// Message queues / streaming (additional)
	"github.com/apache/pulsar-client-go": {},

	// Databases (additional)
	"github.com/neo4j/neo4j-go-driver": {},

	// Windows platform
	"github.com/Microsoft/go-winio": {},

	// Workflow engines
	"go.temporal.io/sdk": {},

	// Embedding / ML
	"github.com/knights-analytics/hugot": {},
}

// resolveImportToIDs walks every IMPORTS edge on every entity in
// entities and, when the import path's longest matching prefix is a
// known external Go package, rewrites the ToID to the `ext:<prefix>`
// form. Idempotent — ToIDs already carrying the `ext:` prefix are left
// alone.
//
// Mutates the entities slice's relationships in place.
func resolveImportToIDs(entities []types.EntityRecord) {
	for i := range entities {
		e := &entities[i]
		// Go IMPORTS edges live on the per-import SCOPE.Component
		// placeholder entities emitted by extractImportEntities.
		if e.Kind != "SCOPE.Component" {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if strings.HasPrefix(r.ToID, "ext:") {
				continue // already external-tagged
			}
			// Go's import path lives in the ToID (the extractor sets
			// ToID = importPath). Some emitters might also stamp
			// source_module / local_name in Properties — prefer those
			// when present, else fall back to ToID.
			mod := ""
			if r.Properties != nil {
				mod = r.Properties["source_module"]
			}
			if mod == "" {
				mod = r.ToID
			}
			if mod == "" {
				continue
			}
			// Defensive: a leading "." is never a Go import path.
			if strings.HasPrefix(mod, ".") {
				continue
			}
			prefix := longestKnownGoPrefix(mod)
			if prefix == "" {
				continue
			}
			r.ToID = "ext:" + prefix
		}
	}
}

// longestKnownGoPrefix returns the longest slash-segmented prefix of
// mod that matches an entry in goKnownExternalRoots. Walks from
// longest to shortest by repeatedly trimming the trailing slash
// segment. Returns "" when no prefix matches.
//
//	"github.com/go-chi/chi/v5/middleware"
//	  → "github.com/go-chi/chi"
//	"net/http"
//	  → "net"
//	"github.com/myorg/internal/util"
//	  → ""
//	"fmt"
//	  → "fmt"
func longestKnownGoPrefix(mod string) string {
	cur := mod
	for cur != "" {
		if _, ok := goKnownExternalRoots[cur]; ok {
			return cur
		}
		slash := strings.LastIndexByte(cur, '/')
		if slash < 0 {
			return ""
		}
		cur = cur[:slash]
	}
	return ""
}
