package sys

import (
	"bufio"
	"errors"
	"regexp"
	"strings"
	"testing"
)

type rawState struct {
	raw, restored int
}

// a prompter reading keys from what was typed, with a terminal that takes them one at a time
// unless lines is set
func typed(text string, color, lines bool) (*Prompter, *strings.Builder, *rawState) {
	out := &strings.Builder{}
	state := &rawState{}
	p := &Prompter{in: bufio.NewReader(strings.NewReader(text)), out: out, color: color, width: func() int { return 40 }}
	if !lines {
		p.makeRaw = func() (func() error, error) {
			state.raw++
			return func() error { state.restored++; return nil }, nil
		}
	}
	return p, out, state
}

var escapes = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// what was drawn, without its escapes and carriage returns
func plain(s string) string {
	return strings.ReplaceAll(escapes.ReplaceAllString(s, ""), "\r", "")
}

func TestKeysComeFromBytesAndEscapeSequences(t *testing.T) {
	p, _, _ := typed("a\r\n\x7f\x03\x1b[A\x1b[B\x1b[C\x1b[D\x1bOH\x1b[F\x1b[3~\x1b[1~\x01\x05\x15\x17é", false, false)
	want := []key{
		{kind: keyRune, r: 'a'}, {kind: keyEnter}, {kind: keyEnter}, {kind: keyBack}, {kind: keyStop},
		{kind: keyUp}, {kind: keyDown}, {kind: keyRight}, {kind: keyLeft}, {kind: keyHome}, {kind: keyEnd}, {kind: keyDelete},
		{kind: keyHome}, {kind: keyHome}, {kind: keyEnd}, {kind: keyKillLine}, {kind: keyKillWord}, {kind: keyRune, r: 'é'},
	}
	for i, w := range want {
		k, err := p.readKey()
		if err != nil || k != w {
			t.Fatal(i, k, err)
		}
	}
	if _, err := p.readKey(); err == nil {
		t.Fatal("read past the end")
	}
}

func TestTheEditorMovesInsertsAndDeletesAtTheCursor(t *testing.T) {
	e := editor{}
	for _, k := range []key{{kind: keyRune, r: 'a'}, {kind: keyRune, r: 'c'}, {kind: keyLeft}, {kind: keyRune, r: 'b'}, {kind: keyEnd}, {kind: keyRune, r: ' '}, {kind: keyRune, r: 'd'}} {
		e.edit(k)
	}
	if string(e.text) != "abc d" || e.at != 5 {
		t.Fatal(string(e.text), e.at)
	}
	e.edit(key{kind: keyKillWord})
	if string(e.text) != "abc " {
		t.Fatalf("%q", string(e.text))
	}
	e.edit(key{kind: keyHome})
	e.edit(key{kind: keyDelete})
	e.edit(key{kind: keyBack})
	if string(e.text) != "bc " || e.at != 0 {
		t.Fatal(string(e.text), e.at)
	}
	e.edit(key{kind: keyRight})
	e.edit(key{kind: keyKillLine})
	if string(e.text) != "c " || e.at != 0 || e.edit(key{kind: keyUp}) {
		t.Fatal(string(e.text), e.at)
	}
}

func TestTextTakesTheDefaultOnEnterAndAsksAgainUntilTheCheckHolds(t *testing.T) {
	p, out, state := typed("\rfoo\r\x15x/demo\r\r", false, false)
	var checked []string
	check := func(v string) error {
		checked = append(checked, v)
		switch v {
		case "":
			return errors.New("this needs an answer")
		case "foo":
			return errors.New(`"foo" is not owner/name`)
		}
		return nil
	}
	v, err := p.Text("the repository", "", "", "", check)
	if err != nil || v != "x/demo" || strings.Join(checked, ",") != ",foo,x/demo" {
		t.Fatal(v, err, checked)
	}
	shown := plain(out.String())
	if !strings.Contains(shown, "? the repository: \n  this needs an answer") || !strings.Contains(shown, "? the repository: foo\n  \"foo\" is not owner/name") || !strings.HasSuffix(shown, "✓ the repository: x/demo\n") {
		t.Fatalf("%q", shown)
	}
	v, err = p.Text("the base branch", "master", "", "", nil)
	if err != nil || v != "master" || !strings.HasSuffix(plain(out.String()), "? the base branch (master): ✓ the base branch: master\n") {
		t.Fatalf("%q %v %q", v, err, plain(out.String()))
	}
	if state.raw != 2 || state.restored != 2 {
		t.Fatal(state)
	}
}

func TestTextStartsFromAGivenValueWithItsProblemShown(t *testing.T) {
	p, out, _ := typed("\x17x/y\r", false, false)
	v, err := p.Text("the repository", "", "you/x", `"you/x" is not there`, nil)
	if err != nil || v != "x/y" || !strings.Contains(plain(out.String()), "? the repository: you/x\n  \"you/x\" is not there") {
		t.Fatalf("%q %v %q", v, err, plain(out.String()))
	}
}

type finalErr struct{ error }

func (finalErr) Final() bool { return true }

func TestAFinalProblemEndsTheQuestionWithIt(t *testing.T) {
	p, _, state := typed("x/y\rmore\r", false, false)
	gone := finalErr{errors.New("gh is not logged in, run gh auth login")}
	if _, err := p.Text("the repository", "", "", "", func(string) error { return gone }); err != error(gone) || state.restored != 1 {
		t.Fatal(err, state)
	}
	p, _, _ = typed("x/y\n", false, true)
	if _, err := p.Text("the repository", "", "", "", func(string) error { return gone }); err != error(gone) {
		t.Fatal(err)
	}
}

func TestCtrlCStopsAnyQuestionAndPutsTheTerminalBack(t *testing.T) {
	for name, ask := range map[string]func(p *Prompter) error{
		"text":    func(p *Prompter) error { _, err := p.Text("the repository", "", "", "", nil); return err },
		"choose":  func(p *Prompter) error { _, err := p.Choose("the preset", []string{"a", "b"}, nil, "", ""); return err },
		"confirm": func(p *Prompter) error { _, err := p.Confirm("Continue?"); return err },
	} {
		p, out, state := typed("j\x03", false, false)
		if err := ask(p); !errors.Is(err, ErrStopped) || state.raw != 1 || state.restored != 1 || !strings.HasSuffix(out.String(), "\n"+showCursor) {
			t.Fatal(name, err, state, out.String())
		}
	}
	p, _, _ := typed("", false, true)
	if _, err := p.Text("the repository", "", "", "", nil); !errors.Is(err, ErrStopped) {
		t.Fatal(err)
	}
}

func TestChooseStartsAtTheDefaultAndMovesWithArrowsOrJAndKAroundTheEnds(t *testing.T) {
	values, hints := []string{"bare", "solo", "team", "full"}, []string{"sessions", "one role", "more roles", "everything"}
	for keys, want := range map[string]string{"\r": "solo", "j\r": "team", "jjj\r": "bare", "k\r": "bare", "kk\r": "full", "\x1b[B\x1b[B\r": "full", "x\x1b[A\r": "bare"} {
		p, out, _ := typed(keys, false, false)
		v, err := p.Choose("the preset", values, hints, "solo", "")
		if err != nil || v != want || !strings.HasSuffix(plain(out.String()), "✓ the preset: "+want+"\n") {
			t.Fatalf("%q gives %q %v %q", keys, v, err, plain(out.String()))
		}
	}
	p, out, _ := typed("\r", false, false)
	if _, err := p.Choose("the tracker", []string{"linear", "github"}, []string{"Linear issues", "GitHub Issues"}, "github", `"linear2" is not one of linear or github`); err != nil {
		t.Fatal(err)
	}
	if want := "? the tracker\n  \"linear2\" is not one of linear or github\n  linear  Linear issues\n> github  GitHub Issues"; !strings.Contains(plain(out.String()), want) {
		t.Fatalf("%q", plain(out.String()))
	}
}

func TestConfirmIsYesOnEnterOrYAndNoOnN(t *testing.T) {
	for keys, want := range map[string]bool{"\r": true, "y": true, "Y": true, "xn": false, "N": false} {
		p, out, _ := typed(keys, false, false)
		yes, err := p.Confirm("Continue?")
		answer := map[bool]string{true: "yes", false: "no"}[want]
		if err != nil || yes != want || !strings.HasSuffix(plain(out.String()), "✓ Continue? "+answer+"\n") {
			t.Fatalf("%q %v %v %q", keys, yes, err, plain(out.String()))
		}
	}
}

func TestWithoutKeysOneAtATimeEachQuestionTakesALine(t *testing.T) {
	p, out, _ := typed("zz\n3\n\nfoo\nbar\nmaybe\nno\n", false, true)
	v, err := p.Choose("the preset", []string{"bare", "solo", "team"}, []string{"sessions", "one role", "more roles"}, "solo", "")
	if err != nil || v != "team" {
		t.Fatal(v, err)
	}
	check := func(v string) error {
		if v == "foo" {
			return errors.New("not foo")
		}
		return nil
	}
	if v, err := p.Text("the name", "demo", "", "", check); err != nil || v != "demo" {
		t.Fatal(v, err)
	}
	if v, err := p.Text("the title", "", "", "", check); err != nil || v != "bar" {
		t.Fatal(v, err)
	}
	if yes, err := p.Confirm("Continue?"); err != nil || yes {
		t.Fatal(yes, err)
	}
	want := "  1  bare  sessions\n  2  solo  one role\n  3  team  more roles\nthe preset (solo): " +
		"  \"zz\" is none of these, type a number from 1 to 3 or a name\nthe preset (solo): " +
		"the name (demo): the title: " + "  not foo\nthe title: " + "Continue? (Y/n) Continue? (Y/n) "
	if out.String() != want {
		t.Fatalf("%q", out.String())
	}
}

func TestStylesAreLeftOutWithoutColorAndTheCursorStillMoves(t *testing.T) {
	p, out, _ := typed("ab\x1b[D\r", false, false)
	if _, err := p.Text("q", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "m") && regexp.MustCompile(`\x1b\[[0-9;]*m`).MatchString(out.String()) {
		t.Fatalf("%q", out.String())
	}
	if !strings.Contains(out.String(), "\x1b[5C") {
		t.Fatalf("%q", out.String())
	}
	p, out, _ = typed("\r", true, false)
	if _, err := p.Text("q", "d", "", "", nil); err != nil || !strings.Contains(out.String(), "\x1b[36m?\x1b[0m q \x1b[2m(d)\x1b[0m: ") || !strings.Contains(out.String(), "\x1b[32m✓\x1b[0m q: \x1b[36md\x1b[0m") {
		t.Fatalf("%q", out.String())
	}
}

func TestARedrawGoesBackOverEveryRowALongLineWrapsInto(t *testing.T) {
	out := &strings.Builder{}
	p := &Prompter{out: out, width: func() int { return 10 }}
	p.draw([]string{strings.Repeat("x", 25), "short"}, -1, 0)
	if p.rows != 4 || p.at != 3 {
		t.Fatal(p.rows, p.at)
	}
	out.Reset()
	p.draw([]string{strings.Repeat("y", 12)}, 0, 12)
	if !strings.HasPrefix(out.String(), hideCursor+"\x1b[3A\r\x1b[J") || p.rows != 2 || p.at != 1 || !strings.HasSuffix(out.String(), "\r\x1b[2C"+showCursor) {
		t.Fatalf("%q %d %d", out.String(), p.rows, p.at)
	}
	out.Reset()
	p.draw([]string{strings.Repeat("z", 10), "next"}, 0, 10)
	if !strings.HasPrefix(out.String(), hideCursor+"\x1b[1A\r\x1b[J") || p.at != 0 || !strings.HasSuffix(out.String(), "\x1b[1A\r\x1b[9C"+showCursor) {
		t.Fatalf("%q %d", out.String(), p.at)
	}
	if visible("\x1b[36m? \x1b[0mé") != 3 {
		t.Fatal(visible("\x1b[36m? \x1b[0mé"))
	}
}

func TestRestoreLeavesTheTerminalAsItWasBelowTheQuestion(t *testing.T) {
	p, out, state := typed("", false, false)
	if !p.keys() {
		t.Fatal("no keys")
	}
	p.draw([]string{"? q", "  a", "> b"}, 0, 4)
	out.Reset()
	p.Restore()
	if state.restored != 1 || out.String() != "\x1b[2B"+showCursor+"\r\n" {
		t.Fatalf("%v %q", state, out.String())
	}
	out.Reset()
	p.Restore()
	if state.restored != 1 || out.String() != "" {
		t.Fatal("restored twice")
	}
}
