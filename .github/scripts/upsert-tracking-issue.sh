#!/usr/bin/env bash
# Upsert the SINGLE recurring tracking issue for a scheduled workflow.
#
# Shared by .github/workflows/grammar-freshness.yml and
# .github/workflows/language-release-calendar.yml. Both used to carry their own
# copy of this logic, and both copies searched only `--state open` — so once a
# human closed the issue, the next cron run opened a brand-new one with no link
# back. #5674 and #6066 are the same recurring report, filed twice, silently
# forked. That silent fork is the failure mode this script exists to prevent.
#
# Policy (see the PR for #6636 for the reasoning):
#   * Identity is keyed on a stable MARKER embedded in the issue body, with the
#     label as an equally stable secondary key. Neither a retitle nor a label
#     edit alone can fork the thread. The title is no longer consulted.
#   * The search covers ALL states, not just open ones.
#   * If the newest match is OPEN  -> update it in place (unchanged behaviour).
#   * If the newest match is CLOSED -> do NOT reopen it. A human closing the
#     issue is a decision ("this instance is handled"), and silently reopening
#     it overrides that decision. Instead open a fresh issue whose body links
#     the previous one, so the chain stays traceable. GitHub renders the
#     back-reference on the closed issue automatically.
#   * If there is no match at all -> create the first issue.
#
# Required environment:
#   ISSUE_TITLE   title used when creating a new issue
#   ISSUE_LABEL   stable label, applied on create and re-applied on update
#   ISSUE_MARKER  stable identity token appended to every body
#   ISSUE_KIND    human word for log lines, e.g. "tracking" or "reminder"
#   BODY_FILE     path to the rendered markdown body
#
# `gh` must be authenticated. Nothing here writes to a closed issue.
set -euo pipefail

: "${ISSUE_TITLE:?ISSUE_TITLE is required}"
: "${ISSUE_LABEL:?ISSUE_LABEL is required}"
: "${ISSUE_MARKER:?ISSUE_MARKER is required}"
: "${ISSUE_KIND:?ISSUE_KIND is required}"
: "${BODY_FILE:?BODY_FILE is required}"

[ -f "$BODY_FILE" ] || { echo "BODY_FILE '$BODY_FILE' does not exist" >&2; exit 1; }

# Newest-first selector shared by every lookup below, so "the newest match"
# means the same thing regardless of which key found it.
newest_jq='sort_by(.createdAt) | reverse | .[0] | select(. != null) | "\(.number) \(.state)"'

lookup() {
  # $1 : issue state to search ("open" or "all"); rest: extra `gh issue list` args
  local state="$1"; shift
  gh issue list --state "$state" --limit 100 --json number,state,createdAt \
     --jq "$newest_jq" "$@" 2>/dev/null || true
}

# Identity keys, in priority order:
#   1) the marker in the body — survives a retitle AND a label edit;
#   2) the label            — survives a retitle and a body rewrite.
# The title is deliberately not a key: retitling must not fork the thread.
find_in() {
  local state="$1" m
  m=$(lookup "$state" --search "in:body \"$ISSUE_MARKER\"")
  [ -n "$m" ] || m=$(lookup "$state" --label "$ISSUE_LABEL")
  printf '%s' "$m"
}

# Prefer the newest OPEN match. Only when no open match exists at all do we
# consider closed ones — so we can never end up with two open issues for the
# same report, which is the fork this script exists to prevent.
match=$(find_in open)
if [ -z "$match" ]; then
  match=$(find_in all)
fi

number=${match%% *}
state=${match##* }
# Normalise: gh reports OPEN/CLOSED, the REST API reports open/closed.
state=$(printf '%s' "$state" | tr '[:upper:]' '[:lower:]')

# Every body we write carries the marker, so the next run can find this issue
# again even if somebody retitles it or strips the label.
body=$(mktemp)
trap 'rm -f "$body"' EXIT

if [ -z "$number" ]; then
  cat "$BODY_FILE" > "$body"
  printf '\n<!-- %s -->\n' "$ISSUE_MARKER" >> "$body"
  echo "No previous $ISSUE_KIND issue found; creating the first one."
  gh issue create --title "$ISSUE_TITLE" --label "$ISSUE_LABEL" --body-file "$body"
  exit 0
fi

if [ "$state" = "open" ]; then
  cat "$BODY_FILE" > "$body"
  printf '\n<!-- %s -->\n' "$ISSUE_MARKER" >> "$body"
  echo "Updating existing open $ISSUE_KIND issue #$number"
  gh issue edit "$number" --body-file "$body"
  # Re-apply the label in case the issue was matched by marker alone.
  gh issue edit "$number" --add-label "$ISSUE_LABEL" || true
  exit 0
fi

# Closed. Respect the close, but leave a trail instead of forking silently.
cat "$BODY_FILE" > "$body"
{
  printf '\n---\n\n'
  printf 'Continues from #%s, which was closed. This is the same recurring report, re-filed by the scheduled workflow rather than reopened — closing it is treated as "this instance is handled", not as "stop reporting".\n' "$number"
  printf '\n<!-- %s -->\n' "$ISSUE_MARKER"
} >> "$body"
echo "Previous $ISSUE_KIND issue #$number is closed; creating a successor that links it."
gh issue create --title "$ISSUE_TITLE" --label "$ISSUE_LABEL" --body-file "$body"
