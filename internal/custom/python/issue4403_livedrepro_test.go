package python_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/python"
	_ "github.com/cajasmota/archigraph/internal/extractors/python"
)

// Issue #4403 LIVE-REPRO — Django global wiring, remaining settings shapes.
//
// #4379 wired MIDDLEWARE / AUTHENTICATION_BACKENDS / REST_FRAMEWORK
// DEFAULT_*_CLASSES. #4403 extends the same synthetic `django_settings` carrier
// to the two remaining *settings-list* shapes:
//
//   - TEMPLATES[...]['OPTIONS']['context_processors'] — dotted-path callables
//     bound app-wide (di_role=context_processor).
//   - INSTALLED_APPS — dotted-path AppConfig entries (di_role=app_config); bare
//     package labels carry no in-repo target and are skipped.
//
// The imperative `AppConfig.ready()` `post_save.connect(...)` signal axis is a
// separate (non-settings) shape tracked by its own follow-up; the `@receiver`
// decorator form is already handled in django.go (HANDLES_SIGNAL).
//
// The same real upvate_core settings.py fixture (testdata/issue4379) declares
// both TEMPLATES context_processors and an INSTALLED_APPS AppConfig
// ("core.apps.CoreConfig"); core/apps.py supplies the real class so the edge
// resolves.

// TestIssue4403_TemplatesContextProcessors asserts the four standard Django
// context_processors are wired as global USES edges (di_role=context_processor).
func TestIssue4403_TemplatesContextProcessors(t *testing.T) {
	settings := loadRepro4379(t, "settings.py", "upvate_core/settings.py")
	settingsEnts := runMerged4379(t, settings)

	want := []string{
		"django.template.context_processors.debug",
		"django.template.context_processors.request",
		"django.contrib.auth.context_processors.auth",
		"django.contrib.messages.context_processors.messages",
	}
	for _, dotted := range want {
		r, ok := hasGlobalUses(settingsEnts, dotted, "context_processor")
		if !ok {
			t.Errorf("expected global context_processor USES edge for %s", dotted)
			continue
		}
		if r.Properties["framework"] != "django" {
			t.Errorf("%s: expected framework=django, got %q", dotted, r.Properties["framework"])
		}
		if r.Properties["dotted_path"] != dotted {
			t.Errorf("%s: dotted_path mismatch %q", dotted, r.Properties["dotted_path"])
		}
	}
}

// TestIssue4403_InstalledAppsAppConfig asserts the dotted-path AppConfig entry
// is wired (di_role=app_config) and that bare package labels are NOT emitted as
// edges (no in-repo target).
func TestIssue4403_InstalledAppsAppConfig(t *testing.T) {
	settings := loadRepro4379(t, "settings.py", "upvate_core/settings.py")
	settingsEnts := runMerged4379(t, settings)

	// The dotted-path AppConfig is wired.
	if _, ok := hasGlobalUses(settingsEnts, "core.apps.CoreConfig", "app_config"); !ok {
		t.Error("expected global app_config USES edge for core.apps.CoreConfig")
	}

	// Bare package labels (no interior dot, or a stdlib/3p package with no
	// in-repo class) must NOT produce an app_config edge. "rest_framework" has
	// no dot so it is skipped entirely.
	for _, e := range settingsEnts {
		for _, r := range e.Relationships {
			if r.Properties["di_role"] == "app_config" && r.ToID == "rest_framework" {
				t.Errorf("bare package label rest_framework should not be wired, got edge %+v", r)
			}
		}
	}
}

// TestIssue4403_AppConfigResolves checks the AppConfig dotted path binds to the
// real CoreConfig class entity once core/apps.py is in the graph and the
// late-binding pass runs (same machinery #4379 uses for middleware).
func TestIssue4403_AppConfigResolves(t *testing.T) {
	settings := loadRepro4379(t, "settings.py", "upvate_core/settings.py")
	apps := loadRepro4379(t, "core/apps.py", "core/apps.py")

	var all []types.EntityRecord
	settingsEnts := runMerged4379(t, settings)
	all = append(all, settingsEnts...)
	all = append(all, runMerged4379(t, apps)...)

	if _, ok := hasGlobalUses(settingsEnts, "core.apps.CoreConfig", "app_config"); !ok {
		t.Fatal("no app_config USES edge for core.apps.CoreConfig")
	}

	// Confirm the CoreConfig class entity is present in the merged graph.
	found := false
	for _, e := range all {
		if e.Name == "CoreConfig" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a CoreConfig class entity from core/apps.py")
	}
}
