// Payload-shape drift pass (#2770 Phase 2A substrate).
//
// This pass cross-references the producer- and consumer-side payload
// shapes extracted by per-language sniffers (registered under
// internal/substrate/payload_shapes_*.go) against the cross-repo HTTP
// route ↔ fetch matcher's emitted links. For every linked endpoint we
// compute the symmetric difference between the producer's request /
// response shape and the consumer's request / response shape and emit
// SchemaDrift findings.
//
// Pipeline:
//
//  1. Sniff every source file once, bucketing PayloadShape records by
//     (repo, file, function-name, direction, side). The substrate
//     PayloadShapeSnifferFor("<lang>") dispatcher returns the
//     registered sniffer; languages with no sniffer registered are
//     silently skipped.
//
//  2. Bind each (repo, file, fn) triple to its graph entity ID via
//     the same effectBinder used by the effect-propagation pass.
//
//  3. Re-read the on-disk cross-repo links file emitted by the HTTP
//     pass (MethodHTTP entries only). For each link, look up the
//     producer-side handler shape and the consumer-side caller shape
//     and compare. The diff drives one SchemaDrift entry per
//     (endpoint, direction).
//
//  4. Write findings to a sidecar <group>-links-payload-drift.json
//     document read by the new MCP tool archigraph_payload_drift.
//
// Confidence model: each drift finding inherits the MIN of the two
// shape confidences and a per-finding severity tag computed from the
// number and kind of mismatched fields.
package links

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/substrate"
)

// MethodPayloadDrift identifies sidecar artefacts produced by this
// pass. Method-segregated so re-runs touch only this pass's output.
const MethodPayloadDrift = "payload_drift"

// driftSidecarSuffix is the suffix appended to paths.Links (trimmed
// of its .json) to derive the on-disk drift sidecar path. Mirrors the
// effect-propagation pass's convention.
const driftSidecarSuffix = "-payload-drift.json"

// DriftSeverity classifies a finding for sort ordering in the MCP
// tool. Higher severity surfaces first.
type DriftSeverity string

const (
	// DriftSeverityHigh — consumer constructs a field the producer
	// has no observable read for, OR producer writes a response
	// field the consumer never destructures. Both shapes are real
	// (non-empty) and confidence on both sides is ≥ 0.8.
	DriftSeverityHigh DriftSeverity = "high"

	// DriftSeverityMedium — same as high but at least one side is
	// confidence < 0.8 (typically: handler reads spread across many
	// statements rather than a single literal), so a missed branch
	// could explain the mismatch.
	DriftSeverityMedium DriftSeverity = "medium"

	// DriftSeverityLow — informational: one side has no observable
	// shape at all (cross-file DTO, dynamic body assembly). Reported
	// so callers can audit blind spots; not a confirmed drift.
	DriftSeverityLow DriftSeverity = "low"
)

// SchemaDrift is one finding: a producer/consumer shape mismatch on
// one direction of one endpoint.
type SchemaDrift struct {
	EndpointID         string        `json:"endpoint_id"`
	EndpointName       string        `json:"endpoint_name"`
	Direction          string        `json:"direction"` // "request" | "response"
	ProducerRepo       string        `json:"producer_repo,omitempty"`
	ConsumerRepo       string        `json:"consumer_repo,omitempty"`
	ProducerEntity     string        `json:"producer_entity,omitempty"`
	ConsumerEntity     string        `json:"consumer_entity,omitempty"`
	ProducerFunction   string        `json:"producer_function,omitempty"`
	ConsumerFunction   string        `json:"consumer_function,omitempty"`
	ProducerFile       string        `json:"producer_file,omitempty"`
	ConsumerFile       string        `json:"consumer_file,omitempty"`
	ProducerFields     []string      `json:"producer_fields,omitempty"`
	ConsumerFields     []string      `json:"consumer_fields,omitempty"`
	MissingInProducer  []string      `json:"missing_in_producer,omitempty"`
	MissingInConsumer  []string      `json:"missing_in_consumer,omitempty"`
	Severity           DriftSeverity `json:"severity"`
	Confidence         float64       `json:"confidence"`
	ProducerConfidence float64       `json:"producer_confidence,omitempty"`
	ConsumerConfidence float64       `json:"consumer_confidence,omitempty"`
	Explanation        string        `json:"explanation"`
}

// payloadDriftDocument is the on-disk shape of the sidecar JSON.
type payloadDriftDocument struct {
	Version int           `json:"version"`
	Method  string        `json:"method"`
	Group   string        `json:"group"`
	Total   int           `json:"total"`
	Findings []SchemaDrift `json:"findings"`
}

// shapeKey identifies one shape bucket — the unique attribution of a
// PayloadShape to a function in a file in a repo.
type shapeKey struct {
	repo string
	file string
	fn   string
}

// shapeBucket holds the producer- and consumer-side shapes for one
// (repo, file, fn) triple, indexed by direction. A single function
// may carry both request and response shapes for both sides — e.g.
// an Express handler reads req.body.X (producer request) and writes
// res.json({Y}) (producer response).
type shapeBucket struct {
	producerRequest  *substrate.PayloadShape
	producerResponse *substrate.PayloadShape
	consumerRequest  *substrate.PayloadShape
	consumerResponse *substrate.PayloadShape
}

// runPayloadDriftPass is the entry point invoked from RunAllPasses
// after the HTTP cross-repo matcher has emitted MethodHTTP entries.
func runPayloadDriftPass(group string, graphs []repoGraph, paths Paths) (PassResult, error) {
	res := PassResult{Pass: "payload_drift"}

	// 1. Sniff every source file once.
	buckets, scanned := sniffPayloadShapes(graphs)
	if scanned == 0 {
		// No T1 source on disk — pass is a no-op; clear any prior
		// sidecar so stale findings don't linger.
		_ = clearDriftSidecar(paths)
		return res, nil
	}

	// 2. Bind (repo, file, fn) → entity ID using the effectBinder.
	binder := newEffectBinder(graphs)
	shapesByEntity := map[string]*shapeBucket{}
	for k, b := range buckets {
		eid := binder.lookup(k.repo, k.file, k.fn)
		if eid == "" {
			continue
		}
		fullID := entityKey(k.repo, eid)
		existing := shapesByEntity[fullID]
		if existing == nil {
			existing = &shapeBucket{}
			shapesByEntity[fullID] = existing
		}
		mergeBucket(existing, b)
	}

	// 3. Read the cross-repo HTTP links file to discover endpoints
	// linked across repos and the producer-handler / consumer-caller
	// entity IDs on each side.
	httpLinks, err := loadHTTPLinks(paths.Links)
	if err != nil {
		return res, err
	}
	// Also build a lookup of every entity ID → (repo, file, name,
	// kind) for output annotation.
	entIndex := buildEntityIndex(graphs)

	// 4. Compute drift findings, deduped by (endpoint, direction,
	// producer entity, consumer entity).
	seen := map[string]bool{}
	var findings []SchemaDrift
	for _, l := range httpLinks {
		producerEntity := l.Target
		consumerEntity := l.Source
		// The HTTP link's target/source are the synthetic
		// http_endpoint_* entities. Resolve through the IMPLEMENTS /
		// CALLS edges to land on the actual handler / caller entity
		// whose function carries the payload shape.
		producerHandlers := resolveProducerHandlers(producerEntity, entIndex)
		consumerCallers := resolveConsumerCallers(consumerEntity, entIndex)
		if len(producerHandlers) == 0 && len(consumerCallers) == 0 {
			continue
		}

		for _, p := range producerHandlers {
			for _, c := range consumerCallers {
				for _, dir := range []substrate.PayloadDirection{
					substrate.PayloadDirectionRequest,
					substrate.PayloadDirectionResponse,
				} {
					prodShape, consShape := pickShapes(shapesByEntity, p, c, dir)
					f, ok := buildDriftFinding(l, p, c, dir, prodShape, consShape, entIndex)
					if !ok {
						continue
					}
					key := f.EndpointID + "|" + f.Direction + "|" + f.ProducerEntity + "|" + f.ConsumerEntity
					if seen[key] {
						continue
					}
					seen[key] = true
					findings = append(findings, f)
				}
			}
		}
	}

	// Sort: severity desc, then endpoint name, then direction.
	sort.SliceStable(findings, func(i, j int) bool {
		si, sj := severityRank(findings[i].Severity), severityRank(findings[j].Severity)
		if si != sj {
			return si > sj
		}
		if findings[i].EndpointName != findings[j].EndpointName {
			return findings[i].EndpointName < findings[j].EndpointName
		}
		return findings[i].Direction < findings[j].Direction
	})

	doc := payloadDriftDocument{
		Version:  1,
		Method:   MethodPayloadDrift,
		Group:    group,
		Total:    len(findings),
		Findings: findings,
	}
	if err := writeDriftSidecar(paths, doc); err != nil {
		return res, err
	}
	res.LinksAdded = len(findings)
	return res, nil
}

// sniffPayloadShapes walks every source file in every repo and
// returns the shape buckets keyed by (repo, file, function-name)
// plus the count of files actually scanned (used to decide whether
// the pass should emit anything at all).
func sniffPayloadShapes(graphs []repoGraph) (map[shapeKey]*shapeBucket, int) {
	buckets := map[shapeKey]*shapeBucket{}
	scanned := 0
	for _, g := range graphs {
		fileSet := map[string]bool{}
		for _, e := range g.Entities {
			if e.SourceFile != "" {
				fileSet[e.SourceFile] = true
			}
		}
		for file := range fileSet {
			lang := substrate.LanguageForPath(file)
			if lang == "" {
				continue
			}
			sniff := substrate.PayloadShapeSnifferFor(lang)
			if sniff == nil {
				continue
			}
			srcRoot := repoSourcePathFor(g.Repo)
			if srcRoot == "" {
				srcRoot = g.FileRoot
			}
			abs := filepath.Join(srcRoot, file)
			content, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			scanned++
			for _, s := range sniff(string(content)) {
				if s.Function == "" {
					continue
				}
				sCopy := s
				k := shapeKey{repo: g.Repo, file: file, fn: s.Function}
				b := buckets[k]
				if b == nil {
					b = &shapeBucket{}
					buckets[k] = b
				}
				switch {
				case s.Side == substrate.PayloadSideProducer && s.Direction == substrate.PayloadDirectionRequest:
					b.producerRequest = mergeShape(b.producerRequest, &sCopy)
				case s.Side == substrate.PayloadSideProducer && s.Direction == substrate.PayloadDirectionResponse:
					b.producerResponse = mergeShape(b.producerResponse, &sCopy)
				case s.Side == substrate.PayloadSideConsumer && s.Direction == substrate.PayloadDirectionRequest:
					b.consumerRequest = mergeShape(b.consumerRequest, &sCopy)
				case s.Side == substrate.PayloadSideConsumer && s.Direction == substrate.PayloadDirectionResponse:
					b.consumerResponse = mergeShape(b.consumerResponse, &sCopy)
				}
			}
		}
	}
	return buckets, scanned
}

// mergeShape combines two shapes attributed to the same function,
// taking the field union and the MAX confidence. Used when a single
// function emits multiple shape records (e.g. both a json= literal
// and a separate body assembly in two branches).
func mergeShape(into, add *substrate.PayloadShape) *substrate.PayloadShape {
	if into == nil {
		return add
	}
	if add == nil {
		return into
	}
	seen := map[string]bool{}
	merged := make([]substrate.PayloadField, 0, len(into.Fields)+len(add.Fields))
	for _, f := range into.Fields {
		if !seen[f.Name] {
			seen[f.Name] = true
			merged = append(merged, f)
		}
	}
	for _, f := range add.Fields {
		if !seen[f.Name] {
			seen[f.Name] = true
			merged = append(merged, f)
		}
	}
	conf := into.Confidence
	if add.Confidence > conf {
		conf = add.Confidence
	}
	out := *into
	out.Fields = merged
	out.Confidence = conf
	if out.EndpointHint == "" {
		out.EndpointHint = add.EndpointHint
	}
	if out.VerbHint == "" {
		out.VerbHint = add.VerbHint
	}
	return &out
}

// mergeBucket folds the shapes from `add` into `into`. Used when the
// binder lands two distinct (file, fn) triples on the same entity ID
// (rare; happens for closure-attributed records).
func mergeBucket(into, add *shapeBucket) {
	into.producerRequest = mergeShape(into.producerRequest, add.producerRequest)
	into.producerResponse = mergeShape(into.producerResponse, add.producerResponse)
	into.consumerRequest = mergeShape(into.consumerRequest, add.consumerRequest)
	into.consumerResponse = mergeShape(into.consumerResponse, add.consumerResponse)
}

// loadHTTPLinks reads the persisted links file and returns the
// MethodHTTP entries (cross-repo HTTP route ↔ fetch links). Returns
// an empty slice when the file does not exist.
func loadHTTPLinks(path string) ([]Link, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc Document
	if err := json.Unmarshal(buf, &doc); err != nil {
		return nil, err
	}
	out := make([]Link, 0, len(doc.Links))
	for _, l := range doc.Links {
		if l.Method == MethodHTTP {
			out = append(out, l)
		}
	}
	return out, nil
}

// entityIndexEntry holds the minimum information about one graph
// entity needed to walk IMPLEMENTS / CALLS edges and to annotate
// findings.
type entityIndexEntry struct {
	repo string
	file string
	name string
	kind string
	// implementsFrom collects entity IDs that IMPLEMENTS this entity
	// (typed as endpoint -> handler).
	implementsFrom []string
	// callsFrom collects entity IDs that CALLS this entity (typed
	// as endpoint -> caller).
	callsFrom []string
}

// buildEntityIndex returns a map from prefixed entity ID
// ("<repo>::<id>") to its index entry. Used to resolve link
// source/target synthetics back to the underlying handler / caller.
func buildEntityIndex(graphs []repoGraph) map[string]*entityIndexEntry {
	idx := map[string]*entityIndexEntry{}
	for _, g := range graphs {
		for _, e := range g.Entities {
			idx[entityKey(g.Repo, e.ID)] = &entityIndexEntry{
				repo: g.Repo,
				file: e.SourceFile,
				name: e.Name,
				kind: e.Kind,
			}
		}
		for _, ed := range g.Edges {
			switch strings.ToUpper(ed.Kind) {
			case "IMPLEMENTS":
				if to := idx[entityKey(g.Repo, ed.ToID)]; to != nil {
					to.implementsFrom = append(to.implementsFrom, entityKey(g.Repo, ed.FromID))
				}
			case "CALLS":
				if to := idx[entityKey(g.Repo, ed.ToID)]; to != nil {
					to.callsFrom = append(to.callsFrom, entityKey(g.Repo, ed.FromID))
				}
			}
		}
	}
	return idx
}

// resolveProducerHandlers walks the IMPLEMENTS edges of the producer-
// side http_endpoint synthetic to return the handler entity IDs. When
// no IMPLEMENTS edge exists (some frameworks skip it), the synthetic
// itself is returned so its source_caller / source_file annotations
// can still be used.
func resolveProducerHandlers(endpointID string, idx map[string]*entityIndexEntry) []string {
	e := idx[endpointID]
	if e == nil {
		return nil
	}
	if len(e.implementsFrom) > 0 {
		return e.implementsFrom
	}
	return []string{endpointID}
}

// resolveConsumerCallers walks the CALLS edges of the consumer-side
// http_endpoint synthetic to return the caller entity IDs.
func resolveConsumerCallers(endpointID string, idx map[string]*entityIndexEntry) []string {
	e := idx[endpointID]
	if e == nil {
		return nil
	}
	if len(e.callsFrom) > 0 {
		return e.callsFrom
	}
	return []string{endpointID}
}

// pickShapes returns the producer and consumer shapes for one
// direction on the given handler / caller pair.
func pickShapes(
	shapesByEntity map[string]*shapeBucket,
	producerEntity, consumerEntity string,
	dir substrate.PayloadDirection,
) (*substrate.PayloadShape, *substrate.PayloadShape) {
	var prod, cons *substrate.PayloadShape
	if pb := shapesByEntity[producerEntity]; pb != nil {
		switch dir {
		case substrate.PayloadDirectionRequest:
			prod = pb.producerRequest
		case substrate.PayloadDirectionResponse:
			prod = pb.producerResponse
		}
	}
	if cb := shapesByEntity[consumerEntity]; cb != nil {
		switch dir {
		case substrate.PayloadDirectionRequest:
			cons = cb.consumerRequest
		case substrate.PayloadDirectionResponse:
			cons = cb.consumerResponse
		}
	}
	return prod, cons
}

// buildDriftFinding computes the SchemaDrift for one (endpoint,
// direction, producer entity, consumer entity) tuple. Returns
// (finding, true) when there is observable evidence on at least one
// side; (zero, false) when both sides are entirely absent.
//
// Honest behaviour:
//   - Both shapes present, fields identical (after case-normalise) →
//     no finding (return false).
//   - Both shapes present, mismatch → SeverityHigh/Medium.
//   - One side present, the other absent → SeverityLow (informational).
//   - Both sides absent → no finding.
func buildDriftFinding(
	link Link,
	producerEntity, consumerEntity string,
	dir substrate.PayloadDirection,
	prod, cons *substrate.PayloadShape,
	idx map[string]*entityIndexEntry,
) (SchemaDrift, bool) {
	if prod == nil && cons == nil {
		return SchemaDrift{}, false
	}
	prodFields := normalisedFieldSet(prod)
	consFields := normalisedFieldSet(cons)

	// Compute symmetric difference (case-normalised).
	missingInConsumer := setDifference(prodFields, consFields)
	missingInProducer := setDifference(consFields, prodFields)

	if prod != nil && cons != nil && len(missingInConsumer) == 0 && len(missingInProducer) == 0 {
		return SchemaDrift{}, false
	}

	// Severity rubric.
	severity := computeSeverity(prod, cons, missingInProducer, missingInConsumer)

	endpointName := ""
	if e := idx[link.Target]; e != nil {
		endpointName = e.name
	}
	if endpointName == "" {
		if e := idx[link.Source]; e != nil {
			endpointName = e.name
		}
	}

	out := SchemaDrift{
		EndpointID:        link.Target,
		EndpointName:     endpointName,
		Direction:        string(dir),
		ProducerEntity:   producerEntity,
		ConsumerEntity:   consumerEntity,
		Severity:         severity,
		MissingInProducer: missingInProducer,
		MissingInConsumer: missingInConsumer,
	}
	if e := idx[producerEntity]; e != nil {
		out.ProducerRepo = e.repo
		out.ProducerFile = e.file
	}
	if e := idx[consumerEntity]; e != nil {
		out.ConsumerRepo = e.repo
		out.ConsumerFile = e.file
	}
	if prod != nil {
		out.ProducerFunction = prod.Function
		out.ProducerFields = fieldNames(prod.Fields)
		out.ProducerConfidence = prod.Confidence
	}
	if cons != nil {
		out.ConsumerFunction = cons.Function
		out.ConsumerFields = fieldNames(cons.Fields)
		out.ConsumerConfidence = cons.Confidence
	}
	out.Confidence = minConfidence(out.ProducerConfidence, out.ConsumerConfidence)
	out.Explanation = explainDrift(out)
	return out, true
}

// normalisedFieldSet returns a set of case-normalised field names for
// the shape, or an empty set when the shape is nil / has no fields.
func normalisedFieldSet(s *substrate.PayloadShape) map[string]string {
	out := map[string]string{}
	if s == nil {
		return out
	}
	for _, f := range s.Fields {
		k := substrate.NormalizeFieldName(f.Name)
		if k == "" {
			continue
		}
		if _, ok := out[k]; !ok {
			out[k] = f.Name
		}
	}
	return out
}

// setDifference returns the original field names from a that are not
// keyed in b, in sorted order of normalised key.
func setDifference(a, b map[string]string) []string {
	keys := make([]string, 0, len(a))
	for k := range a {
		if _, ok := b[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, a[k])
	}
	return out
}

func fieldNames(fs []substrate.PayloadField) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

func minConfidence(a, b float64) float64 {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

func computeSeverity(prod, cons *substrate.PayloadShape, missingInProd, missingInCons []string) DriftSeverity {
	bothPresent := prod != nil && cons != nil &&
		(len(prod.Fields) > 0 || len(cons.Fields) > 0)
	if !bothPresent {
		return DriftSeverityLow
	}
	if len(missingInProd) == 0 && len(missingInCons) == 0 {
		return DriftSeverityLow
	}
	prodConf := 0.0
	consConf := 0.0
	if prod != nil {
		prodConf = prod.Confidence
	}
	if cons != nil {
		consConf = cons.Confidence
	}
	if prodConf >= 0.8 && consConf >= 0.8 {
		return DriftSeverityHigh
	}
	return DriftSeverityMedium
}

func severityRank(s DriftSeverity) int {
	switch s {
	case DriftSeverityHigh:
		return 3
	case DriftSeverityMedium:
		return 2
	case DriftSeverityLow:
		return 1
	}
	return 0
}

// explainDrift returns a single-sentence human-facing summary of the
// finding for the MCP tool to relay back to agents.
func explainDrift(d SchemaDrift) string {
	switch {
	case len(d.MissingInProducer) > 0 && len(d.MissingInConsumer) > 0:
		return fmt.Sprintf(
			"Bidirectional %s drift on %s: consumer sends %d field(s) the producer does not read (%s); producer reads %d field(s) the consumer does not send (%s).",
			d.Direction, d.EndpointName,
			len(d.MissingInProducer), strings.Join(d.MissingInProducer, ", "),
			len(d.MissingInConsumer), strings.Join(d.MissingInConsumer, ", "),
		)
	case len(d.MissingInProducer) > 0:
		return fmt.Sprintf(
			"Consumer sends %d %s field(s) the producer does not read: %s.",
			len(d.MissingInProducer), d.Direction, strings.Join(d.MissingInProducer, ", "),
		)
	case len(d.MissingInConsumer) > 0:
		return fmt.Sprintf(
			"Producer emits %d %s field(s) the consumer does not destructure: %s.",
			len(d.MissingInConsumer), d.Direction, strings.Join(d.MissingInConsumer, ", "),
		)
	case len(d.ProducerFields) == 0 && len(d.ConsumerFields) > 0:
		return fmt.Sprintf(
			"%s shape observed on consumer side (%d field(s)) but no producer-side shape extracted. Likely cross-file DTO or dynamic body assembly on the producer.",
			d.Direction, len(d.ConsumerFields),
		)
	case len(d.ConsumerFields) == 0 && len(d.ProducerFields) > 0:
		return fmt.Sprintf(
			"%s shape observed on producer side (%d field(s)) but no consumer-side shape extracted. Consumer may be assembling the body dynamically.",
			d.Direction, len(d.ProducerFields),
		)
	}
	return fmt.Sprintf("%s shape observed on both sides without divergence.", d.Direction)
}

// writeDriftSidecar persists the document to the sidecar JSON path.
func writeDriftSidecar(paths Paths, doc payloadDriftDocument) error {
	path := driftSidecarPath(paths)
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}

// clearDriftSidecar removes the sidecar so a re-run that finds no
// source on disk doesn't leave stale findings.
func clearDriftSidecar(paths Paths) error {
	path := driftSidecarPath(paths)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// driftSidecarPath returns the on-disk path for this pass's sidecar.
func driftSidecarPath(paths Paths) string {
	return strings.TrimSuffix(paths.Links, ".json") + driftSidecarSuffix
}

// LoadDriftFindings reads the persisted findings sidecar. Returns
// (nil, nil) when the file is missing, so MCP can gracefully report
// the no-data case.
func LoadDriftFindings(paths Paths) ([]SchemaDrift, error) {
	path := driftSidecarPath(paths)
	buf, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc payloadDriftDocument
	if err := json.Unmarshal(buf, &doc); err != nil {
		return nil, err
	}
	return doc.Findings, nil
}

// DriftSidecarPath exposes the on-disk path of the sidecar for the
// MCP tool. Keeps the path construction co-located with the writer so
// the two never disagree on the filename convention.
func DriftSidecarPath(paths Paths) string {
	return driftSidecarPath(paths)
}
