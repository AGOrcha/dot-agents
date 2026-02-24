#!/bin/bash
# dot-agents/lib/commands/remove.sh
# Remove a project from dot-agents management

cmd_remove_help() {
  cat << EOF
${BOLD}dot-agents remove${NC} - Remove a project from dot-agents management

${BOLD}USAGE${NC}
    dot-agents remove <project> [options]

${BOLD}ARGUMENTS${NC}
    <project>         Project name to remove (required)

${BOLD}OPTIONS${NC}
    --clean           Also remove project directories from ~/.agents/
    --dry-run         Show what would be done without making changes
    --force, -f       Skip confirmation prompt
    --yes, -y         Auto-confirm prompts (same as --force)
    --verbose, -v     Show detailed output
    --help, -h        Show this help

${BOLD}DESCRIPTION${NC}
    Unregisters a project from dot-agents and removes config symlinks.

    Removes from project directory:
    - .cursor/rules/ hard links (global--, project-- prefixed files)
    - .claude/rules/ symlinks (global--, project-- prefixed files)
    - AGENTS.md symlink (if pointing to ~/.agents/)
    - opencode.json and .opencode/agent/* symlinks (if pointing to ~/.agents/)
    - .github/copilot-instructions.md symlink (if pointing to ~/.agents/)
    - .github/skills/* and .github/agents/*.agent.md symlinks (if pointing to ~/.agents/)

    With --clean, also removes:
    - ~/.agents/rules/<project>/
    - ~/.agents/settings/<project>/
    - ~/.agents/mcp/<project>/
    - ~/.agents/skills/<project>/
    - ~/.agents/agents/<project>/

    Note: Local .cursor/rules/ files not managed by dot-agents are preserved.

${BOLD}EXAMPLES${NC}
    dot-agents remove myproject           # Unlink and unregister
    dot-agents remove myproject --clean   # Also remove project dirs
    dot-agents remove myproject --dry-run # Preview changes

EOF
}

cmd_remove() {
  local project_name=""
  local clean_dirs=false

  # Parse flags
  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    case $1 in
      --clean)
        clean_dirs=true
        shift
        ;;
      --dry-run)
        DRY_RUN=true
        shift
        ;;
      --force|-f)
        FORCE=true
        shift
        ;;
      --yes|-y)
        YES=true
        shift
        ;;
      --verbose|-v)
        VERBOSE=true
        shift
        ;;
      --help|-h)
        cmd_remove_help
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

  # Get project name from remaining args
  if [ ${#REMAINING_ARGS[@]} -eq 0 ]; then
    log_error "Project name required"
    echo ""
    echo "Usage: dot-agents remove <project>"
    echo ""
    echo "Projects:"
    config_list_projects | while read -r name; do
      echo "  - $name"
    done
    return 1
  fi

  project_name="${REMAINING_ARGS[0]}"

  # Check if project is registered
  local project_path
  project_path=$(config_get_project_path "$project_name")

  if [ -z "$project_path" ]; then
    log_error "Project not found: $project_name"
    echo ""
    echo "Run 'dot-agents status' to see registered projects."
    return 1
  fi

  local display_path="${project_path/#$HOME/~}"

  log_header "dot-agents remove"
  echo -e "Removing project: ${BOLD}$project_name${NC}"
  echo -e "Path: ${DIM}$display_path${NC}"

  # Step 1: Analyze what will be removed
  local step_count=3
  [ "$clean_dirs" = true ] && step_count=4

  init_steps $step_count
  step "Analyzing project..."

  # Check project directory exists
  if [ -d "$project_path" ]; then
    bullet "ok" "Project directory found"
  else
    bullet "warn" "Project directory not found (links may have been moved)"
  fi

  # Count links that will be removed
  local cursor_links=0 claude_links=0 codex_links=0 opencode_links=0 copilot_links=0

  if [ -d "$project_path/.cursor/rules" ]; then
    cursor_links=$(ls -1 "$project_path/.cursor/rules/"*.mdc 2>/dev/null | wc -l | tr -d ' ')
  fi
  if [ -d "$project_path/.claude/rules" ]; then
    claude_links=$(ls -1 "$project_path/.claude/rules/"*.md 2>/dev/null | wc -l | tr -d ' ')
  fi
  [ -L "$project_path/AGENTS.md" ] && ((codex_links++)) || true
  [ -L "$project_path/opencode.json" ] && ((opencode_links++)) || true
  [ -L "$project_path/.github/copilot-instructions.md" ] && ((copilot_links++)) || true
  if [ -d "$project_path/.github/skills" ]; then
    copilot_links=$((copilot_links + $(find "$project_path/.github/skills" -mindepth 1 -maxdepth 1 -type l 2>/dev/null | wc -l | tr -d ' ')))
  fi
  if [ -d "$project_path/.github/agents" ]; then
    copilot_links=$((copilot_links + $(find "$project_path/.github/agents" -mindepth 1 -maxdepth 1 -type l 2>/dev/null | wc -l | tr -d ' ')))
  fi

  local total_links=$((cursor_links + claude_links + codex_links + opencode_links + copilot_links))
  if [ "$total_links" -gt 0 ]; then
    bullet "found" "$total_links managed links found"
  else
    bullet "none" "No managed links found"
  fi

  # Step 2: Preview what will be removed
  step "The following will be removed:"

  preview_section "From $display_path:" \
    ".cursor/rules/global--*.mdc     (hard links)" \
    ".cursor/rules/${project_name}--*.mdc (hard links)" \
    ".cursor/commands/*.md           (symlinks to skills)" \
    ".cursor/agents/*.md             (symlinks to subagents)" \
    ".claude/rules/global--*.md      (symlinks)" \
    ".claude/rules/project--*.md     (symlinks)" \
    "AGENTS.md                       (symlink)" \
    "opencode.json and .opencode/agent/* (symlinks)" \
    ".github/copilot-instructions.md (symlink)" \
    ".github/skills/* and .github/agents/*.agent.md (symlinks)"

  preview_section "From ~/.agents/config.json:" \
    "Project registration for '$project_name'"

  if [ "$clean_dirs" = true ]; then
    warn_box "Destructive Action" \
      "The --clean flag will permanently delete:" \
      "  ~/.agents/rules/$project_name/" \
      "  ~/.agents/settings/$project_name/" \
      "  ~/.agents/mcp/$project_name/" \
      "  ~/.agents/skills/$project_name/" \
      "  ~/.agents/agents/$project_name/"
  else
    info_box "Tip" \
      "Project directories in ~/.agents/ will be preserved." \
      "Use --clean to also remove them."
  fi

  # Handle dry-run mode
  if [ "$DRY_RUN" = true ]; then
    echo ""
    log_info "DRY RUN - no changes made"
    return 0
  fi

  # Confirm before proceeding
  if ! confirm_action "Proceed with removal?"; then
    log_info "Removal cancelled."
    return 0
  fi

  # Step 3: Remove links and unregister
  step "Removing project..."

  # Remove links from project directory
  if [ -d "$project_path" ]; then
    remove_project_links "$project_name" "$project_path"
    bullet "ok" "Removed managed links"
  else
    bullet "skip" "Skipped link removal (directory not found)"
  fi

  # Unregister from config.json
  config_remove_project "$project_name"
  bullet "ok" "Unregistered from config.json"

  # Step 4 (optional): Clean project directories
  if [ "$clean_dirs" = true ]; then
    step "Cleaning project directories..."
    remove_project_dirs "$project_name"
    bullet "ok" "Removed project directories"
  fi

  # Success message
  if [ "$clean_dirs" = true ]; then
    success_with_next_steps "Project '$project_name' removed completely!" \
      "Verify removal: dot-agents status"
  else
    success_with_next_steps "Project '$project_name' unlinked successfully!" \
      "Verify removal: dot-agents status" \
      "To also remove project directories: dot-agents remove $project_name --clean"
  fi

  show_test_commands \
    "dot-agents status" \
    "ls -la $display_path"
}

# Remove symlinks and hard links from project directory
remove_project_links() {
  local project="$1"
  local repo="$2"

  # Remove Cursor rule hard links
  remove_cursor_links "$project" "$repo"

  # Remove Claude Code symlinks
  remove_claude_links "$project" "$repo"

  # Remove Codex symlinks
  remove_codex_links "$project" "$repo"

  # Remove OpenCode symlinks
  remove_opencode_links "$project" "$repo"

  # Remove GitHub Copilot symlink
  remove_copilot_links "$project" "$repo"
}

# Remove Cursor hard links from .cursor/rules/
remove_cursor_links() {
  local project="$1"
  local repo="$2"
  local agents_home="$AGENTS_HOME"
  local cursor_rules_dir="$repo/.cursor/rules"

  if [ ! -d "$cursor_rules_dir" ]; then
    [ "$VERBOSE" = true ] && log_info "No .cursor/rules/ directory"
    return 0
  fi

  local removed_count=0

  # Remove global-prefixed rules
  shopt -s nullglob
  for rule in "$cursor_rules_dir"/global--*.mdc; do
    local basename
    basename=$(basename "$rule")
    local source_name="${basename#global--}"
    local source_path="$agents_home/rules/global/$source_name"

    # Only remove if it's a hard link to our source (same inode)
    if [ -f "$source_path" ] && are_hardlinked "$rule" "$source_path"; then
      if [ "$DRY_RUN" = true ]; then
        log_dry "remove $basename"
      else
        rm "$rule"
        log_info "Removed $basename"
      fi
      ((removed_count++)) || true
    else
      [ "$VERBOSE" = true ] && log_skip "$basename (not managed by dot-agents)"
    fi
  done

  # Remove project-prefixed rules
  for rule in "$cursor_rules_dir"/"${project}--"*.mdc; do
    local basename
    basename=$(basename "$rule")
    local source_name="${basename#${project}--}"
    local source_path="$agents_home/rules/$project/$source_name"

    # Only remove if it's a hard link to our source
    if [ -f "$source_path" ] && are_hardlinked "$rule" "$source_path"; then
      if [ "$DRY_RUN" = true ]; then
        log_dry "remove $basename"
      else
        rm "$rule"
        log_info "Removed $basename"
      fi
      ((removed_count++)) || true
    else
      [ "$VERBOSE" = true ] && log_skip "$basename (not managed by dot-agents)"
    fi
  done
  shopt -u nullglob

  # Remove .cursor/commands/ symlinks that point to our skills
  local cursor_commands_dir="$repo/.cursor/commands"
  if [ -d "$cursor_commands_dir" ]; then
    for link in "$cursor_commands_dir"/*.md; do
      [ -e "$link" ] || continue
      [ -L "$link" ] || continue
      local target
      target=$(readlink "$link")
      if [[ "$target" == "$agents_home"* ]]; then
        if [ "$DRY_RUN" = true ]; then
          log_dry "remove .cursor/commands/$(basename "$link")"
        else
          rm "$link"
          log_info "Removed .cursor/commands/$(basename "$link")"
        fi
        ((removed_count++)) || true
      fi
    done
  fi

  # Remove .cursor/agents/ symlinks that point to our agents
  local cursor_agents_dir="$repo/.cursor/agents"
  if [ -d "$cursor_agents_dir" ]; then
    for link in "$cursor_agents_dir"/*.md; do
      [ -e "$link" ] || continue
      [ -L "$link" ] || continue
      local target
      target=$(readlink "$link")
      if [[ "$target" == "$agents_home"* ]]; then
        if [ "$DRY_RUN" = true ]; then
          log_dry "remove .cursor/agents/$(basename "$link")"
        else
          rm "$link"
          log_info "Removed .cursor/agents/$(basename "$link")"
        fi
        ((removed_count++)) || true
      fi
    done
  fi

  if [ $removed_count -eq 0 ]; then
    [ "$VERBOSE" = true ] && log_info "No Cursor links to remove"
  fi
}

# Remove Claude Code symlinks from .claude/rules/
remove_claude_links() {
  local project="$1"
  local repo="$2"
  local agents_home="$AGENTS_HOME"
  local rules_dir="$repo/.claude/rules"

  # Remove symlinks from .claude/rules/ that point to our ~/.agents/ directory
  if [ -d "$rules_dir" ]; then
    local removed_count=0
    shopt -s nullglob
    for link in "$rules_dir"/*.md; do
      if [ -L "$link" ]; then
        local target
        target=$(readlink "$link")

        # Check if it points to our ~/.agents/ directory
        if [[ "$target" == "$agents_home"* ]]; then
          if [ "$DRY_RUN" = true ]; then
            local filename
            filename=$(basename "$link")
            log_dry "remove .claude/rules/$filename"
          else
            rm "$link"
            ((removed_count++))
          fi
        fi
      fi
    done
    shopt -u nullglob

    if [ "$removed_count" -gt 0 ]; then
      log_info "Removed $removed_count Claude Code rule symlinks"
    elif [ "$VERBOSE" = true ]; then
      log_info "No Claude Code rule symlinks to remove"
    fi
  fi

  # Remove .claude/agents/ symlinks that point to our agents
  local agents_dir="$repo/.claude/agents"
  if [ -d "$agents_dir" ]; then
    for link in "$agents_dir"/*; do
      [ -e "$link" ] || continue
      [ -L "$link" ] || continue
      target=$(readlink "$link")
      if [[ "$target" == "$agents_home"* ]]; then
        if [ "$DRY_RUN" = true ]; then
          log_dry "remove .claude/agents/$(basename "$link")"
        else
          rm "$link"
          log_info "Removed .claude/agents/$(basename "$link")"
        fi
      fi
    done
  fi

  # Remove .claude/skills/ symlinks that point to our skills
  local skills_dir="$repo/.claude/skills"
  if [ -d "$skills_dir" ]; then
    for link in "$skills_dir"/*; do
      [ -e "$link" ] || continue
      [ -L "$link" ] || continue
      target=$(readlink "$link")
      if [[ "$target" == "$agents_home"* ]]; then
        if [ "$DRY_RUN" = true ]; then
          log_dry "remove .claude/skills/$(basename "$link")"
        else
          rm "$link"
          log_info "Removed .claude/skills/$(basename "$link")"
        fi
      fi
    done
  fi
}

# Remove Codex symlinks
remove_codex_links() {
  local project="$1"
  local repo="$2"
  local agents_home="$AGENTS_HOME"

  # Remove AGENTS.md if it's a symlink pointing to our source
  local agents_md="$repo/AGENTS.md"
  if [ -L "$agents_md" ]; then
    local target
    target=$(readlink "$agents_md")

    if [[ "$target" == "$agents_home"* ]]; then
      if [ "$DRY_RUN" = true ]; then
        log_dry "remove AGENTS.md"
      else
        rm "$agents_md"
        log_info "Removed AGENTS.md"
      fi
    else
      [ "$VERBOSE" = true ] && log_skip "AGENTS.md (not managed by dot-agents)"
    fi
  fi

  # Remove .codex/agents/ symlinks that point to our agents
  local agents_dir="$repo/.codex/agents"
  if [ -d "$agents_dir" ]; then
    for link in "$agents_dir"/*; do
      [ -e "$link" ] || continue
      [ -L "$link" ] || continue
      target=$(readlink "$link")
      if [[ "$target" == "$agents_home"* ]]; then
        if [ "$DRY_RUN" = true ]; then
          log_dry "remove .codex/agents/$(basename "$link")"
        else
          rm "$link"
          log_info "Removed .codex/agents/$(basename "$link")"
        fi
      fi
    done
  fi

  # Remove .codex/skills/ symlinks that point to our skills
  local skills_dir="$repo/.codex/skills"
  if [ -d "$skills_dir" ]; then
    for link in "$skills_dir"/*; do
      [ -e "$link" ] || continue
      [ -L "$link" ] || continue
      target=$(readlink "$link")
      if [[ "$target" == "$agents_home"* ]]; then
        if [ "$DRY_RUN" = true ]; then
          log_dry "remove .codex/skills/$(basename "$link")"
        else
          rm "$link"
          log_info "Removed .codex/skills/$(basename "$link")"
        fi
      fi
    done
  fi
}

# Remove OpenCode symlinks
remove_opencode_links() {
  local project="$1"
  local repo="$2"
  local agents_home="$AGENTS_HOME"

  # Remove opencode.json if it's a symlink pointing to our source
  local opencode_json="$repo/opencode.json"
  if [ -L "$opencode_json" ]; then
    local target
    target=$(readlink "$opencode_json")
    if [[ "$target" == "$agents_home"* ]]; then
      if [ "$DRY_RUN" = true ]; then
        log_dry "remove opencode.json"
      else
        rm "$opencode_json"
        log_info "Removed opencode.json"
      fi
    fi
  fi

  # Remove .opencode/agent symlinks that point to our rules
  local agent_dir="$repo/.opencode/agent"
  if [ -d "$agent_dir" ]; then
    for link in "$agent_dir"/*; do
      [ -e "$link" ] || continue
      [ -L "$link" ] || continue
      target=$(readlink "$link")
      if [[ "$target" == "$agents_home"* ]]; then
        if [ "$DRY_RUN" = true ]; then
          log_dry "remove .opencode/agent/$(basename "$link")"
        else
          rm "$link"
          log_info "Removed .opencode/agent/$(basename "$link")"
        fi
      fi
    done
  fi
}

# Remove GitHub Copilot symlink
remove_copilot_links() {
  local project="$1"
  local repo="$2"
  local agents_home="$AGENTS_HOME"

  local copilot_file="$repo/.github/copilot-instructions.md"
  if [ -L "$copilot_file" ]; then
    local target
    target=$(readlink "$copilot_file")
    if [[ "$target" == "$agents_home"* ]]; then
      if [ "$DRY_RUN" = true ]; then
        log_dry "remove .github/copilot-instructions.md"
      else
        rm "$copilot_file"
        log_info "Removed .github/copilot-instructions.md"
      fi
    fi
  fi

  # Remove .github/skills symlinks that point to our skills
  local skills_dir="$repo/.github/skills"
  if [ -d "$skills_dir" ]; then
    for link in "$skills_dir"/*; do
      [ -e "$link" ] || continue
      [ -L "$link" ] || continue
      target=$(readlink "$link")
      if [[ "$target" == "$agents_home"* ]]; then
        if [ "$DRY_RUN" = true ]; then
          log_dry "remove .github/skills/$(basename "$link")"
        else
          rm "$link"
          log_info "Removed .github/skills/$(basename "$link")"
        fi
      fi
    done
  fi

  # Remove .github/agents symlinks that point to our agents
  local agents_dir="$repo/.github/agents"
  if [ -d "$agents_dir" ]; then
    for link in "$agents_dir"/*.agent.md; do
      [ -e "$link" ] || continue
      [ -L "$link" ] || continue
      target=$(readlink "$link")
      if [[ "$target" == "$agents_home"* ]]; then
        if [ "$DRY_RUN" = true ]; then
          log_dry "remove .github/agents/$(basename "$link")"
        else
          rm "$link"
          log_info "Removed .github/agents/$(basename "$link")"
        fi
      fi
    done
  fi
}

# Remove project directories from ~/.agents/
remove_project_dirs() {
  local project="$1"
  local agents_home="$AGENTS_HOME"

  local dirs=(
    "$agents_home/rules/$project"
    "$agents_home/settings/$project"
    "$agents_home/mcp/$project"
    "$agents_home/skills/$project"
    "$agents_home/agents/$project"
  )

  for dir in "${dirs[@]}"; do
    local display_dir="${dir/#$HOME/~}"
    if [ -d "$dir" ]; then
      # Check if directory is empty or has files
      local file_count
      file_count=$(find "$dir" -type f 2>/dev/null | wc -l | tr -d ' ')

      if [ "$DRY_RUN" = true ]; then
        if [ "$file_count" -gt 0 ]; then
          log_dry "remove $display_dir/ ($file_count files)"
        else
          log_dry "remove $display_dir/ (empty)"
        fi
      else
        rm -rf "$dir"
        if [ "$file_count" -gt 0 ]; then
          log_info "Removed $display_dir/ ($file_count files)"
        else
          log_info "Removed $display_dir/"
        fi
      fi
    else
      [ "$VERBOSE" = true ] && log_skip "$display_dir/ (not found)"
    fi
  done
}
