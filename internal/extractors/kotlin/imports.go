// imports.go — IMPORTS to_id resolution for the Kotlin extractor.
//
// Analog of #642 (JS/TS), #650 (Python) and #670 (Java) for Kotlin. The
// Kotlin extractor previously emitted IMPORTS edges whose ToID was the
// full dotted import path ("io.ktor.server.routing.get" or
// "kotlin.io.println"). Neither shape carries the `ext:<package>`
// prefix the resolver's external-disposition gate (refs.go:
// stubPrefixExternal) keys on, so every imported leaf from a known
// external JVM/Kotlin package (kotlin, kotlinx, io.ktor,
// org.springframework, javax, jakarta, ...) had to round-trip through
// the bare-name resolver, miss, and fall back to ExternalUnknown /
// bug-extractor — contributing to the 40.3% orphan rate on
// ktor-samples.
//
// The fix mirrors #670: AFTER buildImport has emitted the IMPORTS
// edges, walk every edge and rewrite the ToID when the source module's
// longest dotted prefix matches a known external JVM/Kotlin package:
//
//	import io.ktor.server.routing.get
//	    → ToID = "ext:io.ktor:get"
//	import io.ktor.server.routing.*       (wildcard)
//	    → ToID = "ext:io.ktor"
//	import kotlin.io.println
//	    → ToID = "ext:kotlin:println"
//	import com.acmecorp.users.UserService   (in-tree, unknown root)
//	    → untouched (com.acmecorp not on allowlist)
//
// In-tree imports (any com.* / org.* root not on the JVM allowlist)
// are NOT touched here — the resolver's ResolveDottedImportTarget path
// already binds them via the source_module + imported_name properties
// once those are present.
//
// The conservative bias is: ONLY rewrite when the LONGEST dotted prefix
// of the import path matches a hard-coded list of well-known external
// JVM/Kotlin packages. Multi-segment prefixes (`io.ktor`,
// `org.springframework`, `com.fasterxml`) are preferred so an unrelated
// `org.acmecorp` user-namespace never collides with the `org` bare
// segment.
//
// Keep in sync with internal/external/synth.go knownExternalPackages
// and with the Java extractor's javaKnownExternalRoots — this list
// need not be exhaustive (any miss stays as-is, which is the pre-fix
// shape), but every entry must also be present in the authoritative
// allowlist or the resolver will misclassify the edge as
// ExternalUnknown.

package kotlin

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// kotlinKnownExternalRoots is the set of dotted prefixes that the
// resolver's external-disposition gate classifies as ExternalKnown via
// the `ext:<prefix>` prefix. When the Kotlin extractor sees an IMPORTS
// edge whose dotted path's longest matching prefix is on this list, it
// rewrites the ToID to `ext:<prefix>:<imported_leaf>` (or just
// `ext:<prefix>` for wildcard imports) so the edge bypasses the bare-
// name resolver and lands on ExternalKnown directly.
//
// Multi-segment dotted roots (`io.ktor`, `org.springframework`) are
// preferred so longest-prefix matching applied at lookup time correctly
// resolves `io.ktor.server.routing` to `io.ktor` without falling
// through to a hypothetical `io` bare root.
var kotlinKnownExternalRoots = map[string]struct{}{
	// Kotlin language / stdlib / ecosystems
	"kotlin":                    {},
	"kotlinx":                   {},
	"org.jetbrains":             {},
	"org.jetbrains.exposed":     {},
	"org.jetbrains.kotlinx":     {},
	"org.jetbrains.kotlin":      {},
	"org.jetbrains.annotations": {},

	// JVM language stdlib
	"java":    {},
	"javax":   {},
	"jakarta": {},
	"scala":   {},
	"groovy":  {},

	// Ktor / Quarkus / SmallRye / Netty / reactive / vertx
	"io.ktor":          {},
	"io.quarkus":       {},
	"io.smallrye":      {},
	"io.netty":         {},
	"io.grpc":          {},
	"io.reactivex":     {},
	"io.vertx":         {},
	"io.micrometer":    {},
	"io.swagger":       {},
	"io.confluent":     {},
	"io.opentelemetry": {},
	"reactor":          {},

	// Spring / Hibernate
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
	"io.mockk":           {},
	"io.kotest":          {},

	// Logging
	"org.slf4j":      {},
	"slf4j":          {},
	"log4j":          {},
	"ch.qos.logback": {},
	"lombok":         {},

	// Apache / Eclipse / Gradle / Codehaus umbrellas
	"org.apache":           {},
	"org.apache.commons":   {},
	"org.apache.kafka":     {},
	"org.apache.avro":      {},
	"org.apache.curator":   {},
	"org.apache.zookeeper": {},
	"org.apache.log4j":     {},
	"org.apache.logging":   {},
	"org.apache.hadoop":    {},
	"org.eclipse":          {},
	"org.eclipse.jetty":    {},
	"org.gradle":           {},
	"org.codehaus":         {},
	"org.glassfish":        {},
	"org.glassfish.jersey": {},
	"org.rocksdb":          {},
	"org.json":             {},
	"org.yaml":             {},

	// Google / Fasterxml (Jackson) / cloud SDKs
	"com.google":            {},
	"com.google.common":     {},
	"com.google.guava":      {},
	"com.google.protobuf":   {},
	"com.google.gson":       {},
	"com.fasterxml":         {},
	"com.fasterxml.jackson": {},
	"com.amazonaws":         {},
	"com.azure":             {},
	"com.microsoft":         {},
	"com.oracle":            {},
	"com.sun":               {},
	"com.typesafe":          {},
	"com.zaxxer":            {}, // HikariCP connection pool
	"com.squareup":          {}, // OkHttp / Retrofit / Moshi

	// Crypto / Redis / misc
	"redis.clients": {}, // Jedis Redis client
	"at.favre.lib":  {}, // BCrypt
}

// resolveImportToIDs walks every IMPORTS edge on every entity in
// entities and, when the import's dotted path's longest matching
// prefix is a known external JVM/Kotlin package, rewrites the ToID to
// the `ext:<prefix>[:<imported_name>]` form. Idempotent — ToIDs
// already carrying the `ext:` prefix are left alone.
//
// Because the Kotlin extractor's buildImport emits IMPORTS edges with
// the ToID set to the FULL dotted module path (with the optional `.*`
// wildcard suffix stripped), and does NOT populate the `source_module`
// / `imported_name` / `wildcard` property triplet that Java/Python
// extractors use, this implementation works directly off the ToID and
// the entity Name. The entity Name carries the same dotted path; when
// the source had a `.*` wildcard suffix the wildcard property is set
// to "1" via buildImport's modification (see kotlin.go).
//
// Mutates the entities slice's relationships in place.
func resolveImportToIDs(entities []types.EntityRecord) {
	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Component" || e.Subtype != "import" {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if strings.HasPrefix(r.ToID, "ext:") {
				continue // already external-tagged
			}
			// Skip relative-style imports defensively. Kotlin has no
			// leading-dot relative imports (every `import` is fully
			// qualified), but the guard mirrors Python/Java for parity.
			if strings.HasPrefix(r.ToID, ".") {
				continue
			}
			mod := r.ToID
			if mod == "" {
				continue
			}
			prefix := longestKnownKotlinPrefix(mod)
			if prefix == "" {
				continue
			}
			wildcard := false
			if r.Properties != nil && r.Properties["wildcard"] == "1" {
				wildcard = true
			}
			lower := strings.ToLower(prefix)
			switch {
			case wildcard:
				// Wildcard import: `import io.ktor.server.routing.*`
				// → ext:io.ktor.
				r.ToID = "ext:" + lower
			case mod == prefix:
				// `import io.ktor` (rare in Kotlin; defensive).
				r.ToID = "ext:" + lower
			default:
				// Strip the matched prefix and a trailing dot, then take
				// the LAST dotted segment as the imported leaf name so
				// `ext:io.ktor:get`, not
				// `ext:io.ktor:server.routing.get`.
				leaf := mod
				if idx := strings.LastIndexByte(leaf, '.'); idx >= 0 {
					leaf = leaf[idx+1:]
				}
				if leaf == "" {
					r.ToID = "ext:" + lower
				} else {
					r.ToID = "ext:" + lower + ":" + leaf
				}
			}
		}
	}
}

// longestKnownKotlinPrefix returns the longest dotted prefix of mod
// that matches an entry in kotlinKnownExternalRoots. Walks from
// longest to shortest by repeatedly trimming the trailing dotted
// segment. Returns "" when no prefix matches.
//
//	"io.ktor.server.routing.get" → "io.ktor"
//	"org.springframework.boot.SpringApplication"
//	                            → "org.springframework"
//	"com.acmecorp.users.UserService"
//	                            → "" (no prefix matches)
//	"kotlin.io.println"         → "kotlin"
func longestKnownKotlinPrefix(mod string) string {
	cur := strings.ToLower(mod)
	for cur != "" {
		if _, ok := kotlinKnownExternalRoots[cur]; ok {
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
