#!/bin/bash
# dot-agents/lib/commands/status.sh
# Show status of managed projects

cmd_status_help() {
  cat << EOF
${BOLD}dot-agents status${NC} - Show status of managed projects

${BOLD}USAGE${NC}
    dot-agents status [options]
    dot-agents status [project]

${BOLD}ARGUMENTS${NC}
    [project]         Show status for specific project (optional)

${BOLD}OPTIONS${NC}
    --audit           Show detailed config/link info for each agent
    --agent <name>    Filter by agent (cursor, claude-code, codex, github-copilot)
    --json            Output in JSON format
    --verbose, -v     Show detailed information
    --help, -h        Show this help

${BOLD}DESCRIPTION${NC}
    Shows all projects registered with dot-agents and their current status.

    Status indicators:
    - [OK]    Project directory exists and configs are linked
    - [WARN]  Project has issues (missing links, outdated config)
    - [ERR]   Project directory not found

    Use --audit for detailed view of which configs are applied where.

${BOLD}EXAMPLES${NC}
    dot-agents status                    # Quick overview
    dot-agents status --audit            # Detailed config info
    dot-agents status --audit myproject  # Audit specific project
    dot-agents status --json             # Output as JSON

EOF
}

cmd_status() {
  local project_filter=""
  local agent_filter=""
  local audit_mode=false

  # Parse flags
  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    case $1 in
      --audit)
        audit_mode=true
        shift
        ;;
      --agent)
        agent_filter="$2"
        shift 2
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
        cmd_status_help
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

  # Get project filter from remaining args
  if [ ${#REMAINING_ARGS[@]} -gt 0 ]; then
    project_filter="${REMAINING_ARGS[0]}"
  fi

  local config_file="$AGENTS_HOME/config.json"

  # Check if config exists
  if [ ! -f "$config_file" ]; then
    if [ "$JSON_OUTPUT" = true ]; then
      echo '{"error": "Not initialized. Run dot-agents init first."}'
    else
      log_error "Not initialized. Run 'dot-agents init' first."
    fi
    return 1
  fi

  # Get projects list
  local projects
  if [ -n "$project_filter" ]; then
    # Check if project exists
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

  # Route to appropriate output
  if [ "$audit_mode" = true ]; then
    if [ "$JSON_OUTPUT" = true ]; then
      output_audit_json "$projects" "$agent_filter"
    else
      output_audit_text "$projects" "$agent_filter"
    fi
  else
    if [ "$JSON_OUTPUT" = true ]; then
      output_status_json "$projects"
    else
      output_status_text "$projects"
    fi
  fi
}

output_status_json() {
  local projects="$1"
  local config_file="$AGENTS_HOME/config.json"

  echo "{"
  echo '  "version": "'$DOT_AGENTS_VERSION'",'
  echo '  "agents_home": "'$AGENTS_HOME'",'
  echo '  "projects": {'

  local first=true
  while IFS= read -r name; do
    [ -z "$name" ] && continue

    local path
    path=$(config_get_project_path "$name")
    local status="ok"
    local issues=""

    # Check project status
    if [ ! -d "$path" ]; then
      status="error"
      issues="directory not found"
    else
      # Check for symlink issues
      local link_issues
      link_issues=$(check_project_links "$name" "$path")
      if [ -n "$link_issues" ]; then
        status="warn"
        issues="$link_issues"
      fi
    fi

    if [ "$first" = true ]; then
      first=false
    else
      echo ","
    fi

    echo -n '    "'$name'": {"path": "'$path'", "status": "'$status'"'
    if [ -n "$issues" ]; then
      echo -n ', "issues": "'$issues'"'
    fi
    echo -n "}"
  done <<< "$projects"

  echo ""
  echo "  }"
  echo "}"
}

output_status_text() {
  local projects="$1"

  log_header "dot-agents status"
  log_info "Home: $AGENTS_HOME"
  echo ""

  # Count projects
  local count=0
  while IFS= read -r name; do
    [ -n "$name" ] && ((count++)) || true
  done <<< "$projects"

  if [ $count -eq 0 ]; then
    log_info "No projects registered."
    echo ""
    echo "Add a project with: dot-agents add <path>"
    return 0
  fi

  echo "Projects ($count):"
  echo ""

  # Display each project
  while IFS= read -r name; do
    [ -z "$name" ] && continue

    local path
    path=$(config_get_project_path "$name")
    local display_path="${path/#$HOME/~}"

    # Check project status
    if [ ! -d "$path" ]; then
      echo -e "  ${RED}[ERR]${NC}  $name"
      echo -e "         ${DIM}$display_path${NC} (not found)"
    else
      # Check for link issues
      local issues_output
      issues_output=$(check_project_links_verbose "$name" "$path")

      if [ -z "$issues_output" ]; then
        echo -e "  ${GREEN}[OK]${NC}   $name"
        echo -e "         ${DIM}$display_path${NC}"
      else
        echo -e "  ${YELLOW}[WARN]${NC} $name"
        echo -e "         ${DIM}$display_path${NC}"
        while IFS= read -r issue; do
          echo -e "         ${YELLOW}↳${NC} $issue"
        done <<< "$issues_output"
      fi
    fi
    echo ""
  done <<< "$projects"
}

# Check project links and return comma-separated issues
# Usage: issues=$(check_project_links PROJECT_NAME PROJECT_PATH)
check_project_links() {
  local name="$1"
  local path="$2"
  local issues=""

  # Check .cursor/rules exists
  if [ ! -d "$path/.cursor/rules" ]; then
    issues="missing .cursor/rules/"
  fi

  # Check for .claude/rules/ if claude rules exist
  if [ -f "$AGENTS_HOME/rules/global/claude-code.mdc" ] || [ -f "$AGENTS_HOME/rules/$name/claude-code.mdc" ]; then
    if [ ! -d "$path/.claude/rules" ]; then
      [ -n "$issues" ] && issues="$issues,"
      issues="${issues}missing .claude/rules/"
    fi
  fi

  # Check for AGENTS.md (Codex)
  if [ -f "$AGENTS_HOME/rules/global/agents.md" ] || [ -f "$AGENTS_HOME/rules/$name/agents.md" ]; then
    if [ ! -e "$path/AGENTS.md" ]; then
      [ -n "$issues" ] && issues="$issues,"
      issues="${issues}missing AGENTS.md"
    fi
  fi

  # Check for .github/copilot-instructions.md
  if [ -f "$AGENTS_HOME/rules/global/copilot-instructions.md" ] || [ -f "$AGENTS_HOME/rules/$name/copilot-instructions.md" ]; then
    if [ ! -e "$path/.github/copilot-instructions.md" ]; then
      [ -n "$issues" ] && issues="$issues,"
      issues="${issues}missing .github/copilot-instructions.md"
    fi
  fi

  # Check for Copilot skills/agents in repo (project-level)
  if [ -d "$AGENTS_HOME/skills/$name" ] && find "$AGENTS_HOME/skills/$name" -mindepth 2 -maxdepth 2 -name SKILL.md -type f 2>/dev/null | grep -q .; then
    if [ ! -d "$path/.github/skills" ]; then
      [ -n "$issues" ] && issues="$issues,"
      issues="${issues}missing .github/skills/"
    fi
  fi
  if [ -d "$AGENTS_HOME/agents/$name" ] && find "$AGENTS_HOME/agents/$name" -mindepth 2 -maxdepth 2 -name AGENT.md -type f 2>/dev/null | grep -q .; then
    if [ ! -d "$path/.github/agents" ]; then
      [ -n "$issues" ] && issues="$issues,"
      issues="${issues}missing .github/agents/"
    fi
  fi

  echo "$issues"
}

# More detailed check for text output - outputs one issue per line
# Usage: check_project_links_verbose PROJECT_NAME PROJECT_PATH
check_project_links_verbose() {
  local name="$1"
  local path="$2"

  # Check .cursor/rules exists and has files
  if [ ! -d "$path/.cursor/rules" ]; then
    echo "missing .cursor/rules/"
  elif [ -z "$(ls -A "$path/.cursor/rules" 2>/dev/null)" ]; then
    echo ".cursor/rules/ is empty"
  fi

  # Check for .claude/rules/
  if [ -f "$AGENTS_HOME/rules/global/claude-code.mdc" ] || [ -f "$AGENTS_HOME/rules/$name/claude-code.mdc" ]; then
    if [ ! -d "$path/.claude/rules" ]; then
      echo "missing .claude/rules/"
    elif [ -z "$(ls -A "$path/.claude/rules" 2>/dev/null)" ]; then
      echo ".claude/rules/ is empty"
    fi
  fi

  # Check for AGENTS.md (Codex)
  if [ -f "$AGENTS_HOME/rules/global/agents.md" ] || [ -f "$AGENTS_HOME/rules/$name/agents.md" ]; then
    if [ ! -e "$path/AGENTS.md" ]; then
      echo "missing AGENTS.md"
    fi
  fi

  # Check for .github/copilot-instructions.md
  if [ -f "$AGENTS_HOME/rules/global/copilot-instructions.md" ] || [ -f "$AGENTS_HOME/rules/$name/copilot-instructions.md" ]; then
    if [ ! -e "$path/.github/copilot-instructions.md" ]; then
      echo "missing .github/copilot-instructions.md"
    fi
  fi

  # Check for Copilot skills/agents in repo (project-level)
  if [ -d "$AGENTS_HOME/skills/$name" ] && find "$AGENTS_HOME/skills/$name" -mindepth 2 -maxdepth 2 -name SKILL.md -type f 2>/dev/null | grep -q .; then
    if [ ! -d "$path/.github/skills" ]; then
      echo "missing .github/skills/"
    fi
  fi
  if [ -d "$AGENTS_HOME/agents/$name" ] && find "$AGENTS_HOME/agents/$name" -mindepth 2 -maxdepth 2 -name AGENT.md -type f 2>/dev/null | grep -q .; then
    if [ ! -d "$path/.github/agents" ]; then
      echo "missing .github/agents/"
    fi
  fi
}

# =============================================================================
# Audit Mode Functions (--audit flag)
# =============================================================================

output_audit_text() {
  local projects="$1"
  local agent_filter="$2"

  log_header "dot-agents audit"
  log_info "Source: $AGENTS_HOME"
  echo ""

  # Check if any projects
  if [ -z "$projects" ]; then
    log_info "No projects registered."
    echo "Add a project with: dot-agents add <path>"
    return 0
  fi

  # Audit each project
  while IFS= read -r name; do
    [ -z "$name" ] && continue

    local path
    path=$(config_get_project_path "$name")
    local display_path="${path/#$HOME/~}"

    echo -e "${BOLD}$name${NC}"
    echo -e "${DIM}$display_path${NC}"
    echo ""

    if [ ! -d "$path" ]; then
      echo -e "  ${RED}Directory not found${NC}"
      echo ""
      continue
    fi

    # Audit Cursor configs
    if [ -z "$agent_filter" ] || [ "$agent_filter" = "cursor" ]; then
      audit_cursor_text "$name" "$path"
    fi

    # Audit Claude Code configs
    if [ -z "$agent_filter" ] || [ "$agent_filter" = "claude-code" ]; then
      audit_claude_text "$name" "$path"
    fi

    # Audit Codex configs
    if [ -z "$agent_filter" ] || [ "$agent_filter" = "codex" ]; then
      audit_codex_text "$name" "$path"
    fi

    # Audit GitHub Copilot configs
    if [ -z "$agent_filter" ] || [ "$agent_filter" = "github-copilot" ] || [ "$agent_filter" = "copilot" ]; then
      audit_copilot_text "$name" "$path"
    fi

  done <<< "$projects"
}

audit_cursor_text() {
  local name="$1"
  local path="$2"

  echo -e "  ${CYAN}Cursor${NC}"

  # Check .cursor/rules/
  local rules_dir="$path/.cursor/rules"
  if [ -d "$rules_dir" ]; then
    local rule_count=0
    shopt -s nullglob
    for rule in "$rules_dir"/*.mdc; do
      local basename
      basename=$(basename "$rule")
      local source_type=""
      local linked_to=""

      # Check if it's a hard link to ~/.agents/
      if [ -f "$rule" ]; then
        # Try to find the source
        if [[ "$basename" == global--* ]]; then
          source_type="global"
          local source_name="${basename#global--}"
          linked_to="~/.agents/rules/global/$source_name"
        elif [[ "$basename" == "${name}--"* ]]; then
          source_type="project"
          local source_name="${basename#${name}--}"
          linked_to="~/.agents/rules/$name/$source_name"
        else
          source_type="local"
        fi

        if [ "$source_type" = "local" ]; then
          echo -e "    ${DIM}○${NC} $basename ${DIM}(local file)${NC}"
        else
          # Check if source exists and matches
          local source_path="${linked_to/#\~/$HOME}"
          if [ -f "$source_path" ] && are_hardlinked "$rule" "$source_path"; then
            echo -e "    ${GREEN}✓${NC} $basename ${DIM}← $linked_to${NC}"
          elif [ -f "$source_path" ]; then
            echo -e "    ${YELLOW}!${NC} $basename ${DIM}(not linked to $linked_to)${NC}"
          else
            echo -e "    ${YELLOW}!${NC} $basename ${DIM}(source missing: $linked_to)${NC}"
          fi
        fi
        ((rule_count++)) || true
      fi
    done
    shopt -u nullglob

    if [ $rule_count -eq 0 ]; then
      echo -e "    ${DIM}(no rules)${NC}"
    fi
  else
    echo -e "    ${DIM}(no .cursor/rules/)${NC}"
  fi
  echo ""
}

audit_claude_text() {
  local name="$1"
  local path="$2"

  echo -e "  ${CYAN}Claude Code${NC}"

  # Check .claude/rules/ directory
  local rules_dir="$path/.claude/rules"
  if [ -d "$rules_dir" ]; then
    local rule_count=0
    local broken_count=0
    shopt -s nullglob
    for link in "$rules_dir"/*.md; do
      if [ -L "$link" ]; then
        local target
        target=$(readlink "$link")
        if [ -f "$target" ]; then
          ((rule_count++))
        else
          ((broken_count++))
        fi
      fi
    done
    shopt -u nullglob

    if [ $rule_count -gt 0 ]; then
      echo -e "    ${GREEN}✓${NC} .claude/rules/ ${DIM}($rule_count rule symlinks)${NC}"
    fi
    if [ $broken_count -gt 0 ]; then
      echo -e "    ${RED}✗${NC} .claude/rules/ ${DIM}($broken_count broken symlinks)${NC}"
    fi
    if [ $rule_count -eq 0 ] && [ $broken_count -eq 0 ]; then
      echo -e "    ${DIM}○${NC} .claude/rules/ ${DIM}(empty)${NC}"
    fi
  else
    echo -e "    ${DIM}(no .claude/rules/)${NC}"
  fi
  echo ""
}

audit_codex_text() {
  local name="$1"
  local path="$2"

  echo -e "  ${CYAN}Codex${NC}"

  # Check AGENTS.md
  local agents_md="$path/AGENTS.md"
  if [ -e "$agents_md" ]; then
    if [ -L "$agents_md" ]; then
      local target
      target=$(readlink "$agents_md")
      local display_target="${target/#$HOME/~}"
      if [ -f "$target" ]; then
        echo -e "    ${GREEN}✓${NC} AGENTS.md ${DIM}→ $display_target${NC}"
      else
        echo -e "    ${RED}✗${NC} AGENTS.md ${DIM}→ $display_target (broken)${NC}"
      fi
    else
      echo -e "    ${DIM}○${NC} AGENTS.md ${DIM}(local file)${NC}"
    fi
  else
    echo -e "    ${DIM}(no AGENTS.md)${NC}"
  fi

  # Check .codex/ directory
  local codex_dir="$path/.codex"
  if [ -d "$codex_dir" ]; then
    echo -e "    ${DIM}○${NC} .codex/ ${DIM}(local directory)${NC}"
  fi
  echo ""
}

audit_copilot_text() {
  local name="$1"
  local path="$2"

  echo -e "  ${CYAN}GitHub Copilot${NC}"

  local copilot_instructions="$path/.github/copilot-instructions.md"
  if [ -e "$copilot_instructions" ]; then
    if [ -L "$copilot_instructions" ]; then
      local target
      target=$(readlink "$copilot_instructions")
      local display_target="${target/#$HOME/~}"
      if [ -f "$target" ]; then
        echo -e "    ${GREEN}✓${NC} .github/copilot-instructions.md ${DIM}→ $display_target${NC}"
      else
        echo -e "    ${RED}✗${NC} .github/copilot-instructions.md ${DIM}→ $display_target (broken)${NC}"
      fi
    else
      echo -e "    ${DIM}○${NC} .github/copilot-instructions.md ${DIM}(local file)${NC}"
    fi
  else
    echo -e "    ${DIM}(no .github/copilot-instructions.md)${NC}"
  fi

  local skills_dir="$path/.github/skills"
  if [ -d "$skills_dir" ]; then
    local n=0
    for d in "$skills_dir"/*/; do [ -e "$d" ] && ((n++)) || true; done
    [ "$n" -gt 0 ] && echo -e "    ${GREEN}✓${NC} .github/skills/ ${DIM}($n skill dir link(s))${NC}" || echo -e "    ${DIM}○${NC} .github/skills/ ${DIM}(empty)${NC}"
  else
    echo -e "    ${DIM}(no .github/skills/)${NC}"
  fi

  local agents_dir="$path/.github/agents"
  if [ -d "$agents_dir" ]; then
    local n=0
    for f in "$agents_dir"/*.agent.md; do [ -e "$f" ] && ((n++)) || true; done
    [ "$n" -gt 0 ] && echo -e "    ${GREEN}✓${NC} .github/agents/ ${DIM}($n agent file link(s))${NC}" || echo -e "    ${DIM}○${NC} .github/agents/ ${DIM}(empty)${NC}"
  else
    echo -e "    ${DIM}(no .github/agents/)${NC}"
  fi
  echo ""
}

output_audit_json() {
  local projects="$1"
  local agent_filter="$2"

  echo "{"
  echo '  "agents_home": "'$AGENTS_HOME'",'
  echo '  "projects": {'

  local first=true
  while IFS= read -r name; do
    [ -z "$name" ] && continue

    local path
    path=$(config_get_project_path "$name")

    if [ "$first" = true ]; then
      first=false
    else
      echo ","
    fi

    echo -n '    "'$name'": {'
    echo -n '"path": "'$path'"'

    if [ -d "$path" ]; then
      # Cursor
      if [ -z "$agent_filter" ] || [ "$agent_filter" = "cursor" ]; then
        echo -n ', "cursor": {'
        echo -n '"rules": ['
        local rules_first=true
        if [ -d "$path/.cursor/rules" ]; then
          shopt -s nullglob
          for rule in "$path/.cursor/rules"/*.mdc; do
            [ "$rules_first" = true ] && rules_first=false || echo -n ","
            echo -n '"'$(basename "$rule")'"'
          done
          shopt -u nullglob
        fi
        echo -n ']}'
      fi

      # Claude
      if [ -z "$agent_filter" ] || [ "$agent_filter" = "claude-code" ]; then
        echo -n ', "claude-code": {'
        echo -n '".claude/rules": ['
        local rules_first=true
        if [ -d "$path/.claude/rules" ]; then
          shopt -s nullglob
          for rule in "$path/.claude/rules"/*.md; do
            [ "$rules_first" = true ] && rules_first=false || echo -n ","
            echo -n '"'$(basename "$rule")'"'
          done
          shopt -u nullglob
        fi
        echo -n ']}'
      fi

      # Codex
      if [ -z "$agent_filter" ] || [ "$agent_filter" = "codex" ]; then
        echo -n ', "codex": {'
        echo -n '"AGENTS.md": '
        if [ -e "$path/AGENTS.md" ]; then
          echo -n 'true'
        else
          echo -n 'false'
        fi
        echo -n '}'
      fi

      # GitHub Copilot
      if [ -z "$agent_filter" ] || [ "$agent_filter" = "github-copilot" ] || [ "$agent_filter" = "copilot" ]; then
        echo -n ', "github-copilot": {'
        echo -n '".github/copilot-instructions.md": '
        if [ -e "$path/.github/copilot-instructions.md" ]; then
          echo -n 'true'
        else
          echo -n 'false'
        fi
        echo -n ', ".github/skills": '
        if [ -d "$path/.github/skills" ]; then
          echo -n 'true'
        else
          echo -n 'false'
        fi
        echo -n ', ".github/agents": '
        if [ -d "$path/.github/agents" ]; then
          echo -n 'true'
        else
          echo -n 'false'
        fi
        echo -n '}'
      fi
    fi

    echo -n '}'
  done <<< "$projects"

  echo ""
  echo "  }"
  echo "}"
}
