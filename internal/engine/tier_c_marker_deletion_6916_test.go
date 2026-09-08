package engine

import (
	"slices"
	"strings"
	"testing"
)

// #6916 Tier C — the nine `name_group: 0` source_patterns that should not have
// been entity rules at all, deleted rather than renamed:
//
//	rust/frameworks/actix_web.yaml:90    Config    "#[actix_web::main]"
//	kotlin/frameworks/kmp.yaml:83        Config    "sourceSets {"
//	ruby/frameworks/sinatra.yaml:73      Service   "helpers do"
//	ruby/frameworks/sinatra.yaml:93      Middleware "before '/admin/*' do"
//	ruby/frameworks/sinatra.yaml:99      Middleware "after do"
//	ruby/frameworks/sinatra.yaml:117     Config    "Sinatra::Application.run!"
//	javascript_typescript/frameworks/langchain.yaml:42  Operation "RunnableSequence.from("
//	javascript_typescript/frameworks/langchain.yaml:66  Operation "tool(async ("
//	javascript_typescript/frameworks/langchain.yaml:71  Operation ".bindTools("
//
// (Line numbers are the pre-deletion ones, from the measurement arm's table.)
//
// A DELETION IS UNGRADEABLE BY A RECALL TEST. Nothing in the suite fails when a
// rule that mints an entity nobody asked for comes back — only a forbidden
// assertion can see it. Every site below therefore carries:
//
//   - an input that PROVABLY produced the marker before the deletion (each was
//     driven through the real detector on the parent commit and printed), and
//   - a positive control from the SAME file: an entity the surviving patterns
//     still mint. Without it these absence assertions would pass on a fixture
//     that silently stopped producing anything at all — for instance if the
//     whole rule file failed to load.
//
// The sibling axis is named explicitly, because enumerating one axis is what
// makes its neighbour look covered. Each of the four touched rule files KEEPS
// patterns that were not part of Tier C, and TestIssue6916_TierCSurvivingSiblings
// pins those by name:
//
//	actix_web.yaml  keeps Config "HttpServer::new(" and Config "App::new()"
//	                (Tier D still owes them a real name) and Middleware from .wrap()
//	kmp.yaml        keeps Module <name>Main from `val commonMain by getting`
//	sinatra.yaml    keeps Route, Controller, Config from `configure :<env> do`,
//	                Middleware from `error <code> do`, Service from `helpers <Module>`
//	langchain.yaml  keeps Operation from `.pipe(`, Operation from `new DynamicTool(`,
//	                Schema from the prompt-template factories, and the Tier D
//	                Service from `create\w+Agent(`
//
// Held constant everywhere: the surviving patterns' spellings, `name_group` on
// every rule that was not deleted, and the four rule files' `frameworks:`
// detection blocks.

// tierC6916Site is one deleted rule site.
type tierC6916Site struct {
	rule string // rule file:line as it stood before the deletion
	lang string
	path string

	// src is an input that minted `marker` before the deletion.
	src string

	// marker is the "<Kind>:<Name>" the deleted `name_group: 0` rule produced.
	// It must never come back.
	marker string

	// control is a "<Kind>:<Name>" the SAME src still mints from a rule that
	// was NOT deleted. It is what makes the absence above evidence.
	control string
}

const tierC6916ActixMain = `use actix_web::{web, App, HttpServer, middleware::Logger};

async fn health() -> &'static str { "ok" }

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    HttpServer::new(|| App::new().wrap(Logger::default()).route("/health", web::get().to(health)))
        .bind(("127.0.0.1", 8080))?
        .run()
        .await
}
`

const tierC6916KmpSourceSets = `plugins {
    kotlin("multiplatform")
}

kotlin {
    sourceSets {
        val commonMain by getting {
            dependencies {
                implementation("io.ktor:ktor-client-core:2.3.0")
            }
        }
    }
}
`

// The sinatra fixtures mirror internal/quality/golden/ruby-sinatra-modular-mini:
// every construct is INDENTED inside the class body and none is the first line
// of the file, which is the shape #6917 restored.
const tierC6916SinatraHelpersBlock = `require 'sinatra/base'

class MyApp < Sinatra::Base
  configure :production do
    set :show_exceptions, false
  end

  helpers AuthHelpers

  helpers do
    def json_helper(payload)
      payload.to_json
    end
  end
end
`

const tierC6916SinatraBeforeWithPath = `require 'sinatra/base'

class MyApp < Sinatra::Base
  before '/admin/*' do
    halt 401 unless authenticate!
  end

  error 404 do
    'not found'
  end
end
`

// The BARE spellings are the shape the measurement arm flagged as the Tier C
// trap: `before`/`after` already carry an OPTIONAL group 1 (the path), and
// extractGroupFromIndex returns "" for an unmatched optional group, after which
// the detector skips the match. So `name_group: 1` would have silently stopped
// extracting these two files while leaving the path-carrying spelling above
// alive — a recall change wearing a rename's clothes. Both spellings are graded
// here so the deletion is pinned on the shape a rename would NOT have changed
// and on the shape it would.
const tierC6916SinatraBeforeBare = `require 'sinatra/base'

class MyApp < Sinatra::Base
  before do
    @started = Time.now
  end

  error 404 do
    'not found'
  end
end
`

const tierC6916SinatraAfterWithPath = `require 'sinatra/base'

class MyApp < Sinatra::Base
  after '/admin/*' do
    response.headers['X-Admin'] = '1'
  end

  error 500 do
    'boom'
  end
end
`

const tierC6916SinatraAfterBare = `require 'sinatra/base'

class MyApp < Sinatra::Base
  after do
    response.headers['X-App'] = 'MyApp'
  end

  error 500 do
    'boom'
  end
end
`

const tierC6916SinatraClassicBoot = `require 'sinatra'

configure :production do
  set :show_exceptions, false
end

get '/invoices' do
  'ok'
end

Sinatra::Application.run!
`

const tierC6916LangchainSequence = `import { RunnableSequence } from "@langchain/core/runnables";

const chain = RunnableSequence.from([prompt, model]);
const piped = prompt.pipe(model);
`

const tierC6916LangchainToolFactory = `import { tool } from "@langchain/core/tools";

const lookup = tool(async (input) => input, { name: "lookup" });
const declared = new DynamicTool({ name: "declared", func: async () => "x" });
`

const tierC6916LangchainBindTools = `import { ChatOpenAI } from "@langchain/openai";

const model = new ChatOpenAI({});
const bound = model.bindTools([lookup]);
const declared = new DynamicTool({ name: "declared", func: async () => "x" });
`

var tierC6916Sites = []tierC6916Site{
	{
		rule: "rust/frameworks/actix_web.yaml:90", lang: "rust", path: "src/main.rs",
		src:    tierC6916ActixMain,
		marker: `Config:#[actix_web::main]`,
		// The .wrap(Logger) Middleware, NOT one of the two surviving Config
		// markers: using a Config as the control would make "the deleted Config
		// is gone" and "Config extraction still works" the same assertion.
		control: "Middleware:Logger",
	},
	{
		rule: "kotlin/frameworks/kmp.yaml:83", lang: "kotlin", path: "build.gradle.kts",
		src:    tierC6916KmpSourceSets,
		marker: "Config:sourceSets {",
		// The named form of the very information the deleted marker gestured at.
		control: "Module:commonMain",
	},
	{
		rule: "ruby/frameworks/sinatra.yaml:73", lang: "ruby", path: "app.rb",
		src:    tierC6916SinatraHelpersBlock,
		marker: "Service:helpers do",
		// The `helpers <Module>` include form — a DIFFERENT rule in the same
		// file, kept, and the one that carries a real bindable name.
		control: "Service:AuthHelpers",
	},
	{
		rule: "ruby/frameworks/sinatra.yaml:93", lang: "ruby", path: "app.rb",
		src:     tierC6916SinatraBeforeWithPath,
		marker:  "Middleware:before '/admin/*' do",
		control: "Middleware:404",
	},
	{
		rule: "ruby/frameworks/sinatra.yaml:93 (bare)", lang: "ruby", path: "app.rb",
		src:     tierC6916SinatraBeforeBare,
		marker:  "Middleware:before do",
		control: "Middleware:404",
	},
	{
		rule: "ruby/frameworks/sinatra.yaml:99", lang: "ruby", path: "app.rb",
		src:     tierC6916SinatraAfterWithPath,
		marker:  "Middleware:after '/admin/*' do",
		control: "Middleware:500",
	},
	{
		rule: "ruby/frameworks/sinatra.yaml:99 (bare)", lang: "ruby", path: "app.rb",
		src:     tierC6916SinatraAfterBare,
		marker:  "Middleware:after do",
		control: "Middleware:500",
	},
	{
		rule: "ruby/frameworks/sinatra.yaml:117", lang: "ruby", path: "boot.rb",
		src:    tierC6916SinatraClassicBoot,
		marker: "Config:Sinatra::Application.run!",
		// `configure :production do` — the file's OTHER Config rule, kept, and
		// the reason "no Config at all" is not the assertion here.
		control: "Config:production",
	},
	{
		rule: "javascript_typescript/frameworks/langchain.yaml:42", lang: "typescript", path: "src/chain.ts",
		src:    tierC6916LangchainSequence,
		marker: "Operation:RunnableSequence.from(",
		// `.pipe()` names the upstream runnable — the bindable form of the same
		// LCEL composition fact.
		control: "Operation:prompt",
	},
	{
		rule: "javascript_typescript/frameworks/langchain.yaml:66", lang: "typescript", path: "src/tools.ts",
		src:     tierC6916LangchainToolFactory,
		marker:  "Operation:tool(async (",
		control: "Operation:DynamicTool",
	},
	{
		rule: "javascript_typescript/frameworks/langchain.yaml:71", lang: "typescript", path: "src/model.ts",
		src:     tierC6916LangchainBindTools,
		marker:  "Operation:.bindTools(",
		control: "Operation:DynamicTool",
	},
}

// TestIssue6916_TierCMarkerIsNoLongerMinted is the change itself, in the only
// direction a deletion can be graded: the marker entity is absent from an input
// that produced it before, and the same input still produces something.
func TestIssue6916_TierCMarkerIsNoLongerMinted(t *testing.T) {
	for _, s := range tierC6916Sites {
		t.Run(s.rule, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, s.path, s.lang, s.src))

			if slices.Contains(ids, s.marker) {
				t.Errorf("%s: the deleted #6916 Tier C rule is back — %q is in the graph again. "+
					"It is a per-file boolean marker named after the whole regex match, with no "+
					"referent for any edge to point at. Entities:\n  %s",
					s.rule, s.marker, strings.Join(ids, "\n  "))
			}
			if !slices.Contains(ids, s.control) {
				t.Errorf("%s: positive control missing — this input no longer mints %q, so the "+
					"absence of %q above proves nothing (an unloadable rule file would pass it). "+
					"Entities:\n  %s",
					s.rule, s.control, s.marker, strings.Join(ids, "\n  "))
			}
		})
	}
}

// TestIssue6916_TierCSurvivingSiblings pins the NEIGHBOUR axis. Three of the
// four touched rule files keep patterns of exactly the same shape as the deleted
// ones — actix_web keeps two other `name_group: 0` Config markers on purpose,
// because they are Tier D (they name a thing that could be captured) rather than
// Tier C. A deletion that took a sibling with it would be invisible to the test
// above, whose only positive control per site is one entity.
func TestIssue6916_TierCSurvivingSiblings(t *testing.T) {
	for _, tc := range []struct {
		name, path, lang, src string
		want                  []string
	}{
		{
			// The two Config markers actix_web KEEPS. They are still
			// `name_group: 0` and still ugly; Tier C deleted only the third,
			// the attribute macro, which has no name to capture at all.
			name: "actix_web keeps HttpServer::new( and App::new(", path: "src/main.rs", lang: "rust",
			src:  tierC6916ActixMain,
			want: []string{"Config:HttpServer::new(", "Config:App::new()", "Middleware:Logger", "Route:/health"},
		},
		{
			name: "kmp keeps the sourceSets MEMBERS", path: "build.gradle.kts", lang: "kotlin",
			src:  tierC6916KmpSourceSets,
			want: []string{"Module:commonMain"},
		},
		{
			name: "sinatra keeps routes, the class, configure, error and helpers <Module>",
			path: "app.rb", lang: "ruby",
			src: `require 'sinatra/base'

class MyApp < Sinatra::Base
  configure :production do
    set :show_exceptions, false
  end

  helpers AuthHelpers

  get '/invoices' do
    'ok'
  end

  error 404 do
    'not found'
  end
end
`,
			want: []string{"Route:/invoices", "Controller:MyApp", "Config:production",
				"Middleware:404", "Service:AuthHelpers"},
		},
		{
			name: "langchain keeps pipe, DynamicTool, the prompt templates and the agent factory",
			path: "src/chain.ts", lang: "typescript",
			src: `import { ChatPromptTemplate } from "@langchain/core/prompts";
import { createReactAgent } from "langchain/agents";

const prompt2 = ChatPromptTemplate.fromMessages([]);
const piped = prompt.pipe(model);
const declared = new DynamicTool({ name: "declared", func: async () => "x" });
const agent = createReactAgent({ llm: model, tools: [declared] });
`,
			want: []string{"Operation:prompt", "Operation:DynamicTool",
				"Schema:ChatPromptTemplate", "Service:createReactAgent("},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := entityIDs6916(detect6916(t, tc.path, tc.lang, tc.src))
			for _, w := range tc.want {
				if !slices.Contains(ids, w) {
					t.Errorf("a pattern NOT part of Tier C stopped firing: %q is missing. "+
						"Entities:\n  %s", w, strings.Join(ids, "\n  "))
				}
			}
		})
	}
}

// TestIssue6916_TierCLangchainMarkersWereNotSuppressedDownstream grades the
// claim this tier's langchain entry rested on — that the three JS `Operation`
// markers were "already dropped downstream as Operation noise, so the rules are
// dead weight". They were NOT.
//
// dropStatementNoiseOperations (precision_dedup.go) drops an `Operation` whose
// Name starts with `@`, contains a bare `=`, or opens with a statement keyword.
// A trailing-paren call fragment has none of those, so all three names sailed
// through the pass and reached the graph — which makes the deletion above a
// deliberate REMOVAL of entities that existed, not the tidying of an inert rule.
// The python `@tool` site (python/frameworks/langchain.yaml, untouched here) is
// the one the pass's own doc comment names, and it IS dropped: case (1).
//
// If the noise predicate is ever widened to cover call fragments, this test
// fails and should be revisited rather than deleted — the point it pins is that
// "already suppressed" cannot be assumed from the doc comment.
func TestIssue6916_TierCLangchainMarkersWereNotSuppressedDownstream(t *testing.T) {
	for _, name := range []string{
		"RunnableSequence.from(",
		"tool(async (",
		".bindTools(",
	} {
		if isStatementNoiseOperationName(name) {
			t.Errorf("%q is dropped by dropStatementNoiseOperations after all — the Tier C "+
				"justification recorded in the rule comments (that the deletion removes "+
				"entities that DID reach the graph) needs re-reading.", name)
		}
	}
	// The control: the shape the pass was written for, still dropped.
	if !isStatementNoiseOperationName("@tool") {
		t.Error(`positive control: "@tool" is no longer treated as statement noise, so the ` +
			`comparison above says nothing about the predicate`)
	}
}
