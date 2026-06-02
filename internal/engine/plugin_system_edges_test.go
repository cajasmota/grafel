// Tests for the applyPluginSystemEdges pass (#3628 area #25).
//
// Strategy: drive the pass directly via DetectorPassArgs (the pass is a pure
// function of Path+Content), then assert on the emitted SCOPE.Plugin entities
// and REGISTERS_PLUGIN edges. Assertions are VALUE-ASSERTING — they check the
// exact plugin name, the system, and the synthetic `plugin:<system>:<name>`
// ToID with a `File:<path>` FromID — not just len>0.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// pluginEdge is the minimal projection of a REGISTERS_PLUGIN edge.
type pluginEdge struct {
	From   string
	To     string
	Plugin string
	System string
	Group  string
}

// runPluginPass runs applyPluginSystemEdges on a single in-memory file and
// returns the Plugin entities + REGISTERS_PLUGIN edges it appended.
func runPluginPass(lang, path, src string) ([]types.EntityRecord, []pluginEdge) {
	res := applyPluginSystemEdges(DetectorPassArgs{
		Lang:    lang,
		Path:    path,
		Content: []byte(src),
	})
	var plugins []types.EntityRecord
	for _, e := range res.Entities {
		if e.Kind == pluginEntityKind {
			plugins = append(plugins, e)
		}
	}
	var edges []pluginEdge
	for _, r := range res.Relationships {
		if r.Kind != pluginEdgeKind {
			continue
		}
		edges = append(edges, pluginEdge{
			From:   r.FromID,
			To:     r.ToID,
			Plugin: r.Properties["plugin"],
			System: r.Properties["system"],
			Group:  r.Properties["group"],
		})
	}
	return plugins, edges
}

// findPluginEdge returns the REGISTERS_PLUGIN edge whose plugin name == name.
func findPluginEdge(edges []pluginEdge, name string) (pluginEdge, bool) {
	for _, e := range edges {
		if e.Plugin == name {
			return e, true
		}
	}
	return pluginEdge{}, false
}

// findPluginEntity returns the SCOPE.Plugin entity whose Name == name.
func findPluginEntity(ents []types.EntityRecord, name string) (types.EntityRecord, bool) {
	for _, e := range ents {
		if e.Name == name {
			return e, true
		}
	}
	return types.EntityRecord{}, false
}

// --- Webpack / Vite / Rollup ------------------------------------------------

func TestPluginPass_WebpackConstructorAndFactory(t *testing.T) {
	src := `
const HtmlWebpackPlugin = require('html-webpack-plugin');
const { terser } = require('rollup-plugin-terser');
module.exports = {
  plugins: [
    new HtmlWebpackPlugin(),
    terser(),
  ],
};
`
	ents, edges := runPluginPass("javascript", "webpack.config.js", src)

	e, ok := findPluginEdge(edges, "HtmlWebpackPlugin")
	if !ok {
		t.Fatalf("expected plugin HtmlWebpackPlugin registered, got edges=%+v", edges)
	}
	if e.System != "webpack" {
		t.Errorf("HtmlWebpackPlugin system = %q, want webpack", e.System)
	}
	if e.To != "plugin:webpack:HtmlWebpackPlugin" {
		t.Errorf("HtmlWebpackPlugin ToID = %q, want plugin:webpack:HtmlWebpackPlugin", e.To)
	}
	if e.From != "File:webpack.config.js" {
		t.Errorf("FromID = %q, want File:webpack.config.js", e.From)
	}

	if _, ok := findPluginEdge(edges, "terser"); !ok {
		t.Errorf("expected factory plugin terser registered, got edges=%+v", edges)
	}

	// Entity assertion.
	if pe, ok := findPluginEntity(ents, "HtmlWebpackPlugin"); !ok {
		t.Errorf("expected SCOPE.Plugin entity HtmlWebpackPlugin")
	} else if pe.Subtype != "webpack" {
		t.Errorf("entity Subtype = %q, want webpack", pe.Subtype)
	}
}

func TestPluginPass_ViteSystemTag(t *testing.T) {
	src := `
import vue from '@vitejs/plugin-vue';
export default {
  plugins: [vue()],
};
`
	_, edges := runPluginPass("typescript", "vite.config.ts", src)
	e, ok := findPluginEdge(edges, "vue")
	if !ok {
		t.Fatalf("expected vite plugin vue, got %+v", edges)
	}
	if e.System != "vite" {
		t.Errorf("vue system = %q, want vite", e.System)
	}
	if e.To != "plugin:vite:vue" {
		t.Errorf("ToID = %q, want plugin:vite:vue", e.To)
	}
}

func TestPluginPass_BundlerSkipsSpreadAndVariable(t *testing.T) {
	// Honest-partial: a spread or bare variable element must NOT become a
	// fabricated plugin entity.
	src := `
module.exports = {
  plugins: [
    ...basePlugins,
    myPluginVar,
    new RealPlugin(),
  ],
};
`
	_, edges := runPluginPass("javascript", "webpack.config.js", src)
	if _, ok := findPluginEdge(edges, "basePlugins"); ok {
		t.Errorf("spread element must not yield a plugin, got %+v", edges)
	}
	if _, ok := findPluginEdge(edges, "myPluginVar"); ok {
		t.Errorf("bare variable element must not yield a plugin, got %+v", edges)
	}
	if _, ok := findPluginEdge(edges, "RealPlugin"); !ok {
		t.Errorf("expected RealPlugin constructor registered, got %+v", edges)
	}
}

// --- Babel / ESLint ---------------------------------------------------------

func TestPluginPass_BabelStringPlugins(t *testing.T) {
	src := `{
  "plugins": ["@babel/plugin-transform-runtime", "macros"]
}`
	_, edges := runPluginPass("json", ".babelrc", src)
	e, ok := findPluginEdge(edges, "@babel/plugin-transform-runtime")
	if !ok {
		t.Fatalf("expected babel plugin, got %+v", edges)
	}
	if e.System != "babel" {
		t.Errorf("system = %q, want babel", e.System)
	}
	if e.To != "plugin:babel:@babel/plugin-transform-runtime" {
		t.Errorf("ToID = %q", e.To)
	}
	if _, ok := findPluginEdge(edges, "macros"); !ok {
		t.Errorf("expected babel plugin macros, got %+v", edges)
	}
}

func TestPluginPass_ESLintPluginsAndExtends(t *testing.T) {
	src := `{
  "plugins": ["react", "@typescript-eslint"],
  "extends": ["airbnb", "prettier"]
}`
	_, edges := runPluginPass("json", ".eslintrc.json", src)
	for _, want := range []string{"react", "@typescript-eslint", "airbnb", "prettier"} {
		e, ok := findPluginEdge(edges, want)
		if !ok {
			t.Errorf("expected eslint plugin %q, got %+v", want, edges)
			continue
		}
		if e.System != "eslint" {
			t.Errorf("%q system = %q, want eslint", want, e.System)
		}
	}
}

// --- pytest -----------------------------------------------------------------

func TestPluginPass_PytestPlugins(t *testing.T) {
	src := `
pytest_plugins = ["myproj.fixtures", "pytester"]

def test_x():
    pass
`
	_, edges := runPluginPass("python", "conftest.py", src)
	e, ok := findPluginEdge(edges, "myproj.fixtures")
	if !ok {
		t.Fatalf("expected pytest plugin myproj.fixtures, got %+v", edges)
	}
	if e.System != "pytest" {
		t.Errorf("system = %q, want pytest", e.System)
	}
	if e.To != "plugin:pytest:myproj.fixtures" {
		t.Errorf("ToID = %q", e.To)
	}
}

// --- setuptools entry points ------------------------------------------------

func TestPluginPass_SetuptoolsEntryPoints(t *testing.T) {
	src := `
from setuptools import setup

setup(
    name="myproj",
    entry_points={
        "flake8.extension": [
            "X = mod:Cls",
            "Y = mod.other:Other",
        ],
        "console_scripts": [
            "myprog = myproj.cli:main",
        ],
    },
)
`
	_, edges := runPluginPass("python", "setup.py", src)
	e, ok := findPluginEdge(edges, "X")
	if !ok {
		t.Fatalf("expected entry-point plugin X, got %+v", edges)
	}
	if e.System != "setuptools" {
		t.Errorf("system = %q, want setuptools", e.System)
	}
	if e.Group != "flake8.extension" {
		t.Errorf("group = %q, want flake8.extension", e.Group)
	}
	if e.To != "plugin:setuptools:X" {
		t.Errorf("ToID = %q, want plugin:setuptools:X", e.To)
	}

	myprog, ok := findPluginEdge(edges, "myprog")
	if !ok {
		t.Fatalf("expected console_scripts entry myprog, got %+v", edges)
	}
	if myprog.Group != "console_scripts" {
		t.Errorf("myprog group = %q, want console_scripts", myprog.Group)
	}
}

// --- Maven ------------------------------------------------------------------

func TestPluginPass_MavenPlugins(t *testing.T) {
	src := `<project>
  <build>
    <plugins>
      <plugin>
        <groupId>org.apache.maven.plugins</groupId>
        <artifactId>maven-compiler-plugin</artifactId>
        <version>3.11.0</version>
      </plugin>
      <plugin>
        <artifactId>maven-surefire-plugin</artifactId>
      </plugin>
    </plugins>
  </build>
  <dependencies>
    <dependency>
      <artifactId>guava</artifactId>
    </dependency>
  </dependencies>
</project>`
	_, edges := runPluginPass("xml", "pom.xml", src)
	e, ok := findPluginEdge(edges, "maven-compiler-plugin")
	if !ok {
		t.Fatalf("expected maven-compiler-plugin, got %+v", edges)
	}
	if e.System != "maven" {
		t.Errorf("system = %q, want maven", e.System)
	}
	if e.To != "plugin:maven:maven-compiler-plugin" {
		t.Errorf("ToID = %q", e.To)
	}
	if _, ok := findPluginEdge(edges, "maven-surefire-plugin"); !ok {
		t.Errorf("expected maven-surefire-plugin, got %+v", edges)
	}
	// Negative: a <dependency>'s artifactId must NOT be a plugin.
	if _, ok := findPluginEdge(edges, "guava"); ok {
		t.Errorf("dependency guava must not be a plugin, got %+v", edges)
	}
}

// --- Gradle -----------------------------------------------------------------

func TestPluginPass_GradleGroovyDSL(t *testing.T) {
	src := `
plugins {
    id 'java'
    id 'org.springframework.boot' version '3.2.0'
}
`
	_, edges := runPluginPass("groovy", "build.gradle", src)
	e, ok := findPluginEdge(edges, "java")
	if !ok {
		t.Fatalf("expected gradle plugin java, got %+v", edges)
	}
	if e.System != "gradle" {
		t.Errorf("system = %q, want gradle", e.System)
	}
	if e.To != "plugin:gradle:java" {
		t.Errorf("ToID = %q, want plugin:gradle:java", e.To)
	}
	if _, ok := findPluginEdge(edges, "org.springframework.boot"); !ok {
		t.Errorf("expected spring boot gradle plugin, got %+v", edges)
	}
}

func TestPluginPass_GradleKotlinDSL(t *testing.T) {
	src := `
plugins {
    id("java")
    kotlin("jvm") version "1.9.0"
    id("org.jetbrains.kotlin.plugin.spring")
}
`
	_, edges := runPluginPass("kotlin", "build.gradle.kts", src)
	if e, ok := findPluginEdge(edges, "java"); !ok {
		t.Fatalf("expected gradle kotlin-DSL plugin java, got %+v", edges)
	} else if e.System != "gradle" {
		t.Errorf("system = %q, want gradle", e.System)
	}
	if _, ok := findPluginEdge(edges, "org.jetbrains.kotlin.plugin.spring"); !ok {
		t.Errorf("expected kotlin spring plugin, got %+v", edges)
	}
}

// --- Negative / no-op cases -------------------------------------------------

func TestPluginPass_NonConfigFileNoPlugins(t *testing.T) {
	// A regular source file that happens to contain the word "plugins"
	// must NOT yield any plugin entity/edge.
	src := `
function loadPlugins() {
  const plugins = [new Foo(), bar()];
  return plugins;
}
`
	ents, edges := runPluginPass("javascript", "src/loader.js", src)
	if len(ents) != 0 || len(edges) != 0 {
		t.Errorf("non-config file must yield no plugins, got ents=%d edges=%d", len(ents), len(edges))
	}
}

func TestPluginPass_EmptyContentNoOp(t *testing.T) {
	ents, edges := runPluginPass("javascript", "webpack.config.js", "")
	if len(ents) != 0 || len(edges) != 0 {
		t.Errorf("empty content must be a no-op, got ents=%d edges=%d", len(ents), len(edges))
	}
}

func TestPluginPass_Dedup(t *testing.T) {
	// The same plugin declared twice yields a single entity + single edge.
	src := `{
  "plugins": ["react", "react"]
}`
	ents, edges := runPluginPass("json", ".eslintrc", src)
	count := 0
	for _, e := range edges {
		if e.Plugin == "react" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 react edge after dedup, got %d", count)
	}
	entCount := 0
	for _, e := range ents {
		if e.Name == "react" {
			entCount++
		}
	}
	if entCount != 1 {
		t.Errorf("expected exactly 1 react entity after dedup, got %d", entCount)
	}
}

func TestPluginPass_PassThroughPreservesPriorSlices(t *testing.T) {
	// The pass must thread through pre-existing entities/relationships
	// unmodified (append-only contract).
	priorEnt := types.EntityRecord{ID: "X", Kind: "SCOPE.Function"}
	priorRel := types.RelationshipRecord{FromID: "A", ToID: "B", Kind: "CALLS"}
	res := applyPluginSystemEdges(DetectorPassArgs{
		Lang:          "javascript",
		Path:          "webpack.config.js",
		Content:       []byte(`module.exports = { plugins: [new P()] };`),
		Entities:      []types.EntityRecord{priorEnt},
		Relationships: []types.RelationshipRecord{priorRel},
	})
	if len(res.Entities) < 2 || res.Entities[0].ID != "X" {
		t.Errorf("prior entity not preserved: %+v", res.Entities)
	}
	if len(res.Relationships) < 2 || res.Relationships[0].Kind != "CALLS" {
		t.Errorf("prior relationship not preserved: %+v", res.Relationships)
	}
}
