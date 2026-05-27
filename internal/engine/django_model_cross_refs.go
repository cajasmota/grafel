// django_model_cross_refs.go — Serializer/Signal/FilterSet → Model cross-ref edges.
//
// Issue #2578 — MCP graph misses serializer/signal/filter cross-references
// when looking up "where is Model X referenced?".  Bench iter 2 q11 surfaced
// the gap: querying GroupDeviceSettings references returned zero results for
// ReadGroupDeviceSettingsSerializer (declared Meta.model = GroupDeviceSettings)
// and for the @receiver(post_save, sender=GroupDeviceSettings) handler in
// replicate_to_datalake.py.
//
// This file adds three REPO-WIDE synthesis passes (they walk every Python file
// to resolve cross-file class names):
//
//	ApplySerializerMetaModelEdges
//	  — DRF Serializer subclasses (any class whose name ends in "Serializer",
//	    or whose bases include a Serializer suffix) with an inner
//	    `class Meta: model = SomeModel` declaration emit a REFERENCES edge
//	    from the Serializer class to the Model class.
//
//	ApplyReceiverSenderEdges
//	  — @receiver(post_save, sender=Model) (and pre_save / post_delete etc.)
//	    decorators emit a LISTENS_FOR edge from the decorated handler function
//	    to the named Model class.
//
//	ApplyFilterSetMetaModelEdges
//	  — django_filter FilterSet subclasses with an inner
//	    `class Meta: model = SomeModel` emit a REFERENCES edge from the
//	    FilterSet class to the Model class.
//
// Architecture mirrors the peer engine passes (django_signal_pubsub_edges.go,
// orm_field_edges.go): each function is APPEND-ONLY — it never modifies or
// removes existing entities or edges, so it cannot regress surrounding passes.
//
// All three passes are called from index.go Pass 2.6 alongside the Celery and
// custom-signal pub/sub passes.
//
// Refs #2578.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// serializerMetaModelRe matches an inner Meta class with a model = SomeName
// assignment inside a Python class body.  We look for it scoped to two levels
// of indentation so that nested-class false positives (e.g. a NestedSerializer
// inside a parent body) are handled correctly by the enclosing-class scanner.
//
// Group 1 = the model class name (bare identifier, may be qualified like
// "accounts.User" — in that case we take the last segment only).
var serializerMetaModelRe = regexp.MustCompile(
	`(?m)^\s{4,}class\s+Meta\s*(?:\([^)]*\))?\s*:\s*\n(?:[^\n]*\n)*?\s+model\s*=\s*([A-Z]\w*)`,
)

// filterSetMetaModelRe is functionally identical to serializerMetaModelRe; we
// keep them separate for clarity and independent testing.
var filterSetMetaModelRe = regexp.MustCompile(
	`(?m)^\s{4,}class\s+Meta\s*(?:\([^)]*\))?\s*:\s*\n(?:[^\n]*\n)*?\s+model\s*=\s*([A-Z]\w*)`,
)

// outerClassRe extracts every top-level class definition in a Python file.
// Group 1 = class name, Group 2 = base-class list (may be empty).
var outerClassRe = regexp.MustCompile(
	`(?m)^class\s+(\w+)\s*(?:\(([^)]*)\))?\s*:`,
)

// receiverSenderLineRe matches a single @receiver(…, sender=SomeClass…) line
// where the sender is a bare class reference (e.g. sender=Building).
// Group 1 = sender class name.
var receiverSenderLineRe = regexp.MustCompile(
	`(?m)^[ \t]*@receiver\s*\([^)]*\bsender\s*=\s*([A-Z]\w*)[^)]*\)`,
)

// receiverSenderStringRe matches @receiver(…, sender='core.Building') or
// @receiver(…, sender='Building') — string-literal sender forms.
// Group 1 = the string contents (may contain a dot-separated app label prefix).
var receiverSenderStringRe = regexp.MustCompile(
	`(?m)^[ \t]*@receiver\s*\([^)]*\bsender\s*=\s*['"]([A-Za-z_][\w.]*)['"][^)]*\)`,
)

// receiverSenderAppsGetModelRe matches the pattern
// @receiver(…, sender=apps.get_model('core', 'Building')) (and double-quote variants).
// Group 1 = app label, Group 2 = model name.
var receiverSenderAppsGetModelRe = regexp.MustCompile(
	`(?m)^[ \t]*@receiver\s*\([^)]*\bsender\s*=\s*(?:\w+\.)?get_model\s*\(\s*['"]([A-Za-z_]\w*)['"][^)]*,\s*['"]([A-Za-z_]\w*)['"][^)]*\)[^)]*\)`,
)

// receiverDefRe matches the def (or async def) that terminates a decorator
// block.  Group 1 = function name.
var receiverDefRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:async\s+)?def\s+(\w+)\s*\(`,
)

// ---------------------------------------------------------------------------
// ApplySerializerMetaModelEdges
// ---------------------------------------------------------------------------

// ApplySerializerMetaModelEdges walks every Python file in the repo and emits
// a REFERENCES edge from each DRF Serializer class to the Model class named in
// its inner `class Meta: model = <Model>` declaration.
//
// A class is treated as a Serializer candidate when its name ends in
// "Serializer" OR when its base-class list contains a name ending in
// "Serializer".  This matches the conventions used in upvate_core and the
// majority of DRF codebases.
//
// pyPaths:    repo-relative paths of every Python file.
// fileReader: returns the source bytes for a repo-relative path.
func ApplySerializerMetaModelEdges(
	pyPaths []string,
	fileReader NestedURLConfFileReader,
) []types.RelationshipRecord {
	if fileReader == nil {
		return nil
	}

	var out []types.RelationshipRecord
	seen := map[string]bool{}

	for _, p := range pyPaths {
		content := fileReader(p)
		if len(content) == 0 {
			continue
		}
		s := string(content)
		// Quick bail-out: if the file has neither "Serializer" nor a Meta class
		// there is nothing to do.
		if !strings.Contains(s, "Serializer") || !strings.Contains(s, "class Meta") {
			continue
		}

		// For each top-level class in the file, check whether it is a
		// Serializer and whether it declares Meta.model.
		classMatches := outerClassRe.FindAllStringSubmatchIndex(s, -1)
		for ci, cm := range classMatches {
			className := s[cm[2]:cm[3]]
			var bases string
			if cm[4] >= 0 {
				bases = s[cm[4]:cm[5]]
			}

			isSerializer := strings.HasSuffix(className, "Serializer") ||
				serializerInBases(bases)
			if !isSerializer {
				continue
			}

			// Determine the extent of this class body: from after the colon to
			// the start of the next top-level class (or end of file).
			bodyStart := cm[1] // byte after the class header line ends
			bodyEnd := len(s)
			if ci+1 < len(classMatches) {
				bodyEnd = classMatches[ci+1][0]
			}
			body := s[bodyStart:bodyEnd]

			// Look for `class Meta: … model = SomeModel` inside the body.
			mm := serializerMetaModelRe.FindStringSubmatch(body)
			if mm == nil {
				continue
			}
			modelName := mm[1]

			key := p + "|Serializer:" + className + "|Model:" + modelName
			if seen[key] {
				continue
			}
			seen[key] = true

			out = append(out, types.RelationshipRecord{
				FromID: "Class:" + className,
				ToID:   "Class:" + modelName,
				Kind:   string(types.RelationshipKindReferences),
				Properties: map[string]string{
					"framework":    "drf",
					"pattern_type": "serializer_meta_model",
					"via":          "Meta.model",
				},
			})
		}
	}
	return out
}

// serializerInBases returns true when the bases string (the raw content
// between the parentheses in `class Foo(bases...)`) contains at least one
// name ending in "Serializer".
func serializerInBases(bases string) bool {
	for _, part := range strings.Split(bases, ",") {
		t := strings.TrimSpace(part)
		// Strip any module prefix: "serializers.ModelSerializer" → "ModelSerializer"
		if dot := strings.LastIndexByte(t, '.'); dot >= 0 {
			t = t[dot+1:]
		}
		if strings.HasSuffix(t, "Serializer") {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ApplyReceiverSenderEdges
// ---------------------------------------------------------------------------

// senderClassNameFromString extracts the model class name from a string sender
// value like "core.Building" or "Building".  It returns the last dot-separated
// segment, which is the class name, and also returns the app label (first
// segment) when present (empty string otherwise).
func senderClassNameFromString(s string) (appLabel, className string) {
	parts := strings.SplitN(s, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}

// ApplyReceiverSenderEdges walks every Python file and emits a HANDLES_SIGNAL
// edge from each @receiver(…, sender=Model) handler function to the named
// Model class.
//
// Three sender forms are handled:
//
//  1. Class reference:   sender=Building            (group 1 = "Building")
//  2. Full string:       sender='core.Building'     (app label + class name)
//  3. Bare string:       sender='Building'          (class name only)
//  4. apps.get_model:    sender=apps.get_model('core', 'Building')
//
// Bare `@receiver(post_save)` handlers (without sender=) are intentionally
// skipped because they don't name a specific model.
//
// Multiple stacked @receiver decorators on the same def (upvate_core pattern:
// one @receiver per model) each produce a separate HANDLES_SIGNAL edge.
func ApplyReceiverSenderEdges(
	pyPaths []string,
	fileReader NestedURLConfFileReader,
) []types.RelationshipRecord {
	if fileReader == nil {
		return nil
	}

	var out []types.RelationshipRecord
	seen := map[string]bool{}

	// emitEdge is a local helper that records a HANDLES_SIGNAL edge if not seen.
	emitEdge := func(p, handlerFunc, senderModel, via string) {
		key := p + "|" + handlerFunc + "|" + senderModel
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, types.RelationshipRecord{
			// Use the same entity-kind prefix as the Django per-file extractor
			// (django.go) which registers signal handler functions as
			// SCOPE.Operation entities. The resolver rewrites "SCOPE.Operation:name"
			// stubs to the corresponding hex entity ID in the graph, so the
			// HANDLES_SIGNAL edge is queryable via find_callers / archigraph_expand.
			//
			// Previously this used "Function:" which the resolver classifies as
			// DispositionDynamic and does NOT rewrite to a hex ID, leaving the
			// FromID as an unresolved stub that graph queries on the entity's hex
			// ID never match. This was root cause of #2589.
			FromID: "SCOPE.Operation:" + handlerFunc,
			ToID:   "Class:" + senderModel,
			Kind:   string(types.RelationshipKindHandlesSignal),
			Properties: map[string]string{
				"framework":    "django_signals",
				"pattern_type": "receiver_sender_model",
				"via":          via,
			},
		})
	}

	for _, p := range pyPaths {
		content := fileReader(p)
		if len(content) == 0 {
			continue
		}
		s := string(content)
		if !strings.Contains(s, "@receiver") || !strings.Contains(s, "sender=") {
			continue
		}

		// --- Form 1: class-reference sender (sender=Building) ---
		for _, idx := range receiverSenderLineRe.FindAllStringSubmatchIndex(s, -1) {
			senderModel := s[idx[2]:idx[3]]
			rest := s[idx[1]:]
			dm := receiverDefRe.FindStringSubmatchIndex(rest)
			if dm == nil {
				continue
			}
			handlerFunc := rest[dm[2]:dm[3]]
			emitEdge(p, handlerFunc, senderModel, "@receiver(sender=)")
		}

		// --- Form 2 & 3: string-literal sender ('core.Building' or 'Building') ---
		for _, idx := range receiverSenderStringRe.FindAllStringSubmatchIndex(s, -1) {
			rawSender := s[idx[2]:idx[3]]
			_, className := senderClassNameFromString(rawSender)
			if className == "" {
				continue
			}
			rest := s[idx[1]:]
			dm := receiverDefRe.FindStringSubmatchIndex(rest)
			if dm == nil {
				continue
			}
			handlerFunc := rest[dm[2]:dm[3]]
			emitEdge(p, handlerFunc, className, "@receiver(sender='<string>')")
		}

		// --- Form 4: apps.get_model('core', 'Building') ---
		for _, idx := range receiverSenderAppsGetModelRe.FindAllStringSubmatchIndex(s, -1) {
			// group 1 = app label (ignored for entity lookup; class name is enough)
			className := s[idx[4]:idx[5]]
			if className == "" {
				continue
			}
			rest := s[idx[1]:]
			dm := receiverDefRe.FindStringSubmatchIndex(rest)
			if dm == nil {
				continue
			}
			handlerFunc := rest[dm[2]:dm[3]]
			emitEdge(p, handlerFunc, className, "@receiver(sender=apps.get_model())")
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// ApplyFilterSetMetaModelEdges
// ---------------------------------------------------------------------------

// ApplyFilterSetMetaModelEdges walks every Python file and emits a REFERENCES
// edge from each django_filter FilterSet subclass to the Model class named in
// its inner `class Meta: model = <Model>` declaration.
//
// A class is treated as a FilterSet candidate when its name ends in "Filter"
// OR when its base-class list contains a name ending in "FilterSet".
func ApplyFilterSetMetaModelEdges(
	pyPaths []string,
	fileReader NestedURLConfFileReader,
) []types.RelationshipRecord {
	if fileReader == nil {
		return nil
	}

	var out []types.RelationshipRecord
	seen := map[string]bool{}

	for _, p := range pyPaths {
		content := fileReader(p)
		if len(content) == 0 {
			continue
		}
		s := string(content)
		if (!strings.Contains(s, "Filter") && !strings.Contains(s, "FilterSet")) ||
			!strings.Contains(s, "class Meta") {
			continue
		}

		classMatches := outerClassRe.FindAllStringSubmatchIndex(s, -1)
		for ci, cm := range classMatches {
			className := s[cm[2]:cm[3]]
			var bases string
			if cm[4] >= 0 {
				bases = s[cm[4]:cm[5]]
			}

			isFilterSet := strings.HasSuffix(className, "Filter") ||
				strings.HasSuffix(className, "FilterSet") ||
				filterSetInBases(bases)
			if !isFilterSet {
				continue
			}

			bodyStart := cm[1]
			bodyEnd := len(s)
			if ci+1 < len(classMatches) {
				bodyEnd = classMatches[ci+1][0]
			}
			body := s[bodyStart:bodyEnd]

			mm := filterSetMetaModelRe.FindStringSubmatch(body)
			if mm == nil {
				continue
			}
			modelName := mm[1]

			key := p + "|FilterSet:" + className + "|Model:" + modelName
			if seen[key] {
				continue
			}
			seen[key] = true

			out = append(out, types.RelationshipRecord{
				FromID: "Class:" + className,
				ToID:   "Class:" + modelName,
				Kind:   string(types.RelationshipKindReferences),
				Properties: map[string]string{
					"framework":    "django_filter",
					"pattern_type": "filterset_meta_model",
					"via":          "Meta.model",
				},
			})
		}
	}
	return out
}

// filterSetInBases returns true when the bases string contains a name ending
// in "FilterSet".
func filterSetInBases(bases string) bool {
	for _, part := range strings.Split(bases, ",") {
		t := strings.TrimSpace(part)
		if dot := strings.LastIndexByte(t, '.'); dot >= 0 {
			t = t[dot+1:]
		}
		if strings.HasSuffix(t, "FilterSet") {
			return true
		}
	}
	return false
}
