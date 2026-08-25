#!/usr/bin/env bash
# Upsert the SINGLE recurring tracking issue for a scheduled workflow.
#
# Shared by .github/workflows/grammar-freshness.yml and
# .github/workflows/language-release-calendar.yml. Both used to carry their own
# copy of this logic, and both copies searched only `--state open` — so once a
# human closed the issue, the next cron run opened a brand-new one with no link
# back. #5674 and #6066 are the same recurring report, filed twice, silently
# forked. That silent fork is the failure mode this script exists to prevent,
# and every rule below is written so that no other door leads to the same place.
#
# Identity
#   The LABEL is the primary key: it is an exact, server-side filter, and one
#   `gh issue list --label` call returns the candidates' bodies too. The MARKER
#   (`<!-- grafel-tracker:... -->`) is then verified LOCALLY, and only when it
#   occupies a line of its own — merely quoting the marker inside prose, a code
#   fence or a review comment does not make an issue ours. The title is not a
#   key at all, so a retitle cannot fork the thread.
#
# Precedence among candidates (documented because it is load-bearing):
#   1. open + marker   2. open, no marker   3. closed + marker   4. closed
#   State dominates the marker, so "the newest OPEN candidate wins" holds
#   unconditionally and two open issues for one report are unreachable.
#   Ties on createdAt are broken by issue number, so the choice is deterministic.
#
# Action
#   * open match   -> update it in place.
#   * closed match -> do NOT reopen. A human closing the issue is a decision
#     ("this instance is handled"), and silently reopening overrides it. File a
#     successor instead and post the "Continues from #N" link as a COMMENT, not
#     in the body: the body is rewritten every month, comments are not, so a
#     chain of three or more stays reconstructible.
#   * no match     -> create the first issue.
#
# Failure handling
#   A lookup that ERRORS must never be read as "no match" — that would create a
#   duplicate on a rate limit or a 502, i.e. fork the thread through a different
#   door. Every gh call's exit status is checked and any failure, or any
#   malformed field, aborts without writing anything. A missed month is free;
#   a duplicate is not.
#
# Required environment:
#   ISSUE_TITLE   title used when creating a new issue
#   ISSUE_LABEL   stable label, applied on create and re-applied on update
#   ISSUE_MARKER  stable identity token, written as its own HTML-comment line
#   ISSUE_KIND    human word for log lines, e.g. "tracking" or "reminder"
#   BODY_FILE     path to the rendered markdown body
set -euo pipefail

: "${ISSUE_TITLE:?ISSUE_TITLE is required}"
: "${ISSUE_LABEL:?ISSUE_LABEL is required}"
: "${ISSUE_MARKER:?ISSUE_MARKER is required}"
: "${ISSUE_KIND:?ISSUE_KIND is required}"
: "${BODY_FILE:?BODY_FILE is required}"

[ -f "$BODY_FILE" ] || { echo "BODY_FILE '$BODY_FILE' does not exist" >&2; exit 1; }

MARKER_LINE="<!-- $ISSUE_MARKER -->"

abort() { echo "ERROR: $* — aborting without creating or editing anything." >&2; exit 1; }

# jq program over the raw `gh issue list --json` array. It annotates each
# candidate with whether the marker stands on a line of its own, applies the
# documented precedence, and emits "<number> <state>" (or nothing at all).
# Sorting newest-first by (createdAt, number) makes the pick deterministic even
# when two issues share a timestamp.
# shellcheck disable=SC2016  # $m is a jq variable, bound below with --arg.
SELECT_JQ='
def trim: sub("[[:space:]]+$";"");   # trailing only: our marker is written at column 0
def marked: ((.body // "") | split("\n") | map(trim) | index($m) != null);
def isopen: ((.state // "") | ascii_downcase) == "open";
map(. + {_m: marked, _o: isopen})
| sort_by(.createdAt, .number) | reverse
| ( map(select(._o and ._m))
  + map(select(._o and (._m | not)))
  + map(select((._o | not) and ._m))
  + map(select((._o | not) and (._m | not))) )
| .[0] // empty
| "\(.number) \(.state)"
'

# Keep only entries that genuinely carry the marker line AND were opened by the
# bot this workflow runs as. Used on the search fallback, where GitHub decides
# what matched and we do not trust it: the marker string appears in these YAML
# files and in the PR that introduced them, so a contributor quoting it in their
# own issue is a natural thing to happen, and without this guard we would
# overwrite their question with the grammar table. On the primary path the
# repo-controlled label already supplies that guarantee.
# shellcheck disable=SC2016  # $m is a jq variable, bound below with --arg.
FILTER_MARKED_JQ='
def trim: sub("[[:space:]]+$";"");   # trailing only: our marker is written at column 0
map(select(
      ((.body // "") | split("\n") | map(trim) | index($m) != null)
      and (.author.is_bot == true)))
'

# Primary lookup: one API call, label-filtered, all states, bodies included.
if ! json=$(gh issue list --state all --label "$ISSUE_LABEL" --limit 100 \
              --json number,state,createdAt,body); then
  abort "gh issue list --label '$ISSUE_LABEL' failed"
fi
if ! match=$(printf '%s' "$json" | jq -r --arg m "$MARKER_LINE" "$SELECT_JQ"); then
  abort "could not parse the issue list returned by gh"
fi

# Fallback for the "label was removed but the marker survived" case. This one
# leans on GitHub's full-text index; if that index does not reach inside HTML
# comments the branch is simply inert and identity degrades to label-only,
# which is still correct. Results are re-verified locally regardless, so a
# loosely tokenised search match can never select a foreign issue.
if [ -z "$match" ]; then
  if ! json=$(gh issue list --state all --search "in:body \"$ISSUE_MARKER\"" --limit 100 \
                --json number,state,createdAt,body,author); then
    abort "gh issue list --search failed"
  fi
  if ! match=$(printf '%s' "$json" | jq -r --arg m "$MARKER_LINE" "$FILTER_MARKED_JQ | $SELECT_JQ"); then
    abort "could not parse the search result returned by gh"
  fi
fi

number=""
state=""
if [ -n "$match" ]; then
  number=${match%% *}
  state=$(printf '%s' "${match##* }" | tr '[:upper:]' '[:lower:]')
  # Validate before branching. Garbage here would otherwise file a public issue
  # reading "Continues from #not, which was closed."
  case "$number" in ''|*[!0-9]*) abort "lookup returned a non-numeric issue number '$number'";; esac
  case "$state" in open|closed) ;; *) abort "lookup returned an unrecognised state '$state'";; esac
fi

body=$(mktemp)
trap 'rm -f "$body"' EXIT
cat "$BODY_FILE" > "$body"
printf '\n%s\n' "$MARKER_LINE" >> "$body"

create_issue() { # echoes the new issue number
  local url new
  if ! url=$(gh issue create --title "$ISSUE_TITLE" --label "$ISSUE_LABEL" --body-file "$body"); then
    abort "gh issue create failed"
  fi
  new=$(printf '%s' "$url" | tr -d '[:space:]')
  new=${new##*/}
  case "$new" in ''|*[!0-9]*) abort "gh issue create returned an unparseable URL '$url'";; esac
  printf '%s' "$new"
}

if [ -z "$number" ]; then
  echo "No previous $ISSUE_KIND issue found; creating the first one."
  create_issue > /dev/null
  exit 0
fi

if [ "$state" = "open" ]; then
  echo "Updating existing open $ISSUE_KIND issue #$number"
  gh issue edit "$number" --body-file "$body" || abort "gh issue edit #$number failed"
  # Re-apply the label in case the issue was matched by the marker alone.
  gh issue edit "$number" --add-label "$ISSUE_LABEL" || true
  exit 0
fi

echo "Previous $ISSUE_KIND issue #$number is closed; creating a successor that links it."
new=$(create_issue)
# The link lives in a COMMENT so that next month's body rewrite cannot erase it.
link_note="Continues from #$number, which was closed. This is the same recurring report, re-filed by the scheduled workflow rather than reopened — closing it is treated as \"this instance is handled\", not as \"stop reporting\"."
if ! gh issue comment "$new" --body "$link_note"; then
  abort "created #$new but could not post the 'Continues from #$number' link comment; link them by hand"
fi
echo "Created #$new, linked to #$number."
