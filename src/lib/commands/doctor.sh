#!/bin/bash
# dot-agents/lib/commands/doctor.sh
# Health check for dot-agents installation

cmd_doctor_help() {
  cat << EOF
${BOLD}dot-agents doctor${NC} - Check health of dot-agents installation

${BOLD}USAGE${NC}
    dot-agents doctor [options]

${BOLD}OPTIONS${NC}
    --redundancy      Check for duplicate/redundant rules across projects
    --migrate         Detect and fix deprecated config formats
    --fix             Auto-fix common issues (use with --migrate)
    --json            Output in JSON format
    --verbose, -v     Show detailed diagnostics
    --help, -h        Show this help

${BOLD}DESCRIPTION${NC}
    Runs health checks on your dot-agents installation:
    - Verifies ~/.agents/ structure
    - Checks for required dependencies (jq)
    - Detects installed AI coding agents
    - Validates config.json schema
    - Checks for common issues

    Use --redundancy to check for duplicate rules across projects.
    Use --migrate to detect deprecated formats (.cursorrules, .claude.json).

${BOLD}EXAMPLES${NC}
    dot-agents doctor                 # Run health check
    dot-agents doctor --redundancy    # Check for duplicate rules
    dot-agents doctor --migrate       # Detect deprecated formats
    dot-agents doctor --migrate --fix # Auto-fix deprecated formats
    dot-agents doctor --json          # Output as JSON

EOF
}

cmd_doctor() {
  local redundancy_mode=false
  local migrate_mode=false
  local fix_mode=false

  # Parse flags
  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    case $1 in
      --redundancy)
        redundancy_mode=true
        shift
        ;;
      --migrate)
        migrate_mode=true
        shift
        ;;
      --fix)
        fix_mode=true
        shift
        ;;
      --json)
        JSON_OUTPUT=true
        shift
        ;;
      --verbose|-v)
        VERBOSE=true
        shift
        ;;
      --help|-h)
        cmd_doctor_help
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

  local checks_passed=0
  local checks_warned=0
  local checks_failed=0

  # Route to specific mode
  if [ "$redundancy_mode" = true ]; then
    run_redundancy_check
  elif [ "$migrate_mode" = true ]; then
    run_migrate_check "$fix_mode"
  elif [ "$JSON_OUTPUT" = true ]; then
    run_doctor_json
  else
    run_doctor_text
  fi
}

run_doctor_text() {
  log_header "dot-agents doctor"
  echo ""

  # Installation conflicts check (first, most critical)
  check_install_conflicts

  # Core checks
  log_section "Core Installation"

  # Check ~/.agents/ exists
  check_text "~/.agents/ directory" \
    "[ -d '$AGENTS_HOME' ]" \
    "Run 'dot-agents init' to create"

  # Check config.json
  check_text "config.json exists" \
    "[ -f '$AGENTS_HOME/config.json' ]" \
    "Run 'dot-agents init' to create"

  # Check config.json is valid JSON
  if [ -f "$AGENTS_HOME/config.json" ]; then
    check_text "config.json is valid JSON" \
      "jq -e '.' '$AGENTS_HOME/config.json' >/dev/null 2>&1" \
      "Fix JSON syntax in config.json"
  fi

  # Check XDG state directory
  check_text "State directory exists" \
    "[ -d '$AGENTS_STATE_DIR' ] || mkdir -p '$AGENTS_STATE_DIR'" \
    ""

  echo ""
  log_section "Dependencies"

  # Check jq
  check_text "jq (JSON processor)" \
    "command -v jq >/dev/null" \
    "Install: brew install jq"

  # Check git
  check_text "git" \
    "command -v git >/dev/null" \
    "Required for sync features"

  echo ""
  log_section "Detected Agents"

  # Use generic platform registry for detection
  local platform
  while IFS= read -r platform; do
    detect_agent_platform \
      "$(platform_display_name "$platform")" \
      "platform_is_installed $platform" \
      "platform_version $platform"
  done < <(platform_ids)

  echo ""
  log_section "Global Settings"

  # Check Claude Code global settings status
  local claude_status
  claude_status=$(claude_global_settings_status)
  case "$claude_status" in
    managed)
      echo -e "  ${GREEN}✓${NC} Claude Code: ~/.claude/settings.json ${DIM}(managed by dot-agents)${NC}"
      ((checks_passed++)) || true
      ;;
    unmanaged_file)
      echo -e "  ${YELLOW}○${NC} Claude Code: ~/.claude/settings.json ${DIM}(exists, not managed)${NC}"
      echo -e "      ${DIM}→ To manage: dot-agents link claude-global${NC}"
      ;;
    not_found)
      echo -e "  ${GRAY}○${NC} Claude Code: ~/.claude/settings.json ${DIM}(not found)${NC}"
      ;;
    symlink_other:*)
      local target="${claude_status#symlink_other:}"
      echo -e "  ${YELLOW}○${NC} Claude Code: ~/.claude/settings.json ${DIM}(symlink to $target)${NC}"
      ;;
  esac

  # Check Claude Code global rules (CLAUDE.md)
  local claude_rules="$HOME/.claude/CLAUDE.md"
  if [ -e "$claude_rules" ]; then
    if [ -L "$claude_rules" ]; then
      local target
      target=$(readlink "$claude_rules" 2>/dev/null)
      local display_target="${target/#$HOME/~}"
      if [ -f "$target" ]; then
        echo -e "  ${GREEN}✓${NC} Claude Code: ~/.claude/CLAUDE.md ${DIM}→ $display_target${NC}"
        ((checks_passed++)) || true
      else
        echo -e "  ${RED}✗${NC} Claude Code: ~/.claude/CLAUDE.md ${DIM}→ $display_target (broken)${NC}"
        ((checks_failed++)) || true
      fi
    else
      echo -e "  ${YELLOW}○${NC} Claude Code: ~/.claude/CLAUDE.md ${DIM}(local file)${NC}"
    fi
  else
    echo -e "  ${GRAY}○${NC} Claude Code: ~/.claude/CLAUDE.md ${DIM}(not found)${NC}"
  fi

  echo ""
  log_section "Hooks Configuration"

  # Check global hooks settings
  local global_settings="$AGENTS_HOME/settings/global/claude-code.json"
  if [ -f "$global_settings" ]; then
    # Validate JSON syntax
    if jq -e '.' "$global_settings" >/dev/null 2>&1; then
      local hook_count=0
      # All 12 Claude Code hook types
      local hook_types=(
        "PreToolUse" "PostToolUse" "PostToolUseFailure"
        "Notification" "UserPromptSubmit"
        "SessionStart" "SessionEnd" "Stop"
        "SubagentStart" "SubagentStop"
        "PreCompact" "PermissionRequest"
      )
      for hook_type in "${hook_types[@]}"; do
        local count
        count=$(jq -r ".hooks.$hook_type | length" "$global_settings" 2>/dev/null || echo "0")
        hook_count=$((hook_count + count))
      done

      if [ "$hook_count" -gt 0 ]; then
        echo -e "  ${GREEN}✓${NC} Global hooks: $hook_count hook(s) configured"
        ((checks_passed++)) || true
      else
        echo -e "  ${GRAY}○${NC} Global hooks: none configured"
      fi
    else
      echo -e "  ${RED}✗${NC} Global settings: invalid JSON syntax"
      echo -e "      ${DIM}→ Check ~/.agents/settings/global/claude-code.json${NC}"
      ((checks_failed++)) || true
    fi
  else
    echo -e "  ${YELLOW}○${NC} Global settings: not found"
    echo -e "      ${DIM}→ Run 'dot-agents init' to create${NC}"
  fi

  echo ""
  log_section "Skills"

  # Check global skills (directory-based)
  local agent_skills_base="$AGENTS_HOME/skills"
  local global_skills="$agent_skills_base/global"
  if [ -d "$global_skills" ]; then
    local skill_count=0
    for skill_dir in "$global_skills"/*/; do
      [ -d "$skill_dir" ] || continue
      [ -f "$skill_dir/SKILL.md" ] && ((skill_count++)) || true
    done

    if [ "$skill_count" -gt 0 ]; then
      echo -e "  ${GREEN}✓${NC} User-scope skills: $skill_count found"
      link_count=$(find $agent_skills_base -maxdepth 1 -type l | wc -l)
      if [ "$link_count" -eq "$skill_count" ]; then
        ((checks_passed++)) || true
      else
        echo -e "  ${YELLOW}○${NC} User-scope skills: $link_count linked, $skill_count total"
        echo -e "      ${DIM}→ Run 'dot-agents link' to refresh project links${NC}"
      fi
    else
      echo -e "  ${YELLOW}○${NC} User-scope skills: directory exists but empty"
      echo -e "      ${DIM}→ Run 'dot-agents init --force' to create templates${NC}"
    fi
  else
    echo -e "  ${YELLOW}○${NC} User-scope skills: not found"
    echo -e "      ${DIM}→ Run 'dot-agents init' to create${NC}"
  fi

  # Check project skills symlinks
  if [ -f "$AGENTS_HOME/config.json" ] && has_jq; then
    local projects
    projects=$(jq -r '.projects | keys[]' "$AGENTS_HOME/config.json" 2>/dev/null)

    for project in $projects; do
      local project_path
      project_path=$(jq -r ".projects[\"$project\"].path" "$AGENTS_HOME/config.json")
      project_path=$(expand_path "$project_path")

      [ -d "$project_path" ] || continue

      local skills_dir="$project_path/.claude/skills"
      if [ -d "$skills_dir" ]; then
        local skill_count=0
        for skill in "$skills_dir"/*/; do
          [ -d "$skill" ] || [ -L "$skill" ] && ((skill_count++)) || true
        done
        if [ "$skill_count" -gt 0 ]; then
          echo -e "  ${GREEN}✓${NC} $project: $skill_count skill(s) linked"
        else
          echo -e "  ${YELLOW}○${NC} $project: .claude/skills/ empty"
          echo -e "      ${DIM}→ dot-agents link${NC}"
        fi
      else
        echo -e "  ${GRAY}○${NC} $project: no .claude/skills/"
      fi
    done
  fi

  echo ""
  log_section "User-level platform dirs (global agents/skills)"

  # Cursor: ~/.cursor/skills, ~/.cursor/agents
  local cursor_commands="${CURSOR_USER_SKILLS:-$HOME/.cursor/skills}"
  local cursor_agents="${CURSOR_USER_AGENTS:-$HOME/.cursor/agents}"
  if [ -d "$cursor_commands" ]; then
    local n=0
    for f in "$cursor_commands"/*.md; do [ -e "$f" ] && ((n++)) || true; done
    [ "$n" -gt 0 ] && echo -e "  ${GREEN}✓${NC} Cursor ~/.cursor/skills: $n skill(s)" || echo -e "  ${YELLOW}○${NC} Cursor ~/.cursor/skills: empty"
  else
    echo -e "  ${GRAY}○${NC} Cursor ~/.cursor/skills: not found"
  fi
  if [ -d "$cursor_agents" ]; then
    local n=0
    for f in "$cursor_agents"/*.md; do [ -e "$f" ] && ((n++)) || true; done
    [ "$n" -gt 0 ] && echo -e "  ${GREEN}✓${NC} Cursor ~/.cursor/agents: $n agent(s)" || echo -e "  ${YELLOW}○${NC} Cursor ~/.cursor/agents: empty"
  else
    echo -e "  ${GRAY}○${NC} Cursor ~/.cursor/agents: not found"
  fi

  # Claude: ~/.claude/skills, ~/.claude/agents
  local claude_skills="${CLAUDE_USER_SKILLS:-$HOME/.claude/skills}"
  local claude_agents="${CLAUDE_USER_AGENTS:-$HOME/.claude/agents}"
  if [ -d "$claude_skills" ]; then
    local n=0
    for d in "$claude_skills"/*/; do [ -e "$d" ] && ((n++)) || true; done
    [ "$n" -gt 0 ] && echo -e "  ${GREEN}✓${NC} Claude ~/.claude/skills: $n skill(s)" || echo -e "  ${YELLOW}○${NC} Claude ~/.claude/skills: empty"
  else
    echo -e "  ${GRAY}○${NC} Claude ~/.claude/skills: not found"
  fi
  if [ -d "$claude_agents" ]; then
    local n=0
    for d in "$claude_agents"/*/; do [ -e "$d" ] && ((n++)) || true; done
    [ "$n" -gt 0 ] && echo -e "  ${GREEN}✓${NC} Claude ~/.claude/agents: $n agent(s)" || echo -e "  ${YELLOW}○${NC} Claude ~/.claude/agents: empty"
  else
    echo -e "  ${GRAY}○${NC} Claude ~/.claude/agents: not found"
  fi

  # Codex: ~/.agents/skills, ~/.codex/agents
  local codex_skills="${CODEX_USER_SKILLS:-$agent_skills_base}"
  local codex_agents="${CODEX_USER_AGENTS:-$HOME/.codex/agents}"
  if [ -d "$codex_skills" ]; then
    local n=0
    for d in "$codex_skills"/*/; do [ -e "$d" ] && ((n++)) || true; done
    [ "$n" -gt 0 ] && echo -e "  ${GREEN}✓${NC} Codex ~/.agents/skills: $n skill(s)" || echo -e "  ${YELLOW}○${NC} Codex ~/.agents/skills: empty"
  else
    echo -e "  ${GRAY}○${NC} Codex ~/.agents/skills: not found"
  fi
  if [ -d "$codex_agents" ]; then
    local n=0
    for d in "$codex_agents"/*/; do [ -e "$d" ] && ((n++)) || true; done
    [ "$n" -gt 0 ] && echo -e "  ${GREEN}✓${NC} Codex ~/.codex/agents: $n agent(s)" || echo -e "  ${YELLOW}○${NC} Codex ~/.codex/agents: empty"
  else
    echo -e "  ${GRAY}○${NC} Codex ~/.codex/agents: not found"
  fi

  # OpenCode: ~/.opencode/agent
  local opencode_agent_dir="${OPEN_CODE_USER_AGENT:-$HOME/.opencode/agent}"
  if [ -d "$opencode_agent_dir" ]; then
    local n=0
    local broken=0
    for f in "$opencode_agent_dir"/*; do
      [ -e "$f" ] || continue
      if [ -L "$f" ]; then
        local target
        target=$(readlink "$f" 2>/dev/null)
        if [ -n "$target" ] && [ -f "$target" ]; then
          ((n++)) || true
        else
          ((broken++)) || true
        fi
      else
        ((n++)) || true
      fi
    done
    if [ "$n" -gt 0 ] && [ "$broken" -eq 0 ]; then
      echo -e "  ${GREEN}✓${NC} OpenCode ~/.opencode/agent: $n agent file(s)"
    elif [ "$n" -gt 0 ] || [ "$broken" -gt 0 ]; then
      echo -e "  ${YELLOW}○${NC} OpenCode ~/.opencode/agent: $n ok, $broken broken"
    else
      echo -e "  ${YELLOW}○${NC} OpenCode ~/.opencode/agent: empty"
    fi
  else
    echo -e "  ${GRAY}○${NC} OpenCode ~/.opencode/agent: not found"
  fi

  # GitHub Copilot custom agent files are discovered via configurable chat.agentFilesLocations
  local copilot_agents="${COPILOT_USER_AGENTS:-$HOME/.github/agents}"
  if [ -n "$copilot_agents" ]; then
    if [ -d "$copilot_agents" ]; then
      local n=0
      for f in "$copilot_agents"/*.md; do [ -e "$f" ] && ((n++)) || true; done
      [ "$n" -gt 0 ] && echo -e "  ${GREEN}✓${NC} GitHub Copilot $copilot_agents: $n custom agent file(s)" || echo -e "  ${YELLOW}○${NC} GitHub Copilot $copilot_agents: empty"
    else
      echo -e "  ${GRAY}○${NC} GitHub Copilot ~/.github/agents: not found"
    fi
  else
    echo -e "  ${GRAY}○${NC} GitHub Copilot custom agents dir: not configured"
    echo -e "      ${DIM}→ Configure chat.agentFilesLocations (workspace/user setting)${NC}"
  fi

  echo ""
  log_section "Subagents"

  source "$LIB_DIR/commands/agents.sh"
  local global_agents="$AGENTS_HOME/agents/global"
  local agents_valid=0
  local agents_invalid=0

  # Global agents (summary line like Skills)
  if [ -d "$global_agents" ]; then
    local global_count=0
    local global_invalid=0
    for agent_dir in "$global_agents"/*/; do
      [ -d "$agent_dir" ] || continue
      [ -f "$agent_dir/AGENT.md" ] || continue
      if validate_agent_dir "$agent_dir"; then
        ((global_count++)) || true
        ((agents_valid++)) || true
      else
        ((global_invalid++)) || true
        ((agents_invalid++)) || true
      fi
    done
    if [ "$global_count" -gt 0 ] && [ "$global_invalid" -eq 0 ]; then
      echo -e "  ${GREEN}✓${NC} Global agents: $global_count found"
      ((checks_passed++)) || true
    elif [ "$global_count" -gt 0 ] || [ "$global_invalid" -gt 0 ]; then
      echo -e "  ${YELLOW}○${NC} Global agents: $global_count valid, $global_invalid invalid"
    else
      echo -e "  ${YELLOW}○${NC} Global agents: directory exists but empty"
    fi
  else
    echo -e "  ${YELLOW}○${NC} Global agents: not found"
    echo -e "      ${DIM}→ Run 'dot-agents init' to create${NC}"
  fi

  # Project agents (one line per managed project, like Skills)
  if [ -f "$AGENTS_HOME/config.json" ] && has_jq; then
    local projects
    projects=$(jq -r '.projects | keys[]' "$AGENTS_HOME/config.json" 2>/dev/null)

    for project in $projects; do
      local project_agents="$AGENTS_HOME/agents/$project"
      if [ -d "$project_agents" ]; then
        local proj_count=0
        local proj_invalid=0
        for agent_dir in "$project_agents"/*/; do
          [ -d "$agent_dir" ] || continue
          [ -f "$agent_dir/AGENT.md" ] || continue
          if validate_agent_dir "$agent_dir"; then
            ((proj_count++)) || true
            ((agents_valid++)) || true
          else
            ((proj_invalid++)) || true
            ((agents_invalid++)) || true
          fi
        done
        if [ "$proj_count" -gt 0 ] && [ "$proj_invalid" -eq 0 ]; then
          echo -e "  ${GREEN}✓${NC} $project: $proj_count agent(s) valid"
        elif [ "$proj_count" -gt 0 ] || [ "$proj_invalid" -gt 0 ]; then
          echo -e "  ${YELLOW}○${NC} $project: $proj_count valid, $proj_invalid invalid"
        else
          echo -e "  ${YELLOW}○${NC} $project: no agents"
        fi
      else
        echo -e "  ${GRAY}○${NC} $project: no agents/"
      fi
    done
  fi

  if [ "$agents_invalid" -gt 0 ]; then
    ((checks_failed++)) || true
  fi

  echo ""
  log_section "Directory Structure"

  # Check key directories exist
  local dirs=(
    "rules/global"
    "settings/global"
    "mcp/global"
    "skills/global"
    "agents/global"
    "scripts"
    "local"
  )

  for dir in "${dirs[@]}"; do
    check_text "$dir/" \
      "[ -d '$AGENTS_HOME/$dir' ]" \
      "mkdir -p ~/.agents/$dir"
  done

  # Check manifests in registered projects
  echo ""
  log_section "Manifests (.agentsrc.json)"

  local manifest_config_file="$AGENTS_HOME/config.json"
  if [ -f "$manifest_config_file" ] && has_jq; then
    local manifest_projects
    manifest_projects=$(jq -r '.projects | keys[]' "$manifest_config_file" 2>/dev/null)
    local any_manifest_issue=false
    while IFS= read -r pname; do
      [ -z "$pname" ] && continue
      local ppath
      ppath=$(jq -r --arg p "$pname" '.projects[$p].path // empty' "$manifest_config_file" 2>/dev/null)
      [ -z "$ppath" ] && continue
      local mf="$ppath/.agentsrc.json"
      if [ ! -f "$mf" ]; then
        echo -e "  ${YELLOW}⚠${NC}  $pname — no manifest (not git-portable)"
        echo -e "       hint: dot-agents install --generate"
        any_manifest_issue=true
      elif ! jq -e '.' "$mf" >/dev/null 2>&1; then
        echo -e "  ${RED}✗${NC}  $pname — corrupt manifest: $mf"
        any_manifest_issue=true
      else
        local git_url
        git_url=$(jq -r '.sources[]? | select(.type=="git") | .url' "$mf" 2>/dev/null | head -1)
        if [ -n "$git_url" ]; then
          local cache_hash
          cache_hash=$(echo -n "$git_url" | md5sum 2>/dev/null | cut -c1-12 || \
                       echo -n "$git_url" | md5 2>/dev/null | cut -c1-12 || echo "unknown")
          local cache_dir="${XDG_CACHE_HOME:-$HOME/.cache}/dot-agents/sources/$cache_hash"
          if [ ! -d "$cache_dir" ]; then
            echo -e "  ${YELLOW}⚠${NC}  $pname — git source not yet fetched"
            echo -e "       hint: dot-agents install (in $ppath)"
            any_manifest_issue=true
          else
            echo -e "  ${GREEN}✓${NC}  $pname — manifest ok (git: $git_url)"
          fi
        else
          echo -e "  ${GREEN}✓${NC}  $pname — manifest ok (local)"
        fi
      fi
    done <<< "$manifest_projects"
    if [ "$any_manifest_issue" = false ]; then
      echo -e "  ${DIM}Tip: run with -v to see per-project manifest details${NC}"
    fi
  else
    echo -e "  ${DIM}No projects registered or jq unavailable${NC}"
  fi

  # Check for deprecated formats in registered projects
  echo ""
  log_section "Deprecated Formats"

  local deprecated_count=0
  local config_file="$AGENTS_HOME/config.json"

  if [ -f "$config_file" ] && has_jq; then
    local projects
    projects=$(jq -r '.projects | keys[]' "$config_file" 2>/dev/null)

    for project in $projects; do
      local project_path
      project_path=$(jq -r ".projects[\"$project\"].path" "$config_file")
      project_path=$(expand_path "$project_path")

      [ -d "$project_path" ] || continue

      local platform
      while IFS= read -r platform; do
        if platform_has_deprecated_format "$platform" "$project_path"; then
          echo -e "  ${YELLOW}⚠${NC}  ${BOLD}$project${NC}: $(platform_display_name "$platform") ${DIM}(deprecated config)${NC}"
          echo -e "      ${DIM}→ dot-agents doctor --migrate --fix${NC}"
          ((deprecated_count++))
        fi
      done < <(platform_ids)
    done

    if [ $deprecated_count -eq 0 ]; then
      echo -e "  ${GREEN}✓${NC} No deprecated formats found"
    fi
  else
    echo -e "  ${DIM}○${NC} Skipped (no config or jq)"
  fi

  # Summary
  echo ""
  echo "────────────────────────────────────────────────────"
  local total=$((checks_passed + checks_warned + checks_failed))
  echo -e "Checks: ${GREEN}$checks_passed passed${NC}, ${YELLOW}$checks_warned warnings${NC}, ${RED}$checks_failed failed${NC} (total: $total)"

  if [ $deprecated_count -gt 0 ]; then
    echo -e "Deprecated: ${YELLOW}$deprecated_count${NC} format(s) need migration"
  fi

  if [ $checks_failed -gt 0 ]; then
    return 1
  fi
  return 0
}

run_doctor_json() {
  echo "{"
  echo '  "version": "'$DOT_AGENTS_VERSION'",'

  # Core checks
  echo '  "core": {'
  echo -n '    "agents_home": '
  [ -d "$AGENTS_HOME" ] && echo '"ok",' || echo '"missing",'
  echo -n '    "config_json": '
  [ -f "$AGENTS_HOME/config.json" ] && echo '"ok",' || echo '"missing",'
  echo -n '    "config_valid": '
  jq -e '.' "$AGENTS_HOME/config.json" >/dev/null 2>&1 && echo '"ok"' || echo '"invalid"'
  echo '  },'

  # Dependencies
  echo '  "dependencies": {'
  echo -n '    "jq": '
  command -v jq >/dev/null && echo '"installed",' || echo '"missing",'
  echo -n '    "git": '
  command -v git >/dev/null && echo '"installed"' || echo '"missing"'
  echo '  },'

  # Agents - use generic platform registry
  echo '  "agents": {'

  local platform json_name version entry first=true
  while IFS= read -r platform; do
    case "$platform" in
      claude) json_name="claude-code" ;;
      copilot) json_name="github-copilot" ;;
      *) json_name="$platform" ;;
    esac

    if [ "$first" = true ]; then
      first=false
    else
      echo ','
    fi

    if platform_is_installed "$platform"; then
      version=$(platform_version "$platform" || echo "unknown")
      entry='{"installed": true, "version": "'"$version"'"}'
    else
      entry='{"installed": false}'
    fi

    echo -n '    "'"$json_name"'": '
    echo -n "$entry"
  done < <(platform_ids)

  echo '  }'
  echo "}"
}

# Check for conflicting installations
check_install_conflicts() {
  local curl_bin="$HOME/.local/bin/dot-agents"
  local hb_bin=""

  # Find Homebrew binary
  if [ -x "/opt/homebrew/bin/dot-agents" ]; then
    hb_bin="/opt/homebrew/bin/dot-agents"
  elif [ -x "/usr/local/bin/dot-agents" ]; then
    hb_bin="/usr/local/bin/dot-agents"
  fi

  # Check for conflict
  if [ -x "$curl_bin" ] && [ -n "$hb_bin" ] && [ -x "$hb_bin" ]; then
    log_section "Installation Conflict"
    echo -e "  ${RED}✗${NC} Multiple installations detected!"
    echo ""
    echo "    curl:     $curl_bin"
    echo "    homebrew: $hb_bin"
    echo ""

    local active
    active=$(command -v dot-agents 2>/dev/null)
    echo "    Active:   $active"
    echo ""

    if [ "$active" = "$curl_bin" ]; then
      echo -e "    ${YELLOW}⚠${NC}  You're running the curl version, but Homebrew is also installed."
      echo "       Homebrew upgrades won't affect the version you're actually using."
      echo ""
      echo "    To fix, remove the curl installation:"
      echo "      rm $curl_bin"
      echo "      rm -rf ~/.local/lib/dot-agents"
      echo "      rm -rf ~/.local/share/dot-agents"
    else
      echo -e "    ${YELLOW}⚠${NC}  Old curl installation still exists but is not in use."
      echo ""
      echo "    To clean up:"
      echo "      rm $curl_bin"
      echo "      rm -rf ~/.local/lib/dot-agents"
      echo "      rm -rf ~/.local/share/dot-agents"
    fi

    echo ""
    ((checks_warned++)) || true
  fi
}

# Helper: Run a check and output result for text mode
check_text() {
  local name="$1"
  local test_cmd="$2"
  local fix_hint="$3"

  if eval "$test_cmd" 2>/dev/null; then
    echo -e "  ${GREEN}✓${NC} $name"
    ((checks_passed++)) || true
  else
    if [ -n "$fix_hint" ]; then
      echo -e "  ${RED}✗${NC} $name"
      echo -e "    ${DIM}→ $fix_hint${NC}"
      ((checks_failed++)) || true
    else
      echo -e "  ${YELLOW}!${NC} $name"
      ((checks_warned++)) || true
    fi
  fi
}

# Helper: Detect an agent using platform module functions
detect_agent_platform() {
  local name="$1"
  local is_installed_func="$2"
  local version_func="$3"

  if $is_installed_func 2>/dev/null; then
    local version
    version=$($version_func 2>/dev/null || echo "unknown")
    echo -e "  ${GREEN}✓${NC} $name ${DIM}($version)${NC}"
    ((checks_passed++)) || true
  else
    echo -e "  ${GRAY}○${NC} $name ${DIM}(not found)${NC}"
  fi
}

# =============================================================================
# Redundancy Check (--redundancy flag)
# =============================================================================

run_redundancy_check() {
  log_header "dot-agents redundancy check"
  echo ""

  local config_file="$AGENTS_HOME/config.json"

  if [ ! -f "$config_file" ]; then
    log_error "Not initialized. Run 'dot-agents init' first."
    return 1
  fi

  log_info "Scanning for duplicate rules across projects..."
  echo ""

  # Collect all rule files
  local all_rules=()
  local rule_contents=()

  # Global rules
  if [ -d "$AGENTS_HOME/rules/global" ]; then
    for rule in "$AGENTS_HOME/rules/global"/*.mdc; do
      [ -f "$rule" ] || continue
      all_rules+=("$rule")
    done
  fi

  # Project rules
  if has_jq; then
    local projects
    projects=$(jq -r '.projects | keys[]' "$config_file" 2>/dev/null)

    for project in $projects; do
      local project_rules_dir="$AGENTS_HOME/rules/$project"
      if [ -d "$project_rules_dir" ]; then
        for rule in "$project_rules_dir"/*.mdc; do
          [ -f "$rule" ] || continue
          all_rules+=("$rule")
        done
      fi
    done
  fi

  local total_rules=${#all_rules[@]}
  log_info "Found $total_rules rule files"
  echo ""

  if [ $total_rules -lt 2 ]; then
    log_success "Not enough rules to check for duplicates"
    return 0
  fi

  # Check for exact duplicate paragraphs
  local duplicates_found=0

  log_section "Checking for duplicate paragraphs..."

  # Create temp file for paragraph analysis
  local temp_dir
  temp_dir=$(mktemp -d)

  for rule in "${all_rules[@]}"; do
    local basename
    basename=$(basename "$rule")
    local display_path="${rule/#$AGENTS_HOME/~/.agents}"

    # Extract paragraphs (blocks separated by blank lines)
    local para_num=0
    local current_para=""

    while IFS= read -r line || [ -n "$line" ]; do
      if [ -z "$line" ]; then
        if [ -n "$current_para" ]; then
          # Save paragraph with source info
          local hash
          hash=$(echo "$current_para" | md5 2>/dev/null || echo "$current_para" | md5sum | cut -d' ' -f1)
          echo "$display_path:$para_num" >> "$temp_dir/$hash"
          ((para_num++))
          current_para=""
        fi
      else
        current_para="$current_para$line"$'\n'
      fi
    done < "$rule"

    # Don't forget last paragraph
    if [ -n "$current_para" ]; then
      local hash
      hash=$(echo "$current_para" | md5 2>/dev/null || echo "$current_para" | md5sum | cut -d' ' -f1)
      echo "$display_path:$para_num" >> "$temp_dir/$hash"
    fi
  done

  # Find duplicates (files with more than one line)
  for hash_file in "$temp_dir"/*; do
    [ -f "$hash_file" ] || continue
    local count
    count=$(wc -l < "$hash_file" | tr -d ' ')
    if [ "$count" -gt 1 ]; then
      ((duplicates_found++))
      echo -e "  ${YELLOW}⚠${NC}  Duplicate paragraph found in:"
      while IFS= read -r location; do
        echo -e "      ${DIM}$location${NC}"
      done < "$hash_file"
      echo ""
    fi
  done

  rm -rf "$temp_dir"

  # Summary
  echo "────────────────────────────────────────────────────"
  if [ $duplicates_found -eq 0 ]; then
    echo -e "${GREEN}✓${NC} No duplicate paragraphs found"
  else
    echo -e "${YELLOW}⚠${NC} Found $duplicates_found duplicate paragraph(s)"
    echo ""
    echo "Consider consolidating duplicate content into global rules."
  fi
}

# =============================================================================
# Migrate Check (--migrate flag)
# =============================================================================

run_migrate_check() {
  local fix_mode="$1"

  log_header "dot-agents migrate check"
  echo ""

  local config_file="$AGENTS_HOME/config.json"

  if [ ! -f "$config_file" ]; then
    log_error "Not initialized. Run 'dot-agents init' first."
    return 1
  fi

  local deprecated_count=0
  local fixed_count=0

  log_section "Scanning for deprecated formats..."
  echo ""

  if has_jq; then
    local projects
    projects=$(jq -r '.projects | keys[]' "$config_file" 2>/dev/null)

    for project in $projects; do
      local project_path
      project_path=$(jq -r ".projects[\"$project\"].path" "$config_file")
      project_path=$(expand_path "$project_path")

      [ -d "$project_path" ] || continue

      # Check for .cursorrules
      if [ -f "$project_path/.cursorrules" ]; then
        ((deprecated_count++))
        echo -e "  ${YELLOW}⚠${NC}  ${BOLD}$project${NC}: .cursorrules"
        echo -e "      ${DIM}Location: $project_path/.cursorrules${NC}"

        if [ "$fix_mode" = true ]; then
          echo -e "      ${CYAN}→${NC} Migrating to .cursor/rules/..."

          # Create .cursor/rules if needed
          mkdir -p "$project_path/.cursor/rules"

          # Convert to .mdc format with frontmatter
          local new_file="$project_path/.cursor/rules/legacy-rules.mdc"
          {
            echo "---"
            echo "alwaysApply: true"
            echo "---"
            echo ""
            cat "$project_path/.cursorrules"
          } > "$new_file"

          # Backup and remove old file
          mv "$project_path/.cursorrules" "$project_path/.cursorrules.backup"
          echo -e "      ${GREEN}✓${NC} Created: .cursor/rules/legacy-rules.mdc"
          echo -e "      ${DIM}Backup: .cursorrules.backup${NC}"
          ((fixed_count++))
        fi
        echo ""
      fi

      # Check for .claude.json
      if [ -f "$project_path/.claude.json" ]; then
        ((deprecated_count++))
        echo -e "  ${YELLOW}⚠${NC}  ${BOLD}$project${NC}: .claude.json"
        echo -e "      ${DIM}Location: $project_path/.claude.json${NC}"

        if [ "$fix_mode" = true ]; then
          echo -e "      ${CYAN}→${NC} Migrating to .claude/settings.json..."

          # Create .claude if needed
          mkdir -p "$project_path/.claude"

          # Move/merge settings
          local new_file="$project_path/.claude/settings.json"
          if [ -f "$new_file" ]; then
            echo -e "      ${YELLOW}!${NC} .claude/settings.json already exists, skipping"
          else
            mv "$project_path/.claude.json" "$new_file"
            echo -e "      ${GREEN}✓${NC} Moved to: .claude/settings.json"
            ((fixed_count++))
          fi
        fi
        echo ""
      fi
    done
  fi

  # Summary
  echo "────────────────────────────────────────────────────"
  if [ $deprecated_count -eq 0 ]; then
    echo -e "${GREEN}✓${NC} No deprecated formats found"
  else
    if [ "$fix_mode" = true ]; then
      echo -e "Fixed $fixed_count of $deprecated_count deprecated format(s)"
    else
      echo -e "${YELLOW}⚠${NC} Found $deprecated_count deprecated format(s)"
      echo ""
      echo "Run 'dot-agents doctor --migrate --fix' to auto-fix"
    fi
  fi
}
