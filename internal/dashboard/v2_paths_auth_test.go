// Tests for the v2 dashboard auth chip + auth_policy wire surfacing.
//
// Refs #1942 Phase 1.
package dashboard

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/engine"
)

func TestResolveAuthChip_RolesHighConfidence(t *testing.T) {
	p := engine.AuthPolicy{
		Required:   true,
		Method:     "annotation",
		Roles:      []string{"ADMIN"},
		Confidence: "high",
	}
	label, tone := resolveAuthChip(p)
	if label != "[Roles: ADMIN]" {
		t.Errorf("label = %q", label)
	}
	if tone != authToneAccent {
		t.Errorf("tone = %q", tone)
	}
}

func TestResolveAuthChip_PublicHighConfidence(t *testing.T) {
	p := engine.AuthPolicy{Required: false, Method: "annotation", Confidence: "high"}
	label, tone := resolveAuthChip(p)
	if label != "[Public]" || tone != authToneMuted {
		t.Errorf("got (%q,%q), want ([Public], muted)", label, tone)
	}
}

func TestResolveAuthChip_AuthRequiredHighConfidence(t *testing.T) {
	p := engine.AuthPolicy{Required: true, Method: "annotation", Confidence: "high"}
	label, tone := resolveAuthChip(p)
	if label != "[Auth required]" || tone != authToneAccent {
		t.Errorf("got (%q,%q)", label, tone)
	}
}

func TestResolveAuthChip_FrameworkDefault(t *testing.T) {
	p := engine.AuthPolicy{Required: true, Method: "framework_default", Confidence: "low"}
	label, tone := resolveAuthChip(p)
	if label != "[Auth: default]" || tone != authToneWarning {
		t.Errorf("got (%q,%q)", label, tone)
	}
}

func TestResolveAuthChip_ConfigDriven(t *testing.T) {
	p := engine.AuthPolicy{Required: true, Method: "config", Confidence: "medium"}
	label, tone := resolveAuthChip(p)
	if label != "[Auth: probable]" || tone != authToneWarning {
		t.Errorf("got (%q,%q)", label, tone)
	}
}

func TestResolveAuthChip_Unknown(t *testing.T) {
	p := engine.AuthPolicy{Method: "unknown", Confidence: "low"}
	label, tone := resolveAuthChip(p)
	if label != "[Auth: unknown]" || tone != authToneMuted {
		t.Errorf("got (%q,%q)", label, tone)
	}
}

func TestResolveAuthChip_MultiRoleAbbreviated(t *testing.T) {
	p := engine.AuthPolicy{
		Required:   true,
		Method:     "annotation",
		Roles:      []string{"ADMIN", "USER", "OPS", "AUDITOR"},
		Confidence: "high",
	}
	label, _ := resolveAuthChip(p)
	if label != "[Roles: ADMIN, USER, +2]" {
		t.Errorf("got %q", label)
	}
}

func TestReadAuthPolicyFromEntity_JSONRoundTrip(t *testing.T) {
	in := engine.AuthPolicy{
		Required:   true,
		Method:     "annotation",
		Roles:      []string{"ADMIN"},
		Confidence: "high",
		SourceChain: []engine.AuthSignal{{
			Kind: "annotation", Text: "@RolesAllowed(\"ADMIN\")", File: "X.java", Line: 42,
		}},
	}
	props := map[string]string{"auth_policy": engine.EncodeAuthPolicy(in)}
	got := readAuthPolicyFromEntity(props)
	if !got.Required || got.Confidence != "high" {
		t.Fatalf("got %#v", got)
	}
	if len(got.SourceChain) != 1 || got.SourceChain[0].Line != 42 {
		t.Errorf("source chain lost: %#v", got.SourceChain)
	}
}

func TestReadAuthPolicyFromEntity_LegacyAuthFlag(t *testing.T) {
	// Legacy non-Java endpoint with only the old `auth=true` flag.
	got := readAuthPolicyFromEntity(map[string]string{"auth": "true"})
	if !got.Required || got.Confidence != "high" {
		t.Errorf("legacy auth=true should resolve to required+high; got %#v", got)
	}
}

func TestReadAuthPolicyFromEntity_EmptyReturnsUnknown(t *testing.T) {
	got := readAuthPolicyFromEntity(nil)
	if got.Method != "unknown" || got.Confidence != "low" {
		t.Errorf("got %#v", got)
	}
}

func TestAuthPolicyToWire(t *testing.T) {
	in := engine.AuthPolicy{
		Required:   true,
		Method:     "config",
		Confidence: "medium",
		Roles:      []string{"ADMIN"},
		SourceChain: []engine.AuthSignal{{
			Kind: "config", Text: "x", File: "application.properties", Line: 7,
		}},
	}
	wire := authPolicyToWire(in)
	if wire == nil || !wire.Required || wire.Method != "config" || wire.Confidence != "medium" {
		t.Fatalf("wire = %#v", wire)
	}
	if len(wire.SourceChain) != 1 || wire.SourceChain[0].File != "application.properties" {
		t.Errorf("wire source chain = %#v", wire.SourceChain)
	}
}

func TestToneRank_AccentBeatsWarningBeatsMuted(t *testing.T) {
	if toneRank(authToneAccent) <= toneRank(authToneWarning) {
		t.Error("accent should outrank warning")
	}
	if toneRank(authToneWarning) <= toneRank(authToneMuted) {
		t.Error("warning should outrank muted")
	}
}
