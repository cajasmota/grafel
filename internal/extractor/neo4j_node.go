package extractor

import "strings"

// neo4j_node.go — the shared addressing convention for Cypher node-label
// entities (issue #6122, refs #6105, #6123).
//
// WHO USES THIS. The five Cypher-STRING neo4j extractors —
// internal/custom/{csharp,elixir,golang,php,rust}/neo4j.go. They recover node
// labels from query text by regex ((n:Person), (:Movie)) and mint one synthetic
// SCOPE.Schema entity per label per file, then promote a fully-static Cypher
// triple to a traversable GRAPH_RELATES edge between two of them.
//
// WHY THE ENTITY NAME IS PREFIXED. A Cypher label is a DATABASE label pulled out
// of a string literal, not a declaration. A repo that stores `Movie` nodes very
// often also declares a `Movie` struct/class. The `node:` prefix keeps the two
// apart in the entity namespace, and that decision is correct and is kept.
//
// WHY THE REF IS NOT THE NAME. It was, and that was the bug. LookupStatusHint
// reaches byName through splitStub (internal/resolve/refs.go:2658), which cuts
// at the FIRST colon and probes with the REMAINDER — so `ToID: "node:"+label`
// could only ever probe `byName["<label>"]`, a key the intended target does not
// carry and the colliding code symbol does. Measured end-to-end: the edge bound
// to the Go struct (cmd/grafel.TestNeo4jGraphRelatesBindsTheNodeEntityNotThe
// Collider6122); the resolver fact is pinned in
// internal/resolve.TestNodeLabelRefCannotAddressANodeEntityAndMisbinds6122.
//
// WHY NOT THE `"Class:"+label` FORM THE SIBLINGS USE. java/neo4j.go:328,
// python/neo4j_neomodel.go:247 and ruby/neo4j_activegraph.go:299 emit exactly
// this relationship as `"Class:"+targetType` and DO resolve. That works because
// their target is a real annotated CLASS whose entity Name is the BARE class
// name, so splitStub's leaf probe lands on it. Pointed at a Cypher label the
// same ref lands on the code symbol instead — the identical defect wearing a
// different prefix, measured in
// internal/resolve.TestClassRefWouldMisbindForACypherLabel6122. Convergence on
// the ref STRING would be convergence on a bug: the two families address
// structurally different targets. Renaming the synthetic entity to the bare
// label to make `Class:` work is worse — it moves the collision from the edge
// onto the entity, discarding the reason the prefix exists.
//
// WHY NOT A MINTED QualifiedName (the #6123 remedy). It would work, but it
// addresses the label GLOBALLY, and byQualifiedName blanks its entry on any
// collision (internal/resolve/refs.go:948-965). The node entities here are
// per-file with a real SourceFile and line, so the same label appearing in two
// files would blank the key and dangle BOTH edges. The #6123 service node dodges
// that only by being file-agnostic (synthetic SourceFile ⇒ one shared ID), which
// these are not. A file-scoped ref is also the more honest one: both endpoints
// of a Cypher triple are minted by the same extractor run on the same file, so
// the target is always in-file and never needs cross-file reach.
//
// WHAT IT USES INSTEAD. The PLT #537 short-form `scope:schema:<file>#<name>`
// (internal/resolve/refs.go:1929), which probes byLocation[file][Name] with the
// FULL entity Name — colon included. No new resolver code, no entity mutation,
// and no cross-file ambiguity.

// neo4jNodeNamePrefix namespaces a Cypher node label in the entity namespace so
// it cannot be confused with a code symbol of the same name.
const neo4jNodeNamePrefix = "node:"

// Neo4jNodeName returns the canonical entity Name for a Cypher node label:
//
//	node:<label>
//
// Callers MUST mint the SCOPE.Schema node entity with this Name, because
// Neo4jNodeTargetID addresses the entity by it.
func Neo4jNodeName(label string) string {
	return neo4jNodeNamePrefix + label
}

// Neo4jNodeTargetID returns the GRAPH_RELATES ToID addressing the node-label
// entity for `label` in `filePath`. Shape:
//
//	scope:schema:<file>#node:<label>
//
// Returns "" when no safe ref can be built, and a caller that gets "" MUST emit
// no edge. Dropping the relationship is deliberate and is NOT the usual
// "an honest dangle beats nothing" case, because the alternative here is not a
// dangle: it is a ref that PARSES, and therefore mis-binds.
//
// THE BOUND, and why it is load-bearing rather than defensive decoration.
// The short-form at refs.go:1932 declines a file path containing ':'; on that
// decline — and on a byLocation miss — execution falls through to the Format A
// parse at refs.go:2037, which fires at EXACTLY stubScopeSegments = 6 with
// parts[4] read as a file path and parts[5] as an entity name. Counting the two
// fixed prefix colons plus the one in `#node:`, a file path with two colons, or
// a label with two colons, reaches six. The resolver really does mis-bind there
// — asserted, not assumed, in the "six segments" subtest of
// internal/resolve.TestNeo4jNodeLocationRefBindsTheNodeEntity6122. This is the
// same trap the #6123 fix walked into in its own output.
//
// Refusing ANY colon rather than counting to two is deliberate: the ceiling then
// does not have to be recomputed if the prefix ever gains a segment, and no
// legitimate input is lost. Every caller's label comes from a regex bounded to
// [A-Za-z_]\w* and is colon-free by construction, so the label arm is
// unreachable from live extraction and exists to keep it that way; the file-path
// arm is reachable in principle (':' is legal in a POSIX filename).
//
// ':' IS THE ONLY REFUSED CHARACTER, and '#' specifically is NOT — which the
// "recompute the ceiling" reason above does not explain on its own. '#' is the
// short-form's member delimiter (stubMemberDelim), so a path containing one
// truncates filePath at the FIRST '#' and mis-reads the remainder as the member.
// That is a miss, not a mis-bind: it adds no colons, so the stub cannot reach six
// segments, misses byLocation, and is left verbatim to dangle honestly. Only ':'
// can turn a miss into a Format A parse, so only ':' is refused. If the ceiling
// or the delimiter ever changes, this is the sentence to revisit.
func Neo4jNodeTargetID(filePath, label string) string {
	if filePath == "" || label == "" {
		return ""
	}
	if strings.ContainsRune(filePath, ':') || strings.ContainsRune(label, ':') {
		return ""
	}
	return "scope:schema:" + filePath + "#" + Neo4jNodeName(label)
}
