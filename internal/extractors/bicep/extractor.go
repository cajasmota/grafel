// Package bicep implements a regex/line-based extractor for Azure Bicep
// infrastructure-as-code files (*.bicep).
//
// No tree-sitter grammar for Bicep is vendored in this repo, so — like the
// COBOL/CDK regex detectors — this extractor parses raw source text. Bicep is
// structurally HCL-like (resource / module / param / var / output declarations
// with symbolic-name cross-references), so the entity + edge model mirrors the
// HCL/Terraform extractor (internal/extractors/hcl):
//
//	resource <symbolicName> 'Microsoft.Storage/storageAccounts@2022-09-01' = {…}
//	    → SCOPE.InfraResource (Kind via bicepResourceKind on the AzureRP type),
//	      named by symbolicName; the deployed name: '…' is folded into Metadata.
//	module  <name> '<path>.bicep' = {…}
//	    → SCOPE.Component/module + IMPORTS edge to the referenced .bicep path.
//	param   <name> <type>            → SCOPE.Schema/param
//	var     <name> = …               → SCOPE.Schema/var
//	output  <name> <type> = …        → SCOPE.Schema/output
//
// Relationships (reusing the HCL DEPENDS_ON edge kind):
//
//	symbolic-name references in a resource/module body
//	    (storageAccount.id, storageAccount.properties.x, foo.outputs.y) → DEPENDS_ON
//	explicit  dependsOn: [a, b]      → DEPENDS_ON to each listed symbolic name
//
// `existing` resources and `[for item in items: {…}]` loop bodies are handled:
// the resource is still emitted (with metadata flags) and references inside a
// loop body are still attributed to the declaring resource.
//
// Edges are emitted as Format-A structural-refs tied to the current file
// (BuildOperationStructuralRef) so the resolver binds them via byLocation to
// the sibling entity in the same file — exactly as the HCL extractor does for
// depends_on.
//
// OTel span: "indexer.extract.bicep" with attributes language, file_line_count,
// entity_count.
//
// Registered under the "bicep" language key via init(); .bicep files are routed
// here by internal/classifier (extension → "bicep").
package bicep

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("bicep", &Extractor{})
}

// Extractor implements extractor.Extractor for Azure Bicep.
type Extractor struct{}

// Language implements extractor.Extractor.
func (e *Extractor) Language() string { return "bicep" }

const lang = "bicep"

var (
	// resource <symbolicName> 'Microsoft.Storage/storageAccounts@2022-09-01' [existing] = {
	// The trailing portion (existing / = / =[for …]) is matched loosely so the
	// declaration line is recognised regardless of loop / existing modifiers.
	reResource = regexp.MustCompile(`(?m)^\s*resource\s+([A-Za-z_][A-Za-z0-9_]*)\s+'([^']+)'\s*(existing)?\s*=`)
	// module <name> '<path>' = {
	reModule = regexp.MustCompile(`(?m)^\s*module\s+([A-Za-z_][A-Za-z0-9_]*)\s+'([^']+)'\s*=`)
	// param <name> <type> [= default]
	reParam = regexp.MustCompile(`(?m)^\s*param\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z0-9_<>\[\]]+)`)
	// var <name> = …
	reVar = regexp.MustCompile(`(?m)^\s*var\s+([A-Za-z_][A-Za-z0-9_]*)\s*=`)
	// output <name> <type> = …
	reOutput = regexp.MustCompile(`(?m)^\s*output\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z0-9_<>\[\]]+)\s*=`)

	// name: 'literal' — the deployed Azure resource name inside a body.
	reNameAttr = regexp.MustCompile(`(?m)^\s*name:\s*'([^']*)'`)
	// dependsOn: [ … ] — explicit dependency list (may span lines).
	reDependsOn = regexp.MustCompile(`(?s)dependsOn:\s*\[(.*?)\]`)
)

// Extract implements extractor.Extractor. Never panics; returns partial
// results on malformed input.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.bicep")
	_, span := tracer.Start(ctx, "indexer.extract.bicep")
	defer span.End()

	lineCount := 0
	if len(file.Content) > 0 {
		lineCount = bytes.Count(file.Content, []byte{'\n'}) + 1
	}

	if len(file.Content) == 0 {
		span.SetAttributes(
			attribute.String("language", lang),
			attribute.Int("file_line_count", 0),
			attribute.Int("entity_count", 0),
		)
		return nil, nil
	}

	src := string(file.Content)
	path := file.Path

	// bicepconfig.json — Bicep's JSON config file, not a .bicep template. Parse
	// its moduleAliases.br / moduleAliases.ts registry aliases instead of the
	// resource/module/param DSL.
	if isBicepConfig(path) {
		records := extractBicepConfig(src, path)
		span.SetAttributes(
			attribute.String("language", lang),
			attribute.Int("file_line_count", lineCount),
			attribute.Int("entity_count", len(records)),
		)
		return records, nil
	}

	// Pre-scan: collect the set of symbolic names declared as resources or
	// modules in this file so reference extraction only emits DEPENDS_ON edges
	// to real local declarations (filters out built-in functions, params, etc.).
	symbolic := collectSymbolicNames(src)

	var records []types.EntityRecord

	records = append(records, extractResources(src, path, symbolic)...)
	records = append(records, extractModules(src, path, symbolic)...)
	records = append(records, extractParams(src, path)...)
	records = append(records, extractVars(src, path)...)
	records = append(records, extractOutputs(src, path)...)

	// #6852 — the module IMPORTS edge keeps the file path as its FromID (#120,
	// and TestBicep_ImportsKeepsFilePathAnchor pins it), but nothing bicep
	// emits is NAMED after the file, so that FROM end resolved to nothing:
	// internal/resolve/refs.go has no path→entity index, and a path-valued
	// FromID resolves only onto a record carrying that exact string as its
	// Name. PrependFileCarrier is the CONDITIONAL form #6815 and #6518 settled
	// on — it mints the carrier only when some relationship is actually
	// anchored on the path. Unconditional would add one bare orphan node per
	// .bicep file across a whole repo, including the many that declare only
	// resources/params and no module at all.
	//
	// Placed on this branch only: the bicepconfig.json branch above returns
	// entities with no relationships whatsoever, so it anchors nothing and the
	// helper would be a guaranteed no-op there.
	records = extractor.PrependFileCarrier(path, lang, records)

	span.SetAttributes(
		attribute.String("language", lang),
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", len(records)),
	)
	return records, nil
}

// collectSymbolicNames returns the set of resource/module symbolic names
// declared in the file.
func collectSymbolicNames(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range reResource.FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	for _, m := range reModule.FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	return out
}

// extractResources emits a SCOPE.InfraResource per `resource` declaration and
// its DEPENDS_ON edges (symbolic-name references + explicit dependsOn).
func extractResources(src, path string, symbolic map[string]bool) []types.EntityRecord {
	var out []types.EntityRecord
	locs := reResource.FindAllStringSubmatchIndex(src, -1)
	for i, loc := range locs {
		symName := src[loc[2]:loc[3]]
		azureType := src[loc[4]:loc[5]]
		isExisting := loc[6] >= 0 // optional `existing` group matched

		startLine := lineOf(src, loc[0])
		// Body spans from this declaration to the next top-level declaration.
		bodyStart := loc[1]
		bodyEnd := len(src)
		if i+1 < len(locs) {
			bodyEnd = locs[i+1][0]
		}
		// Clamp against the next module declaration too (resources/modules are
		// interleaved); find the earliest following declaration boundary.
		bodyEnd = nextDeclBoundary(src, bodyStart, bodyEnd)
		body := src[bodyStart:bodyEnd]
		endLine := startLine + strings.Count(body, "\n")

		rpType, apiVersion := splitAzureType(azureType)

		category := bicepResourceCoarseScope(rpType)
		meta := map[string]interface{}{
			"subtype":           "resource",
			"iac_tool":          "bicep",
			"symbolic_name":     symName,
			"azure_rp_type":     rpType,
			"resource_category": category,
			// resource_scope kept (== resource_category) for back-compat.
			"resource_scope": category,
		}
		if apiVersion != "" {
			meta["api_version"] = apiVersion
		}
		if isExisting {
			meta["existing"] = "true"
		}
		if nm := reNameAttr.FindStringSubmatch(body); nm != nil {
			meta["deployed_name"] = nm[1]
		}
		if isLoop(body) {
			meta["loop"] = "true"
		}

		rec := types.EntityRecord{
			Name:          symName,
			Kind:          "SCOPE.InfraResource",
			Subtype:       "resource",
			SourceFile:    path,
			StartLine:     startLine,
			EndLine:       endLine,
			Language:      lang,
			QualityScore:  0.9,
			QualifiedName: rpType + "." + symName,
			Metadata:      meta,
		}
		rec.Relationships = dependencyEdges(body, path, symName, symbolic)
		out = append(out, rec)
	}
	return out
}

// extractModules emits a SCOPE.Component/module per `module` declaration with
// an IMPORTS edge to the referenced .bicep path plus DEPENDS_ON edges.
func extractModules(src, path string, symbolic map[string]bool) []types.EntityRecord {
	var out []types.EntityRecord
	locs := reModule.FindAllStringSubmatchIndex(src, -1)
	for i, loc := range locs {
		modName := src[loc[2]:loc[3]]
		modPath := src[loc[4]:loc[5]]

		startLine := lineOf(src, loc[0])
		bodyStart := loc[1]
		bodyEnd := len(src)
		if i+1 < len(locs) {
			bodyEnd = locs[i+1][0]
		}
		bodyEnd = nextDeclBoundary(src, bodyStart, bodyEnd)
		body := src[bodyStart:bodyEnd]
		endLine := startLine + strings.Count(body, "\n")

		meta := map[string]interface{}{
			"subtype":  "module",
			"iac_tool": "bicep",
			"label":    modName,
			"source":   modPath,
		}
		if nm := reNameAttr.FindStringSubmatch(body); nm != nil {
			meta["deployed_name"] = nm[1]
		}
		if isLoop(body) {
			meta["loop"] = "true"
		}

		reg := classifyModuleRegistry(modPath)
		if reg.isRegistry {
			meta["module_registry"] = reg.registry // "acr" | "mcr" | "template-spec"
			meta["registry_scheme"] = reg.scheme   // "br" | "ts"
			meta["registry_ref"] = reg.ref         // path/name without scheme + tag
			if reg.tag != "" {
				meta["registry_tag"] = reg.tag
			}
			if reg.alias != "" {
				meta["registry_alias"] = reg.alias
			}
		}

		rec := types.EntityRecord{
			Name:          modName,
			Kind:          "SCOPE.Component",
			Subtype:       "module",
			SourceFile:    path,
			StartLine:     startLine,
			EndLine:       endLine,
			Language:      lang,
			QualityScore:  0.9,
			QualifiedName: modPath,
			Metadata:      meta,
		}

		// IMPORTS edge to the referenced module. A local module resolves to the
		// sibling .bicep file node; a registry module (br:/ts:/br/…/ts/…) is an
		// external artifact pulled from an ACR / MCR / template-spec registry, so
		// it is keyed under an external module namespace rather than a (bogus)
		// local-file component node.
		importTo := "scope:component:file:bicep:" + modPath
		if reg.isRegistry {
			importTo = "scope:component:external:bicep:" + reg.scheme + ":" + reg.ref
			if reg.tag != "" {
				importTo += ":" + reg.tag
			}
		}
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			FromID: path,
			ToID:   importTo,
			Kind:   "IMPORTS",
		})
		rec.Relationships = append(rec.Relationships, dependencyEdges(body, path, modName, symbolic)...)
		out = append(out, rec)
	}
	return out
}

// extractParams emits a SCOPE.Schema/param per `param` declaration.
func extractParams(src, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, loc := range reParam.FindAllStringSubmatchIndex(src, -1) {
		name := src[loc[2]:loc[3]]
		paramType := src[loc[4]:loc[5]]
		out = append(out, types.EntityRecord{
			Name:         "param." + name,
			Kind:         "SCOPE.Schema",
			Subtype:      "param",
			SourceFile:   path,
			StartLine:    lineOf(src, loc[0]),
			EndLine:      lineOf(src, loc[0]),
			Language:     lang,
			QualityScore: 0.8,
			Metadata: map[string]interface{}{
				"subtype":    "param",
				"iac_tool":   "bicep",
				"label":      name,
				"param_type": paramType,
			},
		})
	}
	return out
}

// extractVars emits a SCOPE.Schema/var per `var` declaration.
func extractVars(src, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, loc := range reVar.FindAllStringSubmatchIndex(src, -1) {
		name := src[loc[2]:loc[3]]
		out = append(out, types.EntityRecord{
			Name:         "var." + name,
			Kind:         "SCOPE.Schema",
			Subtype:      "var",
			SourceFile:   path,
			StartLine:    lineOf(src, loc[0]),
			EndLine:      lineOf(src, loc[0]),
			Language:     lang,
			QualityScore: 0.8,
			Metadata: map[string]interface{}{
				"subtype":  "var",
				"iac_tool": "bicep",
				"label":    name,
			},
		})
	}
	return out
}

// extractOutputs emits a SCOPE.Schema/output per `output` declaration.
func extractOutputs(src, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, loc := range reOutput.FindAllStringSubmatchIndex(src, -1) {
		name := src[loc[2]:loc[3]]
		outType := src[loc[4]:loc[5]]
		out = append(out, types.EntityRecord{
			Name:         "output." + name,
			Kind:         "SCOPE.Schema",
			Subtype:      "output",
			SourceFile:   path,
			StartLine:    lineOf(src, loc[0]),
			EndLine:      lineOf(src, loc[0]),
			Language:     lang,
			QualityScore: 0.8,
			Metadata: map[string]interface{}{
				"subtype":     "output",
				"iac_tool":    "bicep",
				"label":       name,
				"output_type": outType,
			},
		})
	}
	return out
}

// dependencyEdges returns DEPENDS_ON edges for a resource/module body: one per
// distinct symbolic name referenced (either through a dotted property access
// like `foo.id` / `foo.outputs.x`, or listed in an explicit dependsOn array).
// Self-references are skipped. Only names in `symbolic` (locally-declared
// resources/modules) produce edges.
func dependencyEdges(body, path, self string, symbolic map[string]bool) []types.RelationshipRecord {
	deps := referencedSymbols(body, self, symbolic)
	if len(deps) == 0 {
		return nil
	}
	var rels []types.RelationshipRecord
	for _, dep := range deps {
		// #6367 — FromID is left EMPTY on purpose. These records are attached
		// to the RESOURCE record (extractResources) and to the MODULE record
		// (extractModules), so graph assembly stamps that owning record's own
		// entity id (cmd/grafel/index.go, relRecordToGraphRel in
		// internal/extractors/incremental.go). The previous `FromID: path`
		// moved the edge off its owner and resolved nowhere: bicep emits no
		// file entity and no bicep entity carries the containing file's path
		// as Name or QualifiedName, so it DANGLED at every path depth —
		// measured 3 of 3 at both a nested and a root path. `.bicep` is also
		// absent from sourceFileExtensions (internal/resolve/refs.go), so the
		// dangle counted as DispositionBugExtractor rather than being masked
		// as DispositionDynamic. See TestBicep_DependsOnAnchoredOnOwner.
		// The module IMPORTS edge keeps its file-path FromID (#120).
		rels = append(rels, types.RelationshipRecord{
			ToID: extractor.BuildOperationStructuralRef(lang, path, dep),
			Kind: "DEPENDS_ON",
		})
	}
	return rels
}

// reSymbolRef matches a symbolic-name property access: `<name>.id`,
// `<name>.properties.x`, `<name>.outputs.y`. The leading boundary excludes
// dotted continuations (so `a.b.c` does not also match `b.c`).
var reSymbolRef = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.(?:id|name|properties|outputs)\b`)

// referencedSymbols returns the sorted-unique set of locally-declared symbolic
// names referenced in body (excluding self), via property access or dependsOn.
func referencedSymbols(body, self string, symbolic map[string]bool) []string {
	seen := map[string]bool{}
	var order []string
	add := func(name string) {
		if name == self || !symbolic[name] || seen[name] {
			return
		}
		seen[name] = true
		order = append(order, name)
	}

	for _, m := range reSymbolRef.FindAllStringSubmatch(body, -1) {
		// Skip a property access whose token is preceded by a '.' (i.e. it is a
		// nested segment, not a root symbol). FindAllStringSubmatch loses that
		// context, so re-check via index below instead.
		add(m[1])
	}
	// Re-filter nested segments: only keep names that appear as a root token
	// (not immediately preceded by '.').
	order = filterRootTokens(body, order)

	// Explicit dependsOn: [ a, b ] — names are bare symbolic identifiers,
	// optionally with array-index / property suffixes.
	for _, dm := range reDependsOn.FindAllStringSubmatch(body, -1) {
		for _, tok := range splitDependsOn(dm[1]) {
			add(tok)
		}
	}
	return order
}

// filterRootTokens drops names from order that never occur in body as a root
// token (a token not immediately preceded by '.'), guarding against matching
// the tail of a longer dotted path such as parent.child.id.
func filterRootTokens(body string, order []string) []string {
	var out []string
	for _, name := range order {
		if occursAsRoot(body, name) {
			out = append(out, name)
		}
	}
	return out
}

// occursAsRoot reports whether name appears in body as a root identifier: a
// match not immediately preceded by '.' or an identifier character.
func occursAsRoot(body, name string) bool {
	idx := 0
	for {
		j := strings.Index(body[idx:], name)
		if j < 0 {
			return false
		}
		pos := idx + j
		if pos == 0 || !isIdentByte(body[pos-1]) && body[pos-1] != '.' {
			// Ensure the char after is a '.' (property access) to count.
			after := pos + len(name)
			if after < len(body) && body[after] == '.' {
				return true
			}
		}
		idx = pos + len(name)
		if idx >= len(body) {
			return false
		}
	}
}

func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// splitDependsOn splits a dependsOn array body into bare symbolic-name tokens,
// stripping array-index / property suffixes (e.g. `vnets[0]` → `vnets`,
// `sa.id` → `sa`).
func splitDependsOn(inner string) []string {
	fields := strings.FieldsFunc(inner, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t' || r == '\r'
	})
	var out []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		// Strip property/index suffix: keep the leading identifier.
		for i := 0; i < len(f); i++ {
			if !isIdentByte(f[i]) {
				f = f[:i]
				break
			}
		}
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// moduleRegistryRef is the parsed form of a Bicep registry/template-spec module
// reference (`br:`/`ts:` schemes, with or without a bicepconfig moduleAlias).
type moduleRegistryRef struct {
	isRegistry bool
	scheme     string // "br" | "ts"
	registry   string // "acr" | "mcr" | "template-spec"
	alias      string // bicepconfig moduleAlias name (br/<alias>:… form), if any
	ref        string // registry path/name with scheme + tag stripped
	tag        string // version tag after the final ':'
}

// classifyModuleRegistry recognises a Bicep module-registry reference. Bicep
// supports two registry schemes for `module` sources:
//
//	br:<registry>/<path>:<tag>            — full ACR OCI reference
//	br/<alias>:<path>:<tag>               — ACR ref via a bicepconfig moduleAlias
//	ts:<sub>/<rg>/<name>:<ver>            — full template-spec reference
//	ts/<alias>:<name>:<ver>               — template-spec via a moduleAlias
//
// The public Microsoft module registry (mcr.microsoft.com / the `public` alias)
// is reported as registry="mcr"; other `br` refs as "acr"; `ts` refs as
// "template-spec". A plain local path (`./mod.bicep`) returns isRegistry=false.
func classifyModuleRegistry(modPath string) moduleRegistryRef {
	scheme, rest, aliased, ok := splitRegistryScheme(modPath)
	if !ok {
		return moduleRegistryRef{}
	}
	r := moduleRegistryRef{isRegistry: true, scheme: scheme}

	// rest is "<alias>:<path>:<tag>" when aliased, else "<registry>/<path>:<tag>".
	if aliased {
		// alias is the first colon-delimited segment.
		if i := strings.IndexByte(rest, ':'); i >= 0 {
			r.alias = rest[:i]
			rest = rest[i+1:]
		} else {
			r.alias = rest
			rest = ""
		}
	}

	// Split the trailing ":<tag>" (the version). A path segment may itself
	// contain no colon, so the final ':' delimits the tag.
	if i := strings.LastIndexByte(rest, ':'); i >= 0 {
		r.ref = rest[:i]
		r.tag = rest[i+1:]
	} else {
		r.ref = rest
	}

	switch scheme {
	case "ts":
		r.registry = "template-spec"
	default: // "br"
		if r.alias == "public" || strings.HasPrefix(r.ref, "mcr.microsoft.com") {
			r.registry = "mcr"
		} else {
			r.registry = "acr"
		}
	}
	return r
}

// splitRegistryScheme detaches a `br:`/`ts:`/`br/`/`ts/` scheme prefix from a
// module path. The `:` form is the full reference, the `/` form is the
// bicepconfig moduleAlias form. Returns ok=false for any other path.
func splitRegistryScheme(modPath string) (scheme, rest string, aliased, ok bool) {
	for _, s := range []string{"br", "ts"} {
		if strings.HasPrefix(modPath, s+":") {
			return s, modPath[len(s)+1:], false, true
		}
		if strings.HasPrefix(modPath, s+"/") {
			return s, modPath[len(s)+1:], true, true
		}
	}
	return "", "", false, false
}

// splitAzureType splits 'Microsoft.Storage/storageAccounts@2022-09-01' into the
// resource-provider type ("Microsoft.Storage/storageAccounts") and api version.
func splitAzureType(full string) (rpType, apiVersion string) {
	if at := strings.IndexByte(full, '@'); at >= 0 {
		return full[:at], full[at+1:]
	}
	return full, ""
}

// isLoop reports whether a body opens with a `[for … in … :` comprehension.
func isLoop(body string) bool {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "[for ") || strings.HasPrefix(trimmed, "[for\t") {
		return true
	}
	return strings.Contains(trimmed, "[for ") && strings.Contains(trimmed, " in ")
}

// nextDeclBoundary returns the byte offset (within [start,max)) of the next
// top-level resource/module/param/var/output/@-decorator declaration after a
// declaration body begins, so a body never bleeds into the following entity.
func nextDeclBoundary(src string, start, max int) int {
	window := src[start:max]
	earliest := len(window)
	for _, re := range []*regexp.Regexp{reResource, reModule, reParam, reVar, reOutput} {
		if loc := re.FindStringIndex(window); loc != nil && loc[0] < earliest {
			earliest = loc[0]
		}
	}
	return start + earliest
}

// isBicepConfig reports whether path is a Bicep configuration file
// (bicepconfig.json), case-insensitively on the basename.
func isBicepConfig(p string) bool {
	return strings.EqualFold(path.Base(p), "bicepconfig.json")
}

// bicepConfig is the subset of bicepconfig.json this extractor reads: the
// moduleAliases map keyed by scheme ("br" / "ts"), each a map of alias name to
// its registry/subscription target.
type bicepConfig struct {
	ModuleAliases map[string]map[string]struct {
		Registry      string `json:"registry"`
		ModulePath    string `json:"modulePath"`
		Subscription  string `json:"subscription"`
		ResourceGroup string `json:"resourceGroup"`
	} `json:"moduleAliases"`
}

// extractBicepConfig emits one config entity for bicepconfig.json plus one
// SCOPE.Schema/module-alias entity per declared moduleAlias, so registry-aliased
// module references (br/<alias>:… / ts/<alias>:…) resolve to their target
// registry / template-spec scope. Malformed JSON yields just the config entity.
func extractBicepConfig(src, p string) []types.EntityRecord {
	cfg := types.EntityRecord{
		Name:          "bicepconfig",
		Kind:          "SCOPE.Config",
		Subtype:       "bicepconfig",
		SourceFile:    p,
		StartLine:     1,
		EndLine:       strings.Count(src, "\n") + 1,
		Language:      lang,
		QualityScore:  0.85,
		QualifiedName: p,
		Metadata: map[string]interface{}{
			"subtype":  "bicepconfig",
			"iac_tool": "bicep",
			"label":    "bicepconfig.json",
		},
	}

	var parsed bicepConfig
	if err := json.Unmarshal([]byte(src), &parsed); err != nil {
		return []types.EntityRecord{cfg}
	}

	out := []types.EntityRecord{cfg}
	// Deterministic order: scheme (br, ts), then alias name.
	for _, scheme := range []string{"br", "ts"} {
		aliases := parsed.ModuleAliases[scheme]
		names := make([]string, 0, len(aliases))
		for name := range aliases {
			names = append(names, name)
		}
		sortStrings(names)
		for _, name := range names {
			a := aliases[name]
			registry := "acr"
			if scheme == "ts" {
				registry = "template-spec"
			} else if name == "public" || strings.HasPrefix(a.Registry, "mcr.microsoft.com") {
				registry = "mcr"
			}
			meta := map[string]interface{}{
				"subtype":         "module-alias",
				"iac_tool":        "bicep",
				"label":           name,
				"registry_scheme": scheme,
				"module_registry": registry,
			}
			if a.Registry != "" {
				meta["registry"] = a.Registry
			}
			if a.ModulePath != "" {
				meta["module_path"] = a.ModulePath
			}
			if a.Subscription != "" {
				meta["subscription"] = a.Subscription
			}
			if a.ResourceGroup != "" {
				meta["resource_group"] = a.ResourceGroup
			}
			out = append(out, types.EntityRecord{
				Name:          scheme + "/" + name,
				Kind:          "SCOPE.Schema",
				Subtype:       "module-alias",
				SourceFile:    p,
				StartLine:     1,
				EndLine:       1,
				Language:      lang,
				QualityScore:  0.85,
				QualifiedName: scheme + "/" + name,
				Metadata:      meta,
			})
		}
	}
	return out
}

// sortStrings sorts a string slice ascending (small local helper to avoid an
// extra import in a file that otherwise has no sort dependency).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// lineOf returns the 1-indexed line number of byte offset off in src.
func lineOf(src string, off int) int {
	if off > len(src) {
		off = len(src)
	}
	return strings.Count(src[:off], "\n") + 1
}
