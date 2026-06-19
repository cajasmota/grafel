# Security Policy

## Supported versions

grafel is in active preview (v0.x). Security fixes are applied to the latest
released version only. Please upgrade to the most recent release before
reporting an issue.

| Version | Supported          |
|---------|--------------------|
| latest  | :white_check_mark: |
| older   | :x:                |

## Reporting a vulnerability

Please **do not** open a public issue for security vulnerabilities.

Report privately through GitHub's
[private vulnerability reporting](https://github.com/cajasmota/grafel/security/advisories/new):

1. Go to the repository's **Security** tab.
2. Click **Report a vulnerability**.
3. Provide a clear description, affected version, reproduction steps, and
   impact assessment.

If you cannot use GitHub's private reporting, you may instead reach the
maintainers through the contact listed on the repository profile.

## What to include

- A description of the vulnerability and its potential impact.
- The grafel version (`grafel --version`) and platform.
- Step-by-step reproduction instructions or a proof of concept.
- Any suggested remediation, if you have one.

## Response expectations

- **Acknowledgement:** within 5 business days.
- **Triage and severity assessment:** within 10 business days.
- **Fix and disclosure:** coordinated with you; we aim to ship a fix and
  publish an advisory as soon as a remediation is validated.

We will keep you informed of progress and credit you in the advisory unless you
prefer to remain anonymous.

## Scope notes

grafel runs entirely on your local machine: it indexes code into an on-disk
graph and serves it over a loopback-only daemon and an MCP server on stdio. No
data is sent to any remote service. Reports about local privilege escalation,
arbitrary code execution during indexing, path traversal, or unsafe handling of
untrusted repository contents are especially valuable.
