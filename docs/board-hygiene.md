# Board hygiene

Every PR must declare its relationship to issues. Pick ONE convention:

## Single-issue PRs

If the PR fully resolves an issue, use:

```
Closes #<N>
```

GitHub auto-closes the issue when the PR merges. Aliases: `Fixes #N`, `Resolves #N`.

## Multi-wave features

For features with multiple sub-PRs (e.g., HTTP FETCHES across multiple languages, landed as separate waves):

- First through second-to-last waves: `Refs #N (wave X of N)`
- Final wave: `Closes #N`

The epic stays open until the last wave merges.

Example: HTTP FETCHES epic (#721) was solved with multiple language implementations. Early PRs used `Refs #721 (wave 1 of 3)`, and the final PR used `Closes #721`.

## Epic + sub-stories

For epics with multiple independent sub-stories, create a sub-issue per sub-story and close those individually. The parent epic stays open until ALL sub-stories close.

Example: An epic like "Graph visualization" might have sub-issues "UI mockups" (#793), "Frontend component" (#804), "Backend API" (#814). Each sub-issue's PR uses `Closes #<sub-issue>`, and the parent epic only closes when all sub-issues are complete.

```
Closes #793
```

This closes only the sub-issue; the parent epic remains open.

## Exempt PRs

For docs-only, formatting, or one-off cleanups that don't map to an issue, add the `board:exempt` label. CI skips the keyword check.

Apply the label when:
- You're fixing a typo in docs with no related issue
- You're reformatting code for consistency without a spec
- You're updating a README or workflow that isn't tied to a feature issue

The label is managed via GitHub's UI: when you open a PR, add it to the PR's labels section.

## Why this convention exists

Board hygiene matters because:

1. **Traceability**: Code changes should link to the issues they resolve. Without this, debugging and understanding the history of a change becomes harder.

2. **Closure automation**: When you use `Closes #N`, GitHub automatically closes the issue on merge. Without the keyword, issues stay open indefinitely even though they're fixed.

3. **Board hygiene**: A board cleanup sweep on 2026-05-20 found 18 merged PRs with no closure keyword, leaving their issues orphaned. Without process enforcement, this pattern recurs.

## CI enforcement

The `board-hygiene` workflow runs on every PR and checks that:

- PR body contains one of: `Closes #N`, `Fixes #N`, `Resolves #N`, `Refs #N`, or `Closes-epic-on-merge-of-PR-#X #N`
- OR the PR has the `board:exempt` label

If neither condition is met, the workflow fails with a helpful message. Fix it by adding a closure keyword to the PR description, or add the `board:exempt` label if the PR legitimately doesn't map to an issue.

## Agents

When agents land PRs, they must:

1. Include a closure keyword in the PR description
2. OR add the `board:exempt` label
3. Do not silence or ignore CI failures from the `board-hygiene` workflow

This ensures the board stays clean and issues are properly closed.
