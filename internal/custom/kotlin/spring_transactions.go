// Package kotlin — Spring @Transactional extractor for Kotlin Spring Boot.
//
// Covers the Transactions lane for lang.kotlin.framework.spring-boot (#4014,
// epic #3872, audit #3886):
//   - transaction_boundary_extraction (missing → partial)
//   - transaction_propagation         (missing → partial)
//   - transaction_rollback_rules      (missing → partial)
//   - transaction_function_stamping   (missing → partial)
//
// THE GAP (#4014). The only Kotlin extractor that previously touched
// @Transactional was custom_kotlin_ktor_transactions (ktor_di_transactions.go),
// which stamps it with framework="ktor", hardcodes propagation="REQUIRED", and
// captures NO rollback/readOnly/isolation attributes — so the
// lang.kotlin.framework.spring-boot Transactions cells got ZERO credit. The
// Java @Transactional extractor (internal/custom/java/transactional.go) relaxes
// its language gate to "kotlin", but it is only wired through the
// custom_java_patterns adapter, which hard-skips non-java files
// (patterns_dispatch.go: `if strings.ToLower(file.Language) != "java" { return }`)
// — so on .kt source it NEVER fires live. This native extractor closes that gap
// on the LIVE Kotlin dispatch path (custom_kotlin_* prefix, custom_dispatch.go).
//
// Spring Boot is the #1 Kotlin backend and @Transactional is its primary
// data-integrity boundary, so this is the highest-value Kotlin Transactions
// item.
//
// Idioms covered (identical annotation syntax to Java, Kotlin `::class` rollback
// lists):
//
//	@Transactional fun transfer() { ... }                         // boundary
//	@Transactional class AccountService { ... }                   // class boundary
//	@Transactional(readOnly = true) fun lookup() = repo.find(...) // read-only marker
//	@Transactional(propagation = Propagation.REQUIRES_NEW) ...    // propagation
//	@Transactional(rollbackFor = [IOException::class]) ...        // rollback rules
//	@Transactional(isolation = Isolation.SERIALIZABLE) ...        // isolation
//
// HONESTY BOUNDARY:
//   - A plain `fun` with no @Transactional gets NO boundary.
//   - readOnly = true methods are stamped read_only=true and NEVER get a
//     db_write effect, even when the body contains a write-shaped call.
//   - db_write is recorded only when a JPA/Spring-Data write call (save/saveAll/
//     persist/merge/delete/deleteAll/insert/update/flush) lexically appears in
//     the (non-readOnly) annotated method body — receiving a repository handle
//     is NOT a write. A read-only `repo.findById(...)` never yields db_write.
//
// Honest limit: regex-based, file-local. Cross-file propagation of a boundary
// into callees is not resolved (matches the txscope honesty rule). Hence the
// cells are flipped to partial, not full.
package kotlin

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
	extractor.Register("custom_kotlin_spring_transactions", &kotlinSpringTxExtractor{})
}

type kotlinSpringTxExtractor struct{}

func (e *kotlinSpringTxExtractor) Language() string {
	return "custom_kotlin_spring_transactions"
}

var (
	// reKtTxAny is the cheap bail-out signal: any @Transactional in the file.
	reKtTxAny = regexp.MustCompile(`@Transactional\b`)

	// reKtSpringImport is the Spring/Jakarta @Transactional import marker. It
	// distinguishes a Spring/JTA transaction boundary from an unrelated symbol,
	// keeping framework attribution honest (the Exposed DSL has no @Transactional
	// annotation, so a bare @Transactional already implies Spring/Jakarta; the
	// import check guards against a same-named user annotation).
	reKtSpringImport = regexp.MustCompile(
		`org\.springframework\.transaction\.annotation\.Transactional|` +
			`jakarta\.transaction\.Transactional|javax\.transaction\.Transactional`)

	// reKtTxClass matches @Transactional (optional attribute body, group 1) on a
	// Kotlin class declaration (group 2). Spans newlines via (?s).
	reKtTxClass = regexp.MustCompile(
		`(?s)@Transactional\b\s*(?:\(([^)]*)\))?\s*` +
			`(?:(?:open|abstract|final|internal|private|public|sealed|data)\s+)*` +
			`class\s+(\w+)`)

	// reKtTxMethod matches @Transactional (optional attribute body, group 1) on a
	// Kotlin fun declaration (group 2). Modifiers + an optional `suspend` are
	// skipped between the annotation and `fun`. Spans newlines via (?s).
	reKtTxMethod = regexp.MustCompile(
		`(?s)@Transactional\b\s*(?:\(([^)]*)\))?\s*` +
			`(?:(?:open|override|abstract|final|internal|private|protected|public|suspend|operator|inline|tailrec)\s+)*` +
			`fun\s+(\w+)\s*\(`)

	// Attribute parsers — mirror the Java extractor (transactional.go) but kept
	// self-contained in the kotlin package per the package's regexp-only style.
	reKtTxPropagation = regexp.MustCompile(`propagation\s*=\s*(?:Propagation\.)?(\w+)`)
	reKtTxTxType      = regexp.MustCompile(`TxType\.(\w+)`)
	reKtTxIsolation   = regexp.MustCompile(`isolation\s*=\s*(?:Isolation\.)?(\w+)`)
	reKtTxReadOnly    = regexp.MustCompile(`readOnly\s*=\s*(true|false)`)
	// reKtTxRollbackFor / reKtTxNoRollback capture the rollbackFor / noRollbackFor
	// list body (group 1) up to the next attribute, the closing bracket, or EOL.
	// The Kotlin form is `rollbackFor = [A::class, B::class]`.
	reKtTxRollbackFor = regexp.MustCompile(`\brollbackFor\s*=\s*\[?([^\]]*?)\]?(?:,\s*\w+\s*=|\)|$)`)
	reKtTxNoRollback  = regexp.MustCompile(`noRollbackFor\s*=\s*\[?([^\]]*?)\]?(?:,\s*\w+\s*=|\)|$)`)
	// reKtTxClassRef pulls each `Foo::class` (Kotlin) or `Foo.class` (interop)
	// token out of a rollback list body.
	reKtTxClassRef = regexp.MustCompile(`(\w+)\s*(?:::|\.)class`)

	// reKtTxWriteCall matches a JPA / Spring-Data write-shaped call in a method
	// body — the honest db_write signal. Bounded to receiver.method( forms so a
	// bare identifier never trips it.
	reKtTxWriteCall = regexp.MustCompile(
		`\.\s*(save|saveAll|saveAndFlush|persist|merge|delete|deleteAll|deleteById|insert|update|flush)\s*\(`)
)

// ktTxClassRefs returns the bare class names referenced in a rollback list body.
func ktTxClassRefs(body string) []string {
	var out []string
	for _, m := range reKtTxClassRef.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// ktTxParseAttributes parses a @Transactional attribute body into structured
// properties. Empty values are omitted; rollback lists are comma-joined.
func ktTxParseAttributes(body string) map[string]string {
	props := map[string]string{}
	if strings.TrimSpace(body) == "" {
		return props
	}
	if m := reKtTxPropagation.FindStringSubmatch(body); m != nil {
		props["propagation"] = m[1]
	} else if m := reKtTxTxType.FindStringSubmatch(body); m != nil {
		props["propagation"] = m[1]
	}
	if m := reKtTxRollbackFor.FindStringSubmatch(body); m != nil {
		if refs := ktTxClassRefs(m[1]); len(refs) > 0 {
			props["rollback_for"] = strings.Join(refs, ", ")
		}
	}
	if m := reKtTxNoRollback.FindStringSubmatch(body); m != nil {
		if refs := ktTxClassRefs(m[1]); len(refs) > 0 {
			props["no_rollback_for"] = strings.Join(refs, ", ")
		}
	}
	if m := reKtTxReadOnly.FindStringSubmatch(body); m != nil {
		props["read_only"] = m[1]
	}
	if m := reKtTxIsolation.FindStringSubmatch(body); m != nil {
		props["isolation"] = m[1]
	}
	return props
}

// ktMatchingBrace returns the index just AFTER the brace-balanced region that
// starts at the opening `{` at openIdx, or len(src) when unbalanced. Used to
// scope a method body for the db_write scan.
func ktMatchingBrace(src string, openIdx int) int {
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

// ktFindEnclosingClass returns the name of the nearest `class <Name>` whose
// declaration precedes offset, or "" when none. File-local, last-wins (the most
// recently opened class before the offset is the declaring class for the
// regex-level model we use).
var reKtClassDecl = regexp.MustCompile(`\bclass\s+(\w+)`)

func ktFindEnclosingClass(src string, offset int) string {
	last := ""
	for _, m := range reKtClassDecl.FindAllStringSubmatchIndex(src, -1) {
		if m[0] >= offset {
			break
		}
		last = src[m[2]:m[3]]
	}
	return last
}

func (e *kotlinSpringTxExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_spring_transactions.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "spring-boot"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	// Require a @Transactional AND a Spring/Jakarta import marker so we never
	// claim a same-named user annotation as a Spring boundary, and so Exposed-only
	// files (which have no @Transactional) are untouched.
	if !reKtTxAny.MatchString(src) || !reKtSpringImport.MatchString(src) {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	emit := func(name, boundary string, line int, attrs map[string]string, dbWrite bool) {
		key := "SCOPE.Pattern:kttx:" + name
		if seen[key] {
			return
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.Pattern", "transaction_boundary", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "spring-boot",
			"transaction_boundary", boundary,
			"transactional", "true",
			"provenance", "INFERRED_FROM_TRANSACTIONAL",
		)
		for k, v := range attrs {
			ent.Properties[k] = v
		}
		// db_write effect: only on a non-readOnly boundary that lexically writes.
		if dbWrite && attrs["read_only"] != "true" {
			ent.Properties["db_write"] = "true"
		}
		entities = append(entities, ent)
	}

	// 1. Class-level @Transactional. Record class offsets so a method-level scan
	//    can attribute its declaring class, and so a class-level annotation is
	//    not double-emitted as a phantom method.
	classBoundaries := map[string]bool{}
	for _, m := range reKtTxClass.FindAllStringSubmatchIndex(src, -1) {
		body := ""
		if m[2] >= 0 {
			body = src[m[2]:m[3]]
		}
		className := src[m[4]:m[5]]
		classBoundaries[className] = true
		attrs := ktTxParseAttributes(body)
		attrs["declaring_class"] = className
		emit(className, "class", lineOf(src, m[0]), attrs, false)
	}

	// 2. Method-level @Transactional.
	for _, m := range reKtTxMethod.FindAllStringSubmatchIndex(src, -1) {
		body := ""
		if m[2] >= 0 {
			body = src[m[2]:m[3]]
		}
		methodName := src[m[4]:m[5]]
		owner := ktFindEnclosingClass(src, m[0])
		name := methodName
		if owner != "" {
			name = owner + "." + methodName
		}
		attrs := ktTxParseAttributes(body)
		attrs["method"] = methodName
		if owner != "" {
			attrs["declaring_class"] = owner
		}

		// Scan the method body (from its opening brace) for a db_write call.
		dbWrite := false
		if openIdx := strings.IndexByte(src[m[1]:], '{'); openIdx >= 0 {
			abs := m[1] + openIdx
			bodySrc := src[abs:ktMatchingBrace(src, abs)]
			dbWrite = reKtTxWriteCall.MatchString(bodySrc)
		} else {
			// Expression-bodied fun (`= repo.save(x)`): scan to end of line.
			rest := src[m[1]:]
			if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
				rest = rest[:nl]
			}
			dbWrite = reKtTxWriteCall.MatchString(rest)
		}
		emit(name, "method", lineOf(src, m[0]), attrs, dbWrite)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
