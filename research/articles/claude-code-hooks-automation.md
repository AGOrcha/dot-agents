# 8 Claude Code Hooks That Automate What You Keep Forgetting

**Author:** darkzodchi (@zodchiii)
**Source:** https://x.com/zodchiii/status/2040000216456143002
**Published:** April 3, 2026
**Related:** https://dev.to/myougatheaxo/claude-code-hooks-auto-format-security-guards-and-test-triggers-on-every-tool-call-33c9

---

Have you ever told Claude Code to do something and it just didn't? You said format the code — it didn't. You said don't touch that file — it did. You said run tests before finishing — it forgot.

Hooks are automatic actions that fire every time Claude edits a file, runs a command, or finishes a task. Set them up once. They work in the background forever.

## What Hooks Are

Hooks are scripts that run automatically before or after Claude Code tool calls. They live in `.claude/settings.json` (project-level) or `~/.claude/settings.json` (global).

Two trigger points:
- **PreToolUse** — Runs before an action. Can block it (exit code 2).
- **PostToolUse** — Runs after an action. Cannot block, only observe.

### Exit Code Protocol

| Code | Meaning |
|------|---------|
| 0 | Allow / success |
| 2 | Block execution (PreToolUse only) |
| Other | Warning (logged, continues) |

### Configuration Structure

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{
          "type": "command",
          "command": "python .claude/hooks/guard.py"
        }]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [{
          "type": "command",
          "command": "python .claude/hooks/format.py",
          "timeout": 30
        }]
      }
    ]
  }
}
```

## The 8 Hooks

### 1. Auto-Format Code

Prettier runs automatically after every file write or edit. Language-aware — uses `ruff` for Python, `gofmt` for Go, `rustfmt` for Rust.

```python
# .claude/hooks/format.py
import json, subprocess, sys
from pathlib import Path

data = json.load(sys.stdin)
file_path = data.get("tool_input", {}).get("file_path", "")
if not file_path:
    sys.exit(0)

p = Path(file_path)
FORMATTERS = {
    ".py":  ["ruff", "format", "--quiet"],
    ".ts":  ["npx", "prettier", "--write"],
    ".tsx": ["npx", "prettier", "--write"],
    ".js":  ["npx", "prettier", "--write"],
    ".go":  ["gofmt", "-w"],
    ".rs":  ["rustfmt"],
}

fmt = FORMATTERS.get(p.suffix)
if fmt:
    subprocess.run([*fmt, str(p)], capture_output=True)

sys.exit(0)
```

### 2. Block Dangerous Commands

Stop destructive actions before they execute — `rm -rf /`, `DROP DATABASE`, `git push --force origin main`.

```python
# .claude/hooks/guard.py
import json, sys

data = json.load(sys.stdin)
command = data.get("tool_input", {}).get("command", "")

BLOCKED = [
    "rm -rf /",
    "rm -rf ~",
    "DROP DATABASE",
    "git push --force origin main",
    "git push --force origin master",
]

for pattern in BLOCKED:
    if pattern in command:
        print(f"[BLOCKED] {pattern}", file=sys.stderr)
        sys.exit(2)

sys.exit(0)
```

### 3. Protect Sensitive Files

Prevent accidental modification of `.env`, `package-lock.json`, or anything in `.git/`. Check the target file path against a list of protected patterns and exit with code 2 to block the edit.

### 4. Run Tests on Edit

Tests run automatically after every source code change. If they fail, Claude sees the output immediately and can fix the issue.

```python
# .claude/hooks/test_runner.py
import json, subprocess, sys
from pathlib import Path

data = json.load(sys.stdin)
file_path = data.get("tool_input", {}).get("file_path", "")
if not file_path:
    sys.exit(0)

p = Path(file_path)
if "src" not in p.parts or p.suffix != ".py" or p.name.startswith("test_"):
    sys.exit(0)

test_file = Path("tests") / f"test_{p.name}"
if not test_file.exists():
    sys.exit(0)

result = subprocess.run(
    ["pytest", str(test_file), "-q", "--no-header", "--tb=short"],
    capture_output=True, text=True, timeout=60
)
print(result.stdout)
if result.returncode != 0:
    print(result.stderr, file=sys.stderr)

sys.exit(0)
```

### 5. Block PR Creation Unless Tests Pass

PreToolUse hook on the Bash tool that detects `gh pr create` commands and runs the full test suite first. Exits with code 2 if tests fail.

### 6. Scan for Leaked Secrets

Catch API keys before they reach git — Anthropic, AWS, GitHub, Stripe, OpenAI patterns detected via regex.

```python
# .claude/hooks/scan_secrets.py
import json, re, sys
from pathlib import Path

data = json.load(sys.stdin)
file_path = data.get("tool_input", {}).get("file_path", "")
if not file_path:
    sys.exit(0)

PATTERNS = {
    "Anthropic": r"sk-ant-api\d{2}-[a-zA-Z0-9_-]{86}",
    "AWS":       r"AKIA[0-9A-Z]{16}",
    "GitHub":    r"ghp_[a-zA-Z0-9]{36}",
    "Stripe":    r"sk_(live|test)_[a-zA-Z0-9]{24}",
    "OpenAI":    r"sk-[a-zA-Z0-9]{48}",
}

EXCLUDES = [r"YOUR_KEY", r"REPLACE_ME", r"example", r"xxxx", r"test_"]

try:
    content = Path(file_path).read_text(errors="replace")
except Exception:
    sys.exit(0)

for name, pattern in PATTERNS.items():
    for match in re.findall(pattern, content):
        if not any(re.search(ex, match, re.I) for ex in EXCLUDES):
            print(f"[SECRET WARNING] {name} key detected: {match[:20]}...",
                  file=sys.stderr)

sys.exit(0)
```

### 7. Lint After Every Edit

Run ESLint, Ruff, or language-specific linters as a PostToolUse hook on Write/Edit.

### 8. Log All Tool Calls

Append every tool invocation to a local log file for audit trail and debugging.

## Environment Variables Available to Hooks

| Variable | Description |
|----------|-------------|
| `CLAUDE_PROJECT_DIR` | Project root directory |
| `CLAUDE_TOOL_NAME` | Tool being executed |
| `CLAUDE_TOOL_INPUT_FILE_PATH` | File path (Write/Edit tools) |
| `CLAUDE_TOOL_INPUT_COMMAND` | Shell command (Bash tool) |

## Three Rules for Reliable Hooks

1. **Always set `timeout`** — Infinite loops freeze Claude Code
2. **Catch exceptions** — Hook bugs shouldn't block workflows
3. **Keep hooks fast** — Formatters under 1 second; avoid full builds

## Connection to dot-agents

This article demonstrates exactly the kind of hook management that dot-agents already handles via `dot-agents hooks`. The [agent-as-operator research](../AGENT_AS_OPERATOR_RESEARCH.md) identifies hooks as the primary mechanism for the orient/persist cycle — session start hooks for orientation, post-tool hooks for quality enforcement, session end hooks for state persistence.

The challenge these hooks highlight is portability: configuring 8 hooks per project is tedious. dot-agents solves this by managing hooks centrally in `~/.agents/hooks/` and distributing them to each project's `.claude/settings.json` (or `.codex/hooks.json` for Codex) via the refresh/install commands. Write the hook once, apply it everywhere.

The [Hermes supervisor pattern](openclaw-hermes-supervisor-pattern.md) takes this further — instead of static hooks, a supervisor agent watches the work agent's output and escalates only when something needs human judgment. Hooks handle the deterministic checks; supervisors handle the judgment calls.
