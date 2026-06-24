// Pulumi (TypeScript + Python) resource + dependency extraction — #3528,
// epic #3512.
//
// Background / the gap this closes
// --------------------------------
// Pulumi is a code-first IaC platform: infrastructure is declared with ordinary
// program code (`new aws.s3.Bucket("data", {...})` in TS, `aws.s3.Bucket("data",
// ...)` in Python) rather than a declarative DSL. The coverage registry stamped
// infra.resource.pulumi `full` citing internal/extractors/hcl/extractor.go — the
// Terraform extractor, which cannot parse .ts/.py. A pure mis-stamp, identical in
// shape to the AWS-CDK gap (#3512). The dormant rules/pulumi/ YAML bucket never
// fired either, because the rule loader keys rules by top-level directory name
// ("pulumi") while real files are tagged "typescript"/"python".
//
// What this pass extracts
// -----------------------
// The Pulumi resource idiom is a constructor call whose FIRST argument is the
// resource's logical name string:
//
//	TS:      const data = new aws.s3.Bucket("data", { versioned: true });
//	         const fn   = new aws.lambda.Function("fn", { role: role.arn });
//	Python:  data = aws.s3.Bucket("data", versioned=True)
//	         fn   = pulumi_aws.lambda_.Function("fn", role=role.arn)
//
// For each resource we emit a SCOPE.InfraResource entity NAMED by its logical
// name string literal, carrying the construct TYPE (`aws.s3.Bucket`) and a coarse
// scope class (service / datastore / queue) in its properties.
//
// Dependency edges (mirroring the hcl extractor's depends_on → DEPENDS_ON, same
// edge kind CDK uses so all IaC dependency edges are uniform):
//
//   - an output of one resource (`bucket.arn` / `bucket.id` / `queue.url`) passed
//     into another resource's args → DEPENDS_ON  consumer → producer.
//   - an explicit `{ dependsOn: [bucket] }` (TS) / `opts=pulumi.ResourceOptions(
//     depends_on=[bucket])` (Python) → DEPENDS_ON  resource → each listed dep.
//   - `new pulumi.StackReference("org/project/stack")` →
//     DEPENDS_ON  this-stack-ref-node → `pulumi-stack:<ref>` cross-stack node.
//
// ComponentResource subclasses (`class X extends pulumi.ComponentResource` /
// `class X(pulumi.ComponentResource)`) are recorded as SCOPE.InfraResource of
// scope `component` so the module boundary is queryable.
//
// Scope guard
// -----------
// Append-only: this pass never modifies or removes existing entities or edges,
// so it cannot regress the surrounding pipeline's bug-rate. Mirrors the
// per-language design of cdk_edges.go.
//
// Refs #3512, #3528.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// pulumiResourceKind is the entity kind for every Pulumi resource — the same
// dedicated SCOPE.InfraResource kind CDK/CFN use, so IaC resources across tools
// are a single queryable class.
const pulumiResourceKind = "SCOPE.InfraResource"

// pulumiDependsOnEdgeKind mirrors the hcl extractor's depends_on → DEPENDS_ON
// edge kind so Pulumi, CDK and Terraform dependency edges are uniform.
const pulumiDependsOnEdgeKind = "DEPENDS_ON"

// pulumiSupportsLanguage reports whether applyPulumiEdges scans `lang`.
// TypeScript/JavaScript and Python are implemented; Go/C#/Java follow later.
func pulumiSupportsLanguage(lang string) bool {
	switch lang {
	case "javascript", "typescript", "python":
		return true
	default:
		return pulumiSupportsLanguageGoNet(lang)
	}
}

func pulumiIsPython(lang string) bool { return lang == "python" }

// pulumiResourceCoarseScope returns the uniform IaC resource_category for a
// Pulumi resource type (e.g. "aws.s3.Bucket", "aws.lambda.Function",
// "aws.sqs.Queue", "aws.dynamodb.Table"). It now delegates to the ONE shared
// classifier (types.IaCResourceCategory) so Pulumi resources carry exactly the
// same `resource_category` values as Terraform / CDK / CFN / Bicep (#3549). The
// entity Kind stays SCOPE.InfraResource so existing Pulumi QualifiedNames and
// DEPENDS_ON edges are unchanged. Matching is on the lower-cased type.
func pulumiResourceCoarseScope(resourceType string) string {
	return types.IaCResourceCategory(resourceType)
}

// pulumiCrossStackNodeID is the canonical node id for a StackReference target,
// so two programs referencing the same upstream stack collapse onto one node.
func pulumiCrossStackNodeID(ref string) string {
	return "pulumi-stack:" + ref
}

// ---------------------------------------------------------------------------
// TypeScript regexes.
// ---------------------------------------------------------------------------

// pulumiTSResourceDeclRe captures `const|let|var VAR = new <provider>.<...>.<Type>("name"`.
// Group 1 = JS var name, group 2 = resource type (provider.[svc.]Type), group 3
// = the "name" logical-name string literal.
//
//	const data = new aws.s3.Bucket("data", { versioned: true });
var pulumiTSResourceDeclRe = regexp.MustCompile(
	`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*new\s+([A-Za-z_$][\w$.]*)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// pulumiTSResourceAnonRe captures an UNASSIGNED `new <type>("name"`.
var pulumiTSResourceAnonRe = regexp.MustCompile(
	`new\s+([A-Za-z_$][\w$.]*)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// pulumiTSComponentRe captures `class X extends pulumi.ComponentResource`.
var pulumiTSComponentRe = regexp.MustCompile(
	`class\s+([A-Za-z_$][\w$]*)\s+extends\s+(?:pulumi\.)?ComponentResource\b`,
)

// pulumiTSStackRefRe captures `new pulumi.StackReference("org/project/stack")`
// optionally assigned. Group 1 = the StackReference target string.
var pulumiTSStackRefRe = regexp.MustCompile(
	`(?:new\s+)?(?:pulumi\.)?StackReference\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// pulumiTSResourceArgsRe matches a full resource constructor and captures its
// args body so we can scan it for `<var>.arn` / `.id` references and dependsOn.
// Group 1 = the resource's logical name, group 2 = the args text.
var pulumiTSResourceArgsRe = regexp.MustCompile(
	`new\s+[A-Za-z_$][\w$.]*\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]\s*,([\s\S]{0,800}?)\)\s*;`,
)

// ---------------------------------------------------------------------------
// Python regexes.
// ---------------------------------------------------------------------------

// pulumiPyResourceDeclRe captures `VAR = <provider>.<...>.<Type>("name"`.
// Group 1 = Python var, group 2 = resource type, group 3 = logical name.
//
//	data = aws.s3.Bucket("data", versioned=True)
//	fn   = pulumi_aws.lambda_.Function("fn", ...)
var pulumiPyResourceDeclRe = regexp.MustCompile(
	`([A-Za-z_][\w]*)\s*=\s*((?:pulumi_)?[A-Za-z_][\w.]*)\s*\(\s*['"]([^'"\n\r]+)['"]`,
)

// pulumiPyResourceAnonRe captures an UNASSIGNED `<type>("name"`.
var pulumiPyResourceAnonRe = regexp.MustCompile(
	`((?:pulumi_)?[A-Za-z_][\w.]*)\s*\(\s*['"]([^'"\n\r]+)['"]`,
)

// pulumiPyComponentRe captures `class X(pulumi.ComponentResource)`.
var pulumiPyComponentRe = regexp.MustCompile(
	`class\s+([A-Za-z_][\w]*)\s*\(\s*(?:pulumi\.)?ComponentResource\s*\)`,
)

// pulumiPyStackRefRe captures `pulumi.StackReference("org/project/stack")`.
var pulumiPyStackRefRe = regexp.MustCompile(
	`(?:pulumi\.)?StackReference\s*\(\s*['"]([^'"\n\r]+)['"]`,
)

// pulumiPyResourceArgsRe matches a full Python resource constructor and captures
// its args. Group 1 = logical name, group 2 = args body.
var pulumiPyResourceArgsRe = regexp.MustCompile(
	`(?:pulumi_)?[A-Za-z_][\w.]*\s*\(\s*['"]([^'"\n\r]+)['"]\s*,([\s\S]{0,800}?)\)\n`,
)

// pulumiOutputRefRe finds `<var>.arn` / `<var>.id` / `<var>.url` / `<var>.name`
// output references inside an args body (shared by TS + Python). Group 1 = the
// producing resource variable.
var pulumiOutputRefRe = regexp.MustCompile(
	`\b([A-Za-z_][\w]*)\s*\.\s*(?:arn|id|url|name|endpoint|bucket|topic_arn|queue_url|queueUrl|topicArn)\b`,
)

// pulumiDependsOnListRe finds the contents of a `dependsOn` / `depends_on`
// list/value. Group 1 = the raw list text (e.g. `[bucket, queue]` or `bucket`).
var pulumiDependsOnListRe = regexp.MustCompile(
	`depends?[_]?[oO]n\s*[=:]\s*(\[[^\]]*\]|[A-Za-z_][\w]*)`,
)

// pulumiIdentRe extracts bare identifiers from a dependsOn list body.
var pulumiIdentRe = regexp.MustCompile(`[A-Za-z_][\w]*`)

// applyPulumiEdges APPENDS SCOPE.InfraResource entities + DEPENDS_ON edges for
// Pulumi programs in TypeScript/JavaScript and Python. Append-only.
func applyPulumiEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if !pulumiSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	// Fast pre-filter: a Pulumi program imports the Pulumi SDK. TS:
	// `@pulumi/pulumi` / `@pulumi/aws`; Python: `import pulumi` / `pulumi_aws`.
	// Go imports `github.com/pulumi/pulumi-aws/sdk/.../go/aws/...` and
	// `github.com/pulumi/pulumi/sdk/.../go/pulumi`; C# imports `using Pulumi;`.
	if !strings.Contains(src, "@pulumi/") && !strings.Contains(src, "pulumi_") &&
		!strings.Contains(src, "import pulumi") && !strings.Contains(src, "* as pulumi") &&
		!strings.Contains(src, "pulumi/pulumi") && !strings.Contains(src, "Pulumi") {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	path := args.Path

	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}

	// varToName maps a file-local resource variable to its logical name, so an
	// output reference (`bucket.arn`) or dependsOn list resolves to the named
	// resource entity.
	varToName := map[string]string{}
	resourceNames := map[string]bool{}
	// nameToType maps a resource logical name to its construct type so the
	// event-source wiring pass (#5501) can tell whether a resource is a wiring
	// resource (EventSourceMapping / BucketNotification / Integration / ...).
	nameToType := map[string]string{}

	emitResource := func(logicalName, resourceType, scopeOverride string, offset int) {
		if logicalName == "" || resourceType == "" {
			return
		}
		nameToType[logicalName] = resourceType
		key := pulumiResourceKind + "|" + logicalName + "|" + path
		if seenEnt[key] {
			return
		}
		seenEnt[key] = true
		resourceNames[logicalName] = true
		scope := scopeOverride
		if scope == "" {
			scope = pulumiResourceCoarseScope(resourceType)
		}
		entities = append(entities, types.EntityRecord{
			Name:       logicalName,
			Kind:       pulumiResourceKind,
			SourceFile: path,
			Language:   lang,
			StartLine:  matchStartLine(src, offset),
			Properties: map[string]string{
				"iac_tool":          "pulumi",
				"construct_type":    resourceType,
				"resource_category": scope,
				// resource_scope kept (== resource_category) for back-compat.
				"resource_scope": scope,
				"logical_id":     logicalName,
				"pattern_type":   "pulumi_program",
			},
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	// stampScalarProps stamps curated literal scalar config props (epic #4194)
	// from a resource's args body onto its already-emitted InfraResource entity,
	// by logical name. It mutates the local `entities` slice in place. No-op when
	// the resource has no entity yet or no curated scalar props.
	stampScalarProps := func(logicalName, argsBody string) {
		if logicalName == "" || argsBody == "" {
			return
		}
		scalars := iacCodeExtractScalarProperties(argsBody)
		if len(scalars) == 0 {
			return
		}
		for i := range entities {
			if entities[i].Kind != pulumiResourceKind || entities[i].Name != logicalName ||
				entities[i].SourceFile != path {
				continue
			}
			if entities[i].Properties == nil {
				entities[i].Properties = map[string]string{}
			}
			for k, v := range scalars {
				if _, exists := entities[i].Properties[k]; !exists {
					entities[i].Properties[k] = v
				}
			}
			return
		}
	}

	emitDependsOn := func(fromName, toName, reason, detail string) {
		if fromName == "" || toName == "" || fromName == toName {
			return
		}
		fromID := fmt.Sprintf("%s:%s", pulumiResourceKind, fromName)
		toID := fmt.Sprintf("%s:%s", pulumiResourceKind, toName)
		key := fromID + "|" + toID + "|" + reason + "|" + detail
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		props := map[string]string{
			"iac_tool":     "pulumi",
			"pattern_type": "pulumi_program",
			"reason":       reason,
		}
		if detail != "" {
			props[reason] = detail
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       pulumiDependsOnEdgeKind,
			Properties: props,
		})
	}

	// emitCrossStack records a DEPENDS_ON edge from a per-file stack-reference
	// node to the canonical cross-stack node, plus the cross-stack node itself.
	emitCrossStack := func(ref string, offset int) {
		if ref == "" {
			return
		}
		nodeID := pulumiCrossStackNodeID(ref)
		key := pulumiResourceKind + "|" + nodeID + "|cross"
		if !seenEnt[key] {
			seenEnt[key] = true
			entities = append(entities, types.EntityRecord{
				Name:       nodeID,
				Kind:       pulumiResourceKind,
				SourceFile: path,
				Language:   lang,
				StartLine:  matchStartLine(src, offset),
				Properties: map[string]string{
					"iac_tool":          "pulumi",
					"construct_type":    "pulumi.StackReference",
					"resource_category": "stack_reference",
					"resource_scope":    "stack_reference",
					"logical_id":        ref,
					"pattern_type":      "pulumi_program",
				},
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.7,
			})
		}
	}

	if pulumiIsPython(lang) {
		applyPulumiEdgesPython(src, emitResource, emitDependsOn, emitCrossStack, stampScalarProps, varToName, resourceNames, nameToType)
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	if pulumiSupportsLanguageGoNet(lang) {
		applyPulumiEdgesGoNet(lang, src, emitResource, emitDependsOn, varToName, resourceNames)
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	// --- TypeScript / JavaScript ---

	// Pass 1: assigned resource declarations — `const X = new type("name"`.
	for _, m := range pulumiTSResourceDeclRe.FindAllStringSubmatchIndex(src, -1) {
		varName := extractGroupFromIndex(src, m, 1)
		resourceType := extractGroupFromIndex(src, m, 2)
		logicalName := extractGroupFromIndex(src, m, 3)
		if logicalName == "" || resourceType == "" {
			continue
		}
		// Skip pulumi.StackReference here — handled as a cross-stack node below.
		if strings.Contains(resourceType, "StackReference") {
			continue
		}
		emitResource(logicalName, resourceType, "", m[0])
		if varName != "" {
			varToName[varName] = logicalName
		}
	}

	// Pass 2: anonymous resource instantiations — `new type("name"`.
	for _, m := range pulumiTSResourceAnonRe.FindAllStringSubmatchIndex(src, -1) {
		resourceType := extractGroupFromIndex(src, m, 1)
		logicalName := extractGroupFromIndex(src, m, 2)
		if logicalName == "" || resourceType == "" {
			continue
		}
		if strings.Contains(resourceType, "StackReference") {
			continue
		}
		emitResource(logicalName, resourceType, "", m[0])
	}

	// Pass 3: ComponentResource subclasses → component-scoped resource node.
	for _, m := range pulumiTSComponentRe.FindAllStringSubmatchIndex(src, -1) {
		name := extractGroupFromIndex(src, m, 1)
		if name == "" {
			continue
		}
		emitResource(name, "pulumi.ComponentResource", "component", m[0])
	}

	// Pass 4: StackReference cross-stack nodes + edges.
	for _, m := range pulumiTSStackRefRe.FindAllStringSubmatchIndex(src, -1) {
		ref := extractGroupFromIndex(src, m, 1)
		emitCrossStack(ref, m[0])
	}

	// Pass 5: dependency edges from the args of each resource — output refs
	// (`other.arn`) and explicit `dependsOn` lists.
	for _, m := range pulumiTSResourceArgsRe.FindAllStringSubmatch(src, -1) {
		consumerName := m[1]
		argsBody := m[2]
		if !resourceNames[consumerName] || argsBody == "" {
			continue
		}
		// epic #4194: stamp curated literal scalar config props onto the entity.
		stampScalarProps(consumerName, argsBody)
		applyPulumiArgEdges(consumerName, argsBody, varToName, emitDependsOn)
		// #5501: if this resource is an event-source wiring resource, emit the
		// direct source→target event_source edge.
		applyPulumiWiringEdges(nameToType[consumerName], argsBody, varToName, emitDependsOn)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// applyPulumiArgEdges scans a resource's args body for output references and
// dependsOn lists and emits the consumer→producer DEPENDS_ON edges. Shared by
// the TS and Python passes.
func applyPulumiArgEdges(
	consumerName, argsBody string,
	varToName map[string]string,
	emitDependsOn func(fromName, toName, reason, detail string),
) {
	// Output references: `<var>.arn` / `.id` / `.url` / ...
	for _, rm := range pulumiOutputRefRe.FindAllStringSubmatch(argsBody, -1) {
		producerVar := rm[1]
		producerName, ok := varToName[producerVar]
		if !ok || producerName == consumerName {
			continue
		}
		emitDependsOn(consumerName, producerName, "output_ref", producerVar)
	}
	// Explicit dependsOn / depends_on lists.
	for _, dm := range pulumiDependsOnListRe.FindAllStringSubmatch(argsBody, -1) {
		listBody := dm[1]
		for _, im := range pulumiIdentRe.FindAllString(listBody, -1) {
			depName, ok := varToName[im]
			if !ok || depName == consumerName {
				continue
			}
			emitDependsOn(consumerName, depName, "depends_on", im)
		}
	}
}

// applyPulumiEdgesPython runs the Pulumi extraction passes against the Python
// idioms, reusing the shared emit closures and var→name binding map.
func applyPulumiEdgesPython(
	src string,
	emitResource func(logicalName, resourceType, scopeOverride string, offset int),
	emitDependsOn func(fromName, toName, reason, detail string),
	emitCrossStack func(ref string, offset int),
	stampScalarProps func(logicalName, argsBody string),
	varToName map[string]string,
	resourceNames map[string]bool,
	nameToType map[string]string,
) {
	// Pass 1: assigned resource declarations — `VAR = type("name"`.
	for _, m := range pulumiPyResourceDeclRe.FindAllStringSubmatchIndex(src, -1) {
		varName := extractGroupFromIndex(src, m, 1)
		resourceType := extractGroupFromIndex(src, m, 2)
		logicalName := extractGroupFromIndex(src, m, 3)
		if logicalName == "" || resourceType == "" {
			continue
		}
		if strings.Contains(resourceType, "StackReference") ||
			strings.HasSuffix(resourceType, "Config") {
			continue
		}
		emitResource(logicalName, resourceType, "", m[0])
		if varName != "" {
			varToName[varName] = logicalName
		}
	}

	// Pass 2: anonymous resource instantiations — `type("name"`.
	for _, m := range pulumiPyResourceAnonRe.FindAllStringSubmatchIndex(src, -1) {
		resourceType := extractGroupFromIndex(src, m, 1)
		logicalName := extractGroupFromIndex(src, m, 2)
		if logicalName == "" || resourceType == "" {
			continue
		}
		if strings.Contains(resourceType, "StackReference") {
			continue
		}
		emitResource(logicalName, resourceType, "", m[0])
	}

	// Pass 3: ComponentResource subclasses → component-scoped resource node.
	for _, m := range pulumiPyComponentRe.FindAllStringSubmatchIndex(src, -1) {
		name := extractGroupFromIndex(src, m, 1)
		if name == "" {
			continue
		}
		emitResource(name, "pulumi.ComponentResource", "component", m[0])
	}

	// Pass 4: StackReference cross-stack nodes.
	for _, m := range pulumiPyStackRefRe.FindAllStringSubmatchIndex(src, -1) {
		ref := extractGroupFromIndex(src, m, 1)
		emitCrossStack(ref, m[0])
	}

	// Pass 5: dependency edges from args — output refs + depends_on lists.
	for _, m := range pulumiPyResourceArgsRe.FindAllStringSubmatch(src, -1) {
		consumerName := m[1]
		argsBody := m[2]
		if !resourceNames[consumerName] || argsBody == "" {
			continue
		}
		// epic #4194: stamp curated literal scalar config props onto the entity.
		stampScalarProps(consumerName, argsBody)
		applyPulumiArgEdges(consumerName, argsBody, varToName, emitDependsOn)
		// #5501: event-source wiring source→target edge for wiring resources.
		applyPulumiWiringEdges(nameToType[consumerName], argsBody, varToName, emitDependsOn)
	}
}
