"""Run one code-review-graph MCP tool and print its result as JSON.

This is the retained Phase-A bridge's execution body. It drives the release's
OWN FastMCP server object through the official in-memory client, so the
returned envelope — structured content, content blocks, and the isError flag
with the release's own error text — is produced by the release rather than
re-derived. Anything less would make "routed to the bridge" a different
answer from "answered by the release", which is the whole point of routing.

Input (stdin, one JSON object):
    {"tool": str, "arguments": {...}, "repo_root": str}

Output (stdout, exactly one JSON object):
    {"is_error": bool, "content": [{"type": "text", "text": str}],
     "structured_content": obj | null}
  or, when the tool could not be invoked at all:
    {"fatal_error": str}
"""

import asyncio
import json
import sys


def _emit(payload):
    json.dump(payload, sys.stdout)
    sys.stdout.flush()


def main() -> int:
    try:
        request = json.load(sys.stdin)
    except Exception as exc:  # noqa: BLE001 - reported to the Go caller
        _emit({"fatal_error": f"unreadable request: {type(exc).__name__}: {exc}"})
        return 1

    try:
        from fastmcp import Client
        from code_review_graph import main as crg_main
    except Exception as exc:  # noqa: BLE001 - reported to the Go caller
        _emit({"fatal_error": f"code-review-graph import failed: {type(exc).__name__}: {exc}"})
        return 1

    repo_root = request.get("repo_root") or None
    # Mirrors `code-review-graph serve --repo <X>`: tools that accept an
    # optional repo_root resolve it from this default.
    crg_main._default_repo_root = repo_root

    async def run():
        async with Client(crg_main.mcp) as client:
            result = await client.call_tool(
                request["tool"],
                request.get("arguments") or {},
                raise_on_error=False,
            )
            content = []
            for block in result.content:
                text = getattr(block, "text", None)
                if text is None:
                    continue
                content.append({"type": "text", "text": text})
            return {
                "is_error": bool(result.is_error),
                "content": content,
                "structured_content": result.structured_content,
            }

    try:
        _emit(asyncio.run(run()))
    except Exception as exc:  # noqa: BLE001 - reported to the Go caller
        _emit({"fatal_error": f"{type(exc).__name__}: {exc}"})
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
