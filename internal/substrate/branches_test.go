package substrate

import "testing"

// TestAnalyzeBranchesPython_Outcomes exercises the classifier directly on
// representative shapes: an except-swallow, an except-raise, an env-gate, an
// early-return guard, and a redirect. Complements the in-pipeline MCP test
// (internal/mcp/effects_branches_4423_test.go) which runs the REAL handler on
// byte-copies of upvate oracle functions.
func TestAnalyzeBranchesPython_Outcomes(t *testing.T) {
	src := `def handler(self, request):
    if not settings.FEATURE_X:
        return Response({"error": "disabled"}, status=status.HTTP_403_FORBIDDEN)
    try:
        if user is None:
            raise ValueError("no user")
        return Response({"ok": True}, status=status.HTTP_200_OK)
    except requests.HTTPError:
        logger.warning("upstream failed")
    except Exception:
        return redirect("/fallback")
`
	br := analyzeBranchesPython(src, 10)
	// env-gate, if-user-none (raise), except requests.HTTPError (swallow),
	// except Exception (redirect). The bare `return Response(...200)` is NOT in
	// a conditional, so it is not a branch.
	if len(br) != 4 {
		t.Fatalf("expected 4 branches, got %d: %+v", len(br), br)
	}

	// env-gate
	if br[0].Kind != BranchEnvGate || br[0].EnvVar != "FEATURE_X" {
		t.Errorf("branch0 = %+v; want env_gate FEATURE_X", br[0])
	}
	if br[0].Outcome != OutcomeReturnValue {
		t.Errorf("branch0 outcome = %v; want return_value", br[0].Outcome)
	}
	if br[0].Returns == nil || br[0].Returns.Status != "403" {
		t.Errorf("branch0 returns = %+v; want status 403", br[0].Returns)
	}

	// guard that raises. It is `guard` (not early_return) because the env-gate
	// above already consumed the leading-guard slot — env-gates and the first
	// non-env guard are distinguished, later guards are mid-body `guard`.
	if br[1].Kind != BranchGuard || br[1].Outcome != OutcomeRaise {
		t.Errorf("branch1 = %+v; want guard/raise", br[1])
	}

	// fall-through 200 return guard? No — `return Response(...200)` is not
	// inside an if, so it is NOT a branch. The next branch is the except-swallow.
	if br[2].Kind != BranchExcept || br[2].Outcome != OutcomeSwallow {
		t.Errorf("branch2 = %+v; want except/swallow", br[2])
	}
	if br[2].Condition != "except requests.HTTPError" {
		t.Errorf("branch2 condition = %q", br[2].Condition)
	}

	// except that redirects
	if br[3].Kind != BranchExcept || br[3].Outcome != OutcomeRedirect {
		t.Errorf("branch3 = %+v; want except/redirect", br[3])
	}

	// Line numbers are absolute (startLine=10 → first guard at line 11).
	if br[0].Line != 11 {
		t.Errorf("branch0 line = %d; want 11", br[0].Line)
	}
}

// TestAnalyzeBranchesPython_NoBranches confirms a straight-line function
// yields no branches (the facet must not invent control flow).
func TestAnalyzeBranchesPython_NoBranches(t *testing.T) {
	src := `def add(a, b):
    total = a + b
    return total
`
	if br := analyzeBranchesPython(src, 1); len(br) != 0 {
		t.Fatalf("straight-line function should have 0 branches, got %+v", br)
	}
}

// TestBranchAnalyzerRegistry confirms python is registered and unknown langs
// return nil (honest-partial at the MCP layer).
func TestBranchAnalyzerRegistry(t *testing.T) {
	if BranchAnalyzerFor("python") == nil {
		t.Fatal("python branch analyzer not registered")
	}
	if BranchAnalyzerFor("cobol") != nil {
		t.Fatal("unexpected analyzer for cobol")
	}
}
