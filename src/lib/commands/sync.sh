#!/bin/bash
# dot-agents/lib/commands/sync.sh
# Git sync utilities for ~/.agents/

cmd_sync_help() {
  cat << EOF
${BOLD}dot-agents sync${NC} - Git sync utilities for ~/.agents/

${BOLD}USAGE${NC}
    dot-agents sync <subcommand> [options]

${BOLD}SUBCOMMANDS${NC}
    init              Initialize git repository in ~/.agents/
    status            Show git status of ~/.agents/
    commit [message]  Commit all changes with optional message
    push              Push commits to remote
    pull              Pull changes from remote
    log               Show recent commit history

${BOLD}OPTIONS${NC}
    --dry-run         Show what would be done without making changes
    --yes, -y         Auto-confirm prompts
    --force, -f       Same as --yes
    --verbose, -v     Show detailed output
    --help, -h        Show this help

${BOLD}DESCRIPTION${NC}
    Manages git operations for your ~/.agents/ configuration directory.

    This allows you to:
    - Version control your AI agent configurations
    - Sync configs across multiple machines
    - Track changes over time

    First time setup:
    1. dot-agents sync init
    2. Create a private GitHub repo
    3. git remote add origin <your-repo-url>
    4. dot-agents sync push

${BOLD}EXAMPLES${NC}
    dot-agents sync init              # Initialize git repo
    dot-agents sync status            # See what's changed
    dot-agents sync commit "Add new rules"
    dot-agents sync push              # Push to remote

EOF
}

cmd_sync() {
  # Parse flags and get subcommand
  local subcmd=""

  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    case $1 in
      --dry-run)
        DRY_RUN=true
        shift
        ;;
      --yes|-y)
        YES=true
        shift
        ;;
      --force|-f)
        FORCE=true
        shift
        ;;
      --verbose|-v)
        VERBOSE=true
        shift
        ;;
      --help|-h)
        cmd_sync_help
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

  # Get subcommand from remaining args
  if [ ${#REMAINING_ARGS[@]} -eq 0 ]; then
    cmd_sync_help
    return 0
  fi

  subcmd="${REMAINING_ARGS[0]}"

  # Route to subcommand
  case "$subcmd" in
    init)
      sync_init
      ;;
    status)
      sync_status
      ;;
    commit)
      local message="${REMAINING_ARGS[1]:-}"
      sync_commit "$message"
      ;;
    push)
      sync_push
      ;;
    pull)
      sync_pull
      ;;
    log)
      sync_log
      ;;
    *)
      log_error "Unknown subcommand: $subcmd"
      echo ""
      echo "Run 'dot-agents sync --help' for available subcommands."
      return 1
      ;;
  esac
}

# Check if git is available
check_git() {
  if ! command -v git &>/dev/null; then
    log_error "git is not installed"
    echo "Install git to use sync features."
    return 1
  fi
  return 0
}

# Check if ~/.agents/ is a git repo
is_agents_git_repo() {
  [ -d "$AGENTS_HOME/.git" ]
}

# Initialize git repository in ~/.agents/
sync_init() {
  check_git || return 1

  log_header "Initialize git repository"
  log_info "Directory: $AGENTS_HOME"
  show_mode_banner

  if is_agents_git_repo; then
    log_info "Git repository already initialized."
    echo ""

    # Show current status
    cd "$AGENTS_HOME"
    local remote
    remote=$(git remote -v 2>/dev/null | head -1 || echo "")

    if [ -n "$remote" ]; then
      log_info "Remote configured:"
      git remote -v | head -2
    else
      echo "Next steps:"
      echo "  1. Create a private repository on GitHub/GitLab"
      echo "  2. Add the remote:"
      echo "     cd $AGENTS_HOME"
      echo "     git remote add origin git@github.com:YOU/agents-config.git"
      echo "  3. Push your config:"
      echo "     dot-agents sync push"
    fi
    return 0
  fi

  if [ "$DRY_RUN" = true ]; then
    log_dry "git init $AGENTS_HOME"
    log_dry "create .gitignore"
    log_dry "git add ."
    log_dry "git commit -m 'Initial commit'"
    return 0
  fi

  # Initialize git repo
  cd "$AGENTS_HOME"
  git init -q
  log_success "Initialized git repository"

  # Create .gitignore if it doesn't exist
  if [ ! -f ".gitignore" ]; then
    cat > .gitignore << 'GITIGNORE'
# dot-agents generated files
.DS_Store
*.swp
*.swo
*~

# Local overrides (not synced)
local/*
!local/.gitkeep

# Secrets (add your own patterns)
# secrets/
# *.secret
# .env*
GITIGNORE
    log_create ".gitignore"
  fi

  # Create local/.gitkeep if local/ exists
  if [ -d "local" ] && [ ! -f "local/.gitkeep" ]; then
    touch "local/.gitkeep"
  fi

  # Initial commit
  git add .
  git commit -q -m "Initial dot-agents configuration"
  log_success "Created initial commit"

  echo ""
  log_info "Next steps:"
  echo "  1. Create a private repository on GitHub/GitLab"
  echo "  2. Add the remote:"
  echo "     cd $AGENTS_HOME"
  echo "     git remote add origin git@github.com:YOU/agents-config.git"
  echo "  3. Push your config:"
  echo "     dot-agents sync push"
}

# Show git status of ~/.agents/
sync_status() {
  check_git || return 1

  if ! is_agents_git_repo; then
    log_error "~/.agents/ is not a git repository"
    echo "Run 'dot-agents sync init' first."
    return 1
  fi

  log_header "Sync status"
  log_info "Directory: $AGENTS_HOME"
  echo ""

  cd "$AGENTS_HOME"

  # Show remote info
  local remote
  remote=$(git remote -v 2>/dev/null | grep '(push)' | head -1 || echo "")
  if [ -n "$remote" ]; then
    echo -e "${DIM}Remote: $(echo "$remote" | awk '{print $2}')${NC}"
    echo ""
  else
    echo -e "${YELLOW}No remote configured${NC}"
    echo ""
  fi

  # Show current branch
  local branch
  branch=$(git branch --show-current 2>/dev/null || echo "unknown")
  echo -e "Branch: ${CYAN}$branch${NC}"

  # Check if ahead/behind remote
  if [ -n "$remote" ]; then
    local ahead behind
    ahead=$(git rev-list --count @{upstream}..HEAD 2>/dev/null || echo "0")
    behind=$(git rev-list --count HEAD..@{upstream} 2>/dev/null || echo "0")

    if [ "$ahead" -gt 0 ] && [ "$behind" -gt 0 ]; then
      echo -e "Status: ${YELLOW}$ahead ahead, $behind behind${NC}"
    elif [ "$ahead" -gt 0 ]; then
      echo -e "Status: ${GREEN}$ahead commit(s) ahead${NC}"
    elif [ "$behind" -gt 0 ]; then
      echo -e "Status: ${YELLOW}$behind commit(s) behind${NC}"
    else
      echo -e "Status: ${GREEN}Up to date${NC}"
    fi
  fi
  echo ""

  # Show changes
  local staged unstaged untracked
  staged=$(git diff --cached --name-only 2>/dev/null | wc -l | tr -d ' ')
  unstaged=$(git diff --name-only 2>/dev/null | wc -l | tr -d ' ')
  untracked=$(git ls-files --others --exclude-standard 2>/dev/null | wc -l | tr -d ' ')

  if [ "$staged" -gt 0 ] || [ "$unstaged" -gt 0 ] || [ "$untracked" -gt 0 ]; then
    echo "Changes:"
    [ "$staged" -gt 0 ] && echo -e "  ${GREEN}$staged staged${NC}"
    [ "$unstaged" -gt 0 ] && echo -e "  ${YELLOW}$unstaged modified${NC}"
    [ "$untracked" -gt 0 ] && echo -e "  ${RED}$untracked untracked${NC}"
    echo ""

    if [ "$VERBOSE" = true ]; then
      git status --short
    else
      echo "Use -v for file list, or run 'git status' in ~/.agents/"
    fi
  else
    echo -e "${GREEN}Working directory clean${NC}"
  fi
}

# Commit all changes
sync_commit() {
  local message="${1:-}"
  check_git || return 1

  if ! is_agents_git_repo; then
    log_error "~/.agents/ is not a git repository"
    echo "Run 'dot-agents sync init' first."
    return 1
  fi

  cd "$AGENTS_HOME"

  # Check for changes
  local has_changes=false
  if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
    has_changes=true
  fi

  if [ "$has_changes" = false ]; then
    log_info "No changes to commit."
    return 0
  fi

  # Generate commit message if not provided
  if [ -z "$message" ]; then
    local modified added deleted
    modified=$(git diff --name-only 2>/dev/null | wc -l | tr -d ' ')
    added=$(git ls-files --others --exclude-standard 2>/dev/null | wc -l | tr -d ' ')
    deleted=$(git diff --name-only --diff-filter=D 2>/dev/null | wc -l | tr -d ' ')

    local parts=()
    [ "$modified" -gt 0 ] && parts+=("$modified modified")
    [ "$added" -gt 0 ] && parts+=("$added new")
    [ "$deleted" -gt 0 ] && parts+=("$deleted deleted")

    if [ ${#parts[@]} -gt 0 ]; then
      message="Update configs: $(IFS=', '; echo "${parts[*]}")"
    else
      message="Update agent configurations"
    fi
  fi

  if [ "$DRY_RUN" = true ]; then
    log_dry "git add ."
    log_dry "git commit -m \"$message\""
    return 0
  fi

  # Stage all changes and commit
  git add .
  git commit -q -m "$message"

  log_success "Committed: $message"

  # Show push hint if there's a remote
  if git remote -v | grep -q 'origin'; then
    echo ""
    log_info "Run 'dot-agents sync push' to push to remote."
  fi
}

# Push to remote
sync_push() {
  check_git || return 1

  if ! is_agents_git_repo; then
    log_error "~/.agents/ is not a git repository"
    echo "Run 'dot-agents sync init' first."
    return 1
  fi

  cd "$AGENTS_HOME"

  # Check if remote is configured
  if ! git remote -v | grep -q 'origin'; then
    log_error "No remote configured"
    echo ""
    echo "Add a remote first:"
    echo "  cd $AGENTS_HOME"
    echo "  git remote add origin git@github.com:YOU/agents-config.git"
    return 1
  fi

  local branch
  branch=$(git branch --show-current 2>/dev/null || echo "main")

  # Check if there's anything to push
  local ahead
  ahead=$(git rev-list --count @{upstream}..HEAD 2>/dev/null || echo "all")

  if [ "$ahead" = "0" ]; then
    log_info "Already up to date with remote."
    return 0
  fi

  log_header "dot-agents sync push"

  # Get remote URL for display
  local remote_url
  remote_url=$(git remote get-url origin 2>/dev/null || echo "origin")
  echo -e "Remote: ${DIM}$remote_url${NC}"
  echo -e "Branch: ${CYAN}$branch${NC}"
  echo ""

  # Show what will be pushed
  if [ "$ahead" = "all" ]; then
    echo -e "${BOLD}Initial push${NC} - all commits will be pushed"
  else
    echo -e "${BOLD}$ahead commit(s)${NC} will be pushed:"
    echo ""
    git log --oneline @{upstream}..HEAD 2>/dev/null | head -10
    [ "$ahead" -gt 10 ] && echo "  ... and $((ahead - 10)) more"
  fi
  echo ""

  if [ "$DRY_RUN" = true ]; then
    log_info "DRY RUN - no changes made"
    return 0
  fi

  # Confirm before pushing
  if ! confirm_action "Push to remote?"; then
    log_info "Push cancelled."
    return 0
  fi

  echo ""
  log_info "Pushing to remote..."
  if git push -u origin "$branch" 2>&1; then
    echo ""
    log_success "Pushed successfully!"
    echo ""
    echo -e "${DIM}Your agent configurations are now synced to the remote.${NC}"
  else
    log_error "Push failed"
    return 1
  fi
}

# Pull from remote
sync_pull() {
  check_git || return 1

  if ! is_agents_git_repo; then
    log_error "~/.agents/ is not a git repository"
    echo "Run 'dot-agents sync init' first."
    return 1
  fi

  cd "$AGENTS_HOME"

  # Check if remote is configured
  if ! git remote -v | grep -q 'origin'; then
    log_error "No remote configured"
    return 1
  fi

  if [ "$DRY_RUN" = true ]; then
    log_dry "git pull"
    log_dry "Prompt: Refresh managed projects with pulled changes?"
    return 0
  fi

  log_info "Pulling from remote..."
  if git pull 2>&1; then
    log_success "Pulled successfully"
  else
    log_error "Pull failed"
    return 1
  fi

  # Offer to refresh managed projects so pulled MCP/rule changes take effect.
  echo ""
  if [ "$YES" = true ] || confirm_action "Refresh managed projects with pulled changes?"; then
    echo ""
    cmd_refresh
  else
    echo ""
    log_info "Run 'dot-agents refresh' to apply changes to managed projects."
  fi

  # Suggest install for projects with git-source manifests
  if command -v jq >/dev/null 2>&1; then
    local manifest_projects=()
    while read -r name; do
      [[ -z "$name" ]] && continue
      local proj_path
      proj_path=$(config_get_project_path "$name")
      if [[ -n "$proj_path" ]] && [[ -f "$proj_path/.agentsrc.json" ]]; then
        local has_git
        has_git=$(jq -r '.sources[]? | select(.type=="git") | .url' "$proj_path/.agentsrc.json" 2>/dev/null | head -1)
        if [[ -n "$has_git" ]]; then
          manifest_projects+=("$name")
        fi
      fi
    done <<< "$(config_list_projects)"
    if [[ ${#manifest_projects[@]} -gt 0 ]]; then
      echo ""
      log_info "Projects with git-source manifests — run 'dot-agents install' in each to pick up new resources:"
      for p in "${manifest_projects[@]}"; do
        echo "    $p"
      done
    fi
  fi
}

# Show recent commit log
sync_log() {
  check_git || return 1

  if ! is_agents_git_repo; then
    log_error "~/.agents/ is not a git repository"
    echo "Run 'dot-agents sync init' first."
    return 1
  fi

  cd "$AGENTS_HOME"

  log_header "Recent commits"
  echo ""

  git log --oneline --decorate -n 10
}
