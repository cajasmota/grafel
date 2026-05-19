package agentpatterns

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfig_thresholds(t *testing.T) {
	c := DefaultConfig()
	if c.PerSubagentThreshold != 2 {
		t.Fatalf("per_subagent_threshold default = %d, want 2", c.PerSubagentThreshold)
	}
	if c.ConvergenceThreshold != 3 {
		t.Fatalf("convergence_threshold default = %d, want 3", c.ConvergenceThreshold)
	}
	if c.ClusterSimilarityThreshold != 0.8 {
		t.Fatalf("cluster_similarity_threshold default = %v, want 0.8", c.ClusterSimilarityThreshold)
	}
	if c.CandidateDecayDays != 90 {
		t.Fatalf("candidate_decay_days default = %d, want 90", c.CandidateDecayDays)
	}
}

func TestLoadSave_roundtrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".archigraph")
	cfg := DefaultConfig()
	cfg.PerSubagentThreshold = 5
	cfg.SilentApplyThreshold["code"] = 0.9
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PerSubagentThreshold != 5 {
		t.Fatalf("per_subagent: want 5, got %d", loaded.PerSubagentThreshold)
	}
	if loaded.SilentApplyThreshold["code"] != 0.9 {
		t.Fatalf("silent_apply code: want 0.9, got %v", loaded.SilentApplyThreshold["code"])
	}
}

func TestSetConfigKey(t *testing.T) {
	cfg := DefaultConfig()
	var err error
	cfg, err = SetConfigKey(cfg, "candidate_decay_days", "180")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CandidateDecayDays != 180 {
		t.Fatalf("want 180, got %d", cfg.CandidateDecayDays)
	}
	cfg, err = SetConfigKey(cfg, "silent_apply_threshold.code", "0.95")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SilentApplyThreshold["code"] != 0.95 {
		t.Fatalf("want 0.95, got %v", cfg.SilentApplyThreshold["code"])
	}
	_, err = SetConfigKey(cfg, "unknown_key", "1")
	if err == nil {
		t.Fatalf("expected error for unknown key")
	}
}

func TestSilentApplyThresholdFor_unknownCategoryFallsBack(t *testing.T) {
	c := DefaultConfig()
	if v := c.SilentApplyThresholdFor("nonsense"); v != DefaultSilentApplyCode {
		t.Fatalf("unknown category should fall back, got %v", v)
	}
	if v := c.SilentApplyThresholdFor(CategoryProcess); v != DefaultSilentApplyProcess {
		t.Fatalf("process category: got %v", v)
	}
}
