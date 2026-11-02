package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/team"
)

func teamProject() (*config.Project, string, error) {
	name := os.Getenv("MINGO_PROJECT")
	if name == "" {
		var ok bool
		if name, ok = config.ProjectOfDir(config.Cwd()); !ok {
			return nil, "", errors.New("run it in a project's clone, or with MINGO_PROJECT set")
		}
	}
	p, err := config.Find(name)
	if err != nil {
		return nil, "", err
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, "", errors.New(config.Cwd() + " is in no git repository")
	}
	root := strings.TrimSpace(string(out))
	if !p.HoldsDir(root) {
		return nil, "", fmt.Errorf("%s is none of %s's clones", root, p.Name)
	}
	return p, root, nil
}

func (e *env) pr(args []string) error {
	if len(args) == 0 || (args[0] != "merge" && args[0] != "rebase") {
		return errors.New("pr merge <number> | pr rebase <branch>")
	}
	p, root, err := teamProject()
	if err != nil {
		return err
	}
	var code int
	if args[0] == "merge" {
		code, err = team.Merge(team.MergeDeps{Root: root, Project: p, Run: team.Capture, Out: e.out}, args[1:])
	} else {
		code, err = team.Rebase(team.RebaseDeps{
			Root: root, Project: p, Run: team.Capture, Out: e.out, TempRoot: os.TempDir(),
			Generators: team.ConfigGenerators(&p.Workflow, team.Capture),
		}, args[1:])
	}
	if err != nil {
		return err
	}
	return exitError(code)
}

func (e *env) gate(args []string) error {
	p, root, err := teamProject()
	if err != nil {
		return err
	}
	code, err := team.Gate(team.GateDeps{Root: root, Targets: p.Workflow.Gate, RunTarget: team.RealTarget(root), Out: e.out, Now: time.Now}, args)
	if err != nil {
		return err
	}
	return exitError(code)
}
