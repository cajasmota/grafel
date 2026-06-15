# Phase 4 - Quality judgment

Judge both runs against an **independently established ground truth**. Your output is `judgment.json`. You must establish ground truth **before** opening either run's answers, to avoid anchoring on whichever side sounded more confident.

## Protocol

For each question in `questions.json`:

### Step A - Establish ground truth (independent)

1. Read the question and its `anchors` + `expected_signals`.
2. Use `rg` / `Read` / `Bash` only (no grafel MCP - the judge uses grep so it does not favor MCP). Read the actual source code.
3. Write the canonical answer: definition locations, callers, kinds, fields, line numbers - whatever the `expected_signals` call for.
4. Commit the ground truth to memory (write it into your scratch) **before** you open `with-mcp.json` or `without-mcp.json`.

### Step B - Score with-MCP

5. Open `with-mcp.json`, read the `answer` for this question.
6. Score against ground truth:
   - **full** - mentions every expected fact, no fabrications.
   - **partial** - mentions some expected facts, no fabrications.
   - **wrong** - contradicts ground truth, OR fabricates entities / paths / lines that do not exist.
   - **unknown** - the agent said "I don't know" or set `unknown: true`.
7. Record `misses` (expected facts not mentioned) and `extras` (fabrications or off-topic claims).

### Step C - Score without-MCP

8. Open `without-mcp.json`, read the `answer` for the same question.
9. Score with the same rubric.
10. Record `misses` and `extras` for this side too.

### Honesty rule

If MCP confidently returned a wrong answer, mark it `wrong`. Do not soften scoring for either side. The whole point of the skill is honest measurement.

## Output schema (`judgment.json`)

```json
{
  "version": 1,
  "judged_at": "<RFC3339>",
  "judgments": [
    {
      "id": "q01",
      "ground_truth_summary": "OrderService is a Go struct in shop-backend/internal/order/service.go:18, with 4 methods (Create, Get, Update, Cancel). Called from 12 sites across 3 packages.",
      "ground_truth_anchors": [
        {"path": "shop-backend/internal/order/service.go", "line": 18, "kind": "struct"}
      ],
      "with_mcp": {
        "score": "full",
        "misses": [],
        "extras": [],
        "rationale": "Mentioned definition, all 4 methods, and caller count. Matches ground truth."
      },
      "without_mcp": {
        "score": "partial",
        "misses": ["caller count (rg returned too many false positives to count cleanly)"],
        "extras": [],
        "rationale": "Found definition and methods but stopped on caller enumeration."
      }
    }
  ],
  "aggregate": {
    "with_mcp": {"full": 8, "partial": 4, "wrong": 1, "unknown": 1},
    "without_mcp": {"full": 5, "partial": 6, "wrong": 1, "unknown": 2}
  }
}
```

## Privacy

- Ground-truth content in `ground_truth_summary` should be a description of the structural facts (kinds, paths, line numbers, counts) and **not** include source-code snippets.
- `ground_truth_anchors` carries `path`, `line`, `kind` - no source content.

## Output

Write `judgment.json` and print the aggregate scoreboard. Return to orchestrator.
