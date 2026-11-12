package claude

import (
	_ "embed"
	"path"

	"github.com/Lindeneg/mingo/internal/ojson"
	"github.com/Lindeneg/mingo/internal/provider"
)

//go:embed words.tmpl
var words string

//go:embed review.yml
var review string

func (Claude) Kit() provider.Kit {
	return provider.Kit{
		Instructions: "CLAUDE.md",
		Import:       "@%s",
		Dir:          ".claude",
		Skills:       ".claude/skills",
		Footers:      []string{`^\s*🤖 Generated with`, `claude\.ai/code/session_`},
		Trailers:     []string{"Co-Authored-By", "Claude-Session"},
		Connectors:   "on the Claude account the sessions use, at claude.ai under Settings, Connectors",
		Words:        words,
		Files:        map[string]map[string]string{"review": {".github/workflows/review.yml": review}},
		// the review workflow posts through the Claude GitHub app, whose comments come from this login
		Review: provider.Review{Secret: "CLAUDE_CODE_OAUTH_TOKEN", Bot: "claude[bot]", Token: "claude setup-token"},
	}
}

func (Claude) AttributionFile() string {
	return path.Join(".claude", "settings.json")
}

// Claude Code's own setting for no attribution in commits, pull requests and their session links
func (Claude) AttributionOff(settings *ojson.Object) bool {
	if have, set := settings.Get("attribution"); set {
		if o, ok := have.(*ojson.Object); ok {
			commit, _ := o.Get("commit")
			pr, _ := o.Get("pr")
			session, _ := o.Get("sessionUrl")
			if commit == "" && pr == "" && session == false {
				return true
			}
		}
	}
	o := ojson.NewObject()
	o.Set("commit", "")
	o.Set("pr", "")
	o.Set("sessionUrl", false)
	settings.Set("attribution", o)
	return false
}
