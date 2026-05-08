// Package resolve rewrites stub-form RelationshipRecord endpoint references
// (e.g. "View:User", "Model:Article", or a bare "Hello") into deterministic
// 16-char graph entity IDs by looking them up in the merged entity set.
//
// This is the substance of PORT-2-FIX (issue #24). PORT-2 produced thousands
// of relationships but every cross-file ToID was left as a stub string, so
// graph traversal dead-ended at the first cross-file reference. The resolver
// closes that gap.
//
// PORT-2-FIX-3 (issue #31) extends the resolver to handle two additional
// reference shapes emitted by Pass 3 cross-language extractors:
//
//   - Format A: scope:<kind>:<subtype>:<lang>:<file_path>:<name>
//   - Format B: scope:<kind>:<subtype>:<lang>:<file_path>:<scope_name>#<member_name>
//
// and adds a kind-hint code path (driven by the relationship's Kind field)
// that biases ambiguous bare-name lookups toward the kind families typically
// referenced by EXTENDS / IMPLEMENTS / CALLS edges.
package resolve

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// Disposition classifies the outcome the resolver assigned to an individual
// relationship endpoint. Every endpoint inspected by References() and
// ReferencesEmbedded() falls into exactly one bucket. The bug-rate metric
// (issue #44) is computed as (BugExtractor + BugResolver) / total.
type Disposition int

const (
	// DispositionResolved — the stub was rewritten to a 16-char entity ID.
	DispositionResolved Disposition = iota
	// DispositionExternalKnown — the endpoint points at an "ext:<pkg>"
	// placeholder AND the package is on the static external-package
	// allowlist (e.g. django, react, fmt).
	DispositionExternalKnown
	// DispositionExternalUnknown — the endpoint points at an "ext:<pkg>"
	// placeholder but the package is NOT on the allowlist. Likely a real
	// external dep we haven't catalogued yet.
	DispositionExternalUnknown
	// DispositionDynamic — the stub matches a pattern that is intrinsically
	// static-unresolvable (reflection, dynamic import, env-driven names,
	// template-built strings). Not a bug; the call cannot be resolved
	// statically by design.
	DispositionDynamic
	// DispositionBugExtractor — stub of form "Kind:Name" where the graph
	// has 0 entities with that Name. An extractor SHOULD have emitted an
	// entity but didn't. This is a bug to fix.
	DispositionBugExtractor
	// DispositionBugResolver — stub points at a Name that DOES exist in the
	// graph (potentially under different kinds), but the resolver couldn't
	// disambiguate it. Resolver bug.
	DispositionBugResolver
	// DispositionUnclassified — catch-all. Should be 0 in production runs;
	// non-zero values warrant investigation.
	DispositionUnclassified
)

// String returns a stable, log-friendly label for a Disposition.
func (d Disposition) String() string {
	switch d {
	case DispositionResolved:
		return "resolved"
	case DispositionExternalKnown:
		return "external-known"
	case DispositionExternalUnknown:
		return "external-unknown"
	case DispositionDynamic:
		return "dynamic"
	case DispositionBugExtractor:
		return "bug-extractor"
	case DispositionBugResolver:
		return "bug-resolver"
	case DispositionUnclassified:
		return "unclassified"
	}
	return "unknown"
}

// AllDispositions enumerates every Disposition value in canonical order.
// Used by the verbose log emitter so the breakdown is always printed in the
// same order regardless of map iteration randomness.
var AllDispositions = []Disposition{
	DispositionResolved,
	DispositionExternalKnown,
	DispositionExternalUnknown,
	DispositionDynamic,
	DispositionBugExtractor,
	DispositionBugResolver,
	DispositionUnclassified,
}

// Per-language dynamic-dispatch pattern catalogs (Refs #44).
//
// Matches here tag a stub as DispositionDynamic instead of bug-extractor /
// bug-resolver. The original Refs #44 commit used a single flat slice tested
// against every stub regardless of source language; that produced false
// positives (a Node `res.send("hello")` matched the Ruby `.send(` pattern,
// `repo.Lookup(id)` matched the Go `plugin.Lookup` pattern, etc.).
//
// The fix groups patterns by the language that owns the runtime-dispatch
// idiom. Patterns that are intrinsically reflective regardless of language
// (template-built names) live in crossLangDynamicPatterns. Receiver-anchored
// reflection APIs that have a unique fully-qualified shape (Go's
// `plugin.Lookup`, JVM `Method.invoke` / `Class.forName().newInstance()`)
// stay tight enough to be safe even when language is unknown — they go in
// the per-language slice plus a small "safe-anchored" cross-language slice.
//
// Language identifiers follow the structural-ref `<lang>:` segment
// convention: "python", "go", "javascript" (also "typescript"), "ruby",
// "java" (also "kotlin", "scala", "jvm"). Unknown / empty languages fall
// back to crossLangDynamicPatterns + safeAnchoredDynamicPatterns only.
var (
	pythonDynamicPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^getattr\(`),                  // getattr(obj, name)(...)
		regexp.MustCompile(`^__getattr__$`),               // __getattr__ magic name
		regexp.MustCompile(`^.*\.__getattr__\(`),          // obj.__getattr__("name")
		regexp.MustCompile(`^.*\.__getattribute__\(`),     // obj.__getattribute__(...)
		regexp.MustCompile(`^setattr\(`),                  // setattr-driven dispatch
		regexp.MustCompile(`^globals\(\)\[`),              // globals()[name](...)
		regexp.MustCompile(`^locals\(\)\[`),               // locals()[name](...)
		regexp.MustCompile(`^vars\(\)\[`),                 // vars()[name](...)
		regexp.MustCompile(`^eval\(`),                     // eval(...)
		regexp.MustCompile(`^exec\(`),                     // exec(...)
		regexp.MustCompile(`^__import__\(`),               // __import__("modname")
		regexp.MustCompile(`^importlib\.`),                // importlib.import_module / etc
		regexp.MustCompile(`^functools\.partial\(`),       // functools.partial(...)
		regexp.MustCompile(`^functools\.partialmethod\(`),
		regexp.MustCompile(`^functools\.reduce\(`),
		regexp.MustCompile(`^operator\.methodcaller\(`), // operator.methodcaller("name")
		regexp.MustCompile(`^operator\.attrgetter\(`),   // operator.attrgetter(...)
		regexp.MustCompile(`^operator\.itemgetter\(`),   // operator.itemgetter(...)
		regexp.MustCompile(`^os\.environ\[`),            // env-driven (Python)
		regexp.MustCompile(`^os\.getenv\(`),             // env-driven (Python)
		// dispatch via dict/list subscript: handlers[key](...), funcs["x"](...).
		// Anchored "<ident>[...](...)" so we don't bite plain attribute access.
		regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*\[[^\]]+\]\(`),
	}

	goDynamicPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^reflect\.`),       // reflect.* (Call, ValueOf, MethodByName, ...)
		regexp.MustCompile(`\.MethodByName\(`), // v.MethodByName("X").Call(...)
		regexp.MustCompile(`\.FieldByName\(`),  // v.FieldByName("X")
		regexp.MustCompile(`^plugin\.Open\(`),  // Go plugin loader
		// Anchored: only `plugin.Lookup(` (or `<x>.plugin.Lookup(`) — bare
		// `repo.Lookup(id)` / `cache.Lookup(...)` are NOT reflection.
		regexp.MustCompile(`\bplugin\.Lookup\(`),
	}

	jsDynamicPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^Reflect\.`),     // Reflect.apply / Reflect.construct / Reflect.get
		regexp.MustCompile(`^Function\(`),    // Function(src)
		regexp.MustCompile(`^new Function\(`), // new Function(src)
		// Dynamic import / require: must NOT be a literal-string first arg —
		// `require("fs")` and `import("./mod")` are statically resolvable.
		regexp.MustCompile("^require\\([^\"'`)]"),
		regexp.MustCompile("^import\\([^\"'`)]"),
		regexp.MustCompile(`^process\.env\.`), // env-driven (JS)
		// JS reflective `Function.prototype.{bind,apply,call}` is real, but
		// the bare `.bind(` / `.apply(` / `.call(` patterns collide with too
		// many domain methods (DB driver `bind`, `discount.apply(order)`,
		// `controller.call(...)`). Keep them out of the JS catalog; the
		// extractors tag truly reflective uses (e.g. `Reflect.apply`) which
		// the explicit `Reflect\.` pattern above already covers.
	}

	rubyDynamicPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^.*\.send\(`),        // obj.send(:name) — Ruby ONLY
		regexp.MustCompile(`^send\(`),            // bare send(:name)
		regexp.MustCompile(`^.*\.public_send\(`), // obj.public_send(:name)
		regexp.MustCompile(`^public_send\(`),
		regexp.MustCompile(`^.*\.__send__\(`),    // obj.__send__(:name)
		regexp.MustCompile(`^method_missing$`),   // ruby method_missing hook
		regexp.MustCompile(`^.*\.method_missing\(`),
		regexp.MustCompile(`^define_method\(`),   // metaprogramming
		regexp.MustCompile(`^.*\.define_method\(`),
		regexp.MustCompile(`^.*\.instance_eval\(`),
		regexp.MustCompile(`^.*\.class_eval\(`),
	}

	jvmDynamicPatterns = []*regexp.Regexp{
		// JVM reflection invoke is `Method.invoke(...)` or
		// `Constructor.invoke(...)`. Anchored to those receivers so a
		// user-defined `cli.invoke(...)` / `cmd.invoke(...)` does NOT match.
		regexp.MustCompile(`\b(?:Method|Constructor)\.invoke\(`),
		regexp.MustCompile(`^Class\.forName\(`), // Class.forName("...")
		// Anchored to the reflective `Class.forName(...).newInstance()` /
		// `<Type>.class.newInstance()` shape so a plain factory method
		// named `newInstance()` on a domain class does NOT match.
		regexp.MustCompile(`Class\.forName\([^)]*\)\.newInstance\(`),
		regexp.MustCompile(`\.class\.newInstance\(`),
		regexp.MustCompile(`^ServiceLoader\.load\(`), // ServiceLoader.load(...)
		regexp.MustCompile(`^System\.getenv\(`),      // env-driven (JVM)
	}

	// Cross-language patterns that are safe to evaluate when language is
	// unknown. Template-built names (`${x}` interpolation) are reflection-
	// shaped in every language that has them.
	crossLangDynamicPatterns = []*regexp.Regexp{
		regexp.MustCompile(`.*\$\{.*\}.*`), // template-built strings ${x}
	}

	// dynamicPatternsByLang dispatches a normalized language tag to its
	// per-language pattern slice. Keys are lower-case canonical names; the
	// resolver normalizes incoming tags before lookup.
	dynamicPatternsByLang = map[string][]*regexp.Regexp{
		"python":     pythonDynamicPatterns,
		"go":         goDynamicPatterns,
		"javascript": jsDynamicPatterns,
		"typescript": jsDynamicPatterns,
		"ruby":       rubyDynamicPatterns,
		"java":       jvmDynamicPatterns,
		"kotlin":     jvmDynamicPatterns,
		"scala":      jvmDynamicPatterns,
		"jvm":        jvmDynamicPatterns,
	}
)

// normalizeLang lowercases a language tag and maps a few common aliases to
// the canonical key used by dynamicPatternsByLang. Unknown tags pass
// through unchanged so the lookup miss falls through to the cross-language
// catalog.
func normalizeLang(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch l {
	case "py":
		return "python"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "rb":
		return "ruby"
	case "kt":
		return "kotlin"
	}
	return l
}

// inferLangFromStub extracts the language tag from a structural-ref stub
// (`scope:<kind>:<subtype>:<lang>:<file>:<tail>`). Returns "" for stubs that
// aren't structural refs.
func inferLangFromStub(stub string) string {
	if !strings.HasPrefix(stub, "scope:") {
		return ""
	}
	parts := strings.SplitN(stub, ":", 6)
	if len(parts) < 5 {
		return ""
	}
	return normalizeLang(parts[3])
}

// isDynamicPattern reports whether the stub matches any reflective /
// runtime-dispatch pattern. Equivalent to isDynamicPatternLang with a
// best-effort language inference (structural-ref segment when available;
// empty otherwise → cross-language catalog only).
func isDynamicPattern(stub string) bool {
	return isDynamicPatternLang(stub, inferLangFromStub(stub))
}

// isDynamicPatternLang gates pattern evaluation on the supplied language.
// When lang resolves to a known per-language catalog only that catalog plus
// the cross-language catalog runs; the receiver-anchored patterns inside
// each per-language slice are already tight enough to be safe.
//
// Empty / unknown languages run only the cross-language catalog. This is
// deliberately conservative: a language-agnostic call site like
// `res.send("hello")` (Node) or `repo.Lookup(id)` (Go domain code) must
// NOT be classified Dynamic without positive evidence.
func isDynamicPatternLang(stub, lang string) bool {
	if stub == "" {
		return false
	}
	for _, re := range crossLangDynamicPatterns {
		if re.MatchString(stub) {
			return true
		}
	}
	if patterns, ok := dynamicPatternsByLang[normalizeLang(lang)]; ok {
		for _, re := range patterns {
			if re.MatchString(stub) {
				return true
			}
		}
	}
	return false
}

// ExternalAllowlist is the function signature used by the resolver to
// decide whether an "ext:<pkg>" endpoint is a known package or not. The
// caller injects the actual allowlist (typically a wrapper around
// internal/external) so this package stays free of an upward import.
//
// The argument is the canonical package name with the "ext:" prefix already
// stripped. A nil ExternalAllowlist treats every external as Unknown.
type ExternalAllowlist func(pkg string) bool

// Index is a kind-aware (kind, name) -> entity_id lookup. The inner map only
// retains a name when the (kind, name) tuple resolves to exactly one entity;
// ambiguous tuples are tracked separately in the embedded ambig set so the
// resolver can leave them as stubs rather than silently picking a wrong match.
type Index struct {
	// byKind[kind][name] = entity_id (only when unique within that kind).
	byKind map[string]map[string]string
	// ambigKind[kind][name] = true when a (kind, name) tuple is ambiguous.
	ambigKind map[string]map[string]bool

	// byName[name] = entity_id (only when unique across ALL kinds). Used
	// for the kind-agnostic fallback when a stub has no "Kind:" prefix or
	// when the kind-specific lookup misses.
	byName map[string]string
	// ambigName[name] = true when a name appears in two or more entities.
	ambigName map[string]bool

	// nameKinds[name][kind] = entity_id for every entity sharing this
	// name. A blank string sentinel means two entities share that
	// (name, kind) tuple — i.e. the kind itself is ambiguous for this
	// name and the kind hint cannot disambiguate via this family.
	nameKinds map[string]map[string]string

	// byLocation[file_path][name] = entity_id, retained only when unique
	// within the file. Used by structural-ref Format A resolution.
	byLocation LocationIndex
	// ambigLocation[file_path][name] = true when (file, name) collides.
	ambigLocation map[string]map[string]bool

	// byLocationKind[file_path][name][kind] = entity_id. Kind-aware
	// (file, name) lookup. PORT-2-FIX-2 emissions can produce two entities
	// at the same (file, name) with different kinds (e.g. SCOPE.Component
	// class + SCOPE.Operation method); kind-aware lookup picks the correct
	// one when the relationship's kind hint maps to a single family.
	// A blank string sentinel marks (file, name, kind) collisions.
	byLocationKind LocationKindIndex

	// byMember[file_path][scope_name][member_name] = entity_id. Used by
	// structural-ref Format B resolution. A blank string sentinel marks
	// (scope, member) collisions inside the same file. Entities are
	// indexed by splitting their dotted Name on the first '.'.
	byMember map[string]map[string]map[string]string
}

// LocationIndex maps file_path -> name -> entity_id, retaining only entries
// that are unique within their file. Returned by BuildLocationIndex.
type LocationIndex map[string]map[string]string

// LocationKindIndex maps file_path -> name -> kind -> entity_id. Used by the
// kind-aware structural-ref / location resolver path to disambiguate
// same-file (file, name) collisions when the relationship supplies a kind
// hint. A blank string value is the ambiguous-within-kind sentinel.
type LocationKindIndex map[string]map[string]map[string]string

// Stats reports how many relationship endpoints the resolver rewrote and how
// many it left as stubs because of ambiguity / missing matches. Surfaced via
// the log line in cmd/archigraph/index.go for instrumentation.
//
// Rewritten/Ambiguous/Unmatched are aggregate counters covering every endpoint
// the resolver inspected (FromID + ToID combined). PORT-2-FIX-4 added the
// per-endpoint counters so callers can tell which side of an edge is failing
// to resolve.
type Stats struct {
	Rewritten int
	Ambiguous int
	Unmatched int

	FromRewritten int
	FromAmbiguous int
	FromUnmatched int
	ToRewritten   int
	ToAmbiguous   int
	ToUnmatched   int

	// VERIFY-2-PREP — every endpoint is also tagged with a Disposition.
	// DispositionCounts holds the tallies; DispositionSamples retains up
	// to 5 distinct representative stubs per disposition so the verbose
	// log can show concrete examples of each bucket. BugRate is the
	// (bug_extractor + bug_resolver) / total ratio surfaced as the v1.0
	// acceptance metric. Total here is the sum of every counter — the
	// number of endpoints the resolver inspected.
	DispositionCounts  map[Disposition]int
	DispositionSamples map[Disposition][]string
	BugRate            float64
}

// initDispositions lazily allocates the disposition maps. Cheap to call on
// every endpoint; we keep it explicit rather than relying on zero values so
// callers reading Stats.DispositionCounts on an unused endpoint never see a
// nil map.
func (s *Stats) initDispositions() {
	if s.DispositionCounts == nil {
		s.DispositionCounts = make(map[Disposition]int, len(AllDispositions))
	}
	if s.DispositionSamples == nil {
		s.DispositionSamples = make(map[Disposition][]string, len(AllDispositions))
	}
}

// recordDisposition adds one endpoint to the disposition tallies and stores
// the stub as a sample if fewer than 5 unique samples have been recorded
// for that disposition.
func (s *Stats) recordDisposition(d Disposition, stub string) {
	s.initDispositions()
	s.DispositionCounts[d]++
	const maxSamples = 5
	cur := s.DispositionSamples[d]
	if len(cur) >= maxSamples {
		return
	}
	for _, existing := range cur {
		if existing == stub {
			return
		}
	}
	s.DispositionSamples[d] = append(cur, stub)
}

// finalizeDispositions computes the BugRate field from the per-disposition
// counters. Called once at the end of References / ReferencesEmbedded.
func (s *Stats) finalizeDispositions() {
	if s.DispositionCounts == nil {
		return
	}
	var total int
	for _, n := range s.DispositionCounts {
		total += n
	}
	if total == 0 {
		s.BugRate = 0
		return
	}
	bugs := s.DispositionCounts[DispositionBugExtractor] +
		s.DispositionCounts[DispositionBugResolver]
	s.BugRate = float64(bugs) / float64(total)
}

// ClassifyEndpoints walks the supplied (fromID, toID) pairs and produces a
// Stats value populated only with disposition counters / samples / BugRate.
// The aggregate Rewritten/Ambiguous/Unmatched counters are NOT populated
// because by the time this is called the rewrite has already happened —
// callers wanting those numbers use Stats from References / ReferencesEmbedded.
//
// Endpoint pairs come from doc.Relationships AFTER external synthesis so
// "ext:" placeholders are already in place. allow is the external-package
// allowlist (typically external.IsKnownExternalPackage).
func (idx Index) ClassifyEndpoints(endpoints []EndpointPair, allow ExternalAllowlist) Stats {
	var stats Stats
	for _, ep := range endpoints {
		if ep.FromID != "" {
			d := idx.classifyDisposition(ep.FromID, ep.FromOriginal, allow)
			stub := ep.FromOriginal
			if stub == "" {
				stub = ep.FromID
			}
			stats.recordDisposition(d, stub)
		}
		if ep.ToID != "" {
			d := idx.classifyDisposition(ep.ToID, ep.ToOriginal, allow)
			stub := ep.ToOriginal
			if stub == "" {
				stub = ep.ToID
			}
			stats.recordDisposition(d, stub)
		}
	}
	stats.finalizeDispositions()
	return stats
}

// EndpointPair carries the post-rewrite IDs and pre-rewrite stubs for one
// relationship's endpoints. Used by ClassifyEndpoints when the caller has
// already finished resolving + synthesising and just wants disposition
// numbers over the final edge state.
type EndpointPair struct {
	FromID       string
	FromOriginal string
	ToID         string
	ToOriginal   string
}

// MergeDispositions sums the per-disposition counts and samples from src
// into dst. Sample lists are deduplicated and capped at 5 entries per
// disposition. BugRate is recomputed from the merged totals. Existing
// counter fields (Rewritten/Ambiguous/Unmatched + per-endpoint variants)
// are NOT touched — callers merge those explicitly.
func MergeDispositions(dst, src *Stats) {
	if dst == nil || src == nil || src.DispositionCounts == nil {
		if dst != nil {
			dst.finalizeDispositions()
		}
		return
	}
	dst.initDispositions()
	for d, n := range src.DispositionCounts {
		dst.DispositionCounts[d] += n
	}
	const maxSamples = 5
	for d, samples := range src.DispositionSamples {
		cur := dst.DispositionSamples[d]
	sampleLoop:
		for _, s := range samples {
			if len(cur) >= maxSamples {
				break
			}
			for _, existing := range cur {
				if existing == s {
					continue sampleLoop
				}
			}
			cur = append(cur, s)
		}
		dst.DispositionSamples[d] = cur
	}
	dst.finalizeDispositions()
}

// BuildIndex constructs a (kind, name) -> entity_id lookup from a slice of
// EntityRecords. Records whose ID field is empty are skipped — the caller is
// expected to populate ID with graph.EntityID(...) before calling BuildIndex.
//
// The returned Index handles two kind forms emitted by upstream extractors:
//
//   - Plain kind, e.g. "Function", "Class", "Model".
//   - SCOPE-prefixed kind, e.g. "SCOPE.View", "SCOPE.Service" — emitted by
//     Pass 3 cross-language extractors. The lookup strips the "SCOPE." prefix
//     so a stub like "View:User" matches an entity of kind "SCOPE.View".
func BuildIndex(entities []types.EntityRecord) Index {
	idx := Index{
		byKind:         make(map[string]map[string]string),
		ambigKind:      make(map[string]map[string]bool),
		byName:         make(map[string]string),
		ambigName:      make(map[string]bool),
		nameKinds:      make(map[string]map[string]string),
		byLocation:     make(LocationIndex),
		ambigLocation:  make(map[string]map[string]bool),
		byLocationKind: make(LocationKindIndex),
		byMember:       make(map[string]map[string]map[string]string),
	}
	for k := range entities {
		e := &entities[k]
		if e.ID == "" || e.Name == "" {
			continue
		}
		// Index under both the plain kind and the trimmed kind ("SCOPE.View"
		// → "View"), so stubs can match either form.
		kinds := []string{e.Kind}
		if trimmed := strings.TrimPrefix(e.Kind, "SCOPE."); trimmed != e.Kind && trimmed != "" {
			kinds = append(kinds, trimmed)
		}
		for _, kind := range kinds {
			if kind == "" {
				continue
			}
			if idx.ambigKind[kind] != nil && idx.ambigKind[kind][e.Name] {
				continue
			}
			bucket := idx.byKind[kind]
			if bucket == nil {
				bucket = make(map[string]string)
				idx.byKind[kind] = bucket
			}
			if existing, ok := bucket[e.Name]; ok && existing != e.ID {
				delete(bucket, e.Name)
				if idx.ambigKind[kind] == nil {
					idx.ambigKind[kind] = make(map[string]bool)
				}
				idx.ambigKind[kind][e.Name] = true
				continue
			}
			bucket[e.Name] = e.ID
		}

		// Track every (name, kind) -> id so the kind-hint fallback can
		// disambiguate when byName flips to ambiguous. The plain entity
		// kind is enough here; SCOPE.* kinds are tracked under both forms
		// to mirror the byKind dual-indexing above.
		nameKindBucket := idx.nameKinds[e.Name]
		if nameKindBucket == nil {
			nameKindBucket = make(map[string]string)
			idx.nameKinds[e.Name] = nameKindBucket
		}
		for _, kind := range kinds {
			if kind == "" {
				continue
			}
			// First writer wins per kind; if a second entity shares the
			// (name, kind) we mark the kind ambiguous for that name by
			// blanking the entry so the hint falls through.
			if existing, ok := nameKindBucket[kind]; ok && existing != e.ID {
				nameKindBucket[kind] = ""
			} else {
				nameKindBucket[kind] = e.ID
			}
		}

		// Location index — (file_path, name) -> entity_id. Same logic as
		// byKind: ambiguous (file, name) tuples are tracked separately so
		// the structural-ref resolver leaves the stub alone.
		if e.SourceFile != "" {
			// Kind-aware (file, name, kind) bucket — collision-safe under
			// PORT-2-FIX-2 emissions. Indexed under both raw and SCOPE-
			// trimmed kinds to mirror byKind.
			fileKindBucket := idx.byLocationKind[e.SourceFile]
			if fileKindBucket == nil {
				fileKindBucket = make(map[string]map[string]string)
				idx.byLocationKind[e.SourceFile] = fileKindBucket
			}
			nameKindBucketLoc := fileKindBucket[e.Name]
			if nameKindBucketLoc == nil {
				nameKindBucketLoc = make(map[string]string)
				fileKindBucket[e.Name] = nameKindBucketLoc
			}
			for _, kind := range kinds {
				if kind == "" {
					continue
				}
				if existing, ok := nameKindBucketLoc[kind]; ok && existing != e.ID {
					nameKindBucketLoc[kind] = "" // ambiguous within (file, name, kind)
				} else if !ok || existing == e.ID {
					nameKindBucketLoc[kind] = e.ID
				}
			}

			if idx.ambigLocation[e.SourceFile] == nil || !idx.ambigLocation[e.SourceFile][e.Name] {
				bucket := idx.byLocation[e.SourceFile]
				if bucket == nil {
					bucket = make(map[string]string)
					idx.byLocation[e.SourceFile] = bucket
				}
				if existing, ok := bucket[e.Name]; ok && existing != e.ID {
					delete(bucket, e.Name)
					if idx.ambigLocation[e.SourceFile] == nil {
						idx.ambigLocation[e.SourceFile] = make(map[string]bool)
					}
					idx.ambigLocation[e.SourceFile][e.Name] = true
				} else {
					bucket[e.Name] = e.ID
				}
			}

			// Member index — Format B references address a member of an
			// enclosing scope (class/module/etc.) by qualified name. Pass 3
			// records typically encode this as "<scope>.<member>" in the
			// Name field, so we split on the first '.'.
			if dot := strings.IndexByte(e.Name, '.'); dot > 0 {
				scope, member := e.Name[:dot], e.Name[dot+1:]
				fileBucket := idx.byMember[e.SourceFile]
				if fileBucket == nil {
					fileBucket = make(map[string]map[string]string)
					idx.byMember[e.SourceFile] = fileBucket
				}
				scopeBucket := fileBucket[scope]
				if scopeBucket == nil {
					scopeBucket = make(map[string]string)
					fileBucket[scope] = scopeBucket
				}
				if existing, ok := scopeBucket[member]; ok && existing != e.ID {
					scopeBucket[member] = "" // blank sentinel → ambiguous
				} else {
					scopeBucket[member] = e.ID
				}
			}
		}

		// Kind-agnostic name index. Two different entities sharing a name
		// (even across kinds) flips the name to ambiguous.
		if idx.ambigName[e.Name] {
			continue
		}
		if existing, ok := idx.byName[e.Name]; ok && existing != e.ID {
			delete(idx.byName, e.Name)
			idx.ambigName[e.Name] = true
			continue
		}
		idx.byName[e.Name] = e.ID
	}
	return idx
}

// BuildLocationIndex returns a (file_path, name) -> entity_id map built from
// the supplied entity slice. Entries that are not unique within their file
// are dropped. Exposed for callers that only need the location lookup.
func BuildLocationIndex(entities []types.EntityRecord) LocationIndex {
	loc := make(LocationIndex)
	ambig := make(map[string]map[string]bool)
	for k := range entities {
		e := &entities[k]
		if e.ID == "" || e.Name == "" || e.SourceFile == "" {
			continue
		}
		if ambig[e.SourceFile] != nil && ambig[e.SourceFile][e.Name] {
			continue
		}
		bucket := loc[e.SourceFile]
		if bucket == nil {
			bucket = make(map[string]string)
			loc[e.SourceFile] = bucket
		}
		if existing, ok := bucket[e.Name]; ok && existing != e.ID {
			delete(bucket, e.Name)
			if ambig[e.SourceFile] == nil {
				ambig[e.SourceFile] = make(map[string]bool)
			}
			ambig[e.SourceFile][e.Name] = true
			continue
		}
		bucket[e.Name] = e.ID
	}
	return loc
}

// Lookup resolves a stub string to an entity ID. The stub is split on the
// first ':' into (kind, name). If only the right-hand side is supplied (no
// ':' present) we fall back to the kind-agnostic name index.
//
// Returns (id, true) only when the lookup is unambiguous. Returns
// ("", false) when the stub has zero matches OR multiple matches — the
// caller leaves the original string in place in either case but tracks the
// outcome in Stats.
func (idx Index) Lookup(stub string) (string, bool) {
	if stub == "" {
		return "", false
	}
	kind, name := splitStub(stub)
	if kind != "" {
		if bucket, ok := idx.byKind[kind]; ok {
			if id, ok := bucket[name]; ok {
				return id, true
			}
		}
		if idx.ambigKind[kind] != nil && idx.ambigKind[kind][name] {
			// Ambiguous within this kind; fall through to kind-agnostic
			// only if the kind-agnostic name is itself unique.
		}
	}
	// Kind-agnostic fallback: bare name (no prefix) OR missed kind lookup.
	lookupName := name
	if kind == "" {
		lookupName = stub
	}
	if id, ok := idx.byName[lookupName]; ok {
		return id, true
	}
	return "", false
}

// LookupStatus reports whether a stub is unambiguous, ambiguous, or unmatched.
// Used by References to populate Stats counters without doing two passes.
func (idx Index) LookupStatus(stub string) (id string, status int) {
	return idx.LookupStatusHint(stub, "")
}

// LookupStatusHint is LookupStatus with an optional relationship-kind hint.
// The hint is the RelationshipRecord.Kind value (e.g. "EXTENDS", "CALLS"),
// not the entity kind. When the bare-name path would otherwise be ambiguous
// the hint biases the lookup toward the entity-kind family typically
// targeted by that relationship. The hint is ignored when the structural-ref
// path or an explicit Kind: prefix already resolves.
//
// When passed "" the function behaves exactly like LookupStatus.
func (idx Index) LookupStatusHint(stub, relKind string) (id string, status int) {
	const (
		statusRewritten = 1
		statusAmbiguous = 2
		statusUnmatched = 3
	)
	if stub == "" {
		return "", statusUnmatched
	}

	// Structural-ref forms (Format A / B). Recognised by the "scope:"
	// prefix and resolved through the location/member indexes — bypasses
	// the kind / name path entirely.
	if id, st, handled := idx.lookupStructural(stub); handled {
		return id, st
	}

	kind, name := splitStub(stub)
	if kind != "" {
		if bucket, ok := idx.byKind[kind]; ok {
			if id, ok := bucket[name]; ok {
				return id, statusRewritten
			}
		}
		if idx.ambigKind[kind] != nil && idx.ambigKind[kind][name] {
			return "", statusAmbiguous
		}
	}
	lookupName := name
	if kind == "" {
		lookupName = stub
	}
	if id, ok := idx.byName[lookupName]; ok {
		return id, statusRewritten
	}
	if idx.ambigName[lookupName] {
		// Ambiguous bare-name. Try the kind hint: pick a family that
		// the relKind biases toward, and if exactly one entity with this
		// name lives in that family, resolve to it.
		if id, ok := idx.lookupByKindHint(lookupName, relKind); ok {
			return id, statusRewritten
		}
		return "", statusAmbiguous
	}
	return "", statusUnmatched
}

// hintKinds returns the entity-kind families preferred for a given
// relationship kind. EXTENDS / IMPLEMENTS prefer Component-shaped kinds;
// CALLS prefers Operation-shaped kinds. Everything else returns nil.
func hintKinds(relKind string) []string {
	switch strings.ToUpper(relKind) {
	case "EXTENDS", "IMPLEMENTS":
		return []string{"Component", "Class", "View", "Model", "SCOPE.Component", "SCOPE.View", "SCOPE.Model"}
	case "CALLS":
		return []string{"Operation", "Function", "Method", "SCOPE.Operation"}
	}
	return nil
}

// lookupByKindHint disambiguates a name using the relKind hint. Returns
// (id, true) only when exactly one entity in the hinted family has this
// name; otherwise ("", false).
func (idx Index) lookupByKindHint(name, relKind string) (string, bool) {
	families := hintKinds(relKind)
	if len(families) == 0 {
		return "", false
	}
	bucket := idx.nameKinds[name]
	if len(bucket) == 0 {
		return "", false
	}
	var match string
	for _, k := range families {
		id := bucket[k]
		if id == "" {
			continue
		}
		if match != "" && match != id {
			// Two distinct entities in the hinted family share this
			// name — hint cannot disambiguate.
			return "", false
		}
		match = id
	}
	if match == "" {
		return "", false
	}
	return match, true
}

// lookupStructural resolves Format A / B references. Returns handled=false
// when the stub doesn't start with "scope:" so the caller falls through to
// the normal Kind:Name / bare-name path.
//
// Format A: scope:<kind>:<subtype>:<lang>:<file_path>:<name>
// Format B: scope:<kind>:<subtype>:<lang>:<file_path>:<scope_name>#<member_name>
func (idx Index) lookupStructural(stub string) (id string, status int, handled bool) {
	const (
		statusRewritten = 1
		statusAmbiguous = 2
		statusUnmatched = 3
	)
	if !strings.HasPrefix(stub, "scope:") {
		return "", 0, false
	}
	parts := strings.SplitN(stub, ":", 6)
	if len(parts) != 6 {
		return "", statusUnmatched, true
	}
	scopeKind := parts[1] // e.g. "component", "operation"
	filePath := parts[4]
	tail := parts[5]
	if filePath == "" || tail == "" {
		return "", statusUnmatched, true
	}

	// Format B: tail contains "#" → (scope_name, member_name).
	if hash := strings.IndexByte(tail, '#'); hash >= 0 {
		scopeName, memberName := tail[:hash], tail[hash+1:]
		if scopeName == "" || memberName == "" {
			return "", statusUnmatched, true
		}
		fileBucket := idx.byMember[filePath]
		if fileBucket == nil {
			return "", statusUnmatched, true
		}
		scopeBucket := fileBucket[scopeName]
		if scopeBucket == nil {
			return "", statusUnmatched, true
		}
		if id, ok := scopeBucket[memberName]; ok {
			if id == "" {
				return "", statusAmbiguous, true
			}
			return id, statusRewritten, true
		}
		return "", statusUnmatched, true
	}

	// Format A: tail is the entity name. Try the kind-aware location
	// index first using the structural-ref's scope-kind segment; this
	// resolves PORT-2-FIX-2 same-file collisions.
	if id, ok := idx.lookupLocationKind(filePath, tail, structuralKindFamilies(scopeKind)); ok {
		return id, statusRewritten, true
	}
	if idx.ambigLocation[filePath] != nil && idx.ambigLocation[filePath][tail] {
		return "", statusAmbiguous, true
	}
	if bucket, ok := idx.byLocation[filePath]; ok {
		if id, ok := bucket[tail]; ok {
			return id, statusRewritten, true
		}
	}
	return "", statusUnmatched, true
}

// structuralKindFamilies maps a scope-kind segment from a structural ref
// (e.g. "component", "operation") to the entity-kind families it might be
// indexed under. Returns nil for unknown segments.
func structuralKindFamilies(scopeKind string) []string {
	switch strings.ToLower(scopeKind) {
	case "component":
		return []string{"Component", "Class", "View", "Model", "SCOPE.Component", "SCOPE.View", "SCOPE.Model"}
	case "operation":
		return []string{"Operation", "Function", "Method", "SCOPE.Operation"}
	}
	return nil
}

// lookupLocationKind picks an entity by (file, name) constrained to the
// supplied kind families. Returns (id, true) only when exactly one family
// resolves to a non-blank entity ID for this (file, name).
func (idx Index) lookupLocationKind(filePath, name string, families []string) (string, bool) {
	if len(families) == 0 {
		return "", false
	}
	fileBucket := idx.byLocationKind[filePath]
	if fileBucket == nil {
		return "", false
	}
	nameBucket := fileBucket[name]
	if len(nameBucket) == 0 {
		return "", false
	}
	var match string
	for _, k := range families {
		id := nameBucket[k]
		if id == "" {
			continue
		}
		if match != "" && match != id {
			return "", false
		}
		match = id
	}
	if match == "" {
		return "", false
	}
	return match, true
}

// splitStub splits a stub string on the first ':' into (kind, name). If no
// ':' is present the full string is returned as the name and kind is empty.
func splitStub(s string) (kind, name string) {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// rewriteOne resolves a single endpoint reference. It returns the (possibly
// rewritten) ID string and the status code from LookupStatusHint. Hex IDs
// and empty strings short-circuit with a zero status, signalling "skip".
func (idx Index) rewriteOne(ref, relKind string) (string, int) {
	if ref == "" || isHexID(ref) {
		return ref, 0
	}
	id, st := idx.LookupStatusHint(ref, relKind)
	if st == 1 { // statusRewritten
		return id, st
	}
	return ref, st
}

// nameExists reports whether the supplied name appears anywhere in the
// graph, regardless of kind. Used by the disposition classifier to
// distinguish bug-extractor (no entity by this name exists) from
// bug-resolver (entity exists but lookup failed).
func (idx Index) nameExists(name string) bool {
	if name == "" {
		return false
	}
	if _, ok := idx.byName[name]; ok {
		return true
	}
	if idx.ambigName[name] {
		return true
	}
	if bucket, ok := idx.nameKinds[name]; ok && len(bucket) > 0 {
		return true
	}
	return false
}

// classifyDisposition returns the Disposition for an endpoint after the
// resolver has finished with it. resolvedID is the value the endpoint now
// carries (post-rewrite); originalStub is the value the endpoint had on
// entry. allow is the optional external-package allowlist.
//
// Equivalent to classifyDispositionLang with no caller-supplied language.
// Language is inferred from the stub itself (structural-ref `<lang>:`
// segment) when possible.
func (idx Index) classifyDisposition(resolvedID, originalStub string, allow ExternalAllowlist) Disposition {
	return idx.classifyDispositionLang(resolvedID, originalStub, "", allow)
}

// classifyDispositionLang is classifyDisposition with an explicit language
// tag (typically pulled from RelationshipRecord.Properties["language"] or
// the equivalent edge-level field). The language gates which per-language
// dynamic-dispatch catalog runs.
//
// Order of checks matters:
//  1. Already a 16-char hex → Resolved.
//  2. "ext:<pkg>" prefix → ExternalKnown / ExternalUnknown depending on allow.
//  3. Dynamic-pattern match on the original stub (gated by language) → Dynamic.
//  4. Stub of form "Kind:Name" or bare "Name" → BugExtractor when the name
//     has zero entities in the graph; BugResolver when it does.
//  5. Anything else → Unclassified.
func (idx Index) classifyDispositionLang(resolvedID, originalStub, lang string, allow ExternalAllowlist) Disposition {
	if isHexID(resolvedID) {
		return DispositionResolved
	}
	if strings.HasPrefix(resolvedID, "ext:") {
		pkg := strings.TrimPrefix(resolvedID, "ext:")
		// Collapse dotted submodules to root for the allowlist check.
		root := pkg
		if dot := strings.IndexByte(pkg, '.'); dot > 0 {
			root = pkg[:dot]
		}
		if allow != nil && (allow(pkg) || allow(root)) {
			return DispositionExternalKnown
		}
		return DispositionExternalUnknown
	}
	// Endpoint still carries its original stub (resolver left it alone).
	// Language preference order: caller-supplied tag (from the edge's
	// Properties["language"], threaded through ReferencesWithAllowlist),
	// then structural-ref `<lang>:` segment as fallback. Non-structural
	// stubs without a caller-supplied language run only the cross-language
	// catalog — see isDynamicPatternLang.
	effLang := lang
	if effLang == "" {
		effLang = inferLangFromStub(originalStub)
	}
	if isDynamicPatternLang(originalStub, effLang) {
		return DispositionDynamic
	}
	// Strip a "Kind:" prefix when present so the name-existence check is
	// kind-agnostic. Structural-ref ("scope:...") stubs pull their name
	// from the trailing segment after the last ':' or '#'.
	_, name := splitStub(originalStub)
	if strings.HasPrefix(originalStub, "scope:") {
		// scope:<kind>:<subtype>:<lang>:<file>:<tail>
		parts := strings.SplitN(originalStub, ":", 6)
		if len(parts) == 6 {
			tail := parts[5]
			if hash := strings.IndexByte(tail, '#'); hash >= 0 {
				name = tail[hash+1:]
			} else {
				name = tail
			}
		}
	}
	if name == "" {
		return DispositionUnclassified
	}
	if idx.nameExists(name) {
		return DispositionBugResolver
	}
	return DispositionBugExtractor
}

// applyEndpointStats records a single endpoint's outcome into the Stats
// counters, updating both the per-endpoint totals and the aggregate ones.
func applyEndpointStats(stats *Stats, status int, isFrom bool) {
	const (
		statusRewritten = 1
		statusAmbiguous = 2
		statusUnmatched = 3
	)
	switch status {
	case statusRewritten:
		stats.Rewritten++
		if isFrom {
			stats.FromRewritten++
		} else {
			stats.ToRewritten++
		}
	case statusAmbiguous:
		stats.Ambiguous++
		if isFrom {
			stats.FromAmbiguous++
		} else {
			stats.ToAmbiguous++
		}
	case statusUnmatched:
		stats.Unmatched++
		if isFrom {
			stats.FromUnmatched++
		} else {
			stats.ToUnmatched++
		}
	}
}

// References rewrites ToID and FromID values in rels in place. It returns
// per-endpoint stats — one rel with both endpoints rewritten counts twice in
// Stats.Rewritten (once per endpoint). The 16-char hex IDs already present
// (matching the shape of graph.EntityID output) are left untouched.
//
// This wrapper preserves the pre-VERIFY-2-PREP signature; callers that want
// disposition tagging should use ReferencesWithAllowlist.
func References(rels []types.RelationshipRecord, idx Index) Stats {
	return ReferencesWithAllowlist(rels, idx, nil)
}

// ReferencesWithAllowlist is References with an optional allowlist for
// classifying "ext:<pkg>" endpoints as ExternalKnown vs ExternalUnknown.
// A nil allowlist treats every external as Unknown.
func ReferencesWithAllowlist(rels []types.RelationshipRecord, idx Index, allow ExternalAllowlist) Stats {
	var stats Stats
	for k := range rels {
		r := &rels[k]
		lang := relLanguage(r)
		if r.FromID != "" && !isHexID(r.FromID) {
			orig := r.FromID
			newID, st := idx.rewriteOne(r.FromID, r.Kind)
			r.FromID = newID
			applyEndpointStats(&stats, st, true)
			d := idx.classifyDispositionLang(r.FromID, orig, lang, allow)
			stats.recordDisposition(d, orig)
		} else if isHexID(r.FromID) {
			stats.recordDisposition(DispositionResolved, r.FromID)
		}
		if r.ToID != "" && !isHexID(r.ToID) {
			orig := r.ToID
			newID, st := idx.rewriteOne(r.ToID, r.Kind)
			r.ToID = newID
			applyEndpointStats(&stats, st, false)
			d := idx.classifyDispositionLang(r.ToID, orig, lang, allow)
			stats.recordDisposition(d, orig)
		} else if isHexID(r.ToID) {
			stats.recordDisposition(DispositionResolved, r.ToID)
		}
	}
	stats.finalizeDispositions()
	return stats
}

// relLanguage extracts the source-language tag for a RelationshipRecord.
// Looks first at Properties["language"] (the canonical key emitted by the
// per-language extractors), then Properties["lang"] (legacy alias), then
// returns "" so the classifier falls back to structural-ref inference.
func relLanguage(r *types.RelationshipRecord) string {
	if r == nil || r.Properties == nil {
		return ""
	}
	if v, ok := r.Properties["language"]; ok && v != "" {
		return v
	}
	if v, ok := r.Properties["lang"]; ok && v != "" {
		return v
	}
	return ""
}

// ReferencesEmbedded walks every EntityRecord's embedded Relationships slice
// and applies the same resolver. Pass 1 extractors emit cross-file CALLS
// edges as embedded relationships, so this is where most of the rewriting
// happens on real codebases.
//
// PORT-2-FIX-4 extends this function to rewrite FromID in addition to ToID.
// Pass 3 cross-language extractors increasingly emit edges where the source
// endpoint is itself a stub (e.g. structural-ref Format A targeting an
// entity in another file). When FromID is empty the caller is still
// expected to substitute the parent entity ID at edge-emission time.
func ReferencesEmbedded(records []types.EntityRecord, idx Index) Stats {
	return ReferencesEmbeddedWithAllowlist(records, idx, nil)
}

// ReferencesEmbeddedWithAllowlist is ReferencesEmbedded with an optional
// external-package allowlist for disposition classification.
func ReferencesEmbeddedWithAllowlist(records []types.EntityRecord, idx Index, allow ExternalAllowlist) Stats {
	var stats Stats
	for k := range records {
		rels := records[k].Relationships
		// Embedded relationships inherit the parent entity's language when
		// the edge itself doesn't carry one — Pass 1 extractors emit edges
		// without a language property because their parent entity already
		// pins it.
		parentLang := records[k].Language
		for j := range rels {
			r := &rels[j]
			lang := relLanguage(r)
			if lang == "" {
				lang = parentLang
			}
			if r.FromID != "" && !isHexID(r.FromID) {
				orig := r.FromID
				newID, st := idx.rewriteOne(r.FromID, r.Kind)
				r.FromID = newID
				applyEndpointStats(&stats, st, true)
				d := idx.classifyDispositionLang(r.FromID, orig, lang, allow)
				stats.recordDisposition(d, orig)
			} else if isHexID(r.FromID) {
				stats.recordDisposition(DispositionResolved, r.FromID)
			}
			if r.ToID != "" && !isHexID(r.ToID) {
				orig := r.ToID
				newID, st := idx.rewriteOne(r.ToID, r.Kind)
				r.ToID = newID
				applyEndpointStats(&stats, st, false)
				d := idx.classifyDispositionLang(r.ToID, orig, lang, allow)
				stats.recordDisposition(d, orig)
			} else if isHexID(r.ToID) {
				stats.recordDisposition(DispositionResolved, r.ToID)
			}
		}
	}
	stats.finalizeDispositions()
	return stats
}

// isHexID reports whether s is a 16-char lower-hex string — the shape of
// graph.EntityID() output. Anything matching this shape is assumed to be an
// already-resolved entity ID and is left untouched.
func isHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
