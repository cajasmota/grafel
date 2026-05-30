// Package kotlin — JPA/Hibernate migration extractor for Kotlin + Compose/KMP
// capability cells:
//
//   - Kotlin JPA migration_parsing (hibernate + spring-data):
//     Flyway Java-based migration classes + Liquibase ChangeSets in Kotlin.
//     Recording-win note: schema_extraction and association_extraction are
//     already handled by internal/custom/java/hibernate.go which accepts
//     ctx.Language == "kotlin" unchanged.
//
//   - framework.compose:
//     context_extraction    — CompositionLocal / LocalXxx / compositionLocalOf
//     deep_link_extraction  — navDeepLink { uriPattern = "..." }
//     native_module_imports — System.loadLibrary / JNI companion object external fun
//     branch_conditions     — if/when/?: controlling-expression extraction
//
//   - framework.kmp:
//     context_extraction    — expect/actual context providers (KMP platform context)
//     native_module_imports — cinterop import, actual fun backed by native
//     branch_conditions     — same agnostic pass as compose (recording win via this file)
//
//   - framework.compose-desktop + framework.compose-multiplatform:
//     native_module_imports — System.loadLibrary (desktop JVM) + cinterop (CMP)
//
// NA cells (honest notes in registry):
//
//	framework.kmp Navigation/deep_link_extraction    — KMP has no Compose Navigation
//	framework.compose-desktop Process/ipc_extraction — Compose Desktop has no IPC tier
//	framework.compose-desktop Process/main_renderer_split — no main/renderer split in JVM UI
//	framework.compose-multiplatform Process/ipc_extraction — CMP has no IPC
//	framework.compose-multiplatform Process/main_renderer_split — no main/renderer split
//	orm.mongodb Models/schema_extraction             — MongoDB is schemaless; no schema to extract
//
// Issue #3275 — Part of Kotlin ORM + UI residual cells.
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
	extractor.Register("custom_kotlin_jpa_migration", &kotlinJPAMigrationExtractor{})
	extractor.Register("custom_kotlin_compose_context", &composeContextExtractor{})
	extractor.Register("custom_kotlin_compose_deeplink", &composeDeepLinkExtractor{})
	extractor.Register("custom_kotlin_native_imports", &kotlinNativeImportsExtractor{})
	extractor.Register("custom_kotlin_branch_conditions", &kotlinBranchCondExtractor{})
}

// ===========================================================================
// kotlinJPAMigrationExtractor
// Covers:
//   lang.kotlin.orm.hibernate  Migrations/migration_parsing
//   lang.kotlin.orm.spring-data Migrations/migration_parsing
// ===========================================================================

// kotlinJPAMigrationExtractor emits migration entities from:
//  1. Flyway Java/Kotlin-based migrations:
//     class V2__Add_orders_table : BaseJavaMigration() { ... }
//     class V3__Add_index : SpringJdbcMigration { ... }
//  2. Flyway SQL migration filenames: V1__init.sql, V2_1__patch.sql
//  3. Liquibase ChangeSets in Kotlin: @ChangeSet(order="001", id="createUsers", author="dev")
//  4. @SpringBootTest with embedded db creation patterns (secondary indicator).
type kotlinJPAMigrationExtractor struct{}

func (e *kotlinJPAMigrationExtractor) Language() string { return "custom_kotlin_jpa_migration" }

var (
	// reFlywayClass matches Flyway versioned migration class declarations:
	//   class V2__Add_orders_table : BaseJavaMigration()
	//   class V3__patch_users : JdbcMigration
	reFlywayClass = regexp.MustCompile(
		`(?m)class\s+(V\d+(?:_\d+)*__[A-Za-z0-9_]+)\s*(?::\s*[A-Za-z][A-Za-z0-9_]*)?`)

	// reFlywayRepeatableClass matches repeatable migration R__description class
	reFlywayRepeatableClass = regexp.MustCompile(
		`(?m)class\s+(R__[A-Za-z0-9_]+)\s*(?::\s*[A-Za-z][A-Za-z0-9_]*)?`)

	// reFlywayBaseMigration matches when a class extends BaseJavaMigration / SpringJdbcMigration
	reFlywayBaseMigration = regexp.MustCompile(
		`(?m):\s*(BaseJavaMigration|SpringJdbcMigration|JdbcMigration|BaselineOnMigrate)\s*(?:\(\s*\))?`)

	// reLiquibaseChangeSet matches Liquibase @ChangeSet annotation
	// Captures: (id value, author value)
	reLiquibaseChangeSet = regexp.MustCompile(
		`@ChangeSet\s*\([^)]*id\s*=\s*"([^"]+)"[^)]*author\s*=\s*"([^"]+)"[^)]*\)`)

	// reLiquibaseChangeSetAlt matches alternate param order (author before id)
	reLiquibaseChangeSetAlt = regexp.MustCompile(
		`@ChangeSet\s*\([^)]*author\s*=\s*"([^"]+)"[^)]*id\s*=\s*"([^"]+)"[^)]*\)`)

	// reFlywayMigrateFn matches flyway.migrate() calls (programmatic migration trigger)
	reFlywayMigrateFn = regexp.MustCompile(
		`(?m)\bflyway\s*(?:\.\s*\w+\s*)*\.\s*migrate\s*\(`)

	// reFlywayConfig matches Flyway configuration in Kotlin DSL / Spring config
	reFlywayConfig = regexp.MustCompile(
		`(?m)\b(?:Flyway|FlywayMigrationStrategy|FlywayConfigurationCustomizer)\b`)

	// reLiquibaseConfig matches Liquibase beans / imports
	reLiquibaseConfig = regexp.MustCompile(
		`(?m)\b(?:SpringLiquibase|LiquibaseMigrationExecutor|liquibase\.Liquibase)\b`)
)

func (e *kotlinJPAMigrationExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_jpa_migration.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("extractor", "jpa_migration"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	// Gate: must contain migration-related tokens.
	if !strings.Contains(src, "Migration") && !strings.Contains(src, "ChangeSet") &&
		!strings.Contains(src, "flyway") && !strings.Contains(src, "Flyway") &&
		!strings.Contains(src, "Liquibase") && !strings.Contains(src, "liquibase") {
		return nil, nil
	}

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

	// 1. Flyway versioned migration classes: V2__Add_orders_table
	for _, m := range reFlywayClass.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(className, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "hibernate",
			"migration_kind", "flyway_versioned",
			"provenance", "INFERRED_FROM_FLYWAY_MIGRATION_CLASS",
		)
		add(ent)
	}

	// 2. Flyway repeatable migration classes: R__description
	for _, m := range reFlywayRepeatableClass.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(className, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "hibernate",
			"migration_kind", "flyway_repeatable",
			"provenance", "INFERRED_FROM_FLYWAY_REPEATABLE_CLASS",
		)
		add(ent)
	}

	// 3. BaseJavaMigration / SpringJdbcMigration extends → migration file
	for _, m := range reFlywayBaseMigration.FindAllStringSubmatchIndex(src, -1) {
		baseClass := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		name := "flyway_migration:" + file.Path
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "hibernate",
			"migration_kind", "flyway_base",
			"base_class", baseClass,
			"provenance", "INFERRED_FROM_FLYWAY_BASE_MIGRATION",
		)
		add(ent)
	}

	// 4. Liquibase @ChangeSet (id before author)
	for _, m := range reLiquibaseChangeSet.FindAllStringSubmatchIndex(src, -1) {
		id := src[m[2]:m[3]]
		author := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := "changeset:" + id + "@" + author
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "hibernate",
			"migration_kind", "liquibase_changeset",
			"changeset_id", id,
			"changeset_author", author,
			"provenance", "INFERRED_FROM_LIQUIBASE_CHANGESET",
		)
		add(ent)
	}

	// 5. Liquibase @ChangeSet (author before id)
	for _, m := range reLiquibaseChangeSetAlt.FindAllStringSubmatchIndex(src, -1) {
		author := src[m[2]:m[3]]
		id := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := "changeset:" + id + "@" + author
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "hibernate",
			"migration_kind", "liquibase_changeset",
			"changeset_id", id,
			"changeset_author", author,
			"provenance", "INFERRED_FROM_LIQUIBASE_CHANGESET_ALT",
		)
		add(ent)
	}

	// 6. flyway.migrate() — programmatic trigger
	for _, m := range reFlywayMigrateFn.FindAllStringSubmatchIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "flyway.migrate:" + file.Path
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "hibernate",
			"migration_kind", "flyway_programmatic",
			"provenance", "INFERRED_FROM_FLYWAY_MIGRATE_CALL",
		)
		add(ent)
	}

	// 7. Flyway/Liquibase config beans (secondary migration_parsing signal)
	if reFlywayConfig.MatchString(src) {
		line := lineOf(src, reFlywayConfig.FindStringIndex(src)[0])
		name := "flyway_config:" + file.Path
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "hibernate",
			"migration_kind", "flyway_config",
			"provenance", "INFERRED_FROM_FLYWAY_CONFIG",
		)
		add(ent)
	}

	if reLiquibaseConfig.MatchString(src) {
		line := lineOf(src, reLiquibaseConfig.FindStringIndex(src)[0])
		name := "liquibase_config:" + file.Path
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "hibernate",
			"migration_kind", "liquibase_config",
			"provenance", "INFERRED_FROM_LIQUIBASE_CONFIG",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ===========================================================================
// composeContextExtractor
// Covers:
//   lang.kotlin.framework.compose  Structure/context_extraction
//   lang.kotlin.framework.kmp      Structure/context_extraction
// ===========================================================================

// composeContextExtractor emits entities from CompositionLocal / KMP context
// provider patterns.
//
// Compose patterns:
//
//	val LocalSomething = compositionLocalOf { DefaultValue() }
//	val LocalSomething = staticCompositionLocalOf { DefaultValue() }
//	CompositionLocalProvider(LocalTheme provides theme) { ... }
//	val theme = LocalSomething.current
//
// KMP context patterns:
//
//	expect class AppContext
//	actual class AppContext(val context: android.content.Context)
//	expect fun platformContext(): PlatformContext
type composeContextExtractor struct{}

func (e *composeContextExtractor) Language() string { return "custom_kotlin_compose_context" }

var (
	// reCompositionLocalDef matches CompositionLocal definition:
	//   val LocalTheme = compositionLocalOf { ... }
	// Captures: (local_name, factory_fn)
	reCompositionLocalDef = regexp.MustCompile(
		`(?m)val\s+(Local[A-Z][A-Za-z0-9_]*)\s*=\s*(compositionLocalOf|staticCompositionLocalOf|compositionLocalWithComputedDefault)\s*(?:<[^>]*>)?\s*\{`)

	// reCompositionLocalProvider matches CompositionLocalProvider usages:
	//   CompositionLocalProvider(LocalX provides value) { ... }
	// Captures: (local_name)
	reCompositionLocalProvider = regexp.MustCompile(
		`CompositionLocalProvider\s*\(\s*(Local[A-Z][A-Za-z0-9_]*)`)

	// reCompositionLocalCurrent matches .current access:
	//   val x = LocalSomething.current
	// Captures: (local_name)
	reCompositionLocalCurrent = regexp.MustCompile(
		`(Local[A-Z][A-Za-z0-9_]*)\.current`)

	// reKmpContextClass matches KMP expect/actual context class patterns:
	//   expect class AppContext
	//   actual class AppContext(val context: ...)
	// Captures: (class_name)
	reKmpContextClass = regexp.MustCompile(
		`(?m)(?:expect|actual)\s+class\s+((?:[A-Z][A-Za-z0-9_]*)?Context[A-Za-z0-9_]*)`)

	// reKmpContextFun matches KMP context factory functions:
	//   expect fun platformContext(): PlatformContext
	//   actual fun platformContext(): PlatformContext = ...
	// Captures: (fun_name)
	reKmpContextFun = regexp.MustCompile(
		`(?m)(?:expect|actual)\s+fun\s+([a-z][A-Za-z0-9_]*[Cc]ontext[A-Za-z0-9_]*)\s*\(`)

	// reAndroidContextParam matches Android Context parameter in KMP actual:
	//   actual class AppContext(val context: android.content.Context)
	reAndroidContextParam = regexp.MustCompile(
		`android\.content\.Context|android\.app\.Activity|android\.app\.Application`)
)

func (e *composeContextExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_compose_context.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("extractor", "compose_context"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	if !strings.Contains(src, "compositionLocalOf") &&
		!strings.Contains(src, "CompositionLocalProvider") &&
		!strings.Contains(src, "Local") &&
		!strings.Contains(src, "Context") &&
		!strings.Contains(src, "expect") && !strings.Contains(src, "actual") {
		return nil, nil
	}

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

	// CompositionLocal definitions.
	for _, m := range reCompositionLocalDef.FindAllStringSubmatchIndex(src, -1) {
		localName := src[m[2]:m[3]]
		factoryFn := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		ent := makeEntity(localName, "SCOPE.Pattern", "context_provider", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"context_kind", factoryFn,
			"provenance", "INFERRED_FROM_COMPOSITION_LOCAL_DEF",
		)
		add(ent)
	}

	// CompositionLocalProvider usage.
	for _, m := range reCompositionLocalProvider.FindAllStringSubmatchIndex(src, -1) {
		localName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		name := "provide:" + localName
		ent := makeEntity(name, "SCOPE.Pattern", "context_provider", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"context_kind", "CompositionLocalProvider",
			"local_name", localName,
			"provenance", "INFERRED_FROM_COMPOSITION_LOCAL_PROVIDER",
		)
		add(ent)
	}

	// LocalXxx.current reads.
	for _, m := range reCompositionLocalCurrent.FindAllStringSubmatchIndex(src, -1) {
		localName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		name := "consume:" + localName
		ent := makeEntity(name, "SCOPE.Pattern", "context_consumer", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"context_kind", "current_access",
			"local_name", localName,
			"provenance", "INFERRED_FROM_COMPOSITION_LOCAL_CURRENT",
		)
		add(ent)
	}

	// KMP expect/actual context classes.
	for _, m := range reKmpContextClass.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		kind := "expect_context"
		if strings.Contains(src[m[0]:m[0]+6], "actual") {
			kind = "actual_context"
		}
		ent := makeEntity(className, "SCOPE.Pattern", "context_provider", file.Path, "kotlin", line)
		props := []string{
			"framework", "kmp",
			"context_kind", kind,
			"provenance", "INFERRED_FROM_KMP_CONTEXT_CLASS",
		}
		if reAndroidContextParam.MatchString(src) {
			props = append(props, "platform", "android")
		}
		setProps(&ent, props...)
		add(ent)
	}

	// KMP context factory functions.
	for _, m := range reKmpContextFun.FindAllStringSubmatchIndex(src, -1) {
		funName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(funName, "SCOPE.Pattern", "context_provider", file.Path, "kotlin", line)
		setProps(&ent, "framework", "kmp",
			"context_kind", "context_factory",
			"provenance", "INFERRED_FROM_KMP_CONTEXT_FUN",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ===========================================================================
// composeDeepLinkExtractor
// Covers:
//   lang.kotlin.framework.compose Navigation/deep_link_extraction
// ===========================================================================

// composeDeepLinkExtractor emits entities from Compose Navigation deep link
// declarations.
//
// Patterns:
//
//	composable(
//	    route = "profile/{id}",
//	    deepLinks = listOf(navDeepLink { uriPattern = "app://profile/{id}" })
//	) { ... }
//
//	val deepLink = navDeepLink {
//	    uriPattern = "https://example.com/users/{userId}"
//	    action = Intent.ACTION_VIEW
//	}
type composeDeepLinkExtractor struct{}

func (e *composeDeepLinkExtractor) Language() string { return "custom_kotlin_compose_deeplink" }

var (
	// reNavDeepLink matches navDeepLink block with uriPattern.
	// Captures: (uri_pattern)
	reNavDeepLinkURI = regexp.MustCompile(
		`navDeepLink\s*\{[^}]*uriPattern\s*=\s*"([^"]+)"[^}]*\}`)

	// reNavDeepLinkAction matches navDeepLink with action only (no URI).
	// Captures: (intent_action)
	reNavDeepLinkAction = regexp.MustCompile(
		`navDeepLink\s*\{[^}]*action\s*=\s*"([^"]+)"[^}]*\}`)

	// reDeepLinksParam matches the deepLinks = listOf(...) argument.
	// Used to detect composables with deep links (outer).
	reDeepLinksParam = regexp.MustCompile(
		`deepLinks\s*=\s*listOf\s*\(`)

	// reAndroidManifestDeepLink matches <intent-filter> with data scheme in AndroidManifest.xml
	// emitting entities from Kotlin companion objects or data class URIs.
	reIntentFilterDeepLink = regexp.MustCompile(
		`android:scheme\s*=\s*"([^"]+)"[^>]*>`)

	// reUriPattern matches standalone uriPattern = "..." outside navDeepLink block.
	reUriPatternStandalone = regexp.MustCompile(
		`uriPattern\s*=\s*"([^"]+)"`)
)

func (e *composeDeepLinkExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_compose_deeplink.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("extractor", "compose_deeplink"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	if !strings.Contains(src, "navDeepLink") && !strings.Contains(src, "uriPattern") &&
		!strings.Contains(src, "deepLinks") {
		return nil, nil
	}

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

	// navDeepLink with URI pattern.
	for _, m := range reNavDeepLinkURI.FindAllStringSubmatchIndex(src, -1) {
		uriPattern := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(uriPattern, "SCOPE.Operation", "deep_link", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"link_kind", "nav_deep_link_uri",
			"uri_pattern", uriPattern,
			"provenance", "INFERRED_FROM_NAV_DEEP_LINK_URI",
		)
		add(ent)
	}

	// navDeepLink with action only.
	for _, m := range reNavDeepLinkAction.FindAllStringSubmatchIndex(src, -1) {
		action := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		name := "deeplink_action:" + action
		ent := makeEntity(name, "SCOPE.Operation", "deep_link", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"link_kind", "nav_deep_link_action",
			"intent_action", action,
			"provenance", "INFERRED_FROM_NAV_DEEP_LINK_ACTION",
		)
		add(ent)
	}

	// Standalone uriPattern (fallback for split declarations).
	for _, m := range reUriPatternStandalone.FindAllStringSubmatchIndex(src, -1) {
		uri := src[m[2]:m[3]]
		key := "SCOPE.Operation:deep_link:" + uri
		if seen[key] {
			continue // already captured via reNavDeepLinkURI
		}
		line := lineOf(src, m[0])
		ent := makeEntity(uri, "SCOPE.Operation", "deep_link", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"link_kind", "uri_pattern_standalone",
			"uri_pattern", uri,
			"provenance", "INFERRED_FROM_URI_PATTERN_STANDALONE",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ===========================================================================
// kotlinNativeImportsExtractor
// Covers:
//   lang.kotlin.framework.compose          Native Bridge/native_module_imports
//   lang.kotlin.framework.kmp              Native Bridge/native_module_imports
//   lang.kotlin.framework.compose-desktop  Native/native_module_imports
//   lang.kotlin.framework.compose-multiplatform Native/native_module_imports
// ===========================================================================

// kotlinNativeImportsExtractor emits entities from native bridge declarations
// across Compose/KMP/Desktop/Multiplatform:
//
// JNI / Android (Compose + Desktop):
//
//	companion object { init { System.loadLibrary("native-lib") } }
//	external fun nativeMethod(x: Int): String
//
// KMP cinterop (KMP + Compose-Multiplatform):
//
//	import platform.Foundation.NSString
//	import cnames.structs.*
//	actual fun nativeFn() = nativeImpl()
//	@CName("nativeFn") fun exposed() = ...
//
// Desktop JVM native (Compose-Desktop):
//
//	System.loadLibrary("swt")
//	Runtime.getRuntime().load("/path/to/lib.so")
type kotlinNativeImportsExtractor struct{}

func (e *kotlinNativeImportsExtractor) Language() string { return "custom_kotlin_native_imports" }

var (
	// reSystemLoadLibrary matches System.loadLibrary("lib-name").
	// Captures: (library_name)
	reSystemLoadLibrary = regexp.MustCompile(
		`System\.loadLibrary\s*\(\s*"([^"]+)"\s*\)`)

	// reRuntimeLoad matches Runtime.getRuntime().load("path").
	// Captures: (library_path)
	reRuntimeLoad = regexp.MustCompile(
		`Runtime\.getRuntime\s*\(\s*\)\.load\s*\(\s*"([^"]+)"\s*\)`)

	// reExternalFun matches JNI external function declarations.
	// Captures: (function_name)
	reExternalFun = regexp.MustCompile(
		`(?m)external\s+fun\s+([a-z][A-Za-z0-9_]*)\s*\(`)

	// reCInteropImport matches KMP cinterop platform imports.
	// Captures: (platform_module)
	reCInteropImport = regexp.MustCompile(
		`(?m)^import\s+(platform\.[A-Za-z][A-Za-z0-9_.]*|cnames\.(?:structs|enums)\.[A-Za-z][A-Za-z0-9_.]*)`)

	// reKmpActualNative matches KMP actual functions that delegate to native:
	//   actual fun nativeFn(): Type = nativeCall()
	// where the body is a single call (not a Kotlin/JVM body).
	reKmpActualNativeCall = regexp.MustCompile(
		`(?m)actual\s+fun\s+([a-z][A-Za-z0-9_]*)\s*\([^)]*\)\s*(?::\s*[A-Za-z][A-Za-z0-9_<>?]*\s*)?=\s*([a-z][A-Za-z0-9_]*)\s*\(`)

	// reCNameAnnotation matches @CName exported symbols (Kotlin/Native C interop).
	// Captures: (c_name)
	reCNameAnnotation = regexp.MustCompile(
		`@CName\s*\(\s*"([^"]+)"\s*\)`)

	// reJniOnLoad matches JNI_OnLoad pattern.
	reJniOnLoad = regexp.MustCompile(`\bJNI_OnLoad\b`)

	// reNativeLibCompanion matches the companion object + init { System.loadLibrary } pattern.
	reNativeLibCompanion = regexp.MustCompile(
		`(?s)companion\s+object\s*\{[^}]*System\.loadLibrary\s*\(`)
)

func (e *kotlinNativeImportsExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_native_imports.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("extractor", "native_imports"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	if !strings.Contains(src, "loadLibrary") && !strings.Contains(src, "external fun") &&
		!strings.Contains(src, "platform.") && !strings.Contains(src, "cnames.") &&
		!strings.Contains(src, "@CName") && !strings.Contains(src, "actual fun") &&
		!strings.Contains(src, "JNI_OnLoad") {
		return nil, nil
	}

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

	// 1. System.loadLibrary("lib")
	for _, m := range reSystemLoadLibrary.FindAllStringSubmatchIndex(src, -1) {
		libName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(libName, "SCOPE.Component", "native_library", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"import_kind", "jni_load_library",
			"library_name", libName,
			"provenance", "INFERRED_FROM_SYSTEM_LOAD_LIBRARY",
		)
		add(ent)
	}

	// 2. Runtime.getRuntime().load("path")
	for _, m := range reRuntimeLoad.FindAllStringSubmatchIndex(src, -1) {
		libPath := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(libPath, "SCOPE.Component", "native_library", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"import_kind", "runtime_load",
			"library_path", libPath,
			"provenance", "INFERRED_FROM_RUNTIME_LOAD",
		)
		add(ent)
	}

	// 3. external fun declarations
	for _, m := range reExternalFun.FindAllStringSubmatchIndex(src, -1) {
		funName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(funName, "SCOPE.Operation", "native_function", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"import_kind", "jni_external_fun",
			"provenance", "INFERRED_FROM_EXTERNAL_FUN",
		)
		add(ent)
	}

	// 4. cinterop platform imports
	for _, m := range reCInteropImport.FindAllStringSubmatchIndex(src, -1) {
		platformModule := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(platformModule, "SCOPE.Component", "native_import", file.Path, "kotlin", line)
		setProps(&ent, "framework", "kmp",
			"import_kind", "cinterop_platform",
			"platform_module", platformModule,
			"provenance", "INFERRED_FROM_CINTEROP_IMPORT",
		)
		add(ent)
	}

	// 5. KMP actual functions backed by native calls
	for _, m := range reKmpActualNativeCall.FindAllStringSubmatchIndex(src, -1) {
		funName := src[m[2]:m[3]]
		nativeCall := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := funName + " -> " + nativeCall
		ent := makeEntity(name, "SCOPE.Operation", "native_function", file.Path, "kotlin", line)
		setProps(&ent, "framework", "kmp",
			"import_kind", "actual_native_delegate",
			"function_name", funName,
			"native_call", nativeCall,
			"provenance", "INFERRED_FROM_KMP_ACTUAL_NATIVE",
		)
		add(ent)
	}

	// 6. @CName annotations (Kotlin/Native C-exported symbols)
	for _, m := range reCNameAnnotation.FindAllStringSubmatchIndex(src, -1) {
		cName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(cName, "SCOPE.Operation", "native_export", file.Path, "kotlin", line)
		setProps(&ent, "framework", "kmp",
			"import_kind", "cname_export",
			"c_name", cName,
			"provenance", "INFERRED_FROM_CNAME_ANNOTATION",
		)
		add(ent)
	}

	// 7. JNI_OnLoad marker
	if reJniOnLoad.MatchString(src) {
		line := lineOf(src, reJniOnLoad.FindStringIndex(src)[0])
		ent := makeEntity("JNI_OnLoad", "SCOPE.Operation", "native_function", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"import_kind", "jni_on_load",
			"provenance", "INFERRED_FROM_JNI_ONLOAD",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ===========================================================================
// kotlinBranchCondExtractor
// Covers:
//   lang.kotlin.framework.compose Data Flow/branch_conditions
//   lang.kotlin.framework.kmp     Data Flow/branch_conditions
// ===========================================================================

// kotlinBranchCondExtractor emits SCOPE.Pattern "branch_condition" entities
// from Kotlin if/when/?: controlling expressions.
//
// Patterns:
//
//	if (user.isLoggedIn) { ... }
//	if (count > 0 && items.isNotEmpty()) { ... }
//	val label = when (state) { is Loading -> ... }
//	val x = if (flag) valueA else valueB
//	items.filter { it.isVisible && it.count > threshold }
type kotlinBranchCondExtractor struct{}

func (e *kotlinBranchCondExtractor) Language() string { return "custom_kotlin_branch_conditions" }

var (
	// reKotlinIfCondition matches if (...) controlling expression.
	// Captures: (condition_expr)
	reKotlinIfCondition = regexp.MustCompile(
		`\bif\s*\(([^)]{3,120})\)\s*(?:\{|[a-zA-Z])`)

	// reKotlinWhenSubject matches when(subject) { ... } branches with is/comparison.
	// We match when-branch lines: "is X ->" or "subject == val ->"
	// Captures: (subject_expr)
	reKotlinWhenSubject = regexp.MustCompile(
		`\bwhen\s*\(([^)]{1,80})\)\s*\{`)

	// reKotlinWhenBranch matches any when block branch arrow (-> on its own line).
	// Covers: "is X ->", "X.Y ->", "val == X ->", etc.
	reKotlinWhenBranch = regexp.MustCompile(`(?m)[^\n]+\s*->\s*`)

	// reKotlinTernaryInline matches inline if:
	//   val x = if (cond) a else b
	// Captures: (condition)
	reKotlinTernaryInline = regexp.MustCompile(
		`=\s*if\s*\(([^)]{3,80})\)\s*(?:\w|")`)

	// reKotlinFilterLambda matches filter/takeIf/takeUnless lambdas with conditions:
	//   .filter { it.active && it.score > 0 }
	// Captures: (lambda_body)
	reKotlinFilterLambda = regexp.MustCompile(
		`\.(?:filter|takeIf|takeUnless|filterNot|first|firstOrNull|any|all|none)\s*\{\s*([^}]{3,100})\s*\}`)

	// reComparisonOp detects a real comparison (not a bare boolean flag).
	reComparisonOp = regexp.MustCompile(`[!=<>]=?|&&|\|\||\bis\b|\bis not\b|\bin\b|\!in\b`)
)

// normalizeCond trims whitespace from a condition expression.
func normalizeCond(s string) string {
	s = strings.TrimSpace(s)
	// Collapse internal runs of whitespace.
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

func (e *kotlinBranchCondExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_branch_conditions.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("extractor", "branch_conditions"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	if !strings.Contains(src, "if ") && !strings.Contains(src, "if(") &&
		!strings.Contains(src, "when(") && !strings.Contains(src, "when (") &&
		!strings.Contains(src, ".filter {") && !strings.Contains(src, ".takeIf {") {
		return nil, nil
	}

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

	emit := func(cond, kind string, offset int) {
		norm := normalizeCond(cond)
		if len(norm) < 3 {
			return
		}
		// Only emit conditions that contain a meaningful operator.
		if !reComparisonOp.MatchString(norm) {
			return
		}
		// Truncate very long conditions.
		if len(norm) > 100 {
			norm = norm[:97] + "..."
		}
		line := lineOf(src, offset)
		name := "branch:" + kind + ":" + norm
		ent := makeEntity(name, "SCOPE.Pattern", "branch_condition", file.Path, "kotlin", line)
		setProps(&ent, "framework", "compose",
			"branch_kind", kind,
			"condition", norm,
			"provenance", "INFERRED_FROM_KOTLIN_"+strings.ToUpper(kind),
		)
		add(ent)
	}

	// 1. if (...) conditions
	for _, m := range reKotlinIfCondition.FindAllStringSubmatchIndex(src, -1) {
		emit(src[m[2]:m[3]], "if", m[0])
	}

	// 2. when(subject) { ... } — emit when the block has is/== branches.
	// The subject itself may be a plain identifier; the comparison lives in branches.
	for _, m := range reKotlinWhenSubject.FindAllStringSubmatchIndex(src, -1) {
		subject := normalizeCond(src[m[2]:m[3]])
		if len(subject) == 0 {
			continue
		}
		// Look ahead for a when-branch (is X -> or val == X ->) within the next 500 bytes.
		end := m[1] + 500
		if end > len(src) {
			end = len(src)
		}
		block := src[m[1]:end]
		if reKotlinWhenBranch.MatchString(block) || reComparisonOp.MatchString(subject) {
			line := lineOf(src, m[0])
			name := "branch:when:" + subject
			ent := makeEntity(name, "SCOPE.Pattern", "branch_condition", file.Path, "kotlin", line)
			setProps(&ent, "framework", "compose",
				"branch_kind", "when",
				"condition", subject,
				"provenance", "INFERRED_FROM_KOTLIN_WHEN",
			)
			add(ent)
		}
	}

	// 3. inline if (condition) ... else ...
	for _, m := range reKotlinTernaryInline.FindAllStringSubmatchIndex(src, -1) {
		emit(src[m[2]:m[3]], "ternary", m[0])
	}

	// 4. filter{} / takeIf{} lambdas
	for _, m := range reKotlinFilterLambda.FindAllStringSubmatchIndex(src, -1) {
		emit(src[m[2]:m[3]], "filter_lambda", m[0])
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
