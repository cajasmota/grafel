// external_service.go — supplemental pass that emits DEPENDS_ON_SERVICE edges
// from JS/TS functions / methods to a shared SCOPE.ExternalService node (epic
// #3628). It lets the graph answer "what third-party services does this
// codebase integrate with, and where?" — SDK-level, NAMED services (Stripe,
// Twilio, SendGrid, AWS S3/SES/SNS/SQS/DynamoDB, OpenAI, Slack, Sentry,
// Firebase, Algolia), distinct from raw HTTP-client CONSUMES_API.
//
// Detected shapes (import-gated — honest-partial, precision-first):
//
//	import Stripe from "stripe";
//	const stripe = new Stripe(key); stripe.charges.create(...)  → stripe
//	import sgMail from "@sendgrid/mail"; sgMail.send(...)        → sendgrid
//	import { S3Client } from "@aws-sdk/client-s3";
//	const c = new S3Client(...); c.send(cmd)                     → aws-s3
//	import { WebClient } from "@slack/web-api"; new WebClient()  → slack
//	import * as Sentry from "@sentry/node"; Sentry.init(...)     → sentry
//
// Recognition is rooted at the SDK IMPORT, two ways:
//
//  1. A `new <Ctor>(...)` where <Ctor> is (or was imported from) a recognised
//     SDK module. The variable it is assigned to is remembered so subsequent
//     method calls on that variable (`stripe.charges.create()`) attribute back
//     to the same service.
//  2. A call whose receiver-root identifier IS an imported SDK binding
//     (`sgMail.send()`, `Sentry.init()`).
//
// Intentionally DROPPED: a `.create()` / `.send()` on a non-SDK object, or a
// `new X()` whose class is not an SDK import.
//
// All node/edge construction lives in extractor.EmitServiceDependencyEdges; the
// SERVICE DICTIONARY lives in extractor.ServiceForImportSource /
// AWSServiceFromArg.

package javascript

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	extreg "github.com/cajasmota/grafel/internal/extractor"
)

// emitServiceDependencyEdges scans the AST for SDK construction / call shapes
// rooted at a recognised external-service import and appends external-service
// entities + DEPENDS_ON_SERVICE edges to x.entities. x.entities[0] MUST be the
// file entity. Safe with an empty tree or no imports.
func (x *extractor) emitServiceDependencyEdges(root ts.Node) {
	if root == nil || len(x.entities) == 0 || len(x.importByLocal) == 0 {
		return
	}

	// localVarService maps a local variable name to the service its value was
	// constructed from (`const stripe = new Stripe(...)` → "stripe"). Populated
	// as we walk so later `stripe.charges.create()` calls attribute correctly.
	localVarService := map[string]string{}

	var calls []extreg.ServiceCall

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "function_declaration", "generator_function_declaration":
			enclosing = x.nodeText(n.ChildByFieldName("name"))
		case "method_definition":
			enclosing = x.nodeText(n.ChildByFieldName("name"))
		case "variable_declarator":
			// const <name> = new <SDKCtor>(...) — remember the binding.
			nameNode := n.ChildByFieldName("name")
			valNode := n.ChildByFieldName("value")
			if nameNode != nil && valNode != nil {
				switch valNode.Type() {
				case "arrow_function", "function", "function_expression":
					enclosing = x.nodeText(nameNode)
				case "new_expression":
					if svc, op := x.jsNewExpressionService(valNode); svc != "" {
						localVarService[x.nodeText(nameNode)] = svc
						calls = append(calls, extreg.ServiceCall{
							Service: svc, FromName: enclosing, Operation: op,
						})
					}
				}
			}
		case "new_expression":
			// A `new <SDKCtor>(...)` not captured by a declarator (e.g. used
			// inline) still signals an integration.
			if n.Parent() == nil || n.Parent().Type() != "variable_declarator" {
				if svc, op := x.jsNewExpressionService(n); svc != "" {
					calls = append(calls, extreg.ServiceCall{
						Service: svc, FromName: enclosing, Operation: op,
					})
				}
			}
		case "call_expression":
			if svc, op := x.jsCallService(n, localVarService); svc != "" {
				calls = append(calls, extreg.ServiceCall{
					Service: svc, FromName: enclosing, Operation: op,
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(root, "")

	extreg.EmitServiceDependencyEdges(&x.entities, x.language, calls)
}

// jsNewExpressionService resolves `new <Ctor>(...)` to a service name when
// <Ctor> is (or was imported from) a recognised SDK module. Returns the service
// and an operation token ("new <Ctor>"), or "" when not an SDK constructor.
//
// For AWS client constructors (`new S3Client(...)`, `new SESClient(...)`) the
// service is derived from the constructor name suffix, since aws-sdk v3 names
// the service in the class.
func (x *extractor) jsNewExpressionService(newNode ts.Node) (string, string) {
	ctor := newNode.ChildByFieldName("constructor")
	if ctor == nil {
		return "", ""
	}
	rootName := ""
	switch ctor.Type() {
	case "identifier", "type_identifier":
		rootName = x.nodeText(ctor)
	case "member_expression":
		// new AWS.S3() / new mod.Stripe()
		if obj := ctor.ChildByFieldName("object"); obj != nil && obj.Type() == "identifier" {
			rootName = x.nodeText(obj)
		}
		if prop := ctor.ChildByFieldName("property"); prop != nil {
			if svc := awsServiceFromCtorName(x.nodeText(prop)); svc != "" {
				if x.importedServiceRoot(rootName) != "" {
					return svc, "new " + x.nodeText(prop)
				}
			}
		}
	default:
		return "", ""
	}
	if rootName == "" {
		return "", ""
	}
	// AWS v3 client classes (S3Client, SESClient, …) — service from class name.
	if svc := awsServiceFromCtorName(rootName); svc != "" {
		if b, ok := x.importByLocal[rootName]; ok && extreg.IsAWSImportSentinel(extreg.ServiceForImportSource(b.importPath)) {
			return svc, "new " + rootName
		}
	}
	// Non-AWS SDK default/named import (`new Stripe`, `new WebClient`, …).
	if svc := x.importedServiceRoot(rootName); svc != "" && !extreg.IsAWSImportSentinel(svc) {
		return svc, "new " + rootName
	}
	return "", ""
}

// jsCallService resolves a call_expression to a service when its receiver root
// is either a known SDK import binding (`sgMail.send`, `Sentry.init`) or a
// local variable previously constructed from an SDK (`stripe.charges.create`).
// Returns the service and the dotted operation tail, or "" when no SDK root.
func (x *extractor) jsCallService(call ts.Node, localVarService map[string]string) (string, string) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return "", ""
	}
	root, tail := x.jsMemberRootAndTail(fn)
	if root == "" {
		return "", ""
	}
	// Local variable constructed from an SDK?
	if svc, ok := localVarService[root]; ok {
		return svc, tail
	}
	// Direct SDK import binding used as the receiver root?
	if svc := x.importedServiceRoot(root); svc != "" && !extreg.IsAWSImportSentinel(svc) {
		return svc, tail
	}
	return "", ""
}

// importedServiceRoot returns the canonical service name for a local identifier
// that is bound to a recognised external-service import, or "" otherwise. AWS
// imports return the bare "aws-" sentinel (caller resolves the concrete
// service from the constructor / client name).
func (x *extractor) importedServiceRoot(local string) string {
	if local == "" {
		return ""
	}
	b, ok := x.importByLocal[local]
	if !ok {
		return ""
	}
	return extreg.ServiceForImportSource(b.importPath)
}

// jsMemberRootAndTail flattens a member_expression into its ROOT identifier and
// the dotted property tail. For `stripe.charges.create` it returns ("stripe",
// "charges.create"); for `sgMail.send` it returns ("sgMail", "send"). Returns
// ("", "") when the chain does not root at a plain identifier.
func (x *extractor) jsMemberRootAndTail(member ts.Node) (string, string) {
	var parts []string
	node := member
	for node != nil && node.Type() == "member_expression" {
		prop := node.ChildByFieldName("property")
		if prop == nil {
			return "", ""
		}
		parts = append([]string{x.nodeText(prop)}, parts...)
		node = node.ChildByFieldName("object")
	}
	if node == nil || node.Type() != "identifier" {
		return "", ""
	}
	return x.nodeText(node), strings.Join(parts, ".")
}

// awsServiceFromCtorName maps an aws-sdk v3 client class name to the canonical
// aws-<svc> service, or "" when the name is not a recognised AWS client class.
//
//	"S3Client"        -> "aws-s3"
//	"SESClient"       -> "aws-ses"
//	"SNSClient"       -> "aws-sns"
//	"SQSClient"       -> "aws-sqs"
//	"DynamoDBClient"  -> "aws-dynamodb"
//	"S3"              -> "aws-s3"     (aws-sdk v2 service class)
func awsServiceFromCtorName(name string) string {
	n := strings.TrimSpace(name)
	n = strings.TrimSuffix(n, "Client")
	return extreg.AWSServiceFromArg(n)
}
