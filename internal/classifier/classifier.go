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
	"regexp"
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
// inside the grafel Python repo that is bundled with the Lambda).
//
// If yamlDataDir does not exist or is not a directory, New returns an error.
// Malformed YAML files produce a warning log and are skipped — they do not
// abort startup.
func New(yamlDataDir string, tracer trace.Tracer) (*Classifier, error) {
	if tracer == nil {
		tracer = otel.Tracer("grafel/classifier")
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
	// Crystal
	".cr": "crystal",
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
	// Common Lisp
	".lisp": "commonlisp",
	".lsp":  "commonlisp",
	".cl":   "commonlisp",
	// Scheme
	".scm": "scheme",
	".ss":  "scheme",
	// Racket
	".rkt": "racket",
	// Zig
	".zig": "zig",
	// Erlang
	".erl": "erlang",
	".hrl": "erlang",
	// Nim
	".nim":    "nim",
	".nimble": "nim",
	// F#
	".fs":     "fsharp",
	".fsi":    "fsharp",
	".fsx":    "fsharp",
	".fsproj": "fsharp",
	// ReasonML
	".re":  "reasonml",
	".rei": "reasonml",
	// Lua
	".lua": "lua",
	// LuaRocks package manifest (#5365) — a *.rockspec is Lua source (a table
	// literal of package/version/source/build/dependencies). Classified as
	// "lua" so it survives classification and reaches the _cross_manifest
	// extractor, which suffix-matches *.rockspec and parses its dependency
	// lists. The Lua tree-sitter extractor also parses it cleanly (valid Lua).
	".rockspec": "lua",
	// SQL
	".sql": "sql",
	// Terraform / HCL
	// .tf and .tfvars are Terraform-specific; route to "terraform" so the
	// extractor emits Language="terraform" on every entity (enabling
	// Terraform-specific resolver patterns and graph labels).
	// .hcl stays "hcl" — Packer, Vault, Consul and generic HCL consumers
	// all use the same HCL grammar and extractor but are not Terraform.
	".tf":     "terraform",
	".tfvars": "terraform",
	".hcl":    "hcl",
	// OpenTofu (#3553) — the Apache-licensed Terraform fork uses byte-for-byte
	// identical HCL with .tofu / .tofu.json extensions. Route to the same
	// "terraform" token so the shared hcl/terraform extractor produces full
	// resource + dependency parity and every downstream IaC engine pass
	// (lang=="terraform" gates in iac_sns_edges, event_bus_edges,
	// http_endpoint_synthesis, dynamic_patterns_terraform) fires unchanged.
	// .tofu.json is handled as a compound suffix in detectLanguage (filepath.Ext
	// only sees ".json"), mirroring the .scala.html precedent.
	".tofu": "terraform",
	// Azure Bicep — Azure-native IaC DSL (resource/module/param/var/output).
	// No tree-sitter grammar is vendored; the bicep extractor is regex/line-
	// based (internal/extractors/bicep) so the tree is nil at dispatch time.
	".bicep": "bicep",
	// Protobuf
	".proto": "protobuf",
	// Avro schema (#3690) — Avro schemas are JSON documents with a `.avsc`
	// extension declaring record/enum/fixed data-contract types. Routed to the
	// content-based "avro" extractor (no tree-sitter grammar; it json-decodes
	// the file body). The companion `.avpr` protocol file carries the same
	// record-type schemas inside a `types` array and is handled identically.
	".avsc": "avro",
	".avpr": "avro",
	// CSS / SCSS / LESS
	".css":  "css",
	".scss": "css",
	".sass": "css",
	".less": "css",
	// Vue / Svelte / Astro single-file components.
	//
	// These tokens are the *runtime extractor-dispatch keys* (extractor.Get
	// is keyed on this Language), NOT coverage languages. Vue/Svelte/Astro are
	// JS/TS frameworks with custom SFC file formats — the same class as React
	// (.tsx → jsts). Their coverage language is jsts: their registry records
	// live at lang.jsts.framework.{vue,svelte,astro} and they do NOT appear as
	// standalone languages on the coverage by-language axis (see #2821 and
	// tools/coverage/languages.go extractorDirAliases). The dedicated SFC
	// extractors crack <template>/<script>/<style> and hand the <script> body
	// to the JS/TS pipeline, so the dispatch token MUST stay the SFC format
	// here — collapsing it to "jsts" would bypass the SFC extractor and drop
	// the component/prop/rune entities. Keep format → dispatch token; the
	// jsts collapse happens on the coverage axis only.
	".vue":    "vue",
	".svelte": "svelte",
	".astro":  "astro",
	// HTML / Templates — all route to "html" to match extractor.Register("html", …)
	".html":       "html",
	".htm":        "html",
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
	// nginx config (#3633, epic #3625) — *.nginx site files. No language
	// extractor exists; the file still reaches the Pass 2.5 detector (where the
	// deployment-topology pass parses upstream/proxy_pass request-flow), because
	// classified files are added to the Pass 2.5 set even when extraction is a
	// no-op (cmd/grafel/index.go).
	".nginx": "nginx",
	// GraphQL
	".graphql": "graphql",
	".gql":     "graphql",
	// #4006 — gqlgen's canonical schema file is graph/schema.graphqls. Without
	// this mapping the file is dropped (lang=""), so the SDL type→type graph
	// (#3805) and gqlgen endpoint synthesis never fire on a real gqlgen project.
	".graphqls": "graphql",
	// Prisma
	".prisma": "prisma",
	// Elm
	".elm": "elm",
	// Haskell
	".hs":  "haskell",
	".lhs": "haskell",
	// Pony — actor-based capability-secure language
	".pony": "pony",
	// Idris — dependently-typed functional language
	".idr": "idris",
	// Solidity — Ethereum smart contracts
	".sol": "solidity",
	// Verilog / SystemVerilog — hardware description languages (EDA / silicon)
	".v":   "verilog",
	".vh":  "verilog",
	".sv":  "systemverilog",
	".svh": "systemverilog",
	// VHDL — hardware description language (EDA / silicon)
	".vhd":  "vhdl",
	".vhdl": "vhdl",
	// Assembly — embedded / OS / crypto / firmware hot paths (#2744).
	// A single "assembly" language token covers every dialect (x86/x86-64,
	// ARM, ARM64/AArch64, m68k) and both syntaxes (AT&T / Intel/NASM); the
	// dialect is recorded as an entity attribute, NOT a separate language
	// (mirrors the vue/svelte/astro = jsts taxonomy lesson). Extension
	// matching is case-insensitive (.s and .S both route here — both are
	// GNU-as sources, .S merely runs the C preprocessor first).
	".s":    "assembly",
	".asm":  "assembly",
	".nasm": "assembly",
	// OCaml — .ml is claimed for OCaml (SML is much less common)
	".ml":  "ocaml",
	".mli": "ocaml",
	// Standard ML — .ml is OCaml above; SML uses .sml/.sig/.fun
	".sml": "sml",
	".sig": "sml",
	".fun": "sml",
	// ReScript
	".res":  "rescript",
	".resi": "rescript",
	// Perl
	".pl": "perl",
	".pm": "perl",
	".t":  "perl",
	// R
	".r":   "r",
	".R":   "r",
	".rmd": "r",
	".Rmd": "r",
	// COBOL — mainframe / banking. .cob/.cbl/.cobol are program source;
	// .cpy is a copybook (the COBOL include unit) — both route to the cobol
	// extractor, which handles COPY directives and data-only copybook bodies.
	".cob":   "cobol",
	".cbl":   "cobol",
	".cobol": "cobol",
	".cpy":   "cobol",
	// IMS DBDGEN/PSBGEN macro decks (#5057). These assembler-macro source decks
	// declare the IMS database/segment hierarchy (.dbd) and a program's PCB view
	// (.psb); the cobol extractor's isIMSMacroDeck branch parses them into the
	// IMS schema entities the COBOL DL/I segment access (#4948) resolves against.
	".dbd": "cobol",
	".psb": "cobol",
	// JCL — IBM Job Control Language. The mainframe batch-orchestration DSL
	// that drives z/OS JES2/JES3 job submission; EXEC PGM= steps name the
	// COBOL programs a job invokes. Routed to the jcl extractor, which emits
	// the JCL→COBOL cross-language bridge (#2843).
	".jcl": "jcl",
	// Markdown / Documentation
	".md":       "markdown",
	".mdx":      "markdown",
	".markdown": "markdown",
	".rst":      "markdown",
}

// exactBasenameLanguageMap maps exact file basenames (case-sensitive) to
// language tokens for files that carry a known extension but must be assigned
// a more specific language key than the generic extension match would produce.
// This map is consulted BEFORE extensionLanguageMap so that specialised
// handlers win over the generic extension-based dispatch.
//
// Issue #497 — Package.swift is a SwiftPM manifest whose extraction
// requires a dedicated regex-based extractor ("swift_package") rather than
// the tree-sitter-based generic Swift extractor ("swift").
var exactBasenameLanguageMap = map[string]string{
	"Package.swift": "swift_package",
	// Elm manifest (#5375) — elm.json is JSON but has no other meaning than the
	// Elm project manifest. Classified as "elm" (rather than dropped by the
	// generic .json routing) so it reaches the _cross_manifest extractor, which
	// exact-name-matches it and parses its application/package dependency blocks.
	"elm.json": "elm",
}

// apiGwOcelotJSONRe matches Ocelot config files with the conventional
// ocelot.<env>.json naming (ocelot.json is matched explicitly). #3723.
var apiGwOcelotJSONRe = regexp.MustCompile(`(?i)^ocelot\.[a-z0-9_-]+\.json$`)

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
	// Reverse-proxy / API-gateway configs (#3633, epic #3625). These carry no
	// language extractor; they still reach the Pass 2.5 detector, where the
	// deployment-topology pass parses their request-flow topology
	// (nginx upstream/proxy_pass, Caddy reverse_proxy).
	"nginx.conf": "nginx",
	"Caddyfile":  "caddy",
	// LuaRocks lockfile (#5365) — luarocks.lock has no code extension; it is a
	// Lua return-table of pinned resolved versions. Classified as "lua" so it
	// reaches the _cross_manifest extractor (which exact-name-matches it and
	// parses the resolved dependency tree as kind=locked deps).
	"luarocks.lock": "lua",
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

	// OpenTofu (#3553) — .tofu.json carries a compound extension; filepath.Ext
	// only returns ".json", which would otherwise fall through to the Debezium
	// JSON routing below (or be dropped). Route it to "terraform" — the same
	// token as .tf/.tofu — for full Terraform extraction parity. Checked before
	// the generic .json branch so OpenTofu JSON config always wins.
	if strings.HasSuffix(lower, ".tofu.json") {
		return "terraform"
	}

	// JSON Schema (#3690) — files following the `*.schema.json` convention are
	// JSON Schema data-contract documents. Routed to the content-based
	// "jsonschema" extractor, which json-decodes the body and content-sniffs for
	// a `$schema`/`properties`/`$ref` shape before emitting (a false positive is
	// a harmless no-op). Checked before the generic `.json` branch so schema
	// files always win over the Debezium routing below. `.tofu.json` already
	// returned above, so there is no conflict with the OpenTofu compound suffix.
	if strings.HasSuffix(lower, ".schema.json") {
		return "jsonschema"
	}

	// Issue #1708 — narrow JSON routing for Debezium / Kafka-Connect
	// connector files. Indexing every .json file would balloon scope
	// (package.json, tsconfig.json, jest.config.json, lockfiles, etc.), so
	// we opt-in path patterns whose only purpose in any reasonable repo is
	// a CDC connector definition. The downstream extractor still content-
	// sniffs for `io.debezium` / `connector.class` to confirm before
	// emitting any entities, so a false positive is a harmless no-op.
	//
	// Path patterns accept either a directory anchor (e.g. cdc/, debezium/)
	// OR a filename suffix (*-connector.json, *.connector.json). The
	// directory check uses both "prefix/cdc/" AND "cdc/<something>" forms
	// because when a monorepo subrepo is indexed with its sub-directory as
	// the root (e.g. fleet entry root=services/cdc/), the file paths the
	// classifier receives are relative to that root and won't contain
	// "/cdc/" as a substring — they start with the filename instead.
	if strings.HasSuffix(lower, ".json") {
		dirAnchor := func(seg string) bool {
			return strings.Contains(lower, "/"+seg+"/") ||
				strings.HasPrefix(lower, seg+"/")
		}
		// #3628 area #16 — OpenAPI / Swagger spec files shipped as JSON. Route
		// the canonical spec filenames (openapi.json / swagger.json, plus the
		// *.openapi.json / *.swagger.json compound forms) to "json" so the
		// OpenAPI endpoint synthesizer can ingest them as endpoint ground-truth.
		// Narrow by filename (these basenames have no other reasonable meaning)
		// to avoid pulling in package.json / tsconfig.json / lockfiles. The
		// synthesizer still content-sniffs for `openapi`/`swagger` + `paths`
		// before emitting, so a stray match is a harmless no-op. The .yaml/.yml
		// spec forms already classify as "yaml" via the extension map.
		if b := path.Base(lower); b == "openapi.json" || b == "swagger.json" ||
			strings.HasSuffix(b, ".openapi.json") || strings.HasSuffix(b, ".swagger.json") {
			return "json"
		}
		// #3723 (epic #3628 area #21) — Ocelot (.NET) API-gateway config. The
		// conventional basename ocelot.json (and ocelot.<env>.json) has no other
		// meaning; route it to "json" so it reaches the Pass 2.5 detector, where
		// applyAPIGatewayRoutingEdges parses Routes[]→downstream service topology.
		if b := path.Base(lower); b == "ocelot.json" || apiGwOcelotJSONRe.MatchString(b) {
			return "json"
		}
		// Azure Bicep configuration file. bicepconfig.json declares the
		// moduleAliases.br / moduleAliases.ts registry aliases used by
		// `br/<alias>:…` / `ts/<alias>:…` module references; route it to the
		// bicep extractor so those aliases become resolvable config records.
		if path.Base(lower) == "bicepconfig.json" {
			return "bicep"
		}
		switch {
		case dirAnchor("cdc"),
			dirAnchor("debezium"),
			dirAnchor("kafka-connect"),
			dirAnchor("connectors"),
			strings.HasSuffix(lower, "-connector.json"),
			strings.HasSuffix(lower, ".connector.json"),
			strings.HasSuffix(lower, "-debezium.json"):
			return "json"
		}
	}

	// Issue #497 — exact-basename check before the extension lookup so that
	// files like "Package.swift" (which carry a recognised extension) can be
	// assigned a more specific language key ("swift_package") instead of the
	// generic one ("swift").
	base := path.Base(norm)
	if lang, ok := exactBasenameLanguageMap[base]; ok {
		return lang
	}

	ext := strings.ToLower(filepath.Ext(norm))
	if lang, ok := extensionLanguageMap[ext]; ok {
		return lang
	}
	// Fall back to basename matching for files like Dockerfile / Containerfile
	// that carry no extension.
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
