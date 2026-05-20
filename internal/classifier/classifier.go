// Package classifier determines the language and skip status of a source file.
// It mirrors the upstream file-classifier behaviour so the Go implementation
// makes identical decisions — required for golden-fixture parity.
//
// Usage:
//
//	c, err := classifier.New("/path/to/languages/_data")
//	if err != nil { ... }
//	result := c.Classify("internal/foo/bar.go")
package classifier

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"
)

// maxIndexableBytes is the size threshold above which a file is skipped.
// Files exactly at this size are NOT skipped (>= check is "> limit").
const maxIndexableBytes int64 = 1 * 1024 * 1024 // 1 MiB

// binaryProbeBytes is how many bytes we inspect for null bytes when detecting
// binary files. Matches Python's convention.
const binaryProbeBytes = 512

// ClassifyResult holds the classification outcome for a single file.
type ClassifyResult struct {
	// Language is the detected language token (e.g. "go", "python").
	// Empty string means the file extension is not recognised.
	Language string

	// Skip is true when the file should not enter the extraction pipeline.
	Skip bool

	// SkipReason is a short machine-readable label explaining why Skip=true.
	// Empty when Skip=false.
	SkipReason string

	// Tier is the parsing tier derived from the YAML registry (0–3).
	// 0 = skip; higher = more expensive extraction.
	// Currently always 0 (skip) or 1 (index).
	Tier int
}

// skipPattern is a single entry from a language's skip_patterns.yaml (Go list
// format).  The YAML files from the Python indexer use different shapes per
// language; we parse only the fields that are present.
type skipPattern struct {
	Pattern string `yaml:"pattern"`
	Action  string `yaml:"action"`
}

// goSkipFile is the top-level shape for Go's skip_patterns.yaml.
type goSkipFile struct {
	SkipPatterns []skipPattern `yaml:"skip_patterns"`
}

// Classifier holds compiled state loaded once at startup.
type Classifier struct {
	// yamlDataDir is the root directory of languages/_data.
	yamlDataDir string

	// globSkips is a list of (glob, reason) pairs sourced from all
	// skip_patterns.yaml files that follow the flat-list format.
	globSkips []globSkip

	// tracer is the OTel tracer used for classify spans.
	tracer trace.Tracer
}

type globSkip struct {
	pattern string
	reason  string
}

// New constructs a Classifier by loading skip patterns from yamlDataDir.
// yamlDataDir should point to the languages/_data directory (e.g. the one
// inside the archigraph Python repo that is bundled with the Lambda).
//
// If yamlDataDir does not exist or is not a directory, New returns an error.
// Malformed YAML files produce a warning log and are skipped — they do not
// abort startup.
func New(yamlDataDir string, tracer trace.Tracer) (*Classifier, error) {
	if tracer == nil {
		tracer = otel.Tracer("archigraph/classifier")
	}
	c := &Classifier{
		yamlDataDir: yamlDataDir,
		tracer:      tracer,
	}
	// If yamlDataDir is empty, skip YAML-driven glob loading entirely;
	// only universal path skips and extension-based language detection apply.
	if yamlDataDir == "" {
		return c, nil
	}
	info, err := os.Stat(yamlDataDir)
	if err != nil {
		return nil, fmt.Errorf("classifier: yamlDataDir %q: %w", yamlDataDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("classifier: yamlDataDir %q is not a directory", yamlDataDir)
	}
	if err := c.loadSkipPatterns(); err != nil {
		// loadSkipPatterns returns an error only for hard failures (walk error).
		return nil, err
	}
	return c, nil
}

// Classify returns the classification result for the given file path.
// sizeBytes is the file size in bytes; pass -1 if unknown (size check is
// skipped in that case).
//
// Classify never panics and never returns an error — it degrades gracefully.
func (c *Classifier) Classify(ctx context.Context, filePath string) ClassifyResult {
	ctx, span := c.tracer.Start(ctx, "classifier.classify",
		trace.WithAttributes(attribute.String("classifier.file_path", filePath)),
	)
	defer span.End()

	result := c.classifyInner(ctx, filePath)

	span.SetAttributes(
		attribute.String("classifier.language", result.Language),
		attribute.Bool("classifier.skip", result.Skip),
		attribute.String("classifier.skip_reason", result.SkipReason),
		attribute.Int("classifier.tier", result.Tier),
	)
	if result.Skip {
		span.SetStatus(codes.Ok, "skipped")
	} else {
		span.SetStatus(codes.Ok, "")
	}
	return result
}

// ClassifyWithSize classifies a file given its already-known size in bytes.
// This is the primary entry point for the extraction pipeline which has the
// size from an S3 object listing without needing an extra stat call.
func (c *Classifier) ClassifyWithSize(ctx context.Context, filePath string, sizeBytes int64) ClassifyResult {
	_, span := c.tracer.Start(ctx, "classifier.classify_with_size",
		trace.WithAttributes(
			attribute.String("classifier.file_path", filePath),
			attribute.Int64("classifier.size_bytes", sizeBytes),
		),
	)
	defer span.End()

	result := c.classifyWithSizeInner(filePath, sizeBytes)

	span.SetAttributes(
		attribute.String("classifier.language", result.Language),
		attribute.Bool("classifier.skip", result.Skip),
		attribute.String("classifier.skip_reason", result.SkipReason),
		attribute.Int("classifier.tier", result.Tier),
	)
	span.SetStatus(codes.Ok, "")
	return result
}

// ---------------------------------------------------------------------------
// Internal classification logic
// ---------------------------------------------------------------------------

// classifyInner performs classification without an OTel span (called from
// Classify which owns the span).
func (c *Classifier) classifyInner(_ context.Context, filePath string) ClassifyResult {
	if filePath == "" {
		return ClassifyResult{Skip: true, SkipReason: "empty_path"}
	}

	norm := normalisePath(filePath)

	// 1. Universal path-based skip checks (vendor, .git, __pycache__, etc.)
	if reason, ok := universalPathSkip(norm); ok {
		return ClassifyResult{Skip: true, SkipReason: reason}
	}

	// 2. Binary extension check
	if isBinaryExtension(norm) {
		return ClassifyResult{Skip: true, SkipReason: "binary"}
	}

	// 3. Language detection by extension
	lang := detectLanguage(norm)

	// 4. YAML-sourced glob skip patterns (e.g. *.pb.go, wire_gen.go)
	if reason, ok := c.matchGlobSkips(norm); ok {
		return ClassifyResult{Language: lang, Skip: true, SkipReason: reason}
	}

	// 5. Unknown extension → skip
	if lang == "" {
		return ClassifyResult{Skip: true, SkipReason: "unsupported_extension"}
	}

	return ClassifyResult{Language: lang, Skip: false, Tier: 1}
}

// classifyWithSizeInner performs classification with a known file size.
func (c *Classifier) classifyWithSizeInner(filePath string, sizeBytes int64) ClassifyResult {
	if filePath == "" {
		return ClassifyResult{Skip: true, SkipReason: "empty_path"}
	}

	// Size check first — cheapest guard.
	if sizeBytes > maxIndexableBytes {
		return ClassifyResult{Skip: true, SkipReason: "too_large"}
	}

	norm := normalisePath(filePath)

	// Universal path-based skip checks.
	if reason, ok := universalPathSkip(norm); ok {
		return ClassifyResult{Skip: true, SkipReason: reason}
	}

	// Binary extension check.
	if isBinaryExtension(norm) {
		return ClassifyResult{Skip: true, SkipReason: "binary"}
	}

	// Language detection.
	lang := detectLanguage(norm)

	// YAML-sourced glob skip patterns.
	if reason, ok := c.matchGlobSkips(norm); ok {
		return ClassifyResult{Language: lang, Skip: true, SkipReason: reason}
	}

	// Unknown extension.
	if lang == "" {
		return ClassifyResult{Skip: true, SkipReason: "unsupported_extension"}
	}

	return ClassifyResult{Language: lang, Skip: false, Tier: 1}
}

// IsBinaryContent inspects the first binaryProbeBytes of a file's content for
// null bytes. Returns true if the file appears to be binary.
func IsBinaryContent(content []byte) bool {
	probe := content
	if len(probe) > binaryProbeBytes {
		probe = probe[:binaryProbeBytes]
	}
	return bytes.ContainsRune(probe, 0)
}

// ---------------------------------------------------------------------------
// Universal path-based skip logic
// ---------------------------------------------------------------------------

// depDirs are dependency/vendor directories that are always skipped regardless
// of language. Checked as path segment substrings.
var depDirs = []string{
	"/node_modules/",
	"/vendor/",
	"/.git/",
	"/__pycache__/",
	"/venv/",
	"/.venv/",
	"/dist/",
	"/build/",
	"/.next/",
	"/target/",
	"/out/",
	"/.expo/",
	"/testdata/",
}

// universalPathSkip returns a skip reason if the normalised path matches any
// well-known skip directory. The slash-prefix ensures we match full segments.
func universalPathSkip(norm string) (string, bool) {
	// Ensure leading slash for segment-boundary matching at path start.
	probe := norm
	if !strings.HasPrefix(probe, "/") {
		probe = "/" + probe
	}

	for _, d := range depDirs {
		if strings.Contains(probe, d) {
			// Strip surrounding slashes for the reason label.
			label := strings.Trim(d, "/.")
			return "vendor_" + label, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Binary extension detection
// ---------------------------------------------------------------------------

var binaryExtensions = map[string]struct{}{
	".so":    {},
	".dll":   {},
	".exe":   {},
	".dylib": {},
	".a":     {},
	".o":     {},
	".obj":   {},
	".lib":   {},
	".pyc":   {},
	".pyo":   {},
	".class": {},
	".jar":   {},
	".war":   {},
	".ear":   {},
	".zip":   {},
	".tar":   {},
	".gz":    {},
	".bz2":   {},
	".xz":    {},
	".7z":    {},
	".rar":   {},
	".png":   {},
	".jpg":   {},
	".jpeg":  {},
	".gif":   {},
	".bmp":   {},
	".ico":   {},
	".svg":   {}, // text but not code
	".woff":  {},
	".woff2": {},
	".ttf":   {},
	".otf":   {},
	".eot":   {},
	".mp3":   {},
	".mp4":   {},
	".avi":   {},
	".mov":   {},
	".wmv":   {},
	".pdf":   {},
}

func isBinaryExtension(norm string) bool {
	ext := strings.ToLower(path.Ext(norm))
	_, ok := binaryExtensions[ext]
	return ok
}

// ---------------------------------------------------------------------------
// Language detection
// ---------------------------------------------------------------------------

// extensionLanguageMap is the single source of truth, mirroring the upstream
// extension-to-language map exactly.
var extensionLanguageMap = map[string]string{
	// Python
	".py":  "python",
	".pyi": "python",
	".pyw": "python",
	// Go
	".go": "go",
	// JavaScript
	".js":  "javascript",
	".jsx": "javascript",
	".mjs": "javascript",
	".cjs": "javascript",
	// TypeScript
	".ts":  "typescript",
	".tsx": "typescript",
	".mts": "typescript",
	".cts": "typescript",
	// Java
	".java": "java",
	// Kotlin
	".kt":  "kotlin",
	".kts": "kotlin",
	// Ruby
	".rb":      "ruby",
	".rake":    "ruby",
	".gemspec": "ruby",
	// PHP
	".php": "php",
	// Rust
	".rs": "rust",
	// C#
	".cs":    "csharp",
	".razor": "razor",
	// Swift
	".swift": "swift",
	// Dart
	".dart": "dart",
	// Scala
	".scala": "scala",
	".sc":    "scala",
	// C / C++
	".c":   "c",
	".h":   "c",
	".cpp": "cpp",
	".cc":  "cpp",
	".cxx": "cpp",
	".hpp": "cpp",
	".hxx": "cpp",
	// Shell
	".sh":   "shell",
	".bash": "shell",
	".zsh":  "shell",
	".ksh":  "shell",
	// Fish shell (distinct syntax — function…end, not POSIX)
	".fish": "fish",
	// Just (command runner) — *.just files; bare Justfile/justfile handled below
	".just": "just",
	// Elixir
	".ex":  "elixir",
	".exs": "elixir",
	// Objective-C
	".m":  "objective_c",
	".mm": "objective_c",
	// Groovy
	".groovy": "groovy",
	".gradle": "groovy",
	".gvy":    "groovy",
	// Clojure
	".clj":  "clojure",
	".cljs": "clojure",
	".cljc": "clojure",
	".edn":  "clojure",
	// Zig
	".zig": "zig",
	// Lua
	".lua": "lua",
	// SQL
	".sql": "sql",
	// HCL / Terraform
	".tf":     "hcl",
	".tfvars": "hcl",
	".hcl":    "hcl",
	// Protobuf
	".proto": "protobuf",
	// CSS / SCSS / LESS
	".css":  "css",
	".scss": "css",
	".sass": "css",
	".less": "css",
	// HTML / Templates — all route to "html" to match extractor.Register("html", …)
	".html":       "html",
	".htm":        "html",
	".vue":        "html",
	".svelte":     "html",
	".astro":      "html",
	".erb":        "html",
	".ejs":        "html",
	".hbs":        "html",
	".handlebars": "html",
	".j2":         "html",
	".jinja":      "html",
	".jinja2":     "html",
	".pug":        "html",
	".njk":        "html",
	".mustache":   "html",
	".twig":       "html",
	".haml":       "html",
	".slim":       "html",
	// YAML — routes to yaml extractor
	".yaml": "yaml",
	".yml":  "yaml",
	// TOML — no toml extractor; route to text so it is not silently dropped
	".toml": "toml",
	// GraphQL
	".graphql": "graphql",
	".gql":     "graphql",
	// Prisma
	".prisma": "prisma",
	// Haskell
	".hs":  "haskell",
	".lhs": "haskell",
	// Perl
	".pl": "perl",
	".pm": "perl",
	".t":  "perl",
	// R
	".r":   "r",
	".R":   "r",
	".rmd": "r",
	".Rmd": "r",
	// Markdown / Documentation
	".md":       "markdown",
	".mdx":      "markdown",
	".markdown": "markdown",
	".rst":      "markdown",
}

// basenameLanguageMap maps exact file basenames (case-sensitive) to language
// tokens. Checked only when the file has no extension or its extension is not
// in extensionLanguageMap.
var basenameLanguageMap = map[string]string{
	"Dockerfile":    "dockerfile",
	"Containerfile": "dockerfile",
	// Just (command runner) — convention allows both "Justfile" and "justfile"
	// (and also ".justfile"). Bare-basename files have no extension.
	"Justfile":  "just",
	"justfile":  "just",
	".justfile": "just",
}

// detectLanguage returns the language token for the given normalised path, or
// "" if neither the extension nor the basename is recognised.
func detectLanguage(norm string) string {
	// Issue #501 — Twirl templates (*.scala.html) carry a double extension.
	// filepath.Ext only returns ".html", so we check for the compound suffix
	// before the single-extension lookup.
	lower := strings.ToLower(norm)
	if strings.HasSuffix(lower, ".scala.html") {
		return "scala"
	}
	ext := strings.ToLower(filepath.Ext(norm))
	if lang, ok := extensionLanguageMap[ext]; ok {
		return lang
	}
	// Fall back to basename matching for files like Dockerfile / Containerfile
	// that carry no extension.
	base := path.Base(norm)
	return basenameLanguageMap[base]
}

// ---------------------------------------------------------------------------
// YAML skip-pattern loading
// ---------------------------------------------------------------------------

// loadSkipPatterns walks yamlDataDir and loads all skip_patterns.yaml files
// that use the flat-list format (as Go, shell, etc. do). YAML files with other
// schemas (Python's condition-based format) are tolerated — unrecognised fields
// are ignored and only the pattern/action fields are used.
func (c *Classifier) loadSkipPatterns() error {
	walkErr := filepath.WalkDir(c.yamlDataDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("classifier: walk error", "path", p, "error", err)
			return nil // continue walking
		}
		if d.IsDir() || filepath.Base(p) != "skip_patterns.yaml" {
			return nil
		}
		c.parseSkipFile(p)
		return nil
	})
	return walkErr
}

// parseSkipFile reads one skip_patterns.yaml and appends any valid glob
// patterns to c.globSkips. Malformed files are logged and skipped.
func (c *Classifier) parseSkipFile(yamlPath string) {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		slog.Warn("classifier: cannot read skip_patterns.yaml", "path", yamlPath, "error", err)
		return
	}

	var doc goSkipFile
	if err := yaml.Unmarshal(data, &doc); err != nil {
		slog.Warn("classifier: malformed skip_patterns.yaml", "path", yamlPath, "error", err)
		return
	}

	for _, sp := range doc.SkipPatterns {
		if sp.Pattern == "" {
			continue
		}
		reason := "generated"
		if strings.Contains(strings.ToLower(sp.Action), "vendor") ||
			strings.Contains(strings.ToLower(sp.Pattern), "vendor") {
			reason = "vendor"
		}
		c.globSkips = append(c.globSkips, globSkip{pattern: sp.Pattern, reason: reason})
	}
}

// matchGlobSkips returns the first skip reason whose glob pattern matches the
// base name or the full normalised path of the file.
func (c *Classifier) matchGlobSkips(norm string) (string, bool) {
	base := path.Base(norm)
	for _, gs := range c.globSkips {
		// Match against basename first (e.g. "*.pb.go" → "service.pb.go").
		if matched, _ := filepath.Match(gs.pattern, base); matched {
			return gs.reason, true
		}
		// Match against full path for directory globs (e.g. "vendor/**").
		if matched, _ := filepath.Match(gs.pattern, norm); matched {
			return gs.reason, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// normalisePath converts backslashes to forward slashes for cross-platform
// consistency, matching Python's normalisation.
func normalisePath(p string) string {
	return filepath.ToSlash(p)
}
