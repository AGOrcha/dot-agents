#!/bin/bash
# dot-agents/lib/platforms/github-copilot.sh
# GitHub Copilot detection, version, and linking

copilot_detect_extension() {
    get_version (){
        if [ -d "$1" ]; then
           basename $1 | rev | cut -d'-' -f2- | rev
        else
           echo ""
        fi
    }
    if [ -d "$HOME/.vscode/extensions" ]; then
       get_version $(find "$HOME/.vscode/extensions" -maxdepth 1 -type d -name "*copilot*" 2>/dev/null | head -n 1)
    elif [ -d "$HOME/.vscode-insiders/extensions" ]; then
       get_version $(find "$HOME/.vscode-insiders/extensions" -maxdepth 1 -type d -name "*copilot*" 2>/dev/null | head -n 1)
    elif [ -d "$HOME/.vscode-server/extensions" ]; then
       get_version $(find "$HOME/.vscode-server/extensions" -maxdepth 1 -type d -name "*copilot*" 2>/dev/null | head -n 1)
    elif [ -d "$HOME/.vscode-server-insiders/extensions" ]; then
       get_version $(find "$HOME/.vscode-server-insiders/extensions" -maxdepth 1 -type d -name "*copilot*" 2>/dev/null | head -n 1)
    fi
}

copilot_detect_cli() {
  if command -v copilot >/dev/null 2>&1; then
    copilot --version 2>/dev/null | head -1
  fi
}

# Check if GitHub Copilot is available.
copilot_is_installed() {
  local ext
  ext=$(copilot_detect_extension)
  [ -n "$ext" ] || command -v copilot >/dev/null 2>&1
}

# Version/info string for doctor output.
copilot_version() {
  local ext_version cli_version

  ext_version=$(copilot_detect_extension)
  cli_version=$(copilot_detect_cli)

  if [ -n "$ext_version" ] && [ -n "$cli_version" ]; then
    echo "$ext_version, CLI: $cli_version"
  elif [ -n "$ext_version" ]; then
    echo "$ext_version (Extension)"
  elif [ -n "$cli_version" ]; then
    echo "CLI: $cli_version"
  fi

}

# Check for deprecated formats (none currently)
copilot_has_deprecated_format() {
  local repo_path="$1"
  return 1
}

# Get deprecated format details
copilot_deprecated_details() {
  local repo_path="$1"
  echo ""
}

# Helper to find the first matching rules file with supported extension.
_copilot_find_rule_file() {
  local base_path="$1"
  for ext in md mdc txt; do
    if [ -f "${base_path}.$ext" ]; then
      echo "${base_path}.$ext"
      return 0
    fi
  done
  echo ""
}

# Resolve source for .github/copilot-instructions.md
# Priority:
# 1. ~/.agents/rules/{project}/copilot-instructions.md
# 2. ~/.agents/rules/global/copilot-instructions.md
# 3. ~/.agents/rules/{project}/rules.(md|mdc|txt)
# 4. ~/.agents/rules/global/rules.(md|mdc|txt)
copilot_resolve_instructions_source() {
  local project="$1"

  if [ -f "$AGENTS_HOME/rules/$project/copilot-instructions.md" ]; then
    echo "$AGENTS_HOME/rules/$project/copilot-instructions.md"
    return 0
  fi

  if [ -f "$AGENTS_HOME/rules/global/copilot-instructions.md" ]; then
    echo "$AGENTS_HOME/rules/global/copilot-instructions.md"
    return 0
  fi

  local source
  source=$(_copilot_find_rule_file "$AGENTS_HOME/rules/$project/rules")
  if [ -n "$source" ]; then
    echo "$source"
    return 0
  fi

  source=$(_copilot_find_rule_file "$AGENTS_HOME/rules/global/rules")
  echo "$source"
}

# Resolve source for workspace MCP config used by Copilot.
# Priority:
# 1. ~/.agents/mcp/{project}/copilot.json
# 2. ~/.agents/mcp/{project}/mcp.json
# 3. ~/.agents/mcp/global/copilot.json
# 4. ~/.agents/mcp/global/mcp.json
copilot_resolve_mcp_source() {
  local project="$1"

  if [ -f "$AGENTS_HOME/mcp/$project/copilot.json" ]; then
    echo "$AGENTS_HOME/mcp/$project/copilot.json"
    return 0
  fi
  if [ -f "$AGENTS_HOME/mcp/$project/mcp.json" ]; then
    echo "$AGENTS_HOME/mcp/$project/mcp.json"
    return 0
  fi
  if [ -f "$AGENTS_HOME/mcp/global/copilot.json" ]; then
    echo "$AGENTS_HOME/mcp/global/copilot.json"
    return 0
  fi
  if [ -f "$AGENTS_HOME/mcp/global/mcp.json" ]; then
    echo "$AGENTS_HOME/mcp/global/mcp.json"
    return 0
  fi

  echo ""
}

# Resolve source for hooks config that Copilot can load via Claude-compatible settings.
# Priority:
# 1. ~/.agents/settings/{project}/claude-code.json
# 2. ~/.agents/settings/global/claude-code.json
copilot_resolve_hooks_source() {
  local project="$1"

  if [ -f "$AGENTS_HOME/settings/$project/claude-code.json" ]; then
    echo "$AGENTS_HOME/settings/$project/claude-code.json"
    return 0
  fi
  if [ -f "$AGENTS_HOME/settings/global/claude-code.json" ]; then
    echo "$AGENTS_HOME/settings/global/claude-code.json"
    return 0
  fi

  echo ""
}

# Create instructions link for Copilot
copilot_create_instructions_link() {
  local project="$1"
  local repo_path="$2"
  local source
  source=$(copilot_resolve_instructions_source "$project")

  if [ -z "$source" ]; then
    log_debug "copilot: no instructions source for project '$project'"
    return 0
  fi

  mkdir -p "$repo_path/.github"
  log_debug "copilot: linking instructions $repo_path/.github/copilot-instructions.md -> $source"
  ln -sf "$source" "$repo_path/.github/copilot-instructions.md"
}

# Create project agent symlinks for Copilot (.github/agents/{name}.agent.md -> AGENT.md)
copilot_create_agents_links() {
  local project="$1"
  local repo_path="$2"

  local agents_target="$repo_path/.github/agents"
  local project_agents="$AGENTS_HOME/agents/$project"

  log_debug "copilot: creating project agent links for '$project' in $agents_target"

  mkdir -p "$agents_target"
  log_debug "copilot: removing existing .agent.md entries in $agents_target"
  rm -f "$agents_target"/*.agent.md 2>/dev/null || true

  if [ -d "$project_agents" ]; then
    for agent_dir in "$project_agents"/*/; do
      [ -d "$agent_dir" ] || continue
      [ -f "$agent_dir/AGENT.md" ] || continue
      local name
      name=$(basename "$agent_dir")
      local target="$agents_target/$name.agent.md"
      if [ -e "$target" ] || [ -L "$target" ]; then
        log_debug "copilot: project agent target already exists, leaving as-is: $target"
      else
        log_debug "copilot: linking project agent $target -> ${agent_dir%/}/AGENT.md"
        ln -sf "${agent_dir%/}/AGENT.md" "$target"
      fi
    done
  else
    log_debug "copilot: no project agents directory at $project_agents"
  fi
}

# Create shared project skills mirror for agent ecosystems (.agents/skills/{name} -> skill dir)
copilot_create_shared_skills_links() {
  local project="$1"
  local repo_path="$2"

  local skills_target="$repo_path/.agents/skills"
  local project_skills="$AGENTS_HOME/skills/$project"

  mkdir -p "$skills_target"
  rm -f "$skills_target"/* 2>/dev/null || true

  if [ -d "$project_skills" ]; then
    for skill_dir in "$project_skills"/*/; do
      [ -d "$skill_dir" ] || continue
      [ -f "$skill_dir/SKILL.md" ] || continue
      local name
      name=$(basename "$skill_dir")
      local target="$skills_target/$name"
      [ -e "$target" ] || [ -L "$target" ] || ln -sf "$skill_dir" "$target"
    done
  fi
}

# Create workspace MCP link for Copilot (.vscode/mcp.json)
copilot_create_mcp_links() {
  local project="$1"
  local repo_path="$2"
  local source
  source=$(copilot_resolve_mcp_source "$project")

  if [ -z "$source" ]; then
    log_debug "copilot: no MCP source for project '$project'"
    return 0
  fi

  mkdir -p "$repo_path/.vscode"
  log_debug "copilot: linking MCP $repo_path/.vscode/mcp.json -> $source"
  ln -sf "$source" "$repo_path/.vscode/mcp.json"
}

# Create hooks-compatible settings link for Copilot (.claude/settings.local.json)
copilot_create_hooks_links() {
  local project="$1"
  local repo_path="$2"
  local source
  source=$(copilot_resolve_hooks_source "$project")

  if [ -z "$source" ]; then
    log_debug "copilot: no hooks source for project '$project'"
    return 0
  fi

  mkdir -p "$repo_path/.claude"
  log_debug "copilot: linking hooks $repo_path/.claude/settings.local.json -> $source"
  ln -sf "$source" "$repo_path/.claude/settings.local.json"
}

# Create all Copilot links (instructions, skills, agents)
copilot_create_links() {
  local project="$1"
  local repo_path="$2"

  log_debug "copilot: start linking for project '$project' repo '$repo_path'"

  copilot_create_instructions_link "$project" "$repo_path"
  copilot_create_shared_skills_links "$project" "$repo_path"
  copilot_create_agents_links "$project" "$repo_path"
  copilot_create_mcp_links "$project" "$repo_path"
  copilot_create_hooks_links "$project" "$repo_path"

  log_debug "copilot: finished linking for project '$project'"
}
