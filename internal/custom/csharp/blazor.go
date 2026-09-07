package csharp

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_csharp_blazor", &blazorExtractor{})
}

type blazorExtractor struct{}

func (e *blazorExtractor) Language() string { return "custom_csharp_blazor" }

var (
	reBlazorPage = regexp.MustCompile(
		`(?m)^@page\s+"([^"]+)"`,
	)
	reBlazorInject = regexp.MustCompile(
		`(?m)^@inject\s+(\w+(?:<[^>]+>)?)\s+(\w+)`,
	)
	// reBlazorCodeMethod matches a METHOD DECLARATION in a Blazor `@code`
	// block or `.razor.cs` code-behind.
	//
	// #6975. The previous pattern was
	//
	//	(?m)(?:private|protected|public|internal)?\s+(?:async\s+)?(?:Task|void|[\w<>\[\]]+)\s+(\w+)\s*\(
	//
	// with the access modifier OPTIONAL and no anchor, so it matched anywhere
	// on a line — including the middle of an expression. `var svc = new
	// OrderService(` therefore parsed as "type `new`, method `OrderService`"
	// and minted a SCOPE.Operation/function named `OrderService`, colliding in
	// the repo-wide byName index with the real class of that name. On the
	// `aspnetcore-mvc` corpus repo (macOS/APFS, gate ON via
	// GRAFEL_INPROC_CUSTOM_EXTRACTORS, in-process Index()) this rule alone
	// produced 21,320 entities in a repo with no Blazor at all.
	//
	// TIGHTENING 1 — AN ANCHOR, NOT A REQUIRED MODIFIER. A C# declaration
	// begins its own line (only indentation precedes it), so `^[ \t]*`
	// excludes every MID-EXPRESSION `new X(` while still admitting the
	// modifier-less members that are legal inside a `@code` block
	// (`void Increment()`), which a required access modifier would drop.
	//
	// THE CORPUS DOES NOT ADJUDICATE THAT CHOICE, and an earlier revision of
	// this comment claimed it did — it cited 110 sites for the anchor against
	// 97 for a required-modifier variant and concluded the modifier "drops
	// exactly the bare `@code` methods". Once tightening 2 lands, the two
	// variants select the IDENTICAL 97 sites over the 105 `.razor` files in
	// archigraph-corpora: 28 distinct names, zero sites found by either one
	// alone, in either direction. Every real method in that corpus carries an
	// access modifier. The anchor is preferred on mechanism, not on a
	// measured margin: it rejects a match for being in the wrong POSITION,
	// which is the actual defect, where a required modifier is a proxy that
	// merely correlates with it on code that follows one convention. The
	// modifier-less case is therefore pinned synthetically
	// (TestBlazorGateAdmitsRazorCodeBehind6975), not by the corpus.
	//
	// TIGHTENING 2 — THE ANCHOR IS NOT ENOUGH ON ITS OWN, AND AN EARLIER
	// REVISION OF THIS COMMENT CLAIMED IT WAS. An anchor only excludes `new X(`
	// when something precedes it on the line. A LINE-LEADING one — the second
	// line of `var svc =\n    new OrderService(db);`, or a bare statement — is
	// still parsed as "type `new`, method `OrderService`" and mints exactly
	// #6973's collider. Measured over the corpus C# — POPULATION: every `.cs`
	// file under the whole archigraph-corpora tree, 3,726 files — 2,503
	// line-leading `new X(` sites, plus 1,666 `return F(` (type `return`,
	// method `F`) and 293 `else if (` (type `else`, method `if`). Restricting
	// the walk to the 7 C#-bearing repos by name gives 3,721 files and 1,661
	// `return F(`; the other two counts are identical either way. Two numbers
	// for one thing in a review record is worse than either, so: the figures
	// here are the whole-tree ones. The file gate keeps those out
	// of `.cs`, but `.razor.cs` IS ordinary C#, so inside the gate the shape
	// survives untouched.
	//
	// It is not only a `.cs` problem. On the corpus `.razor` files this
	// rejection removes 13 of the anchor's 110 matches, and ENUMERATING them
	// rather than counting shows all 13 are non-declarations: 9 `else if (`
	// and 4 `await <Method>(` call sites — the latter minting operations named
	// after a method being CALLED, which is the collider shape again. No
	// declaration is lost on this corpus. The pattern therefore CAPTURES the type token as
	// group 1 and Extract rejects the match when that token is a statement or
	// expression keyword rather than a type — see blazorNonTypeKeywords. Go's
	// RE2 has no lookahead, so this cannot be expressed in the pattern itself.
	//
	// TIGHTENING 3 — the return type is `[\w\.\?]+(?:<[^;\n]*>)?(?:\[\])?`
	// rather than a fixed `Task|void|…` list, and the generic argument is
	// `[^;\n]*` rather than `[^>\n]+`, because NESTED generics are common
	// (`Task<List<Order>>`) and an inner-`>`-terminated class loses them
	// silently — a mutant run caught that regression before it shipped.
	// The `\n` in that class is load-bearing in the OTHER direction and is
	// graded separately (TestBlazorCodeMethodGenericDoesNotSpanLines6975):
	// without it, `[^;]*` is greedy across newlines, so a `<` on one line
	// reaches a `>` many lines below and invents a method out of two unrelated
	// declarations. Greediness plus the `\s+(\w+)\s*\(` tail then makes it
	// settle on the last `>` that still leaves a name and an open paren.
	//
	// WHAT EACH TIGHTENING IS WORTH, over the same 3,726 `.cs` files:
	// the old pattern found 39,009 sites, the anchor alone 23,490, and the
	// anchor plus the keyword rejection 18,853. The regex alone does NOT solve
	// the false-positive problem — the file gate in Extract does. What these
	// buy is that the collider shape does not survive INSIDE the gate, and does
	// not return the moment another dialect widens it.
	reBlazorCodeMethod = regexp.MustCompile(
		`(?m)^[ \t]*(?:(?:private|protected|public|internal|static|virtual|override|sealed|abstract|partial|async|extern|unsafe)\s+)*([\w\.\?]+)(?:<[^;\n]*>)?(?:\[\])?\s+(\w+)\s*\(`,
	)
	reBlazorComponentTag = regexp.MustCompile(
		`<([A-Z][A-Za-z0-9_]*)(?:\s+[^>]*)?>`,
	)
	reBlazorParameter = regexp.MustCompile(
		`\[Parameter\]\s*(?:public\s+)?(?:\w+(?:<[^>]+>)?)\s+(\w+)`,
	)
	reBlazorLayout = regexp.MustCompile(
		`(?m)^@layout\s+(\w+)`,
	)
	reBlazorInherits = regexp.MustCompile(
		`(?m)^@inherits\s+(\w+)`,
	)
)

// blazorNonTypeKeywords are C# tokens that can occupy reBlazorCodeMethod's
// TYPE slot on a line-leading match without the line being a declaration.
//
// #6975. `^[ \t]*` excludes a mid-expression `new X(` but not a line-leading
// one, and a line-leading `new OrderService(db);` parses as "type `new`,
// method `OrderService`" — precisely #6973's collider, an unqualified
// SCOPE.Operation named after a real class. Measured over the corpus C#:
// 2,503 line-leading `new X(`, 1,666 `return F(`, 293 `else if (`. The file
// gate keeps those out of plain `.cs`, but `.razor.cs` is ordinary C# and the
// shape survives inside the gate, so it has to be rejected on its own terms.
//
// WHY A MAP AND NOT A PATTERN: Go's RE2 has no lookahead, so "an identifier
// that is not one of these" cannot be written in the regex. That makes this map
// the MECHANISM, not a convenience list beside it, so every one of its 40
// entries is graded individually and by name —
// TestBlazorNonTypeKeywordsIsGradedAsAnExactSet6975 requires each key to be
// both REACHABLE by the pattern and SUPPRESSED by Extract. Deleting any single
// entry turns that test red. Do not add a key without letting that test see it.
//
// Only the TYPE slot is filtered. The NAME slot needs no companion filter and
// deliberately does not have one — two guards that only ever fire together
// grade neither. A keyword in the name slot is already unreachable: `if (`,
// `while (`, `foreach (`, `switch (`, `catch (`, `using (` and `lock (` put
// the paren straight after the keyword, and the pattern requires `\s+` between
// the type and the name, so those lines produce no match at any start
// position. `else if (` is the exception — the name slot holds `if` — and it
// is rejected here on its TYPE token, `else`.
var blazorNonTypeKeywords = map[string]bool{
	// Statement keywords that can lead a line and be followed by `ident(`.
	"return": true, "new": true, "else": true, "throw": true, "await": true,
	"yield": true, "goto": true, "break": true, "continue": true,
	"case": true, "default": true, "do": true, "try": true, "finally": true,
	"checked": true, "unchecked": true, "stackalloc": true, "delegate": true,
	// Contextual/operator keywords that appear in the same slot.
	"in": true, "is": true, "as": true, "out": true, "ref": true,
	"params": true, "typeof": true, "sizeof": true, "nameof": true,
	"when": true, "with": true, "and": true, "or": true, "not": true,
	// LINQ query clause keywords.
	"from": true, "select": true, "where": true, "let": true,
	"orderby": true, "group": true, "join": true, "into": true,
}

// blazorBuiltinComponents are Blazor/HTML built-in components not emitted as entities.
var blazorBuiltinComponents = map[string]bool{
	"EditForm": true, "InputText": true, "InputNumber": true, "InputDate": true,
	"InputCheckbox": true, "InputSelect": true, "InputFile": true,
	"ValidationSummary": true, "ValidationMessage": true,
	"NavLink": true, "NavMenu": true, "AuthorizeView": true,
	"CascadingAuthenticationState": true, "Router": true, "RouteView": true,
	"FocusOnNavigate": true, "Virtualize": true, "DynamicComponent": true,
}

func (e *blazorExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.blazor_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "blazor"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
		return nil, nil
	}

	// -------------------------------------------------------------------------
	// FILE GATE (#6975)
	// -------------------------------------------------------------------------
	//
	// This extractor had NO file gate. Its sibling blazor_deep.go:179 has one,
	// and every rule below describes a Blazor construct, so it ran the whole
	// Blazor rule set over EVERY C# file in the repository. Measured on the
	// `aspnetcore-mvc` corpus repo — a repo containing no Blazor — with the
	// custom-extractor gate ON (GRAFEL_INPROC_CUSTOM_EXTRACTORS=1, in-process
	// Index(), macOS/APFS): of the 35,409 entities the gate added,
	// **25,479 (72.0%) came from this file** (21,320 code-method + 4,159
	// component-tag). Those are name duplicates of real classes, they evict the
	// real entry from the repo-wide byName index, and the resulting resolution
	// failures cost 6,639 TESTS edges on that repo alone (#6973).
	//
	// WHICH EXTENSIONS THE GATE ADMITS, AND WHY — MEASURED, NOT COPIED.
	// blazor_deep.go gates on `.razor || .razor.cs`; #6975 asks explicitly not
	// to copy that without measuring, because Blazor code-behind lives in
	// `.razor.cs` and a `.razor`-only gate would trade one silent loss for
	// another. Two measurements decided it.
	//
	//  1. Which extensions carry the constructs. Scanning every `.cs`,
	//     `.razor`, `.razor.cs` and `.cshtml` file in archigraph-corpora
	//     (whole tree) with these exact patterns: the five DIRECTIVE
	//     rules (@page, @inject, [Parameter], @layout, @inherits) score
	//     **zero** across all 3,726 `.cs` files. They fire in the 105 `.razor`
	//     files (25/119/10/4/0 = 158 sites) and, to a much smaller extent, in
	//     the 1,129 `.cshtml` files (9/39/0/0/1 = 49 sites) — an earlier
	//     revision of this comment said "only in `.razor`", which was false
	//     over its own stated population. `.cshtml` is classified UNSUPPORTED
	//     (classifier/unsupported.go:132, #6343) so it never reaches an
	//     extractor either way, but the sentence is the evidence for this
	//     gate and it should be true. Only the code-method and component-tag
	//     rules fire in `.cs` — 39,009 and 12,994 sites, all of them false
	//     positives. So no rule here loses a real construct by being denied
	//     plain `.cs`.
	//
	//  2. What can actually reach this function. `.razor` classifies as
	//     language "razor" (classifier.go:345), not "csharp", so the
	//     `file.Language != "csharp"` check above rejects it outright — that
	//     check is live and load-bearing, pinned by
	//     TestBlazorLanguageGuardRejectsNonCSharp6975. Independently, there is
	//     no tree-sitter grammar for razor, so a `.razor` file also has
	//     TSTree == nil and never passes the `file.TSTree != nil` guard at
	//     cmd/grafel/index.go that admits custom extractors at all. Either
	//     alone is sufficient: **the only extension that both carries Blazor
	//     and reaches this code today is `.razor.cs`.** (The same reasoning
	//     applies to blazor_deep.go:179's `.razor` arm, which is dead for
	//     exactly this reason; out of scope here, filed separately.)
	//
	//     `.razor` is kept in the SUFFIX gate because the suffix pair is what
	//     names the Blazor file family, and blazor_deep.go:179 uses the same
	//     pair. It is NOT kept as forward-compatibility for a future razor
	//     grammar, which is what an earlier revision of this comment claimed:
	//     registering a grammar would not help, because the language check
	//     would still reject the file. Reaching `.razor` needs that check
	//     widened as well, which is a separate decision and is not taken here.
	//
	// MARKUP RULES vs CODE RULES. `.razor` and `.razor.cs` are not
	// interchangeable: `@`-directives and PascalCase component tags are Razor
	// MARKUP and cannot appear in a code-behind file, where `<Foo>` is a
	// generic type argument, not a component reference. So the markup rules
	// below are further restricted to `.razor`, and only the two code rules
	// (methods, [Parameter]) run over code-behind.
	isRazorMarkup := strings.HasSuffix(file.Path, ".razor")
	isRazorCodeBehind := strings.HasSuffix(file.Path, ".razor.cs")
	if !isRazorMarkup && !isRazorCodeBehind {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Markup-only rule (#6975): `@`-directives are Razor markup and cannot
	// appear in a `.razor.cs` code-behind file.
	if isRazorMarkup {
		// 1. @page routes -> SCOPE.Operation/endpoint
		for _, m := range reBlazorPage.FindAllStringSubmatchIndex(src, -1) {
			route := src[m[2]:m[3]]
			ent := makeEntity(route, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_PAGE",
				"route_path", route)
			add(ent)
		}

		// 2. @inject -> SCOPE.Component (injected service)
		for _, m := range reBlazorInject.FindAllStringSubmatchIndex(src, -1) {
			serviceType := src[m[2]:m[3]]
			ent := makeEntity(serviceType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_INJECT",
				"service_type", serviceType)
			add(ent)
		}
	}

	// 3. @code block methods -> SCOPE.Operation/function
	//
	// Group 1 is the TYPE token, group 2 the method name. A match whose type
	// slot holds a statement keyword is not a declaration — see
	// blazorNonTypeKeywords (#6975).
	for _, m := range reBlazorCodeMethod.FindAllStringSubmatchIndex(src, -1) {
		if blazorNonTypeKeywords[src[m[2]:m[3]]] {
			continue
		}
		name := src[m[4]:m[5]]
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_CODE_METHOD")
		add(ent)
	}

	// Markup-only rule (#6975): in `.razor.cs` code-behind `<Foo>` is a
	// generic type argument, not a component reference — 12,994 such sites
	// across the corpus's `.cs` files.
	if isRazorMarkup {
		// 4. PascalCase component tags -> SCOPE.UIComponent
		for _, m := range reBlazorComponentTag.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			if blazorBuiltinComponents[name] {
				continue
			}
			ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_COMPONENT_REF")
			add(ent)
		}
	}

	// 5. [Parameter] properties -> SCOPE.Pattern
	for _, m := range reBlazorParameter.FindAllStringSubmatchIndex(src, -1) {
		paramName := src[m[2]:m[3]]
		ent := makeEntity("param:"+paramName, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_PARAMETER",
			"parameter_name", paramName)
		add(ent)
	}

	// Markup-only rule (#6975): `@`-directives are Razor markup and cannot
	// appear in a `.razor.cs` code-behind file.
	if isRazorMarkup {
		// 6. @layout -> SCOPE.Component
		for _, m := range reBlazorLayout.FindAllStringSubmatchIndex(src, -1) {
			layoutName := src[m[2]:m[3]]
			ent := makeEntity("layout:"+layoutName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_LAYOUT")
			add(ent)
		}
	}

	// Markup-only rule (#6975): `@`-directives are Razor markup and cannot
	// appear in a `.razor.cs` code-behind file.
	if isRazorMarkup {
		// 7. @inherits -> SCOPE.Component
		for _, m := range reBlazorInherits.FindAllStringSubmatchIndex(src, -1) {
			baseName := src[m[2]:m[3]]
			ent := makeEntity("inherits:"+baseName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_INHERITS")
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
