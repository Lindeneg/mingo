package kit

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/ojson"
	"github.com/Lindeneg/mingo/internal/team"
)

func init() {
	setups["ruleset"] = makeRuleset
	setups["ports"] = func(d Deps) error {
		for _, role := range d.Project.RoleNames() {
			if err := Ports(d.Project, role, d.Out); err != nil {
				return err
			}
		}
		return nil
	}
	checks["ruleset"] = func(d Deps, r *report) {
		repo, err := d.repo()
		if err != nil {
			r.add(false, "%v", err)
			return
		}
		name, _, err := d.ruleset()
		if err != nil {
			r.add(false, "%v", err)
			return
		}
		what := fmt.Sprintf("the ruleset %s on %s", name, repo)
		have, err := d.names("api", "repos/"+repo+"/rulesets")
		switch {
		case err != nil && noRulesets(err.Error()):
			r.note("not available: %s, %s", what, noRulesetsWhy)
		case err != nil:
			r.unchecked(what, err)
		default:
			r.add(contains(have, name), "%s", what)
		}
	}
	checks["ports"] = func(d Deps, r *report) {
		p := d.Project
		for _, role := range p.RoleNames() {
			offset, ok := p.Ports.Offsets[role]
			if !ok {
				continue
			}
			want := p.Ports.Variable + "=" + strconv.Itoa(offset)
			dir := p.CloneDir(role)
			data, err := os.ReadFile(filepath.Join(dir, ".env"))
			if err == nil && hasLine(string(data), want) {
				r.add(true, "%s's .env with %s", role, want)
				continue
			}
			fix := ""
			if errors.Is(err, fs.ErrNotExist) {
				if _, err := os.Stat(dir); err != nil {
					fix = fmt.Sprintf("mingo setup %s", p.Name)
				} else if _, err := os.Stat(filepath.Join(dir, ".env.example")); errors.Is(err, fs.ErrNotExist) {
					fix = fmt.Sprintf("add .env.example with an empty %s= line, then mingo apply %s", p.Ports.Variable, p.Name)
				}
			}
			r.miss(fix, "%s's .env with %s", role, want)
		}
	}
	fixes["ruleset"] = "mingo apply %s"
	fixes["ports"] = "mingo apply %s"
}

// the module's ruleset, with {base} in the branches it targets set to the project's base branch
func (d Deps) ruleset() (string, []byte, error) {
	data, err := fs.ReadFile(d.Kit.fsys, "rules/ruleset.json")
	if err != nil {
		return "", nil, err
	}
	v, err := ojson.Parse(data)
	root, isObject := v.(*ojson.Object)
	if err != nil || !isObject {
		return "", nil, errors.New("rules/ruleset.json is not a JSON object")
	}
	name, _ := root.Get("name")
	ref := root.Child("conditions").Child("ref_name")
	include, _ := ref.Get("include")
	list, _ := include.([]any)
	for i, item := range list {
		if text, ok := item.(string); ok {
			list[i] = strings.ReplaceAll(text, "{base}", d.Project.Workflow.Base)
		}
	}
	ref.Set("include", list)
	body, err := ojson.MarshalIndent(root, ojson.Indent(data))
	if err != nil {
		return "", nil, err
	}
	text, _ := name.(string)
	return text, body, nil
}

const noRulesetsWhy = "GitHub has rulesets on a private repository only with GitHub Pro, Team or Enterprise Cloud"

// GitHub answers a 403 that asks for an upgrade when the repository's plan has no rulesets
func noRulesets(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "http 403") && strings.Contains(text, "upgrade")
}

func makeRuleset(d Deps) error {
	repo, err := d.repo()
	if err != nil {
		return err
	}
	name, data, err := d.ruleset()
	if err != nil {
		return err
	}
	have, err := d.names("api", "repos/"+repo+"/rulesets")
	if err != nil && noRulesets(err.Error()) {
		fmt.Fprintf(d.Out, "ruleset: not made on %s, %s\n", repo, noRulesetsWhy)
		return nil
	}
	if err != nil {
		return err
	}
	if contains(have, name) {
		return nil
	}
	r, err := d.Run(team.Options{Dir: d.TempRoot, Timeout: ghTimeout, Stdin: string(data)}, "gh", "api", "-X", "POST", "repos/"+repo+"/rulesets", "--input", "-")
	if err != nil {
		return err
	}
	if !r.OK() {
		if noRulesets(r.Stderr + r.Stdout) {
			fmt.Fprintf(d.Out, "ruleset: not made on %s, %s\n", repo, noRulesetsWhy)
			return nil
		}
		return fmt.Errorf("gh api could not make the ruleset:\n%s", team.LastLines(r.Stderr+r.Stdout, shownErrors))
	}
	fmt.Fprintf(d.Out, "ruleset: %s on %s\n", name, repo)
	return nil
}

func hasLine(text, line string) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimRight(l, "\r") == line {
			return true
		}
	}
	return false
}

// writes the role's port offset into its clone's .env, made from .env.example when the clone has no .env,
// and otherwise with every other line of it kept
func Ports(p *config.Project, role string, out io.Writer) error {
	offset, ok := p.Ports.Offsets[role]
	if !ok {
		return nil
	}
	dir := p.CloneDir(role)
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	want := p.Ports.Variable + "=" + strconv.Itoa(offset)
	env := filepath.Join(dir, ".env")
	text, mode := "", fs.FileMode(0o600)
	if old, err := os.ReadFile(env); err == nil {
		next, changed := setLine(string(old), p.Ports.Variable+"=", want)
		if !changed {
			return nil
		}
		if info, err := os.Stat(env); err == nil {
			mode = info.Mode().Perm()
		}
		text = next
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	} else {
		example, err := os.ReadFile(filepath.Join(dir, ".env.example"))
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(out, "ports: leaves out %s, the clone has no .env.example yet, and mingo doctor reports it until it has\n", env)
			return nil
		}
		if err != nil {
			return err
		}
		empty := p.Ports.Variable + "="
		lines := strings.Split(string(example), "\n")
		found := false
		for i, l := range lines {
			if strings.TrimRight(l, "\r") == empty {
				lines[i] = strings.Replace(l, empty, want, 1)
				found = true
			}
		}
		if !found {
			return fmt.Errorf("no empty %s line in %s/.env.example", p.Ports.Variable, dir)
		}
		text = strings.Join(lines, "\n")
	}
	draft := filepath.Join(dir, ".env.draft")
	if err := os.WriteFile(draft, []byte(text), mode); err != nil {
		return err
	}
	if err := os.Chmod(draft, mode); err != nil {
		return err
	}
	if err := os.Rename(draft, env); err != nil {
		return err
	}
	fmt.Fprintf(out, "ports: %s/.env, %s\n", dir, want)
	return nil
}

// sets every line that starts with prefix to line, or adds line at the end when none does, and says whether the text changed
func setLine(text, prefix, line string) (string, bool) {
	lines := strings.Split(text, "\n")
	found, changed := false, false
	for i, l := range lines {
		bare := strings.TrimRight(l, "\r")
		if !strings.HasPrefix(bare, prefix) {
			continue
		}
		found = true
		if bare != line {
			lines[i] = line + l[len(bare):]
			changed = true
		}
	}
	if found {
		return strings.Join(lines, "\n"), changed
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + line + "\n", true
}

const Manifest = "package.json"

// an owned step whose file the repository does not have yet, so mingo leaves its part out until it has
const Absent Action = "absent"

// npm takes the flags after a script's name for its own unless a double dash comes first, while pnpm passes them all on
func shim(pm, script string) string {
	line := "node tools/heavy.mjs " + script + " -- " + pm + " run " + script + ":run"
	if pm == "npm" {
		line += " --"
	}
	return line
}

// the npm line without its double dash is mingo's own too, so apply moves it over
func isShim(cmd, script string) bool {
	return cmd == shim("pnpm", script) || cmd == shim("npm", script) || cmd == strings.TrimSuffix(shim("npm", script), " --")
}

// package.json is the project's own file, so mingo only moves each heavy script behind the shim
func PlanScripts(mods []Module, p *config.Project, read func(path string) ([]byte, bool)) ([]Step, error) {
	used := false
	for _, m := range mods {
		used = used || m.Name == "heavy"
	}
	if !used || len(p.Heavy.Scripts) == 0 {
		return nil, nil
	}
	old, ok := read(Manifest)
	if !ok {
		return []Step{{File: File{Path: Manifest, Module: "heavy"}, Action: Absent, Owned: true, What: "the heavy scripts " + strings.Join(p.Heavy.Scripts, ", ")}}, nil
	}
	v, err := ojson.Parse(old)
	root, isObject := v.(*ojson.Object)
	if err != nil || !isObject {
		return nil, fmt.Errorf("%s is not a JSON object", Manifest)
	}
	pm := "pnpm"
	if p.CI.Stack == "npm" {
		pm = "npm"
	}
	scripts, _ := root.Vals("scripts")
	changed := false
	for _, s := range p.Heavy.Scripts {
		var have any
		if scripts != nil {
			have, _ = scripts.Get(s)
		}
		cmd, isText := have.(string)
		if !isText {
			return nil, fmt.Errorf("%s has no script %s, which %s names in [heavy] scripts", Manifest, s, p.File)
		}
		// a shim of either package manager is mingo's own, with the project's command already moved behind it
		if isShim(cmd, s) {
			if cmd != shim(pm, s) {
				scripts.Set(s, shim(pm, s))
				changed = true
			}
			continue
		}
		if run, ok := scripts.Get(s + ":run"); ok && run != cmd {
			text, _ := run.(string)
			return nil, fmt.Errorf("%s has a script %s:run of its own, %q, so mingo cannot move %s, %q, there, rename %s:run, then run mingo apply %s again", Manifest, s, text, s, cmd, s, p.Name)
		}
		scripts.InsertAfter(s, s+":run", cmd)
		scripts.Set(s, shim(pm, s))
		changed = true
	}
	step := Step{File: File{Path: Manifest, Module: "heavy", Text: string(old)}, Action: Same, Old: old, Owned: true, What: "the heavy scripts"}
	if changed {
		next, err := ojson.MarshalIndent(root, ojson.Indent(old))
		if err != nil {
			return nil, err
		}
		step.File.Text, step.Action = string(next), Update
	}
	return []Step{step}, nil
}

const Settings = ".claude/settings.json"

// Claude Code's own setting for no attribution in commits, pull requests and their session links
func attributionOff() *ojson.Object {
	o := ojson.NewObject()
	o.Set("commit", "")
	o.Set("pr", "")
	o.Set("sessionUrl", false)
	return o
}

func attributionIsOff(v any) bool {
	o, ok := v.(*ojson.Object)
	if !ok {
		return false
	}
	commit, _ := o.Get("commit")
	pr, _ := o.Get("pr")
	session, _ := o.Get("sessionUrl")
	return commit == "" && pr == "" && session == false
}

// the project's settings are its own, so mingo only sets the attribution key in them
func PlanSettings(read func(path string) ([]byte, bool)) ([]Step, error) {
	old, ok := read(Settings)
	root := ojson.NewObject()
	if ok {
		v, err := ojson.Parse(old)
		o, isObject := v.(*ojson.Object)
		if err != nil || !isObject {
			return nil, fmt.Errorf("%s is not a JSON object", Settings)
		}
		root = o
	}
	step := Step{File: File{Path: Settings, Module: "sessions", Text: string(old)}, Action: Same, Old: old, Owned: true, What: "the attribution setting"}
	if have, set := root.Get("attribution"); set && attributionIsOff(have) {
		return []Step{step}, nil
	}
	root.Set("attribution", attributionOff())
	next, err := ojson.Marshal(root)
	if err != nil {
		return nil, err
	}
	step.File.Text, step.Action = string(next), Update
	if !ok {
		step.Action = Add
	}
	return []Step{step}, nil
}
