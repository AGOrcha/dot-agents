#!/usr/bin/env python3
"""Generate the code-review-graph v2.3.8 release-contract fixtures.

The fixtures under ``testdata/crg-release/v2.3.8`` are the ORACLE for the
kg-native code-graph backend's product-facing contract: the MCP tool surface
(names, descriptions, JSON schemas, defaults, required fields), the argument
validation and error messages, the tools/call response envelope and payload
shapes, the SQLite schema at schema version 9, the derived-view rows
(flows / flow_memberships / communities / community_summaries /
flow_snapshots / risk_index), the build/update/postprocess lifecycle result
dicts, and the CLI option surface of every bridge command.

They are GENERATED, never hand-written, so "total parity with v2.3.8" is a
checkable property rather than a claim.

Usage:

    python3 -m pip install 'code-review-graph==2.3.8'
    python3 tools/crgrelease/generate_release_contract.py \
        --out testdata/crg-release/v2.3.8

The generator is deterministic: the fixture repository is committed with
fixed author/committer dates so the two commit SHAs are stable, and every
remaining volatile value (repository root, wall-clock timestamps, ages) is
replaced by a ``${PLACEHOLDER}`` token that the Go tests re-substitute.

Nothing outside the output directory and a private temporary directory is
touched: HOME is redirected before ``code_review_graph`` is imported so the
multi-repo registry, caches and config of the invoking user are never read
or written.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import re
import shutil
import sqlite3
import subprocess
import sys
import tempfile
from pathlib import Path

# Pinned release under test. A mismatch is a hard error: fixtures from any
# other version would silently redefine the contract.
RELEASE_VERSION = "2.3.8"
RELEASE_TAG_COMMIT = "2c6dae32643572ee528eb9b77dbcc17f58f3a8c9"
RELEASE_SCHEMA_VERSION = 9

# Fixed commit identity so the fixture repository's SHAs are reproducible.
COMMIT_ENV = {
    "GIT_AUTHOR_NAME": "crg fixture",
    "GIT_AUTHOR_EMAIL": "fixture@example.com",
    "GIT_COMMITTER_NAME": "crg fixture",
    "GIT_COMMITTER_EMAIL": "fixture@example.com",
    "GIT_AUTHOR_DATE": "2024-01-01T00:00:00+00:00",
    "GIT_COMMITTER_DATE": "2024-01-01T00:00:00+00:00",
}

# Files added by the second commit; everything else is in the first commit.
SECOND_COMMIT_FILES = ("pkg/auth/token.go",)

# The fixture repository's central source file. Most call fixtures target it
# so their payloads stay comparable across tools; naming it once keeps the
# whole CALL_CASES table pointing at the same file.
AUTH_FILE = "pkg/auth/auth.go"

# CLI commands whose option surface the bridge adapter must preserve.
CLI_HELP_COMMANDS = (
    "build",
    "update",
    "postprocess",
    "status",
    "detect-changes",
    "query",
    "impact",
    "search",
    "flows",
    "flow",
    "communities",
    "community",
    "architecture",
    "large-functions",
    "dead-code",
    "refactor",
    "serve",
)

# tools/call fixtures: (case name, tool, arguments). Cases cover a valid call
# with explicit arguments, a defaults-only call, and the three v2.3.8
# validation failures (unknown argument, missing required argument, wrong
# argument type).
CALL_CASES: tuple[tuple[str, str, dict], ...] = (
    ("defaults", "list_graph_stats_tool", {}),
    ("defaults", "get_minimal_context_tool", {}),
    ("task", "get_minimal_context_tool", {"task": "review the auth change"}),
    ("defaults", "get_impact_radius_tool", {}),
    (
        "changed_files",
        "get_impact_radius_tool",
        {"changed_files": [AUTH_FILE], "max_depth": 3},
    ),
    (
        "minimal",
        "get_impact_radius_tool",
        {"changed_files": [AUTH_FILE], "detail_level": "minimal"},
    ),
    (
        # A path with no node rows: the blast radius is empty because nothing
        # about the file is indexed, which is the case the `confidence` marker
        # exists to distinguish from a real zero. estimate_file_tokens also
        # finds no file on disk, so this call carries NO context_savings.
        "unindexed_file",
        "get_impact_radius_tool",
        {"changed_files": ["docs/not-indexed.md"]},
    ),
    (
        "callers_of",
        "query_graph_tool",
        {"pattern": "callers_of", "target": "hashPassword"},
    ),
    (
        # `Login` is called cross-package, so those CALLS edges keep a BARE
        # target and the qualified-target scan finds nothing. Only the
        # bare-name fallback answers, and every result it adds is tagged
        # `target_resolution: "unresolved"`.
        "callers_of_unresolved",
        "query_graph_tool",
        {"pattern": "callers_of", "target": "Login"},
    ),
    (
        "file_summary",
        "query_graph_tool",
        {"pattern": "file_summary", "target": AUTH_FILE},
    ),
    (
        "unknown_pattern",
        "query_graph_tool",
        {"pattern": "no_such_pattern", "target": "Login"},
    ),
    # The remaining patterns the fixture repository can exercise, one per
    # distinct response shape rather than one per pattern name: a bare-target
    # callee projection, a CONTAINS walk that emits no edges, the
    # naming-convention test inference, the import-target projection, and the
    # two empty-result arms that attach an `confidence` marker (a verified
    # language gap, and a "real absence").
    (
        "callees_of",
        "query_graph_tool",
        {"pattern": "callees_of", "target": "handleLogin"},
    ),
    (
        "children_of",
        "query_graph_tool",
        {"pattern": "children_of", "target": AUTH_FILE},
    ),
    (
        "tests_for",
        "query_graph_tool",
        {"pattern": "tests_for", "target": "Login"},
    ),
    (
        "imports_of",
        "query_graph_tool",
        {"pattern": "imports_of", "target": "pkg/auth/auth_test.go"},
    ),
    (
        "importers_of",
        "query_graph_tool",
        {"pattern": "importers_of", "target": AUTH_FILE},
    ),
    (
        "inheritors_of",
        "query_graph_tool",
        {"pattern": "inheritors_of", "target": "Session"},
    ),
    # `map` is in _BUILTIN_CALL_NAMES, so reverse call tracing is skipped
    # before the target is even resolved — and only for a bare name.
    (
        "builtin_target",
        "query_graph_tool",
        {"pattern": "callers_of", "target": "map"},
    ),
    (
        "unresolved_target",
        "query_graph_tool",
        {"pattern": "callers_of", "target": "nosuchsymbol"},
    ),
    # "auth" matches every node under pkg/auth/, which is the disambiguation
    # response: ranked candidates plus the real candidate count.
    (
        "ambiguous_target",
        "query_graph_tool",
        {"pattern": "callers_of", "target": "auth"},
    ),
    (
        "minimal",
        "query_graph_tool",
        {"pattern": "callers_of", "target": "hashPassword", "detail_level": "minimal"},
    ),
    (
        "max_results",
        "query_graph_tool",
        {"pattern": "children_of", "target": AUTH_FILE, "max_results": 2},
    ),
    ("defaults", "get_review_context_tool", {}),
    (
        "minimal",
        "get_review_context_tool",
        {"changed_files": [AUTH_FILE], "detail_level": "minimal"},
    ),
    (
        # include_source=False drops `source_snippets` entirely rather than
        # emitting an empty dict, so the key's absence is the contract.
        "no_source",
        "get_review_context_tool",
        {"changed_files": [AUTH_FILE], "include_source": False},
    ),
    (
        # max_files below the change-set size: the file lists are cut, every
        # `*_total` still reports the untruncated count, `truncated` is set,
        # and snippets are emitted only for the files that survived the cut.
        "max_files",
        "get_review_context_tool",
        {
            "changed_files": [
                AUTH_FILE,
                "cmd/main.go",
                "pkg/auth/auth_test.go",
            ],
            "max_files": 2,
        },
    ),
    (
        # max_lines_per_file below the file's length forces the
        # _extract_relevant_lines path: per-changed-node windows (2 lines of
        # context before, 1 after), merged where they overlap, "..." between
        # kept ranges and "... (truncated)" once the budget is spent.
        "max_lines_per_file",
        "get_review_context_tool",
        {"changed_files": [AUTH_FILE], "max_lines_per_file": 5},
    ),
    ("query", "semantic_search_nodes_tool", {"query": "token"}),
    (
        "kind_filter",
        "semantic_search_nodes_tool",
        {"query": "session", "kind": "Class", "limit": 5},
    ),
    # Without an embedding provider hybrid_search has three reachable modes.
    # "token"/"session" above take the FTS5 path; "andle" is a substring of
    # `handleLogin` that no tokenizer produces, so it can only be found by the
    # LIKE fallback, and a symbol that is absent entirely reports mode "none"
    # plus the empty-result marker.
    ("keyword_fallback", "semantic_search_nodes_tool", {"query": "andle"}),
    ("no_match", "semantic_search_nodes_tool", {"query": "zzqqxx"}),
    (
        "minimal",
        "semantic_search_nodes_tool",
        {"query": "token", "detail_level": "minimal"},
    ),
    ("defaults", "get_docs_section_tool", {"section_name": "usage"}),
    ("unknown", "get_docs_section_tool", {"section_name": "no-such-section"}),
    ("defaults", "find_large_functions_tool", {}),
    ("min_lines", "find_large_functions_tool", {"min_lines": 1, "limit": 5}),
    # min_lines=1 with the default limit returns every node, which is the only
    # way this repository reaches the summary's ten-line cut and its
    # "... and N more" tail.
    ("all", "find_large_functions_tool", {"min_lines": 1}),
    (
        "kind_and_pattern",
        "find_large_functions_tool",
        {
            "min_lines": 3,
            "kind": "Function",
            "file_path_pattern": "pkg/auth",
            "limit": 10,
        },
    ),
    ("defaults", "list_flows_tool", {}),
    ("minimal", "list_flows_tool", {"detail_level": "minimal", "limit": 1}),
    ("sort_by_name", "list_flows_tool", {"sort_by": "name"}),
    ("sort_by_node_count", "list_flows_tool", {"sort_by": "node_count"}),
    ("kind_filter", "list_flows_tool", {"kind": "Function"}),
    ("kind_filter_empty", "list_flows_tool", {"kind": "Test"}),
    ("sort_by_unknown", "list_flows_tool", {"sort_by": "no_such_column"}),
    ("by_id", "get_flow_tool", {"flow_id": 1}),
    ("by_name", "get_flow_tool", {"flow_name": "Login"}),
    ("missing_selector", "get_flow_tool", {}),
    ("include_source", "get_flow_tool", {"flow_id": 1, "include_source": True}),
    ("unknown_id", "get_flow_tool", {"flow_id": 999}),
    # An explicitly supplied EMPTY name is not "no selector": upstream tests
    # `flow_name is not None`, and `"" in name` matches, so this selects the
    # most critical flow.
    ("empty_name", "get_flow_tool", {"flow_name": ""}),
    ("defaults", "get_affected_flows_tool", {}),
    # An explicitly supplied EMPTY list means "nothing changed" and must not
    # trigger git auto-detection, which is a different payload shape from the
    # auto-detected one: no `changed_files`, no `truncated`, no `_hints`.
    ("no_changes", "get_affected_flows_tool", {"changed_files": []}),
    (
        "changed_files",
        "get_affected_flows_tool",
        {"changed_files": [AUTH_FILE], "detail_level": "minimal"},
    ),
    (
        "max_flows",
        "get_affected_flows_tool",
        {"changed_files": [AUTH_FILE], "max_flows": 1},
    ),
    ("defaults", "list_communities_tool", {}),
    ("minimal", "list_communities_tool", {"detail_level": "minimal"}),
    ("min_size", "list_communities_tool", {"min_size": 4}),
    ("max_members", "list_communities_tool", {"max_members": 1}),
    ("max_results", "list_communities_tool", {"max_results": 1, "detail_level": "minimal"}),
    (
        "sort_cohesion",
        "list_communities_tool",
        {"sort_by": "cohesion", "detail_level": "minimal"},
    ),
    (
        "sort_unknown",
        "list_communities_tool",
        {"sort_by": "no_such_column", "detail_level": "minimal"},
    ),
    ("by_id", "get_community_tool", {"community_id": 1, "include_members": True}),
    (
        "by_name",
        "get_community_tool",
        {"community_name": "auth", "include_members": True, "max_members": 2},
    ),
    ("unknown_id", "get_community_tool", {"community_id": 999}),
    # An id that matches nothing does NOT fall back to the name selector.
    (
        "id_wins_over_name",
        "get_community_tool",
        {"community_id": 999, "community_name": "auth"},
    ),
    ("missing_selector", "get_community_tool", {}),
    ("defaults", "get_architecture_overview_tool", {}),
    ("standard", "get_architecture_overview_tool", {"detail_level": "standard"}),
    (
        "standard_max_members",
        "get_architecture_overview_tool",
        {"detail_level": "standard", "max_members": 1},
    ),
    ("defaults", "detect_changes_tool", {}),
    (
        "minimal",
        "detect_changes_tool",
        {"changed_files": [AUTH_FILE], "detail_level": "minimal"},
    ),
    ("defaults", "get_hub_nodes_tool", {}),
    ("minimal", "get_hub_nodes_tool", {"top_n": 3, "detail_level": "minimal"}),
    # `top_n` far above the candidate count: the ceiling never engages, so
    # `truncated` must be false and the summary must carry no ", showing N of
    # M" fragment even though a bound was explicitly requested.
    ("top_n_over_count", "get_hub_nodes_tool", {"top_n": 500}),
    ("defaults", "get_knowledge_gaps_tool", {}),
    # `minimal` drops the file paths from every gap category; `max_per_category`
    # cuts the lists while `summary`/`total_gaps` keep reporting the
    # untruncated counts, which is the whole point of the category totals.
    ("minimal", "get_knowledge_gaps_tool", {"detail_level": "minimal"}),
    ("max_per_category", "get_knowledge_gaps_tool", {"max_per_category": 1}),
    ("defaults", "get_surprising_connections_tool", {}),
    (
        "minimal",
        "get_surprising_connections_tool",
        {"top_n": 3, "detail_level": "minimal"},
    ),
    ("defaults", "get_suggested_questions_tool", {}),
    ("query", "traverse_graph_tool", {"query": "Login"}),
    ("dfs", "traverse_graph_tool", {"query": "Login", "mode": "dfs", "depth": 2}),
    # `token_budget=1` is below the cost of even the start node, so the budget
    # check fires before the first entry is appended: an empty `traversal`
    # with `truncated` true and `start_node` still reported. The budget is
    # deliberately 1 rather than a mid-traversal value: each entry's cost is
    # `len(str(entry)) // 4` over a dict holding two ABSOLUTE paths, so any
    # threshold that cuts mid-traversal moves with the length of the
    # repository root and would not reproduce outside this generator run.
    ("token_budget", "traverse_graph_tool", {"query": "Login", "token_budget": 1}),
    # No FTS or LIKE hit: upstream returns a bare {error, nodes} dict, not the
    # standard `_error_response` shape, and the call is NOT an MCP error.
    ("no_match", "traverse_graph_tool", {"query": "zzzznomatch"}),
    ("dead_code", "refactor_tool", {"mode": "dead_code"}),
    ("suggest", "refactor_tool", {"mode": "suggest"}),
    (
        "rename_missing_names",
        "refactor_tool",
        {"mode": "rename"},
    ),
    ("defaults", "list_repos_tool", {}),
    ("query", "cross_repo_search_tool", {"query": "Login"}),
    ("defaults", "get_bridge_nodes_tool", {}),
    ("minimal", "get_bridge_nodes_tool", {"top_n": 3, "detail_level": "minimal"}),
    # Bound violations, one case per (tool, guarded argument). The published
    # schemas carry no `minimum`, so 0 and negatives bind successfully and
    # then fail the release's own _validate_positive_int check — which runs
    # OUTSIDE the tool's try block, so the ValueError escapes to FastMCP and
    # the envelope is a TRANSPORT error (is_error, no structured content),
    # not an in-band {"status": "error"} payload. A native handler must
    # therefore return a Go error here, never an error payload.
    #
    # Only the arguments below are guarded; every other integer argument on a
    # native tool was probed against the release and returns a normal
    # payload, so a sweep that assumed "integer argument implies guarded"
    # would record the wrong expectation. Verified UNGUARDED:
    # get_impact_radius/get_review_context `max_depth`, semantic_search
    # `limit`, find_large_functions `limit` and `min_lines`,
    # get_affected_flows `max_flows`, list_communities `min_size`,
    # traverse_graph `depth` and `token_budget`.
    #
    # The case name carries the argument under test because the fixture
    # filename is `<tool>__<case>.json`: two cases sharing a (tool, case)
    # pair would overwrite each other, which the duplicate check below now
    # rejects outright.
    ("bound_max_results", "query_graph_tool", {
        "pattern": "callers_of", "target": "Login", "max_results": 0,
    }),
    ("bound_max_results", "get_review_context_tool", {"max_results": 0}),
    ("bound_max_files", "get_review_context_tool", {"max_files": 0}),
    ("bound_max_lines_per_file", "get_review_context_tool", {
        "max_lines_per_file": -1,
    }),
    ("bound_limit", "list_flows_tool", {"limit": 0}),
    ("bound_max_steps", "get_flow_tool", {"flow_id": 1, "max_steps": 0}),
    ("bound_max_source_lines", "get_flow_tool", {
        "flow_id": 1, "max_source_lines": 0,
    }),
    ("bound_max_results", "list_communities_tool", {"max_results": 0}),
    ("bound_max_members", "list_communities_tool", {"max_members": 0}),
    ("bound_max_members", "get_community_tool", {
        "community_id": 1, "max_members": 0,
    }),
    ("bound_max_results", "get_architecture_overview_tool", {"max_results": 0}),
    ("bound_max_members", "get_architecture_overview_tool", {
        "max_members": -1,
    }),
    ("bound_top_n", "get_hub_nodes_tool", {"top_n": -1}),
    ("bound_max_per_category", "get_knowledge_gaps_tool", {
        "max_per_category": 0,
    }),
    ("bound_top_n", "get_surprising_connections_tool", {"top_n": 0}),
    # Validation failures — the exact v2.3.8 error semantics.
    ("invalid_unknown_argument", "list_graph_stats_tool", {"bogus": 1}),
    ("invalid_missing_required", "query_graph_tool", {}),
    ("invalid_wrong_type", "get_impact_radius_tool", {"max_depth": "deep"}),
    (
        "invalid_wrong_type",
        "semantic_search_nodes_tool",
        {"query": "x", "limit": "many"},
    ),
)

# Tools deliberately not exercised: they mutate the working tree, require an
# embedding provider, or need a prior tool's opaque id. Their contract is the
# tools/list schema plus the capability routing decision.
UNEXERCISED_TOOLS = {
    "build_or_update_graph_tool",  # covered by the lifecycle fixtures
    "run_postprocess_tool",  # covered by the lifecycle fixtures
    "embed_graph_tool",
    "generate_wiki_tool",
    "get_wiki_page_tool",
    "apply_refactor_tool",
}


def run_git(repo: Path, *args: str) -> str:
    env = {**os.environ, **COMMIT_ENV}
    out = subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        capture_output=True,
        text=True,
        env=env,
    )
    return out.stdout.strip()


def materialize_repo(source: Path, dest: Path, staging: Path) -> tuple[str, str]:
    """Copy the fixture sources into *dest* and build a two-commit history.

    The second commit's files are held in *staging* — outside the work tree —
    while the first commit is made, so the first commit contains no trace of
    them and ``git diff <first>`` reports exactly the added paths.
    """
    shutil.copytree(source, dest)
    run_git(dest, "init", "-q", "-b", "main")
    run_git(dest, "config", "commit.gpgsign", "false")
    staging.mkdir(parents=True, exist_ok=True)
    for index, rel in enumerate(SECOND_COMMIT_FILES):
        shutil.move(str(dest / rel), str(staging / f"{index}.hold"))
    run_git(dest, "add", "-A")
    run_git(dest, "commit", "-q", "-m", "fixture: initial")
    first = run_git(dest, "rev-parse", "HEAD")
    for index, rel in enumerate(SECOND_COMMIT_FILES):
        shutil.move(str(staging / f"{index}.hold"), str(dest / rel))
    run_git(dest, "add", "-A")
    run_git(dest, "commit", "-q", "-m", "fixture: mint tokens")
    second = run_git(dest, "rev-parse", "HEAD")
    return first, second


def dump_tools_list() -> list[dict]:
    """Capture every published tool's name, description and input schema.

    ``parameter_order`` is the tool function's declared parameter order. It is
    NOT recoverable from the JSON schema (the schema's properties are emitted
    sorted), yet it is observable: the validator reports field errors in
    declaration order, so a server that reorders them does not reproduce the
    release's error semantics.
    """
    import inspect

    from code_review_graph.main import mcp

    async def _list() -> list[dict]:
        tools = await mcp.list_tools()
        out = []
        for tool in tools:
            mcp_tool = tool.to_mcp_tool()
            data = mcp_tool.model_dump(exclude_none=True, mode="json")
            func = getattr(tool, "fn", None)
            if func is None:
                raise RuntimeError(f"cannot read parameter order for {data['name']}")
            out.append(
                {
                    "name": data["name"],
                    "description": data.get("description", ""),
                    "inputSchema": data["inputSchema"],
                    "parameter_order": list(inspect.signature(func).parameters),
                }
            )
        return out

    return asyncio.run(_list())


def _prompt_messages(result) -> list[dict]:
    return [
        message.model_dump(exclude_none=True, mode="json")
        for message in result.messages
    ]


def _templatize(messages: list[dict], markers: dict[str, str]) -> list[dict]:
    """Turn each argument's unique marker back into a ``${arg:name}`` token."""
    for message in messages:
        content = message.get("content", {})
        if not isinstance(content, dict) or not isinstance(content.get("text"), str):
            continue
        for name, marker in markers.items():
            content["text"] = content["text"].replace(marker, "${arg:" + name + "}")
    return messages


async def _capture_prompt(client, prompt) -> dict:
    """Record one prompt's declaration plus its two pinned renderings.

    Two renderings pin the contract: one with no arguments at all (the
    prompt's own defaults) and one with a unique marker per argument, which
    turns the rendered text into a template by showing exactly where each
    argument is interpolated.
    """
    record = prompt.model_dump(exclude_none=True, mode="json")
    names = [a["name"] for a in record.get("arguments", [])]
    default_render = await client.get_prompt(record["name"], {})
    markers = {name: f"<<<ARG:{name}>>>" for name in names}
    marked_render = (
        await client.get_prompt(record["name"], markers)
        if markers
        else default_render
    )
    record["rendered_defaults"] = _prompt_messages(default_render)
    record["rendered_template"] = _templatize(_prompt_messages(marked_render), markers)
    return record


def dump_prompts() -> list[dict]:
    """Capture the published prompt surface and each prompt's rendered text.

    Prompts are part of the same MCP server contract as tools: a client that
    calls ``prompts/list`` against a drop-in replacement must see the same
    names, arguments and messages.
    """
    from fastmcp import Client
    from code_review_graph import main as crg_main

    async def _run() -> list[dict]:
        async with Client(crg_main.mcp) as client:
            prompts = await client.list_prompts()
            return [await _capture_prompt(client, prompt) for prompt in prompts]

    return asyncio.run(_run())


def dump_languages() -> dict:
    """Capture the release's indexed-source inventory.

    ``parser.EXTENSION_TO_LANGUAGE`` is the authoritative extension→language
    map: ``ParserRegistry`` seeds ``_extension_map`` from it, and
    ``detect_language`` lowercases a file's suffix and looks it up there.
    Notebooks (``.ipynb`` → ``notebook``) and Terraform/HCL (``.tf``/``.hcl``
    → ``hcl``) are ordinary entries in that map, not special cases.

    Config-driven custom languages (``.code-review-graph/languages.toml``) are
    deliberately excluded: they are per-repo opt-in, and
    ``custom_languages._validate_entry`` refuses any entry that reuses a
    built-in extension or language name, so the built-in map is exactly the
    inventory every repository is indexed under.

    Shebang routing (``SHEBANG_INTERPRETER_TO_LANGUAGE``) is excluded for the
    same reason it exists: it routes extension-LESS scripts to languages that
    are already in the map, so it adds no language and no extension. It is the
    one part of the inventory an extension cannot express, and the consumer
    (internal/codegraph.ScanCapability) closes that gap by probing for a
    ``#!`` prefix directly.

    ``detect_language`` refines three of these entries by PATH rather than by
    extension — ``.yml``/``.yaml`` become ``ansible`` or ``spring_config``,
    ``.properties`` becomes ``spring_config`` (and is otherwise dropped),
    ``*.blade.php`` becomes ``blade``. The refinements are not recorded: each
    one only relabels an extension the map already claims, and the consumer
    must be CONSERVATIVE — treating ``.properties`` as indexable over-reports
    bridge work, whereas dropping it would silently lose a Spring repo's
    config nodes.
    """
    from code_review_graph.parser import EXTENSION_TO_LANGUAGE

    languages: dict[str, list[str]] = {}
    for extension, language in EXTENSION_TO_LANGUAGE.items():
        languages.setdefault(language, []).append(extension)
    return {
        "languages": {name: sorted(exts) for name, exts in sorted(languages.items())},
        "extensions": dict(sorted(EXTENSION_TO_LANGUAGE.items())),
    }


def dump_calls(repo: Path) -> list[dict]:
    from fastmcp import Client
    from code_review_graph import main as crg_main

    crg_main._default_repo_root = str(repo)

    from code_review_graph.hints import reset_session

    async def _run() -> list[dict]:
        results = []
        async with Client(crg_main.mcp) as client:
            for case, tool, arguments in CALL_CASES:
                # The release's `_hints` block suppresses tools already called
                # in the MCP session, so a shared session would make each
                # fixture depend on the order of every case before it. Each
                # case therefore starts from a fresh session; the
                # order-dependent suppression is pinned separately by
                # dump_session_sequence().
                reset_session()
                record: dict = {
                    "tool": tool,
                    "case": case,
                    "arguments": arguments,
                }
                try:
                    result = await client.call_tool(
                        tool, arguments, raise_on_error=False
                    )
                except Exception as exc:  # pragma: no cover - client-side error
                    record["client_error"] = f"{type(exc).__name__}: {exc}"
                    results.append(record)
                    continue
                record["is_error"] = bool(result.is_error)
                record["content"] = [
                    block.model_dump(exclude_none=True, mode="json")
                    for block in result.content
                ]
                if result.structured_content is not None:
                    record["structured_content"] = result.structured_content
                results.append(record)
        return results

    return asyncio.run(_run())


def dump_session_sequence(repo: Path) -> list[dict]:
    """Capture `_hints` across consecutive calls in ONE MCP session.

    ``_hints.next_steps`` drops tools already called in the session, so the
    block is session-stateful. A drop-in replacement that emitted static
    hints would match every single-call fixture and still be wrong for a real
    client, which is why the sequence is pinned on its own.
    """
    from fastmcp import Client
    from code_review_graph import main as crg_main
    from code_review_graph.hints import reset_session

    crg_main._default_repo_root = str(repo)
    sequence = [
        ("list_flows_tool", {}),
        ("get_flow_tool", {"flow_id": 1}),
        ("list_flows_tool", {}),
        ("get_affected_flows_tool", {}),
    ]

    async def _run() -> list[dict]:
        reset_session()
        out = []
        async with Client(crg_main.mcp) as client:
            for tool, arguments in sequence:
                result = await client.call_tool(tool, arguments, raise_on_error=False)
                structured = result.structured_content or {}
                out.append(
                    {
                        "tool": tool,
                        "arguments": arguments,
                        "hints": structured.get("_hints"),
                    }
                )
        return out

    return asyncio.run(_run())


def dump_lifecycle(repo: Path, first: str) -> dict:
    """Capture the build / update / postprocess result dicts and staleness."""
    from code_review_graph.tools.build import build_or_update_graph, run_postprocess

    lifecycle: dict = {}
    lifecycle["full_build"] = build_or_update_graph(
        full_rebuild=True, repo_root=str(repo)
    )
    lifecycle["postprocess_none_build"] = build_or_update_graph(
        full_rebuild=True, repo_root=str(repo), postprocess="none"
    )
    lifecycle["postprocess_minimal_build"] = build_or_update_graph(
        full_rebuild=True, repo_root=str(repo), postprocess="minimal"
    )
    lifecycle["full_build_again"] = build_or_update_graph(
        full_rebuild=True, repo_root=str(repo)
    )
    lifecycle["summary_tables_after_full_build"] = summary_counts(repo)

    # A standalone postprocess recomputes flows, communities and FTS but
    # deliberately does NOT call _compute_summaries, so the three summary
    # tables are left exactly as the last full build wrote them. Emptying
    # them first makes that observable: after a standalone postprocess they
    # are still empty even though flows/communities were recomputed.
    truncate_summary_tables(repo)
    lifecycle["summary_tables_before_standalone_postprocess"] = summary_counts(repo)
    lifecycle["standalone_postprocess"] = run_postprocess(repo_root=str(repo))
    lifecycle["summary_tables_after_standalone_postprocess"] = summary_counts(repo)
    lifecycle["standalone_postprocess_flows_only"] = run_postprocess(
        repo_root=str(repo), communities=False, fts=False
    )
    lifecycle["standalone_postprocess_no_flows"] = run_postprocess(
        repo_root=str(repo), flows=False, communities=False, fts=False
    )
    lifecycle["summary_tables_after_selective_postprocess"] = summary_counts(repo)
    # Restore the summary tables so the graph fixtures below are complete.
    lifecycle["full_build_restoring_summaries"] = build_or_update_graph(
        full_rebuild=True, repo_root=str(repo)
    )
    lifecycle["summary_tables_restored"] = summary_counts(repo)

    # Automatic (base=None) incremental update: resolves the base from the
    # last-built commit.
    lifecycle["incremental_auto_base_no_changes"] = build_or_update_graph(
        repo_root=str(repo)
    )
    lifecycle["incremental_explicit_base"] = build_or_update_graph(
        repo_root=str(repo), base=first
    )
    lifecycle["incremental_unresolvable_base"] = build_or_update_graph(
        repo_root=str(repo), base="0000000000000000000000000000000000000000"
    )
    # An EXPLICIT empty base is upstream's third state. build.py only
    # resolves the base when it `is None`, so "" is passed through as a ref
    # that matches nothing: the diff is empty and base_resolved echoes "".
    # Pinning it is what stops a Go port from collapsing absent and empty
    # into one "resolve automatically" case and reporting the last-built
    # commit where upstream reports "".
    lifecycle["incremental_empty_base"] = build_or_update_graph(
        repo_root=str(repo), base=""
    )

    # A real incremental update: an uncommitted edit is in `git diff HEAD`,
    # so the auto-resolved base reports it and exactly one file is re-parsed.
    # The original mtime is restored with the content: several tools compare
    # source mtimes against the graph's build time to report staleness, so a
    # file left with a just-now mtime makes those fields race the clock.
    edited = repo / "pkg" / "auth" / "auth.go"
    original = edited.read_text(encoding="utf-8")
    original_stat = edited.stat()
    edited.write_text(
        original + "\n// touched by the release-contract fixture generator\n",
        encoding="utf-8",
    )
    try:
        lifecycle["incremental_auto_base_with_changes"] = build_or_update_graph(
            repo_root=str(repo)
        )
    finally:
        edited.write_text(original, encoding="utf-8")
        os.utime(edited, (original_stat.st_atime, original_stat.st_mtime))
    # Leave the graph matching the committed tree for the graph fixtures.
    lifecycle["final_full_build"] = build_or_update_graph(
        full_rebuild=True, repo_root=str(repo)
    )
    return lifecycle


def dump_clean_build(repo: Path) -> dict:
    """Full build against a pristine database (row ids start at 1)."""
    from code_review_graph.tools.build import build_or_update_graph

    return build_or_update_graph(full_rebuild=True, repo_root=str(repo))


def truncate_summary_tables(repo: Path) -> None:
    """Empty the three summary tables a standalone postprocess leaves alone."""
    conn = connect(repo)
    try:
        for table in ("community_summaries", "flow_snapshots", "risk_index"):
            conn.execute(f"DELETE FROM {table}")
        conn.commit()
    finally:
        conn.close()


def db_path(repo: Path) -> Path:
    return repo / ".code-review-graph" / "graph.db"


def connect(repo: Path) -> sqlite3.Connection:
    conn = sqlite3.connect(db_path(repo))
    conn.row_factory = sqlite3.Row
    return conn


def summary_counts(repo: Path) -> dict:
    conn = connect(repo)
    try:
        counts = {}
        for table in (
            "flows",
            "flow_memberships",
            "communities",
            "community_summaries",
            "flow_snapshots",
            "risk_index",
            "nodes_fts",
        ):
            counts[table] = conn.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
        return counts
    finally:
        conn.close()


def dump_schema(repo: Path) -> dict:
    conn = connect(repo)
    try:
        objects = {"tables": {}, "indexes": {}, "views": {}, "triggers": {}}
        bucket = {
            "table": "tables",
            "index": "indexes",
            "view": "views",
            "trigger": "triggers",
        }
        rows = conn.execute(
            "SELECT type, name, sql FROM sqlite_master ORDER BY type, name"
        ).fetchall()
        for row in rows:
            if row["sql"] is None:
                continue
            objects[bucket[row["type"]]][row["name"]] = row["sql"]
        metadata = {
            row["key"]: row["value"] for row in conn.execute("SELECT key, value FROM metadata")
        }
        return {
            "schema_version": int(metadata.get("schema_version", "0")),
            "metadata_keys": sorted(metadata),
            "objects": objects,
        }
    finally:
        conn.close()


def dump_graph(repo: Path) -> dict:
    conn = connect(repo)
    try:

        def rows(sql: str) -> list[dict]:
            return [dict(r) for r in conn.execute(sql)]

        return {
            "nodes": rows(
                "SELECT id, kind, name, qualified_name, file_path, line_start, "
                "line_end, language, parent_name, params, return_type, modifiers, "
                "is_test, signature, community_id FROM nodes ORDER BY id"
            ),
            "edges": rows(
                "SELECT id, kind, source_qualified, target_qualified, file_path, "
                "line, confidence, confidence_tier FROM edges ORDER BY id"
            ),
            "flows": rows(
                "SELECT id, name, entry_point_id, depth, node_count, file_count, "
                "criticality, path_json FROM flows ORDER BY id"
            ),
            "flow_memberships": rows(
                "SELECT flow_id, node_id, position FROM flow_memberships "
                "ORDER BY flow_id, position"
            ),
            "communities": rows(
                "SELECT id, name, level, parent_id, cohesion, size, "
                "dominant_language, description FROM communities ORDER BY id"
            ),
            "community_summaries": rows(
                "SELECT community_id, name, purpose, key_symbols, risk, size, "
                "dominant_language FROM community_summaries ORDER BY community_id"
            ),
            "flow_snapshots": rows(
                "SELECT flow_id, name, entry_point, critical_path, criticality, "
                "node_count, file_count FROM flow_snapshots ORDER BY flow_id"
            ),
            "risk_index": rows(
                "SELECT node_id, qualified_name, risk_score, caller_count, "
                "test_coverage, security_relevant FROM risk_index ORDER BY node_id"
            ),
            "fts": rows(
                "SELECT rowid AS node_id, name, qualified_name, file_path, "
                "signature FROM nodes_fts ORDER BY rowid"
            ),
        }
    finally:
        conn.close()


def dump_cli(crg_bin: Path, repo: Path) -> dict:
    """Capture the CLI surface of the validated code-review-graph binary."""
    argv0 = os.fspath(crg_bin)
    out: dict = {"help": {}}
    for command in CLI_HELP_COMMANDS:
        proc = subprocess.run(
            [argv0, command, "--help"],
            capture_output=True,
            text=True,
            check=False,
            env={**os.environ, "COLUMNS": "100"},
        )
        out["help"][command] = proc.stdout
    for name, args in (
        ("status_json", ["status", "--repo", str(repo), "--json"]),
        (
            "detect_changes_json",
            ["detect-changes", "--repo", str(repo), "--base", "HEAD~1", "--json"],
        ),
        ("dead_code_json", ["dead-code", "--repo", str(repo), "--json"]),
    ):
        proc = subprocess.run(
            [argv0, *args], capture_output=True, text=True, check=False
        )
        out[name] = {
            "exit_code": proc.returncode,
            "stdout": proc.stdout,
        }
    return out


# Volatile values also appear INSIDE strings: every tools/call response
# carries a compact JSON rendering of its own structured content in
# ``content[0].text``, and several tools embed the graph's build timestamp in
# a human-readable summary. Normalizing the parsed structure alone would
# leave those copies volatile, so the same substitutions are applied to
# string bodies with anchored patterns.
_ISO_TIMESTAMP_RE = re.compile(
    r"\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[+-]\d{2}:\d{2}|Z)?"
)
_JSON_AGE_RE = re.compile(r'("age_seconds"\s*:\s*)-?\d+(?:\.\d+)?')


def _placeholder_for(key: str, item):
    """Return the ``${PLACEHOLDER}`` a volatile key/value pair collapses to.

    ``None`` means the value is not volatile by virtue of its key and should
    be normalized structurally instead.
    """
    if key in TIMESTAMP_KEYS and isinstance(item, str) and item:
        return "${TIMESTAMP}"
    if key in AGE_KEYS and isinstance(item, (int, float)):
        return "${AGE_SECONDS}"
    if key in DURATION_KEYS and isinstance(item, (int, float)):
        return "${DURATION}"
    if key in SAVINGS_KEYS and isinstance(item, (int, float)):
        return "${" + key.upper() + "}"
    return None


def _normalize_text(text: str, repo: Path, commits: dict[str, str]) -> str:
    """Apply the same substitutions to a string body as to parsed structure."""
    for real in (str(Path(repo).resolve()), str(repo)):
        text = text.replace(real, "${REPO_ROOT}")
    for sha, token in commits.items():
        text = text.replace(sha, token)
        text = text.replace(sha[:8], token + "_SHORT")
    text = _ISO_TIMESTAMP_RE.sub("${TIMESTAMP}", text)
    text = _JSON_AGE_RE.sub(r'\1"${AGE_SECONDS}"', text)
    for key in DURATION_KEYS:
        text = re.sub(
            r'("' + key + r'"\s*:\s*)-?\d+(?:\.\d+)?',
            r'\1"${DURATION}"',
            text,
        )
    for key in SAVINGS_KEYS:
        text = re.sub(
            r'("' + key + r'"\s*:\s*)-?\d+(?:\.\d+)?',
            r'\1"${' + key.upper() + '}"',
            text,
        )
    return text


def normalize(value, repo: Path, commits: dict[str, str]):
    """Replace volatile values with stable ``${PLACEHOLDER}`` tokens."""
    if isinstance(value, dict):
        out = {}
        for key, item in value.items():
            placeholder = _placeholder_for(key, item)
            out[key] = (
                normalize(item, repo, commits)
                if placeholder is None
                else placeholder
            )
        return out
    if isinstance(value, list):
        return [normalize(item, repo, commits) for item in value]
    if isinstance(value, str):
        return _normalize_text(value, repo, commits)
    return value


TIMESTAMP_KEYS = {
    "last_updated",
    "updated_at",
    "last_computed",
    "last_postprocessed_at",
    "generated_at",
    "timestamp",
}
AGE_KEYS = {"age_seconds"}
# `context_savings` estimates tokens from a payload that embeds ABSOLUTE file
# paths, so its two numbers are a function of the generation root's string
# length and cannot be reproduced on a machine with a different temporary
# directory. They are normalized away rather than recorded as if they were
# stable: the estimator itself is pinned by a direct test against Python's
# json.dumps length, and each tool test derives the expected value for its own
# root from the release's recorded payloads.
SAVINGS_KEYS = {"saved_tokens", "saved_percent"}
DURATION_KEYS = {
    "duration_s",
    "elapsed_s",
    "signatures_s",
    "fts_s",
    "flows_s",
    "communities_s",
    "summaries_s",
    "build_time_s",
    "parse_time_s",
}


def write_json(path: Path, payload) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(payload, indent=2, sort_keys=True, default=str) + "\n",
        encoding="utf-8",
    )


# The generator runs whatever program ``--crg-bin`` names and rewrites the
# whole fixture tree under whatever directory ``--out`` names. Both are
# operator-supplied strings, so both are pinned down at the single point
# where they enter the program: this script drives exactly one executable and
# owns exactly one tree inside the repository it is run from, and it should
# refuse to run a different program or to write anywhere else rather than do
# either by accident.
_CRG_BIN_NAME_RE = re.compile(r"code-review-graph(?:\.exe)?")


def resolve_crg_bin(value: str) -> Path | None:
    """Resolve ``--crg-bin`` to the code-review-graph executable.

    The basename must be exactly the program this generator knows how to
    drive; the executable path is then re-derived from that allowlisted name
    (through ``PATH`` when the value carries no directory) and must be a
    real, executable file. Returns the resolved absolute path, or ``None``
    when the value does not name a runnable code-review-graph.
    """
    name = Path(value).name
    if not _CRG_BIN_NAME_RE.fullmatch(name):
        return None
    has_dir = os.sep in value or (os.altsep is not None and os.altsep in value)
    if has_dir:
        candidate: str | None = os.fspath(Path(value).parent / name)
    else:
        candidate = shutil.which(name)
    if candidate is None:
        return None
    resolved = Path(candidate).resolve()
    if not os.path.isfile(resolved) or not os.access(resolved, os.X_OK):
        return None
    return resolved


def resolve_out_dir(value: str) -> Path | None:
    """Resolve ``--out`` to a fixture directory inside the working tree.

    The generator creates, overwrites and deletes files beneath this path, so
    it is confined to the repository it is invoked from: ``..`` segments, an
    absolute path elsewhere on the machine, or a symlink leading out of the
    tree are rejected. Returns the resolved absolute path, or ``None`` when
    it falls outside the current working directory.
    """
    root = Path.cwd().resolve()
    resolved = Path(value).resolve()
    if not resolved.is_relative_to(root):
        return None
    return resolved


def main() -> int:
    # A call fixture is written to `calls/<tool>__<case>.json`, so two
    # CALL_CASES entries sharing a (tool, case) pair silently overwrite each
    # other — last one in the tuple wins, with no diff and no error. On a
    # fixture directory a dozen contributors append to, that is a defect in
    # the generator rather than a matter of discipline, so it is rejected
    # here, before the workspace is created and before anything is written.
    seen: dict[tuple[str, str], dict] = {}
    for case, tool, arguments in CALL_CASES:
        key = (tool, case)
        if key in seen:
            print(
                f"duplicate call fixture {tool}__{case}.json: "
                f"{seen[key]} and {arguments}. Case names must be unique per "
                f"tool — name the case after what it varies.",
                file=sys.stderr,
            )
            return 2
        seen[key] = arguments

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--out",
        default="testdata/crg-release/v2.3.8",
        help="fixture output directory",
    )
    parser.add_argument(
        "--crg-bin",
        default=shutil.which("code-review-graph") or "code-review-graph",
        help="code-review-graph executable used for the CLI fixtures",
    )
    args = parser.parse_args()

    crg_bin = resolve_crg_bin(args.crg_bin)
    if crg_bin is None:
        print(
            f"--crg-bin {args.crg_bin!r} is not a runnable code-review-graph "
            f"executable. Pass the path to the installed code-review-graph "
            f"binary (for example .venv/bin/code-review-graph), or put it on "
            f"PATH and omit the flag.",
            file=sys.stderr,
        )
        return 2

    out = resolve_out_dir(args.out)
    if out is None:
        print(
            f"--out {args.out!r} resolves outside {Path.cwd().resolve()}. The "
            f"generator rewrites the repository's own fixture tree, so run it "
            f"from the repository root with an --out inside it (default: "
            f"testdata/crg-release/v{RELEASE_VERSION}).",
            file=sys.stderr,
        )
        return 2

    source = out / "repo"
    if not source.is_dir():
        print(f"fixture sources missing: {source}", file=sys.stderr)
        return 2

    workspace = Path(tempfile.mkdtemp(prefix="crg-release-"))
    # Never read or write the invoking user's home: the multi-repo registry,
    # caches and platform config all live under it.
    fake_home = workspace / "home"
    fake_home.mkdir()
    os.environ["HOME"] = str(fake_home)
    os.environ["USERPROFILE"] = str(fake_home)
    os.environ["XDG_CONFIG_HOME"] = str(fake_home / ".config")
    os.environ["CRG_RECURSE_SUBMODULES"] = "0"

    import importlib.metadata as importlib_metadata

    installed = importlib_metadata.version("code-review-graph")
    if installed != RELEASE_VERSION:
        print(
            f"installed code-review-graph {installed} != pinned {RELEASE_VERSION}",
            file=sys.stderr,
        )
        return 2
    fastmcp_version = importlib_metadata.version("fastmcp")

    repo = workspace / "repo"
    first, second = materialize_repo(source, repo, workspace / "staging")
    commits = {second: "${HEAD_SHA}", first: "${BASE_SHA}"}

    lifecycle = dump_lifecycle(repo, first)
    tools = dump_tools_list()
    prompts = dump_prompts()
    languages = dump_languages()
    # Node/edge/flow/community row ids are AUTOINCREMENT and therefore carry
    # over the lifecycle captures above. Every fixture that embeds an id is
    # taken from a pristine database so the ids start at 1 and regenerating
    # the fixtures is reproducible.
    shutil.rmtree(repo / ".code-review-graph", ignore_errors=True)
    lifecycle["clean_full_build"] = dump_clean_build(repo)
    calls = dump_calls(repo)
    session_sequence = dump_session_sequence(repo)
    schema = dump_schema(repo)
    graph = dump_graph(repo)
    cli = dump_cli(crg_bin, repo)

    if schema["schema_version"] != RELEASE_SCHEMA_VERSION:
        print(
            f"schema version {schema['schema_version']} != pinned "
            f"{RELEASE_SCHEMA_VERSION}",
            file=sys.stderr,
        )
        return 2

    covered = {tool for _, tool, _ in CALL_CASES}
    missing = {tool["name"] for tool in tools} - covered - UNEXERCISED_TOOLS
    if missing:
        print(f"tools without a call fixture: {sorted(missing)}", file=sys.stderr)
        return 2

    write_json(
        out / "release.json",
        {
            "version": RELEASE_VERSION,
            "tag_commit": RELEASE_TAG_COMMIT,
            "schema_version": RELEASE_SCHEMA_VERSION,
            "fastmcp_version": fastmcp_version,
            "protocol_version": "2025-06-18",
            "tool_count": len(tools),
            "base_sha": first,
            "head_sha": second,
            "unexercised_tools": sorted(UNEXERCISED_TOOLS),
        },
    )
    write_json(out / "tools-list.json", tools)
    write_json(out / "prompts.json", prompts)
    write_json(
        out / "session-sequence.json", normalize(session_sequence, repo, commits)
    )
    write_json(out / "languages.json", languages)
    write_json(out / "sqlite-schema.json", schema)
    write_json(out / "graph.json", normalize(graph, repo, commits))
    write_json(out / "lifecycle.json", normalize(lifecycle, repo, commits))
    write_json(out / "cli.json", normalize(cli, repo, commits))
    calls_dir = out / "calls"
    if calls_dir.is_dir():
        shutil.rmtree(calls_dir)
    for record in calls:
        write_json(
            calls_dir / f"{record['tool']}__{record['case']}.json",
            normalize(record, repo, commits),
        )

    shutil.rmtree(workspace, ignore_errors=True)
    print(f"wrote {len(tools)} tool schemas and {len(calls)} call fixtures to {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
