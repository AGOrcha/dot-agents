#!/bin/bash
# dot-agents/lib/utils/interactive.sh
# Interactive setup flow utilities
# Provides step-by-step progress, confirmations, and guided user experience

# Global state for step tracking
CURRENT_STEP=0
TOTAL_STEPS=0

# Initialize step counter for a multi-step operation
init_steps() {
  TOTAL_STEPS="$1"
  CURRENT_STEP=0
}

# Display a numbered step header
step() {
  local message="$1"
  CURRENT_STEP=$((CURRENT_STEP + 1))
  echo ""
  echo -e "${BOLD}[$CURRENT_STEP/$TOTAL_STEPS]${NC} $message"
}

# Confirmation with context (respects --force and --yes)
# Returns 0 for yes, 1 for no
confirm_action() {
  local message="$1"
  local default="${2:-n}"  # Default to "no" for safety

  # Skip prompt if force/yes mode or non-interactive
  if [ "$FORCE" = true ] || [ "$YES" = true ]; then
    return 0
  fi

  # Skip prompt if not a terminal (piped input)
  if [ ! -t 0 ]; then
    [ "$default" = "y" ] && return 0 || return 1
  fi

  local prompt
  if [ "$default" = "y" ]; then
    prompt="[Y/n]"
  else
    prompt="[y/N]"
  fi

  echo ""
  echo -e -n "${YELLOW}$message $prompt: ${NC}"
  read -r response

  case "$response" in
    [yY][eE][sS]|[yY]) return 0 ;;
    [nN][oO]|[nN]) return 1 ;;
    "")
      [ "$default" = "y" ] && return 0 || return 1
      ;;
    *) return 1 ;;
  esac
}

# Show what will be created/modified
preview_changes() {
  local title="$1"
  shift
  local items=("$@")

  echo ""
  echo -e "${BOLD}$title${NC}"
  for item in "${items[@]}"; do
    echo -e "  ${DIM}→${NC} $item"
  done
}

# Show a preview section with a location context
preview_section() {
  local location="$1"
  shift
  local items=("$@")

  echo ""
  echo -e "  ${DIM}In $location:${NC}"
  for item in "${items[@]}"; do
    echo -e "  ${DIM}→${NC} $item"
  done
}

# Info box for notes (cyan border)
info_box() {
  local title="${1:-Note}"
  shift
  local lines=("$@")

  echo ""
  echo -e "${CYAN}┌─ $title ────────────────────────────────────────────┐${NC}"
  for line in "${lines[@]}"; do
    printf "${CYAN}│${NC} %-51s ${CYAN}│${NC}\n" "$line"
  done
  echo -e "${CYAN}└─────────────────────────────────────────────────────┘${NC}"
}

# Warning box for important notices (yellow border)
warn_box() {
  local title="${1:-Warning}"
  shift
  local lines=("$@")

  echo ""
  echo -e "${YELLOW}┌─ $title ────────────────────────────────────────────┐${NC}"
  for line in "${lines[@]}"; do
    printf "${YELLOW}│${NC} %-51s ${YELLOW}│${NC}\n" "$line"
  done
  echo -e "${YELLOW}└─────────────────────────────────────────────────────┘${NC}"
}

# Important/critical box (red border)
important_box() {
  local title="${1:-Important}"
  shift
  local lines=("$@")

  echo ""
  echo -e "${RED}┌─ $title ────────────────────────────────────────────┐${NC}"
  for line in "${lines[@]}"; do
    printf "${RED}│${NC} %-51s ${RED}│${NC}\n" "$line"
  done
  echo -e "${RED}└─────────────────────────────────────────────────────┘${NC}"
}

# Success message with numbered next steps
success_with_next_steps() {
  local message="$1"
  shift
  local steps=("$@")

  echo ""
  log_success "$message"

  if [ ${#steps[@]} -gt 0 ]; then
    echo ""
    echo -e "${BOLD}Next steps:${NC}"
    local i=1
    for s in "${steps[@]}"; do
      echo -e "  ${CYAN}$i.${NC} $s"
      ((i++))
    done
  fi
}

# Show "Test this now with:" suggestion
show_test_commands() {
  local commands=("$@")

  echo ""
  echo -e "${DIM}Test this now with:${NC}"
  for cmd in "${commands[@]}"; do
    echo -e "  ${GREEN}\$${NC} $cmd"
  done
}

# Show file info (size, modification date)
show_file_info() {
  local filepath="$1"

  if [ -f "$filepath" ]; then
    local size modified
    if [ "$(get_os)" = "macos" ]; then
      size=$(stat -f%z "$filepath" 2>/dev/null)
      modified=$(stat -f%Sm -t "%Y-%m-%d" "$filepath" 2>/dev/null)
    else
      size=$(stat --printf="%s" "$filepath" 2>/dev/null)
      modified=$(stat --printf="%y" "$filepath" 2>/dev/null | cut -d' ' -f1)
    fi

    # Convert bytes to human readable
    local human_size
    if [ "$size" -lt 1024 ]; then
      human_size="${size} B"
    elif [ "$size" -lt 1048576 ]; then
      human_size="$(( size / 1024 )) KB"
    else
      human_size="$(( size / 1048576 )) MB"
    fi

    echo "$human_size, last modified $modified"
  else
    echo "file not found"
  fi
}

# Check if running interactively
is_interactive() {
  # Force interactive with flag
  if [ "$INTERACTIVE" = true ]; then
    return 0
  fi

  # Check if stdin is a terminal
  [ -t 0 ]
}

# Intro message for a command
show_intro() {
  local message="$1"
  echo ""
  echo -e "$message"
}

# Bullet point (for use in listings)
bullet() {
  local status="$1"
  local message="$2"

  case "$status" in
    ok|success|found)
      echo -e "  ${GREEN}✓${NC} $message"
      ;;
    warn|warning)
      echo -e "  ${YELLOW}⚠${NC} $message"
      ;;
    error|fail)
      echo -e "  ${RED}✗${NC} $message"
      ;;
    skip|none|notfound)
      echo -e "  ${GRAY}○${NC} $message"
      ;;
    *)
      echo -e "  ${DIM}→${NC} $message"
      ;;
  esac
}
