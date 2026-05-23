// Package fitness implements architectural fitness functions: user-defined
// rules loaded from .archigraph/fitness.yaml that are evaluated against a
// live graph and can gate CI builds.
//
// # Rule grammar
//
// Three rule kinds are supported:
//
//  1. forbid — a directed-edge pattern that must NOT appear:
//     forbid: 'SourceKind -> TargetKind'
//     forbid: 'SourceKind -[EDGE_KIND]-> TargetKind'
//
//  2. require — a structural invariant that must hold for every entity of
//     a given kind:
//     require: 'Kind has-no-outbound-IMPORTS'
//     require: 'Kind has-no-inbound-CALLS'
//
//  3. threshold — a numeric metric assertion:
//     threshold: 'import_cycles.max_size <= 3'
//     threshold: 'orphan_rate_pct < 20'
//     threshold: 'import_cycles.count == 0'
//
// # Severity
//
// Every rule carries an optional severity (error | warn | info). Default is
// "error". CI uses --strict to exit non-zero on any violation.
//
// # Exception list
//
// A rule may list entity IDs or kind:name patterns in an "except" list. Any
// entity matching an exception entry is excluded from that rule's evaluation.
package fitness

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// Config types (parsed from .archigraph/fitness.yaml)
// ─────────────────────────────────────────────────────────────────────────────

// Config is the root structure of .archigraph/fitness.yaml.
type Config struct {
	Rules []RuleConfig `yaml:"rules"`
}

// RuleConfig is a single rule definition as it appears in YAML.
type RuleConfig struct {
	// Name is a human-readable label shown in violation messages.
	Name string `yaml:"name"`

	// Forbid is a directed-edge pattern that must not exist in the graph.
	// Format: 'SourceKind -> TargetKind'  or
	//         'SourceKind -[EDGE_KIND]-> TargetKind'
	Forbid string `yaml:"forbid,omitempty"`

	// Require is a structural invariant that every entity of a given kind
	// must satisfy.
	// Format: 'Kind has-no-outbound-EDGE_KIND'  or
	//         'Kind has-no-inbound-EDGE_KIND'
	Require string `yaml:"require,omitempty"`

	// Threshold is a metric-comparison assertion.
	// Format: 'metric op value'  (e.g. 'import_cycles.max_size <= 3')
	Threshold string `yaml:"threshold,omitempty"`

	// Severity is the impact level of a violation: error | warn | info.
	// Defaults to "error".
	Severity string `yaml:"severity,omitempty"`

	// Except is a list of patterns exempted from this rule.
	// Each entry may be a bare entity ID or 'kind:name' glob.
	Except []string `yaml:"except,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Public result types
// ─────────────────────────────────────────────────────────────────────────────

// Violation is a single rule breach.
type Violation struct {
	// RuleName is the human-readable name of the violated rule.
	RuleName string `json:"rule_name"`
	// Severity is "error" | "warn" | "info".
	Severity string `json:"severity"`
	// Kind is "forbid" | "require" | "threshold".
	Kind string `json:"kind"`
	// Message describes the specific violation instance.
	Message string `json:"message"`
	// FromEntity / ToEntity are populated for edge-level violations.
	FromEntity *EntityRef `json:"from_entity,omitempty"`
	ToEntity   *EntityRef `json:"to_entity,omitempty"`
}

// EntityRef is a lightweight entity descriptor used inside Violation.
type EntityRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	SourceFile string `json:"source_file,omitempty"`
}

// RuleResult is the evaluation outcome for a single rule.
type RuleResult struct {
	Rule       RuleConfig  `json:"rule"`
	Violations []Violation `json:"violations"`
	// Passed is true when len(Violations) == 0.
	Passed bool `json:"passed"`
}

// EvalResult holds the full evaluation of a fitness config against one graph.
type EvalResult struct {
	// TotalRules is the number of rules evaluated.
	TotalRules int `json:"total_rules"`
	// PassedRules / FailedRules are counts.
	PassedRules int `json:"passed_rules"`
	FailedRules int `json:"failed_rules"`
	// ErrorCount / WarnCount / InfoCount count violations by severity.
	ErrorCount int `json:"error_count"`
	WarnCount  int `json:"warn_count"`
	InfoCount  int `json:"info_count"`
	// Results contains one entry per rule.
	Results []RuleResult `json:"results"`
	// SuggestedRules contains auto-detected rules the user might want to adopt.
	SuggestedRules []SuggestedRule `json:"suggested_rules,omitempty"`
}

// SuggestedRule is an auto-detected architectural pattern the user might
// want to codify as an explicit fitness rule.
type SuggestedRule struct {
	// Name is a short description of the suggested rule.
	Name string `json:"name"`
	// YAML is the ready-to-paste YAML snippet.
	YAML string `json:"yaml"`
	// Reason explains why the pattern was detected.
	Reason string `json:"reason"`
}

// HasFailures returns true when any error-severity rule failed.
func (r *EvalResult) HasFailures() bool {
	return r.ErrorCount > 0
}

// ─────────────────────────────────────────────────────────────────────────────
// Loading
// ─────────────────────────────────────────────────────────────────────────────

// DefaultConfigName is the conventional config file name.
const DefaultConfigName = "fitness.yaml"

// LoadConfig reads .archigraph/fitness.yaml from stateDir (the
// <repo>/.archigraph directory) and returns the parsed Config.
// Returns an empty Config (no rules) when the file does not exist.
func LoadConfig(stateDir string) (*Config, error) {
	path := stateDir + "/" + DefaultConfigName
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fitness: read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("fitness: parse %s: %w", path, err)
	}
	return &cfg, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Evaluation
// ─────────────────────────────────────────────────────────────────────────────

// Evaluate runs all rules in cfg against doc and returns an EvalResult.
// It never returns an error; parse/eval problems are surfaced as violations
// with severity "error".
func Evaluate(cfg *Config, doc *graph.Document) *EvalResult {
	res := &EvalResult{TotalRules: len(cfg.Rules)}

	// Build fast-lookup indexes.
	entityByID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		entityByID[doc.Entities[i].ID] = &doc.Entities[i]
	}

	for _, rc := range cfg.Rules {
		rr := evaluateRule(rc, doc, entityByID)
		res.Results = append(res.Results, rr)
		if rr.Passed {
			res.PassedRules++
		} else {
			res.FailedRules++
			for _, v := range rr.Violations {
				switch v.Severity {
				case "warn":
					res.WarnCount++
				case "info":
					res.InfoCount++
				default:
					res.ErrorCount++
				}
			}
		}
	}

	res.SuggestedRules = suggestRules(doc, entityByID)
	return res
}

func evaluateRule(rc RuleConfig, doc *graph.Document, entityByID map[string]*graph.Entity) RuleResult {
	sev := rc.Severity
	if sev == "" {
		sev = "error"
	}
	rr := RuleResult{Rule: rc, Passed: true}

	switch {
	case rc.Forbid != "":
		rr.Violations = evalForbid(rc, doc, entityByID, sev)
	case rc.Require != "":
		rr.Violations = evalRequire(rc, doc, entityByID, sev)
	case rc.Threshold != "":
		rr.Violations = evalThreshold(rc, doc, sev)
	default:
		rr.Violations = []Violation{{
			RuleName: rc.Name,
			Severity: "error",
			Kind:     "parse",
			Message:  "rule has no forbid / require / threshold clause",
		}}
	}

	if len(rr.Violations) > 0 {
		rr.Passed = false
	}
	return rr
}

// ─────────────────────────────────────────────────────────────────────────────
// forbid evaluation
// ─────────────────────────────────────────────────────────────────────────────

// forbidPattern captures the parsed forbid expression.
type forbidPattern struct {
	fromKindGlob string // may be "*"
	edgeKind     string // may be "" (any)
	toKindGlob   string // may be "*"
}

// parseForbid parses expressions like:
//
//	'SourceKind -> TargetKind'
//	'SourceKind -[EDGE]-> TargetKind'
//	'* -> DatabaseTable'
var reForbidWithEdge = regexp.MustCompile(`^(\S+)\s+-\[([^\]]+)\]->\s+(\S+)$`)
var reForbidSimple = regexp.MustCompile(`^(\S+)\s+->\s+(\S+)$`)

func parseForbid(expr string) (forbidPattern, error) {
	expr = strings.TrimSpace(expr)
	if m := reForbidWithEdge.FindStringSubmatch(expr); m != nil {
		return forbidPattern{fromKindGlob: m[1], edgeKind: strings.ToUpper(m[2]), toKindGlob: m[3]}, nil
	}
	if m := reForbidSimple.FindStringSubmatch(expr); m != nil {
		return forbidPattern{fromKindGlob: m[1], toKindGlob: m[2]}, nil
	}
	return forbidPattern{}, fmt.Errorf("cannot parse forbid expression %q; expected 'SourceKind -> TargetKind' or 'SourceKind -[EDGE]-> TargetKind'", expr)
}

func kindMatches(entityKind, glob string) bool {
	if glob == "*" {
		return true
	}
	// Case-insensitive substring/prefix match: 'Model' matches 'DataModel', etc.
	return strings.EqualFold(entityKind, glob) ||
		strings.HasSuffix(strings.ToLower(entityKind), strings.ToLower("."+glob)) ||
		strings.HasPrefix(strings.ToLower(entityKind), strings.ToLower(glob+"."))
}

func evalForbid(rc RuleConfig, doc *graph.Document, entityByID map[string]*graph.Entity, sev string) []Violation {
	pat, err := parseForbid(rc.Forbid)
	if err != nil {
		return []Violation{{RuleName: rc.Name, Severity: "error", Kind: "parse", Message: err.Error()}}
	}

	var violations []Violation
	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		if pat.edgeKind != "" && !strings.EqualFold(rel.Kind, pat.edgeKind) {
			continue
		}
		fromEnt := entityByID[rel.FromID]
		toEnt := entityByID[rel.ToID]
		if fromEnt == nil || toEnt == nil {
			continue
		}
		if !kindMatches(fromEnt.Kind, pat.fromKindGlob) {
			continue
		}
		if !kindMatches(toEnt.Kind, pat.toKindGlob) {
			continue
		}
		if isExcepted(fromEnt, rc.Except) || isExcepted(toEnt, rc.Except) {
			continue
		}
		violations = append(violations, Violation{
			RuleName:   rc.Name,
			Severity:   sev,
			Kind:       "forbid",
			Message:    fmt.Sprintf("forbidden edge: %s(%s) -[%s]-> %s(%s)", fromEnt.Name, fromEnt.Kind, rel.Kind, toEnt.Name, toEnt.Kind),
			FromEntity: entRef(fromEnt),
			ToEntity:   entRef(toEnt),
		})
	}
	return violations
}

// ─────────────────────────────────────────────────────────────────────────────
// require evaluation
// ─────────────────────────────────────────────────────────────────────────────

// requirePattern captures the parsed require expression.
type requirePattern struct {
	kindGlob  string
	direction string // "outbound" | "inbound"
	edgeKind  string
}

// parseRequire parses expressions like:
//
//	'Kind has-no-outbound-IMPORTS'
//	'Kind has-no-inbound-CALLS'
var reRequire = regexp.MustCompile(`^(\S+)\s+has-no-(outbound|inbound)-(\S+)$`)

func parseRequire(expr string) (requirePattern, error) {
	expr = strings.TrimSpace(expr)
	m := reRequire.FindStringSubmatch(expr)
	if m == nil {
		return requirePattern{}, fmt.Errorf("cannot parse require expression %q; expected 'Kind has-no-outbound-EDGE_KIND' or 'Kind has-no-inbound-EDGE_KIND'", expr)
	}
	return requirePattern{kindGlob: m[1], direction: m[2], edgeKind: strings.ToUpper(m[3])}, nil
}

func evalRequire(rc RuleConfig, doc *graph.Document, entityByID map[string]*graph.Entity, sev string) []Violation {
	pat, err := parseRequire(rc.Require)
	if err != nil {
		return []Violation{{RuleName: rc.Name, Severity: "error", Kind: "parse", Message: err.Error()}}
	}

	// Build a set of entity IDs that match the kind glob.
	matchedIDs := make(map[string]bool)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if kindMatches(e.Kind, pat.kindGlob) && !isExcepted(e, rc.Except) {
			matchedIDs[e.ID] = true
		}
	}

	var violations []Violation
	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		if !strings.EqualFold(rel.Kind, pat.edgeKind) {
			continue
		}
		var subjectID, peerID string
		if pat.direction == "outbound" {
			subjectID = rel.FromID
			peerID = rel.ToID
		} else {
			subjectID = rel.ToID
			peerID = rel.FromID
		}
		if !matchedIDs[subjectID] {
			continue
		}
		subjectEnt := entityByID[subjectID]
		peerEnt := entityByID[peerID]
		if subjectEnt == nil {
			continue
		}
		peerName := peerID
		if peerEnt != nil {
			peerName = fmt.Sprintf("%s(%s)", peerEnt.Name, peerEnt.Kind)
		}
		var msg string
		if pat.direction == "outbound" {
			msg = fmt.Sprintf("%s(%s) must have no outbound %s edges, but found -> %s", subjectEnt.Name, subjectEnt.Kind, pat.edgeKind, peerName)
		} else {
			msg = fmt.Sprintf("%s(%s) must have no inbound %s edges, but found %s ->", subjectEnt.Name, subjectEnt.Kind, pat.edgeKind, peerName)
		}
		violations = append(violations, Violation{
			RuleName:   rc.Name,
			Severity:   sev,
			Kind:       "require",
			Message:    msg,
			FromEntity: entRef(subjectEnt),
		})
	}
	return violations
}

// ─────────────────────────────────────────────────────────────────────────────
// threshold evaluation
// ─────────────────────────────────────────────────────────────────────────────

var reThreshold = regexp.MustCompile(`^(\S+)\s*(<=|>=|<|>|==|!=)\s*(\S+)$`)

func evalThreshold(rc RuleConfig, doc *graph.Document, sev string) []Violation {
	expr := strings.TrimSpace(rc.Threshold)
	m := reThreshold.FindStringSubmatch(expr)
	if m == nil {
		return []Violation{{
			RuleName: rc.Name,
			Severity: "error",
			Kind:     "parse",
			Message:  fmt.Sprintf("cannot parse threshold %q; expected 'metric op value' (e.g. 'import_cycles.max_size <= 3')", expr),
		}}
	}
	metric, op, rawTarget := m[1], m[2], m[3]

	target, err := strconv.ParseFloat(rawTarget, 64)
	if err != nil {
		return []Violation{{
			RuleName: rc.Name,
			Severity: "error",
			Kind:     "parse",
			Message:  fmt.Sprintf("threshold target %q is not a number", rawTarget),
		}}
	}

	value, resolveErr := resolveMetric(metric, doc)
	if resolveErr != nil {
		return []Violation{{
			RuleName: rc.Name,
			Severity: "error",
			Kind:     "threshold",
			Message:  resolveErr.Error(),
		}}
	}

	if !evalOp(value, op, target) {
		return []Violation{{
			RuleName: rc.Name,
			Severity: sev,
			Kind:     "threshold",
			Message:  fmt.Sprintf("threshold violated: %s = %.4g %s %.4g is false", metric, value, op, target),
		}}
	}
	return nil
}

// resolveMetric returns the current numeric value of a named metric.
// Supported metrics:
//
//	import_cycles.count       — number of distinct import cycles
//	import_cycles.max_size    — size of the largest cycle
//	orphan_rate_pct           — % of entities with no inbound edges
//	entity_count              — total number of entities
//	relationship_count        — total number of relationships
func resolveMetric(metric string, doc *graph.Document) (float64, error) {
	switch metric {
	case "entity_count":
		return float64(len(doc.Entities)), nil
	case "relationship_count":
		return float64(len(doc.Relationships)), nil
	case "orphan_rate_pct":
		return computeOrphanRatePct(doc), nil
	case "import_cycles.count", "import_cycles.max_size":
		cycles := graph.FindImportCycles(doc.Entities, doc.Relationships, nil)
		if metric == "import_cycles.count" {
			return float64(len(cycles)), nil
		}
		max := 0
		for _, c := range cycles {
			if c.Size > max {
				max = c.Size
			}
		}
		return float64(max), nil
	}
	return 0, fmt.Errorf("unknown metric %q; supported: import_cycles.count, import_cycles.max_size, orphan_rate_pct, entity_count, relationship_count", metric)
}

func evalOp(v float64, op string, target float64) bool {
	switch op {
	case "<=":
		return v <= target
	case ">=":
		return v >= target
	case "<":
		return v < target
	case ">":
		return v > target
	case "==":
		return v == target
	case "!=":
		return v != target
	}
	return false
}

func computeOrphanRatePct(doc *graph.Document) float64 {
	if len(doc.Entities) == 0 {
		return 0
	}
	hasInbound := make(map[string]bool, len(doc.Entities))
	for i := range doc.Relationships {
		hasInbound[doc.Relationships[i].ToID] = true
	}
	orphans := 0
	for i := range doc.Entities {
		if !hasInbound[doc.Entities[i].ID] {
			orphans++
		}
	}
	return float64(orphans) / float64(len(doc.Entities)) * 100
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func entRef(e *graph.Entity) *EntityRef {
	if e == nil {
		return nil
	}
	return &EntityRef{ID: e.ID, Name: e.Name, Kind: e.Kind, SourceFile: e.SourceFile}
}

// isExcepted returns true when e matches any entry in the exception list.
// An exception entry can be:
//   - a bare entity ID (exact match)
//   - 'kind:name' where kind / name may each be '*' for wildcard
func isExcepted(e *graph.Entity, except []string) bool {
	for _, exc := range except {
		if exc == e.ID {
			return true
		}
		if strings.Contains(exc, ":") {
			parts := strings.SplitN(exc, ":", 2)
			kindPat, namePat := parts[0], parts[1]
			kindOK := kindPat == "*" || strings.EqualFold(kindPat, e.Kind)
			nameOK := namePat == "*" || strings.EqualFold(namePat, e.Name) || strings.Contains(strings.ToLower(e.Name), strings.ToLower(namePat))
			if kindOK && nameOK {
				return true
			}
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Auto-suggestion engine
// ─────────────────────────────────────────────────────────────────────────────

// suggestRules looks for common architectural anti-patterns in the graph and
// returns suggested fitness rules the user might want to adopt.
func suggestRules(doc *graph.Document, entityByID map[string]*graph.Entity) []SuggestedRule {
	var suggestions []SuggestedRule

	// Suggestion 1: if any http_endpoint_definition entity directly imports a
	// DatabaseTable or Model entity, suggest a "no DB in handlers" rule.
	handlerHitsDB := false
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != "IMPORTS" && r.Kind != "CALLS" && r.Kind != "USES" {
			continue
		}
		from := entityByID[r.FromID]
		to := entityByID[r.ToID]
		if from == nil || to == nil {
			continue
		}
		if isEndpointOrHandler(from.Kind) && isDatabaseKind(to.Kind) {
			handlerHitsDB = true
			break
		}
	}
	if handlerHitsDB {
		suggestions = append(suggestions, SuggestedRule{
			Name:   "No DB access from HTTP handlers",
			YAML:   "- name: 'No DB access from HTTP handlers'\n  forbid: 'http_endpoint_definition -> DatabaseTable'\n  severity: error",
			Reason: "Detected direct edges from HTTP handler entities to database entities — consider adding a service/repository layer.",
		})
	}

	// Suggestion 2: if import cycles exist, suggest a threshold rule.
	cycles := graph.FindImportCycles(doc.Entities, doc.Relationships, nil)
	if len(cycles) > 0 {
		maxSize := 0
		for _, c := range cycles {
			if c.Size > maxSize {
				maxSize = c.Size
			}
		}
		suggestions = append(suggestions, SuggestedRule{
			Name:   "No import cycles",
			YAML:   fmt.Sprintf("- name: 'No import cycles'\n  threshold: 'import_cycles.count == 0'\n  severity: error"),
			Reason: fmt.Sprintf("Found %d import cycle(s) (largest: %d members). Consider banning new cycles via a threshold rule.", len(cycles), maxSize),
		})
		if maxSize > 3 {
			suggestions = append(suggestions, SuggestedRule{
				Name:   "Cap cycle size at 3",
				YAML:   "- name: 'Max cycle size 3'\n  threshold: 'import_cycles.max_size <= 3'\n  severity: warn",
				Reason: fmt.Sprintf("Largest import cycle has %d members — smaller cycles are easier to break.", maxSize),
			})
		}
	}

	return suggestions
}

func isEndpointOrHandler(kind string) bool {
	lower := strings.ToLower(kind)
	return strings.Contains(lower, "endpoint") ||
		strings.Contains(lower, "handler") ||
		strings.Contains(lower, "controller") ||
		strings.Contains(lower, "view") ||
		strings.Contains(lower, "route")
}

func isDatabaseKind(kind string) bool {
	lower := strings.ToLower(kind)
	return strings.Contains(lower, "database") ||
		strings.Contains(lower, "table") ||
		strings.Contains(lower, "repository") ||
		strings.Contains(lower, "dao") ||
		strings.Contains(lower, "model") ||
		strings.Contains(lower, "entity")
}
