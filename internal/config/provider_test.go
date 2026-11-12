package config

import (
	"path/filepath"

	"github.com/Lindeneg/mingo/internal/provider"
)

// stands in for the provider a project names none of, which this package's tests cannot import
type testProvider struct{}

func init() {
	provider.Register(testProvider{})
}

func (testProvider) Name() string                     { return provider.Default }
func (testProvider) Title() string                    { return "Test CLI" }
func (testProvider) Site() string                     { return "https://example.com" }
func (testProvider) Command() string                  { return "test-cli" }
func (testProvider) CommandEnv() string               { return "MINGO_TEST_CLI" }
func (testProvider) DefaultArgs() []string            { return []string{"--test", "default"} }
func (testProvider) Args(s provider.Session) []string { return append([]string{s.Opener}, s.Args...) }
func (testProvider) ExitKeys() []string               { return []string{"C-d"} }
func (testProvider) ExitShown() string                { return "Ctrl+D" }
func (testProvider) MemoryIndex() string              { return "" }
func (testProvider) MemoryDir(home, clone string) string {
	return filepath.Join(home, "memory", filepath.Base(clone))
}

func (testProvider) Kit() provider.Kit {
	return provider.Kit{Instructions: "AGENTS.md", Import: "Read %s.", Dir: ".test", Skills: ".test/skills",
		Footers: []string{`^\s*Made with the test CLI`}, Trailers: []string{"Co-Authored-By"}}
}
