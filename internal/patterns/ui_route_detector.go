package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// uiRouteDetector detects UI routing definitions (React Router, Vue Router, Angular).
// Matches Python ui_route_detector.py.
type uiRouteDetector struct{}

var (
	uiReactRouteRE     = regexp.MustCompile(`<Route\s+(?:[^>]*\s+)?path\s*=\s*["']([^"']+)["']`)
	uiReactComponentRE = regexp.MustCompile(`(?:component|element)\s*=\s*\{?\s*<?([A-Z][A-Za-z0-9_]*)`)
	uiVueRouteRE       = regexp.MustCompile(`path\s*:\s*["']([^"']+)["']`)
uiAngularRouteRE   = regexp.MustCompile(`path\s*:\s*['"]([^'"]+)['"]`)
uiNextPageRE       = regexp.MustCompile(`(?:^|/)pages/([^/]+(?:/[^/]+)*)\.(?:tsx?|jsx?)$`)
	uiNextAppRouterRE  = regexp.MustCompile(`(?:^|/)app/([^/]+(?:/[^/]+)*)/page\.(?:tsx?|jsx?)$`)
)

func (u *uiRouteDetector) Category() string { return "ui_route" }

func (u *uiRouteDetector) AppliesTo(src string) bool {
	return uiReactRouteRE.MatchString(src) ||
		(uiVueRouteRE.MatchString(src) && strings.Contains(src, "component")) ||
		(uiAngularRouteRE.MatchString(src) && strings.Contains(src, "component")) ||
		uiNextPageRE.MatchString(src) ||
		uiNextAppRouterRE.MatchString(src)
}

func (u *uiRouteDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, routePath, component, framework string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("ui_route_%s_%s", framework, strings.ReplaceAll(routePath, "/", "_")),
			"SCOPE.Operation", "ui_route", language, line,
			map[string]string{
				"kind":      "ui_route",
				"path":      routePath,
				"component": component,
				"framework": framework,
			}))
	}

	// React Router
	for _, m := range uiReactRouteRE.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		component := ""
		// Try to find component in same tag context (next 100 chars)
		end := m[1]
		if end+100 < len(src) {
			end += 100
		} else {
			end = len(src)
		}
		if cm := uiReactComponentRE.FindStringSubmatch(src[m[0]:end]); cm != nil {
			component = cm[1]
		}
		emit("react:"+path, path, component, "react-router", lineOf(src, m[0]))
	}

	// Vue Router (path: "/..." in routes array)
	if strings.Contains(src, "createRouter") || strings.Contains(src, "VueRouter") {
		for _, m := range uiVueRouteRE.FindAllStringSubmatchIndex(src, -1) {
			path := src[m[2]:m[3]]
			if !strings.HasPrefix(path, "/") && path != "" && path[0] != ':' {
				continue
			}
			key := "vue:" + path
			emit(key, path, "", "vue-router", lineOf(src, m[0]))
		}
	}

	// Next.js pages directory
	if m := uiNextPageRE.FindStringSubmatch(filePath); m != nil {
		pagePath := "/" + strings.TrimSuffix(m[1], "/index")
		emit("next:page:"+pagePath, pagePath, "", "nextjs-pages", 1)
	}

	// Next.js app directory
	if m := uiNextAppRouterRE.FindStringSubmatch(filePath); m != nil {
		pagePath := "/" + m[1]
		emit("next:app:"+pagePath, pagePath, "", "nextjs-app-router", 1)
	}

	return results
}

func init() {
	Register(&uiRouteDetector{})
}
