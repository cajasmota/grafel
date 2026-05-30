// Package yaml implements the tree-sitter-based YAML AST extractor for archigraph.
//
// It detects the YAML "flavor" from content heuristics (GitHub Actions, GitLab CI,
// Docker Compose, Kubernetes manifest, Ansible playbook, or generic YAML) and applies
// the appropriate entity extraction strategy.
//
// Entity mapping by flavor:
//
//	GitHub Actions:
//	  top-level name:        → Kind="SCOPE.Operation",  Subtype="workflow"
//	  jobs.<key>:            → Kind="SCOPE.Operation",  Subtype="job"
//	  steps[].name:          → Kind="SCOPE.Operation",  Subtype="step"
//	  steps[].uses:          → Kind="SCOPE.Component",  Subtype="action"
//
//	GitLab CI:
//	  stages[] value:        → Kind="SCOPE.Component",  Subtype="stage"
//	  top-level job key:     → Kind="SCOPE.Operation",  Subtype="job"
//	  script[] entry:        → Kind="SCOPE.Operation",  Subtype="script_step"
//
//	Docker Compose:
//	  services.<key>:        → Kind="SCOPE.Component",  Subtype="service"
//	  ports[] binding:       → Kind="SCOPE.Pattern",    Subtype="port"
//	  volumes.<key>:         → Kind="SCOPE.Schema",     Subtype="volume"
//
//	Kubernetes:
//	  metadata.name + kind:  → Kind="SCOPE.Component",  Subtype="k8s_resource"
//	  containers[].name:     → Kind="SCOPE.Operation",  Subtype="container"
//
//	Ansible:
//	  tasks[].name:          → Kind="SCOPE.Operation",  Subtype="task"
//	  roles[] value:         → Kind="SCOPE.Component",  Subtype="role"
//	  handlers[].name:       → Kind="SCOPE.Operation",  Subtype="handler"
//
//	Generic YAML:
//	  top-level key (depth 1): → Kind="SCOPE.Schema",  Subtype="key"
//
// OTel span: "indexer.extract.yaml" with attributes language, file_line_count,
// entity_count, yaml_flavor.
//
// Registers itself via init() and is imported by registry_gen.go.
package yaml

import (
	"bytes"
	"context"
	"log"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("yaml", &Extractor{})
}

// Extractor implements extractor.Extractor for YAML files.
type Extractor struct{}

// Language returns the canonical language key.
func (e *Extractor) Language() string { return "yaml" }

// Extract implements extractor.Extractor.
// Returns partial results on error — never aborts on a single node failure.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) (entities []types.EntityRecord, retErr error) {
	tracer := otel.Tracer("extractor.yaml")
	ctx, span := tracer.Start(ctx, "indexer.extract.yaml",
		trace.WithAttributes(attribute.String("language", "yaml")),
	)
	defer span.End()

	if len(file.Content) == 0 {
		span.SetAttributes(
			attribute.Int("file_line_count", 0),
			attribute.Int("entity_count", 0),
			attribute.String("yaml_flavor", "empty"),
		)
		return nil, nil
	}

	tree := file.Tree
	if tree == nil {
		parser := sitter.NewParser()
		parser.SetLanguage(yamlGrammar())
		var err error
		tree, err = parser.ParseCtx(ctx, nil, file.Content)
		if err != nil {
			return nil, err
		}
	}

	root := tree.RootNode()
	if root == nil {
		span.SetAttributes(
			attribute.Int("file_line_count", 0),
			attribute.Int("entity_count", 0),
			attribute.String("yaml_flavor", "nil_root"),
		)
		return nil, nil
	}

	lineCount := bytes.Count(file.Content, []byte("\n")) + 1

	flavor := detectFlavor(file.Content, file.Path)
	span.SetAttributes(attribute.String("yaml_flavor", flavor))

	entities = extractByFlavor(flavor, root, file)

	span.SetAttributes(
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", len(entities)),
	)

	return entities, nil
}

// ---------------------------------------------------------------------------
// Flavor detection
// ---------------------------------------------------------------------------

const (
	flavorGitHubActions = "github_actions"
	flavorGitLabCI      = "gitlab_ci"
	flavorDockerCompose = "docker_compose"
	flavorKubernetes    = "kubernetes"
	flavorAnsible       = "ansible"
	flavorKustomize     = "kustomize"
	flavorGeneric       = "generic"
)

// detectFlavor classifies the YAML content into one of the known flavors.
// Checked in order: GitHub Actions, GitLab CI, Docker Compose, Kubernetes,
// Ansible, Generic. Never panics — returns flavorGeneric on any uncertainty.
func detectFlavor(src []byte, path string) (flavor string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("yaml: flavor detection panic: %v — falling back to generic", r)
			flavor = flavorGeneric
		}
	}()

	content := string(src)

	// GitHub Actions: top-level "on:" or "jobs:" key
	if containsTopLevelKey(content, "on") || containsTopLevelKey(content, "jobs") {
		return flavorGitHubActions
	}

	// GitLab CI: top-level "stages:" or path contains .gitlab-ci
	if containsTopLevelKey(content, "stages") || strings.Contains(path, ".gitlab-ci") {
		// Must also have "include:" or "stages:" — already have stages check
		return flavorGitLabCI
	}

	// Docker Compose: top-level "services:" + "image:" somewhere
	if containsTopLevelKey(content, "services") && strings.Contains(content, "image:") {
		return flavorDockerCompose
	}

	// Helm: a Chart.yaml, values.yaml, _helpers.tpl, or a templates/*.yaml
	// carrying Go-template directives. Must be checked BEFORE Kubernetes — a
	// templated manifest carries `apiVersion:`/`kind:` and would otherwise be
	// mis-classified as a plain (unparseable) K8s manifest. The Helm path runs a
	// tolerant pre-strip so the underlying K8s resource is still recovered.
	if hf := helmFlavor(content, path); hf != "" {
		return hf
	}

	// Kustomize: a kustomization.yaml. Must be checked BEFORE Kubernetes because
	// a kustomization carries `kind: Kustomization` + `apiVersion: kustomize...`
	// which would otherwise match the generic K8s manifest branch below.
	// Detect signals (any one suffices):
	//   - filename is kustomization.yaml / kustomization.yml / Kustomization
	//   - apiVersion line mentions kustomize.config.k8s.io
	//   - a top-level `kind: Kustomization`
	//   - a top-level `resources:` key alongside a kustomize transform/generator
	if isKustomizeContent(content, path) {
		return flavorKustomize
	}

	// Kubernetes: "apiVersion:" and "kind:" at top level
	if containsTopLevelKey(content, "apiVersion") && containsTopLevelKey(content, "kind") {
		return flavorKubernetes
	}

	// Ansible: flat tasks/handlers at top level OR list-of-plays format (- hosts:)
	if isAnsibleContent(content) {
		return flavorAnsible
	}

	return flavorGeneric
}

// isAnsibleContent returns true when content looks like an Ansible playbook
// or role task file. Covers two shapes:
//  1. Flat: top-level tasks:/handlers: key with name: entries.
//  2. List-of-plays: top-level list items that contain hosts: key.
func isAnsibleContent(content string) bool {
	// Shape 1: flat tasks/handlers at top level
	if (containsTopLevelKey(content, "tasks") || containsTopLevelKey(content, "handlers")) &&
		strings.Contains(content, "name:") {
		return true
	}
	// Shape 2: list-of-plays — look for "- name:" or "- hosts:" at col 0 or after "---"
	// A play list starts with a dash at column 0 and has hosts: somewhere nearby.
	if strings.Contains(content, "hosts:") && containsListItemWithKey(content, "hosts") {
		return true
	}
	return false
}

// isKustomizeContent reports whether the YAML is a Kustomize kustomization.yaml.
// Any one signal suffices (filename, apiVersion, kind, or a top-level resources/
// bases/components list combined with a kustomize-specific transform/generator).
func isKustomizeContent(content, path string) bool {
	// Filename signal.
	base := path
	if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
		base = base[idx+1:]
	}
	switch base {
	case "kustomization.yaml", "kustomization.yml", "Kustomization":
		return true
	}
	// apiVersion signal — kustomize.config.k8s.io/v1beta1 (or v1).
	if strings.Contains(content, "kustomize.config.k8s.io") {
		return true
	}
	// kind: Kustomization at top level.
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		if (trimmed == "kind: Kustomization" || strings.HasPrefix(trimmed, "kind: Kustomization ")) &&
			len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			return true
		}
	}
	// Structural signal: a top-level resources/bases/components list AND at least
	// one kustomize-specific key. `resources:` alone also appears in some CRDs,
	// so require a corroborating kustomize key to avoid false positives.
	hasResourceList := containsTopLevelKey(content, "resources") ||
		containsTopLevelKey(content, "bases") ||
		containsTopLevelKey(content, "components")
	if hasResourceList {
		for _, k := range []string{
			"patchesStrategicMerge", "patchesJson6902", "patches",
			"configMapGenerator", "secretGenerator", "namePrefix",
			"nameSuffix", "commonLabels",
		} {
			if containsTopLevelKey(content, k) {
				return true
			}
		}
	}
	return false
}

// containsListItemWithKey returns true when the content has a top-level YAML
// list item (line starting with "- ") that contains the given key as an
// indented mapping entry.
func containsListItemWithKey(content, key string) bool {
	marker := "  " + key + ":"
	lines := strings.Split(content, "\n")
	inListItem := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		// Top-level list item starts with "- " or "---"
		if trimmed == "---" {
			inListItem = false
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			inListItem = true
		}
		if inListItem && (strings.HasPrefix(trimmed, marker) || strings.HasPrefix(trimmed, "  "+key+":")) {
			return true
		}
	}
	return false
}

// containsTopLevelKey reports whether content has a line starting with key: (no leading spaces).
func containsTopLevelKey(content, key string) bool {
	marker := key + ":"
	// Check line-by-line for top-level (no indent) key
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == marker || strings.HasPrefix(trimmed, marker+" ") || strings.HasPrefix(trimmed, marker+"\t") {
			// Only top-level: no leading spaces
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

func extractByFlavor(flavor string, root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("yaml: extraction panic for flavor %s: %v", flavor, r)
		}
	}()

	var entities []types.EntityRecord
	switch flavor {
	case flavorGitHubActions:
		entities = extractGitHubActions(root, file)
	case flavorGitLabCI:
		entities = extractGitLabCI(root, file)
	case flavorDockerCompose:
		entities = extractDockerCompose(root, file)
	case flavorKubernetes:
		entities = extractKubernetes(root, file)
	case flavorAnsible:
		entities = extractAnsible(root, file)
	case flavorKustomize:
		entities = extractKustomize(root, file)
	case flavorHelmChart, flavorHelmValues, flavorHelmTemplate, flavorHelmHelpers:
		entities = extractHelm(flavor, root, file)
	default:
		entities = extractGeneric(root, file)
	}
	// Issue #474 chain-fix — file-rooted CONTAINS edges across every YAML
	// flavor use `file.Path` as FromID (see relationships.go). Pre-fix the
	// file had no corresponding entity, so the resolver could not bind the
	// FromID and every such edge landed in bug-extractor (57 on
	// argocd-example-apps alone). Mirror the markdown extractor and emit
	// one SCOPE.Document per YAML file with QualifiedName=file.Path so
	// the byQualifiedName index resolves file.Path → the Document entity
	// ID. The Document is the canonical parent entity for the file, per
	// ADR-0009.
	if len(entities) > 0 {
		docEntity := buildYAMLDocument(flavor, root, file)
		// Prepend so the Document appears first in the file's entity list
		// (matches the markdown extractor's ordering convention).
		entities = append([]types.EntityRecord{docEntity}, entities...)
	}
	// Issue #386 / #90: stamp Properties["language"]="yaml" on every embedded
	// relationship so the resolver dispatches the YAML dynamic-pattern catalog.
	extractor.TagRelationshipsLanguage(entities, "yaml")
	extractor.TagEntitiesLanguage(entities, "yaml")
	return entities
}

// buildYAMLDocument constructs the SCOPE.Document entity for a YAML file.
// QualifiedName equals file.Path so that file-rooted CONTAINS edges (whose
// FromID is set to file.Path by the flavor extractors) resolve via the
// resolver's byQualifiedName index. The Subtype carries the detected YAML
// flavor so downstream tooling can distinguish e.g. a Kubernetes manifest
// Document from an Ansible playbook Document.
//
// Issue #474 chain-fix.
func buildYAMLDocument(flavor string, root *sitter.Node, file extractor.FileInput) types.EntityRecord {
	endLine := bytes.Count(file.Content, []byte("\n")) + 1
	if root != nil {
		endLine = int(root.EndPoint().Row) + 1
	}
	name := file.Path
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		name = name[idx+1:]
	}
	return types.EntityRecord{
		Kind:          "SCOPE.Document",
		Name:          name,
		Subtype:       flavor,
		QualifiedName: file.Path,
		SourceFile:    file.Path,
		Language:      "yaml",
		StartLine:     1,
		EndLine:       endLine,
		QualityScore:  0.7,
	}
}

// ---------------------------------------------------------------------------
// Tree-sitter YAML node helpers
// ---------------------------------------------------------------------------

// nodeText returns the text of a node from source bytes.
func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

// itoa is a tiny wrapper to keep ref-building one-liners readable.
func itoa(i int) string { return strconv.Itoa(i) }

// entity builds a typed EntityRecord with required fields set.
func entity(kind, name, subtype, qualifiedName, sourcefile, language string, startLine, endLine int) types.EntityRecord {
	return types.EntityRecord{
		Kind:          kind,
		Name:          name,
		Subtype:       subtype,
		QualifiedName: qualifiedName,
		SourceFile:    sourcefile,
		Language:      language,
		StartLine:     startLine,
		EndLine:       endLine,
		QualityScore:  0.7,
	}
}

// topLevelMappings returns the direct block_mapping_pair children of the document root.
// YAML tree-sitter structure: stream -> document -> block_node -> block_mapping -> block_mapping_pair
// Only the FIRST document in the stream is returned. For multi-document files use allDocuments.
func topLevelMappings(root *sitter.Node) []*sitter.Node {
	var pairs []*sitter.Node
	if root == nil {
		return pairs
	}
	// Find the first document node
	doc := findFirstChild(root, "document")
	if doc == nil {
		return pairs
	}
	return documentMappings(doc)
}

// documentMappings returns block_mapping_pair children from a single document node.
func documentMappings(doc *sitter.Node) []*sitter.Node {
	var pairs []*sitter.Node
	if doc == nil {
		return pairs
	}
	blockNode := findFirstChild(doc, "block_node")
	if blockNode == nil {
		return pairs
	}
	blockMapping := findFirstChild(blockNode, "block_mapping")
	if blockMapping == nil {
		return pairs
	}
	for i := range blockMapping.ChildCount() {
		child := blockMapping.Child(int(i))
		if child != nil && child.Type() == "block_mapping_pair" {
			pairs = append(pairs, child)
		}
	}
	return pairs
}

// allDocuments returns all document nodes in the stream (multi-document YAML support).
func allDocuments(root *sitter.Node) []*sitter.Node {
	var docs []*sitter.Node
	if root == nil {
		return docs
	}
	for i := range root.ChildCount() {
		child := root.Child(int(i))
		if child != nil && child.Type() == "document" {
			docs = append(docs, child)
		}
	}
	return docs
}

// findFirstChild returns the first direct child of node with the given type.
func findFirstChild(node *sitter.Node, nodeType string) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := range node.ChildCount() {
		child := node.Child(int(i))
		if child != nil && child.Type() == nodeType {
			return child
		}
	}
	return nil
}

// pairValueNode returns the block_node or flow_node value child of a block_mapping_pair.
// It skips the key node (first flow_node/scalar) and the colon separator, returning
// the second flow_node or the block_node that holds the value.
func pairValueNode(pair *sitter.Node) *sitter.Node {
	if pair == nil {
		return nil
	}
	seenColon := false
	for i := range pair.ChildCount() {
		child := pair.Child(int(i))
		if child == nil {
			continue
		}
		t := child.Type()
		if t == ":" {
			seenColon = true
			continue
		}
		if seenColon && (t == "block_node" || t == "flow_node") {
			return child
		}
	}
	return nil
}

// getBlockMapping returns the block_mapping inside a block_node (or nil).
func getBlockMapping(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	if node.Type() == "block_mapping" {
		return node
	}
	return findFirstChild(node, "block_mapping")
}

// getMappingPairsForKey finds the block_mapping_pair with a given key in a list of pairs,
// then returns child pairs of its value mapping.
func getMappingPairsForKey(pairs []*sitter.Node, key string, src []byte) []*sitter.Node {
	for _, p := range pairs {
		k := pairKeyText(p, src)
		if k == key {
			val := pairValueNode(p)
			bm := getBlockMapping(val)
			if bm == nil {
				return nil
			}
			var children []*sitter.Node
			for i := range bm.ChildCount() {
				child := bm.Child(int(i))
				if child != nil && child.Type() == "block_mapping_pair" {
					children = append(children, child)
				}
			}
			return children
		}
	}
	return nil
}

// pairKeyText extracts clean key text from a block_mapping_pair, handling
// the different ways tree-sitter YAML represents keys.
func pairKeyText(pair *sitter.Node, src []byte) string {
	if pair == nil {
		return ""
	}
	// The key is the first non-colon child
	for i := range pair.ChildCount() {
		child := pair.Child(int(i))
		if child == nil {
			continue
		}
		if child.Type() == ":" {
			break
		}
		t := child.Type()
		switch t {
		case "flow_node", "plain_scalar", "double_quote_scalar", "single_quote_scalar":
			return strings.TrimSpace(nodeText(child, src))
		}
	}
	// Fallback: use the raw text of the pair split at ':'
	raw := nodeText(pair, src)
	if idx := strings.Index(raw, ":"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return ""
}

// getSequenceItems returns string values from a block_sequence under a block_node.
func getSequenceItems(valueNode *sitter.Node, src []byte) []string {
	if valueNode == nil {
		return nil
	}
	seq := findFirstChild(valueNode, "block_sequence")
	if seq == nil {
		return nil
	}
	var items []string
	for i := range seq.ChildCount() {
		item := seq.Child(int(i))
		if item == nil || item.Type() != "block_sequence_item" {
			continue
		}
		// The value inside block_sequence_item is block_node or flow_node
		for j := range item.ChildCount() {
			child := item.Child(int(j))
			if child == nil || child.Type() == "-" {
				continue
			}
			text := strings.TrimSpace(nodeText(child, src))
			if text != "" {
				items = append(items, text)
			}
		}
	}
	return items
}

// getSequenceItemMappings returns block_mapping_pair slices for each sequence item.
func getSequenceItemMappings(valueNode *sitter.Node, src []byte) [][]*sitter.Node {
	if valueNode == nil {
		return nil
	}
	seq := findFirstChild(valueNode, "block_sequence")
	if seq == nil {
		return nil
	}
	var result [][]*sitter.Node
	for i := range seq.ChildCount() {
		item := seq.Child(int(i))
		if item == nil || item.Type() != "block_sequence_item" {
			continue
		}
		for j := range item.ChildCount() {
			child := item.Child(int(j))
			if child == nil || child.Type() == "-" {
				continue
			}
			bm := getBlockMapping(child)
			if bm != nil {
				var pairs []*sitter.Node
				for k := range bm.ChildCount() {
					cp := bm.Child(int(k))
					if cp != nil && cp.Type() == "block_mapping_pair" {
						pairs = append(pairs, cp)
					}
				}
				result = append(result, pairs)
			}
		}
	}
	return result
}

// getPairValueText returns the scalar text value of a block_mapping_pair.
func getPairValueText(pair *sitter.Node, src []byte) string {
	if pair == nil {
		return ""
	}
	val := pairValueNode(pair)
	if val == nil {
		return ""
	}
	return strings.TrimSpace(nodeText(val, src))
}

// findPairValueText finds a key in a slice of pairs and returns its value text.
func findPairValueText(pairs []*sitter.Node, key string, src []byte) string {
	for _, p := range pairs {
		if pairKeyText(p, src) == key {
			return getPairValueText(p, src)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// GitHub Actions extractor
// ---------------------------------------------------------------------------

func extractGitHubActions(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	src := file.Content
	pairs := topLevelMappings(root)
	var entities []types.EntityRecord

	// File-scoped ref prefix avoids QualifiedName collisions across workflows.
	// The same `Checkout` step name appears in hundreds of workflows; without
	// scoping the resolver index blanks the QualifiedName entry on collision
	// and every CONTAINS edge to that step lands in bug-extractor (issue #44).
	refPrefix := "github_actions/" + file.Path + "#"

	// Resolve the workflow ref once (top-level `name:`); fall back to file.Path
	// when no name is present.
	workflowRef := ""
	for _, p := range pairs {
		if pairKeyText(p, src) == "name" {
			if v := getPairValueText(p, src); v != "" {
				workflowRef = refPrefix + "workflow/" + v
			}
			break
		}
	}

	// Track unique `uses:` references so we emit one IMPORTS edge per action
	// from the workflow file.
	seenUses := map[string]bool{}

	for _, p := range pairs {
		key := pairKeyText(p, src)
		startLine := int(p.StartPoint().Row) + 1
		endLine := int(p.EndPoint().Row) + 1

		switch key {
		case "name":
			name := getPairValueText(p, src)
			if name != "" {
				entities = append(entities, entity(
					"SCOPE.Operation", name, "workflow",
					refPrefix+"workflow/"+name,
					file.Path, "yaml", startLine, endLine,
				))
			}

		case "jobs":
			jobPairs := getMappingPairsForKey(pairs, "jobs", src)
			for _, jp := range jobPairs {
				jobName := pairKeyText(jp, src)
				if jobName == "" {
					continue
				}
				jStart := int(jp.StartPoint().Row) + 1
				jEnd := int(jp.EndPoint().Row) + 1
				jobRef := refPrefix + "job/" + jobName
				jobEnt := entity(
					"SCOPE.Operation", jobName, "job",
					jobRef,
					file.Path, "yaml", jStart, jEnd,
				)
				// CONTAINS: workflow (or file) → job.
				parentRef := workflowRef
				if parentRef == "" {
					parentRef = file.Path
				}
				jobEnt.Relationships = append(jobEnt.Relationships,
					containsRel(parentRef, jobRef))
				entities = append(entities, jobEnt)

				// Extract steps from this job
				jobValNode := pairValueNode(jp)
				jobMapping := getBlockMapping(jobValNode)
				if jobMapping == nil {
					continue
				}
				var jobPairList []*sitter.Node
				for i := range jobMapping.ChildCount() {
					child := jobMapping.Child(int(i))
					if child != nil && child.Type() == "block_mapping_pair" {
						jobPairList = append(jobPairList, child)
					}
				}
				stepsNode := findValueNodeForKey(jobPairList, "steps", src)
				stepMappings := getSequenceItemMappings(stepsNode, src)
				for stepIdx, stepPairs := range stepMappings {
					// step name
					stepName := findPairValueText(stepPairs, "name", src)
					if stepName != "" {
						sStart, sEnd := pairsLineRange(stepPairs)
						// Include job + position so duplicate step names within
						// the same workflow still get unique refs.
						stepRef := refPrefix + "step/" + jobName + "/" + itoa(stepIdx) + "/" + stepName
						stepEnt := entity(
							"SCOPE.Operation", stepName, "step",
							stepRef,
							file.Path, "yaml", sStart, sEnd,
						)
						stepEnt.Relationships = append(stepEnt.Relationships,
							containsRel(jobRef, stepRef))
						entities = append(entities, stepEnt)
					}
					// uses action
					usesVal := findPairValueText(stepPairs, "uses", src)
					if usesVal != "" {
						sStart, sEnd := pairsLineRange(stepPairs)
						actionRef := refPrefix + "action/" + jobName + "/" + itoa(stepIdx) + "/" + usesVal
						actionEnt := entity(
							"SCOPE.Component", usesVal, "action",
							actionRef,
							file.Path, "yaml", sStart, sEnd,
						)
						// CONTAINS: job → action.
						actionEnt.Relationships = append(actionEnt.Relationships,
							containsRel(jobRef, actionRef))
						// IMPORTS: workflow file → unique action reference
						// (e.g. "actions/checkout@v4"). The `gha_action:` prefix
						// is consumed by external.synth (Refs #44) and lifted to
						// an `ext:gha:<org/name>` placeholder — actions live in
						// the GitHub Actions marketplace, never in the indexed
						// corpus. Attach to the workflow entity if we have one,
						// otherwise to this entity.
						if !seenUses[usesVal] {
							seenUses[usesVal] = true
							importRel := importsRel(file.Path, "gha_action:"+usesVal, "github_actions_uses")
							if workflowRef != "" {
								if wfIdx := findEntityIndex(entities, workflowRef); wfIdx >= 0 {
									entities[wfIdx].Relationships = append(entities[wfIdx].Relationships, importRel)
								} else {
									actionEnt.Relationships = append(actionEnt.Relationships, importRel)
								}
							} else {
								actionEnt.Relationships = append(actionEnt.Relationships, importRel)
							}
						}
						entities = append(entities, actionEnt)
					}
				}
			}
		}
	}

	return entities
}

// findValueNodeForKey finds a key in a slice of pairs and returns the value node.
func findValueNodeForKey(pairs []*sitter.Node, key string, src []byte) *sitter.Node {
	for _, p := range pairs {
		if pairKeyText(p, src) == key {
			return pairValueNode(p)
		}
	}
	return nil
}

// pairsLineRange returns the start/end lines covering a slice of pairs.
func pairsLineRange(pairs []*sitter.Node) (start, end int) {
	if len(pairs) == 0 {
		return 0, 0
	}
	start = int(pairs[0].StartPoint().Row) + 1
	end = int(pairs[len(pairs)-1].EndPoint().Row) + 1
	return start, end
}

// ---------------------------------------------------------------------------
// GitLab CI extractor
// ---------------------------------------------------------------------------

// gitlabReservedKeys are top-level keys that are NOT job definitions.
var gitlabReservedKeys = map[string]bool{
	"stages": true, "variables": true, "include": true, "workflow": true,
	"default": true, "image": true, "services": true, "before_script": true,
	"after_script": true, "cache": true,
}

func extractGitLabCI(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	src := file.Content
	pairs := topLevelMappings(root)
	var entities []types.EntityRecord

	for _, p := range pairs {
		key := pairKeyText(p, src)
		startLine := int(p.StartPoint().Row) + 1
		endLine := int(p.EndPoint().Row) + 1

		switch key {
		case "stages":
			valNode := pairValueNode(p)
			stages := getSequenceItems(valNode, src)
			for _, s := range stages {
				entities = append(entities, entity(
					"SCOPE.Component", s, "stage",
					"gitlab_ci/stage/"+s,
					file.Path, "yaml", startLine, endLine,
				))
			}

		default:
			if gitlabReservedKeys[key] || key == "" {
				continue
			}
			// Treat as a job definition. CONTAINS edge: file → job.
			jobRef := "gitlab_ci/job/" + key
			jobEnt := entity(
				"SCOPE.Operation", key, "job",
				jobRef,
				file.Path, "yaml", startLine, endLine,
			)
			jobEnt.Relationships = append(jobEnt.Relationships,
				containsRel(file.Path, jobRef))
			entities = append(entities, jobEnt)

			// Extract script entries
			jobVal := pairValueNode(p)
			jobBM := getBlockMapping(jobVal)
			if jobBM == nil {
				continue
			}
			var jobPairs []*sitter.Node
			for i := range jobBM.ChildCount() {
				child := jobBM.Child(int(i))
				if child != nil && child.Type() == "block_mapping_pair" {
					jobPairs = append(jobPairs, child)
				}
			}
			scriptNode := findValueNodeForKey(jobPairs, "script", src)
			scripts := getSequenceItems(scriptNode, src)
			for _, s := range scripts {
				entities = append(entities, entity(
					"SCOPE.Operation", s, "script_step",
					"gitlab_ci/script/"+key+"/"+s,
					file.Path, "yaml", startLine, endLine,
				))
			}
		}
	}

	return entities
}

// ---------------------------------------------------------------------------
// Docker Compose extractor
// ---------------------------------------------------------------------------

func extractDockerCompose(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	src := file.Content
	pairs := topLevelMappings(root)
	var entities []types.EntityRecord

	for _, p := range pairs {
		key := pairKeyText(p, src)
		startLine := int(p.StartPoint().Row) + 1
		endLine := int(p.EndPoint().Row) + 1

		switch key {
		case "services":
			servicePairs := getMappingPairsForKey(pairs, "services", src)
			for _, sp := range servicePairs {
				svcName := pairKeyText(sp, src)
				if svcName == "" {
					continue
				}
				sStart := int(sp.StartPoint().Row) + 1
				sEnd := int(sp.EndPoint().Row) + 1
				svcRef := "docker_compose/service/" + svcName
				svcEnt := entity(
					"SCOPE.Component", svcName, "service",
					svcRef,
					file.Path, "yaml", sStart, sEnd,
				)
				// CONTAINS: file → service.
				svcEnt.Relationships = append(svcEnt.Relationships,
					containsRel(file.Path, svcRef))

				// Ports
				svcVal := pairValueNode(sp)
				svcBM := getBlockMapping(svcVal)
				if svcBM == nil {
					entities = append(entities, svcEnt)
					continue
				}
				var svcPairs []*sitter.Node
				for i := range svcBM.ChildCount() {
					child := svcBM.Child(int(i))
					if child != nil && child.Type() == "block_mapping_pair" {
						svcPairs = append(svcPairs, child)
					}
				}

				// IMPORTS: depends_on → service. Compose dependency chains
				// look exactly like an import graph for purposes of resolution
				// (see issue #386).
				dependsNode := findValueNodeForKey(svcPairs, "depends_on", src)
				for _, dep := range getSequenceItems(dependsNode, src) {
					if dep == "" {
						continue
					}
					targetRef := "docker_compose/service/" + dep
					svcEnt.Relationships = append(svcEnt.Relationships,
						importsRel(svcRef, targetRef, "compose_depends_on"))
				}

				// IMPORTS: service → docker image (issue #424). The image ref is
				// an external dependency by definition — its source lives in a
				// container registry, not the indexed corpus. Emit a stub the
				// external-synth pass can route to ext:docker:<image> (lands in
				// ExternalKnown via the docker: allowlist branch).
				if imgVal := findPairValueText(svcPairs, "image", src); imgVal != "" {
					stub := "docker_image:" + imgVal
					svcEnt.Relationships = append(svcEnt.Relationships,
						importsRel(svcRef, stub, "compose_image"))
				}

				// IMPORTS: service → host filesystem mount (issue #424). Compose
				// volume entries can take three shapes:
				//   - "<src>:<dst>[:<mode>]"      (string short syntax)
				//   - "<named-volume>:<dst>"      (string short syntax, src is a key)
				//   - { source: ..., target: ... } (long syntax)
				// We only route source paths that look like a host filesystem
				// reference (./..., ../..., /abs, ~, ${VAR}, $VAR). Named-volume
				// sources and target-only entries already resolve via the
				// top-level volumes block — skip them.
				volNode := findValueNodeForKey(svcPairs, "volumes", src)
				for _, vol := range getSequenceItems(volNode, src) {
					srcPath := composeVolumeSource(vol)
					if !looksLikeHostPath(srcPath) {
						continue
					}
					stub := "host_path:" + srcPath
					svcEnt.Relationships = append(svcEnt.Relationships,
						importsRel(svcRef, stub, "compose_volume_mount"))
				}

				entities = append(entities, svcEnt)

				portsNode := findValueNodeForKey(svcPairs, "ports", src)
				ports := getSequenceItems(portsNode, src)
				for _, port := range ports {
					portRef := "docker_compose/port/" + svcName + "/" + port
					portEnt := entity(
						"SCOPE.Pattern", port, "port",
						portRef,
						file.Path, "yaml", sStart, sEnd,
					)
					// CONTAINS: service → port.
					portEnt.Relationships = append(portEnt.Relationships,
						containsRel(svcRef, portRef))
					entities = append(entities, portEnt)
				}
			}

		case "volumes":
			volPairs := getMappingPairsForKey(pairs, "volumes", src)
			for _, vp := range volPairs {
				volName := pairKeyText(vp, src)
				if volName == "" {
					continue
				}
				vStart := int(vp.StartPoint().Row) + 1
				vEnd := int(vp.EndPoint().Row) + 1
				volRef := "docker_compose/volume/" + volName
				volEnt := entity(
					"SCOPE.Schema", volName, "volume",
					volRef,
					file.Path, "yaml", vStart, vEnd,
				)
				// CONTAINS: file → volume.
				volEnt.Relationships = append(volEnt.Relationships,
					containsRel(file.Path, volRef))
				entities = append(entities, volEnt)
			}

		default:
			_ = startLine
			_ = endLine
		}
	}

	return entities
}

// ---------------------------------------------------------------------------
// Kubernetes extractor
// ---------------------------------------------------------------------------

func extractKubernetes(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	var entities []types.EntityRecord
	// Multi-document YAML: iterate all documents in the stream.
	for _, doc := range allDocuments(root) {
		entities = append(entities, extractKubernetesDoc(doc, file)...)
	}
	return entities
}

// extractKubernetesDoc extracts entities from a single K8s YAML document.
// Entity mapping:
//
//	metadata.name + kind:              → SCOPE.Service (Deployment/Service/StatefulSet/DaemonSet) or SCOPE.Component
//	spec.selector.matchLabels keys:    → SCOPE.Component, subtype="selector"  (Deployment/StatefulSet/DaemonSet)
//	containers[].name:                 → SCOPE.Component, subtype="container"
//	containers[].ports[].containerPort → SCOPE.Component, subtype="container_port"
//	containers[].env[].name:           → SCOPE.Schema,    subtype="env_var"
//	containers[].resources.(limits|requests) keys: → SCOPE.Schema, subtype="resource_limit"
//	containers[].volumeMounts[].name:  → SCOPE.Schema,    subtype="volume_mount"
//	initContainers[].name:             → SCOPE.Component, subtype="init_container"
//	ConfigMap data keys:               → SCOPE.Schema,    subtype="config_key"
//	Ingress spec.rules[].host:         → SCOPE.ExternalAPI, subtype="ingress_host"
//	Ingress spec.rules[].http.paths[].path: → SCOPE.Operation, subtype="ingress_path"
//	Service selector + ports:          → existing extractK8sService logic
func extractKubernetesDoc(doc *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	src := file.Content
	pairs := documentMappings(doc)
	var entities []types.EntityRecord

	// File-scoped ref prefix avoids QualifiedName collisions across manifests
	// in argocd-style apps where the same metadata.name / container name appears
	// in many files (Refs #44).
	refPrefix := "k8s/" + file.Path + "#"

	// Get kind value
	kindVal := ""
	for _, p := range pairs {
		if pairKeyText(p, src) == "kind" {
			kindVal = getPairValueText(p, src)
			break
		}
	}

	// Get metadata.name
	metadataPairs := getMappingPairsForKey(pairs, "metadata", src)
	metadataName := findPairValueText(metadataPairs, "name", src)

	startLine := 1
	endLine := bytes.Count(src, []byte("\n")) + 1
	if doc != nil {
		startLine = int(doc.StartPoint().Row) + 1
		endLine = int(doc.EndPoint().Row) + 1
	}

	resourceRef := ""
	if metadataName != "" {
		// Deployment/Service/StatefulSet/DaemonSet → SCOPE.Service; others → SCOPE.Component.
		topKind := "SCOPE.Component"
		if kindVal == "Service" || kindVal == "Deployment" || kindVal == "StatefulSet" || kindVal == "DaemonSet" {
			topKind = "SCOPE.Service"
		}
		// Include the K8s Kind in the ref to disambiguate same-name resources
		// of different kinds in multi-document manifests (e.g. a Deployment
		// and a Service both named "frontend" — argocd helm-hooks pattern).
		resourceRef = refPrefix + "resource/" + kindVal + "/" + metadataName
		// QualifiedName must match resourceRef so CONTAINS edges from
		// other entities resolve via byQualifiedName (Refs #44). The pre-fix
		// code passed kindVal here, which caused every K8s resource entity
		// to be indexed under its Kind ("Deployment", "Service") and the
		// resolver could never bind ToID = "k8s/<file>#resource/<name>".
		resEnt := entity(
			topKind, metadataName, "k8s_resource",
			resourceRef,
			file.Path, "yaml", startLine, endLine,
		)
		// CONTAINS: file → resource.
		resEnt.Relationships = append(resEnt.Relationships,
			containsRel(file.Path, resourceRef))
		entities = append(entities, resEnt)
	}

	specPairs := getMappingPairsForKey(pairs, "spec", src)

	switch kindVal {
	case "Service":
		entities = append(entities, extractK8sService(specPairs, metadataName, refPrefix, file, src, startLine, endLine)...)

	case "ConfigMap":
		// ConfigMap data keys → SCOPE.Schema config_key
		dataPairs := getMappingPairsForKey(pairs, "data", src)
		for _, dp := range dataPairs {
			k := pairKeyText(dp, src)
			if k == "" {
				continue
			}
			dStart := int(dp.StartPoint().Row) + 1
			dEnd := int(dp.EndPoint().Row) + 1
			entities = append(entities, entity(
				"SCOPE.Schema", k, "config_key",
				refPrefix+"configmap/"+metadataName+"/"+k,
				file.Path, "yaml", dStart, dEnd,
			))
		}

	case "Ingress":
		entities = append(entities, extractK8sIngress(specPairs, metadataName, refPrefix, file, src)...)

	default:
		// Deployment / StatefulSet / DaemonSet / generic workloads.

		// Deployment selector.matchLabels → SCOPE.Component selector
		if kindVal == "Deployment" || kindVal == "StatefulSet" || kindVal == "DaemonSet" {
			selectorPairs := getMappingPairsForKey(specPairs, "selector", src)
			matchLabelPairs := getMappingPairsForKey(selectorPairs, "matchLabels", src)
			for _, mlp := range matchLabelPairs {
				k := pairKeyText(mlp, src)
				v := getPairValueText(mlp, src)
				if k == "" {
					continue
				}
				mlStart := int(mlp.StartPoint().Row) + 1
				mlEnd := int(mlp.EndPoint().Row) + 1
				entities = append(entities, entity(
					"SCOPE.Component", k+"="+v, "selector",
					refPrefix+"selector/"+metadataName+"/"+k,
					file.Path, "yaml", mlStart, mlEnd,
				))
			}
		}

		// initContainers
		templatePairs, innerSpecPairs := k8sTemplatePairs(specPairs, src)
		_ = templatePairs
		initContainersNode := findValueNodeForKey(innerSpecPairs, "initContainers", src)
		if initContainersNode == nil {
			initContainersNode = findValueNodeForKey(specPairs, "initContainers", src)
		}
		initContainerMappings := getSequenceItemMappings(initContainersNode, src)
		for _, icPairs := range initContainerMappings {
			name := findPairValueText(icPairs, "name", src)
			if name == "" {
				continue
			}
			icStart, icEnd := pairsLineRange(icPairs)
			icRef := refPrefix + "init-container/" + name
			icEnt := entity(
				"SCOPE.Component", name, "init_container",
				icRef,
				file.Path, "yaml", icStart, icEnd,
			)
			if resourceRef != "" {
				icEnt.Relationships = append(icEnt.Relationships,
					containsRel(resourceRef, icRef))
			}
			// IMPORTS: init_container → docker image (issue #424).
			if imgVal := findPairValueText(icPairs, "image", src); imgVal != "" {
				icEnt.Relationships = append(icEnt.Relationships,
					importsRel(icRef, "docker_image:"+imgVal, "k8s_image"))
			}
			entities = append(entities, icEnt)
		}

		// Main containers
		containers := findK8sContainers(specPairs, src)
		for _, cPairs := range containers {
			name := findPairValueText(cPairs, "name", src)
			if name == "" {
				continue
			}
			cStart, cEnd := pairsLineRange(cPairs)
			cRef := refPrefix + "container/" + name
			cEnt := entity(
				"SCOPE.Component", name, "container",
				cRef,
				file.Path, "yaml", cStart, cEnd,
			)
			if resourceRef != "" {
				cEnt.Relationships = append(cEnt.Relationships,
					containsRel(resourceRef, cRef))
			}

			// IMPORTS: container → docker image (issue #424). Same routing as
			// compose `image:` — the registry is outside the indexed corpus,
			// so external-synth lifts it to ext:docker:<image>.
			if imgVal := findPairValueText(cPairs, "image", src); imgVal != "" {
				cEnt.Relationships = append(cEnt.Relationships,
					importsRel(cRef, "docker_image:"+imgVal, "k8s_image"))
			}

			entities = append(entities, cEnt)

			// containerPort values
			portsNode := findValueNodeForKey(cPairs, "ports", src)
			portMappings := getSequenceItemMappings(portsNode, src)
			for _, portPairs := range portMappings {
				portVal := findPairValueText(portPairs, "containerPort", src)
				if portVal == "" {
					continue
				}
				portName := findPairValueText(portPairs, "name", src)
				entityName := portVal
				if portName != "" {
					entityName = portName + ":" + portVal
				}
				pStart, pEnd := pairsLineRange(portPairs)
				entities = append(entities, entity(
					"SCOPE.Component", entityName, "container_port",
					refPrefix+"port/"+name+"/"+portVal,
					file.Path, "yaml", pStart, pEnd,
				))
			}

			// env vars
			envNode := findValueNodeForKey(cPairs, "env", src)
			envMappings := getSequenceItemMappings(envNode, src)
			for _, envPairs := range envMappings {
				envName := findPairValueText(envPairs, "name", src)
				if envName == "" {
					continue
				}
				eStart, eEnd := pairsLineRange(envPairs)
				entities = append(entities, entity(
					"SCOPE.Schema", envName, "env_var",
					refPrefix+"env/"+name+"/"+envName,
					file.Path, "yaml", eStart, eEnd,
				))
			}

			// resource limits and requests
			resourcesPairs := getMappingPairsForKey(cPairs, "resources", src)
			for _, section := range []string{"limits", "requests"} {
				sectionPairs := getMappingPairsForKey(resourcesPairs, section, src)
				for _, rp := range sectionPairs {
					rKey := pairKeyText(rp, src)
					rVal := getPairValueText(rp, src)
					if rKey == "" {
						continue
					}
					rStart := int(rp.StartPoint().Row) + 1
					rEnd := int(rp.EndPoint().Row) + 1
					entities = append(entities, entity(
						"SCOPE.Schema", section+"."+rKey+"="+rVal, "resource_limit",
						refPrefix+"resource/"+name+"/"+section+"/"+rKey,
						file.Path, "yaml", rStart, rEnd,
					))
				}
			}

			// volumeMounts
			volumeMountsNode := findValueNodeForKey(cPairs, "volumeMounts", src)
			vmMappings := getSequenceItemMappings(volumeMountsNode, src)
			for _, vmPairs := range vmMappings {
				vmName := findPairValueText(vmPairs, "name", src)
				if vmName == "" {
					continue
				}
				vmStart, vmEnd := pairsLineRange(vmPairs)
				entities = append(entities, entity(
					"SCOPE.Schema", vmName, "volume_mount",
					refPrefix+"volumemount/"+name+"/"+vmName,
					file.Path, "yaml", vmStart, vmEnd,
				))
			}
		}
	}

	return entities
}

// k8sTemplatePairs returns (templatePairs, innerSpecPairs) for Deployment/StatefulSet/DaemonSet.
// innerSpecPairs is spec.template.spec pairs.
func k8sTemplatePairs(specPairs []*sitter.Node, src []byte) ([]*sitter.Node, []*sitter.Node) {
	templatePairs := getMappingPairsForKey(specPairs, "template", src)
	if templatePairs == nil {
		return nil, nil
	}
	innerSpecPairs := getMappingPairsForKey(templatePairs, "spec", src)
	return templatePairs, innerSpecPairs
}

// extractK8sIngress extracts entities from a Kubernetes Ingress spec.
// Emits ingress_host (SCOPE.ExternalAPI) and ingress_path (SCOPE.Operation).
func extractK8sIngress(specPairs []*sitter.Node, ingressName, refPrefix string, file extractor.FileInput, src []byte) []types.EntityRecord {
	var entities []types.EntityRecord

	rulesMappings := getSequenceItemMappings(findValueNodeForKey(specPairs, "rules", src), src)
	for _, rulePairs := range rulesMappings {
		host := findPairValueText(rulePairs, "host", src)
		rStart, rEnd := pairsLineRange(rulePairs)
		if host != "" {
			entities = append(entities, entity(
				"SCOPE.ExternalAPI", host, "ingress_host",
				refPrefix+"ingress/"+ingressName+"/"+host,
				file.Path, "yaml", rStart, rEnd,
			))
		}

		// http.paths[]
		httpPairs := getMappingPairsForKey(rulePairs, "http", src)
		pathMappings := getSequenceItemMappings(findValueNodeForKey(httpPairs, "paths", src), src)
		for _, pathPairs := range pathMappings {
			pathVal := findPairValueText(pathPairs, "path", src)
			if pathVal == "" {
				continue
			}
			pStart, pEnd := pairsLineRange(pathPairs)
			entityName := pathVal
			if host != "" {
				entityName = host + pathVal
			}
			entities = append(entities, entity(
				"SCOPE.Operation", entityName, "ingress_path",
				refPrefix+"ingress-path/"+ingressName+"/"+pathVal,
				file.Path, "yaml", pStart, pEnd,
			))
		}
	}

	return entities
}

// extractK8sService extracts entities from a Kubernetes Service spec.
// Emits selector labels as SCOPE.Component and service ports as SCOPE.Component.
func extractK8sService(specPairs []*sitter.Node, svcName, refPrefix string, file extractor.FileInput, src []byte, startLine, endLine int) []types.EntityRecord {
	var entities []types.EntityRecord

	// Selector entries.
	selectorPairs := getMappingPairsForKey(specPairs, "selector", src)
	for _, sp := range selectorPairs {
		k := pairKeyText(sp, src)
		v := getPairValueText(sp, src)
		if k == "" || v == "" {
			continue
		}
		entities = append(entities, entity(
			"SCOPE.Component", k+"="+v, "selector",
			refPrefix+"selector/"+svcName+"/"+k,
			file.Path, "yaml", startLine, endLine,
		))
	}

	// Service ports.
	portsNode := findValueNodeForKey(specPairs, "ports", src)
	portMappings := getSequenceItemMappings(portsNode, src)
	for _, portPairs := range portMappings {
		portVal := findPairValueText(portPairs, "port", src)
		if portVal == "" {
			continue
		}
		portName := findPairValueText(portPairs, "name", src)
		entityName := portVal
		if portName != "" {
			entityName = portName + ":" + portVal
		}
		pStart, pEnd := pairsLineRange(portPairs)
		entities = append(entities, entity(
			"SCOPE.Component", entityName, "service_port",
			refPrefix+"svc-port/"+svcName+"/"+portVal,
			file.Path, "yaml", pStart, pEnd,
		))
	}

	return entities
}

// findK8sContainers searches for containers in specPairs, drilling into
// template.spec if needed (Deployment/StatefulSet pattern).
func findK8sContainers(specPairs []*sitter.Node, src []byte) [][]*sitter.Node {
	// Try direct spec.containers first
	containersNode := findValueNodeForKey(specPairs, "containers", src)
	if containersNode != nil {
		return getSequenceItemMappings(containersNode, src)
	}
	// Try spec.template.spec.containers
	templatePairs := getMappingPairsForKey(specPairs, "template", src)
	if templatePairs == nil {
		return nil
	}
	innerSpecPairs := getMappingPairsForKey(templatePairs, "spec", src)
	if innerSpecPairs == nil {
		return nil
	}
	containersNode = findValueNodeForKey(innerSpecPairs, "containers", src)
	if containersNode == nil {
		return nil
	}
	return getSequenceItemMappings(containersNode, src)
}

// ---------------------------------------------------------------------------
// Ansible extractor
// ---------------------------------------------------------------------------

func extractAnsible(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	src := file.Content
	var entities []types.EntityRecord

	// Multi-document support: iterate each document independently.
	for _, doc := range allDocuments(root) {
		if isDocSequence(doc) {
			entities = append(entities, extractAnsiblePlaybookDoc(doc, file)...)
		} else {
			// Flat format: top-level tasks/handlers/roles keys. No enclosing
			// play, so CONTAINS edges (when emitted) are rooted at the file.
			for _, p := range documentMappings(doc) {
				entities = append(entities, extractAnsibleSectionPairs(p, file, src, "")...)
			}
		}
	}
	return entities
}

// isDocSequence returns true when a document node has a top-level block_sequence.
func isDocSequence(doc *sitter.Node) bool {
	if doc == nil {
		return false
	}
	bn := findFirstChild(doc, "block_node")
	if bn == nil {
		return false
	}
	return findFirstChild(bn, "block_sequence") != nil
}

// extractAnsiblePlaybookDoc processes a single document node in playbook format.
func extractAnsiblePlaybookDoc(doc *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	src := file.Content
	var entities []types.EntityRecord

	if doc == nil {
		return entities
	}
	bn := findFirstChild(doc, "block_node")
	if bn == nil {
		return entities
	}
	seq := findFirstChild(bn, "block_sequence")
	if seq == nil {
		return entities
	}

	for i := range seq.ChildCount() {
		item := seq.Child(int(i))
		if item == nil || item.Type() != "block_sequence_item" {
			continue
		}

		// Each item is a play mapping.
		for j := range item.ChildCount() {
			child := item.Child(int(j))
			if child == nil || child.Type() == "-" {
				continue
			}
			bm := getBlockMapping(child)
			if bm == nil {
				continue
			}
			var playPairs []*sitter.Node
			for k := range bm.ChildCount() {
				cp := bm.Child(int(k))
				if cp != nil && cp.Type() == "block_mapping_pair" {
					playPairs = append(playPairs, cp)
				}
			}

			// Emit play name (- name: ...) as SCOPE.Service
			playName := findPairValueText(playPairs, "name", src)
			playRef := ""
			if playName != "" {
				pStart, pEnd := pairsLineRange(playPairs)
				playRef = "ansible/play/" + playName
				playEnt := entity(
					"SCOPE.Service", playName, "play",
					playRef,
					file.Path, "yaml", pStart, pEnd,
				)
				// CONTAINS: file → play.
				playEnt.Relationships = append(playEnt.Relationships,
					containsRel(file.Path, playRef))
				entities = append(entities, playEnt)
			}

			// Emit hosts target as SCOPE.Component
			hostsVal := findPairValueText(playPairs, "hosts", src)
			if hostsVal != "" {
				pStart, pEnd := pairsLineRange(playPairs)
				entities = append(entities, entity(
					"SCOPE.Component", hostsVal, "hosts",
					"ansible/hosts/"+hostsVal,
					file.Path, "yaml", pStart, pEnd,
				))
			}

			// Extract tasks, pre_tasks, post_tasks, handlers, roles. The play
			// is the enclosing parent for the CONTAINS edges these emit.
			for _, pp := range playPairs {
				entities = append(entities, extractAnsibleSectionPairs(pp, file, src, playRef)...)
			}
		}
	}

	return entities
}

// extractAnsibleSectionPairs extracts entities from a single block_mapping_pair
// that represents a section (tasks, pre_tasks, post_tasks, handlers, roles).
// parentRef is the canonical ref of the enclosing play (e.g. "ansible/play/X")
// or "" when there's no enclosing play (flat task file). When non-empty, every
// emitted child carries a CONTAINS edge from parentRef.
func extractAnsibleSectionPairs(p *sitter.Node, file extractor.FileInput, src []byte, parentRef string) []types.EntityRecord {
	key := pairKeyText(p, src)
	valNode := pairValueNode(p)
	var entities []types.EntityRecord

	addContains := func(ent *types.EntityRecord, childRef string) {
		if parentRef == "" {
			return
		}
		ent.Relationships = append(ent.Relationships,
			containsRel(parentRef, childRef))
	}

	switch key {
	case "tasks", "pre_tasks", "post_tasks":
		taskMappings := getSequenceItemMappings(valNode, src)
		for _, taskPairs := range taskMappings {
			name := findPairValueText(taskPairs, "name", src)
			if name == "" {
				continue
			}
			tStart, tEnd := pairsLineRange(taskPairs)
			ref := "ansible/task/" + name
			ent := entity(
				"SCOPE.Operation", name, "task",
				ref,
				file.Path, "yaml", tStart, tEnd,
			)
			addContains(&ent, ref)
			entities = append(entities, ent)
		}

	case "handlers":
		handlerMappings := getSequenceItemMappings(valNode, src)
		for _, hPairs := range handlerMappings {
			name := findPairValueText(hPairs, "name", src)
			if name == "" {
				continue
			}
			hStart, hEnd := pairsLineRange(hPairs)
			ref := "ansible/handler/" + name
			ent := entity(
				"SCOPE.Operation", name, "handler",
				ref,
				file.Path, "yaml", hStart, hEnd,
			)
			addContains(&ent, ref)
			entities = append(entities, ent)
		}

	case "roles":
		startLine := int(p.StartPoint().Row) + 1
		endLine := int(p.EndPoint().Row) + 1

		// Roles can be scalar strings OR mappings with role: key.
		// Try scalar strings first.
		roles := getSequenceItems(valNode, src)
		for _, r := range roles {
			// Skip mapping-like entries (they start with "{").
			if strings.HasPrefix(r, "{") || strings.HasPrefix(r, "role:") {
				continue
			}
			roleName := strings.TrimPrefix(r, "- ")
			roleName = strings.Trim(roleName, `"'`)
			if roleName == "" {
				continue
			}
			ref := "ansible/role/" + roleName
			ent := entity(
				"SCOPE.Component", roleName, "role",
				ref,
				file.Path, "yaml", startLine, endLine,
			)
			addContains(&ent, ref)
			entities = append(entities, ent)
		}

		// Also handle roles as mappings with role: key.
		roleMappings := getSequenceItemMappings(valNode, src)
		for _, rPairs := range roleMappings {
			roleName := findPairValueText(rPairs, "role", src)
			if roleName == "" {
				continue
			}
			rStart, rEnd := pairsLineRange(rPairs)
			ref := "ansible/role/" + roleName
			ent := entity(
				"SCOPE.Component", roleName, "role",
				ref,
				file.Path, "yaml", rStart, rEnd,
			)
			addContains(&ent, ref)
			entities = append(entities, ent)
		}
	}

	return entities
}

// ---------------------------------------------------------------------------
// Kustomize extractor (#3520)
// ---------------------------------------------------------------------------

// extractKustomize processes a kustomization.yaml, building the overlay-
// composition graph:
//
//	resources: / bases: / components:   → IMPORTS edges (kustomization → manifest/overlay)
//	patches: / patchesStrategicMerge: / patchesJson6902: → PATCHES edges (kustomization → target)
//	configMapGenerator: / secretGenerator:  → generated-resource entities
//	namespace: / namePrefix: / nameSuffix: / commonLabels: → transform metadata on the kustomization
//
// All IMPORTS/PATCHES edges originate from the kustomization entity whose
// QualifiedName equals file.Path, so they resolve against the per-file
// SCOPE.Document anchor (issue #474 chain-fix) via byQualifiedName.
func extractKustomize(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	src := file.Content
	pairs := topLevelMappings(root)
	var entities []types.EntityRecord

	// The kustomization itself is the root entity for the overlay graph. Its
	// QualifiedName is file.Path so it coincides with the SCOPE.Document anchor
	// the dispatcher prepends; IMPORTS/PATCHES edges use file.Path as FromID and
	// resolve through that anchor.
	name := file.Path
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		name = name[idx+1:]
	}
	startLine := 1
	endLine := bytes.Count(src, []byte("\n")) + 1
	if root != nil {
		endLine = int(root.EndPoint().Row) + 1
	}
	kustRef := file.Path
	kustEnt := entity(
		"SCOPE.Component", name, "kustomization",
		kustRef,
		file.Path, "yaml", startLine, endLine,
	)

	// Collect transform metadata onto the kustomization entity's Properties so
	// downstream tooling sees namespace / prefix / suffix / commonLabels at a
	// glance without re-parsing the file.
	transforms := map[string]string{}
	for _, p := range pairs {
		switch pairKeyText(p, src) {
		case "namespace":
			if v := getPairValueText(p, src); v != "" {
				transforms["kust_namespace"] = v
			}
		case "namePrefix":
			if v := getPairValueText(p, src); v != "" {
				transforms["kust_name_prefix"] = v
			}
		case "nameSuffix":
			if v := getPairValueText(p, src); v != "" {
				transforms["kust_name_suffix"] = v
			}
		case "commonLabels":
			labelPairs := getMappingPairsForKey(pairs, "commonLabels", src)
			var kvs []string
			for _, lp := range labelPairs {
				k := pairKeyText(lp, src)
				v := getPairValueText(lp, src)
				if k != "" {
					kvs = append(kvs, k+"="+v)
				}
			}
			if len(kvs) > 0 {
				transforms["kust_common_labels"] = strings.Join(kvs, ",")
			}
		}
	}
	if len(transforms) > 0 {
		if kustEnt.Properties == nil {
			kustEnt.Properties = map[string]string{}
		}
		for k, v := range transforms {
			kustEnt.Properties[k] = v
		}
	}

	// IMPORTS: resources / bases / components → referenced manifest/overlay path.
	// Each is a list of file or directory paths. kustomize_import_kind records
	// which list the path came from for downstream disambiguation.
	for _, key := range []struct{ field, importKind string }{
		{"resources", "kustomize_resource"},
		{"bases", "kustomize_base"},
		{"components", "kustomize_component"},
	} {
		valNode := findValueNodeForKey(pairs, key.field, src)
		for _, ref := range getSequenceItems(valNode, src) {
			ref = kustTrimScalar(ref)
			if ref == "" {
				continue
			}
			kustEnt.Relationships = append(kustEnt.Relationships,
				importsRel(kustRef, "kustomize_path:"+ref, key.importKind))
		}
	}

	entities = append(entities, kustEnt)

	// PATCHES: patches / patchesStrategicMerge / patchesJson6902. These mutate
	// the kustomization entity in place (append edges).
	kustExtractPatches(&kustEnt, pairs, kustRef, file, src)
	// Re-sync the (value-copied) kustomization entity now that patch edges are
	// attached — it was appended above by value.
	entities[len(entities)-1] = kustEnt

	// Generated resources: configMapGenerator / secretGenerator → new entities.
	entities = append(entities, kustExtractGenerators(pairs, kustRef, file, src)...)

	return entities
}

// kustTrimScalar strips a leading "- " list marker and surrounding quotes from
// a raw sequence-item scalar (getSequenceItems returns the verbatim node text).
func kustTrimScalar(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "- ")
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	return s
}

// kustPatchTargetStub builds the synthetic ToID for a patch target. When the
// patch declares a target {kind,name}, the stub is
// "kustomize_target:<Kind>/<name>"; otherwise it falls back to a file-reference
// stub "kustomize_patch_file:<path>" or "kustomize_inline_patch".
func kustPatchTargetStub(targetKind, targetName, patchFile string) string {
	switch {
	case targetKind != "" && targetName != "":
		return "kustomize_target:" + targetKind + "/" + targetName
	case targetKind != "":
		return "kustomize_target:" + targetKind
	case targetName != "":
		return "kustomize_target:" + targetName
	case patchFile != "":
		return "kustomize_patch_file:" + patchFile
	default:
		return "kustomize_inline_patch"
	}
}

// kustExtractPatches appends PATCHES edges from the kustomization to each patch
// target. Handles the three patch list shapes Kustomize accepts:
//
//	patchesStrategicMerge: [ file.yaml, ... ]            (scalar file paths)
//	patches:              [ { path|patch, target:{kind,name} }, ... ]
//	patchesJson6902:      [ { target:{group,version,kind,name}, path|patch }, ... ]
func kustExtractPatches(kustEnt *types.EntityRecord, pairs []*sitter.Node, kustRef string, file extractor.FileInput, src []byte) {
	patchesRel := func(toID, style, targetKind, targetName string) {
		rel := types.RelationshipRecord{
			FromID: kustRef,
			ToID:   toID,
			Kind:   "PATCHES",
			Properties: map[string]string{
				"patch_style": style,
			},
		}
		if targetKind != "" {
			rel.Properties["target_kind"] = targetKind
		}
		if targetName != "" {
			rel.Properties["target_name"] = targetName
		}
		kustEnt.Relationships = append(kustEnt.Relationships, rel)
	}

	// patchesStrategicMerge: scalar list of file paths.
	smNode := findValueNodeForKey(pairs, "patchesStrategicMerge", src)
	for _, ref := range getSequenceItems(smNode, src) {
		ref = kustTrimScalar(ref)
		if ref == "" {
			continue
		}
		patchesRel(kustPatchTargetStub("", "", ref), "strategic_merge", "", "")
	}

	// patches: list of mappings with optional target and path|patch.
	patchMappings := getSequenceItemMappings(findValueNodeForKey(pairs, "patches", src), src)
	for _, pp := range patchMappings {
		kustEmitPatchMapping(pp, "strategic_merge", patchesRel, src)
	}

	// patchesJson6902: list of mappings with a required target and path|patch.
	json6902Mappings := getSequenceItemMappings(findValueNodeForKey(pairs, "patchesJson6902", src), src)
	for _, pp := range json6902Mappings {
		kustEmitPatchMapping(pp, "json6902", patchesRel, src)
	}
}

// kustEmitPatchMapping resolves a single patch mapping's target (from a nested
// target: {kind,name}) and file/inline body, then emits one PATCHES edge.
func kustEmitPatchMapping(pp []*sitter.Node, style string, emit func(toID, style, targetKind, targetName string), src []byte) {
	targetPairs := getMappingPairsForKey(pp, "target", src)
	targetKind := findPairValueText(targetPairs, "kind", src)
	targetName := findPairValueText(targetPairs, "name", src)
	patchFile := findPairValueText(pp, "path", src)
	finalStyle := style
	if findPairValueText(pp, "patch", src) != "" && patchFile == "" {
		finalStyle = "inline"
	}
	emit(kustPatchTargetStub(targetKind, targetName, patchFile), finalStyle, targetKind, targetName)
}

// kustExtractGenerators emits one generated-resource entity per entry in
// configMapGenerator: / secretGenerator:. The entity Name is the generator's
// name:; the literals/files it pulls are recorded in Properties so the
// generated config surface is visible without re-parsing.
func kustExtractGenerators(pairs []*sitter.Node, kustRef string, file extractor.FileInput, src []byte) []types.EntityRecord {
	var entities []types.EntityRecord

	for _, gen := range []struct{ field, subtype, kind string }{
		{"configMapGenerator", "generated_configmap", "SCOPE.Schema"},
		{"secretGenerator", "generated_secret", "SCOPE.Schema"},
	} {
		genMappings := getSequenceItemMappings(findValueNodeForKey(pairs, gen.field, src), src)
		for _, gp := range genMappings {
			genName := findPairValueText(gp, "name", src)
			if genName == "" {
				continue
			}
			gStart, gEnd := pairsLineRange(gp)
			genRef := "kustomize/" + gen.subtype + "/" + file.Path + "#" + genName
			genEnt := entity(
				gen.kind, genName, gen.subtype,
				genRef,
				file.Path, "yaml", gStart, gEnd,
			)
			// CONTAINS: kustomization → generated resource.
			genEnt.Relationships = append(genEnt.Relationships,
				containsRel(kustRef, genRef))

			// Record the literals: / files: / envs: the generator pulls from.
			props := map[string]string{}
			if lits := getSequenceItems(findValueNodeForKey(gp, "literals", src), src); len(lits) > 0 {
				cleaned := make([]string, 0, len(lits))
				for _, l := range lits {
					cleaned = append(cleaned, kustTrimScalar(l))
				}
				props["literals"] = strings.Join(cleaned, ",")
			}
			if files := getSequenceItems(findValueNodeForKey(gp, "files", src), src); len(files) > 0 {
				cleaned := make([]string, 0, len(files))
				for _, f := range files {
					cleaned = append(cleaned, kustTrimScalar(f))
				}
				props["files"] = strings.Join(cleaned, ",")
			}
			if envs := getSequenceItems(findValueNodeForKey(gp, "envs", src), src); len(envs) > 0 {
				cleaned := make([]string, 0, len(envs))
				for _, e := range envs {
					cleaned = append(cleaned, kustTrimScalar(e))
				}
				props["envs"] = strings.Join(cleaned, ",")
			}
			if len(props) > 0 {
				genEnt.Properties = props
			}
			entities = append(entities, genEnt)
		}
	}

	return entities
}

// ---------------------------------------------------------------------------
// Generic extractor
// ---------------------------------------------------------------------------

func extractGeneric(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	src := file.Content
	pairs := topLevelMappings(root)
	var entities []types.EntityRecord

	for _, p := range pairs {
		key := pairKeyText(p, src)
		if key == "" {
			continue
		}
		startLine := int(p.StartPoint().Row) + 1
		endLine := int(p.EndPoint().Row) + 1
		entities = append(entities, entity(
			"SCOPE.Schema", key, "key",
			"yaml/key/"+key,
			file.Path, "yaml", startLine, endLine,
		))
	}

	return entities
}
