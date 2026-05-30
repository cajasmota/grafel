// middleware.go — Rack middleware + filter detection for Ruby web frameworks.
//
// Detects:
//   - Rack `use SomeMiddleware` calls (all frameworks that mount Rack stacks)
//   - Rails config.middleware stack: use/insert_before/insert_after/swap/delete
//     in config/application.rb + per-environment files — emits each middleware
//     with operation name and order_position (sequential index within file).
//   - Custom Rack middleware classes: class with `def initialize(app)` +
//     `def call(env)` — emitted as rack_middleware_class entities.
//   - Rails / Padrino / Sinatra `before_action` / `after_action` / `around_action`
//     controller filters with full `:only`/`:except` scope capture
//     (middleware-equivalent pattern).
//   - Grape `before` / `after` / `rescue_from` hooks
//   - Hanami middleware in config/application.rb (config.middleware.use ...)
//   - Roda plugin + before/after
//   - Cuba before/after
//   - Sinatra `before do` / `after do` blocks
//
// Coverage cells flipped (all via `go run ./tools/coverage update`):
//
//	lang.ruby.framework.cuba     Middleware/middleware_coverage → partial
//	lang.ruby.framework.grape    Middleware/middleware_coverage → partial
//	lang.ruby.framework.hanami   Middleware/middleware_coverage → partial
//	lang.ruby.framework.padrino  Middleware/middleware_coverage → partial
//	lang.ruby.framework.rails    Middleware/middleware_coverage → full   (#3341)
//	lang.ruby.framework.roda     Middleware/middleware_coverage → partial
//	lang.ruby.framework.sinatra  Middleware/middleware_coverage → partial
//
// Part of #3282 / #3341.
package ruby

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// itoa converts an integer to its decimal string representation.
func itoa(n int) string { return strconv.Itoa(n) }

func init() {
	extractor.Register("ruby_middleware", &rubyMiddlewareExtractor{})
}

type rubyMiddlewareExtractor struct{}

func (e *rubyMiddlewareExtractor) Language() string { return "ruby_middleware" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// Rack `use SomeMiddleware` or `use SomeMiddleware, options` (all frameworks)
	reMWRackUse = regexp.MustCompile(
		`(?m)^\s*use\s+([A-Z][A-Za-z0-9_:]+)`,
	)

	// Rails/Padrino config middleware — captures the operation name and the
	// middleware class name.  Handles:
	//   config.middleware.use          MiddlewareClass
	//   config.middleware.insert_before OtherClass, MiddlewareClass
	//   config.middleware.insert_after  OtherClass, MiddlewareClass
	//   config.middleware.swap          OtherClass, MiddlewareClass
	//   config.middleware.delete        MiddlewareClass
	//
	// Group 1 = operation (use|insert_before|insert_after|swap|delete)
	// Group 2 = first class-name token (the anchor class for insert/swap ops or
	//            the target class for use/delete)
	// Group 3 = optional second class-name token (the new middleware for
	//            insert_before/insert_after/swap)
	reMWConfigOp = regexp.MustCompile(
		`(?m)\bconfig\.middleware\.(use|insert_before|insert_after|swap|delete)\s+([A-Z][A-Za-z0-9_:]+)(?:\s*,\s*([A-Z][A-Za-z0-9_:]+))?`,
	)

	// Rails before_action / after_action / around_action with optional
	// :only / :except scoping.
	//
	// Examples matched:
	//   before_action :authenticate_user!
	//   before_action :set_resource, only: [:show, :edit, :update, :destroy]
	//   after_action  :log_request, except: [:index]
	//
	// Group 1 = filter type  (before_action|after_action|around_action)
	// Group 2 = method name  (:symbol)
	// Group 3 = scope kind   (only|except) — may be empty
	// Group 4 = scope value  (raw text after "only:" or "except:") — may be empty
	reMWRailsFilterScoped = regexp.MustCompile(
		`(?m)^\s*(before_action|after_action|around_action)\s+:([a-z_]+[!?]?)` +
			`(?:[^#\n]*?\b(only|except)\s*:\s*(\[[^\]]*\]|\:[a-z_]+[!?]?))?`,
	)

	// Custom Rack middleware class: any Ruby class that defines both
	// `def initialize(app)` and `def call(env)`.  We detect them in two phases:
	//   Phase 1 — class-name line: `class SomeName`
	//   Phase 2 — presence of both initialize(app) and call(env) in the same
	//             source (file-level check; good enough for single-class files
	//             which is the idiomatic Rack pattern).
	raMwClassDef = regexp.MustCompile(
		`(?m)^\s*class\s+([A-Z][A-Za-z0-9_:]*)`,
	)
	raMwInitApp = regexp.MustCompile(
		`(?m)\bdef\s+initialize\s*\(\s*app\b`,
	)
	raMwCallEnv = regexp.MustCompile(
		`(?m)\bdef\s+call\s*\(\s*@?env\b`,
	)

	// Rails before_action / after_action / around_action (legacy — kept for
	// backwards compat with non-scoped fast-path; replaced by reMWRailsFilterScoped
	// in the extraction loop but retained so existing callers compile).
	reMWRailsFilter = regexp.MustCompile(
		`(?m)^\s*(before_action|after_action|around_action)\s+:([a-z_]+[!?]?)`,
	)

	// Grape before / after / rescue_from hooks
	reMWGrapeHook = regexp.MustCompile(
		`(?m)^\s*(before|after|rescue_from)\b`,
	)

	// Sinatra before do / after do
	reMWSinatraBlock = regexp.MustCompile(
		`(?m)^\s*(before|after)\s+do\b`,
	)

	// Sinatra before '/path' do
	reMWSinatraPathFilter = regexp.MustCompile(
		`(?m)^\s*(before|after)\s+['"]([^'"]+)['"]\s+do`,
	)

	// Roda plugin :name
	reMWRodaPlugin = regexp.MustCompile(
		`(?m)^\s*plugin\s+:([a-z_]+)`,
	)

	// Cuba before / after blocks (Cuba doesn't have formal middleware but
	// supports Rack middleware via `use` and inline before/after patterns in
	// some common wrapper libs).
	reMWCubaBlock = regexp.MustCompile(
		`(?m)^\s*(before|after)\s+do\b`,
	)

	// Hanami middleware detection: Hanami::Application or config.middleware
	reMWHanamiApp = regexp.MustCompile(
		`(?m)\bHanami::Application\b|\bHanami\.application\b`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rubyMiddlewareExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.middleware_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "ruby" {
		return nil, nil
	}

	src := string(file.Content)

	// Fast guard: skip files with no middleware signal.
	hasMW := strings.Contains(src, " use ") ||
		strings.Contains(src, "before_action") ||
		strings.Contains(src, "after_action") ||
		strings.Contains(src, "around_action") ||
		strings.Contains(src, "before do") ||
		strings.Contains(src, "after do") ||
		strings.Contains(src, "before\n") ||
		strings.Contains(src, "after\n") ||
		strings.Contains(src, "before '") ||
		strings.Contains(src, `before "`) ||
		strings.Contains(src, "after '") ||
		strings.Contains(src, `after "`) ||
		strings.Contains(src, "rescue_from") ||
		strings.Contains(src, "plugin :") ||
		strings.Contains(src, "config.middleware") ||
		(strings.Contains(src, "def initialize(app)") && strings.Contains(src, "def call(env)")) ||
		(strings.Contains(src, "def initialize(app)") && strings.Contains(src, "def call(@env)")) ||
		(strings.Contains(src, "def initialize( app") && strings.Contains(src, "def call( env"))
	if !hasMW {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Determine framework context from file content (heuristic).
	isRails := strings.Contains(src, "ActionController") ||
		strings.Contains(src, "ApplicationController") ||
		strings.Contains(src, "ActionDispatch") ||
		strings.Contains(src, "Rails.application")
	isGrape := strings.Contains(src, "Grape::API") || strings.Contains(src, "Grape::API::Instance")
	isSinatra := strings.Contains(src, "Sinatra::Base") || strings.Contains(src, "Sinatra::Application")
	isPadrino := strings.Contains(src, "Padrino::Application")
	isHanami := reMWHanamiApp.MatchString(src)
	isRoda := strings.Contains(src, "< Roda") || strings.Contains(src, "Roda.new")
	isCuba := strings.Contains(src, "Cuba.define") || strings.Contains(src, "Cuba.new")

	// ---- Rack `use MiddlewareName` (universal) ----
	for _, idx := range reMWRackUse.FindAllStringSubmatchIndex(src, -1) {
		mwName := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		fw := detectFramework(isRails, isGrape, isSinatra, isPadrino, isHanami, isRoda, isCuba)
		ent := makeEntity("rack_use:"+mwName, "SCOPE.Pattern", "middleware", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", fw,
			"provenance", "INFERRED_FROM_RACK_USE",
			"middleware_class", mwName,
		)
		add(ent)
	}

	// ---- config.middleware stack (use/insert_before/insert_after/swap/delete) ----
	// Emit one entity per operation, tagged with the operation name and an
	// order_position (1-based sequential index within the file) so consumers can
	// reconstruct the declaration order of the middleware stack.
	{
		allOps := reMWConfigOp.FindAllStringSubmatchIndex(src, -1)
		for pos, idx := range allOps {
			op := src[idx[2]:idx[3]]   // use|insert_before|insert_after|swap|delete
			cls1 := src[idx[4]:idx[5]] // first class token

			// For insert_before/insert_after/swap the second token (if present)
			// is the middleware being added; for use/delete the first token is.
			mwName := cls1
			anchorClass := ""
			if idx[6] >= 0 {
				// second token present — cls1 is the anchor, cls2 is the new MW
				anchorClass = cls1
				mwName = src[idx[6]:idx[7]]
			}

			ln := lineOf(src, idx[0])
			fw := "rails"
			if isPadrino {
				fw = "padrino"
			}

			name := "config_mw:" + mwName
			// For delete/swap with anchor, qualify name to allow multiple ops on same class.
			if op == "delete" {
				name = "config_mw_delete:" + mwName
			} else if (op == "insert_before" || op == "insert_after" || op == "swap") && anchorClass != "" {
				name = "config_mw_" + op + ":" + anchorClass + ":" + mwName
			}

			ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", fw,
				"provenance", "INFERRED_FROM_CONFIG_MIDDLEWARE_OP",
				"middleware_class", mwName,
				"middleware_op", op,
				"order_position", itoa(pos+1),
			)
			if anchorClass != "" {
				setProps(&ent, "anchor_class", anchorClass)
			}
			add(ent)
		}
	}

	// ---- Custom Rack middleware classes ----
	// A class is a Rack middleware if it defines both initialize(app) and call(env).
	if raMwInitApp.MatchString(src) && raMwCallEnv.MatchString(src) {
		for _, cidx := range raMwClassDef.FindAllStringSubmatchIndex(src, -1) {
			className := src[cidx[2]:cidx[3]]
			ln := lineOf(src, cidx[0])
			fw := detectFramework(isRails, isGrape, isSinatra, isPadrino, isHanami, isRoda, isCuba)
			name := "rack_middleware_class:" + className
			ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", fw,
				"provenance", "INFERRED_FROM_RACK_MIDDLEWARE_CLASS",
				"middleware_class", className,
				"rack_interface", "initialize(app)+call(env)",
			)
			add(ent)
		}
	}

	// ---- Rails filters (scoped: only/except) ----
	if isRails {
		for _, idx := range reMWRailsFilterScoped.FindAllStringSubmatchIndex(src, -1) {
			filterType := src[idx[2]:idx[3]]
			filterMethod := src[idx[4]:idx[5]]
			name := "rails_filter:" + filterType + ":" + filterMethod
			ln := lineOf(src, idx[0])
			ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, ln)
			props := []string{
				"framework", "rails",
				"provenance", "INFERRED_FROM_RAILS_FILTER_MW",
				"filter_type", filterType,
				"filter_method", filterMethod,
			}
			// Capture :only/:except scope when present (groups 3 and 4).
			if idx[6] >= 0 {
				scopeKind := src[idx[6]:idx[7]]
				scopeVal := ""
				if idx[8] >= 0 {
					scopeVal = src[idx[8]:idx[9]]
				}
				props = append(props, "filter_scope_kind", scopeKind, "filter_scope", scopeVal)
			}
			setProps(&ent, props...)
			add(ent)
		}
	}

	// ---- Grape hooks ----
	if isGrape {
		for _, idx := range reMWGrapeHook.FindAllStringSubmatchIndex(src, -1) {
			hookType := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("grape_hook:"+hookType, "SCOPE.Pattern", "middleware", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "grape",
				"provenance", "INFERRED_FROM_GRAPE_HOOK",
				"hook_type", hookType,
			)
			add(ent)
		}
	}

	// ---- Sinatra before/after blocks ----
	if isSinatra {
		for _, idx := range reMWSinatraBlock.FindAllStringSubmatchIndex(src, -1) {
			filterType := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("sinatra_filter:"+filterType, "SCOPE.Pattern", "middleware", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "sinatra",
				"provenance", "INFERRED_FROM_SINATRA_FILTER",
				"filter_type", filterType,
			)
			add(ent)
		}

		for _, idx := range reMWSinatraPathFilter.FindAllStringSubmatchIndex(src, -1) {
			filterType := src[idx[2]:idx[3]]
			path := src[idx[4]:idx[5]]
			ln := lineOf(src, idx[0])
			name := "sinatra_filter:" + filterType + ":" + path
			ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "sinatra",
				"provenance", "INFERRED_FROM_SINATRA_PATH_FILTER",
				"filter_type", filterType,
				"filter_path", path,
			)
			add(ent)
		}
	}

	// ---- Roda plugins ----
	if isRoda {
		for _, idx := range reMWRodaPlugin.FindAllStringSubmatchIndex(src, -1) {
			pluginName := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("roda_plugin:"+pluginName, "SCOPE.Pattern", "middleware", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "roda",
				"provenance", "INFERRED_FROM_RODA_PLUGIN",
				"plugin_name", pluginName,
			)
			add(ent)
		}
	}

	// ---- Cuba before/after ----
	if isCuba {
		for _, idx := range reMWCubaBlock.FindAllStringSubmatchIndex(src, -1) {
			filterType := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("cuba_filter:"+filterType, "SCOPE.Pattern", "middleware", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "cuba",
				"provenance", "INFERRED_FROM_CUBA_FILTER",
				"filter_type", filterType,
			)
			add(ent)
		}
	}

	// ---- Hanami middleware ----
	if isHanami {
		for _, idx := range reMWRackUse.FindAllStringSubmatchIndex(src, -1) {
			mwName := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			name := "hanami_mw:" + mwName
			ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "hanami",
				"provenance", "INFERRED_FROM_HANAMI_MIDDLEWARE",
				"middleware_class", mwName,
			)
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// detectFramework returns the most specific framework name based on boolean flags.
func detectFramework(isRails, isGrape, isSinatra, isPadrino, isHanami, isRoda, isCuba bool) string {
	switch {
	case isRails:
		return "rails"
	case isGrape:
		return "grape"
	case isSinatra:
		return "sinatra"
	case isPadrino:
		return "padrino"
	case isHanami:
		return "hanami"
	case isRoda:
		return "roda"
	case isCuba:
		return "cuba"
	default:
		return "rack"
	}
}
