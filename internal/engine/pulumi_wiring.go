// Pulumi AWS event-source / trigger wiring — #5501, stack epic #5479.
//
// Background / the gap this closes
// --------------------------------
// pulumi_edges.go extracts Pulumi resources + DEPENDS_ON edges (output refs,
// explicit dependsOn). What it did NOT model is the AWS event-source / trigger
// WIRING that connects two resources through a dedicated wiring resource:
//
//	new aws.lambda.EventSourceMapping("m", { eventSourceArn: queue.arn,
//	                                         functionName: fn.name });
//	new aws.s3.BucketNotification("n", { bucket: data.id,
//	                                     lambdaFunctions: [{ lambdaFunctionArn: fn.arn }] });
//	new aws.apigatewayv2.Integration("i", { apiId: api.id, integrationUri: fn.arn });
//	new aws.sns.TopicSubscription("s", { topic: alerts.arn, endpoint: fn.arn });
//	new aws.cloudwatch.EventTarget("t", { rule: cron.name, arn: fn.arn });
//
// Each of these is itself a resource, so the existing output_ref miner already
// emitted DEPENDS_ON wiring-resource→queue and wiring-resource→fn (the mapping
// depends on both). That, however, hides the REAL topology: the message flows
// SOURCE → TARGET (queue → lambda). This pass recognises the wiring resource
// types and emits a direct DEPENDS_ON edge SOURCE → TARGET carrying
// reason=event_source — the SAME edge kind + reason CDK's addEventSource pass
// emits (cdk_edges.go), so event-source wiring is uniform across IaC tools.
//
// Reference resolution reuses the same var→logicalName binding the rest of
// pulumi_edges.go uses: an arg value `queue.arn` / `fn.name` is resolved by its
// leading variable (`queue` / `fn`) to the resource entity it names.
//
// Scope guard
// -----------
// Append-only: emits edges only, never mutates existing entities/edges. The
// emitDependsOn closure dedupes, so re-deriving an edge is a no-op.
//
// Refs #5501, #5479.
package engine

import (
	"regexp"
	"strings"
)

// pulumiWiringSpec describes one AWS event-source / trigger wiring resource
// type: the arg holding the SOURCE reference and the arg holding the TARGET
// reference. The emitted edge is source → target (the direction a message /
// event flows), reason=event_source.
type pulumiWiringSpec struct {
	// sourceArgs lists candidate arg keys carrying the event SOURCE reference,
	// in priority order (first one that resolves wins).
	sourceArgs []string
	// targetArgs lists candidate arg keys carrying the event TARGET reference.
	targetArgs []string
}

// pulumiWiringTypes maps a (lower-cased) Pulumi AWS wiring resource type to its
// source/target arg spec. Matching is by suffix on the lower-cased construct
// type so it tolerates the provider prefix (`aws.lambda.eventsourcemapping`)
// and the Go/C# spellings.
var pulumiWiringTypes = map[string]pulumiWiringSpec{
	// SQS / DynamoDB-stream / Kinesis → Lambda.
	"lambda.eventsourcemapping": {
		sourceArgs: []string{"eventSourceArn", "event_source_arn"},
		targetArgs: []string{"functionName", "function_name"},
	},
	// S3 bucket → Lambda (and/or SNS/SQS) notification.
	"s3.bucketnotification": {
		sourceArgs: []string{"bucket"},
		targetArgs: []string{"lambdaFunctions", "lambda_functions", "lambdaFunctionArn",
			"lambda_function_arn", "queues", "topics"},
	},
	// API Gateway v2 integration → Lambda.
	"apigatewayv2.integration": {
		sourceArgs: []string{"apiId", "api_id"},
		targetArgs: []string{"integrationUri", "integration_uri"},
	},
	// API Gateway (v1) integration → Lambda.
	"apigateway.integration": {
		sourceArgs: []string{"restApi", "rest_api", "restApiId", "rest_api_id"},
		targetArgs: []string{"uri"},
	},
	// SNS topic → subscriber (lambda / sqs / http endpoint).
	"sns.topicsubscription": {
		sourceArgs: []string{"topic", "topicArn", "topic_arn"},
		targetArgs: []string{"endpoint"},
	},
	// CloudWatch / EventBridge rule → target.
	"cloudwatch.eventtarget": {
		sourceArgs: []string{"rule"},
		targetArgs: []string{"arn", "targetId", "target_id"},
	},
	// Lambda permission granting a source service invoke rights → the function.
	// principal is a service string, but `sourceArn` (when present) names the
	// triggering resource, so source(sourceArn) → target(function).
	"lambda.permission": {
		sourceArgs: []string{"sourceArn", "source_arn"},
		targetArgs: []string{"function", "functionName", "function_name"},
	},
}

// pulumiWiringSpecFor returns the wiring spec for a resource type, matching by
// suffix on the lower-cased type so the provider prefix is irrelevant. Returns
// (spec, true) when the type is a recognised wiring resource.
func pulumiWiringSpecFor(resourceType string) (pulumiWiringSpec, bool) {
	// Normalise the Python `lambda_` alias (and any other trailing-underscore
	// segment alias) by dropping underscores, so `aws.lambda_.EventSourceMapping`
	// matches the `lambda.eventsourcemapping` suffix key.
	t := strings.ReplaceAll(strings.ToLower(resourceType), "_", "")
	for suffix, spec := range pulumiWiringTypes {
		if strings.HasSuffix(t, suffix) {
			return spec, true
		}
	}
	return pulumiWiringSpec{}, false
}

// pulumiArgRefRe captures a single `key: <ref>` (TS) / `key = <ref>` (Python/Go/
// C#) assignment where the value begins with a `<var>` identifier we can resolve
// to a resource. Group 1 = key, group 2 = leading variable of the value. It is
// permissive on the value tail (anything up to comma/brace/newline) so
// `queue.arn`, `fn.name`, `[{ lambdaFunctionArn: fn.arn }]` all surface their
// leading variable — `lambdaFunctions: [...]` resolves via the refs INSIDE the
// list because those `fn.arn` occurrences match this same regex within the body.
var pulumiArgRefRe = regexp.MustCompile(
	`(?m)(?:^|[\s,{(\[])([A-Za-z_][\w]*)\s*[:=]\s*([A-Za-z_][\w]*)\s*\.\s*[A-Za-z_]`,
)

// applyPulumiWiringEdges scans a wiring resource's args body, resolves the
// source and target references to resource logical names, and emits a
// source→target DEPENDS_ON edge with reason=event_source. It is a no-op when the
// resource type is not a recognised wiring type or when neither end resolves.
//
// Resolution: every `key: <var>.<attr>` in the body yields (key → var). For
// keys nested in a list value (`lambdaFunctions: [{ lambdaFunctionArn: fn.arn }]`)
// BOTH the outer key and the inner key are captured, so the target resolves via
// the inner `lambdaFunctionArn: fn.arn`. We collect ALL target candidates so a
// fan-out notification (one bucket → many lambdas) wires every target.
func applyPulumiWiringEdges(
	resourceType, argsBody string,
	varToName map[string]string,
	emitDependsOn func(fromName, toName, reason, detail string),
) {
	spec, ok := pulumiWiringSpecFor(resourceType)
	if !ok || argsBody == "" {
		return
	}

	// Collect every key→var mapping in the args body (including nested).
	keyToVars := map[string][]string{}
	for _, m := range pulumiArgRefRe.FindAllStringSubmatch(argsBody, -1) {
		key := m[1]
		varName := m[2]
		keyToVars[key] = append(keyToVars[key], varName)
	}

	resolve := func(keys []string) []string {
		var names []string
		seen := map[string]bool{}
		for _, k := range keys {
			for _, v := range keyToVars[k] {
				name, ok := varToName[v]
				if !ok || seen[name] {
					continue
				}
				seen[name] = true
				names = append(names, name)
			}
		}
		return names
	}

	sources := resolve(spec.sourceArgs)
	targets := resolve(spec.targetArgs)
	if len(sources) == 0 || len(targets) == 0 {
		return
	}

	for _, src := range sources {
		for _, tgt := range targets {
			emitDependsOn(src, tgt, "event_source", resourceType)
		}
	}
}
