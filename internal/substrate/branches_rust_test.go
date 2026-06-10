package substrate

import "testing"

// TestAnalyzeBranchesRust_Outcomes exercises the Rust classifier on an axum-ish
// handler with an env-gate (std::env::var), an early-return `if` guard returning
// a StatusCode, a `?` propagation, and a `match` Err arm that returns Err.
func TestAnalyzeBranchesRust_Outcomes(t *testing.T) {
	src := `async fn create_user(State(db): State<Db>, Json(payload): Json<NewUser>) -> Result<Json<User>, StatusCode> {
    if std::env::var("SIGNUP_ENABLED").is_err() {
        return Err(StatusCode::SERVICE_UNAVAILABLE);
    }
    if payload.email.is_empty() {
        return Err(StatusCode::BAD_REQUEST);
    }
    let existing = db.find_by_email(&payload.email).await?;
    match db.insert(payload).await {
        Ok(u) => Ok(Json(u)),
        Err(e) => {
            tracing::error!("insert failed: {e}");
            return Err(StatusCode::INTERNAL_SERVER_ERROR);
        }
    }
}`
	br := analyzeBranchesRust(src, 1)
	if len(br) != 4 {
		t.Fatalf("expected 4 branches, got %d: %+v", len(br), br)
	}

	// env-gate: std::env::var("SIGNUP_ENABLED") → 503
	if br[0].Kind != BranchEnvGate || br[0].EnvVar != "SIGNUP_ENABLED" {
		t.Errorf("branch0 = %+v; want env_gate SIGNUP_ENABLED", br[0])
	}
	if br[0].Outcome != OutcomeRaise {
		t.Errorf("branch0 outcome = %v; want raise (return Err)", br[0].Outcome)
	}
	if br[0].Returns == nil || br[0].Returns.Status != "503" {
		t.Errorf("branch0 returns = %+v; want 503 (SERVICE_UNAVAILABLE)", br[0].Returns)
	}

	// guard returning Err(BAD_REQUEST) → raise, 400 (env-gate took leading slot)
	if br[1].Kind != BranchGuard || br[1].Outcome != OutcomeRaise {
		t.Errorf("branch1 = %+v; want guard/raise", br[1])
	}
	if br[1].Returns == nil || br[1].Returns.Status != "400" {
		t.Errorf("branch1 returns = %+v; want 400 (BAD_REQUEST)", br[1].Returns)
	}

	// `?` propagation → raise
	q := br[2]
	if q.Outcome != OutcomeRaise {
		t.Errorf("branch2 (? op) outcome = %v; want raise; %+v", q.Outcome, q)
	}

	// match Err arm returning Err(500) → except/raise, 500
	if br[3].Kind != BranchExcept || br[3].Outcome != OutcomeRaise {
		t.Errorf("branch3 = %+v; want except/raise", br[3])
	}
	if br[3].Returns == nil || br[3].Returns.Status != "500" {
		t.Errorf("branch3 returns = %+v; want 500 (INTERNAL_SERVER_ERROR)", br[3].Returns)
	}
}

// TestAnalyzeBranchesRust_Panic confirms panic! and .unwrap() are raise.
func TestAnalyzeBranchesRust_Panic(t *testing.T) {
	src := `fn f(cfg: Option<Config>) -> u32 {
    if cfg.is_none() {
        panic!("config required");
    }
    42
}`
	br := analyzeBranchesRust(src, 1)
	if len(br) != 1 || br[0].Outcome != OutcomeRaise {
		t.Fatalf("expected one raise (panic) branch, got %+v", br)
	}
}

// TestAnalyzeBranchesRust_IfLetSwallow confirms a `match` Err arm that recovers
// with a fallback value (no return/raise/?) is a swallow.
func TestAnalyzeBranchesRust_MatchSwallow(t *testing.T) {
	src := `fn load(path: &str) -> Config {
    match read(path) {
        Ok(c) => c,
        Err(_) => {
            Config::default()
        }
    }
}`
	br := analyzeBranchesRust(src, 1)
	if len(br) != 1 || br[0].Kind != BranchExcept || br[0].Outcome != OutcomeSwallow {
		t.Fatalf("expected one swallow except (recover-and-continue), got %+v", br)
	}
}

// TestAnalyzeBranchesRust_IfLetErr confirms an `if let Err(e) = ..` guard that
// returns is surfaced with its condition.
func TestAnalyzeBranchesRust_IfLetErr(t *testing.T) {
	src := `fn run() -> Result<(), Error> {
    if let Err(e) = do_thing() {
        return Err(e);
    }
    Ok(())
}`
	br := analyzeBranchesRust(src, 1)
	if len(br) != 1 {
		t.Fatalf("expected 1 branch, got %d: %+v", len(br), br)
	}
	if br[0].Outcome != OutcomeRaise {
		t.Errorf("if-let-Err outcome = %v; want raise", br[0].Outcome)
	}
	if br[0].Condition != "if let Err(e) = do_thing()" {
		t.Errorf("condition = %q; want `if let Err(e) = do_thing()`", br[0].Condition)
	}
}

// TestAnalyzeBranchesRust_StatusBuilder confirms `.status(NNN)` extraction.
func TestAnalyzeBranchesRust_StatusBuilder(t *testing.T) {
	src := `fn h(ok: bool) -> Response {
    if !ok {
        return Response::builder().status(403).body(()).unwrap();
    }
    Response::new(())
}`
	br := analyzeBranchesRust(src, 1)
	if len(br) != 1 {
		t.Fatalf("expected 1 branch, got %d: %+v", len(br), br)
	}
	if br[0].Returns == nil || br[0].Returns.Status != "403" {
		t.Errorf("returns = %+v; want 403", br[0].Returns)
	}
}

// TestBranchAnalyzerRegistry_Rust confirms rust is registered.
func TestBranchAnalyzerRegistry_Rust(t *testing.T) {
	if BranchAnalyzerFor("rust") == nil {
		t.Fatal("rust branch analyzer not registered")
	}
}
