package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
	"gopkg.in/yaml.v3"
)

// openAPIExtractor extracts OpenAPI/Swagger spec paths, operations, schemas
// and the relationships between them.
//
// Entity kinds emitted:
//   - SCOPE.Config / openapi_spec        — the spec itself (one per file)
//   - SCOPE.Operation / openapi_operation — every method+path pair
//   - SCOPE.Schema / openapi_schema       — every entry under components/schemas
//
// Relationships emitted (as RelationshipRecord on the source entity):
//   - Spec      → Operation : CONTAINS
//   - Spec      → Schema    : CONTAINS
//   - Operation → Schema    : REFERENCES (every $ref reachable in the operation block)
//   - Schema    → Schema    : REFERENCES (every $ref inside the schema body)
//   - Operation → Operation : TAGGED_AS  (when tags: include shared tag names — bidirectional skipped, single-direction edge keyed by tag string)
type openAPIExtractor struct{}

var (
	oaPathRE       = regexp.MustCompile(`(?m)^  (/[^\s:]+)\s*:`)
	oaOperationRE  = regexp.MustCompile(`(?m)^    (get|post|put|patch|delete|head|options)\s*:`)
	oaInfoTitleRE  = regexp.MustCompile(`(?m)^title\s*:\s*(.+)`)
	oaOpenAPIVerRE = regexp.MustCompile(`(?m)^openapi\s*:\s*(.+)`)
	oaSwaggerVerRE = regexp.MustCompile(`(?m)^swagger\s*:\s*(.+)`)

	// $ref: '#/components/schemas/Foo' or "#/definitions/Foo"
	oaRefRE = regexp.MustCompile(`\$ref\s*:\s*['"]?#/(?:components/schemas|definitions)/([A-Za-z0-9_.\-]+)['"]?`)

	// $ref: '#/components/parameters/Foo' or "#/parameters/Foo" (Swagger 2)
	oaParamRefRE = regexp.MustCompile(`\$ref\s*:\s*['"]?#/(?:components/parameters|parameters)/([A-Za-z0-9_.\-]+)['"]?`)

	// schema name lines under components.schemas: indented exactly 4 spaces
	// "    Foo:" — must NOT be deeper. Matches the canonical layout.
	oaSchemaNameRE = regexp.MustCompile(`(?m)^    ([A-Za-z_][A-Za-z0-9_.\-]*)\s*:`)

	// Swagger-2 schema name lines under top-level "definitions:" / "parameters:"
	// — names are indented exactly 2 spaces.
	oaSwagger2NameRE = regexp.MustCompile(`(?m)^  ([A-Za-z_][A-Za-z0-9_.\-]*)\s*:`)

	// tag list entry "    - tagname"
	oaTagItemRE = regexp.MustCompile(`(?m)^\s*-\s*([A-Za-z0-9_.\-]+)\s*$`)
)

var oaActivationTokens = []string{
	"openapi:", "swagger:", "paths:", "components:", "x-openapi",
}

func (o *openAPIExtractor) Category() string { return "openapi" }

func (o *openAPIExtractor) AppliesTo(src string) bool {
	for _, tok := range oaActivationTokens {
		if strings.Contains(src, tok) {
			return true
		}
	}
	return false
}

func (o *openAPIExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Get spec version
	specVersion := ""
	if m := oaOpenAPIVerRE.FindStringSubmatch(src); m != nil {
		specVersion = strings.TrimSpace(m[1])
	} else if m := oaSwaggerVerRE.FindStringSubmatch(src); m != nil {
		specVersion = strings.TrimSpace(m[1])
	}

	// Get API title. Prefer the YAML-aware path (handles the canonical nested
	// "info: { title: ... }" form). Fall back to the flat column-0 regex for
	// degenerate / non-conforming inputs, then to the literal "api" default.
	title := "api"
	if t := extractInfoTitleYAML(src); t != "" {
		title = t
	} else if m := oaInfoTitleRE.FindStringSubmatch(src); m != nil {
		title = strings.TrimSpace(m[1])
	}

	// Schemas first so we know which names exist (for ref filtering decisions
	// downstream resolvers can validate; we still emit refs unconditionally).
	schemaNames := extractSchemaNames(src)
	schemaBlocks := extractSchemaBlocks(src)

	// Parameter components (OpenAPI 3 components.parameters / Swagger 2 parameters).
	parameterNames := extractParameterNames(src)
	var parameterEntities []types.EntityRecord
	for _, name := range parameterNames {
		entityName := "openapi_parameter_" + name
		key := "parameter:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity(filePath,
			entityName, "SCOPE.Schema", "openapi_parameter", language, 1,
			map[string]string{
				"kind":           "openapi",
				"parameter_name": name,
			})
		ent.QualifiedName = "openapi/parameter/" + name
		parameterEntities = append(parameterEntities, ent)
	}

	// Build schema entities with intra-schema $ref edges.
	var schemaEntities []types.EntityRecord
	for _, name := range schemaNames {
		entityName := "openapi_schema_" + name
		key := "schema:" + name
		if seen[key] {
			continue
		}
		seen[key] = true

		body := schemaBlocks[name]
		var rels []types.RelationshipRecord
		refSeen := map[string]bool{}
		for _, m := range oaRefRE.FindAllStringSubmatch(body, -1) {
			target := m[1]
			if target == name || refSeen[target] {
				continue
			}
			refSeen[target] = true
			rels = append(rels, types.RelationshipRecord{
				FromID: entityName,
				ToID:   "openapi_schema_" + target,
				Kind:   "REFERENCES",
				Properties: map[string]string{
					"reference_kind": "schema_ref",
				},
			})
		}

		ent := makeEntity(filePath,
			entityName, "SCOPE.Schema", "openapi_schema", language,
			schemaLineNumber(src, name),
			map[string]string{
				"kind":        "openapi",
				"schema_name": name,
			})
		ent.QualifiedName = "openapi/schema/" + name
		ent.Relationships = rels
		schemaEntities = append(schemaEntities, ent)
	}

	// Spec-level entity (relationships filled in after operations).
	var specEntity *types.EntityRecord
	if specVersion != "" {
		key := "spec:" + filePath
		if !seen[key] {
			seen[key] = true
			ent := makeEntity(filePath,
				"openapi_spec_"+title, "SCOPE.Config", "openapi_spec", language, 1,
				map[string]string{
					"kind":         "openapi",
					"title":        title,
					"spec_version": specVersion,
				})
			ent.QualifiedName = "openapi/spec/" + title
			specEntity = &ent
		}
	}

	// Extract paths + operations and their per-block $ref edges.
	lines := strings.Split(src, "\n")
	currentPath := ""
	type opRange struct {
		startIdx int // first line of the operation's body
	}
	type pendingOp struct {
		entity   *types.EntityRecord
		startIdx int
		method   string
		path     string
	}
	var pendingOps []pendingOp

	for lineIdx, line := range lines {
		if m := oaPathRE.FindStringSubmatch(line); m != nil {
			currentPath = m[1]
			continue
		}
		if currentPath == "" {
			continue
		}
		if m := oaOperationRE.FindStringSubmatch(line); m != nil {
			method := m[1]
			key := fmt.Sprintf("op:%s:%s", method, currentPath)
			if seen[key] {
				continue
			}
			seen[key] = true
			ent := makeEntity(filePath,
				fmt.Sprintf("openapi_op_%s_%s", method, strings.ReplaceAll(currentPath, "/", "_")),
				"SCOPE.Operation", "openapi_operation", language,
				lineIdx+1,
				map[string]string{
					"kind":   "openapi",
					"method": strings.ToUpper(method),
					"path":   currentPath,
				})
			ent.QualifiedName = fmt.Sprintf("openapi/op/%s/%s", strings.ToUpper(method), currentPath)
			pendingOps = append(pendingOps, pendingOp{
				entity:   &ent,
				startIdx: lineIdx,
				method:   method,
				path:     currentPath,
			})
			_ = opRange{startIdx: lineIdx}
		}
	}

	// For every operation block, collect $refs and tags.
	for i := range pendingOps {
		body := operationBody(lines, pendingOps[i].startIdx)

		// Operation → Schema REFERENCES from $ref lines in operation body
		refSeen := map[string]bool{}
		for _, m := range oaRefRE.FindAllStringSubmatch(body, -1) {
			target := m[1]
			if refSeen[target] {
				continue
			}
			refSeen[target] = true
			pendingOps[i].entity.Relationships = append(
				pendingOps[i].entity.Relationships,
				types.RelationshipRecord{
					FromID: pendingOps[i].entity.Name,
					ToID:   "openapi_schema_" + target,
					Kind:   "REFERENCES",
					Properties: map[string]string{
						"reference_kind": "schema_ref",
					},
				},
			)
		}

		// Operation → Parameter REFERENCES from $ref to component parameters
		paramRefSeen := map[string]bool{}
		for _, m := range oaParamRefRE.FindAllStringSubmatch(body, -1) {
			target := m[1]
			if paramRefSeen[target] {
				continue
			}
			paramRefSeen[target] = true
			pendingOps[i].entity.Relationships = append(
				pendingOps[i].entity.Relationships,
				types.RelationshipRecord{
					FromID: pendingOps[i].entity.Name,
					ToID:   "openapi_parameter_" + target,
					Kind:   "REFERENCES",
					Properties: map[string]string{
						"reference_kind": "parameter_ref",
					},
				},
			)
		}

		// Tags: each tag becomes a Properties tag list and is also exposed as
		// a TAGGED_AS relationship using the tag string as the ToID. Downstream
		// resolvers can match on the tag name.
		tags := extractOperationTags(body)
		if len(tags) > 0 {
			pendingOps[i].entity.Properties["tags"] = strings.Join(tags, ",")
			tagSeen := map[string]bool{}
			for _, tag := range tags {
				if tagSeen[tag] {
					continue
				}
				tagSeen[tag] = true
				pendingOps[i].entity.Relationships = append(
					pendingOps[i].entity.Relationships,
					types.RelationshipRecord{
						FromID: pendingOps[i].entity.Name,
						ToID:   "openapi_tag_" + tag,
						Kind:   "TAGGED_AS",
						Properties: map[string]string{
							"tag": tag,
						},
					},
				)
			}
		}
	}

	// Spec CONTAINS every operation and every schema.
	if specEntity != nil {
		for i := range pendingOps {
			specEntity.Relationships = append(specEntity.Relationships,
				types.RelationshipRecord{
					FromID: specEntity.Name,
					ToID:   pendingOps[i].entity.Name,
					Kind:   "CONTAINS",
					Properties: map[string]string{
						"contained_kind": "operation",
					},
				})
		}
		for _, s := range schemaEntities {
			specEntity.Relationships = append(specEntity.Relationships,
				types.RelationshipRecord{
					FromID: specEntity.Name,
					ToID:   s.Name,
					Kind:   "CONTAINS",
					Properties: map[string]string{
						"contained_kind": "schema",
					},
				})
		}
		for _, p := range parameterEntities {
			specEntity.Relationships = append(specEntity.Relationships,
				types.RelationshipRecord{
					FromID: specEntity.Name,
					ToID:   p.Name,
					Kind:   "CONTAINS",
					Properties: map[string]string{
						"contained_kind": "parameter",
					},
				})
		}
		results = append(results, *specEntity)
	}

	for _, s := range schemaEntities {
		results = append(results, s)
	}
	for _, p := range parameterEntities {
		results = append(results, p)
	}
	for _, po := range pendingOps {
		results = append(results, *po.entity)
	}

	return results
}

// extractSchemaNames returns ordered, unique schema names declared under
// "components.schemas:" (OpenAPI 3) or "definitions:" (Swagger 2). Names are
// identified by 4-space-indented keys following the OpenAPI 3 section header
// or 2-space-indented keys for Swagger 2 top-level "definitions:", stopping at
// the next top-level key (column 0).
func extractSchemaNames(src string) []string {
	var names []string
	seen := map[string]bool{}
	for _, sec := range schemaSectionBodiesTagged(src) {
		re := oaSchemaNameRE
		if sec.swagger2 {
			re = oaSwagger2NameRE
		}
		for _, m := range re.FindAllStringSubmatch(sec.body, -1) {
			name := m[1]
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// extractSchemaBlocks returns a map of schema name to the YAML block body
// belonging to that schema (i.e. all lines indented deeper than 4 spaces
// until the next 4-space-indented sibling key).
func extractSchemaBlocks(src string) map[string]string {
	blocks := map[string]string{}
	for _, sec := range schemaSectionBodiesTagged(src) {
		nameRE := oaSchemaNameRE
		if sec.swagger2 {
			nameRE = oaSwagger2NameRE
		}
		lines := strings.Split(sec.body, "\n")
		currentName := ""
		var buf []string
		for _, line := range lines {
			if m := nameRE.FindStringSubmatch(line); m != nil {
				if currentName != "" {
					blocks[currentName] = strings.Join(buf, "\n")
				}
				currentName = m[1]
				buf = nil
				continue
			}
			if currentName == "" {
				continue
			}
			// Stop accumulating if line is non-indented (top-level key).
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && strings.TrimSpace(line) != "" {
				blocks[currentName] = strings.Join(buf, "\n")
				currentName = ""
				buf = nil
				continue
			}
			buf = append(buf, line)
		}
		if currentName != "" {
			blocks[currentName] = strings.Join(buf, "\n")
		}
	}
	return blocks
}

// extractParameterNames returns ordered, unique parameter component names
// declared under "components.parameters:" (OpenAPI 3) or top-level
// "parameters:" (Swagger 2). Names are identified by exactly 4-space-indented
// keys following the section header for OpenAPI 3, or 2-space-indented keys
// for Swagger 2 — same indent strategy as schema extraction.
func extractParameterNames(src string) []string {
	var names []string
	seen := map[string]bool{}
	for _, sec := range parameterSectionBodiesTagged(src) {
		re := oaSchemaNameRE
		if sec.swagger2 {
			re = oaSwagger2NameRE
		}
		for _, m := range re.FindAllStringSubmatch(sec.body, -1) {
			name := m[1]
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// parameterSectionBodiesTagged returns tagged bodies for
// "components.parameters:" (OpenAPI 3) and Swagger-2 top-level "parameters:".
func parameterSectionBodiesTagged(src string) []schemaSection {
	var sections []schemaSection
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, "components:") {
			for j := i + 1; j < len(lines); j++ {
				lj := lines[j]
				if len(lj) > 0 && lj[0] != ' ' && lj[0] != '\t' && strings.TrimSpace(lj) != "" {
					break
				}
				if strings.HasPrefix(lj, "  parameters:") {
					body := collectIndentedBlock(lines, j+1, 4)
					sections = append(sections, schemaSection{body: body, swagger2: false})
					break
				}
			}
		}
		if strings.HasPrefix(l, "parameters:") {
			body := collectIndentedBlock(lines, i+1, 2)
			sections = append(sections, schemaSection{body: body, swagger2: true})
		}
	}
	return sections
}

// schemaSection is a body of YAML carrying schema definitions plus a flag
// indicating whether it came from a Swagger-2 (top-level "definitions:") block,
// where names are indented at 2 spaces rather than the OpenAPI-3 convention of 4.
type schemaSection struct {
	body     string
	swagger2 bool
}

// schemaSectionBodiesTagged returns the bodies of schema-defining sections
// (OpenAPI-3 "components.schemas:" and Swagger-2 top-level "definitions:")
// tagged with the dialect so callers can pick the right name regex.
func schemaSectionBodiesTagged(src string) []schemaSection {
	var sections []schemaSection
	lines := strings.Split(src, "\n")

	// Find every "components:" header at column 0, then locate "  schemas:"
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, "components:") {
			// scan forward until we find "  schemas:" (2-space indent), stop
			// when we hit another top-level key (column 0, non-empty).
			for j := i + 1; j < len(lines); j++ {
				lj := lines[j]
				if len(lj) > 0 && lj[0] != ' ' && lj[0] != '\t' && strings.TrimSpace(lj) != "" {
					break
				}
				if strings.HasPrefix(lj, "  schemas:") {
					body := collectIndentedBlock(lines, j+1, 4)
					sections = append(sections, schemaSection{body: body, swagger2: false})
					break
				}
			}
		}
		if strings.HasPrefix(l, "definitions:") {
			body := collectIndentedBlock(lines, i+1, 2)
			sections = append(sections, schemaSection{body: body, swagger2: true})
		}
	}
	return sections
}

// collectIndentedBlock returns the text of consecutive lines starting at
// startIdx that are indented at least minIndent spaces, stopping at the
// first line with smaller indentation that is non-empty.
func collectIndentedBlock(lines []string, startIdx, minIndent int) string {
	var buf []string
	for k := startIdx; k < len(lines); k++ {
		line := lines[k]
		if strings.TrimSpace(line) == "" {
			buf = append(buf, line)
			continue
		}
		// Count leading spaces
		indent := 0
		for indent < len(line) && line[indent] == ' ' {
			indent++
		}
		if indent < minIndent {
			break
		}
		buf = append(buf, line)
	}
	return strings.Join(buf, "\n")
}

// schemaLineNumber returns the 1-indexed line number of a schema declaration.
// Matches both OpenAPI-3 (4-space indent) and Swagger-2 (2-space indent) forms.
func schemaLineNumber(src, name string) int {
	t4 := "    " + name + ":"
	t2 := "  " + name + ":"
	for i, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, t4) || strings.HasPrefix(line, t2) {
			return i + 1
		}
	}
	return 1
}

// operationBody returns the text of an operation block starting at startIdx
// (the line containing the method key). It accumulates indented continuation
// lines until the next operation-or-shallower key.
func operationBody(lines []string, startIdx int) string {
	if startIdx >= len(lines) {
		return ""
	}
	// Operation header is at indent 4 spaces. Body continues at indent >= 6.
	var buf []string
	buf = append(buf, lines[startIdx])
	for k := startIdx + 1; k < len(lines); k++ {
		line := lines[k]
		if strings.TrimSpace(line) == "" {
			buf = append(buf, line)
			continue
		}
		indent := 0
		for indent < len(line) && line[indent] == ' ' {
			indent++
		}
		// Stop at the next path entry (indent 2), next operation (indent 4),
		// or a top-level key (indent 0).
		if indent <= 4 {
			break
		}
		buf = append(buf, line)
	}
	return strings.Join(buf, "\n")
}

// extractOperationTags returns the tag names declared by an operation's
// "tags:" sequence. Tags are list entries — "      - tag_name" — directly
// underneath a "tags:" key. Inline lists like "tags: [a, b]" are also handled.
func extractOperationTags(body string) []string {
	var out []string
	seen := map[string]bool{}
	lines := strings.Split(body, "\n")
	inTags := false
	tagsIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Inline form: "tags: [a, b, c]"
		if !inTags && strings.HasPrefix(trimmed, "tags:") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "tags:"))
			if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
				inner := strings.TrimSuffix(strings.TrimPrefix(rest, "["), "]")
				for _, raw := range strings.Split(inner, ",") {
					t := strings.Trim(strings.TrimSpace(raw), `"'`)
					if t == "" || seen[t] {
						continue
					}
					seen[t] = true
					out = append(out, t)
				}
				continue
			}
			// Block form: track indentation
			indent := 0
			for indent < len(line) && line[indent] == ' ' {
				indent++
			}
			tagsIndent = indent
			inTags = true
			continue
		}

		if inTags {
			indent := 0
			for indent < len(line) && line[indent] == ' ' {
				indent++
			}
			// Block ends when we go back to a sibling key at <= tagsIndent
			if strings.TrimSpace(line) != "" && indent <= tagsIndent && !strings.HasPrefix(trimmed, "-") {
				inTags = false
				continue
			}
			if m := oaTagItemRE.FindStringSubmatch(line); m != nil {
				t := m[1]
				if t == "" || seen[t] {
					continue
				}
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// extractInfoTitleYAML parses src as YAML and returns the value of info.title
// if present and a non-empty string. Returns "" on any parse failure or when
// the field is absent — callers should fall back to other strategies. This
// handles the canonical nested form:
//
//	info:
//	  title: My API
//	  version: 1.0
func extractInfoTitleYAML(src string) string {
	var doc struct {
		Info struct {
			Title string `yaml:"title"`
		} `yaml:"info"`
	}
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Info.Title)
}

func init() {
	Register(&openAPIExtractor{})
}
