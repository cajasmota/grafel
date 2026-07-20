// auth_posture_conflict_test.go — reconcile regression for the contradictory
// auth-posture dual badge (#auth-posture-conflict).
//
// The dashboard security/auth-coverage panel computes its own has_auth verdict
// via hasAuthProperty. Before the fix it read only the raw per-method signal
// keys (auth_decorator/auth_middleware/auth_annotation) and so reported NO-AUTH
// for a route gated only by an INHERITED controller/global guard — even though
// the engine had already reconciled it to auth_required=true and the posture
// surface showed it authenticated. These tests feed the EXACT property shape the
// engine stamps and assert the panel agrees.
package dashboard

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestHasAuthProperty_InheritedGuardPosture(t *testing.T) {
	// The shape the engine stamps for DELETE /v1/checklists/{checklistId}: gated
	// only by the inherited controller-level @RequirePage guard — no own
	// decorator, so none of the legacy raw signal keys are present.
	e :=

		graph.EntityPtr(graph.Entity{}.WithProperties(map[string]string{
			"auth_required":   "true",
			"auth_method":     "guard",
			"auth_guard":      "@RequirePage(PermissionPage.Checklists)",
			"auth_confidence": "medium",
			"verb":            "DELETE",
			"path":            "/v1/checklists/{checklistId}",
		}))
	has, ev := hasAuthProperty(e)
	if !has {
		t.Fatalf("inherited-guard route reported NO-AUTH (the dual-badge bug); evidence=%q", ev)
	}
}

func TestHasAuthProperty_ExplicitPublicIsUncovered(t *testing.T) {
	// @Public() route: decisive public verdict. Genuinely unauthenticated by
	// design — must NOT be counted as covered (and must not error: it's
	// intentional, the posture is coherent, just public).
	e :=

		graph.EntityPtr(graph.Entity{}.WithProperties(map[string]string{
			"auth_required":   "false",
			"auth_method":     "config",
			"auth_confidence": "high",
			"verb":            "GET",
			"path":            "/v1/checklists/{checklistId}/items",
		}))
	if has, ev := hasAuthProperty(e); has {
		t.Errorf("explicit @Public() route reported covered (evidence=%q) — should be uncovered-by-design", ev)
	}
}

func TestHasAuthProperty_RawGuardSignalStillWorks(t *testing.T) {
	// Method-level @UseGuards with a guard stamp but no auth_required (older index
	// shape) still resolves authed via the raw signal-key fallback.
	e :=

		graph.EntityPtr(graph.Entity{}.WithProperties(map[string]string{
			"auth_guard": "JwtAuthGuard",
		}))
	if has, _ := hasAuthProperty(e); !has {
		t.Errorf("auth_guard raw signal no longer resolves to authed")
	}
}
