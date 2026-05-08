package cpp

import (
	"context"
	"fmt"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_cpp_qt", &qtExtractor{})
}

type qtExtractor struct{}

func (e *qtExtractor) Language() string { return "custom_cpp_qt" }

var (
	reQtQObjectSimple = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*(?::\s*(?:public|protected|private)\s+\w+(?:\s*,\s*(?:public|protected|private)\s+\w+)*)?\s*\{`,
	)
	reQtQObjectMacro = regexp.MustCompile(
		`\bQ_OBJECT\b`,
	)
	reQtSignalSection = regexp.MustCompile(
		`(?m)^signals:\s*$`,
	)
	reQtSlotSection = regexp.MustCompile(
		`(?m)^(?:public|protected|private)\s+slots:\s*$`,
	)
	reQtSignalDecl = regexp.MustCompile(
		`(?m)^\s+(?:void|(?:Q\w+|int|bool|QString|double)\s*\*?)\s+(\w+)\s*\([^)]*\)\s*;`,
	)
	reQtConnectOldStyle = regexp.MustCompile(
		`connect\s*\(\s*(\w+)\s*,\s*SIGNAL\s*\(\s*(\w+)\s*\([^)]*\)\s*\)\s*,\s*(\w+)\s*,\s*SLOT\s*\(\s*(\w+)`,
	)
	reQtConnectNewStyle = regexp.MustCompile(
		`connect\s*\(\s*(\w+)\s*,\s*&([A-Za-z_]\w*)\s*::\s*(\w+)\s*,\s*(\w+)\s*,\s*&([A-Za-z_]\w*)\s*::\s*(\w+)`,
	)
	reQtProperty = regexp.MustCompile(
		`Q_PROPERTY\s*\([^)]*\b(\w+)\s+READ\s+(\w+)`,
	)
)

func (e *qtExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.qt_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "qt"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. QObject subclasses (class with Q_OBJECT macro) -> SCOPE.UIComponent
	// Find all class declarations, then check if Q_OBJECT appears nearby
	qObjectPositions := reQtQObjectMacro.FindAllStringIndex(src, -1)
	qObjectOffsets := make(map[int]bool)
	for _, pos := range qObjectPositions {
		qObjectOffsets[pos[0]] = true
	}

	for _, m := range reQtQObjectSimple.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		classStart := m[0]
		// Check if Q_OBJECT appears within 500 chars of class body opening
		classBodyStart := m[1]
		window := classBodyStart + 500
		if window > len(src) {
			window = len(src)
		}
		bodySnip := src[classBodyStart:window]
		if !reQtQObjectMacro.MatchString(bodySnip) {
			continue
		}
		ent := makeEntity(className, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, classStart))
		setProps(&ent, "framework", "qt", "provenance", "INFERRED_FROM_QT_QOBJECT")
		add(ent)
	}

	// 2. signals: sections -- find methods after signals: -> SCOPE.Pattern
	signalSections := reQtSignalSection.FindAllStringIndex(src, -1)
	for _, ss := range signalSections {
		// Find methods until next section keyword
		nextSection := len(src)
		// Look for next section or closing brace
		sectionSearch := src[ss[1]:]
		nextSectionRe := regexp.MustCompile(`(?m)^\s*(?:signals:|(?:public|protected|private)\s+slots:|public:|private:|protected:|\})`)
		if nm := nextSectionRe.FindStringIndex(sectionSearch); nm != nil {
			nextSection = ss[1] + nm[0]
		}
		sectionBody := src[ss[1]:nextSection]
		for _, dm := range reQtSignalDecl.FindAllStringSubmatchIndex(sectionBody, -1) {
			sigName := sectionBody[dm[2]:dm[3]]
			ent := makeEntity(sigName, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, ss[0]))
			setProps(&ent, "framework", "qt", "provenance", "INFERRED_FROM_QT_SIGNAL",
				"signal_name", sigName)
			add(ent)
		}
	}

	// 3. slots sections -> SCOPE.Operation/function
	slotSections := reQtSlotSection.FindAllStringIndex(src, -1)
	for _, ss := range slotSections {
		nextSection := len(src)
		sectionSearch := src[ss[1]:]
		nextSectionRe := regexp.MustCompile(`(?m)^\s*(?:signals:|(?:public|protected|private)\s+slots:|public:|private:|protected:|\})`)
		if nm := nextSectionRe.FindStringIndex(sectionSearch); nm != nil {
			nextSection = ss[1] + nm[0]
		}
		sectionBody := src[ss[1]:nextSection]
		for _, dm := range reQtSignalDecl.FindAllStringSubmatchIndex(sectionBody, -1) {
			slotName := sectionBody[dm[2]:dm[3]]
			ent := makeEntity(slotName, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, ss[0]))
			setProps(&ent, "framework", "qt", "provenance", "INFERRED_FROM_QT_SLOT",
				"slot_name", slotName)
			add(ent)
		}
	}

	// 4. Old-style connect(sender, SIGNAL(sig()), receiver, SLOT(slot())) -> signal/slot wiring
	connectCount := 0
	for _, m := range reQtConnectOldStyle.FindAllStringSubmatchIndex(src, -1) {
		connectCount++
		sender := src[m[2]:m[3]]
		signal := src[m[4]:m[5]]
		slot := src[m[8]:m[9]]
		name := fmt.Sprintf("connect:%s::%s->%s", sender, signal, slot)
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "qt", "provenance", "INFERRED_FROM_QT_CONNECT",
			"sender", sender, "signal", signal, "slot", slot, "connect_style", "old")
		add(ent)
	}

	// 5. New-style connect -> signal/slot wiring
	for _, m := range reQtConnectNewStyle.FindAllStringSubmatchIndex(src, -1) {
		sender := src[m[2]:m[3]]
		senderClass := src[m[4]:m[5]]
		signal := src[m[6]:m[7]]
		slot := src[m[12]:m[13]]
		name := fmt.Sprintf("connect:%s::%s->%s", senderClass, signal, slot)
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "qt", "provenance", "INFERRED_FROM_QT_CONNECT",
			"sender", sender, "signal", signal, "slot", slot, "connect_style", "new")
		add(ent)
	}

	// 6. Q_PROPERTY declarations -> SCOPE.Pattern (metadata)
	for _, m := range reQtProperty.FindAllStringSubmatchIndex(src, -1) {
		propName := src[m[4]:m[5]]
		ent := makeEntity("property:"+propName, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "qt", "provenance", "INFERRED_FROM_QT_PROPERTY",
			"property_name", propName)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
