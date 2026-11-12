## Working alone

{{block "alone" .}}The owner may be away and unable to answer questions or take decisions. While a session's opener or a later message says the owner is away, until one says the owner is back:

1. Progress is the priority. Never stop to ask.
2. When the documents do not answer a question, take the recommended default, or the simplest choice that keeps the documents' intent, record it in the pull request as decided provisionally with the date, and keep going. The owner reviews those entries later and may reverse them.
3. A question only the owner can answer goes to the {{spaced .Lead}}, who asks it once in its next message to the owner, and the work that does not depend on it goes on.{{end}}

## Working as a team

{{block "team" .}}{{len .Roles}} {{template "agent.cli" .}} sessions work on this repository at once, each in its own clone, each reachable from the others by name {{template "agent.reach" .}} per role. The roles:

| Role | Clone | Does |
|---|---|---|
{{range .Roles}}| {{spaced .Name}} | `{{$.CloneShown .Name}}` | {{cell .Does}} |
{{end}}
The rules:

1. A session works only in its own clone. Nobody switches branches, builds or runs anything in another session's folder.
2. A session takes only the work handed to it{{if eq .Tracker.Kind "linear"}}: it moves its issue to In Progress before touching it and never touches an issue another session has In Progress. The label says who owns an issue; the {{spaced .Lead}} sets it{{else if eq .Tracker.Kind "github"}}: it says on its issue that it started it before touching it and never touches an issue another session started. The label says who owns an issue; the {{spaced .Lead}} sets it{{end}}.
3. A pull request's description begins as the `task-flow` skill says.
4. Only the {{spaced .Lead}} merges. When a pull request has every check green, the review's verdict `{{.Workflow.NoFindings}}` on its current head and every thread resolved, its author messages the {{spaced .Lead}} `ready: PR #n` and moves on. The {{spaced .Lead}} verifies those three things with `mingo pr merge <n>`. The merge procedure and the {{spaced .Lead}}'s other duties are the `{{.Lead}}` skill.
5. After every merge the {{spaced .Lead}} tells the sessions whose branches fell behind `{{.Workflow.Base}}`; each rebases its own, with `mingo pr rebase <branch>` where both sides only added to a shared list.
6. {{with .Workflow.Shared}}Shared files take appends at the end of their lists, so two pull requests meet in one small conflict at most: {{shared .}}.{{else}}A list every session adds to takes appends at its end, so two pull requests meet in one small conflict at most.{{end}}
7. Nobody adds a step that approves a pull request, dismisses a review or greens a check. When a repository rule, a secret or a setting blocks the flow, the {{spaced .Lead}} tells the owner in its next message and meanwhile takes the `ci-unavailable` path of the `task-flow` skill.
8. No attribution footers in commits, pull requests or comments.
9. While the owner is away, decisions follow Working alone above. Otherwise a question the documents do not answer goes with a recommended default to the {{spaced .Lead}}, who asks the owner, and the work that does not depend on it goes on.{{end}}

## Working economically

{{block "economically" .}}Tokens are the team's budget. These rules spend fewer of them without lowering the bar.

1. **A fresh session every issue or two.** At an issue boundary, a pull request ready or merged and never mid-issue, a session writes its handoff memory, `handoff-<role>.md`: the branch and its head, {{if not (eq .Tracker.Kind "none")}}the issue states, {{end}}anything half done, the next step. It messages the {{spaced .Lead}} `ready: PR #n, clearing` and clears itself: its last tool call is `mingo clear`, and the role's loop starts a fresh session in the same clone with the opener. The steps are the `clear-context` skill's.
2. **The next {{if eq .Tracker.Kind "none"}}task{{else}}issue{{end}}.** A session that wakes, or ends {{if eq .Tracker.Kind "none"}}a task{{else}}an issue{{end}} without clearing, takes the {{if eq .Tracker.Kind "none"}}task{{else}}issue{{end}} its handoff or the {{spaced .Lead}} named{{if not (eq .Tracker.Kind "none")}}, else the next {{if eq .Tracker.Kind "linear"}}Todo{{else}}open{{end}} issue with its label, highest priority first{{end}}. With none, it messages the {{spaced .Lead}} `no issue for <role>` and waits.
3. **Lean tool use.** Independent tool calls batched; logs grepped precisely; files read in the parts needed and never twice; edits made with short anchors, not whole blocks.
4. **Effort matched to the work.** High or max for design, security, debugging and triaging findings; medium for implementation from a clear plan; low for merges, rebases and docs. A session's settings change only with the owner's word.
5. **One scoped review per code pull request**, by a subagent on the cheaper model: the diff inline, a named list of the files it may read and a cap on its reads, must-fix and should-fix findings only. Every finding is verified before it is acted on. The `sub-agents` skill has the brief.
6. **Edge cases first.** Before coding a subtle feature, a short list of its edge cases, each with a test. The `edge-cases` skill has the lists.
7. **Fewer handoffs.** Small docs changes fold into the next code pull request; the merge order is planned to spare rebases.
8. **A peer's chat has no reader.** The owner reads the {{spaced .Lead}}'s session alone, and the {{spaced .Lead}} reads only what a peer sends it. So the other roles write no summaries for the owner in their own chats; what the {{spaced .Lead}} needs goes by {{template "agent.send" .}}, and what must last goes to {{if not (eq .Tracker.Kind "none")}}the issue, {{end}}the pull request or the handoff.

Never cut: the author's checks, the one review per code pull request, the tests.{{end}}
