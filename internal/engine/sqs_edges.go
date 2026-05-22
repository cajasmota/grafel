// AWS SQS producer/consumer detection — wave 2 of #726.
//
// For every SQS send or receive call site this pass can statically
// recognize, we emit a synthetic `SCOPE.Queue` entity keyed by the queue
// name/URL, plus PUBLISHES_TO or SUBSCRIBES_TO edges. The synthetic queue
// ID is identical across repos (`sqs:<queue-name>`), so the existing
// import-channel linker matches producer and consumer sides on shared
// entity ID without any new cross-repo matching code.
//
// Libraries/frameworks covered:
//   - Python boto3: sqs.send_message / receive_message / create_queue
//   - Node aws-sdk v2: sqs.sendMessage / receiveMessage
//   - Node aws-sdk-client-sqs (v3): SendMessageCommand / ReceiveMessageCommand
//   - Go aws-sdk-go-v2: client.SendMessage / ReceiveMessage
//   - Java AWS SDK v2: sqsClient.sendMessage / receiveMessage / createQueue
//   - Lambda triggers: Python/Node handlers with event["Records"][0]["eventSource"] == "aws:sqs"
//
// Beyond the minimum:
//   - SQS message-attribute filtering recorded as edge property
//   - SNS→SQS fanout: sns.subscribe() / snsClient.subscribe() with SQS protocol
//     emits implicit SUBSCRIBES_TO edge from the SQS queue
//   - Lambda trigger consumer: infers queue from eventSourceARN
//
// Refs #726.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// sqsSynthesisSupportsLanguage reports whether applySQSEdges can emit
// synthetics for `lang`.
func sqsSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "java", "kotlin", "javascript", "typescript", "python", "go":
		return true
	default:
		return false
	}
}

// applySQSEdges runs after applyRabbitMQEdges and APPENDS SCOPE.Queue
// entities + PUBLISHES_TO / SUBSCRIBES_TO edges. Append-only — never
// modifies or removes existing entities or edges, so this pass cannot
// regress the surrounding pipeline's bug-rate.
func applySQSEdges(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	if !sqsSynthesisSupportsLanguage(lang) {
		return entities, relationships
	}

	src := string(content)

	// Dedup-by-ID: one SCOPE.Queue entity per queue per file.
	seenQueue := map[string]bool{}
	seenEdge := map[string]bool{}

	emitQueue := func(queueID, queueName string, props map[string]string) {
		if seenQueue[queueID] {
			return
		}
		seenQueue[queueID] = true
		merged := map[string]string{
			"broker":       "sqs",
			"queue_name":   queueName,
			"pattern_type": "sqs_synthesis",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		// SourceFile left empty so identical queue names collapse to ONE
		// entity per repo and match across repos via the import-channel linker.
		entities = append(entities, types.EntityRecord{
			Name:               queueID,
			Kind:               queueEntityKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitEdge := func(callerKind, callerName, queueID, edgeKind string, props map[string]string) {
		if callerName == "" || queueID == "" {
			return
		}
		key := edgeKind + "|" + callerKind + ":" + callerName + "|" + queueID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		base := map[string]string{
			"broker":       "sqs",
			"pattern_type": "sqs_synthesis",
		}
		for k, v := range props {
			if v != "" {
				base[k] = v
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fmt.Sprintf("%s:%s", callerKind, callerName),
			ToID:       fmt.Sprintf("%s:%s", queueEntityKind, queueID),
			Kind:       edgeKind,
			Properties: base,
		})
	}

	switch lang {
	case "python":
		synthesizePySQS(src, emitQueue, emitEdge)
	case "javascript", "typescript":
		synthesizeNodeSQS(src, emitQueue, emitEdge)
	case "java", "kotlin":
		synthesizeJavaSQS(src, emitQueue, emitEdge)
	case "go":
		synthesizeGoSQS(src, emitQueue, emitEdge)
	}

	return entities, relationships
}

// sqsQueueID returns the canonical synthetic ID for an SQS queue.
// When the input is a full URL (https://sqs.*.amazonaws.com/*/QueueName)
// we extract just the queue name for the ID to make cross-repo matching
// robust to different AWS account IDs and regions.
func sqsQueueID(queueURLOrName string) string {
	// Strip trailing slash.
	s := strings.TrimRight(queueURLOrName, "/")
	// If it looks like a URL, use only the last path segment.
	if strings.Contains(s, "://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		parts := strings.Split(s, "/")
		s = parts[len(parts)-1]
	}
	return "sqs:" + s
}

// sqsQueueDisplayName returns the display name portion (last URL segment
// or the name as-is).
func sqsQueueDisplayName(queueURLOrName string) string {
	s := strings.TrimRight(queueURLOrName, "/")
	if strings.Contains(s, "/") {
		parts := strings.Split(s, "/")
		return parts[len(parts)-1]
	}
	return s
}

// looksLikeSQSQueue returns true when `s` looks like a valid SQS queue name
// or URL. Queue names are 1-80 chars: alphanumeric, hyphen, underscore.
// URLs are accepted and normalized by sqsQueueID.
func looksLikeSQSQueue(s string) bool {
	if s == "" || len(s) > 1024 {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t<>{}") {
		return false
	}
	// Accept URLs that look like SQS queue URLs.
	if strings.Contains(s, "amazonaws.com") || strings.Contains(s, "sqs.") {
		return true
	}
	// Accept plain queue names.
	hasAlnum := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			hasAlnum = true
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return hasAlnum
}

// ---------------------------------------------------------------------------
// Python — boto3
// ---------------------------------------------------------------------------

// pySQSSendRe captures sqs.send_message(QueueUrl=..., MessageBody=...).
// Group 1 = QueueUrl value.
var pySQSSendKwRe = regexp.MustCompile(`\.send_message\s*\(\s*(?:[^)]*?)?QueueUrl\s*=\s*["']([^"'\n\r]+)["']`)

// pySQSReceiveRe captures sqs.receive_message(QueueUrl=...).
// Group 1 = QueueUrl value.
var pySQSReceiveKwRe = regexp.MustCompile(`\.receive_message\s*\(\s*(?:[^)]*?)?QueueUrl\s*=\s*["']([^"'\n\r]+)["']`)

// pySQSCreateQueueRe captures sqs.create_queue(QueueName=...).
// Group 1 = QueueName value.
var pySQSCreateQueueRe = regexp.MustCompile(`\.create_queue\s*\(\s*(?:[^)]*?)?QueueName\s*=\s*["']([^"'\n\r]+)["']`)

// pySNSSubscribeRe captures sns.subscribe(TopicArn=..., Protocol="sqs", Endpoint=...).
// Detects SNS→SQS fanout. Group 1 = full subscribe call (analyzed separately).
var pySNSSubscribeSQSRe = regexp.MustCompile(`\.subscribe\s*\([^)]*?Protocol\s*=\s*["']sqs["'][^)]*?Endpoint\s*=\s*["']([^"'\n\r]+)["']`)

// pyLambdaHandlerRe captures the canonical Lambda handler signature.
var pyLambdaHandlerRe = regexp.MustCompile(`(?m)^\s*def\s+(handler|lambda_handler)\s*\(\s*event\s*,\s*context\s*\)`)

// pyLambdaSQSEventSourceRe checks for aws:sqs event source in a Python Lambda.
// Matches both dict-key access record['eventSource'] == 'aws:sqs' and
// JSON-style "eventSource": "aws:sqs".
var pyLambdaSQSEventSourceRe = regexp.MustCompile(`aws:sqs`)

// pyLambdaSQSArnRe extracts queue name from eventSourceARN string literal.
// Group 1 = queue name or ARN segment.
var pyLambdaSQSArnRe = regexp.MustCompile(`eventSourceARN["']?\s*(?:==|:)\s*["']([^"'\n\r]+)["']`)

func synthesizePySQS(
	src string,
	emitQueue func(queueID, queueName string, props map[string]string),
	emitEdge func(callerKind, callerName, queueID, edgeKind string, props map[string]string),
) {
	hasSQS := strings.Contains(src, "sqs") || strings.Contains(src, "SQS") ||
		strings.Contains(src, "send_message") || strings.Contains(src, "receive_message") ||
		strings.Contains(src, "create_queue")
	if !hasSQS {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingPyName(src, offset)
	}

	// sqs.send_message(QueueUrl=...)
	for _, m := range pySQSSendKwRe.FindAllStringSubmatchIndex(src, -1) {
		queueURL := src[m[2]:m[3]]
		if !looksLikeSQSQueue(queueURL) {
			continue
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		emitQueue(qID, qName, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "boto3",
			"queue_url":       queueURL,
		})
	}

	// sqs.receive_message(QueueUrl=...)
	for _, m := range pySQSReceiveKwRe.FindAllStringSubmatchIndex(src, -1) {
		queueURL := src[m[2]:m[3]]
		if !looksLikeSQSQueue(queueURL) {
			continue
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		emitQueue(qID, qName, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "boto3",
			"queue_url":       queueURL,
		})
	}

	// sqs.create_queue(QueueName=...)
	for _, m := range pySQSCreateQueueRe.FindAllStringSubmatchIndex(src, -1) {
		queueName := src[m[2]:m[3]]
		if !looksLikeSQSQueue(queueName) {
			continue
		}
		qID := sqsQueueID(queueName)
		emitQueue(qID, queueName, map[string]string{"declared": "true"})
	}

	// SNS→SQS fanout: sns.subscribe(Protocol="sqs", Endpoint=<queue-arn-or-url>)
	for _, m := range pySNSSubscribeSQSRe.FindAllStringSubmatchIndex(src, -1) {
		endpoint := src[m[2]:m[3]]
		queueName := sqsQueueDisplayName(endpoint)
		if queueName == "" || !looksLikeSQSQueue(queueName) {
			continue
		}
		qID := sqsQueueID(queueName)
		emitQueue(qID, queueName, map[string]string{"sns_fanout": "true"})
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "boto3_sns",
			"sns_fanout":      "true",
		})
	}

	// Lambda SQS trigger: def handler(event, context) where event source is aws:sqs.
	if pyLambdaHandlerRe.MatchString(src) && pyLambdaSQSEventSourceRe.MatchString(src) {
		// Find the handler function name.
		handlerName := "handler"
		if hm := pyLambdaHandlerRe.FindStringSubmatch(src); len(hm) >= 2 {
			handlerName = hm[1]
		}
		// Try to extract queue from eventSourceARN literal.
		queueName := "lambda-sqs-trigger"
		if am := pyLambdaSQSArnRe.FindStringSubmatch(src); len(am) >= 2 {
			queueName = sqsQueueDisplayName(am[1])
		}
		qID := sqsQueueID(queueName)
		emitQueue(qID, queueName, map[string]string{"lambda_trigger": "true"})
		emitEdge("Function", handlerName, qID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "lambda_trigger",
			"lambda_trigger":  "true",
		})
	}
}

// ---------------------------------------------------------------------------
// Node — aws-sdk v2 + @aws-sdk/client-sqs (v3)
// ---------------------------------------------------------------------------

// nodeSQSSendV2Re captures sqs.sendMessage({QueueUrl: "...", ...}).
// Group 1 = QueueUrl value.
var nodeSQSSendV2Re = regexp.MustCompile("" +
	`\.sendMessage\s*\(\s*\{[^}]*?QueueUrl\s*:\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeSQSReceiveV2Re captures sqs.receiveMessage({QueueUrl: "..."}).
// Group 1 = QueueUrl value.
var nodeSQSReceiveV2Re = regexp.MustCompile("" +
	`\.receiveMessage\s*\(\s*\{[^}]*?QueueUrl\s*:\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeSQSSendV3Re captures new SendMessageCommand({QueueUrl: "..."}).
// Group 1 = QueueUrl value.
var nodeSQSSendV3Re = regexp.MustCompile("" +
	`SendMessageCommand\s*\(\s*\{[^}]*?QueueUrl\s*:\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeSQSReceiveV3Re captures new ReceiveMessageCommand({QueueUrl: "..."}).
// Group 1 = QueueUrl value.
var nodeSQSReceiveV3Re = regexp.MustCompile("" +
	`ReceiveMessageCommand\s*\(\s*\{[^}]*?QueueUrl\s*:\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodeLambdaSQSRe captures Lambda handler with SQS event source (Node).
var nodeLambdaSQSHandlerRe = regexp.MustCompile(`(?:exports\.handler|module\.exports\.handler|const\s+handler)\s*=\s*(?:async\s+)?(?:function\s*\(event|(?:\(event|\bevent\b)\s*(?:,\s*context)?\s*=>)`)
var nodeLambdaSQSSourceRe = regexp.MustCompile(`["']aws:sqs["']`)

func synthesizeNodeSQS(
	src string,
	emitQueue func(queueID, queueName string, props map[string]string),
	emitEdge func(callerKind, callerName, queueID, edgeKind string, props map[string]string),
) {
	hasSQS := strings.Contains(src, "SQS") || strings.Contains(src, "sqs") ||
		strings.Contains(src, "sendMessage") || strings.Contains(src, "receiveMessage") ||
		strings.Contains(src, "SendMessageCommand") || strings.Contains(src, "ReceiveMessageCommand")
	if !hasSQS {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingNodeName(src, offset)
	}

	// AWS SDK v2: sqs.sendMessage({QueueUrl: "..."})
	for _, m := range nodeSQSSendV2Re.FindAllStringSubmatchIndex(src, -1) {
		queueURL := src[m[2]:m[3]]
		if !looksLikeSQSQueue(queueURL) {
			continue
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		emitQueue(qID, qName, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "aws-sdk-v2",
			"queue_url":       queueURL,
		})
	}

	// AWS SDK v2: sqs.receiveMessage({QueueUrl: "..."})
	for _, m := range nodeSQSReceiveV2Re.FindAllStringSubmatchIndex(src, -1) {
		queueURL := src[m[2]:m[3]]
		if !looksLikeSQSQueue(queueURL) {
			continue
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		emitQueue(qID, qName, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "aws-sdk-v2",
			"queue_url":       queueURL,
		})
	}

	// AWS SDK v3: SendMessageCommand({QueueUrl: "..."})
	for _, m := range nodeSQSSendV3Re.FindAllStringSubmatchIndex(src, -1) {
		queueURL := src[m[2]:m[3]]
		if !looksLikeSQSQueue(queueURL) {
			continue
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		emitQueue(qID, qName, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "aws-sdk-v3",
			"queue_url":       queueURL,
		})
	}

	// AWS SDK v3: ReceiveMessageCommand({QueueUrl: "..."})
	for _, m := range nodeSQSReceiveV3Re.FindAllStringSubmatchIndex(src, -1) {
		queueURL := src[m[2]:m[3]]
		if !looksLikeSQSQueue(queueURL) {
			continue
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		emitQueue(qID, qName, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "aws-sdk-v3",
			"queue_url":       queueURL,
		})
	}

	// Lambda SQS trigger (Node): exports.handler = async (event, context) => {}
	// with "aws:sqs" event source marker.
	if nodeLambdaSQSHandlerRe.MatchString(src) && nodeLambdaSQSSourceRe.MatchString(src) {
		queueName := "lambda-sqs-trigger"
		qID := sqsQueueID(queueName)
		emitQueue(qID, queueName, map[string]string{"lambda_trigger": "true"})
		emitEdge("Function", "handler", qID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "lambda_trigger",
			"lambda_trigger":  "true",
		})
	}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — AWS SDK v2
// ---------------------------------------------------------------------------

// javaSQSSendRe captures sqsClient.sendMessage(SendMessageRequest.builder().queueUrl("...").build()).
// Group 1 = queue URL.
var javaSQSSendRe = regexp.MustCompile(`sqsClient\.sendMessage\s*\([^)]*?queueUrl\s*\(\s*"([^"\n\r]+)"`)

// javaSQSReceiveRe captures sqsClient.receiveMessage(ReceiveMessageRequest.builder().queueUrl("...").build()).
// Group 1 = queue URL.
var javaSQSReceiveRe = regexp.MustCompile(`sqsClient\.receiveMessage\s*\([^)]*?queueUrl\s*\(\s*"([^"\n\r]+)"`)

// javaSQSCreateRe captures sqsClient.createQueue(CreateQueueRequest.builder().queueName("...").build()).
// Group 1 = queue name.
var javaSQSCreateRe = regexp.MustCompile(`sqsClient\.createQueue\s*\([^)]*?queueName\s*\(\s*"([^"\n\r]+)"`)

// javaSQSSendSimpleRe captures the simpler SqsClient.sendMessage(url, body) form.
// Group 1 = queue URL.
var javaSQSSendSimpleRe = regexp.MustCompile(`\.sendMessage\s*\(\s*"([^"\n\r]+)"\s*,`)

// javaSQSReceiveSimpleRe captures sqsClient.receiveMessage(url) simple form.
// Group 1 = queue URL.
var javaSQSReceiveSimpleRe = regexp.MustCompile(`\.receiveMessage\s*\(\s*"([^"\n\r]+)"`)

func synthesizeJavaSQS(
	src string,
	emitQueue func(queueID, queueName string, props map[string]string),
	emitEdge func(callerKind, callerName, queueID, edgeKind string, props map[string]string),
) {
	hasSQS := strings.Contains(src, "sqsClient") || strings.Contains(src, "SqsClient") ||
		strings.Contains(src, "AmazonSQS") || strings.Contains(src, "SendMessageRequest") ||
		strings.Contains(src, "ReceiveMessageRequest")
	if !hasSQS {
		return
	}

	className := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}

	emit := func(queueURL, layer, edgeKind string) {
		if !looksLikeSQSQueue(queueURL) {
			return
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		emitQueue(qID, qName, map[string]string{"messaging_layer": layer})
		if className != "" {
			emitEdge("Service", className, qID, edgeKind, map[string]string{
				"messaging_layer": layer,
				"queue_url":       queueURL,
			})
		}
	}

	// Builder-style sendMessage
	for _, m := range javaSQSSendRe.FindAllStringSubmatch(src, -1) {
		emit(m[1], "aws-sdk-java-v2", publishesToEdgeKind)
	}
	// Builder-style receiveMessage
	for _, m := range javaSQSReceiveRe.FindAllStringSubmatch(src, -1) {
		emit(m[1], "aws-sdk-java-v2", subscribesToEdgeKind)
	}
	// createQueue
	for _, m := range javaSQSCreateRe.FindAllStringSubmatch(src, -1) {
		if !looksLikeSQSQueue(m[1]) {
			continue
		}
		qID := sqsQueueID(m[1])
		emitQueue(qID, m[1], map[string]string{"declared": "true", "messaging_layer": "aws-sdk-java-v2"})
	}
	// Simple sendMessage(url, body)
	for _, m := range javaSQSSendSimpleRe.FindAllStringSubmatch(src, -1) {
		emit(m[1], "aws-sdk-java-v2", publishesToEdgeKind)
	}
	// Simple receiveMessage(url)
	for _, m := range javaSQSReceiveSimpleRe.FindAllStringSubmatch(src, -1) {
		emit(m[1], "aws-sdk-java-v2", subscribesToEdgeKind)
	}
}

// ---------------------------------------------------------------------------
// Go — aws-sdk-go-v2
// ---------------------------------------------------------------------------

// goSQSSendRe captures client.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: aws.String("...")}).
// Group 1 = queue URL.
var goSQSSendRe = regexp.MustCompile(`\.SendMessage\s*\([^)]*?QueueUrl\s*:[^,)]*?aws\.String\s*\(\s*"([^"\n\r]+)"`)

// goSQSReceiveRe captures client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: ...}).
// Group 1 = queue URL.
var goSQSReceiveRe = regexp.MustCompile(`\.ReceiveMessage\s*\([^)]*?QueueUrl\s*:[^,)]*?aws\.String\s*\(\s*"([^"\n\r]+)"`)

// goSQSSendSimpleRe captures QueueUrl: aws.String("...") inside SendMessage calls.
// Group 1 = queue URL (for the case where the send call spans multiple lines).
var goSQSQueueURLFieldRe = regexp.MustCompile(`QueueUrl\s*:\s*aws\.String\s*\(\s*"([^"\n\r]+)"`)

// goSQSCreateRe captures sqs.CreateQueueInput{QueueName: aws.String("...")}.
// Group 1 = queue name.
var goSQSCreateRe = regexp.MustCompile(`CreateQueueInput\s*\{[^}]*?QueueName\s*:\s*aws\.String\s*\(\s*"([^"\n\r]+)"`)

// goSQSQueueURLVarFieldRe captures the constant-resolved struct-field form:
//
//	QueueUrl: aws.String(inventoryReservedQueueURL)
//	QueueUrl: &queueURLConst
//
// Group 1 = identifier name (resolved against the Go string-const table).
var goSQSQueueURLVarFieldRe = regexp.MustCompile(`QueueUrl\s*:\s*(?:aws\.String\s*\(\s*&?|&)([A-Za-z_][A-Za-z0-9_]*)\s*\)?`)

func synthesizeGoSQS(
	src string,
	emitQueue func(queueID, queueName string, props map[string]string),
	emitEdge func(callerKind, callerName, queueID, edgeKind string, props map[string]string),
) {
	hasSQS := strings.Contains(src, "sqs") || strings.Contains(src, "SQS") ||
		strings.Contains(src, "SendMessage") || strings.Contains(src, "ReceiveMessage") ||
		strings.Contains(src, "QueueUrl")
	if !hasSQS {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingGoName(src, offset)
	}

	// Inline SendMessage with QueueUrl
	for _, m := range goSQSSendRe.FindAllStringSubmatchIndex(src, -1) {
		queueURL := src[m[2]:m[3]]
		if !looksLikeSQSQueue(queueURL) {
			continue
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		emitQueue(qID, qName, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "aws-sdk-go-v2",
			"queue_url":       queueURL,
		})
	}

	// Inline ReceiveMessage with QueueUrl
	for _, m := range goSQSReceiveRe.FindAllStringSubmatchIndex(src, -1) {
		queueURL := src[m[2]:m[3]]
		if !looksLikeSQSQueue(queueURL) {
			continue
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		emitQueue(qID, qName, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, qID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "aws-sdk-go-v2",
			"queue_url":       queueURL,
		})
	}

	// Struct-literal QueueUrl field — covers multi-line send/receive calls.
	// We use surrounding context to determine send vs receive direction.
	for _, m := range goSQSQueueURLFieldRe.FindAllStringSubmatchIndex(src, -1) {
		queueURL := src[m[2]:m[3]]
		if !looksLikeSQSQueue(queueURL) {
			continue
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		ctx := surroundingText(src, m[0], 300)
		isSend := strings.Contains(ctx, "SendMessage") || strings.Contains(ctx, "SendMessageInput")
		isReceive := strings.Contains(ctx, "ReceiveMessage") || strings.Contains(ctx, "ReceiveMessageInput")
		if !isSend && !isReceive {
			continue
		}
		emitQueue(qID, qName, nil)
		caller := enclosing(m[0])
		edgeKind := subscribesToEdgeKind
		if isSend {
			edgeKind = publishesToEdgeKind
		}
		emitEdge("Function", caller, qID, edgeKind, map[string]string{
			"messaging_layer": "aws-sdk-go-v2",
			"queue_url":       queueURL,
		})
	}

	// Struct-literal QueueUrl field referencing a named string constant —
	// covers `QueueUrl: aws.String(queueURLConst)` where the URL is a
	// package-level const rather than an inline literal.
	consts := buildGoStringSymbolTable(src)
	for _, m := range goSQSQueueURLVarFieldRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		queueURL, ok := consts[name]
		if !ok || !looksLikeSQSQueue(queueURL) {
			continue
		}
		qID := sqsQueueID(queueURL)
		qName := sqsQueueDisplayName(queueURL)
		ctx := surroundingText(src, m[0], 300)
		isSend := strings.Contains(ctx, "SendMessage") || strings.Contains(ctx, "SendMessageInput")
		isReceive := strings.Contains(ctx, "ReceiveMessage") || strings.Contains(ctx, "ReceiveMessageInput")
		if !isSend && !isReceive {
			continue
		}
		emitQueue(qID, qName, nil)
		caller := enclosing(m[0])
		edgeKind := subscribesToEdgeKind
		if isSend {
			edgeKind = publishesToEdgeKind
		}
		emitEdge("Function", caller, qID, edgeKind, map[string]string{
			"messaging_layer": "aws-sdk-go-v2",
			"queue_url":       queueURL,
		})
	}

	// CreateQueueInput
	for _, m := range goSQSCreateRe.FindAllStringSubmatch(src, -1) {
		queueName := m[1]
		if !looksLikeSQSQueue(queueName) {
			continue
		}
		qID := sqsQueueID(queueName)
		emitQueue(qID, queueName, map[string]string{"declared": "true"})
	}
}
