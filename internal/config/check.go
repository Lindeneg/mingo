package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// a key mingo does not read would load as its default without a word, so every one is refused
func (p *Project) checkKeys(md toml.MetaData) error {
	undecoded := md.Undecoded()
	unknown := map[string]bool{}
	for _, k := range undecoded {
		unknown[k.String()] = true
	}
	var keys, hints []string
	for _, k := range undecoded {
		if under(k, unknown) {
			continue
		}
		hint := ""
		if near := nearestKey(k); near != "" {
			if t := md.Type(k...); t == "Hash" || t == "ArrayHash" {
				near = tableOf(t, append(append(toml.Key{}, k[:len(k)-1]...), near))
			}
			hint = ", did you mean " + near
		}
		keys, hints = append(keys, shownKey(md, k)), append(hints, hint)
	}
	switch {
	case len(keys) == 0:
		return nil
	case len(keys) == 1 && hints[0] == "":
		return fmt.Errorf("%s has the key %s, which mingo does not know, README.md names every key", p.File, keys[0])
	case len(keys) == 1:
		return fmt.Errorf("%s has the key %s, which mingo does not know%s", p.File, keys[0], hints[0])
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s has keys mingo does not know, README.md names every key", p.File)
	for i := range keys {
		b.WriteString("\n  " + keys[i] + hints[i])
	}
	return errors.New(b.String())
}

// a key inside an unknown table is named by the table alone
func under(k toml.Key, unknown map[string]bool) bool {
	for i := 1; i < len(k); i++ {
		if unknown[k[:i].String()] {
			return true
		}
	}
	return false
}

func tableOf(typ string, k toml.Key) string {
	if typ == "ArrayHash" {
		return "[[" + k.String() + "]]"
	}
	return "[" + k.String() + "]"
}

func shownKey(md toml.MetaData, k toml.Key) string {
	if t := md.Type(k...); t == "Hash" || t == "ArrayHash" {
		return tableOf(t, k)
	}
	last := toml.Key{k[len(k)-1]}.String()
	if len(k) == 1 {
		return last
	}
	parent := k[:len(k)-1]
	return tableOf(md.Type(parent...), parent) + " " + last
}

// the known key of the same table that is a slip of the finger away, if there is one
func nearestKey(k toml.Key) string {
	t := reflect.TypeOf(Project{})
	for _, part := range k[:len(k)-1] {
		f, ok := fieldOf(t, part)
		if !ok {
			return ""
		}
		t = f.Type
		for t.Kind() == reflect.Slice {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return ""
		}
	}
	key := k[len(k)-1]
	best, bestDist := "", max(1, len(key)/3)+1
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("toml")
		if tag == "" || tag == "-" {
			continue
		}
		if d := distance(key, tag); d < bestDist {
			best, bestDist = tag, d
		}
	}
	return best
}

func fieldOf(t reflect.Type, tag string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Tag.Get("toml") == tag {
			return t.Field(i), true
		}
	}
	return reflect.StructField{}, false
}

// the edits from a to b, a swap of two neighbours counting as one
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

func shown(table, key string) string {
	if table == "" {
		return key
	}
	return "[" + table + "] " + key
}

// a key set to nothing is a slip, since leaving it out gives its default
func (p *Project) checkSet(md toml.MetaData) error {
	for _, v := range []struct {
		table, key, value string
	}{
		{"", "title", p.Title},
		{"", "lead", p.Lead},
		{"", "opener", p.Opener},
		{"workflow", "base", p.Workflow.Base},
		{"workflow", "ci", p.Workflow.CI},
		{"workflow", "review_line", p.Workflow.ReviewLine},
		{"workflow", "no_findings", p.Workflow.NoFindings},
		{"workflow", "review_heading", p.Workflow.ReviewHeading},
		{"tracker", "team", p.Tracker.Team},
		{"ports", "variable", p.Ports.Variable},
		{"review", "read", p.Review.Read},
		{"review", "skip", p.Review.Skip},
	} {
		path := []string{v.key}
		if v.table != "" {
			path = []string{v.table, v.key}
		}
		if md.IsDefined(path...) && strings.TrimSpace(v.value) == "" {
			return fmt.Errorf("%s sets %s to %q, give it a value or leave the key out", p.File, shown(v.table, v.key), v.value)
		}
	}
	// mingo matches each of these against one line of a comment or a description
	for _, v := range [][2]string{{"review_line", p.Workflow.ReviewLine}, {"no_findings", p.Workflow.NoFindings}, {"review_heading", p.Workflow.ReviewHeading}} {
		if strings.ContainsAny(v[1], "\r\n") {
			return fmt.Errorf("%s has [workflow] %s %q on more than one line, mingo matches it against a single line", p.File, v[0], v[1])
		}
	}
	for _, l := range []struct {
		table, key string
		list       []string
	}{
		{"workflow", "local_runs", p.Workflow.LocalRuns},
		{"workflow", "footers", p.Workflow.Footers},
		{"workflow", "trailers", p.Workflow.Trailers},
		{"workflow", "tags", p.Workflow.Tags},
		{"ci", "setup", p.CI.Setup},
		{"review", "categories", p.Review.Categories},
	} {
		for _, item := range l.list {
			if strings.TrimSpace(item) == "" {
				return fmt.Errorf("%s has an empty entry %q in %s, take it out", p.File, item, shown(l.table, l.key))
			}
		}
	}
	if md.IsDefined("review", "categories") && len(p.Review.Categories) == 0 {
		return fmt.Errorf("%s has [review] categories = [], name what the review reports or leave the key out", p.File)
	}
	for _, check := range sortedKeys(p.CI.Run) {
		if strings.TrimSpace(p.CI.Run[check]) == "" {
			return fmt.Errorf("%s has [ci.run] %s = %q, give the check its command", p.File, check, p.CI.Run[check])
		}
	}
	return nil
}

// owner and name as GitHub allows them, and no .git, which GitHub takes off a new name
var repoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*/[A-Za-z0-9._-]+$`)

func (p *Project) checkRepo() error {
	if p.Repo == "" {
		return nil
	}
	_, repoName, _ := strings.Cut(p.Repo, "/")
	if !repoRe.MatchString(p.Repo) || repoName == "." || repoName == ".." || strings.HasSuffix(repoName, ".git") {
		return fmt.Errorf("%s has the repo %q, write it as owner/name, such as you/shop, without https://github.com/ or .git", p.File, p.Repo)
	}
	return nil
}

// a GitHub Actions job id
var jobRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// mingo pr merge matches each check against the names GitHub reports, which are a job's name and
// its matrix values, so only the ci module, which makes a job of each check, needs job ids
func (p *Project) checkChecks() error {
	ci := slices.Contains(p.Modules, "ci")
	seen := map[string]bool{}
	for _, check := range p.Workflow.Checks {
		switch {
		case ci && !jobRe.MatchString(check):
			return fmt.Errorf("%s has the check %q in [workflow] checks, which GitHub cannot take as a job id, and the ci module makes a job of each check, use letters, digits, - and _, starting with a letter or _", p.File, check)
		case strings.TrimSpace(check) == "":
			return fmt.Errorf("%s has an empty check %q in [workflow] checks, take it out", p.File, check)
		}
		if seen[check] {
			return fmt.Errorf("%s has the check %s twice in [workflow] checks", p.File, check)
		}
		seen[check] = true
	}
	for _, check := range sortedKeys(p.CI.Run) {
		switch {
		case check == "security":
			return fmt.Errorf("%s has [ci.run] security, but the security check runs its own steps, use [ci] audit with the stack other for its audit", p.File)
		case len(p.Workflow.Checks) == 0:
			return fmt.Errorf("%s has [ci.run] %q and no [workflow] checks, add the check there or take its command out", p.File, check)
		case !seen[check]:
			return fmt.Errorf("%s has [ci.run] %q, which is none of the checks %s in [workflow] checks, add it there or take it out", p.File, check, strings.Join(p.Workflow.Checks, " "))
		}
	}
	return nil
}

// what a module needs from the config to do anything at all
func (p *Project) checkModules() error {
	on := func(name string) bool {
		for _, m := range p.Modules {
			if m == name {
				return true
			}
		}
		return false
	}
	switch {
	case on("ci") && p.CI.Stack == "":
		return fmt.Errorf("%s has the ci module without [ci] stack, use pnpm, npm, go or other", p.File)
	case on("ci") && len(p.Workflow.Checks) == 0:
		return fmt.Errorf("%s has the ci module and no [workflow] checks, name the jobs every pull request must pass", p.File)
	case on("ports") && len(p.Ports.Offsets) == 0:
		return fmt.Errorf("%s has the ports module without [ports.offsets], give each role its port offset", p.File)
	}
	return nil
}

// the modules a config names and the lead's name, against what the kit has, when it gave it
func (p *Project) checkKit() error {
	if kitNames == nil {
		return nil
	}
	k, err := kitNames()
	if err != nil {
		return err
	}
	for _, m := range p.Modules {
		switch {
		case slices.Contains(k.Always, m):
			return fmt.Errorf("%s names the module %s, which is always on, take it out of modules", p.File, m)
		case !slices.Contains(k.Modules, m):
			return fmt.Errorf("%s names the module %q, which mingo does not have, it has %s", p.File, m, strings.Join(k.Modules, " "))
		}
	}
	// the lead's skill is in the skills folder under its name, which would be one of the kit's own skills
	if slices.Contains(k.Skills, p.Lead) {
		return fmt.Errorf("%s has the lead %s, whose skill would take %s/%s, the folder of the kit's own %s skill, make another role the lead or rename this one", p.File, p.Lead, p.Provider().Kit().Skills, p.Lead, p.Lead)
	}
	return nil
}

// gate and generate commands run without a shell, split on spaces, so the shell's syntax would reach the program as words
var shellSyntax = []string{"&&", "||", "|", ";", ">", "<", "`", "$(", `"`, "'"}

func (p *Project) checkCommands() error {
	for _, l := range []struct {
		key  string
		list []string
	}{
		{"gate", p.Workflow.Gate},
		{"generate_list", p.Workflow.GenerateList},
		{"generate", p.Workflow.Generate},
	} {
		for _, c := range l.list {
			for _, s := range shellSyntax {
				if strings.Contains(c, s) {
					return fmt.Errorf("%s has %q in [workflow] %s, which mingo runs without a shell, so %s does not work there, put the line in a package script or a script file", p.File, c, l.key, s)
				}
			}
		}
	}
	return nil
}

// a session's name joins the project's and the role's, which two projects can come to share
func (p *Project) checkSessions() error {
	for _, other := range Names() {
		if other == p.Name || !strings.HasPrefix(other, p.Name+"-") && !strings.HasPrefix(p.Name, other+"-") {
			continue
		}
		for _, theirs := range readRoles(other) {
			for _, r := range p.Roles {
				if session := p.SessionName(r.Name); session == other+"-"+theirs {
					return fmt.Errorf("%s has the role %s and project %s the role %s, which are both the tmux session %s, rename one of them", p.File, r.Name, other, theirs, session)
				}
			}
		}
	}
	return nil
}

func readRoles(name string) []string {
	var f struct {
		Roles []struct {
			Name string `toml:"name"`
		} `toml:"roles"`
	}
	if _, err := toml.DecodeFile(File(name), &f); err != nil {
		return nil
	}
	var names []string
	for _, r := range f.Roles {
		names = append(names, r.Name)
	}
	return names
}

// whether a folder is in one of the clones a project's clone pattern names, whatever its roles,
// read without the checks of a load, so a config with a mistake still claims its clones
func clonesHold(name, here string) bool {
	var f struct {
		Clone *string `toml:"clone"`
	}
	clone := DefaultClone
	if _, err := toml.DecodeFile(File(name), &f); err == nil && f.Clone != nil {
		clone = *f.Clone
	}
	pattern := filepath.Clean(strings.ReplaceAll(ExpandHome(clone), "{project}", name))
	if !filepath.IsAbs(pattern) {
		return false
	}
	parts := strings.Split(pattern, "{role}")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	re, err := regexp.Compile("^" + strings.Join(parts, "[^/]+") + "(/|$)")
	return err == nil && re.MatchString(here)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
