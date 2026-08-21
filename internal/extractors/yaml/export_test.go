package yaml

// export_test.go exposes internal test hooks to the external `yaml_test`
// package. The YAML extractor's tests live in `package yaml_test` (they drive
// the extractor through the public extractor.Get registry), so an unexported
// hook is otherwise unreachable from them.

// SetHelmSkipOutput redirects the #6416 Helm sibling-file skip report and
// returns a restore func. Test-only.
var SetHelmSkipOutput = setHelmSkipOutput
