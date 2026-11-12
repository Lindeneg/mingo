---
name: edge-cases
description: Write the edge case list for a change before coding it{{block "areas-named" .}}{{end}}, {{if eq .Tracker.Kind "none"}}write it in the handoff{{else}}post it on the issue{{end}}, and turn each case into a test. Use at step 4 of task-flow and whenever a feature is subtle.
---
# Edge cases first

A subtle feature coded before its edge cases are listed gets the happy path and a review finding for each missing case. Listing first costs ten minutes and saves the review round{{block "rules" .}}{{end}}. The list is {{if eq .Tracker.Kind "linear"}}posted as a comment on the Linear issue{{else if eq .Tracker.Kind "github"}}posted as a comment on the GitHub issue{{else}}written in the handoff{{end}} before the first line of code, each case becomes a test named after it, and the pull request says which test covers which case.

## Always

- The empty case (no rows, no results, no tools called, no text).
- The not-found case and the already-exists case.
{{block "always-access" .}}- The unauthorized case: wrong user, wrong role, expired token.
{{end}}- The duplicate: the same webhook, message, job or confirmation arriving twice.
- The stale case: a source, a cache or a token past its freshness.
- The disconnect mid-way: a stream, a sync, a job.
- The first and the last: the first message of a conversation, the last page of a listing, the last retry.
- The limit: the cap, the budget, the size limit, exactly at and one over.

{{block "areas" .}}{{end}}## Output

{{if eq .Tracker.Kind "none"}}In the handoff:{{else}}On the issue, as a comment:{{end}}

```
Edge cases:
1. <case> -> test: <name>
2. ...
```

In the pull request, under "Tested": the same numbers against the tests that exist. A case with no test is either tested or struck with a reason the reviewer can check.
