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
