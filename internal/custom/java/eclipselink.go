package java

import (
	"context"
	"regexp"
	"strings"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// EclipseLink is the reference implementation of the JPA spec. It uses the
// same javax.persistence / jakarta.persistence annotations as Hibernate
// (@Entity, @Table, @OneToMany, @NamedQuery, ...) plus a few EclipseLink-only
// markers (@Cache from org.eclipse.persistence.annotations, @Customizer). This
// extractor parses model classes, JPA associations, and named/criteria queries
// and emits registry-shaped EntityRecords via the custom_java_ dispatch path.
//
// It self-gates on language=java AND the presence of either a JPA @Entity
// annotation together with an EclipseLink-specific marker, or an explicit
// persistence.xml provider reference, so it does not double-emit on plain
// Hibernate sources (which already have their own coverage record).

func init() {
	extreg.Register("custom_java_eclipselink", &eclipseLinkExtractor{})
}

type eclipseLinkExtractor struct{}

func (e *eclipseLinkExtractor) Language() string { return "custom_java_eclipselink" }

var (
	elEntityClassRE = regexp.MustCompile(
		`(?s)@Entity\b(?:[^{]|\{[^}]*\})*?class\s+(\w+)`)
	elTableNameRE = regexp.MustCompile(
		`(?s)@Table\s*\([^)]*name\s*=\s*"([^"]+)"`)
	elAssociationRE = regexp.MustCompile(
		`(?s)@(OneToMany|ManyToOne|OneToOne|ManyToMany)\b(?:\s*\([^)]*\))?` +
			`\s*(?:@\w+(?:\s*\([^)]*\))?\s*)*(?:private|protected|public|)\s+` +
			`(?:(?:final|transient)\s+)*(?:\w+<(\w+)>|(\w+))\s+(\w+)\s*;`)
	elNamedQueryRE = regexp.MustCompile(
		`(?s)@NamedQuery\s*\(\s*` +
			`(?:name\s*=\s*"(?P<name>[^"]*)"\s*,\s*query\s*=\s*"(?P<query>[^"]*)"` +
			`|query\s*=\s*"(?P<query2>[^"]*)"\s*,\s*name\s*=\s*"(?P<name2>[^"]*)")`)
	elCacheRE = regexp.MustCompile(
		`(?s)@Cache\b(?:\s*\([^)]*\))?(?:[^{]|\{[^}]*\})*?class\s+(\w+)`)
	// EclipseLink-only markers used as a gate signal.
	elMarkerRE = regexp.MustCompile(
		`org\.eclipse\.persistence|@Customizer\b|EclipseLink|eclipselink|persistence\.jdbc\.driver`)
)

// looksLikeEclipseLink returns true when the source carries an EclipseLink
// fingerprint, distinguishing it from a generic Hibernate/JPA source.
func looksLikeEclipseLink(src string) bool {
	return elMarkerRE.MatchString(src)
}

func (e *eclipseLinkExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	if strings.ToLower(file.Language) != "java" {
		return nil, nil
	}
	src := string(file.Content)
	if !looksLikeEclipseLink(src) {
		return nil, nil
	}
	fp := file.Path

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

	// Classes marked @Cache (EclipseLink L2 cache).
	cached := make(map[string]bool)
	for _, m := range elCacheRE.FindAllStringSubmatchIndex(src, -1) {
		cached[src[m[2]:m[3]]] = true
	}

	// @Entity model classes.
	type entInfo struct {
		name   string
		offset int
	}
	var ents []entInfo
	for _, m := range elEntityClassRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "entity", fp, file.Language, lineOf(src, m[0]))
		snippet := src[max(0, m[0]-600):m[1]]
		if tm := elTableNameRE.FindStringSubmatch(snippet); tm != nil {
			setProps(&ent, "table_name", tm[1])
		}
		if cached[name] {
			setProps(&ent, "l2_cache", "true")
		}
		setProps(&ent, "framework", "eclipselink",
			"provenance", "INFERRED_FROM_ECLIPSELINK_ENTITY")
		add(ent)
		ents = append(ents, entInfo{name, m[0]})
	}

	owningEntity := func(offset int) string {
		var best string
		for _, en := range ents {
			if en.offset <= offset {
				best = en.name
			}
		}
		return best
	}

	// JPA associations -> SCOPE.Component "relation" entities carrying the
	// owning entity + target type, so relationship_attribution is recoverable.
	for _, m := range elAssociationRE.FindAllStringSubmatchIndex(src, -1) {
		ann := src[m[2]:m[3]]
		var target string
		if m[4] >= 0 {
			target = src[m[4]:m[5]]
		} else if m[6] >= 0 {
			target = src[m[6]:m[7]]
		}
		field := src[m[8]:m[9]]
		if target == "" {
			continue
		}
		owner := owningEntity(m[0])
		ent := makeEntity(ann+":"+field, "SCOPE.Component", "relation", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eclipselink", "relation_type", ann,
			"field_name", field, "target_entity", target, "owner_entity", owner,
			"provenance", "INFERRED_FROM_ECLIPSELINK_ASSOCIATION")
		add(ent)
	}

	// @NamedQuery JPQL queries.
	qnames := elNamedQueryRE.SubexpNames()
	for _, m := range elNamedQueryRE.FindAllStringSubmatchIndex(src, -1) {
		var qn, jpql string
		for i, n := range qnames {
			if m[2*i] < 0 {
				continue
			}
			switch n {
			case "name", "name2":
				qn = src[m[2*i]:m[2*i+1]]
			case "query", "query2":
				jpql = src[m[2*i]:m[2*i+1]]
			}
		}
		owner := owningEntity(m[0])
		if owner == "" {
			owner = "Unknown"
		}
		// @NamedQuery names conventionally already carry the entity prefix
		// (e.g. "Customer.findAll"); use the declared name verbatim and keep
		// the owning entity as a property rather than double-prefixing it.
		ent := makeEntity(qn, "SCOPE.Operation", "query", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eclipselink", "query_name", qn, "jpql", jpql,
			"owner_entity", owner, "provenance", "INFERRED_FROM_ECLIPSELINK_NAMED_QUERY")
		add(ent)
	}

	// FK + lazy-loading (foreign_key_extraction / lazy_loading_recognition)
	fkResult := ExtractJPAFKAndLazy(src, owningEntity)
	emitJPAFKLazyRegistry(fkResult, fp, file.Language, "eclipselink", add)

	return entities, nil
}
