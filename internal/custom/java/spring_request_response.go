package java

import (
	"regexp"
	"strings"
)

// srrSplitParams splits a Java handler parameter list at top-level commas,
// respecting `<>` generic args and `()` annotation args so a param like
// `Map<String,Object> m` or `@RequestParam(value="x") String x` stays intact.
func srrSplitParams(paramsBlock string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(paramsBlock); i++ {
		switch paramsBlock[i] {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, paramsBlock[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, paramsBlock[start:])
	return parts
}

// Spring Request/Response extractor: ACCEPTS_INPUT and RETURNS relationships.
// Ported from: spring_request_response_extractor.py

var springReqRespFrameworks = map[string]bool{
	"spring_boot": true, "spring-boot": true, "springboot": true,
	"spring_mvc": true, "spring-mvc": true, "springmvc": true,
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
}

var (
	srrControllerRE = regexp.MustCompile(
		`(?s)@(?:Rest)?Controller\b[^{]*?(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	srrHTTPMethodRE = regexp.MustCompile(
		`(?s)(@(?:GetMapping|PostMapping|PutMapping|DeleteMapping|PatchMapping|RequestMapping)` +
			`\s*(?:\([^)]*\))?)\s*(?:(?:public|protected|private)\s+)?(?:static\s+)?` +
			`(?:<[^>]*>\s*)?([\w<>\[\], ]+?)\s+(\w+)\s*\(([^)]*)`)
	srrRequestBodyRE = regexp.MustCompile(
		`@RequestBody\b(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s+(\w+)(?:\s*<[^>]*>)?\s+\w+`)
	// #4475 — Spring's GET-side request DTO: a command/form object bound via
	// `@ModelAttribute FooQuery foo`. Group 1 is the DTO type. The leading
	// `@ModelAttribute` may itself carry args (`@ModelAttribute("cmd")`).
	srrModelAttributeRE = regexp.MustCompile(
		`@ModelAttribute\b(?:\s*\([^)]*\))?(?:\s*@\w+(?:\s*\([^)]*\))?\s*)*\s+(\w+)(?:\s*<[^>]*>)?\s+\w+`)
	// srrParamSplitRE splits a handler param list at top-level commas so each
	// param can be classified (annotated vs bare command object). Generic
	// commas (`Map<K,V>`) are handled by srrSplitParams, not this RE.
	srrAnnotatedParamRE = regexp.MustCompile(`@(\w+)\b`)
	// srrBareParamRE matches a `Type name` param with no annotation. Group 1 is
	// the type (ignoring generic args), group 2 the identifier.
	srrBareParamRE = regexp.MustCompile(`^([A-Z]\w*)(?:\s*<[^>]*>)?\s+(\w+)\s*$`)
	srrResponseEntityRE = regexp.MustCompile(`ResponseEntity\s*<\s*([\w<>, ]+?)\s*>`)
	srrGenericWrapperRE = regexp.MustCompile(`(?:Optional|Mono|Flux|Publisher)\s*<\s*([\w<>, ]+?)\s*>`)
	srrBaseGenericRE    = regexp.MustCompile(`^(\w+)(?:\s*<([^>]+)>)?$`)
)

var srrSkipTypes = map[string]bool{
	"void": true, "Void": true, "int": true, "long": true, "double": true,
	"float": true, "boolean": true, "char": true, "byte": true, "short": true,
	"String": true, "Integer": true, "Long": true, "Double": true, "Float": true,
	"Boolean": true, "Object": true, "List": true, "Map": true, "Set": true,
	"Collection": true, "Optional": true, "Iterable": true, "Stream": true,
	"ResponseEntity": true, "HttpEntity": true, "HttpStatus": true,
	"ModelAndView": true, "Model": true, "RedirectAttributes": true,
	"BindingResult": true, "HttpServletRequest": true, "HttpServletResponse": true,
	"HttpHeaders": true, "MultiValueMap": true, "Mono": true, "Flux": true,
	"Publisher": true, "ServerResponse": true, "ServerRequest": true,
}

// ExtractSpringRequestResponse runs the Spring request/response extractor.
func ExtractSpringRequestResponse(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !springReqRespFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath

	if !srrControllerRE.MatchString(source) {
		return result
	}

	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// Controller offsets
	type ctrlEntry struct {
		name   string
		offset int
	}
	var controllers []ctrlEntry
	for _, m := range srrControllerRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		found := false
		for _, c := range controllers {
			if c.name == name {
				found = true
				break
			}
		}
		if !found {
			controllers = append(controllers, ctrlEntry{name, m[0]})
		}
	}

	findOwner := func(offset int) string {
		var owner string
		for _, c := range controllers {
			if c.offset <= offset {
				owner = c.name
			}
		}
		return owner
	}

	ensureDTO := func(dtoName string, lineNo int) string {
		ref := "scope:schema:spring_dto:" + fp + ":" + dtoName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: dtoName, Kind: "SCOPE.Schema", SourceFile: fp,
			LineStart: lineNo, LineEnd: lineNo,
			Provenance: "INFERRED_FROM_SPRING_REQUEST_RESPONSE", Ref: ref,
			Properties: map[string]any{"kind": "dto", "framework": "spring"},
		})
		return ref
	}

	for _, m := range srrHTTPMethodRE.FindAllStringSubmatchIndex(source, -1) {
		returnTypeRaw := source[m[4]:m[5]]
		methodName := source[m[6]:m[7]]
		paramsBlock := source[m[8]:m[9]]
		lineNo := lineOf(source, m[0])

		controller := findOwner(m[0])
		if controller == "" {
			continue
		}
		endpointRef := "scope:operation:spring_boot_endpoint:" + fp + ":" + controller + "." + methodName

		// ACCEPTS_INPUT: @RequestBody
		if rbMatch := srrRequestBodyRE.FindStringSubmatch(paramsBlock); rbMatch != nil {
			dtoName := rbMatch[1]
			if !srrSkipTypes[dtoName] {
				dtoRef := ensureDTO(dtoName, lineNo)
				addRel(&result, seenRels, Relationship{
					SourceRef: endpointRef, TargetRef: dtoRef,
					RelationshipType: "ACCEPTS_INPUT",
					Properties:       map[string]string{"match_source": "request_body_annotation", "dto_type": dtoName},
				})
			}
		}

		// #4475 — ACCEPTS_INPUT for command/form objects: the Spring GET-side
		// request DTO. Two forms, both the analog of the NestJS @Query() DTO:
		//   (1) `@ModelAttribute FooQuery foo` — explicitly bound command object.
		//   (2) a bare command-object param (`FooQuery foo`) with no binding
		//       annotation — Spring binds these to request params implicitly.
		// Skip primitives / @RequestParam-/@PathVariable-bound scalars and the
		// framework injection types in srrSkipTypes. Conservative: only when the
		// type resolves to a known user DTO.
		for _, mam := range srrModelAttributeRE.FindAllStringSubmatch(paramsBlock, -1) {
			dtoName := mam[1]
			if srrSkipTypes[dtoName] {
				continue
			}
			dtoRef := ensureDTO(dtoName, lineNo)
			addRel(&result, seenRels, Relationship{
				SourceRef: endpointRef, TargetRef: dtoRef,
				RelationshipType: "ACCEPTS_INPUT",
				Properties:       map[string]string{"match_source": "model_attribute_annotation", "dto_type": dtoName},
			})
		}
		for _, param := range srrSplitParams(paramsBlock) {
			param = strings.TrimSpace(param)
			if param == "" {
				continue
			}
			// Annotated params are handled by their dedicated passes
			// (@RequestBody / @ModelAttribute above) or are scalar bindings
			// (@RequestParam/@PathVariable/@RequestHeader) we deliberately skip.
			if srrAnnotatedParamRE.MatchString(param) {
				continue
			}
			bm := srrBareParamRE.FindStringSubmatch(param)
			if bm == nil {
				continue
			}
			dtoName := bm[1]
			if srrSkipTypes[dtoName] {
				continue
			}
			dtoRef := ensureDTO(dtoName, lineNo)
			addRel(&result, seenRels, Relationship{
				SourceRef: endpointRef, TargetRef: dtoRef,
				RelationshipType: "ACCEPTS_INPUT",
				Properties:       map[string]string{"match_source": "command_object_param", "dto_type": dtoName},
			})
		}

		// RETURNS: method return type
		dtoName := unwrapReturnType(returnTypeRaw)
		if dtoName != "" && !srrSkipTypes[dtoName] {
			dtoRef := ensureDTO(dtoName, lineNo)
			addRel(&result, seenRels, Relationship{
				SourceRef: endpointRef, TargetRef: dtoRef,
				RelationshipType: "RETURNS",
				Properties:       map[string]string{"match_source": "return_type", "return_type_raw": returnTypeRaw, "dto_type": dtoName},
			})
		}
	}

	return result
}

func unwrapReturnType(raw string) string {
	// ResponseEntity<T>
	if m := srrResponseEntityRE.FindStringSubmatch(raw); m != nil {
		raw = m[1]
	}
	// Mono<T> / Flux<T> / Optional<T>
	if m := srrGenericWrapperRE.FindStringSubmatch(raw); m != nil {
		raw = m[1]
	}
	// Parse base<inner>
	if m := srrBaseGenericRE.FindStringSubmatch(raw); m != nil {
		base := m[1]
		inner := m[2]
		if srrSkipTypes[base] && inner != "" {
			raw = inner
			if m2 := srrBaseGenericRE.FindStringSubmatch(raw); m2 != nil {
				base = m2[1]
			}
		}
		if srrSkipTypes[base] {
			return ""
		}
		return base
	}
	return ""
}
