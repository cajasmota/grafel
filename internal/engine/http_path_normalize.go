// http_path_normalize.go — pre-canonicalization HTTP path normalization for
// http_endpoint entity ID computation (issue #807).
//
// normalizePath is the canonical normalizer for ALL synthesizers that compute
// http_endpoint entity IDs. It runs BEFORE httproutes.Canonicalize and handles
// cases that are framework-agnostic:
//
//   - Rule 1: Strip env-var prefixes (JS process.env/import.meta.env/
//     template-literal ${VITE_X}, Python os.environ/getenv, Java System.getenv,
//     Go os.Getenv). Sets base_url_var property.
//   - Rule 2: Strip query strings from the path. Sets query_template property.
//   - Rule 3: Collapse duplicate / sequential slashes (e.g. /api//foo → /api/foo).
//   - Rule 4: Convert template-literal ${name} path-parameter substitutions
//     to {name} OpenAPI form (e.g. `/users/${id}` → `/users/{id}`).
//
// Note on trailing slash: the existing httproutes.Canonicalize already strips
// trailing slashes uniformly across all frameworks. This file preserves that
// convention so downstream ID computation and test fixtures are not disturbed.
//
// Refs #807.
package engine

import (
	"regexp"
	"strings"
)

// NormalizedPath holds the result of normalizePath.
type NormalizedPath struct {
	// Path is the normalized path string, ready to be passed to
	// httproutes.Canonicalize followed by httproutes.SyntheticID.
	Path string
	// Props contains additional properties extracted during normalization
	// (e.g. base_url_var, query_template). Callers should merge these into
	// the entity's Properties map via mergeNormalizeProps.
	Props map[string]string
}

// normalizePath applies framework-agnostic normalization rules to rawPath and
// returns the cleaned path plus any extracted properties.
//
// Rules applied in order:
//  1. Strip env-var prefixes (JS, Python, Java, Go) — sets base_url_var
//  4. Convert ${name} template-literal params to {name} OpenAPI form
//     (runs before query-string stripping to avoid misidentifying `?.` as `?`)
//  2. Strip query string — sets query_template
//  3. Collapse duplicate sequential slashes
//
// The returned NormalizedPath.Path is suitable for passing to
// httproutes.Canonicalize. If rawPath is empty, "/" is returned.
func normalizePath(rawPath string) NormalizedPath {
	if rawPath == "" {
		return NormalizedPath{Path: "/"}
	}

	props := map[string]string{}

	// --- Rule 1: Strip env-var prefixes ---
	path, varName := extractEnvVarPrefix(rawPath)
	if varName != "" {
		props["base_url_var"] = varName
	}

	// --- Rule 4: Convert ${name} template params to {name} ---
	// NOTE: Rule 4 runs BEFORE Rule 2 (query string) so that `?` inside
	// template expressions like `${user?.id}` is not misidentified as the
	// start of a query string.
	path = expandTemplateLiteralParams(path)

	// --- Rule 2: Strip query string ---
	// After template params are expanded, any remaining `?` is a genuine
	// query string separator.
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		qs := path[idx+1:]
		path = path[:idx]
		if qs != "" {
			props["query_template"] = qs
		}
	}

	// --- Rule 3: Collapse duplicate slashes ---
	// Only collapse duplicate slashes in the path component. Absolute URLs
	// like `https://host/path` have `//` in the scheme section which must
	// NOT be collapsed. We apply the rule only if the path doesn't start
	// with `http://` or `https://`; callers apply stripURLHost to absolute
	// URLs first, so this guard is just an extra safety net.
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		for strings.Contains(path, "//") {
			path = strings.ReplaceAll(path, "//", "/")
		}
	}

	if path == "" {
		path = "/"
	}

	result := NormalizedPath{Path: path}
	if len(props) > 0 {
		result.Props = props
	}
	return result
}

// ---------------------------------------------------------------------------
// Rule 1: env-var prefix extraction
// ---------------------------------------------------------------------------

// extractEnvVarPrefix removes a recognized env-var expression from the start
// of rawPath. Returns (cleanedPath, varName). If no prefix is found, varName
// is "" and cleanedPath == rawPath.
//
// Recognized prefix forms:
//
//	JS/TS template literal:
//	  ${VITE_X}/path                → varName = "VITE_X"
//	  ${process.env.Y}/path         → varName = "process.env.Y"
//	  ${import.meta.env.Z}/path     → varName = "import.meta.env.Z"
//
//	Python concatenation (pre-tokenized suffix passed to normalizePath):
//	  os.environ['KEY'] + '/path'   → varName = "KEY"
//	  os.environ["KEY"] + '/path'   → varName = "KEY"
//	  os.getenv('KEY') + '/path'    → varName = "KEY"
//	  getenv('KEY') + '/path'       → varName = "KEY"
//
//	Java:
//	  System.getenv("KEY") + "/path" → varName = "KEY"
//
//	Go:
//	  os.Getenv("KEY") + "/path"     → varName = "KEY"
func extractEnvVarPrefix(raw string) (cleanedPath, varName string) {
	raw = strings.TrimSpace(raw)

	// JS/TS template literal: ${...}/rest
	if m := npJSTemplatePrefixRe.FindStringSubmatchIndex(raw); len(m) >= 4 && m[2] >= 0 {
		varName = raw[m[2]:m[3]]
		rest := strings.TrimSpace(raw[m[1]:])
		return rest, varName
	}

	// Python os.environ / os.getenv / getenv  →  + '/path'
	if m := npPyEnvRe.FindStringSubmatchIndex(raw); len(m) >= 8 {
		// Groups 2,4,6 for three alternations (indices: [2][3], [4][5], [6][7])
		varName = firstNonEmptyInRange(raw, m[2:])
		if varName != "" {
			rest := extractConcatSuffix(raw[m[1]:])
			return rest, varName
		}
	}

	// Java System.getenv("KEY")
	if m := npJavaEnvRe.FindStringSubmatchIndex(raw); len(m) >= 4 && m[2] >= 0 {
		varName = raw[m[2]:m[3]]
		rest := extractConcatSuffix(raw[m[1]:])
		return rest, varName
	}

	// Go os.Getenv("KEY")
	if m := npGoEnvRe.FindStringSubmatchIndex(raw); len(m) >= 4 && m[2] >= 0 {
		varName = raw[m[2]:m[3]]
		rest := extractConcatSuffix(raw[m[1]:])
		return rest, varName
	}

	return raw, ""
}

// npJSTemplatePrefixRe matches the JS/TS template-literal env-var prefix at the
// start of a path string. Only EXPLICIT env-var accessor forms are recognized:
//
//	${process.env.Y}/foo   → varName = "process.env.Y"
//	${import.meta.env.Z}/foo → varName = "import.meta.env.Z"
//
// NOTE: Plain ALL_CAPS identifiers like ${VITE_CORE_API} are NOT matched here
// because they may also be local constants (e.g. ${UNKNOWN_BASE} is a valid
// path-parameter placeholder per #706). Callers that have const-folding context
// (canonicalizeTemplateLiteral) handle those cases via isEnvVarStyleExpr.
//
// Capture group 1: the full expression inside ${...}.
var npJSTemplatePrefixRe = regexp.MustCompile(
	`^\$\{((?:process\.env\.|import\.meta\.env\.)[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*)\}`,
)

// npPyEnvRe matches Python env-var prefix patterns at the start of the string.
// Three alternation groups capture the var name:
//   - Group 1 (index 2-3): os.environ['KEY'] or os.environ["KEY"]
//   - Group 2 (index 4-5): os.getenv('KEY') or os.getenv("KEY")
//   - Group 3 (index 6-7): getenv('KEY') or getenv("KEY")
var npPyEnvRe = regexp.MustCompile(
	`^(?:` +
		`os\.environ\[["']([A-Za-z_][\w]*)["']\]` +
		`|os\.getenv\(["']([A-Za-z_][\w]*)["']\)` +
		`|getenv\(["']([A-Za-z_][\w]*)["']\)` +
		`)`,
)

// npJavaEnvRe matches Java System.getenv("KEY") at the start of the string.
// Capture group 1: the key name.
var npJavaEnvRe = regexp.MustCompile(
	`^System\.getenv\("([A-Za-z_][\w]*)"\)`,
)

// npGoEnvRe matches Go os.Getenv("KEY") at the start of the string.
// Capture group 1: the key name.
var npGoEnvRe = regexp.MustCompile(
	`^os\.Getenv\("([A-Za-z_][\w]*)"\)`,
)

// firstNonEmptyInRange returns the first non-empty substring from consecutive
// [start,end) index pairs in the subject string s, provided by match pairs m.
// m should be a slice of (start, end) pairs (i.e. the result of
// FindStringSubmatchIndex with the leading [0][1] and [2][3] slices stripped).
func firstNonEmptyInRange(s string, m []int) string {
	for i := 0; i+1 < len(m); i += 2 {
		start, end := m[i], m[i+1]
		if start >= 0 && end > start {
			return s[start:end]
		}
	}
	return ""
}

// extractConcatSuffix strips an optional leading `+` and surrounding quotes
// from a string like `+ "/path"` or `"/path"` or `/path`, returning the
// path portion. Used after an env-var prefix has been consumed.
func extractConcatSuffix(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "+") {
		s = strings.TrimSpace(s[1:])
	}
	return stripOuterQuotes(s)
}

// ---------------------------------------------------------------------------
// Rule 4: template-literal ${name} → {name}
// ---------------------------------------------------------------------------

// npParamSubstRe matches `${expr}` inside a path string. After Rule 1 has
// stripped any leading env-var prefix, remaining ${...} occurrences are
// path-parameter substitutions, not env-var references.
var npParamSubstRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// expandTemplateLiteralParams converts `${expr}` → `{name}` in a path string
// that has already had its env-var prefix stripped. Complex expressions are
// simplified to their last identifier segment or fall back to `{param}`.
func expandTemplateLiteralParams(path string) string {
	if !strings.Contains(path, "${") {
		return path
	}
	return npParamSubstRe.ReplaceAllStringFunc(path, func(match string) string {
		inner := npParamSubstRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return "{param}"
		}
		expr := strings.TrimSpace(inner[1])

		// Strip TypeScript type assertions: `id as string`, `id as number`
		if idx := strings.Index(expr, " as "); idx >= 0 {
			expr = strings.TrimSpace(expr[:idx])
		}

		// Normalize optional-chain `?.` to `.` so dotted-access handling works.
		expr = strings.ReplaceAll(expr, "?.", ".")
		// Strip a standalone trailing `?` (rare but guard against it).
		expr = strings.TrimSuffix(expr, "?")

		// Plain identifier: use as-is.
		if isPlainIdentifier(expr) {
			return "{" + expr + "}"
		}

		// Dotted / bracket access: `user.id`, `user.id`, `row["id"]` → last segment.
		if dot := strings.LastIndexAny(expr, ".["); dot >= 0 {
			last := expr[dot+1:]
			last = strings.Trim(last, `"']`)
			if isPlainIdentifier(last) {
				return "{" + last + "}"
			}
		}

		// Fallback.
		return "{param}"
	})
}

// isPlainIdentifier returns true if s is a non-empty valid JS/Go identifier.
func isPlainIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '$'
		isDigit := c >= '0' && c <= '9'
		if i == 0 {
			if !isLetter {
				return false
			}
		} else {
			if !isLetter && !isDigit {
				return false
			}
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Helpers shared across synthesizers
// ---------------------------------------------------------------------------

// stripOuterQuotes removes a single layer of surrounding `"..."` or `'...'`
// from s. Returns s unchanged if it is not quoted.
func stripOuterQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// mergeNormalizeProps merges props returned by normalizePath into an existing
// properties map dst. Existing keys in dst are not overwritten so that
// synthesizer-set values take precedence.
func mergeNormalizeProps(dst map[string]string, src map[string]string) {
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}
