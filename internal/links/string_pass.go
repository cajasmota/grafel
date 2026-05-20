package links

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// extractionCategory identifies a string-pattern bucket.
type extractionCategory string

const (
	catWebhookPath    extractionCategory = "webhook_path"
	catHTTPPath       extractionCategory = "http_path"
	catS3URI          extractionCategory = "s3_uri"
	catRedisKey       extractionCategory = "redis_key"
	catKafkaTopic     extractionCategory = "kafka_topic"
	catNATSSubject    extractionCategory = "nats_subject"
	catFeatureFlag    extractionCategory = "feature_flag"
	catSQSARN         extractionCategory = "sqs_arn"
	catSQSURL         extractionCategory = "sqs_url"
	catSNSARN         extractionCategory = "sns_arn"
	catLambdaARN      extractionCategory = "lambda_arn"
	catEventbridgeARN extractionCategory = "eventbridge_arn"
)

// channelFor maps a category to the channel field on emitted links.
func channelFor(cat extractionCategory) string {
	switch cat {
	case catWebhookPath, catHTTPPath:
		return "http"
	case catS3URI:
		return "s3"
	case catRedisKey:
		return "redis_key"
	case catKafkaTopic:
		return "kafka_topic"
	case catNATSSubject:
		return "nats_subject"
	case catFeatureFlag:
		return "feature_flag"
	case catSQSARN, catSQSURL:
		return "sqs"
	case catSNSARN:
		return "sns"
	case catLambdaARN:
		return "lambda"
	case catEventbridgeARN:
		return "eventbridge"
	}
	return string(cat)
}

// patternRule is one entry in the catalog.
type patternRule struct {
	Cat extractionCategory
	Re  *regexp.Regexp
	// extra is a post-match validator. Returns true if the match should
	// be kept. Nil means "keep all".
	extra func(string) bool
}

// isXMLElementRef returns true if the string looks like a Word XML element
// reference (e.g., "./w:tblBorders", "./w:tcBorders"). Such patterns should
// not be classified as HTTP paths. Issue #958.
func isXMLElementRef(s string) bool {
	if !strings.Contains(s, ":") || strings.Contains(s, "://") {
		return false
	}

	// Find the first colon that is not part of ://
	colonIdx := strings.Index(s, ":")
	if colonIdx < 0 {
		return false
	}

	// Check if this colon is part of :// (protocol separator)
	if colonIdx+2 < len(s) && s[colonIdx+1:colonIdx+3] == "//" {
		// This is a protocol (http://, https://, etc.), not an XML namespace
		return false
	}

	// Now check if the part before the colon looks like an XML namespace prefix.
	// We need to extract the last path segment before the colon.
	beforeColon := s[:colonIdx]

	// For paths like "/api/v1/w:something", extract "w"
	// For refs like "./w:something", extract "w"
	lastSlash := strings.LastIndexByte(beforeColon, '/')
	namespace := beforeColon
	if lastSlash >= 0 {
		namespace = beforeColon[lastSlash+1:]
	}

	// If namespace is empty or too long, not XML
	if len(namespace) == 0 || len(namespace) > 4 {
		return false
	}

	// Check if namespace is purely alphabetic (like 'w', 'xml', 'xsl', etc.)
	// These are typical XML namespace prefixes
	for _, c := range namespace {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}

	// At this point, we have a short alphabetic prefix followed by a colon.
	// Check that what follows the colon is a valid XML element name
	// (starts with letter or underscore)
	afterColon := s[colonIdx+1:]
	if len(afterColon) == 0 {
		return false
	}

	firstChar := afterColon[0]
	if !((firstChar >= 'a' && firstChar <= 'z') ||
		(firstChar >= 'A' && firstChar <= 'Z') ||
		firstChar == '_') {
		return false
	}

	// This looks like an XML element reference
	return true
}

// kafkaTLDBlock filters out hostnames misidentified as kafka topics.
var kafkaTLDBlock = map[string]bool{
	"com": true, "net": true, "org": true, "io": true, "co": true,
	"json": true, "yaml": true, "yml": true, "go": true, "py": true,
	"ts": true, "js": true, "jsx": true, "tsx": true, "md": true,
	"txt": true, "html": true, "css": true, "xml": true,
}

func kafkaExtra(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	last := parts[len(parts)-1]
	if kafkaTLDBlock[last] {
		return false
	}
	return true
}

func natsExtra(s string) bool {
	if !strings.ContainsAny(s, "*>") {
		return false
	}
	return kafkaExtra(strings.TrimRight(s, "*>"))
}

// patternCatalog is shared across calls. Compiled once.
var patternCatalog = []patternRule{
	{Cat: catWebhookPath, Re: regexp.MustCompile(`^/(webhooks?|hooks)/[a-zA-Z0-9_\-/.]+$`)},
	{Cat: catHTTPPath, Re: regexp.MustCompile(`^/(api|v\d+|public|internal)(/[a-zA-Z0-9_\-{}.<>:%]+)+/?$`), extra: func(s string) bool {
		return !isXMLElementRef(s)
	}},
	{Cat: catS3URI, Re: regexp.MustCompile(`^s3://[a-z0-9.\-]+(/\S*)?$`)},
	// AWS ARNs and the SQS URL precede the generic redis-key pattern,
	// because that pattern is broad enough to also match colon-separated
	// ARN strings.
	{Cat: catSQSARN, Re: regexp.MustCompile(`^arn:aws:sqs:[a-z0-9-]+:\d+:[a-zA-Z0-9_-]+$`)},
	{Cat: catSQSURL, Re: regexp.MustCompile(`^https://sqs\.[a-z0-9-]+\.amazonaws\.com/\d+/[a-zA-Z0-9_-]+$`)},
	{Cat: catSNSARN, Re: regexp.MustCompile(`^arn:aws:sns:[a-z0-9-]+:\d+:[a-zA-Z0-9_.-]+$`)},
	{Cat: catLambdaARN, Re: regexp.MustCompile(`^arn:aws:lambda:[a-z0-9-]+:\d+:function:[a-zA-Z0-9_.-]+$`)},
	{Cat: catEventbridgeARN, Re: regexp.MustCompile(`^arn:aws:events:[a-z0-9-]+:\d+:[a-zA-Z0-9_/.\-]+$`)},
	{Cat: catRedisKey, Re: regexp.MustCompile(`^[a-z_][a-z0-9_]*(:[a-zA-Z0-9_*{}.\-$%]+){1,5}$`)},
	{Cat: catKafkaTopic, Re: regexp.MustCompile(`^[a-z][a-z0-9._\-]+(\.[a-z0-9._\-]+){1,5}$`), extra: kafkaExtra},
	{Cat: catNATSSubject, Re: regexp.MustCompile(`^[a-z][a-z0-9._\-]+(\.[a-z0-9._\-*>]+){1,5}$`), extra: natsExtra},
	{Cat: catFeatureFlag, Re: regexp.MustCompile(`^(feature|ff|flag)_[a-z0-9_]{2,}$`)},
}

// stringLiteral is the regex used by extractFile to find quoted string
// literals in source files. We deliberately keep this simple — we trade
// a small amount of precision for a single regex that works across most
// languages. The catalog rules above provide the precision.
var stringLiteralRe = regexp.MustCompile(`"([^"\\]{1,256})"|'([^'\\]{1,256})'|` + "`([^`\\\\]{1,256})`")

// Extraction is one classified literal found in a file.
type Extraction struct {
	Category extractionCategory `json:"category"`
	Value    string             `json:"value"`
	File     string             `json:"file"`
	Line     int                `json:"line"`
}

// classify runs the catalog against `s` and returns the first matching
// category, or empty if none matches.
func classify(s string) extractionCategory {
	if s == "" || len(s) > 1024 {
		return ""
	}
	for _, p := range patternCatalog {
		if p.Re.MatchString(s) {
			if p.extra != nil && !p.extra(s) {
				continue
			}
			return p.Cat
		}
	}
	return ""
}

// scanCacheEntry is the shape of <cache>/<file-sha>.json.
type scanCacheEntry struct {
	File   string       `json:"file"`
	Mtime  int64        `json:"mtime_ns"`
	Size   int64        `json:"size"`
	Values []Extraction `json:"values"`
}

// scanFile reads a single file, classifies its string literals, and
// caches the result. cacheDir is the per-repo directory; pass "" to
// disable caching.
func scanFile(absPath, relPath, cacheDir string) ([]Extraction, error) {
	fi, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if fi.IsDir() {
		return nil, nil
	}

	cacheFile := ""
	if cacheDir != "" {
		h := sha256.Sum256([]byte(absPath))
		cacheFile = filepath.Join(cacheDir, hex.EncodeToString(h[:])[:32]+".json")
		if b, err := os.ReadFile(cacheFile); err == nil {
			var e scanCacheEntry
			if json.Unmarshal(b, &e) == nil {
				if e.Mtime == fi.ModTime().UnixNano() && e.Size == fi.Size() {
					return e.Values, nil
				}
			}
		}
	}

	body, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	if len(body) > 4*1024*1024 {
		// Skip very large files.
		return nil, nil
	}
	var out []Extraction
	for _, m := range stringLiteralRe.FindAllSubmatchIndex(body, -1) {
		var raw string
		for i := 1; i <= 3; i++ {
			if m[2*i] != -1 {
				raw = string(body[m[2*i]:m[2*i+1]])
				break
			}
		}
		cat := classify(raw)
		if cat == "" {
			continue
		}
		line := 1 + bytesCount(body[:m[0]], '\n')
		out = append(out, Extraction{Category: cat, Value: raw, File: relPath, Line: line})
	}

	if cacheFile != "" {
		_ = os.MkdirAll(filepath.Dir(cacheFile), 0o755)
		entry := scanCacheEntry{File: absPath, Mtime: fi.ModTime().UnixNano(), Size: fi.Size(), Values: out}
		if b, err := json.Marshal(entry); err == nil {
			tmp := cacheFile + ".tmp"
			if err := os.WriteFile(tmp, b, 0o644); err == nil {
				_ = os.Rename(tmp, cacheFile)
			}
		}
	}
	return out, nil
}

func bytesCount(b []byte, c byte) int {
	n := 0
	for _, x := range b {
		if x == c {
			n++
		}
	}
	return n
}

// scanRepo walks the repo's source files and returns extractions.
// fileRoot is the repo's root directory (absolute). filesFromGraph
// is the unique list of source files referenced by the graph; we scan
// only those files to keep work bounded.
func scanRepo(fileRoot string, filesFromGraph []string, cacheDir string) ([]Extraction, error) {
	if fileRoot == "" {
		return nil, nil
	}
	var out []Extraction
	seen := map[string]bool{}
	for _, rel := range filesFromGraph {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		abs := rel
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(fileRoot, rel)
		}
		ex, err := scanFile(abs, rel, cacheDir)
		if err != nil {
			return nil, err
		}
		out = append(out, ex...)
	}
	return out, nil
}

// thresholds for P3. The link threshold sits at the boundary between the
// "ambiguous" Redis/feature-flag categories (≤0.4) and the more specific
// HTTP/path/AWS categories (≥0.45). Anything in the upper half of the P3
// band is emitted as a link; the lower half lands as a candidate.
const (
	stringLinkThreshold      = 0.45
	stringCandidateThreshold = 0.30
	stringEmissionCap        = 6
)

// runStringPass implements P3.
func runStringPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "string"}
	if len(graphs) < 2 {
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodString), nil, rejects)
		if err != nil {
			return res, err
		}
		_, _, err = replaceByMethod(paths.Candidates, newMethodSet(MethodString), nil, rejects)
		return res, err
	}

	// (category, value) → repo → []hits (with file+line)
	type hit struct {
		repo  string
		file  string
		line  int
		entID string
	}
	combo := map[string]map[string][]hit{}

	for _, g := range graphs {
		// File → first entity-id for that file (best-effort source endpoint).
		entityForFile := map[string]string{}
		var files []string
		for _, e := range g.Entities {
			if e.SourceFile == "" {
				continue
			}
			if _, ok := entityForFile[e.SourceFile]; !ok {
				entityForFile[e.SourceFile] = e.ID
				files = append(files, e.SourceFile)
			}
		}
		repoCacheDir := ""
		if paths.ScanCache != "" {
			repoCacheDir = filepath.Join(paths.ScanCache, g.Repo)
		}
		exs, err := scanRepo(g.FileRoot, files, repoCacheDir)
		if err != nil {
			return res, fmt.Errorf("scan repo %s: %w", g.Repo, err)
		}
		for _, ex := range exs {
			key := string(ex.Category) + "|" + ex.Value
			if combo[key] == nil {
				combo[key] = map[string][]hit{}
			}
			eid := entityForFile[ex.File]
			combo[key][g.Repo] = append(combo[key][g.Repo], hit{
				repo:  g.Repo,
				file:  ex.File,
				line:  ex.Line,
				entID: eid,
			})
		}
	}

	now := discoveredAt()
	var freshLinks, freshCands []Link

	keys := make([]string, 0, len(combo))
	for k := range combo {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// seenPair dedupes (src,tgt) across the whole pass so two distinct
	// matching string values that happen to live in the same pair of
	// repos don't produce duplicate emissions. Keeps total work
	// O(matched pairs) rather than O(values × repo_pairs).
	seenPair := map[string]bool{}

	for _, key := range keys {
		repoMap := combo[key]
		if len(repoMap) < 2 {
			continue
		}
		// key = category|value
		parts := strings.SplitN(key, "|", 2)
		cat := extractionCategory(parts[0])
		val := parts[1]

		repoNames := make([]string, 0, len(repoMap))
		for r := range repoMap {
			repoNames = append(repoNames, r)
		}
		sort.Strings(repoNames)

		emitted := 0
		for i := 0; i < len(repoNames) && emitted < stringEmissionCap; i++ {
			for j := i + 1; j < len(repoNames) && emitted < stringEmissionCap; j++ {
				ra, rb := repoNames[i], repoNames[j]
				if ra == rb {
					continue
				}
				ha := repoMap[ra][0]
				hb := repoMap[rb][0]
				if ha.entID == "" || hb.entID == "" {
					continue
				}
				conf := ScoreString(cat)
				if conf < stringCandidateThreshold {
					continue
				}
				sa := entityKey(ra, ha.entID)
				sb := entityKey(rb, hb.entID)
				src, tgt := orderEndpoints(sa, sb)
				pairKey := src + "|" + tgt + "|" + string(cat)
				if seenPair[pairKey] {
					continue
				}
				seenPair[pairKey] = true
				ch := channelFor(cat)
				link := Link{
					ID:           MakeID(src, tgt, MethodString),
					Source:       src,
					Target:       tgt,
					Relation:     RelationStringMatch,
					Method:       MethodString,
					Confidence:   conf,
					Channel:      strPtr(ch),
					Identifier:   strPtr(val),
					DiscoveredAt: now,
					SourceLocations: [][]string{
						{fmt.Sprintf("%s:%d", ha.file, ha.line)},
						{fmt.Sprintf("%s:%d", hb.file, hb.line)},
					},
				}
				if conf >= stringLinkThreshold {
					freshLinks = append(freshLinks, link)
				} else {
					link.Reason = "string match below threshold"
					freshCands = append(freshCands, link)
				}
				emitted++
			}
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodString), freshLinks, rejects)
	if err != nil {
		return res, err
	}
	cAdded, cSkipped, err := replaceByMethod(paths.Candidates, newMethodSet(MethodString), freshCands, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Candidates = cAdded
	res.Skipped = skipped + cSkipped
	return res, nil
}
