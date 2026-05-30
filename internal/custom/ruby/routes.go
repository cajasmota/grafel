// routes.go — Ruby route-extraction + driver schema extractor.
//
// # Route extraction (route_extraction)
//
// Covers route_extraction for all 8 Ruby http_backend frameworks:
//
//	Rails     — config/routes.rb: get/post/put/patch/delete/resources (already
//	            covered in rails.go for endpoint_synthesis; this extractor emits
//	            dedicated route entities for the route_extraction capability)
//	Grape     — resource :name do / get :path / post :path inside an API class
//	Sinatra   — get '/path' do / post '/path' do verb blocks
//	Padrino   — get :name, :map => '/path' / post '...'
//	Hanami    — get '/path', to: 'controller#action'
//	Roda      — r.get 'path' / r.post 'path' routing-tree style
//	Cuba      — on('path') / get / post  (Rack-level routing DSL)
//	dry-rb    — HTTP.router.get(...) or pure Rack routing stubs
//
// # Driver schema (schema_extraction)
//
// Covers schema_extraction for Ruby driver/ORM records:
//
//	Mongoid     — field :name, type: String/Integer/...
//	Elasticsearch — mappings { properties { field { type '...' } } }  (both
//	              elasticsearch-model gem and raw index definitions)
//	ROM-rb      — schema(:table) { attribute :col, Types::... }
//	DataMapper  — property :name, String (already partially in activerecord.go;
//	              extended here with schema_extraction flip)
//	Sequel      — Sequel.migration { change { create_table :t { String :col } } }
//
// Drivers with no schema DSL (cassandra-driver, pg, mysql2, sqlite3, redis-rb,
// neo4j-ruby-driver, dynamodb-sdk) remain missing/not_applicable — not touched
// here.
//
// Detection is heuristic regex; all new cells are set to `partial`.
//
// Part of issue #3282.
package ruby

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("ruby_routes", &rubyRoutesExtractor{})
	extractor.Register("ruby_driver_schema", &rubyDriverSchemaExtractor{})
}

// ---------------------------------------------------------------------------
// Route extractor
// ---------------------------------------------------------------------------

type rubyRoutesExtractor struct{}

func (e *rubyRoutesExtractor) Language() string { return "ruby_routes" }

// ---------------------------------------------------------------------------
// Compiled regexes — Routes
// ---------------------------------------------------------------------------

var (
	// ---- Grape ----

	// resource :name do / resources :name do (Grape API resources block)
	rbGrapeResource = regexp.MustCompile(
		`(?m)^\s*resources?\s+:([a-z_]+)\s+do`,
	)

	// get 'path' / post 'path' / put 'path' inside a Grape API class
	// (also covers get :root, post :root)
	rbGrapeVerb = regexp.MustCompile(
		`(?m)^\s*(get|post|put|patch|delete|head|options)\s+['"]?([^'"\s,{]+)['"]?`,
	)

	// class FooAPI < Grape::API (heuristic to detect Grape files)
	rbGrapeAPIClass = regexp.MustCompile(
		`(?m)\bGrape::API\b`,
	)

	// prefix ':version' / version 'v1' (Grape versioning)
	rbGrapeVersion = regexp.MustCompile(
		`(?m)^\s*(?:version|prefix)\s+['"]([^'"]+)['"]`,
	)

	// ---- Sinatra ----

	// get '/path' do  /  post '/path' do
	rbSinatraVerb = regexp.MustCompile(
		`(?m)^\s*(get|post|put|patch|delete|head|options)\s+['"]([^'"]+)['"]\s+do`,
	)

	// Sinatra::Application / Sinatra::Base subclass
	rbSinatraBase = regexp.MustCompile(
		`(?m)\b(?:Sinatra::(?:Application|Base)|get\s*['"]|post\s*['"])\b`,
	)

	// ---- Padrino ----

	// get :name, :map => '/path'
	rbPadrinoVerbMap = regexp.MustCompile(
		`(?m)^\s*(get|post|put|patch|delete)\s+:([a-z_]+)(?:[^#\n]*:map\s*=>\s*['"]([^'"]+)['"])?`,
	)

	// Padrino::Application subclass detection
	rbPadrinoBase = regexp.MustCompile(
		`(?m)\bPadrino::Application\b`,
	)

	// ---- Hanami ----

	// get '/path', to: 'controller#action'  (Hanami 2.x router)
	rbHanamiVerb = regexp.MustCompile(
		`(?m)^\s*(get|post|put|patch|delete)\s+['"]([^'"]+)['"]`,
	)

	// Hanami::Routes / Hanami.application / router.get
	rbHanamiRouter = regexp.MustCompile(
		`(?m)\bHanami(?:::Routes|\.application)\b`,
	)

	// ---- Roda ----

	// r.get 'path' / r.post 'path'  (Roda routing tree, generic receiver name)
	rbRodaVerb = regexp.MustCompile(
		`(?m)\b([a-z_]+)\.(get|post|put|patch|delete)\s+['"]([^'"]+)['"]`,
	)

	// class MyApp < Roda
	rbRodaClass = regexp.MustCompile(
		`(?m)\bRoda\b`,
	)

	// ---- Cuba ----

	// on('path') / on(:id) / get { ... }
	rbCubaOn = regexp.MustCompile(
		`(?m)\bon\s*\(\s*['"]([^'"]+)['"]\s*\)`,
	)

	// Cuba.define / App = Cuba.new
	rbCubaDefine = regexp.MustCompile(
		`(?m)\bCuba\.(?:define|new)\b`,
	)

	// ---- dry-rb HTTP routing stubs ----

	// HTTP.router.get / router.add_route
	rbDryHTTPRouter = regexp.MustCompile(
		`(?m)\bHTTP\.router\.(get|post|put|patch|delete)\s+['"]([^'"]+)['"]`,
	)
)

// ---------------------------------------------------------------------------
// Extract — Routes
// ---------------------------------------------------------------------------

func (e *rubyRoutesExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.routes_extractor.extract",
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

	// Fast guard: skip files with no routing signal.
	hasRouting := strings.Contains(src, "get ") || strings.Contains(src, "post ") ||
		strings.Contains(src, "put ") || strings.Contains(src, "patch ") ||
		strings.Contains(src, "delete ") || strings.Contains(src, "Grape::API") ||
		strings.Contains(src, "Sinatra::") || strings.Contains(src, "Padrino::") ||
		strings.Contains(src, "Hanami::Routes") || strings.Contains(src, "Roda") ||
		strings.Contains(src, "Cuba.define") || strings.Contains(src, "HTTP.router") ||
		strings.Contains(src, "resources :")
	if !hasRouting {
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

	// ---- Grape ----
	if rbGrapeAPIClass.MatchString(src) {
		// resource blocks → expand to CRUD-like routes.
		for _, idx := range rbGrapeResource.FindAllStringSubmatchIndex(src, -1) {
			name := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("grape_resource:"+name, "SCOPE.Component", "", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "grape",
				"provenance", "INFERRED_FROM_GRAPE_RESOURCE",
				"resource", name,
			)
			add(ent)
		}

		// Individual verb routes.
		for _, idx := range rbGrapeVerb.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[idx[2]:idx[3]])
			path := src[idx[4]:idx[5]]
			ln := lineOf(src, idx[0])
			routeName := method + " " + path
			ent := makeEntity(routeName, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "grape",
				"provenance", "INFERRED_FROM_GRAPE_VERB",
				"http_method", method,
				"route_path", path,
			)
			add(ent)
		}
	}

	// ---- Sinatra ----
	if rbSinatraBase.MatchString(src) {
		for _, idx := range rbSinatraVerb.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[idx[2]:idx[3]])
			path := src[idx[4]:idx[5]]
			ln := lineOf(src, idx[0])
			routeName := method + " " + path
			ent := makeEntity(routeName, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "sinatra",
				"provenance", "INFERRED_FROM_SINATRA_VERB",
				"http_method", method,
				"route_path", path,
			)
			add(ent)
		}
	}

	// ---- Padrino ----
	if rbPadrinoBase.MatchString(src) {
		for _, idx := range rbPadrinoVerbMap.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[idx[2]:idx[3]])
			actionName := src[idx[4]:idx[5]]
			path := ""
			if idx[6] != -1 {
				path = src[idx[6]:idx[7]]
			} else {
				path = "/" + actionName
			}
			ln := lineOf(src, idx[0])
			routeName := fmt.Sprintf("%s %s", method, path)
			ent := makeEntity(routeName, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "padrino",
				"provenance", "INFERRED_FROM_PADRINO_VERB",
				"http_method", method,
				"route_path", path,
				"action_name", actionName,
			)
			add(ent)
		}
	}

	// ---- Hanami ----
	if rbHanamiRouter.MatchString(src) {
		for _, idx := range rbHanamiVerb.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[idx[2]:idx[3]])
			path := src[idx[4]:idx[5]]
			ln := lineOf(src, idx[0])
			routeName := method + " " + path
			ent := makeEntity(routeName, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "hanami",
				"provenance", "INFERRED_FROM_HANAMI_VERB",
				"http_method", method,
				"route_path", path,
			)
			add(ent)
		}
	}

	// ---- Roda ----
	if rbRodaClass.MatchString(src) {
		for _, idx := range rbRodaVerb.FindAllStringSubmatchIndex(src, -1) {
			method := strings.ToUpper(src[idx[4]:idx[5]])
			path := src[idx[6]:idx[7]]
			ln := lineOf(src, idx[0])
			routeName := method + " " + path
			ent := makeEntity(routeName, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "roda",
				"provenance", "INFERRED_FROM_RODA_VERB",
				"http_method", method,
				"route_path", path,
			)
			add(ent)
		}
	}

	// ---- Cuba ----
	if rbCubaDefine.MatchString(src) {
		for _, idx := range rbCubaOn.FindAllStringSubmatchIndex(src, -1) {
			path := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("cuba_on:"+path, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "cuba",
				"provenance", "INFERRED_FROM_CUBA_ON",
				"route_path", path,
			)
			add(ent)
		}
	}

	// ---- dry-rb HTTP router ----
	for _, idx := range rbDryHTTPRouter.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[idx[2]:idx[3]])
		path := src[idx[4]:idx[5]]
		ln := lineOf(src, idx[0])
		routeName := method + " " + path
		ent := makeEntity(routeName, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "dry-rb",
			"provenance", "INFERRED_FROM_DRY_HTTP_ROUTER",
			"http_method", method,
			"route_path", path,
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Driver schema extractor
// ---------------------------------------------------------------------------

type rubyDriverSchemaExtractor struct{}

func (e *rubyDriverSchemaExtractor) Language() string { return "ruby_driver_schema" }

// ---------------------------------------------------------------------------
// Compiled regexes — Driver Schema
// ---------------------------------------------------------------------------

var (
	// ---- Mongoid ----

	// include Mongoid::Document
	rbMongoidDocument = regexp.MustCompile(
		`(?m)\bMongoid::Document\b`,
	)

	// field :name, type: String / field :count, type: Integer
	rbMongoidField = regexp.MustCompile(
		`(?m)^\s*field\s+:([a-z_]+)(?:[^#\n]*type:\s*([A-Za-z:]+))?`,
	)

	// embeds_one / embeds_many / embedded_in (Mongoid embedding)
	rbMongoidEmbed = regexp.MustCompile(
		`(?m)^\s*(embeds_one|embeds_many|embedded_in|has_many|belongs_to|has_one)\s+:([a-z_]+)`,
	)

	// ---- Elasticsearch (elasticsearch-model gem) ----

	// include Elasticsearch::Model
	rbESModelInclude = regexp.MustCompile(
		`(?m)\bElasticsearch::Model\b`,
	)

	// mappings do / mappings dynamic: 'false' do
	rbESMappings = regexp.MustCompile(
		`(?m)^\s*mappings\b[^#\n]*\bdo\b`,
	)

	// indexes :field_name, type: 'text' / indexes :field_name, type: :keyword
	rbESIndexes = regexp.MustCompile(
		`(?m)^\s*indexes\s+:([a-z_]+)(?:[^#\n]*type:\s*['":?]([a-z_]+)['"]?)?`,
	)

	// Raw ES index creation: client.indices.create(index: 'name', body: { mappings: ... })
	rbESCreateIndex = regexp.MustCompile(
		`(?m)\bclient\.indices\.create\s*\(`,
	)

	// ---- ROM-rb schema ----

	// schema(:table) { attribute :col, Types::... }
	rbROMSchema = regexp.MustCompile(
		`(?m)^\s*schema\s*\(\s*:([a-z_]+)`,
	)

	rbROMAttribute = regexp.MustCompile(
		`(?m)^\s*attribute\s+:([a-z_]+),\s*Types::([A-Za-z:]+)`,
	)

	// ---- Sequel schema ----

	// Sequel.migration { change { create_table :t { Integer :id; String :name } } }
	// Or DB.create_table :t do ...
	rbSequelCreateTable = regexp.MustCompile(
		`(?m)^\s*create_table\s+:([a-z_]+)`,
	)

	// Inside a Sequel create_table block: Integer :col / String :col
	rbSequelColumnType = regexp.MustCompile(
		`(?m)^\s*(Integer|String|Float|BigDecimal|Date|DateTime|Time|File|TrueClass|Fixnum|Bignum|Numeric)\s+:([a-z_]+)`,
	)
)

// ---------------------------------------------------------------------------
// Extract — Driver Schema
// ---------------------------------------------------------------------------

func (e *rubyDriverSchemaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.driver_schema_extractor.extract",
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

	// Fast guard: skip files with no schema-relevant driver tokens.
	hasSchema := strings.Contains(src, "Mongoid::Document") ||
		strings.Contains(src, "Elasticsearch::Model") ||
		strings.Contains(src, "mappings") ||
		strings.Contains(src, "indices.create") ||
		(strings.Contains(src, "ROM::") && strings.Contains(src, "schema(")) ||
		(strings.Contains(src, "Sequel") && strings.Contains(src, "create_table")) ||
		strings.Contains(src, "field :")
	if !hasSchema {
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

	// ---- Mongoid ----
	if rbMongoidDocument.MatchString(src) {
		for _, idx := range rbMongoidField.FindAllStringSubmatchIndex(src, -1) {
			fieldName := src[idx[2]:idx[3]]
			fieldType := ""
			if idx[4] != -1 {
				fieldType = src[idx[4]:idx[5]]
			}
			ln := lineOf(src, idx[0])
			ent := makeEntity("mongoid_field:"+fieldName, "SCOPE.Schema", "column", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "mongoid",
				"provenance", "INFERRED_FROM_MONGOID_FIELD",
				"field_name", fieldName,
				"field_type", fieldType,
			)
			add(ent)
		}

		for _, idx := range rbMongoidEmbed.FindAllStringSubmatchIndex(src, -1) {
			macro := src[idx[2]:idx[3]]
			target := src[idx[4]:idx[5]]
			ln := lineOf(src, idx[0])
			name := fmt.Sprintf("mongoid_%s:%s", macro, target)
			ent := makeEntity(name, "SCOPE.Pattern", "relation", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "mongoid",
				"provenance", "INFERRED_FROM_MONGOID_EMBED",
				"association_type", macro,
				"association_name", target,
			)
			add(ent)
		}
	}

	// ---- Elasticsearch ----
	if rbESModelInclude.MatchString(src) || rbESMappings.MatchString(src) || rbESCreateIndex.MatchString(src) {
		// Emit one entity for the mapping definition.
		if rbESMappings.MatchString(src) {
			loc := rbESMappings.FindStringIndex(src)
			ln := lineOf(src, loc[0])
			ent := makeEntity("es_mappings", "SCOPE.Schema", "table", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "elasticsearch",
				"provenance", "INFERRED_FROM_ES_MAPPINGS",
			)
			add(ent)
		}

		// indexes :field_name → column entities.
		for _, idx := range rbESIndexes.FindAllStringSubmatchIndex(src, -1) {
			fieldName := src[idx[2]:idx[3]]
			fieldType := ""
			if idx[4] != -1 {
				fieldType = src[idx[4]:idx[5]]
			}
			ln := lineOf(src, idx[0])
			ent := makeEntity("es_index:"+fieldName, "SCOPE.Schema", "column", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "elasticsearch",
				"provenance", "INFERRED_FROM_ES_INDEXES",
				"field_name", fieldName,
				"field_type", fieldType,
			)
			add(ent)
		}
	}

	// ---- ROM-rb ----
	if strings.Contains(src, "ROM::") {
		for _, idx := range rbROMSchema.FindAllStringSubmatchIndex(src, -1) {
			tableName := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("rom_schema:"+tableName, "SCOPE.Schema", "table", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "rom-rb",
				"provenance", "INFERRED_FROM_ROM_SCHEMA",
				"table_name", tableName,
			)
			add(ent)
		}

		for _, idx := range rbROMAttribute.FindAllStringSubmatchIndex(src, -1) {
			colName := src[idx[2]:idx[3]]
			colType := src[idx[4]:idx[5]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("rom_attr:"+colName, "SCOPE.Schema", "column", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "rom-rb",
				"provenance", "INFERRED_FROM_ROM_ATTRIBUTE",
				"column_name", colName,
				"column_type", colType,
			)
			add(ent)
		}
	}

	// ---- Sequel ----
	if strings.Contains(src, "Sequel") || strings.Contains(src, "DB.") {
		for _, idx := range rbSequelCreateTable.FindAllStringSubmatchIndex(src, -1) {
			tableName := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("sequel_table:"+tableName, "SCOPE.Schema", "table", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "sequel",
				"provenance", "INFERRED_FROM_SEQUEL_CREATE_TABLE",
				"table_name", tableName,
			)
			add(ent)
		}

		for _, idx := range rbSequelColumnType.FindAllStringSubmatchIndex(src, -1) {
			colType := src[idx[2]:idx[3]]
			colName := src[idx[4]:idx[5]]
			ln := lineOf(src, idx[0])
			ent := makeEntity("sequel_col:"+colName, "SCOPE.Schema", "column", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "sequel",
				"provenance", "INFERRED_FROM_SEQUEL_COLUMN",
				"column_name", colName,
				"column_type", colType,
			)
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
