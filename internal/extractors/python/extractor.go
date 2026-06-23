// Package python implements the tree-sitter–based extractor for Python source files.
//
// Extracted entities (maps to Python indexer scope_mapping.py):
//   - function_definition  → Kind="SCOPE.Operation", Subtype="function"
//   - decorated_definition wrapping function_definition → same kind (decorators are
//     not emitted by the base extractor; framework extractors handle those separately)
//   - class_definition     → Kind="SCOPE.Component", Subtype="class"
//   - methods in class     → Kind="SCOPE.Operation", Subtype="method"
//   - import_statement / import_from_statement → Kind="SCOPE.Component" (module)
//
// Embedded relationships (PORT-2-FIX-2 / issue #25):
//   - CONTAINS:  class    → method   (one per method declared inside a class body)
//   - CALLS:     function → callee   (bare-name target, resolver rewrites cross-file)
//   - IMPORTS:   file     → module   (one per import path)
//
// QualifiedName is set to the module-path-qualified name for function, method,
// and class entities (issue #1413). The module path is derived from the file
// path using filePathToModule — e.g. "app/orders/handlers.py" → "orders.handlers",
// so a function "createOrder" gets QualifiedName "orders.handlers.createOrder".
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package python

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

// emitMigrationEntitiesEnv controls whether Django migration files emit
// Migration entities. Defaults to false (migrations are pruned). Set to "1"
// or "true" to include migration entities in the output. This follows the
// conservative-prune policy pattern (e.g., GRAFEL_EMIT_DESTRUCTURE_DETAIL).
const emitMigrationEntitiesEnv = "GRAFEL_EMIT_MIGRATION_ENTITIES"

func init() {
	extractor.Register("python", &Extractor{})
}

// filePathToModule converts a repo-relative Python file path to its
// dotted module path. Mirrors modulesForPythonFile in internal/resolve
// but is intentionally self-contained to avoid an import cycle.
//
// Examples:
//
//	"app/orders/handlers.py"        → "orders.handlers"  (app/ prefix stripped)
//	"users/__init__.py"             → "users"
//	"src/app/models.py"             → "app.models"        (src/ prefix stripped)
//	"manage.py"                     → "manage"
func filePathToModule(filePath string) string {
	// Strip .py suffix.
	s := strings.TrimSuffix(filePath, ".py")
	// __init__ rolls up to its parent directory.
	if strings.HasSuffix(s, "/__init__") {
		s = strings.TrimSuffix(s, "/__init__")
	}
	// Strip well-known source-root prefixes (mirrors sourceRootPrefixes in resolve).
	for _, prefix := range []string{"src/", "lib/", "app/"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	return strings.ReplaceAll(s, "/", ".")
}

// Extractor implements extractors.Extractor for Python.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "python" }

// Extract walks the tree-sitter CST and returns entity records for the Python file.
//
// OTel span "extractor.python" is emitted with attributes: file, entity_count,
// function_count, class_count.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.python")
	ctx, span := tracer.Start(ctx, "extractor.python")
	defer span.End()

	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		span.SetAttributes(
			attribute.Int("entity_count", 0),
			attribute.Int("function_count", 0),
			attribute.Int("class_count", 0),
		)
		return nil, nil
	}

	// If a pre-parsed tree was supplied by the pipeline, reuse it. Otherwise
	// parse inline via the language's grammar adapter — this path is for direct
	// test callers that hand the extractor raw content with no tree (B2 #5418).
	tree := file.TSTree
	if tree == nil {
		parser, parseErr := pythonAdapter.NewParser(pythonGrammar())
		if parseErr != nil {
			return nil, fmt.Errorf("python extractor: parser init failed: %w", parseErr)
		}
		defer parser.Close()
		tree, parseErr = parser.Parse(file.Content)
		if parseErr != nil {
			return nil, fmt.Errorf("python extractor: parse failed: %w", parseErr)
		}
		if tree == nil {
			return nil, fmt.Errorf("python extractor: parse produced nil tree")
		}
		defer tree.Close()
	}

	root := tree.RootNode()

	var (
		entities      []types.EntityRecord
		functionCount int
		classCount    int
	)

	// Issue #577 — emit a file-level SCOPE.Component (subtype="file")
	// entity per source file so the cross-repo import linker (#566)
	// can map IMPORTS edges back to the originating repo via the
	// resolver's byName index. Generalises the JS/TS fix from #570/#575.
	entities = append(entities, extractor.FileEntity(file))

	// #1617 / #2283 — collapse auto-generated Django migration files to exactly
	// ONE semantic entity per file (kind="Migration") plus the file-level
	// SCOPE.Component retained for import resolution.
	//
	// Before #1617 the full AST walk emitted one entity per operation
	// (AddField, RemoveField, AlterField, …) plus the Migration class itself —
	// producing ~2.3× entity inflation on the UpVate corpus (100 entities for
	// 43 files). #1617 pruned to file-only. #2283 restores exactly one
	// Migration entity per file with per-operation metadata encoded as
	// properties (operations JSON array, op_count, dependencies) rather than
	// as separate graph nodes.
	//
	// Issue #2548: Django migration files are now pruned by default (zero
	// domain logic, pure ORM scaffolding). Opt-in via GRAFEL_EMIT_MIGRATION_ENTITIES=1
	// for backward compatibility or when migration analysis is needed.
	//
	// Issue #2587: Ensure the file-level check actually skips semantic entity
	// extraction. Return early with either just the file entity (default) or
	// file + one Migration entity (opt-in).
	if isDjangoMigrationFile(file.Path) {
		emitMigrations := os.Getenv(emitMigrationEntitiesEnv) == "1" || os.Getenv(emitMigrationEntitiesEnv) == "true"
		if emitMigrations {
			migEnt := extractMigrationEntity(djangoMigrationFile{
				path:     file.Path,
				language: file.Language,
				source:   string(file.Content),
			})
			entities = append(entities, migEnt)
			span.SetAttributes(
				attribute.Int("entity_count", len(entities)),
				attribute.Bool("django_migration_emitted", true),
			)
		} else {
			span.SetAttributes(
				attribute.Bool("django_migration_pruned", true),
			)
		}
		extractor.TagRelationshipsLanguage(entities, "python")
		extractor.TagEntitiesLanguage(entities, "python")
		return entities, nil
	}

	// Issue #1694 — pre-scan top-level imports BEFORE walkNode so the
	// CALL-extraction pass can qualify `<alias>.<leaf>(...)` shapes by
	// resolving the receiver alias against the file's import bindings.
	// Built once per file and threaded through walkNode →
	// extractCallRelationships. Wildcard imports and unresolvable relative
	// imports return nil entries that the call extractor's range guards
	// treat as "no binding" (i.e. the call falls back to bare-name
	// resolution, preserving prior behaviour).
	importMap := buildPythonImportMap(root, file)

	// Issue #1709 — pre-scan module-level constant lists/tuples for
	// callable attribute references (the `STEPS = [(steps.f, ...), ...]`
	// pattern). Built after importMap so the scanner can validate attribute
	// receivers against the import bindings. Threaded through walkNode →
	// extractCallRelationships → extractDataDispatchCalls. Returns nil
	// when no qualifying constants are found (zero-cost for files that
	// don't use the pattern).
	constReg := buildModuleConstRegistry(root, file.Content, importMap)

	// Issue #3762 — pre-scan module-level Prometheus metric declarations
	// (`X = Summary("name", ...)`) so the observability pass can resolve
	// `@X.time()` decorators / `X.inc()` body calls to a metric name. Returns
	// nil when no metric is declared (zero cost for non-Prometheus files).
	metricReg := buildPyMetricRegistry(root, file.Content)

	// Walk top-level children.
	walkBeforeCount := len(entities)
	walkNode(root, file, "", &entities, &functionCount, &classCount, importMap, constReg, metricReg)

	// Issue #699b — emit CONTAINS edges from the file entity to every
	// top-level class (SCOPE.Component/class) and module-level function
	// (SCOPE.Operation/function) added by walkNode.
	//
	// Top-level classes have bare names (no dot separator); methods and
	// nested classes carry a dotted path ("Class.method", "Class.Inner").
	// Module-level functions also have bare names; methods always contain
	// a dot. We use the structural-ref format that the resolver's
	// lookupLocationKind or lookupUniqueRealComponentByName can bind
	// back to the real entity ID after buildDocument runs.
	//
	// This gives every top-level declaration an inbound CONTAINS edge
	// from the file entity, eliminating the orphan-class and
	// orphan-function buckets that account for ~4pp of the Python
	// orphan rate on Django corpora.
	for i := walkBeforeCount; i < len(entities); i++ {
		child := &entities[i]
		var toID string
		switch {
		case child.Kind == "SCOPE.Component" && child.Subtype == "class" &&
			!strings.ContainsRune(child.Name, '.'):
			// Top-level class — mirrors the inner-class CONTAINS stub format
			// (issue #757) with the class's own source file so the resolver's
			// byLocation [file][name] lookup finds it.
			toID = "scope:component:class:python:" + child.SourceFile + ":" + child.Name
		case child.Kind == "SCOPE.Operation" && child.Subtype == "function" &&
			!strings.ContainsRune(child.Name, '.'):
			// Module-level function — bare name (methods always carry a dot).
			// BuildOperationStructuralRef uses subtype="method" in its path
			// but the resolver's lookupLocationKind key is (file, name),
			// so the subtype label in the stub does not gate resolution.
			toID = extractor.BuildOperationStructuralRef("python", child.SourceFile, child.Name)
		}
		if toID != "" {
			entities[0].Relationships = append(entities[0].Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
		}
	}

	// Imports — Issue #693: instead of emitting standalone SCOPE.Component/module
	// placeholder entities (one per import), attach IMPORTS relationships directly
	// to the file entity (entities[0]). Placeholder entities had zero inbound
	// edges and dominated orphan rates on every Python corpus (87/93.4% on
	// django-realworld, 74/79.0% on flask-realworld, 2590/92.9% on pandas).
	//
	// extractImports still builds the IMPORTS relationships with all Properties
	// (local_name, source_module, imported_name, wildcard). attachImportRelationships
	// lifts those relationships onto the file entity and discards the wrapper
	// module entities. The resolver's BuildImportTable reads IMPORTS edges from
	// any entity — it does not require the carrier to be SCOPE.Component/module.
	importEnts := extractImports(root, file)
	importCount := len(importEnts)
	importEnts = attachImportRelationships(importEnts, &entities[0])
	entities = append(entities, importEnts...)

	// Track A (analog of #641 for Python) — REFERENCES-edge emission.
	// Runs after every primary-pass entity is in place so the file-
	// scope symbol table covers functions, methods, classes, class
	// fields (#526), and import bindings. Failures here recover
	// internally to partial results — never aborts primary output.
	func() {
		defer func() { _ = recover() }()
		emitReferences(root, file, &entities)
	}()

	// Issue #1414 — raw SQL procedure call + view read edges.
	// Scans for cursor.execute("CALL proc(...)") and
	// cursor.execute("SELECT ... FROM view") patterns and emits
	// CALLS / READS_FROM edges from the enclosing Python function to
	// the SQL procedure / view entity. Safe: never removes existing edges.
	func() {
		defer func() { _ = recover() }()
		emitRawSQLDBCallEdges(string(file.Content), file.Path, &entities)
	}()

	// Track B (analog of #642 for Python) — IMPORTS ToID rewrite.
	// Rewrites IMPORTS edges whose source_module points at a known
	// external Python package to an `ext:<module>[:<name>]` ToID so
	// the resolver's external-disposition gate classifies them
	// ExternalKnown directly. In-tree imports are untouched — the
	// existing ResolveDottedImportTarget path binds them via
	// source_module / imported_name properties.
	resolveImportToIDs(entities)

	// Issue #1775 — supplemental config-module pass.
	// Runs after all primary extraction passes so it can observe the full
	// entity list and emit a SCOPE.Config/config_module entity for files
	// that would otherwise surface no semantic entities (e.g. settings.py
	// with only module-level assignments) OR are canonical config files by
	// name (e.g. manage.py, celery.py) even when they do carry functions.
	// The pass never removes or modifies existing entities.
	configModuleEmitted := emitConfigModuleEntity(root, file, &entities)

	// Issue #1967 — capture DRF @action(...) decorator kwargs and surface
	// them as Properties on the per-method Operation entity (and stamp a
	// per-method decorator summary on the parent class). Runs after the
	// primary walk so the matching Operation/Class entities exist.
	func() {
		defer func() { _ = recover() }()
		emitDRFActionProperties(root, file, &entities)
	}()

	// Issue #1990 — Django admin REFERENCES edges from the admin module
	// to its registered Models / ModelAdmin classes, plus ModelAdmin
	// property capture (list_display, search_fields, …) and @admin.action
	// method tagging. No-op for non-admin files.
	func() {
		defer func() { _ = recover() }()
		emitDjangoAdminEdges(root, file, &entities)
	}()

	// Issue #1982 — DEPENDS_ON_CONFIG consumer edges. Scans the file body
	// for `settings.X` and `os.environ.get("X")` / `os.getenv("X")` shapes
	// and emits an edge from the enclosing entity (or the file when at
	// module scope) to the canonical settings.py / .env Config entity.
	// Runs after extractImports so the import-binding scan in Phase 1
	// observes IMPORTS edges already attached to the file entity.
	func() {
		defer func() { _ = recover() }()
		emitConfigConsumerEdges(root, file, &entities)
	}()

	// Epic #3628 — error-flow pass. Emits THROWS / CATCHES edges from
	// functions/methods to a shared SCOPE.ExceptionType node for typed
	// `raise X` / `except X` shapes (dynamic raises and bare `except:` are
	// dropped). Runs after primary entity emission so the enclosing
	// function/method entities exist to attach edges to.
	func() {
		defer func() { _ = recover() }()
		emitExceptionFlowEdges(root, file, &entities)
	}()

	// Epic #3628 — third-party integration pass. Emits DEPENDS_ON_SERVICE
	// edges from functions/methods to a shared SCOPE.ExternalService node for
	// recognised SDK call shapes (stripe.Charge.create, boto3.client("s3")…).
	// Import-gated and precision-first: dynamic / non-SDK receivers emit no
	// edge. Runs after primary entity emission so enclosing function/method
	// entities exist to attach edges to.
	func() {
		defer func() { _ = recover() }()
		emitServiceDependencyEdges(root, file, &entities)
	}()

	// #3628 view-layer — supplemental pass that links Flask render_template /
	// Django render / TemplateView.template_name handlers to a shared
	// SCOPE.Template node via RENDERS (dynamic / f-string names are dropped).
	// Runs after primary entity emission so the enclosing function/method/class
	// entities exist to attach edges to.
	func() {
		defer func() { _ = recover() }()
		emitTemplateRenderEdges(root, file, &entities)
	}()

	// Localization topology (child of #3628) — supplemental pass that links
	// functions / methods to a shared SCOPE.TranslationKey node via
	// USES_TRANSLATION for Django / gettext `_('msg')` / `gettext('x')` shapes
	// (import-gated to a recognised gettext source; dynamic keys dropped).
	func() {
		defer func() { _ = recover() }()
		emitTranslationKeyEdges(root, file, &entities)
	}()

	// Issue #1884 — supplemental package-module pass (Wave 1).
	// Emits one Module entity per Python package boundary (__init__.py or
	// plain .py module) so docgen can seed per-package pages and flow
	// narratives can name intra-package edges. Runs last so it can observe
	// all entities emitted by prior passes (class/function names for
	// __init__.py CONTAINS wiring). The pass never removes or modifies
	// existing entities.
	packageModuleCount := emitPackageModuleEntity(file, &entities)

	// Issue #1984 — async-semantics pass.
	// Stamps is_async on async def entities, emits CALLS edges for any
	// await callees the primary walker missed, and emits CALLS edges for
	// Django Channels channel_layer dispatch sites. Append-only; never
	// modifies prior entities except for the is_async Properties stamp.
	func() {
		defer func() { _ = recover() }()
		applyAsyncSemantics(root, file, &entities)
	}()

	// Issues #1979 / #1980 — Celery decorator + dispatch enrichment.
	// Annotates @shared_task / @app.task Operation entities with
	// is_task + decorator kwargs (bind, max_retries, autoretry_for, ...)
	// and emits same-file CALLS edges from .delay() / .apply_async()
	// call sites to their task entity. The cross-file counterpart lives
	// in internal/engine/django_signal_pubsub_edges.go.
	func() {
		defer func() { _ = recover() }()
		applyCeleryAnnotations(file, &entities)
	}()

	// Issues #2007 / #2009 — supplemental REFERENCES emission for
	// shapes the primary `emitReferences` walker excludes: nested
	// constructor calls inside method bodies (`SerializerClass()` in
	// `get_<field>`) and capitalised attribute receivers inside
	// class-body assignment RHSs (`choices=User.TYPE_CHOICES`). Runs
	// after the primary REFERENCES pass and after the Django relational
	// pass so the class-target table observes the full entity list.
	func() {
		defer func() { _ = recover() }()
		emitNestedConstructorRefs(root, file, &entities)
	}()

	// Issue #2008 — DRF SerializerMethodField → method link. For every
	// `<field> = serializers.SerializerMethodField(...)` declaration
	// emit a RESOLVED_BY edge from the field entity to the sibling
	// `get_<field>` (or `method_name=` kwarg) operation entity.
	func() {
		defer func() { _ = recover() }()
		emitSerializerMethodFieldLinks(root, file, &entities)
	}()

	// Issue #2010 — DRF router.register(prefix, ViewSet, ...). Emit
	// REFERENCES from urls.py / routers.py file entities to each
	// registered ViewSet class, capturing url_prefix + basename props.
	func() {
		defer func() { _ = recover() }()
		emitRouterRegisterEdges(root, file, &entities)
	}()

	// Issue #2011 — Generic CBV / DRF viewset inherited-method
	// annotation (option A). Stamp `inherited_methods` and `cbv_bases`
	// properties on each subclass of a recognised generic base, rather
	// than synthesising fake Operation entities for inherited handlers.
	func() {
		defer func() { _ = recover() }()
		emitCBVInheritedMethodAnnotations(root, file, &entities)
	}()

	// Issue #2016 — generic decorator capture. For every decorated
	// function/method, stamp `decorator_<name>` properties on the
	// matching Operation entity. Generalises the pattern established
	// by DRF @action (#2004) and Celery @shared_task (#2006) so that
	// @property, @cached_property, @staticmethod, @classmethod,
	// @contextmanager, @<name>.setter/.getter/.deleter, and arbitrary
	// decorator factories become queryable graph signals. Runs last
	// among the decorator-aware passes so specialised stamps from
	// DRF / Celery / admin / signal passes win on key collisions.
	func() {
		defer func() { _ = recover() }()
		emitGenericDecoratorProperties(root, file, &entities)
	}()

	// #3628 — transaction-boundary stamping. Mark function/method entities that
	// open a DB transaction (@transaction.atomic decorator, `with
	// transaction.atomic():`, SQLAlchemy session.begin()/engine.begin()) with
	// Properties["transactional"]="true" + tx_source. No transitive
	// propagation — only the lexically-enclosing function is stamped.
	func() {
		defer func() { _ = recover() }()
		emitTransactionBoundaryProperties(root, file, &entities)
	}()

	// Issue #2989 — Type System extraction. Stamps pattern_type +
	// structural properties on Protocol / Enum / TypedDict / NamedTuple /
	// dataclass class entities and emits SCOPE.Schema/type_alias entities for
	// module-level type aliases (X = Union[...], X: TypeAlias = ..., PEP 695
	// `type X = ...`). Mirrors the TypeScript type-extraction precedent
	// (#1343). Runs after the primary walk so the class entities it annotates
	// already exist. Append-and-annotate only — never removes prior entities.
	func() {
		defer func() { _ = recover() }()
		applyTypeSystemAnnotations(root, file, &entities)
	}()

	// Issue #1991 — __init__.py re-export annotation.
	// Stamps re_export / package_init / public / alias properties on
	// IMPORTS edges of package __init__.py files. Returns the public
	// names declared by __all__ so the dead-import pass can preserve
	// them as live regardless of in-body usage.
	var publicNames map[string]bool
	func() {
		defer func() { _ = recover() }()
		publicNames = applyReExports(file, &entities)
	}()

	// Issue #1985 — dead-import detection.
	// Scans the file body for identifier references and stamps
	// live=false / dead_import=true on IMPORTS edges whose local
	// binding is never referenced. Wildcards and side-effect imports
	// are never flagged. Re-exports (publicNames from applyReExports)
	// are treated as live.
	func() {
		defer func() { _ = recover() }()
		applyDeadImports(file, &entities, publicNames)
	}()

	// Issue #1964 + #1966 — final safety net for line-bound + language
	// emission. Every entity emitted by ANY pass (primary walk, error
	// patterns, imports, config_module, constraints, package_module, …)
	// MUST carry:
	//   - Language="python"                          (#1966)
	//   - StartLine > 0                              (#1964)
	//   - EndLine   > 0                              (#1964)
	//
	// Earlier waves observed end_line=0 sentinels on Operations/Classes/
	// Configs/Modules/Signal-handlers in production reindex output even
	// though buildClass / buildFunction set non-zero values themselves
	// (issue1964_endline_test covered those paths). The leaks came from
	// (a) entities synthesised by supplemental passes (#1709 constraint
	// emission used file.Language which is "" when the caller doesn't
	// stamp it; errorpattern.go used StartLine for EndLine; config_module
	// hard-coded EndLine=1), (b) future passes that forget to set both
	// fields. A single finalize sweep makes the extractor's contract
	// uniform: no entity leaves Extract with EndLine=0 or Language="".
	//
	// We use a conservative file-end fallback for EndLine. The bundle-side
	// by-name fallback (#1987) still rebinds to the real source location
	// when the original boundary was unknown; this sentinel scrub keeps
	// downstream renderers (source_window, mcp/tools.go) safe.
	fileEnd := int(root.EndPoint().Row) + 1
	if fileEnd < 1 {
		fileEnd = 1
	}
	for i := range entities {
		if entities[i].Language == "" {
			entities[i].Language = "python"
		}
		if entities[i].StartLine == 0 {
			entities[i].StartLine = 1
		}
		if entities[i].EndLine == 0 {
			entities[i].EndLine = fileEnd
		}
	}

	span.SetAttributes(
		attribute.Int("entity_count", len(entities)),
		attribute.Int("function_count", functionCount),
		attribute.Int("class_count", classCount),
		attribute.Int("import_count", importCount),
		attribute.Bool("config_module_emitted", configModuleEmitted),
		attribute.Int("package_module_count", packageModuleCount),
	)
	// Issue #90 — stamp Properties["language"]="python" on every embedded
	// relationship so the resolver's per-language dynamic-pattern dispatch
	// picks the python catalog instead of falling back to the cross-language
	// one. Existing tags are preserved.
	extractor.TagRelationshipsLanguage(entities, "python")
	extractor.TagEntitiesLanguage(entities, "python")
	return entities, nil
}

// isDjangoMigrationFile reports whether path is an auto-generated Django
// migration module. Django places migrations in a `migrations/` package per
// app; generated files are named `NNNN_<slug>.py` and the package always
// carries an `__init__.py`. We treat any `.py` file whose immediate parent
// directory is `migrations` as generated, which matches both the numbered
// migration files and the package `__init__.py`. Hand-written modules never
// live directly inside a `migrations/` directory in idiomatic Django.
//
// # YAML-driven vs code-driven hybrid (issue #2348)
//
// The engine/rules/python/frameworks/django.yaml file_convention
//
//	glob: "*/migrations/0*.py"
//	entity_type: Migration
//	name_from: filename
//
// identifies migration files and emits a lightweight Migration entity via
// the YAML-driven detector pass. This predicate is intentionally kept as a
// post-YAML hook that gates the RICHER extraction performed by
// extractMigrationEntity (operation JSON, dep graph, op_count). The YAML
// pass identifies the file; this code does the semantic extraction. Both
// coexist without conflict: the YAML entity carries pattern_type=file_convention
// and the extractor entity carries Subtype="django" + Properties["operations"].
// The extractor's entity is authoritative for graph consumers.
//
// Issue #1731 — extension catalogue for other migration frameworks (not yet
// implemented; add a corresponding isXxxMigrationFile predicate and wire it
// into the pruning gate above when needed):
//
//   - Rails (Ruby):  db/migrate/*.rb — immediate parent is "migrate" inside a
//     "db/" directory; language guard: .rb suffix.
//
//   - Alembic (Python): versions/*.py under a repo subtree that contains an
//     alembic.ini (walk up the directory tree from the file to find it).
//     Predicate: suffix ".py" AND parent == "versions" AND alembic.ini exists
//     in an ancestor directory within the repo root.
//
//   - Knex (JavaScript/TypeScript): migrations/*.js or migrations/*.ts files
//     in a Node project. Predicate identical to Django but in the JS extractor:
//     suffix ".js"/".ts" AND immediate parent dir == "migrations".
func isDjangoMigrationFile(path string) bool {
	if !strings.HasSuffix(path, ".py") {
		return false
	}
	dir := filepath.Dir(filepath.FromSlash(path))
	return filepath.Base(dir) == "migrations"
}

// walkNode performs a depth-first traversal of the CST, collecting entities.
// parentClass is "" when outside a class body, or the dotted class path when
// inside (e.g. "Outer" for a top-level class, "Outer.Inner" for a nested one).
// Issue #68 — multi-level nesting is preserved by appending each enclosing
// class name with a "." separator as the walker descends.
func walkNode(
	node ts.Node,
	file extractor.FileInput,
	parentClass string,
	out *[]types.EntityRecord,
	funcCount *int,
	classCount *int,
	imports pythonImportMap,
	constReg moduleConstRegistry,
	metricReg pyMetricRegistry,
) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_definition":
		rec := buildClass(node, file)
		if rec.Name != "" {
			// Issue #757 — qualify nested class entity names with the enclosing
			// class path so they match the dotted form used by methods and fields
			// (e.g. "Order.Meta" not bare "Meta"). This mirrors the approach
			// buildFunction uses for methods (parentClass + "." + name). Top-level
			// classes (parentClass=="") keep their bare name unchanged.
			if parentClass != "" {
				rec.Name = parentClass + "." + rec.Name
				rec.Signature = "class " + rec.Name
			}
			// Issue #1413 — re-derive QualifiedName from the (possibly updated)
			// rec.Name so nested classes get the fully-dotted qualified form,
			// e.g. "orders.handlers.Outer.Inner" not "orders.handlers.Inner".
			if mod := filePathToModule(file.Path); mod != "" {
				rec.QualifiedName = mod + "." + rec.Name
			} else {
				rec.QualifiedName = rec.Name
			}
			classIdx := len(*out)
			*out = append(*out, rec)
			*classCount++
			// Issue #698 — emit EXTENDS edges for base classes declared in the
			// argument_list of this class_definition. Top-level classes use their
			// bare name; nested classes use their qualified name (rec.Name).
			// extractBaseClasses consults the global PythonClassRegistry to resolve
			// cross-file bases to the correct source file path when unambiguous.
			if extendsRels := extractBaseClasses(node, file.Path, rec.Name, file.Content); len(extendsRels) > 0 {
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships, extendsRels...)
			}
			// Walk the class body so methods are captured with parentClass set.
			// For nested classes the parent path accumulates: "Outer" → "Outer.Inner".
			// childParent is now just rec.Name (already qualified above).
			childParent := rec.Name
			body := node.ChildByFieldName("body")
			if body != nil {
				before := len(*out)
				for i := range int(body.ChildCount()) {
					walkNode(body.Child(i), file, childParent, out, funcCount, classCount, imports, constReg, metricReg)
				}
				// Issue #526 — class-attribute assignments (DRF ViewSet
				// `serializer_class = ...`, Django Model `title =
				// models.CharField(...)`, SQLAlchemy `id = Column(...)`)
				// are emitted as SCOPE.Schema/field entities whose Name is
				// "<dottedClass>.<attr>" so the resolver's byMember index
				// binds `self.<attr>` references back to them.
				extractClassFields(body, file, childParent, out)
				// Emit CONTAINS edges from the class to every Operation
				// (method) AND every Schema/field entity the walker just
				// appended.
				//
				// History: field entities (#526) were originally emitted
				// WITHOUT a CONTAINS edge because the field's hex ComputeID
				// isn't known until buildDocument runs. The Format A
				// structural-ref pattern already used for class→method
				// edges sidesteps that constraint — the resolver binds the
				// stub to the field entity via byLocation (same-file, name
				// matches the field's Name="<Class>.<attr>"). Closing this
				// gap eliminates the largest residual orphan class on
				// Python corpora (SCOPE.Schema/field with zero edges).
				after := len(*out)
				for k := before; k < after; k++ {
					child := &(*out)[k]
					var toID string
					switch {
					case child.Kind == "SCOPE.Operation":
						// Issue #144 — Format A structural-ref keyed on the
						// source file so the resolver disambiguates by
						// location when two classes in different files
						// declare methods with the same Name. FromID empty →
						// buildDocument substitutes the parent (class)
						// entity ID at emit time.
						toID = extractor.BuildOperationStructuralRef("python", file.Path, child.Name)
					case child.Kind == "SCOPE.Schema" && child.Subtype == "field":
						// Class-attribute assignment emitted by
						// extractClassFields (#526). The stub resolves via
						// byLocation[file][<Class>.<attr>] in the same way
						// class→method does via lookupLocationKind +
						// byLocation fallback.
						toID = extractor.BuildSchemaFieldStructuralRef("python", file.Path, child.Name)
					case child.Kind == "SCOPE.Component" && child.Subtype == "class":
						// Issue #757 — nested class CONTAINS edge. Covers
						// Django Meta/Config inner classes and generic Python
						// nesting. The structural-ref format mirrors the
						// class→method form but targets SCOPE.Component/class.
						// The resolver's byLocation fallback binds the stub
						// via lookupLocationKind using the child's SourceFile
						// and Name (which is already dotted: "<Parent>.<Inner>").
						toID = "scope:component:class:python:" + file.Path + ":" + child.Name
					default:
						continue
					}
					(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
						types.RelationshipRecord{
							ToID: toID,
							Kind: "CONTAINS",
						})
				}
				// Issue #757 — propagate inner-class framework properties onto
				// the parent class entity. For Meta/Config inner classes that
				// carry framework-specific configuration keys (Django db_table,
				// ordering, abstract; Pydantic Config; DRF Serializer.Meta),
				// parse the Meta body and set corresponding properties on the
				// parent entity in-place before pipeline emission (Option A).
				for k := before; k < after; k++ {
					child := &(*out)[k]
					if child.Kind == "SCOPE.Component" && child.Subtype == "class" {
						applyFrameworkInnerClassProperties(&(*out)[classIdx], child, body, file.Content, childParent)
						// Issue #749 — Django Model.Meta constraints=[UniqueConstraint/
						// CheckConstraint] emit SCOPE.Constraint entities that were
						// previously orphaned because no CONTAINS edge linked them to
						// the parent Model class. Parse the Meta.constraints list and
						// emit synthetic SCOPE.Constraint entities with CONTAINS edges.
						// extractMetaConstraintEntities is a no-op for non-Meta inner
						// classes (Config, etc.) — it re-checks the name internally.
						extractMetaConstraintEntities(body, file, child.Name, childParent, classIdx, out)
					}
				}
				// Issues #1977 / #1978 / #1989 — Django relational extraction.
				// After SCOPE.Schema/field entities exist (extractClassFields)
				// and CONTAINS edges are in place, enrich each field with
				// field_type + kwargs (#1978), emit REFERENCES from FK/O2O/M2M
				// fields to their target Model (#1977), and emit REFERENCES
				// from the parent Model to any `<attr> = <Manager>()` style
				// attachment (#1989).
				enrichDjangoModelFieldsAndManagers(body, file, childParent, classIdx, before, after, out)
				// Issue #2061 — DRF serializer field REFERENCES emission.
				// Covers PrimaryKeyRelatedField / HyperlinkedRelatedField /
				// SlugRelatedField (queryset → model), nested-serializer
				// references (FooSerializer(...)), source="…" path binding to
				// Meta.model, and implicit ModelSerializer scalar→Meta.model
				// binding. Runs AFTER applyFrameworkInnerClassProperties so
				// the parent's `meta_model` property is already stamped.
				emitDRFSerializerFieldRefs(body, file, childParent, classIdx, before, after, out)
				// Issue #4871 — stamp per-field VALIDATION constraint chips
				// (Pydantic Field()/Annotated/con*, DRF serializer kwargs,
				// Optional markers, @field_validator presence) onto the
				// SCOPE.Schema/field entities under Properties["validations"]
				// so the dashboard ShapeTree surfaces them — mirroring the TS
				// class-validator support in #4858.
				emitPythonFieldValidations(body, file, childParent, classIdx, before, after, out)
				// Issue #2816 — stamp the DRF class-level authorisation surface
				// (permission_classes attribute + get_permissions override) onto
				// the ViewSet/APIView entity so grafel_auth_coverage can
				// recognise class-level auth, not just per-method decorators.
				applyDRFPermissionProperties(&(*out)[classIdx], body, file.Content)
			}
		}
		return // body handled above — do not recurse further

	case "function_definition":
		rec := buildFunction(node, file, parentClass)
		if rec.Name != "" {
			// Self-recursion is identified by the bare function name, not the
			// class-qualified Name. nameNode is the leaf identifier.
			selfName := rec.Name
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				selfName = nodeText(nameNode, file.Content)
			}
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(node.ChildByFieldName("body"), file.Content, selfName, parentClass, imports, constReg)...)
			funcIdx := len(*out)
			*out = append(*out, rec)
			*funcCount++
			// Issue #2654 — stamp discriminator comparisons found in the body.
			stampPythonDiscriminators(node.ChildByFieldName("body"), file.Content, out, funcIdx)
			// Issue #3689 — stamp OpenTelemetry span-creation sites (no decorator
			// parent for a bare function_definition).
			stampPythonTracingSpans(node, nil, selfName, file.Content, out, funcIdx)
			// Issue #3762 — stamp non-OTel instrumentation (ddtrace/Sentry/
			// Prometheus/New Relic). Bare function → no decorator parent; body
			// calls (tracer.trace, METRIC.inc, …) are still scanned.
			stampPythonObservability(node, nil, selfName, metricReg, file.Content, out, funcIdx)
		}
		return // do not recurse into function body for nested definitions

	case "decorated_definition":
		// A decorated_definition wraps a function_definition or class_definition.
		// The base Python parser does not emit decorator info — framework extractors
		// (FastAPI, Flask, etc.) handle decorator-specific extraction in later passes.
		inner := node.ChildByFieldName("definition")
		if inner == nil {
			return
		}
		switch inner.Type() {
		case "function_definition":
			rec := buildFunction(inner, file, parentClass)
			if rec.Name != "" {
				selfName := rec.Name
				if nameNode := inner.ChildByFieldName("name"); nameNode != nil {
					selfName = nodeText(nameNode, file.Content)
				}
				rec.Relationships = append(rec.Relationships,
					extractCallRelationships(inner.ChildByFieldName("body"), file.Content, selfName, parentClass, imports, constReg)...)
				decoratedFuncIdx := len(*out)
				*out = append(*out, rec)
				*funcCount++
				// Issue #2654 — stamp discriminator comparisons found in the body.
				stampPythonDiscriminators(inner.ChildByFieldName("body"), file.Content, out, decoratedFuncIdx)
				// Issue #3689 — stamp OpenTelemetry span-creation sites, scanning
				// both the body and the decorator list (node is the
				// decorated_definition wrapping inner).
				stampPythonTracingSpans(inner, node, selfName, file.Content, out, decoratedFuncIdx)
				// Issue #3762 — stamp non-OTel instrumentation (ddtrace/Sentry/
				// Prometheus/New Relic) from the decorator list + body.
				stampPythonObservability(inner, node, selfName, metricReg, file.Content, out, decoratedFuncIdx)
			}
		case "class_definition":
			rec := buildClass(inner, file)
			if rec.Name != "" {
				// Issue #757 — qualify nested class name (same as bare class branch).
				if parentClass != "" {
					rec.Name = parentClass + "." + rec.Name
					rec.Signature = "class " + rec.Name
				}
				// Issue #1413 — re-derive QualifiedName after name qualification.
				if mod := filePathToModule(file.Path); mod != "" {
					rec.QualifiedName = mod + "." + rec.Name
				} else {
					rec.QualifiedName = rec.Name
				}
				classIdx := len(*out)
				*out = append(*out, rec)
				*classCount++
				// Issue #698 — emit EXTENDS edges for decorated class.
				if extendsRels := extractBaseClasses(inner, file.Path, rec.Name, file.Content); len(extendsRels) > 0 {
					(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships, extendsRels...)
				}
				childParent := rec.Name
				body := inner.ChildByFieldName("body")
				if body != nil {
					before := len(*out)
					for i := range int(body.ChildCount()) {
						walkNode(body.Child(i), file, childParent, out, funcCount, classCount, imports, constReg, metricReg)
					}
					// Issue #526 — see the bare class_definition branch.
					extractClassFields(body, file, childParent, out)
					after := len(*out)
					for k := before; k < after; k++ {
						child := &(*out)[k]
						var toID string
						switch {
						case child.Kind == "SCOPE.Operation":
							// Issue #144 — structural-ref (Format A) keyed on
							// file path; same rationale as the bare class
							// branch above.
							toID = extractor.BuildOperationStructuralRef("python", file.Path, child.Name)
						case child.Kind == "SCOPE.Schema" && child.Subtype == "field":
							// Class→field CONTAINS (closes #526 deferred
							// emission). See the bare class branch for the
							// detailed rationale.
							toID = extractor.BuildSchemaFieldStructuralRef("python", file.Path, child.Name)
						case child.Kind == "SCOPE.Component" && child.Subtype == "class":
							// Issue #757 — nested class CONTAINS (same logic as
							// the bare class_definition branch above).
							toID = "scope:component:class:python:" + file.Path + ":" + child.Name
						default:
							continue
						}
						(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
							types.RelationshipRecord{
								ToID: toID,
								Kind: "CONTAINS",
							})
					}
					// Issue #757 — propagate inner-class framework properties.
					for k := before; k < after; k++ {
						child := &(*out)[k]
						if child.Kind == "SCOPE.Component" && child.Subtype == "class" {
							applyFrameworkInnerClassProperties(&(*out)[classIdx], child, body, file.Content, childParent)
							// Issue #749 — emit SCOPE.Constraint entities for
							// Meta.constraints=[UniqueConstraint/CheckConstraint].
							extractMetaConstraintEntities(body, file, child.Name, childParent, classIdx, out)
						}
					}
					// Issues #1977 / #1978 / #1989 — see the bare class branch
					// above for the full rationale. Handles decorated model
					// classes (e.g. @python_2_unicode_compatible on legacy
					// Django).
					enrichDjangoModelFieldsAndManagers(body, file, childParent, classIdx, before, after, out)
					// Issue #2061 — DRF serializer field REFERENCES (decorated
					// class branch). See bare branch above for rationale.
					emitDRFSerializerFieldRefs(body, file, childParent, classIdx, before, after, out)
					// Issue #4871 — per-field VALIDATION constraint chips
					// (decorated class branch). See bare branch above.
					emitPythonFieldValidations(body, file, childParent, classIdx, before, after, out)
					// Issue #2816 — DRF class-level authorisation surface
					// (decorated class branch, e.g. @method_decorator-wrapped
					// ViewSets). See bare branch above for rationale.
					applyDRFPermissionProperties(&(*out)[classIdx], body, file.Content)
				}
			}
		}
		return

	default:
		// Recurse into all other node types.
		for i := range int(node.ChildCount()) {
			walkNode(node.Child(i), file, parentClass, out, funcCount, classCount, imports, constReg, metricReg)
		}
	}
}

// buildClass constructs a SCOPE.Component EntityRecord for a class_definition node.
// QualifiedName is set to "<module>.<name>" where module is derived from
// the file path (issue #1413). For nested classes the caller must re-derive
// QualifiedName after prefixing rec.Name with the parent class path.
//
// Issue #2552 — entity Name MUST equal the literal AST symbol extracted from
// the class_definition name node. No post-hoc substitution with inferred labels
// (filename stem, docstring reference, PascalCase conversion) is permitted.
// When a human-friendly label is available from the filename stem and differs
// from the AST name, it is stored in Properties["display_name"] so MCP
// consumers can surface both, but the Name field is never overwritten.
func buildClass(node ts.Node, file extractor.FileInput) types.EntityRecord {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return types.EntityRecord{}
	}
	// Name is always the verbatim AST identifier — never an inferred label.
	name := nodeText(nameNode, file.Content)

	mod := filePathToModule(file.Path)
	qn := mod + "." + name
	if mod == "" {
		qn = name
	}

	// Issue #2552 — when the file stem (e.g. "notes_helper") provides a
	// human-friendly label that differs from the AST name (e.g. "n" or any
	// short single-purpose class), record it as display_name without touching
	// Name. toPascalCase converts "notes_helper" → "NotesHelper". We only set
	// display_name when it is non-empty and different from the AST name so that
	// well-named classes (class NoteHelper in notes_helper.py) don't get a
	// redundant property.
	var props map[string]string
	if base := filepath.Base(file.Path); base != "" {
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if stem != "" && stem != "__init__" {
			pascal := toPascalCase(stem)
			if pascal != "" && pascal != name {
				props = map[string]string{"display_name": pascal}
			}
		}
	}

	return types.EntityRecord{
		Name:               name,
		QualifiedName:      qn,
		Kind:               "SCOPE.Component",
		Subtype:            "class",
		Language:           "python",
		SourceFile:         file.Path,
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "class " + name,
		Properties:         props,
		EnrichmentRequired: false,
	}
}

// toPascalCase converts a snake_case or kebab-case identifier to PascalCase.
// Used by buildClass to derive a human-readable display_name from a filename
// stem when the AST class name is short or otherwise opaque (issue #2552).
//
//	"notes_helper" → "NotesHelper"
//	"n"            → "N"            (single char — capitalised but kept)
//	"my-view"      → "MyView"
func toPascalCase(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	upNext := true
	for _, r := range s {
		if r == '-' || r == '_' {
			upNext = true
			continue
		}
		if upNext && r >= 'a' && r <= 'z' {
			b.WriteRune(r - 32)
		} else {
			b.WriteRune(r)
		}
		upNext = false
	}
	return b.String()
}

// buildFunction constructs a SCOPE.Operation EntityRecord for a function_definition.
// parentClass is "" for module-level functions, or the dotted class path for
// methods (e.g. "Foo" for a top-level class method, "Outer.Inner" for a method
// defined on a nested class — issue #68).
//
// Methods are emitted with Name="<dotted.class.path>.<method>" (issue #45 +
// issue #68) so two classes declaring a same-named method in the same file
// produce distinct entity IDs via ComputeID(SourceFile+Kind+Name). The dotted
// form is the same encoding used by Format B structural references and is
// indexed natively by resolve.Index.byMember, which splits Name on the LAST
// '.' to preserve multi-level scopes.
func buildFunction(node ts.Node, file extractor.FileInput, parentClass string) types.EntityRecord {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return types.EntityRecord{}
	}
	name := nodeText(nameNode, file.Content)

	subtype := "function"
	emittedName := name
	if parentClass != "" {
		subtype = "method"
		emittedName = parentClass + "." + name
	}

	params := ""
	paramsNode := node.ChildByFieldName("parameters")
	if paramsNode != nil {
		params = nodeText(paramsNode, file.Content)
	}

	returnType := ""
	retNode := node.ChildByFieldName("return_type")
	if retNode != nil {
		returnType = " -> " + nodeText(retNode, file.Content)
	}

	// Issue #1413 — set qualified_name to "<module>.<emittedName>".
	// emittedName already contains the dotted class path for methods,
	// so the qualified form is simply module + "." + emittedName.
	mod := filePathToModule(file.Path)
	qn := mod + "." + emittedName
	if mod == "" {
		qn = emittedName
	}

	return types.EntityRecord{
		Name:               emittedName,
		QualifiedName:      qn,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		Language:           "python",
		SourceFile:         file.Path,
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "def " + name + params + returnType,
		EnrichmentRequired: false,
	}
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// `call` node descended from body. The target name is computed from the
// `function` child of the call node:
//
//	identifier               → bare name           (e.g. "open")
//	attribute (a.b.c)        → trailing attribute  (e.g. "c")
//	parenthesized_expression → inner identifier    (best-effort)
//
// FromID is left empty so buildDocument substitutes the caller's entity ID
// at emit time. ToID is the bare callee name when no receiver type can be
// inferred — the resolver rewrites it to a deterministic ID when an
// unambiguous same-named entity exists in the merged index.
//
// Issue #69 — when the call has an inferable receiver type the target is
// emitted in dotted "<Class>.<method>" form so the resolver can bind the
// edge to the correct method entity even when multiple classes in the same
// file declare a same-named method:
//
//	self.foo()       → "<parentClass>.foo"   (true self-recursion drops)
//	ClassName().foo()→ "ClassName.foo"
//	obj.foo()        → "foo" + properties{disposition_hint: "ambiguous"}
//
// parentClass is the dotted enclosing class path of the caller ("" for
// module-level functions). It is used to qualify `self.X` calls and to
// detect when an inferred class-qualified target is in fact self-recursion.
func extractCallRelationships(
	body ts.Node,
	src []byte,
	callerName, parentClass string,
	imports pythonImportMap,
	constReg moduleConstRegistry,
) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAll(body, "call")
	// Self-target in dotted form, used to drop true self-recursion when the
	// receiver resolves back to the caller's own class.
	selfQualified := callerName
	if parentClass != "" {
		selfQualified = parentClass + "." + callerName
	}
	// De-dup by (target, import_alias). A bare-name `foo()` and an attribute
	// call `mod.foo()` whose leaf happens to also be `foo` are different
	// callees in the cross-module case — they would resolve to different
	// entities — so the alias is part of the dedup key.
	type seenKey struct{ target, alias string }
	seen := make(map[seenKey]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))

	// Issue #4681 — local-variable receiver typing. Scan the body once for
	// `localName = ClassName(...)` constructor bindings so a subsequent
	// `localName.method()` resolves to `ClassName.method` instead of an
	// unresolvable bare leaf. This is the dominant unit-test shape
	// (`v = ProposalViewSet(); v.get_counts(...)` / `view = XView()` /
	// `obj = XSerializer(...)`) — without it ComputeCoverage undercounts
	// controller/view coverage because the test→handler CALLS edge never
	// forms. The map mirrors the TS/JS local-receiver typing in #4671.
	localRecv := localReceiverTypes(body, src)
	for _, call := range calls {
		// Issue #2806 — subprocess / exec calls invoke an OS binary, not an
		// indexed Python symbol. `subprocess.run(['libreoffice', ...])`,
		// `os.system('convert ...')`, `subprocess.Popen(...)` etc. must never
		// bind their method leaf (run / call / system / Popen) to a same-named
		// in-repo entity. Detect the shape here and either emit an
		// `ext:<binary>` CALLS edge (when the binary name is a string/list
		// literal we can read) or drop the edge entirely (dynamic argv) so the
		// leaf-name resolver never produces a phantom in-repo edge.
		if isSubprocessExecCall(call, src, imports) {
			if bin := subprocessBinaryName(call, src); bin != "" {
				extTarget := "ext:" + bin
				key := seenKey{extTarget, ""}
				if !seen[key] {
					seen[key] = true
					callLine := strconv.Itoa(int(call.StartPoint().Row) + 1)
					rels = append(rels, types.RelationshipRecord{
						ToID: extTarget,
						Kind: "CALLS",
						Properties: map[string]string{
							"language":     "python",
							"pattern_type": "subprocess_exec",
							"external_bin": bin,
							"line":         callLine,
						},
					})
				}
			}
			// Whether or not we could read a binary name, the method leaf
			// (run/Popen/system/...) must NOT flow to the bare-name resolver.
			continue
		}
		target, ambiguous := callTarget(call, src, parentClass, localRecv)
		if target == "" {
			continue
		}
		// Issue #1694 — cross-module attribute call.
		// Detect the `<import_alias>.<leaf>(...)` shape and stamp the
		// resolver hint properties. callTarget returned the bare leaf with
		// ambiguous=true for this shape; we keep the bare leaf as ToID
		// (so ResolveImports' `ContainsAny(":.#")` skip doesn't drop the
		// edge before the cross-module path runs) and add the alias hint
		// so the resolver can look the binding up against the file's
		// import bucket.
		var importAlias string
		if len(imports) > 0 {
			if alias, leaf := extractCallTargetImportAlias(call, src, imports); alias != "" {
				importAlias = alias
				// Defensive: extractCallTargetImportAlias returned the
				// same leaf callTarget surfaced, but if they ever diverge
				// (grammar drift) prefer the import-aware leaf so the
				// resolver tuple is always consistent.
				target = leaf
			}
		}
		// Drop self-recursion. The bare-name match preserves prior behavior
		// for module-level recursion (parentClass == ""); the dotted match
		// catches `self.foo()` inside the owning class. Self-recursion can't
		// be a cross-module call, so this only fires when importAlias == "".
		if importAlias == "" && (target == callerName || target == selfQualified) {
			continue
		}
		key := seenKey{target, importAlias}
		if seen[key] {
			continue
		}
		seen[key] = true
		callLine := strconv.Itoa(int(call.StartPoint().Row) + 1)
		r := types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		}
		switch {
		case importAlias != "":
			// Cross-module attribute call — the resolver's
			// ResolveCrossModuleCallTarget consumes these properties to
			// look up (source_module, leaf) against the file's import
			// bucket. The ambiguous hint is dropped because the
			// import_alias now disambiguates the call site precisely.
			r.Properties = map[string]string{
				"import_alias": importAlias,
				"call_leaf":    target,
				"line":         callLine,
			}
		case ambiguous:
			r.Properties = map[string]string{
				"disposition_hint": "ambiguous",
				"line":             callLine,
			}
		default:
			r.Properties = map[string]string{"line": callLine}
		}
		rels = append(rels, r)
	}

	// Issue #1709 — data-structure-driven dispatch pass.
	// Detects `for f in STEPS: f(ctx)` patterns where STEPS is a module-level
	// constant list of callable attribute references. Emits one CALLS edge per
	// callable registered in the constant. Uses a seenKeyDD map bridged from
	// the local `seen` map to prevent double-emission when the same
	// (alias, leaf) was already emitted by the direct attribute-call path.
	if len(constReg) > 0 {
		ddSeen := make(map[seenKeyDD]bool, len(seen))
		for k := range seen {
			ddSeen[seenKeyDD{target: k.target, alias: k.alias}] = true
		}
		ddRels := extractDataDispatchCalls(body, src, constReg, callerName, ddSeen)
		rels = append(rels, ddRels...)
	}

	// Issue #5158 — reflective `getattr(self, name)()` dispatch where `name` is a
	// string literal bound earlier in the body. Resolves the CALLS edge to
	// `<parentClass>.<method>` via the cross-language literal-binding resolver.
	// Only fires for in-class methods (parentClass != ""). Bridges the same
	// `seen` set so an edge already emitted by the direct path is not duplicated.
	if parentClass != "" {
		gaSeen := make(map[seenKeyDD]bool, len(seen))
		for k := range seen {
			gaSeen[seenKeyDD{target: k.target, alias: k.alias}] = true
		}
		rels = append(rels, extractGetattrDispatchCalls(body, src, parentClass, callerName, gaSeen)...)
	}

	return rels
}

// subprocessExecLeaves are the subprocess-module function leaves that spawn an
// external OS binary. Their first positional argument is a command (binary
// name or argv list), NEVER a Python symbol — so the call must resolve to an
// External node, not the same-named in-repo function/method (issue #2806).
var subprocessExecLeaves = map[string]struct{}{
	"run": {}, "call": {}, "Popen": {},
	"check_call": {}, "check_output": {}, "getoutput": {}, "getstatusoutput": {},
}

// osExecLeaves are the os-module functions that execute / spawn an OS binary.
// Like the subprocess family, their argv is a command line, not a Python
// symbol. `os.system`/`os.popen` take a command string; the `os.exec*` and
// `os.spawn*` families take a program path (with `os.spawn*` taking a mode arg
// first — handled by subprocessBinaryName via first-string-literal scan).
var osExecLeaves = map[string]struct{}{
	"system": {}, "popen": {},
	"execl": {}, "execle": {}, "execlp": {}, "execlpe": {},
	"execv": {}, "execve": {}, "execvp": {}, "execvpe": {},
	"spawnl": {}, "spawnle": {}, "spawnlp": {}, "spawnlpe": {},
	"spawnv": {}, "spawnve": {}, "spawnvp": {}, "spawnvpe": {},
	"posix_spawn": {}, "posix_spawnp": {},
}

// isSubprocessExecCall reports whether the `call` node invokes an external OS
// binary via the subprocess / os modules. Recognised shapes (receiver matched
// by local module alias so `import subprocess as sp` / `import os as _os` also
// fire):
//
//	subprocess.run(...)        subprocess.Popen(...)   subprocess.check_output(...)
//	os.system(...)             os.popen(...)           os.execv(...) / os.spawn*(...)
//
// Bare `system(...)` / `run(...)` without a module receiver are NOT matched —
// those could be user functions and the normal resolver should handle them.
func isSubprocessExecCall(call ts.Node, src []byte, imports pythonImportMap) bool {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "attribute" {
		return false
	}
	recvNode := fn.ChildByFieldName("object")
	attrNode := fn.ChildByFieldName("attribute")
	if recvNode == nil || attrNode == nil || recvNode.Type() != "identifier" {
		return false
	}
	recv := nodeText(recvNode, src)
	leaf := nodeText(attrNode, src)

	// Resolve the receiver alias to the imported module root, if any, so
	// `import subprocess as sp; sp.run(...)` is recognised.
	module := recv
	if imports != nil {
		if binding, ok := imports[recv]; ok {
			// sourceModule is the dotted import path the alias points at.
			root := binding.sourceModule
			if root == "" {
				root = binding.importedName
			}
			if i := strings.IndexByte(root, '.'); i >= 0 {
				root = root[:i]
			}
			if root != "" {
				module = root
			}
		}
	}

	switch module {
	case "subprocess":
		_, ok := subprocessExecLeaves[leaf]
		return ok
	case "os":
		_, ok := osExecLeaves[leaf]
		return ok
	}
	return false
}

// subprocessBinaryName extracts the OS binary name from a subprocess / os exec
// call's arguments. Returns "" when the binary cannot be statically read (a
// variable argv, an f-string, a joined path, …) — the caller then drops the
// edge rather than guessing.
//
// Handled first-argument shapes (the binary is the program word):
//
//	subprocess.run(['libreoffice', '--headless', f]) → "libreoffice"  (list literal, first string)
//	subprocess.run('ls -la')                          → "ls"           (string literal, first word)
//	os.system('convert in.png out.pdf')               → "convert"
//	os.execv('/usr/bin/git', [...])                   → "git"          (basename of program path)
//
// `os.spawn*` takes a mode argument first; we scan for the first string/list
// literal among the positional arguments rather than assuming arg index 0.
func subprocessBinaryName(call ts.Node, src []byte) string {
	argsNode := call.ChildByFieldName("arguments")
	if argsNode == nil {
		return ""
	}
	for i := 0; i < int(argsNode.NamedChildCount()); i++ {
		arg := argsNode.NamedChild(i)
		switch arg.Type() {
		case "string":
			return binaryFromCommandWord(pyStringLiteralValue(arg, src))
		case "list":
			// First named child string literal of the argv list.
			for j := 0; j < int(arg.NamedChildCount()); j++ {
				el := arg.NamedChild(j)
				if el.Type() == "string" {
					return binaryFromProgramPath(pyStringLiteralValue(el, src))
				}
				// Non-literal first element (variable) → not statically readable.
				return ""
			}
			return ""
		case "keyword_argument", "comment":
			continue
		default:
			// First positional argument is dynamic (identifier, call, etc.)
			// → cannot read the binary name. Stop scanning.
			return ""
		}
	}
	return ""
}

// binaryFromCommandWord takes a shell command string ("convert a b") and
// returns the program word ("convert"), then reduces it to a basename.
func binaryFromCommandWord(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if i := strings.IndexAny(cmd, " \t"); i >= 0 {
		cmd = cmd[:i]
	}
	return binaryFromProgramPath(cmd)
}

// binaryFromProgramPath reduces a program path ("/usr/bin/git") to its
// basename ("git"). A bare name passes through unchanged. Empty / clearly
// non-binary tokens (containing shell metacharacters) yield "".
func binaryFromProgramPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	// Reject tokens that obviously aren't a binary name (shell metachars,
	// interpolation markers from an f-string body we stripped, …).
	if strings.ContainsAny(p, "{}$*?|&;<>()") {
		return ""
	}
	p = filepath.Base(filepath.ToSlash(p))
	if p == "." || p == "/" || p == "" {
		return ""
	}
	return p
}

// pyStringLiteralValue returns the textual content of a tree-sitter Python
// `string` node with its surrounding quotes (and any prefix like r/f/b)
// stripped. f-strings with interpolations are returned with the raw body so
// callers can decide whether the result is statically usable.
func pyStringLiteralValue(n ts.Node, src []byte) string {
	// Prefer the string_content child when present (grammar ≥ 0.20 splits the
	// literal into string_start / string_content / string_end).
	for i := 0; i < int(n.NamedChildCount()); i++ {
		ch := n.NamedChild(i)
		if ch.Type() == "string_content" {
			return nodeText(ch, src)
		}
		if ch.Type() == "interpolation" {
			// f-string with an interpolation as (part of) the program word:
			// not statically readable.
			return ""
		}
	}
	// Fallback: strip quotes/prefix from the raw token text.
	raw := nodeText(n, src)
	raw = strings.TrimLeft(raw, "rRbBfFuU")
	raw = strings.Trim(raw, "\"'")
	return raw
}

// callTarget resolves the callee name from a tree-sitter Python `call` node.
//
// Return values:
//
//	target     — dotted "<Class>.<method>" when the receiver type can be
//	             inferred from the call expression itself, or the bare leaf
//	             identifier otherwise. Empty when the call node has no
//	             resolvable function child.
//	ambiguous  — true when the leaf is a method call on a receiver whose
//	             type cannot be inferred from local syntax (e.g. `obj.foo()`
//	             where `obj` is a parameter or a name bound elsewhere). The
//	             resolver consumes this hint via the relationship's
//	             properties.disposition_hint to classify the edge.
//
// Receiver-type inference is purely syntactic and intentionally narrow:
//
//	self.foo()         — receiver = parentClass     (qualified if non-empty)
//	ClassName().foo()  — receiver = ClassName       (constructor-call result)
//	obj.foo()          — receiver unknown           (ambiguous=true, bare leaf)
//	foo()              — bare identifier            (no receiver, unchanged)
//	a.b.c()            — trailing identifier "c"    (chain receiver unknown)
//
// parentClass is the dotted enclosing class path of the caller, or "" when
// the caller is module-level. It is consulted only for `self.<x>` resolution.
// localReceiverTypes scans a function/method body for local-variable
// constructor bindings and returns a `localName → ClassName` map so a
// subsequent `localName.method()` call types through receiverClass to
// `ClassName.method` (issue #4681). It mirrors the TS/JS local-receiver
// typing landed in #4671 for the dominant unit-test shape:
//
//	v   = ProposalViewSet()       # DRF ViewSet unit spec
//	view = ArticleView()          # Django CBV unit spec
//	obj = ArticleSerializer(data) # DRF serializer unit spec
//	v.get_counts('2025')          # → ProposalViewSet.get_counts
//
// Recognised binding shape: an `assignment` whose left side is a single
// bare identifier and whose right side is a `call` expression whose
// function is a bare PascalCase class identifier (the same conservative
// class-name heuristic receiverClass already uses for `User.save(...)`).
// Namespaced constructors (`pkg.Thing()`), keyword/typed assignments, and
// non-construction RHS shapes are intentionally skipped — a guess there
// risks binding edges to the wrong class.
//
// Precedence/conservatism mirrors #4671: first binding wins on a re-assign
// collision (bias to miss rather than mis-bind to a later reassignment of a
// different type), and a non-PascalCase RHS constructor (factory function,
// lowercased helper) yields no entry. Returns nil for bodies with no
// qualifying binding so the hot path allocates nothing.
func localReceiverTypes(body ts.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	assigns := findAll(body, "assignment")
	if len(assigns) == 0 {
		return nil
	}
	var locals map[string]string
	for _, a := range assigns {
		lhs := a.ChildByFieldName("left")
		if lhs == nil || lhs.Type() != "identifier" {
			// Tuple/subscript/attribute targets don't name a single receiver.
			continue
		}
		rhs := a.ChildByFieldName("right")
		if rhs == nil || rhs.Type() != "call" {
			continue
		}
		ctor := rhs.ChildByFieldName("function")
		if ctor == nil || ctor.Type() != "identifier" {
			// `pkg.Thing()` (attribute) and other non-bare constructors are
			// left to the resolver; we only bind unambiguous local classes.
			continue
		}
		clsName := nodeText(ctor, src)
		if !isPascalCaseClassName(clsName) {
			continue
		}
		name := nodeText(lhs, src)
		if name == "" {
			continue
		}
		if locals == nil {
			locals = map[string]string{}
		}
		if _, seen := locals[name]; !seen {
			locals[name] = clsName
		}
	}
	return locals
}

func callTarget(call ts.Node, src []byte, parentClass string, localRecv map[string]string) (string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return "", false
	}
	switch fn.Type() {
	case "identifier":
		return nodeText(fn, src), false
	case "attribute":
		// Trailing attribute identifier — the leaf method name.
		attr := fn.ChildByFieldName("attribute")
		if attr == nil {
			return "", false
		}
		leaf := nodeText(attr, src)
		// Inspect the receiver (`object` field) to qualify the leaf when we
		// can. Anything we don't recognise falls back to the bare leaf with
		// the ambiguous hint so the resolver can downgrade it appropriately.
		recv := fn.ChildByFieldName("object")
		if recv == nil {
			return leaf, true
		}
		if cls := receiverClass(recv, src, parentClass, localRecv); cls != "" {
			return cls + "." + leaf, false
		}
		return leaf, true
	case "parenthesized_expression":
		for i := 0; i < int(fn.ChildCount()); i++ {
			ch := fn.Child(i)
			if ch.Type() == "identifier" {
				return nodeText(ch, src), false
			}
			if ch.Type() == "attribute" {
				if a := ch.ChildByFieldName("attribute"); a != nil {
					return nodeText(a, src), false
				}
			}
		}
	}
	return "", false
}

// receiverClass infers the class name of a method-call receiver when the
// receiver expression makes the type locally evident. Returns "" when no
// reliable inference is possible — the caller will then emit a bare-name
// edge tagged with disposition_hint=ambiguous.
//
// Recognised shapes:
//
//	self                 — caller's enclosing class (parentClass)
//	ClassName()          — constructor call result; receiver type = ClassName
//	(expr)               — unwrap parenthesised expressions and retry
//
// Anything else (free identifiers, attribute chains, subscripts, etc.) is
// deliberately left unresolved here: making a guess would risk binding edges
// to the wrong class. The resolver's name-collision logic is a safer place
// to disambiguate those cases.
func receiverClass(recv ts.Node, src []byte, parentClass string, localRecv map[string]string) string {
	if recv == nil {
		return ""
	}
	switch recv.Type() {
	case "identifier":
		text := nodeText(recv, src)
		if text == "self" || text == "cls" {
			return parentClass
		}
		// Issue #4681 — local-variable receiver typing. A local bound from a
		// constructor (`v = ProposalViewSet()`) is a lowercase identifier the
		// PascalCase heuristic below never matches; consult the per-body local
		// map first so `v.get_counts()` resolves to `ProposalViewSet.get_counts`.
		if cls := localRecv[text]; cls != "" {
			return cls
		}
		// Issue #557 — bare PascalCase/CamelCase identifier as receiver, e.g.
		// `User.save(...)`, `Article.objects.create(...)`.  Python convention
		// reserves TitleCase identifiers for class names (PEP 8).  When the
		// first rune is uppercase we treat the identifier as a class-name
		// reference and qualify the call site: `User.save` instead of the bare
		// ambiguous `save`. This is intentionally conservative — all-lowercase
		// identifiers (module aliases, instance variables) are left unresolved
		// so we never bind to the wrong class.  SCREAMING_SNAKE constants are
		// also excluded because their first rune is uppercase but they are
		// clearly not class names (e.g. `ALLOWED_HOSTS.append`).
		if isPascalCaseClassName(text) {
			return text
		}
		return ""
	case "call":
		// e.g. B().foo() — function child is the constructor identifier.
		ctor := recv.ChildByFieldName("function")
		if ctor != nil && ctor.Type() == "identifier" {
			return nodeText(ctor, src)
		}
		return ""
	case "parenthesized_expression":
		for i := 0; i < int(recv.ChildCount()); i++ {
			ch := recv.Child(i)
			if ch.IsNamed() {
				return receiverClass(ch, src, parentClass, localRecv)
			}
		}
	}
	return ""
}

// isPascalCaseClassName reports whether name looks like a Python class name
// by PEP 8 convention: starts with an uppercase letter and contains at least
// one lowercase letter (to exclude SCREAMING_SNAKE_CASE constants and single
// uppercase letters used as type variables). Examples:
//
//	User            → true   (class name)
//	ArticleManager  → true   (class name)
//	BaseViewSet     → true   (class name)
//	user            → false  (instance variable / module alias)
//	ALLOWED_HOSTS   → false  (module-level constant)
//	T               → false  (single-letter type variable)
func isPascalCaseClassName(name string) bool {
	if name == "" {
		return false
	}
	first := rune(name[0])
	if first < 'A' || first > 'Z' {
		return false
	}
	// Must contain at least one lowercase letter so that SCREAMING_SNAKE and
	// single-uppercase-letter type vars are excluded.
	hasLower := false
	for _, r := range name[1:] {
		if r >= 'a' && r <= 'z' {
			hasLower = true
			break
		}
	}
	return hasLower
}

// extractImports walks the parse tree for top-level Python import statements
// and returns one SCOPE.Component entity per imported module path. Each
// entity carries one IMPORTS relationship (FromID=file path, ToID=module).
//
// Two grammar shapes are handled:
//
//	import a, b.c [as alias]                     → import_statement
//	from x.y import a, b [as alias]              → import_from_statement
//
// `from x import a` creates one entity per imported name with module path
// "x.a", matching the symbol-level granularity Python uses for cross-module
// resolution.
//
// Issue #93 — every IMPORTS relationship now carries the metadata the
// cross-file resolver needs to bind a bare CALLS target back to its real
// entity:
//
//	Properties["local_name"]    — the identifier as referenced inside the
//	                              importing file (alias when present, else
//	                              the imported leaf name; for `import a.b`
//	                              this is the top-level package "a").
//	Properties["source_module"] — the dotted module path the symbol was
//	                              imported from. For `import x.y` this is
//	                              "x.y"; for `from x import y` this is "x".
//	Properties["imported_name"] — the original (pre-alias) leaf identifier
//	                              inside the source module. Equal to
//	                              local_name when no alias is present.
//	Properties["wildcard"]      — "1" when the import is `from x import *`.
func extractImports(root ts.Node, file extractor.FileInput) []types.EntityRecord {
	if root == nil {
		return nil
	}
	var out []types.EntityRecord
	for _, n := range findAll(root, "import_statement") {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			path, alias := dottedNameAndAlias(ch, file.Content)
			if path == "" {
				continue
			}
			localName := alias
			if localName == "" {
				if dot := strings.IndexByte(path, '.'); dot > 0 {
					localName = path[:dot]
				} else {
					localName = path
				}
			}
			props := map[string]string{
				"local_name":    localName,
				"source_module": path,
				"imported_name": path,
			}
			out = append(out, importRecord(path, file, props))
		}
	}
	for _, n := range findAll(root, "import_from_statement") {
		modNode := n.ChildByFieldName("module_name")
		// Issue #1694 — resolve relative imports (`from . import x`,
		// `from ..pkg import y`) to their absolute dotted module form
		// using the current file's path. Non-relative imports return
		// the literal source text via dottedNamePath, matching prior
		// behaviour exactly. This makes the resolver's `(module, leaf)`
		// reverse index bind relative-import callsites without any
		// extra resolution layer.
		modPath := resolvePythonImportModule(modNode, file)
		if modPath == "" {
			continue
		}
		// Imported names — child types: dotted_name, aliased_import,
		// wildcard_import. We collect each as "<module>.<name>" so the
		// resolver can pick up the leaf symbol.
		emittedAny := false
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			if ch == modNode {
				continue
			}
			name, alias := dottedNameAndAlias(ch, file.Content)
			if name == "" {
				continue
			}
			if name == "*" {
				out = append(out, importRecord(modPath, file, map[string]string{
					"source_module": modPath,
					"wildcard":      "1",
				}))
				emittedAny = true
				continue
			}
			localName := alias
			if localName == "" {
				localName = name
			}
			props := map[string]string{
				"local_name":    localName,
				"source_module": modPath,
				"imported_name": name,
			}
			out = append(out, importRecord(modPath+"."+name, file, props))
			emittedAny = true
		}
		if !emittedAny {
			out = append(out, importRecord(modPath, file, map[string]string{
				"source_module": modPath,
			}))
		}
	}
	return out
}

// dottedNameAndAlias returns (path, alias) for an import-list child node.
// path is the dotted import path stripped of any "as <alias>" suffix; alias
// is the binding identifier introduced by `as` (or "" when not present).
// Wildcards return ("*", ""). Unrecognised shapes return ("", "").
func dottedNameAndAlias(node ts.Node, src []byte) (string, string) {
	if node == nil {
		return "", ""
	}
	if node.Type() != "aliased_import" {
		return dottedNamePath(node, src), ""
	}
	var path, alias string
	if name := node.ChildByFieldName("name"); name != nil {
		path = dottedNamePath(name, src)
	}
	if a := node.ChildByFieldName("alias"); a != nil {
		alias = strings.TrimSpace(nodeText(a, src))
	}
	if path == "" && node.NamedChildCount() > 0 {
		path = dottedNamePath(node.NamedChild(0), src)
	}
	return path, alias
}

// dottedNamePath flattens a dotted_name / identifier / aliased_import node into
// its source-text path. Aliases are stripped (only the underlying name is used).
// Returns "" for unrecognised node shapes.
func dottedNamePath(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	switch node.Type() {
	case "identifier", "dotted_name":
		return strings.TrimSpace(nodeText(node, src))
	case "aliased_import":
		if name := node.ChildByFieldName("name"); name != nil {
			return dottedNamePath(name, src)
		}
		if node.NamedChildCount() > 0 {
			return dottedNamePath(node.NamedChild(0), src)
		}
	case "relative_import":
		// e.g. ".foo" or "..bar" — keep raw text so the resolver can match.
		return strings.TrimSpace(nodeText(node, src))
	case "wildcard_import":
		return "*"
	}
	return ""
}

// importRecord builds a single SCOPE.Component entity for the given module
// path with one embedded IMPORTS relationship. Properties on the IMPORTS
// edge carry the import-binding metadata the cross-file resolver consumes
// (issue #93): local_name, source_module, imported_name, and wildcard.
func importRecord(modulePath string, file extractor.FileInput, props map[string]string) types.EntityRecord {
	return types.EntityRecord{
		Name:       modulePath,
		Kind:       "SCOPE.Component",
		Subtype:    "module",
		SourceFile: file.Path,
		Language:   "python",
		Relationships: []types.RelationshipRecord{
			{
				FromID:     file.Path,
				ToID:       modulePath,
				Kind:       "IMPORTS",
				Properties: props,
			},
		},
	}
}

// extractClassFields walks the immediate children of a class body and emits
// one SCOPE.Schema/field entity per class-attribute assignment. Issue #526.
//
// Recognised shapes (all at class body scope, NOT inside a def):
//
//	serializer_class = ArticleSerializer          # DRF ViewSet
//	queryset         = Article.objects.all()      # DRF
//	model            = User                       # Django ModelForm
//	fields           = ['title', 'body']          # DRF Serializer.Meta
//	permission_classes = [IsAuthenticated]        # DRF
//	id = Column(Integer, primary_key=True)        # SQLAlchemy declarative
//	title = models.CharField(max_length=200)      # Django models
//
// Also handles annotated assignments (PEP 526):
//
//	serializer_class: type[Serializer] = ArticleSerializer
//	count: int = 0
//
// Multi-target assignments (`a = b = expr`, `a, b = (1, 2)`) emit one entity
// per left-hand `identifier`. Tuple/list/subscript/attribute targets that
// aren't a bare class-scope name are skipped — those don't correspond to a
// new attribute declaration on the class.
//
// Dunder names (`__slots__`, `__qualname__`, etc.) and underscore-only names
// are skipped: they don't appear as `self.<name>` references in user code.
//
// The Name field is emitted as "<dottedClass>.<attr>" so the resolver's
// byMember[<file>][<class>][<attr>] index picks the entity up directly,
// binding CALLS edges like `self.serializer_class(...)` → this field.
//
// Field declarations are only emitted at the immediate class-body scope.
// Walker recursion into method bodies, nested classes, and conditional
// blocks (`if X: y = ...`) is intentionally skipped — those are not
// stable class attributes a resolver can rely on, and emitting them would
// risk over-eager binding.
// pyFieldSignature builds a class-field signature for the dashboard shape
// resolver. When a PEP-526 annotation is present it emits the `name: type`
// convention (e.g. `effectiveAt: datetime | None`) the shape parser
// understands; otherwise it emits the bare name (no inferred type). A trailing
// `| None` / `Optional[...]` annotation lets the resolver mark the field
// nullable. (#4868)
func pyFieldSignature(name, typeAnn string) string {
	if typeAnn = strings.TrimSpace(typeAnn); typeAnn == "" {
		return name
	}
	return name + ": " + typeAnn
}

func extractClassFields(
	body ts.Node,
	file extractor.FileInput,
	parentClass string,
	out *[]types.EntityRecord,
) {
	if body == nil || parentClass == "" {
		return
	}
	seen := make(map[string]bool)
	for i := 0; i < int(body.ChildCount()); i++ {
		stmt := body.Child(i)
		if stmt == nil {
			continue
		}
		if stmt.Type() != "expression_statement" {
			continue
		}
		// expression_statement → first named child is the assignment.
		for j := 0; j < int(stmt.NamedChildCount()); j++ {
			expr := stmt.NamedChild(j)
			if expr == nil {
				continue
			}
			var lhs ts.Node
			var typeAnn string
			switch expr.Type() {
			case "assignment":
				lhs = expr.ChildByFieldName("left")
				// #4868 — capture the PEP-526 annotation on `x: int = 0` (the
				// assignment's "type" field) so the dashboard shape row shows
				// the real field type (and `| None` nullability) instead of
				// "unknown". The shape parser reads the `name: type` convention
				// emitted in the Signature below.
				if tn := expr.ChildByFieldName("type"); tn != nil {
					typeAnn = strings.TrimSpace(nodeText(tn, file.Content))
				}
			case "augmented_assignment":
				// `count += 1` at class scope doesn't introduce a new
				// attribute — skip.
				continue
			default:
				continue
			}
			for _, name := range classAttrLHSNames(lhs, file.Content) {
				if name == "" || seen[name] {
					continue
				}
				if skipClassAttrName(name) {
					continue
				}
				seen[name] = true
				// Issue #1725 — derive QualifiedName so SCOPE.Schema/field
				// entities are not 100% empty-qn. Format mirrors the parent
				// class QN: "<module>.<parentClass>.<field>" (or the bare
				// dotted form when filePathToModule cannot derive a module).
				qualName := parentClass + "." + name
				if mod := filePathToModule(file.Path); mod != "" {
					qualName = mod + "." + parentClass + "." + name
				}
				*out = append(*out, types.EntityRecord{
					Name:          parentClass + "." + name,
					QualifiedName: qualName,
					Kind:          "SCOPE.Schema",
					Subtype:       "field",
					Language:      "python",
					SourceFile:    file.Path,
					StartLine:     int(stmt.StartPoint().Row) + 1,
					EndLine:       int(stmt.EndPoint().Row) + 1,
					Signature:     pyFieldSignature(name, typeAnn),
				})
			}
		}
	}
}

// classAttrLHSNames extracts the set of bare-identifier targets on the
// left-hand side of a class-scope assignment. Recognised shapes:
//
//	x              → ["x"]                          (simple)
//	x: int         → ["x"]                          (PEP 526 annotated; LHS is
//	                                                 typed_default_parameter or
//	                                                 "identifier" depending on
//	                                                 grammar version — both
//	                                                 reduce to the leaf name)
//	a = b = expr   → ["a"] then resolver recurses   (chained assignments at
//	                                                 the tree-sitter grammar
//	                                                 level appear as nested
//	                                                 `assignment` nodes inside
//	                                                 the right field; handled
//	                                                 by the caller's
//	                                                 ChildByFieldName loop on
//	                                                 the outer assignment)
//	a, b           → ["a", "b"]                     (tuple/list pattern)
//	self.x         → []                             (attribute LHS — not a
//	                                                 class attribute)
//	x[0]           → []                             (subscript LHS)
func classAttrLHSNames(lhs ts.Node, src []byte) []string {
	if lhs == nil {
		return nil
	}
	switch lhs.Type() {
	case "identifier":
		return []string{nodeText(lhs, src)}
	case "pattern_list", "tuple_pattern", "list_pattern":
		var names []string
		for i := 0; i < int(lhs.NamedChildCount()); i++ {
			ch := lhs.NamedChild(i)
			if ch != nil && ch.Type() == "identifier" {
				names = append(names, nodeText(ch, src))
			}
		}
		return names
	}
	return nil
}

// skipClassAttrName filters dunder / private-by-convention names that should
// not be emitted as class-attribute entities. These either have special
// runtime semantics (`__slots__`, `__qualname__`, `__doc__`) or are pure
// implementation noise (`_`, `__`) that no resolver should bind to.
func skipClassAttrName(name string) bool {
	if name == "" || name == "_" || name == "__" {
		return true
	}
	if strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__") {
		return true
	}
	return false
}

// applyFrameworkInnerClassProperties examines innerClass to determine whether
// it is a known framework configuration class (Django / DRF Meta, Pydantic
// Config) and, if so, parses its body for well-known key–value assignments and
// propagates them as Properties on the parent class entity in-place.
//
// Recognised inner-class names (bare suffix after the last "."):
//
//	Meta   — Django models.Model, Django Form, DRF Serializer/ViewSet
//	Config — Pydantic BaseModel / BaseSettings
//
// Propagated properties (Django Meta):
//
//	abstract        = True         → parent.Properties["is_abstract"] = "true"
//	db_table        = "orders"     → parent.Properties["db_table"]    = "orders"
//	ordering        = ["-created"] → parent.Properties["ordering"]    = ["-created"]
//	unique_together = [...]        → parent.Properties["unique_together"] = …
//	app_label       = "shop"       → parent.Properties["app_label"]   = "shop"
//
// Propagated properties (Pydantic Config):
//
//	orm_mode = True  → parent.Properties["orm_mode"] = "true"
//	env_prefix = "X" → parent.Properties["env_prefix"] = "X"
//	alias_generator = …  → parent.Properties["alias_generator"] = "true"
//
// The childParent parameter is the dotted class path of the enclosing class
// (e.g. "Order") so we can detect the Meta suffix via strings.HasSuffix rather
// than relying on the inner class's full dotted Name ("Order.Meta").
//
// Issue #757 — extractor-side mutation (Option A): properties are set on the
// parent entity slice element before pipeline emission. No new edge kinds.
func applyFrameworkInnerClassProperties(
	parent *types.EntityRecord,
	innerClass *types.EntityRecord,
	classBody ts.Node,
	src []byte,
	childParent string,
) {
	if parent == nil || innerClass == nil || classBody == nil {
		return
	}

	// Compute the bare name of the inner class (after the last ".").
	innerName := innerClass.Name
	if dot := strings.LastIndexByte(innerName, '.'); dot >= 0 {
		innerName = innerName[dot+1:]
	}

	isMeta := innerName == "Meta"
	isConfig := innerName == "Config"
	if !isMeta && !isConfig {
		return
	}

	// Find the class_definition node inside classBody that matches innerClass
	// by start line so we can walk its body for key-value assignments.
	var innerBody ts.Node
	for i := range int(classBody.ChildCount()) {
		ch := classBody.Child(i)
		if ch == nil {
			continue
		}
		// The inner class may be a bare class_definition or wrapped in a
		// decorated_definition. In practice Meta/Config are never decorated,
		// but handle both for robustness.
		var candidate ts.Node
		switch ch.Type() {
		case "class_definition":
			candidate = ch
		case "decorated_definition":
			if inner := ch.ChildByFieldName("definition"); inner != nil && inner.Type() == "class_definition" {
				candidate = inner
			}
		}
		if candidate == nil {
			continue
		}
		nameNode := candidate.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		if nodeText(nameNode, src) == innerName {
			innerBody = candidate.ChildByFieldName("body")
			break
		}
	}
	if innerBody == nil {
		return
	}

	// Collect key = value assignments from the inner class body.
	// We only care about simple identifier = expr pairs at the immediate body
	// scope; nested or complex assignments are skipped.
	props := parseSimpleAssignments(innerBody, src)
	if len(props) == 0 {
		return
	}

	if parent.Properties == nil {
		parent.Properties = make(map[string]string, len(props))
	}

	if isMeta {
		// Django/DRF Meta keys.
		if v, ok := props["abstract"]; ok {
			if strings.EqualFold(v, "True") || v == "1" {
				parent.Properties["is_abstract"] = "true"
			}
		}
		if v, ok := props["db_table"]; ok {
			parent.Properties["db_table"] = stripQuotes(v)
		}
		if v, ok := props["ordering"]; ok {
			parent.Properties["ordering"] = v
		}
		if v, ok := props["unique_together"]; ok {
			parent.Properties["unique_together"] = v
		}
		if v, ok := props["app_label"]; ok {
			parent.Properties["app_label"] = stripQuotes(v)
		}
		// DRF Serializer.Meta fields / model.
		if v, ok := props["fields"]; ok {
			parent.Properties["meta_fields"] = v
		}
		if v, ok := props["model"]; ok {
			parent.Properties["meta_model"] = v
		}
	}

	if isConfig {
		// Pydantic Config keys.
		if v, ok := props["orm_mode"]; ok {
			if strings.EqualFold(v, "True") || v == "1" {
				parent.Properties["orm_mode"] = "true"
			}
		}
		if v, ok := props["env_prefix"]; ok {
			parent.Properties["env_prefix"] = stripQuotes(v)
		}
		if _, ok := props["alias_generator"]; ok {
			parent.Properties["alias_generator"] = "true"
		}
	}
}

// parseSimpleAssignments walks the immediate children of body and returns a
// map[identifier]rawValue for every simple `identifier = expr` assignment. The
// value is the raw source text of the right-hand side, trimmed of whitespace.
// Only the top-level body scope is examined; nested blocks are skipped.
func parseSimpleAssignments(body ts.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	out := make(map[string]string)
	for i := range int(body.ChildCount()) {
		stmt := body.Child(i)
		if stmt == nil {
			continue
		}
		// expression_statement → assignment
		if stmt.Type() != "expression_statement" {
			continue
		}
		for j := range int(stmt.NamedChildCount()) {
			expr := stmt.NamedChild(j)
			if expr == nil || expr.Type() != "assignment" {
				continue
			}
			lhs := expr.ChildByFieldName("left")
			rhs := expr.ChildByFieldName("right")
			if lhs == nil || rhs == nil {
				continue
			}
			if lhs.Type() != "identifier" {
				continue
			}
			key := nodeText(lhs, src)
			val := strings.TrimSpace(nodeText(rhs, src))
			if key != "" && val != "" {
				out[key] = val
			}
		}
	}
	return out
}

// stripQuotes removes surrounding single or double quotes from a Python string
// literal value. Returns the input unchanged when the value is not quoted.
func stripQuotes(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// findAll returns every descendant of root whose Type() matches kind.
// Recursion is iterative to stay safe on deeply-nested trees.
func findAll(root ts.Node, kind string) []ts.Node {
	if root == nil {
		return nil
	}
	var out []ts.Node
	stack := []ts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == kind {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

// extractMetaConstraintEntities parses the `constraints = [...]` assignment
// inside a Django Model.Meta inner class body and emits a SCOPE.Constraint
// entity for each UniqueConstraint / CheckConstraint entry that carries a
// `name=` keyword argument. It also appends CONTAINS relationships from the
// parent class entity (at index classIdx in *out) to each emitted constraint.
//
// Issue #749 — previously these Meta.constraints declarations either produced
// no entities (when the Meta body was only parsed for properties) or produced
// orphan Constraint entities (from the SQLAlchemy YAML rule misfiring on Django
// model files). This function closes the gap by emitting them directly from the
// Python extractor with proper CONTAINS edges.
//
// Parameters:
//
//	metaClassBody  — the class body sitter.Node of the enclosing class (not the
//	                 Meta body itself — we search downward for the Meta assignment)
//	file           — the source file input
//	innerClassName — the full dotted name of the Meta inner class (e.g. "Order.Meta")
//	parentClass    — the name of the enclosing parent class (e.g. "Order")
//	classIdx       — index of the parent class entity in *out
//	out            — the shared entity accumulator
//
// The function is a no-op when innerClassName does not end in ".Meta" or when
// no `constraints = [...]` assignment is present in the Meta body.
func extractMetaConstraintEntities(
	metaClassBody ts.Node,
	file extractor.FileInput,
	innerClassName string,
	parentClass string,
	classIdx int,
	out *[]types.EntityRecord,
) {
	// Only act on inner classes whose bare name is "Meta".
	bareName := innerClassName
	if dot := strings.LastIndexByte(bareName, '.'); dot >= 0 {
		bareName = bareName[dot+1:]
	}
	if bareName != "Meta" {
		return
	}

	// Locate the Meta class_definition node inside metaClassBody so we can
	// walk its body for the `constraints = [...]` assignment.
	var metaBody ts.Node
	for i := range int(metaClassBody.ChildCount()) {
		ch := metaClassBody.Child(i)
		if ch == nil {
			continue
		}
		var candidate ts.Node
		switch ch.Type() {
		case "class_definition":
			candidate = ch
		case "decorated_definition":
			if inner := ch.ChildByFieldName("definition"); inner != nil && inner.Type() == "class_definition" {
				candidate = inner
			}
		}
		if candidate == nil {
			continue
		}
		nameNode := candidate.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		if nodeText(nameNode, file.Content) == "Meta" {
			metaBody = candidate.ChildByFieldName("body")
			break
		}
	}
	if metaBody == nil {
		return
	}

	// Walk the Meta body for `constraints = [...]` assignments.
	for i := range int(metaBody.ChildCount()) {
		stmt := metaBody.Child(i)
		if stmt == nil {
			continue
		}
		if stmt.Type() != "expression_statement" {
			continue
		}
		for j := range int(stmt.NamedChildCount()) {
			expr := stmt.NamedChild(j)
			if expr == nil || expr.Type() != "assignment" {
				continue
			}
			lhs := expr.ChildByFieldName("left")
			rhs := expr.ChildByFieldName("right")
			if lhs == nil || rhs == nil {
				continue
			}
			if lhs.Type() != "identifier" || nodeText(lhs, file.Content) != "constraints" {
				continue
			}
			// Found `constraints = <rhs>`. Walk rhs for UniqueConstraint /
			// CheckConstraint call nodes and extract their `name=` argument.
			constraintCalls := findAll(rhs, "call")
			for _, call := range constraintCalls {
				funcNode := call.ChildByFieldName("function")
				if funcNode == nil {
					continue
				}
				funcText := nodeText(funcNode, file.Content)
				// Accept models.UniqueConstraint, models.CheckConstraint,
				// UniqueConstraint, CheckConstraint (with or without module prefix).
				isConstraintCall := strings.HasSuffix(funcText, "UniqueConstraint") ||
					strings.HasSuffix(funcText, "CheckConstraint")
				if !isConstraintCall {
					continue
				}
				// Extract the `name=` keyword argument value.
				argsNode := call.ChildByFieldName("arguments")
				if argsNode == nil {
					continue
				}
				var constraintName string
				for a := range int(argsNode.ChildCount()) {
					arg := argsNode.Child(a)
					if arg == nil || arg.Type() != "keyword_argument" {
						continue
					}
					keyNode := arg.ChildByFieldName("name")
					valNode := arg.ChildByFieldName("value")
					if keyNode == nil || valNode == nil {
						continue
					}
					if nodeText(keyNode, file.Content) == "name" {
						constraintName = stripQuotes(strings.TrimSpace(nodeText(valNode, file.Content)))
						break
					}
				}
				if constraintName == "" {
					// No `name=` found — skip (anonymous constraints can't be
					// stably identified; leave them unlinked).
					continue
				}

				// Qualified constraint name: "ParentClass.constraint_name" so
				// multiple models in the same file don't collide.
				qualifiedName := parentClass + "." + constraintName

				// Determine whether this is UniqueConstraint or CheckConstraint.
				constraintSubtype := "check"
				if strings.HasSuffix(funcText, "UniqueConstraint") {
					constraintSubtype = "unique"
				}

				// Emit the SCOPE.Constraint entity.
				// Issue #1966 — set Language explicitly to "python" instead
				// of relying on file.Language (which is unset on some caller
				// paths and would emit an empty Language string).
				constRec := types.EntityRecord{
					Name:       qualifiedName,
					Kind:       "SCOPE.Constraint",
					Subtype:    constraintSubtype,
					SourceFile: file.Path,
					Language:   "python",
					StartLine:  int(call.StartPoint().Row) + 1,
					EndLine:    int(call.EndPoint().Row) + 1,
					Properties: map[string]string{
						"pattern_type":    "django_meta_constraint",
						"constraint_name": constraintName,
						"constraint_kind": funcText[strings.LastIndex(funcText, ".")+1:],
					},
					QualityScore: 0.8,
				}
				*out = append(*out, constRec)

				// Emit CONTAINS edge from parent class to this constraint.
				// Use a structural-ref ToID so the resolver can bind the stub
				// via byLocation[file][qualifiedName].
				toID := extractor.BuildConstraintStructuralRef(file.Path, qualifiedName)
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
	}
}

// nodeText returns the raw source bytes for a tree-sitter node as a string.
func nodeText(node ts.Node, src []byte) string {
	return string(src[node.StartByte():node.EndByte()])
}
