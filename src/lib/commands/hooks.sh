#!/bin/bash
# dot-agents/lib/commands/hooks.sh
# Manage Claude Code hooks configuration

cmd_hooks_help() {
  cat << EOF
${BOLD}dot-agents hooks${NC} - Manage Claude Code hooks

${BOLD}USAGE${NC}
    dot-agents hooks [subcommand] [options]

${BOLD}SUBCOMMANDS${NC}
    list                  List all hooks (default)
    add <type>            Add a hook
    remove <type> <index> Remove a hook by index
    edit                  Open hooks settings in editor
    examples              Show common hook examples

${BOLD}OPTIONS${NC}
    --project, -p <name>  Target specific project (default: current directory)
    --global, -g          Target global hooks only
    --json                Output in JSON format
    --help, -h            Show this help

${BOLD}HOOK TYPES${NC}
    Tool Hooks:
      PreToolUse            Before executing any tool (Bash, Edit, Read, etc.)
      PostToolUse           After tool execution completes
      PostToolUseFailure    After tool execution fails

    Session Hooks:
      SessionStart          When a new session is started
      SessionEnd            When a session is ending
      Stop                  Right before Claude concludes its response

    User Interaction Hooks:
      UserPromptSubmit      When the user submits a prompt
      Notification          When notifications are sent
      PermissionRequest     When a permission dialog is displayed

    Subagent Hooks:
      SubagentStart         When a subagent (Task tool) is started
      SubagentStop          When a subagent concludes its response

    Context Hooks:
      PreCompact            Before conversation compaction

${BOLD}ADD OPTIONS${NC}
    --command, -c <cmd>   Shell command to run (required)
    --matcher, -m <pat>   Tool pattern to match (default: "*")

${BOLD}ENVIRONMENT VARIABLES${NC}
    All hooks receive:
      \$SESSION_ID          Current Claude session ID
      \$TRANSCRIPT_PATH     Path to conversation transcript

    Tool hooks (PreToolUse, PostToolUse, PostToolUseFailure):
      \$TOOL_NAME           Name of the tool being used
      \$TOOL_INPUT          The tool's input/arguments
      \$TOOL_OUTPUT         The tool's output (PostToolUse* only)

    Prompt hooks (UserPromptSubmit):
      \$USER_PROMPT         The prompt text submitted

    Subagent hooks (SubagentStart, SubagentStop):
      \$SUBAGENT_ID         The subagent identifier

    Compact hooks (PreCompact):
      \$SUMMARY_PATH        Path to the compaction summary

${BOLD}EXAMPLES${NC}
    dot-agents hooks                          # List all hooks
    dot-agents hooks --global                 # List global hooks only
    dot-agents hooks --project myapp          # List project hooks

    dot-agents hooks add PreToolUse -m "Bash" -c "echo \\\$TOOL_INPUT >> log.txt"
    dot-agents hooks remove PreToolUse 0      # Remove first PreToolUse hook
    dot-agents hooks edit                     # Edit in \$EDITOR
    dot-agents hooks examples                 # Show example hooks

EOF
}

cmd_hooks() {
  # Parse flags
  local subcommand="list"
  local project_name=""
  local global_only=false
  local hook_type=""
  local hook_command=""
  local hook_matcher="*"
  local hook_index=""

  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    case $1 in
      --project|-p)
        project_name="$2"
        shift 2
        ;;
      --global|-g)
        global_only=true
        shift
        ;;
      --command|-c)
        hook_command="$2"
        shift 2
        ;;
      --matcher|-m)
        hook_matcher="$2"
        shift 2
        ;;
      --json)
        JSON_OUTPUT=true
        shift
        ;;
      --help|-h)
        cmd_hooks_help
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

  # Parse subcommand and arguments
  if [ ${#REMAINING_ARGS[@]} -gt 0 ]; then
    subcommand="${REMAINING_ARGS[0]}"
    if [ ${#REMAINING_ARGS[@]} -gt 1 ]; then
      hook_type="${REMAINING_ARGS[1]}"
    fi
    if [ ${#REMAINING_ARGS[@]} -gt 2 ]; then
      hook_index="${REMAINING_ARGS[2]}"
    fi
  fi

  # Determine project context if not explicitly set
  if [ -z "$project_name" ] && [ "$global_only" = false ]; then
    project_name=$(detect_current_project)
  fi

  case "$subcommand" in
    list)
      hooks_list "$project_name" "$global_only"
      ;;
    add)
      if [ -z "$hook_type" ]; then
        log_error "Hook type required."
        log_info "Valid types: PreToolUse, PostToolUse, PostToolUseFailure, Notification,"
        log_info "             UserPromptSubmit, SessionStart, SessionEnd, Stop,"
        log_info "             SubagentStart, SubagentStop, PreCompact, PermissionRequest"
        return 1
      fi
      if [ -z "$hook_command" ]; then
        log_error "Command required. Use --command or -c"
        return 1
      fi
      hooks_add "$project_name" "$global_only" "$hook_type" "$hook_matcher" "$hook_command"
      ;;
    remove)
      if [ -z "$hook_type" ]; then
        log_error "Hook type required"
        return 1
      fi
      if [ -z "$hook_index" ]; then
        log_error "Hook index required"
        return 1
      fi
      hooks_remove "$project_name" "$global_only" "$hook_type" "$hook_index"
      ;;
    edit)
      hooks_edit "$project_name" "$global_only"
      ;;
    examples)
      hooks_examples
      ;;
    *)
      log_error "Unknown subcommand: $subcommand"
      cmd_hooks_help
      return 1
      ;;
  esac
}

# List hooks for global and/or project
hooks_list() {
  local project_name="$1"
  local global_only="$2"

  local global_settings="$AGENTS_HOME/settings/global/claude-code.json"
  local project_settings=""

  if [ -n "$project_name" ]; then
    project_settings="$AGENTS_HOME/settings/$project_name/claude-code.json"
  fi

  if [ "$JSON_OUTPUT" = true ]; then
    hooks_list_json "$global_settings" "$project_settings" "$project_name"
    return
  fi

  log_header "dot-agents hooks"

  if [ -n "$project_name" ] && [ "$global_only" = false ]; then
    echo -e "Project: ${BOLD}$project_name${NC}"
  fi
  echo ""

  # Global hooks
  log_section "Global Hooks"
  echo -e "  ${DIM}~/.agents/settings/global/claude-code.json${NC}"
  echo ""

  if [ -f "$global_settings" ]; then
    hooks_display_from_file "$global_settings" "  "
  else
    echo -e "  ${DIM}No global settings file found${NC}"
    echo -e "  ${DIM}Run 'dot-agents init' to create one${NC}"
  fi

  # Project hooks
  if [ -n "$project_name" ] && [ "$global_only" = false ]; then
    echo ""
    log_section "Project Hooks ($project_name)"
    echo -e "  ${DIM}~/.agents/settings/$project_name/claude-code.json${NC}"
    echo ""

    if [ -f "$project_settings" ]; then
      hooks_display_from_file "$project_settings" "  "
    else
      echo -e "  ${DIM}No project settings file found${NC}"
      echo -e "  ${DIM}Run 'dot-agents add <path>' to create one${NC}"
    fi
  fi

  echo ""
  echo -e "${DIM}Tip: Use 'dot-agents hooks add <type> -m \"pattern\" -c \"command\"' to add hooks${NC}"
}

# Display hooks from a settings file
hooks_display_from_file() {
  local file="$1"
  local indent="$2"

  # All 12 Claude Code hook types
  local hook_types=(
    "PreToolUse" "PostToolUse" "PostToolUseFailure"
    "Notification" "UserPromptSubmit"
    "SessionStart" "SessionEnd" "Stop"
    "SubagentStart" "SubagentStop"
    "PreCompact" "PermissionRequest"
  )
  local has_hooks=false

  for hook_type in "${hook_types[@]}"; do
    local hooks
    hooks=$(jq -r ".hooks.$hook_type // []" "$file" 2>/dev/null)
    local count
    count=$(echo "$hooks" | jq 'length' 2>/dev/null || echo "0")

    if [ "$count" -gt 0 ]; then
      has_hooks=true
      echo -e "${indent}${BOLD}$hook_type:${NC}"

      local i=0
      while [ $i -lt "$count" ]; do
        local matcher
        local command
        matcher=$(echo "$hooks" | jq -r ".[$i].matcher // \"*\"" 2>/dev/null)
        command=$(echo "$hooks" | jq -r ".[$i].hooks[0].command // \"(no command)\"" 2>/dev/null)

        # Truncate long commands
        if [ ${#command} -gt 50 ]; then
          command="${command:0:47}..."
        fi

        echo -e "${indent}  ${CYAN}[$i]${NC} ${YELLOW}$matcher${NC} ${DIM}→${NC} $command"
        ((i++))
      done
      echo ""
    fi
  done

  if [ "$has_hooks" = false ]; then
    echo -e "${indent}${DIM}No hooks configured${NC}"
  fi
}

# Output hooks as JSON
hooks_list_json() {
  local global_settings="$1"
  local project_settings="$2"
  local project_name="$3"

  echo "{"
  echo "  \"global\": {"

  if [ -f "$global_settings" ]; then
    local global_hooks
    global_hooks=$(jq '.hooks // {}' "$global_settings" 2>/dev/null || echo "{}")
    echo "    \"file\": \"$global_settings\","
    echo "    \"hooks\": $global_hooks"
  else
    echo "    \"file\": null,"
    echo "    \"hooks\": {}"
  fi

  echo "  },"
  echo "  \"project\": {"

  if [ -n "$project_name" ] && [ -f "$project_settings" ]; then
    local project_hooks
    project_hooks=$(jq '.hooks // {}' "$project_settings" 2>/dev/null || echo "{}")
    echo "    \"name\": \"$project_name\","
    echo "    \"file\": \"$project_settings\","
    echo "    \"hooks\": $project_hooks"
  else
    echo "    \"name\": ${project_name:+\"$project_name\"}${project_name:-null},"
    echo "    \"file\": null,"
    echo "    \"hooks\": {}"
  fi

  echo "  }"
  echo "}"
}

# Add a hook
hooks_add() {
  local project_name="$1"
  local global_only="$2"
  local hook_type="$3"
  local matcher="$4"
  local command="$5"

  # Validate hook type - all 12 Claude Code hook types
  case "$hook_type" in
    PreToolUse|PostToolUse|PostToolUseFailure|\
    Notification|UserPromptSubmit|\
    SessionStart|SessionEnd|Stop|\
    SubagentStart|SubagentStop|\
    PreCompact|PermissionRequest)
      ;;
    *)
      log_error "Invalid hook type: $hook_type"
      log_info "Valid types: PreToolUse, PostToolUse, PostToolUseFailure, Notification,"
      log_info "             UserPromptSubmit, SessionStart, SessionEnd, Stop,"
      log_info "             SubagentStart, SubagentStop, PreCompact, PermissionRequest"
      return 1
      ;;
  esac

  # Determine target file
  local target_file
  if [ "$global_only" = true ] || [ -z "$project_name" ]; then
    target_file="$AGENTS_HOME/settings/global/claude-code.json"
    log_info "Adding hook to global settings"
  else
    target_file="$AGENTS_HOME/settings/$project_name/claude-code.json"
    log_info "Adding hook to project: $project_name"
  fi

  # Ensure file exists
  if [ ! -f "$target_file" ]; then
    log_error "Settings file not found: $target_file"
    return 1
  fi

  # Build the new hook entry
  local new_hook
  new_hook=$(jq -n \
    --arg matcher "$matcher" \
    --arg command "$command" \
    '{
      "matcher": $matcher,
      "hooks": [
        {
          "type": "command",
          "command": $command
        }
      ]
    }')

  # Add to existing hooks
  local updated
  updated=$(jq \
    --arg type "$hook_type" \
    --argjson hook "$new_hook" \
    '.hooks[$type] = ((.hooks[$type] // []) + [$hook])' \
    "$target_file")

  # Write back
  echo "$updated" > "$target_file"

  log_success "Added $hook_type hook"
  echo -e "  Matcher: ${YELLOW}$matcher${NC}"
  echo -e "  Command: ${DIM}$command${NC}"

  # Auto-update .agentsrc.json manifest if at project scope
  if [[ "$global_only" != "true" ]] && [[ -n "$project_name" ]]; then
    _HOOKS_INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if ! declare -F _agentsrc_add_to_field >/dev/null 2>&1; then
      # shellcheck source=install.sh
      source "$_HOOKS_INSTALL_DIR/install.sh" 2>/dev/null || true
    fi
    if declare -F _agentsrc_add_to_field >/dev/null 2>&1; then
      local manifest
      manifest=$(config_get_project_path "$project_name" 2>/dev/null)
      manifest="${manifest:-$PWD}/$AGENTSRC_FILE"
      if [[ -f "$manifest" ]]; then
        _agentsrc_add_to_field "hooks" "$hook_type" "$manifest"
        log_info "Updated .agentsrc.json: added hook event '$hook_type'"
      fi
    fi
  fi
}

# Remove a hook
hooks_remove() {
  local project_name="$1"
  local global_only="$2"
  local hook_type="$3"
  local index="$4"

  # Validate hook type - all 12 Claude Code hook types
  case "$hook_type" in
    PreToolUse|PostToolUse|PostToolUseFailure|\
    Notification|UserPromptSubmit|\
    SessionStart|SessionEnd|Stop|\
    SubagentStart|SubagentStop|\
    PreCompact|PermissionRequest)
      ;;
    *)
      log_error "Invalid hook type: $hook_type"
      log_info "Valid types: PreToolUse, PostToolUse, PostToolUseFailure, Notification,"
      log_info "             UserPromptSubmit, SessionStart, SessionEnd, Stop,"
      log_info "             SubagentStart, SubagentStop, PreCompact, PermissionRequest"
      return 1
      ;;
  esac

  # Validate index is a number
  if ! [[ "$index" =~ ^[0-9]+$ ]]; then
    log_error "Index must be a number"
    return 1
  fi

  # Determine target file
  local target_file
  if [ "$global_only" = true ] || [ -z "$project_name" ]; then
    target_file="$AGENTS_HOME/settings/global/claude-code.json"
  else
    target_file="$AGENTS_HOME/settings/$project_name/claude-code.json"
  fi

  # Ensure file exists
  if [ ! -f "$target_file" ]; then
    log_error "Settings file not found: $target_file"
    return 1
  fi

  # Check if index exists
  local count
  count=$(jq -r ".hooks.$hook_type | length" "$target_file" 2>/dev/null || echo "0")

  if [ "$index" -ge "$count" ]; then
    log_error "Invalid index: $index (only $count hooks exist)"
    return 1
  fi

  # Remove the hook at index
  local updated
  updated=$(jq \
    --arg type "$hook_type" \
    --argjson idx "$index" \
    '.hooks[$type] = [.hooks[$type][] | select(. != .hooks[$type][$idx])] | .hooks[$type] |= del(.[$idx])' \
    "$target_file")

  # Simpler approach: rebuild array without the index
  updated=$(jq \
    --arg type "$hook_type" \
    --argjson idx "$index" \
    '.hooks[$type] = ([.hooks[$type] | to_entries[] | select(.key != $idx) | .value])' \
    "$target_file")

  # Write back
  echo "$updated" > "$target_file"

  log_success "Removed $hook_type hook at index $index"

  # Auto-update .agentsrc.json: remove event type if no hooks remain at project scope
  if [[ "$global_only" != "true" ]] && [[ -n "$project_name" ]]; then
    _HOOKS_INSTALL_DIR2="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if ! declare -F _agentsrc_remove_from_field >/dev/null 2>&1; then
      # shellcheck source=install.sh
      source "$_HOOKS_INSTALL_DIR2/install.sh" 2>/dev/null || true
    fi
    if declare -F _agentsrc_remove_from_field >/dev/null 2>&1; then
      local manifest
      manifest=$(config_get_project_path "$project_name" 2>/dev/null)
      manifest="${manifest:-$PWD}/$AGENTSRC_FILE"
      if [[ -f "$manifest" ]]; then
        # Only remove event type from manifest if no hooks remain for it
        local remaining
        remaining=$(jq -r ".hooks.$hook_type | length" "$target_file" 2>/dev/null || echo "0")
        if [[ "$remaining" = "0" ]]; then
          _agentsrc_remove_from_field "hooks" "$hook_type" "$manifest"
          log_info "Updated .agentsrc.json: removed hook event '$hook_type'"
        fi
      fi
    fi
  fi
}

# Open settings file in editor
hooks_edit() {
  local project_name="$1"
  local global_only="$2"

  local editor="${EDITOR:-vi}"

  # Determine target file
  local target_file
  if [ "$global_only" = true ] || [ -z "$project_name" ]; then
    target_file="$AGENTS_HOME/settings/global/claude-code.json"
  else
    target_file="$AGENTS_HOME/settings/$project_name/claude-code.json"
  fi

  if [ ! -f "$target_file" ]; then
    log_error "Settings file not found: $target_file"
    return 1
  fi

  log_info "Opening in $editor: $target_file"
  "$editor" "$target_file"
}

# Show example hooks
hooks_examples() {
  cat << 'EOF'

╔══════════════════════════════════════════════════════════════════════════════╗
║                           Claude Code Hook Examples                          ║
╚══════════════════════════════════════════════════════════════════════════════╝

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 TOOL HOOKS (PreToolUse, PostToolUse, PostToolUseFailure)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

┌──────────────────────────────────────────────────────────────────────────────┐
│ 1. COMMAND LOGGING                                                           │
│    Log all Bash commands to a file                                           │
└──────────────────────────────────────────────────────────────────────────────┘

  dot-agents hooks add PreToolUse \
    --matcher "Bash" \
    --command "echo \"[\$(date '+%Y-%m-%d %H:%M:%S')] \$TOOL_INPUT\" >> ~/.claude/command-log.txt"

┌──────────────────────────────────────────────────────────────────────────────┐
│ 2. AUTO-FORMAT ON EDIT                                                       │
│    Run Prettier on files after Claude edits them                             │
└──────────────────────────────────────────────────────────────────────────────┘

  dot-agents hooks add PostToolUse \
    --matcher "Edit" \
    --command "npx prettier --write \"\$TOOL_INPUT\" 2>/dev/null || true"

┌──────────────────────────────────────────────────────────────────────────────┐
│ 3. ERROR REPORTING                                                           │
│    Send notification when a tool fails                                       │
└──────────────────────────────────────────────────────────────────────────────┘

  dot-agents hooks add PostToolUseFailure \
    --matcher "*" \
    --command "osascript -e 'display notification \"Tool failed: \$TOOL_NAME\" with title \"Claude Error\"'"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 SESSION HOOKS (SessionStart, SessionEnd, Stop)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

┌──────────────────────────────────────────────────────────────────────────────┐
│ 4. SESSION LOGGING                                                           │
│    Log when sessions start and end                                           │
└──────────────────────────────────────────────────────────────────────────────┘

  dot-agents hooks add SessionStart \
    --command "echo \"Session started: \$SESSION_ID at \$(date)\" >> ~/.claude/sessions.log"

  dot-agents hooks add SessionEnd \
    --command "echo \"Session ended: \$SESSION_ID at \$(date)\" >> ~/.claude/sessions.log"

┌──────────────────────────────────────────────────────────────────────────────┐
│ 5. AUTO-COMMIT ON STOP                                                       │
│    Remind to commit changes when Claude finishes                             │
└──────────────────────────────────────────────────────────────────────────────┘

  dot-agents hooks add Stop \
    --command "git status --short 2>/dev/null | head -5"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 USER INTERACTION HOOKS (UserPromptSubmit, Notification, PermissionRequest)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

┌──────────────────────────────────────────────────────────────────────────────┐
│ 6. PROMPT LOGGING                                                            │
│    Log all user prompts for analysis                                         │
└──────────────────────────────────────────────────────────────────────────────┘

  dot-agents hooks add UserPromptSubmit \
    --command "echo \"\$(date): \$USER_PROMPT\" >> ~/.claude/prompts.log"

┌──────────────────────────────────────────────────────────────────────────────┐
│ 7. DESKTOP NOTIFICATIONS                                                     │
│    Get macOS notifications from Claude                                       │
└──────────────────────────────────────────────────────────────────────────────┘

  dot-agents hooks add Notification \
    --command "osascript -e 'display notification \"Claude notification\" with title \"Claude\"'"

┌──────────────────────────────────────────────────────────────────────────────┐
│ 8. PERMISSION AUDIT                                                          │
│    Log when permission dialogs are shown                                     │
└──────────────────────────────────────────────────────────────────────────────┘

  dot-agents hooks add PermissionRequest \
    --command "echo \"\$(date): Permission requested\" >> ~/.claude/permissions.log"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 SUBAGENT HOOKS (SubagentStart, SubagentStop)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

┌──────────────────────────────────────────────────────────────────────────────┐
│ 9. SUBAGENT TRACKING                                                         │
│    Monitor Task tool (subagent) execution                                    │
└──────────────────────────────────────────────────────────────────────────────┘

  dot-agents hooks add SubagentStart \
    --command "echo \"Subagent \$SUBAGENT_ID started at \$(date)\" >> ~/.claude/subagents.log"

  dot-agents hooks add SubagentStop \
    --command "echo \"Subagent \$SUBAGENT_ID stopped at \$(date)\" >> ~/.claude/subagents.log"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 CONTEXT HOOKS (PreCompact)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

┌──────────────────────────────────────────────────────────────────────────────┐
│ 10. BACKUP BEFORE COMPACT                                                    │
│     Save conversation before compaction                                      │
└──────────────────────────────────────────────────────────────────────────────┘

  dot-agents hooks add PreCompact \
    --command "cp \"\$TRANSCRIPT_PATH\" ~/.claude/backups/\$(date +%Y%m%d-%H%M%S).jsonl 2>/dev/null || true"

════════════════════════════════════════════════════════════════════════════════

MATCHERS (for tool hooks):
  "*"                   Match all tools
  "Bash"                Match Bash tool only
  "Edit"                Match Edit tool only
  "Bash(git:*)"         Match git commands
  "Bash(npm:*)"         Match npm commands
  "Bash(rm:-rf:*)"      Match dangerous rm commands

ALL 12 HOOK TYPES:
  Tool:        PreToolUse, PostToolUse, PostToolUseFailure
  Session:     SessionStart, SessionEnd, Stop
  User:        UserPromptSubmit, Notification, PermissionRequest
  Subagent:    SubagentStart, SubagentStop
  Context:     PreCompact

ENVIRONMENT VARIABLES BY HOOK TYPE:
  All hooks:            $SESSION_ID, $TRANSCRIPT_PATH
  Tool hooks:           $TOOL_NAME, $TOOL_INPUT, $TOOL_OUTPUT (PostToolUse* only)
  UserPromptSubmit:     $USER_PROMPT
  Subagent hooks:       $SUBAGENT_ID
  PreCompact:           $SUMMARY_PATH

EOF
}

# Detect current project from working directory
detect_current_project() {
  local cwd
  cwd=$(pwd)

  # Check if we're in a registered project
  if [ -f "$AGENTS_HOME/config.json" ]; then
    local projects
    projects=$(jq -r '.projects | to_entries[] | "\(.key):\(.value.path)"' "$AGENTS_HOME/config.json" 2>/dev/null)

    while IFS=: read -r name path; do
      path=$(expand_path "$path")
      if [[ "$cwd" == "$path"* ]]; then
        echo "$name"
        return
      fi
    done <<< "$projects"
  fi

  # Not in a registered project
  echo ""
}
