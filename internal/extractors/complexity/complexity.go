// Package complexity provides shared complexity-signal computation for all
// language extractors. It walks tree-sitter nodes and produces four metrics
// that match the Python indexer's entity complexity signals:
//
//   - CyclomaticComplexity: 1 + count of decision points
//   - MaxCallDepth: deepest nesting of function/method calls
//   - HasConditionals: whether any conditional nodes exist
//   - HasExternalCalls: whether any calls reference external (imported) symbols
//
// Language-specific decision and call node types are configured via Config.
package complexity

import (
	sitter "github.com/smacker/go-tree-sitter"
)

// Signals holds the computed complexity metrics for a single entity.
type Signals struct {
	CyclomaticComplexity int  `json:"cyclomatic_complexity"`
	MaxCallDepth         int  `json:"max_call_depth"`
	HasConditionals      bool `json:"has_conditionals"`
	HasExternalCalls     bool `json:"has_external_calls"`
}

// Config provides language-specific node type sets for complexity analysis.
// Each language extractor constructs one of these and passes it to Compute.
type Config struct {
	// DecisionTypes are node types that contribute to cyclomatic complexity
	// (e.g. "if_statement", "for_statement", "switch_statement").
	DecisionTypes map[string]bool

	// LogicalOperators are binary expression operator texts that count as
	// additional decision points (e.g. "&&", "||", "and", "or").
	LogicalOperators map[string]bool

	// BinaryExpressionType is the node type for binary expressions that may
	// contain logical operators (e.g. "binary_expression").
	BinaryExpressionType string

	// ConditionalTypes are node types that indicate the presence of
	// conditionals (e.g. "if_statement", "switch_statement").
	ConditionalTypes map[string]bool

	// CallTypes are node types that represent function/method invocations
	// (e.g. "method_invocation", "call_expression").
	CallTypes map[string]bool

	// ExternalCallDetector is an optional function that checks if a call node
	// is an external call. If nil, HasExternalCalls is always false.
	ExternalCallDetector func(callNode *sitter.Node, src []byte) bool
}

// Compute walks the subtree rooted at node and returns complexity signals.
func Compute(node *sitter.Node, src []byte, cfg Config) Signals {
	if node == nil {
		return Signals{CyclomaticComplexity: 1}
	}

	s := Signals{CyclomaticComplexity: 1}
	s.CyclomaticComplexity += countDecisions(node, src, cfg)
	s.MaxCallDepth = maxCallDepth(node, 0, cfg)
	s.HasConditionals = hasAny(node, cfg.ConditionalTypes)

	if cfg.ExternalCallDetector != nil {
		s.HasExternalCalls = hasExternalCalls(node, src, cfg)
	}

	return s
}

// countDecisions counts decision points in a subtree.
func countDecisions(node *sitter.Node, src []byte, cfg Config) int {
	count := 0
	stack := []*sitter.Node{node}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		t := n.Type()
		if cfg.DecisionTypes[t] {
			if t == cfg.BinaryExpressionType && len(cfg.LogicalOperators) > 0 {
				// Only count if it contains a logical operator child
				childCount := int(n.ChildCount())
				for i := 0; i < childCount; i++ {
					child := n.Child(i)
					childText := nodeText(child, src)
					if cfg.LogicalOperators[child.Type()] || cfg.LogicalOperators[childText] {
						count++
						break
					}
				}
			} else {
				count++
			}
		}

		childCount := int(n.ChildCount())
		for i := 0; i < childCount; i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return count
}

// maxCallDepth computes the maximum nesting depth of call expressions.
func maxCallDepth(node *sitter.Node, depth int, cfg Config) int {
	if cfg.CallTypes[node.Type()] {
		depth++
	}
	currentMax := depth
	childCount := int(node.ChildCount())
	for i := 0; i < childCount; i++ {
		childMax := maxCallDepth(node.Child(i), depth, cfg)
		if childMax > currentMax {
			currentMax = childMax
		}
	}
	return currentMax
}

// hasAny checks if any descendant has a type in the given set.
func hasAny(node *sitter.Node, types map[string]bool) bool {
	if len(types) == 0 {
		return false
	}
	stack := []*sitter.Node{node}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if types[n.Type()] {
			return true
		}
		childCount := int(n.ChildCount())
		for i := 0; i < childCount; i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return false
}

// hasExternalCalls checks if any call node in the subtree is external.
func hasExternalCalls(node *sitter.Node, srcBytes []byte, cfg Config) bool {
	stack := []*sitter.Node{node}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cfg.CallTypes[n.Type()] && cfg.ExternalCallDetector(n, srcBytes) {
			return true
		}
		childCount := int(n.ChildCount())
		for i := 0; i < childCount; i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return false
}

// nodeText returns the text of a tree-sitter node.
func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	start := node.StartByte()
	end := node.EndByte()
	if int(end) > len(src) {
		end = uint32(len(src))
	}
	return string(src[start:end])
}
