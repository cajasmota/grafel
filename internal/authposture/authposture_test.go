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

func TestRegistry_AllFrameworksRegistered(t *testing.T) {
	reg := NewRegistry()
	fws := reg.Frameworks()
	// With go-middleware (#4543) and phoenix (#4544) landed, the all-framework
	// resolver set is COMPLETE — no stubs remain. Every registered framework has
	// a real resolver.
	want := []string{"aspnet", "django-drf", "express", "fastapi", "flask", "go-middleware", "laravel", "nestjs", "phoenix", "rails", "spring-security"}
	if len(fws) != len(want) {
		t.Fatalf("frameworks=%v, want %v", fws, want)
	}
	for i := range want {
		if fws[i] != want[i] {
			t.Fatalf("frameworks=%v, want %v", fws, want)
		}
	}
	// A signal that matches NO framework's signature → unknown / no resolver
	// (the diff core reports an honest kind_mismatch rather than false-equivalent).
	p, fw := reg.Resolve(Signal{Props: map[string]string{"totally_unrelated_prop": "xyz"}})
	if fw != "" || p.Kind != KindUnknown {
		t.Fatalf("unrecognised signal resolved as %s/%s, want unknown/none", fw, p.Kind)
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

// --- #4538: Rails (before_action + Pundit/CanCanCan) -------------------------

// (A) PROTECTED: class before_action :authenticate_user! → authenticated.
func TestRails_BeforeActionAuthenticate_Authenticated(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "rails"},
		Source: "class PostsController < ApplicationController\n  before_action :authenticate_user!\n  def index; end\nend",
	})
	if fw != "rails" {
		t.Fatalf("framework=%s, want rails", fw)
	}
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (before_action :authenticate_user!)", p.Kind)
	}
}

// (A2) Pundit authorize @x, :update? → action (policy) grant.
func TestRails_PunditAuthorize_ActionGrant(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "rails"},
		Source: "def update\n  @post = Post.find(params[:id])\n  authorize @post, :update?\nend",
	})
	if p.Kind != KindAction || p.Literal != "update" {
		t.Fatalf("got %s/%q, want action/update (Pundit authorize)", p.Kind, p.Literal)
	}
}

// (A3) CanCanCan authorize! :destroy → action grant. require_admin → role admin.
func TestRails_CanCanCanAndAdmin(t *testing.T) {
	reg := NewRegistry()
	cc, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "rails"},
		Source: "def destroy\n  authorize! :destroy, @post\nend"})
	if cc.Kind != KindAction || cc.Literal != "destroy" {
		t.Fatalf("got %s/%q, want action/destroy (CanCanCan authorize!)", cc.Kind, cc.Literal)
	}
	adm, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "rails"},
		Source: "class AdminController < ApplicationController\n  before_action :require_admin\nend"})
	if adm.Kind != KindRole || adm.Literal != "admin" {
		t.Fatalf("got %s/%q, want role/admin (before_action :require_admin)", adm.Kind, adm.Literal)
	}
}

// (B) PUBLIC OVERRIDE: skip_before_action :authenticate_user! → public.
func TestRails_SkipBeforeAction_Public(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "rails"},
		Source: "class PublicController < ApplicationController\n  skip_before_action :authenticate_user!, only: [:index]\nend",
	})
	if p.Kind != KindPublic {
		t.Fatalf("got %s, want public (skip_before_action override)", p.Kind)
	}
}

// (B2) Reconciled auth_required=false (engine resolved an only:-scoped skip) → public.
func TestRails_ReconciledSkipPublic(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "rails", "auth_required": "false", "auth_method": "skip_before_action",
	}})
	if p.Kind != KindPublic {
		t.Fatalf("got %s, want public (reconciled skip_before_action)", p.Kind)
	}
}

// --- #4540: Flask (decorators / before_request) ------------------------------

// (A) PROTECTED: @login_required → authenticated.
func TestFlask_LoginRequired_Authenticated(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "flask"},
		Source: "@app.route('/dash')\n@login_required\ndef dash():\n    return render()",
	})
	if fw != "flask" {
		t.Fatalf("framework=%s, want flask", fw)
	}
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (@login_required)", p.Kind)
	}
}

// (A2) @roles_required('admin') → role; @permission_required('export') → action.
func TestFlask_RolesAndPermission(t *testing.T) {
	reg := NewRegistry()
	r, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "flask"},
		Source: "@roles_required('admin')\ndef admin_view(): pass"})
	if r.Kind != KindRole || r.Literal != "admin" {
		t.Fatalf("got %s/%q, want role/admin (@roles_required)", r.Kind, r.Literal)
	}
	pr, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "flask"},
		Source: "@permission_required('export')\ndef export_view(): pass"})
	if pr.Kind != KindAction || pr.Literal != "export" {
		t.Fatalf("got %s/%q, want action/export (@permission_required)", pr.Kind, pr.Literal)
	}
}

// (B) PUBLIC: a Flask view with no auth decorator (auth_required=false) → public.
func TestFlask_NoDecorator_Public(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "flask", "auth_required": "false",
	}})
	if p.Kind != KindPublic {
		t.Fatalf("got %s, want public (no auth decorator)", p.Kind)
	}
}

// (B2) before_request hook scope → authenticated default for the blueprint.
func TestFlask_BeforeRequestScope_Authenticated(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "flask", "auth_required": "true", "auth_method": "before_request",
	}})
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (before_request scope)", p.Kind)
	}
}

// --- #4541: Laravel (middleware / Gates / Policies) --------------------------

// (A) PROTECTED: ->middleware('auth:sanctum') → authenticated.
func TestLaravel_AuthMiddleware_Authenticated(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "laravel"},
		Source: "Route::get('/profile', [ProfileController::class, 'show'])->middleware('auth:sanctum');",
	})
	if fw != "laravel" {
		t.Fatalf("framework=%s, want laravel", fw)
	}
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (->middleware('auth:sanctum'))", p.Kind)
	}
}

// (A2) role:admin middleware → role; can:update → action; authorize() → action.
func TestLaravel_RoleAndCanAndGate(t *testing.T) {
	reg := NewRegistry()
	role, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "laravel"},
		Source: "Route::get('/admin', 'A@i')->middleware('role:editor');"})
	if role.Kind != KindRole || role.Literal != "editor" {
		t.Fatalf("got %s/%q, want role/editor", role.Kind, role.Literal)
	}
	can, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "laravel"},
		Source: "Route::put('/post/{p}', 'P@u')->middleware('can:update,post');"})
	if can.Kind != KindAction || can.Literal != "update" {
		t.Fatalf("got %s/%q, want action/update (can:)", can.Kind, can.Literal)
	}
	gate, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "laravel"},
		Source: "public function update(Post $post) {\n  $this->authorize('update', $post);\n}"})
	if gate.Kind != KindAction || gate.Literal != "update" {
		t.Fatalf("got %s/%q, want action/update ($this->authorize)", gate.Kind, gate.Literal)
	}
}

// (B) PUBLIC OVERRIDE: ->withoutMiddleware('auth') → public.
func TestLaravel_WithoutMiddleware_Public(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "laravel"},
		Source: "Route::get('/open', 'O@i')->withoutMiddleware('auth');",
	})
	if p.Kind != KindPublic {
		t.Fatalf("got %s, want public (->withoutMiddleware('auth'))", p.Kind)
	}
}

// (B2) No auth middleware (auth_required=false) → public.
func TestLaravel_NoMiddleware_Public(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "laravel", "auth_required": "false",
	}})
	if p.Kind != KindPublic {
		t.Fatalf("got %s, want public (no auth middleware)", p.Kind)
	}
}

// --- #4542: ASP.NET Core ([Authorize] / [AllowAnonymous] / policies) ---------

// (A) PROTECTED: method [Authorize(Roles="Admin")] → role Admin.
func TestAspnet_AuthorizeRoles_Role(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "aspnet"},
		Source: "[Authorize(Roles = \"Admin,Manager\")]\npublic IActionResult Edit() { }",
	})
	if fw != "aspnet" {
		t.Fatalf("framework=%s, want aspnet", fw)
	}
	if p.Kind != KindRole || p.Literal != "Admin" {
		t.Fatalf("got %s/%q, want role/Admin", p.Kind, p.Literal)
	}
}

// (A2) [Authorize(Policy="CanEdit")] → policy/role grant on CanEdit.
func TestAspnet_AuthorizePolicy(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "aspnet"},
		Source: "[Authorize(Policy = \"CanEdit\")]\npublic IActionResult Save() { }",
	})
	if p.Kind != KindRole || p.Literal != "CanEdit" {
		t.Fatalf("got %s/%q, want role/CanEdit (policy)", p.Kind, p.Literal)
	}
}

// (B) PUBLIC OVERRIDE: method [AllowAnonymous] over class [Authorize] → public.
// (C) PRECEDENCE: the class carries [Authorize] but the method [AllowAnonymous]
// wins (most-specific).
func TestAspnet_AllowAnonymous_OverridesClassAuthorize(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{Props: map[string]string{
		"framework":              "aspnet",
		"allow_anonymous":        "true",
		"aspnet_class_authorize": "[Authorize]",
	}})
	if fw != "aspnet" {
		t.Fatalf("framework=%s, want aspnet", fw)
	}
	if p.Kind != KindPublic {
		t.Fatalf("got %s, want public (method [AllowAnonymous] overrides class [Authorize])", p.Kind)
	}
	// A sibling action with NO method attribute inherits the class [Authorize].
	sib, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework":              "aspnet",
		"aspnet_class_authorize": "[Authorize]",
	}})
	if sib.Kind != KindAuthenticated {
		t.Fatalf("sibling: got %s, want authenticated (inherited class [Authorize])", sib.Kind)
	}
}

// (C2) Source-level precedence: an action body carrying BOTH a class-context
// [Authorize] and a method [AllowAnonymous] resolves public.
func TestAspnet_SourceAllowAnonymousWins(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "aspnet"},
		Source: "[AllowAnonymous]\npublic IActionResult Ping() { }",
	})
	if p.Kind != KindPublic {
		t.Fatalf("got %s, want public ([AllowAnonymous] method)", p.Kind)
	}
}

// (D) GLOBAL: a configured FallbackPolicy (RequireAuthenticatedUser) → authenticated
// default when no method/class attribute covers the action.
func TestAspnet_GlobalFallbackPolicy_Authenticated(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework":              "aspnet",
		"aspnet_fallback_policy": "RequireAuthenticatedUser",
	}})
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (global FallbackPolicy)", p.Kind)
	}
}

// --- E2E: each framework's posture diffs correctly against a Django oracle ----

// A Rails authenticated handler vs a Django page oracle is LOOSER (the RBAC
// regression class the diff exists to catch).
func TestRails_E2E_AuthenticatedVsOraclePage_Looser(t *testing.T) {
	reg := NewRegistry()
	oracle, _ := reg.Resolve(Signal{
		Props: map[string]string{"has_get_permissions": "true"}, Source: clientViewSetGetPerms, Action: "approve",
	}) // → page client_admin
	v3, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "rails"},
		Source: "before_action :authenticate_user!"})
	if d := Diff(v3, oracle); d.Verdict != VerdictLooser {
		t.Fatalf("verdict=%s detail=%s, want looser (Rails authenticated vs oracle page)", d.Verdict, d.Detail)
	}
}

// An ASP.NET [AllowAnonymous] override vs a Django authenticated oracle is LOOSER.
func TestAspnet_E2E_PublicVsOracleAuthenticated_Looser(t *testing.T) {
	reg := NewRegistry()
	oracle, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "django", "has_permission_classes": "true", "permission_classes": "IsAuthenticated",
	}})
	v3, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "aspnet", "allow_anonymous": "true", "aspnet_class_authorize": "[Authorize]",
	}})
	if d := Diff(v3, oracle); d.Verdict != VerdictLooser {
		t.Fatalf("verdict=%s detail=%s, want looser ([AllowAnonymous] opened an authenticated endpoint)", d.Verdict, d.Detail)
	}
}

// --- #4543: Go HTTP middleware chains (Gin/Echo/Chi/net-http) ----------------

// (A) PROTECTED: a route under `r.Use(AuthMiddleware())` → authenticated.
func TestGo_RouterUseAuthMiddleware_Authenticated(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{
		Props: map[string]string{"framework": "gin"},
		Source: "r := gin.Default()\n" +
			"r.Use(AuthMiddleware())\n" +
			"r.GET(\"/me\", profileHandler)",
	})
	if fw != "go-middleware" {
		t.Fatalf("framework=%s, want go-middleware", fw)
	}
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (r.Use(AuthMiddleware()))", p.Kind)
	}
}

// (A2) Echo group-level JWT middleware → authenticated; Chi inline route auth.
func TestGo_EchoGroupAndChiRoute_Authenticated(t *testing.T) {
	reg := NewRegistry()
	echo, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "echo"},
		Source: "g := e.Group(\"/api\")\ng.Use(middleware.JWT(secret))\ng.GET(\"/x\", h)"})
	if echo.Kind != KindAuthenticated {
		t.Fatalf("echo: got %s, want authenticated (g.Use(middleware.JWT))", echo.Kind)
	}
	chi, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "chi"},
		Source: "r.Group(func(r chi.Router){\n  r.Use(AuthCtx)\n  r.Get(\"/p\", h)\n})"})
	if chi.Kind != KindAuthenticated {
		t.Fatalf("chi: got %s, want authenticated (r.Use(AuthCtx))", chi.Kind)
	}
	inline, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "gin"},
		Source: "r.GET(\"/x\", AuthRequired(), handler)"})
	if inline.Kind != KindAuthenticated {
		t.Fatalf("inline: got %s, want authenticated (AuthRequired())", inline.Kind)
	}
}

// (B) ROLE: RequireRole("admin") → role; RequireRole("superuser") → superuser.
func TestGo_RequireRole_RoleAndSuperuser(t *testing.T) {
	reg := NewRegistry()
	role, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "gin"},
		Source: "r.GET(\"/admin\", RequireRole(\"admin\"), h)"})
	if role.Kind != KindRole || role.Literal != "admin" {
		t.Fatalf("got %s/%q, want role/admin (RequireRole)", role.Kind, role.Literal)
	}
	su, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "chi"},
		Source: "r.Use(RequireRole(\"superuser\"))"})
	if su.Kind != KindSuperuser {
		t.Fatalf("got %s, want superuser (RequireRole(superuser))", su.Kind)
	}
	perm, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "echo"},
		Source: "e.GET(\"/export\", RequirePermission(\"export_clients\"), h)"})
	if perm.Kind != KindAction || perm.Literal != "export_clients" {
		t.Fatalf("got %s/%q, want action/export_clients (RequirePermission)", perm.Kind, perm.Literal)
	}
}

// (C) NO-AUTH GROUP: a route reaching no auth-ish middleware → unknown (HONEST
// under-claim, never false-public on a name-heuristic).
func TestGo_NoAuthMiddleware_Unknown(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "gin"},
		Source: "pub := r.Group(\"/public\")\npub.Use(Logger())\npub.GET(\"/health\", healthHandler)"})
	if p.Kind != KindUnknown {
		t.Fatalf("got %s, want unknown (no auth middleware; name-heuristic under-claim)", p.Kind)
	}
}

// (C2) EXPLICIT PUBLIC: auth_required=false / a Public middleware → public.
func TestGo_ExplicitPublic_Public(t *testing.T) {
	reg := NewRegistry()
	byProp, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "gin", "auth_required": "false"}})
	if byProp.Kind != KindPublic {
		t.Fatalf("got %s, want public (auth_required=false)", byProp.Kind)
	}
	byMw, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "chi"},
		Source: "r.Group(func(r chi.Router){\n  r.Use(AllowAnonymous)\n  r.Get(\"/open\", h)\n})"})
	if byMw.Kind != KindPublic {
		t.Fatalf("got %s, want public (AllowAnonymous middleware)", byMw.Kind)
	}
}

// (D) E2E: a Go authenticated route vs a Django page oracle is LOOSER.
func TestGo_E2E_AuthenticatedVsOraclePage_Looser(t *testing.T) {
	reg := NewRegistry()
	oracle, _ := reg.Resolve(Signal{
		Props: map[string]string{"has_get_permissions": "true"}, Source: clientViewSetGetPerms, Action: "approve",
	}) // → page client_admin
	v3, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "gin"},
		Source: "r.Use(AuthMiddleware())\nr.GET(\"/clients\", h)"})
	if d := Diff(v3, oracle); d.Verdict != VerdictLooser {
		t.Fatalf("verdict=%s detail=%s, want looser (Go authenticated vs oracle page)", d.Verdict, d.Detail)
	}
}

// --- #4544: Phoenix plug pipelines / pipe_through ----------------------------

const phoenixRouterAuth = `
pipeline :browser do
  plug :fetch_session
end
pipeline :auth do
  plug :require_authenticated_user
end
scope "/dashboard", MyAppWeb do
  pipe_through [:browser, :auth]
  get "/", DashboardController, :index
end
`

const phoenixRouterPublic = `
pipeline :browser do
  plug :fetch_session
end
scope "/", MyAppWeb do
  pipe_through :browser
  get "/", PageController, :index
end
`

// (A) PROTECTED: a route in a scope `pipe_through [:auth]` whose pipeline plugs
// `:require_authenticated_user` → authenticated.
func TestPhoenix_PipeThroughAuth_Authenticated(t *testing.T) {
	reg := NewRegistry()
	p, fw := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "phoenix"},
		Source: phoenixRouterAuth,
	})
	if fw != "phoenix" {
		t.Fatalf("framework=%s, want phoenix", fw)
	}
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (pipe_through [:auth] → require_authenticated_user)", p.Kind)
	}
}

// (B) PUBLIC: a scope piping through only :browser (no auth pipeline) → public.
func TestPhoenix_PublicScope_Public(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{
		Props:  map[string]string{"framework": "phoenix"},
		Source: phoenixRouterPublic,
	})
	if p.Kind != KindPublic {
		t.Fatalf("got %s, want public (pipe_through :browser only, no auth pipeline)", p.Kind)
	}
}

// (C) ROLE: a `:require_admin` plug → superuser; `plug :require_role, :editor` → role.
func TestPhoenix_RolePlugs(t *testing.T) {
	reg := NewRegistry()
	admin, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "phoenix"},
		Source: "pipeline :admin do\n  plug :require_admin\nend\nscope \"/a\" do\n  pipe_through [:auth, :admin]\n  get \"/\", C, :i\nend"})
	if admin.Kind != KindSuperuser {
		t.Fatalf("got %s, want superuser (plug :require_admin)", admin.Kind)
	}
	role, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "phoenix", "auth_plugs": "plug :require_role, :editor"}})
	if role.Kind != KindRole || role.Literal != "editor" {
		t.Fatalf("got %s/%q, want role/editor (plug :require_role, :editor)", role.Kind, role.Literal)
	}
}

// (C2) Guardian.Plug.EnsureAuthenticated → authenticated.
func TestPhoenix_GuardianEnsureAuthenticated(t *testing.T) {
	reg := NewRegistry()
	p, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "phoenix"},
		Source: "pipeline :auth do\n  plug Guardian.Plug.EnsureAuthenticated\nend\nscope \"/s\" do\n  pipe_through [:auth]\n  get \"/\", C, :i\nend"})
	if p.Kind != KindAuthenticated {
		t.Fatalf("got %s, want authenticated (Guardian.Plug.EnsureAuthenticated)", p.Kind)
	}
}

// (D) E2E: a Phoenix public scope vs a Django authenticated oracle is LOOSER.
func TestPhoenix_E2E_PublicVsOracleAuthenticated_Looser(t *testing.T) {
	reg := NewRegistry()
	oracle, _ := reg.Resolve(Signal{Props: map[string]string{
		"framework": "django", "has_permission_classes": "true", "permission_classes": "IsAuthenticated",
	}})
	v3, _ := reg.Resolve(Signal{Props: map[string]string{"framework": "phoenix"}, Source: phoenixRouterPublic})
	if d := Diff(v3, oracle); d.Verdict != VerdictLooser {
		t.Fatalf("verdict=%s detail=%s, want looser (Phoenix public scope vs oracle authenticated)", d.Verdict, d.Detail)
	}
}
