// Package dockerfile implements the tree-sitter–based extractor for Dockerfile source files.
//
// Extracted entities:
//   - from_instruction   → Kind="SCOPE.Component",   Subtype="stage"      (image base + optional AS alias)
//   - run_instruction    → Kind="SCOPE.Operation",   Subtype="run"
//   - copy_instruction   → Kind="SCOPE.Operation",   Subtype="copy"
//   - add_instruction    → Kind="SCOPE.Operation",   Subtype="copy"
//   - expose_instruction → Kind="SCOPE.Pattern",     Subtype="port"
//   - env_instruction    → Kind="SCOPE.Schema",      Subtype="variable"
//   - arg_instruction    → Kind="SCOPE.Schema",       Subtype="build_arg"
//   - entrypoint/cmd     → Kind="SCOPE.Operation",   Subtype="entrypoint"
//
// Multi-stage builds: each entity is tagged with the stage name (from the
// nearest preceding FROM AS alias) in the Qualifier (Properties["stage"]).
//
// Registers itself via init() and is imported by registry_gen.go.
package dockerfile

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	tsdockerfile "github.com/smacker/go-tree-sitter/dockerfile"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("dockerfile", &Extractor{})
}

// Extractor implements extractor.Extractor for Dockerfile.
type Extractor struct{}

// Language returns the canonical language key.
func (e *Extractor) Language() string { return "dockerfile" }

// Extract walks the tree-sitter CST and returns entity records.
// On nil tree or empty src, returns empty slice with nil error.
// Node parse errors are skipped — never panic.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.dockerfile")
	ctx, span := tracer.Start(ctx, "indexer.extract.dockerfile",
		trace.WithAttributes(attribute.String("language", "dockerfile")),
	)
	defer span.End()

	if file.Tree == nil || len(file.Content) == 0 {
		span.SetAttributes(
			attribute.Int("file_line_count", 0),
			attribute.Int("entity_count", 0),
			attribute.Int("stage_count", 0),
		)
		return nil, nil
	}

	// Reuse pre-parsed tree or parse inline.
	tree := file.Tree
	if tree == nil {
		parser := sitter.NewParser()
		parser.SetLanguage(tsdockerfile.GetLanguage())
		var err error
		tree, err = parser.ParseCtx(ctx, nil, file.Content)
		if err != nil {
			return nil, err
		}
	}

	lineCount := strings.Count(string(file.Content), "\n") + 1
	entities, stageCount := walkDockerfile(tree.RootNode(), file)

	span.SetAttributes(
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", len(entities)),
		attribute.Int("stage_count", stageCount),
	)
	return entities, nil
}

// walkDockerfile traverses the root node and emits EntityRecords.
// Returns entities and stage count (number of FROM instructions).
func walkDockerfile(root *sitter.Node, file extractor.FileInput) ([]types.EntityRecord, int) {
	if root == nil {
		return nil, 0
	}

	var entities []types.EntityRecord
	stageCount := 0
	currentStage := "" // alias of the current FROM stage

	for i := range root.ChildCount() {
		node := root.Child(int(i))
		if node == nil {
			continue
		}

		switch node.Type() {
		case "from_instruction":
			if rec, ok := buildFrom(node, file, &currentStage); ok {
				entities = append(entities, rec)
				stageCount++
			}

		case "run_instruction":
			if rec, ok := buildRun(node, file, currentStage); ok {
				entities = append(entities, rec)
			}

		case "copy_instruction":
			if rec, ok := buildCopyLike(node, file, currentStage, "COPY"); ok {
				entities = append(entities, rec)
			}

		case "add_instruction":
			if rec, ok := buildCopyLike(node, file, currentStage, "ADD"); ok {
				entities = append(entities, rec)
			}

		case "expose_instruction":
			if rec, ok := buildExpose(node, file, currentStage); ok {
				entities = append(entities, rec)
			}

		case "env_instruction":
			recs := buildEnv(node, file, currentStage)
			entities = append(entities, recs...)

		case "arg_instruction":
			if rec, ok := buildArg(node, file, currentStage); ok {
				entities = append(entities, rec)
			}

		case "entrypoint_instruction":
			if rec, ok := buildEntrypointOrCmd(node, file, currentStage, "ENTRYPOINT"); ok {
				entities = append(entities, rec)
			}

		case "cmd_instruction":
			if rec, ok := buildEntrypointOrCmd(node, file, currentStage, "CMD"); ok {
				entities = append(entities, rec)
			}
		}
	}

	return entities, stageCount
}

// nodeText returns the UTF-8 text span of a node in the source.
func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

// childByField returns the first child with the given field name.
func childByField(node *sitter.Node, field string) *sitter.Node {
	for i := range node.ChildCount() {
		if node.FieldNameForChild(int(i)) == field {
			return node.Child(int(i))
		}
	}
	return nil
}

// childByType returns the first child with the given node type.
func childByType(node *sitter.Node, t string) *sitter.Node {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && ch.Type() == t {
			return ch
		}
	}
	return nil
}

// stageProps builds the shared properties map for an entity in a stage.
func stageProps(stage string) map[string]string {
	if stage == "" {
		return nil
	}
	return map[string]string{"stage": stage}
}

// buildFrom builds a SCOPE.Component entity for a FROM instruction.
// Updates currentStage to the AS alias (if any).
func buildFrom(node *sitter.Node, file extractor.FileInput, currentStage *string) (types.EntityRecord, bool) {
	spec := childByType(node, "image_spec")
	if spec == nil {
		return types.EntityRecord{}, false
	}
	nameNode := childByField(spec, "name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	imageName := nodeText(nameNode, file.Content)
	if imageName == "" {
		return types.EntityRecord{}, false
	}

	// Tag includes the colon prefix — strip it.
	tagNode := childByField(spec, "tag")
	tag := ""
	if tagNode != nil {
		raw := nodeText(tagNode, file.Content)
		tag = strings.TrimPrefix(raw, ":")
	}
	if tag != "" {
		imageName = imageName + ":" + tag
	}

	// Alias (AS name).
	aliasNode := childByField(node, "as")
	alias := ""
	if aliasNode != nil {
		alias = nodeText(aliasNode, file.Content)
	}
	*currentStage = alias

	props := map[string]string{}
	if alias != "" {
		props["alias"] = alias
	}
	if tag != "" {
		props["tag"] = tag
	}

	return types.EntityRecord{
		Name:         imageName,
		Kind:         "SCOPE.Component",
		Subtype:      "stage",
		SourceFile:   file.Path,
		Language:     "dockerfile",
		StartLine:    int(node.StartPoint().Row) + 1,
		EndLine:      int(node.EndPoint().Row) + 1,
		Signature:    "FROM " + imageName,
		Properties:   props,
		QualityScore: 0.8,
	}, true
}

// buildRun builds a SCOPE.Operation entity for a RUN instruction.
func buildRun(node *sitter.Node, file extractor.FileInput, stage string) (types.EntityRecord, bool) {
	// Extract first token of the shell command.
	name := "RUN"
	sc := childByType(node, "shell_command")
	if sc != nil {
		sf := childByType(sc, "shell_fragment")
		if sf != nil {
			raw := strings.TrimSpace(nodeText(sf, file.Content))
			if raw != "" {
				// First token: take up to first space or &&.
				tok := raw
				if idx := strings.IndexAny(raw, " \t&|;"); idx > 0 {
					tok = raw[:idx]
				}
				if tok != "" {
					name = tok
				}
			}
		}
	}

	props := stageProps(stage)
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Operation",
		Subtype:      "run",
		SourceFile:   file.Path,
		Language:     "dockerfile",
		StartLine:    int(node.StartPoint().Row) + 1,
		EndLine:      int(node.EndPoint().Row) + 1,
		Signature:    strings.TrimSpace(nodeText(node, file.Content)),
		Properties:   props,
		QualityScore: 0.7,
	}, true
}

// buildCopyLike builds a SCOPE.Operation entity for COPY or ADD.
func buildCopyLike(node *sitter.Node, file extractor.FileInput, stage, instruction string) (types.EntityRecord, bool) {
	var paths []string
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && ch.Type() == "path" {
			paths = append(paths, nodeText(ch, file.Content))
		}
	}

	qualifier := ""
	if len(paths) >= 2 {
		qualifier = paths[0] + " -> " + paths[len(paths)-1]
	} else if len(paths) == 1 {
		qualifier = paths[0]
	}

	props := stageProps(stage)
	if qualifier != "" {
		if props == nil {
			props = map[string]string{}
		}
		props["qualifier"] = qualifier
	}

	return types.EntityRecord{
		Name:         instruction,
		Kind:         "SCOPE.Operation",
		Subtype:      "copy",
		SourceFile:   file.Path,
		Language:     "dockerfile",
		StartLine:    int(node.StartPoint().Row) + 1,
		EndLine:      int(node.EndPoint().Row) + 1,
		Signature:    strings.TrimSpace(nodeText(node, file.Content)),
		Properties:   props,
		QualityScore: 0.7,
	}, true
}

// buildExpose builds a SCOPE.Pattern entity for an EXPOSE instruction.
func buildExpose(node *sitter.Node, file extractor.FileInput, stage string) (types.EntityRecord, bool) {
	portNode := childByType(node, "expose_port")
	if portNode == nil {
		return types.EntityRecord{}, false
	}
	port := strings.TrimSpace(nodeText(portNode, file.Content))
	if port == "" {
		return types.EntityRecord{}, false
	}

	props := stageProps(stage)
	return types.EntityRecord{
		Name:         port,
		Kind:         "SCOPE.Pattern",
		Subtype:      "port",
		SourceFile:   file.Path,
		Language:     "dockerfile",
		StartLine:    int(node.StartPoint().Row) + 1,
		EndLine:      int(node.EndPoint().Row) + 1,
		Signature:    "EXPOSE " + port,
		Properties:   props,
		QualityScore: 0.75,
	}, true
}

// buildEnv builds SCOPE.Schema entities for each key in an ENV instruction.
func buildEnv(node *sitter.Node, file extractor.FileInput, stage string) []types.EntityRecord {
	var recs []types.EntityRecord
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
		if key == "" {
			continue
		}
		props := stageProps(stage)
		recs = append(recs, types.EntityRecord{
			Name:         key,
			Kind:         "SCOPE.Schema",
			Subtype:      "variable",
			SourceFile:   file.Path,
			Language:     "dockerfile",
			StartLine:    int(node.StartPoint().Row) + 1,
			EndLine:      int(node.EndPoint().Row) + 1,
			Signature:    "ENV " + key,
			Properties:   props,
			QualityScore: 0.65,
		})
	}
	return recs
}

// buildArg builds a SCOPE.Schema entity for an ARG instruction.
func buildArg(node *sitter.Node, file extractor.FileInput, stage string) (types.EntityRecord, bool) {
	nameNode := childByField(node, "name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := strings.TrimSpace(nodeText(nameNode, file.Content))
	if name == "" {
		return types.EntityRecord{}, false
	}

	props := stageProps(stage)
	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.Schema",
		Subtype:      "build_arg",
		SourceFile:   file.Path,
		Language:     "dockerfile",
		StartLine:    int(node.StartPoint().Row) + 1,
		EndLine:      int(node.EndPoint().Row) + 1,
		Signature:    "ARG " + name,
		Properties:   props,
		QualityScore: 0.65,
	}, true
}

// buildEntrypointOrCmd builds a SCOPE.Operation entity for ENTRYPOINT or CMD.
func buildEntrypointOrCmd(node *sitter.Node, file extractor.FileInput, stage, instruction string) (types.EntityRecord, bool) {
	props := stageProps(stage)
	return types.EntityRecord{
		Name:         instruction,
		Kind:         "SCOPE.Operation",
		Subtype:      "entrypoint",
		SourceFile:   file.Path,
		Language:     "dockerfile",
		StartLine:    int(node.StartPoint().Row) + 1,
		EndLine:      int(node.EndPoint().Row) + 1,
		Signature:    strings.TrimSpace(nodeText(node, file.Content)),
		Properties:   props,
		QualityScore: 0.8,
	}, true
}
