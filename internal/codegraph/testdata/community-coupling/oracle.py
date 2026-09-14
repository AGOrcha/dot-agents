"""Produce the code-review-graph v2.3.8 answers for a cross-community graph.

The generated release fixtures in testdata/crg-release/v2.3.8 are built from a
repository whose only inter-package calls resolve to BARE target names, so its
`cross_community_edges` list is empty and its coupling-warning list is empty.
That leaves the cross-community projection, the minimal-mode pair aggregation
and the coupling warnings of `get_architecture_overview_tool` — plus a
discriminating `sort_by=name` ordering and a partial `community_name` match —
completely unexercised.

This script therefore materialises graph.json (the hand-authored synthetic graph
next to it) into a real release database and records what the release returns,
exactly the way tools/crgrelease/generate_release_contract.py records the
generated fixtures: `${REPO_ROOT}` is substituted on the way in and normalised
back out, so the recorded answers are machine-independent.

Reproduce with:

    cd internal/codegraph/testdata/community-coupling
    HOME=$(mktemp -d) /tmp/crgvenv/bin/python oracle.py

Dependencies: the pinned release installed in that interpreter (fastmcp +
code_review_graph 2.3.8).
"""

from __future__ import annotations

import asyncio
import json
import shutil
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
PLACEHOLDER = "${REPO_ROOT}"

# The tool calls recorded for this graph, as (case name, tool, arguments).
CALL_CASES: tuple[tuple[str, str, dict], ...] = (
    ("coupling_minimal", "get_architecture_overview_tool", {}),
    ("coupling_minimal_max_results", "get_architecture_overview_tool", {"max_results": 1}),
    ("coupling_standard", "get_architecture_overview_tool", {"detail_level": "standard"}),
    (
        "coupling_standard_max_results",
        "get_architecture_overview_tool",
        {"detail_level": "standard", "max_results": 5},
    ),
    (
        # `max_results` bounds the cross-community rows and the warnings; the
        # community list is NOT bounded by it.
        "coupling_standard_one_result",
        "get_architecture_overview_tool",
        {"detail_level": "standard", "max_results": 1},
    ),
    ("sort_name", "list_communities_tool", {"sort_by": "name", "detail_level": "minimal"}),
    ("min_size_boundary", "list_communities_tool", {"min_size": 3, "detail_level": "minimal"}),
    ("by_name_partial", "get_community_tool", {"community_name": "test"}),
    ("hard_cap_members", "get_community_tool", {"community_name": "epsilon", "max_members": 5000}),
    # The `zeta` community's name, description and one member's symbol carry
    # ASCII control characters, which the release strips on the way out.
    ("sanitized", "get_community_tool", {"community_name": "zeta", "include_members": True}),
)

_DUPLICATES = [
    key for key in {(tool, case) for case, tool, _ in CALL_CASES}
    if sum(1 for c, t, _ in CALL_CASES if (t, c) == key) > 1
]
if _DUPLICATES:
    raise SystemExit(f"duplicate call cases would overwrite each other: {_DUPLICATES}")


def materialise(graph: dict, repo: Path) -> None:
    """Write the synthetic graph into a release database under *repo*."""
    from code_review_graph.graph import GraphStore
    from code_review_graph.incremental import get_db_path

    root = str(repo)
    store = GraphStore(get_db_path(repo))
    try:
        conn = store._conn
        for node in graph["nodes"]:
            conn.execute(
                """INSERT INTO nodes
                   (id, kind, name, qualified_name, file_path, line_start,
                    line_end, language, parent_name, params, return_type,
                    modifiers, is_test, file_hash, extra, updated_at)
                   VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,'{}',0)""",
                (
                    node["id"], node["kind"], subst(node["name"], root),
                    subst(node["qualified_name"], root), subst(node["file_path"], root),
                    node["line_start"], node["line_end"], node["language"],
                    node["parent_name"], None, None, None,
                    1 if node["is_test"] else 0, "",
                ),
            )
        for edge in graph["edges"]:
            conn.execute(
                """INSERT INTO edges
                   (id, kind, source_qualified, target_qualified, file_path,
                    line, extra, updated_at)
                   VALUES (?,?,?,?,?,?,'{}',0)""",
                (
                    edge["id"], edge["kind"], subst(edge["source"], root),
                    subst(edge["target"], root), subst(edge["file_path"], root),
                    edge["line"],
                ),
            )
        for key, value in graph["metadata"].items():
            conn.execute(
                "INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)",
                (key, value),
            )
        conn.commit()

        from code_review_graph.communities import store_communities

        store_communities(
            store,
            [
                {
                    "name": c["name"],
                    "level": c["level"],
                    "cohesion": c["cohesion"],
                    "size": c["size"],
                    "dominant_language": c["dominant_language"],
                    "description": c["description"],
                    "members": [subst(m, root) for m in c["members"]],
                }
                for c in graph["communities"]
            ],
        )
    finally:
        store.close()


def subst(value: str, root: str) -> str:
    return value.replace(PLACEHOLDER, root)


def normalise(value, root: str):
    if isinstance(value, str):
        return value.replace(root, PLACEHOLDER)
    if isinstance(value, list):
        return [normalise(item, root) for item in value]
    if isinstance(value, dict):
        out = {key: normalise(item, root) for key, item in value.items()}
        # `_graph` is the build-provenance envelope the server attaches around
        # every tool result, not something a handler produces, and its
        # `age_seconds` changes on every run. Dropping it keeps this recording
        # byte-stable.
        out.pop("_graph", None)
        # `context_savings` counts tokens over payloads that embed absolute
        # paths, so both numbers scale with the length of the generation
        # root's path and are machine-dependent. The generated release
        # contract normalizes them for the same reason.
        savings = out.get("context_savings")
        if isinstance(savings, dict):
            if "saved_tokens" in savings:
                savings["saved_tokens"] = "${SAVED_TOKENS}"
            if "saved_percent" in savings:
                savings["saved_percent"] = "${SAVED_PERCENT}"
        return out
    return value


def record(repo: Path) -> list[dict]:
    from fastmcp import Client
    from code_review_graph import main as crg_main
    from code_review_graph.hints import reset_session

    crg_main._default_repo_root = str(repo)
    root = str(repo)

    async def run() -> list[dict]:
        out = []
        async with Client(crg_main.mcp) as client:
            for case, tool, arguments in CALL_CASES:
                # One fresh hint session per case, matching the generated
                # fixtures: `_hints.next_steps` suppresses tools already
                # called, so a shared session would make each recorded answer
                # depend on every case before it.
                reset_session()
                result = await client.call_tool(tool, arguments, raise_on_error=False)
                out.append(
                    {
                        "tool": tool,
                        "case": case,
                        "arguments": arguments,
                        "is_error": bool(result.is_error),
                        "structured_content": normalise(result.structured_content, root),
                    }
                )
        return out

    return asyncio.run(run())


def main() -> int:
    graph = json.loads((HERE / "graph.json").read_text())
    calls_dir = HERE / "calls"
    with tempfile.TemporaryDirectory() as tmp:
        repo = Path(tmp) / "repo"
        repo.mkdir()
        materialise(graph, repo)
        records = record(repo)

    if calls_dir.exists():
        shutil.rmtree(calls_dir)
    calls_dir.mkdir()
    for entry in records:
        path = calls_dir / f"{entry['tool']}__{entry['case']}.json"
        path.write_text(json.dumps(entry, indent=2, sort_keys=True) + "\n")
    print(f"wrote {len(records)} call answers to {calls_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
