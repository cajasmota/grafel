// Package vbnet lifts VB.NET source into grafel entities and edges.
//
// It is S5 of #6327, requested in #6321. The parsing is done by internal/vbnet
// — the hand-written recursive-descent line scanner shipped in S4 (#6381) —
// and this package is a projection of that containment tree onto EntityRecords
// plus the four edge kinds the epic names: EXTENDS, IMPLEMENTS, IMPORTS, CALLS.
//
// # The two contracts that cost five other languages a bug each
//
// EXTENDS/IMPLEMENTS are emitted EMBEDDED on the owning TYPE record with
// FromID left EMPTY. Both graph-assembly paths — cmd/grafel/index.go's record
// loop and relRecordToGraphRel in internal/extractors/incremental.go —
// substitute the owning record's own entity id only when FromID == "". A
// path-valued FromID would instead be rewritten by ReferencesEmbedded onto
// whichever node carries that path as its Name, i.e. the file carrier, and
// every type in a multi-type file would merge its bases onto that one node.
// That is #6295 (solidity), #6298 (verilog, astro), #6365 and #6367.
// internal/extractors/file_anchored_rels_guard_test.go is the source-level
// half of the same rule and covers this package automatically.
//
// IMPORTS are the one documented exception: they hang on the per-file
// SCOPE.Component carrier with FromID = the importing file's path, because
// BuildImportTable (internal/resolve/imports.go:216) keys the per-file binding
// on it. No per-import placeholder entity is emitted — that is the #742 /
// #681 / #693 pattern solidity moved to in #6371, because a placeholder named
// after the import's leaf segment collides by graph.EntityID with a same-named
// declaration in the same file (#6368/#6369).
//
// # What the disambiguator decides, and what this package must not re-decide
//
// CALLS come from vbnet.Ref.IsCall() and nothing else. That method composes
// the declaration table's ParenKind verdict with the New prefix and the
// statement-head rule, and excludes intrinsics — CType, CInt, GetType and
// friends are call-SHAPED but resolve to no declaration, and one WinForms
// designer file in the measured corpus contains hundreds of CType. Re-deriving
// "looks like a call" here would reintroduce the phantom-CALLS problem #6327
// exists to prevent.
//
// One further guard is explicit rather than inherited: a Ref with Qualified
// true and Qualifier "" has a receiver that is REAL but unnameable by a
// per-file pass — a With-block target or an expression result. Measured on the
// 302-file corpus: 1,624 of 41,748 use sites. IsCall() is already false for
// every one of them, but callTarget refuses them independently so the
// invariant does not rest on that composition holding.
//
// # Known recall limits inherited from S4, stated so they are not rediscovered
//
//   - Every With-block member invocation is dropped. `.SetValue(k, v)` standing
//     alone is a real call and IsCall() reports false for it.
//   - `AddressOf Foo` is not recorded at all — it carries no parentheses, so
//     the reference pass never sees it.
//   - Method-level `Implements IFoo.Bar` is parsed onto Node.Implements but is
//     NOT emitted as an IMPLEMENTS edge: the epic scopes IMPLEMENTS to the
//     type, and a member-level edge to `IFoo.Bar` would compete with the
//     type-level edge to `IFoo` in the same graph.
//   - Hierarchy edges are stamped with the TYPE's declaration line, not the
//     line of the Inherits/Implements clause. vbnet.Node records those clauses
//     as a []string with no positions, so the clause line is not available.
package vbnet

import (
	"context"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
	"github.com/cajasmota/grafel/internal/vbnet"
)

// lang is the canonical slug, matching internal/classifier/classifier.go:368
// (".vb": "vbnet") and internal/substrate/substrate.go:175.
const lang = "vbnet"

func init() {
	extractor.Register(lang, &Extractor{})
}

// Extractor implements extractor.Extractor for VB.NET.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return lang }

// Extract projects one VB.NET file onto entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractVBNet(string(file.Content), file.Path, file.RepoRoot)
	// Both tags are load-bearing, not decoration: relLanguage
	// (internal/resolve/refs.go:5893) reads Properties["language"] off each
	// RELATIONSHIP, and that value is what selects the lang=="vbnet" arm in
	// classifyDispositionLang. Without it every external base type lands in
	// bug-extractor.
	extractor.TagRelationshipsLanguage(out, lang)
	extractor.TagEntitiesLanguage(out, lang)
	return out, nil
}

// extractVBNet is the testable core.
func extractVBNet(src, filePath, repoRoot string) []types.EntityRecord {
	res := vbnet.Parse(src)

	out := []types.EntityRecord{extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: lang,
	})}
	out[0].Relationships = append(out[0].Relationships, importRelationships(filePath, res.File)...)

	// The file carrier owns anything declared outside a type, so file-level and
	// namespace-level call sites anchor there rather than being dropped.
	appendCalls(&out[0], res.File)
	emit(&out, res.File, filePath, repoRoot, 0, "")
	return out
}

// emit walks the containment tree, appending one record per entity-bearing
// declaration and wiring a CONTAINS edge from the nearest entity-bearing
// ancestor.
//
// ownerIdx is that ancestor's index in *out; ns is the innermost enclosing
// Namespace path, stamped on types as a property rather than emitted as its
// own entity (the C# convention — internal/extractors/csharp/csharp.go:280).
// Emitting a namespace node per file would create one duplicate component per
// file sharing a namespace, which in this corpus is 92 files for StaxRip.UI
// alone.
func emit(out *[]types.EntityRecord, n *vbnet.Node, filePath, repoRoot string, ownerIdx int, ns string) {
	for _, child := range n.Children {
		if child.Kind == vbnet.NodeNamespace {
			inner := child.Name
			if ns != "" && inner != "" {
				inner = ns + "." + inner
			}
			emit(out, child, filePath, repoRoot, ownerIdx, inner)
			continue
		}

		kind, subtype, ok := entityKind(child)
		if !ok {
			// Accessors, enum members, imports and Option headers carry no
			// entity of their own. Their call sites belong to the nearest
			// declaration that does have one, which is what ownerIdx is.
			appendCalls(&(*out)[ownerIdx], child)
			emit(out, child, filePath, repoRoot, ownerIdx, ns)
			continue
		}

		name := declName(child)
		// S7a (#6327) — a `Partial` type declared in a designer half is
		// emitted under its `Foo.vb` sibling so both halves derive one
		// graph.EntityID and the existing fold merges them. See partial.go for
		// why the anchor is per-file, why the sibling must exist, and why the
		// rewritten half's span is dropped rather than kept.
		recFile, rewritten := partialAnchor(filePath, repoRoot, child)
		startLine, endLine := child.Span.StartLine, child.Span.EndLine
		if rewritten {
			startLine, endLine = 0, 0
		}
		rec := types.EntityRecord{
			Name:       name,
			Kind:       kind,
			Subtype:    subtype,
			SourceFile: recFile,
			Language:   lang,
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  signatureOf(child),
		}
		if ns != "" {
			rec.Properties = map[string]string{"vbnet_namespace": ns}
		}
		if child.Kind.IsType() {
			rec.Relationships = append(rec.Relationships, hierarchyEdges(child)...)
		}
		appendCalls(&rec, child)

		idx := len(*out)
		*out = append(*out, rec)

		// CONTAINS from the owner, using a structural ref keyed on
		// (file, name) so it binds without needing the child's hex id at
		// emit time. The file carrier owns top-level declarations, which is
		// why ownerIdx starts at 0.
		(*out)[ownerIdx].Relationships = append((*out)[ownerIdx].Relationships,
			types.RelationshipRecord{
				ToID: containsRef(kind, recFile, name),
				Kind: "CONTAINS",
				Properties: types.Props{
					{K: "line", V: strconv.Itoa(child.Span.StartLine)},
				},
			})

		emit(out, child, filePath, repoRoot, idx, ns)
	}
}

// entityKind maps a declaration onto its grafel kind/subtype pair, and reports
// false for the declarations that deliberately get no entity of their own.
//
// The mapping follows the C# extractor so a mixed .NET repo indexes uniformly:
// types are SCOPE.Component, enums are SCOPE.Schema (csharp.go:353), callable
// members are SCOPE.Operation (csharp.go:428) and data members are
// SCOPE.Schema.
func entityKind(n *vbnet.Node) (kind, subtype string, ok bool) {
	switch n.Kind {
	case vbnet.NodeClass, vbnet.NodeModule, vbnet.NodeStructure, vbnet.NodeInterface:
		return "SCOPE.Component", n.Keyword, true
	case vbnet.NodeDelegate:
		return "SCOPE.Component", "delegate", true
	case vbnet.NodeEnum:
		return "SCOPE.Schema", "enum", true
	case vbnet.NodeMethod:
		if n.Constructor {
			return "SCOPE.Operation", "constructor", true
		}
		return "SCOPE.Operation", n.Keyword, true
	case vbnet.NodeProperty:
		return "SCOPE.Operation", "property", true
	case vbnet.NodeEvent:
		return "SCOPE.Operation", "event", true
	case vbnet.NodeField:
		return "SCOPE.Schema", "field", true
	case vbnet.NodeConst:
		return "SCOPE.Schema", "const", true
	}
	return "", "", false
}

// containsRef returns the structural ref for a CONTAINS edge pointing at a
// child declaration. The trailing segment must equal the child's Name exactly:
// the resolver's byLocation fallback binds on (SourceFile, Name).
func containsRef(kind, filePath, name string) string {
	switch kind {
	case "SCOPE.Component":
		return extractor.BuildComponentStructuralRef(lang, filePath, name)
	case "SCOPE.Operation":
		return extractor.BuildOperationStructuralRef(lang, filePath, name)
	default:
		return extractor.BuildSchemaFieldStructuralRef(lang, filePath, name)
	}
}

// declName returns the name this declaration is emitted under.
//
// It follows the C# extractor exactly (csharp.go:303 vs :420): a TYPE is
// emitted under its BARE name, a MEMBER under `<enclosing type>.<member>`.
// Namespaces never appear — they are stamped as a property instead.
//
// The bare-name rule for types is not cosmetic, it is what makes hierarchy
// edges bind. `Inherits LabelBlock` names a class nested inside `SimpleUI`,
// and VB.NET resolves that unqualified: emitting the type as
// `SimpleUI.LabelBlock` leaves the edge dangling. MEASURED by building the
// 302-file corpus both ways and counting hex ToIDs after ReferencesEmbedded:
// prefixing nested types with their container costs 15 EXTENDS, 4 IMPLEMENTS
// and 20 CALLS their resolution (EXTENDS 153 -> 138, IMPLEMENTS 15 -> 11,
// CALLS 3,386 -> 3,366), across EmptyBlock, LabelBlock, TextBlock,
// TextButtonBlock, InfError, InfLine, IFileDialog, IFileOpenDialog,
// IFileSaveDialog, IKnownFolderManager, IModalWindow and ISimpleUIControl.
//
// The cost is the C# extractor's cost too: two same-named types in one file —
// a nested type shadowing its container — hash to one graph.EntityID.
func declName(n *vbnet.Node) string {
	if n.Kind.IsType() || n.Kind == vbnet.NodeDelegate {
		return n.Name
	}
	for x := n.Parent(); x != nil; x = x.Parent() {
		if x.Kind.IsType() && x.Name != "" {
			return x.Name + "." + n.Name
		}
	}
	return n.Name
}

// signatureOf renders a compact declaration signature.
func signatureOf(n *vbnet.Node) string {
	var b strings.Builder
	b.WriteString(n.Name)
	if len(n.TypeParams) > 0 {
		b.WriteString("(Of " + strings.Join(n.TypeParams, ", ") + ")")
	}
	if n.Kind == vbnet.NodeMethod || n.Kind == vbnet.NodeProperty ||
		n.Kind == vbnet.NodeDelegate || n.Kind == vbnet.NodeEvent {
		parts := make([]string, 0, len(n.Params))
		for _, p := range n.Params {
			s := p.Name
			if p.TypeName != "" {
				s += " As " + p.TypeName
			}
			parts = append(parts, s)
		}
		b.WriteString("(" + strings.Join(parts, ", ") + ")")
	}
	if n.TypeName != "" {
		b.WriteString(" As " + n.TypeName)
	}
	return b.String()
}

// hierarchyEdges builds the EXTENDS/IMPLEMENTS edges for one type.
//
// FromID is deliberately left EMPTY — see the package doc. The ToID is the
// base type name as written, minus a `Global.` root qualifier and minus the
// `(Of ...)` type-argument list, so `SettingBag(Of T)` and
// `IComparable(Of Command)` fold onto the declarations they name. The dotted
// prefix is KEPT (`System.Windows.Forms.Form`), because it is the only signal
// that separates a framework base from an in-tree one, and
// classifyDispositionLang's vbnet arm reads it.
func hierarchyEdges(n *vbnet.Node) []types.RelationshipRecord {
	line := strconv.Itoa(n.Span.StartLine)
	var out []types.RelationshipRecord
	seen := map[string]bool{}
	add := func(kind string, raw string) {
		target := baseTypeName(raw)
		if target == "" || seen[kind+":"+target] {
			return
		}
		seen[kind+":"+target] = true
		out = append(out, types.RelationshipRecord{
			// FromID intentionally empty: assembly stamps the owning TYPE.
			ToID: target,
			Kind: kind,
			Properties: types.Props{
				{K: "line", V: line},
			},
		})
	}
	for _, raw := range n.Inherits {
		add("EXTENDS", raw)
	}
	for _, raw := range n.Implements {
		add("IMPLEMENTS", raw)
	}
	return out
}

// baseTypeName normalises an Inherits/Implements operand.
func baseTypeName(s string) string {
	s = strings.TrimSpace(s)
	// `(Of ...)` may nest (`SettingBag(Of List(Of T))`); everything from the
	// first paren on is type arguments, never part of the name.
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	// `Global.` is a root-namespace escape, not a namespace segment. VB.NET is
	// case-insensitive, so the prefix is matched folded.
	if len(s) > 7 && vbnet.FoldName(s[:7]) == "global." {
		s = s[7:]
	}
	return strings.TrimSpace(s)
}

// importRelationships builds the IMPORTS edges for the file carrier.
//
// `Imports System.Text`        → source_module "System.Text", local_name "Text"
// `Imports IO = System.IO`     → source_module "System.IO",   local_name "IO"
//
// A VB.NET `Imports` names a NAMESPACE, not a member, which is why
// source_module carries the whole path rather than the C# extractor's
// split-at-the-last-dot shape (csharp.go:485): splitting would claim
// `Imports System.Text` binds the local name `Text` to a member `Text` of
// module `System`, and no such member exists.
func importRelationships(filePath string, root *vbnet.Node) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	seen := map[string]bool{}
	root.Walk(func(n *vbnet.Node) {
		if n.Kind != vbnet.NodeImport {
			return
		}
		module, local := n.Name, n.Name
		if n.Target != "" {
			// Aliased: Name is the alias, Target is the right-hand side.
			module = n.Target
		} else if dot := strings.LastIndexByte(local, '.'); dot >= 0 {
			local = local[dot+1:]
		}
		module, local = strings.TrimSpace(module), strings.TrimSpace(local)
		if module == "" || local == "" || seen[local+"\x00"+module] {
			return
		}
		seen[local+"\x00"+module] = true
		out = append(out, types.RelationshipRecord{
			// The one place a path-valued FromID is correct: BuildImportTable
			// keys the per-file binding on it (imports.go:216-226), and
			// looksLikeSourceFilePath routes the endpoint to Dynamic.
			FromID: filePath,
			ToID:   module,
			Kind:   "IMPORTS",
			// types.Props is a SORTED slice binary-searched by Props.Get
			// (types/props.go:67); keys must ascend or they read back absent.
			Properties: types.Props{
				{K: "imported_name", V: local},
				{K: "line", V: strconv.Itoa(n.Span.StartLine)},
				{K: "local_name", V: local},
				{K: "source_module", V: module},
			},
		})
	})
	return out
}

// appendCalls adds one CALLS edge per distinct target among this node's own
// use sites. Descendants are NOT walked here: emit recurses, and a declaration
// that gets its own record must own its own calls.
func appendCalls(rec *types.EntityRecord, n *vbnet.Node) {
	seen := map[string]bool{}
	for _, r := range rec.Relationships {
		if r.Kind == "CALLS" {
			seen[r.ToID] = true
		}
	}
	for _, ref := range n.Refs {
		if !ref.IsCall() {
			continue
		}
		target, ok := callTarget(ref)
		if !ok || seen[target] {
			continue
		}
		seen[target] = true
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			// FromID intentionally empty: assembly stamps the owning member.
			ToID: target,
			Kind: "CALLS",
			Properties: types.Props{
				{K: "line", V: strconv.Itoa(ref.Line)},
			},
		})
	}
}

// callTarget renders the CALLS ToID for one use site, and reports false when
// the site must not produce an edge at all.
//
// Me / MyClass / MyBase are not nameable entities — they denote the current
// instance — so they are stripped and the member resolves as a bare name, the
// same answer vbnet's own classifier already reached (parenrefs.go classify
// routes Me.Foo through the ENCLOSING TYPE's scope, not the local one).
// Keeping the prefix would guarantee a dangling edge.
//
// The Qualified-with-empty-Qualifier refusal is the load-bearing half. Such a
// site's receiver is a With-block target or an expression result: real, but
// unnameable by a per-file pass. Resolving its member against this file's
// declarations is exactly the confidently-wrong edge #6327 exists to prevent.
func callTarget(r vbnet.Ref) (string, bool) {
	if r.Qualified && strings.TrimSpace(r.Qualifier) == "" {
		return "", false
	}
	switch vbnet.FoldName(r.Qualifier) {
	case "", "me", "myclass", "mybase":
		return r.Name, r.Name != ""
	}
	return r.Qualifier + "." + r.Name, r.Name != ""
}
