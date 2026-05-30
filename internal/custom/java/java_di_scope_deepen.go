// java_di_scope_deepen.go — DI scope deepening for Micronaut @Requires + JAX-RS @ConversationScoped (#3347).
//
// Delivers di_scope_resolution deepening for two frameworks:
//
//	lang.java.framework.micronaut
//	  @Requires(property="...", value="...") — conditional bean registration
//	  guard. Emits a SCOPE.Pattern with condition_kind=Requires and the parsed
//	  property key + expected value so the graph reflects WHICH beans are
//	  conditionally activated.
//
//	lang.java.framework.jaxrs (CDI conversation scope)
//	  @ConversationScoped on a CDI-managed class declares that the bean has
//	  conversation scope (persists across multiple HTTP requests within a user
//	  session conversation). Emits a SCOPE.Component with scope=conversation.
//
// Both cells are upgrades on the existing partial di_scope_resolution records.
// Micronaut already tracks @Singleton/@Prototype scope from micronaut.go; this
// file adds the @Requires conditional. JAX-RS has no di_scope_resolution cell
// yet — this adds it at partial (single-file annotation detection only; CDI
// conversation lifecycle management requires runtime wiring).
package java

import "regexp"

// ── framework gates ───────────────────────────────────────────────────────────

var diScopeMicronautFrameworks = map[string]bool{
	"micronaut": true, "micronaut-core": true, "micronaut_core": true,
}

var diScopeJaxrsFrameworks = map[string]bool{
	"jaxrs": true, "jax-rs": true,
	"microprofile": true, "eclipse-microprofile": true,
	"jakarta_ee": true, "jakarta-ee": true, "jakartaee": true,
	"java_ee": true, "javaee": true,
	"open_liberty": true, "payara": true,
}

// ── Micronaut @Requires regexes ───────────────────────────────────────────────

// diRequiresPropRE matches @Requires(property="p.key", value="v") on a class.
// Group 1 = property expression (full value inside parens), Group 2 = class name.
var diRequiresPropRE = regexp.MustCompile(
	`(?s)@Requires\s*\(([^)]+)\)\s*` +
		`(?:@\w+(?:\([^)]*\))?\s*)*` +
		`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

// diRequiresPropertyKeyRE extracts property="…" from the Requires args.
var diRequiresPropertyKeyRE = regexp.MustCompile(`property\s*=\s*"([^"]+)"`)

// diRequiresPropertyValueRE extracts value="…" from the Requires args (expected value).
var diRequiresPropertyValueRE = regexp.MustCompile(`(?:,\s*)?value\s*=\s*"([^"]+)"`)

// diRequiresNotEnvRE extracts notEnv="…" from the Requires args.
var diRequiresNotEnvRE = regexp.MustCompile(`notEnv\s*=\s*"([^"]+)"`)

// diRequiresEnvRE extracts env="…" from the Requires args.
var diRequiresEnvRE = regexp.MustCompile(`(?:^|,\s*)env\s*=\s*"([^"]+)"`)

// diRequiresBeanRE extracts bean=ClassName.class from the Requires args.
var diRequiresBeanRE = regexp.MustCompile(`bean\s*=\s*(\w+)\.class`)

// ── JAX-RS / CDI @ConversationScoped regex ────────────────────────────────────

// diConversationScopedRE matches @ConversationScoped on a class.
// Group 1 = class name.
var diConversationScopedRE = regexp.MustCompile(
	`(?s)@ConversationScoped\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

// ── Extractor ─────────────────────────────────────────────────────────────────

// ExtractJavaDIScopeDeepen runs the DI scope deepening extractor.
func ExtractJavaDIScopeDeepen(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)

	// ── Micronaut @Requires ──────────────────────────────────────────────────

	if diScopeMicronautFrameworks[ctx.Framework] {
		for _, m := range diRequiresPropRE.FindAllStringSubmatchIndex(source, -1) {
			argsText := source[m[2]:m[3]]
			cls := source[m[4]:m[5]]

			propKey := ""
			if pk := diRequiresPropertyKeyRE.FindStringSubmatch(argsText); pk != nil {
				propKey = pk[1]
			}
			propValue := ""
			if pv := diRequiresPropertyValueRE.FindStringSubmatch(argsText); pv != nil {
				propValue = pv[1]
			}
			env := ""
			if ev := diRequiresEnvRE.FindStringSubmatch(argsText); ev != nil {
				env = ev[1]
			}
			notEnv := ""
			if nev := diRequiresNotEnvRE.FindStringSubmatch(argsText); nev != nil {
				notEnv = nev[1]
			}
			beanType := ""
			if bt := diRequiresBeanRE.FindStringSubmatch(argsText); bt != nil {
				beanType = bt[1]
			}

			ref := "scope:pattern:micronaut_requires:" + fp + ":" + cls
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: cls, Kind: "SCOPE.Pattern", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_MICRONAUT_REQUIRES",
				Ref:        ref,
				Properties: map[string]any{
					"condition_kind": "Requires",
					"property_key":   propKey,
					"property_value": propValue,
					"env":            env,
					"not_env":        notEnv,
					"required_bean":  beanType,
					"framework":      ctx.Framework,
				},
			})
		}
	}

	// ── JAX-RS / CDI @ConversationScoped ────────────────────────────────────

	if diScopeJaxrsFrameworks[ctx.Framework] {
		for _, m := range diConversationScopedRE.FindAllStringSubmatchIndex(source, -1) {
			cls := source[m[2]:m[3]]
			ref := "scope:component:cdi_conversation_scoped:" + fp + ":" + cls
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: cls, Kind: "SCOPE.Component", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_CDI_CONVERSATION_SCOPED",
				Ref:        ref,
				Properties: map[string]any{
					"scope":     "conversation",
					"framework": ctx.Framework,
				},
			})
		}
	}

	return result
}
