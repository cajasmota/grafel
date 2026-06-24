// rescript.go — ReScript `rescript.json` / `bsconfig.json` manifest parser
// (#5378, epic #5360 Group A).
//
// ReScript (the typed language that compiles to JavaScript, formerly BuckleScript/
// ReasonML) configures every project with a single JSON manifest:
//
//	rescript.json   — the modern name (ReScript v11+).
//	bsconfig.json   — the legacy name (BuckleScript / ReScript < v11). Same schema.
//
// Shape (the dependency-relevant subset):
//
//	{
//	  "name": "my-app",
//	  "sources": [{ "dir": "src", "subdirs": true }],
//	  "package-specs": [{ "module": "es6", "in-source": true }],
//	  "suffix": ".bs.js",
//	  "bs-dependencies":     ["@rescript/react", "rescript-webapi"],
//	  "bs-dev-dependencies": ["@rescript/tools"],
//	  "pinned-dependencies": ["my-local-lib"],
//	  "jsx": { "version": 4, "mode": "classic" }
//	}
//
// Unlike most manifests the dependency lists are FLAT ARRAYS OF PACKAGE NAMES
// (no versions) — ReScript resolves the actual versions from the sibling
// package.json / node_modules, because ReScript packages ARE npm packages. So
// the package manager is npm: a `bs-dependencies` entry names an npm package
// that the JS-ecosystem package.json/lockfile coverage already version-resolves.
// rescript.json is NOT a lockfile (no resolved versions), so lockfile_parsing is
// not_applicable — the npm/yarn/pnpm lockfile parsers cover the resolved tree.
//
//	bs-dependencies     → runtime deps
//	bs-dev-dependencies → dev deps (is_dev=true)
//	pinned-dependencies → runtime deps, additionally flagged pinned (these are
//	                      always rebuilt even when unchanged; a subset of the
//	                      bs-dependencies usually, recorded so they are not lost)
//
// The JSX version/mode and the package-spec module/suffix are surfaced on the
// project anchor as a `rescript_config` property so the JSX/module configuration
// is queryable without a new entity kind, mirroring the Zig build_targets and
// cmake target treatment.
package manifest

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// parseReScriptJSON parses a rescript.json / bsconfig.json manifest and returns
// its declared dependencies. bs-dependencies are runtime; bs-dev-dependencies are
// dev (is_dev=true); pinned-dependencies are runtime and flagged pinned. The
// dependency lists are flat name arrays (no versions) — versions resolve from the
// sibling package.json (these names ARE npm packages). First declaration wins on
// duplicates (runtime > dev), so a name shared between bs-dependencies and
// pinned-dependencies keeps its runtime classification but gains the pinned flag.
func parseReScriptJSON(source string) []dep {
	var data struct {
		BsDependencies    []string `json:"bs-dependencies"`
		BsDevDependencies []string `json:"bs-dev-dependencies"`
		PinnedDeps        []string `json:"pinned-dependencies"`
	}
	if err := json.Unmarshal([]byte(source), &data); err != nil {
		return nil
	}

	var out []dep
	seen := map[string]bool{}
	pinned := map[string]bool{}
	for _, name := range data.PinnedDeps {
		if name != "" {
			pinned[name] = true
		}
	}

	emit := func(name string, isDev bool) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		kind := "runtime"
		if isDev {
			kind = "dev"
		}
		out = append(out, dep{name: name, isDev: isDev, kind: kind})
	}

	// Runtime first so a name shared with a dev dep keeps its runtime kind
	// (first-declaration-wins on the `seen` guard).
	for _, name := range data.BsDependencies {
		emit(name, false)
	}
	for _, name := range data.PinnedDeps {
		emit(name, false)
	}
	for _, name := range data.BsDevDependencies {
		emit(name, true)
	}
	return out
}

// reScriptConfigProperty returns a compact, deterministic summary of the JSX /
// module configuration declared in a rescript.json / bsconfig.json, or "" when
// none of the recognised config keys are present. It is attached to the manifest
// project anchor as the "rescript_config" property so the JSX/module config is
// queryable without introducing a new entity kind. Empty for every other
// manifest.
func reScriptConfigProperty(source string) string {
	var data struct {
		Suffix       string          `json:"suffix"`
		Module       json.RawMessage `json:"module"`
		Namespace    json.RawMessage `json:"namespace"`
		JSX          json.RawMessage `json:"jsx"`
		PackageSpecs json.RawMessage `json:"package-specs"`
		ReactJsx     json.RawMessage `json:"reason"` // legacy: { "react-jsx": 3 }
	}
	if err := json.Unmarshal([]byte(source), &data); err != nil {
		return ""
	}

	parts := map[string]string{}

	// jsx: object { "version": 4, "mode": "classic" } (v11) or a bare number
	// in very old configs.
	if len(data.JSX) > 0 {
		var jsxObj struct {
			Version json.RawMessage `json:"version"`
			Mode    string          `json:"mode"`
		}
		if err := json.Unmarshal(data.JSX, &jsxObj); err == nil {
			if v := trimJSONNumber(jsxObj.Version); v != "" {
				parts["jsx_version"] = v
			}
			if jsxObj.Mode != "" {
				parts["jsx_mode"] = jsxObj.Mode
			}
		}
		// Bare-number form: "jsx": 3.
		if len(parts) == 0 {
			if v := trimJSONNumber(data.JSX); v != "" {
				parts["jsx_version"] = v
			}
		}
	}
	// Legacy "reason": { "react-jsx": 3 }.
	if _, ok := parts["jsx_version"]; !ok && len(data.ReactJsx) > 0 {
		var reason struct {
			ReactJsx json.RawMessage `json:"react-jsx"`
		}
		if err := json.Unmarshal(data.ReactJsx, &reason); err == nil {
			if v := trimJSONNumber(reason.ReactJsx); v != "" {
				parts["jsx_version"] = v
			}
		}
	}

	// module: a string ("es6"|"commonjs") or, via package-specs, an array of
	// { "module": "...", "in-source": true } objects.
	if mod := firstModuleSpec(data.Module, data.PackageSpecs); mod != "" {
		parts["module"] = mod
	}
	if data.Suffix != "" {
		parts["suffix"] = data.Suffix
	}
	if ns := trimJSONScalar(data.Namespace); ns != "" {
		parts["namespace"] = ns
	}

	if len(parts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(';')
		}
		fmt.Fprintf(&b, "%s=%s", k, parts[k])
	}
	return b.String()
}

// firstModuleSpec returns the module format declared either directly as a
// top-level "module" string or via the first entry of "package-specs". Returns
// "" when neither is present.
func firstModuleSpec(module, packageSpecs json.RawMessage) string {
	if m := trimJSONScalar(module); m != "" {
		return m
	}
	if len(packageSpecs) == 0 {
		return ""
	}
	// package-specs may be a single object or an array of objects.
	var arr []struct {
		Module string `json:"module"`
	}
	if err := json.Unmarshal(packageSpecs, &arr); err == nil {
		for _, s := range arr {
			if s.Module != "" {
				return s.Module
			}
		}
		return ""
	}
	var single struct {
		Module string `json:"module"`
	}
	if err := json.Unmarshal(packageSpecs, &single); err == nil {
		return single.Module
	}
	return ""
}

// trimJSONNumber renders a JSON number (or quoted number) raw message as a bare
// string, or "" when the raw message is empty/non-scalar.
func trimJSONNumber(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	s = strings.Trim(s, `"`)
	if s == "" || s == "null" {
		return ""
	}
	return s
}

// trimJSONScalar renders a JSON string scalar raw message as its unquoted value,
// or "" when the raw message is empty/null/non-string.
func trimJSONScalar(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	var str string
	if err := json.Unmarshal([]byte(s), &str); err == nil {
		return str
	}
	return ""
}
