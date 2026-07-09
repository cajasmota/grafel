// http_endpoint_csharp_abp.go — ABP conventional application-service routes.
//
// ABP's ConventionalControllers.Create(...) exposes application services as
// ASP.NET Core controllers by convention. These routes do not have controller
// source files or [HttpGet]/[Route] attributes, so synthesizeASPNetCore cannot
// see them. This pass emits the same canonical http_endpoint_definition shape
// as attribute-routed controllers, attributed to the AppService method entity.
package engine

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
)

var abpAppServiceClassRe = regexp.MustCompile(
	`(?s)(?:^|\n)\s*(?:\[[^\]]+\]\s*)*` +
		`(?:public|internal|sealed|partial|\s)+class\s+([A-Za-z_]\w*AppService)\s*:\s*([^{]+)\{`,
)

var abpPublicMethodRe = regexp.MustCompile(
	`(?m)^\s*public\s+(?:virtual\s+|override\s+|async\s+|new\s+|sealed\s+)*` +
		`[\w<>\[\],.? \t]+\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*)\)`,
)

func synthesizeABPConventionalControllers(content string, emit emitFn) {
	if !abpHasSignal(content) {
		return
	}
	m := abpAppServiceClassRe.FindStringSubmatchIndex(content)
	if len(m) < 6 {
		return
	}
	className := content[m[2]:m[3]]
	bases := content[m[4]:m[5]]
	if !abpIsApplicationService(className, bases) {
		return
	}

	basePath := "/api/app/" + abpKebab(strings.TrimSuffix(className, "AppService"))
	if basePath == "/api/app/" {
		return
	}

	if strings.Contains(bases, "CrudAppService<") || strings.Contains(bases, "AbstractKeyCrudAppService<") {
		emit("GET", canonicalABPPath(basePath), "abp_conventional", "SCOPE.Operation", className+".GetListAsync")
		emit("GET", canonicalABPPath(basePath+"/{id}"), "abp_conventional", "SCOPE.Operation", className+".GetAsync")
		emit("POST", canonicalABPPath(basePath), "abp_conventional", "SCOPE.Operation", className+".CreateAsync")
		emit("PUT", canonicalABPPath(basePath+"/{id}"), "abp_conventional", "SCOPE.Operation", className+".UpdateAsync")
		emit("DELETE", canonicalABPPath(basePath+"/{id}"), "abp_conventional", "SCOPE.Operation", className+".DeleteAsync")
	}

	body := content[m[1]:]
	for _, mm := range abpPublicMethodRe.FindAllStringSubmatch(body, -1) {
		if len(mm) < 3 {
			continue
		}
		methodName := mm[1]
		if methodName == className || abpIsInfrastructureMethod(methodName) {
			continue
		}
		verb, path := abpRouteForMethod(basePath, methodName, mm[2])
		if path == "" {
			continue
		}
		emit(verb, canonicalABPPath(path), "abp_conventional", "SCOPE.Operation", className+"."+methodName)
	}
}

func abpHasSignal(content string) bool {
	return strings.Contains(content, "AppService") &&
		(strings.Contains(content, "ApplicationService") ||
			strings.Contains(content, "CrudAppService") ||
			strings.Contains(content, "IApplicationService") ||
			strings.Contains(content, "Volo.Abp.Application.Services"))
}

func abpIsApplicationService(className, bases string) bool {
	if strings.Contains(bases, "ApplicationService") || strings.Contains(bases, "CrudAppService") {
		return true
	}
	return strings.Contains(bases, "I"+className)
}

func abpIsInfrastructureMethod(name string) bool {
	switch name {
	case "CheckCreatePolicyAsync", "CheckUpdatePolicyAsync", "CheckDeletePolicyAsync", "MapToEntityAsync", "MapToGetListOutputDtoAsync":
		return true
	default:
		return false
	}
}

func abpRouteForMethod(basePath, methodName, params string) (string, string) {
	name := strings.TrimSuffix(methodName, "Async")
	verb := "POST"
	action := name
	idSuffix := false

	switch {
	case strings.HasPrefix(name, "GetList"):
		verb = "GET"
		action = strings.TrimPrefix(name, "GetList")
	case strings.HasPrefix(name, "GetAll"):
		verb = "GET"
		action = strings.TrimPrefix(name, "GetAll")
	case strings.HasPrefix(name, "Get"):
		verb = "GET"
		action = strings.TrimPrefix(name, "Get")
		idSuffix = action == "" && abpHasIDParam(params)
	case strings.HasPrefix(name, "Create"):
		verb = "POST"
		action = strings.TrimPrefix(name, "Create")
	case strings.HasPrefix(name, "Update"):
		verb = "PUT"
		action = strings.TrimPrefix(name, "Update")
		idSuffix = action == "" && abpHasIDParam(params)
	case strings.HasPrefix(name, "Delete"):
		verb = "DELETE"
		action = strings.TrimPrefix(name, "Delete")
		idSuffix = action == "" && abpHasIDParam(params)
	case strings.HasPrefix(name, "Remove"):
		verb = "DELETE"
		action = strings.TrimPrefix(name, "Remove")
	}

	path := basePath
	if idSuffix {
		path += "/{id}"
	}
	if action != "" {
		path += "/" + abpKebab(action)
	}
	return verb, path
}

func abpHasIDParam(params string) bool {
	for _, part := range strings.Split(params, ",") {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "@")
		name = strings.Trim(name, " \t\r\n")
		if strings.EqualFold(name, "id") {
			return true
		}
	}
	return false
}

func canonicalABPPath(path string) string {
	return httproutes.Canonicalize(httproutes.FrameworkASPNetCore, path)
}

func abpKebab(s string) string {
	var b strings.Builder
	var prevLowerOrDigit bool
	for i, r := range s {
		if r == '_' || r == '-' || unicode.IsSpace(r) {
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
			prevLowerOrDigit = false
			continue
		}
		if unicode.IsUpper(r) {
			if i > 0 && prevLowerOrDigit {
				b.WriteByte('-')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
		prevLowerOrDigit = unicode.IsLower(r) || unicode.IsDigit(r)
	}
	return strings.Trim(b.String(), "-")
}
