package python_test

import "testing"

// #4789 — imperative Django signal wiring registered in AppConfig.ready():
// `post_save.connect(my_receiver, sender=Foo)`. Before this fix only the
// `@receiver` decorator form emitted a HANDLES_SIGNAL edge; the imperative
// `.connect()` form produced none.

func findHandler(res []extractResult, name string) *extractResult {
	for i := range res {
		if res[i].Name == name {
			return &res[i]
		}
	}
	return nil
}

// RED→GREEN: a handler wired via `post_save.connect(handler, sender=Foo)` inside
// AppConfig.ready() gets a HANDLES_SIGNAL edge to Class:Foo with signal_type and
// di_role=signal_handler.
func TestDjango_SignalConnect_ReadyMethod(t *testing.T) {
	src := `from django.apps import AppConfig
from django.db.models.signals import post_save

class FooConfig(AppConfig):
    def ready(self):
        post_save.connect(my_handler, sender=Foo)
`
	res := extract(t, "python_django", src)
	h := findHandler(res, "my_handler")
	if h == nil {
		t.Fatalf("expected signal-handler entity 'my_handler'; got %+v", res)
	}
	if h.Props["signal_type"] != "post_save" {
		t.Errorf("signal_type=%q, want post_save", h.Props["signal_type"])
	}
	if h.Props["di_role"] != "signal_handler" {
		t.Errorf("di_role=%q, want signal_handler", h.Props["di_role"])
	}
	if h.Props["sender"] != "Foo" {
		t.Errorf("sender=%q, want Foo", h.Props["sender"])
	}
	found := false
	for _, r := range h.Rels {
		if r.Kind == "HANDLES_SIGNAL" && r.ToID == "Class:Foo" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected HANDLES_SIGNAL → Class:Foo; got rels %+v", h.Rels)
	}
}

// Dotted signal form `signals.post_delete.connect(other_handler, sender=Bar)`
// resolves the signal leaf and the sender model.
func TestDjango_SignalConnect_DottedSignal(t *testing.T) {
	src := `from django.db.models import signals

def wire():
    signals.post_delete.connect(other_handler, sender=Bar)
`
	res := extract(t, "python_django", src)
	h := findHandler(res, "other_handler")
	if h == nil {
		t.Fatalf("expected handler 'other_handler'; got %+v", res)
	}
	if h.Props["signal_type"] != "post_delete" {
		t.Errorf("signal_type=%q, want post_delete", h.Props["signal_type"])
	}
	found := false
	for _, r := range h.Rels {
		if r.Kind == "HANDLES_SIGNAL" && r.ToID == "Class:Bar" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected HANDLES_SIGNAL → Class:Bar; got rels %+v", h.Rels)
	}
}

// Bare `<signal>.connect(<receiver>)` (no sender) wires the handler to the
// signal itself.
func TestDjango_SignalConnect_NoSender(t *testing.T) {
	src := `from django.core.signals import request_started

def setup():
    request_started.connect(on_start)
`
	res := extract(t, "python_django", src)
	h := findHandler(res, "on_start")
	if h == nil {
		t.Fatalf("expected handler 'on_start'; got %+v", res)
	}
	if _, ok := h.Props["sender"]; ok {
		t.Errorf("expected no sender prop, got %q", h.Props["sender"])
	}
	found := false
	for _, r := range h.Rels {
		if r.Kind == "HANDLES_SIGNAL" && r.ToID == "request_started" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected HANDLES_SIGNAL → request_started; got rels %+v", h.Rels)
	}
}

// Gate: a non-signal `.connect()` (e.g. a DB connection or socket) must NOT be
// wired as a signal handler.
func TestDjango_SignalConnect_NonSignalIgnored(t *testing.T) {
	src := `def setup(conn):
    conn.connect(on_message, sender=Thing)
`
	res := extract(t, "python_django", src)
	if h := findHandler(res, "on_message"); h != nil {
		t.Errorf("non-signal .connect() must not be wired; got %+v", h)
	}
}
