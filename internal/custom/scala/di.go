// Package scala — DI extractor for Scala frameworks that have a DI model.
//
// Covers (missing → partial):
//
//	Record                   Capability                   Status
//	───────────────────────────────────────────────────────────
//	framework.finatra        DI/di_binding_extraction     partial
//	framework.finatra        DI/di_injection_point        partial
//	framework.finatra        DI/di_scope_resolution       partial
//	framework.lagom          DI/di_binding_extraction     partial
//	framework.lagom          DI/di_injection_point        partial
//	framework.lagom          DI/di_scope_resolution       partial
//	framework.zio-http       DI/di_binding_extraction     partial
//	framework.zio-http       DI/di_injection_point        partial
//	framework.zio-http       DI/di_scope_resolution       partial
//
// DI models:
//
//	Finatra uses Google Guice via Twitter's inject library:
//	  - @Singleton, @Inject annotations on class constructors/fields
//	  - TwitterModule / AbstractModule bindings
//	  - bind[Service].to[ServiceImpl], bind[Repo].toInstance(...)
//
//	Lagom uses Guice via Play's DI layer:
//	  - LagomApplicationLoader + LagomApplication
//	  - bind[Service].to[ServiceImpl] in AbstractModule
//	  - @Singleton, @Inject standard Guice annotations
//
//	ZIO HTTP uses ZLayer (ZIO's native DI):
//	  - ZLayer.make[Env], ZLayer.scoped, ZLayer.succeed
//	  - type alias for ZIO layer composition (val layer: ZLayer[R, E, A])
//	  - provide / provideLayer / provideSomeLayer
//
// AOP: None of these frameworks use Spring AOP / AspectJ.
// Transactions: Finatra and Lagom can use Slick/c3p0 transactions but these
//
//	are ORM-layer concerns, not framework-enforced annotations → not_applicable.
//
// Honest limit: regex-based, file-local. Guice module wiring across files is
// not resolved. ZLayer cross-file composition is not traced. Cells are partial.
package scala

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
	extractor.Register("custom_scala_di", &scalaDIExtractor{})
}

type scalaDIExtractor struct{}

func (e *scalaDIExtractor) Language() string { return "custom_scala_di" }

// ---------------------------------------------------------------------------
// Guice (Finatra / Lagom) regexes
// ---------------------------------------------------------------------------

var (
	// reGuiceInject matches @Inject annotation:
	//   - class Foo @Inject()(params) — primary constructor injection
	//   - @Inject val foo: T — field injection
	//   - @Inject() def method — method injection
	reGuiceInject = regexp.MustCompile(
		`@Inject\s*(?:\(\s*\))?`)

	// reGuiceSingleton matches @Singleton class annotation.
	reGuiceSingleton = regexp.MustCompile(
		`@Singleton\s+(?:final\s+)?class\s+(\w+)`)

	// reGuiceBind matches bind[Type].to[Impl] or bind[Type].toInstance
	reGuiceBind = regexp.MustCompile(
		`\bbind\s*\[\s*([A-Z][\w]*)\s*\]\s*\.\s*(?:to\s*\[\s*([A-Z][\w]*)\s*\]|toInstance|toSelf|in\s*\[)`)

	// reGuiceModule matches extends TwitterModule / AbstractModule / LagomServicePortBindings
	reGuiceModule = regexp.MustCompile(
		`\bextends\s+(?:TwitterModule|AbstractModule|LagomServicePortBindings|ServiceLocatorModule)\b`)

	// reGuiceProvider matches @Provides def someService(...): T = ...
	reGuiceProvider = regexp.MustCompile(
		`@Provides\s+(?:@Singleton\s+)?def\s+(\w+)\s*\(`)
)

// ---------------------------------------------------------------------------
// ZLayer (ZIO HTTP) regexes
// ---------------------------------------------------------------------------

var (
	// reZLayerMake matches ZLayer.make[Env] or ZLayer.makeSome
	reZLayerMake = regexp.MustCompile(
		`\bZLayer\s*\.\s*(?:make|makeSome|makeWith)\s*\[\s*([A-Z][\w\s,]*)\s*\]`)

	// reZLayerSucceed matches ZLayer.succeed(...)
	reZLayerSucceed = regexp.MustCompile(
		`\bZLayer\s*\.\s*(?:succeed|fromZIO|scoped|fromManaged)\s*\(`)

	// reZLayerVal matches val layer: ZLayer[R, E, A] = ...
	reZLayerVal = regexp.MustCompile(
		`\bval\s+(\w+(?:Layer|Env)?)\s*:\s*ZLayer\s*\[`)

	// reZLayerProvide matches .provide(...) or .provideLayer(...) call
	reZLayerProvide = regexp.MustCompile(
		`\.\s*(?:provide|provideLayer|provideSomeLayer)\s*\(`)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *scalaDIExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.scala_di.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("extractor", "di"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	fw := detectScalaFramework(src)
	isGuice := fw == "finatra" || fw == "lagom"
	isZIO := fw == "zio-http"

	if !isGuice && !isZIO {
		return nil, nil
	}

	if isGuice {
		// --- di_binding_extraction: bind[T].to[Impl] ---
		for _, m := range reGuiceBind.FindAllStringSubmatchIndex(src, -1) {
			iface := src[m[2]:m[3]]
			impl := ""
			if m[4] >= 0 {
				impl = src[m[4]:m[5]]
			}
			name := "bind:" + iface
			if impl != "" {
				name += "→" + impl
			}
			ent := makeEntity(name, "SCOPE.DI", "di_binding", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", fw, "interface", iface, "impl", impl, "provenance", "GUICE_BIND")
			add(ent)
		}

		// --- di_injection_point: @Inject ---
		for _, m := range reGuiceInject.FindAllStringSubmatchIndex(src, -1) {
			ent := makeEntity("inject:"+fileBaseName(file.Path), "SCOPE.DI", "injection_point", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", fw, "injection_type", "constructor_or_field", "provenance", "GUICE_INJECT")
			add(ent)
		}

		// --- di_scope_resolution: @Singleton / @Provides ---
		for _, m := range reGuiceSingleton.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			ent := makeEntity("singleton:"+name, "SCOPE.DI", "di_scope", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", fw, "scope", "singleton", "provenance", "GUICE_SINGLETON")
			add(ent)
		}

		// Module declarations
		for _, m := range reGuiceModule.FindAllStringSubmatchIndex(src, -1) {
			ent := makeEntity("module:"+fileBaseName(file.Path), "SCOPE.DI", "di_module", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", fw, "provenance", "GUICE_MODULE")
			add(ent)
		}

		// @Provides methods
		for _, m := range reGuiceProvider.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			ent := makeEntity("provides:"+name, "SCOPE.DI", "di_binding", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", fw, "provenance", "GUICE_PROVIDES")
			add(ent)
		}
	}

	if isZIO {
		// --- di_binding_extraction: ZLayer definitions ---
		for _, m := range reZLayerMake.FindAllStringSubmatchIndex(src, -1) {
			envType := src[m[2]:m[3]]
			ent := makeEntity("zlayer:"+envType, "SCOPE.DI", "di_binding", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "zio-http", "env_type", envType, "provenance", "ZIO_ZLAYER_MAKE")
			add(ent)
		}
		for _, m := range reZLayerSucceed.FindAllStringSubmatchIndex(src, -1) {
			ent := makeEntity("zlayer:impl:"+fileBaseName(file.Path), "SCOPE.DI", "di_binding", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "zio-http", "provenance", "ZIO_ZLAYER_SUCCEED")
			add(ent)
		}

		// --- di_injection_point: provide/provideLayer calls ---
		for _, m := range reZLayerProvide.FindAllStringSubmatchIndex(src, -1) {
			ent := makeEntity("provide:"+fileBaseName(file.Path), "SCOPE.DI", "injection_point", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "zio-http", "provenance", "ZIO_PROVIDE_LAYER")
			add(ent)
		}

		// --- di_scope_resolution: typed ZLayer val declarations ---
		for _, m := range reZLayerVal.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			ent := makeEntity("layer:"+name, "SCOPE.DI", "di_scope", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "zio-http", "scope", "zlayer", "provenance", "ZIO_ZLAYER_VAL")
			add(ent)
		}
	}

	return entities, nil
}
