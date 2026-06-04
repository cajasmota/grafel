// Package engine implements a YAML-driven framework extraction engine.
//
// It reads YAML rule files (one per framework) from an embedded filesystem,
// compiles regex patterns at init time, and applies them to source files
// to extract framework-specific entities and relationships.
//
// The engine evaluates declarative YAML rules at runtime; no dynamic
// code loading is performed.
package engine

// FrameworkRule is the top-level schema for a single framework YAML file.
// Each file describes detection patterns for one framework (e.g. gin.yaml).
type FrameworkRule struct {
	// FileConventions lists file naming conventions for this framework.
	FileConventions []FileConvention `yaml:"file_conventions"`
	// SourcePatterns maps regex patterns to entity types.
	SourcePatterns []SourcePattern `yaml:"source_patterns"`
	// RelationshipRules maps regex patterns to relationship edges.
	RelationshipRules []RelationshipRule `yaml:"relationship_rules"`
}

// FileConvention describes a file naming pattern for a framework.
type FileConvention struct {
	Pattern     string `yaml:"pattern"`
	Description string `yaml:"description"`
	// Glob is a path glob (path.Match syntax) matched against the repo-relative
	// file path. When a file matches, the convention identifies the file as
	// carrying entities of EntityType.
	Glob string `yaml:"glob"`
	// EntityType is the Kind that should be applied to entities in files
	// matching Glob (e.g. "Migration", "Model", "View").
	EntityType string `yaml:"entity_type"`
	// NameFrom controls how the entity name is derived from a matching file.
	// Supported values:
	//   "filename"   — the base filename without the .py/.js/… extension
	//   "parent_dir" — the immediate parent directory name
	//   "class_name" — entity name comes from source content (not file-driven;
	//                  file convention only tags the file type, name is left
	//                  to source_patterns to supply)
	NameFrom string `yaml:"name_from"`
}

// SourcePattern maps a regex pattern to an entity type.
// When the pattern matches source code, an entity of EntityType is created
// with the name extracted from the capture group at NameGroup.
type SourcePattern struct {
	// Pattern is a regex applied to each line (or whole file if Scope == "file").
	Pattern string `yaml:"pattern"`
	// EntityType is the Kind value for extracted entities (e.g. "Route", "Controller").
	EntityType string `yaml:"entity_type"`
	// NameGroup is the regex capture group index for the entity name.
	// 0 means use the entire match.
	NameGroup int `yaml:"name_group"`
	// Scope controls matching: "file" scans the entire file content,
	// "line" scans line-by-line.
	Scope string `yaml:"scope"`
}

// RelationshipRule maps a regex pattern to a directed relationship edge.
type RelationshipRule struct {
	// Pattern is a regex applied to source content.
	Pattern string `yaml:"pattern"`
	// SourceType is the Kind of the source entity.
	SourceType string `yaml:"source_type"`
	// TargetType is the Kind of the target entity.
	TargetType string `yaml:"target_type"`
	// Relationship is the edge type (e.g. "ROUTES_TO", "CALLS").
	Relationship string `yaml:"relationship"`
	// SourceGroup is the regex capture group index for the source entity name.
	SourceGroup int `yaml:"source_group"`
	// TargetGroup is the regex capture group index for the target entity name.
	TargetGroup int `yaml:"target_group"`
}
