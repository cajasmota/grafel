// Rust entry-point sniffer (#2767 Phase 1B T2).
//
// Recognises:
//   - `fn main(` at module scope → cli_main. Also matches
//     `async fn main(` and `pub fn main(`.
//   - Functions preceded by `#[test]`, `#[bench]`, `#[tokio::test]`,
//     `#[async_std::test]`, `#[rstest]` attributes → test_entry.
//   - Functions preceded by lifecycle / framework attributes
//     (`#[ctor::ctor]`, `#[ctor::dtor]`, `#[no_mangle]`,
//     `#[wasm_bindgen(start)]`, `#[tokio::main]`, `#[actix_web::main]`,
//     `#[tauri::command]`) → framework_lifecycle.
//   - `pub fn|struct|enum|trait|mod|const|static <name>` at module
//     scope → library_export. Rust's visibility model is explicit:
//     anything without `pub` is crate-private and not an entry point.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterEntryPoints("rust", sniffRustEntryPoints) }

// rustMainFnRe matches `fn main(` with optional `pub`, `async`, `const`
// modifiers.
var rustMainFnRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(?:const\s+)?(?:unsafe\s+)?fn\s+main\s*\(`,
)

// rustPubItemRe matches a `pub <kind> <name>` item declaration at
// module scope. Capture 1 = item kind; capture 2 = name.
var rustPubItemRe = regexp.MustCompile(
	`(?m)^[ \t]*pub(?:\([^)]*\))?\s+(?:async\s+)?(?:unsafe\s+)?(?:extern(?:\s+"[^"]*")?\s+)?(fn|struct|enum|trait|mod|const|static|union|type)\s+([A-Za-z_]\w*)`,
)

// rustAttributeRe matches a single `#[…]` attribute line. Capture 1 =
// raw attribute body (the part inside the brackets).
var rustAttributeRe = regexp.MustCompile(`(?m)^[ \t]*#\[([^\]]+)\]`)

// rustFnDeclRe matches any `fn <name>(` declaration regardless of
// visibility, so the attribute lookback can pair an attribute with the
// function it decorates. Capture 1 = name.
var rustFnDeclRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(?:const\s+)?(?:unsafe\s+)?(?:extern(?:\s+"[^"]*")?\s+)?fn\s+([A-Za-z_]\w*)\s*[<(]`,
)

// rustTestAttrs are attributes that mark a function as a test entry.
var rustTestAttrs = map[string]bool{
	"test":             true,
	"bench":            true,
	"tokio::test":      true,
	"async_std::test":  true,
	"rstest":           true,
	"actix_rt::test":   true,
	"actix_web::test":  true,
	"test_log::test":   true,
}

// rustLifecycleAttrs are attributes that mark a function as a runtime
// / framework lifecycle entry. `tokio::main` and `actix_web::main` are
// async-runtime wrappers around `fn main`; we still treat the wrapped
// function as cli_main when its name is `main`, so the lookback below
// only emits lifecycle when the function name is not `main`.
var rustLifecycleAttrs = map[string]bool{
	"ctor::ctor":        true,
	"ctor::dtor":        true,
	"no_mangle":         true,
	"wasm_bindgen(start)": true,
	"tokio::main":       true,
	"actix_web::main":   true,
	"tauri::command":    true,
	"async_trait":       true,
}

func sniffRustEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint
	mainLines := map[int]bool{}

	for _, m := range rustMainFnRe.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		mainLines[line] = true
		out = append(out, EntryPoint{
			Ident: "main",
			Line:  line,
			Kind:  EntryKindCLIMain,
		})
	}

	// Pre-index attribute lines.
	attrAtLine := map[int]string{}
	for _, m := range rustAttributeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		line := lineOfOffset(content, m[0])
		body := strings.TrimSpace(content[m[2]:m[3]])
		attrAtLine[line] = body
	}

	lines := strings.Split(content, "\n")

	for _, m := range rustFnDeclRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		if mainLines[line] {
			continue
		}
		kind := EntryKind("")
		// Look back up to 5 contiguous attribute or blank lines.
	scan:
		for back := 1; back <= 5; back++ {
			lineNo := line - back
			if lineNo < 1 || lineNo > len(lines) {
				break
			}
			body, hasAttr := attrAtLine[lineNo]
			if !hasAttr {
				trimmed := strings.TrimSpace(lines[lineNo-1])
				if trimmed == "" {
					continue
				}
				break
			}
			if rustTestAttrs[body] {
				kind = EntryKindTestEntry
				break scan
			}
			if rustLifecycleAttrs[body] {
				kind = EntryKindFrameworkLifecycle
				break scan
			}
		}
		if kind != "" {
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: kind})
		}
	}

	for _, m := range rustPubItemRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		name := content[m[4]:m[5]]
		line := lineOfOffset(content, m[0])
		if name == "main" && mainLines[line] {
			continue
		}
		out = append(out, EntryPoint{
			Ident: name,
			Line:  line,
			Kind:  EntryKindLibraryExport,
		})
	}

	return out
}
