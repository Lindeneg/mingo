# mingo

mingo runs multiple claude sessions for multiple projects, one tmux session per role.

## Install

You'll need [tmux](https://github.com/tmux/tmux), [git](https://github.com/git/git), [Go 1.27 or newer](https://github.com/golang/go), [gh](https://cli.github.com/) and [claude code](https://claude.com/product/claude-code).

```bash
git clone https://github.com/Lindeneg/mingo.git
cd mingo && go build -o ~/.local/bin/mingo ./cmd/mingo
mingo install
```

Make sure `~/.local/bin` is on your PATH.

```
$ mingo help
mingo runs Claude Code sessions for every project on this machine, one tmux session per role.

A target is <project>/<role>. A role may be an alias and a project its id, and the project may be
left out inside a project's clone, with MINGO_PROJECT set, or with a default project.

  mingo ls                              projects, roles and clones
  mingo agents [project...]             live sessions with state, uptime and Remote Control link
  mingo status [project...]             each role's state, model and effort, and the Claude login
  mingo start [--autostart] [target...] start roles, a whole project when no role is named
  mingo stop [target...]                ask sessions to exit, close what is left after a minute and a half
  mingo restart [--force] <target>      end a session where it stands, the loop starts a fresh one
  mingo attach <target>                 attach, detach with Ctrl+B then D
  mingo ro <target>                     attach read only
  mingo peek <target> [lines]           the last lines of a session's screen
  mingo say <target> <message>          type a message into a session
  mingo clear                           from inside a session, end it so the loop starts a fresh one
  mingo path <target>                   a role's clone
  mingo memory <target>                 a role's handoff memory
  mingo repo [project]                  a project's GitHub repository
  mingo default [project]               show or set the default project
  mingo project add|show|edit <name>    a project's config in ~/.config/mingo/projects
  mingo setup <project>                 clone missing roles and make their memory folders
  mingo new <name> [--preset p] [...]   a project from a preset, its config, clones and files, asking what flags leave out
  mingo apply <project>                 land the files of the project's modules through a pull request
  mingo doctor <project>                what of the project's modules is in place and what is missing
  mingo secret review <project>         set the review's token from what claude setup-token printed
  mingo service enable|disable <project> start a project's autostart roles at boot, at login on a Mac
  mingo install                         the command, tmux and Claude settings
  mingo heavy <label> -- <command...>   run a heavy command in one of the machine's memory slots
  mingo pr merge <number>               check a ready pull request, then squash merge it
  mingo pr rebase <branch>              rebase a branch onto the base in a worktree, keeping both sides of the shared lists
  mingo gate <log>                      run the project's gate targets into a log, stopping at the first red one
  mingo version                         this mingo's version
  mingo help [command]                  this list, or what a command does

mingo help <command> explains a command, and so does mingo <command> --help.
```

## Config

A project is one TOML file in `~/.config/mingo/projects`.

```toml
repo = "you/{project}"
clone = "~/dev/{project}/{role}"
autostart = ["lead"]

[[roles]]
name = "lead"
model = "claude-opus-5-5"
effort = "xhigh"

[[roles]]
name = "dev"
aliases = ["d"]
model = "claude-sonnet-5-5"
```

## The kit

The kit is a set of modules. `mingo new` picks them by preset, and `mingo apply` lands their files through a pull request.

| Preset | What's in it |
|---|---|
| bare | sessions and guards |
| solo | bare with one role, plus workflow, ci and review |
| team | solo with more roles, plus team, tracker, rules and heavy |
| full | everything |

| Module | Gives you |
|---|---|
| sessions | the roles, clones, loop and handoffs |
| guards | the write and kill guards |
| workflow | issue, branch, pull request, merge with `mingo pr merge`, and the skills that go with it |
| ci | a GitHub Actions workflow with a job per check, for pnpm, npm or go |
| review | a Claude review of every pull request |
| skills | sub-agents, edge-cases and spend skills |
| team | `TEAM.md` with the team's rules, and the clear-context skill |
| tracker | planning with Linear or GitHub Issues, and a label per role |
| rules | a ruleset on the base branch |
| heavy | `tools/heavy.mjs`, which puts a Node project's heavy scripts behind the slots |
| ports | a port offset per role in each clone's `.env` |

## A new server

The `provision` folder turns a fresh Ubuntu server into a mingo machine: a user for the sessions, SSH by key only, a firewall, Docker, Node, Go, gh, tmux and Claude Code.

Make the cloud-init file on your own computer and paste it in as user data when you create the server:

```bash
MINGO_USER=you provision/make-cloud-init.sh ~/.ssh/id_ed25519.pub > cloud-init.yaml
```

The server sets itself up on the first boot and reboots. Then SSH in as that user, run `gh auth login` and `claude auth login`, set your git name and email, and install mingo as above.

## Every key

```toml
id = "sh"                          # a short name that works anywhere the project's name does
title = "Shop"                     # the name the kit's texts use, the project's name by default
repo = "you/{project}"
clone = "~/dev/{project}/{role}"   # leave {role} out for a project of one role
lead = "lead"                      # the first role by default
opener = "You are the {role}. Read your handoff memory, then continue."
autostart = ["lead"]               # what mingo service starts at boot
claude_args = ["--permission-mode", "auto", "--remote-control"]
modules = ["workflow", "ci", "review", "team", "tracker"]   # sessions is always on

[[roles]]
name = "lead"
aliases = ["l"]
model = "claude-opus-5-5"
effort = "xhigh"
does = "merges and hands out the work"

[[roles]]
name = "dev"
model = "claude-sonnet-5-5"
effort = "high"
does = "codes the issues labelled dev"

[tracker]
kind = "linear"                    # linear, github or none, the default
team = "ENG"                       # the Closes line then names ENG-n
project = "Shop"

[workflow]
base = "master"
ci = "ci"                          # the workflow whose jobs are the checks
checks = ["check", "test", "build", "security"]
gate = ["pnpm check", "pnpm test", "pnpm build"]
tags = ["(lead)", "(dev)"]         # what a description's first line may be
local_runs = ["pnpm check", "mingo gate"]
generate_list = ["pnpm exec tsx tools/generate.ts --list"]
generate = ["pnpm generate"]
generated = '^generate: wrote (.+)$'
# review_line, no_findings, review_heading, footers and trailers tell the review comment and
# attribution apart, and default to "review:", "no findings", "## Review", Claude Code's footer
# and session link, and Co-Authored-By and Claude-Session

[[workflow.shared]]                # a list everyone appends to
name = "the route list"
pattern = '^src/routes\.ts$'
merge = "lines"                    # lines, json-keys or none

[ci]
stack = "pnpm"                     # pnpm, npm or go
[ci.run]
check = "pnpm check"               # pnpm <check> or npm run <check> by default

[review]
read = "CLAUDE.md first"
categories = ["correctness (a wrong result, a crash)", "a missing test for an edge case"]
skip = "formatting, lint or style"

[heavy]
scripts = ["check", "test", "build", "dev"]   # each moves to <script>:run behind the shim

[ports]
variable = "APP_PORT_OFFSET"       # the empty line in .env.example that gets the offset
[ports.offsets]
lead = 10
dev = 20

[blocks.".claude/skills/task-flow/SKILL.md"]
checks = '''
Run `pnpm check` and the tests of each package the change touches.'''
```

`MINGO_MODEL_<PROJECT>_<ROLE>` and `MINGO_EFFORT_<PROJECT>_<ROLE>` override a role's model or effort for the sessions a start launches.

## License

MIT, see [LICENSE](LICENSE).
