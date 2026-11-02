package kit

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Ask struct {
	Key      string  `toml:"key"`
	Question string  `toml:"question"`
	Default  *string `toml:"default"`
	List     bool    `toml:"list"`
	Each     string  `toml:"each"`
	When     string  `toml:"when"`
}

func (a Ask) Flag() string {
	return "--" + strings.ReplaceAll(a.Key, ".", "-")
}

var Presets = []string{"bare", "solo", "team", "full"}

func (k *Kit) Preset(name string) ([]string, error) {
	switch name {
	case "bare":
		return []string{"guards"}, nil
	case "solo":
		return []string{"guards", "workflow", "ci", "review"}, nil
	case "team":
		return []string{"guards", "workflow", "ci", "review", "team", "tracker", "rules", "heavy"}, nil
	case "full":
		var all []string
		for _, m := range k.Modules {
			if !m.Always {
				all = append(all, m.Name)
			}
		}
		return all, nil
	}
	return nil, fmt.Errorf("no preset %s, the presets are %s", name, strings.Join(Presets, " "))
}

// asks a question at a terminal and says whether one was there to ask
type Prompt func(question, def string) (string, bool)

type answers struct {
	values map[string]string
	each   map[string][][2]string
}

func (a *answers) list(key string) []string {
	var out []string
	for _, item := range strings.Split(a.values[key], ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (a *answers) fill(text string, item string, index int) string {
	text = strings.ReplaceAll(text, "{item}", item)
	text = strings.ReplaceAll(text, "{name}", a.values["name"])
	text = strings.ReplaceAll(text, "{title}", a.values["title"])
	text = strings.ReplaceAll(text, "{offset}", strconv.Itoa((index+1)*10))
	if roles := a.list("roles"); len(roles) > 0 {
		text = strings.ReplaceAll(text, "{first role}", roles[0])
	}
	return text
}

func (a *answers) applies(ask Ask) bool {
	if ask.When == "" {
		return true
	}
	key, value, _ := strings.Cut(ask.When, "=")
	return a.values[key] == value
}

func (a *answers) items(ask Ask) []string {
	if ask.Each == "role" {
		return a.list("roles")
	}
	var out []string
	for _, c := range a.list("workflow.checks") {
		if c != "security" {
			out = append(out, c)
		}
	}
	return out
}

func valueFor(flags map[string][]string, flag, item string) (string, bool) {
	for _, v := range flags[flag] {
		if item == "" {
			return v, true
		}
		if k, val, ok := strings.Cut(v, "="); ok && k == item {
			return val, true
		}
	}
	return "", false
}

// the project's config from the preset, the flags and, at a terminal, the answers to what is left
func (k *Kit) New(name, preset string, flags map[string][]string, prompt Prompt) (string, error) {
	modules, err := k.Preset(preset)
	if err != nil {
		return "", err
	}
	a := &answers{values: map[string]string{"name": name}, each: map[string][][2]string{}}
	known := map[string]bool{"--preset": true}
	var asks []Ask
	for _, m := range k.Modules {
		if m.Always {
			asks = append(asks, m.Asks...)
		}
	}
	for _, m := range k.Modules {
		if !m.Always && contains(modules, m.Name) {
			asks = append(asks, m.Asks...)
		}
	}
	owner := map[string]Module{}
	for _, m := range k.Modules {
		for _, ask := range m.Asks {
			owner[ask.Flag()] = m
		}
	}
	for _, flag := range sortedKeys(flags) {
		m, ok := owner[flag]
		switch {
		case known[flag]:
		case !ok:
			return "", fmt.Errorf("mingo new knows no %s", flag)
		case !m.Always && !contains(modules, m.Name):
			return "", fmt.Errorf("%s belongs to the %s module, which the %s preset leaves out", flag, m.Name, preset)
		}
	}
	answer := func(ask Ask, item string, index int) (string, error) {
		if v, ok := valueFor(flags, ask.Flag(), item); ok {
			return v, nil
		}
		def := ""
		if ask.Default != nil {
			def = a.fill(*ask.Default, item, index)
		}
		if v, asked := prompt(a.fill(ask.Question, item, index), def); asked {
			if v = strings.TrimSpace(v); v != "" {
				return v, nil
			}
		}
		if ask.Default == nil {
			flag := ask.Flag()
			if item != "" {
				flag += " " + item + "=..."
			}
			return "", fmt.Errorf("mingo new needs %s, %s", flag, a.fill(ask.Question, item, index))
		}
		return def, nil
	}
	for _, ask := range asks {
		if !a.applies(ask) {
			continue
		}
		if ask.Each == "" {
			v, err := answer(ask, "", 0)
			if err != nil {
				return "", err
			}
			a.values[ask.Key] = v
			continue
		}
		for i, item := range a.items(ask) {
			v, err := answer(ask, item, i)
			if err != nil {
				return "", err
			}
			a.each[ask.Key] = append(a.each[ask.Key], [2]string{item, v})
		}
	}
	roles := a.list("roles")
	if preset == "solo" && len(roles) != 1 {
		return "", fmt.Errorf("the solo preset has one role, not %d", len(roles))
	}
	if !contains(roles, a.values["lead"]) {
		return "", fmt.Errorf("the lead %s is none of the roles %s", a.values["lead"], strings.Join(roles, " "))
	}
	return a.toml(name, preset, modules)
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(s string) string {
	if bareKey.MatchString(s) {
		return s
	}
	return tomlString(s)
}

// a TOML basic string, whose escapes are fewer than Go's
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = tomlString(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func (a *answers) toml(name, preset string, modules []string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# mingo project %s, made by mingo new with the preset %s\n\n", name, preset)
	fmt.Fprintf(&b, "repo = %s\n", tomlString(a.values["repo"]))
	if t := a.values["title"]; t != "" && t != name {
		fmt.Fprintf(&b, "title = %s\n", tomlString(t))
	}
	fmt.Fprintf(&b, "lead = %s\n", tomlString(a.values["lead"]))
	fmt.Fprintf(&b, "modules = %s\n", tomlList(modules))
	type entry struct{ key, value string }
	tables := []struct {
		name    string
		entries []entry
	}{
		{"workflow", []entry{{"base", "workflow.base"}, {"checks", "workflow.checks"}, {"gate", "workflow.gate"}, {"tags", "workflow.tags"}}},
		{"ci", []entry{{"stack", "ci.stack"}}},
		{"review", []entry{{"read", "review.read"}}},
		{"tracker", []entry{{"kind", "tracker.kind"}, {"team", "tracker.team"}, {"project", "tracker.project"}}},
		{"heavy", []entry{{"scripts", "heavy.scripts"}}},
		{"ports", []entry{{"variable", "ports.variable"}}},
	}
	lists := map[string]bool{"workflow.checks": true, "workflow.gate": true, "workflow.tags": true, "heavy.scripts": true}
	for _, t := range tables {
		var lines []string
		for _, e := range t.entries {
			v, ok := a.values[e.value]
			if !ok || v == "" {
				continue
			}
			if lists[e.value] {
				lines = append(lines, e.key+" = "+tomlList(a.list(e.value)))
			} else {
				lines = append(lines, e.key+" = "+tomlString(v))
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(&b, "\n[%s]\n%s\n", t.name, strings.Join(lines, "\n"))
		}
		sub := map[string]string{"ci": "ci.run", "ports": "ports.offsets"}[t.name]
		if items := a.each[sub]; len(items) > 0 {
			fmt.Fprintf(&b, "\n[%s]\n", sub)
			for _, it := range items {
				if sub == "ports.offsets" {
					n, err := strconv.Atoi(it[1])
					if err != nil {
						return "", fmt.Errorf("the port offset of %s is %s, not a number", it[0], it[1])
					}
					fmt.Fprintf(&b, "%s = %d\n", tomlKey(it[0]), n)
				} else {
					fmt.Fprintf(&b, "%s = %s\n", tomlKey(it[0]), tomlString(it[1]))
				}
			}
		}
	}
	for _, role := range a.list("roles") {
		fmt.Fprintf(&b, "\n[[roles]]\nname = %s\n", tomlString(role))
	}
	return b.String(), nil
}
