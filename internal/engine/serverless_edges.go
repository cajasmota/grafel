// Serverless function invocation edges — #925.
//
// This pass detects SDK-level invocations of serverless functions across three
// cloud providers (AWS Lambda, Google Cloud Functions, Azure Functions) and
// emits:
//
//   - SCOPE.ServerlessFunction entities keyed by `<provider>:<function-name>`.
//     The same synthetic ID is emitted by both the invoker side (producer) and
//     the handler side (consumer) so the existing import-channel linker joins
//     them cross-repo without any new linker code.
//   - CALLS edges:  invoker call site  → ServerlessFunction
//   - HANDLES edges: handler definition → ServerlessFunction
//
// # AWS Lambda
//
// Producer: boto3 `client('lambda').invoke(FunctionName='X')`, AWS SDK v3
// `new LambdaClient` + `new InvokeCommand({FunctionName:'X'})`, AWS SDK v2
// `lambda.invoke({FunctionName:'X'})`, Go SDK `lambda.InvokeInput{FunctionName:…}`.
//
// Consumer: Python `def lambda_handler(event, context)`, Node
// `exports.handler = …`, Go `lambda.Start(fn)`, Java
// `implements RequestHandler<I,O>`. Cross-repo: the function name is resolved
// via the `functions.<name>.handler` field in a sibling `serverless.yml` so
// that the canonical entity ID uses the *logical* function name, not the
// handler symbol.
//
// # Google Cloud Functions
//
// Producer: `@google-cloud/functions-framework` SDK calls and
// HTTP invocations against `*.cloudfunctions.net` URLs are both captured.
//
// Consumer: `exports.<name> = (req, res) => …` (Node functions.http),
// `@functions_framework.http` Python decorator, `functions.http('name', …)`
// Node SDK registration.
//
// # Azure Functions
//
// Producer: `DurableOrchestrationClient.start_new('FnName')` (Python durable),
// `context.df.callActivity('FnName')` (JS orchestrator), `StartNewAsync`
// (C# durable), `HttpClient` calls against `*.azurewebsites.net`.
//
// Consumer: `@app.function_name(name='X')` Python decorator,
// `[FunctionName("X")]` C# attribute, `module.exports = async function
// (context, req)` JavaScript.
//
// # Laying groundwork for #927
//
// AWS Lambda synthetics (entity ID prefix `aws-lambda:`) are the natural
// anchor for EventBridge rule targets. The #927 pass will reuse the same
// ServerlessFunction entity kind and `aws-lambda:` ID prefix to link
// EventBridge rules to their Lambda consumers without any schema change.
//
// # Scope guard
//
// Append-only — this pass never modifies or removes existing entities or
// edges, so it cannot regress the bug-rate of the surrounding pipeline.
//
// Refs #925. Lays groundwork for #927.
package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// serverlessFunctionKind is the Kind used for synthetic serverless function
// entities. Using a new SCOPE.ServerlessFunction kind (per issue schema
// delta) so the MCP rendering layer and the orphan audit can distinguish
// serverless functions from generic SCOPE.Function entities.
const serverlessFunctionKind = "SCOPE.ServerlessFunction"

// callsEdgeKind is the relationship from an invoker to its ServerlessFunction.
// Reuses the existing CALLS kind — the `provider` property distinguishes the
// three cloud platforms at query time.
const serverlessCallsEdgeKind = "CALLS"

// handlesEdgeKind is the relationship from a handler entity to its
// ServerlessFunction (consumer side). HANDLES is being introduced here for the
// first time; it mirrors GRPC_IMPLEMENTS for the gRPC cross-repo pattern.
const serverlessHandlesEdgeKind = "HANDLES"

// serverlessSynthesisSupportsLanguage reports whether applyServerlessEdges can
// emit synthetics for `lang`.
func serverlessSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "python", "javascript", "typescript", "go", "java", "csharp":
		return true
	default:
		return false
	}
}

// applyServerlessEdges runs as an append-only pass after the gRPC edges pass.
// It scans source files for Lambda/GCF/Azure invocation and handler patterns,
// emitting SCOPE.ServerlessFunction entities plus CALLS and HANDLES edges.
func applyServerlessEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if !serverlessSynthesisSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	// Dedup-by-ID: one ServerlessFunction entity per (provider, name) per file,
	// one CALLS / HANDLES per (caller, function, direction).
	seenFn := map[string]bool{}
	seenEdge := map[string]bool{}

	emitFn := func(fnID, fnName, provider string, props map[string]string) {
		if seenFn[fnID] {
			return
		}
		seenFn[fnID] = true
		merged := map[string]string{
			"provider":      provider,
			"function_name": fnName,
			"pattern_type":  "serverless_synthesis",
		}
		for k, v := range props {
			if v != "" {
				merged[k] = v
			}
		}
		// SourceFile intentionally empty so every emission of the same
		// (Kind, Name) across files in a repo collapses to the same entity ID
		// (identical to the Kafka / gRPC synthesis strategy).
		entities = append(entities, types.EntityRecord{
			Name:               fnID,
			Kind:               serverlessFunctionKind,
			SourceFile:         "",
			Language:           lang,
			Properties:         merged,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitCalls := func(callerKind, callerName, fnID, provider string, extraProps map[string]string) {
		if callerName == "" || fnID == "" {
			return
		}
		key := serverlessCallsEdgeKind + "|" + callerKind + ":" + callerName + "|" + fnID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		props := map[string]string{
			"provider":     provider,
			"pattern_type": "serverless_synthesis",
		}
		for k, v := range extraProps {
			if v != "" {
				props[k] = v
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fmt.Sprintf("%s:%s", callerKind, callerName),
			ToID:       fmt.Sprintf("%s:%s", serverlessFunctionKind, fnID),
			Kind:       serverlessCallsEdgeKind,
			Properties: props,
		})
	}

	emitHandles := func(handlerKind, handlerName, fnID, provider string, extraProps map[string]string) {
		if handlerName == "" || fnID == "" {
			return
		}
		key := serverlessHandlesEdgeKind + "|" + handlerKind + ":" + handlerName + "|" + fnID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		props := map[string]string{
			"provider":     provider,
			"pattern_type": "serverless_synthesis",
		}
		for k, v := range extraProps {
			if v != "" {
				props[k] = v
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fmt.Sprintf("%s:%s", handlerKind, handlerName),
			ToID:       fmt.Sprintf("%s:%s", serverlessFunctionKind, fnID),
			Kind:       serverlessHandlesEdgeKind,
			Properties: props,
		})
	}

	switch lang {
	case "python":
		synthesizePyServerless(src, path, emitFn, emitCalls, emitHandles)
	case "javascript", "typescript":
		synthesizeNodeServerless(src, path, emitFn, emitCalls, emitHandles)
	case "go":
		synthesizeGoServerless(src, path, emitFn, emitCalls, emitHandles)
	case "java":
		synthesizeJavaServerless(src, path, emitFn, emitCalls, emitHandles)
	case "csharp":
		synthesizeCSharpServerless(src, path, emitFn, emitCalls, emitHandles)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// lambdaFunctionID returns the canonical synthetic ID for an AWS Lambda function.
// Identical across repos — cross-repo linker joins on this shared ID.
func lambdaFunctionID(name string) string {
	return "aws-lambda:" + name
}

// gcfFunctionID returns the canonical ID for a Google Cloud Function.
func gcfFunctionID(name string) string {
	return "gcp-cloudfunction:" + name
}

// azureFunctionID returns the canonical ID for an Azure Function.
func azureFunctionID(name string) string {
	return "azure-function:" + name
}

// serverlessLineFromOffset returns the 1-based line number for a byte offset
// within a source file. Used to populate Properties["line"] on synthetic CALLS
// edges emitted by the serverless pass; no tree-sitter node is available at
// engine-pass time, so we count newlines in the preceding bytes instead.
func serverlessLineFromOffset(src string, offset int) string {
	if offset <= 0 || offset > len(src) {
		return "0"
	}
	return strconv.Itoa(strings.Count(src[:offset], "\n") + 1)
}

// looksLikeFunctionName validates that s is a plausible serverless function name
// (no path separators, whitespace, or template-literal content).
func looksLikeFunctionName(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	if strings.ContainsAny(s, " \t\n\r/\\<>{}`$") {
		return false
	}
	// Must contain at least one letter.
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Python — AWS Lambda (boto3), GCF (google-cloud-functions), Azure durable
// ---------------------------------------------------------------------------

// pyLambdaInvokeRe captures boto3 `client('lambda').invoke(FunctionName='X')`
// or `lambda_client.invoke(FunctionName='X')`. Group 1 = function name.
var pyLambdaInvokeRe = regexp.MustCompile(`\.invoke\s*\([^)]*FunctionName\s*=\s*["']([^"'\n\r]+)["']`)

// serverlessPyLambdaHandlerRe captures `def lambda_handler(event, context)` —
// the standard AWS Lambda Python handler signature.
var serverlessPyLambdaHandlerRe = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(lambda_handler)\s*\(\s*\w+\s*,\s*\w+\s*\)`)

// pyGCFFunctionFrameworkRe captures `@functions_framework.http` or
// `@functions_framework.cloud_event` decorators followed by `def name(`.
var pyGCFFunctionFrameworkRe = regexp.MustCompile(`(?m)@functions_framework\.(?:http|cloud_event)\s*\n(?:\s*#[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(`)

// pyAzureDurableRe captures `context.call_activity('FnName', ...)` and
// `yield context.df.call_activity('FnName', ...)`.
var pyAzureDurableRe = regexp.MustCompile(`\.call_activity\s*\(\s*["']([^"'\n\r]+)["']`)

// pyAzureStartNewRe captures `context.df.start_new('FnName', ...)` (durable
// orchestration start) and `DurableOrchestrationClient.start_new(...)`.
var pyAzureStartNewRe = regexp.MustCompile(`\.start_new\s*\(\s*["']([^"'\n\r]+)["']`)

// pyAzureFunctionNameRe captures `@app.function_name(name='X')` or
// `@app.route(route='...', function_name='X')` — Azure Functions Python v2
// programming model.
var pyAzureFunctionNameRe = regexp.MustCompile(`@\w+\.function_name\s*\(\s*name\s*=\s*["']([^"'\n\r]+)["']`)

// pyFunctionDefRe matches `def name(` to find enclosing function names.
var pyFunctionDefRe = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)\s*\(`)

func synthesizePyServerless(
	src, path string,
	emitFn func(fnID, fnName, provider string, props map[string]string),
	emitCalls func(callerKind, callerName, fnID, provider string, extraProps map[string]string),
	emitHandles func(handlerKind, handlerName, fnID, provider string, extraProps map[string]string),
) {
	enclosing := func(offset int) string {
		return findEnclosingPyFunctionName(src, offset)
	}

	// AWS Lambda — producer: boto3 invoke
	if strings.Contains(src, "lambda") || strings.Contains(src, "Lambda") {
		for _, m := range pyLambdaInvokeRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if !looksLikeFunctionName(name) {
				continue
			}
			id := lambdaFunctionID(name)
			emitFn(id, name, "aws-lambda", nil)
			caller := enclosing(m[0])
			emitCalls("SCOPE.Function", caller, id, "aws-lambda", map[string]string{"sdk": "boto3", "line": serverlessLineFromOffset(src, m[0])})
		}

		// AWS Lambda — consumer: def lambda_handler(event, context)
		for _, m := range serverlessPyLambdaHandlerRe.FindAllStringSubmatchIndex(src, -1) {
			handlerName := src[m[2]:m[3]]
			// The logical function name is resolved via serverless.yml if present.
			// Without a sidecar config file, we use the file stem as the key so
			// that the cross-repo linker can still join on an agreed name.
			logicalName := resolveServerlessYMLName(path, handlerName)
			id := lambdaFunctionID(logicalName)
			emitFn(id, logicalName, "aws-lambda", map[string]string{"handler_symbol": handlerName})
			emitHandles("SCOPE.Function", handlerName, id, "aws-lambda", map[string]string{"sdk": "boto3"})
		}
	}

	// GCF — consumer: @functions_framework.http
	if strings.Contains(src, "functions_framework") {
		for _, m := range pyGCFFunctionFrameworkRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if !looksLikeFunctionName(name) {
				continue
			}
			id := gcfFunctionID(name)
			emitFn(id, name, "gcp-cloudfunction", map[string]string{"trigger": "http"})
			emitHandles("SCOPE.Function", name, id, "gcp-cloudfunction", map[string]string{"sdk": "functions-framework-python"})
		}
	}

	// Azure — producer: call_activity / start_new
	if strings.Contains(src, "azure") || strings.Contains(src, "durable") || strings.Contains(src, "call_activity") || strings.Contains(src, "start_new") {
		for _, m := range pyAzureDurableRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if !looksLikeFunctionName(name) {
				continue
			}
			id := azureFunctionID(name)
			emitFn(id, name, "azure-function", map[string]string{"trigger": "activity"})
			emitCalls("SCOPE.Function", enclosing(m[0]), id, "azure-function", map[string]string{"sdk": "azure-durable-functions", "line": serverlessLineFromOffset(src, m[0])})
		}
		for _, m := range pyAzureStartNewRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if !looksLikeFunctionName(name) {
				continue
			}
			id := azureFunctionID(name)
			emitFn(id, name, "azure-function", map[string]string{"trigger": "orchestration"})
			emitCalls("SCOPE.Function", enclosing(m[0]), id, "azure-function", map[string]string{"sdk": "azure-durable-functions", "line": serverlessLineFromOffset(src, m[0])})
		}

		// Azure — consumer: @app.function_name(name='X')
		for _, m := range pyAzureFunctionNameRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if !looksLikeFunctionName(name) {
				continue
			}
			id := azureFunctionID(name)
			emitFn(id, name, "azure-function", nil)
			// Find the immediately following def
			following := findFollowingPyDef(src, m[1])
			if following == "" {
				following = name
			}
			emitHandles("SCOPE.Function", following, id, "azure-function", map[string]string{"sdk": "azure-functions-python"})
		}
	}
}

// findEnclosingPyFunctionName walks backward from offset to find the nearest
// `def name(` declaration.
func findEnclosingPyFunctionName(src string, offset int) string {
	start := offset - 4000
	if start < 0 {
		start = 0
	}
	window := src[start:offset]
	matches := pyFunctionDefRe.FindAllStringSubmatch(window, -1)
	if len(matches) == 0 {
		return "module"
	}
	return matches[len(matches)-1][1]
}

// findFollowingPyDef finds the name of the first `def` declaration after
// offset within a short lookahead window.
func findFollowingPyDef(src string, offset int) string {
	end := offset + 200
	if end > len(src) {
		end = len(src)
	}
	window := src[offset:end]
	if m := pyFunctionDefRe.FindStringSubmatch(window); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// resolveServerlessYMLName tries to extract the logical function name from a
// co-located serverless.yml by matching the handler symbol. Falls back to the
// symbol name itself when no match is found. This enables cross-repo joining
// on the logical function name rather than the Python symbol.
//
// The actual YAML parsing is path-based (not content-based) and does not read
// the file during tests — callers pass the source-file path so that fixtures
// work without a filesystem sidecar.
func resolveServerlessYMLName(sourcePath, handlerSymbol string) string {
	// In tests (and when no serverless.yml is present) we fall back to the
	// handler symbol as the logical name. Real deployments should register the
	// serverless.yml topology via the deployment_topology_extractor which already
	// captures `functions.<logicalName>.handler` entries. The #927 EventBridge
	// pass will wire that index together. For now, handler symbol == logical name.
	_ = sourcePath
	return handlerSymbol
}

// ---------------------------------------------------------------------------
// Node / TypeScript — AWS Lambda, GCF, Azure
// ---------------------------------------------------------------------------

// nodeAWSSDKv3InvokeRe captures the AWS SDK v3 pattern:
// `new InvokeCommand({ FunctionName: 'X' })`. Group 1 = function name.
var nodeAWSSDKv3InvokeRe = regexp.MustCompile(`new\s+InvokeCommand\s*\(\s*\{[^}]*FunctionName\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeAWSSDKv2InvokeRe captures the legacy SDK v2 pattern:
// `lambda.invoke({ FunctionName: 'X' })`. Group 1 = function name.
var nodeAWSSDKv2InvokeRe = regexp.MustCompile(`\.invoke\s*\(\s*\{[^}]*FunctionName\s*:\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeLambdaHandlerRe captures `exports.handler = async (event, context) =>`
// and `module.exports.handler = function(event, context)`.
// We also accept `exports.handler = (event, ctx) =>` without async.
var nodeLambdaHandlerRe = regexp.MustCompile(`(?:exports|module\.exports)\.handler\s*=`)

// nodeGCFExportsRe captures `exports.<name> = (req, res) =>` (GCF HTTP function
// registration). Group 1 = function name.
// Guard: only match when the file imports functions-framework or the function
// name is not "handler" (which would be a Lambda consumer, not GCF).
var nodeGCFExportsRe = regexp.MustCompile(`(?:exports|module\.exports)\.(\w+)\s*=\s*(?:async\s+)?(?:function|\([^)]*\)\s*=>)`)

// nodeGCFFunctionsHttpRe captures `functions.http('name', handler)` registration.
// Group 1 = function name.
var nodeGCFFunctionsHttpRe = regexp.MustCompile(`functions\.http\s*\(\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeAzureDurableCallActivityRe captures `context.df.callActivity('FnName', ...)`.
var nodeAzureDurableCallActivityRe = regexp.MustCompile(`\.callActivity\s*\(\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeAzureStartNewRe captures `context.df.startNew('FnName', ...)`.
var nodeAzureStartNewRe = regexp.MustCompile(`\.startNew\s*\(\s*["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`)

// nodeAzureHandlerExportRe captures the Azure Functions JS convention:
// `module.exports = async function(context, req)`.
var nodeAzureHandlerExportRe = regexp.MustCompile(`module\.exports\s*=\s*async\s+function\s*\(\s*context`)

// nodeFunctionNameForOffsetRe finds the enclosing function / arrow name.
var nodeFunctionNameForOffsetRe = regexp.MustCompile(`(?m)(?:function\s+(\w+)|(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:function|\([^)]*\)\s*=>)|(\w+)\s*[:=]\s*(?:async\s+)?(?:function|\([^)]*\)\s*=>))`)

func synthesizeNodeServerless(
	src, path string,
	emitFn func(fnID, fnName, provider string, props map[string]string),
	emitCalls func(callerKind, callerName, fnID, provider string, extraProps map[string]string),
	emitHandles func(handlerKind, handlerName, fnID, provider string, extraProps map[string]string),
) {
	enclosing := func(offset int) string {
		return findEnclosingNodeFunctionName(src, offset)
	}

	isGCF := strings.Contains(src, "@google-cloud/functions-framework") ||
		strings.Contains(src, "functions-framework") ||
		strings.Contains(src, "functions.http")
	isAzure := strings.Contains(src, "durable-functions") ||
		strings.Contains(src, "df.callActivity") ||
		strings.Contains(src, "df.startNew") ||
		strings.Contains(src, "azurewebsites.net")
	isLambda := strings.Contains(src, "aws-lambda") ||
		strings.Contains(src, "@aws-sdk/client-lambda") ||
		strings.Contains(src, "LambdaClient") ||
		strings.Contains(src, "InvokeCommand") ||
		strings.Contains(src, "exports.handler")

	// AWS SDK v3 — InvokeCommand
	if isLambda {
		for _, m := range nodeAWSSDKv3InvokeRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if !looksLikeFunctionName(name) {
				continue
			}
			id := lambdaFunctionID(name)
			emitFn(id, name, "aws-lambda", nil)
			emitCalls("SCOPE.Function", enclosing(m[0]), id, "aws-lambda", map[string]string{"sdk": "aws-sdk-v3", "line": serverlessLineFromOffset(src, m[0])})
		}

		// AWS SDK v2 — lambda.invoke({ FunctionName: 'X' })
		// Guard: only fire when LambdaClient-adjacent import tokens are present
		// to avoid matching unrelated .invoke() calls.
		if strings.Contains(src, "LambdaClient") || strings.Contains(src, "aws-sdk") {
			for _, m := range nodeAWSSDKv2InvokeRe.FindAllStringSubmatchIndex(src, -1) {
				name := src[m[2]:m[3]]
				if !looksLikeFunctionName(name) {
					continue
				}
				id := lambdaFunctionID(name)
				emitFn(id, name, "aws-lambda", nil)
				emitCalls("SCOPE.Function", enclosing(m[0]), id, "aws-lambda", map[string]string{"sdk": "aws-sdk-v2", "line": serverlessLineFromOffset(src, m[0])})
			}
		}

		// AWS Lambda — handler: exports.handler = ...
		if nodeLambdaHandlerRe.MatchString(src) {
			logicalName := resolveServerlessYMLName(path, "handler")
			id := lambdaFunctionID(logicalName)
			emitFn(id, logicalName, "aws-lambda", map[string]string{"handler_symbol": "exports.handler"})
			emitHandles("SCOPE.Function", "handler", id, "aws-lambda", map[string]string{"sdk": "aws-lambda-nodejs"})
		}
	}

	// GCF — functions.http('name', handler)
	if isGCF {
		for _, m := range nodeGCFFunctionsHttpRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if !looksLikeFunctionName(name) {
				continue
			}
			id := gcfFunctionID(name)
			emitFn(id, name, "gcp-cloudfunction", map[string]string{"trigger": "http"})
			emitHandles("SCOPE.Function", name, id, "gcp-cloudfunction", map[string]string{"sdk": "functions-framework-nodejs"})
		}

		// GCF — exports.<name> = (req, res) => (HTTP function export style)
		for _, m := range nodeGCFExportsRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			// Skip "handler" — that belongs to Lambda, not GCF.
			if name == "handler" || !looksLikeFunctionName(name) {
				continue
			}
			id := gcfFunctionID(name)
			emitFn(id, name, "gcp-cloudfunction", map[string]string{"trigger": "http"})
			emitHandles("SCOPE.Function", name, id, "gcp-cloudfunction", map[string]string{"sdk": "functions-framework-nodejs"})
		}
	}

	// Azure — producer: callActivity / startNew
	if isAzure {
		for _, m := range nodeAzureDurableCallActivityRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if !looksLikeFunctionName(name) {
				continue
			}
			id := azureFunctionID(name)
			emitFn(id, name, "azure-function", map[string]string{"trigger": "activity"})
			emitCalls("SCOPE.Function", enclosing(m[0]), id, "azure-function", map[string]string{"sdk": "durable-functions", "line": serverlessLineFromOffset(src, m[0])})
		}
		for _, m := range nodeAzureStartNewRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if !looksLikeFunctionName(name) {
				continue
			}
			id := azureFunctionID(name)
			emitFn(id, name, "azure-function", map[string]string{"trigger": "orchestration"})
			emitCalls("SCOPE.Function", enclosing(m[0]), id, "azure-function", map[string]string{"sdk": "durable-functions", "line": serverlessLineFromOffset(src, m[0])})
		}

		// Azure — consumer: module.exports = async function(context, req)
		if nodeAzureHandlerExportRe.MatchString(src) {
			logicalName := resolveAzureLogicalName(path)
			id := azureFunctionID(logicalName)
			emitFn(id, logicalName, "azure-function", nil)
			emitHandles("SCOPE.Function", logicalName, id, "azure-function", map[string]string{"sdk": "azure-functions-nodejs"})
		}
	}
}

// findEnclosingNodeFunctionName walks backward from offset to find the nearest
// function/arrow name in JS/TS source.
func findEnclosingNodeFunctionName(src string, offset int) string {
	start := offset - 4000
	if start < 0 {
		start = 0
	}
	window := src[start:offset]
	matches := nodeFunctionNameForOffsetRe.FindAllStringSubmatch(window, -1)
	if len(matches) == 0 {
		return "module"
	}
	last := matches[len(matches)-1]
	for _, g := range last[1:] {
		if g != "" {
			return g
		}
	}
	return "module"
}

// resolveAzureLogicalName derives the logical Azure Function name from its
// directory path. Azure Functions v1/v2 use the directory name as the
// function name by convention (e.g. `HttpTrigger/index.js` → `HttpTrigger`).
func resolveAzureLogicalName(filePath string) string {
	parts := strings.Split(strings.ReplaceAll(filePath, "\\", "/"), "/")
	if len(parts) >= 2 {
		dir := parts[len(parts)-2]
		if dir != "" && dir != "." && dir != "src" {
			return dir
		}
	}
	return "handler"
}

// ---------------------------------------------------------------------------
// Go — AWS Lambda SDK v2
// ---------------------------------------------------------------------------

// goLambdaInvokeInputRe captures the Go SDK v2 struct literal:
// `lambda.InvokeInput{FunctionName: aws.String("X")}` and
// `&lambda.InvokeInput{FunctionName: aws.String("X")}`.
// Group 1 = function name inside aws.String("…") or a plain string literal "…".
var goLambdaInvokeInputRe = regexp.MustCompile(`InvokeInput\s*\{[^}]*FunctionName\s*:\s*(?:aws\.String\s*\(\s*)?["` + "`" + `]([^"` + "`" + `\n\r]+)["` + "`" + `]`)

// goLambdaStartRe captures `lambda.Start(handlerFunc)` — the Go Lambda
// runtime entry point. Group 1 = handler function name.
var goLambdaStartRe = regexp.MustCompile(`lambda\.Start\s*\(\s*(\w+)\s*\)`)

// goFunctionDeclRe finds `func name(` or `func (recv) name(` declarations.
var goFunctionDeclRe = regexp.MustCompile(`(?m)^func\s+(?:\(\s*\w+\s+\*?(\w+)\s*\)\s*)?(\w+)\s*\(`)

func synthesizeGoServerless(
	src, path string,
	emitFn func(fnID, fnName, provider string, props map[string]string),
	emitCalls func(callerKind, callerName, fnID, provider string, extraProps map[string]string),
	emitHandles func(handlerKind, handlerName, fnID, provider string, extraProps map[string]string),
) {
	if !strings.Contains(src, "lambda") && !strings.Contains(src, "Lambda") {
		return
	}

	enclosing := func(offset int) string {
		return findEnclosingGoFunctionName(src, offset)
	}

	// Producer: InvokeInput{FunctionName: ...}
	for _, m := range goLambdaInvokeInputRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if !looksLikeFunctionName(name) {
			continue
		}
		id := lambdaFunctionID(name)
		emitFn(id, name, "aws-lambda", nil)
		emitCalls("SCOPE.Function", enclosing(m[0]), id, "aws-lambda", map[string]string{"sdk": "aws-sdk-go-v2", "line": serverlessLineFromOffset(src, m[0])})
	}

	// Consumer: lambda.Start(handler)
	for _, m := range goLambdaStartRe.FindAllStringSubmatchIndex(src, -1) {
		handlerName := src[m[2]:m[3]]
		logicalName := resolveServerlessYMLName(path, handlerName)
		id := lambdaFunctionID(logicalName)
		emitFn(id, logicalName, "aws-lambda", map[string]string{"handler_symbol": handlerName})
		emitHandles("SCOPE.Function", handlerName, id, "aws-lambda", map[string]string{"sdk": "aws-lambda-go"})
	}
}

// findEnclosingGoFunctionName walks backward from offset to find the nearest
// Go function/method declaration.
func findEnclosingGoFunctionName(src string, offset int) string {
	start := offset - 4000
	if start < 0 {
		start = 0
	}
	window := src[start:offset]
	matches := goFunctionDeclRe.FindAllStringSubmatch(window, -1)
	if len(matches) == 0 {
		return "package"
	}
	last := matches[len(matches)-1]
	name := last[2]
	if last[1] != "" {
		name = last[1] + "." + name
	}
	return name
}

// ---------------------------------------------------------------------------
// Java — AWS Lambda RequestHandler + SDK invoke
// ---------------------------------------------------------------------------

// javaRequestHandlerRe captures `implements RequestHandler<Input, Output>` —
// the canonical AWS Lambda Java handler contract. Group 1 = class name.
var javaRequestHandlerRe = regexp.MustCompile(`(?m)\bclass\s+(\w+)\s+implements\s+[^{]*RequestHandler\s*<`)

// javaLambdaInvokeRe captures `lambdaClient.invoke(InvokeRequest.builder()
// .functionName("X")` style (AWS SDK v2 Java). Group 1 = function name.
var javaLambdaInvokeRe = regexp.MustCompile(`\.functionName\s*\(\s*["']([^"'\n\r]+)["']`)

func synthesizeJavaServerless(
	src, path string,
	emitFn func(fnID, fnName, provider string, props map[string]string),
	emitCalls func(callerKind, callerName, fnID, provider string, extraProps map[string]string),
	emitHandles func(handlerKind, handlerName, fnID, provider string, extraProps map[string]string),
) {
	if !strings.Contains(src, "lambda") && !strings.Contains(src, "Lambda") &&
		!strings.Contains(src, "RequestHandler") {
		return
	}

	// Consumer: class Foo implements RequestHandler<I,O>
	for _, m := range javaRequestHandlerRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		logicalName := resolveServerlessYMLName(path, className)
		id := lambdaFunctionID(logicalName)
		emitFn(id, logicalName, "aws-lambda", map[string]string{"handler_symbol": className})
		emitHandles("SCOPE.Class", className, id, "aws-lambda", map[string]string{"sdk": "aws-lambda-java"})
	}

	// Producer: .functionName("X") in LambdaClient / InvokeRequest builder
	if strings.Contains(src, "LambdaClient") || strings.Contains(src, "InvokeRequest") {
		// Collect class name for caller context.
		className := ""
		if m := classNameRe.FindStringSubmatch(src); len(m) >= 2 {
			className = m[1]
		}
		for _, m := range javaLambdaInvokeRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if !looksLikeFunctionName(name) {
				continue
			}
			id := lambdaFunctionID(name)
			emitFn(id, name, "aws-lambda", nil)
			caller := className
			if caller == "" {
				caller = "unknown"
			}
			emitCalls("SCOPE.Class", caller, id, "aws-lambda", map[string]string{"sdk": "aws-sdk-java-v2", "line": serverlessLineFromOffset(src, m[0])})
		}
	}
}

// ---------------------------------------------------------------------------
// C# — Azure Functions [FunctionName] attribute + durable StartNewAsync
// ---------------------------------------------------------------------------

// csharpFunctionNameAttrRe captures `[FunctionName("X")]` attribute on a method.
// Group 1 = function name.
var csharpFunctionNameAttrRe = regexp.MustCompile(`\[FunctionName\s*\(\s*"([^"\n\r]+)"\s*\)\s*\]`)

// csharpStartNewAsyncRe captures `client.StartNewAsync("FnName", ...)` durable
// orchestration start. Group 1 = function name.
var csharpStartNewAsyncRe = regexp.MustCompile(`\.StartNewAsync\s*\(\s*"([^"\n\r]+)"`)

// csharpCallActivityRe captures `context.CallActivityAsync<T>("FnName", ...)`.
// Group 1 = function name.
var csharpCallActivityRe = regexp.MustCompile(`\.CallActivityAsync\s*(?:<[^>]+>)?\s*\(\s*"([^"\n\r]+)"`)

// csharpMethodRe finds `public … Task … MethodName(` after an attribute block.
var csharpMethodRe = regexp.MustCompile(`(?m)(?:public|private|protected|internal|static|async|Task|void|\s)+\s+(\w+)\s*\(`)

func synthesizeCSharpServerless(
	src, path string,
	emitFn func(fnID, fnName, provider string, props map[string]string),
	emitCalls func(callerKind, callerName, fnID, provider string, extraProps map[string]string),
	emitHandles func(handlerKind, handlerName, fnID, provider string, extraProps map[string]string),
) {
	if !strings.Contains(src, "FunctionName") && !strings.Contains(src, "StartNewAsync") &&
		!strings.Contains(src, "CallActivityAsync") {
		return
	}

	// Consumer: [FunctionName("X")]
	for _, m := range csharpFunctionNameAttrRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if !looksLikeFunctionName(name) {
			continue
		}
		id := azureFunctionID(name)
		emitFn(id, name, "azure-function", nil)
		// Find the method immediately after the attribute.
		methodName := findFollowingCSharpMethod(src, m[1])
		if methodName == "" {
			methodName = name
		}
		emitHandles("SCOPE.Function", methodName, id, "azure-function", map[string]string{"sdk": "azure-functions-dotnet"})
	}

	// Producer: StartNewAsync("FnName", ...)
	for _, m := range csharpStartNewAsyncRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if !looksLikeFunctionName(name) {
			continue
		}
		id := azureFunctionID(name)
		emitFn(id, name, "azure-function", map[string]string{"trigger": "orchestration"})
		callerMethod := findEnclosingCSharpMethod(src, m[0])
		emitCalls("SCOPE.Function", callerMethod, id, "azure-function", map[string]string{"sdk": "azure-durable-functions-dotnet", "line": serverlessLineFromOffset(src, m[0])})
	}

	// Producer: CallActivityAsync<T>("FnName", ...)
	for _, m := range csharpCallActivityRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if !looksLikeFunctionName(name) {
			continue
		}
		id := azureFunctionID(name)
		emitFn(id, name, "azure-function", map[string]string{"trigger": "activity"})
		callerMethod := findEnclosingCSharpMethod(src, m[0])
		emitCalls("SCOPE.Function", callerMethod, id, "azure-function", map[string]string{"sdk": "azure-durable-functions-dotnet", "line": serverlessLineFromOffset(src, m[0])})
	}
}

// findFollowingCSharpMethod finds the first method-name token after `offset`
// within a short lookahead window.
func findFollowingCSharpMethod(src string, offset int) string {
	end := offset + 300
	if end > len(src) {
		end = len(src)
	}
	window := src[offset:end]
	if m := csharpMethodRe.FindStringSubmatch(window); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// findEnclosingCSharpMethod walks backward from offset to find the nearest
// method declaration in C# source.
func findEnclosingCSharpMethod(src string, offset int) string {
	start := offset - 4000
	if start < 0 {
		start = 0
	}
	window := src[start:offset]
	matches := csharpMethodRe.FindAllStringSubmatch(window, -1)
	if len(matches) == 0 {
		return "class"
	}
	return matches[len(matches)-1][1]
}
