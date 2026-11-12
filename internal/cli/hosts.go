package cli

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/Lindeneg/mingo"
	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/remote"
)

func (e *env) hosts(args []string) error {
	if len(args) == 0 {
		return badUsage("", "host")
	}
	action, args := args[0], args[1:]
	switch action {
	case "add":
		if len(args) != 2 {
			return badUsage("", "host", "add")
		}
		return e.hostAdd(args[0], args[1])
	case "ls":
		if err := takes(args, 0, "host", "ls"); err != nil {
			return err
		}
		return e.hostLs()
	case "rm":
		if err := exactlyOne(args, "host", "rm"); err != nil {
			return err
		}
		return e.hostRm(args[0])
	}
	return badUsage(fmt.Sprintf("unknown command %q", "host "+action), "host")
}

func (e *env) hostAdd(name, where string) error {
	if !config.IsName(name) {
		return fmt.Errorf("%q is no host name, use lowercase letters, digits and hyphens", name)
	}
	hosts, err := remote.Load()
	if err != nil {
		return err
	}
	have, known := remote.Find(hosts, name)
	if where == "--local" {
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
			return fmt.Errorf("this machine runs %s, and a host is Linux or macOS", runtime.GOOS)
		}
		for _, h := range hosts {
			if h.Local && h.Name != name {
				return fmt.Errorf("this machine is the host %s already", h.Name)
			}
		}
		if known && !have.Local {
			return fmt.Errorf("%s is a host already, reached at %s, mingo host rm %s first", name, have.SSH, name)
		}
		if !known {
			hosts = append(hosts, remote.Host{Name: name, Local: true})
			if err := remote.Save(hosts); err != nil {
				return err
			}
		}
		fmt.Fprintf(e.out, "host %s: this machine\n", name)
		return nil
	}
	if strings.HasPrefix(where, "-") {
		return badUsage(fmt.Sprintf("unknown flag %q", where), "host", "add")
	}
	if known && have.SSH != where {
		return fmt.Errorf("%s is a host already, reached at %s, mingo host rm %s first", name, have.Where(), name)
	}
	h := remote.Host{Name: name, SSH: where}
	version := mingo.Version()
	if err := remote.Install(h, version, e.out); err != nil {
		return err
	}
	if !known {
		hosts = append(hosts, h)
		if err := remote.Save(hosts); err != nil {
			return err
		}
	}
	fmt.Fprintf(e.out, "host %s: mingo %s on %s\n", name, version, where)
	return nil
}

func (e *env) hostLs() error {
	hosts, err := remote.Load()
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		fmt.Fprintf(e.out, "no hosts in %s, so mingo works on this machine alone\n", remote.File())
		return nil
	}
	rows := [][]string{{"HOST", "WHERE", "MINGO"}}
	for _, h := range hosts {
		version := mingo.Version()
		if !h.Local {
			if version, err = remote.Version(h); err != nil {
				version = strings.ReplaceAll(err.Error(), "\n", " ")
			}
		}
		rows = append(rows, []string{h.Name, h.Where(), version})
	}
	widths := []int{0, 0}
	for _, row := range rows {
		widths[0], widths[1] = max(widths[0], len(row[0])), max(widths[1], len(row[1]))
	}
	for _, row := range rows {
		fmt.Fprintf(e.out, "%-*s  %-*s  %s\n", widths[0], row[0], widths[1], row[1], row[2])
	}
	return nil
}

func (e *env) hostRm(name string) error {
	hosts, err := remote.Load()
	if err != nil {
		return err
	}
	var kept []remote.Host
	for _, h := range hosts {
		if h.Name != name {
			kept = append(kept, h)
		}
	}
	if len(kept) == len(hosts) {
		return fmt.Errorf("no host named %s, mingo host ls lists them", name)
	}
	if err := remote.Save(kept); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "host %s: taken off the list, mingo stays installed there\n", name)
	return nil
}

// a computer asks a host to run as the computer's own version, and a host on another one says so
// with its own exit code, which tells the computer to install the same version there and ask again
func expect(args []string) ([]string, error) {
	if len(args) < 2 || args[0] != "--expect" {
		return args, nil
	}
	if want := args[1]; want != mingo.Version() {
		return nil, fmt.Errorf("this host runs mingo %s, not %s", mingo.Version(), want)
	}
	return args[2:], nil
}
