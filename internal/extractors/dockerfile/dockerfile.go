// Package dockerfile implements the tree-sitter–based extractor for Dockerfile source files.
//
// # Entity model (post-#2063 refactor)
//
// A single SCOPE.Component/dockerfile entity is emitted per Dockerfile file.
// All instruction details (RUN, COPY, EXPOSE, ENV, ARG, ENTRYPOINT/CMD) are
// folded into properties on that one entity to avoid degree-0 orphans. Edges:
//
//   - IMPORTS (file → base-image) — one per FROM instruction
//   - CONTAINS (file → stage structural-ref) — one per FROM instruction
//   - USES (file → stage structural-ref) — for COPY --from=<stage>
//
// Multi-stage builds: stage images are captured in properties.stages (CSV) and
// individual stage details in properties.stage_<i>_* keys.
//
// Registers itself via init() and is imported by registry_gen.go.
package dockerfile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cajasmota/grafel/internal/indexstate"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("dockerfile", &Extractor{})
}

// Extractor implements extractor.Extractor for Dockerfile.
type Extractor struct{}

// Language returns the canonical language key.
func (e *Extractor) Language() string { return "dockerfile" }

// Extract walks the tree-sitter CST and returns a single EntityRecord for the
// Dockerfile file. On empty src, returns an empty slice with nil error; on a nil
// tree with non-empty src it parses the content itself (under the daemon-wide
// parse gate) rather than bailing — see the #6154 note on the guard below.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.dockerfile")
	ctx, span := tracer.Start(ctx, "indexer.extract.dockerfile",
		trace.WithAttributes(attribute.String("language", "dockerfile")),
	)
	defer span.End()

	// #6154 — this guard used to also bail on `file.TSTree == nil`, which made
	// the self-parse fallback at the `if tree == nil` block below UNREACHABLE:
	// the only way to reach it was with a nil tree, and a nil tree returned here
	// first. The other six AcquireParseSlot self-parse extractors (python,
	// golang, html, hcl, cpp, yaml) guard on empty content ONLY, so their
	// fallbacks are live; dockerfile was the sole outlier, and the dead block
	// read as coverage in two contradictory audits at once. Guard on content
	// alone, matching the other six, so the fallback the comment below describes
	// can actually run.
	if len(file.Content) == 0 {
		span.SetAttributes(
			attribute.Int("file_line_count", 0),
			attribute.Int("entity_count", 0),
			attribute.Int("stage_count", 0),
		)
		return nil, nil
	}

	// Reuse pre-parsed tree or parse inline.
	//
	// #6154 — WHAT A NIL TSTree ACTUALLY MEANS AT THE PRODUCTION CALL SITE, and
	// the one place this fallback is in tension with the rest of the pipeline.
	// index.go:3447 leaves TSTree nil whenever the factory's Parse returned an
	// error, and ErrHighSyntaxErrorRate is exactly that case — a file the
	// syntax-error gate deliberately rejected. Re-parsing here does not consult
	// that gate, so a rejected Dockerfile is extracted rather than skipped.
	//
	// This is currently harmless and is left deliberately: the re-parse produces
	// the same mostly-ERROR tree the gate saw, and buildDockerfileEntity walks it
	// to the same (empty or near-empty) result, so both paths agree — the parity
	// asserted in dockerfile_test.go. But it does narrow the claim at
	// incremental.go:565-568 that "both paths emit nothing and neither is
	// lying": here the fallback path emits nothing because the tree is bad, not
	// because it declined to look. If the gate ever grows a policy the extractor
	// must honour, this is the site that has to consult it.
	tree := file.TSTree
	if tree == nil {
		parser, perr := dockerfileAdapter.NewParser(dockerfileGrammar())
		if perr != nil {
			return nil, perr
		}
		defer parser.Close()
		var err error
		// #5954 — bound this parse by the same daemon-wide gate that
		// treesitter.ParserFactory.Parse uses. This fallback bypasses the
		// factory, so before #5954 it bypassed parseMu; it must not now
		// bypass AcquireParseSlot, or the in-process parse ceiling is
		// illusory (GOMAXPROCS cannot bound cgo). The defer is scoped to a
		// closure around the parse alone so the release happens BEFORE the node
		// walk (holding it across the walk would throttle extraction, not
		// parsing) and still survives a panic in Parse. See the python
		// extractor for the full rationale.
		func() {
			indexstate.AcquireParseSlot()
			defer indexstate.ReleaseParseSlot()
			tree, err = parser.Parse(file.Content)
		}()
		if err != nil {
			return nil, err
		}
		// #6154 — NIL TREE WITH A NIL ERROR IS REACHABLE, and making this
		// fallback live is what makes it reachable HERE. official.Parse returns
		// (nil, nil) when the parse watchdog is disabled (timeout == 0,
		// ts/official/official.go:155) — it only converts a nil tree into
		// ErrParseDeadlineExceeded while the watchdog is armed. Without this
		// guard the tree.RootNode() below is a nil dereference on a path that
		// did not exist before this change. python (extractor.go:142) and golang
		// (:108) both guard exactly here; dockerfile now matches them.
		if tree == nil {
			return nil, fmt.Errorf("dockerfile extractor: parse produced nil tree")
		}
		// Close ONLY the tree we parsed ourselves — file.TSTree belongs to the
		// caller and is closed by the indexer. Without this the CST leaks for the
		// daemon's lifetime: #5963 (see treesitter/parser.go) measures a
		// tree-sitter CST at ~19.7 bytes of C heap per source byte, and v0.24
		// attaches no finalizer, so nothing reclaims it. Scoped to the function,
		// not the parse closure, because the node walk below still needs the tree.
		defer tree.Close()
	}

	lineCount := strings.Count(string(file.Content), "\n") + 1
	entity, stageCount := buildDockerfileEntity(tree.RootNode(), file)

	// Issue #384 — language tag for resolver dynamic-pattern dispatch.
	// Issue #2371 — entity language tag via named slice so both entity and
	// relationship language fields are stamped on the actual entity value.
	entities := []types.EntityRecord{entity}
	extractor.TagRelationshipsLanguage(entities, "dockerfile")
	extractor.TagEntitiesLanguage(entities, "dockerfile")
	entity = entities[0]

	span.SetAttributes(
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", 1),
		attribute.Int("stage_count", stageCount),
	)

	// Return no entities for Dockerfiles with no FROM instructions (degenerate).
	if stageCount == 0 {
		return nil, nil
	}

	return []types.EntityRecord{entity}, nil
}

// dockerfileData collects all instruction details during the walk.
type dockerfileData struct {
	// stageImages is the list of base-image names (one per FROM).
	stageImages []string
	// stageAliases mirrors stageImages with the AS alias (empty if no alias).
	stageAliases []string
	// aliasToImage maps AS alias → base-image for --from resolution.
	aliasToImage map[string]string

	runCommands      []string
	copyInstructions []string
	exposedPorts     []string
	envVars          []string
	buildArgs        []string
	entrypoints      []string

	// relationships accumulated across all instructions.
	relationships []types.RelationshipRecord
}

// buildDockerfileEntity walks the CST and returns a single file-level entity.
func buildDockerfileEntity(root ts.Node, file extractor.FileInput) (types.EntityRecord, int) {
	if root == nil {
		return types.EntityRecord{}, 0
	}

	d := &dockerfileData{aliasToImage: map[string]string{}}
	currentStage := ""

	for i := range root.ChildCount() {
		node := root.Child(int(i))
		if node == nil {
			continue
		}
		switch node.Type() {
		case "from_instruction":
			collectFrom(node, file, &currentStage, d)
		case "run_instruction":
			collectRun(node, file, d)
		case "copy_instruction":
			collectCopy(node, file, currentStage, d)
		case "add_instruction":
			collectAdd(node, file, d)
		case "expose_instruction":
			collectExpose(node, file, d)
		case "env_instruction":
			collectEnv(node, file, d)
		case "arg_instruction":
			collectArg(node, file, d)
		case "entrypoint_instruction":
			collectEntrypointOrCmd(node, file, "ENTRYPOINT", d)
		case "cmd_instruction":
			collectEntrypointOrCmd(node, file, "CMD", d)
		}
	}

	stageCount := len(d.stageImages)
	if stageCount == 0 {
		return types.EntityRecord{}, 0
	}

	// Build CONTAINS edges (file → stage structural-refs).
	for _, img := range d.stageImages {
		d.relationships = append(d.relationships, types.RelationshipRecord{
			ToID: extractor.BuildOperationStructuralRef("dockerfile", file.Path, img),
			Kind: "CONTAINS",
		})
	}

	// Filename for display.
	name := file.Path
	if slash := strings.LastIndexByte(file.Path, '/'); slash >= 0 {
		name = file.Path[slash+1:]
	}

	props := buildProperties(d)

	return types.EntityRecord{
		Name:          name,
		Kind:          "SCOPE.Component",
		Subtype:       "dockerfile",
		SourceFile:    file.Path,
		Language:      "dockerfile",
		Relationships: d.relationships,
		Properties:    props,
		QualityScore:  0.8,
	}, stageCount
}

// buildProperties serialises all collected instruction data into the flat
// map[string]string properties bag.
func buildProperties(d *dockerfileData) map[string]string {
	props := map[string]string{}

	if len(d.stageImages) > 0 {
		props["stages"] = strings.Join(d.stageImages, ",")
	}
	if len(d.stageAliases) > 0 {
		aliases := make([]string, len(d.stageAliases))
		for i, a := range d.stageAliases {
			if a == "" {
				aliases[i] = d.stageImages[i]
			} else {
				aliases[i] = a
			}
		}
		props["stage_aliases"] = strings.Join(aliases, ",")
	}
	if len(d.runCommands) > 0 {
		if b, err := json.Marshal(d.runCommands); err == nil {
			props["run_commands"] = string(b)
		}
	}
	if len(d.copyInstructions) > 0 {
		if b, err := json.Marshal(d.copyInstructions); err == nil {
			props["copy_instructions"] = string(b)
		}
	}
	if len(d.exposedPorts) > 0 {
		props["exposed_ports"] = strings.Join(d.exposedPorts, ",")
	}
	if len(d.envVars) > 0 {
		props["env_vars"] = strings.Join(d.envVars, ",")
	}
	if len(d.buildArgs) > 0 {
		props["build_args"] = strings.Join(d.buildArgs, ",")
	}
	if len(d.entrypoints) > 0 {
		props["entrypoint"] = strings.Join(d.entrypoints, ";")
	}
	return props
}

// ── instruction collectors ────────────────────────────────────────────────────

// collectFrom processes a FROM instruction: accumulates stage metadata and
// emits an IMPORTS edge (file → base-image).
func collectFrom(node ts.Node, file extractor.FileInput, currentStage *string, d *dockerfileData) {
	spec := childByType(node, "image_spec")
	if spec == nil {
		return
	}
	nameNode := childByField(spec, "name")
	if nameNode == nil {
		return
	}
	imageName := nodeText(nameNode, file.Content)
	if imageName == "" {
		return
	}

	// Tag includes the colon prefix — strip it.
	tagNode := childByField(spec, "tag")
	if tagNode != nil {
		raw := nodeText(tagNode, file.Content)
		tag := strings.TrimPrefix(raw, ":")
		if tag != "" {
			imageName = imageName + ":" + tag
		}
	}

	// Alias (AS name).
	aliasNode := childByField(node, "as")
	alias := ""
	if aliasNode != nil {
		alias = nodeText(aliasNode, file.Content)
	}
	*currentStage = alias

	d.stageImages = append(d.stageImages, imageName)
	d.stageAliases = append(d.stageAliases, alias)
	if alias != "" {
		d.aliasToImage[alias] = imageName
	}

	// IMPORTS edge: file → base image (external dependency).
	importProps := types.Props{
		{K: "import_kind", V: "from"},
		{K: "imported_name", V: imageName},
		{K: "source_module", V: imageName},
	}
	if alias != "" {
		importProps.Set("local_name", alias)
	}
	d.relationships = append(d.relationships, types.RelationshipRecord{
		FromID:     file.Path,
		ToID:       imageName,
		Kind:       "IMPORTS",
		Properties: importProps,
	})
}

func collectRun(node ts.Node, file extractor.FileInput, d *dockerfileData) {
	sig := strings.TrimSpace(nodeText(node, file.Content))
	if sig != "" {
		d.runCommands = append(d.runCommands, sig)
	}
}

func collectCopy(node ts.Node, file extractor.FileInput, currentStage string, d *dockerfileData) {
	sig := strings.TrimSpace(nodeText(node, file.Content))
	if sig != "" {
		d.copyInstructions = append(d.copyInstructions, sig)
	}
	// Check for --from=<stage> and emit a USES edge.
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil || ch.Type() != "param" {
			continue
		}
		raw := nodeText(ch, file.Content)
		eq := strings.IndexByte(raw, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(strings.TrimPrefix(raw[:eq], "--"))
		if key != "from" {
			continue
		}
		stageRef := strings.TrimSpace(raw[eq+1:])
		if stageRef == "" {
			continue
		}
		target := stageRef
		if img, ok := d.aliasToImage[stageRef]; ok {
			target = img
		}
		// #6367 — FromID is left EMPTY on purpose, matching the CONTAINS edges
		// built in buildDockerfileEntity. The record is attached to the single
		// file-level SCOPE.Component, so graph assembly stamps that record's
		// own entity id (cmd/grafel/index.go, relRecordToGraphRel in
		// internal/extractors/incremental.go). The previous `FromID: file.Path`
		// resolved only when the path WAS its own basename, because the
		// component's Name is the basename: measured 3 of 3 DANGLING at
		// "services/api/Dockerfile", and correct only BY ACCIDENT at a root
		// "Dockerfile". `Dockerfile` is also absent from sourceFileExtensions
		// (internal/resolve/refs.go), so the dangle counted as
		// DispositionBugExtractor rather than being masked as
		// DispositionDynamic. See TestDockerfile_UsesAndContainsAnchoredOnOwner.
		// The FROM IMPORTS edge keeps its file-path FromID (#120).
		d.relationships = append(d.relationships, types.RelationshipRecord{
			ToID: extractor.BuildOperationStructuralRef("dockerfile", file.Path, target),
			Kind: "USES",
			Properties: types.Props{
				{K: "from_stage", V: stageRef},
			},
		})
		_ = currentStage // stage context retained for future enrichment
	}
}

func collectAdd(node ts.Node, file extractor.FileInput, d *dockerfileData) {
	sig := strings.TrimSpace(nodeText(node, file.Content))
	if sig != "" {
		d.copyInstructions = append(d.copyInstructions, sig)
	}
}

func collectExpose(node ts.Node, file extractor.FileInput, d *dockerfileData) {
	portNode := childByType(node, "expose_port")
	if portNode == nil {
		return
	}
	port := strings.TrimSpace(nodeText(portNode, file.Content))
	if port != "" {
		d.exposedPorts = append(d.exposedPorts, port)
	}
}

func collectEnv(node ts.Node, file extractor.FileInput, d *dockerfileData) {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil || ch.Type() != "env_pair" {
			continue
		}
		keyNode := childByField(ch, "name")
		if keyNode == nil {
			continue
		}
		key := strings.TrimSpace(nodeText(keyNode, file.Content))
		if key != "" {
			d.envVars = append(d.envVars, key)
		}
	}
}

func collectArg(node ts.Node, file extractor.FileInput, d *dockerfileData) {
	nameNode := childByField(node, "name")
	if nameNode == nil {
		return
	}
	name := strings.TrimSpace(nodeText(nameNode, file.Content))
	if name != "" {
		d.buildArgs = append(d.buildArgs, name)
	}
}

func collectEntrypointOrCmd(node ts.Node, file extractor.FileInput, instruction string, d *dockerfileData) {
	sig := strings.TrimSpace(nodeText(node, file.Content))
	if sig == "" {
		sig = instruction
	}
	d.entrypoints = append(d.entrypoints, sig)
}

// ── tree-sitter helpers ───────────────────────────────────────────────────────

// nodeText returns the UTF-8 text span of a node in the source.
func nodeText(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

// childByField returns the first child with the given field name.
func childByField(node ts.Node, field string) ts.Node {
	for i := range node.ChildCount() {
		if node.FieldNameForChild(int(i)) == field {
			return node.Child(int(i))
		}
	}
	return nil
}

// childByType returns the first child with the given node type.
func childByType(node ts.Node, t string) ts.Node {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && ch.Type() == t {
			return ch
		}
	}
	return nil
}
