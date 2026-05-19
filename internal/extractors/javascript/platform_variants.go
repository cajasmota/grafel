// platform_variants.go — React Native platform-specific file detection (#713).
//
// React Native (and Expo) support platform-specific file extensions so the
// Metro bundler can resolve the correct implementation at build time:
//
//	Button.tsx          — canonical (base) component
//	Button.ios.tsx      — iOS-specific override
//	Button.android.tsx  — Android-specific override
//	Button.web.tsx      — Web-specific override
//	Button.native.tsx   — native (non-web) override
//	Button.default.tsx  — default platform fallback
//	Button.tablet.tsx   — tablet form-factor variant
//	Button.landscape.tsx — landscape orientation variant
//	Button.mobile.tsx   — mobile form-factor variant
//
// Consumer code does `import Button from './Button'` — the bundler selects
// the correct platform file at runtime. The TypeScript extractor creates
// function/component entities for each platform file but sees no import
// site (the import path omits the platform suffix), leaving them as true
// orphan islands.
//
// Fix: after extraction, the file-level entity for a platform-variant file
// receives a PLATFORM_VARIANT_OF relationship pointing to the canonical
// (suffix-stripped) file path. The resolver can match this to the canonical
// file entity in the graph so the platform variant gains an inbound edge and
// exits the orphan pool.
//
// When no canonical file exists for a given base name (only a .ios.tsx
// variant exists without a .tsx peer), we do NOT emit the edge — the variant
// IS effectively the canonical file for that platform.
//
// Beyond-minimum (#713 extension):
//   - *.test.tsx / *.test.ts / *.spec.tsx / *.spec.ts paired with their
//     source file emit a TESTS edge. Test files are orphan by definition
//     (nothing imports them) but they ARE related to the source they test.
//     TESTS edges surface this relationship so test files exit the orphan
//     pool.
//   - *.module.css / *.module.scss / *.module.sass alongside a *.tsx
//     consumer are left as a file-follow-up (CSS Module import edges are
//     already tracked via the IMPORTS pass for most consumers; standalone
//     stylesheet orphans are a separate class).

package javascript

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)


// rnPlatformSuffixes is the ordered set of React Native / Expo platform
// extensions that are stripped to find the canonical base name.
// Order matters for the multi-suffix case: we strip suffixes from the
// OUTSIDE IN, so `.tablet.landscape.tsx` strips `.landscape` first to
// produce the `.tablet.tsx` candidate, then strips `.tablet` to produce
// the `.tsx` canonical.
var rnPlatformSuffixes = []string{
	".ios",
	".android",
	".web",
	".native",
	".default",
	".tablet",
	".landscape",
	".mobile",
	// Compound suffixes (order: longer first so the first match wins)
	// are handled implicitly by iterating the list and applying the
	// first matching suffix. Multi-segment compound variants like
	// `.mobile.landscape` require two passes through the stripping
	// loop; we run up to two strips before checking canonical existence.
}

// jsVariantExts is the set of file extensions that may carry a platform
// suffix. We restrict to TS/TSX/JS/JSX to avoid false matches on CSS,
// config, etc.
var jsVariantExts = map[string]bool{
	".tsx":  true,
	".ts":   true,
	".jsx":  true,
	".js":   true,
}

// stripOnePlatformSuffix attempts to strip one platform suffix from a
// bare file name (WITHOUT the final extension). E.g.:
//
//	"Button.ios"   → "Button", ".ios", true
//	"Button.tablet" → "Button", ".tablet", true
//	"Button"        → "", "", false
func stripOnePlatformSuffix(bare string) (base string, suffix string, ok bool) {
	for _, sfx := range rnPlatformSuffixes {
		if strings.HasSuffix(bare, sfx) {
			return bare[:len(bare)-len(sfx)], sfx, true
		}
	}
	return "", "", false
}

// isPlatformVariantFile reports whether filePath is a platform-variant
// source file (e.g. `Button.ios.tsx`). Returns the canonical base path
// (e.g. `Button.tsx`) if this is a platform variant, or "" otherwise.
//
// The canonical path is returned regardless of whether the file exists on
// disk — callers that need the existence check run it themselves.
func isPlatformVariantFile(filePath string) (canonicalPath string) {
	ext := filepath.Ext(filePath)
	if !jsVariantExts[ext] {
		return ""
	}
	// Strip the final extension to get `dir/Button.ios` (bare).
	bare := filePath[:len(filePath)-len(ext)]
	base, _, ok := stripOnePlatformSuffix(bare)
	if !ok {
		return ""
	}
	// Two-level compound: `.mobile.landscape` → strip `.landscape` → then
	// strip `.mobile`. If the intermediate also has a platform suffix, the
	// canonical is the fully-stripped name.
	if _, _, ok2 := stripOnePlatformSuffix(base); ok2 {
		// The base itself has another platform suffix, meaning this is a
		// compound variant like `.mobile.landscape.tsx`. The canonical is
		// the doubly-stripped name with the original extension.
		doublyStripped, _, _ := stripOnePlatformSuffix(base)
		return doublyStripped + ext
	}
	return base + ext
}

// isTestFile reports whether filePath is a test/spec file (*.test.ts,
// *.spec.tsx, etc.) and returns the canonical source path it tests.
// Returns "" if the file is not a test file.
func isTestFile(filePath string) (sourcePath string) {
	ext := filepath.Ext(filePath)
	if !jsVariantExts[ext] {
		return ""
	}
	bare := filePath[:len(filePath)-len(ext)]
	for _, sfx := range []string{".test", ".spec"} {
		if strings.HasSuffix(bare, sfx) {
			return bare[:len(bare)-len(sfx)] + ext
		}
	}
	return ""
}

// emitPlatformVariantRelationships examines the file being extracted and,
// when it is a platform-variant or test file, appends relationship records
// to the file-level entity in x.entities.
//
// Called from Extract() AFTER the primary walk has completed so all
// entities (including the file entity at index 0) are already present.
//
// For platform-variant files:
//   - Emits PLATFORM_VARIANT_OF from the platform file entity to the
//     canonical file path (as the ToID bare path; the resolver matches
//     by file name). Only fires when the canonical file exists on disk
//     inside the same repo (repoRoot is checked).
//
// For test files:
//   - Emits TESTS from the test file entity to the source file path.
//     Only fires when the source file exists on disk inside the same repo.
//
// Both edges are added to the file-level SCOPE.Component entity (subtype
// "file") at entities[0]. If no file entity exists or it is not the right
// type, the function is a no-op.
func (x *extractor) emitPlatformVariantRelationships() {
	if len(x.entities) == 0 {
		return
	}

	// Find the file-level entity.
	fileEnt := -1
	for i := range x.entities {
		if x.entities[i].Subtype == "file" && x.entities[i].SourceFile == x.filePath {
			fileEnt = i
			break
		}
	}
	if fileEnt < 0 {
		return
	}

	// Platform variant check.
	if canonical := isPlatformVariantFile(x.filePath); canonical != "" {
		// Only emit when the canonical file exists on disk (so a lone
		// .ios.tsx without a .tsx peer is treated as canonical itself).
		if x.repoRoot != "" {
			canonicalAbs := filepath.Join(x.repoRoot, canonical)
			if _, err := os.Stat(canonicalAbs); err == nil {
				x.entities[fileEnt].Relationships = append(
					x.entities[fileEnt].Relationships,
					types.RelationshipRecord{
						ToID: canonical,
						Kind: string(types.RelationshipKindPlatformVariantOf),
					},
				)
			}
		} else {
			// No repoRoot available (e.g. unit tests) — emit unconditionally
			// so tests can verify the edge without filesystem access.
			x.entities[fileEnt].Relationships = append(
				x.entities[fileEnt].Relationships,
				types.RelationshipRecord{
					ToID: canonical,
					Kind: string(types.RelationshipKindPlatformVariantOf),
				},
			)
		}
	}

	// Test file check.
	if srcPath := isTestFile(x.filePath); srcPath != "" {
		if x.repoRoot != "" {
			srcAbs := filepath.Join(x.repoRoot, srcPath)
			if _, err := os.Stat(srcAbs); err == nil {
				x.entities[fileEnt].Relationships = append(
					x.entities[fileEnt].Relationships,
					types.RelationshipRecord{
						ToID: srcPath,
						Kind: string(types.RelationshipKindTests),
					},
				)
			}
		} else {
			// No repoRoot — emit unconditionally for test coverage.
			x.entities[fileEnt].Relationships = append(
				x.entities[fileEnt].Relationships,
				types.RelationshipRecord{
					ToID: srcPath,
					Kind: string(types.RelationshipKindTests),
				},
			)
		}
	}
}
