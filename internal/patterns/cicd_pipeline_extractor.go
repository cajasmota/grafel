package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// cicdPipelineExtractor detects CI/CD pipeline definitions.
// Matches Python cicd_pipeline_extractor.py.
type cicdPipelineExtractor struct{}

var (
	cicdGHAPathRE      = regexp.MustCompile(`(?i)\.github/workflows/[^/]+\.ya?ml`)
	cicdCircleCIPathRE = regexp.MustCompile(`(?i)\.circleci/config\.ya?ml`)
	cicdGHAJobsSectionRE = regexp.MustCompile(`(?m)^jobs\s*:`)
	cicdGHAJobIDRE      = regexp.MustCompile(`(?m)^  ([A-Za-z0-9_\-][A-Za-z0-9_\-]*)\s*:`)
	cicdGHAOnRE         = regexp.MustCompile(`(?m)^on\s*:\s*(.+)`)
	cicdGHARunsOnRE     = regexp.MustCompile(`(?m)^\s{4}runs-on\s*:\s*(.+)`)
	cicdJenkinsTriggerRE = regexp.MustCompile(`(?:pipeline\s*\{|node\s*\{|stage\s*\()`)
	cicdGitLabTriggerRE  = regexp.MustCompile(`(?m)^(?:stages\s*:|\.gitlab-ci)`)
	cicdCircleCITriggerRE = regexp.MustCompile(`(?m)^(?:version|orbs|workflows)\s*:`)
)

func cicdCategory(filePath, src string) string {
	if cicdGHAPathRE.MatchString(filePath) {
		return "github_actions"
	}
	if strings.Contains(strings.ToLower(filePath), "jenkinsfile") {
		return "jenkinsfile"
	}
	if strings.Contains(strings.ToLower(filePath), ".gitlab-ci") {
		return "gitlab_ci"
	}
	if cicdCircleCIPathRE.MatchString(filePath) {
		return "circleci"
	}
	if cicdJenkinsTriggerRE.MatchString(src) {
		return "jenkinsfile"
	}
	if cicdGitLabTriggerRE.MatchString(src) {
		return "gitlab_ci"
	}
	if cicdCircleCITriggerRE.MatchString(src) {
		return "circleci"
	}
	return "ci_pipeline"
}

func (c *cicdPipelineExtractor) Category() string { return "ci_pipeline" }

func (c *cicdPipelineExtractor) AppliesTo(src string) bool {
	return cicdGHAJobsSectionRE.MatchString(src) ||
		cicdJenkinsTriggerRE.MatchString(src) ||
		cicdGitLabTriggerRE.MatchString(src) ||
		cicdCircleCITriggerRE.MatchString(src)
}

func (c *cicdPipelineExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	cat := cicdCategory(filePath, src)

	// GitHub Actions: extract jobs
	if cat == "github_actions" && cicdGHAJobsSectionRE.MatchString(src) {
		trigger := ""
		if tm := cicdGHAOnRE.FindStringSubmatch(src); tm != nil {
			trigger = strings.TrimSpace(tm[1])
		}
		for idx, m := range cicdGHAJobIDRE.FindAllStringSubmatchIndex(src, -1) {
			jobID := src[m[2]:m[3]]
			runner := "ubuntu-latest"
			if rm := cicdGHARunsOnRE.FindString(src); rm != "" {
				runner = strings.TrimSpace(strings.TrimPrefix(rm, "runs-on:"))
			}
			results = append(results, makeEntity(filePath,
				fmt.Sprintf("gha_job_%s", jobID),
				"SCOPE.Operation", "ci_job", language,
				lineOf(src, m[0]),
				map[string]string{
					"kind":     "ci_pipeline",
					"category": "github_actions",
					"job_id":   jobID,
					"runner":   runner,
					"trigger":  trigger,
					"index":    fmt.Sprintf("%d", idx),
				}))
		}
		return results
	}

	// Generic pipeline entity
	results = append(results, makeEntity(filePath,
		"ci_pipeline_"+cat, "SCOPE.Operation", "ci_pipeline", language, 1,
		map[string]string{"kind": "ci_pipeline", "category": cat}))
	return results
}

func init() {
	Register(&cicdPipelineExtractor{})
}
