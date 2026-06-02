#!/usr/bin/env python3
"""Stdlib-only contract tests for scripts/providers.py.

Run from the skill root:  python -m scripts.test_providers

No pytest / network required — exercises provider resolution, the harness
config seam, env hygiene, and the generic-CLI backend via a real `cat`
round-trip. Exits non-zero on first failure.
"""

import os
import stat
import sys
import tempfile

from scripts import providers as p


def _fake_bin(dirpath: str, name: str, body: str) -> str:
    """Write an executable shell stub into dirpath and return its path."""
    path = os.path.join(dirpath, name)
    with open(path, "w") as fh:
        fh.write("#!/bin/sh\n" + body + "\n")
    os.chmod(path, os.stat(path).st_mode | stat.S_IEXEC | stat.S_IXGRP | stat.S_IXOTH)
    return path


def _isolated_env(**overrides):
    """Snapshot env, apply overrides (None deletes), restore on exit."""
    keys = [
        "SKILL_ARCHITECT_PROVIDER", "SKILL_ARCHITECT_CLI_CMD", "SKILL_ARCHITECT_MODEL",
        "SKILL_ARCHITECT_TRIGGER_TOOLS", "SKILL_ARCHITECT_CLI_BIN", "CLAUDECODE",
        "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "SKILL_ARCHITECT_PLATFORM", "PATH",
    ]

    class _Ctx:
        def __enter__(self):
            self._saved = {k: os.environ.get(k) for k in keys}
            for k, v in overrides.items():
                if v is None:
                    os.environ.pop(k, None)
                else:
                    os.environ[k] = v
            return self

        def __exit__(self, *_):
            for k, v in self._saved.items():
                if v is None:
                    os.environ.pop(k, None)
                else:
                    os.environ[k] = v

    return _Ctx()


def test_provider_resolution():
    with _isolated_env(SKILL_ARCHITECT_PROVIDER=None):
        assert p.provider_name() == "claude-cli"
    with _isolated_env(SKILL_ARCHITECT_PROVIDER="OpenAI"):
        assert p.provider_name() == "openai"


def test_harness_defaults_and_overrides():
    with _isolated_env(SKILL_ARCHITECT_TRIGGER_TOOLS=None, SKILL_ARCHITECT_CLI_BIN=None):
        h = p.HarnessConfig.from_env()
        assert h.cli_bin == "claude"
        assert h.root_marker == ".claude"
        assert h.commands_subdir == "commands"
        assert h.trigger_tool_names == ("Skill", "Read")
        assert h.strip_env == ("CLAUDECODE",)
    with _isolated_env(SKILL_ARCHITECT_TRIGGER_TOOLS="Skill, Read, UseSkill",
                       SKILL_ARCHITECT_CLI_BIN="myharness"):
        h = p.HarnessConfig.from_env()
        assert h.trigger_tool_names == ("Skill", "Read", "UseSkill")
        assert h.cli_bin == "myharness"


def test_clean_env_does_not_mutate_real_env():
    with _isolated_env(CLAUDECODE="1"):
        assert "CLAUDECODE" not in p.clean_env()
        assert os.environ["CLAUDECODE"] == "1"


def test_generic_cli_roundtrip():
    with _isolated_env(SKILL_ARCHITECT_PROVIDER="cli", SKILL_ARCHITECT_CLI_CMD="cat"):
        assert p.complete_text("hello-world-42", model="ignored").strip() == "hello-world-42"
    with _isolated_env(SKILL_ARCHITECT_PROVIDER="cli",
                       SKILL_ARCHITECT_CLI_CMD="printf %s {model}",
                       SKILL_ARCHITECT_MODEL="modelX"):
        assert p.complete_text("stdin-ignored") == "modelX"


def test_unknown_provider_raises():
    with _isolated_env(SKILL_ARCHITECT_PROVIDER="bogus"):
        try:
            p.complete_text("x")
        except ValueError as e:
            assert "bogus" in str(e)
        else:
            raise AssertionError("expected ValueError")


def test_missing_api_keys_are_explicit():
    for prov, var in (("anthropic", "ANTHROPIC_API_KEY"), ("openai", "OPENAI_API_KEY")):
        with _isolated_env(SKILL_ARCHITECT_PROVIDER=prov, **{var: None}):
            try:
                p.complete_text("x")
            except RuntimeError as e:
                assert var in str(e)
            else:
                raise AssertionError(f"expected RuntimeError for {prov}")


def test_platform_registry_covers_five_da_platforms():
    assert set(p.PLATFORM_CLIS) == {"claude", "cursor", "codex", "opencode", "copilot"}
    assert set(p.PLATFORM_ORDER) == set(p.PLATFORM_CLIS)


def test_resolve_platform_explicit_and_env():
    with _isolated_env(SKILL_ARCHITECT_PROVIDER=None, SKILL_ARCHITECT_PLATFORM=None):
        # explicit <platform>-cli wins
        assert p.resolve_platform("codex-cli") == "codex"
        assert p.resolve_platform("opencode-cli") == "opencode"
    with _isolated_env(SKILL_ARCHITECT_PLATFORM="cursor"):
        assert p.resolve_platform("claude-cli") == "cursor"  # env honored under the default id


def test_resolve_platform_autodetects_from_path():
    with tempfile.TemporaryDirectory() as d:
        _fake_bin(d, "codex", "exit 0")  # only codex on PATH, no claude/cursor
        with _isolated_env(SKILL_ARCHITECT_PROVIDER=None, SKILL_ARCHITECT_PLATFORM=None, PATH=d):
            assert p.resolve_platform("claude-cli") == "codex"
    with _isolated_env(SKILL_ARCHITECT_PROVIDER=None, SKILL_ARCHITECT_PLATFORM=None, PATH=""):
        assert p.resolve_platform() == "claude"  # nothing on PATH -> claude fallback


def test_platform_cli_stdin_and_arg_delivery():
    with tempfile.TemporaryDirectory() as d:
        # stdin platform (claude): stub ignores flags, echoes stdin
        echo_stdin = _fake_bin(d, "fakecli", "cat")
        with _isolated_env(SKILL_ARCHITECT_PROVIDER="claude-cli", SKILL_ARCHITECT_PLATFORM="claude",
                           SKILL_ARCHITECT_CLI_BIN=echo_stdin):
            assert p.complete_text("hello-via-stdin").strip() == "hello-via-stdin"
        # arg platform (codex): stub prints its last argv (the appended prompt)
        echo_last = _fake_bin(d, "fakecli2", 'for a in "$@"; do last="$a"; done; printf "%s" "$last"')
        with _isolated_env(SKILL_ARCHITECT_PROVIDER="codex-cli", SKILL_ARCHITECT_CLI_BIN=echo_last):
            assert p.complete_text("hello-via-arg") == "hello-via-arg"


def test_platform_cli_nonzero_raises():
    with tempfile.TemporaryDirectory() as d:
        boom = _fake_bin(d, "boom", "echo oops >&2; exit 3")
        with _isolated_env(SKILL_ARCHITECT_PROVIDER="claude-cli", SKILL_ARCHITECT_PLATFORM="claude",
                           SKILL_ARCHITECT_CLI_BIN=boom):
            try:
                p.complete_text("x")
            except RuntimeError as e:
                assert "exited 3" in str(e)
            else:
                raise AssertionError("expected RuntimeError on nonzero exit")


def test_platform_cli_model_flag():
    with tempfile.TemporaryDirectory() as d:
        echo_args = _fake_bin(d, "args", 'printf "%s\\n" "$@"')
        # claude: host_model_ok -> caller's model id is appended via --model
        with _isolated_env(SKILL_ARCHITECT_PROVIDER="claude-cli", SKILL_ARCHITECT_PLATFORM="claude",
                           SKILL_ARCHITECT_CLI_BIN=echo_args, SKILL_ARCHITECT_MODEL=None):
            out = p.complete_text("stdin-ignored", model="claude-opus-4-8")
            assert "--model" in out and "claude-opus-4-8" in out
        # copilot: model_flag is None -> never appended, prompt delivered as arg
        with _isolated_env(SKILL_ARCHITECT_PROVIDER="copilot-cli", SKILL_ARCHITECT_CLI_BIN=echo_args,
                           SKILL_ARCHITECT_MODEL="gpt-x"):
            out = p.complete_text("the-prompt")
            assert "--model" not in out
            assert "the-prompt" in out


def main() -> int:
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_") and callable(v)]
    failed = 0
    for t in tests:
        try:
            t()
            print(f"  ok   {t.__name__}")
        except Exception as e:  # noqa: BLE001 - test harness reports any failure
            failed += 1
            print(f"  FAIL {t.__name__}: {e}")
    print(f"\n{len(tests) - failed}/{len(tests)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
