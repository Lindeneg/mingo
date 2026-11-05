package guard

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	killReason    = "Never end a process by its name. Several projects share this machine, so a process ended by name is every project's. End your own process by its id, its path or its port."
	encodedReason = "An encoded PowerShell command cannot be checked for a process ended by its name. Pass it as plain text."
	everyReason   = "Never end every process with kill -1. Several projects share this machine, so it ends every project's."
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
	shell        dialect
	// text a wrapper hands a shell, such as an ssh command, env -S or every way parallel may be read
	code []string
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
	// more commands bash runs one of their arguments through, with their flags that take a value, case sensitive
	bashWrapperValueFlags = map[string][]string{
		"setsid":  {},
		"stdbuf":  {"-i", "-o", "-e", "--input", "--output", "--error"},
		"chrt":    {"-T", "-P", "-D", "--sched-runtime", "--sched-period", "--sched-deadline"},
		"taskset": {},
		"busybox": {},
		"sshpass": {"-p", "-f", "-d", "-P"},
		"ssh":     {},
		"autossh": {},
		"parallel": {"-a", "-C", "-d", "-E", "-I", "-j", "-J", "-L", "-l", "-n", "-N", "-P", "-s", "-S",
			"--arg-file", "--arg-file-sep", "--arg-sep", "--basefile", "--bf", "--block", "--colsep", "--delay",
			"--delimiter", "--env", "--eof", "--halt", "--header", "--jobs", "--joblog", "--load", "--max-args",
			"--max-chars", "--max-lines", "--max-procs", "--memfree", "--nice", "--profile", "--recend",
			"--recstart", "--replace", "--results", "--retries", "--return", "--rpl", "--slf", "--sshdelay",
			"--sshlogin", "--sshloginfile", "--tagstring", "--termseq", "--tf", "--timeout", "--tmpdir",
			"--transferfile", "--trc", "--wd", "--workdir"},
	}
	// ssh takes its flags in clusters such as -tt or -p22, the letters that take a value and those that do not
	shortFlagLetters = map[string][2]string{
		"ssh":     {"BbcDEeFIiJLlmOoPpQRSWw", "46AaCfGgKkMNnqsTtVvXxYy"},
		"autossh": {"BbcDEeFIiJLlmOoPpQRSWwM", "46AaCfGgKkMNnqsTtVvXxYy"},
	}
	// the words a wrapper takes before its command, the cpu mask of taskset, the priority of chrt, the host of ssh
	wrapperPositionals = map[string]int{"chrt": 1, "taskset": 1, "ssh": 1, "autossh": 1}
	remoteShells       = wordSet("ssh", "autossh")
	parallelSeparators = wordSet(":::", ":::+", "::::", "::::+")
	// powershell calls a kill method by name through these, as in Invoke-CimMethod -MethodName Terminate
	psMethodCallers   = wordSet("invoke-cimmethod", "icim", "invoke-wmimethod", "iwmi")
	declarers         = wordSet("export", "local", "declare", "readonly", "typeset")
	sshCommandOptions = []string{"proxycommand", "localcommand", "remotecommand", "knownhostscommand"}

	timedWrappers = wordSet("timeout", "nice", "watch")
	blockKeywords = wordSet("if", "then", "else", "elif", "elseif", "fi", "do", "done", "while", "until", "for", "foreach", "try", "catch", "finally", "!", "&")
	listers       = wordSet("get-process", "gps", "ps", "pgrep", "pidof")
	cimListers    = wordSet("get-ciminstance", "gcim", "get-wmiobject", "gwmi")
	killers       = wordSet("stop-process", "spps", "kill", "taskkill", "pkill", "killall")
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
	echoFlagRe         = regexp.MustCompile(`^-[neE]+$`)
	printfFormatRe     = regexp.MustCompile(`\\[nrtv]|%[-+ #0-9.]*[a-zA-Z%]`)
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
	names := map[string]bool{}
	if d != powershellDialect {
		names = namedVariables(statements)
	}
	for _, st := range statements {
		if reason := checkKillRules(st, d, depth, names); reason != "" {
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
	var lastRedirect rune
	quoted := false
	// a redirect and the file it names are no words of the command
	redirect, redirectTarget := false, false
	piped := false
	start := 0

	endToken := func() {
		if len(buffer) > 0 || quoted {
			switch {
			case redirect:
				redirectTarget = wantsTarget(string(buffer))
			case redirectTarget:
				redirectTarget = false
			default:
				tokens = append(tokens, token{string(buffer), quoted})
			}
		}
		buffer = buffer[:0]
		quoted = false
		redirect = false
	}
	endSegment := func() {
		endToken()
		redirectTarget = false
		if len(tokens) > 0 {
			pipeline = append(pipeline, tokens)
		}
		tokens = nil
	}
	endStatement := func(end int) {
		endSegment()
		piped = false
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
		afterRedirect := lastRedirect
		lastRedirect = 0
		if quote != 0 {
			switch {
			case ch == escape && quote == '"' && hasNext && (ps || strings.ContainsRune("$`\"\\\n", next)):
				buffer = append(buffer, next)
				i++
			case ch == quote:
				quote = 0
			case !ps && quote == '"' && opensExpansion(src, i):
				end := expansionEnd(src, i)
				buffer = append(buffer, src[i:end+1]...)
				i = end
			default:
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
		case !ps && ch == '#' && len(buffer) == 0 && !quoted && (i == 0 || strings.ContainsRune(" \t\n;&|", src[i-1])):
			// a comment runs to the end of its line and starts only where bash starts a word
			for ; i < len(src) && src[i] != '\n'; i++ {
				src[i] = ' '
			}
			i--
		case !ps && opensExpansion(src, i):
			// a substitution is one word with whatever spaces, lines and quotes it holds
			end := expansionEnd(src, i)
			buffer = append(buffer, src[i:end+1]...)
			i = end
		case !ps && ch == '(' && redirect && len(buffer) == 1:
			// <( and >( are a process substitution, a word and no redirect
			redirect = false
			end := expansionEnd(src, i)
			buffer = append(buffer, src[i:end+1]...)
			i = end
		case !ps && ch == '<' && strings.HasPrefix(string(src[i:min(i+3, len(src))]), "<<<"):
			// a here string's text is read like an argument
			if len(buffer) > 0 && !redirect {
				endToken()
			}
			buffer = append(buffer, '<', '<', '<')
			i += 2
		case !ps && ((ch == '&' && afterRedirect != 0) || (ch == '|' && afterRedirect == '>')):
			// the & of 2>&1 and <&3 and the | of >| belong to the redirect
			buffer = append(buffer, ch)
		case !ps && ch == '&' && next == '>':
			// &> sends both streams to a file and starts a word of its own
			endToken()
			redirect = true
			buffer = append(buffer, ch)
		case !ps && ch == '|' && next == '&':
			// |& pipes both streams
			endSegment()
			piped = true
			i++
		case (ch == '|' || ch == '&') && hasNext && next == ch:
			endStatement(i)
			i++
			start = i + 1
		case ch == '|':
			endSegment()
			piped = !ps
		case ch == '\n' && piped && len(tokens) == 0 && len(buffer) == 0 && !quoted:
			// a pipe at the end of a line goes on to the next one
		case ch == ';' || ch == '\n' || (ch == '&' && !ps):
			endStatement(i)
			start = i + 1
		case isJSSpace(ch):
			endToken()
		default:
			if !ps && (ch == '>' || ch == '<') {
				// a redirect glued to a word is no part of it, unless the word is the number of a stream
				if !redirect {
					if len(buffer) > 0 && (quoted || !digitsRe.MatchString(string(buffer))) {
						endToken()
					}
					redirect = true
				}
				lastRedirect = ch
			}
			buffer = append(buffer, ch)
		}
	}
	endStatement(len(src))
	return statements
}

// a redirect written apart from its file, such as > out.log, takes the next word as that file
func wantsTarget(redirect string) bool {
	op := strings.TrimLeft(redirect, "0123456789")
	for _, o := range []string{"&>>", "&>", "<<-", "<<", "<>", "<&", ">&", ">>", ">|", "<", ">"} {
		if strings.HasPrefix(op, o) {
			return op == o
		}
	}
	return false
}

func opensExpansion(src []rune, i int) bool {
	return src[i] == '`' || (src[i] == '$' && i+1 < len(src) && (src[i+1] == '(' || src[i+1] == '{'))
}

// the index that closes the $( ${ ( or backtick opening at i, or the last index when nothing does
func expansionEnd(src []rune, i int) int {
	switch {
	case src[i] == '`':
		for j := i + 1; j < len(src); j++ {
			if src[j] == '\\' {
				j++
			} else if src[j] == '`' {
				return j
			}
		}
		return len(src) - 1
	case src[i] == '$' && src[i+1] == '{':
		return closingIndex(src, i+1, '{', '}')
	case src[i] == '$':
		return closingIndex(src, i+1, '(', ')')
	}
	return closingIndex(src, i, '(', ')')
}

func closingIndex(src []rune, open int, left, right rune) int {
	depth := 0
	for j := open; j < len(src); j++ {
		switch c := src[j]; {
		case c == '\\':
			j++
		case c == '\'':
			for j++; j < len(src) && src[j] != '\''; j++ {
			}
		case c == '"':
			j = doubleQuoteEnd(src, j)
		case opensExpansion(src, j):
			j = expansionEnd(src, j)
		case c == '#' && left == '(' && j > open && strings.ContainsRune(" \t\n;&|(", src[j-1]):
			for j+1 < len(src) && src[j+1] != '\n' {
				j++
			}
		case c == left:
			depth++
		case c == right:
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return len(src) - 1
}

func doubleQuoteEnd(src []rune, open int) int {
	for k := open + 1; k < len(src); k++ {
		switch c := src[k]; {
		case c == '\\':
			k++
		case c == '"':
			return k
		case opensExpansion(src, k):
			k = expansionEnd(src, k)
		}
	}
	return len(src) - 1
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

func splitBlocks(tokens []token, d dialect) [][]token {
	var parts [][]token
	var current []token
	for _, t := range tokens {
		switch {
		case !t.quoted && (t.text == "{" || t.text == "}" || t.text == "};"):
			if len(current) > 0 {
				parts = append(parts, current)
			}
			current = nil
		// in bash {} is a word, as xargs -I{} and parallel take it
		case !t.quoted && len(t.text) > 1 && strings.HasPrefix(t.text, "{") && !(d == bashDialect && t.text == "{}"):
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
	var code []string
	for steps := 0; steps < 16 && i < len(tokens); steps++ {
		t := tokens[i]
		name := basename(t.text)
		if !t.quoted && (assignmentRe.MatchString(t.text) || blockKeywords[t.text] || (blockKeywords[name] && keywordRe.MatchString(t.text))) {
			i++
			continue
		}
		if _, ok := wrapperValueFlags[name]; ok {
			if name == "command" && d == bashDialect && describesOnly(tokens[i+1:]) {
				break
			}
			from := i + 1
			i = skipWrapperFlags(tokens, from, name, d)
			if name == "env" && d == bashDialect {
				code = append(code, flagValues(tokens[from:min(i, len(tokens))], "-S", "--split-string")...)
			}
			continue
		}
		if _, ok := bashWrapperValueFlags[name]; ok && d == bashDialect {
			from := i + 1
			var unsure bool
			i, unsure = skipBashWrapper(tokens, from, name)
			if remoteShells[name] {
				code = append(code, sshOptionCommands(tokens[from:min(i, len(tokens))])...)
				if i < len(tokens) {
					code = append(code, joinTexts(tokens[i:]))
				}
			}
			// a flag the guard does not know may take the next word, so every word that could start the command is read as one
			if unsure || name == "parallel" {
				for c := from; c < min(len(tokens), from+32) && !parallelSeparators[tokens[c].text]; c++ {
					if !isFlag(tokens[c].text) {
						code = append(code, joinTexts(tokens[c:]))
					}
				}
			}
			continue
		}
		break
	}
	if i >= len(tokens) {
		return segment{code: code, shell: d}, len(code) > 0
	}
	if d == bashDialect && tokens[i].quoted && strings.ContainsAny(tokens[i].text, " \t\n;|&") {
		// a quoted command with spaces, as watch or parallel take it, is read as the shell would read it
		code = append(code, joinTexts(tokens[i:]))
	}
	s := segment{cmd: basename(tokens[i].text), argTokens: tokens[i+1:], shell: d, code: code}
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

// command -v and -V say what a name is and run nothing
func describesOnly(tokens []token) bool {
	for _, t := range tokens {
		if t.quoted || !strings.HasPrefix(t.text, "-") || t.text == "--" || t.text == "-" {
			return false
		}
		if strings.ContainsAny(t.text[1:], "vV") {
			return true
		}
	}
	return false
}

func joinTexts(tokens []token) string {
	texts := make([]string, len(tokens))
	for k, t := range tokens {
		texts[k] = t.text
	}
	return strings.Join(texts, " ")
}

// the values of a flag such as env -S, written -S value, -Svalue or --split-string=value
func flagValues(tokens []token, short, long string) []string {
	var values []string
	for k, t := range tokens {
		switch {
		case (t.text == short || t.text == long) && k+1 < len(tokens):
			values = append(values, tokens[k+1].text)
		case strings.HasPrefix(t.text, long+"="):
			values = append(values, strings.TrimPrefix(t.text, long+"="))
		case len(t.text) > len(short) && strings.HasPrefix(t.text, short) && !strings.HasPrefix(t.text, "--"):
			values = append(values, strings.TrimPrefix(t.text, short))
		}
	}
	return values
}

// ssh -o ProxyCommand and its kin run a command of their own
func sshOptionCommands(tokens []token) []string {
	var commands []string
	for _, option := range flagValues(tokens, "-o", "-o") {
		lower := strings.ToLower(option)
		for _, name := range sshCommandOptions {
			if strings.HasPrefix(lower, name) {
				commands = append(commands, strings.TrimLeft(option[len(name):], " \t="))
			}
		}
	}
	return commands
}

// the index of the command a bash wrapper runs, and whether a flag it does not know leaves that unsure
func skipBashWrapper(tokens []token, i int, wrapper string) (int, bool) {
	i, unsure := skipBashFlags(tokens, i, wrapper)
	if n := wrapperPositionals[wrapper]; n > 0 && i < len(tokens) {
		i += n
		if remoteShells[wrapper] {
			// ssh reads flags after the host too
			var more bool
			i, more = skipBashFlags(tokens, i, wrapper)
			unsure = unsure || more
		}
	}
	return i, unsure
}

func skipBashFlags(tokens []token, i int, wrapper string) (int, bool) {
	unsure := false
	values := bashWrapperValueFlags[wrapper]
	for i < len(tokens) {
		a := tokens[i].text
		if !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		i++
		if a == "--" {
			break
		}
		if letters, ok := shortFlagLetters[wrapper]; ok && !strings.HasPrefix(a, "--") {
			for k := 1; k < len(a); k++ {
				if strings.IndexByte(letters[0], a[k]) >= 0 {
					if k == len(a)-1 {
						i++
					}
					break
				}
				if strings.IndexByte(letters[1], a[k]) < 0 {
					unsure = true
				}
			}
			continue
		}
		flag, _, inline := strings.Cut(a, "=")
		glued := len(flag) > 2 && flag[1] != '-' && slices.Contains(values, flag[:2])
		if !inline && !glued && slices.Contains(values, flag) {
			i++
		}
	}
	return i, unsure
}

func skipWrapperFlags(tokens []token, i int, wrapper string, d dialect) int {
	for i < len(tokens) {
		t := tokens[i]
		// bash takes a quoted flag such as --user='root' as the flag
		if t.quoted && (d != bashDialect || !strings.HasPrefix(t.text, "-")) {
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
		for _, part := range splitBlocks(tokens, d) {
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

// in bash only the command word can call a kill method, an argument such as grep terminate is data
func isKiller(s segment) bool {
	if s.shell == bashDialect {
		return killers[s.cmd] || methodKillRe.MatchString(s.cmd) || (psMethodCallers[s.cmd] && methodKillRe.MatchString(s.text))
	}
	return killers[s.cmd] || methodKillRe.MatchString(s.text)
}

// a lister, or in bash an echo, printf or here string of a word that is no process id, since kill
// given a word ends every process of that name
func listsByName(s segment) bool {
	return isByNameLister(s) || (s.shell == bashDialect && printsAName(s))
}

func printsAName(s segment) bool {
	var words []string
	switch {
	case s.cmd == "echo":
		words = s.args
		for len(words) > 0 && echoFlagRe.MatchString(words[0]) {
			words = words[1:]
		}
	case s.cmd == "printf":
		if slices.Contains(s.args, "-v") {
			return false
		}
		words = s.args
		if len(words) > 0 && words[0] == "--" {
			words = words[1:]
		}
		if len(words) > 1 {
			words = words[1:]
		} else if len(words) == 1 {
			words = []string{printfFormatRe.ReplaceAllString(words[0], " ")}
		}
	case strings.HasPrefix(s.cmd, "<<<"):
		words = append([]string{strings.TrimPrefix(s.cmd, "<<<")}, s.args...)
	}
	for _, w := range words {
		if slices.ContainsFunc(strings.Fields(w), isNameWord) {
			return true
		}
	}
	return false
}

// a word kill reads as a name, not a number, a %job, a $variable or a signal
func isNameWord(w string) bool {
	return w != "" && !digitsRe.MatchString(w) && !strings.HasPrefix(w, "%") && !strings.HasPrefix(w, "$") && !strings.HasPrefix(w, "-") && w != "{}"
}

// the top commands of a substitution, where an echo is what feeds the kill, and every lister deeper in
func bodyListsByName(body string, d dialect, depth int) bool {
	for _, st := range parse(body, d) {
		if slices.ContainsFunc(segments(st, d), listsByName) {
			return true
		}
		for _, inner := range nestedBodies(st.raw, d) {
			if containsByNameLister(inner, d, depth+1) {
				return true
			}
		}
	}
	return false
}

func isByNameLister(s segment) bool {
	return (listers[s.cmd] && !slices.ContainsFunc(s.args, isIDParameter) && !(s.shell == bashDialect && s.cmd == "pgrep" && listsChildrenOnly(s.args))) ||
		(cimListers[s.cmd] && win32ProcessRe.MatchString(s.text))
}

var (
	pgrepValueFlags = wordSet("--delimiter", "--pgroup", "--group", "--older", "--parent", "--session", "--signal", "--terminal", "--euid", "--uid", "--pidfile", "--runstates", "--cgroup", "--ns", "--nslist")
	pgrepShortValue = "dgGOPstuUFr"
)

// pgrep -P picks the children of a process id and no name, unless a pattern narrows it by name,
// -v turns it into every other process, or the parent is 0 or 1, whose children are every service
func listsChildrenOnly(args []string) bool {
	var parents []string
	value := func(i int, glued string) string {
		if glued != "" {
			return glued
		}
		if i+1 < len(args) {
			return args[i+1]
		}
		return ""
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if i != len(args)-1 {
				return false
			}
		case strings.HasPrefix(a, "--"):
			name, inline, glued := strings.Cut(a, "=")
			if name == "--inverse" {
				return false
			}
			if name == "--parent" {
				parents = append(parents, value(i, inline))
			}
			if !glued && pgrepValueFlags[name] {
				i++
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for k := 1; k < len(a); k++ {
				if a[k] == 'v' {
					return false
				}
				if strings.IndexByte(pgrepShortValue, a[k]) >= 0 {
					if a[k] == 'P' {
						parents = append(parents, value(i, a[k+1:]))
					}
					if k == len(a)-1 {
						i++
					}
					break
				}
			}
		default:
			return false
		}
	}
	for _, p := range parents {
		for _, id := range strings.Split(p, ",") {
			if n, err := strconv.Atoi(id); id == "" || (err == nil && n <= 1) {
				return false
			}
		}
	}
	return len(parents) > 0
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
		if slices.ContainsFunc(segments(st, d), listsByName) {
			return true
		}
		return slices.ContainsFunc(nestedBodies(st.raw, d), func(body string) bool { return bodyListsByName(body, d, depth) })
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
		listed := lists(header) || lists(statements[end])
		variable, named := forNames(header)
		if !listed && !named {
			continue
		}
		for _, st := range statements[i+1 : end+1] {
			for _, s := range segments(st, d) {
				if isKiller(s) && (listed || killsVariable(s, variable)) {
					return true
				}
			}
		}
	}
	return false
}

// the variable of a for loop over words, and whether one of those words is a name and no id
func forNames(header statement) (string, bool) {
	if len(header.pipeline) == 0 {
		return "", false
	}
	tokens := header.pipeline[0]
	at := slices.IndexFunc(tokens, func(t token) bool { return !t.quoted && t.text == "for" })
	if at < 0 || at+2 >= len(tokens) || tokens[at+2].text != "in" {
		return "", false
	}
	return tokens[at+1].text, slices.ContainsFunc(tokens[at+3:], func(t token) bool { return isNameWord(t.text) })
}

func killsVariable(s segment, variable string) bool {
	return slices.ContainsFunc(s.args, func(a string) bool { return a == "$"+variable || a == "${"+variable+"}" })
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

// the first option is the signal and every word after it a process id, and the id -1 is every
// process the user may signal
func signalsEveryProcess(args []string) bool {
	i := 0
	if len(args) > 0 {
		switch a := args[0]; {
		case a == "-l" || a == "-L":
			return false
		case a == "-s" || a == "-n":
			i = 2
		case strings.HasPrefix(a, "-"):
			i = 1
		}
	}
	if i < len(args) && args[i] == "--" {
		i++
	}
	return i < len(args) && slices.Contains(args[i:], "-1")
}

// a command the guard does not know as a wrapper, such as strace kill $(pgrep node), still runs
// the kill it is given, so a kill word beside a lister in a substitution is denied
func killsWithAListerInItsArguments(s segment, depth int) bool {
	killWord := slices.ContainsFunc(s.argTokens, func(t token) bool { return !t.quoted && killers[basename(t.text)] })
	return killWord && slices.ContainsFunc(s.argTokens, func(t token) bool {
		return !t.quoted && slices.ContainsFunc(nestedBodies(t.text, bashDialect), func(body string) bool {
			return containsByNameLister(body, bashDialect, depth)
		})
	})
}

func containsKiller(text string, depth int) bool {
	if depth > 6 {
		return false
	}
	for _, st := range parse(text, bashDialect) {
		if slices.ContainsFunc(segments(st, bashDialect), isKiller) {
			return true
		}
		if slices.ContainsFunc(nestedBodies(st.raw, bashDialect), func(body string) bool { return containsKiller(body, depth+1) }) {
			return true
		}
	}
	return false
}

// the variables a command sets to a word kill would read as a name, such as n=node
func namedVariables(statements []statement) map[string]bool {
	names := map[string]bool{}
	for _, st := range statements {
		for _, tokens := range st.pipeline {
			declares := len(tokens) > 0 && declarers[tokens[0].text]
			for k, t := range tokens {
				if !assignmentRe.MatchString(t.text) || (k > 0 && !declares && !assignmentRe.MatchString(tokens[k-1].text)) {
					continue
				}
				name, value, _ := strings.Cut(t.text, "=")
				names[name] = isNameWord(value) && !strings.ContainsAny(value, "`(")
			}
		}
	}
	return names
}

func checkKillRules(st statement, d dialect, depth int, names map[string]bool) string {
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
			if d == bashDialect && signalsEveryProcess(s.args) {
				return everyReason
			}
			if d == powershellDialect && slices.ContainsFunc(s.args, func(a string) bool { return isNameParameter(a) || isInputObjectParameter(a) }) {
				return killReason
			}
			for _, a := range positionalArgs(s.args) {
				if killNameRe.MatchString(a) && !signalRe.MatchString(a) {
					return killReason
				}
				if v := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(a, "$"), "{"), "}"); strings.HasPrefix(a, "$") && names[v] {
					return killReason
				}
			}
		}
		if isByNameLister(s) && methodKillRe.MatchString(s.text) {
			return killReason
		}
		// in bash the rules above already read every command word, and a word in an argument is data
		lower := strings.ToLower(s.unquotedText)
		if d == powershellDialect && listerWordRe.MatchString(lower) && killWordRe.MatchString(lower) {
			return killReason
		}
		if d == bashDialect && killsWithAListerInItsArguments(s, depth) {
			return killReason
		}
		for _, code := range s.code {
			if reason := check(code, bashDialect, depth+1); reason != "" {
				return reason
			}
		}
	}
	for k, s := range segs {
		if listsByName(s) && slices.ContainsFunc(segs[k+1:], isKiller) {
			return killReason
		}
	}
	bodies := nestedBodies(st.raw, d)
	listed := slices.ContainsFunc(bodies, func(body string) bool { return bodyListsByName(body, d, depth) })
	if listed && slices.ContainsFunc(segs, isKiller) {
		return killReason
	}
	// tee >(xargs kill) < <(pgrep node) feeds one substitution from another
	if listed && d == bashDialect && slices.ContainsFunc(bodies, func(body string) bool { return containsKiller(body, depth) }) {
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
					if d == bashDialect {
						t.text = strings.TrimPrefix(t.text, "<<<")
					}
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
