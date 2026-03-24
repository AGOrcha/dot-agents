#!/bin/bash
# dot-agents/lib/commands/refresh.sh
# Refresh managed setup in projects from ~/.agents/

# Source add.sh for shared helpers (create_project_dirs_silent, restore_project_from_active_resources)
_REFRESH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=add.sh
source "$_REFRESH_DIR/add.sh"

cmd_refresh_help() {
  cat << EOF
${BOLD}dot-agents refresh${NC} - Refresh managed setup in projects from ~/.agents/

${BOLD}USAGE${NC}
    dot-agents refresh [project] [options]
    dot-agents refresh [options]              # refresh all managed projects

${BOLD}ARGUMENTS${NC}
    [project]         Project name to refresh (optional). If omitted, all managed projects are refreshed.

${BOLD}OPTIONS${NC}
    --dry-run         Show what would be done without making changes
    --force, -f       Recreate links even if they already exist
    --yes, -y         Auto-confirm (no prompts)
    --verbose, -v     Show detailed output
    --help, -h        Show this help

${BOLD}DESCRIPTION${NC}
    Re-applies links and config from ~/.agents/ into project directories.
    Use after pulling changes to ~/.agents/ or when a project's agent
    config is out of sync. Does not re-register projects or modify
    ~/.agents/ layout—only updates links in each project's tree.

    Writes .agents-refresh in each project with the dot-agents git
    commit (if available) and version that performed the refresh.
    Adds .agents-refresh to the project's .gitignore if not already present.

${BOLD}EXAMPLES${NC}
    dot-agents refresh                  # Refresh all managed projects
    dot-agents refresh myproject        # Refresh one project
    dot-agents refresh --dry-run        # Show what would be updated

EOF
}

REFRESH_MARKER_BASENAME=".agents-refresh"

# Check enabled platforms from config and refresh their detected versions.
# Outputs enabled platform ids (one per line) to stdout.
refresh_enabled_platforms_and_versions() {
  local config_file="$AGENTS_HOME/config.json"
  [ -f "$config_file" ] || return 0

  local enabled_platforms=()
  local platform
  while IFS= read -r platform; do
    local enabled
    enabled=$(config_get_platform_enabled "$platform" 2>/dev/null || true)
    if [ -z "$enabled" ]; then
      # Default to enabled when key is absent
      enabled=true
    fi

    if [ "$enabled" = "true" ]; then
      enabled_platforms+=("$platform")

      local version=""
      if platform_is_installed "$platform"; then
        version=$(platform_version "$platform" || true)
      fi

      config_set_platform_state "$platform" true "$version"

      if [ -n "$version" ]; then
        echo -e "  ${GREEN}✓${NC} $(platform_display_name "$platform") ${DIM}($version)${NC}" >&2
      else
        echo -e "  ${YELLOW}○${NC} $(platform_display_name "$platform") ${DIM}(enabled, not detected)${NC}" >&2
      fi
    fi
  done < <(platform_ids)

  if [ ${#enabled_platforms[@]} -gt 0 ]; then
    printf '%s\n' "${enabled_platforms[@]}"
  fi
}

# Re-apply platform links for a single project, limited to enabled platforms.
refresh_project_links_enabled() {
  local project="$1"
  local repo="$2"
  shift 2
  local enabled_platforms=("$@")

  # Keep Windows mirror behavior consistent with add/refresh workflows
  dot_agents_set_windows_mirror_context "$repo"

  local platform
  for platform in "${enabled_platforms[@]}"; do
    if ! platform_is_installed "$platform"; then
      [ "$VERBOSE" = true ] && log_skip "$(platform_display_name "$platform") not installed"
      continue
    fi

    if [ "$DRY_RUN" = true ]; then
      log_dry "$(platform_dry_run_message "$platform")"
    else
      platform_create_links "$platform" "$project" "$repo"
      log_create "$(platform_success_message "$platform")"
    fi
  done
}

# Write .agents-refresh in project with commit/version that performed refresh
write_refresh_marker() {
  local project_path="$1"
  local commit="${2:-}"
  local describe="${3:-}"
  local marker="$project_path/$REFRESH_MARKER_BASENAME"
  local refreshed_at
  refreshed_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null) || refreshed_at=$(date +"%Y-%m-%dT%H:%M:%S")
  {
    echo "# dot-agents refresh marker — do not edit"
    echo "version=$DOT_AGENTS_VERSION"
    [ -n "$commit" ] && echo "commit=$commit"
    [ -n "$describe" ] && echo "describe=$describe"
    echo "refreshed_at=$refreshed_at"
  } > "$marker"
}

cmd_refresh() {
  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    case $1 in
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
        cmd_refresh_help
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

  if [ ! -f "${AGENTS_HOME:-}/config.json" ]; then
    log_error "Not initialized. Run 'dot-agents init' first."
    return 1
  fi

  local project_filter=""
  [ ${#REMAINING_ARGS[@]} -gt 0 ] && project_filter="${REMAINING_ARGS[0]}"

  local projects
  if [ -n "$project_filter" ]; then
    local path
    path=$(config_get_project_path "$project_filter")
    if [ -z "$path" ]; then
      log_error "Project not found: $project_filter"
      return 1
    fi
    projects="$project_filter"
  else
    projects=$(config_list_projects)
  fi

  if [ -z "$projects" ]; then
    log_info "No managed projects. Add one with: dot-agents add <path>"
    return 0
  fi

  # Resolve dot-agents git commit once (if available)
  local refresh_commit="" refresh_describe=""
  local repo_root
  repo_root=$(dot_agents_repo_root 2>/dev/null) || true
  if [ -n "$repo_root" ] && [ -d "$repo_root/.git" ]; then
    refresh_commit=$(git -C "$repo_root" rev-parse HEAD 2>/dev/null) || true
    [ -n "$refresh_commit" ] && refresh_describe=$(git -C "$repo_root" describe --always --tags 2>/dev/null) || true
  fi

  log_header "dot-agents refresh"

  log_section "Enabled Platforms"
  local enabled_platforms=()
  while IFS= read -r p; do
    [ -n "$p" ] && enabled_platforms+=("$p")
  done < <(refresh_enabled_platforms_and_versions)

  if [ ${#enabled_platforms[@]} -eq 0 ]; then
    log_warn "No enabled platforms in config.json. Nothing to refresh."
    return 0
  fi

  local count=0
  while read -r name; do
    [ -z "$name" ] && continue
    local path
    path=$(config_get_project_path "$name")
    if [ -z "$path" ] || [ ! -d "$path" ]; then
      log_warn "Skipping $name: path not found or not a directory"
      continue
    fi
    echo ""
    echo -e "${BOLD}Refreshing: $name${NC}"
    echo -e "  ${DIM}$path${NC}"
    if [ "$DRY_RUN" != true ]; then
      create_project_dirs_silent "$name"
      local restored_count
      restored_count=$(restore_project_from_active_resources "$name")
      if [ "$restored_count" -gt 0 ]; then
        [ "$VERBOSE" = true ] && log_info "Restored $restored_count item(s) from ~/.agents/resources/$name/"
      fi
    fi
    refresh_project_links_enabled "$name" "$path" "${enabled_platforms[@]}"
    if [ "$DRY_RUN" != true ]; then
      write_refresh_marker "$path" "$refresh_commit" "$refresh_describe"
    else
      [ -n "$refresh_commit" ] && log_dry "Write .agents-refresh (commit=$refresh_commit); add to .gitignore if needed" || log_dry "Write .agents-refresh (version=$DOT_AGENTS_VERSION); add to .gitignore if needed"
    fi
    ((count++)) || true
  done <<< "$projects"

  if [ "$count" -gt 0 ]; then
    echo ""
    log_success "Refreshed $count project(s)."
  fi
}
