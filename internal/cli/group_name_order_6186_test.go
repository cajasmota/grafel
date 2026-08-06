package cli

import (
	"strings"
	"testing"
)

// #6186 F6 (found on review): every real caller of registry.AddGroup wrote
// the group config to disk (registry.SaveGroupConfig) BEFORE AddGroup's
// name validation ever ran. registry.ConfigPathFor filepath.Joins the raw
// name, which collapses "..", so an unvalidated name could write a config
// file outside the intended config directory; the AddGroup rejection that
// followed only left the stray file behind.
//
// These two pin the source order at the two internal/cli call sites (the
// dashboard and internal/install call sites have their own direct
// behavioral tests: internal/dashboard/create_group_traversal_6186_test.go,
// internal/install/group_name_traversal_6186_test.go). Source-order
// checking, not a full behavioral run, because reproducing the wizard TUI /
// onboard flow end-to-end just to observe write ordering is disproportionate
// to what's being pinned — reuses the readSource/funcBody helpers already
// established in watchersync_wiring_6179_test.go for exactly this kind of
// "call X before call Y" invariant.
func TestApplyGroupConfig_ValidatesBeforeSaving(t *testing.T) {
	src := readSource(t, "wizard.go")
	fn := funcBody(t, src, "applyGroupConfig")
	assertValidatesBeforeSave(t, fn, "applyGroupConfig (wizard.go)")
}

func TestRunOnboard_ValidatesBeforeSaving(t *testing.T) {
	src := readSource(t, "onboard.go")
	fn := funcBody(t, src, "runOnboard")
	assertValidatesBeforeSave(t, fn, "runOnboard (onboard.go)")
}

func assertValidatesBeforeSave(t *testing.T, fn, where string) {
	t.Helper()
	iValidate := strings.Index(fn, "registry.ValidateGroupName")
	iSave := strings.Index(fn, "registry.SaveGroupConfig")
	if iValidate < 0 {
		t.Fatalf("%s does not call registry.ValidateGroupName at all (#6186 F6)", where)
	}
	if iSave < 0 {
		t.Fatalf("%s does not call registry.SaveGroupConfig (test assumption broke)", where)
	}
	if iValidate > iSave {
		t.Errorf("%s calls registry.SaveGroupConfig before registry.ValidateGroupName; an "+
			"unvalidated name (e.g. \"../../pwned\") can write a config file outside the config "+
			"directory before the name is ever rejected (#6186 F6)", where)
	}
}
