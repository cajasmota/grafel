# `<repo-slug>` local development

> Step-by-step. Assume the reader has just cloned the repo. Every command in a fenced block with a language tag.

## Prerequisites

- <runtime / version>
- <package manager / version>
- <system deps>

## First-time setup

```bash
# 1. install deps
<command>

# 2. copy env template
cp .env.example .env

# 3. one-time bootstrap
<command>
```

## Run the app

```bash
<command>
```

> What you should see when it works: <one line>.

## Run the tests

```bash
<command>
```

## Common dev tasks

### <task>

```bash
<command>
```

> One line on what this does and when you'd use it.

## Troubleshooting

- **Symptom**: `<error>`. **Cause**: <reason>. **Fix**: <action>.

## Connecting to other repos in the group

> If the repo needs another repo running (e.g., a backend the frontend talks to), document the cross-repo handshake here. Link to the other repo's `how-to/local-dev.md` rather than duplicating its instructions.
