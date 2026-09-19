// Package feedback implements the anonymized quality-report skill (issue #2544).
// It computes structural metrics from a loaded graph, anonymizes all identifiers
// and paths with an ephemeral per-report salt, and renders a markdown report.
package feedback

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// kindFamily maps entity Kind strings to the short prefix used in hashed names.
// Any kind not listed falls back to "ent_".
var kindFamily = map[string]string{
	"function":                 "op_",
	"method":                   "op_",
	"operation":                "op_",
	"class":                    "ent_",
	"struct":                   "ent_",
	"interface":                "ent_",
	"model":                    "ent_",
	"module":                   "mod_",
	"http_endpoint":            "ep_",
	"http_endpoint_definition": "ep_",
	"http_endpoint_call":       "ep_",
	"endpoint":                 "ep_",
	"variable":                 "var_",
	"constant":                 "var_",
	"field":                    "var_",
}

// unusualExts maps file extensions that might narrow a project's identity to
// language-family bucket labels. Extensions not listed are preserved as-is.
var unusualExts = map[string]string{
	".clj":    "<jvm-lang>",
	".cljs":   "<jvm-lang>",
	".cljc":   "<jvm-lang>",
	".ex":     "<beam-lang>",
	".exs":    "<beam-lang>",
	".erl":    "<beam-lang>",
	".hrl":    "<beam-lang>",
	".hs":     "<ml-lang>",
	".lhs":    "<ml-lang>",
	".ml":     "<ml-lang>",
	".mli":    "<ml-lang>",
	".fs":     "<ml-lang>",
	".fsi":    "<ml-lang>",
	".fsx":    "<ml-lang>",
	".elm":    "<ml-lang>",
	".scala":  "<jvm-lang>",
	".groovy": "<jvm-lang>",
	".kt":     "<jvm-lang>",
	".kts":    "<jvm-lang>",
	".lua":    "<scripting-lang>",
	".nim":    "<systems-lang>",
	".zig":    "<systems-lang>",
	".cr":     "<scripting-lang>",
	".jl":     "<ml-lang>",
	".r":      "<ml-lang>",
	".R":      "<ml-lang>",
	".m":      "<matlab-lang>",
	".swift":  "<apple-lang>",
	".dart":   "<flutter-lang>",
}

// NameHash returns a short anonymized identifier for name using the given salt.
// The format is "<kind-prefix><4-hex>", e.g. "ent_a3f7" or "op_92c1".
// kind is the entity Kind string from graph.Entity.Kind.
func NameHash(name string, kind string, salt []byte) string {
	prefix := kindFamily[strings.ToLower(kind)]
	if prefix == "" {
		prefix = "ent_"
	}
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(":"))
	h.Write([]byte(name))
	sum := h.Sum(nil)
	return prefix + hex.EncodeToString(sum[:2])
}

// PathScrub transforms a file path into a depth-preserved structural template.
// Rules:
//   - Split on "/" (filepath.ToSlash normalises OS separators).
//   - Replace each directory segment with "<seg-N>" (1-indexed from the leftmost segment).
//   - The filename's extension is preserved (or bucketed for unusual extensions).
//   - The first segment is replaced with the extension label: "<ext>" e.g. "<go>".
//   - If the total depth (number of segments) exceeds 5 the tail is replaced with "<...>".
//     The cap applies before extension bucketing so the structure is always ≤6 tokens.
//
// Example:
//
//	src/main/java/com/example/UserController.java → <java>/<seg-1>/<seg-2>/<seg-3>/<seg-4>.java
func PathScrub(p string) string {
	p = filepath.ToSlash(p)
	// #7200: no length guard needed — strings.Split with a non-empty separator
	// always returns >= 1 element, so the index below cannot be out of range.
	// Full derivation: extractIndentBody in internal/extractors/nim/nim.go.
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")

	// Pull the filename and extension off the last segment.
	last := segs[len(segs)-1]
	ext := filepath.Ext(last)
	dirSegs := segs[:len(segs)-1]

	// Bucket unusual extensions.
	bucketedExt := ext
	if bucket, ok := unusualExts[strings.ToLower(ext)]; ok {
		bucketedExt = bucket
	}

	// Build the prefix token from the extension family label.
	var extLabel string
	if ext == "" {
		extLabel = "<file>"
	} else {
		// e.g. ".go" → "<go>", ".ts" → "<ts>"
		raw := strings.TrimPrefix(strings.ToLower(ext), ".")
		if bucket, ok := unusualExts[ext]; ok {
			extLabel = bucket
		} else {
			extLabel = "<" + raw + ">"
		}
	}

	// Cap depth: dirs beyond 4 are folded into "<...>".
	// Total path after scrub: extLabel / seg-1 / … / seg-N[capped] . ext
	const maxDirSegs = 4
	truncated := false
	if len(dirSegs) > maxDirSegs {
		dirSegs = dirSegs[:maxDirSegs]
		truncated = true
	}

	parts := make([]string, 0, len(dirSegs)+2)
	parts = append(parts, extLabel)
	for i := range dirSegs {
		parts = append(parts, fmt.Sprintf("<seg-%d>", i+1))
	}
	if truncated {
		parts = append(parts, "<...>")
	}

	result := strings.Join(parts, "/")
	if bucketedExt != "" {
		result += bucketedExt
	}
	return result
}
