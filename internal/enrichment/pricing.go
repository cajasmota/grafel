package enrichment

// pricing.go — Anthropic model pricing constants used by the cost estimator.
//
// Update these constants when Anthropic publishes new pricing.
// All values are in USD per million tokens.
//
// Source: Anthropic pricing page (last updated 2025-Q2).

// Input pricing (USD per million tokens).
const (
	HaikuInputPerMTok   = 0.80
	HaikuOutputPerMTok  = 4.00
	SonnetInputPerMTok  = 3.00
	SonnetOutputPerMTok = 15.00
)

// inputRatio is the fraction of total tokens that are input tokens.
// Based on the enrichment task profile: ~70% input, 30% output.
const inputRatio = 0.70

// outputRatio is the complementary output fraction.
const outputRatio = 1.0 - inputRatio

// tokensPerSecondPerWorker is the assumed throughput for wall-time estimation.
// Calibrated against observed batch runs with 30 parallel workers.
const tokensPerSecondPerWorker = 500

// parallelWorkers is the assumed concurrency level for wall-time estimation.
const parallelWorkers = 30

// TokensPerEntity returns the conservative (upper-bound) token estimate for a
// single entity of the given kind. The estimate covers both prompt (input) and
// generated output (output) combined — the pricing helpers split it by ratio.
func TokensPerEntity(kind string) int {
	switch kind {
	// API surface — most context-heavy: path, params, response shapes.
	case "http_endpoint", "HTTPEndpoint", "SCOPE.HTTPEndpoint", "Route", "SCOPE.Route":
		return 900
	// Process flows — steps + preconditions.
	case "process_flow", "Process", "SCOPE.ScheduledJob":
		return 700
	// Message topics — schema + consumers.
	case "message_topic", "Task":
		return 500
	// Services, controllers, views.
	case "Service", "SCOPE.Service", "Controller", "SCOPE.Controller",
		"View", "SCOPE.View":
		return 600
	// Data layer.
	case "Schema", "Model", "DataAccess", "SCOPE.DataAccess":
		return 550
	// Generic operations / components.
	default:
		return 500
	}
}

// promptOverheadTokens is added once per entity call to account for the fixed
// system prompt, instruction preamble, and JSON scaffolding.
const promptOverheadTokens = 200

// EstimateEntityTokens returns the total estimated tokens for one entity,
// including prompt overhead.
func EstimateEntityTokens(kind string) int {
	return TokensPerEntity(kind) + promptOverheadTokens
}

// USDForTokens computes the estimated cost in USD for the given total token
// count at the given model's rates. The input/output split is 70/30.
func USDForTokens(totalTokens int, model string) float64 {
	input := float64(totalTokens) * inputRatio
	output := float64(totalTokens) * outputRatio

	var inRate, outRate float64
	switch model {
	case "sonnet":
		inRate, outRate = SonnetInputPerMTok, SonnetOutputPerMTok
	default: // haiku
		inRate, outRate = HaikuInputPerMTok, HaikuOutputPerMTok
	}

	// Rates are per million tokens.
	return (input*inRate + output*outRate) / 1_000_000
}

// EstimateWallMinutes returns a rough wall-clock estimate in minutes for
// processing the given total token count with parallelWorkers concurrent
// workers at tokensPerSecondPerWorker throughput.
func EstimateWallMinutes(totalTokens int) float64 {
	if totalTokens == 0 {
		return 0
	}
	totalSeconds := float64(totalTokens) / float64(tokensPerSecondPerWorker*parallelWorkers)
	return totalSeconds / 60.0
}
