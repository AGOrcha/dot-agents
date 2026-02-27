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
CODEX_USER_SKILLS="${CODEX_USER_SKILLS:-$HOME/.agents/skills}"

# Ensure user-level ~/.codex/agents has global agents (symlink dirs)
codex_ensure_user_agents() {
  local global_agents="$AGENTS_HOME/agents/global"
  [ ! -d "$global_agents" ] && return 0

  local home_root
  while IFS= read -r home_root; do
    local user_agents_dir="$home_root/.codex/agents"
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

# Ensure user-level ~/.agents/skills has global skills (symlink dirs)
codex_ensure_user_skills() {
  local skills_root="$AGENTS_HOME/skills"
  local legacy_global="$AGENTS_HOME/skills/global"
  [ ! -d "$skills_root" ] && [ ! -d "$legacy_global" ] && return 0

  local home_root
  while IFS= read -r home_root; do
    local user_skills_dir="$home_root/.agents/skills"
    mkdir -p "$user_skills_dir"

    local source_root
    for source_root in "$skills_root" "$legacy_global"; do
      [ -d "$source_root" ] || continue
      for skill_dir in "$source_root"/*/; do
        [ -d "$skill_dir" ] || continue
        [ -f "$skill_dir/SKILL.md" ] || continue
        local name
        name=$(basename "$skill_dir")
        local target="$user_skills_dir/$name"
        [ -e "$target" ] && [ -L "$target" ] && continue
        ln -sf "$skill_dir" "$target"
      done
    done
  done
}

# Create links for Codex (SYMLINKS - works fine)
codex_create_links() {
  local project="$1"
  local repo_path="$2"

  codex_ensure_user_agents
  codex_ensure_user_skills
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
  # Project skills mirror in repo (.agents/skills)
  codex_create_skills_links "$project" "$repo_path"
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

# Create project skills symlinks for Codex CLI (directory-based, .agents/skills)
codex_create_skills_links() {
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
