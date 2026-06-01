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
	"os"
	"path/filepath"
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

// helmWithScopeRe matches a `with`/`range` action whose pipeline head is a
// `.Values.<path>` expression, so the block body's bare `.field` references can
// be re-rooted at that path. Captures the dotted path (group 1). We only re-root
// for the simple `{{- with .Values.foo }}` / `{{- range .Values.bar }}` shapes
// (no leading function), which is the overwhelmingly common case in real charts.
var helmWithScopeRe = regexp.MustCompile(`^(?:with|range)\s+\$?\.Values\.([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)\s*$`)

// helmScopedFieldRe matches a bare `.field` (NOT `.Values.` / `.Release.` / a
// built-in object) used inside a `with`/`range` block — e.g. `.bar` when the
// block opened with `with .Values.foo`. The path resolves to `<scope>.bar`.
var helmScopedFieldRe = regexp.MustCompile(`(^|[^A-Za-z0-9_.$])\.([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)`)

// helmBuiltinObjects are the Go-template/Helm built-in roots that a bare `.X`
// must NOT be confused with when re-rooting inside a with/range scope.
var helmBuiltinObjects = map[string]bool{
	"Values": true, "Release": true, "Chart": true, "Files": true,
	"Capabilities": true, "Template": true, "Subcharts": true,
}

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

	addVal := func(path string) {
		if path == "" || seenVals[path] {
			return
		}
		seenVals[path] = true
		valueRefs = append(valueRefs, path)
	}

	// scopeStack holds the active `with`/`range` value scopes (dotted .Values
	// paths). An empty string marks a block that opened on a non-.Values pipeline
	// (e.g. `with .Chart`), under which bare `.field` references cannot be
	// resolved to a values key and are therefore ignored.
	var scopeStack []string

	collect := func(body string) {
		// Direct `.Values.<path>` references (any nesting depth).
		for _, m := range helmValuesRefRe.FindAllStringSubmatch(body, -1) {
			addVal(m[1])
		}
		// Bare `.field` references re-rooted at the innermost .Values scope.
		if len(scopeStack) > 0 {
			scope := scopeStack[len(scopeStack)-1]
			if scope != "" {
				for _, m := range helmScopedFieldRe.FindAllStringSubmatch(body, -1) {
					head := m[2]
					if firstSeg := head; !helmBuiltinObjects[firstDotSeg(firstSeg)] {
						addVal(scope + "." + head)
					}
				}
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
		// Collect references from the raw line before mutating it. Process each
		// action in source order so with/range scope pushes take effect for the
		// references that follow on the SAME line, and the matching `end` pops.
		for _, m := range helmActionRe.FindAllStringSubmatch(line, -1) {
			body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m[1]), "-"))
			tok := firstToken(body)
			switch tok {
			case "with", "range":
				collect(m[1])
				if sm := helmWithScopeRe.FindStringSubmatch(body); sm != nil {
					// Re-root nested scope: a `with` inside another `with` whose
					// head itself is a bare field is rare; resolve against parent.
					scopeStack = append(scopeStack, sm[1])
				} else {
					scopeStack = append(scopeStack, "")
				}
			case "end":
				if len(scopeStack) > 0 {
					scopeStack = scopeStack[:len(scopeStack)-1]
				}
			default:
				collect(m[1])
			}
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

// firstDotSeg returns the first dot-delimited segment of a path (e.g.
// "Values.x" → "Values", "bar" → "bar"). Used to reject bare references that are
// actually built-in objects when re-rooting inside a with/range scope.
func firstDotSeg(path string) string {
	if i := strings.IndexByte(path, '.'); i >= 0 {
		return path[:i]
	}
	return path
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
	topPairs := topLevelMappings(root)
	var entities []types.EntityRecord

	// Subchart override blocks: a top-level key in the PARENT values.yaml whose
	// name matches a declared subchart (name or alias) is an override block — its
	// nested keys override the subchart's own values keys (the cross-chart values
	// data-flow). The authoritative subchart set comes from the sibling
	// Chart.yaml; when RepoRoot is unset (direct unit-test calls) we fall back to
	// a content-side channel the test wires through.
	subcharts := helmSiblingSubcharts(file)

	var walk func(pairs []*sitter.Node, prefix string, subchart string)
	walk = func(pairs []*sitter.Node, prefix string, subchart string) {
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

			// Detect entry into a subchart override block at the top level.
			curSub := subchart
			if prefix == "" && subcharts[key] {
				curSub = key
			}

			ent := helmValuesKeyEntity(path, file, start, end)

			// Inside a subchart override block, the path RELATIVE to the block
			// name overrides the subchart's own values key. e.g. parent
			// `postgresql.auth.username` overrides subchart postgresql's
			// `auth.username`. The top-level block key itself has no relative
			// remainder, so it is skipped.
			if curSub != "" && path != curSub {
				relPath := strings.TrimPrefix(path, curSub+".")
				rel := types.RelationshipRecord{
					FromID: file.Path,
					ToID:   "helm_subchart_values:" + curSub + ":" + relPath,
					Kind:   string(types.RelationshipKindOverrides),
					Properties: map[string]string{
						"override_kind": "helm_subchart_value",
						"subchart":      curSub,
						"values_path":   relPath,
						"parent_path":   path,
					},
				}
				ent.Relationships = append(ent.Relationships, rel)
			}

			val := pairValueNode(p)
			childBM := getBlockMapping(val)
			if childBM != nil {
				// Nested map → recurse; emit the intermediate node too so a
				// `.Values.parent` reference (whole sub-tree) can still bind.
				entities = append(entities, ent)
				var childPairs []*sitter.Node
				for i := range childBM.ChildCount() {
					c := childBM.Child(int(i))
					if c != nil && c.Type() == "block_mapping_pair" {
						childPairs = append(childPairs, c)
					}
				}
				walk(childPairs, path, curSub)
				continue
			}
			// Leaf scalar or sequence → values key.
			entities = append(entities, ent)
		}
	}
	walk(topPairs, "", "")

	return entities
}

// helmSiblingSubcharts returns the set of subchart names AND aliases declared in
// the Chart.yaml that sits beside the given values.yaml, so the parent values
// override blocks can be matched. Returns an empty (non-nil) set when RepoRoot
// is unset or no sibling Chart.yaml is found — override edges are simply not
// emitted in that case (hermetic, no false positives).
func helmSiblingSubcharts(file extractor.FileInput) map[string]bool {
	out := map[string]bool{}
	if file.RepoRoot == "" {
		return out
	}
	dir := file.Path
	if idx := strings.LastIndexByte(dir, '/'); idx >= 0 {
		dir = dir[:idx]
	} else {
		dir = ""
	}
	for _, name := range []string{"Chart.yaml", "Chart.yml"} {
		rel := name
		if dir != "" {
			rel = dir + "/" + name
		}
		data, err := os.ReadFile(filepath.Join(file.RepoRoot, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		for n, a := range parseChartSubcharts(data) {
			out[n] = true
			if a != "" {
				out[a] = true
			}
		}
		break
	}
	return out
}

// parseChartSubcharts extracts the dependency name→alias map from Chart.yaml
// bytes. Lightweight line scanner (avoids a second tree-sitter parse from inside
// the values extractor): it walks the `dependencies:` block and pairs each
// `- name:` with an optional sibling `alias:`.
func parseChartSubcharts(data []byte) map[string]string {
	out := map[string]string{}
	lines := strings.Split(string(data), "\n")
	inDeps := false
	curName := ""
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		// Leaving the dependencies block: a new top-level key (no indent).
		if inDeps && ln != "" && ln[0] != ' ' && ln[0] != '\t' && ln[0] != '#' && !strings.HasPrefix(trimmed, "-") {
			inDeps = false
		}
		if strings.HasPrefix(trimmed, "dependencies:") {
			inDeps = true
			continue
		}
		if !inDeps {
			continue
		}
		// New list item resets the current dependency.
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			curName = ""
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		}
		if v, ok := helmInlineValue(trimmed, "name"); ok {
			curName = v
			if _, seen := out[curName]; !seen {
				out[curName] = ""
			}
		}
		if v, ok := helmInlineValue(trimmed, "alias"); ok && curName != "" {
			out[curName] = v
		}
	}
	return out
}

// helmInlineValue parses a `key: value` fragment, returning the unquoted value
// and whether the key matched.
func helmInlineValue(s, key string) (string, bool) {
	if !strings.HasPrefix(s, key+":") {
		return "", false
	}
	v := strings.TrimSpace(strings.TrimPrefix(s, key+":"))
	v = strings.Trim(v, `"'`)
	if v == "" {
		return "", false
	}
	return v, true
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

// helmDefineRe matches a `{{- define "name" }}` action.
var helmDefineRe = regexp.MustCompile(`{{-?\s*define\s+"([^"]+)"\s*-?}}`)

// helmEndRe matches a `{{- end }}` action.
var helmEndRe = regexp.MustCompile(`{{-?\s*end\s*-?}}`)

// helmIncludeArgRe captures both the named-template target AND the argument
// context passed to an `include "name" <arg>` / `template "name" <arg>` call.
// Group 1 = template name, group 2 = the argument expression ("." , "$",
// "(dict ...)", ".Values.foo", etc.) up to the action close.
var helmIncludeArgRe = regexp.MustCompile(`(?:include|template)\s+"([^"]+)"\s*([^})]*?)\s*[-)}]`)

// extractHelmHelpers processes a _helpers.tpl: emits one SCOPE.Operation
// "named_template" entity per `{{- define "name" }}` block. Each named template
// then carries the data-flow edges sourced FROM its own body:
//
//   - INCLUDES edge named_template → named_template for every `include "other"`
//     call inside the body, with the passed argument expression recorded on the
//     edge (the define/include arg-passing flow).
//   - BINDS edge named_template → values_key for every `.Values.<path>` the body
//     references, so helper-resolved values flow is captured the same way
//     templates' bindings are.
//
// Includes that sit OUTSIDE any define block (top-level in the .tpl) keep the
// previous file-anchored behaviour so nothing is lost.
func extractHelmHelpers(file extractor.FileInput) []types.EntityRecord {
	src := string(file.Content)
	var entities []types.EntityRecord

	lines := strings.Split(src, "\n")

	// First pass: emit named_template entities, tracking each block's line span.
	type defBlock struct {
		name       string
		start, end int // 1-based inclusive
	}
	type openDef struct {
		name  string
		start int
	}
	var stack []openDef
	var blocks []defBlock
	seen := map[string]bool{}

	for i, line := range lines {
		if m := helmDefineRe.FindStringSubmatch(line); m != nil {
			stack = append(stack, openDef{name: m[1], start: i + 1})
			continue
		}
		if helmEndRe.MatchString(line) && len(stack) > 0 {
			d := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if d.name == "" {
				continue
			}
			blocks = append(blocks, defBlock{name: d.name, start: d.start, end: i + 1})
			if seen[d.name] {
				continue
			}
			seen[d.name] = true
			ent := entity(
				"SCOPE.Operation", d.name, "named_template",
				"helm_template:"+d.name,
				file.Path, "yaml", d.start, i+1,
			)
			entities = append(entities, ent)
		}
	}

	// definerAt returns the name of the INNERMOST define block containing the
	// 1-based line, or "" when the line is outside every define.
	definerAt := func(line int) string {
		best := ""
		bestSpan := 1 << 30
		for _, b := range blocks {
			if line >= b.start && line <= b.end {
				if span := b.end - b.start; span < bestSpan {
					bestSpan = span
					best = b.name
				}
			}
		}
		return best
	}

	// attachFromDefiner appends rel to the named_template entity for `definer`,
	// or routes it through the file anchor when definer is "".
	attachFromDefiner := func(definer string, rel types.RelationshipRecord) {
		if definer == "" {
			rel.FromID = file.Path
			entities = appendHelmEdge(entities, file, rel)
			return
		}
		rel.FromID = "helm_template:" + definer
		for i := range entities {
			if entities[i].Subtype == "named_template" && entities[i].Name == definer {
				entities[i].Relationships = append(entities[i].Relationships, rel)
				return
			}
		}
		// Defensive: definer block had no entity (duplicate name) — anchor it.
		entities = appendHelmEdge(entities, file, rel)
	}

	// Second pass: per-line, attribute include/define-arg and .Values edges to the
	// enclosing define block. De-dup per (definer,target) so a helper that calls
	// the same include twice yields one edge.
	seenInc := map[string]bool{}
	seenBind := map[string]bool{}
	for i, line := range lines {
		definer := definerAt(i + 1)

		for _, m := range helmIncludeArgRe.FindAllStringSubmatch(line, -1) {
			target := m[1]
			arg := strings.TrimSpace(m[2])
			if target == definer {
				continue // a define never includes itself meaningfully
			}
			key := definer + "\x00" + target
			if seenInc[key] {
				continue
			}
			seenInc[key] = true
			props := map[string]string{
				"include_kind":  "helm_include",
				"template_name": target,
			}
			if arg != "" {
				props["include_arg"] = arg
				props["arg_flow"] = helmArgFlowKind(arg)
			}
			attachFromDefiner(definer, types.RelationshipRecord{
				ToID:       "helm_template:" + target,
				Kind:       "INCLUDES",
				Properties: props,
			})
		}

		for _, m := range helmValuesRefRe.FindAllStringSubmatch(line, -1) {
			path := m[1]
			key := definer + "\x00" + path
			if seenBind[key] {
				continue
			}
			seenBind[key] = true
			attachFromDefiner(definer, types.RelationshipRecord{
				ToID: "helm_values:" + path,
				Kind: "BINDS",
				Properties: map[string]string{
					"binding_kind": "helm_values_ref",
					"values_path":  path,
				},
			})
		}
	}

	return entities
}

// helmArgFlowKind classifies the argument expression passed to an include so the
// edge records HOW context flows into the callee: the root context (`.` / `$`),
// an explicit dict construction (`dict "k" .v`), or a narrowed sub-context
// (`.Values.foo`, `.bar`).
func helmArgFlowKind(arg string) string {
	switch {
	case arg == "." || arg == "$":
		return "root_context"
	case strings.HasPrefix(arg, "(dict") || strings.HasPrefix(arg, "dict "):
		return "dict"
	case strings.HasPrefix(arg, "(list") || strings.HasPrefix(arg, "list "):
		return "list"
	default:
		return "scoped_context"
	}
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
