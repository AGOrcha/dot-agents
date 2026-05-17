package templates

import (
	"bytes"
	_ "embed" // enable //go:embed directives
	"strings"
	texttemplate "text/template"
)

var (
	//go:embed files/agent.md.tmpl
	agentTemplate string

	//go:embed files/skill.md.tmpl
	skillTemplate string
)

type manifestTemplateData struct {
	Name  string
	Title string
}

func RenderSkillManifest(name string) (string, error) {
	return renderManifest(skillTemplate, name)
}

func RenderAgentManifest(name string) (string, error) {
	return renderManifest(agentTemplate, name)
}

func renderManifest(src, name string) (string, error) {
	tmpl, err := texttemplate.New("manifest").Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, manifestTemplateData{
		Name:  name,
		Title: titleFromName(name),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func titleFromName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	if len(parts) == 0 {
		return name
	}
	return strings.Join(parts, " ")
}
