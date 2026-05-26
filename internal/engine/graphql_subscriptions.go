// GraphQL subscription entity + edge synthesis (#727).
//
// Emits:
//   - Subscription entities — identity `graphql_sub:<field_name>`. The
//     cross-repo linker matches by Name; server-side schema definitions
//     and client-side subscription calls collapse to the same identity.
//   - GRAPHQL_PUBLISHES edges — server resolver / schema → Subscription.
//   - GRAPHQL_SUBSCRIBES edges — client component / function → Subscription.
//
// Server patterns:
//   - GraphQL SDL: `type Subscription { <field>: <Type> ... }` in .graphql,
//     .graphqls, .gql files, or inside a `gql\`...\“ / `gql("...")` /
//     `buildSchema(\`...\`)` literal. We extract every field name under
//     the Subscription type.
//   - Apollo Server / graphql-yoga resolvers: `Subscription: { <field>: { subscribe(...) } }`
//     or `Subscription: { <field>: { subscribe: () => pubsub.asyncIterator(...) } }`.
//     The leading `Subscription:` key in a resolvers object is the anchor.
//   - Hasura / PostGraphile: identified by config files / SDL imports
//     (treated as schema source).
//
// Client patterns:
//   - GraphQL operation document: `subscription <Name> { <field>(args) { ... } }`
//     inside `gql\`...\“ / `graphql(\`...\`)` / `useSubscription(\`...\`)` /
//     `useSubscription(gql\`...\`)`. We extract the top-level selection
//     field as the subscription identity.
//   - Apollo `useSubscription(DOCUMENT)` / urql `useSubscription({ query: ... })`.
//   - Subscription parameters captured as `args` property (best-effort:
//     comma-separated argument names from the top selection).
//
// Beyond-minimum:
//   - Filter args on the subscription selection are surfaced via the `args`
//     property on GRAPHQL_SUBSCRIBES (e.g. `messageAdded(channelId: $cid)` →
//     args=channelId).
//   - On the server side, when a `withFilter(asyncIterator, filterFn)` wrapper
//     is present, we set `filtered=true` on the GRAPHQL_PUBLISHES edge.
//
// Refs #727.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

const graphqlSubscriptionKind = "Subscription"
const graphqlSubscriptionIDPrefix = "graphql_sub:"

// applyGraphQLSubscriptionSynthesis runs per-file. Append-only.
func applyGraphQLSubscriptionSynthesis(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := string(content)

	seen := map[string]int{} // id → index in entities slice
	emitSub := func(fieldName, framework, args string) string {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			return ""
		}
		id := graphqlSubscriptionIDPrefix + fieldName
		if idx, ok := seen[id]; ok {
			// Backfill args on the entity if a later sighting carries them.
			if args != "" && entities[idx].Properties["args"] == "" {
				entities[idx].Properties["args"] = args
			}
			return id
		}
		props := map[string]string{
			"field_name":   fieldName,
			"framework":    framework,
			"pattern_type": "graphql_subscription_synthesis",
		}
		if args != "" {
			props["args"] = args
		}
		entities = append(entities, types.EntityRecord{
			ID:                 id,
			Name:               id,
			Kind:               graphqlSubscriptionKind,
			SourceFile:         path,
			Language:           lang,
			Properties:         props,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
		seen[id] = len(entities) - 1
		return id
	}

	emitEdge := func(kind, fromID, toID string, props map[string]string) {
		if fromID == "" || toID == "" {
			return
		}
		if props == nil {
			props = map[string]string{}
		}
		props["pattern_type"] = "graphql_subscription_synthesis"
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       kind,
			Properties: props,
		})
	}

	// Schema-style server SDL — language-agnostic; runs for any file because
	// .graphql is sometimes embedded in JS/TS source.
	synthGraphQLSchema(src, path, lang, emitSub, emitEdge)
	synthGraphQLResolvers(src, path, lang, emitSub, emitEdge)
	synthGraphQLClientSubscriptions(src, path, lang, emitSub, emitEdge)

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// ---------------------------------------------------------------------------
// SDL: type Subscription { ... }
// ---------------------------------------------------------------------------

// graphqlSubscriptionTypeRe captures the body of `type Subscription { ... }`
// in a GraphQL SDL document. We match up to the next top-level `}` — SDL
// type bodies do not nest braces, so naive counting is fine. The body is
// scanned for `<field>(args?): <Type>` declarations.
var graphqlSubscriptionTypeRe = regexp.MustCompile(
	`(?m)^\s*type\s+Subscription\s*(?:implements[^{]*)?\{([\s\S]*?)\n\s*\}`,
)

// graphqlSdlFieldRe captures a single field declaration inside an SDL type
// body: `<name>(args)?: <Type>`. Group 1 = field name; group 2 = optional
// argument block (may be empty).
var graphqlSdlFieldRe = regexp.MustCompile(
	`(?m)^\s*(\w+)\s*(?:\(([^)]*)\))?\s*:\s*[\w!\[\]]+`,
)

func synthGraphQLSchema(
	src, path, lang string,
	emitSub func(field, framework, args string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	// Lightweight prefilter: file must mention "type Subscription".
	if !strings.Contains(src, "type Subscription") {
		return
	}
	for _, body := range graphqlSubscriptionTypeRe.FindAllStringSubmatch(src, -1) {
		if len(body) < 2 {
			continue
		}
		for _, f := range graphqlSdlFieldRe.FindAllStringSubmatch(body[1], -1) {
			if len(f) < 2 {
				continue
			}
			field := f[1]
			// Skip GraphQL keyword tokens that may appear in malformed bodies.
			if field == "type" || field == "input" || field == "enum" {
				continue
			}
			args := ""
			if len(f) >= 3 {
				args = collectArgNames(f[2])
			}
			id := emitSub(field, "graphql_sdl", args)
			if id == "" {
				continue
			}
			props := map[string]string{
				"framework":  "graphql_sdl",
				"field_name": field,
			}
			if args != "" {
				props["args"] = args
			}
			emitEdge(
				string(types.RelationshipKindGraphQLPublishes),
				"Schema:Subscription."+field,
				id,
				props,
			)
		}
	}
}

// collectArgNames pulls argument identifiers from an SDL argument list
// (e.g. `channelId: ID!, userId: ID`) and returns them as a comma-separated
// string. Used to surface filter args as edge metadata.
func collectArgNames(s string) string {
	if s == "" {
		return ""
	}
	var names []string
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		colon := strings.IndexByte(raw, ':')
		if colon <= 0 {
			continue
		}
		names = append(names, strings.TrimSpace(raw[:colon]))
	}
	return strings.Join(names, ",")
}

// ---------------------------------------------------------------------------
// Resolvers: Subscription: { <field>: { subscribe(...) } }
// ---------------------------------------------------------------------------

// graphqlResolverBlockRe captures `Subscription: { ... }` inside a JS/TS
// resolvers object. We match a brace-balanced body using a tolerant
// non-greedy pattern bounded to 8KB — resolver objects can be large.
var graphqlResolverBlockRe = regexp.MustCompile(
	`(?m)\bSubscription\s*:\s*\{`,
)

// graphqlResolverFieldRe captures the field names declared inside the
// resolver block: `<name>: {` or `<name>: { subscribe`.
var graphqlResolverFieldRe = regexp.MustCompile(
	`(?m)^\s*(\w+)\s*:\s*\{[\s\S]{0,80}?(?:subscribe|resolve)\b`,
)

func synthGraphQLResolvers(
	src, path, lang string,
	emitSub func(field, framework, args string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "Subscription:") && !strings.Contains(src, "Subscription :") {
		return
	}
	for _, m := range graphqlResolverBlockRe.FindAllStringIndex(src, -1) {
		// Walk forward to find the matching `}`.
		openIdx := m[1] - 1
		if openIdx < 0 || openIdx >= len(src) || src[openIdx] != '{' {
			continue
		}
		closeIdx := findMatchingBrace(src, openIdx)
		if closeIdx < 0 {
			continue
		}
		body := src[openIdx+1 : closeIdx]
		filtered := strings.Contains(body, "withFilter(")
		for _, f := range graphqlResolverFieldRe.FindAllStringSubmatch(body, -1) {
			if len(f) < 2 {
				continue
			}
			field := f[1]
			id := emitSub(field, "apollo_resolver", "")
			if id == "" {
				continue
			}
			props := map[string]string{
				"framework":  "apollo_resolver",
				"field_name": field,
			}
			if filtered {
				props["filtered"] = "true"
			}
			emitEdge(
				string(types.RelationshipKindGraphQLPublishes),
				"Function:resolver_"+field,
				id,
				props,
			)
		}
	}
}

// ---------------------------------------------------------------------------
// Client: subscription <Name> { <field>(args) { ... } }
// ---------------------------------------------------------------------------

// graphqlSubscriptionOpRe captures the top-level field selected by a
// `subscription` operation, regardless of whether the operation is named.
// Examples accepted:
//   - `subscription { messageAdded { id text } }`
//   - `subscription OnMessage { messageAdded(channelId: $cid) { id } }`
//   - `subscription OnMessage($cid: ID!) { messageAdded(channelId: $cid) { id } }`
//
// Capture: 1 = field name, 2 = optional arg list (may be empty).
var graphqlSubscriptionOpRe = regexp.MustCompile(
	`\bsubscription\b(?:\s+\w+)?(?:\s*\([^)]*\))?\s*\{\s*(\w+)(?:\s*\(([^)]*)\))?`,
)

// graphqlClientHookRe is a softer anchor that establishes the file is
// using a GraphQL client library. We don't require this match — the SDL
// operation regex is enough — but we use it to choose the framework label.
var graphqlClientHookRe = regexp.MustCompile(
	`\b(?:useSubscription|graphql-request|urql|apollo|graphql-yoga|graphql-subscriptions)\b`,
)

func synthGraphQLClientSubscriptions(
	src, path, lang string,
	emitSub func(field, framework, args string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	// Lightweight prefilter — file must mention `subscription` as a keyword.
	// SDL `type Subscription` doesn't match the lowercase form below, so we
	// won't double-fire from schema files.
	if !regexp.MustCompile(`\bsubscription\s`).MatchString(src) &&
		!regexp.MustCompile("\\bsubscription\\s*\\{").MatchString(src) {
		return
	}

	framework := "graphql_client"
	if graphqlClientHookRe.MatchString(src) {
		switch {
		case strings.Contains(src, "@apollo/client") || strings.Contains(src, "ApolloClient"):
			framework = "apollo_client"
		case strings.Contains(src, "urql"):
			framework = "urql"
		case strings.Contains(src, "graphql-request"):
			framework = "graphql_request"
		}
	}

	funcs := indexJSEnclosingFunctions(src)

	for _, m := range graphqlSubscriptionOpRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		field := src[m[2]:m[3]]
		// Reject the SDL `type Subscription` capture form, just in case.
		if field == "" || field == "Subscription" {
			continue
		}
		args := ""
		if len(m) >= 6 && m[4] >= 0 {
			args = collectGraphQLCallArgNames(src[m[4]:m[5]])
		}
		id := emitSub(field, framework, args)
		if id == "" {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		if lang == "python" {
			caller = enclosingPyFuncForOffset(src, m[0])
		}
		fromID := "Function:" + caller
		if caller == "" {
			fromID = "Function:" + sanitiseID(path)
		}
		props := map[string]string{
			"framework":  framework,
			"field_name": field,
		}
		if args != "" {
			props["args"] = args
		}
		emitEdge(
			string(types.RelationshipKindGraphQLSubscribes),
			fromID,
			id,
			props,
		)
	}
}

// collectGraphQLCallArgNames takes the contents of a `<field>(args)` call
// and returns the comma-joined argument names. Args look like
// `channelId: $cid, userId: $uid` — we take the LHS of each colon.
func collectGraphQLCallArgNames(s string) string {
	if s == "" {
		return ""
	}
	var names []string
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		colon := strings.IndexByte(raw, ':')
		if colon <= 0 {
			continue
		}
		names = append(names, strings.TrimSpace(raw[:colon]))
	}
	return strings.Join(names, ",")
}
