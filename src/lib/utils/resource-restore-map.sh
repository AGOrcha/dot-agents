#!/bin/bash
# dot-agents/lib/utils/resource-restore-map.sh
# Mapping helpers for restoring active resource backups into ~/.agents structure.

# Map a resource-relative path to an ~/.agents-relative destination path.
# Usage: dot_agents_map_resource_rel_to_agents_dest <project> <resource-relative-path>
# Returns destination relative path or empty string if unsupported.
dot_agents_map_resource_rel_to_agents_dest() {
  local project="$1"
  local rel="$2"

  case "$rel" in
    ".cursor/settings.json") echo "settings/$project/cursor.json" ;;
    ".cursor/mcp.json") echo "mcp/$project/mcp.json" ;;
    ".cursor/hooks.json") echo "hooks/$project/cursor.json" ;;
    ".cursorignore") echo "settings/$project/cursorignore" ;;
    ".claude/settings.local.json") echo "settings/$project/claude-code.json" ;;
    ".mcp.json") echo "mcp/$project/mcp.json" ;;
    ".vscode/mcp.json") echo "mcp/$project/mcp.json" ;;
    "opencode.json") echo "settings/$project/opencode.json" ;;
    "AGENTS.md") echo "rules/$project/agents.md" ;;
    ".codex/instructions.md") echo "rules/$project/agents.md" ;;
    ".codex/rules.md") echo "rules/$project/agents.md" ;;
    ".codex/config.toml") echo "settings/$project/codex.toml" ;;
    ".github/copilot-instructions.md") echo "rules/$project/copilot-instructions.md" ;;
    ".claude/rules/"*) echo "" ;;
    ".opencode/agent/"*.md)
      local name
      name=$(basename "$rel")
      echo "rules/$project/opencode-$name"
      ;;
    ".github/agents/"*.agent.md)
      local name
      name=$(basename "$rel" .agent.md)
      echo "agents/$project/$name/AGENT.md"
      ;;
    ".codex/agents/"*"/"*)
      local rest="${rel#.codex/agents/}"
      local agent_name="${rest%%/*}"
      local agent_path="${rest#*/}"
      echo "agents/$project/$agent_name/$agent_path"
      ;;
    ".agents/skills/"*"/"*)
      local rest="${rel#.agents/skills/}"
      local skill_name="${rest%%/*}"
      local skill_path="${rest#*/}"
      echo "skills/$project/$skill_name/$skill_path"
      ;;
    ".claude/skills/"*"/"*)
      local rest="${rel#.claude/skills/}"
      local skill_name="${rest%%/*}"
      local skill_path="${rest#*/}"
      echo "skills/$project/$skill_name/$skill_path"
      ;;
    ".github/hooks/"*.json)
      local name
      name=$(basename "$rel")
      echo "hooks/$project/$name"
      ;;
    ".cursor/rules/"*)
      local name
      name=$(basename "$rel")
      if [[ "$name" == "global--"* ]]; then
        echo "rules/global/${name#global--}"
      elif [[ "$name" == "${project}--"* ]]; then
        echo "rules/$project/${name#${project}--}"
      elif [[ "$name" == *.mdc ]] || [[ "$name" == *.md ]]; then
        echo "rules/$project/$name"
      else
        echo ""
      fi
      ;;
    *)
      echo ""
      ;;
  esac
}

# Map a global/home-relative path to an ~/.agents-relative destination path.
# Usage: map_global_rel_to_agents_dest <global-relative-path>
map_global_rel_to_agents_dest() {
  local rel="$1"
  case "$rel" in
    ".claude/settings.json") echo "settings/global/claude-code.json" ;;
    ".cursor/settings.json") echo "settings/global/cursor.json" ;;
    ".cursor/mcp.json") echo "mcp/global/mcp.json" ;;
    ".cursor/hooks.json") echo "hooks/global/cursor.json" ;;
    ".claude/CLAUDE.md") echo "rules/global/agents.md" ;;
    ".codex/config.toml") echo "settings/global/codex.toml" ;;
    *) echo "" ;;
  esac
  return 0
}

# Check whether a platform has an active (non-timestamped) backup in resources.
# Usage: dot_agents_platform_has_active_backup <project-slug> <platform>
dot_agents_platform_has_active_backup() {
  local project_slug="$1"
  local platform="$2"
  local root="$AGENTS_HOME/resources/$project_slug"

  [ -d "$root" ] || return 1

  case "$platform" in
    cursor)
      [ -e "$root/.cursor/rules" ] || [ -e "$root/.cursor/agents" ] || [ -e "$root/.cursor/settings.json" ] || [ -e "$root/.cursor/mcp.json" ] || [ -e "$root/.cursorignore" ]
      ;;
    claude)
      [ -e "$root/.claude/rules" ] || [ -e "$root/.claude/skills" ] || [ -e "$root/.claude/agents" ] || [ -e "$root/.claude/settings.local.json" ] || [ -e "$root/.mcp.json" ]
      ;;
    codex)
      [ -e "$root/AGENTS.md" ] || [ -e "$root/AGENTS.md.dot-agents-backup" ] || [ -e "$root/.codex/agents" ] || [ -e "$root/.codex/config.toml" ] || [ -e "$root/.codex/config.toml.dot-agents-backup" ] || [ -e "$root/.codex/instructions.md" ] || [ -e "$root/.codex/instructions.md.dot-agents-backup" ] || [ -e "$root/.agents/skills" ]
      ;;
    opencode)
      [ -e "$root/opencode.json" ] || [ -e "$root/.opencode/agent" ] || [ -e "$root/.opencode/config.json" ] || [ -e "$root/.opencode/instructions.md" ]
      ;;
    copilot)
      [ -e "$root/.github/copilot-instructions.md" ] || [ -e "$root/.github/agents" ] || [ -e "$root/.vscode/mcp.json" ] || [ -e "$root/.claude/settings.local.json" ] || [ -e "$root/.agents/skills" ]
      ;;
    *)
      return 1
      ;;
  esac
}

export -f dot_agents_map_resource_rel_to_agents_dest
export -f map_global_rel_to_agents_dest
export -f dot_agents_platform_has_active_backup
