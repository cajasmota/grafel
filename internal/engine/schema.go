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
	// Frameworks carries the file's detection metadata (framework identity and
	// the markers that say "this framework is actually present"). Every rule
	// file already declares it; before #6152 nothing in Go read it, so Detect
	// resolved rule sets by file.Language ALONE and every rule fired on every
	// file of that language regardless of whether the framework was there.
	Frameworks FrameworkMeta `yaml:"frameworks"`
	// FileConventions lists file naming conventions for this framework.
	FileConventions []FileConvention `yaml:"file_conventions"`
	// SourcePatterns maps regex patterns to entity types.
	SourcePatterns []SourcePattern `yaml:"source_patterns"`
	// RelationshipRules maps regex patterns to relationship edges.
	RelationshipRules []RelationshipRule `yaml:"relationship_rules"`
}

// FrameworkMeta is the `frameworks:` block of a rule file: who the framework
// is and how to tell it is present. Only the fields the engine consumes are
// modelled; the rest of the block (category, github_stars_2025, notes, …) is
// descriptive and deliberately unmapped.
type FrameworkMeta struct {
	// Name is the human framework name (e.g. "Falcon").
	Name string `yaml:"name"`
	// Detection holds the presence signals for this framework.
	Detection FrameworkDetection `yaml:"detection"`
}

// FrameworkDetection lists the signals that say a framework is in play.
type FrameworkDetection struct {
	// ImportMarkers are literal source substrings that only appear when the
	// framework is imported or used (e.g. "import falcon", "from falcon
	// import"). This is the signal source_patterns marked requires_framework
	// are gated on — it is the only detection signal available from the file
	// content Detect is handed.
	ImportMarkers []string `yaml:"import_markers"`
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
	// RequiresFramework opts this pattern into the framework-presence gate
	// (#6152). When true the pattern only fires on files that carry at least
	// one of the owning rule file's frameworks.detection.import_markers.
	//
	// Opt-IN, not opt-out, and deliberately so: most patterns are already
	// self-gating because their regex names the framework ("cherrypy.expose",
	// ".add_route("). The flag is for patterns whose regex is broad enough to
	// match plain language syntax — a bare `class Foo:` — where the framework
	// is what makes the match meaningful and nothing in the regex checks for
	// it. Defaulting every pattern to gated would suppress cross-file recall
	// for the self-gating majority.
	RequiresFramework bool `yaml:"requires_framework"`
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
	// Terminator is an optional literal string that the span BETWEEN the two
	// captures may not cross (#6666).
	//
	// Why it exists: a rule whose pattern joins two constructs across a bounded
	// `[\s\S]{0,N}?` window has a POSITIONAL window, not a structural one. When
	// the SOURCE construct repeats in a file, the first source's header pairs
	// with the SECOND source's target — one false edge plus one missing edge —
	// and `FindAllStringSubmatch` returns non-overlapping matches, so the
	// correct pairing is never even reachable. RE2 has no negative lookahead,
	// so "no intervening End Module" cannot be written in the pattern.
	//
	// When set, the join site rejects any match whose text between the END of
	// the earlier capture and the START of the later one contains this string,
	// and RESUMES the search at the rejected match's own start rather than
	// past it, so a later source can still pair with the same target.
	//
	// Deliberate limitations, each pinned by a test in
	// relationship_terminator_6666_test.go:
	//   * It is a case-sensitive byte-literal substring test, not a scope
	//     parser. `end module` does not block; `End Module` inside a `'`
	//     comment or a string literal DOES block (a false negative).
	//   * It cannot express nesting, so it suits languages whose block
	//     terminators are explicit and non-nesting (VB `End Module`). Rules
	//     whose boundary is a nesting `}` need real containment and are out of
	//     scope here.
	//   * Resumption is LINE-granular in both directions, so a second source
	//     construct on the SAME line as a previous match's end or start is not
	//     considered. This is not a free choice: resuming mid-line would let a
	//     `(?m)^` in the pattern match a position that is not a line start.
	//     See findRelationshipMatches.
	//   * A pattern using `^` WITHOUT `(?m)` must not carry a terminator; that
	//     `^` means beginning-of-text and re-slicing would make it true at
	//     every resume point.
	//   * source_group or target_group 0 is REFUSED at load in combination
	//     with a terminator: group 0 is the whole match, so the join window is
	//     empty and the guard could never fire. (source_group 0 on its own is
	//     a separate defect, #6788.)
	//
	// Empty (the default) means no guard at all: the join site is then
	// literally FindAllStringSubmatch. Opt-in per rule, asserted by
	// Test6666_TerminatorBlocksCrossModuleJoin's no-terminator control leg.
	// A terminator that never OCCURS is also behaviour-preserving, which is a
	// separate and much easier property to break — see
	// Test6666_AbsentTerminatorIsIdenticalWhenMatchEndsMidLine.
	Terminator string `yaml:"terminator"`
}
