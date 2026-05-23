// regression_1964_1966_1990_test.go — Wave 6 regression bundle.
//
// Each `TestRegression_*` test pins the post-merge contract Wave 6 evidence
// surfaced was still violated by earlier "fix" PRs:
//
//   #1964 — every Python entity (Operation, Class, Schema/field, Constraint,
//           Pattern, Config, Module) emits a non-zero end_line. W6R1 + W6R2
//           saw end_line=0 on production Operations even though #1987 added
//           a regression test for the buildFunction path; the leak came from
//           supplemental passes whose entities never went through buildFunction.
//   #1966 — every Python entity emits Language="python". W6R1 saw
//           language: "" on Operations. The constraint pass was using
//           file.Language (unset on some callers); the finalize sweep
//           catches every other future leak.
//   #1990 — admin.site.register(...) walks EVERY call in the file, not just
//           the first. W6R4 saw 1 edge for 8 register calls.
//
// Fixture-name convention (memory feedback_archigraph_competitor_name_scrub):
// `client_fixture_a`, never a real client.
package python

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractPy12 runs the Python extractor on the given source + path. Helper
// kept local so tests in this file are self-contained.
func extractPy12(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	out, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	return out
}

// TestRegression_1964_AllEntities_NonZero_EndLine_Language — exhaustive sweep
// across every entity kind the Python extractor can emit. The finalize pass
// in Extract() must guarantee Language="python" + StartLine>0 + EndLine>0
// for every emitted EntityRecord, no matter which sub-pass produced it.
func TestRegression_1964_AllEntities_NonZero_EndLine_Language(t *testing.T) {
	src := `# client_fixture_a — exercise every Python extractor pass.
import logging
from rest_framework import viewsets
from rest_framework.decorators import action

# Module-level constant (config-module heuristic, supplemental config pass).
SETTING_A = "x"
SETTING_B = 42

class FixtureModel:
    """Django-like Model with Meta + class fields + constraint."""

    name = "fixture_a"

    class Meta:
        db_table = "fixture_a"
        constraints = [
            UniqueConstraint(fields=["name"], name="fixture_a_name_unique"),
        ]

class FixtureViewSet(viewsets.ModelViewSet):
    serializer_class = None
    queryset = None

    @action(detail=True, methods=["post"])
    def assign_contacts(self, request, pk=None):
        log = logging.getLogger(__name__)
        try:
            log.info("starting assignment for pk=%s", pk)
            if request.data is None:
                return {"status": "missing"}
            result = {"ok": True}
        except Exception as exc:
            return {"error": str(exc)}
        return result


def module_level_helper(a, b):
    return a + b
`

	out := extractPy12(t, src, "client_fixture_a/views.py")

	if len(out) == 0 {
		t.Fatal("no entities emitted")
	}

	// Track which kinds we covered so the test fails loudly if the fixture
	// stops exercising a pass (would be a silent regression).
	seenKinds := map[string]bool{}

	for i, ent := range out {
		// #1966 — every entity must declare language=python.
		if ent.Language != "python" {
			t.Errorf("entity[%d] %s/%s name=%q: language=%q want %q",
				i, ent.Kind, ent.Subtype, ent.Name, ent.Language, "python")
		}
		// #1964 — every entity must have non-zero start_line + end_line.
		if ent.StartLine <= 0 {
			t.Errorf("entity[%d] %s/%s name=%q: start_line=%d (want > 0)",
				i, ent.Kind, ent.Subtype, ent.Name, ent.StartLine)
		}
		if ent.EndLine <= 0 {
			t.Errorf("entity[%d] %s/%s name=%q: end_line=%d (want > 0)",
				i, ent.Kind, ent.Subtype, ent.Name, ent.EndLine)
		}
		if ent.EndLine < ent.StartLine {
			t.Errorf("entity[%d] %s/%s name=%q: end_line(%d) < start_line(%d)",
				i, ent.Kind, ent.Subtype, ent.Name, ent.EndLine, ent.StartLine)
		}
		seenKinds[ent.Kind+"/"+ent.Subtype] = true
	}

	// Fixture coverage assertions — confirm the test actually exercises
	// the entity kinds Wave 6 flagged. Add a new kind to the fixture if
	// these ever fail.
	wantKinds := []string{
		"SCOPE.Operation/method",
		"SCOPE.Operation/function",
		"SCOPE.Component/class",
		"SCOPE.Component/file",
	}
	for _, k := range wantKinds {
		if !seenKinds[k] {
			t.Errorf("fixture did not exercise kind %q (seen: %v)", k, seenKinds)
		}
	}
}

// TestRegression_1964_DecoratedFunction_EndLine — Wave 4 saw end_line=0 on
// @receiver(post_save) signal handlers (decorated functions). buildFunction
// uses the inner function_definition's EndPoint which is correct, but pins
// the contract so future grammar refactors can't regress.
func TestRegression_1964_DecoratedFunction_EndLine(t *testing.T) {
	src := `from django.db.models.signals import post_save
from django.dispatch import receiver

@receiver(post_save, sender="FixtureModel")
def model_post_save(sender, instance, created, **kwargs):
    if created:
        instance.tag = "new"
    return None
`
	out := extractPy12(t, src, "client_fixture_a/signals.py")

	var handler *types.EntityRecord
	for i := range out {
		if out[i].Name == "model_post_save" {
			handler = &out[i]
			break
		}
	}
	if handler == nil {
		t.Fatalf("model_post_save not emitted; got %d entities", len(out))
	}
	if handler.EndLine <= handler.StartLine {
		t.Errorf("decorated signal handler: end_line(%d) must exceed start_line(%d)",
			handler.EndLine, handler.StartLine)
	}
	if handler.Language != "python" {
		t.Errorf("decorated signal handler: language=%q want python", handler.Language)
	}
}

// TestRegression_1964_ConfigModule_RealEndLine — W3R3 saw end_line=0 on the
// Config kind (settings.py), but the bug was actually EndLine=1 hardcoded —
// the bundle source_window then clipped to "imports only". The fix uses
// the real file end so the whole settings body is excerptable.
func TestRegression_1964_ConfigModule_RealEndLine(t *testing.T) {
	// settings.py file with mostly module-level assignments — exercises the
	// config_module heuristic (assignCount/totalCount ratio ≥ threshold).
	src := `# client_fixture_a/settings.py
DEBUG = True
SECRET_KEY = "x"
ALLOWED_HOSTS = ["*"]
DATABASES = {
    "default": {
        "ENGINE": "django.db.backends.sqlite3",
        "NAME": "db.sqlite3",
    }
}
INSTALLED_APPS = []
MIDDLEWARE = []
ROOT_URLCONF = "fixture_a.urls"
WSGI_APPLICATION = "fixture_a.wsgi.application"
LANGUAGE_CODE = "en-us"
TIME_ZONE = "UTC"
USE_I18N = True
USE_TZ = True
STATIC_URL = "static/"
`
	out := extractPy12(t, src, "client_fixture_a/settings.py")

	var cfg *types.EntityRecord
	for i := range out {
		if out[i].Kind == string(types.EntityKindConfig) && out[i].Subtype == "config_module" {
			cfg = &out[i]
			break
		}
	}
	if cfg == nil {
		t.Fatalf("config_module entity not emitted; got %d entities", len(out))
	}
	// settings fixture is >10 lines — end_line must reflect the real file
	// extent, not the historical EndLine=1 hardcode.
	if cfg.EndLine <= 1 {
		t.Errorf("config_module: end_line=%d (want > 1; was hardcoded to 1 pre-#1964)", cfg.EndLine)
	}
}

// TestRegression_1966_AllPasses_LanguagePython — sweep test ensuring every
// supplemental pass (config_module, package_module, error_pattern, imports,
// constraint) stamps Language="python". TestRegression_1964 already checks
// this but for kinds NOT in the previous fixture (error_handling try/catch,
// constraint, import module entities) we exercise them here explicitly.
func TestRegression_1966_AllPasses_LanguagePython(t *testing.T) {
	src := `import logging
from django.db import models


class FixtureModel(models.Model):
    name = models.CharField(max_length=128)

    class Meta:
        constraints = [
            models.UniqueConstraint(fields=["name"], name="fixture_a_name_unique"),
        ]


def handler():
    try:
        logging.info("ok")
    except Exception:
        logging.error("failed")
`
	out := extractPy12(t, src, "client_fixture_a/models.py")

	sawConstraint := false
	sawPattern := false
	for _, ent := range out {
		if ent.Language != "python" {
			t.Errorf("entity %s/%s name=%q has language=%q (want python)",
				ent.Kind, ent.Subtype, ent.Name, ent.Language)
		}
		if ent.Kind == "SCOPE.Constraint" {
			sawConstraint = true
		}
		if ent.Kind == "SCOPE.Pattern" {
			sawPattern = true
		}
	}
	if !sawConstraint {
		t.Errorf("fixture did not exercise SCOPE.Constraint (Meta.constraints pass)")
	}
	if !sawPattern {
		t.Errorf("fixture did not exercise SCOPE.Pattern (error-handling pass)")
	}
}

// TestRegression_1990_AllAdminSiteRegisterCallsWalked — W6R4 evidence: 8
// admin.site.register() calls in core/admin.py emitted only 1 REFERENCES
// edge. The extractor must walk EVERY register call, not stop after the
// first. This fixture is the upvate-shaped one (imports + 8 register calls,
// no local ModelAdmin classes — they're imported from elsewhere).
func TestRegression_1990_AllAdminSiteRegisterCallsWalked(t *testing.T) {
	src := `from django.contrib import admin
from .models import (
    AlphaModel,
    BravoModel,
    CharlieModel,
    DeltaModel,
    EchoModel,
    FoxtrotModel,
    GolfModel,
    HotelModel,
)
from .admin_classes import (
    AlphaAdmin,
    BravoAdmin,
    CharlieAdmin,
    DeltaAdmin,
    EchoAdmin,
    FoxtrotAdmin,
    GolfAdmin,
    HotelAdmin,
)

admin.site.register(AlphaModel, AlphaAdmin)
admin.site.register(BravoModel, BravoAdmin)
admin.site.register(CharlieModel, CharlieAdmin)
admin.site.register(DeltaModel, DeltaAdmin)
admin.site.register(EchoModel, EchoAdmin)
admin.site.register(FoxtrotModel, FoxtrotAdmin)
admin.site.register(GolfModel, GolfAdmin)
admin.site.register(HotelModel, HotelAdmin)
`
	out := extractPy12(t, src, "client_fixture_a/admin.py")

	// Collect every admin_register REFERENCES edge from the file entity
	// (the admin module — entities[0]).
	var refs []types.RelationshipRecord
	for _, ent := range out {
		if ent.SourceFile != "client_fixture_a/admin.py" {
			continue
		}
		for _, r := range ent.Relationships {
			if r.Kind != string(types.RelationshipKindReferences) {
				continue
			}
			if r.Properties == nil || r.Properties["pattern_type"] != "admin_register" {
				continue
			}
			refs = append(refs, r)
		}
	}

	// 8 models + 8 admin classes = 16 REFERENCES edges expected.
	wantCount := 16
	if len(refs) != wantCount {
		var got []string
		for _, r := range refs {
			got = append(got, r.ToID)
		}
		t.Fatalf("expected %d admin_register REFERENCES edges (8 models + 8 admins), got %d:\n  %s",
			wantCount, len(refs), strings.Join(got, "\n  "))
	}

	// Every registered name must appear in at least one edge's ToID.
	wantNames := []string{
		"AlphaModel", "BravoModel", "CharlieModel", "DeltaModel",
		"EchoModel", "FoxtrotModel", "GolfModel", "HotelModel",
		"AlphaAdmin", "BravoAdmin", "CharlieAdmin", "DeltaAdmin",
		"EchoAdmin", "FoxtrotAdmin", "GolfAdmin", "HotelAdmin",
	}
	for _, name := range wantNames {
		found := false
		for _, r := range refs {
			if strings.Contains(r.ToID, ":"+name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing REFERENCES edge for %q (would indicate single-shot/loop-termination regression)", name)
		}
	}
}

// TestRegression_1990_MixedBareAndTwoArgRegisterCalls — guard against a
// future refactor that handles the bare 1-arg form differently from the
// 2-arg form and silently drops calls in the middle of the loop.
func TestRegression_1990_MixedBareAndTwoArgRegisterCalls(t *testing.T) {
	src := `from django.contrib import admin
from .models import A, B, C, D, E

admin.site.register(A)
admin.site.register(B, BAdmin)
admin.site.register(C)
admin.site.register(D, DAdmin)
admin.site.register(E)
`
	out := extractPy12(t, src, "client_fixture_a/admin.py")

	var refs []types.RelationshipRecord
	for _, ent := range out {
		for _, r := range ent.Relationships {
			if r.Kind == string(types.RelationshipKindReferences) &&
				r.Properties != nil &&
				r.Properties["pattern_type"] == "admin_register" {
				refs = append(refs, r)
			}
		}
	}

	// Expected edges: A, B, BAdmin, C, D, DAdmin, E = 7 unique targets.
	wantTargets := []string{"A", "B", "BAdmin", "C", "D", "DAdmin", "E"}
	for _, name := range wantTargets {
		found := false
		for _, r := range refs {
			if strings.Contains(r.ToID, ":"+name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing REFERENCES edge for %q in mixed bare/2-arg fixture", name)
		}
	}
	if len(refs) != len(wantTargets) {
		t.Errorf("expected %d edges across mixed bare+2-arg calls, got %d", len(wantTargets), len(refs))
	}
}
