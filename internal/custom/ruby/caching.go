package ruby

// caching.go — Rails caching-topology extraction (#3692, epic #3628, area #18).
//
// Captures the Rails low-level cache store API as read-through / invalidation
// topology, cross-language consistent with the Spring cache-region node
// (internal/custom/java/caching.go) and the Python cache-region node:
//
//	Rails.cache.fetch("home") { ... }   → cache-site CACHES key "home"
//	                                      (read-through: block result memoised)
//	Rails.cache.read("home")            → CACHES key "home"  (read)
//	Rails.cache.write("home", v)        → CACHES key "home"  (write)
//	Rails.cache.delete("home")          → INVALIDATES key "home"
//	Rails.cache.delete_matched("home/*")→ INVALIDATES key-prefix "home/*"
//
// `Rails.cache` and a bare `cache.fetch`/`cache.delete` inside a controller/view
// both resolve to the same store, so both receiver forms are accepted.
//
// Entity/edge shape:
//
//	target  : SCOPE.Datastore  subtype "cache_region"
//	          Ref  "cache:rails:<key>"   (sites converge on one node)
//	carrier : SCOPE.Operation  the cache call-site  (owner of the edge)
//	edge    : CACHES | INVALIDATES   site → key
//
// Dynamic keys (`Rails.cache.fetch(user_key)` / interpolated "user/#{id}") are
// honest-partial: the edge is emitted with dynamic="true" against a key-prefix
// (interpolation head) or "<dynamic>" target so the intent stays traversable.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_ruby_caching", &railsCachingExtractor{})
}

type railsCachingExtractor struct{}

func (e *railsCachingExtractor) Language() string { return "custom_ruby_caching" }

// reRailsCacheOp matches a Rails cache store call. Group 1 = receiver
// (`Rails.cache` or `cache`); Group 2 = verb; Group 3 = the first-argument key
// literal body (with surrounding quotes captured in the literal alternation).
// The receiver `Rails.cache` is required, OR a bare `cache.<verb>` — the bare
// form is gated on the file containing `Rails.cache` somewhere (so we don't
// match unrelated `cache.foo` on arbitrary objects).
var reRailsCacheOp = regexp.MustCompile(
	`(Rails\.cache|cache)\.(fetch|read|write|delete|delete_matched|read_multi|write_multi|exist\?)\s*\(\s*(["'][^"']*["']|"[^"]*#\{)`)

// railsReadVerbs/railsWriteVerbs/railsInvalidateVerbs classify the verb into the
// cache edge direction + mode.
var (
	railsReadVerbs       = map[string]bool{"fetch": true, "read": true, "read_multi": true, "exist?": true}
	railsWriteVerbs      = map[string]bool{"write": true, "write_multi": true}
	railsInvalidateVerbs = map[string]bool{"delete": true, "delete_matched": true}
)

func railsCacheRef(key string) string {
	return "cache:rails:" + key
}

func (e *railsCachingExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.ruby_caching")
	_, span := tracer.Start(ctx, "custom.ruby_caching")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "Rails.cache") && !strings.Contains(src, ".cache.") {
		return nil, nil
	}
	hasRailsCache := strings.Contains(src, "Rails.cache")

	var out []types.EntityRecord
	seenRegion := make(map[string]bool)
	add := func(e types.EntityRecord) { out = append(out, e) }

	emitRegion := func(key string, dynamic bool, line int) string {
		ref := railsCacheRef(key)
		if !seenRegion[ref] {
			seenRegion[ref] = true
			ent := makeEntity(key, "SCOPE.Datastore", "cache_region", file.Path, file.Language, line)
			setProps(&ent, "framework", "rails", "region", key,
				"cache_kind", "region", "provenance", "INFERRED_FROM_RAILS_CACHE")
			if dynamic {
				setProps(&ent, "dynamic", "true")
			}
			add(ent)
		}
		return ref
	}

	for _, m := range reRailsCacheOp.FindAllStringSubmatchIndex(src, -1) {
		receiver := src[m[2]:m[3]]
		verb := src[m[4]:m[5]]
		rawKey := src[m[6]:m[7]]
		line := lineOf(src, m[0])

		// Bare `cache.<verb>` only counts when the file uses Rails.cache somewhere.
		if receiver == "cache" && !hasRailsCache {
			continue
		}

		// Resolve the key literal. A trailing `#{` marks an interpolated key —
		// the static head before it is a key-prefix (dynamic); a clean literal
		// is a concrete key.
		key, dynamic := normaliseRailsCacheKey(rawKey)

		relType := "CACHES"
		mode := "read_through"
		switch {
		case railsInvalidateVerbs[verb]:
			relType = "INVALIDATES"
			mode = "evict"
			if verb == "delete_matched" {
				mode = "evict_matched"
			}
		case railsWriteVerbs[verb]:
			mode = "write"
		case railsReadVerbs[verb]:
			mode = "read_through"
		}

		siteRef := fmt.Sprintf("rails_cache_op:%s:%d", file.Path, line)
		ent := makeEntity(receiver+"."+verb, "SCOPE.Operation", "cache_method", file.Path, file.Language, line)
		setProps(&ent, "framework", "rails", "cache_verb", verb, "cache_mode", mode,
			"provenance", "INFERRED_FROM_RAILS_CACHE", "ref", siteRef)
		if dynamic {
			setProps(&ent, "dynamic", "true")
		}

		targetRef := emitRegion(key, dynamic, line)
		edgeProps := types.Props{
			{K: "framework", V: "rails"},
			{K: "language", V: file.Language},
			{K: "mode", V: mode},
			{K: "region", V: key},
		}
		if dynamic {
			edgeProps.Set("dynamic", "true")
		}
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID:       targetRef,
			Kind:       relType,
			Properties: edgeProps,
		})
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// normaliseRailsCacheKey resolves the captured first-argument literal into a
// (label, dynamic) pair:
//
//	"home"        -> ("home", false)
//	'users/index' -> ("users/index", false)
//	"user/#{      -> ("user/*", true)   (interpolation head before #{)
//	"#{           -> ("<dynamic>", true)
func normaliseRailsCacheKey(raw string) (string, bool) {
	// Strip surrounding quotes (the capture may be a full literal `"user/#{id}"`
	// or a cut interpolation head `"user/#{`).
	body := strings.Trim(raw, `"'`)
	// Interpolated literal: a `#{` anywhere means the key is dynamic; the static
	// head before the first interpolation is the key-prefix.
	if i := strings.Index(body, "#{"); i >= 0 {
		head := strings.TrimRight(body[:i], `"'`)
		if head == "" {
			return "<dynamic>", true
		}
		return head + "*", true
	}
	if body == "" {
		return "<dynamic>", true
	}
	return body, false
}
