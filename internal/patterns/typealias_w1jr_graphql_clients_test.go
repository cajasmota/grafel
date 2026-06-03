package patterns

// Wave1-structural (epic #3872): prove the language-level TS type_alias
// extractor fires on the real idioms of the GraphQL-flavoured jsts records
// — pothos, type-graphql, graphql-client. The extractor switches on
// language "typescript"/"javascript" (unconditional per-language, no
// framework gate; see typeAliasExtractor.Detect), so a flip of
// type_alias_extraction missing -> full on these records is justified iff
// the framework's idiomatic .ts source yields type_alias entities with
// the EXACT alias_name / alias_of properties. Assertions check exact prop
// values — never len>0 alone.

import "testing"

// findAliasW1jr returns the type_alias entity with the given alias_name,
// or nil. Used to assert exact alias_name/alias_of pairs.
func findAliasW1jr(results []entityRecordAlias, name string) map[string]string {
	for _, p := range results {
		if p["alias_name"] == name {
			return p
		}
	}
	return nil
}

type entityRecordAlias = map[string]string

func propsOfW1jr(t *testing.T, src string) []entityRecordAlias {
	t.Helper()
	d := &typeAliasExtractor{}
	out := []entityRecordAlias{}
	for _, e := range d.Detect("schema.ts", "typescript", src) {
		out = append(out, e.Properties)
	}
	return out
}

// Pothos code-first schemas alias the SchemaBuilder's generic type map —
// `type Types = { Scalars: { ... } }` and field-shape aliases are routine.
func TestW1jr_TypeAlias_PothosSchemaBuilderTypes(t *testing.T) {
	src := `import SchemaBuilder from '@pothos/core';
type UserId = string;
type Types = { Scalars: { ID: { Input: UserId; Output: UserId } } };
const builder = new SchemaBuilder<Types>({});`
	props := propsOfW1jr(t, src)
	a := findAliasW1jr(props, "UserId")
	if a == nil {
		t.Fatalf("expected UserId type_alias, got %+v", props)
	}
	if a["alias_of"] != "string" {
		t.Fatalf("expected UserId alias_of=string, got %q", a["alias_of"])
	}
	if findAliasW1jr(props, "Types") == nil {
		t.Fatalf("expected Types type_alias (Pothos builder generic), got %+v", props)
	}
}

// type-graphql codebases alias resolver context / arg-shape unions.
func TestW1jr_TypeAlias_TypeGraphqlContextAlias(t *testing.T) {
	src := `import { Resolver } from 'type-graphql';
type MyContext = BaseContext & RequestContext;
@Resolver()
class RecipeResolver {}`
	props := propsOfW1jr(t, src)
	a := findAliasW1jr(props, "MyContext")
	if a == nil {
		t.Fatalf("expected MyContext type_alias, got %+v", props)
	}
	// Intersection-type context alias — the extractor captures the full
	// RHS up to the statement terminator.
	if a["alias_of"] != "BaseContext & RequestContext" {
		t.Fatalf("expected MyContext alias_of intersection, got %q", a["alias_of"])
	}
}

// graphql-client operations alias variable/result shapes for typed calls.
func TestW1jr_TypeAlias_GraphqlClientOperationShape(t *testing.T) {
	src := `import { request } from 'graphql-request';
type UserVars = { id: string };
type UserResult = { user: { name: string } };`
	props := propsOfW1jr(t, src)
	v := findAliasW1jr(props, "UserVars")
	if v == nil {
		t.Fatalf("expected UserVars type_alias, got %+v", props)
	}
	if v["alias_of"] != "{ id: string }" {
		t.Fatalf("expected UserVars alias_of, got %q", v["alias_of"])
	}
	if findAliasW1jr(props, "UserResult") == nil {
		t.Fatalf("expected UserResult type_alias, got %+v", props)
	}
}
