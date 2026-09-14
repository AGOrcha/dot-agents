package crgrelease

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// The prompt surface is part of the same server contract as tools/list: the
// generated fixture is what upstream actually published, so it is the oracle
// for everything the embedded contract answers with.
func covPromptFixtureDocs(t *testing.T) []promptDoc {
	t.Helper()
	path := filepath.Join(fixtureDir(t), "prompts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var docs []promptDoc
	if err := json.Unmarshal(data, &docs); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if len(docs) == 0 {
		t.Fatalf("%s declares no prompts", path)
	}
	return docs
}

func covPromptFixtureDoc(t *testing.T, name string) promptDoc {
	t.Helper()
	for _, doc := range covPromptFixtureDocs(t) {
		if doc.Name == name {
			return doc
		}
	}
	t.Fatalf("fixture has no prompt %q", name)
	return promptDoc{}
}

func covPromptByName(t *testing.T, name string) Prompt {
	t.Helper()
	prompt, ok, err := LookupPrompt(name)
	if err != nil {
		t.Fatalf("lookup %q: %v", name, err)
	}
	if !ok {
		t.Fatalf("prompt %q is not published", name)
	}
	return prompt
}

func covPromptExcerpt(s string, at int) string {
	if at > len(s) {
		at = len(s)
	}
	end := at + 40
	if end > len(s) {
		end = len(s)
	}
	return s[at:end]
}

func covPromptFirstDiff(got, want string) string {
	at := 0
	for at < len(got) && at < len(want) && got[at] == want[at] {
		at++
	}
	return fmt.Sprintf("offset %d: got %q, want %q", at, covPromptExcerpt(got, at), covPromptExcerpt(want, at))
}

func covPromptAssertMessages(t *testing.T, got, want []PromptMessage) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("rendered %d messages, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Content.Type != want[i].Content.Type {
			t.Errorf("message %d: got %s/%s, want %s/%s",
				i, got[i].Role, got[i].Content.Type, want[i].Role, want[i].Content.Type)
		}
		if got[i].Content.Text != want[i].Content.Text {
			t.Errorf("message %d text differs, %s", i, covPromptFirstDiff(got[i].Content.Text, want[i].Content.Text))
		}
	}
}

// A drop-in replacement that answers tools/list correctly but publishes a
// different prompt inventory is still a visibly different server, so the
// names, their order, and every argument's declared metadata are pinned.
func TestCovPromptSurfaceMatchesRelease(t *testing.T) {
	docs := covPromptFixtureDocs(t)
	prompts, err := Prompts()
	if err != nil {
		t.Fatalf("prompts: %v", err)
	}
	if len(prompts) != len(docs) {
		t.Fatalf("published %d prompts, release has %d", len(prompts), len(docs))
	}
	for i, doc := range docs {
		got := prompts[i]
		t.Run(doc.Name, func(t *testing.T) {
			covPromptAssertMetadata(t, got, doc)
		})
	}
}

func covPromptAssertMetadata(t *testing.T, got Prompt, doc promptDoc) {
	t.Helper()
	if got.Name != doc.Name {
		t.Fatalf("name %q, want %q", got.Name, doc.Name)
	}
	if got.Description != doc.Description {
		t.Errorf("description %q, want %q", got.Description, doc.Description)
	}
	if !reflect.DeepEqual(got.Arguments, doc.Arguments) {
		t.Errorf("arguments %+v, want %+v", got.Arguments, doc.Arguments)
	}
}

func TestCovPromptLookupPromptFindsEveryPublishedPrompt(t *testing.T) {
	for _, doc := range covPromptFixtureDocs(t) {
		t.Run(doc.Name, func(t *testing.T) {
			covPromptAssertMetadata(t, covPromptByName(t, doc.Name), doc)
		})
	}
}

func TestCovPromptLookupPromptRejectsUnknownName(t *testing.T) {
	prompt, ok, err := LookupPrompt("no_such_prompt")
	if err != nil {
		t.Fatalf("unknown prompt must not be an error: %v", err)
	}
	if ok {
		t.Fatalf("unknown prompt reported as published: %+v", prompt)
	}
	if !reflect.DeepEqual(prompt, Prompt{}) {
		t.Errorf("unknown prompt returned %+v, want zero value", prompt)
	}
}

// prompts/list must reproduce the release's advertised prompt descriptors:
// same prompts in the same order with the same argument metadata.
func TestCovPromptsListResultMatchesRelease(t *testing.T) {
	raw, err := PromptsListResult()
	if err != nil {
		t.Fatalf("prompts/list: %v", err)
	}
	var payload struct {
		Prompts []struct {
			Name        string           `json:"name"`
			Description string           `json:"description"`
			Arguments   []PromptArgument `json:"arguments"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode prompts/list payload %s: %v", raw, err)
	}
	docs := covPromptFixtureDocs(t)
	if len(payload.Prompts) != len(docs) {
		t.Fatalf("prompts/list advertised %d prompts, release has %d", len(payload.Prompts), len(docs))
	}
	for i, doc := range docs {
		got := payload.Prompts[i]
		if got.Name != doc.Name {
			t.Errorf("prompt %d named %q, want %q", i, got.Name, doc.Name)
			continue
		}
		if got.Description != doc.Description {
			t.Errorf("%s: description %q, want %q", doc.Name, got.Description, doc.Description)
		}
		covPromptAssertArguments(t, doc.Name, got.Arguments, doc.Arguments)
	}
}

// An absent `arguments` key and an empty list are the same statement to a
// client, so only the declared argument metadata is compared.
func covPromptAssertArguments(t *testing.T, prompt string, got, want []PromptArgument) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: advertised %d arguments, want %d", prompt, len(got), len(want))
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: argument %d is %+v, want %+v", prompt, i, got[i], want[i])
		}
	}
}

// Omitting an argument must keep the release's own rendering, byte for byte —
// including for the two prompts that declare no arguments at all.
func TestCovPromptRenderWithoutArgumentsMatchesReleaseDefaults(t *testing.T) {
	for _, doc := range covPromptFixtureDocs(t) {
		t.Run(doc.Name, func(t *testing.T) {
			prompt := covPromptByName(t, doc.Name)
			covPromptAssertMessages(t, prompt.Render(nil), doc.RenderedDefaults)
			// A key that is not a declared argument is not an argument.
			covPromptAssertMessages(t,
				prompt.Render(map[string]string{"not_an_argument": "ignored"}),
				doc.RenderedDefaults)
		})
	}
}

type covPromptRenderCase struct {
	prompt     string
	argument   string
	value      string
	wantText   string
	wantAbsent string
}

// A supplied argument must land exactly where the release interpolates it,
// replacing — not appending to — the default rendering at that site.
func TestCovPromptRenderSubstitutesSuppliedArgument(t *testing.T) {
	cases := []covPromptRenderCase{{
		prompt:     "review_changes",
		argument:   "base",
		value:      "origin/main",
		wantText:   `review changes against origin/main")`,
		wantAbsent: `review changes against HEAD~1`,
	}, {
		prompt:     "debug_issue",
		argument:   "description",
		value:      "flaky retry loop",
		wantText:   `debug: flaky retry loop")`,
		wantAbsent: `debug: <description>`,
	}, {
		// An explicitly empty value is a supplied value, not an omission.
		prompt:     "review_changes",
		argument:   "base",
		value:      "",
		wantText:   `review changes against ")`,
		wantAbsent: `review changes against HEAD~1`,
	}}
	for _, tc := range cases {
		t.Run(tc.prompt+"/"+tc.value, func(t *testing.T) {
			covPromptAssertSubstitution(t, tc)
		})
	}
}

func covPromptAssertSubstitution(t *testing.T, tc covPromptRenderCase) {
	t.Helper()
	doc := covPromptFixtureDoc(t, tc.prompt)
	prompt := covPromptByName(t, tc.prompt)
	messages := prompt.Render(map[string]string{tc.argument: tc.value})
	if len(messages) != len(doc.RenderedDefaults) {
		t.Fatalf("rendered %d messages, want %d", len(messages), len(doc.RenderedDefaults))
	}
	text := messages[0].Content.Text
	if messages[0].Role != doc.RenderedDefaults[0].Role {
		t.Errorf("role %q, want %q", messages[0].Role, doc.RenderedDefaults[0].Role)
	}
	if !strings.Contains(text, tc.wantText) {
		t.Errorf("rendered text does not contain %q", tc.wantText)
	}
	if strings.Contains(text, tc.wantAbsent) {
		t.Errorf("rendered text still contains the default %q", tc.wantAbsent)
	}
	if strings.Contains(text, argPlaceholder(tc.argument)) {
		t.Errorf("rendered text still contains the unfilled marker %q", argPlaceholder(tc.argument))
	}
}

// pre_merge_check declares `base` but the release's rendering never
// interpolates it; supplying it must leave the release text untouched rather
// than leak a marker or drop content.
func TestCovPromptRenderKeepsTextWhenReleaseNeverInterpolates(t *testing.T) {
	doc := covPromptFixtureDoc(t, "pre_merge_check")
	prompt := covPromptByName(t, "pre_merge_check")
	covPromptAssertMessages(t, prompt.Render(map[string]string{"base": "origin/main"}), doc.RenderedDefaults)
}

// Render hands out its own slice. If it aliased the loaded contract, one
// client mutating its messages would corrupt every later render of the same
// prompt — for the whole process.
func TestCovPromptRenderReturnsIndependentCopy(t *testing.T) {
	doc := covPromptFixtureDoc(t, "review_changes")
	prompt := covPromptByName(t, "review_changes")

	t.Run("default rendering", func(t *testing.T) {
		first := prompt.Render(nil)
		first[0].Role = "corrupted"
		first[0].Content.Text = "corrupted"
		covPromptAssertMessages(t, prompt.Render(nil), doc.RenderedDefaults)
	})

	t.Run("substituted rendering", func(t *testing.T) {
		args := map[string]string{"base": "origin/main"}
		first := prompt.Render(args)
		first[0].Content.Text = "corrupted"
		second := prompt.Render(args)
		if second[0].Content.Text == "corrupted" {
			t.Fatal("second render returned the mutated message")
		}
		if !strings.Contains(second[0].Content.Text, `review changes against origin/main`) {
			t.Error("second render lost the substituted argument")
		}
	})

	t.Run("mutating a default render does not poison substitution", func(t *testing.T) {
		prompt.Render(nil)[0].Content.Text = "corrupted"
		substituted := prompt.Render(map[string]string{"base": "origin/main"})
		if !strings.Contains(substituted[0].Content.Text, `review changes against origin/main`) {
			t.Fatal("substituted render was built from a corrupted default")
		}
	})
}

func covPromptMessages(texts []string) []PromptMessage {
	out := make([]PromptMessage, 0, len(texts))
	for _, text := range texts {
		out = append(out, PromptMessage{Role: "user", Content: PromptContent{Type: "text", Text: text}})
	}
	return out
}

// covPromptSynthetic builds a prompt whose two renderings are chosen to
// exercise the default-recovery alignment. The published release has no
// multi-argument prompt, so partial argument supply cannot be driven from the
// fixture alone.
func covPromptSynthetic(args []string, template, defaults []string) Prompt {
	declared := make([]PromptArgument, 0, len(args))
	for _, name := range args {
		declared = append(declared, PromptArgument{Name: name})
	}
	return Prompt{
		Name:      "synthetic",
		Arguments: declared,
		defaults:  covPromptMessages(defaults),
		template:  covPromptMessages(template),
	}
}

type covPromptPartialCase struct {
	name     string
	template []string
	defaults []string
	want     string
}

// When a client supplies some arguments and omits others, each omitted site
// must fall back to what the release itself renders there. Where the two
// renderings cannot be aligned the site collapses to empty — never to a
// leaked `${arg:...}` marker and never to another argument's text.
func TestCovPromptRenderFillsOmittedArgumentsFromReleaseDefaults(t *testing.T) {
	cases := []covPromptPartialCase{{
		name:     "recovers the default at an aligned site",
		template: []string{"scan ${arg:a} depth ${arg:b} done"},
		defaults: []string{"scan . depth 2 done"},
		want:     "scan . depth B done",
	}, {
		name:     "no default rendering for the message",
		template: []string{"scan ${arg:a} depth ${arg:b} done"},
		defaults: nil,
		want:     "scan  depth B done",
	}, {
		name:     "omitted argument has no interpolation site",
		template: []string{"scan ${arg:b} done"},
		defaults: []string{"scan 2 done"},
		want:     "scan B done",
	}, {
		name:     "default rendering is shorter than the template prefix",
		template: []string{"aaaaaaaaaaaaaaaaaaaa${arg:a} ${arg:b}"},
		defaults: []string{"xy"},
		want:     "aaaaaaaaaaaaaaaaaaaa B",
	}, {
		name:     "site sits at the very end of the template",
		template: []string{"lead ${arg:b} tail ${arg:a}"},
		defaults: []string{"lead 2222222222 tail X"},
		want:     "lead B tail ",
	}, {
		name:     "anchor after the site is absent from the default rendering",
		template: []string{"pre ${arg:a} mid ${arg:b}"},
		defaults: []string{"pre 1 other stuff here"},
		want:     "pre  mid B",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompt := covPromptSynthetic([]string{"a", "b"}, tc.template, tc.defaults)
			covPromptAssertMessages(t,
				prompt.Render(map[string]string{"b": "B"}),
				covPromptMessages([]string{tc.want}))
		})
	}
}

// covPromptSwapContract replaces the embedded prompt contract for one test and
// restores the real release afterwards. Not parallel-safe by construction.
func covPromptSwapContract(t *testing.T, data []byte) {
	t.Helper()
	original := promptsJSON
	t.Cleanup(func() {
		promptsJSON = original
		covPromptResetPrompts()
		if _, err := Prompts(); err != nil {
			t.Fatalf("restoring the release contract failed: %v", err)
		}
	})
	promptsJSON = data
	covPromptResetPrompts()
}

func covPromptResetPrompts() {
	promptsOnce = sync.Once{}
	promptsList, promptsIndex, promptsErr = nil, nil, nil
}

// A prompt contract that will not decode must surface as an error on every
// entry point rather than as a silently empty prompt surface — an empty
// prompts/list is indistinguishable from a server that publishes no prompts.
func TestCovPromptUndecodableContractIsReportedEverywhere(t *testing.T) {
	covPromptSwapContract(t, []byte(`[{"name": `))

	prompts, err := Prompts()
	if err == nil {
		t.Fatalf("Prompts accepted an undecodable contract: %+v", prompts)
	}
	if !strings.Contains(err.Error(), "crgrelease: decode prompts") {
		t.Errorf("error %q does not name the failing contract", err)
	}
	if prompts != nil {
		t.Errorf("Prompts returned %d prompts alongside an error", len(prompts))
	}

	prompt, ok, err := LookupPrompt("review_changes")
	if err == nil {
		t.Errorf("LookupPrompt hid the decode failure: %+v ok=%v", prompt, ok)
	}
	if ok {
		t.Error("LookupPrompt reported a prompt from an undecodable contract")
	}

	raw, err := PromptsListResult()
	if err == nil {
		t.Errorf("prompts/list served %s from an undecodable contract", raw)
	}
	if raw != nil {
		t.Errorf("prompts/list returned a payload alongside an error: %s", raw)
	}
}
