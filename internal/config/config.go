// Package config loads a project's config and resolves its targets, clones and memory folders.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"

	"github.com/Lindeneg/mingo/internal/provider"
)

const (
	DefaultClone  = "~/dev/{project}/{role}"
	DefaultOpener = "Read your handoff memory, then continue."
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func IsName(s string) bool {
	return nameRe.MatchString(s)
}

func Home() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func Dir() string {
	if v := os.Getenv("MINGO_CONFIG"); v != "" {
		return v
	}
	return filepath.Join(Home(), ".config", "mingo")
}

func StateDir() string {
	if v := os.Getenv("MINGO_STATE"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.TempDir(), "mingo")
	}
	return "/tmp/mingo"
}

func File(name string) string {
	return filepath.Join(Dir(), "projects", name+".toml")
}

// with claude_files local the project's CLAUDE.md is kept here, out of every repository
func ClaudeFile(name string) string {
	return filepath.Join(Dir(), "projects", name, "CLAUDE.md")
}

// the owner's away mode is kept beside the project's config, so it outlasts a reboot
func AwayFile(name string) string {
	return filepath.Join(Dir(), "projects", name, "away")
}

func Away(name string) bool {
	return exists(AwayFile(name))
}

const (
	AwayOpener = "The owner is away, so work by Working alone in TEAM.md until told the owner is back."
	AwayTold   = "The owner is away from now on, so work by Working alone in TEAM.md until told the owner is back."
	BackTold   = "The owner is back, so Working alone in TEAM.md no longer applies, ask as TEAM.md says."
)

// TEAM.md has the rules for when the owner is away
func (p *Project) CanBeAway() bool {
	return slices.Contains(p.Modules, "team")
}

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func Names() []string {
	matches, _ := filepath.Glob(filepath.Join(Dir(), "projects", "*.toml"))
	var names []string
	for _, m := range matches {
		if exists(m) {
			names = append(names, strings.TrimSuffix(filepath.Base(m), ".toml"))
		}
	}
	sort.Strings(names)
	return names
}

type Role struct {
	Name    string   `toml:"name"`
	Aliases []string `toml:"aliases"`
	Model   string   `toml:"model"`
	Effort  string   `toml:"effort"`
	Does    string   `toml:"does"`
}

type Project struct {
	Name        string                       `toml:"-"`
	File        string                       `toml:"-"`
	ID          string                       `toml:"id"`
	Title       string                       `toml:"title"`
	Repo        string                       `toml:"repo"`
	Clone       string                       `toml:"clone"`
	Lead        string                       `toml:"lead"`
	Opener      string                       `toml:"opener"`
	Autostart   []string                     `toml:"autostart"`
	ClaudeArgs  []string                     `toml:"claude_args"`
	ClaudeFiles string                       `toml:"claude_files"`
	Roles       []Role                       `toml:"roles"`
	Workflow    Workflow                     `toml:"workflow"`
	Tracker     Tracker                      `toml:"tracker"`
	CI          CI                           `toml:"ci"`
	Review      Review                       `toml:"review"`
	Ports       Ports                        `toml:"ports"`
	Heavy       Heavy                        `toml:"heavy"`
	Modules     []string                     `toml:"modules"`
	Blocks      map[string]map[string]string `toml:"blocks"`

	argsSet bool
}

// what the kit has that a config names, the modules and the folders of the kit's own skills
type KitNames struct {
	// the modules a config may name
	Modules []string
	// the modules always on, which a config does not name
	Always []string
	// the folders the kit's own skills take under .claude/skills, which the lead's skill cannot share
	Skills []string
}

var kitNames func() (KitNames, error)

// the kit gives what it has at init, read at the first load that needs it, so a config is checked
// against it then. a program without the kit, such as this package's tests, loads without these checks.
// it returns what was given before
func RegisterKit(names func() (KitNames, error)) func() (KitNames, error) {
	before := kitNames
	kitNames = nil
	if names != nil {
		kitNames = sync.OnceValues(names)
	}
	return before
}

func Load(name string) (*Project, error) {
	if !IsName(name) {
		return nil, fmt.Errorf("a project name is lowercase letters, digits and hyphens, not %s", name)
	}
	file := File(name)
	if !exists(file) {
		return nil, fmt.Errorf("no project named %s, see mingo project add %s", name, name)
	}
	p := &Project{}
	md, err := toml.DecodeFile(file, p)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", file, err)
	}
	p.Name, p.File = name, file
	if err := p.checkKeys(md); err != nil {
		return nil, err
	}
	if err := p.checkSet(md); err != nil {
		return nil, err
	}
	p.Repo = strings.ReplaceAll(p.Repo, "{project}", name)
	if err := p.checkRepo(); err != nil {
		return nil, err
	}
	if !md.IsDefined("clone") {
		p.Clone = DefaultClone
	}
	if p.Title == "" {
		p.Title = name
	}
	if !md.IsDefined("opener") {
		p.Opener = DefaultOpener
	}
	p.argsSet = md.IsDefined("claude_args")
	switch p.ClaudeFiles {
	case "":
		p.ClaudeFiles = "repo"
	case "repo", "local":
	default:
		return nil, fmt.Errorf("%s has claude_files %s, use repo or local", file, p.ClaudeFiles)
	}
	if len(p.Roles) == 0 {
		return nil, fmt.Errorf("%s names no roles", file)
	}
	for _, r := range p.Roles {
		if r.Name == "" {
			return nil, fmt.Errorf("%s has a role without a name, give each [[roles]] its name", file)
		}
		if !IsName(r.Name) {
			return nil, fmt.Errorf("%s has a role named %q, use lowercase letters, digits and hyphens", file, r.Name)
		}
	}
	if p.Lead == "" {
		p.Lead = p.Roles[0].Name
	}
	if err := p.checkRoles(); err != nil {
		return nil, err
	}
	if err := p.checkKit(); err != nil {
		return nil, err
	}
	if err := p.workflowDefaults(md); err != nil {
		return nil, err
	}
	if p.ID != "" {
		if err := p.checkID(); err != nil {
			return nil, err
		}
	}
	if err := p.checkSessions(); err != nil {
		return nil, err
	}
	return p, nil
}

// every name a target may use points at one role, and every role has a clone of its own
func (p *Project) checkRoles() error {
	seen := map[string]bool{}
	for _, r := range p.Roles {
		if seen[r.Name] {
			return fmt.Errorf("%s has the role %s twice", p.File, r.Name)
		}
		seen[r.Name] = true
	}
	aliases := map[string]bool{}
	for _, r := range p.Roles {
		for _, a := range r.Aliases {
			switch {
			case !IsName(a):
				return fmt.Errorf("%s has the alias %q, use lowercase letters, digits and hyphens", p.File, a)
			case seen[a]:
				return fmt.Errorf("%s has the alias %s, which is a role's name", p.File, a)
			case aliases[a]:
				return fmt.Errorf("%s has the alias %s twice", p.File, a)
			}
			aliases[a] = true
		}
	}
	if !seen[p.Lead] {
		return fmt.Errorf("%s has the lead %s, which is none of its roles %s", p.File, p.Lead, strings.Join(p.RoleNames(), " "))
	}
	for _, a := range p.Autostart {
		if !seen[a] {
			return fmt.Errorf("%s autostarts %q, which is none of its roles %s", p.File, a, strings.Join(p.RoleNames(), " "))
		}
	}
	if len(p.Roles) > 1 && !strings.Contains(p.Clone, "{role}") {
		return fmt.Errorf("%s has %d roles and one clone for them all, put {role} in clone", p.File, len(p.Roles))
	}
	if !filepath.IsAbs(ExpandHome(p.Clone)) {
		return fmt.Errorf("%s has the clone %q, start it with ~/ or /", p.File, p.Clone)
	}
	return nil
}

func readID(name string) string {
	var f struct {
		ID string `toml:"id"`
	}
	if _, err := toml.DecodeFile(File(name), &f); err != nil {
		return ""
	}
	return f.ID
}

func (p *Project) checkID() error {
	if !IsName(p.ID) {
		return fmt.Errorf("%s has the id %s, use lowercase letters, digits and hyphens", p.File, p.ID)
	}
	for _, other := range Names() {
		if other == p.Name {
			continue
		}
		if other == p.ID {
			return fmt.Errorf("%s has the id %s, which is the name of another project", p.File, p.ID)
		}
		if readID(other) == p.ID {
			return fmt.Errorf("%s has the id %s, which project %s has too", p.File, p.ID, other)
		}
	}
	return nil
}

func NameOf(nameOrID string) (string, bool) {
	if nameOrID == "" {
		return "", false
	}
	if exists(File(nameOrID)) {
		return nameOrID, true
	}
	if IsName(nameOrID) {
		for _, name := range Names() {
			if readID(name) == nameOrID {
				return name, true
			}
		}
	}
	return "", false
}

func Find(nameOrID string) (*Project, error) {
	if name, ok := NameOf(nameOrID); ok {
		return Load(name)
	}
	return Load(nameOrID)
}

func Known(nameOrID string) bool {
	_, ok := NameOf(nameOrID)
	return ok
}

// CLAUDE.md, the files it imports and .claude live in each clone and out of git
func (p *Project) ClaudeLocal() bool {
	return p.ClaudeFiles == "local"
}

// the review on GitHub sees only what the repository has
func DefaultRead(claudeFiles string) string {
	if claudeFiles == "local" {
		return "README.md first"
	}
	return "CLAUDE.md first"
}

func (p *Project) RoleNames() []string {
	names := make([]string, len(p.Roles))
	for i, r := range p.Roles {
		names[i] = r.Name
	}
	return names
}

func (p *Project) Role(name string) *Role {
	for i := range p.Roles {
		if p.Roles[i].Name == name {
			return &p.Roles[i]
		}
	}
	return nil
}

func (p *Project) HasRole(name string) bool {
	return p.Role(name) != nil
}

func (p *Project) Resolve(part string) (string, error) {
	if p.HasRole(part) {
		return part, nil
	}
	for _, r := range p.Roles {
		for _, a := range r.Aliases {
			if a == part {
				return r.Name, nil
			}
		}
	}
	return "", fmt.Errorf("%s has no role or alias %s, it has %s", p.Name, part, strings.Join(p.RoleNames(), " "))
}

var notKeyRe = regexp.MustCompile(`[^A-Z0-9]`)

func (p *Project) EnvKey(role string) string {
	return notKeyRe.ReplaceAllString(strings.ToUpper(p.Name+"_"+role), "_")
}

func (p *Project) Model(role string) string {
	if v := os.Getenv("MINGO_MODEL_" + p.EnvKey(role)); v != "" {
		return v
	}
	if r := p.Role(role); r != nil {
		return r.Model
	}
	return ""
}

func (p *Project) Effort(role string) string {
	if v := os.Getenv("MINGO_EFFORT_" + p.EnvKey(role)); v != "" {
		return v
	}
	if r := p.Role(role); r != nil {
		return r.Effort
	}
	return ""
}

func ExpandHome(path string) string {
	if path == "~" {
		return Home()
	}
	if strings.HasPrefix(path, "~/") {
		return Home() + path[1:]
	}
	return path
}

func (p *Project) CloneDir(role string) string {
	dir := strings.ReplaceAll(ExpandHome(p.Clone), "{project}", p.Name)
	return filepath.Clean(strings.ReplaceAll(dir, "{role}", role))
}

// the clone as the config names it, with the home folder left as ~
func (p *Project) CloneShown(role string) string {
	return strings.ReplaceAll(strings.ReplaceAll(p.Clone, "{project}", p.Name), "{role}", role)
}

func (p *Project) SessionName(role string) string {
	return p.Name + "-" + role
}

func (p *Project) State() string {
	return filepath.Join(StateDir(), p.Name)
}

func (p *Project) MemoryDir(role string) string {
	return p.Provider().MemoryDir(Home(), p.CloneDir(role))
}

// the provider of the agent CLI the project's sessions run
func (p *Project) Provider() provider.Provider {
	prov, ok := provider.Get(provider.Default)
	if !ok {
		panic("mingo has no provider " + provider.Default)
	}
	return prov
}

// the args a session passes, the project's own or else its provider's
func (p *Project) Args() []string {
	if p.argsSet {
		return p.ClaudeArgs
	}
	return p.Provider().DefaultArgs()
}

func (p *Project) OpenerFor(role string) string {
	text := strings.ReplaceAll(p.Opener, "{project}", p.Name)
	return strings.ReplaceAll(text, "{role}", strings.ReplaceAll(role, "-", " "))
}

func (p *Project) DescribeRun(role string) string {
	model, effort := p.Model(role), p.Effort(role)
	if model == "" {
		model = "the default model"
	}
	if effort == "" {
		effort = "default"
	}
	return model + ", effort " + effort
}

func (p *Project) Autostarts(role string) bool {
	for _, r := range p.Autostart {
		if r == role {
			return true
		}
	}
	return false
}

func inside(here, dir string) bool {
	return here == dir || strings.HasPrefix(here, dir+"/")
}

func (p *Project) RoleOfDir(here string) (string, bool) {
	for _, r := range p.Roles {
		if inside(here, p.CloneDir(r.Name)) {
			return r.Name, true
		}
	}
	return "", false
}

// git names a repository by its real path, so a clone is compared through its links too
func (p *Project) HoldsDir(dir string) bool {
	real := func(path string) string {
		if r, err := filepath.EvalSymlinks(path); err == nil {
			return r
		}
		return path
	}
	for _, r := range p.Roles {
		if clone := p.CloneDir(r.Name); inside(dir, clone) || inside(real(dir), real(clone)) {
			return true
		}
	}
	return false
}

// a project whose config fails to load is still named for a folder in its clones, so the load that
// follows says what is wrong with it
func ProjectOfDir(here string) (string, bool) {
	broken := ""
	for _, name := range Names() {
		p, err := Load(name)
		if err != nil {
			if broken == "" && clonesHold(name, here) {
				broken = name
			}
			continue
		}
		if _, ok := p.RoleOfDir(here); ok {
			return name, true
		}
	}
	return broken, broken != ""
}

func Cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return os.Getenv("PWD")
	}
	return dir
}

func DefaultFile() string {
	return filepath.Join(Dir(), "default")
}

func DefaultProject() (string, error) {
	if v := os.Getenv("MINGO_PROJECT"); v != "" {
		return v, nil
	}
	if name, ok := ProjectOfDir(Cwd()); ok {
		return name, nil
	}
	if f, err := os.Open(DefaultFile()); err == nil {
		defer f.Close()
		if info, err := f.Stat(); err == nil && info.Size() > 0 {
			line, _ := bufio.NewReader(f).ReadString('\n')
			return strings.TrimSuffix(line, "\n"), nil
		}
	}
	names := Names()
	if len(names) != 1 {
		return "", errors.New("which project? name it, or set one with mingo default <project>")
	}
	return names[0], nil
}

func ParseTarget(spec string) (*Project, string, error) {
	var name, part string
	if i := strings.Index(spec, "/"); i >= 0 {
		name, part = spec[:i], spec[i+1:]
	} else if Known(spec) {
		name = spec
	} else {
		var err error
		if name, err = DefaultProject(); err != nil {
			return nil, "", err
		}
		part = spec
	}
	p, err := Find(name)
	if err != nil {
		return nil, "", err
	}
	if part == "" {
		return p, "", nil
	}
	role, err := p.Resolve(part)
	if err != nil {
		return nil, "", err
	}
	return p, role, nil
}

func ParseRoleTarget(spec string) (*Project, string, error) {
	p, role, err := ParseTarget(spec)
	if err != nil {
		return nil, "", err
	}
	if role == "" {
		role = p.Lead
	}
	return p, role, nil
}
