package resolve

import "regexp"

// elixirDynamicPatterns are per-language patterns for Elixir.
// Registered via init() into dynamicPatternsByLang.
//
// Elixir dynamic-pattern catalog (issue #44, slice 9).
//
// The Elixir extractor (internal/extractors/elixir/elixir.go) emits CALLS
// edges whose ToID is the bare trailing method identifier extracted from
// dotted call expressions — e.g. `Repo.all(User)` → ToID="all",
// `Phoenix.Controller.render(conn, ...)` → ToID="render". When the
// receiver is an external framework module (Ecto.Repo, Phoenix.Controller,
// Ecto.Changeset, GenServer behaviour, Logger) no in-tree entity exists
// for the callee, so the resolver cannot bind it. These stubs are
// statically unresolvable because:
//  1. The Ecto/Phoenix/OTP modules are imported from the Hex ecosystem
//     — they are never indexed as in-tree entities.
//  2. The extractor emits only the leaf name; without the receiver
//     module the resolver cannot distinguish `Repo.all` from a
//     user-defined function also named `all` on a domain module.
//
// The safer-bias rule (#94) is preserved by the per-language gate
// (lang=="elixir"): names like `all`, `get`, `insert`, `render`,
// `cast`, `change` are common in other ecosystems and must NOT fire
// for Go, Python, Ruby, TypeScript, etc.
//
// Four categories drive the bulk of unresolved Elixir edges:
//  1. Ecto.Repo query + mutation methods (`all`, `get`, `get!`, etc.)
//  2. Phoenix.Conn pipeline helpers (`render`, `json`, `send_resp`, etc.)
//  3. GenServer / OTP behaviour callbacks (`handle_call`, `handle_cast`, etc.)
//  4. Ecto.Changeset builder methods (`cast`, `validate_required`, etc.)
var elixirDynamicPatterns = []*regexp.Regexp{
	// ── Ecto.Repo query methods ──────────────────────────────────────
	// `Repo.all/1`, `Repo.get/2`, `Repo.get!/2`, `Repo.get_by/2`,
	// `Repo.get_by!/2`, `Repo.one/1`, `Repo.one!/1`, `Repo.aggregate/3`
	// all arrive as bare leaf names because the extractor strips the
	// receiver alias. No static resolver can bind them without full
	// module-alias tracking.
	regexp.MustCompile(`^all$`),
	regexp.MustCompile(`^one$`),
	regexp.MustCompile(`^get$`),
	regexp.MustCompile(`^get!$`),
	regexp.MustCompile(`^get_by$`),
	regexp.MustCompile(`^get_by!$`),
	regexp.MustCompile(`^aggregate$`),
	regexp.MustCompile(`^preload$`),
	regexp.MustCompile(`^reload$`),
	regexp.MustCompile(`^transaction$`),
	// Ecto.Repo mutation methods.
	regexp.MustCompile(`^insert$`),
	regexp.MustCompile(`^insert!$`),
	regexp.MustCompile(`^insert_or_update$`),
	regexp.MustCompile(`^insert_or_update!$`),
	regexp.MustCompile(`^update$`),
	regexp.MustCompile(`^update!$`),
	regexp.MustCompile(`^delete$`),
	regexp.MustCompile(`^delete!$`),
	regexp.MustCompile(`^insert_all$`),
	regexp.MustCompile(`^update_all$`),
	regexp.MustCompile(`^delete_all$`),

	// ── Phoenix.Conn pipeline helpers ───────────────────────────────
	// Phoenix controller actions call bare helpers (render/json/
	// send_resp) that are injected by `use Phoenix.Controller` via
	// macro expansion — no static definition exists in the indexed tree.
	regexp.MustCompile(`^render$`),
	regexp.MustCompile(`^json$`),
	regexp.MustCompile(`^text$`),
	regexp.MustCompile(`^html$`),
	regexp.MustCompile(`^send_resp$`),
	regexp.MustCompile(`^send_file$`),
	regexp.MustCompile(`^send_download$`),
	regexp.MustCompile(`^put_flash$`),
	regexp.MustCompile(`^clear_flash$`),
	regexp.MustCompile(`^redirect$`),
	regexp.MustCompile(`^halt$`),
	regexp.MustCompile(`^assign$`),
	regexp.MustCompile(`^fetch_session$`),
	regexp.MustCompile(`^put_session$`),
	regexp.MustCompile(`^get_session$`),
	regexp.MustCompile(`^delete_session$`),
	regexp.MustCompile(`^configure_session$`),
	regexp.MustCompile(`^fetch_flash$`),
	regexp.MustCompile(`^put_resp_content_type$`),
	regexp.MustCompile(`^put_resp_header$`),
	regexp.MustCompile(`^put_req_header$`),
	regexp.MustCompile(`^delete_resp_header$`),
	regexp.MustCompile(`^resp$`),

	// ── GenServer / OTP behaviour callbacks ─────────────────────────
	// OTP behaviour callbacks are invoked by the runtime, not by static
	// call sites the resolver can see. The extractor emits them as
	// ordinary CALLS stubs from the `init/start_link` entry point.
	// snake_case + `handle_` prefix pattern is Elixir/Erlang-specific.
	regexp.MustCompile(`^init$`),
	regexp.MustCompile(`^handle_call$`),
	regexp.MustCompile(`^handle_cast$`),
	regexp.MustCompile(`^handle_info$`),
	regexp.MustCompile(`^handle_continue$`),
	regexp.MustCompile(`^terminate$`),
	regexp.MustCompile(`^code_change$`),
	// Agent / Task OTP helpers.
	regexp.MustCompile(`^start_link$`),
	regexp.MustCompile(`^start$`),
	regexp.MustCompile(`^stop$`),
	regexp.MustCompile(`^child_spec$`),
	// GenServer reply helpers (Process, GenServer module-level calls).
	regexp.MustCompile(`^reply$`),
	regexp.MustCompile(`^noreply$`),

	// ── Ecto.Changeset builder methods ──────────────────────────────
	// Changeset pipelines (|> cast() |> validate_required() ...) arrive
	// as bare leaf names. The `Ecto.Changeset` module is an external
	// Hex dependency never indexed in-tree.
	regexp.MustCompile(`^cast$`),
	regexp.MustCompile(`^cast_assoc$`),
	regexp.MustCompile(`^cast_embed$`),
	regexp.MustCompile(`^validate_required$`),
	regexp.MustCompile(`^validate_length$`),
	regexp.MustCompile(`^validate_format$`),
	regexp.MustCompile(`^validate_inclusion$`),
	regexp.MustCompile(`^validate_exclusion$`),
	regexp.MustCompile(`^validate_number$`),
	regexp.MustCompile(`^validate_acceptance$`),
	regexp.MustCompile(`^validate_confirmation$`),
	regexp.MustCompile(`^validate_change$`),
	regexp.MustCompile(`^validate_unique$`),
	regexp.MustCompile(`^put_assoc$`),
	regexp.MustCompile(`^put_embed$`),
	regexp.MustCompile(`^put_change$`),
	regexp.MustCompile(`^force_change$`),
	regexp.MustCompile(`^change$`),
	regexp.MustCompile(`^changeset$`),
	regexp.MustCompile(`^add_error$`),
	regexp.MustCompile(`^apply_action$`),
	regexp.MustCompile(`^apply_action!$`),
	regexp.MustCompile(`^apply_changes$`),
	regexp.MustCompile(`^fetch_field$`),
	regexp.MustCompile(`^fetch_field!$`),
	regexp.MustCompile(`^get_field$`),
	regexp.MustCompile(`^get_change$`),

	// ── Logger module calls ──────────────────────────────────────────
	// `Logger.debug/info/warn/error` arrive as bare `debug`/`info`/
	// `warn`/`error` after receiver-stripping. Logger is an OTP built-in
	// never indexed in-tree.
	regexp.MustCompile(`^debug$`),
	regexp.MustCompile(`^info$`),
	regexp.MustCompile(`^warning$`),
	regexp.MustCompile(`^error$`),
	regexp.MustCompile(`^critical$`),
	regexp.MustCompile(`^notice$`),

	// ── Ecto.Query DSL (from / where / select / order_by …) ─────────
	// `from q in Query, where: ..., select: ...` macro and named-
	// binding query functions are injected by `import Ecto.Query`.
	regexp.MustCompile(`^from$`),
	regexp.MustCompile(`^where$`),
	regexp.MustCompile(`^select$`),
	regexp.MustCompile(`^order_by$`),
	regexp.MustCompile(`^group_by$`),
	regexp.MustCompile(`^having$`),
	regexp.MustCompile(`^join$`),
	regexp.MustCompile(`^limit$`),
	regexp.MustCompile(`^offset$`),
	regexp.MustCompile(`^lock$`),
	regexp.MustCompile(`^distinct$`),
	regexp.MustCompile(`^subquery$`),
	regexp.MustCompile(`^exclude$`),
	regexp.MustCompile(`^fragment$`),
	regexp.MustCompile(`^dynamic$`),
}

func init() {
	dynamicPatternsByLang["elixir"] = elixirDynamicPatterns
}
