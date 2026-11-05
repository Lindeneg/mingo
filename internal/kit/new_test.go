package kit

import (
	"strings"
	"testing"
)

func noTerminal(string, string) (string, bool) { return "", false }

func TestNewWritesAConfigThatLoadsFromThePresetAndTheFlags(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	flags := map[string][]string{
		"--repo":            {"x/demo"},
		"--roles":           {"lead, dev"},
		"--ci-stack":        {"go"},
		"--workflow-checks": {"vet, test, security"},
		"--ci-run":          {"vet=go vet ./...", "test=go test ./..."},
		"--tracker-kind":    {"linear"},
		"--tracker-team":    {"DEM"},
		"--ports-variable":  {"APP_PORT_OFFSET"},
		"--ports-offsets":   {"dev=25"},
	}
	text, err := k.New("demo", "full", flags, noTerminal)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`repo = "x/demo"`, `lead = "lead"`, `checks = ["vet", "test", "security"]`, "[ci.run]\nvet = \"go vet ./...\"\ntest = \"go test ./...\"",
		`kind = "linear"`, `team = "DEM"`, `project = "demo"`, "[ports.offsets]\nlead = 10\ndev = 25", "[[roles]]\nname = \"dev\"",
		`scripts = ["check", "test", "build", "dev"]`, `base = "master"`,
	} {
		if !strings.Contains(text, line) {
			t.Fatal(line, "\n", text)
		}
	}
	p := project(t, text)
	if mods, err := k.For(p); err != nil || len(mods) != len(k.Modules) {
		t.Fatal(mods, err)
	}
}

func TestNewNamesTheReviewBotAsTheReviewerOnlyWithTheReviewModule(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	solo, err := k.New("demo", "solo", map[string][]string{"--repo": {"x/demo"}, "--ci-stack": {"go"}, "--workflow-checks": {"test"}, "--ci-run": {"test=go test ./..."}}, noTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(solo, "\nreviewer = \"claude[bot]\"\n") || project(t, solo).Workflow.Reviewer != "claude[bot]" {
		t.Fatal(solo)
	}
	bare, err := k.New("demo", "bare", map[string][]string{"--repo": {"x/demo"}}, noTerminal)
	if err != nil || strings.Contains(bare, "reviewer") {
		t.Fatal(bare, err)
	}
}

func TestNewAsksWhatTheFlagsLeaveOutAndSaysWhatItNeedsWithoutATerminal(t *testing.T) {
	k, _ := LoadEmbedded()
	var asked []string
	prompt := func(q, def string) (string, bool) {
		asked = append(asked, q+" ["+def+"]")
		if q == "the GitHub repository, as owner/name" {
			return "x/demo", true
		}
		return "", true
	}
	text, err := k.New("demo", "solo", nil, prompt)
	if err != nil || !strings.Contains(text, `repo = "x/demo"`) || !strings.Contains(text, `stack = "pnpm"`) {
		t.Fatal(text, err)
	}
	if asked[0] != "the GitHub repository, as owner/name []" || asked[2] != "the roles, in the order they start [lead]" {
		t.Fatal(asked)
	}
	for want, c := range map[string]struct {
		preset string
		flags  map[string][]string
	}{
		"mingo new needs --repo":             {"bare", nil},
		"the solo preset has one role":       {"solo", map[string][]string{"--repo": {"x/y"}, "--roles": {"a,b"}}},
		"mingo new knows no --colour":        {"bare", map[string][]string{"--colour": {"red"}}},
		"no preset huge":                     {"huge", nil},
		"the lead boss is none of the roles": {"bare", map[string][]string{"--repo": {"x/y"}, "--lead": {"boss"}}},
		"mingo new needs --ci-run test=...":  {"solo", map[string][]string{"--repo": {"x/y"}, "--ci-stack": {"go"}, "--workflow-checks": {"test"}}},
	} {
		if _, err := k.New("demo", c.preset, c.flags, noTerminal); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, err)
		}
	}
}

func TestNewWritesStringsTOMLReadsBack(t *testing.T) {
	k, _ := LoadEmbedded()
	odd := "a \"quoted\" \\ title\twith\x01control"
	text, err := k.New("demo", "bare", map[string][]string{"--repo": {"x/y"}, "--title": {odd}}, noTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if p := project(t, text); p.Title != odd {
		t.Fatalf("%q\n%s", p.Title, text)
	}
}

func TestNewKeepsClaudesFilesLocalWhenAskedAndTheReviewThenReadsTheReadme(t *testing.T) {
	k, _ := LoadEmbedded()
	var asked []string
	prompt := func(q, def string) (string, bool) {
		asked = append(asked, q+" ["+def+"]")
		switch q {
		case "the GitHub repository, as owner/name":
			return "x/demo", true
		case "where Claude's files go: repo, or local to each clone and out of git":
			return "local", true
		}
		return "", true
	}
	text, err := k.New("demo", "solo", nil, prompt)
	if err != nil || !strings.Contains(text, "\nclaude_files = \"local\"\n") || !strings.Contains(text, "\nread = \"README.md first\"\n") {
		t.Fatal(text, err)
	}
	if !strings.Contains(strings.Join(asked, "\n"), "what the AI review of each pull request reads before the diff [README.md first]") || !project(t, text).ClaudeLocal() {
		t.Fatal(asked)
	}
	kept, err := k.New("demo", "solo", map[string][]string{"--repo": {"x/demo"}, "--claude-files": {"repo"}}, noTerminal)
	if err != nil || strings.Contains(kept, "claude_files") || !strings.Contains(kept, "\nread = \"CLAUDE.md first\"\n") {
		t.Fatal(kept, err)
	}
}

func TestNewAsksTheStackOtherForItsSetupEachChecksCommandAndItsAudit(t *testing.T) {
	k, _ := LoadEmbedded()
	var asked []string
	prompt := func(q, def string) (string, bool) {
		asked = append(asked, q+" ["+def+"]")
		switch q {
		case "the GitHub repository, as owner/name":
			return "x/demo", true
		case "how CI installs the project: pnpm, npm, go or other":
			return "other", true
		case "the commands CI runs to set up the project before each check, empty for none":
			return "make deps, make tools", true
		case "the CI jobs every pull request must pass, such as test or build":
			return "test, security", true
		case "the command of the check test":
			return "make test", true
		case "the dependency audit the security job runs, empty for none":
			return "make audit", true
		}
		return "", true
	}
	text, err := k.New("demo", "solo", nil, prompt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "[ci]\nstack = \"other\"\nsetup = [\"make deps\", \"make tools\"]\naudit = \"make audit\"\n\n[ci.run]\ntest = \"make test\"\n") {
		t.Fatal(text)
	}
	if p := project(t, text); p.CI.Audit != "make audit" || len(p.CI.Setup) != 2 {
		t.Fatal(p.CI)
	}
	bare, err := k.New("demo", "solo", map[string][]string{"--repo": {"x/demo"}, "--ci-stack": {"other"}, "--workflow-checks": {"test"}, "--ci-run": {"test=make test"}}, noTerminal)
	if err != nil || strings.Contains(bare, "setup") || strings.Contains(bare, "audit") {
		t.Fatal(bare, err)
	}
	asked = nil
	if _, err := k.New("demo", "solo", map[string][]string{"--repo": {"x/demo"}}, func(q, def string) (string, bool) { asked = append(asked, q); return "", true }); err != nil {
		t.Fatal(err)
	}
	if all := strings.Join(asked, "\n"); strings.Contains(all, "set up the project") || strings.Contains(all, "dependency audit") || strings.Contains(all, "the command of the check") {
		t.Fatal(all)
	}
}
