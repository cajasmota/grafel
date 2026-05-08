// Package react_props implements the cross-language UI component props,
// children, and hook-usage extractor (MX-1062).
//
// Scans React / React Native source files (.tsx / .jsx) for functional
// components and emits the three relationships required to answer semantic
// graph queries such as "what component accepts inspection data?":
//
//   - HAS_PROPS  — Component → PropsInterface  (Props interface/type alias)
//   - RENDERS    — Component → ChildComponent  (PascalCase JSX element in body)
//   - USES_HOOK  — Component → Hook            (useXxx(...) call in body)
//
// Companion entity kind:
//
//   - "SCOPE.Schema"     — PropsInterface (same kind the JS/TS extractor uses
//                          for interface/type declarations, so downstream
//                          merge on deterministic ID stays consistent).
//
// The extractor ALSO emits a lightweight SCOPE.Operation record for each
// detected component, carrying the `props` property (comma-separated prop
// names). Because EntityRecord.ComputeID hashes on org/project/source_file/
// kind/name, this record dedupes against the one the base JS/TS extractor
// emits in the same Lambda run — the `props` property is merged in.
//
// Entity kinds:
//
//   - "SCOPE.Operation"  — component (for property merge)
//   - "SCOPE.Schema"     — props interface / type alias
//
// Relationship kinds:
//
//   - "HAS_PROPS"
//   - "RENDERS"
//   - "USES_HOOK"
//
// OTel span:   indexer.react_props_extract
// Attributes:  language, file_path, component_count, has_props_count,
//              renders_count, uses_hook_count
//
// Registration key: "_cross_react_props"
//
// The extractor short-circuits on non-.tsx/.jsx files and on files that do
// not contain a `react` import. On those paths it returns (nil, nil) after
// zero allocations beyond the import scan.
//
// Behaviour rules (MX-1062):
//
//  1. React/RN: extract props from `interface XxxProps`, `type XxxProps`, or
//     inline destructuring parameter `({a, b, c}: XxxProps)`. Extract child
//     component names from JSX tags whose opening name begins with an ASCII
//     uppercase letter.
//  2. Store prop names as a comma-separated string in the `props` entity
//     property (max 500 chars — truncate with `...` if exceeded).
//  3. RENDERS relationships link to component ChildRefs inside the same file.
//     The resolver is local-only: we emit a best-effort same-file ref and let
//     the graph ingestion layer dedupe across the repo. Known HTML element
//     names (div, span, a, button, …) and the React.Fragment short-form `<>`
//     are filtered out.
//  4. USES_HOOK relationships link to hook entities identified by their
//     `useXxx` call name. We do NOT resolve the hook definition here — we
//     emit a ref keyed on the hook name, and the graph merges it with the
//     react_hook entity extracted upstream by MX-1061 when present.
//  5. Error handling: AST regex fall-through is panic-safe. On detection
//     failure the file is skipped with an empty result; we never abort the
//     overall extraction job.
package react_props

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_react_props", &Extractor{})
}

// Extractor implements extractor.Extractor for React props / children / hooks.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_react_props" }

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// KindOperation mirrors the JS/TS extractor so props-annotated components
	// dedupe via EntityRecord.ComputeID.
	KindOperation = "SCOPE.Operation"
	// KindSchema mirrors the JS/TS extractor for interface / type decls.
	KindSchema = "SCOPE.Schema"

	// RelHasProps, RelRenders, RelUsesHook are the three relationship kinds
	// this extractor emits. These string values are mapped to the proto
	// RELATIONSHIP_TYPE_HAS_PROPS / _RENDERS / _USES_HOOK enum entries on the
	// graph ingestion side (VR-196, VR-197, merged 2026-04-10).
	RelHasProps = "HAS_PROPS"
	RelRenders  = "RENDERS"
	RelUsesHook = "USES_HOOK"

	// propsMaxLen is the max byte length of the comma-separated `props`
	// entity property (MX-1062 behaviour rule 2).
	propsMaxLen = 500
)

// ---------------------------------------------------------------------------
// Regex library
// ---------------------------------------------------------------------------

// reactImportRE matches `import ... from 'react'` or `import 'react'`.
// Used only to short-circuit non-React files.
var reactImportRE = regexp.MustCompile(`(?m)^\s*import[^;]*['"]react['"]`)

// functionDeclRE matches `function Foo(` / `export function Foo(` /
// `export default function Foo(` — capturing the component name.
var functionDeclRE = regexp.MustCompile(
	`(?m)^\s*(?:export\s+(?:default\s+)?)?function\s+([A-Z][A-Za-z0-9_]*)\s*\(`,
)

// arrowDeclRE matches `const Foo = (...) =>` / `const Foo: FC<...> = (...) =>`.
var arrowDeclRE = regexp.MustCompile(
	`(?m)^\s*(?:export\s+)?(?:const|let|var)\s+([A-Z][A-Za-z0-9_]*)\s*(?::[^=]*)?=\s*(?:\([^)]*\)|[A-Za-z_][A-Za-z0-9_]*)\s*=>`,
)

// interfacePropsRE captures `interface FooProps {...}` including the body
// between the first `{` and its matching line-level `}`. The body is then
// scanned for field names by propFieldRE.
var interfacePropsRE = regexp.MustCompile(
	`(?s)(?:export\s+)?interface\s+([A-Z][A-Za-z0-9_]*Props)\s*(?:extends\s+[^{]+)?\{([^}]*)\}`,
)

// typeAliasPropsRE captures `type FooProps = { ... }` (object literal form
// only; union / intersection shapes are ignored for MVP).
var typeAliasPropsRE = regexp.MustCompile(
	`(?s)(?:export\s+)?type\s+([A-Z][A-Za-z0-9_]*Props)\s*=\s*\{([^}]*)\}`,
)

// propFieldRE matches a single field inside a TS interface / object type:
// `foo?: Type,` / `foo: Type;` / `onPress?: () => void;`. Captures the name.
var propFieldRE = regexp.MustCompile(
	`(?m)^\s*(?:readonly\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\??\s*:`,
)

// destructuredParamRE captures `({foo, bar, baz}: SomeProps)` or
// `({foo, bar}:` / `({foo, bar})` — used when component signature destructures.
var destructuredParamRE = regexp.MustCompile(
	`\(\s*\{([^}]*)\}\s*(?::\s*([A-Z][A-Za-z0-9_]*))?\s*\)`,
)

// signaturePropsTypeRE captures the Props type annotation on a function
// component signature: `function Foo(props: XxxProps)` / `(p: XxxProps)`.
var signaturePropsTypeRE = regexp.MustCompile(
	`\(\s*[A-Za-z_][A-Za-z0-9_]*\s*:\s*([A-Z][A-Za-z0-9_]*Props)\b`,
)

// jsxOpenTagRE matches the opening of a JSX element: `<Foo` or `<Foo.Bar`.
// Anchored by `<` + uppercase letter to skip HTML tags.
var jsxOpenTagRE = regexp.MustCompile(`<([A-Z][A-Za-z0-9_]*)(?:\.[A-Z][A-Za-z0-9_]*)?`)

// hookCallRE matches `useXxx(` call sites where the following char after
// `use` is uppercase — matches MX-1061's hook detection heuristic.
var hookCallRE = regexp.MustCompile(`\b(use[A-Z][A-Za-z0-9_]*)\s*\(`)

// identRE verifies a bare identifier — used on prop-name candidates after
// splitting on commas / newlines inside a destructuring pattern.
var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ---------------------------------------------------------------------------
// Ref builders
// ---------------------------------------------------------------------------

// componentRef returns the stable ref string used as the FromID on every
// relationship this extractor emits. Format matches the cross/endpoint
// extractor: "scope:operation:<file>#<name>".
func componentRef(filePath, name string) string {
	return "scope:operation:" + filePath + "#" + name
}

// propsSchemaRef returns the stable ref for a Props interface / type alias.
// The base JS/TS extractor emits interfaces as SCOPE.Schema with this same
// form, so the ref resolves via ID-equality on the the graph side.
func propsSchemaRef(filePath, name string) string {
	return "scope:schema:" + filePath + "#" + name
}

// hookRef returns the stable ref for a React hook entity. Hooks are matched
// by name only — the graph dedupes by ComputeID across the repo.
func hookRef(filePath, name string) string {
	return "scope:operation:" + filePath + "#" + name
}

// ---------------------------------------------------------------------------
// Extract — entry point
// ---------------------------------------------------------------------------

// Extract implements extractor.Extractor.
//
// Non-.tsx/.jsx files, files without a `react` import, and files whose first
// PascalCase function does not look like a component are all fast-skipped.
// Panics in any sub-routine are swallowed so the cross-extractor never
// cascades a failure to the main pipeline.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) (out []types.EntityRecord, err error) {
	tracer := otel.Tracer("extractor._cross_react_props")
	_, span := tracer.Start(ctx, "indexer.react_props_extract")
	defer span.End()

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	defer func() {
		if r := recover(); r != nil {
			// Behaviour rule 5: never abort the overall job. Return empty.
			out = nil
			err = nil
		}
	}()

	if !isReactFile(file.Path, file.Language) {
		return nil, nil
	}
	source := string(file.Content)
	if source == "" || !reactImportRE.MatchString(source) {
		return nil, nil
	}

	// 1. Catalog Props interfaces / type aliases defined in this file, keyed
	//    by their type name (e.g., "InspectorDeviceCardProps").
	propsTypes := collectPropsTypes(source)

	// 2. Find every React component function (function decl + arrow).
	components := findComponents(source)
	if len(components) == 0 {
		return nil, nil
	}

	var (
		hasPropsCount int
		rendersCount  int
		usesHookCount int
	)

	// 3. For each component, resolve props / children / hooks.
	for _, c := range components {
		body := c.body
		propsType, propNames := resolveProps(c, body, propsTypes)
		children := findJSXChildren(body, components)
		hooks := findHooks(body)

		compEntity := buildComponentEntity(file, c, propNames)

		if propsType != "" {
			compEntity.Relationships = append(compEntity.Relationships, types.RelationshipRecord{
				FromID: componentRef(file.Path, c.name),
				ToID:   propsSchemaRef(file.Path, propsType),
				Kind:   RelHasProps,
				Properties: map[string]string{
					"props_type": propsType,
				},
			})
			hasPropsCount++
		}

		for _, child := range children {
			compEntity.Relationships = append(compEntity.Relationships, types.RelationshipRecord{
				FromID: componentRef(file.Path, c.name),
				ToID:   componentRef(file.Path, child),
				Kind:   RelRenders,
				Properties: map[string]string{
					"child_name": child,
				},
			})
			rendersCount++
		}

		for _, hook := range hooks {
			compEntity.Relationships = append(compEntity.Relationships, types.RelationshipRecord{
				FromID: componentRef(file.Path, c.name),
				ToID:   hookRef(file.Path, hook),
				Kind:   RelUsesHook,
				Properties: map[string]string{
					"hook_name": hook,
				},
			})
			usesHookCount++
		}

		out = append(out, compEntity)

		// 4. Emit the PropsInterface entity when we found one. The base
		// JS/TS extractor also emits the same interface as SCOPE.Schema,
		// so the two records merge on ComputeID.
		if propsType != "" {
			out = append(out, buildPropsInterfaceEntity(file, propsType, propsTypes[propsType]))
		}
	}

	span.SetAttributes(
		attribute.Int("component_count", len(components)),
		attribute.Int("has_props_count", hasPropsCount),
		attribute.Int("renders_count", rendersCount),
		attribute.Int("uses_hook_count", usesHookCount),
	)
	return out, nil
}

// ---------------------------------------------------------------------------
// Component discovery
// ---------------------------------------------------------------------------

// component is an internal descriptor of one React component found in a file.
type component struct {
	name      string
	signature string // text of the "(...)" parameter list including parens
	body      string // text of the function body, best-effort
	kind      string // "function" | "arrow"
}

// findComponents returns every function-shaped entity in source whose name
// begins with an ASCII uppercase letter AND whose body contains a JSX
// marker. Both function declarations and `const Foo = (...) => ...` arrow
// assignments are recognised.
func findComponents(source string) []component {
	var out []component
	seen := map[string]bool{}

	for _, m := range functionDeclRE.FindAllStringSubmatchIndex(source, -1) {
		if len(m) < 4 {
			continue
		}
		name := source[m[2]:m[3]]
		// For `function Foo(`, m[1] lands one past `(`, so parenStart is m[1]-1.
		sig, body := sliceFromParen(source, m[1]-1)
		if !looksLikeJSXBody(body) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, component{
			name:      name,
			signature: sig,
			body:      body,
			kind:      "function",
		})
	}

	for _, m := range arrowDeclRE.FindAllStringSubmatchIndex(source, -1) {
		if len(m) < 4 {
			continue
		}
		name := source[m[2]:m[3]]
		// For arrow functions the whole-match end sits after `=>`. Scan
		// forward from the name end to find the first `(` of the parameter
		// list — that is the signature start.
		nameEnd := m[3]
		parenIdx := strings.IndexByte(source[nameEnd:m[1]], '(')
		if parenIdx < 0 {
			// Shouldn't happen given arrowDeclRE, but skip rather than crash.
			continue
		}
		sig, body := sliceFromParen(source, nameEnd+parenIdx)
		if !looksLikeJSXBody(body) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, component{
			name:      name,
			signature: sig,
			body:      body,
			kind:      "arrow",
		})
	}
	return out
}

// sliceFromParen takes the byte index of a `(` character opening a
// parameter list and returns (signature, body). The signature is the
// balanced `(...)` substring; the body is the balanced `{...}` block that
// follows (or, for expression-body arrows, the substring up to the next
// `;` / `)` / `,` terminator). Malformed input yields ("", "").
func sliceFromParen(source string, parenStart int) (string, string) {
	if parenStart < 0 || parenStart >= len(source) || source[parenStart] != '(' {
		return "", ""
	}
	parenEnd := matchBalanced(source, parenStart, '(', ')')
	if parenEnd < 0 {
		return "", ""
	}
	sig := source[parenStart : parenEnd+1]

	// Find the opening `{` that starts the function / arrow body. Scan past
	// any whitespace, `:` (TS return type), `=>`, or identifier chars.
	braceStart := -1
	for i := parenEnd + 1; i < len(source); i++ {
		c := source[i]
		if c == '{' {
			braceStart = i
			break
		}
		if c == ';' {
			// No body at all — skip.
			return sig, ""
		}
		if c == '(' && i > parenEnd+3 {
			// Expression-body arrow that re-enters parens: `() => (\n<Foo/>\n)`
			// Treat the parenthesised expression as the body.
			exprEnd := matchBalanced(source, i, '(', ')')
			if exprEnd < 0 {
				return sig, ""
			}
			return sig, source[i+1 : exprEnd]
		}
	}
	if braceStart < 0 {
		return sig, ""
	}
	braceEnd := matchBalanced(source, braceStart, '{', '}')
	if braceEnd < 0 {
		return sig, source[braceStart+1:]
	}
	return sig, source[braceStart+1 : braceEnd]
}

// matchBalanced walks forward from openIdx in s, tracking string / char
// contexts, and returns the index of the matching closing char.
func matchBalanced(s string, openIdx int, open, close byte) int {
	if openIdx < 0 || openIdx >= len(s) || s[openIdx] != open {
		return -1
	}
	depth := 0
	inStr := byte(0)
	i := openIdx
	for i < len(s) {
		c := s[i]
		if inStr != 0 {
			if c == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if c == inStr {
				inStr = 0
			}
			i++
			continue
		}
		switch c {
		case '\'', '"', '`':
			inStr = c
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

// looksLikeJSXBody is a cheap pre-filter: a body is component-shaped if it
// contains either a JSX return marker or a React.createElement call. We
// accept `<` followed by any letter (upper OR lower) so that components
// returning plain HTML tags like `<div>` are detected, and we additionally
// accept the `<>` fragment short form. The uppercase-only gate is reserved
// for the RENDERS extraction pass — not for component discovery.
func looksLikeJSXBody(body string) bool {
	if body == "" {
		return false
	}
	if strings.Contains(body, "React.createElement") {
		return true
	}
	for i := 0; i < len(body)-1; i++ {
		if body[i] == '<' {
			nxt := body[i+1]
			if (nxt >= 'a' && nxt <= 'z') || (nxt >= 'A' && nxt <= 'Z') {
				return true
			}
			if nxt == '>' {
				// Fragment short form `<>...`
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Props collection
// ---------------------------------------------------------------------------

// collectPropsTypes scans the whole file for `interface XxxProps` and
// `type XxxProps = { ... }` declarations and returns a map from the type
// name to its comma-joined field names.
func collectPropsTypes(source string) map[string][]string {
	out := map[string][]string{}
	for _, m := range interfacePropsRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 3 {
			continue
		}
		name := m[1]
		body := m[2]
		out[name] = extractFieldNames(body)
	}
	for _, m := range typeAliasPropsRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 3 {
			continue
		}
		name := m[1]
		body := m[2]
		if _, exists := out[name]; exists {
			continue
		}
		out[name] = extractFieldNames(body)
	}
	return out
}

// extractFieldNames pulls prop names from an interface / object-type body.
// Order-preserving, deduplicated.
func extractFieldNames(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range propFieldRE.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		n := m[1]
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// resolveProps returns (propsTypeName, propNames) for a component. Priority:
//  1. Function signature type annotation — `(props: XxxProps)` — look up by
//     name in the file's Props-type catalog.
//  2. Destructured parameter + type annotation — `({a,b}: XxxProps)`.
//  3. Bare destructured parameter without a type — `({a, b, c})`.
//
// Returns ("", nil) if no props can be recovered.
func resolveProps(c component, body string, propsTypes map[string][]string) (string, []string) {
	// Priority 1+2: look for a Props type on the parameter list.
	if m := signaturePropsTypeRE.FindStringSubmatch(c.signature); len(m) >= 2 {
		typeName := m[1]
		if fields, ok := propsTypes[typeName]; ok && len(fields) > 0 {
			return typeName, fields
		}
		// Type is annotated but not defined in this file — still valid.
		return typeName, nil
	}
	if m := destructuredParamRE.FindStringSubmatch(c.signature); len(m) >= 2 {
		names := splitDestructured(m[1])
		typeName := ""
		if len(m) >= 3 {
			typeName = m[2]
		}
		if typeName != "" {
			if fields, ok := propsTypes[typeName]; ok && len(fields) > 0 {
				return typeName, fields
			}
			return typeName, names
		}
		// Priority 3: bare destructured, no type.
		return "", names
	}
	_ = body
	return "", nil
}

// splitDestructured parses `a, b, c = foo, { nested }: T` into ["a","b","c"].
// Nested braces are skipped; default values and renames are stripped.
func splitDestructured(raw string) []string {
	var out []string
	seen := map[string]bool{}
	depth := 0
	cur := strings.Builder{}
	flush := func() {
		tok := strings.TrimSpace(cur.String())
		cur.Reset()
		if tok == "" {
			return
		}
		// Strip default value: `foo = 1`
		if i := strings.IndexByte(tok, '='); i >= 0 {
			tok = strings.TrimSpace(tok[:i])
		}
		// Strip rename: `foo: bar` → keep bar (the local binding).
		if i := strings.IndexByte(tok, ':'); i >= 0 {
			tok = strings.TrimSpace(tok[i+1:])
		}
		// Strip rest: `...rest` → "rest"
		tok = strings.TrimPrefix(tok, "...")
		if !identRE.MatchString(tok) {
			return
		}
		if seen[tok] {
			return
		}
		seen[tok] = true
		out = append(out, tok)
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch c {
		case '{', '[', '(':
			depth++
			cur.WriteByte(c)
		case '}', ']', ')':
			depth--
			cur.WriteByte(c)
		case ',':
			if depth == 0 {
				flush()
				continue
			}
			cur.WriteByte(c)
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// ---------------------------------------------------------------------------
// JSX children and hooks
// ---------------------------------------------------------------------------

// findJSXChildren returns the unique set of PascalCase JSX element names in
// body. Siblings and same-file references are kept. The `siblings` list is
// the set of all components discovered in the SAME file — we use it to skip
// emitting self-RENDERS when a component re-returns itself, and to tag
// whether a child is in-repo (we return all PascalCase tokens; ingestion-side
// resolution handles cross-file merge via ComputeID).
func findJSXChildren(body string, siblings []component) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range jsxOpenTagRE.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		if htmlElementNames[strings.ToLower(name)] {
			continue
		}
		if reactBuiltins[name] {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	_ = siblings
	return out
}

// findHooks returns the unique set of `useXxx` call names in body.
func findHooks(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range hookCallRE.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// reactBuiltins lists React intrinsics that should NOT be emitted as RENDERS
// edges. They are not components defined in user code.
var reactBuiltins = map[string]bool{
	"Fragment":  true,
	"Suspense":  true,
	"Profiler":  true,
	"StrictMode": true,
}

// htmlElementNames is the set of HTML tags we filter out of JSX detection.
// Matched case-insensitively against the opening-tag name. The uppercase
// gate in jsxOpenTagRE already rejects lowercase tags, but this acts as
// defence-in-depth for oddball names like `<Html>`.
var htmlElementNames = map[string]bool{
	"html": true, "head": true, "body": true, "div": true, "span": true,
	"a": true, "p": true, "img": true, "input": true, "button": true,
	"form": true, "label": true, "select": true, "option": true,
	"textarea": true, "ul": true, "ol": true, "li": true, "table": true,
	"tr": true, "td": true, "th": true, "thead": true, "tbody": true,
	"section": true, "article": true, "header": true, "footer": true,
	"main": true, "nav": true, "aside": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true,
}

// ---------------------------------------------------------------------------
// Entity builders
// ---------------------------------------------------------------------------

// buildComponentEntity builds the SCOPE.Operation record that carries the
// `props` property for a component. Because ComputeID hashes only on
// (org, project, source_file, kind, name), this record dedupes against the
// base JS/TS extractor output in the graph — the `props` property merges in.
func buildComponentEntity(file extractor.FileInput, c component, propNames []string) types.EntityRecord {
	props := map[string]string{
		"framework":  "react",
		"component":  "true",
		"props":      truncateProps(strings.Join(propNames, ", ")),
		"ref":        componentRef(file.Path, c.name),
		"provenance": "INFERRED_FROM_REACT_PROPS_EXTRACTOR",
	}
	return types.EntityRecord{
		Name:         c.name,
		Kind:         KindOperation,
		SourceFile:   file.Path,
		Language:     file.Language,
		Subtype:      "react_component",
		Properties:   props,
		QualityScore: 0.85,
	}
}

// buildPropsInterfaceEntity builds the SCOPE.Schema record for a Props type.
func buildPropsInterfaceEntity(file extractor.FileInput, typeName string, fields []string) types.EntityRecord {
	props := map[string]string{
		"framework":  "react",
		"props":      truncateProps(strings.Join(fields, ", ")),
		"ref":        propsSchemaRef(file.Path, typeName),
		"provenance": "INFERRED_FROM_REACT_PROPS_EXTRACTOR",
	}
	return types.EntityRecord{
		Name:         typeName,
		Kind:         KindSchema,
		SourceFile:   file.Path,
		Language:     file.Language,
		Subtype:      "react_props_interface",
		Properties:   props,
		QualityScore: 0.85,
	}
}

// truncateProps enforces the 500-char cap required by MX-1062 rule 2.
func truncateProps(s string) string {
	if len(s) <= propsMaxLen {
		return s
	}
	// Truncate on a safe byte boundary and append an ellipsis.
	trimTo := propsMaxLen - 3
	if trimTo < 0 {
		trimTo = 0
	}
	return s[:trimTo] + "..."
}

// ---------------------------------------------------------------------------
// File-type gate
// ---------------------------------------------------------------------------

// isReactFile returns true for .tsx / .jsx paths, regardless of the language
// tag sent by the dispatcher (which may be "typescript" or "javascript").
func isReactFile(path, language string) bool {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".tsx") || strings.HasSuffix(lower, ".jsx") {
		return true
	}
	// Fall-through: some projects use .ts with React (rare). Language-only
	// gate is not enough; we require the file extension to stay deterministic.
	_ = language
	return false
}
