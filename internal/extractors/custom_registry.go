// custom_registry.go wires all framework-specific custom extractors so their
// init() functions run and register against the global extractor.Registry.
//
// Add a new blank import here whenever a new internal/custom/<lang> package
// is created.
package extractors

import (
	_ "github.com/cajasmota/archigraph/internal/custom/cpp"
	_ "github.com/cajasmota/archigraph/internal/custom/crystal"
	_ "github.com/cajasmota/archigraph/internal/custom/csharp"
	_ "github.com/cajasmota/archigraph/internal/custom/dart"
	_ "github.com/cajasmota/archigraph/internal/custom/elixir"
	_ "github.com/cajasmota/archigraph/internal/custom/fsharp"
	_ "github.com/cajasmota/archigraph/internal/custom/golang"
	_ "github.com/cajasmota/archigraph/internal/custom/java"
	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
	_ "github.com/cajasmota/archigraph/internal/custom/kotlin"
	_ "github.com/cajasmota/archigraph/internal/custom/lua"
	_ "github.com/cajasmota/archigraph/internal/custom/php"
	_ "github.com/cajasmota/archigraph/internal/custom/python"
	_ "github.com/cajasmota/archigraph/internal/custom/ruby"
	_ "github.com/cajasmota/archigraph/internal/custom/rust"
	_ "github.com/cajasmota/archigraph/internal/custom/scala"
	_ "github.com/cajasmota/archigraph/internal/custom/swift"
)
