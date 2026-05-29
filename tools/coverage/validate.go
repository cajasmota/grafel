package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// frameworkSpecificKeyPattern restricts framework-specific capability
// keys to the same snake_case identifier shape used by canonical keys:
// lowercase letters, digits, and underscores. It deliberately rejects
// spaces and hyphens so prettyKey produces clean section headings.
var frameworkSpecificKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// staleDays is the threshold beyond which a capability cell triggers a
// stale-verification warning.
const staleDays = 90

// completenessGateIsError controls the severity of the grouped-
// completeness check (validateGroupedCompleteness): a lane key declared
// by a record's subcategory group taxonomy but absent from the record's
// cells. Now true (#2971, Foundation Wave 2 final step): the registry was
// fully backfilled in #2970, so any missing lane cell is a hard ERROR that
// fails CI. Use `go run ./tools/coverage backfill` to seed new cells before
// adding a record to a grouped subcategory.
const completenessGateIsError = true

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
	validateUniversalCoreConsistency(res)

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
		} else if !categoryIsKnown(rec.Category) {
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
		if rec.HasFrameworkSpecific() {
			validateFrameworkSpecific(res, prefix, rec, repoRoot)
		}
	}

	sort.Strings(res.Errors)
	sort.Strings(res.Warnings)
	return res
}

// validateFlatRecord runs the legacy capability-key + status + cite +
// freshness checks against a record using the flat capability shape.
//
// When a record carries a subcategory whose taxonomy declares groups,
// the flat shape is forbidden — those records MUST use the nested
// shape so the by-language pivot tables can render group-digest
// columns (#2758). Records with no subcategory, or with a subcategory
// that has no group taxonomy (e.g. static_site), are unaffected.
func validateFlatRecord(res *ValidationResult, prefix string, rec Record, repoRoot string) {
	if rec.Subcategory != "" && validSubcategory(rec.Category, rec.Subcategory) && len(knownGroupNames(rec.Subcategory)) > 0 && len(rec.Capabilities) > 0 {
		res.Errors = append(res.Errors, fmt.Sprintf("%s: flat capability shape forbidden for subcategory %q (has group taxonomy; use nested capabilities[group][key])", prefix, rec.Subcategory))
	}
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
		if gname == OtherCapabilitiesColumn {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: group name %q is reserved (the synthetic merged pivot column)", gPrefix, OtherCapabilitiesColumn))
			continue
		}
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
	validateGroupedCompleteness(res, prefix, rec)
}

// validateGroupedCompleteness reports lane cells declared by the record's
// subcategory group taxonomy but absent from the record. It is the
// validate-time mirror of the `backfill` subcommand: every key the
// taxonomy promises should have an explicit cell. AllCapabilities (lane
// cells only — framework_specific is deliberately excluded) is the
// presence set. Messages are appended in SORTED order so output is
// deterministic regardless of map iteration. Subcategories without a
// group taxonomy are skipped (nothing to be complete against).
//
// Severity is governed by completenessGateIsError: while false these are
// warnings (advisory, never fail CI). validateRegistry sorts Errors and
// Warnings globally afterward, so local ordering here is for readability.
func validateGroupedCompleteness(res *ValidationResult, prefix string, rec Record) {
	if rec.Subcategory == "" || !validSubcategory(rec.Category, rec.Subcategory) {
		return
	}
	groups := groupsForSubcategory(rec.Subcategory)
	if len(groups) == 0 {
		return
	}
	present := rec.AllCapabilities()
	var msgs []string
	seen := map[string]struct{}{}
	for _, g := range groups {
		for _, key := range g.Keys {
			if _, ok := present[key]; ok {
				continue
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			owner := groupForCapability(rec.Subcategory, key)
			if owner == "" {
				owner = uncategorizedGroup
			}
			msgs = append(msgs, fmt.Sprintf("%s: lane key %q (group %q) declared by subcategory %q taxonomy but absent from record", prefix, key, owner, rec.Subcategory))
		}
	}
	sort.Strings(msgs)
	if completenessGateIsError {
		res.Errors = append(res.Errors, msgs...)
	} else {
		res.Warnings = append(res.Warnings, msgs...)
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

// validateFrameworkSpecific enforces the #2739 rules for the
// framework_specific field:
//
//   - Group names are non-empty (whitespace-only rejected).
//   - Capability keys match the canonical snake_case shape so
//     prettyKey renders them sensibly.
//   - Capability keys are unique within the record's framework_specific
//     field (no two groups may declare the same key).
//   - Capability keys MUST NOT collide with any key in the record's
//     canonical capabilities (flat or grouped) — that's the headline
//     constraint of the three-tier shape.
//
// Group names are otherwise free-form. As an advisory hint we emit a
// warning when a group name doesn't reference the record's framework
// label (e.g. "Angular Internals" passes; "Internals" warns).
func validateFrameworkSpecific(res *ValidationResult, prefix string, rec Record, repoRoot string) {
	canonical := collectCanonicalKeys(rec)
	seenKeys := map[string]string{}
	groupNames := make([]string, 0, len(rec.FrameworkSpecific))
	for g := range rec.FrameworkSpecific {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)
	for _, gname := range groupNames {
		gPrefix := fmt.Sprintf("%s.framework_specific[%s]", prefix, gname)
		if strings.TrimSpace(gname) == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: group name is empty or whitespace-only", gPrefix))
			continue
		}
		if gname == OtherCapabilitiesColumn {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: group name %q is reserved (the synthetic merged pivot column)", gPrefix, OtherCapabilitiesColumn))
			continue
		}
		if rec.Label != "" && !strings.Contains(strings.ToLower(gname), strings.ToLower(firstLabelToken(rec.Label))) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: group name %q does not reference framework label %q", gPrefix, gname, rec.Label))
		}
		caps := rec.FrameworkSpecific[gname]
		keys := sortedCapKeys(caps)
		for _, k := range keys {
			capPrefix := fmt.Sprintf("%s.%s", gPrefix, k)
			if !frameworkSpecificKeyPattern.MatchString(k) {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: capability key %q must match %s", capPrefix, k, frameworkSpecificKeyPattern.String()))
				continue
			}
			if prev, dup := seenKeys[k]; dup {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: capability key %q already declared under framework_specific group %q", capPrefix, k, prev))
				continue
			}
			if _, clash := canonical[k]; clash {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: capability key %q also appears in canonical capabilities (keys must be unique within the record)", capPrefix, k))
				continue
			}
			seenKeys[k] = gname
			validateCapabilityCell(res, capPrefix, caps[k], repoRoot)
		}
	}
}

// validateUniversalCoreConsistency surfaces the #2940 dictionary
// invariant at validate time: a subcategory group whose name
// case-insensitively matches a universal_core lane MUST be spelled
// identically, and no real group may be named OtherCapabilitiesColumn.
// The dictionary loader (capability_dictionary.go) already rejects a
// divergent dictionary outright, so this is a defensive second gate that
// keeps the validate subcommand authoritative on its own.
func validateUniversalCoreConsistency(res *ValidationResult) {
	d := dict()
	core := d.UniversalCoreOrder()
	if len(core) == 0 {
		return
	}
	canonByLower := map[string]string{}
	for _, name := range core {
		if name == OtherCapabilitiesColumn {
			res.Errors = append(res.Errors, fmt.Sprintf("universal_core lane %q collides with the reserved merged-pivot column name", OtherCapabilitiesColumn))
		}
		canonByLower[strings.ToLower(name)] = name
	}
	for _, cat := range knownCategories() {
		for _, sub := range knownSubcategories(cat) {
			for _, g := range knownGroupNames(sub) {
				if g == OtherCapabilitiesColumn {
					res.Errors = append(res.Errors, fmt.Sprintf("subcategory %q declares a group named %q, which is reserved for the merged pivot column", sub, OtherCapabilitiesColumn))
				}
				if d.IsUniversalCore(g) {
					continue
				}
				if canon, ok := canonByLower[strings.ToLower(g)]; ok {
					res.Errors = append(res.Errors, fmt.Sprintf("subcategory %q group %q must be spelled %q to match universal_core", sub, g, canon))
				}
			}
		}
	}
}

// collectCanonicalKeys returns the set of capability keys carried by
// the record's canonical capabilities (flat map or grouped buckets).
// Used by framework_specific validation to detect key collisions.
func collectCanonicalKeys(rec Record) map[string]struct{} {
	out := map[string]struct{}{}
	for k := range rec.Capabilities {
		out[k] = struct{}{}
	}
	for _, g := range rec.Groups {
		for k := range g {
			out[k] = struct{}{}
		}
	}
	return out
}

// firstLabelToken returns the first whitespace-delimited token of label
// lowercased, used as the framework-name hint for the advisory warning
// on framework_specific group names. Returns label unchanged when it
// contains no whitespace.
func firstLabelToken(label string) string {
	fields := strings.Fields(label)
	if len(fields) == 0 {
		return label
	}
	return fields[0]
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
