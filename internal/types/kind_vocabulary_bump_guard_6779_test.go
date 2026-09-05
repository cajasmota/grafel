package types

// #6779 — KindVocabularyVersion's bump rule ("bump whenever an entity kind is
// RENAMED or RETIRED") was, in the first cut of this mechanism, a doc comment
// and nothing else. Nothing noticed a rename.
//
// Shipping the rule as prose reproduces this repo's dominant defect class
// INSIDE the mechanism built to prevent silent staleness: a stated invariant
// that no test observes. The irony is the argument. This guard makes the rule
// mechanical — rename or retire a kind and the build fails until someone
// consciously bumps the version and re-pins the roster below, in the same
// change.
//
// It reads kinds.go with go/parser rather than a hand-written roster, the same
// way kinds_enum_completeness_6757_test.go and internal/entkinds' sweep guard
// do: a hand-maintained list of what the constants are would be the very
// defect it guards against, one level up.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// pinnedKindVocabularyVersion is KindVocabularyVersion as of the last time the
// roster below was pinned. Move the two together, never one alone.
const pinnedKindVocabularyVersion = 1

// declaredEntityKindValues parses kinds.go and returns every string value
// declared with the explicit type EntityKind, sorted.
//
// The VALUES are what this guard pins, not the constant NAMES: renaming
// EntityKindComponent to EntityKindModule changes nothing on disk, while
// changing its value from "SCOPE.Component" to "SCOPE.Module" orphans every
// stored entity carrying the old spelling. Only the second is a vocabulary
// change.
//
// Const-block type elision is handled the same way the #6757 guard handles it:
// inside a `const (...)` group a spec with neither a type nor values repeats
// the previous spec's type and expression.
func declaredEntityKindValues(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "kinds.go", nil, 0)
	if err != nil {
		t.Fatalf("parse kinds.go: %v", err)
	}

	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		var carriedType ast.Expr
		var carriedValues []ast.Expr
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			typ, values := vs.Type, vs.Values
			if typ == nil && len(values) == 0 {
				typ, values = carriedType, carriedValues
			} else {
				carriedType, carriedValues = typ, values
			}
			ident, ok := typ.(*ast.Ident)
			if !ok || ident.Name != "EntityKind" {
				continue
			}
			for i, name := range vs.Names {
				if name.Name == "_" || i >= len(values) {
					continue
				}
				lit, ok := values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				out = append(out, lit.Value[1:len(lit.Value)-1])
			}
		}
	}
	sort.Strings(out)
	return out
}

// pinnedEntityKindVocabulary is every entity-kind STRING kinds.go declares, as
// of pinnedKindVocabularyVersion. Sorted, and each value appears exactly once —
// both observed by TestEntityKindVocabularyIsStrictlyIncreasing, because the
// comparison below is set-based and sorts its own copy, so neither property
// held itself up.
var pinnedEntityKindVocabulary = []string{
	"AgentPattern",
	"Controller",
	"Decorator",
	"Dependency",
	"Fixture",
	"Implementation",
	"Interface",
	"Middleware",
	"Migration",
	"Module",
	"Relationship",
	"SCOPE.Activity",
	"SCOPE.Channel",
	"SCOPE.ChannelBinding",
	"SCOPE.Class",
	"SCOPE.CodeBlock",
	"SCOPE.Command",
	"SCOPE.Component",
	"SCOPE.Config",
	"SCOPE.Constant",
	"SCOPE.Constraint",
	"SCOPE.CustomValidator",
	"SCOPE.DataAccess",
	"SCOPE.DataLoader",
	"SCOPE.Datastore",
	"SCOPE.DesignDecision",
	"SCOPE.Document",
	"SCOPE.Endpoint",
	"SCOPE.Enum",
	"SCOPE.Event",
	"SCOPE.EventBusEvent",
	"SCOPE.EventFlow",
	"SCOPE.EventType",
	"SCOPE.Evolution",
	"SCOPE.ExceptionType",
	"SCOPE.External",
	"SCOPE.ExternalEndpoint",
	"SCOPE.ExternalService",
	"SCOPE.FeatureFlag",
	"SCOPE.Function",
	"SCOPE.GrpcMethod",
	"SCOPE.GrpcService",
	"SCOPE.Heading",
	"SCOPE.InfraResource",
	"SCOPE.IngressHost",
	"SCOPE.JSX",
	"SCOPE.MarkdownDocument",
	"SCOPE.MessageTopic",
	"SCOPE.Model",
	"SCOPE.ModelEvent",
	"SCOPE.Operation",
	"SCOPE.Package",
	"SCOPE.Pattern",
	"SCOPE.Plugin",
	"SCOPE.Process",
	"SCOPE.Project",
	"SCOPE.Queue",
	"SCOPE.Reference",
	"SCOPE.Route",
	"SCOPE.ScheduledJob",
	"SCOPE.Schema",
	"SCOPE.ScopeUnknown",
	"SCOPE.Section",
	"SCOPE.ServerlessFunction",
	"SCOPE.Service",
	"SCOPE.State",
	"SCOPE.StateMachine",
	"SCOPE.Stylesheet",
	"SCOPE.Table",
	"SCOPE.Template",
	"SCOPE.TranslationKey",
	"SCOPE.UIComponent",
	"SCOPE.Variable",
	"SCOPE.View",
	"SCOPE.Workflow",
	"Task",
	"Test",
	"TestClass",
	"TestConfig",
	"http_endpoint",
	"http_endpoint_call",
	"http_endpoint_definition",
}

// TestEntityKindVocabularyIsStrictlyIncreasing observes the two properties the
// roster's own comment claims — sorted, and no value twice.
//
// Neither is implied by TestEntityKindVocabularyIsPinnedToItsVersion: that test
// sorts its own copy and compares SETS, so a duplicated line is invisible to it
// (scored: duplicating "Migration" left the whole package green). A duplicate
// is not cosmetic — the failure message renders a paste-back roster from the
// PARSED kinds, so a hand-edited pin that has drifted to 83 lines for 82 kinds
// silently disagrees with the artefact the guard tells you to paste, and the
// next author reconciles it by deleting the wrong one.
//
// Varies: nothing — a live observation of the pinned literal.
// Holds constant: the roster. Strict `<` is what makes ONE loop grade BOTH
// claims: `>` catches unsorted, `==` catches a duplicate.
func TestEntityKindVocabularyIsStrictlyIncreasing(t *testing.T) {
	if len(pinnedEntityKindVocabulary) < 10 {
		t.Fatalf("premise: the roster holds %d values, so this loop grades nothing",
			len(pinnedEntityKindVocabulary))
	}
	for i := 1; i < len(pinnedEntityKindVocabulary); i++ {
		prev, cur := pinnedEntityKindVocabulary[i-1], pinnedEntityKindVocabulary[i]
		if prev == cur {
			t.Errorf("pinnedEntityKindVocabulary[%d] and [%d] are both %q — the roster pins a SET, "+
				"so a duplicate changes nothing it asserts while making its own length wrong",
				i-1, i, cur)
			continue
		}
		if prev > cur {
			t.Errorf("pinnedEntityKindVocabulary is not sorted: [%d]=%q comes after [%d]=%q. "+
				"Its doc comment says sorted, and the paste-back roster the bump guard prints is",
				i, cur, i-1, prev)
		}
	}
}

// TestEntityKindVocabularyIsPinnedToItsVersion is the bump rule, enforced.
//
// A kind that DISAPPEARS from kinds.go — renamed to a new spelling, or retired
// outright — is what strands already-indexed graphs, so that case additionally
// demands the version move. A kind that is merely ADDED strands nothing (a
// query for it correctly finds nothing in an older graph, because nothing of
// that kind was ever extracted), so it needs only a re-pin.
func TestEntityKindVocabularyIsPinnedToItsVersion(t *testing.T) {
	actual := declaredEntityKindValues(t)

	// PREMISE GUARD: a parser that silently matched nothing would make every
	// comparison below trivially satisfiable by an empty pin.
	if len(actual) < 10 {
		t.Fatalf("premise: parsed only %d EntityKind values from kinds.go — the parser is not seeing the const block", len(actual))
	}

	pinned := append([]string(nil), pinnedEntityKindVocabulary...)
	sort.Strings(pinned)

	pinnedSet := make(map[string]bool, len(pinned))
	for _, k := range pinned {
		pinnedSet[k] = true
	}
	actualSet := make(map[string]bool, len(actual))
	for _, k := range actual {
		actualSet[k] = true
	}

	var removed, added []string
	for _, k := range pinned {
		if !actualSet[k] {
			removed = append(removed, k)
		}
	}
	for _, k := range actual {
		if !pinnedSet[k] {
			added = append(added, k)
		}
	}

	if len(removed) > 0 && KindVocabularyVersion <= pinnedKindVocabularyVersion {
		// The stranding case. Every graph on disk still carries these
		// spellings and nothing will ever respell them.
		t.Errorf(`%d entity kind(s) were RENAMED or RETIRED without bumping KindVocabularyVersion: %s

Every already-indexed graph still carries those spellings, and a query filtering
on the new ones returns EMPTY against a graph that looks perfectly healthy —
which is the whole of #6779.

Fix, in THIS change:
  1. bump KindVocabularyVersion in kinds.go (currently %d)
  2. set pinnedKindVocabularyVersion to the new value
  3. re-pin pinnedEntityKindVocabulary to the roster printed below`,
			len(removed), strings.Join(removed, ", "), KindVocabularyVersion)
	}

	if len(removed) > 0 || len(added) > 0 || KindVocabularyVersion != pinnedKindVocabularyVersion {
		t.Errorf(`the pinned entity-kind vocabulary is out of date (added: %v, removed: %v; KindVocabularyVersion=%d, pinned=%d).

Re-pin pinnedEntityKindVocabulary to:

%s`, added, removed, KindVocabularyVersion, pinnedKindVocabularyVersion, renderKindPin(actual))
	}
}

// renderKindPin formats the roster as the Go literal to paste back in, so the
// fix for a failing pin is a copy-paste rather than a hand-transcription.
func renderKindPin(kinds []string) string {
	var b strings.Builder
	b.WriteString("var pinnedEntityKindVocabulary = []string{\n")
	for _, k := range kinds {
		b.WriteString("\t\"")
		b.WriteString(k)
		b.WriteString("\",\n")
	}
	b.WriteString("}")
	return b.String()
}
