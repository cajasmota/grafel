// controller_auth.go — Rails controller endpoint-protection stamping
// (#3734, child of #3628 area #6; sibling of #3696's Python auth_endpoint.go).
//
// #3696 established the flat per-endpoint auth contract — stamp the route
// `SCOPE.Operation/endpoint` op itself with `auth_required` / `auth_method` /
// `auth_guard` / `auth_roles`. The Rails routing extractor (rails_routes.go)
// emits route ops from config/routes.rb, but a route's auth posture lives in
// the *controller* (`before_action :authenticate_user!`), a different file.
// The rails Auth registry note named this exact gap as the honest remainder:
// "inferring which controller actions are protected by a controller-level
// before_action ... not modelled."
//
// This extractor closes it *in-file*: for each ActionController subclass it
// resolves the controller-level Devise `before_action :authenticate_<model>!`
// (honouring `only:` / `except:` scoping), plus Pundit / CanCanCan guards, and
// emits one protection op per controller action — a
// `SCOPE.Operation/endpoint` carrying the controller#action handler ref and the
// #3696 flat contract. The route extractor's routes.rb op and this controller
// op share the same `controller#action` handler key, so the security dashboard
// and archigraph_auth_coverage resolve the same posture from either side.
//
// Recognised guards:
//
//	Devise    — `before_action :authenticate_user!` / `:authenticate_admin!`
//	            → every action of the controller (modulo only:/except:) is
//	            auth_required, method=before_action, guard=authenticate_<m>!.
//	            `skip_before_action :authenticate_user!, only: [:index]` removes
//	            the named actions from the protected set (honest scoping).
//	require_*  — `before_action :require_login` / `:authenticate` (Sorcery /
//	            Clearance / hand-rolled) → same posture, guard = the symbol.
//	Pundit     — `before_action :verify_authorized` / per-action `authorize @x`
//	            inside an action body → method=pundit, guard=authorize.
//	CanCanCan  — `load_and_authorize_resource` (controller-level) → all actions
//	            protected; `authorize_resource only: [...]` honoured.
//
// Honest-partial: roles/scopes are not synthesised from opaque policy objects
// (Pundit policies / CanCanCan abilities are out-of-file); a `before_action`
// whose method is resolved dynamically is out of scope. Confidence is "high"
// for a direct controller-level authenticate guard, "medium" for a per-action
// authorize call (the action is protected but the guard is body-local).
package ruby

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_ruby_controller_auth", &railsControllerAuthExtractor{})
}

type railsControllerAuthExtractor struct{}

func (e *railsControllerAuthExtractor) Language() string {
	return "custom_ruby_controller_auth"
}

var (
	// class FooController < ApplicationController / < ActionController::Base
	caClassRe = regexp.MustCompile(
		`(?m)^\s*class\s+([A-Z][A-Za-z0-9_:]*Controller)\b[^\n]*`)

	// before_action :authenticate_user! [, only: [...] | except: [...]]
	// Trailing options are captured with [ \t]* (NOT \s*) so the option region
	// never bleeds across the newline into the following line.
	caAuthenticateRe = regexp.MustCompile(
		`(?m)^[ \t]*before_action\s+:authenticate_([a-z_]+)![ \t]*([^\n\r]*)$`)

	// before_action :require_login / :authenticate / :require_auth / etc.
	caRequireAuthRe = regexp.MustCompile(
		`(?m)^[ \t]*before_action\s+:(require_login|login_required|require_auth|authenticate|require_user|authorize_user|check_authentication|verify_authorized)\b[ \t]*([^\n\r]*)$`)

	// skip_before_action :authenticate_user!, only: [:index]
	caSkipRe = regexp.MustCompile(
		`(?m)^[ \t]*skip_before_action\s+:(?:authenticate_[a-z_]+!|require_login|login_required|authenticate)[ \t]*([^\n\r]*)$`)

	// controller-level CanCanCan: load_and_authorize_resource [only: [...]]
	caCanCanResourceRe = regexp.MustCompile(
		`(?m)^[ \t]*(load_and_authorize_resource|authorize_resource)\b[ \t]*([^\n\r]*)$`)

	// def action_name  — a controller action method declaration.
	caActionDefRe = regexp.MustCompile(`(?m)^\s*def\s+([a-z_][a-z0-9_]*[!?]?)\s*(?:\(|$)`)

	// per-action Pundit authorize call inside a method body: authorize @post
	caPunditAuthorizeRe = regexp.MustCompile(`(?m)^\s*authorize\s+[@a-z_]`)

	// Pundit `authorize @post` — capture the authorized record symbol so we can
	// derive the policy literal (`@post` → `Post` policy). Group 1 = bare name.
	caPunditRecordRe = regexp.MustCompile(`(?m)^\s*authorize\s+@?([A-Za-z_][\w]*)`)

	// Pundit `authorize @post, :destroy?` — the trailing symbol is the explicit
	// policy method. Group 1 = the policy-method name (without the `?`).
	caPunditActionRe = regexp.MustCompile(`(?m)^\s*authorize\s+[@A-Za-z_][\w.]*\s*,\s*:([a-z_]+)\??`)

	// CanCanCan `authorize! :destroy, @post` — the leading symbol is the ability.
	// Group 1 = the ability name.
	caCanCanActionRe = regexp.MustCompile(`(?m)^\s*authorize!\s+:([a-z_]+)`)

	// only:/except: option lists, reused from the routes parser shape.
	caOnlyRe   = regexp.MustCompile(`\bonly:\s*(?:\[([^\]]*)\]|:(\w+))`)
	caExceptRe = regexp.MustCompile(`\bexcept:\s*(?:\[([^\]]*)\]|:(\w+))`)
)

// caControllerLevelAuth is the resolved controller-wide auth posture.
type caControllerLevelAuth struct {
	required   bool
	guard      string
	method     string // "before_action" | "cancancan"
	confidence string
	onlyset    map[string]bool // when non-nil, ONLY these actions are protected
	exceptset  map[string]bool // these actions are NOT protected
	// skips maps action name → true when a skip_before_action removed it.
	skips map[string]bool
}

func (e *railsControllerAuthExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.rails_controller_auth.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		))
	defer span.End()

	if len(file.Content) == 0 || file.Language != "ruby" {
		return nil, nil
	}
	src := string(file.Content)
	// Fast guard: must look like a controller with an auth guard.
	if !strings.Contains(src, "Controller") ||
		!(strings.Contains(src, "before_action") ||
			strings.Contains(src, "load_and_authorize_resource") ||
			strings.Contains(src, "authorize_resource") ||
			strings.Contains(src, "authorize ") ||
			strings.Contains(src, "authorize!")) {
		return nil, nil
	}

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

	for _, cls := range caClassRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[cls[2]:cls[3]]
		body := caClassBody(src, cls[1])
		bodyStart := cls[1]

		level := resolveControllerLevelAuth(body)
		actions := caCollectActions(body)
		if len(actions) == 0 {
			continue
		}

		resource := caControllerResource(className)

		for _, act := range actions {
			posture := caResolveAction(level, body, act.name)
			if !posture.required {
				continue
			}
			handler := resource + "#" + act.name
			ln := lineOf(src, bodyStart+act.off)
			ent := makeEntity(handler, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent, "framework", "rails",
				"provenance", "INFERRED_FROM_RAILS_CONTROLLER_AUTH",
				"controller", className,
				"controller_action", handler,
				"action", act.name)
			posture.stamp(ent.Properties)
			// #4752 — stamp the controller source body so the rails resolver's
			// source-scan fallback fires in the LIVE diff for any guard shape the
			// structured props above don't cover (custom before_action symbols, etc.).
			if body := caSourceSlice(body); body != "" {
				ent.Properties["controller_source"] = body
			}
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// caActionPosture is the resolved posture for a single controller action.
type caActionPosture struct {
	required   bool
	guard      string
	method     string
	confidence string
	// permission is the specific Pundit policy method / CanCanCan ability the
	// action checks (`authorize @post, :destroy?` → "destroy"). Empty when the
	// guard is a coarse authenticate before_action (authn, not authz).
	permission string
	// punditPolicy is the Pundit policy class literal the resolver decodes the
	// exact action grant from (`authorize @post` → "Post"). Empty for non-Pundit.
	punditPolicy string
	// punditAction is the explicit Pundit policy method (`authorize @post,
	// :destroy?` → "destroy"). Empty when the action defaults to the controller
	// action name.
	punditAction string
	// cancancanAbility is the CanCanCan ability literal (`authorize! :destroy` →
	// "destroy" / `load_and_authorize_resource` → the controller action). Empty
	// for non-CanCanCan.
	cancancanAbility string
}

func (p caActionPosture) stamp(props map[string]string) {
	if !p.required {
		return
	}
	props["auth_required"] = "true"
	props["auth_method"] = p.method
	props["auth_confidence"] = p.confidence
	if p.guard != "" {
		props["auth_guard"] = p.guard
	}
	// #authz — the specific authorization action required, so
	// archigraph_auth_coverage answers "what permission does this route require?".
	if p.permission != "" {
		props["auth_permissions"] = p.permission
	}
	// #4751 — stamp the Pundit policy/action and CanCanCan ability LITERALS so the
	// rails resolver decodes the exact action grant (KindAction with the policy
	// codename) instead of degrading to a coarse posture from the source scan.
	if p.punditPolicy != "" {
		props["pundit_policy"] = p.punditPolicy
	}
	if p.punditAction != "" {
		props["pundit_action"] = p.punditAction
	}
	if p.cancancanAbility != "" {
		props["cancancan_ability"] = p.cancancanAbility
	}
}

// resolveControllerLevelAuth scans a controller body for the controller-wide
// auth guard (the first authenticate/require/cancancan before_action) plus the
// skip set. Returns required=false when no controller-level guard is present.
func resolveControllerLevelAuth(body string) caControllerLevelAuth {
	var a caControllerLevelAuth
	a.skips = map[string]bool{}

	if m := caAuthenticateRe.FindStringSubmatch(body); m != nil {
		a.required = true
		a.guard = "authenticate_" + m[1] + "!"
		a.method = "before_action"
		a.confidence = "high"
		a.applyScope(m[2])
	} else if m := caRequireAuthRe.FindStringSubmatch(body); m != nil {
		a.required = true
		a.guard = m[1]
		a.method = "before_action"
		a.confidence = "high"
		a.applyScope(m[2])
	} else if m := caCanCanResourceRe.FindStringSubmatch(body); m != nil {
		a.required = true
		a.guard = m[1]
		a.method = "cancancan"
		a.confidence = "high"
		a.applyScope(m[2])
	}

	// skip_before_action removes named actions from the protected set. The
	// `only:` list names the actions to skip; a bare skip (no only:/except:)
	// removes the guard from ALL actions (the "*" sentinel).
	for _, m := range caSkipRe.FindAllStringSubmatch(body, -1) {
		opts := m[1]
		hasScope := false
		if om := caOnlyRe.FindStringSubmatch(opts); om != nil {
			hasScope = true
			for _, act := range caParseActionList(om[1] + om[2]) {
				a.skips[act] = true
			}
		}
		if em := caExceptRe.FindStringSubmatch(opts); em != nil {
			// skip ... except: [:x] → skip everything but x. Modelled as a
			// full skip (honest-partial: per-action except on skip is rare).
			hasScope = true
			a.skips["*"] = true
			_ = em
		}
		if !hasScope {
			a.skips["*"] = true
		}
	}
	return a
}

// applyScope records only:/except: action filters from a before_action option
// string onto the posture.
func (a *caControllerLevelAuth) applyScope(opts string) {
	if m := caOnlyRe.FindStringSubmatch(opts); m != nil {
		a.onlyset = map[string]bool{}
		for _, act := range caParseActionList(m[1] + m[2]) {
			a.onlyset[act] = true
		}
	}
	if m := caExceptRe.FindStringSubmatch(opts); m != nil {
		a.exceptset = map[string]bool{}
		for _, act := range caParseActionList(m[1] + m[2]) {
			a.exceptset[act] = true
		}
	}
}

// caResolveAction combines the controller-level guard, its only:/except: scope,
// any skip_before_action, and a per-action Pundit authorize call into the final
// posture for one action.
func caResolveAction(level caControllerLevelAuth, body, action string) caActionPosture {
	// Controller-level guard with scoping honoured.
	if level.required {
		protected := true
		if level.onlyset != nil {
			protected = level.onlyset[action]
		}
		if level.exceptset != nil && level.exceptset[action] {
			protected = false
		}
		if level.skips["*"] || level.skips[action] {
			protected = false
		}
		if protected {
			p := caActionPosture{
				required: true, guard: level.guard,
				method: level.method, confidence: level.confidence,
			}
			// #4751 — a controller-level `load_and_authorize_resource` (CanCanCan)
			// authorizes the controller action; stamp the action name as the ability
			// literal so the resolver decodes the exact grant.
			if level.method == "cancancan" {
				p.cancancanAbility = action
			}
			return p
		}
	}

	// Per-action Pundit / CanCanCan authorize inside the action body (medium —
	// the guard is body-local, not a declarative before_action). When the call
	// names an explicit action symbol (`authorize @post, :destroy?` /
	// `authorize! :destroy, @post`) capture it as the required permission.
	if actionBody, ok := caActionBody(body, action); ok {
		if caPunditAuthorizeRe.MatchString(actionBody) {
			perm := ""
			if m := caPunditActionRe.FindStringSubmatch(actionBody); m != nil {
				perm = m[1]
			}
			// #4751 — derive the Pundit policy class literal from the authorized
			// record (`authorize @post` → "Post"); the resolver reads pundit_policy
			// + pundit_action to decode the exact action grant.
			policy := ""
			if m := caPunditRecordRe.FindStringSubmatch(actionBody); m != nil {
				policy = caPunditPolicyClass(m[1])
			}
			return caActionPosture{
				required: true, guard: "authorize",
				method: "pundit", confidence: "medium",
				permission:   perm,
				punditPolicy: policy,
				punditAction: perm,
			}
		}
		if m := caCanCanActionRe.FindStringSubmatch(actionBody); m != nil {
			return caActionPosture{
				required: true, guard: "authorize!",
				method: "cancancan", confidence: "medium",
				permission:       m[1],
				cancancanAbility: m[1],
			}
		}
	}

	return caActionPosture{}
}

// caActionEntry is one action method declaration in a controller body.
type caActionEntry struct {
	name string
	off  int // byte offset within the controller body of the `def` line
}

// caCollectActions returns the controller's public action methods in source
// order. Private helpers below a `private` keyword are excluded (they are not
// routable actions).
func caCollectActions(body string) []caActionEntry {
	privAt := caPrivateOffset(body)
	var out []caActionEntry
	for _, m := range caActionDefRe.FindAllStringSubmatchIndex(body, -1) {
		if privAt >= 0 && m[0] >= privAt {
			break
		}
		out = append(out, caActionEntry{name: body[m[2]:m[3]], off: m[0]})
	}
	return out
}

// caPrivateOffset returns the byte offset of a top-level `private` keyword in a
// controller body, or -1 when absent.
func caPrivateOffset(body string) int {
	re := regexp.MustCompile(`(?m)^\s*private\s*$`)
	if loc := re.FindStringIndex(body); loc != nil {
		return loc[0]
	}
	return -1
}

// caActionBody returns the source span of the named action's method body (from
// the action's `def` to the next top-level `def` or the body end). The bool is
// false when the action is not found.
func caActionBody(body, action string) (string, bool) {
	defRe := regexp.MustCompile(`(?m)^\s*def\s+` + regexp.QuoteMeta(action) + `\b`)
	loc := defRe.FindStringIndex(body)
	if loc == nil {
		return "", false
	}
	rest := body[loc[1]:]
	if nxt := caActionDefRe.FindStringIndex(rest); nxt != nil {
		rest = rest[:nxt[0]]
	}
	return rest, true
}

// caClassBody returns the source of a controller class body starting at offset
// `from` (just after the class declaration line) to the matching class `end`.
// Heuristic: scan to the next top-level `class`/`module` declaration or EOF —
// sufficient for the one-controller-per-file Rails convention.
func caClassBody(src string, from int) string {
	rest := src[from:]
	if nxt := regexp.MustCompile(`(?m)^\s*class\s+[A-Z]`).FindStringIndex(rest); nxt != nil {
		return rest[:nxt[0]]
	}
	return rest
}

// caPunditPolicyClass maps an authorized-record symbol to its Pundit policy class
// literal: `post` / `@post` → "Post"; an already-capitalised constant is kept.
// Pundit infers the policy from the record's class, so we Camelize the bare name.
func caPunditPolicyClass(record string) string {
	record = strings.TrimPrefix(strings.TrimSpace(record), "@")
	if record == "" {
		return ""
	}
	// Camelize a snake_case / lower record name (`blog_post` → "BlogPost").
	var b strings.Builder
	upNext := true
	for _, r := range record {
		if r == '_' {
			upNext = true
			continue
		}
		if upNext && r >= 'a' && r <= 'z' {
			b.WriteRune(r - ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
		upNext = false
	}
	return b.String()
}

// caSourceSlice bounds a controller body for stamping as controller_source — the
// resolver only needs the auth-bearing region, so cap the slice to keep the graph
// payload small while still carrying every before_action / authorize call.
func caSourceSlice(body string) string {
	const maxSource = 4096
	body = strings.TrimSpace(body)
	if len(body) > maxSource {
		return body[:maxSource]
	}
	return body
}

// caControllerResource maps a controller class name to the routes.rb resource
// segment used in the controller#action handler key: `Admin::UsersController`
// → `admin/users`, `PostsController` → `posts`.
func caControllerResource(className string) string {
	className = strings.TrimSuffix(className, "Controller")
	parts := strings.Split(className, "::")
	for i, p := range parts {
		parts[i] = caUnderscore(p)
	}
	return strings.Join(parts, "/")
}

// caUnderscore converts a CamelCase identifier to snake_case
// (`UsersV2` → `users_v2`).
func caUnderscore(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// caParseActionList parses a Ruby symbol list ("`:index, :show`" or "`show`")
// into bare action names, sorted for determinism.
func caParseActionList(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		p = strings.TrimPrefix(p, ":")
		p = strings.Trim(p, "\"' \t")
		if p != "" && p != "only" && p != "except" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
