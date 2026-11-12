package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withConfig(t *testing.T, text string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MINGO_CONFIG", dir)
	if text != "" {
		if err := os.WriteFile(filepath.Join(dir, "hosts.toml"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNoFileMeansNoHosts(t *testing.T) {
	withConfig(t, "")
	if hosts, err := Load(); err != nil || len(hosts) != 0 || OnlySSH(hosts) {
		t.Fatal(hosts, err)
	}
}

func TestSavedHostsLoadBackInTheirOrder(t *testing.T) {
	withConfig(t, "")
	want := []Host{{Name: "server", SSH: "me@server"}, {Name: "mac", Local: true}, {Name: "box", SSH: `odd "host"`}}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatal(got, err)
	}
	if OnlySSH(got) || !OnlySSH(got[:1]) {
		t.Fatal("only ssh")
	}
}

func TestAHostsFileMingoCannotUseIsRefusedNamingWhatIsWrong(t *testing.T) {
	for want, text := range map[string]string{
		`has a host named "Big", use lowercase`:             "[[hosts]]\nname = \"Big\"\nssh = \"x\"\n",
		"has the host a twice":                              "[[hosts]]\nname = \"a\"\nssh = \"x\"\n[[hosts]]\nname = \"a\"\nssh = \"y\"\n",
		"has the host a, which needs either ssh or local":   "[[hosts]]\nname = \"a\"\n",
		"has the host b, which needs either ssh or local":   "[[hosts]]\nname = \"b\"\nssh = \"x\"\nlocal = true\n",
		"has a and b as this machine, it is one host":       "[[hosts]]\nname = \"a\"\nlocal = true\n[[hosts]]\nname = \"b\"\nlocal = true\n",
		`reached at "-oProxyCommand=x", which ssh would`:    "[[hosts]]\nname = \"a\"\nssh = \"-oProxyCommand=x\"\n",
		"has the key hosts.port, which mingo does not know": "[[hosts]]\nname = \"a\"\nssh = \"x\"\nport = 22\n",
	} {
		withConfig(t, text)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q, got %v", want, err)
		}
	}
}

func TestTheHostsMingoIsCalledWithEachArgQuotedForItsShell(t *testing.T) {
	if got := Command([]string{"say", "acme/dev", "it's done; rm -rf ~"}); got != `~/.local/bin/mingo 'say' 'acme/dev' 'it'\''s done; rm -rf ~'` {
		t.Fatal(got)
	}
}

func TestOnlyAVersionFromACleanCommitCanBeInstalledOnAHost(t *testing.T) {
	if err := Installable("v0.0.0-20261112000146-aee1f73252ae"); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"v0.0.0-20261112000146-aee1f73252ae+dirty", "devel", "(devel)", ""} {
		if Installable(v) == nil {
			t.Fatal(v)
		}
	}
}
