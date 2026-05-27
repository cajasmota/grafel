package main

// Mapping-integrity checks for capability-map.yaml.
//
// Layered on top of the existing registry validation, these checks
// answer four questions:
//
//  1. Does every record ID referenced in the mapping exist in the
//     registry? (error — stale mapping entry)
//  2. Does every cited symbol file exist on disk? (error — drift)
//  3. Does every cited function exist in its declared file? (error —
//     rename/removal drift)
//  4. Does every cited test file exist? (error)
//  5. Is every capability cell with status=full|partial covered by a
//     mapping entry? (warning — gentle nudge for unmapped surface)

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// MappingValidationResult mirrors ValidationResult but is scoped to the
// capability-map checks so callers can fold the counts into a single
// validate summary line.
type MappingValidationResult struct {
	Errors   []string
	Warnings []string
	// Checked counts surface in the validate summary line so users can
	// confirm the mapping was actually exercised (vs silently skipped).
	RecordsChecked   int
	SymbolsChecked   int
	FunctionsChecked int
	TestsChecked     int
}

// HasErrors reports whether mapping validation failed.
func (r *MappingValidationResult) HasErrors() bool { return len(r.Errors) > 0 }

// validateCapabilityMap cross-references cm against reg and the
// filesystem rooted at repoRoot. Pass cm==nil when no mapping file is
// present; the function returns an empty result in that case.
func validateCapabilityMap(cm *CapabilityMap, reg *Registry, repoRoot string) *MappingValidationResult {
	res := &MappingValidationResult{}
	if cm == nil {
		return res
	}

	regIndex := indexRegistry(reg)
	for _, recID := range cm.SortedRecordIDs() {
		res.RecordsChecked++
		mapRec := cm.Records[recID]
		regRec, ok := regIndex[recID]
		if !ok {
			res.Errors = append(res.Errors, fmt.Sprintf("capability-map: records[%s]: no matching registry record", recID))
			continue
		}
		validateMappingRecord(res, recID, mapRec, regRec, repoRoot)
	}

	// Mapping-coverage nudge: every registry cell with status full or
	// partial should have a mapping entry. Missing entries are warnings,
	// not errors, since adding mapping is incremental.
	for _, rec := range reg.Records {
		mapRec, mapped := cm.Records[rec.ID]
		for group, caps := range allGroupedCells(rec) {
			for _, key := range sortedCapKeys(caps) {
				cell := caps[key]
				if cell.Status != StatusFull && cell.Status != StatusPartial {
					continue
				}
				if !mapped {
					res.Warnings = append(res.Warnings, fmt.Sprintf("capability-map: %s/%s: %s capability has no mapping entry", rec.ID, displayKey(group, key), cell.Status))
					continue
				}
				if _, ok := mapRec.Lookup(group, key); !ok {
					res.Warnings = append(res.Warnings, fmt.Sprintf("capability-map: %s/%s: %s capability has no mapping entry", rec.ID, displayKey(group, key), cell.Status))
				}
			}
		}
	}

	sort.Strings(res.Errors)
	sort.Strings(res.Warnings)
	return res
}

// validateMappingRecord verifies one mapping record's symbols and tests.
// Reports shape mismatches (mapping says grouped but registry says flat
// or vice versa) and per-entry file/function existence.
func validateMappingRecord(res *MappingValidationResult, recID string, mapRec MapRecord, regRec Record, repoRoot string) {
	if mapRec.IsGrouped() && !regRec.IsGrouped() {
		res.Errors = append(res.Errors, fmt.Sprintf("capability-map: records[%s]: grouped mapping shape does not match flat registry record", recID))
		return
	}
	if !mapRec.IsGrouped() && regRec.IsGrouped() {
		res.Errors = append(res.Errors, fmt.Sprintf("capability-map: records[%s]: flat mapping shape does not match grouped registry record", recID))
		return
	}

	if mapRec.IsGrouped() {
		for _, gname := range mapRec.SortedGroupNames() {
			if _, ok := regRec.Groups[gname]; !ok {
				res.Errors = append(res.Errors, fmt.Sprintf("capability-map: records[%s].%s: group not declared on registry record", recID, gname))
				continue
			}
			for _, key := range mapRec.SortedKeysInGroup(gname) {
				if _, ok := regRec.Groups[gname][key]; !ok {
					res.Errors = append(res.Errors, fmt.Sprintf("capability-map: records[%s].%s.%s: capability not declared on registry record", recID, gname, key))
					continue
				}
				entry := mapRec.Groups[gname][key]
				prefix := fmt.Sprintf("records[%s].%s.%s", recID, gname, key)
				validateMapEntry(res, prefix, entry, repoRoot)
			}
		}
		return
	}

	for _, key := range mapRec.SortedFlatKeys() {
		if _, ok := regRec.Capabilities[key]; !ok {
			res.Errors = append(res.Errors, fmt.Sprintf("capability-map: records[%s].%s: capability not declared on registry record", recID, key))
			continue
		}
		entry := mapRec.Capabilities[key]
		prefix := fmt.Sprintf("records[%s].%s", recID, key)
		validateMapEntry(res, prefix, entry, repoRoot)
	}
}

// validateMapEntry checks each symbol file + function and each test
// file. Function existence is determined by scanning the file for
// `func .*<name>(` or `type <name>` declarations — sufficient for Go
// source where every mapping target lives today and trivial to extend
// to other languages later if needed.
func validateMapEntry(res *MappingValidationResult, prefix string, entry MapEntry, repoRoot string) {
	for _, sym := range entry.Symbols {
		res.SymbolsChecked++
		full := filepath.Join(repoRoot, sym.File)
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			res.Errors = append(res.Errors, fmt.Sprintf("capability-map: %s: symbol file %q not found on disk", prefix, sym.File))
			continue
		}
		if len(sym.Functions) == 0 {
			continue
		}
		decls, err := scanDeclarations(full)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("capability-map: %s: read %q: %v", prefix, sym.File, err))
			continue
		}
		for _, fn := range sym.Functions {
			res.FunctionsChecked++
			if !decls[fn] {
				res.Errors = append(res.Errors, fmt.Sprintf("capability-map: %s: function %q not found in %s", prefix, fn, sym.File))
			}
		}
	}
	for _, t := range entry.Tests {
		res.TestsChecked++
		full := filepath.Join(repoRoot, t.File)
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			res.Errors = append(res.Errors, fmt.Sprintf("capability-map: %s: test file %q not found on disk", prefix, t.File))
		}
	}
}

// indexRegistry returns a record-id → Record map for O(1) lookup during
// mapping cross-referencing.
func indexRegistry(reg *Registry) map[string]Record {
	out := make(map[string]Record, len(reg.Records))
	for _, rec := range reg.Records {
		out[rec.ID] = rec
	}
	return out
}

// allGroupedCells returns the record's cells as a group → key → cell
// map. Flat records surface under a single empty-string group so the
// mapping-coverage walk can use one uniform shape.
func allGroupedCells(rec Record) map[string]map[string]Capability {
	if rec.IsGrouped() {
		return rec.Groups
	}
	return map[string]map[string]Capability{"": rec.Capabilities}
}

// displayKey formats a (group, capability) coordinate for human-facing
// error messages. Flat records render as `key`; grouped records render
// as `group/key`.
func displayKey(group, key string) string {
	if group == "" {
		return key
	}
	return group + "/" + key
}

// declRegexp matches Go function and method declarations and type
// declarations. We anchor on the start of a line so commented-out or
// inline references do not satisfy the existence check.
//
//	func Name(            -> Name
//	func (r *T) Name(     -> Name
//	type Name struct{}    -> Name
//	type Name interface{} -> Name
var declRegexp = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*[\(\[]|^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+`)

// scanDeclarations returns the set of function/type names declared in a
// Go source file. Reads line-by-line to keep memory bounded for the
// large extractor files (some are 2000+ lines).
func scanDeclarations(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]bool{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// Cheap fast-path: every match starts with one of two keywords.
		if !strings.HasPrefix(line, "func ") && !strings.HasPrefix(line, "type ") {
			continue
		}
		matches := declRegexp.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		name := matches[1]
		if name == "" {
			name = matches[2]
		}
		if name != "" {
			out[name] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
