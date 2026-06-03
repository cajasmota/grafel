package substrate

import "testing"

// substrate_jsts_graphql_codefirst_test.go — issue #3903: prove the
// framework-blind, per-LANGUAGE JS/TS substrate sniffers fire on the
// code-first GraphQL TypeScript libraries Pothos and TypeGraphQL.
//
// Every JS/TS substrate sniffer is registered on the "jsts" language slug and
// dispatched solely by file extension via LanguageForPath (see
// internal/substrate/substrate.go LanguageForPath→"jsts",
// internal/links/dataflow_pass.go:97, internal/links/def_use_pass.go,
// internal/links/effect_propagation.go, internal/links/taint_flow.go). None of
// def_use_jsts.go / effect_sinks_jsts.go / taint_sites_jsts.go contains a
// single framework reference. Pothos and TypeGraphQL code lives in ordinary
// TypeScript (.ts) files and therefore already receives these passes.
//
// VERIFY-FIRST findings encoded as assertions below:
//
//  1. The substrate primitive DETECTORS (db_read/db_write/http_out effect
//     sinks, SQL-injection taint sink, def/use chains) fire on Pothos /
//     TypeGraphQL TypeScript bodies — proven here.
//  2. Handler ATTRIBUTION (the EffectMatch/TaintMatch/VarDef .Function field
//     the propagation + taint-flow link passes bind to a graph entity) only
//     succeeds for the standard function forms the per-language header scanner
//     recognises: named functions, `const x = (…) =>` arrows, and PLAIN class
//     methods. It does NOT succeed for the inline-arrow resolver nested in a
//     Pothos `t.field({ resolve: … })` call, nor for a TypeGraphQL method whose
//     parameter list carries `@Arg(…)` decorators (the `(` inside the decorator
//     defeats the method-shorthand header regex). Those forms are documented by
//     the negative probes — and request_sink_dataflow is deliberately NOT
//     credited for either framework because resolvers read `args`, not
//     `req.body`.
//
// These probes justify flipping the per-language Substrate effect / taint /
// def_use cells on the pothos + type-graphql records to `partial` (honest:
// detectors fire + attribute on the standard helper/service/handler forms these
// codebases also contain), while leaving the attribution-fragile and
// request-accessor-dependent cells `missing`.

// pothosSrc is a representative Pothos schema-builder module: a plain helper
// function (which attributes cleanly) performing a DB read+write, an outbound
// HTTP call, and a raw-SQL concat sink, wired into a builder mutation field.
const pothosSrc = `
import { builder } from './builder';

async function persistUser(name) {
  const trimmed = name;
  const existing = await User.findOne({ where: { name: trimmed } });
  const created = await User.create({ name: trimmed });
  await fetch('https://audit.example.com/log');
  const rows = await db.query('SELECT * FROM users WHERE name = ' + trimmed);
  return created;
}

builder.mutationField('createUser', (t) =>
  t.field({
    type: 'User',
    args: { name: t.arg.string({ required: true }) },
    resolve: (root, args) => persistUser(args.name),
  }),
);
`

// typeGraphQLSrc is a representative TypeGraphQL module: a plain service
// function carrying the substrate-bearing primitives, called from an
// @Resolver method.
const typeGraphQLSrc = `
import { Resolver, Mutation, Arg } from 'type-graphql';

async function persistAccount(email) {
  const normalized = email;
  const existing = await Account.findUnique({ where: { email: normalized } });
  const created = await Account.create({ data: { email: normalized } });
  await axios.post('https://audit.example.com/log', { email });
  const rows = await db.query('SELECT * FROM accounts WHERE email = ' + email);
  return created;
}

@Resolver()
export class UserResolver {
  @Mutation(() => User)
  async createUser(@Arg('email') email: string) {
    return persistAccount(email);
  }
}
`

// hasEffect reports whether any EffectMatch carries the given effect.
func hasEffect(ms []EffectMatch, e Effect) bool {
	for _, m := range ms {
		if m.Effect == e {
			return true
		}
	}
	return false
}

// hasEffectIn reports whether any EffectMatch carries effect e AND is attributed
// to function fn (so the propagation link pass can bind it to an entity).
func hasEffectIn(ms []EffectMatch, e Effect, fn string) bool {
	for _, m := range ms {
		if m.Effect == e && m.Function == fn {
			return true
		}
	}
	return false
}

// hasDef / hasUse report whether the def/use set contains the named identifier.
func hasDefIn(defs []VarDef, name, fn string) bool {
	for _, d := range defs {
		if d.Var == name && d.Function == fn {
			return true
		}
	}
	return false
}
func hasUseIn(uses []VarUse, name, fn string) bool {
	for _, u := range uses {
		if u.Var == name && u.Function == fn {
			return true
		}
	}
	return false
}

// --- Pothos -----------------------------------------------------------------

func TestSubstrate_JSTS_Pothos_DefUseAttributes(t *testing.T) {
	defs, uses := sniffDefUseJSTS(pothosSrc)
	if !hasDefIn(defs, "trimmed", "persistUser") {
		t.Errorf("def_use: expected def of `trimmed` in persistUser, defs=%+v", defs)
	}
	if !hasUseIn(uses, "trimmed", "persistUser") {
		t.Errorf("def_use: expected use of `trimmed` in persistUser, uses=%+v", uses)
	}
}

func TestSubstrate_JSTS_Pothos_EffectsAttribute(t *testing.T) {
	ms := sniffEffectsJSTS(pothosSrc)
	for _, want := range []Effect{EffectDBRead, EffectDBWrite, EffectHTTPOut} {
		if !hasEffectIn(ms, want, "persistUser") {
			t.Errorf("effects: expected %s attributed to persistUser, got %+v", want, ms)
		}
	}
}

func TestSubstrate_JSTS_Pothos_TaintFires(t *testing.T) {
	ms := sniffTaintJSTS(pothosSrc)
	if countTaint(ms, TaintKindSink, TaintCategorySQL) == 0 {
		t.Errorf("taint: expected a SQL-injection sink (raw query concat), got %+v", ms)
	}
}

// pothosSanitizerSrc is a Pothos module whose helper cleanses input before use:
// a DOMPurify.sanitize XSS sanitizer and a parameterised db.query(sql, [params])
// SQL sanitizer, both inside the `persistUser` helper wired into a builder
// mutation field.
const pothosSanitizerSrc = `
import { builder } from './builder';
import DOMPurify from 'dompurify';

async function persistUser(name) {
  const clean = DOMPurify.sanitize(name);
  const rows = await db.query('SELECT * FROM users WHERE name = ?', [clean]);
  return clean;
}

builder.mutationField('createUser', (t) =>
  t.field({
    type: 'User',
    args: { name: t.arg.string({ required: true }) },
    resolve: (root, args) => persistUser(args.name),
  }),
);
`

// TestSubstrate_JSTS_Pothos_SanitizerFires proves sanitizer_recognition for
// Pothos: the framework-blind jsts sanitizer detectors fire — DOMPurify.sanitize
// as an XSS sanitizer and the parameterised db.query(sql, [params]) as a SQL
// sanitizer — and BOTH attribute to the persistUser helper. Mirrors the #3903
// taint-sink credit (security-relevant primitives detected per-LANGUAGE
// regardless of framework).
func TestSubstrate_JSTS_Pothos_SanitizerFires(t *testing.T) {
	ms := sniffTaintJSTS(pothosSanitizerSrc)
	if !hasTaintJstsInFn(ms, TaintKindSanitizer, TaintCategoryXSS, "persistUser") {
		t.Errorf("sanitizer: expected an XSS sanitizer (DOMPurify.sanitize) attributed to persistUser, got %+v", ms)
	}
	if !hasTaintJstsInFn(ms, TaintKindSanitizer, TaintCategorySQL, "persistUser") {
		t.Errorf("sanitizer: expected a SQL sanitizer (parameterised db.query) attributed to persistUser, got %+v", ms)
	}
}

// --- TypeGraphQL ------------------------------------------------------------

func TestSubstrate_JSTS_TypeGraphQL_DefUseAttributes(t *testing.T) {
	defs, uses := sniffDefUseJSTS(typeGraphQLSrc)
	if !hasDefIn(defs, "normalized", "persistAccount") {
		t.Errorf("def_use: expected def of `normalized` in persistAccount, defs=%+v", defs)
	}
	if !hasUseIn(uses, "normalized", "persistAccount") {
		t.Errorf("def_use: expected use of `normalized` in persistAccount, uses=%+v", uses)
	}
}

func TestSubstrate_JSTS_TypeGraphQL_EffectsAttribute(t *testing.T) {
	ms := sniffEffectsJSTS(typeGraphQLSrc)
	for _, want := range []Effect{EffectDBRead, EffectDBWrite, EffectHTTPOut} {
		if !hasEffectIn(ms, want, "persistAccount") {
			t.Errorf("effects: expected %s attributed to persistAccount, got %+v", want, ms)
		}
	}
}

func TestSubstrate_JSTS_TypeGraphQL_TaintFires(t *testing.T) {
	ms := sniffTaintJSTS(typeGraphQLSrc)
	if countTaint(ms, TaintKindSink, TaintCategorySQL) == 0 {
		t.Errorf("taint: expected a SQL-injection sink (raw query concat), got %+v", ms)
	}
}

// typeGraphQLSanitizerSrc is a TypeGraphQL module whose service helper cleanses
// input before use: a validator.escape XSS sanitizer and a parameterised
// db.query(sql, [params]) SQL sanitizer, both inside the persistAccount helper
// called from an @Resolver method.
const typeGraphQLSanitizerSrc = `
import { Resolver, Mutation, Arg } from 'type-graphql';
import validator from 'validator';

async function persistAccount(email) {
  const safe = validator.escape(email);
  const rows = await db.query('SELECT * FROM accounts WHERE email = ?', [safe]);
  return safe;
}

@Resolver()
export class UserResolver {
  @Mutation(() => User)
  async createUser(@Arg('email') email: string) {
    return persistAccount(email);
  }
}
`

// TestSubstrate_JSTS_TypeGraphQL_SanitizerFires proves sanitizer_recognition for
// TypeGraphQL: validator.escape (XSS) and the parameterised db.query(sql,
// [params]) (SQL) sanitizers fire and attribute to persistAccount.
func TestSubstrate_JSTS_TypeGraphQL_SanitizerFires(t *testing.T) {
	ms := sniffTaintJSTS(typeGraphQLSanitizerSrc)
	if !hasTaintJstsInFn(ms, TaintKindSanitizer, TaintCategoryXSS, "persistAccount") {
		t.Errorf("sanitizer: expected an XSS sanitizer (validator.escape) attributed to persistAccount, got %+v", ms)
	}
	if !hasTaintJstsInFn(ms, TaintKindSanitizer, TaintCategorySQL, "persistAccount") {
		t.Errorf("sanitizer: expected a SQL sanitizer (parameterised db.query) attributed to persistAccount, got %+v", ms)
	}
}

// hasTaintJstsInFn reports whether a TaintMatch of kind+category attributed to
// fn is present.
func hasTaintJstsInFn(ms []TaintMatch, kind TaintKind, cat TaintCategory, fn string) bool {
	for _, m := range ms {
		if m.Kind == kind && m.Category == cat && m.Function == fn {
			return true
		}
	}
	return false
}

// --- Negative: request_sink_dataflow does NOT fire (honest non-credit) -------

// TestSubstrate_JSTS_GraphQLCodefirst_RequestSinkDataflowDoesNotFire documents
// WHY request_sink_dataflow is left `missing` for Pothos / TypeGraphQL: the
// dataflow_jsts.go matcher keys on req.body / req.query / req.params /
// ctx.request.body accessors, but code-first GraphQL resolvers receive
// untrusted input via resolver `args`, not those request accessors. The
// matcher therefore produces no flow — so we honestly do NOT credit that cell.
func TestSubstrate_JSTS_GraphQLCodefirst_RequestSinkDataflowDoesNotFire(t *testing.T) {
	for name, src := range map[string]string{
		"pothos":       pothosSrc,
		"type-graphql": typeGraphQLSrc,
	} {
		if flows := sniffDataFlowJSTS(src); len(flows) != 0 {
			t.Errorf("[%s] expected NO request_sink_dataflow flows (args, not req.body); got %+v", name, flows)
		}
	}
}
