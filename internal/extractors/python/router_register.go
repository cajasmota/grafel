// router_register.go — DRF `router.register(prefix, ViewSetClass, ...)`
// REFERENCES edge emission from urls.py to the registered ViewSet class.
//
// Issue #2010 — A canonical DRF urls.py looks like:
//
//	from rest_framework.routers import DefaultRouter
//	from .views import PermitViewSet, JurisdictionViewSet
//
//	router = DefaultRouter()
//	router.register(r"permits", PermitViewSet, basename="permit")
//	router.register(r"jurisdictions", JurisdictionViewSet)
//
//	urlpatterns = [path("api/", include(router.urls))]
//
// Before this pass the urls.py file entity (#577) carried IMPORTS edges
// to PermitViewSet / JurisdictionViewSet but no edge that captured the
// *routing* relationship: which ViewSet is mounted at which URL prefix.
// Downstream tools that walk "what code does this URL hit?" stop dead.
//
// We emit a REFERENCES edge from the urls.py file entity to a
// structural-ref of the ViewSet class. The edge carries:
//
//	router_register = "true"
//	url_prefix       = "<prefix>"            (raw text of the first arg)
//	basename         = "<basename>"          (if the kwarg is present)
//	router_var       = "<routerVarName>"     (the variable the call lives on)
//
// Detection is intentionally syntactic — we match ANY `<x>.register(`
// call where the first positional arg is a string literal and the
// second positional arg is a Capitalised identifier. The DefaultRouter
// / SimpleRouter import binding is not strictly required: in practice
// any `register(prefix, ViewSet, ...)` shape in a urls/routes file is
// route registration.
//
// Filtering by file name (urls.py / routers.py / api_urls.py) keeps
// the false-positive surface low — a generic `.register(name, cls)` in
// a non-urls module won't be misinterpreted as DRF routing.

package python

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitRouterRegisterEdges scans the file for `<x>.register(<str>, <ViewSet>,
// ...)` call sites and appends REFERENCES edges from the file entity to
// each ViewSet's structural-ref. Safe with nil/empty inputs and non-urls
// files (no-op).
func emitRouterRegisterEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	if !isRouterRegisterCandidateFile(file.Path) {
		return
	}
	// Locate the file entity index — the REFERENCES from-side.
	fileIdx := -1
	for i := range *entities {
		if (*entities)[i].Kind == "SCOPE.Component" &&
			(*entities)[i].Subtype == "file" &&
			(*entities)[i].SourceFile == file.Path {
			fileIdx = i
			break
		}
	}
	if fileIdx < 0 {
		return
	}

	// Dedup: a single ViewSet may be re-registered with different
	// prefixes (rare but legal). We emit one edge per unique ViewSet
	// per file to avoid inflating REFERENCES counts; the url_prefix
	// property captures the first prefix seen.
	emitted := make(map[string]bool)

	var walk func(n ts.Node)
	walk = func(n ts.Node) {
		if n == nil {
			return
		}
		if n.Type() == "call" {
			tryEmitRegisterEdge(n, file, fileIdx, entities, emitted)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
}

// isRouterRegisterCandidateFile gates the pass to files where DRF
// router-registration is plausible. We accept any of:
//
//	urls.py, api_urls.py, urlpatterns.py, routers.py, router.py
//
// or any file under a `urls/` or `routers/` package. The set is
// intentionally inclusive — false positives in non-DRF urls modules
// fail later (no second-arg identifier → no edge).
func isRouterRegisterCandidateFile(path string) bool {
	if path == "" {
		return false
	}
	clean := filepath.ToSlash(path)
	base := filepath.Base(clean)
	switch base {
	case "urls.py", "api_urls.py", "urlpatterns.py", "routers.py", "router.py":
		return true
	}
	// Sub-package shapes.
	if strings.Contains(clean, "/urls/") || strings.Contains(clean, "/routers/") {
		return true
	}
	return false
}

// tryEmitRegisterEdge inspects a `call` node. If it matches
// `<receiver>.register(<string>, <Ident>, ...)` it emits a REFERENCES
// edge from the file entity to a structural-ref of the second argument
// (treated as the ViewSet class). Non-matching calls return silently.
func tryEmitRegisterEdge(
	callNode ts.Node,
	file extractor.FileInput,
	fileIdx int,
	entities *[]types.EntityRecord,
	emitted map[string]bool,
) {
	fn := callNode.ChildByFieldName("function")
	if fn == nil || fn.Type() != "attribute" {
		return
	}
	attrNode := fn.ChildByFieldName("attribute")
	if attrNode == nil || nodeText(attrNode, file.Content) != "register" {
		return
	}
	recvNode := fn.ChildByFieldName("object")
	routerVar := ""
	if recvNode != nil && recvNode.Type() == "identifier" {
		routerVar = nodeText(recvNode, file.Content)
	}
	argsNode := callNode.ChildByFieldName("arguments")
	if argsNode == nil {
		return
	}

	// Collect positional args (skipping punctuation) and capture the
	// `basename=` kwarg when present.
	var positional []ts.Node
	basename := ""
	for i := 0; i < int(argsNode.ChildCount()); i++ {
		arg := argsNode.Child(i)
		if arg == nil {
			continue
		}
		switch arg.Type() {
		case "(", ")", ",", "comment":
			continue
		case "keyword_argument":
			keyNode := arg.ChildByFieldName("name")
			valNode := arg.ChildByFieldName("value")
			if keyNode == nil || valNode == nil {
				continue
			}
			if nodeText(keyNode, file.Content) == "basename" && valNode.Type() == "string" {
				basename = stripQuotes(strings.TrimSpace(nodeText(valNode, file.Content)))
			}
			continue
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) < 2 {
		return
	}
	prefixNode := positional[0]
	viewSetNode := positional[1]

	// First positional must be a string literal (URL prefix).
	if prefixNode.Type() != "string" {
		return
	}
	prefix := strings.TrimSpace(nodeText(prefixNode, file.Content))
	// Strip leading raw-string `r` prefix and surrounding quotes.
	prefix = strings.TrimPrefix(prefix, "r")
	prefix = strings.TrimPrefix(prefix, "R")
	prefix = strings.Trim(prefix, "\"'")

	// Second positional must be a Capitalised identifier — the ViewSet.
	var viewSetName string
	switch viewSetNode.Type() {
	case "identifier":
		viewSetName = nodeText(viewSetNode, file.Content)
	case "attribute":
		// `views.PermitViewSet` — take the trailing leaf.
		txt := nodeText(viewSetNode, file.Content)
		if dot := strings.LastIndexByte(txt, '.'); dot >= 0 {
			viewSetName = txt[dot+1:]
		} else {
			viewSetName = txt
		}
	default:
		return
	}
	if !isCapitalisedIdent(viewSetName) {
		return
	}
	if emitted[viewSetName] {
		return
	}
	emitted[viewSetName] = true

	toID := buildDjangoModelClassRef(file.Path, viewSetName)
	props := map[string]string{
		"router_register": "true",
		"url_prefix":      prefix,
		"viewset":         viewSetName,
	}
	if routerVar != "" {
		props["router_var"] = routerVar
	}
	if basename != "" {
		props["basename"] = basename
	}
	(*entities)[fileIdx].Relationships = append((*entities)[fileIdx].Relationships,
		types.RelationshipRecord{
			ToID:       toID,
			Kind:       "REFERENCES",
			Properties: props,
		})
}
