// AST-driven Go HTTP route -> handler-method binding pass.
//
// The YAML framework rules for chi/gin/echo/fiber/gorilla_mux emit ROUTES_TO
// edges from `Route:<path>` to `Controller:<bareName>`, where `<bareName>` is
// the second-argument identifier captured by a `(\w+)` group. For idiomatic Go
// HTTP code, the handler is almost always a struct-method expression:
//
//	h := handlers.NewUsersHandler(s)
//	r := chi.NewRouter()
//	r.Get("/users", h.List)
//	r.Get("/users/{id}", h.Get)
//
// Because `(\w+)` cannot span `.`, the regex captures only `h` (the receiver
// variable), so the YAML pass emits `Controller:h` — an edge that doesn't
// resolve to the real handler entity (`UsersHandler.List`,
// `UsersHandler.Get`). Every Go HTTP service using one of the supported
// routers is affected.
//
// This pass walks the Go AST, finds router-style calls of the form
// `<receiver>.<HTTPVerb>("<path>", <recv>.<Method>, ...)`, resolves `<recv>`
// to its declared type using a same-file local-variable map, and rewrites the
// orphan `Controller:<recv>` ROUTES_TO edge to point at
// `Controller:<TypeName>.<Method>` — the qualified-method form used by the
// Go extractor for methods on a struct receiver.
//
// Type resolution is intentionally conservative:
//
//  1. `var h *UsersHandler` / `var h UsersHandler` (typed decl)
//  2. `h := &UsersHandler{...}` / `h := UsersHandler{...}` (composite literal)
//  3. `h := NewUsersHandler(...)` (Go `NewT` constructor idiom — returns `*T`)
//
// If a receiver cannot be confidently resolved we leave the edge untouched
// (safer-bias). The pass only rewrites edges; it never creates new entities,
// so it cannot regress non-Go pipelines or add false-positive Routes.
//
// Refs #613.
package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// goHTTPVerbs is the union of method names used by the supported Go HTTP
// routers to register a single-path handler. Title-case (chi, fiber,
// gorilla_mux), upper-case (gin, echo), and the "Any/All/Handle" catch-alls
// are all included so the same pass covers every framework whose YAML rule
// emits `Controller:<receiverVar>`.
var goHTTPVerbs = map[string]bool{
	// Title-case (chi, fiber, gorilla_mux)
	"Get": true, "Post": true, "Put": true, "Patch": true, "Delete": true,
	"Head": true, "Options": true, "Connect": true, "Trace": true,
	// Upper-case (gin, echo)
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	"HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
	// Catch-alls
	"Any": true, "All": true, "Handle": true, "HandleFunc": true,
}

// applyGoRouteComposition rewrites YAML-emitted ROUTES_TO edges whose target
// is a bare receiver variable (e.g. `Controller:h`) into edges that target
// the qualified handler method (e.g. `Controller:UsersHandler.List`).
//
// Non-Go files are returned unchanged. The pass never adds or removes
// entities and never adds new relationships — it only edits the `ToID` of
// existing ROUTES_TO records, so it cannot regress the surrounding pipeline.
func applyGoRouteComposition(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	rawEntities := args.Entities
	rawRels := args.Relationships
	if lang != "go" || len(content) == 0 {
		return DetectorPassResult{Entities: rawEntities, Relationships: rawRels}
	}
	// Cheap pre-filter: skip files that obviously don't register HTTP routes.
	if !bytesContainsAny(content,
		".Get(", ".Post(", ".Put(", ".Patch(", ".Delete(",
		".GET(", ".POST(", ".PUT(", ".PATCH(", ".DELETE(",
		".Handle(", ".HandleFunc(", ".Any(", ".All(",
	) {
		return DetectorPassResult{Entities: rawEntities, Relationships: rawRels}
	}

	bindings := extractGoHandlerBindings(path, content)
	if len(bindings) == 0 {
		return DetectorPassResult{Entities: rawEntities, Relationships: rawRels}
	}

	// Build a lookup keyed by `(path, receiverVar)` -> qualified method name.
	// The YAML rule emits exactly one ROUTES_TO per `.Verb("/path", h.X)`
	// call site, and we keyed the binding the same way to make the rewrite
	// unambiguous.
	type key struct {
		routePath string
		recvVar   string
	}
	rewrite := map[key]string{}
	for _, b := range bindings {
		rewrite[key{b.routePath, b.recvVar}] = b.qualified
		// Go 1.22+ stdlib patterns embed the method in the pattern string
		// (`"GET /users/{id}"`). The handler arg is keyed by the full pattern,
		// but the ROUTES_TO edge's path may be the bare route ("/users/{id}").
		// Register a method-stripped alias so either keying resolves.
		if stripped := stripGoMethodPrefix(b.routePath); stripped != b.routePath {
			rewrite[key{stripped, b.recvVar}] = b.qualified
		}
	}

	for i, r := range rawRels {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		if !strings.HasPrefix(r.FromID, "Route:") {
			continue
		}
		if !strings.HasPrefix(r.ToID, "Controller:") {
			continue
		}
		routePath := strings.TrimPrefix(r.FromID, "Route:")
		recvVar := strings.TrimPrefix(r.ToID, "Controller:")
		// Only rewrite if the current target is a bare receiver-variable
		// name. If `recvVar` already contains a `.` it's a qualified method
		// reference (or a package call) and we leave it alone.
		if strings.Contains(recvVar, ".") {
			continue
		}
		qualified, ok := rewrite[key{routePath, recvVar}]
		if !ok {
			continue
		}
		newRel := r
		newRel.ToID = "Controller:" + qualified
		if newRel.Properties == nil {
			newRel.Properties = map[string]string{}
		} else {
			// Clone to avoid mutating the original map shared with other
			// relationship slices.
			cloned := make(map[string]string, len(newRel.Properties)+1)
			for k, v := range newRel.Properties {
				cloned[k] = v
			}
			newRel.Properties = cloned
		}
		newRel.Properties["pattern_type"] = "ast_driven"
		newRel.Properties["go_route_binding"] = "method_receiver_resolved"
		rawRels[i] = newRel
	}

	return DetectorPassResult{Entities: rawEntities, Relationships: rawRels}
}

// goHandlerBinding pairs a router call site with the qualified method name
// it targets after receiver-type resolution.
type goHandlerBinding struct {
	routePath string // e.g. "/users/{id}"
	recvVar   string // e.g. "h"
	qualified string // e.g. "UsersHandler.List"
}

// extractGoHandlerBindings parses the Go file, builds a same-file variable
// type map, and returns one binding per recognised `.Verb("/path", recv.M)`
// call where `recv` resolves to a known struct type. Calls that don't fit
// the shape (string-literal first arg, selector-expr second arg, supported
// verb) are skipped silently.
func extractGoHandlerBindings(path string, content []byte) []goHandlerBinding {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, content, parser.SkipObjectResolution)
	if err != nil || file == nil {
		return nil
	}

	varTypes := buildGoVarTypeMap(file)

	var out []goHandlerBinding
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if !goHTTPVerbs[sel.Sel.Name] {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		// First arg must be a string literal path.
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		routePath := strings.Trim(lit.Value, "\"`")
		// Find the handler arg. For .Handle("/p", handler) it's args[1];
		// for chains with middleware (e.g. fiber `.Get("/p", mw1, mw2, h)`)
		// the handler is conventionally the last arg. We scan from the end
		// for the first SelectorExpr whose base is an Ident — that's the
		// receiver-method handler we care about.
		var handlerSel *ast.SelectorExpr
		for i := len(call.Args) - 1; i >= 1; i-- {
			if hs, ok := call.Args[i].(*ast.SelectorExpr); ok {
				if _, ok := hs.X.(*ast.Ident); ok {
					handlerSel = hs
					break
				}
			}
		}
		if handlerSel == nil {
			return true
		}
		recvIdent, _ := handlerSel.X.(*ast.Ident)
		recvVar := recvIdent.Name
		method := handlerSel.Sel.Name
		typeName, ok := varTypes[recvVar]
		if !ok || typeName == "" {
			return true
		}
		out = append(out, goHandlerBinding{
			routePath: routePath,
			recvVar:   recvVar,
			qualified: typeName + "." + method,
		})
		return true
	})
	return out
}

// buildGoVarTypeMap walks the file's top-level + function-level declarations
// and returns a `varName -> typeName` map, where `typeName` is the bare
// struct type the variable holds (pointer-ness is intentionally stripped:
// downstream entities key on the type name, not `*Type`).
//
// Supported declaration shapes:
//
//	var h *T
//	var h T
//	h := &T{...}
//	h := T{...}
//	h := NewT(...)         // Go constructor idiom → *T
//	h := pkg.NewT(...)     // cross-package constructor → *T
func buildGoVarTypeMap(file *ast.File) map[string]string {
	out := map[string]string{}

	record := func(name, typ string) {
		if name == "" || typ == "" || name == "_" {
			return
		}
		// First-write-wins keeps the earliest binding stable. Local
		// shadowing inside nested blocks is rare in route-registration
		// code and we'd rather under-rewrite than mis-rewrite.
		if _, exists := out[name]; !exists {
			out[name] = typ
		}
	}

	visit := func(decl ast.Node) {
		ast.Inspect(decl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				if x.Tok != token.DEFINE && x.Tok != token.ASSIGN {
					return true
				}
				// Pair each LHS Ident with the corresponding RHS.
				for i, lhs := range x.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok {
						continue
					}
					if i >= len(x.Rhs) {
						break
					}
					if typ := inferGoExprType(x.Rhs[i]); typ != "" {
						record(id.Name, typ)
					}
				}
			case *ast.DeclStmt:
				gen, ok := x.Decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					return true
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					typ := typeNameFromExpr(vs.Type)
					for i, name := range vs.Names {
						if typ != "" {
							record(name.Name, typ)
							continue
						}
						if i < len(vs.Values) {
							if inferred := inferGoExprType(vs.Values[i]); inferred != "" {
								record(name.Name, inferred)
							}
						}
					}
				}
			case *ast.GenDecl:
				if x.Tok != token.VAR {
					return true
				}
				for _, spec := range x.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					typ := typeNameFromExpr(vs.Type)
					for i, name := range vs.Names {
						if typ != "" {
							record(name.Name, typ)
							continue
						}
						if i < len(vs.Values) {
							if inferred := inferGoExprType(vs.Values[i]); inferred != "" {
								record(name.Name, inferred)
							}
						}
					}
				}
			}
			return true
		})
	}

	for _, decl := range file.Decls {
		visit(decl)
	}
	return out
}

// inferGoExprType returns the bare struct type name an expression evaluates
// to, for the limited set of shapes used by route-registration code.
// Returns "" when no confident inference can be made.
func inferGoExprType(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.UnaryExpr:
		// &T{...} -> T
		if v.Op == token.AND {
			return inferGoExprType(v.X)
		}
	case *ast.CompositeLit:
		// T{...} or pkg.T{...} -> T
		return typeNameFromExpr(v.Type)
	case *ast.CallExpr:
		// NewT(...) or pkg.NewT(...) -> T (Go constructor idiom).
		switch fn := v.Fun.(type) {
		case *ast.Ident:
			if strings.HasPrefix(fn.Name, "New") && len(fn.Name) > 3 {
				return fn.Name[3:]
			}
		case *ast.SelectorExpr:
			if strings.HasPrefix(fn.Sel.Name, "New") && len(fn.Sel.Name) > 3 {
				return fn.Sel.Name[3:]
			}
		}
	}
	return ""
}

// stripGoMethodPrefix removes a leading Go 1.22+ stdlib method token from a
// net/http pattern (`"GET /users/{id}"` -> `"/users/{id}"`). Only a recognised
// HTTP verb followed by a single space is stripped; all other patterns
// (including third-party-router paths that happen to contain spaces) are
// returned unchanged.
func stripGoMethodPrefix(pattern string) string {
	sp := strings.IndexByte(pattern, ' ')
	if sp <= 0 {
		return pattern
	}
	switch pattern[:sp] {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE":
		return strings.TrimSpace(pattern[sp+1:])
	}
	return pattern
}

// typeNameFromExpr returns the bare type name from a type expression, peeling
// off pointer- and package-qualifier wrappers. Returns "" for unsupported
// shapes (slices, maps, interfaces, etc.).
func typeNameFromExpr(expr ast.Expr) string {
	switch v := expr.(type) {
	case nil:
		return ""
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return typeNameFromExpr(v.X)
	case *ast.SelectorExpr:
		// pkg.T -> T (entity names in the index are unqualified).
		return v.Sel.Name
	}
	return ""
}
