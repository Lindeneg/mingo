---
name: spend
description: Before any command that costs money or GPU time{{block "costly" .}}{{end}}, check the issue's budget line and the scope, run the smallest scope that answers the question, and record the cost in the pull request. Use before every such run.
---
# Spending on purpose

{{block "intro" .}}Tokens, compute and paid accounts are the owner's money; this skill is the check before each run.{{end}}

## Before the run

1. **Which question does the run answer?** Write it in one line. No question, no run.
2. {{block "scopes" .}}**Which scope answers it?** The smallest that answers the question: one case while debugging, never the whole run to find one failure.{{end}}
3. {{block "budget" .}}**Is there a budget line?** The issue names the command, the scope and the cap. Without one, the run waits for one.{{end}}
4. **Is the key the right one?** Development and CI use the development key with its spend limit; production keys are never used by a session. A key is read from the environment, never typed into a command, a brief or a file.
5. {{block "cap" .}}**Run with the cap** where the command takes one; a run that would exceed it is stopped.{{end}}

## After the run

{{block "record" .}}- The cost goes into the pull request under "Spend": the command, the scope, the cost and the cap.{{end}}
- A run that stopped on its cap is reported as such; it is not rerun with a bigger cap{{block "rerun" .}} without a new budget line.{{end}}{{block "report" .}}{{end}}

## Never

{{block "never" .}}- A full run "to see", or to reproduce a failure that one case reproduces.
- Credentials in a brief, a log, a fixture, a commit or a message.{{end}}
- Letting a subagent run anything that spends (`sub-agents` skill).
