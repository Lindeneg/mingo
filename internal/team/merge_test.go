package team

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Lindeneg/mingo/internal/config"
)

const (
	head  = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	older = "ffffffffffffffffffffffffffffffffffffffff"
)

var requiredChecks = []string{"check", "test", "build", "e2e", "e2e-live", "eval-smoke", "security"}

const acmeTOML = `repo = "acme/shop"
clone = "~/dev/acme/{role}"

[tracker]
kind = "linear"
team = "ACM"

[workflow]
base = "master"
checks = ["check", "test", "build", "e2e", "e2e-live", "eval-smoke", "security"]
local_runs = ["pnpm check", "pnpm check:branch", "pnpm gate", "pnpm team:gate"]
tags = ["(lead)", "(senior)", "(junior)"]

[[workflow.shared]]
name = "the tooling table"
pattern = '^docs/tech/tooling\.md$'
merge = "lines"

[[roles]]
name = "lead"
[[roles]]
name = "senior"
[[roles]]
name = "junior"
[[roles]]
name = "mid"
`

func loadProject(t *testing.T, name, text string) *config.Project {
	t.Helper()
	root := t.TempDir()
	t.Setenv("MINGO_CONFIG", root)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", name+".toml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func greenChecks() []Check {
	var out []Check
	for _, name := range requiredChecks {
		out = append(out, Check{Name: name, WorkflowName: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"})
	}
	return out
}

func review(sha, verdict string, at ...string) Comment {
	when := "2026-10-10T10:00:00Z"
	if len(at) > 0 {
		when = at[0]
	}
	return Comment{Body: "review: " + sha + "\n" + verdict + "\n", CreatedAt: when}
}

func pullRequest() PullRequest {
	return PullRequest{
		Number:            41,
		Title:             "ACM-16 tools: the team scripts",
		State:             "OPEN",
		BaseRefName:       "master",
		HeadRefOid:        head,
		Body:              "(senior)\n\nCloses ACM-16\n\n## What changed\n",
		Labels:            []Named{},
		Comments:          []Comment{review(head, "no findings")},
		StatusCheckRollup: greenChecks(),
		Files:             []Path{{"tools/team/merge.ts"}},
		Commits:           []Commit{{""}},
	}
}

func withCheck(name string, change func(*Check)) []Check {
	var out []Check
	for _, c := range greenChecks() {
		if c.Name != name {
			out = append(out, c)
		} else if change != nil {
			change(&c)
			out = append(out, c)
		}
	}
	return out
}

type mergeCase struct {
	t *testing.T
	p *config.Project
}

func newMergeCase(t *testing.T) mergeCase {
	return mergeCase{t, loadProject(t, "acme", acmeTOML)}
}

func (m mergeCase) problems(change func(*PullRequest), rest ...int) []string {
	pr := pullRequest()
	if change != nil {
		change(&pr)
	}
	f := MergeFacts{PullRequest: pr}
	if len(rest) == 2 {
		f.UnresolvedThreads, f.BehindBy = rest[0], rest[1]
	}
	return ListMergeProblems(m.p, f)
}

func equal(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestMergeHoldsNothingOfAPullRequestThatIsGreenReviewedResolvedAndUpToDate(t *testing.T) {
	equal(t, newMergeCase(t).problems(nil), nil)
}

func TestMergeHoldsACheckThatIsNotGreen(t *testing.T) {
	m := newMergeCase(t)
	cases := []struct {
		change  func(*Check)
		problem string
	}{
		{func(c *Check) { c.Conclusion = "FAILURE" }, "the check test is not green: failure"},
		{func(c *Check) { c.Conclusion = "SKIPPED" }, "the check test is not green: skipped"},
		{func(c *Check) { c.Status, c.Conclusion = "IN_PROGRESS", "" }, "the check test is not green: in_progress"},
		{func(c *Check) { c.Status, c.Conclusion = "QUEUED", "" }, "the check test is not green: queued"},
		{nil, "the check test did not run on the head"},
		{func(c *Check) { c.WorkflowName = "another" }, "the check test did not run on the head"},
	}
	for _, c := range cases {
		equal(t, m.problems(func(pr *PullRequest) { pr.StatusCheckRollup = withCheck("test", c.change) }), []string{c.problem})
	}
}

func TestMergeHoldsEveryCheckWhenNoneRan(t *testing.T) {
	if got := newMergeCase(t).problems(func(pr *PullRequest) { pr.StatusCheckRollup = nil }); len(got) != len(requiredChecks) {
		t.Fatal(got)
	}
}

func TestMergeHoldsACheckWithAGreenRunAndARedOneOfTheSameName(t *testing.T) {
	red := greenChecks()[1]
	red.Conclusion = "FAILURE"
	equal(t, newMergeCase(t).problems(func(pr *PullRequest) { pr.StatusCheckRollup = append(greenChecks(), red) }),
		[]string{"the check test is not green: failure"})
}

func TestMergeHoldsAReviewOfAnOlderHead(t *testing.T) {
	equal(t, newMergeCase(t).problems(func(pr *PullRequest) { pr.Comments = []Comment{review(older, "no findings")} }),
		[]string{"the newest review is of fffffff, an older head"})
}

func TestMergeHoldsAReviewThatHasFindings(t *testing.T) {
	equal(t, newMergeCase(t).problems(func(pr *PullRequest) { pr.Comments = []Comment{review(head, "findings: 2")} }),
		[]string{"the review of the head says `findings: 2`, not `no findings`"})
}

func TestMergeHoldsAPullRequestWithNoReviewComment(t *testing.T) {
	equal(t, newMergeCase(t).problems(func(pr *PullRequest) {
		pr.Comments = []Comment{{Body: "looks fine, no findings", CreatedAt: "2026-10-10T10:00:00Z"}}
	}), []string{"not reviewed: no comment begins with `review: <head sha>`"})
}

func TestMergeCountsOnlyTheReviewersCommentsWhenOneIsSet(t *testing.T) {
	m := mergeCase{t, loadProject(t, "acme", strings.Replace(acmeTOML, "[workflow]\n", "[workflow]\nreviewer = \"claude[bot]\"\n", 1))}
	bot := func(c Comment) Comment { c.Author = "claude[bot]"; return c }
	mine := review(head, "no findings")
	mine.Author = "claude"
	equal(t, m.problems(func(pr *PullRequest) { pr.Comments = []Comment{mine} }),
		[]string{"not reviewed: no comment from claude[bot] begins with `review: <head sha>`"})
	equal(t, m.problems(func(pr *PullRequest) { pr.Comments = []Comment{bot(review(head, "no findings")), mine} }), nil)
	held := bot(review(head, "findings: 1", "2026-10-10T10:00:00Z"))
	later := review(head, "no findings", "2026-10-10T11:00:00Z")
	later.Author = "Lindeneg"
	equal(t, m.problems(func(pr *PullRequest) { pr.Comments = []Comment{held, later} }),
		[]string{"the review of the head says `findings: 1`, not `no findings`"})
}

func TestRunMergeReadsEachCommentsAuthorFromGitHub(t *testing.T) {
	p := loadProject(t, "acme", strings.Replace(acmeTOML, "[workflow]\n", "[workflow]\nreviewer = \"claude[bot]\"\n", 1))
	byBot := review(head, "no findings")
	byBot.Author = "claude[bot]"
	if g := newFakeGH(t, p, facts(func(pr *PullRequest) { pr.Comments = []Comment{byBot} }), nil); g.merge("41") != ExitOK {
		t.Fatal(g.printed())
	}
	g := newFakeGH(t, p, facts(nil), nil)
	if g.merge("41") != ExitFailed || !strings.Contains(g.out.String(), "not reviewed: no comment from claude[bot]") {
		t.Fatal(g.printed())
	}
	failed := newFakeGH(t, p, facts(nil), map[string]Result{"api repos/{owner}/{repo}/issues/41/comments": {Code: 1, Stderr: "HTTP 404"}})
	if failed.merge("41") != ExitFailed || len(failed.merges()) != 0 {
		t.Fatal(failed.printed())
	}
	equal(t, failed.printed(), []string{"reading the comments failed:", "HTTP 404"})
}

func TestMergeReadsTheNewestReviewWhateverTheOrderTheCommentsComeIn(t *testing.T) {
	m := newMergeCase(t)
	earlier := review(head, "no findings", "2026-10-10T10:00:00Z")
	later := review(head, "findings: 1", "2026-10-10T11:00:00Z")
	for _, order := range [][]Comment{{later, earlier}, {earlier, later}} {
		if got := m.problems(func(pr *PullRequest) { pr.Comments = order }); len(got) != 1 {
			t.Fatal(got)
		}
	}
	fixed := review(head, "no findings", "2026-10-10T12:00:00Z")
	equal(t, m.problems(func(pr *PullRequest) { pr.Comments = []Comment{fixed, later, earlier} }), nil)
}

func TestMergeTakesNoReviewLineFromTheMiddleOfACommentNorAShortSHA(t *testing.T) {
	quoted := Comment{Body: "> quoted\nreview: " + head + "\nno findings", CreatedAt: "2026-10-10T10:00:00Z"}
	short := Comment{Body: "review: " + head[:7] + "\nno findings", CreatedAt: "2026-10-10T10:00:00Z"}
	if got := newMergeCase(t).problems(func(pr *PullRequest) { pr.Comments = []Comment{quoted, short} }); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestMergeHoldsUnresolvedThreadsAndABranchBehindTheBase(t *testing.T) {
	equal(t, newMergeCase(t).problems(nil, 2, 3), []string{"2 unresolved review thread(s)", "the branch is 3 commit(s) behind master"})
}

func TestMergeHoldsADescriptionWithoutItsFirstLines(t *testing.T) {
	m := newMergeCase(t)
	for body, count := range map[string]int{
		"Closes ACM-16\n\n(senior)\n":            2,
		"(Senior)\n\nCloses ACM-16\n":            1,
		"(senior)\nCloses ACM-16\n":              2,
		"(senior)\n\ncloses ACM-16\n":            1,
		"(senior)\n\nCloses ACM-16 and ACM-17\n": 1,
		"":                                       3,
	} {
		if got := m.problems(func(pr *PullRequest) { pr.Body = body }); len(got) != count {
			t.Fatalf("%q %q", body, got)
		}
	}
}

func TestMergeTakesEveryRolesTagLineAndWindowsLineEnds(t *testing.T) {
	m := newMergeCase(t)
	for _, tag := range m.p.Workflow.Tags {
		equal(t, m.problems(func(pr *PullRequest) { pr.Body = tag + "\r\n\r\nCloses ACM-9\r\n" }), nil)
	}
}

func TestMergeHoldsAClosedOneADraftAndAnotherBase(t *testing.T) {
	equal(t, newMergeCase(t).problems(func(pr *PullRequest) { pr.State, pr.IsDraft, pr.BaseRefName = "MERGED", true, "acm-1-base" }), []string{
		"the pull request is merged, not open",
		"the pull request is a draft",
		"the base is acm-1-base, not master",
	})
}

func TestMergeHoldsADescriptionWithAnAttributionFooter(t *testing.T) {
	m := newMergeCase(t)
	for _, body := range []string{
		"(senior)\n\nCloses ACM-16\n\n## What changed\n\n🤖 Generated with [Claude Code](https://claude.com/claude-code)\n",
		"(senior)\n\nCloses ACM-16\n\n## What changed\n\nhttps://claude.ai/code/session_01HFM4GynXsLQNxVxoeSwSWA\r\n",
	} {
		equal(t, m.problems(func(pr *PullRequest) { pr.Body = body }), []string{"the description carries an attribution footer: remove it"})
	}
}

func TestMergeTakesADescriptionThatNamesClaudeCodeInASentence(t *testing.T) {
	equal(t, newMergeCase(t).problems(func(pr *PullRequest) {
		pr.Body = "(senior)\n\nCloses ACM-16\n\nThe review ran locally with Claude Code over the diff, as generated with the brief.\n"
	}), nil)
}

const record = "(senior)\n\nCloses ACM-16\n\n## Tested\n\n`pnpm gate` green on the head a1b2c3d.\n\n## Review\n\nscoped review: no findings\n"

const notRecorded = "ci-unavailable: the description does not record `pnpm check`, `pnpm check:branch`, `pnpm gate` or `pnpm team:gate`"

func red(pr *PullRequest) {
	pr.Labels = []Named{{CIUnavailableLabel}}
	pr.StatusCheckRollup = withCheck("test", func(c *Check) { c.Conclusion = "FAILURE" })
	pr.Comments = nil
}

func redWith(body string) func(*PullRequest) {
	return func(pr *PullRequest) {
		red(pr)
		pr.Body = body
	}
}

func naming(gate string) string {
	return strings.Replace(record, "`pnpm gate` green", gate+" green", 1)
}

func TestMergeUnderCIUnavailableTakesTheRecordOfTheGateAndTheReviewOnTheHead(t *testing.T) {
	equal(t, newMergeCase(t).problems(redWith(record)), nil)
}

func TestMergeUnderCIUnavailableHoldsADescriptionThatRecordsNeither(t *testing.T) {
	equal(t, newMergeCase(t).problems(red), []string{
		notRecorded,
		"ci-unavailable: the description has no `## Review` section for the local review",
		"ci-unavailable: the description does not name the current head a1b2c3d",
	})
}

func TestMergeUnderCIUnavailableTakesALocalRunNamedAsWholeWords(t *testing.T) {
	m := newMergeCase(t)
	for _, gate := range []string{
		"`pnpm check`", "`pnpm check` and `pnpm test --filter @acme/studio`", "```\npnpm check\n```\n",
		"pnpm check", "pnpm check:", "pnpm check: green", "(pnpm check)", "pnpm check;", "ran pnpm check.",
		"`pnpm check:branch`", "pnpm check:branch: green", "ran pnpm check:branch.", "`pnpm gate`", "`pnpm team:gate`",
		"`pnpm team:gate /tmp/gate.log`", "```\npnpm team:gate gate.log\n```\n", "```\npnpm gate\n```\n", "pnpm team:gate",
		"pnpm gate", "ran pnpm gate, and pnpm team:gate.", "pnpm gate:", "pnpm team:gate: all four targets",
		"(pnpm team:gate)", "pnpm gate;",
	} {
		if got := m.problems(redWith(naming(gate))); len(got) != 0 {
			t.Fatalf("%q %q", gate, got)
		}
	}
}

func TestMergeUnderCIUnavailableTakesTheRunAsTheFirstAndLastWordsAndBeforeACRLF(t *testing.T) {
	m := newMergeCase(t)
	rest := "(junior)\n\nCloses ACM-56\n\n## Review\n\nno findings on a1b2c3d\n\n"
	for _, gate := range []string{"pnpm check", "pnpm gate", "pnpm team:gate"} {
		equal(t, m.problems(redWith(rest+gate)), nil)
		equal(t, m.problems(redWith(strings.ReplaceAll(rest+gate+"\r\n", "\n\n", "\r\n\r\n"))), nil)
		if got := m.problems(redWith(gate + "\n\n" + rest)); contains(got, notRecorded) {
			t.Fatal(gate, got)
		}
	}
}

func TestMergeUnderCIUnavailableHoldsADescriptionThatNamesOtherCommands(t *testing.T) {
	m := newMergeCase(t)
	for _, gate := range []string{
		"`check`", "`pnpm checks`", "`pnpm check:fast`", "`pnpm check:branches`", "`pnpm check:branch-x`",
		"`pnpm check-layers`", "`pnpm checkout`", "`xpnpm check`", "`pnpm format:check`",
		"`pnpm generate:check` and `pnpm i18n:check`", "`team:gate`", "`pnpm gates`", "`pnpm team:gates`",
		"`pnpm gate:fast`", "`pnpm team:gate:fast`", "`pnpm gate-fast`", "`xpnpm gate`", "`pnpm e2e`",
		"`pnpm test` and `pnpm build`",
	} {
		equal(t, m.problems(redWith(naming(gate))), []string{notRecorded})
	}
}

func TestMergeUnderCIUnavailableHoldsARecordMadeOnAnOlderHead(t *testing.T) {
	m := newMergeCase(t)
	want := []string{"ci-unavailable: the description does not name the current head a1b2c3d"}
	equal(t, m.problems(redWith(strings.Replace(naming("`pnpm team:gate`"), head[:7], older[:7], 1))), want)
	equal(t, m.problems(redWith(strings.Replace(record, head[:7], older[:7], 1))), want)
}

func TestMergeUnderCIUnavailableStillHoldsAThreadABranchBehindAndAMissingTagLine(t *testing.T) {
	if got := newMergeCase(t).problems(redWith(strings.Replace(record, "(senior)", "senior", 1)), 1, 1); len(got) != 3 {
		t.Fatal(got)
	}
}

func TestMergeUnderCIUnavailableStillHoldsAReviewOfTheHeadWithFindings(t *testing.T) {
	equal(t, newMergeCase(t).problems(func(pr *PullRequest) {
		redWith(record)(pr)
		pr.Comments = []Comment{review(head, "findings: 2")}
	}), []string{"the review of the head says `findings: 2`, not `no findings`"})
}

func TestMergeUnderCIUnavailableTakesTheRecordOverAReviewOfAnOlderHead(t *testing.T) {
	equal(t, newMergeCase(t).problems(func(pr *PullRequest) {
		redWith(record)(pr)
		pr.Comments = []Comment{review(older, "findings: 2")}
	}), nil)
}

func TestMergeUnderCIUnavailableAsksForNoRecordWhenTheChecksAreGreenAndTheHeadReviewed(t *testing.T) {
	equal(t, newMergeCase(t).problems(func(pr *PullRequest) { pr.Labels = []Named{{CIUnavailableLabel}} }), nil)
}

func TestCountTrailerCommitsCountsTheCommitsThatCarryEitherTrailerOnceEach(t *testing.T) {
	m := newMergeCase(t)
	pr := pullRequest()
	pr.Commits = []Commit{
		{""},
		{"why it changed\n\nCo-Authored-By: Claude <noreply@anthropic.com>"},
		{"Claude-Session: https://claude.ai/code/session_01"},
		{"co-authored-by: A\nClaude-Session: B"},
		{"the trailer Co-Authored-By: is named mid-line here"},
	}
	if n := CountTrailerCommits(&m.p.Workflow, pr); n != 3 {
		t.Fatal(n)
	}
	if n := CountTrailerCommits(&m.p.Workflow, pullRequest()); n != 0 {
		t.Fatal(n)
	}
}

func TestMergeSubjectIsTheTitleWithoutItsIssueIDThenTheNumber(t *testing.T) {
	m := newMergeCase(t)
	for title, want := range map[string]string{
		"ACM-16 tools: the team scripts": "tools: the team scripts (#41)",
		"ACM-7: ci: the jobs":            "ci: the jobs (#41)",
		"docs: no id":                    "docs: no id (#41)",
	} {
		pr := pullRequest()
		pr.Title = title
		if got := MergeSubject(m.p, pr); got != want {
			t.Fatal(got)
		}
	}
}

type fakeGH struct {
	calls   [][]string
	out     *bytes.Buffer
	deps    MergeDeps
	answers map[string]Result
}

func newFakeGH(t *testing.T, p *config.Project, f MergeFacts, answers map[string]Result) *fakeGH {
	g := &fakeGH{out: &bytes.Buffer{}, answers: answers}
	ok := func(v any) Result {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return Result{Stdout: string(data)}
	}
	threads := func(resolved ...bool) any {
		nodes := []map[string]bool{}
		for _, r := range resolved {
			nodes = append(nodes, map[string]bool{"isResolved": r})
		}
		return map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewThreads": map[string]any{"nodes": nodes}}}}}
	}
	run := func(o Options, name string, args ...string) (Result, error) {
		g.calls = append(g.calls, append([]string{name}, args...))
		key := strings.Join(args[:min(2, len(args))], " ")
		if a, found := g.answers[key]; found {
			return a, nil
		}
		switch key {
		case "auth status", "pr merge":
			return Result{}, nil
		case "pr view":
			return ok(f.PullRequest), nil
		case "api graphql":
			unresolved := make([]bool, f.UnresolvedThreads)
			return ok([]any{threads(true, true), threads(unresolved...)}), nil
		case "api repos/{owner}/{repo}/issues/41/comments":
			page := []map[string]any{}
			for _, c := range f.PullRequest.Comments {
				page = append(page, map[string]any{"user": map[string]string{"login": c.Author}, "body": c.Body, "created_at": c.CreatedAt})
			}
			return ok([]any{[]any{}, page}), nil
		}
		return ok(map[string]int{"behind_by": f.BehindBy, "ahead_by": 2}), nil
	}
	g.deps = MergeDeps{Root: t.TempDir(), Project: p, Run: run, Out: g.out}
	return g
}

func (g *fakeGH) merge(args ...string) int {
	code, err := Merge(g.deps, args)
	if err != nil {
		panic(err)
	}
	return code
}

func (g *fakeGH) printed() []string {
	return strings.Split(strings.TrimSuffix(g.out.String(), "\n"), "\n")
}

func (g *fakeGH) merges() [][]string {
	var out [][]string
	for _, c := range g.calls {
		if len(c) > 2 && c[1] == "pr" && c[2] == "merge" {
			out = append(out, c)
		}
	}
	return out
}

func facts(change func(*PullRequest), rest ...int) MergeFacts {
	pr := pullRequest()
	if change != nil {
		change(&pr)
	}
	f := MergeFacts{PullRequest: pr}
	if len(rest) == 2 {
		f.UnresolvedThreads, f.BehindBy = rest[0], rest[1]
	}
	return f
}

func TestRunMergeSquashMergesAPullRequestWithNothingHeldOnTheHeadItChecked(t *testing.T) {
	m := newMergeCase(t)
	g := newFakeGH(t, m.p, facts(func(pr *PullRequest) {
		pr.Files = []Path{{"docs/tech/tooling.md"}, {"tools/team/merge.ts"}}
	}), nil)
	if code := g.merge("41"); code != ExitOK {
		t.Fatal(code, g.printed())
	}
	want := [][]string{{"gh", "pr", "merge", "41", "--squash", "--delete-branch", "--match-head-commit", head, "--subject", "tools: the team scripts (#41)", "--body", ""}}
	if !reflect.DeepEqual(g.merges(), want) {
		t.Fatal(g.merges())
	}
	equal(t, g.printed(), []string{
		"#41 ACM-16 tools: the team scripts, head a1b2c3d",
		"merged: tools: the team scripts (#41)",
		"shared files touched:",
		"  the tooling table: docs/tech/tooling.md",
	})
}

func TestRunMergePrintsALineForTheCommitsWithATrailerAndMergesAllTheSame(t *testing.T) {
	g := newFakeGH(t, newMergeCase(t).p, facts(func(pr *PullRequest) { pr.Commits = []Commit{{"Co-Authored-By: X"}, {""}} }), nil)
	if code := g.merge("41"); code != ExitOK {
		t.Fatal(code)
	}
	if g.printed()[1] != "1 of the branch's commits carry an attribution trailer (Co-Authored-By, Claude-Session): tell the author" {
		t.Fatal(g.printed())
	}
	if c := g.merges()[0]; !reflect.DeepEqual(c[len(c)-2:], []string{"--body", ""}) {
		t.Fatal(c)
	}
}

func TestRunMergePrintsNoTrailerLineForABranchWithNoneAndSaysWhenNoSharedFileWasTouched(t *testing.T) {
	g := newFakeGH(t, newMergeCase(t).p, facts(nil), nil)
	g.merge("41")
	if strings.Contains(g.out.String(), "trailer") {
		t.Fatal(g.printed())
	}
	if p := g.printed(); p[len(p)-1] != "shared files touched: none" {
		t.Fatal(p)
	}
}

func TestRunMergeNeverApprovesDismissesBypassesOrRerunsAnything(t *testing.T) {
	g := newFakeGH(t, newMergeCase(t).p, facts(nil), nil)
	g.merge("41")
	var words []string
	var verbs []string
	for _, c := range g.calls {
		words = append(words, c...)
		verbs = append(verbs, strings.Join(c[1:3], " "))
	}
	all := strings.ReplaceAll(strings.Join(words, " "), "reviewThreads", "")
	for _, w := range []string{"--admin", "--auto", "review", "approve", "dismiss", "rerun", "--force"} {
		if strings.Contains(all, w) {
			t.Fatal(w)
		}
	}
	equal(t, verbs, []string{"auth status", "pr view", "api repos/{owner}/{repo}/issues/41/comments", "api graphql", "api repos/{owner}/{repo}/compare/master..." + head, "pr merge"})
}

func TestRunMergePrintsEveryHeldPointAndDoesNotMerge(t *testing.T) {
	g := newFakeGH(t, newMergeCase(t).p, facts(func(pr *PullRequest) { pr.Comments = []Comment{review(older, "no findings")} }, 1, 2), nil)
	if code := g.merge("41"); code != ExitFailed {
		t.Fatal(code)
	}
	equal(t, g.printed()[1:], []string{
		"held, not merged:",
		"1. the newest review is of fffffff, an older head",
		"2. 1 unresolved review thread(s)",
		"3. the branch is 2 commit(s) behind master",
	})
	if len(g.merges()) != 0 {
		t.Fatal(g.merges())
	}
}

func TestRunMergeSaysWhenGHIsNotLoggedInAndReadsNothing(t *testing.T) {
	g := newFakeGH(t, newMergeCase(t).p, facts(nil), map[string]Result{"auth status": {Code: 1, Stderr: "You are not logged into any GitHub hosts."}})
	if code := g.merge("41"); code != ExitFailed {
		t.Fatal(code)
	}
	equal(t, g.printed(), []string{"gh is not logged in to GitHub: run `gh auth login`, then try again"})
	if len(g.calls) != 1 {
		t.Fatal(g.calls)
	}
}

func TestRunMergeSaysWhenGHIsNotInstalled(t *testing.T) {
	g := newFakeGH(t, newMergeCase(t).p, facts(nil), map[string]Result{"auth status": {NotFound: true}})
	if code := g.merge("41"); code != ExitFailed {
		t.Fatal(code)
	}
	equal(t, g.printed(), []string{"gh, the GitHub CLI, is not installed"})
}

func TestRunMergeStopsWhenAReadFailsOrAnswersInAnotherShapeWithoutMerging(t *testing.T) {
	p := newMergeCase(t).p
	failed := newFakeGH(t, p, facts(nil), map[string]Result{"pr view": {Code: 1, Stderr: "no pull requests found"}})
	if code := failed.merge("41"); code != ExitFailed {
		t.Fatal(code)
	}
	equal(t, failed.printed(), []string{"reading the pull request failed:", "no pull requests found"})
	odd := newFakeGH(t, p, facts(nil), map[string]Result{"api graphql": {Stdout: "{}"}})
	slow := newFakeGH(t, p, facts(nil), map[string]Result{"pr view": {TimedOut: true}})
	partial := newFakeGH(t, p, facts(nil), map[string]Result{"pr view": {Stdout: `{"number": 41}`}})
	for _, g := range []*fakeGH{odd, slow, partial} {
		if code := g.merge("41"); code != ExitFailed || len(g.merges()) != 0 {
			t.Fatal(code, g.printed())
		}
	}
}

func TestRunMergeExitsNonZeroWhenGitHubRefusesTheMerge(t *testing.T) {
	g := newFakeGH(t, newMergeCase(t).p, facts(nil), map[string]Result{"pr merge": {Code: 1, Stderr: "Head branch was modified. Review and try the merge again."}})
	if code := g.merge("41"); code != ExitFailed {
		t.Fatal(code)
	}
	if !strings.Contains(g.out.String(), "the merge failed:\nHead branch was modified") || strings.Contains(g.out.String(), "merged:") {
		t.Fatal(g.printed())
	}
}

func TestRunMergeRefusesWrongArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"41", "42"}, {"#41"}, {"0"}, {"--admin"}} {
		g := newFakeGH(t, newMergeCase(t).p, facts(nil), nil)
		if code := g.merge(args...); code != ExitUsage || len(g.calls) != 0 {
			t.Fatal(args, code, g.calls)
		}
	}
}

func TestMergeWithAGitHubTrackerAsksForClosesHashN(t *testing.T) {
	p := loadProject(t, "demo", "[tracker]\nkind = \"github\"\n[workflow]\ntags = [\"(lead)\"]\n[[roles]]\nname = \"lead\"\n")
	pr := pullRequest()
	pr.BaseRefName, pr.Body = "master", "(lead)\n\nCloses #12\n"
	equal(t, ListMergeProblems(p, MergeFacts{PullRequest: pr}), nil)
	pr.Body = "(lead)\n\nCloses ACM-12\n"
	equal(t, ListMergeProblems(p, MergeFacts{PullRequest: pr}), []string{"the description's third line is not `Closes #n`"})
	if got := MergeSubject(p, pr); got != "ACM-16 tools: the team scripts (#41)" {
		t.Fatal(got)
	}
}

func TestMergeWithoutTagsAsksForTheClosesLineFirst(t *testing.T) {
	p := loadProject(t, "demo", "[tracker]\nkind = \"github\"\n[[roles]]\nname = \"lead\"\n")
	pr := pullRequest()
	pr.BaseRefName, pr.Body = "master", "Closes #3\n\nwhat changed\n"
	equal(t, ListMergeProblems(p, MergeFacts{PullRequest: pr}), nil)
	pr.Body = "what changed\n"
	equal(t, ListMergeProblems(p, MergeFacts{PullRequest: pr}), []string{"the description's first line is not `Closes #n`"})
}

func TestMergeWithoutATrackerAsksForNoClosesLine(t *testing.T) {
	p := loadProject(t, "demo", "[[roles]]\nname = \"lead\"\n")
	pr := pullRequest()
	pr.BaseRefName, pr.Body = "master", "(lead)\n\nwhat changed\n"
	equal(t, ListMergeProblems(p, MergeFacts{PullRequest: pr}), nil)
	pr.Body = "anything\n\nwhat changed\n"
	equal(t, ListMergeProblems(p, MergeFacts{PullRequest: pr}), nil)
}
