// imports.go — IMPORTS to_id resolution for the Java extractor.
//
// Analog of #642 (JS/TS) and #650 (Python) for Java/JVM. The Java
// extractor previously emitted IMPORTS edges whose ToID was the full
// dotted import path ("org.springframework.boot.SpringApplication" or
// "com.foo.Bar"). Neither shape carries the `ext:<package>` prefix the
// resolver's external-disposition gate (refs.go: stubPrefixExternal)
// keys on, so every imported leaf from a known external JVM package
// (java, javax, jakarta, kotlin, org.springframework, io.quarkus,
// org.junit, ...) had to round-trip through the bare-name resolver,
// miss, and fall back to ExternalUnknown / bug-extractor — contributing
// to the 63.4% orphan rate on the fixture-d (Quarkus) corpus.
//
// The fix mirrors #642/#650: AFTER buildImport has emitted the IMPORTS
// edges, walk every edge and rewrite the ToID for edges whose
// source_module's root segment points at a known external JVM package:
//
//	import org.springframework.boot.SpringApplication;
//	    → ToID = "ext:org.springframework:SpringApplication"
//	import io.quarkus.runtime.Quarkus;
//	    → ToID = "ext:io.quarkus:Quarkus"
//	import java.util.List;
//	    → ToID = "ext:java:List"
//	import com.acme.MyType;     // in-tree, unknown root
//	    → untouched (com.acme not on allowlist)
//
// In-tree imports (any com.* / org.* root not on the JVM allowlist)
// are NOT touched here — the resolver's ResolveDottedImportTarget path
// already binds them via the source_module + imported_name properties.
//
// The conservative bias is: ONLY rewrite when the LONGEST dotted prefix
// of source_module matches a hard-coded list of well-known external
// JVM packages. Multi-segment prefixes (`org.springframework`,
// `com.fasterxml`, `io.quarkus`) are preferred so an unrelated
// `org.acmecorp` user-namespace never collides with the `org` bare
// segment.
//
// Keep in sync with internal/external/synth.go knownExternalPackages —
// this list need not be exhaustive (any miss stays as-is, which is the
// pre-fix shape), but every entry must also be present in the
// authoritative allowlist or the resolver will misclassify the edge as
// ExternalUnknown.

package java

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// javaKnownExternalRoots is the set of dotted prefixes that the
// resolver's external-disposition gate classifies as ExternalKnown via
// the `ext:<prefix>` prefix. When the Java extractor sees an IMPORTS
// edge whose source_module's longest matching prefix is on this list,
// it rewrites the ToID to `ext:<prefix>:<imported_name>` (or just
// `ext:<prefix>` for wildcard imports) so the edge bypasses the bare-
// name resolver and lands on ExternalKnown directly.
//
// The list is split into single-segment roots (`java`, `kotlin`) and
// multi-segment dotted roots (`org.springframework`, `io.quarkus`).
// Longest-prefix matching is applied at lookup time so
// `org.springframework.boot.web.servlet` matches `org.springframework`
// without falling through to a hypothetical `org` bare root.
var javaKnownExternalRoots = map[string]struct{}{
	// JVM language stdlib / language ecosystems
	"java":    {},
	"javax":   {},
	"jakarta": {},
	"kotlin":  {},
	"kotlinx": {},
	"scala":   {},
	"groovy":  {},

	// Spring / Java EE / Jakarta EE
	"org.springframework": {},
	"org.hibernate":       {},

	// Test / mock / assertion ecosystem
	"org.junit":          {},
	"org.mockito":        {},
	"org.assertj":        {},
	"org.hamcrest":       {},
	"org.testcontainers": {},
	"junit":              {},
	"mockito":            {},

	// Logging
	"org.slf4j":      {},
	"slf4j":          {},
	"log4j":          {},
	"ch.qos.logback": {},
	"lombok":         {},

	// Apache / Eclipse / JetBrains / Gradle / Codehaus umbrellas
	"org.apache":            {},
	"org.apache.commons":    {},
	"org.apache.kafka":      {},
	"org.apache.avro":       {},
	"org.apache.curator":    {},
	"org.apache.zookeeper":  {},
	"org.apache.log4j":      {},
	"org.apache.logging":    {},
	"org.apache.hadoop":     {},
	"org.eclipse":           {},
	"org.eclipse.jetty":     {},
	"org.jetbrains":         {},
	"org.jetbrains.exposed": {},
	"org.jetbrains.kotlinx": {},
	"org.jetbrains.kotlin":  {},
	"org.gradle":            {},
	"org.codehaus":          {},
	"org.glassfish":         {},
	"org.glassfish.jersey":  {},
	"org.rocksdb":           {},
	"org.json":              {},
	"org.yaml":              {},

	// Quarkus / SmallRye / Netty / gRPC / reactive / metrics / docs
	"io.quarkus":      {},
	"io.smallrye":     {},
	"io.netty":        {},
	"io.grpc":         {},
	"io.reactivex":    {},
	"io.vertx":        {},
	"io.micrometer":   {},
	"io.swagger":      {},
	"io.ktor":         {},
	"io.confluent":    {},
	"io.opentelemetry": {},
	"reactor":         {},

	// Google / Fasterxml (Jackson) / cloud SDKs
	"com.google":           {},
	"com.google.common":    {},
	"com.google.guava":     {},
	"com.google.protobuf":  {},
	"com.google.gson":      {},
	"com.fasterxml":        {},
	"com.fasterxml.jackson": {},
	"com.amazonaws":        {},
	"com.azure":            {},
	"com.microsoft":        {},
	"com.oracle":           {},
	"com.sun":              {},
	"com.typesafe":         {},
	"com.zaxxer":           {}, // HikariCP connection pool

	// Crypto / Redis / misc
	"redis.clients": {},  // Jedis Redis client
	"at.favre.lib":  {},  // BCrypt
}

// resolveImportToIDs walks every IMPORTS edge on every entity in
// entities and, when the source_module's longest dotted prefix matches
// a known external JVM package, rewrites the ToID to the
// `ext:<prefix>[:<imported_name>]` form. Idempotent — ToIDs already
// carrying the `ext:` prefix are left alone.
//
// Mutates the entities slice's relationships in place.
func resolveImportToIDs(entities []types.EntityRecord) {
	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Component" {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if r.Properties == nil {
				continue
			}
			if strings.HasPrefix(r.ToID, "ext:") {
				continue // already external-tagged
			}
			mod := r.Properties["source_module"]
			if mod == "" {
				continue
			}
			// Java has no relative-import shape (no leading ".") — every
			// import is fully qualified. Skip defensively for parity with
			// the Python implementation.
			if strings.HasPrefix(mod, ".") {
				continue
			}
			prefix := longestKnownJavaPrefix(mod)
			if prefix == "" {
				continue
			}
			imported := r.Properties["imported_name"]
			wildcard := r.Properties["wildcard"] == "1"
			lower := strings.ToLower(prefix)
			switch {
			case wildcard:
				// Wildcard import: `import org.springframework.boot.*;`
				// → ext:org.springframework (the imported_name is empty).
				r.ToID = "ext:" + lower
			case imported == "":
				r.ToID = "ext:" + lower
			default:
				// Strip any module-prefix duplication from imported_name
				// so `ext:org.springframework:SpringApplication`, not
				// `ext:org.springframework:org.springframework.boot.SpringApplication`.
				leaf := imported
				if idx := strings.LastIndexByte(leaf, '.'); idx >= 0 {
					leaf = leaf[idx+1:]
				}
				r.ToID = "ext:" + lower + ":" + leaf
			}
		}
	}
}

// longestKnownJavaPrefix returns the longest dotted prefix of mod that
// matches an entry in javaKnownExternalRoots. Walks from longest to
// shortest by repeatedly trimming the trailing dotted segment. Returns
// "" when no prefix matches.
//
//	"org.springframework.boot.SpringApplication"
//	  → "org.springframework" (org.springframework is on the list)
//	"com.acme.MyType"
//	  → "" (no prefix matches)
//	"java.util.List"
//	  → "java"
func longestKnownJavaPrefix(mod string) string {
	cur := strings.ToLower(mod)
	for cur != "" {
		if _, ok := javaKnownExternalRoots[cur]; ok {
			return cur
		}
		dot := strings.LastIndexByte(cur, '.')
		if dot < 0 {
			return ""
		}
		cur = cur[:dot]
	}
	return ""
}
