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
	// THE FIX IS AN ANCHOR, NOT A REQUIRED MODIFIER. A C# declaration begins
	// its own line (only indentation precedes it), so `^[ \t]*` excludes every
	// mid-expression `new X(` while still admitting the modifier-less methods
	// that are idiomatic inside a `@code` block (`void Increment()`).
	// Requiring an access modifier would have been the obvious tightening and
	// is the WRONG one: measured over the 105 `.razor` files in
	// archigraph-corpora, the old pattern finds 126 declaration sites, this
	// one finds 110 (87%), and a required-modifier variant finds only 97
	// (77%) — it drops exactly the bare `@code` methods.
	//
	// The return-type alternation is `[\w\.\?]+(?:<[^;\n]*>)?(?:\[\])?`
	// rather than a fixed `Task|void|…` list, and the generic argument is
	// `[^;\n]*` rather than `[^>\n]+`, because NESTED generics are common
	// (`Task<List<Order>>`) and an inner-`>`-terminated class loses them
	// silently — a mutant run caught that regression here before it shipped.
	// Greediness plus the `\s+(\w+)\s*\(` tail makes it settle on the last
	// `>` that still leaves a name and an open paren on the line.
	//
	// Over the 3,721 `.cs` files in the same corpora the old pattern found
	// 38,983 sites and this one still finds 23,474: the anchor alone does NOT
	// solve the false-positive problem, the file gate in Extract does. What
	// the anchor buys is that the `new X(` class does not return the moment
	// another dialect widens that gate.
	reBlazorCodeMethod = regexp.MustCompile(
		`(?m)^[ \t]*(?:(?:private|protected|public|internal|static|virtual|override|sealed|abstract|partial|async|extern|unsafe)\s+)*[\w\.\?]+(?:<[^;\n]*>)?(?:\[\])?\s+(\w+)\s*\(`,
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
	//     (7 C#-bearing repos) with these exact patterns: the five DIRECTIVE
	//     rules (@page, @inject, [Parameter], @layout, @inherits) score
	//     **zero** across all 3,721 `.cs` files and fire only in the 105
	//     `.razor` files (25/119/10/4/0). Only the code-method and
	//     component-tag rules fire in `.cs` — 38,983 and 12,991 sites, all of
	//     them false positives. So no rule here loses a real construct by
	//     being denied plain `.cs`.
	//
	//  2. What can actually reach this function. `.razor` classifies as
	//     language "razor" (classifier.go:345), not "csharp", and there is no
	//     tree-sitter grammar for it, so a `.razor` file has TSTree == nil and
	//     never passes the `file.TSTree != nil` guard at cmd/grafel/index.go
	//     that admits custom extractors at all. The language check above
	//     therefore already excludes `.razor` — **the only extension that both
	//     carries Blazor and reaches this code today is `.razor.cs`.** (The
	//     same reasoning applies to blazor_deep.go:179's `.razor` arm, which
	//     is dead for exactly this reason; out of scope here, filed separately.)
	//     `.razor` is kept in the gate so this extractor becomes correct rather
	//     than newly wrong if a razor grammar is ever registered.
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
	for _, m := range reBlazorCodeMethod.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_CODE_METHOD")
		add(ent)
	}

	// Markup-only rule (#6975): in `.razor.cs` code-behind `<Foo>` is a
	// generic type argument, not a component reference — 12,991 such sites
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
