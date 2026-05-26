// Webhook endpoint detection — #728.
//
// This pass identifies HTTP endpoints that are registered with an external
// service to receive event callbacks ("inbound webhooks"). It tags matching
// http_endpoint entities with `is_webhook=true` and `webhook_provider=<vendor>`.
// It also emits a SUBSCRIBES_TO edge from the endpoint entity to a synthetic
// SCOPE.External entity representing the external service.
//
// A CONFIDENCE property is attached to every webhook entity based on how many
// independent heuristics matched. High confidence (≥3): decorator name contains
// "webhook" + signature-verification import + provider library import. Medium
// confidence (2): any two. Low confidence (1): decorator name alone.
//
// Provider detection:
//   - Stripe       — stripe.Webhook.construct_event / stripe.webhook / X-Stripe-Signature
//   - GitHub       — X-Hub-Signature-256 + HMAC verification
//   - Twilio       — twilio.request_validator / X-Twilio-Signature
//   - Slack        — slack_sdk.signature / X-Slack-Signature
//   - SendGrid     — sendgrid + X-Twilio-Email-Event-Webhook-Signature
//   - Mailgun      — mailgun + webhook + X-Mailgun-Signature
//   - Svix (generic) — svix.Webhook / webhook.verify
//   - Generic      — route path or function name contains "webhook"
//
// Relationships emitted:
//
//	SUBSCRIBES_TO : <http_endpoint entity> → SCOPE.External:<vendor>
//
// This reuses the existing RelationshipKindSubscribesTo constant (SUBSCRIBES_TO)
// which was introduced in #726 for messaging consumers. The semantic generalises
// cleanly: "this endpoint subscribes to callbacks from the external provider."
//
// All emissions are append-only; existing entities/edges are never modified.
//
// Refs #728.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// webhookExternalKind is the entity kind for synthetic external provider nodes.
const webhookExternalKind = "SCOPE.External"

// applyWebhookEdges is the per-file entry point. Appends is_webhook-tagged
// entities + SUBSCRIBES_TO edges; never modifies existing ones.
func applyWebhookEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := string(content)

	seenExternal := map[string]bool{}

	emitWebhook := func(endpointID, provider, route string, confidence int) {
		// Tag / create the endpoint entity.
		confLabel := webhookConfidenceLabel(confidence)
		extID := "external:" + provider + ":webhook"

		// Emit the external provider entity (once per file per provider).
		if !seenExternal[extID] {
			seenExternal[extID] = true
			entities = append(entities, types.EntityRecord{
				Name:     extID,
				Kind:     webhookExternalKind,
				Language: lang,
				Properties: map[string]string{
					"provider":     provider,
					"service_type": "webhook_source",
					"pattern_type": "webhook_synthesis",
				},
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.7,
			})
		}

		// Emit the endpoint entity (tagged as webhook).
		epID := webhookEndpointID(provider, route, path)
		entities = append(entities, types.EntityRecord{
			Name:       epID,
			Kind:       "http_endpoint",
			SourceFile: path,
			Language:   lang,
			Properties: map[string]string{
				"is_webhook":       "true",
				"webhook_provider": provider,
				"route":            route,
				"confidence":       confLabel,
				"pattern_type":     "webhook_synthesis",
			},
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.75,
		})

		// Emit SUBSCRIBES_TO edge: endpoint → external provider.
		relationships = append(relationships, types.RelationshipRecord{
			FromID: "http_endpoint:" + epID,
			ToID:   fmt.Sprintf("%s:%s", webhookExternalKind, extID),
			Kind:   subscribesToEdgeKind, // "SUBSCRIBES_TO" from kafka_edges.go
			Properties: map[string]string{
				"webhook_provider": provider,
				"confidence":       confLabel,
				"pattern_type":     "webhook_synthesis",
			},
		})
	}

	switch lang {
	case "python":
		synthesizePyWebhooks(src, path, emitWebhook)
	case "javascript", "typescript":
		synthesizeNodeWebhooks(src, path, emitWebhook)
	case "java", "kotlin":
		synthesizeJavaWebhooks(src, path, lang, emitWebhook)
	case "go":
		synthesizeGoWebhooks(src, path, emitWebhook)
	case "ruby":
		synthesizeRubyWebhooks(src, path, emitWebhook)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// ---------------------------------------------------------------------------
// Python — Flask / FastAPI / Django webhook endpoints
// ---------------------------------------------------------------------------

// pyRouteWebhookRe captures Flask/FastAPI route decorators whose path or
// name contains "webhook". Group 1 = path.
var pyRouteWebhookRe = regexp.MustCompile(`@(?:app|router|blueprint|bp)\.(?:route|post|get)\s*\(\s*['"]([^'"]*webhook[^'"]*)['"]`)

// pyStripeConstructEventRe detects Stripe webhook handler.
var pyStripeConstructEventRe = regexp.MustCompile(`stripe\.Webhook\.construct_event\s*\(`)

// pyStripeImportRe checks for stripe import.
var pyStripeImportRe = regexp.MustCompile(`import\s+stripe`)

// pyGitHubSignatureRe detects GitHub webhook signature header check.
var pyGitHubSignatureRe = regexp.MustCompile(`X-Hub-Signature(?:-256)?`)

// pyTwilioValidatorRe detects Twilio request validator.
var pyTwilioValidatorRe = regexp.MustCompile(`RequestValidator|twilio\.request_validator`)

// pySlackVerifierRe detects Slack signature verifier.
var pySlackVerifierRe = regexp.MustCompile(`SignatureVerifier|slack_sdk\.signature`)

// pyMailgunSignatureRe detects Mailgun webhook signature.
var pyMailgunSignatureRe = regexp.MustCompile(`mailgun.*(?:webhook|signature)|X-Mailgun-Signature`)

// pySvixVerifyRe detects Svix webhook verification.
var pySvixVerifyRe = regexp.MustCompile(`svix\.Webhook|wh\.verify\s*\(`)

// pyHMACRe is a generic HMAC check often accompanying webhook verification.
var pyHMACRe = regexp.MustCompile(`hmac\.(?:new|compare_digest|HMAC)|hashlib\.sha(?:1|256)`)

func synthesizePyWebhooks(
	src, path string,
	emitWebhook func(endpointID, provider, route string, confidence int),
) {
	provider, confidence := detectWebhookProvider(src, "python")

	// Path-based detection: route decorator has "webhook" in path.
	for _, m := range pyRouteWebhookRe.FindAllStringSubmatch(src, -1) {
		route := m[1]
		prov := provider
		if prov == "generic" {
			prov = inferProviderFromPath(route)
		}
		emitWebhook(path+":"+route, prov, route, confidence)
	}

	// Signature-based detection even without explicit route decorator.
	if provider != "generic" {
		emitWebhook(path+":sig", provider, inferRouteFromPath(path), confidence)
	}
}

// ---------------------------------------------------------------------------
// Node / TypeScript — Express / Fastify / NestJS
// ---------------------------------------------------------------------------

// nodeRouteWebhookRe captures Express/Fastify/NestJS route registrations
// with "webhook" in the path. Group 1 = HTTP method, group 2 = path.
var nodeRouteWebhookRe = regexp.MustCompile(`(?:app|router|fastify|server)\.(get|post|put|patch|delete)\s*\(\s*['"\x60]([^'"\x60]*webhook[^'"\x60]*)['"\x60]`)

// nodeStripeConstructEventRe detects Stripe webhook.
var nodeStripeConstructEventRe = regexp.MustCompile(`stripe\.webhooks\.constructEvent\s*\(`)

// nodeGitHubSignatureRe detects GitHub webhook signature.
var nodeGitHubSignatureRe = regexp.MustCompile(`x-hub-signature|X-Hub-Signature`)

// nodeTwilioSignatureRe detects Twilio validation.
var nodeTwilioSignatureRe = regexp.MustCompile(`twilio\.validateRequest|validateExpressRequest`)

// nodeSlackVerifyRe detects Slack webhook.
var nodeSlackVerifyRe = regexp.MustCompile(`@slack/bolt|verifyRequestSignature|slack\.receiver`)

// nodeSvixVerifyRe detects Svix.
var nodeSvixVerifyRe = regexp.MustCompile(`svix|wh\.verify\s*\(`)

// nodeBodyRawRe detects `express.raw({type: 'application/json'})` commonly
// used for webhook signature validation (raw body needed).
var nodeBodyRawRe = regexp.MustCompile(`express\.raw\s*\(\s*\{[^}]*type\s*:\s*['"]application/json['"]`)

func synthesizeNodeWebhooks(
	src, path string,
	emitWebhook func(endpointID, provider, route string, confidence int),
) {
	provider, confidence := detectWebhookProvider(src, "node")

	for _, m := range nodeRouteWebhookRe.FindAllStringSubmatch(src, -1) {
		route := m[2]
		prov := provider
		if prov == "generic" {
			prov = inferProviderFromPath(route)
		}
		emitWebhook(path+":"+route, prov, route, confidence)
	}

	if provider != "generic" {
		emitWebhook(path+":sig", provider, inferRouteFromPath(path), confidence)
	}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Spring / JAX-RS
// ---------------------------------------------------------------------------

// javaWebhookRouteRe captures `@RequestMapping("/webhook...")` or
// `@PostMapping("/stripe/webhook")`. Group 1 = path.
var javaWebhookRouteRe = regexp.MustCompile(`@(?:Request|Post|Get|Put|Delete|Patch)Mapping\s*\(\s*(?:value\s*=\s*)?["']([^"']*webhook[^"']*)["']`)

// javaStripeEventRe detects Stripe Java SDK.
var javaStripeEventRe = regexp.MustCompile(`Event\.constructFrom|com\.stripe\.net\.Webhook`)

// javaGitHubSignatureRe detects GitHub webhook header.
var javaGitHubSignatureRe = regexp.MustCompile(`X-Hub-Signature|HmacUtils`)

func synthesizeJavaWebhooks(
	src, path, lang string,
	emitWebhook func(endpointID, provider, route string, confidence int),
) {
	provider, confidence := detectWebhookProvider(src, "java")

	for _, m := range javaWebhookRouteRe.FindAllStringSubmatch(src, -1) {
		route := m[1]
		prov := provider
		if prov == "generic" {
			prov = inferProviderFromPath(route)
		}
		emitWebhook(path+":"+route, prov, route, confidence)
	}

	if provider != "generic" {
		emitWebhook(path+":sig", provider, inferRouteFromPath(path), confidence)
	}
}

// ---------------------------------------------------------------------------
// Go — net/http / Gin / Echo
// ---------------------------------------------------------------------------

// goWebhookHandleRe captures `http.HandleFunc("/webhook...", handler)` and
// gin/echo equivalents. Group 1 = path.
var goWebhookHandleRe = regexp.MustCompile(`(?:HandleFunc|GET|POST|PUT|PATCH|DELETE|Any)\s*\(\s*["'` + "`" + `]([^"'` + "`" + `]*webhook[^"'` + "`" + `]*)["'` + "`" + `]`)

// goStripeRe detects Stripe Go SDK.
var goStripeRe = regexp.MustCompile(`stripe\.ConstructEvent\s*\(`)

// goGitHubSignatureRe detects GitHub webhook validation.
var goGitHubSignatureRe = regexp.MustCompile(`X-Hub-Signature|github\.com/google/go-github`)

func synthesizeGoWebhooks(
	src, path string,
	emitWebhook func(endpointID, provider, route string, confidence int),
) {
	provider, confidence := detectWebhookProvider(src, "go")

	for _, m := range goWebhookHandleRe.FindAllStringSubmatch(src, -1) {
		route := m[1]
		prov := provider
		if prov == "generic" {
			prov = inferProviderFromPath(route)
		}
		emitWebhook(path+":"+route, prov, route, confidence)
	}

	if provider != "generic" {
		emitWebhook(path+":sig", provider, inferRouteFromPath(path), confidence)
	}
}

// ---------------------------------------------------------------------------
// Ruby — Rails / Sinatra
// ---------------------------------------------------------------------------

// rubyWebhookRouteRe captures Rails route definitions with "webhook" in path.
var rubyWebhookRouteRe = regexp.MustCompile(`(?:post|get|put|patch|delete)\s+['"]([^'"]*webhook[^'"]*)['"]`)

// rubyStripeEventRe detects Stripe Ruby.
var rubyStripeEventRe = regexp.MustCompile(`Stripe::Webhook\.construct_event`)

func synthesizeRubyWebhooks(
	src, path string,
	emitWebhook func(endpointID, provider, route string, confidence int),
) {
	provider, confidence := detectWebhookProvider(src, "ruby")

	for _, m := range rubyWebhookRouteRe.FindAllStringSubmatch(src, -1) {
		route := m[1]
		prov := provider
		if prov == "generic" {
			prov = inferProviderFromPath(route)
		}
		emitWebhook(path+":"+route, prov, route, confidence)
	}
}

// ---------------------------------------------------------------------------
// Cross-language provider + confidence detection
// ---------------------------------------------------------------------------

// detectWebhookProvider inspects the full file source and returns the most
// specific provider name + a confidence score (1–3).
// - confidence 1: route/function name alone
// - confidence 2: route + one verification signal
// - confidence 3: route + verification + provider library import
func detectWebhookProvider(src, langHint string) (provider string, confidence int) {
	confidence = 0
	provider = "generic"

	// Provider-specific signals — each sets provider and accumulates confidence.
	switch {
	case pyStripeConstructEventRe.MatchString(src) ||
		nodeStripeConstructEventRe.MatchString(src) ||
		strings.Contains(src, "stripe.Webhook.construct_event") ||
		strings.Contains(src, "stripe.webhooks.constructEvent") ||
		strings.Contains(src, "Stripe::Webhook.construct_event") ||
		strings.Contains(src, "stripe.ConstructEvent("):
		provider = "stripe"
		confidence += 2
	case pyGitHubSignatureRe.MatchString(src) || nodeGitHubSignatureRe.MatchString(src) ||
		goGitHubSignatureRe.MatchString(src) || javaGitHubSignatureRe.MatchString(src):
		provider = "github"
		confidence += 2
	case pyTwilioValidatorRe.MatchString(src) || nodeTwilioSignatureRe.MatchString(src):
		provider = "twilio"
		confidence += 2
	case pySlackVerifierRe.MatchString(src) || nodeSlackVerifyRe.MatchString(src):
		provider = "slack"
		confidence += 2
	case pyMailgunSignatureRe.MatchString(src):
		provider = "mailgun"
		confidence += 2
	case pySvixVerifyRe.MatchString(src) || nodeSvixVerifyRe.MatchString(src):
		provider = "svix"
		confidence += 2
	}

	// Library import bonus.
	if importConfirmation(src, provider, langHint) {
		confidence++
	}

	// Generic webhook path/name pattern.
	if strings.Contains(strings.ToLower(src), "webhook") {
		confidence = max(confidence, 1)
		if provider == "generic" {
			confidence = 1
		}
	}

	return provider, confidence
}

// importConfirmation returns true when the source contains an import for the
// named provider's library.
func importConfirmation(src, provider, langHint string) bool {
	switch provider {
	case "stripe":
		return strings.Contains(src, "import stripe") || strings.Contains(src, "require('stripe')") ||
			strings.Contains(src, `"github.com/stripe/stripe-go"`) ||
			strings.Contains(src, "import com.stripe")
	case "github":
		return strings.Contains(src, "from github") || strings.Contains(src, "import github") ||
			strings.Contains(src, "github3") || strings.Contains(src, "go-github") ||
			strings.Contains(src, "octokit")
	case "twilio":
		return strings.Contains(src, "import twilio") || strings.Contains(src, "require('twilio')") ||
			strings.Contains(src, "from twilio") || strings.Contains(src, "twilio-ruby") ||
			strings.Contains(src, "com.twilio")
	case "slack":
		return strings.Contains(src, "import slack") || strings.Contains(src, "from slack_sdk") ||
			strings.Contains(src, "@slack/bolt") || strings.Contains(src, "slack-ruby-client")
	case "mailgun":
		return strings.Contains(src, "mailgun") || strings.Contains(src, "Mailgun")
	case "svix":
		return strings.Contains(src, "svix")
	}
	return false
}

// inferProviderFromPath attempts to derive the provider from path tokens like
// "/stripe/webhook" or "/github/events". Falls back to "generic".
func inferProviderFromPath(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "stripe"):
		return "stripe"
	case strings.Contains(lower, "github"):
		return "github"
	case strings.Contains(lower, "twilio"):
		return "twilio"
	case strings.Contains(lower, "slack"):
		return "slack"
	case strings.Contains(lower, "mailgun"):
		return "mailgun"
	case strings.Contains(lower, "sendgrid"):
		return "sendgrid"
	}
	return "generic"
}

// inferRouteFromPath derives a plausible route from the file path when no
// explicit route decorator is found but a signature detection matched.
func inferRouteFromPath(filePath string) string {
	lower := strings.ToLower(filePath)
	if idx := strings.LastIndex(lower, "webhook"); idx >= 0 {
		// Return "/webhook" as a safe default.
		return "/webhook"
	}
	return "/"
}

// webhookEndpointID returns a deterministic ID for a webhook endpoint entity.
func webhookEndpointID(provider, route, path string) string {
	return "webhook:" + provider + ":" + route + "@" + path
}

// webhookConfidenceLabel maps an integer score to a human-readable label.
func webhookConfidenceLabel(c int) string {
	switch {
	case c >= 3:
		return "high"
	case c == 2:
		return "medium"
	default:
		return "low"
	}
}

// max returns the larger of a and b.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
