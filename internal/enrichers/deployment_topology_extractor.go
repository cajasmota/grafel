package enrichers

// DeploymentTopologyExtractor detects reverse proxy and container config signals.
// Port of Python deployment_topology_extractor.py (MX-587).

import (
	"path/filepath"
	"regexp"
	"strings"
)

// DeploymentTopologyEntry is one extracted topology signal.
type DeploymentTopologyEntry struct {
	Layer    string
	Kind     string
	Name     string
	Target   string
	FilePath string
}

var (
	nginxLocationRe  = regexp.MustCompile(`(?m)location\s+(?:=\s+|~\*?\s+|)([^\s{]+)\s*\{`)
	nginxProxyPassRe = regexp.MustCompile(`(?m)proxy_pass\s+https?://([a-zA-Z0-9_.:-]+)`)
	caddyRevProxyRe  = regexp.MustCompile(`(?m)reverse_proxy\s+(/\S+)\s+(\S+)`)
	caddyRevSimpleRe = regexp.MustCompile(`(?m)reverse_proxy\s+([a-zA-Z0-9_.:-]+:\d+)`)
	composeServiceRe = regexp.MustCompile(`(?m)^\s{2}([a-zA-Z0-9_-]+)\s*:`)
	k8sIngressPathRe = regexp.MustCompile(`(?m)path:\s*([^\s]+)`)
	kongRouteRe      = regexp.MustCompile(`(?m)paths:\s*\n\s*-\s*([^\s]+)`)
	serverlessFnRe   = regexp.MustCompile(`(?m)^\s{2}([a-zA-Z0-9_-]+)\s*:\s*$`)
	serverlessPathRe = regexp.MustCompile(`path:\s*([^\s]+)`)
)

var gatedTopoBasenames = map[string]bool{
	"nginx.conf":          true,
	"caddyfile":           true,
	"docker-compose.yml":  true,
	"docker-compose.yaml": true,
	"kong.yml":            true,
	"kong.yaml":           true,
	"serverless.yml":      true,
	"kubernetes.yml":      true,
	"kubernetes.yaml":     true,
	"ingress.yml":         true,
	"ingress.yaml":        true,
}

var composeOverrideRe = regexp.MustCompile(`(?i)^docker-compose\.[a-z0-9_-]+\.ya?ml$`)

var skipComposeKeys = map[string]bool{
	"services": true, "version": true, "networks": true, "volumes": true,
}
var skipServerlessKeys = map[string]bool{
	"functions": true, "provider": true, "plugins": true, "custom": true,
}

// DeploymentTopologyAppliesToFile returns true when the file should be processed.
func DeploymentTopologyAppliesToFile(filePath string) bool {
	base := strings.ToLower(filepath.Base(filePath))
	if gatedTopoBasenames[base] || composeOverrideRe.MatchString(base) || strings.HasSuffix(base, ".nginx") {
		return true
	}
	norm := strings.ReplaceAll(filePath, "\\", "/")
	return strings.Contains(norm, "/k8s/") || strings.HasPrefix(norm, "k8s/")
}

// ExtractDeploymentTopology parses infra config and returns topology entries.
func ExtractDeploymentTopology(source, filePath string) []DeploymentTopologyEntry {
	if source == "" {
		return nil
	}
	base := strings.ToLower(filepath.Base(filePath))
	switch {
	case base == "nginx.conf" || strings.HasSuffix(base, ".nginx"):
		return extractNginx(source, filePath)
	case base == "caddyfile":
		return extractCaddy(source, filePath)
	case base == "docker-compose.yml" || base == "docker-compose.yaml" || composeOverrideRe.MatchString(base):
		return extractDockerCompose(source, filePath)
	case base == "kong.yml" || base == "kong.yaml":
		return extractKong(source, filePath)
	case base == "serverless.yml":
		return extractServerless(source, filePath)
	case base == "kubernetes.yml" || base == "kubernetes.yaml" ||
		base == "ingress.yml" || base == "ingress.yaml" ||
		strings.Contains(strings.ReplaceAll(filePath, "\\", "/"), "/k8s/"):
		return extractKubernetes(source, filePath)
	}
	return nil
}

func extractNginx(source, filePath string) []DeploymentTopologyEntry {
	var entries []DeploymentTopologyEntry
	proxyTarget := ""
	if m := nginxProxyPassRe.FindStringSubmatch(source); m != nil {
		proxyTarget = m[1]
	}
	for _, m := range nginxLocationRe.FindAllStringSubmatch(source, -1) {
		entries = append(entries, DeploymentTopologyEntry{
			Layer: "reverse_proxy", Kind: "nginx_location", Name: m[1], Target: proxyTarget, FilePath: filePath,
		})
	}
	return entries
}

func extractCaddy(source, filePath string) []DeploymentTopologyEntry {
	var entries []DeploymentTopologyEntry
	for _, m := range caddyRevProxyRe.FindAllStringSubmatch(source, -1) {
		entries = append(entries, DeploymentTopologyEntry{
			Layer: "reverse_proxy", Kind: "caddy_reverse_proxy", Name: m[1], Target: m[2], FilePath: filePath,
		})
	}
	for _, m := range caddyRevSimpleRe.FindAllStringSubmatch(source, -1) {
		entries = append(entries, DeploymentTopologyEntry{
			Layer: "reverse_proxy", Kind: "caddy_reverse_proxy", Name: m[1], FilePath: filePath,
		})
	}
	return entries
}

func extractDockerCompose(source, filePath string) []DeploymentTopologyEntry {
	var entries []DeploymentTopologyEntry
	for _, m := range composeServiceRe.FindAllStringSubmatch(source, -1) {
		name := m[1]
		if skipComposeKeys[name] {
			continue
		}
		entries = append(entries, DeploymentTopologyEntry{
			Layer: "container", Kind: "docker_service", Name: name, FilePath: filePath,
		})
	}
	return entries
}

func extractKong(source, filePath string) []DeploymentTopologyEntry {
	var entries []DeploymentTopologyEntry
	for _, m := range kongRouteRe.FindAllStringSubmatch(source, -1) {
		entries = append(entries, DeploymentTopologyEntry{
			Layer: "api_gateway", Kind: "kong_route", Name: m[1], FilePath: filePath,
		})
	}
	return entries
}

func extractServerless(source, filePath string) []DeploymentTopologyEntry {
	var entries []DeploymentTopologyEntry
	for _, m := range serverlessFnRe.FindAllStringSubmatch(source, -1) {
		if skipServerlessKeys[m[1]] {
			continue
		}
		entries = append(entries, DeploymentTopologyEntry{
			Layer: "api_gateway", Kind: "serverless_function", Name: m[1], FilePath: filePath,
		})
	}
	for _, m := range serverlessPathRe.FindAllStringSubmatch(source, -1) {
		entries = append(entries, DeploymentTopologyEntry{
			Layer: "api_gateway", Kind: "serverless_http_path", Name: m[1], FilePath: filePath,
		})
	}
	return entries
}

func extractKubernetes(source, filePath string) []DeploymentTopologyEntry {
	var entries []DeploymentTopologyEntry
	for _, m := range k8sIngressPathRe.FindAllStringSubmatch(source, -1) {
		entries = append(entries, DeploymentTopologyEntry{
			Layer: "orchestrator", Kind: "k8s_ingress_path", Name: m[1], FilePath: filePath,
		})
	}
	return entries
}
