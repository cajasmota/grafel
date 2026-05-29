package golang

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// fyne.go — a heuristic extractor for the Fyne desktop-GUI toolkit
// (fyne.io/fyne/v2), issue #3218 cluster 7 (Desktop & Mobile).
//
// Fyne is a single-process, non-HTTP desktop/mobile GUI toolkit. There is no
// route table, no request/response cycle, and — unlike Electron — no
// main/renderer process split or IPC channel. The structural surface that IS
// statically resolvable from source text is the widget/window/event lifecycle:
//
//   - app          : the `app.New()` / `app.NewWithID(...)` application root
//                    → SCOPE.Component (the GUI application object).
//   - window       : `a.NewWindow("Title")` top-level windows
//                    → SCOPE.UIComponent (subtype "window").
//   - widget       : `widget.NewButton(...)`, `widget.NewLabel(...)`, etc.
//                    construction → SCOPE.UIComponent (subtype "widget").
//   - event        : event-handler wiring — `btn.OnTapped = ...`,
//                    `widget.NewButton("x", func(){...})`, `entry.OnChanged`,
//                    `w.SetOnClosed(...)` → SCOPE.Pattern (pattern_kind=event).
//   - native_import: the fyne driver/native package imports that back the
//                    GUI on the host platform → SCOPE.External
//                    (the desktop `native_module_imports` capability surface).
//
// Honesty (registry coverage status, lang.go.framework.fyne):
//
//   - Process.ipc_extraction        → not_applicable: Fyne is single-process;
//                                      there is no IPC channel to extract.
//   - Process.main_renderer_split   → not_applicable: no main/renderer split
//                                      (that is an Electron/Tauri concept).
//   - Native.native_module_imports  → partial: we detect fyne driver/native
//                                      package imports via a heuristic import
//                                      match. We do NOT resolve cgo symbols or
//                                      confirm a platform binding is reached.
//
// The widget/window/event entities are graph value (they populate the GUI
// component tree) but the desktop subcategory has no dedicated capability cell
// for them, so they do not flip a registry cell on their own.
//
// Attribution mirrors observability.go/validation.go: the scanner runs on
// every Go file and only emits when a Fyne marker is present, stamping
// framework=fyne on each entity. A file with no Fyne marker emits nothing.

func init() {
	extractor.Register("custom_go_fyne", &fyneExtractor{})
}

type fyneExtractor struct{}

func (e *fyneExtractor) Language() string { return "custom_go_fyne" }

var (
	// reFyneMarker attributes a file to Fyne via an import path or a canonical
	// package selector. Either the module import or an `app.`/`widget.` use of
	// the toolkit qualifies.
	reFyneMarker = regexp.MustCompile(`fyne\.io/fyne`)

	// reFyneApp matches the application-root constructors.
	//   a := app.New()
	//   a := app.NewWithID("com.example.app")
	reFyneApp = regexp.MustCompile(`(?m)(\w+)\s*:?=\s*app\.(New|NewWithID)\s*\(`)

	// reFyneWindow matches top-level window construction.
	//   w := a.NewWindow("Hello")
	reFyneWindow = regexp.MustCompile(`(?m)(\w+)\s*:?=\s*\w+\.NewWindow\s*\(\s*"([^"]*)"`)

	// reFyneWidget matches widget constructors: widget.NewButton, widget.NewLabel,
	// widget.NewEntry, container.NewVBox, etc. Captures the constructor name.
	reFyneWidget = regexp.MustCompile(`(?m)(?:widget|container|canvas)\.(New\w+)\s*\(`)

	// reFyneEventField matches event-handler field assignments:
	//   btn.OnTapped = func() { ... }
	//   entry.OnChanged = handler
	reFyneEventField = regexp.MustCompile(`(?m)(\w+)\.(On\w+)\s*=`)

	// reFyneEventSetter matches event-handler setter methods:
	//   w.SetOnClosed(func(){...})
	//   w.SetCloseIntercept(...)
	reFyneEventSetter = regexp.MustCompile(`(?m)\w+\.(SetOn\w+|SetCloseIntercept)\s*\(`)

	// reFyneNativeImport matches the fyne driver/native packages that back the
	// GUI on the host platform — the desktop native_module_imports surface.
	reFyneNativeImport = regexp.MustCompile(
		`fyne\.io/fyne(?:/v\d+)?/(driver(?:/[\w/]+)?|internal/driver[\w/]*|app)\b`)
)

func (e *fyneExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.fyne_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "fyne"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "go" {
		return nil, nil
	}

	src := string(file.Content)
	if !reFyneMarker.MatchString(src) {
		return nil, nil
	}

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

	// 1. app.New()/app.NewWithID() — the GUI application root.
	for _, m := range reFyneApp.FindAllStringSubmatchIndex(src, -1) {
		varName := submatch(src, m, 2)
		ctor := submatch(src, m, 4)
		ent := makeEntity("fyne:app:"+varName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fyne", "provenance", "INFERRED_FROM_FYNE_APP",
			"fyne_kind", "app", "constructor", ctor)
		add(ent)
	}

	// 2. NewWindow — top-level windows.
	for _, m := range reFyneWindow.FindAllStringSubmatchIndex(src, -1) {
		varName := submatch(src, m, 2)
		title := submatch(src, m, 4)
		ent := makeEntity("fyne:window:"+varName, "SCOPE.UIComponent", "window", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fyne", "provenance", "INFERRED_FROM_FYNE_WINDOW",
			"fyne_kind", "window", "window_title", title)
		add(ent)
	}

	// 3. widget/container/canvas constructors.
	for _, m := range reFyneWidget.FindAllStringSubmatchIndex(src, -1) {
		ctor := submatch(src, m, 2)
		ent := makeEntity("fyne:widget:"+ctor, "SCOPE.UIComponent", "widget", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fyne", "provenance", "INFERRED_FROM_FYNE_WIDGET",
			"fyne_kind", "widget", "constructor", ctor)
		add(ent)
	}

	// 4. event-handler wiring — field assignments + setter methods.
	for _, m := range reFyneEventField.FindAllStringSubmatchIndex(src, -1) {
		recv := submatch(src, m, 2)
		handler := submatch(src, m, 4)
		ent := makeEntity("fyne:event:"+recv+"."+handler, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fyne", "provenance", "INFERRED_FROM_FYNE_EVENT",
			"pattern_kind", "event", "event_name", handler, "receiver", recv)
		add(ent)
	}
	for _, m := range reFyneEventSetter.FindAllStringSubmatchIndex(src, -1) {
		handler := submatch(src, m, 2)
		ent := makeEntity("fyne:event:"+handler, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fyne", "provenance", "INFERRED_FROM_FYNE_EVENT",
			"pattern_kind", "event", "event_name", handler)
		add(ent)
	}

	// 5. native driver/app package imports — the native_module_imports surface.
	for _, m := range reFyneNativeImport.FindAllStringSubmatchIndex(src, -1) {
		pkg := submatch(src, m, 0)
		ent := makeEntity("fyne:native:"+pkg, "SCOPE.External", "native_import", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fyne", "provenance", "INFERRED_FROM_FYNE_NATIVE_IMPORT",
			"native_kind", "fyne_driver", "import_path", pkg)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
