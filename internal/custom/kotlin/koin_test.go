package kotlin_test

import (
	"strings"
	"testing"
)

// koin_test.go — value-asserting tests for the Koin DI extractor (record
// lang.kotlin.framework.koin; cells di_binding_extraction / di_injection_point
// / di_scope_resolution → full).

const koinModuleSrc = `
package com.example.di

import org.koin.dsl.module
import org.koin.androidx.viewmodel.dsl.viewModel
import org.koin.core.module.dsl.singleOf

val appModule = module {
    single { UserService(get()) }
    factory { Repo(get()) }
    viewModel { ProfileViewModel(get()) }
    single<Logger> { ConsoleLogger() }
    scoped { SessionCache(get(), get()) }
    singleOf(::AnalyticsClient)
}

class ProfileController : KoinComponent {
    val userService: UserService by inject()
    val repo = get<Repo>()
}
`

func TestKoin_BindingScopeAndType(t *testing.T) {
	ents := extract(t, "custom_kotlin_koin", fi("AppModule.kt", "kotlin", koinModuleSrc))
	if len(ents) == 0 {
		t.Fatalf("[koin] expected entities, got none")
	}

	byType := map[string]*entitySummary{}
	for i := range ents {
		if ents[i].Subtype == "di_binding" {
			if bt := ents[i].Props["binding_type"]; bt != "" {
				byType[bt] = &ents[i]
			}
		}
	}

	// single { UserService(get()) } → scope=single, impl=UserService, 1 dep.
	us := byType["UserService"]
	if us == nil {
		t.Fatalf("[koin] missing UserService binding; got %v", byType)
	}
	if us.Props["di_scope"] != "single" {
		t.Errorf("[koin] UserService di_scope = %q, want single", us.Props["di_scope"])
	}
	if us.Props["implementation"] != "UserService" {
		t.Errorf("[koin] UserService implementation = %q, want UserService", us.Props["implementation"])
	}

	// factory { Repo(get()) } → scope=factory.
	repo := byType["Repo"]
	if repo == nil || repo.Props["di_scope"] != "factory" {
		t.Errorf("[koin] Repo binding scope wrong: %+v", repo)
	}

	// viewModel { ProfileViewModel(get()) } → scope=viewModel.
	vm := byType["ProfileViewModel"]
	if vm == nil || vm.Props["di_scope"] != "viewModel" {
		t.Errorf("[koin] ProfileViewModel binding scope wrong: %+v", vm)
	}

	// single<Logger> { ConsoleLogger() } → explicit binding_type=Logger,
	// implementation=ConsoleLogger.
	logger := byType["Logger"]
	if logger == nil {
		t.Fatalf("[koin] missing Logger binding")
	}
	if logger.Props["di_scope"] != "single" {
		t.Errorf("[koin] Logger di_scope = %q, want single", logger.Props["di_scope"])
	}
	if logger.Props["implementation"] != "ConsoleLogger" {
		t.Errorf("[koin] Logger implementation = %q, want ConsoleLogger", logger.Props["implementation"])
	}

	// scoped { SessionCache(get(), get()) } → scope=scoped, 2 deps.
	sc := byType["SessionCache"]
	if sc == nil {
		t.Fatalf("[koin] missing SessionCache binding")
	}
	if sc.Props["di_scope"] != "scoped" {
		t.Errorf("[koin] SessionCache di_scope = %q, want scoped", sc.Props["di_scope"])
	}
	if sc.Props["injected_dep_count"] != "2" {
		t.Errorf("[koin] SessionCache injected_dep_count = %q, want 2", sc.Props["injected_dep_count"])
	}

	// singleOf(::AnalyticsClient) → constructor_ref binding.
	ac := byType["AnalyticsClient"]
	if ac == nil {
		t.Fatalf("[koin] missing AnalyticsClient (singleOf) binding")
	}
	if ac.Props["di_scope"] != "single" {
		t.Errorf("[koin] AnalyticsClient di_scope = %q, want single", ac.Props["di_scope"])
	}
	if ac.Props["binding_style"] != "constructor_ref" {
		t.Errorf("[koin] AnalyticsClient binding_style = %q, want constructor_ref", ac.Props["binding_style"])
	}
}

func TestKoin_InjectionPoints(t *testing.T) {
	ents := extract(t, "custom_kotlin_koin", fi("AppModule.kt", "kotlin", koinModuleSrc))

	var byInject, getCall *entitySummary
	for i := range ents {
		if ents[i].Subtype != "di_injection_point" {
			continue
		}
		switch ents[i].Props["mechanism"] {
		case "property_inject":
			byInject = &ents[i]
		case "get_call":
			getCall = &ents[i]
		}
	}

	// val userService: UserService by inject()
	if byInject == nil {
		t.Fatal("[koin] missing property_inject injection point")
	}
	if byInject.Props["field_name"] != "userService" || byInject.Props["injected_type"] != "UserService" {
		t.Errorf("[koin] property_inject = field %q type %q, want userService/UserService",
			byInject.Props["field_name"], byInject.Props["injected_type"])
	}

	// val repo = get<Repo>()
	if getCall == nil {
		t.Fatal("[koin] missing get_call injection point")
	}
	if getCall.Props["field_name"] != "repo" || getCall.Props["injected_type"] != "Repo" {
		t.Errorf("[koin] get_call = field %q type %q, want repo/Repo",
			getCall.Props["field_name"], getCall.Props["injected_type"])
	}
}

func TestKoin_InjectedDeps(t *testing.T) {
	src := `
import org.koin.dsl.module
val m = module {
    single<OrderService> { OrderServiceImpl(get<Repo>(), get<Clock>()) }
}
`
	ents := extract(t, "custom_kotlin_koin", fi("Order.kt", "kotlin", src))
	var os *entitySummary
	for i := range ents {
		if ents[i].Props["binding_type"] == "OrderService" {
			os = &ents[i]
		}
	}
	if os == nil {
		t.Fatalf("[koin] missing OrderService binding; got %v", ents)
	}
	deps := os.Props["injected_deps"]
	if !strings.Contains(deps, "Repo") || !strings.Contains(deps, "Clock") {
		t.Errorf("[koin] OrderService injected_deps = %q, want Repo and Clock", deps)
	}
	if os.Props["implementation"] != "OrderServiceImpl" {
		t.Errorf("[koin] OrderService implementation = %q, want OrderServiceImpl", os.Props["implementation"])
	}
}

func TestKoin_NonKoinSource(t *testing.T) {
	src := `
package com.example
fun hello() = "world"
`
	ents := extract(t, "custom_kotlin_koin", fi("Hello.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("[koin] expected 0 entities for non-Koin file, got %d", len(ents))
	}
}

func TestKoin_EmptySource(t *testing.T) {
	ents := extract(t, "custom_kotlin_koin", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[koin] expected 0 entities for empty file, got %d", len(ents))
	}
}
