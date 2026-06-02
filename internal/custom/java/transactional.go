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
// Kotlin frameworks are included because Kotlin Spring Boot / Quarkus / Micronaut
// use the identical @Transactional annotation syntax before fun/class declarations.
var txFrameworks = map[string]bool{
	"spring_boot": true, "spring-boot": true, "springboot": true,
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
	// Spring for GraphQL resolver methods (@QueryMapping/@SchemaMapping/...) carry
	// the same Spring @Transactional annotation. #3863.
	"spring_graphql": true, "spring-graphql": true, "springgraphql": true,
	"quarkus":   true,
	"micronaut": true, "micronaut-core": true, "micronaut_core": true,
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"java_ee": true, "javaee": true,
	"jaxrs": true, "jax-rs": true, "jax_rs": true,
	"microprofile": true, "micro-profile": true, "micro_profile": true,
	// Helidon MP uses JTA @Transactional (via MicroProfile / Jakarta EE).
	"helidon": true,
	// Dropwizard-Hibernate exposes Jakarta/JTA @Transactional (and the
	// dropwizard-hibernate @UnitOfWork boundary, handled below). #3863.
	"dropwizard": true,
	// Netflix DGS data-fetcher methods carry Spring @Transactional when a
	// resolver opens a DB transaction. #3863.
	"dgs":         true,
	"netflix-dgs": true, "netflix_dgs": true,
}

// txProgrammaticFrameworks gates the PROGRAMMATIC transaction-boundary /
// rollback detection (UserTransaction.begin()/commit()/rollback(),
// session.beginTransaction(), setRollbackOnly()). Unlike the annotation surface,
// programmatic JTA/Hibernate transaction APIs are available across the whole JVM
// backend ecosystem — including frameworks that do NOT use @Transactional
// (Vert.x, Akka-HTTP, Struts, Javalin, Guice). #3863 (epic #3854). The set is the
// union of txFrameworks and those non-annotation JVM frameworks.
var txProgrammaticFrameworks = func() map[string]bool {
	m := map[string]bool{
		"vertx": true, "vert.x": true, "vert_x": true, "vertx_web": true, "vertx-web": true,
		"akka-http": true, "akka_http": true, "akkahttp": true, "akka-http-java": true,
		"struts":  true,
		"javalin": true,
		"guice":   true,
		"play":    true,
	}
	for k, v := range txFrameworks {
		m[k] = v
	}
	return m
}()

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
	// txJTARollbackOnRE / txJTADontRollbackOnRE capture the Jakarta/JTA spelling
	// (`@Transactional(rollbackOn = …, dontRollbackOn = …)` from
	// jakarta.transaction.Transactional / javax.transaction.Transactional). These
	// are folded into the SAME rollback_for / no_rollback_for properties as the
	// Spring rollbackFor / noRollbackFor so the rollback-rule capability is
	// uniform across the Spring and Jakarta annotation surfaces. #3863.
	txJTARollbackOnRE     = regexp.MustCompile(`\brollbackOn\s*=\s*\{?([^}]*?)\}?(?:,\s*\w+\s*=|\)|$)`)
	txJTADontRollbackOnRE = regexp.MustCompile(`\bdontRollbackOn\s*=\s*\{?([^}]*?)\}?(?:,\s*\w+\s*=|\)|$)`)
	// txClassRefRE pulls each class token out of a rollbackFor list body. Accepts
	// both the Java `Foo.class` form and the Kotlin `Foo::class` form so Kotlin
	// @Transactional(rollbackFor = [Foo::class]) rollback rules are captured.
	txClassRefRE = regexp.MustCompile(`(\w+)\s*(?:\.|::)class`)

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
	// rollback_for accepts BOTH the Spring `rollbackFor=` and the Jakarta/JTA
	// `rollbackOn=` spelling (folded into the same property). Likewise
	// no_rollback_for accepts Spring `noRollbackFor=` and JTA `dontRollbackOn=`.
	if m := txRollbackRE.FindStringSubmatch(body); m != nil {
		if refs := classRefList(m[1]); len(refs) > 0 {
			props["rollback_for"] = joinComma(refs)
		}
	} else if m := txJTARollbackOnRE.FindStringSubmatch(body); m != nil {
		if refs := classRefList(m[1]); len(refs) > 0 {
			props["rollback_for"] = joinComma(refs)
		}
	}
	if m := txNoRollbackRE.FindStringSubmatch(body); m != nil {
		if refs := classRefList(m[1]); len(refs) > 0 {
			props["no_rollback_for"] = joinComma(refs)
		}
	} else if m := txJTADontRollbackOnRE.FindStringSubmatch(body); m != nil {
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
// Accepts both Java and Kotlin source: Kotlin uses the identical @Transactional
// annotation before fun/class declarations (same regex patterns apply).
func ExtractTransactional(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" && ctx.Language != "kotlin" {
		return result
	}
	annotationFW := txFrameworks[ctx.Framework]
	programmaticFW := txProgrammaticFrameworks[ctx.Framework]
	if !annotationFW && !programmaticFW {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// Programmatic transaction boundaries / rollback (net-new, #3863) run for the
	// wider JVM-backend set (including non-@Transactional frameworks). The
	// annotation passes below still gate on txFrameworks only.
	if programmaticFW {
		extractProgrammaticTx(ctx, source, fp, &result, seenRefs, seenRels)
	}
	if !annotationFW {
		return result
	}

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

var (
	// txProgMethodRE finds a concrete method declaration (signature followed by a
	// body-opening brace). It deliberately requires the `{` so abstract/interface
	// declarations (which have no programmatic body) are skipped. Group 1 is the
	// method name; the match end is positioned at the opening brace so the
	// brace-balanced body can be scanned from there.
	txProgMethodRE = regexp.MustCompile(
		`(?s)(?:(?:public|protected|private|static|final|abstract|synchronized|default|native)\s+)*` +
			`(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)` +
			`(\w+)\s*\([^;{]*\)\s*(?:throws\s+[\w.,\s]+?)?\{`)

	// Programmatic transaction-OPEN signals (boundary). Conservative: each is a
	// recognised JTA / Hibernate / JPA programmatic transaction-open call.
	//   userTransaction.begin()            — JTA UserTransaction.
	//   tm.begin() where a *Transaction*   — handled by the UserTransaction arm.
	//   session.beginTransaction()         — Hibernate Session.
	//   em.getTransaction().begin()        — JPA EntityTransaction.
	txProgUserTxBeginRE = regexp.MustCompile(`\b\w*[Tt]ransaction\s*\.\s*begin\s*\(\s*\)`)
	txProgHibBeginRE    = regexp.MustCompile(`\b\w+\s*\.\s*beginTransaction\s*\(\s*\)`)
	txProgEMBeginRE     = regexp.MustCompile(`\bgetTransaction\s*\(\s*\)\s*\.\s*begin\s*\(`)

	// Programmatic ROLLBACK signals.
	//   ctx.setRollbackOnly()      — JTA/EJB mark-rollback.
	//   userTransaction.rollback() — JTA explicit rollback.
	//   tx.rollback()              — Hibernate / JPA EntityTransaction rollback.
	txProgSetRollbackOnlyRE = regexp.MustCompile(`\bsetRollbackOnly\s*\(\s*\)`)
	txProgRollbackCallRE    = regexp.MustCompile(`\b\w+\s*\.\s*rollback\s*\(\s*\)`)
)

// matchingBrace returns the index just AFTER the brace-balanced region that
// starts at the opening `{` located at openIdx. Returns len(src) if unbalanced.
func matchingBrace(src string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}

// extractProgrammaticTx detects PROGRAMMATIC transaction boundaries and rollback
// rules inside method bodies — net-new for #3863 (epic #3854). Unlike the
// annotation surface this fires for the wider JVM-backend framework set
// (txProgrammaticFrameworks), since UserTransaction / Hibernate Session /
// EntityTransaction APIs are framework-agnostic.
//
// HONESTY BOUNDARY: a method is stamped only when a transaction-OPEN call
// (begin/beginTransaction) is lexically present in its own body — receiving a tx
// handle as a parameter is NOT a boundary. A method that only calls rollback()/
// setRollbackOnly() (without an open) is stamped as a rollback-only marker, not a
// boundary, to avoid crediting a boundary that opens elsewhere. Annotation-stamped
// methods are skipped (their ref is already claimed) so no double-count occurs.
func extractProgrammaticTx(ctx PatternContext, source, fp string, result *PatternResult, seenRefs map[string]bool, seenRels map[relKey]bool) {
	for _, m := range txProgMethodRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[2]:m[3]]
		if methodNameStopwords[methodName] {
			continue
		}
		openIdx := m[1] - 1 // the matched `{`
		if openIdx < 0 || openIdx >= len(source) || source[openIdx] != '{' {
			continue
		}
		body := source[openIdx:matchingBrace(source, openIdx)]

		opensTx := txProgUserTxBeginRE.MatchString(body) ||
			txProgHibBeginRE.MatchString(body) ||
			txProgEMBeginRE.MatchString(body)
		rollsBack := txProgSetRollbackOnlyRE.MatchString(body) ||
			txProgRollbackCallRE.MatchString(body)
		if !opensTx && !rollsBack {
			continue
		}

		ownerClass := findEnclosingClass(source, m[0])
		name := methodName
		if ownerClass != "" {
			name = ownerClass + "." + methodName
		}
		ref := "scope:pattern:transaction_boundary:" + fp + ":" + name
		// Do not collide with an annotation-emitted boundary for the same method.
		if seenRefs[ref] {
			continue
		}

		props := map[string]any{
			"method":    methodName,
			"framework": canonicalTxFramework(ctx.Framework),
		}
		if ownerClass != "" {
			props["declaring_class"] = ownerClass
		}
		if opensTx {
			props["transaction_boundary"] = "programmatic"
			props["tx_api"] = programmaticTxAPI(body)
		}
		if rollsBack {
			props["rollback"] = "programmatic"
		}
		addEntity(result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "transaction_boundary",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, openIdx),
			Provenance: "INFERRED_FROM_PROGRAMMATIC_TX", Ref: ref,
			Properties: props,
		})
	}
}

// programmaticTxAPI names the programmatic transaction API a body opens, for the
// tx_api property. Hibernate/EM-specific calls are checked ahead of the generic
// UserTransaction.begin() arm so the most specific label wins.
func programmaticTxAPI(body string) string {
	switch {
	case txProgHibBeginRE.MatchString(body):
		return "hibernate_session"
	case txProgEMBeginRE.MatchString(body):
		return "jpa_entity_transaction"
	case txProgUserTxBeginRE.MatchString(body):
		return "jta_user_transaction"
	default:
		return ""
	}
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
	case "spring_graphql", "spring-graphql", "springgraphql":
		return "spring_graphql"
	case "micronaut", "micronaut-core", "micronaut_core":
		return "micronaut"
	case "jakarta_ee", "jakarta-ee", "jakartaee", "java_ee", "javaee":
		return "jakarta_ee"
	case "jaxrs", "jax-rs", "jax_rs":
		return "jaxrs"
	case "microprofile", "micro-profile", "micro_profile":
		return "microprofile"
	case "helidon":
		return "helidon"
	case "dropwizard":
		return "dropwizard"
	case "dgs", "netflix-dgs", "netflix_dgs":
		return "dgs"
	case "vertx", "vert.x", "vert_x", "vertx_web", "vertx-web":
		return "vertx"
	case "akka-http", "akka_http", "akkahttp", "akka-http-java":
		return "akka_http"
	case "struts":
		return "struts"
	case "javalin":
		return "javalin"
	case "guice":
		return "guice"
	case "play":
		return "play"
	default:
		return framework
	}
}
