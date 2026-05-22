//go:build simplego

package embed

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

// builtinBackend runs the bundled all-MiniLM-L6-v2 model fully in-process via
// hugot's pure-Go (`simplego`) ONNX backend — no CGO, no native libraries.
//
// Model weights are NOT compiled into the binary: hugot v0.7.3 fetches the
// ONNX model from HuggingFace on first use into a local cache directory
// (~/.archigraph/models). This is a one-time ~23MB download. After that the
// backend is fully offline. (This is the one deviation from the original
// "bundle weights as Go bytes" plan — see ADR-0019 / the #461 PR notes.)
type builtinBackend struct {
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
	mu       sync.Mutex
}

// quantizedOnnxFile picks the platform-appropriate quantized MiniLM weights
// to minimise the download and memory footprint.
func quantizedOnnxFile() string {
	switch runtime.GOARCH {
	case "arm64":
		return "onnx/model_qint8_arm64.onnx"
	default:
		return "onnx/model_quint8_avx2.onnx"
	}
}

func modelCacheDir() string {
	d := filepath.Join(homeDir(), "models")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func newBuiltinBackend(ctx context.Context) (Backend, error) {
	cacheDir := modelCacheDir()
	onnxFile := quantizedOnnxFile()

	dlOpts := hugot.NewDownloadOptions()
	dlOpts.OnnxFilePath = onnxFile
	modelPath, err := hugot.DownloadModel(ctx, DefaultBuiltinModel, cacheDir, dlOpts)
	if err != nil {
		return nil, fmt.Errorf("builtin embedding model download (%s): %w", DefaultBuiltinModel, err)
	}

	sess, err := hugot.NewGoSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("builtin embedding session: %w", err)
	}

	cfg := hugot.FeatureExtractionConfig{
		ModelPath:    modelPath,
		Name:         "archigraph-minilm",
		OnnxFilename: filepath.Base(onnxFile),
	}
	pl, err := hugot.NewPipeline(sess, cfg)
	if err != nil {
		_ = sess.Destroy()
		return nil, fmt.Errorf("builtin embedding pipeline: %w", err)
	}

	return &builtinBackend{session: sess, pipeline: pl}, nil
}

func (b *builtinBackend) Dims() int    { return DefaultBuiltinDims }
func (b *builtinBackend) Name() string { return "builtin:all-MiniLM-L6-v2" }

func (b *builtinBackend) Close() error {
	if b.session != nil {
		return b.session.Destroy()
	}
	return nil
}

func (b *builtinBackend) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out, err := b.pipeline.RunPipeline(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("builtin embed: %w", err)
	}
	// hugot already L2-normalizes feature-extraction output; normalize again
	// defensively so the dot-product==cosine contract is guaranteed.
	res := make([][]float32, len(out.Embeddings))
	for i, v := range out.Embeddings {
		res[i] = l2Normalize(v)
	}
	return res, nil
}

// BuiltinCompiledIn reports whether the pure-Go MiniLM backend is available.
func BuiltinCompiledIn() bool { return true }
