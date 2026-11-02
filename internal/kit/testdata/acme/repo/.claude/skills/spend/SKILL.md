---
name: spend
description: Before any command that costs money or GPU time, check the issue's budget line and the scope, run the smallest scope that answers the question, and record the cost in the pull request. Use before every such run.
---
# Spending on purpose

Tokens, compute and paid accounts are the owner's money; this skill is the check before each run.

## Before the run

1. **Which question does the run answer?** Write it in one line. No question, no run.
2. **Which scope answers it?** The smallest that answers the question: one case while debugging, never the whole run to find one failure.
3. **Is there a budget line?** The issue names the command, the scope and the cap. Without one, the run waits for one.
4. **Is the key the right one?** Development and CI use the development key with its spend limit; production keys are never used by a session. A key is read from the environment, never typed into a command, a brief or a file.
5. **Run with the cap** where the command takes one; a run that would exceed it is stopped.

## After the run

- The cost goes into the pull request under "Spend": the command, the scope, the cost and the cap.
- A run that stopped on its cap is reported as such; it is not rerun with a bigger cap without a new budget line.

## Never

- A full run "to see", or to reproduce a failure that one case reproduces.
- Credentials in a brief, a log, a fixture, a commit or a message.
- Letting a subagent run anything that spends (`sub-agents` skill).
