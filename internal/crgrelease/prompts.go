package crgrelease

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Prompt is one published MCP prompt of the pinned release.
//
// Prompts are part of the same server contract as tools: a drop-in
// replacement that answers `tools/list` correctly but has no prompts is still
// a visibly different server to any client that lists or renders them.
type Prompt struct {
	Name        string
	Description string
	Arguments   []PromptArgument
	// defaults is the message set the release renders when the client
	// supplies no arguments — i.e. with each argument's own default applied.
	defaults []PromptMessage
	// template is the same message set with every argument's interpolation
	// site marked, so a client-supplied value can be substituted exactly
	// where the release substitutes it.
	template []PromptMessage
}

// PromptArgument is one declared prompt argument.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage is one rendered prompt message.
type PromptMessage struct {
	Role    string        `json:"role"`
	Content PromptContent `json:"content"`
}

// PromptContent is a prompt message's content block.
type PromptContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type promptDoc struct {
	Name             string           `json:"name"`
	Description      string           `json:"description"`
	Arguments        []PromptArgument `json:"arguments"`
	RenderedDefaults []PromptMessage  `json:"rendered_defaults"`
	RenderedTemplate []PromptMessage  `json:"rendered_template"`
}

var (
	promptsOnce  sync.Once
	promptsList  []Prompt
	promptsIndex map[string]Prompt
	promptsErr   error
)

// Prompts returns the published prompt surface in the release's order.
func Prompts() ([]Prompt, error) {
	loadPrompts()
	return promptsList, promptsErr
}

// LookupPrompt returns the named prompt.
func LookupPrompt(name string) (Prompt, bool, error) {
	loadPrompts()
	if promptsErr != nil {
		return Prompt{}, false, promptsErr
	}
	prompt, ok := promptsIndex[name]
	return prompt, ok, nil
}

func loadPrompts() {
	promptsOnce.Do(func() {
		var docs []promptDoc
		if err := json.Unmarshal(promptsJSON, &docs); err != nil {
			promptsErr = fmt.Errorf("crgrelease: decode prompts: %w", err)
			return
		}
		list := make([]Prompt, 0, len(docs))
		index := make(map[string]Prompt, len(docs))
		for _, doc := range docs {
			prompt := Prompt{
				Name:        doc.Name,
				Description: doc.Description,
				Arguments:   doc.Arguments,
				defaults:    doc.RenderedDefaults,
				template:    doc.RenderedTemplate,
			}
			list = append(list, prompt)
			index[prompt.Name] = prompt
		}
		promptsList, promptsIndex = list, index
	})
}

// argPlaceholder is the marker the fixture generator leaves at each
// argument's interpolation site.
func argPlaceholder(name string) string { return "${arg:" + name + "}" }

// Render produces the prompt's messages for a set of client-supplied
// arguments. An argument the client omits keeps the release's own default
// rendering, which is why both renderings are captured.
func (p Prompt) Render(arguments map[string]string) []PromptMessage {
	supplied := make(map[string]string, len(arguments))
	for _, argument := range p.Arguments {
		if value, ok := arguments[argument.Name]; ok {
			supplied[argument.Name] = value
		}
	}
	if len(supplied) == 0 {
		return cloneMessages(p.defaults)
	}
	// Start from the marked template so every interpolation site is known,
	// then fill each site with either the supplied value or the value the
	// default rendering shows at that site.
	messages := cloneMessages(p.template)
	for index := range messages {
		text := messages[index].Content.Text
		for _, argument := range p.Arguments {
			value, ok := supplied[argument.Name]
			if !ok {
				value = p.defaultValue(argument.Name, index)
			}
			text = strings.ReplaceAll(text, argPlaceholder(argument.Name), value)
		}
		messages[index].Content.Text = text
	}
	return messages
}

// defaultValue recovers what the release renders at an argument's site when
// the client omits it, by aligning the template and default renderings around
// the first placeholder.
func (p Prompt) defaultValue(name string, index int) string {
	if index >= len(p.defaults) || index >= len(p.template) {
		return ""
	}
	marker := argPlaceholder(name)
	template := p.template[index].Content.Text
	rendered := p.defaults[index].Content.Text
	start := strings.Index(template, marker)
	if start < 0 {
		return ""
	}
	suffix := template[start+len(marker):]
	// Any other argument's marker in the suffix makes a positional match
	// ambiguous; the prefix/suffix anchors still bound the value.
	if cut := strings.Index(suffix, "${arg:"); cut >= 0 {
		suffix = suffix[:cut]
	}
	if len(rendered) < start {
		return ""
	}
	tail := rendered[start:]
	end := strings.Index(tail, suffix)
	if suffix == "" || end < 0 {
		return ""
	}
	return tail[:end]
}

func cloneMessages(messages []PromptMessage) []PromptMessage {
	out := make([]PromptMessage, len(messages))
	copy(out, messages)
	return out
}

// PromptsListResult is the exact `prompts/list` result payload.
func PromptsListResult() (json.RawMessage, error) {
	prompts, err := Prompts()
	if err != nil {
		return nil, err
	}
	type descriptor struct {
		Name        string           `json:"name"`
		Description string           `json:"description,omitempty"`
		Arguments   []PromptArgument `json:"arguments,omitempty"`
	}
	out := make([]descriptor, 0, len(prompts))
	for _, prompt := range prompts {
		out = append(out, descriptor{
			Name:        prompt.Name,
			Description: prompt.Description,
			Arguments:   prompt.Arguments,
		})
	}
	return json.Marshal(map[string]any{"prompts": out})
}
