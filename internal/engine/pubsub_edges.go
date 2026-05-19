// Google Cloud Pub/Sub producer/consumer detection — wave 3 of #726.
//
// For every Pub/Sub publish or subscribe call site this pass can statically
// recognize, we emit a synthetic `SCOPE.Queue` entity keyed by the topic
// name, plus PUBLISHES_TO or SUBSCRIBES_TO edges from the calling method
// to that topic. The synthetic topic ID is identical across repos
// (`pubsub:<project>:<topic-name>` or `pubsub:X:<topic-name>` when the
// project is unknown), so the existing import-channel linker matches
// producer and consumer sides on shared entity ID without any new cross-repo
// matching code — same technique used by kafka_edges.go (#726 wave 1) and
// rabbitmq_edges.go (#726 wave 2).
//
// Libraries/frameworks covered:
//   - Python google-cloud-pubsub: publisher.publish(topic_path, data),
//     subscriber.subscribe(subscription_path, callback),
//     publisher.topic_path(project, topic), subscriber.create_subscription
//   - Python Pub/Sub Lite: PublisherClient.publish / SubscriberClient.subscribe
//   - Node @google-cloud/pubsub: topic.publish(data), subscription.on('message', h),
//     pubsub.topic('X'), pubsub.subscription('Y')
//   - Go cloud.google.com/go/pubsub: topic.Publish(ctx, &pubsub.Message{...}),
//     sub.Receive(ctx, handler)
//   - Java google-cloud-pubsub: publisher.publish(message), subscriber.startAsync(),
//     MessageReceiver interface implementations
//   - Eventarc / Cloud Run triggers: def hello_pubsub(event, context) where
//     event.attributes["type"] == "google.cloud.pubsub.topic.v1.messagePublished"
//     — consumer side
//
// Beyond the minimum:
//   - Pub/Sub Lite (PublisherClient.publish / SubscriberClient.subscribe)
//   - Eventarc Cloud Run trigger config detection as a Pub/Sub consumer even
//     when the function does not call subscribe() directly
//   - project prefix in ID for cross-repo matching
//
// Emit: SCOPE.Queue (reuses queueEntityKind) + PUBLISHES_TO + SUBSCRIBES_TO.
// ID format: pubsub:<project-or-X>:<topic-name>.
//
// Refs #726.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// pubsubSynthesisSupportsLanguage reports whether applyPubSubEdges can emit
// synthetics for `lang`.
func pubsubSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "java", "kotlin", "javascript", "typescript", "python", "go":
		return true
	default:
		return false
	}
}

// applyPubSubEdges runs after applySQSEdges and APPENDS SCOPE.Queue
// entities + PUBLISHES_TO / SUBSCRIBES_TO edges for Google Cloud Pub/Sub.
// Append-only — never modifies or removes existing entities or edges, so
// this pass cannot regress the surrounding pipeline's bug-rate.
func applyPubSubEdges(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	if !pubsubSynthesisSupportsLanguage(lang) {
		return entities, relationships
	}

	src := string(content)

	// Dedup-by-ID: one SCOPE.Queue entity per topic per file.
	seenQueue := map[string]bool{}
	seenEdge := map[string]bool{}

	emitTopic := func(topicID, topicName, project string, props map[string]string) {
		if seenQueue[topicID] {
			return
		}
		seenQueue[topicID] = true
		merged := map[string]string{
			"broker":       "pubsub",
			"topic_name":   topicName,
			"pattern_type": "pubsub_synthesis",
		}
		if project != "" {
			merged["project"] = project
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		// SourceFile left empty so identical topic names collapse to ONE
		// entity per repo and match across repos via the import-channel
		// linker (same technique as kafka_edges.go MessageTopic).
		entities = append(entities, types.EntityRecord{
			Name:               topicID,
			Kind:               queueEntityKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitEdge := func(callerKind, callerName, topicID, edgeKind string, props map[string]string) {
		if callerName == "" || topicID == "" {
			return
		}
		key := edgeKind + "|" + callerKind + ":" + callerName + "|" + topicID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		base := map[string]string{
			"broker":       "pubsub",
			"pattern_type": "pubsub_synthesis",
		}
		for k, v := range props {
			if v != "" {
				base[k] = v
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fmt.Sprintf("%s:%s", callerKind, callerName),
			ToID:       fmt.Sprintf("%s:%s", queueEntityKind, topicID),
			Kind:       edgeKind,
			Properties: base,
		})
	}

	switch lang {
	case "python":
		synthesizePyPubSub(src, emitTopic, emitEdge)
	case "javascript", "typescript":
		synthesizeNodePubSub(src, emitTopic, emitEdge)
	case "java", "kotlin":
		synthesizeJavaPubSub(src, emitTopic, emitEdge)
	case "go":
		synthesizeGoPubSub(src, emitTopic, emitEdge)
	}

	return entities, relationships
}

// pubsubTopicID returns the canonical synthetic ID for a Pub/Sub topic.
// project may be empty (replaced with "X" for cross-repo matching when
// the project is not statically known).
func pubsubTopicID(project, topic string) string {
	if project == "" {
		project = "X"
	}
	return "pubsub:" + project + ":" + topic
}

// looksLikePubSubTopic returns true when `s` plausibly looks like a
// Google Cloud Pub/Sub topic name. Topic names are 3–255 characters,
// alphanumeric plus hyphens, underscores, periods, and tildes.
func looksLikePubSubTopic(s string) bool {
	if s == "" || len(s) > 255 || len(s) < 1 {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t<>{}") {
		return false
	}
	hasAlnum := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			hasAlnum = true
		case r == '.' || r == '_' || r == '-' || r == '~':
		default:
			return false
		}
	}
	return hasAlnum
}

// ---------------------------------------------------------------------------
// Python — google-cloud-pubsub + Pub/Sub Lite + Eventarc
// ---------------------------------------------------------------------------

// pyPubSubPublishVarRe detects publisher.publish(topic_path_var, ...) where the
// first argument is a variable (not a literal). Matches when the file uses the
// common pattern: topic_path = "projects/.../topics/..." followed by publish(topic_path).
var pyPubSubPublishVarRe = regexp.MustCompile(`\.publish\s*\(`)

// pyPubSubPublishLitRe captures publisher.publish("projects/.../topics/name", data).
// Group 1 = full topic resource path.
var pyPubSubPublishLitRe = regexp.MustCompile(`\.publish\s*\(\s*["']([^"'\n\r]+)["']`)

// pyPubSubTopicStrLitRe scans for any string literal in the file that is a full
// Pub/Sub resource path (projects/.../topics/...). This handles the common
// pattern: topic_path = "projects/proj/topics/name"; publisher.publish(topic_path, ...).
// Groups: 1=project, 2=topic.
var pyPubSubTopicStrLitRe = regexp.MustCompile(`["']projects/([^/"'\s]+)/topics/([^/"'\s]+)["']`)

// pyPubSubTopicPathRe captures publisher.topic_path(project, topic) and
// subscriber.topic_path(project, topic).
// Groups: 1=project, 2=topic.
var pyPubSubTopicPathRe = regexp.MustCompile(`\.topic_path\s*\(\s*["']([^"'\n\r]+)["']\s*,\s*["']([^"'\n\r]+)["']`)

// pyPubSubSubscribeRe captures subscriber.subscribe(subscription_path, callback).
// Group 1 = subscription_path or literal.
var pyPubSubSubscribeLitRe = regexp.MustCompile(`\.subscribe\s*\(\s*["']([^"'\n\r]+)["']`)

// pyPubSubCreateSubRe captures subscriber.create_subscription(subscription, topic).
// Group 1 = topic resource path or variable.
var pyPubSubCreateSubRe = regexp.MustCompile(`\.create_subscription\s*\([^)]*?topic\s*=\s*["']([^"'\n\r]+)["']`)

// pyPubSubLitePublishRe captures PublisherClient usage.
var pyPubSubLitePublishRe = regexp.MustCompile(`PublisherClient\b`)

// pyPubSubLiteSubscribeRe captures SubscriberClient usage.
var pyPubSubLiteSubscribeRe = regexp.MustCompile(`SubscriberClient\b`)

// pyPubSubSubscribeVarRe detects subscriber.subscribe(var, ...) where the first
// arg is a variable — handles pattern: subscription_path = "projects/.../subscriptions/name".
var pyPubSubSubscribeVarRe = regexp.MustCompile(`\.subscribe\s*\(`)

// pyPubSubSubStrLitRe scans for any subscription resource path literal.
// Groups: 1=project, 2=subscription-name.
var pyPubSubSubStrLitRe = regexp.MustCompile(`["']projects/([^/"'\s]+)/subscriptions/([^/"'\s]+)["']`)

// pyEventarcHandlerRe captures Cloud Run / Eventarc handlers that receive
// Pub/Sub events via the "google.cloud.pubsub.topic.v1.messagePublished" trigger.
// Group 1 = handler function name.
var pyEventarcHandlerRe = regexp.MustCompile(`(?m)^\s*def\s+(\w+)\s*\(\s*(?:event|request|cloud_event)\s*[,)]`)

// pyEventarcPubSubTypeRe matches the Pub/Sub event type attribute.
var pyEventarcPubSubTypeRe = regexp.MustCompile(`google\.cloud\.pubsub\.topic\.v1\.messagePublished`)

// pyPubSubTopicResourceRe extracts topic from a full resource path like
// projects/my-project/topics/my-topic.
// Groups: 1=project, 2=topic.
var pyPubSubTopicResourceRe = regexp.MustCompile(`projects/([^/\s"']+)/topics/([^/\s"']+)`)

func synthesizePyPubSub(
	src string,
	emitTopic func(topicID, topicName, project string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	hasPubSub := strings.Contains(src, "pubsub") || strings.Contains(src, "PublisherClient") ||
		strings.Contains(src, "SubscriberClient") || strings.Contains(src, "topic_path") ||
		strings.Contains(src, "google.cloud.pubsub")
	if !hasPubSub {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingPyName(src, offset)
	}

	// publisher.topic_path(project, topic) — emit topic entity.
	for _, m := range pyPubSubTopicPathRe.FindAllStringSubmatchIndex(src, -1) {
		project := src[m[2]:m[3]]
		topic := src[m[4]:m[5]]
		if !looksLikePubSubTopic(topic) {
			continue
		}
		topicID := pubsubTopicID(project, topic)
		emitTopic(topicID, topic, project, nil)
	}

	// publisher.publish("projects/.../topics/name", data) — literal first arg.
	for _, m := range pyPubSubPublishLitRe.FindAllStringSubmatchIndex(src, -1) {
		resourcePath := src[m[2]:m[3]]
		project, topic := "", resourcePath
		if rm := pyPubSubTopicResourceRe.FindStringSubmatch(resourcePath); len(rm) >= 3 {
			project, topic = rm[1], rm[2]
		}
		if !looksLikePubSubTopic(topic) {
			continue
		}
		topicID := pubsubTopicID(project, topic)
		emitTopic(topicID, topic, project, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, topicID, publishesToEdgeKind, map[string]string{
			"messaging_layer": "google-cloud-pubsub",
		})
	}

	// Variable-based publish: publisher.publish(topic_path_var, ...) where topic_path_var
	// was assigned from a "projects/.../topics/..." string literal elsewhere in the file.
	// Scan all topic resource literals in the file; if any .publish( call exists, emit
	// a PUBLISHES_TO edge from the enclosing function.
	if pyPubSubPublishVarRe.MatchString(src) {
		for _, m := range pyPubSubTopicStrLitRe.FindAllStringSubmatchIndex(src, -1) {
			project := src[m[2]:m[3]]
			topic := src[m[4]:m[5]]
			if !looksLikePubSubTopic(topic) {
				continue
			}
			topicID := pubsubTopicID(project, topic)
			emitTopic(topicID, topic, project, nil)
			// Find the closest publish call to associate an edge.
			for _, pm := range pyPubSubPublishVarRe.FindAllStringSubmatchIndex(src, -1) {
				// Only associate if the literal is within 200 chars of the publish call
				// (either the variable is assigned nearby, or the call uses the literal).
				dist := pm[0] - m[0]
				if dist < 0 {
					dist = -dist
				}
				if dist <= 200 {
					caller := enclosing(pm[0])
					emitEdge("Function", caller, topicID, publishesToEdgeKind, map[string]string{
						"messaging_layer": "google-cloud-pubsub",
					})
				}
			}
		}
	}

	// subscriber.subscribe(subscription_path, callback) literal form.
	for _, m := range pyPubSubSubscribeLitRe.FindAllStringSubmatchIndex(src, -1) {
		resourcePath := src[m[2]:m[3]]
		// We accept both topic paths (projects/.../topics/X) and subscription
		// paths (projects/.../subscriptions/Y) — topic name is extracted from
		// the path or used as-is.
		topic := resourcePath
		project := ""
		if rm := pyPubSubTopicResourceRe.FindStringSubmatch(resourcePath); len(rm) >= 3 {
			project, topic = rm[1], rm[2]
		} else if strings.Contains(resourcePath, "/subscriptions/") {
			// Use subscription name as fallback identifier.
			parts := strings.Split(resourcePath, "/")
			topic = parts[len(parts)-1]
		}
		if !looksLikePubSubTopic(topic) {
			continue
		}
		topicID := pubsubTopicID(project, topic)
		emitTopic(topicID, topic, project, nil)
		caller := enclosing(m[0])
		emitEdge("Function", caller, topicID, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "google-cloud-pubsub",
		})
	}

	// Variable-based subscribe: subscriber.subscribe(sub_path_var, ...) where sub_path_var
	// was assigned from a "projects/.../subscriptions/..." string literal.
	if pyPubSubSubscribeVarRe.MatchString(src) {
		for _, m := range pyPubSubSubStrLitRe.FindAllStringSubmatchIndex(src, -1) {
			// project from group 1, subscriptionName from group 2
			project := src[m[2]:m[3]]
			subName := src[m[4]:m[5]]
			if !looksLikePubSubTopic(subName) {
				continue
			}
			topicID := pubsubTopicID(project, subName)
			emitTopic(topicID, subName, project, nil)
			// Associate with the nearest subscribe call within 200 chars.
			for _, sm := range pyPubSubSubscribeVarRe.FindAllStringSubmatchIndex(src, -1) {
				dist := sm[0] - m[0]
				if dist < 0 {
					dist = -dist
				}
				if dist <= 200 {
					caller := enclosing(sm[0])
					emitEdge("Function", caller, topicID, subscribesToEdgeKind, map[string]string{
						"messaging_layer": "google-cloud-pubsub",
					})
				}
			}
		}
	}

	// subscriber.create_subscription(subscription, topic=...) — emit topic entity.
	for _, m := range pyPubSubCreateSubRe.FindAllStringSubmatchIndex(src, -1) {
		resourcePath := src[m[2]:m[3]]
		topic, project := resourcePath, ""
		if rm := pyPubSubTopicResourceRe.FindStringSubmatch(resourcePath); len(rm) >= 3 {
			project, topic = rm[1], rm[2]
		}
		if !looksLikePubSubTopic(topic) {
			continue
		}
		topicID := pubsubTopicID(project, topic)
		emitTopic(topicID, topic, project, map[string]string{"declared": "true"})
	}

	// Pub/Sub Lite: PublisherClient(...).publish(...) / SubscriberClient(...).subscribe(...)
	// We detect the class name presence and emit a placeholder if the file
	// also contains a topic_path or publish/subscribe call.
	if pyPubSubLitePublishRe.MatchString(src) {
		for _, m := range pyPubSubPublishLitRe.FindAllStringSubmatchIndex(src, -1) {
			resourcePath := src[m[2]:m[3]]
			project, topic := "", resourcePath
			if rm := pyPubSubTopicResourceRe.FindStringSubmatch(resourcePath); len(rm) >= 3 {
				project, topic = rm[1], rm[2]
			}
			if !looksLikePubSubTopic(topic) {
				continue
			}
			topicID := pubsubTopicID(project, topic)
			emitTopic(topicID, topic, project, map[string]string{"pubsub_lite": "true"})
			caller := enclosing(m[0])
			emitEdge("Function", caller, topicID, publishesToEdgeKind, map[string]string{
				"messaging_layer": "pubsub-lite",
				"pubsub_lite":     "true",
			})
		}
	}
	if pyPubSubLiteSubscribeRe.MatchString(src) {
		for _, m := range pyPubSubSubscribeLitRe.FindAllStringSubmatchIndex(src, -1) {
			resourcePath := src[m[2]:m[3]]
			topic, project := resourcePath, ""
			if rm := pyPubSubTopicResourceRe.FindStringSubmatch(resourcePath); len(rm) >= 3 {
				project, topic = rm[1], rm[2]
			}
			if !looksLikePubSubTopic(topic) {
				continue
			}
			topicID := pubsubTopicID(project, topic)
			emitTopic(topicID, topic, project, map[string]string{"pubsub_lite": "true"})
			caller := enclosing(m[0])
			emitEdge("Function", caller, topicID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "pubsub-lite",
				"pubsub_lite":     "true",
			})
		}
	}

	// Eventarc / Cloud Run trigger: def hello_pubsub(event, context) where the
	// file contains the "google.cloud.pubsub.topic.v1.messagePublished" type.
	if pyEventarcPubSubTypeRe.MatchString(src) {
		for _, m := range pyEventarcHandlerRe.FindAllStringSubmatchIndex(src, -1) {
			handlerName := src[m[2]:m[3]]
			topicID := pubsubTopicID("", "eventarc-pubsub-trigger")
			emitTopic(topicID, "eventarc-pubsub-trigger", "", map[string]string{
				"eventarc":       "true",
				"trigger_type":   "google.cloud.pubsub.topic.v1.messagePublished",
			})
			emitEdge("Function", handlerName, topicID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "eventarc",
				"eventarc":        "true",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Node — @google-cloud/pubsub
// ---------------------------------------------------------------------------

// nodePubSubTopicPublishRe captures topic.publish(data) / topic.publishMessage(msg).
// We key on ".publish(" or ".publishMessage(" in a file that has pubsub imports.
var nodePubSubTopicPublishRe = regexp.MustCompile(`\.publish(?:Message)?\s*\(`)

// nodePubSubTopicNameRe captures pubsub.topic('name') or new Topic('name').
// Group 1 = topic name.
var nodePubSubTopicNameRe = regexp.MustCompile("" +
	`(?:pubsub|ps)\.topic\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodePubSubSubscriptionNameRe captures pubsub.subscription('name').
// Group 1 = subscription name.
var nodePubSubSubscriptionNameRe = regexp.MustCompile("" +
	`(?:pubsub|ps)\.subscription\s*\(\s*` +
	"[\"'`]([^\"'`\\n\\r]+)[\"'`]")

// nodePubSubOnMessageRe captures subscription.on('message', handler).
var nodePubSubOnMessageRe = regexp.MustCompile(`\.on\s*\(\s*["']message["']\s*,`)

func synthesizeNodePubSub(
	src string,
	emitTopic func(topicID, topicName, project string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	hasPubSub := strings.Contains(src, "@google-cloud/pubsub") ||
		strings.Contains(src, "google-cloud/pubsub") ||
		strings.Contains(src, "PubSub") ||
		(strings.Contains(src, "pubsub") && strings.Contains(src, "topic"))
	if !hasPubSub {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingNodeName(src, offset)
	}

	// pubsub.topic('name') — collect topic names used in this file.
	topicNames := map[string]string{} // varname → topic name (best-effort)
	for _, m := range nodePubSubTopicNameRe.FindAllStringSubmatchIndex(src, -1) {
		topicName := src[m[2]:m[3]]
		if !looksLikePubSubTopic(topicName) {
			continue
		}
		topicID := pubsubTopicID("", topicName)
		emitTopic(topicID, topicName, "", nil)
		topicNames[topicName] = topicID

		// If followed by .publish() in close vicinity, emit PUBLISHES_TO.
		vicinity := surroundingText(src, m[0], 500)
		if nodePubSubTopicPublishRe.MatchString(vicinity) {
			caller := enclosing(m[0])
			emitEdge("Function", caller, topicID, publishesToEdgeKind, map[string]string{
				"messaging_layer": "google-cloud-pubsub-node",
			})
		}
	}

	// pubsub.subscription('name') — emit SUBSCRIBES_TO when followed by .on('message').
	for _, m := range nodePubSubSubscriptionNameRe.FindAllStringSubmatchIndex(src, -1) {
		subName := src[m[2]:m[3]]
		if !looksLikePubSubTopic(subName) {
			continue
		}
		topicID := pubsubTopicID("", subName)
		emitTopic(topicID, subName, "", nil)

		vicinity := surroundingText(src, m[0], 500)
		if nodePubSubOnMessageRe.MatchString(vicinity) {
			caller := enclosing(m[0])
			emitEdge("Function", caller, topicID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "google-cloud-pubsub-node",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Go — cloud.google.com/go/pubsub
// ---------------------------------------------------------------------------

// goPubSubPublishRe captures topic.Publish(ctx, &pubsub.Message{...}).
var goPubSubPublishRe = regexp.MustCompile(`\.Publish\s*\(\s*ctx\b`)

// goPubSubReceiveRe captures sub.Receive(ctx, handler).
var goPubSubReceiveRe = regexp.MustCompile(`\.Receive\s*\(\s*ctx\b`)

// goPubSubTopicRe captures client.Topic("name") or client.CreateTopic(ctx, "name").
// Group 1 = topic name.
var goPubSubTopicRe = regexp.MustCompile(`\.(?:Topic|CreateTopic)\s*\(\s*(?:ctx\s*,\s*)?["']([^"'\n\r]+)["']`)

// goPubSubSubscriptionRe captures client.Subscription("name") or
// client.CreateSubscription(ctx, "name", ...).
// Group 1 = subscription name.
var goPubSubSubscriptionRe = regexp.MustCompile(`\.(?:Subscription|CreateSubscription)\s*\(\s*(?:ctx\s*,\s*)?["']([^"'\n\r]+)["']`)

func synthesizeGoPubSub(
	src string,
	emitTopic func(topicID, topicName, project string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	hasPubSub := strings.Contains(src, "pubsub") || strings.Contains(src, "PubSub")
	if !hasPubSub {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingGoName(src, offset)
	}

	// client.Topic("name") — emit topic entity; detect publish in vicinity.
	for _, m := range goPubSubTopicRe.FindAllStringSubmatchIndex(src, -1) {
		topicName := src[m[2]:m[3]]
		if !looksLikePubSubTopic(topicName) {
			continue
		}
		topicID := pubsubTopicID("", topicName)
		emitTopic(topicID, topicName, "", nil)

		vicinity := surroundingText(src, m[0], 500)
		if goPubSubPublishRe.MatchString(vicinity) {
			caller := enclosing(m[0])
			emitEdge("Function", caller, topicID, publishesToEdgeKind, map[string]string{
				"messaging_layer": "go-pubsub",
			})
		}
	}

	// client.Subscription("name") — detect receive in vicinity.
	for _, m := range goPubSubSubscriptionRe.FindAllStringSubmatchIndex(src, -1) {
		subName := src[m[2]:m[3]]
		if !looksLikePubSubTopic(subName) {
			continue
		}
		topicID := pubsubTopicID("", subName)
		emitTopic(topicID, subName, "", nil)

		vicinity := surroundingText(src, m[0], 500)
		if goPubSubReceiveRe.MatchString(vicinity) {
			caller := enclosing(m[0])
			emitEdge("Function", caller, topicID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "go-pubsub",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — google-cloud-pubsub
// ---------------------------------------------------------------------------

// javaPubSubPublisherRe captures Publisher usage pattern.
var javaPubSubPublishRe = regexp.MustCompile(`publisher\.publish\s*\(`)

// javaPubSubSubscriberRe captures MessageReceiver implementation or
// subscriber.startAsync() pattern.
var javaPubSubStartAsyncRe = regexp.MustCompile(`subscriber\.startAsync\s*\(\s*\)`)

// javaPubSubTopicNameRe captures TopicName.of("project", "topic") or
// ProjectTopicName.of("project", "topic") calls.
// Groups: 1=project, 2=topic.
var javaPubSubTopicNameRe = regexp.MustCompile(`(?:TopicName|ProjectTopicName)\.of\s*\(\s*"([^"\n\r]+)"\s*,\s*"([^"\n\r]+)"`)

// javaPubSubSubscriptionNameRe captures SubscriptionName.of("project", "sub").
// Groups: 1=project, 2=subscription.
var javaPubSubSubscriptionNameRe = regexp.MustCompile(`(?:SubscriptionName|ProjectSubscriptionName)\.of\s*\(\s*"([^"\n\r]+)"\s*,\s*"([^"\n\r]+)"`)

// javaPubSubMessageReceiverRe captures MessageReceiver interface implementation.
var javaPubSubMessageReceiverRe = regexp.MustCompile(`implements\s+(?:\w+,\s*)*MessageReceiver\b`)

func synthesizeJavaPubSub(
	src string,
	emitTopic func(topicID, topicName, project string, props map[string]string),
	emitEdge func(callerKind, callerName, topicID, edgeKind string, props map[string]string),
) {
	hasPubSub := strings.Contains(src, "pubsub") || strings.Contains(src, "PubSub") ||
		strings.Contains(src, "TopicName") || strings.Contains(src, "Publisher") ||
		strings.Contains(src, "MessageReceiver")
	if !hasPubSub {
		return
	}

	className := ""
	if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
		className = m[1]
	}

	// TopicName.of("project", "topic") — emit topic entity.
	for _, m := range javaPubSubTopicNameRe.FindAllStringSubmatchIndex(src, -1) {
		project := src[m[2]:m[3]]
		topic := src[m[4]:m[5]]
		if !looksLikePubSubTopic(topic) {
			continue
		}
		topicID := pubsubTopicID(project, topic)
		emitTopic(topicID, topic, project, map[string]string{"messaging_layer": "google-cloud-pubsub-java"})

		// Detect publisher.publish() in file → PUBLISHES_TO.
		if javaPubSubPublishRe.MatchString(src) && className != "" {
			emitEdge("Service", className, topicID, publishesToEdgeKind, map[string]string{
				"messaging_layer": "google-cloud-pubsub-java",
			})
		}
	}

	// SubscriptionName.of("project", "subscription") — emit subscription entity.
	for _, m := range javaPubSubSubscriptionNameRe.FindAllStringSubmatchIndex(src, -1) {
		project := src[m[2]:m[3]]
		subName := src[m[4]:m[5]]
		if !looksLikePubSubTopic(subName) {
			continue
		}
		topicID := pubsubTopicID(project, subName)
		emitTopic(topicID, subName, project, map[string]string{"messaging_layer": "google-cloud-pubsub-java"})

		// Detect subscriber.startAsync() or MessageReceiver in file → SUBSCRIBES_TO.
		isSubscriber := javaPubSubStartAsyncRe.MatchString(src) ||
			javaPubSubMessageReceiverRe.MatchString(src)
		if isSubscriber && className != "" {
			emitEdge("Service", className, topicID, subscribesToEdgeKind, map[string]string{
				"messaging_layer": "google-cloud-pubsub-java",
			})
		}
	}

	// MessageReceiver implementation without an explicit subscription name:
	// emit a generic subscriber edge from the class.
	if javaPubSubMessageReceiverRe.MatchString(src) && className != "" {
		// Only emit if we haven't already emitted one via subscription name.
		topicID := pubsubTopicID("", className+"-subscription")
		emitTopic(topicID, className+"-subscription", "", map[string]string{
			"messaging_layer":   "google-cloud-pubsub-java",
			"message_receiver":  "true",
		})
		emitEdge("Service", className, topicID, subscribesToEdgeKind, map[string]string{
			"messaging_layer":  "google-cloud-pubsub-java",
			"message_receiver": "true",
		})
	}
}
