package extractors

import (
	"os"
	"strings"
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
