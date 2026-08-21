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
	if isVBExternalBaseType(name) {
		return name, "", true
	}
	dot := strings.LastIndexByte(name, dottedNameSep)
	if dot <= 0 || dot == len(name)-1 {
		return "", "", false
	}
	head, leaf := name[:dot], name[dot+1:]
	if isVBExternalBaseType(head) {
		return head, leaf, true
	}
	return "", "", false
}
