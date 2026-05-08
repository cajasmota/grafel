package java

// Lombok annotation inference: detect class-level Lombok annotations and
// generate synthetic getter/setter/constructor/builder entities.
//
// Ported from: memx_indexer/languages/java/parser.py (_infer_lombok_entities)

// LombokInfer detects Lombok annotations on a class and returns synthetic
// method entities that Lombok would generate at compile time.
//
// Annotations handled:
//   - @Data      -> getters, setters, equals, hashCode, toString
//   - @Value     -> getters, equals, hashCode, toString
//   - @Getter    -> getters
//   - @Setter    -> setters
//   - @EqualsAndHashCode -> equals, hashCode
//   - @ToString  -> toString
//   - @Builder   -> builder(), build()
//   - @AllArgsConstructor -> allArgsConstructor
//   - @NoArgsConstructor  -> noArgsConstructor
func LombokInfer(className string, annotations []string, filePath string, startLine int) []SecondaryEntity {
	annSet := make(map[string]bool, len(annotations))
	for _, a := range annotations {
		annSet[a] = true
	}

	var entities []SecondaryEntity

	make := func(methodName, signature string) SecondaryEntity {
		return SecondaryEntity{
			Name:       methodName,
			Kind:   "SCOPE.Operation",
			Subtype: "method",
			SourceFile: filePath,
			LineStart:  startLine,
			LineEnd:    startLine,
			Provenance: "INFERRED_FROM_LOMBOK",
			Ref:        "scope:operation:lombok:" + filePath + ":" + className + "." + methodName,
			Properties: map[string]any{
				"framework": "lombok",
				"class":     className,
			},
		}
	}

	// @Getter / @Data / @Value -> getters
	if annSet["@Data"] || annSet["@Value"] || annSet["@Getter"] {
		entities = append(entities, make("get*", "[Lombok] getters for "+className+" fields"))
	}

	// @Setter / @Data -> setters
	if annSet["@Data"] || annSet["@Setter"] {
		entities = append(entities, make("set*", "[Lombok] setters for "+className+" fields"))
	}

	// @Data / @Value / @EqualsAndHashCode -> equals + hashCode
	if annSet["@Data"] || annSet["@Value"] || annSet["@EqualsAndHashCode"] {
		entities = append(entities, make("equals", "boolean equals(Object o)  // @EqualsAndHashCode for "+className))
		entities = append(entities, make("hashCode", "int hashCode()  // @EqualsAndHashCode for "+className))
	}

	// @Data / @Value / @ToString -> toString
	if annSet["@Data"] || annSet["@Value"] || annSet["@ToString"] {
		entities = append(entities, make("toString", "String toString()  // @ToString for "+className))
	}

	// @Builder -> builder() + build()
	if annSet["@Builder"] {
		entities = append(entities, make("builder", "static "+className+"."+className+"Builder builder()  // @Builder"))
		entities = append(entities, make("build", className+" build()  // @Builder"))
	}

	// @AllArgsConstructor
	if annSet["@AllArgsConstructor"] {
		entities = append(entities, make("allArgsConstructor", className+"(all fields)  // @AllArgsConstructor"))
	}

	// @NoArgsConstructor
	if annSet["@NoArgsConstructor"] {
		entities = append(entities, make("noArgsConstructor", className+"()  // @NoArgsConstructor"))
	}

	return entities
}
