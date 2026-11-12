package sys

import (
	"bytes"
	"encoding/xml"
	"path/filepath"
	"strings"
	"text/template"
)

func Label(project string) string {
	return "mingo." + project
}

func PlistPath(home, project string) string {
	return filepath.Join(home, "Library", "LaunchAgents", Label(project)+".plist")
}

func xmlText(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func RenderPlist(tmpl []byte, home, project string) ([]byte, error) {
	t, err := template.New("plist").Parse(string(tmpl))
	if err != nil {
		return nil, err
	}
	data := struct{ Label, Home, Project string }{xmlText(Label(project)), xmlText(home), xmlText(project)}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
