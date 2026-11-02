---
name: edge-cases
description: Write the edge case list for a change before coding it, post it on the issue, and turn each case into a test. Use at step 4 of task-flow and whenever a feature is subtle.
---
# Edge cases first

A subtle feature coded before its edge cases are listed gets the happy path and a review finding for each missing case. Listing first costs ten minutes and saves the review round. The list is posted as a comment on the Linear issue before the first line of code, each case becomes a test named after it, and the pull request says which test covers which case.

## Always

- The empty case (no rows, no results, no tools called, no text).
- The not-found case and the already-exists case.
- The unauthorized case: wrong user, wrong role, expired token.
- The duplicate: the same webhook, message, job or confirmation arriving twice.
- The stale case: a source, a cache or a token past its freshness.
- The disconnect mid-way: a stream, a sync, a job.
- The first and the last: the first message of a conversation, the last page of a listing, the last retry.
- The limit: the cap, the budget, the size limit, exactly at and one over.

## By area

**Checkout**
- A cart emptied in another tab; a price that changed since the cart was filled.
- A payment that times out and then succeeds; the same order placed twice.

## Output

On the issue, as a comment:

```
Edge cases:
1. <case> -> test: <name>
2. ...
```

In the pull request, under "Tested": the same numbers against the tests that exist. A case with no test is either tested or struck with a reason the reviewer can check.
