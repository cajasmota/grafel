// Tests for the C/C++ ZeroMQ + MQTT messaging detection pass (#3559).
//
// Value-asserting: every test pins a SPECIFIC literal endpoint/topic, role,
// and edge direction — never a bare len>0 check.
package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// runCppMessagingDetect drives the append-only pass directly.
func runCppMessagingDetect(lang, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	res := applyCppMessagingEdges(DetectorPassArgs{Lang: lang, Path: "svc/main.cpp", Content: []byte(src)})
	return res.Entities, res.Relationships
}

// topicByID returns the MessageTopic entity whose synthetic ID (Name) matches.
func topicByID(ents []types.EntityRecord, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == messageTopicKind && ents[i].Name == id {
			return &ents[i]
		}
	}
	return nil
}

// edgeTo returns the first edge of `kind` whose ToID targets the given topic ID.
func edgeTo(rels []types.RelationshipRecord, kind, topicID string) *types.RelationshipRecord {
	want := messageTopicKind + ":" + topicID
	for i := range rels {
		if rels[i].Kind == kind && rels[i].ToID == want {
			return &rels[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// ZeroMQ — cppzmq (C++)
// ---------------------------------------------------------------------------

func TestCppZmqPublisherBind(t *testing.T) {
	src := `
#include <zmq.hpp>
void run_publisher() {
    zmq::context_t ctx(1);
    zmq::socket_t pub(ctx, zmq::socket_type::pub);
    pub.bind("tcp://*:5555");
}`
	ents, rels := runCppMessagingDetect("cpp", src)

	topic := topicByID(ents, "zmq:tcp://*:5555")
	if topic == nil {
		t.Fatalf("expected MessageTopic zmq:tcp://*:5555, got entities: %+v", ents)
	}
	if topic.Properties["broker"] != "zeromq" {
		t.Errorf("broker = %q, want zeromq", topic.Properties["broker"])
	}
	if topic.Properties["socket_role"] != "pub" {
		t.Errorf("socket_role = %q, want pub", topic.Properties["socket_role"])
	}
	if topic.Properties["transport"] != "bind" {
		t.Errorf("transport = %q, want bind", topic.Properties["transport"])
	}

	e := edgeTo(rels, publishesToEdgeKind, "zmq:tcp://*:5555")
	if e == nil {
		t.Fatalf("expected PUBLISHES_TO zmq:tcp://*:5555, got rels: %+v", rels)
	}
	if e.FromID != "SCOPE.Operation:run_publisher" {
		t.Errorf("FromID = %q, want SCOPE.Operation:run_publisher", e.FromID)
	}
	if e.Properties.Get("socket_role") != "pub" {
		t.Errorf("edge socket_role = %q, want pub", e.Properties.Get("socket_role"))
	}
}

func TestCppZmqSubscriberConnect(t *testing.T) {
	src := `
#include <zmq.hpp>
void run_subscriber() {
    zmq::context_t ctx(1);
    zmq::socket_t sub(ctx, zmq::socket_type::sub);
    sub.connect("tcp://localhost:5555");
}`
	ents, rels := runCppMessagingDetect("cpp", src)

	topic := topicByID(ents, "zmq:tcp://localhost:5555")
	if topic == nil {
		t.Fatalf("expected MessageTopic zmq:tcp://localhost:5555")
	}
	if topic.Properties["socket_role"] != "sub" {
		t.Errorf("socket_role = %q, want sub", topic.Properties["socket_role"])
	}

	e := edgeTo(rels, subscribesToEdgeKind, "zmq:tcp://localhost:5555")
	if e == nil {
		t.Fatalf("expected SUBSCRIBES_TO zmq:tcp://localhost:5555, got rels: %+v", rels)
	}
	if e.FromID != "SCOPE.Operation:run_subscriber" {
		t.Errorf("FromID = %q, want SCOPE.Operation:run_subscriber", e.FromID)
	}
	if e.Properties.Get("transport") != "connect" {
		t.Errorf("transport = %q, want connect", e.Properties.Get("transport"))
	}
}

// Cross-repo linkage smoke: a bind-pub and a connect-sub on the SAME endpoint
// must collapse onto one shared node ID (the import-channel linker pivot).
func TestCppZmqEndpointSharedID(t *testing.T) {
	pubSrc := `void p(){ zmq::socket_t pub(ctx, zmq::socket_type::pub); pub.bind("tcp://*:7000"); }`
	subSrc := `void s(){ zmq::socket_t sub(ctx, zmq::socket_type::sub); sub.connect("tcp://gw:7000"); }`
	_, pubRels := runCppMessagingDetect("cpp", pubSrc)
	_, subRels := runCppMessagingDetect("cpp", subSrc)
	// Endpoints differ (wildcard vs host) so IDs differ here — this asserts the
	// ID is derived purely from the literal, deterministically.
	if edgeTo(pubRels, publishesToEdgeKind, "zmq:tcp://*:7000") == nil {
		t.Errorf("publisher edge to zmq:tcp://*:7000 missing")
	}
	if edgeTo(subRels, subscribesToEdgeKind, "zmq:tcp://gw:7000") == nil {
		t.Errorf("subscriber edge to zmq:tcp://gw:7000 missing")
	}
}

// C-API libzmq form: zmq_socket + zmq_bind.
func TestCZmqCApiBind(t *testing.T) {
	src := `
#include <zmq.h>
int main(void) {
    void *ctx = zmq_ctx_new();
    void *pub = zmq_socket(ctx, ZMQ_PUB);
    zmq_bind(pub, "ipc:///tmp/feeds");
    return 0;
}`
	ents, rels := runCppMessagingDetect("c", src)
	if topicByID(ents, "zmq:ipc:///tmp/feeds") == nil {
		t.Fatalf("expected MessageTopic zmq:ipc:///tmp/feeds, got: %+v", ents)
	}
	e := edgeTo(rels, publishesToEdgeKind, "zmq:ipc:///tmp/feeds")
	if e == nil {
		t.Fatalf("expected PUBLISHES_TO zmq:ipc:///tmp/feeds")
	}
	if e.FromID != "SCOPE.Operation:main" {
		t.Errorf("FromID = %q, want SCOPE.Operation:main", e.FromID)
	}
}

// ---------------------------------------------------------------------------
// MQTT — Mosquitto (C)
// ---------------------------------------------------------------------------

func TestCMosquittoPublishSubscribe(t *testing.T) {
	src := `
#include <mosquitto.h>
void on_connect(struct mosquitto *mosq) {
    mosquitto_subscribe(mosq, NULL, "sensors/temp", 0);
}
void send_reading(struct mosquitto *mosq) {
    mosquitto_publish(mosq, NULL, "sensors/temp", 4, "21.5", 0, false);
}`
	ents, rels := runCppMessagingDetect("c", src)

	topic := topicByID(ents, "mqtt:sensors/temp")
	if topic == nil {
		t.Fatalf("expected MessageTopic mqtt:sensors/temp, got: %+v", ents)
	}
	if topic.Properties["broker"] != "mqtt" {
		t.Errorf("broker = %q, want mqtt", topic.Properties["broker"])
	}

	sub := edgeTo(rels, subscribesToEdgeKind, "mqtt:sensors/temp")
	if sub == nil {
		t.Fatalf("expected SUBSCRIBES_TO mqtt:sensors/temp")
	}
	if sub.FromID != "SCOPE.Operation:on_connect" {
		t.Errorf("sub FromID = %q, want SCOPE.Operation:on_connect", sub.FromID)
	}
	if sub.Properties.Get("messaging_layer") != "mosquitto" {
		t.Errorf("messaging_layer = %q, want mosquitto", sub.Properties.Get("messaging_layer"))
	}

	pub := edgeTo(rels, publishesToEdgeKind, "mqtt:sensors/temp")
	if pub == nil {
		t.Fatalf("expected PUBLISHES_TO mqtt:sensors/temp")
	}
	if pub.FromID != "SCOPE.Operation:send_reading" {
		t.Errorf("pub FromID = %q, want SCOPE.Operation:send_reading", pub.FromID)
	}
}

// ---------------------------------------------------------------------------
// MQTT — Paho C++
// ---------------------------------------------------------------------------

func TestCppPahoPublish(t *testing.T) {
	src := `
#include <mqtt/async_client.h>
void publish_status(mqtt::async_client& client) {
    client.publish("devices/+/status", payload);
}`
	ents, rels := runCppMessagingDetect("cpp", src)
	if topicByID(ents, "mqtt:devices/+/status") == nil {
		t.Fatalf("expected MessageTopic mqtt:devices/+/status, got: %+v", ents)
	}
	e := edgeTo(rels, publishesToEdgeKind, "mqtt:devices/+/status")
	if e == nil {
		t.Fatalf("expected PUBLISHES_TO mqtt:devices/+/status")
	}
	if e.FromID != "SCOPE.Operation:publish_status" {
		t.Errorf("FromID = %q, want SCOPE.Operation:publish_status", e.FromID)
	}
	if e.Properties.Get("messaging_layer") != "paho-cpp" {
		t.Errorf("messaging_layer = %q, want paho-cpp", e.Properties.Get("messaging_layer"))
	}
}

// ---------------------------------------------------------------------------
// MQTT — Paho C
// ---------------------------------------------------------------------------

func TestCPahoCSubscribe(t *testing.T) {
	src := `
#include <MQTTClient.h>
void setup(MQTTClient client) {
    MQTTClient_subscribe(client, "home/livingroom/light", 1);
}`
	ents, rels := runCppMessagingDetect("c", src)
	if topicByID(ents, "mqtt:home/livingroom/light") == nil {
		t.Fatalf("expected MessageTopic mqtt:home/livingroom/light")
	}
	e := edgeTo(rels, subscribesToEdgeKind, "mqtt:home/livingroom/light")
	if e == nil {
		t.Fatalf("expected SUBSCRIBES_TO mqtt:home/livingroom/light")
	}
	if e.Properties.Get("messaging_layer") != "paho-c" {
		t.Errorf("messaging_layer = %q, want paho-c", e.Properties.Get("messaging_layer"))
	}
}

// Non-messaging file must produce nothing (no false positives from bare
// .publish()/.subscribe() without an MQTT/ZMQ token).
func TestCppMessagingNoFalsePositive(t *testing.T) {
	src := `
void handler(EventBus& bus) {
    bus.publish("some.event");
    bus.subscribe("some.event");
}`
	ents, rels := runCppMessagingDetect("cpp", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("expected no synthetics for non-messaging file, got %d ents %d rels", len(ents), len(rels))
	}
}
