package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Finding mirrors the JSON body written by archigraph_save_finding.
//
// Path is the absolute file path on disk. SavedAtFile is the file's mtime,
// used as a fallback ordering key when SavedAt is missing/unparseable.
type Finding struct {
	Question    string    `json:"question"`
	Answer      string    `json:"answer"`
	Type        string    `json:"type,omitempty"`
	Nodes       []string  `json:"nodes,omitempty"`
	RepoFilter  []string  `json:"repo_filter,omitempty"`
	SavedAt     string    `json:"saved_at,omitempty"`
	Path        string    `json:"path,omitempty"`
	SavedAtFile time.Time `json:"-"`
}

// findingsMemDir resolves the on-disk memory dir for a group: registry override
// or the conventional ~/.archigraph/groups/<group>-memory.
func findingsMemDir(groupName string, lg *LoadedGroup) string {
	if lg != nil && lg.MemoryDir != "" {
		return lg.MemoryDir
	}
	return defaultMemoryDir(groupName)
}

// loadFindings reads every *.json file under memDir and returns them sorted
// newest-first by SavedAt (file mtime fallback). A missing dir is not an
// error — returns an empty slice.
func loadFindings(memDir string) []Finding {
	if memDir == "" {
		return nil
	}
	entries, err := os.ReadDir(memDir)
	if err != nil {
		return nil
	}
	out := make([]Finding, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		full := filepath.Join(memDir, name)
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		var f Finding
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		f.Path = full
		if info, err := ent.Info(); err == nil {
			f.SavedAtFile = info.ModTime()
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return findingTime(out[i]).After(findingTime(out[j]))
	})
	return out
}

// findingTime returns the best-effort timestamp for ordering.
func findingTime(f Finding) time.Time {
	if f.SavedAt != "" {
		if t, err := time.Parse(time.RFC3339, f.SavedAt); err == nil {
			return t
		}
	}
	return f.SavedAtFile
}

// findingsForEntity filters findings whose `nodes` field references any of
// the provided entity IDs (in either local or prefixed form). Both
// `entityLocalID` and `prefixedID` are matched as substrings so callers don't
// have to care which form was saved.
func findingsForEntity(all []Finding, entityIDs ...string) []Finding {
	if len(entityIDs) == 0 {
		return nil
	}
	out := []Finding{}
	for _, f := range all {
		if findingMentions(f, entityIDs...) {
			out = append(out, f)
		}
	}
	return out
}

// findingMentions reports whether a finding's nodes list contains any of the
// supplied IDs (exact match on either form).
func findingMentions(f Finding, ids ...string) bool {
	for _, id := range ids {
		if id == "" {
			continue
		}
		for _, n := range f.Nodes {
			if n == id {
				return true
			}
		}
	}
	return false
}

// findingsSince filters by SavedAt >= since. Zero time keeps everything.
func findingsSince(all []Finding, since time.Time) []Finding {
	if since.IsZero() {
		return all
	}
	out := []Finding{}
	for _, f := range all {
		if !findingTime(f).Before(since) {
			out = append(out, f)
		}
	}
	return out
}

// findingsToJSON renders a slice of findings as the wire shape (drops the
// internal SavedAtFile field).
func findingsToJSON(in []Finding, limit int) []map[string]any {
	if limit > 0 && len(in) > limit {
		in = in[:limit]
	}
	out := make([]map[string]any, 0, len(in))
	for _, f := range in {
		row := map[string]any{
			"question": f.Question,
			"answer":   f.Answer,
			"type":     f.Type,
			"nodes":    f.Nodes,
			"saved_at": f.SavedAt,
			"path":     f.Path,
		}
		if len(f.RepoFilter) > 0 {
			row["repo_filter"] = f.RepoFilter
		}
		out = append(out, row)
	}
	return out
}
