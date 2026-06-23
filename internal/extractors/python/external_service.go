// external_service.go — supplemental pass that emits DEPENDS_ON_SERVICE edges
// from Python functions / methods to a shared SCOPE.ExternalService node (epic
// #3628). It lets the graph answer "what third-party services does this
// codebase integrate with, and where?" — SDK-level, NAMED services (Stripe,
// Twilio, SendGrid, AWS S3/SES/SNS/SQS/DynamoDB, OpenAI, Slack, Sentry,
// Firebase, Algolia), distinct from raw HTTP-client CONSUMES_API.
//
// Detected shapes (import-gated — honest-partial, precision-first):
//
//	import stripe; stripe.Charge.create(...)      → stripe   op "Charge.create"
//	import boto3;  boto3.client("s3").put_object  → aws-s3   op "put_object"
//	import boto3;  boto3.resource("dynamodb")     → aws-dynamodb
//	from twilio.rest import Client; Client(...)   → twilio
//	import sendgrid; sendgrid.SendGridAPIClient() → sendgrid
//	import openai; openai.ChatCompletion.create() → openai   op "ChatCompletion.create"
//	from slack_sdk import WebClient; WebClient()  → slack
//	import sentry_sdk; sentry_sdk.init(...)       → sentry
//
// Intentionally DROPPED (would mislead integration analysis):
//
//	boto3.client(service_var)                     dynamic service arg → aws-generic
//	a local `.create()` on a non-SDK object       (receiver not an SDK import)
//
// All node/edge construction (convergence on one node per service via a
// synthetic SourceFile) lives in extractor.EmitServiceDependencyEdges; the
// SERVICE DICTIONARY lives in extractor.ServiceForImportSource /
// AWSServiceFromArg.

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitServiceDependencyEdges scans every function / method body for SDK call
// shapes rooted at a recognised external-service import and appends
// external-service entities + DEPENDS_ON_SERVICE edges.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input. SDK calls at module scope attach to the file entity.
func emitServiceDependencyEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	imports := buildPythonImportMap(root, file)
	if len(imports) == 0 {
		return // no imports → nothing SDK-rooted can exist
	}
	src := file.Content

	var calls []extractor.ServiceCall

	var stack []string
	current := func() string {
		if len(stack) == 0 {
			return ""
		}
		return stack[len(stack)-1]
	}

	var walk func(n ts.Node, parentClass string)
	walk = func(n ts.Node, parentClass string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_definition":
			cls := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				cls = nodeText(nn, src)
			}
			childCls := cls
			if parentClass != "" && cls != "" {
				childCls = parentClass + "." + cls
			}
			stack = append(stack, childCls)
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), childCls)
				}
			}
			stack = stack[:len(stack)-1]
			return
		case "function_definition":
			leaf := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				leaf = nodeText(nn, src)
			}
			emitted := leaf
			if parentClass != "" && leaf != "" {
				emitted = parentClass + "." + leaf
			}
			stack = append(stack, emitted)
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), parentClass)
				}
			}
			stack = stack[:len(stack)-1]
			return
		case "decorated_definition":
			if inner := n.ChildByFieldName("definition"); inner != nil {
				walk(inner, parentClass)
			}
			return
		case "call":
			if sc, ok := pyServiceCall(n, src, imports, current()); ok {
				calls = append(calls, sc)
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), parentClass)
		}
	}
	walk(root, "")

	extractor.EmitServiceDependencyEdges(entities, "python", calls)
}

// pyServiceCall inspects a `call` node and, when its receiver chain roots at a
// recognised external-service SDK import, returns the resolved ServiceCall.
//
// Two recognition paths:
//
//  1. The call's function is an attribute chain (`stripe.Charge.create`,
//     `boto3.client`, `sentry_sdk.init`) whose ROOT identifier is bound to a
//     recognised SDK import. The dotted tail after the root becomes the
//     operation. For AWS the concrete service comes from the first literal
//     string argument of `client(...)` / `resource(...)`.
//
//  2. The call's function is a bare identifier (`Client(...)`,
//     `WebClient(...)`, `SendGridAPIClient(...)`) that was `from <sdk> import`
//     -ed from a recognised SDK source module — a constructor invocation.
//
// Returns ok=false when no recognised SDK root is found (so non-SDK
// `.create()` / `.send()` calls never fabricate an edge).
func pyServiceCall(call ts.Node, src []byte, imports pythonImportMap, from string) (extractor.ServiceCall, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return extractor.ServiceCall{}, false
	}

	switch fn.Type() {
	case "attribute":
		root, tail := pyAttributeRootAndTail(fn, src)
		if root == "" {
			return extractor.ServiceCall{}, false
		}
		binding, ok := imports[root]
		if !ok {
			return extractor.ServiceCall{}, false
		}
		svc := extractor.ServiceForImportSource(binding.sourceModule)
		if svc == "" {
			return extractor.ServiceCall{}, false
		}
		if extractor.IsAWSImportSentinel(svc) {
			// boto3.client("s3") / boto3.resource("dynamodb") — the concrete
			// AWS service is the first literal string arg. A dynamic arg maps
			// to aws-generic (honest-partial); an unrecognised literal drops.
			if tail == "client" || tail == "resource" {
				resolved := pyAWSServiceFromCall(call, src)
				return extractor.ServiceCall{Service: resolved, FromName: from, Operation: tail}, resolved != ""
			}
			// Other boto3.* calls without an explicit service — generic.
			return extractor.ServiceCall{Service: extractor.ServiceAWSGeneric, FromName: from, Operation: tail}, true
		}
		return extractor.ServiceCall{Service: svc, FromName: from, Operation: tail}, true

	case "identifier":
		// Constructor / function imported `from <sdk> import <Name>`.
		name := strings.TrimSpace(nodeText(fn, src))
		binding, ok := imports[name]
		if !ok || binding.plainModule {
			return extractor.ServiceCall{}, false
		}
		svc := extractor.ServiceForImportSource(binding.sourceModule)
		if svc == "" || extractor.IsAWSImportSentinel(svc) {
			// AWS via `from <x> import` is unusual; require the attribute path.
			return extractor.ServiceCall{}, false
		}
		return extractor.ServiceCall{Service: svc, FromName: from, Operation: name}, true
	}
	return extractor.ServiceCall{}, false
}

// pyAttributeRootAndTail flattens an attribute chain into its ROOT identifier
// and the dotted tail after it. For `stripe.Charge.create` it returns
// ("stripe", "Charge.create"); for `boto3.client` it returns ("boto3",
// "client"). Returns ("", "") when the chain does not root at a plain
// identifier (e.g. a subscript or call in the middle).
func pyAttributeRootAndTail(attr ts.Node, src []byte) (string, string) {
	var parts []string
	node := attr
	for node != nil && node.Type() == "attribute" {
		a := node.ChildByFieldName("attribute")
		if a == nil {
			return "", ""
		}
		parts = append([]string{strings.TrimSpace(nodeText(a, src))}, parts...)
		node = node.ChildByFieldName("object")
	}
	if node == nil || node.Type() != "identifier" {
		return "", "" // root is not a plain identifier → not an SDK module ref
	}
	root := strings.TrimSpace(nodeText(node, src))
	return root, strings.Join(parts, ".")
}

// pyAWSServiceFromCall resolves the AWS service from a boto3.client(...) /
// boto3.resource(...) call: the first positional argument, when a string
// literal, is mapped via AWSServiceFromArg. A non-literal (dynamic variable)
// argument yields aws-generic; an unrecognised literal yields "" (drop).
func pyAWSServiceFromCall(call ts.Node, src []byte) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return extractor.ServiceAWSGeneric
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		a := args.NamedChild(i)
		if a == nil {
			continue
		}
		switch a.Type() {
		case "string":
			lit := pyStringLiteralValue(a, src)
			if svc := extractor.AWSServiceFromArg(lit); svc != "" {
				return svc
			}
			return "" // recognised AWS import but unknown service literal → drop
		case "keyword_argument":
			// service_name="s3"
			if v := a.ChildByFieldName("value"); v != nil && v.Type() == "string" {
				if svc := extractor.AWSServiceFromArg(pyStringLiteralValue(v, src)); svc != "" {
					return svc
				}
			}
			continue
		default:
			// First positional is a dynamic variable → honest-partial generic.
			return extractor.ServiceAWSGeneric
		}
	}
	return extractor.ServiceAWSGeneric
}
