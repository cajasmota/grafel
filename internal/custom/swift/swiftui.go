package swift

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_swift_swiftui", &swiftUIExtractor{})
}

type swiftUIExtractor struct{}

func (e *swiftUIExtractor) Language() string { return "custom_swift_swiftui" }

var (
	reSwiftUIViewConformance = regexp.MustCompile(
		`(?m)(?:struct|class)\s+([A-Z][A-Za-z0-9_]*)\s*:\s*(?:[A-Za-z,\s]*\b)?View\b`,
	)
	reSwiftUINavStack = regexp.MustCompile(
		`NavigationStack\s*\{`,
	)
	reSwiftUINavLink = regexp.MustCompile(
		`NavigationLink\s*\(\s*(?:destination:\s*)?([A-Z][A-Za-z0-9_]*)`,
	)
	reSwiftUINavDestination = regexp.MustCompile(
		`\.navigationDestination\s*\(for:\s*(\w+)\.self`,
	)
	reSwiftUIState = regexp.MustCompile(
		`@State\s+(?:private\s+)?var\s+(\w+)\s*:`,
	)
	reSwiftUIBinding = regexp.MustCompile(
		`@Binding\s+var\s+(\w+)\s*:`,
	)
	reSwiftUIObservedObject = regexp.MustCompile(
		`@(?:ObservedObject|EnvironmentObject|StateObject)\s+var\s+(\w+)\s*:\s*(\w+)`,
	)
	reSwiftUIEnvironment = regexp.MustCompile(
		`@Environment\s*\(\s*\\\.(\w+)\s*\)\s+var\s+(\w+)`,
	)
)

// swiftUIBuiltinViews are SwiftUI framework views not emitted as entities.
var swiftUIBuiltinViews = map[string]bool{
	"Text": true, "Image": true, "Button": true, "TextField": true,
	"SecureField": true, "Toggle": true, "Slider": true, "Stepper": true,
	"Picker": true, "DatePicker": true, "ColorPicker": true,
	"VStack": true, "HStack": true, "ZStack": true, "LazyVStack": true,
	"LazyHStack": true, "LazyVGrid": true, "LazyHGrid": true,
	"List": true, "ScrollView": true, "Form": true, "Group": true,
	"Section": true, "ForEach": true, "NavigationView": true,
	"NavigationStack": true, "NavigationSplitView": true,
	"NavigationLink": true, "TabView": true, "Sheet": true,
	"Alert": true, "ProgressView": true, "Spacer": true, "Divider": true,
	"GeometryReader": true, "EmptyView": true, "AnyView": true,
	"Color": true, "Rectangle": true, "Circle": true, "Path": true,
}

func (e *swiftUIExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/swift")
	_, span := tracer.Start(ctx, "indexer.swiftui_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "swiftui"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "swift" {
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

	// 1. struct/class conforming to View -> SCOPE.UIComponent
	for _, m := range reSwiftUIViewConformance.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if swiftUIBuiltinViews[name] {
			continue
		}
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_VIEW")
		add(ent)
	}

	// 2. NavigationStack -> signals navigation context
	for _, m := range reSwiftUINavStack.FindAllStringIndex(src, -1) {
		ent := makeEntity("NavigationStack", "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_NAV_STACK")
		add(ent)
	}

	// 3. NavigationLink destinations -> SCOPE.Operation/endpoint
	for _, m := range reSwiftUINavLink.FindAllStringSubmatchIndex(src, -1) {
		dest := src[m[2]:m[3]]
		if swiftUIBuiltinViews[dest] {
			continue
		}
		ent := makeEntity("nav:"+dest, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_NAV_LINK",
			"destination", dest)
		add(ent)
	}

	// 4. .navigationDestination -> SCOPE.Operation/endpoint
	for _, m := range reSwiftUINavDestination.FindAllStringSubmatchIndex(src, -1) {
		destType := src[m[2]:m[3]]
		ent := makeEntity("navdest:"+destType, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_NAV_DESTINATION",
			"destination_type", destType)
		add(ent)
	}

	// 5. @State properties -> SCOPE.Pattern
	for _, m := range reSwiftUIState.FindAllStringSubmatchIndex(src, -1) {
		propName := src[m[2]:m[3]]
		ent := makeEntity("state:"+propName, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_STATE",
			"property_wrapper", "@State")
		add(ent)
	}

	// 6. @Binding properties -> SCOPE.Pattern
	for _, m := range reSwiftUIBinding.FindAllStringSubmatchIndex(src, -1) {
		propName := src[m[2]:m[3]]
		ent := makeEntity("binding:"+propName, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_BINDING",
			"property_wrapper", "@Binding")
		add(ent)
	}

	// 7. @ObservedObject/@EnvironmentObject -> SCOPE.Component
	for _, m := range reSwiftUIObservedObject.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		vmType := src[m[4]:m[5]]
		ent := makeEntity(varName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_OBSERVED_OBJECT",
			"view_model_type", vmType)
		add(ent)
	}

	// 8. @Environment -> SCOPE.Pattern
	for _, m := range reSwiftUIEnvironment.FindAllStringSubmatchIndex(src, -1) {
		keyPath := src[m[2]:m[3]]
		ent := makeEntity("env:"+keyPath, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_ENVIRONMENT",
			"environment_key", keyPath)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
