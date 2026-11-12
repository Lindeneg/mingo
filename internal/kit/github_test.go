package kit

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Lindeneg/mingo/internal/team"
)

type fakeGitHub struct {
	labels  string
	secrets string
	calls   []string
	stdin   []string
	fail    string
}

func (f *fakeGitHub) run(o team.Options, name string, args ...string) (team.Result, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	f.stdin = append(f.stdin, o.Stdin)
	if f.fail != "" {
		return team.Result{Code: 1, Stderr: f.fail}, nil
	}
	switch strings.Join(args[:2], " ") {
	case "label list":
		return team.Result{Stdout: f.labels}, nil
	case "secret list":
		return team.Result{Stdout: f.secrets}, nil
	}
	return team.Result{}, nil
}

func githubDeps(t *testing.T, f *fakeGitHub) (Deps, *bytes.Buffer) {
	out := &bytes.Buffer{}
	p := project(t, "repo = \"x/demo\"\nmodules = [\"workflow\"]\n[[roles]]\nname = \"lead\"\n")
	return Deps{Project: p, Run: f.run, Out: out, TempRoot: t.TempDir()}, out
}

func TestTheLabelsAreMadeOnlyWhenMissing(t *testing.T) {
	f := &fakeGitHub{labels: "[]"}
	d, out := githubDeps(t, f)
	if err := makeLabels(d); err != nil {
		t.Fatal(err)
	}
	want := "label create ci-unavailable -R x/demo --description Merged on the local checks and a local review while Actions could not run --color F2994A"
	if len(f.calls) != 2 || f.calls[1] != want || out.String() != "label: ci-unavailable on x/demo\n" {
		t.Fatal(f.calls, out.String())
	}
	f = &fakeGitHub{labels: `[{"name":"bug"},{"name":"ci-unavailable"}]`}
	d, _ = githubDeps(t, f)
	if err := makeLabels(d); err != nil || len(f.calls) != 1 {
		t.Fatal(f.calls, err)
	}
}

func TestDoctorChecksTheLabelAndTheReviewsToken(t *testing.T) {
	for _, c := range []struct {
		f    *fakeGitHub
		want []bool
	}{
		{&fakeGitHub{labels: "[]", secrets: "[]"}, []bool{false, false}},
		{&fakeGitHub{labels: `[{"name":"ci-unavailable"}]`, secrets: `[{"name":"CLAUDE_CODE_OAUTH_TOKEN"}]`}, []bool{true, true}},
	} {
		d, _ := githubDeps(t, c.f)
		for i, name := range []string{"labels", "review-token"} {
			r := &report{}
			checks[name](d, r)
			if len(r.findings) != 1 || r.findings[0].ok != c.want[i] {
				t.Fatal(name, r.findings)
			}
		}
	}
}

func TestTheReviewsTokenIsSetFromWhatWasPastedAndNeverAnEmptyOne(t *testing.T) {
	f := &fakeGitHub{}
	d, out := githubDeps(t, f)
	if err := SetReviewSecret(d, "  sk-ant-oat01-abc\n"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0] != "secret set CLAUDE_CODE_OAUTH_TOKEN -R x/demo" || f.stdin[0] != "sk-ant-oat01-abc" || out.String() != "the review's token is set on x/demo\n" {
		t.Fatal(f.calls, f.stdin, out.String())
	}
	if err := SetReviewSecret(d, "\n"); err == nil || len(f.calls) != 1 {
		t.Fatal("an empty token was set")
	}
}

func TestLabelsMatchWithoutCaseAsOnGitHubAndTheirDescriptionsAreCutByCharacters(t *testing.T) {
	f := &fakeGitHub{labels: `[{"name":"CI-Unavailable"}]`}
	d, _ := githubDeps(t, f)
	if err := makeLabels(d); err != nil || len(f.calls) != 1 {
		t.Fatal(f.calls, err)
	}
	r := &report{}
	checks["labels"](d, r)
	if len(r.findings) != 1 || !r.findings[0].ok {
		t.Fatal(r.findings)
	}
	p := project(t, "repo = \"x/demo\"\nmodules = [\"tracker\"]\n[tracker]\nkind = \"github\"\n[[roles]]\nname = \"dev\"\ndoes = \""+strings.Repeat("é", 120)+"\"\n")
	labels := wantedLabels(Deps{Project: p})
	if len(labels) != 1 || !utf8.ValidString(labels[0].description) || labels[0].description != strings.Repeat("é", 97)+"..." {
		t.Fatal(labels)
	}
}

func TestDoctorSaysItCouldNotCheckTheTokenWhenGhFails(t *testing.T) {
	f := &fakeGitHub{fail: "error connecting to api.github.com"}
	d, _ := githubDeps(t, f)
	for _, name := range []string{"labels", "review-token"} {
		r := &report{}
		checks[name](d, r)
		if r.missing != 0 || r.unknown != 1 || len(r.findings) != 1 || !strings.HasPrefix(r.findings[0].text, "could not check ") || !strings.HasSuffix(r.findings[0].text, "error connecting to api.github.com") {
			t.Fatal(name, r.findings)
		}
	}
}

func TestAGitHubTrackerGetsALabelPerRole(t *testing.T) {
	f := &fakeGitHub{labels: `[{"name":"ci-unavailable"}]`}
	out := &bytes.Buffer{}
	p := project(t, "repo = \"x/demo\"\nmodules = [\"workflow\", \"tracker\"]\n[tracker]\nkind = \"github\"\n[[roles]]\nname = \"tech-lead\"\n[[roles]]\nname = \"dev\"\ndoes = \"codes the issues labelled dev\"\n")
	if err := makeLabels(Deps{Project: p, Run: f.run, Out: out, TempRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"label create tech-lead -R x/demo --description the issues of the tech lead --color 1D76DB",
		"label create dev -R x/demo --description codes the issues labelled dev --color 1D76DB",
	}
	if len(f.calls) != 3 || f.calls[1] != want[0] || f.calls[2] != want[1] {
		t.Fatal(f.calls)
	}
}
