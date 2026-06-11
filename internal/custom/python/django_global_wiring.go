package python

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #4379 — Django global cross-cutting wiring.
//
// Django registers cross-cutting behaviour application-wide through *settings*,
// as ordered lists/tuples of dotted string paths:
//
//	MIDDLEWARE = ['django.middleware.security.SecurityMiddleware',
//	              'core.middleware.token_authentication.TokenAuthenticationMiddleware', ...]
//	AUTHENTICATION_BACKENDS = ['django.contrib.auth.backends.ModelBackend', ...]
//	REST_FRAMEWORK = {
//	    'DEFAULT_AUTHENTICATION_CLASSES': (... dotted paths ...),
//	    'DEFAULT_PERMISSION_CLASSES':     [... dotted paths ...],
//	    'DEFAULT_RENDERER_CLASSES':       (... dotted paths ...),
//	}
//
// Each dotted path binds a real middleware / authentication / permission /
// renderer / backend class app-wide, but the binding is a *string*, so before
// this fix it produced no edge at all: the referenced class had no inbound
// edge from any source entity and looked orphan / dead, and the global scope
// was invisible to the graph.
//
// This mirrors the NestJS global-DI fix (#4329): emit a synthetic
// `django_settings` application entity that owns one USES edge per bound class,
// marked global=true with a di_role and (for MIDDLEWARE) an order index. The
// edge ToID is the *full dotted path*, which resolves structurally through the
// real resolver's QualifiedName index (a Python class in
// core/middleware/token_authentication.py is emitted with QualifiedName
// "core.middleware.token_authentication.TokenAuthenticationMiddleware") — the
// same sys.path-rooted dotted name Django settings use — so the previously
// orphan class is connected merge-stably.

const djangoSettingsEntityName = "django_settings"

var (
	// Top-level settings-list assignments of dotted-path classes. Each matches
	// the assignment head; the list/tuple body is read with a balanced-bracket
	// scan from the captured opening delimiter so nested parens/brackets and
	// trailing comments do not truncate it. group 1 = opening delimiter.
	djangoAuthBackendsSettingRe = regexp.MustCompile(`(?m)^AUTHENTICATION_BACKENDS\s*(?:\+?=)\s*([\[(])`)

	// REST_FRAMEWORK default-class buckets. The value may be a [...] list OR a
	// (...) tuple (both are idiomatic), so the opening delimiter is captured
	// (group 1) and the body read by balanced scan — the legacy
	// djangoDRFAuthClassesRe only handled [...] and silently missed the very
	// common tuple form (e.g. the real upvate_core settings).
	djangoDRFDefaultClassKeyRe = regexp.MustCompile(
		`["'](DEFAULT_AUTHENTICATION_CLASSES|DEFAULT_PERMISSION_CLASSES|DEFAULT_RENDERER_CLASSES)["']\s*:\s*([\[(])`)

	// A single dotted-path string literal item inside a settings list/tuple.
	djangoDottedPathItemRe = regexp.MustCompile(`["']([A-Za-z_][\w.]*\.[A-Za-z_]\w*)["']`)

	// Issue #4403 — TEMPLATES[...]['OPTIONS']['context_processors'] = [dotted, …].
	// The TEMPLATES setting is a list of backend dicts; each may carry an
	// OPTIONS.context_processors list of dotted-path callables bound app-wide.
	// We locate the `"context_processors": [ … ]` key (single or double quoted)
	// anywhere inside the TEMPLATES block and read its balanced list body.
	djangoTemplatesSettingRe     = regexp.MustCompile(`(?m)^TEMPLATES\s*(?:\+?=)\s*([\[(])`)
	djangoContextProcessorsKeyRe = regexp.MustCompile(`["']context_processors["']\s*:\s*([\[(])`)

	// Issue #4403 — INSTALLED_APPS = [ "app", "app.apps.AppConfig", … ]. Entries
	// are either a bare app-package label ("rest_framework") or a dotted path to
	// an AppConfig subclass ("core.apps.CoreConfig"). Only the dotted-path
	// AppConfig form resolves to a real class entity, so we wire those; bare
	// labels carry no in-repo target and are skipped.
	djangoInstalledAppsSettingRe = regexp.MustCompile(`(?m)^INSTALLED_APPS\s*(?:\+?=)\s*([\[(])`)
)

// djangoDRFKeyRole maps a REST_FRAMEWORK default-class bucket key to the
// di_role of the classes it binds app-wide.
var djangoDRFKeyRole = map[string]string{
	"DEFAULT_AUTHENTICATION_CLASSES": "authentication",
	"DEFAULT_PERMISSION_CLASSES":     "permission",
	"DEFAULT_RENDERER_CLASSES":       "renderer",
}

// extractDjangoGlobalWiring scans a Django settings module for global
// cross-cutting wiring (MIDDLEWARE, AUTHENTICATION_BACKENDS, and the DRF
// REST_FRAMEWORK DEFAULT_*_CLASSES buckets) and returns a synthetic
// `django_settings` entity owning one global USES edge per bound dotted-path
// class. Returns nil when the file declares no such wiring (so non-settings
// modules emit nothing).
//
// The edge ToID is the verbatim dotted path; it resolves through the resolver's
// QualifiedName index to the real class entity. Ordering (MIDDLEWARE) is
// preserved via the "order" property. Each class is bound once per role
// (deduped) so a class listed twice does not double-emit.
func extractDjangoGlobalWiring(source, filePath string, line int) *types.EntityRecord {
	var rels []types.RelationshipRecord
	seen := map[string]bool{} // role|dottedPath → emitted

	add := func(role, dottedPath string, order int) {
		dottedPath = strings.TrimSpace(dottedPath)
		if dottedPath == "" {
			return
		}
		key := role + "|" + dottedPath
		if seen[key] {
			return
		}
		seen[key] = true
		props := map[string]string{
			"framework":   "django",
			"language":    "python",
			"di_role":     role,
			"di_scope":    "global",
			"global":      "true",
			"dotted_path": dottedPath,
			"class_name":  djangoDottedLeaf(dottedPath),
			"provenance":  "INFERRED_FROM_DJANGO_SETTINGS",
		}
		if order >= 0 {
			props["order"] = strconv.Itoa(order)
		}
		rels = append(rels, types.RelationshipRecord{
			FromID:     "", // defaults to the owning django_settings entity ID
			ToID:       dottedPath,
			Kind:       string(types.RelationshipKindUses),
			Properties: props,
		})
	}

	// MIDDLEWARE — ordered list of dotted-path middleware classes.
	for _, dp := range djangoSettingsListPaths(source, djangoMiddlewareSettingRe, '[') {
		// order is the index within the concatenation of all MIDDLEWARE
		// assignments encountered, preserving request/response ordering.
		add("middleware", dp, len(rels))
	}

	// AUTHENTICATION_BACKENDS — ordered list/tuple of backend dotted paths.
	for _, dp := range djangoSettingsContainerPaths(source, djangoAuthBackendsSettingRe) {
		add("backend", dp, -1)
	}

	// REST_FRAMEWORK DEFAULT_*_CLASSES buckets (authentication / permission /
	// renderer). The value container may be a list OR a tuple.
	if rfIdx := djangoDRFRestFrameworkRe.FindStringSubmatchIndex(source); rfIdx != nil {
		rfBlock := source[rfIdx[2]:rfIdx[3]]
		for _, m := range djangoDRFDefaultClassKeyRe.FindAllStringSubmatchIndex(rfBlock, -1) {
			key := rfBlock[m[2]:m[3]]
			open := m[5] - 1 // index of captured opening delimiter
			body := djangoStripPyComments(djangoBalancedDelim(rfBlock, open))
			role := djangoDRFKeyRole[key]
			for _, im := range djangoDottedPathItemRe.FindAllStringSubmatch(body, -1) {
				add(role, im[1], -1)
			}
		}
	}

	// TEMPLATES[...]['OPTIONS']['context_processors'] — dotted-path callables
	// bound app-wide for template rendering (issue #4403). Each TEMPLATES
	// backend dict may declare its own context_processors list; we scan every
	// "context_processors": [...] / (...) key found inside the TEMPLATES block.
	for _, m := range djangoTemplatesSettingRe.FindAllStringSubmatchIndex(source, -1) {
		open := m[len(m)-2] // captured opening delimiter of the TEMPLATES container
		tplBlock := djangoBalancedDelim(source, open)
		for _, km := range djangoContextProcessorsKeyRe.FindAllStringSubmatchIndex(tplBlock, -1) {
			cpOpen := km[len(km)-2] // opening delimiter of the context_processors list
			body := djangoStripPyComments(djangoBalancedDelim(tplBlock, cpOpen))
			for _, im := range djangoDottedPathItemRe.FindAllStringSubmatch(body, -1) {
				add("context_processor", im[1], -1)
			}
		}
	}

	// INSTALLED_APPS — dotted-path AppConfig entries bound app-wide (issue
	// #4403). Bare package labels (e.g. "rest_framework") carry no in-repo
	// target and are skipped by djangoDottedPathItemRe (which requires at least
	// one interior dot); the "core.apps.CoreConfig" form resolves to the real
	// AppConfig subclass via the QualifiedName index.
	for _, m := range djangoInstalledAppsSettingRe.FindAllStringSubmatchIndex(source, -1) {
		open := m[len(m)-2]
		body := djangoStripPyComments(djangoBalancedDelim(source, open))
		for _, im := range djangoDottedPathItemRe.FindAllStringSubmatch(body, -1) {
			add("app_config", im[1], -1)
		}
	}

	if len(rels) == 0 {
		return nil
	}

	ent := entity(djangoSettingsEntityName, "SCOPE.Pattern", "application", filePath, line,
		map[string]string{
			"framework":  "django",
			"language":   "python",
			"provenance": "INFERRED_FROM_DJANGO_SETTINGS",
		})
	ent.EnrichmentRequired = false
	ent.Relationships = rels
	return &ent
}

// djangoStripPyComments removes Python `#` line comments from a settings-list
// body so commented-out entries (e.g. `# "app.middleware.LoggingMiddleware"`)
// are not mistaken for live wiring. A `#` is treated as a comment start only
// when it is outside a string literal on that line; everything from there to
// end-of-line is dropped. This preserves quoted dotted-path items while
// discarding commented ones.
func djangoStripPyComments(body string) string {
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		var quote byte // 0, '\'' or '"'
		cut := len(line)
		for i := 0; i < len(line); i++ {
			c := line[i]
			switch {
			case quote != 0:
				if c == quote {
					quote = 0
				}
			case c == '\'' || c == '"':
				quote = c
			case c == '#':
				cut = i
			}
			if cut != len(line) {
				break
			}
		}
		b.WriteString(line[:cut])
		b.WriteByte('\n')
	}
	return b.String()
}

// djangoSettingsListPaths returns the ordered dotted-path string items found in
// every top-level list assignment matched by headRe (whose match end is the
// position just past the opening `open` bracket). Used for MIDDLEWARE, which is
// always a [...] list and may appear in multiple `MIDDLEWARE += [...]` blocks.
func djangoSettingsListPaths(source string, headRe *regexp.Regexp, open byte) []string {
	var out []string
	for _, loc := range headRe.FindAllStringIndex(source, -1) {
		openPos := loc[1] - 1 // position of the opening bracket
		if openPos < 0 || openPos >= len(source) || source[openPos] != open {
			continue
		}
		body := djangoStripPyComments(extractBalancedBrackets(source, openPos))
		for _, m := range djangoDottedPathItemRe.FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// djangoSettingsContainerPaths returns the dotted-path items inside the
// list/tuple matched by headRe, whose final capture group is the opening
// delimiter (`[` or `(`). Handles both AUTHENTICATION_BACKENDS = [...] and
// = (...) forms.
func djangoSettingsContainerPaths(source string, headRe *regexp.Regexp) []string {
	var out []string
	for _, m := range headRe.FindAllStringSubmatchIndex(source, -1) {
		open := m[len(m)-2] // start index of last capture group (the delimiter)
		body := djangoStripPyComments(djangoBalancedDelim(source, open))
		for _, im := range djangoDottedPathItemRe.FindAllStringSubmatch(body, -1) {
			out = append(out, im[1])
		}
	}
	return out
}

// djangoBalancedDelim returns the content between a balanced [..] or (..) pair
// whose opening delimiter is at openPos. Returns "" if openPos is not a
// recognised opening delimiter or the pair is unbalanced.
func djangoBalancedDelim(source string, openPos int) string {
	if openPos < 0 || openPos >= len(source) {
		return ""
	}
	var closeCh byte
	switch source[openPos] {
	case '[':
		closeCh = ']'
	case '(':
		closeCh = ')'
	default:
		return ""
	}
	openCh := source[openPos]
	depth := 0
	for i := openPos; i < len(source); i++ {
		switch source[i] {
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return source[openPos+1 : i]
			}
		}
	}
	return ""
}

// djangoDottedLeaf returns the trailing class-name segment of a dotted path
// ("a.b.C" → "C"). Stored on the edge so consumers can match the class without
// re-splitting the path.
func djangoDottedLeaf(dotted string) string {
	if i := strings.LastIndex(dotted, "."); i >= 0 && i+1 < len(dotted) {
		return dotted[i+1:]
	}
	return dotted
}
