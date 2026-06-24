package manifest

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

func depNames(deps []types.EntityRecord) []string {
	out := make([]string, len(deps))
	for i := range deps {
		out[i] = deps[i].Name
	}
	return out
}

// realRockspec is a representative LuaRocks manifest (shape taken from real
// published rocks such as luafilesystem / lpeg / penlight). It exercises all
// three dependency lists and the bare-vs-constrained name forms.
const realRockspec = `package = "penlight"
version = "1.13.1-1"
source = {
   url = "git+https://github.com/lunarmodules/Penlight.git",
   tag = "1.13.1",
}
description = {
   summary = "Lua libraries for extended operations",
   license = "MIT/X11",
}
dependencies = {
   "lua >= 5.1",
   "luafilesystem >= 1.6.2",
   "lpeg",
}
build_dependencies = {
   "luarocks-build-cpp",
}
test_dependencies = {
   "busted >= 2.0",
   "luacov",
}
build = {
   type = "builtin",
}
`

func TestRockspec_RuntimeDeps(t *testing.T) {
	deps := depEntities(runExtract(t, "penlight-1.13.1-1.rockspec", realRockspec))
	// 3 runtime + 1 build + 2 test = 6 declared deps.
	if len(deps) != 6 {
		t.Fatalf("expected 6 deps, got %d: %+v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "luarocks" {
			t.Errorf("%s: package_manager=%q want luarocks", d.Name, d.Properties["package_manager"])
		}
	}

	// Runtime deps with their parsed version constraints.
	if d := depByName(deps, "lua"); d == nil {
		t.Error("expected runtime dep 'lua' (interpreter floor is a real edge)")
	} else if d.Properties["version"] != ">= 5.1" {
		t.Errorf("lua version=%q want '>= 5.1'", d.Properties["version"])
	} else if d.Properties["is_dev"] != "false" {
		t.Errorf("lua should be is_dev=false")
	}
	if d := depByName(deps, "luafilesystem"); d == nil || d.Properties["version"] != ">= 1.6.2" {
		t.Errorf("luafilesystem version=%v want '>= 1.6.2'", d)
	}
	// Bare name → empty version constraint, still a runtime dep.
	if d := depByName(deps, "lpeg"); d == nil {
		t.Error("expected bare runtime dep 'lpeg'")
	} else if d.Properties["version"] != "" {
		t.Errorf("lpeg version=%q want empty", d.Properties["version"])
	}
}

func TestRockspec_BuildAndTestDepsAreDev(t *testing.T) {
	deps := depEntities(runExtract(t, "penlight-1.13.1-1.rockspec", realRockspec))
	for _, name := range []string{"luarocks-build-cpp", "busted", "luacov"} {
		d := depByName(deps, name)
		if d == nil {
			t.Fatalf("expected build/test dep %q", name)
		}
		if d.Properties["is_dev"] != "true" {
			t.Errorf("%s should be is_dev=true (build/test dependency)", name)
		}
		if d.Properties["dependency_kind"] != "dev" {
			t.Errorf("%s dependency_kind=%q want dev", name, d.Properties["dependency_kind"])
		}
	}
	// busted carries its version constraint.
	if d := depByName(deps, "busted"); d == nil || d.Properties["version"] != ">= 2.0" {
		t.Errorf("busted version=%v want '>= 2.0'", d)
	}
}

func TestRockspec_DependsOnEdges(t *testing.T) {
	rels := dependsOnRels(runExtract(t, "lpeg-1.0.2-1.rockspec",
		`package = "lpeg"
version = "1.0.2-1"
dependencies = { "lua >= 5.1" }
`))
	if len(rels) != 1 {
		t.Fatalf("expected 1 DEPENDS_ON edge, got %d", len(rels))
	}
	if rels[0].Properties["package_manager"] != "luarocks" {
		t.Errorf("edge package_manager=%q want luarocks", rels[0].Properties["package_manager"])
	}
}

func TestRockspec_NoDependencies(t *testing.T) {
	deps := depEntities(runExtract(t, "tiny-1.0-1.rockspec",
		`package = "tiny"
version = "1.0-1"
build = { type = "builtin" }
`))
	if len(deps) != 0 {
		t.Errorf("expected 0 deps for dependency-free rockspec, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// luarocks.lock
// ---------------------------------------------------------------------------

// realLuarocksLock is the shape `luarocks install --lock` writes: a Lua
// return-table with a `dependencies` map of exactly-pinned versions including
// the transitive closure (lua_cliargs / mediator_lua are transitive of busted).
const realLuarocksLock = `return {
   dependencies = {
      lua = "5.4-1",
      ["luafilesystem"] = "1.8.0-1",
      ["lpeg"] = "1.0.2-1",
      ["lua_cliargs"] = "3.0-2",
   },
}
`

func TestLuarocksLock_PinnedVersions(t *testing.T) {
	deps := depEntities(runExtract(t, "luarocks.lock", realLuarocksLock))
	if len(deps) != 4 {
		t.Fatalf("expected 4 locked deps, got %d: %+v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "luarocks" {
			t.Errorf("%s: package_manager=%q want luarocks", d.Name, d.Properties["package_manager"])
		}
		if d.Properties["dependency_kind"] != "locked" {
			t.Errorf("%s: dependency_kind=%q want locked", d.Name, d.Properties["dependency_kind"])
		}
	}
	// Pinned exact versions (both bracketed and bare-key forms).
	if d := depByName(deps, "lua"); d == nil || d.Properties["version"] != "5.4-1" {
		t.Errorf("lua version=%v want '5.4-1' (bare-key form)", d)
	}
	if d := depByName(deps, "lpeg"); d == nil || d.Properties["version"] != "1.0.2-1" {
		t.Errorf("lpeg version=%v want '1.0.2-1' (bracketed form)", d)
	}
	// Transitive dep present only in the lock, never in a rockspec.
	if depByName(deps, "lua_cliargs") == nil {
		t.Error("expected transitive locked dep 'lua_cliargs'")
	}
}

func TestRockspec_IsManifest(t *testing.T) {
	cases := map[string]bool{
		"penlight-1.13.1-1.rockspec": true,
		"path/to/foo.rockspec":       true,
		"luarocks.lock":              true,
		"foo.lua":                    false,
		"rockspec":                   false,
	}
	for path, want := range cases {
		if got := IsManifest(path); got != want {
			t.Errorf("IsManifest(%q)=%v want %v", path, got, want)
		}
	}
}

func TestRockspec_DetectPackageManager(t *testing.T) {
	for _, path := range []string{"foo-1.0-1.rockspec", "luarocks.lock"} {
		if pm := detectPackageManager(path); pm != "luarocks" {
			t.Errorf("detectPackageManager(%q)=%q want luarocks", path, pm)
		}
	}
}
