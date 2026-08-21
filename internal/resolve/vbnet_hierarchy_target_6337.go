package resolve

import "strings"

// VBNetExternalHierarchyTarget classifies an UNRESOLVED VB.NET EXTENDS /
// IMPLEMENTS target as a .NET Framework type reference, and splits it into the
// type spelling and (for a member-level `Implements IFoo.Bar` clause) the
// member leaf (#6337).
//
// It is the single exported entry point onto the vbExternalBaseTypes /
// vbFrameworkRootNamespaces tables, which stay unexported in refs.go beside
// TestVBExternalBaseTypesAreLoadBearing — the test that pins them in both
// directions against a checked-in fixture. internal/external needs the same
// predicate for external synthesis; duplicating the tables there would leave
// the copy unpinned, so it calls through here instead.
//
// WHAT IT DOES NOT ANSWER, and the caller must not treat it as though it did:
// whether the name is absent from the graph. Exactly as with the
// classifyDispositionLang call site (refs.go), an unresolved VB.NET endpoint is
// very often an AMBIGUOUS IN-TREE one — a partial class split across `Foo.vb`
// and `Foo.Designer.vb` — and a WinForms application declaring its own `Panel`
// or `Form` would otherwise have that ambiguity classified as external, hiding
// the partial-class defect. Every caller must pair this with an in-tree
// name check.
//
// The two shapes:
//
//	System.Windows.Forms.Form  -> ("System.Windows.Forms.Form", "",        true)
//	Form                       -> ("Form",                      "",        true)
//	IDisposable.Dispose        -> ("IDisposable",               "Dispose", true)
//	IFrameServer.GetFrame      -> ("",                          "",        false)
//
// The returned typePart is NORMALISED, never the raw input: a generic argument
// list is stripped, so `List(Of Machine)` and `List(Of Profile)` both report
// `List` and share one node. Malformed spellings — a trailing or doubled
// separator, a non-identifier segment — do not classify at all, so a
// truncated `System.` from a misparsed clause stays an unresolved bug edge
// instead of becoming a tidy placeholder (#6337 round 2):
//
//	List(Of Machine)           -> ("List",                      "",        true)
//	IComparable(Of P).CompareTo-> ("IComparable",              "CompareTo", true)
//	System.                    -> ("",                          "",        false)
//
// The member split is attempted ONLY when the whole spelling does not itself
// classify. That ordering matters: `System.Windows.Forms.Form` would otherwise
// split into type `System.Windows.Forms` + member `Form`, because the dotted
// rule keys on the ROOT namespace and is happy either way. There is no way to
// tell a dotted BCL type from a dotted BCL member without the framework type
// index, so a `System.`-rooted spelling is always reported as a type. That is a
// stated limitation, not an oversight; it affects only the ~3 dotted
// member-level clauses in the 302-file corpus and it never changes whether the
// edge resolves.
func VBNetExternalHierarchyTarget(name string) (typePart, member string, ok bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", false
	}
	// NORMALISED, not raw. vbExternalBaseName classifies `List(Of Machine)` on
	// the strength of `List` and returns `List`; returning the raw spelling
	// here would mint one `ext:dotnet:List(Of Machine)` node per instantiation
	// and defeat the grouping this arm exists to provide (#6337 round 2).
	if base, isExt := vbExternalBaseName(name); isExt {
		return base, "", true
	}
	dot := strings.LastIndexByte(name, dottedNameSep)
	if dot <= 0 || dot == len(name)-1 {
		return "", "", false
	}
	head, leaf := name[:dot], name[dot+1:]
	if !isWellFormedVBTypeName(leaf) {
		return "", "", false
	}
	if base, isExt := vbExternalBaseName(head); isExt {
		return base, leaf, true
	}
	return "", "", false
}

// VBNetHierarchyTargetIsMalformed reports whether name is a VB.NET hierarchy
// target that LOOKS like a .NET Framework spelling but is not one — its root
// segment is a framework root namespace, yet the spelling itself is malformed
// (#6337 round 2).
//
// It exists because declining to classify is not enough. The generic
// dotted-root fallback further down classifyExternal keys on the first segment
// and nothing else, so a truncated `System.` that VBNetExternalHierarchyTarget
// refuses still lands on `ext:System` — which resolve.IsResolvedToID accepts,
// so the misparse leaves the bug-edge count either way and the extractor defect
// it represents goes unreported. The only way a malformed clause stays visible
// is for the vbnet arm to BLOCK it rather than pass it on.
//
// The predicate is deliberately narrow. It fires only when the root segment is
// one of the three framework root namespaces, so a malformed name that the
// dotted fallback would have handled on some other authority is untouched, and
// a well-formed one is never blocked:
//
//	System.              -> true   (truncated clause; was ext:System)
//	System..Forms.Form   -> true
//	System.Forms.Form>   -> true
//	System.Windows.Forms -> false  (well-formed; classifies normally)
//	Fomr>                -> false  (not framework-rooted; not ours to judge)
func VBNetHierarchyTargetIsMalformed(name string) bool {
	base := stripVBGenericArgs(name)
	if isWellFormedVBTypeName(base) {
		return false
	}
	root := base
	if dot := strings.IndexByte(base, dottedNameSep); dot >= 0 {
		root = base[:dot]
	}
	// Case-folded, for the same reason and through the same helper the
	// classifier uses (#6337 round 4): `system.` is as much a truncated
	// framework clause as `System.` is, and a case-sensitive test here would
	// block one and pass the other on to the fallback that mints `ext:system`.
	_, ok := vbFrameworkRootCanonical(root)
	return ok
}
