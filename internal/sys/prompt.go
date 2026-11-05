package sys

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// what a prompt returns when ctrl c or the end of the input stops it
var ErrStopped = errors.New("stopped")

// a check's error that would come back whatever is typed, so the prompt ends with it
type final interface{ Final() bool }

func isFinal(err error) bool {
	var f final
	return errors.As(err, &f) && f.Final()
}

// asks questions at a terminal, drawing each as it changes when the terminal takes keys one at a
// time, and a line at a time when it does not
type Prompter struct {
	in      *bufio.Reader
	out     io.Writer
	color   bool
	makeRaw func() (func() error, error)
	width   func() int

	mu      sync.Mutex
	restore func() error
	rows    int
	at      int
	asking  bool
}

func NewPrompter(in, out *os.File) *Prompter {
	term := os.Getenv("TERM")
	shown := IsTerminal(out.Fd())
	p := &Prompter{in: bufio.NewReader(in), out: out, color: shown && os.Getenv("NO_COLOR") == "" && term != "dumb"}
	if shown && IsTerminal(in.Fd()) && term != "dumb" {
		inFd, outFd := in.Fd(), out.Fd()
		p.makeRaw = func() (func() error, error) { return MakeRaw(inFd) }
		p.width = func() int { return Width(outFd) }
	}
	return p
}

const (
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
	dim        = "2"
	cyan       = "36"
	green      = "32"
	yellow     = "33"
)

func (p *Prompter) style(code, s string) string {
	if !p.color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// keys one at a time from here to the next cooked, or false when the terminal cannot
func (p *Prompter) keys() bool {
	if p.makeRaw == nil {
		return false
	}
	restore, err := p.makeRaw()
	if err != nil {
		p.makeRaw = nil
		return false
	}
	p.mu.Lock()
	p.restore, p.rows, p.at, p.asking = restore, 0, 0, true
	p.mu.Unlock()
	return true
}

func (p *Prompter) cooked() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.restore != nil {
		p.restore()
		p.restore = nil
	}
	p.asking = false
}

// puts the terminal back as it was and moves below the question, for a signal that ends mingo
// while it asks
func (p *Prompter) Restore() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.asking {
		return
	}
	if p.restore != nil {
		if down := p.rows - 1 - p.at; down > 0 {
			fmt.Fprintf(p.out, "\x1b[%dB", down)
		}
		io.WriteString(p.out, showCursor)
		p.restore()
		p.restore = nil
	}
	io.WriteString(p.out, "\r\n")
	p.asking = false
}

func (p *Prompter) cols() int {
	if p.width != nil {
		if w := p.width(); w > 0 {
			return w
		}
	}
	return 80
}

// the columns s takes on the screen, leaving out its styles
func visible(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j + 1
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		n++
	}
	return n
}

func rowsOf(width, cols int) int {
	if width == 0 {
		return 1
	}
	return (width + cols - 1) / cols
}

// draws lines over the ones drawn before, with the cursor at column col of line, or hidden when
// line is below 0
func (p *Prompter) draw(lines []string, line, col int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w := p.cols()
	var b strings.Builder
	b.WriteString(hideCursor)
	if p.at > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", p.at)
	}
	b.WriteString("\r\x1b[J")
	starts := make([]int, len(lines))
	rows := 0
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(l)
		starts[i] = rows
		rows += rowsOf(visible(l), w)
	}
	last := rows - 1
	p.at = last
	if line >= 0 {
		n := rowsOf(visible(lines[line]), w)
		row := starts[line] + min(col/w, n-1)
		col = min(col-(row-starts[line])*w, w-1)
		if up := last - row; up > 0 {
			fmt.Fprintf(&b, "\x1b[%dA", up)
		}
		b.WriteString("\r")
		if col > 0 {
			fmt.Fprintf(&b, "\x1b[%dC", col)
		}
		b.WriteString(showCursor)
		p.at = row
	}
	p.rows = rows
	io.WriteString(p.out, b.String())
}

// draws the question's last frame and leaves it above what comes next
func (p *Prompter) finish(line string) {
	p.draw([]string{line}, -1, 0)
	p.mu.Lock()
	io.WriteString(p.out, "\n"+showCursor)
	p.rows, p.at = 0, 0
	p.mu.Unlock()
}

func (p *Prompter) answered(question, answer string) string {
	if answer == "" {
		answer = p.style(dim, "none")
	} else {
		answer = p.style(cyan, answer)
	}
	return p.style(green, "✓") + " " + question + ": " + answer
}

func (p *Prompter) asked(question string) string {
	return p.style(cyan, "?") + " " + question
}

type keyKind int

const (
	keyNone keyKind = iota
	keyRune
	keyEnter
	keyStop
	keyBack
	keyDelete
	keyLeft
	keyRight
	keyUp
	keyDown
	keyHome
	keyEnd
	keyKillLine
	keyKillWord
)

type key struct {
	kind keyKind
	r    rune
}

func (p *Prompter) readKey() (key, error) {
	r, _, err := p.in.ReadRune()
	if err != nil {
		return key{}, err
	}
	switch r {
	case 3:
		return key{kind: keyStop}, nil
	case '\r', '\n':
		return key{kind: keyEnter}, nil
	case 0x7f, 8:
		return key{kind: keyBack}, nil
	case 1:
		return key{kind: keyHome}, nil
	case 5:
		return key{kind: keyEnd}, nil
	case 2:
		return key{kind: keyLeft}, nil
	case 6:
		return key{kind: keyRight}, nil
	case 16:
		return key{kind: keyUp}, nil
	case 14:
		return key{kind: keyDown}, nil
	case 21:
		return key{kind: keyKillLine}, nil
	case 23:
		return key{kind: keyKillWord}, nil
	case 0x1b:
		return p.escape(), nil
	}
	if r < 0x20 {
		return key{}, nil
	}
	return key{kind: keyRune, r: r}, nil
}

// the keys a terminal sends as an escape sequence, which arrive in one read, so a lone escape is
// one with nothing after it yet
func (p *Prompter) escape() key {
	if p.in.Buffered() == 0 {
		return key{}
	}
	b, _ := p.in.ReadByte()
	if b != '[' && b != 'O' {
		return key{}
	}
	var seq []byte
	for p.in.Buffered() > 0 {
		c, _ := p.in.ReadByte()
		seq = append(seq, c)
		if c >= 0x40 && c <= 0x7e {
			break
		}
	}
	switch string(seq) {
	case "A":
		return key{kind: keyUp}
	case "B":
		return key{kind: keyDown}
	case "C":
		return key{kind: keyRight}
	case "D":
		return key{kind: keyLeft}
	case "H", "1~", "7~":
		return key{kind: keyHome}
	case "F", "4~", "8~":
		return key{kind: keyEnd}
	case "3~":
		return key{kind: keyDelete}
	}
	return key{}
}

type editor struct {
	text []rune
	at   int
}

func (e *editor) edit(k key) bool {
	switch k.kind {
	case keyRune:
		e.text = slices.Insert(e.text, e.at, k.r)
		e.at++
	case keyBack:
		if e.at > 0 {
			e.text = slices.Delete(e.text, e.at-1, e.at)
			e.at--
		}
	case keyDelete:
		if e.at < len(e.text) {
			e.text = slices.Delete(e.text, e.at, e.at+1)
		}
	case keyLeft:
		e.at = max(e.at-1, 0)
	case keyRight:
		e.at = min(e.at+1, len(e.text))
	case keyHome:
		e.at = 0
	case keyEnd:
		e.at = len(e.text)
	case keyKillLine:
		e.text = slices.Delete(e.text, 0, e.at)
		e.at = 0
	case keyKillWord:
		from := e.at
		for from > 0 && e.text[from-1] == ' ' {
			from--
		}
		for from > 0 && e.text[from-1] != ' ' {
			from--
		}
		e.text = slices.Delete(e.text, from, e.at)
		e.at = from
	default:
		return false
	}
	return true
}

// ends the question with what stopped it
func (p *Prompter) stop(question string, err error) error {
	p.finish(p.style(dim, "? "+question))
	if err == nil || errors.Is(err, io.EOF) {
		return ErrStopped
	}
	return err
}

// asks for a line of text with def taken for an empty answer, input already typed and problem shown
// under it, and on enter runs check, showing the problem it finds and asking again
func (p *Prompter) Text(question, def, input, problem string, check func(string) error) (string, error) {
	if !p.keys() {
		return p.textLines(question, def, problem, check)
	}
	defer p.cooked()
	e := editor{text: []rune(input), at: utf8.RuneCountInString(input)}
	for {
		head := p.asked(question)
		if def != "" {
			head += " " + p.style(dim, "("+def+")")
		}
		head += ": "
		lines := []string{head + string(e.text)}
		if problem != "" {
			lines = append(lines, "  "+p.style(yellow, problem))
		}
		p.draw(lines, 0, visible(head)+e.at)
		k, err := p.readKey()
		if err != nil || k.kind == keyStop {
			return "", p.stop(question, err)
		}
		if k.kind != keyEnter {
			if e.edit(k) {
				problem = ""
			}
			continue
		}
		v := strings.TrimSpace(string(e.text))
		if v == "" {
			v = def
		}
		if check != nil {
			if err := check(v); err != nil {
				if isFinal(err) {
					return "", p.stop(question, err)
				}
				problem = err.Error()
				continue
			}
		}
		p.finish(p.answered(question, v))
		return v, nil
	}
}

func (p *Prompter) readLine() (string, error) {
	p.mu.Lock()
	p.asking = true
	p.mu.Unlock()
	line, err := p.in.ReadString('\n')
	p.mu.Lock()
	p.asking = false
	p.mu.Unlock()
	if err != nil && line == "" {
		fmt.Fprintln(p.out)
		if errors.Is(err, io.EOF) {
			return "", ErrStopped
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (p *Prompter) textLines(question, def, problem string, check func(string) error) (string, error) {
	for {
		if problem != "" {
			fmt.Fprintf(p.out, "  %s\n", problem)
		}
		if def != "" {
			fmt.Fprintf(p.out, "%s (%s): ", question, def)
		} else {
			fmt.Fprintf(p.out, "%s: ", question)
		}
		v, err := p.readLine()
		if err != nil {
			return "", err
		}
		if v == "" {
			v = def
		}
		if check != nil {
			if err := check(v); err != nil {
				if isFinal(err) {
					return "", err
				}
				problem = err.Error()
				continue
			}
		}
		return v, nil
	}
}

// asks for one of values, each with its hint, def picked to begin with and problem shown under the
// question, where up and down or k and j move and enter picks
func (p *Prompter) Choose(question string, values, hints []string, def, problem string) (string, error) {
	at := max(slices.Index(values, def), 0)
	if !p.keys() {
		return p.chooseLines(question, values, hints, at, problem)
	}
	defer p.cooked()
	wide := 0
	for _, v := range values {
		wide = max(wide, utf8.RuneCountInString(v))
	}
	for {
		lines := []string{p.asked(question)}
		if problem != "" {
			lines = append(lines, "  "+p.style(yellow, problem))
		}
		for i, v := range values {
			hint := ""
			if i < len(hints) && hints[i] != "" {
				hint = "  " + p.style(dim, hints[i])
			}
			v += strings.Repeat(" ", wide-utf8.RuneCountInString(v))
			if i == at {
				lines = append(lines, p.style(cyan, "> "+v)+hint)
			} else {
				lines = append(lines, "  "+v+hint)
			}
		}
		p.draw(lines, -1, 0)
		k, err := p.readKey()
		switch {
		case err != nil || k.kind == keyStop:
			return "", p.stop(question, err)
		case k.kind == keyUp || k.kind == keyRune && k.r == 'k':
			at = (at + len(values) - 1) % len(values)
			problem = ""
		case k.kind == keyDown || k.kind == keyRune && k.r == 'j':
			at = (at + 1) % len(values)
			problem = ""
		case k.kind == keyEnter:
			p.finish(p.answered(question, values[at]))
			return values[at], nil
		}
	}
}

func (p *Prompter) chooseLines(question string, values, hints []string, at int, problem string) (string, error) {
	wide := 0
	for _, v := range values {
		wide = max(wide, utf8.RuneCountInString(v))
	}
	for i, v := range values {
		hint := ""
		if i < len(hints) {
			hint = hints[i]
		}
		fmt.Fprintln(p.out, strings.TrimRight(fmt.Sprintf("  %d  %-*s  %s", i+1, wide, v, hint), " "))
	}
	for {
		if problem != "" {
			fmt.Fprintf(p.out, "  %s\n", problem)
		}
		fmt.Fprintf(p.out, "%s (%s): ", question, values[at])
		v, err := p.readLine()
		if err != nil {
			return "", err
		}
		if v == "" {
			return values[at], nil
		}
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= len(values) {
			return values[n-1], nil
		}
		if slices.Contains(values, v) {
			return v, nil
		}
		problem = fmt.Sprintf("%q is none of these, type a number from 1 to %d or a name", v, len(values))
	}
}

// asks a yes or no question whose answer is yes unless it is no
func (p *Prompter) Confirm(question string) (bool, error) {
	if !p.keys() {
		for {
			fmt.Fprintf(p.out, "%s (Y/n) ", question)
			v, err := p.readLine()
			if err != nil {
				return false, err
			}
			switch strings.ToLower(v) {
			case "", "y", "yes":
				return true, nil
			case "n", "no":
				return false, nil
			}
		}
	}
	defer p.cooked()
	for {
		head := p.asked(question) + " " + p.style(dim, "(Y/n)") + " "
		p.draw([]string{head}, 0, visible(head))
		k, err := p.readKey()
		switch {
		case err != nil || k.kind == keyStop:
			return false, p.stop(question, err)
		case k.kind == keyEnter || k.kind == keyRune && (k.r == 'y' || k.r == 'Y'):
			p.finish(p.style(green, "✓") + " " + question + " " + p.style(cyan, "yes"))
			return true, nil
		case k.kind == keyRune && (k.r == 'n' || k.r == 'N'):
			p.finish(p.style(green, "✓") + " " + question + " " + p.style(cyan, "no"))
			return false, nil
		}
	}
}
