package dart

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
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
	reFlutterNavigatorPush = regexp.MustCompile(
		`Navigator\.push(?:Named)?\s*\([^,]+,\s*(?:MaterialPageRoute[^;]*|\s*["']([^"']+)["'])`,
	)
	reFlutterPushNamed = regexp.MustCompile(
		`Navigator\.push(?:Named|ReplacementNamed|NamedAndRemoveUntil)\s*\([^,]+,\s*["']([^"']+)["']`,
	)
	reFlutterGoRoute = regexp.MustCompile(
		`GoRoute\s*\(\s*path:\s*["']([^"']+)["']`,
	)
	reFlutterStreamBuilder = regexp.MustCompile(
		`(?m)StreamBuilder\s*<\s*([A-Za-z_]\w*)`,
	)
	reFlutterBlocBuilder = regexp.MustCompile(
		`(?m)BlocBuilder\s*<\s*([A-Za-z_]\w*)\s*,`,
	)
	reFlutterContextRead = regexp.MustCompile(
		`context\.(read|watch|select)\s*<\s*([A-Za-z_]\w*)`,
	)
)

func (e *flutterExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/dart")
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

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. StatelessWidget -> SCOPE.UIComponent
	for _, m := range reFlutterStatelessWidget.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_WIDGET",
			"widget_type", "stateless")
		add(ent)
	}

	// 2. StatefulWidget -> SCOPE.UIComponent
	for _, m := range reFlutterStatefulWidget.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_WIDGET",
			"widget_type", "stateful")
		add(ent)
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

	// 7. Navigator.pushNamed routes -> SCOPE.Operation/endpoint
	for _, m := range reFlutterPushNamed.FindAllStringSubmatchIndex(src, -1) {
		route := src[m[2]:m[3]]
		ent := makeEntity("route:"+route, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_NAVIGATOR",
			"route_path", route)
		add(ent)
	}

	// 8. GoRouter routes -> SCOPE.Operation/endpoint
	for _, m := range reFlutterGoRoute.FindAllStringSubmatchIndex(src, -1) {
		route := src[m[2]:m[3]]
		ent := makeEntity("go_route:"+route, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_GO_ROUTER",
			"route_path", route)
		add(ent)
	}

	// 9. StreamBuilder<T> -> SCOPE.Component (stream dep)
	for _, m := range reFlutterStreamBuilder.FindAllStringSubmatchIndex(src, -1) {
		stateType := src[m[2]:m[3]]
		ent := makeEntity("stream:"+stateType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_STREAM_BUILDER",
			"state_type", stateType)
		add(ent)
	}

	// 10. BlocBuilder<T, S> -> SCOPE.Component
	for _, m := range reFlutterBlocBuilder.FindAllStringSubmatchIndex(src, -1) {
		blocType := src[m[2]:m[3]]
		ent := makeEntity("bloc_dep:"+blocType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_BLOC_BUILDER",
			"bloc_type", blocType)
		add(ent)
	}

	// 11. context.read<T>() / context.watch<T>() -> SCOPE.Component
	for _, m := range reFlutterContextRead.FindAllStringSubmatchIndex(src, -1) {
		accessMethod := src[m[2]:m[3]]
		providerType := src[m[4]:m[5]]
		ent := makeEntity("provider:"+providerType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "flutter", "provenance", "INFERRED_FROM_FLUTTER_PROVIDER_ACCESS",
			"access_method", accessMethod, "provider_type", providerType)
		add(ent)
	}

	_ = reFlutterNavigatorPush // captures generic push, named variant above is more specific

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
