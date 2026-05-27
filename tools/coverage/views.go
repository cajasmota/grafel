package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

// ListFilter narrows the result of the list subcommand.
type ListFilter struct {
	Status    string
	Language  string
	Category  string
	StaleDays int
}

// listRecords returns records matching f, in stable ID order.
func listRecords(reg *Registry, f ListFilter) []Record {
	out := make([]Record, 0, len(reg.Records))
	cutoff := time.Time{}
	if f.StaleDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -f.StaleDays)
	}
	for _, rec := range reg.Records {
		if f.Language != "" && rec.Language != f.Language {
			continue
		}
		if f.Category != "" && rec.Category != f.Category {
			continue
		}
		caps := rec.AllCapabilities()
		if f.Status != "" {
			match := false
			for _, cap := range caps {
				if cap.Status == f.Status {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		if f.StaleDays > 0 {
			stale := false
			for _, cap := range caps {
				if cap.VerifiedAt == "" {
					continue
				}
				t, err := time.Parse("2006-01-02", cap.VerifiedAt)
				if err == nil && t.Before(cutoff) {
					stale = true
					break
				}
			}
			if !stale {
				continue
			}
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// findRecord returns the record with the given ID, or nil.
func findRecord(reg *Registry, id string) *Record {
	for i := range reg.Records {
		if reg.Records[i].ID == id {
			return &reg.Records[i]
		}
	}
	return nil
}

// Stats summarises status counts across the registry.
type Stats struct {
	Total       int                       `json:"total"`
	ByStatus    map[string]int            `json:"by_status"`
	ByLanguage  map[string]LanguageStats  `json:"by_language"`
	ByCategory  map[string]int            `json:"by_category"`
	Capabilities int                      `json:"capabilities"`
}

// LanguageStats aggregates per-language status counts.
type LanguageStats struct {
	Records    int `json:"records"`
	Full       int `json:"full"`
	Partial    int `json:"partial"`
	Missing    int `json:"missing"`
	NotAppl    int `json:"not_applicable"`
}

// computeStats aggregates counters across the registry.
func computeStats(reg *Registry) Stats {
	s := Stats{
		Total:      len(reg.Records),
		ByStatus:   map[string]int{},
		ByLanguage: map[string]LanguageStats{},
		ByCategory: map[string]int{},
	}
	for _, rec := range reg.Records {
		s.ByCategory[rec.Category]++
		ls := s.ByLanguage[rec.Language]
		ls.Records++
		for _, cap := range rec.AllCapabilities() {
			s.Capabilities++
			s.ByStatus[cap.Status]++
			switch cap.Status {
			case StatusFull:
				ls.Full++
			case StatusPartial:
				ls.Partial++
			case StatusMissing:
				ls.Missing++
			case StatusNotApplicable:
				ls.NotAppl++
			}
		}
		s.ByLanguage[rec.Language] = ls
	}
	return s
}

// gapsRecords returns records that contain at least one missing or
// partial capability. Filters by language/category like listRecords.
func gapsRecords(reg *Registry, language, category string) []Record {
	out := make([]Record, 0)
	for _, rec := range reg.Records {
		if language != "" && rec.Language != language {
			continue
		}
		if category != "" && rec.Category != category {
			continue
		}
		hit := false
		for _, cap := range rec.AllCapabilities() {
			if cap.Status == StatusMissing || cap.Status == StatusPartial {
				hit = true
				break
			}
		}
		if hit {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// printRecordsText writes a human-readable table to w. Grouped records
// flatten capabilities for display; the per-group structure is shown
// on the markdown detail pages and via JSON output.
func printRecordsText(w io.Writer, recs []Record) {
	for _, rec := range recs {
		fmt.Fprintf(w, "%-50s  %-18s  %-12s  %s\n", rec.ID, rec.Category, rec.Language, rec.Label)
		caps := rec.AllCapabilities()
		keys := sortedCapKeys(caps)
		for _, k := range keys {
			fmt.Fprintf(w, "    %-30s  %s\n", k, caps[k].Status)
		}
	}
}

// printJSON emits v as indented JSON with deterministic key ordering.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
