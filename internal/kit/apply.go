package kit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/team"
)

const (
	Branch      = "mingo-kit"
	gitTimeout  = 120 * time.Second
	ghTimeout   = 60 * time.Second
	shownErrors = 8
)

type Deps struct {
	Kit      *Kit
	Project  *config.Project
	Run      team.Runner
	Out      io.Writer
	TempRoot string
	Version  string
	Home     string
}

func (d Deps) git(dir string, args ...string) (string, error) {
	r, err := d.Run(team.Options{Dir: dir, Timeout: gitTimeout, Env: []string{"GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true"}}, "git", args...)
	if err != nil {
		return "", err
	}
	if r.NotFound {
		return "", errors.New("git is not installed")
	}
	if r.TimedOut {
		return "", fmt.Errorf("git %s did not finish in %d seconds", args[0], int(gitTimeout.Seconds()))
	}
	if r.Code != 0 {
		text := r.Stderr
		if text == "" {
			text = r.Stdout
		}
		return "", fmt.Errorf("git %s failed:\n%s", args[0], team.LastLines(text, shownErrors))
	}
	return r.Stdout, nil
}

func (d Deps) gh(dir string, args ...string) (string, error) {
	r, err := d.Run(team.Options{Dir: dir, Timeout: ghTimeout}, "gh", args...)
	if err != nil {
		return "", err
	}
	if r.NotFound {
		return "", errors.New("gh, the GitHub CLI, is not installed")
	}
	if r.TimedOut {
		return "", fmt.Errorf("gh %s did not finish in %d seconds", args[0], int(ghTimeout.Seconds()))
	}
	if r.Code != 0 {
		text := r.Stderr
		if text == "" {
			text = r.Stdout
		}
		return "", fmt.Errorf("gh %s failed:\n%s", strings.Join(args[:min(2, len(args))], " "), team.LastLines(text, shownErrors))
	}
	return r.Stdout, nil
}

type checkout struct {
	dir   string
	empty bool
	kit   string
	sha   string
}

// the sha a branch has in ls-remote's output, empty when it has none
func refSHA(refs, branch string) string {
	for _, line := range strings.Split(refs, "\n") {
		if sha, ref, ok := strings.Cut(line, "\t"); ok && ref == "refs/heads/"+branch {
			return sha
		}
	}
	return ""
}

func (d Deps) openKitPR(dir string) (string, error) {
	out, err := d.gh(dir, "pr", "list", "--head", Branch, "--state", "open", "--json", "url")
	if err != nil {
		return "", err
	}
	var open []struct {
		URL string `json:"url"`
	}
	if json.Unmarshal([]byte(out), &open) != nil {
		return "", fmt.Errorf("gh pr list did not answer with a list")
	}
	if len(open) == 0 {
		return "", nil
	}
	return open[0].URL, nil
}

// a temporary clone from the lead's clone's origin, so no role's folder is touched. apply works on
// the open pull request's branch when there is one, so what was pushed to it stays; doctor reads the base
func (d Deps) checkout(apply bool) (*checkout, func(), error) {
	p := d.Project
	lead := p.CloneDir(p.Lead)
	url, err := d.git(lead, "remote", "get-url", "origin")
	if err != nil {
		return nil, nil, fmt.Errorf("mingo works on %s through the lead's clone %s, run mingo setup %s first", p.Name, lead, p.Name)
	}
	url = strings.TrimSpace(url)
	refs, err := d.git(lead, "ls-remote", url)
	if err != nil {
		return nil, nil, err
	}
	co := &checkout{empty: strings.TrimSpace(refs) == "", sha: refSHA(refs, Branch)}
	if !co.empty && refSHA(refs, p.Workflow.Base) == "" {
		return nil, nil, fmt.Errorf("%s has no branch %s", url, p.Workflow.Base)
	}
	branch := p.Workflow.Base
	if apply && co.sha != "" {
		if co.kit, err = d.openKitPR(lead); err != nil {
			return nil, nil, err
		}
		if co.kit != "" {
			branch = Branch
		}
	}
	root, err := os.MkdirTemp(d.TempRoot, "mingo-kit-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(root) }
	co.dir = filepath.Join(root, "repo")
	args := []string{"clone", "--quiet", "--depth", "1", "--branch", branch, "--single-branch", url, co.dir}
	if co.empty {
		args = []string{"clone", "--quiet", url, co.dir}
	}
	if _, err := d.git(root, args...); err != nil {
		cleanup()
		return nil, nil, err
	}
	if co.empty {
		if _, err := d.git(co.dir, "symbolic-ref", "HEAD", "refs/heads/"+p.Workflow.Base); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	return co, cleanup, nil
}

func (c *checkout) read(path string) ([]byte, bool) {
	data, err := os.ReadFile(filepath.Join(c.dir, filepath.FromSlash(path)))
	return data, err == nil
}

func (c *checkout) lock() (*Lock, error) {
	data, _ := c.read(LockPath)
	return ParseLock(data)
}

func (d Deps) files() ([]Module, []File, error) {
	mods, err := d.Kit.For(d.Project)
	if err != nil {
		return nil, nil, err
	}
	files, err := d.Kit.Render(d.Project, mods)
	return mods, files, err
}

func (d Deps) diff(root string, s Step) string {
	a, b := filepath.Join("a", filepath.FromSlash(s.File.Path)), filepath.Join("b", filepath.FromSlash(s.File.Path))
	for name, data := range map[string][]byte{a: s.Old, b: []byte(s.File.Text)} {
		full := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, data, 0o644)
	}
	r, _ := d.Run(team.Options{Dir: root, Timeout: gitTimeout}, "git", "diff", "--no-index", "--no-color", "--", a, b)
	return r.Stdout
}

func Apply(d Deps) error {
	mods, files, err := d.files()
	if err != nil {
		return err
	}
	for _, m := range mods {
		for _, s := range m.Setup {
			if err := setups[s](d); err != nil {
				return err
			}
		}
	}
	clones, err := d.clones()
	if err != nil {
		return err
	}
	co, cleanup, err := d.checkout(true)
	if err != nil {
		return err
	}
	defer cleanup()
	lock, err := co.lock()
	if err != nil {
		return err
	}
	steps, err := d.plan(mods, files, co, lock)
	if err != nil {
		return err
	}
	scratch := filepath.Join(filepath.Dir(co.dir), "diff")
	pushed, err := d.applyRepo(co, lock, steps, scratch)
	if err != nil {
		return err
	}
	_, local := d.split(mods, files)
	wrote, err := d.applyClones(mods, local, clones, scratch)
	if err != nil {
		return err
	}
	if !pushed && !wrote {
		fmt.Fprintln(d.Out, "nothing to apply")
	}
	return nil
}

// writes and removes what the steps say in dir, and prints each, after prefix
func (d Deps) land(dir, prefix string, steps []Step, scratch string, remove func(path string) error) ([]string, []string, error) {
	var written, removed []string
	for _, s := range steps {
		switch s.Action {
		case Add, Update:
			full := filepath.Join(dir, filepath.FromSlash(s.File.Path))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return nil, nil, err
			}
			if err := os.WriteFile(full, []byte(s.File.Text), 0o644); err != nil {
				return nil, nil, err
			}
			written = append(written, s.File.Path)
			verb := "adds"
			if s.Action == Update {
				verb = "updates"
			}
			fmt.Fprintf(d.Out, "%s%s %s\n", prefix, verb, s.File.Path)
		case Edited:
			why := "the project changed it, and mingo now renders"
			if s.Own {
				why = "the project has its own, and mingo renders"
			}
			fmt.Fprintf(d.Out, "%sleaves %s alone, %s:\n%s", prefix, s.File.Path, why, d.diff(scratch, s))
		case Remove:
			if err := remove(s.File.Path); err != nil {
				return nil, nil, err
			}
			removed = append(removed, s.File.Path)
			fmt.Fprintf(d.Out, "%sremoves %s, which no module renders now\n", prefix, s.File.Path)
		case Leave:
			if s.Old != nil {
				fmt.Fprintf(d.Out, "%skeeps %s, which no module renders now and the project changed\n", prefix, s.File.Path)
			}
		}
	}
	return written, removed, nil
}

// commits the steps and pushes them, as the first commit or to the kit's pull request, and says whether there was anything to push
func (d Deps) applyRepo(co *checkout, lock *Lock, steps []Step, scratch string) (bool, error) {
	next, lockChanged := NextLock(steps, lock, d.Version)
	written, removed, err := d.land(co.dir, "", steps, scratch, func(path string) error {
		_, err := d.git(co.dir, "rm", "--quiet", "--", path)
		return err
	})
	if err != nil {
		return false, err
	}
	if len(written) == 0 && len(removed) == 0 && !lockChanged {
		return false, nil
	}
	full := filepath.Join(co.dir, filepath.FromSlash(LockPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(full, next.Bytes(), 0o644); err != nil {
		return false, err
	}
	if _, err := d.git(co.dir, append([]string{"add", "--", LockPath}, written...)...); err != nil {
		return false, err
	}
	subject := "mingo kit: " + moduleList(steps, append(append([]string(nil), written...), removed...))
	if _, err := d.git(co.dir, "commit", "--quiet", "-m", subject); err != nil {
		return false, err
	}
	base := d.Project.Workflow.Base
	if co.empty {
		if _, err := d.git(co.dir, "push", "--quiet", "origin", "HEAD:refs/heads/"+base); err != nil {
			return false, err
		}
		fmt.Fprintf(d.Out, "the first commit on %s: %s\n", base, subject)
		return true, nil
	}
	if co.kit != "" {
		if _, err := d.git(co.dir, "push", "--quiet", "origin", "HEAD:refs/heads/"+Branch); err != nil {
			return false, fmt.Errorf("%s moved while mingo applied, run mingo apply %s again\n%v", Branch, d.Project.Name, err)
		}
		fmt.Fprintf(d.Out, "pushed to the open pull request %s\n", co.kit)
		return true, nil
	}
	if _, err := d.git(co.dir, "push", "--quiet", "--force-with-lease=refs/heads/"+Branch+":"+co.sha, "origin", "HEAD:refs/heads/"+Branch); err != nil {
		return false, fmt.Errorf("%s moved while mingo applied, run mingo apply %s again\n%v", Branch, d.Project.Name, err)
	}
	return true, d.pullRequest(co.dir, subject, steps)
}

func moduleList(steps []Step, written []string) string {
	var names []string
	for _, s := range steps {
		if contains(written, s.File.Path) && !contains(names, s.File.Module) {
			names = append(names, s.File.Module)
		}
	}
	if len(names) == 0 {
		return "the lock"
	}
	return strings.Join(names, ", ")
}

func (d Deps) pullRequest(dir, subject string, steps []Step) error {
	var body bytes.Buffer
	for _, s := range steps {
		switch s.Action {
		case Add:
			fmt.Fprintf(&body, "- adds `%s` (%s)\n", s.File.Path, s.File.Module)
		case Update:
			fmt.Fprintf(&body, "- updates `%s` (%s)\n", s.File.Path, s.File.Module)
		case Edited:
			if s.Own {
				fmt.Fprintf(&body, "- leaves `%s` alone, the project has its own\n", s.File.Path)
			} else {
				fmt.Fprintf(&body, "- leaves `%s` alone, the project changed it\n", s.File.Path)
			}
		case Remove:
			fmt.Fprintf(&body, "- removes `%s`, which no module renders now\n", s.File.Path)
		case Leave:
			if s.Old != nil {
				fmt.Fprintf(&body, "- keeps `%s`, which no module renders now and the project changed\n", s.File.Path)
			}
		}
	}
	out, err := d.gh(dir, "pr", "create", "--base", d.Project.Workflow.Base, "--head", Branch, "--title", subject, "--body", body.String())
	if err != nil {
		return err
	}
	fmt.Fprintf(d.Out, "opened %s\n", strings.TrimSpace(out))
	return nil
}

func (d Deps) plan(mods []Module, files []File, co *checkout, lock *Lock) ([]Step, error) {
	if d.Project.ClaudeLocal() {
		repo, local := d.split(mods, files)
		for _, path := range claudePaths(mods, local) {
			if _, ok := co.read(path); ok {
				return nil, fmt.Errorf("claude_files is local, so Claude's files stay out of the repository, and %s has %s, take it out first", d.Project.Repo, path)
			}
		}
		scripts, err := PlanScripts(mods, d.Project, co.read)
		return append(Plan(repo, co.read, lock), scripts...), err
	}
	steps := append(Plan(files, co.read, lock), PlanImports(mods, co.read)...)
	scripts, err := PlanScripts(mods, d.Project, co.read)
	if err != nil {
		return nil, err
	}
	settings, err := PlanSettings(co.read)
	return append(append(steps, scripts...), settings...), err
}
