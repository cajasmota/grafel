// pass_signature_guard_test.go — compile-time enforcement that every
// top-level engine detector pass uses the canonical DetectorPassArgs →
// DetectorPassResult signature introduced by PR #2497.
//
// Background: PR #2497 standardised all engine passes to accept a single
// DetectorPassArgs struct and return DetectorPassResult. Several test
// call sites were missed and silently left using the old ad-hoc positional
// signature, causing `go vet ./...` failures. Those were fixed in PR #2503
// as a side-effect (closes #2509).
//
// This file prevents the regression from recurring: any pass whose
// signature drifts from func(DetectorPassArgs) DetectorPassResult will
// produce a compile-time type mismatch here, making `go build ./...` (and
// therefore the pre-commit gate) fail immediately rather than silently.
package engine

// passSignature is the canonical type that every top-level engine detector
// pass must satisfy.
type passSignature = func(DetectorPassArgs) DetectorPassResult

// _ is a blank-identifier compile-time assertion. The composite literal
// below must type-check as []passSignature. If any function listed here
// has the wrong signature, the build fails with a clear type-mismatch
// error pointing at the offending pass.
//
// Keep this list in the same order as the applyPass call sequence in
// detector.go's Detect method so it is easy to audit against that file.
var _ = []passSignature{
	// Route composition passes
	applySpringRouteComposition,
	applySpringRouteCompositionKotlin,
	applyDjangoRouteComposition,
	applyGoRouteComposition,

	// HTTP / endpoint synthesis
	applyHTTPEndpointSynthesis,

	// ORM
	applyORMQueries,
	applyORMFieldEdges,

	// Message-broker edge passes
	applyKafkaEdges,
	applyKafkaWrapperEdges,
	applyRabbitMQEdges,
	applySQSEdges,
	applyIaCSNSEdges,
	applyDebeziumCDCEdges,
	applyPubSubEdges,
	applyNATSEdges,
	applyPulsarEdges,
	applyRedisPubSubEdges,
	applyBullMQEdges,
	applyInngestEdges,
	applyEventBusEdges,

	// Real-time / streaming
	applyWebSocketSynthesis,
	applySSESynthesis,
	applyGraphQLSubscriptionSynthesis,

	// Scheduling, webhooks, RPC
	applyScheduledJobEdges,
	applyWebhookEdges,
	applyGRPCEdges,

	// Serverless / workflow
	applyServerlessEdges,
	applyWorkflowEdges,
	applySFNStartExecutionEdges,
}
