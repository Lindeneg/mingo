---
name: task-flow
description: Take a task from a Linear issue to a merged pull request on master, through the branch, the CI gate and the Claude review, including the only exception when GitHub Actions is out of usage. Use at the start and the end of every task.
---
# From issue to merge

Linear and GitHub are connected; use the connectors.

## Start

1. Find or create the Linear issue under its milestone (team `ACM`, the project `Acme Shop`). If a milestone has no issues yet, create them first: one per module, feature or document, each one pull request in size, each labelled for a role.
2. Move the issue to In Progress.
3. `git switch master && git pull`, then `git switch -c <issue id>-<short slug>`.
4. Read the documents for the area you will touch. Write the edge case list in the issue as a comment before coding anything subtle.

## Work

5. Make the change with its tests and documents in the same branch. Verify third-party API shapes against current documentation before writing against them.
6. Run `pnpm check` and the tests of each package the change touches, on the head the pull request names. A change to the checkout also runs `pnpm test --filter checkout` whole.
7. Commit; push the branch.

## Pull request

8. Open the pull request to `master`: the role tag line first (`(lead)`, `(dev)`, `(design)`), a blank line, `Closes <issue id>`, then what changed, how it was tested, what to look at first.
9. Wait for CI and for the Claude review. Fix every red job and address every finding on the branch; the next push runs the review again.
10. Keep the branch up to date with `master`; resolve every conversation. A branch made from another branch that has since merged by squash is rebased with `git rebase --onto origin/master <the old base's head>`, or made again from `origin/master` while it has no commit of its own: a plain rebase replays the base's commits onto their own squash and conflicts.

## Merge

11. When every check is green, the review is finished with its findings addressed and every thread resolved, and the branch is up to date, message the lead `ready: PR #n` and move on; the lead is the only session that merges: it verifies those three things with `mingo pr merge <n>`, which squash merges, the issue closes through the pull request, the branch is deleted.

## The only exception

When GitHub Actions is out of usage and the checks cannot run:

* Run the checks of step 6 on the head, nothing skipped.
* Run the review locally with Claude Code over the diff and address its findings.
* Paste the local results into the pull request description, which the merge check reads: it holds a pull request under the label without the description naming `pnpm check`, `pnpm test`, `pnpm build` or `mingo gate`, a `## Review` section and the head's short SHA. Add the label `ci-unavailable`, then message the lead, who merges.
* As soon as Actions is back, run the full CI on `master` by hand and turn anything it finds into the next issue, at the top of the list.

A local check that is not fully green is not a reason to merge anyway; it is a reason to wait.
