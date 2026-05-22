// Django admin URL synthesis pass — Issue #801.
//
// The Django admin (mounted via `path('admin/', admin.site.urls)`) registers
// a family of real HTTP routes per ModelAdmin registration plus a set of
// site-level routes. Archigraph previously extracted zero of these.
//
// This pass synthesizes the standard admin route family by detecting three
// registration patterns in Python source files:
//
//  1. `admin.site.register(Model)` — bare registration
//  2. `@admin.register(Model)` — decorator-based registration
//  3. `class FooAdmin(admin.ModelAdmin)` — class definition (resolved to
//     the model it administers via the register() call or @register decorator)
//
// Per ModelAdmin registration, the following routes are synthesized:
//
//	GET    /admin/<app>/<model>/               changelist
//	GET    /admin/<app>/<model>/add/           add form
//	POST   /admin/<app>/<model>/add/           add submit
//	GET    /admin/<app>/<model>/{id}/change/   change form
//	POST   /admin/<app>/<model>/{id}/change/   change submit
//	GET    /admin/<app>/<model>/{id}/delete/   delete confirm
//	POST   /admin/<app>/<model>/{id}/delete/   delete submit
//	GET    /admin/<app>/<model>/{id}/history/  history
//	GET    /admin/<app>/<model>/autocomplete/  (when search_fields defined)
//
// Per app (one entry for each app that has at least one ModelAdmin):
//
//	GET /admin/<app>/
//
// Site-level (one set per project, deduplicated across files):
//
//	GET  /admin/
//	GET  /admin/login/
//	POST /admin/login/
//	GET  /admin/logout/
//	GET  /admin/password_change/
//	POST /admin/password_change/
//	GET  /admin/jsi18n/
//
// Custom actions on a ModelAdmin (`actions = [myfunc]`) synthesize:
//
//	POST /admin/<app>/<model>/<actionname>/
//
// TabularInline / StackedInline classes don't generate direct routes but
// DO generate autocomplete endpoints for the parent ModelAdmin.
//
// The app name is inferred from the Python package path (e.g. a file at
// `users/admin.py` → app label "users"). The model name is lowercased from
// the class name or register() argument.
//
// All synthetic entities carry:
//
//	framework      = "django_admin"
//	pattern_type   = "django_admin_synthetic"
//	model_class    = "<ClassName>"     (for per-model routes)
//
// Refs: #801
package engine

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Regex definitions
// ---------------------------------------------------------------------------

// adminRegisterRe matches:
//   - admin.site.register(Model)
//   - admin.site.register(Model, AdminClass)
//   - admin.site.register([Model1, Model2], AdminClass)
//
// Group 1: model argument(s) — either a bracketed list or a single identifier.
// Group 2 (optional): explicit admin class name.
//
// The first group deliberately uses non-greedy bracketed-OR-single-ident form to
// avoid capturing the second positional argument (AdminClass) inside group 1.
var adminRegisterRe = regexp.MustCompile(
	`admin\.site\.register\s*\(\s*(\[[\w,\s]+\]|\w+)\s*(?:,\s*(\w+))?\s*\)`,
)

// adminDecoratorRe matches `@admin.register(Model1)` or
// `@admin.register(Model1, Model2)`. Group 1 is the comma-separated model
// list. The decorated class name is captured by the class definition that
// follows (handled by adminClassDefRe applied after locating decorator lines).
var adminDecoratorRe = regexp.MustCompile(
	`@admin\.register\s*\(\s*([\w,\s]+)\s*\)`,
)

// adminClassDefRe matches `class FooAdmin(admin.ModelAdmin)` and variants.
// Group 1: class name. Group 2: base class list.
var adminClassDefRe = regexp.MustCompile(
	`class\s+(\w+)\s*\(([^)]*)\)\s*:`,
)

// adminSearchFieldsRe detects `search_fields` attribute in an admin class
// body. Its presence triggers autocomplete endpoint synthesis.
var adminSearchFieldsRe = regexp.MustCompile(
	`search_fields\s*=`,
)

// adminActionsRe captures `actions = [funcname, "funcname2", ...]` or a
// tuple variant. Group 1: the raw content inside brackets.
var adminActionsRe = regexp.MustCompile(
	`\bactions\s*=\s*[\[\(]([^\]\)]+)[\]\)]`,
)

// adminGetURLsRe detects `def get_urls(self)` to flag custom URL overrides.
var adminGetURLsRe = regexp.MustCompile(
	`\bdef\s+get_urls\s*\(\s*self`,
)

// adminGetURLsPatternRe captures `path('segment', view, name='name')` calls
// inside a get_urls() override. Group 1: path segment.
var adminGetURLsPatternRe = regexp.MustCompile(
	`(?:re_)?path\s*\(\s*r?["']([^"']+)["']`,
)

// adminInlineBaseRe recognises TabularInline / StackedInline base classes.
var adminInlineBaseRe = regexp.MustCompile(
	`\b(?:admin\.)?(?:Tabular|Stacked)Inline\b`,
)

// adminModelAdminBaseRe recognises ModelAdmin (but NOT Inline subclasses).
var adminModelAdminBaseRe = regexp.MustCompile(
	`\b(?:admin\.)?ModelAdmin\b`,
)

// adminActionIdentRe extracts individual identifiers from an actions list
// (handles both `funcname` and `"funcname"` / `'funcname'` forms).
var adminActionIdentRe = regexp.MustCompile(
	`["']?(\w+)["']?`,
)

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

// adminRegistration holds a fully-resolved ModelAdmin registration ready
// for route synthesis.
type adminRegistration struct {
	// modelName is the lowercase model class name (e.g. "user", "article").
	modelName string
	// adminClassName is the ModelAdmin subclass name, or "" for bare
	// registrations without an explicit admin class.
	adminClassName string
	// appLabel is inferred from the file path (e.g. users/admin.py → "users").
	appLabel string
	// sourceFile is the repo-relative file where the registration was found.
	sourceFile string
	// hasSearchFields is true when the admin class body declares search_fields.
	hasSearchFields bool
	// actions is the list of custom action names defined on the admin class.
	actions []string
	// hasGetURLs is true when the admin class overrides get_urls().
	hasGetURLs bool
	// getURLsPatterns is the list of path segments found in get_urls().
	getURLsPatterns []string
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

// ApplyDjangoAdminRoutes synthesizes http_endpoint entities for every
// Django ModelAdmin registration found across all Python files in the repo.
//
// parentFiles: repo-relative paths of all Python files.
// fileReader:  reads a repo-relative path and returns its bytes.
func ApplyDjangoAdminRoutes(
	parentFiles []string,
	fileReader NestedURLConfFileReader,
) []types.EntityRecord {
	if fileReader == nil {
		return nil
	}

	var out []types.EntityRecord
	seen := map[string]bool{}
	seenApps := map[string]bool{}
	siteRoutesEmitted := false

	// #1617 — the Django admin CRUD route family (changelist / add / change /
	// delete / history / login / logout / …) is 100% framework scaffolding:
	// it is generated by `admin.site.urls` with no project-authored handler.
	// On upvate this was 88 endpoints — 11.6% of all defs — with zero inbound
	// architectural signal, swamping the real endpoint surface in /endpoints
	// and topology. We tag every such node `scaffolding="true"` with a low
	// quality score so the caller (runDjangoAdminRoutes) can drop them, while
	// genuinely project-authored admin surfaces — custom `actions = [...]`
	// and `get_urls()` overrides — are tagged `scaffolding="false"` and kept.
	emit := func(verb, path, sourceFile, modelClass string, scaffolding bool) {
		id := httproutes.SyntheticID(verb, path)
		if seen[id] {
			return
		}
		seen[id] = true
		props := map[string]string{
			"verb":         strings.ToUpper(verb),
			"path":         path,
			"framework":    "django_admin",
			"pattern_type": "django_admin_synthetic",
		}
		if modelClass != "" {
			props["model_class"] = modelClass
		}
		quality := 0.8
		if scaffolding {
			props["scaffolding"] = "true"
			quality = 0.1
		} else {
			props["scaffolding"] = "false"
		}
		out = append(out, types.EntityRecord{
			ID:                 id,
			Name:               id,
			Kind:               httpEndpointKind,
			SourceFile:         sourceFile,
			Language:           "python",
			Properties:         props,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       quality,
		})
	}

	for _, relPath := range parentFiles {
		if !isAdminFile(relPath) {
			continue
		}
		content := fileReader(relPath)
		if len(content) == 0 {
			continue
		}
		src := string(content)
		appLabel := inferAppLabel(relPath)

		regs := extractAdminRegistrations(src, relPath, appLabel)
		if len(regs) == 0 {
			continue
		}

		// Site-level routes — emit exactly once per project.
		if !siteRoutesEmitted {
			siteRoutesEmitted = true
			emitAdminSiteRoutes(emit, relPath)
		}

		// Per-app route.
		if appLabel != "" && !seenApps[appLabel] {
			seenApps[appLabel] = true
			emitAdminAppRoute(emit, appLabel, relPath)
		}

		// Per-model routes.
		for _, reg := range regs {
			emitAdminModelRoutes(emit, reg)
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// Detection helpers
// ---------------------------------------------------------------------------

// isAdminFile reports whether a file is likely a Django admin module.
// Matches admin.py, */admin.py, */admin/__init__.py, or *_admin.py files.
func isAdminFile(relPath string) bool {
	base := filepath.Base(relPath)
	if base == "admin.py" || base == "__init__.py" {
		dir := filepath.Dir(relPath)
		if base == "__init__.py" {
			return strings.HasSuffix(dir, "/admin") || dir == "admin"
		}
		return true
	}
	return strings.HasSuffix(base, "_admin.py")
}

// inferAppLabel derives a Django app label from the file path.
// Examples:
//
//	users/admin.py            → "users"
//	myproject/users/admin.py  → "users"
//	admin.py                  → ""
func inferAppLabel(relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." || dir == "" {
		return ""
	}
	// If path is admin/__init__.py, go up two levels.
	if filepath.Base(dir) == "admin" {
		dir = filepath.Dir(dir)
	}
	return filepath.Base(dir)
}

// extractAdminRegistrations finds all ModelAdmin registrations in src.
// It handles:
//  1. admin.site.register(Model) — bare
//  2. admin.site.register(Model, AdminClass) — with explicit class
//  3. @admin.register(Model) — decorator
//  4. class FooAdmin(admin.ModelAdmin) — class definition (auto-linked
//     to models via decorator or register() call in the same file)
func extractAdminRegistrations(src, relPath, appLabel string) []adminRegistration {
	// Flatten parenthesised newlines for multi-line calls.
	flat := flattenParenthesised(src)

	// Build a map of admin class name → parsed body metadata (search_fields,
	// actions, get_urls patterns). We parse ALL admin classes in the file
	// and then match them to their models.
	adminClasses := parseAdminClasses(src)

	var regs []adminRegistration

	// --- Pattern 1 & 2: admin.site.register(...) ---
	for _, m := range adminRegisterRe.FindAllStringSubmatch(flat, -1) {
		modelArg := m[1]
		explicitClass := m[2]

		// Expand bracketed model lists: [Model1, Model2]
		models := splitModelList(modelArg)
		for _, model := range models {
			var meta adminClassMeta
			if explicitClass != "" {
				if ac, ok := adminClasses[explicitClass]; ok {
					meta = ac
				}
			}
			regs = append(regs, adminRegistration{
				modelName:       strings.ToLower(model),
				adminClassName:  explicitClass,
				appLabel:        appLabel,
				sourceFile:      relPath,
				hasSearchFields: meta.hasSearchFields,
				actions:         meta.actions,
				hasGetURLs:      meta.hasGetURLs,
				getURLsPatterns: meta.getURLsPatterns,
			})
		}
	}

	// --- Pattern 3: @admin.register(Model) decorator ---
	// Walk line by line: find the decorator, then pick up the class that
	// immediately follows (possibly with other decorators in between).
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		m := adminDecoratorRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		models := splitModelList(m[1])

		// Locate the class definition following the decorator.
		adminClassName := ""
		for j := i + 1; j < len(lines) && j < i+10; j++ {
			cm := adminClassDefRe.FindStringSubmatch(lines[j])
			if cm != nil {
				adminClassName = cm[1]
				break
			}
		}

		var meta adminClassMeta
		if adminClassName != "" {
			if ac, ok := adminClasses[adminClassName]; ok {
				meta = ac
			}
		}

		for _, model := range models {
			regs = append(regs, adminRegistration{
				modelName:       strings.ToLower(model),
				adminClassName:  adminClassName,
				appLabel:        appLabel,
				sourceFile:      relPath,
				hasSearchFields: meta.hasSearchFields,
				actions:         meta.actions,
				hasGetURLs:      meta.hasGetURLs,
				getURLsPatterns: meta.getURLsPatterns,
			})
		}
	}

	// --- Pattern 4: class FooAdmin(admin.ModelAdmin) with no explicit
	// register() call. Emit for any ModelAdmin subclass not already covered
	// by the above patterns.
	coveredClasses := map[string]bool{}
	for _, r := range regs {
		if r.adminClassName != "" {
			coveredClasses[r.adminClassName] = true
		}
	}
	for className, meta := range adminClasses {
		if coveredClasses[className] {
			continue
		}
		if !meta.isModelAdmin {
			continue
		}
		// Derive model name from class name: strip "Admin" suffix.
		modelName := strings.TrimSuffix(className, "Admin")
		if modelName == className {
			// No "Admin" suffix — use the class name as-is (lowercased).
			modelName = className
		}
		regs = append(regs, adminRegistration{
			modelName:       strings.ToLower(modelName),
			adminClassName:  className,
			appLabel:        appLabel,
			sourceFile:      relPath,
			hasSearchFields: meta.hasSearchFields,
			actions:         meta.actions,
			hasGetURLs:      meta.hasGetURLs,
			getURLsPatterns: meta.getURLsPatterns,
		})
	}

	return regs
}

// adminClassMeta holds parsed metadata for a single admin class definition.
type adminClassMeta struct {
	// isModelAdmin is true when the base class includes admin.ModelAdmin
	// (but NOT Inline classes).
	isModelAdmin    bool
	hasSearchFields bool
	actions         []string
	hasGetURLs      bool
	getURLsPatterns []string
}

// parseAdminClasses scans src for class definitions that subclass
// admin.ModelAdmin (or just ModelAdmin) and returns their metadata.
// Inline subclasses (TabularInline, StackedInline) are excluded from
// isModelAdmin but still parsed.
func parseAdminClasses(src string) map[string]adminClassMeta {
	out := map[string]adminClassMeta{}

	for _, m := range adminClassDefRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		baseList := src[m[4]:m[5]]

		isInline := adminInlineBaseRe.MatchString(baseList)
		isModelAdmin := adminModelAdminBaseRe.MatchString(baseList)
		// Skip classes that don't relate to Django admin at all.
		if !isInline && !isModelAdmin {
			continue
		}

		body := extractClassBody(src, m[1])

		meta := adminClassMeta{
			isModelAdmin:    isModelAdmin && !isInline,
			hasSearchFields: adminSearchFieldsRe.MatchString(body),
			hasGetURLs:      adminGetURLsRe.MatchString(body),
		}
		meta.actions = extractAdminActions(body)
		if meta.hasGetURLs {
			meta.getURLsPatterns = extractGetURLsPatterns(body)
		}
		out[className] = meta
	}
	return out
}

// extractAdminActions parses the `actions = [...]` attribute in a ModelAdmin
// class body and returns the list of action function names.
func extractAdminActions(body string) []string {
	m := adminActionsRe.FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	raw := m[1]
	var names []string
	seen := map[string]bool{}
	for _, tok := range adminActionIdentRe.FindAllStringSubmatch(raw, -1) {
		name := tok[1]
		// Skip Python keywords that might appear in the list.
		if name == "" || name == "True" || name == "False" || name == "None" {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// extractGetURLsPatterns parses `path('segment', ...)` calls inside a
// get_urls() override and returns the path segments found.
func extractGetURLsPatterns(body string) []string {
	// Locate the get_urls def body.
	idx := adminGetURLsRe.FindStringIndex(body)
	if idx == nil {
		return nil
	}
	getURLsBody := extractClassBody(body, idx[1])
	var patterns []string
	for _, m := range adminGetURLsPatternRe.FindAllStringSubmatch(getURLsBody, -1) {
		seg := strings.Trim(m[1], "/")
		if seg != "" {
			patterns = append(patterns, seg)
		}
	}
	return patterns
}

// splitModelList splits a model argument that may be a bracketed list
// (`[Model1, Model2]`) or a bare name into individual model names.
func splitModelList(arg string) []string {
	arg = strings.TrimSpace(arg)
	arg = strings.Trim(arg, "[]")
	var out []string
	for _, tok := range strings.Split(arg, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Route emission helpers
// ---------------------------------------------------------------------------

// emitAdminSiteRoutes emits the site-level admin routes that exist exactly
// once per Django project regardless of how many ModelAdmins are registered.
func emitAdminSiteRoutes(
	emit func(verb, path, sourceFile, modelClass string, scaffolding bool),
	sourceFile string,
) {
	siteRoutes := []struct {
		verb string
		path string
	}{
		{"GET", "/admin"},
		{"GET", "/admin/login"},
		{"POST", "/admin/login"},
		{"GET", "/admin/logout"},
		{"GET", "/admin/password_change"},
		{"POST", "/admin/password_change"},
		{"GET", "/admin/jsi18n"},
	}
	for _, r := range siteRoutes {
		emit(r.verb, r.path, sourceFile, "", true)
	}
}

// emitAdminAppRoute emits the per-app index route: GET /admin/<app>/
func emitAdminAppRoute(
	emit func(verb, path, sourceFile, modelClass string, scaffolding bool),
	appLabel, sourceFile string,
) {
	emit("GET", "/admin/"+appLabel, sourceFile, "", true)
}

// emitAdminModelRoutes emits the standard per-model route family plus any
// custom actions and get_urls() patterns.
func emitAdminModelRoutes(
	emit func(verb, path, sourceFile, modelClass string, scaffolding bool),
	reg adminRegistration,
) {
	base := "/admin/" + reg.appLabel + "/" + reg.modelName
	id := base + "/{id}"
	modelClass := reg.adminClassName
	if modelClass == "" {
		modelClass = reg.modelName
	}

	// Changelist.
	emit("GET", base, reg.sourceFile, modelClass, true)

	// Add.
	emit("GET", base+"/add", reg.sourceFile, modelClass, true)
	emit("POST", base+"/add", reg.sourceFile, modelClass, true)

	// Change.
	emit("GET", id+"/change", reg.sourceFile, modelClass, true)
	emit("POST", id+"/change", reg.sourceFile, modelClass, true)

	// Delete.
	emit("GET", id+"/delete", reg.sourceFile, modelClass, true)
	emit("POST", id+"/delete", reg.sourceFile, modelClass, true)

	// History.
	emit("GET", id+"/history", reg.sourceFile, modelClass, true)

	// Autocomplete — only when search_fields is defined.
	if reg.hasSearchFields {
		emit("GET", base+"/autocomplete", reg.sourceFile, modelClass, true)
	}

	// Custom actions (bulk operations on changelist) — project-authored, kept.
	for _, action := range reg.actions {
		emit("POST", base+"/"+action, reg.sourceFile, modelClass, false)
	}

	// Custom get_urls() patterns — project-authored, kept.
	for _, seg := range reg.getURLsPatterns {
		// get_urls() patterns are under the model prefix.
		emit("GET", base+"/"+seg, reg.sourceFile, modelClass, false)
	}
}
