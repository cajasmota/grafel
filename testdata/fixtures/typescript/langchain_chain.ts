// Proving fixture for LangChain.js prompt_template_extraction,
// chain_composition, and tool_use_detection (#2865). The YAML rule
// internal/engine/rules/javascript_typescript/frameworks/langchain.yaml mines:
//   - prompts: ChatPromptTemplate.fromMessages / PromptTemplate.fromTemplate
//   - chains:  prompt.pipe(model).pipe(parser) + RunnableSequence.from([...])
//   - tools:   new DynamicTool(...) / new DynamicStructuredTool(...) /
//              tool(async () => …) / model.bindTools([...])
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

// chain_composition: LCEL pipe + explicit RunnableSequence.
const chain = prompt.pipe(model).pipe(parser);
const sequence = RunnableSequence.from([summaryPrompt, model, parser]);

// tool_use_detection: three tool declaration idioms + bindTools.
const search = new DynamicTool({
  name: 'search',
  description: 'search the web',
  func: async (q: string) => fetchResults(q),
});

const calculator = new DynamicStructuredTool({
  name: 'calculator',
  schema: calcSchema,
  func: async ({ a, b }) => String(a + b),
});

const weather = tool(async ({ city }) => getWeather(city), {
  name: 'weather',
});

const boundModel = model.bindTools([search, calculator, weather]);
