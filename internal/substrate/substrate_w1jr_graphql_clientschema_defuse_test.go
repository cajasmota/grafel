package substrate

import "testing"

// Wave1-structural (epic #3872): prove the language-level jsts def-use
// sniffer fires on the real idioms of the GraphQL-flavoured jsts records
// — pothos (code-first builder), type-graphql (decorator classes), and
// graphql-client (operation builders). The sniffer registers on the
// "jsts" slug (see init() in def_use_jsts.go) and is framework-agnostic,
// so a flip of def_use_chain_extraction missing/partial -> partial on
// these records is justified iff the framework's idiomatic source yields
// concrete (function, var) def/use pairs. Each assertion below names the
// EXACT enclosing function and variable — never len>0 alone.

// Pothos code-first: the canonical factoring extracts a field's resolver
// into a top-level arrow `const resolveUser = (parent, args, ctx) => {...}`
// then wires it into `t.field({ resolve: resolveUser })`. We assert the
// precise def/use of `userId` and `safe` inside `resolveUser`.
func TestW1jr_DefUseJSTS_PothosResolveBody(t *testing.T) {
	src := `
import { builder } from './builder';

const resolveUser = (parent, args, ctx) => {
  let userId = args.id;
  let safe = userId + 1;
  return ctx.loadUser(safe);
};

builder.queryField('user', (t) =>
  t.field({ type: 'User', resolve: resolveUser }),
);
`
	defs, uses := sniffDefUseJSTS(src)
	if !containsVarDef(defs, "resolveUser", "userId") {
		t.Fatalf("expected def of userId in resolveUser, got %+v", defs)
	}
	if !containsVarDef(defs, "resolveUser", "safe") {
		t.Fatalf("expected def of safe in resolveUser, got %+v", defs)
	}
	// `userId` is defined then read into `safe` — a real def->use chain.
	if !containsVarUse(uses, "resolveUser", "userId") {
		t.Fatalf("expected use of userId in resolveUser, got %+v", uses)
	}
}

// type-graphql decorator class: a @Resolver method binds locals. Assert
// def/use of `total` inside the resolver method `activeRecipes`.
func TestW1jr_DefUseJSTS_TypeGraphqlResolverMethod(t *testing.T) {
	src := `
@Resolver(() => Recipe)
class RecipeResolver {
  @Query(() => [Recipe])
  async activeRecipes(parent, args, ctx) {
    let total = args.limit;
    let bounded = total + 5;
    return ctx.recipes.slice(0, bounded);
  }
}
`
	defs, uses := sniffDefUseJSTS(src)
	if !containsVarDef(defs, "activeRecipes", "total") {
		t.Fatalf("expected def of total in activeRecipes, got %+v", defs)
	}
	if !containsVarDef(defs, "activeRecipes", "bounded") {
		t.Fatalf("expected def of bounded in activeRecipes, got %+v", defs)
	}
	if !containsVarUse(uses, "activeRecipes", "total") {
		t.Fatalf("expected use of total in activeRecipes, got %+v", uses)
	}
}

// graphql-client operation builder: a request function binds the
// composed query variables. Assert def/use of `variables` in `fetchUser`.
func TestW1jr_DefUseJSTS_GraphqlClientOperation(t *testing.T) {
	src := `
import { gql, request } from 'graphql-request';

async function fetchUser(endpoint, id) {
  let variables = { userId: id };
  let merged = variables;
  return request(endpoint, USER_QUERY, merged);
}
`
	defs, uses := sniffDefUseJSTS(src)
	if !containsVarDef(defs, "fetchUser", "variables") {
		t.Fatalf("expected def of variables in fetchUser, got %+v", defs)
	}
	if !containsVarDef(defs, "fetchUser", "merged") {
		t.Fatalf("expected def of merged in fetchUser, got %+v", defs)
	}
	if !containsVarUse(uses, "fetchUser", "variables") {
		t.Fatalf("expected use of variables in fetchUser, got %+v", uses)
	}
}
