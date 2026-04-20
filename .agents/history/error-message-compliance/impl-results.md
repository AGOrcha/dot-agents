## 1. inventory-error-message-behavior

Recorded the current CLI error-shape inventory in `docs/research/error-message-inventory.md`.

Key findings:

- Shared rendering in `commands/ux.go` is real and already strong for root parse errors, arg-shape helpers, workflow repo-context errors, and the `mcp` / `settings` command families.
- Compliance is uneven because many command families still return raw `fmt.Errorf(...)`, especially under `commands/kg/` and `commands/agents/`.
- The highest-value normalization targets are finite-domain validation, recoverable KG/setup failures, and hand-authored `usage:` strings that bypass `UsageError(...)`.
- `enrichCLIError(...)` is useful today, but several recoverable paths still depend on brittle substring matching instead of command-owned typed errors.

---

## 2. define-error-message-contract

Updated `docs/ERROR_MESSAGE_CONTRACT.md` from an initial seed into the current supported contract.

Key decisions:

- helper precedence is now explicit: `UsageError(...)` for invocation shape, `ErrorWithHints(...)` for recoverable runtime/setup, raw wrapped errors only for true execution failures
- finite-domain validation must enumerate valid values and is considered drift if it only says `unknown` or `invalid`
- manual `usage: ...` strings are out of contract; command families should use shared arg helpers instead
- substring-based `enrichCLIError(...)` remains transitional, but repeated recovery classes should move to command-owned typed errors
- success-path `--json` support does not imply machine-readable failure output
