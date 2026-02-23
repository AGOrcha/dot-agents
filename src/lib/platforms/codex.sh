#!/bin/bash
# dot-agents/lib/platforms/codex.sh
# OpenAI Codex CLI detection, version, and linking

# Detect Codex CLI version
codex_detect() {
  if command -v codex >/dev/null 2>&1; then
    codex --version 2>/dev/null | head -1
  fi
}

# Check if Codex is installed
codex_is_installed() {
  command -v codex >/dev/null 2>&1
}

# Get Codex version string
codex_version() {
  codex_detect
}

# User-level dirs for global agents/skills
CODEX_USER_AGENTS="${CODEX_USER_AGENTS:-$HOME/.codex/agents}"
CODEX_USER_SKILLS="${CODEX_USER_SKILLS:-$HOME/.codex/skills}"

# Ensure user-level ~/.codex/agents has global agents (symlink dirs)
codex_ensure_user_agents() {
  local global_agents="$AGENTS_HOME/agents/global"
  mkdir -p "$CODEX_USER_AGENTS"
  [ ! -d "$global_agents" ] && return 0
  for agent_dir in "$global_agents"/*/; do
    [ -d "$agent_dir" ] || continue
    [ -f "$agent_dir/AGENT.md" ] || continue
    local name
    name=$(basename "$agent_dir")
    local target="$CODEX_USER_AGENTS/$name"
    [ -e "$target" ] && [ -L "$target" ] && continue
    ln -sf "$agent_dir" "$target"
  done
}

# Ensure user-level ~/.codex/skills has global skills (symlink dirs)
codex_ensure_user_skills() {
  local global_skills="$AGENTS_HOME/skills/global"
  mkdir -p "$CODEX_USER_SKILLS"
  [ ! -d "$global_skills" ] && return 0
  for skill_dir in "$global_skills"/*/; do
    [ -d "$skill_dir" ] || continue
    [ -f "$skill_dir/SKILL.md" ] || continue
    local name
    name=$(basename "$skill_dir")
    local target="$CODEX_USER_SKILLS/$name"
    [ -e "$target" ] && [ -L "$target" ] && continue
    ln -sf "$skill_dir" "$target"
  done
}

# Create links for Codex (SYMLINKS - works fine)
codex_create_links() {
  local project="$1"
  local repo_path="$2"

  codex_ensure_user_agents
  # Link AGENTS.md from global rules if it exists
  if [ -f "$AGENTS_HOME/rules/global/agents.md" ]; then
    ln -sf "$AGENTS_HOME/rules/global/agents.md" "$repo_path/AGENTS.md"
  elif [ -f "$AGENTS_HOME/rules/global/rules.md" ]; then
    # Fall back to global rules.md if no agents-specific file
    ln -sf "$AGENTS_HOME/rules/global/rules.md" "$repo_path/AGENTS.md"
  fi

  # Project-specific AGENTS.md
  if [ -f "$AGENTS_HOME/rules/$project/agents.md" ]; then
    # If project has its own agents.md, use it instead
    ln -sf "$AGENTS_HOME/rules/$project/agents.md" "$repo_path/AGENTS.md"
  fi

  # Create .codex directory for config
  mkdir -p "$repo_path/.codex"

  # Link TOML config if exists (Codex uses TOML, not JSON)
  if [ -f "$AGENTS_HOME/settings/$project/codex.toml" ]; then
    ln -sf "$AGENTS_HOME/settings/$project/codex.toml" "$repo_path/.codex/config.toml"
  elif [ -f "$AGENTS_HOME/settings/global/codex.toml" ]; then
    ln -sf "$AGENTS_HOME/settings/global/codex.toml" "$repo_path/.codex/config.toml"
  fi

  # Project agents (global → user-level ~/.codex/agents)
  codex_create_agents_links "$project" "$repo_path"
}

# Create agents symlinks for Codex: project agents only (symlink dirs to .codex/agents/)
codex_create_agents_links() {
  local project="$1"
  local repo_path="$2"

  local agents_target="$repo_path/.codex/agents"
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

# Check for deprecated formats (Codex has been stable - no deprecated formats)
codex_has_deprecated_format() {
  local repo_path="$1"
  return 1  # No deprecated formats for Codex
}

# Get deprecated format details
codex_deprecated_details() {
  local repo_path="$1"
  # Codex has no deprecated formats
  echo ""
}

# Create project skills symlinks for Codex CLI (directory-based)
codex_create_skills_links() {
  local project="$1"
  local repo_path="$2"

  codex_ensure_user_skills
  local skills_target="$repo_path/.codex/skills"
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
