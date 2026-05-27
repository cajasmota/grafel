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

		// Capabilities: sort keys for deterministic error ordering.
		keys := sortedCapKeys(rec.Capabilities)
		for _, k := range keys {
			cap := rec.Capabilities[k]
			capPrefix := fmt.Sprintf("%s.capabilities[%s]", prefix, k)
			// When a record opts in to a subcategory, accept either the
			// subcategory's keys or the category-wide keys (subcategories
			// extend, they do not replace). Without a subcategory, fall
			// back to the legacy category-only allow-list.
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
	}

	sort.Strings(res.Errors)
	sort.Strings(res.Warnings)
	return res
}
