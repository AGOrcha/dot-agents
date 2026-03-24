#!/bin/bash
# dot-agents/lib/platforms/claude-code.sh
# Claude Code CLI detection, version, and linking

# Detect Claude Code CLI version
claude_detect() {
  if command -v claude >/dev/null 2>&1; then
    claude --version 2>/dev/null | head -1
  fi
}

# Check if Claude Code is installed
claude_is_installed() {
  command -v claude >/dev/null 2>&1 || [ -d "$HOME/.claude" ]
}

# Get Claude Code version string
claude_version() {
  claude_detect
}

# Helper to find rule file with any extension (.md, .mdc, .txt)
# Returns empty string if not found (always returns 0 for set -e compatibility)
_claude_find_rule_file() {
  local base_path="$1"
  for ext in md mdc txt; do
    if [ -f "${base_path}.$ext" ]; then
      echo "${base_path}.$ext"
      return 0
    fi
  done
  # Return empty string, not error code (for set -e compatibility)
  echo ""
  return 0
}

# Create links for Claude Code (SYMLINKS)
# Claude Code reads rules from .claude/rules/*.md (auto-loaded rule files)
#
# Global rules: ~/.claude/CLAUDE.md → ~/.agents/rules/global/rules.mdc (user-scope)
# Project rules: .claude/rules/{project}--{stem}.md (project-scope)
#
# All files in .claude/rules/ are automatically loaded by Claude Code.
claude_create_links() {
  local project="$1"
  local repo_path="$2"

  claude_ensure_user_agents
  claude_ensure_user_rules
  # Create .claude directory structure
  mkdir -p "$repo_path/.claude/rules"

  # Create symlinks for all 4 rule sources in .claude/rules/
  # Claude Code auto-loads all .md files in this directory
  claude_create_rules_links "$project" "$repo_path"

  # Link settings.local.json if exists
  if [ -f "$AGENTS_HOME/settings/$project/claude-code.json" ]; then
    ln -sf "$AGENTS_HOME/settings/$project/claude-code.json" "$repo_path/.claude/settings.local.json"
  fi

  # Link MCP config if exists
  # Priority: project claude.json, project mcp.json, global claude.json, global mcp.json
  local _mcp_linked=false
  for _scope in "$project" "global"; do
    for _name in "claude.json" "mcp.json"; do
      if [ -f "$AGENTS_HOME/mcp/$_scope/$_name" ]; then
        ln -sf "$AGENTS_HOME/mcp/$_scope/$_name" "$repo_path/.mcp.json"
        _mcp_linked=true
        break 2
      fi
    done
  done
  unset _mcp_linked _scope _name

  # Project agents (global → user-level ~/.claude/agents)
  claude_create_agents_links "$project" "$repo_path"
}

# Create agents symlinks for Claude Code: project agents only (symlink dirs to .claude/agents/)
claude_create_agents_links() {
  local project="$1"
  local repo_path="$2"

  local agents_target="$repo_path/.claude/agents"
  local project_agents="$AGENTS_HOME/agents/$project"

  mkdir -p "$agents_target"
  rm -f "$agents_target"/* 2>/dev/null || true

  if [ -d "$project_agents" ]; then
    for agent_dir in "$project_agents"/*/; do
      [ -d "$agent_dir" ] || continue
      [ -f "$agent_dir/AGENT.md" ] || continue
      local name
      name=$(basename "$agent_dir")
      local target="$agents_target/$name"
      [ -e "$target" ] || [ -L "$target" ] || ln -sf "$agent_dir" "$target"
    done
  fi
}

# Create symlinks in .claude/rules/ for project rule files only.
# Global rules are handled user-level via claude_ensure_user_rules().
claude_create_rules_links() {
  local project="$1"
  local repo_path="$2"
  local rules_dir="$repo_path/.claude/rules"

  shopt -s nullglob
  # Project rules
  local project_rules_dir="$AGENTS_HOME/rules/$project"
  if [ -d "$project_rules_dir" ]; then
    for rule_file in "$project_rules_dir"/*.{md,mdc,txt}; do
      [ -f "$rule_file" ] || continue
      local stem
      stem=$(basename "${rule_file%.*}")
      ln -sf "$rule_file" "$rules_dir/${project}--${stem}.md"
    done
  fi
  shopt -u nullglob
}

# User-level dirs for global agents/skills
CLAUDE_USER_AGENTS="${CLAUDE_USER_AGENTS:-$HOME/.claude/agents}"
CLAUDE_USER_SKILLS="${CLAUDE_USER_SKILLS:-$HOME/.claude/skills}"

# Ensure user-level ~/.claude/agents has global agents (symlink dirs or AGENT.md)
claude_ensure_user_agents() {
  local global_agents="$AGENTS_HOME/agents/global"
  [ ! -d "$global_agents" ] && return 0

  local home_root
  while IFS= read -r home_root; do
    local user_agents_dir="$home_root/.claude/agents"
    mkdir -p "$user_agents_dir"
    for agent_dir in "$global_agents"/*/; do
      [ -d "$agent_dir" ] || continue
      [ -f "$agent_dir/AGENT.md" ] || continue
      local name
      name=$(basename "$agent_dir")
      local target="$user_agents_dir/$name"
      [ -e "$target" ] && [ -L "$target" ] && continue
      ln -sf "$agent_dir" "$target"
    done
  done < <(dot_agents_user_home_roots)
}

# Ensure user-level ~/.claude/skills has global skills (symlink dirs)
claude_ensure_user_skills() {
  local global_skills="$AGENTS_HOME/skills/global"
  [ ! -d "$global_skills" ] && return 0

  local home_root
  while IFS= read -r home_root; do
    local user_skills_dir="$home_root/.claude/skills"
    mkdir -p "$user_skills_dir"
    for skill_dir in "$global_skills"/*/; do
      [ -d "$skill_dir" ] || continue
      [ -f "$skill_dir/SKILL.md" ] || continue
      local name
      name=$(basename "$skill_dir")
      local target="$user_skills_dir/$name"
      [ -e "$target" ] && [ -L "$target" ] && continue
      ln -sf "$skill_dir" "$target"
    done
  done < <(dot_agents_user_home_roots)
}

# Ensure user-level ~/.claude/CLAUDE.md points to the primary global rules file.
# Claude Code auto-loads ~/.claude/CLAUDE.md at user scope.
claude_ensure_user_rules() {
  local home_root
  while IFS= read -r home_root; do
    local target="$home_root/.claude/CLAUDE.md"
    [ -e "$target" ] && [ -L "$target" ] && continue
    for _f in \
      "$AGENTS_HOME/rules/global/claude-code.mdc" \
      "$AGENTS_HOME/rules/global/claude-code.md" \
      "$AGENTS_HOME/rules/global/rules.mdc" \
      "$AGENTS_HOME/rules/global/rules.md" \
      "$AGENTS_HOME/rules/global/rules.txt"; do
      if [ -f "$_f" ]; then
        ln -sf "$_f" "$target"
        break
      fi
    done
  done < <(dot_agents_user_home_roots)
}

# Create skills symlinks for Claude Code: project skills in .claude/skills/ (Claude-native)
# and .agents/skills/ (GCD — also read by Cursor, Codex, OpenCode, Copilot).
claude_create_skills_links() {
  local project="$1"
  local repo_path="$2"

  claude_ensure_user_skills
  local skills_target="$repo_path/.claude/skills"
  local agents_skills_target="$repo_path/.agents/skills"
  local project_skills="$AGENTS_HOME/skills/$project"

  mkdir -p "$skills_target"
  mkdir -p "$agents_skills_target"
  rm -f "$skills_target"/* 2>/dev/null || true
  rm -f "$agents_skills_target"/* 2>/dev/null || true

  if [ -d "$project_skills" ]; then
    for skill_dir in "$project_skills"/*/; do
      [ -d "$skill_dir" ] || continue
      [ -f "$skill_dir/SKILL.md" ] || continue
      local name
      name=$(basename "$skill_dir")
      local claude_target="$skills_target/$name"
      [ -e "$claude_target" ] || [ -L "$claude_target" ] || ln -sf "$skill_dir" "$claude_target"
      local agents_target="$agents_skills_target/$name"
      [ -e "$agents_target" ] || [ -L "$agents_target" ] || ln -sf "$skill_dir" "$agents_target"
    done
  fi
}

# Check for deprecated .claude.json file
claude_has_deprecated_format() {
  local repo_path="$1"
  [ -f "$repo_path/.claude.json" ]
}

# Get deprecated format details
claude_deprecated_details() {
  local repo_path="$1"

  if [ -f "$repo_path/.claude.json" ]; then
    echo ".claude.json → .claude/settings.json"
  fi
}

# Check if global Claude settings are managed by dot-agents
claude_global_settings_managed() {
  local claude_settings="$HOME/.claude/settings.json"
  local agents_settings="$AGENTS_HOME/settings/global/claude-code-global.json"

  # Check if settings.json is a symlink pointing to our managed file
  if [ -L "$claude_settings" ]; then
    local target
    target=$(readlink "$claude_settings" 2>/dev/null)
    [ "$target" = "$agents_settings" ]
  else
    return 1
  fi
}

# Set up global Claude settings management
# Creates symlink from ~/.claude/settings.json → ~/.agents/settings/global/claude-code-global.json
claude_setup_global_settings() {
  local force="${1:-false}"
  local claude_dir="$HOME/.claude"
  local claude_settings="$claude_dir/settings.json"
  local agents_settings="$AGENTS_HOME/settings/global/claude-code-global.json"

  # Create ~/.claude directory if needed
  mkdir -p "$claude_dir"

  # Create ~/.agents/settings/global if needed
  mkdir -p "$AGENTS_HOME/settings/global"

  # Handle existing settings.json
  if [ -e "$claude_settings" ]; then
    if [ -L "$claude_settings" ]; then
      # Already a symlink
      local target
      target=$(readlink "$claude_settings" 2>/dev/null)
      if [ "$target" = "$agents_settings" ]; then
        echo "already_managed"
        return 0
      else
        # Symlink points elsewhere
        if [ "$force" = true ]; then
          rm "$claude_settings"
        else
          echo "symlink_conflict:$target"
          return 1
        fi
      fi
    else
      # Regular file exists
      if [ "$force" = true ]; then
        # Backup and migrate
        if [ ! -f "$agents_settings" ]; then
          cp "$claude_settings" "$agents_settings"
        fi
        mv "$claude_settings" "$claude_settings.backup"
        echo "migrated"
      else
        echo "file_exists"
        return 1
      fi
    fi
  fi

  # Create the managed settings file if it doesn't exist
  if [ ! -f "$agents_settings" ]; then
    echo '{}' > "$agents_settings"
  fi

  # Create symlink
  ln -sf "$agents_settings" "$claude_settings"
  echo "linked"
  return 0
}

# Get Claude global settings status
claude_global_settings_status() {
  local claude_settings="$HOME/.claude/settings.json"
  local agents_settings="$AGENTS_HOME/settings/global/claude-code-global.json"

  if [ ! -e "$claude_settings" ]; then
    echo "not_found"
  elif [ -L "$claude_settings" ]; then
    local target
    target=$(readlink "$claude_settings" 2>/dev/null)
    if [ "$target" = "$agents_settings" ]; then
      echo "managed"
    else
      echo "symlink_other:$target"
    fi
  else
    echo "unmanaged_file"
  fi
}
