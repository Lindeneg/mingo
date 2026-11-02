{{if not (eq .Tracker.Kind "none")}}---
name: plan-milestone
description: "{{block "description" .}}Create a milestone's {{if eq .Tracker.Kind "linear"}}Linear{{else}}GitHub{{end}} issues from its \"done means\": one pull request each, labelled per role, ordered by dependency, with the issue template, and the rule that no role's queue runs dry. Use when a milestone starts, when a role has fewer than two {{if eq .Tracker.Kind "linear"}}Todo{{else}}open{{end}} issues, and when a found problem becomes an issue.{{end}}"
---
# Planning a milestone

Issues are how the sessions work without talking. A good issue is finished in one pull request, by one role, with its tests and documents inside, and it says what done means so the author never guesses. The {{spaced .Lead}} writes them{{block "intro-ref" .}} (`TEAM.md`, Working as a team){{end}}; a peer who finds work writes a draft and the {{spaced .Lead}} labels it.

## Before writing issues

1. {{block "read" .}}Read the milestone's "done means" and the documents it names.{{end}}
2. List the packages and the shared, append-only files the milestone touches{{block "shared-ref" .}} (`TEAM.md`, Working as a team 6){{end}}, so issues on the same list are sequenced or told to append at the end.
3. {{block "order" .}}Draw the dependency order: contracts before consumers; a skeleton before its modules; an interface before its adapters.{{end}}
4. {{block "roles" .}}Decide the role per issue from what each role does (`TEAM.md`, Working as a team).{{end}}

## Slicing rules

- One pull request of at most about 400 changed lines excluding tests, fixtures, catalogs and generated files; split by package boundary first, then by feature.
- Tests, fixtures, documents and open questions are inside the issue, never a separate "add tests" issue.
{{block "skeleton" .}}- A skeleton issue for a new package comes first and is small.{{end}}
{{block "budget-rule" .}}- An issue that needs a run that costs money carries a budget line; without one it must not spend (`spend` skill).{{end}}
- No "investigate X" issue without a done-means that is checkable; a spike has a question, a time box, and the open questions entry it must produce.
- No issue for work another session has In Progress, and no issue that cannot be started because its dependency is not merged, unless it is marked blocked.

## The issue

Title: `<area>: <what>`{{block "title-examples" .}} (`api: the login route`, `docs: record the cache decision`){{end}}.

Body, every section present:

```
## Context
<two or three lines; links to the document sections that define the behaviour>

## Done means
- <checkable statement>
- <checkable statement>

## Edge cases to cover
- <initial list; the author extends it before coding (`edge-cases` skill)>

## Files and shared files
- Packages: <list>
- Append-only lists touched: <list or none>

## Depends on
- {{if eq .Tracker.Kind "linear"}}{{.Tracker.Team}}-n{{else}}#n{{end}} <why>, or none

## Budget
{{block "budget-line" .}}- none | <command, scope, cap, what question it answers>{{end}}

## For the reviewer
- <what to look at first; which sensitive paths, if any>
```

{{block "fields" .}}{{if eq .Tracker.Kind "linear"}}Fields: team `{{.Tracker.Team}}`, project `{{.Tracker.Project}}`, the milestone, the label `<role>`, priority by dependency depth (1 for the roots, 4 for the leaves), state {{if eq .Tracker.Kind "linear"}}Todo{{else}}open{{end}}. Blocking relations in Linear where the connector supports them, else in the body.{{else}}Fields: the milestone and the label `<role>`, the issues made in dependency order. Blocking relations in the body.{{end}}{{end}}

{{block "mechanics" .}}{{if eq .Tracker.Kind "linear"}}## Linear mechanics

- While the project `{{.Tracker.Project}}` does not exist in Linear, making it, its milestones in order with each one's "done means" as its description, and the role labels through the connector is the {{spaced .Lead}}'s first task.
- Issues are created with the connector (`save_issue`), one call each, in dependency order so the ids read in order.
- An issue's state is changed only by its owner session ({{if eq .Tracker.Kind "linear"}}Todo{{else}}open{{end}} to In Progress) or by the pull request's merge (to Done); the {{spaced .Lead}} cancels with a comment that says why.{{else}}## GitHub mechanics

- A milestone is made once with `gh api repos/{owner}/{repo}/milestones -f title=<name> -f description=<done means>`.
- Issues are created with `gh issue create --milestone <name> --label <role>`, one call each, in dependency order so the numbers read in order.
- An issue closes through its pull request's merge; the {{spaced .Lead}} closes one by hand with a comment that says why.{{end}}{{end}}

## Keeping queues full

- Each role: at least 2 {{if eq .Tracker.Kind "linear"}}Todo{{else}}open{{end}} issues in the current milestone at all times, at most about 15 open so the queue stays readable.
- When the current milestone has fewer than 5 {{if eq .Tracker.Kind "linear"}}Todo{{else}}open{{end}} issues in total, create the next milestone's issues before the current one runs dry.
- A role whose issues are all blocked gets an unblocked one from the next milestone rather than an idle hour.

## A found problem

A failing nightly, a review finding out of scope, a bug seen in passing: an issue at the top of the list with the failing command and the log excerpt (ids and sizes, never content or secrets), labelled for the area's role, priority 1{{block "found-promise" .}}, and a line in the owner report{{end}}.

## Never

- An issue for behaviour no document asks for; that is an open question first.
- An issue that decides a product question in its done-means.
- Two issues on the same append-only list without a sequencing note.
- An issue without a done-means, or with a done-means that says "works".
{{end}}