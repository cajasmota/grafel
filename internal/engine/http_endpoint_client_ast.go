// http_endpoint_client_ast.go — tree-sitter AST extraction of JS/TS HTTP
// client calls (fetch / axios / api-client wrappers) for consumer-side
// http_endpoint synthesis (#5527).
//
// WHY AST (vs the long-standing regex pass in http_endpoint_client_synthesis.go):
// the regex pass is mature and handles template-literal URLs, constant folding,
// and wrapper recognition, but everything it emits is stamped `heuristic` by the
// endpoint-stats confidence classifier (classifyDetector) because regex counts
// can over- or under-count. The client side is the half that creates cross-repo
// calls, so a heuristic stamp here is the real-user "1 of 12 resolved / had to
// manually verify the 0-caller ones" friction.
//
// This pass walks `call_expression` nodes and recognises the high-frequency,
// STATIC-URL client shapes deterministically from the syntax tree:
//
//	fetch("/users/1", { method: "POST" })   → http:POST:/users/1
//	axios.get("/users")                     → http:GET:/users
//	axios.post("/users", body)              → http:POST:/users
//	axios("/users")                         → http:GET:/users   (default verb)
//	axios({ url: "/users", method: "put" }) → http:PUT:/users
//	apiClient.get("/users")                 → http:GET:/users
//	fooClient.delete("/users/1")            → http:DELETE:/users/1
//
// Each match is emitted with extraction_method="ast", which the JS client emit
// closure stamps on the entity so classifyDetector returns ast/exact instead of
// regex/heuristic. The pass deliberately handles ONLY static string-literal URLs
// — template literals, env-var concatenation, and runtime-dynamic URLs are left
// to the regex pass (which runs after and stays honest-heuristic). The AST pass
// runs FIRST so its emits win the side-scoped ID dedup; the regex pass then
// re-claims the same IDs as no-ops and only adds the calls AST could not resolve.
package engine

import (
	"context"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// astClientVerbs is the set of HTTP-verb method names recognised on an
// axios-style receiver (`axios.<verb>(...)`, `<x>Client.<verb>(...)`). request
// maps to GET (the verb is then taken from a config object if present).
var astClientVerbs = map[string]string{
	"get":     "GET",
	"post":    "POST",
	"put":     "PUT",
	"delete":  "DELETE",
	"patch":   "PATCH",
	"head":    "HEAD",
	"options": "OPTIONS",
	"request": "GET",
}

// synthesizeFetchAxiosAST is the tree-sitter consumer-side pass. It parses
// content with the JS/TS grammar and emits one client synthetic per static
// fetch/axios/api-client call it can resolve, tagging each with
// extraction_method="ast" via the shared clientSynthState side channel.
//
// emit is the same emitFn the regex pass uses, so dedup, canonicalization, and
// entity shaping are identical — only the confidence stamp differs. A nil state
// disables the AST confidence stamp (the pass still emits, just heuristic),
// matching the regex pass's nil-tolerance.
func synthesizeFetchAxiosAST(content, lang string, emit emitFn, state *clientSynthState) {
	if state == nil {
		// No side channel ⇒ we cannot stamp extraction_method=ast, so there is
		// nothing the AST pass adds over the regex pass that follows. Bail.
		return
	}
	// Cheap pre-filter: skip files with no client markers at all (mirrors the
	// regex pass's early-exit so we never parse a file that has no client call).
	if !strings.Contains(content, "fetch(") &&
		!strings.Contains(content, "axios") &&
		!strings.Contains(content, "Client.") &&
		!strings.Contains(content, "client.") &&
		!strings.Contains(content, "apiClient") {
		return
	}

	tsLang := lang
	if tsLang != "javascript" && tsLang != "typescript" {
		tsLang = "typescript" // safe superset for any JS/TS-shaped input
	}
	factory := treesitter.NewParserFactory(nil)
	pr, err := factory.Parse(context.Background(), []byte(content), tsLang)
	if err != nil || pr == nil || pr.TSTree == nil {
		return // malformed beyond the error gate — regex pass still runs
	}
	defer pr.TSTree.Close()

	funcs := indexJSEnclosingFunctions(content)

	// emitAST stamps the AST provenance on the next emit, then clears it so the
	// regex pass that runs afterwards is never tagged ast by accident.
	emitAST := func(verb, canonical, framework, caller string) {
		state.pendingExtractionMethod = "ast"
		emit(verb, canonical, framework, "Function", caller)
		state.pendingExtractionMethod = ""
	}

	for _, call := range findAllCallExpressions(pr.TSTree.RootNode()) {
		fn := call.ChildByFieldName("function")
		if fn == nil {
			continue
		}
		switch fn.Type() {
		case "identifier":
			// fetch("/path", {method}) / axios("/path") / axios({url,method})
			name := nodeTextJS(fn, content)
			switch name {
			case "fetch":
				if verb, canon, ok := astFetchCall(call, content); ok {
					emitAST(verb, canon, "fetch", enclosingJSFuncAt(funcs, int(call.StartByte())))
				}
			case "axios":
				if verb, canon, ok := astAxiosBareCall(call, content); ok {
					emitAST(verb, canon, "axios", enclosingJSFuncAt(funcs, int(call.StartByte())))
				}
			}
		case "member_expression":
			// <receiver>.<verb>("/path", ...) — axios.get, apiClient.post, etc.
			prop := fn.ChildByFieldName("property")
			recv := fn.ChildByFieldName("object")
			if prop == nil || recv == nil {
				continue
			}
			verb, ok := astClientVerbs[nodeTextJS(prop, content)]
			if !ok {
				continue
			}
			framework, ok := astClientReceiverFramework(nodeTextJS(recv, content))
			if !ok {
				continue
			}
			rawURL, found := astFirstStringArg(call, content)
			if !found {
				continue // dynamic/template URL — leave to the regex pass
			}
			// `request` carries its verb in a config object, not the receiver.
			if v := astConfigMethod(call, content); v != "" {
				verb = v
			}
			path, ok := normalizeRawClientPath(rawURL)
			if !ok {
				continue
			}
			canon := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			emitAST(verb, canon, framework, enclosingJSFuncAt(funcs, int(call.StartByte())))
		}
	}
}

// astClientReceiverFramework maps a call receiver identifier to the framework
// label, recognising only receivers we can confidently type as an HTTP client.
// `axios` and any *Client / *client / apiClient / httpClient identifier qualify;
// arbitrary `.get(...)` receivers (Map, array, DOM) are rejected so we never
// fabricate a client call from an unrelated method.
func astClientReceiverFramework(recv string) (string, bool) {
	if recv == "" {
		return "", false
	}
	if recv == "axios" {
		return "axios", true
	}
	low := strings.ToLower(recv)
	if strings.HasSuffix(low, "client") || low == "api" || low == "http" {
		return "http_client", true
	}
	return "", false
}

// astFetchCall extracts (verb, canonicalPath) from a `fetch(url, opts)` call.
// The URL must be a static string literal; the verb defaults to GET and is
// overridden by a `method:` property in the options object.
func astFetchCall(call ts.Node, src string) (verb, canon string, ok bool) {
	rawURL, found := astFirstStringArg(call, src)
	if !found {
		return "", "", false
	}
	path, normOK := normalizeRawClientPath(rawURL)
	if !normOK {
		return "", "", false
	}
	verb = "GET"
	if m := astConfigMethod(call, src); m != "" {
		verb = m
	}
	return verb, httproutes.Canonicalize(httproutes.FrameworkExpress, path), true
}

// astAxiosBareCall extracts (verb, canonicalPath) from `axios("/path")` or
// `axios({ url: "/path", method: "post" })`.
func astAxiosBareCall(call ts.Node, src string) (verb, canon string, ok bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", "", false
	}
	// Form 1: axios("/path", { method }) — first arg is a string literal.
	if rawURL, found := astFirstStringArg(call, src); found {
		path, normOK := normalizeRawClientPath(rawURL)
		if !normOK {
			return "", "", false
		}
		verb = "GET"
		if m := astConfigMethod(call, src); m != "" {
			verb = m
		}
		return verb, httproutes.Canonicalize(httproutes.FrameworkExpress, path), true
	}
	// Form 2: axios({ url: "/path", method: "post" }) — first arg is an object
	// literal carrying both url and (optional) method.
	obj := astFirstObjectArg(args)
	if obj == nil {
		return "", "", false
	}
	rawURL, found := astObjectStringField(obj, src, "url", "baseURL")
	if !found {
		return "", "", false
	}
	path, normOK := normalizeRawClientPath(rawURL)
	if !normOK {
		return "", "", false
	}
	verb = "GET"
	if m, ok := astObjectStringField(obj, src, "method"); ok && m != "" {
		verb = strings.ToUpper(m)
	}
	return verb, httproutes.Canonicalize(httproutes.FrameworkExpress, path), true
}

// astFirstStringArg returns the first positional string-literal argument of a
// call (quotes stripped), or ("", false) when the first argument is not a
// static string literal.
func astFirstStringArg(call ts.Node, src string) (string, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		c := args.Child(i)
		if c == nil || !c.IsNamed() {
			continue
		}
		if c.Type() == "string" {
			return jsStringLiteralValue(nodeTextJS(c, src)), true
		}
		// First named arg is not a string literal (object/template/identifier);
		// do not skip past it — the URL must be the first positional arg.
		return "", false
	}
	return "", false
}

// astConfigMethod looks for a `{ method: "..." }` property in any object-literal
// argument of the call and returns the upper-cased verb, or "" when absent.
func astConfigMethod(call ts.Node, src string) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		c := args.Child(i)
		if c == nil || c.Type() != "object" {
			continue
		}
		if v, ok := astObjectStringField(c, src, "method"); ok && v != "" {
			return strings.ToUpper(v)
		}
	}
	return ""
}

// astFirstObjectArg returns the first object-literal positional argument.
func astFirstObjectArg(args ts.Node) ts.Node {
	for i := 0; i < int(args.ChildCount()); i++ {
		c := args.Child(i)
		if c != nil && c.Type() == "object" {
			return c
		}
	}
	return nil
}

// astObjectStringField returns the string value of the first matching key in an
// object literal (`{ key: "value" }`). keys are tried in order; the first one
// present with a string-literal value wins.
func astObjectStringField(obj ts.Node, src string, keys ...string) (string, bool) {
	want := make(map[string]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}
	for i := 0; i < int(obj.ChildCount()); i++ {
		pair := obj.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		keyNode := pair.ChildByFieldName("key")
		valNode := pair.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}
		key := jsStringLiteralValue(nodeTextJS(keyNode, src))
		if !want[key] {
			continue
		}
		if valNode.Type() != "string" {
			return "", false
		}
		return jsStringLiteralValue(nodeTextJS(valNode, src)), true
	}
	return "", false
}

// findAllCallExpressions returns every call_expression node under root.
func findAllCallExpressions(root ts.Node) []ts.Node {
	if root == nil {
		return nil
	}
	var out []ts.Node
	stack := []ts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == "call_expression" {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

// nodeTextJS slices the source text spanned by a node.
func nodeTextJS(n ts.Node, src string) string {
	if n == nil {
		return ""
	}
	s, e := int(n.StartByte()), int(n.EndByte())
	if s < 0 || e > len(src) || s > e {
		return ""
	}
	return src[s:e]
}

// jsStringLiteralValue strips matching surrounding quotes (', ", `) from a raw
// literal. Mirrors the javascript extractor's stringLiteralValue (kept local to
// the engine package, which cannot import the package-private extractor helper).
func jsStringLiteralValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '\'' || first == '"' || first == '`') && first == last {
			return s[1 : len(s)-1]
		}
	}
	return s
}
