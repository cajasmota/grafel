// Package kotlin — CST-based Ktor nested route extractor.
//
// This file implements a tree-sitter–driven extractor that walks the Kotlin CST
// and composes nested route("/prefix"){ … } blocks into full paths before
// emitting SCOPE.Operation/endpoint entities.
//
// The regex extractor in ktor.go handles flat get("/path"){} patterns but
// cannot compose nested prefixes. This extractor complements it by walking
// the actual CST to resolve full paths like:
//
//	route("/api") {
//	    route("/v1") {
//	        get("/users") { … }          → "GET /api/v1/users"
//	    }
//	    get("/health") { … }             → "GET /api/health"
//	}
//
// Registration key: "custom_kotlin_ktor_routes"
//
// Issue #3275 — Ktor route_extraction (nested prefix composition).
package kotlin

import (
	"context"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	tskotlin "github.com/smacker/go-tree-sitter/kotlin"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_kotlin_ktor_routes", &ktorRoutesExtractor{})
}

type ktorRoutesExtractor struct{}

func (e *ktorRoutesExtractor) Language() string { return "custom_kotlin_ktor_routes" }

// ktorHTTPVerbs is the set of Ktor DSL HTTP verb call names.
var ktorHTTPVerbs = map[string]bool{
	"get":     true,
	"post":    true,
	"put":     true,
	"delete":  true,
	"patch":   true,
	"head":    true,
	"options": true,
}

// Extract parses the Kotlin file with tree-sitter and walks the CST to emit
// route entities with composed full paths. Only Kotlin files are processed.
func (e *ktorRoutesExtractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "kotlin" {
		return nil, nil
	}
	// Only process files that look like they contain Ktor routing DSL.
	// Quick content gate: must reference at least one routing keyword.
	src := string(file.Content)
	if !strings.Contains(src, "routing") && !strings.Contains(src, "route(") {
		// Still check for bare verb handlers that may appear outside routing{}
		hasVerb := false
		for v := range ktorHTTPVerbs {
			if strings.Contains(src, v+"(\"") {
				hasVerb = true
				break
			}
		}
		if !hasVerb {
			return nil, nil
		}
	}

	parser := sitter.NewParser()
	parser.SetLanguage(tskotlin.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, file.Content)
	if err != nil || tree == nil {
		return nil, nil //nolint:nilerr // parse failures are non-fatal for custom extractors
	}
	defer tree.Close()

	seen := make(map[string]bool)
	var entities []types.EntityRecord

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	walkRoutes(tree.RootNode(), file, []string{}, add)
	return entities, nil
}

// walkRoutes performs a depth-first CST walk, accumulating a prefix stack from
// route("…") DSL blocks and emitting endpoint entities when HTTP verb handlers
// (get/post/put/delete/patch/head/options) are encountered.
func walkRoutes(
	node *sitter.Node,
	file extractor.FileInput,
	prefixes []string,
	add func(types.EntityRecord),
) {
	if node == nil {
		return
	}

	// We are interested in call_expression nodes where the first child is a
	// simple_identifier whose text is either "route" or an HTTP verb.
	if node.Type() == "call_expression" {
		callName, path, lambda := parseKtorCallExpr(node, file.Content)

		switch {
		case callName == "route" && path != "" && lambda != nil:
			// Descend into the lambda with the extended prefix stack.
			newPrefixes := append(prefixes, path) //nolint:gocritic // append to copy intentional
			walkRoutes(lambda, file, newPrefixes, add)
			return

		case ktorHTTPVerbs[callName] && path != "":
			// Compose the full route path from the accumulated prefix stack.
			fullPath := composePath(prefixes, path)
			name := strings.ToUpper(callName) + " " + fullPath
			ln := int(node.StartPoint().Row) + 1
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent,
				"framework", "ktor",
				"provenance", "INFERRED_FROM_KTOR_NESTED_ROUTE",
				"http_method", strings.ToUpper(callName),
				"path", fullPath,
			)
			add(ent)
			// Fall through to also walk inside the handler lambda for sub-routes.
			if lambda != nil {
				walkRoutes(lambda, file, prefixes, add)
			}
			return
		}
	}

	// Default: recurse into all children.
	for i := 0; i < int(node.ChildCount()); i++ {
		walkRoutes(node.Child(i), file, prefixes, add)
	}
}

// parseKtorCallExpr extracts (callName, path, lambdaNode) from a
// call_expression node shaped like: <verb>("path") { … }
//
// The tree-sitter-kotlin grammar represents Ktor DSL calls as:
//
//	call_expression
//	  simple_identifier        ← callName
//	  call_suffix              ← value_arguments "("path")"
//	  call_suffix              ← annotated_lambda { … }
//
// For nested route blocks the chain is two call_expression levels:
//
//	call_expression
//	  call_expression          ← inner: verb + args
//	    simple_identifier
//	    call_suffix (value_arguments)
//	  call_suffix (annotated_lambda)
//
// We handle both shapes.
func parseKtorCallExpr(node *sitter.Node, src []byte) (callName, path string, lambda *sitter.Node) {
	if node.ChildCount() == 0 {
		return "", "", nil
	}
	first := node.Child(0)

	// Shape A: simple_identifier + call_suffix(value_args) + call_suffix(lambda)
	if first.Type() == "simple_identifier" {
		callName = string(src[first.StartByte():first.EndByte()])
		// Walk remaining call_suffix children for path and lambda.
		for i := 1; i < int(node.ChildCount()); i++ {
			cs := node.Child(i)
			if cs.Type() != "call_suffix" {
				continue
			}
			if p := extractStringFromValueArgs(cs, src); p != "" && path == "" {
				path = p
			}
			if lam := extractLambdaNode(cs); lam != nil && lambda == nil {
				lambda = lam
			}
		}
		return callName, path, lambda
	}

	// Shape B: call_expression(inner) + call_suffix(lambda)
	// The outer node carries the trailing lambda; the inner call_expression
	// carries the verb name and the path argument.
	if first.Type() == "call_expression" {
		innerName, innerPath, _ := parseKtorCallExpr(first, src)
		if innerName == "" {
			return "", "", nil
		}
		// Find the trailing lambda on the outer node.
		for i := 1; i < int(node.ChildCount()); i++ {
			cs := node.Child(i)
			if cs.Type() == "call_suffix" {
				if lam := extractLambdaNode(cs); lam != nil {
					lambda = lam
				}
			}
		}
		return innerName, innerPath, lambda
	}

	return "", "", nil
}

// extractStringFromValueArgs returns the first string literal content found
// inside a call_suffix > value_arguments > value_argument > string_literal.
func extractStringFromValueArgs(callSuffix *sitter.Node, src []byte) string {
	if callSuffix == nil {
		return ""
	}
	// Scan for value_arguments child.
	for i := 0; i < int(callSuffix.ChildCount()); i++ {
		va := callSuffix.Child(i)
		if va.Type() != "value_arguments" {
			continue
		}
		for j := 0; j < int(va.ChildCount()); j++ {
			arg := va.Child(j)
			if arg.Type() != "value_argument" {
				continue
			}
			// Descend into value_argument to find string_literal > string_content.
			if s := firstStringContent(arg, src); s != "" {
				return s
			}
		}
	}
	return ""
}

// firstStringContent finds the first string_content descendant and returns its
// text, which is the path without surrounding quotes.
func firstStringContent(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	if node.Type() == "string_content" {
		return string(src[node.StartByte():node.EndByte()])
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		if s := firstStringContent(node.Child(i), src); s != "" {
			return s
		}
	}
	return ""
}

// extractLambdaNode finds an annotated_lambda > lambda_literal > statements
// node inside a call_suffix, which represents the DSL block body.
func extractLambdaNode(callSuffix *sitter.Node) *sitter.Node {
	if callSuffix == nil {
		return nil
	}
	for i := 0; i < int(callSuffix.ChildCount()); i++ {
		al := callSuffix.Child(i)
		if al.Type() != "annotated_lambda" {
			continue
		}
		for j := 0; j < int(al.ChildCount()); j++ {
			ll := al.Child(j)
			if ll.Type() != "lambda_literal" {
				continue
			}
			// Return the statements node (or the lambda_literal itself as fallback).
			for k := 0; k < int(ll.ChildCount()); k++ {
				ch := ll.Child(k)
				if ch.Type() == "statements" {
					return ch
				}
			}
			return ll
		}
	}
	return nil
}

// composePath joins prefix segments with the leaf path, normalising double
// slashes: composePath(["/api", "/v1"], "/users") → "/api/v1/users".
func composePath(prefixes []string, leaf string) string {
	parts := make([]string, 0, len(prefixes)+1)
	parts = append(parts, prefixes...)
	parts = append(parts, leaf)
	return filepath.ToSlash(filepath.Join(parts...))
}
