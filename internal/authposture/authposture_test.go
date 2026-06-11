package authposture

import "testing"

// --- Diff verdict logic -----------------------------------------------------

func TestDiff_Equivalent_SameKindSameLiteral(t *testing.T) {
	r := Diff(
		Posture{Kind: KindPage, Literal: "client_admin"},
		Posture{Kind: KindPage, Literal: "client_admin"},
	)
	if r.Verdict != VerdictEquivalent {
		t.Fatalf("verdict=%s detail=%s, want equivalent", r.Verdict, r.Detail)
	}
}

func TestDiff_Equivalent_SlugSeparatorFold(t *testing.T) {
	// "core_admin" (oracle) vs "core-admin" — wait, that is the SLUG MISMATCH
	// case below. Here equivalence holds across an underscore/case fold that
	// NormalizeKey treats as the SAME key (page slug differing only in case).
	r := Diff(
		Posture{Kind: KindPage, Literal: "Client_Admin"},
		Posture{Kind: KindPage, Literal: "client_admin"},
	)
	if r.Verdict != VerdictEquivalent {
		t.Fatalf("verdict=%s, want equivalent (case fold)", r.Verdict)
	}
}

func TestDiff_SlugMismatch_UnderscoreVsHyphen(t *testing.T) {
	// The canonical RBAC-drift slug bug: oracle uses underscore, v3 uses hyphen.
	// NormalizeKey folds both separators to the SAME form, so these align as the
	// SAME slug → equivalent, NOT a mismatch. This documents that separator
	// drift alone is NOT a slug_mismatch (literal_parity catches that class on
	// the value-set; here the grant is equivalent). The slug_mismatch verdict
	// fires on a genuinely DIFFERENT identifier.
	r := Diff(
		Posture{Kind: KindPage, Literal: "core-admin"},
		Posture{Kind: KindPage, Literal: "core_admin"},
	)
	if r.Verdict != VerdictEquivalent {
		t.Fatalf("verdict=%s, want equivalent (separator fold)", r.Verdict)
	}
	// A truly different slug IS a mismatch.
	r2 := Diff(
		Posture{Kind: KindPage, Literal: "billing_admin"},
		Posture{Kind: KindPage, Literal: "core_admin"},
	)
	if r2.Verdict != VerdictSlugMismatch {
		t.Fatalf("verdict=%s, want slug_mismatch", r2.Verdict)
	}
}

func TestDiff_KindMismatch_PageVsAction(t *testing.T) {
	r := Diff(
		Posture{Kind: KindAction, Literal: "x"},
		Posture{Kind: KindPage, Literal: "x"},
	)
	if r.Verdict != VerdictKindMismatch {
		t.Fatalf("verdict=%s, want kind_mismatch (page vs action same strength)", r.Verdict)
	}
}

func TestDiff_Looser_PageDowngradedToAuthenticated(t *testing.T) {
	// The dangerous RBAC regression: oracle demands a page grant, v3 only
	// requires authentication.
	r := Diff(
		Posture{Kind: KindAuthenticated},
		Posture{Kind: KindPage, Literal: "client_admin"},
	)
	if r.Verdict != VerdictLooser {
		t.Fatalf("verdict=%s, want looser", r.Verdict)
	}
}

func TestDiff_Stricter_AuthenticatedUpgradedToSuperuser(t *testing.T) {
	r := Diff(
		Posture{Kind: KindSuperuser},
		Posture{Kind: KindAuthenticated},
	)
	if r.Verdict != VerdictStricter {
		t.Fatalf("verdict=%s, want stricter", r.Verdict)
	}
}

func TestDiff_UnknownNeverEquivalent(t *testing.T) {
	r := Diff(Posture{Kind: KindUnknown}, Posture{Kind: KindPage, Literal: "x"})
	if r.Verdict != VerdictKindMismatch {
		t.Fatalf("verdict=%s, want kind_mismatch for unknown side", r.Verdict)
	}
}

// --- §10 Django get_permissions decoder -------------------------------------

// The §10 ClientViewSet-style get_permissions: a page arm for a named action,
// a DEAD-CODE `== [list]` arm, and the else DEFAULT ACTION GRANT.
const clientViewSetGetPerms = `
def get_permissions(self):
    if self.action == "approve":
        return [CustomPagePermissionCheck(PERMISSION_PAGES["client_admin"])]
    elif self.action == ["list", "retrieve"]:
        return [IsAuthenticated()]
    elif self.action in ["export", "report"]:
        return [CustomPagePermissionCheck(PERMISSION_PAGES["client_reports"])]
    else:
        return [CustomActionPermissionCheck()]
`

func TestDecode_PageArm(t *testing.T) {
	p, ok := decodeGetPermissions(clientViewSetGetPerms, "approve")
	if !ok {
		t.Fatal("decode failed")
	}
	if p.Kind != KindPage || p.Literal != "client_admin" {
		t.Fatalf("got %s/%q, want page/client_admin", p.Kind, p.Literal)
	}
}

func TestDecode_ElseIsActionGrant_NotAuthenticated(t *testing.T) {
	// CRITICAL: the else arm is the DEFAULT ACTION GRANT, not authenticated.
	// "create" hits no live arm → else.
	p, ok := decodeGetPermissions(clientViewSetGetPerms, "create")
	if !ok {
		t.Fatal("decode failed")
	}
	if p.Kind != KindAction {
		t.Fatalf("else arm decoded as %s, want action (the §10 default-arm rule)", p.Kind)
	}
}

func TestDecode_DeadCodeScalarEqList_FallsThroughToElse(t *testing.T) {
	// CRITICAL: `self.action == ["list","retrieve"]` is DEAD CODE (scalar ==
	// list never matches). "list" must NOT resolve to that arm's IsAuthenticated;
	// it falls through to the else action grant.
	p, ok := decodeGetPermissions(clientViewSetGetPerms, "list")
	if !ok {
		t.Fatal("decode failed")
	}
	if p.Kind == KindAuthenticated {
		t.Fatalf("dead `== [list]` arm was treated as LIVE — decoded list as authenticated; §10 says it is dead code → else")
	}
	if p.Kind != KindAction {
		t.Fatalf("got %s, want action (fall-through to else)", p.Kind)
	}
}

func TestDecode_LiveInListArm(t *testing.T) {
	// `self.action in ["export","report"]` IS live → page client_reports.
	p, ok := decodeGetPermissions(clientViewSetGetPerms, "export")
	if !ok {
		t.Fatal("decode failed")
	}
	if p.Kind != KindPage || p.Literal != "client_reports" {
		t.Fatalf("got %s/%q, want page/client_reports", p.Kind, p.Literal)
	}
}

func TestDecode_SuperuserArm(t *testing.T) {
	src := `
def get_permissions(self):
    if self.action == "destroy":
        return [IsAdminUser()]
    else:
        return [CustomActionPermissionCheck()]
`
	p, ok := decodeGetPermissions(src, "destroy")
	if !ok {
		t.Fatal("decode failed")
	}
	if p.Kind != KindSuperuser {
		t.Fatalf("got %s, want superuser", p.Kind)
	}
}

func TestDecode_AuthenticatedOnlyLiveArm(t *testing.T) {
	// A LIVE `in [list]` arm returning IsAuthenticated → authenticated-only.
	src := `
def get_permissions(self):
    if self.action in ["public_list"]:
        return [IsAuthenticated()]
    else:
        return [CustomActionPermissionCheck()]
`
	p, ok := decodeGetPermissions(src, "public_list")
	if !ok {
		t.Fatal("decode failed")
	}
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated", p.Kind)
	}
}

func TestDecode_PermissionPagesAttrForm(t *testing.T) {
	src := `
def get_permissions(self):
    if self.action == "x":
        return [CustomPagePermissionCheck(PERMISSION_PAGES.CLIENT_ADMIN)]
    else:
        return [CustomActionPermissionCheck()]
`
	p, _ := decodeGetPermissions(src, "x")
	if p.Kind != KindPage || p.Literal != "CLIENT_ADMIN" {
		t.Fatalf("got %s/%q, want page/CLIENT_ADMIN (attr form)", p.Kind, p.Literal)
	}
}

// --- Resolver registry ------------------------------------------------------

func TestRegistry_DjangoElseArm(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{
		Props:  map[string]string{"has_get_permissions": "true"},
		Source: clientViewSetGetPerms,
		Action: "create",
	})
	if fw != "django-drf" {
		t.Fatalf("framework=%s, want django-drf", fw)
	}
	if p.Kind != KindAction {
		t.Fatalf("kind=%s, want action", p.Kind)
	}
}

func TestRegistry_NestRequirePageProp(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{Props: map[string]string{"require_page": "client_admin"}})
	if fw != "nestjs" {
		t.Fatalf("framework=%s, want nestjs", fw)
	}
	if p.Kind != KindPage || p.Literal != "client_admin" {
		t.Fatalf("got %s/%q, want page/client_admin", p.Kind, p.Literal)
	}
}

func TestRegistry_NestRequireActionDecoratorFallback(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{Source: `@RequireAction("export_clients")\n@Get()`})
	if fw != "nestjs" {
		t.Fatalf("framework=%s, want nestjs", fw)
	}
	if p.Kind != KindAction || p.Literal != "export_clients" {
		t.Fatalf("got %s/%q, want action/export_clients", p.Kind, p.Literal)
	}
}

func TestRegistry_StubsRegisteredButDecline(t *testing.T) {
	reg := NewRegistry()
	fws := reg.Frameworks()
	// spring-security (#4708), fastapi (#4709) and express (#4710) are now
	// implemented members; the remaining follow-up frameworks stay stubs.
	want := []string{"aspnet", "django-drf", "express", "fastapi", "flask", "go-middleware", "laravel", "nestjs", "phoenix", "rails", "spring-security"}
	if len(fws) != len(want) {
		t.Fatalf("frameworks=%v, want %v", fws, want)
	}
	for i := range want {
		if fws[i] != want[i] {
			t.Fatalf("frameworks=%v, want %v", fws, want)
		}
	}
	// A still-unimplemented framework (rails) signal → unknown / no resolver.
	p, fw := reg.Resolve(Signal{Props: map[string]string{"pundit_policy": "AdminPolicy"}})
	if fw != "" || p.Kind != KindUnknown {
		t.Fatalf("unimplemented framework resolved as %s/%s, want unknown/none", fw, p.Kind)
	}
}

// --- End-to-end: §10 decode feeds the diff, catching the RBAC regression ----

func TestE2E_OraclePageVsV3Authenticated_IsLooser(t *testing.T) {
	reg := NewRegistry()
	oracle, _ := reg.Resolve(Signal{
		Props:  map[string]string{"has_get_permissions": "true"},
		Source: clientViewSetGetPerms,
		Action: "approve", // → page client_admin
	})
	v3, _ := reg.Resolve(Signal{Props: map[string]string{"auth_required": "true", "auth_guard": "JwtGuard"}})
	d := Diff(v3, oracle)
	if d.Verdict != VerdictLooser {
		t.Fatalf("verdict=%s detail=%s, want looser (page downgraded to authenticated)", d.Verdict, d.Detail)
	}
}

func TestE2E_OraclePageVsV3RequirePage_Equivalent(t *testing.T) {
	reg := NewRegistry()
	oracle, _ := reg.Resolve(Signal{
		Props: map[string]string{"has_get_permissions": "true"}, Source: clientViewSetGetPerms, Action: "approve",
	})
	v3, _ := reg.Resolve(Signal{Props: map[string]string{"require_page": "client-admin"}}) // hyphen variant
	d := Diff(v3, oracle)
	if d.Verdict != VerdictEquivalent {
		t.Fatalf("verdict=%s detail=%s, want equivalent (page client_admin ~ client-admin)", d.Verdict, d.Detail)
	}
}

// --- #4667: EFFECTIVE-guard decode from the engine-stamped auth_guard ---------
//
// The engine stamps the most-specific (handler ▸ class ▸ global) guard's
// decorator text into the auth_guard property — e.g.
// `@RequirePage(PermissionPage.Buildings)` for a per-handler @RequirePage, or
// the inherited class guard for a handler with no override. The resolver MUST
// decode that decorator's page/action literal, NOT collapse every guard to
// authenticated (the bug that produced false NO-AUTH/looser verdicts).

// (A) per-handler @RequirePage with NO class guard → page grant, not authenticated.
func TestNest_EffectiveGuard_HandlerPage_NoClassGuard(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{Props: map[string]string{
		"framework": "nestjs", "auth_required": "true", "auth_method": "guard",
		"auth_guard": "@RequirePage(PermissionPage.Buildings)",
	}})
	if fw != "nestjs" {
		t.Fatalf("framework=%s, want nestjs", fw)
	}
	if p.Kind != KindPage || p.Literal != "Buildings" {
		t.Fatalf("got %s/%q, want page/Buildings (handler guard must not collapse to authenticated)", p.Kind, p.Literal)
	}
}

// @RequireAction enum-form decode.
func TestNest_EffectiveGuard_HandlerAction(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "nestjs", "auth_required": "true", "auth_method": "guard",
		"auth_guard": "@RequireAction(PermissionAction.Lite)",
	}})
	if p.Kind != KindAction || p.Literal != "Lite" {
		t.Fatalf("got %s/%q, want action/Lite", p.Kind, p.Literal)
	}
}

// (B) handler @RequirePage(ContractProposals) OVERRIDES class @RequirePage(Clients):
// the engine stamps the EFFECTIVE (handler) guard into auth_guard, so the
// resolver decodes ContractProposals — while a sibling that inherits the class
// guard decodes Clients.
func TestNest_EffectiveGuard_HandlerOverridesClass(t *testing.T) {
	reg := NewRegistry()
	over, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "nestjs", "auth_required": "true", "auth_method": "guard",
		"auth_guard": "@RequirePage(PermissionPage.ContractProposals)",
	}})
	if over.Kind != KindPage || over.Literal != "ContractProposals" {
		t.Fatalf("override: got %s/%q, want page/ContractProposals (handler wins)", over.Kind, over.Literal)
	}
	sib, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "nestjs", "auth_required": "true", "auth_method": "guard",
		"auth_guard": "@RequirePage(PermissionPage.Clients)",
	}})
	if sib.Kind != KindPage || sib.Literal != "Clients" {
		t.Fatalf("sibling: got %s/%q, want page/Clients (inherited class guard)", sib.Kind, sib.Literal)
	}
}

// A page-guarded NestJS handler vs a Django page oracle is EQUIVALENT — the
// pre-fix collapse to authenticated made this a FALSE looser (RBAC false alarm).
func TestNest_EffectiveGuard_PageVsOraclePage_Equivalent(t *testing.T) {
	reg := NewRegistry()
	oracle, _ := reg.Resolve(Signal{
		Props: map[string]string{"has_get_permissions": "true"}, Source: clientViewSetGetPerms, Action: "approve",
	}) // → page client_admin
	v3, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "nestjs", "auth_required": "true", "auth_guard": "@RequirePage(PermissionPage.client_admin)",
	}})
	if d := Diff(v3, oracle); d.Verdict != VerdictEquivalent {
		t.Fatalf("verdict=%s detail=%s, want equivalent (both page client_admin)", d.Verdict, d.Detail)
	}
}

// Authenticated-only guard (@AuthenticatedOrInternalKey) decodes to authenticated.
func TestNest_EffectiveGuard_AuthenticatedOnly(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "nestjs", "auth_required": "true", "auth_guard": "@AuthenticatedOrInternalKey()",
	}})
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated", p.Kind)
	}
}

// Engine explicit public verdict (@Public → auth_required=false, no guard).
func TestNest_EffectiveGuard_ExplicitPublic(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{Props: map[string]string{
		"framework": "nestjs", "auth_required": "false", "auth_method": "config",
	}})
	if fw != "nestjs" || p.Kind != KindPublic {
		t.Fatalf("got %s/%s, want nestjs/public", fw, p.Kind)
	}
}

// --- #4675: DRF EFFECTIVE permission precedence (method ▸ class ▸ global) -----
//
// The DRF analog of the NestJS effective-guard fix. The resolver must resolve
// the most-specific permission: a per-action `@action(permission_classes=…)`
// override or a `get_permissions` per-action arm ▸ the class permission_classes
// ▸ the global REST_FRAMEWORK DEFAULT_PERMISSION_CLASSES.

// (A) ViewSet class permission_classes=[IsAuthenticated] with an
// @action(permission_classes=[AllowAny]). The extractor stamps the per-action
// `permission_classes` onto the action endpoint, so the action resolves PUBLIC
// while a sibling (carrying the inherited class value) resolves authenticated.
func TestDRF_ActionPermissionClasses_OverridesClass(t *testing.T) {
	reg := NewRegistry()
	// The @action override endpoint: permission_classes=[AllowAny] (per-action).
	action, fw := reg.Resolve(Signal{Props: map[string]string{
		"framework": "django", "permission_classes": "AllowAny",
	}, Action: "ping"})
	if fw != "django-drf" {
		t.Fatalf("framework=%s, want django-drf", fw)
	}
	if action.Kind != KindPublic {
		t.Fatalf("action: got %s, want public (@action(permission_classes=[AllowAny]) overrides class)", action.Kind)
	}
	// A sibling action that inherits the class default: the extractor stamps the
	// CLASS permission_classes onto its endpoint (=[IsAuthenticated]).
	sib, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "django", "has_permission_classes": "true", "permission_classes": "IsAuthenticated",
	}, Action: "list"})
	if sib.Kind != KindAuthenticated {
		t.Fatalf("sibling: got %s, want authenticated (inherited class default)", sib.Kind)
	}
}

// (B) get_permissions with `if self.action == 'x': return [AllowAny()]` else the
// class default → action x is public (method-level arm), others get the else
// class/default arm.
func TestDRF_GetPermissions_PerActionPublic_ElseClass(t *testing.T) {
	src := `
def get_permissions(self):
    if self.action == "open":
        return [AllowAny()]
    else:
        return [IsAuthenticated()]
`
	reg := NewRegistry()
	open, fw := reg.Resolve(Signal{
		Props:  map[string]string{"has_get_permissions": "true"},
		Source: src, Action: "open",
	})
	if fw != "django-drf" {
		t.Fatalf("framework=%s, want django-drf", fw)
	}
	if open.Kind != KindPublic {
		t.Fatalf("action open: got %s, want public (per-action arm)", open.Kind)
	}
	other, _ := reg.Resolve(Signal{
		Props:  map[string]string{"has_get_permissions": "true"},
		Source: src, Action: "list",
	})
	if other.Kind != KindAuthenticated {
		t.Fatalf("action list: got %s, want authenticated (else/class arm)", other.Kind)
	}
}

// (C) No class permission_classes and no get_permissions, but a global
// REST_FRAMEWORK DEFAULT_PERMISSION_CLASSES=[IsAuthenticated] → endpoints resolve
// authenticated via the global default.
func TestDRF_GlobalDefault_AppliesWhenNoMethodOrClass(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{Props: map[string]string{
		"framework": "django", "drf_default_permission_classes": "IsAuthenticated",
	}})
	if fw != "django-drf" {
		t.Fatalf("framework=%s, want django-drf", fw)
	}
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (global DEFAULT_PERMISSION_CLASSES fallback)", p.Kind)
	}
}

// Empty `permission_classes=[]` (extractor stamps NO prop) must fall through to
// the global default, NOT resolve public — the empty-vs-AllowAny distinction.
func TestDRF_EmptyPermissionClasses_FallsToGlobal(t *testing.T) {
	reg := NewRegistry()
	// No permission_classes prop (empty list ⇒ absent), only a global default.
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "django", "drf_default_permission_classes": "IsAuthenticated",
	}, Action: "list"})
	if p.Kind == KindPublic {
		t.Fatalf("empty permission_classes=[] resolved PUBLIC; §4675 says it falls to the global default")
	}
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (global default)", p.Kind)
	}
}

// Class permission_classes wins over the global default (precedence level 2 > 3).
func TestDRF_ClassOverridesGlobalDefault(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "django", "has_permission_classes": "true",
		"permission_classes":             "AllowAny",
		"drf_default_permission_classes": "IsAuthenticated",
	}})
	if p.Kind != KindPublic {
		t.Fatalf("got %s, want public (class permission_classes=[AllowAny] overrides global IsAuthenticated)", p.Kind)
	}
}

// --- #4674: Spring EFFECTIVE-guard precedence (method ▸ class ▸ global) --------
//
// The Spring analog of the NestJS effective-guard fix and the DRF method▸class▸
// global fix. The resolver must resolve the most-specific posture: a method-level
// @PreAuthorize/@Secured/@RolesAllowed/@PermitAll ▸ the controller-class
// annotation ▸ a SecurityFilterChain/HttpSecurity rule matched to the route.

// (A) class @PreAuthorize("hasRole('USER')") with a method @PreAuthorize("permitAll()").
// The annotated method resolves PUBLIC (method wins) while a sibling that carries
// only the class annotation resolves to the USER role.
func TestSpring_MethodPermitAll_OverridesClassRole(t *testing.T) {
	reg := NewRegistry()
	// The method-level @PreAuthorize("permitAll()") override endpoint.
	m, fw := reg.Resolve(Signal{Props: map[string]string{
		"framework":                  "spring",
		"auth_expression":            "permitAll()",
		"spring_class_pre_authorize": "hasRole('USER')",
	}})
	if fw != "spring-security" {
		t.Fatalf("framework=%s, want spring-security", fw)
	}
	if m.Kind != KindPublic {
		t.Fatalf("method: got %s, want public (method @PreAuthorize(permitAll()) overrides class)", m.Kind)
	}
	// A sibling handler with NO method annotation inherits the class @PreAuthorize.
	sib, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework":                  "spring",
		"spring_class_pre_authorize": "hasRole('USER')",
	}})
	if sib.Kind != KindRole || sib.Literal != "USER" {
		t.Fatalf("sibling: got %s/%q, want role/USER (inherited class @PreAuthorize)", sib.Kind, sib.Literal)
	}
}

// (B) No method/class annotation, but a SecurityFilterChain
// `.requestMatchers("/admin/**").hasRole("ADMIN")` rule the engine matched to the
// route → resolves the ADMIN role via the global level.
func TestSpring_GlobalFilterChain_AppliesWhenNoMethodOrClass(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{Props: map[string]string{
		"framework":                   "spring",
		"spring_global_authorization": `requestMatchers("/admin/**").hasRole("ADMIN")`,
	}})
	if fw != "spring-security" {
		t.Fatalf("framework=%s, want spring-security", fw)
	}
	if p.Kind != KindRole || p.Literal != "ADMIN" {
		t.Fatalf("got %s/%q, want role/ADMIN (global SecurityFilterChain rule)", p.Kind, p.Literal)
	}
}

// (B2) A global permitAll() rule resolves PUBLIC; a global authenticated() rule
// resolves authenticated — the rest of the HttpSecurity DSL vocabulary.
func TestSpring_GlobalFilterChain_PermitAllAndAuthenticated(t *testing.T) {
	reg := NewRegistry()
	pub, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework":                   "spring",
		"spring_global_authorization": `antMatchers("/public/**").permitAll()`,
	}})
	if pub.Kind != KindPublic {
		t.Fatalf("got %s, want public (global permitAll())", pub.Kind)
	}
	authed, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework":                   "spring",
		"spring_global_authorization": `requestMatchers("/api/**").authenticated()`,
	}})
	if authed.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (global authenticated())", authed.Kind)
	}
}

// (C) Method @Secured("ROLE_X") over class @RolesAllowed("ROLE_Y") → the method
// annotation wins (X), the class is shadowed.
func TestSpring_MethodSecured_OverridesClassRolesAllowed(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{Props: map[string]string{
		"framework":                  "spring",
		"secured":                    "ROLE_X",
		"spring_class_roles_allowed": "ROLE_Y",
	}})
	if fw != "spring-security" {
		t.Fatalf("framework=%s, want spring-security", fw)
	}
	if p.Kind != KindRole || p.Literal != "X" {
		t.Fatalf("got %s/%q, want role/X (method @Secured wins over class @RolesAllowed)", p.Kind, p.Literal)
	}
}

// Class wins over global (precedence level 2 > 3): a class @PreAuthorize role
// shadows a matching SecurityFilterChain permitAll() rule.
func TestSpring_ClassOverridesGlobal(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework":                   "spring",
		"spring_class_pre_authorize":  "hasRole('MANAGER')",
		"spring_global_authorization": `requestMatchers("/**").permitAll()`,
	}})
	if p.Kind != KindRole || p.Literal != "MANAGER" {
		t.Fatalf("got %s/%q, want role/MANAGER (class @PreAuthorize overrides global permitAll())", p.Kind, p.Literal)
	}
}

// A recognised Spring handler with no decodable method/class/global grant →
// unknown (never false-public).
func TestSpring_NoDecodableGrant_IsUnknownNotPublic(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework":   "spring",
		"auth_method": "spring-security",
	}})
	if p.Kind == KindPublic {
		t.Fatalf("recognised-but-undecodable Spring handler resolved PUBLIC; must be unknown")
	}
	if p.Kind != KindUnknown {
		t.Fatalf("got %s, want unknown", p.Kind)
	}
}

// End-to-end: a Spring class-role handler vs a Django role oracle is EQUIVALENT,
// and a Spring method permitAll() override vs the same oracle is LOOSER (the
// override opened the endpoint).
func TestSpring_E2E_ClassRoleVsOracle_AndMethodOverrideLooser(t *testing.T) {
	reg := NewRegistry()
	// Oracle: a DRF class with permission_classes naming a role-equivalent gate.
	oracle, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "django", "has_permission_classes": "true",
		"permission_classes": "IsAuthenticated",
	}})
	// Sibling Spring handler inherits the class @PreAuthorize → authenticated-or-stricter.
	sib, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "spring", "spring_class_pre_authorize": "isAuthenticated()",
	}})
	if d := Diff(sib, oracle); d.Verdict != VerdictEquivalent {
		t.Fatalf("sibling verdict=%s detail=%s, want equivalent (both authenticated)", d.Verdict, d.Detail)
	}
	// The method override opens it → looser than the authenticated oracle.
	over, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "spring", "auth_expression": "permitAll()",
		"spring_class_pre_authorize": "isAuthenticated()",
	}})
	if d := Diff(over, oracle); d.Verdict != VerdictLooser {
		t.Fatalf("override verdict=%s detail=%s, want looser (permitAll opened an authenticated endpoint)", d.Verdict, d.Detail)
	}
}
