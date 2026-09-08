package engine

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// #6916 Tier A — the eight `Config` source_patterns whose construction is
// assigned to a variable on the same line:
//
// The four Go sites are THREE entries each after #7022 (short / `var` /
// grouped); the line below is the short form, the other two follow it.
//	go/frameworks/chi.yaml:79        chi.NewRouter()
//	go/frameworks/echo.yaml:73       echo.New()
//	go/frameworks/fiber.yaml:73      fiber.New()
//	go/frameworks/gin.yaml:73        gin.Default() / gin.New()
//	go/frameworks/gorm.yaml:15       gorm.Open(...)
//	rust/frameworks/axum.yaml:82     Router::new()
//	csharp/frameworks/asp_net_core.yaml:90  WebApplication.CreateBuilder(...)
//	csharp/frameworks/net_maui.yaml:124     MauiApp.CreateBuilder()
//
// All eight used `name_group: 0`, so the entity Name was the whole regex match
// — `"gin.Default()"`, `"WebApplication.CreateBuilder("`. Those are marker
// nodes naming a call site, not a thing: nothing can bind to them, and because
// the scope is `file` a file constructing several routers collapsed to ONE.
// Measured on the corpus: chi 50 `chi.NewRouter()` -> 30 Config entities;
// actix's sibling shape 83 -> 73.
//
// Naming the entity after the ASSIGNED VARIABLE is what makes a per-router
// anchor possible (#6914) — `api := r.Group("/api")` can hang off `api` rather
// than off a single per-file marker.
//
// These tests follow the Tier B (aws_cdk) template proven in PR #7015, which
// found three defects on two sites. All three of its cases are carried here,
// PER LANGUAGE, because Tier B measured its Python mutant ALIVE after the JS
// axis was fully graded:
//
//  1. the unanchored capture (Tier B's blocker: a WRONG name, not a missing one),
//  2. the type-annotated declaration (which captured the TYPE),
//  3. the over-firing negative (every Tier B mutant moved the rule in the
//     restrictive direction; the permissive one was ungraded).
//
// Note the interaction, which is why case 3 matters more after this change than
// before it: making the Name useful also makes an over-firing hit HARDER to
// spot. A junk entity called `gin.Default()` is visibly a marker; one called
// `r` reads as a real router. These land in `Config`, which #7013 measures at
// 60.5% polluted.
//
// Assertions are on the ARTEFACT — the emitted `<Kind>:<Name>` set — never on a
// count, and every absence assertion carries a positive control.

// tierA6916Site is one rule site. Every fixture for a site is written so the
// ONLY difference between a leg and its control is the token under test.
type tierA6916Site struct {
	rule string // rule file:line, so a failure names the site to edit
	lang string
	path string

	// marker is the Name the shipped `name_group: 0` produced. It must never
	// come back.
	marker string

	// control is a non-Config entity every fixture below emits. Without it the
	// absence assertions could pass on a fixture that stopped producing
	// anything at all.
	control string

	// twoVars constructs the framework object TWICE, bound to `first` and
	// `second`. Empty where a second construction in one file is not idiomatic.
	twoVars       string
	first, second string

	// annotated declares the variable with an explicit type. Must still name
	// the VARIABLE; naming `want` proves the annotation slot is not swallowing
	// the capture.
	annotated     string
	annotatedWant string

	// commented is the same construction inside a line comment: the shape that
	// grades the `(?m)^[ \t]*` anchor. Must mint no Config.
	// commentedCtl is the SAME file with the comment marker removed.
	//
	// On the four Go sites the fixture carries the short, the single-line `var`
	// and the GROUPED spelling, because #7022 split those sites into three
	// entries and the anchor has to be graded on each one separately: with the
	// anchor dropped from the `var` entry alone the whole suite stayed green
	// while `// var mux *chi.Mux = chi.NewRouter()` minted `Config:mux`.
	// commentedExtraWant lists the additional names the control must mint.
	commented          string
	commentedCtl       string
	commentedWant      string
	commentedExtraWant []string

	// nonFramework spells the same call against another package/type; it must
	// mint nothing. nonFrameworkCtl is the same file with the framework
	// spelling restored.
	nonFramework     string
	nonFrameworkCtl  string
	nonFrameworkWant string

	// keywordInit puts a KEYWORD where the pattern reads the variable name:
	// Go's if/switch/go statement heads, in BOTH the `:=` and the plain `=`
	// spelling. Measured at `87106f12c`, these minted `Config:if`,
	// `Config:switch` and `Config:go`; the whole fixture must now mint NOTHING.
	// Go-only: rust's pattern requires a line-initial `let` and C# has no
	// if-initialiser, and both were probed through the real detector and mint
	// nothing here — see the test below.
	keywordInit string

	// groupedVar declares the construction inside a `var (…)` block WITH an
	// explicit type — the shape that falls between a short form with no type
	// slot and a `var` form whose type slot sits behind a literal `var`, since
	// inside the block the `var` is on its own line. It matched before the
	// split and must keep matching: losing a working binding is worse than the
	// junk name the split removed. groupedVarWant is asserted; the block also
	// carries a SECOND declarator, so a per-line entry is graded rather than a
	// once-per-block one.
	groupedVar                      string
	groupedVarWant, groupedVarWant2 string

	// reassigned rebinds an EXISTING variable with no `let`. Only axum carries
	// one: its pattern requires the declaration keyword, and this is the shape
	// that grades that requirement. Must mint no Config.
	reassigned string

	// unassigned constructs the object with no binding at all — the deliberate
	// cost of anchoring on the assignment. Must mint no Config.
	unassigned string

	// qualifier is the package/type prefix on the constructor — `chi.`,
	// `WebApplication.`. Deleting it from nonFrameworkCtl yields the BARE call,
	// which must mint nothing: see
	// TestIssue6916_TierABareConstructorCallMintsNoConfig. Empty for axum,
	// whose qualifier is the type itself (`Router::new()`); removing it leaves
	// `::new()`, which is not Rust, so its over-firing leg is the
	// `MyRouter::new()` substring instead.
	qualifier string

	// fieldTarget assigns the construction to a FIELD rather than to a local,
	// which is what the `(?:\w+\.)?` receiver prefix is for: the entity must be
	// named after the field, not after the receiver and not `recv.field`. Empty
	// for rust, whose pattern has no such prefix (it requires a `let` binding).
	fieldTarget     string
	fieldTargetWant string
}

// tierA6916Sites is the whole tier. Fixtures are grouped by site rather than by
// case so that a site's positive, its controls and its negatives are read
// together — the Tier B review found two of its three defects by reading a
// fixture body against the claim its name made.
var tierA6916Sites = []tierA6916Site{
	{
		rule:      "go/frameworks/chi.yaml:79",
		qualifier: "chi.",
		lang:      "go",
		path:      "main.go",
		marker:    "chi.NewRouter()",
		control:   "listUsers",
		twoVars: `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listUsers(w http.ResponseWriter, r *http.Request) {}

func main() {
	r := chi.NewRouter()
	api := chi.NewRouter()
	r.Mount("/api", api)
}
`,
		first: "r", second: "api",
		keywordInit: `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listUsers(w http.ResponseWriter, r *http.Request) {}

func main() {
	if r := chi.NewRouter(); r != nil {
		_ = r
	}

	var api *chi.Mux
	if api = chi.NewRouter(); api != nil {
		_ = api
	}

	switch api = chi.NewRouter(); {
	default:
		_ = api
	}
}
`,
		groupedVar: `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listUsers(w http.ResponseWriter, r *http.Request) {}

var (
	grouped *chi.Mux = chi.NewRouter()
	secondary *chi.Mux = chi.NewRouter()
)
`,
		groupedVarWant: "grouped", groupedVarWant2: "secondary",
		annotated: `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listUsers(w http.ResponseWriter, r *http.Request) {}

var mux *chi.Mux = chi.NewRouter()
`,
		annotatedWant: "mux",
		commented: `package main

import "net/http"

func listUsers(w http.ResponseWriter, r *http.Request) {}

// Historically this file built its own router:
//	r := chi.NewRouter()
//	var mux *chi.Mux = chi.NewRouter()
//	var (
//		grouped *chi.Mux = chi.NewRouter()
//	)
`,
		commentedCtl: `package main

import "net/http"

func listUsers(w http.ResponseWriter, r *http.Request) {}

// Historically this file built its own router:
r := chi.NewRouter()
var mux *chi.Mux = chi.NewRouter()
var (
	grouped *chi.Mux = chi.NewRouter()
)
`,
		commentedWant: "r", commentedExtraWant: []string{"mux", "grouped"},
		nonFramework: `package main

import (
	"net/http"

	"example.com/app/router"
)

func listUsers(w http.ResponseWriter, r *http.Request) {}

func main() {
	r := router.NewRouter()
	_ = r
}
`,
		nonFrameworkCtl: `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listUsers(w http.ResponseWriter, r *http.Request) {}

func main() {
	r := chi.NewRouter()
	_ = r
}
`,
		nonFrameworkWant: "r",
		unassigned: `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listUsers(w http.ResponseWriter, r *http.Request) {}

func main() {
	http.ListenAndServe(":8080", chi.NewRouter())
}
`,
		fieldTarget: `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listUsers(w http.ResponseWriter, r *http.Request) {}

func (s *Server) setup() {
	s.router = chi.NewRouter()
}
`,
		fieldTargetWant: "router",
	},
	{
		rule:      "go/frameworks/echo.yaml:73",
		qualifier: "echo.",
		lang:      "go",
		path:      "main.go",
		marker:    "echo.New()",
		control:   "listUsers",
		twoVars: `package main

import "github.com/labstack/echo/v4"

func listUsers(c echo.Context) error { return nil }

func main() {
	e := echo.New()
	admin := echo.New()
	_, _ = e, admin
}
`,
		first: "e", second: "admin",
		keywordInit: `package main

import "github.com/labstack/echo/v4"

func listUsers(c echo.Context) error { return nil }

func main() {
	if e := echo.New(); e != nil {
		_ = e
	}

	var admin *echo.Echo
	if admin = echo.New(); admin != nil {
		_ = admin
	}

	switch admin = echo.New(); {
	default:
		_ = admin
	}
}
`,
		groupedVar: `package main

import "github.com/labstack/echo/v4"

func listUsers(c echo.Context) error { return nil }

var (
	grouped *echo.Echo = echo.New()
	secondary *echo.Echo = echo.New()
)
`,
		groupedVarWant: "grouped", groupedVarWant2: "secondary",
		annotated: `package main

import "github.com/labstack/echo/v4"

func listUsers(c echo.Context) error { return nil }

var srv *echo.Echo = echo.New()
`,
		annotatedWant: "srv",
		commented: `package main

import "github.com/labstack/echo/v4"

func listUsers(c echo.Context) error { return nil }

// Example from the README:
//	e := echo.New()
//	var srv *echo.Echo = echo.New()
//	var (
//		grouped *echo.Echo = echo.New()
//	)
`,
		commentedCtl: `package main

import "github.com/labstack/echo/v4"

func listUsers(c echo.Context) error { return nil }

// Example from the README:
e := echo.New()
var srv *echo.Echo = echo.New()
var (
	grouped *echo.Echo = echo.New()
)
`,
		commentedWant: "e", commentedExtraWant: []string{"srv", "grouped"},
		nonFramework: `package main

import (
	"example.com/app/engine"
	"github.com/labstack/echo/v4"
)

func listUsers(c echo.Context) error { return nil }

func main() {
	e := engine.New()
	_ = e
}
`,
		nonFrameworkCtl: `package main

import (
	"example.com/app/engine"
	"github.com/labstack/echo/v4"
)

func listUsers(c echo.Context) error { return nil }

func main() {
	e := echo.New()
	_ = e
}
`,
		nonFrameworkWant: "e",
		unassigned: `package main

import "github.com/labstack/echo/v4"

func listUsers(c echo.Context) error { return nil }

func main() {
	serve(echo.New())
}
`,
		fieldTarget: `package main

import "github.com/labstack/echo/v4"

func listUsers(c echo.Context) error { return nil }

func (s *Server) setup() {
	s.engine = echo.New()
}
`,
		fieldTargetWant: "engine",
	},
	{
		rule:      "go/frameworks/fiber.yaml:73",
		qualifier: "fiber.",
		lang:      "go",
		path:      "main.go",
		marker:    "fiber.New()",
		control:   "listUsers",
		twoVars: `package main

import "github.com/gofiber/fiber/v2"

func listUsers(c *fiber.Ctx) error { return nil }

func main() {
	app := fiber.New()
	metrics := fiber.New()
	app.Mount("/metrics", metrics)
}
`,
		first: "app", second: "metrics",
		keywordInit: `package main

import "github.com/gofiber/fiber/v2"

func listUsers(c *fiber.Ctx) error { return nil }

func main() {
	if app := fiber.New(); app != nil {
		_ = app
	}

	var metrics *fiber.App
	if metrics = fiber.New(); metrics != nil {
		_ = metrics
	}

	switch metrics = fiber.New(); {
	default:
		_ = metrics
	}
}
`,
		groupedVar: `package main

import "github.com/gofiber/fiber/v2"

func listUsers(c *fiber.Ctx) error { return nil }

var (
	grouped *fiber.App = fiber.New()
	secondary *fiber.App = fiber.New()
)
`,
		groupedVarWant: "grouped", groupedVarWant2: "secondary",
		annotated: `package main

import "github.com/gofiber/fiber/v2"

func listUsers(c *fiber.Ctx) error { return nil }

var app *fiber.App = fiber.New()
`,
		annotatedWant: "app",
		commented: `package main

import "github.com/gofiber/fiber/v2"

func listUsers(c *fiber.Ctx) error { return nil }

// Quickstart:
//	app := fiber.New()
//	var srv *fiber.App = fiber.New()
//	var (
//		grouped *fiber.App = fiber.New()
//	)
`,
		commentedCtl: `package main

import "github.com/gofiber/fiber/v2"

func listUsers(c *fiber.Ctx) error { return nil }

// Quickstart:
app := fiber.New()
var srv *fiber.App = fiber.New()
var (
	grouped *fiber.App = fiber.New()
)
`,
		commentedWant: "app", commentedExtraWant: []string{"srv", "grouped"},
		nonFramework: `package main

import (
	"example.com/app/builder"
	"github.com/gofiber/fiber/v2"
)

func listUsers(c *fiber.Ctx) error { return nil }

func main() {
	app := builder.New()
	_ = app
}
`,
		nonFrameworkCtl: `package main

import (
	"example.com/app/builder"
	"github.com/gofiber/fiber/v2"
)

func listUsers(c *fiber.Ctx) error { return nil }

func main() {
	app := fiber.New()
	_ = app
}
`,
		nonFrameworkWant: "app",
		unassigned: `package main

import "github.com/gofiber/fiber/v2"

func listUsers(c *fiber.Ctx) error { return nil }

func main() {
	serve(fiber.New())
}
`,
		fieldTarget: `package main

import "github.com/gofiber/fiber/v2"

func listUsers(c *fiber.Ctx) error { return nil }

func (s *Server) setup() {
	s.app = fiber.New()
}
`,
		fieldTargetWant: "app",
	},
	{
		rule:      "go/frameworks/gin.yaml:73",
		qualifier: "gin.",
		lang:      "go",
		path:      "main.go",
		marker:    "gin.Default()",
		control:   "listUsers",
		// The #6914 shape verbatim: `api := r.Group("/api")` needs `r` to be a
		// named entity before it can hang off anything.
		twoVars: `package main

import "github.com/gin-gonic/gin"

func listUsers(c *gin.Context) {}

func main() {
	r := gin.Default()
	admin := gin.New()
	r.GET("/users", listUsers)
	_ = admin
}
`,
		first: "r", second: "admin",
		keywordInit: `package main

import "github.com/gin-gonic/gin"

func listUsers(c *gin.Context) {}

func main() {
	if r := gin.Default(); r != nil {
		_ = r
	}

	var admin *gin.Engine
	if admin = gin.Default(); admin != nil {
		_ = admin
	}

	switch admin = gin.Default(); {
	default:
		_ = admin
	}
}
`,
		groupedVar: `package main

import "github.com/gin-gonic/gin"

func listUsers(c *gin.Context) {}

var (
	grouped *gin.Engine = gin.Default()
	secondary *gin.Engine = gin.Default()
)
`,
		groupedVarWant: "grouped", groupedVarWant2: "secondary",
		annotated: `package main

import "github.com/gin-gonic/gin"

func listUsers(c *gin.Context) {}

var engine *gin.Engine = gin.Default()
`,
		annotatedWant: "engine",
		commented: `package main

import "github.com/gin-gonic/gin"

func listUsers(c *gin.Context) {}

// The default engine ships with Logger and Recovery:
//	r := gin.Default()
//	var engine *gin.Engine = gin.Default()
//	var (
//		grouped *gin.Engine = gin.Default()
//	)
`,
		commentedCtl: `package main

import "github.com/gin-gonic/gin"

func listUsers(c *gin.Context) {}

// The default engine ships with Logger and Recovery:
r := gin.Default()
var engine *gin.Engine = gin.Default()
var (
	grouped *gin.Engine = gin.Default()
)
`,
		commentedWant: "r", commentedExtraWant: []string{"engine", "grouped"},
		nonFramework: `package main

import (
	"example.com/app/engine"
	"github.com/gin-gonic/gin"
)

func listUsers(c *gin.Context) {}

func main() {
	r := engine.Default()
	_ = r
}
`,
		nonFrameworkCtl: `package main

import (
	"example.com/app/engine"
	"github.com/gin-gonic/gin"
)

func listUsers(c *gin.Context) {}

func main() {
	r := gin.Default()
	_ = r
}
`,
		nonFrameworkWant: "r",
		unassigned: `package main

import "github.com/gin-gonic/gin"

func listUsers(c *gin.Context) {}

func main() {
	serve(gin.Default())
}
`,
		fieldTarget: `package main

import "github.com/gin-gonic/gin"

func listUsers(c *gin.Context) {}

func (s *Server) setup() {
	s.engine = gin.Default()
}
`,
		fieldTargetWant: "engine",
	},
	{
		rule:      "go/frameworks/gorm.yaml:15",
		qualifier: "gorm.",
		lang:      "go",
		path:      "db.go",
		marker:    "gorm.Open(",
		control:   "User",
		twoVars: `package db

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Name string
}

func open() {
	primary, err := gorm.Open(pgDialector(), &gorm.Config{})
	replica, err2 := gorm.Open(pgDialector(), &gorm.Config{})
	_, _, _, _ = primary, replica, err, err2
}
`,
		first: "primary", second: "replica",
		// gorm is the control for the whole #7022 split: it was NOT split, its
		// comma list already made it immune, and it must stay immune.
		keywordInit: `package db

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Name string
}

func open() {
	if db, err := gorm.Open(pgDialector(), &gorm.Config{}); err == nil {
		_, _ = db, err
	}
}
`,
		annotated: `package db

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Name string
}

var conn *gorm.DB = gorm.Open(pgDialector(), &gorm.Config{})
`,
		annotatedWant: "conn",
		commented: `package db

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Name string
}

// Connecting looks like this:
//	db, err := gorm.Open(pgDialector(), &gorm.Config{})
`,
		commentedCtl: `package db

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Name string
}

// Connecting looks like this:
db, err := gorm.Open(pgDialector(), &gorm.Config{})
`,
		commentedWant: "db",
		nonFramework: `package db

import (
	"database/sql"

	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Name string
}

func open() {
	db, err := sql.Open("postgres", dsn)
	_, _ = db, err
}
`,
		nonFrameworkCtl: `package db

import (
	"database/sql"

	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Name string
}

func open() {
	db, err := gorm.Open("postgres", dsn)
	_, _ = db, err
}
`,
		nonFrameworkWant: "db",
		unassigned: `package db

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Name string
}

func open() {
	use(gorm.Open(pgDialector(), &gorm.Config{}))
}
`,
		fieldTarget: `package db

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Name string
}

func (s *Store) connect() {
	s.conn = gorm.Open(pgDialector(), &gorm.Config{})
}
`,
		fieldTargetWant: "conn",
	},
	{
		rule:    "rust/frameworks/axum.yaml:82",
		lang:    "rust",
		path:    "src/main.rs",
		marker:  "Router::new()",
		control: "/health",
		twoVars: `use axum::{routing::get, Router};

async fn health() -> &'static str { "ok" }

fn build() -> Router {
    let api = Router::new();
    let mut app = Router::new();
    app = app.route("/health", get(health)).nest("/api", api);
    app
}
`,
		first: "api", second: "app",
		annotated: `use axum::{routing::get, Router};

async fn health() -> &'static str { "ok" }

fn build() -> Router {
    let app: Router = Router::new();
    app.route("/health", get(health))
}
`,
		annotatedWant: "app",
		commented: `use axum::{routing::get, Router};

async fn health() -> &'static str { "ok" }

fn build() -> Router {
    // let app = Router::new();
    routes().route("/health", get(health))
}
`,
		commentedCtl: `use axum::{routing::get, Router};

async fn health() -> &'static str { "ok" }

fn build() -> Router {
    let app = Router::new();
    routes().route("/health", get(health))
}
`,
		commentedWant: "app",
		// The old unanchored pattern matched `Router::new()` as a SUBSTRING of
		// `MyRouter::new()`, minting a Config in a crate with no axum in it.
		nonFramework: `use axum::routing::get;
use crate::my_router::MyRouter;

async fn health() -> &'static str { "ok" }

fn build() {
    let app = MyRouter::new();
    app.route("/health", get(health));
}
`,
		nonFrameworkCtl: `use axum::routing::get;
use axum::Router;

async fn health() -> &'static str { "ok" }

fn build() {
    let app = Router::new();
    app.route("/health", get(health));
}
`,
		nonFrameworkWant: "app",
		unassigned: `use axum::{routing::get, Router};

async fn health() -> &'static str { "ok" }

fn build() -> Router {
    Router::new().route("/health", get(health))
}
`,
		reassigned: `use axum::{routing::get, Router};

async fn health() -> &'static str { "ok" }

fn rebuild(mut app: Router) -> Router {
    app = Router::new();
    app.route("/health", get(health))
}
`,
	},
	{
		rule:      "csharp/frameworks/asp_net_core.yaml:90",
		qualifier: "WebApplication.",
		lang:      "csharp",
		path:      "Program.cs",
		marker:    "WebApplication.CreateBuilder(",
		// app.MapGet mints Operation:"/health"; the Config rules in this file
		// are the one under test plus none other, so "no Config" is exact.
		control: "/health",
		// Two builders in one C# file is legal but not idiomatic — a
		// WebApplication has one entry point per Program.cs. It is graded here
		// anyway (#7022): an argument about idiom does not fail a build, and
		// leaving the multiplicity axis ungraded on two of eight sites is the
		// asymmetry this PR family keeps filing. Measured through the real
		// detector before it was pinned: [first second].
		twoVars: `var first = WebApplication.CreateBuilder(args);
var second = WebApplication.CreateBuilder(args);
var app = first.Build();
app.MapGet("/health", () => "ok");
`,
		first: "first", second: "second",
		annotated: `var _unused = 0;
WebApplicationBuilder builder = WebApplication.CreateBuilder(args);
var app = builder.Build();
app.MapGet("/health", () => "ok");
`,
		annotatedWant: "builder",
		commented: `// var builder = WebApplication.CreateBuilder(args);
var app = Bootstrap();
app.MapGet("/health", () => "ok");
`,
		commentedCtl: `var builder = WebApplication.CreateBuilder(args);
var app = Bootstrap();
app.MapGet("/health", () => "ok");
`,
		commentedWant: "builder",
		nonFramework: `var builder = Host.CreateBuilder(args);
var app = builder.Build();
app.MapGet("/health", () => "ok");
`,
		nonFrameworkCtl: `var builder = WebApplication.CreateBuilder(args);
var app = builder.Build();
app.MapGet("/health", () => "ok");
`,
		nonFrameworkWant: "builder",
		unassigned: `WebApplication.CreateBuilder(args).Build().Run();
app.MapGet("/health", () => "ok");
`,
		fieldTarget: `public sealed class Bootstrapper
{
    private WebApplicationBuilder builder;

    public void Setup()
    {
        this.builder = WebApplication.CreateBuilder(args);
        var app = this.builder.Build();
        app.MapGet("/health", () => "ok");
    }
}
`,
		fieldTargetWant: "builder",
	},
	{
		rule:      "csharp/frameworks/net_maui.yaml:124",
		qualifier: "MauiApp.",
		lang:      "csharp",
		path:      "MauiProgram.cs",
		marker:    "MauiApp.CreateBuilder(",
		// Routing.RegisterRoute mints Route:"orderdetail". Deliberately NOT
		// `.UseMauiApp<App>()`, which mints a Config of its own and would make
		// the "no Config" assertions below ambiguous.
		control: "orderdetail",
		// As on asp_net_core above: one MauiApp builder per MauiProgram.cs is
		// the idiom, but the multiplicity is graded anyway (#7022). Measured
		// through the real detector before it was pinned: [firstApp secondApp].
		twoVars: `public static class MauiProgram
{
    public static MauiApp CreateMauiApp()
    {
        var firstApp = MauiApp.CreateBuilder();
        var secondApp = MauiApp.CreateBuilder();
        Routing.RegisterRoute("orderdetail", typeof(OrderDetailPage));
        return firstApp.Build();
    }
}
`,
		first: "firstApp", second: "secondApp",
		annotated: `public static class MauiProgram
{
    public static MauiApp CreateMauiApp()
    {
        MauiAppBuilder builder = MauiApp.CreateBuilder();
        Routing.RegisterRoute("orderdetail", typeof(OrderDetailPage));
        return builder.Build();
    }
}
`,
		annotatedWant: "builder",
		commented: `public static class MauiProgram
{
    public static MauiApp CreateMauiApp()
    {
        // var builder = MauiApp.CreateBuilder();
        Routing.RegisterRoute("orderdetail", typeof(OrderDetailPage));
        return Bootstrap();
    }
}
`,
		commentedCtl: `public static class MauiProgram
{
    public static MauiApp CreateMauiApp()
    {
        var builder = MauiApp.CreateBuilder();
        Routing.RegisterRoute("orderdetail", typeof(OrderDetailPage));
        return Bootstrap();
    }
}
`,
		commentedWant: "builder",
		nonFramework: `public static class MauiProgram
{
    public static MauiApp CreateMauiApp()
    {
        var builder = App.CreateBuilder();
        Routing.RegisterRoute("orderdetail", typeof(OrderDetailPage));
        return builder.Build();
    }
}
`,
		nonFrameworkCtl: `public static class MauiProgram
{
    public static MauiApp CreateMauiApp()
    {
        var builder = MauiApp.CreateBuilder();
        Routing.RegisterRoute("orderdetail", typeof(OrderDetailPage));
        return builder.Build();
    }
}
`,
		nonFrameworkWant: "builder",
		unassigned: `public static class MauiProgram
{
    public static MauiApp CreateMauiApp()
    {
        Routing.RegisterRoute("orderdetail", typeof(OrderDetailPage));
        return MauiApp.CreateBuilder().Build();
    }
}
`,
		fieldTarget: `public sealed class Bootstrapper
{
    private MauiAppBuilder builder;

    public void Setup()
    {
        this.builder = MauiApp.CreateBuilder();
        Routing.RegisterRoute("orderdetail", typeof(OrderDetailPage));
    }
}
`,
		fieldTargetWant: "builder",
	},
}

// configNames6916 returns the Names of every emitted Config entity.
func configNames6916(ids []string) []string {
	out := make([]string, 0, 2)
	for _, id := range ids {
		if name, ok := strings.CutPrefix(id, "Config:"); ok {
			out = append(out, name)
		}
	}
	return out
}

// assertControl6916 fails unless the fixture still emits its control entity, so
// that no absence assertion in this file can pass vacuously on a fixture that
// stopped producing anything at all.
func assertControl6916(t *testing.T, ids []string, control string) {
	t.Helper()
	for _, id := range ids {
		if _, name, ok := strings.Cut(id, ":"); ok && name == control {
			return
		}
	}
	t.Errorf("positive control missing: this fixture no longer emits an entity named %q, "+
		"so the assertions above prove nothing. Entities:\n  %s", control, strings.Join(ids, "\n  "))
}

// TestIssue6916_TierAConfigIsNamedAfterItsVariable is the change itself: each
// site's Config entity is named after the ASSIGNED VARIABLE, never after the
// whole regex match.
func TestIssue6916_TierAConfigIsNamedAfterItsVariable(t *testing.T) {
	identifier := regexp.MustCompile(`^\w+$`)

	for _, s := range tierA6916Sites {
		t.Run(s.rule, func(t *testing.T) {
			// Sites with a twoVars fixture are graded there; the rest use
			// their annotated fixture, which is a single construction.
			src, want := s.twoVars, s.first
			if src == "" {
				src, want = s.annotated, s.annotatedWant
			}
			ids := entityIDs6916(detect6916(t, s.path, s.lang, src))

			if !slices.Contains(ids, "Config:"+want) {
				t.Errorf("%s: the Config entity is not named after its assigned variable; "+
					"expected Config:%s among:\n  %s", s.rule, want, strings.Join(ids, "\n  "))
			}
			if slices.Contains(ids, "Config:"+s.marker) {
				t.Errorf("%s: the #6916 marker Name is back: Config:%q. name_group must be the "+
					"variable capture, not 0 (the whole match).", s.rule, s.marker)
			}
			for _, name := range configNames6916(ids) {
				if !identifier.MatchString(name) {
					t.Errorf("%s: a Config entity was minted with a non-identifier Name: %q. "+
						"A marker string cannot be an edge endpoint — that is the whole defect.",
						s.rule, name)
				}
			}
			assertControl6916(t, ids, s.control)
		})
	}
}

// TestIssue6916_TierAEachConstructionGetsItsOwnEntity is the #6914 anchor, and
// the reason this tier is worth doing at all. `scope: file` plus a constant
// Name collapsed every construction in a file onto ONE marker node: chi's 50
// `chi.NewRouter()` calls became 30 Config entities, and
// `chi/middleware/throttle_test.go` built six routers and yielded one. There
// was nothing for a per-router `Middleware REGISTERED_ON <router>` edge to
// point at, and adding the edge rule would not have created one.
//
// All EIGHT sites are graded here. The two C# sites were originally left out on
// the argument that one builder per Program.cs is the idiom; #7022 put them
// back, because an idiom argument does not fail a build and a tightening that
// reintroduced per-file collapsing would have been caught by six sites and
// missed by two.
func TestIssue6916_TierAEachConstructionGetsItsOwnEntity(t *testing.T) {
	for _, s := range tierA6916Sites {
		if s.twoVars == "" {
			continue
		}
		t.Run(s.rule, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, s.path, s.lang, s.twoVars))
			names := configNames6916(ids)

			for _, want := range []string{s.first, s.second} {
				if !slices.Contains(names, want) {
					t.Errorf("%s: two constructions in one file must mint two entities; %q is "+
						"missing. Config names emitted: %v", s.rule, want, names)
				}
			}
			if s.first == s.second {
				t.Fatalf("%s: fixture bug — the two variables must differ", s.rule)
			}
			assertControl6916(t, ids, s.control)
		})
	}
}

// TestIssue6916_TierAAnnotatedDeclarationNamesTheVariable is Tier B's review
// ask 2, carried to every annotated language here. `const app: cdk.App = new
// cdk.App()` captured the TYPE, `App`, because no fixture carried an
// annotation. Go spells it `var mux *chi.Mux = …`, Rust `let app: Router = …`,
// C# `WebApplicationBuilder builder = …`; each has its own slot in its own
// pattern, so each needs its own fixture. Tier B measured its Python mutant
// ALIVE after the JS axis was fully graded — enumerating one axis thoroughly is
// exactly what makes its sibling look covered.
func TestIssue6916_TierAAnnotatedDeclarationNamesTheVariable(t *testing.T) {
	for _, s := range tierA6916Sites {
		t.Run(s.rule, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, s.path, s.lang, s.annotated))

			if !slices.Contains(ids, "Config:"+s.annotatedWant) {
				t.Errorf("%s: an explicitly typed declaration must still name the VARIABLE; "+
					"expected Config:%s among:\n  %s", s.rule, s.annotatedWant, strings.Join(ids, "\n  "))
			}
			for _, name := range configNames6916(ids) {
				if name != s.annotatedWant {
					t.Errorf("%s: the annotation slot leaked into the capture — Config:%q. The "+
						"type is not the entity; deleting the annotation group is what produces this.",
						s.rule, name)
				}
			}
			assertControl6916(t, ids, s.control)
		})
	}
}

// TestIssue6916_TierACommentedConstructionMintsNoConfig grades the
// `(?m)^[ \t]*` ANCHOR, per language. Tier B's blocker was that an unanchored
// capture is not "the assignment target" but "whatever token sits left of the
// `=`" — which turned a dangling edge into one bound to the WRONG variable.
//
// The shape that reaches that seam differs by language. Python's was a
// multi-target assignment; in Go, Rust and C# the production-reachable one is a
// commented-out example line, which every real codebase carries. Unanchored,
// `// r := chi.NewRouter()` mints `Config:r` in a file that builds no router —
// and after this change that junk entity is called `r`, which reads as a real
// router rather than as the visibly-broken marker it used to be.
//
// Each leg's control is the SAME file with the comment marker removed, so the
// absence proves something about the anchor rather than about the fixture.
func TestIssue6916_TierACommentedConstructionMintsNoConfig(t *testing.T) {
	for _, s := range tierA6916Sites {
		t.Run(s.rule, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, s.path, s.lang, s.commented))
			if names := configNames6916(ids); len(names) != 0 {
				t.Errorf("%s: a commented-out construction minted %v. The line is a comment; "+
					"the `(?m)^[ \\t]*` anchor is what keeps the capture off it.", s.rule, names)
			}
			assertControl6916(t, ids, s.control)

			ctl := entityIDs6916(detect6916(t, s.path, s.lang, s.commentedCtl))
			for _, want := range append([]string{s.commentedWant}, s.commentedExtraWant...) {
				if !slices.Contains(ctl, "Config:"+want) {
					t.Errorf("%s: positive control missing — the same file with the comment marker "+
						"removed no longer mints Config:%s, so the absence above proves nothing about "+
						"the comment. Entities:\n  %s", s.rule, want, strings.Join(ctl, "\n  "))
				}
			}
		})
	}
}

// TestIssue6916_TierAGormMultiReturnNamesTheDBNotTheError is the Go spelling of
// Tier B's blocker, and the only site in this tier where the wrong name is
// reachable from ordinary, idiomatic code rather than from a comment.
//
// `gorm.Open` returns `(*gorm.DB, error)`, so `db, err := gorm.Open(...)` is THE
// idiom. Ported verbatim, the Tier B Python pattern — unanchored, with no comma
// list — takes the token immediately left of the `:=` and mints `Config:err`:
// the error value, named as though it were the database. Identifier-shaped, so
// every other assertion in this file passes on it. Measured, not argued: that
// compound mutant produces exactly `Config:err` here.
//
// The two groups cover different halves of the hazard and are graded by
// different inputs — the comma list by THIS fixture, the anchor by the
// commented-out construction in
// TestIssue6916_TierACommentedConstructionMintsNoConfig. Removing the anchor
// ALONE still names `db`, because the comma list keeps the match starting at the
// first target; neither guard is graded by the other.
//
// Go's multi-return is positional — the first name is the first return — which
// is why a comma list can be read here but not in Python, where
// `app, env = cdk.App(), "prod"` pairs names with separate expressions.
func TestIssue6916_TierAGormMultiReturnNamesTheDBNotTheError(t *testing.T) {
	const src = `package db

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Name string
}

func open() (*gorm.DB, error) {
	db, err := gorm.Open(pgDialector(), &gorm.Config{})
	return db, err
}
`
	ids := entityIDs6916(detect6916(t, "db.go", "go", src))
	names := configNames6916(ids)

	if !slices.Contains(names, "db") {
		t.Errorf("`db, err := gorm.Open(...)` must name the CONNECTION; expected Config:db "+
			"among:\n  %s", strings.Join(ids, "\n  "))
	}
	if slices.Contains(names, "err") {
		t.Errorf("the Config was named after the ERROR value: Config:err. A capture with no " +
			"comma list takes the token left of the `:=`, which on Go's idiomatic two-value " +
			"open is `err`. That is a confidently wrong name (#6369), not a missing one, and " +
			"it is identifier-shaped so nothing else in this file can see it.")
	}
	assertControl6916(t, ids, "User")
}

// TestIssue6916_TierANonFrameworkCallMintsNoConfig grades the rule in the
// OVER-FIRING direction — the direction every Tier B mutant missed, and the one
// #7013 measures at 24.5% of framework entities landing in repos that do not
// use the framework (Config specifically at 60.5%).
//
// Each leg is the same file as its control except for the qualifier on the
// constructor. Two of them are pre-existing over-firing this change FIXES: the
// old patterns were unanchored substring matches, so `MyRouter::new()` matched
// axum's `Router\s*::\s*new` and minted a Config in a crate with no axum.
func TestIssue6916_TierANonFrameworkCallMintsNoConfig(t *testing.T) {
	for _, s := range tierA6916Sites {
		t.Run(s.rule, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, s.path, s.lang, s.nonFramework))
			if names := configNames6916(ids); len(names) != 0 {
				t.Errorf("%s: a non-framework spelling of the same call minted %v. The qualifier "+
					"is the only thing keeping this rule off every package with a similarly named "+
					"constructor — and the junk entity now carries a plausible variable name.",
					s.rule, names)
			}
			assertControl6916(t, ids, s.control)

			ctl := entityIDs6916(detect6916(t, s.path, s.lang, s.nonFrameworkCtl))
			if !slices.Contains(ctl, "Config:"+s.nonFrameworkWant) {
				t.Errorf("%s: positive control missing — the same file with the framework "+
					"qualifier restored no longer mints Config:%s, so the absence above proves "+
					"nothing about the qualifier. Entities:\n  %s",
					s.rule, s.nonFrameworkWant, strings.Join(ctl, "\n  "))
			}
		})
	}
}

// TestIssue6916_TierAUnassignedConstructionMintsNoConfig pins the DELIBERATE
// COST of anchoring on the assignment, so it is a decision on the record rather
// than a silent recall regression. An inline construction has no variable, so
// there is nothing for a per-router edge to bind to; the shipped rule gave it a
// marker node instead. Tier B measured six such shapes before and after: every
// one lost only a marker with no edge to anchor.
//
// This is also the fixture that kills a mutant re-widening any of these
// patterns to match without the assignment.
func TestIssue6916_TierAUnassignedConstructionMintsNoConfig(t *testing.T) {
	for _, s := range tierA6916Sites {
		t.Run(s.rule, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, s.path, s.lang, s.unassigned))
			if names := configNames6916(ids); len(names) != 0 {
				t.Errorf("%s: an UNASSIGNED construction minted %v. These rules are anchored on "+
					"the assignment on purpose: with no variable there is nothing for an edge to "+
					"bind to, which is what made the old marker nodes useless.", s.rule, names)
			}
			assertControl6916(t, ids, s.control)
		})
	}
}

// TestIssue6916_TierAChainedBuilderNamesTheAssignedVariable pins a shape the
// "unassigned" fixture above deliberately does NOT cover, because it is not
// unassigned: `var app = WebApplication.CreateBuilder(args).Build();` builds and
// consumes the builder in one expression, and the variable that survives is the
// APPLICATION, not the builder. The rule matches the prefix and names the Config
// `app`.
//
// That is the intended outcome and it is recorded here rather than left to be
// discovered: this entity stands for the framework entry point of the file, and
// `app` is the name a reader of that file would use for it. What matters for
// #6914 is that it is a NAME a per-router edge could bind to instead of the
// marker string `WebApplication.CreateBuilder(`. It is pinned so that a later
// narrowing — requiring the construction to be the whole right-hand side — is a
// deliberate change rather than an accident.
func TestIssue6916_TierAChainedBuilderNamesTheAssignedVariable(t *testing.T) {
	for _, tc := range []struct{ rule, path, src, want, control string }{
		{
			rule: "csharp/frameworks/asp_net_core.yaml:90",
			path: "Program.cs",
			src: `var app = WebApplication.CreateBuilder(args).Build();
app.MapGet("/health", () => "ok");
`,
			want:    "app",
			control: "/health",
		},
		{
			rule: "csharp/frameworks/net_maui.yaml:124",
			path: "MauiProgram.cs",
			src: `public static class MauiProgram
{
    public static MauiApp CreateMauiApp()
    {
        var app = MauiApp.CreateBuilder().Build();
        Routing.RegisterRoute("orderdetail", typeof(OrderDetailPage));
        return app;
    }
}
`,
			want:    "app",
			control: "orderdetail",
		},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, tc.path, "csharp", tc.src))
			if !slices.Contains(ids, "Config:"+tc.want) {
				t.Errorf("%s: a chained build must still name the assigned variable; expected "+
					"Config:%s among:\n  %s", tc.rule, tc.want, strings.Join(ids, "\n  "))
			}
			for _, name := range configNames6916(ids) {
				if name != tc.want {
					t.Errorf("%s: unexpected Config Name %q on a chained build.", tc.rule, name)
				}
			}
			assertControl6916(t, ids, tc.control)
		})
	}
}

// TestIssue6916_TierAFieldTargetNamesTheFieldNotTheReceiver grades the
// `(?:\w+\.)?` receiver prefix, and with it the dotted widening
// `(?:\w+\.)?(\w+)` -> `([\w.]+)`. Building the framework object into a struct
// field is idiomatic in both Go and C#, and there the token left of the `=` is
// `s.router`, not `router`:
//
//   - without the prefix the capture starts at `s` and the entity is named
//     after the RECEIVER — every server in a package collapses onto `s`;
//   - with a dotted capture it is named `s.router`, which is not an identifier
//     and so can never equal the `(\w+)` source of any relationship rule —
//     the #6916 defect again, wearing a nicer name.
//
// axum has no leg here on purpose: its pattern requires a `let` binding, so a
// field assignment is out of scope for it rather than mis-captured by it, and
// TestIssue6916_TierAUnassignedConstructionMintsNoConfig already pins that.
func TestIssue6916_TierAFieldTargetNamesTheFieldNotTheReceiver(t *testing.T) {
	for _, s := range tierA6916Sites {
		if s.fieldTarget == "" {
			continue
		}
		t.Run(s.rule, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, s.path, s.lang, s.fieldTarget))
			names := configNames6916(ids)

			if !slices.Contains(names, s.fieldTargetWant) {
				t.Errorf("%s: a construction assigned to a FIELD must be named after the field; "+
					"expected Config:%s among:\n  %s", s.rule, s.fieldTargetWant, strings.Join(ids, "\n  "))
			}
			for _, name := range names {
				if name != s.fieldTargetWant {
					t.Errorf("%s: Config:%q — the capture took the receiver or the whole dotted "+
						"path instead of the field. A dotted name can never equal the `(\\w+)` "+
						"source of a relationship rule.", s.rule, name)
				}
			}
			assertControl6916(t, ids, s.control)
		})
	}
}

// TestIssue6916_TierABareConstructorCallMintsNoConfig is Tier B's review ask 3
// — the OVER-FIRING direction — carried to every site, and it is the leg that
// found a live mutant on seven of the eight. Making the qualifier optional
// (`(?:chi\.)?NewRouter`, `(?:WebApplication\.)?CreateBuilder`) survived every
// other fixture in this file, because those spell the negative with a DIFFERENT
// qualifier — `router.NewRouter()` — which such a mutant still refuses. The
// input that separates them is the BARE call.
//
// It is production-reachable and not exotic: `r := NewRouter()`, `e := New()`,
// `db, err := Open(dsn)` and `var builder = CreateBuilder(args)` are what a
// package's own constructor call looks like from inside that package, in any
// repo, with no framework anywhere. Tier B's spelling of the same hazard was
// `const app = App()` in ordinary React.
//
// Each leg is DERIVED from that site's own positive control by deleting exactly
// the qualifier, so the two files cannot drift apart and the difference cannot
// be anything else. The derivation is asserted to have changed the text —
// an edit that silently does not land reads as a pass and argues the opposite.
func TestIssue6916_TierABareConstructorCallMintsNoConfig(t *testing.T) {
	for _, s := range tierA6916Sites {
		if s.qualifier == "" {
			continue
		}
		t.Run(s.rule, func(t *testing.T) {
			// Only the qualifier ON THE CONSTRUCTION is removed — `= chi.` — not
			// every mention of it, which would also strip `echo.Context` from
			// the control entity's own signature and make this leg vacuous.
			marker := "= " + s.qualifier
			if n := strings.Count(s.nonFrameworkCtl, marker); n != 1 {
				t.Fatalf("%s: fixture bug — %q occurs %d times in the control file; the "+
					"derivation below must land on exactly the construction.", s.rule, marker, n)
			}
			bare := strings.Replace(s.nonFrameworkCtl, marker, "= ", 1)
			if bare == s.nonFrameworkCtl {
				t.Fatalf("%s: fixture bug — removing the qualifier %q changed nothing, so this "+
					"leg is testing the control file, not the bare call.", s.rule, s.qualifier)
			}

			ids := entityIDs6916(detect6916(t, s.path, s.lang, bare))
			if names := configNames6916(ids); len(names) != 0 {
				t.Errorf("%s: an UNQUALIFIED constructor call minted %v in a file with no "+
					"framework qualifier anywhere. The qualifier is the only thing separating "+
					"this rule from every package that names its own constructor the same way — "+
					"and the junk entity now carries a plausible variable name instead of a "+
					"visibly-broken marker string.", s.rule, names)
			}
			assertControl6916(t, ids, s.control)

			ctl := entityIDs6916(detect6916(t, s.path, s.lang, s.nonFrameworkCtl))
			if !slices.Contains(ctl, "Config:"+s.nonFrameworkWant) {
				t.Errorf("%s: positive control missing — the same file WITH the qualifier no "+
					"longer mints Config:%s, so the absence above proves nothing. Entities:\n  %s",
					s.rule, s.nonFrameworkWant, strings.Join(ctl, "\n  "))
			}
		})
	}
}

// TestIssue6916_TierAKeywordInitialiserMintsNoConfig grades the #7022 split.
//
// A SINGLE Go entry with an optional `var` AND an optional trailing type slot
// let a STATEMENT KEYWORD match in the name slot and the real variable in the
// type slot. Measured at `87106f12c` through the real detector, on all four Go
// sites:
//
//	if r := chi.NewRouter(); …      -> Config:if
//	if r = chi.NewRouter(); …       -> Config:if
//	switch r = chi.NewRouter(); …   -> Config:switch
//	go r = chi.NewRouter()          -> Config:go
//	for r := chi.NewRouter(); …     -> Config:for
//	const r = chi.NewRouter()       -> Config:const
//
// Every one of those is a node named after a syntax token, which is the defect
// class #6916 exists to remove. The fixture carries BOTH the `:=` and the plain
// `=` spelling on purpose: a first cut pinned only `:=`, and a mutant making
// `var` optional on the declaration entry then survived the whole suite while
// the `=` twins came straight back. Pinning one spelling and leaving its legal
// sibling open is the failure this PR family keeps repeating.
//
// gorm carries the fixture too, as the control for the split itself: it was NOT
// split, and it must stay immune.
//
// The other two language families were probed through the real detector and
// mint nothing for their nearest shapes — rust `if let Some(app) = Router::new()`
// (the pattern requires a line-initial `let`) and C# `if (x) builder =
// WebApplication.CreateBuilder(args)` (no if-initialiser exists) — so they carry
// no fixture here. Two of three families measured, not assumed.
func TestIssue6916_TierAKeywordInitialiserMintsNoConfig(t *testing.T) {
	for _, s := range tierA6916Sites {
		if s.keywordInit == "" {
			continue
		}
		t.Run(s.rule, func(t *testing.T) {
			for _, kw := range []string{"if ", "= "} {
				if !strings.Contains(s.keywordInit, kw) {
					t.Fatalf("%s: fixture bug — the keywordInit fixture carries no %q, so it is "+
						"not testing the shape its name claims.", s.rule, kw)
				}
			}
			ids := entityIDs6916(detect6916(t, s.path, s.lang, s.keywordInit))
			if names := configNames6916(ids); len(names) != 0 {
				t.Errorf("%s: a statement keyword reached the name slot and minted %v. A type "+
					"slot is only safe BEHIND a literal `var` (or behind a qualified type); an "+
					"entity named after a keyword is the #6916 defect wearing a new spelling.",
					s.rule, names)
			}
			assertControl6916(t, ids, s.control)

			// Positive control: the ORDINARY assignment form on the same site
			// must still mint its variable. Without this, a rule that stopped
			// firing altogether would pass every assertion above.
			ctl := entityIDs6916(detect6916(t, s.path, s.lang, s.nonFrameworkCtl))
			if !slices.Contains(ctl, "Config:"+s.nonFrameworkWant) {
				t.Errorf("%s: positive control missing — the plain `x := %s(...)` form no longer "+
					"mints Config:%s, so the absence above proves nothing. Entities:\n  %s",
					s.rule, s.qualifier, s.nonFrameworkWant, strings.Join(ctl, "\n  "))
			}
		})
	}
}

// TestIssue6916_TierAGroupedVarDeclarationNamesEachVariable pins the shape that
// the #7022 split first LOST, and that review caught before it shipped.
//
// Splitting one pattern into several is how gaps open. The first cut had two
// entries — a short form with no type slot, and a `var` form whose type slot sat
// behind a literal `var` — and a grouped declaration matched NEITHER, because
// inside `var (…)` the keyword is on its own line:
//
//	var (
//	        r *chi.Mux = chi.NewRouter()
//	)
//
// `Config=[r]` at `87106f12c`, `Config=[]` after the first cut, on all four
// sites. Losing a binding that worked is worse than the junk name the split was
// removing, so a third entry restores it — and stays keyword-proof by requiring
// the type to be package-QUALIFIED, since `if r = chi.NewRouter(); …` cannot
// spell `r` as `chi.Something`. (Making `var` merely optional, the obvious fix,
// reopens the whole keyword hole for the plain-`=` forms above; that was
// measured, not assumed.)
//
// The block carries TWO declarators so that a once-per-block entry is graded,
// not just a once-per-file one.
//
// The residual, measured and accepted: a grouped declarator whose type is
// UNQUALIFIED — `var ( r Mux = chi.NewRouter() )`, reachable only via a
// dot-import or a local alias — minted `[r]` at the parent and mints nothing
// now. That is the deliberate price of keeping keywords out, and it is stated
// here rather than left for the next reader to rediscover.
func TestIssue6916_TierAGroupedVarDeclarationNamesEachVariable(t *testing.T) {
	for _, s := range tierA6916Sites {
		if s.groupedVar == "" {
			continue
		}
		t.Run(s.rule, func(t *testing.T) {
			if !strings.Contains(s.groupedVar, "var (") {
				t.Fatalf("%s: fixture bug — the groupedVar fixture has no `var (` block, so it "+
					"is testing the single-line declaration the `var` entry already covers.", s.rule)
			}
			ids := entityIDs6916(detect6916(t, s.path, s.lang, s.groupedVar))
			names := configNames6916(ids)
			if s.groupedVarWant == s.groupedVarWant2 {
				t.Fatalf("%s: fixture bug — the two declarators must differ", s.rule)
			}
			for _, want := range []string{s.groupedVarWant, s.groupedVarWant2} {
				if !slices.Contains(names, want) {
					t.Errorf("%s: a grouped `var (…)` declaration with an explicit type must name "+
						"EACH declarator; %q is missing. This shape matched before #7022 split the "+
						"rule — dropping it is a regression, not a tightening. Config names "+
						"emitted: %v", s.rule, want, names)
				}
			}
			assertControl6916(t, ids, s.control)
		})
	}
}

// TestIssue6916_TierAReassignmentMintsNoConfig grades axum's `let` requirement,
// which no other fixture reaches: `app = Router::new();` — a plain rebinding of
// an existing `mut` variable — is the only Rust shape that separates a rule
// anchored on the DECLARATION from one that fires on any assignment.
//
// Minting nothing here is the intended behaviour, not an oversight. The name is
// introduced by the `let`, and a rule that fires on every `x = Router::new()`
// position is the unanchored capture wearing a different hat. The cost is a
// file whose ONLY construction is a rebinding, which is what this fixture is;
// where the `let` is also present it already mints the entity.
func TestIssue6916_TierAReassignmentMintsNoConfig(t *testing.T) {
	for _, s := range tierA6916Sites {
		if s.reassigned == "" {
			continue
		}
		t.Run(s.rule, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, s.path, s.lang, s.reassigned))
			if names := configNames6916(ids); len(names) != 0 {
				t.Errorf("%s: a rebinding with no declaration keyword minted %v. The pattern is "+
					"anchored on the `let` that introduces the name.", s.rule, names)
			}
			assertControl6916(t, ids, s.control)
		})
	}
}
