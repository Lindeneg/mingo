# CLAUDE.md

mingo runs agent sessions, through a provider for each agent CLI with Claude Code the first, for every project on one machine: a tmux session per role, a loop that starts a fresh session from the role's handoff whenever one ends, shared slots for heavy commands, guards for unattended sessions, and a kit of modules that sets a project up, from bare sessions to a whole team with its workflow, CI and review. `README.md` says how it is used.

## Layout

| Path | Holds |
|---|---|
| `cmd/mingo` | the entry point, and the end to end tests, which run the real binary |
| `internal/cli` | each command and its help page |
| `internal/config` | a project's TOML config, its checks, and how targets resolve |
| `internal/host` | the tmux sessions |
| `internal/loop` | the loop that runs a role's sessions one after another |
| `internal/heavy` | the shared slots for heavy commands |
| `internal/guard` | the decisions of the write and kill guards an agent CLI runs as hooks |
| `internal/install` | `mingo install` |
| `internal/provider` | what mingo needs of the agent CLI a project's sessions run, and the providers by name |
| `internal/provider/claude` | Claude Code as a provider: its sessions, memory, settings, folder trust, hooks, review workflow and the words the kit's templates take from it |
| `internal/kit` | rendering, the lock, `mingo apply`, `mingo doctor` and `mingo new` |
| `internal/team` | `mingo pr merge`, `mingo pr rebase` and `mingo gate` |
| `internal/sys` | what differs between operating systems |
| `internal/ojson` | JSON that keeps the order of its keys |
| `kits/<module>` | a module: `module.toml` says what it asks, sets up and checks, and `files/` holds its templates |
| `share` | the systemd unit, the launch agent and the project template |
| `provision` | the scripts that set up a fresh server |

## Rules

- The standard library first. The dependencies are `github.com/BurntSushi/toml` and `golang.org/x/sys`, and a new one needs a reason the standard library cannot meet.
- Comments only where the code is unreadable without them, and they say why: plain lowercase, no `-`, `;` or `:`, nothing about where the code came from. Each package has one Go doc line that says what it does.
- A command's help page in `internal/cli/help.go` stays true to what the command does, and so does `README.md`.
- A Claude Code setting, a `gh` call or a GitHub API is checked against its current documentation before code is written against it.
- A kit template is generic. A project's own text goes in its config as a block, never in the template.

## Tests

- A test is named as a sentence that says what holds.
- The end to end tests in `cmd/mingo` build mingo and run it against a private tmux server with a stand-in for Claude Code. Nothing a test runs touches the default tmux server, `/tmp/mingo` or the real home folder: `MINGO_TMUX_SOCKET`, `MINGO_STATE`, `MINGO_CONFIG` and `HOME` point into the test's own folder.
- `internal/kit/testdata/acme` is a made-up project whose rendered files are checked byte for byte. After a template changes on purpose, `KIT_UPDATE=1 go test ./internal/kit` writes them again; read the diff before keeping it.

## Checks

All of these pass before a change lands:

```bash
go test ./...
go vet ./... && staticcheck ./... && gofmt -l .
GOOS=darwin go build ./... && GOOS=windows go build ./...
GOOS=darwin go vet ./...
```

`gofmt -l .` prints nothing. `GOOS=darwin go vet ./...` also compiles the macOS tests.

## Commits

A commit's subject says what is now true, as a plain sentence. The body is short and says why.
