// v2_paths_auth.go — auth_policy surfacing for the v2 paths API.
//
// Phase 1 (#1942) wired the resolver into the Java extractor; this file is
// the bridge that turns the persisted `auth_policy` JSON property on
// http_endpoint entities into the v2 wire shape consumed by the dashboard
// (chip label + tone + expandable source chain).
//
// Future phases (#1942 Phase 2-4: Python / NestJS / Go) emit the SAME
// `auth_policy` property shape so this dashboard plumbing needs zero
// changes per-language.
package dashboard

import (
	"github.com/cajasmota/archigraph/internal/engine"
)

// authChipTone is the visual tone the frontend should use for an auth chip.
const (
	authToneAccent  = "accent"
	authToneMuted   = "muted"
	authToneWarning = "warning"
)

// resolveAuthChip projects an engine.AuthPolicy into the dashboard chip
// label + tone pair. Ordering of branches encodes the precedence specified
// in #1942 Phase 1:
//
//   - high confidence + roles non-empty → `[Roles: ADMIN]` (accent)
//   - high confidence + required=false → `[Public]` (muted)
//   - high confidence + required=true → `[Auth required]` (accent)
//   - method=framework_default + required=true → `[Auth: default]` (warning)
//   - method=config (medium) → `[Auth: probable]` (warning)
//   - method=unknown / low confidence → `[Auth: unknown]` (muted)
func resolveAuthChip(p engine.AuthPolicy) (label, tone string) {
	if p.Confidence == "high" {
		if len(p.Roles) > 0 {
			return "[Roles: " + joinRoles(p.Roles) + "]", authToneAccent
		}
		if !p.Required {
			return "[Public]", authToneMuted
		}
		return "[Auth required]", authToneAccent
	}
	if p.Method == "framework_default" && p.Required {
		return "[Auth: default]", authToneWarning
	}
	if p.Method == "config" && p.Confidence == "medium" {
		return "[Auth: probable]", authToneWarning
	}
	return "[Auth: unknown]", authToneMuted
}

// joinRoles joins resolved roles for the chip label. Up to two roles are
// surfaced inline; longer lists are abbreviated with a "+N" suffix to keep
// the chip narrow.
func joinRoles(roles []string) string {
	switch len(roles) {
	case 0:
		return ""
	case 1:
		return roles[0]
	case 2:
		return roles[0] + ", " + roles[1]
	default:
		// Show first two roles, summarise the rest.
		extra := len(roles) - 2
		return roles[0] + ", " + roles[1] + ", +" + authChipItoa(extra)
	}
}

// authChipItoa is a zero-alloc helper for small non-negative integers (chip labels
// rarely exceed two digits). Avoids pulling strconv into the chip hot path.
func authChipItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// authPolicyToWire converts an engine.AuthPolicy into the v2 wire shape.
func authPolicyToWire(p engine.AuthPolicy) *v2AuthPolicy {
	out := &v2AuthPolicy{
		Required:   p.Required,
		Method:     p.Method,
		Roles:      append([]string(nil), p.Roles...),
		Scopes:     append([]string(nil), p.Scopes...),
		Confidence: p.Confidence,
	}
	if len(p.SourceChain) > 0 {
		out.SourceChain = make([]v2AuthSignal, 0, len(p.SourceChain))
		for _, s := range p.SourceChain {
			out.SourceChain = append(out.SourceChain, v2AuthSignal{
				Kind:     s.Kind,
				EntityID: s.EntityID,
				Text:     s.Text,
				File:     s.File,
				Line:     s.Line,
			})
		}
	}
	return out
}

// authPolicyStronger returns true when the supplied policy outranks the
// already-chosen chip tone for a route. Accent (high-confidence explicit)
// beats warning (config/default), which beats muted (unknown). Used by the
// left-rail aggregation to keep the strongest signal visible when multiple
// handlers share a path.
func authPolicyStronger(p engine.AuthPolicy, currentTone string) bool {
	newTone := toneFromPolicy(p)
	return toneRank(newTone) > toneRank(currentTone)
}

func toneFromPolicy(p engine.AuthPolicy) string {
	_, tone := resolveAuthChip(p)
	return tone
}

func toneRank(tone string) int {
	switch tone {
	case authToneAccent:
		return 3
	case authToneWarning:
		return 2
	case authToneMuted:
		return 1
	}
	return 0
}

// readAuthPolicyFromEntity decodes the `auth_policy` JSON property emitted by
// the indexer. When the property is missing (e.g. Phase 0 endpoints, or
// non-Java endpoints until Phases 2-4 ship) it falls back to a synthesized
// "unknown" policy that matches the Phase 0 muted-chip behaviour from #1950.
func readAuthPolicyFromEntity(props map[string]string) engine.AuthPolicy {
	if props == nil {
		return engine.AuthPolicy{Method: "unknown", Confidence: "low"}
	}
	if raw := props["auth_policy"]; raw != "" {
		return engine.DecodeAuthPolicy(raw)
	}
	// Pre-Phase-1 fallback: if older `auth=true` / `auth_scheme=Bearer` flags
	// were emitted by a non-Java extractor, treat them as a high-confidence
	// "auth required" without a structured source chain.
	if props["auth"] == "true" || props["auth_scheme"] != "" {
		return engine.AuthPolicy{
			Required:   true,
			Method:     "annotation",
			Confidence: "high",
		}
	}
	return engine.AuthPolicy{Method: "unknown", Confidence: "low"}
}
