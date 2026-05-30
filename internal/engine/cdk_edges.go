// AWS CDK (TypeScript) resource + dependency extraction — part of #3512.
//
// Background / the gap this closes
// --------------------------------
// AWS CDK is the marquee IaC framework, but its resource extraction was 0%
// functional while the coverage registry stamped it `full`. The cause was two
// independent defects:
//
//  1. The CDK FrameworkRule YAML lived under rules/cdk/, which the loader
//     buckets by top-level dir name into language key "cdk". The detector
//     resolves compiled rules by file.Language (typescript/javascript/...), and
//     no file is ever tagged "cdk", so the rules never fired. (Fixed by
//     relocating the YAML to rules/javascript_typescript/frameworks/aws_cdk.yaml,
//     which the existing javascript_typescript → typescript/javascript alias in
//     detector.compile() maps onto real .ts/.js files.)
//
//  2. The registry's resource_extraction `full` stamp for infra.resource.aws-cdk
//     cited internal/extractors/hcl/extractor.go — the Terraform extractor, which
//     cannot parse .ts. A pure mis-stamp.
//
// What this pass extracts (CDK-TS)
// --------------------------------
// The #1 CDK idiom is `new <ns>.<Type>(this, 'LogicalId', { ...props })`:
//
//	const dataBucket = new s3.Bucket(this, 'DataBucket', { versioned: true });
//	const handler    = new lambda.Function(this, 'Handler', { ... });
//	dataBucket.grantRead(handler);
//
// For each construct we emit a SCOPE.InfraResource entity NAMED by its
// 'LogicalId' string literal (the stable CDK identity), carrying the construct
// TYPE (`s3.Bucket`) and a coarse scope class (service / datastore / queue) in
// its properties. L1 escape-hatch constructs `new CfnBucket(this,'id',{...})`
// are captured the same way.
//
// Dependency edges (mirroring the hcl extractor's depends_on → DEPENDS_ON):
//
//   - `bucket.grantRead(fn)` / `bucket.grantWrite(fn)` / `*.grant*(fn)` →
//     DEPENDS_ON  fn-resource → bucket-resource  (the grantee depends on the
//     resource it was granted access to). Property grant=<method>.
//   - `fn.addEventSource(new SqsEventSource(queue))` →
//     DEPENDS_ON  fn-resource → queue-resource.
//   - a construct variable passed into another construct's props
//     (`new lambda.Function(this,'F',{ bucket })` or `{ bucket: dataBucket }`)
//     → DEPENDS_ON  enclosing-construct → passed-construct.
//
// The file-local variable → LogicalId binding (built from the `const X = new
// ...` assignment) is what lets a `dataBucket.grantRead(handler)` call resolve
// both endpoints to their LogicalId-named resource entities.
//
// Scope guard
// -----------
// Append-only: this pass never modifies or removes existing entities or edges,
// so it cannot regress the surrounding pipeline's bug-rate. Establishes the
// per-language-bucket pattern that CDK-Python / Pulumi / CDK8s reuse.
//
// Refs #3512.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// cdkResourceKind is the entity kind for every CDK construct resource. We use
// the dedicated SCOPE.InfraResource kind (a registered producer kind) rather
// than overloading SCOPE.Service, so IaC resources are queryable as a class and
// distinguishable from application services.
const cdkResourceKind = "SCOPE.InfraResource"

// cdkDependsOnEdgeKind mirrors the hcl extractor's depends_on → DEPENDS_ON edge
// kind so CDK and Terraform dependency edges are uniform across IaC tools.
const cdkDependsOnEdgeKind = "DEPENDS_ON"

// cdkSupportsLanguage reports whether applyCDKEdges scans `lang`. This PR scopes
// CDK to TypeScript/JavaScript (the flagship CDK language); CDK-Python/Java/Go/C#
// follow later under their own language buckets.
func cdkSupportsLanguage(lang string) bool {
	switch lang {
	case "javascript", "typescript":
		return true
	default:
		return false
	}
}

// cdkResourceCoarseScope maps a CDK construct type (e.g. "s3.Bucket",
// "lambda.Function", "sqs.Queue", "dynamodb.Table", "CfnDBInstance") to a coarse
// architectural scope class. Mirrors the intent of the (dead) iacResourceKind
// helper but returns a bare class string recorded as a property — the entity
// Kind stays SCOPE.InfraResource so all CDK resources remain a single queryable
// class. Matching is on the lower-cased construct type.
func cdkResourceCoarseScope(constructType string) string {
	t := strings.ToLower(constructType)
	switch {
	case strings.Contains(t, "rds") || strings.Contains(t, "dynamodb") ||
		strings.Contains(t, "database") || strings.Contains(t, "dbinstance") ||
		strings.Contains(t, "dbcluster") || strings.Contains(t, "table") ||
		strings.Contains(t, "bucket") || strings.Contains(t, "elasticache") ||
		strings.Contains(t, "redshift"):
		return "datastore"
	case strings.Contains(t, "sqs") || strings.Contains(t, "queue") ||
		strings.Contains(t, "sns") || strings.Contains(t, "topic") ||
		strings.Contains(t, "kinesis") || strings.Contains(t, "eventbus"):
		return "queue"
	default:
		return "service"
	}
}

// cdkConstructDeclRe captures `const|let|var VAR = new <ns>.<Type>(this, 'LogicalId'`.
// Group 1 = JS var name, group 2 = construct type (ns.Type, e.g. "s3.Bucket"),
// group 3 = the 'LogicalId' string literal.
//
//	const dataBucket = new s3.Bucket(this, 'DataBucket', { versioned: true });
var cdkConstructDeclRe = regexp.MustCompile(
	`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*new\s+([A-Za-z_$][\w$.]*)\s*\(\s*(?:this|self|scope|stack|[A-Za-z_$][\w$]*)\s*,\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// cdkConstructAnonRe captures an UNASSIGNED construct instantiation
// `new <ns>.<Type>(this, 'LogicalId'` (no `const X =` prefix). Group 1 =
// construct type, group 2 = 'LogicalId'. Used to emit the resource entity even
// when the construct is not bound to a variable (still has a stable LogicalId).
var cdkConstructAnonRe = regexp.MustCompile(
	`new\s+([A-Za-z_$][\w$.]*)\s*\(\s*(?:this|self|scope|stack|[A-Za-z_$][\w$]*)\s*,\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// cdkGrantRe captures a grant call `<resourceVar>.grant<Something>(<granteeVar>`.
// Group 1 = the resource variable being granted, group 2 = the grant method
// (grantRead / grantWrite / grantReadWrite / grantPutEvents / grant / ...),
// group 3 = the grantee variable. Semantics: the grantee DEPENDS_ON the
// resource (it needs access to it).
var cdkGrantRe = regexp.MustCompile(
	`([A-Za-z_$][\w$]*)\s*\.\s*(grant[A-Za-z]*)\s*\(\s*([A-Za-z_$][\w$]*)`,
)

// cdkAddEventSourceRe captures `<fnVar>.addEventSource(new <SourceType>(<resourceVar>`.
// Group 1 = the function variable, group 2 = the wrapped resource variable
// (the queue/stream/bucket the event source reads from). The function
// DEPENDS_ON that resource.
var cdkAddEventSourceRe = regexp.MustCompile(
	`([A-Za-z_$][\w$]*)\s*\.\s*addEventSource\s*\(\s*new\s+[A-Za-z_$][\w$.]*\s*\(\s*([A-Za-z_$][\w$]*)`,
)

// cdkConstructCallRe is a non-greedy matcher for a full construct instantiation
// up to the closing of its props object/argument list, used to scan props for
// passed-in construct variables. Group 1 = the LogicalId of the construct whose
// props we are scanning, group 2 = the raw props text. We bound the props body
// at the first `})` / `)` that plausibly closes the call to stay file-local and
// avoid runaway matches; regexp cannot balance braces so this is a heuristic
// scan, not a parse.
var cdkConstructPropsRe = regexp.MustCompile(
	`new\s+[A-Za-z_$][\w$.]*\s*\(\s*(?:this|self|scope|stack|[A-Za-z_$][\w$]*)\s*,\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]\s*,\s*\{([\s\S]{0,600}?)\}\s*\)`,
)

// cdkPropsRefRe finds construct variables referenced inside a props body, both
// shorthand (`bucket,` / `bucket }`) and explicit (`bucket: dataBucket`). It
// captures every identifier that could be a construct reference; we filter
// against the known var→LogicalId binding map so only real constructs produce
// edges. Group 1 = explicit-value identifier (RHS of `key: ident`), group 2 =
// shorthand identifier.
var cdkPropsRefRe = regexp.MustCompile(
	`(?:[A-Za-z_$][\w$]*\s*:\s*([A-Za-z_$][\w$]*)|\b([A-Za-z_$][\w$]*)\b)`,
)

// applyCDKEdges APPENDS SCOPE.InfraResource entities + DEPENDS_ON edges for AWS
// CDK constructs in TypeScript/JavaScript. Append-only.
func applyCDKEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if !cdkSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	// Fast pre-filter: a CDK construct file imports from aws-cdk-lib (v2) or
	// @aws-cdk/* (v1) and instantiates constructs with `new`. Guards against
	// matching the generic `new X(this,'id',...)` idiom in non-CDK files.
	if !strings.Contains(src, "aws-cdk") && !strings.Contains(src, "aws_cdk") &&
		!strings.Contains(src, "constructs") {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	path := args.Path

	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}

	// varToLogical maps a file-local construct variable to its LogicalId, so a
	// `dataBucket.grantRead(handler)` call resolves both endpoints to their
	// LogicalId-named resource entities.
	varToLogical := map[string]string{}
	// logicalIDs is the set of LogicalIds we have emitted a resource for, used
	// to filter props-passed identifiers down to real constructs.
	logicalIDs := map[string]bool{}

	emitResource := func(logicalID, constructType string, offset int) {
		if logicalID == "" || constructType == "" {
			return
		}
		key := cdkResourceKind + "|" + logicalID + "|" + path
		if seenEnt[key] {
			return
		}
		seenEnt[key] = true
		logicalIDs[logicalID] = true
		entities = append(entities, types.EntityRecord{
			Name:       logicalID,
			Kind:       cdkResourceKind,
			SourceFile: path,
			Language:   lang,
			StartLine:  matchStartLine(src, offset),
			Properties: map[string]string{
				"iac_tool":       "aws-cdk",
				"construct_type": constructType,
				"resource_scope": cdkResourceCoarseScope(constructType),
				"logical_id":     logicalID,
				"pattern_type":   "cdk_synthesis",
			},
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	// emitDependsOn records `fromLogical --DEPENDS_ON--> toLogical`, the same
	// edge kind the hcl extractor emits for Terraform `depends_on`.
	emitDependsOn := func(fromLogical, toLogical, reason, detail string) {
		if fromLogical == "" || toLogical == "" || fromLogical == toLogical {
			return
		}
		fromID := fmt.Sprintf("%s:%s", cdkResourceKind, fromLogical)
		toID := fmt.Sprintf("%s:%s", cdkResourceKind, toLogical)
		key := fromID + "|" + toID + "|" + reason + "|" + detail
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		props := map[string]string{
			"iac_tool":     "aws-cdk",
			"pattern_type": "cdk_synthesis",
			"reason":       reason,
		}
		if detail != "" {
			props[reason] = detail
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       cdkDependsOnEdgeKind,
			Properties: props,
		})
	}

	// Pass 1: assigned construct declarations — `const X = new ns.Type(this,'Id'`.
	// Record both the resource entity and the var→LogicalId binding.
	for _, m := range cdkConstructDeclRe.FindAllStringSubmatchIndex(src, -1) {
		varName := extractGroupFromIndex(src, m, 1)
		constructType := extractGroupFromIndex(src, m, 2)
		logicalID := extractGroupFromIndex(src, m, 3)
		if logicalID == "" || constructType == "" {
			continue
		}
		emitResource(logicalID, constructType, m[0])
		if varName != "" {
			varToLogical[varName] = logicalID
		}
	}

	// Pass 2: anonymous (unassigned) construct instantiations — still emit the
	// resource keyed by LogicalId. Dedup against Pass 1 via seenEnt.
	for _, m := range cdkConstructAnonRe.FindAllStringSubmatchIndex(src, -1) {
		constructType := extractGroupFromIndex(src, m, 1)
		logicalID := extractGroupFromIndex(src, m, 2)
		if logicalID == "" || constructType == "" {
			continue
		}
		// Skip event-source / subscription wrapper constructs whose first arg is
		// a resource variable, not a (scope, id) pair — these are handled as
		// dependency edges, not standalone resources. Heuristic: a real construct
		// declaration always has `this`/scope as the first arg, which the regex
		// already requires, so anonymous matches here are genuine constructs.
		emitResource(logicalID, constructType, m[0])
	}

	// Pass 3: grant edges — `<resourceVar>.grant*(<granteeVar>)`. The grantee
	// DEPENDS_ON the resource it was granted access to.
	for _, m := range cdkGrantRe.FindAllStringSubmatch(src, -1) {
		resourceVar, grantMethod, granteeVar := m[1], m[2], m[3]
		resLogical := varToLogical[resourceVar]
		granteeLogical := varToLogical[granteeVar]
		if resLogical == "" || granteeLogical == "" {
			continue
		}
		emitDependsOn(granteeLogical, resLogical, "grant", grantMethod)
	}

	// Pass 4: event-source edges — `<fnVar>.addEventSource(new Src(<resourceVar>`.
	// The function DEPENDS_ON the event-source resource.
	for _, m := range cdkAddEventSourceRe.FindAllStringSubmatch(src, -1) {
		fnVar, resourceVar := m[1], m[2]
		fnLogical := varToLogical[fnVar]
		resLogical := varToLogical[resourceVar]
		if fnLogical == "" || resLogical == "" {
			continue
		}
		emitDependsOn(fnLogical, resLogical, "event_source", "")
	}

	// Pass 5: props-passed construct references. When a construct variable is
	// passed into another construct's props (`{ bucket }` / `{ bucket: dataBucket }`),
	// the enclosing construct DEPENDS_ON the passed construct.
	for _, m := range cdkConstructPropsRe.FindAllStringSubmatch(src, -1) {
		enclosingLogical := m[1]
		propsBody := m[2]
		if !logicalIDs[enclosingLogical] || propsBody == "" {
			continue
		}
		for _, rm := range cdkPropsRefRe.FindAllStringSubmatch(propsBody, -1) {
			ref := rm[1]
			if ref == "" {
				ref = rm[2]
			}
			passedLogical, ok := varToLogical[ref]
			if !ok || passedLogical == enclosingLogical {
				continue
			}
			emitDependsOn(enclosingLogical, passedLogical, "props_ref", ref)
		}
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}
