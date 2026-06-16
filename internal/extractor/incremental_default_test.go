package extractor

import "testing"

// TestIsIncrementalEnabledDefaultOn pins the #5231 behaviour change: the
// incremental file-level reindex path is ON by default (env unset) and can be
// forced OFF with GRAFEL_INCREMENTAL_REINDEX=0/false, while an explicit Config
// value always wins over the env.
func TestIsIncrementalEnabledDefaultOn(t *testing.T) {
	t.Run("env unset → default ON (#5231)", func(t *testing.T) {
		t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "")
		var c *ExtractorConfig // nil receiver must also default ON
		if !c.IsIncrementalEnabled() {
			t.Fatal("nil-config IsIncrementalEnabled() = false; want true (default ON)")
		}
		cfg := ConfigFromEnv() // not explicitly set → defers to default
		if !cfg.IsIncrementalEnabled() {
			t.Fatal("ConfigFromEnv().IsIncrementalEnabled() = false; want true (default ON)")
		}
	})

	t.Run("env =0 forces OFF", func(t *testing.T) {
		t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "0")
		var c *ExtractorConfig
		if c.IsIncrementalEnabled() {
			t.Fatal("GRAFEL_INCREMENTAL_REINDEX=0 still enabled; want OFF")
		}
		cfg := ConfigFromEnv()
		if cfg.IsIncrementalEnabled() {
			t.Fatal("ConfigFromEnv with =0 still enabled; want OFF")
		}
	})

	t.Run("env =false forces OFF", func(t *testing.T) {
		t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "false")
		var c *ExtractorConfig
		if c.IsIncrementalEnabled() {
			t.Fatal("GRAFEL_INCREMENTAL_REINDEX=false still enabled; want OFF")
		}
	})

	t.Run("env =1 ON", func(t *testing.T) {
		t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "1")
		var c *ExtractorConfig
		if !c.IsIncrementalEnabled() {
			t.Fatal("GRAFEL_INCREMENTAL_REINDEX=1 not enabled; want ON")
		}
	})

	t.Run("explicit Config wins over env", func(t *testing.T) {
		// Env says OFF, Config explicitly says ON → Config wins.
		t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "0")
		c := &ExtractorConfig{IncrementalReindex: true, IncrementalReindexSet: true}
		if !c.IsIncrementalEnabled() {
			t.Fatal("explicit Config ON overridden by env OFF; Config must win")
		}
		// Env unset (default ON), Config explicitly says OFF → Config wins.
		t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "")
		c = &ExtractorConfig{IncrementalReindex: false, IncrementalReindexSet: true}
		if c.IsIncrementalEnabled() {
			t.Fatal("explicit Config OFF overridden by default ON; Config must win")
		}
	})
}
