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
  command -v claude >/dev/null 2>&1
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
# Claude Code reads rules from multiple locations:
#   - CLAUDE.md at repo root (project-specific instructions)
#   - .claude/rules/*.md (auto-loaded rule files)
#
# We use .claude/rules/ with symlinks to the 4 rule sources:
#   1. global--rules.md        → ~/.agents/rules/global/rules.mdc (all agents, global)
#   2. global--claude-code.md  → ~/.agents/rules/global/claude-code.mdc (Claude, global)
#   3. project--rules.md       → ~/.agents/rules/{project}/rules.mdc (all agents, project)
#   4. project--claude-code.md → ~/.agents/rules/{project}/claude-code.mdc (Claude, project)
#
# All files in .claude/rules/ are automatically loaded by Claude Code.
claude_create_links() {
  local project="$1"
  local repo_path="$2"

  # Create .claude directory structure
  mkdir -p "$repo_path/.claude/rules"

  # Create symlinks for all 4 rule sources in .claude/rules/
  # Claude Code auto-loads all .md files in this directory
  claude_create_rules_links "$project" "$repo_path"

  # Also create CLAUDE.md symlink for backwards compatibility / visibility
  # Points to the project's main rules file (or global if none exists)
  claude_create_main_rules_link "$project" "$repo_path"

  # Link settings.local.json if exists
  if [ -f "$AGENTS_HOME/settings/$project/claude-code.json" ]; then
    ln -sf "$AGENTS_HOME/settings/$project/claude-code.json" "$repo_path/.claude/settings.local.json"
  fi

  # Link MCP config if exists
  if [ -f "$AGENTS_HOME/mcp/$project/claude.json" ]; then
    ln -sf "$AGENTS_HOME/mcp/$project/claude.json" "$repo_path/.mcp.json"
  elif [ -f "$AGENTS_HOME/mcp/global/claude.json" ]; then
    ln -sf "$AGENTS_HOME/mcp/global/claude.json" "$repo_path/.mcp.json"
  fi
}

# Create symlinks in .claude/rules/ for all 4 rule sources
claude_create_rules_links() {
  local project="$1"
  local repo_path="$2"
  local rules_dir="$repo_path/.claude/rules"

  # 1. Global all-agents rules
  local global_rules
  global_rules=$(_claude_find_rule_file "$AGENTS_HOME/rules/global/rules")
  if [ -n "$global_rules" ]; then
    ln -sf "$global_rules" "$rules_dir/global--rules.md"
  fi

  # 2. Global Claude-specific rules
  local global_claude
  global_claude=$(_claude_find_rule_file "$AGENTS_HOME/rules/global/claude-code")
  [ -z "$global_claude" ] && global_claude=$(_claude_find_rule_file "$AGENTS_HOME/rules/global/claude")
  if [ -n "$global_claude" ]; then
    ln -sf "$global_claude" "$rules_dir/global--claude-code.md"
  fi

  # 3. Project all-agents rules
  local project_rules
  project_rules=$(_claude_find_rule_file "$AGENTS_HOME/rules/$project/rules")
  if [ -n "$project_rules" ]; then
    ln -sf "$project_rules" "$rules_dir/project--rules.md"
  fi

  # 4. Project Claude-specific rules
  local project_claude
  project_claude=$(_claude_find_rule_file "$AGENTS_HOME/rules/$project/claude-code")
  [ -z "$project_claude" ] && project_claude=$(_claude_find_rule_file "$AGENTS_HOME/rules/$project/claude")
  if [ -n "$project_claude" ]; then
    ln -sf "$project_claude" "$rules_dir/project--claude-code.md"
  fi
}

# Create CLAUDE.md symlink at repo root for visibility/backwards compatibility
claude_create_main_rules_link() {
  local project="$1"
  local repo_path="$2"

  # Prefer project-specific rules, fall back to global
  local target=""

  # Check for project rules file
  for ext in md mdc txt; do
    if [ -f "$AGENTS_HOME/rules/$project/rules.$ext" ]; then
      target="$AGENTS_HOME/rules/$project/rules.$ext"
      break
    fi
    if [ -f "$AGENTS_HOME/rules/$project/claude-code.$ext" ]; then
      target="$AGENTS_HOME/rules/$project/claude-code.$ext"
      break
    fi
  done

  # Fall back to global rules
  if [ -z "$target" ]; then
    for ext in md mdc txt; do
      if [ -f "$AGENTS_HOME/rules/global/rules.$ext" ]; then
        target="$AGENTS_HOME/rules/global/rules.$ext"
        break
      fi
    done
  fi

  # Create symlink if we found a target
  if [ -n "$target" ]; then
    ln -sf "$target" "$repo_path/CLAUDE.md"
  fi
}

# Create skills symlinks for Claude Code (directory-based)
# Symlinks global and project skills to .claude/skills/ so they work as slash commands
# Project skills override global skills with the same name (with warning)
claude_create_skills_links() {
  local project="$1"
  local repo_path="$2"

  local skills_target="$repo_path/.claude/skills"
  local global_skills="$AGENTS_HOME/skills/global"
  local project_skills="$AGENTS_HOME/skills/$project"

  # Create skills directory
  mkdir -p "$skills_target"

  # Collect project skill names (for conflict detection)
  local project_skill_names=""
  if [ -d "$project_skills" ]; then
    for skill_dir in "$project_skills"/*/; do
      [ -d "$skill_dir" ] || continue
      [ -f "$skill_dir/SKILL.md" ] || continue
      local name
      name=$(basename "$skill_dir")
      project_skill_names="$project_skill_names $name "
    done
  fi

  # Symlink global skills (no prefix, skip if shadowed by project skill)
  if [ -d "$global_skills" ]; then
    for skill_dir in "$global_skills"/*/; do
      [ -d "$skill_dir" ] || continue
      [ -f "$skill_dir/SKILL.md" ] || continue
      local name
      name=$(basename "$skill_dir")
      local target="$skills_target/$name"

      # Check if project has a skill with the same name
      if [[ "$project_skill_names" == *" $name "* ]]; then
        # Project skill shadows global - warn and skip
        echo -e "  ${YELLOW}⚠${NC}  Skill '$name' shadows global skill (project overrides global)" >&2
        continue
      fi

      # Only create if doesn't exist
      [ -e "$target" ] || [ -L "$target" ] || ln -sf "$skill_dir" "$target"
    done
  fi

  # Symlink project skills (no prefix)
  if [ -d "$project_skills" ]; then
    for skill_dir in "$project_skills"/*/; do
      [ -d "$skill_dir" ] || continue
      [ -f "$skill_dir/SKILL.md" ] || continue
      local name
      name=$(basename "$skill_dir")
      local target="$skills_target/$name"
      # Only create if doesn't exist
      [ -e "$target" ] || [ -L "$target" ] || ln -sf "$skill_dir" "$target"
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
