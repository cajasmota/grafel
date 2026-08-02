package main

import (
	"os"
	"strings"
)

// inProcCustomExtractors reports whether the DEFAULT in-process indexing path
// should additionally dispatch the custom/framework extractors registered
// under internal/custom/** (issue #5989).
//
// WHY THIS GATE EXISTS. ~340 framework extractors (Django/DRF/Celery/Flask/
// FastAPI/SQLAlchemy plus 20 other languages, ~141k lines) register in the
// shared extractor registry under PREFIXED keys — "python_django",
// "custom_js_grafeo", … — while ordinary dispatch does an exact-key
// Get(file.Language) lookup on "python". That lookup can never match a
// prefixed key, so none of these extractors has ever run on the default path.
// extractors.RunCustomExtractors is the only dispatcher that selects them, and
// before this change its sole non-test caller sat behind GRAFEL_SUBPROC_EXTRACT
// — an env var with zero writers anywhere in the tree. This was never
// connected; it is not a regression.
//
// WHY IT IS DEFAULT-OFF. Epic #5954 exists to REDUCE indexing peak heap and
// wall time; running 340 additional extractors per file moves both the wrong
// way. The gate keeps default behaviour byte-for-byte unchanged so the cost
// can be measured before anyone decides to flip it.
//
// NOT A FIX FOR GRAFEL_SUBPROC_EXTRACT. The two branches are disjoint, not
// superset/subset: the subprocess branch leaves `classified` nil, which
// silently disables ~12 framework post-passes, and on a Django fixture it is a
// NET LOSS (entities 201→186, http_endpoint_definition 18→4). That hole is
// tracked separately as #6102 and is out of scope here.
func inProcCustomExtractors() bool {
	v := strings.TrimSpace(os.Getenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}
