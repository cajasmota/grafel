// Package lisp implements a shared regex-based extractor for the Lisp family:
// Common Lisp (.lisp/.lsp/.cl), Scheme (.scm/.ss), and Racket (.rkt).
//
// All three dialects share s-expression syntax so one extractor handles them;
// a dialect parameter is derived from file extension and controls which
// form-names are matched for each construct.
//
// Extracted entities:
//   - (defun name ...) / (define (name ...) ...) → SCOPE.Operation (subtype="function")
//   - (defmacro ...) / (define-syntax ...) → SCOPE.Operation (subtype="macro")
//   - (defstruct ...) / (define-struct ...) / (struct ...) → SCOPE.Component (subtype="struct")
//   - (defclass ...) / (define-class ...) → SCOPE.Component (subtype="class")
//   - (define/contract name ...) → SCOPE.Operation (subtype="function", Racket contracts)
//   - (in-package "name") → namespace entity (subtype="namespace")
//   - (load "file") / (require ...) → IMPORTS edges
//   - Module CONTAINS top-level forms
//
// Registers itself via init() under "commonlisp", "scheme", and "racket"
// and is imported by registry_gen.go.
package lisp

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("commonlisp", &Extractor{dialect: "commonlisp"})
	extractor.Register("scheme", &Extractor{dialect: "scheme"})
	extractor.Register("racket", &Extractor{dialect: "racket"})
}

// Extractor implements extractor.Extractor for the Lisp family.
type Extractor struct {
	dialect string // "commonlisp" | "scheme" | "racket"
}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return e.dialect }

// ---------------------------------------------------------------------------
// Compiled regex patterns
// ---------------------------------------------------------------------------

var (
	// Common Lisp: (defun name (args) body)
	defunRE = regexp.MustCompile(
		`(?m)^\s*\(defun\s+([\w\-\?!\*'+<>=.]+)\s*\(`,
	)

	// Scheme/Racket: (define (name args) body)
	defineRE = regexp.MustCompile(
		`(?m)^\s*\(define\s+\(\s*([\w\-\?!\*'+<>=./]+)`,
	)

	// Scheme/Racket: (define name ...) — bare defines at top level
	defineVarRE = regexp.MustCompile(
		`(?m)^\s*\(define\s+([\w\-\?!\*'+<>=./]+)\s+`,
	)

	// Racket: (define/contract (name args) ...) or (define/contract name ...)
	defineContractRE = regexp.MustCompile(
		`(?m)^\s*\(define/contract\s+(?:\(\s*)?([\w\-\?!\*'+<>=./]+)`,
	)

	// Racket: (require/typed ...)
	requireTypedRE = regexp.MustCompile(
		`(?m)^\s*\(require/typed\s+([^\s)]+)`,
	)

	// Common Lisp: (defmacro name ...)
	defmacroRE = regexp.MustCompile(
		`(?m)^\s*\(defmacro\s+([\w\-\?!\*'+<>=.]+)\s`,
	)

	// Scheme: (define-syntax name ...)
	defineSyntaxRE = regexp.MustCompile(
		`(?m)^\s*\(define-syntax\s+([\w\-\?!\*'+<>=.]+)`,
	)

	// Common Lisp: (defstruct name ...)
	defstructRE = regexp.MustCompile(
		`(?m)^\s*\(defstruct\s+([\w\-\?!\*'+<>=.]+)`,
	)

	// Scheme: (define-struct name ...)
	defineStructRE = regexp.MustCompile(
		`(?m)^\s*\(define-struct\s+([\w\-\?!\*'+<>=./]+)`,
	)

	// Racket: (struct name ...)
	structRE = regexp.MustCompile(
		`(?m)^\s*\(struct\s+([\w\-\?!\*'+<>=./]+)`,
	)

	// Common Lisp: (defclass name ...)
	defclassRE = regexp.MustCompile(
		`(?m)^\s*\(defclass\s+([\w\-\?!\*'+<>=.]+)`,
	)

	// Scheme: (define-class name ...)
	defineClassRE = regexp.MustCompile(
		`(?m)^\s*\(define-class\s+([\w\-\?!\*'+<>=./]+)`,
	)

	// Common Lisp: (defmethod name ...)
	defmethodRE = regexp.MustCompile(
		`(?m)^\s*\(defmethod\s+([\w\-\?!\*'+<>=.]+)`,
	)

	// Common Lisp: (in-package "name") or (in-package :name)
	inPackageRE = regexp.MustCompile(
		`(?m)^\s*\(in-package\s+(?:"([^"]+)"|:?([\w\-]+))\s*\)`,
	)

	// Common Lisp / Scheme: (load "filename")
	loadRE = regexp.MustCompile(
		`(?m)^\s*\(load\s+"([^"]+)"`,
	)

	// Scheme: (require "module") or (require module-name)
	requireRE = regexp.MustCompile(
		`(?m)^\s*\(require\s+(?:"([^"]+)"|([\w\-\./]+))`,
	)

	// Racket: (require racket/list) — module paths
	requirePathRE = regexp.MustCompile(
		`(?m)^\s*\(require\s+([\w\-\./]+)`,
	)

	// Generic s-expression call: (name ...) — captures callee names
	callRE = regexp.MustCompile(
		`\(\s*([\w\-\?!\*'+<>=./]+)`,
	)
)

// lispSpecialForms are s-expression heads that are NOT function calls.
var lispSpecialForms = map[string]bool{
	// Common Lisp special operators
	"let": true, "let*": true, "letrec": true, "letrec*": true,
	"flet": true, "labels": true, "macrolet": true,
	"if": true, "cond": true, "case": true, "when": true, "unless": true,
	"and": true, "or": true, "not": true,
	"progn": true, "prog1": true, "prog2": true,
	"block": true, "return-from": true, "return": true,
	"catch": true, "throw": true, "unwind-protect": true,
	"lambda": true, "function": true,
	"setf": true, "setq": true, "set": true, "psetf": true, "psetq": true,
	"defun": true, "defmacro": true, "defstruct": true, "defclass": true,
	"defmethod": true, "defvar": true, "defparameter": true, "defconstant": true,
	"deftype": true, "declaim": true, "declare": true,
	"in-package": true, "defpackage": true,
	"load": true, "require": true, "provide": true,
	"quote": true, "quasiquote": true, "backquote": true,
	"do": true, "do*": true, "dolist": true, "dotimes": true, "loop": true,
	"with-open-file": true, "with-output-to-string": true,
	"multiple-value-bind": true, "multiple-value-list": true,
	"values": true, "nth-value": true,
	"eval-when": true, "load-time-value": true,
	"the": true, "locally": true, "symbol-macrolet": true,
	// Scheme additional
	"define": true, "define-syntax": true, "define-struct": true,
	"define-class": true, "define/contract": true,
	"begin": true, "delay": true, "force": true,
	"call-with-current-continuation": true,
	"syntax-rules": true, "syntax-case": true,
	"let-values": true, "let*-values": true,
	"receive": true, "guard": true,
	// Racket additional
	"struct": true, "require/typed": true, "provide/contract": true,
	"module": true, "module*": true, "module+": true,
	"parameterize": true, "with-syntax": true,
	"for": true, "for/list": true, "for/fold": true, "for/vector": true,
	"match": true, "match-define": true,
	"define-values": true, "let-syntax": true, "letrec-syntax": true,
}

// dialectFromPath derives the dialect from file extension.
func dialectFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".lisp", ".lsp", ".cl":
		return "commonlisp"
	case ".scm", ".ss":
		return "scheme"
	case ".rkt":
		return "racket"
	}
	return "commonlisp" // fallback
}

// Extract processes the Lisp source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	dialect := dialectFromPath(file.Path)
	out := extractLisp(string(file.Content), file.Path, dialect)
	extractor.TagRelationshipsLanguage(out, dialect)
	return out, nil
}

func extractLisp(src, filePath, dialect string) []types.EntityRecord {
	var entities []types.EntityRecord

	// Emit file-level entity.
	entities = append(entities, extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: dialect,
	}))

	// Scrub strings and comments to avoid false positives.
	scrubbed := stripLispStringsAndComments(src)

	// 1. Package/namespace (Common Lisp).
	var packageName string
	if dialect == "commonlisp" {
		if m := inPackageRE.FindStringSubmatch(scrubbed); m != nil {
			pkgName := m[1]
			if pkgName == "" {
				pkgName = strings.ToLower(m[2])
			}
			if pkgName != "" {
				packageName = pkgName
				startLine := lineOf(src, inPackageRE.FindStringIndex(scrubbed)[0])
				entities = append(entities, types.EntityRecord{
					Name:       packageName,
					Kind:       "SCOPE.Component",
					Subtype:    "namespace",
					SourceFile: filePath,
					Language:   dialect,
					StartLine:  startLine,
					EndLine:    startLine,
					Signature:  "(in-package " + packageName + ")",
				})
			}
		}
	}

	// 2. Collect imports (use original source so string literals are intact).
	imports := collectLispImports(src, dialect)
	importEntities := buildLispImportEntities(filePath, dialect, imports)
	entities = append(entities, importEntities...)

	// 3. Operations (functions, macros, methods).
	var operationNames []string
	seen := make(map[string]bool)

	// defun (Common Lisp)
	if dialect == "commonlisp" {
		for _, m := range defunRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if seen[name] {
				continue
			}
			seen[name] = true
			operationNames = append(operationNames, name)
			startLine := lineOf(src, m[0])
			sig := "(defun " + name + " ...)"
			body := extractSexprBody(src, m[0])
			endLine := startLine + strings.Count(body, "\n")
			calls := collectLispCalls(body, name, dialect)
			entities = append(entities, types.EntityRecord{
				Name:          name,
				Kind:          "SCOPE.Operation",
				Subtype:       "function",
				SourceFile:    filePath,
				Language:      dialect,
				StartLine:     startLine,
				EndLine:       endLine,
				Signature:     sig,
				Relationships: calls,
			})
		}

		// defmacro (Common Lisp)
		for _, m := range defmacroRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if seen[name] {
				continue
			}
			seen[name] = true
			operationNames = append(operationNames, name)
			startLine := lineOf(src, m[0])
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Operation",
				Subtype:    "macro",
				SourceFile: filePath,
				Language:   dialect,
				StartLine:  startLine,
				EndLine:    startLine,
				Signature:  "(defmacro " + name + " ...)",
			})
		}

		// defmethod (Common Lisp)
		for _, m := range defmethodRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if seen[name] {
				continue
			}
			seen[name] = true
			operationNames = append(operationNames, name)
			startLine := lineOf(src, m[0])
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Operation",
				Subtype:    "function",
				SourceFile: filePath,
				Language:   dialect,
				StartLine:  startLine,
				EndLine:    startLine,
				Signature:  "(defmethod " + name + " ...)",
			})
		}
	}

	// define function (Scheme/Racket)
	if dialect == "scheme" || dialect == "racket" {
		for _, m := range defineRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if seen[name] || name == "" {
				continue
			}
			seen[name] = true
			operationNames = append(operationNames, name)
			startLine := lineOf(src, m[0])
			body := extractSexprBody(src, m[0])
			endLine := startLine + strings.Count(body, "\n")
			calls := collectLispCalls(body, name, dialect)
			entities = append(entities, types.EntityRecord{
				Name:          name,
				Kind:          "SCOPE.Operation",
				Subtype:       "function",
				SourceFile:    filePath,
				Language:      dialect,
				StartLine:     startLine,
				EndLine:       endLine,
				Signature:     "(define (" + name + " ...) ...)",
				Relationships: calls,
			})
		}

		// define-syntax (Scheme/Racket)
		for _, m := range defineSyntaxRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if seen[name] {
				continue
			}
			seen[name] = true
			operationNames = append(operationNames, name)
			startLine := lineOf(src, m[0])
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Operation",
				Subtype:    "macro",
				SourceFile: filePath,
				Language:   dialect,
				StartLine:  startLine,
				EndLine:    startLine,
				Signature:  "(define-syntax " + name + " ...)",
			})
		}
	}

	// define/contract (Racket)
	if dialect == "racket" {
		for _, m := range defineContractRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if seen[name] {
				continue
			}
			seen[name] = true
			operationNames = append(operationNames, name)
			startLine := lineOf(src, m[0])
			body := extractSexprBody(src, m[0])
			endLine := startLine + strings.Count(body, "\n")
			calls := collectLispCalls(body, name, dialect)
			entities = append(entities, types.EntityRecord{
				Name:          name,
				Kind:          "SCOPE.Operation",
				Subtype:       "function",
				SourceFile:    filePath,
				Language:      dialect,
				StartLine:     startLine,
				EndLine:       endLine,
				Signature:     "(define/contract " + name + " ...)",
				Relationships: calls,
			})
		}
	}

	// 4. Components (structs, classes).
	compSeen := make(map[string]bool)

	// defstruct (Common Lisp)
	if dialect == "commonlisp" {
		for _, m := range defstructRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if compSeen[name] {
				continue
			}
			compSeen[name] = true
			startLine := lineOf(src, m[0])
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "struct",
				SourceFile: filePath,
				Language:   dialect,
				StartLine:  startLine,
				EndLine:    startLine,
				Signature:  "(defstruct " + name + " ...)",
			})
		}

		// defclass (Common Lisp)
		for _, m := range defclassRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if compSeen[name] {
				continue
			}
			compSeen[name] = true
			startLine := lineOf(src, m[0])
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "class",
				SourceFile: filePath,
				Language:   dialect,
				StartLine:  startLine,
				EndLine:    startLine,
				Signature:  "(defclass " + name + " ...)",
			})
		}
	}

	// define-struct (Scheme)
	if dialect == "scheme" {
		for _, m := range defineStructRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if compSeen[name] {
				continue
			}
			compSeen[name] = true
			startLine := lineOf(src, m[0])
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "struct",
				SourceFile: filePath,
				Language:   dialect,
				StartLine:  startLine,
				EndLine:    startLine,
				Signature:  "(define-struct " + name + " ...)",
			})
		}

		// define-class (Scheme)
		for _, m := range defineClassRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if compSeen[name] {
				continue
			}
			compSeen[name] = true
			startLine := lineOf(src, m[0])
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "class",
				SourceFile: filePath,
				Language:   dialect,
				StartLine:  startLine,
				EndLine:    startLine,
				Signature:  "(define-class " + name + " ...)",
			})
		}
	}

	// struct (Racket)
	if dialect == "racket" {
		for _, m := range structRE.FindAllStringSubmatchIndex(scrubbed, -1) {
			if len(m) < 4 {
				continue
			}
			name := scrubbed[m[2]:m[3]]
			if compSeen[name] {
				continue
			}
			compSeen[name] = true
			startLine := lineOf(src, m[0])
			entities = append(entities, types.EntityRecord{
				Name:       name,
				Kind:       "SCOPE.Component",
				Subtype:    "struct",
				SourceFile: filePath,
				Language:   dialect,
				StartLine:  startLine,
				EndLine:    startLine,
				Signature:  "(struct " + name + " ...)",
			})
		}
	}

	// 5. CONTAINS edges from package to top-level operations.
	if packageName != "" && len(operationNames) > 0 {
		var containsRels []types.RelationshipRecord
		for _, opName := range operationNames {
			ref := extractor.BuildOperationStructuralRef(dialect, filePath, opName)
			containsRels = append(containsRels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
		}
		for i := range entities {
			if entities[i].Name == packageName && entities[i].Kind == "SCOPE.Component" && entities[i].Subtype == "namespace" {
				entities[i].Relationships = append(entities[i].Relationships, containsRels...)
				break
			}
		}
	}

	return entities
}

// ---------------------------------------------------------------------------
// Import collection
// ---------------------------------------------------------------------------

func collectLispImports(src, dialect string) []string {
	seen := make(map[string]bool)
	var imports []string

	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		imports = append(imports, name)
	}

	switch dialect {
	case "commonlisp":
		// (load "filename")
		for _, m := range loadRE.FindAllStringSubmatch(src, -1) {
			if len(m) >= 2 {
				add(m[1])
			}
		}
		// (require ...) bare
		for _, m := range requireRE.FindAllStringSubmatch(src, -1) {
			if len(m) >= 3 {
				if m[1] != "" {
					add(m[1])
				} else if m[2] != "" {
					add(m[2])
				}
			}
		}
	case "scheme":
		for _, m := range requireRE.FindAllStringSubmatch(src, -1) {
			if len(m) >= 3 {
				if m[1] != "" {
					add(m[1])
				} else if m[2] != "" {
					add(m[2])
				}
			}
		}
	case "racket":
		// Racket (require racket/list) — module path form
		for _, m := range requirePathRE.FindAllStringSubmatch(src, -1) {
			if len(m) >= 2 {
				add(m[1])
			}
		}
		// (require/typed ...)
		for _, m := range requireTypedRE.FindAllStringSubmatch(src, -1) {
			if len(m) >= 2 {
				add(m[1])
			}
		}
	}

	return imports
}

func buildLispImportEntities(filePath, dialect string, imports []string) []types.EntityRecord {
	if len(imports) == 0 {
		return nil
	}
	out := make([]types.EntityRecord, 0, len(imports))
	seen := make(map[string]bool, len(imports))
	for _, mod := range imports {
		if seen[mod] {
			continue
		}
		seen[mod] = true
		displayName := lispImportDisplayName(mod)
		out = append(out, types.EntityRecord{
			Name:       displayName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   dialect,
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   mod,
					Kind:   "IMPORTS",
				},
			},
		})
	}
	return out
}

func lispImportDisplayName(mod string) string {
	// "path/to/module" → "module"; "module-name" → "module-name"
	if slash := strings.LastIndexByte(mod, '/'); slash >= 0 {
		return mod[slash+1:]
	}
	return mod
}

// ---------------------------------------------------------------------------
// CALLS edge collection
// ---------------------------------------------------------------------------

func collectLispCalls(body, callerName, dialect string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	matches := callRE.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]types.RelationshipRecord, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		target := m[1]
		if target == "" || target == callerName {
			continue
		}
		if lispSpecialForms[target] {
			continue
		}
		if len(target) <= 1 {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// S-expression body extraction
// ---------------------------------------------------------------------------

// extractSexprBody returns the text of the top-level s-expression starting at
// startOffset by matching parentheses.
func extractSexprBody(src string, startOffset int) string {
	// Find the opening paren.
	openIdx := strings.IndexByte(src[startOffset:], '(')
	if openIdx < 0 {
		return ""
	}
	openIdx += startOffset

	depth := 0
	inStr := false
	for i := openIdx; i < len(src); i++ {
		ch := src[i]
		if inStr {
			if ch == '\\' && i+1 < len(src) {
				i++ // skip escaped char
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case ';':
			// Line comment — skip to end of line.
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[openIdx : i+1]
			}
		}
	}
	return src[openIdx:]
}

// ---------------------------------------------------------------------------
// String/comment stripping
// ---------------------------------------------------------------------------

// stripLispStringsAndComments replaces string literals and ; line comments
// with spaces to avoid false pattern matches.
func stripLispStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := false

	for i < len(src) {
		ch := src[i]

		if inStr {
			if ch == '\\' && i+1 < len(src) {
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				continue
			}
			if ch == '"' {
				inStr = false
				out[i] = ' '
			} else {
				out[i] = ' '
			}
			i++
			continue
		}

		// Lisp line comment: ; to end of line
		if ch == ';' {
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
			continue
		}

		// Block comment: #| ... |# (Common Lisp / SRFI)
		if ch == '#' && i+1 < len(src) && src[i+1] == '|' {
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			for i < len(src) {
				if src[i] == '|' && i+1 < len(src) && src[i+1] == '#' {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					break
				}
				out[i] = ' '
				i++
			}
			continue
		}

		if ch == '"' {
			inStr = true
			out[i] = ' '
			i++
			continue
		}

		out[i] = ch
		i++
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// Line number helper
// ---------------------------------------------------------------------------

func lineOf(src string, offset int) int {
	if offset > len(src) {
		offset = len(src)
	}
	return strings.Count(src[:offset], "\n") + 1
}
