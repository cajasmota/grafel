// Tests for the generic string-literal event-identity pass (GAP-005).
//
// Coverage:
//   - Producer (Go): publish-site {eventType:"X"} literal → PUBLISHES_TO.
//   - Producer (JS/TS): publish-site {eventType:"X"} literal → PUBLISHES_TO.
//   - Consumer (Terraform ESM FilterCriteria): data.eventType array →
//     SUBSCRIBES_TO for each value (GAP-003 fold-in).
//   - Precision: bare string not at a publish site → no node; non-allowlisted
//     key at a publish site → no node.
//   - Fan-out cap: a hot event string is capped, not unbounded.
//   - No-op guards: unsupported language, empty content.
//
// Refs GAP-005 (design: .grafel/research/design-gap-005-event-identity.md).
package engine

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func runEventTypeDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyEventTypeEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

func eventTypeEntityByID(ents []types.EntityRecord, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == eventTypeKind && ents[i].Name == id {
			return &ents[i]
		}
	}
	return nil
}

func requireEventTypeEntity(t *testing.T, ents []types.EntityRecord, id, label string) {
	t.Helper()
	if eventTypeEntityByID(ents, id) == nil {
		t.Errorf("%s: expected SCOPE.EventType entity %q; got %v", label, id, entNames(ents))
	}
}

func requireNoEventTypeEntities(t *testing.T, ents []types.EntityRecord, label string) {
	t.Helper()
	for _, e := range ents {
		if e.Kind == eventTypeKind {
			t.Errorf("%s: expected NO SCOPE.EventType entities; got %v", label, entNames(ents))
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Producer — Go
// ---------------------------------------------------------------------------

func TestEventType_GoProducer_PublishSiteLiteral(t *testing.T) {
	src := `package producer

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
)

func PublishOrderPlaced(ctx context.Context, client *kinesis.Client, orderID string) error {
	_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName:   aws.String("orders-stream"),
		PartitionKey: aws.String(orderID),
		Data:         []byte(fmt.Sprintf(` + "`" + `{"eventType":"OrderPlaced","orderId":"%s"}` + "`" + `, orderID)),
	})
	return err
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer.go", src)

	id := eventTypeID("OrderPlaced")
	requireEventTypeEntity(t, ents, id, "Go producer")

	fromID := extractor.BuildOperationStructuralRef("go", "producer.go", "PublishOrderPlaced")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go producer")
}

// TestEventType_GoProducer_FunctionScopeStructField covers the REALISTIC Go
// producer shape (GAP-005 root-cause C, zero-yield-on-real-corpus
// investigation): the event/envelope struct is built and its EventType field
// set SEPARATELY from the publish call — via a struct literal a few lines
// above `client.PutRecord(...)`, not co-located inside the call's own
// argument list. TestEventType_GoProducer_PublishSiteLiteral only covers the
// co-located shape; this exercises the function-scope-recall widening.
func TestEventType_GoProducer_FunctionScopeStructField(t *testing.T) {
	src := `package producer

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
)

type OrderEvent struct {
	EventType string ` + "`json:\"eventType\"`" + `
	OrderID   string ` + "`json:\"orderId\"`" + `
}

func PublishOrderPlaced(ctx context.Context, client *kinesis.Client, orderID string) error {
	evt := OrderEvent{
		EventType: "OrderPlaced",
		OrderID:   orderID,
	}
	body, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	_, err = client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName:   aws.String("orders-stream"),
		PartitionKey: aws.String(orderID),
		Data:         body,
	})
	return err
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer.go", src)

	id := eventTypeID("OrderPlaced")
	requireEventTypeEntity(t, ents, id, "Go producer (function-scope struct field)")

	fromID := extractor.BuildOperationStructuralRef("go", "producer.go", "PublishOrderPlaced")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go producer (function-scope struct field)")
}

// TestEventType_GoProducer_FunctionScopeRecall_RequiresPublishSink verifies
// the recall widening still requires a REAL publish call in the same
// function — an eventType-keyed struct field in a function with no publish
// sink never mints a node (precision guard for root-cause-C fix).
func TestEventType_GoProducer_FunctionScopeRecall_RequiresPublishSink(t *testing.T) {
	src := `package producer

type OrderEvent struct {
	EventType string
}

func BuildOrderEvent(orderID string) OrderEvent {
	evt := OrderEvent{EventType: "OrderPlaced"}
	return evt
}
`
	ents, _ := runEventTypeDetect(t, "go", "producer.go", src)
	requireNoEventTypeEntities(t, ents, "Go struct field with no publish sink in function")
}

// ---------------------------------------------------------------------------
// Producer — Go — EventBridge PutEvents (GAP-015 RC2-RC4)
// ---------------------------------------------------------------------------

// TestEventType_GoProducer_EventBridgePutEvents covers RC2 (PutEvents was
// absent from goPublishSiteRe) + RC3 (DetailType: aws.String("...") wraps
// the string literal in a single SDK-helper call, which the bare-quote
// allowlist regex did not tolerate). A real EventBridge Go producer shape:
// `client.PutEvents(ctx, &eventbridge.PutEventsInput{Entries: []types.
// PutEventsRequestEntry{{DetailType: aws.String("OrderPlaced"), ...}}})`.
func TestEventType_GoProducer_EventBridgePutEvents(t *testing.T) {
	src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

func PublishOrderPlaced(ctx context.Context, client *eventbridge.Client, orderID string) error {
	_, err := client.PutEvents(ctx, &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{
			{
				DetailType: aws.String("OrderPlaced"),
				Detail:     aws.String(orderID),
				Source:     aws.String("orders-service"),
			},
		},
	})
	return err
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer.go", src)

	id := eventTypeID("OrderPlaced")
	requireEventTypeEntity(t, ents, id, "Go EventBridge PutEvents producer")

	fromID := extractor.BuildOperationStructuralRef("go", "producer.go", "PublishOrderPlaced")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go EventBridge PutEvents producer")
}

// TestEventType_GoProducer_EventBridgePutEventsWithContext covers the
// PutEventsWithContext (AWS SDK v1-style) variant of RC2.
func TestEventType_GoProducer_EventBridgePutEventsWithContext(t *testing.T) {
	src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/eventbridge"
)

func PublishOrderShipped(ctx context.Context, client *eventbridge.EventBridge, orderID string) error {
	_, err := client.PutEventsWithContext(ctx, &eventbridge.PutEventsInput{
		Entries: []*eventbridge.PutEventsRequestEntry{
			{
				DetailType: aws.String("OrderShipped"),
				Detail:     aws.String(orderID),
				Source:     aws.String("orders-service"),
			},
		},
	})
	return err
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer_v1.go", src)

	id := eventTypeID("OrderShipped")
	requireEventTypeEntity(t, ents, id, "Go EventBridge PutEventsWithContext producer")

	fromID := extractor.BuildOperationStructuralRef("go", "producer_v1.go", "PublishOrderShipped")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go EventBridge PutEventsWithContext producer")
}

// TestEventType_GoProducer_EventBridgeDetailType_BareQuoteWrapperDifference
// demonstrates RC3 directly: the aws.String(...) wrapper form must match
// where a naive bare-quote-only regex would not. This isn't a regression
// test on the OLD regex (which no longer exists) — it documents intent by
// asserting the wrapped form resolves via the SAME allowlist key
// (DetailType) that the bare-literal form already covered.
func TestEventType_GoProducer_EventBridgeDetailType_BareQuoteWrapperDifference(t *testing.T) {
	src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

func PublishOrderSettled(ctx context.Context, client *eventbridge.Client) error {
	entry := types.PutEventsRequestEntry{DetailType: aws.String("OrderSettled")}
	_, err := client.PutEvents(ctx, &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{entry},
	})
	return err
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer_wrapper.go", src)

	id := eventTypeID("OrderSettled")
	requireEventTypeEntity(t, ents, id, "Go EventBridge aws.String wrapper producer")

	fromID := extractor.BuildOperationStructuralRef("go", "producer_wrapper.go", "PublishOrderSettled")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go EventBridge aws.String wrapper producer")
}

// TestEventType_GoProducer_EventBridgeDetailTypeConst covers RC4: the
// DetailType value is a bare identifier (`aws.String(orderDetailType)`)
// bound to a same-file `const orderDetailType = "OrderShipped"`. The
// producer detector must resolve the identifier to its literal.
func TestEventType_GoProducer_EventBridgeDetailTypeConst(t *testing.T) {
	src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

const orderDetailType = "OrderShipped"

func PublishOrderShipped(ctx context.Context, client *eventbridge.Client, orderID string) error {
	_, err := client.PutEvents(ctx, &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{
			{
				DetailType: aws.String(orderDetailType),
				Detail:     aws.String(orderID),
				Source:     aws.String("orders-service"),
			},
		},
	})
	return err
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer_const.go", src)

	id := eventTypeID("OrderShipped")
	requireEventTypeEntity(t, ents, id, "Go EventBridge const-bound DetailType producer")

	fromID := extractor.BuildOperationStructuralRef("go", "producer_const.go", "PublishOrderShipped")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go EventBridge const-bound DetailType producer")

	ent := eventTypeEntityByID(ents, id)
	if ent == nil {
		t.Fatalf("expected event-type entity %q to exist", id)
	}
	var edgeDetection string
	for _, r := range rels {
		if r.FromID == fromID && r.ToID == fmt.Sprintf("%s:%s", eventTypeKind, id) {
			edgeDetection = r.Properties["detection"]
		}
	}
	if edgeDetection != "eventbridge-detailtype-const" {
		t.Errorf("expected detection=eventbridge-detailtype-const, got %q", edgeDetection)
	}
}

// TestEventType_GoProducer_EventBridgeDetailType_NoPublishSink_NoEdge is the
// negative guard for RC3/RC4: a DetailType: aws.String("X") literal with NO
// PutEvents (or any other recognized publish sink) in the enclosing function
// must mint NOTHING — the publish-sink gate must not be bypassed by the new
// wrapper-call / const-resolution allowances.
func TestEventType_GoProducer_EventBridgeDetailType_NoPublishSink_NoEdge(t *testing.T) {
	src := `package producer

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

func BuildOrderPlacedEntry() types.PutEventsRequestEntry {
	return types.PutEventsRequestEntry{DetailType: aws.String("OrderPlaced")}
}
`
	ents, _ := runEventTypeDetect(t, "go", "producer_no_sink.go", src)
	requireNoEventTypeEntities(t, ents, "DetailType literal with no publish sink in function")
}

// TestEventType_GoProducer_EventBridgeDetailType_ParamShadowsPackageConst is
// the correctness guard for review MUST-FIX #1: the identifier at the publish
// site is the enclosing function's PARAMETER (`detail string`), whose runtime
// value is unknown — but a same-named PACKAGE-LEVEL const with a different
// literal exists elsewhere in the file. The file-global binding table must
// NOT resolve the param to that unrelated const literal. No node/edge.
func TestEventType_GoProducer_EventBridgeDetailType_ParamShadowsPackageConst(t *testing.T) {
	src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

const detail = "OrderPlaced"

func Publish(ctx context.Context, detail string, client *eventbridge.Client) error {
	_, err := client.PutEvents(ctx, &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{
			{DetailType: aws.String(detail)},
		},
	})
	return err
}
`
	ents, _ := runEventTypeDetect(t, "go", "producer_shadow.go", src)
	requireNoEventTypeEntities(t, ents, "param shadows package const — must not resolve to const literal")
}

// TestEventType_GoProducer_EventBridgeDetailType_CrossFunctionLocalNotResolved
// is the reviewer's exact reproducer for MUST-FIX #1: a `:=` local in one
// function must never bind an identically-named identifier at a publish site
// in a DIFFERENT function (there, `detail` is a parameter). No wrong edge.
func TestEventType_GoProducer_EventBridgeDetailType_CrossFunctionLocalNotResolved(t *testing.T) {
	src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

func A() {
	detail := "OrderPlaced"
	_ = detail
}

func B(ctx context.Context, detail string, client *eventbridge.Client) error {
	_, err := client.PutEvents(ctx, &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{
			{DetailType: aws.String(detail)},
		},
	})
	return err
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer_crossfn.go", src)
	requireNoEventTypeEntities(t, ents, "cross-function local must not resolve at param site")
	bFromID := extractor.BuildOperationStructuralRef("go", "producer_crossfn.go", "B")
	for _, r := range rels {
		if r.FromID == bFromID {
			t.Errorf("expected no PUBLISHES_TO edge from B; got edge to %q", r.ToID)
		}
	}
}

// TestEventType_GoProducer_EventBridgeDetailType_FormatterNotMinted is the
// guard for review MUST-FIX #2: a single-arg wrapper that is a FORMATTER or
// TEMPLATE call must not mint a garbage / never-joining node. Both cases have
// a real PutEvents sink, so the ONLY reason to reject is the wrapper's nature.
func TestEventType_GoProducer_EventBridgeDetailType_FormatterNotMinted(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// fmt.Sprintf as the DIRECT DetailType wrapper, with a format verb —
			// the outermost single-wrapper exposure. Runtime value carries a `%s`.
			name: "sprintf-verb",
			src: `package producer

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

func Publish(ctx context.Context, region string, client *eventbridge.Client) error {
	_, err := client.PutEvents(ctx, &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{
			{DetailType: fmt.Sprintf("order.%s.placed", region)},
		},
	})
	return err
}
`,
		},
		{
			// strings.ToUpper as the DIRECT DetailType wrapper — runtime value
			// ("ORDERPLACED") never verbatim-joins the literal "orderplaced".
			name: "strings-toupper",
			src: `package producer

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

func Publish(ctx context.Context, client *eventbridge.Client) error {
	_, err := client.PutEvents(ctx, &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{
			{DetailType: strings.ToUpper("orderplaced")},
		},
	})
	return err
}
`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ents, _ := runEventTypeDetect(t, "go", "producer_fmt.go", c.src)
			requireNoEventTypeEntities(t, ents, "formatter/template wrapper ("+c.name+")")
		})
	}
}

// TestEventType_GoProducer_EventBridgeDetailType_ClosureParamShadowsConst is
// the residual guard for re-review MUST-FIX #1: the identifier at the publish
// site is a parameter of a CLOSURE (func literal) that lexically encloses the
// call, whose runtime value is unknown — but a same-named package-level const
// exists. The shadow guard must see the closure param, not just the top-level
// func decl. No node/edge.
func TestEventType_GoProducer_EventBridgeDetailType_ClosureParamShadowsConst(t *testing.T) {
	src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

const orderType = "OrderPlaced"

func Outer(ctx context.Context, c *eventbridge.Client) {
	h := func(orderType string) {
		_, _ = c.PutEvents(ctx, &eventbridge.PutEventsInput{
			Entries: []types.PutEventsRequestEntry{
				{DetailType: aws.String(orderType)},
			},
		})
	}
	h("ShouldNotResolve")
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer_closure.go", src)
	requireNoEventTypeEntities(t, ents, "closure param shadows package const — must not resolve to const literal")
	outerFromID := extractor.BuildOperationStructuralRef("go", "producer_closure.go", "Outer")
	hFromID := extractor.BuildOperationStructuralRef("go", "producer_closure.go", "h")
	for _, r := range rels {
		if r.FromID == outerFromID || r.FromID == hFromID {
			t.Errorf("expected no PUBLISHES_TO edge from closure/outer; got edge to %q", r.ToID)
		}
	}
}

// TestEventType_GoProducer_EventBridgeDetailType_ConstTemplateNotMinted is the
// residual guard for re-review MUST-FIX #2: a const bound to a `%`-format
// template resolved via the identifier path must be rejected by the same
// value-usability gate the literal path applies. No garbage node.
func TestEventType_GoProducer_EventBridgeDetailType_ConstTemplateNotMinted(t *testing.T) {
	src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

const tmpl = "order.%s.placed"

func Pub(ctx context.Context, c *eventbridge.Client) error {
	_, err := c.PutEvents(ctx, &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{
			{DetailType: aws.String(tmpl)},
		},
	})
	return err
}
`
	ents, _ := runEventTypeDetect(t, "go", "producer_const_tmpl.go", src)
	requireNoEventTypeEntities(t, ents, "const %-template resolved via ident path must not mint")
}

// TestEventType_GoProducer_EventBridgeDetailType_GroupedConst covers re-review
// MUST-FIX #3: a grouped `const ( X = "..." )` block member (bare `X = "..."`,
// no per-line `const` keyword) must resolve at the publish site.
func TestEventType_GoProducer_EventBridgeDetailType_GroupedConst(t *testing.T) {
	src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

const (
	someOther       = "OrderPlaced"
	orderDetailType = "OrderShipped"
)

func PublishOrderShipped(ctx context.Context, client *eventbridge.Client, orderID string) error {
	_, err := client.PutEvents(ctx, &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{
			{DetailType: aws.String(orderDetailType)},
		},
	})
	return err
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer_grouped_const.go", src)

	id := eventTypeID("OrderShipped")
	requireEventTypeEntity(t, ents, id, "Go EventBridge grouped-const DetailType producer")

	fromID := extractor.BuildOperationStructuralRef("go", "producer_grouped_const.go", "PublishOrderShipped")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go EventBridge grouped-const DetailType producer")

	var edgeDetection string
	for _, r := range rels {
		if r.FromID == fromID && r.ToID == toID {
			edgeDetection = r.Properties["detection"]
		}
	}
	if edgeDetection != "eventbridge-detailtype-const" {
		t.Errorf("expected detection=eventbridge-detailtype-const, got %q", edgeDetection)
	}
}

// ---------------------------------------------------------------------------
// Producer — JS/TS
// ---------------------------------------------------------------------------

func TestEventType_JSTSProducer_PublishSiteLiteral(t *testing.T) {
	src := `import { KinesisClient, PutRecordCommand } from "@aws-sdk/client-kinesis";

export async function publishOrderPlaced(client: KinesisClient, orderId: string): Promise<void> {
  await client.send(new PutRecordCommand({
    StreamName: "orders-stream",
    PartitionKey: orderId,
    Data: Buffer.from(JSON.stringify({ eventType: "OrderPlaced", orderId })),
  }));
}
`
	ents, rels := runEventTypeDetect(t, "typescript", "producer.ts", src)

	id := eventTypeID("OrderPlaced")
	requireEventTypeEntity(t, ents, id, "JS/TS producer")

	fromID := extractor.BuildOperationStructuralRef("typescript", "producer.ts", "publishOrderPlaced")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "JS/TS producer")
}

// ---------------------------------------------------------------------------
// Producer — Java (GAP-015 RC5)
// ---------------------------------------------------------------------------

// TestEventType_JavaProducer_BuilderChainPutEvents covers the dominant
// EventBridge shape: the AWS SDK v2 builder chain nests
// `.detailType("X")` INSIDE the `.putEvents(...)` call's own argument list
// (PutEventsRequest.builder().entries(PutEventsRequestEntry.builder()...)).
func TestEventType_JavaProducer_BuilderChainPutEvents(t *testing.T) {
	src := `package producer;

import software.amazon.awssdk.services.eventbridge.EventBridgeClient;
import software.amazon.awssdk.services.eventbridge.model.PutEventsRequest;
import software.amazon.awssdk.services.eventbridge.model.PutEventsRequestEntry;

public class OrderEventPublisher {
    private final EventBridgeClient eventBridgeClient;

    public void publishOrderPlaced(String orderId) {
        eventBridgeClient.putEvents(PutEventsRequest.builder()
            .entries(PutEventsRequestEntry.builder()
                .detailType("OrderPlaced")
                .detail("{\"orderId\":\"" + orderId + "\"}")
                .build())
            .build());
    }
}
`
	ents, rels := runEventTypeDetect(t, "java", "OrderEventPublisher.java", src)

	id := eventTypeID("OrderPlaced")
	requireEventTypeEntity(t, ents, id, "Java producer (builder-chain putEvents)")

	fromID := extractor.BuildOperationStructuralRef("java", "OrderEventPublisher.java", "publishOrderPlaced")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Java producer (builder-chain putEvents)")
}

// TestEventType_JavaProducer_BuilderChainDetailTypeSecondEntry exercises a
// second co-located builder-chain shape with a distinct synthetic event name
// (multi-entry putEvents), confirming the detailType inside the putEvents
// argument binds regardless of surrounding builder verbosity.
func TestEventType_JavaProducer_BuilderChainDetailTypeSecondEntry(t *testing.T) {
	src := `package producer;

import software.amazon.awssdk.services.eventbridge.EventBridgeClient;
import software.amazon.awssdk.services.eventbridge.model.PutEventsRequest;
import software.amazon.awssdk.services.eventbridge.model.PutEventsRequestEntry;

public class ShipmentPublisher {
    private final EventBridgeClient eventBridgeClient;

    public void publishOrderShipped(String orderId) {
        eventBridgeClient.putEvents(
            PutEventsRequest.builder()
                .entries(
                    PutEventsRequestEntry.builder()
                        .source("orders.service")
                        .detailType("OrderShipped")
                        .detail("{\"orderId\":\"" + orderId + "\"}")
                        .build())
                .build());
    }
}
`
	ents, rels := runEventTypeDetect(t, "java", "ShipmentPublisher.java", src)

	id := eventTypeID("OrderShipped")
	requireEventTypeEntity(t, ents, id, "Java producer (builder-chain second entry)")

	fromID := extractor.BuildOperationStructuralRef("java", "ShipmentPublisher.java", "publishOrderShipped")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Java producer (builder-chain second entry)")
}

// TestEventType_JavaProducer_UnbalancedParenInDetailString reproduces
// review MEDIUM-1: the EventBridge `detail` payload is arbitrary JSON /
// free-text, so an unbalanced `(` inside a string literal in the putEvents
// argument (here a `:(` emoticon) must NOT desync paren-depth counting and
// swallow the argument — the detailType must still bind.
func TestEventType_JavaProducer_UnbalancedParenInDetailString(t *testing.T) {
	src := `package producer;

import software.amazon.awssdk.services.eventbridge.EventBridgeClient;
import software.amazon.awssdk.services.eventbridge.model.PutEventsRequest;
import software.amazon.awssdk.services.eventbridge.model.PutEventsRequestEntry;

public class CancelPublisher {
    private final EventBridgeClient eventBridgeClient;

    public void publishOrderCancelled(String orderId) {
        eventBridgeClient.putEvents(PutEventsRequest.builder()
            .entries(PutEventsRequestEntry.builder()
                .detailType("OrderCancelled")
                .detail("{\"msg\":\"Sorry to see you go :(\"}")
                .build())
            .build());
    }
}
`
	ents, rels := runEventTypeDetect(t, "java", "CancelPublisher.java", src)

	id := eventTypeID("OrderCancelled")
	requireEventTypeEntity(t, ents, id, "Java producer (unbalanced paren in detail string)")

	fromID := extractor.BuildOperationStructuralRef("java", "CancelPublisher.java", "publishOrderCancelled")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Java producer (unbalanced paren in detail string)")
}

// TestEventType_JavaProducer_Precision_NoPublishSink verifies that a
// `.detailType("X")` builder call with NO `.putEvents(` sink co-located in
// the same call argument never mints an edge — the heuristic must stay gated
// on a real EventBridge putEvents sink, not just the detailType binding.
func TestEventType_JavaProducer_Precision_NoPublishSink(t *testing.T) {
	src := `package producer;

import software.amazon.awssdk.services.eventbridge.model.PutEventsRequestEntry;

public class NotAPublisher {
    public String describe() {
        return PutEventsRequestEntry.builder().detailType("X").build().toString();
    }
}
`
	ents, _ := runEventTypeDetect(t, "java", "NotAPublisher.java", src)
	requireNoEventTypeEntities(t, ents, "Java detailType with no putEvents sink co-located")
}

// TestEventType_JavaProducer_Precision_UnrelatedPublish reproduces review
// finding #1: a generic `.publish()` (Reactor/RxJava operator, custom bus)
// merely co-existing in method scope with an unrelated `.detailType("X")`
// must NOT associate them. The v1 detector keys ONLY on EventBridge
// `putEvents` with a co-located detailType, so this mints nothing.
func TestEventType_JavaProducer_Precision_UnrelatedPublish(t *testing.T) {
	src := `package producer;

public class AuditHandler {
    public void handle() {
        String label = builder.detailType("OrderPlaced").build();
        flux.publish();
    }
}
`
	ents, _ := runEventTypeDetect(t, "java", "AuditHandler.java", src)
	requireNoEventTypeEntities(t, ents, "unrelated reactor .publish() + stray detailType")
}

// TestEventType_JavaProducer_Precision_SinkInComment reproduces review
// finding #2 (comment span): a `putEvents(` inside a `//` line comment or a
// `/* */` block comment is not a real sink and must be stripped before
// matching.
func TestEventType_JavaProducer_Precision_SinkInComment(t *testing.T) {
	src := `package producer;

public class Commented {
    public void handle() {
        // eventBridgeClient.putEvents(PutEventsRequest.builder().entries(PutEventsRequestEntry.builder().detailType("OrderPlaced").build()).build());
        /* eventBridgeClient.putEvents(builder().detailType("OrderShipped").build()); */
        doNothing();
    }
}
`
	ents, _ := runEventTypeDetect(t, "java", "Commented.java", src)
	requireNoEventTypeEntities(t, ents, "putEvents inside comment span")
}

// TestEventType_JavaProducer_Precision_SinkInStringLiteral reproduces review
// finding #2 (string span): a `putEvents(` inside a string literal (e.g. a
// log line) is not a real sink and must be stripped before matching.
func TestEventType_JavaProducer_Precision_SinkInStringLiteral(t *testing.T) {
	src := `package producer;

public class Logged {
    public void handle() {
        logger.info("calling client.putEvents(request) with detailType OrderPlaced");
    }
}
`
	ents, _ := runEventTypeDetect(t, "java", "Logged.java", src)
	requireNoEventTypeEntities(t, ents, "putEvents inside string literal")
}

// TestEventType_JavaProducer_Precision_ClassScopeNoCaller reproduces review
// finding #3: a co-located putEvents+detailType in a static field
// initializer (NOT inside any indexed method) has an empty enclosing-method
// name, so fromID would be an empty structural-ref — emission must be
// rejected when the enclosing method name is empty.
func TestEventType_JavaProducer_Precision_ClassScopeNoCaller(t *testing.T) {
	src := `package producer;

import software.amazon.awssdk.services.eventbridge.model.PutEventsRequest;
import software.amazon.awssdk.services.eventbridge.model.PutEventsRequestEntry;

public class StaticInit {
    private static final Object RESULT = eventBridgeClient.putEvents(
        PutEventsRequest.builder()
            .entries(PutEventsRequestEntry.builder().detailType("OrderPlaced").build())
            .build());
}
`
	_, rels := runEventTypeDetect(t, "java", "StaticInit.java", src)
	for _, r := range rels {
		if r.Kind == "PUBLISHES_TO" && r.FromID == "" {
			t.Errorf("class-scope sink: expected NO edge with empty caller, got %+v", r)
		}
	}
}

// ---------------------------------------------------------------------------
// Consumer — Terraform aws_lambda_event_source_mapping FilterCriteria
// ---------------------------------------------------------------------------

func TestEventType_TerraformConsumer_FilterCriteria(t *testing.T) {
	src := `resource "aws_lambda_event_source_mapping" "orders_esm" {
  event_source_arn = aws_kinesis_stream.orders.arn
  function_name     = aws_lambda_function.order_consumer.arn

  filter_criteria {
    filter {
      pattern = jsonencode({
        data = {
          eventType = ["OrderPlaced", "OrderCancelled"]
        }
      })
    }
  }
}

resource "aws_lambda_function" "order_consumer" {
  function_name = "order-consumer"
  handler       = "handler.consume"
  runtime       = "go1.x"
}
`
	ents, rels := runEventTypeDetect(t, "hcl", "esm.tf", src)

	placedID := eventTypeID("OrderPlaced")
	cancelledID := eventTypeID("OrderCancelled")
	requireEventTypeEntity(t, ents, placedID, "Terraform consumer (OrderPlaced)")
	requireEventTypeEntity(t, ents, cancelledID, "Terraform consumer (OrderCancelled)")

	fromID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaFunctionID("order_consumer"))
	requireEdgeFromTo(t, rels, fromID, fmt.Sprintf("%s:%s", eventTypeKind, placedID), "SUBSCRIBES_TO", "Terraform consumer (OrderPlaced)")
	requireEdgeFromTo(t, rels, fromID, fmt.Sprintf("%s:%s", eventTypeKind, cancelledID), "SUBSCRIBES_TO", "Terraform consumer (OrderCancelled)")
}

// ---------------------------------------------------------------------------
// Consumer — serverless.yml stream.filterPatterns
// ---------------------------------------------------------------------------

func TestEventType_ServerlessYMLConsumer_FilterPatterns(t *testing.T) {
	src := `functions:
  orderConsumer:
    handler: handler.consume
    events:
      - stream:
          type: kinesis
          arn: arn:aws:kinesis:us-east-1:123456789012:stream/orders-stream
          filterPatterns: [{"data": {"eventType": ["OrderPlaced", "OrderCancelled"]}}]
`
	ents, rels := runEventTypeDetect(t, "yaml", "serverless.yml", src)

	placedID := eventTypeID("OrderPlaced")
	requireEventTypeEntity(t, ents, placedID, "serverless.yml consumer")

	fromID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaFunctionID("orderConsumer"))
	requireEdgeFromTo(t, rels, fromID, fmt.Sprintf("%s:%s", eventTypeKind, placedID), "SUBSCRIBES_TO", "serverless.yml consumer")
}

// ---------------------------------------------------------------------------
// Precision
// ---------------------------------------------------------------------------

// TestEventType_Precision_BareStringNotAtPublishSite verifies that a
// string that merely LOOKS like an eventType envelope, but never appears
// inside a recognized publish call's argument list, mints nothing.
func TestEventType_Precision_BareStringNotAtPublishSite(t *testing.T) {
	src := `package noise

const sample = ` + "`" + `{"eventType":"OrderPlaced"}` + "`" + `

func LogSample() {
	println(sample)
}
`
	ents, _ := runEventTypeDetect(t, "go", "noise.go", src)
	requireNoEventTypeEntities(t, ents, "bare string not at publish site")
}

// TestEventType_Precision_NonAllowlistedKey verifies that a key outside the
// allowlist (e.g. "name") at a genuine publish site does NOT mint a node.
func TestEventType_Precision_NonAllowlistedKey(t *testing.T) {
	src := `package producer

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
)

func PublishSomething(ctx context.Context, client *kinesis.Client, orderID string) error {
	_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName: aws.String("orders-stream"),
		Data:       []byte(fmt.Sprintf(` + "`" + `{"name":"OrderPlaced"}` + "`" + `)),
	})
	return err
}
`
	ents, _ := runEventTypeDetect(t, "go", "producer_bad.go", src)
	requireNoEventTypeEntities(t, ents, "non-allowlisted key at publish site")
}

// TestEventType_Precision_BareTypeKeyNotMinted verifies GAP-005 review
// FIX 2: bare `type` was dropped from the producer allowlist, so common
// non-event payloads carrying a `type` string at a generic publish/emit/send
// call-site mint NOTHING. Each of the three reviewer-reproduced snippets is
// asserted to produce zero SCOPE.EventType nodes.
func TestEventType_Precision_BareTypeKeyNotMinted(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"logger.emit", `export function log() { logger.emit({ type: "error", msg: "boom" }); }`},
		{"httpClient.send", `export function post() { httpClient.send({ type: "json", body: {} }); }`},
		{"styleSheet.emit", `export function style() { styleSheet.emit({ type: "css", rule: "a{}" }); }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ents, _ := runEventTypeDetect(t, "typescript", c.name+".ts", c.src)
			requireNoEventTypeEntities(t, ents, "bare type key ("+c.name+")")
		})
	}
}

// ---------------------------------------------------------------------------
// Consumer — SAM / CloudFormation template FilterCriteria (review FIX 1)
// ---------------------------------------------------------------------------

// TestEventType_SAMConsumer_InlineEventsFilterCriteria covers shape 1: an
// inline `Events:` stream event on an AWS::Serverless::Function carrying a
// FilterCriteria.Pattern data.eventType array → SUBSCRIBES_TO from the
// enclosing SAM function's logical id.
func TestEventType_SAMConsumer_InlineEventsFilterCriteria(t *testing.T) {
	src := `AWSTemplateFormatVersion: '2010-09-09'
Transform: AWS::Serverless-2016-10-31
Resources:
  OrderConsumer:
    Type: AWS::Serverless::Function
    Properties:
      Handler: handler.consume
      Runtime: go1.x
      Events:
        OrderStream:
          Type: Kinesis
          Properties:
            Stream: !ImportValue OrdersStreamArn
            FilterCriteria:
              Filters:
                - Pattern: '{ "data": { "eventType": ["OrderPlaced","OrderCancelled"] } }'
`
	ents, rels := runEventTypeDetect(t, "yaml", "template.yaml", src)

	placedID := eventTypeID("OrderPlaced")
	cancelledID := eventTypeID("OrderCancelled")
	requireEventTypeEntity(t, ents, placedID, "SAM inline (OrderPlaced)")
	requireEventTypeEntity(t, ents, cancelledID, "SAM inline (OrderCancelled)")

	fromID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaFunctionID("OrderConsumer"))
	requireEdgeFromTo(t, rels, fromID, fmt.Sprintf("%s:%s", eventTypeKind, placedID), "SUBSCRIBES_TO", "SAM inline (OrderPlaced)")
	requireEdgeFromTo(t, rels, fromID, fmt.Sprintf("%s:%s", eventTypeKind, cancelledID), "SUBSCRIBES_TO", "SAM inline (OrderCancelled)")
}

// TestEventType_CFNConsumer_StandaloneEventSourceMapping covers shape 2: a
// standalone AWS::Lambda::EventSourceMapping with a top-level FilterCriteria
// and FunctionName: !Ref <fn> → SUBSCRIBES_TO from the referenced function.
func TestEventType_CFNConsumer_StandaloneEventSourceMapping(t *testing.T) {
	src := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  OrderConsumer:
    Type: AWS::Serverless::Function
    Properties:
      Handler: handler.consume
      Runtime: go1.x
  OrdersEsm:
    Type: AWS::Lambda::EventSourceMapping
    Properties:
      FunctionName: !Ref OrderConsumer
      EventSourceArn: !ImportValue OrdersStreamArn
      FilterCriteria:
        Filters:
          - Pattern: '{ "data": { "eventType": ["OrderPlaced"] } }'
`
	ents, rels := runEventTypeDetect(t, "yaml", "template.yaml", src)

	placedID := eventTypeID("OrderPlaced")
	requireEventTypeEntity(t, ents, placedID, "CFN standalone ESM")

	fromID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaFunctionID("OrderConsumer"))
	requireEdgeFromTo(t, rels, fromID, fmt.Sprintf("%s:%s", eventTypeKind, placedID), "SUBSCRIBES_TO", "CFN standalone ESM")
}

// TestEventType_CFNConsumer_RealisticMultilineEscapedPattern covers the
// SHAPE SAM templates actually use in the wild: FilterCriteria.Pattern as a
// multi-line, double-quoted YAML block scalar with \"-escaped inner quotes
// (produced by `sam build`/hand-authored templates that keep the JSON
// readable across lines), NOT the single-line single-quoted compact form
// TestEventType_SAMConsumer_InlineEventsFilterCriteria exercises. Root-cause
// investigation (GAP-005 zero-yield-on-real-corpus) found the regex-based
// key/value extraction never handled escaped quotes or embedded newlines
// inside the Pattern string, so this realistic shape silently mints nothing.
func TestEventType_CFNConsumer_RealisticMultilineEscapedPattern(t *testing.T) {
	src := "AWSTemplateFormatVersion: 2010-09-09\n" +
		"Transform:\n" +
		"  - AWS::Serverless-2016-10-31\n" +
		"Resources:\n" +
		"  OrderValidations:\n" +
		"    Type: AWS::Serverless::Function\n" +
		"    Properties:\n" +
		"      Handler: node_modules/datadog-lambda-js/dist/handler.handler\n" +
		"      Events:\n" +
		"        Stream:\n" +
		"          Type: Kinesis\n" +
		"          Properties:\n" +
		"            Stream: !ImportValue SharedEventsStreamArn\n" +
		"            FilterCriteria:\n" +
		"              Filters:\n" +
		"                - Pattern: \"{ \\\"data\\\": {\n" +
		"                              \\\"eventType\\\": [\n" +
		"                                  \\\"OrderPlaced\\\",\n" +
		"                                  \\\"OrderCancelled\\\",\n" +
		"                                  \\\"OrderShipped\\\"\n" +
		"                                ]\n" +
		"                              }\n" +
		"                            }\"\n"

	ents, rels := runEventTypeDetect(t, "yaml", "template.yaml", src)

	fromID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaFunctionID("OrderValidations"))
	for _, name := range []string{"OrderPlaced", "OrderCancelled", "OrderShipped"} {
		id := eventTypeID(name)
		requireEventTypeEntity(t, ents, id, "SAM realistic multi-line escaped Pattern ("+name+")")
		requireEdgeFromTo(t, rels, fromID, fmt.Sprintf("%s:%s", eventTypeKind, id), "SUBSCRIBES_TO", "SAM realistic multi-line escaped Pattern ("+name+")")
	}
}

// TestEventType_CFNConsumer_NonTemplateYamlIgnored verifies a plain
// non-CFN yaml (no AWSTemplateFormatVersion / Transform / AWS::Serverless /
// ESM marker) mints nothing even if it happens to contain an eventType key.
func TestEventType_CFNConsumer_NonTemplateYamlIgnored(t *testing.T) {
	src := `config:
  routing:
    eventType: ["OrderPlaced"]
`
	ents, _ := runEventTypeDetect(t, "yaml", "config.yaml", src)
	requireNoEventTypeEntities(t, ents, "non-CFN yaml")
}

// ---------------------------------------------------------------------------
// End-to-end join: producer + consumer converge on the same node
// ---------------------------------------------------------------------------

// TestEventType_ProducerConsumerJoinThroughSameNode verifies the keystone
// property: a Go producer emitting "OrderPlaced" and a Terraform consumer
// filtering "OrderPlaced" both connect through the SAME event:type:OrderPlaced
// entity when their (entities, relationships) accumulate into one repo graph
// (mirrors how detector.go threads passArgs.Entities/Relationships across
// files in the same repo).
func TestEventType_ProducerConsumerJoinThroughSameNode(t *testing.T) {
	goSrc := `package producer

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
)

func PublishOrderPlaced(ctx context.Context, client *kinesis.Client, orderID string) error {
	_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName:   aws.String("orders-stream"),
		PartitionKey: aws.String(orderID),
		Data:         []byte(fmt.Sprintf(` + "`" + `{"eventType":"OrderPlaced","orderId":"%s"}` + "`" + `, orderID)),
	})
	return err
}
`
	tfSrc := `resource "aws_lambda_event_source_mapping" "orders_esm" {
  event_source_arn = aws_kinesis_stream.orders.arn
  function_name     = aws_lambda_function.order_consumer.arn

  filter_criteria {
    filter {
      pattern = jsonencode({
        data = {
          eventType = ["OrderPlaced"]
        }
      })
    }
  }
}
`
	res1 := applyEventTypeEdges(DetectorPassArgs{Lang: "go", Path: "producer.go", Content: []byte(goSrc)})
	res2 := applyEventTypeEdges(DetectorPassArgs{
		Lang: "hcl", Path: "esm.tf", Content: []byte(tfSrc),
		Entities: res1.Entities, Relationships: res1.Relationships,
	})

	id := eventTypeID("OrderPlaced")
	entID := fmt.Sprintf("%s:%s", eventTypeKind, id)

	// The join is by shared synthetic ID (Kind+Name), exactly like
	// SCOPE.EventBusEvent / SCOPE.MessageTopic: each per-file pass mints its
	// own entity record (per-file dedup only — cross-file entity merge is a
	// downstream resolve/import-channel-linker concern, out of this pass's
	// scope), but BOTH resolve to the identical verbatim key
	// "event:type:OrderPlaced", so the PUBLISHES_TO and SUBSCRIBES_TO edges
	// below converge on the SAME ToID — the actual join mechanism a
	// messaging_related-style seed traversal relies on.
	requireEventTypeEntity(t, res2.Entities, id, "join (producer+consumer both mint the node)")

	requireEdgeToEB(t, res2.Relationships, entID, "PUBLISHES_TO", "join (producer side)")
	requireEdgeToEB(t, res2.Relationships, entID, "SUBSCRIBES_TO", "join (consumer side)")
}

// TestEventType_ProducerSAMConsumerJoinThroughSameNode is the review FIX 1
// join: a Go producer emitting "OrderPlaced" and a SAM/CFN consumer filtering
// "OrderPlaced" both converge on the SAME event:type:OrderPlaced node.
func TestEventType_ProducerSAMConsumerJoinThroughSameNode(t *testing.T) {
	goSrc := `package producer

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
)

func PublishOrderPlaced(ctx context.Context, client *kinesis.Client, orderID string) error {
	_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName:   aws.String("orders-stream"),
		PartitionKey: aws.String(orderID),
		Data:         []byte(fmt.Sprintf(` + "`" + `{"eventType":"OrderPlaced","orderId":"%s"}` + "`" + `, orderID)),
	})
	return err
}
`
	samSrc := `AWSTemplateFormatVersion: '2010-09-09'
Transform: AWS::Serverless-2016-10-31
Resources:
  OrderConsumer:
    Type: AWS::Serverless::Function
    Properties:
      Handler: handler.consume
      Events:
        OrderStream:
          Type: Kinesis
          Properties:
            Stream: !ImportValue OrdersStreamArn
            FilterCriteria:
              Filters:
                - Pattern: '{ "data": { "eventType": ["OrderPlaced"] } }'
`
	res1 := applyEventTypeEdges(DetectorPassArgs{Lang: "go", Path: "producer.go", Content: []byte(goSrc)})
	res2 := applyEventTypeEdges(DetectorPassArgs{
		Lang: "yaml", Path: "template.yaml", Content: []byte(samSrc),
		Entities: res1.Entities, Relationships: res1.Relationships,
	})

	id := eventTypeID("OrderPlaced")
	entID := fmt.Sprintf("%s:%s", eventTypeKind, id)

	requireEventTypeEntity(t, res2.Entities, id, "SAM join")
	requireEdgeToEB(t, res2.Relationships, entID, "PUBLISHES_TO", "SAM join (producer side)")
	requireEdgeToEB(t, res2.Relationships, entID, "SUBSCRIBES_TO", "SAM join (consumer side)")
}

// ---------------------------------------------------------------------------
// Fan-out cap
// ---------------------------------------------------------------------------

// TestEventType_FanOutCap verifies a hot event string cannot explode edges:
// many distinct producer functions publishing the SAME event-type string in
// one file are capped at eventTypeEmissionCapPerName PUBLISHES_TO edges.
func TestEventType_FanOutCap(t *testing.T) {
	old := eventTypeEmissionCapPerName
	eventTypeEmissionCapPerName = 3
	defer func() { eventTypeEmissionCapPerName = old }()

	src := "package hot\n\nimport (\n\t\"context\"\n\n\t\"github.com/aws/aws-sdk-go-v2/aws\"\n\t\"github.com/aws/aws-sdk-go-v2/service/kinesis\"\n)\n\n"
	for i := 0; i < 10; i++ {
		src += fmt.Sprintf(`func PublishHotEvent%d(ctx context.Context, client *kinesis.Client) error {
	_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName: aws.String("hot-stream"),
		Data:       []byte(`+"`"+`{"eventType":"HotEvent"}`+"`"+`),
	})
	return err
}

`, i)
	}

	_, rels := runEventTypeDetect(t, "go", "hot.go", src)

	toID := fmt.Sprintf("%s:%s", eventTypeKind, eventTypeID("HotEvent"))
	count := 0
	for _, r := range rels {
		if r.Kind == "PUBLISHES_TO" && r.ToID == toID {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("expected fan-out cap to limit PUBLISHES_TO edges to 3, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Producer — Go — event-store write (GAP-015 RC1)
// ---------------------------------------------------------------------------

// TestEventType_GoProducer_EventStoreConstructorArg covers the dominant
// event-store-write producer shape: a domain event is built via a
// `New*Event*(...)` constructor whose event-type is a POSITIONAL string
// literal (not a `key: "value"` field), then persisted via a semantic
// event-store write call (`store.WriteEvent(...)`), NOT a broker publish.
func TestEventType_GoProducer_EventStoreConstructorArg(t *testing.T) {
	src := `package producer

import "context"

func RecordOrderSettled(ctx context.Context, store *EventStore, id string, payload []byte) error {
	evt := NewOrderEvent(id, "OrderSettled", payload)
	return store.WriteEvent(ctx, evt)
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer_store.go", src)

	id := eventTypeID("OrderSettled")
	requireEventTypeEntity(t, ents, id, "Go event-store constructor-arg producer")

	fromID := extractor.BuildOperationStructuralRef("go", "producer_store.go", "RecordOrderSettled")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go event-store constructor-arg producer")

	var edgeDetection string
	for _, r := range rels {
		if r.FromID == fromID && r.ToID == toID {
			edgeDetection = r.Properties["detection"]
		}
	}
	if edgeDetection != "event-store-constructor-arg" {
		t.Errorf("expected detection=event-store-constructor-arg, got %q", edgeDetection)
	}
}

// TestEventType_GoProducer_EventStoreConstructorArg_PublishEvent covers a
// second constructor/sink verb pair: `Make*Event*(...)` + `PublishEvent(...)`.
func TestEventType_GoProducer_EventStoreConstructorArg_PublishEvent(t *testing.T) {
	src := `package producer

import "context"

func RecordOrderPlaced(ctx context.Context, p []byte) error {
	evt := MakeBillingEvent(ctx, "OrderPlaced", p)
	return PublishEvent(evt)
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer_store2.go", src)

	id := eventTypeID("OrderPlaced")
	requireEventTypeEntity(t, ents, id, "Go event-store constructor-arg producer (PublishEvent)")

	fromID := extractor.BuildOperationStructuralRef("go", "producer_store2.go", "RecordOrderPlaced")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go event-store constructor-arg producer (PublishEvent)")
}

// TestEventType_GoProducer_EventStoreConstructorArg_NoSink_NoEdge is the
// CRITICAL negative guard: a constructor literal with NO event-store write
// sink anywhere in the enclosing function must mint NOTHING.
func TestEventType_GoProducer_EventStoreConstructorArg_NoSink_NoEdge(t *testing.T) {
	src := `package producer

func BuildOrderEvent(id string) *OrderEvent {
	evt := NewOrderEvent(id, "OrderPlaced")
	return evt
}
`
	ents, _ := runEventTypeDetect(t, "go", "producer_store3.go", src)
	requireNoEventTypeEntities(t, ents, "Go event-store constructor literal with no write sink")
}

// TestEventType_GoProducer_EventStoreConstructorArg_RawPutItem_NoEdge is the
// CRITICAL negative guard: raw DynamoDB PutItem is NOT a semantic event-store
// write sink — it is far too generic (any DynamoDB write matches it) — so a
// constructor literal in scope with only a raw PutItem call must mint
// NOTHING.
func TestEventType_GoProducer_EventStoreConstructorArg_RawPutItem_NoEdge(t *testing.T) {
	src := `package producer

import "context"

func RecordOrderPlaced(ctx context.Context, dynamo *DynamoClient, id string) error {
	evt := NewOrderEvent(id, "OrderPlaced")
	input := toPutItemInput(evt)
	_, err := dynamo.PutItem(ctx, input)
	return err
}
`
	ents, _ := runEventTypeDetect(t, "go", "producer_store4.go", src)
	requireNoEventTypeEntities(t, ents, "Go event-store constructor literal with only raw PutItem sink")
}

// TestEventType_GoProducer_EventStoreConstructorArg_Formatter_NoEdge is the
// precision guard for a `%`-format-verb/template literal in the constructor
// argument — never a stable wire contract, so no garbage node is minted even
// though a real write sink is present.
func TestEventType_GoProducer_EventStoreConstructorArg_Formatter_NoEdge(t *testing.T) {
	src := `package producer

import (
	"context"
	"fmt"
)

func RecordOrder(ctx context.Context, store *EventStore, id string) error {
	evt := NewOrderEvent(id, fmt.Sprintf("order-%s-placed", id))
	return store.WriteEvent(ctx, evt)
}
`
	ents, _ := runEventTypeDetect(t, "go", "producer_store5.go", src)
	requireNoEventTypeEntities(t, ents, "Go event-store constructor with formatter-templated literal")
}

// TestEventType_GoProducer_EventStoreConstructorArg_MultiLiteralDrop is the
// CRITICAL precision guard (review surface 6a/6c): when the constructor's
// argument list carries MORE THAN ONE string literal, the FIRST-literal
// heuristic cannot tell the event-name argument from a source/topic/region
// argument or a nested-wrapper literal, so the whole binding is DROPPED
// (ambiguous → no guess). Covers `NewOrderEvent(ctx, "orders-svc",
// "OrderSettled", ...)` (source arg first) and the nested-wrapper
// `NewOrderEvent(id, aws.String("region-us"), "OrderPlaced")` shape.
func TestEventType_GoProducer_EventStoreConstructorArg_MultiLiteralDrop(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "source-arg-before-event-name",
			src: `package producer

import "context"

func RecordOrderSettled(ctx context.Context, store *EventStore, id string, payload []byte) error {
	evt := NewOrderEvent(ctx, "orders-svc", "OrderSettled", payload)
	return store.WriteEvent(ctx, evt)
}
`,
		},
		{
			name: "nested-wrapper-literal",
			src: `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func RecordOrderPlaced(ctx context.Context, store *EventStore, id string) error {
	evt := NewOrderEvent(id, aws.String("region-us"), "OrderPlaced")
	return store.WriteEvent(ctx, evt)
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents, _ := runEventTypeDetect(t, "go", "producer_multi.go", tc.src)
			requireNoEventTypeEntities(t, ents, "Go event-store constructor with >1 string literal (ambiguous drop)")
		})
	}
}

// TestEventType_GoProducer_EventStoreConstructorArg_IDLiteralDrop is the
// precision guard for the event-sourcing idiom where the event TYPE lives in
// the constructor NAME and the first (only) string literal is an
// aggregate/ID (`NewOrderPlacedEvent("order-123", 42)`). An ID-shaped value
// (lowercase-kebab/snake-with-digits, UUID-ish) is rejected rather than
// minted as a poison node.
func TestEventType_GoProducer_EventStoreConstructorArg_IDLiteralDrop(t *testing.T) {
	src := `package producer

import "context"

func RecordOrderPlaced(ctx context.Context, store *EventStore) error {
	evt := NewOrderPlacedEvent("order-123", 42)
	return store.WriteEvent(ctx, evt)
}
`
	ents, _ := runEventTypeDetect(t, "go", "producer_idlit.go", src)
	requireNoEventTypeEntities(t, ents, "Go event-store constructor with ID-shaped literal (rejected)")
}

// TestEventType_GoProducer_EventStore_EventuallyNotSink is the CRITICAL sink
// precision guard (review surface 1): `*Eventually(` calls (SaveEventually,
// StoreEventually, WriteEventually, ...) share the `Event` substring but are
// database consistency-mode helpers, NOT semantic event-store writes. The
// sink regex must treat `Event` as a token, not a prefix of `Eventually`, so
// none of these fire even with a valid constructor literal in scope.
func TestEventType_GoProducer_EventStore_EventuallyNotSink(t *testing.T) {
	verbs := []string{
		"SaveEventually", "StoreEventually", "WriteEventually",
		"PublishEventually", "AppendEventually", "StoreEventuallyConsistent",
	}
	for _, verb := range verbs {
		t.Run(verb, func(t *testing.T) {
			src := `package producer

import "context"

func RecordOrder(ctx context.Context, db *DB, orderID string) error {
	evt := NewOrderEvent(orderID, "OrderPlaced")
	return db.` + verb + `(ctx, evt)
}
`
			ents, _ := runEventTypeDetect(t, "go", "producer_eventually.go", src)
			requireNoEventTypeEntities(t, ents, "Go *Eventually call must not be an event-store sink ("+verb+")")
		})
	}
}

// TestEventType_GoProducer_EventStore_RecordEmitTelemetryDropped documents the
// verb-set decision: `RecordEvent`/`EmitEvent` are dropped from the sink verb
// set because they overwhelmingly denote ANALYTICS/telemetry emission, not a
// domain event-store write contract. Even with a valid constructor literal in
// scope, these must not mint a producer edge.
func TestEventType_GoProducer_EventStore_RecordEmitTelemetryDropped(t *testing.T) {
	for _, verb := range []string{"RecordEvent", "EmitEvent"} {
		t.Run(verb, func(t *testing.T) {
			src := `package producer

func RecordOrder(analytics *Analytics, orderID string) error {
	evt := NewOrderEvent(orderID, "OrderPlaced")
	return analytics.` + verb + `(evt)
}
`
			ents, _ := runEventTypeDetect(t, "go", "producer_telemetry.go", src)
			requireNoEventTypeEntities(t, ents, "Go telemetry "+verb+" must not be an event-store sink")
		})
	}
}

// TestEventType_GoProducer_EventStore_EventSuffixVariantsStillMatch guards
// against over-tightening the sink regex: the legitimate `WriteEvents(` /
// `WriteEventBatch(` variants must STILL match after the `Eventually`
// exclusion.
func TestEventType_GoProducer_EventStore_EventSuffixVariantsStillMatch(t *testing.T) {
	for _, verb := range []string{"WriteEvents", "WriteEventBatch", "PublishEvents"} {
		t.Run(verb, func(t *testing.T) {
			src := `package producer

import "context"

func RecordOrder(ctx context.Context, store *EventStore, orderID string) error {
	evt := NewOrderEvent(orderID, "OrderSettled")
	return store.` + verb + `(ctx, evt)
}
`
			ents, rels := runEventTypeDetect(t, "go", "producer_variant.go", src)
			id := eventTypeID("OrderSettled")
			requireEventTypeEntity(t, ents, id, "Go event-store "+verb+" variant sink")
			fromID := extractor.BuildOperationStructuralRef("go", "producer_variant.go", "RecordOrder")
			toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
			requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go event-store "+verb+" variant sink")
		})
	}
}

// ---------------------------------------------------------------------------
// Producer — Go — RC-B-1: DynamoDB-Streams reserved-value denylist
// ---------------------------------------------------------------------------

// TestEventType_GoProducer_DynamoDBStreamsReservedValue_NotMinted is the
// red->green regression for defect RC-B-1: DynamoDB Streams stamps a fixed
// `EventName` field (one of INSERT/MODIFY/REMOVE) on every stream record,
// and "eventName" is a legitimate allowlisted event-envelope key. A stream
// handler that inspects `rec.EventName` in the SAME function scope as a
// real publish call (the function-scope-recall widening covered by
// TestEventType_GoProducer_FunctionScopeStructField) previously minted a
// bogus event:type:INSERT/MODIFY/REMOVE node purely from that plumbing
// field. None of the three reserved values may ever mint a node, checked
// case-insensitively; verified per-value with t.Run subtests.
func TestEventType_GoProducer_DynamoDBStreamsReservedValue_NotMinted(t *testing.T) {
	for _, reserved := range []string{"INSERT", "MODIFY", "REMOVE", "insert", "Modify"} {
		t.Run(reserved, func(t *testing.T) {
			src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
)

type StreamRecord struct {
	EventName string
}

func HandleStreamRecord(ctx context.Context, client *kinesis.Client, orderID string) error {
	rec := StreamRecord{
		EventName: "` + reserved + `",
	}
	_ = rec
	_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName:   aws.String("orders-stream"),
		PartitionKey: aws.String(orderID),
	})
	return err
}
`
			ents, _ := runEventTypeDetect(t, "go", "stream_handler.go", src)
			for _, want := range []string{"INSERT", "MODIFY", "REMOVE"} {
				id := eventTypeID(want)
				if eventTypeEntityByID(ents, id) != nil {
					t.Errorf("EventName:%q must never mint %s; got %v", reserved, id, entNames(ents))
				}
			}
		})
	}
}

// TestEventType_GoProducer_EventNameSyntheticDomainName_StillMinted is the
// companion positive guard: the RC-B-1 denylist must be scoped to the exact
// DynamoDB-Streams reserved vocabulary — a legitimate synthetic domain event
// name carried in the SAME "eventName" key shape must still mint normally.
func TestEventType_GoProducer_EventNameSyntheticDomainName_StillMinted(t *testing.T) {
	src := `package producer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
)

type OrderEvent struct {
	EventName string
}

func PublishOrderPlaced(ctx context.Context, client *kinesis.Client, orderID string) error {
	evt := OrderEvent{
		EventName: "OrderPlaced",
	}
	_ = evt
	_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName:   aws.String("orders-stream"),
		PartitionKey: aws.String(orderID),
	})
	return err
}
`
	ents, rels := runEventTypeDetect(t, "go", "producer_eventname.go", src)

	id := eventTypeID("OrderPlaced")
	requireEventTypeEntity(t, ents, id, "Go producer (legitimate eventName)")

	fromID := extractor.BuildOperationStructuralRef("go", "producer_eventname.go", "PublishOrderPlaced")
	toID := fmt.Sprintf("%s:%s", eventTypeKind, id)
	requireEdgeFromTo(t, rels, fromID, toID, "PUBLISHES_TO", "Go producer (legitimate eventName)")
}

// ---------------------------------------------------------------------------
// No-op guards
// ---------------------------------------------------------------------------

func TestEventType_NoOp_EmptyContent(t *testing.T) {
	res := applyEventTypeEdges(DetectorPassArgs{Lang: "go", Path: "empty.go", Content: nil})
	if len(res.Entities) != 0 || len(res.Relationships) != 0 {
		t.Errorf("expected no-op on empty content, got %d entities / %d relationships", len(res.Entities), len(res.Relationships))
	}
}

func TestEventType_NoOp_UnsupportedLanguage(t *testing.T) {
	src := `class Producer
  def publish
    { eventType: "OrderPlaced" }
  end
end
`
	ents, _ := runEventTypeDetect(t, "ruby", "producer.rb", src)
	requireNoEventTypeEntities(t, ents, "unsupported language (ruby)")
}
