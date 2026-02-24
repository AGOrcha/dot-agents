#!/bin/bash
# dot-agents/lib/platforms/github-copilot.sh
# GitHub Copilot support:
# - .github/copilot-instructions.md
# - .github/skills/*/SKILL.md
# - .github/agents/*.agent.md

# Copilot instructions are file-based, not CLI-based.
copilot_is_installed() {
  return 0
}

# Version/info string for doctor output.
copilot_version() {
  echo "file-based"
}

# User-level skills dir used by VS Code Copilot docs for personal skills.
COPILOT_USER_SKILLS="${COPILOT_USER_SKILLS:-$HOME/.github/skills}"

# Helper to find rule file with any extension (.md, .mdc, .txt)
# Returns empty string if not found.
_copilot_find_rule_file() {
  local base_path="$1"
  for ext in md mdc txt; do
    if [ -f "${base_path}.$ext" ]; then
      echo "${base_path}.$ext"
      return 0
    fi
  done
  echo ""
  return 0
}

# Create .github/copilot-instructions.md symlink.
# Priority:
# 1. ~/.agents/rules/{project}/copilot-instructions.md
# 2. ~/.agents/rules/global/copilot-instructions.md
# 3. ~/.agents/rules/{project}/rules.(md|mdc|txt)
# 4. ~/.agents/rules/global/rules.(md|mdc|txt)
copilot_create_links() {
  local project="$1"
  local repo_path="$2"

  local source=""
  if [ -f "$AGENTS_HOME/rules/$project/copilot-instructions.md" ]; then
    source="$AGENTS_HOME/rules/$project/copilot-instructions.md"
  elif [ -f "$AGENTS_HOME/rules/global/copilot-instructions.md" ]; then
    source="$AGENTS_HOME/rules/global/copilot-instructions.md"
  else
    source=$(_copilot_find_rule_file "$AGENTS_HOME/rules/$project/rules")
    [ -z "$source" ] && source=$(_copilot_find_rule_file "$AGENTS_HOME/rules/global/rules")
  fi

  [ -z "$source" ] && return 0

  mkdir -p "$repo_path/.github"
  ln -sf "$source" "$repo_path/.github/copilot-instructions.md"

  copilot_create_skills_links "$project" "$repo_path"
  copilot_create_agents_links "$project" "$repo_path"
}

# Ensure user-level ~/.github/skills has global skills (symlink dirs)
copilot_ensure_user_skills() {
  local global_skills="$AGENTS_HOME/skills/global"
  mkdir -p "$COPILOT_USER_SKILLS"
  [ ! -d "$global_skills" ] && return 0
  for skill_dir in "$global_skills"/*/; do
    [ -d "$skill_dir" ] || continue
    [ -f "$skill_dir/SKILL.md" ] || continue
    local name
    name=$(basename "$skill_dir")
    local target="$COPILOT_USER_SKILLS/$name"
    [ -e "$target" ] && [ -L "$target" ] && continue
    ln -sf "$skill_dir" "$target"
  done
}

# Create project skills symlinks for Copilot (.github/skills/{name}/ -> skill dir)
copilot_create_skills_links() {
  local project="$1"
  local repo_path="$2"

  copilot_ensure_user_skills
  local skills_target="$repo_path/.github/skills"
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

# Create project agent symlinks for Copilot (.github/agents/{name}.agent.md -> AGENT.md)
copilot_create_agents_links() {
  local project="$1"
  local repo_path="$2"

  local agents_target="$repo_path/.github/agents"
  local project_agents="$AGENTS_HOME/agents/$project"

  mkdir -p "$agents_target"
  rm -f "$agents_target"/*.agent.md 2>/dev/null || true

  if [ -d "$project_agents" ]; then
    for agent_dir in "$project_agents"/*/; do
      [ -d "$agent_dir" ] || continue
      [ -f "$agent_dir/AGENT.md" ] || continue
      local name
      name=$(basename "$agent_dir")
      local target="$agents_target/$name.agent.md"
      [ -e "$target" ] || [ -L "$target" ] || ln -sf "${agent_dir%/}/AGENT.md" "$target"
    done
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
