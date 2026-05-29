package java

import (
	"context"
	"regexp"
	"strings"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// MyBatis. Unlike JPA ORMs, MyBatis maps SQL to interface methods. Two modes
// are extracted:
//
//   - Annotation mode: a @Mapper interface whose methods carry
//     @Select/@Insert/@Update/@Delete with inline SQL, and @Results/@Result
//     result-map declarations.
//   - XML mapper mode: a *.xml file whose root is <mapper namespace="..."> with
//     <select>/<insert>/<update>/<delete>/<resultMap> child elements. The XML is
//     classified by archigraph as language "xml"; this extractor parses it too.
//
// Each mapped statement becomes a SCOPE.Operation "query" entity carrying its
// CRUD verb and (where present) the inline SQL or statement id. Result maps
// become SCOPE.Schema "result_map" entities so result_mapping is recoverable.

func init() {
	extreg.Register("custom_java_mybatis", &myBatisExtractor{})
}

type myBatisExtractor struct{}

func (e *myBatisExtractor) Language() string { return "custom_java_mybatis" }

var (
	// @Mapper public interface FooMapper
	mybatisMapperRE = regexp.MustCompile(
		`(?s)@Mapper\b(?:\s*\([^)]*\))?(?:[^{]|\{[^}]*\})*?interface\s+(\w+)`)
	// @Select("SELECT ...") <ret> methodName(  -- captures verb annotation, SQL, method
	mybatisAnnoStmtRE = regexp.MustCompile(
		`(?s)@(Select|Insert|Update|Delete|SelectProvider|InsertProvider|UpdateProvider|DeleteProvider)\s*\(\s*` +
			`(?:"((?:[^"\\]|\\.)*)"|\{[^}]*\}|[^)]*)\)` +
			`(?:\s*@\w+(?:\s*\([^)]*\))?)*` +
			`\s+(?:[\w.<>,\[\]\s]+?)\s+(\w+)\s*\(`)
	// @Results(id = "fooMap", value = {...})
	mybatisResultsRE = regexp.MustCompile(
		`@Results\s*\(\s*(?:id\s*=\s*"([^"]*)")?`)
	// XML: <mapper namespace="com.app.FooMapper">
	mybatisXMLNamespaceRE = regexp.MustCompile(
		`<mapper\s+namespace\s*=\s*"([^"]+)"`)
	// XML: <select id="findById" ...> ... </select> for each CRUD tag.
	mybatisXMLStmtRE = regexp.MustCompile(
		`(?s)<(select|insert|update|delete)\b[^>]*?\bid\s*=\s*"([^"]+)"[^>]*>`)
	// XML: <resultMap id="fooMap" type="Foo">
	mybatisXMLResultMapRE = regexp.MustCompile(
		`<resultMap\b[^>]*?\bid\s*=\s*"([^"]+)"(?:[^>]*?\btype\s*=\s*"([^"]+)")?`)
)

func (e *myBatisExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	fp := file.Path

	switch lang {
	case "java":
		// Only fire when a MyBatis fingerprint is present.
		if !strings.Contains(src, "@Mapper") && !strings.Contains(src, "@Select") &&
			!strings.Contains(src, "@Insert") && !strings.Contains(src, "org.apache.ibatis") {
			return nil, nil
		}
		return e.extractAnnotations(src, fp, file.Language), nil
	case "xml", "html":
		// MyBatis XML mappers are valid XML; gate on the mapper namespace marker.
		if !strings.Contains(src, "<mapper") || !mybatisXMLNamespaceRE.MatchString(src) {
			return nil, nil
		}
		return e.extractXML(src, fp, file.Language), nil
	default:
		return nil, nil
	}
}

func (e *myBatisExtractor) extractAnnotations(src, fp, language string) []types.EntityRecord {
	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// @Mapper interfaces.
	type mapperInfo struct {
		name   string
		offset int
	}
	var mappers []mapperInfo
	for _, m := range mybatisMapperRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "mapper", fp, language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mybatis", "provenance", "INFERRED_FROM_MYBATIS_MAPPER")
		add(ent)
		mappers = append(mappers, mapperInfo{name, m[0]})
	}
	owningMapper := func(offset int) string {
		var best string
		for _, mp := range mappers {
			if mp.offset <= offset {
				best = mp.name
			}
		}
		return best
	}

	// @Select / @Insert / @Update / @Delete mapped statements.
	for _, m := range mybatisAnnoStmtRE.FindAllStringSubmatchIndex(src, -1) {
		anno := src[m[2]:m[3]]
		var sql string
		if m[4] >= 0 {
			sql = src[m[4]:m[5]]
		}
		method := src[m[6]:m[7]]
		mapper := owningMapper(m[0])
		verb := strings.ToLower(strings.TrimSuffix(anno, "Provider"))
		name := mapper + "." + method
		if mapper == "" {
			name = method
		}
		ent := makeEntity(name, "SCOPE.Operation", "query", fp, language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mybatis", "verb", verb, "method", method,
			"mapper", mapper, "mode", "annotation",
			"provenance", "INFERRED_FROM_MYBATIS_ANNOTATION_STATEMENT")
		if sql != "" {
			setProps(&ent, "sql", sql)
		}
		if strings.HasSuffix(anno, "Provider") {
			setProps(&ent, "provider", "true")
		}
		add(ent)
	}

	// @Results result maps.
	for _, m := range mybatisResultsRE.FindAllStringSubmatchIndex(src, -1) {
		id := ""
		if m[2] >= 0 {
			id = src[m[2]:m[3]]
		}
		mapper := owningMapper(m[0])
		name := "results"
		if id != "" {
			name = id
		} else if mapper != "" {
			name = mapper + ".results"
		}
		ent := makeEntity(name, "SCOPE.Schema", "result_map", fp, language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mybatis", "mapper", mapper, "mode", "annotation",
			"provenance", "INFERRED_FROM_MYBATIS_RESULTS")
		add(ent)
	}

	return entities
}

func (e *myBatisExtractor) extractXML(src, fp, language string) []types.EntityRecord {
	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	namespace := ""
	if m := mybatisXMLNamespaceRE.FindStringSubmatch(src); m != nil {
		namespace = m[1]
	}
	// Namespace -> mapper component.
	nsEnt := makeEntity(namespace, "SCOPE.Component", "mapper", fp, language, lineOf(src, strings.Index(src, "<mapper")))
	setProps(&nsEnt, "framework", "mybatis", "namespace", namespace, "mode", "xml",
		"provenance", "INFERRED_FROM_MYBATIS_XML_MAPPER")
	add(nsEnt)

	// CRUD statements.
	for _, m := range mybatisXMLStmtRE.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		id := src[m[4]:m[5]]
		name := id
		if namespace != "" {
			name = namespace + "." + id
		}
		ent := makeEntity(name, "SCOPE.Operation", "query", fp, language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mybatis", "verb", verb, "statement_id", id,
			"namespace", namespace, "mode", "xml",
			"provenance", "INFERRED_FROM_MYBATIS_XML_STATEMENT")
		add(ent)
	}

	// Result maps.
	for _, m := range mybatisXMLResultMapRE.FindAllStringSubmatchIndex(src, -1) {
		id := src[m[2]:m[3]]
		typ := ""
		if m[4] >= 0 {
			typ = src[m[4]:m[5]]
		}
		ent := makeEntity(id, "SCOPE.Schema", "result_map", fp, language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mybatis", "result_type", typ, "namespace", namespace,
			"mode", "xml", "provenance", "INFERRED_FROM_MYBATIS_XML_RESULT_MAP")
		add(ent)
	}

	return entities
}
