---
name: task-flow
description: Take a task from {{if eq .Tracker.Kind "linear"}}a Linear issue{{else if eq .Tracker.Kind "github"}}a GitHub issue{{else}}its start{{end}} to a merged pull request on {{.Workflow.Base}}, through the branch, the CI gate and the {{template "agent.review" .}}, including the only exception when GitHub Actions is out of usage. Use at the start and the end of every task.
---
# From issue to merge

{{block "intro" .}}{{if eq .Tracker.Kind "linear"}}Linear and GitHub are connected; use the connectors.{{else}}GitHub is connected; use `gh`.{{end}}{{end}}

## Start

{{block "find-issue" .}}{{if eq .Tracker.Kind "none"}}1. Take the task the owner or the plan names, and write it at the top of your handoff.
2. Say in the handoff what done means for it.
{{else}}1. Find or create the {{if eq .Tracker.Kind "linear"}}Linear issue under its milestone (team `{{.Tracker.Team}}`{{with .Tracker.Project}}, the project `{{.}}`{{end}}){{else}}GitHub issue under its milestone{{end}}. If a milestone has no issues yet, create them first: one per module, feature or document, each one pull request in size{{if many .Roles}}, each labelled for a role{{end}}.
2. {{if eq .Tracker.Kind "linear"}}Move the issue to In Progress.{{else}}Say on the issue that you started it.{{end}}
{{end}}{{end}}3. `git switch {{.Workflow.Base}} && git pull`, then `git switch -c {{if not (eq .Tracker.Kind "none")}}<issue id>-{{end}}<short slug>`.
4. {{block "read-first" .}}Read the documents for the area you will touch. Write the edge case list {{if eq .Tracker.Kind "none"}}in the handoff{{else}}in the issue as a comment{{end}} before coding anything subtle.{{end}}

## Work

5. {{block "done" .}}Make the change with its tests and documents in the same branch. Verify third-party API shapes against current documentation before writing against them.{{end}}
6. {{block "checks" .}}Run the checks of what the change touches before marking ready, on the head the pull request names{{with .Workflow.Gate}}: {{ticks .}}{{end}}.{{end}}
7. {{block "commit" .}}Commit; push the branch.{{end}}

## Pull request

8. Open the pull request to `{{.Workflow.Base}}`: {{with .Workflow.Tags}}the role tag line first ({{ticks .}}), a blank line, {{end}}{{if not (eq .Tracker.Kind "none")}}`Closes <issue id>`, then {{end}}what changed, how it was tested, what to look at first.
9. Wait for CI and for the {{template "agent.review" .}}. Fix every red job and address every finding on the branch; the next push runs the review again.
10. Keep the branch up to date with `{{.Workflow.Base}}`; resolve every conversation.{{block "stacked" .}} A branch made from another branch that has since merged by squash is rebased with `git rebase --onto origin/{{.Workflow.Base}} <the old base's head>`, or made again from `origin/{{.Workflow.Base}}` while it has no commit of its own: a plain rebase replays the base's commits onto their own squash and conflicts.{{end}}

## Merge

11. {{block "merge" .}}When every check is green, the review is finished with its findings addressed and every thread resolved, and the branch is up to date, {{if many .Roles}}message the {{spaced .Lead}} `ready: PR #n` and move on; the {{spaced .Lead}} is the only session that merges: it verifies those three things with `mingo pr merge <n>`, which squash merges{{else}}`mingo pr merge <n>` verifies those three things and squash merges{{end}}{{if not (eq .Tracker.Kind "none")}}, the issue closes through the pull request{{end}}, the branch is deleted.{{end}}

## The only exception

When GitHub Actions is out of usage and the checks cannot run:

{{block "exception-checks" .}}* Run the checks of step 6 on the head, nothing skipped.{{end}}
* Run the review locally with {{template "agent.cli" .}} over the diff and address its findings.
{{block "exception-record" .}}* Paste the local results into the pull request description, which the merge check reads: it holds a pull request under the label without the description naming {{orlist .Workflow.LocalRuns}}, a `{{.Workflow.ReviewHeading}}` section and the head's short SHA. Add the label `ci-unavailable`, then {{if many .Roles}}message the {{spaced .Lead}}, who merges{{else}}merge with `mingo pr merge <n>`{{end}}.{{end}}
* As soon as Actions is back, run the full CI on `{{.Workflow.Base}}` by hand and turn anything it finds into the next {{if eq .Tracker.Kind "none"}}task{{else}}issue{{end}}, at the top of the list.

A local check that is not fully green is not a reason to merge anyway; it is a reason to wait.
