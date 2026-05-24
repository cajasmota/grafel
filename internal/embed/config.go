// Package embed provides the semantic-search embedding pipeline for
// archigraph: a pluggable embedding backend (builtin MiniLM via hugot with
// -tags simplego, an OpenAI-compatible HTTP backend, or disabled), AST-aware
// chunking, a per-repo on-disk vector sidecar (embeddings.bin), and
// brute-force cosine search used by the MCP server for RRF fusion with BM25
// (#461, ADR-0019).
//
// Default mode (S6 / #2156): embeddings use bundled MiniLM (via -tags simplego
// builds). BM25 search works without any configuration; semantic search is
// automatic. Power users can opt into an HTTP endpoint via
// ARCHIGRAPH_EMBEDDING_URL=http://localhost:11434/v1 (Ollama / LM Studio /
// any OpenAI-compatible endpoint), or opt out entirely with
// ARCHIGRAPH_EMBEDDING_DISABLE=true.
package embed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Backend kinds.
const (
	BackendBuiltin  = "builtin"
	BackendHTTP     = "http"
	BackendDisabled = "disabled"
)

// Env override variables (ADR-0019). Env always wins over the config file.
const (
	EnvBackend  = "ARCHIGRAPH_EMBEDDING_BACKEND"
	EnvURL      = "ARCHIGRAPH_EMBEDDING_URL"
	EnvModel    = "ARCHIGRAPH_EMBEDDING_MODEL"
	EnvAPIKey   = "ARCHIGRAPH_EMBEDDING_API_KEY"
	EnvDims     = "ARCHIGRAPH_EMBEDDING_DIMS"
	EnvDisable  = "ARCHIGRAPH_EMBEDDING_DISABLE"
)

// DefaultBuiltinModel is the bundled-by-download MiniLM model. hugot fetches
// it once into the model cache on first use; see builtin_simplego.go.
const (
	DefaultBuiltinModel = "sentence-transformers/all-MiniLM-L6-v2"
	DefaultBuiltinDims  = 384
)

// HTTPConfig configures the OpenAI-compatible /v1/embeddings backend.
type HTTPConfig struct {
	URL    string `json:"url"`
	Model  string `json:"model"`
	APIKey string `json:"api_key,omitempty"`
	Dims   int    `json:"dims,omitempty"`
}

// Config is the on-disk + env-resolved embedding configuration.
type Config struct {
	Backend string     `json:"backend"`
	HTTP    HTTPConfig `json:"http,omitempty"`
}

// configFileName is the per-user config file under ~/.archigraph.
const configFileName = "embeddings.json"

// ConfigPath returns the resolved path to embeddings.json. It honours
// ARCHIGRAPH_HOME for test/agent isolation, falling back to ~/.archigraph.
func ConfigPath() string {
	return filepath.Join(homeDir(), configFileName)
}

func homeDir() string {
	if h := os.Getenv("ARCHIGRAPH_HOME"); h != "" {
		return h
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ".archigraph"
	}
	return filepath.Join(h, ".archigraph")
}

// LoadConfig reads embeddings.json (if present) and applies env overrides.
// A missing file is NOT an error: it yields the zero-config default
// (builtin — bundled MiniLM). An unparseable file falls back to the default too,
// surfacing the parse error to the caller for logging.
//
// To opt in to an HTTP backend set ARCHIGRAPH_EMBEDDING_URL or write
// {"backend":"http","http":{"url":"..."}} to ~/.archigraph/embeddings.json.
// To opt out entirely set ARCHIGRAPH_EMBEDDING_DISABLE=true.
func LoadConfig() (Config, error) {
	cfg := Config{Backend: BackendBuiltin}
	var parseErr error

	if data, err := os.ReadFile(ConfigPath()); err == nil {
		var fileCfg Config
		if jerr := json.Unmarshal(data, &fileCfg); jerr != nil {
			parseErr = fmt.Errorf("parse %s: %w", ConfigPath(), jerr)
		} else {
			if fileCfg.Backend != "" {
				cfg.Backend = fileCfg.Backend
			}
			cfg.HTTP = fileCfg.HTTP
		}
	} else if !os.IsNotExist(err) {
		parseErr = fmt.Errorf("read %s: %w", ConfigPath(), err)
	}

	applyEnvOverrides(&cfg)

	if err := cfg.normalize(); err != nil {
		return cfg, err
	}
	return cfg, parseErr
}

func applyEnvOverrides(cfg *Config) {
	// ARCHIGRAPH_EMBEDDING_DISABLE overrides everything: if set to true/1, force disabled.
	if v := os.Getenv(EnvDisable); v != "" && (v == "true" || v == "1" || v == "yes") {
		cfg.Backend = BackendDisabled
		return
	}

	if v := os.Getenv(EnvBackend); v != "" {
		cfg.Backend = v
	}
	// If a URL is supplied via env but no backend was explicitly chosen as
	// builtin/disabled, route through HTTP — this is the documented power-user
	// path (ARCHIGRAPH_EMBEDDING_URL=... just works).
	if v := os.Getenv(EnvURL); v != "" {
		cfg.HTTP.URL = v
		if os.Getenv(EnvBackend) == "" {
			cfg.Backend = BackendHTTP
		}
	}
	if v := os.Getenv(EnvModel); v != "" {
		cfg.HTTP.Model = v
	}
	if v := os.Getenv(EnvAPIKey); v != "" {
		cfg.HTTP.APIKey = v
	}
	if v := os.Getenv(EnvDims); v != "" {
		if d, err := strconv.Atoi(v); err == nil {
			cfg.HTTP.Dims = d
		}
	}
}

func (cfg *Config) normalize() error {
	cfg.Backend = strings.ToLower(strings.TrimSpace(cfg.Backend))
	switch cfg.Backend {
	case "":
		cfg.Backend = BackendBuiltin
	case BackendBuiltin, BackendDisabled:
		// ok
	case BackendHTTP:
		if cfg.HTTP.URL == "" {
			return fmt.Errorf("embedding backend %q requires a url (set %s or http.url)", BackendHTTP, EnvURL)
		}
		if cfg.HTTP.Dims == 0 {
			cfg.HTTP.Dims = DefaultBuiltinDims
		}
	default:
		return fmt.Errorf("unknown embedding backend %q (want builtin|http|disabled)", cfg.Backend)
	}
	return nil
}
