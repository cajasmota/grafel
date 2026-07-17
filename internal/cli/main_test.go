package cli

import (
	"os"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestMain fail-closes the package: when GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1
// it refuses to run if HOME is the real user home. Several tests in this
// package (tools enable/disable, wizard tool selection) reach
// install.ApplyToolDelta -> mcpreg.Register/Unregister, which write real
// per-tool MCP config files (e.g. ~/.cursor/mcp.json, ~/.codeium/...) keyed
// off $HOME — they must never touch the developer's live config.
func TestMain(m *testing.M) {
	testsupport.GuardRealHomeMain()
	os.Exit(m.Run())
}
