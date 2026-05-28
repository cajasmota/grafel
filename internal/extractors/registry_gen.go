// Package extractors imports all extractor sub-packages so their init()
// functions register with the global registry. This file is MANUALLY
// MAINTAINED — add a new blank import in alphabetical order when adding
// a new extractor. See REGISTRY_CODEGEN.md for details.
//
// Do NOT remove this file or its imports — the registry depends on
// side-effects from each sub-package's init().

package extractors

import (
	_ "github.com/cajasmota/archigraph/internal/extractors/assembly"
	_ "github.com/cajasmota/archigraph/internal/extractors/astro"
	_ "github.com/cajasmota/archigraph/internal/extractors/clojure"
	_ "github.com/cajasmota/archigraph/internal/extractors/cpp"
	_ "github.com/cajasmota/archigraph/internal/extractors/crystal"
	_ "github.com/cajasmota/archigraph/internal/extractors/csharp"
	_ "github.com/cajasmota/archigraph/internal/extractors/css"
	_ "github.com/cajasmota/archigraph/internal/extractors/dart"
	_ "github.com/cajasmota/archigraph/internal/extractors/dockerfile"
	_ "github.com/cajasmota/archigraph/internal/extractors/elixir"
	_ "github.com/cajasmota/archigraph/internal/extractors/elm"
	_ "github.com/cajasmota/archigraph/internal/extractors/erlang"
	_ "github.com/cajasmota/archigraph/internal/extractors/fish"
	_ "github.com/cajasmota/archigraph/internal/extractors/fsharp"
	_ "github.com/cajasmota/archigraph/internal/extractors/golang"
	_ "github.com/cajasmota/archigraph/internal/extractors/graphql"
	_ "github.com/cajasmota/archigraph/internal/extractors/groovy"
	_ "github.com/cajasmota/archigraph/internal/extractors/haskell"
	_ "github.com/cajasmota/archigraph/internal/extractors/hcl"
	_ "github.com/cajasmota/archigraph/internal/extractors/html"
	_ "github.com/cajasmota/archigraph/internal/extractors/idris"
	_ "github.com/cajasmota/archigraph/internal/extractors/java"
	_ "github.com/cajasmota/archigraph/internal/extractors/javascript"
	_ "github.com/cajasmota/archigraph/internal/extractors/just"
	_ "github.com/cajasmota/archigraph/internal/extractors/kotlin"
	_ "github.com/cajasmota/archigraph/internal/extractors/lisp"
	_ "github.com/cajasmota/archigraph/internal/extractors/lua"
	_ "github.com/cajasmota/archigraph/internal/extractors/markdown"
	_ "github.com/cajasmota/archigraph/internal/extractors/nim"
	_ "github.com/cajasmota/archigraph/internal/extractors/ocaml"
	_ "github.com/cajasmota/archigraph/internal/extractors/php"
	_ "github.com/cajasmota/archigraph/internal/extractors/pony"
	_ "github.com/cajasmota/archigraph/internal/extractors/proto"
	_ "github.com/cajasmota/archigraph/internal/extractors/python"
	_ "github.com/cajasmota/archigraph/internal/extractors/razor"
	_ "github.com/cajasmota/archigraph/internal/extractors/reasonml"
	_ "github.com/cajasmota/archigraph/internal/extractors/rescript"
	_ "github.com/cajasmota/archigraph/internal/extractors/ruby"
	_ "github.com/cajasmota/archigraph/internal/extractors/rust"
	_ "github.com/cajasmota/archigraph/internal/extractors/scala"
	_ "github.com/cajasmota/archigraph/internal/extractors/shell"
	_ "github.com/cajasmota/archigraph/internal/extractors/sml"
	_ "github.com/cajasmota/archigraph/internal/extractors/solidity"
	_ "github.com/cajasmota/archigraph/internal/extractors/sql"
	_ "github.com/cajasmota/archigraph/internal/extractors/svelte"
	_ "github.com/cajasmota/archigraph/internal/extractors/swift"
	_ "github.com/cajasmota/archigraph/internal/extractors/verilog"
	_ "github.com/cajasmota/archigraph/internal/extractors/vhdl"
	_ "github.com/cajasmota/archigraph/internal/extractors/vue"
	_ "github.com/cajasmota/archigraph/internal/extractors/yaml"
	_ "github.com/cajasmota/archigraph/internal/extractors/zig"
)
