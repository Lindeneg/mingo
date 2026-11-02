package team

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Lindeneg/mingo/internal/config"
)

const (
	Remote          = "origin"
	GitTimeout      = 120 * time.Second
	GenerateTimeout = 300 * time.Second
	mostConflicts   = 127
	shownLines      = 8
)

type Generators struct {
	List  func(tree string) ([]string, error)
	Write func(tree string) ([]string, error)
}

func outputLines(text string) []string {
	var out []string
	for _, l := range lines(text) {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func runCommands(run Runner, tree string, commands []string) (string, error) {
	last := ""
	for _, c := range commands {
		fields := strings.Fields(c)
		r, err := run(Options{Dir: tree, Timeout: GenerateTimeout}, fields[0], fields[1:]...)
		if err != nil {
			return "", err
		}
		switch {
		case r.NotFound:
			return "", stop(fields[0] + " is not installed")
		case r.TimedOut:
			return "", stop(fmt.Sprintf("%s did not finish in %d seconds; nothing was pushed", c, int(GenerateTimeout.Seconds())))
		case r.Code != 0:
			var parts []string
			for _, s := range []string{r.Stdout, r.Stderr} {
				if s = strings.TrimRightFunc(s, isSpace); s != "" {
					parts = append(parts, s)
				}
			}
			return "", stop(c + " failed; nothing was pushed:\n" + LastLines(strings.Join(parts, "\n"), shownLines))
		}
		last = r.Stdout
	}
	return last, nil
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f'
}

// nil when the project names no generators
func ConfigGenerators(w *config.Workflow, run Runner) *Generators {
	if len(w.GenerateList) == 0 || len(w.Generate) == 0 {
		return nil
	}
	return &Generators{
		List: func(tree string) ([]string, error) {
			out, err := runCommands(run, tree, w.GenerateList)
			if err != nil {
				return nil, err
			}
			return outputLines(out), nil
		},
		Write: func(tree string) ([]string, error) {
			out, err := runCommands(run, tree, w.Generate)
			if err != nil {
				return nil, err
			}
			var written []string
			seen := map[string]bool{}
			for _, l := range outputLines(out) {
				if p, ok := w.GeneratedPath(l); ok && !seen[p] {
					seen[p] = true
					written = append(written, p)
				}
			}
			return written, nil
		},
	}
}

type RebaseDeps struct {
	Root       string
	Project    *config.Project
	Run        Runner
	Out        io.Writer
	TempRoot   string
	Generators *Generators
}

type rebaser struct {
	RebaseDeps
	base string
}

func (r *rebaser) git(cwd string, args ...string) (Result, error) {
	res, err := r.Run(Options{Dir: cwd, Timeout: GitTimeout, Env: []string{"GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0"}}, "git", args...)
	if err != nil {
		return res, err
	}
	if res.NotFound {
		return res, stop("git is not installed")
	}
	if res.TimedOut {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		return res, stop(fmt.Sprintf("git %s did not finish in %d seconds", name, int(GitTimeout.Seconds())))
	}
	return res, nil
}

func (r *rebaser) must(cwd, what string, args ...string) (string, error) {
	res, err := r.git(cwd, args...)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		text := res.Stderr
		if text == "" {
			text = res.Stdout
		}
		return "", stop(what + " failed:\n" + LastLines(text, shownLines))
	}
	return res.Stdout, nil
}

func listPaths(output string) []string {
	var out []string
	for _, p := range strings.Split(output, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isDocument(path string) bool {
	return strings.HasSuffix(path, ".md")
}

var plainPath = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

type regenerated struct {
	paths          map[string]bool
	attributesFile string
}

type orderedSet struct {
	order []string
	has   map[string]bool
}

func (s *orderedSet) add(v string) {
	if s.has == nil {
		s.has = map[string]bool{}
	}
	if !s.has[v] {
		s.has[v] = true
		s.order = append(s.order, v)
	}
}

type resolvedFiles struct {
	order []string
	took  map[string]takenSet
}

func (rf *resolvedFiles) add(path string, took []Taken) {
	if rf.took == nil {
		rf.took = map[string]takenSet{}
	}
	if _, ok := rf.took[path]; !ok {
		rf.order = append(rf.order, path)
		rf.took[path] = takenSet{}
	}
	for _, t := range took {
		rf.took[path][t] = true
	}
}

func (r *rebaser) resolveConflict(worktree, scratch, path string) (Outcome, error) {
	shared, ok := r.Project.Workflow.SharedFile(path)
	if !ok || shared.Merge == "none" {
		return edited("not an append-only list"), nil
	}
	var stages [3]Result
	for i := range stages {
		res, err := r.git(worktree, "show", fmt.Sprintf(":%d:%s", i+1, path))
		if err != nil {
			return Outcome{}, err
		}
		stages[i] = res
	}
	base, ours, theirs := stages[0], stages[1], stages[2]
	if ours.Code != 0 || theirs.Code != 0 {
		return edited("one side deleted the file"), nil
	}
	var baseText *string
	if base.Code == 0 {
		baseText = &base.Stdout
	}
	if shared.Merge == "json-keys" {
		return MergeJSONKeys(baseText, ours.Stdout, theirs.Stdout), nil
	}
	files := map[string]string{"ours": ours.Stdout, "base": "", "theirs": theirs.Stdout}
	if baseText != nil {
		files["base"] = *baseText
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(scratch, name), []byte(text), 0o644); err != nil {
			return Outcome{}, err
		}
	}
	merged, err := r.git(scratch, "merge-file", "-p", "--diff3", fmt.Sprintf("--marker-size=%d", MarkerSize), "ours", "base", "theirs")
	if err != nil {
		return Outcome{}, err
	}
	lost := merged.Stdout == "" && (files["ours"] != "" || files["theirs"] != "")
	if merged.Code < 0 || merged.Code > mostConflicts || lost {
		return Outcome{}, stop("git merge-file failed on " + path + ":\n" + LastLines(merged.Stderr, 3))
	}
	outcome := KeepBothSides(merged.Stdout)
	hasNoEnd := func(text string) bool { return text != "" && !strings.HasSuffix(text, "\n") }
	if outcome.Merged() && hasNoEnd(files["ours"]) && hasNoEnd(files["theirs"]) {
		outcome.Text = strings.TrimSuffix(strings.TrimSuffix(outcome.Text, "\n"), "\r")
	}
	return outcome, nil
}

func (r *rebaser) takenLines() []struct {
	taken Taken
	line  string
} {
	return []struct {
		taken Taken
		line  string
	}{
		{Additions, "kept both sides, " + r.base + "'s first"},
		{FirstRewrite, "took an entry " + r.base + " rewrote or removed"},
		{SecondRewrite, "took an entry the branch rewrote or removed"},
		{SharedRewrite, "took an entry both sides changed alike"},
	}
}

const authorsConflict = "a conflict that is the author's to resolve; nothing was pushed:\n"

func (r *rebaser) makeAgain(worktree string, conflicted []string) ([]string, error) {
	for _, path := range conflicted {
		for _, stage := range []int{2, 3} {
			res, err := r.git(worktree, "cat-file", "-e", fmt.Sprintf(":%d:%s", stage, path))
			if err != nil {
				return nil, err
			}
			if res.Code != 0 {
				return nil, stop(authorsConflict + "  " + path + ": one side deleted the generated file")
			}
		}
	}
	written, err := r.Generators.Write(worktree)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, path := range conflicted {
		if !contains(written, path) {
			missing = append(missing, "  "+path+": the generators no longer write it")
		}
	}
	if len(missing) > 0 {
		return nil, stop(authorsConflict + strings.Join(missing, "\n"))
	}
	out, err := r.must(worktree, "reading which generated files git ignores",
		append([]string{"--literal-pathspecs", "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--"}, written...)...)
	if err != nil {
		return nil, err
	}
	ignored := map[string]bool{}
	for _, p := range listPaths(out) {
		ignored[p] = true
	}
	var committed []string
	for _, p := range written {
		if !ignored[p] {
			committed = append(committed, p)
		}
	}
	out, err = r.must(worktree, "listing what the generators changed",
		append([]string{"--literal-pathspecs", "status", "--porcelain", "-z", "--untracked-files=all", "--"}, committed...)...)
	if err != nil {
		return nil, err
	}
	changed := changedInWorktree(out)
	if _, err := r.must(worktree, "staging the generated files", append([]string{"--literal-pathspecs", "add", "--"}, committed...)...); err != nil {
		return nil, err
	}
	var all orderedSet
	for _, p := range append(append([]string{}, conflicted...), changed...) {
		all.add(p)
	}
	return all.order, nil
}

func changedInWorktree(output string) []string {
	entries := strings.Split(output, "\x00")
	var paths []string
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 4 {
			continue
		}
		staged, unstaged, path := entry[0], entry[1], entry[3:]
		if staged == 'R' || staged == 'C' {
			i++
		}
		if unstaged != ' ' || staged == '?' {
			paths = append(paths, path)
		}
	}
	return paths
}

func (r *rebaser) replay(worktree, scratch string, regen *regenerated) (*resolvedFiles, *orderedSet, error) {
	resolved, madeAgain := &resolvedFiles{}, &orderedSet{}
	rebase := func(args ...string) (Result, error) {
		// a resolution git remembers would settle a conflict this command must see
		prefix := []string{"-c", "rerere.enabled=false"}
		if regen != nil {
			prefix = append(prefix, "-c", "core.attributesFile="+regen.attributesFile)
		}
		return r.git(worktree, append(append(prefix, "rebase"), args...)...)
	}
	step, err := rebase(Remote + "/" + r.base)
	if err != nil {
		return nil, nil, err
	}
	for step.Code != 0 {
		out, err := r.must(worktree, "listing the conflicts", "diff", "--name-only", "--diff-filter=U", "-z")
		if err != nil {
			return nil, nil, err
		}
		conflicted := listPaths(out)
		if len(conflicted) == 0 {
			text := step.Stderr
			if text == "" {
				text = step.Stdout
			}
			output := LastLines(text, shownLines)
			r.git(worktree, "rebase", "--abort")
			return nil, nil, stop("the rebase stopped without a conflict:\n" + output)
		}
		var refused, generated []string
		for _, path := range conflicted {
			if regen != nil && regen.paths[path] {
				generated = append(generated, path)
				continue
			}
			outcome, err := r.resolveConflict(worktree, scratch, path)
			if err != nil {
				return nil, nil, err
			}
			if !outcome.Merged() {
				refused = append(refused, "  "+path+": "+outcome.Reason)
				continue
			}
			full := filepath.Join(worktree, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return nil, nil, err
			}
			if err := os.WriteFile(full, []byte(outcome.Text), 0o644); err != nil {
				return nil, nil, err
			}
			if _, err := r.must(worktree, "staging "+path, "add", "--", path); err != nil {
				return nil, nil, err
			}
			resolved.add(path, outcome.Took)
		}
		if len(refused) > 0 {
			r.git(worktree, "rebase", "--abort")
			return nil, nil, stop(authorsConflict + strings.Join(refused, "\n"))
		}
		if regen != nil && len(generated) > 0 {
			again, err := r.makeAgain(worktree, generated)
			if err != nil {
				return nil, nil, err
			}
			for _, p := range again {
				madeAgain.add(p)
			}
		}
		// a resolution that leaves the step with no change of its own ends it, where continue would stop
		next := "--continue"
		staged, err := r.git(worktree, "diff", "--cached", "--quiet")
		if err != nil {
			return nil, nil, err
		}
		if staged.Code == 0 {
			next = "--skip"
		}
		if step, err = rebase(next); err != nil {
			return nil, nil, err
		}
	}
	return resolved, madeAgain, nil
}

func (r *rebaser) findRegenerated(tree, scratch string, met []string) (*regenerated, error) {
	if r.Generators == nil {
		return nil, nil
	}
	var candidates []string
	for _, p := range met {
		if !isDocument(p) {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	listed, err := r.Generators.List(tree)
	if err != nil {
		return nil, err
	}
	var paths, unnamed []string
	for _, p := range candidates {
		if contains(listed, p) {
			paths = append(paths, p)
			if !plainPath.MatchString(p) {
				unnamed = append(unnamed, p)
			}
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	if len(unnamed) > 0 {
		return nil, stop("a generated path git attributes cannot name: " + strings.Join(unnamed, ", "))
	}
	file := filepath.Join(scratch, "attributes")
	var b strings.Builder
	set := map[string]bool{}
	for _, p := range paths {
		b.WriteString("/" + p + " merge=binary\n")
		set[p] = true
	}
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	return &regenerated{paths: set, attributesFile: file}, nil
}

func short(sha string) string {
	return sha[:min(shortSHA, len(sha))]
}

func Rebase(d RebaseDeps, args []string) (int, error) {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(d.Out, "usage: mingo pr rebase <branch>")
		return ExitUsage, nil
	}
	r := &rebaser{RebaseDeps: d, base: d.Project.Workflow.Base}
	err := r.rebase(args[0])
	var s *Stop
	if errors.As(err, &s) {
		fmt.Fprintln(d.Out, s.Message)
		return ExitFailed, nil
	}
	if err != nil {
		return ExitFailed, err
	}
	return ExitOK, nil
}

func (r *rebaser) rebase(branch string) (err error) {
	if branch == r.base {
		return stop(r.base + " is never rebased")
	}
	if _, err := r.must(r.Root, "fetching "+Remote, "fetch", "--prune", Remote); err != nil {
		return err
	}
	found, err := r.git(r.Root, "rev-parse", "--verify", "--quiet", "refs/remotes/"+Remote+"/"+branch)
	if err != nil {
		return err
	}
	if found.Code != 0 {
		return stop(Remote + " has no branch " + branch)
	}
	oldHead := strings.TrimSpace(found.Stdout)
	out, err := r.must(r.Root, "reading "+r.base, "rev-parse", "refs/remotes/"+Remote+"/"+r.base)
	if err != nil {
		return err
	}
	master := strings.TrimSpace(out)
	out, err = r.must(r.Root, "finding the fork point", "merge-base", master, oldHead)
	if err != nil {
		return err
	}
	fork := strings.TrimSpace(out)
	if fork == oldHead && fork != master {
		return stop(branch + " has no commits of its own: " + r.base + " already holds it")
	}
	if fork == master {
		fmt.Fprintf(r.Out, "%s is up to date with %s (%s); nothing pushed\n", branch, r.base, short(master))
		return nil
	}
	changed := func(to string) ([]string, error) {
		out, err := r.must(r.Root, "listing the changes", "diff", "--name-only", "--no-renames", "-z", fork, to)
		return listPaths(out), err
	}
	branchFiles, err := changed(oldHead)
	if err != nil {
		return err
	}
	masterFiles, err := changed(master)
	if err != nil {
		return err
	}
	var met []string
	for _, p := range masterFiles {
		if contains(branchFiles, p) {
			met = append(met, p)
		}
	}

	worktree, err := os.MkdirTemp(r.TempRoot, "mingo-rebase-")
	if err != nil {
		return err
	}
	tree, scratch := filepath.Join(worktree, "tree"), filepath.Join(worktree, "scratch")
	defer func() {
		o := Options{Dir: r.Root, Timeout: GitTimeout}
		r.Run(o, "git", "worktree", "remove", "--force", tree)
		os.RemoveAll(worktree)
		r.Run(o, "git", "worktree", "prune")
	}()
	if err := os.Mkdir(scratch, 0o755); err != nil {
		return err
	}
	if _, err := r.must(r.Root, "making the worktree", "worktree", "add", "--detach", tree, master); err != nil {
		return err
	}
	regen, err := r.findRegenerated(tree, scratch, met)
	if err != nil {
		return err
	}
	if _, err := r.must(tree, "checking out the branch", "checkout", "--quiet", "--detach", oldHead); err != nil {
		return err
	}
	resolved, madeAgain, err := r.replay(tree, scratch, regen)
	if err != nil {
		return err
	}
	out, err = r.must(tree, "reading the new head", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	newHead := strings.TrimSpace(out)
	if newHead == master {
		return stop("every commit of " + branch + " is on " + r.base + " already; nothing was pushed")
	}
	push, err := r.git(tree, "push", "--force-with-lease=refs/heads/"+branch+":"+oldHead, Remote, "HEAD:refs/heads/"+branch)
	if err != nil {
		return err
	}
	if push.Code != 0 {
		return stop("the push was refused (the branch may have moved); nothing changed:\n" + LastLines(push.Stderr, shownLines))
	}

	fmt.Fprintf(r.Out, "%s is on %s %s: %s became %s, pushed\n", branch, r.base, short(master), short(oldHead), short(newHead))
	for _, path := range resolved.order {
		for _, t := range r.takenLines() {
			if resolved.took[path][t.taken] {
				fmt.Fprintf(r.Out, "%s: %s\n", t.line, path)
			}
		}
	}
	for _, path := range madeAgain.order {
		fmt.Fprintln(r.Out, "made again by the generators: "+path)
	}
	for _, path := range met {
		fmt.Fprintf(r.Out, "%s changed a file the branch touches: %s\n", r.base, path)
	}
	gateDue := false
	for _, p := range append(append([]string{}, met...), resolved.order...) {
		gateDue = gateDue || !isDocument(p)
	}
	if gateDue {
		fmt.Fprintf(r.Out, "%s brought code into files the branch touches: the full gate runs again\n", r.base)
	} else {
		fmt.Fprintf(r.Out, "%s brought no code into files the branch touches: no new gate is due\n", r.base)
	}
	return nil
}
