package extractors

import (
	"os"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
)

// InProcCustomExtractorsEnabled reports whether an in-process indexing path
// should additionally dispatch the custom/framework extractors registered under
// internal/custom/** (issue #5989) via RunCustomExtractors.
//
// ONE READER, TWO CALLERS. The env var used to be read only by
// cmd/grafel/inproc_custom.go, which is why the daemon's incremental path could
// not consult it without importing cmd/grafel. #6960: the gate lives here, in
// the package that owns the dispatcher, and cmd/grafel/inproc_custom.go
// delegates. A second literal reading of the same variable would be free to
// drift from this one — that drift is the failure this placement prevents, and
// it is exactly the failure that made incremental and full disagree.
//
// The default stays OFF; see cmd/grafel/inproc_custom.go for the cost rationale
// and internal/extractors/incremental.go's dispatch site for why the incremental
// path must read the SAME gate rather than choosing its own answer: the property
// incremental owes its caller is "same graph as a full reindex", and a pass that
// runs custom extractors when the full path did not is as wrong as one that
// skips them when the full path did.
func InProcCustomExtractorsEnabled() bool {
	v := strings.TrimSpace(os.Getenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// CustomExtractorsEnabled is the WHOLE gate — the config half and the env half —
// and is what every dispatch site should call.
//
// The full in-process path's condition was `i.customExtractors ||
// inProcCustomExtractors()`. A pass with no Indexer cannot evaluate the first
// half, and #6960's first cut read only the second, which left the original
// defect alive in the WithCustomExtractors lane: a full index opted in
// programmatically emits custom entities, and an incremental pass that saw only
// an unset env var would drop them on the next edit. Routing the programmatic
// opt-in through ExtractorConfig.InProcCustomExtractors — which TryIncremental
// already receives — makes the two paths evaluate the same expression over the
// same inputs.
//
// CONFIG WINS OVER ENV, deliberately, and in BOTH directions: an explicit
// `false` in Config suppresses a set env var. That is ExtractorConfig's own
// established precedence (#2320, "Config-first/env-fallback") and the tri-state
// pointer is what distinguishes "Config says no" from "Config did not say".
func CustomExtractorsEnabled(cfg *extractor.ExtractorConfig) bool {
	if cfg != nil && cfg.InProcCustomExtractors != nil {
		return *cfg.InProcCustomExtractors
	}
	return InProcCustomExtractorsEnabled()
}
