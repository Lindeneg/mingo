package team

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/ojson"
)

const (
	catalogs  = "packages/i18n/src/catalogs"
	list      = "docs/tech/open-questions.md"
	catalog   = catalogs + "/studio/en.json"
	appFile   = "apps/api/src/app.ts"
	generated = "packages/i18n/src/catalogs/studio/keys.generated.ts"
	size      = "packages/i18n/src/catalogs/studio/size.generated.ts"
	messages  = catalogs + "/studio/en.messages.generated.json"
	plurals   = "packages/i18n/src/catalogs/studio/plurals.generated.ts"
	manyKeys  = 3
	del       = "\x00delete"
)

const sharedTOML = `
[[workflow.shared]]
name = "the connector registry"
pattern = '^packages/connectors/(?:src/)?registry\.ts$'
merge = "lines"

[[workflow.shared]]
name = "the tool registry"
pattern = '^packages/agent/(?:src/)?tools/registry\.ts$'
merge = "lines"

[[workflow.shared]]
name = "the part schema union"
pattern = '^packages/contracts/src/parts/v\d+\.ts$'
merge = "lines"

[[workflow.shared]]
name = "the queue list"
pattern = '^apps/worker/src/queues\.ts$'
merge = "lines"

[[workflow.shared]]
name = "a route index"
pattern = '^apps/[^/]+/src/routes/index\.ts$'
merge = "lines"

[[workflow.shared]]
name = "an i18n catalog"
pattern = '^packages/i18n/src/catalogs/[^/]+/[^/]+\.json$'
merge = "json-keys"

[[workflow.shared]]
name = "an open questions list"
pattern = '^docs/(?:product|tech)/open-questions\.md$'
merge = "lines"

[[workflow.shared]]
name = "the tooling table"
pattern = '^docs/tech/tooling\.md$'
merge = "lines"

[[workflow.shared]]
name = "a migration"
pattern = '^packages/db/(?:src/)?migrations/'
merge = "none"
`

func entries(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

var listStart = entries("# Open questions", "", "## Decided provisionally", "", "- one", "", "## Undecided", "", "- later")

func withEntries(more ...string) string {
	return strings.Replace(listStart, "- one\n", entries(append([]string{"- one"}, more...)...), 1)
}

func jsonOf(pairs ...string) string {
	o := ojson.NewObject()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i], pairs[i+1])
	}
	data, _ := ojson.Marshal(o)
	return string(data)
}

func keysOf(cat map[string]string) string {
	var keys []string
	for k := range cat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return entries(keys...)
}

func sizeOf(cat map[string]string) string {
	if len(cat) >= manyKeys {
		return entries("many")
	}
	return entries("few")
}

func messagesOf(cat map[string]string) string {
	var keys []string
	for k := range cat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, k, cat[k])
	}
	return jsonOf(pairs...)
}

type fakeGen struct {
	gen      *Generators
	list     int
	write    int
	listedAt []string
}

func fakeGenerators(failure string) *fakeGen {
	f := &fakeGen{}
	install := func(tree string) { os.MkdirAll(filepath.Join(tree, "node_modules", "x"), 0o755) }
	f.gen = &Generators{
		List: func(tree string) ([]string, error) {
			f.list++
			data, _ := os.ReadFile(filepath.Join(tree, catalog))
			f.listedAt = append(f.listedAt, string(data))
			install(tree)
			return []string{generated, size, messages}, nil
		},
		Write: func(tree string) ([]string, error) {
			f.write++
			install(tree)
			if failure != "" {
				return nil, stop("pnpm generate failed; nothing was pushed:\n" + failure)
			}
			data, _ := os.ReadFile(filepath.Join(tree, catalog))
			cat := map[string]string{}
			json.Unmarshal(data, &cat)
			put := func(path, text string) {
				os.MkdirAll(filepath.Dir(filepath.Join(tree, path)), 0o755)
				os.WriteFile(filepath.Join(tree, path), []byte(text), 0o644)
			}
			put(generated, keysOf(cat))
			put(size, sizeOf(cat))
			put(messages, messagesOf(cat))
			if len(cat) < manyKeys {
				return []string{generated, size, messages}, nil
			}
			put(plurals, sizeOf(cat))
			return []string{generated, size, messages, plurals}, nil
		},
	}
	return f
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true")
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if e, ok := err.(*exec.ExitError); ok {
			stderr = string(e.Stderr)
		}
		t.Fatalf("git %s: %v %s", strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(string(out))
}

func isolateGit(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	os.WriteFile(empty, nil, 0o644)
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

var (
	templateOnce sync.Once
	templateDir  string
	templateErr  error
)

func template(t *testing.T) string {
	t.Helper()
	templateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mingo-team-template-")
		if err != nil {
			templateErr = err
			return
		}
		templateDir = dir
		origin, clone := filepath.Join(dir, "origin.git"), filepath.Join(dir, "clone")
		os.Mkdir(filepath.Join(dir, "temp"), 0o755)
		runGit(t, dir, "init", "--quiet", "--bare", "--initial-branch=master", origin)
		runGit(t, dir, "clone", "--quiet", origin, clone)
		runGit(t, clone, "config", "user.name", "Team Test")
		runGit(t, clone, "config", "user.email", "team@example.test")
		runGit(t, clone, "config", "commit.gpgsign", "false")
		runGit(t, clone, "symbolic-ref", "HEAD", "refs/heads/master")
		for path, text := range map[string]string{list: listStart, catalog: jsonOf("a.one", "One"), appFile: entries("app")} {
			os.MkdirAll(filepath.Dir(filepath.Join(clone, path)), 0o755)
			os.WriteFile(filepath.Join(clone, path), []byte(text), 0o644)
		}
		runGit(t, clone, "add", "--all")
		runGit(t, clone, "commit", "--quiet", "-m", "start")
		runGit(t, clone, "push", "--quiet", "origin", "master")
		runGit(t, clone, "branch", "feature")
		runGit(t, clone, "push", "--quiet", "origin", "feature")
	})
	if templateErr != nil {
		t.Fatal(templateErr)
	}
	return templateDir
}

type repo struct {
	t       *testing.T
	home    string
	origin  string
	clone   string
	temp    string
	out     *bytes.Buffer
	project *config.Project
	gen     *Generators
	run     Runner
}

func setUpRepo(t *testing.T, gen *Generators) *repo {
	t.Helper()
	isolateGit(t)
	tmpl := template(t)
	home := t.TempDir()
	if err := os.CopyFS(home, os.DirFS(tmpl)); err != nil {
		t.Fatal(err)
	}
	r := &repo{t: t, home: home, origin: filepath.Join(home, "origin.git"), clone: filepath.Join(home, "clone"), temp: filepath.Join(home, "temp"), out: &bytes.Buffer{}, gen: gen, run: Capture}
	if r.gen == nil {
		r.gen = fakeGenerators("").gen
	}
	r.git(r.clone, "remote", "set-url", "origin", r.origin)
	r.project = loadProject(t, "acme", "[workflow]\nbase = \"master\"\n"+sharedTOML+"\n[[roles]]\nname = \"lead\"\n")
	return r
}

func (r *repo) git(dir string, args ...string) string {
	r.t.Helper()
	return runGit(r.t, dir, args...)
}

func (r *repo) commit(branch string, files map[string]string, message string) {
	r.t.Helper()
	if r.git(r.clone, "branch", "--show-current") != branch {
		r.git(r.clone, "checkout", "--quiet", branch)
	}
	for path, text := range files {
		if text == del {
			r.git(r.clone, "rm", "--quiet", path)
			continue
		}
		full := filepath.Join(r.clone, path)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			r.t.Fatal(err)
		}
		r.git(r.clone, "add", "--", path)
	}
	r.git(r.clone, "commit", "--quiet", "-m", message)
	r.git(r.clone, "push", "--quiet", "origin", branch)
}

func (r *repo) remoteHead(branch string) string {
	return r.git(r.origin, "rev-parse", "refs/heads/"+branch)
}

func (r *repo) show(branch, path string) string {
	return r.git(r.origin, "show", branch+":"+path) + "\n"
}

func (r *repo) resetFeatureToMaster() {
	r.git(r.clone, "branch", "-f", "feature", "master")
	r.git(r.clone, "push", "--quiet", "--force", "origin", "feature")
}

func (r *repo) rebase(args ...string) int {
	r.t.Helper()
	r.out.Reset()
	code, err := Rebase(RebaseDeps{Root: r.clone, Project: r.project, Run: r.run, Out: r.out, TempRoot: r.temp, Generators: r.gen}, args)
	if err != nil {
		r.t.Fatal(err)
	}
	return code
}

func (r *repo) printed() []string {
	text := strings.TrimSuffix(r.out.String(), "\n")
	if text == "" {
		return nil
	}
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "  ") && len(out) > 0 {
			out[len(out)-1] += "\n" + l
		} else {
			out = append(out, l)
		}
	}
	return out
}

func (r *repo) expectClean(branch string) {
	r.t.Helper()
	left, _ := os.ReadDir(r.temp)
	if len(left) != 0 {
		r.t.Fatal("left in the temp folder", left)
	}
	if n := len(strings.Split(r.git(r.clone, "worktree", "list"), "\n")); n != 1 {
		r.t.Fatal("worktrees", n)
	}
	if b := r.git(r.clone, "branch", "--show-current"); b != branch {
		r.t.Fatal(b)
	}
	if s := r.git(r.clone, "status", "--porcelain"); s != "" {
		r.t.Fatal(s)
	}
}

func hasLine(lines []string, line string) bool {
	return contains(lines, line)
}

func last(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

func TestTheRepositoryEachTestStartsFromIsItsOwn(t *testing.T) {
	first, second := setUpRepo(t, nil), setUpRepo(t, nil)
	if first.git(first.clone, "remote", "get-url", "origin") != first.origin || second.git(second.clone, "remote", "get-url", "origin") != second.origin {
		t.Fatal("remotes")
	}
	start := first.remoteHead("master")
	first.commit("feature", map[string]string{appFile: entries("app", "first only")}, "first only")
	first.commit("master", map[string]string{appFile: entries("master", "app")}, "first's master")
	if first.remoteHead("feature") == start || second.remoteHead("master") != start || second.remoteHead("feature") != start {
		t.Fatal("not its own")
	}
	if runGit(t, filepath.Join(template(t), "origin.git"), "rev-parse", "refs/heads/master") != start {
		t.Fatal("the template moved")
	}
}

func TestTheRepositoryHoldsAListACatalogAndACodeFile(t *testing.T) {
	r := setUpRepo(t, nil)
	if r.show("master", list) != listStart || r.show("master", catalog) != jsonOf("a.one", "One") || r.show("feature", appFile) != entries("app") {
		t.Fatal("contents")
	}
	r.expectClean("master")
}

func TestRebaseKeepsBothSidesOfAConflictInAnAppendOnlyListMastersFirst(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{list: withEntries("- feature")}, "feature entry")
	r.commit("master", map[string]string{list: withEntries("- master")}, "master entry")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", list) != withEntries("- master", "- feature") {
		t.Fatal(r.show("feature", list))
	}
	r.git(r.clone, "merge-base", "--is-ancestor", r.remoteHead("master"), r.remoteHead("feature"))
	if !hasLine(r.printed(), "kept both sides, master's first: "+list) || last(r.printed()) != "master brought no code into files the branch touches: no new gate is due" {
		t.Fatal(r.printed())
	}
	r.expectClean("master")
}

func TestRebaseResolvesEveryStopOfABranchWhoseCommitsEachMeetMasterInTheList(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{list: withEntries("- f1")}, "first")
	r.commit("feature", map[string]string{list: withEntries("- f1", "- f2"), appFile: entries("app", "feature")}, "second")
	r.commit("master", map[string]string{list: withEntries("- m1")}, "master entry")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", list) != withEntries("- m1", "- f1", "- f2") || r.git(r.clone, "rev-list", "--count", "origin/master..origin/feature") != "2" {
		t.Fatal(r.show("feature", list))
	}
}

func TestRebaseTakesTheEntryALaterCommitRewroteBehindWhatMasterAppended(t *testing.T) {
	r := setUpRepo(t, nil)
	var twelve []string
	for i := 1; i <= 12; i++ {
		twelve = append(twelve, "- m"+strconv.Itoa(i))
	}
	r.commit("feature", map[string]string{list: withEntries("- f1", "- f2")}, "first")
	r.commit("feature", map[string]string{list: withEntries("- f1, rewritten", "- f2")}, "second")
	r.commit("master", map[string]string{list: withEntries(twelve...)}, "master entries")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", list) != withEntries(append(twelve, "- f1, rewritten", "- f2")...) {
		t.Fatal(r.show("feature", list))
	}
	equal(t, r.printed()[1:], []string{
		"kept both sides, master's first: " + list,
		"took an entry the branch rewrote or removed: " + list,
		"master changed a file the branch touches: " + list,
		"master brought no code into files the branch touches: no new gate is due",
	})
	r.expectClean("master")
}

func TestRebaseTakesALaterCommitThatRewroteBothEntriesRemovedOneOrAppendedAThird(t *testing.T) {
	for _, c := range []struct{ later, result []string }{
		{[]string{"- f1x", "- f2x"}, []string{"- m1", "- f1x", "- f2x"}},
		{[]string{"- f2"}, []string{"- m1", "- f2"}},
		{[]string{"- f1x", "- f3", "- f2"}, []string{"- m1", "- f1x", "- f3", "- f2"}},
		{[]string{"- f1", "- f2x", "- f3"}, []string{"- m1", "- f1", "- f2x", "- f3"}},
	} {
		r := setUpRepo(t, nil)
		r.commit("feature", map[string]string{list: withEntries("- f1", "- f2")}, "first")
		r.commit("feature", map[string]string{list: withEntries(c.later...)}, "second")
		r.commit("master", map[string]string{list: withEntries("- m1")}, "master entry")
		if code := r.rebase("feature"); code != ExitOK || r.show("feature", list) != withEntries(c.result...) {
			t.Fatal(code, r.printed(), r.show("feature", list))
		}
	}
}

func TestRebaseTakesAnEntryMasterRewroteWhileTheBranchAppendedBehindIt(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{list: withEntries("- feature")}, "feature entry")
	r.commit("master", map[string]string{list: strings.Replace(listStart, "- one", "- one, by master", 1)}, "master")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", list) != strings.Replace(listStart, "- one\n", entries("- one, by master", "- feature"), 1) || !hasLine(r.printed(), "took an entry master rewrote or removed: "+list) {
		t.Fatal(r.show("feature", list), r.printed())
	}
}

func TestRebaseStopsWhereALaterCommitRewroteAnEntryMasterRewroteOrRemoved(t *testing.T) {
	for _, masters := range []string{withEntries("- f1, by master"), listStart} {
		r := setUpRepo(t, nil)
		r.commit("master", map[string]string{list: withEntries("- f1")}, "the entry")
		r.resetFeatureToMaster()
		r.commit("feature", map[string]string{list: withEntries("- f1", "- f2")}, "first")
		r.commit("feature", map[string]string{list: withEntries("- f1, by the feature", "- f2")}, "second")
		r.commit("master", map[string]string{list: masters}, "master")
		before := r.remoteHead("feature")
		if code := r.rebase("feature"); code != ExitFailed {
			t.Fatal(code)
		}
		equal(t, r.printed(), []string{"a conflict that is the author's to resolve; nothing was pushed:\n  " + list + ": both sides changed or removed a line that was already there"})
		if r.remoteHead("feature") != before {
			t.Fatal("pushed")
		}
		r.expectClean("master")
	}
}

func TestRebaseKeepsAListsCRLFLineEndsAndAListWithoutAFinalNewlineWithoutOne(t *testing.T) {
	other := "docs/product/open-questions.md"
	forms := []func(...string) string{
		func(e ...string) string { return strings.Join(e, "\r\n") + "\r\n" },
		func(e ...string) string { return strings.Join(e, "\n") },
	}
	for _, form := range forms {
		r := setUpRepo(t, nil)
		r.commit("master", map[string]string{other: form("# List", "- one")}, "the list")
		r.resetFeatureToMaster()
		r.commit("feature", map[string]string{other: form("# List", "- one", "- f1")}, "first")
		r.commit("feature", map[string]string{other: form("# List", "- one", "- f1, rewritten")}, "second")
		r.commit("master", map[string]string{other: form("# List", "- one", "- m1")}, "master entry")
		if code := r.rebase("feature"); code != ExitOK {
			t.Fatal(code, r.printed())
		}
		want := form("# List", "- one", "- m1", "- f1, rewritten")
		if got := r.git(r.clone, "cat-file", "-s", "origin/feature:"+other); got != strconv.Itoa(len(want)) {
			t.Fatal(got, len(want), r.show("feature", other))
		}
		if r.show("feature", other) != strings.TrimRight(want, "\r\n")+"\n" {
			t.Fatalf("%q", r.show("feature", other))
		}
	}
}

func TestRebaseTakesACatalogKeyALaterCommitOfTheBranchChanged(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{catalog: jsonOf("a.one", "One", "f.x", "F")}, "first")
	r.commit("feature", map[string]string{catalog: jsonOf("a.one", "One", "f.x", "F, rewritten")}, "second")
	r.commit("master", map[string]string{catalog: jsonOf("a.one", "One", "m.x", "M")}, "master key")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", catalog) != jsonOf("a.one", "One", "m.x", "M", "f.x", "F, rewritten") {
		t.Fatal(r.show("feature", catalog))
	}
	equal(t, r.printed()[1:3], []string{"kept both sides, master's first: " + catalog, "took an entry the branch rewrote or removed: " + catalog})
}

func TestRebaseMergesTheKeysBothSidesAppendedToACatalogIntoValidJSON(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{catalog: jsonOf("a.one", "One", "f.x", "F")}, "feature key")
	r.commit("master", map[string]string{catalog: jsonOf("a.one", "One", "m.x", "M")}, "master key")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", catalog) != jsonOf("a.one", "One", "m.x", "M", "f.x", "F") || last(r.printed()) != "master brought code into files the branch touches: the full gate runs again" {
		t.Fatal(r.show("feature", catalog), r.printed())
	}
}

func TestRebaseStopsOnACatalogKeyBothSidesAddedWithTwoValues(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{catalog: jsonOf("a.one", "One", "x", "F")}, "feature key")
	r.commit("master", map[string]string{catalog: jsonOf("a.one", "One", "x", "M")}, "master key")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed || !strings.Contains(r.out.String(), catalog+`: both sides added the key "x" with another value`) {
		t.Fatal(code, r.printed())
	}
	if r.remoteHead("feature") != before {
		t.Fatal("pushed")
	}
	r.expectClean("master")
}

func TestRebaseStopsOnAConflictOutsideTheListsWithTheFileNamedAndNothingPushed(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{appFile: entries("app", "feature"), list: withEntries("- feature")}, "feature")
	r.commit("master", map[string]string{appFile: entries("app", "master"), list: withEntries("- master")}, "master")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed {
		t.Fatal(code)
	}
	equal(t, r.printed(), []string{"a conflict that is the author's to resolve; nothing was pushed:\n  " + appFile + ": not an append-only list"})
	if r.remoteHead("feature") != before {
		t.Fatal("pushed")
	}
	r.expectClean("master")
}

func TestRebaseStopsOnAListWhereBothSidesChangedALineThatWasThere(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{list: strings.Replace(listStart, "- one", "- one, by the feature", 1)}, "feature")
	r.commit("master", map[string]string{list: strings.Replace(listStart, "- one", "- one, by master", 1)}, "master")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed || !strings.Contains(r.out.String(), list+": both sides changed or removed a line that was already there") {
		t.Fatal(code, r.printed())
	}
	if r.remoteHead("feature") != before {
		t.Fatal("pushed")
	}
	r.expectClean("master")
}

func TestRebaseStopsOnAListOneSideDeleted(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{list: withEntries("- feature")}, "feature")
	r.commit("master", map[string]string{list: del}, "master drops the list")
	if code := r.rebase("feature"); code != ExitFailed || !strings.Contains(r.out.String(), list+": one side deleted the file") {
		t.Fatal(code, r.printed())
	}
	r.expectClean("master")
}

func TestRebaseSaysABranchThatHoldsMasterIsUpToDateAndPushesNothing(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{appFile: entries("app", "feature")}, "feature")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code)
	}
	if p := r.printed(); len(p) != 1 || !strings.Contains(p[0], "feature is up to date with master") {
		t.Fatal(p)
	}
	if r.remoteHead("feature") != before {
		t.Fatal("pushed")
	}
	r.expectClean("feature")
}

func TestRebaseReportsCodeMasterBroughtIntoAFileTheBranchTouches(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{appFile: entries("app", "", "", "", "feature")}, "feature")
	r.commit("master", map[string]string{appFile: entries("master", "app"), "apps/api/src/other.ts": entries("x")}, "master")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	equal(t, r.printed()[1:], []string{"master changed a file the branch touches: " + appFile, "master brought code into files the branch touches: the full gate runs again"})
	if r.show("feature", appFile) != entries("master", "app", "", "", "", "feature") {
		t.Fatal(r.show("feature", appFile))
	}
}

func TestRebaseAsksForNoGateWhenMasterChangedOnlyOtherFiles(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{appFile: entries("app", "feature")}, "feature")
	r.commit("master", map[string]string{"apps/api/src/other.ts": entries("x")}, "master")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code)
	}
	if p := r.printed(); len(p) != 2 || p[1] != "master brought no code into files the branch touches: no new gate is due" {
		t.Fatal(p)
	}
	if r.show("feature", "apps/api/src/other.ts") != entries("x") {
		t.Fatal("other")
	}
}

func TestRebaseReportsAFileTheBranchTouchesThatMasterRenamed(t *testing.T) {
	r := setUpRepo(t, nil)
	moved := "apps/api/src/moved.ts"
	body := entries("one", "two", "three", "four", "five", "six")
	r.commit("master", map[string]string{appFile: body}, "a longer file")
	r.resetFeatureToMaster()
	r.commit("feature", map[string]string{appFile: body + "feature\n"}, "feature")
	r.commit("master", map[string]string{appFile: del, moved: "master\n" + body}, "master renames it")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if !hasLine(r.printed(), "master changed a file the branch touches: "+appFile) || last(r.printed()) != "master brought code into files the branch touches: the full gate runs again" {
		t.Fatal(r.printed())
	}
	if r.show("feature", moved) != "master\n"+body+"feature\n" {
		t.Fatal(r.show("feature", moved))
	}
}

func TestRebaseRefusesABranchMasterAlreadyHolds(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("master", map[string]string{appFile: entries("app", "master")}, "master")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed {
		t.Fatal(code)
	}
	equal(t, r.printed(), []string{"feature has no commits of its own: master already holds it"})
	if r.remoteHead("feature") != before {
		t.Fatal("pushed")
	}
	r.expectClean("master")
}

func TestRebaseStopsWithNothingPushedWhenGitMergeFileFails(t *testing.T) {
	for _, answer := range []Result{{Code: 129}, {Code: 129, Stdout: "half a file\n"}, {Code: 1}} {
		r := setUpRepo(t, nil)
		r.commit("feature", map[string]string{list: withEntries("- feature")}, "feature entry")
		r.commit("master", map[string]string{list: withEntries("- master")}, "master entry")
		before := r.remoteHead("feature")
		r.run = func(o Options, name string, args ...string) (Result, error) {
			if len(args) > 0 && args[0] == "merge-file" {
				a := answer
				a.Stderr = "error: unknown option `zdiff3'"
				return a, nil
			}
			return Capture(o, name, args...)
		}
		if code := r.rebase("feature"); code != ExitFailed || !strings.Contains(r.out.String(), "git merge-file failed on "+list) {
			t.Fatal(answer, code, r.printed())
		}
		if r.remoteHead("feature") != before {
			t.Fatal("pushed")
		}
		r.expectClean("master")
	}
}

func TestRebaseRemovesTheWorktreesFolderEvenWhenGitCannotRemoveTheWorktree(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{appFile: entries("app", "feature")}, "feature")
	r.commit("master", map[string]string{"apps/api/src/other.ts": entries("x")}, "master")
	r.run = func(o Options, name string, args ...string) (Result, error) {
		if len(args) > 1 && args[0] == "worktree" && args[1] == "remove" {
			return Result{TimedOut: true}, nil
		}
		return Capture(o, name, args...)
	}
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	r.expectClean("master")
}

func TestRebaseRefusesABranchOriginDoesNotHaveAndMasterItself(t *testing.T) {
	r := setUpRepo(t, nil)
	var printed []string
	for _, b := range []string{"nowhere", "master"} {
		if code := r.rebase(b); code != ExitFailed {
			t.Fatal(code)
		}
		printed = append(printed, r.printed()...)
	}
	equal(t, printed, []string{"origin has no branch nowhere", "master is never rebased"})
	r.expectClean("master")
}

func TestRebaseRefusesWrongArguments(t *testing.T) {
	r := setUpRepo(t, nil)
	for _, args := range [][]string{nil, {"a", "b"}, {"--force"}} {
		if code := r.rebase(args...); code != ExitUsage {
			t.Fatal(args, code)
		}
		equal(t, r.printed(), []string{"usage: mingo pr rebase <branch>"})
	}
}

func TestRebaseRefusesThePushWhenTheBranchMovedAfterTheFetch(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{appFile: entries("app", "feature")}, "feature")
	r.commit("master", map[string]string{"apps/api/src/other.ts": entries("x")}, "master")
	r.run = func(o Options, name string, args ...string) (Result, error) {
		if len(args) > 1 && args[0] == "rev-parse" && args[1] == "HEAD" {
			r.commit("feature", map[string]string{appFile: entries("app", "feature", "more")}, "the author's next commit")
		}
		return Capture(o, name, args...)
	}
	if code := r.rebase("feature"); code != ExitFailed || !strings.Contains(r.out.String(), "the push was refused") {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", appFile) != entries("app", "feature", "more") || r.remoteHead("feature") != r.git(r.clone, "rev-parse", "feature") {
		t.Fatal("the author's commit was lost")
	}
}

// a catalog in the order its keys were added, as the tests write them
type orderedCatalog struct {
	keys []string
	vals map[string]string
}

func cat(pairs ...string) orderedCatalog {
	c := orderedCatalog{vals: map[string]string{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		c.keys = append(c.keys, pairs[i])
		c.vals[pairs[i]] = pairs[i+1]
	}
	return c
}

func (c orderedCatalog) with(pairs ...string) orderedCatalog {
	var all []string
	for _, k := range c.keys {
		all = append(all, k, c.vals[k])
	}
	return cat(append(all, pairs...)...)
}

func (c orderedCatalog) json() string {
	var pairs []string
	for _, k := range c.keys {
		pairs = append(pairs, k, c.vals[k])
	}
	return jsonOf(pairs...)
}

func (c orderedCatalog) files() map[string]string {
	return map[string]string{catalog: c.json(), generated: keysOf(c.vals), size: sizeOf(c.vals), messages: messagesOf(c.vals)}
}

var startCatalog = cat("a.one", "One")

func setUpGenerated(t *testing.T, start orderedCatalog, gen *Generators) *repo {
	r := setUpRepo(t, gen)
	r.commit("master", start.files(), "generated files")
	r.resetFeatureToMaster()
	return r
}

func union(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func TestAGeneratedFileIsMadeAgainFromTheCatalogMergedInTheSameStep(t *testing.T) {
	fake := fakeGenerators("")
	r := setUpGenerated(t, startCatalog, fake.gen)
	r.commit("feature", startCatalog.with("b.feature", "B").files(), "feature text")
	r.commit("master", startCatalog.with("c.master", "C").files(), "master text")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	merged := startCatalog.with("c.master", "C", "b.feature", "B")
	p := r.printed()
	if r.show("feature", catalog) != merged.json() || r.show("feature", generated) != keysOf(merged.vals) || r.show("feature", size) != entries("many") {
		t.Fatal(r.show("feature", catalog), r.show("feature", generated), r.show("feature", size))
	}
	for _, line := range []string{
		"made again by the generators: " + generated,
		"made again by the generators: " + size,
		"made again by the generators: " + messages,
		"made again by the generators: " + plurals,
		"kept both sides, master's first: " + catalog,
	} {
		if !hasLine(p, line) {
			t.Fatal(line, p)
		}
	}
	if r.show("feature", messages) != messagesOf(merged.vals) || hasLine(p, "kept both sides, master's first: "+messages) || r.show("feature", plurals) != entries("many") {
		t.Fatal(p)
	}
	if last(p) != "master brought code into files the branch touches: the full gate runs again" || fake.list != 1 || fake.write != 1 {
		t.Fatal(p, fake.list, fake.write)
	}
	if !reflect.DeepEqual(fake.listedAt, []string{startCatalog.with("c.master", "C").json()}) {
		t.Fatal(fake.listedAt)
	}
	r.expectClean("master")
}

func TestAGeneratedFileIsMadeAgainWhenGitCouldHaveMergedBothSidesByText(t *testing.T) {
	fake := fakeGenerators("")
	var pairs []string
	for _, k := range []string{"k1", "k2", "k3", "k4", "k5", "k6", "k7", "k8"} {
		pairs = append(pairs, k, k)
	}
	many := cat(pairs...)
	r := setUpGenerated(t, many, fake.gen)
	r.commit("feature", cat(append([]string{"a", "first"}, pairs...)...).files(), "feature, at the start")
	r.commit("master", many.with("z", "last").files(), "master, at the end")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", generated) != keysOf(union(many.vals, map[string]string{"a": "first", "z": "last"})) || !hasLine(r.printed(), "made again by the generators: "+generated) || fake.write != 1 {
		t.Fatal(r.show("feature", generated), r.printed(), fake.write)
	}
}

func TestAGeneratedFileIsMadeAgainAtEachStepOfABranchWhoseTwoCommitsMeetIt(t *testing.T) {
	fake := fakeGenerators("")
	r := setUpGenerated(t, startCatalog, fake.gen)
	r.commit("feature", startCatalog.with("b.one", "1").files(), "first")
	r.commit("feature", startCatalog.with("b.one", "1", "b.two", "2").files(), "second")
	r.commit("master", startCatalog.with("c.master", "C").files(), "master text")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	want := keysOf(map[string]string{"a.one": "", "b.one": "", "b.two": "", "c.master": ""})
	if r.show("feature", generated) != want || r.git(r.clone, "rev-list", "--count", "origin/master..origin/feature") != "2" || fake.write != 2 {
		t.Fatal(r.show("feature", generated), fake.write)
	}
}

func TestAGeneratedFileStopsBeforeGeneratingWhenAnotherFileOfTheStepIsTheAuthors(t *testing.T) {
	fake := fakeGenerators("")
	r := setUpGenerated(t, startCatalog, fake.gen)
	r.commit("feature", union(startCatalog.with("b.feature", "B").files(), map[string]string{appFile: entries("feature")}), "feature")
	r.commit("master", union(startCatalog.with("c.master", "C").files(), map[string]string{appFile: entries("master")}), "master")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed {
		t.Fatal(code)
	}
	out := r.out.String()
	if !strings.Contains(out, appFile+": not an append-only list") || strings.Contains(out, generated) || fake.write != 0 || r.remoteHead("feature") != before {
		t.Fatal(out, fake.write)
	}
	r.expectClean("master")
}

func TestAGeneratedFileStopsWithTheGeneratorsLastLinesWhenItFails(t *testing.T) {
	fake := fakeGenerators("boom")
	r := setUpGenerated(t, startCatalog, fake.gen)
	r.commit("feature", startCatalog.with("b.feature", "B").files(), "feature text")
	r.commit("master", startCatalog.with("c.master", "C").files(), "master text")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed {
		t.Fatal(code)
	}
	if r.out.String() != "pnpm generate failed; nothing was pushed:\nboom\n" || r.remoteHead("feature") != before {
		t.Fatal(r.out.String())
	}
	r.expectClean("master")
}

func TestAGeneratedFileStopsWhenTheGeneratorsCannotListTheirFiles(t *testing.T) {
	fake := fakeGenerators("")
	failing := &Generators{
		List:  func(string) ([]string, error) { return nil, stop("installing failed; nothing was pushed:\nno store") },
		Write: fake.gen.Write,
	}
	r := setUpGenerated(t, startCatalog, failing)
	r.commit("feature", startCatalog.with("b.feature", "B").files(), "feature text")
	r.commit("master", startCatalog.with("c.master", "C").files(), "master text")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed || !strings.Contains(r.out.String(), "no store") || r.remoteHead("feature") != before {
		t.Fatal(code, r.out.String())
	}
	r.expectClean("master")
}

func TestAGeneratedFileOneSideDeletedIsTheAuthorsToResolve(t *testing.T) {
	fake := fakeGenerators("")
	r := setUpGenerated(t, startCatalog, fake.gen)
	r.commit("feature", startCatalog.with("b.feature", "B").files(), "feature text")
	r.commit("master", map[string]string{generated: del}, "master drops it")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed || !strings.Contains(r.out.String(), generated+": one side deleted the generated file") || fake.write != 0 || r.remoteHead("feature") != before {
		t.Fatal(code, r.out.String(), fake.write)
	}
	r.expectClean("master")
}

func TestAGeneratedFileOnlyOneSideChangedIsTakenAsGitMergedIt(t *testing.T) {
	fake := fakeGenerators("")
	r := setUpGenerated(t, startCatalog, fake.gen)
	r.commit("feature", union(startCatalog.with("b.feature", "B").files(), map[string]string{list: withEntries("- feature")}), "feature text")
	r.commit("master", map[string]string{list: withEntries("- master")}, "master entry")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", generated) != keysOf(startCatalog.with("b.feature", "B").vals) || fake.list != 0 || fake.write != 0 || strings.Contains(r.out.String(), "made again") {
		t.Fatal(r.printed(), fake.list, fake.write)
	}
}

func TestAGeneratedFileGitIgnoresIsNeverStaged(t *testing.T) {
	ignored := "apps/studio/src/routeTree.gen.ts"
	fake := fakeGenerators("")
	withIgnored := &Generators{
		List: fake.gen.List,
		Write: func(tree string) ([]string, error) {
			written, err := fake.gen.Write(tree)
			if err != nil {
				return nil, err
			}
			os.MkdirAll(filepath.Dir(filepath.Join(tree, ignored)), 0o755)
			os.WriteFile(filepath.Join(tree, ignored), []byte(entries("tree")), 0o644)
			return append(written, ignored), nil
		},
	}
	r := setUpRepo(t, withIgnored)
	r.commit("master", union(startCatalog.files(), map[string]string{".gitignore": ignored + "\n"}), "generated files")
	r.resetFeatureToMaster()
	r.commit("feature", startCatalog.with("b.feature", "B").files(), "feature text")
	r.commit("master", startCatalog.with("c.master", "C").files(), "master text")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", generated) != keysOf(startCatalog.with("c.master", "C", "b.feature", "B").vals) {
		t.Fatal(r.show("feature", generated))
	}
	if strings.Contains(r.git(r.clone, "ls-tree", "-r", "--name-only", "feature"), ignored) || hasLine(r.printed(), "made again by the generators: "+ignored) {
		t.Fatal(r.printed())
	}
	r.expectClean("master")
}

func TestAGeneratedFileTheGeneratorsNoLongerWriteStops(t *testing.T) {
	fake := fakeGenerators("")
	forgetting := &Generators{
		List: fake.gen.List,
		Write: func(tree string) ([]string, error) {
			written, err := fake.gen.Write(tree)
			var out []string
			for _, p := range written {
				if p != generated {
					out = append(out, p)
				}
			}
			return out, err
		},
	}
	r := setUpGenerated(t, startCatalog, forgetting)
	r.commit("feature", startCatalog.with("b.feature", "B").files(), "feature text")
	r.commit("master", startCatalog.with("c.master", "C").files(), "master text")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed || !strings.Contains(r.out.String(), generated+": the generators no longer write it") || r.remoteHead("feature") != before {
		t.Fatal(code, r.out.String())
	}
	r.expectClean("master")
}

func TestAGeneratedPathAnAttributesLineCannotNameStopsBeforeTheReplay(t *testing.T) {
	odd := "apps/api/odd name.ts"
	fake := fakeGenerators("")
	listing := &Generators{List: func(string) ([]string, error) { return []string{odd}, nil }, Write: fake.gen.Write}
	r := setUpRepo(t, listing)
	r.commit("feature", map[string]string{odd: entries("feature")}, "feature")
	r.commit("master", map[string]string{odd: entries("master")}, "master")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed {
		t.Fatal(code)
	}
	equal(t, r.printed(), []string{"a generated path git attributes cannot name: " + odd})
	if r.remoteHead("feature") != before {
		t.Fatal("pushed")
	}
	r.expectClean("master")
}

func TestConfigGeneratorsRunTheirCommandsInOrderAndReadTheLastOnesOutput(t *testing.T) {
	p := loadProject(t, "demo", `[workflow]
generate_list = ["first a", "second b"]
generate = ["first c", "third d"]
generated = '^generate: wrote (.+)$'
[[roles]]
name = "lead"
`)
	var ran []string
	run := func(o Options, name string, args ...string) (Result, error) {
		ran = append(ran, name+" "+strings.Join(args, " "))
		switch name {
		case "second":
			return Result{Stdout: " x.ts \n\ny.ts\n"}, nil
		case "third":
			return Result{Stdout: "generate: wrote a.ts\nnoise\ngenerate: wrote b.ts\ngenerate: wrote a.ts\n"}, nil
		}
		return Result{Stdout: "ignored\n"}, nil
	}
	g := ConfigGenerators(&p.Workflow, run)
	listed, err := g.List("/tree")
	if err != nil || !reflect.DeepEqual(listed, []string{"x.ts", "y.ts"}) {
		t.Fatal(listed, err)
	}
	written, err := g.Write("/tree")
	if err != nil || !reflect.DeepEqual(written, []string{"a.ts", "b.ts"}) {
		t.Fatal(written, err)
	}
	equal(t, ran, []string{"first a", "second b", "first c", "third d"})
	fail := func(o Options, name string, args ...string) (Result, error) {
		return Result{Code: 1, Stdout: "out\n", Stderr: "boom\n"}, nil
	}
	if _, err := ConfigGenerators(&p.Workflow, fail).Write("/tree"); err == nil || err.Error() != "first c failed; nothing was pushed:\nout\nboom" {
		t.Fatal(err)
	}
	missing := func(o Options, name string, args ...string) (Result, error) { return Result{NotFound: true}, nil }
	if _, err := ConfigGenerators(&p.Workflow, missing).List("/tree"); err == nil || err.Error() != "first is not installed" {
		t.Fatal(err)
	}
	if ConfigGenerators(&loadProject(t, "bare", "[[roles]]\nname = \"lead\"\n").Workflow, run) != nil {
		t.Fatal("generators without commands")
	}
}

func TestRebaseSkipsAStepTheResolutionLeavesWithNoChangeOfItsOwn(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{list: withEntries("- x")}, "an entry master has too")
	r.commit("feature", map[string]string{appFile: entries("app", "feature")}, "the feature")
	r.commit("master", map[string]string{list: withEntries("- x", "- m")}, "master's entries")
	if code := r.rebase("feature"); code != ExitOK {
		t.Fatal(code, r.printed())
	}
	if r.show("feature", list) != withEntries("- x", "- m") || r.show("feature", appFile) != entries("app", "feature") {
		t.Fatal(r.show("feature", list))
	}
	if n := r.git(r.clone, "rev-list", "--count", "origin/master..origin/feature"); n != "1" {
		t.Fatal(n)
	}
	r.expectClean("master")
}

func TestRebaseRefusesToPushABranchWhoseEveryCommitMasterHolds(t *testing.T) {
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{list: withEntries("- x")}, "an entry master has too")
	r.commit("master", map[string]string{list: withEntries("- x", "- m")}, "master's entries")
	before := r.remoteHead("feature")
	if code := r.rebase("feature"); code != ExitFailed {
		t.Fatal(code, r.printed())
	}
	equal(t, r.printed(), []string{"every commit of feature is on master already; nothing was pushed"})
	if r.remoteHead("feature") != before {
		t.Fatal("pushed")
	}
	r.expectClean("master")
}
