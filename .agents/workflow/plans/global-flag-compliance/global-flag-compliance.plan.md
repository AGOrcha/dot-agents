# Global Flag Compliance Plan

Status: Active

## Problem

The root command exposes persistent global flags:

- `--json`
- `--dry-run`
- `--yes`
- `--force`
- `--verbose`

In practice, support is uneven across the command tree. Some commands implement a flag fully, some partially, and some inherit the flag in `--help` output without honoring it. The CI review on 2026-04-12 exposed one concrete example: `dot-agents status --json` returned ANSI text instead of JSON while advertising global JSON support.

That inconsistency creates two separate problems:

- operator confusion, because `--help` implies uniform support that does not exist
- brittle automation, because scripts cannot rely on root-level flags behaving consistently

## Immediate Fixes Landed

- `status --json` now emits structured JSON instead of human-formatted output
- `.github/workflows/test.yml` was updated to stop asserting stale `USAGE` text and to stop checking the obsolete `sync status` error string

## Follow-Up Scope

1. Inventory all commands and nested subcommands against each global flag.
2. Decide which flags are intended to be universally supported versus only meaningful on specific commands.
3. For unsupported combinations, prefer explicit rejection or documented non-support over silent no-op behavior.
4. Add regression tests for both supported and unsupported cases.

## Initial Risk Areas

- top-level read-only commands that inherit `--json` without a JSON renderer
- nested command trees where root persistent flags leak into help text but command implementations bypass shared UX paths
- mutation commands that honor `--yes` or `--dry-run` inconsistently between direct and delegated helper paths

## Exit Criteria

- every command has a deliberate contract for each global flag
- automation-facing commands with JSON support have regression tests
- unsupported global flags fail clearly or are no longer advertised ambiguously
