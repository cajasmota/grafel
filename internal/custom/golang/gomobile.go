package golang

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// gomobile.go — a heuristic extractor for gomobile (golang.org/x/mobile),
// issue #3218 cluster 7 (Desktop & Mobile).
//
// gomobile produces mobile apps two ways:
//
//   - `gomobile build` of an app package importing golang.org/x/mobile/app —
//     an event-loop GUI app (app.Main(func(a app.App){...})) running on
//     Android/iOS; structure + lifecycle live in that event loop.
//   - `gomobile bind` — Go packages exposed to Java/Kotlin (Android) and
//     Objective-C/Swift (iOS) as a generated FFI binding. The bound surface is
//     the exported Go funcs/types reachable from the host Activity/Fragment/
//     ViewController.
//
// Statically resolvable surfaces from source text:
//
//   - app_struct  : `app.Main(...)` event-loop entry → SCOPE.Component
//                    (the mobile app root; the Structure.context_extraction
//                    surface).
//   - platform    : build-constraint platform conditionals —
//                    `//go:build android` / `// +build ios,android` and the
//                    `runtime.GOOS == "android"` guards → SCOPE.Pattern
//                    (pattern_kind=platform_branch; the Platform.platform_
//                    branching surface).
//   - native      : the gomobile native packages (app/gl/bind/sensor/...) that
//                    bridge to the host platform → SCOPE.External
//                    (the Native Bridge.native_module_imports surface).
//   - bound_func  : exported funcs in a package that imports a gomobile/bind
//                    surface — the FFI boundary crossed by the generated
//                    Java/ObjC bindings → SCOPE.External (subtype native_bridge).
//
// Honesty (registry coverage status, lang.go.framework.gomobile):
//
//   - Structure.context_extraction         → partial: app.Main entry detected;
//                                             no full component-tree resolution.
//   - Platform.platform_branching          → partial: build constraints +
//                                             GOOS guards detected heuristically
//                                             (no build-tag constraint solving).
//   - Native Bridge.native_module_imports  → partial: gomobile native package
//                                             imports detected by import match.
//   - Navigation.* (navigation/screen/deep_link) → not_applicable: gomobile bind
//                                             has no Go-side navigation framework;
//                                             screens/navigation live in the host
//                                             (Android/iOS) UI layer, not Go.
//   - Data Flow.state_management            → not_applicable: no Go-side reactive
//                                             store; state lives host-side.
//   - Lifecycle.state_setter_emission       → not_applicable: a React/SwiftUI-
//                                             style state-setter concept that has
//                                             no gomobile analogue.
//
// Attribution mirrors validation.go: emit only when a gomobile marker is
// present; stamp framework=gomobile on each entity.

func init() {
	extractor.Register("custom_go_gomobile", &gomobileExtractor{})
}

type gomobileExtractor struct{}

func (e *gomobileExtractor) Language() string { return "custom_go_gomobile" }

var (
	// reGomobileMarker attributes a file to gomobile via any x/mobile import.
	reGomobileMarker = regexp.MustCompile(`golang\.org/x/mobile`)

	// reGomobileAppMain matches the gomobile event-loop entry point.
	//   app.Main(func(a app.App) { ... })
	reGomobileAppMain = regexp.MustCompile(`(?m)\bapp\.Main\s*\(`)

	// reGomobileNativeImport matches the gomobile native bridge packages.
	reGomobileNativeImport = regexp.MustCompile(
		`golang\.org/x/mobile/(app|gl|bind|exp/[\w/]+|event/[\w/]+|asset|geom|sensor)\b`)

	// reGoBuildConstraint matches the modern `//go:build <expr>` line.
	reGoBuildConstraint = regexp.MustCompile(`(?m)^//go:build\s+(.+)$`)

	// reGoLegacyBuild matches the legacy `// +build <tags>` line.
	reGoLegacyBuild = regexp.MustCompile(`(?m)^// \+build\s+(.+)$`)

	// reGOOSGuard matches `runtime.GOOS == "android"` / `!= "ios"` platform
	// guards in code.
	reGOOSGuard = regexp.MustCompile(`runtime\.GOOS\s*(?:==|!=)\s*"(\w+)"`)

	// reExportedFunc matches a top-level exported func declaration (the FFI
	// surface of a `gomobile bind` package). Methods (with a receiver) are
	// excluded — they are not part of the generated top-level binding.
	reExportedFunc = regexp.MustCompile(`(?m)^func\s+([A-Z]\w*)\s*\(`)
)

// mobilePlatformTokens are the GOOS/build tags that name a mobile platform; a
// build-constraint or GOOS guard mentioning one of these is treated as a
// platform conditional.
var mobilePlatformTokens = map[string]bool{
	"android": true,
	"ios":     true,
	"darwin":  true, // iOS builds report darwin
}

// hasMobilePlatformToken reports whether a build-constraint expression or GOOS
// value references a mobile platform token.
func hasMobilePlatformToken(expr string) bool {
	for _, tok := range regexp.MustCompile(`[\w]+`).FindAllString(strings.ToLower(expr), -1) {
		if mobilePlatformTokens[tok] {
			return true
		}
	}
	return false
}

func (e *gomobileExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.gomobile_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "gomobile"),
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
	if !reGomobileMarker.MatchString(src) {
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

	// 1. app.Main — the mobile app root (Structure.context_extraction).
	for _, m := range reGomobileAppMain.FindAllStringSubmatchIndex(src, -1) {
		ent := makeEntity("gomobile:app:Main", "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gomobile", "provenance", "INFERRED_FROM_GOMOBILE_APP",
			"gomobile_kind", "app_root")
		add(ent)
	}

	// 2. platform conditionals — build constraints + GOOS guards
	//    (Platform.platform_branching). Only mobile-platform-bearing ones count.
	addPlatformBranch := func(form, expr string, line int) {
		if !hasMobilePlatformToken(expr) {
			return
		}
		expr = strings.TrimSpace(expr)
		ent := makeEntity("gomobile:platform:"+form+":"+expr, "SCOPE.Pattern", "", file.Path, file.Language, line)
		setProps(&ent, "framework", "gomobile", "provenance", "INFERRED_FROM_GOMOBILE_PLATFORM",
			"pattern_kind", "platform_branch", "branch_form", form, "constraint", expr)
		add(ent)
	}
	for _, m := range reGoBuildConstraint.FindAllStringSubmatchIndex(src, -1) {
		addPlatformBranch("go_build", submatch(src, m, 2), lineOf(src, m[0]))
	}
	for _, m := range reGoLegacyBuild.FindAllStringSubmatchIndex(src, -1) {
		addPlatformBranch("legacy_build", submatch(src, m, 2), lineOf(src, m[0]))
	}
	for _, m := range reGOOSGuard.FindAllStringSubmatchIndex(src, -1) {
		addPlatformBranch("goos_guard", submatch(src, m, 2), lineOf(src, m[0]))
	}

	// 3. native bridge package imports (Native Bridge.native_module_imports).
	boundPkg := false
	for _, m := range reGomobileNativeImport.FindAllStringSubmatchIndex(src, -1) {
		pkg := submatch(src, m, 0)
		sub := submatch(src, m, 2)
		if sub == "bind" {
			boundPkg = true
		}
		ent := makeEntity("gomobile:native:"+pkg, "SCOPE.External", "native_import", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gomobile", "provenance", "INFERRED_FROM_GOMOBILE_NATIVE_IMPORT",
			"native_kind", "gomobile", "import_path", pkg)
		add(ent)
	}

	// 4. exported funcs in a bind package — the FFI boundary (native_bridge).
	//    Gated on the bind import so we don't treat every exported func in a
	//    plain x/mobile/app GUI program as a bound FFI symbol.
	if boundPkg {
		for _, m := range reExportedFunc.FindAllStringSubmatchIndex(src, -1) {
			fn := submatch(src, m, 2)
			ent := makeEntity("gomobile:bound:"+fn, "SCOPE.External", "native_bridge", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "gomobile", "provenance", "INFERRED_FROM_GOMOBILE_BIND",
				"native_kind", "bound_func", "func_name", fn)
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
