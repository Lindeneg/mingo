package cli

import (
	"fmt"
	"strings"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/kit"
)

const targets = `
A target is <project>/<role>. The role may be an alias and the project its id. Inside a project's
clone, with MINGO_PROJECT set, or after mingo default <project>, the project may be left out. With
no role, the project's lead.
`

var pages = map[string]string{
	"ls": `mingo ls

Lists every project with its id and repository, then each role with its clone, the lead marked.
`,
	"agents": `mingo agents [project...]

Shows each role's session: its state, how long it has been up, and its Remote Control link. The
state is stopped, starting, between (one session has ended and the next has not started), or what
Claude Code says of the session, such as busy or idle. With no project, every project.
`,
	"status": `mingo status [project...]

Says of each role whether its session is running, between sessions or not running, and the model
and effort it runs on or would start on, then the Claude login of this machine. With no project,
every project.
`,
	"start": `mingo start [--autostart] [target...]

Starts a tmux session per role. Each runs the role's loop: Claude Code in the role's clone with the
project's opener, started again whenever a session ends, so a fresh session goes on from the
handoff. A project starts every role, a role starts alone, and a role already running is left as
it is. With no target, the default project.

  --autostart   a project starts only the roles its autostart names, as the boot service does

Each role starts on the model and effort of its config. MINGO_MODEL_<PROJECT>_<ROLE> and
MINGO_EFFORT_<PROJECT>_<ROLE>, such as MINGO_MODEL_ACME_DEV, change them for the sessions
this start launches.

  mingo start acme
  mingo start acme/dev
` + targets,
	"stop": `mingo stop [target...]

Asks each session to exit, waits up to 90 seconds for them all, then closes the tmux session of any
still there. A project stops every role. With no target, the default project. MINGO_STOP_WAIT
changes the wait, in seconds.
` + targets,
	"restart": `mingo restart [--force] <target>

Ends a role's session where it stands: the loop types /exit within seconds and starts a fresh
session, which goes on from the handoff. The session's context is lost, so its last handoff is what
the fresh one starts from.

  --force   for a session a question or a long turn holds: Ctrl+C first, then /exit, and the
            session's process is ended when that does not end it
` + targets,
	"attach": `mingo attach <target>

Attaches this terminal to a role's session. Detach with Ctrl+B then D. Ctrl+C reaches the session,
and twice, or Ctrl+D, ends it.
` + targets,
	"ro": `mingo ro <target>

Attaches this terminal to a role's session read only: what is typed does not reach it. Detach with
Ctrl+B then D.
` + targets,
	"peek": `mingo peek <target> [lines]

Prints the last lines of a role's screen, 40 unless lines says otherwise.

  mingo peek acme/dev 80
` + targets,
	"say": `mingo say <target> <message>

Types the message into a role's session and presses Enter, as if it were typed there. Between two
sessions nothing is typed, since it would be the next session's first input: try again in a few
seconds.

  mingo say acme/dev "pull master"
` + targets,
	"clear": `mingo clear

For a session to run as its last tool call: the role's loop ends the session within seconds and
starts a fresh one, which goes on from the handoff. Write the handoff first, since /exit ends the
session where it stands. The project and role come from MINGO_PROJECT and MINGO_ROLE, which the
loop sets, or from the clone the command runs in.
`,
	"path": `mingo path <target>

Prints a role's clone, the folder its sessions run in.
` + targets,
	"memory": `mingo memory <target>

Prints a role's handoff, handoff-<role>.md in its memory folder, else its MEMORY.md, else the
folder itself.
` + targets,
	"repo": `mingo repo [project]

Prints a project's GitHub repository, as owner/name. With no project, the default one.
`,
	"default": `mingo default [project]

Sets the default project, or prints it with no project named. A target that leaves the project out
takes MINGO_PROJECT first, then the project of the clone the command runs in, then the default,
then the one project when there is only one.
`,
	"project": `mingo project add|show|edit <name>

A project's config, a TOML file in ~/.config/mingo/projects. MINGO_CONFIG moves the folder.

  add    writes a config from the template, to fill in with edit
  show   prints the config
  edit   opens the config in $EDITOR, or vi

mingo new makes a project with its clones and files in one go. README.md names every key.
`,
	"project add": `mingo project add <name>

Writes ~/.config/mingo/projects/<name>.toml from the template, with one role, lead, and prints its
path. A name is lowercase letters, digits and hyphens. An existing config is left as it is.
`,
	"project show": `mingo project show <name>

Prints a project's config. The name may be the project's id.
`,
	"project edit": `mingo project edit <name>

Opens a project's config in $EDITOR, or vi without one. The name may be the project's id.
`,
	"setup": `mingo setup <project>

Makes what each role needs on this machine, leaving what is there as it is:
  the clone, cloned from the project's repo with gh, then the role's .env from .env.example when
  the project has port offsets, and the new clone marked as trusted in ~/.claude.json
  the memory folder, with a MEMORY.md that points at handoff-<role>.md

Cloning needs gh logged in and a git identity.
`,
	"service": `mingo service enable|disable <project>

enable starts the project's autostart roles at boot from now on, through the systemd user unit
mingo@<project>, while lingering is on for the user. On a Mac it is at login, through the launch
agent mingo.<project>. It starts nothing now. disable undoes it. Stopping the unit stops this
project's sessions and no other's.
`,
	"apply": `mingo apply <project>

Lands the files of the project's modules in its repository through a pull request from the branch
mingo-kit, or as the first commit of an empty repository, and sets up what the modules need on the
machine and on GitHub: memory folders, labels, the ruleset, port offsets.

While the pull request is open, a later apply adds its commit on top, so what was pushed to the
branch stays. .mingo/kit.lock records each file's module, the mingo version and a hash. A file
still as mingo rendered it is replaced. A file the project changed, or had before mingo, is left
alone, and what mingo renders for it is shown. A file no module renders any more is removed while
it is still as rendered. CLAUDE.md, package.json and .claude/settings.json stay the project's:
mingo only adds its lines, wraps the heavy scripts, and sets the attribution key. A second apply
changes nothing.

With claude_files = "local", Claude's files stay out of the repository: CLAUDE.md, made of the text
mingo claude edit keeps and mingo's lines, the files it imports and .claude go straight into every
clone, and each clone's .git/info/exclude keeps them out of git. Their lock is each clone's
.git/mingo/kit.lock, and the same rules hold. A repository that has one of them is refused until it
is taken out.

apply works through the lead's clone, and with local Claude's files through every clone, so mingo
setup comes first.
`,
	"doctor": `mingo doctor <project>

Says what of the project's modules is in place and what is missing, with the command that fixes
each, and the steps only the owner can take, such as the review's token. It reads the base branch,
so a file waiting in the kit's pull request counts as missing. With claude_files = "local" it reads
Claude's files in every clone too. It exits 1 while anything is missing.
`,
	"secret": `mingo secret review <project>

Sets the review's token on the project's repository, the secret CLAUDE_CODE_OAUTH_TOKEN that the
review workflow runs with. Run claude setup-token first, then this, and paste what it printed. At a
terminal what is pasted is not shown, and without one the token is read from stdin.
`,
	"secret review": "",
	"claude": `mingo claude edit <project>

Opens the CLAUDE.md of a project whose claude_files is local in $EDITOR, or vi without one. mingo
keeps it in ~/.config/mingo/projects/<project>/CLAUDE.md, out of every repository, and mingo apply
writes it into every clone with mingo's lines below it.
`,
	"claude edit": "",
	"pr": `mingo pr merge <number>
mingo pr rebase <branch>

The lead's pull request commands, run in one of the project's clones, with the values of its
[workflow] and [tracker] tables. The project is MINGO_PROJECT's, or else the clone's. mingo help pr
merge and mingo help pr rebase say more.
`,
	"pr merge": `mingo pr merge <number>

Checks a ready pull request and squash merges it with its branch deleted, only when nothing holds
it. It holds a pull request:
  with a required check of [workflow] checks that is not green on the head
  with no review comment on the head that says no findings, from [workflow] reviewer when set
  with an unresolved thread, or a branch behind the base
  whose description does not begin with a tag of [workflow] tags, a blank line and the tracker's
  Closes line, or carries an attribution footer
Under the ci-unavailable label, the description's record of the local run, the review heading and
the head's short sha stand in for the checks and the review. It prints every held point, numbered.
The merge has an empty body, so no commit trailer of the branch reaches the base, and it names the
shared files the merge touched, for the notice to the others. It never approves, dismisses or
greens anything.
`,
	"pr rebase": `mingo pr rebase <branch>

Rebases a branch onto the base in a worktree of its own, so the clone it runs in keeps its files,
and pushes it, only over the head it fetched. A conflict in a shared list of [[workflow.shared]]
keeps both sides, or takes what one side alone made of an entry. A generated file both sides
changed is made again by the project's generators. It says whether the base brought code into files
the branch touches, which asks for the full gate again. Any other conflict stops it with the file
named and nothing pushed.
`,
	"gate": `mingo gate <log>

Runs the project's [workflow] gate commands in order, without a shell, with their output in the log,
and stops at the first red one, exiting 1. One line a command says green or red and how long it
took. A log path that is not absolute starts at the repository's root.
`,
	"install": `mingo install

Sets up this machine for mingo, and changes nothing when run again:
  copies this binary to ~/.local/bin/mingo
  writes ~/.tmux.conf when there is none
  adds the guards, deny rules for pkill and killall, and the settings sessions message each other
  with to ~/.claude/settings.json, keeping every other key
`,
	"heavy": `mingo heavy <label> -- <command...>

Runs a heavy command, such as a check, the tests or a build, in one of the machine's slots, which
every project shares. It waits while every slot is taken, or while another command holds one and
less free memory than the least is left, and a command beside another starts only after that one
has run a while. It says who holds the slots while it waits. A command started by one that holds a
slot runs at once, a slot whose command died is taken back, and SIGTERM and SIGHUP reach the
command. The label names the command to the ones that wait.

  MINGO_HEAVY_SLOTS        the slots, 2 unless set
  MINGO_HEAVY_MIN_FREE_MB  the least free memory beside another command, 5000 unless set
  MINGO_HEAVY_RAMP_S       the seconds a command runs before another starts beside it, 60
  MINGO_HEAVY_POLL_S       the seconds between looks while waiting, 5
  MINGO_HEAVY=off          runs every command at once

  mingo heavy check -- pnpm run check:run
`,
	"guard": `mingo guard write|bash

The hooks Claude Code runs before a tool call, which mingo install writes into
~/.claude/settings.json. Each reads the call on stdin and prints a denial, or nothing.

  write   lets Edit, Write and NotebookEdit write only inside the session's project, the temp
          folder outside mingo's state folder, and its memory and plan folders, by their real paths
  bash    denies a Bash or PowerShell command that ends a process by its name: pkill, killall,
          pgrep or pidof feeding a kill or a loop that kills, taskkill /IM, Stop-Process -Name
          and their kin, also inside bash -c, eval and a command substitution. It also denies
          kill -1, which ends every process.

They are guards against slips, not a sandbox.
`,
	"version": `mingo version

Prints this mingo's version. A loop starts the binary on disk again at each session boundary when it
answers this.
`,
	"help": `mingo help [command]

Explains a command, or lists them all with no command. mingo <command> --help does the same.
`,
	"loop": `mingo loop <project> <role>

What mingo start runs in each session's pane: Claude Code in the role's clone, started again
whenever a session ends. It is not for running by hand.
`,
}

func init() {
	pages["secret review"] = pages["secret"]
	pages["claude edit"] = pages["claude"]
}

func (e *env) newPage() string {
	var b strings.Builder
	b.WriteString(`mingo new <name> --preset <preset> [--<key> <value>]...

Makes a project in one go: writes its config from the preset and the answers, then sets it up as
mingo setup does and lands its files as mingo apply does. Its repository must exist on GitHub. A
flag is named after its config key. A terminal is asked for what the flags leave out, and without
one an answer with no default stops it.

  bare   sessions and guards
  solo   bare with one role, plus workflow, ci and review
  team   solo with any roles, plus team, tracker, rules and heavy
  full   every module
`)
	k, err := kit.LoadEmbedded()
	if err != nil {
		return b.String()
	}
	type flag struct{ name, says string }
	var flags []flag
	for _, always := range []bool{true, false} {
		for _, m := range k.Modules {
			if m.Always != always {
				continue
			}
			for _, a := range m.Asks {
				name := a.Flag() + " <value>"
				says := a.Question
				switch a.Each {
				case "check":
					name = a.Flag() + " <check>=<value>"
					says = strings.ReplaceAll(says, "{item}", "<check>") + ", for each check"
				case "role":
					name = a.Flag() + " <role>=<value>"
					says = strings.ReplaceAll(says, "{item}", "<role>") + ", for each role"
				}
				if a.List {
					says += ", separated by commas"
				}
				if a.Default != nil && *a.Default != "" {
					says += ", " + defaultText(*a.Default)
				}
				if a.When != "" {
					key, values, _ := strings.Cut(a.When, "=")
					says += ", when " + key + " is " + strings.ReplaceAll(values, "|", " or ")
				}
				flags = append(flags, flag{name, m.Name + ": " + says})
			}
		}
	}
	width := 0
	for _, f := range flags {
		width = max(width, len(f.name))
	}
	b.WriteString("\n")
	for _, f := range flags {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, f.name, f.says)
	}
	b.WriteString("\n  mingo new demo --preset solo --repo me/demo --ci-stack go --workflow-checks vet,test \\\n      --ci-run vet=\"go vet ./...\" --ci-run test=\"go test ./...\"\n")
	return b.String()
}

func defaultText(def string) string {
	switch def {
	case "{name}":
		return "the project's name unless set"
	case "{first role}":
		return "the first role unless set"
	case "{title}":
		return "the title unless set"
	case "{offset}":
		return "10, 20 and on in role order unless set"
	case "{read}":
		return config.DefaultRead("repo") + ", or " + config.DefaultRead("local") + " when Claude's files are local, unless set"
	}
	return def + " unless set"
}

// the page of a command, with its subcommand when it has one, or false when there is none
func (e *env) page(words []string) (string, bool) {
	if len(words) == 0 {
		return "", false
	}
	if words[0] == "new" {
		return e.newPage(), true
	}
	if len(words) > 1 {
		if text, ok := pages[words[0]+" "+words[1]]; ok && text != "" {
			return text, true
		}
	}
	text, ok := pages[words[0]]
	return text, ok
}
