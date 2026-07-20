package external

import (
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// synth_named_imports.go — per-symbol external node synthesis for NAMED
// imports (#4515, follow-up to #4480).
//
// PROBLEM. A named import like
//
//	import { NotFoundException, BadRequestException } from '@nestjs/common'
//
// brings two distinct framework symbols into scope. A reference to one of
// them — `throw new NotFoundException()`, `extends BaseEntity`, `x: SomeType`
// — arrives at this layer as a bare-name stub (`NotFoundException`). Without
// per-symbol resolution that stub either (a) folds to the PACKAGE-level
// placeholder `ext:@nestjs/common` (losing the distinct symbol identity, so
// #4480's throws→real-class retarget has no class node to land on → the
// `exception:NotFoundException` synthetic node is kept and the user sees a
// DUPLICATE), or (b) dangles unresolved. Either way an imported framework
// exception/class has no stable, distinct node.
//
// FIX. Build, per caller source file, a map of the symbols that file
// named-imports from an EXTERNAL package, keyed local-name → owning package
// root + original imported name. A bare-name reference whose local name is a
// named import is then routed to a per-symbol placeholder
//
//	ext:<package>:<ImportedName>
//
// (Name = ImportedName) — a single, stable, distinct node. Two files that
// import+reference the same symbol from the same package converge on the SAME
// node (dedup is by the deterministic ext id). Two packages that both export
// `NotFoundException` get DISTINCT nodes because the id is package-keyed. The
// `ext:` prefix is preserved, so the post-synthesis classifier treats these as
// honest external placeholders — NOT fidelity bugs.
//
// The per-symbol id shape (`ext:<package>:<leaf>`) is the SAME convention the
// Java/Kotlin extractor's resolveImportToIDs already stamps on IMPORTS edges
// (e.g. `ext:org.apache.poi:XSSFWorkbook`), so #4480's name-keyed resolver and
// the rest of the pipeline already understand it. Here we extend that shape to
// the *reference* edges (CALLS/THROWS/EXTENDS/…) for every language whose
// IMPORTS edges carry the `imported_name` + `local_name` contract.
//
// LANGUAGE GENERALITY. The map is built from the language-agnostic IMPORTS
// edge property contract (`local_name`, `imported_name`, `source_module`,
// `import_path`, `wildcard`); the package root is derived via the existing
// per-language *ExternalPackageRoot helpers. TS/JS and Java/Kotlin carry this
// contract today (the NestJS case is TS/JS). Python (`from pkg import X`) and
// other ecosystems whose extractors do not yet stamp `imported_name` on
// IMPORTS edges are tracked as follow-ups; they flow through unchanged until
// their extractor populates the contract, at which point they pick this up for
// free.

// namedImportTarget is the per-symbol external resolution for one
// (callerFile, localName) named import: the owning package root and the
// original imported symbol name, yielding the id `ext:<pkg>:<imported>`.
type namedImportTarget struct {
	pkg      string // canonical external package root (e.g. "@nestjs/common")
	imported string // original imported symbol name (pre-alias leaf)
}

// namedImportIndex resolves bare-name and namespace-member references to the
// per-symbol external node they were imported as.
type namedImportIndex struct {
	// byFileLocal maps callerFile -> localName -> target for NAMED + DEFAULT
	// imports (`import { Foo } from`, `import Foo from`).
	byFileLocal map[string]map[string]namedImportTarget
	// nsByFileLocal maps callerFile -> namespaceLocal -> package root for
	// NAMESPACE imports (`import * as X from`), so a statically-recoverable
	// member access `X.Foo` resolves to `ext:<pkg>:Foo`.
	nsByFileLocal map[string]map[string]string
}

func (ix *namedImportIndex) empty() bool {
	return ix == nil || (len(ix.byFileLocal) == 0 && len(ix.nsByFileLocal) == 0)
}

// lookup resolves a reference stub originating from callerFile to the package
// root + imported symbol leaf it was imported as, returning (pkg, leaf, true)
// on a hit. It handles:
//   - bare named/default imports: stub == localName.
//   - namespace member access: stub == "X.Foo" where X is a namespace import,
//     yielding (pkg, "Foo") — the statically-recoverable member.
func (ix *namedImportIndex) lookup(callerFile, stub string) (string, string, bool) {
	if ix.empty() || callerFile == "" || stub == "" {
		return "", "", false
	}
	if locals := ix.byFileLocal[callerFile]; locals != nil {
		if t, ok := locals[stub]; ok {
			return t.pkg, t.imported, true
		}
	}
	// Namespace member access `X.Foo` → (pkg, "Foo"). Only the single-level
	// `ns.Member` form is statically recoverable; deeper chains (`X.a.b`) are
	// left for the package-level fold.
	if ns := ix.nsByFileLocal[callerFile]; ns != nil {
		if dot := strings.IndexByte(stub, '.'); dot > 0 {
			head := stub[:dot]
			member := stub[dot+1:]
			if pkg, ok := ns[head]; ok && member != "" &&
				!strings.ContainsAny(member, ".[]()") {
				return pkg, member, true
			}
		}
	}
	return "", "", false
}

// canonicalHasSymbolLeaf reports whether a classifier canonical already carries
// a per-symbol leaf of the `ext:<pkg>:<Symbol>` form — i.e. its LAST
// colon-delimited segment is a Pascal/upper-initial identifier (a type/class
// name), as opposed to a package canon that merely contains a colon for other
// reasons (scoped npm "@scope/pkg" has no colon; "node:fs" / "db.<table>" use a
// colon/dot for the module, whose leaf is lower-initial). When true the #4515
// upgrade is a no-op so POI-style canons (org.apache.poi:XSSFWorkbook) keep
// their precise, already-per-symbol shape.
func canonicalHasSymbolLeaf(canonical string) bool {
	idx := strings.LastIndexByte(canonical, ':')
	if idx < 0 || idx == len(canonical)-1 {
		return false
	}
	leaf := canonical[idx+1:]
	if leaf == "" {
		return false
	}
	c := leaf[0]
	return c >= 'A' && c <= 'Z'
}

// buildNamedImportIndex walks every IMPORTS edge and records the external
// named/default/namespace imports each source file declares, so reference
// edges from that file can resolve a bare local name to its per-symbol
// external node. lang is resolved per edge via entityLang[FromID] (the IMPORTS
// FromID is the file-mirror SCOPE.Component) with a fallback to the edge's
// stamped language. fileForCaller maps an IMPORTS FromID to the owning source
// file path (the same key reference edges are indexed by).
func buildNamedImportIndex(
	doc *graph.Document,
	entityLang map[string]string,
	entityFile map[string]string,
	internal internalRoots,
) *namedImportIndex {
	ix := &namedImportIndex{
		byFileLocal:   map[string]map[string]namedImportTarget{},
		nsByFileLocal: map[string]map[string]string{},
	}
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind != string(types.RelationshipKindImports) || r.PropLen() == 0 {
			continue
		}
		// The caller file is the source file that owns the IMPORTS edge. The
		// FromID is the file-mirror SCOPE.Component (#577); map it back to the
		// declared SourceFile so it matches the key reference edges use
		// (entityFile[refEdge.FromID]). Older path-shaped FromIDs are their own
		// file path.
		callerFile := entityFile[r.FromID]
		if callerFile == "" {
			callerFile = r.FromID
		}
		if callerFile == "" {
			continue
		}

		local := strings.TrimSpace(r.PropGet("local_name"))
		imported := strings.TrimSpace(r.PropGet("imported_name"))
		wildcard := r.PropGet("wildcard") == "1"

		lang := entityLang[r.FromID]
		if lang == "" {
			lang = r.PropGet("language")
		}

		// Derive the canonical external package root for this import. When the
		// import is project-internal/relative (no external root), there is no
		// per-symbol external node to synthesise — skip.
		pkg, ok := importEdgePackageRoot(r.ToID, lang, r.PropsSnapshot(), internal)
		if !ok || pkg == "" {
			continue
		}

		switch {
		case wildcard:
			// `import * as X from 'pkg'` — X.<member> is resolved at reference
			// time to ext:<pkg>:<member>. Record the namespace local.
			if local == "" {
				continue
			}
			if ix.nsByFileLocal[callerFile] == nil {
				ix.nsByFileLocal[callerFile] = map[string]string{}
			}
			ix.nsByFileLocal[callerFile][local] = pkg
		default:
			// Named (`{ Foo }`, `{ Foo as Bar }`) or default (`import Foo`,
			// imported_name == "default") binding. The reference uses the LOCAL
			// name; the per-symbol node is keyed by the IMPORTED name so two
			// aliases of the same symbol converge. For a default import the
			// imported leaf is the package's default export — key it by the
			// local name (the conventional, stable identity of a default).
			if local == "" {
				local = imported
			}
			leaf := imported
			if leaf == "" || leaf == "default" {
				leaf = local
			}
			// Java/Kotlin source_module is the fully-qualified type; strip any
			// dotted prefix so the leaf is the simple symbol name (parity with
			// the extractor's resolveImportToIDs).
			if dot := strings.LastIndexByte(leaf, '.'); dot >= 0 {
				leaf = leaf[dot+1:]
			}
			if local == "" || leaf == "" {
				continue
			}
			if ix.byFileLocal[callerFile] == nil {
				ix.byFileLocal[callerFile] = map[string]namedImportTarget{}
			}
			ix.byFileLocal[callerFile][local] = namedImportTarget{pkg: pkg, imported: leaf}
		}
	}
	if ix.empty() {
		return nil
	}
	return ix
}

// importEdgePackageRoot derives the canonical external package root for an
// IMPORTS edge by dispatching on language to the existing per-language root
// helpers, returning ("", false) when the import is project-internal/relative
// or otherwise not an external dependency. Mirrors the classifier's
// per-language disposition so per-symbol nodes share the SAME package canon as
// the package-level placeholder.
func importEdgePackageRoot(toID, lang string, relProps map[string]string, internal internalRoots) (string, bool) {
	switch lang {
	case "javascript", "typescript", "tsx", "jsx":
		return jsExternalPackageRoot(toID, relProps)
	case "java", "kotlin":
		return javaExternalPackageRoot(toID, relProps, internal.java)
	case "python":
		return pyExternalPackageRoot(toID, relProps, internal.python)
	case "ruby":
		return rubyExternalPackageRoot(toID, relProps, internal.ruby)
	case "rust":
		return rustExternalPackageRoot(toID, relProps, internal.rust)
	case "csharp":
		return csharpExternalPackageRoot(toID, relProps, internal.csharp)
	}
	return "", false
}
