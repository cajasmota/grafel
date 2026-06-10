package substrate

import "testing"

// TestAnalyzeBranchesPHP_Outcomes exercises the PHP classifier on a controller
// action with an env-gate (env() helper), an early-return guard, a status-
// returning guard inside the try, and a try/catch that re-throws.
func TestAnalyzeBranchesPHP_Outcomes(t *testing.T) {
	src := `public function store(Request $request)
{
    if (!env('SIGNUP_ENABLED')) {
        return response()->json(['error' => 'disabled'], 503);
    }
    if ($request->input('email') === null) {
        return response()->json(['error' => 'email required'], 400);
    }
    try {
        if (User::where('email', $request->email)->exists()) {
            return response()->json(['error' => 'conflict'], 409);
        }
        $u = User::create($request->all());
        return response()->json($u, 201);
    } catch (\Exception $e) {
        Log::error($e->getMessage());
        throw new HttpException(500, 'create failed');
    }
}`
	br := analyzeBranchesPHP(src, 1)
	if len(br) != 4 {
		t.Fatalf("expected 4 branches, got %d: %+v", len(br), br)
	}

	// env-gate via env() helper
	if br[0].Kind != BranchEnvGate || br[0].EnvVar != "SIGNUP_ENABLED" {
		t.Errorf("branch0 = %+v; want env_gate SIGNUP_ENABLED", br[0])
	}
	if br[0].Outcome != OutcomeReturnValue {
		t.Errorf("branch0 outcome = %v; want return_value", br[0].Outcome)
	}
	if br[0].Returns == nil || br[0].Returns.Status != "503" {
		t.Errorf("branch0 returns = %+v; want 503", br[0].Returns)
	}

	// 400 guard — the env-gate above consumed the leading-guard slot (guardKind
	// flips firstGuardSeen for env-gates too, mirroring #4434), so this later
	// guard is mid-body `guard`.
	if br[1].Kind != BranchGuard || br[1].Outcome != OutcomeReturnValue {
		t.Errorf("branch1 = %+v; want guard/return_value", br[1])
	}
	if br[1].Returns == nil || br[1].Returns.Status != "400" {
		t.Errorf("branch1 returns = %+v; want 400", br[1].Returns)
	}

	// 409 guard inside the try → mid-body guard
	if br[2].Kind != BranchGuard || br[2].Outcome != OutcomeReturnValue {
		t.Errorf("branch2 = %+v; want guard/return_value", br[2])
	}
	if br[2].Returns == nil || br[2].Returns.Status != "409" {
		t.Errorf("branch2 returns = %+v; want 409", br[2].Returns)
	}

	// catch that re-throws → raise, status 500 from HttpException
	if br[3].Kind != BranchExcept || br[3].Outcome != OutcomeRaise {
		t.Errorf("branch3 = %+v; want except/raise", br[3])
	}
	if br[3].Returns == nil || br[3].Returns.Status != "500" {
		t.Errorf("branch3 returns = %+v; want 500", br[3].Returns)
	}
}

// TestAnalyzeBranchesPHP_Swallow confirms a log-only catch is swallow and that
// the getenv() / $_ENV / $_SERVER env-gate shapes + http_response_code /
// setStatusCode / abort status writes are all recognized.
func TestAnalyzeBranchesPHP_Swallow(t *testing.T) {
	src := `function handle($req)
{
    if (getenv('FEATURE_X') === false) {
        http_response_code(403);
        return;
    }
    if (empty($_SERVER['HTTP_AUTH'])) {
        abort(401);
    }
    if (empty($_ENV['TENANT'])) {
        return response('', 404)->setStatusCode(404);
    }
    try {
        risky();
    } catch (\Throwable $e) {
        Log::warning($e);
    }
}`
	br := analyzeBranchesPHP(src, 10)

	// getenv env-gate → 403, return_value (bare return after the status write)
	if br[0].Kind != BranchEnvGate || br[0].EnvVar != "FEATURE_X" {
		t.Errorf("branch0 = %+v; want env_gate FEATURE_X", br[0])
	}
	if br[0].Returns == nil || br[0].Returns.Status != "403" {
		t.Errorf("branch0 returns = %+v; want 403", br[0].Returns)
	}
	if br[0].Line != 12 {
		t.Errorf("branch0 line = %d; want 12 (absolute)", br[0].Line)
	}

	// $_SERVER env-gate → abort(401) raises
	if br[1].Kind != BranchEnvGate || br[1].EnvVar != "HTTP_AUTH" {
		t.Errorf("branch1 = %+v; want env_gate HTTP_AUTH", br[1])
	}
	if br[1].Returns == nil || br[1].Returns.Status != "401" {
		t.Errorf("branch1 returns = %+v; want 401", br[1].Returns)
	}

	// $_ENV env-gate → 404
	if br[2].Kind != BranchEnvGate || br[2].EnvVar != "TENANT" {
		t.Errorf("branch2 = %+v; want env_gate TENANT", br[2])
	}
	if br[2].Returns == nil || br[2].Returns.Status != "404" {
		t.Errorf("branch2 returns = %+v; want 404", br[2].Returns)
	}

	// log-only catch → swallow
	last := br[len(br)-1]
	if last.Kind != BranchExcept || last.Outcome != OutcomeSwallow {
		t.Errorf("catch branch = %+v; want except/swallow", last)
	}
}

// TestAnalyzeBranchesPHP_BraceLessGuard confirms the brace-less single-statement
// `if (...) throw ...;` form is classified, and Response::HTTP_NOT_FOUND enum
// maps to a code.
func TestAnalyzeBranchesPHP_BraceLessGuard(t *testing.T) {
	src := `public function show($id)
{
    if ($id <= 0) throw new HttpException(400, 'bad id');
    $row = Repo::find($id);
    if ($row === null) return new JsonResponse([], Response::HTTP_NOT_FOUND);
    return new JsonResponse($row);
}`
	br := analyzeBranchesPHP(src, 1)
	if len(br) != 2 {
		t.Fatalf("expected 2 branches, got %d: %+v", len(br), br)
	}
	if br[0].Outcome != OutcomeRaise || br[0].Returns == nil || br[0].Returns.Status != "400" {
		t.Errorf("branch0 = %+v; want raise/400", br[0])
	}
	if br[1].Outcome != OutcomeReturnValue || br[1].Returns == nil || br[1].Returns.Status != "404" {
		t.Errorf("branch1 = %+v; want return_value/404 (Response::HTTP_NOT_FOUND)", br[1])
	}
}

// TestAnalyzeBranchesPHP_PlainIfSkipped confirms a non-control-altering `if` is
// not surfaced (conservative — no drowning the agent in every conditional).
func TestAnalyzeBranchesPHP_PlainIfSkipped(t *testing.T) {
	src := `function f($x)
{
    if ($x > 0) {
        $y = $x + 1;
    }
    return $y;
}`
	if br := analyzeBranchesPHP(src, 1); len(br) != 0 {
		t.Fatalf("plain branching if should not be surfaced; got %+v", br)
	}
}

// TestAnalyzeBranchesPHP_Empty guards the trivial inputs.
func TestAnalyzeBranchesPHP_Empty(t *testing.T) {
	if br := analyzeBranchesPHP("", 1); br != nil {
		t.Fatalf("empty source should yield nil; got %+v", br)
	}
	if br := analyzeBranchesPHP("   \n  \n", 1); br != nil {
		t.Fatalf("blank source should yield nil; got %+v", br)
	}
}

// TestPHPBranchAnalyzerRegistered confirms the registry wiring so the MCP
// effects tool resolves the PHP analyzer for .php paths.
func TestPHPBranchAnalyzerRegistered(t *testing.T) {
	if BranchAnalyzerFor("php") == nil {
		t.Fatal("php branch analyzer not registered")
	}
	if LanguageForPath("app/Http/Controllers/UserController.php") != "php" {
		t.Fatal("LanguageForPath should map .php to php")
	}
}
