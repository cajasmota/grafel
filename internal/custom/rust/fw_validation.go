package rust

// fw_validation.go — DTO + request validation extractors for Rust HTTP frameworks.
//
// Covers dto_extraction and request_validation for:
//   - actix, axum, gotham, hyper, poem, rocket, salvo, tide, tower, warp
//
// Detection surface:
//   dto_extraction:
//     - #[derive(Deserialize)] structs (serde — the lingua franca for HTTP DTOs)
//     - #[derive(Validate)] structs (validator crate)
//     - #[derive(serde::Deserialize)] full-path variant
//
//   request_validation:
//     - #[validate(...)] field attributes (validator crate field-level rules)
//     - validator::Validate usage (trait impl or .validate() call)
//     - actix: web::Json<T>, web::Query<T>, web::Form<T> extractor types
//     - axum: Json<T>, Query<T>, Form<T>, Path<T> extractor types
//     - rocket: Json<T>, Form<T>, request guard impls (impl FromRequest)
//     - poem: #[handler] params that are Deserialize types
//     - tower/hyper: body deserialization via serde_json::from_slice / from_str
//     - salvo: #[extract] / Extractible derive
//     - gotham/tide/warp: Json body extraction primitives
//
// Honesty:
//
//	partial — heuristic regex match on source text. We detect structures likely
//	used as request DTOs but cannot prove type-system flow. Fixtures prove the
//	detection surface. Full semantic confirmation requires import-graph analysis.
//
// Issue #3267 — lang.rust.framework.* Validation cells.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_rust_validation", &rustValidationExtractor{})
}

type rustValidationExtractor struct{}

func (e *rustValidationExtractor) Language() string { return "custom_rust_validation" }

// ---------------------------------------------------------------------------
// Shared regex catalog
// ---------------------------------------------------------------------------

var (
	// #[derive(...Deserialize...)] — serde DTO structs
	reDeserialize = regexp.MustCompile(
		`#\[derive\([^)]*\b(?:Deserialize|serde::Deserialize)\b[^)]*\)\]`,
	)

	// #[derive(...Validate...)] — validator crate
	reValidateDerive = regexp.MustCompile(
		`#\[derive\([^)]*\bValidate\b[^)]*\)\]`,
	)

	// derive list for attribute extraction
	reAnyDerive = regexp.MustCompile(`#\[derive\(([^)]+)\)\]`)

	// struct Name after a derive
	reStructNameV = regexp.MustCompile(`\bstruct\s+(\w+)`)

	// #[validate(length(min=1), email)] field rules
	reValidateAttr = regexp.MustCompile(
		`#\[validate\s*\([^\)]*\)\]`,
	)

	// .validate() call (validator crate trait method)
	reValidateCall = regexp.MustCompile(
		`\.validate\s*\(\s*\)`,
	)

	// actix: web::Json<T>, web::Query<T>, web::Form<T>, web::Path<T>
	reActixExtractorV = regexp.MustCompile(
		`web::(Json|Query|Form|Path)\s*<\s*([A-Za-z_]\w*)`,
	)

	// axum: Json<T>, Query<T>, Form<T>, Path<T> (not prefixed web::)
	reAxumExtractorV = regexp.MustCompile(
		`(?:^|[^::\w])(Json|Query|Form|Path)\s*<\s*([A-Za-z_]\w*)`,
	)

	// rocket: Json<T>, Form<T>, MsgPack<T>
	reRocketExtractorV = regexp.MustCompile(
		`(?:Json|Form|MsgPack|FromForm)\s*<\s*([A-Za-z_]\w*)`,
	)

	// rocket: impl FromRequest for T (request guard)
	reRocketFromRequest = regexp.MustCompile(
		`impl\s+(?:<[^>]*>\s+)?FromRequest(?:<[^>]*>)?\s+for\s+(\w+)`,
	)

	// salvo: #[extract] on a struct field, or Extractible derive
	reSalvoExtractible = regexp.MustCompile(
		`#\[derive\([^)]*\bExtractible\b[^)]*\)\]`,
	)

	// salvo: #[salvo(extract(...))]
	reSalvoExtractAttr = regexp.MustCompile(
		`#\[salvo\s*\(\s*extract`,
	)

	// poem: #[oai(validator(...))]  or poem_openapi derive with Validate
	rePoemOAIValidator = regexp.MustCompile(
		`#\[oai\s*\([^\)]*validator`,
	)

	// serde_json::from_slice / from_str — body deserialization (hyper, tower, raw)
	reSerdeJsonDeser = regexp.MustCompile(
		`serde_json::from_(?:slice|str|reader|value)\s*[:<]`,
	)

	// gotham: extract body / serde-based patterns
	reGothamJsonBody = regexp.MustCompile(
		`Body\s*::\s*to_bytes|serde_json::from_slice`,
	)

	// tide: req.body_json::<T>()
	reTideBodyJson = regexp.MustCompile(
		`req\.body_json\s*::\s*<([A-Za-z_]\w*)>`,
	)

	// warp: warp::body::json() (filter combinator)
	reWarpBodyJson = regexp.MustCompile(
		`warp::body::json\s*\(\s*\)`,
	)

	// tower/hyper: hyper::body::to_bytes / body deserialization
	reHyperBodyDeser = regexp.MustCompile(
		`hyper::body::to_bytes|body::to_bytes|serde_json::from_slice`,
	)
)

// dtoStructsFromSrc scans src for Deserialize or Validate derives and returns
// the struct names, derive list, and source offset for each.
type dtoInfo struct {
	structName  string
	deriveList  string
	offset      int
	hasValidate bool
}

func extractDTOs(src string) []dtoInfo {
	var out []dtoInfo
	seen := make(map[string]bool)

	// Combine: any derive that includes Deserialize OR Validate
	allDerive := reAnyDerive.FindAllStringSubmatchIndex(src, -1)
	for _, dm := range allDerive {
		attrText := src[dm[0]:dm[1]]
		deriveList := src[dm[2]:dm[3]]
		isDeser := strings.Contains(deriveList, "Deserialize")
		isVal := strings.Contains(deriveList, "Validate")
		if !isDeser && !isVal {
			continue
		}
		// Look ahead for struct name
		tail := src[dm[1]:]
		if len(tail) > 500 {
			tail = tail[:500]
		}
		sm := reStructNameV.FindStringSubmatchIndex(tail)
		if sm == nil {
			continue
		}
		sname := tail[sm[2]:sm[3]]
		if seen[sname] {
			continue
		}
		seen[sname] = true
		out = append(out, dtoInfo{
			structName:  sname,
			deriveList:  attrText,
			offset:      dm[0],
			hasValidate: isVal,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rustValidationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_validation_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// -----------------------------------------------------------------------
	// 1. dto_extraction — Deserialize/Validate derived structs
	// -----------------------------------------------------------------------
	for _, dto := range extractDTOs(src) {
		subtype := "dto"
		if dto.hasValidate {
			subtype = "validated_dto"
		}
		ent := makeEntity("dto:"+dto.structName, "SCOPE.Schema", subtype,
			file.Path, file.Language, lineOf(src, dto.offset))
		setProps(&ent,
			"provenance", "INFERRED_FROM_SERDE_DESERIALIZE",
			"struct_name", dto.structName,
		)
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 2. request_validation — #[validate(...)] field attributes
	// -----------------------------------------------------------------------
	for _, m := range reValidateAttr.FindAllStringIndex(src, -1) {
		ent := makeEntity("validate_field_attr", "SCOPE.Pattern", "field_validation",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "provenance", "INFERRED_FROM_VALIDATE_ATTR")
		add(ent)
	}

	// .validate() call
	for _, m := range reValidateCall.FindAllStringIndex(src, -1) {
		ent := makeEntity("validate_call", "SCOPE.Pattern", "request_validation",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "provenance", "INFERRED_FROM_VALIDATE_CALL")
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 3. Framework-specific extractor types
	// -----------------------------------------------------------------------

	// actix: web::Json<T> / web::Query<T> / web::Form<T> / web::Path<T>
	for _, m := range reActixExtractorV.FindAllStringSubmatchIndex(src, -1) {
		extKind := src[m[2]:m[3]]
		typeParam := src[m[4]:m[5]]
		name := "actix_extractor:" + extKind + "<" + typeParam + ">"
		ent := makeEntity(name, "SCOPE.Schema", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "actix_web",
			"extractor_kind", extKind,
			"type_param", typeParam,
			"provenance", "INFERRED_FROM_ACTIX_EXTRACTOR",
		)
		add(ent)
	}

	// axum: Json<T>, Query<T>, Form<T>, Path<T> — avoid double-counting web:: prefix
	for _, m := range reAxumExtractorV.FindAllStringSubmatchIndex(src, -1) {
		// Skip actix web:: prefix variants
		if m[0] >= 5 && src[m[0]:m[0]+5] == "web::" {
			continue
		}
		extKind := src[m[2]:m[3]]
		typeParam := src[m[4]:m[5]]
		name := "axum_extractor:" + extKind + "<" + typeParam + ">"
		ent := makeEntity(name, "SCOPE.Schema", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "axum",
			"extractor_kind", extKind,
			"type_param", typeParam,
			"provenance", "INFERRED_FROM_AXUM_EXTRACTOR",
		)
		add(ent)
	}

	// rocket: Json<T>, Form<T>, MsgPack<T>, FromForm<T>
	for _, m := range reRocketExtractorV.FindAllStringSubmatchIndex(src, -1) {
		typeParam := src[m[2]:m[3]]
		name := "rocket_extractor:" + typeParam
		ent := makeEntity(name, "SCOPE.Schema", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rocket",
			"type_param", typeParam,
			"provenance", "INFERRED_FROM_ROCKET_EXTRACTOR",
		)
		add(ent)
	}

	// rocket: impl FromRequest for T (request guard — validation hook)
	for _, m := range reRocketFromRequest.FindAllStringSubmatchIndex(src, -1) {
		guardType := src[m[2]:m[3]]
		ent := makeEntity("rocket_guard:"+guardType, "SCOPE.Pattern", "request_guard",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rocket",
			"guard_type", guardType,
			"provenance", "INFERRED_FROM_ROCKET_FROM_REQUEST",
		)
		add(ent)
	}

	// salvo: Extractible derive
	for _, m := range reSalvoExtractible.FindAllStringIndex(src, -1) {
		tail := src[m[1]:]
		if len(tail) > 300 {
			tail = tail[:300]
		}
		sm := reStructNameV.FindStringSubmatchIndex(tail)
		if sm == nil {
			continue
		}
		sname := tail[sm[2]:sm[3]]
		ent := makeEntity("salvo_extractible:"+sname, "SCOPE.Schema", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "salvo",
			"struct_name", sname,
			"provenance", "INFERRED_FROM_SALVO_EXTRACTIBLE",
		)
		add(ent)
	}

	// salvo: #[salvo(extract(...))]
	for _, m := range reSalvoExtractAttr.FindAllStringIndex(src, -1) {
		ent := makeEntity("salvo_extract_attr", "SCOPE.Pattern", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "salvo",
			"provenance", "INFERRED_FROM_SALVO_EXTRACT_ATTR",
		)
		add(ent)
	}

	// poem: #[oai(validator(...))]
	for _, m := range rePoemOAIValidator.FindAllStringIndex(src, -1) {
		ent := makeEntity("poem_oai_validator", "SCOPE.Pattern", "request_validation",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "poem",
			"provenance", "INFERRED_FROM_POEM_OAI_VALIDATOR",
		)
		add(ent)
	}

	// tide: req.body_json::<T>()
	for _, m := range reTideBodyJson.FindAllStringSubmatchIndex(src, -1) {
		typeParam := src[m[2]:m[3]]
		ent := makeEntity("tide_body_json:"+typeParam, "SCOPE.Schema", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "tide",
			"type_param", typeParam,
			"provenance", "INFERRED_FROM_TIDE_BODY_JSON",
		)
		add(ent)
	}

	// warp: warp::body::json()
	for _, m := range reWarpBodyJson.FindAllStringIndex(src, -1) {
		ent := makeEntity("warp_body_json", "SCOPE.Pattern", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "warp",
			"provenance", "INFERRED_FROM_WARP_BODY_JSON",
		)
		add(ent)
	}

	// hyper/tower: serde_json::from_slice body deserialization
	for _, m := range reHyperBodyDeser.FindAllStringIndex(src, -1) {
		ent := makeEntity("hyper_body_deser", "SCOPE.Pattern", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "hyper",
			"provenance", "INFERRED_FROM_HYPER_BODY_DESER",
		)
		add(ent)
	}

	// generic: serde_json deserialization (gotham, tower, etc.)
	for _, m := range reSerdeJsonDeser.FindAllStringIndex(src, -1) {
		ent := makeEntity("serde_json_deser", "SCOPE.Pattern", "request_extractor",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"provenance", "INFERRED_FROM_SERDE_JSON_DESER",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
