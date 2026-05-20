// Package java — Quarkus Panache static-method synthesizer.
//
// Issue #804 (sub-story (b) of #787): when the Java extractor encounters a
// class that extends PanacheEntity / PanacheEntityBase, or implements
// PanacheRepository<T> / PanacheRepositoryBase<T,ID>, it calls
// synthesizePanacheEntities which produces SCOPE.Operation entities for every
// method that Panache provides at runtime via the external JAR. Without these
// entities every call to Book.findById(1L), Book.count(), book.persist() etc.
// lands as a bug-extractor unresolved reference.
//
// Issue #818 (sub-story (d) of #787): extends #809 with DSL builder method
// synthesis for PanacheQuery and PanacheUpdate. After #809 resolves static
// methods on entity classes (Order.find, Order.count, etc.), the returned
// PanacheQuery object's chained DSL calls (.list(), .page(), .stream(), etc.)
// remain unresolved because PanacheQuery is an external interface. This file
// adds synthesizePanacheDSLEntities which emits one SCOPE.Component:PanacheQuery
// interface entity and one SCOPE.Operation per DSL method, enabling the
// resolver to bind `q.list()`, `q.page(0,20)`, `q.count()`, etc.
//
// Design mirrors the Lombok synthesizer (lombok.go / #799):
//   - Synthesized entities carry pattern_type and synthesized_from in
//     Properties so downstream consumers can distinguish them from
//     extractor-emitted real entities.
//   - QualityScore = 0.7 (real extractor entities are 1.0).
//   - Reactive Panache entities are tagged reactive="true".
//   - All entities tagged language="java" (TagRelationshipsLanguage runs
//     in the main Extract loop, so relationships are stamped too).
//
// Panache surfaces covered:
//   - io.quarkus.hibernate.orm.panache.PanacheEntity (SQL ORM, static methods)
//   - io.quarkus.hibernate.orm.panache.PanacheEntityBase (SQL ORM, static methods)
//   - io.quarkus.hibernate.orm.panache.PanacheRepository<T>
//   - io.quarkus.hibernate.orm.panache.PanacheRepositoryBase<T,ID>
//   - io.quarkus.hibernate.reactive.panache.PanacheEntity (Reactive)
//   - io.quarkus.hibernate.reactive.panache.PanacheEntityBase (Reactive)
//   - io.quarkus.hibernate.reactive.panache.PanacheRepository<T> (Reactive)
//   - io.quarkus.mongodb.panache.PanacheMongoEntity
//   - io.quarkus.mongodb.panache.PanacheMongoEntityBase
//   - io.quarkus.mongodb.panache.PanacheMongoRepository<T>
//   - io.quarkus.mongodb.panache.reactive.ReactivePanacheMongoEntity
//   - io.quarkus.mongodb.panache.reactive.ReactivePanacheMongoRepository<T>
//
// Additionally synthesizes entities for:
//   - @NamedQuery annotations on entity classes
//   - Panache projection calls (project(MyDto.class))
//   - PanacheQuery DSL methods (#818): list, stream, page, count, etc.
//   - PanacheUpdate DSL methods (#818): where, whereOptional
//   - Reactive variants: ReactivePanacheQuery with Uni/Multi return shapes
package java

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// panacheSynthQuality matches lombokSynthQuality — below real entities (1.0)
// so the indexer dedup layer prefers a real entity if one is ever also parsed.
const panacheSynthQuality = 0.7

// panacheVariant describes which Panache surface a class uses.
type panacheVariant int

const (
	panacheNone          panacheVariant = iota
	panacheSQLEntity                    // extends PanacheEntity / PanacheEntityBase
	panacheSQLRepository                // implements PanacheRepository / PanacheRepositoryBase
	panacheReactiveEntity
	panacheReactiveRepository
	panacheMongoEntity
	panacheMongoRepository
	panacheReactiveMongoEntity
	panacheReactiveMongoRepository
)

// collectRawImports returns the concatenated import declarations from a Java
// source file as a single string. Used by detectPanache to identify which
// Panache variant is in scope without needing the full file content in the
// detection function.
func collectRawImports(src []byte) string {
	var b strings.Builder
	for _, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "import ") {
			b.WriteString(trimmed)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// panacheImportSets maps import package prefixes to the variant they unlock.
// We check import lines to determine which Panache flavour is in scope.
var panacheImportSets = []struct {
	prefix  string
	variant panacheVariant
}{
	{"io.quarkus.hibernate.reactive.panache", panacheReactiveEntity},
	{"io.quarkus.mongodb.panache.reactive", panacheReactiveMongoEntity},
	{"io.quarkus.mongodb.panache", panacheMongoEntity},
	{"io.quarkus.hibernate.orm.panache", panacheSQLEntity},
}

// panacheEntitySuperclasses is the set of simple names (and common short forms)
// that map to Panache entity base classes.
var panacheEntitySuperclasses = map[string]bool{
	"PanacheEntity":            true,
	"PanacheEntityBase":        true,
	"PanacheMongoEntity":       true,
	"PanacheMongoEntityBase":   true,
	"ReactivePanacheMongoEntity": true,
}

// panacheRepositoryInterfaces is the set of simple interface names that map to
// Panache repository interfaces. These are identified by implements clauses.
var panacheRepositoryInterfaces = map[string]bool{
	"PanacheRepository":            true,
	"PanacheRepositoryBase":        true,
	"PanacheMongoRepository":       true,
	"PanacheMongoRepositoryBase":   true,
	"ReactivePanacheMongoRepository": true,
}

// panacheClassInfo holds everything detectPanache extracts from a class declaration.
type panacheClassInfo struct {
	variant   panacheVariant
	isEntity  bool // true = static methods on the class; false = instance methods
	reactive  bool
}

// detectPanache analyses classDeclSrc (the class declaration text, annotations
// + declaration keywords up to the opening brace) and rawFileImports (the full
// import block text of the file) and returns a panacheClassInfo describing
// which Panache surface the class exposes. Returns panacheNone when the class
// is not a Panache entity or repository.
func detectPanache(classDeclSrc, rawFileImports string) panacheClassInfo {
	// Determine which Panache flavour the file imports.
	fileVariant := panacheNone
	isReactive := false
	for _, imp := range panacheImportSets {
		if strings.Contains(rawFileImports, imp.prefix) {
			fileVariant = imp.variant
			if imp.variant == panacheReactiveEntity || imp.variant == panacheReactiveMongoEntity {
				isReactive = true
			}
			break
		}
	}

	// Check extends clause for entity base classes.
	if superclass := extractExtends(classDeclSrc); superclass != "" {
		if panacheEntitySuperclasses[superclass] {
			v := resolveEntityVariant(superclass, fileVariant, isReactive)
			return panacheClassInfo{variant: v, isEntity: true, reactive: isReactive}
		}
	}

	// Check implements clause for repository interfaces.
	for _, iface := range extractImplements(classDeclSrc) {
		if panacheRepositoryInterfaces[iface] {
			v := resolveRepositoryVariant(iface, fileVariant, isReactive)
			return panacheClassInfo{variant: v, isEntity: false, reactive: isReactive}
		}
	}

	return panacheClassInfo{variant: panacheNone}
}

// resolveEntityVariant picks the most specific panacheVariant for an entity
// given the imported package context and the superclass name.
func resolveEntityVariant(superclass string, fileVariant panacheVariant, reactive bool) panacheVariant {
	isMongo := strings.Contains(superclass, "Mongo") || fileVariant == panacheMongoEntity || fileVariant == panacheReactiveMongoEntity
	if isMongo {
		if reactive || strings.HasPrefix(superclass, "Reactive") {
			return panacheReactiveMongoEntity
		}
		return panacheMongoEntity
	}
	if reactive || fileVariant == panacheReactiveEntity {
		return panacheReactiveEntity
	}
	return panacheSQLEntity
}

// resolveRepositoryVariant picks the most specific panacheVariant for a
// repository implementation given the imported package context.
func resolveRepositoryVariant(iface string, fileVariant panacheVariant, reactive bool) panacheVariant {
	isMongo := strings.Contains(iface, "Mongo") || fileVariant == panacheMongoRepository || fileVariant == panacheReactiveMongoRepository
	if isMongo {
		if reactive || strings.HasPrefix(iface, "Reactive") {
			return panacheReactiveMongoRepository
		}
		return panacheMongoRepository
	}
	if reactive || fileVariant == panacheReactiveRepository {
		return panacheReactiveRepository
	}
	return panacheSQLRepository
}

// extractExtends returns the simple name of the superclass from a class
// declaration text, or "" if not found. Handles both bare and generic forms:
//   "extends PanacheEntity" → "PanacheEntity"
//   "extends PanacheEntityBase<Long>" → "PanacheEntityBase"
func extractExtends(classDeclSrc string) string {
	const kw = "extends"
	idx := strings.Index(classDeclSrc, kw)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(classDeclSrc[idx+len(kw):])
	// Read identifier until whitespace, '<', '{', or 'implements'.
	end := 0
	for end < len(rest) && rest[end] != ' ' && rest[end] != '\t' &&
		rest[end] != '\n' && rest[end] != '<' && rest[end] != '{' {
		end++
	}
	name := rest[:end]
	// Strip any fully-qualified prefix (e.g. "io.quarkus.hibernate.orm.panache.PanacheEntity").
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[dot+1:]
	}
	return strings.TrimSpace(name)
}

// extractImplements returns the list of simple interface names from the
// implements clause of a class declaration text.
func extractImplements(classDeclSrc string) []string {
	const kw = "implements"
	idx := strings.Index(classDeclSrc, kw)
	if idx < 0 {
		return nil
	}
	rest := strings.TrimSpace(classDeclSrc[idx+len(kw):])
	// The implements clause ends at '{' or end of string.
	if brace := strings.IndexByte(rest, '{'); brace >= 0 {
		rest = rest[:brace]
	}
	// Split by comma; each token may be "SomeInterface<T, ID>".
	parts := strings.Split(rest, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		// Strip generic params.
		if lt := strings.IndexByte(p, '<'); lt >= 0 {
			p = strings.TrimSpace(p[:lt])
		}
		// Strip fully-qualified prefix.
		if dot := strings.LastIndexByte(p, '.'); dot >= 0 {
			p = p[dot+1:]
		}
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// panacheOp is a thin helper to build a Panache synthesized operation entity.
func panacheOp(
	ownerClass, methodName, signature, synthesizedFrom string,
	extraProps map[string]string,
) types.EntityRecord {
	props := map[string]string{
		"synthesized_from": synthesizedFrom,
		"pattern_type":     "panache_inherited_method",
		"owner":            ownerClass,
	}
	for k, v := range extraProps {
		props[k] = v
	}
	return types.EntityRecord{
		Name:         ownerClass + "." + methodName,
		Kind:         "SCOPE.Operation",
		Subtype:      "method",
		Language:     "java",
		Signature:    signature,
		QualityScore: panacheSynthQuality,
		Properties:   props,
	}
}

// sqlEntityStaticMethods returns the standard static-method surface for SQL
// Panache entity classes (PanacheEntity / PanacheEntityBase).
// These are all inherited static methods that the entity class can call
// directly: Book.findById(1L), Book.count(), etc.
func sqlEntityStaticMethods(className, synthFrom string) []types.EntityRecord {
	s := synthFrom
	isStatic := map[string]string{"is_static": "true"}
	return []types.EntityRecord{
		// findById variants
		panacheOp(className, "findById", className+" findById(Object id)", s, isStatic),
		panacheOp(className, "findById", className+" findById(Object id, LockModeType lockModeType)", s, isStatic),
		panacheOp(className, "findByIdOptional", "Optional<"+className+"> findByIdOptional(Object id)", s, isStatic),
		// find variants
		panacheOp(className, "find", "PanacheQuery<"+className+"> find(String query)", s, isStatic),
		panacheOp(className, "find", "PanacheQuery<"+className+"> find(String query, Object... params)", s, isStatic),
		panacheOp(className, "find", "PanacheQuery<"+className+"> find(String query, Sort sort)", s, isStatic),
		panacheOp(className, "find", "PanacheQuery<"+className+"> find(String query, Sort sort, Object... params)", s, isStatic),
		panacheOp(className, "find", "PanacheQuery<"+className+"> find(String query, Map<String,Object> params)", s, isStatic),
		panacheOp(className, "find", "PanacheQuery<"+className+"> find(String query, Parameters params)", s, isStatic),
		// findAll variants
		panacheOp(className, "findAll", "PanacheQuery<"+className+"> findAll()", s, isStatic),
		panacheOp(className, "findAll", "PanacheQuery<"+className+"> findAll(Sort sort)", s, isStatic),
		// list variants
		panacheOp(className, "list", "List<"+className+"> list(String query, Object... params)", s, isStatic),
		panacheOp(className, "list", "List<"+className+"> list(String query, Sort sort, Object... params)", s, isStatic),
		panacheOp(className, "list", "List<"+className+"> list(String query, Map<String,Object> params)", s, isStatic),
		panacheOp(className, "list", "List<"+className+"> list(String query, Parameters params)", s, isStatic),
		panacheOp(className, "listAll", "List<"+className+"> listAll()", s, isStatic),
		panacheOp(className, "listAll", "List<"+className+"> listAll(Sort sort)", s, isStatic),
		// stream variants
		panacheOp(className, "stream", "Stream<"+className+"> stream(String query, Object... params)", s, isStatic),
		panacheOp(className, "stream", "Stream<"+className+"> stream(String query, Sort sort, Object... params)", s, isStatic),
		panacheOp(className, "streamAll", "Stream<"+className+"> streamAll()", s, isStatic),
		panacheOp(className, "streamAll", "Stream<"+className+"> streamAll(Sort sort)", s, isStatic),
		// count variants
		panacheOp(className, "count", "long count()", s, isStatic),
		panacheOp(className, "count", "long count(String query, Object... params)", s, isStatic),
		panacheOp(className, "count", "long count(String query, Map<String,Object> params)", s, isStatic),
		panacheOp(className, "count", "long count(String query, Parameters params)", s, isStatic),
		// delete variants
		panacheOp(className, "delete", "long delete(String query, Object... params)", s, isStatic),
		panacheOp(className, "delete", "long delete(String query, Map<String,Object> params)", s, isStatic),
		panacheOp(className, "deleteById", "boolean deleteById(Object id)", s, isStatic),
		panacheOp(className, "deleteAll", "long deleteAll()", s, isStatic),
		// persist variants (static form for iterable/stream)
		panacheOp(className, "persist", "void persist(Iterable<"+className+"> entities)", s, isStatic),
		panacheOp(className, "persist", "void persist(Stream<"+className+"> entities)", s, isStatic),
		panacheOp(className, "persist", "void persist("+className+"... entities)", s, isStatic),
		// update
		panacheOp(className, "update", "int update(String query, Object... params)", s, isStatic),
		panacheOp(className, "update", "int update(String query, Map<String,Object> params)", s, isStatic),
		panacheOp(className, "update", "int update(String query, Parameters params)", s, isStatic),
		// project (projections)
		panacheOp(className, "project", "PanacheQuery<T> project(Class<T> type)", s, isStatic),
	}
}

// sqlEntityInstanceMethods returns the instance methods inherited from
// PanacheEntityBase that are available on entity instances (not static).
func sqlEntityInstanceMethods(className, synthFrom string) []types.EntityRecord {
	s := synthFrom
	return []types.EntityRecord{
		panacheOp(className, "persist", "void persist()", s, nil),
		panacheOp(className, "persistAndFlush", "void persistAndFlush()", s, nil),
		panacheOp(className, "delete", "void delete()", s, nil),
		panacheOp(className, "isPersistent", "boolean isPersistent()", s, nil),
		panacheOp(className, "flush", "void flush()", s, nil),
	}
}

// reactiveSQLEntityStaticMethods returns the Reactive Panache variant:
// same surface but wrapped in Uni<T> return types.
func reactiveSQLEntityStaticMethods(className, synthFrom string) []types.EntityRecord {
	s := synthFrom
	reactive := map[string]string{"is_static": "true", "reactive": "true"}
	return []types.EntityRecord{
		panacheOp(className, "findById", "Uni<"+className+"> findById(Object id)", s, reactive),
		panacheOp(className, "findByIdOptional", "Uni<Optional<"+className+">> findByIdOptional(Object id)", s, reactive),
		panacheOp(className, "find", "ReactivePanacheQuery<"+className+"> find(String query)", s, reactive),
		panacheOp(className, "find", "ReactivePanacheQuery<"+className+"> find(String query, Object... params)", s, reactive),
		panacheOp(className, "find", "ReactivePanacheQuery<"+className+"> find(String query, Sort sort)", s, reactive),
		panacheOp(className, "findAll", "ReactivePanacheQuery<"+className+"> findAll()", s, reactive),
		panacheOp(className, "findAll", "ReactivePanacheQuery<"+className+"> findAll(Sort sort)", s, reactive),
		panacheOp(className, "list", "Uni<List<"+className+">> list(String query, Object... params)", s, reactive),
		panacheOp(className, "listAll", "Uni<List<"+className+">> listAll()", s, reactive),
		panacheOp(className, "stream", "Multi<"+className+"> stream(String query, Object... params)", s, reactive),
		panacheOp(className, "streamAll", "Multi<"+className+"> streamAll()", s, reactive),
		panacheOp(className, "count", "Uni<Long> count()", s, reactive),
		panacheOp(className, "count", "Uni<Long> count(String query, Object... params)", s, reactive),
		panacheOp(className, "delete", "Uni<Long> delete(String query, Object... params)", s, reactive),
		panacheOp(className, "deleteById", "Uni<Boolean> deleteById(Object id)", s, reactive),
		panacheOp(className, "deleteAll", "Uni<Long> deleteAll()", s, reactive),
		panacheOp(className, "persist", "Uni<Void> persist(Iterable<"+className+"> entities)", s, reactive),
		panacheOp(className, "update", "Uni<Integer> update(String query, Object... params)", s, reactive),
	}
}

// reactiveSQLEntityInstanceMethods returns Reactive Panache instance methods.
func reactiveSQLEntityInstanceMethods(className, synthFrom string) []types.EntityRecord {
	s := synthFrom
	reactive := map[string]string{"reactive": "true"}
	return []types.EntityRecord{
		panacheOp(className, "persist", "Uni<Void> persist()", s, reactive),
		panacheOp(className, "persistAndFlush", "Uni<Void> persistAndFlush()", s, reactive),
		panacheOp(className, "delete", "Uni<Void> delete()", s, reactive),
		panacheOp(className, "isPersistent", "boolean isPersistent()", s, reactive),
		panacheOp(className, "flush", "Uni<Void> flush()", s, reactive),
	}
}

// mongoEntityStaticMethods returns the MongoDB Panache static-method surface.
// The MongoDB variant is nearly identical to SQL but uses different query
// parameter types (BSON Document, filters, etc.) and has slightly different
// method names.
func mongoEntityStaticMethods(className, synthFrom string) []types.EntityRecord {
	s := synthFrom
	isStatic := map[string]string{"is_static": "true"}
	return []types.EntityRecord{
		panacheOp(className, "findById", className+" findById(Object id)", s, isStatic),
		panacheOp(className, "findByIdOptional", "Optional<"+className+"> findByIdOptional(Object id)", s, isStatic),
		panacheOp(className, "find", "PanacheQuery<"+className+"> find(String query)", s, isStatic),
		panacheOp(className, "find", "PanacheQuery<"+className+"> find(String query, Object... params)", s, isStatic),
		panacheOp(className, "find", "PanacheQuery<"+className+"> find(String query, Sort sort)", s, isStatic),
		panacheOp(className, "find", "PanacheQuery<"+className+"> find(Document query)", s, isStatic),
		panacheOp(className, "findAll", "PanacheQuery<"+className+"> findAll()", s, isStatic),
		panacheOp(className, "findAll", "PanacheQuery<"+className+"> findAll(Sort sort)", s, isStatic),
		panacheOp(className, "list", "List<"+className+"> list(String query, Object... params)", s, isStatic),
		panacheOp(className, "listAll", "List<"+className+"> listAll()", s, isStatic),
		panacheOp(className, "stream", "Stream<"+className+"> stream(String query, Object... params)", s, isStatic),
		panacheOp(className, "streamAll", "Stream<"+className+"> streamAll()", s, isStatic),
		panacheOp(className, "count", "long count()", s, isStatic),
		panacheOp(className, "count", "long count(String query, Object... params)", s, isStatic),
		panacheOp(className, "delete", "long delete(String query, Object... params)", s, isStatic),
		panacheOp(className, "deleteById", "boolean deleteById(Object id)", s, isStatic),
		panacheOp(className, "deleteAll", "long deleteAll()", s, isStatic),
		panacheOp(className, "persist", "void persist("+className+" entity)", s, isStatic),
		panacheOp(className, "persist", "void persist(Iterable<"+className+"> entities)", s, isStatic),
		panacheOp(className, "update", "long update(String query, Object... params)", s, isStatic),
		panacheOp(className, "project", "PanacheQuery<T> project(Class<T> type)", s, isStatic),
	}
}

// mongoEntityInstanceMethods returns the Mongo Panache instance methods.
func mongoEntityInstanceMethods(className, synthFrom string) []types.EntityRecord {
	s := synthFrom
	return []types.EntityRecord{
		panacheOp(className, "persist", "void persist()", s, nil),
		panacheOp(className, "persistOrUpdate", "void persistOrUpdate()", s, nil),
		panacheOp(className, "update", "void update()", s, nil),
		panacheOp(className, "delete", "void delete()", s, nil),
		panacheOp(className, "isPersistent", "boolean isPersistent()", s, nil),
	}
}

// reactiveMongoEntityStaticMethods returns the Reactive MongoDB Panache surface.
func reactiveMongoEntityStaticMethods(className, synthFrom string) []types.EntityRecord {
	s := synthFrom
	reactive := map[string]string{"is_static": "true", "reactive": "true"}
	return []types.EntityRecord{
		panacheOp(className, "findById", "Uni<"+className+"> findById(Object id)", s, reactive),
		panacheOp(className, "findByIdOptional", "Uni<Optional<"+className+">> findByIdOptional(Object id)", s, reactive),
		panacheOp(className, "find", "ReactivePanacheQuery<"+className+"> find(String query)", s, reactive),
		panacheOp(className, "find", "ReactivePanacheQuery<"+className+"> find(String query, Object... params)", s, reactive),
		panacheOp(className, "findAll", "ReactivePanacheQuery<"+className+"> findAll()", s, reactive),
		panacheOp(className, "list", "Uni<List<"+className+">> list(String query, Object... params)", s, reactive),
		panacheOp(className, "listAll", "Uni<List<"+className+">> listAll()", s, reactive),
		panacheOp(className, "stream", "Multi<"+className+"> stream(String query, Object... params)", s, reactive),
		panacheOp(className, "streamAll", "Multi<"+className+"> streamAll()", s, reactive),
		panacheOp(className, "count", "Uni<Long> count()", s, reactive),
		panacheOp(className, "delete", "Uni<Long> delete(String query, Object... params)", s, reactive),
		panacheOp(className, "deleteById", "Uni<Boolean> deleteById(Object id)", s, reactive),
		panacheOp(className, "deleteAll", "Uni<Long> deleteAll()", s, reactive),
		panacheOp(className, "persist", "Uni<Void> persist("+className+" entity)", s, reactive),
	}
}

// reactiveMongoEntityInstanceMethods returns the Reactive MongoDB Panache instance methods.
func reactiveMongoEntityInstanceMethods(className, synthFrom string) []types.EntityRecord {
	s := synthFrom
	reactive := map[string]string{"reactive": "true"}
	return []types.EntityRecord{
		panacheOp(className, "persist", "Uni<Void> persist()", s, reactive),
		panacheOp(className, "persistOrUpdate", "Uni<Void> persistOrUpdate()", s, reactive),
		panacheOp(className, "delete", "Uni<Void> delete()", s, reactive),
		panacheOp(className, "isPersistent", "boolean isPersistent()", s, reactive),
	}
}

// repositoryInstanceMethods returns Panache methods as instance methods on the
// repository class (since repositories hold references to entity types and
// call on them). The surface is the same as static entity methods but emitted
// as instance methods on the repository.
func repositoryInstanceMethods(repoClass, synthFrom string) []types.EntityRecord {
	// Repositories have the same query surface; we emit generically typed versions.
	s := synthFrom
	return []types.EntityRecord{
		panacheOp(repoClass, "findById", "T findById(ID id)", s, nil),
		panacheOp(repoClass, "findByIdOptional", "Optional<T> findByIdOptional(ID id)", s, nil),
		panacheOp(repoClass, "find", "PanacheQuery<T> find(String query)", s, nil),
		panacheOp(repoClass, "find", "PanacheQuery<T> find(String query, Object... params)", s, nil),
		panacheOp(repoClass, "find", "PanacheQuery<T> find(String query, Sort sort)", s, nil),
		panacheOp(repoClass, "find", "PanacheQuery<T> find(String query, Map<String,Object> params)", s, nil),
		panacheOp(repoClass, "findAll", "PanacheQuery<T> findAll()", s, nil),
		panacheOp(repoClass, "findAll", "PanacheQuery<T> findAll(Sort sort)", s, nil),
		panacheOp(repoClass, "list", "List<T> list(String query, Object... params)", s, nil),
		panacheOp(repoClass, "listAll", "List<T> listAll()", s, nil),
		panacheOp(repoClass, "stream", "Stream<T> stream(String query, Object... params)", s, nil),
		panacheOp(repoClass, "streamAll", "Stream<T> streamAll()", s, nil),
		panacheOp(repoClass, "count", "long count()", s, nil),
		panacheOp(repoClass, "count", "long count(String query, Object... params)", s, nil),
		panacheOp(repoClass, "delete", "long delete(String query, Object... params)", s, nil),
		panacheOp(repoClass, "deleteById", "boolean deleteById(ID id)", s, nil),
		panacheOp(repoClass, "deleteAll", "long deleteAll()", s, nil),
		panacheOp(repoClass, "persist", "void persist(T entity)", s, nil),
		panacheOp(repoClass, "persist", "void persist(Iterable<T> entities)", s, nil),
		panacheOp(repoClass, "persistAndFlush", "void persistAndFlush(T entity)", s, nil),
		panacheOp(repoClass, "update", "int update(String query, Object... params)", s, nil),
		panacheOp(repoClass, "flush", "void flush()", s, nil),
		panacheOp(repoClass, "project", "PanacheQuery<P> project(Class<P> type)", s, nil),
	}
}

// reactiveRepositoryInstanceMethods returns the Reactive Panache repository surface.
func reactiveRepositoryInstanceMethods(repoClass, synthFrom string) []types.EntityRecord {
	s := synthFrom
	reactive := map[string]string{"reactive": "true"}
	return []types.EntityRecord{
		panacheOp(repoClass, "findById", "Uni<T> findById(ID id)", s, reactive),
		panacheOp(repoClass, "findByIdOptional", "Uni<Optional<T>> findByIdOptional(ID id)", s, reactive),
		panacheOp(repoClass, "find", "ReactivePanacheQuery<T> find(String query)", s, reactive),
		panacheOp(repoClass, "find", "ReactivePanacheQuery<T> find(String query, Object... params)", s, reactive),
		panacheOp(repoClass, "findAll", "ReactivePanacheQuery<T> findAll()", s, reactive),
		panacheOp(repoClass, "list", "Uni<List<T>> list(String query, Object... params)", s, reactive),
		panacheOp(repoClass, "listAll", "Uni<List<T>> listAll()", s, reactive),
		panacheOp(repoClass, "count", "Uni<Long> count()", s, reactive),
		panacheOp(repoClass, "delete", "Uni<Long> delete(String query, Object... params)", s, reactive),
		panacheOp(repoClass, "deleteById", "Uni<Boolean> deleteById(ID id)", s, reactive),
		panacheOp(repoClass, "deleteAll", "Uni<Long> deleteAll()", s, reactive),
		panacheOp(repoClass, "persist", "Uni<Void> persist(T entity)", s, reactive),
		panacheOp(repoClass, "persistAndFlush", "Uni<Void> persistAndFlush(T entity)", s, reactive),
		panacheOp(repoClass, "flush", "Uni<Void> flush()", s, reactive),
	}
}

// synthesizeNamedQueryEntities extracts @NamedQuery(name="...", query="...")
// annotations from the class declaration text and emits a named Operation
// entity for each one. This lets the resolver bind calls to named queries
// (e.g. Book.find("#Book.byTitle", ...)) to a real entity.
func synthesizeNamedQueryEntities(className, classDeclSrc, sourceFile string) []types.EntityRecord {
	var out []types.EntityRecord
	src := classDeclSrc
	const marker = "@NamedQuery"
	for {
		idx := strings.Index(src, marker)
		if idx < 0 {
			break
		}
		src = src[idx+len(marker):]
		// Find the name="..." attribute within the annotation.
		const nameAttr = `name="`
		ni := strings.Index(src, nameAttr)
		if ni < 0 {
			continue
		}
		rest := src[ni+len(nameAttr):]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			continue
		}
		queryName := rest[:end]
		if queryName == "" {
			continue
		}
		// Emit entity named after the query.
		props := map[string]string{
			"synthesized_from": "quarkus_panache",
			"pattern_type":     "panache_named_query",
			"owner":            className,
			"query_name":       queryName,
		}
		out = append(out, types.EntityRecord{
			Name:         queryName,
			Kind:         "SCOPE.Operation",
			Subtype:      "method",
			SourceFile:   sourceFile,
			Language:     "java",
			Signature:    className + " query " + queryName,
			QualityScore: panacheSynthQuality,
			Properties:   props,
		})
	}
	return out
}

// synthesizePanacheEntities is the main entry point called from the walk
// function in java.go. It mirrors synthesizeLombokEntities in structure.
//
// className is the simple class name (e.g. "Book").
// classDeclSrc is the raw text of the class declaration up to the opening
// brace (annotations + modifiers + class/extends/implements tokens).
// classBodySrc is the raw text of the class body (between { and }).
// sourceFile is the file path for entity SourceFile field.
// rawFileImports is the full import block text (used to detect Panache variant).
//
// Returns nil when the class is not a Panache entity/repository.
func synthesizePanacheEntities(
	className string,
	classDeclSrc string,
	classBodySrc string,
	sourceFile string,
	rawFileImports string,
) []types.EntityRecord {
	info := detectPanache(classDeclSrc, rawFileImports)
	if info.variant == panacheNone {
		return nil
	}

	const synthFrom = "quarkus_panache"
	var out []types.EntityRecord

	switch info.variant {
	case panacheSQLEntity:
		out = append(out, sqlEntityStaticMethods(className, synthFrom)...)
		out = append(out, sqlEntityInstanceMethods(className, synthFrom)...)
	case panacheReactiveEntity:
		out = append(out, reactiveSQLEntityStaticMethods(className, synthFrom)...)
		out = append(out, reactiveSQLEntityInstanceMethods(className, synthFrom)...)
	case panacheMongoEntity:
		out = append(out, mongoEntityStaticMethods(className, synthFrom)...)
		out = append(out, mongoEntityInstanceMethods(className, synthFrom)...)
	case panacheReactiveMongoEntity:
		out = append(out, reactiveMongoEntityStaticMethods(className, synthFrom)...)
		out = append(out, reactiveMongoEntityInstanceMethods(className, synthFrom)...)
	case panacheSQLRepository, panacheMongoRepository:
		out = append(out, repositoryInstanceMethods(className, synthFrom)...)
	case panacheReactiveRepository, panacheReactiveMongoRepository:
		out = append(out, reactiveRepositoryInstanceMethods(className, synthFrom)...)
	}

	// Set SourceFile on all emitted entities.
	for i := range out {
		out[i].SourceFile = sourceFile
	}

	// @NamedQuery synthesis — scan the class declaration for named queries.
	out = append(out, synthesizeNamedQueryEntities(className, classDeclSrc, sourceFile)...)

	// Issue #820 — deduplicate by Name so that overloaded methods (e.g.
	// multiple findById / find / list overloads that all carry the same
	// entity Name "Order.findById") don't collide in the resolver's byName
	// and byKind indexes. The resolver binds CALLS stubs by entity Name,
	// not by signature, so one canonical entity per (class, methodName)
	// pair is sufficient to make every call site resolve. First-writer-wins
	// matches the byName first-writer-wins policy in BuildIndex.
	out = dedupSynthByName(out)

	return out
}

// dedupSynthByName returns a new slice containing the FIRST entity for each
// unique Name. Preserves the original ordering for entities whose Name has
// not been seen before; duplicates (same Name, any Kind/Signature) are
// dropped. Used to prevent synthesized overloads from poisoning the global
// byName index with a blank ambiguous sentinel (issue #820).
func dedupSynthByName(entities []types.EntityRecord) []types.EntityRecord {
	if len(entities) == 0 {
		return entities
	}
	seen := make(map[string]bool, len(entities))
	out := make([]types.EntityRecord, 0, len(entities))
	for _, e := range entities {
		if seen[e.Name] {
			continue
		}
		seen[e.Name] = true
		out = append(out, e)
	}
	return out
}

// ---------------------------------------------------------------------------
// Issue #818 — PanacheQuery / PanacheUpdate DSL builder method synthesis
// ---------------------------------------------------------------------------
//
// The static methods synthesized by #809 (Book.find, Book.findAll, etc.)
// return PanacheQuery<T> — an external Quarkus interface. When callers chain
// DSL methods on the returned object (q.list(), q.page(0,20), q.count(), …)
// those calls are emitted by the extractor as CALLS edges whose target is
// "PanacheQuery.list", "PanacheQuery.page", etc. Without synthesized entities
// for these methods the resolver classifies every chained DSL call as a
// bug-extractor unresolved reference.
//
// synthesizePanacheDSLEntities is called once per Java FILE (not per class)
// when any Panache import is detected. It emits:
//
//   1. A SCOPE.Component "PanacheQuery" interface entity.
//   2. One SCOPE.Operation per DSL method on PanacheQuery (SQL + reactive).
//   3. A SCOPE.Component "PanacheUpdate" interface entity.
//   4. One SCOPE.Operation per DSL method on PanacheUpdate.
//   5. A SCOPE.Component "ReactivePanacheQuery" interface entity.
//   6. One SCOPE.Operation per DSL method on ReactivePanacheQuery.
//
// The entities carry synthesized_from="quarkus_panache_dsl" and
// pattern_type="panache_dsl_method" so downstream consumers can distinguish
// them from entity-class synthesized methods.
//
// Deduplication: the file-scope entity emitter appends these to the global
// entity list. The indexer's dedup layer (byName) prevents duplicate graph
// nodes when multiple Panache entity files share the same PanacheQuery stub.

const panacheDSLSynthFrom = "quarkus_panache_dsl"

// panacheDSLOp builds a DSL method entity owned by a synthetic interface.
func panacheDSLOp(ownerIface, methodName, signature string, extraProps map[string]string) types.EntityRecord {
	props := map[string]string{
		"synthesized_from": panacheDSLSynthFrom,
		"pattern_type":     "panache_dsl_method",
		"owner":            ownerIface,
	}
	for k, v := range extraProps {
		props[k] = v
	}
	return types.EntityRecord{
		Name:         ownerIface + "." + methodName,
		Kind:         "SCOPE.Operation",
		Subtype:      "method",
		Language:     "java",
		Signature:    signature,
		QualityScore: panacheSynthQuality,
		Properties:   props,
	}
}

// panacheQueryDSLMethods returns the blocking (SQL ORM) PanacheQuery DSL surface.
// These are instance methods on the PanacheQuery<T> interface returned by
// entity.find(), entity.findAll(), repository.find(), etc.
func panacheQueryDSLMethods() []types.EntityRecord {
	const iface = "PanacheQuery"
	// No extra props for plain SQL variant.
	return []types.EntityRecord{
		// Terminal result methods
		panacheDSLOp(iface, "list", "List<T> list()", nil),
		panacheDSLOp(iface, "stream", "Stream<T> stream()", nil),
		panacheDSLOp(iface, "singleResult", "T singleResult()", nil),
		panacheDSLOp(iface, "singleResultOptional", "Optional<T> singleResultOptional()", nil),
		panacheDSLOp(iface, "firstResult", "T firstResult()", nil),
		panacheDSLOp(iface, "firstResultOptional", "Optional<T> firstResultOptional()", nil),
		panacheDSLOp(iface, "iterator", "Iterator<T> iterator()", nil),
		// Aggregate
		panacheDSLOp(iface, "count", "long count()", nil),
		panacheDSLOp(iface, "pageCount", "int pageCount()", nil),
		// Pagination (returns PanacheQuery for chaining)
		panacheDSLOp(iface, "page", "PanacheQuery<T> page(Page page)", nil),
		panacheDSLOp(iface, "page", "PanacheQuery<T> page(int index, int size)", nil),
		panacheDSLOp(iface, "nextPage", "PanacheQuery<T> nextPage()", nil),
		panacheDSLOp(iface, "previousPage", "PanacheQuery<T> previousPage()", nil),
		panacheDSLOp(iface, "firstPage", "PanacheQuery<T> firstPage()", nil),
		panacheDSLOp(iface, "lastPage", "PanacheQuery<T> lastPage()", nil),
		panacheDSLOp(iface, "hasNextPage", "boolean hasNextPage()", nil),
		panacheDSLOp(iface, "hasPreviousPage", "boolean hasPreviousPage()", nil),
		// Range
		panacheDSLOp(iface, "range", "PanacheQuery<T> range(int startIndex, int lastIndex)", nil),
		// Hint / lock
		panacheDSLOp(iface, "withHint", "PanacheQuery<T> withHint(String hintName, Object value)", nil),
		panacheDSLOp(iface, "withLock", "PanacheQuery<T> withLock(LockModeType lockMode)", nil),
		// Projection
		panacheDSLOp(iface, "project", "PanacheQuery<T> project(Class<T> type)", nil),
		// Filter
		panacheDSLOp(iface, "filter", "PanacheQuery<T> filter(String filterName, Parameters parameters)", nil),
		panacheDSLOp(iface, "filter", "PanacheQuery<T> filter(String filterName, Map<String,Object> parameters)", nil),
	}
}

// panacheUpdateDSLMethods returns the PanacheUpdate DSL surface.
// PanacheUpdate is returned by entity.update("field=:val", params) and
// provides a fluent where() terminal that executes the update.
func panacheUpdateDSLMethods() []types.EntityRecord {
	const iface = "PanacheUpdate"
	return []types.EntityRecord{
		panacheDSLOp(iface, "where", "int where(String query, Object... params)", nil),
		panacheDSLOp(iface, "where", "int where(String query, Map<String,Object> params)", nil),
		panacheDSLOp(iface, "where", "int where(String query, Parameters params)", nil),
		panacheDSLOp(iface, "whereOptional", "int whereOptional(String query, Object... params)", nil),
	}
}

// reactivePanacheQueryDSLMethods returns the Reactive Panache query DSL surface.
// ReactivePanacheQuery wraps results in Uni<T> or Multi<T> (Mutiny types).
func reactivePanacheQueryDSLMethods() []types.EntityRecord {
	const iface = "ReactivePanacheQuery"
	reactive := map[string]string{"reactive": "true"}
	return []types.EntityRecord{
		// Terminal result methods — wrapped in Uni<>
		panacheDSLOp(iface, "list", "Uni<List<T>> list()", reactive),
		panacheDSLOp(iface, "stream", "Multi<T> stream()", reactive),
		panacheDSLOp(iface, "singleResult", "Uni<T> singleResult()", reactive),
		panacheDSLOp(iface, "singleResultOptional", "Uni<Optional<T>> singleResultOptional()", reactive),
		panacheDSLOp(iface, "firstResult", "Uni<T> firstResult()", reactive),
		panacheDSLOp(iface, "firstResultOptional", "Uni<Optional<T>> firstResultOptional()", reactive),
		// Aggregate
		panacheDSLOp(iface, "count", "Uni<Long> count()", reactive),
		panacheDSLOp(iface, "pageCount", "Uni<Integer> pageCount()", reactive),
		// Pagination
		panacheDSLOp(iface, "page", "ReactivePanacheQuery<T> page(Page page)", reactive),
		panacheDSLOp(iface, "page", "ReactivePanacheQuery<T> page(int index, int size)", reactive),
		panacheDSLOp(iface, "nextPage", "ReactivePanacheQuery<T> nextPage()", reactive),
		panacheDSLOp(iface, "previousPage", "ReactivePanacheQuery<T> previousPage()", reactive),
		panacheDSLOp(iface, "firstPage", "ReactivePanacheQuery<T> firstPage()", reactive),
		panacheDSLOp(iface, "lastPage", "ReactivePanacheQuery<T> lastPage()", reactive),
		panacheDSLOp(iface, "hasNextPage", "Uni<Boolean> hasNextPage()", reactive),
		panacheDSLOp(iface, "hasPreviousPage", "boolean hasPreviousPage()", reactive),
		// Range
		panacheDSLOp(iface, "range", "ReactivePanacheQuery<T> range(int startIndex, int lastIndex)", reactive),
		// Hint / lock / projection / filter
		panacheDSLOp(iface, "withHint", "ReactivePanacheQuery<T> withHint(String hintName, Object value)", reactive),
		panacheDSLOp(iface, "withLock", "ReactivePanacheQuery<T> withLock(LockModeType lockMode)", reactive),
		panacheDSLOp(iface, "project", "ReactivePanacheQuery<T> project(Class<T> type)", reactive),
		panacheDSLOp(iface, "filter", "ReactivePanacheQuery<T> filter(String filterName, Parameters parameters)", reactive),
		panacheDSLOp(iface, "filter", "ReactivePanacheQuery<T> filter(String filterName, Map<String,Object> parameters)", reactive),
	}
}

// synthesizePanacheDSLEntities emits PanacheQuery / PanacheUpdate / ReactivePanacheQuery
// interface entities and all their DSL methods. Called once per Java FILE that
// imports any Quarkus Panache package. The rawFileImports string is the same
// token passed to synthesizePanacheEntities; we reuse the existing import-
// detection machinery to decide whether Panache is in scope.
//
// Returns nil when no Panache import is detected (most Java files).
//
// The SourceFile field on returned entities is intentionally set to the
// calling file — this lets the resolver's file-scope byName lookup bind
// `PanacheQuery.list` references emitted from the same file. Cross-file
// binding via the global byName/byMember index also works because the
// same entity name is emitted from every Panache file and the indexer
// merges by Name (keeping the first occurrence).
func synthesizePanacheDSLEntities(sourceFile, rawFileImports string) []types.EntityRecord {
	// Only emit DSL stubs when the file has a Quarkus Panache import.
	hasPanache := false
	for _, imp := range panacheImportSets {
		if strings.Contains(rawFileImports, imp.prefix) {
			hasPanache = true
			break
		}
	}
	if !hasPanache {
		return nil
	}

	const synthFrom = panacheDSLSynthFrom

	// PanacheQuery interface component entity.
	pqComponent := types.EntityRecord{
		Name:         "PanacheQuery",
		Kind:         "SCOPE.Component",
		Subtype:      "interface",
		Language:     "java",
		SourceFile:   sourceFile,
		Signature:    "interface PanacheQuery<T>",
		QualityScore: panacheSynthQuality,
		Properties: map[string]string{
			"synthesized_from": synthFrom,
			"pattern_type":     "panache_dsl_interface",
		},
	}

	// PanacheUpdate interface component entity.
	puComponent := types.EntityRecord{
		Name:         "PanacheUpdate",
		Kind:         "SCOPE.Component",
		Subtype:      "interface",
		Language:     "java",
		SourceFile:   sourceFile,
		Signature:    "interface PanacheUpdate",
		QualityScore: panacheSynthQuality,
		Properties: map[string]string{
			"synthesized_from": synthFrom,
			"pattern_type":     "panache_dsl_interface",
		},
	}

	// ReactivePanacheQuery interface component entity.
	rpqComponent := types.EntityRecord{
		Name:         "ReactivePanacheQuery",
		Kind:         "SCOPE.Component",
		Subtype:      "interface",
		Language:     "java",
		SourceFile:   sourceFile,
		Signature:    "interface ReactivePanacheQuery<T>",
		QualityScore: panacheSynthQuality,
		Properties: map[string]string{
			"synthesized_from": synthFrom,
			"pattern_type":     "panache_dsl_interface",
			"reactive":         "true",
		},
	}

	var out []types.EntityRecord
	out = append(out, pqComponent)
	out = append(out, panacheQueryDSLMethods()...)
	out = append(out, puComponent)
	out = append(out, panacheUpdateDSLMethods()...)
	out = append(out, rpqComponent)
	out = append(out, reactivePanacheQueryDSLMethods()...)

	// Stamp SourceFile on all method entities.
	for i := range out {
		if out[i].SourceFile == "" {
			out[i].SourceFile = sourceFile
		}
	}
	return out
}
