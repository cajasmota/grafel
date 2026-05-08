package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// portAggregationExtractor detects port bindings across Dockerfile, Compose, and app code.
// Matches Python port_aggregation_extractor.py.
type portAggregationExtractor struct{}

var (
	paDockerExposeRE       = regexp.MustCompile(`(?im)^\s*EXPOSE\s+(\d+)(?:/(tcp|udp))?`)
	paComposePortRE        = regexp.MustCompile(`(?m)^\s+-\s+"?(\d+):(\d+)"?`)
	paSpringPropertiesRE   = regexp.MustCompile(`server\.port\s*=\s*(\d+)`)
paExpressEnvPortRE     = regexp.MustCompile(`process\.env\.PORT\s*\|\|\s*(\d+)`)
	paAppListenRE          = regexp.MustCompile(`\.listen\s*\(\s*(\d+)`)
	paGoAddrRE             = regexp.MustCompile(`["'` + "`" + `]\s*:(\d{4,5})`)
)

func (p *portAggregationExtractor) Category() string { return "port_aggregation" }

func (p *portAggregationExtractor) AppliesTo(src string) bool {
	return paDockerExposeRE.MatchString(src) ||
		paComposePortRE.MatchString(src) ||
		paSpringPropertiesRE.MatchString(src) ||
		paExpressEnvPortRE.MatchString(src) ||
		paAppListenRE.MatchString(src) ||
		paGoAddrRE.MatchString(src)
}

func (p *portAggregationExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	source := "app"
	switch {
	case strings.Contains(strings.ToLower(filePath), "dockerfile"):
		source = "dockerfile"
	case strings.Contains(strings.ToLower(filePath), "docker-compose"):
		source = "compose"
	case strings.HasSuffix(filePath, ".properties") || strings.HasSuffix(filePath, ".yml"):
		source = "spring"
	}

	emit := func(port, proto, src2 string, line int) {
		key := fmt.Sprintf("%s:%s:%s", port, proto, src2)
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("port_%s_%s", port, src2),
			"SCOPE.Config", "port_binding", language, line,
			map[string]string{"kind": "port_aggregation", "port": port, "protocol": proto, "source": src2}))
	}

	// Dockerfile EXPOSE
	for _, m := range paDockerExposeRE.FindAllStringSubmatchIndex(src, -1) {
		port := src[m[2]:m[3]]
		proto := "tcp"
		if m[4] >= 0 {
			proto = src[m[4]:m[5]]
		}
		emit(port, proto, "dockerfile", lineOf(src, m[0]))
	}

	// Docker Compose ports: - "8080:80"
	for _, m := range paComposePortRE.FindAllStringSubmatchIndex(src, -1) {
		hostPort := src[m[2]:m[3]]
		emit(hostPort, "tcp", "compose", lineOf(src, m[0]))
	}

	// Spring server.port
	if m := paSpringPropertiesRE.FindStringSubmatchIndex(src); m != nil {
		emit(src[m[2]:m[3]], "tcp", "spring", lineOf(src, m[0]))
	}

	// Express process.env.PORT || 3000
	if m := paExpressEnvPortRE.FindStringSubmatchIndex(src); m != nil {
		emit(src[m[2]:m[3]], "tcp", "express", lineOf(src, m[0]))
	}

	// .listen(port)
	for _, m := range paAppListenRE.FindAllStringSubmatchIndex(src, -1) {
		port := src[m[2]:m[3]]
		emit(port, "tcp", source, lineOf(src, m[0]))
	}

	// Go :addr
	if language == "go" {
		for _, m := range paGoAddrRE.FindAllStringSubmatchIndex(src, -1) {
			port := src[m[2]:m[3]]
			emit(port, "tcp", "go", lineOf(src, m[0]))
		}
	}

	return results
}

func init() {
	Register(&portAggregationExtractor{})
}
