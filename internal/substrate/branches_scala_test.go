package substrate

import "testing"

// TestAnalyzeBranchesScala_Outcomes exercises the Scala classifier on a service
// method with an env-gate (sys.env.get), an early-return guard returning a
// BadRequest status, an Either Left error branch, and a try/catch that re-throws.
func TestAnalyzeBranchesScala_Outcomes(t *testing.T) {
	src := `def createUser(req: Request): Either[ApiError, User] = {
    if (sys.env.get("SIGNUP_ENABLED").isEmpty) {
      return Left(ServiceUnavailable("signup disabled"))
    }
    if (req.email == null) {
      return Left(BadRequest("email required"))
    }
    try {
      val existing = repo.findByEmail(req.email)
      if (existing.isDefined) {
        return Left(Conflict("email in use"))
      }
      Right(repo.create(req))
    } catch {
      case e: Exception =>
        logger.error("createUser failed", e)
        throw new ServiceException("create failed", 500)
    }
}`
	br := analyzeBranchesScala(src, 1)
	if len(br) != 4 {
		t.Fatalf("expected 4 branches, got %d: %+v", len(br), br)
	}

	// env-gate: sys.env.get("SIGNUP_ENABLED") → ServiceUnavailable (503)
	if br[0].Kind != BranchEnvGate || br[0].EnvVar != "SIGNUP_ENABLED" {
		t.Errorf("branch0 = %+v; want env_gate SIGNUP_ENABLED", br[0])
	}
	if br[0].Outcome != OutcomeReturnValue {
		t.Errorf("branch0 outcome = %v; want return_value", br[0].Outcome)
	}
	if br[0].Returns == nil || br[0].Returns.Status != "503" {
		t.Errorf("branch0 returns = %+v; want 503 (ServiceUnavailable)", br[0].Returns)
	}

	// 400 guard (env-gate consumed the leading slot → guard)
	if br[1].Kind != BranchGuard || br[1].Outcome != OutcomeReturnValue {
		t.Errorf("branch1 = %+v; want guard/return_value", br[1])
	}
	if br[1].Returns == nil || br[1].Returns.Status != "400" {
		t.Errorf("branch1 returns = %+v; want 400 (BadRequest)", br[1].Returns)
	}

	// 409 Conflict guard inside the try
	if br[2].Returns == nil || br[2].Returns.Status != "409" {
		t.Errorf("branch2 returns = %+v; want 409 (Conflict)", br[2].Returns)
	}

	// catch that re-throws → raise
	if br[3].Kind != BranchExcept || br[3].Outcome != OutcomeRaise {
		t.Errorf("branch3 = %+v; want except/raise", br[3])
	}
}

// TestAnalyzeBranchesScala_Swallow confirms a log-only catch is swallow.
func TestAnalyzeBranchesScala_Swallow(t *testing.T) {
	src := `def f(): Unit = {
    try {
      doThing()
    } catch {
      case err: Throwable =>
        logger.warn("ignored", err)
    }
}`
	br := analyzeBranchesScala(src, 1)
	if len(br) != 1 || br[0].Kind != BranchExcept || br[0].Outcome != OutcomeSwallow {
		t.Fatalf("expected one swallow except, got %+v", br)
	}
}

// TestAnalyzeBranchesScala_TryFailure confirms a catch yielding Failure(new
// Exception) is a raise (the Try analogue of a re-throw).
func TestAnalyzeBranchesScala_TryFailure(t *testing.T) {
	src := `def f(): Try[Int] = {
    try {
      Success(risky())
    } catch {
      case e: IOException =>
        Failure(new RuntimeException(e))
    }
}`
	br := analyzeBranchesScala(src, 1)
	if len(br) != 1 || br[0].Kind != BranchExcept || br[0].Outcome != OutcomeRaise {
		t.Fatalf("expected one raise except (Failure(new ...)), got %+v", br)
	}
}

// TestAnalyzeBranchesScala_WithStatus confirms a guard writing .withStatus(NNN)
// surfaces the numeric status.
func TestAnalyzeBranchesScala_WithStatus(t *testing.T) {
	src := `def handle(req: Request): Response = {
    if (!authorized(req)) {
      return Response().withStatus(403)
    }
    Response().withStatus(200)
}`
	br := analyzeBranchesScala(src, 1)
	if len(br) != 1 {
		t.Fatalf("expected 1 branch, got %d: %+v", len(br), br)
	}
	if br[0].Outcome != OutcomeReturnValue || br[0].Returns == nil || br[0].Returns.Status != "403" {
		t.Errorf("branch0 = %+v; want return_value/403", br[0])
	}
}

// TestBranchAnalyzerRegistry_Scala confirms scala is registered.
func TestBranchAnalyzerRegistry_Scala(t *testing.T) {
	if BranchAnalyzerFor("scala") == nil {
		t.Errorf("scala branch analyzer not registered")
	}
}
