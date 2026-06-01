package elixir

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_elixir_ueberauth", &ueberauthExtractor{})
}

// ueberauthExtractor recognises Ueberauth (https://github.com/ueberauth/ueberauth),
// the standard Elixir multi-provider OAuth authentication library.
//
// A Ueberauth pipeline is wired in three places:
//
//  1. The router plugs the library:           `plug Ueberauth`
//  2. Strategies are configured (config.exs):
//     config :ueberauth, Ueberauth,
//     providers: [
//     github: {Ueberauth.Strategy.Github, []},
//     google: {Ueberauth.Strategy.Google, [default_scope: "email"]}
//     ]
//  3. The auth controller handles the callbacks:
//     `def callback(%{assigns: %{ueberauth_auth: auth}} = conn, _params)` and
//     strategy modules implement `handle_request!/1` + `handle_callback!/1`.
//
// Emitted entities (all carry auth=true so auth_coverage counts them):
//   - SCOPE.Component/auth : the `plug Ueberauth` pipeline entrypoint.
//   - SCOPE.Component/auth : each configured `Ueberauth.Strategy.<Provider>`
//     (one OAuth provider strategy per entity, auth_provider=<provider>).
//   - SCOPE.Operation/auth : `handle_request!` / `handle_callback!` strategy
//     callbacks (the OAuth request/callback handlers).
type ueberauthExtractor struct{}

func (e *ueberauthExtractor) Language() string { return "custom_elixir_ueberauth" }

var (
	// plug Ueberauth   (router entrypoint; NOT Ueberauth.Strategy.* which is a
	// strategy reference, so require end-of-token after "Ueberauth").
	reUeberauthPlug = regexp.MustCompile(
		`(?m)^\s*plug\s+Ueberauth\b(?:\s*,|\s*$)`,
	)
	// Ueberauth.Strategy.Github  /  Ueberauth.Strategy.Google
	reUeberauthStrategy = regexp.MustCompile(
		`Ueberauth\.Strategy\.([A-Z]\w+)`,
	)
	// def handle_request!(conn)  /  def handle_callback!(conn)
	reUeberauthCallback = regexp.MustCompile(
		`(?m)^\s*def\s+(handle_request!|handle_callback!)\s*\(`,
	)
	// A `provider: {Ueberauth.Strategy.X, opts}` config tuple's leading atom key,
	// e.g. `github: {Ueberauth.Strategy.Github, []}` -> provider key "github".
	reUeberauthProviderKey = regexp.MustCompile(
		`(?m)^\s*(\w+):\s*\{\s*Ueberauth\.Strategy\.`,
	)
)

// ueberauthStrategyProvider normalises a `Ueberauth.Strategy.<X>` module suffix
// into a provider slug (e.g. "Github" -> "github").
func ueberauthStrategyProvider(strategy string) string {
	return strings.ToLower(strategy)
}

func (e *ueberauthExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.ueberauth_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "ueberauth"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "elixir" {
		return nil, nil
	}

	src := string(file.Content)

	// Gate: the file must reference Ueberauth somewhere.
	if !strings.Contains(src, "Ueberauth") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. Router entrypoint: `plug Ueberauth`.
	for _, m := range reUeberauthPlug.FindAllStringIndex(src, -1) {
		ent := makeEntity("plug:Ueberauth", "SCOPE.Component", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ueberauth",
			"provenance", "INFERRED_FROM_UEBERAUTH_PLUG",
			"auth", "true",
			"auth_provider", "ueberauth",
			"auth_method", "oauth2",
			"middleware_name", "Ueberauth")
		add(ent)
	}

	// 2. Configured / referenced strategies -> one OAuth provider entity each.
	for _, m := range reUeberauthStrategy.FindAllStringSubmatchIndex(src, -1) {
		strategy := src[m[2]:m[3]]
		provider := ueberauthStrategyProvider(strategy)
		ent := makeEntity("Ueberauth.Strategy."+strategy, "SCOPE.Component", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ueberauth",
			"provenance", "INFERRED_FROM_UEBERAUTH_STRATEGY",
			"auth", "true",
			"auth_provider", provider,
			"auth_method", "oauth2",
			"oauth_provider", provider,
			"strategy", strategy,
			"middleware_name", "Ueberauth.Strategy."+strategy)
		add(ent)
	}

	// 3. Strategy callbacks -> OAuth request/callback handlers.
	for _, m := range reUeberauthCallback.FindAllStringSubmatchIndex(src, -1) {
		cb := src[m[2]:m[3]]
		phase := "request"
		if cb == "handle_callback!" {
			phase = "callback"
		}
		ent := makeEntity(cb, "SCOPE.Operation", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ueberauth",
			"provenance", "INFERRED_FROM_UEBERAUTH_CALLBACK",
			"auth", "true",
			"auth_provider", "ueberauth",
			"auth_method", "oauth2",
			"oauth_phase", phase,
			"handler_type", cb)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ueberauthConfiguredProviders returns the provider atom keys declared in a
// `providers: [ ... ]` Ueberauth config block. Exposed for tests and potential
// reuse by config-aware passes; not all call sites need it.
func ueberauthConfiguredProviders(src string) []string {
	var out []string
	for _, m := range reUeberauthProviderKey.FindAllStringSubmatch(src, -1) {
		out = append(out, m[1])
	}
	return uniqueStrings(out)
}
