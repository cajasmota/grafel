// Workflow / orchestration DAG topology — #3628 area #12 (child ticket).
//
// workflow_edges.go (#934) covers Temporal / Cadence / AWS Step Functions: it
// emits SCOPE.Workflow + SCOPE.Activity entities and STARTS_WORKFLOW /
// EXECUTES_ACTIVITY / STEPFUNCTION_STEP_INVOKES edges. What it does NOT model is
// the *task-dependency DAG* that the data-pipeline orchestrators are built
// around — the ordered task→task topology that answers "what runs after what".
//
// This pass extends the same entity/edge shape to three DAG orchestrators:
//
//   - Airflow (Python)      — PythonOperator / @task tasks + `t1 >> t2`,
//     `t1.set_downstream(t2)`, `[t1,t2] >> t3`,
//     and decorator-call chains `extract() >> transform()`.
//   - Celery canvas (Python) — chain(a.s(), b.s(), c.s()) → a→b→c,
//     group(...) fan-out, chord(header)(callback) fan-in.
//   - Argo Workflows (YAML) — templates with `steps:` or `dag.tasks:` +
//     `dependencies: [t1]` → task→task edges.
//
// Entities emitted (reusing workflow_edges.go kinds for cross-pass consistency):
//   - SCOPE.Workflow  — the DAG / canvas / Argo Workflow that owns the tasks
//   - SCOPE.Activity  — an individual task in the DAG
//
// Edge kinds emitted:
//   - EXECUTES_ACTIVITY  — Workflow → Activity (the DAG owns the task). Reuses
//     the existing const from workflow_edges.go.
//   - TASK_DEPENDS_ON    — Activity(upstream) → Activity(downstream). Direction
//     follows execution order: for `extract >> transform`
//     the edge is extract → transform (the canonical
//     types.RelationshipKindTaskDependsOn, already emitted
//     by the Taskfile/Mage extractors for task graphs).
//
// Synthetic entity IDs (SourceFile "" on synthetics so the import-channel linker
// joins them with no new linker code, mirroring workflow_edges.go):
//   - workflow:<engine>:<dagName>
//   - task:<engine>:<dagName>:<taskName>
//
// Honest-partial scope: dynamically-generated tasks (loops building operators,
// `.expand()` dynamic task mapping, programmatically-built chains) are NOT
// resolved — only statically-named tasks and their static dependency operators
// are. This matches the partial coverage status recorded for these engines.
//
// Scope guard: append-only. This pass never modifies or removes existing
// entities or edges, so it cannot regress the surrounding pipeline.
//
// Refs #3628 (area #12).
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Edge kinds
// ---------------------------------------------------------------------------

// taskDependsOnEdgeKind is the task→task DAG dependency edge. It aliases the
// canonical typed constant so the producer-kind validator covers it.
const taskDependsOnEdgeKind = string(types.RelationshipKindTaskDependsOn)

// ---------------------------------------------------------------------------
// Synthetic entity ID helpers
// ---------------------------------------------------------------------------

func dagWorkflowID(engine, dag string) string { return "workflow:" + engine + ":" + dag }
func dagTaskID(engine, dag, task string) string {
	return "task:" + engine + ":" + dag + ":" + task
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

// applyWorkflowDAGEdges detects Airflow / Celery-canvas / Argo task-dependency
// DAGs and emits SCOPE.Workflow + SCOPE.Activity entities plus EXECUTES_ACTIVITY
// (workflow→task) and TASK_DEPENDS_ON (task→task) edges. Append-only.
func applyWorkflowDAGEdges(args DetectorPassArgs) DetectorPassResult {
	entities := args.Entities
	relationships := args.Relationships
	if len(args.Content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	em := newDAGEmitter(args.Lang, entities, relationships)

	switch args.Lang {
	case "python":
		src := string(args.Content)
		synthesizeAirflowDAG(src, em)
		synthesizeCeleryCanvas(src, em)
	case "yaml":
		synthesizeArgoWorkflow(args.Content, em)
	}

	return DetectorPassResult{Entities: em.entities, Relationships: em.relationships}
}

// ---------------------------------------------------------------------------
// Shared emitter
// ---------------------------------------------------------------------------

// dagEmitter accumulates entities/edges with dedup, mirroring the closure-based
// emit helpers in workflow_edges.go but reified so all three synthesizers share
// one dedup namespace within a file.
type dagEmitter struct {
	lang          string
	entities      []types.EntityRecord
	relationships []types.RelationshipRecord
	seenEnt       map[string]bool
	seenEdge      map[string]bool
}

func newDAGEmitter(lang string, ents []types.EntityRecord, rels []types.RelationshipRecord) *dagEmitter {
	return &dagEmitter{
		lang:          lang,
		entities:      ents,
		relationships: rels,
		seenEnt:       map[string]bool{},
		seenEdge:      map[string]bool{},
	}
}

func (e *dagEmitter) workflow(wfID, dagName, engine string, props map[string]string) {
	if e.seenEnt[wfID] {
		return
	}
	e.seenEnt[wfID] = true
	merged := map[string]string{
		"workflow_engine": engine,
		"workflow_name":   dagName,
		"pattern_type":    "workflow_synthesis",
		"topology":        "dag",
	}
	for k, v := range props {
		if v != "" {
			merged[k] = v
		}
	}
	e.entities = append(e.entities, types.EntityRecord{
		Name:               wfID,
		Kind:               workflowKind,
		SourceFile:         "",
		Language:           e.lang,
		Properties:         merged,
		EnrichmentRequired: false,
		EnrichmentStatus:   types.StatusPending,
		QualityScore:       0.85,
	})
}

func (e *dagEmitter) task(taskID, taskName, engine string, props map[string]string) {
	if e.seenEnt[taskID] {
		return
	}
	e.seenEnt[taskID] = true
	merged := map[string]string{
		"workflow_engine": engine,
		"activity_name":   taskName,
		"pattern_type":    "workflow_synthesis",
		"node_role":       "task",
	}
	for k, v := range props {
		if v != "" {
			merged[k] = v
		}
	}
	e.entities = append(e.entities, types.EntityRecord{
		Name:               taskID,
		Kind:               activityKind,
		SourceFile:         "",
		Language:           e.lang,
		Properties:         merged,
		EnrichmentRequired: false,
		EnrichmentStatus:   types.StatusPending,
		QualityScore:       0.85,
	})
}

// executes emits the Workflow→Task ownership edge (EXECUTES_ACTIVITY).
func (e *dagEmitter) executes(wfID, taskID, engine string) {
	if wfID == "" || taskID == "" {
		return
	}
	key := executesActivityEdgeKind + "|" + wfID + "|" + taskID
	if e.seenEdge[key] {
		return
	}
	e.seenEdge[key] = true
	e.relationships = append(e.relationships, types.RelationshipRecord{
		FromID: fmt.Sprintf("%s:%s", workflowKind, wfID),
		ToID:   fmt.Sprintf("%s:%s", activityKind, taskID),
		Kind:   executesActivityEdgeKind,
		Properties: map[string]string{
			"workflow_engine": engine,
			"pattern_type":    "workflow_synthesis",
		},
	})
}

// dependsOn emits the upstream→downstream DAG dependency edge (TASK_DEPENDS_ON).
// The direction follows execution order: `extract >> transform` → extract is
// upstream, transform downstream, edge upstream→downstream.
func (e *dagEmitter) dependsOn(upstreamID, downstreamID, engine string) {
	if upstreamID == "" || downstreamID == "" || upstreamID == downstreamID {
		return
	}
	key := taskDependsOnEdgeKind + "|" + upstreamID + "|" + downstreamID
	if e.seenEdge[key] {
		return
	}
	e.seenEdge[key] = true
	e.relationships = append(e.relationships, types.RelationshipRecord{
		FromID: fmt.Sprintf("%s:%s", activityKind, upstreamID),
		ToID:   fmt.Sprintf("%s:%s", activityKind, downstreamID),
		Kind:   taskDependsOnEdgeKind,
		Properties: map[string]string{
			"workflow_engine": engine,
			"pattern_type":    "workflow_synthesis",
		},
	})
}

// ---------------------------------------------------------------------------
// Airflow (Python)
// ---------------------------------------------------------------------------

// airflowGuardRe is the fast-path gate: an Airflow file imports the SDK or
// constructs a DAG.
var airflowGuardRe = regexp.MustCompile(`(?:from\s+airflow|import\s+airflow|@dag\b|with\s+DAG\s*\(|\bDAG\s*\()`)

// afDagWithRe captures `with DAG("dag_id", ...)` / `with DAG(dag_id="x", ...)`.
// Group 1 = positional id, Group 2 = dag_id= kwarg.
var afDagWithRe = regexp.MustCompile(`with\s+DAG\s*\(\s*(?:["']([^"'\n\r]+)["']|dag_id\s*=\s*["']([^"'\n\r]+)["'])`)

// afDagDecorRe captures the TaskFlow `@dag` decorator followed by `def name(`.
// Group 1 = dag_id= kwarg (optional), Group 2 = function name.
var afDagDecorRe = regexp.MustCompile(`@dag(?:\s*\([^)]*?dag_id\s*=\s*["']([^"'\n\r]+)["'][^)]*?)?\s*\)?\s*\n(?:\s*@[^\n]*\n)*\s*def\s+(\w+)\s*\(`)

// afOperatorRe captures `t1 = PythonOperator(task_id="extract", ...)` and any
// other `*Operator(` or `*Sensor(` assignment. Group 1 = python var, Group 2 =
// task_id kwarg (optional — falls back to the var name).
var afOperatorRe = regexp.MustCompile(`(\w+)\s*=\s*\w*(?:Operator|Sensor)\s*\([^)]*?task_id\s*=\s*["']([^"'\n\r]+)["']`)

// afOperatorNoIDRe captures `t1 = PythonOperator(...)` without a literal task_id
// on the same line (multi-line ctor). Group 1 = var.
var afOperatorNoIDRe = regexp.MustCompile(`(\w+)\s*=\s*\w*(?:Operator|Sensor)\s*\(`)

// afTaskDecorRe captures the TaskFlow `@task` decorator + `def extract(`.
// Group 1 = task_id= kwarg (optional), Group 2 = function name.
var afTaskDecorRe = regexp.MustCompile(`@task(?:\.\w+)?(?:\s*\([^)]*?task_id\s*=\s*["']([^"'\n\r]+)["'][^)]*?)?\s*\)?\s*\n(?:\s*@[^\n]*\n)*\s*def\s+(\w+)\s*\(`)

// afShiftRe captures a chained `>>` / `<<` dependency expression on one logical
// line, e.g. `extract >> transform >> load` or `[t1, t2] >> t3` or
// `extract() >> transform()`. We capture the whole RHS+LHS chain text and parse
// operands out in code so `>>` and `<<` and list operands are all handled.
var afShiftLineRe = regexp.MustCompile(`(?m)^[^\n#]*?(?:>>|<<)[^\n]*$`)

// afSetDownstreamRe captures `t1.set_downstream(t2)` / `t1.set_upstream(t2)`.
// Group 1 = the call-target var, Group 2 = downstream/upstream, Group 3 = arg.
var afSetStreamRe = regexp.MustCompile(`(\w+)\.set_(downstream|upstream)\s*\(\s*\[?\s*([^)\]]+?)\s*\]?\s*\)`)

// afOperandTokenRe pulls bare identifiers out of one side of a `>>` chain,
// handling `[a, b]` lists and `extract()` decorator-task calls (strips the
// parens). It deliberately ignores method calls like `a.s()` (Celery) by
// requiring the identifier not be preceded by a dot.
var afOperandTokenRe = regexp.MustCompile(`(?:^|[\[\s,])(\w+)\s*(?:\(\s*\))?`)

func synthesizeAirflowDAG(src string, em *dagEmitter) {
	if !airflowGuardRe.MatchString(src) {
		return
	}
	const engine = "airflow"

	// Determine the DAG name (best-effort: first `with DAG(...)` or `@dag def`).
	dagName := airflowDagName(src)
	wfID := dagWorkflowID(engine, dagName)

	// Collect task var/name bindings: python var → logical task name.
	varToName := map[string]string{}
	addTask := func(varName, taskName string) {
		if !looksLikeFunctionName(taskName) {
			return
		}
		varToName[varName] = taskName
		taskID := dagTaskID(engine, dagName, taskName)
		em.task(taskID, taskName, engine, map[string]string{"py_var": varName})
		em.workflow(wfID, dagName, engine, nil)
		em.executes(wfID, taskID, engine)
	}

	// Classic operators with an explicit task_id.
	for _, m := range afOperatorRe.FindAllStringSubmatch(src, -1) {
		addTask(m[1], m[2])
	}
	// Operators without a same-line task_id → use the python var as the name.
	for _, m := range afOperatorNoIDRe.FindAllStringSubmatch(src, -1) {
		if _, ok := varToName[m[1]]; ok {
			continue
		}
		addTask(m[1], m[1])
	}
	// TaskFlow @task functions → task name is the function name (var == name).
	for _, m := range afTaskDecorRe.FindAllStringSubmatch(src, -1) {
		name := m[2]
		if m[1] != "" {
			name = m[1]
		}
		// The python identifier used downstream is the def name.
		varToName[m[2]] = name
		if !looksLikeFunctionName(name) {
			continue
		}
		taskID := dagTaskID(engine, dagName, name)
		em.task(taskID, name, engine, map[string]string{"taskflow": "true"})
		em.workflow(wfID, dagName, engine, nil)
		em.executes(wfID, taskID, engine)
	}

	resolve := func(varName string) string {
		name, ok := varToName[varName]
		if !ok {
			// Unknown operand: treat the identifier itself as the task name so a
			// pure-shift DAG (no separate operator assignment) still links.
			if !looksLikeFunctionName(varName) {
				return ""
			}
			name = varName
			taskID := dagTaskID(engine, dagName, name)
			em.task(taskID, name, engine, nil)
			em.workflow(wfID, dagName, engine, nil)
			em.executes(wfID, taskID, engine)
			varToName[varName] = name
		}
		return dagTaskID(engine, dagName, name)
	}

	// Parse `>>` / `<<` dependency chains.
	for _, line := range afShiftLineRe.FindAllString(src, -1) {
		parseAirflowShiftChain(line, engine, resolve, em)
	}

	// set_downstream / set_upstream.
	for _, m := range afSetStreamRe.FindAllStringSubmatch(src, -1) {
		left := resolve(m[1])
		for _, tok := range splitOperands(m[3]) {
			right := resolve(tok)
			if m[2] == "downstream" {
				em.dependsOn(left, right, engine) // left runs before right
			} else {
				em.dependsOn(right, left, engine) // upstream: right before left
			}
		}
	}
}

// parseAirflowShiftChain parses a single `a >> b >> c` (or `<<`) line into
// upstream→downstream TASK_DEPENDS_ON edges. List operands `[a, b] >> c` fan in;
// `a >> [b, c]` fan out. Mixed `>>`/`<<` on one line is rare; we treat the line
// as left-to-right and flip per-operator.
func parseAirflowShiftChain(line, engine string, resolve func(string) string, em *dagEmitter) {
	// Tokenise into operand-groups separated by >> or <<, preserving operator.
	// Split on the operators while keeping them.
	parts := shiftSplitRe.Split(line, -1)
	ops := shiftSplitRe.FindAllString(line, -1)
	if len(ops) == 0 || len(parts) < 2 {
		return
	}
	groups := make([][]string, 0, len(parts))
	for _, p := range parts {
		groups = append(groups, splitOperands(p))
	}
	for i := 0; i < len(ops); i++ {
		leftGroup := groups[i]
		rightGroup := groups[i+1]
		for _, l := range leftGroup {
			lid := resolve(l)
			for _, r := range rightGroup {
				rid := resolve(r)
				if ops[i] == ">>" {
					em.dependsOn(lid, rid, engine)
				} else {
					em.dependsOn(rid, lid, engine)
				}
			}
		}
	}
}

var shiftSplitRe = regexp.MustCompile(`>>|<<`)

// splitOperands extracts task identifiers from one side of a shift operator,
// handling `[a, b]` lists, `extract()` calls, and bare `t1`. Assignment LHS and
// trailing junk are tolerated by only keeping the LAST identifier on a bare
// (non-list) side when an `=` is present.
func splitOperands(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// Drop an assignment prefix `x = a >> b` → only the chain matters; if the
	// fragment contains '=', keep the text after the last '='.
	if idx := strings.LastIndex(s, "="); idx >= 0 && !strings.Contains(s, "==") {
		s = s[idx+1:]
	}
	isList := strings.Contains(s, "[") || strings.Contains(s, ",")
	out := []string{}
	for _, m := range afOperandTokenRe.FindAllStringSubmatch(s, -1) {
		tok := m[1]
		if tok == "" {
			continue
		}
		out = append(out, tok)
	}
	if len(out) == 0 {
		return nil
	}
	if !isList && len(out) > 1 {
		// A bare side like `chain_result = transform` left more than one token
		// only when junk slipped through; keep the last (the actual operand).
		return out[len(out)-1:]
	}
	return out
}

// afFuncDefRe / classNameRe equivalents not needed; airflowDagName scans known
// constructors.
var afDagNamePosRe = regexp.MustCompile(`with\s+DAG\s*\(\s*["']([^"'\n\r]+)["']`)
var afDagNameKwRe = regexp.MustCompile(`dag_id\s*=\s*["']([^"'\n\r]+)["']`)

func airflowDagName(src string) string {
	if m := afDagNamePosRe.FindStringSubmatch(src); len(m) >= 2 {
		return m[1]
	}
	if m := afDagNameKwRe.FindStringSubmatch(src); len(m) >= 2 {
		return m[1]
	}
	if m := afDagDecorRe.FindStringSubmatch(src); len(m) >= 3 {
		if m[1] != "" {
			return m[1]
		}
		return m[2]
	}
	return "dag"
}

// ---------------------------------------------------------------------------
// Celery canvas (Python)
// ---------------------------------------------------------------------------

// celeryGuardRe gates the canvas scan.
var celeryGuardRe = regexp.MustCompile(`(?:from\s+celery|import\s+celery|\bchain\s*\(|\bchord\s*\(|\bgroup\s*\(|\.si\s*\(|\.s\s*\()`)

// celeryChainRe captures the full argument list of a `chain( ... )` call.
var celeryChainCallRe = regexp.MustCompile(`\bchain\s*\(`)

// celeryGroupCallRe / celeryChordCallRe locate group/chord call sites.
var celeryGroupCallRe = regexp.MustCompile(`\bgroup\s*\(`)
var celeryChordCallRe = regexp.MustCompile(`\bchord\s*\(`)

// celerySigRe pulls `taskName.s(...)` / `taskName.si(...)` signatures from a
// canvas argument list. Group 1 = task name.
var celerySigRe = regexp.MustCompile(`(\w+)\.s[i]?\s*\(`)

func synthesizeCeleryCanvas(src string, em *dagEmitter) {
	if !celeryGuardRe.MatchString(src) {
		return
	}
	const engine = "celery"
	const dagName = "canvas"
	wfID := dagWorkflowID(engine, dagName)

	taskID := func(name string) string {
		id := dagTaskID(engine, dagName, name)
		em.task(id, name, engine, nil)
		em.workflow(wfID, dagName, engine, nil)
		em.executes(wfID, id, engine)
		return id
	}

	// chain(a.s(), b.s(), c.s()) → a→b→c (sequential dependency edges).
	for _, idx := range celeryChainCallRe.FindAllStringIndex(src, -1) {
		argStr := extractCallArgString(src, idx[1]-1)
		sigs := celeryCanvasSignatures(argStr)
		var prev string
		for _, name := range sigs {
			cur := taskID(name)
			if prev != "" {
				em.dependsOn(prev, cur, engine)
			}
			prev = cur
		}
	}

	// group(a.s(), b.s()) → fan-out: all parallel, no inter-task edge. We still
	// register the tasks + workflow so the DAG nodes exist; the group has no
	// internal ordering. Recorded honestly as parallel members.
	for _, idx := range celeryGroupCallRe.FindAllStringIndex(src, -1) {
		argStr := extractCallArgString(src, idx[1]-1)
		for _, name := range celeryCanvasSignatures(argStr) {
			id := taskID(name)
			// stamp the group role so the node carries fan-out semantics
			em.markRole(id, "group_member")
		}
	}

	// chord(header)(callback) → fan-in: every header task → callback.
	for _, idx := range celeryChordCallRe.FindAllStringIndex(src, -1) {
		// header is the first call's args; callback is the second call's args.
		headerArgs := extractCallArgString(src, idx[1]-1)
		headerNames := celeryCanvasSignatures(headerArgs)
		// Find the callback: the call group immediately after the header call's
		// closing paren — `chord(header)(callback)`.
		closePos := matchParenEnd(src, idx[1]-1)
		callbackNames := []string{}
		if closePos >= 0 && closePos+1 < len(src) {
			rest := strings.TrimLeft(src[closePos+1:], " \t")
			if strings.HasPrefix(rest, "(") {
				cbArgs := extractCallArgString(src, closePos+1+(len(src[closePos+1:])-len(rest)))
				callbackNames = celeryCanvasSignatures(cbArgs)
			}
		}
		for _, h := range headerNames {
			hid := taskID(h)
			for _, c := range callbackNames {
				cid := taskID(c)
				em.dependsOn(hid, cid, engine) // header runs before callback
			}
		}
	}
}

// celeryCanvasSignatures returns the ordered task names from a canvas arg list,
// e.g. `fetch.s(), process.s(x)` → ["fetch", "process"]. Nested chain/group are
// flattened in source order (honest-partial: nesting topology is collapsed).
func celeryCanvasSignatures(argStr string) []string {
	var out []string
	for _, m := range celerySigRe.FindAllStringSubmatch(argStr, -1) {
		name := m[1]
		// Skip the canvas combinators themselves if they slipped in.
		if name == "chain" || name == "group" || name == "chord" {
			continue
		}
		if looksLikeFunctionName(name) {
			out = append(out, name)
		}
	}
	return out
}

// markRole stamps a node_role-ish property on an already-emitted entity.
func (e *dagEmitter) markRole(id, role string) {
	for i := range e.entities {
		if e.entities[i].Name == id {
			if e.entities[i].Properties == nil {
				e.entities[i].Properties = map[string]string{}
			}
			e.entities[i].Properties["canvas_role"] = role
			return
		}
	}
}

// extractCallArgString returns the text between the matching parens of a call
// whose '(' is at or after openParenPos.
func extractCallArgString(src string, openParenPos int) string {
	for openParenPos < len(src) && src[openParenPos] != '(' {
		openParenPos++
	}
	end := matchParenEnd(src, openParenPos)
	if end < 0 || end <= openParenPos {
		return ""
	}
	return src[openParenPos+1 : end]
}

// matchParenEnd finds the index of the ')' matching the '(' at or after pos.
func matchParenEnd(src string, pos int) int {
	for pos < len(src) && src[pos] != '(' {
		pos++
	}
	if pos >= len(src) {
		return -1
	}
	depth := 0
	for i := pos; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Argo Workflows (YAML)
// ---------------------------------------------------------------------------

// argoGuardRe gates the YAML scan to Argo Workflow manifests.
var argoGuardRe = regexp.MustCompile(`argoproj\.io|kind:\s*["']?(?:Workflow|WorkflowTemplate|CronWorkflow)\b`)

// argoManifest is the minimal Argo Workflow shape we decode.
type argoManifest struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name         string `yaml:"name"`
		GenerateName string `yaml:"generateName"`
	} `yaml:"metadata"`
	Spec struct {
		Entrypoint string         `yaml:"entrypoint"`
		Templates  []argoTemplate `yaml:"templates"`
	} `yaml:"spec"`
}

type argoTemplate struct {
	Name  string `yaml:"name"`
	Steps [][]struct {
		Name     string `yaml:"name"`
		Template string `yaml:"template"`
	} `yaml:"steps"`
	DAG struct {
		Tasks []struct {
			Name         string   `yaml:"name"`
			Template     string   `yaml:"template"`
			Dependencies []string `yaml:"dependencies"`
		} `yaml:"tasks"`
	} `yaml:"dag"`
}

func synthesizeArgoWorkflow(content []byte, em *dagEmitter) {
	if !argoGuardRe.Match(content) {
		return
	}
	const engine = "argo"

	dec := yaml.NewDecoder(strings.NewReader(string(content)))
	for {
		var mf argoManifest
		if err := dec.Decode(&mf); err != nil {
			break
		}
		if !argoKind(mf.Kind) {
			continue
		}
		dagName := mf.Metadata.Name
		if dagName == "" {
			dagName = strings.TrimRight(mf.Metadata.GenerateName, "-")
		}
		if dagName == "" {
			dagName = "argo-workflow"
		}
		wfID := dagWorkflowID(engine, dagName)

		task := func(name string) string {
			id := dagTaskID(engine, dagName, name)
			em.task(id, name, engine, nil)
			em.workflow(wfID, dagName, engine, nil)
			em.executes(wfID, id, engine)
			return id
		}

		for _, tmpl := range mf.Spec.Templates {
			// dag.tasks with explicit dependencies → dep → task edges.
			for _, t := range tmpl.DAG.Tasks {
				if t.Name == "" {
					continue
				}
				cur := task(t.Name)
				for _, dep := range t.Dependencies {
					if dep == "" {
						continue
					}
					depID := task(dep)
					em.dependsOn(depID, cur, engine) // dependency runs before task
				}
			}
			// steps: sequential stages; each stage runs after the previous one.
			var prevStage []string
			for _, stage := range tmpl.Steps {
				var curStage []string
				for _, s := range stage {
					if s.Name == "" {
						continue
					}
					id := task(s.Name)
					curStage = append(curStage, id)
				}
				for _, prev := range prevStage {
					for _, cur := range curStage {
						em.dependsOn(prev, cur, engine)
					}
				}
				if len(curStage) > 0 {
					prevStage = curStage
				}
			}
		}
	}
}

func argoKind(k string) bool {
	switch k {
	case "Workflow", "WorkflowTemplate", "CronWorkflow", "ClusterWorkflowTemplate":
		return true
	default:
		return false
	}
}
