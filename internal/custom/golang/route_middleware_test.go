package golang

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runChi(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ents, err := (&chiExtractor{}).Extract(context.Background(), extractor.FileInput{
		Path: "main.go", Language: "go", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("chi extract: %v", err)
	}
	return ents
}

// decodeChain parses the middleware_chain JSON property into ordered entries.
func decodeChain(t *testing.T, raw string) []goMiddlewareEntry {
	t.Helper()
	if raw == "" {
		t.Fatal("middleware_chain property is empty")
	}
	var chain []goMiddlewareEntry
	if err := json.Unmarshal([]byte(raw), &chain); err != nil {
		t.Fatalf("decode middleware_chain %q: %v", raw, err)
	}
	return chain
}

// indexOf returns the order index of the first chain entry whose Name == name,
// or -1 when absent.
func indexOf(chain []goMiddlewareEntry, name string) int {
	for _, e := range chain {
		if e.Name == name {
			return e.Order
		}
	}
	return -1
}

// TestGinMiddlewareChain_EngineThenRouteOrder — engine-wide `.Use(Logger())`
// followed by `.Use(CORS())` then an inline route middleware. The bound chain
// must be OUTERMOST-first: Logger (index 0) before CORS (index 1) before the
// inline RateLimit (last). Asserts middleware IDENTITY and relative ORDER, not
// just count.
func TestGinMiddlewareChain_EngineThenRouteOrder(t *testing.T) {
	src := `package main
import "github.com/gin-gonic/gin"
func main() {
	r := gin.Default()
	r.Use(Logger())
	r.Use(CORS())
	r.GET("/users", RateLimit(), listUsers)
}
`
	ents := runGin(t, src)
	ep := findEndpoint(t, ents, "GET /users")
	chain := decodeChain(t, ep.Properties["middleware_chain"])

	if ep.Properties["middleware_count"] != "3" {
		t.Fatalf("middleware_count=%q, want 3 (chain=%v)", ep.Properties["middleware_count"], chain)
	}
	logger, cors, rl := indexOf(chain, "Logger"), indexOf(chain, "CORS"), indexOf(chain, "RateLimit")
	if logger < 0 || cors < 0 || rl < 0 {
		t.Fatalf("missing middleware in chain: Logger=%d CORS=%d RateLimit=%d (chain=%v)", logger, cors, rl, chain)
	}
	// Outermost-first: engine middleware precede the inline route middleware.
	if !(logger < cors && cors < rl) {
		t.Errorf("order wrong: Logger=%d CORS=%d RateLimit=%d, want Logger<CORS<RateLimit", logger, cors, rl)
	}
	// scope reflects both engine and route contributions.
	if ep.Properties["middleware_scope"] != "engine+route" {
		t.Errorf("middleware_scope=%q, want engine+route", ep.Properties["middleware_scope"])
	}
	// names property is in chain order.
	if got := ep.Properties["middleware_names"]; got != "Logger,CORS,RateLimit" {
		t.Errorf("middleware_names=%q, want Logger,CORS,RateLimit", got)
	}
}

// TestGinMiddlewareChain_GroupScope — a group constructed with a middleware
// arg: every route on the group inherits it. `g := r.Group("/api", Logger())`
// then `g.GET("/me", h)` → /me chain includes Logger at group scope; a route
// NOT on the group does not.
func TestGinMiddlewareChain_GroupScope(t *testing.T) {
	src := `package main
import "github.com/gin-gonic/gin"
func main() {
	r := gin.Default()
	api := r.Group("/api", AuthRequired(), Logger())
	api.GET("/me", getMe)
	r.GET("/health", healthCheck)
}
`
	ents := runGin(t, src)

	me := findEndpoint(t, ents, "GET /api/me")
	chain := decodeChain(t, me.Properties["middleware_chain"])
	auth, logger := indexOf(chain, "AuthRequired"), indexOf(chain, "Logger")
	if auth < 0 || logger < 0 {
		t.Fatalf("GET /api/me chain missing group middleware: %v", chain)
	}
	if auth >= logger {
		t.Errorf("group order wrong: AuthRequired=%d Logger=%d, want AuthRequired<Logger", auth, logger)
	}
	for _, e := range chain {
		if e.Scope != goMWScopeGroup {
			t.Errorf("entry %q scope=%q, want group", e.Name, e.Scope)
		}
	}
	if me.Properties["middleware_scope"] != "group" {
		t.Errorf("middleware_scope=%q, want group", me.Properties["middleware_scope"])
	}

	// The non-group route has no group/engine .Use → no chain bound.
	health := findEndpoint(t, ents, "GET /health")
	if health.Properties["middleware_chain"] != "" {
		t.Errorf("GET /health should have no middleware_chain, got %q", health.Properties["middleware_chain"])
	}
}

// TestGinMiddlewareChain_GroupUse — middleware registered via `g.Use(mw)` on a
// group var (not in the construction args) still binds to the group's routes.
func TestGinMiddlewareChain_GroupUse(t *testing.T) {
	src := `package main
import "github.com/gin-gonic/gin"
func main() {
	r := gin.Default()
	admin := r.Group("/admin")
	admin.Use(RequireAdmin())
	admin.GET("/stats", stats)
}
`
	ents := runGin(t, src)
	ep := findEndpoint(t, ents, "GET /admin/stats")
	chain := decodeChain(t, ep.Properties["middleware_chain"])
	if indexOf(chain, "RequireAdmin") != 0 {
		t.Errorf("RequireAdmin order=%d, want 0 (chain=%v)", indexOf(chain, "RequireAdmin"), chain)
	}
	if ep.Properties["middleware_scope"] != "group" {
		t.Errorf("middleware_scope=%q, want group", ep.Properties["middleware_scope"])
	}
}

// TestEchoMiddlewareChain_TrailingInline — echo passes route middleware AFTER
// the handler: `e.GET("/me", getMe, JWTAuth())`. The handler must be dropped
// and JWTAuth bound as route-scope middleware.
func TestEchoMiddlewareChain_TrailingInline(t *testing.T) {
	src := `package main
import "github.com/labstack/echo/v4"
func main() {
	e := echo.New()
	e.Use(Recover())
	e.GET("/me", getMe, JWTAuth())
}
`
	ents := runEcho(t, src)
	ep := findEndpoint(t, ents, "GET /me")
	chain := decodeChain(t, ep.Properties["middleware_chain"])
	rec, jwt := indexOf(chain, "Recover"), indexOf(chain, "JWTAuth")
	if rec < 0 || jwt < 0 {
		t.Fatalf("chain missing entries: Recover=%d JWTAuth=%d (chain=%v)", rec, jwt, chain)
	}
	// engine Recover is outermost; route JWTAuth innermost.
	if rec >= jwt {
		t.Errorf("order wrong: Recover=%d JWTAuth=%d, want Recover<JWTAuth", rec, jwt)
	}
	// the handler getMe must NOT appear as middleware.
	if indexOf(chain, "getMe") >= 0 {
		t.Errorf("handler getMe wrongly bound as middleware: %v", chain)
	}
	if ep.Properties["middleware_scope"] != "engine+route" {
		t.Errorf("middleware_scope=%q, want engine+route", ep.Properties["middleware_scope"])
	}
	// auth middleware appears in the chain (not double-modeled) AND carries its
	// auth_kind so the chain is consistent with the #3734 auth surface.
	for _, e := range chain {
		if e.Name == "JWTAuth" && e.AuthKind != "jwt" {
			t.Errorf("JWTAuth auth_kind=%q, want jwt", e.AuthKind)
		}
	}
}

// TestGinMiddlewareChain_DynamicSkipped — a dynamically/spread-built chain is
// NOT statically resolvable: a `.Use(mws...)` spread and a route whose only
// extra arg is the handler produce NO fabricated chain.
func TestGinMiddlewareChain_DynamicSkipped(t *testing.T) {
	src := `package main
import "github.com/gin-gonic/gin"
func main() {
	r := gin.Default()
	mws := buildMiddleware()
	r.Use(mws...)
	r.GET("/plain", plainHandler)
}
`
	ents := runGin(t, src)
	ep := findEndpoint(t, ents, "GET /plain")
	// `mws...` is a spread identifier; parseMiddlewareChain records it as a bare
	// expr (one entry) — but the route itself has only a handler, so the only
	// chain comes from the engine .Use spread. We accept the engine spread entry
	// (it IS a registered middleware var) but assert NO route-scope fabrication.
	if raw := ep.Properties["middleware_chain"]; raw != "" {
		chain := decodeChain(t, raw)
		for _, e := range chain {
			if e.Scope == goMWScopeRoute {
				t.Errorf("no route-scope middleware should be fabricated for a handler-only route, got %v", chain)
			}
		}
	}
}

// TestChiMiddlewareChain_EngineStack — chi's dominant idiom is the engine-wide
// `r.Use(...)` stack. `r.Use(Logger); r.Use(Recoverer)` then `r.Get("/x", h)`
// → /x chain is [Logger, Recoverer] in registration (outermost) order.
func TestChiMiddlewareChain_EngineStack(t *testing.T) {
	src := `package main
import "github.com/go-chi/chi/v5"
func main() {
	r := chi.NewRouter()
	r.Use(Logger)
	r.Use(Recoverer)
	r.Get("/widgets", listWidgets)
}
`
	ents := runChi(t, src)
	ep := findEndpoint(t, ents, "GET /widgets")
	chain := decodeChain(t, ep.Properties["middleware_chain"])
	logger, rec := indexOf(chain, "Logger"), indexOf(chain, "Recoverer")
	if logger < 0 || rec < 0 {
		t.Fatalf("chi chain missing entries: Logger=%d Recoverer=%d (chain=%v)", logger, rec, chain)
	}
	if logger >= rec {
		t.Errorf("chi order wrong: Logger=%d Recoverer=%d, want Logger<Recoverer", logger, rec)
	}
	if ep.Properties["middleware_scope"] != "engine" {
		t.Errorf("middleware_scope=%q, want engine", ep.Properties["middleware_scope"])
	}
}

// TestGinMiddlewareChain_NoMiddleware — a route with no engine/group/inline
// middleware carries no chain (negative case: non-middleware route → no stamp).
func TestGinMiddlewareChain_NoMiddleware(t *testing.T) {
	src := `package main
import "github.com/gin-gonic/gin"
func main() {
	r := gin.New()
	r.GET("/ping", pong)
}
`
	ents := runGin(t, src)
	ep := findEndpoint(t, ents, "GET /ping")
	if ep.Properties["middleware_chain"] != "" {
		t.Errorf("GET /ping: middleware_chain=%q, want empty", ep.Properties["middleware_chain"])
	}
	if ep.Properties["middleware_count"] != "" {
		t.Errorf("GET /ping: middleware_count=%q, want empty", ep.Properties["middleware_count"])
	}
}
