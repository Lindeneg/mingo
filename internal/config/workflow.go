package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

type Shared struct {
	Name    string `toml:"name"`
	Pattern string `toml:"pattern"`
	Merge   string `toml:"merge"`
	re      *regexp.Regexp
}

func (s Shared) Matches(path string) bool {
	return s.re != nil && s.re.MatchString(path)
}

type Workflow struct {
	Base          string   `toml:"base"`
	CI            string   `toml:"ci"`
	Checks        []string `toml:"checks"`
	Gate          []string `toml:"gate"`
	LocalRuns     []string `toml:"local_runs"`
	Reviewer      string   `toml:"reviewer"`
	ReviewLine    string   `toml:"review_line"`
	NoFindings    string   `toml:"no_findings"`
	ReviewHeading string   `toml:"review_heading"`
	Footers       []string `toml:"footers"`
	Trailers      []string `toml:"trailers"`
	Tags          []string `toml:"tags"`
	GenerateList  []string `toml:"generate_list"`
	Generate      []string `toml:"generate"`
	Generated     string   `toml:"generated"`
	Shared        []Shared `toml:"shared"`

	footers   []*regexp.Regexp
	generated *regexp.Regexp
}

type Tracker struct {
	Kind    string `toml:"kind"`
	Team    string `toml:"team"`
	Project string `toml:"project"`
}

type CI struct {
	Stack string            `toml:"stack"`
	Setup []string          `toml:"setup"`
	Audit string            `toml:"audit"`
	Run   map[string]string `toml:"run"`
}

type Ports struct {
	Variable string         `toml:"variable"`
	Offsets  map[string]int `toml:"offsets"`
}

type Heavy struct {
	Scripts []string `toml:"scripts"`
}

type Review struct {
	Read       string   `toml:"read"`
	Categories []string `toml:"categories"`
	Skip       string   `toml:"skip"`
}

var DefaultCategories = []string{
	"correctness (a wrong result, a crash, a race, an unhandled outcome, a swallowed error)",
	"security (a secret in a log or an error, untrusted input treated as trusted)",
	"a missing test for an edge case the issue or the change implies",
}

// the command a check's job runs, named in [ci.run] or from the stack
func (c CI) Command(check string) string {
	if cmd, ok := c.Run[check]; ok {
		return cmd
	}
	switch c.Stack {
	case "pnpm":
		return "pnpm " + check
	case "npm":
		return "npm run " + check
	}
	return ""
}

func (p *Project) workflowDefaults(md toml.MetaData) error {
	w := &p.Workflow
	set := func(key string, field *string, value string) {
		if !md.IsDefined("workflow", key) {
			*field = value
		}
	}
	set("base", &w.Base, "master")
	set("ci", &w.CI, "ci")
	set("review_line", &w.ReviewLine, "review:")
	set("no_findings", &w.NoFindings, "no findings")
	set("review_heading", &w.ReviewHeading, "## Review")
	if !md.IsDefined("workflow", "footers") {
		w.Footers = append([]string(nil), p.Provider().Kit().Footers...)
	}
	if !md.IsDefined("workflow", "trailers") {
		w.Trailers = append([]string(nil), p.Provider().Kit().Trailers...)
	}
	if !md.IsDefined("workflow", "local_runs") {
		w.LocalRuns = append(append([]string(nil), w.Gate...), "mingo gate")
	}
	for key, commands := range map[string][]string{"gate": w.Gate, "generate_list": w.GenerateList, "generate": w.Generate} {
		for _, c := range commands {
			if strings.TrimSpace(c) == "" {
				return fmt.Errorf("%s has an empty command in [workflow] %s", p.File, key)
			}
		}
	}
	if (len(w.GenerateList) > 0) != (len(w.Generate) > 0) {
		return fmt.Errorf("%s names one of generate_list and generate, name both or neither", p.File)
	}
	if len(w.Generate) > 0 && w.Generated == "" {
		return fmt.Errorf("%s names generate without generated, the pattern of the line that names a written file", p.File)
	}
	for _, f := range w.Footers {
		re, err := regexp.Compile(f)
		if err != nil {
			return fmt.Errorf("%s has a footer pattern that is no regexp, %v", p.File, err)
		}
		w.footers = append(w.footers, re)
	}
	if w.Generated != "" {
		re, err := regexp.Compile(w.Generated)
		if err != nil {
			return fmt.Errorf("%s has a generated pattern that is no regexp, %v", p.File, err)
		}
		if re.NumSubexp() < 1 {
			return fmt.Errorf("%s has a generated pattern without a group for the path", p.File)
		}
		w.generated = re
	}
	for i := range w.Shared {
		s := &w.Shared[i]
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("%s has a [[workflow.shared]] without a name, give it name = \"the route list\" or the like", p.File)
		}
		// an empty pattern matches every path, and pr rebase would keep both sides of every conflict
		if strings.TrimSpace(s.Pattern) == "" {
			return fmt.Errorf("%s has the shared file %q without a pattern, give it the regexp of its paths, such as pattern = '^src/routes\\.ts$'", p.File, s.Name)
		}
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			return fmt.Errorf("%s has a shared file %q whose pattern is no regexp, %v", p.File, s.Name, err)
		}
		s.re = re
		if s.Merge == "" {
			s.Merge = "none"
		}
		if s.Merge != "lines" && s.Merge != "json-keys" && s.Merge != "none" {
			return fmt.Errorf("%s has a shared file %q merged by %s, use lines, json-keys or none", p.File, s.Name, s.Merge)
		}
	}
	if err := p.checkCommands(); err != nil {
		return err
	}
	if err := p.checkChecks(); err != nil {
		return err
	}
	if !md.IsDefined("review", "read") {
		p.Review.Read = DefaultRead(p.Provider(), p.AgentFiles)
	}
	if !md.IsDefined("review", "categories") {
		p.Review.Categories = append([]string(nil), DefaultCategories...)
	}
	if !md.IsDefined("review", "skip") {
		p.Review.Skip = "formatting, lint or style"
	}
	for role := range p.Ports.Offsets {
		if !p.HasRole(role) {
			return fmt.Errorf("%s has a port offset for %s, which is no role", p.File, role)
		}
	}
	if len(p.Ports.Offsets) > 0 && p.Ports.Variable == "" {
		return fmt.Errorf("%s has port offsets without [ports] variable, the line of .env.example they fill", p.File)
	}
	switch p.CI.Stack {
	case "", "pnpm", "npm":
	case "go", "other":
		for _, check := range w.Checks {
			if check != "security" && p.CI.Command(check) == "" {
				return fmt.Errorf("%s has the check %s with no command, name it in [ci.run]", p.File, check)
			}
		}
	default:
		return fmt.Errorf("%s has the stack %s, use pnpm, npm, go or other", p.File, p.CI.Stack)
	}
	if p.CI.Stack != "other" {
		if len(p.CI.Setup) > 0 {
			return fmt.Errorf("%s has [ci] setup, which is for the stack other", p.File)
		}
		if p.CI.Audit != "" {
			return fmt.Errorf("%s has [ci] audit, which is for the stack other", p.File)
		}
	}
	switch p.Tracker.Kind {
	case "":
		p.Tracker.Kind = "none"
	case "none", "github":
	case "linear":
		if p.Tracker.Team == "" {
			return fmt.Errorf("%s has a linear tracker without its team key, such as team = \"ENG\"", p.File)
		}
	default:
		return fmt.Errorf("%s has the tracker %s, use linear, github or none", p.File, p.Tracker.Kind)
	}
	return p.checkModules()
}

func (w *Workflow) IsFooter(line string) bool {
	for _, re := range w.footers {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func (w *Workflow) GeneratedPath(line string) (string, bool) {
	if w.generated == nil {
		return "", false
	}
	m := w.generated.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func (w *Workflow) SharedFile(path string) (Shared, bool) {
	for _, s := range w.Shared {
		if s.Matches(path) {
			return s, true
		}
	}
	return Shared{}, false
}

// the closes line and the issue id a title starts with, both nil for no tracker of issues by id
func (p *Project) IssuePatterns() (closes, title *regexp.Regexp) {
	switch p.Tracker.Kind {
	case "linear":
		team := regexp.QuoteMeta(p.Tracker.Team)
		return regexp.MustCompile(`^Closes ` + team + `-\d+$`), regexp.MustCompile(`^` + team + `-\d+:?\s+`)
	case "github":
		return regexp.MustCompile(`^Closes #\d+$`), nil
	}
	return nil, nil
}

func (p *Project) ClosesExample() string {
	switch p.Tracker.Kind {
	case "linear":
		return "Closes " + p.Tracker.Team + "-n"
	case "github":
		return "Closes #n"
	}
	return ""
}
