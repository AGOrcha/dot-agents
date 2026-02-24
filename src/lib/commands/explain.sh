#!/bin/bash
# dot-agents/lib/commands/explain.sh
# Self-documenting system descriptions for AI agents

cmd_explain_help() {
  cat << EOF
${BOLD}dot-agents explain${NC} - Self-documenting system descriptions

${BOLD}USAGE${NC}
    dot-agents explain [topic]
    dot-agents explain --agent <name>

${BOLD}TOPICS${NC}
    (none)          Overview of ~/.agents structure
    rules           How rules work
    hooks           How Claude Code hooks work
    scripts         How scripts work
    settings        How settings work
    mcp             How MCP configs work
    skills          How skills (slash commands) work
    agents          How subagents work (~/.agents/agents/)
    config          What config.json fields mean
    symlinks        How symlinks and hard links work
    platforms       Supported AI agent platforms

${BOLD}OPTIONS${NC}
    --agent <name>  Explain config patterns for a specific agent
                    (cursor, claude-code, codex, opencode, github-copilot)
    --verbose, -v   Show detailed information
    --help, -h      Show this help

${BOLD}DESCRIPTION${NC}
    Provides self-documenting descriptions of the dot-agents system.
    Useful for AI agents that need to understand the configuration
    structure and how to interact with it.

${BOLD}EXAMPLES${NC}
    dot-agents explain              # Overview of the system
    dot-agents explain rules        # How rules work
    dot-agents explain --agent cursor    # Cursor-specific info

EOF
}

cmd_explain() {
  local topic=""
  local agent_name=""

  # Parse flags
  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    case $1 in
      --agent)
        agent_name="$2"
        shift 2
        ;;
      --verbose|-v)
        VERBOSE=true
        shift
        ;;
      --help|-h)
        cmd_explain_help
        return 0
        ;;
      -*)
        log_error "Unknown option: $1"
        return 1
        ;;
      *)
        REMAINING_ARGS+=("$1")
        shift
        ;;
    esac
  done

  # Get topic from remaining args
  if [ ${#REMAINING_ARGS[@]} -gt 0 ]; then
    topic="${REMAINING_ARGS[0]}"
  fi

  # Handle agent-specific explanation
  if [ -n "$agent_name" ]; then
    explain_agent "$agent_name"
    return $?
  fi

  # Route to topic
  case "$topic" in
    ""|overview)
      explain_overview
      ;;
    rules)
      explain_rules
      ;;
    hooks)
      explain_hooks
      ;;
    scripts)
      explain_scripts
      ;;
    settings)
      explain_settings
      ;;
    mcp)
      explain_mcp
      ;;
    skills|commands)
      explain_skills
      ;;
    agents|subagents)
      explain_agents
      ;;
    config|config.json)
      explain_config
      ;;
    symlinks|links)
      explain_symlinks
      ;;
    platforms)
      explain_platforms
      ;;
    *)
      log_error "Unknown topic: $topic"
      echo ""
      echo "Available topics: rules, hooks, scripts, settings, mcp, skills, agents, config, symlinks, platforms"
      return 1
      ;;
  esac
}

explain_overview() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 dot-agents Overview
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WHAT IS DOT-AGENTS?
  A unified configuration layer for AI coding agents.
  One ~/.agents/ directory manages configs for Cursor, Claude Code,
  Codex, OpenCode, and GitHub Copilot - all from a single,
  git-trackable location.

DIRECTORY STRUCTURE:
  ~/.agents/
  ├── config.json          # Project registry and settings
  ├── rules/               # Agent instructions
  │   ├── global/          # Apply to ALL projects
  │   └── {project}/       # Project-specific rules
  ├── scripts/             # Helper scripts
  │   ├── global/
  │   └── {project}/
  ├── skills/              # Slash commands (directory-based)
  │   ├── global/          # Available everywhere
  │   │   └── {skill}/SKILL.md
  │   └── {project}/
  ├── settings/            # Native agent configs
  │   ├── global/
  │   └── {project}/
  ├── mcp/                 # MCP server configs
  │   ├── global/
  │   └── {project}/
  └── local/               # Machine-specific (gitignored)

HOW IT WORKS:
  1. Configs live in ~/.agents/ (single source of truth)
  2. Symlinks/hard links connect to project directories
  3. Edit in one place → changes apply everywhere
  4. Git-track ~/.agents/ for sync across machines

KEY COMMANDS:
  dot-agents init               # Set up ~/.agents/
  dot-agents add <path>         # Register a project
  dot-agents status             # See all projects
  dot-agents status --audit     # See applied configs
  dot-agents doctor             # Check for issues
  dot-agents skills             # Manage skills

For more details, run:
  dot-agents explain <topic>    # rules, settings, mcp, etc.
  dot-agents explain --agent <name>  # cursor, claude-code, etc.

EOF
}

explain_rules() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Understanding: Rules
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WHAT ARE RULES?
  Rules are instruction files that tell AI agents how to behave.
  They're written in Markdown (.mdc or .md) with optional YAML frontmatter.

WHERE DO THEY LIVE?
  ~/.agents/rules/global/    → Apply to ALL projects
  ~/.agents/rules/{project}/ → Apply to specific project only

FILE NAMING:
  rules.mdc         → Applies to ALL agents
  cursor.mdc        → Applies to Cursor only
  claude-code.mdc   → Applies to Claude Code only
  codex.md          → Applies to Codex only

FRONTMATTER OPTIONS:
  ---
  alwaysApply: true    # Always include in context
  globs: ["*.py"]      # Only apply to matching files
  ---

HOW THEY GET TO REPOS:
  Cursor:      Hard links to .cursor/rules/*.mdc
  Claude Code: Symlinks to .claude/rules/*.md
  Codex:       Symlink AGENTS.md → ~/.agents/rules/...

EXAMPLE:
  ~/.agents/rules/global/rules.mdc
  ─────────────────────────────────
  ---
  alwaysApply: true
  ---

  # Coding Standards
  - Write clear, readable code
  - Add comments for complex logic
  - Follow existing patterns

COMMANDS:
  dot-agents status --audit              # See which rules are applied
  dot-agents status --audit --agent cursor  # Cursor rules only

EOF
}

explain_hooks() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Understanding: Hooks (Claude Code)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WHAT ARE HOOKS?
  Hooks are shell commands that execute in response to Claude Code
  events. They let you customize behavior, add logging, run linters,
  send notifications, and more.

WHERE DO THEY LIVE?
  Global:   ~/.agents/settings/global/claude-code.json
  Project:  ~/.agents/settings/{project}/claude-code.json

ALL 12 HOOK TYPES:

  Tool Hooks (run around tool execution):
    PreToolUse          Before executing any tool
    PostToolUse         After tool execution completes
    PostToolUseFailure  After tool execution fails

  Session Hooks (run around session lifecycle):
    SessionStart        When a new session is started
    SessionEnd          When a session is ending
    Stop                Right before Claude concludes its response

  User Interaction Hooks:
    UserPromptSubmit    When the user submits a prompt
    Notification        When notifications are sent
    PermissionRequest   When a permission dialog is displayed

  Subagent Hooks (Task tool):
    SubagentStart       When a subagent is started
    SubagentStop        When a subagent concludes its response

  Context Hooks:
    PreCompact          Before conversation compaction

ENVIRONMENT VARIABLES:
  All hooks receive:
    $SESSION_ID         Current Claude session ID
    $TRANSCRIPT_PATH    Path to conversation transcript

  Tool hooks (PreToolUse, PostToolUse, PostToolUseFailure):
    $TOOL_NAME          Name of the tool being used
    $TOOL_INPUT         The tool's input/arguments
    $TOOL_OUTPUT        The tool's output (PostToolUse* only)

  UserPromptSubmit:
    $USER_PROMPT        The prompt text submitted

  Subagent hooks:
    $SUBAGENT_ID        The subagent identifier

  PreCompact:
    $SUMMARY_PATH       Path to the compaction summary

HOOK FORMAT (in claude-code.json):
  {
    "hooks": {
      "PreToolUse": [
        {
          "matcher": "Bash",
          "hooks": [
            {
              "type": "command",
              "command": "echo \"$TOOL_INPUT\" >> log.txt"
            }
          ]
        }
      ]
    }
  }

MATCHERS (for tool hooks):
  "*"               Match all tools
  "Bash"            Match Bash tool only
  "Edit"            Match Edit tool only
  "Bash(git:*)"     Match git commands
  "Bash(npm:*)"     Match npm commands

COMMANDS:
  dot-agents hooks              # List all hooks
  dot-agents hooks add <type>   # Add a hook
  dot-agents hooks remove       # Remove a hook
  dot-agents hooks edit         # Edit hooks in $EDITOR
  dot-agents hooks examples     # Show example hooks

EOF
}

explain_scripts() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Understanding: Scripts
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WHAT ARE SCRIPTS?
  Helper scripts that can be called by AI agents or manually.
  They automate common tasks like building, testing, or deploying.

WHERE DO THEY LIVE?
  ~/.agents/scripts/global/    → Available to ALL projects
  ~/.agents/scripts/{project}/ → Project-specific scripts

FILE NAMING:
  Use descriptive names: build.sh, test.sh, deploy.sh, format.sh

EXAMPLE:
  ~/.agents/scripts/global/run-tests.sh
  ─────────────────────────────────────
  #!/bin/bash
  # Run project tests with coverage
  npm test -- --coverage

USAGE:
  Scripts are typically symlinked to project directories or
  called directly by AI agents that understand the structure.

BEST PRACTICES:
  - Make scripts executable: chmod +x script.sh
  - Include usage comments at the top
  - Use set -euo pipefail for safety
  - Keep global scripts generic

EOF
}

explain_settings() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Understanding: Settings
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WHAT ARE SETTINGS?
  Native configuration files for each AI agent platform.
  These are JSON/YAML files that control agent behavior beyond rules.

WHERE DO THEY LIVE?
  ~/.agents/settings/global/    → Apply to ALL projects
  ~/.agents/settings/{project}/ → Project-specific settings

EXAMPLES BY PLATFORM:

  Claude Code:
    ~/.agents/settings/global/claude-settings.json
    (becomes .claude/settings.json in projects)

  Cursor:
    ~/.agents/settings/global/cursor-settings.json
    (becomes .cursor/settings.json in projects)

HOW THEY GET TO REPOS:
  Symlinks connect ~/.agents/settings/ to project directories.

EXAMPLE:
  ~/.agents/settings/global/claude-settings.json
  ─────────────────────────────────────────────
  {
    "permissions": {
      "allow_bash": true,
      "allow_file_write": true
    },
    "model": "opus-4.5"
  }

COMMANDS:
  dot-agents status --audit --agent claude-code  # See Claude settings

EOF
}

explain_mcp() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Understanding: MCP (Model Context Protocol)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WHAT IS MCP?
  Model Context Protocol - a standard for connecting AI agents
  to external tools and services (databases, APIs, etc.).

WHERE DO MCP CONFIGS LIVE?
  ~/.agents/mcp/global/    → Apply to ALL projects
  ~/.agents/mcp/{project}/ → Project-specific MCP servers

HOW THEY GET TO REPOS:
  Claude Code: .mcp.json symlink
  Cursor:      .cursor/mcp.json symlink

EXAMPLE:
  ~/.agents/mcp/global/mcp.json
  ─────────────────────────────
  {
    "servers": {
      "filesystem": {
        "command": "npx",
        "args": ["-y", "@anthropic/mcp-server-filesystem"]
      },
      "postgres": {
        "command": "npx",
        "args": ["-y", "@anthropic/mcp-server-postgres"],
        "env": {
          "DATABASE_URL": "postgres://..."
        }
      }
    }
  }

COMMON MCP SERVERS:
  - @anthropic/mcp-server-filesystem
  - @anthropic/mcp-server-postgres
  - @anthropic/mcp-server-github
  - @anthropic/mcp-server-linear

COMMANDS:
  dot-agents status --audit     # Shows MCP status

EOF
}

explain_skills() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Understanding: Skills (Slash Commands)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WHAT ARE SKILLS?
  Directory-based slash commands that extend AI agent capabilities.
  Each skill is a folder containing a SKILL.md file.
  Skills become available as /skill-name in your AI agent.

WHERE DO THEY LIVE?
  ~/.agents/skills/global/         → Available to ALL projects
  ~/.agents/skills/{project}/      → Project-specific skills

DIRECTORY STRUCTURE:
  ~/.agents/skills/global/
  ├── commit/
  │   └── SKILL.md
  ├── pr/
  │   └── SKILL.md
  └── deploy/
      └── SKILL.md

HOW THEY GET TO REPOS (via dot-agents add):
  Claude Code: .claude/skills/{name}/ → symlink to skill directory
  Cursor:      .cursor/commands/{name}.md → symlink to SKILL.md
  Codex:       .codex/skills/{name}/ → symlink to skill directory

EXAMPLE:
  ~/.agents/skills/global/commit/SKILL.md
  ────────────────────────────────────────
  # /commit - Create a git commit

  Create a well-formed git commit with the following steps:
  1. Check git status for staged changes
  2. Generate a descriptive commit message
  3. Run: git commit -m "<message>"

  Commit message format:
  - feat: new feature
  - fix: bug fix
  - docs: documentation
  - refactor: code cleanup

COMMANDS:
  dot-agents skills             # List all skills
  dot-agents skills new <name>  # Create new skill
  dot-agents skills edit <name> # Edit skill in $EDITOR
  dot-agents skills show <name> # Show skill content

USAGE IN AGENT:
  Just type /commit and the agent will follow the instructions.

EOF
}

explain_agents() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Understanding: Subagents
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WHAT ARE SUBAGENTS?
  Directory-based agent definitions under ~/.agents/agents/.
  Same layout as skills: global and per-project, each with AGENT.md.
  They sync with your config when you use 'dot-agents sync' (git).

WHERE DO THEY LIVE?
  ~/.agents/agents/global/         → Available globally
  ~/.agents/agents/{project}/       → Project-specific subagents

DIRECTORY STRUCTURE:
  ~/.agents/agents/global/
  ├── reviewer/
  │   ├── AGENT.md
  │   └── scripts/
          ├── ...
  │       └── deep-web-research.sh
  │  
  └── deployer/
      └── AGENT.md

SYNC:
  Like skills, everything under ~/.agents/ is version-controlled.
  dot-agents sync init / status / commit / push / pull includes agents/.

COMMANDS:
  dot-agents agents               # List all subagents
  dot-agents agents new <name>    # Create new subagent
  dot-agents agents edit <name>   # Edit in $EDITOR
  dot-agents agents show <name>   # Show contents
  dot-agents agents validate <name>  # Check frontmatter

EOF
}

explain_config() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Understanding: config.json
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WHAT IS config.json?
  The central registry for dot-agents. Contains:
  - Registered projects and their paths
  - User preferences

LOCATION:
  ~/.agents/config.json

STRUCTURE:
  {
    "projects": {
      "myproject": {
        "path": "~/Github/myproject"
      }
    },
    "agents": {
      "active": ["cursor", "claude-code"]
    }
  }

KEY FIELDS:

  projects.<name>.path
    The filesystem path to the project directory.
    Use ~ for home directory (portable across machines).

  agents.active
    List of AI agents you use. Used for status and doctor commands.

MODIFICATION:
  Projects are managed via CLI:
    dot-agents add ~/path/to/project
    dot-agents remove myproject

  You CAN edit this file directly, but CLI commands are safer.

EOF
}

explain_symlinks() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Understanding: Symlinks & Hard Links
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

WHY TWO TYPES?
  Cursor doesn't follow symlinks for .cursor/rules/*.mdc files.
  Other agents (Claude Code, Codex) work fine with symlinks.

WHAT DOT-AGENTS CREATES:

  Hard Links (Cursor only):
    .cursor/rules/global--rules.mdc  →→  ~/.agents/rules/global/rules.mdc
    .cursor/rules/project--rules.mdc →→  ~/.agents/rules/{project}/rules.mdc

    Hard links share the same file content. Changes in either
    location affect the other immediately.

  Symlinks (Other agents):
    .claude/rules/*.md  →  ~/.agents/rules/global/ and ~/.agents/rules/{project}/
    AGENTS.md           →  ~/.agents/rules/{project}/AGENTS.md
    .mcp.json           →  ~/.agents/mcp/{project}/mcp.json

    Symlinks are pointers. The actual file lives in ~/.agents/.

NAMING CONVENTION:
  Hard links in .cursor/rules/ are prefixed:
    global--<filename>     From ~/.agents/rules/global/
    {project}--<filename>  From ~/.agents/rules/{project}/

VERIFICATION:
  dot-agents doctor            # Checks for broken links
  dot-agents status --audit    # Shows all applied links

REPAIR:
  dot-agents add <path> --force  # Recreate links

EOF
}

explain_platforms() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Supported Platforms
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

CURSOR
  IDE and CLI for AI-assisted coding.
  Config locations:
    .cursor/rules/*.mdc    - Rules (hard links required)
    .cursor/mcp.json       - MCP servers
    .cursor/settings.json  - IDE settings
    .cursorignore          - File exclusions
  Legacy (deprecated):
    .cursorrules           - Use .cursor/rules/ instead

CLAUDE CODE
  Anthropic's CLI for Claude.
  Config locations:
    .claude/rules/*.md         - Rule files (auto-loaded)
    .claude/settings.local.json - Settings
    .mcp.json                   - MCP servers
    .claude/skills/*/SKILL.md   - Skills (slash commands)

CODEX (OpenAI)
  OpenAI's CLI for GPT models.
  Config locations:
    AGENTS.md              - Project instructions
    .codex/config.toml     - Settings

OPENCODE
  Open-source AI coding assistant.
  Config locations:
    opencode.json          - Project config
    .opencode/agent/*.md   - Agent definitions

GITHUB COPILOT
  GitHub Copilot project instructions.
  Config locations:
    .github/copilot-instructions.md  - Repository instructions
    .github/skills/*/SKILL.md        - Project skills
    .github/agents/*.agent.md        - Project custom agents

DETECTION:
  dot-agents doctor            # Shows installed agents
  dot-agents status --json     # Agent info in JSON

For platform-specific details:
  dot-agents explain --agent cursor
  dot-agents explain --agent claude-code

EOF
}

explain_agent() {
  local agent="$1"

  case "$agent" in
    cursor)
      explain_agent_cursor
      ;;
    claude-code|claude)
      explain_agent_claude
      ;;
    codex)
      explain_agent_codex
      ;;
    opencode)
      explain_agent_opencode
      ;;
    github-copilot|copilot)
      explain_agent_copilot
      ;;
    *)
      log_error "Unknown agent: $agent"
      echo ""
      echo "Available agents: cursor, claude-code, codex, opencode, github-copilot"
      return 1
      ;;
  esac
}

explain_agent_cursor() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Cursor Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ABOUT:
  Cursor is an AI-first code editor built on VS Code.
  It uses HARD LINKS for rules (doesn't follow symlinks).

CONFIG FILES:

  .cursor/rules/*.mdc
    Rule files with optional frontmatter.
    IMPORTANT: Must be hard links, not symlinks.

    Frontmatter options:
      alwaysApply: true    # Always include
      globs: ["*.ts"]      # Apply to matching files

  .cursor/mcp.json
    MCP server configuration.
    Can be symlink → ~/.agents/mcp/

  .cursor/settings.json
    Editor and AI settings.
    Can be symlink → ~/.agents/settings/

  .cursorignore
    Files to exclude from AI context.
    Syntax matches .gitignore.

DEPRECATED:
  .cursorrules - Single file in project root.
  Migrate with: dot-agents doctor --migrate --fix

DETECTION:
  Cursor App: /Applications/Cursor.app (macOS)
  Cursor CLI: cursor --version

HOW DOT-AGENTS MANAGES IT:
  ~/.agents/rules/global/*.mdc      → .cursor/rules/global--*.mdc (hard link)
  ~/.agents/rules/{project}/*.mdc   → .cursor/rules/{project}--*.mdc (hard link)
  ~/.agents/mcp/{project}/          → .cursor/mcp.json (symlink)

EOF
}

explain_agent_claude() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Claude Code Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ABOUT:
  Claude Code is Anthropic's official CLI for Claude.
  It supports symlinks and has a rich configuration system.

CONFIG FILES:

  .claude/rules/*.md
    Rule files loaded automatically by Claude Code.
    dot-agents creates symlinks for 4-layer rules:
      global--rules.md        (all agents, global)
      global--claude-code.md  (Claude only, global)
      project--rules.md       (all agents, project)
      project--claude-code.md (Claude only, project)

  .claude/settings.local.json
    Project settings (hooks, permissions).
    Can be symlink → ~/.agents/settings/{project}/

  CLAUDE.local.md
    Personal notes (gitignored).
    Not managed by dot-agents.

  .mcp.json
    MCP server configuration.
    Can be symlink → ~/.agents/mcp/

  .claude/skills/*/
    Custom slash commands (directory-based).
    Each skill is a directory with SKILL.md.
    Can be symlink → ~/.agents/skills/

GLOBAL CONFIGS:
  ~/.claude/settings.json  - Global Claude settings (hooks, permissions)

DETECTION:
  claude --version

HOW DOT-AGENTS MANAGES IT:
  ~/.agents/rules/global/rules.mdc        → .claude/rules/global--rules.md
  ~/.agents/rules/global/claude-code.mdc  → .claude/rules/global--claude-code.md
  ~/.agents/rules/{project}/rules.mdc     → .claude/rules/project--rules.md
  ~/.agents/rules/{project}/claude-code.mdc → .claude/rules/project--claude-code.md
  ~/.agents/settings/{project}/           → .claude/settings.local.json
  ~/.agents/mcp/{project}/mcp.json        → .mcp.json
  ~/.agents/skills/*/                     → .claude/skills/

EOF
}

explain_agent_codex() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Codex Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ABOUT:
  Codex is OpenAI's CLI for GPT models.
  Uses AGENTS.md for instructions (OpenAI's standard format).

CONFIG FILES:

  AGENTS.md
    Main instruction file in project root.
    Uses OpenAI's standard agent instruction format.
    Can be symlink → ~/.agents/rules/

  AGENTS.override.md
    Override instructions (higher priority).

  .codex/config.toml
    Project configuration in TOML format.

  ~/.codex/config.toml
    Global Codex configuration.

TOML CONFIG EXAMPLE:
  [model]
  name = "gpt-4o"

  [permissions]
  allow_bash = true
  allow_file_write = true

DETECTION:
  codex --version

HOW DOT-AGENTS MANAGES IT:
  ~/.agents/rules/{project}/AGENTS.md  → AGENTS.md (symlink)
  ~/.agents/settings/{project}/        → .codex/ (symlink)

EOF
}

explain_agent_opencode() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 OpenCode Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ABOUT:
  OpenCode is an open-source AI coding assistant.
  It uses JSON configuration and Markdown agent files.

CONFIG FILES:

  opencode.json
    Project configuration in project root.

  .opencode/agent/*.md
    Agent definition files.

  ~/.config/opencode/opencode.json
    Global OpenCode configuration.

JSON CONFIG EXAMPLE:
  {
    "model": "claude-opus-4-5",
    "agent_dir": ".opencode/agent"
  }

DETECTION:
  opencode --version

HOW DOT-AGENTS MANAGES IT:
  ~/.agents/settings/{project}/opencode.json → opencode.json (symlink)
  ~/.agents/rules/{project}/                 → .opencode/agent/ (symlink)

EOF
}

explain_agent_copilot() {
  cat << 'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 GitHub Copilot Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ABOUT:
  GitHub Copilot supports repository-level instructions via
  .github/copilot-instructions.md.

CONFIG FILES:

  .github/copilot-instructions.md
    Project instructions that guide Copilot behavior in this repo.

  .github/skills/{skill}/SKILL.md
    Project skills loaded by Copilot when relevant.

  .github/agents/{name}.agent.md
    Custom agent definitions for VS Code Copilot chat.

HOW DOT-AGENTS MANAGES IT:
  ~/.agents/rules/{project}/copilot-instructions.md
    → .github/copilot-instructions.md (symlink)

  ~/.agents/skills/{project}/{skill}/
    → .github/skills/{skill}/ (symlink directory)

  ~/.agents/agents/{project}/{agent}/AGENT.md
    → .github/agents/{agent}.agent.md (symlink)

  Fallback order when linking:
    1) ~/.agents/rules/{project}/copilot-instructions.md
    2) ~/.agents/rules/global/copilot-instructions.md
    3) ~/.agents/rules/{project}/rules.(md|mdc|txt)
    4) ~/.agents/rules/global/rules.(md|mdc|txt)

VERIFICATION:
  dot-agents status --audit --agent github-copilot

EOF
}
