package coverage

// Config is the per-group coverage-ingestion setting. It mirrors SonarQube's
// `sonar.<lang>.lcov.reportPaths` / `sonar.coverageReportPaths`: archigraph does
// not run tests, it ingests the report CI already emits.
//
// Intended shape in a group config (YAML/JSON), e.g.:
//
//	coverage:
//	  format: lcov                 # v1: only "lcov" is honored
//	  report_paths:                # globs, repo-relative, evaluated at index time
//	    - "coverage/lcov.info"
//	    - "packages/*/coverage/lcov.info"
//	  root_prefix: ""              # optional LCOV path root to strip (see Normalize)
//
// v1 wires the struct + parser/attribution; resolving the globs and stamping
// entities happens in the indexer hook (a follow-up — see ApplyAttributions).
type Config struct {
	// Format selects the parser. v1 supports only "lcov". Empty disables
	// coverage ingestion for the group.
	Format string `json:"format,omitempty" yaml:"format,omitempty"`
	// ReportPaths are repo-relative globs to the coverage artifact(s).
	ReportPaths []string `json:"report_paths,omitempty" yaml:"report_paths,omitempty"`
	// RootPrefix is an optional path prefix stripped from report file paths so
	// they normalize to repo-relative (see Normalize).
	RootPrefix string `json:"root_prefix,omitempty" yaml:"root_prefix,omitempty"`
}

// Config.Format values selecting a parser. When empty, the format is detected
// from the report file (extension + root element) — see DetectFormat.
const (
	FormatLCOV      = "lcov"
	FormatCobertura = "cobertura"
	FormatJaCoCo    = "jacoco"
)

// Enabled reports whether the group has opted into coverage ingestion.
func (c Config) Enabled() bool {
	return c.Format != "" && len(c.ReportPaths) > 0
}
