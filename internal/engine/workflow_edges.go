// Workflow orchestration edges — #934.
//
// This pass detects workflow definitions, activity definitions, workflow
// invocations, and activity calls for three orchestration engines:
//
//   - Temporal (Python, Go, Java/Kotlin SDKs)
//   - Cadence (Java @WorkflowMethod / @ActivityMethod SDK)
//   - AWS Step Functions (ASL JSON state machines, CDK / Terraform / CloudFormation)
//
// Entities emitted:
//   - SCOPE.Workflow      — a workflow class / function
//   - SCOPE.Activity      — an activity function / method
//   - SCOPE.StateMachine  — an AWS Step Functions state machine
//
// Edge kinds emitted:
//   - STARTS_WORKFLOW          — producer (client.start_workflow / ExecuteWorkflow) → Workflow
//   - EXECUTES_ACTIVITY        — Workflow → Activity (within-workflow call)
//   - STEPFUNCTION_STEP_INVOKES — StateMachine Task state → Lambda / target entity
//
// Cross-repo matching follows the same strategy used by Kafka (#726) and gRPC
// (#725): both sides emit synthetic entities keyed by:
//   - workflow:temporal:<WorkflowName>
//   - activity:temporal:<ActivityName>
//   - statemachine:aws-sfn:<StateMachineName>
//
// SourceFile is set to "" on synthetics so the import-channel linker joins
// them without new linker code.
//
// # Temporal — Python SDK
//
// Workflow definition: `@workflow.defn\nclass OrderWF` or
// `@workflow.defn(name="...")` decorator.
// Workflow run method: `@workflow.run\nasync def run(`.
// Activity definition: `@activity.defn\nasync def charge_card(`.
// Client trigger: `client.start_workflow(OrderWF.run, ...)` or
// `handle = await client.start_workflow(...)`.
// Within-workflow activity call: `await workflow.execute_activity(charge_card, ...)`.
//
// # Temporal — Go SDK
//
// Workflow registration: `w.RegisterWorkflow(OrderWorkflow)` or
// `worker.RegisterWorkflow(OrderWorkflow)`.
// Activity registration: `w.RegisterActivity(ChargeCard)` or
// `worker.RegisterActivity(ChargeCard)`.
// Client trigger: `temporalClient.ExecuteWorkflow(ctx, opts, OrderWorkflow, ...)`.
// Within-workflow activity call: `workflow.ExecuteActivity(ctx, ChargeCard, ...)`.
//
// # Temporal / Cadence — Java SDK
//
// Workflow interface: `@WorkflowInterface` + `@WorkflowMethod` on methods.
// Activity interface: `@ActivityInterface` + `@ActivityMethod` on methods.
// Workflow implementation: `WorkflowImpl implements Workflow` (detected via class name).
// Client trigger: `WorkflowClient.start(stub, ...)` or
// `client.newWorkflowStub(OrderWF.class)`.
// Activity call: (stub called directly from workflow; captured by stub method call heuristic).
//
// # AWS Step Functions — ASL JSON
//
// State machine: `*.asl.json` files with a `States` top-level key, or
// Terraform `aws_sfn_state_machine` resource blocks with a JSON definition.
// CDK `new StateMachine(this, "name", {...})`.
// Task state: `"Type": "Task"` with `"Resource": "arn:aws:lambda:..."` or
// `"Resource": "arn:aws:states:::lambda:invoke"`.
// StateMachine invocation: `StepFunctions.start_execution({stateMachineArn: '...'})`,
// `sfn.StartExecution(ctx, &sfn.StartExecutionInput{StateMachineArn: ...})`,
// `new StartExecutionCommand({stateMachineArn: ...})`.
//
// The Lambda ARN extracted from Task states is matched against the `aws-lambda:`
// entity ID emitted by #940 (serverless_edges.go) to produce
// STEPFUNCTION_STEP_INVOKES edges that bridge the two passes without new
// linker code.
//
// # Scope guard
//
// Append-only — this pass never modifies or removes existing entities or edges,
// so it cannot regress the bug-rate of the surrounding pipeline.
//
// Refs #934. Depends on #940 for Lambda entity resolution.
package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Entity kinds
// ---------------------------------------------------------------------------

const workflowKind = "SCOPE.Workflow"
const activityKind = "SCOPE.Activity"
const stateMachineKind = "SCOPE.StateMachine"

// ---------------------------------------------------------------------------
// Edge kinds
// ---------------------------------------------------------------------------

const startsWorkflowEdgeKind = "STARTS_WORKFLOW"
const executesActivityEdgeKind = "EXECUTES_ACTIVITY"
const stepFunctionStepInvokesEdgeKind = "STEPFUNCTION_STEP_INVOKES"

// ---------------------------------------------------------------------------
// Synthetic entity ID helpers
// ---------------------------------------------------------------------------

func temporalWorkflowID(name string) string { return "workflow:temporal:" + name }
func temporalActivityID(name string) string { return "activity:temporal:" + name }
func sfnStateMachineID(name string) string  { return "statemachine:aws-sfn:" + name }

// ---------------------------------------------------------------------------
// Language gate
// ---------------------------------------------------------------------------

// workflowSynthesisSupportsLanguage reports whether applyWorkflowEdges emits
// synthetics for the given language tag.
func workflowSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "python", "go", "java", "kotlin", "javascript", "typescript", "json":
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

// applyWorkflowEdges is an append-only pass that detects Temporal / Cadence /
// Step Functions patterns and emits SCOPE.Workflow, SCOPE.Activity,
// SCOPE.StateMachine entities plus STARTS_WORKFLOW, EXECUTES_ACTIVITY, and
// STEPFUNCTION_STEP_INVOKES edges.
func applyWorkflowEdges(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}

	// Path-based routing: *.asl.json and terraform files go through the ASL
	// parser regardless of language tag.
	lowerPath := strings.ToLower(path)
	isASLFile := strings.HasSuffix(lowerPath, ".asl.json") ||
		strings.HasSuffix(lowerPath, ".asl.yaml") ||
		strings.HasSuffix(lowerPath, ".asl.yml")
	isTerraformFile := strings.HasSuffix(lowerPath, ".tf") || strings.HasSuffix(lowerPath, ".tf.json")
	isCloudFormation := strings.HasSuffix(lowerPath, "cloudformation.json") ||
		strings.HasSuffix(lowerPath, "cloudformation.yaml") ||
		strings.HasSuffix(lowerPath, "template.yaml") ||
		strings.HasSuffix(lowerPath, "template.json")

	if isASLFile {
		return applyASLWorkflowEdges(path, content, entities, relationships)
	}
	if isTerraformFile {
		return applyTerraformSFNEdges(path, content, entities, relationships)
	}
	if isCloudFormation {
		return applyCloudFormationSFNEdges(path, content, entities, relationships)
	}

	if !workflowSynthesisSupportsLanguage(lang) {
		return entities, relationships
	}

	src := string(content)

	// Dedup helpers.
	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}

	emitWorkflow := func(wfID, wfName, engine string, props map[string]string) {
		if seenEnt[wfID] {
			return
		}
		seenEnt[wfID] = true
		merged := map[string]string{
			"workflow_engine": engine,
			"workflow_name":   wfName,
			"pattern_type":    "workflow_synthesis",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		entities = append(entities, types.EntityRecord{
			Name:               wfID,
			Kind:               workflowKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.85,
		})
	}

	emitActivity := func(actID, actName, engine string, props map[string]string) {
		if seenEnt[actID] {
			return
		}
		seenEnt[actID] = true
		merged := map[string]string{
			"workflow_engine": engine,
			"activity_name":   actName,
			"pattern_type":    "workflow_synthesis",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		entities = append(entities, types.EntityRecord{
			Name:               actID,
			Kind:               activityKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.85,
		})
	}

	emitStartsWorkflow := func(callerKind, callerName, wfID, engine string) {
		if callerName == "" || wfID == "" {
			return
		}
		key := startsWorkflowEdgeKind + "|" + callerKind + ":" + callerName + "|" + wfID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fmt.Sprintf("%s:%s", callerKind, callerName),
			ToID:   fmt.Sprintf("%s:%s", workflowKind, wfID),
			Kind:   startsWorkflowEdgeKind,
			Properties: map[string]string{
				"workflow_engine": engine,
				"pattern_type":    "workflow_synthesis",
			},
		})
	}

	emitExecutesActivity := func(wfID, actID, engine string) {
		if wfID == "" || actID == "" {
			return
		}
		key := executesActivityEdgeKind + "|" + wfID + "|" + actID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fmt.Sprintf("%s:%s", workflowKind, wfID),
			ToID:   fmt.Sprintf("%s:%s", activityKind, actID),
			Kind:   executesActivityEdgeKind,
			Properties: map[string]string{
				"workflow_engine": engine,
				"pattern_type":    "workflow_synthesis",
			},
		})
	}

	switch lang {
	case "python":
		synthesizePyWorkflow(src, path, emitWorkflow, emitActivity, emitStartsWorkflow, emitExecutesActivity)
	case "go":
		synthesizeGoWorkflow(src, path, emitWorkflow, emitActivity, emitStartsWorkflow, emitExecutesActivity)
	case "java", "kotlin":
		synthesizeJavaWorkflow(src, path, emitWorkflow, emitActivity, emitStartsWorkflow, emitExecutesActivity)
	case "javascript", "typescript":
		synthesizeNodeWorkflow(src, path, emitWorkflow, emitActivity, emitStartsWorkflow, emitExecutesActivity)
	case "json":
		// Inline JSON in a .json file (not .asl.json): scan for CDK StateMachine
		// or embedded ASL patterns.
		entities, relationships = applyCDKStateMachineJSON(path, content, entities, relationships)
	}

	return entities, relationships
}

// ---------------------------------------------------------------------------
// Python — Temporal SDK
// ---------------------------------------------------------------------------

// pyWorkflowDefnRe captures `@workflow.defn` decorator (possibly with
// `name=` kwarg) followed by `class WorkflowName`.
// Group 1 = optional name string literal, Group 2 = class name.
var pyWorkflowDefnRe = regexp.MustCompile(`@workflow\.defn(?:\s*\(\s*(?:name\s*=\s*["']([^"'\n\r]*)["'])?\s*\))?\s*\n(?:\s*#[^\n]*\n)*\s*class\s+(\w+)`)

// pyActivityDefnRe captures `@activity.defn` decorator followed by `def actName(`.
// Group 1 = optional name string literal, Group 2 = function name.
var pyActivityDefnRe = regexp.MustCompile(`@activity\.defn(?:\s*\(\s*(?:name\s*=\s*["']([^"'\n\r]*)["'])?\s*\))?\s*\n(?:\s*#[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(`)

// pyWorkflowStartRe captures `client.start_workflow(WorkflowClass.run, ...)` or
// `await client.start_workflow(...)`. Group 1 = workflow class/method reference.
var pyWorkflowStartRe = regexp.MustCompile(`\.start_workflow\s*\(\s*(\w+)(?:\.\w+)?\s*[,)]`)

// pyExecuteActivityRe captures `await workflow.execute_activity(activity_fn, ...)`.
// Group 1 = activity function name.
var pyExecuteActivityRe = regexp.MustCompile(`workflow\.execute_activity\s*\(\s*(\w+)\s*[,)]`)

// pyExecuteActivityMethodRe captures `await workflow.execute_activity_method(...)`.
// Group 1 = activity class reference.
var pyExecuteActivityMethodRe = regexp.MustCompile(`workflow\.execute_activity_method\s*\(\s*(\w+)`)

// pyWorkerRegisterRe captures `Worker(client, task_queue='orders', workflows=[OrderWF], activities=[charge_card])`.
// We use a simpler guard: if the file contains `@workflow.defn` or `@activity.defn`
// it is Temporal-aware. The guard prevents false positives from generic worker/workflow names.
var pyTemporalGuardRe = regexp.MustCompile(`(?:@workflow\.defn|@activity\.defn|workflow\.execute_activity|\.start_workflow\s*\(|from\s+temporalio|import\s+temporalio)`)

func synthesizePyWorkflow(
	src, path string,
	emitWorkflow func(wfID, wfName, engine string, props map[string]string),
	emitActivity func(actID, actName, engine string, props map[string]string),
	emitStartsWorkflow func(callerKind, callerName, wfID, engine string),
	emitExecutesActivity func(wfID, actID, engine string),
) {
	// Fast-path guard — avoid regex work on non-Temporal files.
	if !pyTemporalGuardRe.MatchString(src) {
		return
	}

	// Collect workflow definitions: class name → synthetic ID.
	wfClassToID := map[string]string{}

	for _, m := range pyWorkflowDefnRe.FindAllStringSubmatchIndex(src, -1) {
		// Group 1 (optional name override), Group 2 (class name).
		className := src[m[4]:m[5]]
		logicalName := className
		if m[2] != -1 {
			logicalName = src[m[2]:m[3]]
		}
		if !looksLikeFunctionName(logicalName) {
			continue
		}
		wfID := temporalWorkflowID(logicalName)
		wfClassToID[className] = wfID
		emitWorkflow(wfID, logicalName, "temporal", map[string]string{"class_name": className})
	}

	// Collect activity definitions: fn name → synthetic ID.
	actFnToID := map[string]string{}

	for _, m := range pyActivityDefnRe.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[4]:m[5]]
		logicalName := fnName
		if m[2] != -1 {
			logicalName = src[m[2]:m[3]]
		}
		if !looksLikeFunctionName(logicalName) {
			continue
		}
		actID := temporalActivityID(logicalName)
		actFnToID[fnName] = actID
		emitActivity(actID, logicalName, "temporal", map[string]string{"function_name": fnName})
	}

	// Workflow start invocations: producer → Workflow.
	for _, m := range pyWorkflowStartRe.FindAllStringSubmatchIndex(src, -1) {
		wfRef := src[m[2]:m[3]]
		wfID, ok := wfClassToID[wfRef]
		if !ok {
			// May be a cross-repo call — emit synthetic.
			if !looksLikeFunctionName(wfRef) {
				continue
			}
			wfID = temporalWorkflowID(wfRef)
			emitWorkflow(wfID, wfRef, "temporal", nil)
		}
		caller := findEnclosingPyFunctionName(src, m[0])
		emitStartsWorkflow("SCOPE.Function", caller, wfID, "temporal")
	}

	// Within-workflow activity executions: Workflow → Activity.
	// We need to know what workflow class we are inside.
	// Strategy: for each execute_activity call, find the enclosing class.
	for _, m := range pyExecuteActivityRe.FindAllStringSubmatchIndex(src, -1) {
		actRef := src[m[2]:m[3]]
		actID, ok := actFnToID[actRef]
		if !ok {
			if !looksLikeFunctionName(actRef) {
				continue
			}
			actID = temporalActivityID(actRef)
			emitActivity(actID, actRef, "temporal", nil)
		}
		enclosingClass := findEnclosingPyClassName(src, m[0])
		wfID, hasWF := wfClassToID[enclosingClass]
		if !hasWF {
			// Best effort: emit with anonymous workflow context.
			wfID = temporalWorkflowID(enclosingClass)
			emitWorkflow(wfID, enclosingClass, "temporal", nil)
		}
		emitExecutesActivity(wfID, actID, "temporal")
	}

	// execute_activity_method variant.
	for _, m := range pyExecuteActivityMethodRe.FindAllStringSubmatchIndex(src, -1) {
		actRef := src[m[2]:m[3]]
		if !looksLikeFunctionName(actRef) {
			continue
		}
		actID, ok := actFnToID[actRef]
		if !ok {
			actID = temporalActivityID(actRef)
			emitActivity(actID, actRef, "temporal", nil)
		}
		enclosingClass := findEnclosingPyClassName(src, m[0])
		wfID, hasWF := wfClassToID[enclosingClass]
		if !hasWF {
			wfID = temporalWorkflowID(enclosingClass)
			emitWorkflow(wfID, enclosingClass, "temporal", nil)
		}
		emitExecutesActivity(wfID, actID, "temporal")
	}
}

// wfPyClassDefRe finds `class ClassName(` declarations in workflow source files.
var wfPyClassDefRe = regexp.MustCompile(`(?m)^\s*class\s+(\w+)\s*[:(]`)

// findEnclosingPyClassName walks backward from offset to find the nearest
// class declaration in Python source.
func findEnclosingPyClassName(src string, offset int) string {
	start := offset - 8000
	if start < 0 {
		start = 0
	}
	window := src[start:offset]
	matches := wfPyClassDefRe.FindAllStringSubmatch(window, -1)
	if len(matches) == 0 {
		return "module"
	}
	return matches[len(matches)-1][1]
}

// ---------------------------------------------------------------------------
// Go — Temporal SDK
// ---------------------------------------------------------------------------

// goTemporalGuardRe is a fast-path guard for Go files: matches Temporal SDK
// import paths or registration calls.
var goTemporalGuardRe = regexp.MustCompile(`(?:go\.temporal\.io|cadenceworkflow\.io|temporal\.io/sdk|w\.RegisterWorkflow|worker\.RegisterWorkflow|workflow\.ExecuteActivity|temporalClient\.ExecuteWorkflow|\.ExecuteWorkflow\s*\()`)

// goRegisterWorkflowRe captures `w.RegisterWorkflow(OrderWorkflow)` or
// `worker.RegisterWorkflow(OrderWorkflow)`. Group 1 = workflow function name.
var goRegisterWorkflowRe = regexp.MustCompile(`(?:\bw\b|\bworker\b)\.RegisterWorkflow\s*\(\s*(\w+)\s*\)`)

// goRegisterActivityRe captures `w.RegisterActivity(ChargeCard)` or
// `worker.RegisterActivity(ChargeCard)`. Group 1 = activity function name.
var goRegisterActivityRe = regexp.MustCompile(`(?:\bw\b|\bworker\b)\.RegisterActivity\s*\(\s*(\w+)\s*\)`)

// goExecuteWorkflowRe locates .ExecuteWorkflow( call sites. We capture only
// the call-site offset and then use extractGoCallArg to pull the 3rd positional
// argument (the workflow function reference) while skipping nested braces/parens.
var goExecuteWorkflowRe = regexp.MustCompile(`(\w+)\.ExecuteWorkflow\s*\(`)

// goExecuteActivityRe captures `workflow.ExecuteActivity(ctx, ActivityFn, args...)`.
// Group 1 = activity function reference.
var goExecuteActivityRe = regexp.MustCompile(`workflow\.ExecuteActivity\s*\([^,)]+,\s*(\w+)\s*[,)]`)

func synthesizeGoWorkflow(
	src, path string,
	emitWorkflow func(wfID, wfName, engine string, props map[string]string),
	emitActivity func(actID, actName, engine string, props map[string]string),
	emitStartsWorkflow func(callerKind, callerName, wfID, engine string),
	emitExecutesActivity func(wfID, actID, engine string),
) {
	if !goTemporalGuardRe.MatchString(src) {
		return
	}

	// Map from registered function name → synthetic ID.
	wfFnToID := map[string]string{}
	actFnToID := map[string]string{}

	for _, m := range goRegisterWorkflowRe.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[2]:m[3]]
		if !looksLikeFunctionName(fnName) {
			continue
		}
		wfID := temporalWorkflowID(fnName)
		wfFnToID[fnName] = wfID
		emitWorkflow(wfID, fnName, "temporal", map[string]string{"go_func": fnName})
	}

	for _, m := range goRegisterActivityRe.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[2]:m[3]]
		if !looksLikeFunctionName(fnName) {
			continue
		}
		actID := temporalActivityID(fnName)
		actFnToID[fnName] = actID
		emitActivity(actID, fnName, "temporal", map[string]string{"go_func": fnName})
	}

	// ExecuteWorkflow: producer → Workflow.
	// Temporal SDK: ExecuteWorkflow(ctx, options, WorkflowFn, args...)
	// The workflow function is the 3rd positional argument (0-indexed: index 2).
	for _, m := range goExecuteWorkflowRe.FindAllStringSubmatchIndex(src, -1) {
		// m[1] is the offset of the opening paren.
		wfRef := extractGoCallArg(src, m[1]-1, 2) // 0-indexed 3rd arg
		if !looksLikeFunctionName(wfRef) {
			continue
		}
		wfID, ok := wfFnToID[wfRef]
		if !ok {
			wfID = temporalWorkflowID(wfRef)
			emitWorkflow(wfID, wfRef, "temporal", nil)
		}
		caller := findEnclosingGoFunctionName(src, m[0])
		emitStartsWorkflow("SCOPE.Function", caller, wfID, "temporal")
	}

	// ExecuteActivity: Workflow → Activity.
	for _, m := range goExecuteActivityRe.FindAllStringSubmatchIndex(src, -1) {
		actRef := src[m[2]:m[3]]
		if !looksLikeFunctionName(actRef) {
			continue
		}
		actID, ok := actFnToID[actRef]
		if !ok {
			actID = temporalActivityID(actRef)
			emitActivity(actID, actRef, "temporal", nil)
		}
		// Enclosing Go function is the workflow body.
		callerFn := findEnclosingGoFunctionName(src, m[0])
		wfID, ok := wfFnToID[callerFn]
		if !ok {
			wfID = temporalWorkflowID(callerFn)
			emitWorkflow(wfID, callerFn, "temporal", nil)
		}
		emitExecutesActivity(wfID, actID, "temporal")
	}
}

// extractGoCallArg extracts the Nth positional argument (0-indexed) from a
// function call starting with an opening paren at position `openParenPos`.
// Handles nested braces, brackets, and parentheses. Returns the trimmed arg
// token, or "" if not found.
func extractGoCallArg(src string, openParenPos int, n int) string {
	// Find the opening paren.
	for openParenPos < len(src) && src[openParenPos] != '(' {
		openParenPos++
	}
	if openParenPos >= len(src) {
		return ""
	}
	depth := 0
	commas := 0
	argStart := openParenPos + 1
	for i := openParenPos; i < len(src); i++ {
		switch src[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
			if depth == 0 {
				// End of call. If we're at the right arg index, extract it.
				if commas == n {
					arg := strings.TrimSpace(src[argStart:i])
					return arg
				}
				return ""
			}
		case ',':
			if depth == 1 {
				if commas == n {
					arg := strings.TrimSpace(src[argStart:i])
					return arg
				}
				commas++
				argStart = i + 1
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Temporal + Cadence SDKs
// ---------------------------------------------------------------------------

// javaTemporalGuardRe fast-path for Java files.
var javaTemporalGuardRe = regexp.MustCompile(`(?:@WorkflowInterface|@ActivityInterface|@WorkflowMethod|@ActivityMethod|WorkflowClient|io\.temporal|com\.uber\.cadence|WorkflowImplementationOptions)`)

// javaWorkflowInterfaceRe captures `@WorkflowInterface` followed by
// `interface WorkflowName` or `class WorkflowName`. Group 1 = name.
var javaWorkflowInterfaceRe = regexp.MustCompile(`@WorkflowInterface\s*\n(?:\s*(?:public|abstract|@\w+[^\n]*)?\n)*\s*(?:public\s+)?(?:interface|class)\s+(\w+)`)

// javaActivityInterfaceRe captures `@ActivityInterface`. Group 1 = name.
var javaActivityInterfaceRe = regexp.MustCompile(`@ActivityInterface\s*\n(?:\s*(?:public|abstract|@\w+[^\n]*)?\n)*\s*(?:public\s+)?(?:interface|class)\s+(\w+)`)

// javaWorkflowMethodRe captures `@WorkflowMethod` on a method. We extract the
// enclosing interface/class name from the wider context (best-effort).
var javaWorkflowMethodRe = regexp.MustCompile(`@WorkflowMethod`)

// javaActivityMethodRe captures `@ActivityMethod` on a method.
var javaActivityMethodRe = regexp.MustCompile(`@ActivityMethod`)

// javaNewWorkflowStubRe captures `client.newWorkflowStub(OrderWF.class, ...)` or
// `workflowClient.newWorkflowStub(OrderWF.class)`. Group 1 = workflow class name.
var javaNewWorkflowStubRe = regexp.MustCompile(`\.newWorkflowStub\s*\(\s*(\w+)\.class`)

// javaWorkflowClientStartRe captures `WorkflowClient.start(stub, ...)` or
// `client.start(stub, ...)`. Group 1 = stub variable (best-effort).
var javaWorkflowClientStartRe = regexp.MustCompile(`WorkflowClient\.start\s*\(\s*(\w+)`)

// javaExecuteActivityRe captures `activities.chargeCard(...)` — stub method call
// on an activity variable. Too broad alone, so guarded by @ActivityInterface presence.
// Group 1 = variable, Group 2 = method name.
var javaExecuteActivityRe = regexp.MustCompile(`(\w+)\.(\w+)\s*\([^)]*\)\s*;`)

func synthesizeJavaWorkflow(
	src, path string,
	emitWorkflow func(wfID, wfName, engine string, props map[string]string),
	emitActivity func(actID, actName, engine string, props map[string]string),
	emitStartsWorkflow func(callerKind, callerName, wfID, engine string),
	emitExecutesActivity func(wfID, actID, engine string),
) {
	if !javaTemporalGuardRe.MatchString(src) {
		return
	}

	engine := "temporal"
	if strings.Contains(src, "com.uber.cadence") {
		engine = "cadence"
	}

	// Workflow interface / class definitions.
	wfNameToID := map[string]string{}
	for _, m := range javaWorkflowInterfaceRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if !looksLikeFunctionName(name) {
			continue
		}
		wfID := temporalWorkflowID(name)
		wfNameToID[name] = wfID
		emitWorkflow(wfID, name, engine, map[string]string{"java_class": name})
	}

	// Activity interface / class definitions.
	actNameToID := map[string]string{}
	for _, m := range javaActivityInterfaceRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if !looksLikeFunctionName(name) {
			continue
		}
		actID := temporalActivityID(name)
		actNameToID[name] = actID
		emitActivity(actID, name, engine, map[string]string{"java_class": name})
	}

	// Client trigger: .newWorkflowStub(OrderWF.class)
	for _, m := range javaNewWorkflowStubRe.FindAllStringSubmatchIndex(src, -1) {
		wfName := src[m[2]:m[3]]
		wfID, ok := wfNameToID[wfName]
		if !ok {
			if !looksLikeFunctionName(wfName) {
				continue
			}
			wfID = temporalWorkflowID(wfName)
			emitWorkflow(wfID, wfName, engine, nil)
		}
		className := ""
		if cm := classNameRe.FindStringSubmatch(src); len(cm) >= 2 {
			className = cm[1]
		}
		if className == "" {
			className = "unknown"
		}
		emitStartsWorkflow("SCOPE.Class", className, wfID, engine)
	}

	// WorkflowClient.start(stub, ...)
	for _, m := range javaWorkflowClientStartRe.FindAllStringSubmatchIndex(src, -1) {
		stubVar := src[m[2]:m[3]]
		// Try to find its type: `OrderWF stub = ...`
		stubTypeRe := regexp.MustCompile(`(\w+)\s+` + regexp.QuoteMeta(stubVar) + `\s*=`)
		if tm := stubTypeRe.FindStringSubmatch(src); len(tm) >= 2 {
			wfName := tm[1]
			wfID, ok := wfNameToID[wfName]
			if !ok {
				if looksLikeFunctionName(wfName) {
					wfID = temporalWorkflowID(wfName)
					emitWorkflow(wfID, wfName, engine, nil)
				}
			}
			if wfID != "" {
				className := ""
				if cm := classNameRe.FindStringSubmatch(src); len(cm) >= 2 {
					className = cm[1]
				}
				if className == "" {
					className = "unknown"
				}
				emitStartsWorkflow("SCOPE.Class", className, wfID, engine)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Node / TypeScript — Temporal SDK
// ---------------------------------------------------------------------------

// nodeTemporalGuardRe fast-path for Node/TS files.
var nodeTemporalGuardRe = regexp.MustCompile(`(?:@temporalio|temporalio/|@cadenceworkflow|proxyActivities|executeChild|WorkflowClient|client\.start\s*\()`)

// nodeProxyActivitiesRe captures:
//   - `const { chargeCard } = proxyActivities<Activities>(...)` (destructuring before call)
//   - `proxyActivities<Activities>({...})` followed by const { ... }
//
// Group 1 = destructured names (comma-separated).
var nodeProxyActivitiesRe = regexp.MustCompile(`(?:const|let|var)\s+\{([^}]+)\}\s*=\s*proxyActivities`)

// nodeProxyActivitiesAltRe captures `const acts = proxyActivities(...)`.
var nodeProxyActivitiesAltRe = regexp.MustCompile(`(?:const|let|var)\s+(\w+)\s*=\s*proxyActivities\s*(?:<[^>]*>)?\s*\(`)

// nodeWorkflowDefRe captures `export async function workflowName(` or
// `export function workflowName(`. Group 1 = workflow function name.
var nodeWorkflowDefRe = regexp.MustCompile(`(?m)export\s+(?:async\s+)?function\s+(\w+)\s*\(`)

// nodeActivityDefRe captures exported activity functions in an activities file.
// We detect them by the file path containing "activities" or by `@Activity` decorator.
// Group 1 = function name.
var nodeActivityDefRe = regexp.MustCompile(`(?m)export\s+(?:async\s+)?function\s+(\w+)\s*\(`)

// nodeClientStartRe captures `client.start(workflowFn, { ... })` or
// `handle = await client.start(workflowFn, ...)`. Group 1 = workflow fn name.
var nodeClientStartRe = regexp.MustCompile(`client\.start\s*\(\s*(\w+)\s*[,)]`)

// nodeActivityCallRe captures `await acts.chargeCard(...)` where acts is a
// proxyActivities result. Group 1 = activity name (method on proxy).
var nodeActivityCallRe = regexp.MustCompile(`await\s+(\w+)\.(\w+)\s*\(`)

func synthesizeNodeWorkflow(
	src, path string,
	emitWorkflow func(wfID, wfName, engine string, props map[string]string),
	emitActivity func(actID, actName, engine string, props map[string]string),
	emitStartsWorkflow func(callerKind, callerName, wfID, engine string),
	emitExecutesActivity func(wfID, actID, engine string),
) {
	if !nodeTemporalGuardRe.MatchString(src) {
		return
	}

	// Detect workflow file vs activity file by path convention and content.
	lowerPath := strings.ToLower(path)
	isWorkflowFile := strings.Contains(lowerPath, "workflow") || strings.Contains(src, "proxyActivities")
	isActivityFile := strings.Contains(lowerPath, "activit")

	wfFnToID := map[string]string{}

	if isWorkflowFile {
		// Exported workflow functions.
		for _, m := range nodeWorkflowDefRe.FindAllStringSubmatchIndex(src, -1) {
			fnName := src[m[2]:m[3]]
			if !looksLikeFunctionName(fnName) {
				continue
			}
			wfID := temporalWorkflowID(fnName)
			wfFnToID[fnName] = wfID
			emitWorkflow(wfID, fnName, "temporal", map[string]string{"ts_func": fnName})
		}

		// proxyActivities destructuring: collect activity names called in this workflow.
		proxyVars := map[string]bool{}
		proxyActNames := []string{}

		for _, m := range nodeProxyActivitiesRe.FindAllStringSubmatchIndex(src, -1) {
			names := src[m[2]:m[3]]
			for _, n := range strings.Split(names, ",") {
				n = strings.TrimSpace(n)
				if looksLikeFunctionName(n) {
					proxyActNames = append(proxyActNames, n)
				}
			}
		}
		for _, m := range nodeProxyActivitiesAltRe.FindAllStringSubmatchIndex(src, -1) {
			varName := src[m[2]:m[3]]
			proxyVars[varName] = true
		}

		// Emit activity synthetics for proxied activities.
		actFnToID := map[string]string{}
		for _, actName := range proxyActNames {
			actID := temporalActivityID(actName)
			actFnToID[actName] = actID
			emitActivity(actID, actName, "temporal", nil)
		}

		// For workflow → activity edges: match `await actProxy.method(` calls
		// where actProxy is one of the proxy variables or the destructured names
		// are called directly.
		for _, m := range nodeActivityCallRe.FindAllStringSubmatchIndex(src, -1) {
			varName := src[m[2]:m[3]]
			methodName := src[m[4]:m[5]]
			if proxyVars[varName] {
				// `await acts.chargeCard(...)` — varName is a proxyActivities result.
				actID, ok := actFnToID[methodName]
				if !ok {
					if !looksLikeFunctionName(methodName) {
						continue
					}
					actID = temporalActivityID(methodName)
					emitActivity(actID, methodName, "temporal", nil)
				}
				enclosingFn := findEnclosingNodeFunctionName(src, m[0])
				wfID, ok := wfFnToID[enclosingFn]
				if !ok {
					wfID = temporalWorkflowID(enclosingFn)
					emitWorkflow(wfID, enclosingFn, "temporal", nil)
				}
				emitExecutesActivity(wfID, actID, "temporal")
			}
		}
		// Direct destructured calls: `await chargeCard(...)`.
		for _, actName := range proxyActNames {
			directCallRe := regexp.MustCompile(`await\s+` + regexp.QuoteMeta(actName) + `\s*\(`)
			for _, m := range directCallRe.FindAllStringIndex(src, -1) {
				actID, ok := actFnToID[actName]
				if !ok {
					actID = temporalActivityID(actName)
					emitActivity(actID, actName, "temporal", nil)
				}
				enclosingFn := findEnclosingNodeFunctionName(src, m[0])
				wfID, ok := wfFnToID[enclosingFn]
				if !ok {
					wfID = temporalWorkflowID(enclosingFn)
					emitWorkflow(wfID, enclosingFn, "temporal", nil)
				}
				emitExecutesActivity(wfID, actID, "temporal")
			}
		}
	}

	if isActivityFile && !isWorkflowFile {
		for _, m := range nodeActivityDefRe.FindAllStringSubmatchIndex(src, -1) {
			fnName := src[m[2]:m[3]]
			if !looksLikeFunctionName(fnName) {
				continue
			}
			actID := temporalActivityID(fnName)
			emitActivity(actID, fnName, "temporal", map[string]string{"ts_func": fnName})
		}
	}

	// Client-side start.
	for _, m := range nodeClientStartRe.FindAllStringSubmatchIndex(src, -1) {
		wfRef := src[m[2]:m[3]]
		if !looksLikeFunctionName(wfRef) {
			continue
		}
		wfID, ok := wfFnToID[wfRef]
		if !ok {
			wfID = temporalWorkflowID(wfRef)
			emitWorkflow(wfID, wfRef, "temporal", nil)
		}
		caller := findEnclosingNodeFunctionName(src, m[0])
		emitStartsWorkflow("SCOPE.Function", caller, wfID, "temporal")
	}
}

// ---------------------------------------------------------------------------
// AWS Step Functions — ASL JSON
// ---------------------------------------------------------------------------

// aslStates is the minimal structure we need from an ASL document.
type aslDocument struct {
	Comment string                     `json:"Comment"`
	States  map[string]json.RawMessage `json:"States"`
}

type aslState struct {
	Type     string `json:"Type"`
	Resource string `json:"Resource"`
	Next     string `json:"Next"`
	End      bool   `json:"End"`
}

// sfnLambdaARNRe extracts the function name from a Lambda ARN or
// `arn:aws:states:::lambda:invoke` resource string.
// Group 1 = function name (last colon-separated segment, stripping aliases).
var sfnLambdaARNRe = regexp.MustCompile(`arn:aws(?:-[a-z]+)*:lambda:[^:]+:\d+:function:([^:$/\s]+)`)

// sfnStatesLambdaRe matches `arn:aws:states:::lambda:invoke` style resources.
// The actual Lambda ARN is in the Parameters block which we parse separately.
var sfnStatesLambdaRe = regexp.MustCompile(`arn:aws:states:::lambda:invoke`)

// sfnStateMachineNameFromPath extracts the state machine name from the file path.
// e.g. `infra/statemachines/order-flow.asl.json` → `order-flow`.
func sfnStateMachineNameFromPath(path string) string {
	base := path
	if idx := strings.LastIndexAny(path, "/\\"); idx >= 0 {
		base = path[idx+1:]
	}
	// Strip known suffixes.
	for _, sfx := range []string{".asl.json", ".asl.yaml", ".asl.yml", ".json", ".yaml", ".yml"} {
		if strings.HasSuffix(strings.ToLower(base), sfx) {
			base = base[:len(base)-len(sfx)]
			break
		}
	}
	if base == "" {
		return "state-machine"
	}
	return base
}

// applyASLWorkflowEdges parses an ASL JSON file and emits a SCOPE.StateMachine
// entity plus STEPFUNCTION_STEP_INVOKES edges for each Task state that
// references a Lambda function.
func applyASLWorkflowEdges(
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	var doc aslDocument
	if err := json.Unmarshal(content, &doc); err != nil {
		return entities, relationships
	}
	if len(doc.States) == 0 {
		return entities, relationships
	}

	smName := sfnStateMachineNameFromPath(path)
	smID := sfnStateMachineID(smName)

	entities = append(entities, types.EntityRecord{
		Name:               smID,
		Kind:               stateMachineKind,
		SourceFile:         path,
		Language:           "json",
		Properties:         map[string]string{"sm_name": smName, "pattern_type": "workflow_synthesis"},
		EnrichmentRequired: false,
		EnrichmentStatus:   types.StatusPending,
		QualityScore:       0.9,
	})

	seenEdge := map[string]bool{}

	emitStep := func(smID, targetID string, stepName string) {
		key := stepFunctionStepInvokesEdgeKind + "|" + smID + "|" + targetID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fmt.Sprintf("%s:%s", stateMachineKind, smID),
			ToID:   targetID,
			Kind:   stepFunctionStepInvokesEdgeKind,
			Properties: map[string]string{
				"step_name":       stepName,
				"workflow_engine": "aws-sfn",
				"pattern_type":    "workflow_synthesis",
			},
		})
	}

	for stateName, rawState := range doc.States {
		var st aslState
		if err := json.Unmarshal(rawState, &st); err != nil {
			continue
		}
		if st.Type != "Task" || st.Resource == "" {
			continue
		}
		// Direct Lambda ARN: arn:aws:lambda:region:account:function:FunctionName
		if m := sfnLambdaARNRe.FindStringSubmatch(st.Resource); len(m) >= 2 {
			fnName := m[1]
			lambdaEntityID := lambdaFunctionID(fnName)
			// ToID points at the SCOPE.ServerlessFunction entity from #940.
			targetID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaEntityID)
			emitStep(smID, targetID, stateName)
		} else if sfnStatesLambdaRe.MatchString(st.Resource) {
			// `arn:aws:states:::lambda:invoke` — Parameters block has the ARN.
			// Parse the raw state JSON for FunctionName in Parameters.
			extractLambdaFromParams(rawState, smID, stateName, emitStep, &entities, relationships)
		}
	}

	return entities, relationships
}

// aslParams is a partial structure for the Parameters block in an ASL Task state.
type aslParams struct {
	FunctionName  string `json:"FunctionName"`
	FunctionName2 string `json:"FunctionName.$"` // input path reference — skip
}
type aslStateWithParams struct {
	Type       string    `json:"Type"`
	Resource   string    `json:"Resource"`
	Parameters aslParams `json:"Parameters"`
}

func extractLambdaFromParams(
	rawState json.RawMessage,
	smID, stateName string,
	emitStep func(smID, targetID, stepName string),
	entities *[]types.EntityRecord,
	_ []types.RelationshipRecord,
) {
	var st aslStateWithParams
	if err := json.Unmarshal(rawState, &st); err != nil {
		return
	}
	fnName := st.Parameters.FunctionName
	if fnName == "" {
		return
	}
	// fnName may itself be an ARN.
	if m := sfnLambdaARNRe.FindStringSubmatch(fnName); len(m) >= 2 {
		fnName = m[1]
	}
	if !looksLikeFunctionName(fnName) {
		return
	}
	lambdaEntityID := lambdaFunctionID(fnName)
	targetID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaEntityID)
	emitStep(smID, targetID, stateName)
}

// ---------------------------------------------------------------------------
// Terraform SFN — aws_sfn_state_machine resource
// ---------------------------------------------------------------------------

// tfSFNResourceRe detects a Terraform aws_sfn_state_machine resource block.
// Group 1 = resource name (the label, not the SFN name).
var tfSFNResourceRe = regexp.MustCompile(`resource\s+"aws_sfn_state_machine"\s+"(\w+)"\s*\{`)

// tfSFNNameRe captures the `name = "..."` attribute inside the resource block.
// Group 1 = name value.
var tfSFNNameRe = regexp.MustCompile(`name\s*=\s*["']([^"'\n\r]+)["']`)

// tfSFNDefinitionRe captures a JSON string inside the definition attribute (heredoc or string literal).
// We look for "States" as a signal that this is an ASL definition.
var tfSFNDefinitionRe = regexp.MustCompile(`(?s)definition\s*=\s*(?:<<[-\w]*\n(.*?)[-\w]*\n|"((?:[^"\\]|\\.)*)")`)

func applyTerraformSFNEdges(
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	src := string(content)
	if !strings.Contains(src, "aws_sfn_state_machine") {
		return entities, relationships
	}

	// Find each aws_sfn_state_machine resource.
	for _, rm := range tfSFNResourceRe.FindAllStringSubmatchIndex(src, -1) {
		resourceLabel := src[rm[2]:rm[3]]

		// Scan forward from the resource declaration to find the block end.
		// We use a text scan rather than brace-matching because heredoc bodies
		// contain un-balanced JSON braces that confuse a simple brace counter.
		// Instead we scan from rm[1] (after the opening '{') to the next
		// top-level closing '}' that follows a newline.
		blockStart := rm[1]
		blockEnd := findTFResourceBlockEnd(src, blockStart)
		if blockEnd <= blockStart {
			blockEnd = len(src)
		}
		block := src[blockStart:blockEnd]

		// Extract state machine name.
		smName := resourceLabel
		if nm := tfSFNNameRe.FindStringSubmatch(block); len(nm) >= 2 {
			smName = nm[1]
		}
		smID := sfnStateMachineID(smName)

		entities = append(entities, types.EntityRecord{
			Name:               smID,
			Kind:               stateMachineKind,
			SourceFile:         path,
			Language:           "terraform",
			Properties:         map[string]string{"sm_name": smName, "pattern_type": "workflow_synthesis"},
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.85,
		})

		// Extract Lambda ARNs from the full block (covers both inline JSON,
		// heredoc, and quoted string definition forms).
		seenTarget := map[string]bool{}
		for _, lm := range sfnLambdaARNRe.FindAllStringSubmatch(block, -1) {
			fnName := lm[1]
			if !looksLikeFunctionName(fnName) {
				continue
			}
			lambdaEntityID := lambdaFunctionID(fnName)
			targetID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaEntityID)
			if seenTarget[targetID] {
				continue
			}
			seenTarget[targetID] = true
			relationships = append(relationships, types.RelationshipRecord{
				FromID: fmt.Sprintf("%s:%s", stateMachineKind, smID),
				ToID:   targetID,
				Kind:   stepFunctionStepInvokesEdgeKind,
				Properties: map[string]string{
					"workflow_engine": "aws-sfn",
					"pattern_type":    "workflow_synthesis",
				},
			})
		}

		// If the block contains an embedded ASL definition (detected by the
		// "States" key), also parse it for arn:aws:states:::lambda:invoke
		// Parameters blocks.
		if strings.Contains(block, `"States"`) {
			// Extract heredoc body using a non-backreference approach:
			// find <<MARKER, then scan for the matching terminator line.
			if defBody := extractTFHeredocBody(block); defBody != "" &&
				strings.Contains(defBody, `"States"`) {
				// Delegate to full ASL parser for Parameters blocks etc.
				var innerEnts []types.EntityRecord
				var innerRels []types.RelationshipRecord
				innerEnts, innerRels = applyASLWorkflowEdges(smName+".asl.json", []byte(defBody), innerEnts, innerRels)
				// Merge edges only (skip the duplicate SM entity from inner parse).
				fromID := fmt.Sprintf("%s:%s", stateMachineKind, smID)
				for _, r := range innerRels {
					r.FromID = fromID
					relationships = append(relationships, r)
				}
				_ = innerEnts
			}
		}
	}

	return entities, relationships
}

// extractTFHeredocBody finds the first heredoc body inside a Terraform block.
// Terraform heredocs have the form:
//
//	<<MARKER
//	...body...
//	MARKER
//
// Go regex does not support backreferences so we implement this with string ops.
func extractTFHeredocBody(block string) string {
	markerStart := strings.Index(block, "<<")
	if markerStart < 0 {
		return ""
	}
	// Find end of marker name (end of line).
	rest := block[markerStart+2:]
	nlIdx := strings.Index(rest, "\n")
	if nlIdx < 0 {
		return ""
	}
	marker := strings.TrimSpace(rest[:nlIdx])
	if marker == "" {
		return ""
	}
	bodyStart := markerStart + 2 + nlIdx + 1
	if bodyStart >= len(block) {
		return ""
	}
	// Find the terminator line: a line whose trimmed content equals marker.
	body := block[bodyStart:]
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == marker {
			return strings.Join(lines[:i], "\n")
		}
	}
	return ""
}

// findTFResourceBlockEnd finds the end of a Terraform resource block that
// starts just after the opening '{' at `start`. It skips over heredoc sections
// (which can contain unbalanced braces) and tracks HCL brace depth otherwise.
func findTFResourceBlockEnd(src string, start int) int {
	inHeredoc := false
	heredocMarker := ""
	i := start
	depth := 1 // we are already inside the opening '{'

	for i < len(src) {
		if inHeredoc {
			// Look for the heredoc terminator on a line by itself.
			nlIdx := strings.Index(src[i:], "\n")
			if nlIdx < 0 {
				return len(src)
			}
			lineEnd := i + nlIdx
			line := strings.TrimRight(src[i:lineEnd], " \t")
			i = lineEnd + 1
			if line == heredocMarker {
				inHeredoc = false
				heredocMarker = ""
			}
			continue
		}

		ch := src[i]
		switch {
		case ch == '<' && i+1 < len(src) && src[i+1] == '<':
			// Heredoc start: <<MARKER
			j := i + 2
			for j < len(src) && src[j] != '\n' {
				j++
			}
			heredocMarker = strings.TrimSpace(src[i+2 : j])
			inHeredoc = true
			i = j + 1
		case ch == '{':
			depth++
			i++
		case ch == '}':
			depth--
			if depth == 0 {
				return i
			}
			i++
		default:
			i++
		}
	}
	return len(src)
}

// deduplicateEntitiesByName removes duplicate entities with the same Name,
// keeping the FIRST occurrence (the Terraform-sourced one).
func deduplicateEntitiesByName(entities []types.EntityRecord, name string) []types.EntityRecord {
	seen := false
	out := entities[:0]
	for _, e := range entities {
		if e.Name == name {
			if seen {
				continue
			}
			seen = true
		}
		out = append(out, e)
	}
	return out
}

// findMatchingBraceWF finds the closing brace index for the opening brace at
// position `openPos`. Returns -1 if not found.
func findMatchingBraceWF(src string, openPos int) int {
	depth := 0
	for i := openPos; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// CloudFormation — AWS::StepFunctions::StateMachine
// ---------------------------------------------------------------------------

// cfSFNResourceRe detects a CloudFormation StateMachine resource.
var cfSFNResourceRe = regexp.MustCompile(`Type:\s*["']?AWS::StepFunctions::StateMachine["']?`)

// cfSFNLambdaARNRe finds Lambda ARNs inside CloudFormation definitions.
var cfSFNLambdaARNRe = regexp.MustCompile(`arn:aws(?:-[a-z]+)*:lambda:[^:'"]+:\d+:function:([^:'"$/\s\n]+)`)

func applyCloudFormationSFNEdges(
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	src := string(content)
	if !cfSFNResourceRe.MatchString(src) {
		return entities, relationships
	}

	smName := sfnStateMachineNameFromPath(path)
	smID := sfnStateMachineID(smName)

	entities = append(entities, types.EntityRecord{
		Name:               smID,
		Kind:               stateMachineKind,
		SourceFile:         path,
		Language:           "yaml",
		Properties:         map[string]string{"sm_name": smName, "pattern_type": "workflow_synthesis"},
		EnrichmentRequired: false,
		EnrichmentStatus:   types.StatusPending,
		QualityScore:       0.85,
	})

	seenEdge := map[string]bool{}
	for _, m := range cfSFNLambdaARNRe.FindAllStringSubmatch(src, -1) {
		fnName := m[1]
		if !looksLikeFunctionName(fnName) {
			continue
		}
		lambdaEntityID := lambdaFunctionID(fnName)
		targetID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaEntityID)
		key := smID + "|" + targetID
		if seenEdge[key] {
			continue
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fmt.Sprintf("%s:%s", stateMachineKind, smID),
			ToID:   targetID,
			Kind:   stepFunctionStepInvokesEdgeKind,
			Properties: map[string]string{
				"workflow_engine": "aws-sfn",
				"pattern_type":    "workflow_synthesis",
			},
		})
	}

	return entities, relationships
}

// ---------------------------------------------------------------------------
// CDK JSON (inline StateMachine constructs)
// ---------------------------------------------------------------------------

// cdkStateMachineRe detects `new StateMachine(this, "name", {...})` in CDK code.
// Group 1 = construct ID string.
var cdkStateMachineRe = regexp.MustCompile(`new\s+StateMachine\s*\(\s*(?:this|scope|stack)\s*,\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

func applyCDKStateMachineJSON(
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	src := string(content)
	if !strings.Contains(src, "StateMachine") {
		return entities, relationships
	}
	for _, m := range cdkStateMachineRe.FindAllStringSubmatchIndex(src, -1) {
		smName := src[m[2]:m[3]]
		if !looksLikeFunctionName(smName) {
			continue
		}
		smID := sfnStateMachineID(smName)
		entities = append(entities, types.EntityRecord{
			Name:               smID,
			Kind:               stateMachineKind,
			SourceFile:         path,
			Language:           "json",
			Properties:         map[string]string{"sm_name": smName, "pattern_type": "workflow_synthesis"},
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
		// Scan nearby for Lambda ARNs.
		for _, lm := range sfnLambdaARNRe.FindAllStringSubmatch(src, -1) {
			fnName := lm[1]
			if !looksLikeFunctionName(fnName) {
				continue
			}
			lambdaEntityID := lambdaFunctionID(fnName)
			targetID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaEntityID)
			relationships = append(relationships, types.RelationshipRecord{
				FromID: fmt.Sprintf("%s:%s", stateMachineKind, smID),
				ToID:   targetID,
				Kind:   stepFunctionStepInvokesEdgeKind,
				Properties: map[string]string{
					"workflow_engine": "aws-sfn",
					"pattern_type":    "workflow_synthesis",
				},
			})
		}
	}
	return entities, relationships
}

// ---------------------------------------------------------------------------
// Step Functions start_execution invocation (SDK-level)
// ---------------------------------------------------------------------------

// sfnStartExecutionRe captures `sfn.start_execution(StateMachineArn='arn:...')` (Python),
// `sfn.StartExecution(ctx, &sfn.StartExecutionInput{StateMachineArn: aws.String("arn:...")})` (Go),
// `client.send(new StartExecutionCommand({stateMachineArn: 'arn:...'}))` (Node).
// We also detect a short name form `stateMachineArn: 'my-machine-name'`.
var sfnStartExecutionPyRe = regexp.MustCompile(`start_execution\s*\([^)]*[Ss]tate[Mm]achine[Aa]rn\s*=\s*["']([^"'\n\r]+)["']`)
var sfnStartExecutionGoRe = regexp.MustCompile(`[Ss]tate[Mm]achine[Aa]rn\s*:\s*aws\.String\s*\(\s*["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]\s*\)`)
var sfnStartExecutionNodeRe = regexp.MustCompile(`[sS]tate[Mm]achine[Aa]rn\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// sfnGuardRe gates the SFN-invocation scan across languages.
var sfnInvocationGuardRe = regexp.MustCompile(`(?:StartExecution|start_execution|StartExecutionCommand|StepFunctions|stepfunctions|aws-sfn|aws/client.*states)`)

// applySFNStartExecutionEdges scans source code (any language) for
// sfn.start_execution / StartExecution / StartExecutionCommand calls and emits
// STARTS_WORKFLOW edges from the calling function to the state machine entity.
func applySFNStartExecutionEdges(
	lang string,
	src, path string,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if !sfnInvocationGuardRe.MatchString(src) {
		return entities, relationships
	}

	seenEdge := map[string]bool{}
	seenEnt := map[string]bool{}

	emit := func(smRef, callerKind, callerName string) {
		// smRef may be a full ARN or a short name.
		smName := smRef
		if m := regexp.MustCompile(`arn:aws(?:-[a-z]+)*:states:[^:]+:\d+:stateMachine:([^\s"']+)`).FindStringSubmatch(smRef); len(m) >= 2 {
			smName = m[1]
		}
		if !looksLikeFunctionName(smName) {
			return
		}
		smID := sfnStateMachineID(smName)
		if !seenEnt[smID] {
			seenEnt[smID] = true
			entities = append(entities, types.EntityRecord{
				Name:               smID,
				Kind:               stateMachineKind,
				SourceFile:         "",
				Language:           lang,
				Properties:         map[string]string{"sm_name": smName, "pattern_type": "workflow_synthesis"},
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.8,
			})
		}
		key := startsWorkflowEdgeKind + "|" + callerKind + ":" + callerName + "|" + smID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: fmt.Sprintf("%s:%s", callerKind, callerName),
			ToID:   fmt.Sprintf("%s:%s", stateMachineKind, smID),
			Kind:   startsWorkflowEdgeKind,
			Properties: map[string]string{
				"workflow_engine": "aws-sfn",
				"pattern_type":    "workflow_synthesis",
			},
		})
	}

	switch lang {
	case "python":
		for _, m := range sfnStartExecutionPyRe.FindAllStringSubmatchIndex(src, -1) {
			smRef := src[m[2]:m[3]]
			caller := findEnclosingPyFunctionName(src, m[0])
			emit(smRef, "SCOPE.Function", caller)
		}
	case "go":
		for _, m := range sfnStartExecutionGoRe.FindAllStringSubmatchIndex(src, -1) {
			smRef := src[m[2]:m[3]]
			caller := findEnclosingGoFunctionName(src, m[0])
			emit(smRef, "SCOPE.Function", caller)
		}
	case "javascript", "typescript":
		for _, m := range sfnStartExecutionNodeRe.FindAllStringSubmatchIndex(src, -1) {
			smRef := src[m[2]:m[3]]
			caller := findEnclosingNodeFunctionName(src, m[0])
			emit(smRef, "SCOPE.Function", caller)
		}
	case "java":
		for _, m := range sfnStartExecutionNodeRe.FindAllStringSubmatchIndex(src, -1) {
			smRef := src[m[2]:m[3]]
			className := ""
			if cm := classNameRe.FindStringSubmatch(src); len(cm) >= 2 {
				className = cm[1]
			}
			emit(smRef, "SCOPE.Class", className)
		}
	}

	return entities, relationships
}
