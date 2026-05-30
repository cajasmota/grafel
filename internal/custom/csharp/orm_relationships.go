// Package csharp — ORM relationship extractor for LINQ-to-SQL, LinqToDB,
// and NHibernate/FluentNHibernate C# source files.
//
// This extractor focuses on the Relationships capability group:
//
//	association_extraction:
//	  LINQ-to-SQL / LinqToDB: [Association(ThisKey="...", OtherKey="...")]
//	  NHibernate: References(x => x.Nav) / HasMany(x => x.Col)
//	    fluent API calls inside ClassMap<T>
//
//	foreign_key_extraction:
//	  LINQ-to-SQL / LinqToDB: ThisKey/OtherKey extracted from [Association(...)];
//	    also explicit [ForeignKey("col")] attribute on navigation properties.
//	  NHibernate: .Column("fk_col") chained after References()/HasMany()
//
//	lazy_loading_recognition:
//	  NHibernate: .LazyLoad() / .Not.LazyLoad() chained after References/HasMany.
//	  LINQ-to-SQL: DataLoadOptions / LoadWith<T>() deferred-load calls.
//	  (LinqToDB does not support lazy loading — not_applicable.)
//
// NHibernate XML mapping is intentionally out of scope here; only fluent
// FluentNHibernate API is targeted.
//
// Emitted entity kind: SCOPE.Pattern with the appropriate subtype.
package csharp

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_orm_relationships", &ormRelationshipsExtractor{})
}

type ormRelationshipsExtractor struct{}

func (e *ormRelationshipsExtractor) Language() string { return "custom_csharp_orm_relationships" }

// ---------------------------------------------------------------------------
// Regexes — LINQ-to-SQL / LinqToDB associations
// ---------------------------------------------------------------------------

var (
	// [Association(ThisKey="id", OtherKey="customerId")] on a navigation property.
	reAssocAttr = regexp.MustCompile(
		`\[Association\s*\(([^)]*)\)\s*\]`,
	)
	// Extract ThisKey="..." from an Association attribute argument list.
	reAssocThisKey = regexp.MustCompile(
		`ThisKey\s*=\s*["']([^"']+)["']`,
	)
	// Extract OtherKey="..." from an Association attribute argument list.
	reAssocOtherKey = regexp.MustCompile(
		`OtherKey\s*=\s*["']([^"']+)["']`,
	)
	// [ForeignKey("col_name")] explicit attribute.
	reForeignKeyAttr = regexp.MustCompile(
		`\[ForeignKey\s*\(\s*["']([^"']+)["']\s*\)\s*\]`,
	)
	// DataLoadOptions.LoadWith<T>() — LINQ-to-SQL deferred/eager loading.
	reLoadWith = regexp.MustCompile(
		`\.LoadWith\s*<\s*(\w+)\s*>`,
	)
	// LinqToDB namespace marker.
	reLinqToDBNSRel = regexp.MustCompile(`using\s+LinqToDB\b`)
	// LINQ-to-SQL namespace marker.
	reLinqToSQLNSRel = regexp.MustCompile(`using\s+System\.Data\.Linq\b`)
)

// ---------------------------------------------------------------------------
// Regexes — NHibernate / FluentNHibernate relationships
// ---------------------------------------------------------------------------

var (
	// References(x => x.Navigation) — many-to-one in FluentNHibernate.
	// Captures navigation property name from the lambda.
	reNHRef = regexp.MustCompile(
		`(?:^|\.|\s)References\s*\(\s*x\s*=>\s*x\.(\w+)`,
	)
	// HasMany(x => x.Collection) — one-to-many in FluentNHibernate.
	reNHHM = regexp.MustCompile(
		`(?:^|\.|\s)HasMany\s*\(\s*x\s*=>\s*x\.(\w+)`,
	)
	// HasOne(x => x.Reference) — one-to-one in FluentNHibernate.
	reNHHasOne = regexp.MustCompile(
		`(?:^|\.|\s)HasOne\s*\(\s*x\s*=>\s*x\.(\w+)`,
	)
	// .Column("fk_col") chained after References/HasMany/HasOne.
	reNHColumn = regexp.MustCompile(
		`\.Column\s*\(\s*["']([^"']+)["']\s*\)`,
	)
	// .LazyLoad() chained method (enables lazy loading for the association).
	reNHLazyLoad = regexp.MustCompile(
		`\.LazyLoad\s*\(`,
	)
	// .Not.LazyLoad() disabling lazy loading — still marks lazy-load awareness.
	reNHNotLazyLoad = regexp.MustCompile(
		`\.Not\s*\.\s*LazyLoad\s*\(`,
	)
	// ClassMap<T> detection (presence qualifies file as FluentNHibernate mapping).
	reNHClassMapRel = regexp.MustCompile(
		`(?m)class\s+\w+\s*:\s*ClassMap\s*<\s*(\w+)\s*>`,
	)
	// ISession marker — HBM XML or SessionFactory usage.
	reNHSessionRel = regexp.MustCompile(
		`\bISession\b|\bISessionFactory\b`,
	)
	// NHibernate / FluentNHibernate namespace marker.
	reNHibernateNSRel = regexp.MustCompile(
		`using\s+(?:NHibernate|FluentNHibernate)\b`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *ormRelationshipsExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_orm_relationships_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
		return nil, nil
	}

	src := string(file.Content)
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

	isLinqToSQL := reLinqToSQLNSRel.MatchString(src)
	isLinqToDB := reLinqToDBNSRel.MatchString(src)
	isLinq := isLinqToSQL || isLinqToDB
	isNH := reNHibernateNSRel.MatchString(src) || reNHClassMapRel.MatchString(src) || reNHSessionRel.MatchString(src)

	fwName := "linqtodb"
	if isLinqToSQL {
		fwName = "linq-to-sql"
	}

	// -------------------------------------------------------------------------
	// LINQ-to-SQL / LinqToDB
	// -------------------------------------------------------------------------

	if isLinq || reAssocAttr.MatchString(src) {
		// [Association(...)] → association_extraction + foreign_key_extraction
		for _, m := range reAssocAttr.FindAllStringSubmatchIndex(src, -1) {
			args := src[m[2]:m[3]]
			line := lineOf(src, m[0])

			// association_extraction
			assocName := fwName + ":assoc:" + file.Path + ":" + itoa(line)
			entAssoc := makeEntity(assocName, "SCOPE.Pattern", "association_extraction", file.Path, "csharp", line)
			setProps(&entAssoc, "framework", fwName, "provenance", "INFERRED_FROM_LINQ_ASSOCIATION")
			add(entAssoc)

			// foreign_key_extraction — extract ThisKey/OtherKey as FK identifiers
			thisKey := ""
			if mk := reAssocThisKey.FindStringSubmatch(args); len(mk) >= 2 {
				thisKey = mk[1]
			}
			otherKey := ""
			if mk := reAssocOtherKey.FindStringSubmatch(args); len(mk) >= 2 {
				otherKey = mk[1]
			}
			if thisKey != "" || otherKey != "" {
				fkName := fwName + ":fk:" + thisKey + ":" + otherKey + ":" + file.Path + ":" + itoa(line)
				entFK := makeEntity(fkName, "SCOPE.Pattern", "foreign_key_extraction", file.Path, "csharp", line)
				setProps(&entFK, "framework", fwName,
					"provenance", "INFERRED_FROM_LINQ_ASSOCIATION_KEYS",
					"this_key", thisKey, "other_key", otherKey)
				add(entFK)
			}
		}
	}

	// Explicit [ForeignKey("col")] attribute — applies regardless of ORM framework
	// (used in LINQ-to-SQL, LinqToDB, and plain EF annotations).
	if reForeignKeyAttr.MatchString(src) {
		for _, m := range reForeignKeyAttr.FindAllStringSubmatchIndex(src, -1) {
			col := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			fw := fwName
			if !isLinq {
				fw = "csharp"
			}
			name := fw + ":fk_attr:" + col + ":" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "foreign_key_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", fw, "provenance", "INFERRED_FROM_FOREIGN_KEY_ATTR",
				"column", col)
			add(ent)
		}
	}

	if isLinq {

		// LoadWith<T>() — lazy/deferred loading for LINQ-to-SQL
		// (LinqToDB does not support lazy loading so we only emit for linq-to-sql)
		if isLinqToSQL {
			for _, m := range reLoadWith.FindAllStringSubmatchIndex(src, -1) {
				entityType := src[m[2]:m[3]]
				line := lineOf(src, m[0])
				name := "linq-to-sql:lazy:" + entityType + ":" + file.Path + ":" + itoa(line)
				ent := makeEntity(name, "SCOPE.Pattern", "lazy_loading_recognition", file.Path, "csharp", line)
				setProps(&ent, "framework", "linq-to-sql",
					"provenance", "INFERRED_FROM_LINQ_LOAD_WITH",
					"entity_type", entityType)
				add(ent)
			}
		}
	}

	// -------------------------------------------------------------------------
	// NHibernate / FluentNHibernate
	// -------------------------------------------------------------------------

	if isNH {
		// References(x => x.Nav) → association_extraction + foreign_key_extraction
		for _, m := range reNHRef.FindAllStringSubmatchIndex(src, -1) {
			nav := src[m[2]:m[3]]
			line := lineOf(src, m[0])

			// association_extraction
			name := "nhibernate:assoc:ref:" + nav
			ent := makeEntity(name, "SCOPE.Pattern", "association_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_REFERENCES",
				"navigation_property", nav, "cardinality", "many-to-one")
			add(ent)
		}

		// HasMany(x => x.Col) → association_extraction
		for _, m := range reNHHM.FindAllStringSubmatchIndex(src, -1) {
			nav := src[m[2]:m[3]]
			line := lineOf(src, m[0])

			name := "nhibernate:assoc:hasmany:" + nav
			ent := makeEntity(name, "SCOPE.Pattern", "association_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_HAS_MANY",
				"navigation_property", nav, "cardinality", "one-to-many")
			add(ent)
		}

		// HasOne(x => x.Ref) → association_extraction
		for _, m := range reNHHasOne.FindAllStringSubmatchIndex(src, -1) {
			nav := src[m[2]:m[3]]
			line := lineOf(src, m[0])

			name := "nhibernate:assoc:hasone:" + nav
			ent := makeEntity(name, "SCOPE.Pattern", "association_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_HAS_ONE",
				"navigation_property", nav, "cardinality", "one-to-one")
			add(ent)
		}

		// .Column("fk_col") → foreign_key_extraction
		for _, m := range reNHColumn.FindAllStringSubmatchIndex(src, -1) {
			col := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			name := "nhibernate:fk:" + col + ":" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "foreign_key_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_COLUMN",
				"column", col)
			add(ent)
		}

		// .LazyLoad() / .Not.LazyLoad() → lazy_loading_recognition
		for _, m := range reNHLazyLoad.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := "nhibernate:lazy:" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "lazy_loading_recognition", file.Path, "csharp", line)
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_LAZY_LOAD",
				"enabled", "true")
			add(ent)
		}
		for _, m := range reNHNotLazyLoad.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := "nhibernate:not_lazy:" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "lazy_loading_recognition", file.Path, "csharp", line)
			setProps(&ent, "framework", "nhibernate", "provenance", "INFERRED_FROM_NH_NOT_LAZY_LOAD",
				"enabled", "false")
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
