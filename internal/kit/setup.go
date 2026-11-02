package kit

import (
	"encoding/json"
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
		have, err := d.names("api", "repos/"+repo+"/rulesets")
		r.add(err == nil && contains(have, name), "the ruleset %s on %s", name, repo)
	}
	checks["ports"] = func(d Deps, r *report) {
		p := d.Project
		for _, role := range p.RoleNames() {
			offset, ok := p.Ports.Offsets[role]
			if !ok {
				continue
			}
			want := p.Ports.Variable + "=" + strconv.Itoa(offset)
			data, err := os.ReadFile(filepath.Join(p.CloneDir(role), ".env"))
			r.add(err == nil && hasLine(string(data), want), "%s's .env with %s", role, want)
		}
	}
	fixes["ruleset"] = "mingo apply %s"
	fixes["ports"] = "mingo apply %s"
}

func (d Deps) ruleset() (string, []byte, error) {
	data, err := fs.ReadFile(d.Kit.fsys, "rules/ruleset.json")
	if err != nil {
		return "", nil, err
	}
	var r struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", nil, err
	}
	return r.Name, data, nil
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

// writes a clone's .env from its .env.example with the role's port offset, unless it has one
func Ports(p *config.Project, role string, out io.Writer) error {
	offset, ok := p.Ports.Offsets[role]
	if !ok {
		return nil
	}
	dir := p.CloneDir(role)
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		return nil
	}
	example, err := os.ReadFile(filepath.Join(dir, ".env.example"))
	if errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(dir); err != nil {
			return nil
		}
		return fmt.Errorf("no %s/.env.example to make %s/.env from", dir, dir)
	}
	if err != nil {
		return err
	}
	empty := p.Ports.Variable + "="
	want := empty + strconv.Itoa(offset)
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
	draft := filepath.Join(dir, ".env.draft")
	if err := os.WriteFile(draft, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		return err
	}
	if err := os.Rename(draft, filepath.Join(dir, ".env")); err != nil {
		return err
	}
	fmt.Fprintf(out, "ports: %s/.env, %s\n", dir, want)
	return nil
}

const Manifest = "package.json"

func shim(p *config.Project, script string) string {
	pm := "pnpm"
	if p.CI.Stack == "npm" {
		pm = "npm"
	}
	return "node tools/heavy.mjs " + script + " -- " + pm + " run " + script + ":run"
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
		return nil, fmt.Errorf("the heavy module wraps the scripts of %s, which the repository does not have", Manifest)
	}
	v, err := ojson.Parse(old)
	root, isObject := v.(*ojson.Object)
	if err != nil || !isObject {
		return nil, fmt.Errorf("%s is not a JSON object", Manifest)
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
		if cmd == shim(p, s) {
			continue
		}
		scripts.InsertAfter(s, s+":run", cmd)
		scripts.Set(s, shim(p, s))
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
