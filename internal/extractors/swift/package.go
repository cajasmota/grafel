// package.go — regex-based extractor for SwiftPM Package.swift manifests.
//
// Issue #497: Package.swift is a Swift DSL file that declares targets and
// their dependencies. The tree-sitter Swift grammar correctly parses it as
// Swift code, but the existing swift extractor only emits function/class/
// import entities — it does not model the SwiftPM target graph.
//
// This extractor is registered under the language key "swift_package" and
// is dispatched when the classifier assigns that key to a file named
// exactly "Package.swift" (see internal/classifier/classifier.go).
//
// Emitted entities and relationships:
//
//	.target(name: "X", ...)          → SCOPE.Component (subtype="swiftpm_target")
//	.executableTarget(name: "X", ...) → SCOPE.Component (subtype="swiftpm_target",
//	                                     properties["kind"]="executable")
//	.testTarget(name: "X", ...)       → SCOPE.Component (subtype="swiftpm_target",
//	                                     properties["kind"]="test")
//	.product(name: "X", package: "Y") → SCOPE.External  (subtype="swiftpm_product")
//	dependencies: ["Y"]               → DEPENDS_ON edges between target entities
//
// Regex-based parsing is intentional for v1: Package.swift DSL shapes are
// stable and well-defined enough for simple pattern matching, and avoids a
// dependency on a tree-sitter Swift parse of the manifest (which would still
// require AST-level semantic analysis to resolve the call graph anyway).
package swift

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("swift_package", &PackageExtractor{})
}

// PackageExtractor implements extractor.Extractor for Package.swift manifests.
type PackageExtractor struct{}

// Language returns the canonical language key for this extractor.
func (p *PackageExtractor) Language() string { return "swift_package" }

// Extract parses a Package.swift file and emits:
//   - One SCOPE.Component per .target / .executableTarget / .testTarget call
//   - One SCOPE.External per .product(name:package:) dependency reference
//   - DEPENDS_ON edges from each target to its named dependencies
//
// On nil content the extractor returns an empty slice with no error.
func (p *PackageExtractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	targets := extractTargets(src, file.Path)
	extractor.TagRelationshipsLanguage(targets, "swift_package")
	return targets, nil
}

// ---------------------------------------------------------------------------
// Regex patterns
// ---------------------------------------------------------------------------

// targetCallRe matches the opening of a target call:
//
//	.target(name: "X"
//	.executableTarget(name: "X"
//	.testTarget(name: "X"
//
// Capture groups:
//
//	1 — target kind ("target", "executableTarget", "testTarget")
//	2 — target name
var targetCallRe = regexp.MustCompile(`\.(target|executableTarget|testTarget)\s*\(\s*name\s*:\s*"([^"]+)"`)

// targetStringDepRe matches a bare string dependency inside a dependencies array:
//
//	"TargetName"
//
// Capture groups:
//
//	1 — dependency name
var targetStringDepRe = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_.-]*)"`)

// productDepRe matches a .product(name: "X", package: "Y") dependency:
//
//	.product(name: "X", package: "Y")
//
// Capture groups:
//
//	1 — product name
//	2 — package name
var productDepRe = regexp.MustCompile(`\.product\s*\(\s*name\s*:\s*"([^"]+)"\s*,\s*package\s*:\s*"([^"]+)"`)

// ---------------------------------------------------------------------------
// Extraction helpers
// ---------------------------------------------------------------------------

// extractTargets parses the full source text and returns all entity records.
func extractTargets(src, filePath string) []types.EntityRecord {
	lines := strings.Split(src, "\n")

	// Pass 1 — find every target declaration and collect the raw argument
	// block that follows it.
	//
	// We run the regex against the full source (not line-by-line) because
	// `.target(\n    name: "X"` spans two lines and \s* in the pattern
	// matches the newline. We then convert match byte offsets to line numbers.
	type targetDecl struct {
		kind    string // "target" | "executableTarget" | "testTarget"
		name    string
		lineNum int
		body    string // text inside .target( … )
	}

	var decls []targetDecl

	allMatches := targetCallRe.FindAllStringSubmatchIndex(src, -1)
	for _, loc := range allMatches {
		// loc[0]:loc[1] — whole match; loc[2]:loc[3] — kind; loc[4]:loc[5] — name
		if len(loc) < 6 {
			continue
		}
		kind := src[loc[2]:loc[3]]
		name := src[loc[4]:loc[5]]
		// Compute 1-based line number from the byte offset of the match start.
		lineNum := strings.Count(src[:loc[0]], "\n") + 1
		// Identify which line index to start collecting from.
		lineIdx := lineNum - 1
		body := collectArgBlock(lines, lineIdx)

		decls = append(decls, targetDecl{
			kind:    kind,
			name:    name,
			lineNum: lineNum,
			body:    body,
		})
	}

	if len(decls) == 0 {
		return nil
	}

	// Pass 2 — for each target, collect its dependencies and build entities.
	//
	// Index target names so we can distinguish same-package target deps from
	// external product deps at emit time.
	targetNames := make(map[string]bool, len(decls))
	for _, d := range decls {
		targetNames[d.name] = true
	}

	// Track external products already emitted to avoid duplicates.
	emittedProducts := make(map[string]bool)

	var out []types.EntityRecord

	for _, d := range decls {
		// Build the target SCOPE.Component entity.
		rec := types.EntityRecord{
			Name:       d.name,
			Kind:       "SCOPE.Component",
			Subtype:    "swiftpm_target",
			SourceFile: filePath,
			Language:   "swift_package",
			StartLine:  d.lineNum,
			EndLine:    d.lineNum,
			Signature:  "." + d.kind + `(name: "` + d.name + `")`,
			Properties: map[string]string{
				"swiftpm_kind": d.kind,
			},
		}

		// Extract dependencies from the argument body.
		depsSection := extractDepsSection(d.body)

		// Walk product deps first (they have a known package: argument).
		for _, pm := range productDepRe.FindAllStringSubmatch(depsSection, -1) {
			productName := pm[1]
			packageName := pm[2]

			// Emit DEPENDS_ON edge target → product entity.
			rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
				FromID: filePath + "::" + d.name,
				ToID:   productName,
				Kind:   "DEPENDS_ON",
				Properties: map[string]string{
					"dep_kind": "product",
					"package":  packageName,
				},
			})

			// Emit a SCOPE.External entity for the product (once per unique product name).
			key := packageName + "/" + productName
			if !emittedProducts[key] {
				emittedProducts[key] = true
				ext := types.EntityRecord{
					Name:       productName,
					Kind:       "SCOPE.External",
					Subtype:    "swiftpm_product",
					SourceFile: filePath,
					Language:   "swift_package",
					Signature:  `.product(name: "` + productName + `", package: "` + packageName + `")`,
					Properties: map[string]string{
						"package": packageName,
					},
				}
				out = append(out, ext)
			}
		}

		// Walk bare string deps; filter out product name refs already seen
		// and the package own-name refs (e.g. version strings, URLs).
		// A bare "DepName" is a same-package target dependency when the
		// name exists in our target index; otherwise it is emitted as an
		// external name reference.
		//
		// Strip product blocks to avoid re-matching product names as bare deps.
		depsStripped := productDepRe.ReplaceAllString(depsSection, "")
		for _, sm := range targetStringDepRe.FindAllStringSubmatch(depsStripped, -1) {
			depName := sm[1]

			if depName == d.name {
				continue // self-reference
			}
			// Skip URL-like / semver strings.
			if looksLikeURL(depName) || looksLikeSemver(depName) {
				continue
			}

			depKind := "target"
			if !targetNames[depName] {
				depKind = "external"
			}

			rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
				FromID: filePath + "::" + d.name,
				ToID:   depName,
				Kind:   "DEPENDS_ON",
				Properties: map[string]string{
					"dep_kind": depKind,
				},
			})
		}

		out = append(out, rec)
	}

	return out
}

// collectArgBlock scans forward from startLine and returns the concatenated
// text of lines inside the argument parentheses opened on startLine.
func collectArgBlock(lines []string, startLine int) string {
	// Count open parens from the call site line.
	depth := 0
	var buf strings.Builder

	for i := startLine; i < len(lines); i++ {
		line := lines[i]
		for _, ch := range line {
			switch ch {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					buf.WriteString(line)
					return buf.String()
				}
			}
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return buf.String()
}

// extractDepsSection returns the substring of body that falls between the
// "dependencies:" label and the next closing bracket (']') at depth 0, or
// the empty string if no dependencies label is found.
func extractDepsSection(body string) string {
	idx := strings.Index(body, "dependencies:")
	if idx < 0 {
		return ""
	}
	after := body[idx+len("dependencies:"):]
	start := strings.IndexByte(after, '[')
	if start < 0 {
		return ""
	}

	depth := 0
	for i, ch := range after[start:] {
		switch ch {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return after[start : start+i+1]
			}
		}
	}
	return after[start:]
}

// looksLikeURL returns true when name contains a URL-ish scheme or host
// separator, indicating it is a package URL rather than a target name.
func looksLikeURL(name string) bool {
	return strings.Contains(name, "://") ||
		strings.Contains(name, ".git") ||
		strings.Contains(name, ".com") ||
		strings.Contains(name, ".org") ||
		strings.Contains(name, ".io")
}

// looksLikeSemver returns true when name matches a rough semver / version-tag
// pattern (e.g. "1.2.3", "v1.0.0") so we can skip version strings that
// appear as bare strings adjacent to dependency declarations.
var semverRe = regexp.MustCompile(`^v?\d+\.\d+`)

func looksLikeSemver(name string) bool {
	return semverRe.MatchString(name)
}
