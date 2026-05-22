// IaC-declared SNS → SQS fan-out detection — #1596.
//
// The application-side SNS→SQS detection (sqs_edges.go) only sees runtime
// SDK calls (boto3 sns.subscribe, etc.). Fan-out topologies, however, are
// almost always declared in Infrastructure-as-Code, where ONE SNS topic
// subscribes several SQS queues. This pass reads the IaC declarations and
// emits, for each SNS→SQS subscription it can statically recognise:
//
//   - a synthetic SNS topic entity  (SCOPE.Queue, broker=sns, id=sns:<name>)
//   - a synthetic SQS queue entity  (SCOPE.Queue, broker=sqs, id=sqs:<name>)
//   - a SUBSCRIBES_TO edge  FromID=SCOPE.Queue:sqs:<queue>  ToID=SCOPE.Queue:sns:<topic>
//
// The topology view (handlers_topology.go brokerEdges) reads SUBSCRIBES_TO
// edges whose ToID is a Queue entity as that entity's consumers, so the SNS
// topic node renders with one SQS subscriber per subscription. Because the
// topic ID is canonicalised to just the topic name, subscriptions declared
// across DIFFERENT IaC tools (CDK / Terraform / CloudFormation) for the same
// topic name collapse onto a single SNS topic node — producing the intended
// multi-IaC fan-out.
//
// IaC tools covered:
//   - AWS CDK (TypeScript): `new sns.Topic(this, id, {topicName:"x"})` +
//     `topic.addSubscription(new SqsSubscription(queueVar))`, resolving the
//     queue var to its `new sqs.Queue(..., {queueName:"y"})` declaration.
//   - Terraform / HCL: `resource "aws_sns_topic_subscription" "x" { topic_arn=...
//     protocol="sqs" endpoint=aws_sqs_queue.y.arn }` resolving the queue ref
//     to its `resource "aws_sqs_queue" "y" { name="..." }` declaration.
//   - CloudFormation (YAML): `Type: AWS::SNS::Subscription` with `Protocol: sqs`,
//     `TopicArn` (literal ARN, !Ref, or !GetAtt), and `Endpoint` (!GetAtt of
//     an AWS::SQS::Queue resource), resolving the queue logical-id to its
//     QueueName.
//
// # Scope guard
//
// Append-only — this pass never modifies or removes existing entities or
// edges, so it cannot regress the surrounding pipeline's bug-rate.
//
// Refs #1596.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// iacSNSTopicName extracts the bare topic name from a topic name or ARN,
// reusing the package-level snsTopicNameFromARN helper. Defensive against
// stray whitespace.
func iacSNSTopicName(nameOrARN string) string {
	return snsTopicNameFromARN(strings.TrimSpace(nameOrARN))
}

// iacSNSSupportsLanguage reports whether applyIaCSNSEdges scans `lang`.
// CDK arrives as typescript/javascript, Terraform as hcl/terraform,
// CloudFormation as yaml.
func iacSNSSupportsLanguage(lang string) bool {
	switch lang {
	case "javascript", "typescript", "hcl", "terraform", "yaml":
		return true
	default:
		return false
	}
}

// applyIaCSNSEdges is the entry point. Runs after applySQSEdges and is
// append-only.
func applyIaCSNSEdges(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	if !iacSNSSupportsLanguage(lang) {
		return entities, relationships
	}

	src := string(content)
	// Cheap guard: file must mention SNS and a subscription idiom.
	if !strings.Contains(src, "sns") && !strings.Contains(src, "SNS") &&
		!strings.Contains(src, "Sns") {
		return entities, relationships
	}

	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}

	// The SNS topic is a MessageTopic so it collapses onto the same node as
	// any application-side SNS topic of the same name (kafka_wrapper_edges.go
	// emits SNS topics as messageTopicKind).
	emitTopic := func(name, iacTool string) string {
		id := snsTopicID(name)
		if !seenEnt["topic|"+id] {
			seenEnt["topic|"+id] = true
			entities = append(entities, types.EntityRecord{
				Name:     id,
				Kind:     messageTopicKind,
				Language: lang,
				Properties: map[string]string{
					"broker":       "sns",
					"topic_name":   iacSNSTopicName(name),
					"pattern_type": "iac_sns_fanout",
					"iac_tool":     iacTool,
				},
				EnrichmentStatus: types.StatusPending,
				QualityScore:     0.8,
			})
		}
		return id
	}

	emitQueue := func(name, iacTool string) string {
		id := sqsQueueID(name)
		if !seenEnt["queue|"+id] {
			seenEnt["queue|"+id] = true
			entities = append(entities, types.EntityRecord{
				Name:     id,
				Kind:     queueEntityKind,
				Language: lang,
				Properties: map[string]string{
					"broker":       "sqs",
					"queue_name":   sqsQueueDisplayName(name),
					"pattern_type": "iac_sns_fanout",
					"iac_tool":     iacTool,
				},
				EnrichmentStatus: types.StatusPending,
				QualityScore:     0.8,
			})
		}
		return id
	}

	// emitSubscription records SQS-queue --SUBSCRIBES_TO--> SNS-topic so the
	// topology renders the SQS queue as a subscriber of the topic.
	emitSubscription := func(topicName, queueName, iacTool string) {
		if topicName == "" || queueName == "" {
			return
		}
		topicID := emitTopic(topicName, iacTool)
		queueID := emitQueue(queueName, iacTool)
		fromID := fmt.Sprintf("%s:%s", queueEntityKind, queueID)
		toID := fmt.Sprintf("%s:%s", messageTopicKind, topicID)
		key := fromID + "|" + toID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fromID,
			ToID:   toID,
			Kind:   subscribesToEdgeKind,
			Properties: map[string]string{
				"broker":       "sns_sqs",
				"pattern_type": "iac_sns_fanout",
				"sns_fanout":   "true",
				"iac_tool":     iacTool,
			},
		})
	}

	switch lang {
	case "javascript", "typescript":
		applyCDKSNSSubscriptions(src, emitSubscription)
	case "hcl", "terraform":
		applyTerraformSNSSubscriptions(src, emitSubscription)
	case "yaml":
		applyCloudFormationSNSSubscriptions(src, emitSubscription)
	}

	return entities, relationships
}

// ---------------------------------------------------------------------------
// AWS CDK (TypeScript / JavaScript)
// ---------------------------------------------------------------------------

// cdkTopicDeclRe captures `const VAR = new sns.Topic(this, "Id", { topicName: "name" })`.
// Group 1 = JS var name, group 2 = topicName literal.
var cdkTopicDeclRe = regexp.MustCompile(
	`(?:const|let|var)\s+(\w+)\s*=\s*new\s+sns\.Topic\s*\([^)]*?topicName\s*:\s*["'` + "`" + `]([^"'` + "`" + `]+)["'` + "`" + `]`)

// cdkQueueDeclRe captures `const VAR = new sqs.Queue(this, "Id", { queueName: "name" })`.
// Group 1 = JS var name, group 2 = queueName literal.
var cdkQueueDeclRe = regexp.MustCompile(
	`(?:const|let|var)\s+(\w+)\s*=\s*new\s+sqs\.Queue\s*\([^)]*?queueName\s*:\s*["'` + "`" + `]([^"'` + "`" + `]+)["'` + "`" + `]`)

// cdkSubscribeRe captures `topicVar.addSubscription(new SqsSubscription(queueVar))`.
// Group 1 = topic var, group 2 = queue var.
var cdkSubscribeRe = regexp.MustCompile(
	`(\w+)\s*\.addSubscription\s*\(\s*new\s+SqsSubscription\s*\(\s*(\w+)`)

func applyCDKSNSSubscriptions(src string, emit func(topicName, queueName, iacTool string)) {
	if !strings.Contains(src, "addSubscription") || !strings.Contains(src, "SqsSubscription") {
		return
	}
	// Build var → name maps for topics and queues.
	topicNames := map[string]string{}
	for _, m := range cdkTopicDeclRe.FindAllStringSubmatch(src, -1) {
		topicNames[m[1]] = m[2]
	}
	queueNames := map[string]string{}
	for _, m := range cdkQueueDeclRe.FindAllStringSubmatch(src, -1) {
		queueNames[m[1]] = m[2]
	}
	for _, m := range cdkSubscribeRe.FindAllStringSubmatch(src, -1) {
		topicName := topicNames[m[1]]
		queueName := queueNames[m[2]]
		if topicName == "" || queueName == "" {
			continue
		}
		emit(topicName, queueName, "cdk")
	}
}

// ---------------------------------------------------------------------------
// Terraform / HCL
// ---------------------------------------------------------------------------

// tfQueueResRe captures `resource "aws_sqs_queue" "ident" { ... name = "x" ... }`.
// Group 1 = HCL ident, group 2 = block body.
var tfQueueResRe = regexp.MustCompile(`(?s)resource\s+"aws_sqs_queue"\s+"(\w+)"\s*\{(.*?)\n\}`)

// tfQueueNameRe extracts `name = "x"` from a queue block body.
var tfQueueNameRe = regexp.MustCompile(`name\s*=\s*"([^"]+)"`)

// tfSubscriptionResRe captures the whole subscription resource block body.
var tfSubscriptionResRe = regexp.MustCompile(`(?s)resource\s+"aws_sns_topic_subscription"\s+"(\w+)"\s*\{(.*?)\n\}`)

// tfProtocolSQSRe matches `protocol = "sqs"` in a subscription block.
var tfProtocolSQSRe = regexp.MustCompile(`protocol\s*=\s*"sqs"`)

// tfTopicArnLiteralRe matches `topic_arn = "arn:...:name"`.
var tfTopicArnLiteralRe = regexp.MustCompile(`topic_arn\s*=\s*"([^"]+)"`)

// tfTopicArnRefRe matches `topic_arn = aws_sns_topic.ident.arn`.
var tfTopicArnRefRe = regexp.MustCompile(`topic_arn\s*=\s*aws_sns_topic\.(\w+)\.arn`)

// tfTopicResRe captures `resource "aws_sns_topic" "ident" { ... name="x" ... }`.
var tfTopicResRe = regexp.MustCompile(`(?s)resource\s+"aws_sns_topic"\s+"(\w+)"\s*\{(.*?)\n\}`)

// tfEndpointQueueRefRe matches `endpoint = aws_sqs_queue.ident.arn`.
var tfEndpointQueueRefRe = regexp.MustCompile(`endpoint\s*=\s*aws_sqs_queue\.(\w+)\.arn`)

// tfEndpointArnLiteralRe matches `endpoint = "arn:aws:sqs:...:queue-name"`.
var tfEndpointArnLiteralRe = regexp.MustCompile(`endpoint\s*=\s*"([^"]+)"`)

func applyTerraformSNSSubscriptions(src string, emit func(topicName, queueName, iacTool string)) {
	if !strings.Contains(src, "aws_sns_topic_subscription") {
		return
	}
	// Resolve aws_sqs_queue idents → queue names.
	queueNames := map[string]string{}
	for _, m := range tfQueueResRe.FindAllStringSubmatch(src, -1) {
		if nm := tfQueueNameRe.FindStringSubmatch(m[2]); nm != nil {
			queueNames[m[1]] = nm[1]
		}
	}
	// Resolve aws_sns_topic idents → topic names.
	topicNames := map[string]string{}
	for _, m := range tfTopicResRe.FindAllStringSubmatch(src, -1) {
		if nm := tfQueueNameRe.FindStringSubmatch(m[2]); nm != nil {
			topicNames[m[1]] = nm[1]
		}
	}

	for _, m := range tfSubscriptionResRe.FindAllStringSubmatch(src, -1) {
		body := m[2]
		if !tfProtocolSQSRe.MatchString(body) {
			continue
		}
		// Resolve topic name.
		topicName := ""
		if lm := tfTopicArnLiteralRe.FindStringSubmatch(body); lm != nil {
			topicName = iacSNSTopicName(lm[1])
		} else if rm := tfTopicArnRefRe.FindStringSubmatch(body); rm != nil {
			topicName = topicNames[rm[1]]
		}
		// Resolve queue name.
		queueName := ""
		if qm := tfEndpointQueueRefRe.FindStringSubmatch(body); qm != nil {
			queueName = queueNames[qm[1]]
		} else if em := tfEndpointArnLiteralRe.FindStringSubmatch(body); em != nil {
			queueName = sqsQueueDisplayName(em[1])
		}
		if topicName == "" || queueName == "" {
			continue
		}
		emit(topicName, queueName, "terraform")
	}
}

// ---------------------------------------------------------------------------
// CloudFormation (YAML)
// ---------------------------------------------------------------------------

// cfnResourceBlockRe captures each top-level resource: `  LogicalId:\n    Type: ...`.
// We split the Resources section into blocks keyed by logical id and scan
// each block.
var cfnResourceHeaderRe = regexp.MustCompile(`(?m)^  (\w+):\s*$`)

// cfnTypeRe extracts the `Type:` of a resource block.
var cfnTypeRe = regexp.MustCompile(`Type:\s*([\w:]+)`)

// cfnQueueNameRe extracts `QueueName: x`.
var cfnQueueNameRe = regexp.MustCompile(`QueueName:\s*["']?([\w.\-]+)["']?`)

// cfnProtocolSQSRe matches `Protocol: sqs`.
var cfnProtocolSQSRe = regexp.MustCompile(`Protocol:\s*["']?sqs["']?`)

// cfnTopicArnLiteralRe matches `TopicArn: "arn:...:name"` or `TopicArn: arn:...:name`.
var cfnTopicArnLiteralRe = regexp.MustCompile(`TopicArn:\s*["']?(arn:aws:sns:[^"'\s]+)["']?`)

// cfnTopicArnRefRe matches `TopicArn: !Ref Logical` or `TopicArn: { "Ref": "Logical" }`.
var cfnTopicArnRefRe = regexp.MustCompile(`TopicArn:\s*(?:!Ref\s+(\w+)|\{\s*["']?Ref["']?\s*:\s*["'](\w+)["']\s*\})`)

// cfnTopicNamePropRe matches `TopicName: x` inside an AWS::SNS::Topic resource.
var cfnTopicNamePropRe = regexp.MustCompile(`TopicName:\s*["']?([\w.\-]+)["']?`)

// cfnEndpointGetAttRe matches `Endpoint: !GetAtt Logical.Arn`.
var cfnEndpointGetAttRe = regexp.MustCompile(`Endpoint:\s*(?:!GetAtt\s+(\w+)\.Arn|\{\s*["']?Fn::GetAtt["']?\s*:\s*\[\s*["'](\w+)["'])`)

// cfnEndpointArnLiteralRe matches `Endpoint: "arn:aws:sqs:...:queue"`.
var cfnEndpointArnLiteralRe = regexp.MustCompile(`Endpoint:\s*["']?(arn:aws:sqs:[^"'\s]+)["']?`)

// cfnParamDefaultArnRe scans Parameters for `Default: "arn:aws:sns:...:name"`.
var cfnParamDefaultArnRe = regexp.MustCompile(`Default:\s*["']?(arn:aws:sns:[^"'\s]+)["']?`)

// cfnResourceBlock is a parsed CloudFormation resource block.
type cfnResourceBlock struct {
	logicalID string
	typ       string
	body      string
}

// splitCFNResources returns the list of resource blocks under Resources:.
func splitCFNResources(src string) []cfnResourceBlock {
	// Find the Resources: section start.
	idx := strings.Index(src, "\nResources:")
	if idx < 0 {
		if strings.HasPrefix(src, "Resources:") {
			idx = 0
		} else {
			return nil
		}
	}
	section := src[idx:]
	headers := cfnResourceHeaderRe.FindAllStringSubmatchIndex(section, -1)
	var blocks []cfnResourceBlock
	for i, h := range headers {
		id := section[h[2]:h[3]]
		start := h[1]
		end := len(section)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}
		body := section[start:end]
		typ := ""
		if tm := cfnTypeRe.FindStringSubmatch(body); tm != nil {
			typ = tm[1]
		}
		blocks = append(blocks, cfnResourceBlock{logicalID: id, typ: typ, body: body})
	}
	return blocks
}

func applyCloudFormationSNSSubscriptions(src string, emit func(topicName, queueName, iacTool string)) {
	if !strings.Contains(src, "AWS::SNS::Subscription") {
		return
	}
	blocks := splitCFNResources(src)
	if len(blocks) == 0 {
		return
	}

	// Build logical-id → SQS queue name, logical-id → SNS topic name.
	queueNames := map[string]string{}
	topicNames := map[string]string{}
	for _, b := range blocks {
		switch b.typ {
		case "AWS::SQS::Queue":
			if qm := cfnQueueNameRe.FindStringSubmatch(b.body); qm != nil {
				queueNames[b.logicalID] = qm[1]
			} else {
				// No explicit QueueName — fall back to logical id.
				queueNames[b.logicalID] = b.logicalID
			}
		case "AWS::SNS::Topic":
			if tm := cfnTopicNamePropRe.FindStringSubmatch(b.body); tm != nil {
				topicNames[b.logicalID] = tm[1]
			} else {
				topicNames[b.logicalID] = b.logicalID
			}
		}
	}

	// A topic ARN may come in via a Parameter default (e.g. OrderEventsTopicArn).
	paramDefaultTopic := ""
	if pm := cfnParamDefaultArnRe.FindStringSubmatch(src); pm != nil {
		paramDefaultTopic = iacSNSTopicName(pm[1])
	}

	for _, b := range blocks {
		if b.typ != "AWS::SNS::Subscription" {
			continue
		}
		if !cfnProtocolSQSRe.MatchString(b.body) {
			continue
		}
		// Resolve topic name.
		topicName := ""
		if lm := cfnTopicArnLiteralRe.FindStringSubmatch(b.body); lm != nil {
			topicName = iacSNSTopicName(lm[1])
		} else if rm := cfnTopicArnRefRe.FindStringSubmatch(b.body); rm != nil {
			ref := rm[1]
			if ref == "" {
				ref = rm[2]
			}
			if tn, ok := topicNames[ref]; ok {
				topicName = tn
			} else if paramDefaultTopic != "" {
				// !Ref to a Parameter whose Default is a topic ARN.
				topicName = paramDefaultTopic
			}
		}
		// Resolve queue name.
		queueName := ""
		if gm := cfnEndpointGetAttRe.FindStringSubmatch(b.body); gm != nil {
			ref := gm[1]
			if ref == "" {
				ref = gm[2]
			}
			queueName = queueNames[ref]
		} else if em := cfnEndpointArnLiteralRe.FindStringSubmatch(b.body); em != nil {
			queueName = sqsQueueDisplayName(em[1])
		}
		if topicName == "" || queueName == "" {
			continue
		}
		emit(topicName, queueName, "cloudformation")
	}
}
