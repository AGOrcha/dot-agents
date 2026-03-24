#!/bin/bash
# dot-agents/lib/utils/platform-registry.sh
# Generic platform registry and dispatch helpers

# Ordered platform ids currently supported
PLATFORM_IDS=(cursor claude codex opencode copilot)

platform_ids() {
  printf '%s\n' "${PLATFORM_IDS[@]}"
}

platform_display_name() {
  local platform="$1"
  case "$platform" in
    cursor) echo "Cursor" ;;
    claude) echo "Claude Code" ;;
    codex) echo "Codex CLI" ;;
    opencode) echo "OpenCode" ;;
    copilot) echo "GitHub Copilot" ;;
    *) echo "$platform" ;;
  esac
}

platform_dry_run_message() {
  local platform="$1"
  case "$platform" in
    cursor) echo "Create Cursor config (.cursor/ rules, .claude/agents)" ;;
    claude) echo "Create Claude Code config (.claude/ rules, skills, agents; .agents/skills)" ;;
    codex) echo "Create Codex config (AGENTS.md, .agents/skills, .claude/agents, .codex/config.toml)" ;;
    opencode) echo "Create OpenCode config (.opencode/, .agents/skills)" ;;
    copilot) echo "Create GitHub Copilot config (.github/copilot-instructions.md, .agents/skills, .github/agents, .vscode/mcp.json, .claude/settings.local.json)" ;;
    *) echo "Create $platform config" ;;
  esac
}

platform_success_message() {
  local platform="$1"
  case "$platform" in
    cursor) echo ".cursor/ configs (hard links) + .claude/agents/ symlinks" ;;
    claude) echo "Claude Code links (.claude/ + .agents/skills/)" ;;
    codex) echo "Codex links (.agents/skills/, .claude/agents/)" ;;
    opencode) echo "OpenCode links (symlinks)" ;;
    copilot) echo "GitHub Copilot links (symlinks)" ;;
    *) echo "$platform links" ;;
  esac
}

platform_legacy_fn() {
  local platform="$1"
  local operation="$2"

  case "$operation" in
    is_installed)
      case "$platform" in
        cursor) echo "cursor_is_installed" ;;
        claude) echo "claude_is_installed" ;;
        codex) echo "codex_is_installed" ;;
        opencode) echo "opencode_is_installed" ;;
        copilot) echo "copilot_is_installed" ;;
      esac
      ;;
    version)
      case "$platform" in
        cursor) echo "cursor_version" ;;
        claude) echo "claude_version" ;;
        codex) echo "codex_version" ;;
        opencode) echo "opencode_version" ;;
        copilot) echo "copilot_version" ;;
      esac
      ;;
    create_links)
      case "$platform" in
        cursor) echo "cursor_create_all_links" ;;
        claude) echo "platform_claude_create_links" ;;
        codex) echo "platform_codex_create_links" ;;
        opencode) echo "opencode_create_links" ;;
        copilot) echo "copilot_create_links" ;;
      esac
      ;;
    has_deprecated_format)
      case "$platform" in
        cursor) echo "cursor_has_deprecated_format" ;;
        claude) echo "claude_has_deprecated_format" ;;
        codex) echo "codex_has_deprecated_format" ;;
        opencode) echo "opencode_has_deprecated_format" ;;
        copilot) echo "copilot_has_deprecated_format" ;;
      esac
      ;;
    deprecated_details)
      case "$platform" in
        cursor) echo "cursor_deprecated_details" ;;
        claude) echo "claude_deprecated_details" ;;
        codex) echo "codex_deprecated_details" ;;
        opencode) echo "opencode_deprecated_details" ;;
        copilot) echo "copilot_deprecated_details" ;;
      esac
      ;;
  esac
}

platform_is_installed() {
  local platform="$1"
  local fn
  fn=$(platform_legacy_fn "$platform" "is_installed")
  [ -n "$fn" ] || return 1
  "$fn" 2>/dev/null
}

platform_version() {
  local platform="$1"
  local fn
  fn=$(platform_legacy_fn "$platform" "version")
  [ -n "$fn" ] || return 1
  "$fn" 2>/dev/null
}

platform_create_links() {
  local platform="$1"
  local project="$2"
  local repo="$3"
  local fn
  fn=$(platform_legacy_fn "$platform" "create_links")
  [ -n "$fn" ] || return 1
  "$fn" "$project" "$repo"
}

platform_has_deprecated_format() {
  local platform="$1"
  local repo="$2"
  local fn
  fn=$(platform_legacy_fn "$platform" "has_deprecated_format")
  [ -n "$fn" ] || return 1
  "$fn" "$repo"
}

platform_deprecated_details() {
  local platform="$1"
  local repo="$2"
  local fn
  fn=$(platform_legacy_fn "$platform" "deprecated_details")
  [ -n "$fn" ] || return 1
  "$fn" "$repo"
}

# Adapter shims to satisfy canonical create_links semantics
platform_claude_create_links() {
  local project="$1"
  local repo="$2"
  claude_create_links "$project" "$repo"
  claude_create_skills_links "$project" "$repo"
}

platform_codex_create_links() {
  local project="$1"
  local repo="$2"
  codex_create_links "$project" "$repo"
}

platform_validate_contract() {
  local platform="$1"
  local required=(is_installed version create_links has_deprecated_format deprecated_details)
  local operation fn

  for operation in "${required[@]}"; do
    fn=$(platform_legacy_fn "$platform" "$operation")
    if [ -z "$fn" ] || ! declare -F "$fn" >/dev/null 2>&1; then
      return 1
    fi
  done

  return 0
}

platform_validate_registry() {
  local failed=false
  local platform

  for platform in "${PLATFORM_IDS[@]}"; do
    if ! platform_validate_contract "$platform"; then
      failed=true
      log_warn "Platform contract incomplete for '$platform'"
    fi
  done

  [ "$failed" = false ]
}
