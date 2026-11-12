package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Lindeneg/mingo/internal/config"
)

func (e *env) away(name string, away bool) error {
	command := "back"
	if away {
		command = "away"
	}
	if name == "" {
		return badUsage("", command)
	}
	p, err := config.Find(name)
	if err != nil {
		return err
	}
	if !p.CanBeAway() {
		return fmt.Errorf("%s has no team module, whose TEAM.md has the rules for when the owner is away", p.Name)
	}
	if config.Away(p.Name) == away {
		if away {
			fmt.Fprintf(e.out, "%s: the owner is already away\n", p.Name)
		} else {
			fmt.Fprintf(e.out, "%s: the owner is not away\n", p.Name)
		}
		return nil
	}
	file, told := config.AwayFile(p.Name), config.BackTold
	if away {
		told = config.AwayTold
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "%s: the owner is away, and the sessions work by Working alone in TEAM.md until mingo back %s\n", p.Name, p.Name)
	} else {
		if err := os.Remove(file); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "%s: the owner is back, and the sessions ask again\n", p.Name)
	}
	// a session between two is told by its next opener, since keys sent now would become that opener's reply
	for _, r := range p.RoleNames() {
		session := p.SessionName(r)
		if !e.host.Has(session) || !has(marker(p, r, "running")) {
			continue
		}
		if err := e.host.SendText(session, told); err != nil {
			return err
		}
		if err := e.host.SendKeys(session, "Enter"); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "%s: told\n", session)
	}
	return nil
}
