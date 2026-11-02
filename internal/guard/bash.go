package guard

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

const (
	killReason    = "Never end a process by its name. Several projects share this machine, so a process ended by name is every project's. End your own process by its id, its path or its port."
	encodedReason = "An encoded PowerShell command cannot be checked for a process ended by its name. Pass it as plain text."
)

type dialect int

const (
	bashDialect dialect = iota
	powershellDialect
)

type token struct {
	text   string
	quoted bool
}

type statement struct {
	pipeline [][]token
	raw      string
}

type segment struct {
	cmd          string
	args         []string
	argTokens    []token
	text         string
	unquotedText string
}

func wordSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

var (
	powershells  = wordSet("pwsh", "powershell")
	interpreters = wordSet("bash", "sh", "zsh", "dash", "ksh", "pwsh", "powershell", "cmd")
	evaluators   = wordSet("eval", "iex", "invoke-expression")

	foreachFlags      = []string{"-begin", "-process", "-end", "-membername", "-argumentlist", "-remainingscripts", "-throttlelimit", "-timeoutseconds"}
	startProcessFlags = []string{"-filepath", "-argumentlist", "-workingdirectory", "-windowstyle", "-verb", "-redirectstandardoutput", "-redirectstandarderror", "-redirectstandardinput", "-credential"}

	// the commands that run one of their arguments as the command, with the flags of theirs that take a value
	wrapperValueFlags = map[string][]string{
		"sudo":           {"-u", "-g", "-h", "-p", "-r", "-t", "-c", "-d", "--user", "--group", "--host", "--prompt", "--role", "--type", "--chdir", "--close-from", "--chroot", "--other-user"},
		"doas":           {"-u", "-c"},
		"exec":           {"-a"},
		"time":           {"-f", "-o", "--format", "--output"},
		"nohup":          {},
		"command":        {},
		"builtin":        {},
		"env":            {"-u", "-c", "-s", "--unset", "--chdir", "--split-string"},
		"timeout":        {"-s", "-k", "--signal", "--kill-after"},
		"nice":           {"-n", "--adjustment"},
		"ionice":         {"-c", "-n", "-p", "-u"},
		"watch":          {"-n", "-d", "--interval", "--differences"},
		"xargs":          {"-i", "-l", "-n", "-p", "-s", "-a", "-d", "-e", "--max-args", "--max-procs", "--delimiter", "--arg-file", "--eof", "--replace", "--max-lines", "--max-chars"},
		"foreach-object": foreachFlags,
		"%":              foreachFlags,
		"start-process":  startProcessFlags,
		"saps":           startProcessFlags,
		"start":          startProcessFlags,
	}
	// bash xargs flags are case sensitive, the table above is lowercase for the powershell wrappers
	xargsValueFlags = wordSet("-I", "-L", "-n", "-P", "-s", "-a", "-d", "-E", "-i", "-l")
	timedWrappers   = wordSet("timeout", "nice", "watch")
	blockKeywords   = wordSet("if", "then", "else", "elif", "elseif", "fi", "do", "done", "while", "until", "for", "foreach", "try", "catch", "finally", "!", "&")
	listers         = wordSet("get-process", "gps", "ps", "pgrep", "pidof")
	cimListers      = wordSet("get-ciminstance", "gcim", "get-wmiobject", "gwmi")
	killers         = wordSet("stop-process", "spps", "kill", "taskkill", "pkill", "killall")
)

// every unicode space splits words, not only a shell's, so an odd space cannot hide a word
const jsSpaceClass = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

var (
	heredocRe          = regexp.MustCompile(`^<<(-?)` + jsSpaceClass + `*(?:'([^']+)'|"([^"]+)"|([A-Za-z_][A-Za-z0-9_]*))`)
	shellHeredocRe     = regexp.MustCompile(`(?:^|[;&|(]` + jsSpaceClass + `*)(?:bash|sh|zsh|dash|ksh)\b`)
	slashFlagRe        = regexp.MustCompile(`(?i)^/[a-z]+$`)
	assignmentRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	keywordRe          = regexp.MustCompile(`(?i)^[({!&]*[a-z!&]+$`)
	shortFlagRe        = regexp.MustCompile(`^-[A-Za-z]`)
	durationRe         = regexp.MustCompile(`^\d+(\.\d+)?[smhd]?$`)
	idParamRe          = regexp.MustCompile(`(?i)^-id(:|$)`)
	nameParamRe        = regexp.MustCompile(`(?i)^-(n|na|nam|name|p|pr|pro|proc|processname)(:|$)`)
	inputObjectParamRe = regexp.MustCompile(`(?i)^-(i|in|inp|inputobject)(:|$)`)
	psValueParamRe     = regexp.MustCompile(`(?i)^-(erroraction|ea|warningaction|wa|informationaction|ia|errorvariable|ev|warningvariable|wv|informationvariable|iv|outvariable|ov|outbuffer|ob|pipelinevariable|pv|progressaction|proga)(:|$)`)
	digitsRe           = regexp.MustCompile(`^\d+$`)
	methodKillRe       = regexp.MustCompile(`(?i)\.kill\(|\bterminate\b`)
	win32ProcessRe     = regexp.MustCompile(`(?i)win32_process`)
	signalRe           = regexp.MustCompile(`(?i)^(SIG)?(HUP|INT|QUIT|ILL|TRAP|ABRT|IOT|BUS|FPE|KILL|USR1|SEGV|USR2|PIPE|ALRM|TERM|STKFLT|CHLD|CONT|STOP|TSTP|TTIN|TTOU|URG|XCPU|XFSZ|VTALRM|PROF|WINCH|IO|POLL|PWR|SYS|RTMIN|RTMAX)$`)
	taskkillByNameRe   = regexp.MustCompile(`(?i)^[\\/-]{1,2}(im|fi)$`)
	wmicProcessRe      = regexp.MustCompile(`(?i)\bprocess\b`)
	wmicEndRe          = regexp.MustCompile(`(?i)\b(delete|terminate)\b`)
	killNameRe         = regexp.MustCompile(`^[A-Za-z][\w.-]*$`)
	listerWordRe       = regexp.MustCompile(`\b(pgrep|pidof)\b`)
	killWordRe         = regexp.MustCompile(`\b(kill|stop-process|spps|taskkill)\b`)
	encodedFlagRe      = regexp.MustCompile(`^-(encodedcommand|enc|ec|e)$`)
	shellValueFlagRe   = regexp.MustCompile(`^-(executionpolicy|ex|workingdirectory|wd|inputformat|outputformat|version|v|psconsolefile|configurationname|file|f)$`)
)

func Bash(input []byte) string {
	var in, toolInput map[string]json.RawMessage
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	json.Unmarshal(in["tool_input"], &toolInput)
	var command, toolName string
	if json.Unmarshal(toolInput["command"], &command) != nil || command == "" {
		return ""
	}
	json.Unmarshal(in["tool_name"], &toolName)
	d := bashDialect
	if toolName == "PowerShell" {
		d = powershellDialect
	}
	if reason := check(command, d, 0); reason != "" {
		return deny(reason)
	}
	return ""
}

func check(source string, d dialect, depth int) string {
	if depth > 8 || strings.TrimFunc(source, isJSSpace) == "" {
		return ""
	}
	text := source
	if d != powershellDialect {
		var shellBodies []string
		text, shellBodies = stripHeredocs(source)
		for _, body := range shellBodies {
			if reason := check(body, bashDialect, depth+1); reason != "" {
				return reason
			}
		}
	}
	statements := parse(text, d)
	if d != powershellDialect && listsIntoKillingLoop(statements, d, depth) {
		return killReason
	}
	for _, st := range statements {
		if reason := checkKillRules(st, d, depth); reason != "" {
			return reason
		}
		for _, body := range nestedBodies(st.raw, d) {
			if reason := check(body, d, depth+1); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// heredoc bodies are data and leave the text, except those a shell reads as commands
type context struct {
	kind  byte
	depth int
}

// a heredoc opens only where bash reads code, so the scan keeps the quotes and substitutions
// open at each point, across lines. a here string is never a heredoc
func findHeredoc(line string, stack *[]context) []int {
	var found []int
	top := func() *context { return &(*stack)[len(*stack)-1] }
	for i := 0; i < len(line); i++ {
		c := line[i]
		cur := top()
		switch cur.kind {
		case '\'':
			if c == '\'' {
				*stack = (*stack)[:len(*stack)-1]
			}
			continue
		case '"':
			switch {
			case c == '\\':
				i++
			case c == '"':
				*stack = (*stack)[:len(*stack)-1]
			case c == '$' && i+1 < len(line) && line[i+1] == '(':
				*stack = append(*stack, context{kind: '('})
				i++
			case c == '`':
				*stack = append(*stack, context{kind: '`'})
			}
			continue
		}
		switch {
		case c == '\\':
			i++
		case c == '\'' || c == '"':
			*stack = append(*stack, context{kind: c})
		case c == '`' && cur.kind == '`':
			*stack = (*stack)[:len(*stack)-1]
		case c == '`':
			*stack = append(*stack, context{kind: '`'})
		case c == '$' && i+1 < len(line) && line[i+1] == '(':
			*stack = append(*stack, context{kind: '('})
			i++
		case c == '(' && cur.kind == '(':
			cur.depth++
		case c == ')' && cur.kind == '(':
			if cur.depth == 0 {
				*stack = (*stack)[:len(*stack)-1]
			} else {
				cur.depth--
			}
		case c == '#' && (i == 0 || strings.IndexByte(" \t;&|(", line[i-1]) >= 0):
			return found
		case c == '<' && strings.HasPrefix(line[i:], "<<<"):
			i += 2
		case c == '<' && found == nil && strings.HasPrefix(line[i:], "<<"):
			if m := heredocRe.FindStringSubmatchIndex(line[i:]); m != nil {
				for k := range m {
					if m[k] >= 0 {
						m[k] += i
					}
				}
				found = m
				i = m[1] - 1
			} else {
				i++
			}
		}
	}
	return found
}

func stripHeredocs(source string) (string, []string) {
	lines := strings.Split(source, "\n")
	var kept, shellBodies []string
	stack := []context{{kind: 'c'}}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		m := findHeredoc(line, &stack)
		kept = append(kept, line)
		if m == nil {
			continue
		}
		var delimiter string
		for g := 2; g <= 4; g++ {
			if m[2*g] >= 0 {
				delimiter = line[m[2*g]:m[2*g+1]]
				break
			}
		}
		stripTabs := line[m[2]:m[3]] == "-"
		var body []string
		j := i + 1
		for ; j < len(lines); j++ {
			candidate := lines[j]
			if stripTabs {
				candidate = strings.TrimLeft(candidate, "\t")
			}
			if candidate == delimiter {
				break
			}
			body = append(body, lines[j])
		}
		if shellHeredocRe.MatchString(line[:m[0]]) {
			shellBodies = append(shellBodies, strings.Join(body, "\n"))
		}
		i = j
	}
	return strings.Join(kept, "\n"), shellBodies
}

func escapeRune(d dialect) rune {
	if d == powershellDialect {
		return '`'
	}
	return '\\'
}

func parse(source string, d dialect) []statement {
	src := []rune(source)
	ps := d == powershellDialect
	escape := escapeRune(d)
	var statements []statement
	var pipeline [][]token
	var tokens []token
	var buffer []rune
	var quote rune
	quoted := false
	start := 0

	endToken := func() {
		if len(buffer) > 0 || quoted {
			tokens = append(tokens, token{string(buffer), quoted})
		}
		buffer = buffer[:0]
		quoted = false
	}
	endSegment := func() {
		endToken()
		if len(tokens) > 0 {
			pipeline = append(pipeline, tokens)
		}
		tokens = nil
	}
	endStatement := func(end int) {
		endSegment()
		if len(pipeline) > 0 {
			statements = append(statements, statement{pipeline, string(src[start:end])})
		}
		pipeline = nil
	}

	for i := 0; i < len(src); i++ {
		ch := src[i]
		hasNext := i+1 < len(src)
		var next rune
		if hasNext {
			next = src[i+1]
		}
		if quote != 0 {
			if ch == escape && quote == '"' && hasNext && (ps || strings.ContainsRune("$`\"\\\n", next)) {
				buffer = append(buffer, next)
				i++
			} else if ch == quote {
				quote = 0
			} else {
				buffer = append(buffer, ch)
			}
			continue
		}
		switch {
		case ch == '\'' || ch == '"':
			quote = ch
			quoted = true
		case ch == escape && hasNext:
			if next != '\n' {
				buffer = append(buffer, next)
			}
			i++
		case (ch == '|' || ch == '&') && hasNext && next == ch:
			endStatement(i)
			i++
			start = i + 1
		case ch == '|':
			endSegment()
		case ch == ';' || ch == '\n' || (ch == '&' && !ps):
			endStatement(i)
			start = i + 1
		case isJSSpace(ch):
			endToken()
		default:
			buffer = append(buffer, ch)
		}
	}
	endStatement(len(src))
	return statements
}

// the bodies of $(...), (...) and bash backticks, where inside double quotes plain parentheses are data
func nestedBodies(raw string, d dialect) []string {
	r := []rune(raw)
	ps := d == powershellDialect
	escape := escapeRune(d)
	var bodies []string
	var quote rune
	for i := 0; i < len(r); i++ {
		ch := r[i]
		if quote == '\'' {
			if ch == '\'' {
				quote = 0
			}
			continue
		}
		if ch == escape {
			i++
			continue
		}
		if ch == '"' {
			if quote == '"' {
				quote = 0
			} else {
				quote = '"'
			}
			continue
		}
		if ch == '\'' && quote == 0 {
			quote = '\''
			continue
		}
		dollarParen := ch == '$' && i+1 < len(r) && r[i+1] == '('
		plainParen := ch == '(' && quote == 0
		if dollarParen || plainParen {
			open := i
			if dollarParen {
				open = i + 1
			}
			depth := 0
			var innerQuote rune
			for j := open; j < len(r); j++ {
				c := r[j]
				if innerQuote != 0 {
					if c == innerQuote {
						innerQuote = 0
					}
					continue
				}
				if c == escape {
					j++
					continue
				}
				if c == '\'' || c == '"' {
					innerQuote = c
					continue
				}
				if c == '(' {
					depth++
				}
				if c == ')' {
					depth--
					if depth == 0 {
						bodies = append(bodies, string(r[open+1:j]))
						i = j
						break
					}
				}
			}
		} else if ch == '`' && !ps {
			if end := slices.Index(r[i+1:], '`'); end >= 0 {
				bodies = append(bodies, string(r[i+1:i+1+end]))
				i += 1 + end
			}
		}
	}
	return bodies
}

func basename(word string) string {
	name := strings.TrimRight(strings.TrimLeft(word, "({!&"), ")}")
	if k := strings.LastIndexAny(name, `\/`); k >= 0 {
		name = name[k+1:]
	}
	name = strings.ToLower(name)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		if strings.HasSuffix(name, ext) {
			return strings.TrimSuffix(name, ext)
		}
	}
	return name
}

func isFlag(word string) bool {
	return strings.HasPrefix(word, "-") || slashFlagRe.MatchString(word)
}

func splitBlocks(tokens []token) [][]token {
	var parts [][]token
	var current []token
	for _, t := range tokens {
		switch {
		case !t.quoted && (t.text == "{" || t.text == "}" || t.text == "};"):
			if len(current) > 0 {
				parts = append(parts, current)
			}
			current = nil
		case !t.quoted && len(t.text) > 1 && strings.HasPrefix(t.text, "{"):
			if len(current) > 0 {
				parts = append(parts, current)
			}
			current = []token{{t.text[1:], false}}
		default:
			current = append(current, t)
		}
	}
	if len(current) > 0 {
		parts = append(parts, current)
	}
	return parts
}

func takesValue(wrapper, flag string, d dialect) bool {
	if wrapper == "xargs" && d != powershellDialect {
		return xargsValueFlags[flag]
	}
	return slices.Contains(wrapperValueFlags[wrapper], strings.ToLower(flag))
}

// the command word and its arguments once assignments, block keywords and wrappers are skipped
func inspect(tokens []token, d dialect) (segment, bool) {
	i := 0
	for steps := 0; steps < 16 && i < len(tokens); steps++ {
		t := tokens[i]
		name := basename(t.text)
		if !t.quoted && (assignmentRe.MatchString(t.text) || blockKeywords[t.text] || (blockKeywords[name] && keywordRe.MatchString(t.text))) {
			i++
			continue
		}
		if _, ok := wrapperValueFlags[name]; ok {
			i = skipWrapperFlags(tokens, i+1, name, d)
			continue
		}
		break
	}
	if i >= len(tokens) {
		return segment{}, false
	}
	s := segment{cmd: basename(tokens[i].text), argTokens: tokens[i+1:]}
	var all, unquoted []string
	for _, t := range tokens {
		all = append(all, t.text)
		if !t.quoted {
			unquoted = append(unquoted, t.text)
		}
	}
	for _, t := range s.argTokens {
		s.args = append(s.args, t.text)
	}
	s.text = strings.Join(all, " ")
	s.unquotedText = strings.Join(unquoted, " ")
	return s, true
}

func skipWrapperFlags(tokens []token, i int, wrapper string, d dialect) int {
	for i < len(tokens) {
		t := tokens[i]
		if t.quoted {
			break
		}
		if isFlag(t.text) {
			flag, _, hasValue := strings.Cut(t.text, "=")
			glued := len(flag) > 2 && shortFlagRe.MatchString(flag) && !strings.HasPrefix(flag, "--") && takesValue(wrapper, flag[:2], d)
			if !hasValue && !glued && takesValue(wrapper, flag, d) {
				i += 2
			} else {
				i++
			}
		} else if timedWrappers[wrapper] && durationRe.MatchString(t.text) {
			i++
		} else {
			break
		}
	}
	return i
}

func segments(st statement, d dialect) []segment {
	var segs []segment
	for _, tokens := range st.pipeline {
		for _, part := range splitBlocks(tokens) {
			if s, ok := inspect(part, d); ok {
				segs = append(segs, s)
			}
		}
	}
	return segs
}

func isIDParameter(arg string) bool          { return idParamRe.MatchString(arg) }
func isNameParameter(arg string) bool        { return nameParamRe.MatchString(arg) }
func isInputObjectParameter(arg string) bool { return inputObjectParamRe.MatchString(arg) }

func isIDValue(arg string) bool {
	return digitsRe.MatchString(arg) || strings.HasPrefix(arg, "$") || arg == "{}"
}

func isKiller(s segment) bool {
	return killers[s.cmd] || methodKillRe.MatchString(s.text)
}

func isByNameLister(s segment) bool {
	return (listers[s.cmd] && !slices.ContainsFunc(s.args, isIDParameter)) ||
		(cimListers[s.cmd] && win32ProcessRe.MatchString(s.text))
}

func containsByNameLister(text string, d dialect, depth int) bool {
	if depth > 6 {
		return false
	}
	for _, st := range parse(text, d) {
		if slices.ContainsFunc(segments(st, d), isByNameLister) {
			return true
		}
		for _, body := range nestedBodies(st.raw, d) {
			if containsByNameLister(body, d, depth+1) {
				return true
			}
		}
	}
	return false
}

var (
	loopOpeners  = wordSet("for", "while", "until", "select")
	leadKeywords = wordSet("do", "then", "else", "elif", "!", "{")
)

// the first word of each command in the statement, past a keyword that leads it
func commandWords(st statement) []string {
	var words []string
	for _, tokens := range st.pipeline {
		for i := 0; i < len(tokens); i++ {
			if tokens[i].quoted {
				break
			}
			if !leadKeywords[tokens[i].text] {
				words = append(words, tokens[i].text)
				break
			}
		}
	}
	return words
}

func count(words []string, set map[string]bool) int {
	n := 0
	for _, w := range words {
		if set[w] {
			n++
		}
	}
	return n
}

// a loop runs across statements, so a lister that feeds it and a kill in its body meet in no one statement
func listsIntoKillingLoop(statements []statement, d dialect, depth int) bool {
	lists := func(st statement) bool {
		if slices.ContainsFunc(segments(st, d), isByNameLister) {
			return true
		}
		return slices.ContainsFunc(nestedBodies(st.raw, d), func(body string) bool { return containsByNameLister(body, d, depth) })
	}
	done := wordSet("done")
	for i, header := range statements {
		if count(commandWords(header), loopOpeners) == 0 {
			continue
		}
		level, end := 0, len(statements)-1
		for j := i; j < len(statements); j++ {
			words := commandWords(statements[j])
			level += count(words, loopOpeners) - count(words, done)
			if level <= 0 {
				end = j
				break
			}
		}
		if !lists(header) && !lists(statements[end]) {
			continue
		}
		for _, st := range statements[i+1 : end+1] {
			if slices.ContainsFunc(segments(st, d), isKiller) {
				return true
			}
		}
	}
	return false
}

func positionalArgs(args []string) []string {
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if psValueParamRe.MatchString(a) && !strings.Contains(a, ":") {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		positional = append(positional, a)
	}
	return positional
}

func checkKillRules(st statement, d dialect, depth int) string {
	segs := segments(st, d)
	for k, s := range segs {
		if s.cmd == "pkill" || s.cmd == "killall" {
			return killReason
		}
		if s.cmd == "taskkill" && slices.ContainsFunc(s.args, taskkillByNameRe.MatchString) {
			return killReason
		}
		if s.cmd == "wmic" && wmicProcessRe.MatchString(s.text) && wmicEndRe.MatchString(s.text) {
			return killReason
		}
		if s.cmd == "stop-process" || s.cmd == "spps" {
			if slices.ContainsFunc(s.args, isNameParameter) {
				return killReason
			}
			byID := slices.ContainsFunc(s.args, isIDParameter)
			inputObjectAt := slices.IndexFunc(s.args, isInputObjectParameter)
			variable := ""
			if inputObjectAt >= 0 && inputObjectAt+1 < len(s.args) {
				variable = s.args[inputObjectAt+1]
			}
			byVariable := inputObjectAt >= 0 && strings.HasPrefix(variable, "$")
			if inputObjectAt >= 0 && !byVariable {
				return killReason
			}
			if !byVariable {
				variable = ""
			}
			var positional []string
			for _, a := range positionalArgs(s.args) {
				if a != variable {
					positional = append(positional, a)
				}
			}
			positionalByID := len(positional) > 0 && !slices.ContainsFunc(positional, func(a string) bool { return !isIDValue(a) })
			pipedIn := k > 0
			if !byID && !byVariable && !positionalByID && (len(positional) > 0 || !pipedIn) {
				return killReason
			}
		}
		if s.cmd == "kill" {
			if d == powershellDialect && slices.ContainsFunc(s.args, func(a string) bool { return isNameParameter(a) || isInputObjectParameter(a) }) {
				return killReason
			}
			for _, a := range positionalArgs(s.args) {
				if killNameRe.MatchString(a) && !signalRe.MatchString(a) {
					return killReason
				}
			}
		}
		if isByNameLister(s) && methodKillRe.MatchString(s.text) {
			return killReason
		}
		lower := strings.ToLower(s.unquotedText)
		if listerWordRe.MatchString(lower) && killWordRe.MatchString(lower) {
			return killReason
		}
	}
	for k, s := range segs {
		if isByNameLister(s) && slices.ContainsFunc(segs[k+1:], isKiller) {
			return killReason
		}
	}
	bodies := nestedBodies(st.raw, d)
	if slices.ContainsFunc(segs, isKiller) && slices.ContainsFunc(bodies, func(body string) bool { return containsByNameLister(body, d, depth) }) {
		return killReason
	}
	for k, s := range segs {
		if evaluators[s.cmd] {
			if reason := check(strings.Join(s.args, " "), d, depth+1); reason != "" {
				return reason
			}
			continue
		}
		if !interpreters[s.cmd] {
			continue
		}
		inner := bashDialect
		if powershells[s.cmd] {
			inner = powershellDialect
		}
		a := s.argTokens
		j := 0
		for j < len(a) && !a[j].quoted && isFlag(a[j].text) {
			flag := strings.ToLower(a[j].text)
			if powershells[s.cmd] && encodedFlagRe.MatchString(flag) {
				return encodedReason
			}
			if shellValueFlagRe.MatchString(flag) {
				j += 2
			} else {
				j++
			}
		}
		var rest []token
		if j < len(a) {
			for _, t := range a[j:] {
				if t.text != "<<<" {
					rest = append(rest, t)
				}
			}
		}
		var unquoted []string
		for _, t := range rest {
			if t.quoted {
				if reason := check(t.text, inner, depth+1); reason != "" {
					return reason
				}
			} else {
				unquoted = append(unquoted, t.text)
			}
		}
		if text := strings.Join(unquoted, " "); text != "" {
			if reason := check(text, inner, depth+1); reason != "" {
				return reason
			}
		}
		if len(rest) == 0 && k > 0 {
			if reason := check(strings.Join(segs[k-1].args, " "), inner, depth+1); reason != "" {
				return reason
			}
		}
	}
	return ""
}
