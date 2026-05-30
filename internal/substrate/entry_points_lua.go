// Lua entry-point sniffer (Phase 1B).
//
// Recognises:
//   - `#!/usr/bin/env lua` shebang at line 1 → cli_main (synthetic "__main__")
//   - top-level `if arg and arg[0] == debug.getinfo(1,'S').source:sub(2) then` → cli_main
//   - require("busted") / describe()/it() blocks → test_entry
//   - function test_Xxx() patterns (luaunit) → test_entry
//   - function M.main() / function main() at top level → cli_main
//   - love.load / love.update / love.draw (LÖVE game framework) → framework_lifecycle
//   - init_by_lua_block / init_worker_by_lua_block (OpenResty) → framework_lifecycle
//   - Kong handler init() function → framework_lifecycle
//   - Public module functions (no local prefix at top level) → library_export
package substrate

import "regexp"

func init() { RegisterEntryPoints("lua", sniffLuaEntryPoints) }

// luaShebangRe matches a lua shebang on line 1.
var luaShebangRe = regexp.MustCompile(`\A#![^\n]*\blua\b`)

// luaMainFuncRe matches a top-level `function main(` declaration.
var luaMainFuncRe = regexp.MustCompile(`(?m)^function\s+main\s*\(`)

// luaModuleMainRe matches `function M.main(` at top level.
var luaModuleMainRe = regexp.MustCompile(`(?m)^function\s+[A-Za-z_][\w]*\.main\s*\(`)

// luaArgCheckRe matches the canonical Lua script guard:
// if arg and arg[0] == ... or pcall idiom used in standalone scripts.
var luaArgCheckRe = regexp.MustCompile(
	`(?m)\bif\s+arg\s+and\s+arg\s*\[\s*0\s*\]`)

// luaBustedBlockRe matches busted describe/it block calls at module scope.
var luaBustedBlockRe = regexp.MustCompile(
	`(?m)^[ \t]*(describe|it|spec|context)\s*\(\s*["']`)

// luaUnitTestRe matches luaunit-style test methods: function Class:testXxx()
var luaUnitTestRe = regexp.MustCompile(
	`(?m)^function\s+\w+\s*:\s*(test\w+)\s*\(`)

// luaLoveLifecycleRe matches LÖVE framework lifecycle callbacks.
var luaLoveLifecycleRe = regexp.MustCompile(
	`(?m)^function\s+love\s*\.\s*(load|update|draw|quit|keypressed|mousepressed|resize|focus)\s*\(`)

// luaOpenRestyInitRe matches init_by_lua_block / init_worker_by_lua_block directives.
var luaOpenRestyInitRe = regexp.MustCompile(
	`(?m)\binit(?:_worker)?_by_lua(?:_block|_file)\b`)

// luaKongInitRe matches Kong plugin init handler.
var luaKongInitRe = regexp.MustCompile(
	`(?m)^function\s+\w+\s*:\s*init\s*\(`)

// luaTopLevelPublicFuncRe matches non-local top-level function declarations
// that are public exports (no `local` prefix, not lifecycle names).
var luaTopLevelPublicFuncRe = regexp.MustCompile(
	`(?m)^function\s+([A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)?)\s*\(`)

func sniffLuaEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint

	// Shebang → cli_main
	if luaShebangRe.MatchString(content) {
		out = append(out, EntryPoint{Ident: "__main__", Line: 1, Kind: EntryKindCLIMain})
	}

	// function main() / M.main()
	for _, m := range luaMainFuncRe.FindAllStringIndex(content, -1) {
		out = append(out, EntryPoint{
			Ident: "main",
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindCLIMain,
		})
	}
	for _, m := range luaModuleMainRe.FindAllStringIndex(content, -1) {
		out = append(out, EntryPoint{
			Ident: "main",
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindCLIMain,
		})
	}

	// arg-check idiom → standalone script
	if luaArgCheckRe.MatchString(content) {
		loc := luaArgCheckRe.FindStringIndex(content)
		out = append(out, EntryPoint{
			Ident: "__main__",
			Line:  lineOfOffset(content, loc[0]),
			Kind:  EntryKindCLIMain,
		})
	}

	// busted test blocks
	for _, m := range luaBustedBlockRe.FindAllStringIndex(content, -1) {
		out = append(out, EntryPoint{
			Ident: "describe",
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	// luaunit test methods
	for _, m := range luaUnitTestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	// LÖVE game lifecycle callbacks
	for _, m := range luaLoveLifecycleRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := "love." + content[m[2]:m[3]]
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindFrameworkLifecycle,
		})
	}

	// OpenResty init directives → framework lifecycle
	if luaOpenRestyInitRe.MatchString(content) {
		loc := luaOpenRestyInitRe.FindStringIndex(content)
		out = append(out, EntryPoint{
			Ident: "init_by_lua",
			Line:  lineOfOffset(content, loc[0]),
			Kind:  EntryKindFrameworkLifecycle,
		})
	}

	// Kong init handler
	if luaKongInitRe.MatchString(content) {
		loc := luaKongInitRe.FindStringIndex(content)
		out = append(out, EntryPoint{
			Ident: "init",
			Line:  lineOfOffset(content, loc[0]),
			Kind:  EntryKindFrameworkLifecycle,
		})
	}

	// Top-level public functions → library exports
	for _, m := range luaTopLevelPublicFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		// Skip lifecycle names already captured above.
		if name == "main" || name == "love.load" || name == "love.update" {
			continue
		}
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindLibraryExport,
		})
	}

	return out
}
