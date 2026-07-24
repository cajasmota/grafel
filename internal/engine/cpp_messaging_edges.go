// C/C++ ZeroMQ + MQTT producer/consumer detection — #3559 (epic #3505).
//
// Two messaging families that don't fit the Kafka topic model are wired here
// as a dedicated append-only pass:
//
//	ZeroMQ (libzmq / cppzmq):
//	  zmq::socket_t pub(ctx, zmq::socket_type::pub);
//	  pub.bind("tcp://*:5555");
//	  zmq::socket_t sub(ctx, zmq::socket_type::sub);
//	  sub.connect("tcp://localhost:5555");
//	C API:
//	  void *pub = zmq_socket(ctx, ZMQ_PUB);
//	  zmq_bind(pub, "tcp://*:5555");
//	  zmq_connect(sub, "tcp://localhost:5555");
//
//	MQTT (Paho C / Paho C++ / Mosquitto):
//	  mosquitto_publish(mosq, NULL, "sensors/temp", ...);
//	  mosquitto_subscribe(mosq, NULL, "sensors/temp", 0);
//	  client.publish("sensors/temp", ...);    // Paho C++ async_client
//	  client.subscribe("sensors/temp", ...);
//	  MQTTClient_publishMessage(client, "sensors/temp", ...);  // Paho C
//	  MQTTClient_subscribe(client, "sensors/temp", qos);
//
// Both reuse the SCOPE.MessageTopic kind keyed by a broker-prefixed ID so the
// existing cross-repo import-channel linker matches publisher and subscriber
// sides on a shared entity ID, exactly like kafka_edges.go (#726):
//   - ZeroMQ: `zmq:<endpoint>` (e.g. `zmq:tcp://*:5555`) — bind & connect to
//     the same endpoint collapse onto one node, with role pub/sub recorded.
//   - MQTT:   `mqtt:<topic>`   (e.g. `mqtt:sensors/temp`).
//
// HONEST PARTIAL: literal endpoints/topics only. C/C++ lacks the file-local
// const resolution the higher-level extractors use, and routing/ORM-style
// indirection is a poor fit for the language. Non-literal arguments are
// skipped rather than guessed.
//
// Refs #3559, epic #3505.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// cppMessagingSupportsLanguage reports whether applyCppMessagingEdges can
// emit synthetics for `lang`. ZeroMQ + MQTT detection is C/C++ only here.
func cppMessagingSupportsLanguage(lang string) bool {
	return lang == "cpp" || lang == "c"
}

// applyCppMessagingEdges runs after the Kafka/RabbitMQ passes and APPENDS
// SCOPE.MessageTopic entities + PUBLISHES_TO / SUBSCRIBES_TO edges for C/C++
// ZeroMQ and MQTT call sites. Append-only — never modifies or removes
// existing entities or edges, so it cannot regress the surrounding pipeline.
func applyCppMessagingEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 || !cppMessagingSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	seenTopic := map[string]bool{}
	seenEdge := map[string]bool{}

	emitTopic := func(topicID, topicName, broker string, props map[string]string) {
		if seenTopic[topicID] {
			return
		}
		seenTopic[topicID] = true
		merged := map[string]string{
			"broker":          broker,
			"topic_name":      topicName,
			"pattern_type":    "cpp_messaging_synthesis",
			"runtime_dynamic": boolStr(false),
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		// SourceFile left empty so identical endpoints/topics collapse to one
		// node per repo and match across repos via the import-channel linker.
		entities = append(entities, types.EntityRecord{
			Name:               topicID,
			Kind:               messageTopicKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitEdge := func(callerName, topicID, edgeKind, broker string, props map[string]string) {
		if callerName == "" || topicID == "" {
			return
		}
		key := edgeKind + "|" + callerName + "|" + topicID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		base := types.Props{
			{K: "broker", V: broker},
			{K: "pattern_type", V: "cpp_messaging_synthesis"},
		}
		for k, v := range props {
			if v != "" {
				base.Set(k, v)
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fmt.Sprintf("SCOPE.Operation:%s", callerName),
			ToID:       fmt.Sprintf("%s:%s", messageTopicKind, topicID),
			Kind:       edgeKind,
			Properties: base,
		})
	}

	enclosing := func(offset int) string { return findEnclosingCppName(src, offset) }

	synthesizeCppZeroMQ(src, enclosing, emitTopic, emitEdge)
	synthesizeCppMQTT(src, enclosing, emitTopic, emitEdge)

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// zmqTopicID returns the canonical synthetic ID for a ZeroMQ endpoint.
func zmqTopicID(endpoint string) string { return "zmq:" + endpoint }

// mqttTopicID returns the canonical synthetic ID for an MQTT topic.
func mqttTopicID(topic string) string { return "mqtt:" + topic }

// looksLikeZmqEndpoint returns true when `s` looks like a ZeroMQ transport
// endpoint (tcp://, ipc://, inproc://, pgm://, epgm://).
func looksLikeZmqEndpoint(s string) bool {
	if s == "" || len(s) > 250 {
		return false
	}
	for _, scheme := range []string{"tcp://", "ipc://", "inproc://", "pgm://", "epgm://", "vmci://"} {
		if strings.HasPrefix(s, scheme) {
			return true
		}
	}
	return false
}

// looksLikeMqttTopic returns true when `s` is a plausible MQTT topic filter.
// MQTT topics are slash-delimited and may contain + / # wildcards.
func looksLikeMqttTopic(s string) bool {
	if s == "" || len(s) > 250 {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t<>{}\"") {
		return false
	}
	hasAlnum := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			hasAlnum = true
		case r == '/' || r == '.' || r == '_' || r == '-' || r == '+' || r == '#' || r == ':':
		default:
			return false
		}
	}
	return hasAlnum
}

// ---------------------------------------------------------------------------
// ZeroMQ — libzmq (C) + cppzmq (C++)
// ---------------------------------------------------------------------------

// cppZmqSocketCtorRe captures cppzmq socket construction with an explicit
// socket type: `zmq::socket_t pub(ctx, zmq::socket_type::pub);`.
// Group 1 = variable name, group 2 = socket-type token (pub/sub/push/pull/...).
var cppZmqSocketCtorRe = regexp.MustCompile(`zmq::socket_t\s+(\w+)\s*\([^,]+,\s*zmq::socket_type::(\w+)\s*\)`)

// cppZmqSocketCRe captures C-API `zmq_socket(ctx, ZMQ_PUB)` assigned to a var:
// `void *pub = zmq_socket(ctx, ZMQ_PUB);`. Group 1 = var, group 2 = type.
var cppZmqSocketCRe = regexp.MustCompile(`(\w+)\s*=\s*zmq_socket\s*\([^,]+,\s*ZMQ_(\w+)\s*\)`)

// cppZmqBindRe captures `sock.bind("endpoint")` / `zmq_bind(sock, "endpoint")`.
// Group 1 = var (C++ method form), group 2 = endpoint (C++),
// group 3 = var (C func form), group 4 = endpoint (C).
var cppZmqBindRe = regexp.MustCompile(`(\w+)\s*\.\s*bind\s*\(\s*"([^"\n\r]+)"|zmq_bind\s*\(\s*(\w+)\s*,\s*"([^"\n\r]+)"`)

// cppZmqConnectRe captures `sock.connect("endpoint")` / `zmq_connect(sock, "endpoint")`.
var cppZmqConnectRe = regexp.MustCompile(`(\w+)\s*\.\s*connect\s*\(\s*"([^"\n\r]+)"|zmq_connect\s*\(\s*(\w+)\s*,\s*"([^"\n\r]+)"`)

// zmqRoleIsPublisher classifies a ZeroMQ socket-type token as publisher-side.
// PUB / PUSH / PUSH-like sockets produce; SUB / PULL consume.
func zmqRoleIsPublisher(socketType string) (role string, isPub bool, known bool) {
	switch strings.ToLower(socketType) {
	case "pub", "xpub", "push", "pair", "pub_t":
		return "pub", true, true
	case "sub", "xsub", "pull", "sub_t":
		return "sub", false, true
	default:
		return "", false, false
	}
}

func synthesizeCppZeroMQ(
	src string,
	enclosing func(offset int) string,
	emitTopic func(topicID, topicName, broker string, props map[string]string),
	emitEdge func(callerName, topicID, edgeKind, broker string, props map[string]string),
) {
	if !strings.Contains(src, "zmq") && !strings.Contains(src, "ZMQ") {
		return
	}

	// Map socket variable name -> role token (pub/sub) discovered at ctor.
	sockRole := map[string]string{}
	for _, m := range cppZmqSocketCtorRe.FindAllStringSubmatch(src, -1) {
		sockRole[m[1]] = m[2]
	}
	for _, m := range cppZmqSocketCRe.FindAllStringSubmatch(src, -1) {
		sockRole[m[1]] = m[2]
	}

	// emitSocketEndpoint records the endpoint topic + a pub/sub edge using the
	// socket's known role; `op` is bind|connect for property context.
	emitSocketEndpoint := func(sockVar, endpoint, op string, offset int) {
		if !looksLikeZmqEndpoint(endpoint) {
			return
		}
		roleTok := sockRole[sockVar]
		role, isPub, known := zmqRoleIsPublisher(roleTok)
		id := zmqTopicID(endpoint)
		props := map[string]string{
			"messaging_layer": "zeromq",
			"endpoint":        endpoint,
			"transport":       op, // bind | connect
		}
		if known {
			props["socket_role"] = role
		}
		emitTopic(id, endpoint, "zeromq", props)
		// Without a known role, fall back to bind=>publisher, connect=>subscriber
		// (the conventional ZeroMQ topology), but mark direction as inferred.
		edgeKind := subscribesToEdgeKind
		if known {
			if isPub {
				edgeKind = publishesToEdgeKind
			}
		} else if op == "bind" {
			edgeKind = publishesToEdgeKind
		}
		edgeProps := map[string]string{
			"messaging_layer": "zeromq",
			"endpoint":        endpoint,
			"transport":       op,
		}
		if known {
			edgeProps["socket_role"] = role
		} else {
			edgeProps["direction_inferred"] = "true"
		}
		emitEdge(enclosing(offset), id, edgeKind, "zeromq", edgeProps)
	}

	for _, m := range cppZmqBindRe.FindAllStringSubmatchIndex(src, -1) {
		if m[2] != -1 { // C++ method form: var.bind("ep")
			emitSocketEndpoint(submatch(src, m, 1), submatch(src, m, 2), "bind", m[0])
		} else if m[6] != -1 { // C func form: zmq_bind(var, "ep")
			emitSocketEndpoint(submatch(src, m, 3), submatch(src, m, 4), "bind", m[0])
		}
	}
	for _, m := range cppZmqConnectRe.FindAllStringSubmatchIndex(src, -1) {
		if m[2] != -1 {
			emitSocketEndpoint(submatch(src, m, 1), submatch(src, m, 2), "connect", m[0])
		} else if m[6] != -1 {
			emitSocketEndpoint(submatch(src, m, 3), submatch(src, m, 4), "connect", m[0])
		}
	}
}

// ---------------------------------------------------------------------------
// MQTT — Paho C / Paho C++ / Mosquitto
// ---------------------------------------------------------------------------

// cppMosqPublishRe captures `mosquitto_publish(mosq, &mid, "topic", ...)`.
// The topic is the 3rd positional argument. Group 1 = topic.
var cppMosqPublishRe = regexp.MustCompile(`mosquitto_publish\s*\(\s*[^,]+,\s*[^,]+,\s*"([^"\n\r]+)"`)

// cppMosqSubscribeRe captures `mosquitto_subscribe(mosq, NULL, "topic", qos)`.
// Group 1 = topic (3rd argument).
var cppMosqSubscribeRe = regexp.MustCompile(`mosquitto_subscribe\s*\(\s*[^,]+,\s*[^,]+,\s*"([^"\n\r]+)"`)

// cppPahoCPublishRe captures Paho C `MQTTClient_publishMessage(client, "topic", ...)`
// and `MQTTAsync_sendMessage(client, "topic", ...)`. Group 1/2 = topic.
var cppPahoCPublishRe = regexp.MustCompile(`MQTT(?:Client|Async)_(?:publishMessage|send|sendMessage)\s*\(\s*[^,]+,\s*"([^"\n\r]+)"`)

// cppPahoCSubscribeRe captures Paho C `MQTTClient_subscribe(client, "topic", qos)`
// / `MQTTAsync_subscribe(client, "topic", qos, ...)`. Group 1 = topic.
var cppPahoCSubscribeRe = regexp.MustCompile(`MQTT(?:Client|Async)_subscribe\s*\(\s*[^,]+,\s*"([^"\n\r]+)"`)

// cppPahoCppPublishRe captures Paho C++ `client.publish("topic", ...)` /
// `client->publish("topic", ...)`. The `->`/`.` and identifier guard against
// matching unrelated publish() calls. Group 1/2 = topic.
var cppPahoCppPublishRe = regexp.MustCompile(`\w+\s*(?:\.|->)\s*publish\s*\(\s*"([^"\n\r]+)"`)

// cppPahoCppSubscribeRe captures Paho C++ `client.subscribe("topic", ...)`.
var cppPahoCppSubscribeRe = regexp.MustCompile(`\w+\s*(?:\.|->)\s*subscribe\s*\(\s*"([^"\n\r]+)"`)

func synthesizeCppMQTT(
	src string,
	enclosing func(offset int) string,
	emitTopic func(topicID, topicName, broker string, props map[string]string),
	emitEdge func(callerName, topicID, edgeKind, broker string, props map[string]string),
) {
	hasMosq := strings.Contains(src, "mosquitto_")
	hasPahoC := strings.Contains(src, "MQTTClient_") || strings.Contains(src, "MQTTAsync_")
	// Paho C++ is gated on an MQTT/mqtt token to avoid matching arbitrary
	// .publish()/.subscribe() method calls (ZeroMQ already handled above).
	hasPahoCpp := (strings.Contains(src, "mqtt") || strings.Contains(src, "MQTT")) &&
		(strings.Contains(src, "async_client") || strings.Contains(src, "mqtt::") ||
			strings.Contains(src, "PahoClient") || strings.Contains(src, "paho"))
	if !hasMosq && !hasPahoC && !hasPahoCpp {
		return
	}

	pub := func(topic, layer string, offset int) {
		if !looksLikeMqttTopic(topic) {
			return
		}
		id := mqttTopicID(topic)
		emitTopic(id, topic, "mqtt", map[string]string{"messaging_layer": layer})
		emitEdge(enclosing(offset), id, publishesToEdgeKind, "mqtt", map[string]string{
			"messaging_layer": layer,
		})
	}
	sub := func(topic, layer string, offset int) {
		if !looksLikeMqttTopic(topic) {
			return
		}
		id := mqttTopicID(topic)
		emitTopic(id, topic, "mqtt", map[string]string{"messaging_layer": layer})
		emitEdge(enclosing(offset), id, subscribesToEdgeKind, "mqtt", map[string]string{
			"messaging_layer": layer,
		})
	}

	if hasMosq {
		for _, m := range cppMosqPublishRe.FindAllStringSubmatchIndex(src, -1) {
			pub(submatch(src, m, 1), "mosquitto", m[0])
		}
		for _, m := range cppMosqSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
			sub(submatch(src, m, 1), "mosquitto", m[0])
		}
	}
	if hasPahoC {
		for _, m := range cppPahoCPublishRe.FindAllStringSubmatchIndex(src, -1) {
			pub(submatch(src, m, 1), "paho-c", m[0])
		}
		for _, m := range cppPahoCSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
			sub(submatch(src, m, 1), "paho-c", m[0])
		}
	}
	if hasPahoCpp {
		for _, m := range cppPahoCppPublishRe.FindAllStringSubmatchIndex(src, -1) {
			pub(submatch(src, m, 1), "paho-cpp", m[0])
		}
		for _, m := range cppPahoCppSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
			sub(submatch(src, m, 1), "paho-cpp", m[0])
		}
	}
}

// submatch returns the substring for capture group `g` of an index-match `m`,
// or "" when the group did not participate.
func submatch(src string, m []int, g int) string {
	lo, hi := m[2*g], m[2*g+1]
	if lo < 0 || hi < 0 {
		return ""
	}
	return src[lo:hi]
}
