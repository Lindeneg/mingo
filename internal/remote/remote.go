// Package remote keeps the hosts mingo drives from the owner's computer, and installs and runs mingo on each over ssh.
package remote

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Lindeneg/mingo/internal/config"
)

// the module go install builds mingo from on a host
const Module = "github.com/Lindeneg/mingo/cmd/mingo"

// the exit code of a mingo asked to run as another version than its own
const VersionDiffers = 75

type Host struct {
	Name  string `toml:"name"`
	SSH   string `toml:"ssh"`
	Local bool   `toml:"local"`
}

func (h Host) Where() string {
	if h.Local {
		return "this machine"
	}
	return h.SSH
}

func File() string {
	return filepath.Join(config.Dir(), "hosts.toml")
}

// the hosts the file lists, none when there is no file
func Load() ([]Host, error) {
	file := File()
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f struct {
		Hosts []Host `toml:"hosts"`
	}
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", file, err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return nil, fmt.Errorf("%s has the key %s, which mingo does not know", file, keys[0])
	}
	seen, local := map[string]bool{}, ""
	for _, h := range f.Hosts {
		switch {
		case !config.IsName(h.Name):
			return nil, fmt.Errorf("%s has a host named %q, use lowercase letters, digits and hyphens", file, h.Name)
		case seen[h.Name]:
			return nil, fmt.Errorf("%s has the host %s twice", file, h.Name)
		case h.Local == (h.SSH != ""):
			return nil, fmt.Errorf("%s has the host %s, which needs either ssh or local = true", file, h.Name)
		case h.Local && local != "":
			return nil, fmt.Errorf("%s has %s and %s as this machine, it is one host", file, local, h.Name)
		case strings.HasPrefix(h.SSH, "-"):
			return nil, fmt.Errorf("%s has the host %s reached at %q, which ssh would read as an option", file, h.Name, h.SSH)
		}
		seen[h.Name] = true
		if h.Local {
			local = h.Name
		}
	}
	return f.Hosts, nil
}

func Save(hosts []Host) error {
	var b strings.Builder
	b.WriteString("# the machines mingo drives from this computer, which mingo host add and rm keep\n")
	for _, h := range hosts {
		fmt.Fprintf(&b, "\n[[hosts]]\nname = %q\n", h.Name)
		if h.Local {
			b.WriteString("local = true\n")
		} else {
			fmt.Fprintf(&b, "ssh = %q\n", h.SSH)
		}
	}
	if err := os.MkdirAll(filepath.Dir(File()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(File(), []byte(b.String()), 0o644)
}

func Find(hosts []Host, name string) (Host, bool) {
	for _, h := range hosts {
		if h.Name == name {
			return h, true
		}
	}
	return Host{}, false
}

// whether every host is reached over ssh, so nothing runs on this computer itself
func OnlySSH(hosts []Host) bool {
	for _, h := range hosts {
		if h.Local {
			return false
		}
	}
	return len(hosts) > 0
}

// a word as the shell at the other end reads it back
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// the command line that runs the host's mingo, which mingo host add put in ~/.local/bin
func Command(args []string) string {
	line := "~/.local/bin/mingo"
	for _, a := range args {
		line += " " + Quote(a)
	}
	return line
}

func tool() string {
	if v := os.Getenv("MINGO_SSH"); v != "" {
		return v
	}
	return "ssh"
}

// no prompt can wait for an answer nobody gives, and one connection per host serves every call for
// a while, where ssh can share one
func options(tty bool) []string {
	opts := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		dir := filepath.Join(config.StateDir(), "ssh")
		if os.MkdirAll(dir, 0o700) == nil {
			opts = append(opts, "-o", "ControlMaster=auto", "-o", "ControlPath="+filepath.Join(dir, "%C"), "-o", "ControlPersist=10m")
		}
	}
	if tty {
		opts = append(opts, "-t")
	}
	return opts
}

// the ssh command that runs a shell line on the host
func Cmd(h Host, line string, tty bool) *exec.Cmd {
	return exec.Command(tool(), append(options(tty), h.SSH, line)...)
}

// what a shell line printed on the host, and an error with what ssh or the line said on stderr
func Output(h Host, line string) (string, error) {
	cmd := Cmd(h, line, false)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if text := strings.TrimSpace(stderr.String()); text != "" {
			return string(out), fmt.Errorf("%s: %s", h.SSH, text)
		}
		return string(out), fmt.Errorf("%s: %v", h.SSH, err)
	}
	return string(out), nil
}

// a mingo built outside a commit has no version a host can install
func Installable(version string) error {
	switch {
	case version == "" || version == "devel" || version == "(devel)":
		return errors.New("this mingo has no version from git, so a host cannot install the same one, build it in a clone of mingo")
	case strings.HasSuffix(version, "+dirty"):
		return fmt.Errorf("this mingo is %s, built from uncommitted changes, so a host cannot install the same one, build it from a commit pushed to GitHub", version)
	}
	return nil
}

const findGo = `for g in "$(command -v go 2>/dev/null)" /usr/local/go/bin/go "$HOME/.local/go/bin/go"; do if [ -n "$g" ] && [ -x "$g" ]; then printf '%s\n' "$g"; exit 0; fi; done; exit 1`

// builds this version of mingo on the host with go install, and lets it install itself there
func Install(h Host, version string, out io.Writer) error {
	if err := Installable(version); err != nil {
		return err
	}
	system, err := Output(h, "uname -sm")
	if err != nil {
		return err
	}
	if f := strings.Fields(system); len(f) == 0 || f[0] != "Linux" && f[0] != "Darwin" {
		return fmt.Errorf("%s answers uname with %q, and a host is Linux or macOS", h.SSH, strings.TrimSpace(system))
	}
	goTool, err := Output(h, "sh -c "+Quote(findGo))
	goTool = strings.TrimSpace(goTool)
	if err != nil || goTool == "" {
		return fmt.Errorf("%s has no Go, which mingo is installed with there, see https://go.dev/dl", h.SSH)
	}
	script := fmt.Sprintf(`set -e; dir=$(mktemp -d); trap 'rm -rf "$dir"' EXIT; GOBIN="$dir" %s install %s; "$dir/mingo" install`,
		Quote(goTool), Quote(Module+"@"+version))
	cmd := Cmd(h, "sh -c "+Quote(script), false)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installing mingo %s on %s failed: %v", version, h.SSH, err)
	}
	return nil
}

// the version of the host's mingo
func Version(h Host) (string, error) {
	out, err := Output(h, Command([]string{"version"}))
	if err != nil {
		return "", err
	}
	v, ok := strings.CutPrefix(strings.TrimSpace(out), "mingo ")
	if !ok {
		return "", fmt.Errorf("%s: its mingo answers version with %q", h.SSH, strings.TrimSpace(out))
	}
	return v, nil
}
