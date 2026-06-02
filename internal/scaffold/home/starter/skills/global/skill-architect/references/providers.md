# Provider configuration

skill-architect is **provider-pluggable**. The modes that call an LLM (`eval`,
`improve`, `optimize`) route every model call through `scripts/providers.py`,
so you can keep the zero-config default or point the skill at any provider.

## The two seams

| Seam | What it does | Pluggable across |
|------|--------------|------------------|
| `complete_text()` | single-shot completion (rewrites a skill description) | **every** provider below |
| `HarnessConfig` | agentic trigger-eval (does the agent *invoke* the skill?) | agentic harnesses only |

Description improvement (`optimize`/`improve`) is a plain prompt→text call and
works with any provider. **Trigger evaluation** (`eval`, and the eval step
inside `optimize`) needs an *agentic harness* that can decide to invoke a skill
and stream tool-use events — a bare chat API can't do that. The default harness
is the Claude Code CLI; a drop-in compatible CLI can be swapped in via
`HarnessConfig` env vars.

## Selecting a provider

Set `SKILL_ARCHITECT_PROVIDER` (default `claude-cli`):

### `claude-cli` (default)
Local `claude` CLI. No API key — reuses the host session's auth. Supports both
seams (description improvement **and** trigger eval).

```bash
# nothing to configure; this is the default
```

### `anthropic`
Anthropic Messages API. Description improvement only.

```bash
export SKILL_ARCHITECT_PROVIDER=anthropic
export ANTHROPIC_API_KEY=sk-ant-...
export SKILL_ARCHITECT_MODEL=claude-sonnet-4-6   # optional; sensible default
# optional: export ANTHROPIC_BASE_URL=https://api.anthropic.com
```

### `openai`
Any OpenAI-compatible Chat Completions endpoint — OpenAI, Azure OpenAI,
OpenRouter, vLLM, Ollama, LM Studio. Description improvement only.

```bash
export SKILL_ARCHITECT_PROVIDER=openai
export OPENAI_API_KEY=sk-...                      # any non-empty token for local servers
export OPENAI_BASE_URL=https://api.openai.com/v1  # or http://localhost:11434/v1 for Ollama
export SKILL_ARCHITECT_MODEL=gpt-4o-mini          # required: the claude model id won't map here
```

### `cli` (generic escape hatch)
Run any command; the prompt is piped to stdin and stdout is the response. A
literal `{model}` token in the template is replaced with the resolved model id.

```bash
export SKILL_ARCHITECT_PROVIDER=cli
export SKILL_ARCHITECT_CLI_CMD='llm -m gpt-4o'    # or: ollama run llama3, etc.
```

## Model resolution

Precedence for the model id: `SKILL_ARCHITECT_MODEL` env > the `--model` passed
by the calling mode (usually the host session's model) > the provider's default.
The session model id is correct for `claude-cli`/`anthropic` but meaningless for
`openai`, so set `SKILL_ARCHITECT_MODEL` when targeting a non-Claude provider.

`SKILL_ARCHITECT_MAX_TOKENS` (default 4096) caps the HTTP providers' output.

## Pointing trigger-eval at a non-default harness

`HarnessConfig` de-hardcodes the Claude Code markers so a compatible agentic
CLI (one that speaks `-p --output-format stream-json --include-partial-messages`
and emits tool-use stream events) can be swapped in without code changes:

| Env var | Default | Meaning |
|---------|---------|---------|
| `SKILL_ARCHITECT_CLI_BIN` | `claude` | harness CLI binary |
| `SKILL_ARCHITECT_ROOT_MARKER` | `.claude` | project-root marker dir |
| `SKILL_ARCHITECT_COMMANDS_SUBDIR` | `commands` | trigger-registration subdir |
| `SKILL_ARCHITECT_TRIGGER_TOOLS` | `Skill,Read` | tool names that count as a trigger |
| `SKILL_ARCHITECT_STRIP_ENV` | `CLAUDECODE` | env vars stripped before the subprocess |

## Verifying

`scripts/providers.py` ships with a stdlib-only contract test:

```bash
cd <skill-path> && python -m scripts.test_providers
```
