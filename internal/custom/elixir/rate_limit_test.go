package elixir

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runElixirRateLimit(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ents, err := (&rateLimitExtractor{}).Extract(context.Background(), extractor.FileInput{
		Path: path, Language: "elixir", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("rate_limit extract: %v", err)
	}
	return ents
}

func rlByName(ents []types.EntityRecord) map[string]types.EntityRecord {
	out := map[string]types.EntityRecord{}
	for _, e := range ents {
		out[e.Name] = e
	}
	return out
}

func rlKeys(m map[string]types.EntityRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- Hammer -------------------------------------------------------------------

// TestHammerStampsAction — `Hammer.check_rate("login:#{ip}", 60_000, 5)` inside
// SessionController.create stamps rate_limited + the resolved 5/60s rate +
// limit=5 + period=60 on the create action op, source=hammer, scope=route, and
// the static bucket prefix "login" as the limiter name.
func TestHammerStampsAction(t *testing.T) {
	src := `defmodule MyAppWeb.SessionController do
  use MyAppWeb, :controller

  def new(conn, _params) do
    render(conn, "new.html")
  end

  def create(conn, %{"email" => email} = params) do
    case Hammer.check_rate("login:#{conn.remote_ip}", 60_000, 5) do
      {:allow, _count} -> do_login(conn, params)
      {:deny, _limit} -> conn |> put_status(429) |> json(%{error: "rate limited"})
    end
  end
end
`
	byName := rlByName(runElixirRateLimit(t, "lib/my_app_web/controllers/session_controller.ex", src))
	e, ok := byName["action:create"]
	if !ok {
		t.Fatalf("missing rate-limited op for action:create; got %v", rlKeys(byName))
	}
	if e.Properties["rate_limited"] != "true" {
		t.Errorf("rate_limited=%q, want true", e.Properties["rate_limited"])
	}
	if e.Properties["rate_limit"] != "5/60s" {
		t.Errorf("rate_limit=%q, want 5/60s (60_000ms, limit 5)", e.Properties["rate_limit"])
	}
	if e.Properties["limit"] != "5" {
		t.Errorf("limit=%q, want 5", e.Properties["limit"])
	}
	if e.Properties["period"] != "60" {
		t.Errorf("period=%q, want 60 (60_000ms)", e.Properties["period"])
	}
	if e.Properties["rate_limit_scope"] != "route" {
		t.Errorf("rate_limit_scope=%q, want route", e.Properties["rate_limit_scope"])
	}
	if e.Properties["rate_limit_source"] != "hammer" {
		t.Errorf("rate_limit_source=%q, want hammer", e.Properties["rate_limit_source"])
	}
	if e.Properties["rate_limit_name"] != "login" {
		t.Errorf("rate_limit_name=%q, want login (static prefix of \"login:#{ip}\")", e.Properties["rate_limit_name"])
	}
	// NEGATIVE: the plain `new` action (no check_rate) must NOT be stamped.
	if _, leaked := byName["action:new"]; leaked {
		t.Errorf("rate-limit leaked onto plain action:new")
	}
}

// TestHammerCheckRateIncForm — the `check_rate_inc` variant resolves identically.
func TestHammerCheckRateIncForm(t *testing.T) {
	src := `defmodule MyAppWeb.UploadController do
  def create(conn, _params) do
    {:allow, _} = Hammer.check_rate_inc("upload:#{user_id}", 3_600_000, 1, 100)
    do_upload(conn)
  end
end
`
	byName := rlByName(runElixirRateLimit(t, "u.ex", src))
	e, ok := byName["action:create"]
	if !ok {
		t.Fatalf("missing action:create; got %v", rlKeys(byName))
	}
	// 3_600_000ms = 3600s, limit 100 → 100/3600s.
	if e.Properties["rate_limit"] != "100/3600s" {
		t.Errorf("rate_limit=%q, want 100/3600s", e.Properties["rate_limit"])
	}
	if e.Properties["rate_limit_name"] != "upload" {
		t.Errorf("rate_limit_name=%q, want upload", e.Properties["rate_limit_name"])
	}
}

// --- ExRated ------------------------------------------------------------------

// TestExRatedStampsAction — `ExRated.check_rate("api", 60_000, 100)` stamps the
// same numeric contract with source=exrated.
func TestExRatedStampsAction(t *testing.T) {
	src := `defmodule MyAppWeb.ApiController do
  def index(conn, _params) do
    case ExRated.check_rate("api", 60_000, 100) do
      {:ok, _count} -> json(conn, %{ok: true})
      {:error, _limit} -> send_resp(conn, 429, "too many")
    end
  end
end
`
	byName := rlByName(runElixirRateLimit(t, "a.ex", src))
	e, ok := byName["action:index"]
	if !ok {
		t.Fatalf("missing action:index; got %v", rlKeys(byName))
	}
	if e.Properties["rate_limit_source"] != "exrated" {
		t.Errorf("rate_limit_source=%q, want exrated", e.Properties["rate_limit_source"])
	}
	if e.Properties["rate_limit"] != "100/60s" {
		t.Errorf("rate_limit=%q, want 100/60s", e.Properties["rate_limit"])
	}
	if e.Properties["rate_limit_name"] != "api" {
		t.Errorf("rate_limit_name=%q, want api (fully-static bucket)", e.Properties["rate_limit_name"])
	}
}

// TestHammerHonestPartialScale — a config-driven scale (non-literal) stamps
// rate_limited but OMITS the numeric rate (never fabricated). The regex only
// matches integer-literal scale/limit args, so a config var fails the whole
// check_rate match and nothing is stamped — assert the honest no-stamp.
func TestHammerHonestPartialScale(t *testing.T) {
	src := `defmodule MyAppWeb.ReportController do
  def create(conn, _params) do
    {:allow, _} = Hammer.check_rate("report", @window_ms, 3)
    json(conn, %{})
  end
end
`
	ents := runElixirRateLimit(t, "r.ex", src)
	// @window_ms is non-literal → no check_rate match → no rate-limit stamp.
	// This is the honest-partial boundary: we never fabricate a window.
	if len(ents) != 0 {
		t.Errorf("config-driven scale produced %d ops, want 0 (honest no-fabrication): %v", len(ents), ents)
	}
}

// --- PlugAttack ---------------------------------------------------------------

// TestPlugAttackThrottleMarker — `throttle("login", limit: 5, period: 60_000)`
// in a `use PlugAttack` module emits a marker naming the throttle with limit=5,
// period=60, rate 5/60s, source=plug_attack, scope=request.
func TestPlugAttackThrottleMarker(t *testing.T) {
	src := `defmodule MyApp.RateLimiter do
  use PlugAttack

  rule "throttle login by ip", conn do
    throttle("login", limit: 5, period: 60_000,
      storage: {PlugAttack.Storage.Ets, MyApp.Storage})
  end
end
`
	byName := rlByName(runElixirRateLimit(t, "lib/my_app/rate_limiter.ex", src))
	e, ok := byName["plug_attack_throttle:login"]
	if !ok {
		t.Fatalf("missing PlugAttack throttle marker; got %v", rlKeys(byName))
	}
	if e.Kind != "SCOPE.Pattern" || e.Subtype != "rate_limit" {
		t.Errorf("marker kind/subtype=%s/%s, want SCOPE.Pattern/rate_limit", e.Kind, e.Subtype)
	}
	if e.Properties["rate_limit_name"] != "login" {
		t.Errorf("rate_limit_name=%q, want login", e.Properties["rate_limit_name"])
	}
	if e.Properties["limit"] != "5" {
		t.Errorf("limit=%q, want 5", e.Properties["limit"])
	}
	if e.Properties["period"] != "60" {
		t.Errorf("period=%q, want 60 (60_000ms)", e.Properties["period"])
	}
	if e.Properties["rate_limit"] != "5/60s" {
		t.Errorf("rate_limit=%q, want 5/60s", e.Properties["rate_limit"])
	}
	if e.Properties["rate_limit_source"] != "plug_attack" {
		t.Errorf("rate_limit_source=%q, want plug_attack", e.Properties["rate_limit_source"])
	}
	if e.Properties["rate_limit_scope"] != "request" {
		t.Errorf("rate_limit_scope=%q, want request (heuristic — honest-partial route binding)", e.Properties["rate_limit_scope"])
	}
}

// TestPlugAttackNegative_AllowBlock — `allow(...)` and `block(...)` are NOT
// throttles: no rate-limit entity is emitted for them.
func TestPlugAttackNegative_AllowBlock(t *testing.T) {
	src := `defmodule MyApp.RateLimiter do
  use PlugAttack

  rule "allow local", conn do
    allow(conn.remote_ip == {127, 0, 0, 1})
  end

  rule "block bad ua", conn do
    block(conn |> get_req_header("user-agent") == ["BadBot"])
  end
end
`
	ents := runElixirRateLimit(t, "rl.ex", src)
	if len(ents) != 0 {
		t.Errorf("allow/block produced %d rate-limit entities, want 0: %v", len(ents), ents)
	}
}

// TestPlugAttackNegative_NotPlugAttackModule — a stray local `throttle("x",
// limit: 1, period: 1000)` helper in a module that does NOT `use PlugAttack`
// must not be mis-attributed as a PlugAttack throttle.
func TestPlugAttackNegative_NotPlugAttackModule(t *testing.T) {
	src := `defmodule MyApp.Audio do
  def fade(conn) do
    throttle("volume", limit: 1, period: 1000)
  end
end
`
	ents := runElixirRateLimit(t, "audio.ex", src)
	if len(ents) != 0 {
		t.Errorf("throttle() outside a use PlugAttack module produced %d entities, want 0: %v", len(ents), ents)
	}
}

// TestRateLimitNegative_PlainAction — a controller action with no limiter call
// yields no rate-limit ops (does not stamp ordinary actions).
func TestRateLimitNegative_PlainAction(t *testing.T) {
	src := `defmodule MyAppWeb.PageController do
  def index(conn, _params) do
    render(conn, "index.html")
  end
end
`
	ents := runElixirRateLimit(t, "p.ex", src)
	if len(ents) != 0 {
		t.Errorf("plain controller produced %d rate-limit ops, want 0: %v", len(ents), ents)
	}
}

// TestRateLimitNegative_ModuleLevelCheckRate — a `check_rate` call NOT inside any
// `def` (a module-level limiter helper) is honestly skipped: there is no action
// to attribute the throttle to and a route must not be invented.
func TestRateLimitNegative_ModuleLevelCheckRate(t *testing.T) {
	src := `defmodule MyApp.Limiter do
  @result Hammer.check_rate("startup", 1000, 1)
end
`
	ents := runElixirRateLimit(t, "lim.ex", src)
	if len(ents) != 0 {
		t.Errorf("module-level check_rate produced %d ops, want 0: %v", len(ents), ents)
	}
}

// TestRateLimitNegative_NoIdiom — a file with neither check_rate nor PlugAttack
// is a fast no-op.
func TestRateLimitNegative_NoIdiom(t *testing.T) {
	src := `defmodule MyApp.Plain do
  def hello, do: :world
end
`
	ents := runElixirRateLimit(t, "plain.ex", src)
	if len(ents) != 0 {
		t.Errorf("non-rate-limit file produced %d entities, want 0", len(ents))
	}
}
