// Package razor implements a regex-based extractor for Blazor .razor files.
//
// Razor files mix HTML markup with C# code. This extractor does NOT rely on
// tree-sitter (no tree-sitter-razor grammar is available in go-tree-sitter).
// Instead it uses a layered approach:
//
//  1. Component name derived from the filename (PascalCase convention).
//  2. @inject directives scanned via regex → SCOPE.UIComponent entity.
//  3. @code { } block boundary located via brace-counting scanner.
//  4. Inside the @code block:
//     - [Parameter] / [CascadingParameter] annotated properties → SCOPE.Component
//     - void / async event-handler methods → SCOPE.Operation
//
// Entity kind mapping (allowlist-compliant):
//
//	Component name            → SCOPE.UIComponent
//	[Parameter] property      → SCOPE.Component  (subtype="parameter")
//	@inject directive         → SCOPE.UIComponent (subtype="inject")
//	Event-handler method      → SCOPE.Operation  (subtype="event_handler")
//
// OTel span: "indexer.extract.razor"
//
// Error handling: on any parse failure the extractor returns the component-name
// entity with quality_score=0.3 and enrichment_status="degraded" — never panics.
package razor

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("razor", &Extractor{})
}

// Extractor implements extractor.Extractor for Blazor .razor files.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "razor" }

// --- compiled regexps --------------------------------------------------------

var (
	// @inject ServiceType Name
	reInject = regexp.MustCompile(`(?m)^@inject\s+(\S+)\s+(\S+)`)

	// @using Foo.Bar(.Baz)*
	// Captures: dotted module path (the imported namespace).
	reUsing = regexp.MustCompile(`(?m)^@using\s+([\w\.]+)`)

	// @code followed by optional whitespace then {
	reCodeBlock = regexp.MustCompile(`@code\s*\{`)

	// [Parameter] or [CascadingParameter] attribute on its own line (inside @code block)
	reParamAttr = regexp.MustCompile(`\[(?:Cascading)?Parameter\]`)

	// property declaration after [Parameter]: visibility type Name { get; set; }
	// Captures: type, name
	rePropDecl = regexp.MustCompile(`(?:public|protected|private|internal)?\s*(\S+)\s+(\w+)\s*\{[^}]*\}`)

	// event handler: void or async / Task returning method that looks like an event handler
	// Captures: return_type, method_name
	reEventHandler = regexp.MustCompile(`(?:private|public|protected|internal)?\s*(?:async\s+)?(?:void|Task)\s+(\w+)\s*\(`)

	// Method/function call head: identifier followed by `(`. Captures the
	// identifier. Used to harvest CALLS edges from method bodies.
	reCallHead = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
)

// csKeywords are C# / Razor reserved words that appear before "(" but are NOT
// method calls (control flow, declarations, etc.). Filtered out of CALLS.
var csKeywords = map[string]bool{
	"if": true, "else": true, "while": true, "for": true, "foreach": true,
	"switch": true, "case": true, "return": true, "throw": true, "try": true,
	"catch": true, "finally": true, "using": true, "lock": true, "do": true,
	"new": true, "typeof": true, "sizeof": true, "nameof": true, "default": true,
	"checked": true, "unchecked": true, "in": true, "out": true, "ref": true,
	"is": true, "as": true, "void": true, "Task": true, "var": true,
	"true": true, "false": true, "null": true, "this": true, "base": true,
	"await": true, "async": true, "yield": true, "break": true, "continue": true,
	"goto": true, "fixed": true, "stackalloc": true, "delegate": true,
	"public": true, "private": true, "protected": true, "internal": true,
	"static": true, "readonly": true, "const": true, "virtual": true,
	"override": true, "abstract": true, "sealed": true, "partial": true,
}

// Extract processes a .razor file and returns entity records.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) (entities []types.EntityRecord, retErr error) {
	tracer := otel.Tracer("extractor.razor")
	ctx, span := tracer.Start(ctx, "indexer.extract.razor",
		trace.WithAttributes(
			attribute.String("file", file.Path),
		),
	)
	defer func() {
		span.SetAttributes(
			attribute.Int("entity_count", len(entities)),
		)
		span.End()
	}()
	_ = ctx // ctx used by OTel span above

	componentName := componentNameFromPath(file.Path)
	src := string(file.Content)

	// Degraded fallback — invoked on any unrecoverable parse error.
	degraded := func(reason string) []types.EntityRecord {
		return []types.EntityRecord{
			{
				Name:             componentName,
				Kind:             "SCOPE.UIComponent",
				Subtype:          "component",
				SourceFile:       file.Path,
				Language:         "razor",
				QualityScore:     0.3,
				EnrichmentStatus: types.StatusDegraded,
				Metadata: map[string]interface{}{
					"extraction_status": "degraded",
					"degraded_reason":   reason,
				},
			},
		}
	}

	if len(file.Content) == 0 {
		return degraded("empty file"), nil
	}

	// --- 1. Component entity -------------------------------------------------
	componentEntity := types.EntityRecord{
		Name:               componentName,
		QualifiedName:      componentName,
		Kind:               "SCOPE.UIComponent",
		Subtype:            "component",
		SourceFile:         file.Path,
		Language:           "razor",
		QualityScore:       0.9,
		EnrichmentStatus:   types.StatusPending,
		EnrichmentRequired: false,
	}
	// Component entity is the first record; we mutate index 0 below to attach
	// CONTAINS edges to event-handler operations (Issue #378).
	entities = append(entities, componentEntity)

	// --- 2. @inject directives -----------------------------------------------
	for _, m := range reInject.FindAllStringSubmatch(src, -1) {
		svcType := m[1]
		svcName := m[2]
		lineNum := lineOf(src, reInject.FindStringIndex(src)[0])
		entities = append(entities, types.EntityRecord{
			Name:             svcName,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, svcName),
			Kind:             "SCOPE.UIComponent",
			Subtype:          "inject",
			SourceFile:       file.Path,
			Language:         "razor",
			StartLine:        lineNum,
			EndLine:          lineNum,
			QualityScore:     0.85,
			EnrichmentStatus: types.StatusPending,
			Properties: map[string]string{
				"service_type": svcType,
				"inject_name":  svcName,
				"component":    componentName,
			},
		})
	}

	// --- 3. Locate all @inject positions correctly ---------------------------
	// Re-scan inject positions individually for accurate line numbers.
	entities = rebuildInjectEntities(entities, src, file.Path, componentName)

	// --- 3a. @using directives → IMPORTS edges -------------------------------
	// Each @using emits a SCOPE.Component import-stub entity carrying a single
	// IMPORTS edge from the source file → the imported namespace. Mirrors the
	// contract used by the Clojure (#118) and Java (#120) extractors. Inserted
	// after rebuildInjectEntities, which retains only entities[:1].
	entities = append(entities, buildImportEntities(file.Path, src)...)

	// --- 4. @code block ------------------------------------------------------
	codeSrc, codeOffset, ok := extractCodeBlock(src)
	if !ok {
		// No @code block — return what we have (component + injects).
		return entities, nil
	}

	// --- 5. Parse @code block ------------------------------------------------
	codeEntities, err := extractFromCodeBlock(codeSrc, codeOffset, src, file.Path, componentName)
	if err != nil {
		// Non-fatal: append degraded marker but keep component entity.
		return degraded(err.Error()), nil
	}
	entities = append(entities, codeEntities...)

	// --- 6. CONTAINS edges: component → each event_handler (Format A
	// structural-ref keyed on file path so the resolver disambiguates
	// across components that share method names).
	for i := range entities {
		if entities[i].Subtype != "event_handler" {
			continue
		}
		toID := extractor.BuildOperationStructuralRef("razor", file.Path, entities[i].Name)
		entities[0].Relationships = append(entities[0].Relationships, types.RelationshipRecord{
			ToID: toID,
			Kind: "CONTAINS",
		})
	}

	return entities, nil
}

// buildImportEntities scans @using directives and emits one SCOPE.Component
// stub per unique namespace, each carrying a single IMPORTS edge from the
// source file → namespace. Matches the contract used by Clojure / Java
// extractors (Issue #378).
func buildImportEntities(filePath, src string) []types.EntityRecord {
	matches := reUsing.FindAllStringSubmatchIndex(src, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]types.EntityRecord, 0, len(matches))
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		ns := src[m[2]:m[3]]
		if ns == "" || seen[ns] {
			continue
		}
		seen[ns] = true
		lineNum := lineOf(src, m[0])
		out = append(out, types.EntityRecord{
			Name:             topSegment(ns),
			QualifiedName:    ns,
			Kind:             "SCOPE.Component",
			Subtype:          "import",
			SourceFile:       filePath,
			Language:         "razor",
			StartLine:        lineNum,
			EndLine:          lineNum,
			QualityScore:     0.85,
			EnrichmentStatus: types.StatusPending,
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   ns,
					Kind:   "IMPORTS",
					Properties: map[string]string{
						"source_module": parentNamespace(ns),
					},
				},
			},
		})
	}
	return out
}

// parentNamespace returns the dotted-parent of a namespace ("A.B.C" → "A.B").
func parentNamespace(dotted string) string {
	if dot := strings.LastIndexByte(dotted, '.'); dot > 0 {
		return dotted[:dot]
	}
	return dotted
}

// topSegment returns the first dotted segment ("A.B.C" → "A").
func topSegment(dotted string) string {
	if dot := strings.IndexByte(dotted, '.'); dot > 0 {
		return dotted[:dot]
	}
	return dotted
}

// collectCalls scans body for identifier-followed-by-paren patterns and
// returns deduped CALLS edges, dropping C# reserved words and the caller's
// own name (self-recursion).
func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	matches := reCallHead.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]types.RelationshipRecord, 0, len(matches))
	for _, m := range matches {
		head := m[1]
		if head == "" || csKeywords[head] {
			continue
		}
		if head == callerName {
			continue
		}
		if seen[head] {
			continue
		}
		seen[head] = true
		out = append(out, types.RelationshipRecord{
			ToID: head,
			Kind: "CALLS",
		})
	}
	return out
}

// extractMethodBody returns the substring inside the matching {} that follows
// the method signature beginning at sigStart (offset within codeSrc). Returns
// "" if no balanced body is found.
func extractMethodBody(codeSrc string, sigStart int) string {
	openIdx := strings.IndexByte(codeSrc[sigStart:], '{')
	if openIdx < 0 {
		return ""
	}
	abs := sigStart + openIdx
	depth := 0
	for i := abs; i < len(codeSrc); i++ {
		switch codeSrc[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return codeSrc[abs+1 : i]
			}
		}
	}
	return ""
}

// componentNameFromPath derives the PascalCase component name from the file path.
// "path/to/Counter.razor" → "Counter"
func componentNameFromPath(path string) string {
	base := filepath.Base(path)
	// Strip .razor extension
	name := strings.TrimSuffix(base, ".razor")
	name = strings.TrimSuffix(name, ".Razor")
	if name == "" {
		return "Unknown"
	}
	return name
}

// extractCodeBlock locates the @code { ... } block using brace-counting.
// Returns the block content (without outer braces), the byte offset of the
// opening brace in the original src, and whether a block was found.
func extractCodeBlock(src string) (content string, openBraceOffset int, found bool) {
	loc := reCodeBlock.FindStringIndex(src)
	if loc == nil {
		return "", 0, false
	}
	// loc[1]-1 is the position of '{' that reCodeBlock matched.
	braceStart := loc[1] - 1
	depth := 0
	start := -1
	end := -1
	for i := braceStart; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
			if depth == 1 {
				start = i + 1
			}
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if depth == 0 && start != -1 {
			break
		}
	}
	if start == -1 || end == -1 || end <= start {
		return "", 0, false
	}
	return src[start:end], start, true
}

// extractFromCodeBlock parses parameters and event handlers from the @code block.
func extractFromCodeBlock(codeSrc string, codeOffset int, fullSrc, filePath, componentName string) ([]types.EntityRecord, error) {
	var entities []types.EntityRecord

	lines := strings.Split(codeSrc, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if reParamAttr.MatchString(trimmed) {
			// Next non-empty line should be the property declaration.
			for j := i + 1; j < len(lines); j++ {
				propLine := strings.TrimSpace(lines[j])
				if propLine == "" {
					continue
				}
				m := rePropDecl.FindStringSubmatch(propLine)
				if m != nil {
					propType := m[1]
					propName := m[2]
					lineNum := lineOf(fullSrc, codeOffset) + i
					entities = append(entities, types.EntityRecord{
						Name:             propName,
						QualifiedName:    fmt.Sprintf("%s.%s", componentName, propName),
						Kind:             "SCOPE.Component",
						Subtype:          "parameter",
						SourceFile:       filePath,
						Language:         "razor",
						StartLine:        lineNum,
						EndLine:          lineNum,
						QualityScore:     0.85,
						EnrichmentStatus: types.StatusPending,
						Properties: map[string]string{
							"property_type": propType,
							"property_name": propName,
							"component":     componentName,
						},
					})
				}
				break
			}
			continue
		}

		// Event handler detection: void/async void/Task methods.
		if reEventHandler.MatchString(trimmed) {
			m := reEventHandler.FindStringSubmatch(trimmed)
			if m != nil {
				methodName := m[1]
				lineNum := lineOf(fullSrc, codeOffset) + i

				// Locate this signature within codeSrc to capture the body
				// for CALLS scanning. We compute the byte offset of `line`
				// within codeSrc by re-walking lines (cheap; codeSrc is small).
				var lineByteOffset int
				for k := 0; k < i; k++ {
					lineByteOffset += len(lines[k]) + 1 // +1 for the \n consumed by Split
				}
				body := extractMethodBody(codeSrc, lineByteOffset)
				calls := collectCalls(body, methodName)

				entities = append(entities, types.EntityRecord{
					Name:             methodName,
					QualifiedName:    fmt.Sprintf("%s.%s", componentName, methodName),
					Kind:             "SCOPE.Operation",
					Subtype:          "event_handler",
					SourceFile:       filePath,
					Language:         "razor",
					StartLine:        lineNum,
					EndLine:          lineNum,
					QualityScore:     0.85,
					EnrichmentStatus: types.StatusPending,
					Properties: map[string]string{
						"method_name": methodName,
						"component":   componentName,
					},
					Relationships: calls,
				})
			}
		}
	}

	return entities, nil
}

// rebuildInjectEntities replaces the rough inject entities built during the
// initial scan with accurate line numbers for each individual match.
func rebuildInjectEntities(entities []types.EntityRecord, src, filePath, componentName string) []types.EntityRecord {
	// Remove previous inject entities (all but the first = component entity).
	rebuilt := entities[:1] // keep component entity only

	for _, m := range reInject.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		svcType := src[m[2]:m[3]]
		svcName := src[m[4]:m[5]]
		lineNum := lineOf(src, m[0])
		rebuilt = append(rebuilt, types.EntityRecord{
			Name:             svcName,
			QualifiedName:    fmt.Sprintf("%s.%s", componentName, svcName),
			Kind:             "SCOPE.UIComponent",
			Subtype:          "inject",
			SourceFile:       filePath,
			Language:         "razor",
			StartLine:        lineNum,
			EndLine:          lineNum,
			QualityScore:     0.85,
			EnrichmentStatus: types.StatusPending,
			Properties: map[string]string{
				"service_type": svcType,
				"inject_name":  svcName,
				"component":    componentName,
			},
		})
	}
	return rebuilt
}

// lineOf returns the 1-based line number of the given byte offset in src.
func lineOf(src string, offset int) int {
	if offset <= 0 {
		return 1
	}
	if offset > len(src) {
		offset = len(src)
	}
	return strings.Count(src[:offset], "\n") + 1
}
