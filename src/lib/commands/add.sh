#!/bin/bash
# dot-agents/lib/commands/add.sh
# Add a project to dot-agents management

# Restore-map helpers were inlined here after the standalone shell helper was deleted.
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

cmd_add_help() {
  cat << EOF
${BOLD}dot-agents add${NC} - Add a project to dot-agents management

${BOLD}USAGE${NC}
    dot-agents add <path> [options]

${BOLD}ARGUMENTS${NC}
    <path>            Path to project directory (required)

${BOLD}OPTIONS${NC}
    --name <name>     Override project name (default: directory name)
    --dry-run         Show what would be done without making changes
    --force, -f       Overwrite existing configurations
    --verbose, -v     Show detailed output
    --help, -h        Show this help

${BOLD}DESCRIPTION${NC}
    Registers a project with dot-agents and sets up configuration links.
    Existing config files are backed up before being replaced.

    Creates in ~/.agents/:
    - rules/<project>/         Project-specific rules
    - settings/<project>/      Project settings
    - skills/<project>/        Project skills
    - mcp/<project>/           Project MCP configs
    - agents/<project>/        Project subagents

    Links created in project directory:

    ${BOLD}Cursor${NC} (uses HARD LINKS - required by IDE):
    - .cursor/rules/           Project rules
    - .agents/skills/          Project skills
    - .claude/agents/          Project agents (GCD path, read by Cursor)
    - .cursor/settings.json    IDE settings
    - .cursor/mcp.json         MCP server configs
    - .cursorignore            Files to ignore

    ${BOLD}Claude Code${NC} (uses SYMLINKS):
    - .claude/CLAUDE.md (user-level, via claude_ensure_user_rules)
    - .claude/rules/{project}--*.md  Rule files (project-scope)
    - .claude/settings.local.json  Project settings
    - .claude/skills/          Commands as skills (project)
    - .mcp.json                MCP server configs

    ${BOLD}Codex CLI${NC} (uses SYMLINKS):
    - AGENTS.md                Project instructions
    - .agents/skills/*/        Project skills

    ${BOLD}OpenCode${NC} (uses SYMLINKS):
    - .opencode/               Config directory

    ${BOLD}GitHub Copilot${NC} (uses SYMLINKS):
    - .github/copilot-instructions.md   Project instructions
    - .agents/skills/*/                 Project skills
    - .github/agents/*.agent.md         Project custom agents
    - .vscode/mcp.json                  MCP server configs
    - .claude/settings.local.json       Hooks-compatible settings

${BOLD}EXAMPLES${NC}
    dot-agents add ~/Github/myproject
    dot-agents add . --name my-api
    dot-agents add ~/work/app --dry-run

EOF
}

cmd_add() {
  # Parse flags
  local project_name=""
  local project_path=""

  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    case $1 in
      --name)
        project_name="$2"
        shift 2
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
        cmd_add_help
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

  # Get project path from remaining args
  if [ ${#REMAINING_ARGS[@]} -eq 0 ]; then
    log_error "Project path required"
    echo ""
    echo "Usage: dot-agents add <path>"
    return 1
  fi

  project_path="${REMAINING_ARGS[0]}"

  # Expand and validate path
  project_path=$(expand_path "$project_path")

  if [[ ! -d "$project_path" ]]; then
    log_error "Directory not found: $project_path"
    return 1
  fi

  # Derive project name from directory if not specified
  if [ -z "$project_name" ]; then
    project_name=$(basename "$project_path")
  fi

  # Validate project name (alphanumeric, hyphens, underscores)
  if ! [[ "$project_name" =~ ^[a-zA-Z0-9_-]+$ ]]; then
    log_error "Invalid project name: $project_name"
    log_info "Use --name to specify a valid name (alphanumeric, hyphens, underscores)"
    return 1
  fi

  local display_path="${project_path/#$HOME/~}"

  log_header "dot-agents add"
  echo -e "Adding project: ${BOLD}$project_name${NC}"
  echo -e "Path: ${DIM}$display_path${NC}"

  # Note if manifest already exists
  if [[ -f "$project_path/.agentsrc.json" ]]; then
    log_info ".agentsrc.json found — you can also use 'dot-agents install' to apply the manifest directly"
  fi

  # Step 1: Scan project
  init_steps 4
  step "Scanning project..."

  # Check if it's a git repository
  if [ -d "$project_path/.git" ]; then
    bullet "ok" "Valid git repository"
  else
    bullet "none" "Not a git repository (optional)"
  fi

  # Check if already registered
  local existing_path
  existing_path=$(config_get_project_path "$project_name")
  if [ -n "$existing_path" ]; then
    if [ "$FORCE" != true ]; then
      bullet "warn" "Already registered at: $existing_path"
      echo ""
      log_info "Use --force to update, or --name to use a different name"
      return 1
    else
      bullet "warn" "Will update existing registration (--force)"
    fi
  else
    bullet "ok" "Not yet registered"
  fi

  # Check for deprecated formats
  local has_deprecated=false
  local platform
  while IFS= read -r platform; do
    if platform_has_deprecated_format "$platform" "$project_path"; then
      bullet "warn" "Found deprecated $(platform_display_name "$platform") config"
      has_deprecated=true
    fi
  done < <(platform_ids)

  if [[ "$has_deprecated" = true ]]; then
    warn_box "Deprecated Formats Found" \
      "After adding this project, run:" \
      "  dot-agents migrate cursorrules $display_path" \
      "  dot-agents migrate claude-json $display_path"
  fi

  # Step 2: Preview what will be created
  step "The following will be created:"

  preview_section "~/.agents/" \
    "rules/$project_name/              (project rules)" \
    "settings/$project_name/           (project settings)" \
    "  └── claude-code.json            (hooks, permissions)" \
    "mcp/$project_name/                (project MCP configs)" \
    "skills/$project_name/             (project skills)" \
    "agents/$project_name/             (project subagents)"

  preview_section "$display_path/" \
    ".cursor/rules/global--*.mdc       (hard links to global rules)" \
    ".cursor/rules/${project_name}--*.mdc  (hard links to project rules)" \
    ".cursor/settings.json             (hard link to settings)" \
    ".cursor/mcp.json                  (hard link to MCP config)" \
    ".claude/agents/*.md               (symlinks to subagents)" \
    ".cursorignore                     (hard link to ignore patterns)" \
    ".claude/rules/${project_name}--*.md  (symlinks to project rules)" \
    ".claude/settings.local.json       (symlink to settings)" \
    ".claude/skills/*/                 (symlinks to skill directories)" \
    ".mcp.json                         (symlink to MCP config)" \
    "AGENTS.md                         (symlink to rules)" \
    ".agents/skills/*/                 (symlinks to project skills)" \
    ".opencode/                        (symlinks to configs)" \
    ".github/copilot-instructions.md   (symlink to instructions)" \
    ".github/agents/*.agent.md         (symlinks to custom agents)" \
    ".vscode/mcp.json                  (symlink to MCP config)"

  info_box "About Link Types" \
    "Cursor uses HARD LINKS (required by IDE)." \
    "Other agents use symlinks for flexibility."

  # Check for existing files that would be replaced (root-level only)
  check_existing_config_files "$project_name" "$project_path"
  local existing_files=()
  [ ${#_CHECK_EXISTING_FILES[@]} -gt 0 ] && existing_files=("${_CHECK_EXISTING_FILES[@]}")

  # Exhaustively scan for all AI config files in the repo
  scan_existing_ai_configs "$project_path"
  local all_ai_configs=()
  [ ${#_SCAN_AI_CONFIGS[@]} -gt 0 ] && all_ai_configs=("${_SCAN_AI_CONFIGS[@]}")

  # Separate files that will be replaced vs. discovered elsewhere
  local discovered_elsewhere=()
  if [ ${#all_ai_configs[@]} -gt 0 ]; then
    for config in "${all_ai_configs[@]}"; do
      local is_root_file=false
      if [ ${#existing_files[@]} -gt 0 ]; then
        for root_file in "${existing_files[@]}"; do
          if [ "$config" = "$root_file" ]; then
            is_root_file=true
            break
          fi
        done
      fi
      if [ "$is_root_file" = false ]; then
        discovered_elsewhere+=("$config")
      fi
    done
  fi

  # Show files that will be replaced
  if [ ${#existing_files[@]} -gt 0 ]; then
    echo ""
    log_section "Files to Replace"
    echo -e "  ${YELLOW}These root-level files will be backed up and replaced with links:${NC}"
    for file in "${existing_files[@]}"; do
      local display_file="${file#$project_path/}"
      local file_type="file"
      [ -L "$file" ] && file_type="symlink"
      echo -e "  ${YELLOW}!${NC} $display_file ${DIM}($file_type)${NC}"
    done

    if [ "$FORCE" != true ]; then
      echo ""
      echo -e "  ${DIM}Backups stored in ~/.agents/resources/$project_name/backups/<timestamp>/${NC}"
    fi
  fi

  # Show discovered configs elsewhere in the repo (informational)
  if [ ${#discovered_elsewhere[@]} -gt 0 ]; then
    echo ""
    log_section "Other AI Configs Discovered"
    echo -e "  ${CYAN}Found AI agent configs elsewhere in the repo (not replaced):${NC}"
    local shown=0
    for file in "${discovered_elsewhere[@]}"; do
      if [ $shown -lt 10 ]; then
        local display_file="${file#$project_path/}"
        echo -e "  ${CYAN}○${NC} $display_file"
        ((shown++))
      fi
    done
    if [ ${#discovered_elsewhere[@]} -gt 10 ]; then
      echo -e "  ${DIM}... and $((${#discovered_elsewhere[@]} - 10)) more${NC}"
    fi
    echo ""
    echo -e "  ${DIM}Consider migrating these to ~/.agents/ for centralized management.${NC}"
  fi

  # Handle dry-run mode
  if [ "$DRY_RUN" = true ]; then
    echo ""
    log_info "DRY RUN - no changes made"
    return 0
  fi

  # Confirm before proceeding
  local confirm_msg="Proceed?"
  if [ ${#existing_files[@]} -gt 0 ]; then
    confirm_msg="Proceed? (${#existing_files[@]} file(s) will be backed up and replaced)"
  fi

  if ! confirm_action "$confirm_msg"; then
    log_info "Add cancelled."
    return 0
  fi

  # Backup existing files before replacing
  local backup_timestamp=""
  if [ ${#existing_files[@]} -gt 0 ]; then
    backup_timestamp=$(date +%Y%m%d-%H%M%S)
    for file in "${existing_files[@]}"; do
      # Skip backup artifacts (never back up a backup)
      [[ "$(basename "$file")" == *.dot-agents-backup ]] && continue
      if [ -e "$file" ] && [ ! -L "$file" ]; then
        # Copy into ~/.agents/resources then delete original — no *.dot-agents-backup in project
        mirror_project_backup_to_resources "$project_name" "$project_path" "$file" "$backup_timestamp"
        rm "$file"
      elif [ -L "$file" ]; then
        rm "$file"  # Remove existing symlinks without backup
      fi
    done
    bullet "ok" "Backed up ${#existing_files[@]} existing file(s)"
    bullet "ok" "Stored backups in ~/.agents/resources/$project_name/backups/$backup_timestamp/"
  fi

  # Step 3: Create project structure
  step "Creating project structure..."
  create_project_dirs_silent "$project_name"
  bullet "ok" "Created ~/.agents/ directories"

  # Restore from active (non-timestamped) resources first, when available
  local restored_count
  restored_count=$(restore_project_from_active_resources "$project_name")
  if [ "$restored_count" -gt 0 ]; then
    bullet "ok" "Restored $restored_count item(s) from ~/.agents/resources/$project_name/"
  fi

  # Copy project settings template if it doesn't exist
  local templates_dir="$SHARE_DIR/templates/standard"
  local project_settings="$AGENTS_HOME/settings/$project_name/claude-code.json"
  if [ ! -f "$project_settings" ]; then
    cp "$templates_dir/settings/project/claude-code.json" "$project_settings" 2>/dev/null || true
    bullet "ok" "Created project settings template"
  fi

  # Step 4: Link to project (only for installed platforms)
  step "Linking to project..."

  # Enable Windows user-home mirroring when project lives under /mnt/c/Users/<user>/...
  dot_agents_set_windows_mirror_context "$project_path"

  local platform
  while IFS= read -r platform; do
    if dot_agents_platform_has_active_backup "$project_name" "$platform" && [ "$VERBOSE" = true ]; then
      bullet "found" "Found active $(platform_display_name "$platform") backup in ~/.agents/resources/$project_name/"
    fi

    if platform_is_installed "$platform"; then
      platform_create_links "$platform" "$project_name" "$project_path"
      bullet "ok" "$(platform_success_message "$platform")"
    elif [ "$VERBOSE" = true ]; then
      bullet "skip" "$(platform_display_name "$platform") not installed"
    fi
  done < <(platform_ids)

  # Register in config.json
  config_add_project "$project_name" "$project_path"
  bullet "ok" "Registered in config.json"

  # Build next steps based on context
  local next_steps=()
  next_steps+=("Add project rules: edit ~/.agents/rules/$project_name/rules.md")
  if [ "$has_deprecated" = true ]; then
    next_steps+=("Migrate deprecated formats: dot-agents migrate detect")
  fi
  next_steps+=("Check applied configs: dot-agents status --audit")
  if [[ -f "$project_path/.agentsrc.json" ]]; then
    next_steps+=("Manifest found — apply it: dot-agents install")
  else
    next_steps+=("Make it git-portable: dot-agents install --generate")
  fi

  success_with_next_steps "Project '$project_name' added successfully!" "${next_steps[@]}"

  show_test_commands \
    "dot-agents status" \
    "ls -la $display_path/.cursor/rules/"
}

# Mirror a project backup file into AGENTS_HOME/resources/<project-slug>/...
mirror_project_backup_to_resources() {
  local project_slug="$1"
  local project_path="$2"
  local source_file="$3"
  local backup_timestamp="$4"

  [ -f "$source_file" ] || return 0

  local rel_path
  rel_path="${source_file#$project_path/}"
  if [ "$rel_path" = "$source_file" ]; then
    # Fallback to basename if source file is not under project path
    rel_path=$(basename "$source_file")
  fi

  # Active (latest) copy — stored under original filename, no .dot-agents-backup suffix
  local active_target="$AGENTS_HOME/resources/$project_slug/$rel_path"
  mkdir -p "$(dirname "$active_target")"
  if ! cp -a "$source_file" "$active_target" 2>/dev/null; then
    log_warn "Could not mirror active backup to ~/.agents/resources/$project_slug/"
  fi

  # Timestamped immutable copy
  if [ -n "$backup_timestamp" ]; then
    local ts_target="$AGENTS_HOME/resources/$project_slug/backups/$backup_timestamp/$rel_path"
    mkdir -p "$(dirname "$ts_target")"
    if ! cp -a "$source_file" "$ts_target" 2>/dev/null; then
      log_warn "Could not mirror timestamped backup to ~/.agents/resources/$project_slug/backups/$backup_timestamp/"
    fi
  fi
}

# Restore project sources from active (non-timestamped) resources.
# Returns number of restored items on stdout.
restore_project_from_active_resources() {
  local project="$1"
  local root="$AGENTS_HOME/resources/$project"
  local restored=0

  [ -d "$root" ] || { echo 0; return 0; }

  local file
  while IFS= read -r -d '' file; do
    local rel="${file#$root/}"

    # Active restore only: skip timestamped backup snapshots
    [[ "$rel" == backups/* ]] && continue

    # Legacy: if a *.dot-agents-backup-suffixed copy exists alongside a canonical copy,
    # skip the suffixed one (canonical takes priority). New backups won't have the suffix.
    if [[ "$rel" == *.dot-agents-backup ]]; then
      local canonical_rel="${rel%.dot-agents-backup}"
      [ -f "$root/$canonical_rel" ] && continue
      # No canonical copy present — use the legacy backup as the source, mapped without suffix
      rel="$canonical_rel"
    fi

    local dest_rel
    dest_rel=$(dot_agents_map_resource_rel_to_agents_dest "$project" "$rel")
    [ -n "$dest_rel" ] || continue

    local dest="$AGENTS_HOME/$dest_rel"
    mkdir -p "$(dirname "$dest")"
    cp -a "$file" "$dest" 2>/dev/null && ((restored++)) || true
  done < <(find "$root" -type f -print0 2>/dev/null)

  echo "$restored"
}

# Create project directories in ~/.agents/ (silent version for bulk)
create_project_dirs_silent() {
  local project="$1"
  local agents_home="$AGENTS_HOME"

  mkdir -p "$agents_home/rules/$project"
  mkdir -p "$agents_home/settings/$project"
  mkdir -p "$agents_home/mcp/$project"
  mkdir -p "$agents_home/skills/$project"
  mkdir -p "$agents_home/agents/$project"
}

# Create project directories in ~/.agents/ (verbose version)
create_project_dirs() {
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
      [ "$VERBOSE" = true ] && log_skip "$display_dir/ (exists)"
    elif [ "$DRY_RUN" = true ]; then
      log_dry "mkdir $display_dir/"
    else
      mkdir -p "$dir"
      log_create "$display_dir/"
    fi
  done
}

# Set up symlinks and hard links in the project directory
setup_project_links() {
  local project="$1"
  local repo="$2"

  # Enable Windows user-home mirroring when project lives under /mnt/c/Users/<user>/...
  dot_agents_set_windows_mirror_context "$repo"

  # Check for deprecated formats and warn
  check_deprecated_formats "$repo"

  # Use generic platform dispatcher for linking (installed platforms only)
  local platform
  while IFS= read -r platform; do
    if ! platform_is_installed "$platform"; then
      continue
    fi

    if [ "$DRY_RUN" = true ]; then
      log_dry "$(platform_dry_run_message "$platform")"
    else
      platform_create_links "$platform" "$project" "$repo"
      log_create "$(platform_success_message "$platform")"
    fi
  done < <(platform_ids)
}

# Check for deprecated config formats and warn
check_deprecated_formats() {
  local repo="$1"
  local found_deprecated=false

  local platform
  while IFS= read -r platform; do
    if platform_has_deprecated_format "$platform" "$repo"; then
      log_warn "Found deprecated $(platform_display_name "$platform") config"
      local details
      details=$(platform_deprecated_details "$platform" "$repo")
      [ -n "$details" ] && log_info "  → $details"
      found_deprecated=true
    fi
  done < <(platform_ids)

  if [ "$found_deprecated" = true ]; then
    echo ""
  fi
}

# Legacy functions removed - now using platform modules:
#   cursor_create_rule_links() from platforms/cursor.sh
#   claude_create_links() from platforms/claude-code.sh
#   codex_create_links() from platforms/codex.sh
#   opencode_create_links() from platforms/opencode.sh
#   copilot_create_links() from platforms/github-copilot.sh

is_managed_cursor_rule_rel() {
  local project="$1"
  local rel_path="$2"
  [[ "$rel_path" == .cursor/rules/* ]] || return 1
  local name
  name=$(basename "$rel_path")
  [[ "$name" == global--* || "$name" == "${project}"--* ]]
}

is_managed_project_output() {
  local project="$1"
  local project_path="$2"
  local file_path="$3"

  local rel_path="${file_path#$project_path/}"
  if [ "$rel_path" = "$file_path" ]; then
    return 1
  fi

  if is_managed_cursor_rule_rel "$project" "$rel_path"; then
    return 0
  fi

  local dest_rel
  dest_rel=$(dot_agents_map_resource_rel_to_agents_dest "$project" "$rel_path")
  [ -n "$dest_rel" ] || return 1

  are_hardlinked "$file_path" "$AGENTS_HOME/$dest_rel"
}

# Check for existing config files that would be replaced by linking
# Sets global _CHECK_EXISTING_FILES array with results
# Usage: check_existing_config_files "project-name" "/path/to/project"
check_existing_config_files() {
  local project="$1"
  local project_path="$2"
  _CHECK_EXISTING_FILES=()

  # Root-level files that would be directly replaced
  local root_files=(
    "$project_path/.cursor/rules"
    "$project_path/.cursor/agents"
    "$project_path/.cursor/settings.json"
    "$project_path/.cursor/mcp.json"
    "$project_path/.cursorignore"
    "$project_path/.claude/rules"
    "$project_path/.claude/agents"
    "$project_path/.claude/settings.local.json"
    "$project_path/.mcp.json"
    "$project_path/.agents/skills"
    "$project_path/AGENTS.md"
    "$project_path/.codex/instructions.md"
    "$project_path/.codex/agents"
    "$project_path/.opencode/instructions.md"
    "$project_path/.opencode/config.json"
    "$project_path/.github/copilot-instructions.md"
    "$project_path/.github/agents"
    "$project_path/.vscode/mcp.json"
  )

  for file in "${root_files[@]}"; do
    if [ -e "$file" ] || [ -L "$file" ]; then
      if [ -d "$file" ]; then
        # Check for files inside directories
        for subfile in "$file"/*; do
          # Skip files that are already backups to prevent cascading backups
          [[ "$subfile" == *.dot-agents-backup ]] && continue
          [ -e "$subfile" ] || continue
          is_managed_project_output "$project" "$project_path" "$subfile" && continue
          _CHECK_EXISTING_FILES+=("$subfile")
        done
      else
        is_managed_project_output "$project" "$project_path" "$file" && continue
        _CHECK_EXISTING_FILES+=("$file")
      fi
    fi
  done
}

# Exhaustively scan for AI agent config files throughout the repo
# Sets global _SCAN_AI_CONFIGS array with results
# Usage: scan_existing_ai_configs "/path/to/project"
scan_existing_ai_configs() {
  local project_path="$1"
  _SCAN_AI_CONFIGS=()

  # Use find to locate files, excluding common directories
  local exclude_dirs=".git node_modules vendor dist build __pycache__ .venv venv"
  local exclude_args=""
  for dir in $exclude_dirs; do
    exclude_args="$exclude_args -path '*/$dir/*' -prune -o"
  done

  # File patterns to look for (these are AI agent config files)
  local patterns=(
    # Cursor ecosystem
    ".cursorrules"
    ".cursor/rules/*.mdc"
    ".cursor/rules/*.md"
    ".cursor/agents/*.md"
    ".cursor/settings.json"
    ".cursor/mcp.json"
    ".cursorignore"
    # Claude Code ecosystem
    "CLAUDE.md"
    ".claude/settings.json"
    ".claude/settings.local.json"
    ".claude/agents/*.md"
    ".claude/skills/*.md"
    ".claude.json"
    ".mcp.json"
    # Codex ecosystem
    "AGENTS.md"
    ".codex/instructions.md"
    ".codex/config.json"
    "codex.md"
    # OpenCode ecosystem
    ".opencode/instructions.md"
    ".opencode/config.json"
    "OPENCODE.md"
    # Aider
    ".aider*"
    "aider.conf*"
    # Windsurf/Continue
    ".continue/*"
    ".windsurfrules"
    # GitHub Copilot
    ".agents/skills/*/SKILL.md"
    ".github/copilot-instructions.md"
    ".github/skills/*/SKILL.md"
    ".github/agents/*.agent.md"
    ".vscode/mcp.json"
    ".github/hooks/*.json"
    "copilot-instructions.md"
    # Generic AI rules
    ".ai-rules"
    ".ai-instructions"
  )

  # Search for each pattern
  for pattern in "${patterns[@]}"; do
    while IFS= read -r -d '' file; do
      _SCAN_AI_CONFIGS+=("$file")
    done < <(find "$project_path" \
      -path '*/.git/*' -prune -o \
      -path '*/node_modules/*' -prune -o \
      -path '*/vendor/*' -prune -o \
      -path '*/dist/*' -prune -o \
      -path '*/build/*' -prune -o \
      -path '*/__pycache__/*' -prune -o \
      -path '*/.venv/*' -prune -o \
      -path '*/venv/*' -prune -o \
      -name "$pattern" -print0 2>/dev/null)
  done

  # Also check for glob patterns that need special handling
  while IFS= read -r -d '' file; do
    _SCAN_AI_CONFIGS+=("$file")
  done < <(find "$project_path" \
    -path '*/.git/*' -prune -o \
    -path '*/node_modules/*' -prune -o \
    -type f \( -name ".aider*" -o -name "aider.conf*" \) -print0 2>/dev/null)
}
