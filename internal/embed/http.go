package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// httpBackend talks to any OpenAI-compatible POST {url}/v1/embeddings
// endpoint (OpenAI, Ollama, LM Studio, text-embeddings-inference, Voyage…).
type httpBackend struct {
	url    string
	model  string
	apiKey string
	dims   int
	client *http.Client
}

func newHTTPBackend(cfg HTTPConfig) (Backend, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("http embedding backend: url is required")
	}
	dims := cfg.Dims
	if dims == 0 {
		dims = DefaultBuiltinDims
	}
	return &httpBackend{
		url:    normalizeEmbeddingsURL(cfg.URL),
		model:  cfg.Model,
		apiKey: cfg.APIKey,
		dims:   dims,
		client: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// normalizeEmbeddingsURL accepts either a base ("http://host:11434/v1") or a
// full endpoint ("http://host:11434/v1/embeddings") and returns the full
// endpoint.
func normalizeEmbeddingsURL(u string) string {
	u = strings.TrimRight(u, "/")
	if strings.HasSuffix(u, "/embeddings") {
		return u
	}
	return u + "/embeddings"
}

func (h *httpBackend) Dims() int    { return h.dims }
func (h *httpBackend) Name() string { return "http:" + h.model }
func (h *httpBackend) Close() error { return nil }

type embeddingsRequest struct {
	Model string   `json:"model,omitempty"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (h *httpBackend) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embeddingsRequest{Model: h.model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http embeddings request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http embeddings: status %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var er embeddingsResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, fmt.Errorf("http embeddings: decode response: %w", err)
	}
	if er.Error != nil {
		return nil, fmt.Errorf("http embeddings: %s", er.Error.Message)
	}
	if len(er.Data) != len(texts) {
		return nil, fmt.Errorf("http embeddings: expected %d vectors, got %d", len(texts), len(er.Data))
	}
	out := make([][]float32, len(texts))
	for _, d := range er.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("http embeddings: out-of-range index %d", d.Index)
		}
		v := d.Embedding
		// Learn the true dimensionality from the first response and normalize
		// so dot product == cosine, matching the builtin backend's contract.
		if h.dims == 0 || len(v) != h.dims {
			h.dims = len(v)
		}
		out[d.Index] = l2Normalize(v)
	}
	for i := range out {
		if out[i] == nil {
			return nil, fmt.Errorf("http embeddings: missing vector for input %d", i)
		}
	}
	return out, nil
}

func l2Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	inv := float32(1.0 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
