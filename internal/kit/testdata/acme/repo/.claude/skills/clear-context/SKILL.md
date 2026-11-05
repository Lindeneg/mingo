---
name: clear-context
description: Clear a session's context at an issue boundary, where the worker loop starts a fresh session whenever one ends, and the lead's look at the list so that no role runs without a session. Use before clearing your own context, when you wake, at every commit of your own and after every merge. Follow the steps here, never the steps as remembered.
---
# Clearing a context

Each role runs in a loop (mingo's: `mingo start acme/<role>`): when a session ends, the loop starts a fresh one in the same clone with the opener `You are the <role> of acme. Read your handoff memory, then continue.` So a clear is one session's own work, in one turn: write the handoff, tell the lead, ask to be restarted. Nothing waits for a wake and nobody wakes anybody. Read `TEAM.md`, Working economically 1, for why and when; the steps are here.

## Before you clear

Only at an issue boundary: a pull request ready or merged, nothing half done in the tree. Never mid-issue.

1. **Leave the tree clean and nothing running.** Everything committed and pushed, or said in the handoff as not pushed. Every background task of the session is stopped (`TaskStop`) and no wait is left behind: a background wait for a merge, a gate, a dev server. `/exit` is answered with a question while one still runs ("Background work is running"), and the session then reads no message until the loop ends it (below).
2. **Write the handoff**, `handoff-<role>.md` in your clone's memory folder, the newest block first: the branch and its head, the pull requests and the issue states, anything half done, the next step, and what this shift taught. Save any other memory worth keeping in the same turn.
3. **Tell the lead** by `SendMessage` to its row in `ListAgents`, `acme-lead`: `ready: PR #n, clearing` or `clearing, nothing ready`. The lead answers nothing; the loop does the restarting.
4. **Ask to be restarted:** as the last tool call of the turn, the shell command `mingo clear`, which touches the session's restart marker (the loop told the session its project and role), then end the turn with one short line and no further tool call. The loop's watcher sees the marker within five seconds, types `/exit`, and starts the next session when this one has ended. A session that `/exit` has not ended after a minute is ended by its pid: the CLI answers `/exit` with a question nobody is there to answer when a background task still runs or when the session drafted a feedback note with `SendFeedback` ("You have 1 unsent feedback draft"). The minute is what such a session now costs; a draft stays on disk for the owner.
5. **Never touch the marker mid-turn**, and never before the handoff is written: `/exit` arrives within five seconds and ends the session where it stands, in the middle of its last line too, so whatever the turn still had to do or say is lost. What must be read, the line to the lead or a report to the owner, is sent before the marker; a report to the owner is the last message of a turn that does not clear.

## When you wake

1. Read your handoff, then check it against `git status`, the branch and its head, your pull request and your Linear issue. What they say wins; correct the handoff.
2. Report to the lead in one line, by `SendMessage` to its row.
3. Take the issue the handoff or the lead named, else the next Todo issue with your label, and move it to In Progress before anything else.
4. A second opener that arrives while you are at work is a repeat: ignore it.

## A role's first session

A role that has just joined has a clone and no handoff. Its first session finds none and does this instead, once:

1. In its clone, sets up what the project's README asks for.
2. Reads `CLAUDE.md` whole, then the `task-flow` skill.
3. Reports to the lead in one line, by `SendMessage` to its row, and takes the issue the lead names, else the first Todo issue with its label, highest priority first.
4. Writes its first handoff at its first boundary, as above.

## The lead's look at the list

At every commit of its own, after every merge and before ending a turn: `ListAgents`. Every role has a row named `acme-<role>` (the lead's own row is not listed to itself). A role with no row for longer than a minute has a stopped loop or a session that could not start: `mingo status acme`, then `mingo start acme/<role>`. A role whose session hangs (no answer to a message for a long time, judged with care, since a session waiting for its gate is quiet too) gets `mingo restart --force acme/<role>`, which ends the session whatever holds it (Ctrl+C, the loop's `/exit`, then its pid) and costs that session its context, so its handoff is the last one written. Starting or restarting a peer's loop is the one thing the lead does in another role's name, and only through mingo.

