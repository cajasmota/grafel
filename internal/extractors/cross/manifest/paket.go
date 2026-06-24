// paket.go — Paket (.NET/F#) package manager parser (#5368, epic #5360).
//
// Paket (https://fsprojects.github.io/Paket/) is the dependency manager most
// commonly used by F# (and broader .NET) projects as an alternative to bare
// NuGet PackageReference. It pins dependencies in two files at the repo root:
//
//   - paket.dependencies — the MANIFEST: human-authored declared dependencies.
//   - paket.lock         — the LOCKFILE: the fully-resolved transitive closure
//     Paket writes after `paket install`, with exact versions.
//
// ---------------------------------------------------------------------------
// paket.dependencies (manifest)
// ---------------------------------------------------------------------------
//
//	source https://api.nuget.org/v3/index.json
//	storage: none
//	framework: net8.0
//
//	nuget FSharp.Core
//	nuget Giraffe ~> 5.0
//	nuget Microsoft.EntityFrameworkCore >= 7.0.0
//	nuget Newtonsoft.Json 13.0.3
//
//	group Test
//	    source https://api.nuget.org/v3/index.json
//	    nuget Expecto ~> 9.0
//	    nuget FsUnit
//
//	group Build
//	    nuget Fake.Core.Target
//
// A dependency line is `nuget <PackageId> [<version constraint>]`. The package
// id is the first token after `nuget`; the remainder (a Paket constraint such
// as `~> 5.0`, `>= 7.0.0`, `13.0.3`, or a `prerelease`/`content: none` modifier)
// is kept verbatim as the version. Lines declared inside a `group Test` /
// `group Build` block are flagged is_dev=true (the Test/Build groups are the
// Paket idiom for test/build-only tooling, mirroring the conanfile test_requires
// and nimble taskRequires treatment); the implicit `Main` group is runtime.
//
// Honest-partial: `github user/repo` and `git https://…` source dependencies
// (Paket's GitHub/git-file references) are NOT NuGet packages and are skipped —
// they have no package_manager=paket NuGet coordinate. `source`/`framework`/
// `storage`/`redirects`/`strategy`/`lowest_matching`/`references` directives are
// configuration, not dependencies, and are ignored.
//
// ---------------------------------------------------------------------------
// paket.lock (lockfile)
// ---------------------------------------------------------------------------
//
//	NUGET
//	  remote: https://api.nuget.org/v3/index.json
//	    FSharp.Core (8.0.100)
//	    Giraffe (5.0.0)
//	      FSharp.Core (>= 4.7)
//	      Microsoft.AspNetCore.Http.Abstractions (>= 2.2)
//
//	GROUP Test
//	NUGET
//	  remote: https://api.nuget.org/v3/index.json
//	    Expecto (9.0.4)
//
// Each resolved package sits under a `NUGET` block on a line shaped
// `<PackageId> (<version>)` indented beneath a `remote:` line. Transitive
// dependency CONSTRAINT lines (further-indented `Name (>= x)` under a package)
// are NOT separate resolved packages — they are the resolved package's own
// requirements with a constraint operator, so they are de-duplicated by name
// (first-seen, the resolved top-level entry, wins). Packages under a
// `GROUP Test` / `GROUP Build` header are flagged is_dev=true. Lock entries are
// kind="locked" (the resolved tree the manifest never fully names — the
// #2865 lockfile_parsing contract).
package manifest

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Parser: paket.dependencies
// ---------------------------------------------------------------------------

// paketGroupRE matches a `group <Name>` block header in paket.dependencies.
// The group name is captured so Test/Build groups can flag dev dependencies.
var paketGroupRE = regexp.MustCompile(`(?i)^[ \t]*group\s+([A-Za-z0-9_.-]+)\s*$`)

// paketNugetLineRE matches a `nuget <PackageId> [<version constraint>]`
// dependency line. Group 1 is the package id; group 2 is the remaining
// constraint text (kept verbatim as the version, may be empty).
var paketNugetLineRE = regexp.MustCompile(
	`(?i)^[ \t]*nuget\s+([A-Za-z0-9_.][A-Za-z0-9_.-]*)[ \t]*(.*?)[ \t]*$`)

// paketDevGroup reports whether a Paket group name denotes a test/build-only
// (dev) dependency set. The implicit Main group and any other named group are
// treated as runtime.
func paketDevGroup(group string) bool {
	g := strings.ToLower(group)
	return g == "test" || g == "tests" || g == "build" || g == "fake"
}

// paketCleanVersion trims a Paket version constraint of trailing per-package
// modifiers/options that are not part of the version (e.g. `~> 5.0 prerelease`,
// `>= 1.0 content: none`, an inline `//` comment) so the recorded version stays
// the constraint expression. Empty constraint stays empty (an unpinned `nuget
// FSharp.Core`).
func paketCleanVersion(raw string) string {
	v := raw
	// Strip an inline comment.
	if i := strings.Index(v, "//"); i >= 0 {
		v = v[:i]
	}
	// Strip trailing Paket per-package options that follow the constraint.
	for _, opt := range []string{
		" prerelease", " content:", " redirects:", " framework:",
		" copy_local:", " import_targets:", " strategy:", " lowest_matching:",
		" version_in_path:", " specific_version:", " license_download:",
	} {
		if i := strings.Index(strings.ToLower(v), opt); i >= 0 {
			v = v[:i]
		}
	}
	return strings.TrimSpace(v)
}

// parsePaketDependencies parses a paket.dependencies manifest. Runtime deps come
// from `nuget` lines in the implicit Main group (and any non-Test/Build group);
// Test/Build group deps are flagged is_dev=true. First declaration of a name
// wins, with a runtime declaration taking precedence over a later dev one.
func parsePaketDependencies(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	// Two passes so a package declared both in Main (runtime) and a Test group
	// keeps its runtime classification regardless of file order.
	collect := func(wantDev bool) {
		currentDev := false
		for _, line := range strings.Split(source, "\n") {
			if gm := paketGroupRE.FindStringSubmatch(line); gm != nil {
				currentDev = paketDevGroup(gm[1])
				continue
			}
			m := paketNugetLineRE.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if currentDev != wantDev {
				continue
			}
			name := m[1]
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			kind := "runtime"
			if currentDev {
				kind = "dev"
			}
			out = append(out, dep{
				name:    name,
				version: paketCleanVersion(m[2]),
				isDev:   currentDev,
				kind:    kind,
			})
		}
	}
	collect(false) // runtime first
	collect(true)  // then dev
	return out
}

// ---------------------------------------------------------------------------
// Parser: paket.lock
// ---------------------------------------------------------------------------

// paketLockGroupRE matches a `GROUP <Name>` header in paket.lock (groups are
// upper-cased GROUP in the lock; the implicit Main group has no header).
var paketLockGroupRE = regexp.MustCompile(`(?i)^GROUP\s+([A-Za-z0-9_.-]+)\s*$`)

// paketLockSectionRE matches a top-level resolver section header (NUGET, GITHUB,
// GIT, HTTP) which begins at column 0. Only NUGET packages carry a paket NuGet
// coordinate; GITHUB/GIT/HTTP file references are skipped (honest-partial).
var paketLockSectionRE = regexp.MustCompile(`^([A-Z]+)\b`)

// paketLockPkgRE matches a resolved package line `<PackageId> (<version>)`.
// Group 1 is the id, group 2 the resolved version. The line is indented beneath
// a `remote:` line; transitive constraint lines (`Name (>= x)`) match too but
// are de-duplicated by name (the resolved top-level entry, seen first, wins).
var paketLockPkgRE = regexp.MustCompile(
	`^[ \t]+([A-Za-z0-9_.][A-Za-z0-9_.-]*)\s+\(([^)]*)\)\s*$`)

// parsePaketLock parses a paket.lock lockfile, emitting one kind="locked" dep
// per resolved NUGET package. Packages under a `GROUP Test`/`GROUP Build`
// header are flagged is_dev=true. Non-NUGET sections (GITHUB/GIT/HTTP) and the
// `remote:`/`specs:` directive lines are skipped.
func parsePaketLock(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	currentDev := false
	inNuget := false

	for _, line := range strings.Split(source, "\n") {
		if line == "" {
			continue
		}
		// A column-0 line is a section/group header.
		if line[0] != ' ' && line[0] != '\t' {
			if gm := paketLockGroupRE.FindStringSubmatch(line); gm != nil {
				currentDev = paketDevGroup(gm[1])
				inNuget = false
				continue
			}
			if sm := paketLockSectionRE.FindStringSubmatch(line); sm != nil {
				inNuget = sm[1] == "NUGET"
				continue
			}
			inNuget = false
			continue
		}
		if !inNuget {
			continue
		}
		m := paketLockPkgRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		ver := strings.TrimSpace(m[2])
		// A transitive constraint line carries a constraint operator (>= / ~> /
		// < / =) rather than a bare resolved version; a name already seen at its
		// resolved top-level entry is skipped. We also skip a constraint line for
		// a not-yet-seen name (a dep resolved in another group) — its own
		// resolved entry will be (or was) emitted under that group.
		if seen[name] {
			continue
		}
		if strings.ContainsAny(ver, "<>=~") {
			// Constraint-only line for a package whose resolved entry we have not
			// recorded yet — do not record a constraint as a resolved version.
			continue
		}
		seen[name] = true
		kind := "locked"
		out = append(out, dep{
			name:    name,
			version: ver,
			isDev:   currentDev,
			kind:    kind,
		})
	}
	return out
}
