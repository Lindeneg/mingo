// Package host runs the tmux sessions the roles live in.
package host

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Lindeneg/mingo/internal/sys"
)

type Host interface {
	Has(session string) bool
	New(session, dir string, argv []string) error
	Attach(session string, readOnly bool) error
	Peek(session string, lines int) (string, error)
	SendKeys(session string, keys ...string) error
	SendText(session, text string) error
	Kill(session string) error
	List() ([]string, error)
}

func New() Host {
	return Tmux{Socket: os.Getenv("MINGO_TMUX_SOCKET")}
}

type Tmux struct {
	Socket string
}

func (t Tmux) args(rest ...string) []string {
	if t.Socket != "" {
		return append([]string{"-S", t.Socket}, rest...)
	}
	return rest
}

func (t Tmux) run(rest ...string) ([]byte, error) {
	cmd := exec.Command("tmux", t.args(rest...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		return out, errors.New(strings.TrimSpace(stderr.String()))
	}
	return out, err
}

func target(session string) string {
	return "=" + session
}

// a pane target ends in a colon, else tmux reads the session name as a window
func pane(session string) string {
	return "=" + session + ":"
}

func (t Tmux) Has(session string) bool {
	_, err := t.run("has-session", "-t", target(session))
	return err == nil
}

func (t Tmux) New(session, dir string, argv []string) error {
	_, err := t.run(append([]string{"new-session", "-d", "-s", session, "-c", dir}, argv...)...)
	return err
}

func (t Tmux) Attach(session string, readOnly bool) error {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	rest := []string{"attach"}
	if readOnly {
		rest = append(rest, "-r")
	}
	rest = append(rest, "-t", target(session))
	return sys.Exec(path, append([]string{"tmux"}, t.args(rest...)...), os.Environ())
}

func (t Tmux) Peek(session string, lines int) (string, error) {
	out, err := t.run("capture-pane", "-p", "-J", "-t", pane(session), "-S", "-"+strconv.Itoa(lines))
	if err != nil {
		return "", err
	}
	var kept []string
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line != "" {
			kept = append(kept, line)
		}
	}
	if len(kept) > lines {
		kept = kept[len(kept)-lines:]
	}
	if len(kept) == 0 {
		return "", nil
	}
	return strings.Join(kept, "\n") + "\n", nil
}

func (t Tmux) SendKeys(session string, keys ...string) error {
	_, err := t.run(append([]string{"send-keys", "-t", pane(session)}, keys...)...)
	return err
}

func (t Tmux) SendToPane(paneID string, keys ...string) error {
	_, err := t.run(append([]string{"send-keys", "-t", paneID}, keys...)...)
	return err
}

func (t Tmux) SendText(session, text string) error {
	_, err := t.run("send-keys", "-t", pane(session), "-l", "--", text)
	return err
}

func (t Tmux) Kill(session string) error {
	_, err := t.run("kill-session", "-t", target(session))
	return err
}

func (t Tmux) List() ([]string, error) {
	out, err := t.run("list-sessions", "-F", "#{session_name}")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}
