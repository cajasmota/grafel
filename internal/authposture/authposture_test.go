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
	want := []string{"aspnet", "django-drf", "fastapi", "flask", "go-middleware", "laravel", "nestjs", "phoenix", "rails", "spring-security"}
	if len(fws) != len(want) {
		t.Fatalf("frameworks=%v, want %v", fws, want)
	}
	for i := range want {
		if fws[i] != want[i] {
			t.Fatalf("frameworks=%v, want %v", fws, want)
		}
	}
	// A Spring-shaped signal with no resolver implemented yet → unknown.
	p, fw := reg.Resolve(Signal{Props: map[string]string{"pre_authorize": "hasRole('ADMIN')"}})
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
