package kit

import (
	"encoding/json"
	"path"
	"strings"
)

// the files Prettier would format when the project checks or formats them all, given the project's package.json
// and its ignore files, and whether the project has Prettier at all
func prettierOpen(read func(path string) ([]byte, bool), files []File) ([]string, bool) {
	data, ok := read(Manifest)
	if !ok {
		return nil, false
	}
	var pkg struct {
		Dependencies    map[string]any `json:"dependencies"`
		DevDependencies map[string]any `json:"devDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return nil, false
	}
	_, dep := pkg.Dependencies["prettier"]
	_, dev := pkg.DevDependencies["prettier"]
	if !dep && !dev {
		return nil, false
	}
	// prettier reads both by default and skips a file either one ignores
	var sets [][]ignoreRule
	for _, name := range []string{".gitignore", ".prettierignore"} {
		text, _ := read(name)
		sets = append(sets, parseIgnore(string(text)))
	}
	var open []string
	for _, f := range files {
		if !ignored(sets[0], f.Path) && !ignored(sets[1], f.Path) {
			open = append(open, f.Path)
		}
	}
	return open, true
}

type ignoreRule struct {
	segments []string
	negate   bool
	dirOnly  bool
	anchored bool
}

// the lines of a .gitignore, by git's rules
func parseIgnore(text string) []ignoreRule {
	var rules []ignoreRule
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(strings.TrimRight(line, "\r"), " ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			r.negate, line = true, line[1:]
		} else if strings.HasPrefix(line, `\`) {
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly, line = true, strings.TrimSuffix(line, "/")
		}
		r.anchored = strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		r.segments = strings.Split(line, "/")
		rules = append(rules, r)
	}
	return rules
}

// git cannot take a file back out of an ignored folder, so every folder above the file counts first
func ignored(rules []ignoreRule, file string) bool {
	parts := strings.Split(file, "/")
	for i := 1; i < len(parts); i++ {
		if matchIgnore(rules, parts[:i], true) {
			return true
		}
	}
	return matchIgnore(rules, parts, false)
}

// the last rule that matches decides
func matchIgnore(rules []ignoreRule, parts []string, dir bool) bool {
	out := false
	for _, r := range rules {
		if r.dirOnly && !dir {
			continue
		}
		hit := false
		if r.anchored {
			hit = matchSegments(r.segments, parts)
		} else {
			hit, _ = path.Match(r.segments[0], parts[len(parts)-1])
		}
		if hit {
			out = !r.negate
		}
	}
	return out
}

func matchSegments(pattern, parts []string) bool {
	if len(pattern) == 0 {
		return len(parts) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(parts); i++ {
			if matchSegments(pattern[1:], parts[i:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	ok, err := path.Match(pattern[0], parts[0])
	return err == nil && ok && matchSegments(pattern[1:], parts[1:])
}
