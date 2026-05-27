package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// staleDays is the threshold beyond which a capability cell triggers a
// stale-verification warning.
const staleDays = 90

// ValidationResult collects errors (block) and warnings (advisory).
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// HasErrors reports whether validation failed.
func (r *ValidationResult) HasErrors() bool { return len(r.Errors) > 0 }

// validateRegistry runs schema, cite-exists, duplicate-id, capability-key,
// stale-verification, and missing-issue checks against reg. repoRoot is
// used to resolve cite paths.
func validateRegistry(reg *Registry, repoRoot string) *ValidationResult {
	res := &ValidationResult{}
	if reg.SchemaVersion != SchemaVersion {
		res.Errors = append(res.Errors, fmt.Sprintf("schema_version %d unsupported (want %d)", reg.SchemaVersion, SchemaVersion))
	}

	seen := map[string]int{}
	for i, rec := range reg.Records {
		prefix := fmt.Sprintf("records[%d] (%s)", i, rec.ID)
		if err := validateID(rec.ID); err != nil {
			res.Errors = append(res.Errors, prefix+": "+err.Error())
		}
		if prev, ok := seen[rec.ID]; ok {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: duplicate id (also at records[%d])", prefix, prev))
		}
		seen[rec.ID] = i

		if rec.Category == "" {
			res.Errors = append(res.Errors, prefix+": category is empty")
		} else if _, ok := categoryCapabilities[rec.Category]; !ok {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: unknown category %q (known: %v)", prefix, rec.Category, knownCategories()))
		}
		if rec.Subcategory != "" {
			if !validSubcategory(rec.Category, rec.Subcategory) {
				known := knownSubcategories(rec.Category)
				res.Errors = append(res.Errors, fmt.Sprintf("%s: unknown subcategory %q for category %q (known: %v)", prefix, rec.Subcategory, rec.Category, known))
			}
		}
		if rec.Language == "" {
			res.Errors = append(res.Errors, prefix+": language is empty")
		}
		if rec.Label == "" {
			res.Errors = append(res.Errors, prefix+": label is empty")
		}

		// A record must not carry both shapes — the on-disk loader
		// guarantees one or the other but defensive validation guards
		// against in-memory mutation paths populating both.
		if rec.IsGrouped() && len(rec.Capabilities) > 0 {
			res.Errors = append(res.Errors, prefix+": record carries both flat and grouped capabilities")
		}

		if rec.IsGrouped() {
			validateGroupedRecord(res, prefix, rec, repoRoot)
		} else {
			validateFlatRecord(res, prefix, rec, repoRoot)
		}
	}

	sort.Strings(res.Errors)
	sort.Strings(res.Warnings)
	return res
}

// validateFlatRecord runs the legacy capability-key + status + cite +
// freshness checks against a record using the flat capability shape.
func validateFlatRecord(res *ValidationResult, prefix string, rec Record, repoRoot string) {
	keys := sortedCapKeys(rec.Capabilities)
	for _, k := range keys {
		cap := rec.Capabilities[k]
		capPrefix := fmt.Sprintf("%s.capabilities[%s]", prefix, k)
		validKey := false
		if rec.Subcategory != "" && validSubcategory(rec.Category, rec.Subcategory) {
			validKey = validCapabilityKeyForSubcategory(rec.Category, rec.Subcategory, k)
		} else {
			validKey = validCapabilityKey(rec.Category, k)
		}
		if !validKey {
			if rec.Subcategory != "" {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid capability key for category %q subcategory %q", capPrefix, rec.Category, rec.Subcategory))
			} else {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid capability key for category %q", capPrefix, rec.Category))
			}
		}
		validateCapabilityCell(res, capPrefix, cap, repoRoot)
	}
}

// validateGroupedRecord enforces the #2737 rules for the nested shape:
// group names must be canonical for the record's subcategory; each
// capability key appears in exactly one group within the record; each
// capability key must be a member of its declared group's allow-list.
func validateGroupedRecord(res *ValidationResult, prefix string, rec Record, repoRoot string) {
	if rec.Subcategory == "" {
		res.Errors = append(res.Errors, prefix+": grouped capabilities require a subcategory")
		return
	}
	if !validSubcategory(rec.Category, rec.Subcategory) {
		// Already reported above; skip group-level checks to avoid a
		// cascade of duplicates.
		return
	}
	known := knownGroupNames(rec.Subcategory)
	if len(known) == 0 {
		res.Errors = append(res.Errors, fmt.Sprintf("%s: subcategory %q has no declared group taxonomy", prefix, rec.Subcategory))
		return
	}
	// Track each key's first occurrence so duplicates across groups are
	// reported with both locations.
	keyOwner := map[string]string{}
	groupNames := make([]string, 0, len(rec.Groups))
	for g := range rec.Groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)
	for _, gname := range groupNames {
		gPrefix := fmt.Sprintf("%s.capabilities[%s]", prefix, gname)
		if !validGroupName(rec.Subcategory, gname) {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: unknown group %q for subcategory %q (known: %v)", gPrefix, gname, rec.Subcategory, known))
			continue
		}
		caps := rec.Groups[gname]
		keys := sortedCapKeys(caps)
		for _, k := range keys {
			capPrefix := fmt.Sprintf("%s.%s", gPrefix, k)
			if prev, dup := keyOwner[k]; dup {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: capability key %q already declared under group %q", capPrefix, k, prev))
				continue
			}
			keyOwner[k] = gname
			if !validCapabilityKeyForSubcategory(rec.Category, rec.Subcategory, k) {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid capability key for category %q subcategory %q", capPrefix, rec.Category, rec.Subcategory))
			}
			// Capability must belong to this group's allow-list. The
			// synthetic Uncategorized group accepts anything (it exists
			// precisely to park keys outside the canonical taxonomy).
			if gname != uncategorizedGroup {
				allowed := groupAllowedKeys(rec.Subcategory, gname)
				if !inStringSlice(k, allowed) {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: capability %q does not belong to group %q (allowed: %v)", capPrefix, k, gname, allowed))
				}
			}
			validateCapabilityCell(res, capPrefix, caps[k], repoRoot)
		}
	}
}

// validateCapabilityCell runs the per-cell checks (status enum, cite
// paths, ISO date, freshness, tracking issue). Shared between the flat
// and grouped paths.
func validateCapabilityCell(res *ValidationResult, capPrefix string, cap Capability, repoRoot string) {
	if _, ok := validStatuses[cap.Status]; !ok {
		res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid status %q", capPrefix, cap.Status))
	}
	for _, cite := range cap.Cites {
		full := filepath.Join(repoRoot, cite)
		if _, err := os.Stat(full); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: cite %q not found on disk", capPrefix, cite))
		}
	}
	if cap.VerifiedAt != "" {
		t, err := time.Parse("2006-01-02", cap.VerifiedAt)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: verified_at %q not a valid ISO date", capPrefix, cap.VerifiedAt))
		} else if time.Since(t) > staleDays*24*time.Hour {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: verified_at %s is older than %d days", capPrefix, cap.VerifiedAt, staleDays))
		}
	}
	if (cap.Status == StatusMissing || cap.Status == StatusPartial) && cap.Issue == "" {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s capability has no tracking issue", capPrefix, cap.Status))
	}
}

// inStringSlice reports whether needle is present in haystack.
func inStringSlice(needle string, haystack []string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
