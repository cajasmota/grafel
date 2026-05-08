package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// dockerComposeExtractor extracts Docker Compose service definitions.
// Matches Python docker_compose_extractor.py.
type dockerComposeExtractor struct{}

var (
	dcTopKeyRE  = regexp.MustCompile(`(?m)^([A-Za-z_][\w\-]*)\s*:`)
	dcVersionRE = regexp.MustCompile(`(?m)^version\s*:\s*["']?(\d)`)
)

func (d *dockerComposeExtractor) Category() string { return "docker_compose" }

func (d *dockerComposeExtractor) AppliesTo(src string) bool {
	return dcTopKeyRE.MatchString(src) &&
		(strings.Contains(src, "services:") || strings.Contains(src, "version:"))
}

func (d *dockerComposeExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	if !strings.Contains(src, "services:") {
		return nil
	}

	var results []types.EntityRecord
	seen := map[string]bool{}

	version := "unknown"
	if vm := dcVersionRE.FindStringSubmatch(src); vm != nil {
		version = vm[1]
	}

	// Extract service names (2-space indented keys after "services:")
	inServices := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "services:" {
			inServices = true
			continue
		}
		if inServices {
			if len(line) >= 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' {
				// Service name line
				svcName := strings.TrimSuffix(strings.TrimSpace(line), ":")
				if svcName == "" || strings.HasPrefix(svcName, "#") {
					continue
				}
				key := "service:" + svcName
				if seen[key] {
					continue
				}
				seen[key] = true

				props := map[string]string{
					"kind":            "docker_compose",
					"service_name":    svcName,
					"compose_version": version,
				}

				// Try to get image for this service
				results = append(results, makeEntity(filePath,
					"docker_svc_"+svcName, "SCOPE.Service", "docker_service", language, 1,
					props))
			} else if len(line) > 0 && line[0] != ' ' {
				// Left-margin = new top-level key, services block ended
				inServices = false
			}
		}
	}

	return results
}

func init() {
	Register(&dockerComposeExtractor{})
}
