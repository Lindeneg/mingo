// Package install sets up a machine for mingo: the command, tmux's config and each agent CLI's settings.
package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/provider"
	"github.com/Lindeneg/mingo/internal/remote"
)

func BinPath(home string) string {
	return filepath.Join(home, ".local", "bin", "mingo")
}

func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

// written beside the target and renamed over it, so a link there is replaced and never written through
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".mingo-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func Bin(home string, out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	target := BinPath(home)
	if !sameFile(exe, target) {
		data, err := os.ReadFile(exe)
		if err != nil {
			return err
		}
		if err := WriteAtomic(target, data, 0o755); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "command: %s\n", target)
	return nil
}

const tmuxConf = `set -g mouse on
set -g focus-events on
set -g history-limit 50000
`

func TmuxConf(home string, out io.Writer) error {
	path := filepath.Join(home, ".tmux.conf")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.WriteFile(path, []byte(tmuxConf), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(out, "tmux: wrote ~/.tmux.conf")
	return nil
}

// encoding/json says where the text stops being JSON, which ojson does not keep
func NotJSON(file string, data []byte, err error) error {
	var syntax *json.SyntaxError
	if errors.As(json.Unmarshal(data, new(any)), &syntax) {
		at := min(max(int(syntax.Offset)-1, 0), len(data))
		before := data[:at]
		line, column := bytes.Count(before, []byte("\n"))+1, at-bytes.LastIndexByte(before, '\n')
		return fmt.Errorf("%s is not valid JSON at line %d, column %d (%v), left as it is", file, line, column, syntax)
	}
	return fmt.Errorf("%s is not valid JSON (%v), left as it is", file, err)
}

// hooks run through sh, so a path with a character sh reads is quoted
func ShellWord(s string) string {
	plain := s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:@,", r))
	}) < 0
	if plain {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// the file a chain of links ends at, which need not exist yet
func LinkTarget(path string) (string, error) {
	for range 40 {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
		to, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(to) {
			to = filepath.Join(filepath.Dir(path), to)
		}
		path = to
	}
	return "", fmt.Errorf("%s is part of a loop of links", path)
}

func Run(out io.Writer) error {
	home := config.Home()
	hosts, err := remote.Load()
	if err != nil {
		return err
	}
	// tmux and the agent CLIs run on the hosts, and this computer only reaches them
	if remote.OnlySSH(hosts) {
		if err := Bin(home, out); err != nil {
			return err
		}
		fmt.Fprintf(out, "every host in %s is reached over ssh, so this machine needs the command alone\n", remote.File())
		fmt.Fprintln(out, "mingo help lists the commands")
		return nil
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		if runtime.GOOS == "darwin" {
			return errors.New("tmux is missing, brew install tmux")
		}
		return errors.New("tmux is missing, see provision/provision.sh")
	}
	for _, step := range []func(string, io.Writer) error{Bin, TmuxConf} {
		if err := step(home, out); err != nil {
			return err
		}
	}
	for _, name := range provider.Names() {
		p, _ := provider.Get(name)
		if i, ok := p.(provider.Installer); ok {
			if err := i.Install(home, out); err != nil {
				return err
			}
		}
	}
	fmt.Fprintln(out, "mingo help lists the commands")
	return nil
}
