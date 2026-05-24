// django_fk.go — late-binding resolver pass for Django string-FK references.
//
// Issue #2049: ForeignKey('Building', ...) (string reference instead of class
// reference) produced a Constraint/External placeholder entity instead of
// resolving to the real Building Model.
//
// Root cause: the extractor emits a REFERENCES stub of the form
//
//	scope:component:ref:python:<consumer_file>:ClassName
//
// The base resolver's lookupStructural fires the Python component fallback
// (lookupUniqueRealComponentByName) which succeeds only when ClassName is
// globally unique. In multi-app Django projects the same model name appears
// in multiple apps (e.g. two apps both define a "User" model), so the global
// lookup returns ambiguous and the stub is left unresolved — ending up as a
// SCOPE.External placeholder after external synthesis.
//
// This file adds ResolveDjangoStringFKRefs, a post-BuildIndex pass that:
//
//  1. Builds a quick per-app-dir component index over all SCOPE.Component
//     entities so we can do app-qualified lookups.
//  2. Walks every embedded REFERENCES edge that:
//     a. has Properties["django_rel"] set (ForeignKey/OneToOneField/M2M), and
//     b. whose ToID is still an unresolved scope:component:ref:python:* stub.
//  3. Tries to rewrite the stub using two strategies:
//     (a) byPackageComponent[pkgDirOf(consumerFile)][className] — resolves
//         cross-file same-app FKs where Building is in a sibling models file.
//     (b) Properties["django_fk_string"] = "app_label.ClassName" — derives
//         the app directory from the app_label segment and probes
//         byPackageComponent[app_label][className]. Handles cross-app FKs
//         like ForeignKey("auth.User", ...) pointing to django.contrib.auth.
//
// The pass runs AFTER BuildIndex so the byPackageComponent index is fully
// populated, and BEFORE ReferencesEmbeddedWithAllowlist so the disposition
// classifier sees the rewritten hex IDs as already-resolved.
package resolve

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// djangoFKRefPrefix is the structural-ref prefix emitted by
// buildDjangoModelClassRef in internal/extractors/python/django_relational.go.
// We gate the late-binding pass to only these stubs to avoid touching
// non-Django REFERENCES edges.
const djangoFKRefPrefix = "scope:component:ref:python:"

// djangoRelPropKey is the property key set on REFERENCES edges emitted by
// Django FK extraction (ForeignKey / OneToOneField / ManyToManyField).
const djangoRelPropKey = "django_rel"

// djangoFKStringPropKey is the property key set on REFERENCES edges emitted
// for string-literal FK forms (e.g. ForeignKey('Building', ...)). The value
// is the raw string before app_label stripping, e.g. "Building" or
// "auth.User". Set by the extractor (#2049).
const djangoFKStringPropKey = "django_fk_string"

// ResolveDjangoStringFKRefs is a late-binding pass that rewrites unresolved
// Django string-FK REFERENCES stubs to the real Model entity IDs.
//
// It is a method on Index because it needs the byPackageComponent index (for
// same-app cross-file lookup) which is an unexported field. The pass mutates
// the Relationships slice of affected EntityRecords in-place.
//
// Returns the number of stubs that were rewritten.
func (idx Index) ResolveDjangoStringFKRefs(records []types.EntityRecord) int {
	rewrites := 0
	for k := range records {
		rec := &records[k]
		for j := range rec.Relationships {
			r := &rec.Relationships[j]
			if r.Kind != "REFERENCES" {
				continue
			}
			if r.Properties == nil || r.Properties[djangoRelPropKey] == "" {
				continue
			}
			// Only try to rewrite if the ToID is still a structural-ref stub
			// (i.e. not yet a hex entity ID).
			if r.ToID == "" || isHexID(r.ToID) {
				continue
			}
			if !strings.HasPrefix(r.ToID, djangoFKRefPrefix) {
				continue
			}
			// Extract the consumer file path and class name from the stub.
			// Stub shape: scope:component:ref:python:<file>:<className>
			tail := r.ToID[len(djangoFKRefPrefix):]
			lastColon := strings.LastIndexByte(tail, ':')
			if lastColon <= 0 {
				continue
			}
			consumerFile := tail[:lastColon]
			className := tail[lastColon+1:]
			if consumerFile == "" || className == "" {
				continue
			}

			// Strategy 1: byPackageComponent[pkgDirOf(consumerFile)][className]
			// Resolves cross-file FKs within the same Django app (sibling files
			// in the same models directory).
			if id, ok := idx.lookupPackageComponent(pkgDirOf(consumerFile), className); ok && id != "" {
				r.ToID = id
				rewrites++
				continue
			}

			// Strategy 2: app_label-qualified lookup.
			// Uses Properties["django_fk_string"] = "app_label.ClassName" or
			// "ClassName" (bare). For dotted forms, derive the app directory
			// from the app_label segment and probe byPackageComponent.
			if fkStr := r.Properties[djangoFKStringPropKey]; fkStr != "" && fkStr != "self" {
				if dot := strings.IndexByte(fkStr, '.'); dot > 0 {
					// Dotted form: "app_label.ModelName" — the app directory is
					// app_label (the first segment). Django convention maps the
					// app_label directly to the top-level directory name.
					// "auth.User" → try byPackageComponent["auth"][className]
					// "myapp.models.User" → try first segment "myapp"
					appLabel := fkStr[:dot]
					if id, ok := idx.lookupPackageComponent(appLabel, className); ok && id != "" {
						r.ToID = id
						rewrites++
						continue
					}
					// Also try the full dotted prefix as a directory path in case
					// the app uses a nested models package:
					// "myapp.models.User" → try "myapp/models"
					// Replace dots with slashes for the directory form.
					moduleDir := strings.ReplaceAll(fkStr[:strings.LastIndexByte(fkStr, '.')], ".", "/")
					if moduleDir != appLabel {
						if id, ok := idx.lookupPackageComponent(moduleDir, className); ok && id != "" {
							r.ToID = id
							rewrites++
							continue
						}
					}
				}
				// Bare form: "ClassName" — fall through to the global unique
				// lookup. This is the existing behavior when
				// lookupUniqueRealComponentByName succeeds (globally unique
				// model name); we don't duplicate that here since
				// ReferencesEmbeddedWithAllowlist will handle it via
				// lookupStructural → lookupUniqueRealComponentByName.
			}
		}
	}
	return rewrites
}
