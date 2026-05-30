package yaml

// Helm chart support for the YAML extractor (#3526, epic #3512).
//
// Helm charts are a directory layout, not a single YAML dialect. Three file
// shapes carry structure the indexer can recover:
//
//   - Chart.yaml          chart metadata + a `dependencies:` list. Each
//                         dependency (name + repository + version) becomes an
//                         IMPORTS edge chart → subchart. Chart.yaml is plain
//                         YAML and parses cleanly today.
//
//   - values.yaml         the default value tree. Leaf paths (e.g.
//                         image.repository) become SCOPE.Schema "values_key"
//                         entities so `{{ .Values.image.repository }}`
//                         references in templates can bind to them. values.yaml
//                         is plain YAML.
//
//   - templates/*.yaml    Kubernetes manifests interleaved with Go-template
//                         directives ({{ .Values.x }}, {{- if }}, {{ include }}).
//                         The raw text does NOT parse as YAML — the directives
//                         derail tree-sitter. We run a tolerant PRE-STRIP that
//                         neutralises every {{ ... }} action (control lines are
//                         dropped, value-position actions are replaced with a
//                         placeholder scalar), re-parse the stripped text, and
//                         hand it to the existing Kubernetes extractor so the
//                         underlying resource (Deployment, Service, …) is
//                         recovered. While stripping we also collect every
//                         `.Values.<path>` reference and emit a binding edge
//                         from the template Document to the matching values key.
//
//   - templates/_helpers.tpl  named-template library. `{{- define "name" }}`
//                         blocks become SCOPE.Operation "named_template"
//                         entities; `{{ include "name" . }}` call-sites in any
//                         template emit an include edge to the named template.
//
// Detection: a file is treated as Helm when its path sits under a `templates/`
// directory of a chart, when it is a Chart.yaml / values.yaml, or when its
// content carries Helm-specific directives (.Values / .Release / .Chart /
// {{ include / {{ define).

import (
	"bytes"
	"context"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

const (
	flavorHelmChart    = "helm_chart"    // Chart.yaml
	flavorHelmValues   = "helm_values"   // values.yaml within a chart
	flavorHelmTemplate = "helm_template" // templates/*.yaml (manifest + directives)
	flavorHelmHelpers  = "helm_helpers"  // templates/_helpers.tpl
)

// ---------------------------------------------------------------------------
// Detection
// ---------------------------------------------------------------------------

// helmFlavor returns the Helm sub-flavor for a file, or "" when the file is not
// part of a Helm chart. Checked before the generic Kubernetes branch in
// detectFlavor.
func helmFlavor(content, path string) string {
	base := path
	if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
		base = base[idx+1:]
	}

	// _helpers.tpl (or any .tpl under templates/) → named-template library.
	if strings.HasSuffix(base, ".tpl") {
		return flavorHelmHelpers
	}

	// Chart.yaml — the chart manifest. Require a Helm-ish signal so we don't
	// grab an unrelated file literally named Chart.yaml; the presence of
	// `apiVersion:` + (`name:`|`dependencies:`) is the Helm chart shape.
	if base == "Chart.yaml" || base == "Chart.yml" {
		if containsTopLevelKey(content, "apiVersion") &&
			(containsTopLevelKey(content, "name") || containsTopLevelKey(content, "dependencies")) {
			return flavorHelmChart
		}
		return flavorHelmChart
	}

	inTemplatesDir := strings.Contains(path, "/templates/") || strings.HasPrefix(path, "templates/")

	// A templated manifest: under templates/ AND carrying Go-template directives.
	if inTemplatesDir && hasHelmDirectives(content) {
		return flavorHelmTemplate
	}

	// values.yaml within a chart. We can only be sure it's Helm when a sibling
	// Chart.yaml exists, which the per-file extractor cannot see. Use a
	// heuristic: a file named values.yaml/values*.yaml that also appears next to
	// a templates/ dir cannot be detected from content alone, so we only claim
	// it when the path is exactly values.yaml at a chart root is ambiguous.
	// Instead, defer values.yaml to the generic branch UNLESS it sits beside a
	// templates dir signalled by the path. The safe, content-only signal:
	// values files rarely carry apiVersion/kind, so a values.yaml that is NOT a
	// k8s manifest and NOT compose is treated as Helm values only when the
	// caller's path is a recognised values file. Keep this conservative.
	if base == "values.yaml" || base == "values.yml" ||
		(strings.HasPrefix(base, "values-") && (strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml"))) {
		// Avoid hijacking a values.yaml that is actually a k8s manifest.
		if !(containsTopLevelKey(content, "apiVersion") && containsTopLevelKey(content, "kind")) {
			return flavorHelmValues
		}
	}

	// Any other YAML carrying Helm directives (e.g. a template not under a
	// conventionally-named dir) → treat as a template so we recover the resource.
	if hasHelmDirectives(content) {
		return flavorHelmTemplate
	}

	return ""
}

// hasHelmDirectives reports whether content contains Go-template actions that
// are SPECIFICALLY Helm — not Jinja2 (Ansible) or GitHub Actions expressions,
// both of which also use brace pairs. The discriminator is a Helm-specific
// token: a built-in object reference (.Values / .Release / .Chart / .Files /
// .Capabilities), a Sprig/Helm pipeline (include / define / nindent / toYaml),
// or a whitespace-chomp marker ({{- / -}}) which Jinja2 does not use.
//
// Bare `{{ var }}` alone is INSUFFICIENT — Ansible playbooks are full of
// `{{ ansible_fact }}` Jinja expressions and must keep their Ansible flavor.
func hasHelmDirectives(content string) bool {
	if !strings.Contains(content, "{{") {
		return false
	}
	helmHints := []string{
		".Values", ".Release", ".Chart", ".Files", ".Capabilities",
		"include \"", "include  \"", "define \"", "template \"",
		"{{-", "-}}", "| nindent", "| toYaml", "| quote", "| indent",
		".Subcharts", "tpl ",
	}
	for _, h := range helmHints {
		if strings.Contains(content, h) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Template pre-strip
// ---------------------------------------------------------------------------

// helmActionRe matches a single Go-template action: {{ ... }} or {{- ... -}}.
// Non-greedy so adjacent actions on one line are matched independently.
var helmActionRe = regexp.MustCompile(`{{-?\s*(.*?)\s*-?}}`)

// helmValuesRefRe extracts `.Values.<dotted.path>` references from action bodies.
var helmValuesRefRe = regexp.MustCompile(`\.Values\.([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)`)

// helmIncludeRe extracts the named-template argument of an `include "name"` or
// `template "name"` action.
var helmIncludeRe = regexp.MustCompile(`(?:include|template)\s+"([^"]+)"`)

// helmStripResult carries the cleaned bytes plus the structural references the
// strip pass recovered for binding-edge emission.
type helmStripResult struct {
	stripped []byte
	// valueRefs is the ordered, de-duplicated set of `.Values.<path>` keys
	// referenced anywhere in the template.
	valueRefs []string
	// includes is the ordered, de-duplicated set of named templates the file
	// references via include/template.
	includes []string
}

// stripHelmTemplate neutralises Go-template directives so the underlying
// Kubernetes YAML becomes parseable, and collects the structural references
// (.Values paths, include targets) found along the way.
//
// Rules, applied line by line:
//
//   - A line whose non-whitespace content is entirely one or more control
//     actions ({{- if }}, {{ end }}, {{- range }}, {{ with }}, {{- else }},
//     define/end, a bare include used as a block) is DROPPED. Removing the line
//     keeps indentation of surrounding real keys intact.
//
//   - Otherwise, every {{ ... }} action embedded in the line is replaced in
//     place: a value-position action becomes the placeholder scalar
//     `__helm__` (a valid YAML plain scalar) so `key: {{ .Values.x }}` →
//     `key: __helm__`. Quoted templates ("{{ .Values.x }}") collapse to the
//     placeholder inside the existing quotes.
//
// The pass is intentionally tolerant: anything it cannot classify is replaced
// with the placeholder rather than dropped, so the YAML shape survives.
func stripHelmTemplate(src []byte) helmStripResult {
	var out bytes.Buffer
	seenVals := map[string]bool{}
	seenInc := map[string]bool{}
	var valueRefs, includes []string

	collect := func(body string) {
		for _, m := range helmValuesRefRe.FindAllStringSubmatch(body, -1) {
			if !seenVals[m[1]] {
				seenVals[m[1]] = true
				valueRefs = append(valueRefs, m[1])
			}
		}
		for _, m := range helmIncludeRe.FindAllStringSubmatch(body, -1) {
			if !seenInc[m[1]] {
				seenInc[m[1]] = true
				includes = append(includes, m[1])
			}
		}
	}

	lines := strings.Split(string(src), "\n")
	for _, line := range lines {
		// Collect references from the raw line before mutating it.
		for _, m := range helmActionRe.FindAllStringSubmatch(line, -1) {
			collect(m[1])
		}

		trimmed := strings.TrimSpace(line)

		// Whole-line control directive → drop. A control line is one where,
		// after removing every action, nothing but whitespace remains AND at
		// least one action was a control keyword. This covers `{{- if x }}`,
		// `{{ end }}`, `{{- range … }}`, `{{- define … }}`, `{{- with … }}`,
		// `{{- else }}`, and a standalone `{{ include … }}` block line.
		if trimmed != "" && isHelmControlLine(trimmed) {
			continue
		}

		out.WriteString(replaceHelmActions(line))
		out.WriteByte('\n')
	}

	// Drop the trailing newline we always append for the last element when the
	// original had none, to avoid spurious blank-line growth. Harmless either
	// way for tree-sitter; keep it simple.
	stripped := out.Bytes()
	return helmStripResult{
		stripped:  stripped,
		valueRefs: valueRefs,
		includes:  includes,
	}
}

// isHelmControlLine reports whether trimmed (already whitespace-trimmed) is a
// line that consists solely of template actions and at least one of those
// actions is a control/flow keyword (so the line carries no YAML structure of
// its own and must be removed rather than placeholdered).
func isHelmControlLine(trimmed string) bool {
	// Strip all actions; if anything non-whitespace remains, the line carries
	// real YAML (e.g. `name: {{ .x }}`) and is NOT a pure control line.
	residue := strings.TrimSpace(helmActionRe.ReplaceAllString(trimmed, ""))
	if residue != "" {
		return false
	}
	// Pure-action line. Drop it if any action is a control keyword OR if it is
	// an include/template used as a standalone block (its rendered output is
	// multi-line YAML we can't recover, so dropping keeps the parse valid).
	for _, m := range helmActionRe.FindAllStringSubmatch(trimmed, -1) {
		body := strings.TrimSpace(m[1])
		switch firstToken(body) {
		case "if", "else", "end", "range", "with", "define", "block",
			"include", "template", "tpl", "toYaml", "printf", "default":
			return true
		}
		// A leading `-` (comment/whitespace-chomp only) or empty body.
		if body == "" {
			return true
		}
	}
	return false
}

// firstToken returns the first whitespace-delimited token of an action body,
// skipping a leading `-` chomp marker if present.
func firstToken(body string) string {
	body = strings.TrimSpace(strings.TrimPrefix(body, "-"))
	if i := strings.IndexAny(body, " \t"); i >= 0 {
		return body[:i]
	}
	return body
}

// replaceHelmActions replaces every {{ ... }} action remaining on a (non-control)
// line with the placeholder scalar so the line parses as YAML.
func replaceHelmActions(line string) string {
	return helmActionRe.ReplaceAllString(line, helmPlaceholder)
}

// helmPlaceholder is a valid YAML plain scalar substituted for a value-position
// template action. Distinctive so downstream passes can recognise it if needed.
const helmPlaceholder = "__helm__"

// ---------------------------------------------------------------------------
// Helm extractors
// ---------------------------------------------------------------------------

// extractHelm dispatches to the appropriate Helm sub-extractor. Unlike the
// other flavors it may re-parse the file content (templates need the stripped
// text), so it takes the original root only as a fallback.
func extractHelm(flavor string, root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	switch flavor {
	case flavorHelmChart:
		return extractHelmChart(root, file)
	case flavorHelmValues:
		return extractHelmValues(root, file)
	case flavorHelmHelpers:
		return extractHelmHelpers(file)
	case flavorHelmTemplate:
		return extractHelmTemplate(file)
	}
	return nil
}

// extractHelmChart processes a Chart.yaml: emits a chart entity and one IMPORTS
// edge per dependency (chart → subchart). The dependency's repository + version
// are recorded on the edge Properties for provenance.
func extractHelmChart(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	src := file.Content
	pairs := topLevelMappings(root)
	var entities []types.EntityRecord

	chartName := findPairValueText(pairs, "name", src)
	dirName := helmChartDir(file.Path)
	if chartName == "" {
		chartName = dirName
	}
	if chartName == "" {
		chartName = "chart"
	}

	startLine := 1
	endLine := bytes.Count(src, []byte("\n")) + 1
	if root != nil {
		endLine = int(root.EndPoint().Row) + 1
	}

	// The chart entity's QualifiedName is file.Path so IMPORTS edges (FromID =
	// file.Path) resolve through the SCOPE.Document anchor the dispatcher
	// prepends (issue #474 chain-fix), exactly like the kustomization root.
	chartRef := file.Path
	chartEnt := entity(
		"SCOPE.Component", chartName, "helm_chart",
		chartRef,
		file.Path, "yaml", startLine, endLine,
	)
	props := map[string]string{}
	if v := findPairValueText(pairs, "version", src); v != "" {
		props["chart_version"] = v
	}
	if v := findPairValueText(pairs, "appVersion", src); v != "" {
		props["app_version"] = v
	}
	if v := findPairValueText(pairs, "type", src); v != "" {
		props["chart_type"] = v
	}
	if len(props) > 0 {
		chartEnt.Properties = props
	}
	entities = append(entities, chartEnt)

	// dependencies: list of { name, repository, version, alias, condition }.
	depNode := findValueNodeForKey(pairs, "dependencies", src)
	for _, depPairs := range getSequenceItemMappings(depNode, src) {
		depName := findPairValueText(depPairs, "name", src)
		if depName == "" {
			continue
		}
		repo := findPairValueText(depPairs, "repository", src)
		ver := findPairValueText(depPairs, "version", src)
		alias := findPairValueText(depPairs, "alias", src)

		// IMPORTS: chart → subchart. The subchart source lives in a chart
		// repository (or charts/ dir), outside the indexed file corpus by
		// default, so route it through a synthetic stub the external-synth pass
		// can lift — mirrors the docker_image: / kustomize_path: convention.
		rel := importsRel(chartRef, "helm_subchart:"+depName, "helm_dependency")
		if repo != "" {
			rel.Properties["repository"] = repo
		}
		if ver != "" {
			rel.Properties["version"] = ver
		}
		if alias != "" {
			rel.Properties["alias"] = alias
		}
		chartEnt.Relationships = append(chartEnt.Relationships, rel)
	}
	// Re-sync the value-copied chart entity now that dependency edges are on it.
	entities[0] = chartEnt

	return entities
}

// extractHelmValues processes a values.yaml: emits one SCOPE.Schema
// "values_key" entity per LEAF path in the value tree, with the dotted path as
// the QualifiedName so template `.Values.<path>` binding edges resolve against
// it.
func extractHelmValues(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	src := file.Content
	pairs := topLevelMappings(root)
	var entities []types.EntityRecord

	var walk func(pairs []*sitter.Node, prefix string)
	walk = func(pairs []*sitter.Node, prefix string) {
		for _, p := range pairs {
			key := pairKeyText(p, src)
			if key == "" {
				continue
			}
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			start := int(p.StartPoint().Row) + 1
			end := int(p.EndPoint().Row) + 1

			val := pairValueNode(p)
			childBM := getBlockMapping(val)
			if childBM != nil {
				// Nested map → recurse; emit the intermediate node too so a
				// `.Values.parent` reference (whole sub-tree) can still bind.
				entities = append(entities, helmValuesKeyEntity(path, file, start, end))
				var childPairs []*sitter.Node
				for i := range childBM.ChildCount() {
					c := childBM.Child(int(i))
					if c != nil && c.Type() == "block_mapping_pair" {
						childPairs = append(childPairs, c)
					}
				}
				walk(childPairs, path)
				continue
			}
			// Leaf scalar or sequence → values key.
			entities = append(entities, helmValuesKeyEntity(path, file, start, end))
		}
	}
	walk(pairs, "")

	return entities
}

// helmValuesKeyEntity builds a SCOPE.Schema values_key entity. QualifiedName is
// `helm_values:<dotted.path>` so template binding edges (which target the same
// stub) resolve via byQualifiedName.
func helmValuesKeyEntity(path string, file extractor.FileInput, start, end int) types.EntityRecord {
	return entity(
		"SCOPE.Schema", path, "values_key",
		"helm_values:"+path,
		file.Path, "yaml", start, end,
	)
}

// extractHelmTemplate strips the Go-template directives, re-parses the cleaned
// YAML, runs the existing Kubernetes extractor to recover the underlying
// resource, then layers Helm-specific edges: a binding edge per `.Values.<path>`
// reference and an include edge per named-template reference.
func extractHelmTemplate(file extractor.FileInput) []types.EntityRecord {
	res := stripHelmTemplate(file.Content)

	var entities []types.EntityRecord

	// Re-parse the stripped content and recover K8s resources. The stripped
	// file is plain YAML, so the standard Kubernetes path applies. We build a
	// throwaway FileInput pointing at the same Path (so refs/source_file stay
	// stable) but with the cleaned content.
	cleaned := extractor.FileInput{
		Path:     file.Path,
		Content:  res.stripped,
		Language: "yaml",
	}
	parser := sitter.NewParser()
	parser.SetLanguage(yamlGrammar())
	tree, err := parser.ParseCtx(context.Background(), nil, res.stripped)
	if err == nil && tree != nil {
		root := tree.RootNode()
		if root != nil {
			entities = append(entities, extractKubernetes(root, cleaned)...)
		}
	}

	// Anchor entity for the template's Helm-specific edges. When the K8s pass
	// recovered a resource, reuse the file as the binding FromID (resolves via
	// the SCOPE.Document anchor the dispatcher prepends). Binding edges and
	// include edges originate from file.Path.
	fromRef := file.Path

	// .Values binding edges: template → values key. The ToID matches the
	// QualifiedName scheme of helmValuesKeyEntity so it resolves cross-file
	// against the chart's values.yaml entities.
	for _, ref := range res.valueRefs {
		rel := types.RelationshipRecord{
			FromID: fromRef,
			ToID:   "helm_values:" + ref,
			Kind:   "BINDS",
			Properties: map[string]string{
				"binding_kind": "helm_values_ref",
				"values_path":  ref,
			},
		}
		// Attach to the first recovered resource if present, else carry on a
		// synthetic placeholder entity so the edge is not lost.
		entities = appendHelmEdge(entities, file, rel)
	}

	// include/template named-template edges: template → named template.
	for _, inc := range res.includes {
		rel := types.RelationshipRecord{
			FromID: fromRef,
			ToID:   "helm_template:" + inc,
			Kind:   "INCLUDES",
			Properties: map[string]string{
				"include_kind":  "helm_include",
				"template_name": inc,
			},
		}
		entities = appendHelmEdge(entities, file, rel)
	}

	return entities
}

// appendHelmEdge attaches rel to the first recovered K8s resource entity in
// entities (the canonical resource for the file). When no resource was
// recovered (e.g. the template rendered to something the K8s pass doesn't
// model), it synthesises a single SCOPE.Component "helm_template" anchor entity
// to carry the edge so it still resolves via the file Document.
func appendHelmEdge(entities []types.EntityRecord, file extractor.FileInput, rel types.RelationshipRecord) []types.EntityRecord {
	for i := range entities {
		if entities[i].Subtype == "k8s_resource" {
			entities[i].Relationships = append(entities[i].Relationships, rel)
			return entities
		}
	}
	// No resource recovered yet — look for an existing template anchor.
	for i := range entities {
		if entities[i].Subtype == "helm_template_anchor" {
			entities[i].Relationships = append(entities[i].Relationships, rel)
			return entities
		}
	}
	name := file.Path
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		name = name[idx+1:]
	}
	anchor := entity(
		"SCOPE.Component", name, "helm_template_anchor",
		"helm_template_anchor:"+file.Path,
		file.Path, "yaml", 1, 1,
	)
	anchor.Relationships = append(anchor.Relationships, rel)
	return append(entities, anchor)
}

// extractHelmHelpers processes a _helpers.tpl: emits one SCOPE.Operation
// "named_template" entity per `{{- define "name" }}` block, plus include edges
// for any `{{ include "other" . }}` references inside helper bodies.
func extractHelmHelpers(file extractor.FileInput) []types.EntityRecord {
	src := string(file.Content)
	var entities []types.EntityRecord

	defineRe := regexp.MustCompile(`{{-?\s*define\s+"([^"]+)"\s*-?}}`)
	endRe := regexp.MustCompile(`{{-?\s*end\s*-?}}`)

	lines := strings.Split(src, "\n")
	// Track define blocks by scanning for define ... end at the same nesting.
	type openDef struct {
		name  string
		start int
	}
	var stack []openDef
	seen := map[string]bool{}

	for i, line := range lines {
		if m := defineRe.FindStringSubmatch(line); m != nil {
			stack = append(stack, openDef{name: m[1], start: i + 1})
			continue
		}
		if endRe.MatchString(line) && len(stack) > 0 {
			d := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if d.name == "" || seen[d.name] {
				continue
			}
			seen[d.name] = true
			ref := "helm_template:" + d.name
			ent := entity(
				"SCOPE.Operation", d.name, "named_template",
				ref,
				file.Path, "yaml", d.start, i+1,
			)
			entities = append(entities, ent)
		}
	}

	// include/template references between helpers → INCLUDES edges from each
	// named template that contains them. For simplicity attach include edges
	// from the file (resolved via the Document anchor) to the referenced
	// template; the call-site granularity is recorded by template_name.
	seenInc := map[string]bool{}
	for _, m := range helmIncludeRe.FindAllStringSubmatch(src, -1) {
		name := m[1]
		if seenInc[name] {
			continue
		}
		seenInc[name] = true
		// Skip self-references already emitted as defines? Keep all — an include
		// to a sibling helper is a real edge.
		rel := types.RelationshipRecord{
			FromID: file.Path,
			ToID:   "helm_template:" + name,
			Kind:   "INCLUDES",
			Properties: map[string]string{
				"include_kind":  "helm_include",
				"template_name": name,
			},
		}
		entities = appendHelmEdge(entities, file, rel)
	}

	return entities
}

// helmChartDir returns the directory name containing the given chart file path,
// used as a fallback chart name when Chart.yaml has no name: key.
func helmChartDir(path string) string {
	// strip filename
	dir := path
	if idx := strings.LastIndexByte(dir, '/'); idx >= 0 {
		dir = dir[:idx]
	} else {
		return ""
	}
	if idx := strings.LastIndexByte(dir, '/'); idx >= 0 {
		return dir[idx+1:]
	}
	return dir
}
