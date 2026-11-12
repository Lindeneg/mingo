package kit

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
)

const LockPath = ".mingo/kit.lock"

type Locked struct {
	Path   string `toml:"path"`
	Module string `toml:"module"`
	Mingo  string `toml:"mingo"`
	Sum    string `toml:"sha256"`
}

type Lock struct {
	Files []Locked `toml:"file"`
}

func ParseLock(data []byte) (*Lock, error) {
	return parseLock(data, LockPath)
}

func parseLock(data []byte, name string) (*Lock, error) {
	l := &Lock{}
	if len(bytes.TrimSpace(data)) == 0 {
		return l, nil
	}
	if _, err := toml.Decode(string(data), l); err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	return l, nil
}

func (l *Lock) Get(path string) (Locked, bool) {
	for _, f := range l.Files {
		if f.Path == path {
			return f, true
		}
	}
	return Locked{}, false
}

func (l *Lock) Set(f Locked) {
	for i := range l.Files {
		if l.Files[i].Path == f.Path {
			l.Files[i] = f
			return
		}
	}
	l.Files = append(l.Files, f)
}

const lockHeader = "# mingo apply writes this file: the module, the mingo version and the hash of each file it rendered\n\n"

func (l *Lock) Bytes() []byte {
	files := append([]Locked(nil), l.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	var b bytes.Buffer
	b.WriteString(lockHeader)
	for i, f := range files {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[[file]]\npath = %s\nmodule = %s\nmingo = %s\nsha256 = %s\n", tomlString(f.Path), tomlString(f.Module), tomlString(f.Mingo), tomlString(f.Sum))
	}
	return b.Bytes()
}
