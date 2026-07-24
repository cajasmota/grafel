package dart

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_dart_flutter", &flutterExtractor{})
}

type flutterExtractor struct{}

func (e *flutterExtractor) Language() string { return "custom_dart_flutter" }

var (
	reFlutterStatelessWidget = regexp.MustCompile(
		`(?m)class\s+([A-Z][A-Za-z0-9_]*)\s+extends\s+StatelessWidget\b`,
	)
	reFlutterStatefulWidget = regexp.MustCompile(
		`(?m)class\s+([A-Z][A-Za-z0-9_]*)\s+extends\s+StatefulWidget\b`,
	)
	// Riverpod / flutter_hooks widget bases — also screen-level UI components.
	reFlutterConsumerWidget = regexp.MustCompile(
		`(?m)class\s+([A-Z][A-Za-z0-9_]*)\s+extends\s+(ConsumerWidget|ConsumerStatefulWidget|HookWidget|HookConsumerWidget|StatelessHookConsumerWidget)\b`,
	)
	reFlutterBLoC = regexp.MustCompile(
		`(?m)class\s+([A-Z][A-Za-z0-9_]*)\s+extends\s+Bloc\s*<`,
	)
	reFlutterCubit = regexp.MustCompile(
		`(?m)class\s+([A-Z][A-Za-z0-9_]*)\s+extends\s+Cubit\s*<`,
	)
	reFlutterChangeNotifier = regexp.MustCompile(
		`(?m)class\s+([A-Z][A-Za-z0-9_]*)\s+extends\s+ChangeNotifier\b`,
	)
	reFlutterInheritedWidget = regexp.MustCompile(
		`(?m)class\s+([A-Z][A-Za-z0-9_]*)\s+extends\s+InheritedWidget\b`,
	)
	// Navigator.push(context, MaterialPageRoute(builder: (_) => DetailScreen()))
	// — captures the widget constructed inside the route builder.
	reFlutterNavigatorPushWidget = regexp.MustCompile(
		`Navigator\.(?:of\([^)]*\)\.)?push(?:Replacement)?\s*\([^,]+,\s*(?:Material|Cupertino)PageRoute\s*\([^)]*builder:\s*\([^)]*\)\s*=>\s*(?:const\s+)?([A-Z][A-Za-z0-9_]*)`,
	)
	// Navigator.pushNamed(context, "/detail") and named variants → route path.
	reFlutterPushNamed = regexp.MustCompile(
		`Navigator\.(?:of\([^)]*\)\.)?push(?:Named|ReplacementNamed|NamedAndRemoveUntil)\s*\([^,]+,\s*["']([^"']+)["']`,
	)
	// go_router: context.go("/detail") / context.push("/detail") / etc.
	reFlutterContextGo = regexp.MustCompile(
		`context\.(?:go|push|replace|pushReplacement)\s*\(\s*["']([^"']+)["']`,
	)
	// GoRoute(path: "/detail", builder: (_, __) => DetailScreen())
	reFlutterGoRoute = regexp.MustCompile(
		`GoRoute\s*\(\s*path:\s*["']([^"']+)["']`,
	)
	// GoRoute builder target widget (route → screen wiring).
	reFlutterGoRouteBuilder = regexp.MustCompile(
		`builder:\s*\([^)]*\)\s*=>\s*(?:const\s+)?([A-Z][A-Za-z0-9_]*)`,
	)
	reFlutterStreamBuilder = regexp.MustCompile(
		`(?m)StreamBuilder\s*<\s*([A-Za-z_]\w*)`,
	)
	reFlutterBlocBuilder = regexp.MustCompile(
		`(?m)BlocBuilder\s*<\s*([A-Za-z_]\w*)\s*,`,
	)
	// context.read<T>() / context.watch<T>() / context.select<T>()
	reFlutterContextRead = regexp.MustCompile(
		`context\.(read|watch|select)\s*<\s*([A-Za-z_]\w*)`,
	)
	// BlocProvider.of<T>(context) / Provider.of<T>(context) / RepositoryProvider.of<T>
	reFlutterProviderOf = regexp.MustCompile(
		`(BlocProvider|Provider|RepositoryProvider)\.of\s*<\s*([A-Za-z_]\w*)\s*>`,
	)
	// Riverpod: ref.watch(xProvider) / ref.read(xProvider) / ref.listen(xProvider)
	reFlutterRefWatch = regexp.MustCompile(
		`ref\.(watch|read|listen)\s*\(\s*([A-Za-z_]\w*)`,
	)
	// Class declarations — used to attribute call sites to the enclosing
	// widget/host class via brace-span tracking.
	reFlutterClassDecl = regexp.MustCompile(
		`(?m)class\s+([A-Z][A-Za-z0-9_]*)\b`,
	)
	// Widget instance-field declaration — a Flutter widget prop. Captures the
	// `final <Type> <name>;` immutable field idiom that is the canonical
	// Flutter prop shape (constructor `required this.<name>` params bind to
	// these). Group 1 = type, group 2 = field name. `static`/`const` (compile-
	// time constants, not per-instance props) are excluded by requiring the
	// `final` keyword without a leading `static`.
	reFlutterWidgetProp = regexp.MustCompile(
		`(?m)^[ \t]+final\s+([A-Za-z_][\w<>,.()? ]*?[\w>)?])\s+(\w+)\s*;`)
)

// normalizeRoute canonicalizes path parameters so `/profile/:id` and
// `/profile/{id}` collapse to a single route stub `/profile/{id}`.
func normalizeRoute(route string) string {
	if route == "" {
		return route
	}
	segs := strings.Split(route, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, ":") {
			segs[i] = "{" + s[1:] + "}"
		}
	}
	return strings.Join(segs, "/")
}

// classSpan records a class declaration's brace-balanced body range, used to
// attribute a call site to the enclosing widget/host class.
type classSpan struct {
	name  string
	open  int // offset of the opening `{`
	close int // offset just past the matching `}` (exclusive)
}

// buildClassSpans returns class spans with their brace-balanced bodies.
// Regex/brace-counting only and best-effort: braces inside string/char
// literals are not stripped, which only mildly widens a span and never
// crosses into an unrelated class for the common Flutter idiom.
func buildClassSpans(src string) []classSpan {
	var spans []classSpan
	for _, m := range reFlutterClassDecl.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		rel := strings.IndexByte(src[m[1]:], '{')
		if rel < 0 {
			continue
		}
		open := m[1] + rel
		depth := 0
		close := len(src)
		for i := open; i < len(src); i++ {
			switch src[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					close = i + 1
					i = len(src) // break loop
				}
			}
		}
		spans = append(spans, classSpan{name: name, open: open, close: close})
	}
	return spans
}

// enclosingClass returns the name of the innermost class whose body contains
// offset, or "" if the offset is at file scope (e.g. a top-level GoRoute list).
func enclosingClass(spans []classSpan, offset int) string {
	best := ""
	bestOpen := -1
	for _, s := range spans {
		if offset >= s.open && offset < s.close && s.open > bestOpen {
			best = s.name
			bestOpen = s.open
		}
	}
	return best
}

func (e *flutterExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/dart")
	_, span := tracer.Start(ctx, "indexer.flutter_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "flutter"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "dart" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)
	idxByName := make(map[string]int) // entity name -> index in entities

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		idxByName[ent.Name] = len(entities)
		entities = append(entities, ent)
	}

	spans := buildClassSpans(src)

	// Buffered edges, flushed onto their host entity after all entities are
	// emitted (entity emission order is independent of call-site order).
	type pendingEdge struct {
		host string
		rel  types.RelationshipRecord
	}
	var pending []pendingEdge
	attachEdge := func(host string, rel types.RelationshipRecord) {
		if host == "" {
			return
		}
		rel.FromID = host // bare name; resolver substitutes host's hex ID
		pending = append(pending, pendingEdge{host: host, rel: rel})
	}

	// widgetNames records every class promoted to a SCOPE.UIComponent so the
	// prop-extraction pass (Data Flow/prop_extraction, #5361) scopes itself to
	// widget classes only — a plain model/service field is not a widget prop.
	widgetNames := map[string]bool{}

	// 1. StatelessWidget -> SCOPE.UIComponent
	for _, m := range reFlutterStatelessWidget.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_WIDGET",
			"widget_type", "stateless")
		add(ent)
		widgetNames[name] = true
	}

	// 2. StatefulWidget -> SCOPE.UIComponent
	for _, m := range reFlutterStatefulWidget.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_WIDGET",
			"widget_type", "stateful")
		add(ent)
		widgetNames[name] = true
	}

	// 2b. Riverpod/hooks widget bases -> SCOPE.UIComponent
	for _, m := range reFlutterConsumerWidget.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		base := src[m[4]:m[5]]
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_WIDGET",
			"widget_type", "consumer", "widget_base", base)
		add(ent)
		widgetNames[name] = true
	}

	// 2c. Widget prop / tree depth (#5361) — Data Flow/prop_extraction. For
	// each widget class, parse its `final <Type> <name>;` instance fields (the
	// canonical Flutter prop idiom, bound by `required this.<name>` ctor
	// params) and emit one SCOPE.Pattern(prop_extraction) per prop plus a
	// HAS_PROPS edge from the widget component. Scoped to widget classes only
	// (widgetNames) so model/service fields are not mis-tagged as props; the
	// drift IntColumn getters and other ORM fields are not `final ... ;` so
	// they never match here.
	for _, s := range spans {
		if !widgetNames[s.name] {
			continue
		}
		if s.open >= s.close || s.close > len(src) {
			continue
		}
		body := src[s.open:s.close]
		propSeen := map[string]bool{}
		for _, pm := range reFlutterWidgetProp.FindAllStringSubmatchIndex(body, -1) {
			propType := strings.TrimSpace(body[pm[2]:pm[3]])
			propName := body[pm[4]:pm[5]]
			if propName == "" || propSeen[propName] {
				continue
			}
			propSeen[propName] = true
			pName := "prop:" + s.name + ":" + propName
			pent := makeEntity(pName, "SCOPE.Pattern", "prop_extraction", file.Path,
				file.Language, lineOf(src, s.open+pm[0]))
			setProps(&pent, "framework", "flutter",
				"provenance", "INFERRED_FROM_FLUTTER_WIDGET_PROP",
				"widget", s.name, "prop_name", propName, "prop_type", propType)
			add(pent)
			attachEdge(s.name, types.RelationshipRecord{
				ToID: pName,
				Kind: string(types.RelationshipKindHasProps),
				Properties: types.Props{
					{K: "framework", V: "flutter"},
					{K: "prop_name", V: propName},
					{K: "prop_type", V: propType},
				},
			})
		}
	}

	// 3. BLoC classes -> SCOPE.Pattern
	for _, m := range reFlutterBLoC.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_BLOC",
			"state_kind", "bloc")
		add(ent)
	}

	// 4. Cubit classes -> SCOPE.Pattern
	for _, m := range reFlutterCubit.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_BLOC",
			"state_kind", "cubit")
		add(ent)
	}

	// 5. ChangeNotifier -> SCOPE.Pattern
	for _, m := range reFlutterChangeNotifier.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_PROVIDER",
			"state_kind", "change_notifier")
		add(ent)
	}

	// 6. InheritedWidget -> SCOPE.Pattern
	for _, m := range reFlutterInheritedWidget.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_INHERITED_WIDGET")
		add(ent)
	}

	// emitRoute creates (once) a route stub entity so NAVIGATES_TO ToID stubs
	// resolve to a concrete entity. Returns the stub name.
	emitRoute := func(prefix, route string, offset int, provenance string) string {
		norm := normalizeRoute(route)
		stub := prefix + norm
		ent := makeEntity(stub, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, offset))
		setProps(&ent, "framework", "flutter", "provenance", provenance, "route_path", norm)
		add(ent)
		return stub
	}

	// 7. Navigator.pushNamed -> route entity + NAVIGATES_TO edge.
	for _, m := range reFlutterPushNamed.FindAllStringSubmatchIndex(src, -1) {
		route := src[m[2]:m[3]]
		stub := emitRoute("route:", route, m[0], "INFERRED_FROM_FLUTTER_NAVIGATOR")
		attachEdge(enclosingClass(spans, m[0]), types.RelationshipRecord{
			ToID: stub,
			Kind: string(types.RelationshipKindNavigatesTo),
			Properties: types.Props{
				{K: "framework", V: "flutter"},
				{K: "nav_kind", V: "pushNamed"},
				{K: "target", V: "route"},
			},
		})
	}

	// 7b. Navigator.push(MaterialPageRoute(builder: => Widget)) -> widget target.
	for _, m := range reFlutterNavigatorPushWidget.FindAllStringSubmatchIndex(src, -1) {
		target := src[m[2]:m[3]]
		attachEdge(enclosingClass(spans, m[0]), types.RelationshipRecord{
			ToID: target,
			Kind: string(types.RelationshipKindNavigatesTo),
			Properties: types.Props{
				{K: "framework", V: "flutter"},
				{K: "nav_kind", V: "MaterialPageRoute"},
				{K: "target", V: "widget"},
			},
		})
	}

	// 7c. go_router imperative navigation: context.go/push("/detail").
	for _, m := range reFlutterContextGo.FindAllStringSubmatchIndex(src, -1) {
		route := src[m[2]:m[3]]
		stub := emitRoute("route:", route, m[0], "INFERRED_FROM_FLUTTER_GO_ROUTER")
		attachEdge(enclosingClass(spans, m[0]), types.RelationshipRecord{
			ToID: stub,
			Kind: string(types.RelationshipKindNavigatesTo),
			Properties: types.Props{
				{K: "framework", V: "flutter"},
				{K: "nav_kind", V: "go_router"},
				{K: "target", V: "route"},
			},
		})
	}

	// 8. GoRoute(path:, builder: => Screen) -> route entity + route→screen edge.
	for _, m := range reFlutterGoRoute.FindAllStringSubmatchIndex(src, -1) {
		route := src[m[2]:m[3]]
		stub := emitRoute("go_route:", route, m[0], "INFERRED_FROM_FLUTTER_GO_ROUTER")
		// Wire route → screen: scan forward from the path match for builder target.
		tail := src[m[1]:]
		if bm := reFlutterGoRouteBuilder.FindStringSubmatchIndex(tail); bm != nil {
			screen := tail[bm[2]:bm[3]]
			attachEdge(stub, types.RelationshipRecord{
				ToID: screen,
				Kind: string(types.RelationshipKindNavigatesTo),
				Properties: types.Props{
					{K: "framework", V: "flutter"},
					{K: "nav_kind", V: "go_route_builder"},
					{K: "target", V: "widget"},
				},
			})
		}
	}

	// 9. StreamBuilder<T> -> SCOPE.Component (stream dep, kept as entity).
	for _, m := range reFlutterStreamBuilder.FindAllStringSubmatchIndex(src, -1) {
		stateType := src[m[2]:m[3]]
		ent := makeEntity("stream:"+stateType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_STREAM_BUILDER",
			"state_type", stateType)
		add(ent)
	}

	// usesEdge attaches a widget → provider/bloc/viewmodel USES edge.
	usesEdge := func(offset int, target, accessMethod, bindKind string) {
		attachEdge(enclosingClass(spans, offset), types.RelationshipRecord{
			ToID: target,
			Kind: string(types.RelationshipKindUses),
			Properties: types.Props{
				{K: "access_method", V: accessMethod},
				{K: "bind_kind", V: bindKind},
				{K: "framework", V: "flutter"},
			},
		})
	}

	// 10. BlocBuilder<T, S> -> USES edge (widget consumes bloc T).
	for _, m := range reFlutterBlocBuilder.FindAllStringSubmatchIndex(src, -1) {
		usesEdge(m[0], src[m[2]:m[3]], "BlocBuilder", "bloc")
	}

	// 11. context.read<T>() / watch<T>() / select<T>() -> USES edge.
	for _, m := range reFlutterContextRead.FindAllStringSubmatchIndex(src, -1) {
		usesEdge(m[0], src[m[4]:m[5]], "context."+src[m[2]:m[3]], "provider")
	}

	// 12. BlocProvider.of<T>(ctx) / Provider.of<T>(ctx) -> USES edge.
	for _, m := range reFlutterProviderOf.FindAllStringSubmatchIndex(src, -1) {
		usesEdge(m[0], src[m[4]:m[5]], src[m[2]:m[3]]+".of", "provider")
	}

	// 13. Riverpod ref.watch(xProvider) / ref.read(...) -> USES edge.
	for _, m := range reFlutterRefWatch.FindAllStringSubmatchIndex(src, -1) {
		usesEdge(m[0], src[m[4]:m[5]], "ref."+src[m[2]:m[3]], "riverpod")
	}

	// Flush buffered edges onto their host entity. The resolver substitutes
	// the host's hex ID from the bare-name FromID via same-file locality.
	// When the host class was not emitted as a graph entity (e.g. a plain
	// non-widget host, or cross-file/sealed route-class indirection we can't
	// resolve in-file), mark the edge unresolved=true (honest-partial,
	// mirroring the compose treatment) and still attach it so the screen
	// graph records the navigation/binding intent.
	for _, pe := range pending {
		hi, ok := idxByName[pe.host]
		if !ok {
			if pe.rel.Properties == nil {
				pe.rel.Properties = types.Props{}
			}
			pe.rel.Properties.Set("unresolved", "true")
			if len(entities) == 0 {
				continue // nothing to anchor on
			}
			hi = 0
		}
		entities[hi].Relationships = append(entities[hi].Relationships, pe.rel)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
