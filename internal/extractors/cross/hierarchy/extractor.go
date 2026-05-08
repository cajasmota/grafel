// Package hierarchy implements the cross-language class hierarchy extractor.
//
// Detects class inheritance (extends) and interface implementation (implements)
// across language families and emits EXTENDS and IMPLEMENTS relationships.
// Also marks is_abstract on every class entity.
//
// Supported languages:
//   - Java / C# / Kotlin / Scala / Dart / PHP / TypeScript / JavaScript:
//     [abstract] class Foo extends Bar implements Baz, Qux
//   - Python: class Foo(Bar, ABC):  (ABC/Protocol -> is_abstract=true)
//   - Ruby:   class Foo < Bar
//   - Go:     type Foo struct { Bar }  (embedded struct -> EXTENDS)
//   - Rust:   impl Trait for Struct   (-> IMPLEMENTS)
//   - Elixir: @behaviour Module       (-> IMPLEMENTS)
//   - Swift:  protocol Foo            (all protocols are abstract)
//
// Entity kind: "SCOPE.Component"
// Relationships emitted: EXTENDS, IMPLEMENTS
//
// OTel span: indexer.hierarchy_extractor.extract
// Attributes: file_path, language, classes_found, extends_found,
//
//	implements_found, relationships_found, abstract_classes_found
//
// Registration key: "_cross_hierarchy"
package hierarchy

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
	extractor.Register("_cross_hierarchy", &Extractor{})
}

// Extractor detects class hierarchy relationships across all languages.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_hierarchy" }

// ---------------------------------------------------------------------------
// Compiled regular expressions
// ---------------------------------------------------------------------------

// Java / TypeScript / C# / Kotlin / Scala / Dart / PHP
var jtcClassRE = regexp.MustCompile(
	`(?m)(?:^|[\s;{])(?:(abstract)\s+)?class\s+(\w+)(?:<[^>]*>)?` +
		`(?:\s+extends\s+([\w.<>, ]+?))?` +
		`(?:\s+implements\s+([\w.<>, ]+?))?(?:\s*\{|$|\s*:)`,
)

// Python: class Foo(Base1, Base2):
var pyClassRE = regexp.MustCompile(`(?m)^class\s+(\w+)\s*\(([^)]+)\)\s*:`)

// Python ABC/Protocol bases
var pyInterfaceBases = map[string]bool{
	"ABC": true, "ABCMeta": true, "Protocol": true,
	"Interface": true, "AbstractBase": true,
}

// Ruby: class Foo < Bar
var rubyClassRE = regexp.MustCompile(`(?m)^class\s+(\w+)\s*<\s*([\w:]+)`)

// Go embedded struct:
// type Foo struct { ... Bar ... *Baz ... }
var goStructRE = regexp.MustCompile(`(?ms)^type\s+(\w+)\s+struct\s*\{([^}]*)\}`)

// Embedded field: leading whitespace, optional *, uppercase name
var goEmbedRE = regexp.MustCompile(`(?m)^\s+\*?([A-Z]\w*)\s*(?://[^\n]*)?\s*$`)

// Rust: impl Trait for Struct
var rustImplRE = regexp.MustCompile(`(?m)^impl(?:<[^>]*>)?\s+([\w:<>]+)\s+for\s+(\w+)`)

// Elixir: @behaviour Module
var elixirBehaviourRE = regexp.MustCompile(`(?m)^\s*@behaviour\s+([\w.]+)`)

// Elixir: defmodule X
var elixirModuleRE = regexp.MustCompile(`(?m)^defmodule\s+([\w.]+)`)

// Swift: protocol Foo
var swiftProtocolRE = regexp.MustCompile(`(?m)^protocol\s+(\w+)`)

// Strip generic parameters: Foo<Bar> -> Foo
var genericRE = regexp.MustCompile(`<[^>]*>`)

// ---------------------------------------------------------------------------
// Ref builders
// ---------------------------------------------------------------------------

func classRef(filePath, name, language string) string {
	return "scope:component:class:" + language + ":" + filePath + ":" + name
}

func ifaceRef(name, language string) string {
	return "scope:component:interface:" + language + ":" + name
}

// ---------------------------------------------------------------------------
// Result accumulator
// ---------------------------------------------------------------------------

type result struct {
	entities       []types.EntityRecord
	classesFound   int
	extendsFound   int
	implementsFound int
	abstractFound  int
}

func (r *result) addEntity(e types.EntityRecord) {
	r.entities = append(r.entities, e)
}

func (r *result) addRel(from, to, kind string, props map[string]string) {
	// Attach relationship to a sentinel entity so it flows through the pipeline.
	r.entities = append(r.entities, types.EntityRecord{
		Name:       kind + ":" + from + "->" + to,
		Kind:       "relationship",
		Subtype:    strings.ToLower(kind),
		SourceFile: from,
		Language:   props["language"],
		Relationships: []types.RelationshipRecord{
			{
				FromID:     from,
				ToID:       to,
				Kind:       kind,
				Properties: props,
			},
		},
		QualityScore: 0.9,
	})
}

// ---------------------------------------------------------------------------
// Per-language extractors
// ---------------------------------------------------------------------------

func extractJTCSharp(source, filePath, language string, res *result) {
	for _, m := range jtcClassRE.FindAllStringSubmatch(source, -1) {
		abstract := m[1] != ""
		clsName := m[2]
		extendsRaw := strings.TrimSpace(m[3])
		implRaw := strings.TrimSpace(m[4])

		if extendsRaw == "" && implRaw == "" {
			continue
		}

		res.classesFound++
		if abstract {
			res.abstractFound++
		}
		clsID := classRef(filePath, clsName, language)
		res.addEntity(types.EntityRecord{
			Name:       clsName,
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: filePath,
			Language:   language,
			Properties: map[string]string{
				"role":        "class",
				"is_abstract": boolStr(abstract),
				"ref":         clsID,
				"provenance":  "INFERRED_FROM_CLASS_HIERARCHY",
			},
			QualityScore: 0.9,
		})

		if extendsRaw != "" {
			parentName := strings.TrimSpace(genericRE.ReplaceAllString(extendsRaw, ""))
			if parentName != "" {
				parentID := classRef(filePath, parentName, language)
				res.addEntity(types.EntityRecord{
					Name:       parentName,
					Kind:       "SCOPE.Component",
					Subtype:    "class",
					SourceFile: filePath,
					Language:   language,
					Properties: map[string]string{
						"role": "class", "ref": parentID,
						"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
					},
					QualityScore: 0.9,
				})
				res.addRel(clsID, parentID, "EXTENDS", map[string]string{"language": language})
				res.extendsFound++
			}
		}

		if implRaw != "" {
			for _, ifaceRaw := range strings.Split(implRaw, ",") {
				ifaceName := strings.TrimSpace(genericRE.ReplaceAllString(ifaceRaw, ""))
				if ifaceName == "" {
					continue
				}
				ifID := ifaceRef(ifaceName, language)
				res.addEntity(types.EntityRecord{
					Name:       ifaceName,
					Kind:       "SCOPE.Component",
					Subtype:    "interface",
					SourceFile: filePath,
					Language:   language,
					Properties: map[string]string{
						"role": "interface", "ref": ifID,
						"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
					},
					QualityScore: 0.9,
				})
				res.addRel(clsID, ifID, "IMPLEMENTS", map[string]string{"language": language})
				res.implementsFound++
			}
		}
	}
}

func extractPython(source, filePath string, res *result) {
	for _, m := range pyClassRE.FindAllStringSubmatch(source, -1) {
		clsName := m[1]
		basesRaw := strings.TrimSpace(m[2])
		if basesRaw == "" {
			continue
		}
		bases := splitBases(basesRaw)
		if len(bases) == 0 {
			continue
		}

		isAbstract := false
		for _, b := range bases {
			clean := stripSubscript(b)
			if pyInterfaceBases[clean] {
				isAbstract = true
				break
			}
		}

		res.classesFound++
		if isAbstract {
			res.abstractFound++
		}
		clsID := classRef(filePath, clsName, "python")
		res.addEntity(types.EntityRecord{
			Name:       clsName,
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: filePath,
			Language:   "python",
			Properties: map[string]string{
				"role":        "class",
				"is_abstract": boolStr(isAbstract),
				"ref":         clsID,
				"provenance":  "INFERRED_FROM_CLASS_HIERARCHY",
			},
			QualityScore: 0.9,
		})

		for _, base := range bases {
			clean := stripSubscript(base)
			if clean == "" {
				continue
			}
			if pyInterfaceBases[clean] {
				ifID := ifaceRef(clean, "python")
				res.addEntity(types.EntityRecord{
					Name:       clean,
					Kind:       "SCOPE.Component",
					Subtype:    "interface",
					SourceFile: filePath,
					Language:   "python",
					Properties: map[string]string{
						"role": "interface", "ref": ifID,
						"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
					},
					QualityScore: 0.9,
				})
				res.addRel(clsID, ifID, "IMPLEMENTS", map[string]string{"language": "python"})
				res.implementsFound++
			} else {
				parentID := classRef(filePath, clean, "python")
				res.addEntity(types.EntityRecord{
					Name:       clean,
					Kind:       "SCOPE.Component",
					Subtype:    "class",
					SourceFile: filePath,
					Language:   "python",
					Properties: map[string]string{
						"role": "class", "ref": parentID,
						"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
					},
					QualityScore: 0.9,
				})
				res.addRel(clsID, parentID, "EXTENDS", map[string]string{"language": "python"})
				res.extendsFound++
			}
		}
	}
}

func extractRuby(source, filePath string, res *result) {
	for _, m := range rubyClassRE.FindAllStringSubmatch(source, -1) {
		clsName := m[1]
		parentName := strings.TrimSpace(m[2])

		res.classesFound++
		clsID := classRef(filePath, clsName, "ruby")
		parentID := classRef(filePath, parentName, "ruby")

		res.addEntity(types.EntityRecord{
			Name: clsName, Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: filePath, Language: "ruby",
			Properties: map[string]string{
				"role": "class", "ref": clsID,
				"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
			},
			QualityScore: 0.9,
		})
		res.addEntity(types.EntityRecord{
			Name: parentName, Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: filePath, Language: "ruby",
			Properties: map[string]string{
				"role": "class", "ref": parentID,
				"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
			},
			QualityScore: 0.9,
		})
		res.addRel(clsID, parentID, "EXTENDS", map[string]string{"language": "ruby"})
		res.extendsFound++
	}
}

func extractGo(source, filePath string, res *result) {
	for _, sm := range goStructRE.FindAllStringSubmatch(source, -1) {
		clsName := sm[1]
		body := sm[2]

		var embedded []string
		for _, em := range goEmbedRE.FindAllStringSubmatch(body, -1) {
			embedded = append(embedded, em[1])
		}
		if len(embedded) == 0 {
			continue
		}

		res.classesFound++
		clsID := classRef(filePath, clsName, "go")
		res.addEntity(types.EntityRecord{
			Name: clsName, Kind: "SCOPE.Component", Subtype: "struct",
			SourceFile: filePath, Language: "go",
			Properties: map[string]string{
				"role": "struct", "ref": clsID,
				"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
			},
			QualityScore: 0.9,
		})

		for _, emb := range embedded {
			parentID := classRef(filePath, emb, "go")
			res.addEntity(types.EntityRecord{
				Name: emb, Kind: "SCOPE.Component", Subtype: "struct",
				SourceFile: filePath, Language: "go",
				Properties: map[string]string{
					"role": "struct", "ref": parentID,
					"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
				},
				QualityScore: 0.9,
			})
			res.addRel(clsID, parentID, "EXTENDS",
				map[string]string{"language": "go", "kind": "embedded_struct"})
			res.extendsFound++
		}
	}
}

func extractRust(source, filePath string, res *result) {
	for _, m := range rustImplRE.FindAllStringSubmatch(source, -1) {
		traitName := strings.TrimSpace(m[1])
		structName := strings.TrimSpace(m[2])

		res.classesFound++
		structID := classRef(filePath, structName, "rust")
		traitID := ifaceRef(traitName, "rust")

		res.addEntity(types.EntityRecord{
			Name: structName, Kind: "SCOPE.Component", Subtype: "struct",
			SourceFile: filePath, Language: "rust",
			Properties: map[string]string{
				"role": "struct", "ref": structID,
				"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
			},
			QualityScore: 0.9,
		})
		res.addEntity(types.EntityRecord{
			Name: traitName, Kind: "SCOPE.Component", Subtype: "trait",
			SourceFile: filePath, Language: "rust",
			Properties: map[string]string{
				"role": "trait", "ref": traitID,
				"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
			},
			QualityScore: 0.9,
		})
		res.addRel(structID, traitID, "IMPLEMENTS",
			map[string]string{"language": "rust", "kind": "trait_impl"})
		res.implementsFound++
	}
}

func extractElixir(source, filePath string, res *result) {
	moduleMatches := elixirModuleRE.FindAllStringSubmatchIndex(source, -1)
	behaviourMatches := elixirBehaviourRE.FindAllStringSubmatchIndex(source, -1)

	if len(behaviourMatches) == 0 {
		return
	}

	// Find the defmodule that precedes a given offset.
	currentModule := func(pos int) string {
		current := ""
		for _, mm := range moduleMatches {
			if mm[0] <= pos {
				current = source[mm[2]:mm[3]]
			} else {
				break
			}
		}
		return current
	}

	for _, bm := range behaviourMatches {
		behaviourName := strings.TrimSpace(source[bm[2]:bm[3]])
		moduleName := currentModule(bm[0])
		if moduleName == "" {
			continue
		}

		res.classesFound++
		modID := classRef(filePath, moduleName, "elixir")
		behID := ifaceRef(behaviourName, "elixir")

		res.addEntity(types.EntityRecord{
			Name: moduleName, Kind: "SCOPE.Component", Subtype: "module",
			SourceFile: filePath, Language: "elixir",
			Properties: map[string]string{
				"role": "module", "ref": modID,
				"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
			},
			QualityScore: 0.9,
		})
		res.addEntity(types.EntityRecord{
			Name: behaviourName, Kind: "SCOPE.Component", Subtype: "behaviour",
			SourceFile: filePath, Language: "elixir",
			Properties: map[string]string{
				"role": "behaviour", "ref": behID,
				"provenance": "INFERRED_FROM_CLASS_HIERARCHY",
			},
			QualityScore: 0.9,
		})
		res.addRel(modID, behID, "IMPLEMENTS",
			map[string]string{"language": "elixir", "kind": "behaviour"})
		res.implementsFound++
	}
}

func extractSwift(source, filePath string, res *result) {
	for _, m := range swiftProtocolRE.FindAllStringSubmatch(source, -1) {
		protoName := m[1]
		res.classesFound++
		res.abstractFound++
		protoID := classRef(filePath, protoName, "swift")
		res.addEntity(types.EntityRecord{
			Name: protoName, Kind: "SCOPE.Component", Subtype: "protocol",
			SourceFile: filePath, Language: "swift",
			Properties: map[string]string{
				"role":        "protocol",
				"is_abstract": "true",
				"ref":         protoID,
				"provenance":  "INFERRED_FROM_CLASS_HIERARCHY",
			},
			QualityScore: 0.9,
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// stripSubscript removes Python generic subscript: List[str] -> List
var subscriptRE = regexp.MustCompile(`\[.*\]`)

func stripSubscript(s string) string {
	return strings.TrimSpace(subscriptRE.ReplaceAllString(s, ""))
}

// splitBases splits a comma-separated base list, respecting nesting.
func splitBases(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, ch := range s {
		switch ch {
		case '(', '[', '<':
			depth++
		case ')', ']', '>':
			depth--
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(s[start:i])
				if part != "" {
					out = append(out, part)
				}
				start = i + 1
			}
		}
	}
	if tail := strings.TrimSpace(s[start:]); tail != "" {
		out = append(out, tail)
	}
	return out
}

// ---------------------------------------------------------------------------
// Public entry point
// ---------------------------------------------------------------------------

// supportedLanguages lists languages this extractor handles.
var supportedLanguages = map[string]bool{
	"java": true, "typescript": true, "javascript": true, "csharp": true,
	"kotlin": true, "scala": true, "dart": true, "php": true,
	"python": true, "ruby": true, "go": true, "rust": true,
	"elixir": true, "swift": true,
}

// Extract implements extractor.Extractor.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_hierarchy")
	ctx, span := tracer.Start(ctx, "indexer.hierarchy_extractor.extract")
	defer span.End()
	_ = ctx

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	lang := strings.ToLower(file.Language)
	if !supportedLanguages[lang] {
		span.SetAttributes(attribute.Int("classes_found", 0))
		return nil, nil
	}

	source := string(file.Content)
	res := &result{}

	switch lang {
	case "java", "typescript", "javascript", "csharp", "kotlin", "scala", "dart", "php":
		extractJTCSharp(source, file.Path, lang, res)
	case "python":
		extractPython(source, file.Path, res)
	case "ruby":
		extractRuby(source, file.Path, res)
	case "go":
		extractGo(source, file.Path, res)
	case "rust":
		extractRust(source, file.Path, res)
	case "elixir":
		extractElixir(source, file.Path, res)
	case "swift":
		extractSwift(source, file.Path, res)
	}

	span.SetAttributes(
		attribute.Int("classes_found", res.classesFound),
		attribute.Int("extends_found", res.extendsFound),
		attribute.Int("implements_found", res.implementsFound),
		attribute.Int("relationships_found", res.implementsFound+res.extendsFound),
		attribute.Int("abstract_classes_found", res.abstractFound),
	)

	return res.entities, nil
}
