package links

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEffectPropagation_HandlerServiceRepoChain exercises the canonical
// substrate chain from the issue: handler → service → repo. The repo
// owns a direct db_write sink; effects should propagate up to the handler
// with hop-decayed confidence.
func TestEffectPropagation_HandlerServiceRepoChain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "repo.py", `
class UserRepo:
    def insert(self, u):
        u.save()
`)
	writeFile(t, root, "svc.py", `
class UserService:
    def __init__(self, r):
        self.r = r
    def register(self, u):
        return self.r.insert(u)
`)
	writeFile(t, root, "handler.py", `
def handle(req):
    s = UserService(UserRepo())
    return s.register(req)
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "h1", Name: "handle", Kind: "SCOPE.Function", SourceFile: "handler.py"},
			{ID: "s1", Name: "register", Kind: "SCOPE.Function", SourceFile: "svc.py"},
			{ID: "r1", Name: "insert", Kind: "SCOPE.Function", SourceFile: "repo.py"},
		},
		Edges: []edgeRef{
			{FromID: "h1", ToID: "s1", Kind: "CALLS"},
			{FromID: "s1", ToID: "r1", Kind: "CALLS"},
		},
	}}
	paths := Paths{Links: filepath.Join(root, "out", "links.json")}
	res, err := runEffectPropagationPass(graphs, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.LinksAdded == 0 {
		t.Fatal("expected at least one entity stamped with effects")
	}
	// Look up the handler — should carry db_write transitively.
	h := graphs[0].Entities[0]
	effs := h.Properties[EffectPropertyKeyList]
	if !strings.Contains(effs, "db_write") {
		t.Fatalf("handler effects=%q; want db_write", effs)
	}
	if got := h.Properties[EffectPropertyKeySource]; got != "transitive" {
		t.Errorf("handler effect_source=%q; want transitive", got)
	}
	// Repo should be the direct owner.
	r := graphs[0].Entities[2]
	if got := r.Properties[EffectPropertyKeySource]; got != "direct" {
		t.Errorf("repo effect_source=%q; want direct", got)
	}
	// Sidecar written.
	if _, err := os.Stat(filepath.Join(root, "out", "links-effects.json")); err != nil {
		t.Errorf("expected sidecar links-effects.json to exist: %v", err)
	}
}

// TestEffectPropagation_PureFunctionLeftUnstamped verifies that
// functions with no detected sinks (and no transitive callees with
// sinks) are not stamped — keeps graph noise low.
func TestEffectPropagation_PureFunctionLeftUnstamped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pure.py", `
def add(a, b):
    return a + b
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "p1", Name: "add", Kind: "SCOPE.Function", SourceFile: "pure.py"},
		},
	}}
	_, err := runEffectPropagationPass(graphs, Paths{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := graphs[0].Entities[0].Properties[EffectPropertyKeyList]; ok {
		t.Errorf("pure function carries effects=%q; want unstamped", v)
	}
}

// TestEffectPropagation_HopDecay confirms confidence drops as the call
// distance from the sink grows.
func TestEffectPropagation_HopDecay(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "x.go", `
package x

func leaf() {
	rows, _ := db.Query("SELECT 1")
	_ = rows
}

func mid() { leaf() }
func top() { mid() }
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "t", Name: "top", Kind: "SCOPE.Function", SourceFile: "x.go"},
			{ID: "m", Name: "mid", Kind: "SCOPE.Function", SourceFile: "x.go"},
			{ID: "l", Name: "leaf", Kind: "SCOPE.Function", SourceFile: "x.go"},
		},
		Edges: []edgeRef{
			{FromID: "t", ToID: "m", Kind: "CALLS"},
			{FromID: "m", ToID: "l", Kind: "CALLS"},
		},
	}}
	if _, err := runEffectPropagationPass(graphs, Paths{}, nil); err != nil {
		t.Fatal(err)
	}
	leafConf := confFromProps(graphs[0].Entities[2].Properties, "db_read")
	midConf := confFromProps(graphs[0].Entities[1].Properties, "db_read")
	topConf := confFromProps(graphs[0].Entities[0].Properties, "db_read")
	if !(leafConf >= midConf && midConf >= topConf) {
		t.Errorf("hop decay broken: leaf=%v mid=%v top=%v", leafConf, midConf, topConf)
	}
	if topConf == leafConf {
		t.Errorf("hop decay had no effect across 2 hops: top=%v leaf=%v", topConf, leafConf)
	}
}

// TestEffectPropagation_QualifiedNameAndViaHelper is the #2804
// regression. It reproduces two failures that combined to make an
// IO-heavy Celery task report `pure`:
//
//  1. The graph stores methods under their QUALIFIED name
//     ("ProposalViewSet.send_proposals") while the sniffer attributes
//     sinks to the bare name ("send_proposals"); the binder must still
//     connect them.
//  2. A task delegates its DB write to a helper it CALLS — the task body
//     has only S3/env sinks, so DB must arrive transitively.
func TestEffectPropagation_QualifiedNameAndViaHelper(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "task.py", `
import os
import boto3

class Pipeline:
    def process_job(self, payload):
        s3 = boto3.client("s3", region_name=os.getenv("AWS_REGION"))
        s3.download_file(payload["bucket"], payload["key"], "/tmp/x.pdf")
        persist_to_db(payload)
`)
	writeFile(t, root, "helper.py", `
import mysql.connector

def persist_to_db(row):
    conn = mysql.connector.connect(host="db")
    cur = conn.cursor()
    cur.execute("INSERT INTO t VALUES (1)")
    conn.commit()
`)
	graphs := []repoGraph{{
		Repo:     "upvate-core",
		FileRoot: root,
		Entities: []entityNode{
			// Method stored under its qualified name, as the real extractor emits.
			{ID: "job", Name: "Pipeline.process_job", Kind: "SCOPE.Operation", SourceFile: "task.py"},
			{ID: "helper", Name: "persist_to_db", Kind: "SCOPE.Operation", SourceFile: "helper.py"},
		},
		Edges: []edgeRef{
			{FromID: "job", ToID: "helper", Kind: "CALLS"},
		},
	}}
	if _, err := runEffectPropagationPass(graphs, Paths{}, nil); err != nil {
		t.Fatal(err)
	}
	job := graphs[0].Entities[0].Properties
	if job == nil {
		t.Fatal("process_job not stamped — qualified-name binding regression (#2804)")
	}
	effs := job[EffectPropertyKeyList]
	// Direct sinks on the task body.
	for _, want := range []string{"http_out", "env_read"} {
		if !strings.Contains(effs, want) {
			t.Errorf("process_job effects=%q; want direct %q", effs, want)
		}
	}
	// Transitive sinks inherited from the helper it calls.
	for _, want := range []string{"db_read", "db_write"} {
		if !strings.Contains(effs, want) {
			t.Errorf("process_job effects=%q; want transitive %q (via persist_to_db)", effs, want)
		}
	}
	// Helper is the direct DB owner.
	if got := graphs[0].Entities[1].Properties[EffectPropertyKeySource]; got != "direct" {
		t.Errorf("persist_to_db effect_source=%q; want direct", got)
	}
}

// TestEffectPropagation_DecoratorWrapperAndBody is the second half of the
// #2804 regression: a Celery task is represented by TWO entities sharing
// (file, name) — a decorator-wrapper Task node and the Operation body.
// The sniffer attributes sinks to the bare function name, so BOTH must be
// stamped; before the fix the Task node bound to nothing (its kind was
// not function-like) and reported `pure`, exactly as
// process_ecb_pdf_job's Task node did live.
func TestEffectPropagation_DecoratorWrapperAndBody(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "task.py", `
import os
import boto3

@shared_task(bind=True)
def process_job(self, payload):
    s3 = boto3.client("s3", region_name=os.getenv("AWS_REGION"))
    s3.download_file(payload["bucket"], payload["key"], "/tmp/x.pdf")
`)
	graphs := []repoGraph{{
		Repo:     "upvate-core",
		FileRoot: root,
		Entities: []entityNode{
			// Decorator-wrapper node (the @shared_task line) and the
			// function body — both named process_job in the same file.
			{ID: "task", Name: "process_job", Kind: "Task", SourceFile: "task.py"},
			{ID: "op", Name: "process_job", Kind: "SCOPE.Operation", SourceFile: "task.py"},
		},
	}}
	if _, err := runEffectPropagationPass(graphs, Paths{}, nil); err != nil {
		t.Fatal(err)
	}
	for _, ent := range graphs[0].Entities {
		effs := ent.Properties[EffectPropertyKeyList]
		if effs == "" {
			t.Fatalf("%s node (%s) left unstamped — decorator-wrapper binding regression (#2804)", ent.Kind, ent.ID)
		}
		for _, want := range []string{"http_out", "env_read"} {
			if !strings.Contains(effs, want) {
				t.Errorf("%s node effects=%q; want %q", ent.Kind, effs, want)
			}
		}
		if got := ent.Properties[EffectPropertyKeySource]; got != "direct" {
			t.Errorf("%s node effect_source=%q; want direct", ent.Kind, got)
		}
	}
}

// TestEffectPropagation_EndpointInheritsHandlerEffects verifies #2811: an
// http_endpoint synthetic inherits its handler's transitive effect closure via
// the IMPLEMENTS edge (handler → endpoint), and is tagged source="endpoint".
func TestEffectPropagation_EndpointInheritsHandlerEffects(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "views.py", `
class BuildingViewSet:
    def create(self, request):
        b = Building(**request.data)
        b.save()
        return b
`)
	graphs := []repoGraph{{
		Repo:     "upvate-core",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "h1", Name: "create", Kind: "SCOPE.Function", SourceFile: "views.py"},
			{ID: "ep1", Name: "http:POST:/buildings", Kind: "http_endpoint", SourceFile: "views.py",
				Properties: map[string]string{"verb": "POST", "path": "/buildings", "pattern_type": "http_endpoint_synthesis"}},
		},
		Edges: []edgeRef{
			// handler → endpoint (producer-side IMPLEMENTS).
			{FromID: "h1", ToID: "ep1", Kind: "IMPLEMENTS"},
		},
	}}
	paths := Paths{Links: filepath.Join(root, "out", "links.json")}
	if _, err := runEffectPropagationPass(graphs, paths, nil); err != nil {
		t.Fatal(err)
	}
	ep := graphs[0].Entities[1]
	effs := ep.Properties[EffectPropertyKeyList]
	if !strings.Contains(effs, "db_write") {
		t.Fatalf("endpoint effects=%q; want db_write inherited from handler", effs)
	}
	if got := ep.Properties[EffectPropertyKeySource]; got != "endpoint" {
		t.Errorf("endpoint effect_source=%q; want endpoint", got)
	}
	// Sidecar entry for the endpoint should also carry source=endpoint.
	buf, err := os.ReadFile(filepath.Join(root, "out", "links-effects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), `"upvate-core::ep1"`) {
		t.Errorf("sidecar missing endpoint entry; got:\n%s", buf)
	}
	if !strings.Contains(string(buf), `"endpoint"`) {
		t.Errorf("sidecar missing endpoint source tag; got:\n%s", buf)
	}
}

// TestEffectPropagation_EndpointPureHandlerUnstamped verifies an endpoint
// whose handler has no detected effects is left unstamped (no noise).
func TestEffectPropagation_EndpointPureHandlerUnstamped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "ping.py", `
class HealthView:
    def get(self, request):
        return "ok"
`)
	graphs := []repoGraph{{
		Repo:     "upvate-core",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "h1", Name: "get", Kind: "SCOPE.Function", SourceFile: "ping.py"},
			{ID: "ep1", Name: "http:GET:/health", Kind: "http_endpoint", SourceFile: "ping.py",
				Properties: map[string]string{"verb": "GET", "path": "/health"}},
		},
		Edges: []edgeRef{
			{FromID: "h1", ToID: "ep1", Kind: "IMPLEMENTS"},
		},
	}}
	if _, err := runEffectPropagationPass(graphs, Paths{}, nil); err != nil {
		t.Fatal(err)
	}
	if v, ok := graphs[0].Entities[1].Properties[EffectPropertyKeyList]; ok {
		t.Errorf("endpoint with pure handler carries effects=%q; want unstamped", v)
	}
}

// TestEffectPropagation_ScheduledJobInheritsTaskBodyEffects is the #3869
// regression. A Celery SCOPE.ScheduledJob node is keyed on the synthetic job
// ID `celery:<path>:<fn>` (in e.Name) while its backing task function name
// lives in the `handler` property. The substrate sniffer attributes the task
// body's db_write sink to the bare function name, so before the fix the
// ScheduledJob node's (file, name) bind failed and it reported `pure` despite
// its body doing IO. After the fix the node inherits the task body's effects.
func TestEffectPropagation_ScheduledJobInheritsTaskBodyEffects(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tasks.py", `
from celery import shared_task

@shared_task
def send_me_notifications():
    Notification.objects.create(user_id=1, body="hi")
`)
	graphs := []repoGraph{{
		Repo:     "upvate-core",
		FileRoot: root,
		Entities: []entityNode{
			// The Operation body, named with the bare function name.
			{ID: "op", Name: "send_me_notifications", Kind: "SCOPE.Operation", SourceFile: "tasks.py"},
			// The synthetic ScheduledJob node, as scheduled_jobs_edges.go emits
			// it: Name is the celery job ID, the bare task name is in `handler`.
			{ID: "job", Name: "celery:tasks.py:send_me_notifications", Kind: "SCOPE.ScheduledJob",
				SourceFile: "tasks.py", Properties: map[string]string{
					"handler":   "send_me_notifications",
					"framework": "celery",
				}},
		},
	}}
	if _, err := runEffectPropagationPass(graphs, Paths{}, nil); err != nil {
		t.Fatal(err)
	}
	// Locate the ScheduledJob node specifically and assert db_write on it.
	var job *entityNode
	for i := range graphs[0].Entities {
		if graphs[0].Entities[i].Kind == "SCOPE.ScheduledJob" {
			job = &graphs[0].Entities[i]
		}
	}
	if job == nil {
		t.Fatal("ScheduledJob entity missing from graph")
	}
	if job.Properties == nil {
		t.Fatal("ScheduledJob node left unstamped — #3869 binding regression")
	}
	effs := job.Properties[EffectPropertyKeyList]
	if !strings.Contains(effs, "db_write") {
		t.Fatalf("ScheduledJob (celery:...:send_me_notifications) effects=%q; "+
			"want db_write inherited from the task body (#3869)", effs)
	}
}

// TestEffectPropagation_ScheduledJobPureBodyStaysPure is the #3869 negative:
// a ScheduledJob whose task body genuinely does no IO must NOT acquire a
// fabricated effect. The binder only adds a name key — it never invents a sink.
func TestEffectPropagation_ScheduledJobPureBodyStaysPure(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tasks.py", `
from celery import shared_task

@shared_task
def add_numbers():
    return 1 + 2
`)
	graphs := []repoGraph{{
		Repo:     "upvate-core",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "op", Name: "add_numbers", Kind: "SCOPE.Operation", SourceFile: "tasks.py"},
			{ID: "job", Name: "celery:tasks.py:add_numbers", Kind: "SCOPE.ScheduledJob",
				SourceFile: "tasks.py", Properties: map[string]string{
					"handler":   "add_numbers",
					"framework": "celery",
				}},
		},
	}}
	if _, err := runEffectPropagationPass(graphs, Paths{}, nil); err != nil {
		t.Fatal(err)
	}
	var job *entityNode
	for i := range graphs[0].Entities {
		if graphs[0].Entities[i].Kind == "SCOPE.ScheduledJob" {
			job = &graphs[0].Entities[i]
		}
	}
	if job == nil {
		t.Fatal("ScheduledJob entity missing from graph")
	}
	if v, ok := job.Properties[EffectPropertyKeyList]; ok {
		t.Errorf("pure-bodied ScheduledJob carries fabricated effects=%q; want unstamped", v)
	}
}

// TestEffectPropagation_ScheduledJobInheritsTransitiveHelperEffects is the
// #3934 fix: a Celery ScheduledJob whose task body does NO IO directly but
// DELEGATES its write to a helper it CALLS. The helper owns the db_write/
// db_delete sink; the body inherits it transitively via the CALLS fixed-point;
// the ScheduledJob WRAPPER (a separate node with no outgoing CALLS) must in
// turn inherit it from the body. Before #3934 the wrapper bound only the body's
// DIRECT sinks (#3869) — none here — so it reported `pure`.
func TestEffectPropagation_ScheduledJobInheritsTransitiveHelperEffects(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tasks.py", `
from celery import shared_task

@shared_task
def clear_inspections_task():
    _do_clear()
`)
	writeFile(t, root, "helpers.py", `
def _do_clear():
    Inspection.objects.all().delete()
`)
	graphs := []repoGraph{{
		Repo:     "upvate-core",
		FileRoot: root,
		Entities: []entityNode{
			// The task body — bare function name, owns the CALLS edge to the helper.
			{ID: "op", Name: "clear_inspections_task", Kind: "SCOPE.Operation", SourceFile: "tasks.py"},
			// The helper that actually performs the DB delete (direct sink owner).
			{ID: "helper", Name: "_do_clear", Kind: "SCOPE.Operation", SourceFile: "helpers.py"},
			// The synthetic ScheduledJob wrapper: name is the celery job ID, bare
			// task name is in `handler`. It has NO outgoing CALLS of its own.
			{ID: "job", Name: "celery:tasks.py:clear_inspections_task", Kind: "SCOPE.ScheduledJob",
				SourceFile: "tasks.py", Properties: map[string]string{
					"handler":   "clear_inspections_task",
					"framework": "celery",
				}},
		},
		Edges: []edgeRef{
			// CALLS lives on the BODY function, not the wrapper.
			{FromID: "op", ToID: "helper", Kind: "CALLS"},
		},
	}}
	if _, err := runEffectPropagationPass(graphs, Paths{}, nil); err != nil {
		t.Fatal(err)
	}
	var job *entityNode
	for i := range graphs[0].Entities {
		if graphs[0].Entities[i].Kind == "SCOPE.ScheduledJob" {
			job = &graphs[0].Entities[i]
		}
	}
	if job == nil {
		t.Fatal("ScheduledJob entity missing from graph")
	}
	if job.Properties == nil {
		t.Fatal("ScheduledJob node left unstamped — #3934 transitive-via-helper gap")
	}
	effs := job.Properties[EffectPropertyKeyList]
	// The .delete() ORM sink classifies as db_write (and may add db_delete);
	// the wrapper must carry the helper's transitive effect, not be pure.
	if !strings.Contains(effs, "db_write") && !strings.Contains(effs, "db_delete") {
		t.Fatalf("ScheduledJob (celery:...:clear_inspections_task) effects=%q; "+
			"want db_write/db_delete inherited TRANSITIVELY via the helper (#3934)", effs)
	}
	// Effects arrived through a helper the body CALLS, not a direct sink on the
	// wrapper itself → source must be transitive.
	if got := job.Properties[EffectPropertyKeySource]; got != "transitive" {
		t.Errorf("ScheduledJob effect_source=%q; want transitive (inherited via helper)", got)
	}
	// The body itself must also carry the transitive effect (sanity on the join).
	if !strings.Contains(graphs[0].Entities[0].Properties[EffectPropertyKeyList], "db_write") &&
		!strings.Contains(graphs[0].Entities[0].Properties[EffectPropertyKeyList], "db_delete") {
		t.Errorf("task body effects=%q; want transitive db_write/db_delete",
			graphs[0].Entities[0].Properties[EffectPropertyKeyList])
	}
}

// TestEffectPropagation_ScheduledJobTransitivePureStaysPure is the #3934
// negative: a ScheduledJob whose body AND every callee are genuinely pure must
// not acquire a fabricated effect — the body-closure inheritance only copies
// effects that genuinely resolved on the body.
func TestEffectPropagation_ScheduledJobTransitivePureStaysPure(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tasks.py", `
from celery import shared_task

@shared_task
def compute_task():
    return _pure_add()
`)
	writeFile(t, root, "helpers.py", `
def _pure_add():
    return 1 + 2
`)
	graphs := []repoGraph{{
		Repo:     "upvate-core",
		FileRoot: root,
		Entities: []entityNode{
			{ID: "op", Name: "compute_task", Kind: "SCOPE.Operation", SourceFile: "tasks.py"},
			{ID: "helper", Name: "_pure_add", Kind: "SCOPE.Operation", SourceFile: "helpers.py"},
			{ID: "job", Name: "celery:tasks.py:compute_task", Kind: "SCOPE.ScheduledJob",
				SourceFile: "tasks.py", Properties: map[string]string{
					"handler":   "compute_task",
					"framework": "celery",
				}},
		},
		Edges: []edgeRef{
			{FromID: "op", ToID: "helper", Kind: "CALLS"},
		},
	}}
	if _, err := runEffectPropagationPass(graphs, Paths{}, nil); err != nil {
		t.Fatal(err)
	}
	var job *entityNode
	for i := range graphs[0].Entities {
		if graphs[0].Entities[i].Kind == "SCOPE.ScheduledJob" {
			job = &graphs[0].Entities[i]
		}
	}
	if job == nil {
		t.Fatal("ScheduledJob entity missing from graph")
	}
	if v, ok := job.Properties[EffectPropertyKeyList]; ok {
		t.Errorf("pure-chain ScheduledJob carries fabricated effects=%q; want unstamped", v)
	}
}

func confFromProps(props map[string]string, effect string) float64 {
	if props == nil {
		return 0
	}
	raw := props[EffectPropertyKeyConfidence]
	for _, part := range strings.Split(raw, ",") {
		if !strings.HasPrefix(part, effect+"=") {
			continue
		}
		var v float64
		// strconv would do but the format is "%.2f" — sscanf keeps deps minimal.
		_, _ = fmtSscanf(part[len(effect)+1:], "%f", &v)
		return v
	}
	return 0
}

// fmtSscanf is a thin shim so the test file does not pull fmt in for
// only one Sscanf call (gofmt cares about imports).
var fmtSscanf = func(s, format string, args ...any) (int, error) {
	// Inline minimal float parse; tests only ever ask for "%f".
	var x float64
	idx := 0
	neg := false
	if len(s) > 0 && s[0] == '-' {
		neg = true
		idx = 1
	}
	for ; idx < len(s); idx++ {
		c := s[idx]
		if c == '.' {
			frac := 1.0
			for j := idx + 1; j < len(s); j++ {
				d := s[j]
				if d < '0' || d > '9' {
					break
				}
				frac *= 10
				x = x*10 + float64(d-'0')
			}
			x /= frac
			break
		}
		if c < '0' || c > '9' {
			break
		}
		x = x*10 + float64(c-'0')
	}
	if neg {
		x = -x
	}
	if pf, ok := args[0].(*float64); ok {
		*pf = x
	}
	return 1, nil
}
