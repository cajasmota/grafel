package ruby

import (
	"context"
	"fmt"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_ruby_rspec", &rspecExtractor{})
}

type rspecExtractor struct{}

func (e *rspecExtractor) Language() string { return "custom_ruby_rspec" }

var (
	reRspecDescribe = regexp.MustCompile(
		`(?m)^\s*(?:RSpec\.)?(?:describe|context)\s+['"]([^'"]+)['"]`,
	)
	reRspecDescribeConst = regexp.MustCompile(
		`(?m)^\s*(?:RSpec\.)?(?:describe|context)\s+([A-Z][A-Za-z0-9_:]*)`,
	)
	reRspecExample = regexp.MustCompile(
		`(?m)^\s*(?:it|specify)\s+['"]([^'"]+)['"]`,
	)
	reRspecLet = regexp.MustCompile(
		`(?m)^\s*let!?\s*\(:([a-z_]+)\)`,
	)
	reRspecHook = regexp.MustCompile(
		`(?m)^\s*(before|after|around)\s*(?:\(:[a-z_]+\))?\s+do`,
	)
	reRspecShared = regexp.MustCompile(
		`(?m)^\s*shared_(?:examples|context)\s+['"]([^'"]+)['"]`,
	)
	reRspecSubject = regexp.MustCompile(
		`(?m)^\s*subject\s*(?::[a-z_]+|\{|\()`,
	)
	reRspecInclude = regexp.MustCompile(
		`(?m)^\s*(?:include_examples|it_behaves_like|include_context)\s+['"]([^'"]+)['"]`,
	)
)

func (e *rspecExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.rspec_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "rspec"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "ruby" {
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

	// 1. describe/context string labels -> SCOPE.Component
	for _, m := range reRspecDescribe.FindAllStringSubmatchIndex(src, -1) {
		label := src[m[2]:m[3]]
		ent := makeEntity(label, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_GROUP",
			"group_label", label)
		add(ent)
	}

	// 1b. describe/context with constant names -> SCOPE.Component
	for _, m := range reRspecDescribeConst.FindAllStringSubmatchIndex(src, -1) {
		label := src[m[2]:m[3]]
		ent := makeEntity(label, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_GROUP",
			"group_label", label, "is_constant", "true")
		add(ent)
	}

	// 2. it/specify examples -> SCOPE.Operation/function
	for i, m := range reRspecExample.FindAllStringSubmatchIndex(src, -1) {
		label := src[m[2]:m[3]]
		// Deduplicate by position-based name to allow same label in different groups
		name := fmt.Sprintf("%s#%d", label, i)
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_EXAMPLE",
			"example_description", label)
		add(ent)
	}

	// 3. let/let! helpers -> SCOPE.Pattern
	for _, m := range reRspecLet.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_LET",
			"let_name", name)
		add(ent)
	}

	// 4. before/after hooks -> SCOPE.Operation/function
	hookCount := 0
	for _, m := range reRspecHook.FindAllStringSubmatchIndex(src, -1) {
		hookType := src[m[2]:m[3]]
		hookCount++
		name := fmt.Sprintf("%s_hook_%d", hookType, hookCount)
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_HOOK",
			"hook_type", hookType)
		add(ent)
	}

	// 5. shared_examples/shared_context -> SCOPE.Component
	for _, m := range reRspecShared.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_SHARED")
		add(ent)
	}

	// 6. subject blocks -> SCOPE.Pattern
	subjectCount := 0
	for _, m := range reRspecSubject.FindAllStringIndex(src, -1) {
		subjectCount++
		name := fmt.Sprintf("subject_%d", subjectCount)
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_SUBJECT")
		add(ent)
	}

	// 7. include_examples / it_behaves_like -> SCOPE.Pattern (reference)
	for _, m := range reRspecInclude.FindAllStringSubmatchIndex(src, -1) {
		name := "include:" + src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_INCLUDE",
			"shared_group", src[m[2]:m[3]])
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
