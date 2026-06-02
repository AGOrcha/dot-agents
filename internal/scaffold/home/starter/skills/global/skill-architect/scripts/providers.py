#!/usr/bin/env python3
"""Provider-pluggable LLM access for skill-architect.

skill-architect is provider-agnostic. By default it talks to the local
``claude`` CLI (zero-config: it reuses the host Claude Code session's auth and
needs no API key), but it can target any LLM provider by setting
``SKILL_ARCHITECT_PROVIDER`` and the matching credentials. See
``references/providers.md`` for the full matrix.

Two seams live here:

* :func:`complete_text` — single-shot text completion (used to rewrite skill
  descriptions during the optimize loop). Works with every provider below.
* :class:`HarnessConfig` — the *agentic-eval* markers (project marker dir,
  command-trigger dir, trigger tool names, CLI binary, env to strip). Trigger
  evaluation needs an agentic harness that can decide to invoke a skill; the
  default is the Claude Code CLI. Pure text/HTTP providers (``anthropic``,
  ``openai``) can drive description improvement but not trigger eval.

The module is intentionally dependency-free (stdlib only) so it runs anywhere a
skill is scaffolded, without a pip install step.
"""

from __future__ import annotations

import json
import os
import shlex
import subprocess
import urllib.error
import urllib.request
from dataclasses import dataclass


def _split_env(name: str, default: str) -> list[str]:
    """Comma-separated env var → list of non-empty trimmed tokens."""
    raw = os.environ.get(name, default)
    return [tok.strip() for tok in raw.split(",") if tok.strip()]


@dataclass(frozen=True)
class HarnessConfig:
    """Agentic-eval harness markers, de-hardcoded from run_eval.

    Defaults describe the Claude Code CLI. Override any field via the matching
    environment variable to point the trigger evaluator at a compatible
    harness (one that speaks ``-p --output-format stream-json``).
    """

    cli_bin: str = "claude"
    root_marker: str = ".claude"
    commands_subdir: str = "commands"
    trigger_tool_names: tuple[str, ...] = ("Skill", "Read")
    strip_env: tuple[str, ...] = ("CLAUDECODE",)

    @classmethod
    def from_env(cls) -> "HarnessConfig":
        return cls(
            cli_bin=os.environ.get("SKILL_ARCHITECT_CLI_BIN", "claude"),
            root_marker=os.environ.get("SKILL_ARCHITECT_ROOT_MARKER", ".claude"),
            commands_subdir=os.environ.get("SKILL_ARCHITECT_COMMANDS_SUBDIR", "commands"),
            trigger_tool_names=tuple(_split_env("SKILL_ARCHITECT_TRIGGER_TOOLS", "Skill,Read")),
            strip_env=tuple(_split_env("SKILL_ARCHITECT_STRIP_ENV", "CLAUDECODE")),
        )


def clean_env(harness: HarnessConfig | None = None) -> dict[str, str]:
    """Process env with harness-guard vars stripped.

    Removing ``CLAUDECODE`` (the default) lets a ``claude -p`` subprocess nest
    inside an interactive Claude Code session; the guard only matters for
    interactive terminal conflicts, not programmatic subprocess use.
    """
    harness = harness or HarnessConfig.from_env()
    strip = set(harness.strip_env)
    return {k: v for k, v in os.environ.items() if k not in strip}


def provider_name(override: str | None = None) -> str:
    """Resolve the active provider id (lower-cased)."""
    name = (override or os.environ.get("SKILL_ARCHITECT_PROVIDER", "claude-cli")).strip().lower()
    return name or "claude-cli"


def _resolve_model(model: str | None, default: str) -> str:
    """Model precedence: SKILL_ARCHITECT_MODEL env > caller arg > provider default.

    The caller arg is usually the host session's model id, which is correct for
    ``claude-cli``/``anthropic`` but meaningless for ``openai``; targeting a
    non-Claude provider therefore wants ``SKILL_ARCHITECT_MODEL`` set.
    """
    return os.environ.get("SKILL_ARCHITECT_MODEL") or model or default


# --------------------------------------------------------------------------- #
# Text completion backends
# --------------------------------------------------------------------------- #

def complete_text(
    prompt: str,
    *,
    model: str | None = None,
    timeout: int = 300,
    provider: str | None = None,
) -> str:
    """Single-shot text completion via the configured provider.

    Returns the model's text response. Raises RuntimeError on transport/HTTP
    failure and ValueError for an unknown provider id.
    """
    name = provider_name(provider)
    if name in ("claude-cli", "claude", "claude_cli"):
        return _claude_cli_complete(prompt, model, timeout)
    if name == "anthropic":
        return _anthropic_complete(prompt, model, timeout)
    if name in ("openai", "openai-compatible"):
        return _openai_complete(prompt, model, timeout)
    if name in ("cli", "generic-cli", "command"):
        return _generic_cli_complete(prompt, model, timeout)
    raise ValueError(
        f"Unknown SKILL_ARCHITECT_PROVIDER '{name}'. "
        "Supported: claude-cli (default), anthropic, openai, cli."
    )


def _claude_cli_complete(prompt: str, model: str | None, timeout: int) -> str:
    """Default backend: ``claude -p --output-format text`` on stdin.

    Prompt goes over stdin (not argv) because it can embed a full SKILL.md
    body and exceed comfortable argv length. Uses the host session's auth — no
    API key needed.
    """
    bin_ = os.environ.get("SKILL_ARCHITECT_CLI_BIN", "claude")
    cmd = [bin_, "-p", "--output-format", "text"]
    if model:
        cmd.extend(["--model", model])

    result = subprocess.run(
        cmd,
        input=prompt,
        capture_output=True,
        text=True,
        env=clean_env(),
        timeout=timeout,
    )
    if result.returncode != 0:
        raise RuntimeError(
            f"{bin_} -p exited {result.returncode}\nstderr: {result.stderr}"
        )
    return result.stdout


def _http_post_json(url: str, headers: dict[str, str], payload: dict, timeout: int) -> dict:
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={**headers, "Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{url} HTTP {exc.code}: {body[:500]}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"{url} request failed: {exc.reason}") from exc


def _anthropic_complete(prompt: str, model: str | None, timeout: int) -> str:
    """Anthropic Messages API via ANTHROPIC_API_KEY (no SDK dependency)."""
    api_key = os.environ.get("ANTHROPIC_API_KEY")
    if not api_key:
        raise RuntimeError(
            "SKILL_ARCHITECT_PROVIDER=anthropic requires ANTHROPIC_API_KEY."
        )
    base = os.environ.get("ANTHROPIC_BASE_URL", "https://api.anthropic.com").rstrip("/")
    max_tokens = int(os.environ.get("SKILL_ARCHITECT_MAX_TOKENS", "4096"))
    payload = {
        "model": _resolve_model(model, "claude-sonnet-4-6"),
        "max_tokens": max_tokens,
        "messages": [{"role": "user", "content": prompt}],
    }
    headers = {"x-api-key": api_key, "anthropic-version": "2023-06-01"}
    body = _http_post_json(f"{base}/v1/messages", headers, payload, timeout)
    parts = [b.get("text", "") for b in body.get("content", []) if b.get("type") == "text"]
    return "".join(parts)


def _openai_complete(prompt: str, model: str | None, timeout: int) -> str:
    """OpenAI-compatible Chat Completions API.

    Works with OpenAI and any compatible endpoint (Azure OpenAI, OpenRouter,
    vLLM, Ollama, LM Studio, ...) via OPENAI_BASE_URL.
    """
    api_key = os.environ.get("OPENAI_API_KEY")
    if not api_key:
        raise RuntimeError(
            "SKILL_ARCHITECT_PROVIDER=openai requires OPENAI_API_KEY "
            "(any non-empty token for local OpenAI-compatible servers)."
        )
    base = os.environ.get("OPENAI_BASE_URL", "https://api.openai.com/v1").rstrip("/")
    max_tokens = int(os.environ.get("SKILL_ARCHITECT_MAX_TOKENS", "4096"))
    payload = {
        "model": _resolve_model(model, "gpt-4o-mini"),
        "max_tokens": max_tokens,
        "messages": [{"role": "user", "content": prompt}],
    }
    headers = {"Authorization": f"Bearer {api_key}"}
    body = _http_post_json(f"{base}/chat/completions", headers, payload, timeout)
    choices = body.get("choices", [])
    if not choices:
        raise RuntimeError(f"{base}/chat/completions returned no choices: {str(body)[:300]}")
    return choices[0].get("message", {}).get("content", "") or ""


def _generic_cli_complete(prompt: str, model: str | None, timeout: int) -> str:
    """Escape hatch: run an arbitrary CLI, prompt on stdin, stdout = response.

    Set ``SKILL_ARCHITECT_CLI_CMD`` to a command template, e.g.
    ``llm -m gpt-4o`` or ``ollama run llama3``. A literal ``{model}`` token in
    the template is replaced with the resolved model id.
    """
    tmpl = os.environ.get("SKILL_ARCHITECT_CLI_CMD")
    if not tmpl:
        raise RuntimeError(
            "SKILL_ARCHITECT_PROVIDER=cli requires SKILL_ARCHITECT_CLI_CMD "
            "(e.g. 'llm -m gpt-4o'); the prompt is piped to its stdin."
        )
    resolved = _resolve_model(model, "")
    args = shlex.split(tmpl.replace("{model}", resolved))
    result = subprocess.run(
        args,
        input=prompt,
        capture_output=True,
        text=True,
        env=os.environ.copy(),
        timeout=timeout,
    )
    if result.returncode != 0:
        raise RuntimeError(
            f"generic cli '{tmpl}' exited {result.returncode}\nstderr: {result.stderr}"
        )
    return result.stdout
