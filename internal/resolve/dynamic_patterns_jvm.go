package resolve

import "regexp"

// jvmDynamicPatterns is the per-language dynamic-dispatch pattern catalog for
// JVM languages: Java, Kotlin, Scala, and the generic "jvm" tag.
// All four language keys share this slice.
// Matches here tag a stub as DispositionDynamic.
//
// See the per-language catalog overview comment in refs.go for the design
// rationale behind the language-gated approach (Refs #44).
var jvmDynamicPatterns = []*regexp.Regexp{
	// Bare-identifier forms: Java/Kotlin extractors emit only the
	// leaf callee identifier (e.g. `m.invoke(target, args)` →
	// ToID="invoke"). Real reflection in the wild stores the
	// reflective handle in a local var (`Method m = clazz.getMethod(name); m.invoke(...)`),
	// so the receiver-typed pattern `Method.invoke(` never sees the
	// type and the call lands in BugExtractor instead of Dynamic
	// (issue #72). The bare-name anchors below promote those leaf
	// callees into the Dynamic disposition.
	//
	// Collisions with non-reflective user methods (`cli.invoke(...)`,
	// `factory.newInstance()`) are accepted: Dynamic is the
	// appropriate "we know it's dispatch we can't statically resolve"
	// bucket — these stubs are equally unresolvable either way, and
	// classifying them as Dynamic keeps them out of bug-extractor.
	// The per-language gate (Java/Kotlin/Scala/JVM only) keeps these
	// names from polluting other languages.
	regexp.MustCompile(`^forName$`),                 // Class.forName
	regexp.MustCompile(`^invoke$`),                  // Method.invoke / Constructor handle
	regexp.MustCompile(`^newInstance$`),             // Class.newInstance / Constructor.newInstance
	regexp.MustCompile(`^getClass$`),                // Object.getClass — reflection entry point
	regexp.MustCompile(`^getMethod$`),               // Class.getMethod
	regexp.MustCompile(`^getMethods$`),              // Class.getMethods
	regexp.MustCompile(`^getDeclaredMethod$`),       // Class.getDeclaredMethod
	regexp.MustCompile(`^getDeclaredMethods$`),      // Class.getDeclaredMethods
	regexp.MustCompile(`^getField$`),                // Class.getField
	regexp.MustCompile(`^getFields$`),               // Class.getFields
	regexp.MustCompile(`^getDeclaredField$`),        // Class.getDeclaredField
	regexp.MustCompile(`^getDeclaredFields$`),       // Class.getDeclaredFields
	regexp.MustCompile(`^getConstructor$`),          // Class.getConstructor
	regexp.MustCompile(`^getConstructors$`),         // Class.getConstructors
	regexp.MustCompile(`^getDeclaredConstructor$`),  // Class.getDeclaredConstructor
	regexp.MustCompile(`^getDeclaredConstructors$`), // Class.getDeclaredConstructors
	// JVM reflection invoke is `Method.invoke(...)` or
	// `Constructor.invoke(...)`. Anchored to those receivers when
	// the full call expression is present (extractor pre-strip
	// stubs) so a user-defined `cli.invoke(...)` / `cmd.invoke(...)`
	// does NOT match.
	regexp.MustCompile(`\b(?:Method|Constructor)\.invoke\(`),
	regexp.MustCompile(`^Class\.forName\(`), // Class.forName("...")
	// Anchored to the reflective `Class.forName(...).newInstance()` /
	// `<Type>.class.newInstance()` shape so a plain factory method
	// named `newInstance()` on a domain class does NOT match.
	regexp.MustCompile(`Class\.forName\([^)]*\)\.newInstance\(`),
	regexp.MustCompile(`\.class\.newInstance\(`),
	regexp.MustCompile(`^ServiceLoader\.load\(`), // ServiceLoader.load(...)
	regexp.MustCompile(`^System\.getenv\(`),      // env-driven (JVM)
	// Issue #44 — Spring MVC / WebFlux ResponseEntity fluent builder
	// methods. Spring's ResponseEntity uses a static factory + builder
	// pattern:
	//
	//   ResponseEntity.notFound().build()
	//   ResponseEntity.ok(body)
	//   ResponseEntity.status(HttpStatus.CREATED).body(x)
	//   ResponseEntity.noContent().build()
	//
	// The Kotlin/Java extractor emits the trailing method leaf as a
	// bare CALLS stub (e.g. `notFound`, `ok`, `build`, `body`).
	// Without full type-inference the resolver cannot tell whether
	// the caller's receiver is a ResponseEntity builder or a same-
	// named in-tree method. These names are also generic enough
	// (`ok`, `build`, `body`) that user-defined methods can share
	// them — both cases are statically unresolvable and Dynamic is the
	// correct bucket. The JVM-language gate keeps these from
	// polluting non-JVM graphs (#94 safer-bias rule).
	regexp.MustCompile(`^notFound$`),           // ResponseEntity.notFound()
	regexp.MustCompile(`^noContent$`),          // ResponseEntity.noContent()
	regexp.MustCompile(`^badRequest$`),         // ResponseEntity.badRequest()
	regexp.MustCompile(`^accepted$`),           // ResponseEntity.accepted()
	regexp.MustCompile(`^created$`),            // ResponseEntity.created(uri)
	regexp.MustCompile(`^unprocessableEntity$`), // ResponseEntity.unprocessableEntity()
	regexp.MustCompile(`^internalServerError$`), // ResponseEntity.internalServerError()
	// Builder terminal methods that appear as bare leaf stubs when
	// the receiver is a ResponseEntity.BodyBuilder /
	// ResponseEntity.HeadersBuilder (or any other fluent builder that
	// can't be resolved by name alone).
	regexp.MustCompile(`^build$`), // BodyBuilder.build() / HeadersBuilder.build()
	regexp.MustCompile(`^body$`),  // BodyBuilder.body(T)
	// ResponseEntity.ok(T) is already common in user code; but since
	// the receiver is always the static class itself in Spring usage
	// and the name is generic enough to be in-tree we accept the
	// safer-bias trade-off — Dynamic for both cases.
	regexp.MustCompile(`^ok$`), // ResponseEntity.ok(body)

	// Issue #44 — Scala stdlib companion-object and collection method
	// stubs. The Scala extractor emits qualified CALLS edges of the form
	// "Future.successful", "List.map", "Map.get", etc. when the receiver
	// is a stdlib type and full type inference is unavailable.
	//
	// These are statically unresolvable because:
	//  1. Future / Try / Option / List / Map / Seq / Set / Vector are
	//     external types (scala.concurrent / scala.util / scala.collection).
	//  2. The extractor already emits the PascalCase-qualified form, so
	//     the resolver correctly identifies the receiver but can't bind
	//     to an entity (none is emitted for stdlib).
	//
	// Pattern: "^<ScalaStdlibType>\.<method>$" — qualified leaf form.
	// JVM-language gate keeps these from polluting Python / Go / JS / Ruby.
	//
	// scala.concurrent.Future companion methods.
	regexp.MustCompile(`^Future\.successful$`), // Future.successful(v)
	regexp.MustCompile(`^Future\.failed$`),     // Future.failed(ex)
	regexp.MustCompile(`^Future\.apply$`),      // Future { ... }
	regexp.MustCompile(`^Future\.sequence$`),   // Future.sequence(list)
	regexp.MustCompile(`^Future\.traverse$`),   // Future.traverse(coll)(f)
	regexp.MustCompile(`^Future\.unit$`),       // Future.unit
	// scala.util.Try / Success / Failure companion methods.
	regexp.MustCompile(`^Try\.apply$`),     // Try { ... }
	regexp.MustCompile(`^Success\.apply$`), // Success(v)
	regexp.MustCompile(`^Failure\.apply$`), // Failure(ex)
	// scala.collection.immutable.List companion + instance methods.
	regexp.MustCompile(`^List\.apply$`),        // List(a, b, c)
	regexp.MustCompile(`^List\.empty$`),        // List.empty
	regexp.MustCompile(`^List\.from$`),         // List.from(iter)
	regexp.MustCompile(`^List\.map$`),          // list.map(f) — qualified via receiver
	regexp.MustCompile(`^List\.flatMap$`),      // list.flatMap(f)
	regexp.MustCompile(`^List\.filter$`),       // list.filter(p)
	regexp.MustCompile(`^List\.filterNot$`),    // list.filterNot(p)
	regexp.MustCompile(`^List\.find$`),         // list.find(p)
	regexp.MustCompile(`^List\.foldLeft$`),     // list.foldLeft(z)(f)
	regexp.MustCompile(`^List\.foldRight$`),    // list.foldRight(z)(f)
	regexp.MustCompile(`^List\.foreach$`),      // list.foreach(f)
	regexp.MustCompile(`^List\.collect$`),      // list.collect(pf)
	regexp.MustCompile(`^List\.exists$`),       // list.exists(p)
	regexp.MustCompile(`^List\.forall$`),       // list.forall(p)
	regexp.MustCompile(`^List\.head$`),         // list.head
	regexp.MustCompile(`^List\.tail$`),         // list.tail
	regexp.MustCompile(`^List\.size$`),         // list.size
	regexp.MustCompile(`^List\.length$`),       // list.length
	regexp.MustCompile(`^List\.isEmpty$`),      // list.isEmpty
	regexp.MustCompile(`^List\.nonEmpty$`),     // list.nonEmpty
	regexp.MustCompile(`^List\.toList$`),       // list.toList
	regexp.MustCompile(`^List\.toSet$`),        // list.toSet
	regexp.MustCompile(`^List\.toSeq$`),        // list.toSeq
	regexp.MustCompile(`^List\.toVector$`),     // list.toVector
	regexp.MustCompile(`^List\.sorted$`),       // list.sorted
	regexp.MustCompile(`^List\.sortBy$`),       // list.sortBy(f)
	regexp.MustCompile(`^List\.groupBy$`),      // list.groupBy(f)
	regexp.MustCompile(`^List\.take$`),         // list.take(n)
	regexp.MustCompile(`^List\.drop$`),         // list.drop(n)
	regexp.MustCompile(`^List\.zip$`),          // list.zip(other)
	regexp.MustCompile(`^List\.zipWithIndex$`), // list.zipWithIndex
	regexp.MustCompile(`^List\.distinct$`),     // list.distinct
	regexp.MustCompile(`^List\.flatten$`),      // list.flatten
	regexp.MustCompile(`^List\.mkString$`),     // list.mkString(sep)
	// scala.collection.immutable.Map companion + instance methods.
	regexp.MustCompile(`^Map\.apply$`),      // Map(k -> v)
	regexp.MustCompile(`^Map\.empty$`),      // Map.empty
	regexp.MustCompile(`^Map\.from$`),       // Map.from(iter)
	regexp.MustCompile(`^Map\.get$`),        // map.get(k)
	regexp.MustCompile(`^Map\.contains$`),   // map.contains(k)
	regexp.MustCompile(`^Map\.keys$`),       // map.keys
	regexp.MustCompile(`^Map\.values$`),     // map.values
	regexp.MustCompile(`^Map\.updated$`),    // map.updated(k, v)
	regexp.MustCompile(`^Map\.removed$`),    // map.removed(k)
	regexp.MustCompile(`^Map\.map$`),        // map.map(f)
	regexp.MustCompile(`^Map\.filter$`),     // map.filter(p)
	regexp.MustCompile(`^Map\.filterKeys$`), // map.filterKeys(p)
	regexp.MustCompile(`^Map\.mapValues$`),  // map.mapValues(f)
	regexp.MustCompile(`^Map\.toList$`),     // map.toList
	regexp.MustCompile(`^Map\.toSeq$`),      // map.toSeq
	regexp.MustCompile(`^Map\.size$`),       // map.size
	regexp.MustCompile(`^Map\.isEmpty$`),    // map.isEmpty
	// scala.collection.immutable.Seq / Vector / Set mirrors.
	regexp.MustCompile(`^Seq\.apply$`),    // Seq(...)
	regexp.MustCompile(`^Seq\.empty$`),    // Seq.empty
	regexp.MustCompile(`^Seq\.map$`),      // seq.map(f)
	regexp.MustCompile(`^Seq\.flatMap$`),  // seq.flatMap(f)
	regexp.MustCompile(`^Seq\.filter$`),   // seq.filter(p)
	regexp.MustCompile(`^Seq\.filterNot$`), // seq.filterNot(p)
	regexp.MustCompile(`^Seq\.find$`),     // seq.find(p)
	regexp.MustCompile(`^Seq\.foreach$`),  // seq.foreach(f)
	regexp.MustCompile(`^Vector\.apply$`), // Vector(...)
	regexp.MustCompile(`^Vector\.empty$`), // Vector.empty
	regexp.MustCompile(`^Vector\.from$`),  // Vector.from(iter)
	regexp.MustCompile(`^Set\.apply$`),    // Set(...)
	regexp.MustCompile(`^Set\.empty$`),    // Set.empty
	regexp.MustCompile(`^Set\.from$`),     // Set.from(iter)
	// scala.Option instance and companion methods.
	regexp.MustCompile(`^Option\.apply$`), // Option(v)
	regexp.MustCompile(`^Option\.empty$`), // Option.empty / None
	regexp.MustCompile(`^Some\.apply$`),   // Some(v)
	regexp.MustCompile(`^None\.get$`),     // None.get (rare but emitted)
}

func init() {
	dynamicPatternsByLang["java"] = jvmDynamicPatterns
	dynamicPatternsByLang["kotlin"] = jvmDynamicPatterns
	dynamicPatternsByLang["scala"] = jvmDynamicPatterns
	dynamicPatternsByLang["jvm"] = jvmDynamicPatterns
}
