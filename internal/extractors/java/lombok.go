// Package java — Lombok annotation-driven entity synthesis.
//
// Issue #793 (sub-story (a) of #787): when the Java extractor encounters
// Lombok annotations on a class or field, it calls synthesizeLombokEntities
// which produces SCOPE.Operation and SCOPE.Component entities for every
// method that Lombok would generate at compile time. Without these entities
// every call to Order.builder(), OrderBuilder.id(...), User.getId(), etc.
// lands as a bug-extractor unresolved reference.
//
// Design mirrors the drf_viewset_implicit_method synthesizer from #783:
//   - Synthesized entities carry pattern_type and synthesized_from in
//     Properties so downstream consumers can distinguish them from
//     extractor-emitted real entities.
//   - QualityScore = 0.7 (real extractor entities are 1.0, http_endpoint
//     synthetics are 0.8), so dedup prefers the real entity if the
//     annotated processor ever also generates source.
//   - All entities tagged language="java" (TagRelationshipsLanguage runs
//     in the main Extract loop, so relationships on these are stamped too).
//
// Annotations covered:
//   - @Builder / @SuperBuilder
//   - @Builder.Default (field-level; records annotation, no behavioral change)
//   - @Value (immutable: all-args constructor + getters)
//   - @Data (getter + setter + constructor + equals/hashCode/toString)
//   - @Getter / @Setter (class or field level)
//   - @AllArgsConstructor / @RequiredArgsConstructor / @NoArgsConstructor
//   - @With (withFieldName copy-constructor per field)
//   - @Accessors(chain=true) (fluent setters returning this)
//   - @Singular on collection field (addItem + items(Iterable) builder methods)
//   - @Builder on static method (builder for method return type)
package java

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// lombokSynthQuality is the quality score for all Lombok-synthesized entities.
// Below real extractor entities (1.0) so the indexer dedup layer prefers
// a real entity if one is ever also parsed from source.
const lombokSynthQuality = 0.7

// lombokAnnotations is the set of Lombok annotation names (without @) that
// trigger entity synthesis. Checked case-sensitively — Java annotation names
// are PascalCase by convention and Lombok adheres to that.
var lombokAnnotations = map[string]bool{
	"Builder":                 true,
	"SuperBuilder":            true,
	"Value":                   true,
	"Data":                    true,
	"Getter":                  true,
	"Setter":                  true,
	"AllArgsConstructor":      true,
	"RequiredArgsConstructor": true,
	"NoArgsConstructor":       true,
	"With":                    true,
	"Accessors":               true,
	"Singular":                true,
}

// detectedAnnotations returns the set of Lombok annotation names present in
// rawSource (the text covering the class declaration including annotations).
// Detects both `@Builder` and `@lombok.Builder` forms. Also detects
// `@Builder.Default` and `@Builder.ObtainVia` on fields.
func detectedAnnotations(rawSource string) map[string]bool {
	out := make(map[string]bool)
	for i := 0; i < len(rawSource); i++ {
		if rawSource[i] != '@' {
			continue
		}
		// Read the identifier chain that follows @.
		j := i + 1
		for j < len(rawSource) && isAnnotationIdentChar(rawSource[j]) {
			j++
		}
		name := rawSource[i+1 : j]

		// Handle dotted names. Three shapes arise in practice:
		//   "@Builder"           → name="Builder"          (simple)
		//   "@lombok.Builder"    → name="lombok.Builder"   (package-qualified)
		//   "@Builder.Default"   → name="Builder.Default"  (nested member)
		//   "@lombok.extern.slf4j.Slf4j" → ignored (not in lombokAnnotations)
		if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
			leaf := name[dot+1:]
			prefix := name[:dot]
			// Case 1: leaf is a Lombok annotation — e.g. "lombok.Builder"
			//         leaf="Builder", prefix="lombok" → register "Builder"
			if lombokAnnotations[leaf] {
				out[leaf] = true
			}
			// Case 2: prefix is a Lombok annotation — e.g. "Builder.Default"
			//         prefix="Builder", leaf="Default" → register "Builder" + "Builder.Default"
			// Strip any package path from prefix first.
			prefixLeaf := prefix
			if pdot := strings.LastIndexByte(prefix, '.'); pdot >= 0 {
				prefixLeaf = prefix[pdot+1:]
			}
			if lombokAnnotations[prefixLeaf] {
				out[prefixLeaf] = true
				// Also record the dotted form (e.g. "Builder.Default").
				out[prefixLeaf+"."+leaf] = true
			}
		} else {
			if lombokAnnotations[name] {
				out[name] = true
			}
		}
		i = j - 1
	}
	return out
}

func isAnnotationIdentChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '.'
}

// hasAnnotation reports whether anns contains annotationName.
func hasAnnotation(anns map[string]bool, annotationName string) bool {
	return anns[annotationName]
}

// isAccessorsChain reports whether @Accessors(chain=true) is present in
// rawSource. Simple substring scan — sufficient for the cases Lombok supports.
func isAccessorsChain(rawSource string) bool {
	const marker = "@Accessors"
	for start := 0; start < len(rawSource); {
		idx := strings.Index(rawSource[start:], marker)
		if idx < 0 {
			break
		}
		abs := start + idx
		paren := strings.Index(rawSource[abs:], "(")
		if paren < 0 {
			break
		}
		close := strings.Index(rawSource[abs+paren:], ")")
		if close < 0 {
			break
		}
		args := rawSource[abs+paren+1 : abs+paren+close]
		if strings.Contains(args, "chain") && strings.Contains(args, "true") {
			return true
		}
		start = abs + 1
	}
	return false
}

// lombokField represents a parsed field on a class — name + declared type
// + per-field annotations.
type lombokField struct {
	name             string
	typeName         string // leaf type (e.g. "String", "List")
	rawType          string // full type text (e.g. "List<String>")
	annotations      map[string]bool
	isSingular       bool // @Singular detected on this field
	isBuilderDefault bool // @Builder.Default detected on this field
}

// collectLombokFields extracts field declarations from the raw class body
// text. Returns slice of lombokField. We parse raw text rather than the
// tree-sitter AST to keep this self-contained and avoid a second tree
// traversal. Accuracy is sufficient: we only need the field names and leaf
// types.
//
// A line is treated as a field declaration when it:
//   - ends with ';' (after trimming)
//   - has no '(' before the ';' (method calls/declarations have parens)
func collectLombokFields(classBodySrc string) []lombokField {
	var fields []lombokField
	lines := strings.Split(classBodySrc, "\n")
	var pendingAnns []string
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		// Annotation line?
		if strings.HasPrefix(line, "@") {
			pendingAnns = append(pendingAnns, line)
			continue
		}
		if !strings.Contains(line, ";") {
			pendingAnns = nil
			continue
		}
		// Skip method lines — they have '(' before ';'.
		semiIdx := strings.Index(line, ";")
		beforeSemi := line[:semiIdx]
		if strings.Contains(beforeSemi, "(") {
			pendingAnns = nil
			continue
		}
		f := parseFieldLine(beforeSemi)
		if f.name == "" || f.typeName == "" {
			pendingAnns = nil
			continue
		}
		// Merge pending annotations.
		f.annotations = make(map[string]bool)
		for _, annLine := range pendingAnns {
			for ann := range detectedAnnotations(annLine) {
				f.annotations[ann] = true
			}
		}
		f.isSingular = f.annotations["Singular"]
		f.isBuilderDefault = f.annotations["Builder.Default"] || f.annotations["Builder"]
		fields = append(fields, f)
		pendingAnns = nil
	}
	return fields
}

// parseFieldLine parses a single Java field declaration line (before ';'),
// returning name and type info. Handles modifiers, generics, arrays.
func parseFieldLine(line string) lombokField {
	modifiers := map[string]bool{
		"public": true, "private": true, "protected": true,
		"static": true, "final": true, "transient": true, "volatile": true,
	}
	tokens := strings.Fields(line)
	var filtered []string
	for _, t := range tokens {
		if strings.HasPrefix(t, "@") {
			continue // inline annotation
		}
		if modifiers[t] {
			continue
		}
		filtered = append(filtered, t)
	}
	// filtered[0] = type, filtered[1] = name (possibly "name=init...")
	if len(filtered) < 2 {
		return lombokField{}
	}
	typeStr := filtered[0]
	nameStr := filtered[1]
	// Strip initializer from name token.
	if idx := strings.IndexByte(nameStr, '='); idx >= 0 {
		nameStr = nameStr[:idx]
	}
	nameStr = strings.TrimSpace(nameStr)
	if nameStr == "" {
		return lombokField{}
	}
	leafType := leafTypeFromString(typeStr)
	if leafType == "" {
		return lombokField{}
	}
	return lombokField{
		name:     nameStr,
		typeName: leafType,
		rawType:  typeStr,
	}
}

// leafTypeFromString extracts the leaf type name from a raw type string,
// stripping generic parameters (<>) and array markers ([]).
func leafTypeFromString(t string) string {
	if idx := strings.IndexByte(t, '<'); idx >= 0 {
		t = t[:idx]
	}
	if idx := strings.IndexByte(t, '['); idx >= 0 {
		t = t[:idx]
	}
	if dot := strings.LastIndexByte(t, '.'); dot >= 0 {
		t = t[dot+1:]
	}
	return strings.TrimSpace(t)
}

// capitalise upper-cases the first byte of s (ASCII only — Java identifiers
// are overwhelmingly ASCII).
func capitalise(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 32
	}
	return string(b)
}

// getterName converts a field name to its Lombok-generated getter.
// Boolean fields use "is" prefix; all others use "get".
func getterName(fieldName, typeName string) string {
	if typeName == "boolean" || typeName == "Boolean" {
		return "is" + capitalise(fieldName)
	}
	return "get" + capitalise(fieldName)
}

// setterName converts a field name to its Lombok-generated setter.
func setterName(fieldName string) string {
	return "set" + capitalise(fieldName)
}

// withName converts a field name to its @With-generated method.
func withName(fieldName string) string {
	return "with" + capitalise(fieldName)
}

// synthOp returns a synthesized SCOPE.Operation EntityRecord.
func synthOp(
	name, signature, sourceFile, synthesizedFrom, patternType string,
	extraProps map[string]string,
) types.EntityRecord {
	props := map[string]string{
		"synthesized_from": synthesizedFrom,
		"pattern_type":     patternType,
	}
	for k, v := range extraProps {
		props[k] = v
	}
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Operation",
		Subtype:      "method",
		SourceFile:   sourceFile,
		Language:     "java",
		Signature:    signature,
		QualityScore: lombokSynthQuality,
		Properties:   props,
	}
}

// synthComp returns a synthesized SCOPE.Component EntityRecord (for generated
// builder classes such as OrderBuilder).
func synthComp(name, subtype, sourceFile, synthesizedFrom string) types.EntityRecord {
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Component",
		Subtype:      subtype,
		SourceFile:   sourceFile,
		Language:     "java",
		QualityScore: lombokSynthQuality,
		Properties: map[string]string{
			"synthesized_from": synthesizedFrom,
			"pattern_type":     synthesizedFrom,
		},
	}
}

// synthConstructor generates a synthesized constructor entity. The name
// follows the extractor convention: className.className (e.g. Order.Order).
func synthConstructor(
	className string,
	fields []lombokField,
	kind string, // "all", "required", "none" — affects signature display
	sourceFile string,
	synthesizedFrom string,
) types.EntityRecord {
	var paramParts []string
	for _, f := range fields {
		paramParts = append(paramParts, f.rawType+" "+f.name)
	}
	sig := className + "(" + strings.Join(paramParts, ", ") + ")"
	return types.EntityRecord{
		Name:         className + "." + className,
		Kind:         "SCOPE.Operation",
		Subtype:      "constructor",
		SourceFile:   sourceFile,
		Language:     "java",
		Signature:    sig,
		QualityScore: lombokSynthQuality,
		Properties: map[string]string{
			"synthesized_from":  synthesizedFrom,
			"pattern_type":      "lombok_constructor",
			"constructor_kind":  kind,
		},
	}
}

// synthesizeLombokEntities inspects the raw class declaration source and
// returns synthesized entities for all Lombok-generated methods detected.
// Called from the walk function immediately after a class entity is appended.
//
// classDeclSrc is the full class declaration text (annotations + declaration
// tokens, up to but not including the body).
// classBodySrc is the text of the class body (between { and }).
// Returns nil when no Lombok annotations are detected.
func synthesizeLombokEntities(
	className string,
	classDeclSrc string,
	classBodySrc string,
	sourceFile string,
) []types.EntityRecord {
	anns := detectedAnnotations(classDeclSrc)
	if len(anns) == 0 {
		return nil
	}

	// Gate: at least one known Lombok annotation must be present.
	hasLombok := false
	for _, known := range []string{
		"Builder", "SuperBuilder", "Value", "Data",
		"Getter", "Setter",
		"AllArgsConstructor", "RequiredArgsConstructor", "NoArgsConstructor",
		"With", "Accessors",
	} {
		if anns[known] {
			hasLombok = true
			break
		}
	}
	if !hasLombok {
		return nil
	}

	fields := collectLombokFields(classBodySrc)
	var out []types.EntityRecord

	hasBuilder := hasAnnotation(anns, "Builder") || hasAnnotation(anns, "SuperBuilder")
	hasData := hasAnnotation(anns, "Data")
	hasValue := hasAnnotation(anns, "Value")
	hasGetter := hasAnnotation(anns, "Getter")
	hasSetter := hasAnnotation(anns, "Setter")
	hasAllArgs := hasAnnotation(anns, "AllArgsConstructor")
	hasRequired := hasAnnotation(anns, "RequiredArgsConstructor")
	hasNoArgs := hasAnnotation(anns, "NoArgsConstructor")
	hasWith := hasAnnotation(anns, "With")
	accessorsChain := hasAnnotation(anns, "Accessors") && isAccessorsChain(classDeclSrc)

	// -----------------------------------------------------------------------
	// @Builder / @SuperBuilder
	// -----------------------------------------------------------------------
	if hasBuilder {
		synthFrom := "lombok_builder"
		if hasAnnotation(anns, "SuperBuilder") {
			synthFrom = "lombok_super_builder"
		}
		builderClass := className + "Builder"

		// 1. Builder class entity (e.g. OrderBuilder).
		out = append(out, synthComp(builderClass, "class", sourceFile, synthFrom))

		// 2. Static factory: Order.builder() → OrderBuilder.
		out = append(out, synthOp(
			className+".builder",
			builderClass+" builder()",
			sourceFile,
			synthFrom,
			"lombok_builder",
			map[string]string{"returns": builderClass, "is_static": "true"},
		))

		// 3. Fluent setters on the builder — one per field.
		for _, f := range fields {
			if f.isSingular {
				// @Singular: generate addItem(T) + items(Iterable) instead.
				singular := f.name
				if strings.HasSuffix(singular, "s") && len(singular) > 1 {
					singular = singular[:len(singular)-1]
				}
				out = append(out, synthOp(
					builderClass+".add"+capitalise(singular),
					builderClass+" add"+capitalise(singular)+"("+f.typeName+" item)",
					sourceFile,
					"lombok_singular",
					"lombok_builder",
					map[string]string{"returns": builderClass, "field": f.name},
				))
				out = append(out, synthOp(
					builderClass+"."+f.name,
					builderClass+" "+f.name+"(Iterable<? extends "+f.typeName+"> elements)",
					sourceFile,
					"lombok_singular",
					"lombok_builder",
					map[string]string{"returns": builderClass, "field": f.name},
				))
			} else {
				out = append(out, synthOp(
					builderClass+"."+f.name,
					builderClass+" "+f.name+"("+f.rawType+" "+f.name+")",
					sourceFile,
					synthFrom,
					"lombok_builder",
					map[string]string{"returns": builderClass, "field": f.name},
				))
			}
		}

		// 4. build() → returns className.
		out = append(out, synthOp(
			builderClass+".build",
			className+" build()",
			sourceFile,
			synthFrom,
			"lombok_builder",
			map[string]string{"returns": className},
		))
	}

	// -----------------------------------------------------------------------
	// @Value — immutable class: all-args constructor + getters (no setters)
	// -----------------------------------------------------------------------
	if hasValue {
		out = append(out, synthConstructor(className, fields, "all", sourceFile, "lombok_value"))
		for _, f := range fields {
			out = append(out, synthOp(
				className+"."+getterName(f.name, f.typeName),
				f.typeName+" "+getterName(f.name, f.typeName)+"()",
				sourceFile,
				"lombok_value",
				"lombok_accessor",
				map[string]string{"field": f.name, "accessor_kind": "getter"},
			))
		}
	}

	// -----------------------------------------------------------------------
	// @Data — getter + setter + constructor + equals/hashCode/toString
	// -----------------------------------------------------------------------
	if hasData {
		out = append(out, synthConstructor(className, fields, "required", sourceFile, "lombok_data"))
		for _, f := range fields {
			// Getter
			out = append(out, synthOp(
				className+"."+getterName(f.name, f.typeName),
				f.typeName+" "+getterName(f.name, f.typeName)+"()",
				sourceFile,
				"lombok_data",
				"lombok_accessor",
				map[string]string{"field": f.name, "accessor_kind": "getter"},
			))
			// Setter
			out = append(out, synthOp(
				className+"."+setterName(f.name),
				"void "+setterName(f.name)+"("+f.rawType+" "+f.name+")",
				sourceFile,
				"lombok_data",
				"lombok_accessor",
				map[string]string{"field": f.name, "accessor_kind": "setter"},
			))
		}
		out = append(out, synthOp(className+".equals", "boolean equals(Object o)", sourceFile, "lombok_data", "lombok_accessor", nil))
		out = append(out, synthOp(className+".hashCode", "int hashCode()", sourceFile, "lombok_data", "lombok_accessor", nil))
		out = append(out, synthOp(className+".toString", "String toString()", sourceFile, "lombok_data", "lombok_accessor", nil))
	}

	// -----------------------------------------------------------------------
	// @Getter (class-level) — getter per field; skip when @Data or @Value
	// already covered them.
	// -----------------------------------------------------------------------
	if hasGetter && !hasData && !hasValue {
		for _, f := range fields {
			out = append(out, synthOp(
				className+"."+getterName(f.name, f.typeName),
				f.typeName+" "+getterName(f.name, f.typeName)+"()",
				sourceFile,
				"lombok_getter",
				"lombok_accessor",
				map[string]string{"field": f.name, "accessor_kind": "getter"},
			))
		}
	}

	// -----------------------------------------------------------------------
	// @Setter (class-level) — setter per field; skip when @Data already did.
	// -----------------------------------------------------------------------
	if hasSetter && !hasData {
		for _, f := range fields {
			out = append(out, synthOp(
				className+"."+setterName(f.name),
				"void "+setterName(f.name)+"("+f.rawType+" "+f.name+")",
				sourceFile,
				"lombok_setter",
				"lombok_accessor",
				map[string]string{"field": f.name, "accessor_kind": "setter"},
			))
		}
	}

	// -----------------------------------------------------------------------
	// @Accessors(chain=true) — fluent setters that return `this`
	// -----------------------------------------------------------------------
	if accessorsChain {
		for _, f := range fields {
			out = append(out, synthOp(
				className+"."+setterName(f.name),
				className+" "+setterName(f.name)+"("+f.rawType+" "+f.name+")",
				sourceFile,
				"lombok_accessors_chain",
				"lombok_accessor",
				map[string]string{"field": f.name, "accessor_kind": "setter", "chain": "true"},
			))
		}
	}

	// -----------------------------------------------------------------------
	// @AllArgsConstructor — skip when @Data or @Value already generated one.
	// -----------------------------------------------------------------------
	if hasAllArgs && !hasData && !hasValue {
		out = append(out, synthConstructor(className, fields, "all", sourceFile, "lombok_all_args_constructor"))
	}

	// -----------------------------------------------------------------------
	// @RequiredArgsConstructor
	// -----------------------------------------------------------------------
	if hasRequired && !hasData {
		// We conservatively emit with all fields; required = final/non-null
		// fields but we can't easily determine finality from text scanning.
		out = append(out, synthConstructor(className, fields, "required", sourceFile, "lombok_required_args_constructor"))
	}

	// -----------------------------------------------------------------------
	// @NoArgsConstructor
	// -----------------------------------------------------------------------
	if hasNoArgs {
		out = append(out, types.EntityRecord{
			Name:         className + "." + className,
			Kind:         "SCOPE.Operation",
			Subtype:      "constructor",
			SourceFile:   sourceFile,
			Language:     "java",
			Signature:    className + "()",
			QualityScore: lombokSynthQuality,
			Properties: map[string]string{
				"synthesized_from": "lombok_no_args_constructor",
				"pattern_type":     "lombok_constructor",
				"constructor_kind": "none",
			},
		})
	}

	// -----------------------------------------------------------------------
	// @With — withFieldName(T) returning a copy, per field
	// -----------------------------------------------------------------------
	if hasWith {
		for _, f := range fields {
			out = append(out, synthOp(
				className+"."+withName(f.name),
				className+" "+withName(f.name)+"("+f.rawType+" "+f.name+")",
				sourceFile,
				"lombok_with",
				"lombok_with",
				map[string]string{"field": f.name, "returns": className},
			))
		}
	}

	return out
}

// synthesizeFieldLevelLombok generates accessor entities for @Getter, @Setter,
// and @With annotations placed directly on individual fields (field-level),
// rather than on the class. Supplements synthesizeLombokEntities which
// handles class-level annotations. Dedup in the indexer pipeline handles
// any overlap.
func synthesizeFieldLevelLombok(
	className string,
	classBodySrc string,
	sourceFile string,
) []types.EntityRecord {
	fields := collectLombokFields(classBodySrc)
	var out []types.EntityRecord
	for _, f := range fields {
		if f.annotations["Getter"] {
			out = append(out, synthOp(
				className+"."+getterName(f.name, f.typeName),
				f.typeName+" "+getterName(f.name, f.typeName)+"()",
				sourceFile,
				"lombok_getter",
				"lombok_accessor",
				map[string]string{"field": f.name, "accessor_kind": "getter", "level": "field"},
			))
		}
		if f.annotations["Setter"] {
			out = append(out, synthOp(
				className+"."+setterName(f.name),
				"void "+setterName(f.name)+"("+f.rawType+" "+f.name+")",
				sourceFile,
				"lombok_setter",
				"lombok_accessor",
				map[string]string{"field": f.name, "accessor_kind": "setter", "level": "field"},
			))
		}
		if f.annotations["With"] {
			out = append(out, synthOp(
				className+"."+withName(f.name),
				className+" "+withName(f.name)+"("+f.rawType+" "+f.name+")",
				sourceFile,
				"lombok_with",
				"lombok_with",
				map[string]string{"field": f.name, "returns": className, "level": "field"},
			))
		}
	}
	return out
}
