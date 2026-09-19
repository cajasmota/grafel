// Package jsext is the single authority on which on-disk file extensions a
// JS/TS relative import specifier's own extension may legitimately name.
//
// TypeScript under Node16/NodeNext requires the specifier to carry the
// EMITTED extension, not the source one — `./foo.js` is how you reach
// `foo.ts`. But the mapping is per-module-system-family, not a free-for-all:
//
//	.js  <-> .ts .tsx .js .jsx     (the ordinary/ESM-or-CJS family)
//	.mjs <-> .mts .mjs             (always-ESM family)
//	.cjs <-> .cts .cjs             (always-CommonJS family)
//
// `./x.mjs` can only mean `x.mts` or `x.mjs`; it can NEVER mean `x.ts`.
// Letting it reach `x.ts` would mint an edge to a file the author did not
// reference — trading a false negative for a false positive.
//
// This table lives in its own leaf package because BOTH sides of the pipeline
// need it and neither may own it: internal/resolve applies it when probing the
// file-carrier map (#7279) and internal/extractors/javascript applies it when
// stamping importBinding.resolvedFile from the filesystem (#7276). The two
// must give the same answer or the extractor addresses a location the resolver
// keys differently — which is the #7272 defect class. A second independent
// derivation of "which extensions correspond" is exactly what this package
// exists to prevent, so add extensions HERE and nowhere else.
package jsext

// Replacements maps a canonical extension appearing on an import specifier to
// the set of on-disk extensions that specifier may legitimately name, in
// preference order. The TypeScript source forms come first: when both
// `foo.ts` and `foo.js` exist, the `.js` is the build output of the `.ts` and
// the source is what the graph should address.
//
// An extensionless specifier has no family signal and is deliberately NOT
// covered here — callers fall back to their own full extension list. See
// internal/resolve/imports.go's jsExtensionReplacements comment for why that
// asymmetry is in the INPUT rather than the policy.
var Replacements = map[string][]string{
	".ts":  {".ts", ".tsx", ".js", ".jsx"},
	".tsx": {".ts", ".tsx", ".js", ".jsx"},
	".js":  {".ts", ".tsx", ".js", ".jsx"},
	".jsx": {".ts", ".tsx", ".js", ".jsx"},
	".mjs": {".mts", ".mjs"},
	".mts": {".mts", ".mjs"},
	".cjs": {".cts", ".cjs"},
	".cts": {".cts", ".cjs"},
}

// ReplacementsFor returns the on-disk extensions ext may name, in preference
// order. An extension with no family entry returns nil — callers must then
// leave the specifier alone rather than guessing.
func ReplacementsFor(ext string) []string {
	return Replacements[ext]
}
