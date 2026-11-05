package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func refused(t *testing.T, cases map[string][]string) {
	t.Helper()
	for want, lines := range cases {
		addProject(t, "wrong", append(lines, `[[roles]]`, `name = "lead"`)...)
		if _, err := Load("wrong"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q, got %v", want, err)
		}
	}
}

func TestASharedFileNeedsANameAndAPattern(t *testing.T) {
	setupHome(t)
	refused(t, map[string][]string{
		"has a [[workflow.shared]] without a name":         {`[[workflow.shared]]`, `pattern = '^x$'`},
		`has the shared file "the list" without a pattern`: {`[[workflow.shared]]`, `name = "the list"`},
		`has the shared file "the map" without a pattern`:  {`[[workflow.shared]]`, `name = "the map"`, `pattern = ""`},
	})
}

func TestAKeyMingoDoesNotKnowIsRefusedWithTheKeyItIsClosestTo(t *testing.T) {
	setupHome(t)
	refused(t, map[string][]string{
		"has the key modles, which mingo does not know, did you mean modules":                     {`modles = ["ci"]`},
		"has the key autostrat, which mingo does not know, did you mean autostart":                {`autostrat = ["lead"]`},
		"has the key [[roles]] effrot, which mingo does not know, did you mean effort":            {`[[roles]]`, `name = "dev"`, `effrot = "high"`},
		"has the key [workflow] bsae, which mingo does not know, did you mean base":               {`[workflow]`, `bsae = "main"`},
		"has the key [workflow] check, which mingo does not know, did you mean checks":            {`[workflow]`, `check = ["test"]`},
		"has the key [tracker] knd, which mingo does not know, did you mean kind":                 {`[tracker]`, `knd = "github"`},
		"has the key [[workflow.shared]] patern, which mingo does not know, did you mean pattern": {`[[workflow.shared]]`, `name = "x"`, `pattern = 'x'`, `patern = 'y'`},
		"has the key [trackr], which mingo does not know, did you mean [tracker]":                 {`[trackr]`, `kind = "github"`},
		"has the key colour, which mingo does not know, README.md names every key":                {`colour = "red"`},
	})
	addProject(t, "wrong", `modles = ["ci"]`, `colour = "red"`, `[[roles]]`, `name = "lead"`)
	if _, err := Load("wrong"); err == nil || !strings.HasSuffix(err.Error(), "has keys mingo does not know, README.md names every key\n  modles, did you mean modules\n  colour") {
		t.Fatal(err)
	}
}

func TestEveryKeyOfTheReadmeLoads(t *testing.T) {
	home := setupHome(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, _ := strings.Cut(string(data), "## Every key\n\n```toml\n")
	config, _, _ := strings.Cut(rest, "```")
	if !strings.Contains(config, "[blocks.") {
		t.Fatal(config)
	}
	addProject(t, "shop", config)
	if err := os.MkdirAll(filepath.Join(home, "dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustLoad(t, "shop")
}

func TestACommandInCIRunIsForOneOfTheChecks(t *testing.T) {
	setupHome(t)
	refused(t, map[string][]string{
		`has [ci.run] "lint", which is none of the checks test security`:   {`[workflow]`, `checks = ["test", "security"]`, `[ci.run]`, `lint = "make lint"`},
		`has [ci.run] "test" and no [workflow] checks`:                     {`[ci.run]`, `test = "make test"`},
		"has [ci.run] security, but the security check runs its own steps": {`[workflow]`, `checks = ["security"]`, `[ci.run]`, `security = "make audit"`},
		`has [ci.run] test = "", give the check its command`:               {`[workflow]`, `checks = ["test"]`, `[ci.run]`, `test = ""`},
	})
}

func TestChecksAreUniqueGitHubJobIDsAndTheCIModuleNeedsOne(t *testing.T) {
	setupHome(t)
	refused(t, map[string][]string{
		`has the check "type check" in [workflow] checks, which GitHub cannot take as a job`: {`[workflow]`, `checks = ["type check"]`},
		`has the check "1test" in [workflow] checks`:                                         {`[workflow]`, `checks = ["1test"]`},
		`has the check "" in [workflow] checks`:                                              {`[workflow]`, `checks = [""]`},
		"has the check test twice in [workflow] checks":                                      {`[workflow]`, `checks = ["test", "test"]`},
		"has the ci module and no [workflow] checks":                                         {`modules = ["ci"]`, `[ci]`, `stack = "pnpm"`},
	})
	addProject(t, "right", `[workflow]`, `checks = ["type-check", "_lint", "Test2"]`, `[[roles]]`, `name = "lead"`)
	mustLoad(t, "right")
}

func TestTheRepoIsOwnerAndNameAsGitHubHasThem(t *testing.T) {
	setupHome(t)
	for _, repo := range []string{"https://github.com/x/shop", "x/shop.git", "shop", "x/shop/y", "x/", "/shop", "-x/shop", "x/..", "x y/shop"} {
		addProject(t, "wrong", `repo = "`+repo+`"`, `[[roles]]`, `name = "lead"`)
		if _, err := Load("wrong"); err == nil || !strings.Contains(err.Error(), `has the repo "`+repo+`", write it as owner/name, such as you/shop`) {
			t.Fatal(repo, err)
		}
	}
	for _, repo := range []string{"you/{project}", "Some-Org/my_repo.js", "a/b-c.d_e", ""} {
		addProject(t, "right", `repo = "`+repo+`"`, `[[roles]]`, `name = "lead"`)
		mustLoad(t, "right")
	}
}

func TestAKeySetToNothingIsRefused(t *testing.T) {
	setupHome(t)
	refused(t, map[string][]string{
		`sets title to "", give it a value or leave the key out`: {`title = ""`},
		`sets lead to ""`:                                                 {`lead = ""`},
		`sets opener to " "`:                                              {`opener = " "`},
		`sets [workflow] base to ""`:                                      {`[workflow]`, `base = ""`},
		`sets [workflow] ci to ""`:                                        {`[workflow]`, `ci = ""`},
		`sets [tracker] team to ""`:                                       {`[tracker]`, `kind = "linear"`, `team = ""`},
		`sets [ports] variable to ""`:                                     {`[ports]`, `variable = ""`},
		`sets [review] read to ""`:                                        {`[review]`, `read = ""`},
		`has an empty entry "" in [workflow] local_runs, take it out`:     {`[workflow]`, `local_runs = ["pnpm test", ""]`},
		`has an empty entry "" in [ci] setup`:                             {`[ci]`, `stack = "other"`, `setup = [""]`},
		`has an empty entry " " in [workflow] footers`:                    {`[workflow]`, `footers = [" "]`},
		"has [review] categories = [], name what the review reports":      {`[review]`, `categories = []`},
		`has [workflow] review_line "review:\nsha" on more than one line`: {`[workflow]`, `review_line = "review:\nsha"`},
		"has an empty command in [workflow] generate":                     {`[workflow]`, `generate_list = ["x"]`, `generate = [""]`},
	})
	addProject(t, "wrong", `[[roles]]`, `model = "x"`)
	if _, err := Load("wrong"); err == nil || !strings.Contains(err.Error(), "has a role without a name, give each [[roles]] its name") {
		t.Fatal(err)
	}
}

func TestAModuleWithoutTheKeyItNeedsIsRefused(t *testing.T) {
	setupHome(t)
	refused(t, map[string][]string{
		"has the ci module without [ci] stack, use pnpm, npm, go or other":               {`modules = ["ci"]`, `[workflow]`, `checks = ["test"]`},
		"has the ports module without [ports.offsets], give each role its port offset":   {`modules = ["ports"]`, `[ports]`, `variable = "PORT_OFFSET"`},
		"has the tracker module with no tracker, set [tracker] kind to linear or github": {`modules = ["tracker"]`},
	})
}

func TestGateAndGenerateCommandsMayNotUseTheShell(t *testing.T) {
	setupHome(t)
	for _, c := range []string{"pnpm lint && pnpm test", "a || b", "a | b", "a; b", "a > log", "a < in", "echo `date`", "echo $(date)", `go test -run "Foo"`, "go test -run 'Foo'"} {
		value := `['''` + c + `''']`
		for key, lines := range map[string][]string{
			"gate":          {`[workflow]`, `gate = ` + value},
			"generate_list": {`[workflow]`, `generate_list = ` + value, `generate = ["x"]`, `generated = '^wrote (.+)$'`},
			"generate":      {`[workflow]`, `generate_list = ["x --list"]`, `generate = ` + value, `generated = '^wrote (.+)$'`},
		} {
			addProject(t, "wrong", append(lines, `[[roles]]`, `name = "lead"`)...)
			_, err := Load("wrong")
			if err == nil || !strings.Contains(err.Error(), " in [workflow] "+key+", which mingo runs without a shell, so ") || !strings.HasSuffix(err.Error(), "does not work there, put the line in a package script or a script file") {
				t.Fatal(c, key, err)
			}
		}
	}
	addProject(t, "right", `[workflow]`, `checks = ["test"]`, `gate = ["go vet ./...", "go test -race ./..."]`, `local_runs = ["pnpm a && pnpm b"]`, `[ci]`, `stack = "go"`, `[ci.run]`, `test = "go test -run 'Foo: bar' | tee log"`, `[[roles]]`, `name = "lead"`)
	mustLoad(t, "right")
}

func TestARoleWhoseSessionAnotherProjectHasTooIsRefusedNamingBoth(t *testing.T) {
	setupHome(t)
	addProject(t, "shop", `[[roles]]`, `name = "dev-api"`, `[[roles]]`, `name = "web"`)
	addProject(t, "shop-dev", `[[roles]]`, `name = "api"`)
	want := map[string]string{
		"shop":     "shop.toml has the role dev-api and project shop-dev the role api, which are both the tmux session shop-dev-api, rename one of them",
		"shop-dev": "shop-dev.toml has the role api and project shop the role dev-api, which are both the tmux session shop-dev-api, rename one of them",
	}
	for name, w := range want {
		if _, err := Load(name); err == nil || !strings.HasSuffix(err.Error(), w) {
			t.Fatal(name, err)
		}
	}
	addProject(t, "shop-dev", `[[roles]]`, `name = "web"`)
	mustLoad(t, "shop")
	mustLoad(t, "shop-dev")
}

func TestInsideTheCloneOfAConfigThatFailsATargetSaysWhatIsWrong(t *testing.T) {
	home := setupHome(t)
	addDemo(t, home)
	addProject(t, "other", `[[roles]]`, `name = "solo"`)
	addProject(t, "demo", `[[roles]]`, `name = "lead"`, `modles = "x"`, `[[roles]]`, `name = "helper"`)
	t.Chdir(filepath.Join(home, "dev", "demo", "helper"))
	if name, ok := ProjectOfDir(Cwd()); !ok || name != "demo" {
		t.Fatal(name, ok)
	}
	if _, _, err := ParseTarget("lead"); err == nil || !strings.Contains(err.Error(), "demo.toml has the key [[roles]] modles") {
		t.Fatal(err)
	}
	if err := os.WriteFile(File("demo"), []byte("[[roles]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseTarget("lead"); err == nil || !strings.Contains(err.Error(), "demo.toml: toml") {
		t.Fatal(err)
	}
	t.Chdir(home)
	if _, _, err := ParseTarget("lead"); err == nil || !strings.Contains(err.Error(), "which project?") {
		t.Fatal(err)
	}
}
