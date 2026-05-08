package references

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// FrameworkContext is the per-file state produced by DetectFramework
// and consumed by every FrameworkTagger in the chain. It carries the
// language, a flat set of import-like strings ("django.db.models"),
// the declaration lookup map (so taggers can cross-reference decls
// when needed), and the raw source bytes for expression-level lookups.
type FrameworkContext struct {
	// Language is the canonical language name (e.g. "python", "go").
	Language string

	// Imports is the flat set of module / package paths imported by
	// the file. Tagger implementations match against this set by
	// prefix — e.g. "django.db.models" will match a tagger interested
	// in "django.db".
	Imports map[string]struct{}

	// Frameworks is the set of framework identifiers detected from
	// Imports, e.g. {"django", "fastapi"} or {"react", "nextjs"}. The
	// detection is purely prefix-based; each tagger further qualifies.
	Frameworks map[string]struct{}

	// Decls is the Phase-1 lookup map. Included so taggers can make
	// decisions based on the presence / absence of local declarations.
	Decls *declLookup

	// Source is the file content bytes. Taggers that want to inspect
	// adjacent source fragments can use this.
	Source []byte
}

// HasFramework reports whether framework was detected in the current file.
func (c FrameworkContext) HasFramework(framework string) bool {
	if c.Frameworks == nil {
		return false
	}
	_, ok := c.Frameworks[framework]
	return ok
}

// FrameworkTagger enriches a SCOPE.Reference entity with framework
// semantic information. Implementations are expected to be purely
// additive — they mutate the record in place (Properties and Tags) but
// MUST NOT change Name, Kind, or reference_type.
//
// Taggers that do not want to act on a given entity simply return
// without modifying it. The default behaviour — no framework detected,
// no tagger matched — is that the entity passes through unchanged.
type FrameworkTagger interface {
	Tag(rec *types.EntityRecord, ctx FrameworkContext)
}

// CompositeTagger invokes a chain of FrameworkTagger values in order.
// An empty CompositeTagger is a valid no-op (used by ReferenceExtractor
// when the caller does not supply a tagger).
type CompositeTagger struct {
	Taggers []FrameworkTagger
}

// Tag implements FrameworkTagger by iterating the chain.
func (c *CompositeTagger) Tag(rec *types.EntityRecord, ctx FrameworkContext) {
	if c == nil {
		return
	}
	for _, t := range c.Taggers {
		if t == nil {
			continue
		}
		t.Tag(rec, ctx)
	}
}

// DefaultTagger returns the built-in chain of framework-aware taggers
// covering the frameworks most common in the ARCHIGRAPH user base:
//
//	Python:     Django ORM
//	JavaScript: React hooks
//	Java:       Spring Data
//	Ruby:       ActiveRecord
//
// The chain is intentionally small — we extend it in follow-up stories
// as more framework custom extractors come online. Each tagger is
// language-scoped and becomes a no-op on files that did not import the
// corresponding framework.
func DefaultTagger() FrameworkTagger {
	return &CompositeTagger{
		Taggers: []FrameworkTagger{
			&DjangoORMTagger{},
			&ReactHookTagger{},
			&SpringDataTagger{},
			&ActiveRecordTagger{},
		},
	}
}

// ------------------------------------------------------------------
// DetectFramework — import scan + framework inference
// ------------------------------------------------------------------

// DetectFramework produces a FrameworkContext by collecting every
// import-shaped node from the AST and mapping well-known import
// prefixes to framework identifiers. It is called once per file by
// ReferenceExtractor.Extract and passed to every FrameworkTagger.
func DetectFramework(file extractor.FileInput, root *sitter.Node, decls *declLookup) FrameworkContext {
	ctx := FrameworkContext{
		Language:   file.Language,
		Imports:    make(map[string]struct{}),
		Frameworks: make(map[string]struct{}),
		Decls:      decls,
		Source:     file.Content,
	}
	if root == nil {
		return ctx
	}

	importNodeTypes := map[string]struct{}{
		"import_declaration":        {}, // java, go
		"import_statement":          {}, // python, javascript, typescript
		"import_from_statement":     {}, // python
		"import_spec":               {}, // go
		"use_declaration":           {}, // rust
		"using_directive":           {}, // c#
		"require":                   {}, // ruby
		"namespace_use_declaration": {}, // php
	}

	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := importNodeTypes[n.Type()]; ok {
			raw := strings.TrimSpace(nodeText(n, file.Content))
			if raw != "" {
				for _, part := range extractImportPaths(raw) {
					ctx.Imports[part] = struct{}{}
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}

	for imp := range ctx.Imports {
		if fw, ok := frameworkForImport(imp); ok {
			ctx.Frameworks[fw] = struct{}{}
		}
	}

	// Source-level framework fingerprints catch frameworks whose
	// import form is not a dedicated grammar node. Ruby's `require
	// 'active_record'` parses as a regular call rather than an import
	// node, and Groovy / Scala DSLs have similar quirks. The fingerprint
	// list is intentionally tight: only strings that are extremely
	// unlikely to appear outside a genuine framework import.
	for fingerprint, fw := range sourceFingerprints {
		if strings.Contains(string(file.Content), fingerprint) {
			ctx.Frameworks[fw] = struct{}{}
		}
	}
	return ctx
}

// sourceFingerprints maps a literal substring to a framework id. Only
// used for frameworks whose import syntax is not reliably represented
// as an import-shaped AST node.
var sourceFingerprints = map[string]string{
	"'active_record'":   "active_record",
	"\"active_record\"": "active_record",
	"ActiveRecord::":    "active_record",
}

// extractImportPaths pulls the dotted / slashed path fragments out of
// the raw text of an import statement. It is heuristic — taggers only
// care about prefix matches — but good enough for the major frameworks.
func extractImportPaths(raw string) []string {
	raw = strings.ReplaceAll(raw, "\"", "")
	raw = strings.ReplaceAll(raw, "'", "")
	raw = strings.ReplaceAll(raw, "`", "")
	raw = strings.TrimSuffix(raw, ";")
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == ',' || r == '{' || r == '}' || r == '(' || r == ')'
	})
	var out []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" || f == "import" || f == "from" || f == "use" || f == "using" || f == "require" || f == "package" || f == "as" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// frameworkPrefixes maps an import path prefix to its framework id.
// The entries below cover the frameworks the DefaultTagger chain acts
// on. Other frameworks can be added by taggers that ship their own
// detection — they simply call HasFramework with a matching key that
// they inject themselves.
var frameworkPrefixes = map[string]string{
	"django":                   "django",
	"django.db":                "django",
	"rest_framework":           "django",
	"react":                    "react",
	"react-dom":                "react",
	"next":                     "nextjs",
	"next/router":              "nextjs",
	"org.springframework.data": "spring_data",
	"org.springframework":      "spring",
	"ActiveRecord":             "active_record",
	"active_record":            "active_record",
	"activerecord":             "active_record",
}

// frameworkForImport reports the framework id for an import path.
func frameworkForImport(imp string) (string, bool) {
	for prefix, fw := range frameworkPrefixes {
		if imp == prefix || strings.HasPrefix(imp, prefix+".") || strings.HasPrefix(imp, prefix+"/") {
			return fw, true
		}
	}
	return "", false
}

// ------------------------------------------------------------------
// Tagger implementations
// ------------------------------------------------------------------

// DjangoORMTagger recognises Django QuerySet mutations on property_access
// references (.save, .delete, .update, .create, .bulk_create, .filter).
// Reads / filters are marked as "database_read via django_orm"; mutations
// are marked as "database_write via django_orm". Files without a django
// import are ignored.
type DjangoORMTagger struct{}

var djangoWriteMethods = map[string]struct{}{
	"save":             {},
	"delete":           {},
	"update":           {},
	"create":           {},
	"bulk_create":      {},
	"bulk_update":      {},
	"get_or_create":    {},
	"update_or_create": {},
}

var djangoReadMethods = map[string]struct{}{
	"filter":  {},
	"all":     {},
	"get":     {},
	"exclude": {},
	"values":  {},
	"count":   {},
}

func (t *DjangoORMTagger) Tag(rec *types.EntityRecord, ctx FrameworkContext) {
	if ctx.Language != "python" || !ctx.HasFramework("django") {
		return
	}
	if rec.Subtype != RefPropertyAccess && rec.Subtype != RefCall {
		return
	}
	target := rec.Properties["target_name"]
	if target == "" {
		return
	}
	if _, isWrite := djangoWriteMethods[target]; isWrite {
		applyFrameworkTag(rec, "database_write", "django_orm", fmt.Sprintf("Database WRITE via Django ORM at line %d on entity %s", rec.StartLine, targetEntity(rec)))
		return
	}
	if _, isRead := djangoReadMethods[target]; isRead {
		applyFrameworkTag(rec, "database_read", "django_orm", fmt.Sprintf("Database READ via Django ORM at line %d on entity %s", rec.StartLine, targetEntity(rec)))
	}
}

// ReactHookTagger recognises React hook calls (useState, useEffect, etc.)
// on call-expression references, only within files that import React.
type ReactHookTagger struct{}

var reactHookNames = map[string]struct{}{
	"useState":            {},
	"useEffect":           {},
	"useContext":          {},
	"useReducer":          {},
	"useCallback":         {},
	"useMemo":             {},
	"useRef":              {},
	"useLayoutEffect":     {},
	"useImperativeHandle": {},
}

func (t *ReactHookTagger) Tag(rec *types.EntityRecord, ctx FrameworkContext) {
	if ctx.Language != "javascript" && ctx.Language != "typescript" {
		return
	}
	if !ctx.HasFramework("react") && !ctx.HasFramework("nextjs") {
		return
	}
	if rec.Subtype != RefCall {
		return
	}
	target := rec.Properties["target_name"]
	if _, ok := reactHookNames[target]; !ok {
		return
	}
	applyFrameworkTag(rec, "react_hook", "react", fmt.Sprintf("React hook %s at line %d", target, rec.StartLine))
}

// SpringDataTagger marks Java/Kotlin repository method calls on
// Spring Data repositories as "database_read" or "database_write".
type SpringDataTagger struct{}

func (t *SpringDataTagger) Tag(rec *types.EntityRecord, ctx FrameworkContext) {
	if ctx.Language != "java" && ctx.Language != "kotlin" {
		return
	}
	if !ctx.HasFramework("spring_data") && !ctx.HasFramework("spring") {
		return
	}
	if rec.Subtype != RefCall {
		return
	}
	target := rec.Properties["target_name"]
	switch {
	case strings.HasPrefix(target, "findBy"), strings.HasPrefix(target, "findAll"),
		strings.HasPrefix(target, "findOne"), strings.HasPrefix(target, "count"),
		strings.HasPrefix(target, "existsBy"):
		applyFrameworkTag(rec, "database_read", "spring_data",
			fmt.Sprintf("Database READ via Spring Data at line %d on entity %s", rec.StartLine, targetEntity(rec)))
	case strings.HasPrefix(target, "save"), strings.HasPrefix(target, "delete"),
		strings.HasPrefix(target, "update"), strings.HasPrefix(target, "insert"):
		applyFrameworkTag(rec, "database_write", "spring_data",
			fmt.Sprintf("Database WRITE via Spring Data at line %d on entity %s", rec.StartLine, targetEntity(rec)))
	}
}

// ActiveRecordTagger recognises Ruby ActiveRecord CRUD methods.
type ActiveRecordTagger struct{}

var activeRecordWriteMethods = map[string]struct{}{
	"save":        {},
	"save!":       {},
	"create":      {},
	"create!":     {},
	"update":      {},
	"update!":     {},
	"destroy":     {},
	"destroy_all": {},
}

var activeRecordReadMethods = map[string]struct{}{
	"find":    {},
	"find_by": {},
	"where":   {},
	"all":     {},
	"first":   {},
	"last":    {},
	"count":   {},
	"exists?": {},
}

func (t *ActiveRecordTagger) Tag(rec *types.EntityRecord, ctx FrameworkContext) {
	if ctx.Language != "ruby" || !ctx.HasFramework("active_record") {
		return
	}
	if rec.Subtype != RefCall && rec.Subtype != RefPropertyAccess {
		return
	}
	target := rec.Properties["target_name"]
	if _, ok := activeRecordWriteMethods[target]; ok {
		applyFrameworkTag(rec, "database_write", "active_record",
			fmt.Sprintf("Database WRITE via ActiveRecord at line %d on entity %s", rec.StartLine, targetEntity(rec)))
		return
	}
	if _, ok := activeRecordReadMethods[target]; ok {
		applyFrameworkTag(rec, "database_read", "active_record",
			fmt.Sprintf("Database READ via ActiveRecord at line %d on entity %s", rec.StartLine, targetEntity(rec)))
	}
}

// applyFrameworkTag sets the framework-semantic fields on a reference
// entity. Called by every tagger so the emitted shape is consistent.
func applyFrameworkTag(rec *types.EntityRecord, semanticTag, framework, description string) {
	if rec.Properties == nil {
		rec.Properties = make(map[string]string)
	}
	rec.Properties["framework"] = framework
	rec.Properties["semantic_tag"] = semanticTag
	rec.Properties["framework_description"] = description
	rec.Tags = appendUnique(rec.Tags, "framework:"+framework)
	rec.Tags = appendUnique(rec.Tags, "semantic:"+semanticTag)
}

// appendUnique appends v to slice only if it is not already present.
func appendUnique(slice []string, v string) []string {
	for _, existing := range slice {
		if existing == v {
			return slice
		}
	}
	return append(slice, v)
}

// targetEntity returns the receiver-qualified form of a reference name
// suitable for embedding into a human-readable framework tag string.
// Falls back to the target name alone when no receiver is present.
func targetEntity(rec *types.EntityRecord) string {
	if rec == nil {
		return ""
	}
	if recv, ok := rec.Properties["receiver"]; ok && recv != "" {
		return recv
	}
	return rec.Properties["target_name"]
}
