package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// dagRun runs the DAG pass and returns entities + relationships.
func dagRun(lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	res := applyWorkflowDAGEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

// hasDep reports whether a TASK_DEPENDS_ON edge upstream→downstream exists,
// matching on the task-name suffix of the synthetic IDs.
func hasDep(t *testing.T, rels []types.RelationshipRecord, engine, dag, upstream, downstream string) bool {
	t.Helper()
	wantFrom := "SCOPE.Activity:" + dagTaskID(engine, dag, upstream)
	wantTo := "SCOPE.Activity:" + dagTaskID(engine, dag, downstream)
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindTaskDependsOn) && r.FromID == wantFrom && r.ToID == wantTo {
			return true
		}
	}
	return false
}

func hasExecutes(rels []types.RelationshipRecord, engine, dag, task string) bool {
	wantTo := "SCOPE.Activity:" + dagTaskID(engine, dag, task)
	for _, r := range rels {
		if r.Kind == executesActivityEdgeKind && r.ToID == wantTo {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Airflow
// ---------------------------------------------------------------------------

func TestAirflow_ShiftChain_LinearDependencies(t *testing.T) {
	src := `
from airflow import DAG
from airflow.operators.python import PythonOperator

with DAG("etl_pipeline") as dag:
    extract = PythonOperator(task_id="extract", python_callable=do_extract)
    transform = PythonOperator(task_id="transform", python_callable=do_transform)
    load = PythonOperator(task_id="load", python_callable=do_load)

    extract >> transform >> load
`
	_, rels := dagRun("python", "dags/etl.py", src)

	if !hasDep(t, rels, "airflow", "etl_pipeline", "extract", "transform") {
		t.Errorf("missing extract->transform edge; rels=%+v", rels)
	}
	if !hasDep(t, rels, "airflow", "etl_pipeline", "transform", "load") {
		t.Errorf("missing transform->load edge")
	}
	// Direction must be correct: no reverse edge.
	if hasDep(t, rels, "airflow", "etl_pipeline", "transform", "extract") {
		t.Errorf("false reverse edge transform->extract")
	}
	// Workflow owns each task.
	if !hasExecutes(rels, "airflow", "etl_pipeline", "extract") {
		t.Errorf("missing EXECUTES_ACTIVITY for extract")
	}
}

func TestAirflow_FanInList(t *testing.T) {
	src := `
from airflow import DAG
from airflow.operators.bash import BashOperator

with DAG(dag_id="fanin") as dag:
    a = BashOperator(task_id="a")
    b = BashOperator(task_id="b")
    c = BashOperator(task_id="c")
    [a, b] >> c
`
	_, rels := dagRun("python", "dags/fanin.py", src)
	if !hasDep(t, rels, "airflow", "fanin", "a", "c") {
		t.Errorf("missing a->c")
	}
	if !hasDep(t, rels, "airflow", "fanin", "b", "c") {
		t.Errorf("missing b->c")
	}
	if hasDep(t, rels, "airflow", "fanin", "a", "b") {
		t.Errorf("false a->b edge in fan-in")
	}
}

func TestAirflow_SetDownstream(t *testing.T) {
	src := `
from airflow import DAG
from airflow.operators.python import PythonOperator

with DAG("sd") as dag:
    first = PythonOperator(task_id="first")
    second = PythonOperator(task_id="second")
    first.set_downstream(second)
`
	_, rels := dagRun("python", "dags/sd.py", src)
	if !hasDep(t, rels, "airflow", "sd", "first", "second") {
		t.Errorf("set_downstream should yield first->second")
	}
}

func TestAirflow_SetUpstream_DirectionFlipped(t *testing.T) {
	src := `
from airflow import DAG
from airflow.operators.python import PythonOperator

with DAG("su") as dag:
    a = PythonOperator(task_id="a")
    b = PythonOperator(task_id="b")
    b.set_upstream(a)
`
	_, rels := dagRun("python", "dags/su.py", src)
	// b.set_upstream(a): a runs before b → a->b.
	if !hasDep(t, rels, "airflow", "su", "a", "b") {
		t.Errorf("set_upstream should yield a->b")
	}
	if hasDep(t, rels, "airflow", "su", "b", "a") {
		t.Errorf("false b->a edge")
	}
}

func TestAirflow_TaskFlowDecorators(t *testing.T) {
	src := `
from airflow.decorators import dag, task

@dag(dag_id="taskflow_etl")
def pipeline():
    @task
    def extract():
        return 1

    @task
    def transform(x):
        return x

    @task
    def load(x):
        return x

    load(transform(extract()))
    extract() >> transform() >> load()
`
	_, rels := dagRun("python", "dags/tf.py", src)
	if !hasDep(t, rels, "airflow", "taskflow_etl", "extract", "transform") {
		t.Errorf("taskflow extract->transform missing")
	}
	if !hasDep(t, rels, "airflow", "taskflow_etl", "transform", "load") {
		t.Errorf("taskflow transform->load missing")
	}
}

func TestAirflow_LoneTaskNoFalseEdge(t *testing.T) {
	src := `
from airflow import DAG
from airflow.operators.python import PythonOperator

with DAG("solo") as dag:
    only = PythonOperator(task_id="only", python_callable=fn)
`
	ents, rels := dagRun("python", "dags/solo.py", src)
	// Task + workflow exist, but no dependency edge.
	if !hasExecutes(rels, "airflow", "solo", "only") {
		t.Errorf("lone task should still be owned by the DAG")
	}
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindTaskDependsOn) {
			t.Errorf("lone task produced a false dependency edge: %+v", r)
		}
	}
	// Sanity: exactly one activity entity for the task.
	taskCount := 0
	for _, e := range ents {
		if e.Kind == activityKind {
			taskCount++
		}
	}
	if taskCount != 1 {
		t.Errorf("want 1 task entity, got %d", taskCount)
	}
}

// ---------------------------------------------------------------------------
// Celery canvas
// ---------------------------------------------------------------------------

func TestCelery_Chain(t *testing.T) {
	src := `
from celery import chain
result = chain(fetch.s(), process.s(), store.s())()
`
	_, rels := dagRun("python", "tasks.py", src)
	if !hasDep(t, rels, "celery", "canvas", "fetch", "process") {
		t.Errorf("chain fetch->process missing; rels=%+v", rels)
	}
	if !hasDep(t, rels, "celery", "canvas", "process", "store") {
		t.Errorf("chain process->store missing")
	}
	if hasDep(t, rels, "celery", "canvas", "process", "fetch") {
		t.Errorf("false reverse chain edge")
	}
}

func TestCelery_ChainTwoTasks(t *testing.T) {
	src := `
from celery import chain
chain(fetch.s(), process.s())
`
	_, rels := dagRun("python", "tasks.py", src)
	if !hasDep(t, rels, "celery", "canvas", "fetch", "process") {
		t.Errorf("expected fetch->process")
	}
}

func TestCelery_Chord_FanIn(t *testing.T) {
	src := `
from celery import chord
chord([a.s(), b.s()])(callback.s())
`
	_, rels := dagRun("python", "tasks.py", src)
	if !hasDep(t, rels, "celery", "canvas", "a", "callback") {
		t.Errorf("chord a->callback missing; rels=%+v", rels)
	}
	if !hasDep(t, rels, "celery", "canvas", "b", "callback") {
		t.Errorf("chord b->callback missing")
	}
	// header members are parallel — no a->b.
	if hasDep(t, rels, "celery", "canvas", "a", "b") {
		t.Errorf("false a->b edge in chord header")
	}
}

func TestCelery_Group_NoFalseOrdering(t *testing.T) {
	src := `
from celery import group
group(a.s(), b.s(), c.s())
`
	ents, rels := dagRun("python", "tasks.py", src)
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindTaskDependsOn) {
			t.Errorf("group members must not have ordering edges: %+v", r)
		}
	}
	// All three still emitted as tasks.
	tasks := 0
	for _, e := range ents {
		if e.Kind == activityKind {
			tasks++
		}
	}
	if tasks != 3 {
		t.Errorf("want 3 group tasks, got %d", tasks)
	}
}

// ---------------------------------------------------------------------------
// Argo Workflows
// ---------------------------------------------------------------------------

func TestArgo_DAGDependencies(t *testing.T) {
	src := `
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: build-deploy
spec:
  entrypoint: main
  templates:
    - name: main
      dag:
        tasks:
          - name: build
            template: build-tmpl
          - name: test
            template: test-tmpl
            dependencies: [build]
          - name: deploy
            template: deploy-tmpl
            dependencies: [test]
`
	_, rels := dagRun("yaml", "argo/workflow.yaml", src)
	if !hasDep(t, rels, "argo", "build-deploy", "build", "test") {
		t.Errorf("argo build->test missing; rels=%+v", rels)
	}
	if !hasDep(t, rels, "argo", "build-deploy", "test", "deploy") {
		t.Errorf("argo test->deploy missing")
	}
	if hasDep(t, rels, "argo", "build-deploy", "test", "build") {
		t.Errorf("false reverse argo edge")
	}
	if !hasExecutes(rels, "argo", "build-deploy", "build") {
		t.Errorf("argo workflow should own build task")
	}
}

func TestArgo_DAGTwoDeps_FanIn(t *testing.T) {
	src := `
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: fan
spec:
  templates:
    - name: main
      dag:
        tasks:
          - name: a
            template: t
          - name: b
            template: t
          - name: c
            template: t
            dependencies: [a, b]
`
	_, rels := dagRun("yaml", "argo/fan.yaml", src)
	if !hasDep(t, rels, "argo", "fan", "a", "c") {
		t.Errorf("a->c missing")
	}
	if !hasDep(t, rels, "argo", "fan", "b", "c") {
		t.Errorf("b->c missing")
	}
}

func TestArgo_Steps_Sequential(t *testing.T) {
	src := `
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: steps-wf
spec:
  templates:
    - name: main
      steps:
        - - name: prepare
            template: t
        - - name: run
            template: t
`
	_, rels := dagRun("yaml", "argo/steps.yaml", src)
	if !hasDep(t, rels, "argo", "steps-wf", "prepare", "run") {
		t.Errorf("steps prepare->run missing; rels=%+v", rels)
	}
}

func TestArgo_LoneTask_NoFalseEdge(t *testing.T) {
	src := `
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: single
spec:
  templates:
    - name: main
      dag:
        tasks:
          - name: only
            template: t
`
	_, rels := dagRun("yaml", "argo/single.yaml", src)
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindTaskDependsOn) {
			t.Errorf("lone argo task should have no dependency edge: %+v", r)
		}
	}
	if !hasExecutes(rels, "argo", "single", "only") {
		t.Errorf("lone argo task should be owned by workflow")
	}
}

func TestArgo_NonArgoYAMLIgnored(t *testing.T) {
	src := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
        - name: web
`
	ents, rels := dagRun("yaml", "k8s/deploy.yaml", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-Argo YAML must not emit workflow entities/edges; ents=%d rels=%d", len(ents), len(rels))
	}
}

func TestNonWorkflowPythonIgnored(t *testing.T) {
	src := `
def foo():
    return 1
x = foo()
`
	ents, rels := dagRun("python", "util.py", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("plain python must not emit DAG entities; ents=%d rels=%d", len(ents), len(rels))
	}
}
