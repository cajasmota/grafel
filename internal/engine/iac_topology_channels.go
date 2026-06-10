// IaC-declared messaging infrastructure → messaging Topology channels — #4496.
//
// Background
//
// The messaging Topology view (handlers_topology.go) only renders CODE-level
// channels — entities a publish/subscribe library produced (Kafka, RabbitMQ,
// SQS SDK calls, …). Infrastructure-as-Code declarations of the SAME messaging
// primitives — an `aws_sqs_queue`, an `aws_sns_topic`, a GCP `pubsub.Topic`, an
// Azure `servicebus/queues`, an MSK/Kafka cluster — are extracted by the IaC
// extractors (Terraform/HCL, CDK, Pulumi, CloudFormation, Bicep) as generic
// resource entities (SCOPE.Component / SCOPE.InfraResource / SCOPE.Queue) that
// classifyTopologyBucket does NOT surface. The result: a repo whose only queues
// are declared in Terraform shows "No async channels indexed" in /topology even
// though the IaC view lists the queues. (#4496, ref epic #4493.)
//
// What this pass does
//
// For every already-extracted IaC resource entity whose cross-tool
// `resource_category` is a messaging primitive (queue / topic / stream), it
// APPENDS a synthetic Topology channel entity:
//
//   - category queue  → SCOPE.Queue        (broker derived from the resource type)
//   - category topic  → SCOPE.MessageTopic (broker derived from the resource type)
//   - category stream → SCOPE.Queue        (broker derived; Kinesis/MSK/EventHub)
//
// The synthetic entity is keyed by the canonical broker + queue/topic NAME, so
// it COLLAPSES onto any code-level channel of the same name. For SQS/SNS this
// reuses the exact `sqs:<name>` / `sns:<name>` IDs the SDK passes mint, so a
// queue declared in Terraform and published-to by boto3 renders as ONE node
// with the code publisher attached (the bonus code-join, for free). For brokers
// without a code-side ID convention the synthetic entity simply appears as a
// declared-but-unwired channel — which is exactly the desired "show the queue
// even with no detected producer/consumer" behaviour.
//
// Generality
//
// The pass is tool-agnostic and cloud-agnostic: it reads the ONE shared
// `resource_category` join key (types.IaCResourceCategory) and the resource
// TYPE string, both of which every IaC extractor stamps. It therefore covers
// AWS (SQS/SNS/Kinesis/MSK/EventBridge), GCP (Pub/Sub), and Azure (Service Bus
// queues+topics, Event Hubs, Event Grid) uniformly, across Terraform, CDK,
// Pulumi, CloudFormation, and Bicep — without a per-tool branch.
//
// Scope guard
//
// Append-only — never modifies or removes existing entities/edges, and dedupes
// against itself AND against the IDs the code-side / iac_sns passes already
// emit, so it can neither regress the pipeline's bug-rate nor double-count a
// queue that another pass already surfaced.
//
// Refs #4496.
package engine

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// applyIaCTopologyChannels is the entry point. It runs near the end of the
// detector pass chain (after the code-side and iac_sns passes) so it can dedupe
// against the channel IDs those passes already produced. Append-only.
func applyIaCTopologyChannels(args DetectorPassArgs) DetectorPassResult {
	entities := args.Entities
	relationships := args.Relationships
	if len(entities) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	// Index the IDs of channel entities that already exist (code-side passes,
	// iac_sns, or a prior file) so we never emit a duplicate node. The topology
	// view dedupes topics by (broker, name) but queues by entity ID, so we must
	// not re-emit a queue another pass already minted.
	existing := map[string]bool{}
	for i := range entities {
		existing[entities[i].Kind+":"+entities[i].Name] = true
	}

	for i := range entities {
		e := &entities[i]
		if !isIaCResourceEntity(e) {
			continue
		}
		resourceType := iacResourceTypeOf(e)
		category := iacChannelCategory(e, resourceType)
		broker := iacBrokerForResourceType(resourceType)
		if broker == "" {
			// Recognised as messaging by category but the type string didn't map
			// to a known broker — skip rather than emit an "unknown" broker node.
			continue
		}

		name := iacChannelName(e, resourceType)
		if name == "" {
			continue
		}

		var kind, id string
		switch category {
		case types.IaCCategoryTopic:
			kind = messageTopicKind
			id = iacChannelTopicID(broker, name)
		case types.IaCCategoryQueue, types.IaCCategoryStream:
			kind = queueEntityKind
			id = iacChannelQueueID(broker, name)
		default:
			continue
		}

		if existing[kind+":"+id] {
			continue
		}
		existing[kind+":"+id] = true

		props := map[string]string{
			"broker":        broker,
			"pattern_type":  "iac_declared",
			"iac_declared":  "true",
			"resource_type": resourceType,
		}
		if iacTool := iacToolOf(e); iacTool != "" {
			props["iac_tool"] = iacTool
		}
		if kind == messageTopicKind {
			props["topic_name"] = name
		} else {
			props["queue_name"] = name
		}

		entities = append(entities, types.EntityRecord{
			Name:             id,
			Kind:             kind,
			Language:         e.Language,
			SourceFile:       e.SourceFile,
			StartLine:        e.StartLine,
			Properties:       props,
			EnrichmentStatus: types.StatusPending,
			QualityScore:     0.8,
		})
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// isIaCResourceEntity reports whether an entity is an IaC resource declaration
// (from any of the supported tools) rather than application code.
func isIaCResourceEntity(e *types.EntityRecord) bool {
	switch e.Kind {
	// CDK / Pulumi / Bicep.
	case "SCOPE.InfraResource":
		return true
	// CloudFormation semantic kinds (derived from category) + Terraform/HCL
	// resource blocks share SCOPE.Component / SCOPE.Queue / SCOPE.Datastore /
	// SCOPE.ServerlessFunction. We only treat them as IaC when they carry an
	// IaC fingerprint (resource_type / construct_type / iac_tool / subtype).
	case "SCOPE.Component", "SCOPE.Queue", "SCOPE.Datastore", "SCOPE.ServerlessFunction":
		if e.Subtype == "resource" {
			return true
		}
		if iacResourceTypeOf(e) != "" {
			return true
		}
		if iacToolOf(e) != "" {
			return true
		}
		return false
	default:
		return false
	}
}

// iacChannelCategory returns the cross-tool resource_category for an IaC
// resource, preferring an explicit property/metadata stamp and falling back to
// the shared classifier on the resource type (covers Terraform, whose category
// lives in Metadata that does not survive to the read layer but IS present at
// engine time).
func iacChannelCategory(e *types.EntityRecord, resourceType string) string {
	if c := strings.TrimSpace(e.Properties["resource_category"]); c != "" {
		return c
	}
	if c := strings.TrimSpace(e.Properties["resource_scope"]); c != "" {
		return c
	}
	if e.Metadata != nil {
		if v, ok := e.Metadata["resource_category"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if resourceType != "" {
		return types.IaCResourceCategory(resourceType)
	}
	return ""
}

// iacResourceTypeOf extracts the raw IaC resource/construct TYPE string from an
// entity, regardless of which tool produced it:
//
//   - Terraform/HCL: stored in Metadata["resource_type"], and recoverable from
//     the entity Name (`aws_sqs_queue.main` → `aws_sqs_queue`).
//   - CDK/Pulumi:    Properties["construct_type"] (e.g. `sqs.Queue`,
//     `aws.sns.Topic`).
//   - CloudFormation/Bicep: Properties["resource_type"] (e.g.
//     `AWS::SQS::Queue`, `Microsoft.ServiceBus/namespaces/queues`).
func iacResourceTypeOf(e *types.EntityRecord) string {
	if v := strings.TrimSpace(e.Properties["resource_type"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(e.Properties["construct_type"]); v != "" {
		return v
	}
	if e.Metadata != nil {
		for _, k := range []string{"resource_type", "construct_type"} {
			if v, ok := e.Metadata[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	// Terraform entity Name is `<type>.<label>`; the first dotted segment that
	// looks like a resource type (contains an underscore or is a known prefix)
	// is the type.
	if e.Subtype == "resource" && strings.Contains(e.Name, ".") {
		head := e.Name[:strings.Index(e.Name, ".")]
		if head != "" {
			return head
		}
	}
	return ""
}

// iacToolOf returns the IaC tool tag if the entity carries one.
func iacToolOf(e *types.EntityRecord) string {
	if v := strings.TrimSpace(e.Properties["iac_tool"]); v != "" {
		return v
	}
	if e.Metadata != nil {
		if v, ok := e.Metadata["iac_tool"].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// iacChannelName resolves the queue/topic NAME to key the channel by. It prefers
// an explicit name property (the `name`/`queue_name`/`topic_name` literal a user
// set on the resource), falling back to the resource's logical label so a queue
// with no explicit `name=` still appears (keyed by its declaration label).
func iacChannelName(e *types.EntityRecord, resourceType string) string {
	for _, k := range []string{"name", "queue_name", "topic_name", "fifo_name"} {
		if v := strings.TrimSpace(e.Properties[k]); v != "" {
			return v
		}
	}
	if e.Metadata != nil {
		for _, k := range []string{"name", "label", "logical_id"} {
			if v, ok := e.Metadata[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	if v := strings.TrimSpace(e.Properties["logical_id"]); v != "" {
		return v
	}
	// Terraform: Name is `<type>.<label>` → use the label tail.
	if strings.Contains(e.Name, ".") {
		tail := e.Name[strings.LastIndex(e.Name, ".")+1:]
		if tail != "" {
			return tail
		}
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

// iacBrokerForResourceType maps an IaC resource TYPE string (any tool dialect)
// to the canonical broker name used by the topology view. Returns "" when the
// type is not a recognised messaging primitive. Substring + case-insensitive so
// it tolerates every tool's spelling of the "same" resource.
func iacBrokerForResourceType(resourceType string) string {
	t := strings.ToLower(resourceType)
	switch {
	// --- AWS SNS (topic) ---
	case iacContainsAny(t, "aws_sns_topic", "::sns::", "sns.topic", "sns_topic", ".sns.", "snstopic"):
		return "sns"
	// --- AWS SQS (queue) ---
	case iacContainsAny(t, "aws_sqs_queue", "::sqs::", "sqs.queue", "sqs_queue", ".sqs.", "sqsqueue"):
		return "sqs"
	// --- AWS EventBridge (event bus) ---
	case iacContainsAny(t, "::events::eventbus", "eventbus", "aws_cloudwatch_event_bus", "cloudwatchevents", "events.eventbus", "aws_scheduler"):
		return "eventbridge"
	// --- AWS Kinesis / MSK + Kafka (stream) ---
	case iacContainsAny(t, "kinesis", "::msk::", "managedstreaming", "msk", "kafka"):
		return "kafka"
	// --- GCP Pub/Sub (topic) ---
	case iacContainsAny(t, "google_pubsub_topic", "pubsub.topic", "google_pubsub_subscription", "pubsub.subscription", "google_cloud_tasks", "cloudtasks"):
		return "pubsub"
	// --- Azure Service Bus (queue + topic) ---
	case iacContainsAny(t, "microsoft.servicebus/", "servicebus", "azurerm_servicebus", "storagequeue", "microsoft.storage/storageaccounts/queueservices"):
		return "servicebus"
	// --- Azure Event Hubs / Event Grid (stream / topic) ---
	case iacContainsAny(t, "microsoft.eventhub/", "eventhub", "azurerm_eventhub"):
		return "eventhub"
	case iacContainsAny(t, "microsoft.eventgrid/", "eventgrid", "azurerm_eventgrid"):
		return "eventgrid"
	default:
		return ""
	}
}

// iacContainsAny reports whether s contains any of the given substrings. Local
// variadic helper (the package-level containsAny takes a []string slice).
func iacContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// iacChannelQueueID returns the canonical queue entity ID. SQS reuses the exact
// `sqs:<name>` ID the code-side SQS pass mints so an IaC queue collapses onto
// its code publisher/consumer. Other brokers get a `<broker>:<name>` ID.
func iacChannelQueueID(broker, name string) string {
	if broker == "sqs" {
		return sqsQueueID(name)
	}
	return broker + ":" + name
}

// iacChannelTopicID returns the canonical topic entity ID. SNS reuses the exact
// `sns:<name>` ID the code-side SNS pass mints so an IaC topic collapses onto
// its code publisher/subscriber.
func iacChannelTopicID(broker, name string) string {
	if broker == "sns" {
		return snsTopicID(name)
	}
	return broker + ":" + name
}
