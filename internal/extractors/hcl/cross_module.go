package hcl

import (
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ----------------------------------------------------------------
// Issue #4625 — cross-module output references → semantic edges
// ----------------------------------------------------------------
//
// The headline gap: a resource inside one module consumes another module's
// output, e.g. inside the worker stack
//
//	resource "aws_ecs_service" "worker" {
//	  environment { queue_url = module.dispatch_queue.queue_url }
//	}
//
// The generic CALLS miner (extractCalls / canonicalRefFromExpression) collapses
// `module.dispatch_queue.queue_url` to the bare ref `module.dispatch_queue`,
// dropping the `.queue_url` OUTPUT name and carrying no semantics. Worse, the
// resulting CALLS target is the module BLOCK, which the dashboard does not
// render as an IaC resource node — so the edge surfaces only as one of the
// "N relation targets could not be resolved" footer entries and the consuming
// resource + the producing module render as DISCONNECTED boxes.
//
// extractCrossModuleRefs fixes this for resource / data blocks: for every
// `module.<name>.<output>` reference in the body it emits a USES edge
//
//	<this resource> --USES--> module.<name>
//
// carrying:
//
//	dataflow       = "cross_module"
//	module_output  = "<output>"        (the referenced output, e.g. queue_url)
//	input_arg      = "<attr>"          (the consuming attribute key, when known)
//	semantic       = "<label>"         (consumes / redrive / logs-to / assumes /
//	                                     grants / reads / dependency — derived)
//
// The module block IS a same-file entity (module.<name>), so the edge binds via
// byLocation exactly like the existing CALLS/USES edges and is no longer an
// unresolved relation. The dashboard (#4625) promotes module nodes to rendered
// diagram resources so the edge draws between two visible boxes, labelled with
// its semantic.
//
// Generalization note: AWS CDK / Pulumi / CloudFormation / Bicep express the
// same cross-stack export→import pattern (CfnOutput/Fn::ImportValue, StackRef
// outputs, module outputs). The semantic-label derivation here
// (crossModuleSemantic) is intentionally framework-agnostic so those tools can
// reuse it once their extractors emit a comparable cross-stack ref; tracked as
// a follow-up.

// extractCrossModuleRefs walks a resource/data block body and returns USES
// edges for every `module.<name>.<output>` reference, tagged with the output
// name, the consuming attribute, and a derived semantic label. selfRef is the
// canonical ref of the consuming block (used as the edge FromID and to derive
// semantics from the consuming resource type).
func extractCrossModuleRefs(body *sitter.Node, src []byte, path, lang, selfRef string) []types.RelationshipRecord {
	if body == nil {
		return nil
	}
	consumerType := resourceTypeOfRef(selfRef)
	var rels []types.RelationshipRecord
	seen := map[string]struct{}{}

	// walk descends an attribute value tree carrying the enclosing attribute
	// key (argName) so the semantic derivation can use the consuming attribute.
	var walk func(n *sitter.Node, argName string)
	walk = func(n *sitter.Node, argName string) {
		if n == nil {
			return
		}
		if n.Type() == "expression" {
			if name, output := moduleOutputRef(n, src); name != "" && output != "" {
				moduleRef := "module." + name
				if moduleRef != selfRef {
					dedup := argName + "|" + moduleRef + "|" + output
					if _, ok := seen[dedup]; !ok {
						seen[dedup] = struct{}{}
						sem := crossModuleSemantic(consumerType, argName, output)
						rels = append(rels, types.RelationshipRecord{
							FromID: extractor.BuildOperationStructuralRef(lang, path, selfRef),
							ToID:   extractor.BuildOperationStructuralRef(lang, path, moduleRef),
							Kind:   "USES",
							Properties: map[string]string{
								"dataflow":      "cross_module",
								"module_output": output,
								"input_arg":     argName,
								"semantic":      sem,
								"line":          strconv.Itoa(int(n.StartPoint().Row) + 1),
							},
						})
					}
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), argName)
		}
	}

	// Walk top-level attributes and nested blocks, tracking the attribute key
	// that encloses each reference so semantics can use it.
	var descend func(n *sitter.Node)
	descend = func(n *sitter.Node) {
		if n == nil {
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child == nil {
				continue
			}
			switch child.Type() {
			case "attribute":
				key := ""
				if id := firstChildByType(child, "identifier"); id != nil {
					key = nodeText(id, src)
				}
				if key == "depends_on" {
					continue
				}
				walk(child, key)
			case "block":
				// Nested config block (e.g. environment {}, redrive_policy block).
				// Recurse so refs inside nested blocks are captured; the nested
				// block label is not a meaningful arg, so attribute keys inside
				// it are re-derived by the recursive descend.
				if bb := blockBody(child); bb != nil {
					descend(bb)
				}
			}
		}
	}
	descend(body)
	return rels
}

// moduleOutputRef returns (moduleName, outputName) when the expression is a
// `module.<name>.<output>` reference (optionally with deeper attribute access,
// in which case outputName is the first attribute after the module name). It
// returns ("","") for non-module refs or bare `module.<name>` (no output).
func moduleOutputRef(expr *sitter.Node, src []byte) (string, string) {
	parts := referenceParts(expr, src)
	if len(parts) >= 3 && parts[0] == "module" {
		return parts[1], parts[2]
	}
	return "", ""
}

// resourceTypeOfRef returns the resource type segment of a canonical resource
// ref. For "aws_ecs_service.worker" → "aws_ecs_service"; for "data.x.y" → "x".
// Returns "" for module/var/local refs.
func resourceTypeOfRef(ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "data.") {
		rest := ref[len("data."):]
		if i := strings.IndexByte(rest, '.'); i > 0 {
			return rest[:i]
		}
		return rest
	}
	switch {
	case strings.HasPrefix(ref, "module."),
		strings.HasPrefix(ref, "var."),
		strings.HasPrefix(ref, "local."):
		return ""
	}
	if i := strings.IndexByte(ref, '.'); i > 0 {
		return ref[:i]
	}
	return ref
}

// crossModuleSemantic derives a human/UI-facing semantic label for a
// cross-module (or cross-resource) data-flow edge from the consuming resource
// type, the consuming attribute key, and the referenced output name. The labels
// are the cloud-architecture verbs the IaC diagram renders (#4625):
//
//	consumes  — a compute/function resource reads a queue/stream/topic endpoint
//	            (queue_url / queue_arn / stream / topic), i.e. it is a consumer.
//	redrive   — a queue points at a dead-letter queue (redrive policy / DLQ).
//	logs-to   — a resource writes to a CloudWatch log group / log destination.
//	assumes   — a task/exec/instance assumes an IAM role (role arn).
//	grants    — an IAM policy/attachment empowers a role/principal.
//	reads     — a generic read of another resource's id/arn/name (default for
//	            data refs and unclassified id/arn outputs).
//	dependency— fallback when nothing more specific is derivable.
//
// Derivation is by signal priority: attribute key first (most specific to the
// consumer's intent), then output name, then consumer resource type.
func crossModuleSemantic(consumerType, argName, output string) string {
	a := strings.ToLower(argName)
	o := strings.ToLower(output)
	ct := strings.ToLower(consumerType)

	// Dead-letter / redrive wiring.
	if strings.Contains(a, "redrive") || strings.Contains(a, "dead_letter") ||
		strings.Contains(a, "dlq") || strings.Contains(o, "dead_letter") ||
		strings.Contains(o, "dlq") {
		return "redrive"
	}

	// Observability: CloudWatch log groups / log destinations.
	if strings.Contains(a, "log_group") || strings.Contains(a, "log_destination") ||
		strings.Contains(a, "cloudwatch") || strings.Contains(o, "log_group") ||
		strings.Contains(o, "cloudwatch_log") {
		return "logs-to"
	}

	// IAM: a policy/attachment empowering a role.
	if strings.Contains(ct, "iam_role_policy") || strings.Contains(ct, "iam_policy") ||
		strings.Contains(ct, "policy_attachment") {
		if strings.Contains(o, "role") || strings.Contains(a, "role") {
			return "grants"
		}
	}
	// IAM: a task/exec/instance assuming a role.
	if strings.Contains(a, "role_arn") || strings.Contains(a, "execution_role") ||
		strings.Contains(a, "task_role") || strings.Contains(a, "instance_role") ||
		(strings.Contains(a, "role") && strings.Contains(o, "role")) {
		return "assumes"
	}

	// Messaging consumption: a compute/function resource reading a queue /
	// stream / topic endpoint is a CONSUMER of that channel.
	if strings.Contains(o, "queue_url") || strings.Contains(o, "queue_arn") ||
		strings.Contains(o, "queue_name") || strings.Contains(o, "topic_arn") ||
		strings.Contains(o, "stream_arn") || strings.Contains(o, "stream_name") ||
		strings.Contains(a, "queue_url") || strings.Contains(a, "queue_arn") ||
		strings.Contains(a, "topic_arn") {
		return "consumes"
	}

	// Generic id/arn/name read of another resource's output.
	if strings.HasSuffix(o, "_arn") || o == "arn" ||
		strings.HasSuffix(o, "_id") || o == "id" ||
		strings.HasSuffix(o, "_name") || o == "name" ||
		strings.HasSuffix(o, "endpoint") || strings.HasSuffix(o, "_url") {
		return "reads"
	}

	return "dependency"
}
