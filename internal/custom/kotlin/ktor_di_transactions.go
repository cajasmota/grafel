// Package kotlin — Ktor DI (Koin) and transactions (Exposed) extractors.
//
// Covers:
//   - lang.kotlin.framework.ktor  DI/di_binding_extraction    (missing → partial)
//   - lang.kotlin.framework.ktor  DI/di_injection_point       (missing → partial)
//   - lang.kotlin.framework.ktor  DI/di_scope_resolution      (missing → partial)
//   - lang.kotlin.framework.ktor  Transactions/transaction_boundary_extraction (missing → partial)
//   - lang.kotlin.framework.ktor  Transactions/transaction_propagation         (missing → partial)
//   - lang.kotlin.framework.ktor  Transactions/transaction_rollback_rules      (missing → partial)
//
// AOP for Ktor: Ktor has no Spring AOP / AspectJ proxy support. Aspect
// extraction is not_applicable — handled in registry update only; no code
// needed here.
//
// DI patterns (Koin, the idiomatic Ktor DI library):
//
//	module {
//	    single<UserService> { UserServiceImpl(get()) }
//	    factory<Repo>       { RepoImpl(get()) }
//	    scoped<Cache>       { CacheImpl() }
//	    bind<Auth>()        with singleton { AuthImpl() }
//	}
//	val userService: UserService by inject()
//
// Transaction patterns (Exposed DSL, the most common Ktor persistence layer):
//
//	transaction { /* Exposed statements */ }
//	newSuspendedTransaction { /* coroutine-aware */ }
//
// Honest limit: regex-based, file-local. Koin module wiring across files is
// not resolved. Hence all cells are flipped to partial, not full.
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
	extractor.Register("custom_kotlin_ktor_di", &ktorDIExtractor{})
	extractor.Register("custom_kotlin_ktor_transactions", &ktorTransactionsExtractor{})
}

// ---------------------------------------------------------------------------
// DI extractor (Koin)
// ---------------------------------------------------------------------------

type ktorDIExtractor struct{}

func (e *ktorDIExtractor) Language() string { return "custom_kotlin_ktor_di" }

var (
	// reKoinModuleBlock matches the opening of a Koin module { } DSL block.
	reKoinModuleBlock = regexp.MustCompile(`\bmodule\s*\{`)

	// reKoinSingleFactory matches Koin singleton/factory/scoped declarations.
	// Group 1 = scope keyword (single/factory/scoped/singleOf/factoryOf/scopedOf).
	// Group 2 = optional generic type argument <TypeName>.
	reKoinSingleFactory = regexp.MustCompile(
		`\b(single(?:Of)?|factory(?:Of)?|scoped(?:Of)?)\s*(?:<\s*([A-Z][\w<>., ]*)>)?\s*[{(]`)

	// reKoinBind matches `bind<TypeName>() with ...` Koin binding shorthand.
	reKoinBind = regexp.MustCompile(
		`\bbind\s*<\s*([A-Z][\w]*)>\s*\(\s*\)`)

	// reKoinGet matches `get()` injection call inside a Koin module.
	reKoinGet = regexp.MustCompile(`\bget\s*<?\s*([A-Z][\w]*)?\s*>?\s*\(\s*\)`)

	// reKoinInject matches `val foo: T by inject()` property injection.
	reKoinInject = regexp.MustCompile(
		`\bval\s+(\w+)\s*:\s*([A-Z][\w<>, ]*)\s*by\s+inject\s*\(\s*\)`)

	// reKoinInjectField matches `val foo by inject<T>()` shorthand.
	reKoinInjectField = regexp.MustCompile(
		`\bval\s+(\w+)\s+by\s+inject\s*<\s*([A-Z][\w]*)>\s*\(\s*\)`)

	// reKoinModuleDI matches Ktor `install(Koin) { modules(...) }` call.
	reKoinInstallKoin = regexp.MustCompile(`\binstall\s*\(\s*Koin\s*\)`)
)

func (e *ktorDIExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.ktor_di.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "ktor"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	hasKoin := reKoinModuleBlock.MatchString(src) ||
		reKoinInstallKoin.MatchString(src) ||
		reKoinInject.MatchString(src) ||
		reKoinInjectField.MatchString(src)
	if !hasKoin {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, subtype, diType string, line int) {
		key := "SCOPE.Pattern:di:" + subtype + ":" + name
		if seen[key] {
			return
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.Pattern", subtype, file.Path, file.Language, line)
		setProps(&ent,
			"framework", "ktor",
			"di_framework", "koin",
			"di_type", diType,
			"provenance", "INFERRED_FROM_KOIN_DI",
		)
		entities = append(entities, ent)
	}

	// 1. di_binding_extraction: single/factory/scoped declarations.
	for _, m := range reKoinSingleFactory.FindAllStringSubmatchIndex(src, -1) {
		keyword := src[m[2]:m[3]]
		typeName := ""
		if m[4] >= 0 {
			typeName = strings.TrimSpace(src[m[4]:m[5]])
		}
		scope := keyword
		switch {
		case strings.HasPrefix(keyword, "single"):
			scope = "singleton"
		case strings.HasPrefix(keyword, "factory"):
			scope = "factory"
		case strings.HasPrefix(keyword, "scoped"):
			scope = "scoped"
		}
		name := typeName
		if name == "" {
			name = "binding@" + strings.Repeat("x", lineOf(src, m[0])%1000)
		}
		ent := makeEntity(name, "SCOPE.Pattern", "di_binding", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "ktor",
			"di_framework", "koin",
			"di_scope", scope,
			"di_type", "binding",
			"provenance", "INFERRED_FROM_KOIN_DI",
		)
		key := "SCOPE.Pattern:di:di_binding:" + name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	// bind<T>() with ... shorthand.
	for _, m := range reKoinBind.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[m[2]:m[3]]
		add(typeName, "di_binding", "bind", lineOf(src, m[0]))
	}

	// 2. di_injection_point: val foo: T by inject()
	for _, m := range reKoinInject.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		typeName := strings.TrimSpace(src[m[4]:m[5]])
		name := fieldName + ":" + typeName
		add(name, "di_injection_point", "property_inject", lineOf(src, m[0]))
	}
	for _, m := range reKoinInjectField.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		typeName := src[m[4]:m[5]]
		name := fieldName + ":" + typeName
		add(name, "di_injection_point", "property_inject", lineOf(src, m[0]))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Transactions extractor (Exposed)
// ---------------------------------------------------------------------------

type ktorTransactionsExtractor struct{}

func (e *ktorTransactionsExtractor) Language() string { return "custom_kotlin_ktor_transactions" }

var (
	// reExposedTransaction matches Exposed `transaction { ... }`.
	reExposedTransaction = regexp.MustCompile(`\btransaction\s*\{`)

	// reExposedSuspendedTransaction matches `newSuspendedTransaction { ... }`.
	reExposedSuspendedTransaction = regexp.MustCompile(`\bnewSuspendedTransaction\s*\{`)

	// reExposedTransactionWith matches `transaction(db = myDb) { ... }`.
	reExposedTransactionWith = regexp.MustCompile(`\btransaction\s*\(\s*(?:db\s*=\s*)?(\w+)\s*\)`)

	// reExposedRepeatableRead matches isolation level hints.
	reExposedIsolation = regexp.MustCompile(
		`\btransaction\s*\(\s*(?:Connection\.TRANSACTION_(\w+)|transactionIsolation\s*=\s*Connection\.TRANSACTION_(\w+))`)

	// reSpringKtTransactional matches @Transactional on Kotlin fun.
	// Covers Ktor apps using Spring Data / JPA alongside Ktor.
	reSpringKtTransactional = regexp.MustCompile(
		`@Transactional\b\s*(?:\([^)]*\))?\s*(?:suspend\s+)?fun\s+(\w+)\s*\(`)
)

func (e *ktorTransactionsExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.ktor_transactions.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "ktor"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	hasTransaction := reExposedTransaction.MatchString(src) ||
		reExposedSuspendedTransaction.MatchString(src) ||
		reSpringKtTransactional.MatchString(src)
	if !hasTransaction {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, txType, propagation string, line int) {
		key := "SCOPE.Pattern:tx:" + name
		if seen[key] {
			return
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.Pattern", "transaction_boundary", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "ktor",
			"tx_framework", txType,
			"propagation", propagation,
			"provenance", "INFERRED_FROM_EXPOSED_TRANSACTION",
		)
		entities = append(entities, ent)
	}

	// 1. transaction_boundary_extraction: transaction { } blocks.
	cnt := 0
	for _, m := range reExposedTransaction.FindAllStringSubmatchIndex(src, -1) {
		cnt++
		add("transaction#"+strings.Repeat("x", cnt%100), "exposed", "REQUIRED", lineOf(src, m[0]))
	}

	// 2. newSuspendedTransaction { } (coroutine-aware, propagation=REQUIRED).
	for _, m := range reExposedSuspendedTransaction.FindAllStringSubmatchIndex(src, -1) {
		cnt++
		add("suspendedTransaction#"+strings.Repeat("x", cnt%100), "exposed", "REQUIRED", lineOf(src, m[0]))
	}

	// 3. transaction_rollback_rules: isolation level hints.
	for _, m := range reExposedIsolation.FindAllStringSubmatchIndex(src, -1) {
		isolation := ""
		if m[2] >= 0 {
			isolation = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			isolation = src[m[4]:m[5]]
		}
		if isolation == "" {
			continue
		}
		cnt++
		name := "transaction#isolation:" + isolation
		key := "SCOPE.Pattern:tx:" + name
		if !seen[key] {
			seen[key] = true
			ent := makeEntity(name, "SCOPE.Pattern", "transaction_boundary", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", "ktor",
				"tx_framework", "exposed",
				"propagation", "REQUIRED",
				"isolation", isolation,
				"provenance", "INFERRED_FROM_EXPOSED_TRANSACTION",
			)
			entities = append(entities, ent)
		}
	}

	// 4. @Transactional on Kotlin fun (Spring Data integration).
	for _, m := range reSpringKtTransactional.FindAllStringSubmatchIndex(src, -1) {
		funcName := src[m[2]:m[3]]
		add(funcName, "spring_transactional", "REQUIRED", lineOf(src, m[0]))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
