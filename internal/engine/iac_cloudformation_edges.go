// CloudFormation resource modeling + dependency edges — #3518 (epic #3512).
//
// Until now CloudFormation (and SAM) templates parsed as *generic* YAML: the
// whole `Resources:` block collapsed into one opaque top-level key, so the
// graph had near-zero CFN structure (the only exception was the SNS→SQS
// fan-out detection in iac_cloudformation_sns equivalent, iac_sns_edges.go).
// This pass adds first-class modeling:
//
//   - Each `LogicalId: { Type: AWS::*, Properties: {...} }` resource becomes a
//     SCOPE.* entity (Kind mapped from the AWS type via cfnResourceKind, the
//     CFN analogue of patterns.iacResourceKind: Datastore / Queue / Service…).
//   - Parameters, Outputs and Mappings become SCOPE.Schema / SCOPE.Config
//     entities so cross-stack references resolve.
//   - Dependency edges — the core gap — between a referencing resource and the
//     resource (or Parameter) it references:
//   - `!Ref X`              / `{ "Ref": "X" }`              → DEPENDS_ON
//   - `!GetAtt X.Attr`      / `{ "Fn::GetAtt": ["X","A"] }` → USES
//   - `DependsOn: X` / `[X]`                                → DEPENDS_ON
//   - `!Sub "...${X}..."`   / `{ "Fn::Sub": "...${X}..." }` → DEPENDS_ON
//   - Cross-stack: `!ImportValue Name` / `{ "Fn::ImportValue": ... }` and
//     `Outputs.<O>.Export.Name` emit a synthetic SCOPE.Config export node so a
//     producing stack's Export and a consuming stack's ImportValue collapse on
//     the same `cfn-export:<name>` ID (cross-repo / cross-stack join, no new
//     linker code).
//   - SAM: `AWS::Serverless::Function` joins the serverless_edges.go
//     `aws-lambda:<name>` synthetic; its `Events:` become triggers —
//   - Api / HttpApi  → http_endpoint_definition + ROUTES_TO
//   - SQS / SNS / ... → SUBSCRIBES_TO from the function to the queue/topic
//   - Schedule       → SCOPE.ScheduledJob + TRIGGERS
//   - `AWS::CloudFormation::Stack` TemplateURL → IMPORTS (nested stack).
//
// # Scope guard
//
// Append-only — every entity / edge this pass emits is new; it never modifies
// or removes anything produced by an earlier pass, so it cannot regress the
// surrounding pipeline's bug-rate. It is gated on actually looking like a CFN
// template (cfnIsTemplate), so non-CFN YAML is untouched.
//
// All helpers are prefixed `cfn` to avoid colliding with the SNS-fanout
// helpers in iac_sns_edges.go (which share the engine package).
//
// Refs #3518. Joins serverless_edges.go (#925) aws-lambda synthetics.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// Entity / edge kinds (reuse existing Kinds — no new producer Kind to register)
// ---------------------------------------------------------------------------

const (
	cfnSchemaEntityKind = "SCOPE.Schema"
	cfnConfigEntityKind = "SCOPE.Config"
	cfnScheduledJobKind = "SCOPE.ScheduledJob"

	cfnDependsOnEdgeKind = "DEPENDS_ON"
	cfnUsesEdgeKind      = "USES"
	cfnImportsEdgeKind   = "IMPORTS"
	cfnRoutesToEdgeKind  = "ROUTES_TO"
	cfnSubscribesToKind  = "SUBSCRIBES_TO"
	cfnTriggersEdgeKind  = "TRIGGERS"
)

// cfnResourceKind maps an AWS CloudFormation resource Type (e.g.
// "AWS::S3::Bucket") to the SCOPE.* scope used for the resource entity. It now
// delegates to the ONE shared classifier (types.IaCResourceCategory →
// types.IaCKindForCategory) so the CFN entity Kind can never diverge from the
// uniform `resource_category` property stamped on every IaC tool's resources
// (#3549). The historical CFN Kinds (SCOPE.Datastore / SCOPE.Queue /
// SCOPE.ServerlessFunction) are preserved by IaCKindForCategory's mapping:
// datastore/storage/cache→Datastore, queue/topic/stream→Queue, function→
// ServerlessFunction, everything else→SCOPE.InfraResource.
func cfnResourceKind(awsType string) string {
	return types.IaCKindForCategory(types.IaCResourceCategory(awsType))
}

// ---------------------------------------------------------------------------
// Template detection
// ---------------------------------------------------------------------------

// cfnTypeLineRe matches a resource `Type: AWS::Service::Resource` (YAML),
// optionally quoted.
var cfnTypeLineRe = regexp.MustCompile(`(?m)Type:\s*["']?(AWS::[\w:]+|Custom::[\w:]+)["']?`)

// cfnTypeJSONRe matches a JSON `"Type": "AWS::Service::Resource"`.
var cfnTypeJSONRe = regexp.MustCompile(`"Type"\s*:\s*"(AWS::[\w:]+|Custom::[\w:]+)"`)

// cfnIsTemplate reports whether src looks like a CloudFormation / SAM template:
// it must mention AWSTemplateFormatVersion or the SAM Transform, OR carry a
// `Resources:` block together with at least one `Type: AWS::*` child.
func cfnIsTemplate(src string) bool {
	if strings.Contains(src, "AWSTemplateFormatVersion") {
		return true
	}
	if strings.Contains(src, "AWS::Serverless-2016-10-31") {
		return true
	}
	hasResources := strings.Contains(src, "\nResources:") ||
		strings.HasPrefix(src, "Resources:") ||
		strings.Contains(src, `"Resources"`)
	if !hasResources {
		return false
	}
	return cfnTypeLineRe.MatchString(src) || cfnTypeJSONRe.MatchString(src)
}

// ---------------------------------------------------------------------------
// Resource blocks (YAML)
// ---------------------------------------------------------------------------

// cfnYAMLResourceHeaderRe matches a `  LogicalId:` 2-space-indented key inside
// the Resources section (same convention as cfnResourceHeaderRe in
// iac_sns_edges.go, kept private here under the cfn prefix to avoid coupling).
var cfnYAMLResourceHeaderRe = regexp.MustCompile(`(?m)^  ([A-Za-z0-9]+):\s*$`)

// cfnResource is a parsed resource: logical id, AWS type, body text, properties
// body text, DependsOn list, and source line.
type cfnResource struct {
	logicalID string
	typ       string
	body      string
	line      int
}

// cfnSectionBody returns the indented body of a top-level YAML section
// (`Name:` at column 0) up to the next top-level key. Returns "" if absent.
func cfnSectionBody(src, name string) string {
	marker := "\n" + name + ":"
	idx := strings.Index(src, marker)
	if idx < 0 {
		if strings.HasPrefix(src, name+":") {
			idx = 0
		} else {
			return ""
		}
	} else {
		idx++ // skip leading newline
	}
	rest := src[idx:]
	// Body starts after the header line.
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return ""
	}
	body := rest[nl+1:]
	// Cut at the next top-level (column-0, non-space) key line.
	lines := strings.Split(body, "\n")
	var out []string
	for _, ln := range lines {
		if len(ln) > 0 && ln[0] != ' ' && ln[0] != '\t' && strings.Contains(ln, ":") {
			break
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// cfnLineOf returns the 1-based line of a byte offset within src.
func cfnLineOf(src string, off int) int {
	if off < 0 || off > len(src) {
		return 1
	}
	return strings.Count(src[:off], "\n") + 1
}

// cfnParseYAMLResources splits the Resources: section into resource blocks.
func cfnParseYAMLResources(src string) []cfnResource {
	body := cfnSectionBody(src, "Resources")
	if body == "" {
		return nil
	}
	// Offset of the Resources body within src for line numbers.
	bodyOff := strings.Index(src, body)
	headers := cfnYAMLResourceHeaderRe.FindAllStringSubmatchIndex(body, -1)
	var blocks []cfnResource
	for i, h := range headers {
		id := body[h[2]:h[3]]
		start := h[1]
		end := len(body)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}
		blk := body[start:end]
		typ := ""
		if tm := cfnTypeLineRe.FindStringSubmatch(blk); tm != nil {
			typ = tm[1]
		}
		line := 1
		if bodyOff >= 0 {
			line = cfnLineOf(src, bodyOff+h[0])
		}
		blocks = append(blocks, cfnResource{logicalID: id, typ: typ, body: blk, line: line})
	}
	return blocks
}

// ---------------------------------------------------------------------------
// Resource blocks (JSON)
// ---------------------------------------------------------------------------

// cfnJSONResourceRe matches `"LogicalId": { ... "Type": "AWS::..." ... }` at a
// shallow level. We capture the logical id and use a brace-matched body. Group
// 1 = logical id.
var cfnJSONLogicalRe = regexp.MustCompile(`"([A-Za-z0-9]+)"\s*:\s*\{`)

// cfnParseJSONResources extracts resource blocks from a JSON template by
// locating the "Resources" object and brace-matching each logical-id value.
func cfnParseJSONResources(src string) []cfnResource {
	ri := strings.Index(src, `"Resources"`)
	if ri < 0 {
		return nil
	}
	// Find the opening brace of the Resources object.
	ob := strings.IndexByte(src[ri:], '{')
	if ob < 0 {
		return nil
	}
	start := ri + ob
	end := cfnMatchBrace(src, start)
	if end <= start {
		return nil
	}
	section := src[start+1 : end]
	sectionOff := start + 1
	var blocks []cfnResource
	for _, m := range cfnJSONLogicalRe.FindAllStringSubmatchIndex(section, -1) {
		id := section[m[2]:m[3]]
		// brace start is the last byte of the match (the `{`).
		braceRel := m[1] - 1
		bodyEnd := cfnMatchBrace(section, braceRel)
		if bodyEnd <= braceRel {
			continue
		}
		body := section[braceRel : bodyEnd+1]
		// Only keep blocks that declare a Type (skips nested non-resource objs).
		tm := cfnTypeJSONRe.FindStringSubmatch(body)
		if tm == nil {
			continue
		}
		blocks = append(blocks, cfnResource{
			logicalID: id,
			typ:       tm[1],
			body:      body,
			line:      cfnLineOf(src, sectionOff+m[0]),
		})
	}
	return blocks
}

// cfnMatchBrace returns the index of the `}` matching the `{` at openIdx,
// honoring string literals. Returns -1 on imbalance.
func cfnMatchBrace(s string, openIdx int) int {
	depth := 0
	inStr := false
	esc := false
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
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
// Reference extraction (Ref / GetAtt / DependsOn / Sub / ImportValue)
// ---------------------------------------------------------------------------

var (
	// !Ref X  and  { "Ref": "X" }
	cfnRefShortRe = regexp.MustCompile(`!Ref\s+([A-Za-z0-9]+)`)
	cfnRefLongRe  = regexp.MustCompile(`["']?Ref["']?\s*:\s*["']([A-Za-z0-9]+)["']`)

	// !GetAtt X.Attr  and  { "Fn::GetAtt": ["X", "Attr"] } / "X.Attr"
	cfnGetAttShortRe   = regexp.MustCompile(`!GetAtt\s+([A-Za-z0-9]+)\.[\w.]+`)
	cfnGetAttLongArrRe = regexp.MustCompile(`["']?Fn::GetAtt["']?\s*:\s*\[\s*["']([A-Za-z0-9]+)["']`)
	cfnGetAttLongStrRe = regexp.MustCompile(`["']?Fn::GetAtt["']?\s*:\s*["']([A-Za-z0-9]+)\.`)

	// DependsOn: X  |  DependsOn: [X, Y]  |  "DependsOn": ["X","Y"]
	cfnDependsOnRe     = regexp.MustCompile(`["']?DependsOn["']?\s*:\s*(.+)`)
	cfnIdentRe         = regexp.MustCompile(`[A-Za-z0-9]+`)
	cfnDependsScalarRe = regexp.MustCompile(`["']?DependsOn["']?\s*:\s*["']?([A-Za-z0-9]+)["']?\s*$`)

	// !Sub "...${X}..." / { "Fn::Sub": "...${X}..." } — capture ${Logical}
	// references (skip ${X.Attr} pseudo and ${AWS::...} pseudo-parameters).
	cfnSubVarRe = regexp.MustCompile(`\$\{\s*([A-Za-z0-9]+)\s*\}`)

	// !ImportValue Name  /  { "Fn::ImportValue": "Name" }
	cfnImportValueShortRe = regexp.MustCompile(`!ImportValue\s+["']?([\w:-]+)["']?`)
	cfnImportValueLongRe  = regexp.MustCompile(`["']?Fn::ImportValue["']?\s*:\s*["']?([\w:-]+)["']?`)
)

// cfnCollectRefs scans a resource body and returns the set of (logicalID → edgeKind)
// references it makes to other logical ids. GetAtt → USES; Ref/Sub/DependsOn →
// DEPENDS_ON. A USES wins over a duplicate DEPENDS_ON to the same target so the
// stronger semantic (attribute read) is recorded.
func cfnCollectRefs(body string) map[string]string {
	refs := map[string]string{}
	add := func(id, kind string) {
		if id == "" {
			return
		}
		if kind == cfnUsesEdgeKind {
			refs[id] = cfnUsesEdgeKind
		} else if refs[id] == "" {
			refs[id] = kind
		}
	}

	for _, m := range cfnGetAttShortRe.FindAllStringSubmatch(body, -1) {
		add(m[1], cfnUsesEdgeKind)
	}
	for _, m := range cfnGetAttLongArrRe.FindAllStringSubmatch(body, -1) {
		add(m[1], cfnUsesEdgeKind)
	}
	for _, m := range cfnGetAttLongStrRe.FindAllStringSubmatch(body, -1) {
		add(m[1], cfnUsesEdgeKind)
	}
	for _, m := range cfnRefShortRe.FindAllStringSubmatch(body, -1) {
		add(m[1], cfnDependsOnEdgeKind)
	}
	for _, m := range cfnRefLongRe.FindAllStringSubmatch(body, -1) {
		add(m[1], cfnDependsOnEdgeKind)
	}
	for _, m := range cfnSubVarRe.FindAllStringSubmatch(body, -1) {
		add(m[1], cfnDependsOnEdgeKind)
	}
	// DependsOn lines (explicit ordering).
	for _, line := range strings.Split(body, "\n") {
		lm := cfnDependsOnRe.FindStringSubmatch(line)
		if lm == nil {
			continue
		}
		val := strings.TrimSpace(lm[1])
		if val == "" || strings.HasPrefix(val, "#") {
			continue
		}
		// Scalar form on the same line.
		if sm := cfnDependsScalarRe.FindStringSubmatch(line); sm != nil && !strings.Contains(val, "[") {
			add(sm[1], cfnDependsOnEdgeKind)
			continue
		}
		// Inline / flow list form: DependsOn: [A, B] or ["A","B"].
		for _, id := range cfnIdentRe.FindAllString(val, -1) {
			add(id, cfnDependsOnEdgeKind)
		}
	}
	return refs
}

// cfnCollectImports returns the set of cross-stack export names referenced via
// Fn::ImportValue within a body.
func cfnCollectImports(body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range cfnImportValueShortRe.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	for _, m := range cfnImportValueLongRe.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Pass entry point
// ---------------------------------------------------------------------------

// cfnSupportsLanguage reports whether the CFN pass scans this language. CFN /
// SAM templates arrive classified as yaml or json.
func cfnSupportsLanguage(lang string) bool {
	switch lang {
	case "yaml", "json":
		return true
	default:
		return false
	}
}

// applyCloudFormationEdges is the engine pass entry point. Append-only.
func applyCloudFormationEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	entities := args.Entities
	relationships := args.Relationships
	if len(args.Content) == 0 || !cfnSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := string(args.Content)
	if !cfnIsTemplate(src) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	var resources []cfnResource
	if lang == "json" {
		resources = cfnParseJSONResources(src)
	} else {
		resources = cfnParseYAMLResources(src)
	}
	if len(resources) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	path := args.Path
	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}

	// resourceRef returns the canonical entity ID for a logical id. CFN resource
	// entities are file-scoped (a logical id is unique only within its template),
	// so we namespace by path.
	resourceRef := func(logicalID string) string {
		return fmt.Sprintf("cfn:%s#%s", path, logicalID)
	}

	emitEntity := func(id, kind, subtype, name string, line int, props map[string]string) {
		if seenEnt[id] {
			return
		}
		seenEnt[id] = true
		entities = append(entities, types.EntityRecord{
			Name:             id,
			Kind:             kind,
			Subtype:          subtype,
			QualifiedName:    id,
			SourceFile:       path,
			Language:         lang,
			StartLine:        line,
			EndLine:          line,
			Properties:       props,
			EnrichmentStatus: types.StatusPending,
			QualityScore:     0.8,
		})
	}

	emitEdge := func(fromID, toID, kind string, props types.Props) {
		if fromID == "" || toID == "" {
			return
		}
		key := kind + "|" + fromID + "|" + toID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       kind,
			Properties: props,
		})
	}

	// --- Parameters / Mappings → SCOPE.Schema; resolvable Ref targets. -------
	knownIDs := map[string]string{} // logicalID → entity ID (resources + params)
	paramBody := cfnSectionBody(src, "Parameters")
	for _, id := range cfnTopLevelKeys(paramBody) {
		eid := resourceRef(id)
		emitEntity(eid, cfnSchemaEntityKind, "cfn_parameter", id,
			cfnKeyLine(src, "Parameters", id),
			map[string]string{"kind": "iac", "iac_tool": "cloudformation", "cfn_section": "Parameters"})
		knownIDs[id] = eid
	}
	mappingBody := cfnSectionBody(src, "Mappings")
	for _, id := range cfnTopLevelKeys(mappingBody) {
		eid := resourceRef("Mapping_" + id)
		emitEntity(eid, cfnSchemaEntityKind, "cfn_mapping", id,
			cfnKeyLine(src, "Mappings", id),
			map[string]string{"kind": "iac", "iac_tool": "cloudformation", "cfn_section": "Mappings"})
		knownIDs[id] = eid
	}

	// --- Resources → entities. ----------------------------------------------
	for _, r := range resources {
		if r.logicalID == "" || r.typ == "" {
			continue
		}
		kind := cfnResourceKind(r.typ)
		eid := resourceRef(r.logicalID)
		props := types.Props{
			{K: "iac_tool", V: "cloudformation"},
			{K: "kind", V: "iac"},
			{K: "logical_id", V: r.logicalID},
			{K: "resource_category", V: types.IaCResourceCategory(r.typ)},
			{K: "resource_type", V: r.typ},
		}
		if strings.HasPrefix(r.typ, "AWS::Serverless::") {
			props.Set("iac_tool", "sam")
		}
		// Epic #4194 — stamp curated scalar config properties (InstanceType,
		// Runtime, MemorySize, Timeout, DBInstanceClass, ...) from the resource
		// body. Intrinsic-function values (Ref/GetAtt/Sub/...) are skipped (they
		// remain reference edges mined below).
		for k, v := range cfnExtractScalarProperties(r.body) {
			if _, exists := props.Lookup(k); !exists {
				props.Set(k, v)
			}
		}
		emitEntity(eid, kind, "cfn_resource", r.logicalID, r.line, props.Snapshot())
		knownIDs[r.logicalID] = eid
	}

	// --- Dependency edges between resources (and to Parameters). ------------
	for _, r := range resources {
		if r.logicalID == "" || r.typ == "" {
			continue
		}
		from := resourceRef(r.logicalID)
		fromID := cfnResourceKindFromID(cfnResourceKind(r.typ), from)
		for target, edgeKind := range cfnCollectRefs(r.body) {
			if target == r.logicalID {
				continue // self-ref (e.g. ${AWS::Region} already filtered)
			}
			toEID, ok := knownIDs[target]
			if !ok {
				continue // unresolved ref (pseudo-param or external) — skip
			}
			// Determine the to-side Kind for the ID prefix.
			toKind := cfnSchemaEntityKind
			for _, rr := range resources {
				if rr.logicalID == target {
					toKind = cfnResourceKind(rr.typ)
					break
				}
			}
			emitEdge(fromID, cfnResourceKindFromID(toKind, toEID), edgeKind,
				types.Props{{K: "iac_tool", V: "cloudformation"}, {K: "ref_kind", V: edgeKind}})
		}

		// Cross-stack imports referenced by this resource.
		for _, exp := range cfnCollectImports(r.body) {
			expID := "cfn-export:" + exp
			emitEntity(expID, cfnConfigEntityKind, "cfn_export", exp, r.line,
				map[string]string{"kind": "iac", "iac_tool": "cloudformation", "export_name": exp})
			emitEdge(fromID, fmt.Sprintf("%s:%s", cfnConfigEntityKind, expID),
				cfnDependsOnEdgeKind,
				types.Props{{K: "cross_stack", V: "true"}, {K: "iac_tool", V: "cloudformation"}})
		}

		// Nested stacks: AWS::CloudFormation::Stack TemplateURL → IMPORTS.
		if r.typ == "AWS::CloudFormation::Stack" {
			if url := cfnExtractTemplateURL(r.body); url != "" {
				emitEdge(fromID, "ext:cfn-stack:"+url, cfnImportsEdgeKind,
					types.Props{{K: "iac_tool", V: "cloudformation"}, {K: "nested_stack", V: "true"}})
			}
		}

		// SAM function event sources.
		if r.typ == "AWS::Serverless::Function" {
			cfnApplySAMFunction(r, fromID, knownIDs, resourceRef, emitEntity, emitEdge)
		}

		// Standalone AWS::Lambda::EventSourceMapping — #5801 Path B. SAM's
		// inline Events: block (cfnApplySAMFunction above) already models the
		// source→handler trigger for AWS::Serverless::Function; this handles
		// the decoupled, non-SAM shape where the mapping is its own resource.
		if r.typ == "AWS::Lambda::EventSourceMapping" {
			cfnApplyEventSourceMapping(r, resources, knownIDs, emitEdge)
		}
	}

	// --- Outputs.Export → producing-side export node. -----------------------
	outputsBody := cfnSectionBody(src, "Outputs")
	for _, name := range cfnCollectExportNames(outputsBody) {
		expID := "cfn-export:" + name
		emitEntity(expID, cfnConfigEntityKind, "cfn_export", name,
			cfnKeyLine(src, "Outputs", name),
			map[string]string{"kind": "iac", "iac_tool": "cloudformation", "export_name": name, "side": "producer"})
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// cfnResourceKindFromID builds the `<Kind>:<id>` edge endpoint string.
func cfnResourceKindFromID(kind, id string) string {
	return fmt.Sprintf("%s:%s", kind, id)
}

// ---------------------------------------------------------------------------
// SAM function event sources
// ---------------------------------------------------------------------------

var (
	// FunctionName: foo  (used to join the serverless aws-lambda synthetic).
	cfnSAMFunctionNameRe = regexp.MustCompile(`FunctionName:\s*["']?([\w-]+)["']?`)
	// An Events: entry has Type: Api / HttpApi / SQS / SNS / Schedule / ...
	cfnSAMEventTypeRe = regexp.MustCompile(`Type:\s*["']?(Api|HttpApi|SQS|SNS|Schedule|ScheduleV2|DynamoDB|Kinesis|S3|SnsTopic)["']?`)
	cfnSAMApiPathRe   = regexp.MustCompile(`Path:\s*["']?([^\s"']+)["']?`)
	cfnSAMApiMethodRe = regexp.MustCompile(`Method:\s*["']?(\w+)["']?`)
	cfnSAMScheduleRe  = regexp.MustCompile(`Schedule:\s*["']?([^\s"'][^"'\n]*)["']?`)
)

// cfnApplySAMFunction handles an AWS::Serverless::Function: it joins the
// serverless_edges.go aws-lambda:<name> synthetic and emits trigger edges from
// its Events:.
func cfnApplySAMFunction(
	r cfnResource,
	fromID string,
	knownIDs map[string]string,
	resourceRef func(string) string,
	emitEntity func(id, kind, subtype, name string, line int, props map[string]string),
	emitEdge func(fromID, toID, kind string, props types.Props),
) {
	// Join the serverless synthetic: logical name → aws-lambda:<name>. Prefer
	// an explicit FunctionName, else the logical id.
	fnName := r.logicalID
	if m := cfnSAMFunctionNameRe.FindStringSubmatch(r.body); m != nil {
		fnName = m[1]
	}
	lambdaID := "aws-lambda:" + fnName
	// Append-only join: the serverless pass emits the same SCOPE.ServerlessFunction
	// ID for the handler code; emit a HANDLES-style USES edge so the CFN resource
	// links to its runtime function entity (collapses cross-repo on shared ID).
	emitEntity(lambdaID, "SCOPE.ServerlessFunction", "sam_function", fnName, r.line,
		map[string]string{"function_name": fnName, "iac_tool": "sam", "provider": "aws-lambda"})
	emitEdge(fromID, "SCOPE.ServerlessFunction:"+lambdaID, cfnUsesEdgeKind,
		types.Props{{K: "iac_tool", V: "sam"}, {K: "join", V: "serverless_synthetic"}})

	// Walk Events: looking for trigger types.
	eventsBody := cfnIndentedSubBody(r.body, "Events")
	if eventsBody == "" {
		return
	}
	for _, ev := range cfnSplitEvents(eventsBody) {
		tm := cfnSAMEventTypeRe.FindStringSubmatch(ev)
		if tm == nil {
			continue
		}
		switch tm[1] {
		case "Api", "HttpApi":
			path := ""
			method := "ANY"
			if pm := cfnSAMApiPathRe.FindStringSubmatch(ev); pm != nil {
				path = pm[1]
			}
			if mm := cfnSAMApiMethodRe.FindStringSubmatch(ev); mm != nil {
				method = strings.ToUpper(mm[1])
			}
			if path == "" {
				continue
			}
			epID := "http_endpoint_definition:" + method + " " + path
			emitEntity(epID, "http_endpoint_definition", "sam_api", method+" "+path, r.line,
				map[string]string{"http_method": method, "iac_tool": "sam", "route_path": path})
			emitEdge("http_endpoint_definition:"+epID, fromID, cfnRoutesToEdgeKind,
				types.Props{{K: "iac_tool", V: "sam"}, {K: "trigger", V: tm[1]}})
		case "SQS", "SNS", "SnsTopic", "DynamoDB", "Kinesis", "S3":
			// Resolve the source resource via a Ref/GetAtt inside the event.
			for target := range cfnCollectRefs(ev) {
				if toEID, ok := knownIDs[target]; ok {
					emitEdge(fromID, "SCOPE.Queue:"+toEID, cfnSubscribesToKind,
						types.Props{{K: "iac_tool", V: "sam"}, {K: "trigger", V: tm[1]}})
					// Fall back: also link via generic kind if not a queue.
				}
			}
		case "Schedule", "ScheduleV2":
			expr := ""
			if sm := cfnSAMScheduleRe.FindStringSubmatch(ev); sm != nil {
				expr = strings.TrimSpace(sm[1])
			}
			jobID := resourceRef(r.logicalID + "_schedule")
			emitEntity(jobID, cfnScheduledJobKind, "sam_schedule", r.logicalID+" schedule", r.line,
				map[string]string{"iac_tool": "sam", "schedule": expr})
			emitEdge(fmt.Sprintf("%s:%s", cfnScheduledJobKind, jobID), fromID,
				cfnTriggersEdgeKind, types.Props{{K: "iac_tool", V: "sam"}, {K: "schedule", V: expr}})
		}
	}
}

// ---------------------------------------------------------------------------
// Standalone AWS::Lambda::EventSourceMapping (#5801 Path B)
// ---------------------------------------------------------------------------
//
// SAM's inline `Events:` block on an AWS::Serverless::Function (Path A, see
// cfnApplySAMFunction above) already emits a handler --SUBSCRIBES_TO--> queue
// trigger edge. A standalone `AWS::Lambda::EventSourceMapping` resource
// decouples the same relationship — `EventSourceArn` names the queue/stream
// and `FunctionName` names the handler — and, unhandled, produced only two
// generic USES/DEPENDS_ON edges into the mapping resource itself (no
// source→handler edge at all). This mirrors Path A's edge shape exactly
// (same fromID=handler / toID=source / SUBSCRIBES_TO convention) so
// ApplyAsyncTriggerEdges (#5686) synthesises the SAME inbound DELIVERS_TO
// (source → handler) for both paths, and both render identically in
// topology/impact/trace.
//
// Conservative / same-template only: EventSourceArn and FunctionName are
// resolved via !Ref / !GetAtt X.<Attr> to a logical id declared in THIS
// template. A logical id that isn't declared here (cross-stack, imported
// ARN, etc.) is skipped — no guessed edge is emitted.
var (
	// EventSourceArn: !GetAtt OrdersStream.Arn | !GetAtt Table.StreamArn
	cfnESMEventSourceArnGetAttRe = regexp.MustCompile(`EventSourceArn:\s*!GetAtt\s+([A-Za-z0-9]+)\.[\w.]+`)
	// EventSourceArn: !Ref SomeQueue  (Ref of a queue resource yields its ARN
	// for some resource types in practice; treated the same as GetAtt here).
	cfnESMEventSourceArnRefRe = regexp.MustCompile(`EventSourceArn:\s*!Ref\s+([A-Za-z0-9]+)`)
	// FunctionName: !Ref StreamHandlerFn
	cfnESMFunctionNameRefRe = regexp.MustCompile(`FunctionName:\s*!Ref\s+([A-Za-z0-9]+)`)
)

// cfnApplyEventSourceMapping resolves a standalone AWS::Lambda::EventSourceMapping
// resource's EventSourceArn and FunctionName intrinsics to logical ids declared
// in the same template and, when both resolve, emits the handler-->source
// SUBSCRIBES_TO edge matching the SAM inline-Events convention.
func cfnApplyEventSourceMapping(
	r cfnResource,
	resources []cfnResource,
	knownIDs map[string]string,
	emitEdge func(fromID, toID, kind string, props types.Props),
) {
	sourceLogical := ""
	if m := cfnESMEventSourceArnGetAttRe.FindStringSubmatch(r.body); m != nil {
		sourceLogical = m[1]
	} else if m := cfnESMEventSourceArnRefRe.FindStringSubmatch(r.body); m != nil {
		sourceLogical = m[1]
	}
	handlerLogical := ""
	if m := cfnESMFunctionNameRefRe.FindStringSubmatch(r.body); m != nil {
		handlerLogical = m[1]
	}
	if sourceLogical == "" || handlerLogical == "" {
		return
	}
	sourceEID, ok := knownIDs[sourceLogical]
	if !ok {
		return // logical id not declared in this template — honest skip, no guess.
	}
	handlerEID, ok := knownIDs[handlerLogical]
	if !ok {
		return
	}
	// Default to cfnSchemaEntityKind (SCOPE.Schema): the only way a knownIDs
	// hit is absent from `resources` is that it's a CFN Parameter, which the
	// Parameters emission (and the generic Ref/GetAtt loop) records with
	// cfnSchemaEntityKind — NOT cfnConfigEntityKind (reserved for cfn_export).
	// Using SCOPE.Config here would produce a dangling edge endpoint under a
	// Kind no entity was emitted with (#5801 Bug 1).
	sourceKind := cfnSchemaEntityKind
	handlerKind := cfnSchemaEntityKind
	for _, rr := range resources {
		switch rr.logicalID {
		case sourceLogical:
			sourceKind = cfnResourceKind(rr.typ)
		case handlerLogical:
			handlerKind = cfnResourceKind(rr.typ)
		}
	}
	handlerFromID := cfnResourceKindFromID(handlerKind, handlerEID)
	sourceToID := cfnResourceKindFromID(sourceKind, sourceEID)
	emitEdge(handlerFromID, sourceToID, cfnSubscribesToKind,
		types.Props{
			{K: "iac_tool", V: "cloudformation"},
			{K: "resource_type", V: r.typ},
			{K: "trigger", V: "EventSourceMapping"},
		})
}

// ---------------------------------------------------------------------------
// YAML sub-section helpers
// ---------------------------------------------------------------------------

// cfnTopLevelKeys returns the immediate child keys of an indented section body
// (keys at the minimum indentation level present).
func cfnTopLevelKeys(body string) []string {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	minIndent := -1
	for _, ln := range strings.Split(body, "\n") {
		if strings.TrimSpace(ln) == "" || strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		ind := len(ln) - len(strings.TrimLeft(ln, " "))
		if minIndent < 0 || ind < minIndent {
			minIndent = ind
		}
	}
	if minIndent < 0 {
		return nil
	}
	var keys []string
	seen := map[string]bool{}
	keyRe := regexp.MustCompile(`^(\s*)([A-Za-z0-9]+):`)
	for _, ln := range strings.Split(body, "\n") {
		m := keyRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		if len(m[1]) != minIndent {
			continue
		}
		if !seen[m[2]] {
			seen[m[2]] = true
			keys = append(keys, m[2])
		}
	}
	return keys
}

// cfnKeyLine returns the 1-based source line of `<section>.<key>` (best effort).
func cfnKeyLine(src, section, key string) int {
	body := cfnSectionBody(src, section)
	if body == "" {
		return 1
	}
	off := strings.Index(src, body)
	if off < 0 {
		return 1
	}
	re := regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(key) + `:`)
	if loc := re.FindStringIndex(body); loc != nil {
		return cfnLineOf(src, off+loc[0])
	}
	return cfnLineOf(src, off)
}

// cfnIndentedSubBody returns the indented body of a `Key:` mapping found
// anywhere within parent (used for Events: inside a SAM function body).
func cfnIndentedSubBody(parent, key string) string {
	lines := strings.Split(parent, "\n")
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t != key+":" && !strings.HasPrefix(t, key+":") {
			continue
		}
		keyIndent := len(ln) - len(strings.TrimLeft(ln, " "))
		var out []string
		for _, sub := range lines[i+1:] {
			if strings.TrimSpace(sub) == "" {
				out = append(out, sub)
				continue
			}
			ind := len(sub) - len(strings.TrimLeft(sub, " "))
			if ind <= keyIndent {
				break
			}
			out = append(out, sub)
		}
		return strings.Join(out, "\n")
	}
	return ""
}

// cfnSplitEvents splits an Events: body into per-event chunks (each child key
// starts a new event). Returns the raw text of each event so its Type:/Ref can
// be scanned independently.
func cfnSplitEvents(eventsBody string) []string {
	lines := strings.Split(eventsBody, "\n")
	// Determine child indentation = minimum non-empty indentation.
	minIndent := -1
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		ind := len(ln) - len(strings.TrimLeft(ln, " "))
		if minIndent < 0 || ind < minIndent {
			minIndent = ind
		}
	}
	if minIndent < 0 {
		return nil
	}
	var chunks []string
	var cur []string
	for _, ln := range lines {
		ind := len(ln) - len(strings.TrimLeft(ln, " "))
		isHeader := strings.TrimSpace(ln) != "" && ind == minIndent &&
			strings.Contains(ln, ":")
		if isHeader && len(cur) > 0 {
			chunks = append(chunks, strings.Join(cur, "\n"))
			cur = nil
		}
		cur = append(cur, ln)
	}
	if len(cur) > 0 {
		chunks = append(chunks, strings.Join(cur, "\n"))
	}
	return chunks
}

// cfnExtractTemplateURL pulls the TemplateURL literal from a nested-stack body.
var cfnTemplateURLRe = regexp.MustCompile(`TemplateURL:\s*["']?([^\s"'{}]+)["']?`)

func cfnExtractTemplateURL(body string) string {
	if m := cfnTemplateURLRe.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return ""
}

// cfnCollectExportNames scans an Outputs: body for `Export: { Name: ... }`
// entries (both `!Sub`/literal Name forms collapse to the literal token).
var cfnExportNameRe = regexp.MustCompile(`Name:\s*["']?([\w:-]+)["']?`)

func cfnCollectExportNames(outputsBody string) []string {
	if outputsBody == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	// Only consider Name: lines that appear under an Export: key.
	lines := strings.Split(outputsBody, "\n")
	inExport := false
	exportIndent := -1
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		ind := len(ln) - len(strings.TrimLeft(ln, " "))
		if t == "Export:" || strings.HasPrefix(t, "Export:") {
			inExport = true
			exportIndent = ind
			continue
		}
		if inExport {
			if t != "" && ind <= exportIndent {
				inExport = false
			} else if m := cfnExportNameRe.FindStringSubmatch(ln); m != nil {
				if !seen[m[1]] {
					seen[m[1]] = true
					out = append(out, m[1])
				}
				inExport = false
			}
		}
	}
	return out
}
