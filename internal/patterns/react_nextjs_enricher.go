package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// reactNextJSEnricher detects React/Next.js component patterns.
// Matches Python react_nextjs_enricher.py.
type reactNextJSEnricher struct{}

var (
	rnUseClientRE    = regexp.MustCompile(`(?m)^['"]use client['"]`)
	rnUseServerRE    = regexp.MustCompile(`(?m)['"]use server['"]`)
	rnHookRE         = regexp.MustCompile(`\b(use[A-Z]\w*)\s*\(`)
	rnJSXHandlerRE     = regexp.MustCompile(`on[A-Z]\w+\s*=\s*\{`)
	rnNextPageRE       = regexp.MustCompile(`export\s+(?:default\s+)?function\s+(?:Page|Home|Index|App|\w+Page)\s*\(`)
	rnGetServerPropsRE = regexp.MustCompile(`export\s+(?:async\s+)?function\s+getServerSideProps\s*\(`)
	rnGetStaticPropsRE = regexp.MustCompile(`export\s+(?:async\s+)?function\s+getStaticProps\s*\(`)
)

func (r *reactNextJSEnricher) Category() string { return "react_component_enrichment" }

func (r *reactNextJSEnricher) AppliesTo(src string) bool {
	return rnUseClientRE.MatchString(src) ||
		rnHookRE.MatchString(src) ||
		rnJSXHandlerRE.MatchString(src) ||
		rnNextPageRE.MatchString(src) ||
		rnGetServerPropsRE.MatchString(src)
}

func (r *reactNextJSEnricher) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, subtype string, line int, props map[string]string) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Component", subtype, language, line, props))
	}

	// Client component directive
	if m := rnUseClientRE.FindStringIndex(src); m != nil {
		emit("use_client", "react_client_component", "client_component", lineOf(src, m[0]),
			map[string]string{"kind": "react_component_enrichment", "component_type": "client"})
	}

	// Server component / action
	if m := rnUseServerRE.FindStringIndex(src); m != nil {
		emit("use_server", "react_server_component", "server_component", lineOf(src, m[0]),
			map[string]string{"kind": "react_component_enrichment", "component_type": "server"})
	}

	// React hooks used
	hookSeen := map[string]bool{}
	for _, m := range rnHookRE.FindAllStringSubmatchIndex(src, -1) {
		hook := src[m[2]:m[3]]
		if hookSeen[hook] {
			continue
		}
		hookSeen[hook] = true
		emit("hook:"+hook, fmt.Sprintf("react_hook_%s", hook), "react_hook", lineOf(src, m[0]),
			map[string]string{"kind": "react_component_enrichment", "hook_name": hook})
	}

	// Next.js getServerSideProps
	if m := rnGetServerPropsRE.FindStringIndex(src); m != nil {
		emit("next:getSSP", "next_get_server_side_props", "next_ssr", lineOf(src, m[0]),
			map[string]string{"kind": "react_component_enrichment", "next_pattern": "getServerSideProps"})
	}

	// Next.js getStaticProps
	if m := rnGetStaticPropsRE.FindStringIndex(src); m != nil {
		emit("next:getStaticProps", "next_get_static_props", "next_ssg", lineOf(src, m[0]),
			map[string]string{"kind": "react_component_enrichment", "next_pattern": "getStaticProps"})
	}

	return results
}

func init() {
	Register(&reactNextJSEnricher{})
}
