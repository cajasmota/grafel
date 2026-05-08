package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// iacDetector detects Infrastructure as Code resource definitions.
// Matches Python iac_detector.py.
type iacDetector struct{}

var (
	iacTFResourceRE = regexp.MustCompile(`(?m)^resource\s+"([^"]+)"\s+"([^"]+)"\s*\{`)
	iacTFModuleRE   = regexp.MustCompile(`(?m)^module\s+"([^"]+)"\s*\{`)
	iacPulumiRE     = regexp.MustCompile(`new\s+(?:aws|azure|gcp|k8s)\.\w+\.(\w+)\s*\(`)
	iacCDKRE        = regexp.MustCompile(`new\s+(?:cdk\.|aws_cdk\.)(?:\w+\.)*(\w+)\s*\(`)
	iacK8sKindRE    = regexp.MustCompile(`(?m)^kind:\s+(\w+)`)
)

func iacResourceKind(resourceType string) string {
	t := strings.ToLower(resourceType)
	switch {
	case strings.Contains(t, "rds") || strings.Contains(t, "database") || strings.Contains(t, "db"):
		return "SCOPE.Datastore"
	case strings.Contains(t, "sqs") || strings.Contains(t, "queue") || strings.Contains(t, "sns") || strings.Contains(t, "kafka"):
		return "SCOPE.Queue"
	default:
		return "SCOPE.Service"
	}
}

func (i *iacDetector) Category() string { return "iac" }

func (i *iacDetector) AppliesTo(src string) bool {
	return iacTFResourceRE.MatchString(src) ||
		iacTFModuleRE.MatchString(src) ||
		iacPulumiRE.MatchString(src) ||
		iacCDKRE.MatchString(src) ||
		iacK8sKindRE.MatchString(src)
}

func (i *iacDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Terraform resource blocks
	for _, m := range iacTFResourceRE.FindAllStringSubmatchIndex(src, -1) {
		resType := src[m[2]:m[3]]
		resName := src[m[4]:m[5]]
		key := fmt.Sprintf("tf:%s:%s", resType, resName)
		if seen[key] {
			continue
		}
		seen[key] = true
		kind := iacResourceKind(resType)
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("iac_%s_%s", resType, resName), kind, "iac_resource", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "iac", "resource_type": resType, "resource_name": resName, "iac_tool": "terraform"}))
	}

	// Terraform module blocks
	for _, m := range iacTFModuleRE.FindAllStringSubmatchIndex(src, -1) {
		modName := src[m[2]:m[3]]
		key := "tf:module:" + modName
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"iac_module_"+modName, "SCOPE.Service", "iac_module", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "iac", "resource_name": modName, "iac_tool": "terraform_module"}))
	}

	// Pulumi resources
	for _, m := range iacPulumiRE.FindAllStringSubmatchIndex(src, -1) {
		resType := src[m[2]:m[3]]
		key := "pulumi:" + resType
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"iac_pulumi_"+resType, "SCOPE.Service", "iac_resource", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "iac", "resource_type": resType, "iac_tool": "pulumi"}))
	}

	// Kubernetes kind:
	for _, m := range iacK8sKindRE.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		key := "k8s:" + kind
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"iac_k8s_"+kind, "SCOPE.Service", "k8s_resource", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "iac", "resource_type": kind, "iac_tool": "kubernetes"}))
	}

	return results
}

func init() {
	Register(&iacDetector{})
}
