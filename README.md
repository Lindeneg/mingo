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
  mingo away <project>                  the owner is away, the sessions take defaults instead of asking
  mingo back <project>                  the owner is back, the sessions ask again
  mingo path <target>                   a role's clone
  mingo memory <target>                 a role's handoff memory
  mingo repo [project]                  a project's GitHub repository
  mingo default [project]               show or set the default project
  mingo project add|show|edit <name>    a project's config in ~/.config/mingo/projects
  mingo setup <project>                 clone missing roles and make their memory folders
  mingo new <name> [--preset p] [...]   a project from a preset, its config, clones and files, asking what flags leave out
  mingo apply <project>                 land the files of the project's modules through a pull request
  mingo doctor <project>                what of the project's modules is in place and what is missing
  mingo claude edit <project>           a local project's CLAUDE.md, which mingo apply writes into every clone
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
| team | solo with any roles, plus skills, team, tracker, rules, and heavy for Node |
| full | everything, heavy only for Node |

At a terminal `mingo new` asks for what its flags leave out, checks each answer as it is given, and says what it will do before it writes anything. Without a terminal the flags and the defaults are all it has.

| Module | Gives you |
|---|---|
| sessions | the roles, clones, loop and handoffs |
| guards | the write and kill guards |
| workflow | issue, branch, pull request, merge with `mingo pr merge`, and the skills that go with it |
| ci | a GitHub Actions workflow with a job per check, for pnpm, npm, go or any other stack with its own commands |
| review | a Claude review of every pull request |
| skills | sub-agents, edge-cases and spend skills |
| team | `TEAM.md` with the team's rules, and the clear-context skill |
| tracker | planning with Linear or GitHub Issues, and a label per role |
| rules | a ruleset on the base branch |
| heavy | `tools/heavy.mjs`, which puts a Node project's heavy scripts behind the slots |
| ports | a port offset per role in each clone's `.env` |

The guards catch mistakes, not a session that means to get round them.

On a repository an organization owns, the secret scan of the ci module needs gitleaks' license key, free from [gitleaks.io](https://gitleaks.io), as the repository's secret `GITLEAKS_LICENSE`.

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
title = "Shop"                     # the project's display name, its name by default
repo = "you/{project}"             # owner/name on GitHub
clone = "~/dev/{project}/{role}"   # leave {role} out for a project of one role
lead = "lead"                      # the first role by default
opener = "You are the {role}. Read your handoff memory, then continue."
autostart = ["lead"]               # what mingo service starts at boot
claude_args = ["--permission-mode", "auto", "--remote-control"]
claude_files = "repo"              # or local: CLAUDE.md, what it imports and .claude in each clone, out of git
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
kind = "linear"                    # linear, github or none, none by default, with which the tracker module does nothing
team = "ENG"                       # the Closes line then names ENG-n
project = "Shop"

[workflow]
base = "master"
ci = "ci"                          # the workflow whose jobs are the checks
checks = ["check", "test", "build", "security"]   # the checks by the names GitHub shows on a pull request, job ids with the ci module, which needs one
gate = ["pnpm check", "pnpm test", "pnpm build"]  # run without a shell, so no &&, pipes, redirects or quotes
tags = ["(lead)", "(dev)"]         # the role tags a description starts with
reviewer = "claude[bot]"           # whose review comments count, anyone's by default
local_runs = ["pnpm check", "mingo gate"]
# generate_list names the generated files and generate writes them again, for mingo pr rebase. They
# run in a fresh checkout without the clone's installed dependencies, so they must run without them
generate_list = ["node tools/generate.mjs --list"]   # these two without a shell too
generate = ["node tools/generate.mjs"]
generated = '^generate: wrote (.+)$'
# review_line, no_findings, review_heading, footers and trailers tell the review comment and
# attribution apart, and default to "review:", "no findings", "## Review", Claude Code's footer
# and session link, and Co-Authored-By and Claude-Session

[[workflow.shared]]                # a list everyone appends to
name = "the route list"
pattern = '^src/routes\.ts$'
merge = "lines"                    # lines, json-keys or none

[ci]
stack = "pnpm"                     # pnpm, npm, go or other, which the ci module needs
# setup = ["make deps"]            # other only: the commands CI runs before each check
# audit = "make audit"             # other only: the dependency audit of the security job
[ci.run]
check = "pnpm check"               # a shell line for one of the checks, pnpm <check> or npm run <check> by default

[review]
read = "CLAUDE.md first"           # README.md first when claude_files is local
categories = ["correctness (a wrong result, a crash)", "a missing test for an edge case"]
skip = "formatting, lint or style"

[heavy]
scripts = ["check", "test", "build", "dev"]   # each moves to <script>:run behind the shim

[ports]
variable = "APP_PORT_OFFSET"       # the empty line in .env.example that gets the offset
[ports.offsets]                    # which the ports module needs
lead = 10
dev = 20

[blocks.".claude/skills/task-flow/SKILL.md"]
checks = '''
Run `pnpm check` and the tests of each package the change touches.'''
```

mingo checks the config each time it reads it, and refuses a key it does not know, a key set to nothing, and a value it cannot use, naming the file, the key and the value. Two projects whose roles would share a tmux session, such as shop's role `dev-api` and shop-dev's role `api`, are refused too. While a config does not load, its running loops go on with the config they loaded, and `stop`, `restart`, `attach`, `ro`, `peek` and `say` still reach a session written as `<project>/<role>`. `stop <project>` then stops each role that has run a session on this machine.

`MINGO_MODEL_<PROJECT>_<ROLE>` and `MINGO_EFFORT_<PROJECT>_<ROLE>` override a role's model or effort for the sessions a start launches.

## License

MIT, see [LICENSE](LICENSE).
