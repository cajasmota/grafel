package java

import "regexp"

// Transactional custom extractor: @Transactional boundary, propagation, and
// rollback-rule extraction for JVM backend frameworks (#3003, epic #2847).
//
// Covers the Transactions lane:
//   - transaction_boundary_extraction: detect @Transactional on a class or
//     method and emit a SCOPE.Pattern(subtype=transaction_boundary) entity
//     linking the annotated method to its declaring class via OWNS.
//   - transaction_propagation: capture propagation=Propagation.<MODE> from the
//     annotation body (REQUIRED / REQUIRES_NEW / MANDATORY / SUPPORTS /
//     NOT_SUPPORTED / NEVER / NESTED).
//   - transaction_rollback_rules: capture rollbackFor / noRollbackFor class
//     lists from the annotation body.
//
// Both the Spring (org.springframework.transaction.annotation.Transactional)
// and Jakarta/JTA (jakarta.transaction.Transactional /
// javax.transaction.Transactional) annotations share the same simple name
// `@Transactional`; this extractor matches on the simple name and records the
// captured attributes regardless of which import supplied them.

// txFrameworks gates the frameworks for which @Transactional extraction runs.
// All entries share the Spring/JTA @Transactional annotation surface.
var txFrameworks = map[string]bool{
	"spring_boot": true, "spring-boot": true, "springboot": true,
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
	"quarkus":   true,
	"micronaut": true, "micronaut-core": true, "micronaut_core": true,
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"java_ee": true, "javaee": true,
	"jaxrs": true, "jax-rs": true, "jax_rs": true,
}

var (
	// txMethodRE matches @Transactional (with optional attribute body) on a
	// method declaration, capturing the optional annotation body (group 1) and
	// the method name (group 2). Modifiers/return type are skipped between the
	// annotation and the method name. The negative-lookahead-free form keeps
	// the regexp Go-RE2 compatible: a class declaration is filtered out by
	// rejecting the `class`/`interface`/`enum` keywords as the method name.
	txMethodRE = regexp.MustCompile(
		`(?s)@Transactional\b\s*(?:\(([^)]*)\))?\s*` +
			`(?:(?:public|protected|private|static|final|abstract|synchronized|default)\s+)*` +
			`(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)` +
			`(\w+)\s*\(`)

	// txClassRE matches @Transactional (with optional attribute body) on a
	// class/interface declaration, capturing the optional annotation body
	// (group 1) and the class name (group 2).
	txClassRE = regexp.MustCompile(
		`(?s)@Transactional\b\s*(?:\(([^)]*)\))?\s*` +
			`(?:(?:public|protected|private|abstract|final)\s+)*` +
			`(?:class|interface)\s+(\w+)`)

	// txPropagationRE extracts propagation=Propagation.<MODE> (Spring) or the
	// bare propagation=<MODE> form. Group 1 is the propagation mode.
	txPropagationRE = regexp.MustCompile(`propagation\s*=\s*(?:Propagation\.)?(\w+)`)
	// txJTATxTypeRE extracts the Jakarta/JTA positional propagation form
	// @Transactional(Transactional.TxType.REQUIRES_NEW) / TxType.MANDATORY etc.
	txJTATxTypeRE = regexp.MustCompile(`TxType\.(\w+)`)

	// txRollbackRE extracts rollbackFor=X.class (single) and the leading class
	// of a rollbackFor={A.class, B.class} list. All classes are captured by
	// scanning the matched body separately via txClassRefRE.
	txRollbackRE   = regexp.MustCompile(`rollbackFor\s*=\s*\{?([^}]*?)\}?(?:,\s*\w+\s*=|\)|$)`)
	txNoRollbackRE = regexp.MustCompile(`noRollbackFor\s*=\s*\{?([^}]*?)\}?(?:,\s*\w+\s*=|\)|$)`)
	// txClassRefRE pulls each `Foo.class` token out of a rollbackFor list body.
	txClassRefRE = regexp.MustCompile(`(\w+)\.class`)

	// txReadOnlyRE extracts readOnly=true|false.
	txReadOnlyRE = regexp.MustCompile(`readOnly\s*=\s*(true|false)`)
	// txIsolationRE extracts isolation=Isolation.<LEVEL> or bare isolation=<LEVEL>.
	txIsolationRE = regexp.MustCompile(`isolation\s*=\s*(?:Isolation\.)?(\w+)`)
)

// classRefList scans a rollbackFor/noRollbackFor body for all `X.class`
// tokens and returns the bare class names (e.g. "RuntimeException").
func classRefList(body string) []string {
	var out []string
	for _, m := range txClassRefRE.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// txParseAttributes parses the @Transactional attribute body into structured
// properties: propagation, rollback_for, no_rollback_for, read_only, isolation.
// Empty values are omitted. rollback_for / no_rollback_for are comma-joined.
func txParseAttributes(body string) map[string]any {
	props := map[string]any{}
	if body == "" {
		return props
	}
	if m := txPropagationRE.FindStringSubmatch(body); m != nil {
		props["propagation"] = m[1]
	} else if m := txJTATxTypeRE.FindStringSubmatch(body); m != nil {
		// Jakarta/JTA positional propagation: @Transactional(TxType.REQUIRES_NEW).
		props["propagation"] = m[1]
	}
	if m := txRollbackRE.FindStringSubmatch(body); m != nil {
		if refs := classRefList(m[1]); len(refs) > 0 {
			props["rollback_for"] = joinComma(refs)
		}
	}
	if m := txNoRollbackRE.FindStringSubmatch(body); m != nil {
		if refs := classRefList(m[1]); len(refs) > 0 {
			props["no_rollback_for"] = joinComma(refs)
		}
	}
	if m := txReadOnlyRE.FindStringSubmatch(body); m != nil {
		props["read_only"] = m[1]
	}
	if m := txIsolationRE.FindStringSubmatch(body); m != nil {
		props["isolation"] = m[1]
	}
	return props
}

// joinComma joins a slice of strings with ", " without importing strings
// (kept consistent with the regexp-only style of this package's siblings).
func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// methodNameStopwords are keywords that txMethodRE can spuriously capture as a
// "method name" when @Transactional sits directly on a class declaration; they
// are filtered so a class-level annotation never emits a phantom method.
var methodNameStopwords = map[string]bool{
	"class": true, "interface": true, "enum": true, "record": true,
}

// ExtractTransactional runs the @Transactional extractor.
func ExtractTransactional(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !txFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// 1. Class-level @Transactional. Record offsets so class-level boundaries
	//    are not double-counted as methods, and so method boundaries can link
	//    OWNS edges to the right declaring class.
	type txClassInfo struct {
		offset int
		body   string
	}
	classBoundaries := make(map[string]txClassInfo)
	for _, m := range txClassRE.FindAllStringSubmatchIndex(source, -1) {
		var body string
		if m[2] >= 0 {
			body = source[m[2]:m[3]]
		}
		className := source[m[4]:m[5]]
		if _, ok := classBoundaries[className]; !ok {
			classBoundaries[className] = txClassInfo{m[0], body}
		}

		props := map[string]any{
			"transaction_boundary": "class",
			"declaring_class":      className,
			"framework":            canonicalTxFramework(ctx.Framework),
		}
		for k, v := range txParseAttributes(body) {
			props[k] = v
		}
		ref := "scope:pattern:transaction_boundary:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", Subtype: "transaction_boundary",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_TRANSACTIONAL", Ref: ref,
			Properties: props,
		})
	}

	// 2. Method-level @Transactional.
	for _, m := range txMethodRE.FindAllStringSubmatchIndex(source, -1) {
		var body string
		if m[2] >= 0 {
			body = source[m[2]:m[3]]
		}
		methodName := source[m[4]:m[5]]
		if methodNameStopwords[methodName] {
			// @Transactional sat on a class declaration; handled in pass 1.
			continue
		}

		ownerClass := findEnclosingClass(source, m[0])
		name := methodName
		if ownerClass != "" {
			name = ownerClass + "." + methodName
		}

		props := map[string]any{
			"transaction_boundary": "method",
			"method":               methodName,
			"framework":            canonicalTxFramework(ctx.Framework),
		}
		if ownerClass != "" {
			props["declaring_class"] = ownerClass
		}
		for k, v := range txParseAttributes(body) {
			props[k] = v
		}

		ref := "scope:pattern:transaction_boundary:" + fp + ":" + name
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "transaction_boundary",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_TRANSACTIONAL", Ref: ref,
			Properties: props,
		}) {
			// Link the boundary to its declaring class when that class itself
			// carries a (class-level) transaction boundary entity.
			if ci, ok := classBoundaries[ownerClass]; ok {
				_ = ci
				classRef := "scope:pattern:transaction_boundary:" + fp + ":" + ownerClass
				addRel(&result, seenRels, Relationship{
					SourceRef: classRef, TargetRef: ref, RelationshipType: "OWNS",
				})
			}
		}
	}

	return result
}

// canonicalTxFramework normalises a framework alias to its canonical name for
// the entity `framework` property, matching the convention used by the
// sibling extractors.
func canonicalTxFramework(framework string) string {
	switch framework {
	case "spring_boot", "spring-boot", "springboot":
		return "spring_boot"
	case "spring_webflux", "spring-webflux", "springwebflux":
		return "spring_webflux"
	case "micronaut", "micronaut-core", "micronaut_core":
		return "micronaut"
	case "jakarta_ee", "jakarta-ee", "jakartaee", "java_ee", "javaee":
		return "jakarta_ee"
	case "jaxrs", "jax-rs", "jax_rs":
		return "jaxrs"
	default:
		return framework
	}
}
