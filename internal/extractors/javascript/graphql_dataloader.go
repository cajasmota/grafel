// graphql_dataloader.go — issue #3624 (epic #3607).
//
// GraphQL DataLoader N+1 batch-loader extraction
// ───────────────────────────────────────────────
// GraphQL field resolvers that naively fetch a related record per parent
// object trigger the classic N+1 query problem: resolving `author` for N
// posts issues N separate user fetches. The `dataloader` npm package solves
// this by batching per-key loads into a single call:
//
//   import DataLoader from 'dataloader'
//
//   const userLoader = new DataLoader(batchUsers)          // wraps a batch fn
//   const postLoader = new DataLoader((ids) => batchPosts(ids))  // inline arrow
//
//   const resolvers = {
//     Post: {
//       author: (post) => userLoader.load(post.authorId),  // batched fetch
//     },
//   }
//
// This extractor records the N+1-avoidance wiring:
//
//   1. Loader entity — one SCOPE.DataLoader per `new DataLoader(...)` that is
//      assigned to a const/let/var or a class field. The entity is named by
//      the variable/field it is assigned to (e.g. `userLoader`).
//
//   2. BATCHES edge — SCOPE.DataLoader → batch function. When the first
//      argument to `new DataLoader(...)` is a bare identifier (a named batch
//      function) or an arrow that immediately delegates to a single call
//      (`(ids) => batchPosts(ids)`), we emit a BATCHES edge to that function.
//
//   3. USES edge — resolver → loader. At each `<loader>.load(id)` /
//      `<loader>.loadMany(ids)` call site inside a resolver (any function/
//      method body), we emit a USES edge from the enclosing function to the
//      loader. This surfaces which resolver batches via which loader.
//
// Honest-partial: only statically-named loaders (assigned to an identifier or
// a class field) are captured. Loaders built dynamically (returned from a
// factory, stored in a map, constructed inline inside a resolver return) are
// out of scope; the load()-site USES edge still fires for any name that
// resolves to a known loader in the same file.
//
// All entities/edges carry Properties["via"] = "graphql_dataloader" so the
// resolver and dashboards can classify them.

package javascript

import (
	"strconv"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

// PropViaGraphQLDataLoader is the value stamped on Properties["via"] for every
// entity / edge emitted by the GraphQL DataLoader pass.
const PropViaGraphQLDataLoader = "graphql_dataloader"

// dataLoaderTracker holds the per-file set of statically-named DataLoader
// instances. Built once per file by buildDataLoaderTracker after
// importByLocal is populated. Nil when the file does not import the
// "dataloader" package (fast-path for the overwhelming majority of files).
type dataLoaderTracker struct {
	// loaders maps a loader variable/field name (e.g. "userLoader") to a true
	// flag. Used by load()-site detection to gate USES edges to known loaders.
	loaders map[string]bool
}

// buildDataLoaderTracker constructs a dataLoaderTracker by scanning the file
// for `new DataLoader(...)` constructions, where the `DataLoader` identifier
// is bound to the "dataloader" npm package.
//
// Returns nil when no "dataloader" import is found (fast-path) or when the
// file contains no statically-named loader.
func (x *extractor) buildDataLoaderTracker(root ts.Node) *dataLoaderTracker {
	if x.importByLocal == nil || root == nil {
		return nil
	}
	// Find local names bound to the "dataloader" package (default import is the
	// idiomatic `import DataLoader from 'dataloader'`; also accept a named or
	// namespace import that resolves to the same package).
	ctorLocals := make(map[string]bool)
	for localName, b := range x.importByLocal {
		if b == nil || b.importPath != "dataloader" {
			continue
		}
		ctorLocals[localName] = true
	}
	if len(ctorLocals) == 0 {
		return nil
	}

	t := &dataLoaderTracker{loaders: make(map[string]bool)}
	t.scanForLoaders(x, root, ctorLocals)
	if len(t.loaders) == 0 {
		return nil
	}
	return t
}

// scanForLoaders walks the AST recording, for each
//
//	const <loaderVar> = new DataLoader(<batch>)
//	<loaderVar> = new DataLoader(<batch>)              (assignment_expression)
//	class C { <field> = new DataLoader(<batch>) }      (field_definition)
//
// the loader entity (named by the LHS) and a BATCHES edge to the wrapped batch
// function when it can be statically resolved.
func (t *dataLoaderTracker) scanForLoaders(x *extractor, root ts.Node, ctorLocals map[string]bool) {
	stack := []ts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		switch n.Type() {
		case "variable_declarator":
			t.processAssignment(x, n.ChildByFieldName("name"), n.ChildByFieldName("value"), ctorLocals)
		case "assignment_expression":
			t.processAssignment(x, n.ChildByFieldName("left"), n.ChildByFieldName("right"), ctorLocals)
		case "public_field_definition", "field_definition":
			t.processAssignment(x, n.ChildByFieldName("name"), n.ChildByFieldName("value"), ctorLocals)
		}
		for i := int(n.ChildCount()) - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
}

// processAssignment records a loader when valueNode is `new DataLoader(...)`
// and nameNode is a simple identifier / property name.
func (t *dataLoaderTracker) processAssignment(x *extractor, nameNode, valueNode ts.Node, ctorLocals map[string]bool) {
	if nameNode == nil || valueNode == nil {
		return
	}
	loaderName := loaderLHSName(x, nameNode)
	if loaderName == "" {
		return
	}
	if valueNode.Type() != "new_expression" {
		return
	}
	if !isDataLoaderConstruction(x, valueNode, ctorLocals) {
		return
	}

	t.loaders[loaderName] = true

	props := map[string]string{
		"via":         PropViaGraphQLDataLoader,
		"loader_name": loaderName,
	}
	var rels []types.RelationshipRecord
	if batchFn := dataLoaderBatchFn(x, valueNode); batchFn != "" {
		props["batch_fn"] = batchFn
		rels = append(rels, types.RelationshipRecord{
			ToID: batchFn,
			Kind: string(types.RelationshipKindBatches),
			Properties: map[string]string{
				"via":  PropViaGraphQLDataLoader,
				"line": strconv.Itoa(int(valueNode.StartPoint().Row) + 1),
			},
		})
	}
	x.emitWithProps(loaderName, string(types.EntityKindDataLoader), valueNode, "dataloader", "new DataLoader("+loaderName+")", props, rels)
}

// loaderLHSName returns the loader name for the LHS of an assignment:
//   - identifier            → its text ("userLoader")
//   - property_identifier   → its text (class field "userLoader")
//   - member_expression     → the trailing property ("this.userLoader" → "userLoader")
//
// Returns "" for destructuring / computed / unsupported shapes.
func loaderLHSName(x *extractor, nameNode ts.Node) string {
	switch nameNode.Type() {
	case "identifier", "property_identifier", "shorthand_property_identifier":
		return x.nodeText(nameNode)
	case "member_expression":
		prop := nameNode.ChildByFieldName("property")
		if prop != nil {
			return x.nodeText(prop)
		}
	}
	return ""
}

// isDataLoaderConstruction reports whether newExpr is `new DataLoader(...)`
// where the constructor identifier is bound to the "dataloader" package.
func isDataLoaderConstruction(x *extractor, newExpr ts.Node, ctorLocals map[string]bool) bool {
	ctor := newExpr.ChildByFieldName("constructor")
	if ctor == nil {
		return false
	}
	switch ctor.Type() {
	case "identifier":
		// `new DataLoader(...)` — default/named import.
		return ctorLocals[x.nodeText(ctor)]
	case "member_expression":
		// `new dl.DataLoader(...)` — namespace import. Accept when the object is
		// a dataloader-bound local; the property is the class name.
		obj := ctor.ChildByFieldName("object")
		if obj != nil && obj.Type() == "identifier" {
			return ctorLocals[x.nodeText(obj)]
		}
	}
	return false
}

// dataLoaderBatchFn resolves the wrapped batch function name from the first
// argument to `new DataLoader(<batch>)`:
//
//   - bare identifier:            new DataLoader(batchUsers)        → "batchUsers"
//   - delegating arrow:           new DataLoader((ids) => batchUsers(ids)) → "batchUsers"
//   - delegating function expr:   new DataLoader(function (ids) { return batchUsers(ids) }) → "batchUsers"
//
// Returns "" when the batch function cannot be named statically (inline
// multi-statement body, member-expression callee, etc.). In that honest-partial
// case the loader entity is still emitted; only the BATCHES edge is omitted.
func dataLoaderBatchFn(x *extractor, newExpr ts.Node) string {
	args := newExpr.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	var firstArg ts.Node
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "(", ")", ",":
			continue
		}
		firstArg = ch
		break
	}
	if firstArg == nil {
		return ""
	}

	switch firstArg.Type() {
	case "identifier":
		return x.nodeText(firstArg)
	case "arrow_function", "function_expression":
		return delegatedCallName(x, firstArg.ChildByFieldName("body"))
	}
	return ""
}

// delegatedCallName extracts the callee identifier from an arrow/function body
// that immediately delegates to a single call:
//
//	(ids) => batchUsers(ids)                  body = call_expression
//	(ids) => { return batchUsers(ids) }       body = statement_block → return → call
//
// Returns "" when the body is not a simple single-call delegation or when the
// callee is not a bare identifier (e.g. `this.batch(ids)`).
func delegatedCallName(x *extractor, body ts.Node) string {
	if body == nil {
		return ""
	}
	switch body.Type() {
	case "call_expression":
		fn := body.ChildByFieldName("function")
		if fn != nil && fn.Type() == "identifier" {
			return x.nodeText(fn)
		}
	case "statement_block":
		for i := 0; i < int(body.ChildCount()); i++ {
			stmt := body.Child(i)
			if stmt == nil || stmt.Type() != "return_statement" {
				continue
			}
			for j := 0; j < int(stmt.ChildCount()); j++ {
				ch := stmt.Child(j)
				if ch == nil || ch.Type() != "call_expression" {
					continue
				}
				fn := ch.ChildByFieldName("function")
				if fn != nil && fn.Type() == "identifier" {
					return x.nodeText(fn)
				}
			}
		}
	}
	return ""
}

// dataLoaderLoadEdges checks whether callNode is `<loader>.load(id)` or
// `<loader>.loadMany(ids)` where <loader> is a statically-known loader in this
// file. When it matches, it returns a USES RelationshipRecord from the
// enclosing resolver (callerName) to the loader, tagged
// Properties["via"]="graphql_dataloader".
//
// Recognised receiver shapes:
//
//	userLoader.load(id)            object = identifier
//	this.userLoader.load(id)       object = member_expression (trailing prop)
//	context.userLoader.load(id)    object = member_expression (trailing prop)
func (t *dataLoaderTracker) dataLoaderLoadEdges(x *extractor, callNode ts.Node) []types.RelationshipRecord {
	if t == nil {
		return nil
	}
	fn := callNode.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return nil
	}
	method := fn.ChildByFieldName("property")
	if method == nil {
		return nil
	}
	switch x.nodeText(method) {
	case "load", "loadMany":
	default:
		return nil
	}

	loaderName := loaderReceiverName(x, fn.ChildByFieldName("object"))
	if loaderName == "" || !t.loaders[loaderName] {
		return nil
	}

	return []types.RelationshipRecord{{
		ToID: loaderName,
		Kind: string(types.RelationshipKindUses),
		Properties: map[string]string{
			"via":    PropViaGraphQLDataLoader,
			"loader": loaderName,
			"line":   strconv.Itoa(int(callNode.StartPoint().Row) + 1),
		},
	}}
}

// loaderReceiverName resolves the loader name from the object of the
// `<obj>.load(...)` member expression:
//   - identifier            → its text ("userLoader")
//   - member_expression     → the trailing property ("this.userLoader" /
//     "context.loaders.userLoader" → "userLoader")
func loaderReceiverName(x *extractor, obj ts.Node) string {
	if obj == nil {
		return ""
	}
	switch obj.Type() {
	case "identifier":
		return x.nodeText(obj)
	case "member_expression":
		prop := obj.ChildByFieldName("property")
		if prop != nil {
			return x.nodeText(prop)
		}
	}
	return ""
}
