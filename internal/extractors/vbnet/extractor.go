// Package vbnet lifts VB.NET source into grafel entities and edges.
//
// It is S5 of #6327, requested in #6321. The parsing is done by internal/vbnet
// — the hand-written recursive-descent line scanner shipped in S4 (#6381) —
// and this package is a projection of that containment tree onto EntityRecords
// plus the four edge kinds the epic names: EXTENDS, IMPLEMENTS, IMPORTS, CALLS,
// and — since S7b — REFERENCES for `Handles` and `AddressOf` wiring.
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
//   - (LIFTED by S7b, #6327.) `AddressOf Foo` carries no parentheses, so the
//     reference pass never saw it. It is now scanned separately and emitted
//     as REFERENCES — see references.go for why that kind and that direction.
//   - (LIFTED by S7c, #6327.) Method/property/event-level
//     `Implements IFoo.Bar` was parsed onto Node.Implements and deliberately
//     not emitted, on the argument that a member-level edge to `IFoo.Bar`
//     would COMPETE with the type-level edge to `IFoo`. That argument does
//     not survive contact with how grafel identifies an edge. See
//     memberImplementsEdges for the answer and for what replaced it.
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
	appendReferences(&out[0], res.File)
	// The sibling cache is per-extraction: it is never shared across files, so
	// concurrent extraction needs no synchronisation, and a designer file
	// declaring several partial types PARSES its sibling once. The directory
	// listing that finds the sibling is not cached — see siblingCache.declares.
	emit(&out, res.File, filePath, repoRoot, 0, "", siblingCache{})
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
func emit(out *[]types.EntityRecord, n *vbnet.Node, filePath, repoRoot string, ownerIdx int, ns string, sib siblingCache) {
	for _, child := range n.Children {
		if child.Kind == vbnet.NodeNamespace {
			inner := child.Name
			if ns != "" && inner != "" {
				inner = ns + "." + inner
			}
			emit(out, child, filePath, repoRoot, ownerIdx, inner, sib)
			continue
		}

		kind, subtype, ok := entityKind(child)
		if !ok {
			// Accessors, enum members, imports and Option headers carry no
			// entity of their own. Their call sites belong to the nearest
			// declaration that does have one, which is what ownerIdx is.
			appendCalls(&(*out)[ownerIdx], child)
			appendReferences(&(*out)[ownerIdx], child)
			emit(out, child, filePath, repoRoot, ownerIdx, ns, sib)
			continue
		}

		name := declName(child)
		// S7a (#6327) — a `Partial` type declared in a designer half is
		// emitted under its `Foo.vb` sibling so both halves derive one
		// graph.EntityID and the existing fold merges them. See partial.go for
		// why the anchor is per-file, why the sibling must declare the same
		// type in the same namespace, and why the rewritten half's span is
		// dropped rather than kept.
		recFile, rewritten := partialAnchor(filePath, repoRoot, ns, child, sib)
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
		} else {
			// S7c (#6327): member-level `Implements IFoo.Bar`.
			rec.Relationships = append(rec.Relationships, memberImplementsEdges(child)...)
		}
		appendCalls(&rec, child)
		// S7b (#6327): `Handles` clauses and `AddressOf` operands.
		appendReferences(&rec, child)

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

		emit(out, child, filePath, repoRoot, idx, ns, sib)
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

// memberImplementsEdges builds the IMPLEMENTS edges for ONE member —
// the method, property or event carrying an `Implements IFoo.Bar` clause.
//
// # Why this is IMPLEMENTS and not a new kind, and why it does not compete
//
// The prior decision (recorded in the package doc, now lifted) was that a
// member edge to `IFoo.Bar` would COMPETE with the type edge to `IFoo`.
// It does not, and the reason is mechanical rather than a judgement call:
// the two edges share NEITHER endpoint. The type edge is stamped on the TYPE
// record and points at `IFoo`; this one is stamped on the MEMBER record —
// FromID stays empty and assembly substitutes the owning member (see
// cmd/grafel/index.go, where the relationship loop stamps the id computed for
// THAT record, so a member record can only ever receive the member's id) —
// and points at `IFoo.Bar`. Two edges that agree on neither endpoint cannot
// fold into or shadow one another whatever the storage layer keys on;
// internal/graph/graph.go states outright that (from, to, kind) is NOT a
// unique edge key, and this argument deliberately does not rest on it being
// one. The fear would be real in a graph that keyed IMPLEMENTS by owning
// TYPE alone; this one does not.
//
// Nor is the member edge a weaker restatement of the type edge. It is the
// only thing in the graph that says WHICH member satisfies WHICH interface
// member — a question the type edge cannot express, and the question a
// VB.NET codebase actually answers explicitly, because VB.NET requires the
// clause rather than inferring satisfaction from the signature. Measured on
// the 302-file corpus: 137 member-level clauses against 65 type-level ones
// (naming 73 types), so the form suppressed here was TWICE the form emitted.
//
// A distinct kind (IMPLEMENTS_MEMBER) was considered and rejected for the
// reason S7b rejected HANDLES in references.go: a VB-only kind is invisible
// to every existing traversal, query and quality gate, and would have to be
// threaded through types.AllRelationshipKinds, the producer-boundary scan
// and the dashboard for one language. GRPC_IMPLEMENTS is not a precedent
// against that — it exists because its target is a different entity FAMILY
// (a GrpcMethod spec node), not because its source is a member.
//
// What the objection does earn is a way to TELL THE TWO APART without
// resolving either endpoint, since an unresolved stub carries no kind. That
// is the `via` property, the same discriminator appendReferences stamps.
// types.Props is a SORTED slice binary-searched by Props.Get
// (types/props.go:67), so "line" must precede "via" or both read back absent.
//
// The ToID follows the SAME normalisation rules baseTypeName applies to the
// type-level clause — `(Of ...)` arguments trimmed, `Global.` stripped,
// dotted prefix KEPT — but it cannot reuse baseTypeName itself. That
// function trims from the FIRST `(` to the end of the string, which is
// right when the operand IS a type name and wrong here: on
// `IComparable(Of Command).CompareTo` it would drop `.CompareTo` and
// silently produce the TYPE-level target, i.e. exactly the duplicate the
// old objection feared. memberTargetName removes the BALANCED argument
// group and keeps the tail.
//
// # Resolution, stated rather than assumed
//
// A member-level ToID names an OPERATION, but internal/resolve/refs.go:2028
// routes EXTENDS/IMPLEMENTS to componentKindFamily and refs.go:3526 keeps
// the package-scoped fallback Component-only on purpose. Emitting the edge
// does not change that, and this slice does not touch internal/resolve.
// The consequence is measured, not guessed, by
// TestCorpusMemberImplements_6327.
func memberImplementsEdges(n *vbnet.Node) []types.RelationshipRecord {
	if len(n.Implements) == 0 {
		return nil
	}
	// Stamped with the MEMBER's declaration line, not the clause's: vbnet.Node
	// records Implements as a []string with no positions — the same limit
	// hierarchyEdges and appendReferences already document. The clause closes
	// the signature, so it is on or just after that line in every case.
	line := strconv.Itoa(n.Span.StartLine)
	var out []types.RelationshipRecord
	// No dedup, deliberately, and unlike hierarchyEdges. A type may name the
	// same interface once per clause across several clauses, so type-level
	// dedup is reachable; a MEMBER cannot implement the same interface member
	// twice — VB.NET rejects it outright — so a `seen` set here would be a
	// branch no legal input can enter and no test can falsify. An earlier
	// revision carried one; it was deleted rather than left unfalsifiable.
	for _, raw := range n.Implements {
		target := memberTargetName(raw)
		if target == "" {
			continue
		}
		out = append(out, types.RelationshipRecord{
			// FromID intentionally empty: assembly stamps the owning MEMBER.
			ToID: target,
			Kind: "IMPLEMENTS",
			Properties: types.Props{
				{K: "line", V: line},
				{K: "via", V: "implements-member"},
			},
		})
	}
	return out
}

// memberTargetName normalises a member-level `Implements` operand.
//
// Unlike baseTypeName it must survive text AFTER the type-argument list,
// because the operand is `Interface.Member` and the arguments sit in the
// middle: `IComparable(Of Command).CompareTo` -> `IComparable.CompareTo`.
// Groups may nest (`IEnumerable(Of List(Of T)).GetEnumerator`), so they are
// removed by depth rather than by index.
//
// It also unescapes `[...]`, which baseTypeName did not and which was a
// permanently dud edge rather than a cosmetic flaw: scanutil.go's takeIdent
// strips the brackets off every DECLARED name, so a target left as
// `IFrameServer.[Error]` names nothing in the graph and can never resolve.
// VB keyword-named members — Error, Stop, Class, Date, New — are routinely
// escaped, and either half may be: `IFoo.[Error]` and `[IFoo].Bar` are both
// idiomatic.
func memberTargetName(s string) string {
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteByte(s[i])
			}
		}
	}
	out := stripIdentEscapes(strings.TrimSpace(b.String()))
	// `Global.` is a root-namespace escape, not a namespace segment; VB.NET is
	// case-insensitive, so it is matched folded. Same rule as baseTypeName.
	if len(out) > 7 && vbnet.FoldName(out[:7]) == "global." {
		out = out[7:]
	}
	return strings.TrimSpace(out)
}

// stripIdentEscapes removes VB.NET's `[...]` identifier escapes from a dotted
// name, per segment: `[IFoo].[Error]` -> `IFoo.Error`. Brackets carry no other
// meaning in a type or member name, so removing the bytes is the whole rule.
func stripIdentEscapes(s string) string {
	if !strings.ContainsAny(s, "[]") {
		return s
	}
	return strings.NewReplacer("[", "", "]", "").Replace(s)
}

// baseTypeName normalises an Inherits/Implements operand.
func baseTypeName(s string) string {
	s = strings.TrimSpace(s)
	// `(Of ...)` may nest (`SettingBag(Of List(Of T))`); everything from the
	// first paren on is type arguments, never part of the name.
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	// Same unescaping memberTargetName does, for the same reason: takeIdent
	// strips `[...]` off declared names, so `Inherits [Global]` or
	// `Implements [IFoo]` would otherwise name nothing. Less exposed than the
	// member case — a bracket-escaped TYPE name is rarer than a bracket-escaped
	// member — but wrong in exactly the same way.
	s = stripIdentEscapes(strings.TrimSpace(s))
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
	// The Me/MyClass/MyBase fold is shared with the S7b REFERENCES edges
	// (references.go), so the two edge kinds cannot drift apart on it.
	target := memberTarget(r.Qualifier, r.Name)
	return target, target != ""
}
