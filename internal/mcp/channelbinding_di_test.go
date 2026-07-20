package mcp

// channelbinding_di_test.go — #5782 (ADR-0025) semantic-bleed guard: a
// ChannelBinding's channel edge (BINDS_CHANNEL) must never be projected as a
// dependency-injection edge in grafel_inspect / neighbors output.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

func TestIsDIEdgeKind_ExcludesBindsChannel(t *testing.T) {
	// The config→channel edge kind must NOT be treated as a DI edge.
	if isDIEdgeKind(string(types.RelationshipKindBindsChannel)) {
		t.Error("BINDS_CHANNEL must NOT be classified as a dependency-injection edge")
	}
	// Contrast: the reused Helm/DI "BINDS" kind IS a DI edge — the reason we
	// use a distinct BINDS_CHANNEL kind for messaging bindings.
	if !isDIEdgeKind(string(types.RelationshipKindBinds)) {
		t.Error("BINDS should still be classified as a DI edge (contrast case)")
	}
	// BINDS_TOPIC is likewise not a DI edge.
	if isDIEdgeKind(string(types.RelationshipKindBindsTopic)) {
		t.Error("BINDS_TOPIC must NOT be classified as a dependency-injection edge")
	}
}

// TestBindingMatchesEdge_RealGraphStorage guards the #5782 live-corpus finding:
// a BINDS_* edge whose FromID is the DANGLING synthetic "scope:channelbinding:.."
// id must still be matched to a ChannelBinding entity that is stored under a
// content-HASH id. Exercises each field-derivation fallback in
// bindingDirChannelSuffix so the matcher survives whichever fields fbwrite keeps.
func TestBindingMatchesEdge_RealGraphStorage(t *testing.T) {
	// The exact shape observed on the event-driven-ai graph.
	const synthetic = "scope:channelbinding:spring_properties:src/main/resources/application.properties:outgoing:feedback-out"

	// Case 1: rich Properties present (channel + direction).
	viaProps :=

		graph.EntityPtr(graph.Entity{
			ID: "hash-abc", Name: "feedback-out", Kind: string(types.EntityKindChannelBinding),
			SourceFile:    "src/main/resources/application.properties",
			QualifiedName: "feedback-ingest-service::src/main/resources/application.properties#outgoing:feedback-out",
		}.WithProperties(map[string]string{"channel": "feedback-out", "direction": "outgoing"}))
	if !bindingMatchesEdge(viaProps, synthetic) {
		t.Error("must match via Properties channel+direction on a hash-keyed entity")
	}

	// Case 2: Properties trimmed — direction must come from Subtype, channel from Name.
	viaSubtype := &graph.Entity{
		ID: "hash-abc", Name: "feedback-out", Kind: string(types.EntityKindChannelBinding),
		SourceFile: "src/main/resources/application.properties", Subtype: "outgoing",
		QualifiedName: "feedback-ingest-service::src/main/resources/application.properties#outgoing:feedback-out",
	}
	if !bindingMatchesEdge(viaSubtype, synthetic) {
		t.Error("must match via Subtype(direction)+Name(channel) when Properties are absent")
	}

	// Case 3: only QualifiedName survives (no Properties, no Subtype).
	viaQName := &graph.Entity{
		ID: "hash-abc", Name: "feedback-out", Kind: string(types.EntityKindChannelBinding),
		SourceFile:    "src/main/resources/application.properties",
		QualifiedName: "feedback-ingest-service::src/main/resources/application.properties#outgoing:feedback-out",
	}
	// Blank the Name-derived channel path to force the '#' fallback specifically.
	viaQName.Name = ""
	if !bindingMatchesEdge(viaQName, synthetic) {
		t.Error("must match via the QualifiedName '#<direction>:<channel>' tail as a last resort")
	}

	// Negative: a DIFFERENT binding (wrong channel) must NOT match.
	other := &graph.Entity{
		ID: "hash-xyz", Name: "audit-out", Kind: string(types.EntityKindChannelBinding),
		SourceFile: "src/main/resources/application.properties", Subtype: "outgoing",
	}
	if bindingMatchesEdge(other, synthetic) {
		t.Error("a binding for a different channel must not match the feedback-out edge")
	}

	// Negative: same (direction, channel) but a DIFFERENT source file must not
	// match (cross-file disambiguation).
	otherFile := &graph.Entity{
		ID: "hash-def", Name: "feedback-out", Kind: string(types.EntityKindChannelBinding),
		SourceFile: "src/main/resources/other.properties", Subtype: "outgoing",
	}
	if bindingMatchesEdge(otherFile, synthetic) {
		t.Error("a same-channel binding in a different config file must not match")
	}
}
