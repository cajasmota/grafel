package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// entityVersionTracker detects API versioning patterns.
// Matches Python entity_version_tracker.py.
type entityVersionTracker struct{}

var (
	evtURLVersionRE     = regexp.MustCompile(`/(?:api/)?v(\d+)/`)
	evtHeaderVersionRE  = regexp.MustCompile(`(?i)Accept(?:-Version)?:\s*v?(\d+(?:\.\d+)*)`)
	evtGoModVersionRE   = regexp.MustCompile(`module\s+\S+/v(\d+)`)
	evtPackageVersionRE = regexp.MustCompile(`(?:"version":|version\s*=\s*)["'](\d+\.\d+\.\d+)["']`)
	evtSemverCommentRE  = regexp.MustCompile(`@version\s+(\d+\.\d+\.\d+)`)
	evtAnnotationVerRE  = regexp.MustCompile(`@ApiVersion\s*\(\s*["']?(\d+)["']?\s*\)`)
)

func (e *entityVersionTracker) Category() string { return "entity_version_tracker" }

func (e *entityVersionTracker) AppliesTo(src string) bool {
	return evtURLVersionRE.MatchString(src) ||
		evtHeaderVersionRE.MatchString(src) ||
		evtGoModVersionRE.MatchString(src) ||
		evtPackageVersionRE.MatchString(src) ||
		evtSemverCommentRE.MatchString(src) ||
		evtAnnotationVerRE.MatchString(src)
}

func (e *entityVersionTracker) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	for idx, m := range evtURLVersionRE.FindAllStringSubmatchIndex(src, -1) {
		ver := src[m[2]:m[3]]
		key := fmt.Sprintf("url_version:v%s", ver)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("api_version_v%s_%d", ver, idx),
			"SCOPE.Pattern", "api_version", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "entity_version_tracker", "version": "v" + ver, "version_source": "url_path"}))
	}

	if m := evtGoModVersionRE.FindStringSubmatch(src); m != nil {
		key := "go_module:v" + m[1]
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"go_module_version_v"+m[1], "SCOPE.Pattern", "module_version", language, 1,
				map[string]string{"kind": "entity_version_tracker", "version": "v" + m[1], "version_source": "go_module"}))
		}
	}

	if m := evtPackageVersionRE.FindStringSubmatch(src); m != nil {
		key := "package:" + m[1]
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"package_version_"+m[1], "SCOPE.Pattern", "package_version", language, 1,
				map[string]string{"kind": "entity_version_tracker", "version": m[1], "version_source": "package_json"}))
		}
	}

	for _, m := range evtAnnotationVerRE.FindAllStringSubmatchIndex(src, -1) {
		ver := src[m[2]:m[3]]
		key := "annotation:v" + ver
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"api_annotation_version_v"+ver, "SCOPE.Pattern", "api_version", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "entity_version_tracker", "version": "v" + ver, "version_source": "annotation"}))
	}

	return results
}

func init() {
	Register(&entityVersionTracker{})
}
