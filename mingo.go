// Package mingo embeds the files mingo writes and its kit of modules, and says its version.
package mingo

import (
	"embed"
	"runtime/debug"
)

//go:embed share/mingo@.service share/mingo.plist share/project.toml.example
var Share embed.FS

//go:embed all:kits
var Kits embed.FS

var version string

func Version() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	revision, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if revision == "" {
		return "devel"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if dirty {
		revision += "+dirty"
	}
	return revision
}
