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
	"github.com/Lindeneg/mingo/internal/provider"
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
			if runes := []rune(text); len(runes) > 100 {
				text = string(runes[:97]) + "..."
			}
			out = append(out, label{r.Name, text, "1D76DB"})
		}
	}
	return out
}

const CIUnavailable = "ci-unavailable"

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
		if hasLabel(have, l.name) {
			continue
		}
		if _, err := d.gh(d.TempRoot, "label", "create", l.name, "-R", repo, "--description", l.description, "--color", l.color); err != nil {
			return err
		}
		fmt.Fprintf(d.Out, "label: %s on %s\n", l.name, repo)
	}
	return nil
}

// GitHub takes Bug and bug for one label
func hasLabel(have []string, name string) bool {
	for _, h := range have {
		if strings.EqualFold(h, name) {
			return true
		}
	}
	return false
}

func handoffLine(role string) string {
	return "- [Handoff](handoff-" + role + ".md): read first, where the work stands and the next step\n"
}

func pointsAtHandoff(memory []byte, role string) bool {
	return bytes.Contains(memory, []byte("handoff-"+role+".md"))
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

// makes the role's memory folder and an index that points at its handoff, and puts the pointer first
// in an index without it, since a session reads only the start of a long one. a CLI with no index
// gets the folder alone
func SeedRole(p *config.Project, role string) (string, bool, error) {
	dir := p.MemoryDir(role)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	index := p.Provider().MemoryIndex()
	if index == "" {
		return dir, false, nil
	}
	file := filepath.Join(dir, index)
	old, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return file, true, os.WriteFile(file, []byte(handoffLine(role)), 0o644)
	}
	if err != nil {
		return file, false, err
	}
	if pointsAtHandoff(old, role) {
		return file, false, nil
	}
	return file, true, os.WriteFile(file, append([]byte(handoffLine(role)), old...), 0o644)
}

type finding struct {
	ok    bool
	text  string
	owner bool
	fix   string
	note  bool
}

type report struct {
	findings []finding
	missing  int
	unknown  int
}

func (r *report) add(ok bool, format string, args ...any) {
	r.findings = append(r.findings, finding{ok: ok, text: fmt.Sprintf(format, args...)})
	if !ok {
		r.missing++
	}
}

// a missing finding with a fix of its own, or the check's fix when fix is empty
func (r *report) miss(fix, format string, args ...any) {
	r.findings = append(r.findings, finding{text: fmt.Sprintf(format, args...), fix: fix})
	r.missing++
}

// a line that is neither in place nor missing
func (r *report) note(format string, args ...any) {
	r.findings = append(r.findings, finding{text: fmt.Sprintf(format, args...), note: true})
}

// what a check could not find out, which counts as neither in place nor missing
func (r *report) unchecked(what string, err error) {
	r.note("could not check %s: %s", what, strings.ReplaceAll(err.Error(), "\n", "\n      "))
	r.unknown++
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
		index := p.Provider().MemoryIndex()
		for _, role := range p.RoleNames() {
			if index == "" {
				info, err := os.Stat(p.MemoryDir(role))
				r.add(err == nil && info.IsDir(), "%s's memory folder %s", role, p.MemoryDir(role))
				continue
			}
			data, err := os.ReadFile(filepath.Join(p.MemoryDir(role), index))
			r.add(err == nil && pointsAtHandoff(data, role), "%s's %s pointing at handoff-%s.md", role, index, role)
		}
	},
	"guards": func(d Deps, r *report) {
		prov := d.Project.Provider()
		i, ok := prov.(provider.Installer)
		if !ok {
			r.note("the write and kill guards are not available with %s, which runs no hooks", prov.Title())
			return
		}
		in, text := i.Installed(d.Home)
		r.add(in, "%s", text)
	},
}

func init() {
	checks["labels"] = func(d Deps, r *report) {
		repo, err := d.repo()
		if err != nil {
			r.add(false, "%v", err)
			return
		}
		wanted := wantedLabels(d)
		if len(wanted) == 0 {
			return
		}
		have, err := d.names("label", "list", "-R", repo, "--limit", "500", "--json", "name")
		if err != nil {
			r.unchecked("the labels on "+repo, err)
			return
		}
		for _, l := range wanted {
			r.add(hasLabel(have, l.name), "the label %s on %s", l.name, repo)
		}
	}
	checks["linear"] = func(d Deps, r *report) {
		if d.Project.Tracker.Kind == "linear" {
			r.findings = append(r.findings, finding{text: "the Linear connector " + d.Project.Provider().Kit().Connectors, owner: true})
			r.findings = append(r.findings, finding{text: fmt.Sprintf("the Linear team %s, made in Linear with exactly that identifier, since the connector cannot make a team", d.Project.Tracker.Team), owner: true})
		}
	}
	checks["review-token"] = func(d Deps, r *report) {
		repo, err := d.repo()
		if err != nil {
			r.add(false, "%v", err)
			return
		}
		review := d.Project.Provider().Kit().Review
		what := fmt.Sprintf("the review's token, the secret %s on %s", review.Secret, repo)
		have, err := d.names("secret", "list", "-R", repo, "--json", "name")
		if err != nil {
			r.unchecked(what, err)
			return
		}
		if contains(have, review.Secret) {
			r.add(true, "%s", what)
		} else {
			r.miss("only you can: "+review.Token+", then mingo secret review %s", "%s", what)
		}
	}
}

var fixes = map[string]string{
	"labels": "mingo apply %s",
	"clones": "mingo setup %s",
	"memory": "mingo setup %s",
	"guards": "mingo install",
}

// returns how many things are missing, and an error when a check could not find out
func Doctor(d Deps) (int, error) {
	p := d.Project
	mods, files, err := d.files()
	if err != nil {
		return 0, err
	}
	fmt.Fprintln(d.Out, p.Name)
	missing, unknown := 0, 0
	done := map[string]bool{}
	for _, m := range mods {
		fmt.Fprintf(d.Out, "  %s: %s\n", m.Name, m.Does)
		for _, c := range m.Checks {
			if done[c] {
				continue
			}
			done[c] = true
			r := &report{}
			checks[c](d, r)
			for _, f := range r.findings {
				fix := f.fix
				if fix == "" {
					fix = fixes[c]
				}
				if strings.Contains(fix, "%s") {
					fix = fmt.Sprintf(fix, p.Name)
				}
				switch {
				case f.owner:
					fmt.Fprintf(d.Out, "    only you can: %s\n", f.text)
				case f.note:
					fmt.Fprintf(d.Out, "    %s\n", f.text)
				case f.ok:
					fmt.Fprintf(d.Out, "    in place: %s\n", f.text)
				default:
					fmt.Fprintf(d.Out, "    missing: %s (%s)\n", f.text, fix)
				}
			}
			missing += r.missing
			unknown += r.unknown
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
	rendered := files
	if p.ClaudeLocal() {
		repo, local, err := d.split(mods, files)
		if err != nil {
			return 0, err
		}
		rendered = append(repo, local...)
	}
	if open, used := prettierOpen(co.read, rendered); used {
		if len(open) == 0 {
			fmt.Fprintln(d.Out, "    in place: mingo's files out of Prettier in .prettierignore")
		}
		for _, path := range open {
			fmt.Fprintf(d.Out, "    missing: %s in .prettierignore, since Prettier would rewrite it (add it by hand)\n", path)
		}
		missing += len(open)
	}
	if p.ClaudeLocal() {
		_, local, err := d.split(mods, files)
		if err != nil {
			return 0, err
		}
		for _, role := range p.RoleNames() {
			c, err := d.clone(role)
			if err != nil {
				continue
			}
			fmt.Fprintf(d.Out, "  Claude's files in %s's clone\n", role)
			unexcluded, _, err := c.unexcluded(p.Provider().Kit(), mods)
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
			steps, err := planClaude(p.Provider(), local, c.read, lock)
			if err != nil {
				return 0, fmt.Errorf("in %s's clone, %v", role, err)
			}
			missing += d.report(steps)
		}
	}
	if unknown > 0 {
		return missing, fmt.Errorf("could not check %d of what the modules need, see above", unknown)
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
			} else if s.Action == Absent {
				fmt.Fprintf(d.Out, "    missing: %s in %s, which the repository does not have (add it, then mingo apply %s)\n", s.What, s.File.Path, d.Project.Name)
				missing++
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

// sets the review's token on the project's repository from what the provider's token command printed
func SetReviewSecret(d Deps, token string) error {
	repo, err := d.repo()
	if err != nil {
		return err
	}
	review := d.Project.Provider().Kit().Review
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("no token given, paste what %s printed", review.Token)
	}
	r, err := d.Run(team.Options{Dir: d.TempRoot, Timeout: ghTimeout, Stdin: token}, "gh", "secret", "set", review.Secret, "-R", repo)
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
