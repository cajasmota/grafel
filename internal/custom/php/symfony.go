package php

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_php_symfony", &symfonyExtractor{})
}

type symfonyExtractor struct{}

func (e *symfonyExtractor) Language() string { return "custom_php_symfony" }

// ---------------------------------------------------------------------------
// Routing regexes
// ---------------------------------------------------------------------------

var (
	// PHP8 attribute route — full attribute body captured for method+name parsing.
	// Captures: (1) path  (2) full attribute body after the path literal.
	// Example: #[Route('/products/{id}', methods: ['GET'], name: 'product_show')]
	reSymRouteAttrFull = regexp.MustCompile(
		`#\[Route\s*\(\s*['"]([^'"]+)['"]((?:[^[\]()]*|\[[^\]]*\])*)\)`,
	)

	// methods array inside a Route attribute body: methods: ['GET', 'POST'] or methods: ["GET"]
	reSymRouteMethods = regexp.MustCompile(
		`(?i)methods\s*:\s*\[([^\]]*)\]`,
	)

	// single-method shorthand: methods: 'GET'
	reSymRouteMethodsSingle = regexp.MustCompile(
		`(?i)methods\s*:\s*['"]([A-Z]+)['"]`,
	)

	// name: 'product_show'
	reSymRouteName = regexp.MustCompile(
		`(?i)name\s*:\s*['"]([^'"]+)['"]`,
	)

	// Class-level #[Route('/prefix')] or #[Route(prefix: '/prefix')]
	// Captured body is full so we can re-use reSymRouteMethods / reSymRouteName.
	reSymClassRouteAttr = regexp.MustCompile(
		`(?m)#\[Route\s*\(\s*['"]([^'"]+)['"]((?:[^[\]()]*|\[[^\]]*\])*)\)(?:[^\{]*\{)?[^#]*?class\s+\w+`,
	)

	// Annotation route @Route("/path",...) — full body captured.
	reSymRouteAnnotFull = regexp.MustCompile(
		`(?m)@Route\s*\(\s*['"]([^'"]+)['"]((?:[^)]*))`,
	)

	// YAML routes: path: /products/{id}
	reSymYAMLRouteName = regexp.MustCompile(
		`(?m)^(\w[\w._-]+):\s*$`,
	)
	reSymYAMLRoutePath = regexp.MustCompile(
		`(?m)^\s+path:\s*([^\s#\n]+)`,
	)
	reSymYAMLRouteController = regexp.MustCompile(
		`(?m)^\s+controller:\s*([^\s#\n]+)`,
	)
	reSymYAMLRouteMethods = regexp.MustCompile(
		`(?m)^\s+methods:\s*\[([^\]]*)\]`,
	)

	// Controller class
	reSymController = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:AbstractController|Controller)\b`,
	)

	// Doctrine entities
	reSymEntity = regexp.MustCompile(
		`(?m)#\[ORM\\Entity\b|@ORM\\Entity\b`,
	)
	reSymEntityClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\b`,
	)

	// ORM relations
	reSymORMRelation = regexp.MustCompile(
		`#\[ORM\\(OneToMany|ManyToOne|ManyToMany|OneToOne)\b`,
	)

	// EventSubscriberInterface
	reSymEventSubscriber = regexp.MustCompile(
		`(?m)implements\s+EventSubscriberInterface\b`,
	)
	reSymSubscriberClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\s`,
	)

	// getSubscribedEvents — extract event names
	reSymSubscribedEvents = regexp.MustCompile(
		`(?m)getSubscribedEvents\s*\([^{]*\{([^}]*)\}`,
	)
	reSymEventKey = regexp.MustCompile(
		`['"]([^'"]+)['"]\s*=>`,
	)

	// Kernel event listeners: addListener / addSubscriber
	reSymKernelListener = regexp.MustCompile(
		`(?m)\$eventDispatcher->addListener\s*\(\s*['"]([^'"]+)['"]`,
	)
	reSymKernelSubscriber = regexp.MustCompile(
		`(?m)\$eventDispatcher->addSubscriber\s*\(\s*new\s+(\w+)`,
	)

	// Message handlers
	reSymMessageHandler = regexp.MustCompile(
		`(?m)#\[AsMessageHandler\]|implements\s+MessageHandlerInterface\b`,
	)
	reSymServiceClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\b`,
	)
)

// ---------------------------------------------------------------------------
// Auth regexes
// ---------------------------------------------------------------------------

var (
	// #[IsGranted('ROLE_ADMIN')] or #[IsGranted("ROLE_ADMIN")]
	reSymIsGrantedAttr = regexp.MustCompile(
		`#\[IsGranted\s*\(\s*['"]([^'"]+)['"]`,
	)

	// $this->denyAccessUnlessGranted('ROLE_USER') — captures the role/attribute
	reSymDenyAccessUnlessGranted = regexp.MustCompile(
		`\$this->denyAccessUnlessGranted\s*\(\s*['"]([^'"]+)['"]`,
	)

	// security.yaml access_control roles: - { path: ^/admin, roles: ROLE_ADMIN }
	reSymSecurityAccessControl = regexp.MustCompile(
		`(?m)-\s*\{\s*path:\s*['"]*([^'"}\s,]+)['"]*[^}]*roles:\s*([^\},\n]+)`,
	)

	// security.yaml firewall names: main:, api:, dev:  (only top-level under firewalls:)
	reSymFirewallName = regexp.MustCompile(
		`(?m)^\s{4,8}(\w+):\s*$`,
	)

	// Voter class: extends Voter or implements VoterInterface
	reSymVoter = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:Voter|AbstractVoter)\b|class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+VoterInterface\b`,
	)

	// supports() method inside a Voter — captures the attribute string
	reSymVoterSupports = regexp.MustCompile(
		`(?m)case\s+['"]([^'"]+)['"]\s*:`,
	)
)

// ---------------------------------------------------------------------------
// Validation regexes
// ---------------------------------------------------------------------------

var (
	// Assert constraints as PHP8 attributes: #[Assert\NotBlank], #[Assert\Length(min: 2)], etc.
	// Captures: (1) constraint name  (2) optional argument body
	reSymAssertAttr = regexp.MustCompile(
		`#\[Assert\\(\w+)(?:\s*\(([^)]*)\))?`,
	)

	// Assert constraints as annotations: @Assert\NotBlank(), @Assert\Email
	reSymAssertAnnot = regexp.MustCompile(
		`@Assert\\(\w+)(?:\s*\(([^)]*)\))?`,
	)

	// $validator->validate($object) or $this->validator->validate($obj) call
	reSymValidatorValidate = regexp.MustCompile(
		`(?:\$\w+(?:->\w+)*)->validate\s*\(\s*\$`,
	)

	// DTO / Form type: class FooRequest or class FooDTO or extends AbstractType
	reSymDTOClass = regexp.MustCompile(
		`(?m)class\s+(\w+(?:Request|DTO|Input|Data|Form|Type))\b`,
	)

	// Symfony Validator component use
	reSymValidatorUse = regexp.MustCompile(
		`use\s+Symfony\\Component\\Validator\\`,
	)
)

// ---------------------------------------------------------------------------
// YAML file detection
// ---------------------------------------------------------------------------

func isSymfonyYAML(path string) bool {
	return strings.Contains(path, "config/routes") ||
		strings.HasSuffix(path, "routes.yaml") ||
		strings.HasSuffix(path, "routes.yml")
}

func isSecurityYAML(path string) bool {
	return strings.Contains(path, "config/packages/security") ||
		strings.HasSuffix(path, "security.yaml") ||
		strings.HasSuffix(path, "security.yml")
}

// ---------------------------------------------------------------------------
// Helper: parse HTTP methods from a Route attribute body fragment.
// Returns e.g. ["GET"] or ["GET","POST"] or nil if not present.
// ---------------------------------------------------------------------------

func symParseRouteMethods(body string) []string {
	if m := reSymRouteMethods.FindStringSubmatch(body); m != nil {
		// e.g. "'GET', 'POST'" -> ["GET","POST"]
		raw := m[1]
		var out []string
		for _, part := range strings.Split(raw, ",") {
			part = strings.Trim(part, " \t'\"")
			if part != "" {
				out = append(out, strings.ToUpper(part))
			}
		}
		return out
	}
	if m := reSymRouteMethodsSingle.FindStringSubmatch(body); m != nil {
		return []string{strings.ToUpper(m[1])}
	}
	return nil
}

// symParseRouteName extracts name: 'xxx' from a Route attribute body.
func symParseRouteName(body string) string {
	if m := reSymRouteName.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return ""
}

// symSplitMethodList splits a raw comma-separated list of HTTP methods (which
// may be bare or quoted) into a slice of uppercase method names.
// Example inputs: "GET", "'GET', 'POST'", "GET, HEAD"
func symSplitMethodList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.Trim(part, " \t'\"[]")
		if part != "" {
			out = append(out, strings.ToUpper(part))
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Main extractor
// ---------------------------------------------------------------------------

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

	// Accept .php files and Symfony YAML config files.
	isPHP := file.Language == "php"
	isRouteYAML := isSymfonyYAML(file.Path)
	isSecYAML := isSecurityYAML(file.Path)
	if !isPHP && !isRouteYAML && !isSecYAML {
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

	// -----------------------------------------------------------------------
	// YAML route extraction
	// -----------------------------------------------------------------------
	if isRouteYAML {
		// Walk through route blocks: name:\n  path: ...\n  methods: [...]
		// We scan with line-by-line YAML approach: find route names then look ahead.
		lines := strings.Split(src, "\n")
		for i, line := range lines {
			nm := reSymYAMLRouteName.FindStringSubmatch(line)
			if nm == nil {
				continue
			}
			routeID := nm[1]
			// Look ahead up to 10 lines for path:, methods:, controller:
			block := strings.Join(lines[i:min(i+12, len(lines))], "\n")
			pathM := reSymYAMLRoutePath.FindStringSubmatch(block)
			if pathM == nil {
				continue
			}
			routePath := strings.TrimSpace(pathM[1])
			methods := []string{"GET"} // default
			if mM := reSymYAMLRouteMethods.FindStringSubmatch(block); mM != nil {
				// mM[1] is the raw content inside [...], e.g. "GET" or "POST, PUT"
				parsed := symSplitMethodList(mM[1])
				if len(parsed) > 0 {
					methods = parsed
				}
			}
			_ = reSymYAMLRouteController // used for provenance note
			for _, method := range methods {
				name := method + " " + routePath
				ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, i+1)
				setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_YAML_ROUTE",
					"route_path", routePath, "http_method", method, "route_name", routeID,
					"route_style", "yaml")
				add(ent)
			}
		}
		span.SetAttributes(attribute.Int("entity_count", len(entities)))
		return entities, nil
	}

	// -----------------------------------------------------------------------
	// security.yaml extraction
	// -----------------------------------------------------------------------
	if isSecYAML {
		// access_control entries
		for _, m := range reSymSecurityAccessControl.FindAllStringSubmatchIndex(src, -1) {
			path := strings.TrimSpace(src[m[2]:m[3]])
			roles := strings.TrimSpace(src[m[4]:m[5]])
			name := "access_control:" + path
			ent := makeEntity(name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_SECURITY_YAML",
				"path_pattern", path, "roles", roles)
			add(ent)
		}
		span.SetAttributes(attribute.Int("entity_count", len(entities)))
		return entities, nil
	}

	// -----------------------------------------------------------------------
	// PHP source extraction
	// -----------------------------------------------------------------------

	// 1. PHP8 attribute routes with full method+name extraction
	for _, m := range reSymRouteAttrFull.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		body := src[m[4]:m[5]]
		methods := symParseRouteMethods(body)
		routeName := symParseRouteName(body)

		if len(methods) == 0 {
			// Emit one endpoint without method prefix (matches existing tests)
			ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ROUTE",
				"route_path", path, "route_style", "attribute")
			if routeName != "" {
				setProps(&ent, "route_name", routeName)
			}
			add(ent)
		} else {
			for _, method := range methods {
				name := method + " " + path
				ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
				setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ROUTE",
					"route_path", path, "http_method", method, "route_style", "attribute")
				if routeName != "" {
					setProps(&ent, "route_name", routeName)
				}
				add(ent)
			}
			// Also emit a bare path entity for backward-compat deduplication
			bareEnt := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&bareEnt, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ROUTE",
				"route_path", path, "route_style", "attribute")
			if routeName != "" {
				setProps(&bareEnt, "route_name", routeName)
			}
			add(bareEnt)
		}
	}

	// 2. Annotation routes @Route("/path",...) with method+name
	for _, m := range reSymRouteAnnotFull.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		body := src[m[4]:m[5]]
		methods := symParseRouteMethods(body)
		routeName := symParseRouteName(body)

		if len(methods) == 0 {
			ent := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ROUTE",
				"route_path", path, "route_style", "annotation")
			if routeName != "" {
				setProps(&ent, "route_name", routeName)
			}
			add(ent)
		} else {
			for _, method := range methods {
				name := method + " " + path
				ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
				setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ROUTE",
					"route_path", path, "http_method", method, "route_style", "annotation")
				if routeName != "" {
					setProps(&ent, "route_name", routeName)
				}
				add(ent)
			}
			bareEnt := makeEntity(path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&bareEnt, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ROUTE",
				"route_path", path, "route_style", "annotation")
			if routeName != "" {
				setProps(&bareEnt, "route_name", routeName)
			}
			add(bareEnt)
		}
	}

	// 3. Controller classes -> SCOPE.Component
	for _, m := range reSymController.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_CONTROLLER")
		add(ent)
	}

	// 4. Doctrine entities
	entityMatches := reSymEntity.FindAllStringIndex(src, -1)
	for _, em := range entityMatches {
		rest := src[em[0]:]
		cm := reSymEntityClass.FindStringSubmatch(rest)
		if cm != nil {
			name := cm[1]
			ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, em[0]))
			setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ENTITY")
			add(ent)
		}
	}

	// 5. ORM relations -> SCOPE.Component
	for _, m := range reSymORMRelation.FindAllStringSubmatchIndex(src, -1) {
		relType := src[m[2]:m[3]]
		ent := makeEntity("relation:"+relType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_RELATION",
			"relation_type", relType)
		add(ent)
	}

	// 6. EventSubscriberInterface implementors -> SCOPE.Pattern
	//    + extract event names from getSubscribedEvents
	subscriberMatches := reSymEventSubscriber.FindAllStringIndex(src, -1)
	for _, sm := range subscriberMatches {
		prefix := src[:sm[0]]
		cm := reSymSubscriberClass.FindAllStringSubmatch(prefix, -1)
		if len(cm) > 0 {
			name := cm[len(cm)-1][1]
			ent := makeEntity(name, "SCOPE.Pattern", "event_subscriber", file.Path, file.Language, lineOf(src, sm[0]))
			setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_EVENT_SUBSCRIBER")
			add(ent)

			// Extract individual subscribed events
			rest := src[sm[0]:]
			if evM := reSymSubscribedEvents.FindStringSubmatch(rest); evM != nil {
				body := evM[1]
				for _, evK := range reSymEventKey.FindAllStringSubmatch(body, -1) {
					eventName := evK[1]
					evEnt := makeEntity("event:"+eventName, "SCOPE.Pattern", "event", file.Path, file.Language, lineOf(src, sm[0]))
					setProps(&evEnt, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_SUBSCRIBED_EVENT",
						"event_name", eventName, "subscriber", name)
					add(evEnt)
				}
			}
		}
	}

	// 7. Kernel event listeners (addListener / addSubscriber)
	for _, m := range reSymKernelListener.FindAllStringSubmatchIndex(src, -1) {
		eventName := src[m[2]:m[3]]
		ent := makeEntity("event:"+eventName, "SCOPE.Pattern", "event", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_KERNEL_LISTENER",
			"event_name", eventName)
		add(ent)
	}
	for _, m := range reSymKernelSubscriber.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "event_subscriber", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_KERNEL_SUBSCRIBER")
		add(ent)
	}

	// 8. Message handlers -> SCOPE.Service
	handlerMatches := reSymMessageHandler.FindAllStringIndex(src, -1)
	for _, hm := range handlerMatches {
		prefix := src[:hm[0]]
		cm := reSymServiceClass.FindAllStringSubmatch(prefix, -1)
		if len(cm) > 0 {
			name := cm[len(cm)-1][1]
			ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, hm[0]))
			setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_MESSAGE_HANDLER")
			add(ent)
		}
	}

	// -----------------------------------------------------------------------
	// Auth extraction
	// -----------------------------------------------------------------------

	// 9. #[IsGranted('ROLE_ADMIN')]
	for _, m := range reSymIsGrantedAttr.FindAllStringSubmatchIndex(src, -1) {
		role := src[m[2]:m[3]]
		ent := makeEntity("isgranted:"+role, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_IS_GRANTED",
			"role", role)
		add(ent)
	}

	// 10. $this->denyAccessUnlessGranted('...')
	for _, m := range reSymDenyAccessUnlessGranted.FindAllStringSubmatchIndex(src, -1) {
		role := src[m[2]:m[3]]
		ent := makeEntity("deny_unless_granted:"+role, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_DENY_ACCESS",
			"role", role)
		add(ent)
	}

	// 11. Voter classes (extends Voter or implements VoterInterface)
	for _, m := range reSymVoter.FindAllStringSubmatchIndex(src, -1) {
		// Group 2 is the name from extends Voter, group 4 from implements VoterInterface
		name := ""
		if m[2] != -1 {
			name = src[m[2]:m[3]]
		} else if m[4] != -1 {
			name = src[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		ent := makeEntity("voter:"+name, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_VOTER",
			"voter_class", name)
		add(ent)

		// Extract supported attributes from case 'ATTRIBUTE': inside the class
		// Find the class body after this match
		rest := src[m[0]:]
		for _, vS := range reSymVoterSupports.FindAllStringSubmatch(rest, -1) {
			attr := vS[1]
			attrEnt := makeEntity("voter_attr:"+attr, "SCOPE.Pattern", "auth", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&attrEnt, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_VOTER_ATTR",
				"voter_class", name, "voter_attribute", attr)
			add(attrEnt)
		}
	}

	// -----------------------------------------------------------------------
	// Validation extraction
	// -----------------------------------------------------------------------

	// 12. Assert constraints as PHP8 attributes
	for _, m := range reSymAssertAttr.FindAllStringSubmatchIndex(src, -1) {
		constraint := src[m[2]:m[3]]
		args := ""
		if m[4] != -1 {
			args = strings.TrimSpace(src[m[4]:m[5]])
		}
		name := "assert:" + constraint
		if args != "" {
			name = "assert:" + constraint + "(" + args + ")"
		}
		ent := makeEntity(name, "SCOPE.Pattern", "validation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ASSERT",
			"constraint", constraint)
		if args != "" {
			setProps(&ent, "constraint_args", args)
		}
		add(ent)
	}

	// 13. Assert constraints as annotations
	for _, m := range reSymAssertAnnot.FindAllStringSubmatchIndex(src, -1) {
		constraint := src[m[2]:m[3]]
		args := ""
		if m[4] != -1 {
			args = strings.TrimSpace(src[m[4]:m[5]])
		}
		name := "assert:" + constraint
		if args != "" {
			name = "assert:" + constraint + "(" + args + ")"
		}
		ent := makeEntity(name, "SCOPE.Pattern", "validation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_ASSERT_ANNOT",
			"constraint", constraint)
		if args != "" {
			setProps(&ent, "constraint_args", args)
		}
		add(ent)
	}

	// 14. $validator->validate($obj) — programmatic validation call
	if reSymValidatorValidate.MatchString(src) {
		ent := makeEntity("symfony:validator_validate", "SCOPE.Pattern", "validation", file.Path, file.Language, 1)
		setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_VALIDATOR_VALIDATE")
		add(ent)
	}

	// 15. DTO / Request classes carrying validation
	if reSymValidatorUse.MatchString(src) || reSymAssertAttr.MatchString(src) || reSymAssertAnnot.MatchString(src) {
		for _, m := range reSymDTOClass.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			ent := makeEntity("dto:"+name, "SCOPE.Component", "dto", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "symfony", "provenance", "INFERRED_FROM_SYMFONY_DTO")
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
