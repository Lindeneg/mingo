---
name: plan-milestone
description: "Create a milestone's Linear issues from its \"done means\": one pull request each, labelled per role, ordered by dependency, with the issue template, and the rule that no role's queue runs dry. Use when a milestone starts, when a role has fewer than two Todo issues, and when a found problem becomes an issue."
---
# Planning a milestone

Issues are how the sessions work without talking. A good issue is finished in one pull request, by one role, with its tests and documents inside, and it says what done means so the author never guesses. The lead writes them (`TEAM.md`, Working as a team); a peer who finds work writes a draft and the lead labels it.

## Before writing issues

1. Read the milestone's "done means" and the documents it names.
2. List the packages and the shared, append-only files the milestone touches (`TEAM.md`, Working as a team 6), so issues on the same list are sequenced or told to append at the end.
3. Draw the dependency order: contracts before consumers; a skeleton before its modules; an interface before its adapters.
4. Decide the role per issue from what each role does (`TEAM.md`, Working as a team).

## Slicing rules

- One pull request of at most about 400 changed lines excluding tests, fixtures, catalogs and generated files; split by package boundary first, then by feature.
- Tests, fixtures, documents and open questions are inside the issue, never a separate "add tests" issue.
- A skeleton issue for a new package comes first and is small.
- An issue that needs a run that costs money carries a budget line; without one it must not spend (`spend` skill).
- No "investigate X" issue without a done-means that is checkable; a spike has a question, a time box, and the open questions entry it must produce.
- No issue for work another session has In Progress, and no issue that cannot be started because its dependency is not merged, unless it is marked blocked.

## The issue

Title: `<area>: <what>` (`api: the login route`, `docs: record the cache decision`).

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
- ACM-n <why>, or none

## Budget
- none | <command, scope, cap, what question it answers>

## For the reviewer
- <what to look at first; which sensitive paths, if any>
```

Fields: team `ACM`, project `Acme Shop`, the milestone, the label `<role>`, priority by dependency depth (1 for the roots, 4 for the leaves), state Todo. Blocking relations in Linear where the connector supports them, else in the body.

## Linear mechanics

- While the project `Acme Shop` does not exist in Linear, making it, its milestones in order with each one's "done means" as its description, and the role labels through the connector is the lead's first task.
- Issues are created with the connector (`save_issue`), one call each, in dependency order so the ids read in order.
- An issue's state is changed only by its owner session (Todo to In Progress) or by the pull request's merge (to Done); the lead cancels with a comment that says why.

## Keeping queues full

- Each role: at least 2 Todo issues in the current milestone at all times, at most about 15 open so the queue stays readable.
- When the current milestone has fewer than 5 Todo issues in total, create the next milestone's issues before the current one runs dry.
- A role whose issues are all blocked gets an unblocked one from the next milestone rather than an idle hour.

## A found problem

A failing nightly, a review finding out of scope, a bug seen in passing: an issue at the top of the list with the failing command and the log excerpt (ids and sizes, never content or secrets), labelled for the area's role, priority 1, and a line in the owner report.

## Never

- An issue for behaviour no document asks for; that is an open question first.
- An issue that decides a product question in its done-means.
- Two issues on the same append-only list without a sequencing note.
- An issue without a done-means, or with a done-means that says "works".
