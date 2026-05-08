package php

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_php_symfony", &symfonyExtractor{})
}

type symfonyExtractor struct{}

func (e *symfonyExtractor) Language() string { return "custom_php_symfony" }

var (
	reSymfonyRouteAttr = regexp.MustCompile(
		`#\[Route\s*\(\s*['"]([^'"]+)['"]`,
	)
	reSymfonyRouteAnnot = regexp.MustCompile(
		`(?m)@Route\s*\(\s*['"]([^'"]+)['"]`,
	)
	reSymfonyController = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:AbstractController|Controller)\b`,
	)
	reSymfonyEntity = regexp.MustCompile(
		`(?m)#\[ORM\\Entity\b|@ORM\\Entity\b`,
	)
	reSymfonyEntityClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\b`,
	)
	reSymfonyORMRelation = regexp.MustCompile(
		`#\[ORM\\(OneToMany|ManyToOne|ManyToMany|OneToOne)\b`,
	)
	reSymfonyEventSubscriber = regexp.MustCompile(
		`(?m)implements\s+EventSubscriberInterface\b`,
	)
	reSymfonySubscriberClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\s`,
	)
	reSymfonyMessageHandler = regexp.MustCompile(
		`(?m)#\[AsMessageHandler\]|implements\s+MessageHandlerInterface\b`,
	)
	reSymfonyServiceClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\b`,
	)
)

func (e *symfonyExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.symfony_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "symfony"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "php" {
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

	// 1. PHP8 attribute routes #[Route("/path")] -> SCOPE.Operation/endpoint
	for _, m := range reSymfonyRouteAttr.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ROUTE",
			"route_path", path, "route_style", "attribute")
		add(ent)
	}

	// 2. Annotation routes @Route("/path") -> SCOPE.Operation/endpoint
	for _, m := range reSymfonyRouteAnnot.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ROUTE",
			"route_path", path, "route_style", "annotation")
		add(ent)
	}

	// 3. Controller classes -> SCOPE.Component
	for _, m := range reSymfonyController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_CONTROLLER")
		add(ent)
	}

	// 4. Doctrine entities: find @ORM\Entity or #[ORM\Entity] then nearest class
	entityMatches := reSymfonyEntity.FindAllStringIndex(src, -1)
	for _, em := range entityMatches {
		// Find the next class declaration after the annotation
		rest := src[em[0]:]
		cm := reSymfonyEntityClass.FindStringSubmatch(rest)
		if cm != nil {
			name := cm[1]
			ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, em[0]))
			setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ENTITY")
			add(ent)
		}
	}

	// 5. ORM relations -> SCOPE.Component
	for _, m := range reSymfonyORMRelation.FindAllStringSubmatchIndex(src, -1) {
		relType := src[m[2]:m[3]]
		ent := makeEntity("relation:"+relType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_RELATION",
			"relation_type", relType)
		add(ent)
	}

	// 6. EventSubscriberInterface implementors -> SCOPE.Pattern
	subscriberMatches := reSymfonyEventSubscriber.FindAllStringIndex(src, -1)
	for _, sm := range subscriberMatches {
		// Find preceding class name
		prefix := src[:sm[0]]
		cm := reSymfonySubscriberClass.FindAllStringSubmatch(prefix, -1)
		if len(cm) > 0 {
			name := cm[len(cm)-1][1]
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, sm[0]))
			setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_EVENT_SUBSCRIBER")
			add(ent)
		}
	}

	// 7. Message handlers -> SCOPE.Service
	handlerMatches := reSymfonyMessageHandler.FindAllStringIndex(src, -1)
	for _, hm := range handlerMatches {
		prefix := src[:hm[0]]
		cm := reSymfonyServiceClass.FindAllStringSubmatch(prefix, -1)
		if len(cm) > 0 {
			name := cm[len(cm)-1][1]
			ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, hm[0]))
			setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_MESSAGE_HANDLER")
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
