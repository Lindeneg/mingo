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
no role, the project's lead. With hosts, the command runs on the host that has the project, as mingo
help host says.
`

// a loop runs on the config it loaded, so the commands that control one reach it without a config
// that loads
const asWritten = `
While a project's config does not load, its running loops go on, and a target written as
<project>/<role> still reaches the session by those names, the role as written and not as an alias.
`

var pages = map[string]string{
	"ls": `mingo ls

Lists every project with its id and repository, then each role with its clone, the lead marked.
A project whose config does not load shows its error in its place, and ls exits 1.
`,
	"agents": `mingo agents [project...]

Shows each role's session: its state, how long it has been up, and its remote link, Claude Code's
Remote Control. The state is stopped, starting, between (one session has ended and the next has not
started), or what the agent CLI says of the session, such as busy or idle. With no project, every
project. A project whose config does not load shows its error in its place, and agents exits 1.
`,
	"status": `mingo status [project...]

Says of each role whether its session is running, between sessions or not running, and the model and
effort it runs on or would start on, then the agent CLI's login on this machine. With no project,
every project. A project whose config does not load shows its error in its place, and status exits
1.
`,
	"start": `mingo start [--autostart] [target...]

Starts a tmux session per role. Each runs the role's loop: the project's agent CLI in the role's
clone with the project's opener, started again whenever a session ends, so a fresh session goes on
from the handoff. A project starts every role, a role starts alone, and a role already running is
left as it is. With no target, the default project. Every target is checked before any session
starts, and nothing starts while the agent CLI is not installed.

  --autostart   a project starts only the roles its autostart names, as the boot service does

Each role starts on the model and effort of its config. MINGO_MODEL_<PROJECT>_<ROLE> and
MINGO_EFFORT_<PROJECT>_<ROLE>, such as MINGO_MODEL_ACME_DEV, change them for the sessions
this start launches.

  mingo start acme
  mingo start acme/dev
` + targets,
	"stop": `mingo stop [target...]

Asks each session to exit, waits up to 90 seconds for them all, then closes the tmux session of any
still there. A project stops every role. With no target, the default project. Every target is
checked before any session is asked. MINGO_STOP_WAIT changes the wait, in seconds.
` + targets + asWritten + `A project whose config does not load stops each role that has run a session on this machine.
`,
	"restart": `mingo restart [--force] <target>

Ends a role's session where it stands: the loop types the agent CLI's exit, /exit for Claude Code,
within seconds and starts a fresh session, which goes on from the handoff. The session's context is
lost, so its last handoff is what the fresh one starts from.

  --force   for a session a question or a long turn holds: Ctrl+C first, then the exit, and the
            session's process is ended when that does not end it
` + targets + asWritten,
	"attach": `mingo attach <target>

Attaches this terminal to a role's session. Detach with Ctrl+B then D. Ctrl+C reaches the session,
and twice, or Ctrl+D, ends it.
` + targets + asWritten,
	"ro": `mingo ro <target>

Attaches this terminal to a role's session read only: what is typed does not reach it. Detach with
Ctrl+B then D.
` + targets + asWritten,
	"peek": `mingo peek <target> [lines]

Prints the last lines of a role's screen, 40 unless lines says otherwise.

  mingo peek acme/dev 80
` + targets + asWritten,
	"say": `mingo say <target> <message>

Types the message into a role's session and presses Enter, as if it were typed there. Between two
sessions nothing is typed, since it would be the next session's first input: try again in a few
seconds.

  mingo say acme/dev "pull master"
` + targets + asWritten,
	"clear": `mingo clear

For a session to run as its last tool call: the role's loop ends the session within seconds and
starts a fresh one, which goes on from the handoff. Write the handoff first, since the exit the loop
types ends the session where it stands. The project and role come from MINGO_PROJECT and MINGO_ROLE,
which the loop sets, and then the config is not read, so a config that does not load does not stop
it. Else they come from the clone the command runs in. With no session of the role running, it says
so and changes nothing.
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

Sets the default project, or prints the one set with no project named, and says so when none is
set or the one set no longer exists. A target that leaves the project out takes MINGO_PROJECT first,
then the project of the clone the command runs in, then the default, then the one project when there
is only one. With hosts the default is this computer's own and may be a project of any host.
`,
	"project": `mingo project add|show|edit <name>

A project's config, a TOML file in ~/.config/mingo/projects. MINGO_CONFIG moves the folder.

  add    writes a config from the template, to fill in with edit, with hosts on the host --host
         names, which may be left out with one host
  show   prints the config
  edit   opens the config in $EDITOR, or vi

mingo new makes a project with its clones and files in one go. README.md names every key. A config
with a key mingo does not know, or a value it cannot use, is refused with the file, the key and the
value named.
`,
	"project add": `mingo project add <name>

Writes ~/.config/mingo/projects/<name>.toml from the template, with one role, lead, and prints its
path. A name is lowercase letters, digits and hyphens. An existing config is left as it is.
`,
	"project show": `mingo project show <name>

Prints a project's config. The name may be the project's id.
`,
	"project edit": `mingo project edit <name>

Opens a project's config in $EDITOR, or vi without one. The name may be the project's id. When the
editor closes the config is loaded again, and what keeps it from loading is shown, with the file
kept as it was left.
`,
	"setup": `mingo setup <project>

Makes what each role needs on this machine, leaving what is there as it is:
  the clone, cloned from the project's repo with gh
  the clone marked as trusted where the agent CLI keeps that, ~/.claude.json for Claude Code, then
  the role's .env from .env.example when the project has port offsets, or the offset line set in
  the .env it has, for a clone that was there before too
  the memory folder, with the index the CLI reads first, MEMORY.md for Claude Code, pointing at
  handoff-<role>.md, and the pointer put first in an index that lacks it

Cloning needs gh and git installed, gh logged in and a git identity.
`,
	"service": `mingo service enable|disable <project>

enable starts the project's autostart roles at boot from now on, through the systemd user unit
mingo@<project>, while lingering is on for the user, and says whether it is. Without lingering they
start when the user logs in, and loginctl enable-linger turns it on. On a Mac it is at login,
through the launch agent mingo.<project>. It starts nothing now. disable undoes it and says so.
Stopping the unit stops this project's sessions and no other's.
`,
	"apply": `mingo apply <project>

Lands the files of the project's modules in its repository through a pull request from the branch
mingo-kit, or as the first commit of an empty repository, and sets up what the modules need on the
machine and on GitHub: memory folders, labels, the ruleset on the base branch, and each role's port
offset in its clone's .env.

Before it changes anything it checks the modules and the files they render, the lead's clone with
its remote origin and base branch, the lock, the files the repository already has, and with
agent_files = "local" every clone, so a mistake there changes nothing. Then it sets up the memory
folders, the port offsets, the ruleset and the labels, in that order, pushes, and writes local
agent files into the clones, with the ruleset after the push on an empty repository. A step that
fails stops apply there, with the steps before it done, and mingo doctor shows what is still missing.

While the pull request is open, a later apply adds its commit on top, so what was pushed to the
branch stays. .mingo/kit.lock records each file's module, the mingo version and a hash. A file
still as mingo rendered it is replaced. A file the project changed, or had before mingo, is left
alone, and what mingo renders for it is shown. A file no module renders any more is removed while
it is still as rendered. The instructions file, package.json and the agent CLI's settings, CLAUDE.md
and .claude/settings.json for Claude Code, stay the project's: mingo only adds its lines, wraps the
heavy scripts, and sets the attribution key. A second apply changes nothing.

Until the repository has a package.json, mingo leaves the heavy scripts out, and until a clone has
an .env or an .env.example, its port offset. On a private repository whose plan has no rulesets,
which takes GitHub Pro, Team or Enterprise Cloud, it leaves the ruleset out. It says so each time,
and mingo doctor reports the scripts and the offset as missing and the ruleset as not available.

With agent_files = "local", the agent files stay out of the repository: the instructions file, made
of the text mingo instructions edit keeps and mingo's lines, the files it imports and the CLI's
folder, .claude for Claude Code, go straight into every clone, and each clone's .git/info/exclude
keeps them out of git. Their lock is each clone's .git/mingo/kit.lock, and the same rules hold. A
repository that has one of them is refused until it is taken out.

apply works through the lead's clone, and with local agent files through every clone, so mingo
setup comes first.
`,
	"doctor": `mingo doctor <project>

Says what of the project's modules is in place and what is missing, with the command that fixes
each, and the steps only the owner can take, such as the review's token. It reads the base branch,
so a file waiting in the kit's pull request counts as missing. With agent_files = "local" it reads
the agent files in every clone too. Where package.json has Prettier, it names each of mingo's files
that neither .gitignore nor .prettierignore keeps out of it, since a format would rewrite them and
break their hashes. What it cannot find out, such as the labels while gh is logged out, it names
with gh's error and counts as neither. It exits 1 while anything is missing or could not be checked.
`,
	"secret": `mingo secret review <project>

Sets the review's token on the project's repository, the secret the review workflow runs with,
CLAUDE_CODE_OAUTH_TOKEN for Claude Code. Run the provider's token command first, claude setup-token
for Claude Code, then this, and paste what it printed. At a terminal what is pasted is not shown,
and without one the token is read from stdin.
`,
	"secret review": "",
	"host": `mingo host add <name> <destination>
mingo host add <name> --local
mingo host ls
mingo host rm <name>

The machines mingo drives from this computer, kept in ~/.config/mingo/hosts.toml. A host is this
machine itself, or one reached over ssh by a destination, user@server or a Host of ~/.ssh/config,
which must take a key without a prompt. Without hosts mingo works on this machine alone. A host is
Linux or macOS.

add with a destination checks that ssh reaches it and that it is Linux or macOS, finds Go there,
installs this mingo's own version with go install, and runs that mingo's install, which puts it in
~/.local/bin and sets the machine up as mingo install says. Adding the host again installs again. A
mingo built from uncommitted changes cannot be installed, since a host builds it from GitHub. add
--local adds this machine, which can be one host only.

ls lists each host with its destination and the version of its mingo, or what kept ssh from
asking. rm takes a host off the list and leaves mingo installed there.

With hosts, a command for a project runs on the host that has it, with its output and exit code,
and attach, ro, new, project add and edit, instructions edit and secret review get this terminal.
A project's config, clones and sessions stay on its host. Which host has which project is kept in
mingo's state folder and asked again when a host no longer has it, and a project name on two hosts
is refused. ls, agents and status ask every host at once, or the hosts of the projects named, and
put the host first on each line. new and project add take --host, which may be left out with one
host. Each call carries this mingo's version, and a host on another one gets this one installed
first. What runs inside a session, clear, heavy, gate, pr and guard, stays on the host.
`,
	"host add": "",
	"host ls":  "",
	"host rm":  "",
	"instructions": `mingo instructions edit <project>

Opens the instructions file of a project whose agent_files is local in $EDITOR, or vi without one,
CLAUDE.md for Claude Code. mingo keeps it in ~/.config/mingo/projects/<project>/, out of every
repository, and mingo apply writes it into every clone with mingo's lines below it.
`,
	"instructions edit": "",
	"pr": `mingo pr merge <number>
mingo pr rebase <branch>

The lead's pull request commands, run in one of the project's clones, with the values of its
[workflow] and [tracker] tables. The project is MINGO_PROJECT's, or else the clone's. mingo help pr
merge and mingo help pr rebase say more.
`,
	"pr merge": `mingo pr merge <number>

Checks a ready pull request and squash merges it with its branch deleted, only when nothing holds
it. It first checks that gh is logged in to github.com with the account it uses there, and shows
what gh says when it is not. It holds a pull request:
  with a required check of [workflow] checks that is not green on the head
  with no review comment on the head that says no findings, from [workflow] reviewer when set
  with an unresolved thread, or a branch behind the base
  whose description does not begin with a tag of [workflow] tags, a blank line and the tracker's
  Closes line, or carries an attribution footer
Under the ci-unavailable label, the description's record of the local run, the review heading and
the head's short sha stand in for the checks and the review. It prints every held point, numbered.
The merge has an empty body, so no commit trailer of the branch reaches the base, and it names the
shared files the merge touched, for the notice to the others. When the merge went through but
deleting the branch failed, it says merged and shows gh's error, for the branch to be deleted by
hand. It never approves, dismisses or greens anything.
`,
	"pr rebase": `mingo pr rebase <branch>

Rebases a branch onto the base in a worktree of its own, so the clone it runs in keeps its files,
and pushes it, only over the head it fetched. A conflict in a shared list of [[workflow.shared]]
keeps both sides, or takes what one side alone made of an entry. A generated file both sides
changed is made again by the project's generators. They run in that worktree, a fresh checkout
without the dependencies installed in the clone, so they must run without them. It says whether
the base brought code into files the branch touches, which asks for the full gate again. Any other
conflict stops it with the file named and nothing pushed. Ctrl+C, SIGTERM or SIGHUP stops it at
once, removes its worktree and exits non-zero.
`,
	"gate": `mingo gate <log>

Runs the project's [workflow] gate commands in order, without a shell, with their output in the log,
and stops at the first red one, exiting 1. One line a command says green or red and how long it
took. A command still running after 30 minutes is red, and it is ended with every process it
started. A log path that is not absolute starts at the repository's root. A gate command with &&,
a pipe, a redirect or quotes is refused when the config loads: such a line goes in a package script
or a script file.
`,
	"install": `mingo install

Sets up this machine for mingo, and changes nothing when run again:
  copies this binary to ~/.local/bin/mingo
  writes ~/.tmux.conf when there is none
  writes each agent CLI's settings. For Claude Code it adds the guards, deny rules for pkill and
  killall, and the settings sessions message each other with to ~/.claude/settings.json, keeping
  every other key, and prints each key it changed with its old and new value. A settings.json that
  is a link is written where the link points. A guard an older mingo wrote with mingo's path
  unquoted is quoted, since sh splits the path at a space, and one without its provider gets it.
On a computer whose hosts are all reached over ssh it copies the binary alone, since the sessions
run on the hosts.
`,
	"heavy": `mingo heavy <label> -- <command...>

Runs a heavy command, such as a check, the tests or a build, in one of the machine's slots, which
every project shares. A command that is not on PATH fails at once, before any wait. It waits while
every slot is taken, or while another command holds one and less free memory than the least is
left, and a command beside another starts only after that one has run a while. It says which of
these it waits for and who holds the slots. A command started by one that holds a slot runs at
once, and SIGTERM and SIGHUP reach the command. A slot is held while mingo heavy or its command
runs, so a command that outlives a killed mingo heavy keeps its slot, and a slot whose command died
is taken back. On Linux the command is sent SIGTERM when mingo heavy is killed. The label names the
command to the ones that wait.

  MINGO_HEAVY_SLOTS        the slots, 2 unless set
  MINGO_HEAVY_MIN_FREE_MB  the least free memory beside another command, 5000 unless set
  MINGO_HEAVY_RAMP_S       the seconds a command runs before another starts beside it, 60
  MINGO_HEAVY_POLL_S       the seconds between looks while waiting, 5
  MINGO_HEAVY=off          runs every command at once

  mingo heavy check -- pnpm run check:run
`,
	"guard": `mingo guard <provider> write|bash

The hooks an agent CLI runs before a tool call, which mingo install writes into the CLI's settings,
~/.claude/settings.json for Claude Code, whose provider is claude. Each reads the call on stdin and
prints a denial, or nothing. A hook an older mingo wrote names no provider, and is Claude Code's.

  write   lets Edit, Write and NotebookEdit write only inside the session's project, the temp
          folder (and /tmp on macOS) outside mingo's state folder, and its memory and plan
          folders, by their real paths
  bash    denies a Bash or PowerShell command that ends a process by its name: pkill, killall,
          pgrep or pidof by name feeding a kill or a loop that kills, kill given a name, also
          through xargs or a loop, taskkill /IM, Stop-Process -Name and their kin, also inside
          bash -c, eval, ssh, sudo and the like, and a command substitution. It also denies
          kill -1, which ends every process.

They are guards against slips, not a sandbox.
`,
	"version": `mingo version

Prints this mingo's version. A loop starts the binary on disk again at each session boundary when it
answers this and takes the config, as mingo help loop says.
`,
	"help": `mingo help [command]

Explains a command, or lists them all with no command. --help or -h among a command's arguments
does the same, before any --.
`,
	"loop": `mingo loop <project> <role>
mingo loop --check <project> <role>

What mingo start runs in each session's pane: the project's agent CLI in the role's clone, started
again whenever a session ends. A session that ends within a minute by itself is a failed start, and
the wait before the next one doubles, up to five minutes. A session that mingo restart or mingo
clear ended is no failed start. mingo stop ends a loop that waits between sessions within a second.

At each session boundary the loop hands over to the mingo on disk, so a rebuilt mingo takes over,
but only when that mingo answers mingo version and takes the config. --check asks it: it prints
what keeps it from running the loop on the config and exits 1, or exits 0. A mingo too old to
answer --check is not handed over to.

When the project's config does not load, the loop says why in its pane: at its start it tries
again every few seconds, and at a session boundary it goes on with the config it loaded and tries
again at the next one. When the CLI cannot be found, it says so and looks again every few seconds.
It is not for running by hand.
`,
}

const awayText = `away says the owner is away. Each running session of the project is told at once and each fresh
session's opener says so, and the sessions then work by the Working alone section of TEAM.md, which
takes the recommended default instead of asking. back says the owner is back, and the running
sessions are told so. A session between two is told by its next opener. The mode is kept in
~/.config/mingo/projects/<project>/away, so it outlasts a reboot, and it needs the team module,
whose TEAM.md has the rules.
`

func init() {
	pages["secret review"] = pages["secret"]
	pages["instructions edit"] = pages["instructions"]
	for _, action := range []string{"add", "ls", "rm"} {
		pages["host "+action] = pages["host"]
	}
	pages["away"] = "mingo away <project>\nmingo back <project>\n\n" + awayText
	pages["back"] = "mingo back <project>\nmingo away <project>\n\n" + awayText
}

func (e *env) newPage() string {
	var b strings.Builder
	b.WriteString(`mingo new <name> --preset <preset> [--<key> <value>]...

Makes a project in one go: writes its config from the preset and the answers, then sets it up as
mingo setup does and lands its files as mingo apply does. Its repository must exist on GitHub, which
mingo new checks with gh before it writes anything. A flag is named after its config key and is
given as --key value or --key=value.

At a terminal mingo new asks for what the flags leave out, with a list to pick from where the
answers are few, checks each answer as it is given and asks again when it is wrong, then says what
it will do and asks to continue. Until then Ctrl+C stops it with nothing written. Without a terminal
the flags and the defaults are all it has, and a wrong or missing answer stops it before it writes
anything. With hosts, --host names the host the project goes on, and may be left out with one host.
`)
	for i, p := range kit.Presets {
		fmt.Fprintf(&b, "\n  %-5s  %s", p, kit.PresetHints[i])
	}
	b.WriteString(`

heavy is left out on a stack other than pnpm or npm, or when the repository's default branch has no
package.json, and each heavy script must be one of its scripts. tracker is left out with the tracker
none.
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
				if values := a.Values(); len(values) > 0 {
					says += ", one of " + strings.Join(values[:len(values)-1], ", ") + " or " + values[len(values)-1]
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
		prov := config.DefaultProvider()
		return config.DefaultRead(prov, "repo") + ", or " + config.DefaultRead(prov, "local") + " when the agent files are local, unless set"
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
