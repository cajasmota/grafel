// auth.go — Lua auth extractor (auth_coverage).
//
// Covers the Auth lane for Lua web frameworks by detecting:
//
//	OpenResty:
//	  - ngx.req.get_headers() for Authorization header inspection
//	  - jwt decoding patterns via lua-resty-jwt / resty.jwt
//	  - ngx.var.cookie_session / ngx.req.get_headers()["Authorization"]
//	  - access_by_lua_block / access_by_lua_file directives (auth gates)
//	  - Kong plugin handler patterns: plugin.access() with credential checks
//
//	Lapis:
//	  - before_filter / before_action patterns
//	  - lapis.util.check_params / lapis.db.Users:find for user lookup
//	  - require("lapis.session") / session.current_user
//	  - bcrypt / crypto password verification (resty.sha256)
//
// All cells are partial: heuristic pattern matching without data-flow.
package lua

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("lua_auth", &luaAuthExtractor{})
}

// luaAuthExtractor detects auth patterns in Lua source files.
type luaAuthExtractor struct{}

func (e *luaAuthExtractor) Language() string { return "lua_auth" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// JWT: require("resty.jwt") / local jwt = require "resty.jwt"
	reLuaJWTRequire = regexp.MustCompile(
		`(?m)\brequire\s*[("']resty\.jwt["')]?\)?`)

	// jwt:verify / jwt:decode / jwt:load_jwt
	reLuaJWTVerify = regexp.MustCompile(
		`(?m)\bjwt\s*:\s*(verify|decode|load_jwt)\s*\(`)

	// Authorization header: ngx.req.get_headers()["Authorization"]
	reLuaAuthHeader = regexp.MustCompile(
		`(?m)\bngx\.req\.get_headers\s*\(\s*\)\s*\[["']Authorization["']\]`)

	// Cookie/session: ngx.var.cookie_ or ngx.req.get_headers()["Cookie"]
	reLuaCookieAuth = regexp.MustCompile(
		`(?m)\bngx\.var\.cookie_(\w+)|\bngx\.req\.get_headers\s*\(\s*\)\s*\[["']Cookie["']\]`)

	// access_by_lua_block / access_by_lua_file
	reLuaAccessByLua = regexp.MustCompile(
		`(?m)\baccess_by_lua(?:_block|_file)\b`)

	// Lapis session: require("lapis.session") / session.current_user
	reLapisSession = regexp.MustCompile(
		`(?m)\brequire\s*[("']lapis\.session["']?\)?|\bsession\.current_user\b`)

	// Lapis before_filter authentication
	reLapisBeforeFilter = regexp.MustCompile(
		`(?m)\bbefore_filter\s*\(?\s*function\b|\bbefore_filter\s*,?\s*function\b`)

	// Lapis user lookup: Users:find / Models.users:find
	reLapisUserFind = regexp.MustCompile(
		`(?m)\bUsers?\s*:\s*find\s*\(|\bModels\s*\.\s*[Uu]sers?\s*:\s*find\s*\(`)

	// bcrypt / crypto: require("bcrypt") / resty.sha256 / resty.hmac
	reLuaCrypto = regexp.MustCompile(
		`(?m)\brequire\s*[("'](?:bcrypt|resty\.sha[0-9]+|resty\.hmac|resty\.md5)["']?\)?`)

	// Kong plugin access handler: function plugin:access(conf)
	reLuaKongAccess = regexp.MustCompile(
		`(?m)function\s+\w+\s*:\s*access\s*\(\s*\w*\s*\)`)

	// OIDC: require("resty.openidc") / local openidc = require "resty.openidc"
	reLuaOIDCRequire = regexp.MustCompile(
		`(?m)\brequire\s*[("']resty\.openidc["')]?\)?`)

	// openidc.authenticate(opts) — the lua-resty-openidc auth entry point.
	reLuaOIDCAuthenticate = regexp.MustCompile(
		`(?m)\bopenidc\s*\.\s*authenticate\s*\(`)

	// Lapis @require_login class annotation / decorator.
	reLapisRequireLogin = regexp.MustCompile(
		`(?m)@require_login\b|\brequire_login\b`)
)

// Extract implements extractor.Extractor.
func (e *luaAuthExtractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

	// Fast guard
	hasAuth := strings.Contains(src, "jwt") || strings.Contains(src, "JWT") ||
		strings.Contains(src, "Authorization") || strings.Contains(src, "authenticate") ||
		strings.Contains(src, "session") || strings.Contains(src, "cookie") ||
		strings.Contains(src, "access_by_lua") || strings.Contains(src, "before_filter") ||
		strings.Contains(src, "bcrypt") || strings.Contains(src, "resty.hmac") ||
		strings.Contains(src, ":access(") || strings.Contains(src, "Users:find") ||
		strings.Contains(src, "openidc") || strings.Contains(src, "require_login")
	if !hasAuth {
		return nil, nil
	}

	var out []types.EntityRecord

	// JWT auth
	if reLuaJWTRequire.MatchString(src) {
		idx := reLuaJWTRequire.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("lua_jwt_import", string(types.EntityKindPattern), "auth_config", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "library", "resty.jwt", "kind", "jwt_import", "auth_method", "jwt")
		out = append(out, entity)
	}
	for _, idx := range reLuaJWTVerify.FindAllStringSubmatchIndex(src, -1) {
		op := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("jwt_"+op, string(types.EntityKindPattern), "auth_guard", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "library", "resty.jwt", "kind", "jwt_"+op, "auth_method", "jwt")
		out = append(out, entity)
	}

	// OIDC auth (lua-resty-openidc)
	if reLuaOIDCRequire.MatchString(src) {
		idx := reLuaOIDCRequire.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("lua_oidc_import", string(types.EntityKindPattern), "auth_config", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "library", "resty.openidc", "kind", "oidc_import", "auth_method", "oidc")
		out = append(out, entity)
	}
	for _, idx := range reLuaOIDCAuthenticate.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("oidc_authenticate", string(types.EntityKindPattern), "auth_guard", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "library", "resty.openidc", "kind", "oidc_authenticate", "auth_method", "oidc")
		out = append(out, entity)
	}

	// Authorization header
	for _, idx := range reLuaAuthHeader.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("auth_header_check", string(types.EntityKindPattern), "auth_guard", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "framework", "openresty", "kind", "header_check")
		out = append(out, entity)
	}

	// Cookie/session auth
	for _, idx := range reLuaCookieAuth.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("cookie_auth", string(types.EntityKindPattern), "auth_guard", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "framework", "openresty", "kind", "cookie_check", "auth_method", "session")
		out = append(out, entity)
	}

	// access_by_lua gate
	for _, idx := range reLuaAccessByLua.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("access_by_lua_gate", string(types.EntityKindPattern), "auth_guard", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "framework", "openresty", "kind", "access_gate")
		out = append(out, entity)
	}

	// Lapis session
	if reLapisSession.MatchString(src) {
		idx := reLapisSession.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_session_auth", string(types.EntityKindPattern), "auth_guard", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "framework", "lapis", "kind", "session_auth", "auth_method", "session")
		out = append(out, entity)
	}

	// Lapis before_filter
	for _, idx := range reLapisBeforeFilter.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_before_filter", string(types.EntityKindPattern), "auth_guard", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "framework", "lapis", "kind", "before_filter", "auth_method", "session")
		out = append(out, entity)
	}

	// Lapis @require_login annotation / require_login mixin
	for _, idx := range reLapisRequireLogin.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_require_login", string(types.EntityKindPattern), "auth_guard", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "framework", "lapis", "kind", "require_login", "auth_method", "session")
		out = append(out, entity)
	}

	// Lapis user lookup
	for _, idx := range reLapisUserFind.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("lapis_user_find", string(types.EntityKindPattern), "auth_helper", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "framework", "lapis", "kind", "user_lookup")
		out = append(out, entity)
	}

	// Crypto primitives
	if reLuaCrypto.MatchString(src) {
		idx := reLuaCrypto.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("lua_crypto_import", string(types.EntityKindPattern), "auth_helper", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "library", "crypto", "kind", "password_hashing")
		out = append(out, entity)
	}

	// Kong access handler
	for _, idx := range reLuaKongAccess.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("kong_access_handler", string(types.EntityKindPattern), "auth_guard", file.Path, "lua", ln)
		setProps(&entity, "signal", "auth", "framework", "kong", "kind", "access_handler")
		out = append(out, entity)
	}

	return out, nil
}
