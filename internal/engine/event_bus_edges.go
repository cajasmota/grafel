// Managed event-bus producer/consumer detection — #927.
//
// Three append-only sub-detectors in one pass:
//
//  1. AWS EventBridge
//     Producers: boto3 `events.put_events(Entries=[{Source,DetailType,...}])` (Python),
//     `EventBridgeClient.send(new PutEventsCommand({Entries:[...]})` (Node/TS),
//     `ebClient.PutEvents(ctx, &eventbridge.PutEventsInput{Entries:…})` (Go).
//     Consumers / IaC rules: Terraform `aws_cloudwatch_event_rule` with `event_pattern`
//     JSON containing `source` + `detail-type` fields; CDK / serverless.yml patterns
//     detected via regex over raw text. Rule targets resolved to Lambda via
//     lambdaFunctionID() from serverless_edges.go (#925).
//     Synthetics: SCOPE.EventBusEvent keyed by `event:eventbridge:<source>:<detail-type>`.
//     Edges: PUBLISHES_TO (producer→synthetic), SUBSCRIBES_TO (rule→synthetic),
//     EVENTBRIDGE_TRIGGERS (rule→lambda target).
//
//  2. Azure EventGrid
//     Producers: `EventGridPublisherClient.send(events)` (Python azure-eventgrid,
//     Node @azure/eventgrid), `EventGridSenderClient.sendEvents` (Node SDK v12+).
//     Consumers: `@app.event_grid_trigger(name='X')` (Python v2 model),
//     `EventGridTrigger` binding attribute (C# / Python), Azure Function
//     receiving an EventGridEvent.
//     Synthetics: SCOPE.EventBusEvent keyed by `event:eventgrid:<topic>:<event-type>`.
//     Edges: PUBLISHES_TO, SUBSCRIBES_TO, EVENTGRID_TRIGGERS.
//
//  3. CNCF CloudEvents (spec-level, language-agnostic)
//     Producers: `CloudEvent(...)` builder + any HTTP POST with `ce-type`/`ce-source`
//     headers, Go `cloudevents.NewEvent()`, Python cloudevents SDK `CloudEvent(...)`,
//     Node `new CloudEvent(...)`.
//     Consumers: HTTP handler that reads `ce-type`/`ce-source` headers; `@app.route`
//     decorated with cloudevents handler; Go `cloudevents.NewClientHTTP()` receiver.
//     Synthetics: SCOPE.EventBusEvent keyed by `event:cloudevents:<source>:<type>`.
//     Edges: PUBLISHES_TO (producer→synthetic), SUBSCRIBES_TO (consumer→synthetic),
//     CLOUDEVENT_FLOWS (emitter route → receiver route, same-file only).
//
// # False-positive guards
//
//   - Plain HTTP routes without CE headers are never tagged.
//   - EventBridge guard: file must mention "eventbridge", "put_events", "EventBridge",
//     or "PutEventsCommand" before scanning.
//   - EventGrid guard: file must mention "eventgrid", "EventGrid", "EventGridPublisherClient".
//   - CloudEvents guard: file must mention "CloudEvent", "ce-type", or "cloudevents".
//
// # Scope guard
//
// Append-only — this pass never modifies or removes existing entities or edges,
// so it cannot regress the bug-rate of the surrounding pipeline.
//
// Builds on the aws-lambda entity-ID prefix from #925 for EventBridge→Lambda links.
//
// Refs #927.
package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// eventBusEventKind is the SCOPE kind for synthetic managed event-bus events.
const eventBusEventKind = "SCOPE.EventBusEvent"

// eventBridgeTriggersEdge, eventGridTriggersEdge, cloudEventFlowsEdge are the
// new edge kinds introduced by #927.
const (
	eventBridgeTriggersEdge = "EVENTBRIDGE_TRIGGERS"
	eventGridTriggersEdge   = "EVENTGRID_TRIGGERS"
	cloudEventFlowsEdge     = "CLOUDEVENT_FLOWS"
)

// eventBusSynthesisSupportsLanguage reports whether applyEventBusEdges can
// emit synthetics for `lang`. The HCL/Terraform path is handled separately
// via a text-based regex scan (no language gate needed — TF files come as
// lang="hcl" or lang="terraform").
func eventBusSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "python", "javascript", "typescript", "go", "csharp", "hcl", "terraform":
		return true
	default:
		return false
	}
}

// applyEventBusEdges is the single entry-point pass that runs after
// applyRedisPubSubEdges. It dispatches to three sub-detectors and is
// append-only.
func applyEventBusEdges(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	if !eventBusSynthesisSupportsLanguage(lang) {
		return entities, relationships
	}

	src := string(content)

	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}

	emitEvent := func(id, busType, source, detailType string, props map[string]string) {
		if seenEnt[id] {
			return
		}
		seenEnt[id] = true
		merged := map[string]string{
			"bus_type":     busType,
			"source":       source,
			"detail_type":  detailType,
			"pattern_type": "event_bus_synthesis",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		entities = append(entities, types.EntityRecord{
			Name:               id,
			Kind:               eventBusEventKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitEdge := func(fromID, toID, kind string, props map[string]string) {
		if fromID == "" || toID == "" {
			return
		}
		key := kind + "|" + fromID + "|" + toID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       kind,
			Properties: props,
		})
	}

	applyEventBridgeEdges(lang, src, path, emitEvent, emitEdge)
	applyEventGridEdges(lang, src, path, emitEvent, emitEdge)
	applyCloudEventEdges(lang, src, path, emitEvent, emitEdge)

	return entities, relationships
}

// eventBridgeEventID returns the canonical synthetic ID for an EventBridge
// event routing key. Identical across repos for cross-repo linking.
func eventBridgeEventID(source, detailType string) string {
	return fmt.Sprintf("event:eventbridge:%s:%s", source, detailType)
}

// eventGridEventID returns the canonical synthetic ID for an EventGrid event.
func eventGridEventID(topic, eventType string) string {
	return fmt.Sprintf("event:eventgrid:%s:%s", topic, eventType)
}

// cloudEventID returns the canonical synthetic ID for a CloudEvents event.
func cloudEventID(ceSource, ceType string) string {
	return fmt.Sprintf("event:cloudevents:%s:%s", ceSource, ceType)
}

// ---------------------------------------------------------------------------
// AWS EventBridge
// ---------------------------------------------------------------------------

// pyEventBridgePutEventsRe matches Python boto3:
//
//	events.put_events(Entries=[{...}])
//	eb_client.put_events(Entries=[{'Source':'X','DetailType':'Y',...}])
var pyEventBridgePutEventsRe = regexp.MustCompile(`\.put_events\s*\(\s*Entries\s*=\s*\[`)

// pyEntrySourceRe extracts 'Source': 'value' from a Python dict literal.
var pyEntrySourceRe = regexp.MustCompile(`['"]Source['"]\s*:\s*['"]([^'"]+)['"]`)

// pyEntryDetailTypeRe extracts 'DetailType': 'value'.
var pyEntryDetailTypeRe = regexp.MustCompile(`['"]DetailType['"]\s*:\s*['"]([^'"]+)['"]`)

// nodeEBPutEventsCommandRe matches Node/TS AWS SDK v3:
//
//	new PutEventsCommand({ Entries: [{...}] })
var nodeEBPutEventsCommandRe = regexp.MustCompile(`new\s+PutEventsCommand\s*\(\s*\{[^)]*Entries\s*:`)

// nodeEBSourceRe extracts Source: 'value' (single/double/backtick quotes).
var nodeEBSourceRe = regexp.MustCompile(`Source\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeEBDetailTypeRe extracts DetailType: 'value'.
var nodeEBDetailTypeRe = regexp.MustCompile(`DetailType\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// goEBPutEventsRe matches Go AWS SDK v2:
//
//	ebtypes.PutEventsRequestEntry{Source: aws.String("X"), DetailType: aws.String("Y")}
var goEBPutEventsRe = regexp.MustCompile(`PutEventsRequestEntry\s*\{`)

// goEBSourceRe extracts Source: aws.String("X") or Source: &"X".
var goEBSourceRe = regexp.MustCompile(`Source\s*:\s*(?:aws\.String\s*\(\s*)?["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]`)

// goEBDetailTypeRe extracts DetailType: ...
var goEBDetailTypeRe = regexp.MustCompile(`DetailType\s*:\s*(?:aws\.String\s*\(\s*)?["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]`)

// hclEventRuleRe matches `resource "aws_cloudwatch_event_rule" "name"` in a
// line-oriented way to avoid brace-counting issues with nested jsonencode({}).
// Group 1 = resource label name. We scan the full file and extract blocks
// by finding the start of each resource block, then walking forward.
var hclEventRuleRe = regexp.MustCompile(`resource\s+"aws_cloudwatch_event_rule"\s+"(\w+)"`)

// hclEventPatternJSONStringRe matches the escaped-string form:
//
//	event_pattern = "{\"source\":[\"orders\"],\"detail-type\":[\"OrderPlaced\"]}"
var hclEventPatternJSONStringRe = regexp.MustCompile(`event_pattern\s*=\s*"((?:[^"\\]|\\.)*)"`)

// hclEventPatternJSONEncodeRe matches the jsonencode({...}) form by finding
// the source/detail-type lines inside the call.
// We rely on line-level extraction rather than balanced-brace extraction.
var hclEventPatternSourceLineRe = regexp.MustCompile(`(?m)^\s*source\s*=\s*\[\s*"([^"]+)"`)
var hclEventPatternDetailTypeLineRe = regexp.MustCompile(`(?m)["']detail-type["']\s*=\s*\[\s*"([^"]+)"`)

// hclEventPatternJSONEncodeBlockRe finds `event_pattern = jsonencode({` and captures until the
// matching `})`.
var hclEventPatternJSONEncodeBlockRe = regexp.MustCompile(`event_pattern\s*=\s*jsonencode\s*\(`)

// hclEventTargetRuleRe matches `resource "aws_cloudwatch_event_target" "name"` blocks
// that contain a target_id + rule + arn referencing a lambda.
var hclEventTargetRuleRe = regexp.MustCompile(`(?s)resource\s+"aws_cloudwatch_event_target"\s+"(\w+)"\s+\{([^}]*(?:\{[^}]*\}[^}]*)*)\}`)

// hclTargetRuleNameRe extracts `rule = aws_cloudwatch_event_rule.<name>.name` or
// `rule = "<name>"`.
var hclTargetRuleNameRe = regexp.MustCompile(`rule\s*=\s*(?:aws_cloudwatch_event_rule\.(\w+)\.name|"([^"]+)")`)

// hclTargetArnRe extracts `arn = aws_lambda_function.<name>.arn`.
var hclTargetArnRe = regexp.MustCompile(`arn\s*=\s*aws_lambda_function\.(\w+)\.arn`)

// hclTargetLambdaArnLiteralRe extracts `arn = "arn:aws:lambda:...:function:FnName"`.
var hclTargetLambdaArnLiteralRe = regexp.MustCompile(`arn\s*=\s*"arn:aws:lambda:[^"]*:function:([^"]+)"`)

// cdkEventPatternSourceRe matches CDK TypeScript/Python eventPattern: { source: ['X'], 'detail-type': ['Y'] }.
var cdkEventPatternSourceRe = regexp.MustCompile(`source\s*:\s*\[\s*['"]([^'"]+)['"]`)
var cdkEventPatternDetailTypeRe = regexp.MustCompile(`['"?]detail-type['"?]\s*:\s*\[\s*['"]([^'"]+)['"]`)

// cdkLambdaTargetRe matches addTarget(new targets.LambdaFunction(fn)) or addTarget(lambdaFn).
var cdkLambdaTargetRe = regexp.MustCompile(`addTarget\s*\(\s*(?:new\s+[A-Za-z.]+\s*\(\s*)?(\w+)\s*[,)]`)

// serverlessEventBridgeRe matches serverless.yml event[Bridge] stanza:
//
//	eventBridge:
//	  eventBus: ...
//	  pattern:
//	    source: [X]
//	    detail-type: [Y]
var serverlessYMLEBSourceRe = regexp.MustCompile(`source\s*:\s*\[\s*['"]?([^'"\]\s]+)['"]?`)
var serverlessYMLEBDetailTypeRe = regexp.MustCompile(`detail-type\s*:\s*\[\s*['"]?([^'"\]\s]+)['"]?`)

// applyEventBridgeEdges scans a single file for EventBridge patterns.
func applyEventBridgeEdges(
	lang, src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Fast-path guard — skip files with no EventBridge tokens.
	if !strings.Contains(src, "eventbridge") &&
		!strings.Contains(src, "EventBridge") &&
		!strings.Contains(src, "put_events") &&
		!strings.Contains(src, "PutEventsCommand") &&
		!strings.Contains(src, "PutEventsRequestEntry") &&
		!strings.Contains(src, "aws_cloudwatch_event_rule") &&
		!strings.Contains(src, "eventBridge") &&
		!strings.Contains(src, "EventBridgeClient") {
		return
	}

	switch lang {
	case "python":
		applyEventBridgePython(src, path, emitEvent, emitEdge)
	case "javascript", "typescript":
		applyEventBridgeNode(src, path, emitEvent, emitEdge)
	case "go":
		applyEventBridgeGo(src, path, emitEvent, emitEdge)
	case "hcl", "terraform":
		applyEventBridgeHCL(src, path, emitEvent, emitEdge)
	}

	// CDK TypeScript patterns are caught under javascript/typescript above.
	// serverless.yml is lang="yaml" — scan with a text-only path.
	if strings.Contains(path, "serverless") && strings.HasSuffix(path, ".yml") {
		applyEventBridgeServerlessYML(src, path, emitEvent, emitEdge)
	}
}

func applyEventBridgePython(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Producer: .put_events(Entries=[{...}])
	for _, m := range pyEventBridgePutEventsRe.FindAllStringIndex(src, -1) {
		// Extract the list argument — walk to closing bracket.
		start := m[1] - 1 // rewind to the opening [
		inner := extractBalancedBracket(src, start)

		source := ""
		if sm := pyEntrySourceRe.FindStringSubmatch(inner); sm != nil {
			source = sm[1]
		}
		detailType := ""
		if dm := pyEntryDetailTypeRe.FindStringSubmatch(inner); dm != nil {
			detailType = dm[1]
		}
		if source == "" || detailType == "" {
			continue
		}
		id := eventBridgeEventID(source, detailType)
		emitEvent(id, "eventbridge", source, detailType, nil)
		caller := findEnclosingPyFunctionName(src, m[0])
		emitEdge(
			fmt.Sprintf("SCOPE.Function:%s", caller),
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			"PUBLISHES_TO",
			map[string]string{"bus": "eventbridge", "source": source, "detail_type": detailType, "sdk": "boto3"},
		)
	}
}

func applyEventBridgeNode(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Producer: new PutEventsCommand({ Entries: [...] })
	for _, m := range nodeEBPutEventsCommandRe.FindAllStringIndex(src, -1) {
		// Look for Source / DetailType in a window after the match.
		end := m[1] + 800
		if end > len(src) {
			end = len(src)
		}
		window := src[m[0]:end]
		source := ""
		if sm := nodeEBSourceRe.FindStringSubmatch(window); sm != nil {
			source = sm[1]
		}
		detailType := ""
		if dm := nodeEBDetailTypeRe.FindStringSubmatch(window); dm != nil {
			detailType = dm[1]
		}
		if source == "" || detailType == "" {
			continue
		}
		id := eventBridgeEventID(source, detailType)
		emitEvent(id, "eventbridge", source, detailType, nil)
		caller := findEnclosingNodeFunctionName(src, m[0])
		emitEdge(
			fmt.Sprintf("SCOPE.Function:%s", caller),
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			"PUBLISHES_TO",
			map[string]string{"bus": "eventbridge", "source": source, "detail_type": detailType, "sdk": "aws-sdk-v3"},
		)
	}

	// CDK: eventPattern: { source: ['X'], 'detail-type': ['Y'] }
	if strings.Contains(src, "eventPattern") || strings.Contains(src, "EventPattern") {
		applyCDKEventPattern(src, path, emitEvent, emitEdge)
	}
}

func applyEventBridgeGo(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	for _, m := range goEBPutEventsRe.FindAllStringIndex(src, -1) {
		end := m[1] + 600
		if end > len(src) {
			end = len(src)
		}
		window := src[m[0]:end]
		source := ""
		if sm := goEBSourceRe.FindStringSubmatch(window); sm != nil {
			source = sm[1]
		}
		detailType := ""
		if dm := goEBDetailTypeRe.FindStringSubmatch(window); dm != nil {
			detailType = dm[1]
		}
		if source == "" || detailType == "" {
			continue
		}
		id := eventBridgeEventID(source, detailType)
		emitEvent(id, "eventbridge", source, detailType, nil)
		caller := findEnclosingGoFunctionName(src, m[0])
		emitEdge(
			fmt.Sprintf("SCOPE.Function:%s", caller),
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			"PUBLISHES_TO",
			map[string]string{"bus": "eventbridge", "source": source, "detail_type": detailType, "sdk": "aws-sdk-go-v2"},
		)
	}
}

// applyEventBridgeHCL detects aws_cloudwatch_event_rule + aws_cloudwatch_event_target
// resources in Terraform/HCL files.
func applyEventBridgeHCL(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Step 1: parse rules — collect ruleName → (source, detailType)
	type ruleInfo struct {
		source     string
		detailType string
	}
	rules := map[string]ruleInfo{}

	for _, m := range hclEventRuleRe.FindAllStringSubmatchIndex(src, -1) {
		ruleName := src[m[2]:m[3]]

		// Extract the body of this resource block by finding the opening brace
		// and walking forward with balanced-brace counting.
		blockStart := m[1]
		bracePos := strings.Index(src[blockStart:], "{")
		if bracePos < 0 {
			continue
		}
		bracePos += blockStart
		body := extractBalancedBraces(src, bracePos)

		source, detailType := extractEventBridgePattern(body)
		if source == "" || detailType == "" {
			continue
		}

		ri := ruleInfo{source: source, detailType: detailType}
		rules[ruleName] = ri
		id := eventBridgeEventID(source, detailType)
		emitEvent(id, "eventbridge", source, detailType, map[string]string{"iac": "terraform", "rule_name": ruleName})
		ruleEntityID := fmt.Sprintf("SCOPE.Component:aws_cloudwatch_event_rule.%s", ruleName)
		emitEdge(ruleEntityID, fmt.Sprintf("%s:%s", eventBusEventKind, id), "SUBSCRIBES_TO",
			map[string]string{"bus": "eventbridge", "source": source, "detail_type": detailType, "iac": "terraform"})
	}

	// Step 2: parse targets — link rule → lambda.
	for _, m := range hclEventTargetRuleRe.FindAllStringSubmatch(src, -1) {
		body := m[2]

		// Resolve rule name from `rule = aws_cloudwatch_event_rule.X.name` or literal.
		ruleName := ""
		if rm := hclTargetRuleNameRe.FindStringSubmatch(body); rm != nil {
			if rm[1] != "" {
				ruleName = rm[1]
			} else {
				ruleName = rm[2]
			}
		}

		// Find lambda function name from ARN reference or literal.
		lambdaName := ""
		if am := hclTargetArnRe.FindStringSubmatch(body); am != nil {
			lambdaName = am[1]
		} else if am := hclTargetLambdaArnLiteralRe.FindStringSubmatch(body); am != nil {
			lambdaName = am[1]
		}

		if lambdaName == "" {
			continue
		}

		// Emit EVENTBRIDGE_TRIGGERS: rule → lambda ServerlessFunction entity.
		lambdaID := lambdaFunctionID(lambdaName)
		var ruleEntityID string
		if ruleName != "" {
			ruleEntityID = fmt.Sprintf("SCOPE.Component:aws_cloudwatch_event_rule.%s", ruleName)
		} else {
			ruleEntityID = "SCOPE.Component:aws_cloudwatch_event_rule.unknown"
		}
		emitEdge(
			ruleEntityID,
			fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaID),
			eventBridgeTriggersEdge,
			map[string]string{"bus": "eventbridge", "target_type": "lambda", "lambda_name": lambdaName, "iac": "terraform"},
		)

		// Also emit SUBSCRIBES_TO from lambda to the event if we resolved the rule.
		if ruleName != "" {
			if ri, ok := rules[ruleName]; ok {
				id := eventBridgeEventID(ri.source, ri.detailType)
				emitEdge(
					fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaID),
					fmt.Sprintf("%s:%s", eventBusEventKind, id),
					"SUBSCRIBES_TO",
					map[string]string{"bus": "eventbridge", "via": "rule", "rule_name": ruleName, "iac": "terraform"},
				)
			}
		}
	}
}

// extractEventBridgePattern extracts source and detail-type from an HCL block body.
// Handles three formats:
//  1. event_pattern = "{\"source\":[\"X\"],\"detail-type\":[\"Y\"]}"  (escaped JSON string)
//  2. event_pattern = jsonencode({ source = ["X"], "detail-type" = ["Y"] })  (HCL native)
//  3. event_pattern = <<JSON ... JSON (heredoc — not yet handled, returns "","")
func extractEventBridgePattern(body string) (source, detailType string) {
	// Format 1: escaped JSON string.
	if m := hclEventPatternJSONStringRe.FindStringSubmatch(body); m != nil {
		// Unescape the string.
		unescaped := strings.ReplaceAll(m[1], `\"`, `"`)
		var pattern map[string]interface{}
		if err := json.Unmarshal([]byte(unescaped), &pattern); err == nil {
			source = extractFirstStringFromEventPatternField(pattern, "source")
			detailType = extractFirstStringFromEventPatternField(pattern, "detail-type")
			if source != "" && detailType != "" {
				return
			}
		}
		// Regex fallback.
		source, detailType = extractEventPatternFieldsRegex(unescaped)
		return
	}

	// Format 2: jsonencode({...}) — extract the block content and look for
	// line-level source / detail-type assignments.
	if m := hclEventPatternJSONEncodeBlockRe.FindStringIndex(body); m != nil {
		// Walk forward from the opening paren to find the balanced content.
		parenPos := strings.Index(body[m[0]:], "(")
		if parenPos >= 0 {
			parenPos += m[0]
			inner := extractBalancedParensEngine(body, parenPos)
			if sm := hclEventPatternSourceLineRe.FindStringSubmatch(inner); sm != nil {
				source = sm[1]
			}
			if dm := hclEventPatternDetailTypeLineRe.FindStringSubmatch(inner); dm != nil {
				detailType = dm[1]
			}
		}
	}
	return
}

// extractBalancedBraces extracts the content inside the first balanced `{`…`}`
// starting at position start (which must point at the opening `{`).
func extractBalancedBraces(src string, start int) string {
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start+1 : i]
			}
		}
	}
	return src[start:]
}

// applyCDKEventPattern handles CDK TypeScript patterns like:
//
//	eventPattern: { source: ['order.svc'], 'detail-type': ['OrderPlaced'] }
//	rule.addTarget(new targets.LambdaFunction(fn))
func applyCDKEventPattern(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Find eventPattern blocks and extract source + detail-type.
	sourceMatches := cdkEventPatternSourceRe.FindAllStringSubmatch(src, -1)
	dtMatches := cdkEventPatternDetailTypeRe.FindAllStringSubmatch(src, -1)

	for i, sm := range sourceMatches {
		source := sm[1]
		if i >= len(dtMatches) {
			break
		}
		detailType := dtMatches[i][1]
		if source == "" || detailType == "" {
			continue
		}
		id := eventBridgeEventID(source, detailType)
		emitEvent(id, "eventbridge", source, detailType, map[string]string{"iac": "cdk"})
		// Rule entity — use caller context.
		caller := findEnclosingNodeFunctionName(src, 0)
		ruleID := fmt.Sprintf("SCOPE.Function:%s", caller)
		emitEdge(ruleID, fmt.Sprintf("%s:%s", eventBusEventKind, id), "SUBSCRIBES_TO",
			map[string]string{"bus": "eventbridge", "source": source, "detail_type": detailType, "iac": "cdk"})

		// addTarget — link to lambda.
		if tm := cdkLambdaTargetRe.FindStringSubmatch(src); tm != nil {
			lambdaVarName := tm[1]
			lambdaID := lambdaFunctionID(lambdaVarName)
			emitEdge(ruleID,
				fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaID),
				eventBridgeTriggersEdge,
				map[string]string{"bus": "eventbridge", "target_var": lambdaVarName, "iac": "cdk"})
		}
	}
}

// applyEventBridgeServerlessYML handles serverless.yml eventBridge trigger stanzas.
func applyEventBridgeServerlessYML(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	if !strings.Contains(src, "eventBridge") {
		return
	}
	sourceMatches := serverlessYMLEBSourceRe.FindAllStringSubmatch(src, -1)
	dtMatches := serverlessYMLEBDetailTypeRe.FindAllStringSubmatch(src, -1)
	for i, sm := range sourceMatches {
		source := sm[1]
		if i >= len(dtMatches) {
			break
		}
		detailType := dtMatches[i][1]
		if source == "" || detailType == "" {
			continue
		}
		id := eventBridgeEventID(source, detailType)
		emitEvent(id, "eventbridge", source, detailType, map[string]string{"iac": "serverless.yml"})
		emitEdge(
			fmt.Sprintf("SCOPE.Config:%s", path),
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			"SUBSCRIBES_TO",
			map[string]string{"bus": "eventbridge", "iac": "serverless.yml"},
		)
	}
}

// extractEventPatternFieldsRegex falls back to regex-based extraction when JSON parsing fails.
func extractEventPatternFieldsRegex(pattern string) (source, detailType string) {
	if m := regexp.MustCompile(`"source"\s*:\s*\[\s*"([^"]+)"`).FindStringSubmatch(pattern); m != nil {
		source = m[1]
	}
	if m := regexp.MustCompile(`"detail-type"\s*:\s*\[\s*"([^"]+)"`).FindStringSubmatch(pattern); m != nil {
		detailType = m[1]
	}
	return
}

// extractFirstStringFromEventPatternField returns the first string element of a
// JSON array field in an EventBridge event_pattern object.
func extractFirstStringFromEventPatternField(pattern map[string]interface{}, field string) string {
	v, ok := pattern[field]
	if !ok {
		return ""
	}
	arr, ok := v.([]interface{})
	if !ok || len(arr) == 0 {
		return ""
	}
	s, _ := arr[0].(string)
	return s
}

// extractBalancedParensEngine extracts the content inside the first balanced `(`…`)`
// starting at position start (which should point at the opening `(`).
// Named with Engine suffix to avoid collision with the python package function.
func extractBalancedParensEngine(src string, start int) string {
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[start+1 : i]
			}
		}
	}
	return ""
}

// extractBalancedBracket extracts the content inside the first balanced `[`…`]`
// starting at position start.
func extractBalancedBracket(src string, start int) string {
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return src[start+1 : i]
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Azure EventGrid
// ---------------------------------------------------------------------------

// pyEventGridSendRe matches Python azure-eventgrid send call:
//
//	client.send([EventGridEvent(subject=..., event_type='X', ...)], ...)
//	EventGridPublisherClient(...).send(events)
var pyEventGridSendRe = regexp.MustCompile(`EventGridPublisherClient|\.send\s*\(\s*\[`)

// pyEventGridEventTypeRe extracts event_type='X' from a Python call.
var pyEventGridEventTypeRe = regexp.MustCompile(`event_type\s*=\s*['"]([^'"]+)['"]`)

// pyEventGridSubjectRe extracts subject='X'.
var pyEventGridSubjectRe = regexp.MustCompile(`subject\s*=\s*['"]([^'"]+)['"]`)

// pyEventGridTriggerRe matches @app.event_grid_trigger(name='fn') + def name(
var pyEventGridTriggerRe = regexp.MustCompile(`(?m)@\w+\.event_grid_trigger\s*\([^)]*\)\s*\n(?:\s*#[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(`)

// pyEventGridTriggerAttrRe matches @app.route(..., trigger=EventGridTrigger())
var pyEventGridTriggerAttrRe = regexp.MustCompile(`EventGridTrigger\s*\(`)

// nodeEGSendRe matches Node @azure/eventgrid:
//
//	client.send(events) where client is EventGridPublisherClient / EventGridSenderClient
var nodeEGSendRe = regexp.MustCompile(`EventGridPublisherClient|EventGridSenderClient|sendEvents\s*\(`)

// nodeEGEventTypeRe extracts eventType: 'X' from a Node object literal
// (single, double, or backtick quotes).
var nodeEGEventTypeRe = regexp.MustCompile(`eventType\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeEGSubjectRe extracts subject: 'X' (single, double, or backtick quotes).
var nodeEGSubjectRe = regexp.MustCompile(`subject\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeEGSendCallRe matches the sendEvents / send call site that contains
// the event object array — used to anchor the event data extraction window.
var nodeEGSendCallRe = regexp.MustCompile(`(?:sendEvents|\.send)\s*\(\s*\[`)

// csharpEGSendRe matches C# EventGridPublisherClient.SendEventsAsync.
var csharpEGSendRe = regexp.MustCompile(`SendEventsAsync\s*\(|EventGridPublisherClient`)

// csharpEGEventTypeRe extracts EventType = "X" from a C# object initializer.
var csharpEGEventTypeRe = regexp.MustCompile(`EventType\s*=\s*"([^"\n\r]+)"`)

// csharpEGSubjectRe extracts Subject = "X".
var csharpEGSubjectRe = regexp.MustCompile(`Subject\s*=\s*"([^"\n\r]+)"`)

// csharpEGTriggerRe matches [EventGridTrigger] C# binding attribute.
var csharpEGTriggerRe = regexp.MustCompile(`\[EventGridTrigger\]|\bEventGridEvent\b`)

func applyEventGridEdges(
	lang, src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Fast-path guard.
	if !strings.Contains(src, "eventgrid") &&
		!strings.Contains(src, "EventGrid") &&
		!strings.Contains(src, "event_grid") {
		return
	}

	switch lang {
	case "python":
		applyEventGridPython(src, path, emitEvent, emitEdge)
	case "javascript", "typescript":
		applyEventGridNode(src, path, emitEvent, emitEdge)
	case "csharp":
		applyEventGridCSharp(src, path, emitEvent, emitEdge)
	}
}

func applyEventGridPython(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Producer: EventGridPublisherClient(...).send(...)
	if pyEventGridSendRe.MatchString(src) {
		// Extract event type + subject from call site window.
		for _, m := range regexp.MustCompile(`EventGridEvent\s*\(`).FindAllStringIndex(src, -1) {
			end := m[1] + 400
			if end > len(src) {
				end = len(src)
			}
			window := src[m[0]:end]
			eventType := ""
			if em := pyEventGridEventTypeRe.FindStringSubmatch(window); em != nil {
				eventType = em[1]
			}
			subject := ""
			if sm := pyEventGridSubjectRe.FindStringSubmatch(window); sm != nil {
				subject = sm[1]
			}
			if eventType == "" {
				continue
			}
			id := eventGridEventID(subject, eventType)
			emitEvent(id, "eventgrid", subject, eventType, nil)
			caller := findEnclosingPyFunctionName(src, m[0])
			emitEdge(
				fmt.Sprintf("SCOPE.Function:%s", caller),
				fmt.Sprintf("%s:%s", eventBusEventKind, id),
				"PUBLISHES_TO",
				map[string]string{"bus": "eventgrid", "event_type": eventType, "subject": subject, "sdk": "azure-eventgrid-python"},
			)
		}
	}

	// Consumer: @app.event_grid_trigger(name='X') + def fn(
	for _, m := range pyEventGridTriggerRe.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[2]:m[3]]
		id := eventGridEventID("*", "*") // wildcard — consumer with no explicit topic
		emitEvent(id, "eventgrid", "*", "*", map[string]string{"consumer": fnName})
		emitEdge(
			fmt.Sprintf("SCOPE.Function:%s", fnName),
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			"SUBSCRIBES_TO",
			map[string]string{"bus": "eventgrid", "trigger": "event_grid_trigger", "sdk": "azure-functions-python"},
		)
		// Also emit EVENTGRID_TRIGGERS → azure-function entity (from #925).
		azID := azureFunctionID(fnName)
		emitEdge(
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			fmt.Sprintf("%s:%s", serverlessFunctionKind, azID),
			eventGridTriggersEdge,
			map[string]string{"bus": "eventgrid", "function_name": fnName},
		)
	}
}

func applyEventGridNode(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	if !nodeEGSendRe.MatchString(src) {
		return
	}
	// Producer: anchor on the send call site that contains the event array,
	// then extract eventType + subject from the call window.
	for _, m := range nodeEGSendCallRe.FindAllStringIndex(src, -1) {
		// Extract the array content.
		bracketPos := m[1] - 1 // rewind to `[`
		inner := extractBalancedBracket(src, bracketPos)
		eventType := ""
		if em := nodeEGEventTypeRe.FindStringSubmatch(inner); em != nil {
			eventType = em[1]
		}
		subject := ""
		if sm := nodeEGSubjectRe.FindStringSubmatch(inner); sm != nil {
			subject = sm[1]
		}
		if eventType == "" {
			continue
		}
		id := eventGridEventID(subject, eventType)
		emitEvent(id, "eventgrid", subject, eventType, nil)
		caller := findEnclosingNodeFunctionName(src, m[0])
		emitEdge(
			fmt.Sprintf("SCOPE.Function:%s", caller),
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			"PUBLISHES_TO",
			map[string]string{"bus": "eventgrid", "event_type": eventType, "subject": subject, "sdk": "azure-eventgrid-js"},
		)
	}
}

func applyEventGridCSharp(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Producer: EventGridPublisherClient.SendEventsAsync
	if csharpEGSendRe.MatchString(src) {
		for _, m := range regexp.MustCompile(`EventGridEvent\s*\(`).FindAllStringIndex(src, -1) {
			end := m[1] + 400
			if end > len(src) {
				end = len(src)
			}
			window := src[m[0]:end]
			eventType := ""
			if em := csharpEGEventTypeRe.FindStringSubmatch(window); em != nil {
				eventType = em[1]
			}
			subject := ""
			if sm := csharpEGSubjectRe.FindStringSubmatch(window); sm != nil {
				subject = sm[1]
			}
			if eventType == "" {
				continue
			}
			id := eventGridEventID(subject, eventType)
			emitEvent(id, "eventgrid", subject, eventType, nil)
			caller := findEnclosingCSharpMethod(src, m[0])
			emitEdge(
				fmt.Sprintf("SCOPE.Function:%s", caller),
				fmt.Sprintf("%s:%s", eventBusEventKind, id),
				"PUBLISHES_TO",
				map[string]string{"bus": "eventgrid", "event_type": eventType, "subject": subject, "sdk": "azure-eventgrid-dotnet"},
			)
		}
	}

	// Consumer: [EventGridTrigger] — the attribute appears inside a method
	// parameter list, so we search backward for the enclosing method declaration.
	if csharpEGTriggerRe.MatchString(src) {
		for _, m := range csharpEGTriggerRe.FindAllStringIndex(src, -1) {
			// Search backward from trigger match to find the method.
			methodName := findEnclosingCSharpMethod(src, m[0])
			if methodName == "" {
				// Fall back to forward search.
				methodName = findFollowingCSharpMethod(src, m[1])
			}
			if methodName == "" {
				continue
			}
			id := eventGridEventID("*", "*")
			emitEvent(id, "eventgrid", "*", "*", map[string]string{"consumer": methodName})
			emitEdge(
				fmt.Sprintf("SCOPE.Function:%s", methodName),
				fmt.Sprintf("%s:%s", eventBusEventKind, id),
				"SUBSCRIBES_TO",
				map[string]string{"bus": "eventgrid", "trigger": "EventGridTrigger", "sdk": "azure-functions-dotnet"},
			)
			azID := azureFunctionID(methodName)
			emitEdge(
				fmt.Sprintf("%s:%s", eventBusEventKind, id),
				fmt.Sprintf("%s:%s", serverlessFunctionKind, azID),
				eventGridTriggersEdge,
				map[string]string{"bus": "eventgrid", "function_name": methodName},
			)
		}
	}
}

// ---------------------------------------------------------------------------
// CNCF CloudEvents
// ---------------------------------------------------------------------------

// ceHeaderRe matches HTTP header reads for CloudEvents mandatory attributes:
//
//	request.headers.get('ce-type'), req.header('ce-source'), etc.
var ceHeaderRe = regexp.MustCompile(`(?i)["']ce-(?:type|source|id|specversion)["']`)

// ceTypeHeaderRe extracts the literal ce-type value when known statically.
var ceTypeHeaderRe = regexp.MustCompile(`(?i)ce-type["']\s*[):,=]\s*["']([^"'\n\r]+)["']`)

// ceSourceHeaderRe extracts the literal ce-source value.
var ceSourceHeaderRe = regexp.MustCompile(`(?i)ce-source["']\s*[):,=]\s*["']([^"'\n\r]+)["']`)

// pyCloudEventBuilderRe matches Python cloudevents SDK:
//
//	CloudEvent({'type': 'X', 'source': '/Y', ...})
var pyCloudEventBuilderRe = regexp.MustCompile(`CloudEvent\s*\(\s*\{`)

// pyCloudEventTypeRe extracts 'type': 'X' from a Python CloudEvent constructor.
var pyCloudEventTypeRe = regexp.MustCompile(`['"]type['"]\s*:\s*['"]([^'"]+)['"]`)

// pyCloudEventSourceRe extracts 'source': '/X'.
var pyCloudEventSourceRe = regexp.MustCompile(`['"]source['"]\s*:\s*['"]([^'"]+)['"]`)

// nodeCloudEventBuilderRe matches Node/TS:
//
//	new CloudEvent({ type: 'X', source: '/Y' })
var nodeCloudEventBuilderRe = regexp.MustCompile(`new\s+CloudEvent\s*\(\s*\{`)

// nodeCloudEventTypeRe extracts type: 'X' from a JS CloudEvent constructor
// (single, double, or backtick quotes).
var nodeCloudEventTypeRe = regexp.MustCompile(`type\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeCloudEventSourceRe extracts source: '/X' (single, double, or backtick quotes).
var nodeCloudEventSourceRe = regexp.MustCompile(`source\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// goCloudEventNewEventRe matches Go SDK:
//
//	cloudevents.NewEvent()
var goCloudEventNewEventRe = regexp.MustCompile(`cloudevents\.NewEvent\s*\(\s*\)`)

// goCloudEventSetTypeRe extracts event.SetType("X").
var goCloudEventSetTypeRe = regexp.MustCompile(`\.SetType\s*\(\s*["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]`)

// goCloudEventSetSourceRe extracts event.SetSource("X").
var goCloudEventSetSourceRe = regexp.MustCompile(`\.SetSource\s*\(\s*["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]`)

// goCloudEventClientRe matches Go SDK consumer: cloudevents.NewClientHTTP()
var goCloudEventClientRe = regexp.MustCompile(`cloudevents\.NewClientHTTP\s*\(\s*\)`)

func applyCloudEventEdges(
	lang, src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Fast-path guard — skip files without CloudEvent tokens.
	if !strings.Contains(src, "CloudEvent") &&
		!strings.Contains(src, "cloudevents") &&
		!strings.Contains(src, "ce-type") &&
		!strings.Contains(src, "ce-source") {
		return
	}

	switch lang {
	case "python":
		applyCloudEventPython(src, path, emitEvent, emitEdge)
	case "javascript", "typescript":
		applyCloudEventNode(src, path, emitEvent, emitEdge)
	case "go":
		applyCloudEventGo(src, path, emitEvent, emitEdge)
	}

	// HTTP-layer guard: any language — routes that explicitly read ce-type header.
	// Only fire when the file does NOT already have a CloudEvent SDK constructor
	// (avoids double-counting) and does reference the header.
	hasSDKConstructor := pyCloudEventBuilderRe.MatchString(src) ||
		nodeCloudEventBuilderRe.MatchString(src) ||
		goCloudEventNewEventRe.MatchString(src)
	if !hasSDKConstructor && ceHeaderRe.MatchString(src) {
		applyCloudEventHTTPHeaders(lang, src, path, emitEvent, emitEdge)
	}
}

func applyCloudEventPython(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	for _, m := range pyCloudEventBuilderRe.FindAllStringIndex(src, -1) {
		end := m[1] + 400
		if end > len(src) {
			end = len(src)
		}
		window := src[m[0]:end]
		ceType := ""
		if tm := pyCloudEventTypeRe.FindStringSubmatch(window); tm != nil {
			ceType = tm[1]
		}
		ceSource := ""
		if sm := pyCloudEventSourceRe.FindStringSubmatch(window); sm != nil {
			ceSource = sm[1]
		}
		if ceType == "" {
			continue
		}
		id := cloudEventID(ceSource, ceType)
		emitEvent(id, "cloudevents", ceSource, ceType, nil)
		caller := findEnclosingPyFunctionName(src, m[0])
		emitEdge(
			fmt.Sprintf("SCOPE.Function:%s", caller),
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			"PUBLISHES_TO",
			map[string]string{"bus": "cloudevents", "ce_type": ceType, "ce_source": ceSource, "sdk": "cloudevents-python"},
		)
	}
}

func applyCloudEventNode(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	for _, m := range nodeCloudEventBuilderRe.FindAllStringIndex(src, -1) {
		end := m[1] + 400
		if end > len(src) {
			end = len(src)
		}
		window := src[m[0]:end]
		ceType := ""
		if tm := nodeCloudEventTypeRe.FindStringSubmatch(window); tm != nil {
			ceType = tm[1]
		}
		ceSource := ""
		if sm := nodeCloudEventSourceRe.FindStringSubmatch(window); sm != nil {
			ceSource = sm[1]
		}
		if ceType == "" {
			continue
		}
		id := cloudEventID(ceSource, ceType)
		emitEvent(id, "cloudevents", ceSource, ceType, nil)
		caller := findEnclosingNodeFunctionName(src, m[0])
		emitEdge(
			fmt.Sprintf("SCOPE.Function:%s", caller),
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			"PUBLISHES_TO",
			map[string]string{"bus": "cloudevents", "ce_type": ceType, "ce_source": ceSource, "sdk": "cloudevents-js"},
		)
	}
}

func applyCloudEventGo(
	src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Producer: cloudevents.NewEvent() + SetType + SetSource
	for _, m := range goCloudEventNewEventRe.FindAllStringIndex(src, -1) {
		end := m[1] + 400
		if end > len(src) {
			end = len(src)
		}
		window := src[m[0]:end]
		ceType := ""
		if tm := goCloudEventSetTypeRe.FindStringSubmatch(window); tm != nil {
			ceType = tm[1]
		}
		ceSource := ""
		if sm := goCloudEventSetSourceRe.FindStringSubmatch(window); sm != nil {
			ceSource = sm[1]
		}
		if ceType == "" {
			continue
		}
		id := cloudEventID(ceSource, ceType)
		emitEvent(id, "cloudevents", ceSource, ceType, nil)
		caller := findEnclosingGoFunctionName(src, m[0])
		emitEdge(
			fmt.Sprintf("SCOPE.Function:%s", caller),
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			"PUBLISHES_TO",
			map[string]string{"bus": "cloudevents", "ce_type": ceType, "ce_source": ceSource, "sdk": "cloudevents-go"},
		)
	}

	// Consumer: cloudevents.NewClientHTTP() — emit wildcard consumer entity.
	if goCloudEventClientRe.MatchString(src) {
		id := cloudEventID("*", "*")
		emitEvent(id, "cloudevents", "*", "*", map[string]string{"consumer": "cloudevents-http"})
		caller := findEnclosingGoFunctionName(src, 0)
		emitEdge(
			fmt.Sprintf("SCOPE.Function:%s", caller),
			fmt.Sprintf("%s:%s", eventBusEventKind, id),
			"SUBSCRIBES_TO",
			map[string]string{"bus": "cloudevents", "sdk": "cloudevents-go"},
		)
	}
}

// applyCloudEventHTTPHeaders handles the spec-level CloudEvents detection path:
// any HTTP handler that reads ce-type / ce-source headers without a full SDK.
func applyCloudEventHTTPHeaders(
	lang, src, path string,
	emitEvent func(id, busType, source, detailType string, props map[string]string),
	emitEdge func(fromID, toID, kind string, props map[string]string),
) {
	// Try to extract the literal ce-type and ce-source from the same handler.
	ceType := ""
	if m := ceTypeHeaderRe.FindStringSubmatch(src); m != nil {
		ceType = m[1]
	}
	ceSource := ""
	if m := ceSourceHeaderRe.FindStringSubmatch(src); m != nil {
		ceSource = m[1]
	}

	// Only emit a CloudEvent entity if at least ce-type is readable statically.
	if ceType == "" && !strings.Contains(src, "ce-specversion") {
		return
	}
	if ceType == "" {
		ceType = "*"
	}

	id := cloudEventID(ceSource, ceType)
	emitEvent(id, "cloudevents", ceSource, ceType, map[string]string{"detection": "http-header"})

	// Emit a CLOUDEVENT_FLOWS edge from the handler entity to the event.
	switch lang {
	case "python":
		// Find function that reads the header.
		for _, m := range ceHeaderRe.FindAllStringIndex(src, -1) {
			caller := findEnclosingPyFunctionName(src, m[0])
			emitEdge(
				fmt.Sprintf("SCOPE.Function:%s", caller),
				fmt.Sprintf("%s:%s", eventBusEventKind, id),
				cloudEventFlowsEdge,
				map[string]string{"bus": "cloudevents", "detection": "http-header", "ce_type": ceType},
			)
			break // one edge per handler
		}
	case "javascript", "typescript":
		for _, m := range ceHeaderRe.FindAllStringIndex(src, -1) {
			caller := findEnclosingNodeFunctionName(src, m[0])
			emitEdge(
				fmt.Sprintf("SCOPE.Function:%s", caller),
				fmt.Sprintf("%s:%s", eventBusEventKind, id),
				cloudEventFlowsEdge,
				map[string]string{"bus": "cloudevents", "detection": "http-header", "ce_type": ceType},
			)
			break
		}
	case "go":
		for _, m := range ceHeaderRe.FindAllStringIndex(src, -1) {
			caller := findEnclosingGoFunctionName(src, m[0])
			emitEdge(
				fmt.Sprintf("SCOPE.Function:%s", caller),
				fmt.Sprintf("%s:%s", eventBusEventKind, id),
				cloudEventFlowsEdge,
				map[string]string{"bus": "cloudevents", "detection": "http-header", "ce_type": ceType},
			)
			break
		}
	}
}
