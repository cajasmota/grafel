package substrate

import "testing"

// substrate_cap_sibling_sweep_test.go drives the framework-AGNOSTIC,
// per-LANGUAGE constant-binding sniffers (sniffCSharp / sniffKotlin /
// sniffRuby) on the idiom of trailing-sibling frameworks that a credit
// wave left as stale `missing` Substrate cells in docs/coverage/registry.json
// (epic #3872). Each sniffer is registered on the LANGUAGE slug
// (Register("csharp"/"kotlin"/"ruby", ...)) and gates purely on file
// CONTENT — there is zero per-framework branching — so a HotChocolate /
// Koin / Dagger-Hilt / Retrofit / kotlinx-serialization / graphql-kotlin /
// graphql-ruby source file dispatches the SAME sniffer as its flagship
// siblings. These tests PROVE that by asserting the EXACT
// literal / env-var / default / import-source / confidence the sniffer
// emits on each trailing sibling's idiom (never len>0).
//
// Caps proven per cell:
//   constant_propagation       -> ProvenanceLiteral + exact Value + Confidence 1.0
//   env_fallback_recognition   -> ProvenanceEnvFallback + exact EnvVar/Value + Confidence 0.85
//   import_resolution_quality  -> ProvenanceCrossFile + exact ImportSource + Confidence 0.6
//   confidence_overlay         -> the exact per-Binding Confidence the #2769 graph-wide overlay consumes

// TestSubstrateCap_HotChocolate_CSharp drives sniffCSharp on a HotChocolate
// GraphQL resolver-class file. HotChocolate is a .cs file like any other, so
// the C# substrate sniffer fires identically to aspnet-core/servicestack.
func TestSubstrateCap_HotChocolate_CSharp(t *testing.T) {
	const src = `using HotChocolate;
using HotChocolate.Types;
using Cfg = MyApp.Configuration;

[QueryType]
public class UserQueries {
    public const string SchemaName = "https://schema.example.com/graphql";
    private static readonly string DefaultRole = "viewer";
    public static readonly string ConnString =
        Environment.GetEnvironmentVariable("GRAPHQL_DB") ?? "postgres://localhost/graphql";

    public User GetUser(int id) => null;
}
`
	by := bindMap(sniffCSharp(src))
	// constant_propagation
	if g := by["SchemaName"]; g.Value != "https://schema.example.com/graphql" || g.Provenance != ProvenanceLiteral || g.Confidence != 1.0 {
		t.Errorf("constant_propagation SchemaName: %+v", g)
	}
	if g := by["DefaultRole"]; g.Value != "viewer" || g.Provenance != ProvenanceLiteral {
		t.Errorf("constant_propagation DefaultRole: %+v", g)
	}
	// env_fallback_recognition
	if g := by["ConnString"]; g.Value != "postgres://localhost/graphql" || g.EnvVar != "GRAPHQL_DB" || g.Provenance != ProvenanceEnvFallback || g.Confidence != 0.85 {
		t.Errorf("env_fallback_recognition ConnString: %+v", g)
	}
	// import_resolution_quality
	if g := by["HotChocolate"]; g.ImportSource != "HotChocolate" || g.Provenance != ProvenanceCrossFile || g.Confidence != 0.6 {
		t.Errorf("import_resolution_quality HotChocolate using: %+v", g)
	}
	if g := by["Types"]; g.ImportSource != "HotChocolate" {
		t.Errorf("import_resolution_quality HotChocolate.Types using: %+v", g)
	}
	if g := by["Cfg"]; g.ImportSource != "MyApp.Configuration" {
		t.Errorf("import_resolution_quality aliased using: %+v", g)
	}
}

// TestSubstrateCap_KotlinTrailingSiblings drives sniffKotlin on each Kotlin
// trailing-sibling idiom: Koin (DI module), Dagger-Hilt (@Module object),
// Retrofit (service interface), kotlinx-serialization (@Serializable data
// class), graphql-kotlin (Query resolver). All are .kt files, so the Kotlin
// substrate sniffer fires identically to ktor/spring-boot.
func TestSubstrateCap_KotlinTrailingSiblings(t *testing.T) {
	// Koin: top-level const + env fallback + import.
	t.Run("koin", func(t *testing.T) {
		const src = `package com.example.di
import org.koin.dsl.module
import org.koin.core.module.Module as KoinModule

const val KOIN_QUALIFIER = "primaryDb"
val DB_URL = System.getenv("KOIN_DB_URL") ?: "jdbc:postgresql://localhost/koin"
`
		by := bindMap(sniffKotlin(src))
		if g := by["KOIN_QUALIFIER"]; g.Value != "primaryDb" || g.Provenance != ProvenanceLiteral || g.Confidence != 1.0 {
			t.Errorf("constant_propagation KOIN_QUALIFIER: %+v", g)
		}
		if g := by["DB_URL"]; g.Value != "jdbc:postgresql://localhost/koin" || g.EnvVar != "KOIN_DB_URL" || g.Provenance != ProvenanceEnvFallback || g.Confidence != 0.85 {
			t.Errorf("env_fallback_recognition DB_URL: %+v", g)
		}
		if g := by["module"]; g.ImportSource != "org.koin.dsl" || g.Provenance != ProvenanceCrossFile || g.Confidence != 0.6 {
			t.Errorf("import_resolution_quality koin module import: %+v", g)
		}
		if g := by["KoinModule"]; g.ImportSource != "org.koin.core.module.Module" {
			t.Errorf("import_resolution_quality aliased import: %+v", g)
		}
	})

	// Dagger-Hilt: @Module/@InstallIn object with top-level const + import.
	t.Run("dagger-hilt", func(t *testing.T) {
		const src = `package com.example.di
import dagger.hilt.android.HiltAndroidApp
import dagger.Module as DaggerModule

const val HILT_BASE_URL = "https://hilt.example.com/api"
val HILT_TIMEOUT = System.getenv("HILT_TIMEOUT_MS") ?: "30000"
`
		by := bindMap(sniffKotlin(src))
		if g := by["HILT_BASE_URL"]; g.Value != "https://hilt.example.com/api" || g.Provenance != ProvenanceLiteral {
			t.Errorf("constant_propagation HILT_BASE_URL: %+v", g)
		}
		if g := by["HILT_TIMEOUT"]; g.Value != "30000" || g.EnvVar != "HILT_TIMEOUT_MS" || g.Provenance != ProvenanceEnvFallback {
			t.Errorf("env_fallback_recognition HILT_TIMEOUT: %+v", g)
		}
		if g := by["HiltAndroidApp"]; g.ImportSource != "dagger.hilt.android" || g.Provenance != ProvenanceCrossFile {
			t.Errorf("import_resolution_quality hilt import: %+v", g)
		}
		if g := by["DaggerModule"]; g.ImportSource != "dagger.Module" {
			t.Errorf("import_resolution_quality aliased dagger import: %+v", g)
		}
	})

	// Retrofit: service-interface file with @GET base path const + env + import.
	t.Run("retrofit", func(t *testing.T) {
		const src = `package com.example.net
import retrofit2.http.GET
import retrofit2.Retrofit

const val RETROFIT_BASE = "https://api.example.com/v2/"
val RETROFIT_HOST = System.getenv("RETROFIT_HOST") ?: "api.example.com"
`
		by := bindMap(sniffKotlin(src))
		if g := by["RETROFIT_BASE"]; g.Value != "https://api.example.com/v2/" || g.Provenance != ProvenanceLiteral {
			t.Errorf("constant_propagation RETROFIT_BASE: %+v", g)
		}
		if g := by["RETROFIT_HOST"]; g.Value != "api.example.com" || g.EnvVar != "RETROFIT_HOST" || g.Provenance != ProvenanceEnvFallback {
			t.Errorf("env_fallback_recognition RETROFIT_HOST: %+v", g)
		}
		if g := by["GET"]; g.ImportSource != "retrofit2.http" || g.Provenance != ProvenanceCrossFile {
			t.Errorf("import_resolution_quality retrofit GET import: %+v", g)
		}
		if g := by["Retrofit"]; g.ImportSource != "retrofit2" {
			t.Errorf("import_resolution_quality retrofit import: %+v", g)
		}
	})

	// kotlinx-serialization: @Serializable data class file with const + import.
	t.Run("kotlinx-serialization", func(t *testing.T) {
		const src = `package com.example.model
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json as KJson

const val SERIAL_VERSION = "v1.2.0"
val SERIAL_MODE = System.getenv("SERIAL_MODE") ?: "lenient"
`
		by := bindMap(sniffKotlin(src))
		if g := by["SERIAL_VERSION"]; g.Value != "v1.2.0" || g.Provenance != ProvenanceLiteral {
			t.Errorf("constant_propagation SERIAL_VERSION: %+v", g)
		}
		if g := by["SERIAL_MODE"]; g.Value != "lenient" || g.EnvVar != "SERIAL_MODE" || g.Provenance != ProvenanceEnvFallback {
			t.Errorf("env_fallback_recognition SERIAL_MODE: %+v", g)
		}
		if g := by["Serializable"]; g.ImportSource != "kotlinx.serialization" || g.Provenance != ProvenanceCrossFile {
			t.Errorf("import_resolution_quality serialization import: %+v", g)
		}
		if g := by["KJson"]; g.ImportSource != "kotlinx.serialization.json.Json" {
			t.Errorf("import_resolution_quality aliased Json import: %+v", g)
		}
	})

	// graphql-kotlin: Query resolver file with schema const + env + import.
	t.Run("graphql-kotlin", func(t *testing.T) {
		const src = `package com.example.graphql
import com.expediagroup.graphql.server.operations.Query
import com.expediagroup.graphql.generator.annotations.GraphQLDescription as Desc

const val GQL_ENDPOINT = "/graphql"
val GQL_PLAYGROUND = System.getenv("GQL_PLAYGROUND") ?: "enabled"
`
		by := bindMap(sniffKotlin(src))
		if g := by["GQL_ENDPOINT"]; g.Value != "/graphql" || g.Provenance != ProvenanceLiteral {
			t.Errorf("constant_propagation GQL_ENDPOINT: %+v", g)
		}
		if g := by["GQL_PLAYGROUND"]; g.Value != "enabled" || g.EnvVar != "GQL_PLAYGROUND" || g.Provenance != ProvenanceEnvFallback {
			t.Errorf("env_fallback_recognition GQL_PLAYGROUND: %+v", g)
		}
		if g := by["Query"]; g.ImportSource != "com.expediagroup.graphql.server.operations" || g.Provenance != ProvenanceCrossFile {
			t.Errorf("import_resolution_quality graphql-kotlin Query import: %+v", g)
		}
		if g := by["Desc"]; g.ImportSource != "com.expediagroup.graphql.generator.annotations.GraphQLDescription" {
			t.Errorf("import_resolution_quality aliased Desc import: %+v", g)
		}
	})
}

// TestSubstrateCap_GraphQLRuby drives sniffRuby on a graphql-ruby type/schema
// file. graphql-ruby is a .rb file, so the Ruby substrate sniffer fires
// identically to rails/sinatra.
func TestSubstrateCap_GraphQLRuby(t *testing.T) {
	const src = `require "graphql"
require_relative "./types/user_type"

GRAPHQL_PATH = "/graphql"
DEFAULT_COMPLEXITY = '100'
MAX_DEPTH = ENV.fetch("GRAPHQL_MAX_DEPTH", "15")
SCHEMA_URL = ENV["GRAPHQL_SCHEMA_URL"] || "https://schema.example.com"
`
	by := bindMap(sniffRuby(src))
	// constant_propagation
	if g := by["GRAPHQL_PATH"]; g.Value != "/graphql" || g.Provenance != ProvenanceLiteral || g.Confidence != 1.0 {
		t.Errorf("constant_propagation GRAPHQL_PATH: %+v", g)
	}
	if g := by["DEFAULT_COMPLEXITY"]; g.Value != "100" || g.Provenance != ProvenanceLiteral {
		t.Errorf("constant_propagation DEFAULT_COMPLEXITY: %+v", g)
	}
	// env_fallback_recognition (ENV.fetch + ENV[]||)
	if g := by["MAX_DEPTH"]; g.Value != "15" || g.EnvVar != "GRAPHQL_MAX_DEPTH" || g.Provenance != ProvenanceEnvFallback || g.Confidence != 0.85 {
		t.Errorf("env_fallback_recognition MAX_DEPTH: %+v", g)
	}
	if g := by["SCHEMA_URL"]; g.Value != "https://schema.example.com" || g.EnvVar != "GRAPHQL_SCHEMA_URL" || g.Provenance != ProvenanceEnvFallback {
		t.Errorf("env_fallback_recognition SCHEMA_URL: %+v", g)
	}
	// import_resolution_quality (require + require_relative)
	if g := by["graphql"]; g.ImportSource != "graphql" || g.Provenance != ProvenanceCrossFile || g.Confidence != 0.6 {
		t.Errorf("import_resolution_quality graphql require: %+v", g)
	}
	if g := by["user_type"]; g.ImportSource != "./types/user_type" || g.Provenance != ProvenanceCrossFile {
		t.Errorf("import_resolution_quality user_type require_relative: %+v", g)
	}
}
