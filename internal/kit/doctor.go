package kit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/install"
	"github.com/Lindeneg/mingo/internal/team"
)

var setups = map[string]func(Deps) error{
	"memory": seedMemory,
	"labels": makeLabels,
}

type label struct {
	name, description, color string
}

func wantedLabels(d Deps) []label {
	p := d.Project
	var out []label
	if contains(p.Modules, "workflow") {
		out = append(out, label{CIUnavailable, "Merged on the local checks and a local review while Actions could not run", "F2994A"})
	}
	if contains(p.Modules, "tracker") && p.Tracker.Kind == "github" {
		for _, r := range p.Roles {
			text := r.Does
			if text == "" {
				text = "the issues of the " + strings.ReplaceAll(r.Name, "-", " ")
			}
			if len(text) > 100 {
				text = text[:97] + "..."
			}
			out = append(out, label{r.Name, text, "1D76DB"})
		}
	}
	return out
}

const CIUnavailable = "ci-unavailable"

const ReviewSecret = "CLAUDE_CODE_OAUTH_TOKEN"

func (d Deps) repo() (string, error) {
	if d.Project.Repo == "" {
		return "", fmt.Errorf("%s names no repo", d.Project.Name)
	}
	return d.Project.Repo, nil
}

func (d Deps) names(args ...string) ([]string, error) {
	out, err := d.gh(d.TempRoot, args...)
	if err != nil {
		return nil, err
	}
	var items []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		return nil, fmt.Errorf("gh %s did not answer with a list", args[0])
	}
	var names []string
	for _, i := range items {
		names = append(names, i.Name)
	}
	return names, nil
}

func makeLabels(d Deps) error {
	repo, err := d.repo()
	if err != nil {
		return err
	}
	have, err := d.names("label", "list", "-R", repo, "--limit", "500", "--json", "name")
	if err != nil {
		return err
	}
	for _, l := range wantedLabels(d) {
		if contains(have, l.name) {
			continue
		}
		if _, err := d.gh(d.TempRoot, "label", "create", l.name, "-R", repo, "--description", l.description, "--color", l.color); err != nil {
			return err
		}
		fmt.Fprintf(d.Out, "label: %s on %s\n", l.name, repo)
	}
	return nil
}

func handoffLine(role string) string {
	return "- [Handoff](handoff-" + role + ".md): read first, where the work stands and the next step\n"
}

func seedMemory(d Deps) error {
	for _, role := range d.Project.RoleNames() {
		file, made, err := SeedRole(d.Project, role)
		if err != nil {
			return err
		}
		if made {
			fmt.Fprintf(d.Out, "memory: %s\n", file)
		}
	}
	return nil
}

// makes the role's memory folder and a MEMORY.md that points at its handoff, unless one is there
func SeedRole(p *config.Project, role string) (string, bool, error) {
	dir := p.MemoryDir(role)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	file := filepath.Join(dir, "MEMORY.md")
	if _, err := os.Stat(file); err == nil {
		return file, false, nil
	}
	return file, true, os.WriteFile(file, []byte(handoffLine(role)), 0o644)
}

type finding struct {
	ok    bool
	text  string
	owner bool
}

type report struct {
	findings []finding
	missing  int
}

func (r *report) add(ok bool, format string, args ...any) {
	r.findings = append(r.findings, finding{ok: ok, text: fmt.Sprintf(format, args...)})
	if !ok {
		r.missing++
	}
}

var checks = map[string]func(Deps, *report){
	"clones": func(d Deps, r *report) {
		p := d.Project
		for _, role := range p.RoleNames() {
			dir := p.CloneDir(role)
			info, err := os.Stat(dir)
			r.add(err == nil && info.IsDir(), "the clone of %s, %s", role, dir)
		}
	},
	"memory": func(d Deps, r *report) {
		p := d.Project
		for _, role := range p.RoleNames() {
			file := filepath.Join(p.MemoryDir(role), "MEMORY.md")
			data, err := os.ReadFile(file)
			r.add(err == nil && bytes.Contains(data, []byte("handoff-"+role+".md")), "%s's MEMORY.md pointing at handoff-%s.md", role, role)
		}
	},
	"guards": func(d Deps, r *report) {
		file := filepath.Join(d.Home, ".claude", "settings.json")
		data, err := os.ReadFile(file)
		ok := false
		if err == nil {
			before, after, err := install.MergeSettings(data, d.Home)
			ok = err == nil && bytes.Equal(before, after)
		}
		r.add(ok, "the write and kill guards in %s", file)
	},
}

func init() {
	checks["labels"] = func(d Deps, r *report) {
		repo, err := d.repo()
		if err != nil {
			r.add(false, "%v", err)
			return
		}
		have, err := d.names("label", "list", "-R", repo, "--limit", "500", "--json", "name")
		for _, l := range wantedLabels(d) {
			r.add(err == nil && contains(have, l.name), "the label %s on %s", l.name, repo)
		}
	}
	checks["linear"] = func(d Deps, r *report) {
		if d.Project.Tracker.Kind == "linear" {
			r.findings = append(r.findings, finding{text: "the Linear connector on the Claude account the sessions use, at claude.ai under Settings, Connectors", owner: true})
		}
	}
	checks["review-token"] = func(d Deps, r *report) {
		repo, err := d.repo()
		if err != nil {
			r.add(false, "%v", err)
			return
		}
		have, err := d.names("secret", "list", "-R", repo, "--json", "name")
		r.add(err == nil && contains(have, ReviewSecret), "the review's token, the secret %s on %s", ReviewSecret, repo)
	}
}

var fixes = map[string]string{
	"labels":       "mingo apply %s",
	"review-token": "only you can: claude setup-token, then mingo secret review %s",
	"clones":       "mingo setup %s",
	"memory":       "mingo setup %s",
	"guards":       "mingo install",
}

// returns how many things are missing
func Doctor(d Deps) (int, error) {
	p := d.Project
	mods, files, err := d.files()
	if err != nil {
		return 0, err
	}
	fmt.Fprintln(d.Out, p.Name)
	missing := 0
	for _, m := range mods {
		fmt.Fprintf(d.Out, "  %s: %s\n", m.Name, m.Does)
		for _, c := range m.Checks {
			r := &report{}
			checks[c](d, r)
			fix := fixes[c]
			if strings.Contains(fix, "%s") {
				fix = fmt.Sprintf(fix, p.Name)
			}
			for _, f := range r.findings {
				if f.owner {
					fmt.Fprintf(d.Out, "    only you can: %s\n", f.text)
				} else if f.ok {
					fmt.Fprintf(d.Out, "    in place: %s\n", f.text)
				} else {
					fmt.Fprintf(d.Out, "    missing: %s (%s)\n", f.text, fix)
				}
			}
			missing += r.missing
		}
	}
	co, cleanup, err := d.checkout(false)
	if err != nil {
		return 0, err
	}
	defer cleanup()
	lock, err := co.lock()
	if err != nil {
		return 0, err
	}
	fmt.Fprintln(d.Out, "  files")
	steps, err := d.plan(mods, files, co, lock)
	if err != nil {
		return 0, err
	}
	missing += d.report(steps)
	if p.ClaudeLocal() {
		_, local := d.split(mods, files)
		for _, role := range p.RoleNames() {
			c, err := d.clone(role)
			if err != nil {
				continue
			}
			fmt.Fprintf(d.Out, "  Claude's files in %s's clone\n", role)
			unexcluded, _, err := c.unexcluded(mods)
			if err != nil {
				return 0, err
			}
			if len(unexcluded) == 0 {
				fmt.Fprintln(d.Out, "    in place: Claude's files out of git in .git/info/exclude")
			} else {
				fmt.Fprintf(d.Out, "    missing: %s in .git/info/exclude (mingo apply %s)\n", strings.Join(unexcluded, " "), p.Name)
				missing++
			}
			lock, err := c.lock()
			if err != nil {
				return 0, err
			}
			steps, err := planClaude(mods, local, c.read, lock)
			if err != nil {
				return 0, fmt.Errorf("in %s's clone, %v", role, err)
			}
			missing += d.report(steps)
		}
	}
	return missing, nil
}

// prints what of the steps is in place and what is missing, and returns how many are missing
func (d Deps) report(steps []Step) int {
	missing := 0
	for _, s := range steps {
		if s.Owned {
			if s.Action == Same {
				fmt.Fprintf(d.Out, "    in place: %s in %s\n", s.What, s.File.Path)
			} else {
				fmt.Fprintf(d.Out, "    missing: %s in %s (mingo apply %s)\n", s.What, s.File.Path, d.Project.Name)
				missing++
			}
			continue
		}
		switch s.Action {
		case Same:
			fmt.Fprintf(d.Out, "    in place: %s\n", s.File.Path)
		case Kept:
			fmt.Fprintf(d.Out, "    in place, as the project changed it: %s\n", s.File.Path)
		case Add:
			fmt.Fprintf(d.Out, "    missing: %s (mingo apply %s)\n", s.File.Path, d.Project.Name)
			missing++
		case Update:
			fmt.Fprintf(d.Out, "    out of date: %s (mingo apply %s)\n", s.File.Path, d.Project.Name)
			missing++
		case Edited:
			if s.Own {
				fmt.Fprintf(d.Out, "    the project's own, and mingo renders it otherwise: %s\n", s.File.Path)
			} else {
				fmt.Fprintf(d.Out, "    changed by the project, and mingo renders it otherwise: %s\n", s.File.Path)
			}
		case Remove:
			fmt.Fprintf(d.Out, "    left over: %s, which no module renders now (mingo apply %s)\n", s.File.Path, d.Project.Name)
			missing++
		case Leave:
			if s.Old != nil {
				fmt.Fprintf(d.Out, "    the project's own now: %s, which no module renders\n", s.File.Path)
			}
		}
	}
	return missing
}

// sets the review's token on the project's repository from what claude setup-token printed
func SetReviewSecret(d Deps, token string) error {
	repo, err := d.repo()
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("no token given, paste what claude setup-token printed")
	}
	r, err := d.Run(team.Options{Dir: d.TempRoot, Timeout: ghTimeout, Stdin: token}, "gh", "secret", "set", ReviewSecret, "-R", repo)
	if err != nil {
		return err
	}
	if r.NotFound {
		return errors.New("gh, the GitHub CLI, is not installed")
	}
	if !r.OK() {
		return fmt.Errorf("gh secret set failed:\n%s", team.LastLines(r.Stderr+r.Stdout, shownErrors))
	}
	fmt.Fprintf(d.Out, "the review's token is set on %s\n", repo)
	return nil
}
