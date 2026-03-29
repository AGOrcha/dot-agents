#!/bin/bash
# dot-agents/lib/commands/import.sh
# Import project/global platform configs into ~/.agents/

_IMPORT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=add.sh
source "$_IMPORT_DIR/add.sh"

cmd_import_help() {
  cat << EOF
${BOLD}dot-agents import${NC} - Import configs into ~/.agents/

${BOLD}USAGE${NC}
    dot-agents import [project] [options]

${BOLD}OPTIONS${NC}
    --scope <project|global|all>    Import scope (default: all)
    --dry-run                        Show what would be done
    --yes, -y                        Auto-confirm conflicts
    --verbose, -v                    Show detailed output
    --help, -h                       Show this help
EOF
  return 0
}

_import_project_candidates() {
  local project="$1"
  local repo="$2"
  local rel

  for rel in \
    ".cursor/settings.json" ".cursor/mcp.json" ".cursorignore" \
    ".claude/settings.local.json" ".mcp.json" ".vscode/mcp.json" \
    "opencode.json" "AGENTS.md" ".codex/instructions.md" ".codex/rules.md" \
    ".codex/config.toml" ".github/copilot-instructions.md"; do
    [[ -e "$repo/$rel" || -L "$repo/$rel" ]] || continue
    local dest_rel
    dest_rel=$(dot_agents_map_resource_rel_to_agents_dest "$project" "$rel")
    [[ -n "$dest_rel" ]] || continue
    echo "$project|$repo|$repo/$rel|$dest_rel"
  done

  local dir path dest_rel rel_path
  for dir in ".cursor/rules" ".agents/skills" ".claude/skills" ".github/agents" ".codex/agents"; do
    [[ -d "$repo/$dir" ]] || continue
    while IFS= read -r -d '' path; do
      rel_path="${path#$repo/}"
      dest_rel=$(dot_agents_map_resource_rel_to_agents_dest "$project" "$rel_path")
      [[ -n "$dest_rel" ]] || continue
      echo "$project|$repo|$path|$dest_rel"
    done < <(find "$repo/$dir" -type f -print0 2>/dev/null)
  done
  return 0
}

scan_global_import_candidates() {
  local rel src dest_rel
  for rel in ".claude/settings.json" ".cursor/settings.json" ".cursor/mcp.json" ".claude/CLAUDE.md" ".codex/config.toml"; do
    src="$HOME/$rel"
    [[ -e "$src" || -L "$src" ]] || continue
    dest_rel=$(map_global_rel_to_agents_dest "$rel")
    [[ -n "$dest_rel" ]] || continue
    echo "global|$HOME|$src|$dest_rel"
  done
  return 0
}

cmd_import() {
  local scope="all"
  local arg next_arg
  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    arg="$1"
    next_arg="${2:-}"
    case "$arg" in
      --scope)
        scope="$next_arg"
        shift 2
        ;;
      --dry-run)
        DRY_RUN=true
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
        cmd_import_help
        return 0
        ;;
      -*)
        log_error "Unknown option: $arg"
        return 1
        ;;
      *)
        REMAINING_ARGS+=("$arg")
        shift
        ;;
    esac
  done

  scope=$(echo "$scope" | tr '[:upper:]' '[:lower:]')
  if [[ "$scope" != "project" && "$scope" != "global" && "$scope" != "all" ]]; then
    log_error "Invalid --scope value: $scope"
    return 1
  fi

  local project_filter="${REMAINING_ARGS[0]:-}"
  local candidates=()
  local project path line
  if [[ "$scope" == "project" || "$scope" == "all" ]]; then
    local projects
    if [[ -n "$project_filter" ]]; then
      path=$(config_get_project_path "$project_filter")
      [[ -n "$path" ]] || { log_error "Project not found: $project_filter"; return 1; }
      projects="$project_filter"
    else
      projects=$(config_list_projects)
    fi

    while read -r project; do
      [[ -n "$project" ]] || continue
      path=$(config_get_project_path "$project")
      [[ -d "$path" ]] || continue
      while IFS= read -r line; do
        [[ -n "$line" ]] && candidates+=("$line")
      done < <(_import_project_candidates "$project" "$path")
    done <<< "$projects"
  fi

  if [[ "$scope" == "global" || "$scope" == "all" ]]; then
    while IFS= read -r line; do
      [[ -n "$line" ]] && candidates+=("$line")
    done < <(scan_global_import_candidates)
  fi

  if [[ ${#candidates[@]} -eq 0 ]]; then
    log_info "No import candidates found."
    return 0
  fi

  local timestamp
  timestamp=$(date +%Y%m%d-%H%M%S)
  local imported=0 skipped=0
  local src_mtime dest_mtime
  for line in "${candidates[@]}"; do
    IFS='|' read -r project path src dest_rel <<< "$line"
    local dest="$AGENTS_HOME/$dest_rel"
    [[ -f "$src" ]] || continue

    if [[ ! -e "$dest" ]]; then
      if [[ "$DRY_RUN" == true ]]; then
        log_dry "Import ${src#$path/} -> $dest_rel"
        ((imported++)) || true
        continue
      fi
      mirror_project_backup_to_resources "$project" "$path" "$src" "$timestamp"
      mkdir -p "$(dirname "$dest")"
      cp -a "$src" "$dest" 2>/dev/null || { ((skipped++)) || true; continue; }
      log_create "Imported $dest_rel"
      ((imported++)) || true
      continue
    fi

    if cmp -s "$src" "$dest"; then
      continue
    fi

    src_mtime=$(stat -f "%m" "$src" 2>/dev/null || echo 0)
    dest_mtime=$(stat -f "%m" "$dest" 2>/dev/null || echo 0)
    local newer="destination"
    [[ "$src_mtime" -gt "$dest_mtime" ]] && newer="source"
    if [[ "$YES" != true ]] && ! confirm_action "Import conflict for $dest_rel (newer=$newer). Overwrite ~/.agents copy?"; then
      ((skipped++)) || true
      continue
    fi

    if [[ "$DRY_RUN" == true ]]; then
      log_dry "Replace $dest_rel from $src"
      ((imported++)) || true
      continue
    fi

    mirror_project_backup_to_resources "$project" "$AGENTS_HOME" "$dest" "$timestamp"
    mirror_project_backup_to_resources "$project" "$path" "$src" "$timestamp"
    cp -a "$src" "$dest" 2>/dev/null || { ((skipped++)) || true; continue; }
    log_create "Updated $dest_rel"
    ((imported++)) || true
  done

  log_success "Import complete: $imported imported, $skipped skipped."
  return 0
}
