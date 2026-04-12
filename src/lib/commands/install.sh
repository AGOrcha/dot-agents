#!/bin/bash
# dot-agents/lib/commands/install.sh
# Set up a project from a .agentsrc.json manifest (git-portable install)

# Source add.sh for shared helpers
_INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=add.sh
if ! declare -F create_project_dirs_silent >/dev/null 2>&1; then
  source "$_INSTALL_DIR/add.sh"
fi

REFRESH_MARKER_BASENAME=".agents-refresh"

write_refresh_marker() {
  local project_path="$1"
  local commit="${2:-}"
  local describe="${3:-}"
  local marker="$project_path/$REFRESH_MARKER_BASENAME"
  local refreshed_at
  refreshed_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null) || refreshed_at=$(date +"%Y-%m-%dT%H:%M:%S")
  {
    echo "# dot-agents refresh marker — do not edit"
    echo "version=${DOT_AGENTS_VERSION:-unknown}"
    [ -n "$commit" ] && echo "commit=$commit"
    [ -n "$describe" ] && echo "describe=$describe"
    echo "refreshed_at=$refreshed_at"
  } > "$marker"
}

AGENTSRC_FILE=".agentsrc.json"

cmd_install_help() {
  cat << EOF
${BOLD}dot-agents install${NC} - Set up a project from .agentsrc.json manifest

${BOLD}USAGE${NC}
    dot-agents install [options]

${BOLD}OPTIONS${NC}
    --generate        Create .agentsrc.json from current ~/.agents/ state
    --strict          Fail if any declared resource is not found (default: warn)
    --dry-run         Show what would be done without making changes
    --force, -f       Re-fetch git sources even if already cached
    --yes, -y         Auto-confirm prompts
    --verbose, -v     Show detailed output
    --help, -h        Show this help

${BOLD}DESCRIPTION${NC}
    Reads .agentsrc.json in the current directory and wires up all declared
    resources (skills, rules, agents, hooks, MCP configs, settings) by
    creating the appropriate platform-specific symlinks and hard links.

    Solves the "works only on my machine" problem: commit .agentsrc.json to
    git so any contributor can run 'dot-agents install' after cloning.

    Sources can be local (~/.agents/) or a remote git repository. Remote
    sources are cloned once and cached in ~/.cache/dot-agents/sources/.

    --generate scans the current ~/.agents/ state for this project and
    writes a .agentsrc.json you can then commit.

${BOLD}MANIFEST FORMAT${NC}
    {
      "version": 1,
      "project": "my-project",
      "skills": ["agent-start", "self-review"],
      "rules": ["global", "project"],
      "agents": [],
      "hooks": true,
      "mcp": true,
      "settings": true,
      "sources": [
        {"type": "local"},
        {"type": "git", "url": "https://github.com/team/agents.git", "ref": "main"}
      ]
    }

${BOLD}EXAMPLES${NC}
    dot-agents install                   # Set up from .agentsrc.json
    dot-agents install --generate        # Create .agentsrc.json first
    dot-agents install --dry-run         # Preview without changes
    dot-agents install --force           # Re-fetch remote sources

EOF
  return 0
}

cmd_install() {
  local do_generate=false
  local strict=false

  parse_common_flags "$@"
  set -- "${REMAINING_ARGS[@]+"${REMAINING_ARGS[@]}"}"

  if [[ "${SHOW_HELP:-false}" == true ]]; then
    cmd_install_help
    return 0
  fi

  while [[ $# -gt 0 ]]; do
    local arg="$1"
    case "$arg" in
      --generate)
        do_generate=true
        shift
        ;;
      --strict)
        strict=true
        shift
        ;;
      -*)
        log_error "Unknown option: $arg"
        return 1
        ;;
      *)
        shift
        ;;
    esac
  done

  if [[ "$do_generate" == true ]]; then
    install_generate
  else
    install_run "$strict"
  fi
}

# ─── install_run ─────────────────────────────────────────────────────────────

install_run() {
  local strict="${1:-false}"
  local project_path="$PWD"
  local manifest="$project_path/$AGENTSRC_FILE"

  log_header "dot-agents install"

  # 1. Verify manifest exists
  if [[ ! -f "$manifest" ]]; then
    log_error "$AGENTSRC_FILE not found in current directory"
    echo ""
    echo "  Run 'dot-agents install --generate' to create one, or"
    echo "  run 'dot-agents add .' to register this project first."
    return 1
  fi

  # 2. Verify ~/.agents/ is initialized
  if [[ ! -f "${AGENTS_HOME:-}/config.json" ]]; then
    log_error "~/.agents/ not initialized. Run 'dot-agents init' first."
    return 1
  fi

  # 3. Read manifest
  if ! _agentsrc_read "$manifest"; then
    log_error "Failed to read $AGENTSRC_FILE — check it is valid JSON"
    return 1
  fi
  # Globals set by _agentsrc_read:
  #   AGENTSRC_PROJECT, AGENTSRC_SKILLS (space-sep), AGENTSRC_RULES (space-sep),
  #   AGENTSRC_AGENTS (space-sep), AGENTSRC_HOOKS, AGENTSRC_MCP,
  #   AGENTSRC_SETTINGS, AGENTSRC_SOURCES_JSON

  local project_name="${AGENTSRC_PROJECT:-}"
  if [[ -z "$project_name" ]]; then
    project_name=$(basename "$project_path")
  fi

  local display_path="${project_path/#$HOME/~}"
  echo -e "Project: ${BOLD}$project_name${NC}"
  echo -e "Path:    ${DIM}$display_path${NC}"
  show_mode_banner

  # 4. Resolve and fetch sources
  log_section "Resolving sources"
  local resolved_sources=()
  if ! _agentsrc_resolve_sources resolved_sources; then
    log_warn "Source resolution had errors — some resources may be unavailable"
  fi

  # 5. Populate ~/.agents/skills/{project}/ and agents/{project}/ from sources
  if [[ ${#resolved_sources[@]} -gt 0 ]]; then
    local skill_name
    for skill_name in $AGENTSRC_SKILLS; do
      _agentsrc_link_resource_from_sources "skills" "$skill_name" "$project_name" "${resolved_sources[@]}"
      local rc=$?
      if [[ $rc -ne 0 ]]; then
        if [[ "$strict" == true ]]; then
          log_error "Skill '$skill_name' not found in any source (--strict mode)"
          return 1
        else
          log_warn "Skill '$skill_name' not found in any source — skipping"
        fi
      fi
    done

    local agent_name
    for agent_name in $AGENTSRC_AGENTS; do
      _agentsrc_link_resource_from_sources "agents" "$agent_name" "$project_name" "${resolved_sources[@]}"
      local rc=$?
      if [[ $rc -ne 0 ]]; then
        if [[ "$strict" == true ]]; then
          log_error "Agent '$agent_name' not found in any source (--strict mode)"
          return 1
        else
          log_warn "Agent '$agent_name' not found in any source — skipping"
        fi
      fi
    done
  fi

  # 6. Create project dirs in ~/.agents/
  if [[ "$DRY_RUN" != true ]]; then
    create_project_dirs_silent "$project_name"
    bullet "ok" "Ensured ~/.agents/ project directories"
  else
    log_dry "create ~/.agents/ directories for '$project_name'"
  fi

  # 7. Register in config.json
  local existing_path
  existing_path=$(config_get_project_path "$project_name")
  if [[ -z "$existing_path" ]]; then
    if [[ "$DRY_RUN" != true ]]; then
      config_add_project "$project_name" "$project_path"
      bullet "ok" "Registered '$project_name' in config.json"
    else
      log_dry "register '$project_name' in config.json"
    fi
  else
    bullet "skip" "Already registered in config.json"
  fi

  # 8. Create platform links (handles rules, hooks, mcp, settings, skills, agents)
  log_section "Creating platform links"
  dot_agents_set_windows_mirror_context "$project_path"

  local platform
  while IFS= read -r platform; do
    if platform_is_installed "$platform"; then
      if [[ "$DRY_RUN" == true ]]; then
        log_dry "$(platform_dry_run_message "$platform")"
      else
        platform_create_links "$platform" "$project_name" "$project_path"
        bullet "ok" "$(platform_success_message "$platform")"
      fi
    elif [[ "$VERBOSE" == true ]]; then
      bullet "skip" "$(platform_display_name "$platform") not installed"
    fi
  done < <(platform_ids)

  # 9. Write .agents-refresh marker
  if [[ "$DRY_RUN" != true ]]; then
    local refresh_commit="" refresh_describe=""
    local repo_root
    repo_root=$(dot_agents_repo_root 2>/dev/null) || true
    if [[ -n "$repo_root" ]] && [[ -d "$repo_root/.git" ]]; then
      refresh_commit=$(git -C "$repo_root" rev-parse HEAD 2>/dev/null) || true
      [[ -n "$refresh_commit" ]] && refresh_describe=$(git -C "$repo_root" describe --always --tags 2>/dev/null) || true
    fi
    write_refresh_marker "$project_path" "$refresh_commit" "$refresh_describe"
    bullet "ok" "Wrote .agents-refresh marker"
  fi

  success_with_next_steps "Project '$project_name' installed successfully!" \
    "Check links: dot-agents status --audit" \
    "Update manifest: dot-agents install --generate"
}

# ─── install_generate ────────────────────────────────────────────────────────

install_generate() {
  local project_path="$PWD"
  local manifest="$project_path/$AGENTSRC_FILE"

  log_header "dot-agents install --generate"

  if ! _json_has_jq; then
    log_error "jq is required to generate $AGENTSRC_FILE"
    return 1
  fi

  # Derive project name
  local project_name
  project_name=$(_agentsrc_find_project_by_path "$project_path")
  if [[ -z "$project_name" ]]; then
    project_name=$(basename "$project_path")
    log_info "Project not registered — using directory name: $project_name"
  fi

  # Collect skills (global + project)
  local skills=()
  local skill_dir
  for skill_dir in "$AGENTS_HOME/skills/global"/*/; do
    [[ -d "$skill_dir" ]] && [[ -f "$skill_dir/SKILL.md" ]] || continue
    skills+=("$(basename "$skill_dir")")
  done
  for skill_dir in "$AGENTS_HOME/skills/$project_name"/*/; do
    [[ -d "$skill_dir" ]] && [[ -f "$skill_dir/SKILL.md" ]] || continue
    skills+=("$(basename "$skill_dir")")
  done

  # Collect agents (global + project)
  local agents=()
  local agent_dir
  for agent_dir in "$AGENTS_HOME/agents/global"/*/; do
    [[ -d "$agent_dir" ]] && [[ -f "$agent_dir/AGENT.md" ]] || continue
    agents+=("$(basename "$agent_dir")")
  done
  for agent_dir in "$AGENTS_HOME/agents/$project_name"/*/; do
    [[ -d "$agent_dir" ]] && [[ -f "$agent_dir/AGENT.md" ]] || continue
    agents+=("$(basename "$agent_dir")")
  done

  # Determine rule scopes
  local rules=("global")
  if [[ -d "$AGENTS_HOME/rules/$project_name" ]]; then
    local has_project_rules=false
    for f in "$AGENTS_HOME/rules/$project_name"/*.{md,mdc,txt}; do
      [[ -f "$f" ]] && has_project_rules=true && break
    done
    [[ "$has_project_rules" == true ]] && rules+=("project")
  fi

  # Detect hooks — list which event types have non-empty entries
  local hooks_val=false
  local hooks_settings="$AGENTS_HOME/settings/$project_name/claude-code.json"
  if [[ -f "$hooks_settings" ]]; then
    local hook_events=()
    while IFS= read -r evt; do
      hook_events+=("$evt")
    done < <(jq -r '.hooks | to_entries[] | select(.value | length > 0) | .key' \
               "$hooks_settings" 2>/dev/null | sort)
    if [[ ${#hook_events[@]} -gt 0 ]]; then
      hooks_val=$(printf '%s\n' "${hook_events[@]}" | jq -R . | jq -s .)
    fi
  fi

  # Detect MCP — list named servers from first MCP config found
  local mcp_val=false
  local mcp_file=""
  for scope in "$project_name" "global"; do
    for fname in "claude.json" "mcp.json"; do
      local candidate="$AGENTS_HOME/mcp/$scope/$fname"
      [[ -f "$candidate" ]] && mcp_file="$candidate" && break
    done
    [[ -n "$mcp_file" ]] && break
  done
  if [[ -n "$mcp_file" ]]; then
    local server_names=()
    while IFS= read -r srv; do
      server_names+=("$srv")
    done < <(jq -r '.servers | keys[]' "$mcp_file" 2>/dev/null | sort)
    if [[ ${#server_names[@]} -gt 0 ]]; then
      mcp_val=$(printf '%s\n' "${server_names[@]}" | jq -R . | jq -s .)
    fi
  fi

  # Detect settings
  local settings_val=false
  if [[ -f "$AGENTS_HOME/settings/$project_name/cursor.json" ]] || \
     [[ -f "$AGENTS_HOME/settings/global/cursor.json" ]]; then
    settings_val=true
  fi

  if [[ "$DRY_RUN" == true ]]; then
    log_dry "Would write $AGENTSRC_FILE with:"
    log_dry "  project:  $project_name"
    log_dry "  skills:   ${skills[*]:-<none>}"
    log_dry "  rules:    ${rules[*]}"
    log_dry "  agents:   ${agents[*]:-<none>}"
    log_dry "  hooks:    $hooks_val"
    log_dry "  mcp:      $mcp_val"
    log_dry "  settings: $settings_val"
    return 0
  fi

  # Build skills JSON array
  local skills_json="[]"
  if [[ ${#skills[@]} -gt 0 ]]; then
    skills_json=$(printf '%s\n' "${skills[@]}" | jq -R . | jq -s .)
  fi

  # Build rules JSON array
  local rules_json
  rules_json=$(printf '%s\n' "${rules[@]}" | jq -R . | jq -s .)

  # Build agents JSON array
  local agents_json="[]"
  if [[ ${#agents[@]} -gt 0 ]]; then
    agents_json=$(printf '%s\n' "${agents[@]}" | jq -R . | jq -s .)
  fi

  # Write manifest
  jq -n \
    --arg schema "https://dot-agents.dev/schemas/agentsrc.json" \
    --arg project "$project_name" \
    --argjson skills "$skills_json" \
    --argjson rules "$rules_json" \
    --argjson agents "$agents_json" \
    --argjson hooks "$hooks_val" \
    --argjson mcp "$mcp_val" \
    --argjson settings "$settings_val" \
    '{
      "$schema": $schema,
      "version": 1,
      "project": $project,
      "skills": $skills,
      "rules": $rules,
      "agents": $agents,
      "hooks": $hooks,
      "mcp": $mcp,
      "settings": $settings,
      "sources": [{"type": "local"}]
    }' > "$manifest"

  log_success "Generated $AGENTSRC_FILE"
  echo ""
  echo -e "  ${DIM}Skills: ${#skills[@]}, Rules: ${#rules[@]}, Agents: ${#agents[@]}${NC}"
  echo ""
  echo "Next steps:"
  echo "  1. Review: cat $AGENTSRC_FILE"
  echo "  2. Commit: git add $AGENTSRC_FILE && git commit -m 'Add dot-agents manifest'"
  echo "  3. Others: dot-agents install   (after cloning)"
}

# ─── manifest mutation helpers ───────────────────────────────────────────────

# Add a name to a hooks or mcp field in .agentsrc.json.
# If the field is already `true` (all), do nothing.
# If false or missing, start a named list.
# Usage: _agentsrc_add_to_field <field> <name> [manifest]
_agentsrc_add_to_field() {
  local field="$1" name="$2" manifest="${3:-$PWD/$AGENTSRC_FILE}"
  [[ -f "$manifest" ]] || return 0
  _json_has_jq || return 0

  local current
  current=$(jq -r ".$field" "$manifest" 2>/dev/null)

  # If already `true`, everything is included — nothing to do
  [[ "$current" == "true" ]] && return 0

  # If false/null, start with just this name
  # If array, append if not already present
  local updated
  if [[ "$current" == "false" || "$current" == "null" ]]; then
    updated=$(jq --arg field "$field" --arg name "$name" \
      '.[$field] = [$name]' "$manifest")
  else
    # Check if already in array
    if jq -e --arg field "$field" --arg name "$name" \
        '.[$field] | arrays | index($name) != null' "$manifest" >/dev/null 2>&1; then
      return 0
    fi
    updated=$(jq --arg field "$field" --arg name "$name" \
      '.[$field] = (.[$field] + [$name])' "$manifest")
  fi
  echo "$updated" > "$manifest"
}

# Remove a name from a hooks or mcp field in .agentsrc.json.
# If the field is `true`, do nothing (can't selectively remove from "all").
# Usage: _agentsrc_remove_from_field <field> <name> [manifest]
_agentsrc_remove_from_field() {
  local field="$1" name="$2" manifest="${3:-$PWD/$AGENTSRC_FILE}"
  [[ -f "$manifest" ]] || return 0
  _json_has_jq || return 0

  local current
  current=$(jq -r ".$field" "$manifest" 2>/dev/null)
  [[ "$current" == "true" ]] && return 0  # can't selectively remove from "all"

  local updated
  updated=$(jq --arg field "$field" --arg name "$name" \
    '.[$field] = [.[$field][] | select(. != $name)]' "$manifest" 2>/dev/null)
  [[ -n "$updated" ]] && echo "$updated" > "$manifest"
}

# ─── internal helpers ─────────────────────────────────────────────────────────

# Read .agentsrc.json and set globals.
# Globals set: AGENTSRC_PROJECT, AGENTSRC_SKILLS, AGENTSRC_RULES,
#              AGENTSRC_AGENTS, AGENTSRC_HOOKS, AGENTSRC_MCP,
#              AGENTSRC_SETTINGS, AGENTSRC_SOURCES_JSON
_agentsrc_read() {
  local manifest="$1"

  if ! _json_has_jq; then
    log_error "jq is required to read $AGENTSRC_FILE"
    return 1
  fi

  local json
  json=$(cat "$manifest") || return 1

  if ! echo "$json" | jq -e '.' >/dev/null 2>&1; then
    return 1
  fi

  AGENTSRC_PROJECT=$(echo "$json" | jq -r '.project // empty')
  AGENTSRC_SKILLS=$(echo "$json"  | jq -r '(.skills // [])[]')
  AGENTSRC_RULES=$(echo "$json"   | jq -r '(.rules  // [])[]')
  AGENTSRC_AGENTS=$(echo "$json"  | jq -r '(.agents // [])[]')
  AGENTSRC_HOOKS=$(echo "$json"   | jq -r '.hooks    // false')
  AGENTSRC_MCP=$(echo "$json"     | jq -r '.mcp      // false')
  AGENTSRC_SETTINGS=$(echo "$json"| jq -r '.settings // false')
  AGENTSRC_SOURCES_JSON=$(echo "$json" | jq -c '.sources // [{"type":"local"}]')

  export AGENTSRC_PROJECT AGENTSRC_SKILLS AGENTSRC_RULES AGENTSRC_AGENTS
  export AGENTSRC_HOOKS AGENTSRC_MCP AGENTSRC_SETTINGS AGENTSRC_SOURCES_JSON
}

# Resolve all sources to local directories.
# Fills the nameref array (bash 4.3+) or a global fallback.
# Usage: _agentsrc_resolve_sources resolved_arr
_agentsrc_resolve_sources() {
  local -n _resolved_ref="$1"
  _resolved_ref=()

  local source_count
  source_count=$(echo "$AGENTSRC_SOURCES_JSON" | jq 'length')
  local i=0
  local had_error=false

  while [[ "$i" -lt "$source_count" ]]; do
    local src_type src_url src_ref
    src_type=$(echo "$AGENTSRC_SOURCES_JSON" | jq -r ".[$i].type // \"local\"")
    src_url=$(echo "$AGENTSRC_SOURCES_JSON"  | jq -r ".[$i].url  // empty")
    src_ref=$(echo "$AGENTSRC_SOURCES_JSON"  | jq -r ".[$i].ref  // empty")

    case "$src_type" in
      local)
        local local_path
        local_path=$(echo "$AGENTSRC_SOURCES_JSON" | jq -r ".[$i].path // empty")
        if [[ -n "$local_path" ]]; then
          local_path=$(expand_path "$local_path")
        else
          local_path="$AGENTS_HOME"
        fi
        _resolved_ref+=("$local_path")
        bullet "ok" "Local source: ${local_path/#$HOME/~}"
        ;;
      git)
        if [[ -z "$src_url" ]]; then
          log_warn "Git source #$i missing 'url' — skipping"
          had_error=true
          i=$((i+1))
          continue
        fi
        local cache_dir
        if ! cache_dir=$(_agentsrc_fetch_git_source "$src_url" "$src_ref"); then
          log_warn "Failed to fetch git source: $src_url"
          had_error=true
          i=$((i+1))
          continue
        fi
        _resolved_ref+=("$cache_dir")
        bullet "ok" "Git source: $src_url"
        ;;
      *)
        log_warn "Unknown source type '$src_type' — skipping"
        had_error=true
        ;;
    esac
    i=$((i+1))
  done

  [[ "$had_error" == true ]] && return 1 || return 0
}

# Remove a path only when it is under an allowed prefix.
_agentsrc_safe_remove_path() {
  local target="$1"
  local allowed_prefix="$2"
  if [[ -z "$target" ]] || [[ -z "$allowed_prefix" ]]; then
    log_warn "Refusing to remove path with empty target/prefix"
    return 1
  fi
  case "$target" in
    "$allowed_prefix"/*) ;;
    *)
      log_warn "Refusing to remove path outside managed prefix: $target"
      return 1
      ;;
  esac
  rm -rf -- "$target"
}

# Clone or update a git source to the cache directory.
# Echoes the cache directory path on success.
_agentsrc_fetch_git_source() {
  local url="$1"
  local ref="${2:-}"

  if ! command -v git &>/dev/null; then
    log_error "git is not installed — cannot fetch remote sources"
    return 1
  fi

  local cache_dir
  cache_dir=$(_agentsrc_git_cache_dir "$url")

  if [[ -d "$cache_dir/.git" ]]; then
    # Already cloned — update unless recently fetched and not forced
    local last_fetch="$cache_dir/.last-fetch"
    local do_update=true
    if [[ "$FORCE" != true ]] && [[ -f "$last_fetch" ]]; then
      local age=$(( $(date +%s) - $(date -r "$last_fetch" +%s 2>/dev/null || echo 0) ))
      [[ "$age" -lt 3600 ]] && do_update=false
    fi

    if [[ "$do_update" == true ]]; then
      [[ "$VERBOSE" == true ]] && log_info "Updating cached source: $url"
      if [[ "$DRY_RUN" == true ]]; then
        log_dry "git -C $cache_dir pull"
      else
        git -C "$cache_dir" pull -q 2>/dev/null || \
          log_warn "Could not update cached source — using existing copy"
        touch "$last_fetch"
      fi
    else
      [[ "$VERBOSE" == true ]] && log_info "Using cached source (< 1h old): $url"
    fi
  else
    # First clone
    [[ "$VERBOSE" == true ]] && log_info "Cloning source: $url"
    if [[ "$DRY_RUN" == true ]]; then
      local clone_cmd="git clone --depth 1"
      [[ -n "$ref" ]] && clone_cmd="$clone_cmd --branch $ref"
      log_dry "$clone_cmd $url $cache_dir"
      echo "$cache_dir"
      return 0
    fi

    mkdir -p "$cache_dir"
    local clone_args=(--depth 1)
    [[ -n "$ref" ]] && clone_args+=(--branch "$ref")
    if ! git clone "${clone_args[@]}" "$url" "$cache_dir" -q 2>/dev/null; then
      _agentsrc_safe_remove_path "$cache_dir" "$AGENTS_CACHE_DIR/sources" >/dev/null 2>&1 || true
      return 1
    fi
    touch "$cache_dir/.last-fetch"
  fi

  echo "$cache_dir"
  return 0
}

# Return the cache directory for a git URL.
_agentsrc_git_cache_dir() {
  local url="$1"
  local hash
  # md5sum (Linux) or md5 (macOS)
  if command -v md5sum &>/dev/null; then
    hash=$(echo -n "$url" | md5sum | cut -c1-12)
  elif command -v md5 &>/dev/null; then
    hash=$(echo -n "$url" | md5 | cut -c1-12)
  else
    hash=$(echo -n "$url" | cksum | cut -d' ' -f1)
  fi
  echo "$AGENTS_CACHE_DIR/sources/$hash"
  return 0
}

# Link a resource from the first source that provides it into ~/.agents/{type}/{project}/.
# Returns 0 if found+linked, 1 if not found anywhere.
_agentsrc_link_resource_from_sources() {
  local resource_type="$1"   # "skills" or "agents"
  local resource_name="$2"
  local project_name="$3"
  shift 3
  local sources=("$@")

  local marker_file
  case "$resource_type" in
    skills) marker_file="SKILL.md" ;;
    agents) marker_file="AGENT.md" ;;
    *)      marker_file="" ;;
  esac

  local dest_dir="$AGENTS_HOME/$resource_type/$project_name/$resource_name"

  for src_root in "${sources[@]}"; do
    local candidate="$src_root/$resource_type/global/$resource_name"
    if [[ -d "$candidate" && ( -z "$marker_file" || -f "$candidate/$marker_file" ) ]]; then
      if [[ "$DRY_RUN" == true ]]; then
        log_dry "link $resource_type/$resource_name → ${candidate/#$HOME/~}"
        return 0
      fi
      # Symlink source directory into ~/.agents/{type}/{project}/
      if [[ -e "$dest_dir" ]] || [[ -L "$dest_dir" ]]; then
        # Already exists (own copy or link) — don't overwrite unless --force
        if [[ "$FORCE" == true ]]; then
          if ! _agentsrc_safe_remove_path "$dest_dir" "$AGENTS_HOME/$resource_type/$project_name"; then
            return 1
          fi
        else
          return 0
        fi
      fi
      ln -sf "$candidate" "$dest_dir"
      [[ "$VERBOSE" == true ]] && bullet "ok" "Linked $resource_type/$resource_name from ${src_root/#$HOME/~}"
      return 0
    fi
  done
  return 1
}

# Find a project name by its path in config.json.
_agentsrc_find_project_by_path() {
  local target_path="$1"
  if ! _json_has_jq; then
    return 1
  fi
  local config_file="$AGENTS_HOME/config.json"
  [[ -f "$config_file" ]] || return 1
  jq -r --arg p "$target_path" \
    '.projects | to_entries[] | select(.value.path == $p) | .key' \
    "$config_file" 2>/dev/null | head -1
}
