// Package team holds the lead's pull request commands: the merge check, the rebase that keeps both
// sides of shared lists, and the gate.
package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Lindeneg/mingo/internal/config"
)

const CIUnavailableLabel = "ci-unavailable"

const GHTimeout = 60 * time.Second

const shortSHA = 7

type Runner func(o Options, name string, args ...string) (Result, error)

type Check struct {
	Name         string `json:"name"`
	WorkflowName string `json:"workflowName"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
}

type Comment struct {
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

type PullRequest struct {
	Number            int       `json:"number"`
	Title             string    `json:"title"`
	State             string    `json:"state"`
	IsDraft           bool      `json:"isDraft"`
	BaseRefName       string    `json:"baseRefName"`
	HeadRefOid        string    `json:"headRefOid"`
	Body              string    `json:"body"`
	Labels            []Named   `json:"labels"`
	Comments          []Comment `json:"comments"`
	StatusCheckRollup []Check   `json:"statusCheckRollup"`
	Files             []Path    `json:"files"`
	Commits           []Commit  `json:"commits"`
}

type Named struct {
	Name string `json:"name"`
}

type Path struct {
	Path string `json:"path"`
}

type Commit struct {
	MessageBody string `json:"messageBody"`
}

var pullRequestFields = []string{"number", "title", "state", "isDraft", "baseRefName", "headRefOid", "body", "labels", "comments", "statusCheckRollup", "files", "commits"}

type MergeFacts struct {
	PullRequest       PullRequest
	UnresolvedThreads int
	BehindBy          int
}

var lineEnd = regexp.MustCompile(`\r?\n`)

func lines(s string) []string {
	return lineEnd.Split(s, -1)
}

func checkProblems(w *config.Workflow, pr PullRequest) []string {
	var out []string
	for _, name := range w.Checks {
		var runs []Check
		for _, c := range pr.StatusCheckRollup {
			if c.Name == name && c.WorkflowName == w.CI {
				runs = append(runs, c)
			}
		}
		if len(runs) == 0 {
			out = append(out, "the check "+name+" did not run on the head")
			continue
		}
		for _, run := range runs {
			if run.Status == "COMPLETED" && run.Conclusion == "SUCCESS" {
				continue
			}
			state := strings.ToLower(run.Status)
			if run.Status == "COMPLETED" {
				state = strings.ToLower(run.Conclusion)
				if run.Conclusion == "" {
					state = "no conclusion"
				}
			} else if run.Status == "" {
				state = "unknown"
			}
			out = append(out, "the check "+name+" is not green: "+state)
			break
		}
	}
	return out
}

type reviewProblem struct {
	message   string
	isFinding bool
}

func reviewLine(w *config.Workflow) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(w.ReviewLine) + ` ([0-9a-f]{40})\s*$`)
}

func findReviewProblem(w *config.Workflow, pr PullRequest) *reviewProblem {
	type review struct{ sha, verdict, at string }
	var reviews []review
	re := reviewLine(w)
	for _, c := range pr.Comments {
		ls := lines(c.Body)
		verdict := ""
		if len(ls) > 1 {
			verdict = strings.TrimSpace(ls[1])
		}
		if m := re.FindStringSubmatch(ls[0]); m != nil {
			reviews = append(reviews, review{m[1], verdict, c.CreatedAt})
		}
	}
	sort.SliceStable(reviews, func(i, j int) bool { return reviews[i].at < reviews[j].at })
	if len(reviews) == 0 {
		return &reviewProblem{message: "not reviewed: no comment begins with `" + w.ReviewLine + " <head sha>`"}
	}
	newest := reviews[len(reviews)-1]
	if newest.sha != pr.HeadRefOid {
		return &reviewProblem{message: "the newest review is of " + newest.sha[:shortSHA] + ", an older head"}
	}
	if newest.verdict == w.NoFindings {
		return nil
	}
	return &reviewProblem{message: "the review of the head says `" + newest.verdict + "`, not `" + w.NoFindings + "`", isFinding: true}
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// a command named as whole words, so pnpm checks is no pnpm check
func namesCommand(body, command string) bool {
	for from := 0; ; {
		i := strings.Index(body[from:], command)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(command)
		from = start + 1
		if start > 0 && (isWordByte(body[start-1]) || body[start-1] == '-') {
			continue
		}
		if end < len(body) {
			next := body[end]
			if isWordByte(next) || next == '-' {
				continue
			}
			if next == ':' && end+1 < len(body) && isWordByte(body[end+1]) {
				continue
			}
		}
		return true
	}
}

func orList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = "`" + s + "`"
	}
	if len(quoted) < 2 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}

func localRecordProblems(w *config.Workflow, pr PullRequest) []string {
	var out []string
	named := false
	for _, c := range w.LocalRuns {
		named = named || namesCommand(pr.Body, c)
	}
	if !named {
		out = append(out, "does not record "+orList(w.LocalRuns))
	}
	heading := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(w.ReviewHeading) + `\s*$`)
	if !heading.MatchString(pr.Body) {
		out = append(out, "has no `"+w.ReviewHeading+"` section for the local review")
	}
	head := pr.HeadRefOid[:min(shortSHA, len(pr.HeadRefOid))]
	if !strings.Contains(pr.Body, head) {
		out = append(out, "does not name the current head "+head)
	}
	for i, p := range out {
		out[i] = CIUnavailableLabel + ": the description " + p
	}
	return out
}

func ListMergeProblems(p *config.Project, f MergeFacts) []string {
	w := &p.Workflow
	pr := f.PullRequest
	var out []string
	if pr.State != "OPEN" {
		out = append(out, "the pull request is "+strings.ToLower(pr.State)+", not open")
	}
	if pr.IsDraft {
		out = append(out, "the pull request is a draft")
	}
	if pr.BaseRefName != w.Base {
		out = append(out, "the base is "+pr.BaseRefName+", not "+w.Base)
	}
	checks := checkProblems(w, pr)
	review := findReviewProblem(w, pr)
	local := false
	for _, l := range pr.Labels {
		local = local || l.Name == CIUnavailableLabel
	}
	switch {
	case !local:
		out = append(out, checks...)
		if review != nil {
			out = append(out, review.message)
		}
	case review != nil && review.isFinding:
		out = append(out, review.message)
	case len(checks) > 0 || review != nil:
		out = append(out, localRecordProblems(w, pr)...)
	}
	if f.UnresolvedThreads > 0 {
		out = append(out, fmt.Sprintf("%d unresolved review thread(s)", f.UnresolvedThreads))
	}
	if f.BehindBy > 0 {
		out = append(out, fmt.Sprintf("the branch is %d commit(s) behind %s", f.BehindBy, w.Base))
	}
	ls := lines(pr.Body)
	closesAt := 0
	if tags := w.Tags; len(tags) > 0 {
		if !contains(tags, ls[0]) {
			out = append(out, "the description's first line is not a role tag ("+strings.Join(tags, ", ")+")")
		}
		if len(ls) < 2 || ls[1] != "" {
			out = append(out, "the description's second line is not blank")
		}
		closesAt = 2
	}
	if closes, _ := p.IssuePatterns(); closes != nil && (len(ls) <= closesAt || !closes.MatchString(ls[closesAt])) {
		out = append(out, "the description's "+[]string{"first", "second", "third"}[closesAt]+" line is not `"+p.ClosesExample()+"`")
	}
	for _, l := range ls {
		if w.IsFooter(l) {
			out = append(out, "the description carries an attribution footer: remove it")
			break
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func trailerPattern(w *config.Workflow) *regexp.Regexp {
	quoted := make([]string, len(w.Trailers))
	for i, t := range w.Trailers {
		quoted[i] = regexp.QuoteMeta(t)
	}
	return regexp.MustCompile(`(?im)^(?:` + strings.Join(quoted, "|") + `):`)
}

func CountTrailerCommits(w *config.Workflow, pr PullRequest) int {
	if len(w.Trailers) == 0 {
		return 0
	}
	re := trailerPattern(w)
	n := 0
	for _, c := range pr.Commits {
		if re.MatchString(c.MessageBody) {
			n++
		}
	}
	return n
}

func MergeSubject(p *config.Project, pr PullRequest) string {
	title := pr.Title
	if _, prefix := p.IssuePatterns(); prefix != nil {
		title = prefix.ReplaceAllString(title, "")
	}
	return title + " (#" + strconv.Itoa(pr.Number) + ")"
}

type MergeDeps struct {
	Root    string
	Project *config.Project
	Run     Runner
	Out     io.Writer
}

func gh(d MergeDeps, what string, args ...string) (string, error) {
	r, err := d.Run(Options{Dir: d.Root, Timeout: GHTimeout}, "gh", args...)
	if err != nil {
		return "", err
	}
	switch {
	case r.NotFound:
		return "", stop("gh, the GitHub CLI, is not installed")
	case r.TimedOut:
		return "", stop(fmt.Sprintf("%s did not finish in %d seconds", what, int(GHTimeout.Seconds())))
	case r.Code != 0:
		text := r.Stderr
		if text == "" {
			text = r.Stdout
		}
		return "", stop(what + " failed:\n" + LastLines(text, 5))
	}
	return r.Stdout, nil
}

// the keys must all be there, a missing one is a shape this command does not know
func parse(text, what string, into any, keys ...string) error {
	var raw json.RawMessage
	if json.Unmarshal([]byte(text), &raw) != nil {
		return stop(what + ": gh did not answer with JSON")
	}
	shape := stop(what + ": gh answered in a shape this command does not know")
	if len(keys) > 0 {
		var present map[string]json.RawMessage
		if json.Unmarshal(raw, &present) != nil {
			return shape
		}
		for _, k := range keys {
			if _, ok := present[k]; !ok {
				return shape
			}
		}
	}
	if json.Unmarshal(raw, into) != nil {
		return shape
	}
	return nil
}

const threadsQuery = `query($owner: String!, $repo: String!, $number: Int!, $endCursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviewThreads(first: 100, after: $endCursor) {
        nodes { isResolved }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`

type threadPage struct {
	Data *struct {
		Repository *struct {
			PullRequest *struct {
				ReviewThreads *struct {
					Nodes []struct {
						IsResolved bool `json:"isResolved"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

func readFacts(d MergeDeps, number string) (MergeFacts, error) {
	var f MergeFacts
	what := "reading the pull request"
	out, err := gh(d, what, "pr", "view", number, "--json", strings.Join(pullRequestFields, ","))
	if err != nil {
		return f, err
	}
	if err := parse(out, what, &f.PullRequest, pullRequestFields...); err != nil {
		return f, err
	}
	what = "reading the review threads"
	out, err = gh(d, what, "api", "graphql", "--paginate", "--slurp",
		"-F", "owner={owner}", "-F", "repo={repo}", "-F", "number="+strconv.Itoa(f.PullRequest.Number),
		"-f", "query="+threadsQuery)
	if err != nil {
		return f, err
	}
	var pages []threadPage
	if err := parse(out, what, &pages); err != nil {
		return f, err
	}
	for _, page := range pages {
		if page.Data == nil || page.Data.Repository == nil || page.Data.Repository.PullRequest == nil || page.Data.Repository.PullRequest.ReviewThreads == nil {
			return f, stop(what + ": gh answered in a shape this command does not know")
		}
		for _, n := range page.Data.Repository.PullRequest.ReviewThreads.Nodes {
			if !n.IsResolved {
				f.UnresolvedThreads++
			}
		}
	}
	base := d.Project.Workflow.Base
	what = "comparing the head with " + base
	out, err = gh(d, what, "api", "repos/{owner}/{repo}/compare/"+base+"..."+f.PullRequest.HeadRefOid)
	if err != nil {
		return f, err
	}
	var comparison struct {
		BehindBy int `json:"behind_by"`
	}
	if err := parse(out, what, &comparison, "behind_by"); err != nil {
		return f, err
	}
	f.BehindBy = comparison.BehindBy
	return f, nil
}

var prNumberRe = regexp.MustCompile(`^[1-9][0-9]*$`)

func Merge(d MergeDeps, args []string) (int, error) {
	if len(args) != 1 || !prNumberRe.MatchString(args[0]) {
		fmt.Fprintln(d.Out, "usage: mingo pr merge <pull request number>")
		return ExitUsage, nil
	}
	code, err := merge(d, args[0])
	var s *Stop
	if errors.As(err, &s) {
		fmt.Fprintln(d.Out, s.Message)
		return ExitFailed, nil
	}
	return code, err
}

func merge(d MergeDeps, number string) (int, error) {
	p, w := d.Project, &d.Project.Workflow
	login, err := d.Run(Options{Dir: d.Root, Timeout: GHTimeout}, "gh", "auth", "status")
	if err != nil {
		return ExitFailed, err
	}
	if login.NotFound {
		return ExitFailed, stop("gh, the GitHub CLI, is not installed")
	}
	if !login.OK() {
		return ExitFailed, stop("gh is not logged in to GitHub: run `gh auth login`, then try again")
	}
	f, err := readFacts(d, number)
	if err != nil {
		return ExitFailed, err
	}
	pr := f.PullRequest
	head := pr.HeadRefOid
	fmt.Fprintf(d.Out, "#%d %s, head %s\n", pr.Number, pr.Title, head[:min(shortSHA, len(head))])
	if n := CountTrailerCommits(w, pr); n > 0 {
		fmt.Fprintf(d.Out, "%d of the branch's commits carry an attribution trailer (%s): tell the author\n", n, strings.Join(w.Trailers, ", "))
	}
	if problems := ListMergeProblems(p, f); len(problems) > 0 {
		fmt.Fprintln(d.Out, "held, not merged:")
		for i, problem := range problems {
			fmt.Fprintf(d.Out, "%d. %s\n", i+1, problem)
		}
		return ExitFailed, nil
	}
	subject := MergeSubject(p, pr)
	if _, err := gh(d, "the merge", "pr", "merge", strconv.Itoa(pr.Number), "--squash", "--delete-branch",
		"--match-head-commit", head, "--subject", subject, "--body", ""); err != nil {
		return ExitFailed, err
	}
	fmt.Fprintln(d.Out, "merged: "+subject)
	var shared []string
	for _, file := range pr.Files {
		if s, ok := w.SharedFile(file.Path); ok {
			shared = append(shared, s.Name+": "+file.Path)
		}
	}
	if len(shared) == 0 {
		fmt.Fprintln(d.Out, "shared files touched: none")
	} else {
		fmt.Fprintln(d.Out, "shared files touched:")
	}
	for _, line := range shared {
		fmt.Fprintln(d.Out, "  "+line)
	}
	return ExitOK, nil
}
