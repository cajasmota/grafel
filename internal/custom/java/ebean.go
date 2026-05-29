package java

import (
	"context"
	"regexp"
	"strings"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// Ebean ORM. Ebean uses JPA-style mapping annotations (@Entity, @Table,
// @OneToMany, @ManyToOne, ...) but adds two Ebean-specific access patterns:
//
//   - the `io.ebean.Model` base class, where an @Entity subclasses Model and
//     gains instance save()/delete() and a static find handle;
//   - the `io.ebean.Finder<ID, T>` static query handle declared on the entity,
//     plus `DB.find(Foo.class)` / `Ebean.find(Foo.class)` fluent queries.
//
// This extractor parses model classes, JPA associations, Finder declarations,
// and DB/Ebean query roots, emitting registry-shaped EntityRecords through the
// custom_java_ dispatch path. It self-gates on an Ebean import/marker so it
// does not fire on generic JPA sources owned by other coverage records.

func init() {
	extreg.Register("custom_java_ebean", &ebeanExtractor{})
}

type ebeanExtractor struct{}

func (e *ebeanExtractor) Language() string { return "custom_java_ebean" }

var (
	ebeanMarkerRE = regexp.MustCompile(
		`io\.ebean|\bextends\s+(?:io\.ebean\.)?Model\b|\bFinder\s*<|\bDB\.find\s*\(|\bEbean\.find\s*\(`)
	ebeanEntityClassRE = regexp.MustCompile(
		`(?s)@Entity\b(?:[^{]|\{[^}]*\})*?class\s+(\w+)`)
	ebeanTableNameRE = regexp.MustCompile(
		`(?s)@Table\s*\([^)]*name\s*=\s*"([^"]+)"`)
	ebeanModelRE = regexp.MustCompile(
		`class\s+(\w+)\s+extends\s+(?:io\.ebean\.)?Model\b`)
	ebeanAssociationRE = regexp.MustCompile(
		`(?s)@(OneToMany|ManyToOne|OneToOne|ManyToMany)\b(?:\s*\([^)]*\))?` +
			`\s*(?:@\w+(?:\s*\([^)]*\))?\s*)*(?:private|protected|public|)\s+` +
			`(?:(?:final|transient)\s+)*(?:\w+<(\w+)>|(\w+))\s+(\w+)\s*;`)
	// Finder<Long, Customer> find = new Finder<>(Customer.class);
	ebeanFinderRE = regexp.MustCompile(
		`\bFinder\s*<\s*\w+\s*,\s*(\w+)\s*>\s+(\w+)`)
	// DB.find(Customer.class) / Ebean.find(Customer.class)
	ebeanQueryRootRE = regexp.MustCompile(
		`\b(?:DB|Ebean)\.find\s*\(\s*(\w+)\.class`)
)

func (e *ebeanExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	if strings.ToLower(file.Language) != "java" {
		return nil, nil
	}
	src := string(file.Content)
	if !ebeanMarkerRE.MatchString(src) {
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

	// Model base-class subclasses (active-record style).
	modelClasses := make(map[string]bool)
	for _, m := range ebeanModelRE.FindAllStringSubmatchIndex(src, -1) {
		modelClasses[src[m[2]:m[3]]] = true
	}

	// @Entity model classes (also covers @Entity-less Model subclasses below).
	type entInfo struct {
		name   string
		offset int
	}
	var ents []entInfo
	seenEntity := make(map[string]bool)
	emitEntity := func(name string, offset int) {
		if seenEntity[name] {
			return
		}
		seenEntity[name] = true
		ent := makeEntity(name, "SCOPE.Schema", "entity", fp, file.Language, lineOf(src, offset))
		snippet := src[max(0, offset-600):min(len(src), offset+400)]
		if tm := ebeanTableNameRE.FindStringSubmatch(snippet); tm != nil {
			setProps(&ent, "table_name", tm[1])
		}
		if modelClasses[name] {
			setProps(&ent, "model_base", "true")
		}
		setProps(&ent, "framework", "ebean",
			"provenance", "INFERRED_FROM_EBEAN_ENTITY")
		add(ent)
		ents = append(ents, entInfo{name, offset})
	}

	for _, m := range ebeanEntityClassRE.FindAllStringSubmatchIndex(src, -1) {
		emitEntity(src[m[2]:m[3]], m[0])
	}
	// Model subclasses without (or before) an @Entity match still count.
	for _, m := range ebeanModelRE.FindAllStringSubmatchIndex(src, -1) {
		emitEntity(src[m[2]:m[3]], m[0])
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

	// JPA associations -> SCOPE.Component relation entities.
	for _, m := range ebeanAssociationRE.FindAllStringSubmatchIndex(src, -1) {
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
		setProps(&ent, "framework", "ebean", "relation_type", ann,
			"field_name", field, "target_entity", target, "owner_entity", owner,
			"provenance", "INFERRED_FROM_EBEAN_ASSOCIATION")
		add(ent)
	}

	// Finder<ID, T> static query handles.
	for _, m := range ebeanFinderRE.FindAllStringSubmatchIndex(src, -1) {
		target := src[m[2]:m[3]]
		fieldName := src[m[4]:m[5]]
		ent := makeEntity("finder:"+target, "SCOPE.Component", "finder", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ebean", "target_entity", target,
			"field_name", fieldName, "provenance", "INFERRED_FROM_EBEAN_FINDER")
		add(ent)
	}

	// DB.find(Foo.class) / Ebean.find(Foo.class) query roots.
	for _, m := range ebeanQueryRootRE.FindAllStringSubmatchIndex(src, -1) {
		target := src[m[2]:m[3]]
		ent := makeEntity("query:"+target, "SCOPE.Operation", "query", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ebean", "target_entity", target,
			"provenance", "INFERRED_FROM_EBEAN_QUERY")
		add(ent)
	}

	// FK + lazy-loading (foreign_key_extraction / lazy_loading_recognition)
	fkResult := ExtractJPAFKAndLazy(src, owningEntity)
	emitJPAFKLazyRegistry(fkResult, fp, file.Language, "ebean", add)

	return entities, nil
}
