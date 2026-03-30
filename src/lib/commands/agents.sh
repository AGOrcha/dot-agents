#!/bin/bash
# dot-agents/lib/commands/agents.sh
# Manage subagents (directory-based agent definitions that sync like skills)

cmd_agents_help() {
  cat << EOF
${BOLD}dot-agents agents${NC} - Manage subagents

${BOLD}USAGE${NC}
    dot-agents agents [subcommand] [options]

${BOLD}SUBCOMMANDS${NC}
    (none)            List all subagents (global + project)
    new <name>        Create a new subagent from template
    edit <name>       Open agent's AGENT.md in \$EDITOR
    show <name>       Display agent contents
    validate <name>   Validate agent frontmatter

${BOLD}OPTIONS${NC}
    --global, -g      Filter to global agents only
    --project <name>  Filter to specific project agents
    --json            Output in JSON format
    --help, -h        Show this help

${BOLD}DESCRIPTION${NC}
    Subagents are directory-based agent definitions that live under ~/.agents/agents/.
    They sync with your config when you use 'dot-agents sync' (same as skills).

    Each subagent is a directory containing:
    - AGENT.md        Required - agent definition with frontmatter
    - scripts/        Optional - helper scripts
    - references/     Optional - additional context documents

    Layout (mirrors skills):
    - Global agents: ~/.agents/agents/global/{agent-name}/AGENT.md
    - Project agents: ~/.agents/agents/{project}/{agent-name}/AGENT.md

${BOLD}EXAMPLES${NC}
    dot-agents agents                     # List all subagents
    dot-agents agents --global            # List global agents only
    dot-agents agents --project myapp     # List project agents
    dot-agents agents new reviewer        # Create new subagent
    dot-agents agents edit reviewer       # Edit in \$EDITOR
    dot-agents agents show reviewer       # Display contents

EOF
}

cmd_agents() {
  local filter_global=false
  local filter_project=""
  local subcommand=""
  local agent_name=""

  parse_common_flags "$@"
  set -- "${REMAINING_ARGS[@]+"${REMAINING_ARGS[@]}"}"

  if [ "${SHOW_HELP:-false}" = true ]; then
    cmd_agents_help
    return 0
  fi

  while [[ $# -gt 0 ]]; do
    case $1 in
      --global|-g)
        filter_global=true
        shift
        ;;
      --project)
        filter_project="$2"
        shift 2
        ;;
      new|edit|show|validate)
        subcommand="$1"
        shift
        if [[ $# -gt 0 && ! "$1" =~ ^- ]]; then
          agent_name="$1"
          shift
        fi
        ;;
      -*)
        log_error "Unknown option: $1"
        return 1
        ;;
      *)
        if [ -z "$subcommand" ]; then
          subcommand="$1"
        elif [ -z "$agent_name" ]; then
          agent_name="$1"
        fi
        shift
        ;;
    esac
  done

  case "$subcommand" in
    new)
      agents_new "$agent_name"
      ;;
    edit)
      agents_edit "$agent_name"
      ;;
    show)
      agents_show "$agent_name"
      ;;
    validate)
      agents_validate "$agent_name"
      ;;
    "")
      agents_list "$filter_global" "$filter_project"
      ;;
    *)
      log_error "Unknown subcommand: $subcommand"
      echo "Run 'dot-agents agents --help' for usage"
      return 1
      ;;
  esac
}

agents_list() {
  local filter_global="$1"
  local filter_project="$2"
  local global_dir="$AGENTS_HOME/agents/global"
  local found_any=false

  if [ "$JSON_OUTPUT" = true ]; then
    agents_list_json "$filter_global" "$filter_project"
    return
  fi

  log_header "dot-agents agents"

  if [ "$filter_global" = true ] || [ -z "$filter_project" ]; then
    if [ -d "$global_dir" ]; then
      local has_agents=false
      for agent_dir in "$global_dir"/*/; do
        [ -d "$agent_dir" ] || continue
        [ -f "$agent_dir/AGENT.md" ] || continue
        has_agents=true
        break
      done

      if [ "$has_agents" = true ]; then
        echo ""
        log_section "Global Agents (~/.agents/agents/global/)"
        for agent_dir in "$global_dir"/*/; do
          [ -d "$agent_dir" ] || continue
          [ -f "$agent_dir/AGENT.md" ] || continue
          local name
          name=$(basename "$agent_dir")
          local desc
          desc=$(get_agent_description "$agent_dir/AGENT.md")
          printf "  ${GREEN}%s${NC}  %s\n" "$name" "${DIM}$desc${NC}"
          found_any=true
        done
      fi
    fi
  fi

  if [ "$filter_global" != true ]; then
    if [ -n "$filter_project" ]; then
      local project_dir="$AGENTS_HOME/agents/$filter_project"
      if [ -d "$project_dir" ]; then
        local has_agents=false
        for agent_dir in "$project_dir"/*/; do
          [ -d "$agent_dir" ] || continue
          [ -f "$agent_dir/AGENT.md" ] || continue
          has_agents=true
          break
        done

        if [ "$has_agents" = true ]; then
          echo ""
          log_section "Project Agents: $filter_project"
          for agent_dir in "$project_dir"/*/; do
            [ -d "$agent_dir" ] || continue
            [ -f "$agent_dir/AGENT.md" ] || continue
            local name
            name=$(basename "$agent_dir")
            local desc
            desc=$(get_agent_description "$agent_dir/AGENT.md")
            printf "  ${GREEN}%s${NC}  %s\n" "$name" "${DIM}$desc${NC}"
            found_any=true
          done
        fi
      else
        echo ""
        echo -e "${DIM}No agents found for project: $filter_project${NC}"
      fi
    else
      for project_dir in "$AGENTS_HOME/agents"/*/; do
        [ -d "$project_dir" ] || continue
        local project
        project=$(basename "$project_dir")
        [ "$project" = "global" ] && continue
        [ "$project" = "_template" ] && continue

        local has_agents=false
        for agent_dir in "$project_dir"/*/; do
          [ -d "$agent_dir" ] || continue
          [ -f "$agent_dir/AGENT.md" ] || continue
          has_agents=true
          break
        done

        if [ "$has_agents" = true ]; then
          echo ""
          log_section "Project Agents: $project"
          for agent_dir in "$project_dir"/*/; do
            [ -d "$agent_dir" ] || continue
            [ -f "$agent_dir/AGENT.md" ] || continue
            local name
            name=$(basename "$agent_dir")
            local desc
            desc=$(get_agent_description "$agent_dir/AGENT.md")
            printf "  ${GREEN}%s${NC}  %s\n" "$name" "${DIM}$desc${NC}"
            found_any=true
          done
        fi
      done
    fi
  fi

  if [ "$found_any" = false ]; then
    echo ""
    echo -e "${DIM}No subagents found.${NC}"
    echo ""
    echo "Create one with: dot-agents agents new <name>"
  else
    echo ""
    echo -e "${DIM}Tip: Use 'dot-agents agents new <name>' to create a new subagent${NC}"
  fi
}

agents_list_json() {
  local filter_global="$1"
  local filter_project="$2"
  local global_dir="$AGENTS_HOME/agents/global"

  echo "{"
  echo '  "agents": {'

  local first=true

  if [ "$filter_global" = true ] || [ -z "$filter_project" ]; then
    if [ -d "$global_dir" ]; then
      echo '    "global": ['
      local first_agent=true
      for agent_dir in "$global_dir"/*/; do
        [ -d "$agent_dir" ] || continue
        [ -f "$agent_dir/AGENT.md" ] || continue
        local name
        name=$(basename "$agent_dir")
        local desc
        desc=$(get_agent_description "$agent_dir/AGENT.md")
        [ "$first_agent" = true ] || echo ","
        printf '      {"name": "%s", "path": "%s", "description": "%s"}' "$name" "$agent_dir" "$desc"
        first_agent=false
      done
      echo ""
      echo "    ]"
      first=false
    fi
  fi

  if [ "$filter_global" != true ]; then
    for project_dir in "$AGENTS_HOME/agents"/*/; do
      [ -d "$project_dir" ] || continue
      local project
      project=$(basename "$project_dir")
      [ "$project" = "global" ] && continue
      [ "$project" = "_template" ] && continue
      [ -n "$filter_project" ] && [ "$project" != "$filter_project" ] && continue

      local has_agents=false
      for agent_dir in "$project_dir"/*/; do
        [ -d "$agent_dir" ] || continue
        [ -f "$agent_dir/AGENT.md" ] || continue
        has_agents=true
        break
      done
      [ "$has_agents" = true ] || continue

      [ "$first" = true ] || echo ","
      echo "    \"$project\": ["
      local first_agent=true
      for agent_dir in "$project_dir"/*/; do
        [ -d "$agent_dir" ] || continue
        [ -f "$agent_dir/AGENT.md" ] || continue
        local name
        name=$(basename "$agent_dir")
        local desc
        desc=$(get_agent_description "$agent_dir/AGENT.md")
        [ "$first_agent" = true ] || echo ","
        printf '      {"name": "%s", "path": "%s", "description": "%s"}' "$name" "$agent_dir" "$desc"
        first_agent=false
      done
      echo ""
      echo "    ]"
      first=false
    done
  fi

  echo "  }"
  echo "}"
}

get_agent_description() {
  local file="$1"
  if head -1 "$file" 2>/dev/null | grep -q '^---$'; then
    local desc
    desc=$(awk '/^---$/{if(++c==2)exit} c==1 && /^description:/{gsub(/^description:[[:space:]]*"?|"?$/,""); print; exit}' "$file" 2>/dev/null)
    if [ -n "$desc" ]; then
      echo "$desc" | head -c 60
      return
    fi
  fi
  awk '
    /^---$/ { in_fm = !in_fm; next }
    in_fm { next }
    /^#/ { next }
    /^$/ { next }
    { print; exit }
  ' "$file" 2>/dev/null | head -c 60
}

agents_new() {
  local name="$1"
  local scope="global"

  if [ -z "$name" ]; then
    log_error "Agent name required"
    echo "Usage: dot-agents agents new <name>"
    return 1
  fi

  name=$(echo "$name" | tr -cd 'a-zA-Z0-9_-')

  local target_dir="$AGENTS_HOME/agents/$scope/$name"
  local target_file="$target_dir/AGENT.md"

  if [ -d "$target_dir" ]; then
    log_error "Agent already exists: $target_dir"
    echo "Use 'dot-agents agents edit $name' to modify it"
    return 1
  fi

  mkdir -p "$target_dir"

  local title_name
  title_name="$(echo "${name:0:1}" | tr '[:lower:]' '[:upper:]')${name:1}"
  title_name="${title_name//-/ }"

  cat > "$target_file" << EOF
---
name: "$title_name"
description: "Brief description of the Agents goals"
model: "{default-model}"
is_background: true|false
---

# $title_name

Description of this subagent.

## Role

- Primary responsibility
- When to invoke

## Instructions

1. Key instruction
2. Key instruction

## Notes

- Important consideration
EOF

  log_success "Created subagent: $target_dir"
  echo ""
  echo "Edit with: dot-agents agents edit $name"
  echo "Or open directly: \$EDITOR $target_file"

  # Auto-update .agentsrc.json in the registered project repo, not CWD.
  if [[ "$scope" != "global" ]] && command -v jq >/dev/null 2>&1; then
    local proj_path
    proj_path=$(config_get_project_path "$scope" 2>/dev/null)
    if [[ -n "$proj_path" ]] && [[ -f "$proj_path/$AGENTSRC_FILE" ]]; then
      local manifest="$proj_path/$AGENTSRC_FILE"
      local already
      already=$(jq -r --arg n "$name" '(.agents // []) | index($n)' "$manifest" 2>/dev/null)
      if [[ "$already" = "null" ]]; then
        local updated
        updated=$(jq --arg n "$name" '.agents = ((.agents // []) + [$n])' "$manifest")
        echo "$updated" > "$manifest"
        log_info "Updated .agentsrc.json: added agent '$name'"
      fi
    fi
  fi
}

agents_edit() {
  local name="$1"

  if [ -z "$name" ]; then
    log_error "Agent name required"
    echo "Usage: dot-agents agents edit <name>"
    return 1
  fi

  local agent_dir
  agent_dir=$(find_agent_dir "$name")

  if [ -z "$agent_dir" ]; then
    log_error "Agent not found: $name"
    echo "Available agents:"
    agents_list false ""
    return 1
  fi

  local editor="${EDITOR:-vim}"
  "$editor" "$agent_dir/AGENT.md"
}

agents_show() {
  local name="$1"

  if [ -z "$name" ]; then
    log_error "Agent name required"
    echo "Usage: dot-agents agents show <name>"
    return 1
  fi

  local agent_dir
  agent_dir=$(find_agent_dir "$name")

  if [ -z "$agent_dir" ]; then
    log_error "Agent not found: $name"
    return 1
  fi

  log_header "$name"
  echo -e "${DIM}$agent_dir/AGENT.md${NC}"
  echo ""
  cat "$agent_dir/AGENT.md"

  if [ -d "$agent_dir/scripts" ]; then
    echo ""
    echo -e "${DIM}Scripts:${NC}"
    ls -1 "$agent_dir/scripts/" 2>/dev/null | sed 's/^/  /'
  fi
  if [ -d "$agent_dir/references" ]; then
    echo ""
    echo -e "${DIM}References:${NC}"
    ls -1 "$agent_dir/references/" 2>/dev/null | sed 's/^/  /'
  fi
}

agents_validate() {
  local name="$1"

  if [ -z "$name" ]; then
    log_error "Agent name required"
    echo "Usage: dot-agents agents validate <name>"
    return 1
  fi

  local agent_dir
  agent_dir=$(find_agent_dir "$name")

  if [ -z "$agent_dir" ]; then
    log_error "Agent not found: $name"
    return 1
  fi

  local agent_file="$agent_dir/AGENT.md"
  local valid=true

  log_header "Validating: $name"

  if [ ! -f "$agent_file" ]; then
    echo -e "  ${RED}✗${NC} AGENT.md not found"
    return 1
  fi
  echo -e "  ${GREEN}✓${NC} AGENT.md exists"

  if ! head -1 "$agent_file" | grep -q '^---$'; then
    echo -e "  ${RED}✗${NC} Missing YAML frontmatter"
    valid=false
  else
    echo -e "  ${GREEN}✓${NC} Has YAML frontmatter"
    if grep -q '^name:' "$agent_file"; then
      echo -e "  ${GREEN}✓${NC} Has 'name' field"
    else
      echo -e "  ${RED}✗${NC} Missing 'name' field"
      valid=false
    fi
    if grep -q '^description:' "$agent_file"; then
      echo -e "  ${GREEN}✓${NC} Has 'description' field"
    else
      echo -e "  ${RED}✗${NC} Missing 'description' field"
      valid=false
    fi
  fi

  echo ""
  if [ "$valid" = true ]; then
    log_success "Agent is valid"
    return 0
  else
    log_error "Agent has validation errors"
    return 1
  fi
}

# Validate agent dir (AGENT.md exists, frontmatter, name, description). Return 0 if valid.
validate_agent_dir() {
  local agent_dir="$1"
  local agent_file="$agent_dir/AGENT.md"
  [ -f "$agent_file" ] || return 1
  head -1 "$agent_file" | grep -q '^---$' || return 1
  grep -q '^name:' "$agent_file" || return 1
  grep -q '^description:' "$agent_file" || return 1
  return 0
}

find_agent_dir() {
  local name="$1"

  local global_dir="$AGENTS_HOME/agents/global/$name"
  if [ -d "$global_dir" ] && [ -f "$global_dir/AGENT.md" ]; then
    echo "$global_dir"
    return
  fi

  for project_dir in "$AGENTS_HOME/agents"/*/; do
    [ -d "$project_dir" ] || continue
    local project
    project=$(basename "$project_dir")
    [ "$project" = "global" ] && continue
    [ "$project" = "_template" ] && continue

    local agent_dir="$project_dir/$name"
    if [ -d "$agent_dir" ] && [ -f "$agent_dir/AGENT.md" ]; then
      echo "$agent_dir"
      return
    fi
  done
}
