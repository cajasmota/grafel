// auth_coverage_posture_conflict_test.go — reconcile regression for the
// contradictory auth-posture dual badge (#auth-posture-conflict).
//
// grafel_auth_coverage's determineAuthCoverage must honor the engine's
// authoritative reconciled posture (auth_required=true): a route gated only by
// an INHERITED controller/global guard carries no own decorator, so the raw
// signal-1 keys are absent, yet it IS authenticated and must not be flagged
// NO-AUTH while grafel_endpoint_posture shows it authenticated.
package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestDetermineAuthCoverage_InheritedGuardPosture(t *testing.T) {
	e :=

		graph.EntityPtr(graph.Entity{}.WithProperties(map[string]string{
			"auth_required": "true",
			"auth_method":   "guard",
			"auth_guard":    "@RequirePage(PermissionPage.Checklists)",
			"verb":          "DELETE",
			"path":          "/v1/checklists/{checklistId}",
		}))
	has, ev := determineAuthCoverage(e, nil, nil, nil, false, "")
	if !has {
		t.Fatalf("inherited-guard route reported NO-AUTH (dual-badge bug); evidence=%q", ev)
	}
}

func TestDetermineAuthCoverage_ExplicitPublicUncovered(t *testing.T) {
	e :=

		graph.EntityPtr(graph.Entity{}.WithProperties(map[string]string{
			"auth_required": "false",
			"auth_method":   "config",
			"verb":          "GET",
			"path":          "/v1/checklists/{checklistId}/items",
		}))
	if has, ev := determineAuthCoverage(e, nil, nil, nil, false, ""); has {
		t.Errorf("explicit @Public() route reported covered (evidence=%q)", ev)
	}
}

func TestDetermineAuthCoverage_AuthRequiredOnlyStamp(t *testing.T) {
	// gRPC/tRPC interceptor case: auth_required=true with an auth_method but no
	// guard symbol still reconciles to authed.
	e :=

		graph.EntityPtr(graph.Entity{}.WithProperties(map[string]string{
			"auth_required": "true",
			"auth_method":   "grpc_interceptor",
		}))
	if has, _ := determineAuthCoverage(e, nil, nil, nil, false, ""); !has {
		t.Errorf("auth_required=true (interceptor) not honored as authed")
	}
}
