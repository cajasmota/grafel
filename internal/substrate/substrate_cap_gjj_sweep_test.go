package substrate

import "testing"

// substrate_cap_gjj_sweep_test.go drives the framework-AGNOSTIC,
// per-LANGUAGE constant-binding sniffers (sniffGo / sniffJava / sniffJSTS)
// on the idiom of trailing-sibling frameworks that a credit wave left as
// stale `missing` Substrate cells in docs/coverage/registry.json (epic
// #3872). Each sniffer is registered on the LANGUAGE slug
// (Register("go"/"java"/"jsts", ...)) and gates PURELY on file CONTENT —
// there is zero per-framework branching — so a gqlgen / fx / wire (Go),
// dgs / guice (Java), pothos / type-graphql (JS/TS) source file dispatches
// the SAME sniffer as its flagship HTTP siblings (gin / spring-boot /
// express). These tests PROVE that by asserting the EXACT
// literal / env-var / default / import-source / confidence the sniffer
// emits on each trailing sibling's idiom (never len>0).
//
// Caps proven per cell (mirrors the koin/dagger-hilt/hotchocolate/
// graphql-ruby precedent merged in PR #4182):
//   constant_propagation       -> ProvenanceLiteral + exact Value + Confidence 1.0
//   env_fallback_recognition   -> ProvenanceEnvFallback + exact EnvVar/Value + Confidence 0.85
//   import_resolution_quality  -> ProvenanceCrossFile + exact ImportSource + Confidence 0.6
//   confidence_overlay         -> the exact per-Binding Confidence the #2769 graph-wide overlay consumes

// TestSubstrateCapGJJ_GoTrailingSiblings drives sniffGo on each Go
// trailing-sibling idiom: gqlgen (generated GraphQL resolver), fx
// (uber-go/fx DI module), wire (google/wire provider set). All are .go
// files, so the Go substrate sniffer fires identically to gin/echo.
func TestSubstrateCapGJJ_GoTrailingSiblings(t *testing.T) {
	// gqlgen: a resolver file with the schema-doc const, an env-fallback for
	// the playground endpoint, and the canonical gqlgen runtime imports.
	t.Run("gqlgen", func(t *testing.T) {
		const src = `package graph

import (
	"github.com/99designs/gqlgen/graphql"
	gqlhandler "github.com/99designs/gqlgen/graphql/handler"
)

const GqlgenSchemaPath = "/query"
var GqlgenPlaygroundURL = cmp.Or(os.Getenv("GQLGEN_PLAYGROUND_URL"), "http://localhost:8080/")
`
		by := bindMap(sniffGo(src))
		if g := by["GqlgenSchemaPath"]; g.Value != "/query" || g.Provenance != ProvenanceLiteral || g.Confidence != 1.0 {
			t.Errorf("constant_propagation GqlgenSchemaPath: %+v", g)
		}
		if g := by["GqlgenPlaygroundURL"]; g.Value != "http://localhost:8080/" || g.EnvVar != "GQLGEN_PLAYGROUND_URL" || g.Provenance != ProvenanceEnvFallback || g.Confidence != 0.85 {
			t.Errorf("env_fallback_recognition GqlgenPlaygroundURL: %+v", g)
		}
		if g := by["graphql"]; g.ImportSource != "github.com/99designs/gqlgen/graphql" || g.Provenance != ProvenanceCrossFile || g.Confidence != 0.6 {
			t.Errorf("import_resolution_quality gqlgen graphql import: %+v", g)
		}
		if g := by["gqlhandler"]; g.ImportSource != "github.com/99designs/gqlgen/graphql/handler" {
			t.Errorf("import_resolution_quality aliased gqlgen handler import: %+v", g)
		}
	})

	// fx: a uber-go/fx module-provider file with a module-name const, an
	// env-fallback for the bind address, and the fx runtime imports.
	t.Run("fx", func(t *testing.T) {
		const src = `package app

import (
	"go.uber.org/fx"
	fxevent "go.uber.org/fx/fxevent"
)

const FxModuleName = "http-server"
var FxBindAddr = cmp.Or(os.Getenv("FX_BIND_ADDR"), ":3000")
`
		by := bindMap(sniffGo(src))
		if g := by["FxModuleName"]; g.Value != "http-server" || g.Provenance != ProvenanceLiteral || g.Confidence != 1.0 {
			t.Errorf("constant_propagation FxModuleName: %+v", g)
		}
		if g := by["FxBindAddr"]; g.Value != ":3000" || g.EnvVar != "FX_BIND_ADDR" || g.Provenance != ProvenanceEnvFallback || g.Confidence != 0.85 {
			t.Errorf("env_fallback_recognition FxBindAddr: %+v", g)
		}
		if g := by["fx"]; g.ImportSource != "go.uber.org/fx" || g.Provenance != ProvenanceCrossFile || g.Confidence != 0.6 {
			t.Errorf("import_resolution_quality fx import: %+v", g)
		}
		if g := by["fxevent"]; g.ImportSource != "go.uber.org/fx/fxevent" {
			t.Errorf("import_resolution_quality aliased fxevent import: %+v", g)
		}
	})

	// wire: a google/wire provider-set file with a provider-set name const,
	// an env-fallback for the DSN, and the wire imports.
	t.Run("wire", func(t *testing.T) {
		const src = `package di

import (
	"github.com/google/wire"
	googlewire "github.com/google/wire/cmd/wire"
)

const WireProviderSet = "AppSet"
var WireDSN = cmp.Or(os.Getenv("WIRE_DSN"), "postgres://localhost/wire")
`
		by := bindMap(sniffGo(src))
		if g := by["WireProviderSet"]; g.Value != "AppSet" || g.Provenance != ProvenanceLiteral || g.Confidence != 1.0 {
			t.Errorf("constant_propagation WireProviderSet: %+v", g)
		}
		if g := by["WireDSN"]; g.Value != "postgres://localhost/wire" || g.EnvVar != "WIRE_DSN" || g.Provenance != ProvenanceEnvFallback || g.Confidence != 0.85 {
			t.Errorf("env_fallback_recognition WireDSN: %+v", g)
		}
		if g := by["wire"]; g.ImportSource != "github.com/google/wire" || g.Provenance != ProvenanceCrossFile || g.Confidence != 0.6 {
			t.Errorf("import_resolution_quality wire import: %+v", g)
		}
		if g := by["googlewire"]; g.ImportSource != "github.com/google/wire/cmd/wire" {
			t.Errorf("import_resolution_quality aliased wire cmd import: %+v", g)
		}
	})
}

// TestSubstrateCapGJJ_JavaTrailingSiblings drives sniffJava on each Java
// trailing-sibling idiom: dgs (Netflix DGS GraphQL @DgsComponent), guice
// (Google Guice AbstractModule). Both are .java files, so the Java
// substrate sniffer fires identically to spring-boot/micronaut.
func TestSubstrateCapGJJ_JavaTrailingSiblings(t *testing.T) {
	// dgs: a @DgsComponent datafetcher class with a schema-path constant, an
	// Optional.ofNullable env-fallback, and the DGS framework imports.
	t.Run("dgs", func(t *testing.T) {
		const src = `package com.example.graphql;

import com.netflix.graphql.dgs.DgsComponent;
import com.netflix.graphql.dgs.DgsQuery;

@DgsComponent
public class UserDatafetcher {
    public static final String DGS_SCHEMA_PATH = "/graphql";
    public static final String DGS_ENDPOINT =
        Optional.ofNullable(System.getenv("DGS_ENDPOINT")).orElse("http://localhost:8080/graphql");
}
`
		by := bindMap(sniffJava(src))
		if g := by["DGS_SCHEMA_PATH"]; g.Value != "/graphql" || g.Provenance != ProvenanceLiteral || g.Confidence != 1.0 {
			t.Errorf("constant_propagation DGS_SCHEMA_PATH: %+v", g)
		}
		if g := by["DGS_ENDPOINT"]; g.Value != "http://localhost:8080/graphql" || g.EnvVar != "DGS_ENDPOINT" || g.Provenance != ProvenanceEnvFallback || g.Confidence != 0.85 {
			t.Errorf("env_fallback_recognition DGS_ENDPOINT: %+v", g)
		}
		if g := by["DgsComponent"]; g.ImportSource != "com.netflix.graphql.dgs" || g.Provenance != ProvenanceCrossFile || g.Confidence != 0.6 {
			t.Errorf("import_resolution_quality DgsComponent import: %+v", g)
		}
		if g := by["DgsQuery"]; g.ImportSource != "com.netflix.graphql.dgs" {
			t.Errorf("import_resolution_quality DgsQuery import: %+v", g)
		}
	})

	// guice: an AbstractModule subclass with a binding-name constant, a
	// System.getenv ternary env-fallback, and the Guice imports.
	t.Run("guice", func(t *testing.T) {
		const src = `package com.example.di;

import com.google.inject.AbstractModule;
import com.google.inject.name.Named;

public class AppModule extends AbstractModule {
    public static final String GUICE_BINDING_NAME = "primaryDataSource";
    public static final String GUICE_JDBC_URL =
        System.getenv("GUICE_JDBC_URL") != null ? System.getenv("GUICE_JDBC_URL") : "jdbc:postgresql://localhost/guice";

    @Override
    protected void configure() {}
}
`
		by := bindMap(sniffJava(src))
		if g := by["GUICE_BINDING_NAME"]; g.Value != "primaryDataSource" || g.Provenance != ProvenanceLiteral || g.Confidence != 1.0 {
			t.Errorf("constant_propagation GUICE_BINDING_NAME: %+v", g)
		}
		if g := by["GUICE_JDBC_URL"]; g.Value != "jdbc:postgresql://localhost/guice" || g.EnvVar != "GUICE_JDBC_URL" || g.Provenance != ProvenanceEnvFallback || g.Confidence != 0.85 {
			t.Errorf("env_fallback_recognition GUICE_JDBC_URL: %+v", g)
		}
		if g := by["AbstractModule"]; g.ImportSource != "com.google.inject" || g.Provenance != ProvenanceCrossFile || g.Confidence != 0.6 {
			t.Errorf("import_resolution_quality AbstractModule import: %+v", g)
		}
		if g := by["Named"]; g.ImportSource != "com.google.inject.name" {
			t.Errorf("import_resolution_quality Named import: %+v", g)
		}
	})
}

// TestSubstrateCapGJJ_JstsTrailingSiblings drives sniffJSTS on each JS/TS
// trailing-sibling idiom: pothos (@pothos/core SchemaBuilder), type-graphql
// (@ObjectType decorator classes). Both are .ts files, so the JS/TS
// substrate sniffer fires identically to express/nestjs.
func TestSubstrateCapGJJ_JstsTrailingSiblings(t *testing.T) {
	// pothos: a SchemaBuilder module with a schema-path const, a process.env
	// ?? fallback for the endpoint, and the @pothos/core imports.
	t.Run("pothos", func(t *testing.T) {
		const src = `import SchemaBuilder from "@pothos/core";
import { PrismaPlugin } from "@pothos/plugin-prisma";

export const POTHOS_SCHEMA_PATH = "/graphql";
const POTHOS_ENDPOINT = process.env.POTHOS_ENDPOINT ?? "http://localhost:4000/graphql";
`
		by := bindMap(sniffJSTS(src))
		if g := by["POTHOS_SCHEMA_PATH"]; g.Value != "/graphql" || g.Provenance != ProvenanceLiteral || g.Confidence != 1.0 {
			t.Errorf("constant_propagation POTHOS_SCHEMA_PATH: %+v", g)
		}
		if g := by["POTHOS_ENDPOINT"]; g.Value != "http://localhost:4000/graphql" || g.EnvVar != "POTHOS_ENDPOINT" || g.Provenance != ProvenanceEnvFallback || g.Confidence != 0.85 {
			t.Errorf("env_fallback_recognition POTHOS_ENDPOINT: %+v", g)
		}
		if g := by["PrismaPlugin"]; g.ImportSource != "@pothos/plugin-prisma" || g.Provenance != ProvenanceCrossFile || g.Confidence != 0.6 {
			t.Errorf("import_resolution_quality pothos PrismaPlugin import: %+v", g)
		}
	})

	// type-graphql: an @ObjectType/@Resolver module with a route const, a
	// process.env || fallback for the path, and the type-graphql imports.
	t.Run("type-graphql", func(t *testing.T) {
		const src = `import { ObjectType, Field } from "type-graphql";
import { Resolver as TGResolver } from "type-graphql";

export const TYPEGQL_ROUTE = "/api/graphql";
const TYPEGQL_PLAYGROUND = process.env.TYPEGQL_PLAYGROUND || "http://localhost:5000/playground";
`
		by := bindMap(sniffJSTS(src))
		if g := by["TYPEGQL_ROUTE"]; g.Value != "/api/graphql" || g.Provenance != ProvenanceLiteral || g.Confidence != 1.0 {
			t.Errorf("constant_propagation TYPEGQL_ROUTE: %+v", g)
		}
		if g := by["TYPEGQL_PLAYGROUND"]; g.Value != "http://localhost:5000/playground" || g.EnvVar != "TYPEGQL_PLAYGROUND" || g.Provenance != ProvenanceEnvFallback || g.Confidence != 0.85 {
			t.Errorf("env_fallback_recognition TYPEGQL_PLAYGROUND: %+v", g)
		}
		if g := by["ObjectType"]; g.ImportSource != "type-graphql" || g.Provenance != ProvenanceCrossFile || g.Confidence != 0.6 {
			t.Errorf("import_resolution_quality type-graphql ObjectType import: %+v", g)
		}
		if g := by["TGResolver"]; g.ImportSource != "type-graphql" {
			t.Errorf("import_resolution_quality aliased type-graphql Resolver import: %+v", g)
		}
	})
}
