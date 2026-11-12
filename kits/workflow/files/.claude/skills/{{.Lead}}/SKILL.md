{{if many .Roles}}---
name: {{.Lead}}
description: "{{block "description" .}}The {{spaced .Lead}}'s recurring duties: the start of a shift, the merge procedure on a `ready: PR #n`, the notice after a merge and the report to the owner. Use at the start of every shift, on every `ready` and after every merge.{{end}}"
---
# The {{spaced .Lead}}

{{block "intro" .}}The {{spaced .Lead}} is the only session that merges and the one that keeps the work and the documents coherent. Its own issues come second to these duties.{{end}}

## Start of a shift (a wake, or a fresh session)

{{block "shift" .}}1. Read the handoff; check it against `git status`, the branch and `gh pr list --state open`.
2. Pull requests waiting on you: handle every `ready` first (below), oldest first.
3. The owner report (below) if the last one is older than a shift.
4. Then your own issue.{{end}}

## On `ready: PR #n`

{{block "ready" .}}Verify, do not trust. `mingo pr merge <n>` checks that every required check is green on the head, that the newest review of the head says `{{.Workflow.NoFindings}}`, that no thread is unresolved, that the branch is up to date with `{{.Workflow.Base}}` and that the description's first lines are right with no attribution footer. It prints every held point as a numbered list and squash merges, with the branch deleted, only with none. Before it, read the diff against the issue: a drive-by change outside it is held.

The merge is its own command, run after any check's output has been read, never chained behind a step that may fail.

Anything not good: hold. Message the author with each held point as a numbered list; the author's next `ready` names the commit that answers each point.{{end}}

## After a merge

{{block "after-merge" .}}1. **The notice** to every live peer: the new `{{.Workflow.Base}}` head, the shared files the merge touched, which `mingo pr merge` names, and which open branches are now behind and must rebase, with `mingo pr rebase <branch>` where both sides only added to a shared list.
2. The issue closed through the pull request; if it did not, close it and say why.{{end}}

## The owner report

{{block "report" .}}One message to the owner at the end of a shift, at most 12 lines: what merged, what is in progress, the decisions taken, and what only the owner can do, asked once.{{end}}

## Never

{{block "never" .}}- Merge with a red check, a stale review, an unresolved thread, or a branch behind `{{.Workflow.Base}}`, your own pull requests included.
- Approve, dismiss or green anything by hand or by workflow.
- Edit a peer's branch or work in a peer's clone.{{end}}
{{end}}