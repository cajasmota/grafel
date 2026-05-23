// Java auth_policy resolver — Phase 1 of #1942.
//
// Resolves a structured `auth_policy` for each Java endpoint by combining
// four kinds of static signals:
//
//  1. Handler-level annotations.
//     @PermitAll                       → public, high confidence
//     @DenyAll                         → required (always rejects), high
//     @RolesAllowed({"ADMIN","USER"})  → required + roles, high
//     @Secured("ROLE_ADMIN")           → required + roles, high (Spring)
//     @PreAuthorize("hasRole('ADMIN')")→ required + roles, high (Spring)
//
//  2. Class-level annotation inheritance.
//     The same annotations applied to the controller class apply to every
//     method that doesn't override with @PermitAll. Walked here.
//
//  3. Quarkus config-driven policies (application.properties).
//     `quarkus.http.auth.permission.<name>.paths=/api/foo/*`
//     `quarkus.http.auth.permission.<name>.policy=authenticated|deny|permit`
//     `quarkus.http.auth.permission.<name>.roles-allowed=ADMIN,USER`
//
//  4. Framework default posture (Quarkus).
//     When the project declares the `quarkus-security` extension and no
//     @PermitAll / @RolesAllowed annotation covers the endpoint, the
//     framework default is "auth required" with LOW confidence.
//
// Output is a structured AuthPolicy serialized as JSON on the synthetic
// http_endpoint entity (`properties["auth_policy"]`). Flat companion fields
// (`auth_required`, `auth_method`, `auth_confidence`) are also written so
// downstream consumers that don't unmarshal JSON can still filter cheaply.
//
// This file ships Phase 1 (Java only). Phases 2-4 (Python / NestJS / Go)
// reuse the AuthPolicy / AuthSignal shapes and the same dashboard surfacing.
//
// Refs #1942 (Phase 1).
package engine

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// AuthPolicy is the resolved auth posture for a single endpoint.
//
// Confidence levels:
//   - high   = explicit annotation directly on the handler or its class.
//   - medium = config-driven policy matching the endpoint path.
//   - low    = framework default (no explicit signal).
type AuthPolicy struct {
	Required    bool         `json:"required"`
	Method      string       `json:"method"` // "annotation" | "middleware" | "config" | "framework_default" | "unknown"
	Roles       []string     `json:"roles,omitempty"`
	Scopes      []string     `json:"scopes,omitempty"`
	Confidence  string       `json:"confidence"` // "high" | "medium" | "low"
	SourceChain []AuthSignal `json:"source_chain,omitempty"`
}

// AuthSignal is one piece of evidence that contributed to the policy.
type AuthSignal struct {
	Kind     string `json:"kind"` // "annotation" | "middleware" | "config" | "framework_default"
	EntityID string `json:"entity_id,omitempty"`
	Text     string `json:"text"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// JavaAuthContext carries cross-file signals used during resolution. It is
// optional — when empty the resolver falls back to handler/class-only
// signals (still useful for Spring projects without a Quarkus config).
type JavaAuthContext struct {
	// QuarkusSecurityEnabled is true when the project pulls in the
	// quarkus-security / quarkus-smallrye-jwt / quarkus-oidc extension
	// (detected from pom.xml or build.gradle). When true, the framework
	// default for any endpoint without @PermitAll is "auth required" at
	// LOW confidence.
	QuarkusSecurityEnabled bool

	// QuarkusSecurityFile is the path of the build descriptor that
	// declared the security extension (used for the source-chain entry).
	QuarkusSecurityFile string

	// QuarkusPermissions are the parsed `quarkus.http.auth.permission.*`
	// blocks from application.properties. Each entry is checked against
	// the endpoint path; a match contributes a config-source signal.
	QuarkusPermissions []QuarkusPermission
}

// QuarkusPermission models one `quarkus.http.auth.permission.<name>.*`
// block from a Quarkus application.properties.
type QuarkusPermission struct {
	Name         string
	Paths        []string // raw path patterns from `.paths=`
	Policy       string   // "authenticated" | "deny" | "permit" | <named-policy>
	RolesAllowed []string
	File         string
	Line         int
}

// Annotation regexes (handler-/class-level).
var (
	javaPermitAllRe = regexp.MustCompile(`@PermitAll\b`)
	javaDenyAllRe   = regexp.MustCompile(`@DenyAll\b`)
	// @RolesAllowed("ADMIN") | @RolesAllowed({"ADMIN","USER"}) | @RolesAllowed(value = {...})
	javaRolesAllowedRe = regexp.MustCompile(`@RolesAllowed\s*\(([^)]*)\)`)
	// Spring @Secured("ROLE_ADMIN") / @Secured({"ROLE_ADMIN","ROLE_USER"})
	javaSecuredRe = regexp.MustCompile(`@Secured\s*\(([^)]*)\)`)
	// Spring @PreAuthorize("hasRole('ADMIN')") / @PreAuthorize("hasAnyRole('A','B')")
	javaPreAuthorizeRe = regexp.MustCompile(`@PreAuthorize\s*\(\s*"([^"]*)"\s*\)`)
)

// authRoleArgRe extracts quoted role names from an annotation argument list,
// stripping the conventional Spring "ROLE_" prefix to keep policies portable
// across frameworks.
var authRoleArgRe = regexp.MustCompile(`"([^"]+)"`)

// preAuthRoleRe extracts role names from a Spring SpEL expression such as
// `hasRole('ADMIN')`, `hasAnyRole('ADMIN','USER')`, or `hasAuthority('SCOPE_x')`.
var preAuthRoleRe = regexp.MustCompile(`(?:hasRole|hasAnyRole|hasAuthority|hasAnyAuthority)\s*\(\s*([^)]+)\)`)

// preAuthQuotedRe pulls quoted args (single or double quoted) out of the SpEL
// expression captured by preAuthRoleRe.
var preAuthQuotedRe = regexp.MustCompile(`['"]([^'"]+)['"]`)

// ResolveJavaAuthPolicy combines per-method, per-class, and project-level
// signals into a structured AuthPolicy.
//
//   - methodAnnoText: the joined annotation block immediately above the
//     handler method declaration.
//   - methodLine: 1-based line number where the handler method is declared.
//   - classAnnoText: the joined annotation block immediately above the
//     controller class declaration.
//   - classLine: 1-based line number where the controller class is declared.
//   - file: repo-relative source file path.
//   - canonicalPath: the resolved endpoint route (e.g. "/auth/login"). Used
//     to match Quarkus permission path patterns.
//   - ctx: optional cross-file context (Quarkus extension + permissions).
//
// Precedence (highest first):
//  1. Method-level @PermitAll / @DenyAll / @RolesAllowed / @Secured / @PreAuthorize.
//  2. Class-level @RolesAllowed / @Secured / @PreAuthorize / @PermitAll.
//  3. Quarkus permission policy matching `canonicalPath`.
//  4. Quarkus framework default (when security extension is present).
//  5. "unknown" (no signal at all).
func ResolveJavaAuthPolicy(
	methodAnnoText string,
	methodLine int,
	classAnnoText string,
	className string,
	classLine int,
	file string,
	canonicalPath string,
	ctx JavaAuthContext,
) AuthPolicy {
	// ---- 1. Method-level signals (highest precedence) ----
	if mp, ok := matchAnnotationPolicy(methodAnnoText, methodLine, file, "method"); ok {
		return mp
	}

	// ---- 2. Class-level inheritance ----
	if cp, ok := matchAnnotationPolicy(classAnnoText, classLine, file, "class"); ok {
		// Decorate the source-chain text to make it obvious the signal
		// comes from the class, not the method.
		for i := range cp.SourceChain {
			cp.SourceChain[i].EntityID = className
		}
		return cp
	}

	// ---- 3. Quarkus config-driven policy ----
	if canonicalPath != "" {
		for _, perm := range ctx.QuarkusPermissions {
			if quarkusPermMatches(perm, canonicalPath) {
				return policyFromQuarkusPermission(perm)
			}
		}
	}

	// ---- 4. Quarkus framework default ----
	if ctx.QuarkusSecurityEnabled {
		return AuthPolicy{
			Required:   true,
			Method:     "framework_default",
			Confidence: "low",
			SourceChain: []AuthSignal{{
				Kind: "framework_default",
				Text: "quarkus-security extension detected; default posture requires authentication unless @PermitAll covers the handler",
				File: ctx.QuarkusSecurityFile,
				Line: 0,
			}},
		}
	}

	// ---- 5. Unknown — no signal at all ----
	return AuthPolicy{
		Method:     "unknown",
		Confidence: "low",
	}
}

// matchAnnotationPolicy inspects a single annotation block (method-level or
// class-level) and produces an AuthPolicy if any auth annotation matched.
// The bool indicates whether a match was found. `tier` is "method" or
// "class" — used only for the source-chain `kind` (always "annotation",
// but the surrounding caller may decorate further).
func matchAnnotationPolicy(annoText string, line int, file, _ string) (AuthPolicy, bool) {
	if annoText == "" {
		return AuthPolicy{}, false
	}

	// @PermitAll — explicit public.
	if javaPermitAllRe.MatchString(annoText) {
		return AuthPolicy{
			Required:   false,
			Method:     "annotation",
			Confidence: "high",
			SourceChain: []AuthSignal{{
				Kind: "annotation",
				Text: "@PermitAll",
				File: file,
				Line: line,
			}},
		}, true
	}

	// @DenyAll — explicit blanket reject. Treat as required=true (any
	// request will be rejected) so the dashboard surfaces it as locked
	// down rather than public.
	if javaDenyAllRe.MatchString(annoText) {
		return AuthPolicy{
			Required:   true,
			Method:     "annotation",
			Confidence: "high",
			SourceChain: []AuthSignal{{
				Kind: "annotation",
				Text: "@DenyAll",
				File: file,
				Line: line,
			}},
		}, true
	}

	// @RolesAllowed({...}) — JAX-RS / Jakarta Security.
	if m := javaRolesAllowedRe.FindStringSubmatch(annoText); m != nil {
		roles := extractQuotedTokens(m[1])
		return AuthPolicy{
			Required:   true,
			Method:     "annotation",
			Roles:      roles,
			Confidence: "high",
			SourceChain: []AuthSignal{{
				Kind: "annotation",
				Text: "@RolesAllowed(" + strings.TrimSpace(m[1]) + ")",
				File: file,
				Line: line,
			}},
		}, true
	}

	// @Secured(...) — Spring.
	if m := javaSecuredRe.FindStringSubmatch(annoText); m != nil {
		roles := stripRolePrefix(extractQuotedTokens(m[1]))
		return AuthPolicy{
			Required:   true,
			Method:     "annotation",
			Roles:      roles,
			Confidence: "high",
			SourceChain: []AuthSignal{{
				Kind: "annotation",
				Text: "@Secured(" + strings.TrimSpace(m[1]) + ")",
				File: file,
				Line: line,
			}},
		}, true
	}

	// @PreAuthorize("...") — Spring SpEL.
	if m := javaPreAuthorizeRe.FindStringSubmatch(annoText); m != nil {
		roles := parsePreAuthorizeRoles(m[1])
		return AuthPolicy{
			Required:   true,
			Method:     "annotation",
			Roles:      roles,
			Confidence: "high",
			SourceChain: []AuthSignal{{
				Kind: "annotation",
				Text: "@PreAuthorize(\"" + m[1] + "\")",
				File: file,
				Line: line,
			}},
		}, true
	}

	return AuthPolicy{}, false
}

// extractQuotedTokens pulls every "quoted" token out of an annotation
// argument list. Preserves source order.
func extractQuotedTokens(s string) []string {
	matches := authRoleArgRe.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		v := strings.TrimSpace(m[1])
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// stripRolePrefix removes Spring's conventional "ROLE_" prefix from each
// captured role so policies are portable across frameworks.
func stripRolePrefix(roles []string) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, strings.TrimPrefix(r, "ROLE_"))
	}
	return out
}

// parsePreAuthorizeRoles parses a Spring SpEL expression such as
// `hasRole('ADMIN')` or `hasAnyRole('ADMIN','USER')` and returns the
// captured role names. Returns nil when no role helper is recognised.
func parsePreAuthorizeRoles(spel string) []string {
	var roles []string
	for _, m := range preAuthRoleRe.FindAllStringSubmatch(spel, -1) {
		for _, q := range preAuthQuotedRe.FindAllStringSubmatch(m[1], -1) {
			r := strings.TrimPrefix(strings.TrimSpace(q[1]), "ROLE_")
			if r != "" {
				roles = append(roles, r)
			}
		}
	}
	return roles
}

// quarkusPermMatches reports whether any of the permission's path patterns
// covers `endpointPath`. The grammar matches Quarkus's documented behaviour:
//   - exact match
//   - `*` suffix wildcard (`/api/foo/*` matches `/api/foo/bar`)
func quarkusPermMatches(perm QuarkusPermission, endpointPath string) bool {
	for _, pat := range perm.Paths {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if pat == endpointPath {
			return true
		}
		if strings.HasSuffix(pat, "/*") {
			prefix := strings.TrimSuffix(pat, "/*")
			if endpointPath == prefix || strings.HasPrefix(endpointPath, prefix+"/") {
				return true
			}
		}
		if strings.HasSuffix(pat, "*") {
			prefix := strings.TrimSuffix(pat, "*")
			if strings.HasPrefix(endpointPath, prefix) {
				return true
			}
		}
	}
	return false
}

// policyFromQuarkusPermission projects a Quarkus permission block onto an
// AuthPolicy. Confidence is "medium" — config-driven but not annotation-direct.
func policyFromQuarkusPermission(perm QuarkusPermission) AuthPolicy {
	pol := strings.ToLower(strings.TrimSpace(perm.Policy))
	required := true
	if pol == "permit" {
		required = false
	}
	signal := AuthSignal{
		Kind: "config",
		Text: "quarkus.http.auth.permission." + perm.Name + ".policy=" + perm.Policy,
		File: perm.File,
		Line: perm.Line,
	}
	return AuthPolicy{
		Required:    required,
		Method:      "config",
		Roles:       append([]string(nil), perm.RolesAllowed...),
		Confidence:  "medium",
		SourceChain: []AuthSignal{signal},
	}
}

// EncodeAuthPolicy serializes the policy to a stable JSON string suitable
// for storing in an EntityRecord.Properties map. Roles/Scopes are sorted
// to keep the encoded form deterministic across runs.
func EncodeAuthPolicy(p AuthPolicy) string {
	cp := p
	if len(cp.Roles) > 1 {
		sort.Strings(cp.Roles)
	}
	if len(cp.Scopes) > 1 {
		sort.Strings(cp.Scopes)
	}
	b, err := json.Marshal(cp)
	if err != nil {
		return ""
	}
	return string(b)
}

// DecodeAuthPolicy is the inverse of EncodeAuthPolicy. Returns the zero
// AuthPolicy when the input is empty or malformed (defensive — callers
// fall back to the "unknown" chip).
func DecodeAuthPolicy(s string) AuthPolicy {
	if s == "" {
		return AuthPolicy{Method: "unknown", Confidence: "low"}
	}
	var p AuthPolicy
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return AuthPolicy{Method: "unknown", Confidence: "low"}
	}
	return p
}

// ---------------------------------------------------------------------------
// Quarkus context extraction
// ---------------------------------------------------------------------------

// quarkusSecurityExtensionMarkers are the artifact identifiers that signal a
// Quarkus security extension is on the classpath. When any of these appears
// in pom.xml or build.gradle the framework default for unannotated endpoints
// becomes "auth required" at LOW confidence.
var quarkusSecurityExtensionMarkers = []string{
	"quarkus-security",
	"quarkus-smallrye-jwt",
	"quarkus-oidc",
	"quarkus-elytron-security",
	"quarkus-keycloak-authorization",
}

// DetectQuarkusSecurityExtension returns (true, descriptorFile) when any of
// the known Quarkus security extension markers appears in the supplied
// pom.xml / build.gradle / build.gradle.kts content map.
func DetectQuarkusSecurityExtension(buildDescriptors map[string]string) (bool, string) {
	// Stable iteration order so the picked descriptor is deterministic.
	paths := make([]string, 0, len(buildDescriptors))
	for p := range buildDescriptors {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		content := buildDescriptors[p]
		for _, marker := range quarkusSecurityExtensionMarkers {
			if strings.Contains(content, marker) {
				return true, p
			}
		}
	}
	return false, ""
}

// quarkusPermissionLineRe matches one application.properties line of the form
//
//	quarkus.http.auth.permission.<name>.<field>=<value>
//
// where <field> is one of `paths`, `policy`, `roles-allowed`.
var quarkusPermissionLineRe = regexp.MustCompile(
	`^\s*quarkus\.http\.auth\.permission\.([^.]+)\.(paths|policy|roles-allowed)\s*=\s*(.+?)\s*$`,
)

// ParseQuarkusPermissions parses a Quarkus application.properties file and
// returns one QuarkusPermission per `quarkus.http.auth.permission.<name>.*`
// block found. Unknown fields are ignored. Comments (`#` / `!`) are skipped.
func ParseQuarkusPermissions(content, file string) []QuarkusPermission {
	if content == "" {
		return nil
	}
	byName := map[string]*QuarkusPermission{}
	order := []string{}
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		m := quarkusPermissionLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, field, value := m[1], m[2], m[3]
		perm, ok := byName[name]
		if !ok {
			perm = &QuarkusPermission{Name: name, File: file, Line: i + 1}
			byName[name] = perm
			order = append(order, name)
		}
		switch field {
		case "paths":
			perm.Paths = splitAuthCSV(value)
		case "policy":
			perm.Policy = value
		case "roles-allowed":
			perm.RolesAllowed = splitAuthCSV(value)
		}
	}
	out := make([]QuarkusPermission, 0, len(order))
	for _, n := range order {
		out = append(out, *byName[n])
	}
	return out
}

// splitAuthCSV splits a comma-separated value, trims each token, and drops
// empties.
func splitAuthCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// BuildJavaAuthContext is the convenience constructor: given the raw text of
// every pom.xml / build.gradle / build.gradle.kts and every
// application.properties in the repo, it produces a populated
// JavaAuthContext ready to pass into ApplyJavaAnnotationRoutesWithContext.
func BuildJavaAuthContext(
	buildDescriptors map[string]string,
	propertiesFiles map[string]string,
) JavaAuthContext {
	ctx := JavaAuthContext{}
	if ok, file := DetectQuarkusSecurityExtension(buildDescriptors); ok {
		ctx.QuarkusSecurityEnabled = true
		ctx.QuarkusSecurityFile = file
	}
	// Stable order for deterministic permission precedence.
	files := make([]string, 0, len(propertiesFiles))
	for f := range propertiesFiles {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		ctx.QuarkusPermissions = append(
			ctx.QuarkusPermissions,
			ParseQuarkusPermissions(propertiesFiles[f], f)...,
		)
	}
	return ctx
}
