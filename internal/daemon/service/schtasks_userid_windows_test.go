//go:build windows

package service

import (
	"strings"
	"testing"
)

// renderTaskXMLWithSID renders the daemon task with a caller-controlled SID so
// the conditional <UserId> logic can be exercised deterministically (the
// production GenerateTaskXML derives the SID from the live user).
//
// It goes through the production renderer (renderTaskXML, schtasks_policy.go)
// rather than re-executing the template itself: a helper that parsed the
// template on its own would keep passing after production stopped using it.
func renderTaskXMLWithSID(t *testing.T, sid string) string {
	t.Helper()
	out, err := renderTaskXML(daemonTaskVars{
		TaskName:        "com.grafel.daemon",
		UserSID:         sid,
		WrapperHost:     `C:\Windows\System32\wscript.exe`,
		WrapperPath:     `C:\Users\testuser\wrapper.vbs`,
		RestartInterval: intervalXML(restartOnFailureInterval),
		RestartCount:    restartOnFailureCount,
	})
	if err != nil {
		t.Fatalf("render task XML: %v", err)
	}
	return string(out)
}

// TestTaskXML_UserIDPresentWhenSIDKnown verifies a non-empty SID renders a
// <UserId> element carrying that SID (bug 3).
func TestTaskXML_UserIDPresentWhenSIDKnown(t *testing.T) {
	const sid = "S-1-5-21-1111111111-2222222222-3333333333-1001"
	out := renderTaskXMLWithSID(t, sid)
	want := "<UserId>" + sid + "</UserId>"
	if !strings.Contains(out, want) {
		t.Errorf("expected %q in rendered XML:\n%s", want, out)
	}
}

// TestTaskXML_UserIDOmittedWhenSIDEmpty verifies an empty SID degrades to "no
// <UserId>" (fire on any logon) instead of invalid <UserId></UserId>, which
// Task Scheduler rejects (bug 3).
func TestTaskXML_UserIDOmittedWhenSIDEmpty(t *testing.T) {
	out := renderTaskXMLWithSID(t, "")
	if strings.Contains(out, "<UserId>") {
		t.Errorf("expected no <UserId> element when SID is empty, got:\n%s", out)
	}
	if !strings.Contains(out, "<LogonTrigger>") {
		t.Errorf("expected LogonTrigger even without a UserId:\n%s", out)
	}
}

// TestCurrentUserSID_Shape verifies the native-API SID resolver returns either
// an empty string or a trimmed Windows SID (starts with "S-"), never raw
// whoami CSV output (bug 3 — no more shelling out to a PATH-shadowable whoami).
func TestCurrentUserSID_Shape(t *testing.T) {
	sid := currentUserSID()
	if sid == "" {
		return // acceptable: degrades to "any logon"
	}
	if sid != strings.TrimSpace(sid) {
		t.Errorf("SID is not trimmed: %q", sid)
	}
	if !strings.HasPrefix(sid, "S-") {
		t.Errorf("expected a Windows SID (S-...), got %q", sid)
	}
}
