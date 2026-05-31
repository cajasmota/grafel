package kotlin_test

import (
	"strings"
	"testing"
)

// dagger_hilt_test.go — value-asserting tests for the Dagger/Hilt DI extractor
// (record lang.kotlin.framework.dagger-hilt; cells di_binding_extraction /
// di_injection_point / di_scope_resolution → full).

const hiltModuleSrc = `
package com.example.di

import dagger.Module
import dagger.Provides
import dagger.Binds
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Inject
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {

    @Provides
    @Singleton
    fun provideRepo(api: ApiService, clock: Clock): Repo = RepoImpl(api, clock)

    @Provides
    fun provideClock(): Clock = SystemClock()
}

@Module
@InstallIn(SingletonComponent::class)
abstract class BindModule {
    @Binds
    abstract fun bindLogger(impl: ConsoleLogger): Logger
}

class UserService @Inject constructor(
    private val repo: Repo,
    private val logger: Logger,
)

@HiltViewModel
class ProfileViewModel @Inject constructor(private val repo: Repo) : ViewModel()

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject lateinit var userService: UserService
}

@HiltAndroidApp
class App : Application()
`

func TestHilt_ProvidesBinding(t *testing.T) {
	ents := extract(t, "custom_kotlin_dagger_hilt", fi("NetworkModule.kt", "kotlin", hiltModuleSrc))
	if len(ents) == 0 {
		t.Fatalf("[hilt] expected entities, got none")
	}

	byType := map[string]*entitySummary{}
	for i := range ents {
		if ents[i].Subtype == "di_binding" && ents[i].Props["binding_style"] == "provides" {
			byType[ents[i].Props["binding_type"]] = &ents[i]
		}
	}

	// @Provides @Singleton fun provideRepo(api, clock): Repo
	repo := byType["Repo"]
	if repo == nil {
		t.Fatalf("[hilt] missing Repo provides binding; got %v", byType)
	}
	if repo.Props["di_scope"] != "singleton" {
		t.Errorf("[hilt] Repo di_scope = %q, want singleton (method @Singleton)", repo.Props["di_scope"])
	}
	if repo.Props["implementation"] != "Repo" {
		t.Errorf("[hilt] Repo implementation = %q, want Repo", repo.Props["implementation"])
	}
	if repo.Props["provider_method"] != "provideRepo" {
		t.Errorf("[hilt] Repo provider_method = %q, want provideRepo", repo.Props["provider_method"])
	}
	if repo.Props["injected_dep_count"] != "2" {
		t.Errorf("[hilt] Repo injected_dep_count = %q, want 2", repo.Props["injected_dep_count"])
	}
	if !strings.Contains(repo.Props["injected_deps"], "ApiService") || !strings.Contains(repo.Props["injected_deps"], "Clock") {
		t.Errorf("[hilt] Repo injected_deps = %q, want ApiService and Clock", repo.Props["injected_deps"])
	}
	if repo.Props["component"] != "SingletonComponent" {
		t.Errorf("[hilt] Repo component = %q, want SingletonComponent", repo.Props["component"])
	}

	// @Provides fun provideClock(): Clock — no method scope → component scope.
	clock := byType["Clock"]
	if clock == nil {
		t.Fatalf("[hilt] missing Clock provides binding")
	}
	if clock.Props["di_scope"] != "singleton" {
		t.Errorf("[hilt] Clock di_scope = %q, want singleton (from @InstallIn component)", clock.Props["di_scope"])
	}
}

func TestHilt_BindsBinding(t *testing.T) {
	ents := extract(t, "custom_kotlin_dagger_hilt", fi("BindModule.kt", "kotlin", hiltModuleSrc))

	var logger *entitySummary
	for i := range ents {
		if ents[i].Subtype == "di_binding" && ents[i].Props["binding_style"] == "binds" && ents[i].Props["binding_type"] == "Logger" {
			logger = &ents[i]
		}
	}
	// @Binds abstract fun bindLogger(impl: ConsoleLogger): Logger
	if logger == nil {
		t.Fatalf("[hilt] missing Logger binds binding")
	}
	if logger.Props["binding_type"] != "Logger" {
		t.Errorf("[hilt] Logger binding_type = %q, want Logger (interface)", logger.Props["binding_type"])
	}
	if logger.Props["implementation"] != "ConsoleLogger" {
		t.Errorf("[hilt] Logger implementation = %q, want ConsoleLogger (impl param)", logger.Props["implementation"])
	}
}

func TestHilt_ConstructorInjection(t *testing.T) {
	ents := extract(t, "custom_kotlin_dagger_hilt", fi("UserService.kt", "kotlin", hiltModuleSrc))

	ctorByType := map[string]*entitySummary{}
	for i := range ents {
		if ents[i].Subtype == "di_injection_point" && ents[i].Props["mechanism"] == "constructor_inject" {
			ctorByType[ents[i].Props["injected_type"]] = &ents[i]
		}
	}
	// UserService @Inject constructor(repo: Repo, logger: Logger)
	if ctorByType["Repo"] == nil {
		t.Errorf("[hilt] missing constructor injection point for Repo; got %v", ctorByType)
	}
	if ctorByType["Logger"] == nil {
		t.Errorf("[hilt] missing constructor injection point for Logger")
	}
	if ip := ctorByType["Repo"]; ip != nil && ip.Props["field_name"] != "repo" {
		t.Errorf("[hilt] Repo injection field_name = %q, want repo", ip.Props["field_name"])
	}
}

func TestHilt_FieldInjection(t *testing.T) {
	ents := extract(t, "custom_kotlin_dagger_hilt", fi("MainActivity.kt", "kotlin", hiltModuleSrc))

	var field *entitySummary
	for i := range ents {
		if ents[i].Subtype == "di_injection_point" && ents[i].Props["mechanism"] == "field_inject" {
			field = &ents[i]
		}
	}
	// @Inject lateinit var userService: UserService
	if field == nil {
		t.Fatalf("[hilt] missing field injection point")
	}
	if field.Props["field_name"] != "userService" || field.Props["injected_type"] != "UserService" {
		t.Errorf("[hilt] field inject = field %q type %q, want userService/UserService",
			field.Props["field_name"], field.Props["injected_type"])
	}
}

func TestHilt_EntryPointsScope(t *testing.T) {
	ents := extract(t, "custom_kotlin_dagger_hilt", fi("App.kt", "kotlin", hiltModuleSrc))

	byMarker := map[string]*entitySummary{}
	for i := range ents {
		if ents[i].Subtype == "di_scope_resolution" {
			byMarker[ents[i].Props["entry_point_kind"]] = &ents[i]
		}
	}

	// @HiltAndroidApp class App → singleton scope.
	app := byMarker["HiltAndroidApp"]
	if app == nil {
		t.Fatalf("[hilt] missing HiltAndroidApp entry point; got %v", byMarker)
	}
	if app.Props["component_class"] != "App" || app.Props["di_scope"] != "singleton" {
		t.Errorf("[hilt] HiltAndroidApp = class %q scope %q, want App/singleton",
			app.Props["component_class"], app.Props["di_scope"])
	}

	// @HiltViewModel class ProfileViewModel → viewmodel scope.
	vm := byMarker["HiltViewModel"]
	if vm == nil || vm.Props["component_class"] != "ProfileViewModel" || vm.Props["di_scope"] != "viewmodel" {
		t.Errorf("[hilt] HiltViewModel entry point wrong: %+v", vm)
	}

	// @AndroidEntryPoint class MainActivity → activity scope.
	act := byMarker["AndroidEntryPoint"]
	if act == nil || act.Props["component_class"] != "MainActivity" || act.Props["di_scope"] != "activity" {
		t.Errorf("[hilt] AndroidEntryPoint entry point wrong: %+v", act)
	}
}

func TestHilt_NonHiltSource(t *testing.T) {
	src := `
package com.example
fun hello() = "world"
`
	ents := extract(t, "custom_kotlin_dagger_hilt", fi("Hello.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("[hilt] expected 0 entities for non-Hilt file, got %d", len(ents))
	}
}

func TestHilt_EmptySource(t *testing.T) {
	ents := extract(t, "custom_kotlin_dagger_hilt", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[hilt] expected 0 entities for empty file, got %d", len(ents))
	}
}

func TestHilt_JavaSourceIgnored(t *testing.T) {
	// Dagger annotations are identical in Java; this extractor is gated to
	// kotlin so the Java pipeline stays untouched.
	ents := extract(t, "custom_kotlin_dagger_hilt", fi("NetworkModule.java", "java", hiltModuleSrc))
	if len(ents) != 0 {
		t.Errorf("[hilt] expected 0 entities for java source, got %d", len(ents))
	}
}
