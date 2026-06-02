// http_endpoint_graphql_ruby.go — graphql-ruby server → http_endpoint_definition synthesis.
//
// graphql-ruby (https://graphql-ruby.org, the `graphql` gem) is the dominant
// GraphQL server framework for Ruby. Schemas are code-first: the three GraphQL
// root operation types are Ruby classes subclassing a project `BaseObject`
// (itself a subclass of `GraphQL::Schema::Object`), conventionally named
// `Types::QueryType`, `Types::MutationType`, and `Types::SubscriptionType` and
// registered on the schema via `query(Types::QueryType)` /
// `mutation(Types::MutationType)` / `subscription(Types::SubscriptionType)`:
//
//	module Types
//	  class QueryType < Types::BaseObject
//	    field :users, [Types::UserType], null: false
//	    def users
//	      User.all
//	    end
//
//	    field :user, Types::UserType, null: true do
//	      argument :id, ID, required: true
//	    end
//	    def user(id:)
//	      User.find(id)
//	    end
//	  end
//
//	  class MutationType < Types::BaseObject
//	    field :create_user, Types::UserType, null: false
//	    def create_user(name:)
//	      User.create(name: name)
//	    end
//	  end
//	end
//
// Each `field :<name>` declared on a root type is a GraphQL operation. We map it
// to the canonical operation-endpoint shape shared with every other GraphQL
// server archigraph indexes — the JS/TS Apollo server (synthesizeGraphQLResolvers),
// the Python Strawberry server (synthesizeStrawberry), the Go gqlgen server
// (synthesizeGqlgen), the C# HotChocolate server (synthesizeHotChocolate) and
// the Elixir Absinthe server:
//
//	http:GRAPHQL:/graphql/<RootType>/<field>
//
// where RootType is Query / Mutation / Subscription (derived from the class
// name's `Query|Mutation|Subscription` prefix, stripping the conventional
// `Type` suffix). Emitting the identical id shape is what lets the GraphQL
// client-link synthesizer and the cross-repo linker join Ruby GraphQL servers
// to their consumers.
//
// Handler attribution: graphql-ruby resolves a `field :name` to the instance
// method `def name` on the same type by default (the method name equals the
// field name). We attribute the endpoint to that method, referencing it as
// `SCOPE.Operation:<field>` plus a same-file `handler_file` hint (the field
// and its resolver method are declared together in the type class), mirroring
// the Rails/Sinatra cross-file attribution mechanism. The shared resolver
// post-pass rebinds this into a HANDLES edge against the extracted Ruby method
// entity.
//
// Detection is gated on a graphql-ruby file-signal (a `GraphQL::Schema` /
// `BaseObject` / `field :` marker on a `*Type < *BaseObject` class) so the
// synthesizer is a no-op on every other Ruby file. Honest-partial: fields whose
// resolver is dynamically generated (e.g. via `Types::BaseObject.field` macros
// or delegated resolvers) still yield an endpoint, but the handler ref points at
// the conventional same-name method and is dropped by the resolver if no such
// method exists.
//
// Closes #3621 (epic #3607).
package engine

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Fast-path gate
// ---------------------------------------------------------------------------

// graphqlRubyGateRe matches a graphql-ruby file signal: a class subclassing a
// `*BaseObject` (the conventional base for graphql-ruby type classes) or a
// direct `GraphQL::Schema::Object` subclass, OR a `field :` declaration. We
// require at least one of these markers before scanning so the synthesizer
// no-ops on arbitrary Ruby (and on Rails/Sinatra route files, which never carry
// a `< *BaseObject` superclass on a `*Type` class).
var graphqlRubyGateRe = regexp.MustCompile(
	`(?m)<\s*(?:[\w:]*BaseObject|GraphQL::Schema::Object)\b|^\s*field\s+:`,
)

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

// graphqlRubyRootClassRe matches a root operation type class declaration:
//
//	class QueryType < Types::BaseObject
//	class Types::MutationType < Types::BaseObject
//	class SubscriptionType < GraphQL::Schema::Object
//
// graphql-ruby's convention names the three root types `QueryType`,
// `MutationType`, `SubscriptionType` (the `Type` suffix is conventional but the
// schema registration `query(...)` is what actually designates them; we key on
// the conventional name, which is universal in real apps). The class may be
// namespaced (`Types::QueryType`) — we capture only the trailing operation word.
//
// Capture group 1 = the root operation word (Query | Mutation | Subscription).
var graphqlRubyRootClassRe = regexp.MustCompile(
	`(?m)^\s*class\s+(?:[\w:]+::)?(Query|Mutation|Subscription)Type\s*<\s*[\w:]*(?:BaseObject|GraphQL::Schema::Object)\b`,
)

// graphqlRubyFieldRe matches a `field :name` declaration inside a type class
// body. graphql-ruby field names are Ruby symbols (snake_case by convention).
// The declaration may carry a return type and keyword options after the symbol;
// we capture only the field name. A trailing `do ... end` resolver block (rare)
// or inline options are tolerated because we anchor on the `field :<name>`
// prefix.
//
// Capture group 1 = the field name (the resolver method name by default).
var graphqlRubyFieldRe = regexp.MustCompile(
	`(?m)^\s*field\s+:([a-zA-Z_]\w*)\b`,
)

// graphqlRubyClassOpenRe matches any `class`/`module` opener — used to find the
// extent of a root type class body (the body ends at the matching `end`, but a
// line-counted scan to the next class/module-at-same-or-lower-indent boundary is
// sufficient for the flat, convention-driven type files graphql-ruby produces).
var graphqlRubyClassOpenRe = regexp.MustCompile(`(?m)^(\s*)(?:class|module)\s`)

// ---------------------------------------------------------------------------
// Synthesizer
// ---------------------------------------------------------------------------

// synthesizeGraphQLRuby scans a Ruby file for graphql-ruby root operation type
// classes (QueryType / MutationType / SubscriptionType) and emits one
// http_endpoint_definition per `field :<name>` declared in each, in the
// canonical `http:GRAPHQL:/graphql/<Root>/<field>` shape. The handler is the
// same-name resolver method (`def <name>`), attributed as
// `SCOPE.Operation:<field>` with a same-file `handler_file` hint.
//
// `path` is the Ruby file path; it is supplied as the handler_file hint because
// graphql-ruby declares the `field` and its `def` resolver together in the type
// class (same file by construction).
func synthesizeGraphQLRuby(content, path string, emit emitFileFn) {
	if !graphqlRubyGateRe.MatchString(content) {
		return
	}

	// Find each root operation type class and the byte extent of its body.
	classMatches := graphqlRubyRootClassRe.FindAllStringSubmatchIndex(content, -1)
	if len(classMatches) == 0 {
		return
	}

	// Pre-compute the indentation + offset of every class/module opener so we
	// can bound each root class body at the next opener of the same or lower
	// indentation (or EOF).
	openers := graphqlRubyClassOpenRe.FindAllStringSubmatchIndex(content, -1)

	for _, cm := range classMatches {
		root := content[cm[2]:cm[3]] // "Query" | "Mutation" | "Subscription"
		classHeaderStart := cm[0]
		bodyStart := cm[1]

		// Determine this class's indentation (leading whitespace of the
		// `class` line) so we can find the matching close boundary.
		classIndent := leadingIndentAt(content, classHeaderStart)

		// Body ends at the next class/module opener whose indentation is <=
		// this class's indentation, or EOF.
		bodyEnd := len(content)
		for _, om := range openers {
			if om[0] <= classHeaderStart {
				continue
			}
			indent := content[om[2]:om[3]]
			if len(indent) <= len(classIndent) {
				bodyEnd = om[0]
				break
			}
		}
		body := content[bodyStart:bodyEnd]

		// Emit one endpoint per `field :<name>` in the class body, de-duped by
		// field name (graphql-ruby disallows duplicate field names on a type).
		seen := map[string]bool{}
		for _, fm := range graphqlRubyFieldRe.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 4 {
				continue
			}
			field := body[fm[2]:fm[3]]
			if seen[field] {
				continue
			}
			seen[field] = true

			// Absolute def-line of the field declaration in the file.
			fieldOffsetInFile := bodyStart + fm[0]
			defLine := lineOfOffset(content, fieldOffsetInFile)

			routePath := "/graphql/" + root + "/" + field
			canonical := httproutes.Canonicalize(httproutes.FrameworkGraphQLRuby, routePath)
			// Handler ref: the same-name resolver method `def <field>` on the
			// type class. SCOPE.Operation is the kind the Ruby extractor lands
			// instance methods under; the same-file handler_file hint
			// disambiguates the (common) shared field names across types.
			emit("GRAPHQL", canonical, "graphql-ruby", "SCOPE.Operation", field, path, defLine)
		}
	}
}

// leadingIndentAt returns the leading whitespace (spaces/tabs) of the line that
// contains byte offset off.
func leadingIndentAt(content string, off int) string {
	// Walk back to the start of the line.
	lineStart := off
	for lineStart > 0 && content[lineStart-1] != '\n' {
		lineStart--
	}
	i := lineStart
	for i < len(content) && (content[i] == ' ' || content[i] == '\t') {
		i++
	}
	return content[lineStart:i]
}
