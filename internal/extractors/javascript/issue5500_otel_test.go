// Package javascript_test — issue #5500 (epic #5479): JS/TS OpenTelemetry
// span-creation coverage on parity with Python. Verifies the import/receiver
// attribution gate (so unrelated `.startSpan` is not matched) and the extended
// idioms (inline trace.getTracer(...).startSpan, @vercel/otel registerOTel,
// manual context.with(trace.setSpan(...)) scopes).
package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// TestOTel_JS_StartActiveSpan_WithImport verifies the headline fixture: a
// startActiveSpan inside a function in an OTEL-imported file → INSTRUMENTS edge
// carrying the span name.
func TestOTel_JS_StartActiveSpan_WithImport(t *testing.T) {
	src := `
import { trace } from '@opentelemetry/api';
const tracer = trace.getTracer('orders');

async function handleOrder(req) {
  return tracer.startActiveSpan('handleOrder', async (span) => {
    span.end();
  });
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsSpanEdge(ents, "handleOrder", "span:handleOrder")
	if r == nil {
		t.Fatal("INSTRUMENTS edge handleOrder → span:handleOrder not found")
	}
	if r.Properties["span_name"] != "handleOrder" {
		t.Errorf("span_name=%q, want handleOrder", r.Properties["span_name"])
	}
	if r.Properties["api"] != "startActiveSpan" {
		t.Errorf("api=%q, want startActiveSpan", r.Properties["api"])
	}
}

// TestOTel_JS_InlineGetTracer verifies the inline chain
// trace.getTracer(...).startSpan(...) is matched (receiver mentions getTracer).
func TestOTel_JS_InlineGetTracer(t *testing.T) {
	src := `
import { trace } from '@opentelemetry/api';

function ship() {
  const span = trace.getTracer('svc').startSpan('ship.it');
  span.end();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if jsSpanEdge(ents, "ship", "span:ship.it") == nil {
		t.Fatal("INSTRUMENTS edge ship → span:ship.it not found")
	}
}

// TestOTel_JS_NoImport_NonTracerReceiver_NoEdge is the attribution-gate
// negative: a `.startSpan` on a non-tracer receiver with NO @opentelemetry
// import produces no edge.
func TestOTel_JS_NoImport_NonTracerReceiver_NoEdge(t *testing.T) {
	src := `
import { DatePicker } from 'some-ui-lib';

function pick() {
  const r = datePicker.startSpan('2020-01-01');
  return r;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	e := findByNameRel(ents, "pick")
	if e == nil {
		t.Fatal("entity pick not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "INSTRUMENTS" {
			t.Errorf("unexpected INSTRUMENTS edge on non-OTEL .startSpan: → %s", r.ToID)
		}
	}
}

// TestOTel_JS_RegisterOTel verifies the @vercel/otel registerOTel('app') setup
// site emits an INSTRUMENTS edge keyed on the literal service name.
func TestOTel_JS_RegisterOTel(t *testing.T) {
	src := `
import { registerOTel } from '@vercel/otel';

export function register() {
  registerOTel('my-app');
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsSpanEdge(ents, "register", "span:my-app")
	if r == nil {
		if e := findByNameRel(ents, "register"); e != nil {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge register → span:my-app not found")
	}
	if r.Properties["api"] != "registerOTel" {
		t.Errorf("api=%q, want registerOTel", r.Properties["api"])
	}
}

// TestOTel_JS_ContextWithScope verifies a manual context.with(trace.setSpan(...))
// span scope emits a dynamic INSTRUMENTS edge keyed on the enclosing function.
func TestOTel_JS_ContextWithScope(t *testing.T) {
	src := `
import { context, trace } from '@opentelemetry/api';

function doWork(span) {
  return context.with(trace.setSpan(context.active(), span), () => {
    return compute();
  });
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsSpanEdge(ents, "doWork", "span:doWork")
	if r == nil {
		if e := findByNameRel(ents, "doWork"); e != nil {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge doWork → span:doWork not found")
	}
	if r.Properties["dynamic"] != "true" {
		t.Errorf("dynamic=%q, want true", r.Properties["dynamic"])
	}
	if r.Properties["api"] != "context.with" {
		t.Errorf("api=%q, want context.with", r.Properties["api"])
	}
}

// TestOTel_JS_ImportGate_RescuesPlainReceiver: with the OTEL import present, a
// span call on a non-conventional receiver name still matches (import arm of the
// gate). Without the import it would be dropped (covered above).
func TestOTel_JS_ImportGate_RescuesPlainReceiver(t *testing.T) {
	src := `
import { trace } from '@opentelemetry/api';

function op() {
  const span = t.startSpan('op.name');
  span.end();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if jsSpanEdge(ents, "op", "span:op.name") == nil {
		t.Fatal("INSTRUMENTS edge op → span:op.name not found (import arm of gate)")
	}
}

var _ = types.RelationshipKindInstruments
