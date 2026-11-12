// Package provider says what mingo needs of the agent CLI a project's sessions run, and keeps the providers mingo has by name.
package provider

import (
	"io"
	"os"
	"sort"
	"time"

	"github.com/Lindeneg/mingo/internal/ojson"
)

// the provider of a project that names none
const Default = "claude"

// what a session starts with
type Session struct {
	Opener string
	Name   string
	Model  string
	Effort string
	Args   []string
}

type Provider interface {
	// the name a config gives it
	Name() string
	// the CLI's name as people read it
	Title() string
	// where the CLI's install is explained
	Site() string
	// the command a session runs
	Command() string
	// the variable that names another command in its place, such as a stand in
	CommandEnv() string
	// the args a project that sets none passes
	DefaultArgs() []string
	// the args that start a session, each part used as far as the CLI takes it
	Args(s Session) []string
	// the keys typed into a session's pane to end it
	ExitKeys() []string
	// the exit keys as a message names them
	ExitShown() string
	// the folder a clone's memory lives in
	MemoryDir(home, clone string) string
	// the file in the memory folder the CLI reads first, which mingo points at the handoff, empty for none
	MemoryIndex() string
	// what the kit writes for the CLI into a project
	Kit() Kit
}

type Kit struct {
	// the file at the root of a clone the CLI reads first
	Instructions string
	// the line in the instructions file that brings in another file, %s standing for its path
	Import string
	// the folder of the CLI's own files in a clone
	Dir string
	// the folder the CLI reads skills from
	Skills string
	// the commit footers and trailers mingo pr merge refuses unless a project sets its own
	Footers  []string
	Trailers []string
	// where a connector such as Linear's is added for the sessions
	Connectors string
	// the texts the kit's templates take from the CLI, as text/template definitions named agent.<name>
	Words string
	// the templates the CLI gives a module, by module and then by path
	Files map[string]map[string]string
	// the review on GitHub, empty for a CLI without one
	Review Review
}

type Review struct {
	// the repository secret its token is kept in
	Secret string
	// the login its comments come from
	Bot string
	// the command that prints a token
	Token string
}

// a CLI with a setting in the clone that turns attribution off
type Attribution interface {
	// the settings file in the clone
	AttributionFile() string
	// turns attribution off in the settings, and says whether it was off already
	AttributionOff(settings *ojson.Object) bool
}

// a CLI that says who it is logged in as, the text written to out, and an error when it is not logged in
type Login interface {
	Login(command string, out io.Writer) error
}

type Live struct {
	Name    string
	Pid     int
	State   string
	Started time.Time
}

// a CLI that lists its live sessions and links to them
type Lister interface {
	Live(command string) []Live
	// the session's remote link, empty for none
	Link(home string, pid int) string
}

// a CLI with settings mingo install writes, such as the guards' hooks
type Installer interface {
	Install(home string, out io.Writer) error
	// whether the settings are in place, and the line doctor names them by
	Installed(home string) (bool, string)
}

// a CLI that asks before it works in a folder it does not know
type Truster interface {
	Trust(home, dir string) error
	Trusted(home, dir string) bool
}

type WriteCall struct {
	// the files the call writes, more than one for a patch
	Paths []string
	Cwd   string
}

type CommandCall struct {
	Command    string
	PowerShell bool
}

// a CLI whose hooks run mingo's guards
type Guarded interface {
	// the write in a hook's input, false when it writes nothing
	WriteCall(input []byte) (WriteCall, bool)
	// the command in a hook's input, false when it runs none
	CommandCall(input []byte) (CommandCall, bool)
	// what the hook prints to deny the call
	Deny(reason string) string
	// the variable that names the session's project folder
	ProjectVar() string
	// the folders outside the project a session may write to
	WriteDirs(home, root string) []string
	// those folders as a denial names them
	WriteDirsShown() string
}

var known = map[string]Provider{}

func Register(p Provider) {
	known[p.Name()] = p
}

func Get(name string) (Provider, bool) {
	p, ok := known[name]
	return p, ok
}

func Names() []string {
	var names []string
	for name := range known {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// the command a session runs, and whether the provider's variable named it
func CommandOf(p Provider) (string, bool) {
	if v := os.Getenv(p.CommandEnv()); v != "" {
		return v, true
	}
	return p.Command(), false
}
