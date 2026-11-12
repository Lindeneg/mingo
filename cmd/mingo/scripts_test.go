//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryShellScriptParses(t *testing.T) {
	root := repoRoot()
	var files []string
	for _, pattern := range []string{"provision/*.sh"} {
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no scripts found")
	}
	for _, f := range files {
		if out, err := exec.Command("bash", "-n", f).CombinedOutput(); err != nil {
			t.Fatal(f, string(out))
		}
	}
}

func (h *home) publicKey() string {
	key := filepath.Join(h.root, "key.pub")
	h.write(key, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample you@pc\n")
	return key
}

func TestTheCloudConfigCarriesTheKeysAndProvisionAndRunsItAsTheUser(t *testing.T) {
	h := newHome(t)
	r := h.runIn("", []string{"MINGO_USER=sam"}, filepath.Join(repoRoot(), "provision", "make-cloud-init.sh"), h.publicKey())
	if r.code != 0 || r.lines()[0] != "#cloud-config" ||
		!strings.Contains(r.out, "      ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample you@pc") ||
		!strings.Contains(r.out, "      # Provisions a fresh Ubuntu") ||
		!strings.Contains(r.out, "MINGO_USER=sam MINGO_SSH_PUBKEY_FILE=/root/mingo/authorized_keys /root/mingo/provision.sh") {
		t.Fatal(r.out)
	}
}

func TestTheCloudConfigNeedsTheUser(t *testing.T) {
	h := newHome(t)
	for _, env := range [][]string{nil, {"MINGO_USER=Sam Smith"}} {
		r := h.runIn("", env, filepath.Join(repoRoot(), "provision", "make-cloud-init.sh"), h.publicKey())
		if r.code != 2 || !strings.Contains(r.out, "set MINGO_USER to a plain user name") {
			t.Fatal(env, r.code, r.out)
		}
	}
}

func TestTheCloudConfigRefusesAFileThatIsNotAPublicKey(t *testing.T) {
	h := newHome(t)
	id := filepath.Join(h.root, "id")
	h.write(id, "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n")
	r := h.runIn("", nil, filepath.Join(repoRoot(), "provision", "make-cloud-init.sh"), id)
	if r.code != 2 || !strings.Contains(r.out, "not a public key file") {
		t.Fatal(r.code, r.out)
	}
}

func TestProvisionRefusesToRunAsAnyoneButRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("runs as root")
	}
	h := newHome(t)
	r := h.runIn("", nil, filepath.Join(repoRoot(), "provision", "provision.sh"))
	if r.code == 0 || !strings.Contains(r.out, "run as root") {
		t.Fatal(r.out)
	}
}
