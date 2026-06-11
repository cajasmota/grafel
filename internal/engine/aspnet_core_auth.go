// aspnet_core_auth.go — ASP.NET Core [Authorize]/[AllowAnonymous] posture
// stamping onto the synthesized http_endpoint_definition entities (#4750).
//
// synthesizeASPNetCore (aspnet_core_routes.go) emits the route endpoints but
// DISCARDS the [Authorize]/[AllowAnonymous]/policy/roles attributes in the
// intervening-attribute stack, so the authposture aspnet resolver had NO props
// to decode and honestly reported unknown. This post-pass re-parses the C# file
// and stamps the STRUCTURED method ▸ class ▸ global posture the resolver reads
// (internal/authposture/aspnet.go):
//
//	method  → auth_required / auth_roles / auth_policy / allow_anonymous
//	class   → aspnet_class_authorize / aspnet_class_roles / aspnet_class_policy /
//	          aspnet_class_allow_anonymous
//	global  → aspnet_fallback_policy (a Program.cs FallbackPolicy in the same file
//	          — the cross-file Startup case stays the documented source-scan gap)
//
// It mirrors the in-place post-pass pattern applyHotChocolateAuthShapes /
// applyLaravelRateLimit use: it only mutates Properties on the aspnet_core
// endpoints this file just produced; it never adds or removes entities.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

var (
	// [Authorize(Roles = "Admin,Mgr")] — first role captured by the resolver.
	aspnetAuthRolesRe = regexp.MustCompile(`\[\s*Authorize\s*\([^)]*Roles\s*=\s*"([^"]+)"`)
	// [Authorize(Policy = "CanEdit")].
	aspnetAuthPolicyRe = regexp.MustCompile(`\[\s*Authorize\s*\([^)]*Policy\s*=\s*"([^"]+)"`)
	// A bare [Authorize] / [Authorize(AuthenticationSchemes=...)] (no Roles/Policy).
	aspnetAuthBareRe = regexp.MustCompile(`\[\s*Authorize\b`)
	// [AllowAnonymous].
	aspnetAllowAnonAttrRe = regexp.MustCompile(`\[\s*AllowAnonymous\s*\]`)

	// A method declaration with its preceding attribute stack. Group 1 = the
	// attribute block (may be empty), group 2 = the method name. Anchored on the
	// HTTP verb attribute so we only scan action methods.
	aspnetActionBlockRe = regexp.MustCompile(
		`((?:[ \t]*\[[^\]\r\n]+\][ \t]*[\r\n]+)+)[ \t]*` +
			`(?:public|protected|private|internal|static|virtual|override|sealed|async|\s)+` +
			`[\w<>\[\],.\s?]+?\s+([A-Za-z_]\w*)\s*\(`)

	// class-level attribute stack + the controller class declaration.
	aspnetClassBlockRe = regexp.MustCompile(
		`((?:[ \t]*\[[^\]\r\n]+\][ \t]*[\r\n]+)+)[ \t]*` +
			`(?:public|internal|sealed|abstract|partial|static|\s)*` +
			`class\s+([A-Za-z_]\w*Controller)\b`)

	// Program.cs FallbackPolicy: `o.FallbackPolicy = new
	// AuthorizationPolicyBuilder().RequireAuthenticatedUser().Build();`
	aspnetFallbackPolicyRe = regexp.MustCompile(`FallbackPolicy\s*=`)
)

// aspnetAttrPosture is the resolved attribute posture for one block.
type aspnetAttrPosture struct {
	allowAnon bool
	authorize bool
	roles     string
	policy    string
}

// resolveAspnetAttrBlock decodes an attribute block into a posture.
func resolveAspnetAttrBlock(block string) aspnetAttrPosture {
	var p aspnetAttrPosture
	if aspnetAllowAnonAttrRe.MatchString(block) {
		p.allowAnon = true
	}
	if m := aspnetAuthRolesRe.FindStringSubmatch(block); m != nil {
		p.authorize = true
		p.roles = m[1]
	}
	if m := aspnetAuthPolicyRe.FindStringSubmatch(block); m != nil {
		p.authorize = true
		p.policy = m[1]
	}
	if aspnetAuthBareRe.MatchString(block) {
		p.authorize = true
	}
	return p
}

// applyAspnetCoreAuth stamps the method ▸ class ▸ global [Authorize] posture onto
// the aspnet_core endpoints emitted for `path` (#4750). Cheap-gated on the file
// carrying an [Authorize]/[AllowAnonymous] attribute or a FallbackPolicy.
func applyAspnetCoreAuth(content, path string, entities []types.EntityRecord, before int) {
	if before >= len(entities) {
		return
	}
	if !strings.Contains(content, "[Authorize") && !strings.Contains(content, "[AllowAnonymous") &&
		!aspnetFallbackPolicyRe.MatchString(content) {
		return
	}

	// Per-action attribute postures, keyed by method name.
	methodPosture := map[string]aspnetAttrPosture{}
	// The action source block (attributes + signature) for the source-scan
	// fallback (#4752), keyed by method name.
	methodSource := map[string]string{}
	for _, m := range aspnetActionBlockRe.FindAllStringSubmatch(content, -1) {
		block := m[1]
		name := m[2]
		methodPosture[name] = resolveAspnetAttrBlock(block)
		methodSource[name] = strings.TrimSpace(m[0])
	}

	// Class-level posture (first controller class wins — one per file is the norm).
	var classPosture aspnetAttrPosture
	if m := aspnetClassBlockRe.FindStringSubmatch(content); m != nil {
		classPosture = resolveAspnetAttrBlock(m[1])
	}

	// Global FallbackPolicy declared in this file (best-effort; same-file only).
	fallback := ""
	if aspnetFallbackPolicyRe.MatchString(content) {
		if strings.Contains(content, "RequireAuthenticatedUser") {
			fallback = "RequireAuthenticatedUser"
		} else {
			fallback = "FallbackPolicy"
		}
	}

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path || e.Properties == nil {
			continue
		}
		if e.Properties["framework"] != "aspnet_core" {
			continue
		}
		method := aspnetEndpointMethodName(e.Properties["source_handler"])

		// (1) METHOD level — the per-action attribute block.
		if mp, ok := methodPosture[method]; ok {
			stampAspnetMethodPosture(e.Properties, mp)
		}
		if src, ok := methodSource[method]; ok && src != "" {
			e.Properties["action_source"] = src
		}
		// (2) CLASS level — stamped as aspnet_class_* (the resolver applies them
		// only when no method attribute covers the action).
		stampAspnetClassPosture(e.Properties, classPosture)
		// (3) GLOBAL level — the same-file FallbackPolicy.
		if fallback != "" {
			e.Properties["aspnet_fallback_policy"] = fallback
		}
	}
}

// stampAspnetMethodPosture writes the method-level attribute posture: an
// [AllowAnonymous] override (allow_anonymous=true / auth_required=false) wins;
// otherwise [Authorize(Roles/Policy)] → auth_roles/auth_policy + auth_required.
func stampAspnetMethodPosture(props map[string]string, p aspnetAttrPosture) {
	if p.allowAnon {
		props["allow_anonymous"] = "true"
		props["auth_required"] = "false"
		return
	}
	if !p.authorize {
		return
	}
	props["auth_required"] = "true"
	if p.roles != "" {
		props["auth_roles"] = p.roles
	}
	if p.policy != "" {
		props["auth_policy"] = p.policy
	}
}

// stampAspnetClassPosture writes the class-level attribute posture as the
// aspnet_class_* props the resolver reads for actions without their own attribute.
func stampAspnetClassPosture(props map[string]string, p aspnetAttrPosture) {
	if p.allowAnon {
		props["aspnet_class_allow_anonymous"] = "true"
	}
	if p.roles != "" {
		props["aspnet_class_roles"] = p.roles
	}
	if p.policy != "" {
		props["aspnet_class_policy"] = p.policy
	}
	if p.authorize && p.roles == "" && p.policy == "" {
		props["aspnet_class_authorize"] = "true"
	}
}

// aspnetEndpointMethodName extracts the action method name from a
// `SCOPE.Operation:<Class>.<Method>` source_handler.
func aspnetEndpointMethodName(sourceHandler string) string {
	op := sourceHandler
	if c := strings.LastIndexByte(op, ':'); c >= 0 {
		op = op[c+1:]
	}
	if d := strings.LastIndexByte(op, '.'); d >= 0 {
		op = op[d+1:]
	}
	return op
}
