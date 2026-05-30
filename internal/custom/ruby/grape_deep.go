// grape_deep.go — Deep Grape routing, param-validation, auth, and testing extraction.
//
// This extractor deepens the Grape-specific capabilities beyond what the
// heuristic passes in routes.go, validation.go, auth.go, and middleware.go
// already provide.  Specifically:
//
//	Routing (route_extraction → full):
//	  - Full path composition: namespace/group/segment nesting stacks the path
//	    prefix; resource/resources adds the resource name as a prefix segment.
//	  - route_param :id detection (adds /:param to the current path context).
//	  - mount API::X → emits a mount_point entity with the target module.
//	  - Verb entities carry the fully-resolved path in resolved_path property.
//
//	Validation / DTO (dto_extraction + request_validation → full):
//	  - Per-param entities now carry: qualifier (requires|optional), type,
//	    values constraint, regexp constraint, allow_blank, and default.
//	  - Each verb block's params do...end is linked to the enclosing endpoint
//	    via endpoint_path / endpoint_method properties.
//
//	Auth (auth_coverage → partial, documented Grape-native patterns):
//	  - http_basic_auth + http_digest_auth blocks (Grape::Middleware::Auth::Base).
//	  - before { authenticate! } / before { error!('401', 401) unless current_user }.
//	  - helpers { def authenticate! ... } helper block detection.
//
//	Testing (tests_linkage → partial):
//	  - rack-test integration: `include Rack::Test::Methods`, `def app`, Grape API mount.
//
// Part of issue #3345.
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
	extractor.Register("ruby_grape_deep", &grapeDeepExtractor{})
}

type grapeDeepExtractor struct{}

func (e *grapeDeepExtractor) Language() string { return "ruby_grape_deep" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// ---- Grape API class detection ----

	// class Foo < Grape::API  or  class Foo < Grape::API::Instance
	raGrapeAPIClassRe = regexp.MustCompile(
		`(?m)\bGrape::API(?:::Instance)?\b`,
	)

	// ---- Routing: scope/namespace/group/segment DSL ----

	// namespace :name do / namespace '/path' do / group :name do / segment :name do
	// Captures: (1) DSL keyword, (2) the path/symbol token (without leading colon).
	raGrapeNamespaceRe = regexp.MustCompile(
		`^\s*(namespace|group|segment)\s+['":?]?([A-Za-z0-9_/:-]+)['"]?\s+do\s*$`,
	)

	// resource :name do / resources :name do
	// Captures: (1) name symbol.
	raGrapeResourceRe = regexp.MustCompile(
		`^\s*resources?\s+:([A-Za-z0-9_]+)\s+do\s*$`,
	)

	// route_param :id do / route_param :slug do
	// Captures: (1) param name.
	raGrapeRouteParamRe = regexp.MustCompile(
		`^\s*route_param\s+:([A-Za-z0-9_]+)\s+do\s*$`,
	)

	// end — exact end line.
	raGrapeEndRe = regexp.MustCompile(`^\s*end\s*$`)

	// class/module declaration line — pushes non-path block.
	raGrapeClassDeclRe = regexp.MustCompile(`^\s*(?:class|module)\s+`)

	// helpers do / helpers { — non-path block.
	raGrapeHelpersDeclRe = regexp.MustCompile(`^\s*helpers\s+(?:do|\{)\s*$`)

	// params do — non-path block.
	raGrapeParamsDeclRe = regexp.MustCompile(`^\s*params\s+do\b`)

	// ---- Routing: verb lines (with optional `do` at end) ----

	// get 'path' do / post 'path' do / etc.
	// Also covers bare: get do (no path)
	// Captures: (1) method, (2) optional quoted path, (3) optional symbol path.
	raGrapeVerbRe = regexp.MustCompile(
		`^\s*(get|post|put|patch|delete|head|options)\s+(?:['"]([^'"]+)['"]|:([A-Za-z0-9_]+))?\s*(?:do\b.*)?$`,
	)

	// verb line with `do` at end — means it opens a block.
	raGrapeVerbDoDeclRe = regexp.MustCompile(
		`^\s*(?:get|post|put|patch|delete|head|options)\b.*\bdo\b`,
	)

	// ---- Routing: mount ----

	// mount API::V1 => '/api'  /  mount API::V1, at: '/api'  /  mount SomeAPI
	// Captures: (1) module path, (2) optional mount point.
	raGrapeMountRe = regexp.MustCompile(
		`(?m)^\s*mount\s+([A-Z][A-Za-z0-9_:]+)(?:\s*=>\s*['"]([^'"]+)['"]|\s*,\s*at:\s*['"]([^'"]+)['"])?`,
	)

	// ---- Validation: params block and per-param lines ----

	// params do
	raGrapeParamsBlockRe = regexp.MustCompile(`(?m)^\s*params\s+do\b`)

	// requires :field[, type: SomeType[, values: [...]][, regexp: /.../ ]]
	// optional :field[, type: SomeType[, default: val]]
	// Captures: (1) qualifier (requires|optional), (2) field name.
	raGrapeParamLineRe = regexp.MustCompile(
		`(?m)^\s*(requires|optional)\s+:([A-Za-z0-9_]+)([^\n]*)`,
	)

	// type: SomeType  — within a param line tail.
	raGrapeParamTypeRe = regexp.MustCompile(
		`\btype:\s*([A-Za-z:][A-Za-z0-9_:.]*)`,
	)

	// values: [:a, :b] / values: 1..10  — constraint.
	raGrapeParamValuesRe = regexp.MustCompile(
		`\bvalues:\s*([^\s,)]+)`,
	)

	// regexp: /.../ — validation constraint.
	raGrapeParamRegexpRe = regexp.MustCompile(
		`\bregexp:\s*(\/[^/]+\/[imxo]*)`,
	)

	// default: val — default value.
	raGrapeParamDefaultRe = regexp.MustCompile(
		`\bdefault:\s*([^\s,)]+)`,
	)

	// allow_blank: true/false
	raGrapeParamAllowBlankRe = regexp.MustCompile(
		`\ballow_blank:\s*(true|false)`,
	)

	// ---- Auth: Grape-native patterns ----

	// http_basic_auth { |u, p| ... }  or  http_basic_auth do |u, p| ... end
	raGrapeHTTPBasicRe = regexp.MustCompile(
		`(?m)\bhttp_basic_auth\b`,
	)

	// http_digest_auth { ... }
	raGrapeHTTPDigestRe = regexp.MustCompile(
		`(?m)\bhttp_digest_auth\b`,
	)

	// before { authenticate! } / before { ... unless current_user }
	// Matches inline and block forms.  Note: `!` is not a \w char so we avoid
	// \b after it and instead match `authenticate!` literally.
	raGrapeBeforeAuthRe = regexp.MustCompile(
		`(?m)\bbefore[^#\n]*authenticate!|\bbefore[^#\n]*current_user`,
	)

	// error!('Unauthorized', 401) inside a before block — explicit auth guard.
	raGrapeErrorAuthRe = regexp.MustCompile(
		`(?m)\berror!\s*\(['"](Unauthorized|401|Forbidden|403)['"]\s*,\s*40[13]\)`,
	)

	// helpers do ... def authenticate! ... end / helpers { ... }
	raGrapeHelpersBlockRe = regexp.MustCompile(
		`(?m)^\s*helpers\s+(?:do|\{)\b`,
	)

	// def authenticate! inside a helpers block.
	// Note: `!` is not a \w char so \b after it fails in RE2; match literally.
	raGrapeAuthHelperDefRe = regexp.MustCompile(
		`(?m)\bdef\s+authenticate!`,
	)

	// ---- Testing: rack-test ----

	// include Rack::Test::Methods
	raGrapeRackTestIncRe = regexp.MustCompile(
		`(?m)\binclude\s+Rack::Test::Methods\b`,
	)

	// def app ... SomeAPI ... end — rack-test app method.
	raGrapeRackTestAppRe = regexp.MustCompile(
		`(?m)\bdef\s+app\b`,
	)

	// RSpec.describe SomeAPI / describe SomeAPI (Grape API class as subject).
	raGrapeRSpecDescribeRe = regexp.MustCompile(
		`(?m)\bdescribe\s+([A-Z][A-Za-z0-9_:]+)\b[^#\n]*Grape::API|RSpec\.describe\s+([A-Z][A-Za-z0-9_:]+)\b`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *grapeDeepExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.grape_deep_extractor.extract",
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

	// Fast guard: must be a Grape file or a rack-test spec for a Grape API.
	isGrapeAPI := raGrapeAPIClassRe.MatchString(src)
	isGrapeTest := raGrapeRackTestIncRe.MatchString(src) && raGrapeRackTestAppRe.MatchString(src)
	if !isGrapeAPI && !isGrapeTest {
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

	if isGrapeAPI {
		grapeExtractRouting(src, file.Path, add)
		grapeExtractValidation(src, file.Path, add)
		grapeExtractAuth(src, file.Path, add)
	}

	if isGrapeTest {
		grapeExtractTesting(src, file.Path, add)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Routing — full path composition
// ---------------------------------------------------------------------------

// blockKind categorises the kind of `do`-block opened on a line so the stack
// knows whether to pop pathStack when the matching `end` arrives.
type blockKind int

const (
	bkPath    blockKind = iota // namespace / resource / route_param → advances path
	bkNonPath                  // everything else: class, helpers, params, verb body, etc.
)

// grapeExtractRouting walks the source line-by-line, tracking `do`/`end`
// balance to maintain a path-prefix stack.  Only namespace/resource/route_param
// blocks advance the path; verb bodies, helpers, params, and class declarations
// push a bkNonPath sentinel so their `end`s are correctly paired without
// affecting the path.
func grapeExtractRouting(src, fp string, add func(types.EntityRecord)) {
	lines := strings.Split(src, "\n")

	// pathStack[0] is always ""; additional elements are path segments pushed by
	// namespace/resource/route_param blocks.
	pathStack := []string{""}
	// blockStack mirrors the do/end nesting.  pathStack only grows when a bkPath
	// entry is pushed; it shrinks when that entry is popped.
	blockStack := []blockKind{bkNonPath} // bottom: the class body

	currentPath := func() string {
		var segs []string
		for _, s := range pathStack {
			if s != "" {
				segs = append(segs, strings.TrimPrefix(s, "/"))
			}
		}
		if len(segs) == 0 {
			return "/"
		}
		return "/" + strings.Join(segs, "/")
	}

	for i, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		ln := i + 1

		if trimmed == "" {
			continue
		}

		// ---- end: pop the innermost block ----
		if raGrapeEndRe.MatchString(trimmed) {
			if len(blockStack) > 1 {
				bk := blockStack[len(blockStack)-1]
				blockStack = blockStack[:len(blockStack)-1]
				if bk == bkPath && len(pathStack) > 1 {
					pathStack = pathStack[:len(pathStack)-1]
				}
			}
			continue
		}

		// ---- class / module declaration: push non-path block ----
		if raGrapeClassDeclRe.MatchString(trimmed) {
			blockStack = append(blockStack, bkNonPath)
			continue
		}

		// ---- helpers do / helpers { : non-path block ----
		if raGrapeHelpersDeclRe.MatchString(trimmed) {
			blockStack = append(blockStack, bkNonPath)
			continue
		}

		// ---- params do: non-path block ----
		if raGrapeParamsDeclRe.MatchString(trimmed) {
			blockStack = append(blockStack, bkNonPath)
			continue
		}

		// ---- namespace / group / segment :name do → path block ----
		if m := raGrapeNamespaceRe.FindStringSubmatch(trimmed); m != nil {
			seg := m[2]
			seg = strings.Trim(seg, `'"`)
			if !strings.HasPrefix(seg, "/") {
				seg = "/" + seg
			}
			pathStack = append(pathStack, seg)
			blockStack = append(blockStack, bkPath)
			continue
		}

		// ---- resource / resources :name do → path block ----
		if m := raGrapeResourceRe.FindStringSubmatch(trimmed); m != nil {
			seg := "/" + m[1]
			pathStack = append(pathStack, seg)
			blockStack = append(blockStack, bkPath)
			continue
		}

		// ---- route_param :id do → path block ----
		if m := raGrapeRouteParamRe.FindStringSubmatch(trimmed); m != nil {
			paramName := m[1]
			seg := "/:" + paramName
			pathStack = append(pathStack, seg)
			blockStack = append(blockStack, bkPath)

			// Emit a route_param entity.
			ent := makeEntity(
				"grape_route_param:"+paramName,
				"SCOPE.Component",
				"route_param",
				fp, "ruby", ln,
			)
			setProps(&ent,
				"framework", "grape",
				"provenance", "INFERRED_FROM_GRAPE_ROUTE_PARAM",
				"param_name", paramName,
				"path_segment", "/:"+paramName,
				"resolved_path", currentPath(),
			)
			add(ent)
			continue
		}

		// ---- verb lines ----
		if m := raGrapeVerbRe.FindStringSubmatch(trimmed); m != nil {
			method := strings.ToUpper(m[1])
			var verbPath string
			if m[2] != "" {
				verbPath = m[2]
			} else if m[3] != "" {
				verbPath = ":" + m[3]
			}

			base := currentPath()
			var resolvedPath string
			switch {
			case verbPath == "" || verbPath == "/":
				resolvedPath = base
			case strings.HasPrefix(verbPath, "/"):
				resolvedPath = strings.TrimRight(base, "/") + verbPath
			default:
				resolvedPath = strings.TrimRight(base, "/") + "/" + verbPath
			}

			name := method + " " + resolvedPath
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", fp, "ruby", ln)
			setProps(&ent,
				"framework", "grape",
				"provenance", "INFERRED_FROM_GRAPE_VERB_DEEP",
				"http_method", method,
				"route_path", verbPath,
				"resolved_path", resolvedPath,
			)
			add(ent)

			// If this verb line opens a do-block, push a non-path block so its
			// `end` is correctly consumed.
			if raGrapeVerbDoDeclRe.MatchString(trimmed) {
				blockStack = append(blockStack, bkNonPath)
			}
			continue
		}

		// ---- mount API::X ----
		if m := raGrapeMountRe.FindStringSubmatch(trimmed); m != nil {
			target := m[1]
			mountAt := m[2]
			if mountAt == "" {
				mountAt = m[3]
			}
			if mountAt == "" {
				parts := strings.Split(target, "::")
				last := parts[len(parts)-1]
				mountAt = "/" + camelToSnake(last)
			}
			name := "grape_mount:" + target
			ent := makeEntity(name, "SCOPE.Component", "mount_point", fp, "ruby", ln)
			setProps(&ent,
				"framework", "grape",
				"provenance", "INFERRED_FROM_GRAPE_MOUNT",
				"mount_target", target,
				"mount_at", mountAt,
			)
			add(ent)
			continue
		}

		// Any other line containing `do` at the end opens a non-path block.
		if strings.HasSuffix(trimmed, " do") || strings.HasSuffix(trimmed, "\tdo") ||
			trimmed == "do" || strings.Contains(trimmed, " do |") {
			blockStack = append(blockStack, bkNonPath)
		}
	}
}

// ---------------------------------------------------------------------------
// Validation — deep per-param extraction with type + constraints
// ---------------------------------------------------------------------------

func grapeExtractValidation(src, fp string, add func(types.EntityRecord)) {
	// Find all params do...end blocks; for each, extract per-param entities with
	// full property set: qualifier, type, values, regexp, default, allow_blank.
	// We also emit one block-level entity linking to the nearest preceding verb.

	// Find each params block start.
	for _, blockIdx := range raGrapeParamsBlockRe.FindAllStringIndex(src, -1) {
		blockStart := blockIdx[0]
		ln := lineOf(src, blockStart)

		// Determine the enclosing endpoint by scanning backwards for the nearest verb.
		beforeBlock := src[:blockStart]
		endpointPath := ""
		endpointMethod := ""
		if vIdx := raGrapeVerbRe.FindAllStringSubmatchIndex(beforeBlock, -1); len(vIdx) > 0 {
			last := vIdx[len(vIdx)-1]
			endpointMethod = strings.ToUpper(beforeBlock[last[2]:last[3]])
			if last[4] != -1 {
				endpointPath = beforeBlock[last[4]:last[5]]
			}
		}

		// Emit a block-scope entity for this params block.
		blockName := fmt.Sprintf("grape_params_block@L%d", ln)
		blockEnt := makeEntity(blockName, "SCOPE.Pattern", "request_validation", fp, "ruby", ln)
		setProps(&blockEnt,
			"framework", "grape",
			"provenance", "INFERRED_FROM_GRAPE_PARAMS_BLOCK_DEEP",
			"signal", "validation",
			"endpoint_method", endpointMethod,
			"endpoint_path", endpointPath,
		)
		add(blockEnt)

		// Extract the block body: everything from blockStart to the matching `end`.
		// We approximate the block body as up to 60 lines after `params do`.
		blockEnd := blockStart
		depth := 0
		afterBlock := src[blockStart:]
		for j, ch := range afterBlock {
			if ch == '\n' {
				lineSnip := ""
				// Get the current line.
				nlIdx := strings.Index(afterBlock[j+1:], "\n")
				if nlIdx == -1 {
					lineSnip = afterBlock[j+1:]
				} else {
					lineSnip = afterBlock[j+1 : j+1+nlIdx]
				}
				t := strings.TrimSpace(lineSnip)
				if strings.HasSuffix(t, " do") || strings.HasSuffix(t, " do\r") ||
					t == "do" || strings.Contains(t, "do |") {
					depth++
				}
				if t == "end" || t == "end\r" {
					if depth == 0 {
						blockEnd = blockStart + j
						break
					}
					depth--
				}
			}
		}
		// Include everything between `params do` and the matching `end`.
		var blockSrc string
		if blockEnd > blockStart {
			blockSrc = src[blockStart:blockEnd]
		} else {
			// Fallback: just scan 2000 chars ahead.
			end := blockStart + 2000
			if end > len(src) {
				end = len(src)
			}
			blockSrc = src[blockStart:end]
		}

		// Extract per-param entities from the block.
		for _, idx := range raGrapeParamLineRe.FindAllStringSubmatchIndex(blockSrc, -1) {
			qualifier := blockSrc[idx[2]:idx[3]]
			field := blockSrc[idx[4]:idx[5]]
			tail := blockSrc[idx[6]:idx[7]]
			paramLn := lineOf(src, blockStart+idx[0])

			// Parse type.
			paramType := ""
			if tm := raGrapeParamTypeRe.FindStringSubmatch(tail); tm != nil {
				paramType = tm[1]
			}

			// Parse values constraint.
			valuesConstraint := ""
			if vm := raGrapeParamValuesRe.FindStringSubmatch(tail); vm != nil {
				valuesConstraint = vm[1]
			}

			// Parse regexp constraint.
			regexpConstraint := ""
			if rm := raGrapeParamRegexpRe.FindStringSubmatch(tail); rm != nil {
				regexpConstraint = rm[1]
			}

			// Parse default.
			defaultVal := ""
			if dm := raGrapeParamDefaultRe.FindStringSubmatch(tail); dm != nil {
				defaultVal = dm[1]
			}

			// Parse allow_blank.
			allowBlank := ""
			if am := raGrapeParamAllowBlankRe.FindStringSubmatch(tail); am != nil {
				allowBlank = am[1]
			}

			name := fmt.Sprintf("grape_param:%s:%s", qualifier, field)
			ent := makeEntity(name, "SCOPE.Schema", "dto_field", fp, "ruby", paramLn)
			setProps(&ent,
				"framework", "grape",
				"provenance", "INFERRED_FROM_GRAPE_PARAM_DEEP",
				"signal", "dto",
				"qualifier", qualifier,
				"field", field,
				"endpoint_method", endpointMethod,
				"endpoint_path", endpointPath,
			)
			if paramType != "" {
				setProps(&ent, "param_type", paramType)
			}
			if valuesConstraint != "" {
				setProps(&ent, "values_constraint", valuesConstraint)
			}
			if regexpConstraint != "" {
				setProps(&ent, "regexp_constraint", regexpConstraint)
			}
			if defaultVal != "" {
				setProps(&ent, "default_value", defaultVal)
			}
			if allowBlank != "" {
				setProps(&ent, "allow_blank", allowBlank)
			}
			add(ent)
		}
	}
}

// ---------------------------------------------------------------------------
// Auth — Grape-native patterns
// ---------------------------------------------------------------------------

func grapeExtractAuth(src, fp string, add func(types.EntityRecord)) {
	// http_basic_auth
	if raGrapeHTTPBasicRe.MatchString(src) {
		loc := raGrapeHTTPBasicRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		ent := makeEntity("grape_http_basic_auth", "SCOPE.Pattern", "auth_guard", fp, "ruby", ln)
		setProps(&ent,
			"signal", "auth",
			"library", "grape",
			"kind", "http_basic",
			"mechanism", "http_basic_auth",
			"auth_required", "true",
			"framework", "grape",
			"provenance", "INFERRED_FROM_GRAPE_HTTP_BASIC",
		)
		add(ent)
	}

	// http_digest_auth
	if raGrapeHTTPDigestRe.MatchString(src) {
		loc := raGrapeHTTPDigestRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		ent := makeEntity("grape_http_digest_auth", "SCOPE.Pattern", "auth_guard", fp, "ruby", ln)
		setProps(&ent,
			"signal", "auth",
			"library", "grape",
			"kind", "http_digest",
			"mechanism", "http_digest_auth",
			"auth_required", "true",
			"framework", "grape",
			"provenance", "INFERRED_FROM_GRAPE_HTTP_DIGEST",
		)
		add(ent)
	}

	// before { authenticate! } / before do ... authenticate! ... end
	for _, idx := range raGrapeBeforeAuthRe.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		ent := makeEntity("grape_before_authenticate", "SCOPE.Pattern", "auth_guard", fp, "ruby", ln)
		setProps(&ent,
			"signal", "auth",
			"library", "grape",
			"kind", "before_hook",
			"mechanism", "before_authenticate",
			"auth_required", "true",
			"framework", "grape",
			"provenance", "INFERRED_FROM_GRAPE_BEFORE_AUTH",
		)
		add(ent)
		break // emit once per file; not per occurrence
	}

	// error!('Unauthorized', 401) — explicit auth rejection.
	if raGrapeErrorAuthRe.MatchString(src) {
		loc := raGrapeErrorAuthRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		ent := makeEntity("grape_error_unauthorized", "SCOPE.Pattern", "auth_guard", fp, "ruby", ln)
		setProps(&ent,
			"signal", "auth",
			"library", "grape",
			"kind", "error_guard",
			"mechanism", "error_unauthorized",
			"auth_required", "true",
			"framework", "grape",
			"provenance", "INFERRED_FROM_GRAPE_ERROR_AUTH",
		)
		add(ent)
	}

	// helpers { def authenticate! ... } — helper method definition.
	if raGrapeHelpersBlockRe.MatchString(src) && raGrapeAuthHelperDefRe.MatchString(src) {
		loc := raGrapeAuthHelperDefRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		ent := makeEntity("grape_authenticate_helper", "SCOPE.Pattern", "auth_helper", fp, "ruby", ln)
		setProps(&ent,
			"signal", "auth",
			"library", "grape",
			"kind", "helper_definition",
			"mechanism", "helpers_authenticate",
			"framework", "grape",
			"provenance", "INFERRED_FROM_GRAPE_AUTH_HELPER",
		)
		add(ent)
	}
}

// ---------------------------------------------------------------------------
// Testing — rack-test linkage
// ---------------------------------------------------------------------------

func grapeExtractTesting(src, fp string, add func(types.EntityRecord)) {
	// include Rack::Test::Methods — primary rack-test signal.
	if raGrapeRackTestIncRe.MatchString(src) {
		loc := raGrapeRackTestIncRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		ent := makeEntity("rack_test_methods", "SCOPE.Pattern", "test_linkage", fp, "ruby", ln)
		setProps(&ent,
			"framework", "grape",
			"library", "rack_test",
			"provenance", "INFERRED_FROM_RACK_TEST_INCLUDE",
			"signal", "testing",
		)
		add(ent)
	}

	// def app — rack-test app method definition.
	if raGrapeRackTestAppRe.MatchString(src) {
		loc := raGrapeRackTestAppRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		ent := makeEntity("rack_test_app", "SCOPE.Pattern", "test_linkage", fp, "ruby", ln)
		setProps(&ent,
			"framework", "grape",
			"library", "rack_test",
			"provenance", "INFERRED_FROM_RACK_TEST_APP",
			"signal", "testing",
		)
		add(ent)
	}

	// RSpec.describe SomeGrapeAPI ...
	for _, idx := range raGrapeRSpecDescribeRe.FindAllStringSubmatchIndex(src, -1) {
		apiName := ""
		if idx[2] != -1 {
			apiName = src[idx[2]:idx[3]]
		} else if idx[4] != -1 {
			apiName = src[idx[4]:idx[5]]
		}
		if apiName == "" {
			continue
		}
		ln := lineOf(src, idx[0])
		ent := makeEntity("grape_spec_describe:"+apiName, "SCOPE.Pattern", "test_linkage", fp, "ruby", ln)
		setProps(&ent,
			"framework", "grape",
			"library", "rspec",
			"provenance", "INFERRED_FROM_GRAPE_RSPEC_DESCRIBE",
			"signal", "testing",
			"api_class", apiName,
		)
		add(ent)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// leadingSpaces returns the number of leading space characters (each tab = 1).
func leadingSpaces(s string) int {
	n := 0
	for _, ch := range s {
		if ch == ' ' || ch == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

// camelToSnake converts CamelCase to snake_case for mount-at heuristic.
// e.g. "UsersAPI" → "users_api"
func camelToSnake(s string) string {
	var b strings.Builder
	for i, ch := range s {
		if ch >= 'A' && ch <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(ch | 0x20) // to lower
		} else {
			b.WriteRune(ch)
		}
	}
	return b.String()
}
