package substrate

import "testing"

// substrate_structural_gojava_wave1_test.go drives the framework-AGNOSTIC,
// per-LANGUAGE def-use sniffers (sniffDefUseGo / sniffDefUseJava) on the
// real idiom of the wave-1 structural records that a credit sweep left as
// stale `missing` Substrate `def_use_chain_extraction` cells in
// docs/coverage/registry.json (epic #3872): fx + wire (Go DI), guice (Java
// DI), dgs + spring-graphql (Java GraphQL). gqlgen's def_use was already
// credited in #3918; this file does NOT re-prove it.
//
// Each sniffer is registered on the LANGUAGE slug
// (RegisterDefUseSniffer("go"|"java", …) in def_use_golang.go /
// def_use_java.go) and dispatches PURELY by file extension via
// LanguageForPath (.go→go, .java→java) — zero per-framework branching. So an
// fx provider func, a wire injector func, a guice AbstractModule.configure(),
// a dgs @DgsQuery datafetcher, and a spring-graphql @QueryMapping controller
// each dispatch the SAME sniffer as their flagship HTTP siblings (gin /
// spring-boot). These tests PROVE the def-use sniffer extracts the EXACT
// local-variable DEFINITION and USE and ATTRIBUTES both to the enclosing
// framework method/func — never len>0 — which is exactly what the generic
// def_use_pass.go composes into reaching-definition chains.
//
// PER-CAP HONESTY: only def_use_chain_extraction is a per-FILE sniffer
// provable here. The sibling structural caps flipped to `partial` on these
// records (dead_code/reachability/module_cycle/pure_function) are
// whole-GRAPH passes in internal/links with "zero per-language code"; they
// apply to any Go/Java entity and are credited by mirroring the gin /
// spring-boot siblings, not by a per-file sniffer test. type_alias_extraction
// stays `partial` for Go (the `type X = Y` extractor) and `not_applicable`
// for Java (no type-alias syntax) — see the registry notes.

// --- Go DI siblings: fx + wire ---------------------------------------------

// TestStructural_Go_Fx_DefUseAttributes drives sniffDefUseGo on a uber-go/fx
// provider constructor. fx wires dependencies through ordinary provider
// funcs (`func NewServer(cfg Config) *Server`); the def-use sniffer must
// capture the local `addr := cfg.Addr` definition AND its later use in the
// `&Server{Addr: addr}` literal, both attributed to NewServer.
func TestStructural_Go_Fx_DefUseAttributes(t *testing.T) {
	const src = `package app

import "go.uber.org/fx"

// NewServer is an fx provider constructor (fx.Provide(NewServer)).
func NewServer(cfg Config) *Server {
	addr := cfg.Addr
	srv := &Server{Addr: addr}
	return srv
}
`
	defs, uses := sniffDefUseGo(src)
	if !hasDefIn(defs, "addr", "NewServer") {
		t.Errorf("def_use: expected def of local `addr` in fx provider NewServer, defs=%+v", defs)
	}
	if !hasUseIn(uses, "addr", "NewServer") {
		t.Errorf("def_use: expected use of local `addr` in fx provider NewServer, uses=%+v", uses)
	}
	if !hasDefIn(defs, "srv", "NewServer") {
		t.Errorf("def_use: expected def of local `srv` in fx provider NewServer, defs=%+v", defs)
	}
}

// TestStructural_Go_Wire_DefUseAttributes drives sniffDefUseGo on a
// google/wire provider func. wire generates injectors that call provider
// funcs like `func provideDB(cfg Config) (*DB, error)`; the def-use sniffer
// must capture the `dsn := cfg.DSN` definition and the `open(dsn)` use, both
// attributed to provideDB.
func TestStructural_Go_Wire_DefUseAttributes(t *testing.T) {
	const src = `package di

import "github.com/google/wire"

// provideDB is a wire provider (wire.NewSet(provideDB)).
func provideDB(cfg Config) (*DB, error) {
	dsn := cfg.DSN
	db, err := open(dsn)
	return db, err
}
`
	defs, uses := sniffDefUseGo(src)
	if !hasDefIn(defs, "dsn", "provideDB") {
		t.Errorf("def_use: expected def of local `dsn` in wire provider provideDB, defs=%+v", defs)
	}
	if !hasUseIn(uses, "dsn", "provideDB") {
		t.Errorf("def_use: expected use of local `dsn` (in open(dsn)) in wire provider provideDB, uses=%+v", uses)
	}
	if !hasDefIn(defs, "db", "provideDB") {
		t.Errorf("def_use: expected def of local `db` in wire provider provideDB, defs=%+v", defs)
	}
}

// --- Java DI sibling: guice -------------------------------------------------

// TestStructural_Java_Guice_DefUseAttributes drives sniffDefUseJava on a
// Google Guice AbstractModule. Guice bindings live in the `protected void
// configure()` method; the def-use sniffer must capture the `String url =
// jdbcUrl` typed declaration AND its use in `bindUrl(url)`, both attributed
// to configure.
func TestStructural_Java_Guice_DefUseAttributes(t *testing.T) {
	const src = `package com.example.di;

import com.google.inject.AbstractModule;

public class AppModule extends AbstractModule {
    @Override
    protected void configure() {
        String url = jdbcUrl;
        bindUrl(url);
    }
}
`
	defs, uses := sniffDefUseJava(src)
	if !hasDefIn(defs, "url", "configure") {
		t.Errorf("def_use: expected def of local `url` in guice configure(), defs=%+v", defs)
	}
	if !hasUseIn(uses, "url", "configure") {
		t.Errorf("def_use: expected use of local `url` (in bindUrl(url)) in guice configure(), uses=%+v", uses)
	}
}

// --- Java GraphQL siblings: dgs + spring-graphql ----------------------------

// TestStructural_Java_Dgs_DefUseAttributes drives sniffDefUseJava on a
// Netflix DGS @DgsQuery datafetcher method. The def-use sniffer must capture
// the `List<User> result = repository.findAll()` typed declaration AND its
// use in `return result`, both attributed to the datafetcher method users.
func TestStructural_Java_Dgs_DefUseAttributes(t *testing.T) {
	const src = `package com.example.graphql;

import com.netflix.graphql.dgs.DgsComponent;
import com.netflix.graphql.dgs.DgsQuery;

@DgsComponent
public class UserDatafetcher {
    @DgsQuery
    public List<User> users(DataFetchingEnvironment env) {
        List<User> result = repository.findAll();
        return result;
    }
}
`
	defs, uses := sniffDefUseJava(src)
	if !hasDefIn(defs, "result", "users") {
		t.Errorf("def_use: expected def of local `result` in DGS datafetcher users, defs=%+v", defs)
	}
	if !hasUseIn(uses, "result", "users") {
		t.Errorf("def_use: expected use of local `result` (in return result) in DGS datafetcher users, uses=%+v", uses)
	}
}

// TestStructural_Java_SpringGraphql_DefUseAttributes drives sniffDefUseJava
// on a Spring for GraphQL @QueryMapping controller method. The def-use
// sniffer must capture the `Book found = service.lookup(id)` typed
// declaration AND its use in `return found`, both attributed to bookById.
func TestStructural_Java_SpringGraphql_DefUseAttributes(t *testing.T) {
	const src = `package com.example.graphql;

import org.springframework.graphql.data.method.annotation.QueryMapping;
import org.springframework.stereotype.Controller;

@Controller
public class BookController {
    @QueryMapping
    public Book bookById(@Argument String id) {
        Book found = service.lookup(id);
        return found;
    }
}
`
	defs, uses := sniffDefUseJava(src)
	if !hasDefIn(defs, "found", "bookById") {
		t.Errorf("def_use: expected def of local `found` in spring-graphql bookById, defs=%+v", defs)
	}
	if !hasUseIn(uses, "found", "bookById") {
		t.Errorf("def_use: expected use of local `found` (in return found) in spring-graphql bookById, uses=%+v", uses)
	}
}
