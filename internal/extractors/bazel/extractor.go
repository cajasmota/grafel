// Package bazel — see parser.go for package-level documentation.
package bazel

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/types"
)

// maxBuildFileBytes caps the bytes we read from any single BUILD file.
// Pathologically large generated BUILD files won't stall the indexer.
const maxBuildFileBytes = 512 * 1024 // 512 KiB

// buildFilenames is the set of recognised BUILD file basenames.
var buildFilenames = map[string]bool{
	"BUILD":       true,
	"BUILD.bazel": true,
}

// workspaceFilenames identifies repo-root WORKSPACE markers. Presence of
// any of these files signals a Bazel workspace root (used for diagnostics).
var workspaceFilenames = map[string]bool{
	"WORKSPACE":       true,
	"WORKSPACE.bazel": true,
	"MODULE.bazel":    true,
}

// IsBuildFile reports whether the given relative path is a BUILD or
// BUILD.bazel file that this extractor should process.
func IsBuildFile(relPath string) bool {
	return buildFilenames[filepath.Base(relPath)]
}

// IsWorkspaceFile reports whether the given relative path is a Bazel
// WORKSPACE/MODULE file.
func IsWorkspaceFile(relPath string) bool {
	return workspaceFilenames[filepath.Base(relPath)]
}

// Discover walks the supplied file list, parses every BUILD/BUILD.bazel file,
// and returns:
//
//   - entities: one SCOPE.Config entity per BUILD file + one SCOPE.Component
//     (subtype="bazel_target") per rule found.
//   - rels:     BAZEL_DEPENDS_ON edges between target entities.
//
// repoRoot is the absolute filesystem path of the repo being indexed.
// files is the list of relative paths (same list the rest of the indexer sees).
//
// Failure is per-file and non-fatal: a parse error on one BUILD file does not
// prevent the rest from being processed.
func Discover(ctx context.Context, repoRoot string, files []string) ([]types.EntityRecord, []types.RelationshipRecord, error) {
	tracer := otel.Tracer("extractor.bazel")
	ctx, span := tracer.Start(ctx, "bazel.Discover")
	defer span.End()
	_ = ctx

	var entities []types.EntityRecord
	var rels []types.RelationshipRecord

	// label → entity ID map for emitting BAZEL_DEPENDS_ON edges.
	labelToID := map[string]string{}

	// First pass: parse every BUILD file and collect rules + entities.
	type parsedBuild struct {
		sourceFile string
		rules      []Rule
	}
	var builds []parsedBuild

	for _, rel := range files {
		if !IsBuildFile(rel) {
			continue
		}
		abs := filepath.Join(repoRoot, rel)
		content, err := readBounded(abs)
		if err != nil {
			// Non-fatal: skip files that can't be read.
			continue
		}

		pkg := bazelPackage(rel)
		rules, err := ParseBUILD(content, pkg, rel)
		if err != nil {
			// Non-fatal: emit a build-file entity without rules.
			rules = nil
		}

		// Emit a SCOPE.Config entity for the BUILD file itself.
		buildEnt := buildFileEntity(rel, len(rules))
		entities = append(entities, buildEnt)

		// Emit a SCOPE.Component entity for each rule.
		for i := range rules {
			r := &rules[i]
			ent := ruleEntity(r)
			labelToID[r.Label()] = ent.ID
			entities = append(entities, ent)
		}

		builds = append(builds, parsedBuild{sourceFile: rel, rules: rules})
	}

	// Second pass: emit BAZEL_DEPENDS_ON edges now that all label→ID mappings
	// are populated (handles forward references between BUILD files).
	for _, b := range builds {
		for i := range b.rules {
			r := &b.rules[i]
			fromID := labelToID[r.Label()]
			if fromID == "" {
				continue
			}
			for _, dep := range r.Deps {
				toID, ok := labelToID[dep]
				if !ok {
					// External or out-of-scope dep: emit with the label as ToID.
					// The resolver overlay will mark these as "external dep".
					toID = externalDepID(dep)
				}
				rels = append(rels, types.RelationshipRecord{
					FromID: fromID,
					ToID:   toID,
					Kind:   RelationshipKindBazelDependsOn,
					Properties: types.Props{
						{K: "dep_label", V: dep},
						{K: "rule_kind", V: r.Kind},
						{K: "source_rule", V: r.Label()},
					},
				})
			}
		}
	}

	sort.Slice(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })
	sort.Slice(rels, func(i, j int) bool {
		if rels[i].FromID != rels[j].FromID {
			return rels[i].FromID < rels[j].FromID
		}
		return rels[i].ToID < rels[j].ToID
	})

	span.SetAttributes(
		attribute.Int("bazel_build_files", len(builds)),
		attribute.Int("bazel_entities", len(entities)),
		attribute.Int("bazel_edges", len(rels)),
	)
	return entities, rels, nil
}

// RelationshipKindBazelDependsOn is the relationship kind emitted by the
// Bazel extractor for declared build-level dependencies.
const RelationshipKindBazelDependsOn = "BAZEL_DEPENDS_ON"

// bazelPackage converts a BUILD file path to the Bazel package path.
// "services/auth/BUILD" → "services/auth"
// "BUILD" → ""
func bazelPackage(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		return ""
	}
	return dir
}

// buildFileEntity returns a SCOPE.Config entity for a BUILD file.
func buildFileEntity(sourceFile string, ruleCount int) types.EntityRecord {
	id := entityID("bazel_build", sourceFile)
	return types.EntityRecord{
		ID:         id,
		Name:       sourceFile,
		Kind:       string(types.EntityKindConfig),
		Subtype:    "bazel_build",
		SourceFile: sourceFile,
		Language:   "bazel",
		Properties: map[string]string{
			"format":     "starlark",
			"rule_count": fmt.Sprintf("%d", ruleCount),
		},
		QualityScore:     1.0,
		EnrichmentStatus: types.StatusPending,
	}
}

// ruleEntity returns a SCOPE.Component entity for a single Bazel build rule.
func ruleEntity(r *Rule) types.EntityRecord {
	label := r.Label()
	id := entityID("bazel_target", label)
	return types.EntityRecord{
		ID:         id,
		Name:       label,
		Kind:       string(types.EntityKindComponent),
		Subtype:    "bazel_target",
		SourceFile: r.SourceFile,
		StartLine:  r.StartLine,
		Language:   "bazel",
		Properties: map[string]string{
			"bazel_package": r.Package,
			"label":         label,
			"rule_kind":     r.Kind,
			"target_name":   r.Name,
		},
		QualityScore:     1.0,
		EnrichmentStatus: types.StatusPending,
	}
}

// entityID returns a deterministic 16-char hex ID from a namespace + key.
func entityID(ns, key string) string {
	h := sha256.New()
	h.Write([]byte("bazel\x00" + ns + "\x00" + key))
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// externalDepID returns a stable synthetic ID for an external or
// out-of-scope dependency label (e.g. "@maven//:guava").
func externalDepID(label string) string {
	return entityID("bazel_ext_dep", label)
}

// readBounded reads at most maxBuildFileBytes from path.
func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, maxBuildFileBytes)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

// TargetLabel normalises a raw label string into a canonical "//pkg:name" form.
// Short-form ":name" labels require the caller to supply pkg.
// Returns the label unchanged if it is already absolute or external.
func TargetLabel(raw, pkg string) string {
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "@") {
		return raw
	}
	if strings.HasPrefix(raw, ":") {
		return fmt.Sprintf("//%s%s", pkg, raw)
	}
	return raw
}
