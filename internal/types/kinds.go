package types

// EntityKind enumerates the SCOPE.* entity kinds emitted by archigraph
// extractors. Producers should use these typed constants rather than
// free-form string literals so that drift between code and SCHEMA.md
// (Issue #77) stays detectable at compile time.
//
// The on-disk graph keeps the namespaced "SCOPE." form. The MCP rendering
// layer strips the prefix when surfacing kinds to the agent (ADR-0003).
type EntityKind string

const (
	EntityKindOperation     EntityKind = "SCOPE.Operation"
	EntityKindComponent     EntityKind = "SCOPE.Component"
	EntityKindClass         EntityKind = "SCOPE.Class"
	EntityKindFunction      EntityKind = "SCOPE.Function"
	EntityKindSchema        EntityKind = "SCOPE.Schema"
	EntityKindVariable      EntityKind = "SCOPE.Variable"
	EntityKindReference     EntityKind = "SCOPE.Reference"
	EntityKindPattern       EntityKind = "SCOPE.Pattern"
	EntityKindEvolution     EntityKind = "SCOPE.Evolution"
	EntityKindEndpoint      EntityKind = "SCOPE.Endpoint"
	EntityKindRoute         EntityKind = "SCOPE.Route"
	EntityKindService       EntityKind = "SCOPE.Service"
	EntityKindView          EntityKind = "SCOPE.View"
	EntityKindUIComponent   EntityKind = "SCOPE.UIComponent"
	EntityKindJSX           EntityKind = "SCOPE.JSX"
	EntityKindStylesheet    EntityKind = "SCOPE.Stylesheet"
	EntityKindQueue         EntityKind = "SCOPE.Queue"
	EntityKindEvent         EntityKind = "SCOPE.Event"
	EntityKindDatastore     EntityKind = "SCOPE.Datastore"
	EntityKindDataAccess    EntityKind = "SCOPE.DataAccess"
	EntityKindExternalAPI   EntityKind = "SCOPE.ExternalAPI"
	EntityKindInfraResource EntityKind = "SCOPE.InfraResource"
	EntityKindCodeBlock     EntityKind = "SCOPE.CodeBlock"
	EntityKindDocument      EntityKind = "SCOPE.Document"
	EntityKindHeading       EntityKind = "SCOPE.Heading"
	EntityKindScopeUnknown  EntityKind = "SCOPE.ScopeUnknown"
	// Documented-but-previously-undocumented kinds (Issue #77 reconciliation):
	EntityKindExternal EntityKind = "SCOPE.External"
	EntityKindProject  EntityKind = "SCOPE.Project"
	EntityKindConfig   EntityKind = "SCOPE.Config"
	EntityKindModel    EntityKind = "SCOPE.Model"
)

// AllEntityKinds returns every EntityKind that archigraph extractors are
// permitted to emit. Used by tests and tooling to validate that no
// producer leaks a free-form kind string.
func AllEntityKinds() []EntityKind {
	return []EntityKind{
		EntityKindOperation,
		EntityKindComponent,
		EntityKindClass,
		EntityKindFunction,
		EntityKindSchema,
		EntityKindVariable,
		EntityKindReference,
		EntityKindPattern,
		EntityKindEvolution,
		EntityKindEndpoint,
		EntityKindRoute,
		EntityKindService,
		EntityKindView,
		EntityKindUIComponent,
		EntityKindJSX,
		EntityKindStylesheet,
		EntityKindQueue,
		EntityKindEvent,
		EntityKindDatastore,
		EntityKindDataAccess,
		EntityKindExternalAPI,
		EntityKindInfraResource,
		EntityKindCodeBlock,
		EntityKindDocument,
		EntityKindHeading,
		EntityKindScopeUnknown,
		EntityKindExternal,
		EntityKindProject,
		EntityKindConfig,
		EntityKindModel,
	}
}

// IsValidEntityKind reports whether s is one of the typed EntityKind values.
func IsValidEntityKind(s string) bool {
	for _, k := range AllEntityKinds() {
		if string(k) == s {
			return true
		}
	}
	return false
}

// RelationshipKind enumerates the directed-edge kinds emitted by archigraph
// extractors and resolvers. As with EntityKind, producers should reference
// these constants rather than ad-hoc string literals.
type RelationshipKind string

const (
	RelationshipKindCalls         RelationshipKind = "CALLS"
	RelationshipKindImports       RelationshipKind = "IMPORTS"
	RelationshipKindExtends       RelationshipKind = "EXTENDS"
	RelationshipKindImplements    RelationshipKind = "IMPLEMENTS"
	RelationshipKindUses          RelationshipKind = "USES"
	RelationshipKindUsesHook      RelationshipKind = "USES_HOOK"
	RelationshipKindContains      RelationshipKind = "CONTAINS"
	RelationshipKindDependsOn     RelationshipKind = "DEPENDS_ON"
	RelationshipKindReferences    RelationshipKind = "REFERENCES"
	RelationshipKindRoutesTo      RelationshipKind = "ROUTES_TO"
	RelationshipKindServes        RelationshipKind = "SERVES"
	RelationshipKindPublishesTo   RelationshipKind = "PUBLISHES_TO"
	RelationshipKindTests         RelationshipKind = "TESTS"
	RelationshipKindHasProps      RelationshipKind = "HAS_PROPS"
	RelationshipKindAccessesTable RelationshipKind = "ACCESSES_TABLE"
	RelationshipKindInjectedInto  RelationshipKind = "INJECTED_INTO"
	RelationshipKindReadsFrom     RelationshipKind = "READS_FROM"
	RelationshipKindWritesTo      RelationshipKind = "WRITES_TO"
	// Documented-but-previously-undocumented relationship kinds (Issue #77):
	RelationshipKindRenders RelationshipKind = "RENDERS"
	RelationshipKindReturns RelationshipKind = "RETURNS"
	// Issue #86: surfaced by the producer-boundary validator scan. Emitted
	// by the OpenAPI pattern extractor to associate operations with their
	// tag entities.
	RelationshipKindTaggedAs RelationshipKind = "TAGGED_AS"
)

// AllRelationshipKinds returns every RelationshipKind producers may emit.
func AllRelationshipKinds() []RelationshipKind {
	return []RelationshipKind{
		RelationshipKindCalls,
		RelationshipKindImports,
		RelationshipKindExtends,
		RelationshipKindImplements,
		RelationshipKindUses,
		RelationshipKindUsesHook,
		RelationshipKindContains,
		RelationshipKindDependsOn,
		RelationshipKindReferences,
		RelationshipKindRoutesTo,
		RelationshipKindServes,
		RelationshipKindPublishesTo,
		RelationshipKindTests,
		RelationshipKindHasProps,
		RelationshipKindAccessesTable,
		RelationshipKindInjectedInto,
		RelationshipKindReadsFrom,
		RelationshipKindWritesTo,
		RelationshipKindRenders,
		RelationshipKindReturns,
		RelationshipKindTaggedAs,
	}
}

// IsValidRelationshipKind reports whether s is one of the typed RelationshipKind values.
func IsValidRelationshipKind(s string) bool {
	for _, k := range AllRelationshipKinds() {
		if string(k) == s {
			return true
		}
	}
	return false
}
