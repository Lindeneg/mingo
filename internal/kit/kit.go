// Package kit renders a project's modules into its repository and sets up and checks what they need.
package kit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
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
	files   []string
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

// workflow files use [[ ]], since GitHub's own expressions are written with braces
func delims(repoPath string) (string, string) {
	if strings.HasSuffix(repoPath, ".yml") || strings.HasSuffix(repoPath, ".yaml") {
		return "[[", "]]"
	}
	return "{{", "}}"
}

// a file whose path or text renders blank is left out, which is how a template says it does not apply
func (k *Kit) Render(p *config.Project, mods []Module) ([]File, error) {
	used := map[string]bool{}
	var out []File
	for _, m := range mods {
		for _, name := range m.files {
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
			data, err := fs.ReadFile(k.fsys, path.Join(m.Name, "files", name))
			if err != nil {
				return nil, err
			}
			left, right := delims(repoPath)
			t, err := template.New(repoPath).Delims(left, right).Option("missingkey=error").Funcs(funcs).Parse(string(data))
			if err != nil {
				return nil, fmt.Errorf("module %s: %v", m.Name, err)
			}
			blocks := p.Blocks[repoPath]
			used[repoPath] = true
			for _, block := range sortedKeys(blocks) {
				if t.Lookup(block) == nil {
					return nil, fmt.Errorf("%s has a block %s for %s, which has no such block", p.File, block, repoPath)
				}
				text := blocks[block]
				if strings.TrimSpace(text) == "" {
					text = `{{""}}`
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
