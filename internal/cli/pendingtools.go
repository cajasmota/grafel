package cli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
)

// pending_tools.json stashes a per-tool selection made by `grafel install
// --tools` (or its picker) BEFORE any group exists. Historically that
// selection was dropped on the floor (install.go printed "saved on first group
// registration" but persisted nothing), so the very next `wizard`/`group add`
// re-defaulted to all-tools and scaffolded rules folders + MCP for every
// adapter — the #5701 ordering footgun. We now persist the intent here and the
// next group registration consumes it (see applyGroupConfig).
const pendingToolsFile = "pending_tools.json"

// pendingToolsPath returns the path to the pending-selection file under the
// grafel home dir.
func pendingToolsPath() (string, error) {
	home, err := registry.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, pendingToolsFile), nil
}

// savePendingTools writes ids as the pending per-tool selection. ids is assumed
// already validated/normalized (registry-order adapter IDs).
func savePendingTools(ids []string) error {
	p, err := pendingToolsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(struct {
		Tools []string `json:"tools"`
	}{Tools: ids}, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// consumePendingTools reads and DELETES the pending selection, returning the
// normalized adapter IDs (nil when none/invalid). It is deliberately best-
// effort: a missing or malformed file yields nil with no error, and the file is
// removed once read so a stashed selection is applied to exactly one group.
func consumePendingTools() []string {
	p, err := pendingToolsPath()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	// Consume: remove regardless of parse outcome so a corrupt file can't wedge
	// every future group registration.
	_ = os.Remove(p)
	var doc struct {
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil
	}
	ids := tooladapter.NormalizeSelection(doc.Tools)
	if len(ids) == 0 {
		return nil
	}
	return ids
}
