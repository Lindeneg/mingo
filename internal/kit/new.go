package kit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/provider"
	"github.com/Lindeneg/mingo/internal/team"
)

type Ask struct {
	Key      string   `toml:"key"`
	Question string   `toml:"question"`
	Default  *string  `toml:"default"`
	List     bool     `toml:"list"`
	Each     string   `toml:"each"`
	When     string   `toml:"when"`
	Choices  []Choice `toml:"choices"`
}

// one answer of a question whose answers are few, with what it means
type Choice struct {
	Value string `toml:"value"`
	Hint  string `toml:"hint"`
}

func (a Ask) Flag() string {
	return "--" + strings.NewReplacer(".", "-", "_", "-").Replace(a.Key)
}

func (a Ask) Values() []string {
	values := make([]string, len(a.Choices))
	for i, c := range a.Choices {
		values[i] = c.Value
	}
	return values
}

func (a Ask) hints() []string {
	hints := make([]string, len(a.Choices))
	for i, c := range a.Choices {
		hints[i] = c.Hint
	}
	return hints
}

var Presets = []string{"bare", "solo", "team", "full"}

// what each of Presets holds, in its order
var PresetHints = []string{
	"sessions and guards",
	"bare with one role, plus workflow, ci and review",
	"solo with any roles, plus skills, team, tracker, rules, and heavy for Node",
	"every module, heavy only for Node",
}

const presetQuestion = "the preset"

func (k *Kit) Preset(name string) ([]string, error) {
	switch name {
	case "bare":
		return []string{"guards"}, nil
	case "solo":
		return []string{"guards", "workflow", "ci", "review"}, nil
	case "team":
		return []string{"guards", "workflow", "ci", "review", "skills", "team", "tracker", "rules", "heavy"}, nil
	case "full":
		var all []string
		for _, m := range k.Modules {
			if !m.Always {
				all = append(all, m.Name)
			}
		}
		return all, nil
	}
	return nil, fmt.Errorf("%q is not one of %s", name, joinWith(Presets, "or"))
}

// asks at a terminal what the flags leave out
type Asker interface {
	Choose(question string, values, hints []string, def, problem string) (string, error)
	Text(question, def, input, problem string, check func(string) error) (string, error)
}

type NewDeps struct {
	// nil without a terminal, when the flags and the defaults are all there is
	Ask Asker
	// runs gh to check the repository and its package.json
	Run team.Runner
	// takes what mingo new says between its questions
	Out io.Writer
}

// the config mingo new made and what it sets up
type Made struct {
	TOML    string
	Name    string
	Repo    string
	Roles   []string
	Modules []string
	Local   bool
}

type answers struct {
	values map[string]string
	each   map[string][][2]string
}

func split(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (a *answers) list(key string) []string {
	return split(a.values[key])
}

func (a *answers) fill(text string, item string, index int) string {
	first := ""
	if roles := a.list("roles"); len(roles) > 0 {
		first = roles[0]
	}
	return strings.NewReplacer(
		"{item}", item,
		"{name}", a.values["name"],
		"{title}", a.values["title"],
		"{offset}", strconv.Itoa((index+1)*10),
		"{read}", config.DefaultRead(config.DefaultProvider(), a.values["claude_files"]),
		"{first role}", first,
	).Replace(text)
}

func gate(ask Ask) (key string, values []string) {
	key, list, _ := strings.Cut(ask.When, "=")
	return key, strings.Split(list, "|")
}

func (a *answers) applies(ask Ask) bool {
	if ask.When == "" {
		return true
	}
	key, values := gate(ask)
	return contains(values, a.values[key])
}

// the items a per role or per check ask goes through, where the security check runs no command
func itemsOf(each string, list []string) []string {
	if each == "role" {
		return list
	}
	return without(list, "security")
}

// the ask whose answer lists the items of a per role or per check ask
func source(each string) string {
	if each == "role" {
		return "roles"
	}
	return "workflow.checks"
}

func (a *answers) items(ask Ask) []string {
	return itemsOf(ask.Each, a.list(source(ask.Each)))
}

// "a, b and c" with word as the last joint
func joinWith(items []string, word string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " " + word + " " + items[len(items)-1]
}

// flags mingo new cannot read, a mistake in the command line rather than in an answer
type FlagError struct{ error }

func (f FlagError) Unwrap() error { return f.error }

// a check's error that answering again cannot put right, so the question ends with it
type final struct{ error }

func (final) Final() bool { return true }

func (f final) Unwrap() error { return f.error }

type newRun struct {
	k       *Kit
	d       NewDeps
	preset  string
	modules []string
	a       *answers
	byFlag  map[string]Ask
	owner   map[string]Module
	single  map[string]string
	each    map[string]map[string]string
	order   map[string][]string
	scripts map[string]bool
}

// the provider of the project being made
func (r *newRun) provider() provider.Provider {
	return config.DefaultProvider()
}

// the flags as they were given, trimmed, each named once unless it is per check or per role
func (r *newRun) readFlags(flags map[string][]string) error {
	for _, flag := range sortedKeys(flags) {
		values := append([]string(nil), flags[flag]...)
		ask, known := r.byFlag[flag]
		if !known && flag != "--preset" {
			return fmt.Errorf("mingo new knows no %s, mingo help new lists its flags", flag)
		}
		for i := range values {
			values[i] = strings.TrimSpace(values[i])
			if values[i] == "" {
				return fmt.Errorf("%s has an empty value", flag)
			}
		}
		if ask.Each == "" {
			if len(values) > 1 {
				return fmt.Errorf("%s is given %d times, give it once", flag, len(values))
			}
			r.single[flag] = values[0]
			continue
		}
		r.each[flag] = map[string]string{}
		for _, v := range values {
			item, value, ok := strings.Cut(v, "=")
			item, value = strings.TrimSpace(item), strings.TrimSpace(value)
			if !ok || item == "" || value == "" {
				return fmt.Errorf("%s takes <%s>=<value>, not %q", flag, ask.Each, v)
			}
			if _, twice := r.each[flag][item]; twice {
				return fmt.Errorf("%s names %s twice", flag, item)
			}
			r.each[flag][item] = value
			r.order[flag] = append(r.order[flag], item)
		}
	}
	return nil
}

func (r *newRun) given(flag string) bool {
	_, single := r.single[flag]
	_, each := r.each[flag]
	return single || each
}

func (r *newRun) flagOf(key string) string {
	for flag, ask := range r.byFlag {
		if ask.Key == key {
			return flag
		}
	}
	return "--" + key
}

// the flag's error when its ask does not apply, since the answer it hangs on is another
func (r *newRun) notFor(ask Ask, value string) error {
	key, values := gate(ask)
	return fmt.Errorf("%s is only for %s %s, not %s", ask.Flag(), r.flagOf(key), joinWith(values, "or"), value)
}

// the flag's error when it names an item there is none of
func (r *newRun) unknownItems(ask Ask, items []string) error {
	flag := ask.Flag()
	for _, item := range r.order[flag] {
		if contains(items, item) {
			continue
		}
		if len(items) == 0 {
			return fmt.Errorf("%s names %s, and there is no %s to name", flag, item, ask.Each)
		}
		return fmt.Errorf("%s names %s, which is none of the %ss %s", flag, item, ask.Each, joinWith(items, "and"))
	}
	return nil
}

func (r *newRun) choosePreset() (string, error) {
	problem := ""
	if v, given := r.single["--preset"]; given {
		_, err := r.k.Preset(v)
		switch {
		case err == nil:
			return v, nil
		case r.d.Ask == nil:
			return "", fmt.Errorf("--preset: %w", err)
		}
		problem = err.Error() + ", pick one below"
	}
	if r.d.Ask == nil {
		return "", errors.New("mingo new needs --preset, one of " + strings.Join(Presets, " "))
	}
	return r.d.Ask.Choose(presetQuestion, Presets, PresetHints, "", problem)
}

// the project's config from the preset, the flags and, at a terminal, the answers to what is left
func (k *Kit) New(name string, flags map[string][]string, d NewDeps) (Made, error) {
	if d.Out == nil {
		d.Out = io.Discard
	}
	r := &newRun{
		k: k, d: d, a: &answers{values: map[string]string{"name": name}, each: map[string][][2]string{}},
		byFlag: map[string]Ask{}, owner: map[string]Module{},
		single: map[string]string{}, each: map[string]map[string]string{}, order: map[string][]string{},
	}
	for _, m := range k.Modules {
		for _, ask := range m.Asks {
			r.byFlag[ask.Flag()], r.owner[ask.Flag()] = ask, m
		}
	}
	if err := r.readFlags(flags); err != nil {
		return Made{}, FlagError{err}
	}
	preset, err := r.choosePreset()
	if err != nil {
		return Made{}, err
	}
	r.preset = preset
	if r.modules, err = k.Preset(preset); err != nil {
		return Made{}, err
	}
	var asks []Ask
	for _, always := range []bool{true, false} {
		for _, m := range k.Modules {
			if m.Always == always && (always || contains(r.modules, m.Name)) {
				asks = append(asks, m.Asks...)
			}
		}
	}
	if err := r.early(asks); err != nil {
		return Made{}, err
	}
	for _, ask := range asks {
		if err := r.ask(ask); err != nil {
			return Made{}, err
		}
	}
	if stack := r.a.values["ci.stack"]; (stack != "pnpm" && stack != "npm") || r.scripts == nil {
		r.modules = without(r.modules, "heavy")
		delete(r.a.values, "heavy.scripts")
	}
	if r.a.values["tracker.kind"] == "none" {
		r.modules = without(r.modules, "tracker")
		delete(r.a.values, "tracker.kind")
	}
	if contains(r.modules, "review") {
		r.a.values["workflow.reviewer"] = r.provider().Kit().Review.Bot
	}
	text, err := r.a.toml(name, preset, r.modules)
	if err != nil {
		return Made{}, err
	}
	return Made{
		TOML: text, Name: name, Repo: r.a.values["repo"], Roles: r.a.list("roles"), Modules: r.modules,
		Local: r.a.values["claude_files"] == "local",
	}, nil
}

func without(list []string, s string) []string {
	var out []string
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// what the flags alone already get wrong, before anything is asked, judged by the flags whose own
// values hold
func (r *newRun) early(asks []Ask) error {
	flags := map[string]bool{}
	for flag := range r.single {
		flags[flag] = true
	}
	for flag := range r.each {
		flags[flag] = true
	}
	for _, flag := range sortedKeys(flags) {
		if m, ok := r.owner[flag]; ok && !m.Always && !contains(r.modules, m.Name) {
			return fmt.Errorf("%s belongs to the %s module, which the %s preset leaves out", flag, m.Name, r.preset)
		}
	}
	holding := func(key string) (string, bool) {
		flag := r.flagOf(key)
		v, ok := r.single[flag]
		return v, ok && r.check(r.byFlag[flag], v, true) == nil
	}
	for _, ask := range asks {
		if !r.given(ask.Flag()) {
			continue
		}
		if ask.When != "" {
			key, values := gate(ask)
			if v, ok := holding(key); ok && !contains(values, v) {
				return r.notFor(ask, v)
			}
		}
		if ask.Each != "" {
			if v, ok := holding(source(ask.Each)); ok {
				if err := r.unknownItems(ask, itemsOf(ask.Each, split(v))); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *newRun) ask(ask Ask) error {
	if !r.a.applies(ask) {
		if r.given(ask.Flag()) {
			key, _ := gate(ask)
			return r.notFor(ask, r.a.values[key])
		}
		return nil
	}
	if ask.Key == "heavy.scripts" {
		ok, err := r.manifest()
		if err != nil {
			return err
		}
		if !ok {
			if r.given(ask.Flag()) {
				return fmt.Errorf("%s names scripts, but %s has no %s on its default branch", ask.Flag(), r.a.values["repo"], Manifest)
			}
			fmt.Fprintf(r.d.Out, "%s has no %s on its default branch, so heavy is left out\n", r.a.values["repo"], Manifest)
			return nil
		}
	}
	if ask.Each == "" {
		v, err := r.answer(ask, "", 0)
		r.a.values[ask.Key] = v
		return err
	}
	items := r.a.items(ask)
	if err := r.unknownItems(ask, items); err != nil {
		return err
	}
	for i, item := range items {
		v, err := r.answer(ask, item, i)
		if err != nil {
			return err
		}
		r.a.each[ask.Key] = append(r.a.each[ask.Key], [2]string{item, v})
	}
	return nil
}

// the answer from the flag when it holds, else from the terminal, else the default
func (r *newRun) answer(ask Ask, item string, index int) (string, error) {
	question := r.a.fill(ask.Question, item, index)
	def := ""
	if ask.Default != nil {
		def = r.a.fill(*ask.Default, item, index)
	}
	check := func(v string) error { return r.check(ask, v, ask.Default != nil) }
	flag := ask.Flag()
	v, given := r.single[flag]
	if item != "" {
		v, given = r.each[flag][item]
		flag += " " + item
	}
	input, problem := "", ""
	if given {
		err := check(v)
		switch {
		case err == nil:
			return v, nil
		case r.d.Ask == nil || isFinal(err):
			return "", flagError(flag, err)
		}
		input, problem = v, err.Error()
	}
	if r.d.Ask == nil {
		if ask.Default == nil {
			if item != "" {
				flag = ask.Flag() + " " + item + "=..."
			}
			return "", fmt.Errorf("mingo new needs %s, %s", flag, question)
		}
		if err := check(def); err != nil {
			return "", fmt.Errorf("%w, so give %s", err, ask.Flag())
		}
		return def, nil
	}
	if len(ask.Choices) > 0 {
		if problem != "" {
			problem += ", pick one below"
		}
		return r.d.Ask.Choose(question, ask.Values(), ask.hints(), def, problem)
	}
	return r.d.Ask.Text(question, def, input, problem, check)
}

func isFinal(err error) bool {
	var f final
	return errors.As(err, &f)
}

func flagError(flag string, err error) error {
	if isFinal(err) {
		return err
	}
	return fmt.Errorf("%s: %w", flag, err)
}

var (
	repoForm = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*/[A-Za-z0-9._-]+$`)
	jobID    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
)

// owner and name as GitHub allows them, without the .git GitHub takes off a new name
func isRepo(s string) bool {
	_, name, _ := strings.Cut(s, "/")
	return repoForm.MatchString(s) && name != "." && name != ".." && !strings.HasSuffix(name, ".git")
}

// what the gate runs is split on spaces and run without a shell
var shellSyntax = []string{"&&", "||", "|", ";", ">", "<", "`", "$(", `"`, "'"}

// the project and role whose tmux session a role of the new project would share
func sessionTaken(name, role string) (string, string, bool) {
	session := name + "-" + role
	for _, other := range config.Names() {
		if other == name || !strings.HasPrefix(session, other+"-") {
			continue
		}
		var f struct {
			Roles []struct {
				Name string `toml:"name"`
			} `toml:"roles"`
		}
		if _, err := toml.DecodeFile(config.File(other), &f); err != nil {
			continue
		}
		for _, theirs := range f.Roles {
			if other+"-"+theirs.Name == session {
				return other, theirs.Name, true
			}
		}
	}
	return "", "", false
}

// the problem with an answer, said so that the one who gave it can put it right
func (r *newRun) check(ask Ask, v string, hasDefault bool) error {
	if v == "" {
		if hasDefault {
			return nil
		}
		return errors.New("this needs an answer")
	}
	if len(ask.Choices) > 0 {
		if !contains(ask.Values(), v) {
			return fmt.Errorf("%q is not one of %s", v, joinWith(ask.Values(), "or"))
		}
		return nil
	}
	var items []string
	if ask.List {
		for _, item := range strings.Split(v, ",") {
			if item = strings.TrimSpace(item); item == "" {
				return fmt.Errorf("%q has an empty item, separate the items with single commas", v)
			}
			items = append(items, item)
		}
	}
	twice := func() string {
		seen := map[string]bool{}
		for _, item := range items {
			if seen[item] {
				return item
			}
			seen[item] = true
		}
		return ""
	}
	switch ask.Key {
	case "repo":
		switch {
		case isRepo(v):
			return r.repoExists(v)
		case strings.Contains(v, "github.com") || strings.HasSuffix(v, ".git"):
			return fmt.Errorf("%q is not owner/name, such as you/shop, without https://github.com/ or .git", v)
		}
		return fmt.Errorf("%q is not owner/name, such as you/shop", v)
	case "roles":
		for _, role := range items {
			if !config.IsName(role) {
				return fmt.Errorf("%q is not a role name, use lowercase letters, digits and hyphens", role)
			}
		}
		if role := twice(); role != "" {
			return fmt.Errorf("the role %s is named twice", role)
		}
		if r.preset == "solo" && len(items) != 1 {
			return fmt.Errorf("the solo preset has one role, not %d", len(items))
		}
		for _, role := range items {
			if other, theirs, taken := sessionTaken(r.a.values["name"], role); taken {
				return fmt.Errorf("the role %s makes the tmux session %s-%s, which the project %s has for its role %s, rename the role", role, r.a.values["name"], role, other, theirs)
			}
		}
		// the lead's skill is in the skills folder under its name, so a role named like one of the kit's skills cannot lead
		skills, reserved := r.k.skillFolders(), 0
		for _, role := range items {
			if contains(skills, role) {
				reserved++
			}
		}
		switch {
		case reserved < len(items):
		case len(items) == 1:
			return fmt.Errorf("the role %s cannot lead, the lead's skill would take %s/%s, the folder of the kit's own %s skill, rename the role", items[0], r.provider().Kit().Skills, items[0], items[0])
		default:
			return fmt.Errorf("none of the roles %s can lead, the lead's skill would take the folder of one of the kit's own skills in %s, rename one", joinWith(items, "and"), r.provider().Kit().Skills)
		}
	case "workflow.gate":
		for _, c := range items {
			for _, s := range shellSyntax {
				if strings.Contains(c, s) {
					return fmt.Errorf("%q is a gate command, which mingo runs without a shell, so %s does not work there, put the line in a package script or a script file", c, s)
				}
			}
		}
	case "lead":
		if roles := r.a.list("roles"); !contains(roles, v) {
			return fmt.Errorf("%q is none of the roles %s", v, joinWith(roles, "and"))
		}
		if contains(r.k.skillFolders(), v) {
			return fmt.Errorf("%s cannot lead, the lead's skill would take %s/%s, the folder of the kit's own %s skill, pick another role", v, r.provider().Kit().Skills, v, v)
		}
	case "workflow.checks":
		for _, c := range items {
			if !jobID.MatchString(c) {
				return fmt.Errorf("%q is not a GitHub job id, start it with a letter or _ and use only letters, digits, - and _", c)
			}
		}
		if c := twice(); c != "" {
			return fmt.Errorf("the check %s is named twice", c)
		}
	case "ports.offsets":
		if n, err := strconv.Atoi(v); err != nil || n < 0 {
			return fmt.Errorf("%q is not a whole number", v)
		}
	case "heavy.scripts":
		var missing []string
		for _, s := range items {
			if !r.scripts[s] && !contains(missing, s) {
				missing = append(missing, s)
			}
		}
		switch len(missing) {
		case 0:
		case 1:
			return fmt.Errorf("the %s of %s has no script %s", Manifest, r.a.values["repo"], missing[0])
		default:
			return fmt.Errorf("the %s of %s has no scripts %s", Manifest, r.a.values["repo"], joinWith(missing, "or"))
		}
	}
	return nil
}

func (r *newRun) gh(args ...string) (team.Result, error) {
	res, err := r.d.Run(team.Options{Timeout: ghTimeout}, "gh", args...)
	switch {
	case err != nil:
		return res, final{err}
	case res.NotFound:
		return res, final{errors.New("gh, the GitHub CLI, is not installed, and mingo new checks the repository with it, see https://cli.github.com")}
	case res.Code == 4:
		return res, final{errors.New("gh is not logged in, run gh auth login")}
	case res.TimedOut:
		return res, fmt.Errorf("gh %s did not finish in %d seconds", args[0], int(ghTimeout.Seconds()))
	}
	return res, nil
}

func lastLine(text string) string {
	return team.LastLines(text, 1)
}

func (r *newRun) repoExists(repo string) error {
	res, err := r.gh("repo", "view", repo, "--json", "name")
	switch {
	case err != nil:
		return err
	case res.Code == 0:
		return nil
	case strings.Contains(res.Stderr, "Could not resolve to a Repository"):
		return fmt.Errorf("there is no repository %s on GitHub, or gh's account cannot see it", repo)
	}
	return fmt.Errorf("gh repo view %s failed, %s", repo, lastLine(res.Stderr+res.Stdout))
}

// reads the scripts of the repository's package.json on its default branch once, and says
// whether it has one
func (r *newRun) manifest() (bool, error) {
	if r.scripts != nil {
		return true, nil
	}
	repo := r.a.values["repo"]
	res, err := r.gh("api", "-H", "Accept: application/vnd.github.raw+json", "repos/"+repo+"/contents/"+Manifest)
	switch {
	case err != nil:
		return false, err
	case res.Code != 0 && strings.Contains(res.Stderr, "HTTP 404"):
		return false, nil
	case res.Code != 0:
		return false, fmt.Errorf("gh could not read the %s of %s, %s", Manifest, repo, lastLine(res.Stderr))
	}
	var manifest struct {
		Scripts map[string]any `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &manifest); err != nil {
		return false, fmt.Errorf("the %s of %s does not read as JSON, %v", Manifest, repo, err)
	}
	r.scripts = map[string]bool{}
	for name, cmd := range manifest.Scripts {
		if _, ok := cmd.(string); ok {
			r.scripts[name] = true
		}
	}
	return true, nil
}

// what mingo new does once it goes on, said before it asks to
func (m Made) Next(file string) string {
	p := &config.Project{Name: m.Name, Clone: config.DefaultClone}
	var clones, kept []string
	for _, role := range m.Roles {
		if info, err := os.Stat(p.CloneDir(role)); err == nil && info.IsDir() {
			kept = append(kept, p.CloneShown(role))
		} else {
			clones = append(clones, p.CloneShown(role))
		}
	}
	var b strings.Builder
	b.WriteString("\nmingo new will\n")
	fmt.Fprintf(&b, "  write the config %s\n", file)
	if len(clones) > 0 {
		fmt.Fprintf(&b, "  clone %s into %s\n", m.Repo, joinWith(clones, "and"))
	}
	switch len(kept) {
	case 0:
	case 1:
		fmt.Fprintf(&b, "  use the clone already in %s\n", kept[0])
	default:
		fmt.Fprintf(&b, "  use the clones already in %s\n", joinWith(kept, "and"))
	}
	fmt.Fprintf(&b, "  open a pull request on %s with the kit's files\n", m.Repo)
	if m.Local {
		b.WriteString("  write Claude's files into each clone, out of git\n")
	}
	return b.String()
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(s string) string {
	if bareKey.MatchString(s) {
		return s
	}
	return tomlString(s)
}

// a TOML basic string, whose escapes are fewer than Go's
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = tomlString(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func (a *answers) toml(name, preset string, modules []string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# mingo project %s, made by mingo new with the preset %s\n\n", name, preset)
	fmt.Fprintf(&b, "repo = %s\n", tomlString(a.values["repo"]))
	if t := a.values["title"]; t != "" && t != name {
		fmt.Fprintf(&b, "title = %s\n", tomlString(t))
	}
	fmt.Fprintf(&b, "lead = %s\n", tomlString(a.values["lead"]))
	if v := a.values["claude_files"]; v != "" && v != "repo" {
		fmt.Fprintf(&b, "claude_files = %s\n", tomlString(v))
	}
	fmt.Fprintf(&b, "modules = %s\n", tomlList(modules))
	type entry struct{ key, value string }
	tables := []struct {
		name    string
		entries []entry
	}{
		{"workflow", []entry{{"base", "workflow.base"}, {"checks", "workflow.checks"}, {"gate", "workflow.gate"}, {"tags", "workflow.tags"}, {"reviewer", "workflow.reviewer"}}},
		{"ci", []entry{{"stack", "ci.stack"}, {"setup", "ci.setup"}, {"audit", "ci.audit"}}},
		{"review", []entry{{"read", "review.read"}}},
		{"tracker", []entry{{"kind", "tracker.kind"}, {"team", "tracker.team"}, {"project", "tracker.project"}}},
		{"heavy", []entry{{"scripts", "heavy.scripts"}}},
		{"ports", []entry{{"variable", "ports.variable"}}},
	}
	lists := map[string]bool{"workflow.checks": true, "workflow.gate": true, "workflow.tags": true, "ci.setup": true, "heavy.scripts": true}
	for _, t := range tables {
		var lines []string
		for _, e := range t.entries {
			v, ok := a.values[e.value]
			if !ok || v == "" {
				continue
			}
			if lists[e.value] {
				lines = append(lines, e.key+" = "+tomlList(a.list(e.value)))
			} else {
				lines = append(lines, e.key+" = "+tomlString(v))
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(&b, "\n[%s]\n%s\n", t.name, strings.Join(lines, "\n"))
		}
		sub := map[string]string{"ci": "ci.run", "ports": "ports.offsets"}[t.name]
		if items := a.each[sub]; len(items) > 0 {
			fmt.Fprintf(&b, "\n[%s]\n", sub)
			for _, it := range items {
				if sub == "ports.offsets" {
					n, err := strconv.Atoi(it[1])
					if err != nil {
						return "", fmt.Errorf("the port offset of %s is %s, not a number", it[0], it[1])
					}
					fmt.Fprintf(&b, "%s = %d\n", tomlKey(it[0]), n)
				} else {
					fmt.Fprintf(&b, "%s = %s\n", tomlKey(it[0]), tomlString(it[1]))
				}
			}
		}
	}
	for _, role := range a.list("roles") {
		fmt.Fprintf(&b, "\n[[roles]]\nname = %s\n", tomlString(role))
	}
	return b.String(), nil
}
