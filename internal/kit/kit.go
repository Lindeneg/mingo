// Package kit renders a project's modules into its repository and sets up and checks what they need.
package kit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"

	"github.com/Lindeneg/mingo"
	"github.com/Lindeneg/mingo/internal/config"
)

type Module struct {
	Name    string   `toml:"-"`
	Does    string   `toml:"does"`
	Always  bool     `toml:"always"`
	Setup   []string `toml:"setup"`
	Checks  []string `toml:"checks"`
	Imports []string `toml:"imports"`
	Lines   []string `toml:"lines"`
	Asks    []Ask    `toml:"asks"`
	// whether its files come from the provider, as a review workflow does
	Agent bool `toml:"agent"`
	files []string
}

type Kit struct {
	fsys    fs.FS
	Modules []Module
}

// every folder of fsys with a module.toml is a module, and every file under its files folder a template
func Load(fsys fs.FS) (*Kit, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	k := &Kit{fsys: fsys}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(e.Name(), "module.toml"))
		if err != nil {
			continue
		}
		m := Module{Name: e.Name()}
		if _, err := toml.Decode(string(data), &m); err != nil {
			return nil, fmt.Errorf("module %s: %v", e.Name(), err)
		}
		root := path.Join(e.Name(), "files")
		if _, err := fs.Stat(fsys, root); err == nil {
			err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					m.files = append(m.files, strings.TrimPrefix(p, root+"/"))
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("module %s: %v", e.Name(), err)
			}
		}
		sort.Strings(m.files)
		k.Modules = append(k.Modules, m)
	}
	return k, nil
}

func (k *Kit) Module(name string) (Module, bool) {
	for _, m := range k.Modules {
		if m.Name == name {
			return m, true
		}
	}
	return Module{}, false
}

func (k *Kit) Names() []string {
	var names []string
	for _, m := range k.Modules {
		names = append(names, m.Name)
	}
	return names
}

// the kit's skills live under skills, and the provider places them in its own skills folder
const skillsRoot = "skills/"

// the folders the kit's own skills take in the skills folder, read from the paths that are no template
func (k *Kit) skillFolders() []string {
	var folders []string
	for _, m := range k.Modules {
		for _, name := range m.files {
			rest, ok := strings.CutPrefix(name, skillsRoot)
			folder, _, _ := strings.Cut(rest, "/")
			if ok && folder != "" && !strings.Contains(folder, "{{") && !contains(folders, folder) {
				folders = append(folders, folder)
			}
		}
	}
	sort.Strings(folders)
	return folders
}

func (k *Kit) names() config.KitNames {
	var names config.KitNames
	for _, m := range k.Modules {
		if m.Always {
			names.Always = append(names.Always, m.Name)
		} else {
			names.Modules = append(names.Modules, m.Name)
		}
	}
	names.Skills = k.skillFolders()
	return names
}

// a load refuses a module the kit does not have and a lead named like one of its skills, which only
// the kit knows
func init() {
	config.RegisterKit(func() (config.KitNames, error) {
		k, err := LoadEmbedded()
		if err != nil {
			return config.KitNames{}, err
		}
		return k.names(), nil
	})
}

// the modules a project uses, those always on first, then the ones it names in kit order
func (k *Kit) For(p *config.Project) ([]Module, error) {
	for _, name := range p.Modules {
		m, ok := k.Module(name)
		if !ok {
			return nil, fmt.Errorf("%s names the module %s, mingo has %s", p.File, name, strings.Join(k.Names(), " "))
		}
		if m.Always {
			return nil, fmt.Errorf("%s names the module %s, which is always on", p.File, name)
		}
		if prov := p.Provider(); m.Agent && len(prov.Kit().Files[name]) == 0 {
			return nil, fmt.Errorf("%s names the module %s, which %s has no files for", p.File, name, prov.Title())
		}
	}
	var out []Module
	for _, m := range k.Modules {
		if m.Always {
			out = append(out, m)
		}
	}
	for _, m := range k.Modules {
		if !m.Always && contains(p.Modules, m.Name) {
			out = append(out, m)
		}
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type File struct {
	Path   string
	Module string
	Text   string
}

func (f File) Sum() string {
	return Sum([]byte(f.Text))
}

func Sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

var funcs = template.FuncMap{
	"join": strings.Join,
	"spaced": func(s string) string {
		return strings.ReplaceAll(s, "-", " ")
	},
	"ticks": func(list []string) string {
		out := make([]string, len(list))
		for i, s := range list {
			out[i] = "`" + s + "`"
		}
		return strings.Join(out, ", ")
	},
	"semis": func(list []string) string {
		return strings.Join(list, "; ")
	},
	"has": contains,
	"orlist": func(list []string) string {
		out := make([]string, len(list))
		for i, s := range list {
			out[i] = "`" + s + "`"
		}
		if len(out) < 2 {
			return strings.Join(out, "")
		}
		return strings.Join(out[:len(out)-1], ", ") + " or " + out[len(out)-1]
	},
	"many": func(roles []config.Role) bool {
		return len(roles) > 1
	},
	"yaml":   yamlScalar,
	"indent": indent,
	"cell": func(s string) string {
		return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), "|", `\|`)
	},
	"shared": func(files []config.Shared) string {
		var names []string
		for _, f := range files {
			if !contains(names, f.Name) {
				names = append(names, f.Name)
			}
		}
		if len(names) < 2 {
			return strings.Join(names, "")
		}
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	},
}

// a value as YAML reads it back, left plain where plain reads as the same string, JSON quoted elsewhere
func yamlScalar(s string) string {
	if yamlPlain(s) {
		return s
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

var yamlNotString = regexp.MustCompile(`(?i)^(~|null|true|false|yes|no|on|off|y|n|[-+]?\.?[0-9].*|\.(inf|nan))$`)

func yamlPlain(s string) bool {
	if s == "" || s != strings.TrimSpace(s) || yamlNotString.MatchString(s) {
		return false
	}
	if strings.ContainsAny(s[:1], "-?:,[]{}#&*!|>'\"%@`") {
		return false
	}
	if strings.Contains(s, ": ") || strings.Contains(s, " #") || strings.HasSuffix(s, ":") {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '\u0085' || r == '\u2028' || r == '\u2029' || r == '\ufeff' {
			return false
		}
	}
	return true
}

// a value inside a literal block keeps each of its lines at the block's indent
func indent(n int, s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\n"+strings.Repeat(" ", n))
}

// workflow files use [[ ]], since GitHub's own expressions are written with braces
func delims(repoPath string) (string, string) {
	if strings.HasSuffix(repoPath, ".yml") || strings.HasSuffix(repoPath, ".yaml") {
		return "[[", "]]"
	}
	return "{{", "}}"
}

// a file whose path or text renders blank is left out, which is how a template says it does not apply
func (k *Kit) Render(p *config.Project, mods []Module) ([]File, error) {
	agent := p.Provider().Kit()
	used := map[string]bool{}
	from := map[string]string{}
	var out []File
	for _, m := range mods {
		sources := map[string]func() (string, error){}
		var names []string
		for _, name := range m.files {
			sources[name] = func() (string, error) {
				data, err := fs.ReadFile(k.fsys, path.Join(m.Name, "files", name))
				return string(data), err
			}
			names = append(names, name)
		}
		for _, name := range sortedKeys(agent.Files[m.Name]) {
			text := agent.Files[m.Name][name]
			sources[name] = func() (string, error) { return text, nil }
			names = append(names, name)
		}
		for _, name := range names {
			pathTmpl, err := template.New(name).Option("missingkey=error").Funcs(funcs).Parse(name)
			if err != nil {
				return nil, fmt.Errorf("module %s: %v", m.Name, err)
			}
			var pb bytes.Buffer
			if err := pathTmpl.Execute(&pb, p); err != nil {
				return nil, fmt.Errorf("module %s: %v", m.Name, err)
			}
			repoPath := strings.TrimSpace(pb.String())
			if repoPath == "" {
				continue
			}
			if rest, ok := strings.CutPrefix(repoPath, skillsRoot); ok {
				repoPath = path.Join(agent.Skills, rest)
			}
			if first, ok := from[repoPath]; ok {
				return nil, fmt.Errorf("%s: %s and %s/%s both render %s, so a role is named like one of the kit's files, rename it", p.File, first, m.Name, name, repoPath)
			}
			from[repoPath] = m.Name + "/" + name
			source, err := sources[name]()
			if err != nil {
				return nil, err
			}
			left, right := delims(repoPath)
			t, err := template.New(repoPath).Delims(left, right).Option("missingkey=error").Funcs(funcs).Parse(source)
			if err != nil {
				return nil, fmt.Errorf("module %s: %v", m.Name, err)
			}
			// the provider's words are written with braces, as every template but a workflow is
			if left == "{{" {
				if _, err := t.New("agent.words").Parse(agent.Words); err != nil {
					return nil, fmt.Errorf("the words of %s: %v", p.Provider().Title(), err)
				}
				if _, err := t.New("agent.instructions").Parse(agent.Instructions); err != nil {
					return nil, err
				}
			}
			blocks := p.Blocks[repoPath]
			used[repoPath] = true
			for _, block := range sortedKeys(blocks) {
				if t.Lookup(block) == nil {
					return nil, fmt.Errorf("%s has a block %s for %s, which has no such block", p.File, block, repoPath)
				}
				text := blocks[block]
				if strings.TrimSpace(text) == "" {
					text = left + `""` + right
				}
				if _, err := t.New(block).Parse(text); err != nil {
					return nil, fmt.Errorf("%s, the block %s for %s: %v", p.File, block, repoPath, err)
				}
			}
			var b bytes.Buffer
			if err := t.Execute(&b, p); err != nil {
				return nil, fmt.Errorf("module %s, %s: %v", m.Name, repoPath, err)
			}
			if strings.TrimSpace(b.String()) == "" {
				continue
			}
			out = append(out, File{Path: repoPath, Module: m.Name, Text: b.String()})
		}
	}
	for _, repoPath := range sortedKeys(p.Blocks) {
		if !used[repoPath] {
			return nil, fmt.Errorf("%s has blocks for %s, which none of its modules renders", p.File, repoPath)
		}
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func LoadEmbedded() (*Kit, error) {
	sub, err := fs.Sub(mingo.Kits, "kits")
	if err != nil {
		return nil, err
	}
	return Load(sub)
}
