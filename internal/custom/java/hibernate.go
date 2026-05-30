package java

import "regexp"

// Hibernate / JPA deep custom extractor.
// Ported from: hibernate_extractor.py

var hibernateFrameworks = map[string]bool{
	"hibernate": true, "jpa": true, "spring_data_jpa": true,
	"spring-data-jpa": true, "springdatajpa": true,
}

// kotlinHibernateFrameworks mirrors hibernateFrameworks for Kotlin ORM records
// (lang.kotlin.orm.hibernate, lang.kotlin.orm.spring-data). Kotlin uses the
// same JPA/Hibernate annotations (@Entity, @OneToMany, @JoinColumn, etc.)
// before data class / class declarations, so the extractor applies unchanged.
var kotlinHibernateFrameworks = map[string]bool{
	"hibernate": true, "jpa": true, "spring_data_jpa": true,
	"spring-data-jpa": true, "springdatajpa": true,
}

var (
	hibEntityClassRE = regexp.MustCompile(
		`(?s)@Entity\b(?:[^{]|\{[^}]*\})*?class\s+(\w+)`)
	hibTableNameRE = regexp.MustCompile(
		`(?s)@Table\s*\([^)]*name\s*=\s*\"([^\"]+)\"`)
	// hibAssociationRE matches @OneToMany/@ManyToOne/@OneToOne/@ManyToMany
	// association annotations. Accepts both:
	//   Java:   private List<Employee> employees;
	//   Kotlin: var employees: List<Employee>? = null  (no trailing semicolon)
	// Group 1 = annotation name, group 2 = generic target type, group 3 = raw
	// target type (non-generic), group 4 = field/property name.
	hibAssociationRE = regexp.MustCompile(
		`(?s)@(OneToMany|ManyToOne|OneToOne|ManyToMany)\b(?:\s*\([^)]*\))?` +
			`\s*(?:@\w+(?:\s*\([^)]*\))?\s*)*(?:private|protected|public|var|val|)\s+` +
			`(?:(?:final|transient)\s+)*(?:\w+<(\w+)>|(\w+))\s+(\w+)(?:\s*[:?=;])`)
	hibNamedQueryRE = regexp.MustCompile(
		`(?s)@NamedQuery\s*\(\s*` +
			`(?:name\s*=\s*\"(?P<name>[^\"]*)\"\s*,\s*query\s*=\s*\"(?P<query>[^\"]*)\"` +
			`|query\s*=\s*\"(?P<query2>[^\"]*)\"\s*,\s*name\s*=\s*\"(?P<name2>[^\"]*)\")`)
	hibCriteriaBuilderRE = regexp.MustCompile(
		`(?s)\bCriteriaBuilder\b\s*(?:<[^>]*>)?\s+(\w+)\s*=`)
	hibCriteriaQueryRE = regexp.MustCompile(
		`(?s)\bCriteriaQuery\b\s*<(\w+)>\s+(\w+)\s*=`)
	hibCacheableRE = regexp.MustCompile(
		`(?s)@(?:Cacheable|Cache)\b[^{]*?class\s+(\w+)`)
	hibConverterRE = regexp.MustCompile(
		`(?s)@Converter\b(?:\s*\([^)]*\))?(?:[^{]|\{[^}]*\})*?class\s+(\w+)`)
	hibEntityManagerRE = regexp.MustCompile(
		`\bEntityManager\b\s+(\w+)\s*[=;,)]`)
	hibEmWriteRE = regexp.MustCompile(
		`(?m)\b(\w+)\.(persist|merge|remove)\s*\(\s*(\w+)`)
)

// ExtractHibernate runs the Hibernate/JPA extractor.
// Accepts both Java and Kotlin source: Kotlin JPA entities use the same
// @Entity, @Table, @OneToMany, @ManyToOne, @JoinColumn annotations before
// class / data class declarations (regex patterns match identically).
func ExtractHibernate(ctx PatternContext) PatternResult {
	var result PatternResult
	if (ctx.Language != "java" && ctx.Language != "kotlin") || !hibernateFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// Cacheable classes
	cacheableNames := make(map[string]bool)
	for _, m := range hibCacheableRE.FindAllStringSubmatchIndex(source, -1) {
		cacheableNames[source[m[2]:m[3]]] = true
	}

	// 1. @Entity classes
	type entityInfo struct {
		name   string
		ref    string
		offset int
	}
	var entities []entityInfo
	for _, m := range hibEntityClassRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:schema:hibernate_entity:" + fp + ":" + name
		if seenRefs[ref] {
			continue
		}
		seenRefs[ref] = true

		// Table name
		snippet := source[max(0, m[0]-600):m[1]]
		var tableName string
		if tm := hibTableNameRE.FindStringSubmatch(snippet); tm != nil {
			tableName = tm[1]
		}

		props := map[string]any{"framework": "hibernate"}
		if tableName != "" {
			props["table_name"] = tableName
		}
		if cacheableNames[name] {
			props["l2_cache"] = true
		}

		entities = append(entities, entityInfo{name, ref, m[0]})
		result.Entities = append(result.Entities, SecondaryEntity{
			Name: name, Kind: "SCOPE.Schema", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_HIBERNATE_ENTITY", Ref: ref,
			Properties: props,
		})
	}

	findOwningEntity := func(offset int) (string, string) {
		var bestName, bestRef string
		for _, e := range entities {
			if e.offset <= offset {
				bestName = e.name
				bestRef = e.ref
			}
		}
		return bestName, bestRef
	}

	// 2. JPA associations
	for _, m := range hibAssociationRE.FindAllStringSubmatchIndex(source, -1) {
		ann := source[m[2]:m[3]]
		var targetType string
		if m[4] >= 0 {
			targetType = source[m[4]:m[5]]
		} else if m[6] >= 0 {
			targetType = source[m[6]:m[7]]
		}
		if targetType == "" {
			continue
		}

		_, ownerRef := findOwningEntity(m[0])
		if ownerRef == "" {
			continue
		}
		targetRef := "scope:schema:hibernate_entity:" + fp + ":" + targetType
		addRel(&result, seenRels, Relationship{
			SourceRef: ownerRef, TargetRef: targetRef,
			RelationshipType: "DEPENDS_ON",
			Properties: map[string]string{
				"association_kind": ann, "field_type": targetType,
				"provenance": "INFERRED_FROM_HIBERNATE_ASSOCIATION",
			},
		})
	}

	// 3. @NamedQuery
	namedQueryNames := hibNamedQueryRE.SubexpNames()
	for _, m := range hibNamedQueryRE.FindAllStringSubmatchIndex(source, -1) {
		var queryName, hql string
		for i, name := range namedQueryNames {
			if m[2*i] < 0 {
				continue
			}
			switch name {
			case "name", "name2":
				queryName = source[m[2*i]:m[2*i+1]]
			case "query", "query2":
				hql = source[m[2*i]:m[2*i+1]]
			}
		}
		entityName, entityRef := findOwningEntity(m[0])
		if entityName == "" {
			entityName = "Unknown"
		}

		ref := "scope:operation:hibernate_named_query:" + fp + ":" + entityName + "." + queryName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: entityName + "." + queryName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_HIBERNATE_HQL", Ref: ref,
			Properties: map[string]any{
				"query_name": queryName, "hql": hql, "framework": "hibernate",
			},
		}) {
			if entityRef != "" {
				addRel(&result, seenRels, Relationship{
					SourceRef: entityRef, TargetRef: ref, RelationshipType: "OWNS",
				})
			}
		}
	}

	// 4. CriteriaBuilder / CriteriaQuery
	criteriaIdx := 0
	seenCriteriaVars := make(map[string]bool)
	for _, m := range hibCriteriaBuilderRE.FindAllStringSubmatchIndex(source, -1) {
		varName := source[m[2]:m[3]]
		if seenCriteriaVars[varName] {
			continue
		}
		seenCriteriaVars[varName] = true
		ref := "scope:operation:hibernate_criteria:" + fp + ":" + varName + "_" + itoa(criteriaIdx)
		criteriaIdx++
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: "criteria." + varName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_HIBERNATE_CRITERIA", Ref: ref,
			Properties: map[string]any{
				"criteria_var": varName, "criteria_kind": "CriteriaBuilder",
				"framework": "hibernate",
			},
		})
	}
	for _, m := range hibCriteriaQueryRE.FindAllStringSubmatchIndex(source, -1) {
		rootType := source[m[2]:m[3]]
		varName := source[m[4]:m[5]]
		if seenCriteriaVars[varName] {
			continue
		}
		seenCriteriaVars[varName] = true
		ref := "scope:operation:hibernate_criteria:" + fp + ":" + varName + "_" + itoa(criteriaIdx)
		criteriaIdx++
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: "criteria." + varName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_HIBERNATE_CRITERIA", Ref: ref,
			Properties: map[string]any{
				"criteria_var": varName, "criteria_kind": "CriteriaQuery",
				"root_type": rootType, "framework": "hibernate",
			},
		})
		rootEntityRef := "scope:schema:hibernate_entity:" + fp + ":" + rootType
		addRel(&result, seenRels, Relationship{
			SourceRef: ref, TargetRef: rootEntityRef, RelationshipType: "READS_FROM",
			Properties: map[string]string{"provenance": "INFERRED_FROM_HIBERNATE_CRITERIA"},
		})
	}

	// 5. @Converter
	for _, m := range hibConverterRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:component:hibernate_converter:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_HIBERNATE_CONVERTER", Ref: ref,
			Properties: map[string]any{"framework": "hibernate"},
		})
	}

	// 6. EntityManager
	seenEMClasses := make(map[string]bool)
	for _, m := range hibEntityManagerRE.FindAllStringSubmatchIndex(source, -1) {
		cls := findEnclosingClass(source, m[0])
		if cls == "" || seenEMClasses[cls] {
			continue
		}
		seenEMClasses[cls] = true
		ref := "scope:service:hibernate_em:" + fp + ":" + cls
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: cls, Kind: "SCOPE.Service", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_HIBERNATE_EM", Ref: ref,
			Properties: map[string]any{"em_kind": "EntityManager", "framework": "hibernate"},
		})
	}

	// 7. EntityManager write ops
	for _, m := range hibEmWriteRE.FindAllStringSubmatchIndex(source, -1) {
		emVar := source[m[2]:m[3]]
		opName := source[m[4]:m[5]]
		argName := source[m[6]:m[7]]
		lineNo := lineOf(source, m[0])
		opRef := "scope:operation:hibernate_em_write:" + fp + ":" + emVar + ":" + opName + ":" + itoa(lineNo)
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: emVar + "." + opName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineNo, LineEnd: lineNo,
			Provenance: "INFERRED_FROM_HIBERNATE_EM_WRITE", Ref: opRef,
			Properties: map[string]any{
				"em_var": emVar, "operation": opName, "argument": argName,
				"framework": "hibernate",
			},
		}) {
			targetEntityRef := "scope:schema:hibernate_entity:" + fp + ":" + argName
			addRel(&result, seenRels, Relationship{
				SourceRef: opRef, TargetRef: targetEntityRef, RelationshipType: "WRITES_TO",
				Properties: map[string]string{"provenance": "INFERRED_FROM_HIBERNATE_EM_WRITE"},
			})
		}
	}

	// 8. FK + lazy-loading (foreign_key_extraction / lazy_loading_recognition)
	hibOwnerFn := func(offset int) string {
		name, _ := findOwningEntity(offset)
		return name
	}
	fkResult := ExtractJPAFKAndLazy(source, hibOwnerFn)
	emitJPAFKLazy(fkResult, fp, "java", "hibernate", &result.Entities, seenRefs)

	return result
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
