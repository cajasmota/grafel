// Package scala — Scala type-system extractor.
//
// Covers (deepened to value-asserting bar, #3451):
//
//	Framework records         Cap                          Status
//	─────────────────────────────────────────────────────────────
//	all jvm_backend + play    Type System/type_extraction         full
//	all jvm_backend + play    Type System/enum_extraction         full
//	all jvm_backend + play    Type System/interface_extraction    full
//	all jvm_backend + play    Type System/type_alias_extraction   full
//
// Scala's type system:
//   - case class Foo(a: Int, b: String) → type_extraction; fields captured
//   - class Foo(val x: Int)             → type_extraction; ctor val/var fields
//   - object Foo                        → type_extraction (singleton/module)
//   - sealed trait / sealed abstract class + case object/class → enum_extraction
//     (Scala 2 ADT idiom); member cases collected from the same file
//   - enum Foo { case A, B, Rgb(v: Int) } → enum_extraction (Scala 3 enum);
//     case names captured, including parameterized cases
//   - trait Foo[F[_]] extends B { def m(...): R } → interface_extraction;
//     method names, supertraits and self-type captured
//   - type Alias[T] = SomeType          → type_alias_extraction; target captured
//   - opaque type Alias = T             → type_alias_extraction (Scala 3)
//
// Honest limit: regex/file-local. Cross-file ADT hierarchies (sealed trait in
// one file, subclasses in another) are only partially linked — the discriminant
// and any same-file members are captured; remote subclasses are not.
package scala

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
	extractor.Register("custom_scala_type_system", &scalaTypeSystemExtractor{})
}

type scalaTypeSystemExtractor struct{}

func (e *scalaTypeSystemExtractor) Language() string { return "custom_scala_type_system" }

// ---------------------------------------------------------------------------
// Regexes
// ---------------------------------------------------------------------------

var (
	// type_extraction: case class (value object / DTO). Captures name, optional
	// type-param block, and the primary-constructor parameter list.
	//   1=name 2=typeparams 3=params
	reCaseClass = regexp.MustCompile(
		`(?m)^\s*(?:final\s+)?case\s+class\s+(\w+)\s*(\[(?:[^\[\]]|\[[^\]]*\])*\])?\s*(?:\(([^)]*)\))?`)

	// type_extraction: plain class (NOT sealed/abstract — those are ADT/interface
	// and handled separately to avoid double-emit). 1=name 2=typeparams 3=ctor params
	rePlainClass = regexp.MustCompile(
		`(?m)^\s*(?:final\s+)?class\s+(\w+)\s*(\[(?:[^\[\]]|\[[^\]]*\])*\])?\s*(?:\(([^)]*)\))?`)

	// type_extraction: object (singleton companion)
	reObject = regexp.MustCompile(
		`(?m)^\s*(?:case\s+)?object\s+(\w+)\b`)

	// enum_extraction: sealed trait (Scala 2 ADT discriminant). 1=name 2=extends
	reSealedTrait = regexp.MustCompile(
		`(?m)^\s*sealed\s+(?:abstract\s+)?trait\s+(\w+)\b\s*(?:\[(?:[^\[\]]|\[[^\]]*\])*\])?\s*(?:extends\s+([^\{]+?))?\s*(?:\{|$)`)

	// enum_extraction: sealed abstract class (Scala 2 sealed base). 1=name
	reSealedAbstractClass = regexp.MustCompile(
		`(?m)^\s*sealed\s+abstract\s+class\s+(\w+)\b`)

	// enum_extraction: Scala 3 enum. 1=name 2=typeparams 3=extends-clause
	reScala3Enum = regexp.MustCompile(
		`(?m)^\s*enum\s+(\w+)\s*(\[(?:[^\[\]]|\[[^\]]*\])*\])?\s*(?:extends\s+([^\{]+))?\{`)

	// Scala 3 enum case line(s) inside the body. Captures the case fragment
	// after the `case` keyword; may list several comma-separated names and/or a
	// parameterized case: `case Red, Green` or `case Rgb(v: Int)`.
	reEnumCaseLine = regexp.MustCompile(
		`(?m)^\s*case\s+([A-Za-z_][\w, ()\[\]:<>.]*)`)

	// Each enum-case name token (drops parameter lists / type params).
	reEnumCaseName = regexp.MustCompile(`([A-Z]\w*)`)

	// interface_extraction: trait (not sealed). 1=name 2=typeparams 3=extends
	reTrait = regexp.MustCompile(
		`(?m)^\s*(?:private\s+|protected\s+)?(?:sealed\s+)?trait\s+(\w+)\s*(\[(?:[^\[\]]|\[[^\]]*\])*\])?\s*(?:extends\s+([^\{]+?))?\s*(?:\{|$)`)

	// interface_extraction: abstract class. 1=name 2=typeparams 3=extends
	reAbstractClass = regexp.MustCompile(
		`(?m)^\s*(?:private\s+|protected\s+)?abstract\s+class\s+(\w+)\s*(\[(?:[^\[\]]|\[[^\]]*\])*\])?\s*(?:\([^)]*\))?\s*(?:extends\s+([^\{]+?))?\s*(?:\{|$)`)

	// trait/abstract method signature: `def name(...)...` or `def name: R`.
	reDefMethod = regexp.MustCompile(`(?m)^\s*def\s+(\w+)`)

	// self-type annotation inside a trait body: `self: Foo =>` / `this: Foo =>`.
	reSelfType = regexp.MustCompile(`(?m)^\s*(?:\w+)\s*:\s*([\w\[\] .]+?)\s*=>`)

	// type_alias_extraction: type Alias[..] = RHS. 1=name 2=typeparams 3=rhs
	reTypeAlias = regexp.MustCompile(
		`(?m)^\s*type\s+(\w+)\s*(\[[^\]]*\])?\s*=\s*(.+)`)

	// type_alias_extraction: opaque type (Scala 3). 1=name 2=typeparams 3=rhs
	reOpaqueType = regexp.MustCompile(
		`(?m)^\s*opaque\s+type\s+(\w+)\s*(\[[^\]]*\])?\s*=\s*(.+)`)

	// A top-level type-declaration keyword on its own line — used to detect that
	// a bodyless trait/class header is NOT the owner of a later `{ ... }` block.
	scalaReDeclBreak = regexp.MustCompile(
		`(?m)^\s*(?:final\s+|sealed\s+|abstract\s+|private\s+|protected\s+|case\s+)*(?:trait|class|object|enum)\b`)

	// case object/class that names a parent (Scala 2 enum-member idiom).
	//   1=member 2=parent
	reAdtMember = regexp.MustCompile(
		`(?m)^\s*(?:final\s+)?case\s+(?:object|class)\s+(\w+)[^\n]*\bextends\s+(\w+)`)
)

// ---------------------------------------------------------------------------
// Field / member parsing helpers
// ---------------------------------------------------------------------------

// scalaParseFields turns a primary-constructor parameter list into a
// comma-joined list of field names, e.g. "id: Long, name: String" → "id,name".
// Handles default values and val/var modifiers; ignores nested generic commas.
func scalaParseFields(params string) string {
	params = strings.TrimSpace(params)
	if params == "" {
		return ""
	}
	var names []string
	for _, part := range scalaSplitTopLevel(params, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// strip leading modifiers like `val`, `var`, `private val`, `override`.
		for _, mod := range []string{"private ", "protected ", "override ", "final ", "implicit ", "val ", "var "} {
			for strings.HasPrefix(part, mod) {
				part = strings.TrimSpace(strings.TrimPrefix(part, mod))
			}
		}
		// name is up to the first ':'.
		if idx := strings.IndexByte(part, ':'); idx >= 0 {
			name := strings.TrimSpace(part[:idx])
			if name != "" {
				names = append(names, name)
			}
		} else if part != "" {
			names = append(names, part)
		}
	}
	return strings.Join(names, ",")
}

// scalaSplitTopLevel splits s on sep, but only at bracket-depth 0 so that
// generic parameter commas (List[A, B]) and tuple commas don't over-split.
func scalaSplitTopLevel(s string, sep byte) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			if depth > 0 {
				depth--
			}
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// scalaTrimTypeParams strips the surrounding brackets from a "[A, B]" block.
func scalaTrimTypeParams(tp string) string {
	tp = strings.TrimSpace(tp)
	tp = strings.TrimPrefix(tp, "[")
	tp = strings.TrimSuffix(tp, "]")
	return strings.TrimSpace(tp)
}

// scalaEnumBody returns the brace-delimited body following the enum header at
// headerEnd (the offset just past the opening '{').
func scalaEnumBody(src string, openBrace int) string {
	depth := 0
	for i := openBrace; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[openBrace+1 : i]
			}
		}
	}
	return src[openBrace:]
}

// scalaParseEnumCases extracts the distinct case names declared in an enum body.
func scalaParseEnumCases(body string) []string {
	var cases []string
	seen := map[string]bool{}
	for _, m := range reEnumCaseLine.FindAllStringSubmatch(body, -1) {
		frag := m[1]
		// A parameterized case `Rgb(v: Int)` — keep only the name before '('.
		// Multiple bare cases `Red, Green, Blue` — split at top level.
		for _, piece := range scalaSplitTopLevel(frag, ',') {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}
			nm := reEnumCaseName.FindString(piece)
			if nm != "" && !seen[nm] {
				seen[nm] = true
				cases = append(cases, nm)
			}
		}
	}
	return cases
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *scalaTypeSystemExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.scala_type_system.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("extractor", "type_system"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
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

	// Pre-index Scala 2 ADT members (case object/class ... extends Parent) so a
	// sealed trait/abstract base can list its same-file variants.
	adtMembers := map[string][]string{}
	for _, m := range reAdtMember.FindAllStringSubmatch(src, -1) {
		member, parent := m[1], m[2]
		adtMembers[parent] = append(adtMembers[parent], member)
	}

	// --- type_extraction: case class (value objects / DTOs) ---
	for _, m := range reCaseClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "case_class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "case_class",
			"provenance", "SCALA_CASE_CLASS",
		)
		if m[4] >= 0 {
			setProps(&ent, "type_params", scalaTrimTypeParams(src[m[4]:m[5]]))
		}
		if m[6] >= 0 {
			if fields := scalaParseFields(src[m[6]:m[7]]); fields != "" {
				setProps(&ent, "fields", fields)
			}
		}
		add(ent)
	}

	// --- type_extraction: plain class ---
	for _, m := range rePlainClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "class",
			"provenance", "SCALA_CLASS",
		)
		if m[4] >= 0 {
			setProps(&ent, "type_params", scalaTrimTypeParams(src[m[4]:m[5]]))
		}
		if m[6] >= 0 {
			if fields := scalaParseFields(src[m[6]:m[7]]); fields != "" {
				setProps(&ent, "fields", fields)
			}
		}
		add(ent)
	}

	// --- type_extraction: object (companion/module) ---
	for _, m := range reObject.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "object", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "object",
			"provenance", "SCALA_OBJECT",
		)
		add(ent)
	}

	// --- enum_extraction: sealed trait (Scala 2 ADT) ---
	for _, m := range reSealedTrait.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "sealed_trait", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "sealed_trait",
			"provenance", "SCALA_SEALED_TRAIT",
			"is_adt", "true",
		)
		if m[4] >= 0 {
			if sup := strings.TrimSpace(src[m[4]:m[5]]); sup != "" {
				setProps(&ent, "extends", sup)
			}
		}
		if members := adtMembers[name]; len(members) > 0 {
			setProps(&ent, "enum_cases", strings.Join(members, ","))
		}
		add(ent)
	}

	// --- enum_extraction: sealed abstract class ---
	for _, m := range reSealedAbstractClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "sealed_abstract_class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "sealed_abstract_class",
			"provenance", "SCALA_SEALED_ABSTRACT_CLASS",
			"is_adt", "true",
		)
		if members := adtMembers[name]; len(members) > 0 {
			setProps(&ent, "enum_cases", strings.Join(members, ","))
		}
		add(ent)
	}

	// --- enum_extraction: Scala 3 enum ---
	for _, m := range reScala3Enum.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "enum", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "enum",
			"provenance", "SCALA3_ENUM",
			"is_adt", "true",
		)
		if m[4] >= 0 {
			setProps(&ent, "type_params", scalaTrimTypeParams(src[m[4]:m[5]]))
		}
		if m[6] >= 0 {
			if sup := strings.TrimSpace(src[m[6]:m[7]]); sup != "" {
				setProps(&ent, "extends", sup)
			}
		}
		// Locate the opening brace (last index of the full match) and parse cases.
		openBrace := strings.LastIndexByte(src[m[0]:m[1]], '{')
		if openBrace >= 0 {
			body := scalaEnumBody(src, m[0]+openBrace)
			if cases := scalaParseEnumCases(body); len(cases) > 0 {
				setProps(&ent, "enum_cases", strings.Join(cases, ","))
			}
		}
		add(ent)
	}

	// --- interface_extraction: trait ---
	for _, m := range reTrait.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Interface", "trait", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "trait",
			"provenance", "SCALA_TRAIT",
		)
		if m[4] >= 0 {
			setProps(&ent, "type_params", scalaTrimTypeParams(src[m[4]:m[5]]))
		}
		if m[6] >= 0 {
			if sup := strings.TrimSpace(src[m[6]:m[7]]); sup != "" {
				setProps(&ent, "extends", sup)
			}
		}
		scalaAnnotateBody(src, m[0], &ent)
		add(ent)
	}

	// --- interface_extraction: abstract class ---
	for _, m := range reAbstractClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Interface", "abstract_class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "abstract_class",
			"provenance", "SCALA_ABSTRACT_CLASS",
		)
		if m[4] >= 0 {
			setProps(&ent, "type_params", scalaTrimTypeParams(src[m[4]:m[5]]))
		}
		if m[6] >= 0 {
			if sup := strings.TrimSpace(src[m[6]:m[7]]); sup != "" {
				setProps(&ent, "extends", sup)
			}
		}
		scalaAnnotateBody(src, m[0], &ent)
		add(ent)
	}

	// --- type_alias_extraction: type Alias = ... ---
	for _, m := range reTypeAlias.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "type_alias", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "type_alias",
			"provenance", "SCALA_TYPE_ALIAS",
		)
		if m[4] >= 0 {
			setProps(&ent, "type_params", scalaTrimTypeParams(src[m[4]:m[5]]))
		}
		if m[6] >= 0 {
			setProps(&ent, "aliased_type", strings.TrimSpace(src[m[6]:m[7]]))
		}
		add(ent)
	}

	// --- type_alias_extraction: opaque type (Scala 3) ---
	for _, m := range reOpaqueType.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Type", "opaque_type", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"type_kind", "opaque_type",
			"provenance", "SCALA3_OPAQUE_TYPE",
		)
		if m[4] >= 0 {
			setProps(&ent, "type_params", scalaTrimTypeParams(src[m[4]:m[5]]))
		}
		if m[6] >= 0 {
			setProps(&ent, "aliased_type", strings.TrimSpace(src[m[6]:m[7]]))
		}
		add(ent)
	}

	return entities, nil
}

// scalaAnnotateBody locates the brace-delimited body that begins at or after
// bodyStart and stamps method names (`methods`) and any self-type (`self_type`)
// onto ent. It is a no-op for traits/classes with no `{ ... }` block.
func scalaAnnotateBody(src string, bodyStart int, ent *types.EntityRecord) {
	open := strings.IndexByte(src[bodyStart:], '{')
	if open < 0 {
		return
	}
	// Guard against a bodyless trait/class (`trait Foo`) accidentally absorbing
	// a *later* type's body: if another top-level declaration keyword appears
	// between the header and the brace, this header has no body of its own.
	gap := src[bodyStart : bodyStart+open]
	// The header's own keyword is the first decl match in the gap. If a *second*
	// top-level declaration keyword appears before the brace, this header is
	// bodyless and the brace belongs to a later type — bail out.
	if locs := scalaReDeclBreak.FindAllStringIndex(gap, -1); len(locs) > 1 {
		return
	}
	body := scalaEnumBody(src, bodyStart+open)

	var methods []string
	seen := map[string]bool{}
	for _, mm := range reDefMethod.FindAllStringSubmatch(body, -1) {
		nm := mm[1]
		if !seen[nm] {
			seen[nm] = true
			methods = append(methods, nm)
		}
	}
	if len(methods) > 0 {
		setProps(ent, "methods", strings.Join(methods, ","))
	}
	if sm := reSelfType.FindStringSubmatch(body); sm != nil {
		setProps(ent, "self_type", strings.TrimSpace(sm[1]))
	}
}
