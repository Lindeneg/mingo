//go:build unix

package sys

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func plistStrings(t *testing.T, home, project string) []string {
	t.Helper()
	tmpl, err := os.ReadFile(filepath.Join("..", "..", "share", "mingo.plist"))
	if err != nil {
		t.Fatal(err)
	}
	plist, err := RenderPlist(tmpl, home, project)
	if err != nil {
		t.Fatal(err)
	}
	d := xml.NewDecoder(strings.NewReader(string(plist)))
	var out []string
	in := false
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err, string(plist))
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			if in = tok.Name.Local == "string"; in {
				out = append(out, "")
			}
		case xml.CharData:
			if in {
				out[len(out)-1] += string(tok)
			}
		case xml.EndElement:
			in = false
		}
	}
}

func TestThePlistRunsTheProjectsAgentWithEscapedPaths(t *testing.T) {
	home := "/Users/a&b <c>"
	got := plistStrings(t, home, "demo")
	if len(got) != 10 {
		t.Fatal(len(got), got)
	}
	want := map[int]string{
		0: "mingo.demo", 1: "/bin/sh", 2: "-c", 4: "mingo.demo", 5: home + "/.local/bin/mingo", 6: "demo",
		7: home + "/.local/bin:/opt/homebrew/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin",
		8: home + "/Library/Logs/mingo.demo.log", 9: home + "/Library/Logs/mingo.demo.log",
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("%d: %q", i, got[i])
		}
	}
	if Label("demo") != "mingo.demo" || PlistPath("/h", "demo") != "/h/Library/LaunchAgents/mingo.demo.plist" {
		t.Fatal(Label("demo"), PlistPath("/h", "demo"))
	}
}

func TestThePlistsScriptStartsTheProjectAndStopsItOnSigterm(t *testing.T) {
	script := plistStrings(t, "/h", "demo")[3]
	if !strings.Contains(script, "/usr/bin/host api.anthropic.com") {
		t.Fatal(script)
	}
	script = strings.Replace(script, "/usr/bin/host api.anthropic.com", "true", 1)
	dir := t.TempDir()
	calls, fake := filepath.Join(dir, "calls"), filepath.Join(dir, "mingo")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"$*\" >>'"+calls+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", script, "mingo.demo", fake, "demo")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	read := func() string {
		data, _ := os.ReadFile(calls)
		return string(data)
	}
	for i := 0; i < 50 && read() == ""; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if read() != "start --autostart demo\n" {
		t.Fatal(read())
	}
	time.Sleep(500 * time.Millisecond)
	cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the agent did not end on sigterm")
	}
	if read() != "start --autostart demo\nstop demo\n" {
		t.Fatal(read())
	}
}
