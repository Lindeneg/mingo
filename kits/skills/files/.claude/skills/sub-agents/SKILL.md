---
name: sub-agents
description: When and how to delegate to a subagent (the Agent tool), and when not to. Covers the one scoped review per code pull request, bounded explorations, the brief templates, the model and effort to use, read caps, and the rule that every finding is verified before it is acted on. Use before spawning any subagent and whenever a review is due on a pull request.
---
# Subagents

A subagent starts with an empty context and re-reads what you already know; its turns cost tokens like yours and its report reaches nobody but you. So a subagent is spent on purpose, with a brief that bounds it, and never as a reflex.{{block "rules" .}}{{end}}

## When a subagent is allowed

1. **The scoped review** of a code pull request: one per pull request, a second pass only over a fix commit that changed code. Always.
2. **A bounded exploration** whose answer needs sweeping many files and where only the conclusion matters{{block "exploration-examples" .}} (which modules read the environment, every caller of a function){{end}}. Not for a question one `grep` answers, and not for reading a document you can read in parts.
3. **A parallel, independent, bounded task the issue names**{{block "parallel-examples" .}} (the same change in three modules, three translations){{end}}, each with its own brief, run in one message so they overlap.

## When a subagent is not allowed

- Waiting for anything: a clear, a wake, CI, a review. Background waits and the `clear-context` skill handle those.
{{block "never-run" .}}- Running the gate, the suite or any command that spends money. You run those.{{end}}
- Committing, pushing, merging, opening or closing issues, messaging other sessions. Subagents report; the session acts.
- A "second opinion" on code you have not read yourself, or a review of a docs-only, fixtures-only or catalogs-only pull request (those get a read by hand).
- Anything that needs a credential. A brief never contains a key, a token or a connection string.
- Touching another clone or the owner's files.

## Model and effort

- Reviews and explorations: `model: "sonnet"`, the cheaper model. Pure lookups with a yes or no answer: `model: "haiku"`.
- Never the strongest model for a subagent unless the issue says so and names why.
- Effort: `low` for explorations and lookups, `medium` for the review. Set it in the Agent call; do not leave it to default.
- `run_in_background: true` for anything you do not need before your next action, so you keep working; `false` only when the very next step depends on it.

## The review brief

Paste the diff inline (`git diff {{.Workflow.Base}}...HEAD` or `gh pr diff <n>`); name the files it may read beyond the diff; cap the reads. The template, filled and sent as the prompt:

```
You are reviewing a pull request for {{.Title}}. Report must-fix and should-fix findings only.

Context: <one line: the issue id and what the change does>. {{block "brief-rules" .}}The project's rules are in CLAUDE.md.{{end}}

Diff:
<the diff>

You may read these files in full, and nothing else: <list, at most 8>. At most 12 file reads in total. Do not modify anything. Do not run commands.

Look for, in this order:
{{block "look-for" .}}1. Correctness: a wrong result, a crash, a race, an unhandled outcome, a retry of a write, a swallowed error.
2. Security: a secret or personal data in a log or an error, untrusted input treated as trusted.
3. Missing tests for these edge cases: <the issue's edge case list>.
4. Documents: a behaviour the documents do not describe.{{end}}

{{block "skip" .}}Do not report formatting, lint, style or anything a tool checks.{{end}} Do not praise. Do not summarise the change.

Output: either the single line `no findings`, or a list where each finding is:
- [must-fix | should-fix] <file>:<line>: <what fails and with which input or state>. Fix: <one sentence>.
```

## The exploration brief

```
Answer one question about the {{.Title}} repository; do not modify anything and do not run commands other than reads.

Question: <the question>.
Search in: <directories>. At most <n> file reads.
Answer with: a list of <file>:<line> entries that bear on the question, then one paragraph with the conclusion. Nothing else.
```

## After the subagent returns

1. **Verify every finding yourself** before acting: open the lines; a finding that does not reproduce in your reading is rejected with a reason.
2. Fix the must-fix findings; fix or answer the should-fix ones.
3. Record in the pull request description under "Review": `scoped review: N findings, M fixed, K rejected (reasons)`. The subagent's report itself is not pasted anywhere.
4. A fix commit that changed code gets a second pass with the same brief over the fix commit's diff only; a fix that only changed tests or docs does not.
5. The pull request's own Claude review (the GitHub action) is separate and also runs; its findings are handled the same way.

## Cost rules

- One brief, bounded, beats three vague ones. If you cannot write the file list and the read cap, you do not know enough to delegate yet; read first.
- A fan-out is planned in the issue, never improvised; the issue's comment says how many subagents and why.
- A subagent that reports it ran out of its cap is not re-spawned with a bigger cap by reflex; the question is narrowed.
