package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// langchainFixture mirrors testdata/fixtures/typescript/langchain_chain.ts —
// the repo-root proving fixture cited in the coverage registry. It exercises
// the prompt_template_extraction, chain_composition, and tool_use_detection
// patterns added to langchain.yaml in #2865. Kept inline so the test loads the
// REAL embedded rule (LoadAllRules) rather than a synthetic test rule, proving
// the shipped YAML — not a stand-in — performs the extraction.
const langchainFixture = `
import { ChatPromptTemplate, PromptTemplate } from '@langchain/core/prompts';
import { RunnableSequence } from '@langchain/core/runnables';
import { DynamicTool, DynamicStructuredTool, tool } from '@langchain/core/tools';
import { ChatOpenAI } from '@langchain/openai';

const prompt = ChatPromptTemplate.fromMessages([
  ['system', 'You are a helpful assistant.'],
  ['human', '{question}'],
]);
const summaryPrompt = PromptTemplate.fromTemplate('Summarize: {text}');
const model = new ChatOpenAI({ model: 'gpt-4o' });

const chain = prompt.pipe(model).pipe(parser);
const sequence = RunnableSequence.from([summaryPrompt, model, parser]);

const search = new DynamicTool({ name: 'search', func: async (q) => fetchResults(q) });
const calculator = new DynamicStructuredTool({ name: 'calculator', func: async () => '42' });
const weather = tool(async ({ city }) => getWeather(city), { name: 'weather' });
const boundModel = model.bindTools([search, calculator, weather]);
`

// TestDetect_LangChain_PromptsChainsTools proves #2865: the shipped
// langchain.yaml rule extracts prompt templates (Schema), chain composition
// (Operation: .pipe / RunnableSequence.from), and tool declarations
// (Operation: DynamicTool / DynamicStructuredTool / tool() / bindTools).
func TestDetect_LangChain_PromptsChainsTools(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "src/agent.ts",
		Content:  []byte(langchainFixture),
		Language: "javascript_typescript",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	names := map[string]bool{}
	kindCount := map[string]int{}
	for _, e := range result.Entities {
		names[e.Name] = true
		kindCount[e.Kind]++
	}

	// prompt_template_extraction: ChatPromptTemplate + PromptTemplate prompts
	// surface as Schema entities named for the template class.
	if !names["ChatPromptTemplate"] {
		t.Errorf("expected ChatPromptTemplate prompt entity; got names %v", keys(names))
	}
	if !names["PromptTemplate"] {
		t.Errorf("expected PromptTemplate prompt entity; got names %v", keys(names))
	}
	if c := countByKind(result.Entities, "Schema"); c < 2 {
		t.Errorf("expected >=2 Schema (prompt) entities, got %d", c)
	}

	// chain_composition: .pipe() stages (captures upstream runnable name) +
	// RunnableSequence.from.
	if !names["prompt"] {
		t.Errorf("expected .pipe() chain stage named 'prompt'; got %v", keys(names))
	}
	if !names["RunnableSequence.from("] {
		t.Errorf("expected RunnableSequence.from composition entity; got %v", keys(names))
	}

	// tool_use_detection: the three tool declaration idioms + bindTools.
	if !names["DynamicTool"] {
		t.Errorf("expected DynamicTool entity; got %v", keys(names))
	}
	if !names["DynamicStructuredTool"] {
		t.Errorf("expected DynamicStructuredTool entity; got %v", keys(names))
	}
	if !names[".bindTools("] {
		t.Errorf("expected bindTools entity; got %v", keys(names))
	}
	// tool(async (...) factory.
	var sawToolFactory bool
	for n := range names {
		if n == "tool(async (" || n == "tool((" {
			sawToolFactory = true
		}
	}
	if !sawToolFactory {
		t.Errorf("expected tool() factory entity; got %v", keys(names))
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
