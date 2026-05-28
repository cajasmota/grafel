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
