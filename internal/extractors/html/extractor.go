// Package html implements the tree-sitter–based extractor for HTML template files.
//
// Extracted entities:
//
//	form element         → Kind="SCOPE.Operation",   Subtype="form"         (action attr or "form")
//	script src include   → Kind="SCOPE.Component",   Subtype="script_include"
//	link stylesheet      → Kind="SCOPE.Component",   Subtype="style_include"
//	img src include      → Kind="SCOPE.Component",   Subtype="image_include"
//	mustache expression  → Kind="SCOPE.Pattern",     Subtype="template_expr"  (dot/filter only)
//	custom element       → Kind="SCOPE.Component",   Subtype="component"
//	Vue directive (@/v-) → Kind="SCOPE.Pattern",     Subtype="directive"
//	Angular ng- attr     → Kind="SCOPE.Pattern",     Subtype="directive"
//	Jinja2 directive     → Kind="SCOPE.Component",   Subtype="jinja_directive" (block/extends/include/macro/for/if)
//	form field child     → Kind="SCOPE.UIComponent", Subtype="form_field"   (input/select/textarea/button)
//
// Emitted relationships (per issue #373 PORT-RELS-HTML):
//
//	IMPORTS  — asset references via <script src>, <link href>, <img src>.
//	          Edge FromID = file path, ToID = href/src value. Properties:
//	          local_name (filename basename), source_module (raw href/src),
//	          imported_name (""). CALLS and CONTAINS are not applicable to
//	          HTML templates (no functions/classes/methods to model).
//
// Registers itself via init() and is imported by registry_gen.go.
package html

import (
	"context"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("html", &Extractor{})
}

// Extractor implements extractor.Extractor for HTML template files.
type Extractor struct{}

// Language returns the canonical language key.
func (e *Extractor) Language() string { return "html" }

// reCustomElement matches Web Component / custom element tag names.
// Per spec: tag name matching [A-Z][a-zA-Z]+-[a-zA-Z]+ OR containing a hyphen
// (both uppercase-starting like CustomWidget-v2 and lowercase like my-component).
// HTML spec: custom elements must contain a hyphen; this regex captures both
// PascalCase-hyphenated and lowercase-hyphenated forms.
var reCustomElement = regexp.MustCompile(`^[A-Za-z][a-zA-Z]*-[a-zA-Z]`)

// reMustache matches text containing {{ ... }} template expressions.
var reMustache = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)

// reJinja2Directive matches Jinja2 {% ... %} block-level directives.
// Capture group 1: the directive keyword (block, extends, include, macro, for, if, etc.)
// Capture group 2: the directive argument (name/path/expression), trimmed.
var reJinja2Directive = regexp.MustCompile(`\{%-?\s*(block|extends|include|macro|for|if|elif|else|endif|endblock|endfor|endmacro)\b([^%]*)%-?\}`)

// formFieldTags is the set of HTML tags that are treated as form field children.
var formFieldTags = map[string]struct{}{
	"input":    {},
	"select":   {},
	"textarea": {},
	"button":   {},
}

// Extract walks the tree-sitter CST and returns entity records.
// On nil tree or empty src, returns empty slice with nil error.
// Node parse errors are skipped — never panic.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.html")
	ctx, span := tracer.Start(ctx, "indexer.extract.html",
		trace.WithAttributes(attribute.String("language", "html")),
	)
	defer span.End()

	if len(file.Content) == 0 {
		span.SetAttributes(
			attribute.Int("file_line_count", 0),
			attribute.Int("entity_count", 0),
		)
		return nil, nil
	}

	// Reuse pre-parsed tree or parse inline.
	tree := file.Tree
	if tree == nil {
		parser := sitter.NewParser()
		parser.SetLanguage(htmlGrammar())
		var err error
		tree, err = parser.ParseCtx(ctx, nil, file.Content)
		if err != nil {
			return nil, err
		}
	}

	lineCount := strings.Count(string(file.Content), "\n") + 1

	// First pass: tree-sitter CST walk for structural HTML entities.
	entities := walkDocument(tree.RootNode(), file)

	// Second pass: regex scan of raw source for Jinja2 {% %} directives.
	// Tree-sitter's HTML grammar tokenises Jinja2 blocks as raw_text or text
	// nodes (not structured elements), so we scan the raw source bytes.
	entities = append(entities, extractJinja2Directives(file)...)

	span.SetAttributes(
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", len(entities)),
	)
	return entities, nil
}

// walkDocument traverses the full document tree, collecting entities from all nodes.
func walkDocument(root *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	if root == nil {
		return nil
	}

	var entities []types.EntityRecord
	stack := []*sitter.Node{root}

	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node == nil {
			continue
		}

		switch node.Type() {
		case "element":
			recs := visitElement(node, file)
			entities = append(entities, recs...)
			// Form elements are fully handled by visitElement (including
			// descendant form-field children). Skip generic child-push for
			// elements that visitElement consumed so we don't double-emit.
			if isFormElement(node, file) {
				continue
			}

		case "script_element":
			if rec, ok := visitScriptElement(node, file); ok {
				entities = append(entities, rec)
			}

		case "self_closing_tag":
			// e.g. <img src="..." /> in XHTML-style documents.
			if rec, ok := visitSelfClosingTag(node, file); ok {
				entities = append(entities, rec)
			}

		case "text":
			recs := visitTextNode(node, file)
			entities = append(entities, recs...)
		}

		// Push children onto the stack.
		for i := int(node.ChildCount()) - 1; i >= 0; i-- {
			ch := node.Child(i)
			if ch != nil {
				stack = append(stack, ch)
			}
		}
	}

	return entities
}

// isFormElement reports whether node is an <element> whose start tag is <form>.
// Used by walkDocument to avoid re-pushing form children onto the stack after
// visitElement has already descended into them.
func isFormElement(node *sitter.Node, file extractor.FileInput) bool {
	startTag := childByType(node, "start_tag")
	if startTag == nil {
		return false
	}
	tagNameNode := childByType(startTag, "tag_name")
	if tagNameNode == nil {
		return false
	}
	return strings.ToLower(nodeText(tagNameNode, file.Content)) == "form"
}

// visitElement handles <form>, <link rel="stylesheet">, and custom elements.
// For <form> elements it also descends into children to extract form fields.
// Also checks attributes for Vue/Angular directives.
func visitElement(node *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	var recs []types.EntityRecord

	startTag := childByType(node, "start_tag")
	if startTag == nil {
		return nil
	}

	tagNameNode := childByType(startTag, "tag_name")
	if tagNameNode == nil {
		return nil
	}
	tagName := nodeText(tagNameNode, file.Content)
	if tagName == "" {
		return nil
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	switch strings.ToLower(tagName) {
	case "form":
		action := attrValue(startTag, "action", file.Content)
		name := action
		if name == "" {
			name = "form"
		}
		recs = append(recs, types.EntityRecord{
			Name:         name,
			Kind:         "SCOPE.Operation",
			Subtype:      "form",
			SourceFile:   file.Path,
			Language:     "html",
			StartLine:    startLine,
			EndLine:      endLine,
			Signature:    "form action=" + name,
			QualityScore: 0.7,
		})
		// Descend into form children to extract field entities.
		recs = append(recs, visitFormFields(node, file)...)

	case "link":
		rel := attrValue(startTag, "rel", file.Content)
		if strings.ToLower(rel) == "stylesheet" {
			href := attrValue(startTag, "href", file.Content)
			if href != "" {
				recs = append(recs, types.EntityRecord{
					Name:         href,
					Kind:         "SCOPE.Component",
					Subtype:      "style_include",
					SourceFile:   file.Path,
					Language:     "html",
					StartLine:    startLine,
					EndLine:      endLine,
					Signature:    "link rel=stylesheet href=" + href,
					QualityScore: 0.7,
					Relationships: []types.RelationshipRecord{
						buildAssetImportRel(file.Path, href),
					},
				})
			}
		}

	case "img":
		src := attrValue(startTag, "src", file.Content)
		if src != "" {
			recs = append(recs, types.EntityRecord{
				Name:         src,
				Kind:         "SCOPE.Component",
				Subtype:      "image_include",
				SourceFile:   file.Path,
				Language:     "html",
				StartLine:    startLine,
				EndLine:      endLine,
				Signature:    "img src=" + src,
				QualityScore: 0.65,
				Relationships: []types.RelationshipRecord{
					buildAssetImportRel(file.Path, src),
				},
			})
		}
	}

	// Custom element: tag name contains a hyphen (Web Components spec).
	if reCustomElement.MatchString(tagName) {
		recs = append(recs, types.EntityRecord{
			Name:         tagName,
			Kind:         "SCOPE.Component",
			Subtype:      "component",
			SourceFile:   file.Path,
			Language:     "html",
			StartLine:    startLine,
			EndLine:      endLine,
			Signature:    "<" + tagName + ">",
			QualityScore: 0.75,
		})
	}

	// Vue/Angular directive attributes.
	recs = append(recs, visitAttributes(startTag, file, tagName, startLine, endLine)...)

	return recs
}

// visitFormFields walks the immediate and nested children of a <form> element
// and emits SCOPE.UIComponent entities for <input>, <select>, <textarea>,
// and <button> tags.
func visitFormFields(formNode *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	var recs []types.EntityRecord

	// BFS over all descendants of the form node (not just direct children,
	// to handle divs/fieldsets wrapping the fields).
	queue := []*sitter.Node{}
	for i := range formNode.ChildCount() {
		if ch := formNode.Child(int(i)); ch != nil {
			queue = append(queue, ch)
		}
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if node == nil {
			continue
		}

		if node.Type() == "element" {
			startTag := childByType(node, "start_tag")
			if startTag != nil {
				tagNameNode := childByType(startTag, "tag_name")
				if tagNameNode != nil {
					tagName := strings.ToLower(nodeText(tagNameNode, file.Content))
					if _, isField := formFieldTags[tagName]; isField {
						rec := buildFormFieldEntity(node, startTag, tagName, file)
						recs = append(recs, rec)
					}
				}
			}
		}

		// Also handle self-closing tags (void elements like <input>).
		if node.Type() == "self_closing_tag" {
			tagNameNode := childByType(node, "tag_name")
			if tagNameNode != nil {
				tagName := strings.ToLower(nodeText(tagNameNode, file.Content))
				if _, isField := formFieldTags[tagName]; isField {
					rec := buildFormFieldEntity(node, node, tagName, file)
					recs = append(recs, rec)
				}
			}
		}

		// Enqueue children for BFS.
		for i := range node.ChildCount() {
			if ch := node.Child(int(i)); ch != nil {
				queue = append(queue, ch)
			}
		}
	}

	return recs
}

// buildFormFieldEntity constructs a SCOPE.UIComponent entity record for a
// form field element. It prefers the "name" attribute for the entity name,
// falls back to "id", then to the tag name itself.
func buildFormFieldEntity(node *sitter.Node, attrSource *sitter.Node, tagName string, file extractor.FileInput) types.EntityRecord {
	name := attrValue(attrSource, "name", file.Content)
	if name == "" {
		name = attrValue(attrSource, "id", file.Content)
	}
	if name == "" {
		name = tagName
	}

	fieldType := attrValue(attrSource, "type", file.Content)
	sig := tagName
	if fieldType != "" {
		sig += " type=" + fieldType
	}
	if name != tagName {
		sig += " name=" + name
	}

	return types.EntityRecord{
		Name:         name,
		Kind:         "SCOPE.UIComponent",
		Subtype:      "form_field",
		SourceFile:   file.Path,
		Language:     "html",
		StartLine:    int(node.StartPoint().Row) + 1,
		EndLine:      int(node.EndPoint().Row) + 1,
		Signature:    sig,
		QualityScore: 0.65,
	}
}

// extractJinja2Directives scans the raw source bytes for Jinja2 {% ... %}
// directives and emits SCOPE.Component entities with Subtype="jinja_directive".
//
// Tree-sitter's HTML grammar does not structurally parse Jinja2 control blocks;
// they appear as raw text fragments in the CST. We use a line-aware regex scan
// so we can report accurate line numbers.
func extractJinja2Directives(file extractor.FileInput) []types.EntityRecord {
	src := string(file.Content)
	if !strings.Contains(src, "{%") {
		return nil
	}

	lines := strings.Split(src, "\n")
	var recs []types.EntityRecord

	for lineIdx, line := range lines {
		lineNum := lineIdx + 1
		matches := reJinja2Directive.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			keyword := strings.TrimSpace(m[1])
			arg := strings.TrimSpace(m[2])

			// Only emit for "opening" directives — skip endif/endblock/endfor/endmacro/else/elif
			// to avoid inflating counts with closing tags.
			switch keyword {
			case "endif", "endblock", "endfor", "endmacro", "else", "elif":
				continue
			}

			name := keyword
			if arg != "" {
				// For named directives, include the first token of the argument.
				firstToken := strings.Fields(arg)
				if len(firstToken) > 0 {
					name = keyword + ":" + firstToken[0]
				}
			}

			recs = append(recs, types.EntityRecord{
				Name:         name,
				Kind:         "SCOPE.Component",
				Subtype:      "jinja_directive",
				SourceFile:   file.Path,
				Language:     "html",
				StartLine:    lineNum,
				EndLine:      lineNum,
				Signature:    "{% " + keyword + " " + arg + " %}",
				QualityScore: 0.65,
			})
		}
	}

	return recs
}

// visitScriptElement handles <script src="..."> includes.
func visitScriptElement(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	startTag := childByType(node, "start_tag")
	if startTag == nil {
		return types.EntityRecord{}, false
	}

	src := attrValue(startTag, "src", file.Content)
	if src == "" {
		return types.EntityRecord{}, false
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	return types.EntityRecord{
		Name:         src,
		Kind:         "SCOPE.Component",
		Subtype:      "script_include",
		SourceFile:   file.Path,
		Language:     "html",
		StartLine:    startLine,
		EndLine:      endLine,
		Signature:    "script src=" + src,
		QualityScore: 0.7,
		Relationships: []types.RelationshipRecord{
			buildAssetImportRel(file.Path, src),
		},
	}, true
}

// visitSelfClosingTag handles XHTML-style void elements like
// <img src="..." />, <link href="..." />, <script src="..." />.
// The HTML grammar parses these as self_closing_tag nodes rather than as
// element/script_element wrappers, so we cover them separately here to
// keep IMPORTS coverage symmetric with the start_tag form.
func visitSelfClosingTag(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	tagNameNode := childByType(node, "tag_name")
	if tagNameNode == nil {
		return types.EntityRecord{}, false
	}
	tagName := strings.ToLower(nodeText(tagNameNode, file.Content))

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	switch tagName {
	case "img":
		src := attrValue(node, "src", file.Content)
		if src == "" {
			return types.EntityRecord{}, false
		}
		return types.EntityRecord{
			Name:         src,
			Kind:         "SCOPE.Component",
			Subtype:      "image_include",
			SourceFile:   file.Path,
			Language:     "html",
			StartLine:    startLine,
			EndLine:      endLine,
			Signature:    "img src=" + src,
			QualityScore: 0.65,
			Relationships: []types.RelationshipRecord{
				buildAssetImportRel(file.Path, src),
			},
		}, true

	case "script":
		src := attrValue(node, "src", file.Content)
		if src == "" {
			return types.EntityRecord{}, false
		}
		return types.EntityRecord{
			Name:         src,
			Kind:         "SCOPE.Component",
			Subtype:      "script_include",
			SourceFile:   file.Path,
			Language:     "html",
			StartLine:    startLine,
			EndLine:      endLine,
			Signature:    "script src=" + src,
			QualityScore: 0.7,
			Relationships: []types.RelationshipRecord{
				buildAssetImportRel(file.Path, src),
			},
		}, true

	case "link":
		rel := attrValue(node, "rel", file.Content)
		if strings.ToLower(rel) != "stylesheet" {
			return types.EntityRecord{}, false
		}
		href := attrValue(node, "href", file.Content)
		if href == "" {
			return types.EntityRecord{}, false
		}
		return types.EntityRecord{
			Name:         href,
			Kind:         "SCOPE.Component",
			Subtype:      "style_include",
			SourceFile:   file.Path,
			Language:     "html",
			StartLine:    startLine,
			EndLine:      endLine,
			Signature:    "link rel=stylesheet href=" + href,
			QualityScore: 0.7,
			Relationships: []types.RelationshipRecord{
				buildAssetImportRel(file.Path, href),
			},
		}, true
	}
	return types.EntityRecord{}, false
}

// buildAssetImportRel constructs the IMPORTS relationship contract used by
// HTML asset references (issue #373). The cross-file resolver consumes:
//
//	local_name    — the filename basename of the asset (e.g. "app.js" from
//	                "/static/app.js"). Best-effort identifier for the
//	                imported binding inside this file.
//	source_module — the raw href/src value as written in source.
//	imported_name — empty: HTML asset includes do not introduce named
//	                bindings the way `import { x } from "y"` does.
func buildAssetImportRel(fromPath, ref string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: fromPath,
		ToID:   ref,
		Kind:   "IMPORTS",
		Properties: map[string]string{
			"local_name":    assetBasename(ref),
			"source_module": ref,
			"imported_name": "",
		},
	}
}

// assetBasename returns the trailing path segment of a URL or file ref,
// stripped of any query string or fragment. Used for the local_name
// property on HTML IMPORTS edges. Falls back to the full ref if no
// path segment can be extracted.
func assetBasename(ref string) string {
	s := ref
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return ref
	}
	return s
}

// visitTextNode scans text content for {{ }} mustache template expressions.
// Emits only expressions that contain a dot or pipe (filter) character to
// reduce noise from simple variable references.
func visitTextNode(node *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	text := nodeText(node, file.Content)
	if text == "" {
		return nil
	}

	matches := reMustache.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	var recs []types.EntityRecord
	for _, m := range matches {
		expr := strings.TrimSpace(m[1])
		if expr == "" {
			continue
		}
		// Spec rule 4: only emit if expression contains a dot or pipe filter.
		if !strings.ContainsAny(expr, ".|") {
			continue
		}
		recs = append(recs, types.EntityRecord{
			Name:         expr,
			Kind:         "SCOPE.Pattern",
			Subtype:      "template_expr",
			SourceFile:   file.Path,
			Language:     "html",
			StartLine:    startLine,
			EndLine:      endLine,
			Signature:    "{{ " + expr + " }}",
			QualityScore: 0.6,
		})
	}
	return recs
}

// visitAttributes walks attribute nodes in a start_tag and emits Vue/Angular
// directive entities.
func visitAttributes(startTag *sitter.Node, file extractor.FileInput, tagName string, startLine, endLine int) []types.EntityRecord {
	var recs []types.EntityRecord

	for i := range startTag.ChildCount() {
		ch := startTag.Child(int(i))
		if ch == nil || ch.Type() != "attribute" {
			continue
		}
		attrNameNode := childByType(ch, "attribute_name")
		if attrNameNode == nil {
			continue
		}
		attrName := nodeText(attrNameNode, file.Content)
		if attrName == "" {
			continue
		}

		// Vue directive: attribute starts with @ or v-
		if strings.HasPrefix(attrName, "@") || strings.HasPrefix(attrName, "v-") {
			recs = append(recs, types.EntityRecord{
				Name:         attrName,
				Kind:         "SCOPE.Pattern",
				Subtype:      "directive",
				SourceFile:   file.Path,
				Language:     "html",
				StartLine:    startLine,
				EndLine:      endLine,
				Signature:    tagName + " " + attrName,
				QualityScore: 0.65,
			})
			continue
		}

		// Angular directive: attribute starts with ng-
		if strings.HasPrefix(attrName, "ng-") {
			recs = append(recs, types.EntityRecord{
				Name:         attrName,
				Kind:         "SCOPE.Pattern",
				Subtype:      "directive",
				SourceFile:   file.Path,
				Language:     "html",
				StartLine:    startLine,
				EndLine:      endLine,
				Signature:    tagName + " " + attrName,
				QualityScore: 0.65,
			})
		}
	}
	return recs
}

// nodeText returns the UTF-8 text span of a node in the source.
func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	start := node.StartByte()
	end := node.EndByte()
	if int(end) > len(src) {
		end = uint32(len(src))
	}
	return string(src[start:end])
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

// attrValue looks up the value of a named attribute in a start_tag node.
// Returns empty string if the attribute is absent or has no value.
func attrValue(startTag *sitter.Node, name string, src []byte) string {
	for i := range startTag.ChildCount() {
		ch := startTag.Child(int(i))
		if ch == nil || ch.Type() != "attribute" {
			continue
		}
		attrNameNode := childByType(ch, "attribute_name")
		if attrNameNode == nil {
			continue
		}
		if !strings.EqualFold(nodeText(attrNameNode, src), name) {
			continue
		}
		// Value is inside quoted_attribute_value → attribute_value
		qav := childByType(ch, "quoted_attribute_value")
		if qav != nil {
			av := childByType(qav, "attribute_value")
			if av != nil {
				return nodeText(av, src)
			}
		}
		// Unquoted attribute value
		av := childByType(ch, "attribute_value")
		if av != nil {
			return nodeText(av, src)
		}
	}
	return ""
}
