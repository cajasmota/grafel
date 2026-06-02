package swift

import (
	"context"
	"regexp"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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
	// NavigationLink(value: …) { Text("Detail") } — value-based navigation
	// whose concrete destination is declared elsewhere via
	// .navigationDestination(for:). The destination View is not statically
	// resolvable in-file, so we record the value type as the nav target.
	reSwiftUINavLinkValue = regexp.MustCompile(
		`NavigationLink\s*\(\s*value:\s*([A-Za-z_][A-Za-z0-9_.]*)`,
	)
	reSwiftUINavDestination = regexp.MustCompile(
		`\.navigationDestination\s*\(for:\s*(\w+)\.self`,
	)
	// .sheet(isPresented: …) { DetailView() } / .fullScreenCover(...) { … }
	// modal navigation transitions whose presented View we capture as the
	// destination of a NAVIGATES_TO edge.
	reSwiftUIModalPresent = regexp.MustCompile(
		`\.(sheet|fullScreenCover)\s*\([^)]*\)\s*\{\s*(?:[A-Za-z_][A-Za-z0-9_]*\s+in\s*)?([A-Z][A-Za-z0-9_]*)\s*\(`,
	)
	reSwiftUIState = regexp.MustCompile(
		`@State\s+(?:private\s+)?var\s+(\w+)\s*:`,
	)
	reSwiftUIBinding = regexp.MustCompile(
		`@Binding\s+var\s+(\w+)\s*:`,
	)
	// Matches both the type-annotated form (`@ObservedObject var x: Vm`) and the
	// initializer form (`@StateObject var x = Vm()`). Exactly one of the type
	// (group 2) or the init-type (group 3) is populated per match.
	reSwiftUIObservedObject = regexp.MustCompile(
		`@(?:ObservedObject|EnvironmentObject|StateObject)\s+var\s+(\w+)\s*(?::\s*(\w+)|=\s*([A-Z]\w*)\s*\()`,
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

	// viewSpans records each emitted View struct's start offset + entity ID so
	// edges (navigation, observable USES) can be attributed to the enclosing
	// View. A match at offset O belongs to the View with the greatest start
	// offset <= O.
	type viewSpan struct {
		start int
		id    string
		name  string
	}
	var viewSpans []viewSpan

	// 1. struct/class conforming to View -> SCOPE.UIComponent
	for _, m := range reSwiftUIViewConformance.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if swiftUIBuiltinViews[name] {
			continue
		}
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_VIEW")
		add(ent)
		viewSpans = append(viewSpans, viewSpan{start: m[0], id: ent.ID, name: name})
	}

	// enclosingView returns the (id, name) of the View whose declaration most
	// closely precedes offset, or ("","") if none — used as the FromID of
	// navigation / USES edges.
	enclosingView := func(offset int) (string, string) {
		id, name := "", ""
		best := -1
		for _, vs := range viewSpans {
			if vs.start <= offset && vs.start > best {
				best = vs.start
				id, name = vs.id, vs.name
			}
		}
		return id, name
	}

	// relsByView buffers relationships keyed by the source View's entity ID;
	// they are flushed onto the entities after all emission completes (so slice
	// growth from later add() calls can't invalidate pointers).
	relsByView := make(map[string][]types.RelationshipRecord)
	attach := func(id string, rel types.RelationshipRecord) {
		if id == "" {
			return
		}
		relsByView[id] = append(relsByView[id], rel)
	}

	// 2. NavigationStack -> signals navigation context
	for _, m := range reSwiftUINavStack.FindAllStringIndex(src, -1) {
		ent := makeEntity("NavigationStack", "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_NAV_STACK")
		add(ent)
	}

	// 3. NavigationLink destinations -> SCOPE.Operation/endpoint + NAVIGATES_TO
	for _, m := range reSwiftUINavLink.FindAllStringSubmatchIndex(src, -1) {
		dest := src[m[2]:m[3]]
		if swiftUIBuiltinViews[dest] {
			continue
		}
		ent := makeEntity("nav:"+dest, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_NAV_LINK",
			"destination", dest)
		add(ent)

		// NAVIGATES_TO: enclosing View -> destination View. ToID is a synthetic
		// "view:<Dest>" stub the linker resolves against the destination's
		// SCOPE.UIComponent (cross-file resolution happens in the engine).
		if fromID, fromName := enclosingView(m[0]); fromID != "" {
			attach(fromID, types.RelationshipRecord{
				FromID: fromID,
				ToID:   "view:" + dest,
				Kind:   "NAVIGATES_TO",
				Properties: map[string]string{
					"framework":   "swiftui",
					"destination": dest,
					"from_view":   fromName,
					"line":        itoa(lineOf(src, m[0])),
					"via":         "navigation_link",
				},
			})
		}
	}

	// 3b. NavigationLink(value:) -> NAVIGATES_TO (value-based; concrete
	// destination declared via .navigationDestination(for:), not in-file).
	for _, m := range reSwiftUINavLinkValue.FindAllStringSubmatchIndex(src, -1) {
		val := src[m[2]:m[3]]
		if fromID, fromName := enclosingView(m[0]); fromID != "" {
			attach(fromID, types.RelationshipRecord{
				FromID: fromID,
				ToID:   "navvalue:" + val,
				Kind:   "NAVIGATES_TO",
				// Honest-partial: the destination View backing this value type is
				// resolved elsewhere via .navigationDestination(for:) and cannot
				// be followed in-file.
				Confidence: 0.6,
				Properties: map[string]string{
					"framework": "swiftui",
					"value":     val,
					"from_view": fromName,
					"line":      itoa(lineOf(src, m[0])),
					"via":       "navigation_link_value",
					"partial":   "destination_resolved_via_navigationDestination",
				},
			})
		}
	}

	// 3c. .sheet / .fullScreenCover -> NAVIGATES_TO (modal transitions).
	for _, m := range reSwiftUIModalPresent.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		dest := src[m[4]:m[5]]
		if swiftUIBuiltinViews[dest] {
			continue
		}
		if fromID, fromName := enclosingView(m[0]); fromID != "" {
			attach(fromID, types.RelationshipRecord{
				FromID: fromID,
				ToID:   "view:" + dest,
				Kind:   "NAVIGATES_TO",
				Properties: map[string]string{
					"framework":   "swiftui",
					"destination": dest,
					"from_view":   fromName,
					"line":        itoa(lineOf(src, m[0])),
					"via":         "modal_" + kind,
				},
			})
		}
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

	// 7. @ObservedObject/@EnvironmentObject/@StateObject -> SCOPE.Component
	//    + USES edge: enclosing View -> observable type (view model / app state).
	for _, m := range reSwiftUIObservedObject.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		vmType := ""
		if m[4] >= 0 { // `: Type` annotation form
			vmType = src[m[4]:m[5]]
		} else if m[6] >= 0 { // `= Type(...)` initializer form
			vmType = src[m[6]:m[7]]
		}
		if vmType == "" {
			continue
		}
		ent := makeEntity(varName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_OBSERVED_OBJECT",
			"view_model_type", vmType)
		add(ent)

		// USES: View -> observable type. The wrapper token (@StateObject etc.)
		// is recorded for traceability. ToID is a synthetic "type:<Vm>" stub the
		// linker resolves to the observable's declaration (often cross-file).
		wrapper := "@ObservedObject"
		if idx := indexBackWrapper(src, m[0]); idx != "" {
			wrapper = idx
		}
		if fromID, fromName := enclosingView(m[0]); fromID != "" {
			attach(fromID, types.RelationshipRecord{
				FromID: fromID,
				ToID:   "type:" + vmType,
				Kind:   "USES",
				Properties: map[string]string{
					"framework":        "swiftui",
					"observable_type":  vmType,
					"property":         varName,
					"property_wrapper": wrapper,
					"from_view":        fromName,
					"line":             itoa(lineOf(src, m[0])),
					"via":              "observable_object",
				},
			})
		}
	}

	// 8. @Environment -> SCOPE.Pattern
	for _, m := range reSwiftUIEnvironment.FindAllStringSubmatchIndex(src, -1) {
		keyPath := src[m[2]:m[3]]
		ent := makeEntity("env:"+keyPath, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "swiftui", "provenance", "INFERRED_FROM_SWIFTUI_ENVIRONMENT",
			"environment_key", keyPath)
		add(ent)
	}

	// Flush buffered relationships onto their source View entities. Done after
	// all add() calls so slice growth cannot invalidate references.
	if len(relsByView) > 0 {
		for i := range entities {
			if rels, ok := relsByView[entities[i].ID]; ok {
				entities[i].Relationships = append(entities[i].Relationships, rels...)
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// indexBackWrapper looks back from offset (the start of an @Observed/State/...
// match) to recover which property wrapper token was used. The shared regex
// matches three alternatives without capturing which one, so we re-scan the
// matched prefix.
func indexBackWrapper(src string, offset int) string {
	// The match begins at the '@'; read "@" + the wrapper identifier.
	if offset >= len(src) || src[offset] != '@' {
		return ""
	}
	end := offset + 1
	for end < len(src) && isIdentByte(src[end]) {
		end++
	}
	if end > offset+1 {
		return src[offset:end]
	}
	return ""
}

func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}
