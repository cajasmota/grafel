package engine

import (
	"slices"
	"strings"
	"testing"
)

// #6916 Tier C, 10th and LAST site — python/frameworks/langchain.yaml:43,
// the `@tool` decorator, deleted.
//
//	pattern: "@tool\b"   entity_type: Operation   name_group: 0   scope: function
//
// Name group 0 is the whole match, and the match is the decorator text itself,
// so EVERY decorated function in a file produced the same Name "@tool" — one
// per-file boolean marker, not one entity per tool. `@tool("named")` matched
// only "@tool" as well (the pattern ends at `\b`), so the Name was invariant.
//
// WHY THIS SITE NEEDS ITS OWN TEST INSTEAD OF A ROW IN tierC6916Sites:
//
// It is the ONLY one of the ten where "already dropped downstream" is TRUE, and
// that makes the ordinary Tier C assertion VACUOUS here. dropStatementNoiseOperations
// (precision_dedup.go, always on — PrecisionDedupEnabled has no production
// toggle) drops an Operation whose Name is "@" + a bare identifier, which
// "@tool" is exactly. So `Operation:@tool` was absent from Detect's output
// BEFORE this deletion too: a post-dedup absence assertion passes at the parent
// commit and grades nothing.
//
// The only input that distinguishes the deletion is a detector run with the
// precision pass OFF, which is what TestIssue6916_TierCPythonToolMarkerIsNoLongerMintedAtTheRuleLayer
// does. Its companion below then pins the OTHER half of the claim — that with
// the pass ON nothing observable changed, i.e. this was a zero-recall-change
// cleanup rather than a recall reduction.
//
// VARIED across the two decorator spellings: bare `@tool` and `@tool("named")`
// (the second is why "capture the argument instead" was not an option — the
// bare form has no argument, and the pattern never reached the parenthesis).
// HELD CONSTANT: the file's two surviving source_patterns (the LCEL pipe-chain
// Operation at name_group 1 and the `create_\w+_agent(` Service at name_group 0,
// which is Tier D, not Tier C), the `frameworks:` detection block, and
// isStatementNoiseOperationName's predicate — the filter is deliberately NOT
// narrowed to match the rule deletion, because it guards a shape, not a rule.

// tierC6916PyTool is one python source file that fired the deleted rule. Both
// decorated functions are indented inside no class and are not the first line
// of the file, and the file also exercises both surviving patterns so every
// absence assertion below has a positive control from the SAME input.
const tierC6916PyTool = `from langchain_core.tools import tool
from langchain.agents import create_react_agent
from langchain_core.prompts import ChatPromptTemplate


@tool
def search(query: str) -> str:
    """Search the web."""
    return "results"


@tool("weather-lookup")
def weather(city: str) -> str:
    """Look up the weather."""
    return "sunny"


prompt = ChatPromptTemplate.from_messages([])
chain = prompt | model | parser
agent = create_react_agent(llm, tools=[search, weather])
`

const (
	// tierC6916PyToolMarker is the "<Kind>:<Name>" the deleted rule produced.
	// Measured at the parent commit with the precision pass off; it must never
	// come back.
	tierC6916PyToolMarker = "Operation:@tool"

	// The two positive controls, from the file's two SURVIVING patterns. Without
	// them the absence assertion would also pass on a rule file that failed to
	// load — the loader silently SKIPS an unparseable rule file, so "no entities
	// at all" is a real and silent failure mode here.
	tierC6916PyChainControl = "Operation:chain"
	tierC6916PyAgentControl = "Service:create_react_agent("
)

// TestIssue6916_TierCPythonToolMarkerIsNoLongerMintedAtTheRuleLayer is the
// change itself, graded on the only input that can see it: the precision pass
// disabled, so what is asserted is the RULE's output rather than the filtered
// graph's.
func TestIssue6916_TierCPythonToolMarkerIsNoLongerMintedAtTheRuleLayer(t *testing.T) {
	old := PrecisionDedupEnabled
	PrecisionDedupEnabled = false
	t.Cleanup(func() { PrecisionDedupEnabled = old })

	ids := entityIDs6916(detect6916(t, "app/agent.py", "python", tierC6916PyTool))

	if slices.Contains(ids, tierC6916PyToolMarker) {
		t.Errorf("python/frameworks/langchain.yaml: the deleted #6916 Tier C `@tool` rule is "+
			"back — %q is minted again. It is the decorator text itself, identical for every "+
			"decorated function in the file, so it is one per-file boolean marker with no "+
			"referent for any edge. Entities:\n  %s",
			tierC6916PyToolMarker, strings.Join(ids, "\n  "))
	}
	for _, control := range []string{tierC6916PyChainControl, tierC6916PyAgentControl} {
		if !slices.Contains(ids, control) {
			t.Errorf("positive control missing — this input no longer mints %q, so the absence "+
				"of %q above proves nothing (an unloadable langchain.yaml would pass it). "+
				"Entities:\n  %s", control, tierC6916PyToolMarker, strings.Join(ids, "\n  "))
		}
	}
}

// TestIssue6916_TierCPythonToolDeletionChangedNoGraph pins the second half of
// the justification: with the precision pass ON — its only production state —
// the marker was ALREADY absent, so this deletion removed a rule whose output
// never reached a graph. If this test ever fails, the deletion above stopped
// being a no-op and became a recall reduction that needs re-arguing.
func TestIssue6916_TierCPythonToolDeletionChangedNoGraph(t *testing.T) {
	if !PrecisionDedupEnabled {
		t.Fatal("precondition: PrecisionDedupEnabled must be true here — that is the " +
			"production state this test is about")
	}
	ids := entityIDs6916(detect6916(t, "app/agent.py", "python", tierC6916PyTool))

	if slices.Contains(ids, tierC6916PyToolMarker) {
		t.Errorf("%q reached the post-dedup graph, which contradicts the premise this "+
			"deletion rested on (that dropStatementNoiseOperations always removed it). "+
			"Entities:\n  %s", tierC6916PyToolMarker, strings.Join(ids, "\n  "))
	}
	// Same controls: the surviving patterns must still reach the real graph, not
	// merely the rule layer.
	for _, control := range []string{tierC6916PyChainControl, tierC6916PyAgentControl} {
		if !slices.Contains(ids, control) {
			t.Errorf("a python langchain pattern NOT part of Tier C stopped reaching the "+
				"graph: %q is missing. Entities:\n  %s", control, strings.Join(ids, "\n  "))
		}
	}
}

// TestIssue6916_TierCPythonToolPredicateStillGuardsTheShape pins that the
// filter was NOT narrowed alongside the rule deletion. The rule is gone; the
// shape it produced is still noise, and any other rule that produces it must
// still be dropped. Deleting the rule and relaxing the filter in the same change
// would leave the shape ungated with nothing failing.
func TestIssue6916_TierCPythonToolPredicateStillGuardsTheShape(t *testing.T) {
	for _, name := range []string{"@tool", "@property", "@app.route"} {
		if !isStatementNoiseOperationName(name) {
			t.Errorf("isStatementNoiseOperationName(%q) = false — the bare-decorator case (1) "+
				"was relaxed. #6916 Tier C deleted the RULE that produced this shape, not the "+
				"guard against it.", name)
		}
	}
	// The negative direction, so the assertion above is not satisfied by a
	// predicate that returns true for everything: a real call idiom is kept.
	for _, name := range []string{"createTRPCClient<", "DynamicTool", "prompt"} {
		if isStatementNoiseOperationName(name) {
			t.Errorf("isStatementNoiseOperationName(%q) = true — the predicate widened and is "+
				"now eating real operations.", name)
		}
	}
}
