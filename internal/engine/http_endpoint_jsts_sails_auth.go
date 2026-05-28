// Corpus-wide Sails policy → endpoint auth attribution (#2897).
//
// Sails does not gate routes with route-level middleware or guards; it maps
// controllers/actions to *policies* in `config/policies.js`:
//
//	module.exports.policies = {
//	  '*': 'isLoggedIn',                 // global default
//	  AuthController: {
//	    login: true,                     // action-level override (public)
//	    logout: 'isLoggedIn',
//	  },
//	  DashboardController: 'isLoggedIn', // controller-level catch-all
//	}
//
// The endpoints themselves are synthesised from a *different* file,
// `config/routes.js` (synthesizeSails), so the per-file pass in
// http_endpoint_jsts_auth.go (#2852) can never see both halves at once — it
// records only the global default posture as a framework_specific cell and
// leaves the standard auth_coverage cell at `partial`.
//
// This corpus-wide post-pass closes that gap, mirroring the cross-file
// Java/Quarkus JavaAuthContext approach (java_auth_policy.go) and the
// ApplyResponseShapesCorpus pass (response_shape_corpus.go): it parses every
// config/policies.js in the repo, then resolves each Sails endpoint's
// controller/action against the policy map with honest override precedence and
// stamps a `config`-method, medium-confidence auth_policy onto the endpoint
// entity.
//
// Precedence (highest first):
//  1. action-level override  — `AuthController: { login: true }`
//  2. controller-level value — `DashboardController: 'isLoggedIn'`
//  3. global `'*'` default
//
// A `true` value at any level means the action is explicitly public; a named
// policy (or `false` blanket-deny) means protected.
//
// The pass is additive: it only sets auth_* properties on Sails
// http_endpoint_definition entities that don't already carry a higher-signal
// posture, never adds/removes entities, and is a no-op on repos with no Sails
// policy file.
//
// Refs #2897 (follow-up to #2852).
package engine

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// SailsAuthStats reports counters from the corpus-wide Sails policy pass.
type SailsAuthStats struct {
	// PolicyFiles is the number of config/policies.js files parsed.
	PolicyFiles int
	// Endpoints is the number of Sails http_endpoint_definition entities seen.
	Endpoints int
	// Attributed is the number of endpoints stamped with a resolved auth_policy.
	Attributed int
}

// ApplySailsAuthPolicy runs the corpus-wide Sails policy attribution pass over
// the merged record set. It parses every config/policies.{js,ts} file found in
// `paths` (via the content reader) into a SailsPolicyMap, then for each Sails
// endpoint resolves its controller/action policy chain and stamps the
// auth_policy contract.
//
// `entities` is mutated in place (Properties only). `paths` is the
// repo-relative file set still live at this point of the pipeline (config
// files often produce no extracted entities, so we discover policy files from
// the path list rather than entity SourceFiles). `reader` returns file content
// by repo-relative path. When no policy file is found, the pass is a no-op.
func ApplySailsAuthPolicy(entities []types.EntityRecord, paths []string, reader CorpusFileReader) SailsAuthStats {
	var stats SailsAuthStats
	if reader == nil || len(entities) == 0 || len(paths) == 0 {
		return stats
	}

	// Parse every Sails policy file in the corpus, keyed by project dir so a
	// routes.js endpoint and its sibling policies.js resolve to the same map.
	policyMaps := map[string]SailsPolicyMap{}
	for _, sf := range paths {
		if !sailsPoliciesFile(sf) {
			continue
		}
		content := reader(sf)
		if len(content) == 0 {
			continue
		}
		pm, ok := ParseSailsPolicies(string(content), sf)
		if !ok {
			continue
		}
		stats.PolicyFiles++
		policyMaps[sailsProjectDir(sf)] = pm
	}
	if len(policyMaps) == 0 {
		return stats
	}

	for i := range entities {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.Properties == nil {
			continue
		}
		if e.Properties["framework"] != "sails" {
			continue
		}
		stats.Endpoints++
		// Don't clobber an already-resolved high/medium posture from a prior
		// pass (defensive — the per-file pass never resolves Sails today).
		if m := e.Properties["auth_method"]; m != "" && m != "unknown" {
			continue
		}

		// Find the policy map for this endpoint's project. Sails endpoints carry
		// the routes.js path as SourceFile; match the policy file that shares
		// the same project dir, falling back to the sole map when there is one.
		pm, ok := policyMaps[sailsProjectDir(e.SourceFile)]
		if !ok {
			if len(policyMaps) == 1 {
				for _, only := range policyMaps {
					pm, ok = only, true
				}
			}
		}
		if !ok {
			continue
		}

		controller := e.Properties["handler_file"] // e.g. "DashboardController"
		action := sailsActionFromHandler(e.Properties["source_handler"])
		policy, resolved := resolveSailsEndpointAuth(pm, controller, action)
		if !resolved {
			continue
		}
		stampAuthPolicy(e.Properties, policy)
		stats.Attributed++
	}
	return stats
}

// sailsActionFromHandler extracts the action name from a Sails endpoint's
// source_handler property. synthesizeSails stamps it as "Controller:<method>"
// (refKind "Controller", refName the bare action), so we take the segment
// after the colon.
func sailsActionFromHandler(ref string) string {
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		return strings.TrimSpace(ref[i+1:])
	}
	return strings.TrimSpace(ref)
}

// sailsProjectDir returns the directory two levels above a config/policies.js
// or config/routes.js path — i.e. the Sails project root — so a routes.js
// endpoint and a policies.js map in the same project resolve to the same key.
// For `api/cfg/config/policies.js` → `api/cfg`; for the common
// `config/policies.js` → "" (repo root).
func sailsProjectDir(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	// Strip the trailing `config/<file>` segment.
	if i := strings.LastIndex(p, "config/"); i >= 0 {
		return strings.TrimSuffix(p[:i], "/")
	}
	return ""
}

// resolveSailsEndpointAuth resolves the auth posture for a single Sails
// endpoint by applying the action > controller > global precedence. Returns
// the resolved AuthPolicy and true when a posture could be determined.
func resolveSailsEndpointAuth(pm SailsPolicyMap, controller, action string) (AuthPolicy, bool) {
	// 1. Action-level / controller-level entry.
	if controller != "" {
		if cp, ok := pm.Controllers[controller]; ok {
			// 1a. action-level override.
			if action != "" {
				if val, ok := cp.Actions[action]; ok {
					return sailsPolicyFromValue(pm.File, controller, action, "action", val), true
				}
			}
			// 1b. controller-level catch-all value.
			if cp.HasControllerPolicy {
				return sailsPolicyFromValue(pm.File, controller, action, "controller", cp.ControllerPolicy), true
			}
			// 1c. object block exists but no matching action and no catch-all →
			// fall through to the global default below.
		}
	}

	// 2. Global '*' default.
	if pm.HasDefault {
		return sailsPolicyFromValue(pm.File, controller, action, "global", pm.DefaultPolicy), true
	}

	return AuthPolicy{}, false
}

// sailsPolicyFromValue builds an AuthPolicy from a raw Sails policy value and
// the level it was resolved at (for the evidence text). `true` is public; any
// named policy (or blanket-deny `false`) is protected. Confidence is medium —
// the binding is config-driven and cross-file rather than a direct route-level
// guard.
func sailsPolicyFromValue(file, controller, action, level, val string) AuthPolicy {
	protected := sailsPolicyValueProtected(val)
	desc := sailsLevelDesc(level, controller, action)
	text := desc + " policy " + val
	if !protected {
		text = desc + " public (policy: true)"
	}
	return AuthPolicy{
		Required:   protected,
		Method:     "config",
		Confidence: "medium",
		SourceChain: []AuthSignal{{
			Kind: "config",
			Text: text,
			File: file,
		}},
	}
}

// sailsLevelDesc renders a short human description of the policy resolution
// level for the evidence source chain.
func sailsLevelDesc(level, controller, action string) string {
	switch level {
	case "action":
		return "config/policies.js " + controller + "." + action
	case "controller":
		return "config/policies.js " + controller
	default:
		return "config/policies.js '*' default"
	}
}
