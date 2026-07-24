// Tests for the #1617 Django custom-signal pub/sub and Celery cross-file
// dispatch synthesis passes.
package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

func relsOfKind(rels []types.RelationshipRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Celery cross-file dispatch
// ---------------------------------------------------------------------------

func TestApplyCeleryDispatchEdges_CrossFile(t *testing.T) {
	files := map[string]string{
		"core/tasks/notify.py": `from celery import shared_task

@shared_task
def send_notifications(user_id):
    pass
`,
		"core/views/order.py": `from core.tasks.notify import send_notifications

def create_order(request):
    send_notifications.delay(request.user.id)
`,
		"core/services/billing.py": `from core.tasks.notify import send_notifications

def charge(invoice):
    send_notifications.apply_async(args=[invoice.id])
`,
	}
	reader := adminFileMapReader(files)
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}

	rels := ApplyCeleryDispatchEdges(paths, reader)
	calls := relsOfKind(rels, "CALLS")
	if len(calls) != 2 {
		t.Fatalf("expected 2 cross-file CALLS dispatch edges, got %d: %v", len(calls), rels)
	}
	for _, r := range calls {
		if r.ToID != "Function:send_notifications" {
			t.Errorf("dispatch edge ToID = %q, want Function:send_notifications", r.ToID)
		}
		if r.Properties.Get("pattern_type") != "celery_dispatch_synthesis" {
			t.Errorf("missing celery_dispatch_synthesis tag: %v", r.Properties)
		}
	}
	callers := map[string]bool{}
	for _, r := range calls {
		callers[r.FromID] = true
	}
	if !callers["SCOPE.Operation:create_order"] || !callers["SCOPE.Operation:charge"] {
		t.Errorf("expected callers create_order + charge, got %v", callers)
	}
}

func TestApplyCeleryDispatchEdges_NoSelfDispatch(t *testing.T) {
	files := map[string]string{
		"core/tasks/chain.py": `from celery import shared_task

@shared_task
def step_one(x):
    # self-dispatch inside its own def must not produce an edge
    step_one.delay(x + 1)
`,
	}
	reader := adminFileMapReader(files)
	rels := ApplyCeleryDispatchEdges([]string{"core/tasks/chain.py"}, reader)
	if n := len(relsOfKind(rels, "CALLS")); n != 0 {
		t.Fatalf("expected 0 self-dispatch edges, got %d: %v", n, rels)
	}
}

func TestApplyCeleryDispatchEdges_IgnoresUnknownTask(t *testing.T) {
	files := map[string]string{
		"core/views/x.py": `def handler(request):
    some_random_obj.delay(1)
`,
	}
	reader := adminFileMapReader(files)
	rels := ApplyCeleryDispatchEdges([]string{"core/views/x.py"}, reader)
	if len(rels) != 0 {
		t.Fatalf("expected no edges for unknown dispatch target, got %v", rels)
	}
}

// ---------------------------------------------------------------------------
// Django custom-signal pub/sub
// ---------------------------------------------------------------------------

func TestApplyDjangoSignalPubSub_PublisherAndSubscriber(t *testing.T) {
	files := map[string]string{
		"core/signals/defs.py": `from django.dispatch import Signal, receiver

inspection_pre_update = Signal()
inspection_post_update = Signal()

@receiver(inspection_pre_update)
def handle_inspection_pre_update(sender=None, request=None, data=None, **kwargs):
    pass

@receiver(inspection_post_update)
def handle_inspection_post_update(sender=None, request=None, data=None, **kwargs):
    pass
`,
		"core/views/inspection.py": `from core.signals.defs import inspection_pre_update, inspection_post_update

def update(self, request):
    inspection_pre_update.send(sender=self.__class__, request=request, data={})
    inspection_post_update.send(sender=self.__class__, request=request, data={})
`,
	}
	reader := adminFileMapReader(files)
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}

	ents, rels := ApplyDjangoSignalPubSub(paths, reader)

	// Two topics.
	topicCount := 0
	for _, e := range ents {
		if e.Kind == signalTopicKind && e.Properties["framework"] == "django_signals" {
			topicCount++
		}
	}
	if topicCount != 2 {
		t.Fatalf("expected 2 signal topics, got %d: %v", topicCount, ents)
	}

	subs := relsOfKind(rels, "SUBSCRIBES_TO")
	pubs := relsOfKind(rels, "PUBLISHES_TO")
	if len(subs) != 2 {
		t.Errorf("expected 2 SUBSCRIBES_TO (handlers), got %d: %v", len(subs), subs)
	}
	if len(pubs) != 2 {
		t.Errorf("expected 2 PUBLISHES_TO (send sites), got %d: %v", len(pubs), pubs)
	}

	// Verify a specific handler→topic subscription.
	foundSub := false
	for _, r := range subs {
		if r.FromID == "SCOPE.Operation:handle_inspection_pre_update" &&
			r.ToID == signalTopicKind+":inspection_pre_update" {
			foundSub = true
		}
	}
	if !foundSub {
		t.Errorf("missing handler subscription edge; subs=%v", subs)
	}
	// Verify publisher edge from the sending function.
	foundPub := false
	for _, r := range pubs {
		if r.FromID == "SCOPE.Operation:update" {
			foundPub = true
		}
	}
	if !foundPub {
		t.Errorf("missing publisher edge from update(); pubs=%v", pubs)
	}

	// Regression guard for #1649: the topic entity's pre-stamp ID must match
	// the form used on edge endpoints, AND the suffix after the first ':'
	// must equal Entity.Name. resolve.splitStub splits on ':' and then looks
	// up byKind[kind][name] / byName[name] — any mismatch leaves the ToID as
	// a stub on-disk forever (PUBLISHES_TO/SUBSCRIBES_TO orphaned from their
	// topic at runtime, even though graph.fb persistence is correct).
	for _, e := range ents {
		if e.Kind != signalTopicKind {
			continue
		}
		want := signalTopicKind + ":" + e.Name
		if e.ID != want {
			t.Errorf("topic entity ID=%q want=%q (must round-trip splitStub→Name)", e.ID, want)
		}
	}
	for _, r := range append(append([]types.RelationshipRecord(nil), subs...), pubs...) {
		// ToID must be "<kind>:<bareName>" — never include the historical
		// "django_signal:" disambiguator in the resolver-visible segment.
		if !strings.HasPrefix(r.ToID, signalTopicKind+":") {
			t.Errorf("edge ToID=%q missing %s: prefix", r.ToID, signalTopicKind)
			continue
		}
		suffix := r.ToID[len(signalTopicKind)+1:]
		if strings.Contains(suffix, ":") {
			t.Errorf("edge ToID=%q has extra ':' in name segment — resolver will fail to bind to topic Entity.Name (#1649)", r.ToID)
		}
	}
}

func TestApplyDjangoSignalPubSub_IgnoresBuiltinSignals(t *testing.T) {
	// post_save is a built-in signal (not defined via Signal()) — it must NOT
	// become a topic; its model linkage is HANDLES_SIGNAL elsewhere.
	files := map[string]string{
		"core/signals/replicate.py": `from django.db.models.signals import post_save
from django.dispatch import receiver

@receiver(post_save, sender=Group)
def replicate(sender, instance, **kwargs):
    pass
`,
	}
	reader := adminFileMapReader(files)
	ents, rels := ApplyDjangoSignalPubSub([]string{"core/signals/replicate.py"}, reader)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("built-in signals must not produce topics/edges, got ents=%v rels=%v", ents, rels)
	}
}
