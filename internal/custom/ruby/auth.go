// auth.go — Ruby auth extractor (auth_coverage).
//
// Covers the Auth lane for Ruby http_backend frameworks by detecting:
//
//	Devise        — devise_for, before_action :authenticate_user!, current_user,
//	                devise :registerable, require_login
//	JWT           — JWT.encode / JWT.decode, jwt_token, jwt_header
//	Warden        — Warden::Manager / env['warden'] / warden.authenticate
//	CanCanCan     — authorize!, load_and_authorize_resource, can? / cannot?
//	Pundit        — authorize, policy_scope, Pundit::Policy
//	Doorkeeper    — doorkeeper_for :all, before_action :doorkeeper_authorize!
//	Rack::Auth    — Rack::Auth::Basic, Rack::Auth::Digest
//	OmniAuth      — OmniAuth::Builder / provider :github
//
// All cells are flipped to `partial` because detection is heuristic
// (require/pattern matching) and does not perform cross-file dataflow.
// This is consistent with the Java and Python observability extractors.
//
// Part of issue #3282.
package ruby

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("ruby_auth", &rubyAuthExtractor{})
}

// rubyAuthExtractor detects auth patterns across Ruby source files.
type rubyAuthExtractor struct{}

func (e *rubyAuthExtractor) Language() string { return "ruby_auth" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// --------------- Devise ---------------

	// devise_for :users / devise_for :admins, ...
	rbDeviseForRe = regexp.MustCompile(
		`(?m)\bdevise_for\s+:([a-z_]+)`)

	// before_action :authenticate_user! / :authenticate_admin!
	rbDeviseAuthenticateRe = regexp.MustCompile(
		`(?m)\bbefore_action\s+:authenticate_([a-z_]+)!`)

	// devise :registerable, :authenticatable, ...
	rbDeviseModulesRe = regexp.MustCompile(
		`(?m)\bdevise\s+:([a-z_]+(?:,\s*:[a-z_]+)*)`)

	// current_user / current_admin helper references
	rbDeviseCurrentUserRe = regexp.MustCompile(
		`(?m)\bcurrent_([a-z_]+)\b`)

	// require_login (Sorcery / Clearance compatibility)
	rbRequireLoginRe = regexp.MustCompile(
		`(?m)\b(?:require_login|login_required)\b`)

	// --------------- JWT ---------------

	// require 'jwt'
	rbJWTRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"]jwt['"]`)

	// JWT.encode(payload, secret, ...) / JWT.decode(token, secret, ...)
	rbJWTEncodeDecodeRe = regexp.MustCompile(
		`(?m)\bJWT\.(encode|decode)\s*\(`)

	// jwt_token / Authorization: Bearer pattern
	rbJWTTokenRe = regexp.MustCompile(
		`(?m)(?:jwt_token|bearer_token|Authorization.*Bearer|decode_jwt)\b`)

	// --------------- Warden ---------------

	// Warden::Manager.new / use Warden::Manager
	rbWardenManagerRe = regexp.MustCompile(
		`(?m)\bWarden::Manager\b`)

	// env['warden'] / env["warden"]
	rbWardenEnvRe = regexp.MustCompile(
		`(?m)\benv\[['"]warden['"]\]`)

	// warden.authenticate / warden.authenticate!
	rbWardenAuthRe = regexp.MustCompile(
		`(?m)\b(\w+)\.authenticate!?\s*\(\s*:`)

	// --------------- CanCanCan ---------------

	// authorize! :action, resource
	rbCanCanAuthorizeRe = regexp.MustCompile(
		`(?m)\bauthorize!\s+:[a-z_]+`)

	// load_and_authorize_resource / load_and_permit_resource
	rbCanCanLoadRe = regexp.MustCompile(
		`(?m)\b(?:load_and_authorize_resource|load_and_permit_resource|can\?|cannot\?)\b`)

	// ability.can :action, Model / can :manage, :all
	rbCanCanAbilityRe = regexp.MustCompile(
		`(?m)\b(?:can|cannot)\s+:[a-z_]+,\s+`)

	// --------------- Pundit ---------------

	// authorize @resource / authorize resource
	rbPunditAuthorizeRe = regexp.MustCompile(
		`(?m)\bauthorize\s+[@a-z_]`)

	// policy_scope(Model) / policy(resource)
	rbPunditPolicyScopeRe = regexp.MustCompile(
		`(?m)\b(?:policy_scope|policy)\s*\(`)

	// Pundit::Policy / include Pundit
	rbPunditClassRe = regexp.MustCompile(
		`(?m)\b(?:Pundit::Policy\b|include Pundit\b)`)

	// --------------- Doorkeeper ---------------

	// before_action :doorkeeper_authorize!
	rbDoorkeeperAuthorizeRe = regexp.MustCompile(
		`(?m)\bbefore_action\s+:doorkeeper_authorize!`)

	// doorkeeper_for :all / use_doorkeeper
	rbDoorkeeperForRe = regexp.MustCompile(
		`(?m)\b(?:doorkeeper_for\s+:|use_doorkeeper)\b`)

	// --------------- Rack::Auth / OmniAuth ---------------

	// Rack::Auth::Basic / Rack::Auth::Digest
	rbRackAuthRe = regexp.MustCompile(
		`(?m)\bRack::Auth::(Basic|Digest)\b`)

	// OmniAuth::Builder / OmniAuth.config / provider :github
	rbOmniAuthRe = regexp.MustCompile(
		`(?m)\b(?:OmniAuth(?:::Builder|\.config)|provider\s+:[a-z_]+)\b`)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rubyAuthExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.ruby_auth")
	_, span := tracer.Start(ctx, "custom.ruby_auth")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

	// Fast guard: skip files with no auth-relevant tokens.
	hasAuth := strings.Contains(src, "devise") || strings.Contains(src, "Devise") ||
		strings.Contains(src, "authenticate") || strings.Contains(src, "JWT") ||
		strings.Contains(src, "jwt") || strings.Contains(src, "Warden") ||
		strings.Contains(src, "warden") || strings.Contains(src, "authorize") ||
		strings.Contains(src, "CanCan") || strings.Contains(src, "Pundit") ||
		strings.Contains(src, "doorkeeper") || strings.Contains(src, "Doorkeeper") ||
		strings.Contains(src, "OmniAuth") || strings.Contains(src, "Rack::Auth") ||
		strings.Contains(src, "require_login") || strings.Contains(src, "login_required")
	if !hasAuth {
		return nil, nil
	}

	var out []types.EntityRecord

	out = append(out, extractDevise(src, file.Path)...)
	out = append(out, extractJWT(src, file.Path)...)
	out = append(out, extractWarden(src, file.Path)...)
	out = append(out, extractCanCanCan(src, file.Path)...)
	out = append(out, extractPundit(src, file.Path)...)
	out = append(out, extractDoorkeeper(src, file.Path)...)
	out = append(out, extractRackOmniAuth(src, file.Path)...)

	return out, nil
}

// ---------------------------------------------------------------------------
// Devise
// ---------------------------------------------------------------------------

func extractDevise(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// devise_for :model
	for _, idx := range rbDeviseForRe.FindAllStringSubmatchIndex(src, -1) {
		model := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		e := makeEntity("devise_for:"+model, string(types.EntityKindPattern), "auth_guard", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "devise", "kind", "route_registration", "model", model)
		out = append(out, e)
	}

	// before_action :authenticate_<model>!
	for _, idx := range rbDeviseAuthenticateRe.FindAllStringSubmatchIndex(src, -1) {
		model := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		e := makeEntity("authenticate_"+model+"!", string(types.EntityKindPattern), "auth_guard", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "devise", "kind", "before_action", "model", model)
		out = append(out, e)
	}

	// devise :registerable, :authenticatable ...
	for _, idx := range rbDeviseModulesRe.FindAllStringSubmatchIndex(src, -1) {
		modules := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		e := makeEntity("devise_modules", string(types.EntityKindPattern), "auth_config", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "devise", "kind", "modules", "modules_list", modules)
		out = append(out, e)
	}

	// require_login / login_required
	if rbRequireLoginRe.MatchString(src) {
		loc := rbRequireLoginRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		e := makeEntity("require_login", string(types.EntityKindPattern), "auth_guard", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "devise_compat", "kind", "before_action")
		out = append(out, e)
	}

	// current_user / current_admin usage (only emit when devise_for is present)
	if rbDeviseForRe.MatchString(src) {
		for _, idx := range rbDeviseCurrentUserRe.FindAllStringSubmatchIndex(src, -1) {
			model := src[idx[2]:idx[3]]
			// Skip very common non-user models that appear in many contexts
			if model == "user" || model == "admin" || model == "member" || model == "account" {
				ln := lineOf(src, idx[0])
				e := makeEntity("current_"+model, string(types.EntityKindPattern), "auth_helper", fp, "ruby", ln)
				setProps(&e, "signal", "auth", "library", "devise", "kind", "helper", "model", model)
				out = append(out, e)
			}
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// JWT
// ---------------------------------------------------------------------------

func extractJWT(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	if !rbJWTRequireRe.MatchString(src) && !rbJWTEncodeDecodeRe.MatchString(src) &&
		!rbJWTTokenRe.MatchString(src) {
		return nil
	}

	// JWT.encode / JWT.decode call sites
	for _, idx := range rbJWTEncodeDecodeRe.FindAllStringSubmatchIndex(src, -1) {
		op := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		e := makeEntity("JWT."+op, string(types.EntityKindPattern), "auth_token", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "jwt", "kind", "token_operation", "operation", op)
		out = append(out, e)
	}

	// jwt_token / Bearer pattern
	if rbJWTTokenRe.MatchString(src) && !rbJWTEncodeDecodeRe.MatchString(src) {
		loc := rbJWTTokenRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		e := makeEntity("jwt_token", string(types.EntityKindPattern), "auth_token", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "jwt", "kind", "token_usage")
		out = append(out, e)
	}

	// file-level signal when only require
	if rbJWTRequireRe.MatchString(src) && !rbJWTEncodeDecodeRe.MatchString(src) && !rbJWTTokenRe.MatchString(src) {
		loc := rbJWTRequireRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		e := makeEntity("jwt", string(types.EntityKindPattern), "auth_token", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "jwt", "kind", "require")
		out = append(out, e)
	}

	return out
}

// ---------------------------------------------------------------------------
// Warden
// ---------------------------------------------------------------------------

func extractWarden(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	if !rbWardenManagerRe.MatchString(src) && !rbWardenEnvRe.MatchString(src) &&
		!rbWardenAuthRe.MatchString(src) {
		return nil
	}

	// Warden::Manager
	if rbWardenManagerRe.MatchString(src) {
		loc := rbWardenManagerRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		e := makeEntity("Warden::Manager", string(types.EntityKindPattern), "auth_middleware", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "warden", "kind", "middleware_setup")
		out = append(out, e)
	}

	// env['warden'] usage
	if rbWardenEnvRe.MatchString(src) {
		loc := rbWardenEnvRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		e := makeEntity("env.warden", string(types.EntityKindPattern), "auth_helper", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "warden", "kind", "env_access")
		out = append(out, e)
	}

	// warden.authenticate! / warden.authenticate
	for _, idx := range rbWardenAuthRe.FindAllStringSubmatchIndex(src, -1) {
		receiver := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		e := makeEntity(receiver+".authenticate", string(types.EntityKindPattern), "auth_guard", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "warden", "kind", "authenticate_call", "receiver", receiver)
		out = append(out, e)
	}

	return out
}

// ---------------------------------------------------------------------------
// CanCanCan
// ---------------------------------------------------------------------------

func extractCanCanCan(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	if !rbCanCanAuthorizeRe.MatchString(src) && !rbCanCanLoadRe.MatchString(src) &&
		!rbCanCanAbilityRe.MatchString(src) {
		return nil
	}

	// authorize! :action
	for _, idx := range rbCanCanAuthorizeRe.FindAllStringSubmatchIndex(src, -1) {
		ln := lineOf(src, idx[0])
		e := makeEntity("authorize!", string(types.EntityKindPattern), "auth_guard", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "cancancan", "kind", "authorization_check")
		out = append(out, e)
	}

	// load_and_authorize_resource / can? / cannot?
	if rbCanCanLoadRe.MatchString(src) {
		loc := rbCanCanLoadRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		callSite := src[loc[0]:loc[1]]
		e := makeEntity(strings.TrimSpace(callSite), string(types.EntityKindPattern), "auth_guard", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "cancancan", "kind", "resource_authorization")
		out = append(out, e)
	}

	// can :manage, :all / cannot :destroy, Article
	for _, idx := range rbCanCanAbilityRe.FindAllStringSubmatchIndex(src, -1) {
		ln := lineOf(src, idx[0])
		callSite := strings.TrimSpace(src[idx[0]:idx[1]])
		if len(callSite) > 50 {
			callSite = callSite[:50]
		}
		e := makeEntity(callSite, string(types.EntityKindPattern), "auth_policy", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "cancancan", "kind", "ability_rule")
		out = append(out, e)
	}

	return out
}

// ---------------------------------------------------------------------------
// Pundit
// ---------------------------------------------------------------------------

func extractPundit(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	if !rbPunditAuthorizeRe.MatchString(src) && !rbPunditPolicyScopeRe.MatchString(src) &&
		!rbPunditClassRe.MatchString(src) {
		return nil
	}

	// authorize @resource
	for _, idx := range rbPunditAuthorizeRe.FindAllStringSubmatchIndex(src, -1) {
		ln := lineOf(src, idx[0])
		e := makeEntity("authorize", string(types.EntityKindPattern), "auth_guard", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "pundit", "kind", "authorize_call")
		out = append(out, e)
	}

	// policy_scope(...) / policy(...)
	for _, idx := range rbPunditPolicyScopeRe.FindAllStringSubmatchIndex(src, -1) {
		ln := lineOf(src, idx[0])
		callSite := strings.TrimSpace(src[idx[0]:idx[1]])
		e := makeEntity(callSite, string(types.EntityKindPattern), "auth_policy", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "pundit", "kind", "policy_scope")
		out = append(out, e)
	}

	// Pundit::Policy / include Pundit
	if rbPunditClassRe.MatchString(src) {
		loc := rbPunditClassRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		e := makeEntity("Pundit::Policy", string(types.EntityKindPattern), "auth_policy", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "pundit", "kind", "policy_class")
		out = append(out, e)
	}

	return out
}

// ---------------------------------------------------------------------------
// Doorkeeper
// ---------------------------------------------------------------------------

func extractDoorkeeper(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	if !rbDoorkeeperAuthorizeRe.MatchString(src) && !rbDoorkeeperForRe.MatchString(src) {
		return nil
	}

	// before_action :doorkeeper_authorize!
	if rbDoorkeeperAuthorizeRe.MatchString(src) {
		loc := rbDoorkeeperAuthorizeRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		e := makeEntity("doorkeeper_authorize!", string(types.EntityKindPattern), "auth_guard", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "doorkeeper", "kind", "before_action")
		out = append(out, e)
	}

	// doorkeeper_for :all / use_doorkeeper
	if rbDoorkeeperForRe.MatchString(src) {
		loc := rbDoorkeeperForRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		e := makeEntity("doorkeeper", string(types.EntityKindPattern), "auth_config", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "doorkeeper", "kind", "oauth_setup")
		out = append(out, e)
	}

	return out
}

// ---------------------------------------------------------------------------
// Rack::Auth + OmniAuth
// ---------------------------------------------------------------------------

func extractRackOmniAuth(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// Rack::Auth::Basic / Rack::Auth::Digest
	for _, idx := range rbRackAuthRe.FindAllStringSubmatchIndex(src, -1) {
		scheme := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		e := makeEntity("Rack::Auth::"+scheme, string(types.EntityKindPattern), "auth_middleware", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "rack_auth", "kind", "middleware", "scheme", scheme)
		out = append(out, e)
	}

	// OmniAuth::Builder / provider :provider_name
	for _, idx := range rbOmniAuthRe.FindAllStringSubmatchIndex(src, -1) {
		ln := lineOf(src, idx[0])
		callSite := strings.TrimSpace(src[idx[0]:idx[1]])
		if len(callSite) > 50 {
			callSite = callSite[:50]
		}
		e := makeEntity(callSite, string(types.EntityKindPattern), "auth_config", fp, "ruby", ln)
		setProps(&e, "signal", "auth", "library", "omniauth", "kind", "provider_registration")
		out = append(out, e)
	}

	return out
}
