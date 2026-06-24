// elm.go — Elm `elm.json` package manifest parser (#5375, epic #5360).
//
// elm.json (https://github.com/elm/compiler/blob/master/docs/elm.json/) is the
// manifest for an Elm project. It comes in two shapes distinguished by the
// `"type"` field:
//
//	APPLICATION (an app you compile and run):
//	  {
//	    "type": "application",
//	    "dependencies": {
//	      "direct":   { "elm/browser": "1.0.2", "elm/html": "1.0.0" },
//	      "indirect": { "elm/virtual-dom": "1.0.3" }
//	    },
//	    "test-dependencies": {
//	      "direct":   { "elm-explorations/test": "2.1.0" },
//	      "indirect": {}
//	    }
//	  }
//
//	PACKAGE (a reusable library published to the Elm registry):
//	  {
//	    "type": "package",
//	    "dependencies":      { "elm/core": "1.0.0 <= v < 2.0.0" },
//	    "test-dependencies": { "elm-explorations/test": "2.0.0 <= v < 3.0.0" }
//	  }
//
// An application's `dependencies` is an object with `direct`/`indirect` sub-maps
// of EXACTLY-PINNED versions (elm.json IS the lockfile — there is no separate
// lockfile format), so indirect deps are flagged indirect=true. A package's
// `dependencies` is a FLAT map of version-RANGE constraints. Both shapes carry a
// `test-dependencies` block whose deps are flagged is_dev=true. The package
// manager is `elm`; the registry is the Elm package website (package.elm-lang.org)
// and package names are GitHub-style `author/project` slugs.
package manifest

import "encoding/json"

// parseElmJSON parses an Elm `elm.json` manifest (application or package shape)
// and returns its declared dependencies. Application indirect deps are flagged
// indirect=true; every test-dependency is flagged is_dev=true. First declaration
// of a name wins on duplicates (direct runtime > indirect > test).
func parseElmJSON(source string) []dep {
	// The two shapes differ in the value type of "dependencies":
	//   application: { "direct": {...}, "indirect": {...} }
	//   package:     { "elm/core": "..." }
	// We decode "dependencies"/"test-dependencies" into json.RawMessage and then
	// try the nested-object shape first, falling back to the flat-map shape.
	var data struct {
		Type         string          `json:"type"`
		Dependencies json.RawMessage `json:"dependencies"`
		TestDeps     json.RawMessage `json:"test-dependencies"`
	}
	if err := json.Unmarshal([]byte(source), &data); err != nil {
		return nil
	}

	var out []dep
	seen := map[string]bool{}
	emit := func(name, version string, isDev, indirect bool) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		kind := "runtime"
		if isDev {
			kind = "dev"
		}
		out = append(out, dep{name: name, version: version, isDev: isDev, kind: kind, indirect: indirect})
	}

	// emitBlock decodes one dependency block (the application nested
	// direct/indirect form OR the package flat-map form) and emits its deps.
	emitBlock := func(raw json.RawMessage, isDev bool) {
		if len(raw) == 0 {
			return
		}
		// Application shape: { "direct": {...}, "indirect": {...} }.
		var nested struct {
			Direct   map[string]string `json:"direct"`
			Indirect map[string]string `json:"indirect"`
		}
		if err := json.Unmarshal(raw, &nested); err == nil && (nested.Direct != nil || nested.Indirect != nil) {
			for name, ver := range nested.Direct {
				emit(name, ver, isDev, false)
			}
			for name, ver := range nested.Indirect {
				emit(name, ver, isDev, true)
			}
			return
		}
		// Package shape: flat { "author/project": "constraint" }.
		var flat map[string]string
		if err := json.Unmarshal(raw, &flat); err == nil {
			for name, ver := range flat {
				emit(name, ver, isDev, false)
			}
		}
	}

	// Runtime deps first so a name shared with a test dep keeps its runtime
	// classification (first-declaration-wins on the `seen` guard).
	emitBlock(data.Dependencies, false)
	emitBlock(data.TestDeps, true)
	return out
}
