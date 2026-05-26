// Package clojure implements a regex-based extractor for Clojure source files.
//
// Extracted entities:
//   - defn / defn-      → Kind="SCOPE.Operation", Subtype="function"
//   - defrecord / defprotocol / deftype / defmulti / definterface
//     → Kind="SCOPE.Component", Subtype="class"
//   - ns                → Kind="SCOPE.Component", Subtype="namespace"
//   - imported namespaces (:require / :use / :import)
//     → Kind="SCOPE.Component" stub carrying an IMPORTS relationship
//
// Issue #366 — relationship parity with java/kotlin/scala:
//
//   - IMPORTS edges are emitted from file.Path → imported namespace for
//     every entry in (:require ...), (:use ...) and (:import ...) of the
//     enclosing (ns ...) form. Properties carry local_name /
//     source_module / imported_name (and wildcard="1" for :refer :all
//     and :use).
//   - CALLS edges are attached to each defn / defn- entity, one per
//     unique (callee args ...) form discovered inside its body. Special
//     forms (let, if, fn, do, ...) and self-recursion are filtered out
//     to match the java/kotlin extractor dedup semantics.
//   - CONTAINS edges are attached from the namespace (ns ...) component
//     to every defn / defn- / defrecord / deftype / defprotocol /
//     defmulti / definterface declared at the top level, using the
//     canonical Format A structural-ref
//     (`scope:operation:method:clojure:<file>:<name>`) for operations and
//     a bare-name reference for nested components.
//
// No tree-sitter grammar for Clojure is bundled in smacker/go-tree-sitter,
// so this extractor parses Clojure with regular expressions plus a
// hand-rolled paren walker. Registers itself via init() and is imported
// by registry_gen.go.
package clojure

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("clojure", &Extractor{})
}

// Extractor implements extractor.Extractor for Clojure.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "clojure" }

// Patterns mirror Python ClojureParser.
var (
	// (defn name [...] ...) or (defn- name ...)
	defnRE = regexp.MustCompile(
		`(?m)^\s*\(defn-?\s+([\w\-\?!\*'+]+)\s*(?:\[[^\]]*\]|\()`,
	)
	// Top-level type declarations.
	deftypeRE = regexp.MustCompile(
		`(?m)^\s*\((?:defrecord|defprotocol|deftype|defmulti|definterface)\s+([\w\-\?!\*'+]+)`,
	)
	// Namespace form: (ns my.app ...)
	nsRE = regexp.MustCompile(
		`(?m)^\s*\(ns\s+([\w\-\.]+)`,
	)
	// :require entry shapes:
	//   [foo.bar :as fb]
	//   [foo.bar :refer [a b]]
	//   [foo.bar :refer :all]
	//   foo.bar           (bare symbol form)
	requireVecRE = regexp.MustCompile(
		`\[\s*([\w\-\./]+)\s*(?::as\s+[\w\-]+|:refer\s+(?:\[[^\]]*\]|:all))?\s*\]`,
	)
	// :import entry shapes:
	//   java.util.Date                 (bare symbol)
	//   [java.util Date Calendar]     (vector form)
	importVecRE = regexp.MustCompile(
		`\[\s*([\w\-\./]+)\s+([^\]]+)\]`,
	)
	bareSymRE = regexp.MustCompile(`[\w\-\.]+`)
)

// clojureSpecialForms lists Clojure forms that look like calls to a
// bare-regex walker but are not actual function invocations. Matches the
// Python extractor's drop list and the kotlin/scala keyword-stop set.
var clojureSpecialForms = map[string]bool{
	"let": true, "let*": true, "letfn": true, "loop": true, "recur": true,
	"if": true, "if-not": true, "if-let": true, "if-some": true,
	"when": true, "when-not": true, "when-let": true, "when-some": true,
	"cond": true, "condp": true, "case": true,
	"do": true, "doseq": true, "dotimes": true, "doall": true, "dorun": true,
	"for": true, "while": true,
	"try": true, "catch": true, "finally": true, "throw": true,
	"fn": true, "fn*": true, "quote": true, "var": true, "set!": true,
	"new": true, "the-ns": true,
	"and": true, "or": true, "not": true,
	"def": true, "defn": true, "defn-": true, "defmacro": true,
	"defrecord": true, "deftype": true, "defprotocol": true,
	"defmulti": true, "defmethod": true, "definterface": true,
	"ns": true, ":require": true, ":use": true, ":import": true,
	"->": true, "->>": true, "some->": true, "some->>": true,
	"as->": true, "cond->": true, "cond->>": true,
	"comment": true, "declare": true,
}

// Extract processes the Clojure source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractClojure(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "clojure")
	extractor.TagEntitiesLanguage(out, "clojure")
	return out, nil
}

func extractClojure(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// 1. Namespace component (if present) + IMPORTS edges from its body.
	nsName, nsForm, nsStartByte := findNsForm(src)
	imports := collectImports(nsForm)
	importEntities := buildImportEntities(filePath, imports)

	var nsRec *types.EntityRecord
	if nsName != "" {
		startLine := strings.Count(src[:nsStartByte], "\n") + 1
		endLine := findFormEnd(src, nsStartByte)
		nsRec = &types.EntityRecord{
			Name:               nsName,
			Kind:               "SCOPE.Component",
			Subtype:            "namespace",
			SourceFile:         filePath,
			Language:           "clojure",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "(ns " + nsName + ")",
			EnrichmentRequired: false,
		}
	}

	importNames := make([]string, 0, len(imports))
	for _, im := range imports {
		importNames = append(importNames, im.module)
	}
	importsCSV := strings.Join(importNames, ",")

	// 2. defn / defn- → functions, with CALLS edges from each body.
	type opOffset struct {
		idx  int    // index into entities slice
		name string // entity name (for CONTAINS structural-ref)
	}
	var ops []opOffset

	for _, m := range defnRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findFormEnd(src, m[0])
		body := extractFormBody(src, m[0])
		calls := collectCalls(body, name)

		rec := types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            "function",
			SourceFile:         filePath,
			Language:           "clojure",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "(defn " + name + " [...])",
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": importsCSV,
			},
			Relationships: calls,
		}
		ops = append(ops, opOffset{idx: len(entities), name: name})
		entities = append(entities, rec)
	}

	// 3. defrecord / defprotocol / deftype → class-like components.
	type compOffset struct {
		idx  int
		name string
	}
	var comps []compOffset

	for _, m := range deftypeRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findFormEnd(src, m[0])

		rec := types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Component",
			Subtype:            "class",
			SourceFile:         filePath,
			Language:           "clojure",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "(defrecord " + name + " ...)",
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": importsCSV,
			},
		}
		comps = append(comps, compOffset{idx: len(entities), name: name})
		entities = append(entities, rec)
	}

	// 4. CONTAINS edges from namespace → operations + components.
	if nsRec != nil {
		for _, op := range ops {
			ref := extractor.BuildOperationStructuralRef("clojure", filePath, op.name)
			nsRec.Relationships = append(nsRec.Relationships, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
		}
		for _, cp := range comps {
			nsRec.Relationships = append(nsRec.Relationships, types.RelationshipRecord{
				ToID: cp.name,
				Kind: "CONTAINS",
			})
		}
		// Prepend namespace + imports so file readers see header first.
		head := []types.EntityRecord{*nsRec}
		head = append(head, importEntities...)
		entities = append(head, entities...)
	} else if len(importEntities) > 0 {
		entities = append(importEntities, entities...)
	}

	return entities
}

// importSpec describes one resolved import entry.
type importSpec struct {
	module     string // e.g. "clojure.string"
	localName  string // e.g. "str" (alias) or trailing segment for plain
	importedAs string // matches localName for plain/:as
	wildcard   bool
}

// findNsForm locates the first (ns NAME ...) form and returns the
// namespace name, the textual body of the form (without the outer
// parens) and the byte offset of the opening "(". Returns ("", "", -1)
// when no namespace is present.
func findNsForm(src string) (string, string, int) {
	loc := nsRE.FindStringSubmatchIndex(src)
	if loc == nil {
		return "", "", -1
	}
	// loc[0] points at the leading whitespace; find the actual "(".
	open := strings.Index(src[loc[0]:], "(")
	if open < 0 {
		return "", "", -1
	}
	abs := loc[0] + open
	// Find matching close paren.
	depth := 0
	for i := abs; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				name := src[loc[2]:loc[3]]
				body := src[abs+1 : i]
				return name, body, abs
			}
		}
	}
	return "", "", -1
}

// collectImports scans the body of an (ns ...) form for :require, :use
// and :import sections and returns a slice of importSpec entries.
func collectImports(nsBody string) []importSpec {
	if nsBody == "" {
		return nil
	}
	var out []importSpec
	// Find each section by keyword and walk paren-balanced content.
	for _, kw := range []string{":require", ":use", ":import"} {
		for _, sec := range findKeywordSections(nsBody, kw) {
			switch kw {
			case ":require":
				out = append(out, parseRequireSection(sec)...)
			case ":use":
				out = append(out, parseUseSection(sec)...)
			case ":import":
				out = append(out, parseImportSection(sec)...)
			}
		}
	}
	return out
}

// findKeywordSections returns the textual content following each
// occurrence of keyword inside body. Each returned slice is bounded by
// the next top-level keyword or end-of-body so nested vectors and
// brackets are preserved verbatim.
func findKeywordSections(body, keyword string) []string {
	var out []string
	idx := 0
	for {
		k := strings.Index(body[idx:], keyword)
		if k < 0 {
			return out
		}
		start := idx + k + len(keyword)
		// Walk forward to next sibling keyword (":require", ":use",
		// ":import", ":refer", ":as") at paren-depth 0 or to end of body.
		end := len(body)
		depth := 0
		for i := start; i < len(body); i++ {
			ch := body[i]
			switch ch {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth--
			case ':':
				if depth == 0 && i > start {
					rest := body[i:]
					if hasKWPrefix(rest, ":require") ||
						hasKWPrefix(rest, ":use") ||
						hasKWPrefix(rest, ":import") {
						end = i
						i = len(body)
					}
				}
			}
		}
		out = append(out, body[start:end])
		idx = end
	}
}

func hasKWPrefix(s, kw string) bool {
	if !strings.HasPrefix(s, kw) {
		return false
	}
	if len(s) == len(kw) {
		return true
	}
	c := s[len(kw)]
	// Keyword boundary: whitespace, paren, bracket, end of string.
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' ||
		c == '(' || c == '[' || c == '{' || c == ')' || c == ']' || c == '}'
}

// parseRequireSection handles vector-form and bare-symbol require entries.
func parseRequireSection(section string) []importSpec {
	var out []importSpec
	// Vector form: [ns :as alias] / [ns :refer [a b]] / [ns :refer :all].
	for _, m := range requireVecRE.FindAllStringSubmatch(section, -1) {
		if len(m) < 2 {
			continue
		}
		mod := m[1]
		spec := importSpec{module: mod}
		// Detect :refer :all → wildcard.
		full := m[0]
		if strings.Contains(full, ":refer :all") || strings.Contains(full, ":refer\t:all") {
			spec.wildcard = true
		}
		// Extract :as alias.
		if asIdx := strings.Index(full, ":as"); asIdx >= 0 {
			rest := strings.TrimSpace(full[asIdx+3:])
			rest = strings.TrimRight(rest, "]")
			rest = strings.TrimSpace(rest)
			if alias := firstToken(rest); alias != "" {
				spec.localName = alias
				spec.importedAs = alias
			}
		}
		if spec.localName == "" && !spec.wildcard {
			spec.localName = lastSegment(mod)
			spec.importedAs = spec.localName
		}
		out = append(out, spec)
	}
	// Strip vector forms before scanning bare symbols, so we don't
	// double-count vector-form module names.
	stripped := requireVecRE.ReplaceAllString(section, " ")
	for _, sym := range bareSymRE.FindAllString(stripped, -1) {
		if !strings.Contains(sym, ".") {
			continue
		}
		out = append(out, importSpec{
			module:     sym,
			localName:  lastSegment(sym),
			importedAs: lastSegment(sym),
		})
	}
	return out
}

// parseUseSection treats every entry as a wildcard import (matches
// Clojure's :use semantics where everything public is referred unless
// :only/:exclude refines it).
func parseUseSection(section string) []importSpec {
	var out []importSpec
	for _, m := range requireVecRE.FindAllStringSubmatch(section, -1) {
		if len(m) < 2 {
			continue
		}
		out = append(out, importSpec{module: m[1], wildcard: true})
	}
	stripped := requireVecRE.ReplaceAllString(section, " ")
	for _, sym := range bareSymRE.FindAllString(stripped, -1) {
		if !strings.Contains(sym, ".") {
			continue
		}
		out = append(out, importSpec{module: sym, wildcard: true})
	}
	return out
}

// parseImportSection handles bare symbols (java.util.Date) and vector
// forms ([java.util Date Calendar]).
func parseImportSection(section string) []importSpec {
	var out []importSpec
	// Vector form first: [pkg Class1 Class2 ...].
	for _, m := range importVecRE.FindAllStringSubmatch(section, -1) {
		if len(m) < 3 {
			continue
		}
		pkg := m[1]
		for _, cls := range bareSymRE.FindAllString(m[2], -1) {
			out = append(out, importSpec{
				module:     pkg + "." + cls,
				localName:  cls,
				importedAs: cls,
			})
		}
	}
	stripped := importVecRE.ReplaceAllString(section, " ")
	for _, sym := range bareSymRE.FindAllString(stripped, -1) {
		if !strings.Contains(sym, ".") {
			continue
		}
		out = append(out, importSpec{
			module:     sym,
			localName:  lastSegment(sym),
			importedAs: lastSegment(sym),
		})
	}
	return out
}

func firstToken(s string) string {
	for i, ch := range s {
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == ']' {
			return s[:i]
		}
	}
	return s
}

func lastSegment(dotted string) string {
	if dot := strings.LastIndexByte(dotted, '.'); dot >= 0 {
		return dotted[dot+1:]
	}
	return dotted
}

// buildImportEntities turns each importSpec into a SCOPE.Component stub
// carrying the IMPORTS edge from file.Path → module. Properties match
// the contract Python (#93) and Java (#120) emit.
func buildImportEntities(filePath string, imports []importSpec) []types.EntityRecord {
	if len(imports) == 0 {
		return nil
	}
	out := make([]types.EntityRecord, 0, len(imports))
	seen := make(map[string]bool, len(imports))
	for _, im := range imports {
		key := im.module + "|" + im.localName
		if seen[key] {
			continue
		}
		seen[key] = true

		props := map[string]string{
			"source_module": parentModule(im.module),
		}
		if im.wildcard {
			props["wildcard"] = "1"
		} else if im.localName != "" {
			props["local_name"] = im.localName
			props["imported_name"] = im.importedAs
		}
		out = append(out, types.EntityRecord{
			Name:       topSegment(im.module),
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "clojure",
			Relationships: []types.RelationshipRecord{
				{
					FromID:     filePath,
					ToID:       im.module,
					Kind:       "IMPORTS",
					Properties: props,
				},
			},
		})
	}
	return out
}

func parentModule(dotted string) string {
	if dot := strings.LastIndexByte(dotted, '.'); dot > 0 {
		return dotted[:dot]
	}
	return dotted
}

func topSegment(dotted string) string {
	if dot := strings.IndexByte(dotted, '.'); dot > 0 {
		return dotted[:dot]
	}
	return dotted
}

// extractFormBody returns the textual content (without the outer parens)
// of the (defn ...) form starting at startPos.
func extractFormBody(src string, startPos int) string {
	open := strings.Index(src[startPos:], "(")
	if open < 0 {
		return ""
	}
	abs := startPos + open
	depth := 0
	for i := abs; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[abs+1 : i]
			}
		}
	}
	return src[abs+1:]
}

// callHeadRE captures the first symbol after an opening paren, which is
// the call head in Clojure call shape (callee args ...). Symbols can
// include `-`, `?`, `!`, `*`, `'`, `+`, `/` and `.` for namespace-qualified
// invocations.
var callHeadRE = regexp.MustCompile(`\(([\w\-\?!\*'+/\.]+)`)

// collectCalls walks body, extracts every (callee ...) head, drops
// special forms / self-recursion, dedupes, and returns CALLS edges.
func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	// Strip string literals and line comments so they don't pollute the
	// scan. A naive pass is enough — false positives here are rare.
	scrubbed := stripStringsAndComments(body)
	matches := callHeadRE.FindAllStringSubmatch(scrubbed, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]types.RelationshipRecord, 0, len(matches))
	for _, m := range matches {
		head := m[1]
		if clojureSpecialForms[head] {
			continue
		}
		if head == callerName {
			continue
		}
		// Drop pure-numeric heads from things like "(0 ...)" data.
		if head == "" || (head[0] >= '0' && head[0] <= '9') {
			continue
		}
		if seen[head] {
			continue
		}
		seen[head] = true
		out = append(out, types.RelationshipRecord{
			ToID: head,
			Kind: "CALLS",
		})
	}
	return out
}

// stripStringsAndComments replaces string literals and ;-line-comments
// with spaces so the call-head scanner doesn't pick up tokens that
// happen to live inside them.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := false
	for i < len(src) {
		ch := src[i]
		if inStr {
			out[i] = ' '
			if ch == '\\' && i+1 < len(src) {
				out[i+1] = ' '
				i += 2
				continue
			}
			if ch == '"' {
				inStr = false
			}
			i++
			continue
		}
		switch ch {
		case '"':
			inStr = true
			out[i] = ' '
			i++
		case ';':
			// Comment to end of line.
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
		default:
			out[i] = ch
			i++
		}
	}
	return string(out)
}

// findFormEnd returns the line number of the closing ) for the Lisp form at startPos.
func findFormEnd(src string, startPos int) int {
	openParen := strings.Index(src[startPos:], "(")
	if openParen < 0 {
		return strings.Count(src[:startPos], "\n") + 1
	}
	abs := startPos + openParen
	depth := 0
	for i, ch := range src[abs:] {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return strings.Count(src[:abs+i], "\n") + 1
			}
		}
	}
	return strings.Count(src, "\n") + 1
}
