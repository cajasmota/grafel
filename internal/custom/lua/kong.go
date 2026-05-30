// kong.go — Lua API-gateway plugin extractor for Kong and Apache APISIX.
//
// Covers middleware_coverage, auth_coverage, route_extraction for the two
// dominant OpenResty-based API gateways. Both share a plugin-lifecycle model:
// a plugin declares an ordered set of request-lifecycle phase handlers, a
// numeric priority that determines its position in the global plugin chain,
// a version, a name, and a JSON-schema-style config table.
//
//	Kong (kong/plugins/<name>/):
//	  - handler.lua: `local MyPlugin = {}` returning a table with
//	    `MyPlugin.PRIORITY = 1000`, `MyPlugin.VERSION = "1.0"`, and lifecycle
//	    methods `function MyPlugin:access(conf)`, `:header_filter`,
//	    `:body_filter`, `:log`, `:init_worker`, `:certificate`, `:rewrite`,
//	    `:preread`, `:configure`, `:ws_handshake`.
//	  - schema.lua: `return { name = "x", fields = { { config = { … } } } }`
//	    → config-schema entity with the declared field names.
//	  - api.lua: Admin API route definitions (`["/foo"] = { … }`).
//	  - daos.lua: custom DAO entity declarations.
//
//	Apache APISIX (apisix/plugins/<name>.lua):
//	  - plugin module: `local _M = { version = …, priority = …, name = "x",
//	    schema = schema }` + phase functions `function _M.rewrite(conf, ctx)`,
//	    `_M.access`, `_M.header_filter`, `_M.body_filter`, `_M.log`,
//	    `_M.before_proxy`, `_M.delayed_body_filter`.
//	  - `local schema = { type = "object", properties = { … } }` → config schema.
//
// All cells are partial: regex-based detection without full AST parsing, but
// they capture the plugin name, ordered phase set, priority, version, and the
// config-schema field names so the plugin chain is reconstructable.
package lua

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("lua_kong", &luaKongExtractor{})
}

// luaKongExtractor detects Kong and APISIX API-gateway plugins.
type luaKongExtractor struct{}

func (e *luaKongExtractor) Language() string { return "lua_kong" }

// kongPhaseRank gives the canonical Kong/OpenResty request-lifecycle order of a
// plugin phase, so the emitted plugin's phase set carries a stable, comparable
// `phase_order` independent of textual position in the handler. Phases not in
// the table sort after the known ones. The ranking mirrors luaPhaseRank but is
// keyed on the bare Kong/APISIX phase-method names (no `_by_lua` suffix).
func kongPhaseRank(phase string) int {
	switch phase {
	case "init_worker":
		return 1
	case "configure":
		return 2
	case "certificate":
		return 3
	case "rewrite":
		return 4
	case "preread":
		return 5
	case "access":
		return 6
	case "before_proxy":
		return 7
	case "header_filter":
		return 8
	case "body_filter", "delayed_body_filter":
		return 9
	case "log":
		return 10
	case "ws_handshake":
		return 11
	default:
		return 99
	}
}

// kongLooksLikeAuth reports whether a plugin behaves as an authentication /
// access-control plugin, inferred from its name. Kong and APISIX auth plugins
// (key-auth, jwt, basic-auth, oauth2, ldap-auth, acl, openid-connect, …) gate
// the request in the `access` phase; callers combine this name heuristic with
// the phase to decide whether to stamp the `auth` signal.
func kongLooksLikeAuth(name string) bool {
	n := strings.ToLower(name)
	for _, kw := range []string{"auth", "jwt", "oauth", "key", "acl", "ldap", "openid", "hmac", "rbac"} {
		if strings.Contains(n, kw) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// Kong handler phase methods: function MyPlugin:access(conf)
	// Captures (1) the plugin table name and (2) the phase.
	reKongPhase = regexp.MustCompile(
		`(?m)function\s+([A-Za-z_]\w*)\s*:\s*(init_worker|configure|certificate|` +
			`rewrite|preread|access|header_filter|body_filter|log|ws_handshake|ws_close)\s*\(`)

	// Kong priority: MyPlugin.PRIORITY = 1000  (also handles `local PRIORITY`)
	reKongPriority = regexp.MustCompile(
		`(?m)(?:([A-Za-z_]\w*)\s*\.\s*)?PRIORITY\s*=\s*(-?\d+)`)

	// Kong version: MyPlugin.VERSION = "1.0.0"
	reKongVersion = regexp.MustCompile(
		`(?m)(?:[A-Za-z_]\w*\s*\.\s*)?VERSION\s*=\s*["']([^"']+)["']`)

	// schema.lua name field: name = "my-plugin"  (top-level plugin name)
	reKongSchemaName = regexp.MustCompile(
		`(?m)^\s*name\s*=\s*["']([A-Za-z0-9_\-]+)["']`)

	// Kong schema field declarations inside fields = { … }:
	//   { foo = { type = "string" } }  →  captures `foo`
	reKongSchemaField = regexp.MustCompile(
		`(?m)\{\s*([a-z_][a-z0-9_]*)\s*=\s*\{\s*type\s*=`)

	// Kong fields = { … } table marker.
	reKongFields = regexp.MustCompile(`(?m)\bfields\s*=\s*\{`)

	// Kong Admin API routes in api.lua: ["/plugins/foo"] = {
	reKongAdminAPI = regexp.MustCompile(
		`(?m)\[\s*["'](/[^"']*)["']\s*\]\s*=\s*\{`)

	// Kong custom DAO entity in daos.lua: name = "my_entities" within a daos table,
	// detected via the primary_key declaration which is DAO-specific.
	reKongDaoPrimaryKey = regexp.MustCompile(
		`(?m)\bprimary_key\s*=\s*\{`)

	// APISIX plugin module fields: _M.access / _M.rewrite defined as functions.
	//   function _M.access(conf, ctx)  →  captures the phase.
	reApisixPhase = regexp.MustCompile(
		`(?m)function\s+_M\s*\.\s*(rewrite|access|header_filter|body_filter|log|` +
			`before_proxy|delayed_body_filter|init_worker|destroy)\s*\(`)

	// APISIX module-table fields: name = "x", priority = 2000, version = 0.1
	reApisixName = regexp.MustCompile(
		`(?m)\bname\s*=\s*["']([A-Za-z0-9_\-]+)["']`)
	reApisixPriority = regexp.MustCompile(
		`(?m)\bpriority\s*=\s*(-?\d+)`)
	reApisixVersion = regexp.MustCompile(
		`(?m)\bversion\s*=\s*([0-9][0-9.]*|["'][^"']+["'])`)

	// APISIX schema properties: properties = { foo = { … }, bar = { … } }
	reApisixSchemaProps = regexp.MustCompile(`(?m)\bproperties\s*=\s*\{`)
	reApisixSchemaField = regexp.MustCompile(
		`(?m)\{?\s*([a-z_][a-z0-9_]*)\s*=\s*\{\s*type\s*=`)
)

// kongPathKind classifies a file path as a Kong or APISIX plugin location and
// flags the conventional Kong companion files (schema/api/daos).
func kongPathKind(path string) (isKong, isApisix, isSchema, isAPI, isDao bool) {
	p := strings.ToLower(path)
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	isKong = strings.Contains(p, "kong/plugins/") || strings.Contains(p, "kong/plugins\\")
	isApisix = strings.Contains(p, "apisix/plugins/") || strings.Contains(p, "apisix/plugins\\")
	isSchema = base == "schema.lua"
	isAPI = base == "api.lua"
	isDao = base == "daos.lua"
	return
}

// Extract implements extractor.Extractor.
func (e *luaKongExtractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	ext := strings.ToLower(file.Path)
	if !strings.HasSuffix(ext, ".lua") {
		return nil, nil
	}
	src := string(file.Content)

	isKongPath, isApisixPath, isSchemaFile, isAPIFile, isDaoFile := kongPathKind(file.Path)

	// Content signals so we still fire when paths don't follow the convention
	// (e.g. a vendored single-file plugin) but the code is unmistakably a
	// gateway plugin.
	hasKongSignal := isKongPath || strings.Contains(src, ".PRIORITY") ||
		(strings.Contains(src, "function") && reKongPhase.MatchString(src) &&
			(strings.Contains(src, "PRIORITY") || strings.Contains(src, "VERSION")))
	hasApisixSignal := isApisixPath ||
		(strings.Contains(src, "local _M") && reApisixPhase.MatchString(src))

	if !hasKongSignal && !hasApisixSignal && !isSchemaFile && !isAPIFile && !isDaoFile {
		return nil, nil
	}

	var out []types.EntityRecord

	if hasApisixSignal {
		out = append(out, e.extractApisix(src, file.Path)...)
	} else if hasKongSignal || isSchemaFile || isAPIFile || isDaoFile {
		out = append(out, e.extractKong(src, file.Path, isSchemaFile, isAPIFile, isDaoFile)...)
	}

	return out, nil
}

// ---------------------------------------------------------------------------
// Kong
// ---------------------------------------------------------------------------

func (e *luaKongExtractor) extractKong(src, path string, isSchemaFile, isAPIFile, isDaoFile bool) []types.EntityRecord {
	var out []types.EntityRecord

	// --- Plugin name + priority + version (handler.lua) ---
	pluginName := kongPluginNameFromPath(path)
	priority := ""
	if m := reKongPriority.FindStringSubmatch(src); m != nil {
		priority = m[2]
	}
	version := ""
	if m := reKongVersion.FindStringSubmatch(src); m != nil {
		version = m[1]
	}

	// --- Phase methods (ordered chain) ---
	phaseSet := map[string]bool{}
	type phaseHit struct {
		phase string
		line  int
		owner string
	}
	var phaseHits []phaseHit
	for _, idx := range reKongPhase.FindAllStringSubmatchIndex(src, -1) {
		owner := src[idx[2]:idx[3]]
		phase := src[idx[4]:idx[5]]
		phaseSet[phase] = true
		phaseHits = append(phaseHits, phaseHit{phase: phase, line: lineOf(src, idx[0]), owner: owner})
	}

	// Emit the plugin entity itself (once) if this file carries handler signals.
	if len(phaseHits) > 0 || priority != "" || version != "" {
		owner := pluginName
		if owner == "" && len(phaseHits) > 0 {
			owner = phaseHits[0].owner
		}
		if owner == "" {
			owner = "kong_plugin"
		}
		ln := 1
		if len(phaseHits) > 0 {
			ln = phaseHits[0].line
		}
		plugin := makeEntity("kong_plugin:"+owner, string(types.EntityKindPattern), "gateway_plugin", path, "lua", ln)
		phases := sortedPhases(phaseSet)
		setProps(&plugin,
			"signal", "middleware",
			"framework", "kong",
			"kind", "plugin",
			"plugin_name", owner,
			"phases", strings.Join(phases, ","),
		)
		if priority != "" {
			setProps(&plugin, "priority", priority)
		}
		if version != "" {
			setProps(&plugin, "version", version)
		}
		if kongLooksLikeAuth(owner) {
			setProps(&plugin, "signal", "auth", "auth_plugin", "true")
		}
		out = append(out, plugin)
	}

	// Emit one middleware_hook entity per phase, carrying priority + phase_order
	// so the global plugin chain (ordered by priority, then phase) is
	// reconstructable.
	for chainIdx, h := range phaseHits {
		entity := makeEntity("kong_phase:"+h.owner+":"+h.phase, string(types.EntityKindPattern), "middleware_hook", path, "lua", h.line)
		setProps(&entity,
			"signal", "middleware",
			"framework", "kong",
			"kind", "plugin_phase",
			"plugin_name", h.owner,
			"phase", h.phase,
			"phase_order", strconv.Itoa(kongPhaseRank(h.phase)),
			"chain_index", strconv.Itoa(chainIdx),
		)
		if priority != "" {
			setProps(&entity, "priority", priority)
		}
		if (h.phase == "access" || h.phase == "certificate") && kongLooksLikeAuth(h.owner) {
			setProps(&entity, "signal", "auth")
		}
		out = append(out, entity)
	}

	// --- schema.lua: config-schema fields ---
	if isSchemaFile || reKongFields.MatchString(src) {
		schemaName := pluginName
		if m := reKongSchemaName.FindStringSubmatch(src); m != nil {
			schemaName = m[1]
		}
		if schemaName == "" {
			schemaName = "kong_plugin"
		}
		var fields []string
		for _, m := range reKongSchemaField.FindAllStringSubmatch(src, -1) {
			// `config` is Kong's structural record-wrapper key, not a
			// user-facing config field; skip it so field_count reflects the
			// actual tunable options.
			if m[1] == "config" {
				continue
			}
			fields = append(fields, m[1])
		}
		schemaLine := 1
		if m := reKongFields.FindStringIndex(src); m != nil {
			schemaLine = lineOf(src, m[0])
		}
		entity := makeEntity("kong_schema:"+schemaName, string(types.EntityKindSchema), "config_schema", path, "lua", schemaLine)
		setProps(&entity,
			"signal", "config",
			"framework", "kong",
			"kind", "plugin_config_schema",
			"plugin_name", schemaName,
			"field_count", strconv.Itoa(len(fields)),
		)
		if len(fields) > 0 {
			setProps(&entity, "fields", strings.Join(fields, ","))
		}
		out = append(out, entity)
	}

	// --- api.lua: Admin API routes ---
	if isAPIFile {
		for _, idx := range reKongAdminAPI.FindAllStringSubmatchIndex(src, -1) {
			p := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			entity := makeEntity("kong_admin_route:"+p, string(types.EntityKindRoute), "http_route", path, "lua", ln)
			setProps(&entity,
				"signal", "routing",
				"framework", "kong",
				"kind", "admin_api_route",
				"path", p,
				"canonical_path", luaCanonicalPath(p),
			)
			out = append(out, entity)
		}
	}

	// --- daos.lua: custom DAO entities ---
	if isDaoFile || (reKongDaoPrimaryKey.MatchString(src) && strings.Contains(strings.ToLower(path), "dao")) {
		for _, idx := range reKongDaoPrimaryKey.FindAllStringIndex(src, -1) {
			ln := lineOf(src, idx[0])
			daoName := pluginName
			// look back for the nearest `name = "..."` before primary_key
			if m := reKongSchemaName.FindStringSubmatch(src[:idx[0]]); m != nil {
				daoName = m[1]
			}
			if daoName == "" {
				daoName = "kong_dao"
			}
			entity := makeEntity("kong_dao:"+daoName, string(types.EntityKindModel), "gateway_entity", path, "lua", ln)
			setProps(&entity,
				"signal", "model",
				"framework", "kong",
				"kind", "custom_dao",
				"entity_name", daoName,
			)
			out = append(out, entity)
		}
	}

	return out
}

// kongPluginNameFromPath derives the plugin name from a Kong plugin path such
// as `kong/plugins/my-auth/handler.lua` → `my-auth`. Returns "" when the path
// does not follow the convention.
func kongPluginNameFromPath(path string) string {
	p := strings.ReplaceAll(path, "\\", "/")
	const marker = "kong/plugins/"
	i := strings.Index(strings.ToLower(p), marker)
	if i < 0 {
		return ""
	}
	rest := p[i+len(marker):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[:j]
	}
	return strings.TrimSuffix(rest, ".lua")
}

// ---------------------------------------------------------------------------
// APISIX
// ---------------------------------------------------------------------------

func (e *luaKongExtractor) extractApisix(src, path string) []types.EntityRecord {
	var out []types.EntityRecord

	pluginName := apisixPluginNameFromPath(path)
	if m := reApisixName.FindStringSubmatch(src); m != nil {
		pluginName = m[1]
	}
	if pluginName == "" {
		pluginName = "apisix_plugin"
	}
	priority := ""
	if m := reApisixPriority.FindStringSubmatch(src); m != nil {
		priority = m[1]
	}
	version := ""
	if m := reApisixVersion.FindStringSubmatch(src); m != nil {
		version = strings.Trim(m[1], `"'`)
	}

	// --- Phase functions (ordered chain) ---
	phaseSet := map[string]bool{}
	type phaseHit struct {
		phase string
		line  int
	}
	var phaseHits []phaseHit
	for _, idx := range reApisixPhase.FindAllStringSubmatchIndex(src, -1) {
		phase := src[idx[2]:idx[3]]
		if phase == "destroy" {
			continue
		}
		phaseSet[phase] = true
		phaseHits = append(phaseHits, phaseHit{phase: phase, line: lineOf(src, idx[0])})
	}

	if len(phaseHits) > 0 || priority != "" {
		ln := 1
		if len(phaseHits) > 0 {
			ln = phaseHits[0].line
		}
		plugin := makeEntity("apisix_plugin:"+pluginName, string(types.EntityKindPattern), "gateway_plugin", path, "lua", ln)
		setProps(&plugin,
			"signal", "middleware",
			"framework", "apisix",
			"kind", "plugin",
			"plugin_name", pluginName,
			"phases", strings.Join(sortedPhases(phaseSet), ","),
		)
		if priority != "" {
			setProps(&plugin, "priority", priority)
		}
		if version != "" {
			setProps(&plugin, "version", version)
		}
		if kongLooksLikeAuth(pluginName) {
			setProps(&plugin, "signal", "auth", "auth_plugin", "true")
		}
		out = append(out, plugin)
	}

	for chainIdx, h := range phaseHits {
		entity := makeEntity("apisix_phase:"+pluginName+":"+h.phase, string(types.EntityKindPattern), "middleware_hook", path, "lua", h.line)
		setProps(&entity,
			"signal", "middleware",
			"framework", "apisix",
			"kind", "plugin_phase",
			"plugin_name", pluginName,
			"phase", h.phase,
			"phase_order", strconv.Itoa(kongPhaseRank(h.phase)),
			"chain_index", strconv.Itoa(chainIdx),
		)
		if priority != "" {
			setProps(&entity, "priority", priority)
		}
		if h.phase == "access" && kongLooksLikeAuth(pluginName) {
			setProps(&entity, "signal", "auth")
		}
		out = append(out, entity)
	}

	// --- schema: properties = { … } ---
	if reApisixSchemaProps.MatchString(src) {
		propsIdx := reApisixSchemaProps.FindStringIndex(src)
		// Scan field names only within the properties block (bounded window) to
		// avoid pulling in unrelated `x = { type = … }` tables elsewhere.
		window := src[propsIdx[1]:]
		if len(window) > 2000 {
			window = window[:2000]
		}
		var fields []string
		seen := map[string]bool{}
		for _, m := range reApisixSchemaField.FindAllStringSubmatch(window, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				fields = append(fields, m[1])
			}
		}
		entity := makeEntity("apisix_schema:"+pluginName, string(types.EntityKindSchema), "config_schema", path, "lua", lineOf(src, propsIdx[0]))
		setProps(&entity,
			"signal", "config",
			"framework", "apisix",
			"kind", "plugin_config_schema",
			"plugin_name", pluginName,
			"field_count", strconv.Itoa(len(fields)),
		)
		if len(fields) > 0 {
			setProps(&entity, "fields", strings.Join(fields, ","))
		}
		out = append(out, entity)
	}

	return out
}

// apisixPluginNameFromPath derives the plugin name from an APISIX plugin path
// such as `apisix/plugins/key-auth.lua` → `key-auth`.
func apisixPluginNameFromPath(path string) string {
	p := strings.ReplaceAll(path, "\\", "/")
	const marker = "apisix/plugins/"
	i := strings.Index(strings.ToLower(p), marker)
	if i < 0 {
		return ""
	}
	rest := p[i+len(marker):]
	if j := strings.Index(rest, "/"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSuffix(rest, ".lua")
}

// sortedPhases returns the phase set ordered by canonical lifecycle rank so the
// emitted `phases` list is deterministic and chain-comparable.
func sortedPhases(set map[string]bool) []string {
	var phases []string
	for p := range set {
		phases = append(phases, p)
	}
	// insertion sort by (rank, name) — small N, avoids a sort import churn.
	for i := 1; i < len(phases); i++ {
		for j := i; j > 0; j-- {
			a, b := phases[j-1], phases[j]
			if kongPhaseRank(a) > kongPhaseRank(b) ||
				(kongPhaseRank(a) == kongPhaseRank(b) && a > b) {
				phases[j-1], phases[j] = b, a
			} else {
				break
			}
		}
	}
	return phases
}
