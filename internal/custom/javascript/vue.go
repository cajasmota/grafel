package javascript

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_vue", &vueExtractor{})
}

type vueExtractor struct{}

func (e *vueExtractor) Language() string { return "custom_js_vue" }

var (
	reVueScriptSetupAttr = regexp.MustCompile(
		`<script[^>]*\bsetup\b`,
	)
	reVueDefineComponentName = regexp.MustCompile(
		`defineComponent\s*\(\s*\{[^}]*name\s*:\s*['"]([^'"]+)['"]`,
	)
	reVueComposable = regexp.MustCompile(
		`export\s+(?:(?:const|function)\s+)(use[A-Z][A-Za-z0-9_]*)`,
	)
	reVuePiniaStore = regexp.MustCompile(
		`defineStore\s*\(\s*['"]([^'"]+)['"]`,
	)
	reVueDefineProps = regexp.MustCompile(
		`defineProps\s*(?:<[^>]*>)?\s*\(`,
	)
	reVueDefineEmits = regexp.MustCompile(
		`defineEmits\s*(?:<[^>]*>)?\s*\(`,
	)
	reVueProvide = regexp.MustCompile(
		`provide\s*\(\s*['"]([^'"]+)['"]`,
	)
	reVueInject = regexp.MustCompile(
		`inject\s*\(\s*['"]([^'"]+)['"]`,
	)
	reVueRouter = regexp.MustCompile(
		`(?:createRouter|useRouter)\s*\(`,
	)
	reVueRouteRecord = regexp.MustCompile(
		`path\s*:\s*['"]([^'"]*?)['"]`,
	)
)

func isVueFile(fp string) bool {
	return strings.HasSuffix(filepath.ToSlash(fp), ".vue")
}

func (e *vueExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.vue_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "vue"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	fp := filepath.ToSlash(file.Path)
	vueFile := isVueFile(fp)

	if lang != "typescript" && lang != "javascript" && !vueFile {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Derive component name from filename (PascalCase from kebab-case)
	stem := filepath.Base(fp)
	ext := filepath.Ext(stem)
	stem = strings.TrimSuffix(stem, ext)
	compName := toPascalCase(stem)

	// .vue SFC component
	if vueFile {
		isSetup := reVueScriptSetupAttr.MatchString(src)
		hasDefineProps := reVueDefineProps.MatchString(src)
		hasDefineEmits := reVueDefineEmits.MatchString(src)

		// Try to get name from defineComponent
		if nm := reVueDefineComponentName.FindStringSubmatch(src); nm != nil {
			compName = nm[1]
		}

		ent := makeEntity(compName, "SCOPE.UIComponent", "component", file.Path, file.Language, 1)
		setProps(&ent, "framework", "vue", "is_setup", fmt.Sprintf("%v", isSetup),
			"has_define_props", fmt.Sprintf("%v", hasDefineProps),
			"has_define_emits", fmt.Sprintf("%v", hasDefineEmits),
			"provenance", "INFERRED_FROM_VUE_COMPONENT")
		addEntity(ent)
	}

	// Composables (use*)
	for _, m := range reVueComposable.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "composable", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vue", "provenance", "INFERRED_FROM_VUE_COMPOSABLE")
		addEntity(ent)
	}

	// Pinia stores
	for _, m := range reVuePiniaStore.FindAllStringSubmatchIndex(src, -1) {
		storeId := src[m[2]:m[3]]
		ent := makeEntity(storeId, "SCOPE.Component", "pinia_store", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vue", "store_id", storeId,
			"provenance", "INFERRED_FROM_VUE_PINIA_STORE")
		addEntity(ent)
	}

	// provide/inject
	for _, m := range reVueProvide.FindAllStringSubmatchIndex(src, -1) {
		key := src[m[2]:m[3]]
		ent := makeEntity("provide:"+key, "SCOPE.Component", "provide", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vue", "provide_key", key,
			"provenance", "INFERRED_FROM_VUE_PROVIDE")
		addEntity(ent)
	}
	for _, m := range reVueInject.FindAllStringSubmatchIndex(src, -1) {
		key := src[m[2]:m[3]]
		ent := makeEntity("inject:"+key, "SCOPE.Component", "inject", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vue", "inject_key", key,
			"provenance", "INFERRED_FROM_VUE_INJECT")
		addEntity(ent)
	}

	// Router
	for _, m := range reVueRouter.FindAllStringIndex(src, -1) {
		ent := makeEntity("VueRouter", "SCOPE.Service", "router", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vue", "provenance", "INFERRED_FROM_VUE_ROUTER")
		addEntity(ent)
	}

	// Route records
	for _, m := range reVueRouteRecord.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		if path == "" {
			continue
		}
		ent := makeEntity(path, "SCOPE.Operation", "route", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "vue", "route_path", path,
			"provenance", "INFERRED_FROM_VUE_ROUTE")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
