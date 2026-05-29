// Package task implements a build-system extractor for Task
// (taskfile.dev) — a Go-inspired, cross-platform task runner whose tasks are
// declared in a Taskfile.yml.
//
// A Taskfile declares named tasks under a top-level `tasks:` map. Each task
// may declare:
//
//   - deps:  a list of prerequisite tasks (run before the task's cmds),
//   - cmds:  a list of shell commands or { task: <name> } sub-task calls.
//
// Both forms create an inter-task dependency. A Taskfile may also pull in
// other Taskfiles through a top-level `includes:` map; each include is a
// namespace whose value is either a path string or a mapping with a
// `taskfile:` key.
//
// This extractor parses the Taskfile YAML and emits:
//
//   - one SCOPE.Component (subtype="taskfile") per Taskfile, carrying a
//     CONTAINS edge to each task and an IMPORTS edge per include
//     (target_extraction);
//   - one SCOPE.Operation (subtype="task") per declared task, carrying a
//     TASK_DEPENDS_ON edge to each task referenced via deps: or a
//     { task: <name> } command (dependency_graph).
//
// Detection mirrors build_tools.yaml: Taskfile.yml / Taskfile.yaml /
// taskfile.yml / taskfile.yaml. Failure is per-file and non-fatal.
package task

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
	"gopkg.in/yaml.v3"

	"github.com/cajasmota/archigraph/internal/types"
)

// maxTaskfileBytes caps the bytes read from any single Taskfile.
const maxTaskfileBytes = 1 << 20 // 1 MiB

// RelationshipKindTaskDependsOn is the edge kind emitted between tasks for a
// declared deps: prerequisite or a { task: <name> } command reference. It
// aliases the canonical types.RelationshipKindTaskDependsOn.
const RelationshipKindTaskDependsOn = string(types.RelationshipKindTaskDependsOn)

// taskfileBasenames is the set of recognised Taskfile basenames.
var taskfileBasenames = map[string]bool{
	"Taskfile.yml":  true,
	"Taskfile.yaml": true,
	"taskfile.yml":  true,
	"taskfile.yaml": true,
}

// IsTaskfile reports whether relPath is a Task taskfile this extractor should
// process.
func IsTaskfile(relPath string) bool {
	return taskfileBasenames[filepath.Base(relPath)]
}

// Discover walks files, parses every Taskfile, and returns task entities plus
// their TASK_DEPENDS_ON edges. repoRoot is the absolute repo path; files are
// repo-relative. Per-file parse failures are non-fatal.
func Discover(ctx context.Context, repoRoot string, files []string) ([]types.EntityRecord, []types.RelationshipRecord, error) {
	tracer := otel.Tracer("extractor.task")
	ctx, span := tracer.Start(ctx, "task.Discover")
	defer span.End()
	_ = ctx

	var entities []types.EntityRecord
	var rels []types.RelationshipRecord

	// (taskfile rel + task name) → entity ID. Dependencies are resolved
	// within the same Taskfile first; cross-file (namespaced) deps fall back
	// to a synthetic external ID.
	taskToID := map[string]string{}
	type pendingTask struct {
		rel  string
		name string
		deps []string
	}
	var allTasks []pendingTask
	var taskfileCount int

	for _, rel := range files {
		if !IsTaskfile(rel) {
			continue
		}
		abs := filepath.Join(repoRoot, rel)
		content, err := readBounded(abs)
		if err != nil {
			continue
		}
		tf, ok := ParseTaskfile(content)
		if !ok {
			continue
		}
		taskfileCount++

		fileEnt := taskfileEntity(rel, len(tf.Tasks))

		// IMPORTS — one stub edge per include namespace.
		for _, inc := range tf.Includes {
			fileEnt.Relationships = append(fileEnt.Relationships, types.RelationshipRecord{
				FromID: fileEnt.ID,
				ToID:   inc.Path,
				Kind:   "IMPORTS",
				Properties: map[string]string{
					"include_namespace": inc.Namespace,
					"taskfile_path":     inc.Path,
				},
			})
		}

		for _, t := range tf.Tasks {
			ent := taskEntity(rel, t)
			taskToID[rel+"\x00"+t.Name] = ent.ID
			entities = append(entities, ent)
			allTasks = append(allTasks, pendingTask{rel: rel, name: t.Name, deps: t.Deps})

			fileEnt.Relationships = append(fileEnt.Relationships, types.RelationshipRecord{
				FromID: fileEnt.ID,
				ToID:   ent.ID,
				Kind:   "CONTAINS",
			})
		}
		entities = append(entities, fileEnt)
	}

	// Second pass: dependency edges.
	for _, t := range allTasks {
		fromID := taskToID[t.rel+"\x00"+t.name]
		if fromID == "" {
			continue
		}
		seen := map[string]bool{}
		for _, dep := range t.deps {
			if dep == "" || dep == t.name || seen[dep] {
				continue
			}
			seen[dep] = true
			toID, ok := taskToID[t.rel+"\x00"+dep]
			if !ok {
				// Namespaced include task (e.g. "docker:build") or a task in
				// a sibling Taskfile we did not see — synthetic external ID.
				toID = entityID("task_ext", dep)
			}
			rels = append(rels, types.RelationshipRecord{
				FromID: fromID,
				ToID:   toID,
				Kind:   RelationshipKindTaskDependsOn,
				Properties: map[string]string{
					"dep_task":    dep,
					"source_task": t.name,
				},
			})
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
		attribute.Int("task_taskfiles", taskfileCount),
		attribute.Int("task_entities", len(entities)),
		attribute.Int("task_edges", len(rels)),
	)
	return entities, rels, nil
}

// Taskfile is the parsed result of a Taskfile.
type Taskfile struct {
	Tasks    []ParsedTask
	Includes []Include
}

// ParsedTask is a single declared task with its resolved dependency names.
type ParsedTask struct {
	Name string
	Deps []string // names from deps: and { task: <name> } cmds
}

// Include is a single top-level include namespace.
type Include struct {
	Namespace string
	Path      string
}

// ParseTaskfile parses content as a Taskfile and returns its tasks and
// includes. The second return is false when the content is not a YAML mapping
// with a recognisable Taskfile shape (no tasks: and no includes:).
func ParseTaskfile(content []byte) (Taskfile, bool) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return Taskfile{}, false
	}
	doc := documentMapping(&root)
	if doc == nil {
		return Taskfile{}, false
	}

	tasksNode := mapValue(doc, "tasks")
	includesNode := mapValue(doc, "includes")
	if tasksNode == nil && includesNode == nil {
		return Taskfile{}, false
	}

	tf := Taskfile{}

	// includes: namespace → path | { taskfile: path }
	if includesNode != nil && includesNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(includesNode.Content); i += 2 {
			ns := includesNode.Content[i].Value
			val := includesNode.Content[i+1]
			path := includePath(val)
			tf.Includes = append(tf.Includes, Include{Namespace: ns, Path: path})
		}
	}

	// tasks: name → task body
	if tasksNode != nil && tasksNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(tasksNode.Content); i += 2 {
			name := tasksNode.Content[i].Value
			if name == "" {
				continue
			}
			body := tasksNode.Content[i+1]
			tf.Tasks = append(tf.Tasks, ParsedTask{
				Name: name,
				Deps: taskDeps(body),
			})
		}
	}

	return tf, true
}

// documentMapping unwraps a top-level DocumentNode to its mapping child, or
// returns the node directly if it is already a mapping.
func documentMapping(root *yaml.Node) *yaml.Node {
	n := root
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	return n
}

// mapValue returns the value node for key in a mapping node, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// includePath resolves an include value to its taskfile path. The value is
// either a scalar path or a mapping carrying a `taskfile:` key.
func includePath(val *yaml.Node) string {
	switch val.Kind {
	case yaml.ScalarNode:
		return val.Value
	case yaml.MappingNode:
		if p := mapValue(val, "taskfile"); p != nil && p.Kind == yaml.ScalarNode {
			return p.Value
		}
	}
	return ""
}

// taskDeps extracts dependency task names from a task body: every entry of
// deps: (scalar name or { task: <name> } mapping) and every { task: <name> }
// entry inside cmds:.
func taskDeps(body *yaml.Node) []string {
	if body == nil || body.Kind != yaml.MappingNode {
		return nil
	}
	var deps []string
	if d := mapValue(body, "deps"); d != nil && d.Kind == yaml.SequenceNode {
		for _, item := range d.Content {
			if n := refTaskName(item); n != "" {
				deps = append(deps, n)
			}
		}
	}
	if c := mapValue(body, "cmds"); c != nil && c.Kind == yaml.SequenceNode {
		for _, item := range c.Content {
			// Only { task: <name> } command entries are inter-task calls;
			// bare shell-string cmds are opaque and not traversed.
			if item.Kind == yaml.MappingNode {
				if n := mapValue(item, "task"); n != nil && n.Kind == yaml.ScalarNode {
					deps = append(deps, n.Value)
				}
			}
		}
	}
	return deps
}

// refTaskName resolves a deps: entry to a task name. An entry is either a
// bare scalar ("build") or a mapping with a `task:` key ({ task: build,
// vars: {...} }).
func refTaskName(item *yaml.Node) string {
	switch item.Kind {
	case yaml.ScalarNode:
		return item.Value
	case yaml.MappingNode:
		if n := mapValue(item, "task"); n != nil && n.Kind == yaml.ScalarNode {
			return n.Value
		}
	}
	return ""
}

// taskfileEntity returns a SCOPE.Component entity for a Taskfile.
func taskfileEntity(rel string, taskCount int) types.EntityRecord {
	return types.EntityRecord{
		ID:         entityID("taskfile", rel),
		Name:       rel,
		Kind:       string(types.EntityKindComponent),
		Subtype:    "taskfile",
		SourceFile: rel,
		Language:   "task",
		Properties: map[string]string{
			"build_system": "task",
			"format":       "yaml",
			"task_count":   fmt.Sprintf("%d", taskCount),
		},
		QualityScore:     1.0,
		EnrichmentStatus: types.StatusPending,
	}
}

// taskEntity returns a SCOPE.Operation entity for a single task.
func taskEntity(rel string, t ParsedTask) types.EntityRecord {
	props := map[string]string{
		"build_system": "task",
		"task_name":    t.Name,
	}
	if len(t.Deps) > 0 {
		props["dependencies"] = strings.Join(t.Deps, ",")
	}
	return types.EntityRecord{
		ID:               entityID("task", rel+"\x00"+t.Name),
		Name:             t.Name,
		Kind:             string(types.EntityKindOperation),
		Subtype:          "task",
		SourceFile:       rel,
		Language:         "task",
		Signature:        t.Name + ":",
		Properties:       props,
		QualityScore:     1.0,
		EnrichmentStatus: types.StatusPending,
	}
}

// entityID returns a deterministic 16-char hex ID from a namespace + key.
func entityID(ns, key string) string {
	h := sha256.New()
	h.Write([]byte("task\x00" + ns + "\x00" + key))
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// readBounded reads at most maxTaskfileBytes from path.
func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, maxTaskfileBytes)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}
