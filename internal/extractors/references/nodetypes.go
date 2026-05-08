// Package references implements SCOPE.Reference extraction — the second
// half of the indexer's symbol graph. Where the per-language extractors
// under internal/extractors/<language> emit SCOPE.Operation / SCOPE.Component
// entities for *declarations* (where a symbol is DEFINED), this package
// emits SCOPE.Reference entities for *usages* (where a symbol is READ,
// WRITTEN, CALLED, ANNOTATED, or PASSED AS AN ARGUMENT).
//
// The package is intentionally decoupled from the per-language extractor
// packages under internal/extractors/<language> — it takes a parsed
// tree-sitter tree as input and is driven entirely by language-keyed
// node-type tables (see nodetypes.go). A higher-level integration (the
// pipeline or a wrapping extractor) is expected to call
// ReferenceExtractor.Extract after the language extractor has already run.
//
// Two-phase architecture (MX-1048):
//
//	Phase 1 — declaration collection: traverse the AST once and populate
//	an in-memory map name -> (kind, line) for every declaration in the
//	file. This map is held in local scope for the duration of the call.
//
//	Phase 2 — reference resolution: traverse the AST a second time,
//	emitting SCOPE.Reference EntityRecord values and resolving target_kind
//	against the Phase 1 map wherever a match exists.
//
// Both phases execute inside the same Extract() call — no separate
// goroutine, no separate Lambda invocation, no disk persistence.
package references

// referenceType is the canonical set of SCOPE.Reference categories.
// Values are written into EntityRecord.Properties["reference_type"] and
// also into EntityRecord.Subtype so downstream consumers can pivot on
// either field.
const (
	RefRead           = "read"
	RefWrite          = "write"
	RefCall           = "call"
	RefType           = "type"
	RefArgument       = "argument"
	RefPropertyAccess = "property_access"
)

// AllReferenceTypes lists every SCOPE.Reference category emitted by this
// package. Tests pivot on this slice to assert full enumeration coverage.
var AllReferenceTypes = []string{
	RefRead,
	RefWrite,
	RefCall,
	RefType,
	RefArgument,
	RefPropertyAccess,
}

// langNodeTypes holds the per-language tree-sitter node type dispatch
// tables used by the two extraction phases. The fields are declared as
// map[string]struct{} sets so membership checks are O(1) without needing
// a helper function at every call site.
//
// A language with an empty set for a given category simply skips that
// category during traversal — per spec rule 8, missing grammar nodes are
// NOT treated as errors.
type langNodeTypes struct {
	// Phase-1 declaration node types. Used to seed the in-file lookup map.
	// Each entry maps a tree-sitter node type to the SCOPE.* kind it
	// should resolve to when referenced.
	declarations map[string]string

	// Phase-2 reference categories.
	identifier     map[string]struct{} // read-context identifier node types
	writeTarget    map[string]struct{} // assignment LHS / declarator node types
	memberAccess   map[string]struct{} // property_access / field_expression node types
	callExpression map[string]struct{} // call/invocation node types
	typeAnnotation map[string]struct{} // type reference node types

	// nameField is the field name that the grammar uses on call / member
	// nodes to get the target identifier. Empty when the grammar has no
	// named field and we must scan child nodes manually.
	callNameField   string
	memberNameField string
	memberObjField  string
}

// set is a convenience builder for the map[string]struct{} literals below.
func set(values ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(values))
	for _, v := range values {
		m[v] = struct{}{}
	}
	return m
}

// languageTable is the full 22-language dispatch table for the reference
// extractor. Languages not in the table are still accepted — Extract()
// simply emits zero reference entities for them rather than returning an
// error. This matches behaviour rule 8 in the MX-1048 spec.
//
// Node type names are taken from the upstream tree-sitter grammars used
// by smacker/go-tree-sitter. When a grammar exposes multiple synonyms for
// a concept (e.g. tree-sitter-java uses both "type_identifier" and
// "generic_type") all synonyms are listed.
var languageTable = map[string]*langNodeTypes{
	// ------------------------------------------------------------------
	// Python
	// ------------------------------------------------------------------
	"python": {
		declarations: map[string]string{
			"function_definition": "SCOPE.Operation",
			"class_definition":    "SCOPE.Component",
			"assignment":          "SCOPE.Variable",
		},
		identifier:      set("identifier"),
		writeTarget:     set("assignment", "augmented_assignment"),
		memberAccess:    set("attribute"),
		callExpression:  set("call"),
		typeAnnotation:  set("type", "generic_type"),
		callNameField:   "function",
		memberNameField: "attribute",
		memberObjField:  "object",
	},

	// ------------------------------------------------------------------
	// Go
	// ------------------------------------------------------------------
	"go": {
		declarations: map[string]string{
			"function_declaration":  "SCOPE.Operation",
			"method_declaration":    "SCOPE.Operation",
			"type_spec":             "SCOPE.Component",
			"var_spec":              "SCOPE.Variable",
			"const_spec":            "SCOPE.Variable",
			"short_var_declaration": "SCOPE.Variable",
		},
		identifier:      set("identifier"),
		writeTarget:     set("assignment_statement", "short_var_declaration", "inc_statement", "dec_statement"),
		memberAccess:    set("selector_expression"),
		callExpression:  set("call_expression"),
		typeAnnotation:  set("type_identifier", "qualified_type", "generic_type", "pointer_type"),
		callNameField:   "function",
		memberNameField: "field",
		memberObjField:  "operand",
	},

	// ------------------------------------------------------------------
	// JavaScript
	// ------------------------------------------------------------------
	"javascript": {
		declarations: map[string]string{
			"function_declaration": "SCOPE.Operation",
			"method_definition":    "SCOPE.Operation",
			"class_declaration":    "SCOPE.Component",
			"variable_declarator":  "SCOPE.Variable",
			"lexical_declaration":  "SCOPE.Variable",
		},
		identifier:      set("identifier"),
		writeTarget:     set("assignment_expression", "augmented_assignment_expression", "variable_declarator", "update_expression"),
		memberAccess:    set("member_expression"),
		callExpression:  set("call_expression", "new_expression"),
		callNameField:   "function",
		memberNameField: "property",
		memberObjField:  "object",
	},

	// ------------------------------------------------------------------
	// TypeScript
	// ------------------------------------------------------------------
	"typescript": {
		declarations: map[string]string{
			"function_declaration":   "SCOPE.Operation",
			"method_definition":      "SCOPE.Operation",
			"method_signature":       "SCOPE.Operation",
			"class_declaration":      "SCOPE.Component",
			"interface_declaration":  "SCOPE.Component",
			"type_alias_declaration": "SCOPE.Schema",
			"variable_declarator":    "SCOPE.Variable",
			"lexical_declaration":    "SCOPE.Variable",
			"enum_declaration":       "SCOPE.Schema",
		},
		identifier:      set("identifier", "type_identifier"),
		writeTarget:     set("assignment_expression", "augmented_assignment_expression", "variable_declarator", "update_expression"),
		memberAccess:    set("member_expression"),
		callExpression:  set("call_expression", "new_expression"),
		typeAnnotation:  set("type_identifier", "generic_type", "predefined_type"),
		callNameField:   "function",
		memberNameField: "property",
		memberObjField:  "object",
	},

	// ------------------------------------------------------------------
	// Java
	// ------------------------------------------------------------------
	"java": {
		declarations: map[string]string{
			"method_declaration":         "SCOPE.Operation",
			"constructor_declaration":    "SCOPE.Operation",
			"class_declaration":          "SCOPE.Component",
			"interface_declaration":      "SCOPE.Component",
			"enum_declaration":           "SCOPE.Schema",
			"record_declaration":         "SCOPE.Component",
			"field_declaration":          "SCOPE.Variable",
			"local_variable_declaration": "SCOPE.Variable",
		},
		identifier:      set("identifier"),
		writeTarget:     set("assignment_expression", "update_expression", "variable_declarator"),
		memberAccess:    set("field_access"),
		callExpression:  set("method_invocation", "object_creation_expression"),
		typeAnnotation:  set("type_identifier", "generic_type", "scoped_type_identifier"),
		callNameField:   "name",
		memberNameField: "field",
		memberObjField:  "object",
	},

	// ------------------------------------------------------------------
	// Kotlin
	// ------------------------------------------------------------------
	"kotlin": {
		declarations: map[string]string{
			"function_declaration": "SCOPE.Operation",
			"class_declaration":    "SCOPE.Component",
			"object_declaration":   "SCOPE.Component",
			"property_declaration": "SCOPE.Variable",
		},
		identifier:      set("simple_identifier", "identifier"),
		writeTarget:     set("assignment", "postfix_expression"),
		memberAccess:    set("navigation_expression"),
		callExpression:  set("call_expression"),
		typeAnnotation:  set("user_type", "type_identifier"),
		callNameField:   "function",
		memberNameField: "navigation_suffix",
		memberObjField:  "expression",
	},

	// ------------------------------------------------------------------
	// Ruby
	// ------------------------------------------------------------------
	"ruby": {
		declarations: map[string]string{
			"method":           "SCOPE.Operation",
			"singleton_method": "SCOPE.Operation",
			"class":            "SCOPE.Component",
			"module":           "SCOPE.Component",
			"assignment":       "SCOPE.Variable",
		},
		identifier:      set("identifier", "constant"),
		writeTarget:     set("assignment", "operator_assignment"),
		memberAccess:    set("call"),
		callExpression:  set("call", "method_call"),
		typeAnnotation:  set("constant"),
		callNameField:   "method",
		memberNameField: "method",
		memberObjField:  "receiver",
	},

	// ------------------------------------------------------------------
	// PHP
	// ------------------------------------------------------------------
	"php": {
		declarations: map[string]string{
			"function_definition":   "SCOPE.Operation",
			"method_declaration":    "SCOPE.Operation",
			"class_declaration":     "SCOPE.Component",
			"interface_declaration": "SCOPE.Component",
			"trait_declaration":     "SCOPE.Component",
			"property_declaration":  "SCOPE.Variable",
		},
		identifier:      set("name", "variable_name"),
		writeTarget:     set("assignment_expression", "augmented_assignment_expression", "update_expression"),
		memberAccess:    set("member_access_expression", "scoped_property_access_expression"),
		callExpression:  set("function_call_expression", "method_call_expression", "object_creation_expression"),
		typeAnnotation:  set("named_type", "type_name"),
		callNameField:   "function",
		memberNameField: "name",
		memberObjField:  "object",
	},

	// ------------------------------------------------------------------
	// Rust
	// ------------------------------------------------------------------
	"rust": {
		declarations: map[string]string{
			"function_item":   "SCOPE.Operation",
			"struct_item":     "SCOPE.Component",
			"enum_item":       "SCOPE.Schema",
			"trait_item":      "SCOPE.Component",
			"impl_item":       "SCOPE.Component",
			"let_declaration": "SCOPE.Variable",
			"const_item":      "SCOPE.Variable",
			"static_item":     "SCOPE.Variable",
		},
		identifier:      set("identifier"),
		writeTarget:     set("assignment_expression", "compound_assignment_expr", "let_declaration"),
		memberAccess:    set("field_expression"),
		callExpression:  set("call_expression", "macro_invocation"),
		typeAnnotation:  set("type_identifier", "generic_type", "scoped_type_identifier"),
		callNameField:   "function",
		memberNameField: "field",
		memberObjField:  "value",
	},

	// ------------------------------------------------------------------
	// C#
	// ------------------------------------------------------------------
	"csharp": {
		declarations: map[string]string{
			"method_declaration":          "SCOPE.Operation",
			"constructor_declaration":     "SCOPE.Operation",
			"class_declaration":           "SCOPE.Component",
			"interface_declaration":       "SCOPE.Component",
			"struct_declaration":          "SCOPE.Component",
			"enum_declaration":            "SCOPE.Schema",
			"field_declaration":           "SCOPE.Variable",
			"local_declaration_statement": "SCOPE.Variable",
			"property_declaration":        "SCOPE.Variable",
		},
		identifier:      set("identifier"),
		writeTarget:     set("assignment_expression", "postfix_unary_expression", "variable_declarator"),
		memberAccess:    set("member_access_expression"),
		callExpression:  set("invocation_expression", "object_creation_expression"),
		typeAnnotation:  set("identifier_name", "generic_name", "predefined_type"),
		callNameField:   "function",
		memberNameField: "name",
		memberObjField:  "expression",
	},

	// ------------------------------------------------------------------
	// Elixir
	// ------------------------------------------------------------------
	"elixir": {
		declarations: map[string]string{
			"call": "SCOPE.Operation", // def/defp are calls in elixir's grammar
		},
		identifier:     set("identifier"),
		writeTarget:    set("binary_operator"),
		memberAccess:   set("dot"),
		callExpression: set("call"),
	},

	// ------------------------------------------------------------------
	// Scala
	// ------------------------------------------------------------------
	"scala": {
		declarations: map[string]string{
			"function_definition":  "SCOPE.Operation",
			"function_declaration": "SCOPE.Operation",
			"class_definition":     "SCOPE.Component",
			"object_definition":    "SCOPE.Component",
			"trait_definition":     "SCOPE.Component",
			"val_definition":       "SCOPE.Variable",
			"var_definition":       "SCOPE.Variable",
		},
		identifier:      set("identifier"),
		writeTarget:     set("assignment_expression"),
		memberAccess:    set("field_expression"),
		callExpression:  set("call_expression", "generic_function"),
		typeAnnotation:  set("type_identifier", "generic_type"),
		callNameField:   "function",
		memberNameField: "field",
		memberObjField:  "value",
	},

	// ------------------------------------------------------------------
	// Swift
	// ------------------------------------------------------------------
	"swift": {
		declarations: map[string]string{
			"function_declaration": "SCOPE.Operation",
			"class_declaration":    "SCOPE.Component",
			"protocol_declaration": "SCOPE.Component",
			"property_declaration": "SCOPE.Variable",
		},
		identifier:      set("simple_identifier", "identifier"),
		writeTarget:     set("assignment"),
		memberAccess:    set("navigation_expression"),
		callExpression:  set("call_expression"),
		typeAnnotation:  set("type_identifier", "user_type"),
		memberNameField: "suffix",
		memberObjField:  "target",
	},

	// ------------------------------------------------------------------
	// Dart
	// ------------------------------------------------------------------
	"dart": {
		declarations: map[string]string{
			"function_signature":              "SCOPE.Operation",
			"method_signature":                "SCOPE.Operation",
			"class_definition":                "SCOPE.Component",
			"mixin_declaration":               "SCOPE.Component",
			"initialized_variable_definition": "SCOPE.Variable",
		},
		identifier:     set("identifier"),
		writeTarget:    set("assignment_expression", "postfix_expression"),
		memberAccess:   set("unconditional_assignable_selector", "selector"),
		callExpression: set("method_invocation", "function_expression_invocation"),
		typeAnnotation: set("type_identifier", "type_name"),
	},

	// ------------------------------------------------------------------
	// C++
	// ------------------------------------------------------------------
	"cpp": {
		declarations: map[string]string{
			"function_definition": "SCOPE.Operation",
			"class_specifier":     "SCOPE.Component",
			"struct_specifier":    "SCOPE.Component",
			"declaration":         "SCOPE.Variable",
		},
		identifier:      set("identifier"),
		writeTarget:     set("assignment_expression", "update_expression", "init_declarator"),
		memberAccess:    set("field_expression"),
		callExpression:  set("call_expression"),
		typeAnnotation:  set("type_identifier", "template_type", "qualified_identifier"),
		callNameField:   "function",
		memberNameField: "field",
		memberObjField:  "argument",
	},

	// ------------------------------------------------------------------
	// Clojure
	// ------------------------------------------------------------------
	"clojure": {
		declarations: map[string]string{
			"list_lit": "SCOPE.Operation", // (defn ...) is a list form
		},
		identifier:     set("sym_lit"),
		callExpression: set("list_lit"),
	},

	// ------------------------------------------------------------------
	// Groovy
	// ------------------------------------------------------------------
	"groovy": {
		declarations: map[string]string{
			"method_declaration": "SCOPE.Operation",
			"class_declaration":  "SCOPE.Component",
			"field_declaration":  "SCOPE.Variable",
		},
		identifier:     set("identifier"),
		writeTarget:    set("assignment", "update_expression"),
		memberAccess:   set("property_expression"),
		callExpression: set("method_call_expression"),
		typeAnnotation: set("type_identifier", "generic_type"),
	},

	// ------------------------------------------------------------------
	// Lua
	// ------------------------------------------------------------------
	"lua": {
		declarations: map[string]string{
			"function_declaration":       "SCOPE.Operation",
			"local_function":             "SCOPE.Operation",
			"function_definition":        "SCOPE.Operation",
			"variable_declaration":       "SCOPE.Variable",
			"local_variable_declaration": "SCOPE.Variable",
		},
		identifier:      set("identifier"),
		writeTarget:     set("assignment_statement", "variable_declaration"),
		memberAccess:    set("dot_index_expression", "method_index_expression"),
		callExpression:  set("function_call"),
		callNameField:   "name",
		memberNameField: "field",
		memberObjField:  "table",
	},

	// ------------------------------------------------------------------
	// Objective-C
	// ------------------------------------------------------------------
	"objc": {
		declarations: map[string]string{
			"method_definition":    "SCOPE.Operation",
			"class_interface":      "SCOPE.Component",
			"class_implementation": "SCOPE.Component",
			"protocol_declaration": "SCOPE.Component",
			"declaration":          "SCOPE.Variable",
		},
		identifier:     set("identifier"),
		writeTarget:    set("assignment_expression", "init_declarator"),
		memberAccess:   set("field_expression"),
		callExpression: set("message_expression", "call_expression"),
		typeAnnotation: set("type_identifier"),
	},

	// ------------------------------------------------------------------
	// Zig
	// ------------------------------------------------------------------
	"zig": {
		declarations: map[string]string{
			"function_declaration":  "SCOPE.Operation",
			"variable_declaration":  "SCOPE.Variable",
			"container_declaration": "SCOPE.Component",
		},
		identifier:     set("identifier"),
		writeTarget:    set("assignment_expression", "variable_declaration"),
		memberAccess:   set("field_access"),
		callExpression: set("call_expression"),
		typeAnnotation: set("identifier"),
	},

	// ------------------------------------------------------------------
	// Shell (bash)
	// ------------------------------------------------------------------
	"shell": {
		declarations: map[string]string{
			"function_definition": "SCOPE.Operation",
			"variable_assignment": "SCOPE.Variable",
		},
		identifier:     set("variable_name", "word"),
		writeTarget:    set("variable_assignment"),
		callExpression: set("command"),
	},

	// ------------------------------------------------------------------
	// SQL
	// ------------------------------------------------------------------
	"sql": {
		// SQL has no real lexical scope — we still emit reference edges
		// for table/column name usages.
		declarations: map[string]string{
			"create_table":    "SCOPE.Schema",
			"create_view":     "SCOPE.Schema",
			"create_function": "SCOPE.Operation",
		},
		identifier:     set("identifier", "object_reference"),
		callExpression: set("invocation"),
	},
}

// aliasMap remaps non-canonical language names onto entries in
// languageTable. Kept separate so callers can use whatever casing /
// spelling the surrounding pipeline uses without us having to duplicate
// every map entry.
var aliasMap = map[string]string{
	"golang":      "go",
	"js":          "javascript",
	"ts":          "typescript",
	"cs":          "csharp",
	"c_sharp":     "csharp",
	"c#":          "csharp",
	"cplusplus":   "cpp",
	"c++":         "cpp",
	"obj-c":       "objc",
	"objective-c": "objc",
	"objectivec":  "objc",
	"bash":        "shell",
	"sh":          "shell",
	"rb":          "ruby",
	"py":          "python",
}

// tableFor resolves a language (possibly via alias) to its node type
// table. Returns nil when the language is not in the table — callers
// treat that as "emit zero references" per rule 8.
func tableFor(language string) *langNodeTypes {
	if t, ok := languageTable[language]; ok {
		return t
	}
	if canonical, ok := aliasMap[language]; ok {
		return languageTable[canonical]
	}
	return nil
}
