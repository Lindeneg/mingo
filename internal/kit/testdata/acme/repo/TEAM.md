## Working alone

The owner is away until the shop opens. Until then:

1. Progress is the priority. Never stop to ask.
2. A question the documents do not answer gets the simplest answer that keeps the documents' intent, written in the pull request as decided provisionally.

## Working as a team

3 Claude Code sessions work on this repository at once, each in its own clone, each reachable from the others by name through the session messaging tool: `ListAgents` shows a row `acme-<role>` per role. The roles:

| Role | Clone | Does |
|---|---|---|
| lead | `~/dev/acme/lead` | merges, hands out the issues and keeps the documents true |
| dev | `~/dev/acme/dev` | codes the issues labelled dev: the checkout, the catalog and the api |
| design | `~/dev/acme/design` | codes the issues labelled design: the pages, the styles and the copy |

The rules:

1. A session works only in its own clone. Nobody switches branches, builds or runs anything in another session's folder.
2. A session takes only the work handed to it: it moves its issue to In Progress before touching it and never touches an issue another session has In Progress. The label says who owns an issue; the lead sets it.
3. A pull request's description begins as the `task-flow` skill says.
4. Only the lead merges. When a pull request has every check green, the review's verdict `no findings` on its current head and every thread resolved, its author messages the lead `ready: PR #n` and moves on. The lead verifies those three things with `mingo pr merge <n>`. The merge procedure and the lead's other duties are the `lead` skill.
5. After every merge the lead tells the sessions whose branches fell behind `master`; each rebases its own, with `mingo pr rebase <branch>` where both sides only added to a shared list.
6. Shared files take appends at the end of their lists, so two pull requests meet in one small conflict at most: the route list and a catalog.
7. Nobody adds a step that approves a pull request, dismisses a review or greens a check. When a repository rule, a secret or a setting blocks the flow, the lead tells the owner in its next message and meanwhile takes the `ci-unavailable` path of the `task-flow` skill.
8. No attribution footers in commits, pull requests or comments.
9. Decisions follow the Working alone rule above: the recommended default, recorded as decided provisionally, and on with the work.

## Working economically

Tokens are the team's budget. These rules spend fewer of them without lowering the bar.

1. **A fresh session every issue or two.** At an issue boundary, a pull request ready or merged and never mid-issue, a session writes its handoff memory, `handoff-<role>.md`: the branch and its head, the issue states, anything half done, the next step. It messages the lead `ready: PR #n, clearing` and clears itself: its last tool call is `mingo clear`, and the role's loop starts a fresh session in the same clone with the opener. The steps are the `clear-context` skill's.
2. **The next issue.** A session that wakes, or ends an issue without clearing, takes the issue its handoff or the lead named, else the next Todo issue with its label, highest priority first. With none, it messages the lead `no issue for <role>` and waits.
3. **Lean tool use.** Independent tool calls batched; logs grepped precisely; files read in the parts needed and never twice; edits made with short anchors, not whole blocks.
4. **Effort matched to the work.** High or max for design, security, debugging and triaging findings; medium for implementation from a clear plan; low for merges, rebases and docs. A session's settings change only with the owner's word.
5. **One scoped review per code pull request**, by a subagent on the cheaper model: the diff inline, a named list of the files it may read and a cap on its reads, must-fix and should-fix findings only. Every finding is verified before it is acted on. The `sub-agents` skill has the brief.
6. **Edge cases first.** Before coding a subtle feature, a short list of its edge cases, each with a test. The `edge-cases` skill has the lists.
7. **Fewer handoffs.** Small docs changes fold into the next code pull request; the merge order is planned to spare rebases.
8. **A peer's chat has no reader.** The owner reads the lead's session alone, and the lead reads only what a peer sends it. So the other roles write no summaries for the owner in their own chats; what the lead needs goes by `SendMessage`, and what must last goes to the issue, the pull request or the handoff.

Never cut: the author's checks, the one review per code pull request, the tests.
