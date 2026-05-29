package java

import "regexp"

// Dropwizard custom extractor — DI, auth, middleware, transactions, DTO, tests.
//
// Dropwizard uses Jersey (JAX-RS) for HTTP routing, Guice/HK2 for DI,
// Dropwizard-Auth for @Authenticated/@RolesAllowed, ContainerRequestFilter
// for middleware, JDBI @Transaction for transactions, and
// DropwizardAppRule/ResourceTestRule for integration tests.
//
// Coverage cells delivered (#3087):
//   - DI:          di_binding_extraction, di_injection_point, di_scope_resolution  → partial
//   - Auth:        auth_coverage                                                    → partial
//   - Middleware:  middleware_coverage                                              → partial
//   - Validation:  request_validation, dto_extraction                              → partial
//   - Transactions: transaction_boundary_extraction, transaction_propagation,
//                   transaction_rollback_rules                                      → partial
//   - Testing:     tests_linkage                                                    → partial
//   - AOP:         advice_attribution, aspect_extraction, pointcut_resolution      → not_applicable

var dropwizardFrameworks = map[string]bool{
	"dropwizard": true,
}

var (
	// DI: @Inject field/constructor injection — di_injection_point.
	dwInjectFieldRE = regexp.MustCompile(
		`(?s)@Inject\b[^;{(]*?(?:private|protected|public)\s+(?:final\s+)?` +
			`(\w+)(?:\s*<[^>]*>)?\s+\w+\s*;`)

	dwInjectConstructorRE = regexp.MustCompile(
		`(?s)@Inject\b\s*(?:public|protected|)\s+(\w+)\s*\(`)

	// DI: @Singleton / @RequestScoped scoped class — di_scope_resolution.
	dwScopedClassRE = regexp.MustCompile(
		`(?s)@(Singleton|RequestScoped|SessionScoped|PerLookup)\b` +
			`[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

	// DI: @Provides method (Guice module binding) — di_binding_extraction.
	dwProvidesMethodRE = regexp.MustCompile(
		`(?s)@Provides\b[^;{]*?(?:public|protected|private|)\s+(?:static\s+)?` +
			`(?:@\w+\s+)*(\w+)(?:\s*<[^>]*>)?\s+(\w+)\s*\(`)

	// DI: AbstractBinder.bind(...).to(...) form — di_binding_extraction.
	dwBindToRE = regexp.MustCompile(
		`(?s)\bbind\s*\(\s*(\w+)(?:\.class)?\s*\)\s*\.to\s*\(\s*(\w+)(?:\.class)?`)

	// Auth: @Authenticated filter marker on resource methods/classes — auth_coverage.
	dwAuthenticatedRE = regexp.MustCompile(
		`(?s)@Authenticated\b[^{;]*?(?:(?:public|protected|private)\s+)?` +
			`(?:(?:static|final|abstract)\s+)*` +
			`(?:(?:<[^>]*>\s+)?(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+))?(\w+)\s*[\({]`)

	// Auth: @RolesAllowed on resource methods — auth_coverage.
	dwRolesAllowedRE = regexp.MustCompile(
		`(?s)@RolesAllowed\s*\(\s*(?:\{[^}]*\}|"[^"]*")\s*\)[^{;]*?` +
			`(?:(?:public|protected|private)\s+)?` +
			`(?:(?:static|final)\s+)*` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)(\w+)\s*\(`)

	// Auth: @PermitAll on resource methods — auth_coverage.
	dwPermitAllRE = regexp.MustCompile(
		`(?s)@PermitAll\b[^{;]*?(?:(?:public|protected|private)\s+)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)(\w+)\s*\(`)

	// Middleware: ContainerRequestFilter implementation — middleware_coverage.
	dwContainerFilterRE = regexp.MustCompile(
		`(?s)class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*` +
			`(ContainerRequestFilter|ContainerResponseFilter)\b`)

	// Middleware: @Provider annotation on filter class.
	dwProviderRE = regexp.MustCompile(`@Provider\b`)

	// Middleware: @Priority annotation on filter classes.
	dwPriorityRE = regexp.MustCompile(`@Priority\s*\(\s*(\d+)\s*\)`)

	// Transactions: JDBI @Transaction — transaction_boundary_extraction.
	dwJDBITransactionRE = regexp.MustCompile(
		`(?s)@Transaction\b\s*(?:\(([^)]*)\))?\s*` +
			`(?:(?:public|protected|private|static|final|abstract|synchronized|default)\s+)*` +
			`(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)` +
			`(\w+)\s*\(`)

	// Transactions: JDBI @Transaction on DAO interface/class.
	dwJDBITransactionClassRE = regexp.MustCompile(
		`(?s)@Transaction\b\s*(?:\(([^)]*)\))?\s*` +
			`(?:(?:public|protected|private|abstract|final)\s+)*` +
			`(?:class|interface)\s+(\w+)`)

	// JDBI SQL annotation — di_binding_extraction evidence (DAO factories).
	dwSqlQueryRE = regexp.MustCompile(
		`(?s)@(SqlQuery|SqlUpdate|SqlBatch|SqlCall)\b[^;{]*?` +
			`(?:public|protected|private|)\s+(?:abstract\s+)?` +
			`(?:[\w<>\[\], ]+\s+)(\w+)\s*\(`)

	// Tests: DropwizardAppRule / DropwizardExtension field — tests_linkage.
	dwAppRuleRE = regexp.MustCompile(
		`(?s)(?:DropwizardAppRule|DropwizardExtensionsSupport|DropwizardExtension|` +
			`AppRuleConfigOverride)\s*<[^>]*>\s+(\w+)`)

	// Tests: ResourceTestRule field — tests_linkage.
	dwResourceTestRuleRE = regexp.MustCompile(
		`(?s)ResourceTestRule(?:\s*<[^>]*>)?\s+(\w+)`)

	// Tests: @ExtendWith(DropwizardExtensionsSupport.class).
	dwExtendWithDropwizardRE = regexp.MustCompile(
		`@ExtendWith\s*\([^)]*DropwizardExtensionsSupport\s*\.class`)
)

// ExtractDropwizard runs the Dropwizard-specific extractor.
func ExtractDropwizard(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !dropwizardFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// -------------------------------------------------------------------------
	// DI: @Provides bindings (Guice module) — di_binding_extraction.
	// -------------------------------------------------------------------------
	for _, m := range dwProvidesMethodRE.FindAllStringSubmatchIndex(source, -1) {
		returnType := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		ref := "scope:operation:dw_provides:" + fp + ":" + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_GUICE_PROVIDES", Ref: ref,
			Properties: map[string]any{
				"provides_type": returnType,
				"framework":     "dropwizard",
				"di_pattern":    "guice_provides",
			},
		})
	}

	// .bind(X.class).to(Y.class) module bindings — di_binding_extraction.
	for _, m := range dwBindToRE.FindAllStringSubmatchIndex(source, -1) {
		impl := source[m[2]:m[3]]
		iface := source[m[4]:m[5]]
		ref := "scope:pattern:dw_bind:" + fp + ":" + impl + "_to_" + iface
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: impl + " → " + iface, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_GUICE_BIND", Ref: ref,
			Properties: map[string]any{
				"impl_type":  impl,
				"iface_type": iface,
				"framework":  "dropwizard",
				"di_pattern": "guice_bind_to",
			},
		})
	}

	// -------------------------------------------------------------------------
	// DI: @Inject field injection — di_injection_point.
	// -------------------------------------------------------------------------
	for _, m := range dwInjectFieldRE.FindAllStringSubmatchIndex(source, -1) {
		injectedType := source[m[2]:m[3]]
		if primitiveTypes[injectedType] {
			continue
		}
		ref := "scope:dependency:dw_inject_field:" + fp + ":" + injectedType
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: injectedType, Kind: "SCOPE.Dependency", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_INJECT_FIELD", Ref: ref,
			Properties: map[string]any{
				"injected_type": injectedType,
				"framework":     "dropwizard",
				"di_pattern":    "field_injection",
			},
		})
	}

	// @Inject constructor injection — di_injection_point.
	for _, m := range dwInjectConstructorRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:operation:dw_inject_ctor:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Operation", Subtype: "constructor",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_INJECT_CONSTRUCTOR", Ref: ref,
			Properties: map[string]any{
				"class_name": className,
				"framework":  "dropwizard",
				"di_pattern": "constructor_injection",
			},
		})
	}

	// -------------------------------------------------------------------------
	// DI: Scoped beans — di_scope_resolution.
	// -------------------------------------------------------------------------
	for _, m := range dwScopedClassRE.FindAllStringSubmatchIndex(source, -1) {
		scope := source[m[2]:m[3]]
		className := source[m[4]:m[5]]
		ref := "scope:component:dw_scoped:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_DI_SCOPE", Ref: ref,
			Properties: map[string]any{
				"di_scope":   scope,
				"framework":  "dropwizard",
				"di_pattern": "scoped_bean",
			},
		})
	}

	// -------------------------------------------------------------------------
	// Auth: @Authenticated — auth_coverage.
	// -------------------------------------------------------------------------
	for _, m := range dwAuthenticatedRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ownerClass := findEnclosingClass(source, m[0])
		fullName := name
		if ownerClass != "" && ownerClass != name {
			fullName = ownerClass + "." + name
		}
		ref := "scope:operation:dw_authenticated:" + fp + ":" + fullName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: fullName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_AUTHENTICATED", Ref: ref,
			Properties: map[string]any{
				"auth_annotation": "Authenticated",
				"framework":       "dropwizard",
				"auth_required":   true,
			},
		})
	}

	// @RolesAllowed — auth_coverage.
	for _, m := range dwRolesAllowedRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[2]:m[3]]
		ownerClass := findEnclosingClass(source, m[0])
		fullName := methodName
		if ownerClass != "" {
			fullName = ownerClass + "." + methodName
		}
		ref := "scope:operation:dw_roles_allowed:" + fp + ":" + fullName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: fullName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_ROLES_ALLOWED", Ref: ref,
			Properties: map[string]any{
				"auth_annotation": "RolesAllowed",
				"framework":       "dropwizard",
				"auth_required":   true,
			},
		})
	}

	// @PermitAll — auth_coverage (public endpoints explicitly opted out).
	for _, m := range dwPermitAllRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[2]:m[3]]
		ownerClass := findEnclosingClass(source, m[0])
		fullName := methodName
		if ownerClass != "" {
			fullName = ownerClass + "." + methodName
		}
		ref := "scope:operation:dw_permit_all:" + fp + ":" + fullName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: fullName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_PERMIT_ALL", Ref: ref,
			Properties: map[string]any{
				"auth_annotation": "PermitAll",
				"framework":       "dropwizard",
				"auth_required":   false,
			},
		})
	}

	// -------------------------------------------------------------------------
	// Middleware: ContainerRequestFilter / ContainerResponseFilter — middleware_coverage.
	// -------------------------------------------------------------------------
	for _, m := range dwContainerFilterRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		filterType := source[m[4]:m[5]]
		ref := "scope:component:dw_filter:" + fp + ":" + className
		// Check for @Provider in the preceding 400-char window.
		windowStart := m[0]
		if windowStart > 400 {
			windowStart = m[0] - 400
		} else {
			windowStart = 0
		}
		window := source[windowStart:m[0]]
		hasProvider := dwProviderRE.MatchString(window)

		// Detect @Priority.
		var priority string
		if pm := dwPriorityRE.FindStringSubmatch(window); pm != nil {
			priority = pm[1]
		}

		props := map[string]any{
			"filter_type": toLowerCase(filterType),
			"framework":   "dropwizard",
			"middleware":  "jaxrs_container_filter",
		}
		if hasProvider {
			props["provider_registered"] = true
		}
		if priority != "" {
			props["priority"] = priority
		}

		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_FILTER", Ref: ref,
			Properties: props,
		})
	}

	// -------------------------------------------------------------------------
	// Transactions: JDBI @Transaction on methods — transaction_boundary_extraction.
	// -------------------------------------------------------------------------
	for _, m := range dwJDBITransactionRE.FindAllStringSubmatchIndex(source, -1) {
		var txAttr string
		if m[2] >= 0 {
			txAttr = source[m[2]:m[3]]
		}
		methodName := source[m[4]:m[5]]
		if methodNameStopwords[methodName] {
			continue
		}
		ownerClass := findEnclosingClass(source, m[0])
		name := methodName
		if ownerClass != "" {
			name = ownerClass + "." + methodName
		}
		props := map[string]any{
			"transaction_boundary": "method",
			"method":               methodName,
			"framework":            "dropwizard",
			"tx_type":              "jdbi_transaction",
		}
		if ownerClass != "" {
			props["declaring_class"] = ownerClass
		}
		// JDBI @Transaction supports isolation level.
		if txAttr != "" {
			props["tx_attribute"] = txAttr
		}
		ref := "scope:pattern:dw_tx_boundary:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "transaction_boundary",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_JDBI_TRANSACTION", Ref: ref,
			Properties: props,
		})
	}

	// JDBI @Transaction on class/interface — transaction_boundary_extraction.
	for _, m := range dwJDBITransactionClassRE.FindAllStringSubmatchIndex(source, -1) {
		var txAttr string
		if m[2] >= 0 {
			txAttr = source[m[2]:m[3]]
		}
		className := source[m[4]:m[5]]
		props := map[string]any{
			"transaction_boundary": "class",
			"declaring_class":      className,
			"framework":            "dropwizard",
			"tx_type":              "jdbi_transaction",
		}
		if txAttr != "" {
			props["tx_attribute"] = txAttr
		}
		ref := "scope:pattern:dw_tx_boundary:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", Subtype: "transaction_boundary",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_JDBI_TRANSACTION_CLASS", Ref: ref,
			Properties: props,
		})
	}

	// -------------------------------------------------------------------------
	// JDBI SQL DAO annotations — di_binding_extraction evidence.
	// -------------------------------------------------------------------------
	for _, m := range dwSqlQueryRE.FindAllStringSubmatchIndex(source, -1) {
		sqlAnn := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		ownerClass := findEnclosingClass(source, m[0])
		name := methodName
		if ownerClass != "" {
			name = ownerClass + "." + methodName
		}
		ref := "scope:operation:dw_jdbi_sql:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_JDBI_SQL", Ref: ref,
			Properties: map[string]any{
				"sql_annotation": sqlAnn,
				"framework":      "dropwizard",
				"di_pattern":     "jdbi_dao",
			},
		})
	}

	// -------------------------------------------------------------------------
	// Tests: DropwizardAppRule / ResourceTestRule — tests_linkage.
	// -------------------------------------------------------------------------
	for _, m := range dwAppRuleRE.FindAllStringSubmatchIndex(source, -1) {
		fieldName := source[m[2]:m[3]]
		ref := "scope:component:dw_app_rule:" + fp + ":" + fieldName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: fieldName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_APP_RULE", Ref: ref,
			Properties: map[string]any{
				"test_rule":  "DropwizardAppRule",
				"framework":  "dropwizard",
				"test_scope": "integration",
			},
		})
	}

	for _, m := range dwResourceTestRuleRE.FindAllStringSubmatchIndex(source, -1) {
		fieldName := source[m[2]:m[3]]
		ref := "scope:component:dw_resource_test_rule:" + fp + ":" + fieldName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: fieldName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_DROPWIZARD_RESOURCE_TEST_RULE", Ref: ref,
			Properties: map[string]any{
				"test_rule":  "ResourceTestRule",
				"framework":  "dropwizard",
				"test_scope": "unit",
			},
		})
	}

	// @ExtendWith(DropwizardExtensionsSupport.class) — tests_linkage.
	if dwExtendWithDropwizardRE.MatchString(source) {
		ownerClass := findEnclosingClass(source, 0)
		if ownerClass == "" {
			ownerClass = "UnknownTest"
		}
		ref := "scope:component:dw_extensions_support:" + fp + ":" + ownerClass
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ownerClass, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: 1, LineEnd: 1,
			Provenance: "INFERRED_FROM_DROPWIZARD_EXTENSIONS_SUPPORT", Ref: ref,
			Properties: map[string]any{
				"test_rule":  "DropwizardExtensionsSupport",
				"framework":  "dropwizard",
				"test_scope": "integration",
			},
		})
	}

	_ = seenRels
	return result
}
